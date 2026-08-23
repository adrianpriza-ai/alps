package repo

import (
	"fmt"
	"os"
	"strings"

	"github.com/adrianpriza-ai/alps/cli"
	"github.com/adrianpriza-ai/alps/config"
	"github.com/adrianpriza-ai/alps/more"
	"github.com/adrianpriza-ai/alps/ui"
)

// Backend implements the repo-specific backend logic
type Backend struct {
	cfg *config.Config
}

// New creates a new repo backend
func New(cfg *config.Config) *Backend {
	return &Backend{
		cfg: cfg,
	}
}

// Update fetches and caches repo updates
func (b *Backend) Update(dryRun bool) error {
	if dryRun {
		ui.Msgf(b.cfg, ui.LevelWarn, "DRY-RUN: would fetch and cache repo updates")
		return nil
	}
	if err := more.FetchAndCache(b.cfg); err != nil {
		ui.Msg(b.cfg, ui.LevelError, err.Error())
		return err
	}
	ui.Msg(b.cfg, ui.LevelOK, "repo cache updated")

	summary, err := more.CheckUpdates(b.cfg)
	if err != nil {
		ui.Msg(b.cfg, ui.LevelWarn, fmt.Sprintf("could not check for updates: %v", err))
		return nil
	}
	if summary == nil {
		return nil
	}
	if len(summary.Upgradeable) == 0 && len(summary.Stale) == 0 {
		ui.Msg(b.cfg, ui.LevelOK, "all installed packages are up to date")
		return nil
	}
	if len(summary.Upgradeable) > 0 {
		ui.Msgf(b.cfg, ui.LevelInfo,
			"%d package(s) have updates available — run 'alps repo upgrade' to apply",
			len(summary.Upgradeable))
		for _, pkg := range summary.Upgradeable {
			fmt.Printf("       %s\n", pkg)
		}
	}
	if len(summary.Stale) > 0 {
		ui.Msgf(b.cfg, ui.LevelWarn,
			"%d package(s) no longer in repo — run 'alps repo remove <pkg>' to clean up",
			len(summary.Stale))
		for _, name := range summary.Stale {
			fmt.Printf("       %s\n", name)
		}
	}
	return nil
}

// List lists available packages
func (b *Backend) List(args []string) error {
	// Sub-actions: install → ListInstalled, remove → ListStale
	if len(args) > 0 {
		action := cli.ResolveListAction(args[0], b.cfg)
		switch action {
		case "install":
			fmt.Println()
			if err := more.ListInstalled(b.cfg); err != nil {
				ui.Msgf(b.cfg, ui.LevelError, "%v", err)
				return err
			}
			fmt.Println()
			return nil
		case "remove":
			fmt.Println()
			if err := more.ListStale(b.cfg); err != nil {
				ui.Msgf(b.cfg, ui.LevelError, "%v", err)
				return err
			}
			fmt.Println()
			return nil
		}
		// Fall through to full list
	}

	entries, err := more.List(b.cfg)
	if err != nil {
		ui.Msgf(b.cfg, ui.LevelError, "%v", err)
		return err
	}
	if len(entries) == 0 {
		ui.Msg(b.cfg, ui.LevelWarn, "No packages in repo.")
		return nil
	}
	installed, _ := more.ReadInstalled()
	fmt.Println()
	for _, e := range entries {
		installedVer := ""
		if rec, ok := installed[e.Name]; ok {
			installedVer = rec.Version
			if installedVer == "" {
				installedVer = "installed"
			}
			if strings.HasPrefix(rec.Source, "github:") {
				installedVer += " [github]"
			}
		}
		ui.PrintRepoEntry(b.cfg, e.Name, e.Version, e.Desc, e.Arch, installedVer)
	}
	fmt.Println()
	return nil
}

