// Package platform provides canonical platform detection, path-prefix, and
// package-name validation used throughout the alps codebase. All other
// packages should import this package instead of defining their own copies.
package platform

import (
	"fmt"
	"os"
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
func IsRoot() bool {
	return os.Getuid() == 0
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
func NormalizeArch(goarch string) string {
	switch goarch {
	case "amd64":
		return "x86_64"
	case "arm64":
		return "aarch64"
	case "386":
		return "i686"
	case "arm":
		return "armv7l"
	default:
		return goarch
	}
}

// ValidatePkgName checks that a package-name component is safe for use as
// a directory name. It rejects empty names, path traversal sequences,
// leading dots, names longer than 255 bytes, and characters outside the
// allowed set (alphanumerics, '-', '_', '+', '.').
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
// Allowed: alphanumerics plus '-', '_', '+', and '.'.
func isValidNameChar(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') ||
		r == '-' || r == '_' || r == '+' || r == '.'
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
func LibDir() string {
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
