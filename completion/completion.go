package completion

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/adrianpriza-ai/alps/cli"
	"github.com/adrianpriza-ai/alps/config"
	"github.com/adrianpriza-ai/alps/extra"
	"github.com/adrianpriza-ai/alps/platform"
)

// Generate writes a shell completion script to w. It returns an error for an
// unsupported shell; the caller decides the exit status.
func Generate(shell string, w io.Writer) error {
	cmds := effectiveCmds()
	aliases := aliasWords(cmds)
	subcmds := subcommandWords()
	backend := detectBackend()

	switch shell {
	case "fish":
		return genFish(w, cmds, aliases, subcmds, backend)
	case "bash":
		return genBash(w, cmds, aliases, subcmds, backend)
	case "zsh":
		return genZsh(w, cmds, aliases, subcmds, backend)
	default:
		return fmt.Errorf("unknown shell: %s (supported: fish, bash, zsh)", shell)
	}
}

// cacheFile and installedFile point into the platform-owned directories, so
// the generated scripts read the same paths the program writes on every
// supported platform (including macOS).
func cacheFile() string     { return filepath.Join(platform.CacheDir(), "main.txt") }
func installedFile() string { return filepath.Join(platform.LibDir(), "installed.json") }

// AURNamesCachePath returns the path for the AUR package names cache. It reuses
// the same invoking-user resolution as the AUR build cache (platform.UserCacheRoot)
// so the file written under sudo by the names-cache writer is the same file the
// generated completion command reads. The $HOME fallback preserves the previous
// behaviour when the home directory cannot be determined.
func AURNamesCachePath() string {
	root, err := platform.UserCacheRoot()
	if err != nil {
		if home := os.Getenv("HOME"); home != "" {
			return filepath.Join(home, ".cache", "alps", "aur-names.txt")
		}
		return ""
	}
	return filepath.Join(root, "aur-names.txt")
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
		return "zypper -q packages 2>/dev/null | awk -F'|' 'NR>2{gsub(/[[:space:]],\"\",$3); print $3}' | sort -u"
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
		return "zypper -q packages --installed-only 2>/dev/null | awk -F'|' 'NR>2{gsub(/[[:space:]],\"\",$3); print $3}'"
	case "apk":
		return "apk info 2>/dev/null"
	default:
		return "dpkg --get-selections 2>/dev/null | awk '{print $1}'"
	}
}

// moreListCmd lists package names from cache. The path is single-quoted so a
// space or shell metacharacter in it cannot break the command.
func moreListCmd(path string) string {
	return fmt.Sprintf(`grep '^\[' %s 2>/dev/null | tr -d '[]'`, shellQuote(path))
}

// moreInstalledCmd lists installed packages.
func moreInstalledCmd(path string) string {
	return fmt.Sprintf(`jq -r 'keys[]' %s 2>/dev/null`, shellQuote(path))
}

// aurNamesCmd reads the AUR package name cache. The path is baked in at
// generation time from AURNamesCachePath — the same path the program's cache
// writer uses — so the reader and the writer cannot drift apart.
func aurNamesCmd() string {
	return fmt.Sprintf("cat %s 2>/dev/null", shellQuote(AURNamesCachePath()))
}

// aurInstalledCmd lists AUR-installed packages.
func aurInstalledCmd() string {
	return `pacman -Qm 2>/dev/null | awk '{print $1}'`
}

// subcommandWords returns the valid subcommand words per subsystem, sourced
// from cli so the generated scripts cannot drift from the command tables.
func subcommandWords() map[string]string {
	words := make(map[string]string, 5)
	for _, sys := range []string{"repo", "aur", "winget", "flatpak", "snap"} {
		words[sys] = strings.Join(cli.ValidSubCmds(sys), " ")
	}
	return words
}

// aliasWords returns the alias spellings (built-in and config-defined) that
// resolve to one of cmds, keyed by alias, so the short forms complete
// alongside the long commands. Aliases whose target is not offered on this
// system, that duplicate a command name, or that are not plain shell words
// are skipped.
func aliasWords(cmds []string) map[string]string {
	targets := make(map[string]bool, len(cmds))
	for _, c := range cmds {
		targets[c] = true
	}
	aliases := make(map[string]string)
	for a, target := range config.Load().Aliases {
		if targets[target] && !targets[a] && safeWord(a) {
			aliases[a] = target
		}
	}
	return aliases
}

// safeWord reports whether s is a plain shell word that can be embedded in
// the generated scripts without quoting.
func safeWord(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case r == '-' || r == '_' || r == '+' || r == '.' || r == '@':
		default:
			return false
		}
	}
	return true
}

