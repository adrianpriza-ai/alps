package pkgregistry

import (
	"fmt"
	"os/exec"
)

// Backend describes a package manager registry entry. pack and extra both
// store their per-backend tables in this type; only the tables differ.
type Backend struct {
	Name   string
	Bin    string
	Sudo   bool
	CmdMap map[string][]string

	// ForceFlag lists the native flags implementing alps -f ("force").
	// Empty means the backend has no equivalent; -f then warns and is ignored.
	ForceFlag []string

	// DryRunFlag is the backend's native simulation flag. The main command
	// path runs dry-run as print-only (the runner never executes), so it is
	// not appended there; it is only for callers that genuinely execute a
	// simulation, such as pacmanInstallRepoPkgs.
	DryRunFlag  string
	YesFlag     string
	VerboseFlag string
	QuietFlag   string
}

// Registry is a named set of backends with an ordered detection list.
type Registry struct {
	backends       map[string]*Backend
	detectionOrder []string

	// extraVerbs, when set, decides verbs supported outside of CmdMap.
	extraVerbs func(backendName, verb string) bool
}

// New creates an empty registry with the given detection order.
func New(detectionOrder []string) *Registry {
	return &Registry{
		backends:       map[string]*Backend{},
		detectionOrder: append([]string(nil), detectionOrder...),
	}
}

// Register adds a backend to the registry.
func (r *Registry) Register(b Backend) {
	cp := b
	r.backends[b.Name] = &cp
}

// Detect returns the first available backend in detection order.
func (r *Registry) Detect() *Backend {
	for _, name := range r.detectionOrder {
		b, ok := r.backends[name]
		if !ok {
			continue
		}
		if _, err := exec.LookPath(b.Bin); err == nil {
			return b
		}
	}
	return nil
}

// AllNames returns all registered backend names in detection order.
func (r *Registry) AllNames() []string {
	out := make([]string, 0, len(r.detectionOrder))
	for _, name := range r.detectionOrder {
		if _, ok := r.backends[name]; ok {
			out = append(out, name)
		}
	}
	return out
}

// Lookup returns a copy of the command for a backend so callers appending
// to the result cannot corrupt the registry's backing array.
func (r *Registry) Lookup(backendName, verb string) (cmd []string, ok bool) {
	b, found := r.backends[backendName]
	if !found {
		return nil, false
	}
	c, found := b.CmdMap[verb]
	if !found {
		return nil, false
	}
	out := make([]string, len(c))
	copy(out, c)
	return out, true
}

// CommandSupported checks if a backend supports a specific command: either a
// CmdMap entry or an extra-verbs hook the owning package installed.
func (r *Registry) CommandSupported(backendName, verb string) bool {
	b, found := r.backends[backendName]
	if !found {
		return false
	}
	if _, supported := b.CmdMap[verb]; supported {
		return true
	}
	if r.extraVerbs != nil {
		return r.extraVerbs(backendName, verb)
	}
	return false
}

// SetExtraVerbs installs a hook deciding verbs supported outside of CmdMap.
// extra uses it for verbs handled through its own logic; pack uses the
// edit-sources hook below, which is the same mechanism with a fixed table.
func (r *Registry) SetExtraVerbs(fn func(backendName, verb string) bool) {
	r.extraVerbs = fn
}

// Sudo reports whether the backend's commands need elevated privileges.
func (r *Registry) Sudo(backendName string) bool {
	if b, ok := r.backends[backendName]; ok {
		return b.Sudo
	}
	return false
}

// GetYesFlag returns the native "assume yes" flag for a backend, or "" if none.
func (r *Registry) GetYesFlag(backendName string) string {
	if b, ok := r.backends[backendName]; ok {
		return b.YesFlag
	}
	return ""
}

// ForceFlags returns the native flags implementing alps -f for a backend.
// An empty result means the backend has no equivalent and -f is ignored.
func (r *Registry) ForceFlags(backendName string) []string {
	if b, ok := r.backends[backendName]; ok {
		return b.ForceFlag
	}
	return nil
}

// GetDryRunFlag returns the native dry-run / simulation flag for a backend,
// or "" if none.
func (r *Registry) GetDryRunFlag(backendName string) string {
	if b, ok := r.backends[backendName]; ok {
		return b.DryRunFlag
	}
	return ""
}

// YesSupported reports whether the backend can emit an assume-yes flag.
// A backend supports -y exactly when a YesFlag is configured, so a dead
// YesFlag (set but never emitted) is impossible by construction.
func (r *Registry) YesSupported(backendName string) bool {
	return r.GetYesFlag(backendName) != ""
}

// UnsupportedFlagWarnings returns warnings for alps flags the backend cannot
// honour. Messages are printed by the caller (registry packages cannot
// import ui).
func (r *Registry) UnsupportedFlagWarnings(backendName string, noConfirm, force bool) []string {
	if _, ok := r.backends[backendName]; !ok {
		return nil
	}
	var out []string
	if noConfirm && !r.YesSupported(backendName) {
		out = append(out, fmt.Sprintf("-y is not supported by %s — prompts will still appear", backendName))
	}
	if force && len(r.ForceFlags(backendName)) == 0 {
		out = append(out, fmt.Sprintf("-f is not supported by %s — it was ignored", backendName))
	}
	return out
}

// DryRunEmitted reports whether a backend emits a native simulation flag.
// Backends without one cannot show a package plan in dry-run mode.
func (r *Registry) DryRunEmitted(backendName string) bool {
	return r.GetDryRunFlag(backendName) != ""
}

// Flags holds all parsed alps meta-flags from user args.
type Flags struct {
	DryRun    bool
	NoConfirm bool
	Verbose   bool
	Quiet     bool
	Force     bool
}

// ParseFlags splits raw args into package names and bare flags. Recognized
// alps flags: / -n simulate, no changes written, / -y skip confirmation
// prompts; --verbose, --quiet and --force are consumed by ParseFlagsExt.
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

// BuildExtraFlags assembles the extra flags slice to append to a backend
// command based on the resolved Flags state.
func (r *Registry) BuildExtraFlags(backendName string, dryRun, noConfirm bool) []string {
	return r.BuildExtraFlagsExt(backendName, Flags{DryRun: dryRun, NoConfirm: noConfirm})
}

// BuildExtraFlagsExt is the full version of BuildExtraFlags that accepts a
// Flags struct. Dry-run is print-only (the runner never executes), so no
// native dry-run flag is appended; callers that genuinely execute a
// simulation use GetDryRunFlag directly.
func (r *Registry) BuildExtraFlagsExt(backendName string, f Flags) []string {
	b, ok := r.backends[backendName]
	if !ok {
		return nil
	}
	var flags []string

	if f.NoConfirm && b.YesFlag != "" {
		flags = AppendUniq(flags, b.YesFlag)
	}
	if f.Verbose && b.VerboseFlag != "" {
		flags = AppendUniq(flags, b.VerboseFlag)
	}
	if f.Quiet && b.QuietFlag != "" {
		flags = AppendUniq(flags, b.QuietFlag)
	}
	if f.Force {
		for _, ff := range b.ForceFlag {
			flags = AppendUniq(flags, ff)
		}
	}
	return flags
}

// AppendUniq appends s to slice only if not already present (case-sensitive;
// command-line flags like -v and -V are distinct options).
func AppendUniq(flags []string, s string) []string {
	for _, existing := range flags {
		if existing == s {
			return flags
		}
	}
	return append(flags, s)
}
