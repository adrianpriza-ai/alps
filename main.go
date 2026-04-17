package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"alps/aur"
	"alps/completion"
	"alps/config"
	"alps/more"
	"alps/ui"
)

const version = "v0.6"

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
	for _, b := range []string{"apt", "dnf", "pacman"} {
		if _, err := exec.LookPath(b); err == nil {
			return b
		}
	}
	return ""
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
	"dnf": {
		"install":    {"dnf", "install"},
		"remove":     {"dnf", "remove"},
		"purge":      {"dnf", "remove"},
		"update":     {"dnf", "check-update"},
		"upgrade":    {"dnf", "upgrade"},
		"search":     {"dnf", "search"},
		"show":       {"dnf", "info"},
		"list":       {"dnf", "list"},
		"autoremove": {"dnf", "autoremove"},
		"clean":      {"dnf", "clean", "all"},
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
	case "apt", "pacman", "dnf":
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
	default:
		mapped, ok := cmdMap[backend][subcmd]
		if !ok {
			mapped = []string{backend, subcmd}
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

func runPacmanWithAURFallback(args []string, cfg *config.Config) {
	if len(args) == 0 {
		ui.Msg(cfg, ui.LevelError, "Package name required")
		return
	}

	pkgs, noConfirm := splitFlags(args)

	ui.Msgf(cfg, ui.LevelInfo, "install (pacman -S %s)", strings.Join(pkgs, " "))
	fmt.Println()

	// Pisahkan paket repo vs AUR
	var repoPkgs []string
	var aurPkgs []string

	for _, pkg := range pkgs {
		ui.Msgf(cfg, ui.LevelInfo, "Looking for %s%s%s in repo...",
			cfg.Style.ColorBold, pkg, cfg.Style.ColorReset+cfg.Style.ColorInfo)
		fmt.Print(cfg.Style.ColorReset)

		checkCmd := exec.Command("pacman", "-Si", pkg)
		checkCmd.Stdout = nil
		checkCmd.Stderr = nil

		if err := checkCmd.Run(); err != nil {
			ui.Msgf(cfg, ui.LevelWarn, "%s not found in repo, will try AUR.", pkg)
			aurPkgs = append(aurPkgs, pkg)
		} else {
			repoPkgs = append(repoPkgs, pkg)
		}
	}

	// Install repo packages sekaligus
	if len(repoPkgs) > 0 {
		ui.Msgf(cfg, ui.LevelInfo, "Install %s%s%s from repo?",
			cfg.Style.ColorBold, strings.Join(repoPkgs, " "), cfg.Style.ColorReset+cfg.Style.ColorInfo)
		fmt.Print(cfg.Style.ColorReset)
		if ui.Confirm() {
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
		} else {
			ui.Msg(cfg, ui.LevelWarn, "Skipped.")
		}
	}

	// Install AUR packages
	if len(aurPkgs) > 0 {
		ui.Msgf(cfg, ui.LevelInfo, "Search AUR for %s%s%s?",
			cfg.Style.ColorBold, strings.Join(aurPkgs, " "), cfg.Style.ColorReset+cfg.Style.ColorInfo)
		fmt.Print(cfg.Style.ColorReset)
		if ui.Confirm() {
			if err := aur.Install(aurPkgs, noConfirm); err != nil {
				ui.Msgf(cfg, ui.LevelError, "%v", err)
			} else {
				ui.Msg(cfg, ui.LevelOK, "Done.")
			}
		} else {
			ui.Msg(cfg, ui.LevelWarn, "Skipped.")
		}
	}
}

func runPacmanSearch(args []string, cfg *config.Config) {
	if len(args) == 0 {
		ui.Msg(cfg, ui.LevelError, "Search query required")
		return
	}
	query := strings.Join(args, " ")

	ui.Msgf(cfg, ui.LevelInfo, "Searching '%s' in repo...", query)
	fmt.Println()
	repoCmd := exec.Command("pacman", "-Ss", query)
	repoCmd.Stdout = os.Stdout
	repoCmd.Stderr = os.Stderr
	repoCmd.Run()

	fmt.Println()
	ui.Msgf(cfg, ui.LevelInfo, "Searching '%s' in AUR...", query)
	fmt.Println()

	results, err := aur.Search(query)
	if err != nil {
		ui.Msgf(cfg, ui.LevelError, "%v", err)
		return
	}
	if len(results) == 0 {
		ui.Msg(cfg, ui.LevelWarn, "No results found in AUR")
		return
	}

	max := 10
	if len(results) < max {
		max = len(results)
	}
	for _, p := range results[:max] {
		fmt.Printf("  %saur/%s%s %s%s%s\n",
			cfg.Style.ColorPrimary, p.Name, cfg.Style.ColorReset,
			cfg.Style.ColorDim, p.Version, cfg.Style.ColorReset)
		fmt.Printf("    %s%s%s  \033[2m(votes: %d)\033[0m\n",
			cfg.Style.ColorDim, p.Description, cfg.Style.ColorReset, p.Votes)
	}
	if len(results) > 10 {
		ui.Msgf(cfg, ui.LevelInfo, "...and %d more results", len(results)-10)
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

	var outdated []aur.Package
	for name, installedVer := range installed {
		pkg, err := aur.Info(name)
		if err != nil {
			continue
		}
		if pkg.Version != installedVer {
			outdated = append(outdated, *pkg)
			fmt.Printf("  %s%s%s  %s%s%s -> %s%s%s\n",
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
