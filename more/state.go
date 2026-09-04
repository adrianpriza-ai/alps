package more

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/adrianpriza-ai/alps/platform"
)

// OwnedItem represents a file/directory/service owned by a package
type OwnedItem struct {
	Path string `json:"path"`
	Type string `json:"type"` // "file", "dir", "symlink", "service"
}

// InstalledRecord holds metadata for an installed package.
type InstalledRecord struct {
	Version     string      `json:"version"`
	InstalledAt string      `json:"installed_at"`
	RemoveLines []string    `json:"remove_lines,omitempty"`
	PurgeLines  []string    `json:"purge_lines,omitempty"`
	Servers     []string    `json:"servers,omitempty"`
	Safety      string      `json:"safety,omitempty"`
	OwnedItems  []OwnedItem `json:"owned_items,omitempty"`
	Source      string      `json:"source,omitempty"`
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
		backup := filepath.Clean(getInstalledFile() + ".bak")
		_ = os.WriteFile(backup, data, 0644) // #nosec G703
		fmt.Printf("  %s  installed.json is corrupt — backed up to %s, resetting.\n", currentStyle().SymWarn, backup)
		return make(map[string]InstalledRecord), nil
	}
	return records, nil
}

// MarkInstalledEntryWithOwnedItems updates the installed record with owned items.
func MarkInstalledEntryWithOwnedItems(e *Entry, ownedItems []OwnedItem) error {
	return MarkInstalledRecord(e.Name, InstalledRecord{
		Version:     e.Version,
		InstalledAt: time.Now().Format(time.RFC3339),
		RemoveLines: append([]string(nil), e.RemoveLines...),
		PurgeLines:  append([]string(nil), e.PurgeLines...),
		Servers:     append([]string(nil), e.Servers...),
		Safety:      e.Safety,
		OwnedItems:  ownedItems,
		Source:      e.Source,
	})
}

func MarkInstalledRecord(name string, rec InstalledRecord) error {
	return withInstalledLock(func() error {
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
	})
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
	return withInstalledLock(func() error {
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
	})
}

// stateMu serializes in-process state mutations so that concurrent goroutines
// (e.g. parallel installs within one run) cannot interleave read-modify-write
// cycles and lose each other's records.
var stateMu sync.Mutex

// withInstalledLock runs fn while holding both the in-process mutex and an
// exclusive advisory lock on the state lock file. The mutex covers goroutines
// inside one process; the flock covers separate alps processes running at the
// same time. The kernel releases the flock automatically when the process
// exits, so a crash can never leave a stale lock behind.
func withInstalledLock(fn func() error) error {
	stateMu.Lock()
	defer stateMu.Unlock()

	lock, err := acquireInstalledLock()
	if err != nil {
		return err
	}
	defer lock.Close() // closing the fd releases the flock

	return fn()
}

// acquireInstalledLock opens (creating it if needed) and exclusively locks the
// state lock file, returning the open handle.
//
// The lock lives next to installed.json when the current user can create files
// in that directory — Termux, macOS, root, and tests with a redirected state
// path. On non-root Linux the state directory (/var/lib/alps) is root-owned,
// so it falls back to the user's cache directory; concurrent alps runs by the
// same user then share one lock, which covers the realistic lost-update races
// (two installs launched from the same account). Mixed root/unprivileged
// writers use different locks, which is accepted since that combination is not
// a realistic concurrent scenario.
func acquireInstalledLock() (*os.File, error) {
	candidates := []string{getInstalledFile() + ".lock"}
	if !platform.IsTermux() && !platform.IsMacOS() && !platform.IsRoot() {
		if home, err := os.UserHomeDir(); err == nil {
			candidates = append(candidates, filepath.Join(home, ".cache", "alps", "installed.json.lock"))
		}
	}

	var lastErr error
	for _, path := range candidates {
		lock, err := openAndLockFile(path)
		if err == nil {
			return lock, nil
		}
		lastErr = err
	}
	return nil, fmt.Errorf("cannot lock installed state: %w", lastErr)
}

// openAndLockFile creates the parent directory, opens the lock file, and takes
// an exclusive flock on it. The first candidate is tried first, so on
// non-root Linux the root-owned /var/lib/alps attempt fails with EACCES and
// the caller moves on to the user-cache fallback.
func openAndLockFile(path string) (*os.File, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		f.Close()
		return nil, err
	}
	return f, nil
}
