package more

import (
	"fmt"
	"strings"

	"github.com/adrianpriza-ai/alps/config"
	"github.com/adrianpriza-ai/alps/platform"
	"github.com/adrianpriza-ai/alps/priv"
)

// Install installs a package entry, upgrading if a different version is already present.
func Install(e *Entry, cfg *config.Config) error {
	priv.Invalidate()

	rec, isInstalled := GetInstalled(e.Name)

	if isInstalled {
		// Different version → upgrade, not reinstall
		if e.Version != "" && rec.Version != "" && e.Version != rec.Version {
			fmt.Printf("  %s  %s: %s -> %s\n",
				cfg.Style.SymArrow, e.Name, rec.Version, e.Version)
			return runOperation(e, platform.OperationUpgrade)
		}

		// Same version or missing version info → reinstall
		if e.Version != "" && rec.Version != "" {
			fmt.Printf("  %s  %s %s already installed — reinstalling...\n",
				cfg.Style.SymInfo, e.Name, e.Version)
		} else {
			fmt.Printf("  %s  %s already installed — reinstalling...\n",
				cfg.Style.SymInfo, e.Name)
		}
	}

	return runOperation(e, platform.OperationInstall)
}

// InstallFromGitHub fetches an ALPSMORE file and installs it.
// Official alps-more entries take priority.
func InstallFromGitHub(repoPath string, cfg *config.Config) error {
	fmt.Printf("  fetching ALPSMORE from github.com/%s...\n", repoPath)

	e, err := FetchALPSMORE(repoPath)
	if err != nil {
		return err
	}

	// Official alps-more takes priority.
	if official, err := Find(e.Name, cfg); err == nil {
		fmt.Printf("  %s  %q found in official alps-more repo — using that instead.\n",
			cfg.Style.SymInfo, official.Name)
		return Install(official, cfg)
	}

	e.Source = "github:" + repoPath

	if err := Validate(e); err != nil {
		return err
	}

	return Install(e, cfg)
}

// Remove runs remove commands for a package.
func Remove(e *Entry, cfg *config.Config) error {
	priv.Invalidate()

	rec, isInstalled := GetInstalled(e.Name)

	// Step 1: Run remove commands
	lines, err := Scrape(e, platform.OperationRemove)
	if err != nil {
		if isInstalled && len(rec.OwnedItems) > 0 {
			// Even if remove commands fail, try to remove tracked items
			cleanupOwnedItems(rec.OwnedItems)
			cleanupTempFiles()
			return UnmarkInstalled(e.Name)
		}
		return err
	}

	server, err := resolveServerIfNeeded(e)
	if err != nil {
		return err
	}

	ctx := NewMacroContext(e, server)
	ctx.Op = platform.OperationRemove

	manifest, err := Filter(lines, ctx, platform.OperationRemove)
	if err != nil {
		return fmt.Errorf("failed to filter commands: %w", err)
	}

	if err := WriteManifest(manifest); err != nil {
		return fmt.Errorf("failed to write manifest: %w", err)
	}

	if err := ExecuteManifest(manifest, e, platform.OperationRemove, ctx); err != nil {
		fmt.Printf("  remove commands failed: %v\n", err)
		return fmt.Errorf("remove execution failed: %w", err)
	}

	// Step 2: Remove tracked items (always run this last)
	if isInstalled {
		cleanupOwnedItems(rec.OwnedItems)
	}

	cleanupTempFiles()

	if isInstalled {
		return UnmarkInstalled(e.Name)
	}
	return nil
}

// Upgrade upgrades a single package by name.
// Handles both alps-more and GitHub-sourced packages.
func Upgrade(name string, cfg *config.Config) error {
	priv.Invalidate()

	rec, isInstalled := GetInstalled(name)
	if !isInstalled {
		return fmt.Errorf("package %q is not installed via alps-more", name)
	}

	if IsRemoteSource(rec.Source) {
		return UpgradeFromSource(name, rec.Source, cfg)
	}

	e, err := Find(name, cfg)
	if err != nil {
		return err
	}

	if e.Version == "" || rec.Version == "" {
		fmt.Printf("  %s  %s: no version info, reinstalling...\n", cfg.Style.SymInfo, name)
		WarnReducedSafety(e, rec, cfg)
		return runOperation(e, platform.OperationInstall)
	}

	if e.Version == rec.Version {
		fmt.Printf("  %s  %s %s is already up to date.\n", cfg.Style.SymOK, name, e.Version)
		return nil
	}

	fmt.Printf("  %s  %s: %s -> %s\n", cfg.Style.SymArrow, name, rec.Version, e.Version)
	WarnReducedSafety(e, rec, cfg)

	return runOperation(e, platform.OperationUpgrade)
}

