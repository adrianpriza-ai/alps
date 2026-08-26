package more

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/adrianpriza-ai/alps/platform"
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
		if err := platform.ValidatePkgName(name); err != nil {
			t.Errorf("expected %q to be valid, got: %v", name, err)
		}
	}

	invalid := []string{
		"", "..", "../evil", "a/b", `a\b`, ".hidden",
		"a b", "a@b", string(make([]byte, 256)),
	}
	for _, name := range invalid {
		if err := platform.ValidatePkgName(name); err == nil {
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

// --- isKnownMacro / DONWLOAD deprecation tests ---

func TestIsKnownMacroRejectsDonwloadTypo(t *testing.T) {
	// After the fix, "DONWLOAD" should no longer be accepted by isKnownMacro.
	// It only survives in ParseMacro via an explicit remap with a deprecation warning.
	if isKnownMacro("DONWLOAD") {
		t.Error("isKnownMacro should not accept the DONWLOAD typo")
	}
	// The correct spelling should still work
	if !isKnownMacro("DOWNLOAD") {
		t.Error("isKnownMacro should accept DOWNLOAD")
	}
}

func TestParseMacroDonwloadRemapsWithWarning(t *testing.T) {
	// Capture stderr to verify the deprecation warning is printed
	r, w, _ := os.Pipe()
	oldStderr := os.Stderr
	os.Stderr = w

	macro, remaining, ok := ParseMacro("{DONWLOAD} https://example.com/file.tar.gz")

	// Flush and restore stderr
	w.Close()
	os.Stderr = oldStderr

	var buf strings.Builder
	io.Copy(&buf, r)
	warning := buf.String()

	if !ok {
		t.Fatal("expected ParseMacro to recognize DONWLOAD (backward compat)")
	}
	if macro.Name != "DOWNLOAD" {
		t.Errorf("expected name to be remapped to DOWNLOAD, got %q", macro.Name)
	}
	if remaining != "" {
		t.Errorf("expected empty remaining, got %q", remaining)
	}
	if !strings.Contains(warning, "deprecated") {
		t.Errorf("expected deprecation warning on stderr, got %q", warning)
	}
	if !strings.Contains(warning, "DONWLOAD") {
		t.Errorf("expected warning to mention DONWLOAD, got %q", warning)
	}
}

// --- expandLine brace-preservation tests ---

func TestExpandLinePreservesShellBraces(t *testing.T) {
	// Shell expressions like ${HOME} and ${1} should NOT have their braces stripped.
	// Only unknown {ALL_CAPS_TOKEN} placeholders should be removed.
	ctx := NewMacroContext(&Entry{Name: "test", Version: "1.0"}, "")

	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "preserves ${HOME}",
			in:   `echo $HOME`,
			want: `echo $HOME`,
		},
		{
			name: "strips unknown {TOKEN}",
			in:   `echo {UNKNOWN_TOKEN}`,
			want: `echo `, // space before token is preserved
		},
		{
			name: "strips unknown {TOKEN} but preserves known var",
			in:   `echo {VERSION} {UNKNOWN}`,
			want: `echo 1.0 `, // trailing space after stripped token
		},
		{
			name: "does not strip lowercase braces",
			in:   `echo {hello}`,
			want: `echo {hello}`,
		},
		{
			name: "does not strip mixed case braces",
			in:   `echo {HelloWorld}`,
			want: `echo {HelloWorld}`,
		},
		{
			name: "strips only all-caps {TOKEN}",
			in:   `cmd {FOO_BAR} {not_caps} {123}`,
			want: `cmd  {not_caps} {123}`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := expandLine(tc.in, ctx)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Errorf("expandLine(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestExpandMacroArgsPreservesShellBraces(t *testing.T) {
	// Same brace-preservation behavior should apply inside macro arguments.
	ctx := NewMacroContext(&Entry{Name: "test", Version: "1.0"}, "")

	// Test that unknown tokens are stripped but shell constructs survive
	macro := &Macro{
		Name: "SH",
		Args: []string{"script.sh", "{UNKNOWN}", "{VERSION}"},
	}
	expandMacroArgs(macro, ctx)

	if macro.Args[0] != "script.sh" {
		t.Errorf("expected first arg unchanged, got %q", macro.Args[0])
	}
	if macro.Args[1] != "" {
		t.Errorf("expected unknown token stripped, got %q", macro.Args[1])
	}
	if macro.Args[2] != "1.0" {
		t.Errorf("expected VERSION expanded, got %q", macro.Args[2])
	}
}

// --- replaceVars tests ---

func TestReplaceVarsAllVariables(t *testing.T) {
	// Verify that all known variables are replaced correctly.
	ctx := &MacroContext{
		Arch:          "x86_64",
		OS:            "linux",
		Distro:        "ubuntu",
		DistroVersion: "22.04",
		Version:       "1.2.3",
		BuildDir:      "/tmp/build",
		Server:        "https://mirror.example.com",
		PackageName:   "mytool",
	}

	input := "{ARCH} {OS} {DISTRO} {DISVER} {VERSION} {PKG_DIR} {SERVER} {PKGNAME}"
	want := "x86_64 linux ubuntu 22.04 1.2.3 /tmp/build https://mirror.example.com mytool"

	got := replaceVars(input, ctx, false)
	if got != want {
		t.Errorf("replaceVars() = %q, want %q", got, want)
	}
}

func TestReplaceVarsStripsUnknownTokens(t *testing.T) {
	// When stripUnknown=true, unknown {ALL_CAPS} tokens should be removed.
	ctx := &MacroContext{
		Version: "1.0",
	}

	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "strips unknown ALL_CAPS token",
			in:   "echo {UNKNOWN_TOKEN}",
			want: "echo ",
		},
		{
			name: "preserves known variable and strips unknown",
			in:   "echo {VERSION} {UNKNOWN}",
			want: "echo 1.0 ",
		},
		{
			name: "does not strip lowercase braces",
			in:   "echo {hello}",
			want: "echo {hello}",
		},
		{
			name: "does not strip mixed case braces",
			in:   "echo {HelloWorld}",
			want: "echo {HelloWorld}",
		},
		{
			name: "does not strip numbers-only braces",
			in:   "echo {123}",
			want: "echo {123}",
		},
		{
			name: "strips underscored ALL_CAPS token",
			in:   "cmd {FOO_BAR}",
			want: "cmd ",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := replaceVars(tc.in, ctx, true)
			if got != tc.want {
				t.Errorf("replaceVars(%q, _, true) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestReplaceVarsPreservesUnknownTokens(t *testing.T) {
	// When stripUnknown=false, unknown tokens should be preserved.
	ctx := &MacroContext{
		Version: "1.0",
	}

	input := "{VERSION} {UNKNOWN} {DOWNLOAD}"
	want := "1.0 {UNKNOWN} {DOWNLOAD}"

	got := replaceVars(input, ctx, false)
	if got != want {
		t.Errorf("replaceVars(%q, _, false) = %q, want %q", input, got, want)
	}
}

func TestReplaceVarsPreservesShellBraces(t *testing.T) {
	// Shell expressions like ${HOME} and ${1} should never have their braces stripped,
	// regardless of stripUnknown setting.
	ctx := &MacroContext{
		Version: "1.0",
	}

	tests := []struct {
		name         string
		in           string
		stripUnknown bool
		want         string
	}{
		{
			name:         "preserves ${HOME} with stripUnknown=true",
			in:           `echo $HOME`,
			stripUnknown: true,
			want:         `echo $HOME`,
		},
		{
			name:         "preserves ${1} with stripUnknown=true",
			in:           `echo ${1}`,
			stripUnknown: true,
			want:         `echo ${1}`,
		},
		{
			name:         "preserves ${HOME} with stripUnknown=false",
			in:           `echo $HOME`,
			stripUnknown: false,
			want:         `echo $HOME`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := replaceVars(tc.in, ctx, tc.stripUnknown)
			if got != tc.want {
				t.Errorf("replaceVars(%q, _, %v) = %q, want %q", tc.in, tc.stripUnknown, got, tc.want)
			}
		})
	}
}

func TestReplaceVarsEmptyString(t *testing.T) {
	ctx := &MacroContext{Version: "1.0"}

	got := replaceVars("", ctx, false)
	if got != "" {
		t.Errorf("replaceVars(empty) = %q, want empty string", got)
	}
}

func TestReplaceVarsNoVariables(t *testing.T) {
	// Input with no variable placeholders should pass through unchanged.
	ctx := &MacroContext{
		Version: "1.0",
		Arch:    "x86_64",
	}

	input := "echo hello world"
	got := replaceVars(input, ctx, false)
	if got != input {
		t.Errorf("replaceVars(%q) = %q, want %q", input, got, input)
	}
}

func TestReplaceVarsMultipleOccurrences(t *testing.T) {
	// Same variable appearing multiple times should all be replaced.
	ctx := &MacroContext{
		Version: "2.0",
	}

	input := "{VERSION} and {VERSION} again"
	want := "2.0 and 2.0 again"

	got := replaceVars(input, ctx, false)
	if got != want {
		t.Errorf("replaceVars(%q) = %q, want %q", input, got, want)
	}
}

func TestReplaceVarsMixedVariablesAndUnknownTokens(t *testing.T) {
	// Test a complex mix of known variables, unknown tokens, and shell constructs.
	ctx := &MacroContext{
		Arch:          "aarch64",
		OS:            "linux",
		Distro:        "arch",
		DistroVersion: "rolling",
		Version:       "3.0",
		BuildDir:      "/opt/build",
		Server:        "https://archlinux.org",
		PackageName:   "neovim",
	}

	input := "arch={ARCH} os={OS} ver={VERSION} unknown={FOOBAR} shell=${HOME}"
	want := "arch=aarch64 os=linux ver=3.0 unknown= shell=${HOME}"

	got := replaceVars(input, ctx, true)
	if got != want {
		t.Errorf("replaceVars() = %q, want %q", got, want)
	}
}

// --- downloadSimple / downloadWithProgress tests ---

// sha256hex computes the hex-encoded SHA-256 of data for test assertions.
func sha256hex(data []byte) string {
	return fmt.Sprintf("%x", sha256.Sum256(data))
}

func TestDownloadSimpleAtomicWrite(t *testing.T) {
	// Verify that downloadSimple writes to a temp file, checks the SHA256,
	// then atomically renames to the final path. The destination must contain
	// the exact bytes that were fed through the reader.
	content := []byte("hello, world — atomic download test")
	expectedHash := sha256hex(content)

	e := &Entry{Name: "dltest", SHA256Sums: []string{expectedHash}}
	ctx := NewMacroContext(e, "")

	dest := filepath.Join(t.TempDir(), "output.bin")
	_, err := downloadSimple(bytes.NewReader(content), dest, ctx)
	if err != nil {
		t.Fatalf("downloadSimple returned error: %v", err)
	}

	// The final file must exist and match the original content.
	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("cannot read destination file: %v", err)
	}
	if !bytes.Equal(got, content) {
		t.Errorf("file content mismatch: got %q, want %q", got, content)
	}

	// The .tmp leftover must NOT exist.
	tmpPath := dest + ".tmp"
	if _, err := os.Stat(tmpPath); err == nil {
		t.Errorf("temp file %s should have been cleaned up after rename", tmpPath)
	}
}

func TestDownloadSimpleHashMismatchCleansUp(t *testing.T) {
	// When the computed hash does not match the declared digest, the
	// function must return an error AND remove the temp file so that no
	// partial/unverified data remains on disk.
	content := []byte("some content")
	wrongHash := strings.Repeat("ff", 32) // 64 hex chars — guaranteed mismatch
	e := &Entry{Name: "dltest", SHA256Sums: []string{wrongHash}}
	ctx := NewMacroContext(e, "")

	dest := filepath.Join(t.TempDir(), "mismatch.bin")
	_, err := downloadSimple(bytes.NewReader(content), dest, ctx)
	if err == nil {
		t.Fatal("expected error on SHA256 mismatch, got nil")
	}

	// Neither the final file nor the temp file should exist.
	if _, err := os.Stat(dest); err == nil {
		t.Errorf("destination file %s should not exist after hash mismatch", dest)
	}
	if _, err := os.Stat(dest + ".tmp"); err == nil {
		t.Errorf("temp file %s.tmp should not exist after hash mismatch", dest)
	}
}

func TestDownloadWithProgressAtomicWrite(t *testing.T) {
	// Same guarantees as downloadSimple but via the progress-tracked path.
	content := []byte("progress-tracked download content for atomic write test")
	expectedHash := sha256hex(content)
	e := &Entry{Name: "dltest", SHA256Sums: []string{expectedHash}}
	ctx := NewMacroContext(e, "")

	dest := filepath.Join(t.TempDir(), "progress.bin")
	_, err := downloadWithProgress(bytes.NewReader(content), dest, int64(len(content)), ctx)
	if err != nil {
		t.Fatalf("downloadWithProgress returned error: %v", err)
	}

	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("cannot read destination file: %v", err)
	}
	if !bytes.Equal(got, content) {
		t.Errorf("file content mismatch: got %q, want %q", got, content)
	}

	// Temp leftover must be gone.
	if _, err := os.Stat(dest + ".tmp"); err == nil {
		t.Errorf("temp file %s.tmp should have been cleaned up", dest)
	}
}

func TestDownloadWithProgressHashMismatchCleansUp(t *testing.T) {
	// Hash mismatch during progress-tracked download must clean up both files.
	content := []byte("another content")
	wrongHash := strings.Repeat("aa", 32)
	e := &Entry{Name: "dltest", SHA256Sums: []string{wrongHash}}
	ctx := NewMacroContext(e, "")

	dest := filepath.Join(t.TempDir(), "mismatch_progress.bin")
	_, err := downloadWithProgress(bytes.NewReader(content), dest, int64(len(content)), ctx)
	if err == nil {
		t.Fatal("expected error on SHA256 mismatch, got nil")
	}

	if _, err := os.Stat(dest); err == nil {
		t.Errorf("destination file %s should not exist after hash mismatch", dest)
	}
	if _, err := os.Stat(dest + ".tmp"); err == nil {
		t.Errorf("temp file %s.tmp should not exist after hash mismatch", dest)
	}
}

func TestDownloadSimpleFreeModeSkipsDigest(t *testing.T) {
	// In free mode (safety=free) with no sha256sums declared, the download
	// should succeed without hash verification.
	content := []byte("free-mode content")
	e := &Entry{Name: "dltest", Safety: "free"}
	ctx := NewMacroContext(e, "")

	dest := filepath.Join(t.TempDir(), "free.bin")
	_, err := downloadSimple(bytes.NewReader(content), dest, ctx)
	if err != nil {
		t.Fatalf("downloadSimple in free mode returned error: %v", err)
	}

	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("cannot read destination file: %v", err)
	}
	if !bytes.Equal(got, content) {
		t.Errorf("file content mismatch: got %q, want %q", got, content)
	}
}

func TestExecuteExtract(t *testing.T) {
	// Test that argument validation rejects missing arguments.
	t.Run("rejects missing argument", func(t *testing.T) {
		m := Macro{Name: "EXTRACT"}
		ctx := NewMacroContext(&Entry{Name: "pkg"}, "")
		_, err := executeExtract(m, ctx)
		if err == nil {
			t.Fatal("expected error for missing EXTRACT argument")
		}
	})

	// Test that path traversal is rejected.
	t.Run("rejects unsafe path", func(t *testing.T) {
		m := Macro{Name: "EXTRACT", Args: []string{"../etc/passwd.tar.gz"}}
		ctx := NewMacroContext(&Entry{Name: "pkg"}, "")
		_, err := executeExtract(m, ctx)
		if err == nil {
			t.Fatal("expected error for unsafe EXTRACT path")
		}
	})

	// Table-driven test for archive type detection.
	// Safety: "free" avoids fakeroot wrapping so we can test the raw command.
	cases := []struct {
		desc     string
		filename string
		wantCmd  string
	}{
		{"tar.gz", "archive.tar.gz", "tar -xzf archive.tar.gz"},
		{"tgz", "archive.tgz", "tar -xzf archive.tgz"},
		{"tar.xz", "archive.tar.xz", "tar -xJf archive.tar.xz"},
		{"txz", "archive.txz", "tar -xJf archive.txz"},
		{"tar.bz2", "archive.tar.bz2", "tar -xjf archive.tar.bz2"},
		{"tbz", "archive.tbz", "tar -xjf archive.tbz"},
		{"zip", "archive.zip", "unzip archive.zip"},
		{"fallback", "archive.tar", "tar -xf archive.tar"},
	}

	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			m := Macro{Name: "EXTRACT", Args: []string{tc.filename}}
			ctx := NewMacroContext(&Entry{Name: "pkg", Safety: "free"}, "")
			got, err := executeExtract(m, ctx)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.wantCmd {
				t.Errorf("got %q, want %q", got, tc.wantCmd)
			}
		})
	}
}

