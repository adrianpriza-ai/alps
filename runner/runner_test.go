package runner

import (
	"context"
	"testing"
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
