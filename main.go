package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"alps/aur"
	"alps/completion"
	"alps/config"
	"alps/flatpak"
	"alps/more"
	"alps/snap"
	"alps/ui"
)

const version = "v0.6 by \033]8;;https://github.com/adrianpriza-ai\aadrianpriza-ai\033]8;;\a"

func main() {
	cfg := config.Load()

	if len(os.Args) < 2 {
		ui.PrintHelp(cfg)
		os.Exit(0)
	}

	cmd := os.Args[1]
	args := os.Args[2:]

	switch cmd {
	case "version", "--version":
		fmt.Printf("%salps%s %s\n", cfg.Style.ColorPrimary, cfg.Style.ColorReset, version)
	case "completion":
		if len(args) == 0 {
			fmt.Fprintln(os.Stderr, "Usage: alps completion <fish|bash|zsh>")
			os.Exit(1)
		}
		completion.Generate(args[0], cfg)
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
		resolved := resolveAlias(cmd, cfg)
		runPkg(resolved, args, cfg)
	}
}

func resolveAlias(cmd string, cfg *config.Config) string {
	if real, ok := cfg.Aliases[cmd]; ok {
		return real
	}
	return cmd
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

func runPkg(subcmd string, args []string, cfg *config.Config) {
	backend := detectBackend()
	ui.PrintHeader(cfg)

	if backend == "" {
		ui.Msg(cfg, ui.LevelError, "No supported package manager found (apt/dnf/pacman)")
		os.Exit(1)
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
	ui.Msg(cfg, ui.LevelInfo, "Removing orphaned packages...")
	if !ui.Confirm() {
		ui.Msg(cfg, ui.LevelWarn, "Cancelled.")
		return
	}
	cmd := exec.Command("bash", "-c", "sudo pacman -Rns $(pacman -Qdtq)")
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

	ui.Msgf(cfg, ui.LevelInfo, "install (pacman -S %s)", strings.Join(pkgs, " "))
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
		cmd := exec.Command("sudo", append([]string{"pacman"}, pacmanArgs...)...)
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
		pkgs, err := aur.Search(query)
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
		cfg.Style.ColorReset+cfg.Style.ColorInfo)
	fmt.Print(cfg.Style.ColorReset)
	fmt.Println()

	if sudo {
		if err := ensureSudo(); err != nil {
			ui.Msg(cfg, ui.LevelError, "sudo authentication failed")
			return false
		}
	}

	var cmd *exec.Cmd
	if sudo {
		cmd = exec.Command("sudo", append([]string{cmdArgs[0]}, fullArgs...)...)
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

// ensureSudo ensures we have a valid sudo token, prompting only when needed.
func ensureSudo() error {
	if exec.Command("sudo", "-n", "true").Run() == nil {
		return nil
	}
	fmt.Println()
	pw := exec.Command("sudo", "-v")
	pw.Stdout = os.Stdout
	pw.Stderr = os.Stderr
	pw.Stdin = os.Stdin
	return pw.Run()
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

// runRepo handles: alps repo update | list | install <pkg> | remove <pkg>
func runRepo(args []string, cfg *config.Config) {
	ui.PrintHeader(cfg)

	if len(args) == 0 {
		ui.Msg(cfg, ui.LevelError, "Usage: alps repo <update|list|install|remove> [package]")
		os.Exit(1)
	}

	subcmd := args[0]
	rest := args[1:]

	switch subcmd {
	case "update":
		ui.Msg(cfg, ui.LevelInfo, "Updating alps-more repo...")
		fmt.Println()
		if err := ensureSudo(); err != nil {
			ui.Msg(cfg, ui.LevelError, "sudo authentication failed")
			os.Exit(1)
		}
		if err := more.FetchAndCache(); err != nil {
			ui.Msgf(cfg, ui.LevelError, "update failed: %v", err)
			os.Exit(1)
		}
		ui.Msgf(cfg, ui.LevelOK, "Repo updated. Cache: %s", more.CachePath())

	case "list":
		entries, err := more.List()
		if err != nil {
			ui.Msgf(cfg, ui.LevelError, "%v", err)
			os.Exit(1)
		}
		if len(entries) == 0 {
			ui.Msg(cfg, ui.LevelWarn, "No packages in repo.")
			return
		}
		fmt.Println()
		for _, e := range entries {
			fmt.Printf("  %s%s%s  %s%s%s  \033[2m[%s]\033[0m\n",
				cfg.Style.ColorPrimary, e.Name, cfg.Style.ColorReset,
				cfg.Style.ColorDim, e.Desc, cfg.Style.ColorReset,
				strings.Join(e.Arch, ", "))
		}
		fmt.Println()

	case "install":
		if len(rest) == 0 {
			ui.Msg(cfg, ui.LevelError, "Usage: alps repo install <package>")
			os.Exit(1)
		}
		pkgName := rest[0]
		entry, err := more.Find(pkgName)
		if err != nil {
			ui.Msgf(cfg, ui.LevelError, "%v", err)
			os.Exit(1)
		}

		// Validate arch, os, deps — stop on any failure
		if err := more.Validate(entry); err != nil {
			ui.Msgf(cfg, ui.LevelError, "%v", err)
			os.Exit(1)
		}

		ui.Msgf(cfg, ui.LevelInfo, "Install %s%s%s from alps-more?",
			cfg.Style.ColorBold, entry.Name, cfg.Style.ColorReset+cfg.Style.ColorInfo)
		if entry.Desc != "" {
			fmt.Printf("  %s%s%s\n", cfg.Style.ColorDim, entry.Desc, cfg.Style.ColorReset)
		}
		fmt.Println()
		for _, line := range entry.CmdLines {
			fmt.Printf("  %s$ %s%s\n", cfg.Style.ColorDim, line, cfg.Style.ColorReset)
		}
		fmt.Print(cfg.Style.ColorReset)
		fmt.Println()
		if !ui.Confirm() {
			ui.Msg(cfg, ui.LevelWarn, "Cancelled.")
			return
		}

		fmt.Println()
		if err := more.Install(entry); err != nil {
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
		entry, err := more.Find(pkgName)
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
		if err := more.Remove(entry); err != nil {
			ui.Msgf(cfg, ui.LevelError, "%v", err)
			os.Exit(1)
		}
		ui.Msg(cfg, ui.LevelOK, "Done.")

	default:
		ui.Msgf(cfg, ui.LevelError, "Unknown repo subcommand: %s", subcmd)
		os.Exit(1)
	}
}

// runAUR handles: alps aur install | search | list | clean
func runAUR(args []string, cfg *config.Config) {
	ui.PrintHeader(cfg)

	if len(args) == 0 {
		ui.Msg(cfg, ui.LevelError, "Usage: alps aur <install|search|list|clean> [args]")
		os.Exit(1)
	}

	subcmd := args[0]
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
		results, err := aur.Search(query)
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
		if err := os.RemoveAll(cacheRoot); err != nil {
			ui.Msgf(cfg, ui.LevelError, "failed to remove cache: %v", err)
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
		ui.Msg(cfg, ui.LevelError, "sudo authentication failed")
		return
	}

	// Install repo packages — full output in user's language
	if len(repoPkgs) > 0 {
		aptArgs := append([]string{realBackend, "install"}, repoPkgs...)
		if noConfirm {
			aptArgs = append(aptArgs, "-y")
		}
		cmd := exec.Command("sudo", aptArgs...)
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

	subcmd := args[0]
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

	subcmd := args[0]
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
