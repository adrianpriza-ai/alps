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

var validSubCmds = map[string]map[string]bool{
	"aur": {
		"install": true, "search": true, "list": true,
		"remove": true, "clean": true, "build-local": true, "fetch-abs": true,
		"update": true, "upgrade": true,
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
		"autoremove": true, "clean": true,
	},
}

// ResolveCmd resolves a command using the 3-tier alias chain (hard commands -> config aliases -> default aliases)
func ResolveCmd(cmd string, cfg *config.Config) (string, error) {
	if hardCommands[cmd] {
		return cmd, nil
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
