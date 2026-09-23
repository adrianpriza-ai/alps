// Package platform provides canonical platform detection, path-prefix, and
// package-name validation used throughout the alps codebase. All other
// packages should import this package instead of defining their own copies.
//
// Two path conventions coexist by design, and must not be unified:
//
//   - System state (CacheDir, LibDir): root-owned, machine-wide locations
//     (/var/cache/alps, /var/lib/alps; Termux and macOS have their own
//     platform roots). These hold shared program state such as the apt-style
//     repo index (main.txt) and the installed-package record (installed.json).
//     Writing here requires root (or the platform prefix); moving /var/lib/alps
//     would orphan every existing installed.json, so do not relocate it
//     without owner sign-off.
//
//   - User build cache (UserCacheRoot): a per-invoking-user directory under
//     ~/.cache/alps, resolved through SUDO_USER/DOAS_USER so files are owned by
//     the human who ran alps rather than root. The AUR pipeline uses this
//     (UserCacheRoot()/aur) because makepkg refuses to run as root. It is a
//     deliberately separate convention, not an oversight.
package platform

import (
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"runtime"
	"strings"
)

// IsTermux reports whether the process is running inside Termux on Android.
// It checks two environment variables that Termux always sets.
func IsTermux() bool {
	return os.Getenv("TERMUX_VERSION") != "" ||
		os.Getenv("PREFIX") == "/data/data/com.termux/files/usr"
}

// IsMacOS reports whether the process is running on macOS (darwin).
func IsMacOS() bool {
	return runtime.GOOS == "darwin"
}

// IsRoot reports whether the current effective user is root (uid 0).
// This is the single canonical root check for the whole program: the
// effective uid is what determines whether a command will actually run
// as root, so it wins over the real uid under setuid installs and
// user namespaces.
func IsRoot() bool {
	return os.Geteuid() == 0
}

// IsWSL reports whether the process is running inside Windows Subsystem
// for Linux. It checks WSL-specific environment variables first, then
// falls back to reading /proc/version for the "microsoft" or "wsl" keyword.
func IsWSL() bool {
	if os.Getenv("WSL_DISTRO_NAME") != "" || os.Getenv("WSL_INTEROP") != "" {
		return true
	}
	if data, err := os.ReadFile("/proc/version"); err == nil {
		lower := strings.ToLower(string(data))
		return strings.Contains(lower, "microsoft") || strings.Contains(lower, "wsl")
	}
	return false
}

// UsesASCIIFallback reports whether TERM names a primitive terminal (the
// linux console, dumb, or unset) that cannot render Unicode symbols.
// Callers use it to swap Unicode glyphs for ASCII fallbacks.
func UsesASCIIFallback() bool {
	term := os.Getenv("TERM")
	return term == "linux" || term == "dumb" || term == ""
}

// DistroID returns the distribution ID from /etc/os-release, or "termux"
// inside Termux. It is empty when the file is missing or has no ID field.
func DistroID() string {
	if IsTermux() {
		return "termux"
	}
	data, err := os.ReadFile("/etc/os-release")
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "ID=") {
			return strings.ToLower(strings.Trim(line[3:], `"'`))
		}
	}
	return ""
}

var archDistros = []string{"arch", "manjaro", "endeavouros", "garuda", "artix"}
var debianDistros = []string{"debian", "ubuntu", "linuxmint", "pop", "elementary", "kali"}

// IsArchBased reports whether the detected distribution is Arch or an
// Arch derivative (manjaro, endeavouros, garuda, artix).
func IsArchBased() bool {
	d := DistroID()
	for _, id := range archDistros {
		if d == id {
			return true
		}
	}
	return false
}

// IsDebianBased reports whether the detected distribution is Debian or a
// Debian derivative (ubuntu, linuxmint, pop, elementary, kali).
func IsDebianBased() bool {
	d := DistroID()
	for _, id := range debianDistros {
		if d == id {
			return true
		}
	}
	return false
}

// HasSnapd reports whether snap is usable: the snap binary is in PATH, no
// nosnap.pref pins it off, and the snapd daemon is active.
func HasSnapd() bool {
	if _, err := exec.LookPath("snap"); err != nil {
		return false
	}
	if _, err := os.Stat("/etc/apt/preferences.d/nosnap.pref"); err == nil {
		return false
	}
	return exec.Command("systemctl", "is-active", "--quiet", "snapd").Run() == nil
}

// TermuxPrefix returns the Termux $PREFIX path when running inside Termux,
// or an empty string on regular Linux/WSL systems.
func TermuxPrefix() string {
	if !IsTermux() {
		return ""
	}
	prefix := os.Getenv("PREFIX")
	if prefix == "" {
		prefix = "/data/data/com.termux/files/usr"
	}
	return prefix
}

// MacOSPrefix returns the macOS installation prefix.
// It respects Homebrew's $HOMEBREW_PREFIX if set, otherwise defaults
// to /usr/local (the standard location for manual installs on macOS).
func MacOSPrefix() string {
	if !IsMacOS() {
		return ""
	}
	if prefix := os.Getenv("HOMEBREW_PREFIX"); prefix != "" {
		return prefix
	}
	return "/usr/local"
}

