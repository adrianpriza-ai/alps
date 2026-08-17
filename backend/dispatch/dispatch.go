package dispatch

import (
	"github.com/adrianpriza-ai/alps/cli"
	"github.com/adrianpriza-ai/alps/config"
)

// Backend represents a package manager backend
type Backend string

const (
	BackendRepo    Backend = "repo"
	BackendAUR     Backend = "aur"
	BackendExtra   Backend = "extra"
	BackendWinget  Backend = "winget"
	BackendFlatpak Backend = "flatpak"
	BackendSnap    Backend = "snap"
	BackendPkg     Backend = "pkg" // Native package manager
)

// Command represents a resolved command with its backend
type Command struct {
	Backend Backend
	SubCmd  string
	Args    []string
	Cfg     *config.Config
}

// ResolveCommand resolves a command to its backend and subcommand
func ResolveCommand(cmd string, args []string, cfg *config.Config) (*Command, error) {
	resolved, err := cli.ResolveCmd(cmd, cfg)
	if err != nil {
		return nil, err
	}

	backend := map[string]Backend{
		"repo":    BackendRepo,
		"aur":     BackendAUR,
		"extra":   BackendExtra,
		"winget":  BackendWinget,
		"flatpak": BackendFlatpak,
		"snap":    BackendSnap,
	}[resolved]

	if backend == "" {
		// Default to native package manager
		backend = BackendPkg
	}

	return &Command{
		Backend: backend,
		SubCmd:  resolved,
		Args:    args,
		Cfg:     cfg,
	}, nil
}

// ResolveSubCommand resolves a subcommand for a specific backend
func ResolveSubCommand(backend Backend, rawSubcmd string, cfg *config.Config) (string, error) {
	system := string(backend)
	if backend == BackendPkg {
		// For native package manager, subcommands are validated elsewhere
		return rawSubcmd, nil
	}
	return cli.ResolveSubCmd(system, rawSubcmd, cfg)
}
