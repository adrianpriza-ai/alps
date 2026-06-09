package snap

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/adrianpriza-ai/alps/priv"
)

// IsAvailable checks if snapd is running.
func IsAvailable() bool {
	if _, err := exec.LookPath("snap"); err != nil {
		return false
	}
	if _, err := os.Stat("/etc/apt/preferences.d/nosnap.pref"); err == nil {
		return false
	}
	return exec.Command("systemctl", "is-active", "--quiet", "snapd").Run() == nil
}

// Install installs packages via snap.
func Install(pkgNames []string, classic bool) error {
	args := append([]string{"snap", "install"}, pkgNames...)
	if classic {
		args = append(args, "--classic")
	}
	cmd, err := priv.Command(args...)
	if err != nil {
		return err
	}
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
	cmd, err := priv.Command("snap", "remove", pkgName)
	if err != nil {
		return err
	}
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("snap remove failed: %w", err)
	}
	return nil
}

// Search searches snap store.
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

// Update updates snap packages.
func Update() error {
	cmd, err := priv.Command("snap", "refresh")
	if err != nil {
		return err
	}
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("snap refresh failed: %w", err)
	}
	return nil
}

// Exists checks if a package exists in snap store.
func Exists(pkgName string) bool {
	out, err := exec.Command("snap", "find", "--narrow", pkgName).Output()
	if err != nil {
		return false
	}
	lines := strings.Split(string(out), "\n")
	for _, line := range lines[1:] {
		fields := strings.Fields(line)
		if len(fields) > 0 && strings.EqualFold(fields[0], pkgName) {
			return true
		}
	}
	return false
}
