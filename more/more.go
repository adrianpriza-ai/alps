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
	Servers      []string // optional mirror list; falls back to defaultServers if empty
	Sudo         bool
	Safety       string // "strict" or "free" (default: "strict")
	CmdLines     []string
	RemoveLines  []string
	UpgradeLines []string
	PurgeLines   []string
	// Source is set for entries fetched outside alps-more.
	// Format: "github:user/repo"
	Source string
}

// Parse parses main.txt content.
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

		// New package header
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			if current != nil {
				entries[current.Name] = current
			}
			name := line[1 : len(line)-1]
			current = &Entry{Name: name, Safety: "strict"} // default to strict mode
			inCmd, inRemove, inUpgrade, inPurge = false, false, false, false
			continue
		}

		if current == nil {
			continue
		}

		switch {
		case line == "cmd_begin":
			inCmd = true
			inRemove, inUpgrade, inPurge = false, false, false
		case line == "cmd_end":
			inCmd = false
		case line == "remove_begin":
			inRemove = true
			inCmd, inUpgrade, inPurge = false, false, false
		case line == "remove_end":
			inRemove = false
		case line == "upgrade_begin":
			inUpgrade = true
			inCmd, inRemove, inPurge = false, false, false
		case line == "upgrade_end":
			inUpgrade = false
		case line == "purge_begin":
			inPurge = true
			inCmd, inRemove, inUpgrade = false, false, false
		case line == "purge_end":
			inPurge = false
		case inCmd:
			current.CmdLines = append(current.CmdLines, line)
		case inRemove:
			current.RemoveLines = append(current.RemoveLines, line)
		case inUpgrade:
			current.UpgradeLines = append(current.UpgradeLines, line)
		case inPurge:
			current.PurgeLines = append(current.PurgeLines, line)
		default:
			idx := strings.Index(line, "=")
			if idx < 0 {
				continue
			}
			key := strings.TrimSpace(strings.ToLower(line[:idx]))
			val := strings.TrimSpace(line[idx+1:])

			switch key {
			case "desc":
				current.Desc = val
			case "author":
				current.Author = val
			case "version":
				current.Version = val
			case "arch":
				current.Arch = splitTrim(val)
			case "os":
				current.OS = splitTrim(val)
			case "servers":
				current.Servers = splitTrim(val)
			case "deps":
				current.Deps = splitTrim(val)
			case "sudo":
				current.Sudo = strings.ToLower(val) == "true"
			case "safety":
				safety := strings.ToLower(val)
				if safety == "strict" || safety == "free" {
					current.Safety = safety
				} else {
					current.Safety = "strict" // default
				}
			}
		}
	}

	// Save last entry
	if current != nil {
		entries[current.Name] = current
	}

	return entries, scanner.Err()
}

// Find looks up a package by name.
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

// List returns all entries for current distro from main.txt,
// plus any GitHub-sourced packages currently installed.
// Official alps-more entries always win on name conflict.
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

	// Append GitHub-sourced installed packages not in main.txt.
	records, err := ReadInstalled()
	if err == nil {
		for name, rec := range records {
			if !isRemoteSource(rec.Source) {
				continue
			}
			if _, exists := filtered[name]; exists {
				// Official entry wins — skip
				continue
			}
			filtered[name] = &Entry{
				Name:        name,
				Version:     rec.Version,
				RemoveLines: append([]string(nil), rec.RemoveLines...),
				PurgeLines:  append([]string(nil), rec.PurgeLines...),
				Servers:     append([]string(nil), rec.Servers...),
				Sudo:        rec.Sudo,
				Safety:      rec.Safety,
				Source:      rec.Source,
			}
		}
	}

	return filtered, nil
}

// Search returns entries matching query.
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

// Validate checks compatibility.
func Validate(e *Entry) error {
	// arch check (required)
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

	// os/distro check
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

	// deps check
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

	// cmd_begin/cmd_end check (required)
	if len(e.CmdLines) == 0 {
		return fmt.Errorf(
			"package %q has no install commands (cmd_begin/cmd_end) defined — cannot install",
			e.Name,
		)
	}

	// Safety mode validation
	if e.Safety == "" {
		e.Safety = "strict" // default
	}
	if e.Safety == "strict" && e.Sudo {
		return fmt.Errorf(
			"package %q has safety=strict but sudo=true — strict mode uses fakeroot instead of sudo",
			e.Name,
		)
	}

	// Remove lines validation based on safety mode
	if e.Safety == "free" {
		// Free mode requires remove lines since no automatic tracking
		if len(e.RemoveLines) == 0 {
			return fmt.Errorf(
				"package %q has safety=free but no remove commands (remove_begin/remove_end) — free mode requires manual remove commands",
				e.Name,
			)
		}
	}
	// Strict mode: remove lines are optional (auto-generated from macros)

	return nil
}

