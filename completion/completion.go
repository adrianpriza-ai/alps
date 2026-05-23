package completion

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/adrianpriza-ai/alps/config"
)

// Generate prints a shell completion script to stdout.
func Generate(shell string, cfg *config.Config) {
	cmds := effectiveCmds(cfg)
	backend := detectBackend()

	switch shell {
	case "fish":
		genFish(cmds, backend)
	case "bash":
		genBash(cmds, backend)
	case "zsh":
		genZsh(cmds, backend)
	default:
		fmt.Fprintf(os.Stderr, "Unknown shell: %s (supported: fish, bash, zsh)\n", shell)
		os.Exit(1)
	}
}

// ── Environment detection ─────────────────────────────────────────

func isTermux() bool {
	return os.Getenv("TERMUX_VERSION") != "" ||
		os.Getenv("PREFIX") == "/data/data/com.termux/files/usr"
}

func isWSL() bool {
	if os.Getenv("WSL_DISTRO_NAME") != "" || os.Getenv("WSL_INTEROP") != "" {
		return true
	}
	if data, err := os.ReadFile("/proc/version"); err == nil {
		lower := strings.ToLower(string(data))
		return strings.Contains(lower, "microsoft") || strings.Contains(lower, "wsl")
	}
	return false
}

// cacheDir returns the alps-more cache directory for the current environment.
func cacheDir() string {
	if isTermux() {
		prefix := os.Getenv("PREFIX")
		if prefix == "" {
			prefix = "/data/data/com.termux/files/usr"
		}
		return filepath.Join(prefix, "var/cache/alps/more")
	}
	return "/var/cache/alps/more"
}

// cacheFile returns the full path to main.txt in the cache.
func cacheFile() string { return filepath.Join(cacheDir(), "main.txt") }

// installedFile returns the full path to installed.json in the cache.
func installedFile() string { return filepath.Join(cacheDir(), "installed.json") }

// ── Backend detection ─────────────────────────────────────────────

func detectBackend() string {
	for _, b := range []string{"apt", "apt-get", "dnf", "pacman"} {
		if _, err := exec.LookPath(b); err == nil {
			return b
		}
	}
	return "apt" // fallback
}

// pkgListCmd returns the shell snippet that lists available packages for the backend.
func pkgListCmd(backend string) string {
	switch backend {
	case "pacman":
		return "pacman -Ssq 2>/dev/null"
	case "dnf":
		return "dnf repoquery --quiet --qf '%{name}' 2>/dev/null"
	default: // apt / apt-get
		return "apt-cache pkgnames 2>/dev/null"
	}
}

// installedListCmd returns the shell snippet that lists installed packages.
func installedListCmd(backend string) string {
	switch backend {
	case "pacman":
		return "pacman -Qq 2>/dev/null"
	case "dnf":
		return "dnf list --installed --quiet 2>/dev/null | awk 'NR>1{print $1}'"
	default: // apt / apt-get
		return "dpkg --get-selections 2>/dev/null | awk '{print $1}'"
	}
}

// moreListCmd returns a POSIX-compatible shell snippet that lists
// all package names from the alps-more main.txt cache.
// Uses tr+grep instead of grep -oP (PCRE unavailable on some systems / Termux).
func moreListCmd(path string) string {
	return fmt.Sprintf(
		`grep '^\[' %s 2>/dev/null | tr -d '[]'`,
		path,
	)
}

// moreInstalledCmd returns a shell snippet that lists packages
// installed via alps-more (from installed.json).
func moreInstalledCmd(path string) string {
	return fmt.Sprintf(`jq -r 'keys[]' %s 2>/dev/null`, path)
}

// ── Fish ──────────────────────────────────────────────────────────

