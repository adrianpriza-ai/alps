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
	SymOK        string
	SymErr       string
	SymWarn      string
	SymInfo      string
	SymPkg       string
	SymArrow     string
	SymBullet    string
	ShowHeader   bool
	TitleStyle   string
	HeaderLines  []string
	HeaderText   string
}

type Config struct {
	ConfigAliases map[string]string // aliases from config files only
	Style         Style
	Aliases       map[string]string
	GlobalPath    string
	UserPath      string
	Version       string
}

var defaults = map[string]string{
	"color_primary": `\e[36m`,
	"color_success": `\e[32m`,
	"color_warning": `\e[33m`,
	"color_error":   `\e[31m`,
	"color_info":    `\e[34m`,
	"color_dim":     `\e[2m`,
	"color_reset":   `\e[0m`,
	"color_bold":    `\e[1m`,
	"sym_ok":        "✓",
	"sym_err":       "✗",
	"sym_warn":      "⚠",
	"sym_info":      "◆",
	"sym_pkg":       "::",
	"sym_arrow":     "->",
	"sym_bullet":    "::",
	"show_header":   "true",
	"title_style":   "default",
	"header_text":   "alps",
}

// DefaultAliases are built-in short aliases.
var DefaultAliases = map[string]string{
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
	// subsystems
	"ex": "extra",
	"wg": "winget",
	"fp": "flatpak",
	"sk": "snap",
}

// DefaultSubCmdAliases are built-in short aliases for subcommands.
var DefaultSubCmdAliases = map[string]string{
	"bl":  "build-local",
	"fa":  "fetch-abs",
	"abs": "fetch-abs",
	"add": "install",
	"del": "remove",
}

func globalConfigPath() string { return "/etc/alps/config" }

// isTTY checks if running in a Linux TTY.
func isTTY() bool {
	return os.Getenv("TERM") == "linux" || os.Getenv("TERM") == "dumb" || os.Getenv("TERM") == ""
}

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
	configAliases := make(map[string]string)
	headerLines := []string{}

	globalPath := globalConfigPath()
	userPath := userConfigPath()

	parseFile(globalPath, kv, aliases, configAliases, &headerLines)
	parseFile(userPath, kv, aliases, configAliases, &headerLines)

	// Fill in default aliases only if not overridden by config
	for k, v := range DefaultAliases {
		if _, exists := aliases[k]; !exists {
			aliases[k] = v
		}
	}

	// Override symbols for TTY
	if isTTY() {
		kv["sym_ok"] = " OK "
		kv["sym_err"] = "ERR "
		kv["sym_warn"] = "WARN"
		kv["sym_info"] = "INFO"
	}

	return &Config{
		Style: Style{
			ColorPrimary: unescape(kv["color_primary"]),
			ColorSuccess: unescape(kv["color_success"]),
			ColorWarning: unescape(kv["color_warning"]),
			ColorError:   unescape(kv["color_error"]),
			ColorInfo:    unescape(kv["color_info"]),
			ColorDim:     unescape(kv["color_dim"]),
			ColorReset:   unescape(kv["color_reset"]),
			ColorBold:    unescape(kv["color_bold"]),
			SymOK:        kv["sym_ok"],
			SymErr:       kv["sym_err"],
			SymWarn:      kv["sym_warn"],
			SymInfo:      kv["sym_info"],
			SymPkg:       kv["sym_pkg"],
			SymArrow:     kv["sym_arrow"],
			SymBullet:    kv["sym_bullet"],
			ShowHeader:   kv["show_header"] == "true",
			TitleStyle:   kv["title_style"],
			HeaderLines:  headerLines,
			HeaderText:   kv["header_text"],
		},
		Aliases:       aliases,
		ConfigAliases: configAliases,
		GlobalPath:    globalPath,
		UserPath:      userPath,
	}
}

func parseFile(path string, kv map[string]string, aliases map[string]string, configAliases map[string]string, headerLines *[]string) {
	f, err := os.Open(path)
	if err != nil {
		return // file not found is OK
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue // skip empty/comment lines
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
		if len(val) >= 2 && (val[0] == val[len(val)-1]) && (val[0] == '"' || val[0] == '\'') {
			val = val[1 : len(val)-1]
		}

		lowerKey := strings.ToLower(rawKey)

		switch {
		case strings.HasPrefix(lowerKey, "alias_"):
			// Preserve original case for alias name
			aliasName := rawKey[len("alias_"):]
			aliases[aliasName] = val
			configAliases[aliasName] = val
		case strings.HasPrefix(lowerKey, "title_line"):
			*headerLines = append(*headerLines, unescape(val))
		default:
			kv[lowerKey] = val
		}
	}
}

// unescape converts escape sequences.
func unescape(s string) string {
	s = strings.ReplaceAll(s, `\e`, "\033")
	s = strings.ReplaceAll(s, `\033`, "\033")
	return s
}
