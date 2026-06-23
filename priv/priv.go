package priv

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// isTermux checks if running in Termux.
func isTermux() bool {
	return os.Getenv("TERMUX_VERSION") != "" ||
		os.Getenv("PREFIX") == "/data/data/com.termux/files/usr"
}

// IsRoot checks if running as root.
func IsRoot() bool {
	return os.Getuid() == 0
}

// HasSudo checks if sudo exists.
func HasSudo() bool {
	_, err := exec.LookPath("sudo")
	return err == nil
}

// HasSu checks if su exists.
func HasSu() bool {
	_, err := exec.LookPath("su")
	return err == nil
}

// Command returns a command with privilege escalation.
func Command(args ...string) (*exec.Cmd, error) {
	if len(args) == 0 {
		return nil, fmt.Errorf("no command provided")
	}

	// Termux owns its prefix — no escalation needed
	if isTermux() {
		return exec.Command(args[0], args[1:]...), nil
	}

	// Already root — run directly
	if IsRoot() {
		return exec.Command(args[0], args[1:]...), nil
	}

	// sudo available
	if HasSudo() {
		return exec.Command("sudo", args...), nil
	}

	// su fallback
	if HasSu() {
		joined := strings.Join(args, " ")
		return exec.Command("su", "-c", joined), nil
	}

	return nil, fmt.Errorf("no privilege escalation available (no sudo or su)")
}

// Ensure gets a valid privilege token.
func Ensure() error {
	// Termux owns its prefix — no escalation needed or available
	if isTermux() {
		return nil
	}

	if IsRoot() {
		return nil
	}

	if HasSudo() {
		// Check if sudo token already valid
		if exec.Command("sudo", "-n", "true").Run() == nil {
			return nil
		}
		fmt.Println()
		pw := exec.Command("sudo", "-v")
		pw.Stdout = os.Stdout
		pw.Stderr = os.Stderr
		pw.Stdin = os.Stdin
		return pw.Run()
	}

	if HasSu() {
		// su will prompt when command is run, nothing to pre-auth
		return nil
	}

	return fmt.Errorf("no privilege escalation available (no sudo or su)")
}

// CommandSudoOnly is like Command but never falls back to su.
func CommandSudoOnly(args ...string) (*exec.Cmd, error) {
	if len(args) == 0 {
		return nil, fmt.Errorf("no command provided")
	}

	if isTermux() {
		return exec.Command(args[0], args[1:]...), nil
	}

	if IsRoot() {
		return exec.Command(args[0], args[1:]...), nil
	}

	if HasSudo() {
		return exec.Command("sudo", args...), nil
	}

	return nil, fmt.Errorf("sudo is required for this operation — install sudo or run as root")
}

// EnsureSudoOnly is like Ensure but never accepts su.
func EnsureSudoOnly() error {
	if isTermux() {
		return nil
	}

	if IsRoot() {
		return nil
	}

	if HasSudo() {
		if exec.Command("sudo", "-n", "true").Run() == nil {
			return nil
		}
		fmt.Println()
		pw := exec.Command("sudo", "-v")
		pw.Stdout = os.Stdout
		pw.Stderr = os.Stderr
		pw.Stdin = os.Stdin
		return pw.Run()
	}

	return fmt.Errorf("sudo is required for this operation — install sudo or run as root")
}

// Invalidate invalidates the sudo credentials cache by running sudo -k.
// Only runs when not in Termux and sudo is available.
func Invalidate() error {
	// Only invalidate sudo credentials if not in Termux and sudo is available
	if !isTermux() && HasSudo() {
		return exec.Command("sudo", "-k").Run()
	}
	return nil
}
