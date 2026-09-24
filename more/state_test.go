package more

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

// TestInstalledRecordJSONRoundTrip verifies that marshaling an InstalledRecord
// to JSON and back preserves all fields, including nested OwnedItems.
func TestInstalledRecordJSONRoundTrip(t *testing.T) {
	original := map[string]InstalledRecord{
		"mytool": {
			Version:     "1.2.3",
			InstalledAt: "2026-08-23T10:00:00Z",
			RemoveLines: []string{"rm -f /usr/bin/mytool"},
			PurgeLines:  []string{"rm -rf /etc/mytool"},
			Servers:     []string{"https://example.com/"},
			Safety:      "strict",
			OwnedItems: []OwnedItem{
				{Path: "/usr/bin/mytool", Type: "file"},
				{Path: "/etc/mytool/config", Type: "file"},
				{Path: "/opt/mytool", Type: "dir"},
			},
			Source: "github:user/repo",
		},
	}

	data, err := json.MarshalIndent(original, "", "  ")
	if err != nil {
		t.Fatalf("MarshalIndent failed: %v", err)
	}

	var decoded map[string]InstalledRecord
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	rec, ok := decoded["mytool"]
	if !ok {
		t.Fatal("expected 'mytool' key in decoded map")
	}
	if rec.Version != "1.2.3" {
		t.Errorf("Version = %q, want %q", rec.Version, "1.2.3")
	}
	if rec.InstalledAt != "2026-08-23T10:00:00Z" {
		t.Errorf("InstalledAt = %q, want %q", rec.InstalledAt, "2026-08-23T10:00:00Z")
	}
	if len(rec.OwnedItems) != 3 {
		t.Errorf("OwnedItems length = %d, want 3", len(rec.OwnedItems))
	}
	if rec.OwnedItems[0].Path != "/usr/bin/mytool" || rec.OwnedItems[0].Type != "file" {
		t.Errorf("OwnedItems[0] = %+v, want {Path:/usr/bin/mytool Type:file}", rec.OwnedItems[0])
	}
	if rec.Source != "github:user/repo" {
		t.Errorf("Source = %q, want %q", rec.Source, "github:user/repo")
	}
	if rec.Safety != "strict" {
		t.Errorf("Safety = %q, want %q", rec.Safety, "strict")
	}
}

// TestInstalledRecordEmptyFieldsRoundTrip verifies that empty/zero fields
// survive JSON round-trip and that omitempty tags work correctly.
func TestInstalledRecordEmptyFieldsRoundTrip(t *testing.T) {
	original := map[string]InstalledRecord{
		"minimal": {
			Version: "0.1",
		},
	}

	data, err := json.MarshalIndent(original, "", "  ")
	if err != nil {
		t.Fatalf("MarshalIndent failed: %v", err)
	}

	// The JSON should not contain omitted fields.
	jsonStr := string(data)
	for _, omitted := range []string{"remove_lines", "purge_lines", "servers", "safety", "owned_items", "source"} {
		if strings.Contains(jsonStr, omitted) {
			t.Errorf("JSON should omit %q field when empty, but found in:\n%s", omitted, jsonStr)
		}
	}

	var decoded map[string]InstalledRecord
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	rec := decoded["minimal"]
	if rec.Version != "0.1" {
		t.Errorf("Version = %q, want %q", rec.Version, "0.1")
	}
	if len(rec.RemoveLines) != 0 {
		t.Errorf("RemoveLines should be nil/empty, got %v", rec.RemoveLines)
	}
}

// TestReadInstalledMissingFile verifies ReadInstalled returns an empty map
// (no error) when the installed.json file does not exist.
func TestReadInstalledMissingFile(t *testing.T) {
	redirectInstalledFile(t)

	records, err := ReadInstalled()
	if err != nil {
		t.Fatalf("ReadInstalled() error = %v, want nil", err)
	}
	if len(records) != 0 {
		t.Errorf("ReadInstalled() returned %d records, want 0", len(records))
	}
}

// TestReadInstalledEmptyFile verifies an existing empty state is corruption;
// a missing state file remains the valid representation of no packages.
func TestReadInstalledEmptyFile(t *testing.T) {
	for _, data := range []string{"", " \n\t"} {
		dir := redirectInstalledFile(t)
		path := filepath.Join(dir, "installed.json")
		if err := os.WriteFile(path, []byte(data), 0644); err != nil {
			t.Fatal(err)
		}

		records, err := ReadInstalled()
		if err == nil || !strings.Contains(err.Error(), "installed state is corrupt") {
			t.Fatalf("ReadInstalled() error = %v, want corrupt empty-state error", err)
		}
		if records != nil {
			t.Errorf("ReadInstalled() records = %v, want nil for empty state", records)
		}
	}
}

