package completion

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"alps/config"
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

func detectBackend() string {
	for _, b := range []string{"apt", "dnf", "pacman"} {
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
	default: // apt
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
	default: // apt
		return "dpkg --get-selections 2>/dev/null | awk '{print $1}'"
	}
}

func builtinCmds() []string {
	return []string{
		"help", "aliases", "config-show", "version",
		"install", "remove", "purge", "update", "upgrade",
		"full-upgrade", "search", "show", "list",
		"autoremove", "autoclean", "clean",
	}
}

func aliasKeys(cfg *config.Config) []string {
	keys := make([]string, 0, len(cfg.Aliases))
	for k := range cfg.Aliases {
		keys = append(keys, k)
	}
	return keys
}

// ── Fish ──────────────────────────────────────────────────────────

func genFish(cmds []string, backend string) {
	fmt.Println("# alps fish completion")
	fmt.Println("# Install: alps completion fish > ~/.config/fish/completions/alps.fish")
	fmt.Println()
	fmt.Println("complete -c alps -f")
	fmt.Println()

	for _, cmd := range cmds {
		fmt.Printf("complete -c alps -n '__fish_use_subcommand' -a '%s' -d '%s'\n",
			cmd, cmdDesc(cmd))
	}

	pkgList := pkgListCmd(backend)
	installedList := installedListCmd(backend)

	fmt.Printf(`
# Available package completion for install/search
complete -c alps -n '__fish_seen_subcommand_from install ins i search se' \
    -a '(%s)' -d 'package'

# Installed package completion for remove/purge
complete -c alps -n '__fish_seen_subcommand_from remove rm purge pu' \
    -a '(%s)' -d 'installed'
`, pkgList, installedList)
}

// ── Bash ──────────────────────────────────────────────────────────

func genBash(cmds []string, backend string) {
	cmdList := strings.Join(cmds, " ")
	pkgList := pkgListCmd(backend)
	installedList := installedListCmd(backend)

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
    esac
}

complete -F _alps_completions alps
`, cmdList, pkgList, installedList)
}

// ── Zsh ───────────────────────────────────────────────────────────

func genZsh(cmds []string, backend string) {
	cmdList := make([]string, 0, len(cmds))
	for _, c := range cmds {
		cmdList = append(cmdList, fmt.Sprintf("'%s:%s'", c, cmdDesc(c)))
	}

	pkgList := pkgListCmd(backend)
	installedList := installedListCmd(backend)

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
            esac
            ;;
    esac
}

_alps
`, strings.Join(cmdList, "\n                "), pkgList, installedList)
}

func cmdDesc(cmd string) string {
	descs := map[string]string{
		"help":         "show help",
		"aliases":      "show aliases",
		"config-show":  "show config",
		"version":      "show version",
		"completion":   "generate shell completion",
		"install":      "install package",
		"remove":       "remove package",
		"purge":        "purge package",
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

func effectiveCmds(cfg *config.Config) []string {
	if hasCustomAliases(cfg) {
		cmds := []string{"help", "aliases", "config-show", "version", "repo"}
		for k := range cfg.Aliases {
			cmds = append(cmds, k)
		}
		return cmds
	}
	return []string{
		"help", "aliases", "config-show", "version", "repo",
		"install", "remove", "purge", "update", "upgrade",
		"full-upgrade", "search", "show", "list",
		"autoremove", "autoclean", "clean",
	}
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
