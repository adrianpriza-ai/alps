package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/adrianpriza-ai/alps/aur"
	"github.com/adrianpriza-ai/alps/backend/aurbackend"
	"github.com/adrianpriza-ai/alps/backend/repo"
	"github.com/adrianpriza-ai/alps/cli"
	"github.com/adrianpriza-ai/alps/completion"
	"github.com/adrianpriza-ai/alps/config"
	"github.com/adrianpriza-ai/alps/extra"
	"github.com/adrianpriza-ai/alps/pack"
	"github.com/adrianpriza-ai/alps/platform"
	"github.com/adrianpriza-ai/alps/priv"
	"github.com/adrianpriza-ai/alps/runner"
	"github.com/adrianpriza-ai/alps/ui"
)

var Version = "dev"

const author = "\033]8;;https://github.com/adrianpriza-ai\aadrianpriza-ai\033]8;;\a"

func main() {
	cfg := config.Load()
	cfg.Version = Version

	if len(os.Args) < 2 {
		ui.PrintDiagnostic(cfg)
		os.Exit(0)
	}

	cmd := strings.ToLower(os.Args[1])
	args := os.Args[2:]

	switch cmd {
	case "version", "--version":
		fmt.Printf("%salps%s %s by %s\n", cfg.Style.ColorPrimary, cfg.Style.ColorReset, Version, author)
	case "completion":
		if len(args) == 0 {
			fmt.Fprintln(os.Stderr, "Usage: alps completion <fish|bash|zsh>")
			os.Exit(1)
		}
		if err := completion.Generate(args[0], os.Stdout); err != nil {
			ui.Msgf(cfg, ui.LevelError, "%v", err)
			os.Exit(1)
		}
	case "help", "--help", "-h":
		ui.PrintHelp(cfg)
	case "aliases":
		ui.PrintAliases(cfg)
	case "config-show":
		ui.PrintConfigShow(cfg)
	default:
		resolved, err := cli.ResolveCmd(cmd, cfg)
		if err != nil {
			ui.Msgf(cfg, ui.LevelError, "%v", err)
			os.Exit(1)
		}
		dispatchResolved(resolved, args, cfg)
	}
}

func dispatchResolved(resolved string, args []string, cfg *config.Config) {
	switch resolved {
	case "repo":
		runRepo(args, cfg)
	case "aur":
		runAUR(args, cfg)
	case "winget":
		runWinget(args, cfg)
	case "flatpak":
		runFlatpak(args, cfg)
	case "snap":
		runSnap(args, cfg)
	default:
		runPkg(resolved, args, cfg)
	}
}

func detectBackend() string {
	return pack.DetectName()
}

func detectRealBackend() string {
	b := pack.Detect()
	if b == nil {
		return ""
	}
	return b.Bin
}

func needsSudo(backend string) bool {
	return pack.NeedsSudo(backend)
}

func runPkg(subcmd string, args []string, cfg *config.Config) {
	backend := detectBackend()
	ui.PrintHeader(cfg)

	if backend == "" {
		ui.Msg(cfg, ui.LevelError, "No supported package manager found (apt/dnf/pacman/zypper/apk/brew)")
		os.Exit(1)
	}

	_, flags := splitFlagsAll(args)
	dryRun := flags.DryRun

	// Check if the backend supports this command
	if !pack.CommandSupported(backend, subcmd) {
		ui.Msgf(cfg, ui.LevelError, "Command '%s' is not supported by %s", subcmd, backend)
		os.Exit(1)
	}

	// Handle edit-sources command
	if subcmd == "edit-sources" {
		runEditSources(backend, cfg)
		return
	}

	if backend == "pacman" && (subcmd == "update" || subcmd == "upgrade") {
		if !warnPacmanPartialUpgrade(cfg) {
			ui.Msg(cfg, ui.LevelWarn, "Cancelled.")
			return
		}
		fmt.Println()
	}

	switch backend {
	case "pacman":
		switch subcmd {
		case "install":
			runPacmanWithAURFallback(args, dryRun, cfg)
		case "search":
			runPacmanSearch(args, cfg)
		case "autoremove":
			runPacmanAutoremove(cfg)
		case "upgrade", "full-upgrade":
			runPacmanUpgrade(subcmd, args, flags, cfg)
		default:
			runPkgDefault(backend, subcmd, args, flags, cfg)
		}
	case "apt":
		switch subcmd {
		case "install":
			runAptWithSnapFallback(args, dryRun, cfg)
		case "search":
			runAptSearch(args, cfg)
		case "full-upgrade":
			runAptFullUpgrade(args, flags, cfg)
		default:
			runPkgDefault(backend, subcmd, args, flags, cfg)
		}
	case "brew":
		// brew doesn't need special handling like pacman/aur fallback
		runPkgDefault(backend, subcmd, args, flags, cfg)
	default:
		runPkgDefault(backend, subcmd, args, flags, cfg)
	}
}

func warnPacmanPartialUpgrade(cfg *config.Config) bool {
	ui.Msg(cfg, ui.LevelWarn, "Running pacman -Sy or -Su alone is not recommended on Arch.")
	fmt.Print("     This may cause partial upgrades and break your system.")
	fmt.Println()
	fmt.Println()
	ui.Msgf(cfg, ui.LevelInfo, "Use %sfull-upgrade%s (alps fug) to sync and upgrade at once.",
		cfg.Style.ColorBold, cfg.Style.ColorReset+cfg.Style.ColorInfo)
	fmt.Print(cfg.Style.ColorReset)
	fmt.Println()
	return ui.PromptYesNo("  Continue anyway?", false)
}

func runPkgDefault(backend, subcmd string, args []string, flags pack.Flags, cfg *config.Config) {
	realBackend := detectRealBackend()
	mapped, ok := pack.Lookup(backend, subcmd) // Lookup already returns a copy
	if !ok {
		mapped = []string{realBackend, subcmd}
	} else {
		mapped[0] = realBackend
	}
	runWithBackendFlagsExt(mapped, args, cfg, backend, subcmd, flags)
}

