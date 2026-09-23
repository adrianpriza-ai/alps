package extra

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/adrianpriza-ai/alps/internal/pkgregistry"
	"github.com/adrianpriza-ai/alps/platform"
	"github.com/adrianpriza-ai/alps/priv"
)

// Backend describes a container/flatpak-style package manager.
type Backend = pkgregistry.Backend

// Flags holds all parsed alps meta-flags from user args.
type Flags = pkgregistry.Flags

var reg = pkgregistry.New([]string{"snap", "flatpak", "winget"})

// extraVerbs maps backend names to verbs that are supported outside of
// CmdMap. These are commands the backend handles through its own logic
// rather than a direct command-line mapping.
var extraVerbs = map[string]map[string]bool{
	"snap":    {"purge": true, "show": true, "upgrade": true},
	"flatpak": {"purge": true, "show": true, "upgrade": true, "clean": true},
	"winget":  {"purge": true, "show": true, "upgrade": true},
}

func init() {
	reg.SetExtraVerbs(extraVerbsSupported)

	// snap
	reg.Register(Backend{
		Name: "snap",
		Bin:  "snap",
		Sudo: true,
		CmdMap: map[string][]string{
			"install": {"snap", "install"},
			"remove":  {"snap", "remove"},
			"purge":   {"snap", "remove", "--purge"},
			"search":  {"snap", "find"},
			"show":    {"snap", "info"},
			"list":    {"snap", "list"},
			"update":  {"snap", "refresh"},
			"upgrade": {"snap", "refresh"},
		},
	})

	// flatpak
	reg.Register(Backend{
		Name:    "flatpak",
		Bin:     "flatpak",
		Sudo:    false,
		YesFlag: "-y",
		CmdMap: map[string][]string{
			"install":    {"flatpak", "install", "flathub"},
			"remove":     {"flatpak", "remove"},
			"purge":      {"flatpak", "remove", "--delete-data"},
			"search":     {"flatpak", "search"},
			"show":       {"flatpak", "info"},
			"list":       {"flatpak", "list", "--app", "--columns=name,application,version"},
			"update":     {"flatpak", "update"},
			"upgrade":    {"flatpak", "update"},
			"autoremove": {"flatpak", "uninstall", "--unused"},
			"clean":      {"flatpak", "uninstall", "--unused"},
		},
	})

	// winget (Windows Package Manager)
	reg.Register(Backend{
		Name: "winget",
		Bin:  "winget.exe",
		Sudo: false,
		CmdMap: map[string][]string{
			"install": {"winget.exe", "install"},
			"remove":  {"winget.exe", "uninstall"},
			"purge":   {"winget.exe", "uninstall"},
			"search":  {"winget.exe", "search"},
			"show":    {"winget.exe", "show"},
			"list":    {"winget.exe", "list"},
			"update":  {"winget.exe", "upgrade"},
			// Intentional: upgrade runs winget upgrade --all to upgrade all packages at once.
			// This is aggressive by design; users should review with 'alps winget update' first.
			"upgrade": {"winget.exe", "upgrade", "--all"},
		},
	})
}

// extraVerbsSupported reports verbs handled outside of CmdMap.
func extraVerbsSupported(backendName, verb string) bool {
	if extras, ok := extraVerbs[backendName]; ok {
		return extras[verb]
	}
	return false
}

// Register adds a backend.
func Register(b Backend) { reg.Register(b) }

// isWingetAvailable checks if winget.exe is available in WSL.
func isWingetAvailable() bool {
	_, err := exec.LookPath("winget.exe")
	return err == nil
}

// IsAvailable checks if a specific backend is available.
func IsAvailable(backendName string) bool {
	switch backendName {
	case "snap":
		return platform.HasSnapd()
	case "flatpak":
		_, err := exec.LookPath("flatpak")
		return err == nil
	case "winget":
		return platform.IsWSL() && isWingetAvailable()
	default:
		return false
	}
}

// NeedsSudo checks if backend requires sudo.
func NeedsSudo(name string) bool {
	return reg.Sudo(name)
}

// Lookup returns a copy of the command for a backend so callers appending
// to the result cannot corrupt the registry's backing array.
func Lookup(backendName, verb string) (cmd []string, ok bool) {
	return reg.Lookup(backendName, verb)
}

// CommandSupported checks if a backend supports a specific command.
func CommandSupported(backendName, verb string) bool {
	return reg.CommandSupported(backendName, verb)
}

// YesSupported returns true if the backend supports the alps -y flag.
func YesSupported(backendName string) bool { return reg.YesSupported(backendName) }

// GetYesFlag returns the native "assume yes" flag for a backend, or "" if none.
func GetYesFlag(backendName string) string { return reg.GetYesFlag(backendName) }

// ForceFlags returns the native flags implementing alps -f for a backend.
// An empty result means the backend has no equivalent and -f is ignored.
func ForceFlags(backendName string) []string { return reg.ForceFlags(backendName) }

// UnsupportedFlagWarnings returns warnings for alps flags the backend cannot
// honour. Messages are printed by the caller (extra cannot import ui).
func UnsupportedFlagWarnings(backendName string, noConfirm, force bool) []string {
	return reg.UnsupportedFlagWarnings(backendName, noConfirm, force)
}

// GetDryRunFlag returns the native dry-run / simulation flag for a backend, or "" if none.
func GetDryRunFlag(backendName string) string { return reg.GetDryRunFlag(backendName) }

// ParseFlags splits raw args into package names and Flags struct.
func ParseFlags(args []string) (pkgs []string, dryRun, noConfirm bool) {
	return pkgregistry.ParseFlags(args)
}