// Install installs packages from the repo
func (b *Backend) Install(pkgs []string, dryRun bool) error {
	if len(pkgs) == 0 {
		ui.Msg(b.cfg, ui.LevelError, "Usage: alps repo install <package> [packages...]")
		return fmt.Errorf("package name required")
	}

	var hasErrors bool
	for _, pkgName := range pkgs {
		entry, remoteRef, err := b.fetchRepoEntry(pkgName)
		if err != nil {
			ui.Msgf(b.cfg, ui.LevelError, "%v", err)
			hasErrors = true
			continue
		}

		if err := more.Validate(entry); err != nil {
			ui.Msgf(b.cfg, ui.LevelError, "%v", err)
			hasErrors = true
			continue
		}

		sourceStr := "alps-more"
		if remoteRef != nil && entry.Source != "" {
			sourceStr = remoteRef.DisplayURL()
		}
		b.printRepoInstallPreview(entry, sourceStr)

		if dryRun {
			ui.Msgf(b.cfg, ui.LevelWarn, "DRY-RUN: would install %s", entry.Name)
			continue
		}
		if !ui.Confirm() {
			ui.Msg(b.cfg, ui.LevelWarn, "Cancelled for "+entry.Name)
			continue
		}

		fmt.Println()
		if err := more.Install(entry, b.cfg); err != nil {
			ui.Msgf(b.cfg, ui.LevelError, "failed to install %s: %v", entry.Name, err)
			hasErrors = true
		} else {
			ui.Msg(b.cfg, ui.LevelOK, entry.Name+" installed.")
		}
	}
	if hasErrors {
		return fmt.Errorf("some packages failed to install")
	}
	return nil
}

// Remove removes packages from the repo
func (b *Backend) Remove(pkgs []string, dryRun bool) error {
	if len(pkgs) == 0 {
		ui.Msg(b.cfg, ui.LevelError, "Usage: alps repo remove <package> [packages...]")
		return fmt.Errorf("package name required")
	}

	var hasErrors bool
	for _, pkgName := range pkgs {
		entry, stale, err := more.RemovalEntry(pkgName, b.cfg)
		if err != nil {
			ui.Msgf(b.cfg, ui.LevelError, "%v", err)
			hasErrors = true
			continue
		}

		// Validate package is installed before confirmation
		_, isInstalled := more.GetInstalled(pkgName)
		if !isInstalled {
			ui.Msgf(b.cfg, ui.LevelError, "package %q is not installed via alps-more", pkgName)
			hasErrors = true
			continue
		}

		ui.Msgf(b.cfg, ui.LevelInfo, "Remove %s%s%s from alps-more?",
			b.cfg.Style.ColorBold, entry.Name, b.cfg.Style.ColorReset+b.cfg.Style.ColorInfo)
		if stale {
			ui.Msg(b.cfg, ui.LevelWarn, "package is no longer in repo; using saved uninstall commands")
		}
		fmt.Println()
		for _, line := range entry.RemoveLines {
			fmt.Printf("  %s$ %s%s\n", b.cfg.Style.ColorDim, line, b.cfg.Style.ColorReset)
		}
		fmt.Print(b.cfg.Style.ColorReset)
		fmt.Println()
		if dryRun {
			ui.Msgf(b.cfg, ui.LevelWarn, "DRY-RUN: would remove %s", entry.Name)
			continue
		}
		if !ui.Confirm() {
			ui.Msg(b.cfg, ui.LevelWarn, "Cancelled for "+entry.Name)
			continue
		}

		fmt.Println()
		if err := more.Remove(entry, b.cfg); err != nil {
			ui.Msgf(b.cfg, ui.LevelError, "failed to remove %s: %v", entry.Name, err)
			hasErrors = true
		} else {
			ui.Msg(b.cfg, ui.LevelOK, entry.Name+" removed.")
		}
	}
	if hasErrors {
		return fmt.Errorf("some packages failed to remove")
	}
	return nil
}

