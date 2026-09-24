package repo

import (
	"errors"
	"reflect"
	"sort"
	"testing"

	"github.com/adrianpriza-ai/alps/more"
)

func TestUpgradeTarget(t *testing.T) {
	tests := []struct {
		name             string
		entryVersion     string
		installedVersion string
		wantTarget       string
		wantUpgradable   bool
		wantOk           bool
	}{
		{
			name:             "empty entry version - no version info",
			entryVersion:     "",
			installedVersion: "1.2.0",
			wantTarget:       "",
			wantUpgradable:   false,
			wantOk:           false,
		},
		{
			name:             "empty installed version with non-empty entry - upgradable",
			entryVersion:     "1.2.0",
			installedVersion: "",
			wantTarget:       "1.2.0",
			wantUpgradable:   true,
			wantOk:           true,
		},
		{
			name:             "both non-empty and equal - not upgradable",
			entryVersion:     "1.2.0",
			installedVersion: "1.2.0",
			wantTarget:       "",
			wantUpgradable:   false,
			wantOk:           true,
		},
		{
			name:             "both non-empty and different - upgradable",
			entryVersion:     "1.3.0",
			installedVersion: "1.2.0",
			wantTarget:       "1.3.0",
			wantUpgradable:   true,
			wantOk:           true,
		},
		{
			name:             "both empty - no version info",
			entryVersion:     "",
			installedVersion: "",
			wantTarget:       "",
			wantUpgradable:   false,
			wantOk:           false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			target, upgradable, ok := upgradeTarget(tt.entryVersion, tt.installedVersion)
			if target != tt.wantTarget {
				t.Errorf("upgradeTarget() target = %v, want %v", target, tt.wantTarget)
			}
			if upgradable != tt.wantUpgradable {
				t.Errorf("upgradeTarget() upgradable = %v, want %v", upgradable, tt.wantUpgradable)
			}
			if ok != tt.wantOk {
				t.Errorf("upgradeTarget() ok = %v, want %v", ok, tt.wantOk)
			}
		})
	}
}

func TestUpgradeSummary(t *testing.T) {
	tests := []struct {
		name      string
		upgraded  int
		skipped   int
		failed    int
		wantError bool
	}{
		{
			name:      "all upgraded, no failures",
			upgraded:  5,
			skipped:   0,
			failed:    0,
			wantError: false,
		},
		{
			name:      "some skipped, no failures",
			upgraded:  3,
			skipped:   2,
			failed:    0,
			wantError: false,
		},
		{
			name:      "some failures",
			upgraded:  2,
			skipped:   1,
			failed:    1,
			wantError: true,
		},
		{
			name:      "only skipped, no failures",
			upgraded:  0,
			skipped:   5,
			failed:    0,
			wantError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, shouldError := upgradeSummary(tt.upgraded, tt.skipped, tt.failed)
			if shouldError != tt.wantError {
				t.Errorf("upgradeSummary() shouldError = %v, want %v", shouldError, tt.wantError)
			}
		})
	}
}

// TestSortedInstalledNamesDeterministic asserts that the upgrade preview order
// is stable across repeated calls for the same installed-state map.
func TestSortedInstalledNamesDeterministic(t *testing.T) {
	records := map[string]more.InstalledRecord{
		"ztool":  {Version: "1.0.0"},
		"alpha":  {Version: "2.0.0"},
		"midpkg": {Version: ""},
		"beta":   {Version: "0.9.0"},
	}

	first := sortedInstalledNames(records)
	if !sort.StringsAreSorted(first) {
		t.Errorf("sortedInstalledNames() = %v, want sorted output", first)
	}
	for i := 0; i < 10; i++ {
		again := sortedInstalledNames(records)
		if !reflect.DeepEqual(first, again) {
			t.Fatalf("iteration %d: order changed: %v vs %v", i, first, again)
		}
	}
}

// TestBuildUpgradePreviewsOrderAndContent checks the actual preview slice: the
// builder must walk records in sorted order and classify each row.
func TestBuildUpgradePreviewsOrderAndContent(t *testing.T) {
	records := map[string]more.InstalledRecord{
		"ztool":  {Version: "1.0.0"},
		"alpha":  {Version: "2.0.0"},
		"nover":  {Version: "3.0.0"},
		"beta":   {Version: "0.9.0"},
	}
	entries := map[string]string{
		"alpha": "2.0.0", // up to date
		"beta":  "1.0.0", // upgradable
		"ztool": "",      // entry without version information
		// "nover" has no repo entry -> stale
	}
	resolve := func(name string, rec more.InstalledRecord) previewResolution {
		ver, ok := entries[name]
		if !ok {
			return previewResolution{err: errors.New("stale — no longer in repo")}
		}
		if ver == "" {
			return previewResolution{entry: &more.Entry{Name: name}}
		}
		return previewResolution{entry: &more.Entry{Name: name, Version: ver}}
	}

	first := buildUpgradePreviews(records, resolve)

	wantNames := []string{"alpha", "beta", "nover", "ztool"}
	gotNames := make([]string, len(first))
	wantErrs := []string{"", "", "stale — no longer in repo", "no version information — skipped"}
	wantTo := []string{"2.0.0", "1.0.0", "", ""}
	for i, p := range first {
		gotNames[i] = p.name
		if p.err != wantErrs[i] {
			t.Errorf("row %d (%s): err = %q, want %q", i, p.name, p.err, wantErrs[i])
		}
		if p.to != wantTo[i] {
			t.Errorf("row %d (%s): to = %q, want %q", i, p.name, p.to, wantTo[i])
		}
	}
	if !reflect.DeepEqual(gotNames, wantNames) {
		t.Errorf("preview order = %v, want %v", gotNames, wantNames)
	}

	// Repeated builds over the same records must be identical.
	for i := 0; i < 10; i++ {
		again := buildUpgradePreviews(records, resolve)
		if !reflect.DeepEqual(first, again) {
			t.Fatalf("iteration %d: preview slice changed across runs", i)
		}
	}
}
