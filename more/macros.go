package more

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

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

// InstalledPath tracks a file/directory installed via macros for automatic uninstall
type InstalledPath struct {
	Path      string
	Type      string // "file", "dir", "symlink", "service", "user"
	Generated bool   // true if this was auto-generated for uninstall
}

// DeferredOperation represents a file operation to be executed after commands complete
type DeferredOperation struct {
	Type string // "install", "chmod", "chown", etc.
	Src  string
	Dst  string
	Mode string
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
	Safety         string              // "strict" or "free"
	InstalledPaths []InstalledPath     // Track installed files for auto-uninstall (internal)
	DeferredOps    []DeferredOperation // Deferred file operations
	BuildDir       string              // Build directory for source files
}

// NewMacroContext creates a new macro execution context
func NewMacroContext(e *Entry, server string) *MacroContext {
	if e == nil {
		return &MacroContext{
			PackageName:    "",
			Version:        "",
			Server:         server,
			Arch:           normalizeArch(runtime.GOARCH),
			Safety:         "",
			InstalledPaths: []InstalledPath{},
		}
	}
	return &MacroContext{
		PackageName:    e.Name,
		Version:        e.Version,
		Server:         server,
		Arch:           normalizeArch(runtime.GOARCH),
		Safety:         e.Safety,
		InstalledPaths: []InstalledPath{},
	}
}

// ParseMacro parses a macro from a command line
// Returns (macro, remainingLine, isMacro)
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

	// In strict mode, process structured macros
	if ctx.Safety == "strict" {
		macro, remaining, isMacro := ParseMacro(line)
		if !isMacro {
			// In strict mode, non-macro lines are passed through but may be validated
			// Replace standard macros first
			line = strings.ReplaceAll(line, "{ARCH}", ctx.Arch)
			line = strings.ReplaceAll(line, "{VERSION}", ctx.Version)
			line = strings.ReplaceAll(line, "{SERVER}", ctx.Server)
			line = strings.ReplaceAll(line, "{PKGNAME}", ctx.PackageName)
			return line, nil
		}

		// Expand macro args with variables before executing
		for i, arg := range macro.Args {
			arg = strings.ReplaceAll(arg, "{ARCH}", ctx.Arch)
			arg = strings.ReplaceAll(arg, "{VERSION}", ctx.Version)
			arg = strings.ReplaceAll(arg, "{SERVER}", ctx.Server)
			arg = strings.ReplaceAll(arg, "{PKGNAME}", ctx.PackageName)
			macro.Args[i] = arg
		}

		result, err := executeMacro(macro, ctx)
		if err != nil {
			return "", fmt.Errorf("failed to execute macro %s: %w", macro.Name, err)
		}

		// If result is empty, this is a legacy macro - pass through unchanged
		if result == "" {
			// If there's remaining text, process it
			if remaining != "" {
				remainingResult, err := expandLine(remaining, ctx)
				if err != nil {
					return "", err
				}
				if remainingResult != "" {
					return "{" + macro.Name + " " + strings.Join(macro.Args, " ") + "} " + remainingResult, nil
				}
			}
			return line, nil
		}

		// If there's remaining text after the macro, process it too
		if remaining != "" {
			remainingResult, err := expandLine(remaining, ctx)
			if err != nil {
				return "", err
			}
			if remainingResult != "" {
				result = result + " && " + remainingResult
			}
		}

		return result, nil
	}

	// In free mode, macros are expanded but lines are otherwise left as-is
	macro, remaining, isMacro := ParseMacro(line)
	if isMacro {
		// Expand macro args with variables before executing
		for i, arg := range macro.Args {
			arg = strings.ReplaceAll(arg, "{ARCH}", ctx.Arch)
			arg = strings.ReplaceAll(arg, "{VERSION}", ctx.Version)
			arg = strings.ReplaceAll(arg, "{SERVER}", ctx.Server)
			arg = strings.ReplaceAll(arg, "{PKGNAME}", ctx.PackageName)
			macro.Args[i] = arg
		}

		result, err := executeMacro(macro, ctx)
		if err != nil {
			return "", fmt.Errorf("failed to execute macro %s: %w", macro.Name, err)
		}
		// If result is empty, this is a legacy macro - pass through unchanged
		if result == "" {
			// If there's remaining text, process it
			if remaining != "" {
				remainingResult, err := expandLine(remaining, ctx)
				if err != nil {
					return "", err
				}
				if remainingResult != "" {
					return "{" + macro.Name + " " + strings.Join(macro.Args, " ") + "} " + remainingResult, nil
				}
			}
			return line, nil
		}
		if remaining != "" {
			remainingResult, err := expandLine(remaining, ctx)
			if err != nil {
				return "", err
			}
			if remainingResult != "" {
				result = result + " && " + remainingResult
			}
		}
		return result, nil
	}

	// Replace standard macros for non-macro lines in free mode
	line = strings.ReplaceAll(line, "{ARCH}", ctx.Arch)
	line = strings.ReplaceAll(line, "{VERSION}", ctx.Version)
	line = strings.ReplaceAll(line, "{SERVER}", ctx.Server)
	line = strings.ReplaceAll(line, "{PKGNAME}", ctx.PackageName)

	return line, nil
}

