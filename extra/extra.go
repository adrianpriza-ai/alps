package extra

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/adrianpriza-ai/alps/priv"
)

// Backend describes a container/flatpak-style package manager.
type Backend struct {
	Name        string
	Bin         string
	Sudo        bool
	CmdMap      map[string][]string
	YesFlag     string
	DryRunFlag  string
	VerboseFlag string
	QuietFlag   string
	ForceFlag   string
}

var registry = map[string]*Backend{} // backend name to Backend
var detectionOrder = []string{"snap", "flatpak", "winget"}

// Register adds a backend.
func Register(b Backend) {
	cp := b
	registry[b.Name] = &cp
}

// Detect returns the first available backend.
func Detect() *Backend {
	for _, name := range detectionOrder {
		b, ok := registry[name]
		if !ok {
			continue
		}
		if name == "winget" {
			if isWSL() && isWingetAvailable() {
				return b
			}
			continue
		}
		if _, err := exec.LookPath(b.Bin); err == nil {
			// Special check for snap to ensure it's not blocked
			if name == "snap" && !isSnapAvailable() {
				continue
			}
			return b
		}
	}
	return nil
}

// DetectName returns the detected backend name.
func DetectName() string {
	if b := Detect(); b != nil {
		return b.Name
	}
	return ""
}

// isWSL checks if running on Windows Subsystem for Linux.
func isWSL() bool {
	if os.Getenv("WSL_DISTRO_NAME") != "" || os.Getenv("WSL_INTEROP") != "" {
		return true
	}
	if data, err := os.ReadFile("/proc/version"); err == nil {
		lower := strings.ToLower(string(data))
		return strings.Contains(lower, "microsoft") || strings.Contains(lower, "wsl")
	}
	return false
}

// isWingetAvailable checks if winget.exe is available in WSL.
func isWingetAvailable() bool {
	_, err := exec.LookPath("winget.exe")
	return err == nil
}

// isSnapAvailable checks if snapd is running and not blocked.
func isSnapAvailable() bool {
	if _, err := os.Stat("/etc/apt/preferences.d/nosnap.pref"); err == nil {
		return false
	}
	return exec.Command("systemctl", "is-active", "--quiet", "snapd").Run() == nil
}

// NeedsSudo checks if backend requires sudo.
func NeedsSudo(name string) bool {
	if b, ok := registry[name]; ok {
		return b.Sudo
	}
	return false
}

// Lookup returns the command for a backend.
func Lookup(backendName, verb string) (cmd []string, ok bool) {
	b, found := registry[backendName]
	if !found {
		return nil, false
	}
	c, found := b.CmdMap[verb]
	return c, found
}

// CommandSupported checks if a backend supports a specific command.
func CommandSupported(backendName, verb string) bool {
	b, found := registry[backendName]
	if !found {
		return false
	}
	_, supported := b.CmdMap[verb]
	if supported {
		return true
	}
	// Handle special cases where commands may not be in CmdMap but are still supported
	switch backendName {
	case "snap":
		if verb == "purge" || verb == "show" || verb == "upgrade" {
			return true
		}
	case "flatpak":
		if verb == "purge" || verb == "show" || verb == "upgrade" || verb == "clean" {
			return true
		}
	case "winget":
		if verb == "purge" || verb == "show" || verb == "upgrade" {
			return true
		}
	}
	return false
}

// YesSupported returns true if the backend supports the alps -y flag.
func YesSupported(backendName string) bool {
	// Currently, snap and flatpak don't support -y in the same way as native package managers
	// flatpak has -y but we handle it differently
	return false
}

// GetYesFlag returns the native "assume yes" flag for a backend, or "" if none.
func GetYesFlag(backendName string) string {
	if b, ok := registry[backendName]; ok {
		return b.YesFlag
	}
	return ""
}

// GetDryRunFlag returns the native dry-run / simulation flag for a backend, or "" if none.
func GetDryRunFlag(backendName string) string {
	if b, ok := registry[backendName]; ok {
		return b.DryRunFlag
	}
	return ""
}

// Flags holds all parsed alps meta-flags from user args.
type Flags struct {
	DryRun    bool
	NoConfirm bool
	Verbose   bool
	Quiet     bool
	Force     bool
}

// ParseFlags splits raw args into package names and Flags struct.
func ParseFlags(args []string) (pkgs []string, dryRun, noConfirm bool) {
	for _, a := range args {
		switch {
		case a == "--dry-run" || a == "-n":
			dryRun = true
		case a == "--noconfirm" || a == "-y":
			noConfirm = true
		default:
			pkgs = append(pkgs, a)
		}
	}
	return
}