// ParseFlagsExt is the full flag parser returning a Flags struct.
func ParseFlagsExt(args []string) (pkgs []string, f Flags) {
	return pkgregistry.ParseFlagsExt(args)
}

// BuildExtraFlags assembles the extra flags slice to append to a backend command based on the resolved Flags state.
func BuildExtraFlags(backendName string, dryRun, noConfirm bool) []string {
	return reg.BuildExtraFlags(backendName, dryRun, noConfirm)
}

// BuildExtraFlagsExt is the full version of BuildExtraFlags that accepts a Flags struct.
func BuildExtraFlagsExt(backendName string, f Flags) []string {
	return reg.BuildExtraFlagsExt(backendName, f)
}

// AllNames returns all registered backend names.
func AllNames() []string { return reg.AllNames() }

// runCommand builds and runs a backend command, elevating with priv when the
// backend needs sudo. includeStdin attaches the terminal for interactive
// commands (install/remove prompts); read-only verbs omit it.
func runCommand(backendName string, args []string, includeStdin bool) error {
	var cmd *exec.Cmd
	var err error

	if NeedsSudo(backendName) {
		cmd, err = priv.Command(args...)
		if err != nil {
			return err
		}
	} else {
		cmd = exec.Command(args[0], args[1:]...)
	}

	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if includeStdin {
		cmd.Stdin = os.Stdin
	}
	return cmd.Run()
}

// buildVerbArgs assembles the full argument list for a backend verb: the
// registered command, the verb's trailing arguments, and the native flags
// implementing the alps meta-flags in f (e.g. flatpak -y).
func buildVerbArgs(backendName, verb string, f Flags, trailing ...string) ([]string, error) {
	args, ok := Lookup(backendName, verb)
	if !ok {
		return nil, fmt.Errorf("%s not supported by %s", verb, backendName)
	}
	args = append(args, trailing...)
	args = append(args, BuildExtraFlagsExt(backendName, f)...)
	return args, nil
}

// Install installs packages via the specified backend.
// The classic parameter is only used for snap backend.
func Install(backendName string, pkgNames []string, classic bool, f Flags) error {
	args, err := buildVerbArgs(backendName, "install", f, pkgNames...)
	if err != nil {
		return err
	}
	if backendName == "snap" && classic {
		args = append(args, "--classic")
	}

	if err := runCommand(backendName, args, true); err != nil {
		return fmt.Errorf("%s install failed: %w", backendName, err)
	}
	return nil
}

// Remove removes a package via the specified backend.
func Remove(backendName string, pkgName string, f Flags) error {
	args, err := buildVerbArgs(backendName, "remove", f, pkgName)
	if err != nil {
		return err
	}

	if err := runCommand(backendName, args, true); err != nil {
		return fmt.Errorf("%s remove failed: %w", backendName, err)
	}
	return nil
}

// Purge removes a package and its configuration via the specified backend.
func Purge(backendName string, pkgName string, f Flags) error {
	args, err := buildVerbArgs(backendName, "purge", f, pkgName)
	if err != nil {
		// Fall back to remove if purge not supported
		return Remove(backendName, pkgName, f)
	}

	if err := runCommand(backendName, args, true); err != nil {
		return fmt.Errorf("%s purge failed: %w", backendName, err)
	}
	return nil
}

// Search searches for packages via the specified backend.
func Search(backendName string, query string, f Flags) error {
	args, err := buildVerbArgs(backendName, "search", f, query)
	if err != nil {
		return err
	}
	if err := runCommand(backendName, args, false); err != nil {
		return fmt.Errorf("%s search failed: %w", backendName, err)
	}
	return nil
}

// Show shows package information via the specified backend.
func Show(backendName string, pkgName string, f Flags) error {
	args, err := buildVerbArgs(backendName, "show", f, pkgName)
	if err != nil {
		return err
	}
	if err := runCommand(backendName, args, false); err != nil {
		return fmt.Errorf("%s show failed: %w", backendName, err)
	}
	return nil
}

// List lists installed packages via the specified backend.
func List(backendName string) error {
	args, ok := Lookup(backendName, "list")
	if !ok {
		return fmt.Errorf("list not supported by %s", backendName)
	}

	if err := runCommand(backendName, args, false); err != nil {
		return fmt.Errorf("%s list failed: %w", backendName, err)
	}
	return nil
}

// Update updates package lists via the specified backend.
func Update(backendName string, f Flags) error {
	args, err := buildVerbArgs(backendName, "update", f)
	if err != nil {
		return err
	}

	if err := runCommand(backendName, args, true); err != nil {
		return fmt.Errorf("%s update failed: %w", backendName, err)
	}
	return nil
}

// Upgrade upgrades packages via the specified backend.
func Upgrade(backendName string, f Flags) error {
	args, err := buildVerbArgs(backendName, "upgrade", f)
	if err != nil {
		return err
	}

	if err := runCommand(backendName, args, true); err != nil {
		return fmt.Errorf("%s upgrade failed: %w", backendName, err)
	}
	return nil
}

// Autoremove removes unused dependencies via the specified backend.
func Autoremove(backendName string, f Flags) error {
	args, err := buildVerbArgs(backendName, "autoremove", f)
	if err != nil {
		return err
	}

	if err := runCommand(backendName, args, true); err != nil {
		return fmt.Errorf("%s autoremove failed: %w", backendName, err)
	}
	return nil
}

// Clean cleans package cache via the specified backend.
func Clean(backendName string, f Flags) error {
	args, err := buildVerbArgs(backendName, "clean", f)
	if err != nil {
		return err
	}

	if err := runCommand(backendName, args, true); err != nil {
		return fmt.Errorf("%s clean failed: %w", backendName, err)
	}
	return nil
}
