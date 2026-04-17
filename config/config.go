package config

import (
	"bufio"
	"os"
	"path/filepath"
	"strconv"
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
	ProgressStyle     string
	ProgressApt       string
	ProgressDnf       string
	ProgressPacman    string
	ProgressAur       string
	ProgressBarChar   string
	ProgressBarEmpty  string
	ProgressBarWidth  int
	ProgressSpinChars string

	// Header
	ShowHeader  bool
	TitleStyle  string   // "default" | "custom"
	HeaderLines []string // used when TitleStyle == "custom"
	HeaderText  string
}

type Config struct {
	Style      Style
	Aliases    map[string]string
	GlobalPath string
	UserPath   string
}

var defaults = map[string]string{
	"color_primary":       `\e[36m`,
	"color_success":       `\e[32m`,
	"color_warning":       `\e[33m`,
	"color_error":         `\e[31m`,
	"color_info":          `\e[34m`,
	"color_dim":           `\e[2m`,
	"color_reset":         `\e[0m`,
	"color_bold":          `\e[1m`,
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

var defaultAliases = map[string]string{
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
	kv := make(map[string]string, len(defaults))
	for k, v := range defaults {
		kv[k] = v
	}

	aliases := make(map[string]string)
	headerLines := []string{}

	globalPath := globalConfigPath()
	userPath := userConfigPath()

	parseFile(globalPath, kv, aliases, &headerLines)
	parseFile(userPath, kv, aliases, &headerLines)

	// Fill in default aliases only if not overridden by config
	for k, v := range defaultAliases {
		if _, exists := aliases[k]; !exists {
			aliases[k] = v
		}
	}

	width := 30
	if w, err := strconv.Atoi(kv["progress_bar_width"]); err == nil && w > 0 {
		width = w
	}

	return &Config{
		Style: Style{
			ColorPrimary:      unescape(kv["color_primary"]),
			ColorSuccess:      unescape(kv["color_success"]),
			ColorWarning:      unescape(kv["color_warning"]),
			ColorError:        unescape(kv["color_error"]),
			ColorInfo:         unescape(kv["color_info"]),
			ColorDim:          unescape(kv["color_dim"]),
			ColorReset:        unescape(kv["color_reset"]),
			ColorBold:         unescape(kv["color_bold"]),
			SymOK:             kv["sym_ok"],
			SymErr:            kv["sym_err"],
			SymWarn:           kv["sym_warn"],
			SymInfo:           kv["sym_info"],
			SymPkg:            kv["sym_pkg"],
			SymArrow:          kv["sym_arrow"],
			SymBullet:         kv["sym_bullet"],
			ProgressStyle:     kv["progress_style"],
			ProgressApt:       kv["progress_apt"],
			ProgressDnf:       kv["progress_dnf"],
			ProgressPacman:    kv["progress_pacman"],
			ProgressAur:       kv["progress_aur"],
			ProgressBarChar:   kv["progress_bar_char"],
			ProgressBarEmpty:  kv["progress_bar_empty"],
			ProgressBarWidth:  width,
			ProgressSpinChars: kv["progress_spin_chars"],
			ShowHeader:        kv["show_header"] == "true",
			TitleStyle:        kv["title_style"],
			HeaderLines:       headerLines,
			HeaderText:        kv["header_text"],
		},
		Aliases:    aliases,
		GlobalPath: globalPath,
		UserPath:   userPath,
	}
}

func parseFile(path string, kv map[string]string, aliases map[string]string, headerLines *[]string) {
	f, err := os.Open(path)
	if err != nil {
		return // file not found is normal (e.g. no global config)
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
		// Strip surrounding quotes
		if len(val) >= 2 &&
			((val[0] == '"' && val[len(val)-1] == '"') ||
				(val[0] == '\'' && val[len(val)-1] == '\'')) {
			val = val[1 : len(val)-1]
		}

		lowerKey := strings.ToLower(rawKey)

		switch {
		case strings.HasPrefix(lowerKey, "alias_"):
			// Preserve original case for alias name (e.g. alias_-S stays -S)
			aliases[rawKey[len("alias_"):]] = val
		case strings.HasPrefix(lowerKey, "title_line"):
			*headerLines = append(*headerLines, unescape(val))
		default:
			kv[lowerKey] = val
		}
	}
}

// unescape converts \e and \033 literals into the actual ESC byte.
func unescape(s string) string {
	s = strings.ReplaceAll(s, `\e`, "\033")
	s = strings.ReplaceAll(s, `\033`, "\033")
	return s
}
