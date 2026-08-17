package more

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
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
	"github.com/adrianpriza-ai/alps/internal/runner"
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
	SHA256Sums   []string
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
		e.Deps = parseDeps(val)
	case "safety":
		safety := strings.ToLower(val)
		if safety == "strict" || safety == "free" {
			e.Safety = safety
		} else {
			e.Safety = "strict" // default
		}
	case "sha256sums":
		e.SHA256Sums = splitTrim(val)
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
	if err := validateArchitecture(e); err != nil {
		return err
	}

	if err := validateOS(e); err != nil {
		return err
	}

	if err := validateDependencies(e); err != nil {
		return err
	}

	if err := validateInstallCommands(e); err != nil {
		return err
	}

	validateSafetyMode(e)

	if err := validateSafetyRequirements(e); err != nil {
		return err
	}

	return nil
}

// validateArchitecture checks that the package supports the current architecture
func validateArchitecture(e *Entry) error {
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
	return nil
}

// validateOS checks that the package supports the current OS/distro
func validateOS(e *Entry) error {
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
	return nil
}

// validateDependencies checks that all required dependencies are available
func validateDependencies(e *Entry) error {
	if len(e.Deps) == 0 {
		return nil
	}

	var missing []string
	for _, depGroup := range e.Deps {
		if !checkDependencyGroup(depGroup) {
			missing = append(missing, depGroup)
		}
	}

	if len(missing) > 0 {
		return fmt.Errorf(
			"package %q requires missing dependencies: %s\n  install them first then retry",
			e.Name, strings.Join(missing, ", "),
		)
	}
	return nil
}

// checkDependencyGroup checks if a dependency group (single or OR-group) is satisfied
func checkDependencyGroup(depGroup string) bool {
	if strings.Contains(depGroup, "/") {
		alternatives := strings.Split(depGroup, "/")
		for _, alt := range alternatives {
			alt = strings.TrimSpace(alt)
			if _, err := exec.LookPath(alt); err == nil {
				return true
			}
		}
		return false
	}
	// Single dependency
	_, err := exec.LookPath(depGroup)
	return err == nil
}

// validateInstallCommands checks that install commands are defined
func validateInstallCommands(e *Entry) error {
	if len(e.CmdLines) == 0 {
		return fmt.Errorf(
			"package %q has no install commands (cmd_begin/cmd_end) defined — cannot install",
			e.Name,
		)
	}
	return nil
}

// validateSafetyMode sets default safety mode if not specified
func validateSafetyMode(e *Entry) {
	if e.Safety == "" {
		e.Safety = "strict"
	}
}

