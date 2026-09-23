package config

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/adrianpriza-ai/alps/platform"
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
	// Version is the build version, set by main from the -ldflags-injected
	// value; it is empty when Config is built in library or test use.
	Version       string
	AURRequireGPG bool
}

var defaults = map[string]string{
	"color_primary":   `\e[36m`,
	"color_success":   `\e[32m`,
	"color_warning":   `\e[33m`,
	"color_error":     `\e[31m`,
	"color_info":      `\e[34m`,
	"color_dim":       `\e[2m`,
	"color_reset":     `\e[0m`,
	"color_bold":      `\e[1m`,
	"sym_ok":          "✓",
	"sym_err":         "✗",
	"sym_warn":        "⚠",
	"sym_info":        "◆",
	"sym_pkg":         "::",
	"sym_arrow":       "->",
	"sym_bullet":      "::",
	"show_header":     "true",
	"title_style":     "default",
	"header_text":     "ALPS",
	"aur_require_gpg": "false",
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

// globalConfigPath returns the system-wide config path. ALPS_GLOBAL_CONFIG
// overrides it when set — a seam for tests and packaging, not a user feature.
func globalConfigPath() string {
	if p := os.Getenv("ALPS_GLOBAL_CONFIG"); p != "" {
		return p
	}
	return "/etc/alps/config"
}

func userConfigPath() string {
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		home, _ := os.UserHomeDir()
		base = filepath.Join(home, ".config")
	}
	return filepath.Join(base, "alps", "config")
}

// Load reads /etc/alps/config (or ALPS_GLOBAL_CONFIG) and then the user's
// config, layering the user file over the global one. The two files merge
// with different rules:
//
//   - every key/value is last-one-wins, so the user file overrides the
//     global file;
//   - title_line entries are per-file: the user file's header lines replace
//     the global file's when the user file defines any, so a user banner
//     fully replaces the distro banner instead of stacking on top of it.
func Load() *Config {
	kv := make(map[string]string, len(defaults))
	for k, v := range defaults {
		kv[k] = v
	}

	aliases := make(map[string]string)
	configAliases := make(map[string]string)

	globalPath := globalConfigPath()
	userPath := userConfigPath()

	globalHeaders := parseFile(globalPath, kv, aliases, configAliases)
	userHeaders := parseFile(userPath, kv, aliases, configAliases)

	headerLines := globalHeaders
	if len(userHeaders) > 0 {
		headerLines = userHeaders
	}

	// Fill in default aliases only if not overridden by config
	for k, v := range DefaultAliases {
		if _, exists := aliases[k]; !exists {
			aliases[k] = v
		}
	}

	// Override symbols for ASCII terminals
	if platform.UsesASCIIFallback() {
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
		AURRequireGPG: kv["aur_require_gpg"] == "true",
	}
}

// parseFile reads one config file into kv and the alias maps and returns the
// header lines (title_line entries, unescaped) defined by this file. A
// missing file is normal and returns no lines. Any other read error — a
// permission problem, or a line longer than the scanner limit — is reported
// on stderr instead of silently producing a partial parse.
func parseFile(path string, kv map[string]string, aliases map[string]string, configAliases map[string]string) []string {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		fmt.Fprintf(os.Stderr, "alps: cannot read config %s: %v\n", path, err)
		return nil
	}
	defer f.Close()

	var headerLines []string
	scanner := bufio.NewScanner(f)
	// Raise the token limit so multi-KiB ASCII-art title_line entries parse.
	scanner.Buffer(make([]byte, 0, 64*1024), 1<<20)
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
			headerLines = append(headerLines, unescape(val))
		default:
			kv[lowerKey] = val
		}
	}
	if err := scanner.Err(); err != nil {
		fmt.Fprintf(os.Stderr, "alps: stopped reading config %s: %v\n", path, err)
	}
	return headerLines
}

// unescape converts the escape spellings a config value may use — \e, \033
// and \x1b — into the ESC control character. Colour values and title_line
// lines are unescaped; sym_* values are taken literally, so an escape
// spelling written there is printed as-is.
func unescape(s string) string {
	s = strings.ReplaceAll(s, `\x1b`, "\033")
	s = strings.ReplaceAll(s, `\e`, "\033")
	s = strings.ReplaceAll(s, `\033`, "\033")
	return s
}

// cachedOnce/cachedCfg back LoadCached.
var (
	cachedOnce sync.Once
	cachedCfg  *Config
)

// LoadCached returns the same config as Load, read once and memoized for
// the process lifetime. Use it on hot paths that cannot receive a *Config
// from the caller; Load stays uncached so tests can point the config paths
// at fresh locations and re-load.
func LoadCached() *Config {
	cachedOnce.Do(func() {
		cachedCfg = Load()
	})
	return cachedCfg
}
