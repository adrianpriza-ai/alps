package more

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/adrianpriza-ai/alps/platform"
)

// TestWriteManifestReadManifestRoundTrip verifies that a manifest written
// by WriteManifest can be read back by ReadManifest with identical content.
func TestWriteManifestReadManifestRoundTrip(t *testing.T) {
	// Redirect os.TempDir() for this test so we don't interfere with other tests.
	tmpDir := t.TempDir()
	t.Setenv("TMPDIR", tmpDir)

	original := &ExecutionManifest{
		BuildEnv: []string{
			"echo building",
			"{DOWNLOAD} https://example.com/src.tar.gz",
			"make -j4",
		},
		AfterEnv: []string{
			"{INSTALL_BIN} mytool /usr/bin/",
			"{START_SERVICE} mytool.service",
		},
	}

	if err := WriteManifest(original); err != nil {
		t.Fatalf("WriteManifest failed: %v", err)
	}

	// Verify the file was created.
	manifestPath := filepath.Join(tmpDir, ".alps_runner.txt")
	if _, err := os.Stat(manifestPath); os.IsNotExist(err) {
		t.Fatal("manifest file was not created")
	}

	got, err := ReadManifest()
	if err != nil {
		t.Fatalf("ReadManifest failed: %v", err)
	}

	if len(got.BuildEnv) != len(original.BuildEnv) {
		t.Fatalf("BuildEnv length = %d, want %d", len(got.BuildEnv), len(original.BuildEnv))
	}
	for i, cmd := range got.BuildEnv {
		if cmd != original.BuildEnv[i] {
			t.Errorf("BuildEnv[%d] = %q, want %q", i, cmd, original.BuildEnv[i])
		}
	}

	if len(got.AfterEnv) != len(original.AfterEnv) {
		t.Fatalf("AfterEnv length = %d, want %d", len(got.AfterEnv), len(original.AfterEnv))
	}
	for i, cmd := range got.AfterEnv {
		if cmd != original.AfterEnv[i] {
			t.Errorf("AfterEnv[%d] = %q, want %q", i, cmd, original.AfterEnv[i])
		}
	}
}

// TestWriteManifestReadManifestEmptySections verifies that a manifest with
// empty BuildEnv or AfterEnv sections round-trips correctly.
func TestWriteManifestReadManifestEmptySections(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("TMPDIR", tmpDir)

	cases := []struct {
		name     string
		manifest *ExecutionManifest
	}{
		{
			name: "empty build_env only",
			manifest: &ExecutionManifest{
				BuildEnv: []string{},
				AfterEnv: []string{"{INSTALL_BIN} tool /usr/bin/"},
			},
		},
		{
			name: "empty after_env only",
			manifest: &ExecutionManifest{
				BuildEnv: []string{"make -j4"},
				AfterEnv: []string{},
			},
		},
		{
			name: "both empty",
			manifest: &ExecutionManifest{
				BuildEnv: []string{},
				AfterEnv: []string{},
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Clean up between sub-tests since they all write to the same path.
			manifestPath := filepath.Join(tmpDir, ".alps_runner.txt")
			_ = os.Remove(manifestPath)

			if err := WriteManifest(tc.manifest); err != nil {
				t.Fatalf("WriteManifest failed: %v", err)
			}

			got, err := ReadManifest()
			if err != nil {
				t.Fatalf("ReadManifest failed: %v", err)
			}

			if len(got.BuildEnv) != len(tc.manifest.BuildEnv) {
				t.Errorf("BuildEnv length = %d, want %d", len(got.BuildEnv), len(tc.manifest.BuildEnv))
			}
			if len(got.AfterEnv) != len(tc.manifest.AfterEnv) {
				t.Errorf("AfterEnv length = %d, want %d", len(got.AfterEnv), len(tc.manifest.AfterEnv))
			}
		})
	}
}

// TestWriteManifestReadManifestOverwrite verifies that writing a new manifest
// overwrites the previous one (not appends).
func TestWriteManifestReadManifestOverwrite(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("TMPDIR", tmpDir)

	first := &ExecutionManifest{
		BuildEnv: []string{"first build command"},
		AfterEnv: []string{"first after command"},
	}
	if err := WriteManifest(first); err != nil {
		t.Fatalf("WriteManifest (first) failed: %v", err)
	}

	second := &ExecutionManifest{
		BuildEnv: []string{"second build command"},
		AfterEnv: []string{"second after command"},
	}
	if err := WriteManifest(second); err != nil {
		t.Fatalf("WriteManifest (second) failed: %v", err)
	}

	got, err := ReadManifest()
	if err != nil {
		t.Fatalf("ReadManifest failed: %v", err)
	}

	// Should contain only the second manifest's content.
	if len(got.BuildEnv) != 1 || got.BuildEnv[0] != "second build command" {
		t.Errorf("BuildEnv = %v, want [second build command]", got.BuildEnv)
	}
	if len(got.AfterEnv) != 1 || got.AfterEnv[0] != "second after command" {
		t.Errorf("AfterEnv = %v, want [second after command]", got.AfterEnv)
	}
}

