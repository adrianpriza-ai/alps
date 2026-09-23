package pack

import (
	"reflect"
	"testing"

	"github.com/adrianpriza-ai/alps/internal/pkgregistry"
)

// Dry-run is print-only for the runner path: BuildExtraFlagsExt never emits
// the native simulation flag. Manual appends in dry-run branches (e.g., pacman
// -p, apt --dry-run) are the only way to get package plan display.
func TestBuildExtraFlagsExtDryRunNeverEmits(t *testing.T) {
	for _, name := range AllNames() {
		got := BuildExtraFlagsExt(name, Flags{DryRun: true})
		dryRunFlag := GetDryRunFlag(name)
		for _, flag := range got {
			if flag == dryRunFlag {
				t.Errorf("%s: BuildExtraFlagsExt(DryRun) should not emit DryRunFlag %q, but got %v", name, dryRunFlag, got)
			}
		}
	}
}

// pacman(8) documents --print (-p) as valid for -S, -R and -U, so a single
// DryRunFlag serves install, remove and purge. Pin it against drift.
func TestPacmanDryRunFlagValidForAllTransactionVerbs(t *testing.T) {
	if got := GetDryRunFlag("pacman"); got != "-p" {
		t.Errorf("pacman DryRunFlag = %q, want -p", got)
	}
}

func TestDryRunEmitted(t *testing.T) {
	simulating := map[string]bool{
		"apt":     true,
		"apt-get": true,
		"dnf":     true,
		"pacman":  true,
		"zypper":  true,
		"apk":     true,
		"brew":    false,
	}
	for _, name := range AllNames() {
		want := simulating[name]
		if got := DryRunEmitted(name); got != want {
			t.Errorf("%s: DryRunEmitted = %v, want %v", name, got, want)
		}
	}
}

func TestBuildExtraFlagsExtNoConfirm(t *testing.T) {
	tests := []struct {
		backend  string
		expected []string
	}{
		{"apt", []string{"-y"}},
		{"apt-get", []string{"-y"}},
		{"dnf", []string{"-y"}},
		{"zypper", []string{"--no-confirm"}},
		{"pacman", []string{"--noconfirm"}},
		{"apk", nil},
		{"brew", nil},
	}
	for _, tt := range tests {
		if _, ok := Lookup(tt.backend, "install"); !ok {
			t.Fatalf("backend %q is not registered", tt.backend)
		}
		got := BuildExtraFlagsExt(tt.backend, Flags{NoConfirm: true})
		if !reflect.DeepEqual(got, tt.expected) {
			t.Errorf("%s: BuildExtraFlagsExt(NoConfirm) = %v, want %v", tt.backend, got, tt.expected)
		}
	}
}

func TestBuildExtraFlagsExtForce(t *testing.T) {
	tests := []struct {
		backend  string
		expected []string
	}{
		{"apt", []string{"--allow-downgrades", "--allow-change-held-packages"}},
		{"apt-get", []string{"--allow-downgrades", "--allow-change-held-packages"}},
		{"dnf", nil},
		{"zypper", []string{"--force-resolution"}},
		{"pacman", nil},
		{"apk", nil},
		{"brew", nil},
	}
	for _, tt := range tests {
		got := BuildExtraFlagsExt(tt.backend, Flags{Force: true})
		if !reflect.DeepEqual(got, tt.expected) {
			t.Errorf("%s: BuildExtraFlagsExt(Force) = %v, want %v", tt.backend, got, tt.expected)
		}
	}
}

func TestUnsupportedFlagWarnings(t *testing.T) {
	tests := []struct {
		backend  string
		noConf   bool
		force    bool
		expected []string
	}{
		{"apk", true, false, []string{"-y is not supported by apk — prompts will still appear"}},
		{"brew", true, false, []string{"-y is not supported by brew — prompts will still appear"}},
		{"apt", true, false, nil},
		{"zypper", true, false, nil},
		{"dnf", false, true, []string{"-f is not supported by dnf — it was ignored"}},
		{"pacman", false, true, []string{"-f is not supported by pacman — it was ignored"}},
		{"apt", false, true, nil},
		{"dnf", false, false, nil},
	}
	for _, tt := range tests {
		got := UnsupportedFlagWarnings(tt.backend, tt.noConf, tt.force)
		if !reflect.DeepEqual(got, tt.expected) {
			t.Errorf("%s (noConfirm=%v force=%v): warnings = %v, want %v", tt.backend, tt.noConf, tt.force, got, tt.expected)
		}
	}
}

