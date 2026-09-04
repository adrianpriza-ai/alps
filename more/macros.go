package more

import (
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/adrianpriza-ai/alps/platform"
)

// getSymOK returns the current style's success symbol.
func getSymOK() string {
	return currentStyle().SymOK
}

// validateSafePath ensures target paths do not contain path traversal (..).
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

// InstalledPath tracks a file/directory installed via macros for automatic uninstall.
type InstalledPath struct {
	Path      string
	Type      string // "file", "dir", "symlink", "service"
	Generated bool   // true if this was auto-generated for uninstall
}

// DeferredOperation represents a file operation to be executed after commands complete.
type DeferredOperation struct {
	Type    string // "install", "install_dir", "symlink", "enable_service", "start_service", "create_user", etc.
	Src     string
	Dst     string
	Mode    string
	CmdArgs []string // for command-based deferred ops (service control, user management)
}

// Macro represents an installation macro.
type Macro struct {
	Name string
	Args []string
}

// MacroContext holds context for macro expansion and execution.
type MacroContext struct {
	PackageName    string
	Version        string
	Server         string
	Arch           string
	OS             string                 // Operating system (linux, darwin, etc.)
	Distro         string                 // Linux distribution ID (ubuntu, debian, etc.)
	Safety         string                 // "strict" or "free"
	SHA256Sums     []string               // SHA-256 checksums for downloads
	SHA256Index    int                    // Current index for SHA256 sum assignment
	Op             platform.OperationType // current operation (install/upgrade/remove/purge)
	InstalledPaths []InstalledPath        // Track installed files for auto-uninstall (internal)
	DeferredOps    []DeferredOperation    // Deferred file operations
	BuildDir       string                 // Build directory for source files
	DistroVersion  string
}

