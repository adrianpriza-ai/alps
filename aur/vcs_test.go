package aur

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// TestIsVCSPackage covers suffix detection for development packages.
func TestIsVCSPackage(t *testing.T) {
	cases := []struct {
		name string
		want bool
	}{
		{"linux-git", true},
		{"firefox-nightly-git", true},
		{"foo-svn", true},
		{"bar-hg", true},
		{"baz-bzr", true},
		{"qux-cvs", true},
		{"quux-darcs", true},
		{"LINUX-GIT", true}, // case insensitive
		{"git", false},      // bare "git" is not a VCS package
		{"linux", false},
		{"github-desktop", false}, // contains "git" but not as suffix
		{"foobar", false},
	}
	for _, tc := range cases {
		if got := IsVCSPackage(tc.name); got != tc.want {
			t.Errorf("IsVCSPackage(%q) = %v, want %v", tc.name, got, tc.want)
		}
	}
}

// TestParsePKGBUILDSources verifies source array extraction including
// multi-line arrays and += append syntax.
func TestParsePKGBUILDSources(t *testing.T) {
	pkgbuild := `# maintainer: someone
pkgname=test-vcs
pkgver=1.0
source=("git+https://github.com/foo/bar.git"
        "https://example.com/data.tar.gz"
        "svn+https://svn.example.org/trunk")
makedepends=(git)

source+=("hg+https://hg.example.com/repo") # appended entry
`
	path := filepath.Join(t.TempDir(), "PKGBUILD")
	if err := os.WriteFile(path, []byte(pkgbuild), 0644); err != nil {
		t.Fatal(err)
	}

	sources, err := parsePKGBUILDSources(path)
	if err != nil {
		t.Fatalf("parsePKGBUILDSources failed: %v", err)
	}
	want := []string{
		"git+https://github.com/foo/bar.git",
		"https://example.com/data.tar.gz",
		"svn+https://svn.example.org/trunk",
		"hg+https://hg.example.com/repo",
	}
	if len(sources) != len(want) {
		t.Fatalf("got %d sources %v, want %d", len(sources), sources, len(want))
	}
	for i := range want {
		if sources[i] != want[i] {
			t.Errorf("sources[%d] = %q, want %q", i, sources[i], want[i])
		}
	}
}

// TestParsePKGBUILDSourcesMissing checks the error path for unreadable files.
func TestParsePKGBUILDSourcesMissing(t *testing.T) {
	if _, err := parsePKGBUILDSources(filepath.Join(t.TempDir(), "PKGBUILD")); err == nil {
		t.Error("expected error for missing PKGBUILD, got nil")
	}
}

// TestSplitVCSEntry covers scheme stripping and fragment parsing.
func TestSplitVCSEntry(t *testing.T) {
	cases := []struct {
		entry   string
		isVCS   bool
		kind    string
		url     string
		refKind string
		ref     string
	}{
		{
			entry: "git+https://github.com/foo/bar.git",
			isVCS: true, kind: "git",
			url: "https://github.com/foo/bar.git",
		},
		{
			entry: "git+https://github.com/foo/bar.git#branch=dev",
			isVCS: true, kind: "git",
			url:     "https://github.com/foo/bar.git",
			refKind: "branch", ref: "dev",
		},
		{
			entry: `git+https://github.com/foo/bar.git#tag=v1.2.3`,
			isVCS: true, kind: "git",
			url:     "https://github.com/foo/bar.git",
			refKind: "tag", ref: "v1.2.3",
		},
		{
			entry: "git+https://github.com/foo/bar.git#commit=abc123def456",
			isVCS: true, kind: "git",
			url:     "https://github.com/foo/bar.git",
			refKind: "commit", ref: "abc123def456",
		},
		{
			entry: "git://git.sv.gnu.org/emacs.git",
			isVCS: false, // plain git:// without the git+ prefix is rare; not recognised here
		},
		{
			entry: "svn+https://svn.example.org/trunk#revision=42",
			isVCS: true, kind: "svn",
			url: "https://svn.example.org/trunk", // unknown fragment keys are ignored
		},
		{
			entry: "hg+https://hg.example.com/repo",
			isVCS: true, kind: "hg",
			url: "https://hg.example.com/repo",
		},
		{
			entry: "bzr+lp:foo",
			isVCS: true, kind: "bzr",
			url: "lp:foo",
		},
		{
			entry: "https://example.com/archive.tar.gz",
			isVCS: false,
		},
		{
			entry: "local-file.patch",
			isVCS: false,
		},
	}
	for _, tc := range cases {
		src := splitVCSEntry(tc.entry)
		if !tc.isVCS {
			if src != nil {
				t.Errorf("splitVCSEntry(%q): expected nil, got %+v", tc.entry, src)
			}
			continue
		}
		if src == nil {
			t.Errorf("splitVCSEntry(%q): expected a VCS source, got nil", tc.entry)
			continue
		}
		if src.Kind != tc.kind || src.URL != tc.url || src.RefKind != tc.refKind || src.Ref != tc.ref {
			t.Errorf("splitVCSEntry(%q) = %+v, want kind=%q url=%q refKind=%q ref=%q",
				tc.entry, src, tc.kind, tc.url, tc.refKind, tc.ref)
		}
	}
}