// IsRemoteSource returns true if the source string refers to a remote git forge.
func IsRemoteSource(source string) bool {
	_, err := ParseSource(source)
	return err == nil
}

// UpgradeFromSource re-fetches an ALPSMORE file from a remote source string
// ("github:user/repo" or "gitlab:user/repo") and upgrades if newer.
func UpgradeFromSource(name, source string, cfg *config.Config) error {
	priv.Invalidate()

	e, err := FetchALPSMOREFromSource(source)
	if err != nil {
		return fmt.Errorf("failed to fetch ALPSMORE from %s: %w", source, err)
	}
	e.Source = source

	rec, isInstalled := GetInstalled(name)
	if !isInstalled {
		return fmt.Errorf("package %q is not installed", name)
	}

	if e.Version == "" || rec.Version == "" {
		fmt.Printf("  %s  %s: no version info, reinstalling...\n", cfg.Style.SymInfo, name)
		WarnReducedSafety(e, rec, cfg)
		return runOperation(e, platform.OperationInstall)
	}
	if e.Version == rec.Version {
		fmt.Printf("  %s  %s %s is already up to date.\n", cfg.Style.SymOK, name, e.Version)
		return nil
	}

	fmt.Printf("  %s  %s: %s -> %s\n", cfg.Style.SymArrow, name, rec.Version, e.Version)
	WarnReducedSafety(e, rec, cfg)

	return runOperation(e, platform.OperationUpgrade)
}

// UpgradeEntry upgrades a single package using an already-resolved repo entry
// and its installed record. Unlike Upgrade(), it skips redundant lookups —
// useful when the caller (e.g. the backend) has already fetched the entry
// and compared versions during the preview phase.
func UpgradeEntry(e *Entry, rec *InstalledRecord, cfg *config.Config) error {
	priv.Invalidate()

	if e.Version == "" || rec.Version == "" {
		fmt.Printf("  %s  %s: no version info, reinstalling...\n", cfg.Style.SymInfo, e.Name)
		WarnReducedSafety(e, *rec, cfg)
		return runOperation(e, platform.OperationInstall)
	}

	if e.Version == rec.Version {
		fmt.Printf("  %s  %s %s is already up to date.\n", cfg.Style.SymOK, e.Name, e.Version)
		return nil
	}

	fmt.Printf("  %s  %s: %s -> %s\n", cfg.Style.SymArrow, e.Name, rec.Version, e.Version)
	WarnReducedSafety(e, *rec, cfg)

	return runOperation(e, platform.OperationUpgrade)
}

// UpgradeAll upgrades all installed packages.
// GitHub-sourced packages are upgraded by re-fetching their ALPSMORE file.
func UpgradeAll(cfg *config.Config) error {
	records, err := ReadInstalled()
	if err != nil {
		return err
	}
	if len(records) == 0 {
		fmt.Println("  No packages installed via alps-more.")
		return nil
	}

	var upgraded, upToDate, failed, stale int
	for name := range records {
		rec := records[name]

		// Remote-sourced (github/gitlab): upgrade by re-fetching ALPSMORE.
		if IsRemoteSource(rec.Source) {
			if err := UpgradeFromSource(name, rec.Source, cfg); err != nil {
				fmt.Printf("  %s  %s: %v\n", cfg.Style.SymErr, name, err)
				failed++
			} else {
				upgraded++
			}
			continue
		}

		e, err := Find(name, cfg)
		if err != nil {
			if strings.Contains(err.Error(), "not found in alps-more repo") {
				fmt.Printf("  %s  %s: no longer in repo (stale) — skipping\n", cfg.Style.SymWarn, name)
				fmt.Printf("       to remove: alps repo remove %s\n", name)
				stale++
			} else {
				fmt.Printf("  %s  %s: %v\n", cfg.Style.SymErr, name, err)
				failed++
			}
			continue
		}

		if e.Version != "" && rec.Version != "" && e.Version == rec.Version {
			fmt.Printf("  %s  %s %s\n", cfg.Style.SymOK, name, e.Version)
			upToDate++
			continue
		}

		if err := Upgrade(name, cfg); err != nil {
			fmt.Printf("  %s  %s: %v\n", cfg.Style.SymErr, name, err)
			failed++
		} else {
			upgraded++
		}
	}

	fmt.Printf("\n  upgraded: %d  up-to-date: %d  failed: %d", upgraded, upToDate, failed)
	if stale > 0 {
		fmt.Printf("  stale: %d", stale)
	}
	fmt.Println()
	return nil
}

