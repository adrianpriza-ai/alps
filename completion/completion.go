package completion

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Generate prints a shell completion script to stdout.
func Generate(shell string) {
	cmds := effectiveCmds()
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

// Environment detection

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

func cacheFile() string     { return filepath.Join(cacheDir(), "main.txt") }
func installedFile() string { return filepath.Join(cacheDir(), "installed.json") }

// AURNamesCachePath returns the path where aur.go should write AUR package
// names after a search. Used by completion scripts to complete aur install/search.
// This is user-specific (not system-wide) so it lives under $HOME.
// aur.go writes here; completion scripts read from here at tab-complete time.
func AURNamesCachePath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(os.Getenv("HOME"), ".cache", "alps", "aur-names.txt")
	}
	return filepath.Join(home, ".cache", "alps", "aur-names.txt")
}

// Backend detection

func detectBackend() string {
	for _, b := range []string{"apt", "apt-get", "dnf", "pacman"} {
		if _, err := exec.LookPath(b); err == nil {
			return b
		}
	}
	return "apt"
}

func pkgListCmd(backend string) string {
	switch backend {
	case "pacman":
		return "pacman -Ssq 2>/dev/null"
	case "dnf":
		return "dnf repoquery --quiet --qf '%{name}' 2>/dev/null"
	default:
		return "apt-cache pkgnames 2>/dev/null"
	}
}

func installedListCmd(backend string) string {
	switch backend {
	case "pacman":
		return "pacman -Qq 2>/dev/null"
	case "dnf":
		return "dnf list --installed --quiet 2>/dev/null | awk 'NR>1{print $1}'"
	default:
		return "dpkg --get-selections 2>/dev/null | awk '{print $1}'"
	}
}

// moreListCmd lists all package names from the alps-more main.txt cache.
// POSIX-compatible: no grep -oP (unavailable on Termux).
func moreListCmd(path string) string {
	return fmt.Sprintf(`grep '^\[' %s 2>/dev/null | tr -d '[]'`, path)
}

// moreInstalledCmd lists packages installed via alps-more (installed.json keys).
func moreInstalledCmd(path string) string {
	return fmt.Sprintf(`jq -r 'keys[]' %s 2>/dev/null`, path)
}

// aurNamesCmd reads the AUR package name cache populated by `alps aur search`.
// Uses $HOME so it expands correctly at tab-complete time, not at generation time.
func aurNamesCmd() string {
	return `cat "$HOME/.cache/alps/aur-names.txt" 2>/dev/null`
}

// aurInstalledCmd lists AUR-installed packages via pacman -Qm (foreign packages).
func aurInstalledCmd() string {
	return `pacman -Qm 2>/dev/null | awk '{print $1}'`
}

// Fish

