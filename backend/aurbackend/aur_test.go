package aurbackend

import (
	"testing"

	"github.com/adrianpriza-ai/alps/aur"
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
			got := aur.Vercmp(tt.a, tt.b)
			if got != 0 {
				t.Errorf("Vercmp(%q, %q) = %d, want 0", tt.a, tt.b, got)
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
			got := aur.Vercmp(tt.a, tt.b)
			if got != 1 {
				t.Errorf("Vercmp(%q, %q) = %d, want 1", tt.a, tt.b, got)
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
			got := aur.Vercmp(tt.a, tt.b)
			if got != -1 {
				t.Errorf("Vercmp(%q, %q) = %d, want -1", tt.a, tt.b, got)
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
			got := aur.Vercmp(tt.a, tt.b)
			if got != tt.expected {
				t.Errorf("Vercmp(%q, %q) = %d, want %d", tt.a, tt.b, got, tt.expected)
			}
		})
	}
}

// TestVercmpFallbackWithoutBinary verifies that Vercmp works correctly
// using the system vercmp binary (or its pure-Go fallback if absent).
func TestVercmpFallbackWithoutBinary(t *testing.T) {
	// Test equal versions
	result := aur.Vercmp("1.0.0", "1.0.0")
	if result != 0 {
		t.Errorf("Vercmp equal: got %d, want 0", result)
	}
	// Test newer version
	result = aur.Vercmp("1.0.1", "1.0.0")
	if result != 1 {
		t.Errorf("Vercmp newer: got %d, want 1", result)
	}
	// Test older version
	result = aur.Vercmp("1.0.0", "1.0.1")
	if result != -1 {
		t.Errorf("Vercmp older: got %d, want -1", result)
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
		ab := aur.Vercmp(a, b)
		ba := aur.Vercmp(b, a)

		if ab == ba && ab != 0 {
			t.Errorf("vercmp(%q, %q) = %d and vercmp(%q, %q) = %d — should be opposite signs", a, b, ab, b, a, ba)
		}
		if ab == 0 && ba != 0 {
			t.Errorf("vercmp(%q, %q) = 0 but vercmp(%q, %q) = %d — equal is not symmetric", a, b, b, a, ba)
		}
	}
}