// executeMacro executes a single macro and returns the shell command
func executeMacro(macro Macro, ctx *MacroContext) (string, error) {
	switch macro.Name {
	case "INSTALL_BIN":
		return executeInstallBin(macro, ctx)
	case "INSTALL_LIB":
		return executeInstallLib(macro, ctx)
	case "INSTALL_CONF":
		return executeInstallConf(macro, ctx)
	case "INSTALL_MAN":
		return executeInstallMan(macro, ctx)
	case "INSTALL_SERVICE":
		return executeInstallService(macro, ctx)
	case "ENABLE_SERVICE":
		return executeEnableService(macro, ctx)
	case "DISABLE_SERVICE":
		return executeDisableService(macro, ctx)
	case "START_SERVICE":
		return executeStartService(macro, ctx)
	case "STOP_SERVICE":
		return executeStopService(macro, ctx)
	case "RESTART_SERVICE":
		return executeRestartService(macro, ctx)
	case "EXTRACT":
		return executeExtract(macro, ctx)
	case "INSTALL_DIR":
		return executeInstallDir(macro, ctx)
	case "SYMLINK":
		return executeSymlink(macro, ctx)
	case "CREATE_USER":
		return executeCreateUser(macro, ctx)
	case "REMOVE_USER":
		return executeRemoveUser(macro, ctx)
	// Legacy macros - return empty string to let the existing system handle them
	case "DOWNLOAD", "BASH_RUN", "CURL_RUN":
		return "", nil
	default:
		return "", fmt.Errorf("unknown macro: %s", macro.Name)
	}
}

// executeInstallBin installs a binary to /usr/bin (or Termux equivalent) or specified directory (deferred)
func executeInstallBin(macro Macro, ctx *MacroContext) (string, error) {
	if len(macro.Args) < 1 {
		return "", fmt.Errorf("INSTALL_BIN requires at least 1 argument: source [dest]")
	}

	src := macro.Args[0]
	dest := filepath.Join(termuxPrefix(), "/usr/bin") + "/"
	if len(macro.Args) > 1 {
		dest = macro.Args[1]
	}

	// Ensure destination ends with /
	if !strings.HasSuffix(dest, "/") {
		dest = dest + "/"
	}

	// Resolve source path relative to build directory if not absolute
	if !filepath.IsAbs(src) {
		src = filepath.Join(ctx.BuildDir, src)
	}

	finalDst := dest + filepath.Base(src)

	// Track for uninstall
	ctx.InstalledPaths = append(ctx.InstalledPaths, InstalledPath{
		Path:      finalDst,
		Type:      "file",
		Generated: true,
	})

	// Defer the actual installation
	ctx.DeferredOps = append(ctx.DeferredOps, DeferredOperation{
		Type: "install",
		Src:  src,
		Dst:  finalDst,
		Mode: "755",
	})

	return "", nil // Return empty to indicate deferred operation
}

