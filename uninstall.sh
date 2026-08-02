#!/bin/sh
# Usage: curl -fsSL https://alps-project.pages.dev/uninstall.sh | sh
# Purge config/cache too: curl -fsSL https://alps-project.pages.dev/uninstall.sh | sh -s -- --purge

set -e

REPO="adrianpriza-ai/alps"
SITE_URL="https://alps-project.pages.dev"

is_utf8() {
    case "${LC_ALL:-${LC_CTYPE:-${LANG:-}}}" in
        *UTF-8*|*utf-8*|*UTF8*|*utf8*) return 0 ;;
    esac
    return 1
}

if [ -t 1 ] && is_utf8; then
    GREEN='\033[0;32m'; BLUE='\033[0;34m'; YELLOW='\033[0;33m'
    RED='\033[0;31m'; BOLD='\033[1m'; DIM='\033[2m'; RST='\033[0m'
    SYM_INFO='◆'; SYM_OK='✓'; SYM_WARN='⚠'; SYM_ERR='✗'
else
    if [ -t 1 ]; then
        GREEN='\033[0;32m'; BLUE='\033[0;34m'; YELLOW='\033[0;33m'
        RED='\033[0;31m'; BOLD='\033[1m'; DIM='\033[2m'; RST='\033[0m'
    else
        GREEN=''; BLUE=''; YELLOW=''; RED=''; BOLD=''; DIM=''; RST=''
    fi
    SYM_INFO='[INFO]'; SYM_OK='[ OK ]'; SYM_WARN='[WARN]'; SYM_ERR='[ERRO]'
fi

info()  { printf "  ${BLUE}${SYM_INFO}${RST}  %s\n" "$1"; }
ok()    { printf "  ${GREEN}${SYM_OK}${RST}  %s\n" "$1"; }
warn()  { printf "  ${YELLOW}${SYM_WARN}${RST}  %s\n" "$1"; }
die()   { printf "  ${RED}${SYM_ERR}${RST}  %s\n" "$1" >&2; exit 1; }

http_get() {
    _http_get_url="$1"
    if command -v curl >/dev/null 2>&1; then
        curl -fsSL "$_http_get_url"
    elif command -v wget >/dev/null 2>&1; then
        wget -qO- "$_http_get_url"
    else
        die "Neither curl nor wget is installed."
    fi
}

download_file() {
    _download_url="$1"
    _download_dest="$2"
    if command -v curl >/dev/null 2>&1; then
        curl -fsSL "$_download_url" -o "$_download_dest"
    elif command -v wget >/dev/null 2>&1; then
        wget -qO "$_download_dest" "$_download_url"
    else
        die "Neither curl nor wget is installed."
    fi
}

is_termux() {
    [ -n "$TERMUX_VERSION" ] || [ "$PREFIX" = "/data/data/com.termux/files/usr" ]
}

is_macos() {
    [ "$(uname -s)" = "Darwin" ]
}

detect_privileges() {
    if [ "$(id -u)" -eq 0 ]; then
        UNINSTALL_MODE="system"
        SUDO=""
        info "Running as root. Will uninstall system-wide."
    else
        SUDO=""
        if command -v sudo >/dev/null 2>&1 && sudo -n true 2>/dev/null; then
            SUDO="sudo"
        elif command -v doas >/dev/null 2>&1 && doas -n true 2>/dev/null; then
            SUDO="doas"
        elif command -v sudo >/dev/null 2>&1 && [ -t 1 ]; then
            SUDO="sudo"
        elif command -v doas >/dev/null 2>&1 && [ -t 1 ]; then
            SUDO="doas"
        fi

        if [ -n "$SUDO" ]; then
            UNINSTALL_MODE="system"
            info "$SUDO privileges detected. Will attempt system-wide uninstallation."
        else
            UNINSTALL_MODE="user"
            if command -v sudo >/dev/null 2>&1 || command -v doas >/dev/null 2>&1; then
                info "sudo/doas found but not usable (non-interactive or no passwordless access). Will attempt user-local uninstallation."
            else
                info "No sudo/root access available. Will attempt user-local uninstallation."
            fi
        fi
    fi
}

is_debian_based() {
    [ -f /etc/os-release ] || return 1
    . /etc/os-release
    case "${ID:-} ${ID_LIKE:-}" in
        *debian*|*ubuntu*) return 0 ;;
    esac
    return 1
}

is_arch_based() {
    [ -f /etc/os-release ] || return 1
    . /etc/os-release
    case "${ID:-} ${ID_LIKE:-}" in
        *arch*) return 0 ;;
    esac
    if command -v pacman >/dev/null 2>&1 && command -v makepkg >/dev/null 2>&1; then
        return 0
    fi
    return 1
}

