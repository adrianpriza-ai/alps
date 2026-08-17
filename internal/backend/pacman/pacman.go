package pacman

import (
	"fmt"
	"strings"

	"github.com/adrianpriza-ai/alps/config"
	"github.com/adrianpriza-ai/alps/internal/backend"
	"github.com/adrianpriza-ai/alps/ui"
)

// Backend implements the backend.Backend interface for pacman
type Backend struct {
	*backend.BaseBackend
}

// New creates a new pacman backend
func New() *Backend {
	return &Backend{
		BaseBackend: backend.NewBaseBackend("pacman", "pacman"),
	}
}

func (b *Backend) Install(pkgs []string, dryRun bool, cfg *config.Config) error {
	if len(pkgs) == 0 {
		return fmt.Errorf("package name required")
	}

	if dryRun {
		ui.Msgf(cfg, ui.LevelWarn, "DRY-RUN: would install %s package(s): %s", b.Name(), strings.Join(pkgs, " "))
		return nil
	}

	return fmt.Errorf("not yet implemented - needs runner integration")
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
