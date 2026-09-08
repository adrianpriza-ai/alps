package more

import (
	"bufio"
	"bytes"
	"fmt"
	"math"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"

	"github.com/adrianpriza-ai/alps/platform"
)

// --- ALPSMORE text parsing (from parse.go) ---

// Parse parses a raw ALPSMORE text (a collection of [section] blocks)
// and returns a map of package names to Entry structs.
func Parse(data []byte) (map[string]*Entry, error) {
	entries := make(map[string]*Entry)
	var current *Entry
	var inCmd, inRemove, inUpgrade, inPurge, inSums, inSumsBlock bool

	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			if inSumsBlock {
				return nil, fmt.Errorf("unclosed sha256sums block in [%s] (missing sha256sums_end before next section)", current.Name)
			}
			if current != nil {
				resolveEntry(entries, current)
			}
			name := line[1 : len(line)-1]
			current = &Entry{Name: name, Safety: "strict"} // default to strict mode
			inCmd, inRemove, inUpgrade, inPurge, inSums, inSumsBlock = false, false, false, false, false, false
			continue
		}

		if current == nil {
			continue
		}

		if consumed, err := parseSectionTag(line, current, &inCmd, &inRemove, &inUpgrade, &inPurge, &inSumsBlock); consumed {
			if err != nil {
				return nil, err
			}
			inSums = false
			continue
		}

		// Inside a sha256sums_begin/end block every line must be a {FILE} or
		// {SUMS} definition. Any other construct — key=value lines, pasted
		// checksum lines, command text — is rejected so a block can never
		// silently mix checksum declaration styles.
		if inSumsBlock {
			if err := current.parseSumsBlockLine(line); err != nil {
				return nil, err
			}
			continue
		}

		// After a sha256sums key, bare lines may carry one "hash  filename"
		// pair each — sha256sum output pasted verbatim. Any other construct
		// ends the checksum block.
		if inSums {
			if hash, name, ok := parseSumsContinuation(line); ok {
				if err := current.addNamedSum(name, hash); err != nil {
					return nil, err
				}
				continue
			}
			if !strings.Contains(line, "=") {
				return nil, fmt.Errorf("invalid sha256sums continuation line %q (want a 64-hex hash followed by the filename)", line)
			}
			inSums = false
		}

		if parseSectionBody(line, current, inCmd, inRemove, inUpgrade, inPurge) {
			continue
		}

		isSums, err := parseKeyValue(line, current)
		if err != nil {
			return nil, err
		}
		inSums = isSums
	}

	if inSumsBlock {
		return nil, fmt.Errorf("unclosed sha256sums block in [%s] (missing sha256sums_end at end of file)", current.Name)
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
// and updates the section state booleans. sha256sums_begin/sha256sums_end
// delimit the named-checksum block, which may only open at top level and only
// once per entry. Returns true if the line was consumed, plus a parse error
// for invalid checksum-block usage.
func parseSectionTag(line string, e *Entry, inCmd, inRemove, inUpgrade, inPurge, inSumsBlock *bool) (bool, error) {
	switch line {
	case "cmd_begin":
		*inCmd = true
		*inRemove, *inUpgrade, *inPurge, *inSumsBlock = false, false, false, false
		e.pendingSumsFile, e.lastSumsFile = "", ""
	case "cmd_end":
		*inCmd = false
	case "remove_begin":
		*inRemove = true
		*inCmd, *inUpgrade, *inPurge, *inSumsBlock = false, false, false, false
		e.pendingSumsFile, e.lastSumsFile = "", ""
	case "remove_end":
		*inRemove = false
	case "upgrade_begin":
		*inUpgrade = true
		*inCmd, *inRemove, *inPurge, *inSumsBlock = false, false, false, false
		e.pendingSumsFile, e.lastSumsFile = "", ""
	case "upgrade_end":
		*inUpgrade = false
	case "purge_begin":
		*inPurge = true
		*inCmd, *inRemove, *inUpgrade, *inSumsBlock = false, false, false, false
		e.pendingSumsFile, e.lastSumsFile = "", ""
	case "purge_end":
		*inPurge = false
	case "sha256sums_begin":
		if *inCmd || *inRemove || *inUpgrade || *inPurge {
			return true, fmt.Errorf("sha256sums_begin must appear at top level, not inside a cmd/remove/upgrade/purge block")
		}
		if *inSumsBlock {
			return true, fmt.Errorf("duplicate sha256sums_begin in entry %q (previous block was not closed with sha256sums_end)", e.Name)
		}
		if e.usedSumsBlock {
			return true, fmt.Errorf("duplicate sha256sums_begin block in entry %q (only one checksum block per entry)", e.Name)
		}
		if len(e.SHA256ByName) > 0 || len(e.SHA256Sums) > 0 {
			return true, fmt.Errorf("cannot mix sha256sums_begin block with other sha256sums declarations in entry %q", e.Name)
		}
		*inSumsBlock = true
		e.usedSumsBlock = true
	case "sha256sums_end":
		if e.pendingSumsFile != "" {
			return true, fmt.Errorf("orphan {FILE} %q in sha256sums block (missing {SUMS} before sha256sums_end)", e.pendingSumsFile)
		}
		e.lastSumsFile = ""
		*inSumsBlock = false
	default:
		return false, nil
	}
	return true, nil
}

// parseSumsBlockLine handles one line inside a sha256sums_begin/end block.
// {FILE} <name> declares the filename that the next {SUMS} attaches to;
// {SUMS} <64hex> records the digest under the pending filename. Both macros
// must come in pairs — an orphan of either is a parse error, as is any other
// macro or bare line.
func (e *Entry) parseSumsBlockLine(line string) error {
	if !strings.HasPrefix(line, "{") {
		return fmt.Errorf("invalid line in sha256sums block %q (want {FILE} <name> or {SUMS} <64hex>)", line)
	}
	end := strings.Index(line, "}")
	if end == -1 {
		return fmt.Errorf("invalid line in sha256sums block %q (unclosed macro)", line)
	}
	name := strings.ToUpper(strings.TrimSpace(line[1:end]))
	value := strings.TrimSpace(line[end+1:])

	switch name {
	case "FILE":
		if e.pendingSumsFile != "" {
			return fmt.Errorf("orphan {FILE} %q in sha256sums block (missing {SUMS} before the next {FILE})", e.pendingSumsFile)
		}
		filename := stripMatchedQuotes(value)
		if filename == "" {
			return fmt.Errorf("empty {FILE} in sha256sums block")
		}
		e.pendingSumsFile = filename
	case "SUMS":
		if e.pendingSumsFile == "" {
			return fmt.Errorf("orphan {SUMS} in sha256sums block (missing preceding {FILE})")
		}
		if !isValidSha256(value) {
			return fmt.Errorf("invalid {SUMS} %q in sha256sums block (want exactly 64 hex characters)", value)
		}
		if err := e.addNamedSum(e.pendingSumsFile, value); err != nil {
			return err
		}
		e.lastSumsFile = e.pendingSumsFile
		e.pendingSumsFile = ""
	case "SIZE":
		// {SIZE} attaches to the file currently being declared: the pending
		// {FILE} if its {SUMS} has not landed yet, otherwise the last pair.
		target := e.pendingSumsFile
		if target == "" {
			target = e.lastSumsFile
		}
		if target == "" {
			return fmt.Errorf("orphan {SIZE} in sha256sums block (missing preceding {FILE})")
		}
		maxBytes, unlimited, err := parseSizeToken(value)
		if err != nil {
			return err
		}
		if e.SHA256SizeByName == nil {
			e.SHA256SizeByName = make(map[string]int64)
		}
		if unlimited {
			e.SHA256SizeByName[target] = unlimitedDownloadSize
		} else {
			e.SHA256SizeByName[target] = maxBytes
		}
	default:
		return fmt.Errorf("unknown macro {%s} in sha256sums block (want {FILE}, {SUMS} or {SIZE})", name)
	}
	return nil
}

// parseSizeToken parses a {SIZE} value into a byte cap. Accepted forms:
// "unl"/"unlimited" (no cap), "<n>" or "<n>m" (MB, 1024²), "<n>g" (GB,
// 1024³). A positive value must not exceed maxDownloadSize — a per-file cap
// can only tighten the global default, never widen it; the unlimited token is
// the explicit opt-out for files that genuinely need more.
func parseSizeToken(token string) (maxBytes int64, unlimited bool, err error) {
	s := strings.ToLower(strings.TrimSpace(token))
	switch s {
	case "unl", "unlimited":
		return 0, true, nil
	}

	mult := float64(1024 * 1024) // default suffix: MB
	switch {
	case strings.HasSuffix(s, "g"):
		mult = float64(1024 * 1024 * 1024)
		s = strings.TrimSuffix(s, "g")
	case strings.HasSuffix(s, "m"):
		s = strings.TrimSuffix(s, "m")
	}

	v, err := strconv.ParseFloat(s, 64)
	if err != nil || math.IsNaN(v) || math.IsInf(v, 0) || v <= 0 {
		return 0, false, fmt.Errorf("invalid {SIZE} %q (want a positive number of MB or GB, or unl/unlimited)", token)
	}

	bytes := v * mult
	if bytes > float64(maxDownloadSize) {
		return 0, false, fmt.Errorf("{SIZE} %q exceeds the global download cap (%d bytes) — raise the default instead of per-package caps", token, maxDownloadSize)
	}
	return int64(math.Round(bytes)), false, nil
}

// stripMatchedQuotes removes a single matching outer pair of double or single
// quotes from a filename. Unmatched quotes are left untouched.
func stripMatchedQuotes(s string) string {
	if len(s) >= 2 {
		if (s[0] == '"' && s[len(s)-1] == '"') || (s[0] == '\'' && s[len(s)-1] == '\'') {
			return s[1 : len(s)-1]
		}
	}
	return s
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

// parseKeyValue parses a "key = value" line and sets the corresponding Entry
// field. It reports whether the key was "sha256sums" (the caller uses that to
// enable multi-line checksum continuation lines) and any parse error.
func parseKeyValue(line string, e *Entry) (bool, error) {
	idx := strings.Index(line, "=")
	if idx < 0 {
		return false, nil
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
		if err := e.addSHA256Sums(val); err != nil {
			return false, err
		}
		return true, nil
	}
	return false, nil
}

// addSHA256Sums parses the comma-separated value of a sha256sums key into the
// entry. Fields may be bare 64-hex hashes (legacy positional list) or
// name=hash pairs (looked up by destination filename). Mixing the two styles,
// or declaring the same filename twice, is rejected so a typo cannot silently
// weaken verification.
func (e *Entry) addSHA256Sums(val string) error {
	if e.usedSumsBlock {
		return fmt.Errorf("cannot mix sha256sums_begin block with other sha256sums declarations in entry %q", e.Name)
	}
	for _, field := range splitTrim(val) {
		if strings.Contains(field, "=") {
			if len(e.SHA256Sums) > 0 {
				return fmt.Errorf("cannot mix positional and named sha256sums entries (%d positional hashes already declared)", len(e.SHA256Sums))
			}
			idx := strings.Index(field, "=")
			name := strings.TrimSpace(field[:idx])
			hash := strings.TrimSpace(field[idx+1:])
			if name == "" {
				return fmt.Errorf("invalid sha256sums entry %q: missing filename", field)
			}
			if !isValidSha256(hash) {
				return fmt.Errorf("invalid sha256sums entry %q (want exactly 64 hex characters)", field)
			}
			if err := e.addNamedSum(name, hash); err != nil {
				return err
			}
		} else if isValidSha256(field) {
			if len(e.SHA256ByName) > 0 {
				return fmt.Errorf("cannot mix named and positional sha256sums entries (%d named hashes already declared)", len(e.SHA256ByName))
			}
			e.SHA256Sums = append(e.SHA256Sums, field)
		} else {
			return fmt.Errorf("invalid sha256sums entry %q (want a 64-hex hash or name=hash)", field)
		}
	}
	return nil
}

// addNamedSum records a filename → digest mapping, rejecting duplicates.
func (e *Entry) addNamedSum(name, hash string) error {
	if e.SHA256ByName == nil {
		e.SHA256ByName = make(map[string]string)
	}
	if _, exists := e.SHA256ByName[name]; exists {
		return fmt.Errorf("duplicate sha256sums entry for %q", name)
	}
	e.SHA256ByName[name] = hash
	return nil
}

// parseSumsContinuation parses a paste-format checksum line ("hash  filename",
// the output shape of sha256sum) into a filename → digest pair. A trailing '*'
// binary marker from "sha256sum -b" is tolerated. ok is false for anything that
// is not a checksum line so the caller can fall back to normal line handling.
func parseSumsContinuation(line string) (hash, name string, ok bool) {
	fields := strings.Fields(line)
	if len(fields) < 2 {
		return "", "", false
	}
	hash = strings.TrimSuffix(fields[0], "*")
	if !isValidSha256(hash) {
		return "", "", false
	}
	name = strings.Join(fields[1:], " ")
	return hash, name, true
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

	// In strict mode a named checksum that never matches a download means the
	// manifest was edited without updating the checksums, or the filename
	// mapping is wrong. Either way the author should hear about it before the
	// install fails at download time with a confusing "missing checksum" error.
	if e.Safety == "strict" {
		warnUnusedNamedChecksums(e)
	}

	return nil
}

// unusedNamedChecksums returns the destination filenames declared in the
// entry's named sha256sums that no {DOWNLOAD} or {BASH_RUN} in the install or
// upgrade commands ever downloads. Matching mirrors runtime behavior: the FILE
// argument of {DOWNLOAD} if given, otherwise the URL basename; for {BASH_RUN}
// the URL basename. Macros with placeholders that cannot be resolved are
// skipped rather than guessed at.
func unusedNamedChecksums(e *Entry) []string {
	if len(e.SHA256ByName) == 0 {
		return nil
	}
	ctx := NewMacroContext(e, "")
	downloaded := make(map[string]bool)

	collect := func(lines []string) {
		for _, line := range lines {
			macro, _, isMacro := ParseMacro(line)
			if !isMacro {
				continue
			}
			switch macro.Name {
			case "DOWNLOAD":
				if len(macro.Args) == 0 {
					continue
				}
				args := make([]string, len(macro.Args))
				for i, a := range macro.Args {
					args[i] = replaceVars(a, ctx, true)
				}
				dest := filepath.Base(args[0])
				if len(args) > 1 {
					dest = filepath.Base(args[1])
				}
				if strings.Contains(dest, "{") {
					continue // filename depends on an unresolvable placeholder
				}
				downloaded[dest] = true
			case "BASH_RUN":
				if len(macro.Args) == 0 {
					continue
				}
				script := replaceVars(macro.Args[0], ctx, true)
				if !strings.HasPrefix(script, "http://") && !strings.HasPrefix(script, "https://") {
					continue // local script — nothing is downloaded
				}
				base := filepath.Base(script)
				if strings.Contains(base, "{") {
					continue
				}
				downloaded[base] = true
			}
		}
	}
	collect(e.CmdLines)
	collect(e.UpgradeLines)

	var unused []string
	for name := range e.SHA256ByName {
		if !downloaded[name] {
			unused = append(unused, name)
		}
	}
	sort.Strings(unused)
	return unused
}

// warnUnusedNamedChecksums prints a warning for named checksums that never
// match any download. Informational only — validation still passes.
func warnUnusedNamedChecksums(e *Entry) {
	unused := unusedNamedChecksums(e)
	if len(unused) == 0 {
		return
	}
	fmt.Printf("  %s  %s declares sha256sums for files that are never downloaded: %s\n",
		currentStyle().SymWarn, e.Name, strings.Join(unused, ", "))
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
