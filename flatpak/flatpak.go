package flatpak

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// IsAvailable returns true if flatpak binary exists.
func IsAvailable() bool {
	_, err := exec.LookPath("flatpak")
	return err == nil
}

// Install installs one or more flatpak packages from flathub.
func Install(pkgNames []string, noConfirm bool) error {
	args := []string{"install", "flathub"}
	args = append(args, pkgNames...)
	if noConfirm {
		args = append(args, "-y")
	}
	cmd := exec.Command("flatpak", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("flatpak install failed: %w", err)
	}
	return nil
}

// Remove removes a flatpak package.
func Remove(pkgName string, noConfirm bool) error {
	args := []string{"remove", pkgName}
	if noConfirm {
		args = append(args, "-y")
	}
	cmd := exec.Command("flatpak", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("flatpak remove failed: %w", err)
	}
	return nil
}

// Search searches flathub for packages.
func Search(query string) error {
	cmd := exec.Command("flatpak", "search", query)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("flatpak search failed: %w", err)
	}
	return nil
}

// List lists installed flatpak packages.
func List() error {
	cmd := exec.Command("flatpak", "list", "--app", "--columns=name,application,version")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// Update updates all flatpak packages.
func Update(noConfirm bool) error {
	args := []string{"update"}
	if noConfirm {
		args = append(args, "-y")
	}
	cmd := exec.Command("flatpak", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("flatpak update failed: %w", err)
	}
	return nil
}

// Exists checks if a package exists on flathub.
func Exists(pkgName string) bool {
	out, err := exec.Command("flatpak", "search", "--columns=application", pkgName).Output()
	if err != nil {
		return false
	}
	return strings.Contains(strings.ToLower(string(out)), strings.ToLower(pkgName))
}
