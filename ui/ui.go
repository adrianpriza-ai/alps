package ui

import (
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"

	"github.com/adrianpriza-ai/alps/config"
	"github.com/adrianpriza-ai/alps/more"
	"github.com/adrianpriza-ai/alps/pack"
)

type Level int

const (
	LevelOK Level = iota
	LevelError
	LevelWarn
	LevelInfo
)

func sym(cfg *config.Config, l Level) (string, string) {
	s := cfg.Style
	switch l {
	case LevelOK:
		return s.ColorSuccess, s.SymOK
	case LevelError:
		return s.ColorError, s.SymErr
	case LevelWarn:
		return s.ColorWarning, s.SymWarn
	default:
		return s.ColorInfo, s.SymInfo
	}
}

func Msg(cfg *config.Config, l Level, text string) {
	color, symbol := sym(cfg, l)
	fmt.Printf("  %s%s%s  %s%s\n", color, symbol, cfg.Style.ColorReset, text, cfg.Style.ColorReset)
}

func Msgf(cfg *config.Config, l Level, format string, a ...any) {
	color, symbol := sym(cfg, l)
	text := fmt.Sprintf(format, a...)
	fmt.Printf("  %s%s%s  %s%s\n", color, symbol, cfg.Style.ColorReset, text, cfg.Style.ColorReset)
}

// Confirm prompts for confirmation.
func Confirm() bool {
	fmt.Print("  Continue? [Y/n] ")
	var input string
	fmt.Scanln(&input)
	input = strings.ToLower(strings.TrimSpace(input))
	return input == "" || input == "y" || input == "yes"
}

func PrintHeader(cfg *config.Config) {
	if !cfg.Style.ShowHeader {
		return
	}

	if cfg.Style.TitleStyle == "custom" && len(cfg.Style.HeaderLines) > 0 {
		fmt.Println()
		for _, line := range cfg.Style.HeaderLines {
			fmt.Println(line)
		}
		fmt.Println()
		return
	}

	term := os.Getenv("TERM")
	if term == "linux" || term == "" {
		fmt.Printf("\n  \033[1;97mALPS\033[0m  \033[2mAdvanced Linux Package System · %s\033[0m\n\n", cfg.Version)
		return
	}

	version := cfg.Version
	if len(version) > 6 {
		version = version[:6]
	}

	fmt.Print("\n\033[97m                   /^\\\n")
	fmt.Print("\033[97m   ALPS\033[37m        /^\\/   \\/\\\n")
	fmt.Printf("\033[37m     %-6s%3s\033[1;32m/___\\____\\_\\\033[0m\n\n", version, "")
}

// printSectionTitle prints a bold, compact section heading (no rule line,
// to keep `alps help` short).
func printSectionTitle(cfg *config.Config, title string) {
	s := cfg.Style
	fmt.Printf("  %s%s%s\n", s.ColorBold, title, s.ColorReset)
}

// padRight pads str with spaces up to width w (minimum one space), used to
// line up columns without relying on fmt's own field-width verbs.
func padRight(str string, w int) string {
	pad := w - len(str)
	if pad < 1 {
		pad = 1
	}
	return str + strings.Repeat(" ", pad)
}

// printRow prints one "bullet  command   description" line, padding the command column to cmdW.
// Built with string concatenation (not a single Printf) so the number of %s verbs can never drift out of sync.
func printRow(cfg *config.Config, cmdW int, cmd, desc string) {
	s := cfg.Style
	line := "  " + s.ColorDim + s.SymBullet + s.ColorReset + " " +
		s.ColorPrimary + padRight(cmd, cmdW) + s.ColorReset +
		s.ColorDim + desc + s.ColorReset
	fmt.Println(line)
}

func printRows(cfg *config.Config, cmdW int, rows [][2]string) {
	for _, r := range rows {
		printRow(cfg, cmdW, r[0], r[1])
	}
}

