package more

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/adrianpriza-ai/alps/priv"
)

// OperationType represents the type of operation being performed
type OperationType string

const (
	OperationInstall OperationType = "install"
	OperationRemove  OperationType = "remove"
	OperationUpgrade OperationType = "upgrade"
	OperationPurge   OperationType = "purge"
)

// ExecutionManifest represents the execution plan stored in the temp dir
// (/tmp/.alps_runner.txt on Linux, $PREFIX/tmp/.alps_runner.txt on Termux)
type ExecutionManifest struct {
	BuildEnv  []string // Build environment commands (before installation)
	AfterEnv  []string // After environment commands (installation macros)
	ScriptNum int      // Counter for generating script numbers
}

// Scrape extracts command blocks from an Entry based on the operation type
func Scrape(e *Entry, op OperationType) ([]string, error) {
	var lines []string

	switch op {
	case OperationInstall:
		if len(e.CmdLines) == 0 {
			return nil, fmt.Errorf("package %q has no install commands (cmd_begin/cmd_end)", e.Name)
		}
		lines = append([]string(nil), e.CmdLines...)
	case OperationRemove:
		// Try to get remove lines from entry first
		if len(e.RemoveLines) > 0 {
			lines = append([]string(nil), e.RemoveLines...)
		} else {
			// Fallback to installed.json if available
			rec, isInstalled := GetInstalled(e.Name)
			if !isInstalled {
				return nil, fmt.Errorf("package %q is not installed and has no remove commands", e.Name)
			}
			if len(rec.RemoveLines) == 0 {
				return nil, fmt.Errorf("package %q has no remove commands in installed.json", e.Name)
			}
			lines = append([]string(nil), rec.RemoveLines...)
		}
	case OperationUpgrade:
		if len(e.UpgradeLines) > 0 {
			lines = append([]string(nil), e.UpgradeLines...)
		} else {
			// Fallback to cmd_lines if upgrade_lines not defined
			if len(e.CmdLines) == 0 {
				return nil, fmt.Errorf("package %q has no upgrade or install commands", e.Name)
			}
			lines = append([]string(nil), e.CmdLines...)
		}
	case OperationPurge:
		// Try to get purge lines from entry first
		if len(e.PurgeLines) > 0 {
			lines = append([]string(nil), e.PurgeLines...)
		} else {
			// Fallback to installed.json if available
			rec, isInstalled := GetInstalled(e.Name)
			if !isInstalled {
				return nil, fmt.Errorf("package %q is not installed and has no purge commands", e.Name)
			}
			if len(rec.PurgeLines) == 0 {
				return nil, fmt.Errorf("package %q has no purge commands in installed.json", e.Name)
			}
			lines = append([]string(nil), rec.PurgeLines...)
		}
	default:
		return nil, fmt.Errorf("unknown operation type: %s", op)
	}

	return lines, nil
}

// BuildEnvMacros defines macros that should be executed in build_env
var BuildEnvMacros = map[string]bool{
	"BASH_RUN": true,
	"DOWNLOAD": true,
	"EXTRACT":  true,
	"SH":       true,
}

// AfterEnvMacros defines macros that should be executed in after_env
var AfterEnvMacros = map[string]bool{
	"INSTALL_BIN":     true,
	"INSTALL_LIB":     true,
	"INSTALL_CONF":    true,
	"INSTALL_MAN":     true,
	"INSTALL_SERVICE": true,
	"INSTALL_DIR":     true,
	"SYMLINK":         true,
	"ENABLE_SERVICE":  true,
	"DISABLE_SERVICE": true,
	"START_SERVICE":   true,
	"STOP_SERVICE":    true,
	"RESTART_SERVICE": true,
	"CREATE_USER":     true,
	"REMOVE_USER":     true,
}

