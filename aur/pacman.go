package aur

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"

	"github.com/adrianpriza-ai/alps/priv"
)

// checkRequirements verifies that required tools are available.
// It uses pacman -Qq (package DB) as the primary check, with exec.LookPath
// as a fallback — OR logic means either one confirming presence is sufficient.
// For base-devel: it has been a real package since late 2022; we also fall
// back to checking "fakeroot" in PATH as a secondary sentinel.
func checkRequirements(needGit, needBaseDevel bool) error {
	var missing []string
	if needGit {
		if !pkgInstalled("git") && !hasInPath("git") {
			missing = append(missing, "git")
		}
	}
	if needBaseDevel {
		// base-devel is a proper package since Arch 2022; fakeroot is the fallback sentinel.
		if !pkgInstalled("base-devel") && !hasInPath("fakeroot") {
			missing = append(missing, "base-devel")
		}
	}
	if len(missing) == 0 {
		return nil
	}
	installHint := "sudo pacman -S " + strings.Join(missing, " ")
	return fmt.Errorf(
		"missing required tools: %s\n  Run: %s",
		strings.Join(missing, ", "),
		installHint,
	)
}

// pkgInstalled reports whether a package is registered in the pacman DB.
func pkgInstalled(name string) bool {
	return exec.Command("pacman", "-Qq", name).Run() == nil
}

// hasInPath reports whether a binary is reachable via PATH.
func hasInPath(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

// pkgCache caches pacman query results to avoid redundant shell-outs.
// Each lookup uses a lock-release-acquire pattern: read the map under
// the lock, release it while running the expensive pacman subprocess,
// then re-lock to publish the result. This is the standard
// "double-checked locking" pattern in Go and is safe because the map
// is only ever written under the lock.
//
// NOTE: newPkgCache() is called fresh in buildInstallPlan, BuildLocal,
// and checkDeps, so cross-invocation caching does not happen. If two
// packages share deps, the second runs pacman -Qi/-Si for the same
// names again. Hoisting the cache to the package level (or to aurCache)
// would fix this, but contention is low in practice.
type pkgCache struct {
	mu        sync.Mutex
	installed map[string]bool
	inRepo    map[string]bool
	provided  map[string]bool
}

func newPkgCache() *pkgCache {
	return &pkgCache{
		installed: make(map[string]bool),
		inRepo:    make(map[string]bool),
		provided:  make(map[string]bool),
	}
}

// IsInstalled returns whether name is installed, caching the result.
// The lock is held only for map reads/writes; the pacman subprocess
// runs without the lock to avoid blocking concurrent goroutines.
func (c *pkgCache) IsInstalled(name string) bool {
	c.mu.Lock()
	if v, ok := c.installed[name]; ok {
		c.mu.Unlock()
		return v
	}
	c.mu.Unlock()
	v := isInstalled(name)
	c.mu.Lock()
	c.installed[name] = v
	c.mu.Unlock()
	return v
}

// InRepo returns whether name is in the official pacman repositories.
func (c *pkgCache) InRepo(name string) bool {
	c.mu.Lock()
	if v, ok := c.inRepo[name]; ok {
		c.mu.Unlock()
		return v
	}
	c.mu.Unlock()
	v := inPacmanRepo(name)
	c.mu.Lock()
	c.inRepo[name] = v
	c.mu.Unlock()
	return v
}

// HasProvider returns whether name is satisfied by any installed package.
func (c *pkgCache) HasProvider(name string) bool {
	c.mu.Lock()
	if v, ok := c.provided[name]; ok {
		c.mu.Unlock()
		return v
	}
	c.mu.Unlock()
	v := hasProvider(name)
	c.mu.Lock()
	c.provided[name] = v
	c.mu.Unlock()
	return v
}

// unsatisfiedDeps finds unmet dependencies.
func unsatisfiedDeps(deps []string) []string {
	if len(deps) == 0 {
		return nil
	}
	stripped := make([]string, len(deps))
	for i, d := range deps {
		stripped[i] = stripVerConstraint(d)
	}
	out, err := exec.Command("pacman", append([]string{"-T"}, stripped...)...).CombinedOutput()
	if err == nil {
		return nil // all satisfied
	}
	var result []string
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			result = append(result, line)
		}
	}
	return result
}

// hasProvider checks if a dep is satisfied.
func hasProvider(dep string) bool {
	return exec.Command("pacman", "-T", dep).Run() == nil
}

