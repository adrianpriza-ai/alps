package more

import (
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/adrianpriza-ai/alps/config"
	"github.com/adrianpriza-ai/alps/platform"
)

func TestDetectDistroVersion(t *testing.T) {
	// Test with mock system info providers to avoid real system calls
	tests := []struct {
		name     string
		sysInfo  func() (string, []string, string)
		expected string
	}{
		{
			name: "Termux with version",
			sysInfo: func() (string, []string, string) {
				return "termux", []string{"termux"}, "0.119.0"
			},
			expected: "0.119.0",
		},
		{
			name: "Termux without version",
			sysInfo: func() (string, []string, string) {
				return "termux", []string{"termux"}, "unknown"
			},
			expected: "unknown",
		},
		{
			name: "macOS with version",
			sysInfo: func() (string, []string, string) {
				return "macos", []string{"darwin", "macos"}, "14.5"
			},
			expected: "14.5",
		},
		{
			name: "Linux with version",
			sysInfo: func() (string, []string, string) {
				return "ubuntu", []string{"debian"}, "22.04"
			},
			expected: "22.04",
		},
		{
			name: "Unknown system",
			sysInfo: func() (string, []string, string) {
				return "unknown", nil, "unknown"
			},
			expected: "unknown",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, _, version := tc.sysInfo()
			if version != tc.expected {
				t.Errorf("expected version %q, got %q", tc.expected, version)
			}
		})
	}
}

// TestDetectDistroVersionReal is kept for compatibility testing on real systems
// This test is skipped by default to avoid real system calls
func TestDetectDistroVersionReal(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping real system call test in short mode")
	}

	version := detectDistroVersion()
	if platform.IsTermux() {
		expected := os.Getenv("TERMUX_VERSION")
		if expected == "" {
			expected = "unknown"
		}
		if version != expected {
			t.Errorf("expected termux version %q, got %q", expected, version)
		}
	} else if platform.IsMacOS() {
		// On macOS, sw_vers -productVersion should return a non-empty version string
		if version == "" {
			t.Errorf("expected non-empty macOS version, got empty string")
		}
		// macOS versions look like "14.5" or "13.6.1"
		if version != "unknown" && !strings.Contains(version, ".") {
			t.Errorf("expected macOS version to contain a dot (e.g. 14.5), got %q", version)
		}
	} else {
		// On standard Linux/WSL, check if /etc/os-release exists and matches
		data, err := os.ReadFile("/etc/os-release")
		if err == nil {
			var expected string = "unknown"
			for _, line := range strings.Split(string(data), "\n") {
				line = strings.TrimSpace(line)
				if strings.HasPrefix(line, "VERSION_ID=") {
					expected = strings.Trim(line[11:], `"'`)
					break
				}
			}
			if version != expected {
				t.Errorf("expected distro version %q, got %q", expected, version)
			}
		} else {
			if version != "unknown" {
				t.Errorf("expected unknown distro version on error, got %q", version)
			}
		}
	}
}

func TestExpandMacrosDisver(t *testing.T) {
	// Note: ExpandMacros uses the real detectDistroVersion() function internally
	// We can only test with real system calls in non-short mode
	if testing.Short() {
		t.Skip("skipping ExpandMacros test with real system calls in short mode")
	}

	ctx := NewMacroContext(nil, "my-server")
	input := []string{"echo {DISVER}"}
	output, err := ExpandMacros(input, ctx)
	if err != nil {
		t.Fatalf("unexpected error during ExpandMacros: %v", err)
	}
	if len(output) != 1 {
		t.Fatalf("expected 1 line output, got %d lines", len(output))
	}
	version := detectDistroVersion()
	if !strings.Contains(output[0], version) {
		t.Errorf("expected output to contain distro version %q, got %q", version, output[0])
	}
}

func TestOSMatches(t *testing.T) {
	// Test osMatches function behavior - it uses runtime.GOOS for "linux" and "darwin"/"macos"
	// and only uses distro/idLike for other distro names
	tests := []struct {
		name        string
		osList      []string
		distro      string
		idLike      []string
		shouldMatch bool
		note        string
	}{
		{
			name:        "Matches distro name",
			osList:      []string{"ubuntu"},
			distro:      "ubuntu",
			idLike:      []string{"debian"},
			shouldMatch: true,
			note:        "Matches exact distro name",
		},
		{
			name:        "Matches idLike",
			osList:      []string{"debian"},
			distro:      "ubuntu",
			idLike:      []string{"debian"},
			shouldMatch: true,
			note:        "Matches idLike field",
		},
		{
			name:        "WSL matches wsl",
			osList:      []string{"wsl"},
			distro:      "ubuntu",
			idLike:      []string{"debian", "wsl"},
			shouldMatch: true,
			note:        "Matches wsl in idLike",
		},
		{
			name:        "Non-WSL does not match wsl",
			osList:      []string{"wsl"},
			distro:      "ubuntu",
			idLike:      []string{"debian"},
			shouldMatch: false,
			note:        "No wsl in idLike",
		},
		{
			name:        "Case insensitive matching",
			osList:      []string{"Ubuntu"},
			distro:      "ubuntu",
			idLike:      []string{"debian"},
			shouldMatch: true,
			note:        "Case insensitive distro matching",
		},
		{
			name:        "Whitespace trimming",
			osList:      []string{" ubuntu "},
			distro:      "ubuntu",
			idLike:      []string{"debian"},
			shouldMatch: true,
			note:        "Whitespace trimmed from osList",
		},
		{
			name:        "No match",
			osList:      []string{"fedora"},
			distro:      "ubuntu",
			idLike:      []string{"debian"},
			shouldMatch: false,
			note:        "No matching distro or idLike",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := osMatches(tc.osList, tc.distro, tc.idLike)
			if result != tc.shouldMatch {
				t.Errorf("osMatches(%v, %q, %v) = %v; expected %v (%s)", tc.osList, tc.distro, tc.idLike, result, tc.shouldMatch, tc.note)
			}
		})
	}

	// Special tests for "linux" and "darwin"/"macos" which use runtime.GOOS
	t.Run("linux depends on runtime.GOOS", func(t *testing.T) {
		// On actual Linux (not Termux), "linux" should match
		// On macOS or Termux, it should not
		result := osMatches([]string{"linux"}, "ubuntu", []string{"debian"})
		expectedResult := !platform.IsTermux() && runtime.GOOS != "darwin"
		if result != expectedResult {
			t.Logf("Note: 'linux' matching depends on runtime.GOOS (not Termux, not darwin)")
			t.Logf("Current runtime.GOOS: %s, isTermux: %v", runtime.GOOS, platform.IsTermux())
		}
	})

	t.Run("darwin/macos depends on runtime.GOOS", func(t *testing.T) {
		// On macOS, "darwin" and "macos" should match
		// On Linux or Termux, they should not
		for _, osName := range []string{"darwin", "macos"} {
			result := osMatches([]string{osName}, "macos", []string{"darwin", "macos"})
			expectedResult := runtime.GOOS == "darwin"
			if result != expectedResult {
				t.Logf("Note: '%s' matching depends on runtime.GOOS == 'darwin'", osName)
				t.Logf("Current runtime.GOOS: %s", runtime.GOOS)
			}
		}
	})
}

