package more

import (
	"strings"
	"testing"
)

func TestRequireNextSha256(t *testing.T) {
	validHash := strings.Repeat("ab", 32) // 64 hex chars

	t.Run("no sums listed is rejected", func(t *testing.T) {
		ctx := NewMacroContext(&Entry{Name: "pkg"}, "")
		_, err := requireNextSha256(ctx, "script.sh")
		if err == nil {
			t.Fatal("expected error when sha256sums is absent")
		}
	})

	t.Run("downloads exceeding declared sums are rejected", func(t *testing.T) {
		ctx := NewMacroContext(&Entry{Name: "pkg", SHA256Sums: []string{validHash}}, "")
		if _, err := requireNextSha256(ctx, "a"); err != nil {
			t.Fatalf("unexpected error on first download: %v", err)
		}
		_, err := requireNextSha256(ctx, "b")
		if err == nil {
			t.Fatal("expected error when downloads exceed declared sums")
		}
	})

	t.Run("invalid digest format is rejected", func(t *testing.T) {
		ctx := NewMacroContext(&Entry{Name: "pkg", SHA256Sums: []string{"not-a-digest"}}, "")
		_, err := requireNextSha256(ctx, "a")
		if err == nil {
			t.Fatal("expected error for malformed digest")
		}
	})

	t.Run("valid digests are returned in declaration order", func(t *testing.T) {
		second := strings.Repeat("cd", 32)
		ctx := NewMacroContext(&Entry{Name: "pkg", SHA256Sums: []string{validHash, second}}, "")
		got1, err := requireNextSha256(ctx, "first")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got1 != validHash {
			t.Errorf("first digest = %q, want %q", got1, validHash)
		}
		got2, err := requireNextSha256(ctx, "second")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got2 != second {
			t.Errorf("second digest = %q, want %q", got2, second)
		}
	})
}

func TestRequireNextSha256FreeMode(t *testing.T) {
	validHash := strings.Repeat("ab", 32) // 64 hex chars

	t.Run("free mode skips missing digests without error", func(t *testing.T) {
		ctx := NewMacroContext(&Entry{Name: "pkg", Safety: "free"}, "")
		expected, err := requireNextSha256(ctx, "script.sh")
		if err != nil {
			t.Fatalf("free mode should not error on missing digest: %v", err)
		}
		if expected != "" {
			t.Errorf("free mode without sums should return empty expected digest, got %q", expected)
		}
	})

	t.Run("free mode still verifies when digests are declared", func(t *testing.T) {
		ctx := NewMacroContext(&Entry{Name: "pkg", Safety: "free", SHA256Sums: []string{validHash}}, "")
		expected, err := requireNextSha256(ctx, "script.sh")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if expected != validHash {
			t.Errorf("expected digest %q, got %q", validHash, expected)
		}
	})

	t.Run("free mode skips out-of-range digests without error", func(t *testing.T) {
		ctx := NewMacroContext(&Entry{Name: "pkg", Safety: "free", SHA256Sums: []string{validHash}}, "")
		if _, err := requireNextSha256(ctx, "a"); err != nil {
			t.Fatalf("unexpected error on first download: %v", err)
		}
		if _, err := requireNextSha256(ctx, "b"); err != nil {
			t.Fatalf("free mode should not error when downloads exceed sums: %v", err)
		}
	})
}

func TestIsValidSha256(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{strings.Repeat("a", 64), true},
		{strings.Repeat("A", 64), true},
		{strings.Repeat("0", 64), true},
		{strings.Repeat("f", 64), true},
		{strings.Repeat("g", 64), false},
		{strings.Repeat("a", 63), false},
		{strings.Repeat("a", 65), false},
		{"", false},
		{strings.Repeat("a", 32) + "zz" + strings.Repeat("a", 30), false},
	}
	for _, tc := range cases {
		if got := isValidSha256(tc.in); got != tc.want {
			t.Errorf("isValidSha256(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestValidatePkgNameComponent(t *testing.T) {
	valid := []string{"mytool", "nodejs-lts", "my_pkg", "1password", "lib+plus", "a.b"}
	for _, name := range valid {
		if err := validatePkgNameComponent(name); err != nil {
			t.Errorf("expected %q to be valid, got: %v", name, err)
		}
	}

	invalid := []string{
		"", "..", "../evil", "a/b", `a\b`, ".hidden",
		"a b", "a@b", string(make([]byte, 256)),
	}
	for _, name := range invalid {
		if err := validatePkgNameComponent(name); err == nil {
			t.Errorf("expected %q to be rejected", name)
		}
	}
}

func TestGetBuildDirRejectsTraversal(t *testing.T) {
	// getBuildDir must refuse names that would escape the cache root.
	_, err := getBuildDir("../evil")
	if err == nil {
		t.Fatal("expected getBuildDir to reject a path-traversal package name")
	}
}
