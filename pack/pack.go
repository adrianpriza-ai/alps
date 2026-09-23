package pack

import (
	"os/exec"

	"github.com/adrianpriza-ai/alps/internal/pkgregistry"
	"github.com/adrianpriza-ai/alps/platform"
)

// Backend describes a native package manager.
type Backend = pkgregistry.Backend

// Flags holds all parsed alps meta-flags from user args.
type Flags = pkgregistry.Flags

var reg = pkgregistry.New([]string{"apt", "apt-get", "dnf", "pacman", "zypper", "apk", "brew"})

// editSourcesBackends lists backends that support the edit-sources command.
var editSourcesBackends = map[string]bool{
	"apt":     true,
	"apt-get": true,
	"dnf":     true,
	"pacman":  true,
	"zypper":  true,
	"apk":     true,
}

func init() {
	reg.SetExtraVerbs(func(backendName, verb string) bool {
		return verb == "edit-sources" && editSourcesBackends[backendName]
	})

	// apt and apt-get
	reg.Register(Backend{
		Name:        "apt",
		Bin:         "apt",
		Sudo:        true,
		YesFlag:     "-y",
		DryRunFlag:  "--dry-run",
		VerboseFlag: "-V",
		QuietFlag:   "-qq",
		ForceFlag:   []string{"--allow-downgrades", "--allow-change-held-packages"},
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
	reg.Register(Backend{
		Name:        "apt-get",
		Bin:         "apt-get",
		Sudo:        true,
		YesFlag:     "-y",
		DryRunFlag:  "--dry-run",
		VerboseFlag: "-V",
		QuietFlag:   "-qq",
		ForceFlag:   []string{"--allow-downgrades", "--allow-change-held-packages"},
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
	reg.Register(Backend{
		Name:        "dnf",
		Bin:         "dnf",
		Sudo:        true,
		YesFlag:     "-y",
		DryRunFlag:  "--assumeno",
		VerboseFlag: "-v",
		QuietFlag:   "-q",
		ForceFlag:   nil,
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
	reg.Register(Backend{
		Name:    "pacman",
		Bin:     "pacman",
		Sudo:    true,
		YesFlag: "--noconfirm",
		// pacman(8) documents --print under transaction options applying to
		// -S, -R and -U, so -p is valid for remove and purge verbs too.
		DryRunFlag:  "-p",
		VerboseFlag: "-v",
		QuietFlag:   "-q",
		ForceFlag:   nil, // --overwrite=* is too broad to bind to alps -f
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
	reg.Register(Backend{
		Name:        "zypper",
		Bin:         "zypper",
		Sudo:        true,
		YesFlag:     "--no-confirm",
		DryRunFlag:  "--dry-run",
		VerboseFlag: "-v",
		QuietFlag:   "-q",
		ForceFlag:   []string{"--force-resolution"},
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
			"autoremove":   {"zypper", "remove", "--clean-deps"},
			"autoclean":    {"zypper", "clean", "--all"},
			"clean":        {"zypper", "clean", "--all"},
		},
	})

	// apk
	reg.Register(Backend{
		Name:        "apk",
		Bin:         "apk",
		Sudo:        true,
		YesFlag:     "",
		DryRunFlag:  "--simulate",
		VerboseFlag: "-v",
		QuietFlag:   "-q",
		ForceFlag:   nil,
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
	reg.Register(Backend{
		Name:        "brew",
		Bin:         "brew",
		Sudo:        false,
		YesFlag:     "",
		DryRunFlag:  "",
		VerboseFlag: "-v",
		QuietFlag:   "-q",
		ForceFlag:   nil,
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

// Register adds a backend.
func Register(b Backend) { reg.Register(b) }

// Detect returns the first available backend.
func Detect() *Backend { return reg.Detect() }

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

// UnsupportedFlagWarnings returns warnings for alps flags the backend cannot
// honour. Messages are printed by the caller (pack cannot import ui).
func UnsupportedFlagWarnings(backendName string, noConfirm, force bool) []string {
	return reg.UnsupportedFlagWarnings(backendName, noConfirm, force)
}

// GetYesFlag returns the native "assume yes" flag for a backend, or "" if none.
func GetYesFlag(backendName string) string { return reg.GetYesFlag(backendName) }

// ForceFlags returns the native flags implementing alps -f for a backend.
// An empty result means the backend has no equivalent and -f is ignored.
func ForceFlags(backendName string) []string { return reg.ForceFlags(backendName) }

// GetDryRunFlag returns the native dry-run / simulation flag for a backend, or "" if none.
func GetDryRunFlag(backendName string) string { return reg.GetDryRunFlag(backendName) }

// DryRunEmitted reports whether a backend emits a native simulation flag.
// Backends without one cannot show a package plan in dry-run mode.
func DryRunEmitted(backendName string) bool { return reg.DryRunEmitted(backendName) }

// ParseFlags splits raw args into package names and Flags struct.
// Recognized alps flags: -n simulate, no changes written; -y skip
// confirmation prompts. --verbose, --quiet and --force are consumed by
// ParseFlagsExt.
func ParseFlags(args []string) (pkgs []string, dryRun, noConfirm bool) {
	return pkgregistry.ParseFlags(args)
}

// ParseFlagsExt is the full flag parser returning a Flags struct.
func ParseFlagsExt(args []string) (pkgs []string, f Flags) {
	return pkgregistry.ParseFlagsExt(args)
}

// BuildExtraFlags assembles the extra flags slice to append to a backend command based on the resolved Flags state.
// Pass the backend name so the correct native flags are emitted.
func BuildExtraFlags(backendName string, dryRun, noConfirm bool) []string {
	return reg.BuildExtraFlags(backendName, dryRun, noConfirm)
}

// BuildExtraFlagsExt is the full version of BuildExtraFlags that accepts a Flags struct.
// Dry-run is print-only (the runner never executes), so no native dry-run
// flag is appended; callers that genuinely execute a simulation use
// GetDryRunFlag directly.
func BuildExtraFlagsExt(backendName string, f Flags) []string {
	return reg.BuildExtraFlagsExt(backendName, f)
}

// AllNames returns all registered backend names.
func AllNames() []string { return reg.AllNames() }

// DetectRealApt returns "apt" or "apt-get".
func DetectRealApt() string {
	if _, err := exec.LookPath("apt"); err == nil {
		return "apt"
	}
	return "apt-get"
}