// validateSafetyRequirements checks that safety mode requirements are met
func validateSafetyRequirements(e *Entry) error {
	if e.Safety == "free" && len(e.RemoveLines) == 0 {
		return fmt.Errorf(
			"package %q has safety=free but no remove commands (remove_begin/remove_end) — free mode requires manual remove commands",
			e.Name,
		)
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

	cfg := config.Load()
	symOK := cfg.Style.SymOK
	symWarn := cfg.Style.SymWarn

	fmt.Printf("  removing owned items (%d items)...\n", len(items))

	for i := len(items) - 1; i >= 0; i-- {
		item := items[i]

		switch item.Type {
		case "file":
			if err := removeFile(item.Path); err != nil {
				fmt.Printf("  %s  failed to remove file %s: %v\n", symWarn, item.Path, err)
			} else {
				fmt.Printf("  %s  removed file %s\n", symOK, item.Path)
			}
		case "dir":
			if err := removeDir(item.Path); err != nil {
				fmt.Printf("  %s  failed to remove directory %s: %v\n", symWarn, item.Path, err)
			} else {
				fmt.Printf("  %s  removed directory %s\n", symOK, item.Path)
			}
		case "symlink":
			if err := removeSymlink(item.Path); err != nil {
				fmt.Printf("  %s  failed to remove symlink %s: %v\n", symWarn, item.Path, err)
			} else {
				fmt.Printf("  %s  removed symlink %s\n", symOK, item.Path)
			}
		case "service":
			if err := removeService(item.Path); err != nil {
				fmt.Printf("  %s  failed to remove service %s: %v\n", symWarn, item.Path, err)
			} else {
				fmt.Printf("  %s  removed service %s\n", symOK, item.Path)
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

	cfg := config.Load()
	symOK := cfg.Style.SymOK
	symWarn := cfg.Style.SymWarn

	fmt.Printf("  cleaning up owned items (%d items)...\n", len(items))

	// Check if we need sudo
	needSudo := !isTermux() && !isMacOS() && !isRoot()

	for i := len(items) - 1; i >= 0; i-- {
		item := items[i]

		switch item.Type {
		case "file":
			if err := removeFileWithSudo(item.Path, needSudo); err != nil {
				fmt.Printf("  %s  failed to remove file %s: %v\n", symWarn, item.Path, err)
			} else {
				fmt.Printf("  %s  removed file %s\n", symOK, item.Path)
			}
		case "dir":
			if err := removeDir(item.Path); err != nil {
				fmt.Printf("  %s  failed to remove directory %s: %v\n", symWarn, item.Path, err)
			} else {
				fmt.Printf("  %s  removed directory %s\n", symOK, item.Path)
			}
		case "symlink":
			if err := removeSymlinkWithSudo(item.Path, needSudo); err != nil {
				fmt.Printf("  %s  failed to remove symlink %s: %v\n", symWarn, item.Path, err)
			} else {
				fmt.Printf("  %s  removed symlink %s\n", symOK, item.Path)
			}
		case "service":
			if err := removeService(item.Path); err != nil {
				fmt.Printf("  %s  failed to remove service %s: %v\n", symWarn, item.Path, err)
			} else {
				fmt.Printf("  %s  removed service %s\n", symOK, item.Path)
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
	r := runner.NewDefaultRunner(false)
	cmd := runner.BuildCommand("rm", "-f", path)
	if useSudo && !(isTermux() || isMacOS()) {
		cmd = cmd.WithPrivilege()
	}
	return r.Run(context.Background(), cmd)
}

// removeSymlinkWithSudo removes a symlink with optional sudo
func removeSymlinkWithSudo(path string, useSudo bool) error {
	r := runner.NewDefaultRunner(false)
	cmd := runner.BuildCommand("rm", "-f", path)
	if useSudo && !(isTermux() || isMacOS()) {
		cmd = cmd.WithPrivilege()
	}
	return r.Run(context.Background(), cmd)
}

func removeFile(path string) error {
	r := runner.NewDefaultRunner(false)
	cmd := runner.BuildCommand("rm", "-f", path)
	if !(isTermux() || isMacOS()) {
		cmd = cmd.WithPrivilege()
	}
	return r.Run(context.Background(), cmd)
}

func removeDir(path string) error {
	cmd := exec.Command("rmdir", path)
	cmd.Stdout = nil
	cmd.Stderr = nil
	_ = cmd.Run()
	return nil
}

func removeSymlink(path string) error {
	r := runner.NewDefaultRunner(false)
	cmd := runner.BuildCommand("rm", "-f", path)
	if !(isTermux() || isMacOS()) {
		cmd = cmd.WithPrivilege()
	}
	return r.Run(context.Background(), cmd)
}

func removeService(service string) error {
	if isTermux() || isMacOS() {
		return nil
	}

	r := runner.NewDefaultRunner(false)

	// Stop the service
	stopCmd := runner.BuildCommand("systemctl", "stop", service).WithPrivilege()
	_ = r.Run(context.Background(), stopCmd)

	// Disable the service
	disableCmd := runner.BuildCommand("systemctl", "disable", service).WithPrivilege()
	_ = r.Run(context.Background(), disableCmd)

	// Remove the service file
	serviceFile := "/etc/systemd/system/" + service
	if _, err := os.Stat(serviceFile); err == nil {
		removeFile(serviceFile)
	}

	return nil
}

func removeUser(username string) error {
	r := runner.NewDefaultRunner(false)
	cmd := runner.BuildCommand("userdel", username).WithPrivilege()
	// Ignore errors, user may not exist
	_ = r.Run(context.Background(), cmd)
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

// WarnReducedSafety prints a warning when the entry runs in free mode, so a
// maintainer silently switching a package from strict to free never goes
// unnoticed at install or upgrade time. It is a no-op for strict-mode entries.
func WarnReducedSafety(e *Entry, rec InstalledRecord, cfg *config.Config) {
	if e == nil || e.Safety != "free" {
		return
	}
	msg := "This package/script runs at reduced safety (safety=free): commands are not validated and downloads are not SHA-256 verified."
	if rec.Safety == "strict" {
		msg = "Safety mode changed from strict to free — this package/script now runs at reduced safety: commands are not validated and downloads are not SHA-256 verified."
	}
	fmt.Printf("  %s%s%s  %s\n", cfg.Style.ColorWarning, cfg.Style.SymWarn, cfg.Style.ColorReset, msg)
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
		WarnReducedSafety(e, rec, cfg)
		return runInstall(e, cfg)
	}

	if e.Version == rec.Version {
		fmt.Printf("  %s  %s %s is already up to date.\n", cfg.Style.SymOK, name, e.Version)
		return nil
	}

	fmt.Printf("  %s  %s: %s -> %s\n", cfg.Style.SymArrow, name, rec.Version, e.Version)
	WarnReducedSafety(e, rec, cfg)

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
		WarnReducedSafety(e, rec, cfg)
		return runInstall(e, cfg)
	}
	if e.Version == rec.Version {
		fmt.Printf("  %s  %s %s is already up to date.\n", cfg.Style.SymOK, name, e.Version)
		return nil
	}

	fmt.Printf("  %s  %s: %s -> %s\n", cfg.Style.SymArrow, name, rec.Version, e.Version)
	WarnReducedSafety(e, rec, cfg)

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

// validatePurgeCommands checks that purge operations have required commands
func validatePurgeCommands(e *Entry, rec InstalledRecord) error {
	if len(e.RemoveLines) == 0 && len(e.PurgeLines) == 0 && len(rec.OwnedItems) == 0 {
		return fmt.Errorf("package %q has no remove or purge commands defined", e.Name)
	}
	return nil
}

// executePurgeRemoveStep executes the remove phase of purge
func executePurgeRemoveStep(e *Entry, server string) error {
	if len(e.RemoveLines) == 0 {
		return nil
	}

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

	return nil
}

// executePurgePurgeStep executes the purge phase of purge
func executePurgePurgeStep(e *Entry, server string) error {
	if len(e.PurgeLines) == 0 {
		return nil
	}

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
// Security: the package name is validated so a crafted name (e.g. "../evil")
// cannot escape the build cache root via path traversal.
func getBuildDir(pkgName string) (string, error) {
	if err := validatePkgNameComponent(pkgName); err != nil {
		return "", fmt.Errorf("invalid package name %q: %w", pkgName, err)
	}
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

// validatePkgNameComponent rejects package names that could escape the build
// cache root or otherwise break the expected directory layout.
func validatePkgNameComponent(name string) error {
	if name == "" {
		return fmt.Errorf("empty package name")
	}
	if strings.Contains(name, "..") || strings.Contains(name, "/") || strings.Contains(name, `\`) {
		return fmt.Errorf("package name must not contain path separators or traversal sequences")
	}
	if strings.HasPrefix(name, ".") {
		return fmt.Errorf("package name must not start with a dot")
	}
	if len(name) > 255 {
		return fmt.Errorf("package name too long")
	}
	for _, r := range name {
		if !(r >= 'a' && r <= 'z') && !(r >= 'A' && r <= 'Z') && !(r >= '0' && r <= '9') &&
			r != '-' && r != '_' && r != '+' && r != '.' {
			return fmt.Errorf("invalid character %q in package name", r)
		}
	}
	return nil
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

// handleBashRun processes {BASH_RUN} macro (downloads and executes scripts).
// Supports full URLs and relative paths. See ALPSMORE.md for details.
// Security: downloaded scripts MUST match an entry-level sha256sums digest
// before they are written or executed; unverified downloads are rejected.
// Usage: {BASH_RUN} url [args...]
func handleBashRun(line, server, pkgDir string, ctx *MacroContext) (string, error) {
	// Extract the path after {BASH_RUN}
	idx := strings.Index(line, "{BASH_RUN}")
	if idx < 0 {
		return line, nil
	}

	after := line[idx+len("{BASH_RUN}"):]
	parts := strings.Fields(strings.TrimSpace(after))
	if len(parts) < 1 {
		return "", fmt.Errorf("{BASH_RUN} requires script URL: {BASH_RUN} url [args...]")
	}

	scriptURL := parts[0]
	scriptArgs := ""
	if len(parts) > 1 {
		scriptArgs = " " + strings.Join(parts[1:], " ")
	}

	// Use full URL as-is, otherwise prepend server for relative paths
	finalURL := scriptURL
	if !strings.HasPrefix(scriptURL, "http://") && !strings.HasPrefix(scriptURL, "https://") {
		if server == "" {
			return "", fmt.Errorf("{BASH_RUN} relative path requires a server to be configured")
		}
		finalURL = server + scriptURL
	}

	if !isAllowedURL(finalURL) {
		return "", fmt.Errorf("disallowed URL host/scheme for {BASH_RUN}: %s", finalURL)
	}

	// Security: every downloaded script must be declared in sha256sums, and the
	// digest is resolved before anything is fetched so unverifiable content is
	// never downloaded in the first place.
	if ctx == nil {
		return "", fmt.Errorf("{BASH_RUN} requires macro context for digest verification")
	}
	expectedHash, err := requireNextSha256(ctx, scriptURL)
	if err != nil {
		return "", err
	}

	scriptData, err := downloadScriptWithLimit(finalURL, maxScriptSize)
	if err != nil {
		return "", fmt.Errorf("failed to download script from %s: %w", finalURL, err)
	}

	computedHash := fmt.Sprintf("%x", sha256.Sum256(scriptData))
	// Free mode may opt out of digest verification (expectedHash == "").
	if expectedHash != "" && computedHash != expectedHash {
		return "", fmt.Errorf("SHA256 mismatch for %s: expected %s, got %s", finalURL, expectedHash, computedHash)
	}

	// Write script with restrictive permissions
	tmpFile := filepath.Join(pkgDir, fmt.Sprintf(".alps_script_%x.sh", sha256.Sum256([]byte(finalURL))))
	if err := os.WriteFile(tmpFile, scriptData, 0700); err != nil {
		return "", fmt.Errorf("failed to write temp script to %s: %w", tmpFile, err)
	}

	prefix := line[:idx]
	return strings.TrimSpace(prefix + "bash " + tmpFile + scriptArgs), nil
}

// downloadScriptWithLimit downloads a script with size limit and security checks.
// This is a local version of downloadOnceWithSizeLimit for script downloads.
func downloadScriptWithLimit(url string, maxSize int64) ([]byte, error) {
	if !isAllowedURL(url) {
		return nil, fmt.Errorf("disallowed download URL host/scheme: %s", url)
	}
	client := &http.Client{Timeout: scriptDownloadTimeout}
	resp, err := client.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d from %s", resp.StatusCode, url)
	}

	// Security: Limit response size to prevent denial of service
	limitedReader := io.LimitReader(resp.Body, maxSize)
	body, err := io.ReadAll(limitedReader)
	if err != nil {
		return nil, err
	}
	if len(body) == 0 {
		return nil, fmt.Errorf("empty response from %s", url)
	}
	// Check if we hit the size limit
	if int64(len(body)) >= maxSize {
		return nil, fmt.Errorf("response too large from %s (exceeds %d bytes)", url, maxSize)
	}
	return body, nil
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
// Security: Displays the generated script before execution for transparency.
func runScript(script, pkgDir string, entry *Entry, ctx *MacroContext) error {
	// Security: Display the script that will be executed
	fmt.Printf("  Executing the following script:\n")
	fmt.Printf("  ---\n")
	for _, line := range strings.Split(script, "\n") {
		fmt.Printf("  %s\n", line)
	}
	fmt.Printf("  ---\n")

	// Security: use a unique temporary file with restrictive (owner-only)
	// permissions instead of a fixed, predictable name in the build directory.
	tmpFile, err := os.CreateTemp(pkgDir, ".alps_run_*.sh")
	if err != nil {
		return fmt.Errorf("cannot create build script in %s: %w", pkgDir, err)
	}
	tmpName := tmpFile.Name()
	defer os.Remove(tmpName)

	if err := tmpFile.Chmod(0700); err != nil {
		tmpFile.Close()
		return fmt.Errorf("cannot chmod build script: %w", err)
	}
	if _, err := tmpFile.WriteString(script); err != nil {
		tmpFile.Close()
		return fmt.Errorf("cannot write build script: %w", err)
	}
	if err := tmpFile.Close(); err != nil {
		return fmt.Errorf("cannot close build script: %w", err)
	}

	cmd := buildScriptCmd(tmpName, pkgDir, entry, ctx)
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

// processLineMacros handles {BASH_RUN} and structured macros.
// Returns (skip, expandedLine, error). skip=true means the caller should continue to the next line.
func processLineMacros(line, server, pkgDir string, ctx *MacroContext) (skip bool, out string, err error) {
	// Handle legacy macros
	if skip, result, err := handleLegacyMacros(line, server, pkgDir, ctx); err != nil || skip {
		return skip, result, err
	}

	// Handle structured macros (DOWNLOAD, INSTALL_BIN, etc.) when context is available
	return handleStructuredMacros(line, ctx)
}

// handleLegacyMacros handles {BASH_RUN} macros.
// Note: {DOWNLOAD} is now handled by the structured macro system in macros.go
func handleLegacyMacros(line, server, pkgDir string, ctx *MacroContext) (skip bool, out string, err error) {
	// Handle {BASH_RUN} macro
	if strings.Contains(line, "{BASH_RUN}") {
		line, err = handleBashRun(line, server, pkgDir, ctx)
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

// parseDeps parses dependency string, preserving OR groups (slash-separated)
// e.g., "curl/wget, git" -> ["curl/wget", "git"]
func parseDeps(val string) []string {
	groups := strings.Split(val, ",")
	out := make([]string, 0, len(groups))
	for _, group := range groups {
		group = strings.TrimSpace(group)
		if group != "" {
			out = append(out, group)
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