func PrintHelp(cfg *config.Config) {
	s := cfg.Style
	PrintHeader(cfg)
	fmt.Printf("  %sUsage%s   alps %s<command>%s [args]  ·  %salps aliases%s for shortcuts\n\n",
		s.ColorBold, s.ColorReset, s.ColorPrimary, s.ColorReset, s.ColorDim, s.ColorReset)

	printSectionTitle(cfg, "Core")
	printRows(cfg, 19, [][2]string{
		{"install <pkg>", "install repo/AUR/more pkg"},
		{"remove <pkg>", "remove a package"},
		{"purge <pkg>", "remove + config/data"},
		{"search <query>", "search repo + AUR"},
		{"show <pkg>", "show package info"},
		{"list", "list installed packages"},
		{"update", "refresh package indexes"},
		{"upgrade", "upgrade system + AUR"},
		{"full-upgrade", "safe pacman -Syu"},
		{"autoremove", "remove orphaned pkgs"},
		{"autoclean", "clean package cache"},
		{"clean", "remove cached packages"},
		{"edit-sources", "edit repo source list"},
		{"completion <shell>", "generate shell completion"},
		{"help", "show this help"},
		{"aliases", "show active aliases"},
		{"config-show", "show config & paths"},
		{"version", "binary version"},
	})
	fmt.Println()

	printSectionTitle(cfg, "Flags")
	printRows(cfg, 20, [][2]string{
		{"-n, --dry-run", "simulate, no changes written"},
		{"-y, --noconfirm", "skip confirmation prompts (main backends)"},
		{"-v, --verbose", "enable verbose output"},
		{"-q, --quiet", "suppress non-error output"},
		{"-f, --force", "force operation (skip safety checks)"},
	})
	fmt.Println()

	printSectionTitle(cfg, "Repo")
	printRows(cfg, 22, [][2]string{
		{"repo update", "refresh alps-more cache"},
		{"repo list", "list available packages"},
		{"repo list install", "list installed packages"},
		{"repo list remove", "list stale packages"},
		{"repo install <pkg|url>", "install pkg or remote ALPSMORE"},
		{"repo remove <pkg>", "remove alps-more package"},
		{"repo purge <pkg>", "remove pkg + config/data"},
		{"repo search <query>", "search alps-more packages"},
		{"repo upgrade [pkg]", "upgrade installed package(s)"},
		{"repo clean", "remove build cache"},
	})
	fmt.Println()

	// Distro-specific subcommands
	distro := detectDistroID()
	if isArchBased(distro) {
		printSectionTitle(cfg, "AUR")
		printRows(cfg, 21, [][2]string{
			{"aur install <pkg>", "install directly from AUR"},
			{"aur search <query>", "search AUR only"},
			{"aur list", "list installed AUR packages"},
			{"aur remove <pkg>", "remove via pacman -R"},
			{"aur clean", "remove build cache"},
			{"aur build-local [dir]", "build a local PKGBUILD"},
			{"aur fetch-abs <pkg>", "fetch official PKGBUILD"},
		})
		fmt.Println()
		fmt.Printf("  %s%s%s %sArch tip:%s use %sfull-upgrade%s, not update/upgrade — avoids partial upgrades\n\n",
			s.ColorWarning, s.SymWarn, s.ColorReset,
			s.ColorBold, s.ColorReset,
			s.ColorPrimary, s.ColorReset)
	}

	if isDebianBased(distro) && isSnapAvailable() {
		printSectionTitle(cfg, "Snap")
		printRows(cfg, 19, [][2]string{
			{"snap install <pkg>", "install via snap"},
			{"snap search <query>", "search snap store"},
			{"snap list", "list installed snaps"},
			{"snap update", "refresh all snaps"},
			{"snap remove <pkg>", "remove snap package"},
		})
		fmt.Println()
	}

	if isFlatpakAvailable() {
		printSectionTitle(cfg, "Flatpak")
		printRows(cfg, 23, [][2]string{
			{"flatpak install <pkg>", "install from flathub"},
			{"flatpak search <query>", "search flathub"},
			{"flatpak list", "list installed flatpaks"},
			{"flatpak update", "update all flatpaks"},
			{"flatpak remove <pkg>", "remove flatpak"},
		})
		fmt.Println()
	}

	fmt.Printf("  %sOther commands are passed directly to the backend.%s\n\n", s.ColorDim, s.ColorReset)
}