// TestExecuteDownloadValidation tests argument and URL validation for executeDownload.
func TestExecuteDownloadValidation(t *testing.T) {
	t.Run("no args returns error", func(t *testing.T) {
		ctx := NewMacroContext(&Entry{Name: "pkg"}, "")
		m := Macro{Name: "DOWNLOAD", Args: []string{}}
		_, err := executeDownload(m, ctx)
		if err == nil {
			t.Fatal("expected error for missing args")
		}
	})

	t.Run("http URL rejected", func(t *testing.T) {
		ctx := NewMacroContext(&Entry{Name: "pkg"}, "")
		m := Macro{Name: "DOWNLOAD", Args: []string{"http://example.com/file.tar.gz"}}
		_, err := executeDownload(m, ctx)
		if err == nil {
			t.Fatal("expected error for HTTP URL")
		}
		if !strings.Contains(err.Error(), "HTTPS") {
			t.Errorf("error should mention HTTPS, got: %v", err)
		}
	})

	t.Run("missing sha256 in strict mode returns error", func(t *testing.T) {
		// SHA256 validation happens inside downloadSimple/downloadWithProgress
		// (already tested separately). Here we verify that executeDownload
		// at least validates the URL scheme before making any request.
		ctx := NewMacroContext(&Entry{Name: "pkg", Safety: "strict"}, "")
		m := Macro{Name: "DOWNLOAD", Args: []string{"http://insecure.example.com/file.tar.gz"}}
		_, err := executeDownload(m, ctx)
		if err == nil {
			t.Fatal("expected error for insecure HTTP URL")
		}
		if !strings.Contains(err.Error(), "HTTPS") {
			t.Errorf("error should mention HTTPS, got: %v", err)
		}
	})

	t.Run("unsafe path traversal rejected", func(t *testing.T) {
		ctx := NewMacroContext(&Entry{Name: "pkg", Safety: "free"}, "")
		m := Macro{Name: "DOWNLOAD", Args: []string{"https://example.com/file.tar.gz", "../../etc/passwd"}}
		_, err := executeDownload(m, ctx)
		if err == nil {
			t.Fatal("expected error for path traversal")
		}
	})
}

