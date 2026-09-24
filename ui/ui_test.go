package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/adrianpriza-ai/alps/config"
)

// swapStdin replaces os.Stdin for the duration of the test.
func swapStdin(t *testing.T, r *os.File) {
	t.Helper()
	old := os.Stdin
	os.Stdin = os.NewFile(uintptr(r.Fd()), "stdin")
	t.Cleanup(func() { os.Stdin = old })
}

// TestConfirmRefusesOnEOF pins the regression from UC-1: a closed or empty
// stdin is an I/O failure, not a blank "yes by default" answer.
func TestConfirmRefusesOnEOF(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	w.Close() // empty, closed stdin

	swapStdin(t, r)
	if Confirm() {
		t.Fatal("Confirm() returned true when stdin is at EOF; a failed read must not auto-confirm destructive actions")
	}
}

// TestConfirmRefusesOnDevNull covers a real-world trigger of the same bug:
// `alps aur remove firefox < /dev/null` used to remove the package silently.
func TestConfirmRefusesOnDevNull(t *testing.T) {
	f, err := os.Open("/dev/null")
	if err != nil {
		t.Skip("no /dev/null available")
	}
	defer f.Close()

	swapStdin(t, f)
	confirmed := false
	notice := captureStderr(t, func() { confirmed = Confirm() })
	if confirmed {
		t.Fatal("Confirm() returned true with stdin at /dev/null")
	}
	if !strings.Contains(notice, "no terminal available") {
		t.Fatalf("Confirm() notice = %q, want no-terminal warning", notice)
	}
}

// TestConfirmHonoursExplicitN feeds an explicit "n" and asserts Confirm()
// answers false. The pipe is not a terminal, so the non-interactive guard is
// what refuses here; the y/n parsing itself is covered by the promptAnswer
// table test.
func TestConfirmHonoursExplicitN(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.WriteString("n\n"); err != nil {
		t.Fatal(err)
	}
	w.Close()

	swapStdin(t, r)
	if Confirm() {
		t.Fatal("Confirm() returned true for explicit 'n' input")
	}
}

// captureStdout runs fn while redirecting os.Stdout to a pipe and returns
// everything written to it.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	orig := os.Stdout
	os.Stdout = w
	fn()
	os.Stdout = orig
	w.Close()

	var sb strings.Builder
	buf := make([]byte, 4096)
	for {
		n, err := r.Read(buf)
		sb.Write(buf[:n])
		if err != nil {
			break
		}
	}
	return sb.String()
}

func captureStderr(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	orig := os.Stderr
	os.Stderr = w
	fn()
	os.Stderr = orig
	w.Close()

	var sb strings.Builder
	buf := make([]byte, 4096)
	for {
		n, err := r.Read(buf)
		sb.Write(buf[:n])
		if err != nil {
			break
		}
	}
	return sb.String()
}

// TestPrintHeaderShowsFullVersion pins UC-2: a seven-character version such
// as v0.9.48 must not be truncated in either header branch.
func TestPrintHeaderShowsFullVersion(t *testing.T) {
	cfg := &config.Config{
		Version: "v0.9.48",
		Style:   config.Style{ShowHeader: true},
	}

	for _, term := range []string{"linux", "xterm-256color"} {
		t.Setenv("TERM", term)
		out := captureStdout(t, func() { PrintHeader(cfg) })
		if !strings.Contains(out, "v0.9.48") {
			t.Errorf("header with TERM=%s does not show the full version v0.9.48; got:\n%s", term, out)
		}
		if strings.Contains(out, "v0.9.4 ") {
			t.Errorf("header with TERM=%s still shows the truncated version; got:\n%s", term, out)
		}
	}
}

// TestPrintHeaderUsesHeaderText pins CC-7: Style.HeaderText replaces the
// hardcoded ALPS title in both header branches, and an empty value falls back
// to ALPS.
func TestPrintHeaderUsesHeaderText(t *testing.T) {
	cfg := &config.Config{
		Version: "v1.2.3",
		Style:   config.Style{ShowHeader: true, HeaderText: "MYSYSTEM"},
	}
	for _, term := range []string{"linux", "xterm-256color"} {
		t.Setenv("TERM", term)
		out := captureStdout(t, func() { PrintHeader(cfg) })
		if !strings.Contains(out, "MYSYSTEM") {
			t.Errorf("header with TERM=%s does not use Style.HeaderText (got:\n%s)", term, out)
		}
	}

	empty := &config.Config{Version: "v1.2.3", Style: config.Style{ShowHeader: true}}
	t.Setenv("TERM", "linux")
	out := captureStdout(t, func() { PrintHeader(empty) })
	if !strings.Contains(out, "ALPS") {
		t.Errorf("header with empty HeaderText does not fall back to ALPS (got:\n%s)", out)
	}
}