// checkDeps checks dependencies.
func checkDeps(pkg *Package) (missingRepo []string, aurOnly []string, err error) {
	cache := newPkgCache()
	allDeps := append(pkg.Depends, pkg.MakeDepends...)
	for _, dep := range unsatisfiedDeps(allDeps) {
		name := stripVerConstraint(dep)
		if cache.InRepo(name) {
			missingRepo = append(missingRepo, name)
			continue
		}
		if Exists(name) {
			aurOnly = append(aurOnly, name)
		} else {
			err = fmt.Errorf("dep %q not found anywhere", name)
			return
		}
	}
	return
}

func isInstalled(name string) bool {
	return exec.Command("pacman", "-Qi", name).Run() == nil
}

// parsePacmanQLine splits a single "name version" line from `pacman -Q`
// output. Returns name and version, or empty strings if the line is empty
// or malformed.
func parsePacmanQLine(line string) (name, version string) {
	parts := strings.Fields(strings.TrimSpace(line))
	if len(parts) < 2 {
		return "", ""
	}
	return parts[0], parts[1]
}

// pkgInstalledVersion returns the installed version of a package, or an empty string if not installed.
// Uses pacman -Q (not -Qi) to get the raw name-version string.
func pkgInstalledVersion(name string) string {
	out, err := exec.Command("pacman", "-Q", name).Output()
	if err != nil {
		return ""
	}
	_, ver := parsePacmanQLine(string(out))
	return ver
}

// Vercmp compares two Arch Linux version strings using the vercmp binary.
// Returns 1 if a > b, 0 if equal, -1 if a < b.
// Falls back to a pure-Go Arch version comparator when vercmp is not
// installed (containers, CI, non-Arch hosts).
func Vercmp(a, b string) int {
	out, err := exec.Command("vercmp", a, b).Output()
	if err == nil {
		switch strings.TrimSpace(string(out)) {
		case "1":
			return 1
		case "0":
			return 0
		default:
			return -1
		}
	}
	// Pure-Go fallback: parse epoch:pkgver-pkgrel and compare segment by segment.
	return vercmpFallback(a, b)
}

// vercmp is the unexported wrapper kept for internal callers within this package.
func vercmp(a, b string) int { return Vercmp(a, b) }

// --- Pure-Go Arch Linux version comparison (fallback) ---
//
// Arch version format: [epoch:]pkgver[-pkgrel]
//   epoch  : integer (default 0)
//   pkgver : alphanumeric segments ("1.0.1" → [1, 0, 1])
//   pkgrel : integer (default 1)
//
// Comparison order: epoch → pkgver → pkgrel.
// Within pkgver, each segment is compared: numeric segments as integers,
// alphabetic segments lexicographically.  A longer common prefix is newer
// (e.g. 1.0.1 > 1.0).  Digits sort before letters (1.0a < 1.0.1).

// splitVersion splits a version string into epoch, pkgver, pkgrel.
// Missing epoch defaults to 0; missing pkgrel defaults to "1".
func splitVersion(v string) (epoch int, pkgver, pkgrel string) {
	epoch = 0
	pkgrel = "1"

	// Strip leading/trailing whitespace
	v = strings.TrimSpace(v)

	// Epoch: everything before the first ':'.
	// Sscanf error is intentionally ignored: non-numeric epochs (e.g.
	// "abc:1.0") leave epoch=0, matching pacman's lenient tolerance.
	if idx := strings.Index(v, ":"); idx >= 0 {
		fmt.Sscanf(v[:idx], "%d", &epoch)
		v = v[idx+1:]
	}

	// pkgrel: everything after the last '-'
	if idx := strings.LastIndex(v, "-"); idx >= 0 {
		pkgrel = v[idx+1:]
		pkgver = v[:idx]
	} else {
		pkgver = v
	}

	if pkgrel == "" {
		pkgrel = "1"
	}
	return
}

// compareVersions is a pure-Go Arch version comparator used when the vercmp
// binary is unavailable.  It handles epoch, pkgver segments, and pkgrel.
func compareVersions(a, b string) int {
	epochA, pkgverA, pkgrelA := splitVersion(a)
	epochB, pkgverB, pkgrelB := splitVersion(b)

	// 1. Epoch comparison
	if epochA > epochB {
		return 1
	}
	if epochA < epochB {
		return -1
	}

	// 2. pkgver comparison (segment by segment)
	if c := compareSegments(pkgverA, pkgverB); c != 0 {
		return c
	}

	// 3. pkgrel comparison (segment by segment)
	if c := compareSegments(pkgrelA, pkgrelB); c != 0 {
		return c
	}
	return 0
}

