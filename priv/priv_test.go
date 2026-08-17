package priv

import (
	"testing"
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
