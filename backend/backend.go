package backend

import (
	"github.com/adrianpriza-ai/alps/config"
	"github.com/adrianpriza-ai/alps/pack"
)

// Backend represents a package manager backend interface
type Backend interface {
	// Name returns the backend name
	Name() string
	// Detect checks if this backend is available on the system
	Detect() bool
	// Install installs packages
	Install(pkgs []string, dryRun bool, cfg *config.Config) error
	// Remove removes packages
	Remove(pkgs []string, dryRun bool, cfg *config.Config) error
	// Purge purges packages and their config files
	Purge(pkgs []string, dryRun bool, cfg *config.Config) error
	// Search searches for packages
	Search(query string, cfg *config.Config) error
	// Show shows package information
	Show(pkg string, cfg *config.Config) error
	// List lists installed packages
	List(cfg *config.Config) error
	// Update updates package lists
	Update(dryRun bool, cfg *config.Config) error
	// Upgrade upgrades packages
	Upgrade(dryRun bool, cfg *config.Config) error
	// Autoremove removes unused dependencies
	Autoremove(dryRun bool, cfg *config.Config) error
	// Clean cleans package cache
	Clean(dryRun bool, cfg *config.Config) error
	// CommandSupported checks if a command is supported by this backend
	CommandSupported(cmd string) bool
	// NeedsSudo returns true if this backend requires sudo
	NeedsSudo() bool
}

// BaseBackend provides common functionality for all backends
type BaseBackend struct {
	binName string
	name    string
}

func NewBaseBackend(binName, name string) *BaseBackend {
	return &BaseBackend{
		binName: binName,
		name:    name,
	}
}

func (b *BaseBackend) Name() string {
	return b.name
}

func (b *BaseBackend) Detect() bool {
	return pack.DetectName() == b.name
}

func (b *BaseBackend) CommandSupported(cmd string) bool {
	return pack.CommandSupported(b.name, cmd)
}

func (b *BaseBackend) NeedsSudo() bool {
	return pack.NeedsSudo(b.name)
}
