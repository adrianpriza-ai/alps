package aur

// VCS / development package update detection.
//
// Development packages (-git, -svn, -hg, …) carry versions derived from the
// state of their upstream repositories (e.g. "1.4.2.r15.g8c0f4a6"). The AUR
// RPC publishes whatever version the packager last baked in, so ordinary
// version comparison reports "up to date" while dozens of upstream commits
// have already landed.
//
// This file detects updates the same way paru/yay do, but without evaluating
// pkgver() locally (which would require downloading and building sources):
//
//  1. Ensure a fresh checkout of the package's AUR repo in the build cache
//     and read the PKGBUILD from it.
//  2. Parse the source=() array for the first VCS entry ("git+", "svn+",
//     "hg+" scheme prefixes), honouring #branch= / #tag= / #commit=
//     fragments as defined by makepkg.
//  3. Ask the upstream remote directly for its current revision —
//     git ls-remote / svn info / hg identify — no source clone needed.
//  4. Report the package as outdated unless the installed version string
//     embeds that revision, which is exactly how makepkg-style pkgver()
//     functions record what was built.

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

var (
	// ErrVCSPinned means the VCS source is anchored to a fixed tag or commit.
	// Its published version is then stable, so ordinary RPC version
	// comparison applies; callers should fall back to vercmp.
	ErrVCSPinned = errors.New("vcs source is pinned to a fixed tag or commit")

	// ErrUnknownVCSVersion means the installed version string does not encode
	// an upstream revision that could be compared against the remote.
	ErrUnknownVCSVersion = errors.New("installed version does not encode an upstream revision")
)

// vcsSuffixes lists the naming-convention suffixes that mark development
// packages on the AUR.
var vcsSuffixes = []string{"-git", "-svn", "-hg", "-bzr", "-cvs", "-darcs"}

// IsVCSPackage reports whether name follows the AUR naming convention for a
// VCS-backed development package.
func IsVCSPackage(name string) bool {
	lower := strings.ToLower(name)
	for _, suffix := range vcsSuffixes {
		if strings.HasSuffix(lower, suffix) {
			return true
		}
	}
	return false
}

// vcsSource describes one VCS entry taken from a PKGBUILD source array.
type vcsSource struct {
	Kind    string // "git", "svn" or "hg" ("bzr"/"darcs" parse but are unsupported)
	URL     string // upstream URL with the makepkg scheme prefix removed
	Ref     string // value of a #branch= / #tag= / #commit= fragment, if any
	RefKind string // "branch", "tag" or "commit"; empty means moving tip
}

// CheckVCSUpdate reports whether the development package pkg (currently
// installed as installedVer) has upstream revisions newer than those baked
// into the installed build:
//
//   - (true, rev, nil)  — upstream moved ahead; rebuild available
//   - (false, rev, nil) — installed build matches the upstream tip
//   - (_, _, ErrVCSPinned) — static source anchor; use ordinary vercmp
//   - (_, _, ErrUnknownVCSVersion) — installedVer carries no comparable revision
//   - (_, _, err) — probe failed (network/tooling); caller should warn
func CheckVCSUpdate(pkg *Package, installedVer string) (bool, string, error) {
	if pkg == nil || !IsVCSPackage(pkg.Name) {
		return false, "", nil
	}

	src, err := loadVCSSource(pkg.Name)
	if err != nil {
		return false, "", err
	}
	// A pinned tag or commit never moves by itself: the maintainer bumps the
	// published pkgver when re-pointing it, so plain version comparison is
	// authoritative there.
	if src.RefKind == "tag" || src.RefKind == "commit" {
		return false, "", ErrVCSPinned
	}

	rev, err := src.latestRevision()
	if err != nil {
		return false, "", err
	}

	upToDate, known := revisionUpToDate(src.Kind, installedVer, rev)
	if !known {
		return false, "", fmt.Errorf("%w (%s)", ErrUnknownVCSVersion, src.Kind)
	}

	// Shorten full hashes to something readable in UI output.
	displayRev := rev
	if len(displayRev) > 12 {
		displayRev = displayRev[:12]
	}
	return !upToDate, displayRev, nil
}