func splitFlags(args []string) (pkgs []string, noConfirm bool) {
	pkgs, _, noConfirm = pack.ParseFlags(args)
	return
}

// splitFlagsAll returns full parsed flags using ParseFlagsExt.
func splitFlagsAll(args []string) (pkgs []string, f pack.Flags) {
	return pack.ParseFlagsExt(args)
}

func fmtCmd(cmdArgs []string, extraArgs []string) string {
	parts := make([]string, len(cmdArgs))
	copy(parts, cmdArgs)
	for _, a := range extraArgs {
		if a != "" {
			parts = append(parts, a)
		}
	}
	return strings.Join(parts, " ")
}

func runWithBackend(cmdArgs []string, args []string, cfg *config.Config, backend, subcmd string) bool {
	return runWithBackendFlagsExt(cmdArgs, args, cfg, backend, subcmd, pack.Flags{})
}

func runWithBackendFlags(cmdArgs []string, args []string, cfg *config.Config, backend, subcmd string, dryRun, noConfirm bool) bool {
	return runWithBackendFlagsExt(cmdArgs, args, cfg, backend, subcmd, pack.Flags{DryRun: dryRun, NoConfirm: noConfirm})
}

func runWithBackendFlagsExt(cmdArgs []string, args []string, cfg *config.Config, backend, subcmd string, f pack.Flags) bool {
	sudo := needsSudo(backend)

	// Strip alps meta-flags from args before forwarding to backend.
	cleanArgs, _ := pack.ParseFlagsExt(args)

	// Append backend-native flags for all active alps flags.
	extraFlags := pack.BuildExtraFlagsExt(backend, f)
	cleanArgs = append(cleanArgs, extraFlags...)

	// Report alps flags the backend cannot honour so they are never dropped silently.
	for _, w := range pack.UnsupportedFlagWarnings(backend, f.NoConfirm, f.Force) {
		ui.Msgf(cfg, ui.LevelWarn, "%s", w)
	}
	if f.DryRun && !pack.DryRunEmitted(backend) {
		ui.Msgf(cfg, ui.LevelWarn, "--dry-run is preview-only for %s — no package plan shown", backend)
	}

	fullArgs := make([]string, len(cmdArgs[1:]))
	copy(fullArgs, cmdArgs[1:])
	fullArgs = append(fullArgs, cleanArgs...)

	display := fmtCmd(cmdArgs, cleanArgs)
	if f.DryRun {
		fmt.Println()
	}
	ui.Msgf(cfg, ui.LevelInfo, "%s (%s%s%s)",
		subcmd,
		cfg.Style.ColorDim,
		display,
		cfg.Style.ColorReset)
	fmt.Print(cfg.Style.ColorReset)
	fmt.Println()

	if sudo && !f.DryRun {
		if err := ensureSudo(); err != nil {
			ui.Msg(cfg, ui.LevelError, "privilege escalation failed")
			return false
		}
	}

	// Use the new runner for consistent command execution
	r := runner.NewDefaultRunner(f.DryRun).WithWriter(os.Stdout)
	cmd := runner.BuildCommand(cmdArgs[0], fullArgs...)
	if sudo {
		cmd = cmd.WithPrivilege()
	}

	if err := r.Run(context.Background(), cmd); err != nil {
		ui.Msgf(cfg, ui.LevelError, "%v", err)
		return false
	}
	ui.Msg(cfg, ui.LevelOK, "Done.")
	return true
}

func ensureSudo() error {
	return priv.Ensure()
}

func runEditSources(backend string, cfg *config.Config) {
	var sourcesFile string
	var editor string

	// Determine the sources file for each backend
	switch backend {
	case "apt", "apt-get":
		sourcesFile = "/etc/apt/sources.list"
	case "pacman":
		sourcesFile = "/etc/pacman.conf"
	case "dnf":
		sourcesFile = "/etc/yum.repos.d/"
	case "zypper":
		sourcesFile = "/etc/zypp/repos.d/"
	case "apk":
		sourcesFile = "/etc/apk/repositories"
	case "brew":
		sourcesFile = "" // brew doesn't have a traditional sources file
		ui.Msgf(cfg, ui.LevelInfo, "brew doesn't use traditional sources files. Use 'brew tap' to add repositories.")
		return
	default:
		ui.Msgf(cfg, ui.LevelError, "edit-sources is not supported for %s", backend)
		os.Exit(1)
	}

	// Determine the editor to use
	editor = os.Getenv("EDITOR")
	if editor == "" {
		editor = os.Getenv("VISUAL")
	}
	if editor == "" {
		// Try common editors
		for _, e := range []string{"nano", "vim", "vi", "editor"} {
			if _, err := exec.LookPath(e); err == nil {
				editor = e
				break
			}
		}
	}

	if editor == "" {
		ui.Msg(cfg, ui.LevelError, "No editor found. Please set EDITOR or VISUAL environment variable.")
		os.Exit(1)
	}

	// Check if we need sudo
	needSudo := needsSudo(backend)
	if needSudo {
		if err := ensureSudo(); err != nil {
			ui.Msg(cfg, ui.LevelError, "privilege escalation failed")
			os.Exit(1)
		}
	}

	ui.Msgf(cfg, ui.LevelInfo, "Opening %s with %s...", sourcesFile, editor)

	var cmd *exec.Cmd
	var err error

	if needSudo {
		cmd, err = priv.Command(editor, sourcesFile)
	} else {
		cmd = exec.Command(editor, sourcesFile)
	}

	if err != nil {
		ui.Msgf(cfg, ui.LevelError, "%v", err)
		os.Exit(1)
	}

	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		ui.Msgf(cfg, ui.LevelError, "Failed to open editor: %v", err)
		os.Exit(1)
	}

	ui.Msg(cfg, ui.LevelOK, "Done.")
}

