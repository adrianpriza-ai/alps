package more

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/adrianpriza-ai/alps/config"
	"github.com/adrianpriza-ai/alps/priv"
)

const scriptDownloadTimeout = 5 * time.Minute

// Entry represents a package entry.
type Entry struct {
	Name         string
	Desc         string
	Author       string
	Version      string
	Arch         []string
	OS           []string
	Deps         []string
	Servers      []string
	Safety       string
	CmdLines     []string
	RemoveLines  []string
	UpgradeLines []string
	PurgeLines   []string
	Source       string
}

func Parse(data []byte) (map[string]*Entry, error) {
	entries := make(map[string]*Entry)
	var current *Entry
	var inCmd, inRemove, inUpgrade, inPurge bool

	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			if current != nil {
				resolveEntry(entries, current)
			}
			name := line[1 : len(line)-1]
			current = &Entry{Name: name, Safety: "strict"} // default to strict mode
			inCmd, inRemove, inUpgrade, inPurge = false, false, false, false
			continue
		}

		if current == nil {
			continue
		}

		if consumed := parseSectionTag(line, &inCmd, &inRemove, &inUpgrade, &inPurge); consumed {
			continue
		}
		if parseSectionBody(line, current, inCmd, inRemove, inUpgrade, inPurge) {
			continue
		}
		parseKeyValue(line, current)
	}

	if current != nil {
		resolveEntry(entries, current)
	}

	return entries, scanner.Err()
}

func resolveEntry(entries map[string]*Entry, current *Entry) {
	existing, exists := entries[current.Name]
	if !exists {
		entries[current.Name] = current
		return
	}
	distro, distroLike := detectDistro()
	existingMatches := osMatches(existing.OS, distro, distroLike)
	currentMatches := osMatches(current.OS, distro, distroLike)
	if !existingMatches && currentMatches {
		entries[current.Name] = current
	}
}

func parseSectionTag(line string, inCmd, inRemove, inUpgrade, inPurge *bool) bool {
	switch line {
	case "cmd_begin":
		*inCmd = true
		*inRemove, *inUpgrade, *inPurge = false, false, false
	case "cmd_end":
		*inCmd = false
	case "remove_begin":
		*inRemove = true
		*inCmd, *inUpgrade, *inPurge = false, false, false
	case "remove_end":
		*inRemove = false
	case "upgrade_begin":
		*inUpgrade = true
		*inCmd, *inRemove, *inPurge = false, false, false
	case "upgrade_end":
		*inUpgrade = false
	case "purge_begin":
		*inPurge = true
		*inCmd, *inRemove, *inUpgrade = false, false, false
	case "purge_end":
		*inPurge = false
	default:
		return false
	}
	return true
}

func parseSectionBody(line string, e *Entry, inCmd, inRemove, inUpgrade, inPurge bool) bool {
	switch {
	case inCmd:
		e.CmdLines = append(e.CmdLines, line)
	case inRemove:
		e.RemoveLines = append(e.RemoveLines, line)
	case inUpgrade:
		e.UpgradeLines = append(e.UpgradeLines, line)
	case inPurge:
		e.PurgeLines = append(e.PurgeLines, line)
	default:
		return false
	}
	return true
}

func parseKeyValue(line string, e *Entry) {
	idx := strings.Index(line, "=")
	if idx < 0 {
		return
	}
	key := strings.TrimSpace(strings.ToLower(line[:idx]))
	val := strings.TrimSpace(line[idx+1:])

	switch key {
	case "desc":
		e.Desc = val
	case "author":
		e.Author = val
	case "version":
		e.Version = val
	case "arch":
		e.Arch = splitTrim(val)
	case "os":
		e.OS = splitTrim(val)
	case "servers":
		e.Servers = splitTrim(val)
	case "deps":
		e.Deps = splitTrim(val)
	case "safety":
		safety := strings.ToLower(val)
		if safety == "strict" || safety == "free" {
			e.Safety = safety
		} else {
			e.Safety = "strict" // default
		}
	}
}