// executeInstallLib installs a library to /usr/lib (or Termux equivalent) or specified directory (deferred)
func executeInstallLib(macro Macro, ctx *MacroContext) (string, error) {
	if len(macro.Args) < 1 {
		return "", fmt.Errorf("INSTALL_LIB requires at least 1 argument: source [dest]")
	}

	src := macro.Args[0]
	dest := filepath.Join(termuxPrefix(), "/usr/lib") + "/"
	if len(macro.Args) > 1 {
		dest = macro.Args[1]
	}

	if !strings.HasSuffix(dest, "/") {
		dest = dest + "/"
	}

	if !filepath.IsAbs(src) {
		src = filepath.Join(ctx.BuildDir, src)
	}

	finalDst := dest + filepath.Base(src)

	ctx.InstalledPaths = append(ctx.InstalledPaths, InstalledPath{
		Path:      finalDst,
		Type:      "file",
		Generated: true,
	})

	ctx.DeferredOps = append(ctx.DeferredOps, DeferredOperation{
		Type: "install",
		Src:  src,
		Dst:  finalDst,
		Mode: "644",
	})

	return "", nil
}

// executeInstallConf installs a config file to /etc (or Termux equivalent) or specified directory (deferred)
func executeInstallConf(macro Macro, ctx *MacroContext) (string, error) {
	if len(macro.Args) < 1 {
		return "", fmt.Errorf("INSTALL_CONF requires at least 1 argument: source [dest]")
	}

	src := macro.Args[0]
	dest := filepath.Join(termuxPrefix(), "/etc") + "/"
	if len(macro.Args) > 1 {
		dest = macro.Args[1]
	}

	if !strings.HasSuffix(dest, "/") {
		dest = dest + "/"
	}

	if !filepath.IsAbs(src) {
		src = filepath.Join(ctx.BuildDir, src)
	}

	finalDst := dest + filepath.Base(src)

	ctx.InstalledPaths = append(ctx.InstalledPaths, InstalledPath{
		Path:      finalDst,
		Type:      "file",
		Generated: true,
	})

	ctx.DeferredOps = append(ctx.DeferredOps, DeferredOperation{
		Type: "install",
		Src:  src,
		Dst:  finalDst,
		Mode: "644",
	})

	return "", nil
}

// executeInstallMan installs a man page to /usr/share/man (or Termux equivalent) or specified directory (deferred)
func executeInstallMan(macro Macro, ctx *MacroContext) (string, error) {
	if len(macro.Args) < 1 {
		return "", fmt.Errorf("INSTALL_MAN requires at least 1 argument: source [dest]")
	}

	src := macro.Args[0]
	dest := filepath.Join(termuxPrefix(), "/usr/share/man/man1") + "/"
	if len(macro.Args) > 1 {
		dest = macro.Args[1]
	}

	if !strings.HasSuffix(dest, "/") {
		dest = dest + "/"
	}

	if !filepath.IsAbs(src) {
		src = filepath.Join(ctx.BuildDir, src)
	}

	finalDst := dest + filepath.Base(src)

	ctx.InstalledPaths = append(ctx.InstalledPaths, InstalledPath{
		Path:      finalDst + ".gz",
		Type:      "file",
		Generated: true,
	})

	ctx.DeferredOps = append(ctx.DeferredOps, DeferredOperation{
		Type: "install_man",
		Src:  src,
		Dst:  finalDst,
		Mode: "644",
	})

	return "", nil
}

