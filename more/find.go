package more

import (
	"fmt"
	"sort"
	"strings"

	"github.com/adrianpriza-ai/alps/config"
)

// checkCacheStatus verifies the cache exists and prints a warning if expired.
func checkCacheStatus(cfg *config.Config) error {
	exists, expired := CacheStatus()
	if !exists {
		return fmt.Errorf("no cache found, run: alps repo update")
	}
	if expired {
		fmt.Printf("  %s  repo cache is expired (>90 days). Using old cache.\n", cfg.Style.SymWarn)
		fmt.Println("        Run 'alps repo update' to refresh.")
		fmt.Println()
	}
	return nil
}

// loadCacheEntries verifies cache status, reads, and parses the repo cache.
func loadCacheEntries(cfg *config.Config) (map[string]*Entry, error) {
	if err := checkCacheStatus(cfg); err != nil {
		return nil, err
	}

	data, err := ReadCache()
	if err != nil {
		return nil, err
	}

	entries, err := Parse(data)
	if err != nil {
		return nil, fmt.Errorf("failed to parse repo: %w", err)
	}

	return entries, nil
}

// findEntry looks up a package by name in entries and validates distro compatibility.
func findEntry(entries map[string]*Entry, name string, distro string, distroLike []string) (*Entry, error) {
	e, ok := entries[name]
	if !ok {
		return nil, fmt.Errorf("package %q not found in alps-more repo", name)
	}

	if !osMatches(e.OS, distro, distroLike) {
		return nil, fmt.Errorf(
			"package %q is not available for your distro (%s)\n  supported: %s",
			name, distro, strings.Join(e.OS, ", "),
		)
	}

	return e, nil
}

// Find looks up a package by name in the repo cache and returns its Entry.
func Find(name string, cfg *config.Config) (*Entry, error) {
	entries, err := loadCacheEntries(cfg)
	if err != nil {
		return nil, err
	}

	distro, distroLike := detectDistro()
	return findEntry(entries, name, distro, distroLike)
}

// List returns entries for the current distro, including GitHub-sourced installs.
func List(cfg *config.Config) (map[string]*Entry, error) {
	all, err := loadCacheEntries(cfg)
	if err != nil {
		return nil, err
	}

	distro, distroLike := detectDistro()

	filtered := make(map[string]*Entry)
	for _, e := range all {
		if osMatches(e.OS, distro, distroLike) {
			filtered[e.Name] = e
		}
	}

	// Append GitHub-sourced installs not in main.txt.
	records, err := ReadInstalled()
	if err == nil {
		for name, rec := range records {
			if !IsRemoteSource(rec.Source) {
				continue
			}
			if _, exists := filtered[name]; exists {
				continue
			}
			filtered[name] = &Entry{
				Name:        name,
				Version:     rec.Version,
				RemoveLines: append([]string(nil), rec.RemoveLines...),
				PurgeLines:  append([]string(nil), rec.PurgeLines...),
				Servers:     append([]string(nil), rec.Servers...),
				Safety:      rec.Safety,
				Source:      rec.Source,
			}
		}
	}

	return filtered, nil
}

// Search returns entries whose name, description, author, or dependencies
// match the query (case-insensitive).
func Search(query string, cfg *config.Config) ([]*Entry, error) {
	entries, err := List(cfg)
	if err != nil {
		return nil, err
	}

	q := strings.ToLower(query)
	var results []*Entry
	for _, e := range entries {
		if entryMatchesQuery(e, q) {
			results = append(results, e)
		}
	}
	return results, nil
}

// entryMatchesQuery reports whether the query appears in the entry's name,
// description, author, or any of its dependencies, all case-insensitive.
func entryMatchesQuery(e *Entry, q string) bool {
	if strings.Contains(strings.ToLower(e.Name), q) ||
		strings.Contains(strings.ToLower(e.Desc), q) ||
		strings.Contains(strings.ToLower(e.Author), q) {
		return true
	}
	for _, dep := range e.Deps {
		if strings.Contains(strings.ToLower(dep), q) {
			return true
		}
	}
	return false
}