// loadVCSSource returns the first VCS source declared by the package's
// current AUR PKGBUILD.
func loadVCSSource(pkgName string) (*vcsSource, error) {
	path, err := ensureFreshPKGBUILD(pkgName)
	if err != nil {
		return nil, fmt.Errorf("vcs check for %s: %w", pkgName, err)
	}
	entries, err := parsePKGBUILDSources(path)
	if err != nil {
		return nil, fmt.Errorf("vcs check for %s: %w", pkgName, err)
	}
	src := firstVCSSource(entries)
	if src == nil {
		return nil, fmt.Errorf("vcs check for %s: PKGBUILD declares no VCS source", pkgName)
	}
	return src, nil
}

// ensureFreshPKGBUILD returns a filesystem path to an up-to-date PKGBUILD
// for pkgName, using (and refreshing) the shared build cache checkout.
func ensureFreshPKGBUILD(pkgName string) (string, error) {
	if err := validatePkgName(pkgName); err != nil {
		return "", err
	}
	pkgDir, err := aurCacheDir(pkgName)
	if err != nil {
		return "", err
	}
	// Quiet mode keeps update scans readable; failures still surface through
	// the returned error so callers can warn instead of guessing.
	if err := syncAURRepo(pkgName, pkgDir, true); err != nil {
		return "", err
	}
	pkgbuildPath := filepath.Join(pkgDir, "PKGBUILD")
	if info, err := os.Stat(pkgbuildPath); err != nil || info.IsDir() {
		return "", fmt.Errorf("no PKGBUILD found for %s after cache sync", pkgName)
	}
	return pkgbuildPath, nil
}

// parsePKGBUILDSources extracts every entry of the PKGBUILD's source array,
// including entries contributed via source+=(...) appends.
func parsePKGBUILDSources(path string) ([]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read PKGBUILD: %w", err)
	}
	var sources []string
	var inSrc bool
	for _, line := range strings.Split(string(data), "\n") {
		t := strings.TrimSpace(stripInlineComment(line))
		switch {
		case strings.HasPrefix(t, "source+=("):
			inSrc = true
			inner := strings.TrimPrefix(t, "source+=(")
			sources = append(sources, parseArrayLine(strings.TrimSuffix(inner, ")"))...)
			if strings.Contains(t, ")") {
				inSrc = false
			}
		case strings.HasPrefix(t, "source=("):
			inSrc = true
			inner := strings.TrimPrefix(t, "source=(")
			sources = append(sources, parseArrayLine(strings.TrimSuffix(inner, ")"))...)
			if strings.Contains(t, ")") {
				inSrc = false
			}
		case inSrc:
			if strings.Contains(t, ")") {
				inSrc = false
				t = strings.TrimSuffix(t, ")")
			}
			sources = append(sources, parseArrayLine(t)...)
		}
	}
	return sources, nil
}

// stripInlineComment removes a trailing shell comment from a PKGBUILD line,
// honouring double quotes so URL fragments like "#tag=v1.0" survive intact.
func stripInlineComment(line string) string {
	var inQuote bool
	for i := 0; i < len(line); i++ {
		switch line[i] {
		case '\\':
			i++ // skip escaped character
		case '"':
			inQuote = !inQuote
		case '#':
			if !inQuote {
				return line[:i]
			}
		}
	}
	return line
}

// firstVCSSource scans parsed source entries in order and returns the first
// one referencing a recognised VCS protocol, or nil when none does.
func firstVCSSource(entries []string) *vcsSource {
	for _, entry := range entries {
		if src := splitVCSEntry(entry); src != nil {
			return src
		}
	}
	return nil
}