// detectDistroID reads /etc/os-release ID.
func detectDistroID() string {
	data, err := os.ReadFile("/etc/os-release")
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "ID=") {
			return strings.ToLower(strings.Trim(line[3:], `"'`))
		}
	}
	return ""
}

func isArchBased(distro string) bool {
	for _, d := range []string{"arch", "manjaro", "endeavouros", "garuda", "artix"} {
		if distro == d {
			return true
		}
	}
	return false
}

func isDebianBased(distro string) bool {
	for _, d := range []string{"debian", "ubuntu", "linuxmint", "pop", "elementary", "kali"} {
		if distro == d {
			return true
		}
	}
	return false
}

func isSnapAvailable() bool {
	if _, err := exec.LookPath("snap"); err != nil {
		return false
	}
	if _, err := os.Stat("/etc/apt/preferences.d/nosnap.pref"); err == nil {
		return false
	}
	return true
}

func isFlatpakAvailable() bool {
	_, err := exec.LookPath("flatpak")
	return err == nil
}

// aliasColWidth is the padding width for the short-alias column so the
// arrow and target command line up across every row.
const aliasColWidth = 8

// printAliasRow prints one "short -> target" line with aligned columns.
// Built with string concatenation (not a single Printf) to avoid the %s-verb / argument-count mismatch.
func printAliasRow(cfg *config.Config, short, target string) {
	s := cfg.Style
	line := "  " + s.ColorPrimary + padRight(short, aliasColWidth) + s.ColorReset +
		s.ColorDim + s.SymArrow + s.ColorReset + "  " + target
	fmt.Println(line)
}

func PrintAliases(cfg *config.Config) {
	s := cfg.Style
	PrintHeader(cfg)

	printSectionTitle(cfg, "Active Aliases")
	keys := sortedKeys(cfg.Aliases)
	for _, k := range keys {
		printAliasRow(cfg, k, cfg.Aliases[k])
	}
	fmt.Println()

	distro := detectDistroID()
	if isArchBased(distro) {
		printSectionTitle(cfg, "AUR Subcommand Aliases")
		subKeys := sortedKeys(config.DefaultSubCmdAliases)
		for _, k := range subKeys {
			printAliasRow(cfg, k, config.DefaultSubCmdAliases[k])
		}
		fmt.Println()
	}

	fmt.Printf("  %sDefine your own in /etc/alps/config or ~/.config/alps/config (alias_<name> = <command>)%s\n\n",
		s.ColorDim, s.ColorReset)
}

func PrintConfigShow(cfg *config.Config) {
	s := cfg.Style
	PrintHeader(cfg)
	fmt.Printf("  %sConfig paths:%s\n", s.ColorBold, s.ColorReset)
	printConfigPath(cfg, cfg.GlobalPath)
	printConfigPath(cfg, cfg.UserPath)
	fmt.Println()
	fmt.Printf("  %sStyle preview:%s\n", s.ColorBold, s.ColorReset)
	fmt.Printf("  %s%s%s ok    %s%s%s error    %s%s%s warn    %s%s%s info\n\n",
		s.ColorSuccess, s.SymOK, s.ColorReset,
		s.ColorError, s.SymErr, s.ColorReset,
		s.ColorWarning, s.SymWarn, s.ColorReset,
		s.ColorInfo, s.SymInfo, s.ColorReset)
	fmt.Printf("  %sTitle style:%s  %s%s%s\n\n",
		s.ColorBold, s.ColorReset, s.ColorPrimary, s.TitleStyle, s.ColorReset)
}

