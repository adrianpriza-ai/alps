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
		// A resolved alias may map to a subsystem — re-dispatch if so.
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

// hardCommands is the complete set of built-in alps command names.
// These are always valid regardless of aliases.
var hardCommands = map[string]bool{
	// meta
	"help": true, "--help": true, "-h": true,
	"version": true, "--version": true,
	"aliases": true, "config-show": true, "completion": true,
	// subsystems
	"repo": true, "aur": true, "flatpak": true, "snap": true,
	// package operations
	"install": true, "remove": true, "purge": true,
	"update": true, "upgrade": true, "full-upgrade": true,
	"search": true, "show": true, "list": true,
	"autoremove": true, "autoclean": true, "clean": true,
	"edit-sources": true,
}

// resolveCmd implements 3-tier command resolution:
//  1. Hard command  — always valid as-is (install, repo, aur, …)
//  2. Config alias  — defined in /etc/alps/config or ~/.config/alps/config
//  3. Default alias — built-in short names (ins, rm, se, …)
//
// Anything outside these three tiers returns an error.
func resolveCmd(cmd string, cfg *config.Config) (string, error) {
	// Tier 1: built-in command
	if hardCommands[cmd] {
		return cmd, nil
	}
	// Tier 2: user-defined config alias
	if v, ok := cfg.ConfigAliases[cmd]; ok {
		return v, nil
	}
	// Tier 3: default short alias
	if v, ok := config.DefaultAliases[cmd]; ok {
		return v, nil
	}
	return "", fmt.Errorf("unknown command %q — run 'alps help' for available commands", cmd)
}

// validSubCmds lists the accepted subcommand names per subsystem.
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

// resolveSubCmd applies the same 3-tier resolution as resolveCmd but for
// subcommands inside a subsystem (aur, repo, flatpak, snap).
//  1. Direct match   — "install", "search", etc.
//  2. Config alias   — user-defined via alias_ins = install in config
//  3. Default alias  — built-in shorts: ins, rm, se, ls, up, ug, …
func resolveSubCmd(system, subcmd string, cfg *config.Config) (string, error) {
	valid := validSubCmds[system]

	// Tier 1: direct subcommand name
	if valid[subcmd] {
		return subcmd, nil
	}
	// Tier 2: config alias → check if it resolves to a valid subcommand
	if v, ok := cfg.ConfigAliases[subcmd]; ok {
		if valid[v] {
			return v, nil
		}
	}
	// Tier 3: default short alias → check DefaultAliases then DefaultSubCmdAliases
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

	// Build a sorted readable list for the error
	names := make([]string, 0, len(valid))
	for k := range valid {
		names = append(names, k)
	}
	sort.Strings(names)
	return "", fmt.Errorf("unknown %s subcommand %q\n  valid: %s", system, subcmd, strings.Join(names, ", "))
}

func detectBackend() string {
	for _, b := range []string{"apt", "apt-get", "dnf", "pacman"} {
		if _, err := exec.LookPath(b); err == nil {
			return b
		}
	}
	return ""
}

// detectRealBackend returns the actual binary name.
func detectRealBackend() string {
	return detectBackend()
}

var cmdMap = map[string]map[string][]string{
	"apt": {
		"install":      {"apt", "install"},
		"remove":       {"apt", "remove"},
		"purge":        {"apt", "purge"},
		"update":       {"apt", "update"},
		"upgrade":      {"apt", "upgrade"},
		"full-upgrade": {"apt", "full-upgrade"},
		"search":       {"apt", "search"},
		"show":         {"apt", "show"},
		"list":         {"apt", "list"},
		"autoremove":   {"apt", "autoremove"},
		"autoclean":    {"apt", "autoclean"},
		"clean":        {"apt", "clean"},
	},
	"apt-get": {
		"install":      {"apt-get", "install"},
		"remove":       {"apt-get", "remove"},
		"purge":        {"apt-get", "purge"},
		"update":       {"apt-get", "update"},
		"upgrade":      {"apt-get", "upgrade"},
		"full-upgrade": {"apt-get", "dist-upgrade"},
		"search":       {"apt-cache", "search"},
		"show":         {"apt-cache", "show"},
		"list":         {"dpkg", "--list"},
		"autoremove":   {"apt-get", "autoremove"},
		"autoclean":    {"apt-get", "autoclean"},
		"clean":        {"apt-get", "clean"},
	},
	"dnf": {
		"install":      {"dnf", "install"},
		"remove":       {"dnf", "remove"},
		"purge":        {"dnf", "remove"},
		"update":       {"dnf", "check-update"},
		"upgrade":      {"dnf", "upgrade"},
		"full-upgrade": {"dnf", "upgrade", "--refresh"},
		"search":       {"dnf", "search"},
		"show":         {"dnf", "info"},
		"list":         {"dnf", "list"},
		"autoremove":   {"dnf", "autoremove"},
		"clean":        {"dnf", "clean", "all"},
	},
	"pacman": {
		"install":      {"pacman", "-S"},
		"remove":       {"pacman", "-R"},
		"purge":        {"pacman", "-Rns"},
		"update":       {"pacman", "-Sy"},
		"upgrade":      {"pacman", "-Su"},
		"full-upgrade": {"pacman", "-Syu"},
		"search":       {"pacman", "-Ss"},
		"show":         {"pacman", "-Si"},
		"list":         {"pacman", "-Q"},
		"clean":        {"pacman", "-Sc"},
	},
}

