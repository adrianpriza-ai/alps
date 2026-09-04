package more

import (
	"context"
	"fmt"
	"os"

	"github.com/adrianpriza-ai/alps/platform"
	"github.com/adrianpriza-ai/alps/runner"
)

// cleanupOwnedItems removes tracked items with proper privilege handling.
// Uses sudo on non-Termux systems unless already root.
func cleanupOwnedItems(items []OwnedItem) {
	if len(items) == 0 {
		return
	}

	style := currentStyle()
	symOK := style.SymOK
	symWarn := style.SymWarn

	fmt.Printf("  cleaning up owned items (%d items)...\n", len(items))

	// Check if we need sudo
	needSudo := !platform.IsTermux() && !platform.IsMacOS() && !platform.IsRoot()

	for i := len(items) - 1; i >= 0; i-- {
		item := items[i]

		switch item.Type {
		case "file":
			if err := removePathWithSudo(item.Path, needSudo); err != nil {
				fmt.Printf("  %s  failed to remove file %s: %v\n", symWarn, item.Path, err)
			} else {
				fmt.Printf("  %s  removed file %s\n", symOK, item.Path)
			}
		case "dir":
			if err := removeDir(item.Path); err != nil {
				fmt.Printf("  %s  failed to remove directory %s: %v\n", symWarn, item.Path, err)
			} else {
				fmt.Printf("  %s  removed directory %s\n", symOK, item.Path)
			}
		case "symlink":
			if err := removePathWithSudo(item.Path, needSudo); err != nil {
				fmt.Printf("  %s  failed to remove symlink %s: %v\n", symWarn, item.Path, err)
			} else {
				fmt.Printf("  %s  removed symlink %s\n", symOK, item.Path)
			}
		case "service":
			if err := removeService(item.Path); err != nil {
				fmt.Printf("  %s  failed to remove service %s: %v\n", symWarn, item.Path, err)
			} else {
				fmt.Printf("  %s  removed service %s\n", symOK, item.Path)
			}
		}
	}
}

// removePathWithSudo removes a file or symlink with optional sudo privilege escalation.
// Used by cleanupOwnedItems() for both file and symlink removal.
func removePathWithSudo(path string, useSudo bool) error {
	r := runner.NewDefaultRunner(false)
	cmd := runner.BuildCommand("rm", "-f", path)
	if useSudo && !(platform.IsTermux() || platform.IsMacOS()) {
		cmd = cmd.WithPrivilege()
	}
	return r.Run(context.Background(), cmd)
}

// removeDir removes a directory using rmdir (only works on empty dirs).
// Uses the runner package for consistent privilege escalation on systems
// where the directory may require elevated permissions to remove.
func removeDir(path string) error {
	r := runner.NewDefaultRunner(false)
	cmd := runner.BuildCommand("rmdir", path).WithPrivilege()
	if err := r.Run(context.Background(), cmd); err != nil {
		fmt.Fprintf(os.Stderr, "  warning: could not remove directory %s (not empty?): %v\n", path, err)
		return err
	}
	return nil
}

// removeService stops, disables, and removes a systemd service file.
func removeService(service string) error {
	if platform.IsTermux() || platform.IsMacOS() {
		return nil
	}

	r := runner.NewDefaultRunner(false)

	// Stop the service
	stopCmd := runner.BuildCommand("systemctl", "stop", service).WithPrivilege()
	_ = r.Run(context.Background(), stopCmd)

	// Disable the service
	disableCmd := runner.BuildCommand("systemctl", "disable", service).WithPrivilege()
	_ = r.Run(context.Background(), disableCmd)

	// Remove the service file
	serviceFile := "/etc/systemd/system/" + service
	if _, err := os.Stat(serviceFile); err == nil {
		removePathWithSudo(serviceFile, true)
	}

	return nil
}

// cleanupTempFiles removes the per-run scratch directory holding the
// execution manifest and its temp scripts. Only this run's directory is
// touched, so a concurrent alps run's manifest/scripts are never clobbered.
func cleanupTempFiles() {
	if runScratchDir == "" {
		return
	}
	_ = os.RemoveAll(runScratchDir)
	runScratchDir = ""
}