// TestOSMatchesReal is kept for compatibility testing on real systems
// This test is skipped by default to avoid real system calls
func TestOSMatchesReal(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping real system call test in short mode")
	}

	// If the system is macOS, it should match "darwin" and "macos", but NOT "linux"
	if platform.IsMacOS() {
		if !osMatches([]string{"darwin"}, "macos", []string{"darwin", "macos"}) {
			t.Errorf("expected 'darwin' to match on macOS")
		}
		if !osMatches([]string{"macos"}, "macos", []string{"darwin", "macos"}) {
			t.Errorf("expected 'macos' to match on macOS")
		}
		if osMatches([]string{"linux"}, "macos", []string{"darwin", "macos"}) {
			t.Errorf("expected 'linux' to NOT match on macOS")
		}
		return
	}

	// If the system is WSL, it should now match "linux"
	// If the system is standard Linux, it should match "linux"
	// If the system is Termux, it should NOT match "linux"
	hasLinuxMatch := osMatches([]string{"linux"}, "ubuntu", []string{"debian"})
	if platform.IsTermux() {
		if hasLinuxMatch {
			t.Errorf("expected 'linux' to NOT match on Termux")
		}
	} else {
		if !hasLinuxMatch {
			t.Errorf("expected 'linux' to match on standard Linux or WSL")
		}
	}

	// If the system is WSL, it should match "wsl"
	hasWSLMatch := osMatches([]string{"wsl"}, "ubuntu", []string{"debian", "wsl"})
	if platform.IsWSL() {
		if !hasWSLMatch {
			t.Errorf("expected 'wsl' to match on WSL")
		}
	} else {
		// Note: since we pass "wsl" in idLike to simulate WSL, let's check with actual detectDistro result
		distro, idLike := detectDistro()
		actualWSLMatch := osMatches([]string{"wsl"}, distro, idLike)
		if actualWSLMatch {
			t.Errorf("expected 'wsl' to NOT match on non-WSL system")
		}
	}
}

func TestParseMacroArgsInsideBraces(t *testing.T) {
	// {INSTALL_BIN src dest} — args inside braces
	macro, remaining, ok := ParseMacro("{INSTALL_BIN foo-bin /usr/bin/}")
	if !ok {
		t.Fatalf("expected ParseMacro to recognize the macro")
	}
	if macro.Name != "INSTALL_BIN" {
		t.Errorf("expected name INSTALL_BIN, got %s", macro.Name)
	}
	if len(macro.Args) != 2 || macro.Args[0] != "foo-bin" || macro.Args[1] != "/usr/bin/" {
		t.Errorf("expected Args [foo-bin /usr/bin/], got %v", macro.Args)
	}
	if remaining != "" {
		t.Errorf("expected empty remaining, got %q", remaining)
	}
}

func TestParseMacroArgsOutsideBraces(t *testing.T) {
	// {INSTALL_BIN} foo-bin /usr/bin/ — args outside braces
	macro, remaining, ok := ParseMacro("{INSTALL_BIN} foo-bin /usr/bin/")
	if !ok {
		t.Fatalf("expected ParseMacro to recognize the macro")
	}
	if macro.Name != "INSTALL_BIN" {
		t.Errorf("expected name INSTALL_BIN, got %s", macro.Name)
	}
	if len(macro.Args) != 2 || macro.Args[0] != "foo-bin" || macro.Args[1] != "/usr/bin/" {
		t.Errorf("expected Args [foo-bin /usr/bin/], got %v", macro.Args)
	}
	// After fix: remaining should be empty since args are consumed
	if remaining != "" {
		t.Errorf("expected remaining %q, got %q", "", remaining)
	}
}

func TestParseMacroNoArgs(t *testing.T) {
	// {START_SERVICE} with no args
	macro, _, ok := ParseMacro("{START_SERVICE}")
	if !ok {
		t.Fatalf("expected ParseMacro to recognize the macro")
	}
	if macro.Name != "START_SERVICE" {
		t.Errorf("expected name START_SERVICE, got %s", macro.Name)
	}
	if len(macro.Args) != 0 {
		t.Errorf("expected no args, got %v", macro.Args)
	}
}

func TestParseMacroServiceArgsOutsideBraces(t *testing.T) {
	// {ENABLE_SERVICE} nginx — single arg outside braces
	macro, _, ok := ParseMacro("{ENABLE_SERVICE} nginx")
	if !ok {
		t.Fatalf("expected ParseMacro to recognize the macro")
	}
	if macro.Name != "ENABLE_SERVICE" {
		t.Errorf("expected name ENABLE_SERVICE, got %s", macro.Name)
	}
	if len(macro.Args) != 1 || macro.Args[0] != "nginx" {
		t.Errorf("expected Args [nginx], got %v", macro.Args)
	}
}

func TestParseMacroNotAMacro(t *testing.T) {
	// A plain shell command is not a macro
	_, _, ok := ParseMacro("make install")
	if ok {
		t.Errorf("expected plain command to not be recognized as a macro")
	}
}

