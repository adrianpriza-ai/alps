package runner

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/adrianpriza-ai/alps/platform"
	"github.com/adrianpriza-ai/alps/priv"
)

func TestBuildCommand(t *testing.T) {
	cmd := BuildCommand("echo", "hello", "world")
	if cmd.Program != "echo" {
		t.Errorf("Expected program 'echo', got '%s'", cmd.Program)
	}
	if len(cmd.Args) != 2 {
		t.Errorf("Expected 2 args, got %d", len(cmd.Args))
	}
	if cmd.Shell {
		t.Error("Expected Shell=false for BuildCommand")
	}
	if cmd.Privileged {
		t.Error("Expected Privileged=false for BuildCommand")
	}
}

func TestBuildShellCommand(t *testing.T) {
	cmd := BuildShellCommand("echo hello")
	if cmd.Program != "echo hello" {
		t.Errorf("Expected program 'echo hello', got '%s'", cmd.Program)
	}
	if !cmd.Shell {
		t.Error("Expected Shell=true for BuildShellCommand")
	}
	if cmd.Privileged {
		t.Error("Expected Privileged=false for BuildShellCommand")
	}
}

func TestCommandWithPrivilege(t *testing.T) {
	cmd := BuildCommand("ls").WithPrivilege()
	if !cmd.Privileged {
		t.Error("Expected Privileged=true after WithPrivilege")
	}
}

func TestCommandWithDir(t *testing.T) {
	cmd := BuildCommand("ls").WithDir("/tmp")
	if cmd.Dir != "/tmp" {
		t.Errorf("Expected Dir='/tmp', got '%s'", cmd.Dir)
	}
}

func TestCommandWithEnv(t *testing.T) {
	cmd := BuildCommand("ls").WithEnv("FOO=bar", "BAZ=qux")
	if len(cmd.Env) != 2 {
		t.Errorf("Expected 2 env vars, got %d", len(cmd.Env))
	}
}

func TestDryRunCommand(t *testing.T) {
	r := NewDefaultRunner(true)
	cmd := BuildCommand("echo", "test")
	err := r.Run(context.Background(), cmd)
	if err != nil {
		t.Errorf("Dry run should not error, got: %v", err)
	}
}

func TestDefaultRunnerDryRun(t *testing.T) {
	r := NewDefaultRunner(true)
	if !r.dryRun {
		t.Error("Expected dryRun=true")
	}
}

func TestRunDirectCommand(t *testing.T) {
	r := NewDefaultRunner(false)
	if err := r.Run(context.Background(), BuildCommand("true")); err != nil {
		t.Errorf("Run(true) failed: %v", err)
	}
}

func TestRunPrivilegedWithoutEscalationMethod(t *testing.T) {
	if priv.IsRoot() || platform.IsMacOS() || platform.IsTermux() {
		t.Skip("DecidePrivilege only fails on unprivileged Linux with no escalation tools")
	}
	t.Setenv("PATH", t.TempDir())

	r := NewDefaultRunner(false)
	err := r.Run(context.Background(), BuildCommand("true").WithPrivilege())
	if err == nil {
		t.Fatal("expected error when no escalation method is available")
	}
	if !strings.Contains(err.Error(), "privilege escalation failed") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestDryRunWritesCommandToWriter(t *testing.T) {
	var buf bytes.Buffer
	r := NewDefaultRunner(true).WithWriter(&buf)
	if err := r.Run(context.Background(), BuildCommand("false")); err != nil {
		t.Fatalf("dry run should not error, got: %v", err)
	}
	if !strings.Contains(buf.String(), "DRY-RUN: false") {
		t.Errorf("dry-run output %q missing command text", buf.String())
	}
}

func TestDryRunMarksPrivilegedCommand(t *testing.T) {
	var buf bytes.Buffer
	r := NewDefaultRunner(true).WithWriter(&buf)
	cmd := BuildCommand("pacman", "-S", "foo").WithPrivilege()
	if err := r.Run(context.Background(), cmd); err != nil {
		t.Fatalf("dry run should not error, got: %v", err)
	}
	if !strings.Contains(buf.String(), "[priv]") {
		t.Errorf("dry-run output %q missing [priv] marker", buf.String())
	}
}
