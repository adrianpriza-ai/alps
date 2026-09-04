package more

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/adrianpriza-ai/alps/platform"
)

// installParams configures executeInstallGeneric for each INSTALL_* macro.
type installParams struct {
	macroName     string        // e.g. "INSTALL_BIN" — used in error messages
	perm          string        // file permission (e.g. "755", "644")
	installedType string        // "file" or "service"
	defaultDir    func() string // returns the platform-specific default directory
}

// defaultBinDir returns the default binary directory for the current platform.
func defaultBinDir() string {
	if platform.IsMacOS() {
		return filepath.Join(platform.MacOSPrefix(), "bin")
	}
	return filepath.Join(platform.TermuxPrefix(), "/usr/bin")
}

// defaultLibDir returns the default library directory for the current platform.
func defaultLibDir() string {
	if platform.IsMacOS() {
		return filepath.Join(platform.MacOSPrefix(), "lib")
	}
	return filepath.Join(platform.TermuxPrefix(), "/usr/lib")
}

// defaultConfDir returns the default config directory for the current platform.
func defaultConfDir() string {
	if platform.IsMacOS() {
		return filepath.Join(platform.MacOSPrefix(), "etc")
	}
	return filepath.Join(platform.TermuxPrefix(), "/etc")
}

// defaultManDir returns the default man page directory for the current platform.
func defaultManDir() string {
	if platform.IsMacOS() {
		return filepath.Join(platform.MacOSPrefix(), "share/man/man1")
	}
	return filepath.Join(platform.TermuxPrefix(), "/usr/share/man/man1")
}

// executeInstallGeneric handles the common pattern for INSTALL_BIN, INSTALL_LIB,
// INSTALL_CONF, and INSTALL_MAN: validate args, resolve dest, track installed
// paths, and return a shell command that copies the file.
func executeInstallGeneric(macro Macro, ctx *MacroContext, p installParams) (string, error) {
	if len(macro.Args) < 1 {
		return "", fmt.Errorf("%s requires at least 1 argument: source [dest]", p.macroName)
	}
	if err := validateSafePath(macro.Args[0]); err != nil {
		return "", fmt.Errorf("%s invalid source: %w", p.macroName, err)
	}

	dest, err := resolveInstallDest(macro.Args, p)
	if err != nil {
		return "", err
	}

	ctx.InstalledPaths = append(ctx.InstalledPaths, InstalledPath{
		Path:      dest,
		Type:      p.installedType,
		Generated: true,
	})

	return buildInstallCmd(macro.Args[0], dest, p), nil
}

// resolveInstallDest computes the destination path: uses the last argument if
// provided, otherwise joins the filename onto the platform-specific default dir.
func resolveInstallDest(args []string, p installParams) (string, error) {
	source := args[0]
	if len(args) >= 2 {
		dest := args[len(args)-1]
		if err := validateSafePath(dest); err != nil {
			return "", fmt.Errorf("%s invalid dest: %w", p.macroName, err)
		}
		if strings.HasSuffix(dest, "/") {
			dest += filepath.Base(source)
		}
		return dest, nil
	}
	return filepath.Join(p.defaultDir(), filepath.Base(source)), nil
}

// buildInstallCmd returns the shell command string for a file install.
// The parent directory is computed in Go and every interpolated path is
// single-quoted, so macro-supplied paths with spaces or shell metacharacters
// can neither break the command nor inject extra commands (the old
// "mkdir -p $(dirname ...)" shell-ism would run whatever $(...) found).
func buildInstallCmd(source, dest string, p installParams) string {
	// When dest includes a custom path, create its parent dir; otherwise mkdir
	// the platform default dir up front (mkdir -p is idempotent either way).
	parent := p.defaultDir()
	if strings.Contains(dest, "/") {
		parent = filepath.Dir(dest)
	}
	msg := fmt.Sprintf("  %s  installed %s to %s", getSymOK(), source, dest)
	return fmt.Sprintf("mkdir -p %s && cp %s %s && chmod %s %s && echo %s",
		shellQuote(parent), shellQuote(source), shellQuote(dest), p.perm, shellQuote(dest), shellQuote(msg))
}

// executeInstallBin installs a binary to /usr/bin (or Termux equivalent) or specified directory.
func executeInstallBin(macro Macro, ctx *MacroContext) (string, error) {
	return executeInstallGeneric(macro, ctx, installParams{
		macroName:     "INSTALL_BIN",
		perm:          "755",
		installedType: "file",
		defaultDir:    defaultBinDir,
	})
}

// executeInstallLib installs a library to /usr/lib (or Termux equivalent) or specified directory.
func executeInstallLib(macro Macro, ctx *MacroContext) (string, error) {
	return executeInstallGeneric(macro, ctx, installParams{
		macroName:     "INSTALL_LIB",
		perm:          "644",
		installedType: "file",
		defaultDir:    defaultLibDir,
	})
}

// executeInstallConf installs a config file to /etc (or Termux equivalent) or specified directory.
func executeInstallConf(macro Macro, ctx *MacroContext) (string, error) {
	return executeInstallGeneric(macro, ctx, installParams{
		macroName:     "INSTALL_CONF",
		perm:          "644",
		installedType: "file",
		defaultDir:    defaultConfDir,
	})
}