func runPacmanAutoremove(cfg *config.Config) {
	out, err := exec.Command("pacman", "-Qdtq").Output()
	if err != nil || strings.TrimSpace(string(out)) == "" {
		ui.Msg(cfg, ui.LevelInfo, "No orphaned packages found.")
		return
	}

	orphans := strings.Fields(strings.TrimSpace(string(out)))
	ui.Msgf(cfg, ui.LevelInfo, "Removing %d orphaned package(s): %s", len(orphans), strings.Join(orphans, " "))
	if !ui.Confirm() {
		ui.Msg(cfg, ui.LevelWarn, "Cancelled.")
		return
	}

	args := append([]string{"pacman", "-Rns", "--noconfirm"}, orphans...)
	cmd, err := priv.Command(args...)
	if err != nil {
		ui.Msgf(cfg, ui.LevelError, "%v", err)
		return
	}
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	if err := cmd.Run(); err != nil {
		ui.Msgf(cfg, ui.LevelError, "autoremove failed: %v", err)
		return
	}
	ui.Msg(cfg, ui.LevelOK, "Done.")
}

func runPacmanUpgrade(subcmd string, args []string, flags pack.Flags, cfg *config.Config) {
	// Invalidate sudo credentials before upgrade
	priv.Invalidate()

	pacmanArgs := []string{"pacman", "-Su"}
	if subcmd == "full-upgrade" {
		pacmanArgs = []string{"pacman", "-Syu"}
	}
	if !runWithBackendFlagsExt(pacmanArgs, args, cfg, "pacman", subcmd, flags) {
		return
	}

	if flags.NoConfirm {
		ui.Msg(cfg, ui.LevelInfo, "Skipping AUR upgrade because -y/--noconfirm was passed. Run without -y to upgrade AUR packages interactively.")
		return
	}

	// Use the new AUR backend for upgrade
	aurBackend := aurbackend.New(cfg)
	if err := aurBackend.Upgrade(nil, flags.DryRun); err != nil {
		// Non-fatal error - main upgrade succeeded, AUR upgrade failed
		ui.Msgf(cfg, ui.LevelWarn, "AUR upgrade skipped: %v", err)
	}
}

func runPacmanWithAURFallback(args []string, dryRun bool, cfg *config.Config) {
	if len(args) == 0 {
		ui.Msg(cfg, ui.LevelError, "Package name required")
		return
	}

	pkgs, flags := splitFlagsAll(args)
	noConfirm := flags.NoConfirm

	// Print warnings for unsupported flags
	for _, w := range pack.UnsupportedFlagWarnings("pacman", flags.NoConfirm, flags.Force) {
		ui.Msgf(cfg, ui.LevelWarn, "%s", w)
	}

	if dryRun {
		if pack.DryRunEmitted("pacman") {
			ui.Msgf(cfg, ui.LevelWarn, "DRY-RUN: showing package plan (no changes will be made)")
		} else {
			ui.Msgf(cfg, ui.LevelWarn, "DRY-RUN: no changes will be made")
		}
		fmt.Println()
	}
	ui.Msgf(cfg, ui.LevelInfo, "install %s(pacman -S %s)%s",
		cfg.Style.ColorDim,
		strings.Join(pkgs, " "),
		cfg.Style.ColorReset)
	fmt.Println()

	var filePkgs []string
	var namePkgs []string
	for _, p := range pkgs {
		if isFilePath(p) {
			filePkgs = append(filePkgs, p)
		} else {
			namePkgs = append(namePkgs, p)
		}
	}

	var notFound []string
	if len(namePkgs) > 0 {
		spArgs := append([]string{"-Sp"}, namePkgs...)
		var spStderr strings.Builder
		spCmd := exec.Command("pacman", spArgs...) // #nosec G702
		spCmd.Stdout = nil
		spCmd.Stderr = &spStderr
		spCmd.Env = append(os.Environ(), "LANG=C", "LC_ALL=C")
		spCmd.Run()
		notFound = parseNotFound(spStderr.String())
	}

	notFoundSet := make(map[string]bool, len(notFound))
	for _, p := range notFound {
		notFoundSet[p] = true
	}

	repoPkgs := append([]string{}, filePkgs...)
	for _, p := range namePkgs {
		if !notFoundSet[p] {
			repoPkgs = append(repoPkgs, p)
		}
	}

	if len(repoPkgs) > 0 {
		pacmanInstallRepoPkgs(repoPkgs, dryRun, noConfirm, flags.Force, cfg)
	}

	if len(notFound) > 0 {
		fmt.Println()
		ui.Msgf(cfg, ui.LevelWarn, "Not found in repo: %s", strings.Join(notFound, " "))
		ui.Msgf(cfg, ui.LevelInfo, "To install from AUR, run: alps aur install %s", strings.Join(notFound, " "))
	}
}

func pacmanInstallRepoPkgs(repoPkgs []string, dryRun, noConfirm, force bool, cfg *config.Config) {
	if !dryRun {
		if err := ensureSudo(); err != nil {
			ui.Msg(cfg, ui.LevelError, "sudo authentication failed")
			return
		}
	}
	pacmanArgs := append([]string{"-S"}, repoPkgs...)

	// Build extra flags using the flag pipeline
	flags := pack.Flags{NoConfirm: noConfirm, Force: force}
	extraFlags := pack.BuildExtraFlagsExt("pacman", flags)
	pacmanArgs = append(pacmanArgs, extraFlags...)

	// In dry-run mode, append the native simulation flag to show the package plan
	if dryRun {
		dryRunFlag := pack.GetDryRunFlag("pacman")
		if dryRunFlag != "" {
			pacmanArgs = append(pacmanArgs, dryRunFlag)
		}
	}

	var cmd *exec.Cmd
	var err error
	if dryRun {
		cmd = exec.Command("pacman", pacmanArgs...) // #nosec G702
	} else {
		cmd, err = priv.Command(append([]string{"pacman"}, pacmanArgs...)...)
	}
	if err != nil {
		ui.Msgf(cfg, ui.LevelError, "%v", err)
		return
	}
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	if err := cmd.Run(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
			ui.Msg(cfg, ui.LevelWarn, "Installation cancelled.")
		} else {
			ui.Msgf(cfg, ui.LevelError, "Installation failed: %v", err)
		}
	} else {
		ui.Msg(cfg, ui.LevelOK, "Done.")
	}
}

