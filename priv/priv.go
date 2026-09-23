package priv

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"strings"

	"github.com/adrianpriza-ai/alps/platform"
)

// PrivilegeMethod represents the privilege escalation method.
type PrivilegeMethod string

const (
	MethodNone   PrivilegeMethod = "none"
	MethodSudo   PrivilegeMethod = "sudo"
	MethodDoas   PrivilegeMethod = "doas"
	MethodPkexec PrivilegeMethod = "pkexec"
	MethodSu     PrivilegeMethod = "su"
)

// PrivilegeDecision represents a structured privilege escalation decision.
type PrivilegeDecision struct {
	Method     PrivilegeMethod // The escalation method to use
	Exec       string          // The executable to run
	Args       []string        // Arguments to the executable
	Reason     string          // Why this method was chosen
	Privileged bool            // Whether privilege escalation is required
}

// IsRoot reports whether the process runs with root privileges. It delegates
// to platform.IsRoot — the single canonical check, based on the effective uid.
func IsRoot() bool {
	return platform.IsRoot()
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

// DecidePrivilege returns a structured privilege escalation decision.
// This provides transparency into which method is chosen and why.
func DecidePrivilege(args ...string) (*PrivilegeDecision, error) {
	if len(args) == 0 {
		return nil, fmt.Errorf("no command provided")
	}

	// Termux owns its prefix — no escalation needed
	if platform.IsTermux() {
		return &PrivilegeDecision{
			Method:     MethodNone,
			Exec:       args[0],
			Args:       args[1:],
			Reason:     "Termux owns its prefix, no escalation needed",
			Privileged: false,
		}, nil
	}

	// macOS uses user directories for most operations
	if platform.IsMacOS() {
		if HasSudo() {
			return &PrivilegeDecision{
				Method:     MethodSudo,
				Exec:       "sudo",
				Args:       args,
				Reason:     "macOS with sudo available",
				Privileged: true,
			}, nil
		}
		return &PrivilegeDecision{
			Method:     MethodNone,
			Exec:       args[0],
			Args:       args[1:],
			Reason:     "macOS without sudo, user directory operation",
			Privileged: false,
		}, nil
	}

	// Already root — run directly
	if IsRoot() {
		return &PrivilegeDecision{
			Method:     MethodNone,
			Exec:       args[0],
			Args:       args[1:],
			Reason:     "Already running as root",
			Privileged: false,
		}, nil
	}

	// sudo available (preferred method)
	if HasSudo() {
		return &PrivilegeDecision{
			Method:     MethodSudo,
			Exec:       "sudo",
			Args:       args,
			Reason:     "sudo available (preferred method)",
			Privileged: true,
		}, nil
	}

	// doas available (OpenBSD/Linux alternative)
	if HasDoas() {
		return &PrivilegeDecision{
			Method:     MethodDoas,
			Exec:       "doas",
			Args:       args,
			Reason:     "doas available (alternative to sudo)",
			Privileged: true,
		}, nil
	}

	// pkexec available (PolicyKit)
	if HasPkexec() {
		return &PrivilegeDecision{
			Method:     MethodPkexec,
			Exec:       "pkexec",
			Args:       args,
			Reason:     "pkexec available (PolicyKit)",
			Privileged: true,
		}, nil
	}

	// su fallback — shell-escape each arg safely to prevent injection.
	if HasSu() {
		escaped := make([]string, len(args))
		for i, a := range args {
			escaped[i] = shellEscape(a)
		}
		return &PrivilegeDecision{
			Method:     MethodSu,
			Exec:       "su",
			Args:       []string{"-c", strings.Join(escaped, " ")},
			Reason:     "su fallback (legacy compatibility)",
			Privileged: true,
		}, nil
	}

	return nil, fmt.Errorf("no privilege escalation available (no sudo, doas, pkexec, or su)")
}

// Command returns a command with privilege escalation (legacy interface).
// Kept for backward compatibility. New code should use DecidePrivilege.
func Command(args ...string) (*exec.Cmd, error) {
	decision, err := DecidePrivilege(args...)
	if err != nil {
		return nil, err
	}

	// DecidePrivilege has already resolved both the executable and its
	// arguments for the privileged and unprivileged cases, so no separate
	// unprivileged path is needed.
	return exec.Command(decision.Exec, decision.Args...), nil
}

// escalatePolicy selects which escalation methods are acceptable.
type escalatePolicy struct {
	allowPkexec bool
	allowSu     bool
}

// accepts reports whether the policy permits m.
func (p escalatePolicy) accepts(m PrivilegeMethod) bool {
	switch m {
	case MethodPkexec:
		return p.allowPkexec
	case MethodSu:
		return p.allowSu
	}
	return true
}

// errNoModernMethod is returned when only pkexec or su is available and the
// policy excludes both.
var errNoModernMethod = errors.New("sudo or doas is required for this operation")

// ensureWith verifies that an acceptable escalation method exists and prompts
// for credentials when the chosen method supports pre-authentication. Method
// selection is delegated to DecidePrivilege so both paths share one preference
// table.
func ensureWith(p escalatePolicy) error {
	decision, err := DecidePrivilege("true")
	if err != nil {
		return err
	}

	if !p.accepts(decision.Method) {
		return errNoModernMethod
	}

	switch decision.Method {
	case MethodNone:
		return nil
	case MethodSudo:
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
	default:
		// doas, pkexec and su prompt when the escalated command runs
		return nil
	}
}

// Ensure gets a valid privilege token, accepting every escalation method.
func Ensure() error {
	return ensureWith(escalatePolicy{allowPkexec: true, allowSu: true})
}

// EnsureModern ensures privilege access using modern methods (sudo/doas) only.
func EnsureModern() error {
	return ensureWith(escalatePolicy{})
}

// CommandModern is like Command but never falls back to pkexec or su.
func CommandModern(args ...string) (*exec.Cmd, error) {
	return commandWith(escalatePolicy{}, args...)
}

// commandWith returns the escalated command for args under the given policy.
// Method selection is delegated to DecidePrivilege so both paths share one
// preference table.
func commandWith(p escalatePolicy, args ...string) (*exec.Cmd, error) {
	decision, err := DecidePrivilege(args...)
	if err != nil {
		return nil, err
	}

	if !p.accepts(decision.Method) {
		return nil, errNoModernMethod
	}

	return exec.Command(decision.Exec, decision.Args...), nil
}

// Invalidate invalidates all available privilege escalation caches.
func Invalidate() error {
	if platform.IsTermux() {
		return nil
	}

	if platform.IsMacOS() {
		// On macOS, invalidate sudo if available
		if HasSudo() {
			if err := exec.Command("sudo", "-k").Run(); err != nil {
				return fmt.Errorf("sudo invalidate failed: %v", err)
			}
		}
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
//
// Every argument is quoted unconditionally so that shell metacharacters that
// were not on an explicit allowlist (e.g. ';', '|', '&', '`', '(', ')', '*',
// '?', '~', '#') can never be interpreted by the su -c fallback shell.
func shellEscape(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
