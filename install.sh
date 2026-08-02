#!/bin/sh
# Usage: curl -fsSL https://alps-project.pages.dev/install.sh | sh

set -e

REPO="adrianpriza-ai/alps"
SITE_URL="https://alps-project.pages.dev"
BASE_URL="https://github.com/$REPO/releases/latest/download"
API_URL="https://api.github.com/repos/$REPO/releases/latest"

UNAME=$(uname -s)
MACOS=0
LINUX=0
case "$UNAME" in
    Darwin) MACOS=1 ;;
    Linux) LINUX=1 ;;
esac

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

verify_checksum() {
    file_path="$1"
    file_name="$2"
    sha_tmp=""
    sha_tmp="$(mktemp "${TMPDIR:-/tmp}/alps_sha_XXXXXX")"
    
    info "Verifying checksum..."
    if ! download_file "$BASE_URL/SHA256SUMS" "$sha_tmp" 2>/dev/null; then
        warn "Could not download SHA256SUMS for verification. Skipping checksum check."
        rm -f "$sha_tmp"
        return 0
    fi
    
    expected_sha=$(awk -v name="$file_name" '$2 == name { print $1; exit }' "$sha_tmp")
    
    if [ -z "$expected_sha" ]; then
        warn "Checksum for $file_name not found in SHA256SUMS. Skipping check."
        rm -f "$sha_tmp"
        return 0
    fi
    
    if command -v sha256sum >/dev/null 2>&1; then
        actual_sha=$(sha256sum "$file_path" | cut -d' ' -f1)
    elif command -v shasum >/dev/null 2>&1; then
        actual_sha=$(shasum -a 256 "$file_path" | cut -d' ' -f1)
    else
        warn "Neither sha256sum nor shasum is installed. Skipping checksum verification."
        rm -f "$sha_tmp"
        return 0
    fi
    
    rm -f "$sha_tmp"
    
    if [ "$actual_sha" != "$expected_sha" ]; then
        die "Checksum verification failed! The downloaded file may be corrupted or tampered with."
    fi
    ok "Checksum verified successfully."
}

is_termux() {
    [ -n "$TERMUX_VERSION" ] || [ "$PREFIX" = "/data/data/com.termux/files/usr" ]
}

detect_privileges() {
    if [ "$(id -u)" -eq 0 ]; then
        INSTALL_MODE="system"
        SUDO=""
        BIN_DIR="/usr/local/bin"
        info "Running as root. Will install system-wide."
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
            INSTALL_MODE="system"
            BIN_DIR="/usr/local/bin"
            info "$SUDO privileges detected. Will install system-wide."
        else
            INSTALL_MODE="user"
            BIN_DIR="$HOME/.local/bin"
            if command -v sudo >/dev/null 2>&1 || command -v doas >/dev/null 2>&1; then
                info "sudo/doas found but not usable (non-interactive or no passwordless access). Will install to user directory ($BIN_DIR)."
            else
                info "No sudo/root access available. Will install to user directory ($BIN_DIR)."
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

detect_arch() {
    case "$(uname -m)" in
        x86_64)        echo "amd64" ;;
        aarch64|arm64) echo "arm64" ;;
        armv7l|armv7*) echo "armv7" ;;
        *) die "Unsupported architecture: $(uname -m)" ;;
    esac
}

get_latest_version() {
    ver=""
    ver=$(http_get "$API_URL" 2>/dev/null | grep '"tag_name"' | head -1 | sed 's/.*"tag_name": *"\([^"]*\)".*/\1/')
    if [ -z "$ver" ]; then
        ver=$(http_get "https://github.com/$REPO/releases/latest" 2>/dev/null | grep -o 'releases/tag/[^"]*' | head -1 | sed 's|.*/||')
    fi
    echo "$ver"
}

get_installed_version() {
    command -v alps >/dev/null 2>&1 || return 0
    alps version 2>/dev/null | grep -o 'v[0-9][^ ]*' | head -1
}