func isFilePath(s string) bool {
	return strings.HasPrefix(s, "./") ||
		strings.HasPrefix(s, "/") ||
		strings.HasSuffix(s, ".pkg.tar.zst") ||
		strings.HasSuffix(s, ".pkg.tar.xz") ||
		strings.HasSuffix(s, ".deb") ||
		strings.HasSuffix(s, ".rpm") ||
		strings.HasSuffix(s, ".apk")
}

func parseNotFound(stderr string) []string {
	var missing []string
	for _, line := range strings.Split(stderr, "\n") {
		line = strings.TrimSpace(line)
		const prefix = "error: target not found: "
		if strings.HasPrefix(line, prefix) {
			missing = append(missing, strings.TrimPrefix(line, prefix))
		}
	}
	return missing
}

func runPacmanSearch(args []string, cfg *config.Config) {
	if len(args) == 0 {
		ui.Msg(cfg, ui.LevelError, "Search query required")
		return
	}
	// Clean args to remove alps meta-flags before building search query
	pkgs, _, _ := pack.ParseFlags(args)
	query := strings.Join(pkgs, " ")

	type aurResult struct {
		pkgs []aur.Package
		err  error
	}
	aurCh := make(chan aurResult, 1)
	go func() {
		pkgs, err := aur.SearchNarrow(query)
		aurCh <- aurResult{pkgs, err}
	}()

	ui.Msgf(cfg, ui.LevelInfo, "Searching '%s' in repo...", query)
	fmt.Println()
	repoCmd := exec.Command("pacman", "-Ss", query) // #nosec G702
	repoCmd.Stdout = os.Stdout
	repoCmd.Stderr = os.Stderr

	repoCmd.Run()

	fmt.Println()
	ui.Msgf(cfg, ui.LevelInfo, "Searching '%s' in AUR...", query)
	fmt.Println()

	res := <-aurCh
	if res.err != nil {
		ui.Msgf(cfg, ui.LevelError, "AUR: %v", res.err)
		return
	}
	if len(res.pkgs) == 0 {
		ui.Msg(cfg, ui.LevelWarn, "No results found in AUR")
		return
	}

	for i, p := range res.pkgs {
		aur.PrintSearchResult(os.Stdout, i+1, p, "aur")
	}
	fmt.Println()
	appendAURNamesCache(res.pkgs)
}

func isArch() bool {
	return platform.IsArchBased()
}

func runAUR(args []string, cfg *config.Config) {
	ui.PrintHeader(cfg)

	if !isArch() {
		ui.Msg(cfg, ui.LevelError, "AUR is only available on Arch Linux")
		os.Exit(1)
	}

	if len(args) == 0 {
		ui.Msgf(cfg, ui.LevelError, "Usage: alps aur <%s> [args]", strings.Join(cli.ValidSubCmds("aur"), "|"))
		os.Exit(1)
	}

	rawSubcmd := args[0]
	subcmd, err := cli.ResolveSubCmd("aur", rawSubcmd, cfg)
	if err != nil {
		ui.Msgf(cfg, ui.LevelError, "%v", err)
		os.Exit(1)
	}
	rest := args[1:]
	pkgs, restFlags := pack.ParseFlagsExt(rest)
	dryRun := restFlags.DryRun
	if restFlags.NoConfirm {
		ui.Msg(cfg, ui.LevelError, "-y/--noconfirm is not supported for AUR operations. Run without -y so AUR prompts stay interactive.")
		os.Exit(1)
	}

	// Use the new AUR backend
	backend := aurbackend.New(cfg)

	switch subcmd {
	case "install", "update", "upgrade":
		if len(pkgs) == 0 {
			if subcmd == "install" {
				ui.Msg(cfg, ui.LevelError, "Usage: alps aur install <package> [packages...]")
				os.Exit(1)
			} else {
				// No packages provided for update/upgrade: upgrade all outdated AUR packages.
				if err := backend.Upgrade(pkgs, dryRun); err != nil {
					os.Exit(1)
				}
				return
			}
		}
		if dryRun {
			ui.Msgf(cfg, ui.LevelWarn, "DRY-RUN: would %s AUR package(s): %s", subcmd, strings.Join(pkgs, " "))
			fmt.Println()
			for _, pkg := range pkgs {
				if info, err2 := aur.Info(pkg); err2 == nil {
					aur.PrintPackageInfo(os.Stdout, info)
				} else {
					ui.Msgf(cfg, ui.LevelWarn, "%s: not found in AUR", pkg)
				}
			}
			return
		}
		if err := backend.Install(pkgs, dryRun); err != nil {
			os.Exit(1)
		}

	case "search":
		if len(rest) == 0 {
			ui.Msg(cfg, ui.LevelError, "Usage: alps aur search <query>")
			os.Exit(1)
		}
		query := strings.Join(pkgs, " ")
		if query == "" {
			query = strings.Join(rest, " ")
		}
		results, err := backend.Search(query)
		if err != nil {
			os.Exit(1)
		}
		appendAURNamesCache(results)

	case "list":
		if err := backend.List(); err != nil {
			os.Exit(1)
		}

	case "remove":
		if err := backend.Remove(pkgs, dryRun); err != nil {
			os.Exit(1)
		}

	case "build-local":
		dir := "."
		if len(pkgs) > 0 {
			dir = pkgs[0]
		}
		if err := backend.BuildLocal(dir, dryRun); err != nil {
			os.Exit(1)
		}

	case "fetch-abs":
		pkgName := ""
		if len(pkgs) > 0 {
			pkgName = pkgs[0]
		}
		if err := backend.FetchABS(pkgName); err != nil {
			os.Exit(1)
		}

	case "info":
		if len(pkgs) == 0 {
			ui.Msg(cfg, ui.LevelError, "Usage: alps aur info <package>")
			os.Exit(1)
		}
		for _, pkg := range pkgs {
			if info, err := aur.Info(pkg); err != nil {
				ui.Msgf(cfg, ui.LevelError, "%s: not found in AUR", pkg)
			} else {
				aur.PrintPackageInfo(os.Stdout, info)
			}
		}

	case "clone":
		if len(pkgs) == 0 {
			ui.Msg(cfg, ui.LevelError, "Usage: alps aur clone <package>")
			os.Exit(1)
		}
		if err := backend.Clone(pkgs[0]); err != nil {
			os.Exit(1)
		}

	case "orphans":
		if err := backend.Orphans(); err != nil {
			os.Exit(1)
		}

	case "clean":
		if err := backend.Clean(dryRun); err != nil {
			os.Exit(1)
		}

	default:
		ui.Msgf(cfg, ui.LevelError, "Unknown aur subcommand: %s", subcmd)
		os.Exit(1)
	}
}

