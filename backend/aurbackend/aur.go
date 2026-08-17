package aurbackend

import (
	"fmt"
	"os"
	"strings"

	"github.com/adrianpriza-ai/alps/aur"
	"github.com/adrianpriza-ai/alps/config"
	"github.com/adrianpriza-ai/alps/ui"
)

// Backend implements the AUR-specific backend logic
type Backend struct {
	cfg *config.Config
}

// New creates a new AUR backend
func New(cfg *config.Config) *Backend {
	return &Backend{
		cfg: cfg,
	}
}

// Install installs AUR packages
func (b *Backend) Install(pkgs []string, dryRun bool) error {
	if len(pkgs) == 0 {
		ui.Msg(b.cfg, ui.LevelError, "Usage: alps aur install <package> [packages...]")
		return fmt.Errorf("package name required")
	}

	if dryRun {
		ui.Msgf(b.cfg, ui.LevelWarn, "DRY-RUN: would install AUR package(s): %s", strings.Join(pkgs, " "))
		return nil
	}

	if err := aur.Install(pkgs, false); err != nil {
		ui.Msgf(b.cfg, ui.LevelError, "%v", err)
		return err
	}
	ui.Msg(b.cfg, ui.LevelOK, "Done.")
	return nil
}

// Remove removes AUR packages
func (b *Backend) Remove(pkgs []string, dryRun bool) error {
	if len(pkgs) == 0 {
		ui.Msg(b.cfg, ui.LevelError, "Usage: alps aur remove <package> [packages...]")
		return fmt.Errorf("package name required")
	}

	if dryRun {
		ui.Msgf(b.cfg, ui.LevelWarn, "DRY-RUN: would remove AUR package(s): %s", strings.Join(pkgs, " "))
		return nil
	}

	ui.Msgf(b.cfg, ui.LevelWarn, "Remove AUR package(s) %s%s%s?",
		b.cfg.Style.ColorBold, strings.Join(pkgs, " "), b.cfg.Style.ColorReset+b.cfg.Style.ColorWarning)
	fmt.Print(b.cfg.Style.ColorReset)
	fmt.Println()
	if !ui.Confirm() {
		ui.Msg(b.cfg, ui.LevelWarn, "Cancelled.")
		return nil
	}

	var hasErrors bool
	for _, pkgName := range pkgs {
		if err := aur.Remove(pkgName, false); err != nil {
			ui.Msgf(b.cfg, ui.LevelError, "failed to remove %s: %v", pkgName, err)
			hasErrors = true
		} else {
			ui.Msg(b.cfg, ui.LevelOK, pkgName+" removed.")
		}
	}
	if hasErrors {
		return fmt.Errorf("some packages failed to remove")
	}
	return nil
}

// Search searches AUR packages
func (b *Backend) Search(query string) error {
	if query == "" {
		ui.Msg(b.cfg, ui.LevelError, "Usage: alps aur search <query>")
		return fmt.Errorf("search query required")
	}

	ui.Msgf(b.cfg, ui.LevelInfo, "Searching '%s' in AUR...", query)
	fmt.Println()
	results, err := aur.SearchNarrow(query)
	if err != nil {
		ui.Msgf(b.cfg, ui.LevelError, "%v", err)
		return err
	}
	if len(results) == 0 {
		ui.Msg(b.cfg, ui.LevelWarn, "No results found in AUR")
		return nil
	}
	for i, p := range results {
		aur.PrintSearchResult(i+1, p, "aur")
	}
	fmt.Println()
	return nil
}

// List lists installed AUR packages
func (b *Backend) List() error {
	installed, err := aur.ListInstalledAUR()
	if err != nil {
		ui.Msgf(b.cfg, ui.LevelError, "%v", err)
		return err
	}
	if len(installed) == 0 {
		ui.Msg(b.cfg, ui.LevelInfo, "No AUR packages installed.")
		return nil
	}
	fmt.Println()
	for name, ver := range installed {
		fmt.Printf("  %s%s%s  %s%s%s\n",
			b.cfg.Style.ColorPrimary, name, b.cfg.Style.ColorReset,
			b.cfg.Style.ColorDim, ver, b.cfg.Style.ColorReset)
	}
	fmt.Println()
	return nil
}

// Upgrade upgrades AUR packages
func (b *Backend) Upgrade(pkgs []string, dryRun bool) error {
	if len(pkgs) == 0 {
		// Upgrade all outdated AUR packages
		return b.upgradeAll(dryRun)
	}

	// Upgrade specific packages
	for _, pkg := range pkgs {
		if dryRun {
			ui.Msgf(b.cfg, ui.LevelWarn, "DRY-RUN: would upgrade AUR package %s", pkg)
		} else {
			if err := aur.Install([]string{pkg}, false); err != nil {
				ui.Msgf(b.cfg, ui.LevelError, "failed to upgrade %s: %v", pkg, err)
				return err
			} else {
				ui.Msg(b.cfg, ui.LevelOK, pkg+" upgraded.")
			}
		}
	}
	return nil
}

