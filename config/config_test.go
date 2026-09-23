package config

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeUserConfig writes a user config into dir and points XDG_CONFIG_HOME
// at it. Returns the config file path.
func writeUserConfig(t *testing.T, dir, content string) string {
	t.Helper()
	path := filepath.Join(dir, "alps", "config")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_CONFIG_HOME", dir)
	return path
}

// writeGlobalConfig writes a global config into dir and points
// ALPS_GLOBAL_CONFIG at it. Returns the config file path.
func writeGlobalConfig(t *testing.T, dir, content string) string {
	t.Helper()
	path := filepath.Join(dir, "alps", "config")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ALPS_GLOBAL_CONFIG", path)
	return path
}

// TestUserHeaderLinesReplaceGlobal pins the merge rule for title_line: the
// user file's header lines replace the global file's when the user file
// defines any, while every other key is last-one-wins per file.
func TestUserHeaderLinesReplaceGlobal(t *testing.T) {
	globalDir := t.TempDir()
	userDir := t.TempDir()

	writeGlobalConfig(t, globalDir, `
color_primary = \e[35m
title_line = GLOBAL BANNER ONE
title_line = GLOBAL BANNER TWO
`)
	writeUserConfig(t, userDir, `
color_primary = \e[36m
title_line = USER BANNER ONE
title_line = USER BANNER TWO
`)

	cfg := Load()

	if len(cfg.Style.HeaderLines) != 2 {
		t.Fatalf("HeaderLines = %q, want exactly the two user lines", cfg.Style.HeaderLines)
	}
	for _, line := range cfg.Style.HeaderLines {
		if strings.Contains(line, "GLOBAL") {
			t.Errorf("HeaderLines still contains a global line: %q (all lines: %q)", line, cfg.Style.HeaderLines)
		}
	}
	if got, want := cfg.Style.HeaderLines[0], "USER BANNER ONE"; got != want {
		t.Errorf("HeaderLines[0] = %q, want %q", got, want)
	}
	if got, want := cfg.Style.ColorPrimary, "\033[36m"; got != want {
		t.Errorf("ColorPrimary = %q, want the user file's %q", got, want)
	}
}

// TestGlobalHeaderLinesSurviveWhenUserDefinesNone covers the other half of
// the title_line rule: without user entries the global banner is kept.
func TestGlobalHeaderLinesSurviveWhenUserDefinesNone(t *testing.T) {
	globalDir := t.TempDir()
	userDir := t.TempDir()

	writeGlobalConfig(t, globalDir, `
title_line = GLOBAL BANNER ONE
title_line = GLOBAL BANNER TWO
`)
	writeUserConfig(t, userDir, `
color_primary = \e[36m
`)

	cfg := Load()

	want := []string{"GLOBAL BANNER ONE", "GLOBAL BANNER TWO"}
	if len(cfg.Style.HeaderLines) != len(want) {
		t.Fatalf("HeaderLines = %q, want %q", cfg.Style.HeaderLines, want)
	}
	for i, w := range want {
		if cfg.Style.HeaderLines[i] != w {
			t.Errorf("HeaderLines[%d] = %q, want %q", i, cfg.Style.HeaderLines[i], w)
		}
	}
}

// TestMissingConfigFilesYieldDefaults covers the both-files-missing path:
// defaults apply and no header lines exist.
func TestMissingConfigFilesYieldDefaults(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(t.TempDir(), "does-not-exist"))
	t.Setenv("ALPS_GLOBAL_CONFIG", filepath.Join(t.TempDir(), "does-not-exist", "config"))

	cfg := Load()

	if len(cfg.Style.HeaderLines) != 0 {
		t.Errorf("HeaderLines = %q, want none", cfg.Style.HeaderLines)
	}
	if got, want := cfg.Style.ColorPrimary, "\033[36m"; got != want {
		t.Errorf("ColorPrimary = %q, want default %q", got, want)
	}
	if !cfg.Style.ShowHeader {
		t.Error("ShowHeader = false, want the default true")
	}
	if got := cfg.Aliases["ins"]; got != "install" {
		t.Errorf("Aliases[\"ins\"] = %q, want \"install\"", got)
	}
}

// TestUserAliasesMergeIntoLoad checks the alias path of the two-file merge:
// user aliases are kept under their original case and override defaults.
func TestUserAliasesMergeIntoLoad(t *testing.T) {
	globalDir := t.TempDir()
	userDir := t.TempDir()

	writeGlobalConfig(t, globalDir, `alias_Mixed = search`)
	writeUserConfig(t, userDir, `alias_foo = install`)

	cfg := Load()

	if got := cfg.Aliases["foo"]; got != "install" {
		t.Errorf("Aliases[\"foo\"] = %q, want \"install\"", got)
	}
	if got := cfg.Aliases["Mixed"]; got != "search" {
		t.Errorf("Aliases[\"Mixed\"] = %q, want \"search\" (case must be preserved)", got)
	}
}

