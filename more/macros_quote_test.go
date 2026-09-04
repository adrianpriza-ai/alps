package more

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/adrianpriza-ai/alps/platform"
)

// TestShellQuote verifies the single-quote escaping helper produces inert
// literals for every shell metacharacter class.
func TestShellQuote(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"plain", "'plain'"},
		{"with space", "'with space'"},
		{"a'b", `'a'\''b'`},
		{"$(rm -rf /)", "'$(rm -rf /)'"},
		{"`id`", "'`id`'"},
		{"x; rm -rf /", "'x; rm -rf /'"},
		{"$HOME/.bin/tool", "'$HOME/.bin/tool'"},
		{"", "''"},
	}
	for _, tc := range cases {
		if got := shellQuote(tc.in); got != tc.want {
			t.Errorf("shellQuote(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// TestExecuteInstallBinQuotesPaths verifies INSTALL_BIN keeps a source/dest
// with spaces and command substitution as single literal arguments.
func TestExecuteInstallBinQuotesPaths(t *testing.T) {
	m := Macro{Name: "INSTALL_BIN", Args: []string{"my tool.bin", "/opt/alps tools/$(touch pwned)"}}
	ctx := testCtx()
	cmd, err := executeInstallBin(m, ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// The command substitution must stay inside single quotes — the shell
	// must never see a bare $(...) to execute.
	wantCp := "cp 'my tool.bin' '/opt/alps tools/$(touch pwned)'"
	if !strings.Contains(cmd, wantCp) {
		t.Errorf("expected quoted cp in command, got: %s", cmd)
	}
	// The parent directory is computed in Go and quoted too (no $(dirname)).
	if !strings.Contains(cmd, "mkdir -p '/opt/alps tools'") {
		t.Errorf("expected quoted mkdir of the parent dir, got: %s", cmd)
	}
	if len(ctx.InstalledPaths) != 1 {
		t.Fatalf("expected 1 tracked path, got %d", len(ctx.InstalledPaths))
	}
	if ctx.InstalledPaths[0].Path != "/opt/alps tools/$(touch pwned)" {
		t.Errorf("tracked path = %q", ctx.InstalledPaths[0].Path)
	}
}

// TestExecuteExtractQuotesArchive verifies EXTRACT emits a quoted archive name
// so an archive with spaces or metacharacters cannot inject commands.
func TestExecuteExtractQuotesArchive(t *testing.T) {
	ctx := NewMacroContext(&Entry{Name: "pkg", Safety: "free"}, "")
	m := Macro{Name: "EXTRACT", Args: []string{"a; touch /tmp/pwned.tar.gz"}}
	cmd, err := executeExtract(m, ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "tar -xzf 'a; touch /tmp/pwned.tar.gz'"
	if cmd != want {
		t.Errorf("got %q, want %q", cmd, want)
	}
}

// TestExecuteBashRunQuotesArgs verifies BASH_RUN passes each trailing argument
// through as its own single-quoted word (metacharacters stay inert).
func TestExecuteBashRunQuotesArgs(t *testing.T) {
	ctx := NewMacroContext(&Entry{Name: "pkg", Safety: "free"}, "")
	ctx.BuildDir = t.TempDir()
	m := Macro{Name: "BASH_RUN", Args: []string{"setup.sh", "--flag", "$(id); touch /tmp/x"}}
	cmd, err := executeBashRun(m, ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "bash '" + filepath.Join(ctx.BuildDir, "setup.sh") + "' '--flag' '$(id); touch /tmp/x'"
	if cmd != want {
		t.Errorf("got %q, want %q", cmd, want)
	}
}

// TestExecuteSHQuotesScriptPath verifies SH quotes the script path.
func TestExecuteSHQuotesScriptPath(t *testing.T) {
	ctx := NewMacroContext(&Entry{Name: "pkg", Safety: "free"}, "")
	m := Macro{Name: "SH", Args: []string{"my script.sh"}}
	cmd, err := executeSH(m, ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(cmd, "'my script.sh'") {
		t.Errorf("expected quoted script path in %q", cmd)
	}
	// An unquoted interpolation would leave the path bare at the end of the
	// command (a space after "script.sh", then nothing).
	if strings.HasSuffix(cmd, " my script.sh") {
		t.Errorf("script path must not appear unquoted, got %q", cmd)
	}
}

// TestServiceMacrosQuoteName verifies service-control macros quote the name.
func TestServiceMacrosQuoteName(t *testing.T) {
	if platform.IsTermux() || platform.IsMacOS() {
		t.Skip("service macros are a no-op on Termux/macOS")
	}
	ctx := testCtx()
	cases := []struct {
		name     string
		fn       func(Macro, *MacroContext) (string, error)
		wantPref string
	}{
		{"ENABLE_SERVICE", executeEnableService, "systemctl enable "},
		{"DISABLE_SERVICE", executeDisableService, "systemctl disable "},
		{"START_SERVICE", executeStartService, "systemctl start "},
		{"STOP_SERVICE", executeStopService, "systemctl stop "},
		{"RESTART_SERVICE", executeRestartService, "systemctl restart "},
	}
	for _, tc := range cases {
		m := Macro{Name: tc.name, Args: []string{"evil$(id).service"}}
		cmd, err := tc.fn(m, ctx)
		if err != nil {
			t.Fatalf("%s: unexpected error: %v", tc.name, err)
		}
		want := tc.wantPref + "'evil$(id).service'"
		if cmd != want {
			t.Errorf("%s returned %q, want %q", tc.name, cmd, want)
		}
	}
}

// TestUserMacrosQuoteName verifies CREATE_USER/REMOVE_USER quote the username.
func TestUserMacrosQuoteName(t *testing.T) {
	if platform.IsTermux() || platform.IsMacOS() {
		t.Skip("user macros are a no-op on Termux/macOS")
	}
	ctx := testCtx()

	create, err := executeCreateUser(Macro{Name: "CREATE_USER", Args: []string{"bob; rm -rf /"}}, ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if want := "useradd -r -s /bin/false 'bob; rm -rf /'"; create != want {
		t.Errorf("CREATE_USER returned %q, want %q", create, want)
	}

	remove, err := executeRemoveUser(Macro{Name: "REMOVE_USER", Args: []string{"bob; rm -rf /"}}, ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if want := "userdel 'bob; rm -rf /'"; remove != want {
		t.Errorf("REMOVE_USER returned %q, want %q", remove, want)
	}
}
