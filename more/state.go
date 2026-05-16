package more

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"time"
)

const installedFile = "/var/cache/alps/more/installed.json"

// InstalledRecord holds metadata for an installed package.
type InstalledRecord struct {
	Version     string `json:"version"`
	InstalledAt string `json:"installed_at"`
}

// ReadInstalled reads the installed.json state file.
// If the file is corrupt, backs it up and returns an empty map.
func ReadInstalled() (map[string]InstalledRecord, error) {
	data, err := os.ReadFile(installedFile)
	if os.IsNotExist(err) {
		return make(map[string]InstalledRecord), nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to read installed state: %w", err)
	}

	// Empty file is valid — treat as empty map
	if len(bytes.TrimSpace(data)) == 0 {
		return make(map[string]InstalledRecord), nil
	}

	var records map[string]InstalledRecord
	if err := json.Unmarshal(data, &records); err != nil {
		// Corrupt JSON — back up and reset so alps keeps working
		backup := installedFile + ".bak"
		_ = os.WriteFile(backup, data, 0644)
		fmt.Printf("  %s  installed.json is corrupt — backed up to %s, resetting.\n", symWarn(), backup)
		return make(map[string]InstalledRecord), nil
	}
	return records, nil
}

// MarkInstalled writes/updates the installed record for a package.
// Requires sudo (cacheDir is root-owned).
func MarkInstalled(name, version string) error {
	records, err := ReadInstalled()
	if err != nil {
		return err
	}

	records[name] = InstalledRecord{
		Version:     version,
		InstalledAt: time.Now().Format(time.RFC3339),
	}

	data, err := json.MarshalIndent(records, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal installed state: %w", err)
	}

	// Ensure cache dir exists
	mkdirCmd := exec.Command("sudo", "mkdir", "-p", cacheDir)
	mkdirCmd.Stdout = os.Stdout
	mkdirCmd.Stderr = os.Stderr
	if err := mkdirCmd.Run(); err != nil {
		return fmt.Errorf("failed to create cache dir: %w", err)
	}

	write := exec.Command("sudo", "tee", installedFile)
	write.Stdin = bytes.NewReader(data)
	write.Stdout = io.Discard
	write.Stderr = os.Stderr
	if err := write.Run(); err != nil {
		return fmt.Errorf("failed to write installed state: %w", err)
	}

	return nil
}

// GetInstalled returns the record for a single package, and whether it exists.
func GetInstalled(name string) (InstalledRecord, bool) {
	records, err := ReadInstalled()
	if err != nil {
		return InstalledRecord{}, false
	}
	rec, ok := records[name]
	return rec, ok
}

// UnmarkInstalled removes a package from the installed state file.
func UnmarkInstalled(name string) error {
	records, err := ReadInstalled()
	if err != nil {
		return err
	}

	delete(records, name)

	data, err := json.MarshalIndent(records, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal installed state: %w", err)
	}

	write := exec.Command("sudo", "tee", installedFile)
	write.Stdin = bytes.NewReader(data)
	write.Stdout = io.Discard
	write.Stderr = os.Stderr
	return write.Run()
}