// TestReadInstalledCorruptJSON verifies corrupt state is surfaced and left
// untouched instead of being reported as a successful empty installation.
func TestReadInstalledCorruptJSON(t *testing.T) {
	dir := redirectInstalledFile(t)
	path := filepath.Join(dir, "installed.json")
	corruptData := []byte(`{"mytool": {version: "broken"}}`)
	if err := os.WriteFile(path, corruptData, 0644); err != nil {
		t.Fatal(err)
	}

	records, err := ReadInstalled()
	if err == nil {
		t.Fatal("ReadInstalled() error = nil, want corrupt-state error")
	}
	if !strings.Contains(err.Error(), "installed state is corrupt") {
		t.Errorf("ReadInstalled() error = %q, want corrupt-state context", err)
	}
	if records != nil {
		t.Errorf("ReadInstalled() records = %v, want nil on corrupt state", records)
	}

	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(after, corruptData) {
		t.Errorf("corrupt state changed after read: got %q, want %q", after, corruptData)
	}
}

// TestInstalledRecordWithEmptyOwnedItems verifies that a record with an empty
// OwnedItems slice marshals correctly and round-trips without data loss.
func TestInstalledRecordWithEmptyOwnedItems(t *testing.T) {
	rec := InstalledRecord{
		Version:     "2.0",
		InstalledAt: "2026-08-23T12:00:00Z",
		OwnedItems:  []OwnedItem{},
	}

	data, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		t.Fatalf("MarshalIndent failed: %v", err)
	}

	var decoded InstalledRecord
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if decoded.Version != "2.0" {
		t.Errorf("Version = %q, want %q", decoded.Version, "2.0")
	}
	// Empty slice is serialized with omitempty, so it deserializes as nil.
	// This is expected Go JSON behavior — the test verifies the round-trip
	// does not produce unexpected data.
	if len(decoded.OwnedItems) != 0 {
		t.Errorf("OwnedItems should be empty after round-trip, got %v", decoded.OwnedItems)
	}
}

// TestMarkInstalledRecordJSON verifies that MarkInstalledRecord's JSON output
// is valid and contains the expected fields. We test the marshal step directly
// since the write step requires system paths.
func TestMarkInstalledRecordJSON(t *testing.T) {
	records := map[string]InstalledRecord{
		"pkg-a": {
			Version:     "1.0.0",
			InstalledAt: "2026-08-23T14:00:00Z",
			RemoveLines: []string{"rm /usr/bin/pkg-a"},
			Safety:      "strict",
			OwnedItems: []OwnedItem{
				{Path: "/usr/bin/pkg-a", Type: "file"},
			},
		},
		"pkg-b": {
			Version:     "2.1.0",
			InstalledAt: "2026-08-23T15:00:00Z",
			Safety:      "free",
		},
	}

	data, err := json.MarshalIndent(records, "", "  ")
	if err != nil {
		t.Fatalf("MarshalIndent failed: %v", err)
	}

	// Verify the JSON is valid by unmarshaling back.
	var decoded map[string]InstalledRecord
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("round-trip Unmarshal failed: %v", err)
	}

	if len(decoded) != 2 {
		t.Errorf("expected 2 records, got %d", len(decoded))
	}
	if decoded["pkg-a"].Version != "1.0.0" {
		t.Errorf("pkg-a Version = %q, want %q", decoded["pkg-a"].Version, "1.0.0")
	}
	if decoded["pkg-b"].Safety != "free" {
		t.Errorf("pkg-b Safety = %q, want %q", decoded["pkg-b"].Safety, "free")
	}
}

// redirectInstalledFile points the installed state file at a temp dir for the
// duration of a test, restoring the previous value afterwards.
func redirectInstalledFile(t *testing.T) string {
	t.Helper()
	tmpDir := t.TempDir()
	old := installedFileOverride
	installedFileOverride = filepath.Join(tmpDir, "installed.json")
	t.Cleanup(func() { installedFileOverride = old })
	return tmpDir
}

// TestMarkInstalledRecordConcurrent verifies that concurrent marks of
// different packages all survive — each read-modify-write cycle is serialized
// by the state lock, so no goroutine's record overwrites another's.
func TestMarkInstalledRecordConcurrent(t *testing.T) {
	redirectInstalledFile(t)

	const n = 6
	var wg sync.WaitGroup
	errCh := make(chan error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			name := fmt.Sprintf("pkg-%d", i)
			errCh <- MarkInstalledRecord(name, InstalledRecord{Version: "1.0.0", InstalledAt: "2026-09-04T00:00:00Z"})
		}(i)
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		if err != nil {
			t.Fatalf("concurrent MarkInstalledRecord failed: %v", err)
		}
	}

	records, err := ReadInstalled()
	if err != nil {
		t.Fatalf("ReadInstalled failed: %v", err)
	}
	if len(records) != n {
		t.Errorf("expected %d records after concurrent marks, got %d: %v", n, len(records), records)
	}
	for i := 0; i < n; i++ {
		name := fmt.Sprintf("pkg-%d", i)
		if _, ok := records[name]; !ok {
			t.Errorf("record %q was lost by a concurrent writer", name)
		}
	}
}

