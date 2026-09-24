package more

import (
	"fmt"
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

// detectDistro returns the canonical distribution ID and ID_LIKE values.
func detectDistro() (string, []string) {
	id := platform.DistroID()
	if id == "" {
		return "unknown", nil
	}
	return id, platform.DistroIDLike()
}

func detectDistroVersion() string {
	if version := platform.DistroVersion(); version != "" {
		return version
	}
	return "unknown"
}

// osMatches checks whether a package's OS list includes the current system.
func osMatches(osList []string, distro string, idLike []string) bool {
	for _, o := range osList {
		o = strings.ToLower(strings.TrimSpace(o))
		if o == "linux" {
			if !platform.IsTermux() && !platform.IsMacOS() {
				return true
			}
			continue
		}
		if o == "darwin" || o == "macos" {
			if platform.IsMacOS() {
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