// compareSegments splits two pkgver strings into alphanumeric tokens and
// compares them pairwise. Digits sort before letters.
// When one string ends, if the other has extra numeric digits it is newer (1.0.1 > 1.0),
// but if the other has extra alpha characters (e.g. rc, pre, alpha) the ended version
// is the final release and thus newer (1.0 > 1.0rc1).
func compareSegments(a, b string) int {
	aTokens := tokenizeVersion(a)
	bTokens := tokenizeVersion(b)

	maxLen := len(aTokens)
	if len(bTokens) > maxLen {
		maxLen = len(bTokens)
	}

	for i := 0; i < maxLen; i++ {
		var aVal string
		var bVal string
		if i < len(aTokens) {
			aVal = aTokens[i]
		}
		if i < len(bTokens) {
			bVal = bTokens[i]
		}

		// Missing segment check:
		if aVal == "" {
			if isDigit(bVal[0]) {
				return -1 // b has extra numeric subversion -> b is newer (1.0 vs 1.0.1)
			}
			return 1 // b has extra alpha prerelease tag -> a is final release, so a is newer (1.0 vs 1.0rc1)
		}
		if bVal == "" {
			if isDigit(aVal[0]) {
				return 1 // a has extra numeric subversion -> a is newer (1.0.1 vs 1.0)
			}
			return -1 // a has extra alpha prerelease tag -> b is final release, so b is newer (1.0rc1 vs 1.0)
		}

		aIsDigit := isDigit(aVal[0])
		bIsDigit := isDigit(bVal[0])

		// Digits always sort before letters
		if aIsDigit && !bIsDigit {
			return 1
		}
		if !aIsDigit && bIsDigit {
			return -1
		}

		if aIsDigit {
			// Both numeric: compare as integers
			var aNum, bNum int
			fmt.Sscanf(aVal, "%d", &aNum)
			fmt.Sscanf(bVal, "%d", &bNum)
			if aNum > bNum {
				return 1
			}
			if aNum < bNum {
				return -1
			}
			// Equal numeric values: longer digit string is newer (e.g. 1.0.01 > 1.0.1)
			if len(aVal) > len(bVal) {
				return 1
			}
			if len(aVal) < len(bVal) {
				return -1
			}
		} else {
			// Both alphabetic: lexicographic comparison
			if aVal > bVal {
				return 1
			}
			if aVal < bVal {
				return -1
			}
		}
	}
	return 0
}

// tokenizeVersion splits a version string into alphanumeric tokens,
// separating digits from letters and splitting on dots.
// e.g. "1.0.1rc2" → ["1", "0", "1", "rc", "2"]
func tokenizeVersion(v string) []string {
	var tokens []string
	i := 0
	for i < len(v) {
		if v[i] == '.' {
			i++
			continue
		}
		j := i
		if isDigit(v[i]) {
			for j < len(v) && isDigit(v[j]) {
				j++
			}
		} else {
			for j < len(v) && !isDigit(v[j]) && v[j] != '.' {
				j++
			}
		}
		if j > i {
			tokens = append(tokens, v[i:j])
		}
		i = j
	}
	return tokens
}

func isDigit(b byte) bool {
	return b >= '0' && b <= '9'
}

// checkConflicts checks if a package conflicts with any installed packages.
// Returns a list of conflict package names that are currently installed.
func checkConflicts(conflicts []string) []string {
	var installedConflicts []string
	for _, conflict := range conflicts {
		name := stripVerConstraint(conflict)
		if isInstalled(name) {
			installedConflicts = append(installedConflicts, name)
		}
	}
	return installedConflicts
}

// vercmpFallback is the entry point for pure-Go version comparison.
func vercmpFallback(a, b string) int {
	if a == b {
		return 0
	}
	return compareVersions(a, b)
}

func inPacmanRepo(name string) bool {
	return exec.Command("pacman", "-Si", name).Run() == nil
}