// WarnReducedSafety prints a warning when the entry runs in free mode.
func WarnReducedSafety(e *Entry, rec InstalledRecord, cfg *config.Config) {
	if e == nil || e.Safety != "free" {
		return
	}
	msg := "This package/script runs at reduced safety (safety=free)."
	if rec.Safety == "strict" {
		msg = "Safety changed from strict to free — now running at reduced safety."
	}
	fmt.Printf("  %s%s%s  %s\n", cfg.Style.ColorWarning, cfg.Style.SymWarn, cfg.Style.ColorReset, msg)
}

// Purge removes a package and its config/data files.
func Purge(name string, cfg *config.Config) error {
	priv.Invalidate()

	e, _, err := RemovalEntry(name, cfg)
	if err != nil {
		return err
	}

	rec, isInstalled := GetInstalled(name)

	if err := validatePurgeCommands(e, rec); err != nil {
		return err
	}

	server, err := resolveServerIfNeeded(e)
	if err != nil {
		return err
	}

	// Step 1: Run remove commands
	if err := executePurgeRemoveStep(e, server); err != nil {
		return err
	}

	// Step 2: Run purge commands
	if err := executePurgePurgeStep(e, server); err != nil {
		return err
	}

	// Step 3: Remove tracked items (always run this last)
	if isInstalled {
		cleanupOwnedItems(rec.OwnedItems)
	}

	cleanupTempFiles()

	if isInstalled {
		return UnmarkInstalled(name)
	}
	return nil
}

// RemovalEntry returns the repo entry or saved uninstall snapshot.
func RemovalEntry(name string, cfg *config.Config) (*Entry, bool, error) {
	e, err := Find(name, cfg)
	if err == nil {
		return e, false, nil
	}
	if !strings.Contains(err.Error(), "not found in alps-more repo") {
		return nil, false, err
	}

	rec, isInstalled := GetInstalled(name)
	if !isInstalled {
		return nil, true, err
	}
	if len(rec.RemoveLines) == 0 && len(rec.PurgeLines) == 0 {
		return nil, true, fmt.Errorf("package %q is stale and has no saved remove/purge commands", name)
	}

	return &Entry{
		Name:        name,
		Version:     rec.Version,
		Servers:     append([]string(nil), rec.Servers...),
		Safety:      rec.Safety,
		RemoveLines: append([]string(nil), rec.RemoveLines...),
		PurgeLines:  append([]string(nil), rec.PurgeLines...),
		Source:      rec.Source,
	}, true, nil
}

// executePurgeRemoveStep executes the remove phase of purge.
func executePurgeRemoveStep(e *Entry, server string) error {
	if len(e.RemoveLines) == 0 {
		return nil
	}

	lines, err := Scrape(e, platform.OperationRemove)
	if err != nil {
		return fmt.Errorf("remove commands failed: %w", err)
	}

	ctx := NewMacroContext(e, server)
	ctx.Op = platform.OperationRemove

	manifest, err := Filter(lines, ctx, platform.OperationRemove)
	if err != nil {
		return fmt.Errorf("failed to filter remove commands: %w", err)
	}

	if err := WriteManifest(manifest); err != nil {
		return fmt.Errorf("failed to write manifest: %w", err)
	}

	if err := ExecuteManifest(manifest, e, platform.OperationRemove, ctx); err != nil {
		return fmt.Errorf("remove step failed: %w", err)
	}

	return nil
}