setup_path() {
    bin_dir="$1"
    bin_dir="${bin_dir%/}"
    
    case ":$PATH:" in
        *:"$bin_dir":*) return 0 ;;
    esac
    
    warn "$bin_dir is not in your PATH."
    
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
        if grep -F "$bin_dir" "$shell_profile" >/dev/null 2>&1; then
            ok "$bin_dir is already configured in $shell_profile"
            return 0
        fi
        info "Adding $bin_dir to PATH in $shell_profile..."
        if [ "$shell_name" = "fish" ]; then
            printf "\n# Added by ALPS installer\nfish_add_path %s\n" "$bin_dir" >> "$shell_profile"
        else
            printf "\n# Added by ALPS installer\nexport PATH=\"\$PATH:%s\"\n" "$bin_dir" >> "$shell_profile"
        fi
        ok "PATH updated. Please restart your terminal or run: source $shell_profile"
    else
        warn "Please add the following line to your shell configuration file:"
        printf "    export PATH=\"\$PATH:%s\"\n" "$bin_dir"
    fi
}

setup_completions() {
    alps_bin="$1"
    info "Setting up shell completions..."
    
    if [ "$INSTALL_MODE" = "system" ] && ! is_termux; then
        if [ "$MACOS" -eq 1 ]; then
            if command -v fish >/dev/null 2>&1; then
                if $SUDO mkdir -p "$HOME/.config/fish/completions" 2>/dev/null; then
                    "$alps_bin" completion fish 2>/dev/null | $SUDO tee "$HOME/.config/fish/completions/alps-pm.fish" >/dev/null
                    $SUDO ln -sf alps-pm.fish "$HOME/.config/fish/completions/alps.fish" >/dev/null 2>&1
                    ok "Fish completion installed for macOS"
                fi
            fi
            if command -v bash >/dev/null 2>&1; then
                bash_dir="$HOME/.bash_completion.d"
                if $SUDO mkdir -p "$bash_dir" 2>/dev/null; then
                    "$alps_bin" completion bash 2>/dev/null | $SUDO tee "$bash_dir/alps-pm" >/dev/null
                    $SUDO ln -sf alps-pm "$bash_dir/alps" >/dev/null 2>&1
                    ok "Bash completion installed for macOS"
                fi
            fi
            if command -v zsh >/dev/null 2>&1; then
                if $SUDO mkdir -p /usr/local/share/zsh/site-functions 2>/dev/null; then
                    "$alps_bin" completion zsh 2>/dev/null | $SUDO tee /usr/local/share/zsh/site-functions/_alps-pm >/dev/null
                    $SUDO ln -sf _alps-pm /usr/local/share/zsh/site-functions/_alps >/dev/null 2>&1
                    ok "Zsh completion installed for macOS"
                fi
            fi
        else
            if command -v fish >/dev/null 2>&1; then
                if $SUDO mkdir -p /usr/share/fish/vendor_completions.d 2>/dev/null; then
                    "$alps_bin" completion fish 2>/dev/null | $SUDO tee /usr/share/fish/vendor_completions.d/alps-pm.fish >/dev/null
                    $SUDO ln -sf alps-pm.fish /usr/share/fish/vendor_completions.d/alps.fish >/dev/null 2>&1
                    ok "Fish completion installed system-wide"
                fi
            fi
            if $SUDO mkdir -p /usr/share/bash-completion/completions 2>/dev/null; then
                "$alps_bin" completion bash 2>/dev/null | $SUDO tee /usr/share/bash-completion/completions/alps-pm >/dev/null
                $SUDO ln -sf alps-pm /usr/share/bash-completion/completions/alps >/dev/null 2>&1
                ok "Bash completion installed system-wide"
            fi
            if command -v zsh >/dev/null 2>&1; then
                if $SUDO mkdir -p /usr/share/zsh/site-functions 2>/dev/null; then
                    "$alps_bin" completion zsh 2>/dev/null | $SUDO tee /usr/share/zsh/site-functions/_alps-pm >/dev/null
                    $SUDO ln -sf _alps-pm /usr/share/zsh/site-functions/_alps >/dev/null 2>&1
                    ok "Zsh completion installed system-wide"
                fi
            fi
        fi
    elif is_termux; then
        if command -v fish >/dev/null 2>&1; then
            mkdir -p "$PREFIX/share/fish/vendor_completions.d"
            "$alps_bin" completion fish > "$PREFIX/share/fish/vendor_completions.d/alps-pm.fish" 2>/dev/null
            ln -sf alps-pm.fish "$PREFIX/share/fish/vendor_completions.d/alps.fish" 2>/dev/null
            ok "Fish completion installed for Termux"
        fi
        if command -v bash >/dev/null 2>&1; then
            mkdir -p "$PREFIX/share/bash-completion/completions"
            "$alps_bin" completion bash > "$PREFIX/share/bash-completion/completions/alps-pm" 2>/dev/null
            ln -sf alps-pm "$PREFIX/share/bash-completion/completions/alps" 2>/dev/null
            ok "Bash completion installed for Termux"
        fi
        if command -v zsh >/dev/null 2>&1; then
            mkdir -p "$PREFIX/share/zsh/site-functions"
            "$alps_bin" completion zsh > "$PREFIX/share/zsh/site-functions/_alps-pm" 2>/dev/null
            ln -sf _alps-pm "$PREFIX/share/zsh/site-functions/_alps" 2>/dev/null
            ok "Zsh completion installed for Termux"
        fi
    else
        if command -v fish >/dev/null 2>&1; then
            fish_dir="$HOME/.config/fish/completions"
            mkdir -p "$fish_dir"
            "$alps_bin" completion fish > "$fish_dir/alps-pm.fish" 2>/dev/null
            ln -sf alps-pm.fish "$fish_dir/alps.fish" 2>/dev/null
            ok "Fish completion installed locally in $fish_dir"
        fi
        
        if [ "$MACOS" -eq 1 ]; then
            bash_dir="$HOME/.bash_completion.d"
        else
            bash_dir="$HOME/.local/share/bash-completion/completions"
        fi
        mkdir -p "$bash_dir"
        if "$alps_bin" completion bash > "$bash_dir/alps-pm" 2>/dev/null; then
            ln -sf alps-pm "$bash_dir/alps" 2>/dev/null
            ok "Bash completion installed locally in $bash_dir"
        fi
        
        if command -v zsh >/dev/null 2>&1; then
            if [ "$MACOS" -eq 1 ]; then
                zsh_dir="/usr/local/share/zsh/site-functions"
            else
                zsh_dir="$HOME/.zsh/completion"
            fi
            mkdir -p "$zsh_dir"
            if "$alps_bin" completion zsh > "$zsh_dir/_alps-pm" 2>/dev/null; then
                ln -sf _alps-pm "$zsh_dir/_alps" 2>/dev/null
                ok "Zsh completion installed locally in $zsh_dir"
            fi
        fi
    fi
}