// executeInstallService installs a systemd service file (deferred).
// On Termux, systemd is not available, so this is a no-op.
func executeInstallService(macro Macro, ctx *MacroContext) (string, error) {
	if isTermux() {
		return "", nil // Termux has no systemd
	}
	if len(macro.Args) < 1 {
		return "", fmt.Errorf("INSTALL_SERVICE requires at least 1 argument: source [dest]")
	}

	src := macro.Args[0]
	dest := "/etc/systemd/system/"
	if len(macro.Args) > 1 {
		dest = macro.Args[1]
	}

	if !strings.HasSuffix(dest, "/") {
		dest = dest + "/"
	}

	if !filepath.IsAbs(src) {
		src = filepath.Join(ctx.BuildDir, src)
	}

	serviceName := filepath.Base(src)
	finalDst := dest + serviceName

	ctx.InstalledPaths = append(ctx.InstalledPaths, InstalledPath{
		Path:      serviceName,
		Type:      "service",
		Generated: true,
	})

	ctx.DeferredOps = append(ctx.DeferredOps, DeferredOperation{
		Type: "install",
		Src:  src,
		Dst:  finalDst,
		Mode: "644",
	})

	return "", nil
}

// executeEnableService enables a systemd service.
// On Termux, systemd is not available, so this is a no-op.
func executeEnableService(macro Macro, ctx *MacroContext) (string, error) {
	if isTermux() {
		return "", nil // Termux has no systemd
	}
	if len(macro.Args) < 1 {
		return "", fmt.Errorf("ENABLE_SERVICE requires 1 argument: service_name")
	}

	service := macro.Args[0]
	ctx.InstalledPaths = append(ctx.InstalledPaths, InstalledPath{
		Path:      service,
		Type:      "service",
		Generated: true,
	})

	return fmt.Sprintf("systemctl enable %s", service), nil
}

// executeDisableService disables a systemd service.
// On Termux, systemd is not available, so this is a no-op.
func executeDisableService(macro Macro, ctx *MacroContext) (string, error) {
	if isTermux() {
		return "", nil // Termux has no systemd
	}
	if len(macro.Args) < 1 {
		return "", fmt.Errorf("DISABLE_SERVICE requires 1 argument: service_name")
	}

	return fmt.Sprintf("systemctl disable %s", macro.Args[0]), nil
}

// executeStartService starts a systemd service.
// On Termux, systemd is not available, so this is a no-op.
func executeStartService(macro Macro, ctx *MacroContext) (string, error) {
	if isTermux() {
		return "", nil // Termux has no systemd
	}
	if len(macro.Args) < 1 {
		return "", fmt.Errorf("START_SERVICE requires 1 argument: service_name")
	}

	return fmt.Sprintf("systemctl start %s", macro.Args[0]), nil
}

// executeStopService stops a systemd service.
// On Termux, systemd is not available, so this is a no-op.
func executeStopService(macro Macro, ctx *MacroContext) (string, error) {
	if isTermux() {
		return "", nil // Termux has no systemd
	}
	if len(macro.Args) < 1 {
		return "", fmt.Errorf("STOP_SERVICE requires 1 argument: service_name")
	}

	return fmt.Sprintf("systemctl stop %s", macro.Args[0]), nil
}

// executeRestartService restarts a systemd service.
// On Termux, systemd is not available, so this is a no-op.
func executeRestartService(macro Macro, ctx *MacroContext) (string, error) {
	if isTermux() {
		return "", nil // Termux has no systemd
	}
	if len(macro.Args) < 1 {
		return "", fmt.Errorf("RESTART_SERVICE requires 1 argument: service_name")
	}

	return fmt.Sprintf("systemctl restart %s", macro.Args[0]), nil
}

