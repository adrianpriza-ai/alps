package main

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/adrianpriza-ai/alps/aur"
	"github.com/adrianpriza-ai/alps/completion"
	"github.com/adrianpriza-ai/alps/config"
	"github.com/adrianpriza-ai/alps/flatpak"
	"github.com/adrianpriza-ai/alps/more"
	"github.com/adrianpriza-ai/alps/pack"
	"github.com/adrianpriza-ai/alps/priv"
	"github.com/adrianpriza-ai/alps/snap"
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

	cmd := os.Args[1]
	args := os.Args[2:]

	switch cmd {
	case "version", "--version":
		fmt.Printf("%salps%s %s by %s\n", cfg.Style.ColorPrimary, cfg.Style.ColorReset, Version, author)
	case "completion":
		if len(args) == 0 {
			fmt.Fprintln(os.Stderr, "Usage: alps completion <fish|bash|zsh>")
			os.Exit(1)
		}
		completion.Generate(args[0])
	case "help", "--help", "-h":
		ui.PrintHelp(cfg)
	case "aliases":
		ui.PrintAliases(cfg)
	case "config-show":
		ui.PrintConfigShow(cfg)
	case "repo":
		runRepo(args, cfg)
	case "aur":
		runAUR(args, cfg)
	case "flatpak":
		runFlatpak(args, cfg)
	case "snap":
		runSnap(args, cfg)
	default:
		resolved, err := resolveCmd(cmd, cfg)
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
	case "flatpak":
		runFlatpak(args, cfg)
	case "snap":
		runSnap(args, cfg)
	default:
		runPkg(resolved, args, cfg)
	}
}

var hardCommands = map[string]bool{
	"help": true, "--help": true, "-h": true,
	"version": true, "--version": true,
	"aliases": true, "config-show": true, "completion": true,
	"repo": true, "aur": true, "flatpak": true, "snap": true,
	"install": true, "remove": true, "purge": true,
	"update": true, "upgrade": true, "full-upgrade": true,
	"search": true, "show": true, "list": true,
	"autoremove": true, "autoclean": true, "clean": true,
	"edit-sources": true,
}

func resolveCmd(cmd string, cfg *config.Config) (string, error) {
	if hardCommands[cmd] {
		return cmd, nil
	}
	if v, ok := cfg.ConfigAliases[cmd]; ok {
		return v, nil
	}
	if v, ok := config.DefaultAliases[cmd]; ok {
		return v, nil
	}
	return "", fmt.Errorf("unknown command %q — run 'alps help' for available commands", cmd)
}

var validSubCmds = map[string]map[string]bool{
	"aur": {
		"install": true, "search": true, "list": true,
		"remove": true, "clean": true, "build-local": true, "fetch-abs": true,
		"update": true, "upgrade": true,
	},
	"repo": {
		"update": true, "list": true, "install": true,
		"remove": true, "purge": true, "search": true, "upgrade": true, "clean": true,
	},
	"flatpak": {
		"install": true, "remove": true, "search": true, "list": true, "update": true,
	},
	"snap": {
		"install": true, "remove": true, "search": true, "list": true, "update": true,
	},
}

func resolveSubCmd(system, subcmd string, cfg *config.Config) (string, error) {
	valid := validSubCmds[system]

	if valid[subcmd] {
		return subcmd, nil
	}
	if v, ok := cfg.ConfigAliases[subcmd]; ok {
		if valid[v] {
			return v, nil
		}
	}
	if v, ok := config.DefaultAliases[subcmd]; ok {
		if valid[v] {
			return v, nil
		}
	}
	if v, ok := config.DefaultSubCmdAliases[subcmd]; ok {
		if valid[v] {
			return v, nil
		}
	}

	names := make([]string, 0, len(valid))
	for k := range valid {
		names = append(names, k)
	}
	sort.Strings(names)
	return "", fmt.Errorf("unknown %s subcommand %q\n  valid: %s", system, subcmd, strings.Join(names, ", "))
}

// resolveListAction resolves repo list sub-actions using 3-tier alias chain.
func resolveListAction(action string, cfg *config.Config) string {
	// Tier 1: exact match
	if action == "install" || action == "remove" {
		return action
	}
	// Tier 2: config alias
	if v, ok := cfg.ConfigAliases[action]; ok {
		if v == "install" || v == "remove" {
			return v
		}
	}
	// Tier 3: default aliases
	if v, ok := config.DefaultAliases[action]; ok {
		if v == "install" || v == "remove" {
			return v
		}
	}
	if v, ok := config.DefaultSubCmdAliases[action]; ok {
		if v == "install" || v == "remove" {
			return v
		}
	}
	return ""
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

func readLine() string {
	reader := bufio.NewReader(os.Stdin)
	line, _ := reader.ReadString('\n')
	return strings.TrimSpace(line)
}

func runPkg(subcmd string, args []string, cfg *config.Config) {
	backend := detectBackend()
	ui.PrintHeader(cfg)

	if backend == "" {
		ui.Msg(cfg, ui.LevelError, "No supported package manager found (apt/dnf/pacman/zypper/apk)")
		os.Exit(1)
	}

	_, flags := splitFlagsAll(args)
	dryRun := flags.DryRun

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
		default:
			runPkgDefault(backend, subcmd, args, flags, cfg)
		}
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
	fmt.Print("  Continue anyway? [y/N] ")
	return strings.ToLower(readLine()) == "y"
}