install_termux() {
    arch=""
    termux_arch=""
    bin_url=""
    dest=""
    tmp=""
    dest_pm=""

    if [ -z "$PREFIX" ]; then
        die "Termux detected but \$PREFIX is not set."
    fi

    arch=$(detect_arch)
    
    case "$arch" in
        amd64) termux_arch="x86_64" ;;
        arm64) termux_arch="aarch64" ;;
        armv7) termux_arch="arm" ;;
        *) die "Unsupported Termux architecture: $arch" ;;
    esac

    bin_url="$BASE_URL/alps-termux-$termux_arch"
    dest="$PREFIX/bin/alps"
    dest_pm="$PREFIX/bin/alps-pm"
    tmp="$(mktemp "${TMPDIR:-/tmp}/alps_XXXXXX")"

    info "Downloading alps $LATEST ($termux_arch) for Termux..."
    download_file "$bin_url" "$tmp" || die "Download failed"
    verify_checksum "$tmp" "alps-termux-$termux_arch"
    chmod +x "$tmp"
    rm -f "$dest_pm" "$dest" 2>/dev/null || true
    mv "$tmp" "$dest_pm"
    ln -sf alps-pm "$dest"
    ok "Installed to $dest_pm and symlinked $dest"
    setup_completions "$dest_pm"
}

