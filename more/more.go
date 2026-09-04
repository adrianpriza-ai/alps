package more

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/adrianpriza-ai/alps/config"
	"github.com/adrianpriza-ai/alps/platform"
)

// Directory Layout
//
// The more/ package uses three distinct directory concepts. Keeping them
// straight matters because cache cleans should only touch the first two,
// while the third must survive across upgrades.
//
// 1. Index cache — platform.CacheDir()
//    Linux:   /var/cache/alps/more
//    Termux:  $PREFIX/var/cache/alps/more
//    macOS:   ~/Library/Caches/alps/more
//
//    Contents: main.txt (package index from mirrors), last_sync (timestamp).
//    Expendable: yes — re-fetched by `alps repo update`.
//    Functions: getCacheFile(), getLastSyncFile(), ensureCacheDir().
//
// 2. Build cache — getBuildCacheRoot()
//    All platforms: ~/.cache/alps/more/<package-name>/
//
//    Contents: source checkouts, build artifacts, compiled binaries.
//    Expendable: yes — rebuilt from source on next install/upgrade.
//    Functions: getBuildDir(), CleanCache(), BuildCacheDir().
//
// 3. Persistent state — platform.LibDir()
//    Linux:   /var/lib/alps
//    Termux:  $PREFIX/var/lib/alps
//    macOS:   ~/Library/Application Support/alps
//
//    Contents: installed.json (records of installed packages, their versions,
//              remove/purge lines, owned file paths, safety mode).
//    Expendable: NO — deleting this loses track of installed packages.
//    Functions: getInstalledFile(), ReadInstalled(), MarkInstalledRecord().
//
// Naming: more.BuildCacheDir() returns the BUILD cache root (#2), while
// platform.CacheDir() returns the INDEX cache (#1).

const scriptDownloadTimeout = 5 * time.Minute

// Entry represents a package entry.
type Entry struct {
	Name         string
	Desc         string
	Author       string
	Version      string
	Arch         []string
	OS           []string
	Deps         []string
	Servers      []string
	Safety       string
	SHA256Sums   []string
	CmdLines     []string
	RemoveLines  []string
	UpgradeLines []string
	PurgeLines   []string
	Source       string
}

// reqTool describes a single binary requirement.
type reqTool struct {
	bin   string      // executable to look up via PATH
	label string      // human-readable name shown in the warning
	hint  string      // short install hint shown after the warning
	skip  func() bool // return true to skip the check on this platform
}

// requirements lists every tool that alps-more may need.
var requirements = []reqTool{
	{
		bin:   "bash",
		label: "bash",
		hint:  "install bash via your package manager",
	},
	{
		bin:   "tar",
		label: "tar",
		hint:  "install tar via your package manager",
	},
	{
		bin:   "unzip",
		label: "unzip (needed for .zip archives)",
		hint:  "install unzip via your package manager",
	},
	{
		// mkdir, cp, chmod, gzip, ln — all from coreutils; test one sentinel binary
		bin:   "gzip",
		label: "gzip / GNU coreutils (mkdir, cp, chmod, gzip, ln)",
		hint:  "install coreutils and gzip via your package manager",
	},
	{
		bin:   "fakeroot",
		label: "fakeroot (needed for strict-mode installs)",
		hint:  "alps install fakeroot  OR  apt install fakeroot",
		// fakeroot is not needed on Termux or macOS
		skip: func() bool { return platform.IsTermux() || platform.IsMacOS() },
	},
	{
		bin:   "systemctl",
		label: "systemctl (needed for systemd service macros)",
		hint:  "install systemd or run on a systemd-based distro",
		// systemd is not available on Termux or macOS
		skip: func() bool { return platform.IsTermux() || platform.IsMacOS() },
	},
	{
		bin:   "useradd",
		label: "useradd/userdel (needed for CREATE_USER / REMOVE_USER macros)",
		hint:  "install shadow-utils or equivalent via your package manager",
		// useradd is not available on Termux or macOS
		skip: func() bool { return platform.IsTermux() || platform.IsMacOS() },
	},
}

// WarnMissingRequirements checks for missing tools and prints a warning for
// each one that is absent but relevant on the current platform.
// It never returns an error — the warnings are informational only.
func WarnMissingRequirements(cfg *config.Config) {
	var missing []reqTool
	for _, r := range requirements {
		if r.skip != nil && r.skip() {
			continue
		}
		if _, err := exec.LookPath(r.bin); err != nil {
			missing = append(missing, r)
		}
	}
	if len(missing) == 0 {
		return
	}
	fmt.Printf("  %s  some requirements are missing — certain features may not work:\n", cfg.Style.SymWarn)
	for _, r := range missing {
		fmt.Printf("       • %s\n", r.label)
		fmt.Printf("         hint: %s\n", r.hint)
	}
	fmt.Println()
}

// needsMirror checks if commands use {BASH_RUN} or {SERVER}.
func needsMirror(e *Entry) bool {
	for _, lines := range [][]string{e.CmdLines, e.UpgradeLines, e.RemoveLines, e.PurgeLines} {
		for _, l := range lines {
			// Skip comment lines — they are not real commands and may
			// mention {SERVER} or {BASH_RUN} incidentally.
			if strings.HasPrefix(l, "#") {
				continue
			}
			if strings.Contains(l, "{BASH_RUN}") ||
				strings.Contains(l, "{SERVER}") {
				return true
			}
		}
	}
	return false
}

// getBuildDir returns the per-package build directory.
// Pattern: ~/.cache/alps/more/<package-name>/
// Security: the package name is validated so a crafted name (e.g. "../evil")
// cannot escape the build cache root via path traversal.
func getBuildDir(pkgName string) (string, error) {
	if err := platform.ValidatePkgName(pkgName); err != nil {
		return "", fmt.Errorf("invalid package name %q: %w", pkgName, err)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cannot determine home directory: %w", err)
	}
	dir := filepath.Join(home, ".cache", "alps", "more", pkgName)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", fmt.Errorf("cannot create build directory %s: %w", dir, err)
	}
	return dir, nil
}

// hasFakeroot checks if fakeroot is available.
func hasFakeroot() bool {
	_, err := exec.LookPath("fakeroot")
	return err == nil
}

// requireFakeroot returns an error if fakeroot is not available.
// Termux and macOS are exempt — they own their prefix and do not use fakeroot.
func requireFakeroot() error {
	if platform.IsTermux() || platform.IsMacOS() || platform.IsRoot() {
		return nil
	}
	if !hasFakeroot() {
		return fmt.Errorf("fakeroot is required, please install it first")
	}
	return nil
}

// splitTrim splits a comma-separated string and trims whitespace from each part.
func splitTrim(s string) []string {
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// parseDeps parses dependency string, preserving OR groups (slash-separated)
// e.g., "curl/wget, git" -> ["curl/wget", "git"]
func parseDeps(val string) []string {
	groups := strings.Split(val, ",")
	out := make([]string, 0, len(groups))
	for _, group := range groups {
		group = strings.TrimSpace(group)
		if group != "" {
			out = append(out, group)
		}
	}
	return out
}

// containsCI performs a case-insensitive check for target in list.
func containsCI(list []string, target string) bool {
	t := strings.ToLower(target)
	for _, item := range list {
		if strings.ToLower(item) == t {
			return true
		}
	}
	return false
}
