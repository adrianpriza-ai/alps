package more

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"time"
)

// InstalledRecord holds metadata for an installed package.
type InstalledRecord struct {
	Version     string   `json:"version"`
	InstalledAt string   `json:"installed_at"`
	RemoveLines []string `json:"remove_lines,omitempty"`
	PurgeLines  []string `json:"purge_lines,omitempty"`
	Servers     []string `json:"servers,omitempty"`
	Sudo        bool     `json:"sudo,omitempty"`
	// Source is set for packages installed outside alps-more.
	// Format: "github:user/repo" or "gitlab:user/repo"
	Source string `json:"source,omitempty"`
}

// ReadInstalled reads the installed.json state file.
func ReadInstalled() (map[string]InstalledRecord, error) {
	data, err := os.ReadFile(getInstalledFile())
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
		backup := getInstalledFile() + ".bak"
		_ = os.WriteFile(backup, data, 0644)
		warn := "⚠"
		if t := os.Getenv("TERM"); t == "linux" || t == "dumb" || t == "" {
			warn = "!!"
		}
		fmt.Printf("  %s  installed.json is corrupt — backed up to %s, resetting.\n", warn, backup)
		return make(map[string]InstalledRecord), nil
	}
	return records, nil
}

// MarkInstalled updates the installed record (version + timestamp only).
func MarkInstalled(name, version string) error {
	return MarkInstalledRecord(name, InstalledRecord{
		Version:     version,
		InstalledAt: time.Now().Format(time.RFC3339),
	})
}

// MarkInstalledEntry updates the installed record with full entry metadata.
func MarkInstalledEntry(e *Entry) error {
	return MarkInstalledRecord(e.Name, InstalledRecord{
		Version:     e.Version,
		InstalledAt: time.Now().Format(time.RFC3339),
		RemoveLines: append([]string(nil), e.RemoveLines...),
		PurgeLines:  append([]string(nil), e.PurgeLines...),
		Servers:     append([]string(nil), e.Servers...),
		Sudo:        e.Sudo,
		Source:      e.Source,
	})
}

func MarkInstalledRecord(name string, rec InstalledRecord) error {
	records, err := ReadInstalled()
	if err != nil {
		return err
	}

	records[name] = rec

	data, err := json.MarshalIndent(records, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal installed state: %w", err)
	}

	if err := ensureLibDir(); err != nil {
		return fmt.Errorf("failed to create lib dir: %w", err)
	}
	if err := writeCacheFile(getInstalledFile(), data); err != nil {
		return fmt.Errorf("failed to write installed state: %w", err)
	}
	return nil
}

// GetInstalled returns the record for a package.
func GetInstalled(name string) (InstalledRecord, bool) {
	records, err := ReadInstalled()
	if err != nil {
		return InstalledRecord{}, false
	}
	rec, ok := records[name]
	return rec, ok
}

// UnmarkInstalled removes a package from state.
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

	return writeCacheFile(getInstalledFile(), data)
}