// TestOversizedTitleLineParses pins the raised scanner limit: a header line
// longer than the 64 KiB default must be fully parsed, not silently
// truncated (title_line holds ASCII art that can grow that big).
func TestOversizedTitleLineParses(t *testing.T) {
	userDir := t.TempDir()
	t.Setenv("ALPS_GLOBAL_CONFIG", filepath.Join(t.TempDir(), "missing", "config"))

	banner := strings.Repeat("A", 70_000)
	writeUserConfig(t, userDir, "title_line = "+banner+"\n")

	cfg := Load()

	if len(cfg.Style.HeaderLines) != 1 {
		t.Fatalf("HeaderLines has %d entries, want 1", len(cfg.Style.HeaderLines))
	}
	if got := cfg.Style.HeaderLines[0]; got != banner {
		t.Errorf("HeaderLines[0] has length %d, want the full %d characters", len(got), len(banner))
	}
}

// TestPermissionErrorIsReported pins that an unreadable config file is
// reported on stderr instead of being silently treated as missing.
func TestPermissionErrorIsReported(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root: permission errors cannot be produced")
	}

	globalDir := t.TempDir()
	userDir := t.TempDir()
	t.Setenv("ALPS_GLOBAL_CONFIG", filepath.Join(t.TempDir(), "missing", "config"))

	path := writeUserConfig(t, userDir, "color_primary = \\e[36m\n")
	if err := os.Chmod(path, 0o000); err != nil {
		t.Fatal(err)
	}
	_ = globalDir

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	oldStderr := os.Stderr
	os.Stderr = w
	cfg := Load()
	os.Stderr = oldStderr
	w.Close()
	output, _ := io.ReadAll(r)

	if !strings.Contains(string(output), "cannot read config") {
		t.Errorf("stderr output %q does not report the unreadable config", string(output))
	}
	if !strings.Contains(string(output), path) {
		t.Errorf("stderr output %q does not name the unreadable file %q", string(output), path)
	}
	if got := cfg.Style.ColorPrimary; got != "\033[36m" {
		t.Errorf("ColorPrimary = %q, want the user value; an unreadable file must not abort loading", got)
	}
}

// TestLoadCachedReturnsSameInstance pins the LoadCached contract: one parse
// per process, same pointer back on every call. Load stays uncached.
func TestLoadCachedReturnsSameInstance(t *testing.T) {
	a := LoadCached()
	b := LoadCached()
	if a != b {
		t.Error("LoadCached() returned different instances across calls")
	}
	if a == nil {
		t.Fatal("LoadCached() returned nil")
	}
}

// TestUnescapeSpellings pins CC-8: every recognised escape spelling expands
// to ESC, and text without escapes passes through unchanged.
func TestUnescapeSpellings(t *testing.T) {
	cases := []struct{ in, want string }{
		{`\e[32m`, "\033[32m"},
		{`\033[32m`, "\033[32m"},
		{`\x1b[32m`, "\033[32m"},
		{`\e[1m bold \e[0m`, "\033[1m bold \033[0m"},
		{`pre \033[0m post`, "pre \033[0m post"},
		{"plain text", "plain text"},
	}
	for _, tc := range cases {
		if got := unescape(tc.in); got != tc.want {
			t.Errorf("unescape(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// TestSymValuesAreTakenLiterally pins the unescape asymmetry: sym_* values
// are printed as written, while colour values are unescaped.
func TestSymValuesAreTakenLiterally(t *testing.T) {
	globalDir := t.TempDir()
	userDir := t.TempDir()
	t.Setenv("ALPS_GLOBAL_CONFIG", filepath.Join(globalDir, "alps", "config"))

	writeUserConfig(t, userDir, "sym_ok = \\e[32m✓\ncolor_primary = \\e[36m\n")

	cfg := Load()

	if got, want := cfg.Style.SymOK, `\e[32m✓`; got != want {
		t.Errorf("SymOK = %q, want the literal config text %q (sym_* values are not unescaped)", got, want)
	}
	if got, want := cfg.Style.ColorPrimary, "\033[36m"; got != want {
		t.Errorf("ColorPrimary = %q, want the unescaped %q", got, want)
	}
}