// sortedKeys returns the keys of m in sorted order so generated output is
// stable across runs.
func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// writef prints format with args after checking that the %s verb count
// matches the argument count, so a drifted template fails with an error at
// test time instead of emitting %!s(MISSING) into a user's shell completion.
func writef(w io.Writer, format string, args ...any) error {
	if n := strings.Count(format, "%s"); n != len(args) {
		return fmt.Errorf("completion template drift: %d %%s verbs, %d arguments", n, len(args))
	}
	_, err := fmt.Fprintf(w, format, args...)
	return err
}

// shellQuote wraps s in single quotes for safe use inside a shell command,
// escaping any embedded single quotes (POSIX sh compatible).
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func genFish(w io.Writer, cmds []string, aliases map[string]string, subcmds map[string]string, backend string) error {
	pkgList := pkgListCmd(backend)
	installedList := installedListCmd(backend)
	morePkgs := moreListCmd(cacheFile())
	moreInstalled := moreInstalledCmd(installedFile())
	aurNames := aurNamesCmd()
	aurInstalled := aurInstalledCmd()

	fmt.Fprintln(w, "# alps fish completion")
	fmt.Fprintln(w, "# Install: alps completion fish > ~/.config/fish/completions/alps.fish")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "complete -c alps -f")
	fmt.Fprintln(w)

	for _, cmd := range cmds {
		if err := writef(w, "complete -c alps -n '__fish_use_subcommand' -a '%s' -d '%s'\n",
			cmd, cmdDesc(cmd)); err != nil {
			return err
		}
	}
	for _, a := range sortedKeys(aliases) {
		if err := writef(w, "complete -c alps -n '__fish_use_subcommand' -a '%s' -d 'alias for %s'\n",
			a, aliases[a]); err != nil {
			return err
		}
	}

	return writef(w, `
# top-level commands
# $t[1]=alps  $t[2]=cmd  $t[3]=subcmd  $t[4]=arg

# repo subcommands
complete -c alps -n 'set -l t (commandline -poc); contains -- "$t[2]" repo; and test (count $t) -eq 2' \
    -a '%s' -d 'repo subcommand'

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
    -a '%s' -d 'aur subcommand'

# aur install/search → pacman repo + AUR
complete -c alps -n 'set -l t (commandline -poc); contains -- "$t[2]" aur; and contains -- "$t[3]" install ins search se' \
    -a "(%s)" -d 'repo package'
complete -c alps -n 'set -l t (commandline -poc); contains -- "$t[2]" aur; and contains -- "$t[3]" install ins search se' \
    -a "(%s)" -d 'AUR package'

# aur info/clone → AUR packages only
complete -c alps -n 'set -l t (commandline -poc); contains -- "$t[2]" aur; and contains -- "$t[3]" info clone' \
    -a "(%s)" -d 'AUR package'

# aur remove/update/upgrade → AUR-installed packages
complete -c alps -n 'set -l t (commandline -poc); contains -- "$t[2]" aur; and contains -- "$t[3]" remove rm update upgrade' \
    -a "(%s)" -d 'AUR installed package'

# aur build-local / bl → directories
complete -c alps -n 'set -l t (commandline -poc); contains -- "$t[2]" aur; and contains -- "$t[3]" build-local bl' \
    -a "(__fish_complete_directories)" -d 'directory'

# flatpak subcommands (fp alias included)
complete -c alps -n 'set -l t (commandline -poc); contains -- "$t[2]" flatpak fp; and test (count $t) -eq 2' \
    -a '%s' -d 'flatpak subcommand'

# snap subcommands (sk alias included)
complete -c alps -n 'set -l t (commandline -poc); contains -- "$t[2]" snap sk; and test (count $t) -eq 2' \
    -a '%s' -d 'snap subcommand'

# winget subcommands (wg alias included)
complete -c alps -n 'set -l t (commandline -poc); contains -- "$t[2]" winget wg; and test (count $t) -eq 2' \
    -a '%s' -d 'winget subcommand'

# top-level install/search → all repo packages
complete -c alps -n 'set -l t (commandline -poc); contains -- "$t[2]" install ins search se; and test (count $t) -eq 2' \
    -a "(%s)" -d 'package'

# top-level remove/purge → installed packages
complete -c alps -n 'set -l t (commandline -poc); contains -- "$t[2]" remove rm purge pu; and test (count $t) -eq 2' \
    -a "(%s)" -d 'installed package'
`, subcmds["repo"], morePkgs, moreInstalled,
		subcmds["aur"], pkgList, aurNames,
		aurNames,
		aurInstalled,
		subcmds["flatpak"],
		subcmds["snap"],
		subcmds["winget"],
		pkgList,
		installedList)
}

