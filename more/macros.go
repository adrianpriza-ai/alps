package more

import (
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

func getSymOK() string {
	return currentStyle().SymOK
}

// isTermux checks if running in Termux.
func isTermux() bool {
	return os.Getenv("TERMUX_VERSION") != "" ||
		os.Getenv("PREFIX") == "/data/data/com.termux/files/usr"
}

// termuxPrefix returns the Termux $PREFIX when running inside Termux,
// or an empty string on regular Linux/WSL systems.
func termuxPrefix() string {
	if !isTermux() {
		return ""
	}
	prefix := os.Getenv("PREFIX")
	if prefix == "" {
		prefix = "/data/data/com.termux/files/usr"
	}
	return prefix
}

// macOSPrefix returns the macOS prefix when running on macOS.
// On macOS, we typically use /usr/local for manual installations,
// but respect Homebrew's prefix if available.
func macOSPrefix() string {
	if !isMacOS() {
		return ""
	}
	// Check for Homebrew prefix
	if prefix := os.Getenv("HOMEBREW_PREFIX"); prefix != "" {
		return prefix
	}
	// Default to /usr/local on macOS
	return "/usr/local"
}

// validateSafePath ensures target paths do not contain path traversal (..)
func validateSafePath(path string) error {
	if path == "" {
		return nil
	}
	clean := filepath.Clean(path)
	// Check for '..' components in path
	parts := strings.Split(filepath.ToSlash(clean), "/")
	for _, part := range parts {
		if part == ".." {
			return fmt.Errorf("path traversal detected in path %q", path)
		}
	}
	return nil
}

// wrapWithFakeroot wraps a command with fakeroot if the operation is install/upgrade, safety mode is strict, not in Termux, and fakeroot is available.
// Remove/purge are never wrapped — they need real privileges to delete files.
// On macOS, fakeroot is not available, so this is a no-op.
func wrapWithFakeroot(cmd string, ctx *MacroContext) string {
	isInstallOp := ctx.Op == OperationInstall || ctx.Op == OperationUpgrade || ctx.Op == ""
	if isInstallOp && (ctx.Safety == "strict" || ctx.Safety == "") && !isTermux() && !isMacOS() && !isRoot() && hasFakeroot() {
		cmd = stripSudo(cmd)
		return fmt.Sprintf("fakeroot -- %s", cmd)
	}
	return cmd
}

// InstalledPath tracks a file/directory installed via macros for automatic uninstall
type InstalledPath struct {
	Path      string
	Type      string // "file", "dir", "symlink", "service"
	Generated bool   // true if this was auto-generated for uninstall
}

// DeferredOperation represents a file operation to be executed after commands complete
type DeferredOperation struct {
	Type    string // "install", "install_dir", "symlink", "enable_service", "start_service", "create_user", etc.
	Src     string
	Dst     string
	Mode    string
	CmdArgs []string // for command-based deferred ops (service control, user management)
}

// Macro represents an installation macro
type Macro struct {
	Name string
	Args []string
}

// MacroContext holds context for macro expansion and execution
type MacroContext struct {
	PackageName    string
	Version        string
	Server         string
	Arch           string
	OS             string              // Operating system (linux, darwin, etc.)
	Distro         string              // Linux distribution ID (ubuntu, debian, etc.)
	Safety         string              // "strict" or "free"
	SHA256Sums     []string            // SHA-256 checksums for downloads
	SHA256Index    int                 // Current index for SHA256 sum assignment
	Op             OperationType       // current operation (install/upgrade/remove/purge)
	InstalledPaths []InstalledPath     // Track installed files for auto-uninstall (internal)
	DeferredOps    []DeferredOperation // Deferred file operations
	BuildDir       string              // Build directory for source files
	DistroVersion  string
}

// NewMacroContext creates a new macro execution context
func NewMacroContext(e *Entry, server string) *MacroContext {
	distroVer := detectDistroVersion()
	distro, _ := detectDistro()
	os := runtime.GOOS

	if e == nil {
		return &MacroContext{
			PackageName:    "",
			Version:        "",
			Server:         server,
			Arch:           normalizeArch(runtime.GOARCH),
			OS:             os,
			Distro:         distro,
			Safety:         "",
			SHA256Sums:     []string{},
			SHA256Index:    0,
			InstalledPaths: []InstalledPath{},
			BuildDir:       "",
			DistroVersion:  distroVer,
		}
	}
	return &MacroContext{
		PackageName:    e.Name,
		Version:        e.Version,
		Server:         server,
		Arch:           normalizeArch(runtime.GOARCH),
		OS:             os,
		Distro:         distro,
		Safety:         e.Safety,
		SHA256Sums:     e.SHA256Sums,
		SHA256Index:    0,
		InstalledPaths: []InstalledPath{},
		BuildDir:       "",
		DistroVersion:  distroVer,
	}
}

// isKnownMacro checks if a macro name is recognized by ALPS.
func isKnownMacro(name string) bool {
	name = strings.ToUpper(name)
	_, ok := macroRegistry[name]
	return ok
}

// ParseMacro parses a macro from a command line, handling both {MACRO arg1 arg2} (args inside braces) and {MACRO} arg1 arg2 (args outside braces).
// Returns (macro, remainingLine, isMacro).
func ParseMacro(line string) (Macro, string, bool) {
	if !strings.HasPrefix(line, "{") {
		return Macro{}, line, false
	}

	end := strings.Index(line, "}")
	if end == -1 {
		return Macro{}, line, false
	}

	macroText := line[1:end]
	parts := strings.Fields(macroText)
	if len(parts) == 0 {
		return Macro{}, line, false
	}

	name := strings.ToUpper(parts[0])
	if name == "DONWLOAD" {
		fmt.Fprintf(os.Stderr, "  warning: {DONWLOAD} is deprecated, use {DOWNLOAD} instead\n")
		name = "DOWNLOAD"
	}

	if !isKnownMacro(name) {
		return Macro{}, line, false
	}

	macro := Macro{
		Name: name,
		Args: parts[1:],
	}

	remaining := strings.TrimSpace(line[end+1:])

	// Support {MACRO} arg1 arg2 format (args outside braces)
	if remaining != "" {
		extraArgs := strings.Fields(remaining)
		macro.Args = append(macro.Args, extraArgs...)
		// Return empty remaining since we've consumed the args
		return macro, "", true
	}

	return macro, remaining, true
}

// ExpandMacros expands macros in command lines based on safety mode
func ExpandMacros(lines []string, ctx *MacroContext) ([]string, error) {
	var expanded []string

	for _, line := range lines {
		expandedLine, err := expandLine(line, ctx)
		if err != nil {
			return nil, fmt.Errorf("macro expansion error: %w", err)
		}
		if expandedLine != "" {
			expanded = append(expanded, expandedLine)
		}
	}

	return expanded, nil
}

// replaceVars replaces all known variable placeholders in a string using a MacroContext.
// When stripUnknown is true, remaining {ALL_CAPS_TOKEN} patterns are stripped (but
// literal braces in shell expressions like ${1} or ${HOME} are preserved).
func replaceVars(s string, ctx *MacroContext, stripUnknown bool) string {
	s = strings.ReplaceAll(s, "{ARCH}", ctx.Arch)
	s = strings.ReplaceAll(s, "{OS}", ctx.OS)
	s = strings.ReplaceAll(s, "{DISTRO}", ctx.Distro)
	s = strings.ReplaceAll(s, "{VERSION}", ctx.Version)
	s = strings.ReplaceAll(s, "{PKG_DIR}", ctx.BuildDir)
	s = strings.ReplaceAll(s, "{SERVER}", ctx.Server)
	s = strings.ReplaceAll(s, "{PKGNAME}", ctx.PackageName)
	s = strings.ReplaceAll(s, "{DISVER}", ctx.DistroVersion)
	if stripUnknown {
		// Strip any remaining unknown {TOKEN} placeholders so they never reach the shell.
		// Use stripUnknownTokens to only strip {ALL_CAPS_TOKEN} patterns, preserving
		// literal braces in shell expressions like ${1} or ${HOME}.
		s = stripUnknownTokens(s)
	}
	return s
}

// expandLine expands macros in a single line
func expandLine(line string, ctx *MacroContext) (string, error) {
	line = strings.TrimSpace(line)
	if line == "" || strings.HasPrefix(line, "#") {
		return "", nil
	}

	// Replace plain variable tokens in a line that has no structured macro prefix.
	replaceLocal := func(s string) string {
		return replaceVars(s, ctx, true)
	}

	macro, remaining, isMacro := ParseMacro(line)
	if !isMacro {
		return replaceLocal(line), nil
	}

	// Expand variable tokens inside macro arguments.
	expandMacroArgs(&macro, ctx)

	result, err := executeMacro(macro, ctx)
	if err != nil {
		return "", fmt.Errorf("failed to execute macro %s: %w", macro.Name, err)
	}

	return combineMacroResult(line, result, remaining, macro, ctx)
}

// expandMacroArgs replaces variable tokens inside macro argument strings in-place.
func expandMacroArgs(macro *Macro, ctx *MacroContext) {
	for i, arg := range macro.Args {
		macro.Args[i] = replaceVars(arg, ctx, true)
	}
}

// combineMacroResult combines the result of a macro execution with any remaining text on the same line.
func combineMacroResult(rawLine, result, remaining string, macro Macro, ctx *MacroContext) (string, error) {
	if result == "" {
		// Macro produced no shell command (Go-only work, conditional no-op, or unknown macro).
		// Never pass the raw {MACRO ...} line through to the shell.
		if remaining == "" {
			return "", nil
		}
		// Process any trailing text that followed the macro on the same line.
		remainingResult, err := expandLine(remaining, ctx)
		if err != nil {
			return "", err
		}
		return remainingResult, nil
	}

	if remaining == "" {
		return result, nil
	}
	remainingResult, err := expandLine(remaining, ctx)
	if err != nil {
		return "", err
	}
	if remainingResult != "" {
		result = result + " && " + remainingResult
	}
	return result, nil
}

// macroHandler is a function that executes a named macro.
type macroHandler func(Macro, *MacroContext) (string, error)

// macroRegistry maps macro names to their handler functions.
// All macros now return shell commands for direct execution.
var macroRegistry = map[string]macroHandler{
	"DOWNLOAD":        executeDownload,
	"BASH_RUN":        executeBashRun,
	"EXTRACT":         executeExtract,
	"SH":              executeSH,
	"INSTALL_BIN":     executeInstallBin,
	"INSTALL_LIB":     executeInstallLib,
	"INSTALL_CONF":    executeInstallConf,
	"INSTALL_MAN":     executeInstallMan,
	"INSTALL_SERVICE": executeInstallService,
	"INSTALL_DIR":     executeInstallDir,
	"SYMLINK":         executeSymlink,
	"ENABLE_SERVICE":  executeEnableService,
	"DISABLE_SERVICE": executeDisableService,
	"START_SERVICE":   executeStartService,
	"STOP_SERVICE":    executeStopService,
	"RESTART_SERVICE": executeRestartService,
	"CREATE_USER":     executeCreateUser,
	"REMOVE_USER":     executeRemoveUser,
}

// isInstallOnlyMacro returns true for macros that install or create files/services/users
func isInstallOnlyMacro(name string) bool {
	switch name {
	case "INSTALL_BIN", "INSTALL_LIB", "INSTALL_CONF", "INSTALL_MAN",
		"INSTALL_SERVICE", "INSTALL_DIR", "SYMLINK", "ENABLE_SERVICE",
		"START_SERVICE", "RESTART_SERVICE", "CREATE_USER":
		return true
	default:
		return false
	}
}

// executeMacro executes a single macro and returns the shell command.
// Unknown macro names are silently ignored (return "", nil) so that typos
// or unrecognised macros never reach the shell.
func executeMacro(macro Macro, ctx *MacroContext) (string, error) {
	if ctx != nil && (ctx.Op == OperationRemove || ctx.Op == OperationPurge) && isInstallOnlyMacro(macro.Name) {
		return "", nil // Skip install/creation macros during remove or purge
	}
	handler, ok := macroRegistry[macro.Name]
	if !ok {
		return "", nil // Unknown macros are silently skipped
	}
	return handler(macro, ctx)
}

// executeInstallBin installs a binary to /usr/bin (or Termux equivalent) or specified directory
func executeInstallBin(macro Macro, ctx *MacroContext) (string, error) {
	if len(macro.Args) < 1 {
		return "", fmt.Errorf("INSTALL_BIN requires at least 1 argument: source [dest]")
	}
	if err := validateSafePath(macro.Args[0]); err != nil {
		return "", fmt.Errorf("INSTALL_BIN invalid source: %w", err)
	}

	// Track for uninstall (use the last arg as dest if provided)
	if len(macro.Args) >= 2 {
		dest := macro.Args[len(macro.Args)-1]
		if err := validateSafePath(dest); err != nil {
			return "", fmt.Errorf("INSTALL_BIN invalid dest: %w", err)
		}
		// If dest is a directory (ends with /), append the filename
		if strings.HasSuffix(dest, "/") {
			dest = dest + filepath.Base(macro.Args[0])
		}
		ctx.InstalledPaths = append(ctx.InstalledPaths, InstalledPath{
			Path:      dest,
			Type:      "file",
			Generated: true,
		})
	} else {
		// Default to /usr/bin directory (or macOS equivalent)
		var binDir string
		if isMacOS() {
			binDir = filepath.Join(macOSPrefix(), "bin")
		} else {
			binDir = filepath.Join(termuxPrefix(), "/usr/bin")
		}
		dest := filepath.Join(binDir, filepath.Base(macro.Args[0]))
		ctx.InstalledPaths = append(ctx.InstalledPaths, InstalledPath{
			Path:      dest,
			Type:      "file",
			Generated: true,
		})
	}

	// Return install command using mkdir, cp, chmod, and echo
	symOK := getSymOK()
	if len(macro.Args) >= 2 {
		dest := macro.Args[len(macro.Args)-1]
		if strings.HasSuffix(dest, "/") {
			dest = dest + filepath.Base(macro.Args[0])
		}
		return fmt.Sprintf("mkdir -p $(dirname %s) && cp %s %s && chmod 755 %s && echo \"  %s  installed %s to %s\"", dest, macro.Args[0], dest, dest, symOK, macro.Args[0], dest), nil
	}
	// Default to /usr/bin directory (or macOS equivalent)
	var binDir, dest string
	if isMacOS() {
		binDir = filepath.Join(macOSPrefix(), "bin")
	} else {
		binDir = filepath.Join(termuxPrefix(), "/usr/bin")
	}
	dest = filepath.Join(binDir, filepath.Base(macro.Args[0]))
	return fmt.Sprintf("mkdir -p %s && cp %s %s && chmod 755 %s && echo \"  %s  installed %s to %s\"", binDir, macro.Args[0], dest, dest, symOK, macro.Args[0], dest), nil
}

// executeInstallLib installs a library to /usr/lib (or Termux equivalent) or specified directory
func executeInstallLib(macro Macro, ctx *MacroContext) (string, error) {
	if len(macro.Args) < 1 {
		return "", fmt.Errorf("INSTALL_LIB requires at least 1 argument: source [dest]")
	}
	if err := validateSafePath(macro.Args[0]); err != nil {
		return "", fmt.Errorf("INSTALL_LIB invalid source: %w", err)
	}

	symOK := getSymOK()
	// Track for uninstall (use the last arg as dest if provided)
	if len(macro.Args) >= 2 {
		dest := macro.Args[len(macro.Args)-1]
		if err := validateSafePath(dest); err != nil {
			return "", fmt.Errorf("INSTALL_LIB invalid dest: %w", err)
		}
		// If dest is a directory (ends with /), append the filename
		if strings.HasSuffix(dest, "/") {
			dest = dest + filepath.Base(macro.Args[0])
		}
		ctx.InstalledPaths = append(ctx.InstalledPaths, InstalledPath{
			Path:      dest,
			Type:      "file",
			Generated: true,
		})
		// Return install command using mkdir, cp, chmod, and echo
		return fmt.Sprintf("mkdir -p $(dirname %s) && cp %s %s && chmod 644 %s && echo \"  %s  installed %s to %s\"", dest, macro.Args[0], dest, dest, symOK, macro.Args[0], dest), nil
	}
	// Default to /usr/lib directory (or macOS equivalent)
	var libDir, dest string
	if isMacOS() {
		libDir = filepath.Join(macOSPrefix(), "lib")
	} else {
		libDir = filepath.Join(termuxPrefix(), "/usr/lib")
	}
	dest = filepath.Join(libDir, filepath.Base(macro.Args[0]))
	ctx.InstalledPaths = append(ctx.InstalledPaths, InstalledPath{
		Path:      dest,
		Type:      "file",
		Generated: true,
	})
	// Return install command using mkdir, cp, chmod, and echo
	return fmt.Sprintf("mkdir -p %s && cp %s %s && chmod 644 %s && echo \"  %s  installed %s to %s\"", libDir, macro.Args[0], dest, dest, symOK, macro.Args[0], dest), nil
}

// executeInstallConf installs a config file to /etc (or Termux equivalent) or specified directory
func executeInstallConf(macro Macro, ctx *MacroContext) (string, error) {
	if len(macro.Args) < 1 {
		return "", fmt.Errorf("INSTALL_CONF requires at least 1 argument: source [dest]")
	}
	if err := validateSafePath(macro.Args[0]); err != nil {
		return "", fmt.Errorf("INSTALL_CONF invalid source: %w", err)
	}

	symOK := getSymOK()
	// Track for uninstall (use the last arg as dest if provided)
	if len(macro.Args) >= 2 {
		dest := macro.Args[len(macro.Args)-1]
		if err := validateSafePath(dest); err != nil {
			return "", fmt.Errorf("INSTALL_CONF invalid dest: %w", err)
		}
		// If dest is a directory (ends with /), append the filename
		if strings.HasSuffix(dest, "/") {
			dest = dest + filepath.Base(macro.Args[0])
		}
		ctx.InstalledPaths = append(ctx.InstalledPaths, InstalledPath{
			Path:      dest,
			Type:      "file",
			Generated: true,
		})
		// Return install command using mkdir, cp, chmod, and echo
		return fmt.Sprintf("mkdir -p $(dirname %s) && cp %s %s && chmod 644 %s && echo \"  %s  installed %s to %s\"", dest, macro.Args[0], dest, dest, symOK, macro.Args[0], dest), nil
	}
	// Default to /etc directory (or macOS equivalent)
	var etcDir, dest string
	if isMacOS() {
		etcDir = filepath.Join(macOSPrefix(), "etc") // Use /usr/local/etc on macOS for consistency
	} else {
		etcDir = filepath.Join(termuxPrefix(), "/etc")
	}
	dest = filepath.Join(etcDir, filepath.Base(macro.Args[0]))
	ctx.InstalledPaths = append(ctx.InstalledPaths, InstalledPath{
		Path:      dest,
		Type:      "file",
		Generated: true,
	})
	// Return install command using mkdir, cp, chmod, and echo
	return fmt.Sprintf("mkdir -p %s && cp %s %s && chmod 644 %s && echo \"  %s  installed %s to %s\"", etcDir, macro.Args[0], dest, dest, symOK, macro.Args[0], dest), nil
}

// executeInstallMan installs a man page to /usr/share/man (or Termux equivalent) or specified directory
func executeInstallMan(macro Macro, ctx *MacroContext) (string, error) {
	if len(macro.Args) < 1 {
		return "", fmt.Errorf("INSTALL_MAN requires at least 1 argument: source [dest]")
	}
	if err := validateSafePath(macro.Args[0]); err != nil {
		return "", fmt.Errorf("INSTALL_MAN invalid source: %w", err)
	}

	symOK := getSymOK()
	// Track for uninstall (use the last arg as dest if provided)
	if len(macro.Args) >= 2 {
		dest := macro.Args[len(macro.Args)-1]
		if err := validateSafePath(dest); err != nil {
			return "", fmt.Errorf("INSTALL_MAN invalid dest: %w", err)
		}
		// If dest is a directory (ends with /), append the filename
		if strings.HasSuffix(dest, "/") {
			dest = dest + filepath.Base(macro.Args[0])
		}
		// Add .gz extension since we gzip the man page
		if !strings.HasSuffix(dest, ".gz") {
			dest = dest + ".gz"
		}
		ctx.InstalledPaths = append(ctx.InstalledPaths, InstalledPath{
			Path:      dest,
			Type:      "file",
			Generated: true,
		})
		uncompressedDest := strings.TrimSuffix(dest, ".gz")
		return fmt.Sprintf("mkdir -p $(dirname %s) && cp %s %s && chmod 644 %s && gzip -f %s && echo \"  %s  installed %s to %s\"", uncompressedDest, macro.Args[0], uncompressedDest, uncompressedDest, uncompressedDest, symOK, macro.Args[0], dest), nil
	}
	// Default to /usr/share/man/man1 directory (or macOS equivalent)
	sourceFile := filepath.Base(macro.Args[0])
	var manDir, dest string
	if isMacOS() {
		manDir = filepath.Join(macOSPrefix(), "share/man/man1")
	} else {
		manDir = filepath.Join(termuxPrefix(), "/usr/share/man/man1")
	}
	dest = filepath.Join(manDir, sourceFile)
	// Add .gz extension since we gzip the man page
	if !strings.HasSuffix(dest, ".gz") {
		dest = dest + ".gz"
	}
	ctx.InstalledPaths = append(ctx.InstalledPaths, InstalledPath{
		Path:      dest,
		Type:      "file",
		Generated: true,
	})
	// Return install command using mkdir, cp, chmod, gzip, and echo
	// We need to copy to the destination without .gz first, then gzip it
	uncompressedDest := strings.TrimSuffix(dest, ".gz")
	return fmt.Sprintf("mkdir -p %s && cp %s %s && chmod 644 %s && gzip -f %s && echo \"  %s  installed %s to %s\"", manDir, macro.Args[0], uncompressedDest, uncompressedDest, uncompressedDest, symOK, macro.Args[0], dest), nil
}

// executeInstallService installs a systemd service file.
// On Termux and macOS, systemd is not available, so this is a no-op.
func executeInstallService(macro Macro, ctx *MacroContext) (string, error) {
	if isTermux() || isMacOS() {
		return "", nil // Termux and macOS have no systemd
	}
	if len(macro.Args) < 1 {
		return "", fmt.Errorf("INSTALL_SERVICE requires at least 1 argument: source [dest]")
	}

	if err := validateSafePath(macro.Args[0]); err != nil {
		return "", fmt.Errorf("INSTALL_SERVICE invalid source: %w", err)
	}

	symOK := getSymOK()
	// Track for uninstall (use the service name, not the full path)
	if len(macro.Args) >= 2 {
		dest := macro.Args[len(macro.Args)-1]
		if err := validateSafePath(dest); err != nil {
			return "", fmt.Errorf("INSTALL_SERVICE invalid dest: %w", err)
		}
		// If dest is a directory (ends with /), append the filename
		if strings.HasSuffix(dest, "/") {
			dest = dest + filepath.Base(macro.Args[0])
		}
		// Store just the service name (basename)
		ctx.InstalledPaths = append(ctx.InstalledPaths, InstalledPath{
			Path:      filepath.Base(dest),
			Type:      "service",
			Generated: true,
		})
		// Return install command using mkdir, cp, chmod, and echo
		return fmt.Sprintf("mkdir -p $(dirname %s) && cp %s %s && chmod 644 %s && echo \"  %s  installed %s to %s\"", dest, macro.Args[0], dest, dest, symOK, macro.Args[0], dest), nil
	}
	// Default to /etc/systemd/system directory, store just the service name
	serviceName := filepath.Base(macro.Args[0])
	dest := filepath.Join("/etc/systemd/system", serviceName)
	ctx.InstalledPaths = append(ctx.InstalledPaths, InstalledPath{
		Path:      serviceName,
		Type:      "service",
		Generated: true,
	})
	// Return install command using mkdir, cp, chmod, and echo
	return fmt.Sprintf("mkdir -p /etc/systemd/system && cp %s %s && chmod 644 %s && echo \"  %s  installed %s to %s\"", macro.Args[0], dest, dest, symOK, macro.Args[0], dest), nil
}

// executeEnableService enables a systemd service.
// On Termux and macOS, systemd is not available, so this is a no-op.
func executeEnableService(macro Macro, ctx *MacroContext) (string, error) {
	if isTermux() || isMacOS() {
		return "", nil // Termux and macOS have no systemd
	}
	if len(macro.Args) < 1 {
		return "", fmt.Errorf("ENABLE_SERVICE requires 1 argument: service_name")
	}

	service := macro.Args[0]
	// Track the service for removal (will be disabled and removed)
	// Store just the service name (basename if it's a path)
	ctx.InstalledPaths = append(ctx.InstalledPaths, InstalledPath{
		Path:      filepath.Base(service),
		Type:      "service",
		Generated: true,
	})

	return fmt.Sprintf("systemctl enable %s", service), nil
}

// executeDisableService disables a systemd service.
// On Termux and macOS, systemd is not available, so this is a no-op.
func executeDisableService(macro Macro, ctx *MacroContext) (string, error) {
	if isTermux() || isMacOS() {
		return "", nil // Termux and macOS have no systemd
	}
	if len(macro.Args) < 1 {
		return "", fmt.Errorf("DISABLE_SERVICE requires 1 argument: service_name")
	}

	service := macro.Args[0]
	return fmt.Sprintf("systemctl disable %s", service), nil
}

// executeStartService starts a systemd service.
// On Termux and macOS, systemd is not available, so this is a no-op.
func executeStartService(macro Macro, ctx *MacroContext) (string, error) {
	if isTermux() || isMacOS() {
		return "", nil // Termux and macOS have no systemd
	}
	if len(macro.Args) < 1 {
		return "", fmt.Errorf("START_SERVICE requires 1 argument: service_name")
	}

	service := macro.Args[0]
	return fmt.Sprintf("systemctl start %s", service), nil
}

// executeStopService stops a systemd service.
// On Termux and macOS, systemd is not available, so this is a no-op.
func executeStopService(macro Macro, ctx *MacroContext) (string, error) {
	if isTermux() || isMacOS() {
		return "", nil // Termux and macOS have no systemd
	}
	if len(macro.Args) < 1 {
		return "", fmt.Errorf("STOP_SERVICE requires 1 argument: service_name")
	}

	service := macro.Args[0]
	return fmt.Sprintf("systemctl stop %s", service), nil
}

// executeRestartService restarts a systemd service.
// On Termux and macOS, systemd is not available, so this is a no-op.
func executeRestartService(macro Macro, ctx *MacroContext) (string, error) {
	if isTermux() || isMacOS() {
		return "", nil // Termux and macOS have no systemd
	}
	if len(macro.Args) < 1 {
		return "", fmt.Errorf("RESTART_SERVICE requires 1 argument: service_name")
	}

	service := macro.Args[0]
	return fmt.Sprintf("systemctl restart %s", service), nil
}

// executeExtract extracts an archive
func executeExtract(macro Macro, ctx *MacroContext) (string, error) {
	if len(macro.Args) < 1 {
		return "", fmt.Errorf("EXTRACT requires 1 argument: archive_file")
	}

	archive := macro.Args[0]
	if err := validateSafePath(archive); err != nil {
		return "", fmt.Errorf("EXTRACT invalid archive path: %w", err)
	}

	// Detect archive type and extract accordingly
	var cmd string
	if strings.HasSuffix(archive, ".tar.gz") || strings.HasSuffix(archive, ".tgz") {
		cmd = fmt.Sprintf("tar -xzf %s", archive)
	} else if strings.HasSuffix(archive, ".tar.xz") || strings.HasSuffix(archive, ".txz") {
		cmd = fmt.Sprintf("tar -xJf %s", archive)
	} else if strings.HasSuffix(archive, ".tar.bz2") || strings.HasSuffix(archive, ".tbz") {
		cmd = fmt.Sprintf("tar -xjf %s", archive)
	} else if strings.HasSuffix(archive, ".zip") {
		cmd = fmt.Sprintf("unzip %s", archive)
	} else {
		cmd = fmt.Sprintf("tar -xf %s", archive)
	}

	return wrapWithFakeroot(cmd, ctx), nil
}

// executeInstallDir creates a directory with standard permissions
func executeInstallDir(macro Macro, ctx *MacroContext) (string, error) {
	if len(macro.Args) < 1 {
		return "", fmt.Errorf("INSTALL_DIR requires 1 argument: directory")
	}

	dir := macro.Args[0]
	if err := validateSafePath(dir); err != nil {
		return "", fmt.Errorf("INSTALL_DIR invalid directory path: %w", err)
	}

	ctx.InstalledPaths = append(ctx.InstalledPaths, InstalledPath{
		Path:      dir,
		Type:      "dir",
		Generated: true,
	})

	symOK := getSymOK()
	return fmt.Sprintf("mkdir -p %s && echo \"  %s  installed directory %s\"", dir, symOK, dir), nil
}

// executeSymlink creates a symbolic link
func executeSymlink(macro Macro, ctx *MacroContext) (string, error) {
	if len(macro.Args) < 2 {
		return "", fmt.Errorf("SYMLINK requires 2 arguments: target link_name")
	}

	target := macro.Args[0]
	link := macro.Args[1]

	if err := validateSafePath(target); err != nil {
		return "", fmt.Errorf("SYMLINK invalid target path: %w", err)
	}
	if err := validateSafePath(link); err != nil {
		return "", fmt.Errorf("SYMLINK invalid link path: %w", err)
	}

	ctx.InstalledPaths = append(ctx.InstalledPaths, InstalledPath{
		Path:      link,
		Type:      "symlink",
		Generated: true,
	})

	symOK := getSymOK()
	return fmt.Sprintf("ln -sf %s %s && echo \"  %s  installed symlink %s -> %s\"", target, link, symOK, link, target), nil
}

// executeCreateUser creates a system user. On Termux and macOS, useradd is not available, so this is a no-op.
// Users are not tracked for automatic removal as they may be shared between packages.
func executeCreateUser(macro Macro, ctx *MacroContext) (string, error) {
	if isTermux() || isMacOS() {
		return "", nil // Termux and macOS have no useradd
	}
	if len(macro.Args) < 1 {
		return "", fmt.Errorf("CREATE_USER requires 1 argument: username")
	}

	username := macro.Args[0]

	// Don't track users for automatic removal - they may be shared between packages
	// Users should be removed manually if needed

	return fmt.Sprintf("useradd -r -s /bin/false %s", username), nil
}

// executeRemoveUser removes a system user.
// On Termux and macOS, userdel is not available, so this is a no-op.
func executeRemoveUser(macro Macro, ctx *MacroContext) (string, error) {
	if isTermux() || isMacOS() {
		return "", nil // Termux and macOS have no userdel
	}
	if len(macro.Args) < 1 {
		return "", fmt.Errorf("REMOVE_USER requires 1 argument: username")
	}

	username := macro.Args[0]
	return fmt.Sprintf("userdel %s", username), nil
}

// executeDownload downloads a file using Go's HTTP client with progress display
func executeDownload(macro Macro, ctx *MacroContext) (string, error) {
	if len(macro.Args) < 1 {
		return "", fmt.Errorf("DOWNLOAD requires at least 1 argument: URL [file]")
	}

	url := macro.Args[0]
	if !isSafeDownloadURL(url) {
		return "", fmt.Errorf("DOWNLOAD requires HTTPS: %s", url)
	}

	file, err := resolveDownloadPath(macro.Args, ctx)
	if err != nil {
		return "", err
	}

	if err := prepareDownloadDirectory(file); err != nil {
		return "", err
	}

	return performDownload(url, file, ctx)
}

// resolveDownloadPath resolves the download path from macro arguments
func resolveDownloadPath(args []string, ctx *MacroContext) (string, error) {
	file := ""
	if len(args) > 1 {
		file = args[1]
	} else {
		file = filepath.Base(args[0])
	}

	if err := validateSafePath(file); err != nil {
		return "", fmt.Errorf("DOWNLOAD invalid file path: %w", err)
	}

	// Resolve relative to build directory
	if !filepath.IsAbs(file) {
		file = filepath.Join(ctx.BuildDir, file)
	}

	if ctx.BuildDir != "" {
		cleanBuildDir := filepath.Clean(ctx.BuildDir)
		cleanFile := filepath.Clean(file)
		rel, err := filepath.Rel(cleanBuildDir, cleanFile)
		if err != nil || strings.HasPrefix(rel, "..") || rel == ".." {
			return "", fmt.Errorf("file path %q escapes build directory %q", file, ctx.BuildDir)
		}
	}

	return file, nil
}

// prepareDownloadDirectory creates the directory for the download file
func prepareDownloadDirectory(file string) error {
	if err := os.MkdirAll(filepath.Dir(file), 0755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}
	return nil
}

// performDownload executes the HTTP download with progress tracking
func performDownload(url, file string, ctx *MacroContext) (string, error) {
	client := &http.Client{Timeout: 10 * time.Minute}
	resp, err := client.Get(url)
	if err != nil {
		return "", fmt.Errorf("failed to download %s: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("failed to download %s: HTTP %d", url, resp.StatusCode)
	}

	// Get content length for progress tracking
	contentLength := resp.ContentLength
	if contentLength <= 0 {
		return downloadSimple(resp.Body, file, ctx)
	}

	return downloadWithProgress(resp.Body, file, contentLength, ctx)
}

// requireNextSha256 returns the expected SHA-256 digest for the next download
// in the entry, consuming one entry from the sha256sums list.
//
// Security: downloads are a code-execution trust boundary. In strict mode
// (the default) a manifest that triggers a download MUST provide a matching
// sha256sums entry, otherwise we refuse to fetch anything. In free mode the
// user has opted out of guardrails, so missing digests are allowed; the
// reduced-safety notice is shown at install confirmation time.
func requireNextSha256(ctx *MacroContext, what string) (string, error) {
	if len(ctx.SHA256Sums) > 0 && ctx.SHA256Index < len(ctx.SHA256Sums) {
		expected := ctx.SHA256Sums[ctx.SHA256Index]
		ctx.SHA256Index++ // Increment index for next download
		if isValidSha256(expected) {
			return expected, nil
		}
	}
	return sha256Missing(ctx, what)
}

// sha256Missing handles a download with no usable digest. In strict mode this
// is a hard error (unverified downloads are never fetched or executed). In
// free mode the download is allowed; the reduced-safety notice is shown at
// install confirmation time (see the repo backend install preview) rather than
// per download.
func sha256Missing(ctx *MacroContext, what string) (string, error) {
	if ctx.Safety == "free" {
		return "", nil
	}
	switch {
	case len(ctx.SHA256Sums) == 0:
		return "", fmt.Errorf("%s requires a sha256sums entry in the manifest — refusing to download unverified content (strict mode)", what)
	case ctx.SHA256Index >= len(ctx.SHA256Sums):
		return "", fmt.Errorf("%s is missing a sha256sums entry (download %d of %d) — refusing to download unverified content (strict mode)", what, ctx.SHA256Index+1, len(ctx.SHA256Sums))
	default:
		return "", fmt.Errorf("invalid sha256sums entry %q for %s (want exactly 64 hex characters)", ctx.SHA256Sums[ctx.SHA256Index-1], what)
	}
}

// isValidSha256 reports whether s is a 64-character hexadecimal SHA-256 digest.
func isValidSha256(s string) bool {
	if len(s) != 64 {
		return false
	}
	for _, r := range s {
		if !(r >= '0' && r <= '9') && !(r >= 'a' && r <= 'f') && !(r >= 'A' && r <= 'F') {
			return false
		}
	}
	return true
}

// downloadSimple performs a download without progress tracking.
// Security: writes to a temporary file, verifies SHA256, then atomically
// renames to the final path so a crash or hash mismatch never leaves a
// partial or unverified file at the destination.
func downloadSimple(body io.Reader, file string, ctx *MacroContext) (string, error) {
	// Read all bytes to compute SHA256 before writing anything to disk
	bodyBytes, err := io.ReadAll(body)
	if err != nil {
		return "", fmt.Errorf("failed to read download: %w", err)
	}

	// Compute SHA256 of downloaded content
	hash := sha256.Sum256(bodyBytes)
	computedHash := fmt.Sprintf("%x", hash)

	// The manifest must declare a digest for every download.
	expectedHash, err := requireNextSha256(ctx, filepath.Base(file))
	if err != nil {
		return "", err
	}

	// Free mode may opt out of digest verification (expectedHash == "").
	if expectedHash != "" && computedHash != expectedHash {
		return "", fmt.Errorf("SHA256 mismatch for %s: expected %s, got %s", file, expectedHash, computedHash)
	}

	// Write to a temp file in the same directory (ensures rename is atomic)
	tmpPath := file + ".tmp"
	if err := os.WriteFile(tmpPath, bodyBytes, 0644); err != nil {
		return "", fmt.Errorf("failed to write file %s: %w", tmpPath, err)
	}

	// Atomically move the verified file to its final destination
	if err := os.Rename(tmpPath, file); err != nil {
		_ = os.Remove(tmpPath)
		return "", fmt.Errorf("failed to move file to %s: %w", file, err)
	}
	return "", nil
}

// downloadWithProgress performs a download with progress tracking.
// Security: streams to a temporary file, verifies SHA256, then atomically
// renames to the final path so a crash or hash mismatch never leaves a
// partial or unverified file at the destination.
func downloadWithProgress(body io.Reader, file string, contentLength int64, ctx *MacroContext) (string, error) {
	// Write to a temp file in the same directory (ensures rename is atomic)
	tmpPath := file + ".tmp"
	out, err := os.Create(tmpPath)
	if err != nil {
		return "", fmt.Errorf("failed to create file %s: %w", tmpPath, err)
	}
	defer out.Close()

	displayName := filepath.Base(file)
	progress := setupProgressDisplay(contentLength, displayName)

	// Use a tee reader to compute SHA256 while downloading
	hasher := sha256.New()
	teeReader := io.TeeReader(body, hasher)

	reader := &progressReader{
		reader: teeReader,
		total:  contentLength,
		onProgress: func(bytesRead int) {
			progress.update(bytesRead)
		},
	}

	if _, err := io.Copy(out, reader); err != nil {
		_ = os.Remove(tmpPath)
		return "", fmt.Errorf("failed to write file %s: %w", tmpPath, err)
	}

	fmt.Println()

	// Compute SHA256 of downloaded content
	computedHash := fmt.Sprintf("%x", hasher.Sum(nil))

	// The manifest must declare a digest for every download.
	expectedHash, err := requireNextSha256(ctx, displayName)
	if err != nil {
		_ = os.Remove(tmpPath)
		return "", err
	}

	// Free mode may opt out of digest verification (expectedHash == "").
	if expectedHash != "" && computedHash != expectedHash {
		_ = os.Remove(tmpPath)
		return "", fmt.Errorf("SHA256 mismatch for %s: expected %s, got %s", file, expectedHash, computedHash)
	}

	// Atomically move the verified file to its final destination
	if err := os.Rename(tmpPath, file); err != nil {
		_ = os.Remove(tmpPath)
		return "", fmt.Errorf("failed to move file to %s: %w", file, err)
	}
	return "", nil
}

// progressDisplay manages the download progress display
type progressDisplay struct {
	startTime     time.Time
	downloaded    int64
	lastUpdate    time.Time
	contentLength int64
	barWidth      int
	nameColWidth  int
	truncatedName string
}

// setupProgressDisplay initializes the progress display
func setupProgressDisplay(contentLength int64, displayName string) *progressDisplay {
	startTime := time.Now()
	termWidth := getTerminalWidth()

	const barWidth = 20
	// Total fixed width of non-name components (sizeStr 10, speedStr 12, timeStr 5, bar 22, percent 4, spacers 8) = 61 chars.
	// Leave a 2-character right margin to guarantee line length stays strictly less than termWidth and never auto-wraps.
	targetMaxLen := termWidth - 2
	nameColWidth := targetMaxLen - 61
	if nameColWidth < 3 {
		nameColWidth = 3
	}

	truncatedName := displayName
	if len(displayName) > nameColWidth {
		if nameColWidth > 3 {
			truncatedName = displayName[:nameColWidth-3] + "..."
		} else {
			truncatedName = displayName[:nameColWidth]
		}
	}

	return &progressDisplay{
		startTime:     startTime,
		lastUpdate:    startTime,
		contentLength: contentLength,
		barWidth:      barWidth,
		nameColWidth:  nameColWidth,
		truncatedName: truncatedName,
	}
}

// update updates the progress display
func (p *progressDisplay) update(bytesRead int) {
	p.downloaded += int64(bytesRead)
	now := time.Now()

	if !p.shouldUpdate(now) {
		return
	}
	p.lastUpdate = now

	stats := p.calculateStats(now)
	p.renderProgress(stats)
}

// shouldUpdate determines if the display should be updated
func (p *progressDisplay) shouldUpdate(now time.Time) bool {
	return now.Sub(p.lastUpdate) >= 50*time.Millisecond || p.downloaded >= p.contentLength
}

// progressStats holds calculated progress statistics
type progressStats struct {
	percent   float64
	speed     float64
	remaining float64
}

// calculateStats computes progress statistics
func (p *progressDisplay) calculateStats(now time.Time) progressStats {
	percent := float64(p.downloaded) / float64(p.contentLength) * 100
	elapsed := now.Sub(p.startTime).Seconds()

	var speed float64
	if elapsed > 0 {
		speed = float64(p.downloaded) / elapsed
	}

	var remaining float64
	if speed > 0 {
		remaining = float64(p.contentLength-p.downloaded) / speed
	}

	return progressStats{
		percent:   percent,
		speed:     speed,
		remaining: remaining,
	}
}

// renderProgress displays the progress bar and statistics
func (p *progressDisplay) renderProgress(stats progressStats) {
	var sizeSB strings.Builder
	formatSize(&sizeSB, p.downloaded)
	sizeStr := fmt.Sprintf("%10s", sizeSB.String())

	var speedSB strings.Builder
	formatSize(&speedSB, int64(stats.speed))
	speedStr := fmt.Sprintf("%12s", speedSB.String()+"/s")

	var timeSB strings.Builder
	formatTime(&timeSB, stats.remaining)

	percent := stats.percent
	if percent < 0 {
		percent = 0
	}
	if percent > 100 {
		percent = 100
	}

	filled := int(percent / 100 * float64(p.barWidth))
	if filled > p.barWidth {
		filled = p.barWidth
	}
	if filled < 0 {
		filled = 0
	}
	bar := strings.Repeat("#", filled) + strings.Repeat("-", p.barWidth-filled)

	// Print with carriage return and clear the rest of the line, then flush
	fmt.Fprintf(os.Stdout, "\r%-*s  %s  %s  %s [%s] %3.0f%%\033[K",
		p.nameColWidth, p.truncatedName, sizeStr, speedStr, timeSB.String(), bar, percent)
}

// progressReader wraps a reader to track download progress
type progressReader struct {
	reader     io.Reader
	total      int64
	onProgress func(int)
}

func (pr *progressReader) Read(p []byte) (n int, err error) {
	n, err = pr.reader.Read(p)
	if n > 0 && pr.onProgress != nil {
		pr.onProgress(n)
	}
	return
}

// formatSize formats bytes to a strings.Builder for efficiency
func formatSize(sb *strings.Builder, bytes int64) {
	if bytes <= 0 {
		sb.WriteString("0 B")
		return
	}

	const unit = 1024

	if bytes < unit {
		sb.WriteString(fmt.Sprintf("%d B", bytes))
		return
	}

	// Explicit thresholds for each unit
	const (
		KB = unit
		MB = unit * unit
		GB = unit * unit * unit
		TB = unit * unit * unit * unit
	)

	var value float64
	var unitName string

	if bytes < MB {
		value = float64(bytes) / KB
		unitName = "KiB"
	} else if bytes < GB {
		value = float64(bytes) / MB
		unitName = "MiB"
	} else if bytes < TB {
		value = float64(bytes) / GB
		unitName = "GiB"
	} else {
		value = float64(bytes) / TB
		unitName = "TiB"
	}

	// Round to 1 decimal place
	roundedValue := float64(int(value*10+0.5)) / 10.0

	intPart := int(roundedValue)
	decPart := int((roundedValue-float64(intPart))*10 + 0.5)

	if decPart >= 10 {
		intPart++
		decPart = 0
	}

	sb.WriteString(fmt.Sprintf("%d,%d %s", intPart, decPart, unitName))
}

// formatTime formats time to a strings.Builder for efficiency
func formatTime(sb *strings.Builder, seconds float64) {
	if seconds < 0 {
		seconds = 0
	}
	if seconds > 5999 { // cap at 99m 59s to preserve 5-char width (99:59)
		seconds = 5999
	}
	mins := int(seconds / 60)
	secs := int(seconds) % 60
	sb.WriteString(fmt.Sprintf("%02d:%02d", mins, secs))
}

// getTerminalWidth returns the current terminal width, or 80 if detection fails
func getTerminalWidth() int {
	// Try to get terminal width using ioctl
	type winsize struct {
		Row    uint16
		Col    uint16
		Xpixel uint16
		Ypixel uint16
	}

	ws := &winsize{}

	var tiocgwinsz uintptr = 0x5413 // Linux
	if runtime.GOOS == "darwin" {
		tiocgwinsz = 0x40087468 // macOS
	}

	// Try stdout first, then stdin
	fd := uintptr(syscall.Stdout)
	retCode, _, _ := syscall.Syscall(
		syscall.SYS_IOCTL,
		fd,
		tiocgwinsz,
		uintptr(unsafe.Pointer(ws)),
	)

	if int(retCode) == -1 || ws.Col == 0 {
		fd = uintptr(syscall.Stdin)
		retCode, _, _ = syscall.Syscall(
			syscall.SYS_IOCTL,
			fd,
			tiocgwinsz,
			uintptr(unsafe.Pointer(ws)),
		)
	}

	if int(retCode) == -1 || ws.Col == 0 {
		// Fallback to stty if ioctl fails
		if cmd := exec.Command("stty", "size"); cmd != nil {
			cmd.Stdin = os.Stdin
			if out, err := cmd.Output(); err == nil {
				// stty size returns "rows cols"
				parts := strings.Split(strings.TrimSpace(string(out)), " ")
				if len(parts) == 2 {
					if cols, err := fmt.Sscanf(parts[1], "%d", &ws.Col); err == nil && cols == 1 && ws.Col > 0 {
						return int(ws.Col)
					}
				}
			}
		}
		// Default fallback
		return 80
	}

	return int(ws.Col)
}

// executeBashRun downloads and executes a bash script
func executeBashRun(macro Macro, ctx *MacroContext) (string, error) {
	if len(macro.Args) < 1 {
		return "", fmt.Errorf("BASH_RUN requires at least 1 argument: script [args]")
	}

	script := macro.Args[0]
	args := ""
	if len(macro.Args) > 1 {
		args = strings.Join(macro.Args[1:], " ")
	}

	// If URL, download first using Go's HTTP client
	if strings.HasPrefix(script, "http://") || strings.HasPrefix(script, "https://") {
		if !isSafeDownloadURL(script) {
			return "", fmt.Errorf("BASH_RUN requires HTTPS: %s", script)
		}

		// The manifest must declare a digest for every downloaded script; resolve
		// it before fetching so unverifiable content is never downloaded.
		expectedHash, err := requireNextSha256(ctx, filepath.Base(script))
		if err != nil {
			return "", err
		}

		// Download with size limit and security checks via the hardened fetcher.
		bodyBytes, err := downloadScriptWithLimit(script, maxScriptSize)
		if err != nil {
			return "", fmt.Errorf("failed to download %s: %w", script, err)
		}

		// Compute SHA256 of downloaded content
		hash := sha256.Sum256(bodyBytes)
		computedHash := fmt.Sprintf("%x", hash)

		// Free mode may opt out of digest verification (expectedHash == "").
		if expectedHash != "" && computedHash != expectedHash {
			return "", fmt.Errorf("SHA256 mismatch for %s: expected %s, got %s", script, expectedHash, computedHash)
		}

		localScript := filepath.Join(ctx.BuildDir, filepath.Base(script))

		// Create directory if it doesn't exist
		if err := os.MkdirAll(filepath.Dir(localScript), 0755); err != nil {
			return "", fmt.Errorf("failed to create directory: %w", err)
		}

		// Write the file (0755 makes the script executable)
		if err := os.WriteFile(localScript, bodyBytes, 0755); err != nil {
			return "", fmt.Errorf("failed to write file %s: %w", localScript, err)
		}

		// Return command to execute the script
		if args != "" {
			cmd := fmt.Sprintf("bash %s %s", localScript, args)
			return wrapWithFakeroot(cmd, ctx), nil
		}
		cmd := fmt.Sprintf("bash %s", localScript)
		return wrapWithFakeroot(cmd, ctx), nil
	}

	// Local script
	if !filepath.IsAbs(script) {
		script = filepath.Join(ctx.BuildDir, script)
	}

	if args != "" {
		cmd := fmt.Sprintf("bash %s %s", script, args)
		return wrapWithFakeroot(cmd, ctx), nil
	}
	cmd := fmt.Sprintf("bash %s", script)
	return wrapWithFakeroot(cmd, ctx), nil
}

// executeSH executes a shell script with bash or sh
func executeSH(macro Macro, ctx *MacroContext) (string, error) {
	if len(macro.Args) < 1 {
		return "", fmt.Errorf("SH requires 1 argument: script_path")
	}

	scriptPath := macro.Args[0]

	// Use bash if available, otherwise fall back to sh
	shell := "sh"
	if _, err := exec.LookPath("bash"); err == nil {
		shell = "bash"
	}

	cmd := fmt.Sprintf("%s %s", shell, scriptPath)
	return wrapWithFakeroot(cmd, ctx), nil
}

// GenerateOwnedItems converts tracked installed paths to OwnedItems for state storage
func GenerateOwnedItems(ctx *MacroContext) []OwnedItem {
	var items []OwnedItem

	// Only include generated items (those from macros)
	for _, path := range ctx.InstalledPaths {
		if path.Generated {
			items = append(items, OwnedItem{
				Path: path.Path,
				Type: path.Type,
			})
		}
	}

	return items
}