// NewMacroContext creates a new macro execution context.
func NewMacroContext(e *Entry, server string) *MacroContext {
	distroVer := detectDistroVersion()
	distro, _ := detectDistro()
	os := runtime.GOOS

	if e == nil {
		return &MacroContext{
			PackageName:    "",
			Version:        "",
			Server:         server,
			Arch:           platform.NormalizeArch(runtime.GOARCH),
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
		Arch:           platform.NormalizeArch(runtime.GOARCH),
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

// ExpandMacros expands macros in command lines based on safety mode.
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

// expandLine expands macros in a single line.
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

// isInstallOnlyMacro returns true for macros that install or create files/services/users.
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

// isRemovalOp reports whether an operation tears a package down.
func isRemovalOp(op platform.OperationType) bool {
	return op == platform.OperationRemove || op == platform.OperationPurge
}

// skipMacroForOp reports whether a macro must not run for the given operation:
// install/creation macros are dropped during remove and purge, everything else
// runs. Manifest categorization, after_env execution and macro dispatch all
// ask this one function, so the three filters cannot disagree.
func skipMacroForOp(name string, op platform.OperationType) bool {
	return isRemovalOp(op) && isInstallOnlyMacro(name)
}

// executeMacro executes a single macro and returns the shell command.
// Unknown macro names are silently ignored (return "", nil) so that typos
// or unrecognised macros never reach the shell.
func executeMacro(macro Macro, ctx *MacroContext) (string, error) {
	if ctx != nil && skipMacroForOp(macro.Name, ctx.Op) {
		return "", nil // Skip install/creation macros during remove or purge
	}
	handler, ok := macroRegistry[macro.Name]
	if !ok {
		return "", nil // Unknown macros are silently skipped
	}
	return handler(macro, ctx)
}

// executeBashRun downloads and executes a bash script.
func executeBashRun(macro Macro, ctx *MacroContext) (string, error) {
	if len(macro.Args) < 1 {
		return "", fmt.Errorf("BASH_RUN requires at least 1 argument: script [args]")
	}

	script := macro.Args[0]
	// Quote each trailing argument so it reaches the script as one literal
	// word — these come from third-party ALPSMORE files and could otherwise
	// smuggle shell metacharacters into the executed command line.
	quotedArgs := make([]string, 0, len(macro.Args)-1)
	for _, a := range macro.Args[1:] {
		quotedArgs = append(quotedArgs, shellQuote(a))
	}
	args := strings.Join(quotedArgs, " ")

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

		// Download with size limit and security checks via the shared capped fetcher.
		bodyBytes, err := fetchBytes(script, scriptDownloadTimeout, maxScriptSize, func(u string) error {
			if !isSafeDownloadURL(u) {
				return fmt.Errorf("script download requires HTTPS: %s", u)
			}
			return nil
		})
		if err != nil {
			return "", fmt.Errorf("failed to download %s: %w", script, err)
		}

		// Compute SHA256 of downloaded content
		hash := sha256Sum(bodyBytes)
		computedHash := hash

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

		// Return command to execute the script (path quoted).
		if args != "" {
			cmd := fmt.Sprintf("bash %s %s", shellQuote(localScript), args)
			return wrapWithFakeroot(cmd, ctx), nil
		}
		cmd := fmt.Sprintf("bash %s", shellQuote(localScript))
		return wrapWithFakeroot(cmd, ctx), nil
	}

	// Local script
	if !filepath.IsAbs(script) {
		script = filepath.Join(ctx.BuildDir, script)
	}

	if args != "" {
		cmd := fmt.Sprintf("bash %s %s", shellQuote(script), args)
		return wrapWithFakeroot(cmd, ctx), nil
	}
	cmd := fmt.Sprintf("bash %s", shellQuote(script))
	return wrapWithFakeroot(cmd, ctx), nil
}

// sha256Sum computes the hex-encoded SHA-256 digest of data.
func sha256Sum(data []byte) string {
	h := sha256.New()
	h.Write(data)
	return fmt.Sprintf("%x", h.Sum(nil))
}

// executeSH executes a shell script with bash or sh.
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

	cmd := fmt.Sprintf("%s %s", shell, shellQuote(scriptPath))
	return wrapWithFakeroot(cmd, ctx), nil
}

// GenerateOwnedItems converts tracked installed paths to OwnedItems for state storage.
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

// --- Service macros (ENABLE/DISABLE/START/STOP/RESTART_SERVICE) ---

// executeEnableService enables a systemd service.
// On Termux and macOS, systemd is not available, so this is a no-op.
func executeEnableService(macro Macro, ctx *MacroContext) (string, error) {
	if platform.IsTermux() || platform.IsMacOS() {
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

	return fmt.Sprintf("systemctl enable %s", shellQuote(service)), nil
}

// executeDisableService disables a systemd service.
// On Termux and macOS, systemd is not available, so this is a no-op.
func executeDisableService(macro Macro, ctx *MacroContext) (string, error) {
	if platform.IsTermux() || platform.IsMacOS() {
		return "", nil // Termux and macOS have no systemd
	}
	if len(macro.Args) < 1 {
		return "", fmt.Errorf("DISABLE_SERVICE requires 1 argument: service_name")
	}

	service := macro.Args[0]
	return fmt.Sprintf("systemctl disable %s", shellQuote(service)), nil
}

// executeStartService starts a systemd service.
// On Termux and macOS, systemd is not available, so this is a no-op.
func executeStartService(macro Macro, ctx *MacroContext) (string, error) {
	if platform.IsTermux() || platform.IsMacOS() {
		return "", nil // Termux and macOS have no systemd
	}
	if len(macro.Args) < 1 {
		return "", fmt.Errorf("START_SERVICE requires 1 argument: service_name")
	}

	service := macro.Args[0]
	return fmt.Sprintf("systemctl start %s", shellQuote(service)), nil
}

// executeStopService stops a systemd service.
// On Termux and macOS, systemd is not available, so this is a no-op.
func executeStopService(macro Macro, ctx *MacroContext) (string, error) {
	if platform.IsTermux() || platform.IsMacOS() {
		return "", nil // Termux and macOS have no systemd
	}
	if len(macro.Args) < 1 {
		return "", fmt.Errorf("STOP_SERVICE requires 1 argument: service_name")
	}

	service := macro.Args[0]
	return fmt.Sprintf("systemctl stop %s", shellQuote(service)), nil
}

// executeRestartService restarts a systemd service.
// On Termux and macOS, systemd is not available, so this is a no-op.
func executeRestartService(macro Macro, ctx *MacroContext) (string, error) {
	if platform.IsTermux() || platform.IsMacOS() {
		return "", nil // Termux and macOS have no systemd
	}
	if len(macro.Args) < 1 {
		return "", fmt.Errorf("RESTART_SERVICE requires 1 argument: service_name")
	}

	service := macro.Args[0]
	return fmt.Sprintf("systemctl restart %s", shellQuote(service)), nil
}

// --- User macros (CREATE_USER / REMOVE_USER) ---

// executeCreateUser creates a system user. On Termux and macOS, useradd is not available, so this is a no-op.
// Users are not tracked for automatic removal as they may be shared between packages.
func executeCreateUser(macro Macro, ctx *MacroContext) (string, error) {
	if platform.IsTermux() || platform.IsMacOS() {
		return "", nil // Termux and macOS have no useradd
	}
	if len(macro.Args) < 1 {
		return "", fmt.Errorf("CREATE_USER requires 1 argument: username")
	}

	username := macro.Args[0]

	// Don't track users for automatic removal - they may be shared between packages
	// Users should be removed manually if needed

	return fmt.Sprintf("useradd -r -s /bin/false %s", shellQuote(username)), nil
}

// executeRemoveUser removes a system user.
// On Termux and macOS, userdel is not available, so this is a no-op.
func executeRemoveUser(macro Macro, ctx *MacroContext) (string, error) {
	if platform.IsTermux() || platform.IsMacOS() {
		return "", nil // Termux and macOS have no userdel
	}
	if len(macro.Args) < 1 {
		return "", fmt.Errorf("REMOVE_USER requires 1 argument: username")
	}

	username := macro.Args[0]
	return fmt.Sprintf("userdel %s", shellQuote(username)), nil
}
