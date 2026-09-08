package more

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"

	"github.com/adrianpriza-ai/alps/platform"
)

// shellQuote wraps s in single quotes so the shell treats it as one literal
// argument, escaping any embedded single quote ('\” is the POSIX idiom).
//
// Macro-supplied paths and names come from third-party ALPSMORE files, so
// interpolating them unquoted would let spaces split a path into several
// words and let metacharacters like $(), backticks or ; execute injected
// commands. Quoting every interpolated value keeps the whole command inert.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// isAlreadyFakeroot reports whether the command already starts with fakeroot.
func isAlreadyFakeroot(cmd string) bool {
	trimmed := strings.TrimSpace(cmd)
	return strings.HasPrefix(trimmed, "fakeroot ") || strings.HasPrefix(trimmed, "/usr/bin/fakeroot ")
}

// shouldWrapWithFakeroot reports whether commands should be wrapped with fakeroot
// based on the operation, safety setting, and host platform.
func shouldWrapWithFakeroot(ctx *MacroContext) bool {
	if ctx == nil {
		return false
	}
	isInstallOp := ctx.Op == platform.OperationInstall || ctx.Op == platform.OperationUpgrade || ctx.Op == ""
	isStrict := ctx.Safety == "strict" || ctx.Safety == ""
	return isInstallOp && isStrict && !platform.IsTermux() && !platform.IsMacOS() && !platform.IsRoot()
}

// wrapWithFakeroot wraps a command with fakeroot if the operation is install/upgrade,
// safety mode is strict, not in Termux, and fakeroot is available.
func wrapWithFakeroot(cmd string, ctx *MacroContext) string {
	if shouldWrapWithFakeroot(ctx) && hasFakeroot() && !isAlreadyFakeroot(cmd) {
		cmd = stripSudo(cmd)
		return fmt.Sprintf("fakeroot -- %s", cmd)
	}
	return cmd
}

// detectDistro reads /etc/os-release (or uses platform helpers) to return the
// distro ID and the ID_LIKE list for matching package OS fields.
func detectDistro() (id string, idLike []string) {
	// Termux has no /etc/os-release — it is its own environment
	if platform.IsTermux() {
		return "termux", []string{"termux"}
	}

	// macOS detection
	if runtime.GOOS == "darwin" {
		return "macos", []string{"darwin", "macos"}
	}

	data, err := os.ReadFile("/etc/os-release")
	if err != nil {
		return "unknown", nil
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "ID=") {
			id = strings.Trim(line[3:], `"'`)
		} else if strings.HasPrefix(line, "ID_LIKE=") {
			raw := strings.Trim(line[8:], `"'`)
			idLike = strings.Fields(raw)
		}
	}

	// Inject "wsl" so entries with os=wsl explicitly match on WSL hosts
	if platform.IsWSL() {
		idLike = append(idLike, "wsl")
	}
	return
}

// detectDistroVersion returns the version string for the current distro.
func detectDistroVersion() string {
	if platform.IsTermux() {
		ver := os.Getenv("TERMUX_VERSION")
		if ver != "" {
			return ver
		}
		return "unknown"
	}

	// macOS version detection
	if runtime.GOOS == "darwin" {
		cmd := exec.Command("sw_vers", "-productVersion")
		output, err := cmd.Output()
		if err == nil {
			return strings.TrimSpace(string(output))
		}
		return "unknown"
	}

	data, err := os.ReadFile("/etc/os-release")
	if err != nil {
		return "unknown"
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "VERSION_ID=") {
			return strings.Trim(line[11:], `"'`)
		}
	}
	return "unknown"
}

// osMatches checks whether a package's OS list includes the current system.
func osMatches(osList []string, distro string, idLike []string) bool {
	for _, o := range osList {
		o = strings.ToLower(strings.TrimSpace(o))
		if o == "linux" {
			if !platform.IsTermux() && runtime.GOOS != "darwin" {
				return true
			}
			continue
		}
		if o == "darwin" || o == "macos" {
			if runtime.GOOS == "darwin" {
				return true
			}
			continue
		}
		if strings.ToLower(distro) == o {
			return true
		}
		for _, like := range idLike {
			if strings.ToLower(like) == o {
				return true
			}
		}
	}
	return false
}
