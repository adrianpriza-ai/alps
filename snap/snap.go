package snap

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// IsAvailable returns true if snapd is running and not blocked.
func IsAvailable() bool {
	// Check if snap binary exists
	if _, err := exec.LookPath("snap"); err != nil {
		return false
	}
	// Check if snap is blocked (e.g. Linux Mint nosnap.pref)
	if _, err := os.Stat("/etc/apt/preferences.d/nosnap.pref"); err == nil {
		return false
	}
	// Check if snapd is active
	return exec.Command("systemctl", "is-active", "--quiet", "snapd").Run() == nil
}

// Install installs one or more packages via snap.
func Install(pkgNames []string, classic bool) error {
	args := append([]string{"install"}, pkgNames...)
	if classic {
		args = append(args, "--classic")
	}
	cmd := exec.Command("sudo", append([]string{"snap"}, args...)...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("snap install failed: %w", err)
	}
	return nil
}

// Remove removes a snap package.
func Remove(pkgName string) error {
	cmd := exec.Command("sudo", "snap", "remove", pkgName)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("snap remove failed: %w", err)
	}
	return nil
}

// Search searches for packages in the snap store.
func Search(query string) error {
	cmd := exec.Command("snap", "find", query)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("snap search failed: %w", err)
	}
	return nil
}

// List lists installed snap packages.
func List() error {
	cmd := exec.Command("snap", "list")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// Update updates all snap packages.
func Update() error {
	cmd := exec.Command("sudo", "snap", "refresh")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("snap refresh failed: %w", err)
	}
	return nil
}

// NotFound parses snap find output to check if a package exists.
// Returns true if the package was found in snap store.
func Exists(pkgName string) bool {
	out, err := exec.Command("snap", "find", "--narrow", pkgName).Output()
	if err != nil {
		return false
	}
	lines := strings.Split(string(out), "\n")
	for _, line := range lines[1:] { // skip header
		fields := strings.Fields(line)
		if len(fields) > 0 && strings.EqualFold(fields[0], pkgName) {
			return true
		}
	}
	return false
}