// ParseFlagsExt is the full flag parser returning a Flags struct.
func ParseFlagsExt(args []string) (pkgs []string, f Flags) {
	for _, a := range args {
		switch {
		case a == "--dry-run" || a == "-n":
			f.DryRun = true
		case a == "--noconfirm" || a == "-y":
			f.NoConfirm = true
		case a == "--verbose" || a == "-v":
			f.Verbose = true
		case a == "--quiet" || a == "-q":
			f.Quiet = true
		case a == "--force" || a == "-f":
			f.Force = true
		default:
			pkgs = append(pkgs, a)
		}
	}
	return
}

// BuildExtraFlags assembles the extra flags slice to append to a backend command based on the resolved Flags state.
func BuildExtraFlags(backendName string, dryRun, noConfirm bool) []string {
	return BuildExtraFlagsExt(backendName, Flags{DryRun: dryRun, NoConfirm: noConfirm})
}

// BuildExtraFlagsExt is the full version of BuildExtraFlags that accepts a Flags struct.
func BuildExtraFlagsExt(backendName string, f Flags) []string {
	b, ok := registry[backendName]
	if !ok {
		return nil
	}
	var flags []string

	if f.DryRun && b.DryRunFlag != "" {
		flags = appendUniq(flags, b.DryRunFlag)
	}
	if f.NoConfirm && YesSupported(backendName) && b.YesFlag != "" {
		flags = appendUniq(flags, b.YesFlag)
	}
	if f.Verbose && b.VerboseFlag != "" {
		flags = appendUniq(flags, b.VerboseFlag)
	}
	if f.Quiet && b.QuietFlag != "" {
		flags = appendUniq(flags, b.QuietFlag)
	}
	if f.Force && b.ForceFlag != "" {
		flags = appendUniq(flags, b.ForceFlag)
	}
	return flags
}

// appendUniq appends s to slice only if not already present (case-insensitive).
func appendUniq(flags []string, s string) []string {
	for _, existing := range flags {
		if strings.EqualFold(existing, s) {
			return flags
		}
	}
	return append(flags, s)
}

// AllNames returns all registered backend names.
func AllNames() []string {
	out := make([]string, 0, len(detectionOrder))
	for _, name := range detectionOrder {
		if _, ok := registry[name]; ok {
			out = append(out, name)
		}
	}
	return out
}

// IsAvailable checks if a specific backend is available.
func IsAvailable(backendName string) bool {
	switch backendName {
	case "snap":
		if _, err := exec.LookPath("snap"); err != nil {
			return false
		}
		return isSnapAvailable()
	case "flatpak":
		_, err := exec.LookPath("flatpak")
		return err == nil
	case "winget":
		return isWSL() && isWingetAvailable()
	default:
		return false
	}
}

// Install installs packages via the specified backend.
// The classic parameter is only used for snap backend.
func Install(backendName string, pkgNames []string, classic bool) error {
	args, ok := Lookup(backendName, "install")
	if !ok {
		return fmt.Errorf("install not supported by %s", backendName)
	}

	args = append(args, pkgNames...)
	if backendName == "snap" && classic {
		args = append(args, "--classic")
	}

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
	cmd.Stdin = os.Stdin
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s install failed: %w", backendName, err)
	}
	return nil
}

// Remove removes a package via the specified backend.
func Remove(backendName string, pkgName string) error {
	args, ok := Lookup(backendName, "remove")
	if !ok {
		return fmt.Errorf("remove not supported by %s", backendName)
	}

	args = append(args, pkgName)

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
	cmd.Stdin = os.Stdin
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s remove failed: %w", backendName, err)
	}
	return nil
}

// Purge removes a package and its configuration via the specified backend.
func Purge(backendName string, pkgName string) error {
	args, ok := Lookup(backendName, "purge")
	if !ok {
		// Fall back to remove if purge not supported
		return Remove(backendName, pkgName)
	}

	args = append(args, pkgName)

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
	cmd.Stdin = os.Stdin
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s purge failed: %w", backendName, err)
	}
	return nil
}

// Search searches for packages via the specified backend.
func Search(backendName string, query string) error {
	args, ok := Lookup(backendName, "search")
	if !ok {
		return fmt.Errorf("search not supported by %s", backendName)
	}

	args = append(args, query)
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s search failed: %w", backendName, err)
	}
	return nil
}