// Purge purges packages and their config files
func (b *Backend) Purge(pkgs []string, dryRun bool) error {
	if len(pkgs) == 0 {
		ui.Msg(b.cfg, ui.LevelError, "Usage: alps repo purge <package> [packages...]")
		return fmt.Errorf("package name required")
	}

	var hasErrors bool
	for _, pkgName := range pkgs {
		entry, stale, err := more.RemovalEntry(pkgName, b.cfg)
		if err != nil {
			ui.Msgf(b.cfg, ui.LevelError, "%v", err)
			hasErrors = true
			continue
		}

		// Validate package is installed before confirmation
		_, isInstalled := more.GetInstalled(pkgName)
		if !isInstalled {
			ui.Msgf(b.cfg, ui.LevelError, "package %q is not installed via alps-more", pkgName)
			hasErrors = true
			continue
		}

		ui.Msgf(b.cfg, ui.LevelWarn, "Purge %s%s%s? This removes the package AND its config/data files.",
			b.cfg.Style.ColorBold, entry.Name, b.cfg.Style.ColorReset+b.cfg.Style.ColorWarning)
		if stale {
			ui.Msg(b.cfg, ui.LevelWarn, "package is no longer in repo; using saved uninstall commands")
		}
		fmt.Println()

		if len(entry.RemoveLines) > 0 {
			fmt.Printf("  %sremove:%s\n", b.cfg.Style.ColorBold, b.cfg.Style.ColorReset)
			for _, line := range entry.RemoveLines {
				fmt.Printf("  %s$ %s%s\n", b.cfg.Style.ColorDim, line, b.cfg.Style.ColorReset)
			}
			fmt.Println()
		}
		if len(entry.PurgeLines) > 0 {
			fmt.Printf("  %spurge:%s\n", b.cfg.Style.ColorBold, b.cfg.Style.ColorReset)
			for _, line := range entry.PurgeLines {
				fmt.Printf("  %s$ %s%s\n", b.cfg.Style.ColorDim, line, b.cfg.Style.ColorReset)
			}
		} else {
			fmt.Printf("  %s%s  no purge_cmd defined — only remove will run%s\n",
				b.cfg.Style.ColorDim, b.cfg.Style.SymWarn, b.cfg.Style.ColorReset)
		}

		fmt.Print(b.cfg.Style.ColorReset)
		fmt.Println()
		if dryRun {
			ui.Msgf(b.cfg, ui.LevelWarn, "DRY-RUN: would purge %s", entry.Name)
			continue
		}
		if !ui.Confirm() {
			ui.Msg(b.cfg, ui.LevelWarn, "Cancelled for "+entry.Name)
			continue
		}

		fmt.Println()
		if err := more.Purge(pkgName, b.cfg); err != nil {
			ui.Msgf(b.cfg, ui.LevelError, "failed to purge %s: %v", entry.Name, err)
			hasErrors = true
		} else {
			ui.Msg(b.cfg, ui.LevelOK, entry.Name+" purged.")
		}
	}
	if hasErrors {
		return fmt.Errorf("some packages failed to purge")
	}
	return nil
}

// Search searches for packages in the repo
func (b *Backend) Search(query string) error {
	if query == "" {
		ui.Msg(b.cfg, ui.LevelError, "Usage: alps repo search <query>")
		return fmt.Errorf("search query required")
	}
	results, err := more.Search(query, b.cfg)
	if err != nil {
		ui.Msgf(b.cfg, ui.LevelError, "%v", err)
		return err
	}
	if len(results) == 0 {
		ui.Msgf(b.cfg, ui.LevelWarn, "No results for '%s' in alps-more.", query)
		return nil
	}
	fmt.Println()
	for _, e := range results {
		ui.PrintRepoSearchResult(b.cfg, e.Name, e.Version, e.Desc)
	}
	fmt.Println()
	return nil
}

// Upgrade upgrades installed packages
func (b *Backend) Upgrade(pkgs []string) error {
	if len(pkgs) == 0 {
		ui.Msg(b.cfg, ui.LevelInfo, "Checking alps-more packages for updates...")
		fmt.Println()
		if err := more.UpgradeAll(b.cfg); err != nil {
			ui.Msgf(b.cfg, ui.LevelError, "%v", err)
			return err
		}
	} else {
		var hasErrors bool
		for _, pkgName := range pkgs {
			if err := more.Upgrade(pkgName, b.cfg); err != nil {
				ui.Msgf(b.cfg, ui.LevelError, "failed to upgrade %s: %v", pkgName, err)
				hasErrors = true
			} else {
				ui.Msg(b.cfg, ui.LevelOK, pkgName+" upgraded.")
			}
		}
		if hasErrors {
			return fmt.Errorf("some packages failed to upgrade")
		}
	}
	return nil
}

