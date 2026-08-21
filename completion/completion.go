package completion

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Generate prints a shell completion script.
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

func isTermux() bool {
	return os.Getenv("TERMUX_VERSION") != "" ||
		os.Getenv("PREFIX") == "/data/data/com.termux/files/usr"
}

// isWSL checks if running on Windows Subsystem for Linux.
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

// cacheDir returns the cache directory.
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

// libDir returns the state directory.
func libDir() string {
	if isTermux() {
		prefix := os.Getenv("PREFIX")
		if prefix == "" {
			prefix = "/data/data/com.termux/files/usr"
		}
		return filepath.Join(prefix, "var/lib/alps")
	}
	return "/var/lib/alps"
}

func cacheFile() string     { return filepath.Join(cacheDir(), "main.txt") }
func installedFile() string { return filepath.Join(libDir(), "installed.json") }

// AURNamesCachePath returns the path for AUR package names cache.
func AURNamesCachePath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(os.Getenv("HOME"), ".cache", "alps", "aur-names.txt")
	}
	return filepath.Join(home, ".cache", "alps", "aur-names.txt")
}

func detectBackend() string {
	for _, b := range []string{"apt", "apt-get", "dnf", "pacman", "zypper", "apk"} {
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
	case "zypper":
		return "zypper -q packages 2>/dev/null | awk -F'|' 'NR>2{gsub(/[[:space:]]/,\"\",$3); print $3}' | sort -u"
	case "apk":
		return "apk search -q 2>/dev/null"
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
	case "zypper":
		return "zypper -q packages --installed-only 2>/dev/null | awk -F'|' 'NR>2{gsub(/[[:space:]]/,\"\",$3); print $3}'"
	case "apk":
		return "apk info 2>/dev/null"
	default:
		return "dpkg --get-selections 2>/dev/null | awk '{print $1}'"
	}
}

// moreListCmd lists package names from cache.
func moreListCmd(path string) string {
	return fmt.Sprintf(`grep '^\[' %s 2>/dev/null | tr -d '[]'`, path)
}

// moreInstalledCmd lists installed packages.
func moreInstalledCmd(path string) string {
	return fmt.Sprintf(`jq -r 'keys[]' %s 2>/dev/null`, path)
}

// aurNamesCmd reads the AUR package name cache.
func aurNamesCmd() string {
	return `cat "$HOME/.cache/alps/aur-names.txt" 2>/dev/null`
}

// aurInstalledCmd lists AUR-installed packages.
func aurInstalledCmd() string {
	return `pacman -Qm 2>/dev/null | awk '{print $1}'`
}

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
# top-level commands
# $t[1]=alps  $t[2]=cmd  $t[3]=subcmd  $t[4]=arg

# repo subcommands
complete -c alps -n 'set -l t (commandline -poc); contains -- "$t[2]" repo; and test (count $t) -eq 2' \
    -a 'update list install remove purge search upgrade clean' -d 'repo subcommand'

# repo list sub-actions
complete -c alps -n 'set -l t (commandline -poc); contains -- "$t[2]" repo; and contains -- "$t[3]" list ls; and test (count $t) -eq 3' \
    -a 'install remove' -d 'list action'

# repo install/search → alps-more packages
complete -c alps -n 'set -l t (commandline -poc); contains -- "$t[2]" repo; and contains -- "$t[3]" install ins search se' \
    -a "(%s)" -d 'alps-more package'

# repo remove/purge/upgrade → installed alps-more packages
complete -c alps -n 'set -l t (commandline -poc); contains -- "$t[2]" repo; and contains -- "$t[3]" remove rm purge pu upgrade ug' \
    -a "(%s)" -d 'installed alps-more package'

# aur subcommands
complete -c alps -n 'set -l t (commandline -poc); contains -- "$t[2]" aur; and test (count $t) -eq 2' \
    -a 'install search list remove clean build-local fetch-abs info clone orphans' -d 'aur subcommand'

# aur install/search → pacman repo + AUR
complete -c alps -n 'set -l t (commandline -poc); contains -- "$t[2]" aur; and contains -- "$t[3]" install ins search se' \
    -a "(%s)" -d 'repo package'
complete -c alps -n 'set -l t (commandline -poc); contains -- "$t[2]" aur; and contains -- "$t[3]" install ins search se' \
    -a "(%s)" -d 'AUR package'

# aur info/clone → AUR packages only
complete -c alps -n 'set -l t (commandline -poc); contains -- "$t[2]" aur; and contains -- "$t[3]" info clone' \
    -a "(%s)" -d 'AUR package'

# aur remove → AUR-installed packages
complete -c alps -n 'set -l t (commandline -poc); contains -- "$t[2]" aur; and contains -- "$t[3]" remove rm' \
    -a "(%s)" -d 'AUR installed package'

# aur build-local / bl → directories
complete -c alps -n 'set -l t (commandline -poc); contains -- "$t[2]" aur; and contains -- "$t[3]" build-local bl' \
    -a "(__fish_complete_directories)" -d 'directory'

# flatpak subcommands (fp alias included)
complete -c alps -n 'set -l t (commandline -poc); contains -- "$t[2]" flatpak fp; and test (count $t) -eq 2' \
    -a 'install remove purge search show list update upgrade autoremove clean' -d 'flatpak subcommand'

# snap subcommands (sk alias included)
complete -c alps -n 'set -l t (commandline -poc); contains -- "$t[2]" snap sk; and test (count $t) -eq 2' \
    -a 'install remove purge search show list update upgrade autoremove clean' -d 'snap subcommand'