// Filter separates build_env and after_env macros from scraped lines, creates temporary scripts,
// and generates an execution manifest.
// Note on Privilege Escalation & Macro Execution:
//   - Macros in BuildEnvMacros (DOWNLOAD, BASH_RUN, EXTRACT, SH) and AfterEnvMacros (INSTALL_*, *_SERVICE, *_USER)
//     are executed by Go with Go-level process privileges during manifest execution.
//   - Plain shell commands placed into temp scripts are wrapped in fakeroot when running under strict safety
//     (install/upgrade on Linux as non-root), and any inner sudo/doas/pkexec commands are stripped by stripSudo.
func Filter(lines []string, ctx *MacroContext, op OperationType) (*ExecutionManifest, error) {
	manifest := &ExecutionManifest{
		BuildEnv:  []string{},
		AfterEnv:  []string{},
		ScriptNum: 0,
	}
	stripEsc := false
	if (op == OperationInstall || op == OperationUpgrade) &&
		(ctx.Safety == "strict" || ctx.Safety == "") &&
		!isTermux() && !isMacOS() && !isRoot() {
		stripEsc = true
	}

	var currentBuffer []string

	// First, expand all placeholders globally
	expandedLines := make([]string, len(lines))
	for i, line := range lines {
		expandedLines[i] = expandPlaceholders(line, ctx)
	}

	// Process line by line
	for _, line := range expandedLines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		// Check if line starts with a macro
		macro, _, isMacro := ParseMacro(line)

		if isMacro {
			// Flush current buffer if it has content
			if len(currentBuffer) > 0 {
				scriptPath, err := writeTempScript(currentBuffer, manifest.ScriptNum)
				if err != nil {
					return nil, fmt.Errorf("failed to write temp script: %w", err)
				}
				// Use the actual shell command, not macro syntax
				shell := "sh"
				if _, err := exec.LookPath("bash"); err == nil {
					shell = "bash"
				}
				manifest.BuildEnv = append(manifest.BuildEnv, fmt.Sprintf("%s %s", shell, scriptPath))
				manifest.ScriptNum++
				currentBuffer = []string{}
			}

			// Categorize macro and store raw macro syntax (defer expansion to execution)
			if BuildEnvMacros[macro.Name] {
				// Store raw macro syntax in build_env
				manifest.BuildEnv = append(manifest.BuildEnv, line)
			} else if AfterEnvMacros[macro.Name] {
				// Store raw macro syntax in after_env
				manifest.AfterEnv = append(manifest.AfterEnv, line)
			} else {
				// Unknown macro - treat as plain text
				if stripEsc {
					line = stripSudo(line)
				}
				currentBuffer = append(currentBuffer, line)
			}
		} else {
			// Plain shell command - strip privilege escalation when it will run
			// under fakeroot (install/upgrade + strict), otherwise keep as-is.
			if stripEsc {
				line = stripSudo(line)
			}
			currentBuffer = append(currentBuffer, line)
		}
	}

	// Flush remaining buffer
	if len(currentBuffer) > 0 {
		scriptPath, err := writeTempScript(currentBuffer, manifest.ScriptNum)
		if err != nil {
			return nil, fmt.Errorf("failed to write temp script: %w", err)
		}
		// Use the actual shell command, not macro syntax
		shell := "sh"
		if _, err := exec.LookPath("bash"); err == nil {
			shell = "bash"
		}
		manifest.BuildEnv = append(manifest.BuildEnv, fmt.Sprintf("%s %s", shell, scriptPath))
		manifest.ScriptNum++
	}

	return manifest, nil
}

// expandPlaceholders expands all placeholder tokens in a line
func expandPlaceholders(line string, ctx *MacroContext) string {
	line = strings.ReplaceAll(line, "{ARCH}", ctx.Arch)
	line = strings.ReplaceAll(line, "{VERSION}", ctx.Version)
	line = strings.ReplaceAll(line, "{SERVER}", ctx.Server)
	line = strings.ReplaceAll(line, "{PKGNAME}", ctx.PackageName)
	line = strings.ReplaceAll(line, "{DISVER}", ctx.DistroVersion)
	return line
}

