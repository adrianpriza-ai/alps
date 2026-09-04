package more

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/adrianpriza-ai/alps/config"
	"github.com/adrianpriza-ai/alps/platform"
)

// TestInstallRemovePipelineRealCommands exercises the full install -> remove
// pipeline end to end. The entry's commands are plain shell commands, which
// Filter places in build_env, so they really execute (mkdir/cp/echo/rm) inside
// the per-package build directory without privilege escalation: safety=free
// skips fakeroot wrapping and build_env never runs through sudo. HOME and the
// state file are redirected to temp dirs so nothing touches the real system.
func TestInstallRemovePipelineRealCommands(t *testing.T) {
	t.Setenv("HOME", t.TempDir()) // point the build cache at a temp dir
	redirectInstalledFile(t)
	t.Setenv("TERM", "") // keep any progress/style output inert

	name := "pipeline-test"
	e := &Entry{
		Name:    name,
		Version: "1.0.0",
		Safety:  "free",
		CmdLines: []string{
			"mkdir -p share/doc",
			"echo 'hello pipeline' > share/doc/readme.txt",
			"cp share/doc/readme.txt share/doc/readme.copy",
		},
		RemoveLines: []string{
			"rm -f share/doc/readme.copy",
			"rm -f share/doc/readme.txt",
			"rmdir share/doc",
		},
	}

	if err := Install(e, &config.Config{}); err != nil {
		t.Fatalf("Install failed: %v", err)
	}

	// The install commands really ran inside the build dir.
	buildDir, err := getBuildDir(name)
	if err != nil {
		t.Fatalf("getBuildDir failed: %v", err)
	}
	readme := filepath.Join(buildDir, "share", "doc", "readme.txt")
	data, err := os.ReadFile(readme)
	if err != nil {
		t.Fatalf("install did not create %s: %v", readme, err)
	}
	if string(data) != "hello pipeline\n" {
		t.Errorf("readme content = %q, want %q", data, "hello pipeline\n")
	}
	if _, err := os.Stat(filepath.Join(buildDir, "share", "doc", "readme.copy")); err != nil {
		t.Errorf("install did not create the copied file: %v", err)
	}

	// The state file recorded the install, including the remove lines.
	rec, ok := GetInstalled(name)
	if !ok {
		t.Fatal("expected the package to be recorded as installed")
	}
	if rec.Version != "1.0.0" {
		t.Errorf("recorded version = %q, want %q", rec.Version, "1.0.0")
	}
	if len(rec.RemoveLines) != len(e.RemoveLines) {
		t.Errorf("recorded %d remove lines, want %d: %v", len(rec.RemoveLines), len(e.RemoveLines), rec.RemoveLines)
	}

	// Remove executes the remove commands and unmarks the package.
	if err := Remove(e, &config.Config{}); err != nil {
		t.Fatalf("Remove failed: %v", err)
	}
	if _, err := os.Stat(readme); err == nil {
		t.Errorf("remove did not delete %s", readme)
	}
	if _, err := os.Stat(filepath.Join(buildDir, "share", "doc")); err == nil {
		t.Errorf("remove did not delete the empty share/doc directory")
	}
	if _, ok := GetInstalled(name); ok {
		t.Error("package should be unmarked after remove")
	}
}

// TestRemoveFallsBackToSavedRemoveLines verifies that removing a package whose
// entry no longer carries remove commands falls back to the lines saved in
// installed.json at install time.
func TestRemoveFallsBackToSavedRemoveLines(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	redirectInstalledFile(t)
	t.Setenv("TERM", "")

	name := "fallback-test"
	e := &Entry{
		Name:    name,
		Version: "2.0.0",
		Safety:  "free",
		CmdLines: []string{
			"mkdir -p data",
			"echo x > data/file.txt",
		},
		RemoveLines: []string{
			"rm -f data/file.txt",
			"rmdir data",
		},
	}

	if err := Install(e, &config.Config{}); err != nil {
		t.Fatalf("Install failed: %v", err)
	}

	// The entry handed to Remove has no remove commands — Scrape must use the
	// record saved during install.
	stripped := &Entry{Name: name, Safety: "free"}
	if err := Remove(stripped, &config.Config{}); err != nil {
		t.Fatalf("Remove with stripped entry failed: %v", err)
	}

	buildDir, err := getBuildDir(name)
	if err != nil {
		t.Fatalf("getBuildDir failed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(buildDir, "data")); err == nil {
		t.Errorf("remove commands from installed.json did not clean up %s", filepath.Join(buildDir, "data"))
	}
	if _, ok := GetInstalled(name); ok {
		t.Error("package should be unmarked after remove")
	}
}

// TestDiffOwnedItems verifies that diffOwnedItems returns exactly the items
// present in the old list but missing from the new one, matching by path+type.
func TestDiffOwnedItems(t *testing.T) {
	old := []OwnedItem{
		{Path: "/usr/bin/tool", Type: "file"},
		{Path: "/etc/tool", Type: "file"},       // dropped by the new version
		{Path: "/opt/tool", Type: "dir"},        // kept, same path+type
		{Path: "/usr/bin/relink", Type: "file"}, // type changed in the new version
	}
	new := []OwnedItem{
		{Path: "/usr/bin/tool", Type: "file"},
		{Path: "/opt/tool", Type: "dir"},
		{Path: "/usr/bin/relink", Type: "symlink"},
		{Path: "/usr/bin/newbin", Type: "file"}, // new item — must not be stale
	}

	stale := diffOwnedItems(old, new)

	want := []OwnedItem{
		{Path: "/etc/tool", Type: "file"},
		{Path: "/usr/bin/relink", Type: "file"},
	}
	if len(stale) != len(want) {
		t.Fatalf("diffOwnedItems returned %d items %v, want %d: %v", len(stale), stale, len(want), want)
	}
	for i, w := range want {
		if stale[i] != w {
			t.Errorf("stale[%d] = %+v, want %+v", i, stale[i], w)
		}
	}
}

// TestDiffOwnedItemsEmpty verifies the edge cases: no old items, or identical
// lists, produce no stale items.
func TestDiffOwnedItemsEmpty(t *testing.T) {
	items := []OwnedItem{{Path: "/usr/bin/a", Type: "file"}}
	if got := diffOwnedItems(nil, items); len(got) != 0 {
		t.Errorf("diffOwnedItems(nil, items) = %v, want none", got)
	}
	if got := diffOwnedItems(items, items); len(got) != 0 {
		t.Errorf("diffOwnedItems(same, same) = %v, want none", got)
	}
}

// TestRunOperationRejectsNoCommands verifies that runOperation refuses an
// install whose entry has no install commands before executing anything.
func TestRunOperationRejectsNoCommands(t *testing.T) {
	e := &Entry{Name: "empty-pkg", CmdLines: nil, Safety: "free"}
	err := runOperation(e, platform.OperationInstall)
	if err == nil {
		t.Fatal("expected error for an entry with no install commands")
	}
	if !strings.Contains(err.Error(), "no install commands") {
		t.Errorf("error should mention missing install commands, got: %v", err)
	}
}