install_arch_pacman() {
    info "Arch-based system detected. Attempting installation via makepkg/pacman..."
    
    run_user=""
    if [ "$(id -u)" -eq 0 ]; then
        if [ -n "$SUDO_USER" ]; then
            run_user="$SUDO_USER"
        else
            warn "Running as pure root; makepkg cannot run as root. Falling back to direct binary install."
            return 1
        fi
    fi
    
    tmp_dir=$(mktemp -d "${TMPDIR:-/tmp}/alps-pkg-XXXXXX")
    
    if [ "$(id -u)" -eq 0 ] && [ -n "$run_user" ]; then
        chown -R "$run_user" "$tmp_dir"
    fi
    
    pkgbuild_path="$tmp_dir/PKGBUILD"
    
    if [ -f "PKGBUILD" ]; then
        info "Using local PKGBUILD..."
        cp "PKGBUILD" "$pkgbuild_path"
    else
        info "Downloading PKGBUILD..."
        if ! download_file "$SITE_URL/PKGBUILD" "$pkgbuild_path" 2>/dev/null && \
           ! download_file "https://raw.githubusercontent.com/$REPO/main/PKGBUILD" "$pkgbuild_path" 2>/dev/null && \
           ! download_file "https://raw.githubusercontent.com/$REPO/master/PKGBUILD" "$pkgbuild_path" 2>/dev/null; then
            warn "Could not download PKGBUILD. Falling back to binary installation."
            rm -rf "$tmp_dir"
            return 1
        fi
    fi

    ver_clean="${LATEST#v}"
    sed -i "s/^pkgver=.*/pkgver=$ver_clean/" "$pkgbuild_path"

    if [ "$(id -u)" -eq 0 ] && [ -n "$run_user" ]; then
        chown "$run_user:$run_user" "$pkgbuild_path"
    fi

    build_success=0
    info "Running makepkg to build and install alps-pm package..."
    
    if [ "$(id -u)" -eq 0 ] && [ -n "$run_user" ]; then
        if su "$run_user" -c "cd '$tmp_dir' && makepkg -si --noconfirm"; then
            build_success=1
        fi
    else
        if (cd "$tmp_dir" && makepkg -si --noconfirm); then
            build_success=1
        fi
    fi
    
    rm -rf "$tmp_dir"
    
    if [ "$build_success" -eq 1 ]; then
        ok "Successfully installed alps-pm package via pacman!"
        return 0
    else
        warn "makepkg/pacman installation failed. Falling back to binary installation."
        return 1
    fi
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

is_macos() {
    [ "$MACOS" -eq 1 ]
}

install_alpine_apk() {
    info "Alpine Linux detected. Attempting installation via abuild/apk..."

    if [ ! -f "$HOME/.abuild/abuild-key.rsa" ]; then
        info "Configuring abuild keys..."
        if [ "$(id -u)" -eq 0 ]; then
            abuild-keygen -i -a -n
        else
            $SUDO abuild-keygen -i -a -n
        fi
    fi

    tmp_dir=$(mktemp -d "${TMPDIR:-/tmp}/alps-apk-XXXXXX")
    
    run_user=""
    if [ "$(id -u)" -eq 0 ]; then
        if [ -n "$SUDO_USER" ]; then
            run_user="$SUDO_USER"
        fi
    fi

    if [ "$(id -u)" -eq 0 ] && [ -n "$run_user" ]; then
        chown -R "$run_user" "$tmp_dir"
    fi

    apkbuild_path="$tmp_dir/APKBUILD"

    if [ -f "APKBUILD" ]; then
        info "Using local APKBUILD..."
        cp "APKBUILD" "$apkbuild_path"
    else
        info "Downloading APKBUILD..."
        if ! download_file "$SITE_URL/APKBUILD" "$apkbuild_path" 2>/dev/null && \
           ! download_file "https://raw.githubusercontent.com/$REPO/main/APKBUILD" "$apkbuild_path" 2>/dev/null && \
           ! download_file "https://raw.githubusercontent.com/$REPO/master/APKBUILD" "$apkbuild_path" 2>/dev/null; then
            warn "Could not download APKBUILD. Falling back to binary installation."
            rm -rf "$tmp_dir"
            return 1
        fi
    fi

    ver_clean="${LATEST#v}"
    sed -i "s/^pkgver=.*/pkgver=$ver_clean/" "$apkbuild_path"

    if [ "$(id -u)" -eq 0 ] && [ -n "$run_user" ]; then
        chown "$run_user:$run_user" "$apkbuild_path"
    fi

    build_success=0
    info "Running abuild to build and install alps-pm..."
    
    if [ "$(id -u)" -eq 0 ] && [ -n "$run_user" ]; then
        su "$run_user" -c "cd '$tmp_dir' && abuild checksum"
    else
        (cd "$tmp_dir" && abuild checksum)
    fi

    if [ "$(id -u)" -eq 0 ]; then
        if [ -z "$run_user" ]; then
            if (cd "$tmp_dir" && abuild -F -i -r); then
                build_success=1
            fi
        else
            if su "$run_user" -c "cd '$tmp_dir' && abuild -i -r"; then
                build_success=1
            fi
        fi
    else
        if (cd "$tmp_dir" && abuild -i -r); then
            build_success=1
        fi
    fi

    rm -rf "$tmp_dir"

    if [ "$build_success" -eq 1 ]; then
        ok "Successfully installed alps-pm package via apk!"
        return 0
    else
        warn "abuild/apk installation failed. Falling back to binary installation."
        return 1
    fi
}

install_debian() {
    if [ "$INSTALL_MODE" = "user" ]; then
        warn "No sudo/root access available on Debian/Ubuntu system. Falling back to user-local binary installation..."
        install_linux
        return 0
    fi

    arch=""
    deb_arch=""
    deb_url=""
    tmp_deb=""
    ver=""
    arch=$(detect_arch)

    case "$arch" in
        amd64) deb_arch="amd64" ;;
        arm64) deb_arch="arm64" ;;
        armv7) deb_arch="armhf" ;;
    esac

    ver="${LATEST#v}"
    deb_url="$BASE_URL/alps_${ver}_${deb_arch}.deb"
    tmp_deb="$(mktemp "${TMPDIR:-/tmp}/alps_XXXXXX.deb")"

    info "Trying .deb package for $arch..."
    if download_file "$deb_url" "$tmp_deb" 2>/dev/null && [ -s "$tmp_deb" ]; then
        verify_checksum "$tmp_deb" "alps_${ver}_${deb_arch}.deb"
        info "Installing .deb..."
        if $SUDO dpkg -i "$tmp_deb" || $SUDO apt-get install -f -y; then
            rm -f "$tmp_deb"
            ok "Installed via dpkg"
            $SUDO mv /usr/bin/alps /usr/bin/alps-pm 2>/dev/null || true
            $SUDO ln -sf alps-pm /usr/bin/alps
            setup_completions "/usr/bin/alps-pm"
        else
            rm -f "$tmp_deb"
            warn ".deb installation failed. Falling back to binary installation..."
            install_linux
        fi
    else
        rm -f "$tmp_deb"
        warn ".deb not found for this release; falling back to binary"
        install_linux
    fi
}