func appendAURNamesCache(pkgs []aur.Package) {
	path := completion.AURNamesCachePath()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return
	}

	names := make(map[string]bool)
	for _, p := range pkgs {
		names[p.Name] = true
	}
	if data, err := os.ReadFile(path); err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			if line = strings.TrimSpace(line); line != "" {
				names[line] = true
			}
		}
	}
	merged := make([]string, 0, len(names))
	for name := range names {
		merged = append(merged, name)
	}
	sort.Strings(merged)

	// Write to a temp file in the same directory, then rename, so readers of
	// the cache never see a half-written file.
	tmp, err := os.CreateTemp(filepath.Dir(path), ".aur-names-*")
	if err != nil {
		return
	}
	tmpPath := tmp.Name()
	ok := false
	defer func() {
		tmp.Close()
		if !ok {
			os.Remove(tmpPath)
		}
	}()
	for _, name := range merged {
		if _, err := fmt.Fprintln(tmp, name); err != nil {
			return
		}
	}
	if err := tmp.Close(); err != nil {
		return
	}
	if err := os.Chmod(tmpPath, 0644); err != nil {
		return
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return
	}
	ok = true
}

func runRepo(args []string, cfg *config.Config) {
	ui.PrintHeader(cfg)

	if len(args) == 0 {
		ui.Msg(cfg, ui.LevelError, "Usage: alps repo <update|list|install|remove|purge|search|upgrade> [package]")
		os.Exit(1)
	}

	rawSubcmd := args[0]
	subcmd, err := cli.ResolveSubCmd("repo", rawSubcmd, cfg)
	if err != nil {
		ui.Msgf(cfg, ui.LevelError, "%v", err)
		os.Exit(1)
	}
	rest := args[1:]
	pkgs, restFlags := pack.ParseFlagsExt(rest)
	dryRun := restFlags.DryRun

	if restFlags.NoConfirm {
		ui.Msg(cfg, ui.LevelError, "-y/--noconfirm is not supported for alps repo operations. Run without -y so prompts stay interactive.")
		os.Exit(1)
	}

	// Use the new repo backend
	backend := repo.New(cfg)

	switch subcmd {

	case "update":
		if err := backend.Update(dryRun); err != nil {
			os.Exit(1)
		}

	case "list":
		if err := backend.List(pkgs); err != nil {
			os.Exit(1)
		}

	case "install":
		if err := backend.Install(pkgs, dryRun); err != nil {
			os.Exit(1)
		}

	case "remove":
		if err := backend.Remove(pkgs, dryRun); err != nil {
			os.Exit(1)
		}

	case "purge":
		if err := backend.Purge(pkgs, dryRun); err != nil {
			os.Exit(1)
		}

	case "search":
		query := strings.Join(pkgs, " ")
		if query == "" {
			query = strings.Join(rest, " ")
		}
		if err := backend.Search(query); err != nil {
			os.Exit(1)
		}

	case "upgrade":
		if err := backend.Upgrade(pkgs, dryRun); err != nil {
			os.Exit(1)
		}

	case "clean":
		if err := backend.Clean(dryRun); err != nil {
			os.Exit(1)
		}

	default:
		ui.Msgf(cfg, ui.LevelError, "Unknown repo subcommand: %s", subcmd)
		os.Exit(1)
	}
}

func runFlatpak(args []string, cfg *config.Config) {
	ui.PrintHeader(cfg)

	if !extra.IsAvailable("flatpak") {
		ui.Msg(cfg, ui.LevelError, "flatpak is not installed")
		os.Exit(1)
	}

	if len(args) == 0 {
		ui.Msgf(cfg, ui.LevelError, "Usage: alps flatpak <%s> [args]", strings.Join(cli.ValidSubCmds("flatpak"), "|"))
		os.Exit(1)
	}

	rawSubcmd := args[0]
	subcmd, err := cli.ResolveSubCmd("flatpak", rawSubcmd, cfg)
	if err != nil {
		ui.Msgf(cfg, ui.LevelError, "%v", err)
		os.Exit(1)
	}
	rest := args[1:]
	pkgs, restFlags := pack.ParseFlagsExt(rest)
	warnUnsupportedExtraFlags("flatpak", restFlags, cfg)

	switch subcmd {
	case "install":
		extraBackendInstall("flatpak", pkgs, restFlags, cfg)
	case "remove":
		extraBackendRemove("flatpak", pkgs, restFlags, cfg)
	case "purge":
		extraBackendPurge("flatpak", pkgs, restFlags, cfg)
	case "search":
		extraBackendSearch("flatpak", rest, pkgs, restFlags, cfg)
	case "show":
		extraBackendShow("flatpak", pkgs, restFlags, cfg)
	case "list":
		extraBackendList("flatpak", cfg)
	case "update":
		extraBackendUpdate("flatpak", restFlags, cfg)
	case "upgrade":
		extraBackendUpgrade("flatpak", restFlags, cfg)
	case "autoremove":
		extraBackendAutoremove("flatpak", restFlags, cfg)
	case "clean":
		extraBackendClean("flatpak", restFlags, cfg)
	default:
		ui.Msgf(cfg, ui.LevelError, "Unknown flatpak subcommand: %s", subcmd)
		os.Exit(1)
	}
}

