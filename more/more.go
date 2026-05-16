package more

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"

	"github.com/adrianpriza-ai/alps/priv"
)

// Entry represents a single package entry from main.txt.
type Entry struct {
	Name         string
	Desc         string
	Author       string   // optional
	Version      string   // optional
	Arch         []string // required, no field = error
	OS           []string // "linux" = all, or specific distros
	Deps         []string // optional
	Sudo         bool     // optional, run ensureSudo() once before cmds
	CmdLines     []string
	RemoveLines  []string
	UpgradeLines []string
}

// Parse parses main.txt content into a map of entries.
func Parse(data []byte) (map[string]*Entry, error) {
	entries := make(map[string]*Entry)
	var current *Entry
	var inCmd, inRemove, inUpgrade bool

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
			inCmd, inRemove, inUpgrade = false, false, false
			continue
		}

		if current == nil {
			continue
		}

		switch {
		case line == "cmd_begin":
			inCmd = true
			inRemove, inUpgrade = false, false
		case line == "cmd_end":
			inCmd = false
		case line == "remove_begin":
			inRemove = true
			inCmd, inUpgrade = false, false
		case line == "remove_end":
			inRemove = false
		case line == "upgrade_begin":
			inUpgrade = true
			inCmd, inRemove = false, false
		case line == "upgrade_end":
			inUpgrade = false
		case inCmd:
			current.CmdLines = append(current.CmdLines, line)
		case inRemove:
			current.RemoveLines = append(current.RemoveLines, line)
		case inUpgrade:
			current.UpgradeLines = append(current.UpgradeLines, line)
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
func Find(name string) (*Entry, error) {
	exists, expired := CacheStatus()
	if !exists {
		return nil, fmt.Errorf("no cache found, run: alps repo update")
	}
	if expired {
		fmt.Printf("  %s  repo cache is expired (>90 days). Using old cache.\n", symWarn())
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
func List() (map[string]*Entry, error) {
	exists, expired := CacheStatus()
	if !exists {
		return nil, fmt.Errorf("no cache found, run: alps repo update")
	}
	if expired {
		fmt.Printf("  %s  repo cache is expired (>90 days). Using old cache.\n", symWarn())
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
func Search(query string) ([]*Entry, error) {
	entries, err := List()
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
//   - Not installed         → install
//   - Installed, new version available → upgrade (upgrade_cmd or fallback to install cmd)
//   - Installed, same/no version       → reinstall
func Install(e *Entry) error {
	if e.Sudo {
		if err := ensureSudo(); err != nil {
			return fmt.Errorf("sudo authentication failed: %w", err)
		}
	}

	rec, isInstalled := GetInstalled(e.Name)

	if isInstalled {
		if e.Version != "" && rec.Version != "" && e.Version != rec.Version {
			fmt.Printf("  %s  %s: %s -> %s\n",
				symUpgrade(), e.Name, rec.Version, e.Version)
			return runUpgrade(e)
		}

		if e.Version != "" && rec.Version != "" {
			fmt.Printf("  %s  %s %s already up to date. Reinstalling...\n",
				symReinstall(), e.Name, e.Version)
		} else {
			fmt.Printf("  %s  %s already installed. Reinstalling...\n",
				symReinstall(), e.Name)
		}
	}

	return runInstall(e)
}

// Remove runs the remove lines for a package.
func Remove(e *Entry) error {
	if len(e.RemoveLines) == 0 {
		return fmt.Errorf("package %q has no remove commands defined", e.Name)
	}
	if e.Sudo {
		if err := ensureSudo(); err != nil {
			return fmt.Errorf("sudo authentication failed: %w", err)
		}
	}
	return runLines(e.RemoveLines)
}

// Upgrade upgrades a single installed package if a newer version is available.
func Upgrade(name string) error {
	e, err := Find(name)
	if err != nil {
		return err
	}

	rec, isInstalled := GetInstalled(name)
	if !isInstalled {
		return fmt.Errorf("package %q is not installed via alps-more", name)
	}

	if e.Version == "" || rec.Version == "" {
		fmt.Printf("  %s  %s: no version info, reinstalling...\n", symReinstall(), name)
		return runInstall(e)
	}

	if e.Version == rec.Version {
		fmt.Printf("  ok  %s %s is already up to date.\n", name, e.Version)
		return nil
	}

	fmt.Printf("  %s  %s: %s -> %s\n", symUpgrade(), name, rec.Version, e.Version)

	if e.Sudo {
		if err := ensureSudo(); err != nil {
			return fmt.Errorf("sudo authentication failed: %w", err)
		}
	}
	return runUpgrade(e)
}

// UpgradeAll upgrades all packages tracked in installed.json.
// Stale entries (removed from repo) are reported but not removed automatically.
func UpgradeAll() error {
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
		e, err := Find(name)
		if err != nil {
			// Package no longer exists in repo — stale entry
			if strings.Contains(err.Error(), "not found in alps-more repo") {
				fmt.Printf("  %s  %s: no longer in repo (stale) — skipping\n", symWarn(), name)
				fmt.Printf("       to remove: alps repo remove %s\n", name)
				stale++
			} else {
				fmt.Printf("  %s  %s: %v\n", symErr(), name, err)
				failed++
			}
			continue
		}

		rec := records[name]
		if e.Version != "" && rec.Version != "" && e.Version == rec.Version {
			fmt.Printf("  %s  %s %s\n", symOK(), name, e.Version)
			upToDate++
			continue
		}

		if err := Upgrade(name); err != nil {
			fmt.Printf("  %s  %s: %v\n", symErr(), name, err)
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

// --- internal helpers ---

func runInstall(e *Entry) error {
	if len(e.CmdLines) == 0 {
		return fmt.Errorf("package %q has no install commands", e.Name)
	}
	if err := runLines(e.CmdLines); err != nil {
		// Install failed — auto-cleanup so the system is not left in a broken state.
		// User can then retry with `alps repo install <pkg>` cleanly.
		if len(e.RemoveLines) > 0 {
			fmt.Printf("  %s  install failed — running cleanup to undo partial install...\n", symWarn())
			if rerr := runLines(e.RemoveLines); rerr != nil {
				fmt.Printf("  %s  cleanup also failed: %v\n", symErr(), rerr)
				fmt.Printf("  %s  you may need to clean up manually before retrying.\n", symWarn())
			} else {
				fmt.Printf("  %s  cleanup done. Run `alps repo install %s` to retry.\n", symOK(), e.Name)
			}
		} else {
			fmt.Printf("  %s  no remove_cmd defined — cannot auto-cleanup. Check manually.\n", symWarn())
		}
		return err
	}
	return MarkInstalled(e.Name, e.Version)
}

func runUpgrade(e *Entry) error {
	lines := e.UpgradeLines
	if len(lines) == 0 {
		lines = e.CmdLines
	}
	if len(lines) == 0 {
		return fmt.Errorf("package %q has no upgrade or install commands", e.Name)
	}
	if err := runLines(lines); err != nil {
		// Upgrade failed — run remove so user can do a clean reinstall.
		if len(e.RemoveLines) > 0 {
			fmt.Printf("  %s  upgrade failed — running cleanup...\n", symWarn())
			if rerr := runLines(e.RemoveLines); rerr != nil {
				fmt.Printf("  %s  cleanup also failed: %v\n", symErr(), rerr)
				fmt.Printf("  %s  you may need to clean up manually before retrying.\n", symWarn())
			} else {
				fmt.Printf("  %s  cleanup done. Run `alps repo install %s` to reinstall.\n", symOK(), e.Name)
			}
		} else {
			fmt.Printf("  %s  no remove_cmd defined — cannot auto-cleanup. Check manually.\n", symWarn())
		}
		return err
	}
	return MarkInstalled(e.Name, e.Version)
}

func ensureSudo() error {
	return priv.Ensure()
}

func runLines(lines []string) error {
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
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

// --- tty-aware helpers ---

func isTTY() bool {
	term := os.Getenv("TERM")
	return term == "linux" || term == "dumb" || term == ""
}

func symUpgrade() string {
	if isTTY() {
		return "->"
	}
	return "↑"
}

func symReinstall() string {
	if isTTY() {
		return ">>"
	}
	return "⟳"
}

func symOK() string {
	if isTTY() {
		return "ok"
	}
	return "✓"
}

func symErr() string {
	if isTTY() {
		return "!!"
	}
	return "✗"
}

func symWarn() string {
	if isTTY() {
		return "!!"
	}
	return "⚠"
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
