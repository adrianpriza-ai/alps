package more

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/adrianpriza-ai/alps/platform"
)

// TestCleanupTempFilesRemovesScratchDir verifies that cleanupTempFiles removes
// the whole per-run scratch directory (manifest plus any temp scripts) and
// resets the cached path so a later run starts fresh.
func TestCleanupTempFilesRemovesScratchDir(t *testing.T) {
	old := runScratchDir
	t.Cleanup(func() { runScratchDir = old })

	dir, err := ensureRunScratchDir()
	if err != nil {
		t.Fatalf("ensureRunScratchDir failed: %v", err)
	}

	// Write a manifest and a temp script into the scratch dir.
	if err := WriteManifest(&ExecutionManifest{BuildEnv: []string{"echo building"}}); err != nil {
		t.Fatalf("WriteManifest failed: %v", err)
	}
	script, err := writeTempScript([]string{"echo hi"}, 0)
	if err != nil {
		t.Fatalf("writeTempScript failed: %v", err)
	}

	manifest := filepath.Join(dir, ".alps_runner.txt")
	for _, path := range []string{manifest, script} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("expected %s to exist before cleanup: %v", path, err)
		}
	}

	cleanupTempFiles()

	if _, err := os.Stat(dir); err == nil {
		t.Errorf("scratch directory %s should be removed by cleanupTempFiles", dir)
	}
	if runScratchDir != "" {
		t.Errorf("runScratchDir = %q, want empty after cleanup", runScratchDir)
	}
}

// TestCleanupTempFilesWithoutScratch verifies that cleanupTempFiles is a no-op
// when no scratch directory was created (no error, nothing removed).
func TestCleanupTempFilesWithoutScratch(t *testing.T) {
	old := runScratchDir
	t.Cleanup(func() { runScratchDir = old })
	runScratchDir = ""

	cleanupTempFiles() // must not panic
	if runScratchDir != "" {
		t.Errorf("runScratchDir = %q, want empty", runScratchDir)
	}
}

// TestRemovePathWithSudoNoEscalation verifies that removePathWithSudo removes
// a file when no privilege escalation is requested.
func TestRemovePathWithSudoNoEscalation(t *testing.T) {
	target := filepath.Join(t.TempDir(), "to-remove.txt")
	if err := os.WriteFile(target, []byte("x"), 0644); err != nil {
		t.Fatalf("failed to create target file: %v", err)
	}

	if err := removePathWithSudo(target, false); err != nil {
		t.Fatalf("removePathWithSudo returned error: %v", err)
	}
	if _, err := os.Stat(target); err == nil {
		t.Errorf("file %s should have been removed", target)
	}
}

// TestRemoveServiceNoopOnUnsupportedPlatforms verifies that removeService is a
// no-op on Termux and macOS (systemd is not available there).
func TestRemoveServiceNoopOnUnsupportedPlatforms(t *testing.T) {
	if !platform.IsTermux() && !platform.IsMacOS() {
		t.Skip("removeService runs systemctl on Linux — covered by integration tests")
	}
	if err := removeService("nonexistent-alps-test.service"); err != nil {
		t.Errorf("removeService should be a no-op on Termux/macOS, got error: %v", err)
	}
}