// extraBackendInstall installs via a container-style backend, honouring the
// alps meta-flags in f (e.g. flatpak -y) instead of dropping them silently.
func extraBackendInstall(backendName string, pkgs []string, f pack.Flags, cfg *config.Config) {
	if len(pkgs) == 0 {
		ui.Msgf(cfg, ui.LevelError, "Usage: alps %s install <package>", backendName)
		os.Exit(1)
	}
	if f.DryRun {
		ui.Msgf(cfg, ui.LevelWarn, "DRY-RUN: would install %s package(s): %s", backendName, strings.Join(pkgs, " "))
		return
	}
	if err := extra.Install(backendName, pkgs, false, f); err != nil {
		ui.Msgf(cfg, ui.LevelError, "%v", err)
		os.Exit(1)
	}
	ui.Msg(cfg, ui.LevelOK, "Done.")
}

func extraBackendRemove(backendName string, pkgs []string, f pack.Flags, cfg *config.Config) {
	if len(pkgs) == 0 {
		ui.Msgf(cfg, ui.LevelError, "Usage: alps %s remove <package>", backendName)
		os.Exit(1)
	}
	if f.DryRun {
		ui.Msgf(cfg, ui.LevelWarn, "DRY-RUN: would remove %s package: %s", backendName, pkgs[0])
		return
	}
	if err := extra.Remove(backendName, pkgs[0], f); err != nil {
		ui.Msgf(cfg, ui.LevelError, "%v", err)
		os.Exit(1)
	}
	ui.Msg(cfg, ui.LevelOK, "Done.")
}

func extraBackendPurge(backendName string, pkgs []string, f pack.Flags, cfg *config.Config) {
	if len(pkgs) == 0 {
		ui.Msgf(cfg, ui.LevelError, "Usage: alps %s purge <package>", backendName)
		os.Exit(1)
	}
	if f.DryRun {
		ui.Msgf(cfg, ui.LevelWarn, "DRY-RUN: would purge %s package: %s", backendName, pkgs[0])
		return
	}
	if err := extra.Purge(backendName, pkgs[0], f); err != nil {
		ui.Msgf(cfg, ui.LevelError, "%v", err)
		os.Exit(1)
	}
	ui.Msg(cfg, ui.LevelOK, "Done.")
}

func extraBackendSearch(backendName string, rest, pkgs []string, f pack.Flags, cfg *config.Config) {
	if len(rest) == 0 {
		ui.Msgf(cfg, ui.LevelError, "Usage: alps %s search <query>", backendName)
		os.Exit(1)
	}
	if err := extra.Search(backendName, strings.Join(pkgs, " "), f); err != nil {
		ui.Msgf(cfg, ui.LevelError, "%v", err)
		os.Exit(1)
	}
}

func extraBackendShow(backendName string, pkgs []string, f pack.Flags, cfg *config.Config) {
	if len(pkgs) == 0 {
		ui.Msgf(cfg, ui.LevelError, "Usage: alps %s show <package>", backendName)
		os.Exit(1)
	}
	if err := extra.Show(backendName, pkgs[0], f); err != nil {
		ui.Msgf(cfg, ui.LevelError, "%v", err)
		os.Exit(1)
	}
}

func extraBackendList(backendName string, cfg *config.Config) {
	if err := extra.List(backendName); err != nil {
		ui.Msgf(cfg, ui.LevelError, "%v", err)
		os.Exit(1)
	}
}

func extraBackendUpdate(backendName string, f pack.Flags, cfg *config.Config) {
	if f.DryRun {
		ui.Msgf(cfg, ui.LevelWarn, "DRY-RUN: would update all %s packages", backendName)
		return
	}
	if err := extra.Update(backendName, f); err != nil {
		ui.Msgf(cfg, ui.LevelError, "%v", err)
		os.Exit(1)
	}
	ui.Msg(cfg, ui.LevelOK, "Done.")
}

func extraBackendUpgrade(backendName string, f pack.Flags, cfg *config.Config) {
	if f.DryRun {
		ui.Msgf(cfg, ui.LevelWarn, "DRY-RUN: would upgrade all %s packages", backendName)
		return
	}
	if err := extra.Upgrade(backendName, f); err != nil {
		ui.Msgf(cfg, ui.LevelError, "%v", err)
		os.Exit(1)
	}
	ui.Msg(cfg, ui.LevelOK, "Done.")
}

func extraBackendAutoremove(backendName string, f pack.Flags, cfg *config.Config) {
	if f.DryRun {
		ui.Msgf(cfg, ui.LevelWarn, "DRY-RUN: would autoremove unused %s packages", backendName)
		return
	}
	if err := extra.Autoremove(backendName, f); err != nil {
		ui.Msgf(cfg, ui.LevelError, "%v", err)
		os.Exit(1)
	}
	ui.Msg(cfg, ui.LevelOK, "Done.")
}

func extraBackendClean(backendName string, f pack.Flags, cfg *config.Config) {
	if f.DryRun {
		ui.Msgf(cfg, ui.LevelWarn, "DRY-RUN: would clean %s package cache", backendName)
		return
	}
	if err := extra.Clean(backendName, f); err != nil {
		ui.Msgf(cfg, ui.LevelError, "%v", err)
		os.Exit(1)
	}
	ui.Msg(cfg, ui.LevelOK, "Done.")
}

