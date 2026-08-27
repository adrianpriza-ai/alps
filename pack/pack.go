package pack

import (
	"os/exec"
	"strings"

	"github.com/adrianpriza-ai/alps/platform"
)

// Backend describes a native package manager.
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
var detectionOrder = []string{"apt", "apt-get", "dnf", "pacman", "zypper", "apk", "brew"}

// editSourcesBackends lists backends that support the edit-sources command.
var editSourcesBackends = map[string]bool{
	"apt":     true,
	"apt-get": true,
	"dnf":     true,
	"pacman":  true,
	"zypper":  true,
	"apk":     true,
}

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
		if _, err := exec.LookPath(b.Bin); err == nil {
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

// NeedsSudo checks if backend requires sudo.
func NeedsSudo(name string) bool {
	if platform.IsTermux() {
		return false
	}
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
	if _, supported := b.CmdMap[verb]; supported {
		return true
	}
	if verb == "edit-sources" {
		return editSourcesBackends[backendName]
	}
	return false
}

// yesSupportedBackends lists backends where alps exposes -y / --noconfirm.
// Only main package managers (apt, apt-get, pacman) are included per design.
var yesSupportedBackends = map[string]bool{
	"apt":     true,
	"apt-get": true,
	"pacman":  true,
}

// YesSupported returns true if the backend supports the alps -y flag.
func YesSupported(backendName string) bool {
	return yesSupportedBackends[backendName]
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
// Recognized alps flags: / -n simulate, no changes written, / -y skip confirmation prompts, / -v enable verbose output, / -q suppress non-error output, / -f force operation (skip safety checks).
func ParseFlags(args []string) (pkgs []string, dryRun, noConfirm bool) {
	for _, a := range args {
		switch {
		case a == "--dry-run" || a == "-n":
			dryRun = true
		case a == "--noconfirm" || a == "-y":
			noConfirm = true
		default:
			// --verbose, --quiet, --force are consumed by ParseFlagsExt;
			// pass through unknown args as package names.
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
// Pass the backend name so the correct native flags are emitted.
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

// DetectRealApt returns "apt" or "apt-get".
func DetectRealApt() string {
	if _, err := exec.LookPath("apt"); err == nil {
		return "apt"
	}
	return "apt-get"
}

func init() {
	// apt and apt-get
	Register(Backend{
		Name:        "apt",
		Bin:         "apt",
		Sudo:        true,
		YesFlag:     "-y",
		DryRunFlag:  "--dry-run",
		VerboseFlag: "-V",
		QuietFlag:   "-qq",
		ForceFlag:   "--force-yes",
		CmdMap: map[string][]string{
			"install":      {"apt", "install"},
			"remove":       {"apt", "remove"},
			"purge":        {"apt", "purge"},
			"update":       {"apt", "update"},
			"upgrade":      {"apt", "upgrade"},
			"full-upgrade": {"apt", "full-upgrade"},
			"search":       {"apt", "search"},
			"show":         {"apt", "show"},
			"list":         {"apt", "list"},
			"autoremove":   {"apt", "autoremove"},
			"autoclean":    {"apt", "autoclean"},
			"clean":        {"apt", "clean"},
		},
	})

	// apt-get
	Register(Backend{
		Name:        "apt-get",
		Bin:         "apt-get",
		Sudo:        true,
		YesFlag:     "-y",
		DryRunFlag:  "--dry-run",
		VerboseFlag: "-V",
		QuietFlag:   "-qq",
		ForceFlag:   "--force-yes",
		CmdMap: map[string][]string{
			"install":      {"apt-get", "install"},
			"remove":       {"apt-get", "remove"},
			"purge":        {"apt-get", "purge"},
			"update":       {"apt-get", "update"},
			"upgrade":      {"apt-get", "upgrade"},
			"full-upgrade": {"apt-get", "dist-upgrade"},
			"search":       {"apt-cache", "search"},
			"show":         {"apt-cache", "show"},
			"list":         {"dpkg", "--list"},
			"autoremove":   {"apt-get", "autoremove"},
			"autoclean":    {"apt-get", "autoclean"},
			"clean":        {"apt-get", "clean"},
		},
	})

	// dnf
	Register(Backend{
		Name:        "dnf",
		Bin:         "dnf",
		Sudo:        true,
		YesFlag:     "-y",
		DryRunFlag:  "--assumeno",
		VerboseFlag: "-v",
		QuietFlag:   "-q",
		ForceFlag:   "--skip-broken",
		CmdMap: map[string][]string{
			"install":      {"dnf", "install"},
			"remove":       {"dnf", "remove"},
			"purge":        {"dnf", "remove"},
			"update":       {"dnf", "check-update"},
			"upgrade":      {"dnf", "upgrade"},
			"full-upgrade": {"dnf", "upgrade", "--refresh"},
			"search":       {"dnf", "search"},
			"show":         {"dnf", "info"},
			"list":         {"dnf", "list"},
			"autoremove":   {"dnf", "autoremove"},
			"autoclean":    {"dnf", "clean", "all"},
			"clean":        {"dnf", "clean", "all"},
		},
	})

	// pacman
	Register(Backend{
		Name:        "pacman",
		Bin:         "pacman",
		Sudo:        true,
		YesFlag:     "--noconfirm",
		DryRunFlag:  "-p",
		VerboseFlag: "-v",
		QuietFlag:   "-q",
		ForceFlag:   "--overwrite=*",
		CmdMap: map[string][]string{
			"install":      {"pacman", "-S"},
			"remove":       {"pacman", "-R"},
			"purge":        {"pacman", "-Rns"},
			"update":       {"pacman", "-Sy"},
			"upgrade":      {"pacman", "-Su"},
			"full-upgrade": {"pacman", "-Syu"},
			"search":       {"pacman", "-Ss"},
			"show":         {"pacman", "-Si"},
			"list":         {"pacman", "-Q"},
			"clean":        {"pacman", "-Sc"},
		},
	})

	// zypper
	Register(Backend{
		Name:        "zypper",
		Bin:         "zypper",
		Sudo:        true,
		YesFlag:     "--non-interactive",
		DryRunFlag:  "--dry-run",
		VerboseFlag: "-v",
		QuietFlag:   "-q",
		ForceFlag:   "--force",
		CmdMap: map[string][]string{
			"install":      {"zypper", "install"},
			"remove":       {"zypper", "remove"},
			"purge":        {"zypper", "remove", "--clean-deps"},
			"update":       {"zypper", "refresh"},
			"upgrade":      {"zypper", "update"},
			"full-upgrade": {"zypper", "dist-upgrade"},
			"search":       {"zypper", "search"},
			"show":         {"zypper", "info"},
			"list":         {"zypper", "packages", "--installed-only"},
			"autoremove":   {"zypper", "remove", "--clean-deps", "--no-confirm"},
			"autoclean":    {"zypper", "clean", "--all"},
			"clean":        {"zypper", "clean", "--all"},
		},
	})

	// apk
	Register(Backend{
		Name:        "apk",
		Bin:         "apk",
		Sudo:        true,
		YesFlag:     "",
		DryRunFlag:  "--simulate",
		VerboseFlag: "-v",
		QuietFlag:   "-q",
		ForceFlag:   "--force-overwrite",
		CmdMap: map[string][]string{
			"install":      {"apk", "add"},
			"remove":       {"apk", "del"},
			"purge":        {"apk", "del", "--purge"},
			"update":       {"apk", "update"},
			"upgrade":      {"apk", "upgrade"},
			"full-upgrade": {"apk", "upgrade"},
			"search":       {"apk", "search"},
			"show":         {"apk", "info"},
			"list":         {"apk", "list", "--installed"},
			"autoremove":   {"apk", "fix", "--purge"}, // Alpine equivalent: fix dependencies and remove unused
			"autoclean":    {"apk", "cache", "clean"},
			"clean":        {"apk", "cache", "clean"},
		},
	})

	// brew (Homebrew)
	Register(Backend{
		Name:        "brew",
		Bin:         "brew",
		Sudo:        false,
		YesFlag:     "",
		DryRunFlag:  "",
		VerboseFlag: "-v",
		QuietFlag:   "-q",
		ForceFlag:   "--force",
		CmdMap: map[string][]string{
			"install":      {"brew", "install"},
			"remove":       {"brew", "uninstall"},
			"purge":        {"brew", "uninstall"},
			"update":       {"brew", "update"},
			"upgrade":      {"brew", "upgrade"},
			"full-upgrade": {"brew", "upgrade"},
			"search":       {"brew", "search"},
			"show":         {"brew", "info"},
			"list":         {"brew", "list"},
			"autoremove":   {"brew", "autoremove"},
			"autoclean":    {"brew", "cleanup"},
			"clean":        {"brew", "cleanup"},
		},
	})
}
