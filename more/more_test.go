package more

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDetectDistroVersion(t *testing.T) {
	version := detectDistroVersion()
	if isTermux() {
		expected := os.Getenv("TERMUX_VERSION")
		if expected == "" {
			expected = "unknown"
		}
		if version != expected {
			t.Errorf("expected termux version %q, got %q", expected, version)
		}
	} else if isMacOS() {
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

func TestExpandVarsDisver(t *testing.T) {
	version := detectDistroVersion()
	input := "echo {DISVER} {ARCH} {OS}"
	output := expandVars(input, "my-server", "my-pkg-dir", "1.0.0")

	if !strings.Contains(output, version) {
		t.Errorf("expected expanded output to contain distro version %q, got %q", version, output)
	}
}

func TestExpandMacrosDisver(t *testing.T) {
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
	// If the system is macOS, it should match "darwin" and "macos", but NOT "linux"
	if isMacOS() {
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
	if isTermux() {
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
	if isWSL() {
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

	if isTermux() {
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
	if isTermux() {
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
	// Create a temporary package entry
	pkgName := "alps_test_fakeroot"
	e := &Entry{
		Name:   pkgName,
		Safety: "strict",
	}

	// Create macro context
	ctx := NewMacroContext(e, "test-server")

	// Ensure build directory is cleaned up
	buildDir, err := getBuildDir(pkgName)
	if err != nil {
		t.Fatalf("failed to get build dir: %v", err)
	}
	defer os.RemoveAll(buildDir)

	// We want to verify that when safety = strict, build_env runs with fakeroot if available.
	// We can write a simple command that outputs the user ID.
	// Since fakeroot makes the command run as root (uid 0), we can check if it outputs 0.
	// However, on Termux or systems without fakeroot, it won't be wrapped.
	// So we can check the behavior conditionally.
	manifest := &ExecutionManifest{
		BuildEnv: []string{"id -u > uid_strict.txt"},
		AfterEnv: []string{},
	}

	err = ExecuteManifest(manifest, e, OperationInstall, ctx)
	if err != nil {
		// If fakeroot is required but not installed, this could fail, which is expected.
		// Let's check if fakeroot exists first.
		if !hasFakeroot() && !isTermux() && !isMacOS() {
			if !strings.Contains(err.Error(), "fakeroot is required") {
				t.Errorf("expected 'fakeroot is required' error when fakeroot is missing, got: %v", err)
			}
			return
		}
		t.Fatalf("ExecuteManifest failed: %v", err)
	}

	// Read output file
	uidFile := filepath.Join(buildDir, "uid_strict.txt")
	data, err := os.ReadFile(uidFile)
	if err != nil {
		t.Fatalf("failed to read uid_strict.txt: %v", err)
	}

	uidStr := strings.TrimSpace(string(data))
	if !isTermux() && !isMacOS() && hasFakeroot() {
		if uidStr != "0" {
			t.Errorf("expected uid to be 0 under fakeroot, got %q", uidStr)
		}
	}

	// Now test safety = free
	e.Safety = "free"
	ctx.Safety = "free"

	manifestFree := &ExecutionManifest{
		BuildEnv: []string{"id -u > uid_free.txt"},
		AfterEnv: []string{},
	}

	err = ExecuteManifest(manifestFree, e, OperationInstall, ctx)
	if err != nil {
		t.Fatalf("ExecuteManifest failed with safety=free: %v", err)
	}

	uidFileFree := filepath.Join(buildDir, "uid_free.txt")
	dataFree, err := os.ReadFile(uidFileFree)
	if err != nil {
		t.Fatalf("failed to read uid_free.txt: %v", err)
	}

	uidStrFree := strings.TrimSpace(string(dataFree))
	if !isTermux() && !isMacOS() && hasFakeroot() {
		// If running as a normal user under free mode, uid should not be 0
		// (Unless the test itself is running as root)
		if os.Getuid() != 0 && uidStrFree == "0" {
			t.Errorf("expected uid to not be 0 under free mode, got %q", uidStrFree)
		}
	}
}

// TestMacOS validates macOS-specific behaviour: correct OS detection, platform
// directories, and no-op for Linux-only macros (systemd services, useradd).
func TestMacOS(t *testing.T) {
	if !isMacOS() {
		t.Skip("skipping macOS-specific tests on non-macOS system")
	}

	// detectDistro should return "macos"
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

	// Cache dir should be inside ~/Library/Caches
	cacheDir := getCacheDir()
	if !strings.Contains(cacheDir, "Library/Caches") {
		t.Errorf("expected cache dir to be under Library/Caches on macOS, got %q", cacheDir)
	}

	// Lib dir should be inside ~/Library/Application Support
	libDir := getLibDir()
	if !strings.Contains(libDir, "Library/Application Support") {
		t.Errorf("expected lib dir to be under Library/Application Support on macOS, got %q", libDir)
	}

	// Service macros should be no-ops on macOS
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

	// User macros should be no-ops on macOS
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

	// requireFakeroot should not error on macOS (exempt)
	if err := requireFakeroot(); err != nil {
		t.Errorf("requireFakeroot() should be a no-op on macOS, got error: %v", err)
	}

	// wrapWithFakeroot should not wrap on macOS
	origCmd := "make install"
	wrapped := wrapWithFakeroot(origCmd, ctx)
	if wrapped != origCmd {
		t.Errorf("wrapWithFakeroot on macOS should not wrap the command; got %q", wrapped)
	}
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

func TestIsAllowedURL(t *testing.T) {
	validURLs := []string{
		"https://github.com/foo/bar",
		"https://raw.githubusercontent.com/foo/bar/main/file.txt",
		"https://adrianpriza-ai.github.io/alps-more/main.txt",
		"https://codeberg.org/user/repo/raw/branch/main/file",
		"https://moreland.codeberg.page/alps-more/main.txt",
		"https://gitlab.com/user/repo/-/raw/main/file",
	}

	invalidURLs := []string{
		"http://evil.com/payload.sh",
		"https://attacker.org/malware",
		"ftp://github.com/file",
		"file:///etc/passwd",
	}

	for _, url := range validURLs {
		if !isAllowedURL(url) {
			t.Errorf("expected URL %q to be allowed", url)
		}
	}

	for _, url := range invalidURLs {
		if isAllowedURL(url) {
			t.Errorf("expected URL %q to be rejected", url)
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
