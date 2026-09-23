package cli

import (
	"fmt"
	"sort"
	"strings"

	"github.com/adrianpriza-ai/alps/config"
)

var hardCommands = map[string]bool{
	"help": true, "--help": true, "-h": true,
	"version": true, "--version": true,
	"aliases": true, "config-show": true, "completion": true,
	"repo": true, "aur": true, "winget": true, "flatpak": true, "snap": true,
	"install": true, "remove": true, "purge": true,
	"update": true, "upgrade": true, "full-upgrade": true,
	"search": true, "show": true, "list": true,
	"autoremove": true, "autoclean": true, "clean": true,
	"edit-sources": true,
}

// commandDescriptions maps a command name to its help description. It is
// filled from commandHelp below.
var commandDescriptions = map[string]string{}

// commandUsage maps a command name to its help-display label, including the
// argument placeholder (e.g. "install <pkg>"). It is filled from commandHelp.
var commandUsage = map[string]string{}

// commandHelp lists every typeable top-level command as name, argument
// placeholder, description, in help-display order. Flags (--help, -h,
// --version) are deliberately absent: they are covered by the Flags section.
var commandHelp = [][3]string{
	{"install", " <pkg>", "install a package"},
	{"remove", " <pkg>", "remove a package"},
	{"purge", " <pkg>", "remove a package and its data"},
	{"search", " <query>", "search packages"},
	{"show", " <pkg>", "show package info"},
	{"list", "", "list installed packages"},
	{"update", "", "refresh package indexes"},
	{"upgrade", "", "upgrade installed packages"},
	{"full-upgrade", "", "sync repos and upgrade all"},
	{"autoremove", "", "remove orphaned packages"},
	{"autoclean", "", "clean partial downloaded packages"},
	{"clean", "", "remove cached packages"},
	{"edit-sources", "", "edit repository sources"},
	{"completion", " <shell>", "generate shell completion"},
	{"help", "", "show this help"},
	{"aliases", "", "show active aliases"},
	{"config-show", "", "show config & paths"},
	{"version", "", "binary version"},
}

func init() {
	for _, row := range commandHelp {
		commandDescriptions[row[0]] = row[2]
		commandUsage[row[0]] = row[0] + row[1]
	}
}

// Commands returns the top-level commands a user can type, sorted. Flag
// spellings (--help, -h, --version) are excluded.
func Commands() []string {
	names := make([]string, 0, len(hardCommands))
	for cmd := range hardCommands {
		if strings.HasPrefix(cmd, "-") {
			continue
		}
		names = append(names, cmd)
	}
	sort.Strings(names)
	return names
}

// SubCmds returns the valid subcommands for a subsystem, sorted, or nil when
// the subsystem is unknown.
func SubCmds(system string) []string {
	return ValidSubCmds(system)
}

// CommandDesc returns the help description for a command, falling back to
// the command name itself when none is registered.
func CommandDesc(cmd string) string {
	if d, ok := commandDescriptions[cmd]; ok {
		return d
	}
	return cmd
}

// CommandUsage returns the help-display label for a command, including its
// argument placeholder (e.g. "install <pkg>"), or the bare name when the
// command is unknown.
func CommandUsage(cmd string) string {
	if u, ok := commandUsage[cmd]; ok {
		return u
	}
	return cmd
}

// CoreHelpRows returns the top-level commands paired with their help
// descriptions, in display order. It backs the Core section of ui.PrintHelp.
func CoreHelpRows() [][2]string {
	rows := make([][2]string, 0, len(commandHelp))
	for _, row := range commandHelp {
		rows = append(rows, [2]string{CommandUsage(row[0]), row[2]})
	}
	return rows
}

// subCmdHelp lists every subcommand as name, argument placeholder,
// description, keyed by subsystem in help-display order. A valid subcommand
// missing here falls back to its own name when rendered.
var subCmdHelp = map[string][][3]string{
	"aur": {
		{"install", " <pkg>", "install directly from AUR"},
		{"search", " <query>", "search AUR only"},
		{"list", "", "list installed AUR packages"},
		{"update", " [pkg]", "upgrade outdated AUR packages"},
		{"upgrade", " [pkg]", "rebuild & upgrade AUR package(s)"},
		{"remove", " <pkg>", "remove via pacman -R"},
		{"clean", "", "remove build cache"},
		{"build-local", " [dir]", "build a local PKGBUILD"},
		{"fetch-abs", " <pkg>", "fetch official PKGBUILD"},
		{"info", " <pkg>", "show AUR package metadata"},
		{"clone", " <pkg>", "clone AUR PKGBUILD for inspection"},
		{"orphans", "", "list AUR orphan packages"},
	},
	"repo": {
		{"update", "", "refresh repo cache"},
		{"list", "", "list available packages"},
		{"install", " <pkg|url>", "install pkg or from a URL"},
		{"remove", " <pkg>", "remove a repo package"},
		{"purge", " <pkg>", "remove a repo package and its data"},
		{"search", " <query>", "search repo packages"},
		{"upgrade", " [pkg]", "upgrade installed package(s)"},
		{"clean", "", "remove build cache"},
	},
	"winget": {
		{"install", " <pkg>", "install via winget"},
		{"remove", " <pkg>", "remove via winget"},
		{"purge", " <pkg>", "remove a winget package and its data"},
		{"search", " <query>", "search winget packages"},
		{"show", " <pkg>", "show winget package info"},
		{"list", "", "list installed winget packages"}, {"update", "", "list upgradable winget packages"},
		{"upgrade", "", "upgrade all winget packages"},
	},
	"flatpak": {
		{"install", " <pkg>", "install from flathub"},
		{"remove", " <pkg>", "remove flatpak"},
		{"purge", " <pkg>", "remove a flatpak and its data"},
		{"search", " <query>", "search flathub"},
		{"show", " <pkg>", "show flatpak info"},
		{"list", "", "list installed flatpaks"}, {"update", "", "update all flatpaks"},
		{"upgrade", "", "upgrade flatpak(s)"},
		{"autoremove", "", "remove unused flatpak runtimes"},
		{"clean", "", "remove unused flatpak runtimes"},
	},
	"snap": {
		{"install", " <pkg>", "install via snap"},
		{"remove", " <pkg>", "remove snap package"},
		{"purge", " <pkg>", "remove a snap and its data"},
		{"search", " <query>", "search snap store"},
		{"show", " <pkg>", "show snap info"},
		{"list", "", "list installed snaps"}, {"update", "", "refresh all snaps"},
		{"upgrade", "", "upgrade snap(s)"},
	},
}

