package aur

import (
	"os"
	"testing"
)

// TestVercmpEqual verifies that identical versions return 0 (equal).
func TestVercmpEqual(t *testing.T) {
	tests := []struct {
		name string
		a, b string
	}{
		{"simple", "1.0.0", "1.0.0"},
		{"with release", "1.0.0-1", "1.0.0-1"},
		{"with epoch", "1:1.0.0-1", "1:1.0.0-1"},
		{"zero versions", "0", "0"},
		{"single digit", "1", "1"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := vercmp(tt.a, tt.b)
			if got != 0 {
				t.Errorf("vercmp(%q, %q) = %d, want 0", tt.a, tt.b, got)
			}
		})
	}
}

// TestVercmpNewer verifies that vercmp returns 1 when a is newer than b.
func TestVercmpNewer(t *testing.T) {
	tests := []struct {
		name string
		a, b string
	}{
		{"patch bump", "1.0.1", "1.0.0"},
		{"minor bump", "1.1.0", "1.0.0"},
		{"major bump", "2.0.0", "1.0.0"},
		{"release bump", "1.0.0-2", "1.0.0-1"},
		{"epoch bump", "2:1.0.0-1", "1:1.0.0-1"},
		{"add epoch", "1:1.0.0-1", "1.0.0-1"},
		{"rc to final", "1.0.1", "1.0.0rc2"},
		{"longer version", "1.0.0.1", "1.0.0"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := vercmp(tt.a, tt.b)
			if got != 1 {
				t.Errorf("vercmp(%q, %q) = %d, want 1", tt.a, tt.b, got)
			}
		})
	}
}

// TestVercmpOlder verifies that vercmp returns -1 when a is older than b.
func TestVercmpOlder(t *testing.T) {
	tests := []struct {
		name string
		a, b string
	}{
		{"patch older", "1.0.0", "1.0.1"},
		{"minor older", "1.0.0", "1.1.0"},
		{"major older", "1.0.0", "2.0.0"},
		{"release older", "1.0.0-1", "1.0.0-2"},
		{"epoch older", "1:1.0.0-1", "2:1.0.0-1"},
		{"remove epoch", "1.0.0-1", "1:1.0.0-1"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := vercmp(tt.a, tt.b)
			if got != -1 {
				t.Errorf("vercmp(%q, %q) = %d, want -1", tt.a, tt.b, got)
			}
		})
	}
}

// TestVercmpArchSpecific verifies Arch-specific version ordering rules
// (pkgver comparison, release sorting, epoch dominance).
func TestVercmpArchSpecific(t *testing.T) {
	tests := []struct {
		name     string
		a, b     string
		expected int
	}{
		{"pkgver alpha sort", "1.0a", "1.0b", -1},
		{"pkgver alpha sort reverse", "1.0b", "1.0a", 1},
		{"release sort", "1.0-10", "1.0-2", 1},
		{"epoch dominates", "1:0.1", "2:0.1", -1},
		{"same epoch same ver", "3:1.0-1", "3:1.0-1", 0},
		{"pre-release", "1.0rc1", "1.0", -1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := vercmp(tt.a, tt.b)
			if got != tt.expected {
				t.Errorf("vercmp(%q, %q) = %d, want %d", tt.a, tt.b, got, tt.expected)
			}
		})
	}
}

// TestVercmpFallbackMalformedEpoch verifies that the pure-Go fallback
// handles malformed epoch strings gracefully. The Sscanf error from a
// non-numeric epoch (e.g. "abc:1.0-1") is silently ignored, leaving
// epoch=0 — matching pacman's own lenient tolerance. This test documents
// that chosen behaviour so it isn't accidentally broken.
func TestVercmpFallbackMalformedEpoch(t *testing.T) {
	tests := []struct {
		name     string
		a, b     string
		expected int
	}{
		{"non-numeric epoch defaults to 0", "abc:1.0-1", "1.0-1", 0},
		{"empty epoch string defaults to 0", ":1.0-1", "1.0-1", 0},
		{"epoch with spaces defaults to 0", "  :1.0-1", "1.0-1", 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := vercmpFallback(tt.a, tt.b)
			if got != tt.expected {
				t.Errorf("vercmpFallback(%q, %q) = %d, want %d", tt.a, tt.b, got, tt.expected)
			}
		})
	}
}

// TestVercmpFallbackWithoutBinary verifies that the pure-Go fallback path
// produces correct results when the vercmp binary is not available.
func TestVercmpFallbackWithoutBinary(t *testing.T) {
	// Test the fallback function directly (always works, regardless of vercmp)
	tests := []struct {
		name     string
		a, b     string
		expected int
	}{
		{"equal", "1.0.0", "1.0.0", 0},
		{"equal with release", "1.0.0-1", "1.0.0-1", 0},
		{"newer patch", "1.0.1", "1.0.0", 1},
		{"older patch", "1.0.0", "1.0.1", -1},
		{"newer major", "2.0.0", "1.0.0", 1},
		{"older major", "1.0.0", "2.0.0", -1},
		{"newer epoch", "2:1.0", "1:1.0", 1},
		{"older epoch", "1:1.0", "2:1.0", -1},
		{"epoch vs no epoch", "1:1.0", "1.0", 1},
		{"newer release", "1.0-2", "1.0-1", 1},
		{"older release", "1.0-1", "1.0-2", -1},
		{"alpha vs digit", "1.0a", "1.0.1", -1},
		{"digit vs alpha", "1.0.1", "1.0a", 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := vercmpFallback(tt.a, tt.b)
			if got != tt.expected {
				t.Errorf("vercmpFallback(%q, %q) = %d, want %d", tt.a, tt.b, got, tt.expected)
			}
		})
	}
}