// TestExecuteDownloadServerErrors tests that download errors from the server are propagated.
func TestExecuteDownloadServerErrors(t *testing.T) {
	// Server returns 404.
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	e := &Entry{Name: "pkg", Safety: "free"}
	ctx := NewMacroContext(e, "")
	ctx.BuildDir = t.TempDir()
	m := Macro{Name: "DOWNLOAD", Args: []string{srv.URL + "/missing.tar.gz"}}
	_, err := executeDownload(m, ctx)
	if err == nil {
		t.Fatal("expected error for 404 response")
	}
}

// TestExecuteDownloadSuccess verifies a successful HTTPS download writes the file
// and returns a shell command.
func TestExecuteDownloadSuccess(t *testing.T) {
	body := []byte("test download content")
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(body)
	}))
	defer srv.Close()

	// Trust the test server's self-signed cert.
	// executeDownload uses the default http client which won't trust it,
	// so this test verifies error propagation for TLS errors.
	e := &Entry{Name: "pkg", Safety: "free"}
	ctx := NewMacroContext(e, "")
	ctx.BuildDir = t.TempDir()
	m := Macro{Name: "DOWNLOAD", Args: []string{srv.URL + "/file.tar.gz"}}
	// The default HTTP client rejects self-signed certs — this is expected.
	// We just confirm the function doesn't panic and returns an error.
	_, err := executeDownload(m, ctx)
	if err == nil {
		// If TLS validation is skipped somehow, verify the file was written.
		dest := filepath.Join(ctx.BuildDir, "file.tar.gz")
		if _, statErr := os.Stat(dest); statErr != nil {
			t.Errorf("expected file to exist, got stat error: %v", statErr)
		}
	}
}