is_alpine() {
    [ -f /etc/os-release ] || return 1
    . /etc/os-release
    case "${ID:-} ${ID_LIKE:-}" in
        *alpine*) return 0 ;;
    esac
    if command -v apk >/dev/null 2>&1 && command -v abuild >/dev/null 2>&1; then
        return 0
    fi
    return 1
}

is_package_installed() {
    pkg_name="$1"
    if command -v pacman >/dev/null 2>&1 && pacman -Qi "$pkg_name" >/dev/null 2>&1; then
        echo "pacman"
        return 0
    elif command -v apk >/dev/null 2>&1 && apk info "$pkg_name" >/dev/null 2>&1; then
        echo "apk"
        return 0
    elif command -v dpkg >/dev/null 2>&1 && dpkg -s "$pkg_name" >/dev/null 2>&1; then
        echo "dpkg"
        return 0
    fi
    return 1
}

remove_file() {
    file="$1"
    if [ -f "$file" ] || [ -L "$file" ]; then
        if [ -w "$(dirname "$file")" ] 2>/dev/null; then
            rm -f "$file"
        elif [ -n "$SUDO" ]; then
            if ! $SUDO rm -f "$file"; then
                warn "Could not remove $file"
                return 1
            fi
        else
            warn "Skipping $file; permission denied and sudo is unavailable."
            return 1
        fi
        ok "Removed $file"
        return 0
    fi
    return 0
}

remove_dir() {
    dir="$1"
    if [ -d "$dir" ]; then
        if [ -w "$(dirname "$dir")" ] 2>/dev/null; then
            rm -rf "$dir"
        elif [ -n "$SUDO" ]; then
            if ! $SUDO rm -rf "$dir"; then
                warn "Could not purge $dir"
                return 1
            fi
        else
            warn "Skipping $dir; permission denied and sudo is unavailable."
            return 1
        fi
        ok "Purged directory $dir"
        return 0
    fi
    return 0
}

remove_symlink() {
    link="$1"
    if [ -L "$link" ]; then
        remove_file "$link"
        return 0
    fi
    return 0
}

cleanup_path() {
    local bin_dir="$1"
    bin_dir="${bin_dir%/}"

    case ":$PATH:" in
        *:"$bin_dir":*)
            shell_profile=""
            shell_name=$(basename "${SHELL:-bash}")

            case "$shell_name" in
                bash)
                    if [ -f "$HOME/.bashrc" ]; then
                        shell_profile="$HOME/.bashrc"
                    elif [ -f "$HOME/.bash_profile" ]; then
                        shell_profile="$HOME/.bash_profile"
                    fi
                    ;;
                zsh)
                    if [ -f "$HOME/.zshrc" ]; then
                        shell_profile="$HOME/.zshrc"
                    fi
                    ;;
                fish)
                    if [ -d "$HOME/.config/fish" ]; then
                        shell_profile="$HOME/.config/fish/config.fish"
                    fi
                    ;;
            esac

            if [ -z "$shell_profile" ]; then
                if [ -f "$HOME/.profile" ]; then
                    shell_profile="$HOME/.profile"
                fi
            fi

            if [ -n "$shell_profile" ]; then
                if [ "$shell_name" = "fish" ]; then
                    if grep -F "fish_add_path $bin_dir" "$shell_profile" >/dev/null 2>&1; then
                        info "Removing fish_add_path entry from $shell_profile..."
                        sed -i "/fish_add_path $bin_dir/d; /^# Added by ALPS installer$/d" "$shell_profile"
                        ok "Cleaned up fish_add_path from $shell_profile"
                    fi
                else
                    if grep -F "$bin_dir" "$shell_profile" >/dev/null 2>&1; then
                        info "Removing PATH entry from $shell_profile..."
                        sed -i "/export PATH.*$bin_dir/d; /^# Added by ALPS installer$/d" "$shell_profile"
                        ok "Cleaned up PATH from $shell_profile"
                    fi
                fi
            else
                warn "Could not find shell profile to clean up PATH from. You may need to manually remove $bin_dir from your PATH."
            fi
            ;;
    esac
}

remove_completions_system() {
    info "Removing system-wide shell completions..."

    remove_file "/usr/share/fish/vendor_completions.d/alps.fish"
    remove_file "/usr/share/fish/vendor_completions.d/alps-pm.fish"
    remove_file "/usr/share/fish/completions/alps.fish"
    remove_file "/usr/share/fish/completions/alps-pm.fish"

    remove_file "/usr/share/bash-completion/completions/alps"
    remove_file "/usr/share/bash-completion/completions/alps-pm"

    remove_file "/usr/share/zsh/site-functions/_alps"
    remove_file "/usr/share/zsh/site-functions/_alps-pm"
}

