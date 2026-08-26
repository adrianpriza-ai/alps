package more

import (
	"bufio"
	"bytes"
	"fmt"
	"os/exec"
	"runtime"
	"strings"

	"github.com/adrianpriza-ai/alps/platform"
)

// --- ALPSMORE text parsing (from parse.go) ---

// Parse parses a raw ALPSMORE text (a collection of [section] blocks)
// and returns a map of package names to Entry structs.
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

// resolveEntry merges a parsed entry into the map, preferring the entry
// whose OS list matches the current distro when duplicates exist.
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

// parseSectionTag detects cmd_begin/cmd_end/remove_begin/etc. section markers
// and updates the section state booleans. Returns true if the line was consumed.
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

// parseSectionBody appends the line to the appropriate section's command list.
// Returns true if the line was placed in a section.
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

// parseKeyValue parses a "key = value" line and sets the corresponding Entry field.
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

// --- Entry validation (from validate.go) ---

// Validate checks that an entry is compatible with the current system.
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

// validateArchitecture checks that the package supports the current architecture.
func validateArchitecture(e *Entry) error {
	if len(e.Arch) == 0 {
		return fmt.Errorf(
			"package %q has no 'arch' field defined in repo — cannot install safely",
			e.Name,
		)
	}
	sysArch := platform.NormalizeArch(runtime.GOARCH)
	if !containsCI(e.Arch, sysArch) {
		return fmt.Errorf(
			"package %q does not support your architecture (%s)\n  supported: %s",
			e.Name, sysArch, strings.Join(e.Arch, ", "),
		)
	}
	return nil
}

// validateOS checks that the package supports the current OS/distro.
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

// validateDependencies checks that all required dependencies are available.
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

// checkDependencyGroup checks if a dependency group (single or OR-group) is satisfied.
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

// validateInstallCommands checks that install commands are defined.
func validateInstallCommands(e *Entry) error {
	if len(e.CmdLines) == 0 {
		return fmt.Errorf(
			"package %q has no install commands (cmd_begin/cmd_end) defined — cannot install",
			e.Name,
		)
	}
	return nil
}

// validateSafetyMode sets default safety mode if not specified.
func validateSafetyMode(e *Entry) {
	if e.Safety == "" {
		e.Safety = "strict"
	}
}

// validateSafetyRequirements checks that safety mode requirements are met.
func validateSafetyRequirements(e *Entry) error {
	if e.Safety == "free" && len(e.RemoveLines) == 0 {
		return fmt.Errorf(
			"package %q has safety=free but no remove commands (remove_begin/remove_end) — free mode requires manual remove commands",
			e.Name,
		)
	}
	return nil
}

// validatePurgeCommands checks that purge operations have required commands.
func validatePurgeCommands(e *Entry, rec InstalledRecord) error {
	if len(e.RemoveLines) == 0 && len(e.PurgeLines) == 0 && len(rec.OwnedItems) == 0 {
		return fmt.Errorf("package %q has no remove or purge commands defined", e.Name)
	}
	return nil
}
