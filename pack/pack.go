// Package pack provides a unified interface for native package managers.
package pack

import "os/exec"

// Backend describes a native package manager.
type Backend struct {
	Name   string              // canonical short name (apt, pacman, zypper)
	Bin    string              // actual binary to invoke
	Sudo   bool                // requires privilege escalation
	CmdMap map[string][]string // maps alps verbs to backend commands
}

var registry = map[string]*Backend{} // backend name to Backend
var detectionOrder = []string{"apt", "apt-get", "dnf", "pacman", "zypper", "apk"}

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
		Name: "apt",
		Bin:  "apt",
		Sudo: true,
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
		Name: "apt-get",
		Bin:  "apt-get",
		Sudo: true,
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
		Name: "dnf",
		Bin:  "dnf",
		Sudo: true,
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
			"clean":        {"dnf", "clean", "all"},
		},
	})

	// pacman
	Register(Backend{
		Name: "pacman",
		Bin:  "pacman",
		Sudo: true,
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
		Name: "zypper",
		Bin:  "zypper",
		Sudo: true,
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
			"clean":        {"zypper", "clean", "--all"},
		},
	})

	// apk
	Register(Backend{
		Name: "apk",
		Bin:  "apk",
		Sudo: true,
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
			"clean":        {"apk", "cache", "clean"},
		},
	})
}
