package zypper

import (
	"fmt"

	"github.com/adrianpriza-ai/alps/backend"
	"github.com/adrianpriza-ai/alps/config"
)

// Backend implements the backend.Backend interface for zypper
type Backend struct {
	*backend.BaseBackend
}

// New creates a new zypper backend
func New() *Backend {
	return &Backend{
		BaseBackend: backend.NewBaseBackend("zypper", "zypper"),
	}
}

func (b *Backend) Install(pkgs []string, dryRun bool, cfg *config.Config) error {
	return fmt.Errorf("not yet implemented")
}

func (b *Backend) Remove(pkgs []string, dryRun bool, cfg *config.Config) error {
	return fmt.Errorf("not yet implemented")
}

func (b *Backend) Purge(pkgs []string, dryRun bool, cfg *config.Config) error {
	return fmt.Errorf("not yet implemented")
}

func (b *Backend) Search(query string, cfg *config.Config) error {
	return fmt.Errorf("not yet implemented")
}

func (b *Backend) Show(pkg string, cfg *config.Config) error {
	return fmt.Errorf("not yet implemented")
}

func (b *Backend) List(cfg *config.Config) error {
	return fmt.Errorf("not yet implemented")
}

func (b *Backend) Update(dryRun bool, cfg *config.Config) error {
	return fmt.Errorf("not yet implemented")
}

func (b *Backend) Upgrade(dryRun bool, cfg *config.Config) error {
	return fmt.Errorf("not yet implemented")
}

func (b *Backend) Autoremove(dryRun bool, cfg *config.Config) error {
	return fmt.Errorf("not yet implemented")
}

func (b *Backend) Clean(dryRun bool, cfg *config.Config) error {
	return fmt.Errorf("not yet implemented")
}
