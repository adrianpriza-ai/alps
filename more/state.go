package more

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/adrianpriza-ai/alps/platform"
	"github.com/adrianpriza-ai/alps/runner"
)

var (
	lockTimeout       = 10 * time.Second
	lockRetryInterval = 100 * time.Millisecond
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

	if len(bytes.TrimSpace(data)) == 0 {
		return nil, fmt.Errorf("installed state is corrupt: file is empty")
	}

	var records map[string]InstalledRecord
	if err := json.Unmarshal(data, &records); err != nil {
		return nil, fmt.Errorf("installed state is corrupt: %w", err)
	}
	if records == nil {
		return nil, fmt.Errorf("installed state is corrupt: expected a JSON object")
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
	// The name becomes the installed.json key and later a completion word;
	// only package-name-charset values may be persisted.
	if err := platform.ValidatePkgName(name); err != nil {
		return fmt.Errorf("refusing to record installed entry: %w", err)
	}
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
// The lock lives next to installed.json so every writer — root or
// unprivileged — serializes on the same file. When the state directory is
// root-owned and the current user cannot create the lock directly, it is
// seeded through the same privileged channel writeCacheFile uses for the
// state file itself. A user-cache fallback remains only for environments
// where even the privileged seed fails.
func acquireInstalledLock() (*os.File, error) {
	lockPath := getInstalledFile() + ".lock"

	lock, err := openAndLockFile(lockPath)
	if err == nil {
		return lock, nil
	}
	if seedErr := seedLockFile(lockPath); seedErr == nil {
		if lock, err := openAndLockFile(lockPath); err == nil {
			return lock, nil
		}
	}

	if home, homeErr := os.UserHomeDir(); homeErr == nil {
		if lock, fallbackErr := openAndLockFile(filepath.Join(home, ".cache", "alps", "installed.json.lock")); fallbackErr == nil {
			return lock, nil
		}
	}
	return nil, fmt.Errorf("cannot lock installed state: %w", err)
}

// seedLockFile creates an empty lock file through the privileged install path
// used for the state file, so unprivileged writers can share the canonical
// lock with root writers.
func seedLockFile(path string) error {
	r := runner.NewDefaultRunner(false)
	cmd := runner.BuildCommand("install", "-m", "0644", "/dev/null", path).WithPrivilege()
	return r.Run(context.Background(), cmd)
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
	// Non-blocking attempt with a bounded retry so a hung process does not
	// block state writes forever.
	deadline := time.Now().Add(lockTimeout)
	for {
		if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err == nil {
			return f, nil
		}
		if time.Now().After(deadline) {
			f.Close()
			return nil, fmt.Errorf("timeout waiting for lock on %s", path)
		}
		time.Sleep(lockRetryInterval)
	}
}