// splitVCSEntry converts one raw source-array entry into a vcsSource, or nil
// when the entry does not reference a VCS protocol makepkg understands.
// Example: "git+https://github.com/foo/bar.git#branch=main"
func splitVCSEntry(entry string) *vcsSource {
	rawURL := entry
	var fragment string
	if idx := strings.Index(rawURL, "#"); idx >= 0 {
		fragment = rawURL[idx+1:]
		rawURL = rawURL[:idx]
	}

	src := &vcsSource{}
	for _, scheme := range []struct{ prefix, kind string }{
		{"git+", "git"},
		{"svn+", "svn"},
		{"hg+", "hg"},
		{"bzr+", "bzr"},
	} {
		if strings.HasPrefix(strings.ToLower(rawURL), scheme.prefix) {
			src.Kind = scheme.kind
			rawURL = rawURL[len(scheme.prefix):]
			break
		}
	}
	if src.Kind == "" {
		return nil
	}
	src.URL = rawURL

	// Fragment keys follow makepkg: branch=<name>, tag=<name>, commit=<sha>.
	if key, value, ok := strings.Cut(fragment, "="); ok {
		switch strings.ToLower(strings.TrimSpace(key)) {
		case "branch":
			src.RefKind, src.Ref = "branch", strings.TrimSpace(value)
		case "tag":
			src.RefKind, src.Ref = "tag", strings.TrimSpace(value)
		case "commit":
			src.RefKind, src.Ref = "commit", strings.TrimSpace(value)
		}
	}
	return src
}

// latestRevision queries the upstream remote for its current revision:
// a full commit hash for git/hg or a decimal revision number for svn.
func (s *vcsSource) latestRevision() (string, error) {
	switch s.Kind {
	case "git":
		return s.gitRevision()
	case "hg":
		return s.hgRevision()
	case "svn":
		return s.svnRevision()
	default:
		return "", fmt.Errorf("%s packages are not supported by VCS update detection", s.Kind)
	}
}

// gitRevision resolves the current object hash of the configured branch/tag
// (or HEAD when moving) without cloning, via `git ls-remote`.
func (s *vcsSource) gitRevision() (string, error) {
	args := []string{"ls-remote", s.URL}
	switch s.RefKind {
	case "branch":
		args = append(args, "refs/heads/"+s.Ref)
	case "tag":
		args = append(args, "refs/tags/"+s.Ref)
	case "commit":
		// Pinned commits are filtered out before probing, but keep the
		// guard here so the function stays correct on its own.
		return s.Ref, nil
	default:
		args = append(args, "HEAD")
	}

	cmd, err := unprivilegedCommand("git", args...)
	if err != nil {
		return "", err
	}
	cmd.Env = safeMakepkgEnv()
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git ls-remote %s failed: %w", s.URL, err)
	}
	rev := pickGitRef(string(out))
	if rev == "" {
		ref := s.Ref
		if ref == "" {
			ref = "HEAD"
		}
		return "", fmt.Errorf("upstream %s has no matching ref %q", s.URL, ref)
	}
	return rev, nil
}

// hgRevision identifies the current changeset of the mercurial remote.
func (s *vcsSource) hgRevision() (string, error) {
	args := []string{"identify"}
	if s.RefKind == "branch" || s.RefKind == "tag" {
		args = append(args, "-r", s.Ref)
	}
	args = append(args, s.URL)

	cmd, err := unprivilegedCommand("hg", args...)
	if err != nil {
		return "", err
	}
	cmd.Env = safeMakepkgEnv()
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("hg identify %s failed: %w", s.URL, err)
	}
	fields := strings.Fields(string(out))
	if len(fields) == 0 {
		return "", fmt.Errorf("hg identify %s produced no output", s.URL)
	}
	// Output looks like "1a2b3c4d5e6f+" — strip any local-modification '+'.
	return strings.TrimSuffix(fields[0], "+"), nil
}

