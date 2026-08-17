package brew

import (
	"fmt"

	"github.com/adrianpriza-ai/alps/config"
	"github.com/adrianpriza-ai/alps/internal/backend"
)

// Backend implements the backend.Backend interface for brew
type Backend struct {
	*backend.BaseBackend
}

// New creates a new brew backend
func New() *Backend {
	return &Backend{
		BaseBackend: backend.NewBaseBackend("brew", "brew"),
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
