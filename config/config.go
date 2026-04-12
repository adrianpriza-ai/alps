package config

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
)

type Style struct {
	ColorPrimary string
	ColorSuccess string
	ColorWarning string
	ColorError   string
	ColorInfo    string
	ColorDim     string
	ColorReset   string
	ColorBold    string

	SymOK     string
	SymErr    string
	SymWarn   string
	SymInfo   string
	SymPkg    string
	SymArrow  string
	SymBullet string

	// Progress: preset name
	// presets: pacman | bar | spinner | dots | none
	// per-backend override: progress_apt | progress_dnf | progress_pacman | progress_aur
	ProgressStyle       string
	ProgressApt         string
	ProgressDnf         string
	ProgressPacman      string
	ProgressAur         string
	ProgressBarChar     string
	ProgressBarEmpty    string
	ProgressBarWidth    int
	ProgressSpinChars   string

	// Header
	ShowHeader  bool
	TitleStyle  string // "default" | "custom"
	HeaderLines []string // used when TitleStyle == "custom"
	HeaderText  string   // fallback / shown in config-show
}

type Config struct {
	Style      Style
	Aliases    map[string]string
	GlobalPath string
	UserPath   string
}

var defaults = map[string]string{
	"color_primary":       "\033[36m",
	"color_success":       "\033[32m",
	"color_warning":       "\033[33m",
	"color_error":         "\033[31m",
	"color_info":          "\033[34m",
	"color_dim":           "\033[2m",
	"color_reset":         "\033[0m",
	"color_bold":          "\033[1m",
	"sym_ok":              "✓",
	"sym_err":             "✗",
	"sym_warn":            "⚠",
	"sym_info":            "◆",
	"sym_pkg":             "::",
	"sym_arrow":           "->",
	"sym_bullet":          "::",
	"progress_style":      "pacman",
	"progress_apt":        "",
	"progress_dnf":        "",
	"progress_pacman":     "",
	"progress_aur":        "spinner",
	"progress_bar_char":   "#",
	"progress_bar_empty":  "-",
	"progress_bar_width":  "30",
	"progress_spin_chars": `\|/-`,
	"show_header":         "true",
	"title_style":         "default",
	"header_text":         "alps",
}

func globalConfigPath() string { return "/etc/alps/config" }

func userConfigPath() string {
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		home, _ := os.UserHomeDir()
		base = filepath.Join(home, ".config")
	}
	return filepath.Join(base, "alps", "config")
}

func Load() *Config {
	kv := make(map[string]string)
	aliases := make(map[string]string)
	headerLines := []string{}

	for k, v := range defaults {
		kv[k] = v
	}

	globalPath := globalConfigPath()
	userPath := userConfigPath()

	parseFile(globalPath, kv, aliases, &headerLines)
	parseFile(userPath, kv, aliases, &headerLines)

	defaultAliases := map[string]string{
		"ins": "install",
		"rm":  "remove",
		"pu":  "purge",
		"up":  "update",
		"ug":  "upgrade",
		"fug": "full-upgrade",
		"se":  "search",
		"sh":  "show",
		"ls":  "list",
		"au":  "autoremove",
		"ac":  "autoclean",
		"cl":  "clean",
		"ed":  "edit-sources",
	}
	for k, v := range defaultAliases {
		if _, exists := aliases[k]; !exists {
			aliases[k] = v
		}
	}

	width := 30
	if w := parseInt(kv["progress_bar_width"]); w > 0 {
		width = w
	}

	return &Config{
		Style: Style{
			ColorPrimary:    unescape(kv["color_primary"]),
			ColorSuccess:    unescape(kv["color_success"]),
			ColorWarning:    unescape(kv["color_warning"]),
			ColorError:      unescape(kv["color_error"]),
			ColorInfo:       unescape(kv["color_info"]),
			ColorDim:        unescape(kv["color_dim"]),
			ColorReset:      unescape(kv["color_reset"]),
			ColorBold:       unescape(kv["color_bold"]),
			SymOK:           kv["sym_ok"],
			SymErr:          kv["sym_err"],
			SymWarn:         kv["sym_warn"],
			SymInfo:         kv["sym_info"],
			SymPkg:          kv["sym_pkg"],
			SymArrow:        kv["sym_arrow"],
			SymBullet:       kv["sym_bullet"],
			ProgressStyle:   kv["progress_style"],
			ProgressApt:     kv["progress_apt"],
			ProgressDnf:     kv["progress_dnf"],
			ProgressPacman:  kv["progress_pacman"],
			ProgressAur:     kv["progress_aur"],
			ProgressBarChar:  kv["progress_bar_char"],
			ProgressBarEmpty: kv["progress_bar_empty"],
			ProgressBarWidth: width,
			ProgressSpinChars: kv["progress_spin_chars"],
			ShowHeader:      kv["show_header"] == "true",
			TitleStyle:      kv["title_style"],
			HeaderLines:     headerLines,
			HeaderText:      kv["header_text"],
		},
		Aliases:    aliases,
		GlobalPath: globalPath,
		UserPath:   userPath,
	}
}

func parseFile(path string, kv map[string]string, aliases map[string]string, headerLines *[]string) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		idx := strings.Index(line, "=")
		if idx < 0 {
			continue
		}

		rawKey := strings.TrimSpace(line[:idx])
		val := strings.TrimSpace(line[idx+1:])

		// Strip inline comment
		if ci := strings.Index(val, " #"); ci >= 0 {
			val = strings.TrimSpace(val[:ci])
		}
		// Strip quotes
		if len(val) >= 2 && ((val[0] == '"' && val[len(val)-1] == '"') ||
			(val[0] == '\'' && val[len(val)-1] == '\'')) {
			val = val[1 : len(val)-1]
		}

		// Preserve case for alias_ keys (for -S, -R etc)
		lowerKey := strings.ToLower(rawKey)

		if strings.HasPrefix(lowerKey, "alias_") {
			aliasName := rawKey[len("alias_"):]
			aliases[aliasName] = val
		} else if strings.HasPrefix(lowerKey, "title_line") {
			*headerLines = append(*headerLines, unescape(val))
		} else {
			kv[lowerKey] = val
		}
	}
}

func parseInt(s string) int {
	n := 0
	for _, c := range s {
		if c >= '0' && c <= '9' {
			n = n*10 + int(c-'0')
		}
	}
	return n
}

func unescape(s string) string {
	s = strings.ReplaceAll(s, `\e`, "\033")
	s = strings.ReplaceAll(s, `\033`, "\033")
	return s
}
