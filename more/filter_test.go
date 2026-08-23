package more

import (
	"strings"
	"testing"
)

// --- Filter() tests ---

// testCtx returns a minimal MacroContext suitable for Filter tests.
func testCtx() *MacroContext {
	return NewMacroContext(&Entry{Name: "test", Safety: "strict"}, "")
}

func TestFilterSeparatesMacros(t *testing.T) {
	lines := []string{
		"echo building",
		"{DOWNLOAD} https://example.com/src.tar.gz",
		"make -j4",
		"{INSTALL_BIN} mytool /usr/bin/",
	}
	ctx := testCtx()
	manifest, err := Filter(lines, ctx, OperationInstall)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// BuildEnv should contain the DOWNLOAD macro line
	if len(manifest.BuildEnv) == 0 {
		t.Fatal("expected BuildEnv entries")
	}
	foundDownload := false
	for _, cmd := range manifest.BuildEnv {
		if strings.Contains(cmd, "{DOWNLOAD}") || strings.Contains(cmd, "DOWNLOAD") {
			foundDownload = true
		}
	}
	if !foundDownload {
		t.Errorf("expected DOWNLOAD macro in BuildEnv, got %v", manifest.BuildEnv)
	}

	// AfterEnv should contain the INSTALL_BIN macro line
	if len(manifest.AfterEnv) == 0 {
		t.Fatal("expected AfterEnv entries")
	}
	foundInstall := false
	for _, cmd := range manifest.AfterEnv {
		if strings.Contains(cmd, "INSTALL_BIN") {
			foundInstall = true
		}
	}
	if !foundInstall {
		t.Errorf("expected INSTALL_BIN macro in AfterEnv, got %v", manifest.AfterEnv)
	}
}

func TestFilterSkipsCommentsAndEmptyLines(t *testing.T) {
	lines := []string{
		"# This is a comment",
		"",
		"   ",
		"echo hello",
	}
	ctx := testCtx()
	manifest, err := Filter(lines, ctx, OperationInstall)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Comments and empty lines should not appear in any manifest section
	for _, cmd := range manifest.BuildEnv {
		if strings.Contains(cmd, "# This is a comment") {
			t.Errorf("comment found in BuildEnv: %q", cmd)
		}
	}
	for _, cmd := range manifest.AfterEnv {
		if strings.Contains(cmd, "# This is a comment") {
			t.Errorf("comment found in AfterEnv: %q", cmd)
		}
	}
}

func TestFilterShellCommandsGoToBuildEnv(t *testing.T) {
	lines := []string{
		"echo building",
		"make -j4",
	}
	ctx := testCtx()
	manifest, err := Filter(lines, ctx, OperationInstall)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Plain shell commands should end up in BuildEnv (as temp scripts)
	if len(manifest.BuildEnv) == 0 {
		t.Error("expected shell commands in BuildEnv")
	}
	// AfterEnv should be empty (no install macros)
	if len(manifest.AfterEnv) != 0 {
		t.Errorf("expected empty AfterEnv, got %v", manifest.AfterEnv)
	}
}

func TestFilterInstallMacrosSkippedDuringRemove(t *testing.T) {
	lines := []string{
		"{INSTALL_BIN} mytool /usr/bin/",
		"{ENABLE_SERVICE} myservice",
	}
	ctx := testCtx()
	ctx.Op = OperationRemove
	manifest, err := Filter(lines, ctx, OperationRemove)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Install-only macros should be filtered out during remove/purge
	if len(manifest.AfterEnv) != 0 {
		t.Errorf("expected AfterEnv to be empty during remove, got %v", manifest.AfterEnv)
	}
}

func TestFilterInstallMacrosSkippedDuringPurge(t *testing.T) {
	lines := []string{
		"{INSTALL_BIN} mytool /usr/bin/",
	}
	ctx := testCtx()
	ctx.Op = OperationPurge
	manifest, err := Filter(lines, ctx, OperationPurge)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(manifest.AfterEnv) != 0 {
		t.Errorf("expected AfterEnv to be empty during purge, got %v", manifest.AfterEnv)
	}
}

func TestFilterEmptyLines(t *testing.T) {
	lines := []string{}
	ctx := testCtx()
	manifest, err := Filter(lines, ctx, OperationInstall)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(manifest.BuildEnv) != 0 || len(manifest.AfterEnv) != 0 {
		t.Errorf("expected empty manifest, got BuildEnv=%v AfterEnv=%v", manifest.BuildEnv, manifest.AfterEnv)
	}
}

