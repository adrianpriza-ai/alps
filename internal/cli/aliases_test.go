package cli

import (
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
		{"extra install", "extra", "install", "install", false},
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

func TestIsHardCommand(t *testing.T) {
	tests := []struct {
		cmd  string
		want bool
	}{
		{"install", true},
		{"help", true},
		{"repo", true},
		{"aur", true},
		{"extra", true},
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
		{"extra install", "extra", "install", true},
		{"extra invalid", "extra", "invalid", false},
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