// Install installs a package from alps-more.
func Install(e *Entry, cfg *config.Config) error {
	// Invalidate sudo credentials before install (security measure)
	priv.Invalidate()

	if e.Sudo {
		if err := ensureSudo(); err != nil {
			return fmt.Errorf("sudo authentication failed: %w", err)
		}
	}

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

// InstallFromGitHub fetches an ALPSMORE file from a GitHub repo and installs it.
// repoPath must be in the form "user/repo".
// Official alps-more entries always take priority — if the package name exists
// in main.txt, that entry is used instead.
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
	// Invalidate sudo credentials before remove (security measure)
	priv.Invalidate()

	// First, remove tracked owned items from installed.json
	rec, isInstalled := GetInstalled(e.Name)
	if isInstalled && len(rec.OwnedItems) > 0 {
		fmt.Printf("  removing %d tracked item(s)...\n", len(rec.OwnedItems))
		if err := RemoveOwnedItems(rec.OwnedItems, e.Sudo); err != nil {
			fmt.Printf("  %s  error removing owned items: %v\n", cfg.Style.SymWarn, err)
		}
	}

	// Then run manual remove lines
	if len(e.RemoveLines) == 0 {
		// If no manual remove lines but we had owned items, we're done
		if isInstalled && len(rec.OwnedItems) > 0 {
			return UnmarkInstalled(e.Name)
		}
		return fmt.Errorf("package %q has no remove commands defined", e.Name)
	}
	if e.Sudo {
		if err := ensureSudo(); err != nil {
			return fmt.Errorf("sudo authentication failed: %w", err)
		}
	}
	server := ""
	if needsMirror(e) {
		var err error
		server, err = resolveServer(e.Servers)
		if err != nil {
			return fmt.Errorf("cannot resolve mirror server for {BASH_RUN}/{SERVER} macros: %w", err)
		}
	}
	if err := runLines(e.Name, e.RemoveLines, server, e.Version); err != nil {
		return err
	}
	return UnmarkInstalled(e.Name)
}

// RemoveOwnedItems safely removes items tracked in owned_items list.
func RemoveOwnedItems(items []OwnedItem, sudo bool) error {
	if len(items) == 0 {
		return nil
	}

	// Process in reverse order for proper cleanup (children before parents)
	for i := len(items) - 1; i >= 0; i-- {
		item := items[i]

		switch item.Type {
		case "file":
			if err := removeFile(item.Path, sudo); err != nil {
				fmt.Printf("  %s  failed to remove file %s: %v\n", "⚠", item.Path, err)
			}
		case "dir":
			if err := removeDir(item.Path, sudo); err != nil {
				fmt.Printf("  %s  failed to remove directory %s: %v\n", "⚠", item.Path, err)
			}
		case "symlink":
			if err := removeSymlink(item.Path, sudo); err != nil {
				fmt.Printf("  %s  failed to remove symlink %s: %v\n", "⚠", item.Path, err)
			}
		case "service":
			if err := removeService(item.Path, sudo); err != nil {
				fmt.Printf("  %s  failed to remove service %s: %v\n", "⚠", item.Path, err)
			}
		case "user":
			if err := removeUser(item.Path, sudo); err != nil {
				fmt.Printf("  %s  failed to remove user %s: %v\n", "⚠", item.Path, err)
			}
		}
	}

	return nil
}

func removeFile(path string, useSudo bool) error {
	var cmd *exec.Cmd
	args := []string{"rm", "-f", path}
	if useSudo && !isTermux() {
		var err error
		cmd, err = priv.Command(args...)
		if err != nil {
			return err
		}
	} else {
		cmd = exec.Command(args[0], args[1:]...)
	}
	return cmd.Run()
}

func removeDir(path string, useSudo bool) error {
	var cmd *exec.Cmd
	args := []string{"rmdir", path}
	if useSudo && !isTermux() {
		var err error
		cmd, err = priv.Command(args...)
		if err != nil {
			return err
		}
	} else {
		cmd = exec.Command(args[0], args[1:]...)
	}
	// Don't fail if directory is not empty
	cmd.Stdout = nil
	cmd.Stderr = nil
	_ = cmd.Run()
	return nil
}