// Remove removes a package via pacman. It tries plain removal first;
// if that fails due to dependency conflicts (common for AUR packages
// with AUR-only deps), it retries with -Rns (recursive + nosave).
//
// IMPORTANT: The retry only fires for dependency-related failures, not
// user cancellation. When pacman prompts "Remove (1) foo? [y/N]" and
// the user answers N, pacman exits with status 1 — the same as a
// dependency conflict. We detect this by checking whether the package
// is still installed after the first attempt: if it is, the removal
// was likely cancelled (or had a conflict), and we offer the stronger
// -Rns option instead of blindly retrying.
func Remove(pkgName string, noConfirm bool) error {
	if err := validatePkgName(pkgName); err != nil {
		return err
	}

	// Bail early if the package isn't installed at all — avoids the
	// confusing double-failure loop where -R fails then -Rns also fails.
	if !isInstalled(pkgName) {
		return fmt.Errorf("package %s is not installed", pkgName)
	}

	// Try plain removal first: pacman -R <pkg>
	args := []string{"pacman", "-R", pkgName}
	if noConfirm {
		args = append(args, "--noconfirm")
	}
	cmd, err := priv.Command(args...)
	if err != nil {
		return err
	}
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	if err := cmd.Run(); err == nil {
		return nil
	}

	// Plain -R failed. Check if the package is still installed — if it
	// is, the failure was either a user cancellation or a dependency
	// conflict. In either case, do NOT auto-retry with -Rns because
	// the user may have intentionally cancelled. Instead, tell them
	// what happened and let them decide.
	if isInstalled(pkgName) {
		warnStderr("pacman -R failed for %s (dependency conflict or user cancellation).", pkgName)
		warnStderr("To force removal including unneeded deps, run:")
		warnStderr("  sudo pacman -Rns %s", pkgName)
		return fmt.Errorf("removal of %s failed", pkgName)
	}

	// Package is no longer installed — the -R partially succeeded or
	// the removal went through despite the error code. Nothing to do.
	return nil
}

// GetInstalledAUR returns installed AUR packages.
func GetInstalledAUR() (map[string]string, error) {
	out, err := exec.Command("pacman", "-Qm").Output()
	if err != nil {
		return nil, fmt.Errorf("pacman -Qm failed: %w", err)
	}
	installed := make(map[string]string)
	for _, line := range strings.Split(string(out), "\n") {
		name, ver := parsePacmanQLine(line)
		if name != "" {
			installed[name] = ver
		}
	}
	return installed, nil
}

// FindAUROrphans returns AUR packages that have no reverse dependencies.
// It cross-references pacman -Qtdq (orphans) with pacman -Qm (AUR packages).
func FindAUROrphans() ([]string, error) {
	// Get packages with no reverse dependencies (orphans)
	orphansOut, err := exec.Command("pacman", "-Qtdq").Output()
	if err != nil {
		// pacman -Qtdq returns exit code 1 if no orphans found, which is normal
		if strings.Contains(err.Error(), "exit status 1") {
			return []string{}, nil
		}
		return nil, fmt.Errorf("pacman -Qtdq failed: %w", err)
	}

	orphanNames := make(map[string]bool)
	for _, line := range strings.Split(string(orphansOut), "\n") {
		if line != "" {
			orphanNames[line] = true
		}
	}

	// Get installed AUR packages
	aurPackages, err := GetInstalledAUR()
	if err != nil {
		return nil, err
	}

	// Find intersection: AUR packages that are also orphans
	var aurOrphans []string
	for name := range aurPackages {
		if orphanNames[name] {
			aurOrphans = append(aurOrphans, name)
		}
	}

	return aurOrphans, nil
}

// ListInstalledAUR returns installed AUR packages.
func ListInstalledAUR() (map[string]string, error) {
	return GetInstalledAUR()
}

// PacmanConfig holds pacman settings parsed from pacman-conf
type PacmanConfig struct {
	IgnorePkg   []string
	IgnoreGroup []string
}

// ReadPacmanConf reads pacman configuration using pacman-conf.
func ReadPacmanConf() (*PacmanConfig, error) {
	out, err := exec.Command("pacman-conf").Output()
	if err != nil {
		return nil, fmt.Errorf("failed to run pacman-conf: %w", err)
	}

	conf := &PacmanConfig{}
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "[") {
			continue
		}

		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}

		key := strings.TrimSpace(parts[0])
		val := strings.TrimSpace(parts[1])

		if val == "" {
			continue
		}

		switch key {
		case "IgnorePkg":
			conf.IgnorePkg = append(conf.IgnorePkg, strings.Fields(val)...)
		case "IgnoreGroup":
			conf.IgnoreGroup = append(conf.IgnoreGroup, strings.Fields(val)...)
		}
	}
	return conf, nil
}