// Clean removes the repo cache
func (b *Backend) Clean(dryRun bool) error {
	cacheDir := more.CacheDir()
	if _, err := os.Stat(cacheDir); os.IsNotExist(err) {
		ui.Msg(b.cfg, ui.LevelInfo, "No repo cache found.")
		return nil
	}
	if dryRun {
		ui.Msgf(b.cfg, ui.LevelWarn, "DRY-RUN: would remove repo cache at %s", cacheDir)
		return nil
	}
	ui.Msgf(b.cfg, ui.LevelInfo, "Remove repo cache? (%s)", cacheDir)
	if !ui.Confirm() {
		ui.Msg(b.cfg, ui.LevelWarn, "Cancelled.")
		return nil
	}
	if err := more.CleanCache(); err != nil {
		ui.Msgf(b.cfg, ui.LevelError, "%v", err)
		return err
	}
	ui.Msg(b.cfg, ui.LevelOK, "Done.")
	return nil
}

// fetchRepoEntry fetches a repo entry
func (b *Backend) fetchRepoEntry(pkgName string) (*more.Entry, *more.RemoteRef, error) {
	var remoteRef *more.RemoteRef
	var err error

	if more.IsRemoteURL(pkgName) {
		remoteRef, err = more.ParseRemoteURL(pkgName)
		if err != nil {
			return nil, nil, err
		}
	}

	if remoteRef != nil {
		fmt.Println()
		ui.Msgf(b.cfg, ui.LevelInfo, "fetching ALPSMORE from %s...", remoteRef.DisplayURL())
		fmt.Println()

		var resolved more.RemoteRef
		entry, resolved, err := more.FetchALPSMORERemote(*remoteRef)
		if err != nil {
			return nil, nil, err
		}

		source := resolved.Source()
		// Official alps-more takes priority
		if official, findErr := more.Find(entry.Name, b.cfg); findErr == nil {
			ui.Msgf(b.cfg, ui.LevelInfo, "%q found in official alps-more repo — using that instead.", official.Name)
			fmt.Println()
			entry = official
		} else {
			entry.Source = source
		}
		remoteRef = &resolved
		return entry, remoteRef, nil
	}

	entry, err := more.Find(pkgName, b.cfg)
	if err != nil {
		return nil, nil, err
	}

	return entry, nil, nil
}

// printRepoInstallPreview shows install preview for alps-more and GitHub entries
func (b *Backend) printRepoInstallPreview(entry *more.Entry, source string) {
	ui.Msgf(b.cfg, ui.LevelInfo, "Install %s%s%s from %s?",
		b.cfg.Style.ColorBold, entry.Name, b.cfg.Style.ColorReset+b.cfg.Style.ColorInfo, source)
	if entry.Desc != "" {
		fmt.Printf("  %s%s%s\n", b.cfg.Style.ColorDim, entry.Desc, b.cfg.Style.ColorReset)
	}
	if entry.Author != "" {
		fmt.Printf("  %sauthor: %s%s\n", b.cfg.Style.ColorDim, entry.Author, b.cfg.Style.ColorReset)
	}
	if entry.Version != "" {
		fmt.Printf("  %sversion: %s%s\n", b.cfg.Style.ColorDim, entry.Version, b.cfg.Style.ColorReset)
	}
	fmt.Println()

	fmt.Printf("  %sinstall:%s\n", b.cfg.Style.ColorBold, b.cfg.Style.ColorReset)
	for _, line := range entry.CmdLines {
		fmt.Printf("  %s$ %s%s\n", b.cfg.Style.ColorDim, line, b.cfg.Style.ColorReset)
	}

	fmt.Println()
	fmt.Print(b.cfg.Style.ColorReset)

	// Warn about free-mode packages (and flag a strict→free change) directly
	// above the confirmation prompt so it stays visible on TTY screens where
	// the top of the preview has already scrolled away.
	rec, _ := more.GetInstalled(entry.Name)
	more.WarnReducedSafety(entry, rec, b.cfg)

	fmt.Println()
}
