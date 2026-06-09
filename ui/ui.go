package ui

import (
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"

	"github.com/adrianpriza-ai/alps/config"
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
		fmt.Printf("\n  \033[1;97mALPS\033[0m \033[2m%s\033[0m\n\n", cfg.Version)
		return
	}
	fmt.Print("\n\033[97m                   /^\\ \n")
	fmt.Print("\033[97m   ALPS\033[37m        /^\\/   \\/\\ \n")
	fmt.Print("\033[37m     v0.9     \033[1;32m/___\\____\\_\\\033[0m\n\n")
}

func PrintHelp(cfg *config.Config) {
	s := cfg.Style
	PrintHeader(cfg)
	fmt.Printf("  %sUsage:%s  alps %s<command>%s [args]\n\n",
		s.ColorBold, s.ColorReset, s.ColorPrimary, s.ColorReset)

	fmt.Printf("  %sBuilt-in:%s\n", s.ColorBold, s.ColorReset)
	builtins := [][2]string{
		{"help", "show this help"},
		{"aliases", "show active aliases"},
		{"config-show", "show active config & paths"},
		{"version", "binary version"},
	}
	for _, b := range builtins {
		fmt.Printf("  %s%s%s  %-24s %s%s%s\n",
			s.ColorDim, s.SymBullet, s.ColorReset,
			s.ColorPrimary+b[0]+s.ColorReset,
			s.ColorDim, b[1], s.ColorReset)
	}
	fmt.Println()

	fmt.Printf("  %sRepo:%s\n", s.ColorBold, s.ColorReset)
	repoSubs := [][2]string{
		{"repo update", "refresh alps-more cache"},
		{"repo list", "list available packages"},
		{"repo list install", "list installed packages"},
		{"repo list remove", "list stale packages"},
		{"repo install <pkg|url>", "install package or remote ALPSMORE file"},
		{"repo remove <pkg>", "remove alps-more package"},
		{"repo purge <pkg>", "remove package including configs/data"},
		{"repo search <query>", "search alps-more packages"},
		{"repo upgrade [pkg]", "upgrade installed package(s)"},
	}
	for _, r := range repoSubs {
		fmt.Printf("  %s%s%s  %-24s %s%s%s\n",
			s.ColorDim, s.SymBullet, s.ColorReset,
			s.ColorPrimary+r[0]+s.ColorReset,
			s.ColorDim, r[1], s.ColorReset)
	}
	fmt.Println()

	// Distro-specific subcommands
	distro := detectDistroID()
	if isArchBased(distro) {
		fmt.Printf("  %sAUR:%s\n", s.ColorBold, s.ColorReset)
		aurSubs := [][2]string{
			{"aur install <pkg>", "install directly from AUR"},
			{"aur search <query>", "search AUR only"},
			{"aur list", "list installed AUR packages"},
			{"aur remove <pkg>", "remove via pacman -R"},
			{"aur clean", "remove build cache"},
			{"aur build-local [dir]", "build a local PKGBUILD"},
			{"aur fetch-abs <pkg>", "fetch official PKGBUILD"},
		}
		for _, a := range aurSubs {
			fmt.Printf("  %s%s%s  %-24s %s%s%s\n",
				s.ColorDim, s.SymBullet, s.ColorReset,
				s.ColorPrimary+a[0]+s.ColorReset,
				s.ColorDim, a[1], s.ColorReset)
		}
		fmt.Println()
		fmt.Printf("  %s%s  Arch tip:%s %sup%s and %sug%s alone cause partial upgrades.\n",
			s.ColorWarning, s.SymWarn, s.ColorReset,
			s.ColorPrimary, s.ColorReset,
			s.ColorPrimary, s.ColorReset)
		fmt.Printf("     Always use %sfug%s (full-upgrade / pacman -Syu) instead.\n\n",
			s.ColorPrimary, s.ColorReset)
	}

	if isDebianBased(distro) && isSnapAvailable() {
		fmt.Printf("  %sSnap:%s\n", s.ColorBold, s.ColorReset)
		snapSubs := [][2]string{
			{"snap install <pkg>", "install via snap"},
			{"snap search <query>", "search snap store"},
			{"snap list", "list installed snaps"},
			{"snap update", "refresh all snaps"},
			{"snap remove <pkg>", "remove snap package"},
		}
		for _, sn := range snapSubs {
			fmt.Printf("  %s%s%s  %-24s %s%s%s\n",
				s.ColorDim, s.SymBullet, s.ColorReset,
				s.ColorPrimary+sn[0]+s.ColorReset,
				s.ColorDim, sn[1], s.ColorReset)
		}
		fmt.Println()
	}

	if isFlatpakAvailable() {
		fmt.Printf("  %sFlatpak:%s\n", s.ColorBold, s.ColorReset)
		fpSubs := [][2]string{
			{"flatpak install <pkg>", "install from flathub"},
			{"flatpak search <query>", "search flathub"},
			{"flatpak list", "list installed flatpaks"},
			{"flatpak update", "update all flatpaks"},
			{"flatpak remove <pkg>", "remove flatpak"},
		}
		for _, fp := range fpSubs {
			fmt.Printf("  %s%s%s  %-24s %s%s%s\n",
				s.ColorDim, s.SymBullet, s.ColorReset,
				s.ColorPrimary+fp[0]+s.ColorReset,
				s.ColorDim, fp[1], s.ColorReset)
		}
		fmt.Println()
	}

	fmt.Printf("  %sAliases:%s\n", s.ColorBold, s.ColorReset)
	keys := sortedKeys(cfg.Aliases)
	for _, k := range keys {
		fmt.Printf("  %s%s%s  %s%-15s%s %s %s\n",
			s.ColorDim, s.SymBullet, s.ColorReset,
			s.ColorPrimary, k, s.ColorReset,
			s.SymArrow, cfg.Aliases[k])
	}
	fmt.Println()

	fmt.Printf("  %sSubcommand Aliases:%s\n", s.ColorBold, s.ColorReset)
	subKeys := sortedKeys(config.DefaultSubCmdAliases)
	for _, k := range subKeys {
		fmt.Printf("  %s%s%s  %s%-15s%s %s %s\n",
			s.ColorDim, s.SymBullet, s.ColorReset,
			s.ColorPrimary, k, s.ColorReset,
			s.SymArrow, config.DefaultSubCmdAliases[k])
	}
	fmt.Println()
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

func PrintAliases(cfg *config.Config) {
	s := cfg.Style
	PrintHeader(cfg)
	fmt.Printf("  %sActive aliases:%s\n\n", s.ColorBold, s.ColorReset)
	keys := sortedKeys(cfg.Aliases)
	for _, k := range keys {
		fmt.Printf("  %s%-14s%s %s  %s\n",
			s.ColorPrimary, k, s.ColorReset,
			s.SymArrow, cfg.Aliases[k])
	}
	fmt.Println()
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

// isTTY checks for basic TTY.
func isTTY() bool {
	term := os.Getenv("TERM")
	return term == "linux" || term == "dumb" || term == ""
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
