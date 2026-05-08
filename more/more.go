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
	Name        string
	Desc        string
	Arch        []string // required, no field = error
	OS          []string // "linux" = all, or specific distros
	Deps        []string // optional
	Sudo        bool     // optional, run ensureSudo() once before cmds
	CmdLines    []string
	RemoveLines []string
}

// Parse parses main.txt content into a map of entries.
func Parse(data []byte) (map[string]*Entry, error) {
	entries := make(map[string]*Entry)
	var current *Entry
	var inCmd, inRemove bool

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
			inCmd, inRemove = false, false
			continue
		}

		if current == nil {
			continue
		}

		if handleBlockToggle(line, &inCmd, &inRemove) {
			continue
		}
		if inCmd {
			current.CmdLines = append(current.CmdLines, line)
			continue
		}
		if inRemove {
			current.RemoveLines = append(current.RemoveLines, line)
			continue
		}
		parseKeyValue(line, current)
	}

	// Save last entry
	if current != nil {
		entries[current.Name] = current
	}

	return entries, scanner.Err()
}

func handleBlockToggle(line string, inCmd, inRemove *bool) bool {
	switch line {
	case "cmd_begin":
		*inCmd = true
		*inRemove = false
	case "cmd_end":
		*inCmd = false
	case "remove_begin":
		*inRemove = true
		*inCmd = false
	case "remove_end":
		*inRemove = false
	default:
		return false
	}
	return true
}

func parseKeyValue(line string, entry *Entry) {
	idx := strings.Index(line, "=")
	if idx < 0 {
		return
	}
	key := strings.TrimSpace(strings.ToLower(line[:idx]))
	val := strings.TrimSpace(line[idx+1:])

	switch key {
	case "desc":
		entry.Desc = val
	case "arch":
		entry.Arch = splitTrim(val)
	case "os":
		entry.OS = splitTrim(val)
	case "deps":
		entry.Deps = splitTrim(val)
	case "sudo":
		entry.Sudo = strings.ToLower(val) == "true"
	}
}

// baseName returns the package name without @distro suffix.
// e.g. "ollama@arch" → "ollama", "ollama" → "ollama"
func baseName(name string) string {
	if idx := strings.Index(name, "@"); idx != -1 {
		return name[:idx]
	}
	return name
}

// Find looks up a package by name from cache, matching current distro.
// Supports @distro suffix: "ollama@arch", "ollama@debian".
// User always types the base name e.g. "ollama".
func Find(name string) (*Entry, error) {
	exists, expired := CacheStatus()
	if !exists {
		return nil, fmt.Errorf("no cache found, run: alps repo update")
	}
	if expired {
		fmt.Println("  \033[33m⚠  repo cache is expired (>90 days). Using old cache.\033[0m")
		fmt.Println("     Run 'alps repo update' to refresh.")
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

	// Collect all entries whose base name matches
	var candidates []*Entry
	for _, e := range entries {
		if baseName(e.Name) == name {
			candidates = append(candidates, e)
		}
	}

	if len(candidates) == 0 {
		return nil, fmt.Errorf("package %q not found in alps-more repo", name)
	}

	// Filter by os match
	for _, e := range candidates {
		if osMatches(e.OS, distro, distroLike) {
			return e, nil
		}
	}

	// No os match found — collect supported distros for error message
	var supported []string
	for _, e := range candidates {
		supported = append(supported, strings.Join(e.OS, "/"))
	}
	return nil, fmt.Errorf(
		"package %q is not available for your distro (%s)\n  available for: %s",
		name, distro, strings.Join(supported, ", "),
	)
}

// List returns deduplicated entries — one per base name, preferring current distro.
func List() (map[string]*Entry, error) {
	exists, expired := CacheStatus()
	if !exists {
		return nil, fmt.Errorf("no cache found, run: alps repo update")
	}
	if expired {
		fmt.Println("  \033[33m⚠  repo cache is expired (>90 days). Using old cache.\033[0m")
		fmt.Println("     Run 'alps repo update' to refresh.")
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

	// Group by base name, prefer distro match
	deduped := make(map[string]*Entry)
	for _, e := range all {
		base := baseName(e.Name)
		existing, seen := deduped[base]
		if !seen {
			deduped[base] = e
			continue
		}
		// Prefer the one that matches current distro
		if osMatches(e.OS, distro, distroLike) && !osMatches(existing.OS, distro, distroLike) {
			deduped[base] = e
		}
	}
	return deduped, nil
}

// Validate checks arch, os, and deps compatibility.
// Returns a descriptive error if any check fails.
func Validate(e *Entry) error {
	// --- arch check (required) ---
	if len(e.Arch) == 0 {
		return fmt.Errorf(
			"package %q has no 'arch' field defined in repo — cannot install safely",
			e.Name,
		)
	}
	sysArch := runtime.GOARCH
	// Normalize: GOARCH uses amd64/arm64, uname uses x86_64/aarch64
	sysArch = normalizeArch(sysArch)
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

	// --- deps check (optional field, but if present must all exist) ---
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

// Install runs the cmd_begin...cmd_end lines for a package.
func Install(e *Entry) error {
	if len(e.CmdLines) == 0 {
		return fmt.Errorf("package %q has no install commands", e.Name)
	}
	if e.Sudo {
		if err := ensureSudo(); err != nil {
			return fmt.Errorf("sudo authentication failed: %w", err)
		}
	}
	return runLines(e.CmdLines)
}

// Remove runs the remove_begin...remove_end lines for a package.
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

// ensureSudo ensures privilege escalation is available.
func ensureSudo() error {
	return priv.Ensure()
}

// runLines executes each line via bash, stopping immediately on error.
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

// --- helpers ---

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

// normalizeArch maps GOARCH values to uname -m style names.
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

// detectDistro reads /etc/os-release and returns (ID, ID_LIKE).
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

// osMatches checks if the entry's os list matches this system.
func osMatches(osList []string, distro string, idLike []string) bool {
	for _, o := range osList {
		o = strings.ToLower(strings.TrimSpace(o))
		switch o {
		case "linux":
			return true // all linux distros
		default:
			if strings.ToLower(distro) == o {
				return true
			}
			for _, like := range idLike {
				if strings.ToLower(like) == o {
					return true
				}
			}
		}
	}
	return false
}
