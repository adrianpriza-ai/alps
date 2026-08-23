package more

import (
	"testing"
)

// --- Scrape() tests ---

func TestScrapeInstall(t *testing.T) {
	e := &Entry{Name: "test-pkg", CmdLines: []string{"make", "make install"}}
	lines, err := Scrape(e, OperationInstall)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(lines) != 2 || lines[0] != "make" || lines[1] != "make install" {
		t.Errorf("expected [make, make install], got %v", lines)
	}
	// Verify the returned slice is a copy, not a reference to the original
	e.CmdLines[0] = "changed"
	if lines[0] != "make" {
		t.Errorf("returned slice should be a copy; got %q after mutating entry", lines[0])
	}
}

func TestScrapeInstallEmpty(t *testing.T) {
	e := &Entry{Name: "empty-pkg"}
	_, err := Scrape(e, OperationInstall)
	if err == nil {
		t.Fatal("expected error for entry with no CmdLines")
	}
	if !containsStr(err.Error(), "no install commands") {
		t.Errorf("expected 'no install commands' in error, got %q", err.Error())
	}
}

func TestScrapeRemove(t *testing.T) {
	// Remove lines defined on the entry
	e := &Entry{Name: "rm-pkg", RemoveLines: []string{"rm -rf /opt/rm-pkg"}}
	lines, err := Scrape(e, OperationRemove)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(lines) != 1 || lines[0] != "rm -rf /opt/rm-pkg" {
		t.Errorf("expected [rm -rf /opt/rm-pkg], got %v", lines)
	}
}

func TestScrapeRemoveNotInstalled(t *testing.T) {
	// No RemoveLines and not installed → error
	e := &Entry{Name: "nonexistent-pkg"}
	_, err := Scrape(e, OperationRemove)
	if err == nil {
		t.Fatal("expected error for non-installed package with no remove lines")
	}
}

func TestScrapeUpgrade(t *testing.T) {
	// UpgradeLines defined → use them
	e := &Entry{Name: "up-pkg", UpgradeLines: []string{"git pull", "make"}}
	lines, err := Scrape(e, OperationUpgrade)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(lines) != 2 || lines[0] != "git pull" || lines[1] != "make" {
		t.Errorf("expected [git pull, make], got %v", lines)
	}
}

func TestScrapeUpgradeFallsBackToCmdLines(t *testing.T) {
	// No UpgradeLines → fall back to CmdLines
	e := &Entry{Name: "up-pkg-fb", CmdLines: []string{"make install"}}
	lines, err := Scrape(e, OperationUpgrade)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(lines) != 1 || lines[0] != "make install" {
		t.Errorf("expected [make install], got %v", lines)
	}
}

func TestScrapeUpgradeNoCommands(t *testing.T) {
	// No UpgradeLines and no CmdLines → error
	e := &Entry{Name: "up-empty"}
	_, err := Scrape(e, OperationUpgrade)
	if err == nil {
		t.Fatal("expected error when both UpgradeLines and CmdLines are empty")
	}
}

func TestScrapePurge(t *testing.T) {
	// PurgeLines defined → use them
	e := &Entry{Name: "pg-pkg", PurgeLines: []string{"rm -rf /etc/pg-pkg", "rm -rf /var/lib/pg-pkg"}}
	lines, err := Scrape(e, OperationPurge)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(lines) != 2 {
		t.Errorf("expected 2 purge lines, got %d", len(lines))
	}
}

func TestScrapePurgeNotInstalled(t *testing.T) {
	// No PurgeLines and not installed → error
	e := &Entry{Name: "pg-nonexistent"}
	_, err := Scrape(e, OperationPurge)
	if err == nil {
		t.Fatal("expected error for non-installed package with no purge lines")
	}
}

func TestScrapeUnknownOp(t *testing.T) {
	e := &Entry{Name: "test-pkg", CmdLines: []string{"echo hi"}}
	_, err := Scrape(e, OperationType("unknown"))
	if err == nil {
		t.Fatal("expected error for unknown operation type")
	}
}

// containsStr is a simple helper to check if a string contains a substring.
func containsStr(s, substr string) bool {
	return len(s) >= len(substr) && searchString(s, substr)
}

func searchString(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