# winget subcommands (wg alias included)
complete -c alps -n 'set -l t (commandline -poc); contains -- "$t[2]" winget wg; and test (count $t) -eq 2' \
    -a 'install remove purge search show list update upgrade' -d 'winget subcommand'

# top-level install/search → all repo packages
complete -c alps -n 'set -l t (commandline -poc); contains -- "$t[2]" install ins search se; and test (count $t) -eq 2' \
    -a "(%s)" -d 'package'

# top-level remove/purge → installed packages
complete -c alps -n 'set -l t (commandline -poc); contains -- "$t[2]" remove rm purge pu; and test (count $t) -eq 2' \
    -a "(%s)" -d 'installed package'
`, morePkgs, moreInstalled,
	pkgList, aurNames,
	aurNames,
	aurInstalled,
	pkgList,
	installedList)
}

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
                list)
                    COMPREPLY=($(compgen -W "install remove" -- "$cur"))
                    ;;
                install|search)
                    COMPREPLY=($(compgen -W "$(%s)" -- "$cur"))
                    ;;
                remove|purge|upgrade)
                    COMPREPLY=($(compgen -W "$(%s)" -- "$cur"))
                    ;;
                *)
                    COMPREPLY=($(compgen -W "update list install remove purge search upgrade clean" -- "$cur"))
                    ;;
            esac
            ;;
        aur)
            case "${words[2]}" in
                install|search)
                    COMPREPLY=($(compgen -W "$(%s) $(%s)" -- "$cur"))
                    ;;
                info|clone)
                    COMPREPLY=($(compgen -W "$(%s)" -- "$cur"))
                    ;;
                remove)
                    COMPREPLY=($(compgen -W "$(%s)" -- "$cur"))
                    ;;
                build-local)
                    COMPREPLY=($(compgen -d -- "$cur"))
                    ;;
                fetch-abs|orphans)
                    ;;
                *)
                    COMPREPLY=($(compgen -W "install search list remove clean build-local fetch-abs info clone orphans" -- "$cur"))
                    ;;
            esac
            ;;
        winget)
            case "${words[2]}" in
                *)
                    COMPREPLY=($(compgen -W "install remove purge search show list update upgrade" -- "$cur"))
                    ;;
            esac
            ;;
        flatpak)
            case "${words[2]}" in
                *)
                    COMPREPLY=($(compgen -W "install remove purge search show list update upgrade autoremove clean" -- "$cur"))
                    ;;
            esac
            ;;
        snap)
            case "${words[2]}" in
                *)
                    COMPREPLY=($(compgen -W "install remove purge search show list update upgrade autoremove clean" -- "$cur"))
                    ;;
            esac
            ;;
    esac
}

complete -F _alps_completions alps
`, cmdList,
		pkgList, installedList,
		morePkgs, moreInstalled,
		pkgList, aurNames, aurInstalled,
		aurNames)
}

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
                        list)
                            _describe 'list action' '(install remove)'
                            ;;
                        install|search)
                            local morepkgs
                            morepkgs=(${(f)"$(%s)"})
                            _describe 'alps-more package' morepkgs
                            ;;
                        remove|purge|upgrade)
                            local moreinst
                            moreinst=(${(f)"$(%s)"})
                            _describe 'installed alps-more package' moreinst
                            ;;
                        *)
                            _describe 'repo subcommand' \
                                '(update list install remove purge search upgrade clean)'
                            ;;
                    esac
                    ;;
                aur)
                    case ${words[3]} in
                        install|search)
                            local repopkgs
                            repopkgs=(${(f)"$(%s)"})
                            _describe 'repo package' repopkgs
                            local aurpkgs
                            aurpkgs=(${(f)"$(%s)"})
                            _describe 'AUR package' aurpkgs
                            ;;
                        info|clone)
                            local aurpkgs
                            aurpkgs=(${(f)"$(%s)"})
                            _describe 'AUR package' aurpkgs
                            ;;
                        remove)
                            local aurinst
                            aurinst=(${(f)"$(%s)"})
                            _describe 'AUR installed package' aurinst
                            ;;
                        build-local)
                            _path_files -/
                            ;;
                        fetch-abs|orphans)
                            ;;
                        *)
                            _describe 'aur subcommand' \
                                '(install search list remove clean build-local fetch-abs info clone orphans)'
                            ;;
                    esac
                    ;;
                winget)
                    case ${words[3]} in
                        *)
                            _describe 'winget subcommand' \
                                '(install remove purge search show list update upgrade)'
                            ;;
                    esac
                    ;;
                flatpak)
                    case ${words[3]} in
                        *)
                            _describe 'flatpak subcommand' \
                                '(install remove purge search show list update upgrade autoremove clean)'
                            ;;
                    esac
                    ;;
                snap)
                    case ${words[3]} in
                        *)
                            _describe 'snap subcommand' \
                                '(install remove purge search show list update upgrade autoremove clean)'
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
		morePkgs, moreInstalled,
		pkgList, aurNames, aurInstalled,
		aurNames)
}

// cmdDesc returns a description for a command.
func cmdDesc(cmd string) string {
	descs := map[string]string{
		"help":         "show help",
		"aliases":      "show aliases",
		"config-show":  "show config",
		"version":      "show version",
		"completion":   "generate shell completion",
		"repo":         "manage alps-more repo packages",
		"aur":          "manage AUR packages directly",
		"winget":       "manage winget packages (WSL)",
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
		"info":         "show AUR package metadata",
		"clone":        "clone AUR PKGBUILD for inspection",
		"orphans":      "list AUR orphan packages",
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