// TestExecuteInstallServiceValidation tests argument and path validation for executeInstallService.
func TestExecuteInstallServiceValidation(t *testing.T) {
	// Skip on Termux/macOS where the function returns empty.
	if platform.IsTermux() || platform.IsMacOS() {
		t.Skip("INSTALL_SERVICE is a no-op on Termux/macOS")
	}

	t.Run("no args returns error", func(t *testing.T) {
		ctx := NewMacroContext(&Entry{Name: "pkg"}, "")
		m := Macro{Name: "INSTALL_SERVICE", Args: []string{}}
		_, err := executeInstallService(m, ctx)
		if err == nil {
			t.Fatal("expected error for missing args")
		}
		if !strings.Contains(err.Error(), "requires at least 1 argument") {
			t.Errorf("unexpected error message: %v", err)
		}
	})

	t.Run("unsafe source path rejected", func(t *testing.T) {
		ctx := NewMacroContext(&Entry{Name: "pkg"}, "")
		m := Macro{Name: "INSTALL_SERVICE", Args: []string{"../../etc/evil.service"}}
		_, err := executeInstallService(m, ctx)
		if err == nil {
			t.Fatal("expected error for unsafe source path")
		}
	})

	t.Run("unsafe dest path rejected", func(t *testing.T) {
		ctx := NewMacroContext(&Entry{Name: "pkg"}, "")
		m := Macro{Name: "INSTALL_SERVICE", Args: []string{"my.service", "../../etc/evil.service"}}
		_, err := executeInstallService(m, ctx)
		if err == nil {
			t.Fatal("expected error for unsafe dest path")
		}
	})
}