// executeInstallMan installs a man page to /usr/share/man (or Termux equivalent) or specified directory.
// Man pages are gzipped after installation.
func executeInstallMan(macro Macro, ctx *MacroContext) (string, error) {
	// Resolve args and track installed path using the generic helper
	if len(macro.Args) < 1 {
		return "", fmt.Errorf("INSTALL_MAN requires at least 1 argument: source [dest]")
	}
	if err := validateSafePath(macro.Args[0]); err != nil {
		return "", fmt.Errorf("INSTALL_MAN invalid source: %w", err)
	}
	p := installParams{macroName: "INSTALL_MAN", perm: "644", installedType: "file", defaultDir: defaultManDir}
	dest, err := resolveInstallDest(macro.Args, p)
	if err != nil {
		return "", err
	}
	// Add .gz extension since we gzip the man page
	if !strings.HasSuffix(dest, ".gz") {
		dest += ".gz"
	}
	ctx.InstalledPaths = append(ctx.InstalledPaths, InstalledPath{
		Path:      dest,
		Type:      "file",
		Generated: true,
	})
	// Copy without .gz suffix first, then gzip in-place. All paths are
	// single-quoted so macro-supplied file names stay one literal word.
	uncompressedDest := strings.TrimSuffix(dest, ".gz")
	msg := fmt.Sprintf("  %s  installed %s to %s", getSymOK(), macro.Args[0], dest)
	return fmt.Sprintf("mkdir -p %s && cp %s %s && chmod 644 %s && gzip -f %s && echo %s",
		shellQuote(filepath.Dir(uncompressedDest)), shellQuote(macro.Args[0]), shellQuote(uncompressedDest),
		shellQuote(uncompressedDest), shellQuote(uncompressedDest), shellQuote(msg)), nil
}

// executeInstallService installs a systemd service file.
// On Termux and macOS, systemd is not available, so this is a no-op.
func executeInstallService(macro Macro, ctx *MacroContext) (string, error) {
	if platform.IsTermux() || platform.IsMacOS() {
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
		// Return install command using mkdir, cp, chmod, and echo. The parent
		// dir is computed in Go and all paths are single-quoted.
		msg := fmt.Sprintf("  %s  installed %s to %s", symOK, macro.Args[0], dest)
		return fmt.Sprintf("mkdir -p %s && cp %s %s && chmod 644 %s && echo %s",
			shellQuote(filepath.Dir(dest)), shellQuote(macro.Args[0]), shellQuote(dest), shellQuote(dest), shellQuote(msg)), nil
	}
	// Default to /etc/systemd/system directory, store just the service name
	serviceName := filepath.Base(macro.Args[0])
	dest := filepath.Join("/etc/systemd/system", serviceName)
	ctx.InstalledPaths = append(ctx.InstalledPaths, InstalledPath{
		Path:      serviceName,
		Type:      "service",
		Generated: true,
	})
	// Return install command using mkdir, cp, chmod, and echo.
	msg := fmt.Sprintf("  %s  installed %s to %s", symOK, macro.Args[0], dest)
	return fmt.Sprintf("mkdir -p %s && cp %s %s && chmod 644 %s && echo %s",
		shellQuote("/etc/systemd/system"), shellQuote(macro.Args[0]), shellQuote(dest), shellQuote(dest), shellQuote(msg)), nil
}

// executeInstallDir creates a directory with standard permissions.
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

	msg := fmt.Sprintf("  %s  installed directory %s", getSymOK(), dir)
	return fmt.Sprintf("mkdir -p %s && echo %s", shellQuote(dir), shellQuote(msg)), nil
}

// executeSymlink creates a symbolic link.
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

	msg := fmt.Sprintf("  %s  installed symlink %s -> %s", getSymOK(), link, target)
	return fmt.Sprintf("ln -sf %s %s && echo %s", shellQuote(target), shellQuote(link), shellQuote(msg)), nil
}

// executeExtract extracts an archive.
func executeExtract(macro Macro, ctx *MacroContext) (string, error) {
	if len(macro.Args) < 1 {
		return "", fmt.Errorf("EXTRACT requires 1 argument: archive_file")
	}

	archive := macro.Args[0]
	if err := validateSafePath(archive); err != nil {
		return "", fmt.Errorf("EXTRACT invalid archive path: %w", err)
	}

	// Detect archive type and extract accordingly. The archive name is
	// single-quoted so spaces or metacharacters in it cannot inject commands.
	var cmd string
	if strings.HasSuffix(archive, ".tar.gz") || strings.HasSuffix(archive, ".tgz") {
		cmd = fmt.Sprintf("tar -xzf %s", shellQuote(archive))
	} else if strings.HasSuffix(archive, ".tar.xz") || strings.HasSuffix(archive, ".txz") {
		cmd = fmt.Sprintf("tar -xJf %s", shellQuote(archive))
	} else if strings.HasSuffix(archive, ".tar.bz2") || strings.HasSuffix(archive, ".tbz") {
		cmd = fmt.Sprintf("tar -xjf %s", shellQuote(archive))
	} else if strings.HasSuffix(archive, ".zip") {
		cmd = fmt.Sprintf("unzip %s", shellQuote(archive))
	} else {
		cmd = fmt.Sprintf("tar -xf %s", shellQuote(archive))
	}

	return wrapWithFakeroot(cmd, ctx), nil
}