install_linux_bin() {
    arch=""
    bin_url=""
    tmp=""
    dest_alps=""
    dest_alps_pm=""
    arch=$(detect_arch)
    bin_url="$BASE_URL/alps-linux-$arch"
    tmp="$(mktemp "${TMPDIR:-/tmp}/alps_XXXXXX")"

    info "Downloading alps $LATEST ($arch)..."
    download_file "$bin_url" "$tmp" || die "Download failed"
    verify_checksum "$tmp" "alps-linux-$arch"
    chmod +x "$tmp"

    dest_alps="$BIN_DIR/alps"
    dest_alps_pm="$BIN_DIR/alps-pm"

    if [ "$INSTALL_MODE" = "system" ]; then
        $SUDO mkdir -p "$BIN_DIR"
        info "Installing system-wide to $dest_alps_pm..."
        $SUDO rm -f "$dest_alps_pm" "$dest_alps" 2>/dev/null || true
        if $SUDO mv "$tmp" "$dest_alps_pm"; then
            $SUDO ln -sf alps-pm "$dest_alps"
            ok "Installed system-wide to $dest_alps_pm and created symlink $dest_alps"
            setup_completions "$dest_alps_pm"
        else
            rm -f "$tmp"
            warn "Failed to move binary system-wide. Attempting user-local fallback..."
            INSTALL_MODE="user"
            BIN_DIR="$HOME/.local/bin"
            install_linux_bin
        fi
    else
        mkdir -p "$BIN_DIR"
        info "Installing to user-local $dest_alps_pm..."
        rm -f "$dest_alps_pm" "$dest_alps" 2>/dev/null || true
        mv "$tmp" "$dest_alps_pm"
        ln -sf alps-pm "$dest_alps"
        ok "Installed to user-local $dest_alps_pm and created symlink $dest_alps"
        setup_path "$BIN_DIR"
        setup_completions "$dest_alps_pm"
    fi
}

