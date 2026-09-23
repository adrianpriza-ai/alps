package repo

import "testing"

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