remove_completions_termux() {
    info "Removing Termux shell completions..."

    if [ -z "$PREFIX" ]; then
        warn "Termux detected but \$PREFIX is not set. Skipping Termux completion removal."
        return
    fi

    remove_file "$PREFIX/share/fish/vendor_completions.d/alps.fish"
    remove_file "$PREFIX/share/fish/vendor_completions.d/alps-pm.fish"

    remove_file "$PREFIX/share/bash-completion/completions/alps"
    remove_file "$PREFIX/share/bash-completion/completions/alps-pm"

    remove_file "$PREFIX/share/zsh/site-functions/_alps"
    remove_file "$PREFIX/share/zsh/site-functions/_alps-pm"
}

remove_completions_user() {
    info "Removing user-local shell completions..."

    remove_file "$HOME/.config/fish/completions/alps.fish"
    remove_file "$HOME/.config/fish/completions/alps-pm.fish"

    remove_file "$HOME/.local/share/bash-completion/completions/alps"
    remove_file "$HOME/.local/share/bash-completion/completions/alps-pm"

    remove_file "$HOME/.zsh/completion/_alps"
    remove_file "$HOME/.zsh/completion/_alps-pm"
}

remove_completions_macos() {
    info "Removing macOS shell completions..."

    remove_file "$HOME/.config/fish/completions/alps.fish"
    remove_file "$HOME/.config/fish/completions/alps-pm.fish"

    remove_file "$HOME/.bash_completion.d/alps"
    remove_file "$HOME/.bash_completion.d/alps-pm"

    remove_file "/usr/local/share/zsh/site-functions/_alps"
    remove_file "/usr/local/share/zsh/site-functions/_alps-pm"
}

remove_config_cache_macos() {
    info "Purging macOS configurations and caches..."

    remove_dir "$HOME/Library/Application Support/alps"
    remove_dir "$HOME/Library/Caches/alps"
}

remove_binaries_macos() {
    info "Removing macOS binary files..."

    remove_file "/usr/local/bin/alps"
    remove_file "/usr/local/bin/alps-pm"
    remove_symlink "/usr/local/bin/alps"
}

remove_package_manager() {
    pkg_manager=""
    pkg_name="alps-pm"

    pkg_manager=$(is_package_installed "$pkg_name") || return 0

    case "$pkg_manager" in
        pacman)
            info "Detected pacman installation. Removing via pacman..."
            if [ "$UNINSTALL_MODE" = "system" ] || [ -n "$SUDO" ]; then
                if [ "$PURGE" -eq 1 ]; then
                    if ! $SUDO pacman -Rns "$pkg_name" --noconfirm; then
                        warn "Pacman removal with config purge failed. Trying without purge..."
                        $SUDO pacman -Rns "$pkg_name" --noconfirm || warn "Pacman removal failed."
                    fi
                else
                    if ! $SUDO pacman -Rns "$pkg_name" --noconfirm; then
                        warn "Pacman removal failed."
                    fi
                fi
            else
                warn "Skipping pacman removal; sudo is unavailable."
            fi
            ;;
        apk)
            info "Detected apk installation. Removing via apk..."
            if [ "$UNINSTALL_MODE" = "system" ] || [ -n "$SUDO" ]; then
                if ! $SUDO apk del "$pkg_name"; then
                    warn "Apk removal failed."
                fi
            else
                warn "Skipping apk removal; sudo is unavailable."
            fi
            ;;
        dpkg)
            info "Detected dpkg installation. Removing via dpkg/apt..."
            if [ "$UNINSTALL_MODE" = "system" ] || [ -n "$SUDO" ]; then
                if [ "$PURGE" -eq 1 ]; then
                    if ! $SUDO apt-get purge -y "$pkg_name"; then
                        warn "Apt purge failed. Trying dpkg..."
                        if ! $SUDO dpkg -P "$pkg_name"; then
                            warn "Dpkg purge failed."
                        fi
                    fi
                else
                    if ! $SUDO apt-get remove -y "$pkg_name"; then
                        warn "Apt removal failed. Trying dpkg..."
                        if ! $SUDO dpkg -r "$pkg_name"; then
                            warn "Dpkg removal failed."
                        fi
                    fi
                fi
            else
                warn "Skipping dpkg removal; sudo is unavailable."
            fi
            ;;
    esac
}

