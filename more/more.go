package more

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"sort"
	"strings"

	"github.com/adrianpriza-ai/alps/config"
	"github.com/adrianpriza-ai/alps/priv"
)

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
			current = &Entry{Name: name}
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

	return nil
}

// Install installs a package from alps-more.
func Install(e *Entry, cfg *config.Config) error {
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
	if len(e.RemoveLines) == 0 {
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
			return fmt.Errorf("cannot resolve mirror for {CURL_RUN}/{SERVER}: %w", err)
		}
	}
	return runLines(e.RemoveLines, server)
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
		RemoveLines: append([]string(nil), rec.RemoveLines...),
		PurgeLines:  append([]string(nil), rec.PurgeLines...),
		Source:      rec.Source,
	}, true, nil
}

// Upgrade upgrades a single package by name.
// Handles both alps-more and GitHub-sourced packages.
func Upgrade(name string, cfg *config.Config) error {
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

// isRemoteSource returns true if the source string refers to a known remote
// provider (currently github and gitlab).
func isRemoteSource(source string) bool {
	return strings.HasPrefix(source, "github:") || strings.HasPrefix(source, "gitlab:")
}

// UpgradeFromSource re-fetches an ALPSMORE file from a remote source string
// ("github:user/repo" or "gitlab:user/repo") and upgrades if newer.
func UpgradeFromSource(name, source string, cfg *config.Config) error {
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
	e, _, err := RemovalEntry(name, cfg)
	if err != nil {
		return err
	}

	_, isInstalled := GetInstalled(name)
	if !isInstalled {
		return fmt.Errorf("package %q is not installed via alps-more", name)
	}

	if len(e.RemoveLines) == 0 && len(e.PurgeLines) == 0 {
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
			return fmt.Errorf("cannot resolve mirror for {CURL_RUN}/{SERVER}: %w", err)
		}
	}

	// Step 1: remove (binary, service)
	if len(e.RemoveLines) > 0 {
		if err := runLines(e.RemoveLines, server); err != nil {
			return fmt.Errorf("remove step failed: %w", err)
		}
	}

	// Step 2: purge (configs, data)
	if len(e.PurgeLines) > 0 {
		if err := runLines(e.PurgeLines, server); err != nil {
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
			return fmt.Errorf("cannot resolve mirror for {CURL_RUN}/{SERVER}: %w", err)
		}
	}

	if err := runLines(e.CmdLines, server); err != nil {
		if len(e.RemoveLines) > 0 {
			fmt.Printf("  %s  install failed — running cleanup to undo partial install...\n", cfg.Style.SymWarn)
			if rerr := runLines(e.RemoveLines, server); rerr != nil {
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
	return MarkInstalledEntry(e)
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
			return fmt.Errorf("cannot resolve mirror for {CURL_RUN}/{SERVER}: %w", err)
		}
	}

	if err := runLines(lines, server); err != nil {
		if len(e.RemoveLines) > 0 {
			fmt.Printf("  %s  upgrade failed — running cleanup...\n", cfg.Style.SymWarn)
			if rerr := runLines(e.RemoveLines, server); rerr != nil {
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
	return MarkInstalledEntry(e)
}

func ensureSudo() error {
	if isTermux() {
		return nil // Termux owns its prefix — no privilege escalation needed
	}
	return priv.EnsureSudoOnly()
}

// needsMirror checks if commands use {CURL_RUN} or {SERVER}.
func needsMirror(e *Entry) bool {
	for _, lines := range [][]string{e.CmdLines, e.UpgradeLines, e.RemoveLines, e.PurgeLines} {
		for _, l := range lines {
			if strings.Contains(l, "{CURL_RUN}") || strings.Contains(l, "{SERVER}") {
				return true
			}
		}
	}
	return false
}

// runLines executes shell commands.
func runLines(lines []string, server string) error {
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		if server != "" {
			if strings.Contains(line, "{CURL_RUN}") {
				line = strings.ReplaceAll(line, "{CURL_RUN}", "curl -fsSL "+server)
				line = strings.TrimRight(line, " \t") + " | sh"
			}
			line = strings.ReplaceAll(line, "{SERVER}", server)
		}
		cmd := exec.Command("bash", "-c", line)
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
			return !isTermux() && !isWSL()
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