// SubCmdHelp returns the valid subcommands of a system paired with their
// help descriptions, in a stable order suitable for direct rendering. It
// returns nil when the subsystem is unknown.
func SubCmdHelp(system string) [][2]string {
	valid, ok := validSubCmds[system]
	if !ok {
		return nil
	}
	rows := make([][2]string, 0, len(valid))
	seen := map[string]bool{}
	for _, row := range subCmdHelp[system] {
		if valid[row[0]] && !seen[row[0]] {
			rows = append(rows, [2]string{system + " " + row[0] + row[1], row[2]})
			seen[row[0]] = true
		}
	}
	// Render any subcommand that is valid but has no curated description.
	for _, name := range ValidSubCmds(system) {
		if !seen[name] {
			rows = append(rows, [2]string{system + " " + name, name})
		}
	}
	return rows
}

var validSubCmds = map[string]map[string]bool{
	"aur": {
		"install": true, "search": true, "list": true,
		"remove": true, "clean": true, "build-local": true, "fetch-abs": true,
		"update": true, "upgrade": true, "info": true, "clone": true, "orphans": true,
	},
	"repo": {
		"update": true, "list": true, "install": true,
		"remove": true, "purge": true, "search": true, "upgrade": true, "clean": true,
	},
	"winget": {
		"install": true, "remove": true, "purge": true, "search": true,
		"show": true, "list": true, "update": true, "upgrade": true,
	},
	"flatpak": {
		"install": true, "remove": true, "purge": true, "search": true,
		"show": true, "list": true, "update": true, "upgrade": true,
		"autoremove": true, "clean": true,
	},
	"snap": {
		"install": true, "remove": true, "purge": true, "search": true,
		"show": true, "list": true, "update": true, "upgrade": true,
	},
}

// ResolveCmd resolves a command using the 3-tier alias chain (hard commands -> config aliases -> default aliases).
// The command word is matched case-insensitively (`alps INSTALL` works); the
// returned command is always the canonical lower-case spelling. Subcommand
// and alias spellings keep their case, so user-defined aliases remain usable.
func ResolveCmd(cmd string, cfg *config.Config) (string, error) {
	lower := strings.ToLower(cmd)
	if hardCommands[lower] {
		return lower, nil
	}
	if v, ok := cfg.ConfigAliases[cmd]; ok {
		return v, nil
	}
	if v, ok := config.DefaultAliases[cmd]; ok {
		return v, nil
	}
	return "", fmt.Errorf("unknown command %q — run 'alps help' for available commands", cmd)
}

// ResolveSubCmd resolves a subcommand for a specific system using the 3-tier alias chain
func ResolveSubCmd(system, subcmd string, cfg *config.Config) (string, error) {
	valid := validSubCmds[system]

	if valid[subcmd] {
		return subcmd, nil
	}
	if v, ok := cfg.ConfigAliases[subcmd]; ok {
		if valid[v] {
			return v, nil
		}
	}
	if v, ok := config.DefaultAliases[subcmd]; ok {
		if valid[v] {
			return v, nil
		}
	}
	if v, ok := config.DefaultSubCmdAliases[subcmd]; ok {
		if valid[v] {
			return v, nil
		}
	}

	names := make([]string, 0, len(valid))
	for k := range valid {
		names = append(names, k)
	}
	sort.Strings(names)
	return "", fmt.Errorf("unknown %s subcommand %q\n  valid: %s", system, subcmd, strings.Join(names, ", "))
}

// ResolveListAction resolves repo list sub-actions using 3-tier alias chain.
func ResolveListAction(action string, cfg *config.Config) string {
	// Tier 1: exact match
	if action == "install" || action == "remove" {
		return action
	}
	// Tier 2: config alias
	if v, ok := cfg.ConfigAliases[action]; ok {
		if v == "install" || v == "remove" {
			return v
		}
	}
	// Tier 3: default subcmd aliases (for add/del, etc)
	if v, ok := config.DefaultSubCmdAliases[action]; ok {
		if v == "install" || v == "remove" {
			return v
		}
	}
	// Tier 4: default main aliases
	if v, ok := config.DefaultAliases[action]; ok {
		if v == "install" || v == "remove" {
			return v
		}
	}
	return ""
}

// IsHardCommand checks if a command is a hard command (no alias resolution needed)
func IsHardCommand(cmd string) bool {
	return hardCommands[cmd]
}

// IsValidSubCmd checks if a subcommand is valid for a given system
func IsValidSubCmd(system, subcmd string) bool {
	valid, ok := validSubCmds[system]
	if !ok {
		return false
	}
	return valid[subcmd]
}

// ValidSubCmds returns the valid subcommands for a system, sorted.
func ValidSubCmds(system string) []string {
	valid, ok := validSubCmds[system]
	if !ok {
		return nil
	}
	names := make([]string, 0, len(valid))
	for k := range valid {
		names = append(names, k)
	}
	sort.Strings(names)
	return names
}