// svnRevision reads the latest revision number of the subversion remote.
func (s *vcsSource) svnRevision() (string, error) {
	cmd, err := unprivilegedCommand("svn", "info", "--non-interactive", s.URL)
	if err != nil {
		return "", err
	}
	cmd.Env = safeMakepkgEnv()
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("svn info %s failed: %w", s.URL, err)
	}
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if value, ok := strings.CutPrefix(line, "Revision:"); ok {
			return strings.TrimSpace(value), nil
		}
	}
	return "", fmt.Errorf("svn info %s reported no revision", s.URL)
}

// pickGitRef selects the object hash from `git ls-remote` output, preferring
// the peeled "^{}" line because annotated tags point at a tag object while
// the peeled line names the underlying commit.
func pickGitRef(out string) string {
	firstHash := ""
	peeledHash := ""
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		hash := strings.Fields(line)[0]
		if firstHash == "" {
			firstHash = hash
		}
		if strings.HasSuffix(line, "^{}") {
			peeledHash = hash
		}
	}
	if peeledHash != "" {
		return peeledHash
	}
	return firstHash
}

// revisionUpToDate compares the revision recorded in installedVer against the
// current upstream revision rev. The second result reports whether a reliable
// comparison could be made at all; unknown versions must not be treated as
// either up to date or outdated.
func revisionUpToDate(kind, installedVer, rev string) (bool, bool) {
	if rev == "" || installedVer == "" {
		return false, false
	}
	switch kind {
	case "git", "hg":
		installed := extractHexRevision(installedVer)
		if installed == "" {
			return false, false
		}
		// Prefix match both ways handles different abbreviation lengths
		// between the local pkgver() output and the full remote hash.
		return strings.HasPrefix(rev, installed) || strings.HasPrefix(installed, rev), true
	case "svn":
		installed := extractSVNRevision(installedVer)
		if installed == "" {
			return false, false
		}
		return installed == rev, true
	default:
		return false, false
	}
}

const (
	minHashLength      = 7  // standard git short-hash length
	minDigitHashLength = 12 // pure-digit runs must be this long: 8-digit dates (YYYYMMDD) must not match
)

// extractHexRevision scans a VCS package version for the longest hexadecimal
// run that encodes a commit/node abbreviation:
//
//	r1234.abcdefg        → abcdefg
//	1.4.2.r15.g8c0f4a6   → 8c0f4a6
//	v2.1-14-gabc1234567  → abc1234567
//
// Runs shorter than minHashLength are ignored. Runs containing letters always
// beat pure-digit runs of any length, so date components like 20240613 don't
// shadow a real hash; a pure-digit run is only accepted when no lettered run
// exists and it is at least minDigitHashLength long.
func extractHexRevision(version string) string {
	lower := strings.ToLower(version)
	bestLetter := ""
	bestDigit := ""

	start := -1
	flush := func(end int) {
		if start < 0 {
			return
		}
		run := lower[start:end]
		start = -1
		hasLetter := strings.ContainsAny(run, "abcdef")
		if hasLetter && len(run) >= minHashLength && len(run) > len(bestLetter) {
			bestLetter = run
		}
		if !hasLetter && len(run) >= minDigitHashLength && len(run) > len(bestDigit) {
			bestDigit = run
		}
	}
	for i := 0; i < len(lower); i++ {
		c := lower[i]
		isHex := (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')
		if isHex {
			if start < 0 {
				start = i
			}
		} else {
			flush(i)
		}
	}
	flush(len(lower))

	if bestLetter != "" {
		return bestLetter
	}
	return bestDigit
}

// extractSVNRevision pulls the decimal revision number out of an installed
// subversion package version ("r1234", "1.2r56" → "1234"/"56").
// Only the explicit r<digits> notation counts as a revision: accepting bare
// digit runs would misread ordinary version components (e.g. "1.2.3") and
// report a false comparison result.
func extractSVNRevision(version string) string {
	for i := 0; i < len(version); i++ {
		if version[i] != 'r' || i+1 >= len(version) || !isDigit(version[i+1]) {
			continue
		}
		j := i + 1
		for j < len(version) && isDigit(version[j]) {
			j++
		}
		return version[i+1 : j]
	}
	return ""
}
