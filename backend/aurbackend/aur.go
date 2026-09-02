package aurbackend

import (
	"errors"
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
		aur.PrintSearchResult(os.Stdout, i+1, p, "aur")
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

// Clone clones an AUR package repository to the current directory
func (b *Backend) Clone(pkgName string) error {
	if err := aur.Clone(pkgName); err != nil {
		ui.Msgf(b.cfg, ui.LevelError, "%v", err)
		return err
	}
	return nil
}

// Orphans lists AUR packages that have no reverse dependencies
func (b *Backend) Orphans() error {
	orphans, err := aur.FindAUROrphans()
	if err != nil {
		ui.Msgf(b.cfg, ui.LevelError, "%v", err)
		return err
	}

	if len(orphans) == 0 {
		ui.Msg(b.cfg, ui.LevelInfo, "No AUR orphan packages found.")
		return nil
	}

	ui.Msgf(b.cfg, ui.LevelInfo, "Found %d AUR orphan package(s):", len(orphans))
	for _, orphan := range orphans {
		fmt.Printf("  - %s\n", orphan)
	}
	return nil
}

// buildIgnoreSet reads the pacman config and returns a set of packages the user has marked to ignore.
func buildIgnoreSet() map[string]bool {
	pacConf, _ := aur.ReadPacmanConf()
	ignoreSet := make(map[string]bool)
	if pacConf != nil {
		for _, pkg := range pacConf.IgnorePkg {
			ignoreSet[pkg] = true
		}
	}
	return ignoreSet
}

// filterNonIgnored returns the names of installed AUR packages that are not in the ignore set,
// printing a skip message for each ignored one.
func filterNonIgnored(installed map[string]string, ignoreSet map[string]bool, symArrow string) []string {
	names := make([]string, 0, len(installed))
	for name := range installed {
		if ignoreSet[name] {
			fmt.Printf("  %s  %s: skipping ignored package\n", symArrow, name)
			continue
		}
		names = append(names, name)
	}
	return names
}

// isVCSPackage checks if a package is a VCS (version control system) package
// by delegating to the shared detection in the aur package.
func isVCSPackage(name string) bool {
	return aur.IsVCSPackage(name)
}

// findOutdated compares installed versions against the latest AUR info and returns packages with
// a newer version available, printing the version comparison for each.
//
// VCS development packages (-git, -svn, -hg, …) get special treatment: their
// versions track upstream commits, so static RPC comparison cannot detect
// updates. Instead, aur.CheckVCSUpdate probes the package's actual upstream
// remote and compares revisions. When that probe is impossible (no network,
// unsupported VCS, version without an embedded revision) we fall back to the
// legacy "may have updates" warning rather than reporting a false result.
func findOutdated(installed map[string]string, latest map[string]*aur.Package, ignoreSet map[string]bool, style config.Style) []aur.Package {
	var outdated []aur.Package
	for name, installedVer := range installed {
		if ignoreSet[name] {
			continue
		}
		pkg, ok := latest[name]
		if !ok {
			continue
		}
		// VCS packages have dynamic versions based on git commits.
		// Probe the upstream repository for a definitive answer instead of
		// comparing against the AUR RPC's frozen snapshot version.
		if isVCSPackage(name) {
			update, upstreamRev, err := aur.CheckVCSUpdate(pkg, installedVer)
			switch {
			case err == nil && update:
				outdated = append(outdated, *pkg)
				fmt.Printf("  %s%s%s  %s%s%s → %srebuild%s (upstream %s)\n",
					style.ColorPrimary, pkg.Name, style.ColorReset,
					style.ColorDim, installedVer, style.ColorReset,
					style.ColorSuccess, style.ColorReset, upstreamRev)
				continue
			case err == nil:
				// Installed build already matches the upstream tip.
				fmt.Printf("  %s%s%s  %s%s%s (up to date with upstream %s)\n",
					style.ColorDim, pkg.Name, style.ColorReset,
					style.ColorDim, installedVer, style.ColorReset, upstreamRev)
				continue
			case errors.Is(err, aur.ErrVCSPinned):
				// Source anchored to a fixed tag/commit — its published
				// version is stable, so fall through to normal comparison.
			case errors.Is(err, aur.ErrUnknownVCSVersion):
				fmt.Printf("  %s%s%s  %s%s%s (VCS package — version carries no revision; may have updates)\n",
					style.ColorDim, pkg.Name, style.ColorReset,
					style.ColorDim, installedVer, style.ColorReset)
				continue
			default:
				fmt.Printf("  %s%s%s  %s%s%s (VCS update check failed: %v — may have updates)\n",
					style.ColorDim, pkg.Name, style.ColorReset,
					style.ColorDim, installedVer, style.ColorReset, err)
				continue
			}
		}
		// Use Vercmp for proper Arch version comparison.
		// Vercmp returns 1 when AUR version is newer, 0 when equal, -1 when installed is newer.
		if aur.Vercmp(pkg.Version, installedVer) == 1 {
			outdated = append(outdated, *pkg)
			fmt.Printf("  %s%s%s  %s%s%s → %s%s%s\n",
				style.ColorPrimary, pkg.Name, style.ColorReset,
				style.ColorDim, installedVer, style.ColorReset,
				style.ColorSuccess, pkg.Version, style.ColorReset)
		}
	}
	return outdated
}

// upgradeAll upgrades all outdated AUR packages.
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

	ignoreSet := buildIgnoreSet()
	names := filterNonIgnored(installed, ignoreSet, b.cfg.Style.SymArrow)

	latest, err := aur.InfoBatch(names)
	if err != nil {
		ui.Msgf(b.cfg, ui.LevelError, "failed to check for AUR updates: %v", err)
		return err
	}

	outdated := findOutdated(installed, latest, ignoreSet, b.cfg.Style)

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
	}
	ui.Msg(b.cfg, ui.LevelOK, "AUR upgrade complete.")
	return nil
}