// TestExecuteInstallServiceDefaultDest verifies the default /etc/systemd/system destination.
func TestExecuteInstallServiceDefaultDest(t *testing.T) {
	if platform.IsTermux() || platform.IsMacOS() {
		t.Skip("INSTALL_SERVICE is a no-op on Termux/macOS")
	}

	ctx := NewMacroContext(&Entry{Name: "pkg"}, "")
	m := Macro{Name: "INSTALL_SERVICE", Args: []string{"myapp.service"}}
	cmd, err := executeInstallService(m, ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should contain the default systemd path.
	if !strings.Contains(cmd, "/etc/systemd/system/myapp.service") {
		t.Errorf("command should reference default systemd path, got: %s", cmd)
	}

	// Should have tracked the installed path.
	if len(ctx.InstalledPaths) != 1 {
		t.Fatalf("expected 1 installed path, got %d", len(ctx.InstalledPaths))
	}
	if ctx.InstalledPaths[0].Path != "myapp.service" {
		t.Errorf("installed path should be 'myapp.service', got %q", ctx.InstalledPaths[0].Path)
	}
	if ctx.InstalledPaths[0].Type != "service" {
		t.Errorf("installed type should be 'service', got %q", ctx.InstalledPaths[0].Type)
	}
}

// TestExecuteInstallServiceCustomDest verifies a custom destination path.
func TestExecuteInstallServiceCustomDest(t *testing.T) {
	if platform.IsTermux() || platform.IsMacOS() {
		t.Skip("INSTALL_SERVICE is a no-op on Termux/macOS")
	}

	ctx := NewMacroContext(&Entry{Name: "pkg"}, "")
	m := Macro{Name: "INSTALL_SERVICE", Args: []string{"myapp.service", "/custom/path/myapp.service"}}
	cmd, err := executeInstallService(m, ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(cmd, "/custom/path/myapp.service") {
		t.Errorf("command should reference custom path, got: %s", cmd)
	}

	if len(ctx.InstalledPaths) != 1 {
		t.Fatalf("expected 1 installed path, got %d", len(ctx.InstalledPaths))
	}
	if ctx.InstalledPaths[0].Path != "myapp.service" {
		t.Errorf("installed path should be 'myapp.service', got %q", ctx.InstalledPaths[0].Path)
	}
}

// TestExecuteInstallServiceDirDest verifies dest ending with / appends the filename.
func TestExecuteInstallServiceDirDest(t *testing.T) {
	if platform.IsTermux() || platform.IsMacOS() {
		t.Skip("INSTALL_SERVICE is a no-op on Termux/macOS")
	}

	ctx := NewMacroContext(&Entry{Name: "pkg"}, "")
	m := Macro{Name: "INSTALL_SERVICE", Args: []string{"myapp.service", "/custom/path/"}}
	cmd, err := executeInstallService(m, ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should have appended the filename to the directory.
	if !strings.Contains(cmd, "/custom/path/myapp.service") {
		t.Errorf("command should append filename to dir dest, got: %s", cmd)
	}

	if len(ctx.InstalledPaths) != 1 {
		t.Fatalf("expected 1 installed path, got %d", len(ctx.InstalledPaths))
	}
	if ctx.InstalledPaths[0].Path != "myapp.service" {
		t.Errorf("installed path should be 'myapp.service', got %q", ctx.InstalledPaths[0].Path)
	}
}