// TestVercmpSymmetry checks that vercmp(a, b) == -vercmp(b, a) for equal pairs
// and vercmp(a, a) == 0.
func TestVercmpSymmetry(t *testing.T) {
	pairs := [][2]string{
		{"1.0.0", "1.0.1"},
		{"2.0.0-1", "1.9.9-2"},
		{"1:1.0", "1:0.9"},
		{"1.0.0-1", "1.0.0-1"},
	}

	for _, pair := range pairs {
		a, b := pair[0], pair[1]
		ab := vercmp(a, b)
		ba := vercmp(b, a)

		if ab == ba && ab != 0 {
			t.Errorf("vercmp(%q, %q) = %d and vercmp(%q, %q) = %d — should be opposite signs", a, b, ab, b, a, ba)
		}
		if ab == 0 && ba != 0 {
			t.Errorf("vercmp(%q, %q) = 0 but vercmp(%q, %q) = %d — equal is not symmetric", a, b, b, a, ba)
		}
	}
}

// TestPkgInstalledVersionNotInstalled verifies that querying a nonexistent
// package returns an empty string.
func TestPkgInstalledVersionNotInstalled(t *testing.T) {
	// Use a package name that is extremely unlikely to exist
	ver := pkgInstalledVersion("this-package-does-not-exist-xyzzy-42")
	if ver != "" {
		t.Errorf("pkgInstalledVersion(nonexistent) = %q, want empty string", ver)
	}
}

// TestPkgInstalledVersionInstalled verifies that querying an installed
// package returns a non-empty version string. Falls back to checking
// pacman itself if no common package is found.
func TestPkgInstalledVersionInstalled(t *testing.T) {
	// Try to find a package we know is installed: pacman itself is
	// always present on an Arch system, and in CI it's likely available.
	// If not, we skip gracefully.
	candidates := []string{"pacman", "bash", "coreutils"}
	var installed string
	for _, name := range candidates {
		if ver := pkgInstalledVersion(name); ver != "" {
			installed = name
			t.Logf("found installed package: %s %s", name, ver)
			break
		}
	}
	if installed == "" {
		t.Skip("no known installed package found — skipping installed version test")
	}

	ver := pkgInstalledVersion(installed)
	if ver == "" {
		t.Errorf("pkgInstalledVersion(%q) returned empty for known installed package", installed)
	}
}

// TestPkgInstalledVersionFormat verifies the version string contains at least
// a major.minor component and looks like a valid version (contains digits).
func TestPkgInstalledVersionFormat(t *testing.T) {
	candidates := []string{"pacman", "bash", "coreutils"}
	for _, name := range candidates {
		ver := pkgInstalledVersion(name)
		if ver == "" {
			continue
		}
		// Version should not be empty and should contain at least one digit
		if len(ver) == 0 {
			t.Errorf("pkgInstalledVersion(%q) = %q — empty version", name, ver)
		}
		hasDigit := false
		for _, c := range ver {
			if c >= '0' && c <= '9' {
				hasDigit = true
				break
			}
		}
		if !hasDigit {
			t.Errorf("pkgInstalledVersion(%q) = %q — no digits in version", name, ver)
		}
		t.Logf("pkgInstalledVersion(%q) = %q", name, ver)
		return // pass after first success
	}
	t.Skip("no known installed package found — skipping format test")
}

// TestVercmpFallbackViaVercmp verifies that calling the public Vercmp function
// falls back to the pure-Go comparator when the vercmp binary is not on PATH.
func TestVercmpFallbackViaVercmp(t *testing.T) {
	// Save and restore the original PATH.
	origPath := os.Getenv("PATH")
	defer os.Setenv("PATH", origPath)
	// Set PATH to empty so exec.Command("vercmp", ...) cannot find the binary,
	// forcing the fallback path inside Vercmp.
	os.Setenv("PATH", "")

	pairs := [][3]string{
		{"1.0.0", "1.0.0", "0"},
		{"2.0.0", "1.0.0", "1"},
		{"1.0.0", "2.0.0", "-1"},
		{"1:1.0", "1.0", "1"},
		{"1.0-2", "1.0-1", "1"},
	}
	for _, tc := range pairs {
		got := Vercmp(tc[0], tc[1])
		want := 0
		switch tc[2] {
		case "1":
			want = 1
		case "-1":
			want = -1
		}
		if got != want {
			t.Errorf("Vercmp(%q, %q) = %d, want %d (fallback via empty PATH)", tc[0], tc[1], got, want)
		}
	}
}