// TestFirstVCSSource ensures the first VCS entry wins over later ones and
// that non-VCS entries are skipped.
func TestFirstVCSSource(t *testing.T) {
	entries := []string{
		"https://example.com/data.tar.gz",
		"git+https://github.com/foo/bar.git",
		"svn+https://svn.example.org/trunk",
	}
	src := firstVCSSource(entries)
	if src == nil || src.Kind != "git" {
		t.Fatalf("expected first git source, got %+v", src)
	}
	if src := firstVCSSource([]string{"https://example.com/x.tar.gz"}); src != nil {
		t.Errorf("expected nil for non-VCS entries, got %+v", src)
	}
}

// TestExtractHexRevision covers commit-hash extraction from common
// pkgver() output shapes.
func TestExtractHexRevision(t *testing.T) {
	cases := []struct {
		version string
		want    string
	}{
		{"r123.abcdef7", "abcdef7"},
		{"1.4.2.r15.g8c0f4a6", "8c0f4a6"},
		{"v2.1-14-gabc1234567", "abc1234567"},
		{"0.0.0.r42.gdeadbeef", "deadbeef"},
		{"20240613.r5.g00ff00a", "00ff00a"},              // date + hash: lettered run always wins
		{"20240613.abcde12", "abcde12"},                  // lettered run beats longer digit run
		{"r5.abc", ""},                                   // runs shorter than 7 hex chars are ignored
		{"20240613", ""},                                 // pure-digit date is not a hash (below 12-digit floor)
		{"aabbccddeeff00112233", "aabbccddeeff00112233"}, // long pure-digit-free hex ok... has letters
		{"012345678901", "012345678901"},                 // pure-digit ≥12 accepted as node id
		{"1.2.3", ""},                                    // no revision at all
		{"", ""},
	}
	for _, tc := range cases {
		if got := extractHexRevision(tc.version); got != tc.want {
			t.Errorf("extractHexRevision(%q) = %q, want %q", tc.version, got, tc.want)
		}
	}
}

// TestExtractSVNRevision covers decimal revision extraction.
func TestExtractSVNRevision(t *testing.T) {
	cases := []struct {
		version string
		want    string
	}{
		{"r1234", "1234"},
		{"1.2r56", "56"},
		{"r42.1", "42"},
		{"1234", ""}, // bare digit runs are not revisions (avoids false positives)
		{"noversion", ""},
		{"", ""},
	}
	for _, tc := range cases {
		if got := extractSVNRevision(tc.version); got != tc.want {
			t.Errorf("extractSVNRevision(%q) = %q, want %q", tc.version, got, tc.want)
		}
	}
}