func printConfigPath(cfg *config.Config, path string) {
	s := cfg.Style
	if _, err := os.Stat(path); err == nil {
		fmt.Printf("  %s%s%s  %s [loaded]\n", s.ColorSuccess, s.SymOK, s.ColorReset, path)
	} else {
		fmt.Printf("  %s%s%s  %s%s (not found)%s\n",
			s.ColorDim, s.SymBullet, s.ColorReset, s.ColorDim, path, s.ColorReset)
	}
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// symUpgrade returns an upgrade arrow.
func symUpgrade() string {
	if isTTY() {
		return "->"
	}
	return "↑"
}

// symReinstall returns a reinstall symbol.
func symReinstall() string {
	if isTTY() {
		return ">>"
	}
	return "⟳"
}

// isTermux checks if running in Termux environment.
func isTermux() bool {
	return os.Getenv("TERMUX_VERSION") != "" ||
		os.Getenv("PREFIX") == "/data/data/com.termux/files/usr"
}

// isTTY checks for basic TTY.
func isTTY() bool {
	term := os.Getenv("TERM")
	return term == "linux" || term == "dumb" || term == ""
}

// PrintDiagnostic displays system diagnostic information.
func PrintDiagnostic(cfg *config.Config) {
	PrintHeader(cfg)

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

	backend := pack.DetectName()
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

// PrintRepoEntry prints a repo list entry.
func PrintRepoEntry(cfg *config.Config, name, version, desc string, arch []string, installedVer string) {
	s := cfg.Style

	verStr := ""
	if version != "" {
		verStr = fmt.Sprintf(" %s%s%s", s.ColorDim, version, s.ColorReset)
	}

	instTag := ""
	if installedVer != "" {
		label := installedVer
		if label == "" {
			label = "installed"
		}
		instTag = fmt.Sprintf(" %s[%s]%s", s.ColorSuccess, label, s.ColorReset)
	}

	archStr := ""
	if len(arch) > 0 {
		archStr = fmt.Sprintf(" %s[%s]%s", s.ColorDim, strings.Join(arch, ", "), s.ColorReset)
	}

	fmt.Printf("  %s%s%s%s%s  %s%s%s%s\n",
		s.ColorPrimary, name, s.ColorReset,
		verStr, instTag,
		s.ColorDim, desc, s.ColorReset,
		archStr)
}

// PrintRepoSearchResult prints a repo search result.
func PrintRepoSearchResult(cfg *config.Config, name, version, desc string) {
	s := cfg.Style

	verStr := ""
	if version != "" {
		verStr = fmt.Sprintf(" %s%s%s", s.ColorDim, version, s.ColorReset)
	}

	fmt.Printf("  %s%s%s%s  %s\n",
		s.ColorPrimary, name, s.ColorReset,
		verStr, desc)
}

// PrintUpgradeStatus prints upgrade status.
func PrintUpgradeStatus(cfg *config.Config, name, fromVer, toVer string) {
	s := cfg.Style
	arrow := symUpgrade()
	if fromVer != "" && toVer != "" {
		fmt.Printf("  %s%s%s  %s: %s%s%s %s %s%s%s\n",
			s.ColorInfo, arrow, s.ColorReset,
			name,
			s.ColorDim, fromVer, s.ColorReset,
			arrow,
			s.ColorSuccess, toVer, s.ColorReset)
	} else {
		fmt.Printf("  %s%s%s  %s: update available\n",
			s.ColorInfo, arrow, s.ColorReset, name)
	}
}

// PrintReinstallStatus prints reinstall status.
func PrintReinstallStatus(cfg *config.Config, name, version string) {
	s := cfg.Style
	sym := symReinstall()
	if version != "" {
		fmt.Printf("  %s%s%s  %s %s%s%s already up to date — reinstalling...\n",
			s.ColorWarning, sym, s.ColorReset,
			name, s.ColorDim, version, s.ColorReset)
	} else {
		fmt.Printf("  %s%s%s  %s already installed — reinstalling...\n",
			s.ColorWarning, sym, s.ColorReset, name)
	}
}
