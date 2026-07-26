package more

import (
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
		InstalledPaths: []InstalledPath{},
		BuildDir:       "",
		DistroVersion:  distroVer,
	}
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

	macro := Macro{
		Name: strings.ToUpper(parts[0]),
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

// expandLine expands macros in a single line
func expandLine(line string, ctx *MacroContext) (string, error) {
	line = strings.TrimSpace(line)
	if line == "" || strings.HasPrefix(line, "#") {
		return "", nil
	}

	// Replace plain variable tokens in a line that has no structured macro prefix.
	replaceVars := func(s string) string {
		s = strings.ReplaceAll(s, "{ARCH}", ctx.Arch)
		s = strings.ReplaceAll(s, "{OS}", ctx.OS)
		s = strings.ReplaceAll(s, "{DISTRO}", ctx.Distro)
		s = strings.ReplaceAll(s, "{VERSION}", ctx.Version)
		s = strings.ReplaceAll(s, "{PKG_DIR}", ctx.BuildDir)
		s = strings.ReplaceAll(s, "{SERVER}", ctx.Server)
		s = strings.ReplaceAll(s, "{PKGNAME}", ctx.PackageName)
		s = strings.ReplaceAll(s, "{DISVER}", ctx.DistroVersion)
		return s
	}

	macro, remaining, isMacro := ParseMacro(line)
	if !isMacro {
		return replaceVars(line), nil
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
		arg = strings.ReplaceAll(arg, "{ARCH}", ctx.Arch)
		arg = strings.ReplaceAll(arg, "{OS}", ctx.OS)
		arg = strings.ReplaceAll(arg, "{DISTRO}", ctx.Distro)
		arg = strings.ReplaceAll(arg, "{VERSION}", ctx.Version)
		arg = strings.ReplaceAll(arg, "{PKG_DIR}", ctx.BuildDir)
		arg = strings.ReplaceAll(arg, "{SERVER}", ctx.Server)
		arg = strings.ReplaceAll(arg, "{PKGNAME}", ctx.PackageName)
		arg = strings.ReplaceAll(arg, "{DISVER}", ctx.DistroVersion)
		macro.Args[i] = arg
	}
}

// combineMacroResult combines the result of a macro execution with any remaining text on the same line, returning the raw line unchanged.
func combineMacroResult(rawLine, result, remaining string, macro Macro, ctx *MacroContext) (string, error) {
	// Macros that execute entirely in Go and should not be passed to shell
	goOnlyMacros := map[string]bool{
		"DOWNLOAD": true,
	}

	if result == "" {
		// If this is a Go-only macro, skip it entirely
		if goOnlyMacros[macro.Name] {
			if remaining == "" {
				return "", nil // Skip the entire line
			}
			// Process remaining text only
			remainingResult, err := expandLine(remaining, ctx)
			if err != nil {
				return "", err
			}
			return remainingResult, nil
		}

		// Legacy / deferred macro — pass the raw line through, but still
		// process any trailing text.
		if remaining == "" {
			return rawLine, nil
		}
		remainingResult, err := expandLine(remaining, ctx)
		if err != nil {
			return "", err
		}
		if remainingResult != "" {
			return "{" + macro.Name + " " + strings.Join(macro.Args, " ") + "} " + remainingResult, nil
		}
		return rawLine, nil
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

// executeMacro executes a single macro and returns the shell command.
func executeMacro(macro Macro, ctx *MacroContext) (string, error) {
	handler, ok := macroRegistry[macro.Name]
	if !ok {
		return "", fmt.Errorf("unknown macro: %s", macro.Name)
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

	// Return install command using mkdir, cp, and chmod
	if len(macro.Args) >= 2 {
		dest := macro.Args[len(macro.Args)-1]
		if strings.HasSuffix(dest, "/") {
			dest = dest + filepath.Base(macro.Args[0])
		}
		return fmt.Sprintf("mkdir -p $(dirname %s) && cp %s %s && chmod 755 %s", dest, macro.Args[0], dest, dest), nil
	}
	// Default to /usr/bin directory (or macOS equivalent)
	var binDir, dest string
	if isMacOS() {
		binDir = filepath.Join(macOSPrefix(), "bin")
	} else {
		binDir = filepath.Join(termuxPrefix(), "/usr/bin")
	}
	dest = filepath.Join(binDir, filepath.Base(macro.Args[0]))
	return fmt.Sprintf("mkdir -p %s && cp %s %s && chmod 755 %s", binDir, macro.Args[0], dest, dest), nil
}

// executeInstallLib installs a library to /usr/lib (or Termux equivalent) or specified directory
func executeInstallLib(macro Macro, ctx *MacroContext) (string, error) {
	if len(macro.Args) < 1 {
		return "", fmt.Errorf("INSTALL_LIB requires at least 1 argument: source [dest]")
	}
	if err := validateSafePath(macro.Args[0]); err != nil {
		return "", fmt.Errorf("INSTALL_LIB invalid source: %w", err)
	}

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
		// Return install command using mkdir, cp, and chmod
		return fmt.Sprintf("mkdir -p $(dirname %s) && cp %s %s && chmod 644 %s", dest, macro.Args[0], dest, dest), nil
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
	// Return install command using mkdir, cp, and chmod
	return fmt.Sprintf("mkdir -p %s && cp %s %s && chmod 644 %s", libDir, macro.Args[0], dest, dest), nil
}

// executeInstallConf installs a config file to /etc (or Termux equivalent) or specified directory
func executeInstallConf(macro Macro, ctx *MacroContext) (string, error) {
	if len(macro.Args) < 1 {
		return "", fmt.Errorf("INSTALL_CONF requires at least 1 argument: source [dest]")
	}
	if err := validateSafePath(macro.Args[0]); err != nil {
		return "", fmt.Errorf("INSTALL_CONF invalid source: %w", err)
	}

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
		// Return install command using mkdir, cp, and chmod
		return fmt.Sprintf("mkdir -p $(dirname %s) && cp %s %s && chmod 644 %s", dest, macro.Args[0], dest, dest), nil
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
	// Return install command using mkdir, cp, and chmod
	return fmt.Sprintf("mkdir -p %s && cp %s %s && chmod 644 %s", etcDir, macro.Args[0], dest, dest), nil
}

// executeInstallMan installs a man page to /usr/share/man (or Termux equivalent) or specified directory
func executeInstallMan(macro Macro, ctx *MacroContext) (string, error) {
	if len(macro.Args) < 1 {
		return "", fmt.Errorf("INSTALL_MAN requires at least 1 argument: source [dest]")
	}
	if err := validateSafePath(macro.Args[0]); err != nil {
		return "", fmt.Errorf("INSTALL_MAN invalid source: %w", err)
	}

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
		return fmt.Sprintf("mkdir -p $(dirname %s) && cp %s %s && chmod 644 %s && gzip -f %s", uncompressedDest, macro.Args[0], uncompressedDest, uncompressedDest, uncompressedDest), nil
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
	// Return install command using mkdir, cp, chmod, and gzip
	// We need to copy to the destination without .gz first, then gzip it
	uncompressedDest := strings.TrimSuffix(dest, ".gz")
	return fmt.Sprintf("mkdir -p %s && cp %s %s && chmod 644 %s && gzip -f %s", manDir, macro.Args[0], uncompressedDest, uncompressedDest, uncompressedDest), nil
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
		// Return install command using mkdir, cp, and chmod
		return fmt.Sprintf("mkdir -p $(dirname %s) && cp %s %s && chmod 644 %s", dest, macro.Args[0], dest, dest), nil
	}
	// Default to /etc/systemd/system directory, store just the service name
	serviceName := filepath.Base(macro.Args[0])
	dest := filepath.Join("/etc/systemd/system", serviceName)
	ctx.InstalledPaths = append(ctx.InstalledPaths, InstalledPath{
		Path:      serviceName,
		Type:      "service",
		Generated: true,
	})
	// Return install command using mkdir, cp, and chmod
	return fmt.Sprintf("mkdir -p /etc/systemd/system && cp %s %s && chmod 644 %s", macro.Args[0], dest, dest), nil
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

	return fmt.Sprintf("mkdir -p %s", dir), nil
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

	return fmt.Sprintf("ln -sf %s %s", target, link), nil
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
	if !isAllowedURL(url) {
		return "", fmt.Errorf("disallowed URL host/scheme for DOWNLOAD: %s", url)
	}
	file := ""
	if len(macro.Args) > 1 {
		file = macro.Args[1]
	} else {
		file = filepath.Base(url)
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

	// Create directory if it doesn't exist
	if err := os.MkdirAll(filepath.Dir(file), 0755); err != nil {
		return "", fmt.Errorf("failed to create directory: %w", err)
	}

	// Download the file
	resp, err := http.Get(url)
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
		// If content length is unknown, fall back to simple download
		out, err := os.Create(file)
		if err != nil {
			return "", fmt.Errorf("failed to create file %s: %w", file, err)
		}
		defer out.Close()

		if _, err := io.Copy(out, resp.Body); err != nil {
			return "", fmt.Errorf("failed to write file %s: %w", file, err)
		}
		return "", nil
	}

	// Create the file
	out, err := os.Create(file)
	if err != nil {
		return "", fmt.Errorf("failed to create file %s: %w", file, err)
	}
	defer out.Close()

	// Progress tracking with display
	displayName := filepath.Base(file)
	startTime := time.Now()
	var downloaded int64
	lastUpdate := startTime
	termWidth := getTerminalWidth()

	// Pre-calculate fixed layout elements
	barWidth := 20

	// Pre-calculate maximum filename length (assuming worst case for right side)
	// Right side format: "XXX,X XB   XXX,X XB/s 00:00 [####################]  100%"
	maxRightSideLen := 50 // Estimated maximum right side length
	maxFilenameLen := termWidth - maxRightSideLen - 1
	if maxFilenameLen < 3 {
		maxFilenameLen = 3
	}

	// Truncate filename once
	truncatedName := displayName
	if len(displayName) > maxFilenameLen {
		truncatedName = displayName[:maxFilenameLen-3] + "..."
	}

	// Create a progress reader
	reader := &progressReader{
		reader: resp.Body,
		total:  contentLength,
		onProgress: func(bytesRead int) {
			downloaded += int64(bytesRead)
			now := time.Now()

			// Update display every 100ms
			if now.Sub(lastUpdate) < 100*time.Millisecond && downloaded < contentLength {
				return
			}
			lastUpdate = now

			// Calculate progress
			percent := float64(downloaded) / float64(contentLength) * 100
			elapsed := now.Sub(startTime).Seconds()

			// Calculate speed
			var speed float64
			if elapsed > 0 {
				speed = float64(downloaded) / elapsed
			}

			// Calculate remaining time
			var remaining float64
			if speed > 0 {
				remaining = float64(contentLength-downloaded) / speed
			}

			// Format sizes using strings.Builder for efficiency
			var sb strings.Builder

			// Add downloaded size
			formatSize(&sb, downloaded)
			sb.WriteString("   ")

			// Add speed
			formatSize(&sb, int64(speed))
			sb.WriteString("/s ")

			// Add time
			formatTime(&sb, remaining)

			// Build progress bar
			filled := int(percent / 100 * float64(barWidth))
			sb.WriteString(" [")
			for i := 0; i < filled; i++ {
				sb.WriteByte('#')
			}
			for i := filled; i < barWidth; i++ {
				sb.WriteByte('-')
			}
			sb.WriteString("]  ")

			// Add percentage
			sb.WriteString(fmt.Sprintf("%.0f%%", percent))

			// Display progress with filename on left, stats on right
			fmt.Printf("\r%s %-*s", truncatedName, termWidth-len(truncatedName)-1, sb.String())
		},
	}

	// Copy with progress tracking
	if _, err := io.Copy(out, reader); err != nil {
		return "", fmt.Errorf("failed to write file %s: %w", file, err)
	}

	// Print final newline
	fmt.Println()

	// Return empty string since download is done in Go
	return "", nil
}

// progressReader wraps a reader to track download progress
type progressReader struct {
	reader     io.Reader
	total      int64
	read       int64
	onProgress func(int)
}

func (pr *progressReader) Read(p []byte) (n int, err error) {
	n, err = pr.reader.Read(p)
	if n > 0 {
		pr.read += int64(n)
		if pr.onProgress != nil {
			pr.onProgress(n)
		}
	}
	return
}

// formatSize formats bytes to a strings.Builder for efficiency
func formatSize(sb *strings.Builder, bytes int64) {
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

	// Define TIOCGWINSZ for different platforms
	const TIOCGWINSZ = 0x5413 // Linux

	retCode, _, _ := syscall.Syscall(
		syscall.SYS_IOCTL,
		uintptr(syscall.Stdin),
		uintptr(TIOCGWINSZ),
		uintptr(unsafe.Pointer(ws)),
	)

	if int(retCode) == -1 {
		// Fallback to stty if ioctl fails
		if cmd := exec.Command("stty", "size"); cmd != nil {
			cmd.Stdin = os.Stdin
			if out, err := cmd.Output(); err == nil {
				// stty size returns "rows cols"
				parts := strings.Split(strings.TrimSpace(string(out)), " ")
				if len(parts) == 2 {
					if cols, err := fmt.Sscanf(parts[1], "%d", &ws.Col); err == nil && cols == 1 {
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
		if !isAllowedURL(script) {
			return "", fmt.Errorf("disallowed URL host/scheme for BASH_RUN: %s", script)
		}
		localScript := filepath.Join(ctx.BuildDir, filepath.Base(script))

		// Create directory if it doesn't exist
		if err := os.MkdirAll(filepath.Dir(localScript), 0755); err != nil {
			return "", fmt.Errorf("failed to create directory: %w", err)
		}

		// Download the script
		resp, err := http.Get(script)
		if err != nil {
			return "", fmt.Errorf("failed to download %s: %w", script, err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			return "", fmt.Errorf("failed to download %s: HTTP %d", script, resp.StatusCode)
		}

		// Create the file
		out, err := os.Create(localScript)
		if err != nil {
			return "", fmt.Errorf("failed to create file %s: %w", localScript, err)
		}
		defer out.Close()

		// Copy the response body to the file
		if _, err := io.Copy(out, resp.Body); err != nil {
			return "", fmt.Errorf("failed to write file %s: %w", localScript, err)
		}

		// Make the script executable
		if err := os.Chmod(localScript, 0755); err != nil {
			return "", fmt.Errorf("failed to make script executable: %w", err)
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

// GenerateUninstallCommands generates uninstall commands based on tracked installed paths
func GenerateUninstallCommands(ctx *MacroContext) []string {
	var commands []string

	// Process in reverse order for proper cleanup (children before parents)
	for i := len(ctx.InstalledPaths) - 1; i >= 0; i-- {
		path := ctx.InstalledPaths[i]
		if !path.Generated {
			continue
		}

		switch path.Type {
		case "file":
			commands = append(commands, fmt.Sprintf("rm -f %s", path.Path))
		case "dir":
			commands = append(commands, fmt.Sprintf("rmdir %s 2>/dev/null || true", path.Path))
		case "symlink":
			commands = append(commands, fmt.Sprintf("rm -f %s", path.Path))
		case "service":
			// On Termux and macOS, systemd is not available, so skip service commands
			if !isTermux() && !isMacOS() {
				commands = append(commands, fmt.Sprintf("systemctl disable %s 2>/dev/null || true", path.Path))
				commands = append(commands, fmt.Sprintf("systemctl stop %s 2>/dev/null || true", path.Path))
			}
		case "user":
			commands = append(commands, fmt.Sprintf("userdel %s 2>/dev/null || true", path.Path))
		}
	}

	return commands
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

// ExecuteDeferredOps is no longer needed - macros now return shell commands directly
func ExecuteDeferredOps(ctx *MacroContext) error {
	return nil
}
