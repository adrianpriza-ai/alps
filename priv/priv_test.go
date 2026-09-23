package priv

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/adrianpriza-ai/alps/platform"
)

func TestPrivilegeDecisionStructure(t *testing.T) {
	// Test that privilege decision returns structured data
	decision, err := DecidePrivilege("echo", "test")
	if err != nil {
		t.Fatalf("DecidePrivilege failed: %v", err)
	}

	if decision.Exec == "" {
		t.Error("Expected Exec to be set")
	}

	if decision.Reason == "" {
		t.Error("Expected Reason to be set")
	}

	// Should have a valid method
	validMethods := map[PrivilegeMethod]bool{
		MethodNone:   true,
		MethodSudo:   true,
		MethodDoas:   true,
		MethodPkexec: true,
		MethodSu:     true,
	}

	if !validMethods[decision.Method] {
		t.Errorf("Invalid method: %s", decision.Method)
	}
}

func TestDecidePrivilegeNoArgs(t *testing.T) {
	_, err := DecidePrivilege()
	if err == nil {
		t.Error("Expected error for no arguments")
	}
}

func TestShellEscape(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"simple", "'simple'"},
		{"with space", "'with space'"},
		{"with'quote", "'with'\\''quote'"},
		{"with$var", "'with$var'"},
		{"with\\backslash", "'with\\backslash'"},
		{"with\"double", "'with\"double'"},
		{";reboot", "';reboot'"},
		{"$(id)", "'$(id)'"},
		{"a|b", "'a|b'"},
		{"a;rm", "'a;rm'"},
		{"`whoami`", "'`whoami`'"},
		{"a&b", "'a&b'"},
		{"*", "'*'"},
		{"", "''"},
	}

	for _, tt := range tests {
		result := shellEscape(tt.input)
		if result != tt.expected {
			t.Errorf("shellEscape(%q) = %q, expected %q", tt.input, result, tt.expected)
		}
	}
}

// TestShellEscapeRendersSingleWordArg verifies that the escaping never splits
// into multiple shell words and keeps metacharacters inert.
func TestShellEscapeRendersSingleWordArg(t *testing.T) {
	cases := []string{"a;reboot", "x$(id)y", "a|b", "a&b", "`id`", "a b c", "'quoted'", "\";\""}
	for _, in := range cases {
		out := shellEscape(in)
		if len(out) < 2 || out[0] != '\'' || out[len(out)-1] != '\'' {
			t.Errorf("shellEscape(%q) = %q should be fully single-quoted", in, out)
		}
	}
}

func TestPrivilegeMethodString(t *testing.T) {
	methods := []PrivilegeMethod{
		MethodNone,
		MethodSudo,
		MethodDoas,
		MethodPkexec,
		MethodSu,
	}

	for _, method := range methods {
		if string(method) == "" {
			t.Errorf("Method %v should have string representation", method)
		}
	}
}

// stubBin writes an executable stub for name into dir.
func stubBin(t *testing.T, dir, name string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("stub %s: %v", name, err)
	}
}

// escalationTestEnv skips environments where PATH cannot control the decision.
func escalationTestEnv(t *testing.T) {
	t.Helper()
	if IsRoot() || platform.IsMacOS() || platform.IsTermux() {
		t.Skip("method selection is only testable on unprivileged Linux via a controlled PATH")
	}
}

func TestDecidePrivilegeMethodPreference(t *testing.T) {
	escalationTestEnv(t)

	empty := t.TempDir()
	doasOnly := t.TempDir()
	stubBin(t, doasOnly, "doas")
	sudoAndDoas := t.TempDir()
	stubBin(t, sudoAndDoas, "doas")
	stubBin(t, sudoAndDoas, "sudo")

	tests := []struct {
		name string
		path string
		want PrivilegeMethod // empty means DecidePrivilege must fail
	}{
		{"no escalation available", empty, ""},
		{"doas only", doasOnly, MethodDoas},
		{"sudo preferred over doas", sudoAndDoas, MethodSudo},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("PATH", tt.path)
			decision, err := DecidePrivilege("true")
			if tt.want == "" {
				if err == nil {
					t.Fatalf("expected error with empty toolset, got method %s", decision.Method)
				}
				return
			}
			if err != nil {
				t.Fatalf("DecidePrivilege failed: %v", err)
			}
			if decision.Method != tt.want {
				t.Errorf("method = %s, want %s", decision.Method, tt.want)
			}
		})
	}
}

func TestEnsureWithAgreesWithDecidePrivilege(t *testing.T) {
	escalationTestEnv(t)

	empty := t.TempDir()
	doasOnly := t.TempDir()
	stubBin(t, doasOnly, "doas")
	sudoOnly := t.TempDir()
	stubBin(t, sudoOnly, "sudo")
	pkexecOnly := t.TempDir()
	stubBin(t, pkexecOnly, "pkexec")
	suOnly := t.TempDir()
	stubBin(t, suOnly, "su")

	tests := []struct {
		name         string
		path         string
		decideFails  bool
		fullFails    bool
		modernFails  bool
		modernErrMsg string
	}{
		{"nothing available", empty, true, true, true, ""},
		{"doas available", doasOnly, false, false, false, ""},
		{"sudo available", sudoOnly, false, false, false, ""},
		{"pkexec only", pkexecOnly, false, false, true, "sudo or doas is required for this operation"},
		{"su only", suOnly, false, false, true, "sudo or doas is required for this operation"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("PATH", tt.path)

			_, decErr := DecidePrivilege("true")
			fullErr := ensureWith(escalatePolicy{allowPkexec: true, allowSu: true})
			modernErr := ensureWith(escalatePolicy{})

			if (decErr != nil) != tt.decideFails {
				t.Errorf("DecidePrivilege error = %v, wantFails %v", decErr, tt.decideFails)
			}
			if (fullErr != nil) != tt.fullFails {
				t.Errorf("ensureWith(full) error = %v, wantFails %v", fullErr, tt.fullFails)
			}
			if (modernErr != nil) != tt.modernFails {
				t.Errorf("ensureWith(modern) error = %v, wantFails %v", modernErr, tt.modernFails)
			}
			if tt.modernErrMsg != "" && modernErr != nil && modernErr.Error() != tt.modernErrMsg {
				t.Errorf("ensureWith(modern) error = %q, want %q", modernErr.Error(), tt.modernErrMsg)
			}
			if tt.modernErrMsg != "" {
				if _, err := CommandModern("true"); err == nil || err.Error() != tt.modernErrMsg {
					t.Errorf("CommandModern error = %v, want %q", err, tt.modernErrMsg)
				}
			}
		})
	}
}