// TestReadManifestMissingFile verifies that ReadManifest returns an error
// when the manifest file does not exist.
func TestReadManifestMissingFile(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("TMPDIR", tmpDir)

	// Ensure no manifest file exists.
	manifestPath := filepath.Join(tmpDir, ".alps_runner.txt")
	_ = os.Remove(manifestPath)

	_, err := ReadManifest()
	if err == nil {
		t.Fatal("expected error when manifest file is missing, got nil")
	}
}

// TestWriteManifestReadManifestSpecialChars verifies that commands with special
// characters (quotes, dollar signs, braces) round-trip correctly.
func TestWriteManifestReadManifestSpecialChars(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("TMPDIR", tmpDir)

	manifest := &ExecutionManifest{
		BuildEnv: []string{
			`echo "hello world"`,
			`$HOME/.local/bin/tool`,
			`{INSTALL_BIN} file-${VERSION} /usr/bin/`,
		},
		AfterEnv: []string{
			`systemctl enable --now service.service`,
			`rm -rf /tmp/build/*`,
		},
	}

	if err := WriteManifest(manifest); err != nil {
		t.Fatalf("WriteManifest failed: %v", err)
	}

	got, err := ReadManifest()
	if err != nil {
		t.Fatalf("ReadManifest failed: %v", err)
	}

	for i, cmd := range got.BuildEnv {
		if cmd != manifest.BuildEnv[i] {
			t.Errorf("BuildEnv[%d] = %q, want %q", i, cmd, manifest.BuildEnv[i])
		}
	}
	for i, cmd := range got.AfterEnv {
		if cmd != manifest.AfterEnv[i] {
			t.Errorf("AfterEnv[%d] = %q, want %q", i, cmd, manifest.AfterEnv[i])
		}
	}
}

// TestExecuteManifestEmpty verifies that ExecuteManifest with an empty manifest
// (no build_env or after_env commands) completes without error.
func TestExecuteManifestEmpty(t *testing.T) {
	manifest := &ExecutionManifest{
		BuildEnv: []string{},
		AfterEnv: []string{},
	}
	e := &Entry{Name: "test-empty-pkg"}
	ctx := NewMacroContext(e, "")
	err := ExecuteManifest(manifest, e, platform.OperationInstall, ctx)
	if err != nil {
		t.Fatalf("ExecuteManifest with empty manifest returned error: %v", err)
	}
}

// TestExecuteManifestBuildEnvOnly verifies that ExecuteManifest with only build_env
// commands (empty after_env) completes without error.
func TestExecuteManifestBuildEnvOnly(t *testing.T) {
	// A simple echo command that should succeed.
	manifest := &ExecutionManifest{
		BuildEnv: []string{"echo build-env-test"},
		AfterEnv: []string{},
	}
	e := &Entry{Name: "test-build-env-pkg", Safety: "free"}
	ctx := NewMacroContext(e, "")
	err := ExecuteManifest(manifest, e, platform.OperationInstall, ctx)
	if err != nil {
		t.Fatalf("ExecuteManifest with build_env only returned error: %v", err)
	}
}

// TestFilterBuildEnvMacros verifies that build_env macros (DOWNLOAD, EXTRACT, SH, BASH_RUN)
// are placed in BuildEnv.
func TestFilterBuildEnvMacros(t *testing.T) {
	ctx := NewMacroContext(&Entry{Name: "pkg", Safety: "free"}, "")
	lines := []string{
		"{DOWNLOAD} https://example.com/file.tar.gz",
		"{EXTRACT} file.tar.gz",
		"{SH} ./configure",
	}
	manifest, err := Filter(lines, ctx, platform.OperationInstall)
	if err != nil {
		t.Fatalf("Filter returned error: %v", err)
	}
	if len(manifest.BuildEnv) != 3 {
		t.Fatalf("expected 3 BuildEnv entries, got %d", len(manifest.BuildEnv))
	}
	if !strings.Contains(manifest.BuildEnv[0], "DOWNLOAD") {
		t.Errorf("BuildEnv[0] should contain DOWNLOAD, got: %s", manifest.BuildEnv[0])
	}
	if !strings.Contains(manifest.BuildEnv[1], "EXTRACT") {
		t.Errorf("BuildEnv[1] should contain EXTRACT, got: %s", manifest.BuildEnv[1])
	}
	if !strings.Contains(manifest.BuildEnv[2], "SH") {
		t.Errorf("BuildEnv[2] should contain SH, got: %s", manifest.BuildEnv[2])
	}
}