// executeExtract extracts an archive
func executeExtract(macro Macro, ctx *MacroContext) (string, error) {
	if len(macro.Args) < 1 {
		return "", fmt.Errorf("EXTRACT requires 1 argument: archive_file")
	}

	archive := macro.Args[0]

	// Detect archive type and extract accordingly
	if strings.HasSuffix(archive, ".tar.gz") || strings.HasSuffix(archive, ".tgz") {
		return fmt.Sprintf("tar -xzf %s", archive), nil
	} else if strings.HasSuffix(archive, ".tar.xz") || strings.HasSuffix(archive, ".txz") {
		return fmt.Sprintf("tar -xJf %s", archive), nil
	} else if strings.HasSuffix(archive, ".tar.bz2") || strings.HasSuffix(archive, ".tbz") {
		return fmt.Sprintf("tar -xjf %s", archive), nil
	} else if strings.HasSuffix(archive, ".zip") {
		return fmt.Sprintf("unzip %s", archive), nil
	} else {
		return fmt.Sprintf("tar -xf %s", archive), nil
	}
}

// executeInstallDir creates a directory with standard permissions (deferred)
func executeInstallDir(macro Macro, ctx *MacroContext) (string, error) {
	if len(macro.Args) < 1 {
		return "", fmt.Errorf("INSTALL_DIR requires 1 argument: directory")
	}

	dir := macro.Args[0]

	ctx.InstalledPaths = append(ctx.InstalledPaths, InstalledPath{
		Path:      dir,
		Type:      "dir",
		Generated: true,
	})

	ctx.DeferredOps = append(ctx.DeferredOps, DeferredOperation{
		Type: "install_dir",
		Dst:  dir,
		Mode: "755",
	})

	return "", nil
}

// executeSymlink creates a symbolic link (deferred)
func executeSymlink(macro Macro, ctx *MacroContext) (string, error) {
	if len(macro.Args) < 2 {
		return "", fmt.Errorf("SYMLINK requires 2 arguments: target link_name")
	}

	target := macro.Args[0]
	link := macro.Args[1]

	ctx.InstalledPaths = append(ctx.InstalledPaths, InstalledPath{
		Path:      link,
		Type:      "symlink",
		Generated: true,
	})

	ctx.DeferredOps = append(ctx.DeferredOps, DeferredOperation{
		Type: "symlink",
		Src:  target,
		Dst:  link,
	})

	return "", nil
}

// executeCreateUser creates a system user.
// On Termux, useradd is not available, so this is a no-op.
func executeCreateUser(macro Macro, ctx *MacroContext) (string, error) {
	if isTermux() {
		return "", nil // Termux has no useradd
	}
	if len(macro.Args) < 1 {
		return "", fmt.Errorf("CREATE_USER requires 1 argument: username")
	}

	username := macro.Args[0]

	ctx.InstalledPaths = append(ctx.InstalledPaths, InstalledPath{
		Path:      username,
		Type:      "user",
		Generated: true,
	})

	return fmt.Sprintf("useradd -r -s /bin/false %s", username), nil
}