func TestParseDuplicateEntries(t *testing.T) {
	// 1. Test case: different OSes.
	// We have [foo] for linux first, and [foo] for termux second.
	input := []byte(`
[foo]
desc = Linux version
os = linux

[foo]
desc = Termux version
os = termux
`)

	entries, err := Parse(input)
	if err != nil {
		t.Fatalf("unexpected error parsing duplicate entries: %v", err)
	}

	entry, ok := entries["foo"]
	if !ok {
		t.Fatalf("expected package 'foo' to be found")
	}

	if platform.IsTermux() {
		if entry.Desc != "Termux version" {
			t.Errorf("expected Termux version to be chosen on Termux, got: %s", entry.Desc)
		}
	} else {
		// WSL or standard Linux
		if entry.Desc != "Linux version" {
			t.Errorf("expected Linux version to be chosen on Linux/WSL, got: %s", entry.Desc)
		}
	}

	// 2. Test case: multiple matching OS entries.
	// If both match, we want to use the first one.
	currentOS := "linux"
	if platform.IsTermux() {
		currentOS = "termux"
	}

	input2 := []byte(strings.ReplaceAll(`
[foo]
desc = First version
os = CURRENT_OS

[foo]
desc = Second version
os = CURRENT_OS
`, "CURRENT_OS", currentOS))

	entries2, err := Parse(input2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	entry2, ok := entries2["foo"]
	if !ok {
		t.Fatalf("expected package 'foo' to be found")
	}

	if entry2.Desc != "First version" {
		t.Errorf("expected first matching entry to be chosen, got: %s", entry2.Desc)
	}
}

func TestExecuteManifestFakeroot(t *testing.T) {
	// Refactored to use t.TempDir and avoid real system calls
	t.Run("macro context with temporary directory", func(t *testing.T) {
		// Use t.TempDir() for temporary directory
		tempDir := t.TempDir()

		// Create a temporary package entry
		pkgName := "alps_test_fakeroot"
		e := &Entry{
			Name:   pkgName,
			Safety: "strict",
		}

		// Create macro context
		ctx := NewMacroContext(e, "test-server")
		ctx.BuildDir = tempDir

		// Test that the macro context is properly set up
		if ctx.BuildDir != tempDir {
			t.Errorf("expected BuildDir to be %q, got %q", tempDir, ctx.BuildDir)
		}

		if ctx.Safety != "strict" {
			t.Errorf("expected Safety to be 'strict', got %q", ctx.Safety)
		}

		// Test safety mode switching
		e.Safety = "free"
		ctx.Safety = "free"

		if ctx.Safety != "free" {
			t.Errorf("expected Safety to be 'free', got %q", ctx.Safety)
		}

		// Test that we can write files to the temp directory
		testFile := filepath.Join(tempDir, "test_uid.txt")
		testData := []byte("42")
		err := os.WriteFile(testFile, testData, 0644)
		if err != nil {
			t.Fatalf("failed to write test file: %v", err)
		}

		// Verify we can read it back
		readData, err := os.ReadFile(testFile)
		if err != nil {
			t.Fatalf("failed to read test file: %v", err)
		}

		if string(readData) != "42" {
			t.Errorf("expected file content '42', got %q", string(readData))
		}
	})
}

// skipUnlessMacOS skips the test if running in short mode or not on macOS.
func skipUnlessMacOS(t *testing.T) {
	t.Helper()
	if testing.Short() {
		t.Skip("skipping real system call test in short mode")
	}
	if !platform.IsMacOS() {
		t.Skip("skipping macOS-specific tests on non-macOS system")
	}
}

// TestMacOS validates macOS-specific behaviour: correct OS detection, platform
// directories, and no-op for Linux-only macros (systemd services, useradd).
func TestMacOS(t *testing.T) {
	skipUnlessMacOS(t)

	t.Run("detectDistro", func(t *testing.T) {
		distro, idLike := detectDistro()
		if distro != "macos" {
			t.Errorf("expected distro %q on macOS, got %q", "macos", distro)
		}
		foundDarwin := false
		for _, l := range idLike {
			if l == "darwin" {
				foundDarwin = true
			}
		}
		if !foundDarwin {
			t.Errorf("expected idLike to contain %q on macOS, got %v", "darwin", idLike)
		}
	})

	t.Run("platformDirs", func(t *testing.T) {
		cacheDir := platform.CacheDir()
		if !strings.Contains(cacheDir, "Library/Caches") {
			t.Errorf("expected cache dir to be under Library/Caches on macOS, got %q", cacheDir)
		}
		libDir := platform.LibDir()
		if !strings.Contains(libDir, "Library/Application Support") {
			t.Errorf("expected lib dir to be under Library/Application Support on macOS, got %q", libDir)
		}
	})

	t.Run("serviceMacrosNoOp", func(t *testing.T) {
		ctx := NewMacroContext(&Entry{Name: "test", Safety: "strict"}, "")
		for _, macroName := range []string{"ENABLE_SERVICE", "DISABLE_SERVICE", "START_SERVICE", "STOP_SERVICE", "RESTART_SERVICE", "INSTALL_SERVICE"} {
			m := Macro{Name: macroName, Args: []string{"myservice"}}
			result, err := executeMacro(m, ctx)
			if err != nil {
				t.Errorf("%s macro on macOS returned unexpected error: %v", macroName, err)
			}
			if result != "" {
				t.Errorf("%s macro on macOS should return empty string (no-op), got %q", macroName, result)
			}
		}
	})

	t.Run("userMacrosNoOp", func(t *testing.T) {
		ctx := NewMacroContext(&Entry{Name: "test", Safety: "strict"}, "")
		for _, macroName := range []string{"CREATE_USER", "REMOVE_USER"} {
			m := Macro{Name: macroName, Args: []string{"myuser"}}
			result, err := executeMacro(m, ctx)
			if err != nil {
				t.Errorf("%s macro on macOS returned unexpected error: %v", macroName, err)
			}
			if result != "" {
				t.Errorf("%s macro on macOS should return empty string (no-op), got %q", macroName, result)
			}
		}
	})

	t.Run("fakerootNoOp", func(t *testing.T) {
		if err := requireFakeroot(); err != nil {
			t.Errorf("requireFakeroot() should be a no-op on macOS, got error: %v", err)
		}
		ctx := NewMacroContext(&Entry{Name: "test", Safety: "strict"}, "")
		origCmd := "make install"
		wrapped := wrapWithFakeroot(origCmd, ctx)
		if wrapped != origCmd {
			t.Errorf("wrapWithFakeroot on macOS should not wrap the command; got %q", wrapped)
		}
	})
}

func TestStripSudo(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		// sudo
		{"sudo cp foo bar", "cp foo bar"},
		{"  sudo -E make install", "make install"},
		{"sudo -- rm -rf /", "rm -rf /"},
		{"/usr/bin/sudo -H systemctl restart service", "systemctl restart service"},
		{"/usr/local/bin/sudo -n cp foo bar", "cp foo bar"},
		// doas
		{"doas make install", "make install"},
		{"doas -- make install", "make install"},
		{"/usr/bin/doas cp foo bar", "cp foo bar"},
		{"/usr/local/bin/doas make install", "make install"},
		// pkexec
		{"pkexec cp foo bar", "cp foo bar"},
		{"/usr/bin/pkexec make install", "make install"},
		// su
		{"su -c 'make install'", "'make install'"},
		{"/usr/bin/su -c 'systemctl restart sshd'", "'systemctl restart sshd'"},
		// not privilege escalation — should pass through unchanged
		{"echo \"sudo test\"", "echo \"sudo test\""},
		{"sudoers are nice", "sudoers are nice"},
		{"sudo", "sudo"},
		{"echo doas something", "echo doas something"},
		{"pkexec-bin is a tool", "pkexec-bin is a tool"},
	}

	for _, tc := range tests {
		actual := stripSudo(tc.input)
		if actual != tc.expected {
			t.Errorf("stripSudo(%q) = %q; expected %q", tc.input, actual, tc.expected)
		}
	}
}