// TestUnmarkInstalledConcurrent verifies that removing one package while
// marking others does not drop the surviving records (and vice versa).
func TestUnmarkInstalledConcurrent(t *testing.T) {
	redirectInstalledFile(t)

	for _, name := range []string{"keep-a", "keep-b", "drop-c"} {
		if err := MarkInstalledRecord(name, InstalledRecord{Version: "1.0.0"}); err != nil {
			t.Fatalf("seed MarkInstalledRecord(%q) failed: %v", name, err)
		}
	}

	var wg sync.WaitGroup
	errCh := make(chan error, 3)
	ops := []func() error{
		func() error { return UnmarkInstalled("drop-c") },
		func() error { return MarkInstalledRecord("keep-c", InstalledRecord{Version: "2.0.0"}) },
		func() error { return UnmarkInstalled("keep-b") },
	}
	for _, op := range ops {
		wg.Add(1)
		go func(op func() error) {
			defer wg.Done()
			errCh <- op()
		}(op)
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		if err != nil {
			t.Fatalf("concurrent state operation failed: %v", err)
		}
	}

	records, err := ReadInstalled()
	if err != nil {
		t.Fatalf("ReadInstalled failed: %v", err)
	}
	want := map[string]string{"keep-a": "1.0.0", "keep-c": "2.0.0"}
	if len(records) != len(want) {
		t.Errorf("expected records %v after concurrent ops, got %d: %v", want, len(records), records)
	}
	for name, ver := range want {
		rec, ok := records[name]
		if !ok {
			t.Errorf("record %q missing after concurrent ops", name)
			continue
		}
		if rec.Version != ver {
			t.Errorf("%s version = %q, want %q", name, rec.Version, ver)
		}
	}
}

// TestMarkInstalledEntryWithOwnedItemsRoundTrip verifies the install pipeline's
// state step: MarkInstalledEntryWithOwnedItems persists an entry together with
// its owned items, and the record reads back intact.
func TestMarkInstalledEntryWithOwnedItemsRoundTrip(t *testing.T) {
	redirectInstalledFile(t)

	e := &Entry{
		Name:        "tool",
		Version:     "1.4.2",
		RemoveLines: []string{"rm -f /usr/bin/tool"},
		PurgeLines:  []string{"rm -rf /etc/tool"},
		Servers:     []string{"https://example.com/"},
		Safety:      "free",
		Source:      "github:user/tool",
	}
	items := []OwnedItem{
		{Path: "/usr/bin/tool", Type: "file"},
		{Path: "/etc/tool", Type: "dir"},
		{Path: "/usr/bin/tool-link", Type: "symlink"},
	}

	if err := MarkInstalledEntryWithOwnedItems(e, items); err != nil {
		t.Fatalf("MarkInstalledEntryWithOwnedItems failed: %v", err)
	}

	rec, ok := GetInstalled("tool")
	if !ok {
		t.Fatal("expected record for 'tool' after marking")
	}
	if rec.Version != "1.4.2" {
		t.Errorf("Version = %q, want %q", rec.Version, "1.4.2")
	}
	if rec.Safety != "free" {
		t.Errorf("Safety = %q, want %q", rec.Safety, "free")
	}
	if rec.Source != "github:user/tool" {
		t.Errorf("Source = %q, want %q", rec.Source, "github:user/tool")
	}
	if len(rec.RemoveLines) != 1 || rec.RemoveLines[0] != "rm -f /usr/bin/tool" {
		t.Errorf("RemoveLines = %v, want [rm -f /usr/bin/tool]", rec.RemoveLines)
	}
	if len(rec.PurgeLines) != 1 || rec.PurgeLines[0] != "rm -rf /etc/tool" {
		t.Errorf("PurgeLines = %v, want [rm -rf /etc/tool]", rec.PurgeLines)
	}
	if len(rec.OwnedItems) != len(items) {
		t.Fatalf("OwnedItems length = %d, want %d: %v", len(rec.OwnedItems), len(items), rec.OwnedItems)
	}
	for i, want := range items {
		if rec.OwnedItems[i] != want {
			t.Errorf("OwnedItems[%d] = %+v, want %+v", i, rec.OwnedItems[i], want)
		}
	}
}

// TestLockTimeout verifies that openAndLockFile returns an error when the lock
// is held by another process, rather than blocking forever (I8).
func TestLockTimeout(t *testing.T) {
	tmpDir := t.TempDir()
	lockPath := filepath.Join(tmpDir, "test.lock")

	// Hold the lock in this goroutine.
	holder, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		t.Fatalf("failed to create lock file: %v", err)
	}
	defer holder.Close()
	if err := syscall.Flock(int(holder.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		t.Fatalf("failed to acquire preliminary lock: %v", err)
	}
	defer syscall.Flock(int(holder.Fd()), syscall.LOCK_UN)

	// Reduce timeout for test speed.
	origTimeout := lockTimeout
	origInterval := lockRetryInterval
	lockTimeout = 200 * time.Millisecond
	lockRetryInterval = 50 * time.Millisecond
	defer func() {
		lockTimeout = origTimeout
		lockRetryInterval = origInterval
	}()

	start := time.Now()
	_, err = openAndLockFile(lockPath)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected timeout error when lock is held, got nil")
	}
	if !strings.Contains(err.Error(), "timeout") {
		t.Errorf("expected timeout error, got: %v", err)
	}
	if elapsed > 2*time.Second {
		t.Errorf("lock acquisition took too long: %v", elapsed)
	}
}
