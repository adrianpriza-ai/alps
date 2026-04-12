package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"alps/aur"
	"alps/config"
	"alps/ui"
)

func main() {
	cfg := config.Load()

	if len(os.Args) < 2 {
		ui.PrintHelp(cfg)
		os.Exit(0)
	}

	cmd := os.Args[1]
	args := os.Args[2:]

	switch cmd {
	case "help", "--help", "-h":
		ui.PrintHelp(cfg)
	case "aliases":
		ui.PrintAliases(cfg)
	case "config-show":
		ui.PrintConfigShow(cfg)
	case "version", "--version":
		fmt.Printf("%salps%s v1.0.0\n", cfg.Style.ColorPrimary, cfg.Style.ColorReset)
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
		"install":      {"apt-get", "install"},
		"remove":       {"apt-get", "remove"},
		"purge":        {"apt-get", "purge"},
		"update":       {"apt-get", "update"},
		"upgrade":      {"apt-get", "upgrade"},
		"full-upgrade": {"apt-get", "dist-upgrade"},
		"search":       {"apt-cache", "search"},
		"show":         {"apt-cache", "show"},
		"list":         {"dpkg", "-l"},
		"autoremove":   {"apt-get", "autoremove"},
		"autoclean":    {"apt-get", "autoclean"},
		"clean":        {"apt-get", "clean"},
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
		"install":    {"pacman", "-S"},
		"remove":     {"pacman", "-R"},
		"purge":      {"pacman", "-Rns"},
		"update":     {"pacman", "-Sy"},
		"upgrade":    {"pacman", "-Su"},
		"search":     {"pacman", "-Ss"},
		"show":       {"pacman", "-Si"},
		"list":       {"pacman", "-Q"},
		"autoremove": {"pacman", "-Qdtq"},
		"clean":      {"pacman", "-Sc"},
	},
}

func runPkg(subcmd string, args []string, cfg *config.Config) {
	backend := detectBackend()

	ui.PrintHeader(cfg)

	if backend == "" {
		ui.Msg(cfg, ui.LevelError, "No supported package manager found (apt/dnf/pacman)")
		os.Exit(1)
	}

	if backend == "pacman" && subcmd == "install" {
		runPacmanWithAURFallback(subcmd, args, cfg)
		return
	}

	if backend == "pacman" && subcmd == "search" {
		runPacmanSearch(args, cfg)
		return
	}

	mapped, ok := cmdMap[backend][subcmd]
	if !ok {
		mapped = []string{backend, subcmd}
	}

	runWithBackend(mapped, args, cfg, backend, subcmd)
}

func runPacmanWithAURFallback(subcmd string, args []string, cfg *config.Config) {
	if len(args) == 0 {
		ui.Msg(cfg, ui.LevelError, "Package name required")
		return
	}

	noConfirm := false
	pkgs := []string{}
	for _, a := range args {
		if a == "--noconfirm" || a == "-y" {
			noConfirm = true
		} else {
			pkgs = append(pkgs, a)
		}
	}

	for _, pkg := range pkgs {
		ui.Msgf(cfg, ui.LevelInfo, "Looking for %s%s%s in repo...",
			cfg.Style.ColorBold, pkg, cfg.Style.ColorReset+cfg.Style.ColorInfo)

		pacmanArgs := []string{"-S", pkg}
		if noConfirm {
			pacmanArgs = append(pacmanArgs, "--noconfirm")
		}
		cmd := exec.Command("sudo", append([]string{"pacman"}, pacmanArgs...)...)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		cmd.Stdin = os.Stdin

		if err := cmd.Run(); err == nil {
			ui.Msg(cfg, ui.LevelOK, "Done.")
			continue
		}

		ui.Msgf(cfg, ui.LevelWarn, "%s not found in repo, searching AUR...", pkg)

		if !aur.Exists(pkg) {
			ui.Msgf(cfg, ui.LevelError, "%s not found in repo or AUR", pkg)
			continue
		}

		ui.Msgf(cfg, ui.LevelInfo, "Found in AUR, installing %s%s%s...",
			cfg.Style.ColorBold, pkg, cfg.Style.ColorReset+cfg.Style.ColorInfo)

		if err := aur.Install(pkg, noConfirm); err != nil {
			ui.Msgf(cfg, ui.LevelError, "%v", err)
			continue
		}
		ui.Msg(cfg, ui.LevelOK, "Done.")
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

func runWithBackend(cmdArgs []string, args []string, cfg *config.Config, backend, subcmd string) {
	needsSudo := backend == "apt" || backend == "pacman"
	fullArgs := append(cmdArgs[1:], args...)

	ui.Msgf(cfg, ui.LevelInfo, "%s %s%s%s %s",
		cmdArgs[0], cfg.Style.ColorBold, subcmd,
		cfg.Style.ColorReset+cfg.Style.ColorInfo,
		strings.Join(args, " "))
	fmt.Println()

	needsProgress := subcmd == "update" || subcmd == "upgrade" ||
		subcmd == "install" || subcmd == "remove" || subcmd == "purge" ||
		subcmd == "full-upgrade"

	var stop func()
	if needsProgress {
		stop = ui.StartProgress(cfg, "Running "+backend+" "+subcmd)
	}

	var cmd *exec.Cmd
	if needsSudo {
		cmd = exec.Command("sudo", append([]string{cmdArgs[0]}, fullArgs...)...)
	} else {
		cmd = exec.Command(cmdArgs[0], fullArgs...)
	}

	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin

	if stop != nil {
		time.Sleep(300 * time.Millisecond)
		stop()
	}

	// Confirmation before running
	fmt.Printf("  Continue? [Y/n] ")
	var input string
	fmt.Scanln(&input)
	input = strings.ToLower(strings.TrimSpace(input))
	if input != "" && input != "y" && input != "yes" {
		ui.Msg(cfg, ui.LevelWarn, "Cancelled.")
		return
	}

	err := cmd.Run()
	fmt.Println()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			ui.Msgf(cfg, ui.LevelError, "%s exited with code %d", backend, exitErr.ExitCode())
			os.Exit(exitErr.ExitCode())
		}
		ui.Msgf(cfg, ui.LevelError, "%v", err)
		os.Exit(1)
	}
	ui.Msg(cfg, ui.LevelOK, "Done.")
}