// NormalizeArch maps Go's runtime.GOARCH values to the standard Linux
// distribution architecture names used in ALPSMORE manifests.
//
// For GOARCH=arm the ARM variant depends on the GOARM setting used at build
// time, so we read GOARM from the environment: GOARM=6 yields armv6l (the
// original Raspberry Pi and Pi Zero), GOARM=5 yields armv5l, GOARM=7 yields
// armv7l. An unset GOARM defaults to armv7l to preserve the historical
// mapping for ARMv7 boards; if a manifest targets armv6l it is only matched
// when GOARM is set explicitly at build or runtime.
func NormalizeArch(goarch string) string {
	switch goarch {
	case "amd64":
		return "x86_64"
	case "arm64":
		return "aarch64"
	case "386":
		return "i686"
	case "arm":
		switch os.Getenv("GOARM") {
		case "5":
			return "armv5l"
		case "6":
			return "armv6l"
		case "7":
			return "armv7l"
		default:
			return "armv7l"
		}
	default:
		return goarch
	}
}

// ValidatePkgName checks that a package-name component is safe for use as
// a directory name. It rejects empty names, path traversal sequences,
// leading dots, names longer than 255 bytes, and characters outside the
// allowed set (alphanumerics, '-', '_', '+', '.', '@'). The '@' is permitted
// because AUR package names use it (e.g. python@3).
func ValidatePkgName(name string) error {
	if name == "" {
		return fmt.Errorf("empty package name")
	}
	if strings.Contains(name, "..") || strings.Contains(name, "/") || strings.Contains(name, `\`) {
		return fmt.Errorf("package name must not contain path separators or traversal sequences")
	}
	if strings.HasPrefix(name, ".") {
		return fmt.Errorf("package name must not start with a dot")
	}
	if len(name) > 255 {
		return fmt.Errorf("package name too long")
	}
	for _, r := range name {
		if !isValidNameChar(r) {
			return fmt.Errorf("invalid character %q in package name", r)
		}
	}
	return nil
}

// isValidNameChar reports whether a rune is permitted in a package name.
// Allowed: alphanumerics plus '-', '_', '+', '.', and '@' (AUR names use it).
func isValidNameChar(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') ||
		r == '-' || r == '_' || r == '+' || r == '.' || r == '@'
}

// OperationType represents the type of operation being performed on packages.
type OperationType string

const (
	OperationInstall OperationType = "install"
	OperationRemove  OperationType = "remove"
	OperationUpgrade OperationType = "upgrade"
	OperationPurge   OperationType = "purge"
)

// CacheDir returns the cache directory (expendable — main.txt, last_sync).
// The cache directory contains temporary files that can be safely deleted.
// On Termux: $PREFIX/var/cache/alps/more
// On macOS: ~/Library/Caches/alps/more
// On Linux: /var/cache/alps/more
func CacheDir() string {
	if IsTermux() {
		prefix := os.Getenv("PREFIX")
		if prefix == "" {
			prefix = "/data/data/com.termux/files/usr"
		}
		return filepath.Join(prefix, "var/cache/alps/more")
	}
	if IsMacOS() {
		// On macOS, use ~/Library/Caches/alps/more for user cache
		home, err := os.UserHomeDir()
		if err != nil {
			return "/var/cache/alps/more"
		}
		return filepath.Join(home, "Library", "Caches", "alps", "more")
	}
	return "/var/cache/alps/more"
}

// LibDir returns the state directory (persistent — installed.json).
// The lib directory contains persistent state that must survive cache cleans.
// On Termux: $PREFIX/var/lib/alps
// On macOS: ~/Library/Application Support/alps
// On Linux: /var/lib/alps
// ALPS_LIB_DIR overrides the path — a seam for tests and packaging, not a
// user feature.
func LibDir() string {
	if dir := os.Getenv("ALPS_LIB_DIR"); dir != "" {
		return dir
	}
	if IsTermux() {
		prefix := os.Getenv("PREFIX")
		if prefix == "" {
			prefix = "/data/data/com.termux/files/usr"
		}
		return filepath.Join(prefix, "var/lib/alps")
	}
	if IsMacOS() {
		// On macOS, use ~/Library/Application Support/alps for persistent state
		home, err := os.UserHomeDir()
		if err != nil {
			return "/var/lib/alps"
		}
		return filepath.Join(home, "Library", "Application Support", "alps")
	}
	return "/var/lib/alps"
}

// UserCacheRoot returns the per-invoking-user alps cache root (~/.cache/alps),
// used for build caches that must not be owned by root. When alps is run under
// sudo or doas, the cache is resolved to the invoking user's home (via
// SUDO_USER/DOAS_USER) rather than /root, so build artifacts and the AUR names
// cache are written where the human who ran alps can read them. It is the
// single source of the user-cache location; callers join their own
// subdirectory (e.g. "aur") onto the result.
func UserCacheRoot() (string, error) {
	if sudoUser := os.Getenv("SUDO_USER"); sudoUser != "" && sudoUser != "root" {
		if u, err := user.Lookup(sudoUser); err == nil && u.HomeDir != "" {
			return filepath.Join(u.HomeDir, ".cache", "alps"), nil
		}
	}
	if doasUser := os.Getenv("DOAS_USER"); doasUser != "" && doasUser != "root" {
		if u, err := user.Lookup(doasUser); err == nil && u.HomeDir != "" {
			return filepath.Join(u.HomeDir, ".cache", "alps"), nil
		}
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".cache", "alps"), nil
}
