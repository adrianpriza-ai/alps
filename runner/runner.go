package runner

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/adrianpriza-ai/alps/priv"
)

// Command represents a structured command with explicit metadata.
// This replaces shell-string execution with typed, safe command construction.
type Command struct {
	Program    string   // Executable to run
	Args       []string // Arguments to the program
	Dir        string   // Working directory (empty = current)
	Env        []string // Environment variables (empty = inherit)
	Privileged bool     // Whether privilege escalation is required
	Shell      bool     // Whether to execute via shell (sh -c)
}

// Runner is the interface for command execution.
// This provides a single execution point for all backends with consistent policy.
type Runner interface {
	Run(ctx context.Context, cmd Command) error
}

// DefaultRunner implements Runner with standard privilege escalation and execution.
type DefaultRunner struct {
	dryRun bool
}

// NewDefaultRunner creates a new DefaultRunner.
func NewDefaultRunner(dryRun bool) *DefaultRunner {
	return &DefaultRunner{dryRun: dryRun}
}

// Run executes a command according to its configuration.
func (r *DefaultRunner) Run(ctx context.Context, cmd Command) error {
	if r.dryRun {
		return r.dryRunCommand(cmd)
	}

	if cmd.Shell {
		return r.runShellCommand(ctx, cmd)
	}
	return r.runDirectCommand(ctx, cmd)
}

// runDirectCommand executes a command directly without shell interpretation.
// This is the default and safest execution method.
func (r *DefaultRunner) runDirectCommand(ctx context.Context, cmd Command) error {
	args := cmd.Args
	if cmd.Privileged {
		// Use structured privilege decision for transparency
		decision, err := priv.DecidePrivilege(append([]string{cmd.Program}, args...)...)
		if err != nil {
			return fmt.Errorf("privilege escalation failed: %w", err)
		}
		// Build command from decision
		privCmd := exec.CommandContext(ctx, decision.Exec, decision.Args...)
		privCmd.Stdout = os.Stdout
		privCmd.Stderr = os.Stderr
		privCmd.Stdin = os.Stdin
		if cmd.Dir != "" {
			privCmd.Dir = cmd.Dir
		}
		if len(cmd.Env) > 0 {
			privCmd.Env = append(os.Environ(), cmd.Env...)
		}
		return privCmd.Run()
	}

	// Direct execution without privilege escalation
	execCmd := exec.CommandContext(ctx, cmd.Program, args...)
	execCmd.Stdout = os.Stdout
	execCmd.Stderr = os.Stderr
	execCmd.Stdin = os.Stdin
	if cmd.Dir != "" {
		execCmd.Dir = cmd.Dir
	}
	if len(cmd.Env) > 0 {
		execCmd.Env = append(os.Environ(), cmd.Env...)
	}
	return execCmd.Run()
}

// runShellCommand executes a command via shell (sh -c).
// This should only be used when explicitly required by the manifest.
func (r *DefaultRunner) runShellCommand(ctx context.Context, cmd Command) error {
	// Construct shell command string
	shellCmd := cmd.Program
	if len(cmd.Args) > 0 {
		shellCmd = shellCmd + " " + strings.Join(cmd.Args, " ")
	}

	shellArgs := []string{"-c", shellCmd}
	if cmd.Privileged {
		// Use structured privilege decision for transparency
		decision, err := priv.DecidePrivilege(append([]string{"sh"}, shellArgs...)...)
		if err != nil {
			return fmt.Errorf("privilege escalation failed: %w", err)
		}
		// Build command from decision
		privCmd := exec.CommandContext(ctx, decision.Exec, decision.Args...)
		privCmd.Stdout = os.Stdout
		privCmd.Stderr = os.Stderr
		privCmd.Stdin = os.Stdin
		if cmd.Dir != "" {
			privCmd.Dir = cmd.Dir
		}
		if len(cmd.Env) > 0 {
			privCmd.Env = append(os.Environ(), cmd.Env...)
		}
		return privCmd.Run()
	}

	// Direct shell execution without privilege escalation
	execCmd := exec.CommandContext(ctx, "sh", shellArgs...)
	execCmd.Stdout = os.Stdout
	execCmd.Stderr = os.Stderr
	execCmd.Stdin = os.Stdin
	if cmd.Dir != "" {
		execCmd.Dir = cmd.Dir
	}
	if len(cmd.Env) > 0 {
		execCmd.Env = append(os.Environ(), cmd.Env...)
	}
	return execCmd.Run()
}

// dryRunCommand prints what would be executed without actually running it.
func (r *DefaultRunner) dryRunCommand(cmd Command) error {
	var cmdStr string
	if cmd.Shell {
		cmdStr = cmd.Program
		if len(cmd.Args) > 0 {
			cmdStr = cmdStr + " " + strings.Join(cmd.Args, " ")
		}
		cmdStr = "sh -c " + cmdStr
	} else {
		cmdStr = cmd.Program
		if len(cmd.Args) > 0 {
			cmdStr = cmdStr + " " + strings.Join(cmd.Args, " ")
		}
	}

	if cmd.Privileged {
		cmdStr = "[priv] " + cmdStr
	}

	if cmd.Dir != "" {
		cmdStr = cmdStr + " (in " + cmd.Dir + ")"
	}

	fmt.Printf("DRY RUN: %s\n", cmdStr)
	return nil
}

// BuildCommand creates a Command from program and arguments.
// This is the preferred way to construct commands - direct execution.
func BuildCommand(program string, args ...string) Command {
	return Command{
		Program: program,
		Args:    args,
		Shell:   false,
	}
}

// BuildShellCommand creates a Command that will be executed via shell.
// This should only be used when shell interpretation is explicitly required.
func BuildShellCommand(shellCmd string) Command {
	return Command{
		Program: shellCmd,
		Shell:   true,
	}
}

// WithPrivilege marks a command as requiring privilege escalation.
func (c Command) WithPrivilege() Command {
	c.Privileged = true
	return c
}

// WithDir sets the working directory for a command.
func (c Command) WithDir(dir string) Command {
	c.Dir = dir
	return c
}

// WithEnv sets environment variables for a command.
func (c Command) WithEnv(env ...string) Command {
	c.Env = env
	return c
}
