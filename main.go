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
		printDiagnostic(cfg)
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
	},
	"repo": {
		"update": true, "list": true, "install": true,
		"remove": true, "purge": true, "search": true, "upgrade": true,
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

	if backend == "pacman" && (subcmd == "update" || subcmd == "upgrade") {
		ui.Msg(cfg, ui.LevelWarn, "Running pacman -Sy or -Su alone is not recommended on Arch.")
		fmt.Print("     This may cause partial upgrades and break your system.")
		fmt.Println()
		fmt.Println()
		ui.Msgf(cfg, ui.LevelInfo, "Use %sfull-upgrade%s (alps fug) to sync and upgrade at once.",
			cfg.Style.ColorBold, cfg.Style.ColorReset+cfg.Style.ColorInfo)
		fmt.Print(cfg.Style.ColorReset)
		fmt.Println()
		fmt.Print("  Continue anyway? [y/N] ")
		if strings.ToLower(readLine()) != "y" {
			ui.Msg(cfg, ui.LevelWarn, "Cancelled.")
			return
		}
		fmt.Println()
	}

	switch {
	case backend == "pacman" && subcmd == "install":
		runPacmanWithAURFallback(args, cfg)
	case backend == "pacman" && subcmd == "search":
		runPacmanSearch(args, cfg)
	case backend == "pacman" && subcmd == "autoremove":
		runPacmanAutoremove(cfg)
	case backend == "pacman" && (subcmd == "upgrade" || subcmd == "full-upgrade"):
		runPacmanUpgrade(subcmd, args, cfg)
	case backend == "apt" && subcmd == "install":
		runAptWithSnapFallback(args, cfg)
	case backend == "apt" && subcmd == "search":
		runAptSearch(args, cfg)
	default:
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
		runWithBackend(mapped, args, cfg, backend, subcmd)
	}
}