func removeSymlink(path string, useSudo bool) error {
	var cmd *exec.Cmd
	args := []string{"rm", "-f", path}
	if useSudo && !isTermux() {
		var err error
		cmd, err = priv.Command(args...)
		if err != nil {
			return err
		}
	} else {
		cmd = exec.Command(args[0], args[1:]...)
	}
	return cmd.Run()
}

func removeService(service string, useSudo bool) error {
	// Termux has no systemd — nothing to stop/disable/remove.
	if isTermux() {
		return nil
	}

	// Stop and disable the service
	stopCmd := []string{"systemctl", "stop", service}
	disableCmd := []string{"systemctl", "disable", service}

	for _, args := range [][]string{stopCmd, disableCmd} {
		var cmd *exec.Cmd
		var err error
		if useSudo {
			cmd, err = priv.Command(args...)
			if err != nil {
				return err
			}
		} else {
			cmd = exec.Command(args[0], args[1:]...)
		}
		cmd.Stdout = nil
		cmd.Stderr = nil
		_ = cmd.Run() // Don't fail if service doesn't exist
	}

	// Remove the service file if it exists
	serviceFile := "/etc/systemd/system/" + service
	if _, err := os.Stat(serviceFile); err == nil {
		removeFile(serviceFile, useSudo)
	}

	return nil
}

func removeUser(username string, useSudo bool) error {
	var cmd *exec.Cmd
	args := []string{"userdel", username}
	if useSudo && !isTermux() {
		var err error
		cmd, err = priv.Command(args...)
		if err != nil {
			return err
		}
	} else {
		cmd = exec.Command(args[0], args[1:]...)
	}
	cmd.Stdout = nil
	cmd.Stderr = nil
	_ = cmd.Run() // Don't fail if user doesn't exist
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
		Sudo:        rec.Sudo,
		Safety:      rec.Safety,
		RemoveLines: append([]string(nil), rec.RemoveLines...),
		PurgeLines:  append([]string(nil), rec.PurgeLines...),
		Source:      rec.Source,
	}, true, nil
}

