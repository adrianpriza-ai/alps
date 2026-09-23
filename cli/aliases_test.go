package cli

import (
	"strings"
	"testing"

	"github.com/adrianpriza-ai/alps/config"
)

func TestResolveCmd(t *testing.T) {
	cfg := config.Load()

	tests := []struct {
		name    string
		cmd     string
		want    string
		wantErr bool
	}{
		{"hard command", "install", "install", false},
		{"hard command help", "help", "help", false},
		{"hard command repo", "repo", "repo", false},
		{"hard command aur", "aur", "aur", false},
		{"unknown command", "unknown", "", true},
		{"default alias ins", "ins", "install", false},
		{"default alias up", "up", "update", false},
		{"default alias rm", "rm", "remove", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ResolveCmd(tt.cmd, cfg)
			if (err != nil) != tt.wantErr {
				t.Errorf("ResolveCmd() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want && !tt.wantErr {
				t.Errorf("ResolveCmd() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestResolveSubCmd(t *testing.T) {
	cfg := config.Load()

	tests := []struct {
		name    string
		system  string
		subcmd  string
		want    string
		wantErr bool
	}{
		{"aur install", "aur", "install", "install", false},
		{"aur search", "aur", "search", "search", false},
		{"repo update", "repo", "update", "update", false},
		{"repo install", "repo", "install", "install", false},
		{"unknown aur subcmd", "aur", "unknown", "", true},
		{"unknown repo subcmd", "repo", "invalid", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ResolveSubCmd(tt.system, tt.subcmd, cfg)
			if (err != nil) != tt.wantErr {
				t.Errorf("ResolveSubCmd() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want && !tt.wantErr {
				t.Errorf("ResolveSubCmd() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestResolveListAction(t *testing.T) {
	cfg := config.Load()

	tests := []struct {
		name   string
		action string
		want   string
	}{
		{"install action", "install", "install"},
		{"remove action", "remove", "remove"},
		{"default alias add", "add", "install"},
		{"default alias del", "del", "remove"},
		{"unknown action", "unknown", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ResolveListAction(tt.action, cfg)
			if got != tt.want {
				t.Errorf("ResolveListAction() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestCommandsPinsContract pins the Commands() contract: it exposes the
// typeable top-level commands (including completion), is sorted, and omits
// the flag spellings.
func TestCommandsPinsContract(t *testing.T) {
	cmds := Commands()

	found := false
	for _, c := range cmds {
		if c == "completion" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Commands() = %v, want it to contain \"completion\"", cmds)
	}

	for _, flag := range []string{"--help", "-h", "--version"} {
		for _, c := range cmds {
			if c == flag {
				t.Errorf("Commands() contains flag spelling %q", flag)
			}
		}
	}

	for i := 1; i < len(cmds); i++ {
		if cmds[i-1] > cmds[i] {
			t.Fatalf("Commands() is not sorted: %q before %q", cmds[i-1], cmds[i])
		}
	}
}

// TestSubCmdsContainsAURUpdate pins CC-1: the aur table must offer
// update/upgrade so help and completions can show them, and SubCmds must
// agree with ValidSubCmds.
func TestSubCmdsContainsAURUpdate(t *testing.T) {
	for _, want := range []string{"update", "upgrade"} {
		found := false
		for _, s := range SubCmds("aur") {
			if s == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("SubCmds(\"aur\") does not contain %q; got %v", want, SubCmds("aur"))
		}
	}

	if SubCmds("no-such-system") != nil {
		t.Errorf("SubCmds for unknown system = %v, want nil", SubCmds("no-such-system"))
	}

	got := strings.Join(SubCmds("aur"), " ")
	if want := strings.Join(ValidSubCmds("aur"), " "); got != want {
		t.Errorf("SubCmds(%q) = %q, want %q", "aur", got, want)
	}
}

// TestSubCmdHelpCoversEverySubCmd guards against drift inside cli itself:
// every valid subcommand must have a help row, and every help row must name
// a valid subcommand.
func TestSubCmdHelpCoversEverySubCmd(t *testing.T) {
	for _, sys := range []string{"aur", "repo", "winget", "flatpak", "snap"} {
		rows := SubCmdHelp(sys)
		if rows == nil {
			t.Fatalf("SubCmdHelp(%q) = nil, want rows", sys)
		}
		if got, want := len(rows), len(ValidSubCmds(sys)); got != want {
			t.Errorf("SubCmdHelp(%q) has %d rows, want %d", sys, got, want)
		}
		for _, row := range rows {
			fields := strings.Fields(row[0])
			if len(fields) < 2 {
				t.Errorf("SubCmdHelp(%q) row %q is malformed", sys, row[0])
				continue
			}
			if fields[0] != sys || !IsValidSubCmd(sys, fields[1]) {
				t.Errorf("SubCmdHelp(%q) row %q is not a valid subcommand", sys, row[0])
			}
			if row[1] == "" {
				t.Errorf("SubCmdHelp(%q) row %q has no description", sys, row[0])
			}
		}
	}

	if SubCmdHelp("no-such-system") != nil {
		t.Errorf("SubCmdHelp for unknown system = %v, want nil", SubCmdHelp("no-such-system"))
	}
}

// TestCoreHelpRowsCoversCommands pins that CoreHelpRows covers every
// non-flag, non-subsystem command returned by Commands(), so a command
// added to hardCommands without a help row fails the test instead of
// vanishing from the help screen. Subsystems are rendered in their own
// sections by SubCmdHelp.
func TestCoreHelpRowsCoversCommands(t *testing.T) {
	subsystems := map[string]bool{
		"repo": true, "aur": true, "winget": true, "flatpak": true, "snap": true,
	}
	rows := CoreHelpRows()
	labelled := make(map[string]bool, len(rows))
	for _, row := range rows {
		name := strings.Fields(row[0])[0]
		labelled[name] = true
		if subsystems[name] {
			t.Errorf("CoreHelpRows has a row for subsystem %q; subsystems render in their own sections", name)
		}
		if row[1] == "" {
			t.Errorf("CoreHelpRows row %q has no description", row[0])
		}
	}
	for _, cmd := range Commands() {
		if subsystems[cmd] {
			continue
		}
		if !labelled[cmd] {
			t.Errorf("CoreHelpRows has no row for command %q", cmd)
		}
	}
}

// TestResolveCmdCaseInsensitive pins CC-2: the top-level command word is
// matched case-insensitively and resolves to the canonical lower-case
// spelling, while alias lookups keep their case.
func TestResolveCmdCaseInsensitive(t *testing.T) {
	cfg := config.Load()

	for _, tc := range []struct{ cmd, want string }{
		{"INSTALL", "install"},
		{"Install", "install"},
		{"Version", "version"},
		{"AUR", "aur"},
	} {
		got, err := ResolveCmd(tc.cmd, cfg)
		if err != nil {
			t.Errorf("ResolveCmd(%q) returned error: %v", tc.cmd, err)
			continue
		}
		if got != tc.want {
			t.Errorf("ResolveCmd(%q) = %q, want %q", tc.cmd, got, tc.want)
		}
	}

	if got, err := ResolveCmd("rm", cfg); err != nil || got != "remove" {
		t.Errorf("ResolveCmd(\"rm\") = %q, %v, want \"remove\", nil", got, err)
	}
	if _, err := ResolveCmd("UNKNOWN-COMMAND", cfg); err == nil {
		t.Error("ResolveCmd(\"UNKNOWN-COMMAND\") expected an error, got nil")
	}
}

func TestIsHardCommand(t *testing.T) {
	tests := []struct {
		cmd  string
		want bool
	}{
		{"install", true},
		{"help", true},
		{"repo", true},
		{"aur", true},
		{"unknown", false},
		{"myalias", false},
	}

	for _, tt := range tests {
		t.Run(tt.cmd, func(t *testing.T) {
			got := IsHardCommand(tt.cmd)
			if got != tt.want {
				t.Errorf("IsHardCommand() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIsValidSubCmd(t *testing.T) {
	tests := []struct {
		name   string
		system string
		subcmd string
		want   bool
	}{
		{"aur install", "aur", "install", true},
		{"aur search", "aur", "search", true},
		{"aur invalid", "aur", "invalid", false},
		{"repo install", "repo", "install", true},
		{"repo update", "repo", "update", true},
		{"repo invalid", "repo", "invalid", false},
		{"invalid system", "invalid", "install", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsValidSubCmd(tt.system, tt.subcmd)
			if got != tt.want {
				t.Errorf("IsValidSubCmd() = %v, want %v", got, tt.want)
			}
		})
	}
}