// warnUnsupportedExtraFlags reports alps flags the container-style backends
// cannot honour so they are never dropped silently.
func warnUnsupportedExtraFlags(backendName string, f pack.Flags, cfg *config.Config) {
	for _, w := range extra.UnsupportedFlagWarnings(backendName, f.NoConfirm, f.Force) {
		ui.Msgf(cfg, ui.LevelWarn, "%s", w)
	}
}

func runSnap(args []string, cfg *config.Config) {
	ui.PrintHeader(cfg)

	if !extra.IsAvailable("snap") {
		ui.Msg(cfg, ui.LevelError, "snap is not available (not installed or blocked)")
		os.Exit(1)
	}

	if len(args) == 0 {
		ui.Msgf(cfg, ui.LevelError, "Usage: alps snap <%s> [args]", strings.Join(cli.ValidSubCmds("snap"), "|"))
		os.Exit(1)
	}

	rawSubcmd := args[0]
	subcmd, err := cli.ResolveSubCmd("snap", rawSubcmd, cfg)
	if err != nil {
		ui.Msgf(cfg, ui.LevelError, "%v", err)
		os.Exit(1)
	}
	rest := args[1:]
	pkgs, restFlags := pack.ParseFlagsExt(rest)
	warnUnsupportedExtraFlags("snap", restFlags, cfg)

	switch subcmd {
	case "install":
		extraBackendInstall("snap", pkgs, restFlags, cfg)
	case "remove":
		extraBackendRemove("snap", pkgs, restFlags, cfg)
	case "purge":
		extraBackendPurge("snap", pkgs, restFlags, cfg)
	case "search":
		extraBackendSearch("snap", rest, pkgs, restFlags, cfg)
	case "show":
		extraBackendShow("snap", pkgs, restFlags, cfg)
	case "list":
		extraBackendList("snap", cfg)
	case "update":
		extraBackendUpdate("snap", restFlags, cfg)
	case "upgrade":
		extraBackendUpgrade("snap", restFlags, cfg)
	case "autoremove":
		extraBackendAutoremove("snap", restFlags, cfg)
	case "clean":
		extraBackendClean("snap", restFlags, cfg)
	default:
		ui.Msgf(cfg, ui.LevelError, "Unknown snap subcommand: %s", subcmd)
		os.Exit(1)
	}
}

func runWinget(args []string, cfg *config.Config) {
	ui.PrintHeader(cfg)

	if !extra.IsAvailable("winget") {
		ui.Msg(cfg, ui.LevelError, "winget is not available (WSL only)")
		os.Exit(1)
	}

	if len(args) == 0 {
		ui.Msgf(cfg, ui.LevelError, "Usage: alps winget <%s> [args]", strings.Join(cli.ValidSubCmds("winget"), "|"))
		os.Exit(1)
	}

	rawSubcmd := args[0]
	subcmd, err := cli.ResolveSubCmd("winget", rawSubcmd, cfg)
	if err != nil {
		ui.Msgf(cfg, ui.LevelError, "%v", err)
		os.Exit(1)
	}
	rest := args[1:]
	pkgs, restFlags := pack.ParseFlagsExt(rest)
	warnUnsupportedExtraFlags("winget", restFlags, cfg)

	switch subcmd {
	case "install":
		extraBackendInstall("winget", pkgs, restFlags, cfg)
	case "remove":
		extraBackendRemove("winget", pkgs, restFlags, cfg)
	case "purge":
		extraBackendPurge("winget", pkgs, restFlags, cfg)
	case "search":
		extraBackendSearch("winget", rest, pkgs, restFlags, cfg)
	case "show":
		extraBackendShow("winget", pkgs, restFlags, cfg)
	case "list":
		extraBackendList("winget", cfg)
	case "update":
		extraBackendUpdate("winget", restFlags, cfg)
	case "upgrade":
		extraBackendUpgrade("winget", restFlags, cfg)
	default:
		ui.Msgf(cfg, ui.LevelError, "Unknown winget subcommand: %s", subcmd)
		os.Exit(1)
	}
}