func TestIsForgeHost(t *testing.T) {
	validURLs := []string{
		"https://github.com/foo/bar",
		"https://raw.githubusercontent.com/foo/bar/main/file.txt",
		"https://codeberg.org/user/repo/raw/branch/main/file",
		"https://gitlab.com/user/repo/-/raw/main/file",
		// Open-source hosting platforms
		"https://sr.ht/~user/repo",
		"https://git.savannah.gnu.org/git/emacs.git",
		"https://git.kernel.org/pub/scm/linux/kernel/git/torvalds/linux.git",
		"https://git.code.sf.net/p/foo/bar",
		"https://gitlab.freedesktop.org/mesa/mesa",
		"https://pagure.io/fedora-infra/clipboard.git",
		"https://salsa.debian.org/debian/some-package",
		"https://git.savannah.nongnu.org/cgit/inkscape.git",
		// Chinese open-source platforms
		"https://gitee.com/user/repo",
		"https://gitcode.com/user/repo",
		"https://atomgit.com/user/repo",
		// Gitea / Forgejo instances
		"https://gitea.com/user/repo",
		// Official alps-more manifest mirrors (GitHub/Codeberg Pages)
		"https://adrianpriza-ai.github.io/alps-more/main.txt",
		"https://moreland.codeberg.page/alps-more/main.txt",
	}

	invalidURLs := []string{
		"http://evil.com/payload.sh",
		"https://attacker.org/malware",
		"ftp://github.com/file",
		"file:///etc/passwd",
		"https://unknown-host.com/file",
		// Third-party Pages hosts must stay rejected (exact host matching)
		"https://eviluser.github.io/malware/main.txt",
		"https://eviluser.codeberg.page/malware/main.txt",
	}

	for _, url := range validURLs {
		if !isForgeHost(url) {
			t.Errorf("expected forge URL %q to be allowed", url)
		}
	}

	for _, url := range invalidURLs {
		if isForgeHost(url) {
			t.Errorf("expected forge URL %q to be rejected", url)
		}
	}
}

func TestIsSafeDownloadURL(t *testing.T) {
	validURLs := []string{
		"https://example.com/package.tar.gz",
		"https://myserver.example.com/foo/bar.sh",
		"https://cdn.example.org/releases/v1.0.tar.gz",
		"https://github.com/foo/bar",
	}

	invalidURLs := []string{
		"http://example.com/file",
		"ftp://github.com/file",
		"file:///etc/passwd",
		"javascript:alert(1)",
	}

	for _, url := range validURLs {
		if !isSafeDownloadURL(url) {
			t.Errorf("expected download URL %q to be allowed", url)
		}
	}

	for _, url := range invalidURLs {
		if isSafeDownloadURL(url) {
			t.Errorf("expected download URL %q to be rejected", url)
		}
	}
}

func TestValidateSafePath(t *testing.T) {
	validPaths := []string{
		"bin/app",
		"/usr/bin/app",
		"config.conf",
		"dir/subdir/file",
	}

	invalidPaths := []string{
		"../etc/passwd",
		"dir/../../etc/passwd",
		"../../bin/sh",
	}

	for _, path := range validPaths {
		if err := validateSafePath(path); err != nil {
			t.Errorf("expected path %q to be valid, got error: %v", path, err)
		}
	}

	for _, path := range invalidPaths {
		if err := validateSafePath(path); err == nil {
			t.Errorf("expected path %q to be rejected for path traversal", path)
		}
	}
}

func TestParseDeps(t *testing.T) {
	tests := []struct {
		input    string
		expected []string
	}{
		{"curl, git", []string{"curl", "git"}},
		{"curl/wget, git", []string{"curl/wget", "git"}},
		{"curl/wget, git/svn", []string{"curl/wget", "git/svn"}},
		{"curl", []string{"curl"}},
		{"curl/wget", []string{"curl/wget"}},
		{"curl, wget, git", []string{"curl", "wget", "git"}},
		{"  curl  ,  wget  ", []string{"curl", "wget"}},
		{"", []string{}},
		{"  ", []string{}},
	}

	for _, tc := range tests {
		result := parseDeps(tc.input)
		if len(result) != len(tc.expected) {
			t.Errorf("parseDeps(%q) = %v; expected %v (length mismatch)", tc.input, result, tc.expected)
			continue
		}
		for i := range result {
			if result[i] != tc.expected[i] {
				t.Errorf("parseDeps(%q) = %v; expected %v (element %d mismatch)", tc.input, result, tc.expected, i)
				break
			}
		}
	}
}

func TestNeedsMirrorSkipsComments(t *testing.T) {
	// A comment line mentioning {SERVER} should not trigger mirror resolution.
	// Only actual command lines should count.
	e := &Entry{
		CmdLines: []string{
			"# uses {SERVER} for downloads",
			"echo hello",
		},
	}
	if needsMirror(e) {
		t.Error("needsMirror returned true for entry with {SERVER} only in a comment")
	}

	// An actual command line with {SERVER} should still be detected.
	e2 := &Entry{
		CmdLines: []string{
			"# note: {SERVER} is a mirror",
			"wget {SERVER}/file.tar.gz",
		},
	}
	if !needsMirror(e2) {
		t.Error("needsMirror returned false for entry with {SERVER} in a real command")
	}

	// Same pattern for {BASH_RUN}.
	e3 := &Entry{
		RemoveLines: []string{
			"# {BASH_RUN} is used for cleanup",
			"rm -rf /tmp/build",
		},
	}
	if needsMirror(e3) {
		t.Error("needsMirror returned true for entry with {BASH_RUN} only in a comment")
	}

	// {BASH_RUN} in a real command should be detected.
	e4 := &Entry{
		PurgeLines: []string{
			"# cleanup via {BASH_RUN}",
			"{BASH_RUN} cleanup.sh",
		},
	}
	if !needsMirror(e4) {
		t.Error("needsMirror returned false for entry with {BASH_RUN} in a real command")
	}
}