// Show shows package information via the specified backend.
func Show(backendName string, pkgName string) error {
	args, ok := Lookup(backendName, "show")
	if !ok {
		return fmt.Errorf("show not supported by %s", backendName)
	}

	args = append(args, pkgName)
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
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

	cmd := exec.Command(args[0], args[1:]...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s list failed: %w", backendName, err)
	}
	return nil
}

// Update updates package lists via the specified backend.
func Update(backendName string) error {
	args, ok := Lookup(backendName, "update")
	if !ok {
		return fmt.Errorf("update not supported by %s", backendName)
	}

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
	cmd.Stdin = os.Stdin
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s update failed: %w", backendName, err)
	}
	return nil
}

// Upgrade upgrades packages via the specified backend.
func Upgrade(backendName string) error {
	args, ok := Lookup(backendName, "upgrade")
	if !ok {
		return fmt.Errorf("upgrade not supported by %s", backendName)
	}

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
	cmd.Stdin = os.Stdin
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s upgrade failed: %w", backendName, err)
	}
	return nil
}

// Autoremove removes unused dependencies via the specified backend.
func Autoremove(backendName string) error {
	args, ok := Lookup(backendName, "autoremove")
	if !ok {
		return fmt.Errorf("autoremove not supported by %s", backendName)
	}

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
	cmd.Stdin = os.Stdin
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s autoremove failed: %w", backendName, err)
	}
	return nil
}

// Clean cleans package cache via the specified backend.
func Clean(backendName string) error {
	args, ok := Lookup(backendName, "clean")
	if !ok {
		return fmt.Errorf("clean not supported by %s", backendName)
	}

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
	cmd.Stdin = os.Stdin
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s clean failed: %w", backendName, err)
	}
	return nil
}

// Exists checks if a package exists in the backend's repository.
func Exists(backendName string, pkgName string) bool {
	switch backendName {
	case "snap":
		out, err := exec.Command("snap", "find", "--narrow", pkgName).Output()
		if err != nil {
			return false
		}
		lines := strings.Split(string(out), "\n")
		for _, line := range lines[1:] {
			fields := strings.Fields(line)
			if len(fields) > 0 && strings.EqualFold(fields[0], pkgName) {
				return true
			}
		}
		return false
	case "flatpak":
		out, err := exec.Command("flatpak", "search", "--columns=application", pkgName).Output()
		if err != nil {
			return false
		}
		return strings.Contains(strings.ToLower(string(out)), strings.ToLower(pkgName))
	case "winget":
		out, err := exec.Command("winget.exe", "search", pkgName).Output()
		if err != nil {
			return false
		}
		return strings.Contains(strings.ToLower(string(out)), strings.ToLower(pkgName))
	default:
		return false
	}
}

func init() {
	// snap
	Register(Backend{
		Name:        "snap",
		Bin:         "snap",
		Sudo:        true,
		YesFlag:     "",
		DryRunFlag:  "",
		VerboseFlag: "",
		QuietFlag:   "",
		ForceFlag:   "",
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
	Register(Backend{
		Name:        "flatpak",
		Bin:         "flatpak",
		Sudo:        false,
		YesFlag:     "-y",
		DryRunFlag:  "",
		VerboseFlag: "",
		QuietFlag:   "",
		ForceFlag:   "",
		CmdMap: map[string][]string{
			"install": {"flatpak", "install", "flathub"},
			"remove":  {"flatpak", "remove"},
			"purge":   {"flatpak", "remove", "--delete-data"},
			"search":  {"flatpak", "search"},
			"show":    {"flatpak", "info"},
			"list":    {"flatpak", "list", "--app", "--columns=name,application,version"},
			"update":  {"flatpak", "update"},
			"upgrade": {"flatpak", "update"},
			"clean":   {"flatpak", "uninstall", "--unused"},
		},
	})

	// winget (Windows Package Manager)
	Register(Backend{
		Name:        "winget",
		Bin:         "winget.exe",
		Sudo:        false,
		YesFlag:     "",
		DryRunFlag:  "",
		VerboseFlag: "",
		QuietFlag:   "",
		ForceFlag:   "",
		CmdMap: map[string][]string{
			"install": {"winget.exe", "install"},
			"remove":  {"winget.exe", "uninstall"},
			"purge":   {"winget.exe", "uninstall"},
			"search":  {"winget.exe", "search"},
			"show":    {"winget.exe", "show"},
			"list":    {"winget.exe", "list"},
			"update":  {"winget.exe", "upgrade"},
			"upgrade": {"winget.exe", "upgrade", "--all"},
		},
	})
}