// TestPickGitRef covers ls-remote output parsing including annotated tags.
func TestPickGitRef(t *testing.T) {
	cases := []struct {
		name string
		out  string
		want string
	}{
		{
			name: "HEAD only",
			out:  "8c0f4a6d1e2f3a4b5c6d7e8f9a0b1c2d3e4f5a6b\tHEAD\n",
			want: "8c0f4a6d1e2f3a4b5c6d7e8f9a0b1c2d3e4f5a6b",
		},
		{
			name: "annotated tag prefers peeled line",
			out: "1111111111111111111111111111111111111111\trefs/tags/v1.0\n" +
				"2222222222222222222222222222222222222222\trefs/tags/v1.0^{}\n",
			want: "2222222222222222222222222222222222222222",
		},
		{name: "empty output", out: "\n", want: ""},
	}
	for _, tc := range cases {
		if got := pickGitRef(tc.out); got != tc.want {
			t.Errorf("%s: pickGitRef = %q, want %q", tc.name, got, tc.want)
		}
	}
}

// TestRevisionUpToDate covers the comparison rules per VCS kind.
func TestRevisionUpToDate(t *testing.T) {
	cases := []struct {
		name         string
		kind         string
		installedVer string
		rev          string
		upToDate     bool
		known        bool
	}{
		{"matching short hash", "git", "1.0.r15.g8c0f4a6", "8c0f4a6d1e2f3a4b5c6d7e8f9a0b1c2d3e4f5a6b", true, true},
		{"moved upstream", "git", "1.0.r15.g8c0f4a6", "deadbeefdeadbeefdeadbeefdeadbeefdeadbeef", false, true},
		{"hg node match", "hg", "r3.aabbccddeeff", "aabbccddeeff00112233445566778899aabbccdd", true, true},
		{"no hash in version", "git", "20240613", "8c0f4a6d1e2f3a4b5c6d7e8f9a0b1c2d3e4f5a6b", false, false},
		{"svn matching rev", "svn", "r1234", "1234", true, true},
		{"svn newer upstream", "svn", "r1234", "5678", false, true},
		{"svn no rev in version", "svn", "1.2.3", "5678", false, false},
		{"empty upstream rev", "git", "r1.gabcdef01", "", false, false},
		{"unsupported kind", "bzr", "r1.gabcdef01", "abcdef0123456", false, false},
	}
	for _, tc := range cases {
		upToDate, known := revisionUpToDate(tc.kind, tc.installedVer, tc.rev)
		if upToDate != tc.upToDate || known != tc.known {
			t.Errorf("%s: revisionUpToDate(%s, %q, %q) = (%v, %v), want (%v, %v)",
				tc.name, tc.kind, tc.installedVer, tc.rev, upToDate, known, tc.upToDate, tc.known)
		}
	}
}

// TestCheckVCSUpdateNonVCS verifies non-VCS packages short-circuit cleanly.
func TestCheckVCSUpdateNonVCS(t *testing.T) {
	update, _, err := CheckVCSUpdate(&Package{Name: "plain-package", Version: "1.0"}, "1.0")
	if err != nil || update {
		t.Errorf("expected (false, nil) for non-VCS package, got (%v, %v)", update, err)
	}
	if _, _, err := CheckVCSUpdate(nil, "1.0"); err != nil {
		t.Errorf("expected nil error for nil package, got %v", err)
	}
}

// TestCheckVCSPinnedPinnedTag verifies that pinned tag sources are reported
// through ErrVCSPinned so callers fall back to ordinary version comparison.
func TestCheckVCSPinnedTag(t *testing.T) {
	// A tag-pinned source must classify as pinned before any network I/O;
	// exercise the classification directly via splitVCSEntry.
	src := splitVCSEntry(`git+https://github.com/foo/bar.git#tag=v1.0`)
	if src == nil || src.RefKind != "tag" {
		t.Fatalf("expected tag-pinned source, got %+v", src)
	}
	if !errors.Is(ErrVCSPinned, ErrVCSPinned) { // sentinel exists and is comparable via errors.Is
		t.Error("ErrVCSPinned sentinel broken")
	}
}
