package extra

import (
	"reflect"
	"testing"

	"github.com/adrianpriza-ai/alps/cli"
	"github.com/adrianpriza-ai/alps/internal/pkgregistry"
)

// Dry-run is print-only for the extra backends too: no native flag appended.
func TestBuildExtraFlagsExtDryRunNeverEmitsExtra(t *testing.T) {
	for _, name := range AllNames() {
		got := BuildExtraFlagsExt(name, Flags{DryRun: true})
		if len(got) != 0 {
			t.Errorf("%s: BuildExtraFlagsExt(DryRun) = %v, want none", name, got)
		}
	}
}

// -y must emit flatpak's native flag and warn (via UnsupportedFlagWarnings)
// for snap and winget, which have no assume-yes equivalent.
func TestYesSupportExtra(t *testing.T) {
	if !YesSupported("flatpak") {
		t.Error("flatpak should support -y")
	}
	for _, name := range []string{"snap", "winget"} {
		if YesSupported(name) {
			t.Errorf("%s should not support -y", name)
		}
	}

	if got := BuildExtraFlagsExt("flatpak", Flags{NoConfirm: true}); !reflect.DeepEqual(got, []string{"-y"}) {
		t.Errorf("flatpak NoConfirm flags = %v, want [-y]", got)
	}
	for _, name := range []string{"snap", "winget"} {
		if got := BuildExtraFlagsExt(name, Flags{NoConfirm: true}); len(got) != 0 {
			t.Errorf("%s NoConfirm flags = %v, want none", name, got)
		}
	}
}

func TestUnsupportedFlagWarningsExtra(t *testing.T) {
	tests := []struct {
		backend  string
		noConf   bool
		force    bool
		expected []string
	}{
		{"snap", true, false, []string{"-y is not supported by snap — prompts will still appear"}},
		{"winget", true, false, []string{"-y is not supported by winget — prompts will still appear"}},
		{"flatpak", true, false, nil},
		{"snap", false, true, []string{"-f is not supported by snap — it was ignored"}},
		{"winget", false, true, []string{"-f is not supported by winget — it was ignored"}},
		{"flatpak", false, true, []string{"-f is not supported by flatpak — it was ignored"}},
		{"flatpak", false, false, nil},
	}
	for _, tt := range tests {
		got := UnsupportedFlagWarnings(tt.backend, tt.noConf, tt.force)
		if !reflect.DeepEqual(got, tt.expected) {
			t.Errorf("%s (noConfirm=%v force=%v): warnings = %v, want %v", tt.backend, tt.noConf, tt.force, got, tt.expected)
		}
	}
}

// Flags are case-sensitive: dedupe must not swallow a differently-cased flag.
func TestAppendUniqCaseSensitiveExtra(t *testing.T) {
	got := pkgregistry.AppendUniq([]string{"-V"}, "-v")
	want := []string{"-V", "-v"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("appendUniq([-V], -v) = %v, want %v", got, want)
	}
}

// Appending to Lookup's result must never corrupt the registry slice.
func TestLookupReturnsCopy(t *testing.T) {
	args, ok := Lookup("snap", "install")
	if !ok {
		t.Fatal("Lookup(snap, install) not found")
	}
	args = append(args, "CORRUPTED")

	again, ok := Lookup("snap", "install")
	if !ok {
		t.Fatal("second Lookup(snap, install) not found")
	}
	want := []string{"snap", "install"}
	if !reflect.DeepEqual(again, want) {
		t.Errorf("registry slice corrupted: got %v, want %v", again, want)
	}
}

// The verb helpers must actually emit the native flags for alps meta-flags:
// BuildExtraFlagsExt is only useful if the execution path appends it.
func TestVerbArgsCarryNativeFlags(t *testing.T) {
	tests := []struct {
		name     string
		backend  string
		verb     string
		f        Flags
		trailing []string
		want     []string
	}{
		{"flatpak install -y", "flatpak", "install", Flags{NoConfirm: true}, []string{"pkg"}, []string{"flatpak", "install", "flathub", "pkg", "-y"}},
		{"flatpak update -y", "flatpak", "update", Flags{NoConfirm: true}, nil, []string{"flatpak", "update", "-y"}},
		{"snap install -y is dropped but never silent (warning path)", "snap", "install", Flags{NoConfirm: true}, []string{"pkg"}, []string{"snap", "install", "pkg"}},
		{"winget upgrade with -y/-f", "winget", "upgrade", Flags{NoConfirm: true, Force: true}, nil, []string{"winget.exe", "upgrade", "--all"}},
	}
	for _, tt := range tests {
		got, err := buildVerbArgs(tt.backend, tt.verb, tt.f, tt.trailing...)
		if err != nil {
			t.Fatalf("%s: %v", tt.name, err)
		}
		if !reflect.DeepEqual(got, tt.want) {
			t.Errorf("%s: got %v, want %v", tt.name, got, tt.want)
		}
	}
}

// Every subcommand the help text and completions advertise (via cli) must be
// runnable, so no advertised command can regress into a no-op.
func TestAdvertisedSubcommandsAreSupported(t *testing.T) {
	for _, backend := range AllNames() {
		for _, verb := range cli.SubCmds(backend) {
			if !CommandSupported(backend, verb) {
				t.Errorf("cli advertises %s %s but the backend does not support it", backend, verb)
			}
		}
	}
}