func genFish(cmds []string, backend string) {
	pkgList := pkgListCmd(backend)
	installedList := installedListCmd(backend)
	morePkgs := moreListCmd(cacheFile())
	moreInstalled := moreInstalledCmd(installedFile())
	aurNames := aurNamesCmd()
	aurInstalled := aurInstalledCmd()

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

complete -c alps -n '__fish_seen_subcommand_from repo; and __fish_seen_subcommand_from install search' \
    -a "(%s)" -d 'alps-more package'
complete -c alps -n '__fish_seen_subcommand_from repo; and __fish_seen_subcommand_from remove purge' \
    -a "(%s)" -d 'installed alps-more package'
complete -c alps -n '__fish_seen_subcommand_from repo; and __fish_seen_subcommand_from upgrade' \
    -a "(%s)" -d 'installed alps-more package'

# ── aur subcommands ───────────────────────────────────────────────
complete -c alps -n '__fish_seen_subcommand_from aur' \
    -a 'install search list remove clean build-local fetch-abs' -d 'aur subcommand'

# aur install/search: complete from local AUR name cache (~/.cache/alps/aur-names.txt)
# Cache is populated automatically when 'alps aur search' is run.
complete -c alps -n '__fish_seen_subcommand_from aur; and __fish_seen_subcommand_from install search' \
    -a "(%s)" -d 'AUR package'
# aur remove: complete from pacman -Qm (foreign/AUR-installed packages)
complete -c alps -n '__fish_seen_subcommand_from aur; and __fish_seen_subcommand_from remove' \
    -a "(%s)" -d 'AUR installed package'
# aur build-local: complete directories
complete -c alps -n '__fish_seen_subcommand_from aur; and __fish_seen_subcommand_from build-local' \
    -a "(__fish_complete_directories)" -d 'directory'

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
`, morePkgs, moreInstalled, moreInstalled,
		aurNames, aurInstalled,
		pkgList, installedList)
}

// Bash

func genBash(cmds []string, backend string) {
	cmdList := strings.Join(cmds, " ")
	pkgList := pkgListCmd(backend)
	installedList := installedListCmd(backend)
	morePkgs := moreListCmd(cacheFile())
	moreInstalled := moreInstalledCmd(installedFile())
	aurNames := aurNamesCmd()
	aurInstalled := aurInstalledCmd()

	fmt.Printf(`# alps bash completion
# Install: alps completion bash | sudo tee /usr/share/bash-completion/completions/alps
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
        aur)
            case "${words[2]}" in
                install|search)
                    # AUR name cache — populated by 'alps aur search'
                    COMPREPLY=($(compgen -W "$(%s)" -- "$cur"))
                    ;;
                remove)
                    # Foreign packages installed via pacman (AUR)
                    COMPREPLY=($(compgen -W "$(%s)" -- "$cur"))
                    ;;
                build-local)
                    # Directory completion
                    COMPREPLY=($(compgen -d -- "$cur"))
                    ;;
                fetch-abs)
                    # Official package names — no local cache, leave empty
                    ;;
                *)
                    COMPREPLY=($(compgen -W "install search list remove clean build-local fetch-abs" -- "$cur"))
                    ;;
            esac
            ;;
    esac
}

complete -F _alps_completions alps
`, cmdList,
		pkgList, installedList,
		morePkgs, moreInstalled, moreInstalled,
		aurNames, aurInstalled)
}

// Zsh

func genZsh(cmds []string, backend string) {
	cmdList := make([]string, 0, len(cmds))
	for _, c := range cmds {
		cmdList = append(cmdList, fmt.Sprintf("'%s:%s'", c, cmdDesc(c)))
	}

	pkgList := pkgListCmd(backend)
	installedList := installedListCmd(backend)
	morePkgs := moreListCmd(cacheFile())
	moreInstalled := moreInstalledCmd(installedFile())
	aurNames := aurNamesCmd()
	aurInstalled := aurInstalledCmd()

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
                aur)
                    case ${words[3]} in
                        install|search)
                            # AUR name cache — populated by 'alps aur search'
                            local aurpkgs
                            aurpkgs=(${(f)"$(%s)"})
                            _describe 'AUR package' aurpkgs
                            ;;
                        remove)
                            # Foreign packages installed via pacman (AUR)
                            local aurinst
                            aurinst=(${(f)"$(%s)"})
                            _describe 'AUR installed package' aurinst
                            ;;
                        build-local)
                            # Directory completion
                            _path_files -/
                            ;;
                        fetch-abs)
                            # Official package names — no local cache
                            ;;
                        *)
                            _describe 'aur subcommand' \
                                '(install search list remove clean build-local fetch-abs)'
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
		morePkgs, moreInstalled, moreInstalled,
		aurNames, aurInstalled)
}

// Command metadata

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
	}
	if d, ok := descs[cmd]; ok {
		return d
	}
	return cmd
}

// effectiveCmds returns the command list for this distro/environment.
func effectiveCmds() []string {
	base := []string{
		"help", "aliases", "config-show", "version", "repo", "flatpak",
		"install", "remove", "purge", "update", "upgrade",
		"full-upgrade", "search", "show", "list",
		"autoremove", "autoclean", "clean",
	}

	if isTermux() {
		return base
	}

	distro := detectDistroID()

	switch distro {
	case "arch", "manjaro", "endeavouros", "garuda", "artix":
		base = append(base, "aur")
	case "ubuntu", "debian", "linuxmint", "pop", "elementary", "kali":
		if isSnapAvailable() {
			base = append(base, "snap")
		}
	}

	return base
}

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

func isSnapAvailable() bool {
	if _, err := exec.LookPath("snap"); err != nil {
		return false
	}
	if _, err := os.Stat("/etc/apt/preferences.d/nosnap.pref"); err == nil {
		return false
	}
	return true
}
