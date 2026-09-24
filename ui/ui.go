package ui

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"

	"github.com/adrianpriza-ai/alps/cli"
	"github.com/adrianpriza-ai/alps/config"
	"github.com/adrianpriza-ai/alps/extra"
	"github.com/adrianpriza-ai/alps/more"
	"github.com/adrianpriza-ai/alps/pack"
	"github.com/adrianpriza-ai/alps/platform"
	"golang.org/x/term"
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

// Confirm is the confirmation gate for destructive actions. A failed or
// unavailable stdin is never treated as a blank "yes" answer.
func Confirm() bool {
	return PromptYesNo("  Continue?", true)
}

// promptSuffix returns the [Y/n] or [y/N] hint shown after a prompt.
func promptSuffix(defaultYes bool) string {
	if defaultYes {
		return "[Y/n]"
	}
	return "[y/N]"
}

// stdinIsTerminal reports whether stdin is an interactive terminal.
func stdinIsTerminal() bool {
	return term.IsTerminal(int(os.Stdin.Fd()))
}

// ReadLine reads one line from stdin and returns it trimmed. A read error
// (EOF, closed stdin) yields the empty string.
func ReadLine() string {
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil && line == "" {
		return ""
	}
	return strings.TrimSpace(line)
}

// PromptYesNo prints prompt with a [Y/n] or [y/N] hint and reads the answer.
// Empty input on a real terminal takes defaultYes. On I/O error, or when
// stdin is not a terminal, it prints a notice to stderr and answers no.
func PromptYesNo(prompt string, defaultYes bool) bool {
	if !stdinIsTerminal() {
		fmt.Fprintln(os.Stderr, "  no terminal available — assuming 'no'")
		return false
	}

	fmt.Printf("%s %s ", prompt, promptSuffix(defaultYes))
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil && strings.TrimSpace(line) == "" {
		// A failed read is not a blank answer: refuse rather than assume yes.
		return false
	}
	return promptAnswer(line, defaultYes)
}

// promptAnswer maps trimmed user input to a yes/no answer using the prompt's
// documented default. Anything that is not y/yes/n/no is a no.
func promptAnswer(line string, defaultYes bool) bool {
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "":
		return defaultYes
	case "y", "yes":
		return true
	case "n", "no":
		return false
	default:
		return false
	}
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
	text := cfg.Style.HeaderText
	if text == "" {
		text = "ALPS"
	}
	if term == "linux" || term == "" {
		fmt.Printf("\n  \033[1;97m%s\033[0m  \033[2mAdvanced Linux Package System · %s\033[0m\n\n", text, cfg.Version)
		return
	}

	fmt.Printf("\n                   /^\\\n")
	fmt.Printf("   %s        /^\\/   \\/\\\n", text)
	fmt.Printf("     %-6s   \033[1;32m/___\\____\\_\\\033[0m\n\n", cfg.Version)
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
	printRows(cfg, 19, cli.CoreHelpRows())
	fmt.Println()

	printSectionTitle(cfg, "Flags")
	printRows(cfg, 16, [][2]string{
		{"-n, --dry-run", "simulate, no changes written"},
		{"-y, --noconfirm", "skip confirmation prompts"},
		{"-v, --verbose", "enable verbose output"},
		{"-q, --quiet", "suppress non-error output"},
		{"-f, --force", "force operation (skip safety checks)"},
	})
	fmt.Println()

	printSectionTitle(cfg, "Repo")
	printRows(cfg, 23, cli.SubCmdHelp("repo"))
	fmt.Println()

	// Distro-specific subcommands
	if platform.IsArchBased() {
		printSectionTitle(cfg, "AUR")
		printRows(cfg, 22, cli.SubCmdHelp("aur"))
		fmt.Println()
		fmt.Printf("  %s%s%s %sArch tip:%s use %sfull-upgrade%s, not update/upgrade — avoids partial upgrades\n\n",
			s.ColorWarning, s.SymWarn, s.ColorReset,
			s.ColorBold, s.ColorReset,
			s.ColorPrimary, s.ColorReset)
	}

	if platform.IsDebianBased() && extra.IsAvailable("snap") {
		printSectionTitle(cfg, "Snap")
		printRows(cfg, 19, cli.SubCmdHelp("snap"))
		fmt.Println()
	}

	if extra.IsAvailable("flatpak") {
		printSectionTitle(cfg, "Flatpak")
		printRows(cfg, 23, cli.SubCmdHelp("flatpak"))
		fmt.Println()
	}

	fmt.Printf("  %sOther commands are passed directly to your system's package manager.%s\n\n", s.ColorDim, s.ColorReset)
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

	if platform.IsArchBased() {
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
	if platform.UsesASCIIFallback() {
		return "->"
	}
	return "↑"
}

// symReinstall returns a reinstall symbol.
func symReinstall() string {
	if platform.UsesASCIIFallback() {
		return ">>"
	}
	return "⟳"
}

// PrintDiagnostic displays system diagnostic information.
func PrintDiagnostic(cfg *config.Config) {
	PrintHeader(cfg)

	var distro string
	if platform.IsTermux() {
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
		distro = platform.DistroName()
		if distro == "" {
			distro = "unknown"
		}
	}

	backend := pack.DetectName()
	if backend == "" {
		backend = "none detected"
	}

	installed, err := more.ReadInstalled()
	moreLine := fmt.Sprintf("%d package(s) installed via alps-more", len(installed))
	if err != nil {
		// A broken state file must not be reported as "0 packages".
		Msgf(cfg, LevelWarn, "alps-more  state unreadable: %v", err)
		moreLine = "state unreadable — package count unknown"
	}

	extras := []string{}
	if !platform.IsTermux() {
		if extra.IsAvailable("flatpak") {
			extras = append(extras, "flatpak")
		}
		if extra.IsAvailable("snap") {
			extras = append(extras, "snap")
		}
		if _, err := exec.LookPath("paru"); err == nil {
			extras = append(extras, "paru")
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
	fmt.Printf("  %smore%s     %s%s%s\n", pri, rst, dim, moreLine, rst)
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
		instTag = fmt.Sprintf(" %s[%s]%s", s.ColorSuccess, installedVer, s.ColorReset)
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