// Upgrade upgrades a single package by name.
// Handles both alps-more and GitHub-sourced packages.
func Upgrade(name string, cfg *config.Config) error {
	// Invalidate sudo credentials before upgrade (security measure)
	priv.Invalidate()

	rec, isInstalled := GetInstalled(name)
	if !isInstalled {
		return fmt.Errorf("package %q is not installed via alps-more", name)
	}

	// Remote-sourced (github/gitlab): re-fetch ALPSMORE and compare versions.
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

	if e.Sudo {
		if err := ensureSudo(); err != nil {
			return fmt.Errorf("sudo authentication failed: %w", err)
		}
	}
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
	// Invalidate sudo credentials before upgrade (security measure)
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

	if e.Sudo {
		if err := ensureSudo(); err != nil {
			return fmt.Errorf("sudo authentication failed: %w", err)
		}
	}
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
	// Invalidate sudo credentials before purge (security measure)
	priv.Invalidate()

	e, _, err := RemovalEntry(name, cfg)
	if err != nil {
		return err
	}

	rec, isInstalled := GetInstalled(name)
	if !isInstalled {
		return fmt.Errorf("package %q is not installed via alps-more", name)
	}

	if len(e.RemoveLines) == 0 && len(e.PurgeLines) == 0 && len(rec.OwnedItems) == 0 {
		return fmt.Errorf("package %q has no remove or purge commands defined", e.Name)
	}

	if e.Sudo {
		if err := ensureSudo(); err != nil {
			return fmt.Errorf("sudo authentication failed: %w", err)
		}
	}

	server := ""
	if needsMirror(e) {
		var err error
		server, err = resolveServer(e.Servers)
		if err != nil {
			return fmt.Errorf("cannot resolve mirror server for {BASH_RUN}/{SERVER} macros: %w", err)
		}
	}

	// Step 1: remove tracked owned items (binary, service)
	if len(rec.OwnedItems) > 0 {
		fmt.Printf("  removing %d tracked item(s)...\n", len(rec.OwnedItems))
		if err := RemoveOwnedItems(rec.OwnedItems, e.Sudo); err != nil {
			fmt.Printf("  %s  error removing owned items: %v\n", cfg.Style.SymWarn, err)
		}
	}

	// Step 2: manual remove (binary, service)
	if len(e.RemoveLines) > 0 {
		if err := runLines(e.Name, e.RemoveLines, server, e.Version); err != nil {
			return fmt.Errorf("remove step failed: %w", err)
		}
	}

	// Step 3: purge (configs, data)
	if len(e.PurgeLines) > 0 {
		if err := runLines(e.Name, e.PurgeLines, server, e.Version); err != nil {
			return fmt.Errorf("purge step failed: %w", err)
		}
	}

	return UnmarkInstalled(name)
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

	// Warn if strict mode but fakeroot not available
	if ctx.Safety == "strict" && !hasFakeroot() {
		fmt.Printf("  %s  fakeroot not available - build and file operations may require manual setup\n", cfg.Style.SymWarn)
	}

	// Use context-aware execution for safety mode and macro tracking
	if err := runLinesWithContextMacro(e.Name, e.CmdLines, server, e.Version, e, ctx); err != nil {
		// Generate owned items for cleanup
		ownedItems := GenerateOwnedItems(ctx)

		// Clean up tracked owned items on failure
		if len(ownedItems) > 0 {
			fmt.Printf("  %s  install failed — running cleanup to undo partial install...\n", cfg.Style.SymWarn)
			if rerr := RemoveOwnedItems(ownedItems, e.Sudo); rerr != nil {
				fmt.Printf("  %s  cleanup also failed: %v\n", cfg.Style.SymErr, rerr)
				fmt.Printf("  %s  you may need to clean up manually before retrying.\n", cfg.Style.SymWarn)
			} else {
				fmt.Printf("  %s  cleanup done. Run `alps repo install %s` to retry.\n", cfg.Style.SymOK, e.Name)
			}
		} else {
			fmt.Printf("  %s  no remove_cmd defined — cannot auto-cleanup. Check manually.\n", cfg.Style.SymWarn)
		}
		return err
	}

	// Execute deferred file operations (install macros)
	if err := ExecuteDeferredOps(ctx); err != nil {
		return fmt.Errorf("failed to execute deferred file operations: %w", err)
	}

	// Generate owned items from macro context and save to installed record
	ownedItems := GenerateOwnedItems(ctx)

	return MarkInstalledEntryWithOwnedItems(e, ownedItems)
}