func genBash(w io.Writer, cmds []string, aliases map[string]string, subcmds map[string]string, backend string) error {
	words := append([]string{}, cmds...)
	words = append(words, sortedKeys(aliases)...)
	cmdList := strings.Join(words, " ")

	pkgList := pkgListCmd(backend)
	installedList := installedListCmd(backend)
	morePkgs := moreListCmd(cacheFile())
	moreInstalled := moreInstalledCmd(installedFile())
	aurNames := aurNamesCmd()
	aurInstalled := aurInstalledCmd()

	return writef(w, `# alps bash completion
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
                    COMPREPLY=($(compgen -W "%s" -- "$cur"))
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
                remove|update|upgrade)
                    COMPREPLY=($(compgen -W "$(%s)" -- "$cur"))
                    ;;
                build-local)
                    COMPREPLY=($(compgen -d -- "$cur"))
                    ;;
                fetch-abs|orphans)
                    ;;
                *)
                    COMPREPLY=($(compgen -W "%s" -- "$cur"))
                    ;;
            esac
            ;;
        winget)
            case "${words[2]}" in
                *)
                    COMPREPLY=($(compgen -W "%s" -- "$cur"))
                    ;;
            esac
            ;;
        flatpak)
            case "${words[2]}" in
                *)
                    COMPREPLY=($(compgen -W "%s" -- "$cur"))
                    ;;
            esac
            ;;
        snap)
            case "${words[2]}" in
                *)
                    COMPREPLY=($(compgen -W "%s" -- "$cur"))
                    ;;
            esac
            ;;
    esac
}

complete -F _alps_completions alps
`, cmdList,
		pkgList, installedList,
		morePkgs, moreInstalled,
		subcmds["repo"],
		pkgList, aurNames, aurNames,
		aurInstalled,
		subcmds["aur"],
		subcmds["winget"],
		subcmds["flatpak"],
		subcmds["snap"])
}

func genZsh(w io.Writer, cmds []string, aliases map[string]string, subcmds map[string]string, backend string) error {
	cmdList := make([]string, 0, len(cmds)+len(aliases))
	for _, c := range cmds {
		cmdList = append(cmdList, fmt.Sprintf("'%s:%s'", c, cmdDesc(c)))
	}
	for _, a := range sortedKeys(aliases) {
		cmdList = append(cmdList, fmt.Sprintf("'%s:alias for %s'", a, aliases[a]))
	}

	pkgList := pkgListCmd(backend)
	installedList := installedListCmd(backend)
	morePkgs := moreListCmd(cacheFile())
	moreInstalled := moreInstalledCmd(installedFile())
	aurNames := aurNamesCmd()
	aurInstalled := aurInstalledCmd()

	return writef(w, `#compdef alps
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
                            local -a list_actions
                            list_actions=(install remove)
                            _describe 'list action' list_actions
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
                            local -a repo_subcmds
                            repo_subcmds=(%s)
                            _describe 'repo subcommand' repo_subcmds
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
                        remove|update|upgrade)
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
                            local -a aur_subcmds
                            aur_subcmds=(%s)
                            _describe 'aur subcommand' aur_subcmds
                            ;;
                    esac
                    ;;
                winget)
                    case ${words[3]} in
                        *)
                            local -a winget_subcmds
                            winget_subcmds=(%s)
                            _describe 'winget subcommand' winget_subcmds
                            ;;
                    esac
                    ;;
                flatpak)
                    case ${words[3]} in
                        *)
                            local -a flatpak_subcmds
                            flatpak_subcmds=(%s)
                            _describe 'flatpak subcommand' flatpak_subcmds
                            ;;
                    esac
                    ;;
                snap)
                    case ${words[3]} in
                        *)
                            local -a snap_subcmds
                            snap_subcmds=(%s)
                            _describe 'snap subcommand' snap_subcmds
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
		subcmds["repo"],
		pkgList, aurNames, aurNames,
		aurInstalled,
		subcmds["aur"],
		subcmds["winget"],
		subcmds["flatpak"],
		subcmds["snap"])
}

// cmdDesc returns a description for a command, sourced from the cli package
// so help text and completion text cannot drift apart.
func cmdDesc(cmd string) string {
	return cli.CommandDesc(cmd)
}

// subsystemCmds lists the commands gated by environment: they are appended
// to the base list only when the distro or installed tooling offers them.
var subsystemCmds = map[string]bool{
	"aur": true, "winget": true, "flatpak": true, "snap": true,
}

// effectiveCmds returns the command list for this distro/environment,
// derived from cli.Commands(). Subsystem commands appear only when the
// program can actually run them: the gates match extra.IsAvailable, the
// authority the real command path uses.
func effectiveCmds() []string {
	base := make([]string, 0, len(cli.Commands()))
	for _, cmd := range cli.Commands() {
		if !subsystemCmds[cmd] {
			base = append(base, cmd)
		}
	}

	if platform.IsTermux() {
		return base
	}

	switch {
	case platform.IsArchBased():
		base = append(base, "aur")
	case platform.IsDebianBased():
		if extra.IsAvailable("snap") {
			base = append(base, "snap")
		}
	}

	if extra.IsAvailable("flatpak") {
		base = append(base, "flatpak")
	}
	if extra.IsAvailable("winget") {
		base = append(base, "winget")
	}

	return base
}