// needsSudo returns true for backends that require privilege escalation.
func needsSudo(backend string) bool {
	switch backend {
	case "apt", "apt-get", "pacman", "dnf":
		return true
	}
	return false
}

func readLine() string {
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Scan()
	return strings.TrimSpace(scanner.Text())
}

func runPkg(subcmd string, args []string, cfg *config.Config) {
	backend := detectBackend()
	ui.PrintHeader(cfg)

	if backend == "" {
		ui.Msg(cfg, ui.LevelError, "No supported package manager found (apt/dnf/pacman)")
		os.Exit(1)
	}

	// Warn about partial upgrades on Arch
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
		// Use real binary (apt or apt-get)
		realBackend := detectRealBackend()
		mapped, ok := cmdMap[backend][subcmd]
		if !ok {
			mapped = []string{realBackend, subcmd}
		} else {
			// Replace "apt" with real binary in mapped args
			mapped[0] = realBackend
		}
		runWithBackend(mapped, args, cfg, backend, subcmd)
	}
}

// splitFlags separates --flag/-f args from plain package names.
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

// fmtCmd formats a command+args display string, e.g. "pacman -S nano".
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

// isFilePath returns true if arg looks like a local file path.
func isFilePath(s string) bool {
	return strings.HasPrefix(s, "./") ||
		strings.HasPrefix(s, "/") ||
		strings.HasSuffix(s, ".pkg.tar.zst") ||
		strings.HasSuffix(s, ".pkg.tar.xz") ||
		strings.HasSuffix(s, ".deb") ||
		strings.HasSuffix(s, ".rpm")
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

	// Separate file paths from package names
	var filePkgs []string
	var namePkgs []string
	for _, p := range pkgs {
		if isFilePath(p) {
			filePkgs = append(filePkgs, p)
		} else {
			namePkgs = append(namePkgs, p)
		}
	}

	// Use pacman -Sp to check which named packages exist
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

	// repoPkgs = file paths + named packages that exist in repo
	repoPkgs := append([]string{}, filePkgs...)
	for _, p := range namePkgs {
		if !notFoundSet[p] {
			repoPkgs = append(repoPkgs, p)
		}
	}

	// Install repo packages
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

	// AUR fallback for not found
	if len(notFound) > 0 {
		fmt.Println()
		ui.Msgf(cfg, ui.LevelWarn, "Not found in repo: %s", strings.Join(notFound, " "))
		ui.Msgf(cfg, ui.LevelInfo, "Search AUR for %s%s%s?",
			cfg.Style.ColorBold, strings.Join(notFound, " "), cfg.Style.ColorReset+cfg.Style.ColorInfo)
		fmt.Print(cfg.Style.ColorReset)
		if ui.Confirm() {
			if err := aur.Install(notFound, noConfirm); err != nil {
				ui.Msgf(cfg, ui.LevelError, "%v", err)
			} else {
				ui.Msg(cfg, ui.LevelOK, "Done.")
			}
		} else {
			ui.Msg(cfg, ui.LevelWarn, "Skipped.")
		}
	}
}

// parseNotFound extracts package names from pacman "error: target not found: X" lines.
func parseNotFound(stderr string) []string {
	var missing []string
	for _, line := range strings.Split(stderr, "\n") {
		line = strings.TrimSpace(line)
		// pacman outputs: "error: target not found: <pkgname>"
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

	// Start AUR search in background immediately
	type aurResult struct {
		pkgs []aur.Package
		err  error
	}
	aurCh := make(chan aurResult, 1)
	go func() {
		pkgs, err := aur.SearchNarrow(query)
		aurCh <- aurResult{pkgs, err}
	}()

	// Repo search — local, fast
	ui.Msgf(cfg, ui.LevelInfo, "Searching '%s' in repo...", query)
	fmt.Println()
	repoCmd := exec.Command("pacman", "-Ss", query)
	repoCmd.Stdout = os.Stdout
	repoCmd.Stderr = os.Stderr

	repoCmd.Run()

	// AUR results — already running in background
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

func runWithBackend(cmdArgs []string, args []string, cfg *config.Config, backend, subcmd string) bool {
	sudo := needsSudo(backend)

	// Buat salinan untuk menghindari slice mutation
	fullArgs := make([]string, len(cmdArgs[1:]))
	copy(fullArgs, cmdArgs[1:])
	fullArgs = append(fullArgs, args...)

	// Format display: "install (apt install nano)" — tanpa trailing space kalau args kosong
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
			os.Exit(exitErr.ExitCode())
		}
		ui.Msgf(cfg, ui.LevelError, "%v", err)
		os.Exit(1)
	}
	ui.Msg(cfg, ui.LevelOK, "Done.")
	return true
}

