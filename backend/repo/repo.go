package repo

import (
	"fmt"
	"os"
	"sort"
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
		default:
			return fmt.Errorf("unknown list action %q (valid: install, remove)", args[0])
		}
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
	installed, err := more.ReadInstalled()
	if err != nil {
		ui.Msgf(b.cfg, ui.LevelWarn, "could not read installed state: %v", err)
		installed = make(map[string]more.InstalledRecord)
	}
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

// removeOrPurge removes or purges each package, returning true when any failed.
func (b *Backend) removeOrPurge(pkgs []string, dryRun, purge bool) (bool, error) {
	var hasErrors bool
	for _, pkgName := range pkgs {
		entry, stale, removalErr := more.RemovalEntry(pkgName, b.cfg)
		if removalErr != nil {
			ui.Msgf(b.cfg, ui.LevelError, "%v", removalErr)
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

		if purge {
			ui.Msgf(b.cfg, ui.LevelWarn, "Purge %s%s%s? This removes the package AND its config/data files.",
				b.cfg.Style.ColorBold, entry.Name, b.cfg.Style.ColorReset+b.cfg.Style.ColorWarning)
		} else {
			ui.Msgf(b.cfg, ui.LevelInfo, "Remove %s%s%s from alps-more?",
				b.cfg.Style.ColorBold, entry.Name, b.cfg.Style.ColorReset+b.cfg.Style.ColorInfo)
		}
		if stale {
			ui.Msg(b.cfg, ui.LevelWarn, "package is no longer in repo; using saved uninstall commands")
		}
		fmt.Println()

		if purge {
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
		} else {
			for _, line := range entry.RemoveLines {
				fmt.Printf("  %s$ %s%s\n", b.cfg.Style.ColorDim, line, b.cfg.Style.ColorReset)
			}
		}

		fmt.Print(b.cfg.Style.ColorReset)
		fmt.Println()
		if dryRun {
			action := "remove"
			if purge {
				action = "purge"
			}
			ui.Msgf(b.cfg, ui.LevelWarn, "DRY-RUN: would %s %s", action, entry.Name)
			continue
		}
		if !ui.Confirm() {
			ui.Msg(b.cfg, ui.LevelWarn, "Cancelled for "+entry.Name)
			continue
		}

		fmt.Println()
		var opErr error
		if purge {
			opErr = more.Purge(pkgName, b.cfg)
		} else {
			opErr = more.Remove(entry, b.cfg)
		}

		if opErr != nil {
			action := "remove"
			if purge {
				action = "purge"
			}
			ui.Msgf(b.cfg, ui.LevelError, "failed to %s %s: %v", action, entry.Name, opErr)
			hasErrors = true
		} else {
			action := "removed"
			if purge {
				action = "purged"
			}
			ui.Msg(b.cfg, ui.LevelOK, entry.Name+" "+action+".")
		}
	}
	if hasErrors {
		action := "remove"
		if purge {
			action = "purge"
		}
		return true, fmt.Errorf("some packages failed to %s", action)
	}
	return false, nil
}

// Remove removes packages from the repo
func (b *Backend) Remove(pkgs []string, dryRun bool) error {
	if len(pkgs) == 0 {
		ui.Msg(b.cfg, ui.LevelError, "Usage: alps repo remove <package> [packages...]")
		return fmt.Errorf("package name required")
	}
	_, err := b.removeOrPurge(pkgs, dryRun, false)
	return err
}

// Purge purges packages and their config files
func (b *Backend) Purge(pkgs []string, dryRun bool) error {
	if len(pkgs) == 0 {
		ui.Msg(b.cfg, ui.LevelError, "Usage: alps repo purge <package> [packages...]")
		return fmt.Errorf("package name required")
	}
	_, err := b.removeOrPurge(pkgs, dryRun, true)
	return err
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

// upgradeTarget reports whether a package should be upgraded, and to which
// version. ok is false when the entry carries no version to compare against.
func upgradeTarget(entryVersion, installedVersion string) (target string, upgradable, ok bool) {
	if entryVersion == "" {
		return "", false, false
	}
	if installedVersion == "" {
		return entryVersion, true, true
	}
	if entryVersion == installedVersion {
		return "", false, true
	}
	return entryVersion, true, true
}

// upgradeSummary returns the summary text and whether the caller should return an error.
func upgradeSummary(upgraded, skipped, failed int) (string, bool) {
	summary := fmt.Sprintf("Upgrade summary: %d upgraded", upgraded)
	if skipped > 0 {
		summary += fmt.Sprintf(", %d skipped", skipped)
	}
	if failed > 0 {
		summary += fmt.Sprintf(", %d failed", failed)
	}
	return summary, failed > 0
}

// Upgrade upgrades installed packages
func (b *Backend) Upgrade(pkgs []string, dryRun bool) error {
	// pkgPreview holds the pre-check result for a single package.
	// It stores the resolved entry and installed record so the execute
	// phase can call more.UpgradeEntry / more.UpgradeFromSource directly
	// without re-reading the installed DB or re-checking versions.
	type pkgPreview struct {
		name     string
		from, to string
		err      string // non-empty if the package can't be upgraded
		entry    *more.Entry
		rec      *more.InstalledRecord
		remote   string // non-empty if sourced from github/gitlab
	}

	var previews []pkgPreview

	if len(pkgs) == 0 {
		// Upgrade all: read installed packages and build a preview.
		records, err := more.ReadInstalled()
		if err != nil {
			ui.Msgf(b.cfg, ui.LevelError, "%v", err)
			return err
		}
		if len(records) == 0 {
			ui.Msg(b.cfg, ui.LevelWarn, "No packages installed via alps-more.")
			return nil
		}

		fmt.Println()
		names := make([]string, 0, len(records))
		for name := range records {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			rec := records[name]
			recCopy := rec // avoid pointer aliasing across iterations
			if more.IsRemoteSource(recCopy.Source) {
				fe, fetchErr := more.FetchALPSMOREFromSource(recCopy.Source)
				if fetchErr != nil {
					previews = append(previews, pkgPreview{name: name, err: fmt.Sprintf("fetch failed: %v", fetchErr), rec: &recCopy, remote: recCopy.Source})
					continue
				}
				fe.Source = recCopy.Source
				target, upgradable, ok := upgradeTarget(fe.Version, recCopy.Version)
				if !ok {
					previews = append(previews, pkgPreview{name: name, err: "no version information — skipped", rec: &recCopy, remote: recCopy.Source})
					continue
				}
				if upgradable {
					previews = append(previews, pkgPreview{name: name, from: recCopy.Version, to: target, entry: fe, rec: &recCopy, remote: recCopy.Source})
				} else {
					previews = append(previews, pkgPreview{name: name, from: recCopy.Version, to: recCopy.Version, rec: &recCopy, remote: recCopy.Source})
				}
				continue
			}
			e, findErr := more.Find(name, b.cfg)
			if findErr != nil {
				previews = append(previews, pkgPreview{name: name, err: "stale — no longer in repo", rec: &recCopy})
				continue
			}
			target, upgradable, ok := upgradeTarget(e.Version, recCopy.Version)
			if !ok {
				previews = append(previews, pkgPreview{name: name, err: "no version information — skipped", rec: &recCopy})
				continue
			}
			if upgradable {
				previews = append(previews, pkgPreview{name: name, from: recCopy.Version, to: target, entry: e, rec: &recCopy})
			} else {
				previews = append(previews, pkgPreview{name: name, from: recCopy.Version, to: recCopy.Version, entry: e, rec: &recCopy})
			}
		}
	} else {
		// Upgrade specific packages: build a preview for each.
		for _, pkgName := range pkgs {
			rec, isInstalled := more.GetInstalled(pkgName)
			if !isInstalled {
				previews = append(previews, pkgPreview{name: pkgName, err: "not installed"})
				continue
			}
			recCopy := rec // take address safely across iterations

			// Remote packages are fetched and checked at upgrade time
			// since the repo cache won't have their entry.
			if more.IsRemoteSource(recCopy.Source) {
				// Fetch the remote ALPSMORE to get the latest version for preview.
				fe, fetchErr := more.FetchALPSMOREFromSource(recCopy.Source)
				if fetchErr != nil {
					previews = append(previews, pkgPreview{name: pkgName, err: fmt.Sprintf("fetch failed: %v", fetchErr), rec: &recCopy, remote: recCopy.Source})
					continue
				}
				fe.Source = recCopy.Source
				target, upgradable, ok := upgradeTarget(fe.Version, recCopy.Version)
				if !ok {
					previews = append(previews, pkgPreview{name: pkgName, err: "no version information — skipped", rec: &recCopy, remote: recCopy.Source})
					continue
				}
				if upgradable {
					previews = append(previews, pkgPreview{name: pkgName, from: recCopy.Version, to: target, entry: fe, rec: &recCopy, remote: recCopy.Source})
				} else {
					previews = append(previews, pkgPreview{name: pkgName, from: recCopy.Version, to: recCopy.Version, rec: &recCopy, remote: recCopy.Source})
				}
				continue
			}

			e, findErr := more.Find(pkgName, b.cfg)
			if findErr != nil {
				previews = append(previews, pkgPreview{name: pkgName, err: findErr.Error(), rec: &recCopy})
				continue
			}

			target, upgradable, ok := upgradeTarget(e.Version, recCopy.Version)
			if !ok {
				previews = append(previews, pkgPreview{name: pkgName, err: "no version information — skipped", rec: &recCopy})
				continue
			}
			if upgradable {
				previews = append(previews, pkgPreview{name: pkgName, from: recCopy.Version, to: target, entry: e, rec: &recCopy})
			} else {
				previews = append(previews, pkgPreview{name: pkgName, from: recCopy.Version, to: recCopy.Version, entry: e, rec: &recCopy})
			}
		}
	}

	if len(previews) == 0 {
		return nil
	}

	// Count how many packages actually need upgrading.
	var upgradable int
	for _, p := range previews {
		if p.err == "" && p.from != p.to && p.to != "" {
			upgradable++
		}
	}

	if upgradable == 0 {
		ui.Msg(b.cfg, ui.LevelOK, "All alps-more packages are up to date.")
		return nil
	}

	// Show the full preview.
	ui.Msgf(b.cfg, ui.LevelInfo, "Upgrade %d package(s)?", upgradable)
	fmt.Println()
	for _, p := range previews {
		if p.err != "" {
			fmt.Printf("  %s!%s  %s %s(%s)%s\n",
				b.cfg.Style.ColorWarning, b.cfg.Style.ColorReset,
				p.name, b.cfg.Style.ColorDim, p.err, b.cfg.Style.ColorReset)
		} else if p.from == p.to {
			// Skip up-to-date packages — they clutter the preview when
			// there are many installed packages.
			continue
		} else {
			tag := ""
			if p.remote != "" {
				tag = " [remote]"
			}
			fmt.Printf("  %s%s%s  %s: %s%s%s -> %s%s%s%s\n",
				b.cfg.Style.ColorDim, b.cfg.Style.SymArrow, b.cfg.Style.ColorReset,
				p.name,
				b.cfg.Style.ColorDim, p.from, b.cfg.Style.ColorReset,
				b.cfg.Style.ColorSuccess, p.to, tag, b.cfg.Style.ColorReset)
		}
	}
	fmt.Println()

	if dryRun {
		ui.Msgf(b.cfg, ui.LevelWarn, "DRY-RUN: would upgrade %d package(s)", upgradable)
		return nil
	}

	if !ui.Confirm() {
		ui.Msg(b.cfg, ui.LevelWarn, "Upgrade cancelled.")
		return nil
	}

	// Execute upgrades. The preview already resolved every entry and
	// compared versions, so we call the lower-level UpgradeEntry /
	// UpgradeFromSource directly — no redundant lookups.
	fmt.Println()
	var upgraded, failed, skipped int
	for _, p := range previews {
		if p.err != "" {
			ui.Msgf(b.cfg, ui.LevelWarn, "%s: %s", p.name, p.err)
			skipped++
			continue
		}
		if p.from == p.to {
			continue
		}

		var err error
		if p.remote != "" && p.entry != nil {
			err = more.UpgradeFromSource(p.name, p.remote, b.cfg)
		} else if p.entry != nil && p.rec != nil {
			err = more.UpgradeEntry(p.entry, p.rec, b.cfg)
		} else {
			err = more.Upgrade(p.name, b.cfg)
		}

		if err != nil {
			ui.Msgf(b.cfg, ui.LevelError, "failed to upgrade %s: %v", p.name, err)
			failed++
		} else {
			ui.Msg(b.cfg, ui.LevelOK, p.name+" upgraded.")
			upgraded++
		}
	}

	// Summary when upgrading multiple packages.
	if len(previews) > 1 {
		fmt.Println()
		summary, _ := upgradeSummary(upgraded, skipped, failed)
		ui.Msg(b.cfg, ui.LevelInfo, summary)
	}

	if failed > 0 {
		return fmt.Errorf("%d package(s) failed to upgrade", failed)
	}
	return nil
}

// Clean removes the repo cache
func (b *Backend) Clean(dryRun bool) error {
	cacheDir, err := more.BuildCacheDir()
	if err != nil {
		ui.Msgf(b.cfg, ui.LevelError, "%v", err)
		return err
	}
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

	if more.IsRemoteURL(pkgName) {
		var parseErr error
		remoteRef, parseErr = more.ParseRemoteURL(pkgName)
		if parseErr != nil {
			return nil, nil, parseErr
		}
	}

	if remoteRef != nil {
		fmt.Println()
		ui.Msgf(b.cfg, ui.LevelInfo, "fetching ALPSMORE from %s...", remoteRef.DisplayURL())
		fmt.Println()

		var resolved more.RemoteRef
		entry, resolved, fetchErr := more.FetchALPSMORERemote(*remoteRef)
		if fetchErr != nil {
			return nil, nil, fetchErr
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

	entry, findErr := more.Find(pkgName, b.cfg)
	if findErr != nil {
		return nil, nil, findErr
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