func runPkgDefault(backend, subcmd string, args []string, flags pack.Flags, cfg *config.Config) {
	realBackend := detectRealBackend()
	mapped, ok := pack.Lookup(backend, subcmd)
	if !ok {
		mapped = []string{realBackend, subcmd}
	} else {
		tmp := make([]string, len(mapped))
		copy(tmp, mapped)
		mapped = tmp
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

	fullArgs := make([]string, len(cmdArgs[1:]))
	copy(fullArgs, cmdArgs[1:])
	fullArgs = append(fullArgs, cleanArgs...)

	display := fmtCmd(cmdArgs, cleanArgs)
	if f.DryRun {
		ui.Msgf(cfg, ui.LevelWarn, "DRY-RUN: no changes will be made")
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

	var cmd *exec.Cmd
	if sudo && !f.DryRun {
		var err error
		cmd, err = priv.Command(append([]string{cmdArgs[0]}, fullArgs...)...)
		if err != nil {
			ui.Msgf(cfg, ui.LevelError, "%v", err)
			return false
		}
	} else {
		cmd = exec.Command(cmdArgs[0], fullArgs...) // #nosec G702
	}
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin

	if err := cmd.Run(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			ui.Msgf(cfg, ui.LevelError, "%s exited with code %d", backend, exitErr.ExitCode())
			return false
		}
		ui.Msgf(cfg, ui.LevelError, "%v", err)
		return false
	}
	ui.Msg(cfg, ui.LevelOK, "Done.")
	return true
}

func ensureSudo() error {
	return priv.Ensure()
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

	runAURUpgrade(flags.DryRun, cfg)
}

func runPacmanWithAURFallback(args []string, dryRun bool, cfg *config.Config) {
	if len(args) == 0 {
		ui.Msg(cfg, ui.LevelError, "Package name required")
		return
	}

	pkgs, noConfirm := splitFlags(args)

	if dryRun {
		ui.Msgf(cfg, ui.LevelWarn, "DRY-RUN: no changes will be made")
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
		pacmanInstallRepoPkgs(repoPkgs, dryRun, noConfirm, cfg)
	}

	if len(notFound) > 0 {
		fmt.Println()
		ui.Msgf(cfg, ui.LevelWarn, "Not found in repo: %s", strings.Join(notFound, " "))
		ui.Msgf(cfg, ui.LevelInfo, "To install from AUR, run: alps aur install %s", strings.Join(notFound, " "))
	}
}

func pacmanInstallRepoPkgs(repoPkgs []string, dryRun, noConfirm bool, cfg *config.Config) {
	if !dryRun {
		if err := ensureSudo(); err != nil {
			ui.Msg(cfg, ui.LevelError, "sudo authentication failed")
			return
		}
	}
	pacmanArgs := append([]string{"-S"}, repoPkgs...)
	if noConfirm {
		pacmanArgs = append(pacmanArgs, "--noconfirm")
	}
	if dryRun {
		pacmanArgs = append(pacmanArgs, pack.GetDryRunFlag("pacman"))
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
	query := strings.Join(args, " ")

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
		aur.PrintSearchResult(i+1, p, "aur")
	}
	fmt.Println()
	appendAURNamesCache(res.pkgs)
}

func runAURUpgrade(dryRun bool, cfg *config.Config) {
	installed, err := aur.GetInstalledAUR()
	if err != nil {
		ui.Msgf(cfg, ui.LevelError, "failed to list AUR packages: %v", err)
		return
	}
	if len(installed) == 0 {
		ui.Msg(cfg, ui.LevelInfo, "No AUR packages installed.")
		return
	}

	ui.Msgf(cfg, ui.LevelInfo, "Checking %d AUR package(s) for updates...", len(installed))

	names := make([]string, 0, len(installed))
	for name := range installed {
		names = append(names, name)
	}
	latest, err := aur.InfoBatch(names)
	if err != nil {
		ui.Msgf(cfg, ui.LevelError, "failed to check for AUR updates: %v", err)
		return
	}

	pacConf, _ := aur.ReadPacmanConf()
	ignoreSet := make(map[string]bool)
	if pacConf != nil {
		for _, pkg := range pacConf.IgnorePkg {
			ignoreSet[pkg] = true
		}
	}

	var outdated []aur.Package
	for name, installedVer := range installed {
		pkg, ok := latest[name]
		if !ok {
			continue
		}
		if pkg.Version != installedVer {
			if ignoreSet[name] {
				fmt.Printf("  %s  %s: ignoring package upgrade (%s => %s)\n",
					cfg.Style.SymArrow, pkg.Name, installedVer, pkg.Version)
				continue
			}
			outdated = append(outdated, *pkg)
			fmt.Printf("  %s%s%s  %s%s%s → %s%s%s\n",
				cfg.Style.ColorPrimary, pkg.Name, cfg.Style.ColorReset,
				cfg.Style.ColorDim, installedVer, cfg.Style.ColorReset,
				cfg.Style.ColorSuccess, pkg.Version, cfg.Style.ColorReset)
		}
	}

	if len(outdated) == 0 {
		ui.Msg(cfg, ui.LevelOK, "All AUR packages are up to date.")
		return
	}

	fmt.Println()
	ui.Msgf(cfg, ui.LevelInfo, "%d AUR package(s) to upgrade.", len(outdated))
	fmt.Println()

	var toUpgrade []string
	for _, pkg := range outdated {
		if dryRun {
			ui.Msgf(cfg, ui.LevelWarn, "DRY-RUN: would upgrade AUR package %s", pkg.Name)
		} else {
			toUpgrade = append(toUpgrade, pkg.Name)
		}
	}

	if dryRun || len(toUpgrade) == 0 {
		return
	}

	if err := aur.Install(toUpgrade, false); err != nil {
		ui.Msgf(cfg, ui.LevelError, "AUR upgrade failed: %v", err)
	} else {
		ui.Msg(cfg, ui.LevelOK, "AUR upgrade complete.")
	}
}

func isArch() bool {
	_, err := exec.LookPath("pacman")
	return err == nil
}

func runAUR(args []string, cfg *config.Config) {
	ui.PrintHeader(cfg)

	if !isArch() {
		ui.Msg(cfg, ui.LevelError, "AUR is only available on Arch Linux")
		os.Exit(1)
	}

	if len(args) == 0 {
		ui.Msg(cfg, ui.LevelError, "Usage: alps aur <install|search|list|remove|clean|build-local|fetch-abs> [args]")
		os.Exit(1)
	}

	rawSubcmd := args[0]
	subcmd, err := resolveSubCmd("aur", rawSubcmd, cfg)
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

	switch subcmd {
	case "install", "update", "upgrade":
		if len(pkgs) == 0 {
			if subcmd == "install" {
				ui.Msg(cfg, ui.LevelError, "Usage: alps aur install <package> [packages...]")
				os.Exit(1)
			} else {
				// No packages provided for update/upgrade: upgrade all outdated AUR packages.
				runAURUpgrade(dryRun, cfg)
				return
			}
		}
		if dryRun {
			ui.Msgf(cfg, ui.LevelWarn, "DRY-RUN: would %s AUR package(s): %s", subcmd, strings.Join(pkgs, " "))
			fmt.Println()
			for _, pkg := range pkgs {
				if info, err2 := aur.Info(pkg); err2 == nil {
					aur.PrintPackageInfo(info)
				} else {
					ui.Msgf(cfg, ui.LevelWarn, "%s: not found in AUR", pkg)
				}
			}
			return
		}
		// noConfirm intentionally omitted — user must confirm AUR installs.
		if err := aur.Install(pkgs, false); err != nil {
			ui.Msgf(cfg, ui.LevelError, "%v", err)
			os.Exit(1)
		}
		ui.Msg(cfg, ui.LevelOK, "Done.")

	case "search":
		if len(rest) == 0 {
			ui.Msg(cfg, ui.LevelError, "Usage: alps aur search <query>")
			os.Exit(1)
		}
		query := strings.Join(pkgs, " ")
		if query == "" {
			query = strings.Join(rest, " ")
		}
		ui.Msgf(cfg, ui.LevelInfo, "Searching '%s' in AUR...", query)
		fmt.Println()
		results, err := aur.SearchNarrow(query)
		if err != nil {
			ui.Msgf(cfg, ui.LevelError, "%v", err)
			os.Exit(1)
		}
		if len(results) == 0 {
			ui.Msg(cfg, ui.LevelWarn, "No results found in AUR")
			return
		}
		for i, p := range results {
			aur.PrintSearchResult(i+1, p, "aur")
		}
		fmt.Println()
		appendAURNamesCache(results)

	case "list":
		installed, err := aur.ListInstalledAUR()
		if err != nil {
			ui.Msgf(cfg, ui.LevelError, "%v", err)
			os.Exit(1)
		}
		if len(installed) == 0 {
			ui.Msg(cfg, ui.LevelInfo, "No AUR packages installed.")
			return
		}
		fmt.Println()
		for name, ver := range installed {
			fmt.Printf("  %s%s%s  %s%s%s\n",
				cfg.Style.ColorPrimary, name, cfg.Style.ColorReset,
				cfg.Style.ColorDim, ver, cfg.Style.ColorReset)
		}
		fmt.Println()

	case "remove":
		if len(pkgs) == 0 {
			ui.Msg(cfg, ui.LevelError, "Usage: alps aur remove <package> [packages...]")
			os.Exit(1)
		}

		if dryRun {
			ui.Msgf(cfg, ui.LevelWarn, "DRY-RUN: would remove AUR package(s): %s", strings.Join(pkgs, " "))
			return
		}
		ui.Msgf(cfg, ui.LevelWarn, "Remove AUR package(s) %s%s%s?",
			cfg.Style.ColorBold, strings.Join(pkgs, " "), cfg.Style.ColorReset+cfg.Style.ColorWarning)
		fmt.Print(cfg.Style.ColorReset)
		fmt.Println()
		if !ui.Confirm() {
			ui.Msg(cfg, ui.LevelWarn, "Cancelled.")
			return
		}

		var hasErrors bool
		for _, pkgName := range pkgs {
			if err := aur.Remove(pkgName, false); err != nil {
				ui.Msgf(cfg, ui.LevelError, "failed to remove %s: %v", pkgName, err)
				hasErrors = true
			} else {
				ui.Msg(cfg, ui.LevelOK, pkgName+" removed.")
			}
		}
		if hasErrors {
			os.Exit(1)
		}

	case "build-local":
		dir := "."
		if len(pkgs) > 0 {
			dir = pkgs[0]
		}
		if dryRun {
			ui.Msgf(cfg, ui.LevelWarn, "DRY-RUN: would build PKGBUILD in %s", dir)
			return
		}
		ui.Msgf(cfg, ui.LevelInfo, "Building local PKGBUILD in %s%s%s...",
			cfg.Style.ColorBold, dir, cfg.Style.ColorReset+cfg.Style.ColorInfo)
		fmt.Print(cfg.Style.ColorReset)
		fmt.Println()
		if err := aur.BuildLocal(dir, false); err != nil {
			ui.Msgf(cfg, ui.LevelError, "%v", err)
			os.Exit(1)
		}
		ui.Msg(cfg, ui.LevelOK, "Done.")

	case "fetch-abs":
		if len(pkgs) == 0 {
			ui.Msg(cfg, ui.LevelError, "Usage: alps aur fetch-abs <package>")
			os.Exit(1)
		}
		pkgName := pkgs[0]
		ui.Msgf(cfg, ui.LevelInfo, "Fetching PKGBUILD for %s%s%s from ABS...",
			cfg.Style.ColorBold, pkgName, cfg.Style.ColorReset+cfg.Style.ColorInfo)
		fmt.Print(cfg.Style.ColorReset)
		fmt.Println()
		dir, err := aur.FetchABS(pkgName)
		if err != nil {
			ui.Msgf(cfg, ui.LevelError, "%v", err)
			os.Exit(1)
		}
		ui.Msgf(cfg, ui.LevelOK, "PKGBUILD saved to: %s", dir)

	case "clean":
		cacheRoot, err := aur.AURCacheRoot()
		if err != nil {
			ui.Msgf(cfg, ui.LevelError, "%v", err)
			os.Exit(1)
		}
		if _, err := os.Stat(cacheRoot); os.IsNotExist(err) {
			ui.Msg(cfg, ui.LevelInfo, "No AUR cache found.")
			return
		}
		if dryRun {
			ui.Msgf(cfg, ui.LevelWarn, "DRY-RUN: would remove AUR build cache at %s", cacheRoot)
			return
		}
		ui.Msgf(cfg, ui.LevelInfo, "Remove AUR build cache? (%s)", cacheRoot)
		if !ui.Confirm() {
			ui.Msg(cfg, ui.LevelWarn, "Cancelled.")
			return
		}
		if err := aur.CleanCache(""); err != nil {
			ui.Msgf(cfg, ui.LevelError, "%v", err)
			os.Exit(1)
		}
		ui.Msg(cfg, ui.LevelOK, "Cache removed.")

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
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return
	}
	defer f.Close()
	for _, p := range pkgs {
		fmt.Fprintln(f, p.Name)
	}
}

// printRepoInstallPreview shows install preview for alps-more and GitHub entries.
func printRepoInstallPreview(entry *more.Entry, source string, cfg *config.Config) {
	ui.Msgf(cfg, ui.LevelInfo, "Install %s%s%s from %s?",
		cfg.Style.ColorBold, entry.Name, cfg.Style.ColorReset+cfg.Style.ColorInfo, source)
	if entry.Desc != "" {
		fmt.Printf("  %s%s%s\n", cfg.Style.ColorDim, entry.Desc, cfg.Style.ColorReset)
	}
	if entry.Author != "" {
		fmt.Printf("  %sauthor: %s%s\n", cfg.Style.ColorDim, entry.Author, cfg.Style.ColorReset)
	}
	if entry.Version != "" {
		fmt.Printf("  %sversion: %s%s\n", cfg.Style.ColorDim, entry.Version, cfg.Style.ColorReset)
	}
	fmt.Println()

	fmt.Printf("  %sinstall:%s\n", cfg.Style.ColorBold, cfg.Style.ColorReset)
	for _, line := range entry.CmdLines {
		fmt.Printf("  %s$ %s%s\n", cfg.Style.ColorDim, line, cfg.Style.ColorReset)
	}

	fmt.Println()

	fmt.Print(cfg.Style.ColorReset)
	fmt.Println()
}

func runRepoUpdate(dryRun bool, cfg *config.Config) {
	if dryRun {
		ui.Msgf(cfg, ui.LevelWarn, "DRY-RUN: would fetch and cache repo updates")
		return
	}
	if err := more.FetchAndCache(cfg); err != nil {
		ui.Msg(cfg, ui.LevelError, err.Error())
		os.Exit(1)
	}
	ui.Msg(cfg, ui.LevelOK, "repo cache updated")

	summary, err := more.CheckUpdates(cfg)
	if err != nil {
		ui.Msg(cfg, ui.LevelWarn, fmt.Sprintf("could not check for updates: %v", err))
		return
	}
	if summary == nil {
		return
	}
	if len(summary.Upgradeable) == 0 && len(summary.Stale) == 0 {
		ui.Msg(cfg, ui.LevelOK, "all installed packages are up to date")
		return
	}
	if len(summary.Upgradeable) > 0 {
		ui.Msgf(cfg, ui.LevelInfo,
			"%d package(s) have updates available — run 'alps repo upgrade' to apply",
			len(summary.Upgradeable))
		for _, pkg := range summary.Upgradeable {
			fmt.Printf("       %s\n", pkg)
		}
	}
	if len(summary.Stale) > 0 {
		ui.Msgf(cfg, ui.LevelWarn,
			"%d package(s) no longer in repo — run 'alps repo remove <pkg>' to clean up",
			len(summary.Stale))
		for _, name := range summary.Stale {
			fmt.Printf("       %s\n", name)
		}
	}
}

func runRepoList(rest []string, cfg *config.Config) {
	// Sub-actions: install → ListInstalled, remove → ListStale
	if len(rest) > 0 {
		action := resolveListAction(rest[0], cfg)
		switch action {
		case "install":
			fmt.Println()
			if err := more.ListInstalled(cfg); err != nil {
				ui.Msgf(cfg, ui.LevelError, "%v", err)
				os.Exit(1)
			}
			fmt.Println()
			return
		case "remove":
			fmt.Println()
			if err := more.ListStale(cfg); err != nil {
				ui.Msgf(cfg, ui.LevelError, "%v", err)
				os.Exit(1)
			}
			fmt.Println()
			return
		}
		// Fall through to full list
	}

	entries, err := more.List(cfg)
	if err != nil {
		ui.Msgf(cfg, ui.LevelError, "%v", err)
		os.Exit(1)
	}
	if len(entries) == 0 {
		ui.Msg(cfg, ui.LevelWarn, "No packages in repo.")
		return
	}
	installed, _ := more.ReadInstalled()
	fmt.Println()
	for _, e := range entries {
		installedVer := ""
		if rec, ok := installed[e.Name]; ok {
			installedVer = rec.Version
			if installedVer == "" {
				installedVer = "installed"
			}
			if strings.HasPrefix(rec.Source, "github:") {
				installedVer += " [github]"
			}
		}
		ui.PrintRepoEntry(cfg, e.Name, e.Version, e.Desc, e.Arch, installedVer)
	}
	fmt.Println()
}

func fetchRepoEntry(pkgName string, cfg *config.Config) (*more.Entry, *more.RemoteRef, error) {
	var remoteRef *more.RemoteRef
	var err error

	if more.IsRemoteURL(pkgName) {
		remoteRef, err = more.ParseRemoteURL(pkgName)
		if err != nil {
			return nil, nil, err
		}
	}

	if remoteRef != nil {
		fmt.Println()
		ui.Msgf(cfg, ui.LevelInfo, "fetching ALPSMORE from %s...", remoteRef.DisplayURL())
		fmt.Println()

		var resolved more.RemoteRef
		entry, resolved, err := more.FetchALPSMORERemote(*remoteRef)
		if err != nil {
			return nil, nil, err
		}

		source := resolved.Source()
		// Official alps-more takes priority
		if official, findErr := more.Find(entry.Name, cfg); findErr == nil {
			ui.Msgf(cfg, ui.LevelInfo, "%q found in official alps-more repo — using that instead.", official.Name)
			fmt.Println()
			entry = official
		} else {
			entry.Source = source
		}
		remoteRef = &resolved
		return entry, remoteRef, nil
	}

	entry, err := more.Find(pkgName, cfg)
	if err != nil {
		return nil, nil, err
	}

	return entry, nil, nil
}

func runRepoInstall(pkgs []string, dryRun bool, cfg *config.Config) {
	if len(pkgs) == 0 {
		ui.Msg(cfg, ui.LevelError, "Usage: alps repo install <package> [packages...]")
		os.Exit(1)
	}

	var hasErrors bool
	for _, pkgName := range pkgs {
		entry, remoteRef, err := fetchRepoEntry(pkgName, cfg)
		if err != nil {
			ui.Msgf(cfg, ui.LevelError, "%v", err)
			hasErrors = true
			continue
		}

		if err := more.Validate(entry); err != nil {
			ui.Msgf(cfg, ui.LevelError, "%v", err)
			hasErrors = true
			continue
		}

		sourceStr := "alps-more"
		if remoteRef != nil && entry.Source != "" {
			sourceStr = remoteRef.DisplayURL()
		}
		printRepoInstallPreview(entry, sourceStr, cfg)

		if dryRun {
			ui.Msgf(cfg, ui.LevelWarn, "DRY-RUN: would install %s", entry.Name)
			continue
		}
		if !ui.Confirm() {
			ui.Msg(cfg, ui.LevelWarn, "Cancelled for "+entry.Name)
			continue
		}

		fmt.Println()
		if err := more.Install(entry, cfg); err != nil {
			ui.Msgf(cfg, ui.LevelError, "failed to install %s: %v", entry.Name, err)
			hasErrors = true
		} else {
			ui.Msg(cfg, ui.LevelOK, entry.Name+" installed.")
		}
	}
	if hasErrors {
		os.Exit(1)
	}
}

func runRepoRemove(pkgs []string, dryRun bool, cfg *config.Config) {
	if len(pkgs) == 0 {
		ui.Msg(cfg, ui.LevelError, "Usage: alps repo remove <package> [packages...]")
		os.Exit(1)
	}

	var hasErrors bool
	for _, pkgName := range pkgs {
		entry, stale, err := more.RemovalEntry(pkgName, cfg)
		if err != nil {
			ui.Msgf(cfg, ui.LevelError, "%v", err)
			hasErrors = true
			continue
		}

		// Validate package is installed before confirmation
		_, isInstalled := more.GetInstalled(pkgName)
		if !isInstalled {
			ui.Msgf(cfg, ui.LevelError, "package %q is not installed via alps-more", pkgName)
			hasErrors = true
			continue
		}

		ui.Msgf(cfg, ui.LevelInfo, "Remove %s%s%s from alps-more?",
			cfg.Style.ColorBold, entry.Name, cfg.Style.ColorReset+cfg.Style.ColorInfo)
		if stale {
			ui.Msg(cfg, ui.LevelWarn, "package is no longer in repo; using saved uninstall commands")
		}
		fmt.Println()
		for _, line := range entry.RemoveLines {
			fmt.Printf("  %s$ %s%s\n", cfg.Style.ColorDim, line, cfg.Style.ColorReset)
		}
		fmt.Print(cfg.Style.ColorReset)
		fmt.Println()
		if dryRun {
			ui.Msgf(cfg, ui.LevelWarn, "DRY-RUN: would remove %s", entry.Name)
			continue
		}
		if !ui.Confirm() {
			ui.Msg(cfg, ui.LevelWarn, "Cancelled for "+entry.Name)
			continue
		}

		fmt.Println()
		if err := more.Remove(entry, cfg); err != nil {
			ui.Msgf(cfg, ui.LevelError, "failed to remove %s: %v", entry.Name, err)
			hasErrors = true
		} else {
			ui.Msg(cfg, ui.LevelOK, entry.Name+" removed.")
		}
	}
	if hasErrors {
		os.Exit(1)
	}
}

func runRepoPurge(pkgs []string, dryRun bool, cfg *config.Config) {
	if len(pkgs) == 0 {
		ui.Msg(cfg, ui.LevelError, "Usage: alps repo purge <package> [packages...]")
		os.Exit(1)
	}

	var hasErrors bool
	for _, pkgName := range pkgs {
		entry, stale, err := more.RemovalEntry(pkgName, cfg)
		if err != nil {
			ui.Msgf(cfg, ui.LevelError, "%v", err)
			hasErrors = true
			continue
		}

		// Validate package is installed before confirmation
		_, isInstalled := more.GetInstalled(pkgName)
		if !isInstalled {
			ui.Msgf(cfg, ui.LevelError, "package %q is not installed via alps-more", pkgName)
			hasErrors = true
			continue
		}

		ui.Msgf(cfg, ui.LevelWarn, "Purge %s%s%s? This removes the package AND its config/data files.",
			cfg.Style.ColorBold, entry.Name, cfg.Style.ColorReset+cfg.Style.ColorWarning)
		if stale {
			ui.Msg(cfg, ui.LevelWarn, "package is no longer in repo; using saved uninstall commands")
		}
		fmt.Println()

		if len(entry.RemoveLines) > 0 {
			fmt.Printf("  %sremove:%s\n", cfg.Style.ColorBold, cfg.Style.ColorReset)
			for _, line := range entry.RemoveLines {
				fmt.Printf("  %s$ %s%s\n", cfg.Style.ColorDim, line, cfg.Style.ColorReset)
			}
			fmt.Println()
		}
		if len(entry.PurgeLines) > 0 {
			fmt.Printf("  %spurge:%s\n", cfg.Style.ColorBold, cfg.Style.ColorReset)
			for _, line := range entry.PurgeLines {
				fmt.Printf("  %s$ %s%s\n", cfg.Style.ColorDim, line, cfg.Style.ColorReset)
			}
		} else {
			fmt.Printf("  %s%s  no purge_cmd defined — only remove will run%s\n",
				cfg.Style.ColorDim, cfg.Style.SymWarn, cfg.Style.ColorReset)
		}

		fmt.Print(cfg.Style.ColorReset)
		fmt.Println()
		if dryRun {
			ui.Msgf(cfg, ui.LevelWarn, "DRY-RUN: would purge %s", entry.Name)
			continue
		}
		if !ui.Confirm() {
			ui.Msg(cfg, ui.LevelWarn, "Cancelled for "+entry.Name)
			continue
		}

		fmt.Println()
		if err := more.Purge(pkgName, cfg); err != nil {
			ui.Msgf(cfg, ui.LevelError, "failed to purge %s: %v", entry.Name, err)
			hasErrors = true
		} else {
			ui.Msg(cfg, ui.LevelOK, entry.Name+" purged.")
		}
	}
	if hasErrors {
		os.Exit(1)
	}
}

func runRepoSearch(pkgs []string, rest []string, cfg *config.Config) {
	if len(pkgs) == 0 {
		ui.Msg(cfg, ui.LevelError, "Usage: alps repo search <query>")
		os.Exit(1)
	}
	query := strings.Join(pkgs, " ")
	if query == "" {
		query = strings.Join(rest, " ")
	}
	results, err := more.Search(query, cfg)
	if err != nil {
		ui.Msgf(cfg, ui.LevelError, "%v", err)
		os.Exit(1)
	}
	if len(results) == 0 {
		ui.Msgf(cfg, ui.LevelWarn, "No results for '%s' in alps-more.", query)
		return
	}
	fmt.Println()
	for _, e := range results {
		ui.PrintRepoSearchResult(cfg, e.Name, e.Version, e.Desc)
	}
	fmt.Println()
}

func runRepoUpgrade(rest []string, cfg *config.Config) {
	if len(rest) == 0 {
		ui.Msg(cfg, ui.LevelInfo, "Checking alps-more packages for updates...")
		fmt.Println()
		if err := more.UpgradeAll(cfg); err != nil {
			ui.Msgf(cfg, ui.LevelError, "%v", err)
			os.Exit(1)
		}
	} else {
		var hasErrors bool
		for _, pkgName := range rest {
			if err := more.Upgrade(pkgName, cfg); err != nil {
				ui.Msgf(cfg, ui.LevelError, "failed to upgrade %s: %v", pkgName, err)
				hasErrors = true
			} else {
				ui.Msg(cfg, ui.LevelOK, pkgName+" upgraded.")
			}
		}
		if hasErrors {
			os.Exit(1)
		}
	}
}

func runRepoClean(dryRun bool, cfg *config.Config) {
	cacheDir := more.CacheDir()
	if _, err := os.Stat(cacheDir); os.IsNotExist(err) {
		ui.Msg(cfg, ui.LevelInfo, "No repo cache found.")
		return
	}
	if dryRun {
		ui.Msgf(cfg, ui.LevelWarn, "DRY-RUN: would remove repo cache at %s", cacheDir)
		return
	}
	ui.Msgf(cfg, ui.LevelInfo, "Remove repo cache? (%s)", cacheDir)
	if !ui.Confirm() {
		ui.Msg(cfg, ui.LevelWarn, "Cancelled.")
		return
	}
	if err := more.CleanCache(); err != nil {
		ui.Msgf(cfg, ui.LevelError, "%v", err)
		os.Exit(1)
	}
	ui.Msg(cfg, ui.LevelOK, "Done.")
}

func runRepo(args []string, cfg *config.Config) {
	ui.PrintHeader(cfg)

	if len(args) == 0 {
		ui.Msg(cfg, ui.LevelError, "Usage: alps repo <update|list|install|remove|purge|search|upgrade> [package]")
		os.Exit(1)
	}

	rawSubcmd := args[0]
	subcmd, err := resolveSubCmd("repo", rawSubcmd, cfg)
	if err != nil {
		ui.Msgf(cfg, ui.LevelError, "%v", err)
		os.Exit(1)
	}
	rest := args[1:]
	pkgs, restFlags := pack.ParseFlagsExt(rest)
	dryRun := restFlags.DryRun
	// -y is intentionally NOT supported for repo operations.

	switch subcmd {

	case "update":
		runRepoUpdate(dryRun, cfg)

	case "list":
		runRepoList(rest, cfg)

	case "install":
		runRepoInstall(pkgs, dryRun, cfg)

	case "remove":
		runRepoRemove(pkgs, dryRun, cfg)

	case "purge":
		runRepoPurge(pkgs, dryRun, cfg)

	case "search":
		runRepoSearch(pkgs, rest, cfg)

	case "upgrade":
		runRepoUpgrade(rest, cfg)

	case "clean":
		runRepoClean(dryRun, cfg)

	default:
		ui.Msgf(cfg, ui.LevelError, "Unknown repo subcommand: %s", subcmd)
		os.Exit(1)
	}
}

func runFlatpak(args []string, cfg *config.Config) {
	ui.PrintHeader(cfg)

	if !flatpak.IsAvailable() {
		ui.Msg(cfg, ui.LevelError, "flatpak is not installed")
		os.Exit(1)
	}

	if len(args) == 0 {
		ui.Msg(cfg, ui.LevelError, "Usage: alps flatpak <install|remove|search|list|update> [args]")
		os.Exit(1)
	}

	rawSubcmd := args[0]
	subcmd, err := resolveSubCmd("flatpak", rawSubcmd, cfg)
	if err != nil {
		ui.Msgf(cfg, ui.LevelError, "%v", err)
		os.Exit(1)
	}
	rest := args[1:]
	pkgs, restFlags := pack.ParseFlagsExt(rest)
	dryRun := restFlags.DryRun
	// -y / noConfirm intentionally NOT supported for flatpak.

	switch subcmd {
	case "install":
		flatpakInstall(pkgs, dryRun, cfg)
	case "remove":
		flatpakRemove(pkgs, dryRun, cfg)
	case "search":
		flatpakSearch(rest, pkgs, cfg)
	case "list":
		flatpakList(cfg)
	case "update":
		flatpakUpdate(dryRun, cfg)
	default:
		ui.Msgf(cfg, ui.LevelError, "Unknown flatpak subcommand: %s", subcmd)
		os.Exit(1)
	}
}

func flatpakInstall(pkgs []string, dryRun bool, cfg *config.Config) {
	if len(pkgs) == 0 {
		ui.Msg(cfg, ui.LevelError, "Usage: alps flatpak install <package>")
		os.Exit(1)
	}
	if dryRun {
		ui.Msgf(cfg, ui.LevelWarn, "DRY-RUN: would install flatpak package(s): %s", strings.Join(pkgs, " "))
		return
	}
	if err := flatpak.Install(pkgs, false); err != nil {
		ui.Msgf(cfg, ui.LevelError, "%v", err)
		os.Exit(1)
	}
	ui.Msg(cfg, ui.LevelOK, "Done.")
}

func flatpakRemove(pkgs []string, dryRun bool, cfg *config.Config) {
	if len(pkgs) == 0 {
		ui.Msg(cfg, ui.LevelError, "Usage: alps flatpak remove <package>")
		os.Exit(1)
	}
	if dryRun {
		ui.Msgf(cfg, ui.LevelWarn, "DRY-RUN: would remove flatpak package: %s", pkgs[0])
		return
	}
	if err := flatpak.Remove(pkgs[0], false); err != nil {
		ui.Msgf(cfg, ui.LevelError, "%v", err)
		os.Exit(1)
	}
	ui.Msg(cfg, ui.LevelOK, "Done.")
}

func flatpakSearch(rest, pkgs []string, cfg *config.Config) {
	if len(rest) == 0 {
		ui.Msg(cfg, ui.LevelError, "Usage: alps flatpak search <query>")
		os.Exit(1)
	}
	if err := flatpak.Search(strings.Join(pkgs, " ")); err != nil {
		ui.Msgf(cfg, ui.LevelError, "%v", err)
		os.Exit(1)
	}
}

func flatpakList(cfg *config.Config) {
	if err := flatpak.List(); err != nil {
		ui.Msgf(cfg, ui.LevelError, "%v", err)
		os.Exit(1)
	}
}

func flatpakUpdate(dryRun bool, cfg *config.Config) {
	if dryRun {
		ui.Msgf(cfg, ui.LevelWarn, "DRY-RUN: would update all flatpak packages")
		return
	}
	if err := flatpak.Update(false); err != nil {
		ui.Msgf(cfg, ui.LevelError, "%v", err)
		os.Exit(1)
	}
	ui.Msg(cfg, ui.LevelOK, "Done.")
}

func runSnap(args []string, cfg *config.Config) {
	ui.PrintHeader(cfg)

	if !snap.IsAvailable() {
		ui.Msg(cfg, ui.LevelError, "snap is not available (not installed or blocked)")
		os.Exit(1)
	}

	if len(args) == 0 {
		ui.Msg(cfg, ui.LevelError, "Usage: alps snap <install|remove|search|list|update> [args]")
		os.Exit(1)
	}

	rawSubcmd := args[0]
	subcmd, err := resolveSubCmd("snap", rawSubcmd, cfg)
	if err != nil {
		ui.Msgf(cfg, ui.LevelError, "%v", err)
		os.Exit(1)
	}
	rest := args[1:]
	pkgs, restFlags := pack.ParseFlagsExt(rest)
	dryRun := restFlags.DryRun
	// -y / noConfirm intentionally NOT supported for snap operations.

	switch subcmd {
	case "install":
		snapInstall(pkgs, dryRun, cfg)
	case "remove":
		snapRemove(pkgs, dryRun, cfg)
	case "search":
		snapSearch(rest, pkgs, cfg)
	case "list":
		snapList(cfg)
	case "update":
		snapUpdate(dryRun, cfg)
	default:
		ui.Msgf(cfg, ui.LevelError, "Unknown snap subcommand: %s", subcmd)
		os.Exit(1)
	}
}

func snapInstall(pkgs []string, dryRun bool, cfg *config.Config) {
	if len(pkgs) == 0 {
		ui.Msg(cfg, ui.LevelError, "Usage: alps snap install <package>")
		os.Exit(1)
	}
	if dryRun {
		ui.Msgf(cfg, ui.LevelWarn, "DRY-RUN: would install snap package(s): %s", strings.Join(pkgs, " "))
		return
	}
	if err := snap.Install(pkgs, false); err != nil {
		ui.Msgf(cfg, ui.LevelError, "%v", err)
		os.Exit(1)
	}
	ui.Msg(cfg, ui.LevelOK, "Done.")
}

func snapRemove(pkgs []string, dryRun bool, cfg *config.Config) {
	if len(pkgs) == 0 {
		ui.Msg(cfg, ui.LevelError, "Usage: alps snap remove <package>")
		os.Exit(1)
	}
	if dryRun {
		ui.Msgf(cfg, ui.LevelWarn, "DRY-RUN: would remove snap package: %s", pkgs[0])
		return
	}
	if err := snap.Remove(pkgs[0]); err != nil {
		ui.Msgf(cfg, ui.LevelError, "%v", err)
		os.Exit(1)
	}
	ui.Msg(cfg, ui.LevelOK, "Done.")
}

func snapSearch(rest, pkgs []string, cfg *config.Config) {
	if len(rest) == 0 {
		ui.Msg(cfg, ui.LevelError, "Usage: alps snap search <query>")
		os.Exit(1)
	}
	if err := snap.Search(strings.Join(pkgs, " ")); err != nil {
		ui.Msgf(cfg, ui.LevelError, "%v", err)
		os.Exit(1)
	}
}

func snapList(cfg *config.Config) {
	if err := snap.List(); err != nil {
		ui.Msgf(cfg, ui.LevelError, "%v", err)
		os.Exit(1)
	}
}

func snapUpdate(dryRun bool, cfg *config.Config) {
	if dryRun {
		ui.Msgf(cfg, ui.LevelWarn, "DRY-RUN: would refresh all snap packages")
		return
	}
	if err := snap.Update(); err != nil {
		ui.Msgf(cfg, ui.LevelError, "%v", err)
		os.Exit(1)
	}
	ui.Msg(cfg, ui.LevelOK, "Done.")
}

func runAptWithSnapFallback(args []string, dryRun bool, cfg *config.Config) {
	if len(args) == 0 {
		ui.Msg(cfg, ui.LevelError, "Package name required")
		return
	}

	pkgs, noConfirm := splitFlags(args)
	realBackend := pack.DetectRealApt()

	if dryRun {
		ui.Msgf(cfg, ui.LevelWarn, "DRY-RUN: no changes will be made")
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
		if noConfirm {
			aptArgs = append(aptArgs, "-y")
		}
		if dryRun {
			aptArgs = append(aptArgs, pack.GetDryRunFlag("apt"))
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

	if len(notFound) > 0 && snap.IsAvailable() {
		fmt.Println()
		ui.Msgf(cfg, ui.LevelWarn, "Not found in apt: %s", strings.Join(notFound, " "))
		ui.Msgf(cfg, ui.LevelInfo, "Try snap for %s%s%s?",
			cfg.Style.ColorBold, strings.Join(notFound, " "), cfg.Style.ColorReset+cfg.Style.ColorInfo)
		fmt.Print(cfg.Style.ColorReset)
		if dryRun {
			ui.Msgf(cfg, ui.LevelWarn, "DRY-RUN: would prompt to install snap package(s): %s", strings.Join(notFound, " "))
		} else if ui.Confirm() {
			if err := snap.Install(notFound, false); err != nil {
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
	query := strings.Join(args, " ")
	realBackend := pack.DetectRealApt()

	type snapDone struct{ err error }
	snapCh := make(chan snapDone, 1)
	snapEnabled := snap.IsAvailable()
	if snapEnabled {
		go func() {
			snapCh <- snapDone{snap.Search(query)}
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