install_linux() {
    install_linux_bin
}

install_macos_bin() {
    arch=""
    bin_url=""
    tmp=""
    dest_alps=""
    dest_alps_pm=""
    arch=$(detect_arch)
    
    case "$arch" in
        amd64) macos_arch="amd64" ;;
        arm64) macos_arch="arm64" ;;
        *) die "Unsupported macOS architecture: $arch" ;;
    esac
    
    bin_url="$BASE_URL/alps-darwin-$macos_arch"
    tmp="$(mktemp "${TMPDIR:-/tmp}/alps_XXXXXX")"

    info "Downloading alps $LATEST ($macos_arch) for macOS..."
    download_file "$bin_url" "$tmp" || die "Download failed"
    verify_checksum "$tmp" "alps-darwin-$macos_arch"
    chmod +x "$tmp"

    dest_alps="$BIN_DIR/alps"
    dest_alps_pm="$BIN_DIR/alps-pm"

    if [ "$INSTALL_MODE" = "system" ]; then
        $SUDO mkdir -p "$BIN_DIR"
        info "Installing system-wide to $dest_alps_pm..."
        $SUDO rm -f "$dest_alps_pm" "$dest_alps" 2>/dev/null || true
        if $SUDO mv "$tmp" "$dest_alps_pm"; then
            $SUDO ln -sf alps-pm "$dest_alps"
            ok "Installed system-wide to $dest_alps_pm and created symlink $dest_alps"
            setup_completions "$dest_alps_pm"
        else
            rm -f "$tmp"
            warn "Failed to move binary system-wide. Attempting user-local fallback..."
            INSTALL_MODE="user"
            BIN_DIR="$HOME/.local/bin"
            install_macos_bin
        fi
    else
        mkdir -p "$BIN_DIR"
        info "Installing to user-local $dest_alps_pm..."
        rm -f "$dest_alps_pm" "$dest_alps" 2>/dev/null || true
        mv "$tmp" "$dest_alps_pm"
        ln -sf alps-pm "$dest_alps"
        ok "Installed to user-local $dest_alps_pm and created symlink $dest_alps"
        setup_path "$BIN_DIR"
        setup_completions "$dest_alps_pm"
    fi
}

main() {
    if ! command -v curl >/dev/null 2>&1 && ! command -v wget >/dev/null 2>&1; then
        die "Either 'curl' or 'wget' is required to run this installer."
    fi

    printf "\n  ${BOLD}ALPS Installer${RST}\n\n"

    if is_termux; then
        SUDO=""
        INSTALL_MODE="termux"
        info "Environment: Termux (Android)"
        info "Installing to \$PREFIX/bin; no sudo required."
    elif is_macos; then
        detect_privileges
        info "Environment: macOS"
    else
        detect_privileges
    fi

    LATEST=$(get_latest_version)
    [ -n "$LATEST" ] || die "Could not fetch latest version from GitHub"

    INSTALLED=$(get_installed_version)

    if [ -n "$INSTALLED" ]; then
        if [ "$INSTALLED" = "$LATEST" ]; then
            ok "alps $INSTALLED is already up to date"
            printf "\n"
            exit 0
        fi
        info "Upgrading alps $INSTALLED to $LATEST"
    else
        info "Installing alps $LATEST"
    fi

    if is_termux; then
        install_termux
    elif is_macos; then
        install_macos_bin
    elif is_arch_based; then
        info "Environment: Arch Linux"
        install_arch_pacman || install_linux
    elif is_alpine; then
        info "Environment: Alpine Linux"
        install_alpine_apk || install_linux
    elif is_debian_based; then
        info "Environment: Debian/Ubuntu"
        install_debian
    else
        info "Environment: Linux"
        install_linux
    fi

    printf "\n"
    ok "Done! Run 'alps help' or 'alps-pm help' to get started."
    printf "\n"
}

main