// TestPromptAnswerTable covers the documented mapping of user input to
// answers for both defaults, including the historical re-prompt inputs.
func TestPromptAnswerTable(t *testing.T) {
	cases := []struct {
		input      string
		defaultYes bool
		want       bool
	}{
		{"", true, true},
		{"", false, false},
		{"y", true, true},
		{"y", false, true},
		{"Y", true, true},
		{"Y", false, true},
		{"yes", true, true},
		{"YES", false, true},
		{"Yes ", true, true},
		{"n", true, false},
		{"n", false, false},
		{"N", true, false},
		{"no", false, false},
		{"NO", true, false},
		{"garbage", true, false},
		{"garbage", false, false},
		{"  y  ", false, true},
		{"\n", true, true},
		{"\n", false, false},
	}
	for _, tc := range cases {
		if got := promptAnswer(tc.input, tc.defaultYes); got != tc.want {
			t.Errorf("promptAnswer(%q, defaultYes=%v) = %v, want %v", tc.input, tc.defaultYes, got, tc.want)
		}
	}
}

// TestPromptYesNoRefusesWhenStdinNotTerminal asserts the non-interactive
// guard: without a terminal, prompts answer no instead of taking the default.
func TestPromptYesNoRefusesWhenStdinNotTerminal(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	if _, err := w.WriteString("\n"); err != nil {
		t.Fatal(err)
	}
	w.Close()

	swapStdin(t, r)
	if PromptYesNo("Proceed?", true) {
		t.Fatal("PromptYesNo(defaultYes=true) returned true on a non-terminal stdin")
	}
}

// TestReadLineReturnsContentAndTrims covers the shared single-line reader.
func TestReadLineReturnsContentAndTrims(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.WriteString("  hello world  \n"); err != nil {
		t.Fatal(err)
	}
	w.Close()

	swapStdin(t, r)
	if got := ReadLine(); got != "hello world" {
		t.Fatalf("ReadLine() = %q, want %q", got, "hello world")
	}
}

// TestReadLineEOFReturnsEmpty covers the reader's error path.
func TestReadLineEOFReturnsEmpty(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	w.Close()

	swapStdin(t, r)
	if got := ReadLine(); got != "" {
		t.Fatalf("ReadLine() at EOF = %q, want empty", got)
	}
}

// TestSequentialReadsShareBufferedInput pins the shared stdin reader: input
// queued behind the first line (e.g. pasted "y\ny\n") must still be there for
// the next read. Per-call readers swallowed the queued line, so sequential
// prompts hit a premature EOF or hung waiting for already-consumed input.
// PromptYesNo inherits the same reader but has a terminal gate, so this goes
// through ReadLine, which reads the same buffered source.
func TestSequentialReadsShareBufferedInput(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.WriteString("first\nsecond\n"); err != nil {
		t.Fatal(err)
	}
	w.Close()

	swapStdin(t, r)
	if got := ReadLine(); got != "first" {
		t.Fatalf("first ReadLine() = %q, want %q", got, "first")
	}
	if got := ReadLine(); got != "second" {
		t.Fatalf("second ReadLine() = %q, want %q (queued input was swallowed)", got, "second")
	}
}

// TestPromptOutputFormats ensures the [Y/n] / [y/N] hints follow defaultYes.
func TestPromptOutputFormats(t *testing.T) {
	cases := []struct {
		defaultYes bool
		wantSuffix string
	}{
		{true, "[Y/n]"},
		{false, "[y/N]"},
	}
	for _, tc := range cases {
		got := promptSuffix(tc.defaultYes)
		if !strings.HasSuffix(got, tc.wantSuffix) {
			t.Errorf("promptSuffix(defaultYes=%v) = %q, want suffix %q", tc.defaultYes, got, tc.wantSuffix)
		}
	}
}

// TestDiagnosticSurfacesUnreadableState pins UC-6: when installed.json
// cannot be read, the diagnostic reports the failure instead of presenting
// "0 package(s)" as a fact.
func TestDiagnosticSurfacesUnreadableState(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root: permission errors cannot be produced")
	}

	stateDir := t.TempDir()
	stateFile := filepath.Join(stateDir, "installed.json")
	if err := os.WriteFile(stateFile, []byte(`{"alps":{}}`), 0o000); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ALPS_LIB_DIR", stateDir)

	cfg := &config.Config{Version: "v1.2.3", Style: config.Style{ShowHeader: false}}
	out := captureStdout(t, func() { PrintDiagnostic(cfg) })

	if strings.Contains(out, "0 package(s)") {
		t.Errorf("diagnostic reports \"0 package(s)\" for an unreadable state file; got:\n%s", out)
	}
	if !strings.Contains(out, "state unreadable") {
		t.Errorf("diagnostic does not report the unreadable state file; got:\n%s", out)
	}
}

func TestDiagnosticSurfacesCorruptState(t *testing.T) {
	stateDir := t.TempDir()
	stateFile := filepath.Join(stateDir, "installed.json")
	if err := os.WriteFile(stateFile, []byte(`{"broken":`), 0644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ALPS_LIB_DIR", stateDir)

	cfg := &config.Config{Version: "v1.2.3", Style: config.Style{ShowHeader: false}}
	out := captureStdout(t, func() { PrintDiagnostic(cfg) })

	if strings.Contains(out, "0 package(s)") {
		t.Errorf("diagnostic reports \"0 package(s)\" for corrupt state; got:\n%s", out)
	}
	if !strings.Contains(out, "state unreadable") || !strings.Contains(out, "installed state is corrupt") {
		t.Errorf("diagnostic does not surface corrupt state; got:\n%s", out)
	}
}