// writeTempScript writes a temporary script file and returns its path
func writeTempScript(lines []string, num int) (string, error) {
	tmpDir := os.TempDir()
	scriptPath := filepath.Join(tmpDir, fmt.Sprintf(".alps_run%d.sh", num))

	content := "set -e\n" + strings.Join(lines, "\n")
	if err := os.WriteFile(scriptPath, []byte(content), 0755); err != nil {
		return "", err
	}

	return scriptPath, nil
}

// WriteManifest writes the execution manifest to .alps_runner.txt in the temp
// dir (os.TempDir(); /tmp on Linux, $PREFIX/tmp on Termux)
func WriteManifest(manifest *ExecutionManifest) error {
	manifestPath := filepath.Join(os.TempDir(), ".alps_runner.txt")

	var content strings.Builder
	content.WriteString("build_env\n")
	for _, cmd := range manifest.BuildEnv {
		content.WriteString(cmd + "\n")
	}
	content.WriteString("after_env\n")
	for _, cmd := range manifest.AfterEnv {
		content.WriteString(cmd + "\n")
	}

	return os.WriteFile(manifestPath, []byte(content.String()), 0644)
}

// ReadManifest reads the execution manifest from .alps_runner.txt in the temp
// dir (os.TempDir(); /tmp on Linux, $PREFIX/tmp on Termux)
func ReadManifest() (*ExecutionManifest, error) {
	manifestPath := filepath.Join(os.TempDir(), ".alps_runner.txt")
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		return nil, err
	}

	manifest := &ExecutionManifest{
		BuildEnv:  []string{},
		AfterEnv:  []string{},
		ScriptNum: 0,
	}

	lines := strings.Split(string(data), "\n")
	var currentSection string

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		if line == "build_env" {
			currentSection = "build_env"
			continue
		}
		if line == "after_env" {
			currentSection = "after_env"
			continue
		}

		switch currentSection {
		case "build_env":
			manifest.BuildEnv = append(manifest.BuildEnv, line)
		case "after_env":
			manifest.AfterEnv = append(manifest.AfterEnv, line)
		}
	}

	return manifest, nil
}

// Executes execution manifest with proper error handling, wraps build_env and after_env with fakeroot if safety=strict, and avoids automatic cleanup/removal.
func ExecuteManifest(manifest *ExecutionManifest, e *Entry, op OperationType, ctx *MacroContext) error {
	afterEnvSudo := !isTermux() && !isMacOS()

	// Get build directory for macro context and change to it
	pkgDir, err := getBuildDir(e.Name)
	if err != nil {
		return err
	}
	ctx.BuildDir = pkgDir

	// Change to package-specific build directory
	originalDir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get current directory: %w", err)
	}
	if err := os.Chdir(pkgDir); err != nil {
		return fmt.Errorf("failed to change to build directory %s: %w", pkgDir, err)
	}
	defer os.Chdir(originalDir) // Always restore original directory

	// Execute build_env first
	if len(manifest.BuildEnv) > 0 {
		fmt.Printf("  executing build_env (%d commands)...\n", len(manifest.BuildEnv))
		for _, cmd := range manifest.BuildEnv {
			// Expand macros before execution
			expanded, err := ExpandMacros([]string{cmd}, ctx)
			if err != nil {
				return fmt.Errorf("failed to expand build_env macro: %w", err)
			}
			if len(expanded) > 0 && expanded[0] != "" {
				cmd = expanded[0]
			} else {
				// Skip empty commands (macros that execute in Go)
				continue
			}

			// Wraps build_env and after_env with fakeroot for install/upgrade operations, removing permissions and purge system files.
			// On macOS, fakeroot is not available, so this is skipped.
			if (op == OperationInstall || op == OperationUpgrade) &&
				(ctx.Safety == "strict" || ctx.Safety == "") && !isTermux() && !isMacOS() && !isRoot() {
				if err := requireFakeroot(); err != nil {
					return err
				}
				if hasFakeroot() && !isAlreadyFakeroot(cmd) {
					cmd = stripSudo(cmd)
					cmd = fmt.Sprintf("fakeroot -- %s", cmd)
				}
			}

			if err := executeCommand(cmd, false); err != nil {
				return fmt.Errorf("build_env command failed: %w", err)
			}
		}
		fmt.Printf("  build_env completed successfully\n")
	}

	// Only execute after_env if build_env succeeded
	if len(manifest.AfterEnv) > 0 {
		fmt.Printf("  executing after_env (%d commands)...\n", len(manifest.AfterEnv))
		for _, cmd := range manifest.AfterEnv {
			// Expand macros before execution
			expanded, err := ExpandMacros([]string{cmd}, ctx)
			if err != nil {
				return fmt.Errorf("failed to expand after_env macro: %w", err)
			}
			if len(expanded) > 0 && expanded[0] != "" {
				cmd = expanded[0]
			} else {
				// Skip empty commands (macros that execute in Go)
				continue
			}

			if err := executeCommand(cmd, afterEnvSudo); err != nil {
				return fmt.Errorf("after_env command failed: %w", err)
			}
		}
		fmt.Printf("  after_env completed successfully\n")
	}

	return nil
}

