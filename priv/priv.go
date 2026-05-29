package priv

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// isTermux returns true when running inside Termux on Android.
func isTermux() bool {
	return os.Getenv("TERMUX_VERSION") != "" ||
		os.Getenv("PREFIX") == "/data/data/com.termux/files/usr"
}

// IsRoot returns true if current process is running as root.
func IsRoot() bool {
	return os.Getuid() == 0
}

// HasSudo returns true if sudo binary exists.
func HasSudo() bool {
	_, err := exec.LookPath("sudo")
	return err == nil
}

// HasSu returns true if su binary exists.
func HasSu() bool {
	_, err := exec.LookPath("su")
	return err == nil
}

// Command returns a command with appropriate privilege escalation.
// On Termux: runs directly — no escalation needed or available.
// Priority elsewhere: already root > sudo > su -c > error
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

// Ensure gets a valid privilege token (sudo -v or no-op if root/su/Termux).
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