func runUpgrade(e *Entry, cfg *config.Config) error {
	lines := e.UpgradeLines
	if len(lines) == 0 {
		lines = e.CmdLines
	}
	if len(lines) == 0 {
		return fmt.Errorf("package %q has no upgrade or install commands", e.Name)
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

	// Warn if strict mode but fakeroot not available
	if ctx.Safety == "strict" && !hasFakeroot() {
		fmt.Printf("  %s  fakeroot not available - build and file operations may require manual setup\n", cfg.Style.SymWarn)
	}

	// Use context-aware execution for safety mode and macro tracking
	if err := runLinesWithContextMacro(e.Name, lines, server, e.Version, e, ctx); err != nil {
		// Generate owned items for cleanup
		ownedItems := GenerateOwnedItems(ctx)

		// Clean up tracked owned items on failure
		if len(ownedItems) > 0 {
			fmt.Printf("  %s  upgrade failed — running cleanup...\n", cfg.Style.SymWarn)
			if rerr := RemoveOwnedItems(ownedItems, e.Sudo); rerr != nil {
				fmt.Printf("  %s  cleanup also failed: %v\n", cfg.Style.SymErr, rerr)
				fmt.Printf("  %s  you may need to clean up manually before retrying.\n", cfg.Style.SymWarn)
			} else {
				fmt.Printf("  %s  cleanup done. Run `alps repo install %s` to reinstall.\n", cfg.Style.SymOK, e.Name)
			}
		} else {
			fmt.Printf("  %s  no remove_cmd defined — cannot auto-cleanup. Check manually.\n", cfg.Style.SymWarn)
		}
		return err
	}

	// Execute deferred file operations (install macros)
	if err := ExecuteDeferredOps(ctx); err != nil {
		return fmt.Errorf("failed to execute deferred file operations: %w", err)
	}

	// Generate owned items from macro context and save to installed record
	ownedItems := GenerateOwnedItems(ctx)

	return MarkInstalledEntryWithOwnedItems(e, ownedItems)
}

func ensureSudo() error {
	if isTermux() {
		return nil // Termux owns its prefix — no privilege escalation needed
	}
	return priv.EnsureSudoOnly()
}

// needsMirror checks if commands use {BASH_RUN}, {CURL_RUN} (deprecated), or {SERVER}.
func needsMirror(e *Entry) bool {
	for _, lines := range [][]string{e.CmdLines, e.UpgradeLines, e.RemoveLines, e.PurgeLines} {
		for _, l := range lines {
			if strings.Contains(l, "{BASH_RUN}") ||
				strings.Contains(l, "{CURL_RUN}") ||
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

// expandVars replaces variable placeholders in command lines.
// See ALPSMORE.md for macro documentation.
func expandVars(line, server, pkgDir, pkgVersion string) string {
	sysArch := normalizeArch(runtime.GOARCH)
	distro, _ := detectDistro()

	line = strings.ReplaceAll(line, "{ARCH}", sysArch)
	line = strings.ReplaceAll(line, "{OS}", runtime.GOOS)
	line = strings.ReplaceAll(line, "{DISTRO}", distro)
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

	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}

		// Expand variables
		line = expandVars(line, server, pkgDir, pkgVersion)

		// Handle {DOWNLOAD} macro
		if strings.HasPrefix(strings.TrimSpace(line), "{DOWNLOAD}") {
			if err := handleDownloadMacro(line, pkgDir); err != nil {
				return fmt.Errorf("{DOWNLOAD} failed: %w", err)
			}
			continue
		}

		// Handle {BASH_RUN} macro
		if strings.Contains(line, "{BASH_RUN}") {
			var bashErr error
			line, bashErr = handleBashRun(line, server, pkgDir)
			if bashErr != nil {
				return fmt.Errorf("{BASH_RUN} failed: %w", bashErr)
			}
		}

		// Handle {CURL_RUN} deprecated alias
		if strings.Contains(line, "{CURL_RUN}") {
			var bashErr error
			line = strings.ReplaceAll(line, "{CURL_RUN}", "{BASH_RUN}")
			line, bashErr = handleBashRun(line, server, pkgDir)
			if bashErr != nil {
				return fmt.Errorf("{CURL_RUN} failed (deprecated, use {BASH_RUN}): %w", bashErr)
			}
		}

		// Handle new structured macros (INSTALL_BIN, etc.) when context is available
		if ctx != nil {
			_, _, isMacro := ParseMacro(line)
			if isMacro {
				// This is a structured macro, execute it through the macro system
				expanded, err := expandLine(line, ctx)
				if err != nil {
					return fmt.Errorf("macro expansion failed: %w", err)
				}
				if expanded != "" {
					line = expanded
				} else {
					continue // Macro was handled internally
				}
			} else {
				// In strict mode, validate non-macro lines
				if ctx.Safety == "strict" {
					if err := ValidateLine(line); err != nil {
						return fmt.Errorf("security validation failed: %w", err)
					}
				}
			}
		}

		// Execute command in build directory
		// Use fakeroot for build commands in strict mode if available
		var cmd *exec.Cmd
		useFakeroot := false
		if ctx != nil && ctx.Safety == "strict" && hasFakeroot() {
			useFakeroot = true
		} else if ctx == nil && entry != nil && entry.Safety == "strict" && hasFakeroot() {
			useFakeroot = true
		}

		if useFakeroot {
			cmd = exec.Command("fakeroot", "bash", "-c", line)
		} else {
			cmd = exec.Command("bash", "-c", line)
		}
		cmd.Dir = pkgDir
		cmd.Env = append(os.Environ(), "TERM=xterm-256color")
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		cmd.Stdin = os.Stdin
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("command failed: %s\n  error: %w", line, err)
		}
	}
	return nil
}

// isTermux checks if running in Termux.
func isTermux() bool {
	return os.Getenv("TERMUX_VERSION") != "" ||
		os.Getenv("PREFIX") == "/data/data/com.termux/files/usr"
}

// hasFakeroot checks if fakeroot is available
func hasFakeroot() bool {
	_, err := exec.LookPath("fakeroot")
	return err == nil
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

func osMatches(osList []string, distro string, idLike []string) bool {
	for _, o := range osList {
		o = strings.ToLower(strings.TrimSpace(o))
		if o == "linux" {
			if !isTermux() && !isWSL() {
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