func Find(name string, cfg *config.Config) (*Entry, error) {
	exists, expired := CacheStatus()
	if !exists {
		return nil, fmt.Errorf("no cache found, run: alps repo update")
	}
	if expired {
		fmt.Printf("  %s  repo cache is expired (>90 days). Using old cache.\n", cfg.Style.SymWarn)
		fmt.Println("        Run 'alps repo update' to refresh.")
		fmt.Println()
	}

	data, err := ReadCache()
	if err != nil {
		return nil, err
	}

	entries, err := Parse(data)
	if err != nil {
		return nil, fmt.Errorf("failed to parse repo: %w", err)
	}

	distro, distroLike := detectDistro()

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

// List returns entries for the current distro, including GitHub-sourced installs.
func List(cfg *config.Config) (map[string]*Entry, error) {
	exists, expired := CacheStatus()
	if !exists {
		return nil, fmt.Errorf("no cache found, run: alps repo update")
	}
	if expired {
		fmt.Printf("  %s  repo cache is expired (>90 days). Using old cache.\n", cfg.Style.SymWarn)
		fmt.Println("        Run 'alps repo update' to refresh.")
		fmt.Println()
	}

	data, err := ReadCache()
	if err != nil {
		return nil, err
	}

	all, err := Parse(data)
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
			if !isRemoteSource(rec.Source) {
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

func Search(query string, cfg *config.Config) ([]*Entry, error) {
	entries, err := List(cfg)
	if err != nil {
		return nil, err
	}

	q := strings.ToLower(query)
	var results []*Entry
	for _, e := range entries {
		if strings.Contains(strings.ToLower(e.Name), q) ||
			strings.Contains(strings.ToLower(e.Desc), q) {
			results = append(results, e)
		}
	}
	return results, nil
}

func Validate(e *Entry) error {
	if len(e.Arch) == 0 {
		return fmt.Errorf(
			"package %q has no 'arch' field defined in repo — cannot install safely",
			e.Name,
		)
	}
	sysArch := normalizeArch(runtime.GOARCH)
	if !containsCI(e.Arch, sysArch) {
		return fmt.Errorf(
			"package %q does not support your architecture (%s)\n  supported: %s",
			e.Name, sysArch, strings.Join(e.Arch, ", "),
		)
	}

	if len(e.OS) == 0 {
		return fmt.Errorf(
			"package %q has no 'os' field defined in repo — cannot install safely",
			e.Name,
		)
	}
	distro, distroLike := detectDistro()
	if !osMatches(e.OS, distro, distroLike) {
		return fmt.Errorf(
			"package %q does not support your distro (%s)\n  supported: %s",
			e.Name, distro, strings.Join(e.OS, ", "),
		)
	}

	if len(e.Deps) > 0 {
		var missing []string
		for _, dep := range e.Deps {
			if _, err := exec.LookPath(dep); err != nil {
				missing = append(missing, dep)
			}
		}
		if len(missing) > 0 {
			return fmt.Errorf(
				"package %q requires missing dependencies: %s\n  install them first then retry",
				e.Name, strings.Join(missing, ", "),
			)
		}
	}

	if len(e.CmdLines) == 0 {
		return fmt.Errorf(
			"package %q has no install commands (cmd_begin/cmd_end) defined — cannot install",
			e.Name,
		)
	}

	if e.Safety == "" {
		e.Safety = "strict"
	}

	if e.Safety == "free" {
		if len(e.RemoveLines) == 0 {
			return fmt.Errorf(
				"package %q has safety=free but no remove commands (remove_begin/remove_end) — free mode requires manual remove commands",
				e.Name,
			)
		}
	}

	return nil
}

func Install(e *Entry, cfg *config.Config) error {
	priv.Invalidate()

	rec, isInstalled := GetInstalled(e.Name)

	if isInstalled {
		if e.Version != "" && rec.Version != "" && e.Version != rec.Version {
			fmt.Printf("  %s  %s: %s -> %s\n",
				cfg.Style.SymArrow, e.Name, rec.Version, e.Version)
			return runUpgrade(e, cfg)
		}

		if e.Version != "" && rec.Version != "" {
			fmt.Printf("  %s  %s %s already up to date. Reinstalling...\n",
				cfg.Style.SymInfo, e.Name, e.Version)
		} else {
			fmt.Printf("  %s  %s already installed. Reinstalling...\n",
				cfg.Style.SymInfo, e.Name)
		}
	}

	return runInstall(e, cfg)
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
	// Skip validation here since it's done in main.go before confirmation

	// Step 1: Run remove commands
	lines, err := Scrape(e, OperationRemove)
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
	ctx.Op = OperationRemove

	manifest, err := Filter(lines, ctx, OperationRemove)
	if err != nil {
		return fmt.Errorf("failed to filter commands: %w", err)
	}

	if err := WriteManifest(manifest); err != nil {
		return fmt.Errorf("failed to write manifest: %w", err)
	}

	if err := ExecuteManifest(manifest, e, OperationRemove, ctx); err != nil {
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

// RemoveOwnedItems safely removes items tracked in owned_items list.
func RemoveOwnedItems(items []OwnedItem) error {
	if len(items) == 0 {
		return nil
	}

	fmt.Printf("  removing owned items (%d items)...\n", len(items))

	for i := len(items) - 1; i >= 0; i-- {
		item := items[i]

		switch item.Type {
		case "file":
			if err := removeFile(item.Path); err != nil {
				fmt.Printf("  %s  failed to remove file %s: %v\n", "⚠", item.Path, err)
			} else {
				fmt.Printf("  ✓ removed file %s\n", item.Path)
			}
		case "dir":
			if err := removeDir(item.Path); err != nil {
				fmt.Printf("  %s  failed to remove directory %s: %v\n", "⚠", item.Path, err)
			} else {
				fmt.Printf("  ✓ removed directory %s\n", item.Path)
			}
		case "symlink":
			if err := removeSymlink(item.Path); err != nil {
				fmt.Printf("  %s  failed to remove symlink %s: %v\n", "⚠", item.Path, err)
			} else {
				fmt.Printf("  ✓ removed symlink %s\n", item.Path)
			}
		case "service":
			if err := removeService(item.Path); err != nil {
				fmt.Printf("  %s  failed to remove service %s: %v\n", "⚠", item.Path, err)
			} else {
				fmt.Printf("  ✓ removed service %s\n", item.Path)
			}
		}
	}

	return nil
}

// cleanupOwnedItems removes tracked items with proper privilege handling
// Uses sudo on non-Termux systems unless already root
func cleanupOwnedItems(items []OwnedItem) {
	if len(items) == 0 {
		return
	}

	fmt.Printf("  cleaning up owned items (%d items)...\n", len(items))

	// Check if we need sudo
	needSudo := !isTermux() && !isMacOS() && !isRoot()

	for i := len(items) - 1; i >= 0; i-- {
		item := items[i]

		switch item.Type {
		case "file":
			if err := removeFileWithSudo(item.Path, needSudo); err != nil {
				fmt.Printf("  %s  failed to remove file %s: %v\n", "⚠", item.Path, err)
			} else {
				fmt.Printf("  ✓ removed file %s\n", item.Path)
			}
		case "dir":
			if err := removeDir(item.Path); err != nil {
				fmt.Printf("  %s  failed to remove directory %s: %v\n", "⚠", item.Path, err)
			} else {
				fmt.Printf("  ✓ removed directory %s\n", item.Path)
			}
		case "symlink":
			if err := removeSymlinkWithSudo(item.Path, needSudo); err != nil {
				fmt.Printf("  %s  failed to remove symlink %s: %v\n", "⚠", item.Path, err)
			} else {
				fmt.Printf("  ✓ removed symlink %s\n", item.Path)
			}
		case "service":
			if err := removeService(item.Path); err != nil {
				fmt.Printf("  %s  failed to remove service %s: %v\n", "⚠", item.Path, err)
			} else {
				fmt.Printf("  ✓ removed service %s\n", item.Path)
			}
		}
	}
}

// isRoot checks if the current user is root
func isRoot() bool {
	return os.Getuid() == 0
}

// removeFileWithSudo removes a file with optional sudo
func removeFileWithSudo(path string, useSudo bool) error {
	if isTermux() || isMacOS() || !useSudo {
		cmd := exec.Command("rm", "-f", path)
		return cmd.Run()
	}

	// Use priv for elevated privileges
	cmd, err := priv.Command("rm", "-f", path)
	if err != nil {
		return err
	}
	return cmd.Run()
}

// removeSymlinkWithSudo removes a symlink with optional sudo
func removeSymlinkWithSudo(path string, useSudo bool) error {
	if isTermux() || isMacOS() || !useSudo {
		cmd := exec.Command("rm", "-f", path)
		return cmd.Run()
	}

	// Use priv for elevated privileges
	cmd, err := priv.Command("rm", "-f", path)
	if err != nil {
		return err
	}
	return cmd.Run()
}

func removeFile(path string) error {
	if isTermux() || isMacOS() {
		cmd := exec.Command("rm", "-f", path)
		return cmd.Run()
	}

	// Use priv for elevated privileges on non-Termux/non-macOS systems
	cmd, err := priv.Command("rm", "-f", path)
	if err != nil {
		return err
	}
	return cmd.Run()
}

func removeDir(path string) error {
	cmd := exec.Command("rmdir", path)
	cmd.Stdout = nil
	cmd.Stderr = nil
	_ = cmd.Run()
	return nil
}

func removeSymlink(path string) error {
	if isTermux() || isMacOS() {
		cmd := exec.Command("rm", "-f", path)
		return cmd.Run()
	}

	// Use priv for elevated privileges on non-Termux/non-macOS systems
	cmd, err := priv.Command("rm", "-f", path)
	if err != nil {
		return err
	}
	return cmd.Run()
}

func removeService(service string) error {
	if isTermux() || isMacOS() {
		return nil
	}

	// Stop the service
	stopCmd, err := priv.Command("systemctl", "stop", service)
	if err == nil {
		stopCmd.Stdout = nil
		stopCmd.Stderr = nil
		_ = stopCmd.Run()
	}

	// Disable the service
	disableCmd, err := priv.Command("systemctl", "disable", service)
	if err == nil {
		disableCmd.Stdout = nil
		disableCmd.Stderr = nil
		_ = disableCmd.Run()
	}

	// Remove the service file
	serviceFile := "/etc/systemd/system/" + service
	if _, err := os.Stat(serviceFile); err == nil {
		removeFile(serviceFile)
	}

	return nil
}

func removeUser(username string) error {
	cmd := exec.Command("userdel", username)
	cmd.Stdout = nil
	cmd.Stderr = nil
	_ = cmd.Run()
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

// Upgrade upgrades a single package by name.
// Handles both alps-more and GitHub-sourced packages.
func Upgrade(name string, cfg *config.Config) error {
	priv.Invalidate()

	rec, isInstalled := GetInstalled(name)
	if !isInstalled {
		return fmt.Errorf("package %q is not installed via alps-more", name)
	}

	if isRemoteSource(rec.Source) {
		return UpgradeFromSource(name, rec.Source, cfg)
	}

	e, err := Find(name, cfg)
	if err != nil {
		return err
	}

	if e.Version == "" || rec.Version == "" {
		fmt.Printf("  %s  %s: no version info, reinstalling...\n", cfg.Style.SymInfo, name)
		return runInstall(e, cfg)
	}

	if e.Version == rec.Version {
		fmt.Printf("  %s  %s %s is already up to date.\n", cfg.Style.SymOK, name, e.Version)
		return nil
	}

	fmt.Printf("  %s  %s: %s -> %s\n", cfg.Style.SymArrow, name, rec.Version, e.Version)

	return runUpgrade(e, cfg)
}

// isRemoteSource returns true if the source string refers to a remote git forge.
func isRemoteSource(source string) bool {
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
		return runInstall(e, cfg)
	}
	if e.Version == rec.Version {
		fmt.Printf("  %s  %s %s is already up to date.\n", cfg.Style.SymOK, name, e.Version)
		return nil
	}

	fmt.Printf("  %s  %s: %s -> %s\n", cfg.Style.SymArrow, name, rec.Version, e.Version)

	return runUpgrade(e, cfg)
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
		if isRemoteSource(rec.Source) {
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
		if isRemoteSource(rec.Source) {
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

	var stale []string
	for name, rec := range records {
		if isRemoteSource(rec.Source) {
			continue
		}
		_, findErr := Find(name, cfg)
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

	summary := &UpdateSummary{}

	for name, rec := range records {
		// GitHub-sourced: skip stale detection, not applicable.
		if isRemoteSource(rec.Source) {
			continue
		}

		e, findErr := Find(name, cfg)
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

// Purge removes a package and its config/data files.
func Purge(name string, cfg *config.Config) error {
	priv.Invalidate()

	e, _, err := RemovalEntry(name, cfg)
	if err != nil {
		return err
	}

	rec, isInstalled := GetInstalled(name)
	// Skip validation here since it's done in main.go before confirmation

	if len(e.RemoveLines) == 0 && len(e.PurgeLines) == 0 && len(rec.OwnedItems) == 0 {
		return fmt.Errorf("package %q has no remove or purge commands defined", e.Name)
	}

	server, err := resolveServerIfNeeded(e)
	if err != nil {
		return err
	}

	// Step 1: Run remove commands
	if len(e.RemoveLines) > 0 {
		lines, err := Scrape(e, OperationRemove)
		if err != nil {
			return fmt.Errorf("remove commands failed: %w", err)
		}
		ctx := NewMacroContext(e, server)
		ctx.Op = OperationRemove
		manifest, err := Filter(lines, ctx, OperationRemove)
		if err != nil {
			return fmt.Errorf("failed to filter remove commands: %w", err)
		}
		if err := WriteManifest(manifest); err != nil {
			return fmt.Errorf("failed to write manifest: %w", err)
		}
		if err := ExecuteManifest(manifest, e, OperationRemove, ctx); err != nil {
			return fmt.Errorf("remove step failed: %w", err)
		}
	}

	// Step 2: Run purge commands
	if len(e.PurgeLines) > 0 {
		lines, err := Scrape(e, OperationPurge)
		if err != nil {
			return fmt.Errorf("purge commands failed: %w", err)
		}
		ctx := NewMacroContext(e, server)
		ctx.Op = OperationPurge
		manifest, err := Filter(lines, ctx, OperationPurge)
		if err != nil {
			return fmt.Errorf("failed to filter purge commands: %w", err)
		}
		if err := WriteManifest(manifest); err != nil {
			return fmt.Errorf("failed to write manifest: %w", err)
		}
		if err := ExecuteManifest(manifest, e, OperationPurge, ctx); err != nil {
			return fmt.Errorf("purge step failed: %w", err)
		}
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

// removeOwnedItemsVerbose removes tracked owned items and prints warnings on error.
func removeOwnedItemsVerbose(items []OwnedItem, cfg *config.Config) {
	if len(items) == 0 {
		return
	}
	fmt.Printf("  removing %d tracked item(s)...\n", len(items))
	cleanupOwnedItems(items)
}

func runInstall(e *Entry, cfg *config.Config) error {
	if len(e.CmdLines) == 0 {
		return fmt.Errorf("package %q has no install commands", e.Name)
	}

	server := ""
	if needsMirror(e) {
		var err error
		server, err = resolveServer(e.Servers)
		if err != nil {
			return fmt.Errorf("cannot resolve mirror server for {BASH_RUN}/{SERVER} macros: %w", err)
		}
	}

	// Get build directory for macro context
	pkgDir, err := getBuildDir(e.Name)
	if err != nil {
		return err
	}

	// Create macro context for tracking owned items
	ctx := NewMacroContext(e, server)
	ctx.BuildDir = pkgDir
	ctx.Op = OperationInstall

	// New flow: scrape -> filter -> apply safety -> execute
	lines, err := Scrape(e, OperationInstall)
	if err != nil {
		return err
	}

	manifest, err := Filter(lines, ctx, OperationInstall)
	if err != nil {
		return fmt.Errorf("failed to filter commands: %w", err)
	}

	// Write manifest for debugging purposes
	if err := WriteManifest(manifest); err != nil {
		return fmt.Errorf("failed to write manifest: %w", err)
	}

	if err := ExecuteManifest(manifest, e, OperationInstall, ctx); err != nil {
		return err
	}

	// Generate owned items from macro context and save to installed record
	ownedItems := GenerateOwnedItems(ctx)

	cleanupTempFiles()

	return MarkInstalledEntryWithOwnedItems(e, ownedItems)
}

func runUpgrade(e *Entry, cfg *config.Config) error {
	// New flow: scrape -> filter -> apply safety -> execute
	lines, err := Scrape(e, OperationUpgrade)
	if err != nil {
		return err
	}

	server := ""
	if needsMirror(e) {
		var err error
		server, err = resolveServer(e.Servers)
		if err != nil {
			return fmt.Errorf("cannot resolve mirror server for {BASH_RUN}/{SERVER} macros: %w", err)
		}
	}

	// Get build directory for macro context
	pkgDir, err := getBuildDir(e.Name)
	if err != nil {
		return err
	}

	// Create macro context for tracking owned items
	ctx := NewMacroContext(e, server)
	ctx.BuildDir = pkgDir
	ctx.Op = OperationUpgrade

	manifest, err := Filter(lines, ctx, OperationUpgrade)
	if err != nil {
		return fmt.Errorf("failed to filter commands: %w", err)
	}

	// Write manifest for debugging purposes
	if err := WriteManifest(manifest); err != nil {
		return fmt.Errorf("failed to write manifest: %w", err)
	}

	if err := ExecuteManifest(manifest, e, OperationUpgrade, ctx); err != nil {
		return err
	}

	ownedItems := GenerateOwnedItems(ctx)

	cleanupTempFiles()

	return MarkInstalledEntryWithOwnedItems(e, ownedItems)
}

func ensureSudo() error {
	if isTermux() || isMacOS() {
		return nil // Termux and macOS own their prefix — no privilege escalation needed
	}
	return priv.EnsureSudoOnly()
}

// reqTool describes a single binary requirement.
type reqTool struct {
	bin   string      // executable to look up via PATH
	label string      // human-readable name shown in the warning
	hint  string      // short install hint shown after the warning
	skip  func() bool // return true to skip the check on this platform
}

// requirements lists every tool that alps-more may need.
var requirements = []reqTool{
	{
		bin:   "bash",
		label: "bash",
		hint:  "install bash via your package manager",
	},
	{
		bin:   "tar",
		label: "tar",
		hint:  "install tar via your package manager",
	},
	{
		bin:   "unzip",
		label: "unzip (needed for .zip archives)",
		hint:  "install unzip via your package manager",
	},
	{
		// mkdir, cp, chmod, gzip, ln — all from coreutils; test one sentinel binary
		bin:   "gzip",
		label: "gzip / GNU coreutils (mkdir, cp, chmod, gzip, ln)",
		hint:  "install coreutils and gzip via your package manager",
	},
	{
		bin:   "fakeroot",
		label: "fakeroot (needed for strict-mode installs)",
		hint:  "alps install fakeroot  OR  apt install fakeroot",
		// fakeroot is not needed on Termux or macOS
		skip: func() bool { return isTermux() || isMacOS() },
	},
	{
		bin:   "systemctl",
		label: "systemctl (needed for systemd service macros)",
		hint:  "install systemd or run on a systemd-based distro",
		// systemd is not available on Termux or macOS
		skip: func() bool { return isTermux() || isMacOS() },
	},
	{
		bin:   "useradd",
		label: "useradd/userdel (needed for CREATE_USER / REMOVE_USER macros)",
		hint:  "install shadow-utils or equivalent via your package manager",
		// useradd is not available on Termux or macOS
		skip: func() bool { return isTermux() || isMacOS() },
	},
}

// WarnMissingRequirements checks for missing tools and prints a warning for
// each one that is absent but relevant on the current platform.
// It never returns an error — the warnings are informational only.
func WarnMissingRequirements(cfg *config.Config) {
	var missing []reqTool
	for _, r := range requirements {
		if r.skip != nil && r.skip() {
			continue
		}
		if _, err := exec.LookPath(r.bin); err != nil {
			missing = append(missing, r)
		}
	}
	if len(missing) == 0 {
		return
	}
	fmt.Printf("  %s  some requirements are missing — certain features may not work:\n", cfg.Style.SymWarn)
	for _, r := range missing {
		fmt.Printf("       • %s\n", r.label)
		fmt.Printf("         hint: %s\n", r.hint)
	}
	fmt.Println()
}

// needsMirror checks if commands use {BASH_RUN} or {SERVER}.
func needsMirror(e *Entry) bool {
	for _, lines := range [][]string{e.CmdLines, e.UpgradeLines, e.RemoveLines, e.PurgeLines} {
		for _, l := range lines {
			if strings.Contains(l, "{BASH_RUN}") ||
				strings.Contains(l, "{SERVER}") {
				return true
			}
		}
	}
	return false
}

// getBuildDir returns the per-package build directory.
// Pattern: ~/.cache/alps/more/<package-name>/
func getBuildDir(pkgName string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cannot determine home directory: %w", err)
	}
	dir := filepath.Join(home, ".cache", "alps", "more", pkgName)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", fmt.Errorf("cannot create build directory %s: %w", dir, err)
	}
	return dir, nil
}

// cleanupTempFiles removes temporary files created during package operations.
// This includes .alps_runner.txt and .alps_run*.sh files in the temp dir
// (os.TempDir(); /tmp on Linux, $PREFIX/tmp on Termux).
func cleanupTempFiles() {
	tmpDir := os.TempDir()

	runnerFile := filepath.Join(tmpDir, ".alps_runner.txt")
	_ = os.Remove(runnerFile)

	matches, err := filepath.Glob(filepath.Join(tmpDir, ".alps_run*.sh"))
	if err == nil {
		for _, match := range matches {
			_ = os.Remove(match)
		}
	}
}

// expandVars replaces variable placeholders in command lines.
// See ALPSMORE.md for macro documentation.
func expandVars(line, server, pkgDir, pkgVersion string) string {
	sysArch := normalizeArch(runtime.GOARCH)
	distro, _ := detectDistro()
	distroVer := detectDistroVersion()

	line = strings.ReplaceAll(line, "{ARCH}", sysArch)
	line = strings.ReplaceAll(line, "{OS}", runtime.GOOS)
	line = strings.ReplaceAll(line, "{DISTRO}", distro)
	line = strings.ReplaceAll(line, "{DISVER}", distroVer)
	line = strings.ReplaceAll(line, "{VERSION}", pkgVersion)
	line = strings.ReplaceAll(line, "{PKG_DIR}", pkgDir)
	if server != "" {
		line = strings.ReplaceAll(line, "{SERVER}", server)
	}
	return line
}

// handleDownloadMacro processes {DOWNLOAD} macro.
// See ALPSMORE.md for usage and examples.
func handleDownloadMacro(line, pkgDir string) error {
	trimmed := strings.TrimSpace(line)
	if !strings.HasPrefix(trimmed, "{DOWNLOAD}") {
		return fmt.Errorf("not a download macro: %s", line)
	}

	rest := strings.TrimSpace(strings.TrimPrefix(trimmed, "{DOWNLOAD}"))
	parts := strings.Fields(rest)
	if len(parts) == 0 {
		return fmt.Errorf("{DOWNLOAD} requires a URL")
	}

	url := parts[0]
	if !isAllowedURL(url) {
		return fmt.Errorf("disallowed URL host/scheme for {DOWNLOAD}: %s", url)
	}
	output := ""
	if len(parts) >= 2 {
		output = parts[1]
	} else {
		// Derive filename from URL
		if idx := strings.LastIndex(url, "/"); idx >= 0 && idx < len(url)-1 {
			output = url[idx+1:]
		} else {
			output = "download"
		}
		// Strip query string
		if idx := strings.Index(output, "?"); idx >= 0 {
			output = output[:idx]
		}
	}

	destPath := filepath.Join(pkgDir, output)

	fmt.Printf("  ↓ downloading %s\n", url)

	client := &http.Client{Timeout: 5 * time.Minute}
	resp, err := client.Get(url)
	if err != nil {
		return fmt.Errorf("download failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download failed: HTTP %d from %s", resp.StatusCode, url)
	}

	f, err := os.Create(destPath)
	if err != nil {
		return fmt.Errorf("cannot create file %s: %w", destPath, err)
	}
	defer f.Close()

	_, err = io.Copy(f, resp.Body)
	if err != nil {
		return fmt.Errorf("failed to write %s: %w", destPath, err)
	}

	fmt.Printf("  ✓ saved %s\n", output)
	return nil
}

// handleBashRun processes {BASH_RUN} macro (downloads and executes scripts).
// Supports full URLs and relative paths. See ALPSMORE.md for details.
func handleBashRun(line, server, pkgDir string) (string, error) {
	// Extract the path after {BASH_RUN}
	idx := strings.Index(line, "{BASH_RUN}")
	if idx < 0 {
		return line, nil
	}

	after := line[idx+len("{BASH_RUN}"):]
	parts := strings.Fields(strings.TrimSpace(after))
	if len(parts) == 0 {
		return "", fmt.Errorf("{BASH_RUN} requires a script path (URL or relative path)")
	}

	scriptPath := parts[0]
	scriptArgs := ""
	if len(parts) > 1 {
		scriptArgs = " " + strings.Join(parts[1:], " ")
	}

	// Use full URL as-is, otherwise prepend server for relative paths
	scriptURL := scriptPath
	if !strings.HasPrefix(scriptPath, "http://") && !strings.HasPrefix(scriptPath, "https://") {
		if server == "" {
			return "", fmt.Errorf("{BASH_RUN} relative path requires a server to be configured")
		}
		scriptURL = server + scriptPath
	}

	if !isAllowedURL(scriptURL) {
		return "", fmt.Errorf("disallowed URL host/scheme for {BASH_RUN}: %s", scriptURL)
	}

	client := &http.Client{Timeout: scriptDownloadTimeout}
	resp, err := client.Get(scriptURL)
	if err != nil {
		return "", fmt.Errorf("failed to download script from %s: %w", scriptURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("failed to download script: HTTP %d from %s", resp.StatusCode, scriptURL)
	}

	scriptData, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read script from %s: %w", scriptURL, err)
	}

	tmpFile := filepath.Join(pkgDir, ".alps_script.sh")
	if err := os.WriteFile(tmpFile, scriptData, 0755); err != nil {
		return "", fmt.Errorf("failed to write temp script to %s: %w", tmpFile, err)
	}

	prefix := line[:idx]
	return strings.TrimSpace(prefix + "bash " + tmpFile + scriptArgs), nil
}

// runLines executes commands in package build directory with macro expansion.
// See ALPSMORE.md for supported macros and usage.
func runLines(pkgName string, lines []string, server string, pkgVersion string) error {
	return runLinesWithContext(pkgName, lines, server, pkgVersion, nil)
}

// runLinesWithContext executes commands with macro expansion and context for safety mode
func runLinesWithContext(pkgName string, lines []string, server string, pkgVersion string, entry *Entry) error {
	ctx := NewMacroContext(entry, server)
	return runLinesWithContextMacro(pkgName, lines, server, pkgVersion, entry, ctx)
}

// runLinesWithContextMacro executes commands with a provided macro context for tracking owned items
func runLinesWithContextMacro(pkgName string, lines []string, server string, pkgVersion string, entry *Entry, ctx *MacroContext) error {
	pkgDir, err := getBuildDir(pkgName)
	if err != nil {
		return err
	}

	// Create macro context if not provided and entry is available
	if ctx == nil && entry != nil {
		ctx = NewMacroContext(entry, server)
	}

	// Combine all lines into a single script
	var scriptLines []string
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}

		line = expandVars(line, server, pkgDir, pkgVersion)

		skip, processed, err := processLineMacros(line, server, pkgDir, ctx)
		if err != nil {
			return err
		}
		if skip {
			continue
		}
		line = processed

		if line != "" {
			scriptLines = append(scriptLines, line)
		}
	}

	// Execute all lines as a single script (supports multi-line bash constructs
	// like if/then/fi, for/do/done, while/do/done, etc.)
	if len(scriptLines) > 0 {
		// set -e makes the script fail on the first error (preserves the old
		// "&&" fail-fast behavior across lines joined with newlines).
		script := "#!/usr/bin/env bash\nset -e\n" + strings.Join(scriptLines, "\n")
		if err := runScript(script, pkgDir, entry, ctx); err != nil {
			return fmt.Errorf("command failed:\n  %s\n  error: %w", strings.Join(scriptLines, "\n  "), err)
		}
	}

	return nil
}

// runScript writes a script to a temp file in pkgDir and executes it,
// wrapping in fakeroot when appropriate. The temp file is removed afterward.
func runScript(script, pkgDir string, entry *Entry, ctx *MacroContext) error {
	tmpFile := filepath.Join(pkgDir, ".alps_run.sh")
	if err := os.WriteFile(tmpFile, []byte(script), 0755); err != nil {
		return fmt.Errorf("cannot write build script to %s: %w", tmpFile, err)
	}
	defer os.Remove(tmpFile)

	cmd := buildScriptCmd(tmpFile, pkgDir, entry, ctx)
	return cmd.Run()
}

// buildScriptCmd builds the exec.Cmd for a script file, wrapping in fakeroot when appropriate.
func buildScriptCmd(scriptPath, pkgDir string, entry *Entry, ctx *MacroContext) *exec.Cmd {
	useFakeroot := false
	if ctx != nil && ctx.Safety == "strict" && hasFakeroot() {
		useFakeroot = true
	} else if ctx == nil && entry != nil && entry.Safety == "strict" && hasFakeroot() {
		useFakeroot = true
	}

	var cmd *exec.Cmd
	if useFakeroot {
		cmd = exec.Command("fakeroot", "bash", scriptPath)
	} else {
		cmd = exec.Command("bash", scriptPath)
	}
	cmd.Dir = pkgDir
	cmd.Env = append(os.Environ(), "TERM=xterm-256color")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	return cmd
}

// processLineMacros handles {DOWNLOAD}, {BASH_RUN}, and structured macros.
// Returns (skip, expandedLine, error). skip=true means the caller should continue to the next line.
func processLineMacros(line, server, pkgDir string, ctx *MacroContext) (skip bool, out string, err error) {
	// Handle legacy macros
	if skip, result, err := handleLegacyMacros(line, server, pkgDir); err != nil || skip {
		return skip, result, err
	}

	// Handle structured macros (INSTALL_BIN, etc.) when context is available
	return handleStructuredMacros(line, ctx)
}

// handleLegacyMacros handles {DOWNLOAD} and {BASH_RUN} macros.
func handleLegacyMacros(line, server, pkgDir string) (skip bool, out string, err error) {
	// Handle {DOWNLOAD} macro
	if strings.HasPrefix(strings.TrimSpace(line), "{DOWNLOAD}") {
		if err := handleDownloadMacro(line, pkgDir); err != nil {
			return false, "", fmt.Errorf("{DOWNLOAD} failed: %w", err)
		}
		return true, "", nil
	}

	// Handle {BASH_RUN} macro
	if strings.Contains(line, "{BASH_RUN}") {
		line, err = handleBashRun(line, server, pkgDir)
		if err != nil {
			return false, "", fmt.Errorf("{BASH_RUN} failed: %w", err)
		}
		return false, line, nil
	}

	return false, line, nil
}

// handleStructuredMacros handles INSTALL_* and other structured macros
func handleStructuredMacros(line string, ctx *MacroContext) (skip bool, out string, err error) {
	if ctx == nil {
		return false, line, nil
	}

	macro, remaining, isMacro := ParseMacro(line)
	if !isMacro {
		return handleNonMacroLine(line, ctx)
	}

	// Handle INSTALL_* macros for deferred execution
	if isInstallMacro(macro.Name) {
		return handleInstallMacro(line, macro, remaining, ctx)
	}

	// For other macros, use normal expansion
	return handleOtherMacros(line, ctx)
}

// handleNonMacroLine validates and returns non-macro lines
func handleNonMacroLine(line string, ctx *MacroContext) (skip bool, out string, err error) {
	// No validation needed - macros are now expanded to shell commands
	return false, line, nil
}

// handleInstallMacro processes INSTALL_* macros and skips them from the script
func handleInstallMacro(line string, macro Macro, remaining string, ctx *MacroContext) (skip bool, out string, err error) {
	// Expand variable tokens inside macro arguments for tracking
	expandMacroArgs(&macro, ctx)

	// Execute the macro for tracking purposes only (don't use the result)
	_, err = executeMacro(macro, ctx)
	if err != nil {
		return false, "", fmt.Errorf("macro tracking failed: %w", err)
	}

	// Skip the macro line entirely from the script
	if remaining == "" {
		return true, "", nil
	}

	// Process remaining text (if any) and return that only
	remainingResult, err := expandLine(remaining, ctx)
	if err != nil {
		return false, "", err
	}
	if remainingResult != "" {
		return false, remainingResult, nil
	}
	return true, "", nil
}

// handleOtherMacros processes non-INSTALL structured macros
func handleOtherMacros(line string, ctx *MacroContext) (skip bool, out string, err error) {
	expanded, err := expandLine(line, ctx)
	if err != nil {
		return false, "", fmt.Errorf("macro expansion failed: %w", err)
	}
	if expanded == "" {
		return true, "", nil // Macro was handled internally
	}
	return false, expanded, nil
}

// isInstallMacro checks if a macro is a deferred macro that should be excluded from the script
// Matches any macro matching: INSTALL_*, *_SERVICE, or *_USER patterns
func isInstallMacro(macroName string) bool {
	// Match any macro starting with INSTALL_
	if strings.HasPrefix(macroName, "INSTALL_") {
		return true
	}
	// Match any macro ending with _SERVICE
	if strings.HasSuffix(macroName, "_SERVICE") {
		return true
	}
	// Match any macro ending with _USER
	if strings.HasSuffix(macroName, "_USER") {
		return true
	}
	// Legacy exact matches for SYMLINK and other deferred ops
	installMacros := []string{
		"SYMLINK",
	}
	for _, m := range installMacros {
		if macroName == m {
			return true
		}
	}
	return false
}

// hasFakeroot checks if fakeroot is available.
func hasFakeroot() bool {
	_, err := exec.LookPath("fakeroot")
	return err == nil
}

// requireFakeroot returns an error if fakeroot is not available.
// Termux and macOS are exempt — they own their prefix and do not use fakeroot.
func requireFakeroot() error {
	if isTermux() || isMacOS() || isRoot() {
		return nil
	}
	if !hasFakeroot() {
		return fmt.Errorf("fakeroot is required, please install it first")
	}
	return nil
}

// hasFakerootLocal checks if fakeroot is available (alias for hasFakeroot)
func hasFakerootLocal() bool {
	return hasFakeroot()
}

// isWSL checks if running in WSL.
func isWSL() bool {
	if os.Getenv("WSL_DISTRO_NAME") != "" || os.Getenv("WSL_INTEROP") != "" {
		return true
	}
	if data, err := os.ReadFile("/proc/version"); err == nil {
		lower := strings.ToLower(string(data))
		return strings.Contains(lower, "microsoft") || strings.Contains(lower, "wsl")
	}
	return false
}

func splitTrim(s string) []string {
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func containsCI(list []string, target string) bool {
	t := strings.ToLower(target)
	for _, item := range list {
		if strings.ToLower(item) == t {
			return true
		}
	}
	return false
}

func normalizeArch(goarch string) string {
	switch goarch {
	case "amd64":
		return "x86_64"
	case "arm64":
		return "aarch64"
	case "386":
		return "i686"
	case "arm":
		return "armv7l"
	default:
		return goarch
	}
}

func detectDistro() (id string, idLike []string) {
	// Termux has no /etc/os-release — it is its own environment
	if isTermux() {
		return "termux", []string{"termux"}
	}

	// macOS detection
	if runtime.GOOS == "darwin" {
		return "macos", []string{"darwin", "macos"}
	}

	data, err := os.ReadFile("/etc/os-release")
	if err != nil {
		return "unknown", nil
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "ID=") {
			id = strings.Trim(line[3:], `"'`)
		} else if strings.HasPrefix(line, "ID_LIKE=") {
			raw := strings.Trim(line[8:], `"'`)
			idLike = strings.Fields(raw)
		}
	}

	// Inject "wsl" so entries with os=wsl explicitly match on WSL hosts
	if isWSL() {
		idLike = append(idLike, "wsl")
	}
	return
}

func detectDistroVersion() string {
	if isTermux() {
		ver := os.Getenv("TERMUX_VERSION")
		if ver != "" {
			return ver
		}
		return "unknown"
	}

	// macOS version detection
	if runtime.GOOS == "darwin" {
		cmd := exec.Command("sw_vers", "-productVersion")
		output, err := cmd.Output()
		if err == nil {
			return strings.TrimSpace(string(output))
		}
		return "unknown"
	}

	data, err := os.ReadFile("/etc/os-release")
	if err != nil {
		return "unknown"
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "VERSION_ID=") {
			return strings.Trim(line[11:], `"'`)
		}
	}
	return "unknown"
}

func osMatches(osList []string, distro string, idLike []string) bool {
	for _, o := range osList {
		o = strings.ToLower(strings.TrimSpace(o))
		if o == "linux" {
			if !isTermux() && runtime.GOOS != "darwin" {
				return true
			}
			continue
		}
		if o == "darwin" || o == "macos" {
			if runtime.GOOS == "darwin" {
				return true
			}
			continue
		}
		if strings.ToLower(distro) == o {
			return true
		}
		for _, like := range idLike {
			if strings.ToLower(like) == o {
				return true
			}
		}
	}
	return false
}