// ensureSudo ensures privilege escalation is available.
func ensureSudo() error {
	return priv.Ensure()
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

	// Fetch all info in parallel
	names := make([]string, 0, len(installed))
	for name := range installed {
		names = append(names, name)
	}
	latest := aur.InfoBatch(names)

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

	for _, pkg := range outdated {
		ui.Msgf(cfg, ui.LevelInfo, "Upgrading %s%s%s...",
			cfg.Style.ColorBold, pkg.Name, cfg.Style.ColorReset+cfg.Style.ColorInfo)
		if err := aur.Install([]string{pkg.Name}, noConfirm); err != nil {
			ui.Msgf(cfg, ui.LevelError, "failed to upgrade %s: %v", pkg.Name, err)
		} else {
			ui.Msgf(cfg, ui.LevelOK, "%s upgraded.", pkg.Name)
		}
	}
}

// runRepo handles: alps repo update | list | install | remove | search | upgrade
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
		ui.Msg(cfg, ui.LevelInfo, "Updating alps-more repo...")
		fmt.Println()
		if err := ensureSudo(); err != nil {
			ui.Msg(cfg, ui.LevelError, "sudo authentication failed")
			os.Exit(1)
		}
		if err := more.FetchAndCache(cfg); err != nil {
			ui.Msgf(cfg, ui.LevelError, "update failed: %v", err)
			os.Exit(1)
		}
		ui.Msgf(cfg, ui.LevelOK, "Repo updated. Cache: %s", more.CachePath())

	case "list":
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
		entry, err := more.Find(pkgName, cfg)
		if err != nil {
			ui.Msgf(cfg, ui.LevelError, "%v", err)
			os.Exit(1)
		}

		if err := more.Validate(entry); err != nil {
			ui.Msgf(cfg, ui.LevelError, "%v", err)
			os.Exit(1)
		}

		ui.Msgf(cfg, ui.LevelInfo, "Install %s%s%s from alps-more?",
			cfg.Style.ColorBold, entry.Name, cfg.Style.ColorReset+cfg.Style.ColorInfo)
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
		entry, err := more.Find(pkgName, cfg)
		if err != nil {
			ui.Msgf(cfg, ui.LevelError, "%v", err)
			os.Exit(1)
		}

		ui.Msgf(cfg, ui.LevelInfo, "Remove %s%s%s from alps-more?",
			cfg.Style.ColorBold, entry.Name, cfg.Style.ColorReset+cfg.Style.ColorInfo)
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
		entry, err := more.Find(pkgName, cfg)
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
			// No package name → upgrade all
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

// isArch returns true if running on an Arch-based distro.
func isArch() bool {
	_, err := exec.LookPath("pacman")
	return err == nil
}

// runAUR handles: alps aur install|search|list|remove|clean|build-local|fetch-abs
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
		// SearchNarrow: first word hits the API, remaining words narrow in-memory
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
		// Populate completion cache so tab-complete works for future installs
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

func runAptWithSnapFallback(args []string, cfg *config.Config) {
	if len(args) == 0 {
		ui.Msg(cfg, ui.LevelError, "Package name required")
		return
	}

	pkgs, noConfirm := splitFlags(args)
	realBackend := detectRealBackend()

	ui.Msgf(cfg, ui.LevelInfo, "install (%s install %s)", realBackend, strings.Join(pkgs, " "))
	fmt.Println()

	// Separate file paths from package names
	var notFound []string
	var repoPkgs []string
	for _, pkg := range pkgs {
		if isFilePath(pkg) {
			// file path — install directly, no check needed
			repoPkgs = append(repoPkgs, pkg)
			continue
		}
		// check with apt-cache show (silent, no LANG=C needed)
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

	// Install repo packages — full output in user's language
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

	// Snap fallback for not found
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
	realBackend := detectRealBackend()

	// Start snap search in background if available
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

// runFlatpak handles: alps flatpak install|remove|search|list|update
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

// runSnap handles: alps snap install|remove|search|list|update
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

// isTermux returns true when running inside Termux on Android.
func isTermux() bool {
	return os.Getenv("TERMUX_VERSION") != "" ||
		os.Getenv("PREFIX") == "/data/data/com.termux/files/usr"
}

// appendAURNamesCache writes package names from search results into the AUR
// name cache used by shell completions. Duplicates are harmless — shells dedup.
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

// printDiagnostic shows a quick system overview when alps is run with no args.
func printDiagnostic(cfg *config.Config) {
	ui.PrintHeader(cfg)

	var distro string
	if isTermux() {
		distro = "Termux"
		if v := os.Getenv("TERMUX_VERSION"); v != "" {
			distro = "Termux " + v
		}

		// FIX: Use absolute path to Android's native getprop binary
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
