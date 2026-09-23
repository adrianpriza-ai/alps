package platform

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestIsRootEffectiveUID pins that IsRoot answers the effective-uid question
// ("what can this process do"), not the real-uid one ("who am I"). A revert
// to os.Getuid() diverges under a setuid install or user namespace and must
// fail this test. uid and euid cannot be varied within one process, so also
// verify manually by running this test both as a normal user and as root.
func TestIsRootEffectiveUID(t *testing.T) {
	if IsRoot() != (os.Geteuid() == 0) {
		t.Errorf("IsRoot() = %v, want os.Geteuid() == 0 (euid %d)", IsRoot(), os.Geteuid())
	}
}

// TestValidatePkgName pins the rules shared by every package-name gate in the
// program: '@' is legal (AUR names like python@3), while traversal
// sequences, leading dots, separators, whitespace and over-long names are not.
func TestValidatePkgName(t *testing.T) {
	tests := []struct {
		name    string
		wantErr bool
	}{
		{"", true},
		{"foo", false},
		{"foo-bar", false},
		{"foo_bar", false},
		{"foo+bar", false},
		{"foo.bar", false},
		{"python@3", false},
		{"UPPERCASE", false},
		{"..foo", true},
		{"a..b", true},
		{".hidden", true},
		{"foo/bar", true},
		{"foo\\bar", true},
		{"foo bar", true},
		{strings.Repeat("a", 256), true},
		{strings.Repeat("a", 255), false},
	}
	for _, tc := range tests {
		err := ValidatePkgName(tc.name)
		if (err != nil) != tc.wantErr {
			t.Errorf("ValidatePkgName(%q) error = %v, wantErr %v", tc.name, err, tc.wantErr)
		}
	}
}

// TestUserCacheRoot pins the invoking-user cache resolution. SUDO_USER/DOAS_USER
// that name root, or any name that cannot be looked up, must fall through to the
// process's own home (so the path is never /root/.cache/alps). A resolvable
// non-root user would change the path, so that branch is left to manual checks.
func TestUserCacheRoot(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home directory available")
	}
	want := filepath.Join(home, ".cache", "alps")

	cases := []struct {
		name string
		env  map[string]string // "" value means unset
	}{
		{"unset", nil},
		{"SUDO_USER=root", map[string]string{"SUDO_USER": "root"}},
		{"DOAS_USER=root", map[string]string{"DOAS_USER": "root"}},
		{"SUDO_USER=unknown-user", map[string]string{"SUDO_USER": "this-user-does-not-exist-xyz"}},
		{"DOAS_USER=unknown-user", map[string]string{"DOAS_USER": "this-user-does-not-exist-xyz"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			os.Unsetenv("SUDO_USER")
			os.Unsetenv("DOAS_USER")
			t.Cleanup(func() {
				os.Unsetenv("SUDO_USER")
				os.Unsetenv("DOAS_USER")
			})
			for k, v := range tc.env {
				t.Setenv(k, v)
			}
			got, err := UserCacheRoot()
			if err != nil {
				t.Fatalf("UserCacheRoot() error = %v", err)
			}
			if got != want {
				t.Errorf("UserCacheRoot() = %q, want %q", got, want)
			}
		})
	}
}

// TestNormalizeArch pins the GOARCH->distro-arch mapping. The arm case is
// GOARM-sensitive: an unset GOARM keeps the historical armv7l default, while
// GOARM=5/6/7 select armv5l/armv6l/armv7l so armv6l boards (Pi Zero) actually
// match a manifest that lists them. The other arches ignore GOARM entirely.
func TestNormalizeArch(t *testing.T) {
	tests := []struct {
		goarch string
		goarm  string // "" means unset
		want   string
	}{
		{"amd64", "", "x86_64"},
		{"amd64", "6", "x86_64"}, // GOARM does not affect non-arm
		{"arm64", "", "aarch64"},
		{"386", "", "i686"},
		{"arm", "", "armv7l"},   // historical default preserved
		{"arm", "5", "armv5l"},
		{"arm", "6", "armv6l"},  // Pi Zero / original Pi
		{"arm", "7", "armv7l"},
		{"riscv64", "", "riscv64"}, // unknown arches pass through
	}

	for _, tc := range tests {
		t.Run(tc.goarch+"@GOARM="+tc.goarm, func(t *testing.T) {
			if tc.goarm == "" {
				t.Setenv("GOARM", "")
				os.Unsetenv("GOARM")
			} else {
				t.Setenv("GOARM", tc.goarm)
			}
			if got := NormalizeArch(tc.goarch); got != tc.want {
				t.Errorf("NormalizeArch(%q) = %q, want %q", tc.goarch, got, tc.want)
			}
		})
	}
}

// TestTermuxPrefix pins the Termux prefix resolution: inside Termux it is
// $PREFIX, falling back to the Android default when PREFIX is empty; outside
// Termux it is always empty. PREFIX empty and unset are indistinguishable to
// os.Getenv, so the table exercises "" for both.
func TestTermuxPrefix(t *testing.T) {
	tests := []struct {
		name      string
		termuxVer string
		prefix    string
		want      string
	}{
		{"outside Termux", "", "/opt/local", ""},
		{"via TERMUX_VERSION", "0.119.0", "/custom/prefix", "/custom/prefix"},
		{"empty PREFIX", "0.119.0", "", "/data/data/com.termux/files/usr"},
		{"via PREFIX only", "", "/data/data/com.termux/files/usr", "/data/data/com.termux/files/usr"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("TERMUX_VERSION", tc.termuxVer)
			t.Setenv("PREFIX", tc.prefix)
			if got := TermuxPrefix(); got != tc.want {
				t.Errorf("TermuxPrefix() = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestMacOSPrefix pins the macOS prefix resolution: on darwin HOMEBREW_PREFIX
// wins and an empty one falls back to /usr/local; on every other OS the result
// is empty regardless of HOMEBREW_PREFIX.
func TestMacOSPrefix(t *testing.T) {
	if runtime.GOOS == "darwin" {
		t.Run("HOMEBREW_PREFIX", func(t *testing.T) {
			t.Setenv("HOMEBREW_PREFIX", "/opt/homebrew")
			if got := MacOSPrefix(); got != "/opt/homebrew" {
				t.Errorf("MacOSPrefix() = %q, want %q", got, "/opt/homebrew")
			}
		})
		t.Run("default", func(t *testing.T) {
			t.Setenv("HOMEBREW_PREFIX", "")
			if got := MacOSPrefix(); got != "/usr/local" {
				t.Errorf("MacOSPrefix() = %q, want %q", got, "/usr/local")
			}
		})
		return
	}
	t.Run("non-darwin", func(t *testing.T) {
		t.Setenv("HOMEBREW_PREFIX", "/opt/homebrew")
		if got := MacOSPrefix(); got != "" {
			t.Errorf("MacOSPrefix() = %q on %s, want empty", got, runtime.GOOS)
		}
	})
}