// Appending to Lookup's result must never corrupt the registry slice.
func TestLookupReturnsCopy(t *testing.T) {
	args, ok := Lookup("apt", "install")
	if !ok {
		t.Fatal("Lookup(apt, install) not found")
	}
	args = append(args, "CORRUPTED")

	again, ok := Lookup("apt", "install")
	if !ok {
		t.Fatal("second Lookup(apt, install) not found")
	}
	want := []string{"apt", "install"}
	if !reflect.DeepEqual(again, want) {
		t.Errorf("registry slice corrupted: got %v, want %v", again, want)
	}
}

// No backend may map alps -f to a deprecated or wrong-semantics flag.
func TestNoDeprecatedForceFlags(t *testing.T) {
	banned := []string{"--force-yes", "--skip-broken", "--non-interactive"}
	for _, name := range AllNames() {
		for _, ff := range ForceFlags(name) {
			for _, b := range banned {
				if ff == b {
					t.Errorf("%s uses banned force flag %q", name, ff)
				}
			}
		}
	}
}

// Flags are case-sensitive: -v and -V are distinct options for several
// tools (apt verbose vs version), so appendUniq must not dedupe across case.
func TestAppendUniqCaseSensitive(t *testing.T) {
	got := pkgregistry.AppendUniq([]string{"-V"}, "-v")
	want := []string{"-V", "-v"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("appendUniq([-V], -v) = %v, want %v", got, want)
	}

	if dup := pkgregistry.AppendUniq([]string{"-v"}, "-v"); !reflect.DeepEqual(dup, []string{"-v"}) {
		t.Errorf("appendUniq([-v], -v) = %v, want [-v]", dup)
	}
}

func TestBuildExtraFlagsExtCombined(t *testing.T) {
	got := BuildExtraFlagsExt("apt", Flags{NoConfirm: true, Force: true})
	want := []string{"-y", "--allow-downgrades", "--allow-change-held-packages"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("combined apt flags = %v, want %v", got, want)
	}
}

// Verify that backends with native simulation flags return them via GetDryRunFlag.
func TestDryRunFlagPresence(t *testing.T) {
	backendsWithDryRun := map[string]string{
		"apt":     "--dry-run",
		"apt-get": "--dry-run",
		"pacman":  "-p",
		"apk":     "--simulate",
		"dnf":     "--assumeno",
		"zypper":  "--dry-run",
	}
	for backend, expectedFlag := range backendsWithDryRun {
		got := GetDryRunFlag(backend)
		if got != expectedFlag {
			t.Errorf("GetDryRunFlag(%s) = %q, want %q", backend, got, expectedFlag)
		}
	}
}

// Verify that when building flags for dry-run mode with Force/NoConfirm,
// BuildExtraFlagsExt emits those flags but never the DryRunFlag.
// This ensures manual appends in dry-run branches are the only way to get
// the native simulation flag for plan display.
func TestBuildExtraFlagsExtDryRunWithOtherFlags(t *testing.T) {
	tests := []struct {
		backend string
		flags   Flags
	}{
		{"apt", Flags{DryRun: true, NoConfirm: true}},
		{"apt", Flags{DryRun: true, Force: true}},
		{"pacman", Flags{DryRun: true, NoConfirm: true}},
		{"pacman", Flags{DryRun: true, Force: true}},
	}
	for _, tt := range tests {
		got := BuildExtraFlagsExt(tt.backend, tt.flags)
		dryRunFlag := GetDryRunFlag(tt.backend)
		for _, flag := range got {
			if flag == dryRunFlag {
				t.Errorf("%s: BuildExtraFlagsExt(%v) should not emit DryRunFlag %q, but got %v", tt.backend, tt.flags, dryRunFlag, got)
			}
		}
	}
}
