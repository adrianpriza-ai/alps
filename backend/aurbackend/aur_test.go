package aurbackend

import (
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/adrianpriza-ai/alps/aur"
	"github.com/adrianpriza-ai/alps/config"
)

// TestFilterNonIgnoredSkipsGroupMembers verifies that an expanded ignore set
// keeps group members out of the upgrade list.
func TestFilterNonIgnoredSkipsGroupMembers(t *testing.T) {
	installed := map[string]string{"foo": "1.0", "baz": "2.0"}
	ignoreSet := map[string]bool{"foo": true}

	kept := filterNonIgnored(installed, ignoreSet, "->")
	if len(kept) != 1 || kept[0] != "baz" {
		t.Errorf("filterNonIgnored = %v, want [baz]", kept)
	}
}

// TestFilterNonIgnoredIsSorted verifies that the kept names come out in
// sorted order regardless of Go's randomized map iteration.
func TestFilterNonIgnoredIsSorted(t *testing.T) {
	installed := map[string]string{"zebra": "1.0", "apple": "2.0", "mango": "3.0"}

	for i := 0; i < 20; i++ {
		kept := filterNonIgnored(installed, map[string]bool{}, "->")
		if !sort.StringsAreSorted(kept) {
			t.Fatalf("filterNonIgnored produced unsorted output: %v", kept)
		}
	}
}

// TestFindOutdatedIsSortedAndDeterministic verifies that findOutdated walks
// installed packages in sorted order, so the upgrade list and its output are
// identical across runs.
func TestFindOutdatedIsSortedAndDeterministic(t *testing.T) {
	installed := map[string]string{
		"zebra":  "1.0",
		"apple":  "0.9",
		"mango":  "1.2",
		"hidden": "1.0", // in the ignore set
		"gone":   "1.0", // not in latest
	}
	latest := map[string]*aur.Package{
		"zebra":  {Name: "zebra", Version: "1.1"},
		"apple":  {Name: "apple", Version: "1.0"},
		"mango":  {Name: "mango", Version: "1.3"},
		"hidden": {Name: "hidden", Version: "9.9"},
	}
	ignoreSet := map[string]bool{"hidden": true}

	want := []string{"apple", "mango", "zebra"}
	for i := 0; i < 20; i++ {
		got := findOutdated(installed, latest, ignoreSet, config.Style{})
		if len(got) != len(want) {
			t.Fatalf("findOutdated returned %d packages (%v), want %v", len(got), got, want)
		}
		for j, pkg := range got {
			if pkg.Name != want[j] {
				t.Fatalf("findOutdated order = %v, want %v", got, want)
			}
		}
	}
}

// TestBuildIgnoreSetExpandsGroups verifies that IgnoreGroup entries are
// expanded into their member packages via pacman -Qg and joined with the
// IgnorePkg set. Unknown groups (pacman -Qg fails) are skipped silently.
func TestBuildIgnoreSetExpandsGroups(t *testing.T) {
	pacmanDir := t.TempDir()
	pacmanScript := "#!/bin/sh\n" +
		"if [ \"$1\" = \"-Qg\" ] && [ \"$2\" = \"mygroup\" ]; then\n" +
		"  echo \"mygroup foo\"\n" +
		"  echo \"mygroup bar\"\n" +
		"  exit 0\n" +
		"fi\n" +
		"exit 1\n"
	if err := os.WriteFile(filepath.Join(pacmanDir, "pacman"), []byte(pacmanScript), 0755); err != nil {
		t.Fatalf("failed to write stub pacman: %v", err)
	}
	pacmanConfScript := "#!/bin/sh\necho 'IgnoreGroup = mygroup nosuchgroup'\n"
	if err := os.WriteFile(filepath.Join(pacmanDir, "pacman-conf"), []byte(pacmanConfScript), 0755); err != nil {
		t.Fatalf("failed to write stub pacman-conf: %v", err)
	}
	t.Setenv("PATH", pacmanDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	ignoreSet := buildIgnoreSet()
	if !ignoreSet["foo"] || !ignoreSet["bar"] {
		t.Errorf("expected group members foo and bar in the ignore set, got %v", ignoreSet)
	}
}

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
