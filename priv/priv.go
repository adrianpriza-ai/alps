package priv

import (
	"fmt"
	"os"
	"os/exec"
	"os/user"
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

// HasDoas checks if doas exists (OpenBSD/Linux alternative to sudo).
func HasDoas() bool {
	_, err := exec.LookPath("doas")
	return err == nil
}

// HasPkexec checks if pkexec (PolicyKit) exists.
func HasPkexec() bool {
	_, err := exec.LookPath("pkexec")
	return err == nil
}

// IsInGroup checks if current user is in a specific group.
func IsInGroup(groupName string) bool {
	groups, err := os.Getgroups()
	if err != nil {
		return false
	}
	for _, gid := range groups {
		g, err := user.LookupGroupId(fmt.Sprint(gid))
		if err == nil && g.Name == groupName {
			return true
		}
	}
	return false
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

	// doas available (OpenBSD/Linux alternative)
	if HasDoas() {
		return exec.Command("doas", args...), nil
	}

	// pkexec available (PolicyKit)
	if HasPkexec() {
		return exec.Command("pkexec", args...), nil
	}

	// su fallback — shell-escape each arg safely to prevent injection.
	if HasSu() {
		escaped := make([]string, len(args))
		for i, a := range args {
			escaped[i] = shellEscape(a)
		}
		return exec.Command("su", "-c", strings.Join(escaped, " ")), nil
	}

	return nil, fmt.Errorf("no privilege escalation available (no sudo, doas, pkexec, or su)")
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

	if HasDoas() {
		// doas will prompt when command is run, nothing to pre-auth
		return nil
	}

	if HasPkexec() {
		// pkexec will handle auth via GUI/system, nothing to pre-auth
		return nil
	}

	if HasSu() {
		// su will prompt when command is run, nothing to pre-auth
		return nil
	}

	return fmt.Errorf("no privilege escalation available (no sudo, doas, pkexec, or su)")
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

// CommandModern prefers modern escalation methods (sudo/doas) over legacy (su/pkexec).
func CommandModern(args ...string) (*exec.Cmd, error) {
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

	if HasDoas() {
		return exec.Command("doas", args...), nil
	}

	return nil, fmt.Errorf("sudo or doas is required for this operation")
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

// EnsureModern ensures privilege access using modern methods (sudo/doas) only.
func EnsureModern() error {
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

	if HasDoas() {
		return nil
	}

	return fmt.Errorf("sudo or doas is required for this operation")
}

// Invalidate invalidates all available privilege escalation caches.
func Invalidate() error {
	if isTermux() {
		return nil
	}

	var errs []string

	if HasSudo() {
		if err := exec.Command("sudo", "-k").Run(); err != nil {
			errs = append(errs, fmt.Sprintf("sudo invalidate failed: %v", err))
		}
	}

	if HasDoas() {
		if err := exec.Command("doas", "-L").Run(); err != nil {
			errs = append(errs, fmt.Sprintf("doas invalidate failed: %v", err))
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("invalidation errors: %s", strings.Join(errs, "; "))
	}

	return nil
}

// shellEscape wraps a string in single quotes for safe use inside a shell,
// escaping any embedded single quotes (POSIX sh compatible).
func shellEscape(s string) string {
	if !strings.ContainsAny(s, `'|\"$\\`+" \t\n") {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