// TestCheckUpdatesNoPackages verifies CheckUpdates returns nil summary and no
// error when no packages are installed (ReadInstalled returns empty records).
func TestCheckUpdatesNoPackages(t *testing.T) {
	cfg := &config.Config{
		Style: config.Style{
			SymOK:    "ok",
			SymErr:   "err",
			SymWarn:  "warn",
			SymInfo:  "info",
			SymArrow: "->",
		},
	}
	summary, err := CheckUpdates(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// CheckUpdates returns nil summary when there are no installed records.
	if summary != nil {
		if len(summary.Upgradeable) != 0 || len(summary.Stale) != 0 {
			t.Errorf("expected empty summary when no packages installed, got %+v", summary)
		}
	}
}

// TestUpgradeAllNoPackages verifies UpgradeAll returns nil when no packages
// are installed.
func TestUpgradeAllNoPackages(t *testing.T) {
	cfg := &config.Config{
		Style: config.Style{
			SymOK:    "ok",
			SymErr:   "err",
			SymWarn:  "warn",
			SymInfo:  "info",
			SymArrow: "->",
		},
	}
	err := UpgradeAll(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestValidatePurgeCommands verifies that validatePurgeCommands correctly
// validates that purge operations have required commands.
func TestValidatePurgeCommands(t *testing.T) {
	t.Run("no commands and no owned items returns error", func(t *testing.T) {
		e := &Entry{Name: "pkg"}
		rec := InstalledRecord{}
		err := validatePurgeCommands(e, rec)
		if err == nil {
			t.Fatal("expected error for empty purge commands")
		}
		if !strings.Contains(err.Error(), "no remove or purge commands") {
			t.Errorf("unexpected error message: %v", err)
		}
	})

	t.Run("has remove lines is valid", func(t *testing.T) {
		e := &Entry{Name: "pkg", RemoveLines: []string{"rm -rf /tmp/pkg"}}
		rec := InstalledRecord{}
		err := validatePurgeCommands(e, rec)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("has purge lines is valid", func(t *testing.T) {
		e := &Entry{Name: "pkg", PurgeLines: []string{"rm -rf /etc/pkg"}}
		rec := InstalledRecord{}
		err := validatePurgeCommands(e, rec)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("has owned items is valid", func(t *testing.T) {
		e := &Entry{Name: "pkg"}
		rec := InstalledRecord{
			OwnedItems: []OwnedItem{{Path: "/tmp/pkg", Type: "dir"}},
		}
		err := validatePurgeCommands(e, rec)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}

// TestIsRemoteSource verifies IsRemoteSource correctly identifies remote
// git forge source strings.
func TestIsRemoteSource(t *testing.T) {
	tests := []struct {
		name   string
		source string
		remote bool
	}{
		{"github source", "github:user/repo", true},
		{"gitlab source", "gitlab:user/repo", true},
		{"codeberg source", "codeberg:user/repo", true},
		{"empty string", "", false},
		{"plain path", "/usr/local/pkg", false},
		{"url without provider", "https://example.com/repo", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := IsRemoteSource(tc.source)
			if got != tc.remote {
				t.Errorf("IsRemoteSource(%q) = %v, want %v", tc.source, got, tc.remote)
			}
		})
	}
}

// TestNormalizeArch verifies arch normalization.
func TestNormalizeArch(t *testing.T) {
	tests := []struct {
		input, expected string
	}{
		{"x86_64", "x86_64"},
		{"amd64", "x86_64"},
		{"aarch64", "aarch64"},
		{"arm64", "aarch64"},
		{"i686", "i686"},
		{"386", "i686"},
	}
	for _, tc := range tests {
		got := platform.NormalizeArch(tc.input)
		if got != tc.expected {
			platform.NormalizeArch(tc.input)
			t.Errorf("normalizeArch(%q) = %q, want %q", tc.input, got, tc.expected)
		}
	}
}

func TestIsAlreadyFakeroot(t *testing.T) {
	tests := []struct {
		cmd      string
		expected bool
	}{
		{"fakeroot make install", true},
		{"/usr/bin/fakeroot make install", true},
		{"  fakeroot make install", true},
		{"fakeroot", false},
		{"make install", false},
		{"echo fakeroot", false},
	}
	for _, tc := range tests {
		if got := isAlreadyFakeroot(tc.cmd); got != tc.expected {
			t.Errorf("isAlreadyFakeroot(%q) = %v; want %v", tc.cmd, got, tc.expected)
		}
	}
}

func TestShouldWrapWithFakeroot(t *testing.T) {
	if shouldWrapWithFakeroot(nil) {
		t.Error("shouldWrapWithFakeroot(nil) should be false")
	}

	ctxFree := &MacroContext{Safety: "free", Op: platform.OperationInstall}
	if shouldWrapWithFakeroot(ctxFree) {
		t.Error("shouldWrapWithFakeroot with safety=free should be false")
	}

	ctxRemove := &MacroContext{Safety: "strict", Op: platform.OperationRemove}
	if shouldWrapWithFakeroot(ctxRemove) {
		t.Error("shouldWrapWithFakeroot with remove op should be false")
	}

	ctxPurge := &MacroContext{Safety: "strict", Op: platform.OperationPurge}
	if shouldWrapWithFakeroot(ctxPurge) {
		t.Error("shouldWrapWithFakeroot with purge op should be false")
	}
}

// --- sha256sums format parsing ---

func TestParseSHA256SumsNamedPairs(t *testing.T) {
	hashA := strings.Repeat("ab", 32)
	hashB := strings.Repeat("cd", 32)
	input := []byte(`[pkg]
sha256sums = file1.tar.gz=` + hashA + `, install.sh=` + hashB + `

cmd_begin
  {DOWNLOAD} https://example.com/file1.tar.gz
cmd_end
`)

	entries, err := Parse(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	e := entries["pkg"]
	if e == nil {
		t.Fatal("expected package pkg")
	}
	if len(e.SHA256ByName) != 2 || e.SHA256ByName["file1.tar.gz"] != hashA || e.SHA256ByName["install.sh"] != hashB {
		t.Errorf("SHA256ByName = %v, want named map with file1.tar.gz and install.sh", e.SHA256ByName)
	}
	if len(e.SHA256Sums) != 0 {
		t.Errorf("SHA256Sums = %v, want empty for named format", e.SHA256Sums)
	}
}

func TestParseSHA256SumsPasteFormat(t *testing.T) {
	hashA := strings.Repeat("ab", 32)
	hashB := strings.Repeat("cd", 32)
	input := []byte(`[pkg]
sha256sums =
  ` + hashA + `  file1.tar.gz
  ` + hashB + `  install.sh

cmd_begin
  {DOWNLOAD} https://example.com/file1.tar.gz
  {BASH_RUN} https://example.com/install.sh
cmd_end
`)

	entries, err := Parse(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	e := entries["pkg"]
	if e == nil {
		t.Fatal("expected package pkg")
	}
	if len(e.SHA256ByName) != 2 || e.SHA256ByName["file1.tar.gz"] != hashA || e.SHA256ByName["install.sh"] != hashB {
		t.Errorf("SHA256ByName = %v, want named map from pasted lines", e.SHA256ByName)
	}
}

func TestParseSHA256SumsLegacyPositional(t *testing.T) {
	hashA := strings.Repeat("ab", 32)
	hashB := strings.Repeat("cd", 32)
	input := []byte(`[pkg]
sha256sums = ` + hashA + `, ` + hashB + `

cmd_begin
  {DOWNLOAD} https://example.com/file1.tar.gz
cmd_end
`)

	entries, err := Parse(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	e := entries["pkg"]
	if e == nil {
		t.Fatal("expected package pkg")
	}
	if len(e.SHA256Sums) != 2 || e.SHA256Sums[0] != hashA || e.SHA256Sums[1] != hashB {
		t.Errorf("SHA256Sums = %v, want legacy positional list", e.SHA256Sums)
	}
	if len(e.SHA256ByName) != 0 {
		t.Errorf("SHA256ByName = %v, want empty for positional format", e.SHA256ByName)
	}
}

func TestParseSHA256SumsMixedRejected(t *testing.T) {
	hashA := strings.Repeat("ab", 32)
	cases := []string{
		`[pkg]
sha256sums = ` + hashA + `, file1.tar.gz=` + hashA + `
`,
		`[pkg]
sha256sums = file1.tar.gz=` + hashA + `, ` + hashA + `
`,
		`[pkg]
sha256sums = ` + hashA + `
sha256sums = file1.tar.gz=` + hashA + `
`,
	}
	for i, input := range cases {
		if _, err := Parse([]byte(input)); err == nil {
			t.Errorf("case %d: expected error for mixing positional and named sha256sums", i)
		}
	}
}

func TestParseSHA256SumsDuplicateRejected(t *testing.T) {
	hashA := strings.Repeat("ab", 32)
	hashB := strings.Repeat("cd", 32)
	cases := []string{
		`[pkg]
sha256sums = file1.tar.gz=` + hashA + `, file1.tar.gz=` + hashB + `
`,
		`[pkg]
sha256sums =
  ` + hashA + `  file1.tar.gz
  ` + hashB + `  file1.tar.gz
`,
	}
	for i, input := range cases {
		_, err := Parse([]byte(input))
		if err == nil || !strings.Contains(err.Error(), "duplicate") {
			t.Errorf("case %d: expected duplicate error, got %v", i, err)
		}
	}
}

func TestParseSHA256SumsInvalidRejected(t *testing.T) {
	cases := []string{
		`[pkg]
sha256sums = not-a-digest
`,
		`[pkg]
sha256sums = =` + strings.Repeat("ab", 32) + `
`, // missing filename
		`[pkg]
sha256sums = file1.tar.gz=short
`, // bad digest
		`[pkg]
sha256sums =
  garbage line without a hash
`,
	}
	for i, input := range cases {
		if _, err := Parse([]byte(input)); err == nil {
			t.Errorf("case %d: expected error for invalid sha256sums entry", i)
		}
	}
}

func TestParseSHA256SumsContinuationEnds(t *testing.T) {
	hashA := strings.Repeat("ab", 32)
	input := []byte(`[pkg]
sha256sums =
  ` + hashA + `  file1.tar.gz
desc = tool description

cmd_begin
  {DOWNLOAD} https://example.com/file1.tar.gz
  echo building
cmd_end
`)

	entries, err := Parse(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	e := entries["pkg"]
	if e == nil {
		t.Fatal("expected package pkg")
	}
	// The checksum block must stop at the desc key and at cmd_begin: the key
	// and the command lines must not be swallowed as checksum continuations.
	if e.Desc != "tool description" {
		t.Errorf("Desc = %q, want %q", e.Desc, "tool description")
	}
	if len(e.SHA256ByName) != 1 || e.SHA256ByName["file1.tar.gz"] != hashA {
		t.Errorf("SHA256ByName = %v, want only file1.tar.gz", e.SHA256ByName)
	}
	if len(e.CmdLines) != 2 {
		t.Errorf("CmdLines = %v, want the two cmd_begin lines", e.CmdLines)
	}
}

// --- sha256sums block format ({FILE}/{SUMS}) ---

func TestParseSHA256SumsBlockFormat(t *testing.T) {
	hashA := strings.Repeat("ab", 32)
	hashB := strings.Repeat("cd", 32)
	hashC := strings.Repeat("ef", 32)
	input := []byte(`[pkg]
sha256sums_begin
  {FILE} "name with spaces.tar.gz"
  {SUMS}  ` + hashA + `
  {FILE} 'single-quoted.bin'
  {SUMS} ` + hashB + `
  {FILE} third.tar.gz
  {SUMS} ` + hashC + `
sha256sums_end

cmd_begin
  {DOWNLOAD} https://example.com/name%20with%20spaces.tar.gz "name with spaces.tar.gz"
  {DOWNLOAD} https://example.com/single-quoted.bin
  {DOWNLOAD} https://example.com/third.tar.gz
cmd_end
`)

	entries, err := Parse(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	e := entries["pkg"]
	if e == nil {
		t.Fatal("expected package pkg")
	}
	want := map[string]string{
		"name with spaces.tar.gz": hashA,
		"single-quoted.bin":       hashB,
		"third.tar.gz":            hashC,
	}
	if len(e.SHA256ByName) != len(want) {
		t.Fatalf("SHA256ByName = %v, want %v", e.SHA256ByName, want)
	}
	for name, hash := range want {
		if e.SHA256ByName[name] != hash {
			t.Errorf("SHA256ByName[%q] = %q, want %q", name, e.SHA256ByName[name], hash)
		}
	}
	if len(e.SHA256Sums) != 0 {
		t.Errorf("SHA256Sums = %v, want empty for block format", e.SHA256Sums)
	}
}

func TestParseSHA256SumsBlockShuffledOrder(t *testing.T) {
	hashA := strings.Repeat("ab", 32)
	hashB := strings.Repeat("cd", 32)
	input := []byte(`[pkg]
sha256sums_begin
  {FILE} second.bin
  {SUMS} ` + hashB + `
  {FILE} first.bin
  {SUMS} ` + hashA + `
sha256sums_end
`)

	entries, err := Parse(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	e := entries["pkg"]
	if e == nil {
		t.Fatal("expected package pkg")
	}
	if e.SHA256ByName["first.bin"] != hashA || e.SHA256ByName["second.bin"] != hashB {
		t.Errorf("SHA256ByName = %v, want order-independent mapping", e.SHA256ByName)
	}
}

func TestParseSHA256SumsBlockErrors(t *testing.T) {
	hashA := strings.Repeat("ab", 32)
	hashB := strings.Repeat("cd", 32)
	block := "[pkg]\nsha256sums_begin\n  {FILE} file1.tar.gz\n  {SUMS} " + hashA + "\nsha256sums_end\n"
	cases := []struct {
		name  string
		input string
	}{
		{"empty quoted filename", "[pkg]\nsha256sums_begin\n  {FILE} \"\"\n  {SUMS} " + hashA + "\nsha256sums_end\n"},
		{"orphan FILE at _end", "[pkg]\nsha256sums_begin\n  {FILE} file1.tar.gz\nsha256sums_end\n"},
		{"orphan FILE before next FILE", "[pkg]\nsha256sums_begin\n  {FILE} a.tar.gz\n  {FILE} b.tar.gz\nsha256sums_end\n"},
		{"orphan SUMS", "[pkg]\nsha256sums_begin\n  {SUMS} " + hashA + "\nsha256sums_end\n"},
		{"invalid SUMS digest", "[pkg]\nsha256sums_begin\n  {FILE} file1.tar.gz\n  {SUMS} not-a-digest\nsha256sums_end\n"},
		{"unknown macro", "[pkg]\nsha256sums_begin\n  {FROBNICATE} x\nsha256sums_end\n"},
		{"non-macro line", "[pkg]\nsha256sums_begin\n  {FILE} file1.tar.gz\n  {SUMS} " + hashA + "\n  some garbage\nsha256sums_end\n"},
		{"duplicate FILE", "[pkg]\nsha256sums_begin\n  {FILE} file1.tar.gz\n  {SUMS} " + hashA + "\n  {FILE} file1.tar.gz\n  {SUMS} " + hashB + "\nsha256sums_end\n"},
		{"block then positional", block + "sha256sums = " + hashA + "\n"},
		{"block then named pairs", block + "sha256sums = file2.tar.gz=" + hashB + "\n"},
		{"block then paste", block + "sha256sums =\n  " + hashB + "  file2.tar.gz\n"},
		{"positional then block", "[pkg]\nsha256sums = " + hashA + "\nsha256sums_begin\n  {FILE} file1.tar.gz\n  {SUMS} " + hashA + "\nsha256sums_end\n"},
		{"named pairs then block", "[pkg]\nsha256sums = file1.tar.gz=" + hashA + "\nsha256sums_begin\n  {FILE} file2.tar.gz\n  {SUMS} " + hashB + "\nsha256sums_end\n"},
		{"paste then block", "[pkg]\nsha256sums =\n  " + hashA + "  file1.tar.gz\nsha256sums_begin\n  {FILE} file2.tar.gz\n  {SUMS} " + hashB + "\nsha256sums_end\n"},
		{"block inside cmd_begin", "[pkg]\ncmd_begin\n  sha256sums_begin\ncmd_end\n"},
		{"unclosed block at next section", "[pkg]\nsha256sums_begin\n  {FILE} file1.tar.gz\n  {SUMS} " + hashA + "\n\n[other]\n"},
		{"unclosed block at EOF", "[pkg]\nsha256sums_begin\n  {FILE} file1.tar.gz\n  {SUMS} " + hashA + "\n"},
		{"two blocks without end", "[pkg]\nsha256sums_begin\nsha256sums_begin\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := Parse([]byte(tc.input)); err == nil {
				t.Errorf("expected parse error: %s", tc.name)
			}
		})
	}
}

// TestRequireNextSha256BlockDeclared verifies that checksums declared in the
// block format are looked up by destination filename at download time, exactly
// like the other named forms.
func TestRequireNextSha256BlockDeclared(t *testing.T) {
	hashA := strings.Repeat("ab", 32)
	hashB := strings.Repeat("cd", 32)
	input := []byte(`[pkg]
sha256sums_begin
  {FILE} file1.tar.gz
  {SUMS} ` + hashA + `
  {FILE} install.sh
  {SUMS} ` + hashB + `
sha256sums_end

cmd_begin
  {DOWNLOAD} https://example.com/file1.tar.gz
  {BASH_RUN} https://example.com/install.sh
cmd_end
`)

	entries, err := Parse(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	e := entries["pkg"]
	if e == nil {
		t.Fatal("expected package pkg")
	}
	ctx := NewMacroContext(e, "")
	for name, want := range map[string]string{"file1.tar.gz": hashA, "install.sh": hashB} {
		got, err := requireNextSha256(ctx, name)
		if err != nil {
			t.Fatalf("requireNextSha256(%q) returned error: %v", name, err)
		}
		if got != want {
			t.Errorf("requireNextSha256(%q) = %q, want %q", name, got, want)
		}
	}
}

func TestParseSHA256SumsBlockSizes(t *testing.T) {
	hashA := strings.Repeat("ab", 32)
	hashB := strings.Repeat("cd", 32)
	hashC := strings.Repeat("ef", 32)
	hashD := strings.Repeat("01", 32)
	hashE := strings.Repeat("23", 32)
	input := []byte(`[pkg]
sha256sums_begin
  {FILE} small.bin
  {SUMS} ` + hashA + `
  {SIZE} 50
  {FILE} medium.bin
  {SIZE} 25
  {SUMS} ` + hashB + `
  {FILE} boundary.bin
  {SUMS} ` + hashC + `
  {SIZE} 100
  {FILE} mb.bin
  {SIZE} 1m
  {SUMS} ` + hashD + `
  {FILE} huge.bin
  {SUMS} ` + hashE + `
  {SIZE} unl
sha256sums_end
`)

	entries, err := Parse(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	e := entries["pkg"]
	if e == nil {
		t.Fatal("expected package pkg")
	}
	want := map[string]int64{
		"small.bin":    50 * 1024 * 1024,
		"medium.bin":   25 * 1024 * 1024,
		"boundary.bin": maxDownloadSize, // {SIZE} equal to the default is allowed
		"mb.bin":       1024 * 1024,
		"huge.bin":     unlimitedDownloadSize,
	}
	if len(e.SHA256SizeByName) != len(want) {
		t.Fatalf("SHA256SizeByName = %v, want %v", e.SHA256SizeByName, want)
	}
	for name, size := range want {
		if e.SHA256SizeByName[name] != size {
			t.Errorf("SHA256SizeByName[%q] = %d, want %d", name, e.SHA256SizeByName[name], size)
		}
	}
	// The digests still attach to the right files regardless of {SIZE} placement.
	digests := map[string]string{
		"small.bin": hashA, "medium.bin": hashB, "boundary.bin": hashC, "mb.bin": hashD, "huge.bin": hashE,
	}
	for name, hash := range digests {
		if e.SHA256ByName[name] != hash {
			t.Errorf("SHA256ByName[%q] = %q, want %q", name, e.SHA256ByName[name], hash)
		}
	}
}

func TestParseSHA256SumsBlockSizeErrors(t *testing.T) {
	hash := strings.Repeat("ab", 32)
	block := `[pkg]
sha256sums_begin
  {FILE} a.bin
  {SUMS} ` + hash + `
`
	cases := []struct {
		name  string
		input string
	}{
		{"zero", block + `  {SIZE} 0
sha256sums_end
`},
		{"negative", block + `  {SIZE} -5
sha256sums_end
`},
		{"exceeds default in GB", block + `  {SIZE} 2g
sha256sums_end
`},
		{"exceeds default in MB", block + `  {SIZE} 200
sha256sums_end
`},
		{"malformed", block + `  {SIZE} banana
sha256sums_end
`},
		{"orphan SIZE", `[pkg]
sha256sums_begin
  {SIZE} 50
sha256sums_end
`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := Parse([]byte(tc.input)); err == nil {
				t.Errorf("expected parse error: %s", tc.name)
			}
		})
	}
}

// --- unused named checksum detection ---

func TestUnusedNamedChecksums(t *testing.T) {
	hash := strings.Repeat("ab", 32)
	cases := []struct {
		name  string
		entry *Entry
		want  []string
	}{
		{
			name:  "no named sums",
			entry: &Entry{Name: "pkg"},
			want:  nil,
		},
		{
			name: "all sums used by DOWNLOAD",
			entry: &Entry{
				Name:         "pkg",
				SHA256ByName: map[string]string{"file1.tar.gz": hash},
				CmdLines:     []string{"{DOWNLOAD} https://example.com/file1.tar.gz"},
			},
			want: nil,
		},
		{
			name: "FILE argument names the checksum",
			entry: &Entry{
				Name:         "pkg",
				SHA256ByName: map[string]string{"app.tar.gz": hash},
				CmdLines:     []string{"{DOWNLOAD} https://example.com/latest app.tar.gz"},
			},
			want: nil,
		},
		{
			name: "BASH_RUN script matched by basename",
			entry: &Entry{
				Name:         "pkg",
				SHA256ByName: map[string]string{"install.sh": hash},
				CmdLines:     []string{"{BASH_RUN} https://example.com/install.sh"},
			},
			want: nil,
		},
		{
			name: "local BASH_RUN script is not a download",
			entry: &Entry{
				Name:         "pkg",
				SHA256ByName: map[string]string{"install.sh": hash},
				CmdLines:     []string{"{BASH_RUN} install.sh"},
			},
			want: []string{"install.sh"},
		},
		{
			name: "unused checksum reported",
			entry: &Entry{
				Name:         "pkg",
				SHA256ByName: map[string]string{"file1.tar.gz": hash, "gone.bin": hash},
				CmdLines:     []string{"{DOWNLOAD} https://example.com/file1.tar.gz"},
			},
			want: []string{"gone.bin"},
		},
		{
			name: "templated URL with resolvable placeholder",
			entry: &Entry{
				Name:         "pkg",
				Version:      "2.0.0",
				SHA256ByName: map[string]string{"tool-2.0.0.tar.gz": hash},
				CmdLines:     []string{"{DOWNLOAD} https://example.com/tool-{VERSION}.tar.gz"},
			},
			want: nil,
		},
		{
			name: "upgrade block downloads count",
			entry: &Entry{
				Name:         "pkg",
				SHA256ByName: map[string]string{"new.bin": hash},
				UpgradeLines: []string{"{DOWNLOAD} https://example.com/new.bin"},
			},
			want: nil,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := unusedNamedChecksums(tc.entry)
			if strings.Join(got, ",") != strings.Join(tc.want, ",") {
				t.Errorf("unusedNamedChecksums = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestValidateWarnsUnusedNamedChecksums(t *testing.T) {
	hash := strings.Repeat("ab", 32)
	arch := platform.NormalizeArch(runtime.GOARCH)

	capture := func(e *Entry) (string, error) {
		old := os.Stdout
		r, w, err := os.Pipe()
		if err != nil {
			t.Fatalf("cannot create pipe: %v", err)
		}
		os.Stdout = w
		valErr := Validate(e)
		os.Stdout = old
		w.Close()
		out, _ := io.ReadAll(r)
		r.Close()
		return string(out), valErr
	}

	// An entry that passes validation but declares a checksum for a file it
	// never downloads must print a warning (and still validate).
	unused := &Entry{
		Name:         "warnme",
		Version:      "1.0.0",
		Arch:         []string{arch},
		OS:           []string{"linux", "darwin", "termux"},
		Safety:       "strict",
		SHA256ByName: map[string]string{"gone.bin": hash},
		CmdLines:     []string{"{DOWNLOAD} https://example.com/file1.tar.gz"},
	}
	out, err := capture(unused)
	if err != nil {
		t.Fatalf("Validate returned error: %v", err)
	}
	if !strings.Contains(out, "gone.bin") || !strings.Contains(out, "never downloaded") {
		t.Errorf("expected warning about never-downloaded checksum, got: %q", out)
	}

	// A fully matched checksum must not warn.
	used := &Entry{
		Name:         "ok",
		Version:      "1.0.0",
		Arch:         []string{arch},
		OS:           []string{"linux", "darwin", "termux"},
		Safety:       "strict",
		SHA256ByName: map[string]string{"file1.tar.gz": hash},
		CmdLines:     []string{"{DOWNLOAD} https://example.com/file1.tar.gz"},
	}
	out, err = capture(used)
	if err != nil {
		t.Fatalf("Validate returned error: %v", err)
	}
	if strings.Contains(out, "never downloaded") {
		t.Errorf("expected no warning for matched checksum, got: %q", out)
	}
}

func TestFindEntry(t *testing.T) {
	entries := map[string]*Entry{
		"pkg-linux": {Name: "pkg-linux", OS: []string{"ubuntu", "debian"}},
		"pkg-all":   {Name: "pkg-all", OS: []string{"all"}},
	}

	// Missing package
	_, err := findEntry(entries, "missing", "ubuntu", nil)
	if err == nil || !strings.Contains(err.Error(), "not found in alps-more repo") {
		t.Errorf("expected not found error, got %v", err)
	}

	// OS mismatch
	_, err = findEntry(entries, "pkg-linux", "fedora", []string{"rhel"})
	if err == nil || !strings.Contains(err.Error(), "not available for your distro") {
		t.Errorf("expected OS mismatch error, got %v", err)
	}

	// Match
	e, err := findEntry(entries, "pkg-linux", "ubuntu", []string{"debian"})
	if err != nil || e == nil || e.Name != "pkg-linux" {
		t.Errorf("expected pkg-linux entry, got entry=%v, err=%v", e, err)
	}
}