func genFish(cmds []string, backend string) {
	pkgList := pkgListCmd(backend)
	installedList := installedListCmd(backend)
	morePkgs := moreListCmd(cacheFile())
	moreInstalled := moreInstalledCmd(installedFile())

	fmt.Println("# alps fish completion")
	fmt.Println("# Install: alps completion fish > ~/.config/fish/completions/alps.fish")
	fmt.Println()
	fmt.Println("complete -c alps -f")
	fmt.Println()

	for _, cmd := range cmds {
		fmt.Printf("complete -c alps -n '__fish_use_subcommand' -a '%s' -d '%s'\n",
			cmd, cmdDesc(cmd))
	}

	fmt.Printf(`
# ── repo subcommands ─────────────────────────────────────────────
complete -c alps -n '__fish_seen_subcommand_from repo' \
    -a 'update list install remove purge search upgrade' -d 'repo subcommand'

# repo install/remove/search/purge: complete from alps-more cache
complete -c alps -n '__fish_seen_subcommand_from repo; and __fish_seen_subcommand_from install search' \
    -a "(%s)" -d 'alps-more package'
complete -c alps -n '__fish_seen_subcommand_from repo; and __fish_seen_subcommand_from remove purge' \
    -a "(%s)" -d 'installed alps-more package'
complete -c alps -n '__fish_seen_subcommand_from repo; and __fish_seen_subcommand_from upgrade' \
    -a "(%s)" -d 'installed alps-more package'

# ── aur subcommands ───────────────────────────────────────────────
complete -c alps -n '__fish_seen_subcommand_from aur' \
    -a 'install search list clean' -d 'aur subcommand'

# ── flatpak subcommands ───────────────────────────────────────────
complete -c alps -n '__fish_seen_subcommand_from flatpak' \
    -a 'install remove search list update' -d 'flatpak subcommand'

# ── snap subcommands ──────────────────────────────────────────────
complete -c alps -n '__fish_seen_subcommand_from snap' \
    -a 'install remove search list update' -d 'snap subcommand'

# ── package completion ────────────────────────────────────────────
complete -c alps -n '__fish_seen_subcommand_from install search' \
    -a "(%s)" -d 'package'
complete -c alps -n '__fish_seen_subcommand_from remove purge' \
    -a "(%s)" -d 'installed package'
`, morePkgs, moreInstalled, moreInstalled, pkgList, installedList)
}

// ── Bash ──────────────────────────────────────────────────────────

func genBash(cmds []string, backend string) {
	cmdList := strings.Join(cmds, " ")
	pkgList := pkgListCmd(backend)
	installedList := installedListCmd(backend)
	morePkgs := moreListCmd(cacheFile())
	moreInstalled := moreInstalledCmd(installedFile())

	fmt.Printf(`# alps bash completion
# Install: alps completion bash > /etc/bash_completion.d/alps
# or:      source <(alps completion bash)

_alps_completions() {
    local cur prev words cword
    _init_completion || return

    local commands="%s"

    if [[ $cword -eq 1 ]]; then
        COMPREPLY=($(compgen -W "$commands" -- "$cur"))
        return
    fi

    case "${words[1]}" in
        install|ins|i|search|se)
            COMPREPLY=($(compgen -W "$(%s)" -- "$cur"))
            ;;
        remove|rm|purge|pu)
            COMPREPLY=($(compgen -W "$(%s)" -- "$cur"))
            ;;
        repo)
            case "${words[2]}" in
                install|search)
                    COMPREPLY=($(compgen -W "$(%s)" -- "$cur"))
                    ;;
                remove|purge)
                    COMPREPLY=($(compgen -W "$(%s)" -- "$cur"))
                    ;;
                upgrade)
                    COMPREPLY=($(compgen -W "$(%s)" -- "$cur"))
                    ;;
                *)
                    COMPREPLY=($(compgen -W "update list install remove purge search upgrade" -- "$cur"))
                    ;;
            esac
            ;;
    esac
}

complete -F _alps_completions alps
`, cmdList, pkgList, installedList, morePkgs, moreInstalled, moreInstalled)
}

// ── Zsh ───────────────────────────────────────────────────────────

func genZsh(cmds []string, backend string) {
	cmdList := make([]string, 0, len(cmds))
	for _, c := range cmds {
		cmdList = append(cmdList, fmt.Sprintf("'%s:%s'", c, cmdDesc(c)))
	}

	pkgList := pkgListCmd(backend)
	installedList := installedListCmd(backend)
	morePkgs := moreListCmd(cacheFile())
	moreInstalled := moreInstalledCmd(installedFile())

	fmt.Printf(`#compdef alps
# alps zsh completion
# Install: alps completion zsh > "${fpath[1]}/_alps"
# then:    autoload -U compinit && compinit

_alps() {
    local state

    _arguments \
        '1: :->command' \
        '*: :->args'

    case $state in
        command)
            local commands
            commands=(
                %s
            )
            _describe 'command' commands
            ;;
        args)
            case ${words[2]} in
                install|ins|i|search|se)
                    local pkgs
                    pkgs=(${(f)"$(%s)"})
                    _describe 'package' pkgs
                    ;;
                remove|rm|purge|pu)
                    local installed
                    installed=(${(f)"$(%s)"})
                    _describe 'installed package' installed
                    ;;
                repo)
                    case ${words[3]} in
                        install|search)
                            local morepkgs
                            morepkgs=(${(f)"$(%s)"})
                            _describe 'alps-more package' morepkgs
                            ;;
                        remove|purge)
                            local moreinst
                            moreinst=(${(f)"$(%s)"})
                            _describe 'installed alps-more package' moreinst
                            ;;
                        upgrade)
                            local instpkgs
                            instpkgs=(${(f)"$(%s)"})
                            _describe 'installed alps-more package' instpkgs
                            ;;
                        *)
                            _describe 'repo subcommand' \
                                '(update list install remove purge search upgrade)'
                            ;;
                    esac
                    ;;
            esac
            ;;
    esac
}

_alps
`, strings.Join(cmdList, "\n                "),
		pkgList, installedList,
		morePkgs, moreInstalled, moreInstalled)
}