func TestFilterDisableServiceNotSkippedDuringRemove(t *testing.T) {
	// DISABLE_SERVICE and STOP_SERVICE are NOT install-only macros,
	// so they should NOT be filtered out during remove.
	lines := []string{
		"{STOP_SERVICE} myservice",
		"{DISABLE_SERVICE} myservice",
	}
	ctx := testCtx()
	ctx.Op = OperationRemove
	manifest, err := Filter(lines, ctx, OperationRemove)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(manifest.AfterEnv) != 2 {
		t.Errorf("expected 2 AfterEnv entries (STOP/DISABLE are not install-only), got %d: %v",
			len(manifest.AfterEnv), manifest.AfterEnv)
	}
}

// --- executeInstallBin() tests ---

func TestExecuteInstallBinNoArgs(t *testing.T) {
	m := Macro{Name: "INSTALL_BIN"}
	ctx := testCtx()
	_, err := executeInstallBin(m, ctx)
	if err == nil {
		t.Fatal("expected error when no arguments provided")
	}
}

func TestExecuteInstallBinWithSourceAndDest(t *testing.T) {
	m := Macro{Name: "INSTALL_BIN", Args: []string{"mytool", "/usr/local/bin/mytool"}}
	ctx := testCtx()
	cmd, err := executeInstallBin(m, ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should track the installed path
	if len(ctx.InstalledPaths) != 1 {
		t.Fatalf("expected 1 installed path, got %d", len(ctx.InstalledPaths))
	}
	tracked := ctx.InstalledPaths[0]
	if tracked.Path != "/usr/local/bin/mytool" {
		t.Errorf("expected tracked path /usr/local/bin/mytool, got %q", tracked.Path)
	}
	if tracked.Type != "file" {
		t.Errorf("expected type 'file', got %q", tracked.Type)
	}
	if !tracked.Generated {
		t.Error("expected Generated to be true")
	}

	// Should return a shell command with mkdir, cp, chmod, echo
	if !strings.Contains(cmd, "mkdir") {
		t.Errorf("expected command to contain mkdir, got %q", cmd)
	}
	if !strings.Contains(cmd, "cp mytool /usr/local/bin/mytool") {
		t.Errorf("expected command to contain cp, got %q", cmd)
	}
	if !strings.Contains(cmd, "chmod 755") {
		t.Errorf("expected command to contain chmod 755, got %q", cmd)
	}
}

func TestExecuteInstallBinDestEndsWithSlash(t *testing.T) {
	// When dest ends with /, the source filename should be appended
	m := Macro{Name: "INSTALL_BIN", Args: []string{"mytool", "/usr/bin/"}}
	ctx := testCtx()
	cmd, err := executeInstallBin(m, ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Tracked path should be /usr/bin/mytool
	if len(ctx.InstalledPaths) != 1 {
		t.Fatalf("expected 1 installed path, got %d", len(ctx.InstalledPaths))
	}
	if ctx.InstalledPaths[0].Path != "/usr/bin/mytool" {
		t.Errorf("expected tracked path /usr/bin/mytool, got %q", ctx.InstalledPaths[0].Path)
	}

	// Command should reference /usr/bin/mytool
	if !strings.Contains(cmd, "/usr/bin/mytool") {
		t.Errorf("expected command to contain /usr/bin/mytool, got %q", cmd)
	}
}

func TestExecuteInstallBinSourceOnly(t *testing.T) {
	// With only source, dest defaults to the platform bin directory + basename.
	m := Macro{Name: "INSTALL_BIN", Args: []string{"mytool"}}
	ctx := testCtx()
	cmd, err := executeInstallBin(m, ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should track a path ending in <binDir>/mytool
	if len(ctx.InstalledPaths) != 1 {
		t.Fatalf("expected 1 installed path, got %d", len(ctx.InstalledPaths))
	}
	trackedPath := ctx.InstalledPaths[0].Path
	if !strings.HasSuffix(trackedPath, "/mytool") {
		t.Errorf("expected tracked path to end with /mytool, got %q", trackedPath)
	}

	// Command should reference the default bin dir
	if !strings.Contains(cmd, "mytool") {
		t.Errorf("expected command to reference mytool, got %q", cmd)
	}
}

func TestExecuteInstallBinRejectsTraversal(t *testing.T) {
	// Source with path traversal should be rejected
	m := Macro{Name: "INSTALL_BIN", Args: []string{"../../etc/passwd"}}
	ctx := testCtx()
	_, err := executeInstallBin(m, ctx)
	if err == nil {
		t.Fatal("expected error for path traversal in source")
	}

	// Dest with path traversal should also be rejected
	m2 := Macro{Name: "INSTALL_BIN", Args: []string{"mytool", "../../etc/passwd"}}
	_, err = executeInstallBin(m2, ctx)
	if err == nil {
		t.Fatal("expected error for path traversal in dest")
	}
}
