package ui

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"alps/config"
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

// Confirm prints "[Y/n]" and returns true unless user explicitly says no.
func Confirm() bool {
	fmt.Print("  Continue? [Y/n] ")
	var input string
	fmt.Scanln(&input)
	input = strings.ToLower(strings.TrimSpace(input))
	return input == "" || input == "y" || input == "yes"
}

// ──────────────────────────────────────────
// HEADER
// ──────────────────────────────────────────

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

	fmt.Print("\033[0m\033[97m                   *\n")
	fmt.Print("\033[97m                  /^\\ \033[37m *             \033[97mCustomizable\033[37m\n")
	fmt.Print("\033[97m ALPS\033[37m        /^\\ /   \\/^\\\n")
	fmt.Print("\033[37m   v0.6     /   \\   /^\\  \\         \033[97mpackage manager\033[37m\n")
	fmt.Print("\033[1;32m           /_____\\_/___\\__\\\033[0m\n\n")
}

// ──────────────────────────────────────────
// HELP / ALIASES / CONFIG-SHOW
// ──────────────────────────────────────────

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
		{"repo install <pkg>", "install from alps-more"},
		{"repo remove <pkg>", "remove from alps-more"},
	}
	for _, r := range repoSubs {
		fmt.Printf("  %s%s%s  %-24s %s%s%s\n",
			s.ColorDim, s.SymBullet, s.ColorReset,
			s.ColorPrimary+r[0]+s.ColorReset,
			s.ColorDim, r[1], s.ColorReset)
	}
	fmt.Println()
	
	fmt.Printf("  %sAliases:%s\n", s.ColorBold, s.ColorReset)
	keys := sortedKeys(cfg.Aliases)
	for _, k := range keys {
		fmt.Printf("  %s%s%s  %s%-15s%s %s %s\n",
			s.ColorDim, s.SymBullet, s.ColorReset,
			s.ColorPrimary, k, s.ColorReset,
			s.SymArrow, cfg.Aliases[k])
	}
	fmt.Println()
	fmt.Printf("  %sOther commands are passed directly to the backend.%s\n\n", s.ColorDim, s.ColorReset)
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

// ──────────────────────────────────────────
// PROGRESS
// ──────────────────────────────────────────

func ProgressForBackend(cfg *config.Config, backend string) string {
	s := cfg.Style
	switch backend {
	case "apt", "apt-get":
		if s.ProgressApt != "" {
			return s.ProgressApt
		}
	case "dnf":
		if s.ProgressDnf != "" {
			return s.ProgressDnf
		}
	case "pacman":
		if s.ProgressPacman != "" {
			return s.ProgressPacman
		}
	case "aur":
		if s.ProgressAur != "" {
			return s.ProgressAur
		}
	}
	return s.ProgressStyle
}

func StartProgress(cfg *config.Config, msg string) func() {
	return StartProgressFor(cfg, msg, "")
}

func StartProgressFor(cfg *config.Config, msg string, backend string) func() {
	switch ProgressForBackend(cfg, backend) {
	case "pacman":
		return startPacman(cfg, msg)
	case "bar":
		return startBar(cfg, msg)
	case "spinner":
		return startSpinner(cfg, msg)
	case "dots":
		return startDots(cfg, msg)
	default:
		return func() {}
	}
}

func startPacman(cfg *config.Config, msg string) func() {
	s := cfg.Style
	done := make(chan struct{})
	go func() {
		width := s.ProgressBarWidth
		step := 0
		for {
			select {
			case <-done:
				return
			default:
				pct := step % 101
				filled := pct * width / 100
				bar := strings.Repeat(s.ProgressBarChar, filled) +
					strings.Repeat(s.ProgressBarEmpty, width-filled)
				fmt.Printf("\r  %s%s%s %s [%s%s%s%s] %3d%%",
					s.ColorPrimary, s.SymBullet, s.ColorReset,
					msg,
					s.ColorSuccess, bar, s.ColorDim, s.ColorReset,
					pct)
				step += 2
				time.Sleep(60 * time.Millisecond)
			}
		}
	}()
	return func() { close(done); fmt.Print("\r\033[K") }
}

func startBar(cfg *config.Config, msg string) func() {
	s := cfg.Style
	done := make(chan struct{})
	go func() {
		width := s.ProgressBarWidth
		step := 0
		for {
			select {
			case <-done:
				return
			default:
				pct := step % 101
				filled := pct * width / 100
				empty := width - filled - 1
				if empty < 0 {
					empty = 0
				}
				bar := strings.Repeat("=", filled) + ">" + strings.Repeat(" ", empty)
				fmt.Printf("\r  %s [%s%s%s] %3d%%",
					msg, s.ColorSuccess, bar, s.ColorReset, pct)
				step += 2
				time.Sleep(60 * time.Millisecond)
			}
		}
	}()
	return func() { close(done); fmt.Print("\r\033[K") }
}

func startSpinner(cfg *config.Config, msg string) func() {
	s := cfg.Style
	chars := []rune(s.ProgressSpinChars)
	if len(chars) == 0 {
		chars = []rune(`\|/-`)
	}
	done := make(chan struct{})
	go func() {
		i := 0
		for {
			select {
			case <-done:
				return
			default:
				fmt.Printf("\r  %s%c%s  %s", s.ColorPrimary, chars[i%len(chars)], s.ColorReset, msg)
				i++
				time.Sleep(80 * time.Millisecond)
			}
		}
	}()
	return func() { close(done); fmt.Print("\r\033[K") }
}

func startDots(cfg *config.Config, msg string) func() {
	s := cfg.Style
	done := make(chan struct{})
	go func() {
		dots := []string{".", "..", "..."}
		i := 0
		for {
			select {
			case <-done:
				return
			default:
				fmt.Printf("\r  %s%s%s%-3s   ",
					s.ColorDim, msg, s.ColorReset, dots[i%3])
				i++
				time.Sleep(400 * time.Millisecond)
			}
		}
	}()
	return func() { close(done); fmt.Print("\r\033[K") }
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