// ── Command metadata ──────────────────────────────────────────────

func cmdDesc(cmd string) string {
	descs := map[string]string{
		"help":         "show help",
		"aliases":      "show aliases",
		"config-show":  "show config",
		"version":      "show version",
		"completion":   "generate shell completion",
		"repo":         "manage alps-more repo packages",
		"aur":          "manage AUR packages directly",
		"flatpak":      "manage flatpak packages",
		"snap":         "manage snap packages",
		"install":      "install package",
		"remove":       "remove package",
		"purge":        "purge package and config",
		"update":       "update package lists",
		"upgrade":      "upgrade packages",
		"full-upgrade": "full system upgrade",
		"search":       "search packages",
		"show":         "show package info",
		"list":         "list packages",
		"autoremove":   "remove unused packages",
		"autoclean":    "clean partial packages",
		"clean":        "clean package cache",
		"ins":          "alias: install",
		"rm":           "alias: remove",
		"pu":           "alias: purge",
		"up":           "alias: update",
		"ug":           "alias: upgrade",
		"fug":          "alias: full-upgrade",
		"se":           "alias: search",
		"sh":           "alias: show",
		"ls":           "alias: list",
		"au":           "alias: autoremove",
		"ac":           "alias: autoclean",
		"cl":           "alias: clean",
	}
	if d, ok := descs[cmd]; ok {
		return d
	}
	return cmd
}

// effectiveCmds returns the command list for this distro/environment.
func effectiveCmds(cfg *config.Config) []string {
	base := []string{
		"help", "aliases", "config-show", "version", "repo", "flatpak",
		"install", "remove", "purge", "update", "upgrade",
		"full-upgrade", "search", "show", "list",
		"autoremove", "autoclean", "clean",
	}

	// Termux: no AUR, no snap
	if isTermux() {
		return applyAliasFilter(base, cfg)
	}

	distro := detectDistroID()

	// Arch-based: add aur
	switch distro {
	case "arch", "manjaro", "endeavouros", "garuda", "artix":
		base = append(base, "aur")
	// Debian/Ubuntu-based: add snap if available
	case "ubuntu", "debian", "linuxmint", "pop", "elementary", "kali":
		if isSnapAvailable() {
			base = append(base, "snap")
		}
	// WSL: snap usually blocked, skip silently; no aur
	default:
		if isWSL() {
			// no extra subcommands for WSL specifically
		}
	}

	return applyAliasFilter(base, cfg)
}

// applyAliasFilter returns base commands, replacing package commands with
// active aliases when the user has custom aliases configured.
func applyAliasFilter(base []string, cfg *config.Config) []string {
	if !hasCustomAliases(cfg) {
		return base
	}
	cmds := []string{}
	for _, b := range base {
		switch b {
		case "help", "aliases", "config-show", "version", "repo", "aur", "flatpak", "snap":
			cmds = append(cmds, b)
		}
	}
	for k := range cfg.Aliases {
		cmds = append(cmds, k)
	}
	return cmds
}

// detectDistroID reads /etc/os-release ID field.
// Returns "termux" on Termux where the file does not exist.
func detectDistroID() string {
	if isTermux() {
		return "termux"
	}
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

// isSnapAvailable checks if snap is usable on this system.
func isSnapAvailable() bool {
	if _, err := exec.LookPath("snap"); err != nil {
		return false
	}
	if _, err := os.Stat("/etc/apt/preferences.d/nosnap.pref"); err == nil {
		return false
	}
	return true
}

var defaultAliasKeys = map[string]string{
	"ins": "install", "rm": "remove", "pu": "purge",
	"up": "update", "ug": "upgrade", "fug": "full-upgrade",
	"se": "search", "sh": "show", "ls": "list",
	"au": "autoremove", "ac": "autoclean", "cl": "clean",
	"ed": "edit-sources",
}

func hasCustomAliases(cfg *config.Config) bool {
	for k, v := range cfg.Aliases {
		def, ok := defaultAliasKeys[k]
		if !ok || def != v {
			return true
		}
	}
	return false
}