// executeRemoveUser removes a system user.
// On Termux, userdel is not available, so this is a no-op.
func executeRemoveUser(macro Macro, ctx *MacroContext) (string, error) {
	if isTermux() {
		return "", nil // Termux has no userdel
	}
	if len(macro.Args) < 1 {
		return "", fmt.Errorf("REMOVE_USER requires 1 argument: username")
	}

	return fmt.Sprintf("userdel %s", macro.Args[0]), nil
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
			commands = append(commands, fmt.Sprintf("systemctl disable %s 2>/dev/null || true", path.Path))
			commands = append(commands, fmt.Sprintf("systemctl stop %s 2>/dev/null || true", path.Path))
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

// ExecuteDeferredOps executes all deferred file operations
func ExecuteDeferredOps(ctx *MacroContext) error {
	if len(ctx.DeferredOps) == 0 {
		return nil
	}

	// Note: fakeroot is used during the build phase, not during deferred operations
	// Files created during build already have correct ownership/permissions from fakeroot

	for _, op := range ctx.DeferredOps {
		var err error
		switch op.Type {
		case "install":
			err = executeInstallOp(op, false)
		case "install_man":
			err = executeInstallManOp(op, false)
		case "install_dir":
			err = executeInstallDirOp(op, false)
		case "symlink":
			err = executeSymlinkOp(op, false)
		default:
			err = fmt.Errorf("unknown operation type: %s", op.Type)
		}

		if err != nil {
			return fmt.Errorf("failed to execute deferred operation %s: %w", op.Type, err)
		}
	}

	return nil
}

func executeInstallOp(op DeferredOperation, useFakeroot bool) error {
	// Create destination directory if needed
	destDir := filepath.Dir(op.Dst)
	if err := os.MkdirAll(destDir, 0755); err != nil {
		return fmt.Errorf("failed to create destination directory: %w", err)
	}

	// Copy the file
	srcFile, err := os.Open(op.Src)
	if err != nil {
		return fmt.Errorf("failed to open source file: %w", err)
	}
	defer srcFile.Close()

	// Handle permissions
	var perm os.FileMode = 0644
	if op.Mode == "755" {
		perm = 0755
	}

	// Direct copy (fakeroot is used during build phase, not here)
	destFile, err := os.OpenFile(op.Dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, perm)
	if err != nil {
		return fmt.Errorf("failed to create destination file: %w", err)
	}
	defer destFile.Close()

	if _, err := io.Copy(destFile, srcFile); err != nil {
		return fmt.Errorf("failed to copy file: %w", err)
	}

	// Set permissions explicitly
	if err := os.Chmod(op.Dst, perm); err != nil {
		return fmt.Errorf("failed to set permissions: %w", err)
	}

	return nil
}

func executeInstallManOp(op DeferredOperation, useFakeroot bool) error {
	// First install the file
	if err := executeInstallOp(DeferredOperation{
		Type: "install",
		Src:  op.Src,
		Dst:  op.Dst,
		Mode: op.Mode,
	}, false); err != nil {
		return err
	}

	// Then compress it (fakeroot is used during build phase, not here)
	cmd := exec.Command("gzip", "-f", op.Dst)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func executeInstallDirOp(op DeferredOperation, useFakeroot bool) error {
	// Direct directory creation (fakeroot is used during build phase, not here)
	return os.MkdirAll(op.Dst, 0755)
}

func executeSymlinkOp(op DeferredOperation, useFakeroot bool) error {
	// Remove existing link if it exists
	os.Remove(op.Dst)

	// Direct symlink creation (fakeroot is used during build phase, not here)
	return os.Symlink(op.Src, op.Dst)
}

// ValidateLine checks if a command line is safe to execute in strict mode
func ValidateLine(line string) error {
	dangerousPatterns := []string{
		"rm -rf /",
		"rm -rf /*",
		":(){:|:&};:",
		"mkfs",
		"dd if=/dev/zero",
		"dd if=/dev/random",
		"> /dev/sda",
		"> /dev/sdb",
		":(){:|:&};:",
	}

	line = strings.TrimSpace(line)
	for _, pattern := range dangerousPatterns {
		if strings.Contains(line, pattern) {
			return fmt.Errorf("potentially dangerous command detected: %s", pattern)
		}
	}

	return nil
}

// IsProtectedPath checks if a path is protected and should require explicit intent
func IsProtectedPath(path string) bool {
	path = strings.TrimSpace(path)
	protectedPaths := []string{
		"/",
		"/usr",
		"/bin",
		"/sbin",
		"/lib",
		"/lib64",
		"/etc",
		"/var",
		"/home",
		"/root",
		"/opt",
	}

	// Also protect the Termux prefix tree when running under Termux.
	if prefix := termuxPrefix(); prefix != "" {
		protectedPaths = append(protectedPaths,
			prefix,
			filepath.Join(prefix, "usr"),
			filepath.Join(prefix, "etc"),
			filepath.Join(prefix, "var"),
		)
	}

	for _, protected := range protectedPaths {
		if path == protected {
			return true
		}
		// Check if path is a direct child of protected path
		if strings.HasPrefix(path, protected+"/") && !strings.Contains(strings.TrimPrefix(path, protected+"/"), "/") {
			return true
		}
	}

	return false
}
