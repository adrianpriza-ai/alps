package more

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"

	"github.com/adrianpriza-ai/alps/platform"
)

// wrapWithFakeroot wraps a command with fakeroot if the operation is install/upgrade, safety mode is strict, not in Termux, and fakeroot is available.
// Remove/purge are never wrapped — they need real privileges to delete files.
// On macOS, fakeroot is not available, so this is a no-op.
func wrapWithFakeroot(cmd string, ctx *MacroContext) string {
	isInstallOp := ctx.Op == platform.OperationInstall || ctx.Op == platform.OperationUpgrade || ctx.Op == ""
	if isInstallOp && (ctx.Safety == "strict" || ctx.Safety == "") && !platform.IsTermux() && !platform.IsMacOS() && !platform.IsRoot() && hasFakeroot() {
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
