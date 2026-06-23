<div align="center">
  <img src="https://adrianpriza-ai.github.io/alps/alps.png" alt="ALPS" style="width:100%;max-width:800px"/>

  # ALPS
  **Advanced Linux Package System**

  *The customizable package manager frontend*

  [![Release](https://img.shields.io/github/v/release/adrianpriza-ai/alps?include_prereleases&style=flat&color=red)](https://github.com/adrianpriza-ai/alps/releases)
  [![License](https://img.shields.io/badge/License-MIT-green?style=flat)](LICENSE)
  [![Go](https://img.shields.io/badge/Go-1.22+-00ADD8?style=flat&logo=go)](https://go.dev)
  [![Build](https://github.com/adrianpriza-ai/alps/actions/workflows/build.yml/badge.svg)](https://github.com/adrianpriza-ai/alps/actions/workflows/build.yml)
  [![Go Report Card](https://goreportcard.com/badge/github.com/adrianpriza-ai/alps?v=1)](https://goreportcard.com/report/github.com/adrianpriza-ai/alps)

  [![AUR](https://img.shields.io/badge/AUR-built--in-1793D1?style=flat&logo=archlinux)](https://aur.archlinux.org)
  [![alps-more](https://img.shields.io/badge/alps--more-repo-orange?style=flat)](https://github.com/adrianpriza-ai/alps-more)

</div>

---

ALPS is a Go-based frontend for `apt`, `apt-get`, `dnf`, `pacman`, `zypper`, and `apk` with built-in AUR, Flatpak, and Snap support, a custom cross-distro script repo (alps-more), fully customizable output styling, and a unified command interface across distros — including Termux on Android and WSL on Windows.

> **One tool. Every distro. Your style.**

## Features

| Feature | Description |
|---------|-------------|
| **Multi-distro** | Auto-detects `apt`, `apt-get`, `dnf`, `pacman`, `zypper`, or `apk` |
| **Termux support** | Full support on Android — no sudo, native `$PREFIX` paths |
| **WSL support** | Works on WSL; alps-more entries can target `os = wsl` |
| **Built-in AUR** | Full recursive dep resolution, PKGBUILD review, yay fallback |
| **Snap & Flatpak** | First-class subcommands; snap auto-offered on Ubuntu/Debian |
| **alps-more** | Cross-distro script repo with version tracking, mirror failover, purge support, and remote installs from GitHub/GitLab |
| **Customizable** | Colors, symbols, header, aliases — all via config file |
| **Smart completion** | fish, bash, zsh — distro-aware, AUR name cache, live package completion |
| **Build isolation** | Per-package build directories (`~/.cache/alps/more/<pkg>/`) |

## Installation

### One-line install

#### Curl

```bash
curl -fsSL https://alps-project.pages.dev/install.sh | sh
```

#### Wget

```bash
wget -qO- --show-progress=0 https://alps-project.pages.dev/install.sh | sh
```

Auto-detects Termux, Debian/Ubuntu (.deb), Arch Linux (PKGBUILD), Alpine Linux (APKBUILD) or generic Linux (binary). Handles upgrades too — safe to re-run.

### From source

```bash
git clone https://github.com/adrianpriza-ai/alps
cd alps && make install
```

Requires Go 1.22+

### Pre-built binaries

Download `alps-linux-amd64`, `alps-linux-arm64`, or `alps-linux-armv7` from [Releases](https://github.com/adrianpriza-ai/alps/releases).

### Shell completion

```bash
# Fish
alps completion fish > ~/.config/fish/completions/alps.fish

# Bash
sudo sh -c 'alps completion bash > /usr/share/bash-completion/completions/alps'

# Zsh
alps completion zsh > "${fpath[1]}/_alps" && autoload -U compinit && compinit
```

Completion is environment-aware — AUR subcommands only appear on Arch, snap only on Debian/Ubuntu, neither on Termux. Tab-completing `alps aur install` draws from a local AUR name cache populated by every search. `alps repo install [TAB]` completes live from the alps-more cache.

## Global Flags

| Flag | Description | Supported Subsystems |
|------|-------------|---------------------|
| `-n, --dry-run` | Preview actions without making changes | All (repo, aur, flatpak, snap, apt, pacman, dnf, zypper, apk) |
| `-y, --noconfirm` | Skip confirmation prompts | Main package managers only (apt, pacman, dnf, zypper, apk) |

**Important**: For safety, `-y` is **intentionally NOT supported** for secondary package managers (`aur`, `repo`, `flatpak`, `snap`) and fallback paths. These operations always require explicit user confirmation.

## Usage

```
alps <command> [args]
```

| Command | Description |
|---|---|
| `install <pkg>` | Install from repo or fallback (auto-detected) |
| `remove <pkg>` | Remove package |
| `purge <pkg>` | Remove package and config/data |
| `search <query>` | Search repo + AUR simultaneously (AUR only for Arch Linux) |
| `show <pkg>` | Show package information |
| `list` | List installed packages |
| `update` | Update package database |
| `upgrade` | Upgrade system + AUR packages |
| `full-upgrade` | Sync repos and upgrade all packages |
| `autoremove` | Remove orphaned dependencies |
| `autoclean` | Clean package cache |
| `clean` | Clean all cached packages |
| `repo <subcommand>` | Manage alps-more packages |
| `aur <subcommand>` | AUR management (Arch only) |
| `flatpak <subcommand>` | Flatpak management |
| `snap <subcommand>` | Snap management (Ubuntu/Debian) |
| `completion <shell>` | Generate shell completion |
| `config-show` | Show active config and paths |
| `aliases` | Show active aliases |
| `version` | Show version |
| `help` | Show help |

Unknown commands produce a clear error instead of being passed silently to the backend.

## Command Resolution

Every command goes through a **3-tier check** before anything runs:

1. **Hard command** — built-in name (`install`, `repo`, `aur`, `flatpak`, ...)
2. **Config alias** — defined by you in the config file (`alias_i = install`)
3. **Default short** — built-in short names (`ins`, `rm`, `se`, `fp`, ...)
4. Anything else → `unknown command "x" — run 'alps help' for available commands`

The same 3-tier system applies to subcommands inside `aur`, `repo`, `flatpak`, and `snap`.

### Default Aliases

**Top-level:**

| Alias | Command | Alias | Command |
|---|---|---|---|
| `ins` | install | `se` | search |
| `rm` | remove | `sh` | show |
| `pu` | purge | `ls` | list |
| `up` | update | `au` | autoremove |
| `ug` | upgrade | `ac` | autoclean |
| `fug` | full-upgrade | `cl` | clean |
| `fp` | flatpak | `sk` | snap |

**Subcommand-only** (work inside any subsystem that has a matching subcommand):

| Alias | Command | Context |
|---|---|---|
| `bl` | build-local | aur only |
| `fa` | fetch-abs | aur only |
| `abs` | fetch-abs | aur only |

```bash
alps fp ins firefox       # flatpak install firefox
alps aur se neovim-git    # aur search neovim-git
alps repo ins ollama      # repo install ollama
alps aur bl ./mypkg       # aur build-local ./mypkg
```

**Arch Linux Specific**: Run `alps aliases` on Arch to see specialized AUR subcommand aliases.

## Configuration

| Path | Scope |
|---|---|
| `/etc/alps/config` | Global |
| `~/.config/alps/config` | Per-user (overrides global) |

**Config file format (INI-style):**

```ini
# Colors
color_primary = "\e[36m"    # cyan
color_success = "\e[32m"    # green
color_warning = "\e[33m"    # yellow
color_error   = "\e[31m"    # red
color_info    = "\e[34m"    # blue
color_dim     = "\e[2;37m"  # dim white
color_bold    = "\e[1m"     # bold
color_reset   = "\e[0m"     # reset

# Symbols
sym_ok   = "✓"    sym_err  = "✗"
sym_warn = "⚠"    sym_info = "◆"
sym_arrow = "→"   sym_bullet = "•"

# Header
show_header  = true
title_style  = "default"   # or "custom"
title_line1  = "\e[1;97m  ╔═════════════╗"
title_line2  = "\e[1;97m  ║ ALPS  /\/\/ ║"
title_line3  = "\e[1;97m  ╚═════════════╝"

# Aliases
alias_i   = "install"
alias_-S  = "install"
alias_-R  = "remove"
```

## AUR (Arch Linux)

`alps install <pkg>` on Arch tries `pacman -S` first, then falls through to AUR automatically. Uses `yay` if available, otherwise the built-in makepkg path.

### Built-in AUR features

- **Full recursive dep resolution** — AUR deps are resolved and built in topological order
- **PKGBUILD review** — All PKGBUILDs shown up-front for review before any build starts
- **Single confirm prompt** — Covers the entire build plan
- **Provider selection** — Unknown dep with no exact match → presents up to 5 candidates to pick a provider
- **Accurate dep checking** — Uses `pacman -T` for virtual packages and provides handling
- **Makedep cleanup** — Removal offered once at the very end

```bash
alps aur install <pkg>          # install with full dep resolution
alps aur search <query>         # multi-word narrowing search
alps aur list                   # list installed AUR packages
alps aur remove <pkg>           # remove via pacman -R
alps aur clean                  # remove build cache (~/.cache/alps/aur/)
alps aur build-local [dir]      # build from a local PKGBUILD directory
alps aur fetch-abs <pkg>        # fetch official PKGBUILD (asp or Arch GitLab)
```

**Requirements**: `git` and `base-devel`. `sudo` is required — `su` fallback is not supported for AUR operations.

## Flatpak & Snap

```bash
# Flatpak
alps flatpak install <pkg>
alps flatpak search <query>
alps flatpak update
alps flatpak remove <pkg>
alps flatpak list

# Snap (Ubuntu/Debian only)
alps snap install <pkg>
alps snap search <query>
alps snap update
alps snap remove <pkg>
alps snap list
```

Snap is available on Debian/Ubuntu and auto-offered as a fallback when apt can't find a package. `sudo` is required for both — `su` fallback is not supported.

## alps-more

Cross-distro script repo for tools not in standard package managers. 

### Quick User Guide

```bash
# Repo management
alps repo update                          # refresh cache from fastest mirror
alps repo list                            # list available packages for your distro
alps repo list install                    # list installed packages (alps-more + remote)
alps repo list remove                     # list stale packages no longer in repo

# Package operations
alps repo search <query>                  # search by name or description
alps repo install <pkg>                   # install with preview and validation
alps repo install github.com/user/repo    # install from a GitHub ALPSMORE file
alps repo install gitlab.com/user/repo    # install from a GitLab ALPSMORE file
alps repo install codeberg.org/user/repo  # install from a Codeberg ALPSMORE file
alps repo upgrade [pkg]                   # upgrade one or all installed packages
alps repo remove <pkg>                    # remove package
alps repo purge <pkg>                     # remove package and delete config/data
alps repo clean                           # remove build cache (~/.cache/alps/more)
```

### Runtime Details

- **Build directory**: Every package command runs inside a per-package build dir: `~/.cache/alps/more/<package>/`
- **State file**: Installed packages and snapshots stored in `installed.json`:
  - Default: `/var/lib/alps/installed.json`
  - Termux: `$PREFIX/var/lib/alps/installed.json`
- **Repo cache**: Upstream index cached at `/var/cache/alps/more/main.txt` (Termux path differs)
- **Official mirrors**: [GitHub Pages](https://github.com/adrianpiza-ai/alps-more) and [Codeberg](https://codeberg.org/moreland/alps-more)

### Key Features

- **Variable expansion**: `{ARCH}`, `{OS}`, `{DISTRO}`, `{VERSION}`, `{PKG_DIR}`, `{SERVER}` placeholders
- **Macros**:
  - `{DOWNLOAD} URL [OUTPUT_FILE]` — Downloads files using Go's HTTP client (no curl dependency)
  - `{BASH_RUN} PATH [ARGS...]` — Downloads and runs bash scripts (no curl dependency)
  - `{CURL_RUN}` — Deprecated alias of `{BASH_RUN}` (backward compatible)
- **Mirror failover**: Packages using macros resolve reachable server before running
- **Sudo handling**: `sudo = true` requests privilege escalation; Termux skips sudo

### Validation & Safety

- **Arch/OS validation**: Refuses to install if fields don't match the host
- **Dependency checking**: `deps` checked via `exec.LookPath` before install
- **Preview & confirmation**: Full install preview shown; explicit confirmation required (no `-y`)
- **Remote install priority**: Official alps-more entries win on name conflict
- **Dry-run support**: `--dry-run` (`-n`) supported for preview
- **Auto-cleanup**: Install/upgrade failures trigger automatic cleanup via `remove_begin` if defined

### Full Authoring Guide

Complete documentation, including all package fields, command blocks, macros, placeholders, examples, and publishing instructions are in **[ALPSMORE.md](ALPSMORE.md)**.

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md).

## License

MIT