// BuildLocal builds a local PKGBUILD
func (b *Backend) BuildLocal(dir string, dryRun bool) error {
	if dir == "" {
		dir = "."
	}
	if dryRun {
		ui.Msgf(b.cfg, ui.LevelWarn, "DRY-RUN: would build PKGBUILD in %s", dir)
		return nil
	}
	ui.Msgf(b.cfg, ui.LevelInfo, "Building local PKGBUILD in %s%s%s...",
		b.cfg.Style.ColorBold, dir, b.cfg.Style.ColorReset+b.cfg.Style.ColorInfo)
	fmt.Print(b.cfg.Style.ColorReset)
	fmt.Println()
	if err := aur.BuildLocal(dir, false); err != nil {
		ui.Msgf(b.cfg, ui.LevelError, "%v", err)
		return err
	}
	ui.Msg(b.cfg, ui.LevelOK, "Done.")
	return nil
}

// FetchABS fetches PKGBUILD from ABS
func (b *Backend) FetchABS(pkgName string) error {
	if pkgName == "" {
		ui.Msg(b.cfg, ui.LevelError, "Usage: alps aur fetch-abs <package>")
		return fmt.Errorf("package name required")
	}
	ui.Msgf(b.cfg, ui.LevelInfo, "Fetching PKGBUILD for %s%s%s from ABS...",
		b.cfg.Style.ColorBold, pkgName, b.cfg.Style.ColorReset+b.cfg.Style.ColorInfo)
	fmt.Print(b.cfg.Style.ColorReset)
	fmt.Println()
	dir, err := aur.FetchABS(pkgName)
	if err != nil {
		ui.Msgf(b.cfg, ui.LevelError, "%v", err)
		return err
	}
	ui.Msgf(b.cfg, ui.LevelOK, "PKGBUILD saved to: %s", dir)
	return nil
}

// Clean removes AUR build cache
func (b *Backend) Clean(dryRun bool) error {
	cacheRoot, err := aur.AURCacheRoot()
	if err != nil {
		ui.Msgf(b.cfg, ui.LevelError, "%v", err)
		return err
	}
	if _, err := os.Stat(cacheRoot); os.IsNotExist(err) {
		ui.Msg(b.cfg, ui.LevelInfo, "No AUR cache found.")
		return nil
	}
	if dryRun {
		ui.Msgf(b.cfg, ui.LevelWarn, "DRY-RUN: would remove AUR build cache at %s", cacheRoot)
		return nil
	}
	ui.Msgf(b.cfg, ui.LevelInfo, "Remove AUR build cache? (%s)", cacheRoot)
	if !ui.Confirm() {
		ui.Msg(b.cfg, ui.LevelWarn, "Cancelled.")
		return nil
	}
	if err := aur.CleanCache(""); err != nil {
		ui.Msgf(b.cfg, ui.LevelError, "%v", err)
		return err
	}
	ui.Msg(b.cfg, ui.LevelOK, "Cache removed.")
	return nil
}

// upgradeAll upgrades all outdated AUR packages
func (b *Backend) upgradeAll(dryRun bool) error {
	installed, err := aur.GetInstalledAUR()
	if err != nil {
		ui.Msgf(b.cfg, ui.LevelError, "failed to list AUR packages: %v", err)
		return err
	}
	if len(installed) == 0 {
		ui.Msg(b.cfg, ui.LevelInfo, "No AUR packages installed.")
		return nil
	}

	ui.Msgf(b.cfg, ui.LevelInfo, "Checking %d AUR package(s) for updates...", len(installed))

	pacConf, _ := aur.ReadPacmanConf()
	ignoreSet := make(map[string]bool)
	if pacConf != nil {
		for _, pkg := range pacConf.IgnorePkg {
			ignoreSet[pkg] = true
		}
	}

	names := make([]string, 0, len(installed))
	for name := range installed {
		if ignoreSet[name] {
			fmt.Printf("  %s  %s: skipping ignored package\n",
				b.cfg.Style.SymArrow, name)
			continue
		}
		names = append(names, name)
	}
	latest, err := aur.InfoBatch(names)
	if err != nil {
		ui.Msgf(b.cfg, ui.LevelError, "failed to check for AUR updates: %v", err)
		return err
	}

	var outdated []aur.Package
	for name, installedVer := range installed {
		if ignoreSet[name] {
			continue
		}
		pkg, ok := latest[name]
		if !ok {
			continue
		}
		if pkg.Version != installedVer {
			outdated = append(outdated, *pkg)
			fmt.Printf("  %s%s%s  %s%s%s → %s%s%s\n",
				b.cfg.Style.ColorPrimary, pkg.Name, b.cfg.Style.ColorReset,
				b.cfg.Style.ColorDim, installedVer, b.cfg.Style.ColorReset,
				b.cfg.Style.ColorSuccess, pkg.Version, b.cfg.Style.ColorReset)
		}
	}

	if len(outdated) == 0 {
		ui.Msg(b.cfg, ui.LevelOK, "All AUR packages are up to date.")
		return nil
	}

	fmt.Println()
	ui.Msgf(b.cfg, ui.LevelInfo, "%d AUR package(s) to upgrade.", len(outdated))
	fmt.Println()

	var toUpgrade []string
	for _, pkg := range outdated {
		if dryRun {
			ui.Msgf(b.cfg, ui.LevelWarn, "DRY-RUN: would upgrade AUR package %s", pkg.Name)
		} else {
			toUpgrade = append(toUpgrade, pkg.Name)
		}
	}

	if dryRun || len(toUpgrade) == 0 {
		return nil
	}

	if err := aur.Install(toUpgrade, false); err != nil {
		ui.Msgf(b.cfg, ui.LevelError, "AUR upgrade failed: %v", err)
		return err
	} else {
		ui.Msg(b.cfg, ui.LevelOK, "AUR upgrade complete.")
	}
	return nil
}