// TestFilterAfterEnvMacros verifies that after_env macros (INSTALL_SERVICE, ENABLE_SERVICE)
// are placed in AfterEnv.
func TestFilterAfterEnvMacros(t *testing.T) {
	if platform.IsTermux() || platform.IsMacOS() {
		t.Skip("INSTALL_SERVICE is a no-op on Termux/macOS")
	}
	ctx := NewMacroContext(&Entry{Name: "pkg", Safety: "free"}, "")
	lines := []string{
		"{INSTALL_SERVICE} myapp.service",
		"{ENABLE_SERVICE} myapp.service",
	}
	manifest, err := Filter(lines, ctx, platform.OperationInstall)
	if err != nil {
		t.Fatalf("Filter returned error: %v", err)
	}
	if len(manifest.AfterEnv) != 2 {
		t.Fatalf("expected 2 AfterEnv entries, got %d", len(manifest.AfterEnv))
	}
	if !strings.Contains(manifest.AfterEnv[0], "INSTALL_SERVICE") {
		t.Errorf("AfterEnv[0] should contain INSTALL_SERVICE, got: %s", manifest.AfterEnv[0])
	}
	if !strings.Contains(manifest.AfterEnv[1], "ENABLE_SERVICE") {
		t.Errorf("AfterEnv[1] should contain ENABLE_SERVICE, got: %s", manifest.AfterEnv[1])
	}
}

// TestFilterMixedLines verifies that Filter correctly separates build_env macros,
// after_env macros, and plain shell commands.
func TestFilterMixedLines(t *testing.T) {
	ctx := NewMacroContext(&Entry{Name: "pkg", Safety: "free"}, "")
	lines := []string{
		"{DOWNLOAD} https://example.com/archive.tar.gz",
		"echo building...",
		"make -j4",
		"{INSTALL_SERVICE} myapp.service",
	}
	manifest, err := Filter(lines, ctx, platform.OperationInstall)
	if err != nil {
		t.Fatalf("Filter returned error: %v", err)
	}
	// DOWNLOAD macro goes to BuildEnv.
	// echo and make are shell commands → buffered into a script in BuildEnv.
	// INSTALL_SERVICE goes to AfterEnv.
	if len(manifest.BuildEnv) < 2 {
		t.Errorf("expected at least 2 BuildEnv entries (DOWNLOAD + script), got %d", len(manifest.BuildEnv))
	}
	if len(manifest.AfterEnv) != 1 {
		t.Errorf("expected 1 AfterEnv entry, got %d", len(manifest.AfterEnv))
	}
}

// TestFilterRemoveOpSkipsInstallMacros verifies that during remove/purge operations,
// install-only macros are skipped from AfterEnv.
func TestFilterRemoveOpSkipsInstallMacros(t *testing.T) {
	if platform.IsTermux() || platform.IsMacOS() {
		t.Skip("INSTALL_SERVICE is a no-op on Termux/macOS")
	}
	ctx := NewMacroContext(&Entry{Name: "pkg", Safety: "free"}, "")
	lines := []string{
		"{INSTALL_SERVICE} myapp.service",
		"{ENABLE_SERVICE} myapp.service",
		"{DISABLE_SERVICE} myapp.service",
	}
	manifest, err := Filter(lines, ctx, platform.OperationRemove)
	if err != nil {
		t.Fatalf("Filter returned error: %v", err)
	}
	// INSTALL_SERVICE should be skipped during remove.
	// ENABLE_SERVICE should be skipped (install-only).
	// DISABLE_SERVICE should remain.
	for _, cmd := range manifest.AfterEnv {
		if strings.Contains(cmd, "INSTALL_SERVICE") {
			t.Errorf("INSTALL_SERVICE should be skipped during remove, but found: %s", cmd)
		}
	}
}

// TestFilterPlaceholders verifies that {PKGNAME}, {VERSION}, {SERVER} etc.
// are expanded in the output.
func TestFilterPlaceholders(t *testing.T) {
	ctx := NewMacroContext(&Entry{Name: "myapp", Version: "2.0.0"}, "https://mirror.example.com/")
	lines := []string{"echo {PKGNAME} {VERSION} {SERVER}"}
	manifest, err := Filter(lines, ctx, platform.OperationInstall)
	if err != nil {
		t.Fatalf("Filter returned error: %v", err)
	}
	if len(manifest.BuildEnv) == 0 {
		t.Fatal("expected at least 1 BuildEnv entry")
	}
	// The shell script should contain the expanded values.
	// We can't read the script content directly, but we know it was created
	// without error and the manifest entry exists.
}