// --- Installed package listing (from list.go) ---

// ListInstalled prints all packages installed via alps-more or GitHub.
func ListInstalled(cfg *config.Config) error {
	records, err := ReadInstalled()
	if err != nil {
		return err
	}
	if len(records) == 0 {
		fmt.Println("  No packages installed via alps-more.")
		return nil
	}

	names := make([]string, 0, len(records))
	for name := range records {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		rec := records[name]
		ver := rec.Version
		if ver == "" {
			ver = "(no version)"
		}
		tag := ""
		if IsRemoteSource(rec.Source) {
			tag = "  [" + rec.Source + "]"
		}
		fmt.Printf("  %s  %s %s%s\n", cfg.Style.SymOK, name, ver, tag)
		if rec.InstalledAt != "" {
			fmt.Printf("         installed: %s\n", rec.InstalledAt)
		}
	}
	return nil
}

// ListStale prints packages that are in installed.json but no longer in main.txt.
// GitHub-sourced packages are not considered stale.
func ListStale(cfg *config.Config) error {
	records, err := ReadInstalled()
	if err != nil {
		return err
	}
	if len(records) == 0 {
		fmt.Println("  No packages installed via alps-more.")
		return nil
	}

	var hasLocal bool
	for _, rec := range records {
		if !IsRemoteSource(rec.Source) {
			hasLocal = true
			break
		}
	}
	if !hasLocal {
		fmt.Printf("  %s  No stale packages found.\n", cfg.Style.SymOK)
		return nil
	}

	entries, err := loadCacheEntries(cfg)
	if err != nil {
		return err
	}
	distro, distroLike := detectDistro()

	var stale []string
	for name, rec := range records {
		if IsRemoteSource(rec.Source) {
			continue
		}
		_, findErr := findEntry(entries, name, distro, distroLike)
		if findErr != nil && strings.Contains(findErr.Error(), "not found in alps-more repo") {
			stale = append(stale, name)
		}
	}

	if len(stale) == 0 {
		fmt.Printf("  %s  No stale packages found.\n", cfg.Style.SymOK)
		return nil
	}

	sort.Strings(stale)
	fmt.Printf("  %s  Packages no longer in alps-more repo:\n", cfg.Style.SymWarn)
	for _, name := range stale {
		fmt.Printf("    %s  %s\n", cfg.Style.SymBullet, name)
		fmt.Printf("         to remove: alps repo remove %s\n", name)
	}
	return nil
}

// UpdateSummary holds upgrade and stale package info.
type UpdateSummary struct {
	Upgradeable []string // formatted: "name oldver → newver"
	Stale       []string // package names absent from repo
}

// CheckUpdates checks for upgrades and stale packages.
func CheckUpdates(cfg *config.Config) (*UpdateSummary, error) {
	records, err := ReadInstalled()
	if err != nil {
		return nil, err
	}
	if len(records) == 0 {
		return nil, nil
	}

	var hasLocal bool
	for _, rec := range records {
		if !IsRemoteSource(rec.Source) {
			hasLocal = true
			break
		}
	}
	if !hasLocal {
		return &UpdateSummary{}, nil
	}

	entries, err := loadCacheEntries(cfg)
	if err != nil {
		return nil, err
	}
	distro, distroLike := detectDistro()

	summary := &UpdateSummary{}

	for name, rec := range records {
		// GitHub-sourced: skip stale detection, not applicable.
		if IsRemoteSource(rec.Source) {
			continue
		}

		e, findErr := findEntry(entries, name, distro, distroLike)
		if findErr != nil {
			if strings.Contains(findErr.Error(), "not found in alps-more repo") {
				summary.Stale = append(summary.Stale, name)
				continue
			}
			return nil, findErr
		}

		if e.Version != "" && rec.Version != "" && e.Version != rec.Version {
			summary.Upgradeable = append(summary.Upgradeable,
				fmt.Sprintf("%s %s → %s", name, rec.Version, e.Version))
		}
	}

	sort.Strings(summary.Upgradeable)
	sort.Strings(summary.Stale)
	return summary, nil
}