// executeCommand executes a single command with optional sudo
func executeCommand(cmd string, useSudo bool) error {
	var execCmd *exec.Cmd
	if useSudo && !isTermux() {
		var err error
		execCmd, err = priv.Command("sh", "-c", cmd)
		if err != nil {
			return err
		}
	} else {
		execCmd = exec.Command("sh", "-c", cmd)
	}
	execCmd.Stdout = os.Stdout
	execCmd.Stderr = os.Stderr
	return execCmd.Run()
}

// isAlreadyFakeroot returns true if the command already starts with fakeroot
// (e.g. from macro-level wrapping via wrapWithFakeroot).
func isAlreadyFakeroot(cmd string) bool {
	trimmed := strings.TrimSpace(cmd)
	return strings.HasPrefix(trimmed, "fakeroot ") || strings.HasPrefix(trimmed, "/usr/bin/fakeroot ")
}

// privEscPrefixes lists known privilege escalation commands and their absolute paths.
var privEscPrefixes = []string{
	"sudo ", "sudo\t", "/usr/bin/sudo ", "/usr/bin/sudo\t",
	"/usr/local/bin/sudo ", "/usr/local/bin/sudo\t",
	"doas ", "doas\t", "/usr/bin/doas ", "/usr/bin/doas\t",
	"/usr/local/bin/doas ", "/usr/local/bin/doas\t",
	"pkexec ", "pkexec\t", "/usr/bin/pkexec ", "/usr/bin/pkexec\t",
	// su is special: "su -c 'command'" — strip it too
	"su -c ", "/usr/bin/su -c ",
}

// commonFlags lists flag prefixes that may follow sudo/doas/pkexec (without args).
var commonFlags = []string{
	"-E ", "-H ", "-S ", "-A ", "-k ", "-n ", "-p ", "-u ", "-g ",
	"-E\t", "-H\t", "-S\t", "-A\t", "-k\t", "-n\t", "-p\t", "-u\t", "-g\t",
	"--preserve-env=", "--non-interactive ", "--reset-timestamp ",
	"-- ",
}

// stripPrivEsc removes privilege escalation commands (sudo, doas, pkexec, su)
// and their common flags from the beginning of a command string.
func stripPrivEsc(cmd string) string {
	trimmed := strings.TrimSpace(cmd)

	stripped := false
	for _, prefix := range privEscPrefixes {
		if strings.HasPrefix(trimmed, prefix) {
			trimmed = strings.TrimSpace(trimmed[len(prefix):])
			stripped = true
			break
		}
	}

	if !stripped {
		return cmd
	}

	// Strip common flags that may follow the escalation command
	for {
		skipped := false
		for _, flag := range commonFlags {
			if strings.HasPrefix(trimmed, flag) {
				trimmed = strings.TrimSpace(trimmed[len(flag):])
				skipped = true
				break
			}
		}
		if !skipped {
			break
		}
	}
	return trimmed
}

// stripSudo is an alias for stripPrivEsc for backward compatibility.
func stripSudo(cmd string) string {
	return stripPrivEsc(cmd)
}