func splitFlags(args []string) (pkgs []string, noConfirm bool) {
	for _, a := range args {
		if a == "--noconfirm" || a == "-y" {
			noConfirm = true
		} else {
			pkgs = append(pkgs, a)
		}
	}
	return
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
	sudo := needsSudo(backend)

	fullArgs := make([]string, len(cmdArgs[1:]))
	copy(fullArgs, cmdArgs[1:])
	fullArgs = append(fullArgs, args...)

	display := fmtCmd(cmdArgs, args)
	ui.Msgf(cfg, ui.LevelInfo, "%s (%s%s%s)",
		subcmd,
		cfg.Style.ColorDim,
		display,
		cfg.Style.ColorReset)
	fmt.Print(cfg.Style.ColorReset)
	fmt.Println()

	if sudo {
		if err := ensureSudo(); err != nil {
			ui.Msg(cfg, ui.LevelError, "privilege escalation failed")
			return false
		}
	}

	var cmd *exec.Cmd
	if sudo {
		var err error
		cmd, err = priv.Command(append([]string{cmdArgs[0]}, fullArgs...)...)
		if err != nil {
			ui.Msgf(cfg, ui.LevelError, "%v", err)
			return false
		}
	} else {
		cmd = exec.Command(cmdArgs[0], fullArgs...)
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

func isTermux() bool {
	return os.Getenv("TERMUX_VERSION") != "" ||
		os.Getenv("PREFIX") == "/data/data/com.termux/files/usr"
}

func printDiagnostic(cfg *config.Config) {
	ui.PrintHeader(cfg)

	var distro string
	if isTermux() {
		distro = "Termux"
		if v := os.Getenv("TERMUX_VERSION"); v != "" {
			distro = "Termux " + v
		}

		out, err := exec.Command("/system/bin/getprop", "ro.build.version.release").Output()
		if err == nil {
			if v := strings.TrimSpace(string(out)); v != "" {
				distro += " (Android " + v + ")"
			}
		}
	} else {
		distro = "unknown"
		if data, err := os.ReadFile("/etc/os-release"); err == nil {
			for _, line := range strings.Split(string(data), "\n") {
				if strings.HasPrefix(line, "PRETTY_NAME=") {
					distro = strings.Trim(line[12:], `"'`)
					break
				}
			}
		}
	}

	backend := detectBackend()
	if backend == "" {
		backend = "none detected"
	}

	installed, _ := more.ReadInstalled()
	moreCount := len(installed)

	extras := []string{}
	if !isTermux() {
		if _, err := exec.LookPath("flatpak"); err == nil {
			extras = append(extras, "flatpak")
		}
		if _, err := exec.LookPath("snap"); err == nil {
			extras = append(extras, "snap")
		}
		if _, err := exec.LookPath("yay"); err == nil {
			extras = append(extras, "yay")
		}
	}

	dim := cfg.Style.ColorDim
	rst := cfg.Style.ColorReset
	pri := cfg.Style.ColorPrimary

	fmt.Printf("  %ssystem%s   %s\n", pri, rst, distro)
	fmt.Printf("  %sbackend%s  %s\n", pri, rst, backend)
	if len(extras) > 0 {
		fmt.Printf("  %sextras%s   %s\n", pri, rst, strings.Join(extras, "  "))
	}
	fmt.Printf("  %smore%s     %s%d package(s) installed via alps-more%s\n", pri, rst, dim, moreCount, rst)
	fmt.Println()
	fmt.Printf("  %srun 'alps help' for commands%s\n", dim, rst)
	fmt.Println()
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

func runPacmanUpgrade(subcmd string, args []string, cfg *config.Config) {
	pacmanArgs := []string{"pacman", "-Su"}
	if subcmd == "full-upgrade" {
		pacmanArgs = []string{"pacman", "-Syu"}
	}
	if !runWithBackend(pacmanArgs, args, cfg, "pacman", subcmd) {
		return
	}
	_, noConfirm := splitFlags(args)
	runAURUpgrade(noConfirm, cfg)
}

func runPacmanWithAURFallback(args []string, cfg *config.Config) {
	if len(args) == 0 {
		ui.Msg(cfg, ui.LevelError, "Package name required")
		return
	}

	pkgs, noConfirm := splitFlags(args)

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
		spCmd := exec.Command("pacman", spArgs...)
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
		if err := ensureSudo(); err != nil {
			ui.Msg(cfg, ui.LevelError, "sudo authentication failed")
			return
		}
		pacmanArgs := append([]string{"-S"}, repoPkgs...)
		if noConfirm {
			pacmanArgs = append(pacmanArgs, "--noconfirm")
		}
		cmd, err := priv.Command(append([]string{"pacman"}, pacmanArgs...)...)
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

	if len(notFound) > 0 {
		fmt.Println()
		ui.Msgf(cfg, ui.LevelWarn, "Not found in repo: %s", strings.Join(notFound, " "))
		ui.Msgf(cfg, ui.LevelInfo, "To install from AUR, run: alps aur install %s", strings.Join(notFound, " "))
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
	repoCmd := exec.Command("pacman", "-Ss", query)
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

func runAURUpgrade(noConfirm bool, cfg *config.Config) {
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
	fmt.Println()

	names := make([]string, 0, len(installed))
	for name := range installed {
		names = append(names, name)
	}
	latest, err := aur.InfoBatch(names)
	if err != nil {
		ui.Msgf(cfg, ui.LevelError, "failed to check for AUR updates: %v", err)
		return
	}

	var outdated []aur.Package
	for name, installedVer := range installed {
		pkg, ok := latest[name]
		if !ok {
			continue
		}
		if pkg.Version != installedVer {
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

	var failed []string
	for _, pkg := range outdated {
		ui.Msgf(cfg, ui.LevelInfo, "Upgrading %s%s%s...",
			cfg.Style.ColorBold, pkg.Name, cfg.Style.ColorReset+cfg.Style.ColorInfo)
		if err := aur.Install([]string{pkg.Name}, noConfirm); err != nil {
			ui.Msgf(cfg, ui.LevelError, "failed to upgrade %s: %v", pkg.Name, err)
			failed = append(failed, pkg.Name)
		} else {
			ui.Msgf(cfg, ui.LevelOK, "%s upgraded.", pkg.Name)
		}
	}

	if len(failed) > 0 {
		ui.Msgf(cfg, ui.LevelWarn, "Partial completion: %d of %d package(s) failed to upgrade: %s",
			len(failed), len(outdated), strings.Join(failed, ", "))
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

	switch subcmd {
	case "install":
		if len(rest) == 0 {
			ui.Msg(cfg, ui.LevelError, "Usage: alps aur install <package>")
			os.Exit(1)
		}
		_, noConfirm := splitFlags(rest)
		pkgs, _ := splitFlags(rest)
		if err := aur.Install(pkgs, noConfirm); err != nil {
			ui.Msgf(cfg, ui.LevelError, "%v", err)
			os.Exit(1)
		}
		ui.Msg(cfg, ui.LevelOK, "Done.")

	case "search":
		if len(rest) == 0 {
			ui.Msg(cfg, ui.LevelError, "Usage: alps aur search <query>")
			os.Exit(1)
		}
		query := strings.Join(rest, " ")
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
		if len(rest) == 0 {
			ui.Msg(cfg, ui.LevelError, "Usage: alps aur remove <package>")
			os.Exit(1)
		}
		_, noConfirm := splitFlags(rest)
		pkgName := rest[0]
		ui.Msgf(cfg, ui.LevelWarn, "Remove AUR package %s%s%s?",
			cfg.Style.ColorBold, pkgName, cfg.Style.ColorReset+cfg.Style.ColorWarning)
		fmt.Print(cfg.Style.ColorReset)
		fmt.Println()
		if !noConfirm && !ui.Confirm() {
			ui.Msg(cfg, ui.LevelWarn, "Cancelled.")
			return
		}
		if err := aur.Remove(pkgName, noConfirm); err != nil {
			ui.Msgf(cfg, ui.LevelError, "%v", err)
			os.Exit(1)
		}
		ui.Msg(cfg, ui.LevelOK, "Done.")

	case "build-local":
		dir := "."
		if len(rest) > 0 {
			dir = rest[0]
		}
		_, noConfirm := splitFlags(rest)
		ui.Msgf(cfg, ui.LevelInfo, "Building local PKGBUILD in %s%s%s...",
			cfg.Style.ColorBold, dir, cfg.Style.ColorReset+cfg.Style.ColorInfo)
		fmt.Print(cfg.Style.ColorReset)
		fmt.Println()
		if err := aur.BuildLocal(dir, noConfirm); err != nil {
			ui.Msgf(cfg, ui.LevelError, "%v", err)
			os.Exit(1)
		}
		ui.Msg(cfg, ui.LevelOK, "Done.")

	case "fetch-abs":
		if len(rest) == 0 {
			ui.Msg(cfg, ui.LevelError, "Usage: alps aur fetch-abs <package>")
			os.Exit(1)
		}
		pkgName := rest[0]
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
	if len(entry.RemoveLines) > 0 {
		fmt.Printf("  %s%s  auto-cleanup on failure: enabled%s\n",
			cfg.Style.ColorDim, cfg.Style.SymOK, cfg.Style.ColorReset)
	} else {
		fmt.Printf("  %s%s  no remove_cmd — cannot auto-cleanup if install fails%s\n",
			cfg.Style.ColorWarning, cfg.Style.SymWarn, cfg.Style.ColorReset)
	}

	fmt.Print(cfg.Style.ColorReset)
	fmt.Println()
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

	switch subcmd {

	case "update":
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

	case "list":
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

	case "install":
		if len(rest) == 0 {
			ui.Msg(cfg, ui.LevelError, "Usage: alps repo install <package>")
			os.Exit(1)
		}
		pkgName := rest[0]

		// Remote repo install from github.com or gitlab.com
		var remoteProvider, remoteRepoPath string
		switch {
		case strings.HasPrefix(pkgName, "github.com/"):
			remoteProvider = "github"
			remoteRepoPath = strings.TrimPrefix(pkgName, "github.com/")
		case strings.HasPrefix(pkgName, "gitlab.com/"):
			remoteProvider = "gitlab"
			remoteRepoPath = strings.TrimPrefix(pkgName, "gitlab.com/")
		}

		if remoteProvider != "" {
			fmt.Println()
			ui.Msgf(cfg, ui.LevelInfo, "fetching ALPSMORE from %s.com/%s...", remoteProvider, remoteRepoPath)
			fmt.Println()

			source := remoteProvider + ":" + remoteRepoPath
			entry, err := more.FetchALPSMOREFromSource(source)
			if err != nil {
				ui.Msgf(cfg, ui.LevelError, "%v", err)
				os.Exit(1)
			}

			// Official alps-more takes priority
			if official, findErr := more.Find(entry.Name, cfg); findErr == nil {
				ui.Msgf(cfg, ui.LevelInfo, "%q found in official alps-more repo — using that instead.", official.Name)
				fmt.Println()
				entry = official
				pkgName = official.Name
				goto alpsMoreInstall
			}

			entry.Source = source

			if err := more.Validate(entry); err != nil {
				ui.Msgf(cfg, ui.LevelError, "%v", err)
				os.Exit(1)
			}

			printRepoInstallPreview(entry, remoteProvider+".com/"+remoteRepoPath, cfg)
			if !ui.Confirm() {
				ui.Msg(cfg, ui.LevelWarn, "Cancelled.")
				return
			}
			fmt.Println()
			if err := more.Install(entry, cfg); err != nil {
				ui.Msgf(cfg, ui.LevelError, "%v", err)
				os.Exit(1)
			}
			ui.Msg(cfg, ui.LevelOK, "Done.")
			return
		}

	alpsMoreInstall:
		entry, err := more.Find(pkgName, cfg)
		if err != nil {
			ui.Msgf(cfg, ui.LevelError, "%v", err)
			os.Exit(1)
		}

		if err := more.Validate(entry); err != nil {
			ui.Msgf(cfg, ui.LevelError, "%v", err)
			os.Exit(1)
		}

		printRepoInstallPreview(entry, "alps-more", cfg)
		if !ui.Confirm() {
			ui.Msg(cfg, ui.LevelWarn, "Cancelled.")
			return
		}

		fmt.Println()
		if err := more.Install(entry, cfg); err != nil {
			ui.Msgf(cfg, ui.LevelError, "%v", err)
			os.Exit(1)
		}
		ui.Msg(cfg, ui.LevelOK, "Done.")

	case "remove":
		if len(rest) == 0 {
			ui.Msg(cfg, ui.LevelError, "Usage: alps repo remove <package>")
			os.Exit(1)
		}
		pkgName := rest[0]
		entry, stale, err := more.RemovalEntry(pkgName, cfg)
		if err != nil {
			ui.Msgf(cfg, ui.LevelError, "%v", err)
			os.Exit(1)
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
		if !ui.Confirm() {
			ui.Msg(cfg, ui.LevelWarn, "Cancelled.")
			return
		}

		fmt.Println()
		if err := more.Remove(entry, cfg); err != nil {
			ui.Msgf(cfg, ui.LevelError, "%v", err)
			os.Exit(1)
		}
		if err := more.UnmarkInstalled(pkgName); err != nil {
			ui.Msgf(cfg, ui.LevelWarn, "removed but failed to update state: %v", err)
		}
		ui.Msg(cfg, ui.LevelOK, "Done.")

	case "purge":
		if len(rest) == 0 {
			ui.Msg(cfg, ui.LevelError, "Usage: alps repo purge <package>")
			os.Exit(1)
		}
		pkgName := rest[0]
		entry, stale, err := more.RemovalEntry(pkgName, cfg)
		if err != nil {
			ui.Msgf(cfg, ui.LevelError, "%v", err)
			os.Exit(1)
		}

		_, isInstalled := more.GetInstalled(pkgName)
		if !isInstalled {
			ui.Msgf(cfg, ui.LevelError, "package %q is not installed via alps-more", pkgName)
			os.Exit(1)
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
		if !ui.Confirm() {
			ui.Msg(cfg, ui.LevelWarn, "Cancelled.")
			return
		}

		fmt.Println()
		if err := more.Purge(pkgName, cfg); err != nil {
			ui.Msgf(cfg, ui.LevelError, "%v", err)
			os.Exit(1)
		}
		ui.Msg(cfg, ui.LevelOK, "Done.")

	case "search":
		if len(rest) == 0 {
			ui.Msg(cfg, ui.LevelError, "Usage: alps repo search <query>")
			os.Exit(1)
		}
		query := strings.Join(rest, " ")
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

	case "upgrade":
		if len(rest) == 0 {
			ui.Msg(cfg, ui.LevelInfo, "Checking alps-more packages for updates...")
			fmt.Println()
			if err := more.UpgradeAll(cfg); err != nil {
				ui.Msgf(cfg, ui.LevelError, "%v", err)
				os.Exit(1)
			}
		} else {
			pkgName := rest[0]
			if err := more.Upgrade(pkgName, cfg); err != nil {
				ui.Msgf(cfg, ui.LevelError, "%v", err)
				os.Exit(1)
			}
			ui.Msg(cfg, ui.LevelOK, "Done.")
		}

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
	_, noConfirm := splitFlags(rest)
	pkgs, _ := splitFlags(rest)

	switch subcmd {
	case "install":
		if len(pkgs) == 0 {
			ui.Msg(cfg, ui.LevelError, "Usage: alps flatpak install <package>")
			os.Exit(1)
		}
		if err := flatpak.Install(pkgs, noConfirm); err != nil {
			ui.Msgf(cfg, ui.LevelError, "%v", err)
			os.Exit(1)
		}
		ui.Msg(cfg, ui.LevelOK, "Done.")
	case "remove":
		if len(pkgs) == 0 {
			ui.Msg(cfg, ui.LevelError, "Usage: alps flatpak remove <package>")
			os.Exit(1)
		}
		if err := flatpak.Remove(pkgs[0], noConfirm); err != nil {
			ui.Msgf(cfg, ui.LevelError, "%v", err)
			os.Exit(1)
		}
		ui.Msg(cfg, ui.LevelOK, "Done.")
	case "search":
		if len(rest) == 0 {
			ui.Msg(cfg, ui.LevelError, "Usage: alps flatpak search <query>")
			os.Exit(1)
		}
		if err := flatpak.Search(strings.Join(rest, " ")); err != nil {
			ui.Msgf(cfg, ui.LevelError, "%v", err)
			os.Exit(1)
		}
	case "list":
		if err := flatpak.List(); err != nil {
			ui.Msgf(cfg, ui.LevelError, "%v", err)
			os.Exit(1)
		}
	case "update":
		if err := flatpak.Update(noConfirm); err != nil {
			ui.Msgf(cfg, ui.LevelError, "%v", err)
			os.Exit(1)
		}
		ui.Msg(cfg, ui.LevelOK, "Done.")
	default:
		ui.Msgf(cfg, ui.LevelError, "Unknown flatpak subcommand: %s", subcmd)
		os.Exit(1)
	}
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
	pkgs, _ := splitFlags(rest)

	switch subcmd {
	case "install":
		if len(pkgs) == 0 {
			ui.Msg(cfg, ui.LevelError, "Usage: alps snap install <package>")
			os.Exit(1)
		}
		if err := snap.Install(pkgs, false); err != nil {
			ui.Msgf(cfg, ui.LevelError, "%v", err)
			os.Exit(1)
		}
		ui.Msg(cfg, ui.LevelOK, "Done.")
	case "remove":
		if len(pkgs) == 0 {
			ui.Msg(cfg, ui.LevelError, "Usage: alps snap remove <package>")
			os.Exit(1)
		}
		if err := snap.Remove(pkgs[0]); err != nil {
			ui.Msgf(cfg, ui.LevelError, "%v", err)
			os.Exit(1)
		}
		ui.Msg(cfg, ui.LevelOK, "Done.")
	case "search":
		if len(rest) == 0 {
			ui.Msg(cfg, ui.LevelError, "Usage: alps snap search <query>")
			os.Exit(1)
		}
		if err := snap.Search(strings.Join(rest, " ")); err != nil {
			ui.Msgf(cfg, ui.LevelError, "%v", err)
			os.Exit(1)
		}
	case "list":
		if err := snap.List(); err != nil {
			ui.Msgf(cfg, ui.LevelError, "%v", err)
			os.Exit(1)
		}
	case "update":
		if err := snap.Update(); err != nil {
			ui.Msgf(cfg, ui.LevelError, "%v", err)
			os.Exit(1)
		}
		ui.Msg(cfg, ui.LevelOK, "Done.")
	default:
		ui.Msgf(cfg, ui.LevelError, "Unknown snap subcommand: %s", subcmd)
		os.Exit(1)
	}
}

func runAptWithSnapFallback(args []string, cfg *config.Config) {
	if len(args) == 0 {
		ui.Msg(cfg, ui.LevelError, "Package name required")
		return
	}

	pkgs, noConfirm := splitFlags(args)
	realBackend := pack.DetectRealApt()

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
			chk := exec.Command(chkCmd, "show", pkg)
			chk.Stdout = nil
			chk.Stderr = nil
			if chk.Run() != nil {
				notFound = append(notFound, pkg)
				continue
			}
		}
		repoPkgs = append(repoPkgs, pkg)
	}

	if err := ensureSudo(); err != nil {
		ui.Msg(cfg, ui.LevelError, "privilege escalation failed")
		return
	}

	if len(repoPkgs) > 0 {
		aptArgs := append([]string{realBackend, "install"}, repoPkgs...)
		if noConfirm {
			aptArgs = append(aptArgs, "-y")
		}
		cmd, err := priv.Command(aptArgs...)
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
		if ui.Confirm() {
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
	aptCmd := exec.Command(realBackend, "search", query)
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