func runAptWithSnapFallback(args []string, dryRun bool, cfg *config.Config) {
	if len(args) == 0 {
		ui.Msg(cfg, ui.LevelError, "Package name required")
		return
	}

	pkgs, flags := splitFlagsAll(args)
	noConfirm := flags.NoConfirm
	realBackend := pack.DetectRealApt()

	// Print warnings for unsupported flags
	for _, w := range pack.UnsupportedFlagWarnings(realBackend, flags.NoConfirm, flags.Force) {
		ui.Msgf(cfg, ui.LevelWarn, "%s", w)
	}

	if dryRun {
		if pack.DryRunEmitted(realBackend) {
			ui.Msgf(cfg, ui.LevelWarn, "DRY-RUN: showing package plan (no changes will be made)")
		} else {
			ui.Msgf(cfg, ui.LevelWarn, "DRY-RUN: no changes will be made")
		}
		fmt.Println()
	}
	ui.Msgf(cfg, ui.LevelInfo, "install (%s install %s)", realBackend, strings.Join(pkgs, " "))
	fmt.Println()

	var notFound []string
	var repoPkgs []string
	for _, pkg := range pkgs {
		if isFilePath(pkg) {
			repoPkgs = append(repoPkgs, pkg)
			continue
		}
		chkCmd := "apt-cache"
		if _, err := exec.LookPath("apt-cache"); err != nil {
			chkCmd = ""
		}
		if chkCmd != "" {
			chk := exec.Command(chkCmd, "show", pkg) // #nosec G702
			chk.Stdout = nil
			chk.Stderr = nil
			if chk.Run() != nil {
				notFound = append(notFound, pkg)
				continue
			}
		}
		repoPkgs = append(repoPkgs, pkg)
	}

	if !dryRun {
		if err := ensureSudo(); err != nil {
			ui.Msg(cfg, ui.LevelError, "privilege escalation failed")
			return
		}
	}

	if len(repoPkgs) > 0 {
		aptArgs := append([]string{realBackend, "install"}, repoPkgs...)

		// Build extra flags using the flag pipeline
		installFlags := pack.Flags{NoConfirm: noConfirm, Force: flags.Force}
		extraFlags := pack.BuildExtraFlagsExt(realBackend, installFlags)
		aptArgs = append(aptArgs, extraFlags...)

		// In dry-run mode, append the native simulation flag to show the package plan
		if dryRun {
			dryRunFlag := pack.GetDryRunFlag(realBackend)
			if dryRunFlag != "" {
				aptArgs = append(aptArgs, dryRunFlag)
			}
		}

		var cmd *exec.Cmd
		var err error
		if dryRun {
			cmd = exec.Command(aptArgs[0], aptArgs[1:]...) // #nosec G702
		} else {
			cmd, err = priv.Command(aptArgs...)
		}
		if err != nil {
			ui.Msgf(cfg, ui.LevelError, "%v", err)
			return
		}
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		cmd.Stdin = os.Stdin
		if err := cmd.Run(); err != nil {
			if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
				ui.Msg(cfg, ui.LevelWarn, "Installation cancelled.")
			} else {
				ui.Msgf(cfg, ui.LevelError, "Installation failed: %v", err)
			}
		} else {
			ui.Msg(cfg, ui.LevelOK, "Done.")
		}
	}

	if len(notFound) > 0 && extra.IsAvailable("snap") {
		fmt.Println()
		ui.Msgf(cfg, ui.LevelWarn, "Not found in apt: %s", strings.Join(notFound, " "))
		ui.Msgf(cfg, ui.LevelInfo, "Try snap for %s%s%s?",
			cfg.Style.ColorBold, strings.Join(notFound, " "), cfg.Style.ColorReset+cfg.Style.ColorInfo)
		fmt.Print(cfg.Style.ColorReset)
		if dryRun {
			ui.Msgf(cfg, ui.LevelWarn, "DRY-RUN: would prompt to install snap package(s): %s", strings.Join(notFound, " "))
		} else if ui.Confirm() {
			if err := extra.Install("snap", notFound, false, pack.Flags{}); err != nil {
				ui.Msgf(cfg, ui.LevelError, "%v", err)
			} else {
				ui.Msg(cfg, ui.LevelOK, "Done.")
			}
		} else {
			ui.Msg(cfg, ui.LevelWarn, "Skipped.")
		}
	} else if len(notFound) > 0 {
		ui.Msgf(cfg, ui.LevelWarn, "Not found in apt: %s", strings.Join(notFound, " "))
	}
}

func runAptSearch(args []string, cfg *config.Config) {
	if len(args) == 0 {
		ui.Msg(cfg, ui.LevelError, "Search query required")
		return
	}
	// Clean args to remove alps meta-flags before building search query
	pkgs, _, _ := pack.ParseFlags(args)
	query := strings.Join(pkgs, " ")
	realBackend := pack.DetectRealApt()

	type snapDone struct{ err error }
	snapCh := make(chan snapDone, 1)
	snapEnabled := extra.IsAvailable("snap")
	if snapEnabled {
		go func() {
			snapCh <- snapDone{extra.Search("snap", query, pack.Flags{})}
		}()
	}

	ui.Msgf(cfg, ui.LevelInfo, "Searching '%s' in apt...", query)
	fmt.Println()
	aptCmd := exec.Command(realBackend, "search", query) // #nosec G702
	aptCmd.Stdout = os.Stdout
	aptCmd.Stderr = os.Stderr
	aptCmd.Run()

	if snapEnabled {
		fmt.Println()
		ui.Msgf(cfg, ui.LevelInfo, "Searching '%s' in snap...", query)
		fmt.Println()
		<-snapCh
	}
}

// runAptFullUpgrade runs apt update followed by apt upgrade instead of apt full-upgrade.
func runAptFullUpgrade(args []string, f pack.Flags, cfg *config.Config) {
	dryRun := f.DryRun
	realBackend := pack.DetectRealApt()

	ui.Msgf(cfg, ui.LevelInfo, "full-upgrade %s(%s update && %s upgrade)%s",
		cfg.Style.ColorDim,
		realBackend,
		realBackend,
		cfg.Style.ColorReset)
	fmt.Println()

	if dryRun {
		ui.Msgf(cfg, ui.LevelWarn, "DRY-RUN: no changes will be made")
		return
	}

	// apt needs root for both update and upgrade
	if !platform.IsTermux() {
		if err := ensureSudo(); err != nil {
			ui.Msg(cfg, ui.LevelError, "privilege escalation failed")
			return
		}
	}

	runAptStep := func(stepArgs ...string) error {
		cmd, err := priv.Command(stepArgs...)
		if err != nil {
			return err
		}
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		cmd.Stdin = os.Stdin
		return cmd.Run()
	}

	// Step 1: apt update
	if err := runAptStep(realBackend, "update"); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
			ui.Msg(cfg, ui.LevelWarn, "Update cancelled.")
		} else {
			ui.Msgf(cfg, ui.LevelError, "apt update failed: %v", err)
		}
		return
	}
	ui.Msg(cfg, ui.LevelOK, "Update done.")
	fmt.Println()

	// Step 2: apt upgrade
	upgradeArgs := []string{realBackend, "upgrade"}

	// Build extra flags using the flag pipeline
	extraFlags := pack.BuildExtraFlagsExt(realBackend, f)
	upgradeArgs = append(upgradeArgs, extraFlags...)

	if err := runAptStep(upgradeArgs...); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
			ui.Msg(cfg, ui.LevelWarn, "Upgrade cancelled.")
		} else {
			ui.Msgf(cfg, ui.LevelError, "apt upgrade failed: %v", err)
		}
		return
	}
	ui.Msg(cfg, ui.LevelOK, "Done.")
}