// executePurgePurgeStep executes the purge phase of purge.
func executePurgePurgeStep(e *Entry, server string) error {
	if len(e.PurgeLines) == 0 {
		return nil
	}

	lines, err := Scrape(e, platform.OperationPurge)
	if err != nil {
		return fmt.Errorf("purge commands failed: %w", err)
	}

	ctx := NewMacroContext(e, server)
	ctx.Op = platform.OperationPurge

	manifest, err := Filter(lines, ctx, platform.OperationPurge)
	if err != nil {
		return fmt.Errorf("failed to filter purge commands: %w", err)
	}

	if err := WriteManifest(manifest); err != nil {
		return fmt.Errorf("failed to write manifest: %w", err)
	}

	if err := ExecuteManifest(manifest, e, platform.OperationPurge, ctx); err != nil {
		return fmt.Errorf("purge step failed: %w", err)
	}

	return nil
}

// resolveServerIfNeeded resolves a mirror server URL when the entry references
// {BASH_RUN} or {SERVER} macros. Returns "" when not needed.
func resolveServerIfNeeded(e *Entry) (string, error) {
	if !needsMirror(e) {
		return "", nil
	}
	server, err := resolveServer(e.Servers)
	if err != nil {
		return "", fmt.Errorf("cannot resolve mirror server for {BASH_RUN}/{SERVER} macros: %w", err)
	}
	return server, nil
}

// runOperation executes the scrape -> filter -> manifest -> execute pipeline
// shared by install and upgrade flows, then records the installed state with
// the owned items collected during execution.
func runOperation(e *Entry, op platform.OperationType) error {
	// Install requires explicit commands; upgrades tolerate entries whose
	// ALPSMORE file has no commands in this revision.
	if op == platform.OperationInstall && len(e.CmdLines) == 0 {
		return fmt.Errorf("package %q has no install commands", e.Name)
	}

	server, err := resolveServerIfNeeded(e)
	if err != nil {
		return err
	}

	// Snapshot the old owned items before the operation so we can diff later.
	// On upgrade this lets us remove files the new version no longer installs.
	oldRec, hasOld := GetInstalled(e.Name)
	var oldOwned []OwnedItem
	if hasOld {
		oldOwned = oldRec.OwnedItems
	}

	// Get build directory for macro context
	pkgDir, err := getBuildDir(e.Name)
	if err != nil {
		return err
	}

	// Create macro context for tracking owned items
	ctx := NewMacroContext(e, server)
	ctx.BuildDir = pkgDir
	ctx.Op = op

	// Flow: scrape -> filter -> apply safety -> execute
	lines, err := Scrape(e, op)
	if err != nil {
		return err
	}

	manifest, err := Filter(lines, ctx, op)
	if err != nil {
		return fmt.Errorf("failed to filter commands: %w", err)
	}

	// Write manifest for debugging purposes
	if err := WriteManifest(manifest); err != nil {
		return fmt.Errorf("failed to write manifest: %w", err)
	}

	if err := ExecuteManifest(manifest, e, op, ctx); err != nil {
		return err
	}

	// Generate owned items from macro context and save to installed record
	newOwned := GenerateOwnedItems(ctx)

	// On upgrade, remove files the old version owned but the new version does not.
	// This prevents orphaned files from lingering after a version bump.
	if op == platform.OperationUpgrade && len(oldOwned) > 0 {
		stale := diffOwnedItems(oldOwned, newOwned)
		if len(stale) > 0 {
			cleanupOwnedItems(stale)
		}
	}

	cleanupTempFiles()

	return MarkInstalledEntryWithOwnedItems(e, newOwned)
}

// diffOwnedItems returns items present in old but not in new, matched by path+type.
func diffOwnedItems(old, new []OwnedItem) []OwnedItem {
	type key struct {
		Path string
		Type string
	}
	newSet := make(map[key]bool, len(new))
	for _, item := range new {
		newSet[key{item.Path, item.Type}] = true
	}
	var stale []OwnedItem
	for _, item := range old {
		if !newSet[key{item.Path, item.Type}] {
			stale = append(stale, item)
		}
	}
	return stale
}