remove_binaries_system() {
    info "Removing system-wide binary files..."

    remove_file "/usr/local/bin/alps"
    remove_file "/usr/local/bin/alps-pm"
    remove_symlink "/usr/local/bin/alps"

    remove_file "/usr/bin/alps"
    remove_file "/usr/bin/alps-pm"
    remove_symlink "/usr/bin/alps"
}

remove_binaries_termux() {
    info "Removing Termux binary files..."

    if [ -z "$PREFIX" ]; then
        warn "Termux detected but \$PREFIX is not set. Skipping Termux binary removal."
        return
    fi

    remove_file "$PREFIX/bin/alps"
    remove_file "$PREFIX/bin/alps-pm"
    remove_symlink "$PREFIX/bin/alps"
}

remove_binaries_user() {
    info "Removing user-local binary files..."

    remove_file "$HOME/.local/bin/alps"
    remove_file "$HOME/.local/bin/alps-pm"
    remove_symlink "$HOME/.local/bin/alps"
}

remove_config_cache_system() {
    info "Purging system-wide configurations and caches..."

    remove_dir "/etc/alps"
    remove_dir "/var/cache/alps"
}

remove_config_cache_user() {
    info "Purging user configurations and caches..."

    remove_dir "$HOME/.config/alps"
}

remove_config_cache_termux() {
    info "Purging Termux configurations and caches..."

    if [ -z "$PREFIX" ]; then
        warn "Termux detected but \$PREFIX is not set. Skipping Termux config removal."
        return
    fi

    remove_dir "$PREFIX/etc/alps"
    remove_dir "$PREFIX/var/cache/alps"
}

uninstall_termux() {
    info "Uninstalling ALPS from Termux..."

    remove_binaries_termux
    remove_completions_termux

    if [ "$PURGE" -eq 1 ]; then
        remove_config_cache_termux
        remove_config_cache_user
    fi

    remove_binaries_user
    remove_completions_user
}

uninstall_package_manager() {
    info "Checking for package manager installations..."

    remove_package_manager

    remove_binaries_system
    remove_completions_system

    if [ "$PURGE" -eq 1 ]; then
        remove_config_cache_system
        remove_config_cache_user
    fi

    remove_binaries_user
    remove_completions_user
}

uninstall_standard() {
    info "Performing standard uninstallation..."

    remove_package_manager

    remove_binaries_system
    remove_completions_system

    remove_binaries_user
    remove_completions_user

    if [ -d "$HOME/.local/bin" ]; then
        if [ ! -f "$HOME/.local/bin/alps" ] && [ ! -f "$HOME/.local/bin/alps-pm" ]; then
            if [ -z "$(ls -A "$HOME/.local/bin" 2>/dev/null)" ]; then
                cleanup_path "$HOME/.local/bin"
            fi
        fi
    fi

    if [ "$PURGE" -eq 1 ]; then
        remove_config_cache_system
        remove_config_cache_user
    fi
}

uninstall_macos() {
    info "Uninstalling ALPS from macOS..."

    remove_binaries_macos
    remove_completions_macos
    remove_binaries_user
    remove_completions_user

    if [ "$PURGE" -eq 1 ]; then
        remove_config_cache_macos
        remove_config_cache_user
    fi
}

main() {
    PURGE=0
    for arg in "$@"; do
        case "$arg" in
            --purge|-p) PURGE=1 ;;
            *) warn "Unknown argument: $arg" ;;
        esac
    done

    printf "\n  ${BOLD}ALPS Uninstaller${RST}\n\n"

    if ! command -v curl >/dev/null 2>&1 && ! command -v wget >/dev/null 2>&1; then
        warn "Neither curl nor wget is available. Some cleanup features may be limited."
    fi

    if is_termux; then
        SUDO=""
        UNINSTALL_MODE="termux"
        info "Environment: Termux (Android)"
        uninstall_termux
    elif is_macos; then
        detect_privileges
        info "Environment: macOS"
        uninstall_macos
    else
        detect_privileges

        if is_arch_based; then
            info "Environment: Arch Linux"
            uninstall_package_manager
        elif is_alpine; then
            info "Environment: Alpine Linux"
            uninstall_package_manager
        elif is_debian_based; then
            info "Environment: Debian/Ubuntu"
            uninstall_package_manager
        else
            info "Environment: Linux (generic)"
            uninstall_standard
        fi
    fi

    info "Final cleanup..."
    remove_symlink "/usr/local/bin/alps"
    remove_symlink "/usr/bin/alps"
    remove_symlink "$HOME/.local/bin/alps"

    printf "\n"
    ok "Uninstall completed successfully."
    printf "\n"
}

if [ "${0##*/}" = "uninstall.sh" ] || [ "$0" = "sh" ] || [ "$0" = "/bin/sh" ]; then
    main "$@"
fi
