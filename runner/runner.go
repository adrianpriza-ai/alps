package runner

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	"github.com/adrianpriza-ai/alps/priv"
)

// Command represents a structured command with explicit metadata.
// This replaces shell-string execution with typed, safe command construction.
// That guarantee only holds for direct commands (Shell=false): a shell command
// is handed to `sh -c` verbatim, without escaping — see BuildShellCommand.
type Command struct {
	Program    string   // Executable to run
	Args       []string // Arguments to the program
	Dir        string   // Working directory (empty = current)
	Env        []string // Environment variables appended over the inherited environment (empty = inherit); keys can be added or overridden, never unset
	Privileged bool     // Whether privilege escalation is required
	Shell      bool     // Whether to execute via shell (sh -c); the command string is passed to the shell unescaped
}

// Runner is the interface for command execution.
// This provides a single execution point for all backends with consistent policy.
type Runner interface {
	Run(ctx context.Context, cmd Command) error
}

// DefaultRunner implements Runner with standard privilege escalation and execution.
type DefaultRunner struct {
	dryRun bool
	out    io.Writer // destination for non-error output such as dry-run lines
}

// NewDefaultRunner creates a new DefaultRunner. Dry-run output is written to
// os.Stdout unless WithWriter redirects it.
func NewDefaultRunner(dryRun bool) *DefaultRunner {
	return &DefaultRunner{dryRun: dryRun, out: os.Stdout}
}

// WithWriter sets the destination for runner output such as dry-run lines.
func (r *DefaultRunner) WithWriter(out io.Writer) *DefaultRunner {
	r.out = out
	return r
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
		privCmd := exec.CommandContext(ctx, decision.Exec, decision.Args...)
		applyIO(privCmd, cmd)
		return privCmd.Run()
	}

	// Direct execution without privilege escalation
	execCmd := exec.CommandContext(ctx, cmd.Program, args...)
	applyIO(execCmd, cmd)
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
		privCmd := exec.CommandContext(ctx, decision.Exec, decision.Args...)
		applyIO(privCmd, cmd)
		return privCmd.Run()
	}

	// Direct shell execution without privilege escalation
	execCmd := exec.CommandContext(ctx, "sh", shellArgs...)
	applyIO(execCmd, cmd)
	return execCmd.Run()
}

// applyIO points the command at the process stdio streams and applies the
// command's working directory and environment overrides.
func applyIO(c *exec.Cmd, cmd Command) {
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	c.Stdin = os.Stdin
	if cmd.Dir != "" {
		c.Dir = cmd.Dir
	}
	if len(cmd.Env) > 0 {
		c.Env = append(os.Environ(), cmd.Env...)
	}
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

	out := r.out
	if out == nil {
		out = os.Stdout
	}
	fmt.Fprintf(out, "DRY-RUN: %s\n", cmdStr)
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

// BuildShellCommand creates a Command that will be executed via `sh -c`.
//
// The command string is passed to the shell verbatim: nothing is escaped or
// quoted, so every shell metacharacter it contains (';', '|', '$(...)',
// globs, quotes) is live and will be interpreted by the shell. It may only
// be used with trusted, already-validated input (today: ALPSMORE manifest
// command lines via more/parser.go). Arguments that must be treated as
// literal data need single-quoting before they reach this constructor.
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

// WithEnv sets environment variables for a command. Keys can only be added
// or overridden on top of the inherited environment — they can never be
// unset. A minimal environment is not reachable through the runner either:
// whenever Command.Env is non-empty the runner prepends os.Environ() to it,
// so the process environment is always inherited in full. A
// security-sensitive caller that needs a minimal environment must build a
// raw exec.Cmd and assign its Env directly (see aur.safeMakepkgEnv for that
// pattern).
func (c Command) WithEnv(env ...string) Command {
	c.Env = env
	return c
}
