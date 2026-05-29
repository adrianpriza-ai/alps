package more

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"

	"github.com/adrianpriza-ai/alps/config"
	"github.com/adrianpriza-ai/alps/priv"
)

// Entry represents a single package entry from main.txt.
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
}

// Parse parses main.txt content into a map of entries.
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

// Find looks up a package by name from cache, matching current distro.
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

// List returns all entries filtered to the current distro.
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
	return filtered, nil
}

// Search returns entries whose name or desc contains query (case-insensitive).
// Filters to current distro, like pacman -Ss.
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

// Validate checks arch, os, and deps compatibility.
func Validate(e *Entry) error {
	// --- arch check (required) ---
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

	// --- os/distro check ---
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

	// --- deps check ---
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

// Install installs a package, handling already-installed and upgrade cases.
//
//   - Not installed                    → install
//   - Installed, new version available → upgrade (upgrade_cmd or fallback to install cmd)
//   - Installed, same/no version       → reinstall
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

// Remove runs the remove lines for a package.
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
	if needsCurlRun(e) {
		var err error
		server, err = resolveServer(e.Servers)
		if err != nil {
			return fmt.Errorf("cannot resolve mirror for {CURL_RUN}: %w", err)
		}
	}
	return runLines(e.RemoveLines, server)
}

// Upgrade upgrades a single installed package if a newer version is available.
func Upgrade(name string, cfg *config.Config) error {
	e, err := Find(name, cfg)
	if err != nil {
		return err
	}

	rec, isInstalled := GetInstalled(name)
	if !isInstalled {
		return fmt.Errorf("package %q is not installed via alps-more", name)
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

// UpgradeAll upgrades all packages tracked in installed.json.
// Stale entries (removed from repo) are reported but not removed automatically.
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

		rec := records[name]
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

// Purge removes a package and deletes its config/data files.
// Runs remove_begin first (uninstall binary/service),
// then purge_begin (delete configs/user data).
// Falls back to remove-only if no purge_begin is defined.
func Purge(name string, cfg *config.Config) error {
	e, err := Find(name, cfg)
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
	if needsCurlRun(e) {
		var err error
		server, err = resolveServer(e.Servers)
		if err != nil {
			return fmt.Errorf("cannot resolve mirror for {CURL_RUN}: %w", err)
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

// --- internal helpers ---

func runInstall(e *Entry, cfg *config.Config) error {
	if len(e.CmdLines) == 0 {
		return fmt.Errorf("package %q has no install commands", e.Name)
	}

	server := ""
	if needsCurlRun(e) {
		var err error
		server, err = resolveServer(e.Servers)
		if err != nil {
			return fmt.Errorf("cannot resolve mirror for {CURL_RUN}: %w", err)
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
	return MarkInstalled(e.Name, e.Version)
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
	if needsCurlRun(e) {
		var err error
		server, err = resolveServer(e.Servers)
		if err != nil {
			return fmt.Errorf("cannot resolve mirror for {CURL_RUN}: %w", err)
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
	return MarkInstalled(e.Name, e.Version)
}

func ensureSudo() error {
	if isTermux() {
		return nil // Termux owns its prefix — no privilege escalation needed
	}
	return priv.Ensure()
}

// needsCurlRun reports whether any command in the entry uses the {CURL_RUN} macro.
func needsCurlRun(e *Entry) bool {
	for _, lines := range [][]string{e.CmdLines, e.UpgradeLines, e.RemoveLines, e.PurgeLines} {
		for _, l := range lines {
			if strings.Contains(l, "{CURL_RUN}") {
				return true
			}
		}
	}
	return false
}

// runLines executes a slice of shell commands in order.
// {CURL_RUN}<path> is expanded to: curl -fsSL <resolved_server><path> | sh
// The server URL comes from resolveServer; if the entry has no servers= field
// it automatically falls back to the default alps-more mirrors.
func runLines(lines []string, server string) error {
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		if server != "" && strings.Contains(line, "{CURL_RUN}") {
			line = strings.ReplaceAll(line, "{CURL_RUN}", "curl -fsSL "+server)
			line = strings.TrimRight(line, " \t") + " | sh"
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

// isTermux returns true when running inside Termux on Android.
func isTermux() bool {
	return os.Getenv("TERMUX_VERSION") != "" ||
		os.Getenv("PREFIX") == "/data/data/com.termux/files/usr"
}

// isWSL returns true when running inside Windows Subsystem for Linux.
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
			return true
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
