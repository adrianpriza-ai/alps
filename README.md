<div align="center">
  <img src="https://adrianpriza-ai.github.io/alps/alps.png" alt="ALPS" style="width:100%;max-width:800px"/>

  # ALPS
  **Advanced Linux Package System**

  *The customizable package manager frontend (Linux, macOS, Termux, WSL)*

  [![Release](https://img.shields.io/github/v/release/adrianpriza-ai/alps?include_prereleases&style=flat&color=red)](https://github.com/adrianpriza-ai/alps/releases)
  [![License](https://img.shields.io/badge/License-MIT-green?style=flat)](LICENSE)
  [![Go](https://img.shields.io/badge/Go-1.25.13+-00ADD8?style=flat&logo=go)](https://go.dev)
  [![Build](https://github.com/adrianpriza-ai/alps/actions/workflows/build.yml/badge.svg)](https://github.com/adrianpriza-ai/alps/actions/workflows/build.yml)
  
</div>

---

ALPS is a Go-based frontend for `apt`, `apt-get`, `dnf`, `pacman`, `zypper`, and `apk`. It also handles AUR, Snap, Flatpak, and Winget, plus a custom script repo called alps-more. One command interface across distros — Linux, macOS, Termux on Android, WSL on Windows.

## Features

| Feature | Description |
|-|-|
| **Multi-distro** | Auto-detects `apt`, `apt-get`, `dnf`, `pacman`, `zypper`, or `apk` |
| **Termux support** | Full support on Android — no sudo, native `$PREFIX` paths |
| **macOS support** | Full support with Homebrew integration; macOS-specific paths and behaviors |
| **WSL support** | Works on WSL; alps-more entries can target `os = wsl` |
| **Built-in AUR** | Full recursive dep resolution, PKGBUILD review, yay/paru fallback |
| **Extra packages** | Snap, Flatpak, and Winget — same command shape across all three |
| **alps-more** | Cross-distro script repo with version tracking, mirror failover, and remote installs from GitHub/GitLab |
| **Security** | HTTPS-only downloads, SHA-256 verification, signed APT repositories, response size limits |
| **Customizable** | Colors, symbols, header, aliases — all via config file |
| **Completion** | fish, bash, zsh — distro-aware, AUR name cache, live package completion |
| **Build isolation** | Per-package build directories (`~/.cache/alps/more/<pkg>/`) |

## Installation

### One-line install

#### Curl

```bash
curl -fsSL https://alps-project.pages.dev/install.sh | sh
```

#### Wget

```bash
wget -qO- https://alps-project.pages.dev/install.sh | sh
```

Auto-detects Termux, Debian/Ubuntu (.deb), Arch Linux (PKGBUILD), Alpine Linux (APKBUILD) or generic Linux (binary). Safe to re-run for upgrades.

### From source

```bash
git clone https://github.com/adrianpriza-ai/alps
cd alps && make install
```

Requires Go 1.25.13+

### Pre-built binaries

Download `alps-linux-amd64`, `alps-linux-arm64`, or `alps-linux-armv7` from [Releases](https://github.com/adrianpriza-ai/alps/releases).

### Shell Completion

#### Fish
```fish
mkdir -p ~/.config/fish/completions
alps completion fish > ~/.config/fish/completions/alps.fish

```

#### Bash

```bash
# Linux
sudo mkdir -p /usr/share/bash-completion/completions
alps completion bash | sudo tee /usr/share/bash-completion/completions/alps > /dev/null

# macOS
mkdir -p $(brew --prefix)/etc/bash_completion.d
alps completion bash > $(brew --prefix)/etc/bash_completion.d/alps

```

#### Zsh

```zsh
# Linux
alps completion zsh | sudo tee "${fpath[1]}/_alps" > /dev/null

# macOS
mkdir -p $(brew --prefix)/share/zsh/site-functions
alps completion zsh > $(brew --prefix)/share/zsh/site-functions/_alps

```

> **Note:** For Zsh, run `autoload -U compinit && compinit` in your shell (or add it to `~/.zshrc`) to enable completions.

Completion is environment-aware — AUR subcommands only appear on Arch, snap only on Debian/Ubuntu, neither on Termux. Tab-completing `alps aur install` draws from a local AUR name cache populated by every search. `alps repo install [TAB]` completes live from the alps-more cache.

## Global Flags

| Flag | Description | Supported Subsystems |
|-|-|-|
| `-n, --dry-run` | Preview actions without making changes | All (repo, aur, extra, apt, pacman, dnf, zypper, apk) |
| `-y, --noconfirm` | Skip confirmation prompts | Main package managers only (apt, pacman, dnf, zypper, apk) |

**Important**: For safety, `-y` is **intentionally NOT supported** for secondary package managers (`aur`, `repo`, `extra`) and fallback paths. These operations always require explicit user confirmation.

## Usage

```
alps <command> [args]
```

| Command | Description |
|-|-|
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
| `snap/flatpak/winget <subcommand>` | Manage that package manager directly |
| `completion <shell>` | Generate shell completion |
| `config-show` | Show active config and paths |
| `aliases` | Show active aliases |
| `version` | Show version |
| `help` | Show help |

Unknown commands produce a clear error instead of being passed silently to the backend.

## Default Aliases

**Top-level:**

| Alias | Command | Alias | Command |
|-|-|-|-|
| `ins` | install | `se` | search |
| `rm` | remove | `sh` | show |
| `pu` | purge | `ls` | list |
| `up` | update | `au` | autoremove |
| `ug` | upgrade | `ac` | autoclean |
| `fug` | full-upgrade | `cl` | clean |
| `fp` | flatpak | `sk` | snap |
| `wg` | winget |  |  |

**Subcommand-only** (work inside any subsystem that has a matching subcommand):

| Alias | Command | Context |
|-|-|-|
| `bl` | build-local | aur only |
| `fa` | fetch-abs | aur only |
| `abs` | fetch-abs | aur only |

```bash
alps fp ins firefox       # flatpak install firefox
alps sk ins firefox       # snap install firefox
alps aur se neovim-git    # aur search neovim-git
alps repo ins ollama      # alps-more install ollama
alps aur bl ./mypkg       # aur build-local ./mypkg
```

**Arch Linux Specific**: Run `alps aliases` on Arch to see specialized AUR subcommand aliases.

## Configuration

| Path | Scope |
|-|-|
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
sym_ok   = "✓"
sym_err  = "✗"
sym_warn = "⚠"
sym_info = "◆"
sym_arrow = "→"
sym_bullet = "•"

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

`alps install <pkg>` on Arch tries `pacman -S` first, then falls through to AUR automatically. Uses `yay` or `paru` if available (paru preferred), otherwise the built-in makepkg path.

### Requirements

```bash
sudo pacman -S --needed git base-devel
```

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

## Extra Packages (Snap, Flatpak, Winget)

```bash
# Manage each package manager directly — no auto-detection
alps snap install <pkg>     # Snap (Ubuntu/Debian)
alps flatpak install <pkg>  # Flatpak
alps winget install <pkg>   # Winget (WSL)
```

Each backend supports `install`, `remove`, `purge`, `search`, `show`, `list`, `update`, and `upgrade`; snap and flatpak also support `autoremove`/`clean` where the manager allows it.

Snap is available on Debian/Ubuntu and auto-offered as a fallback when apt can't find a package. Winget is available on WSL for Windows package management.

## alps-more

Cross-distro script repo for tools

### Requirements

**Common requirements (all platforms):**
- **bash** – For running installation scripts.
- **tar & unzip** – For extracting archives (.tar.gz, .tar.xz, .tar.bz2, .zip).
- **GNU Coreutils** – Standard file utilities (mkdir, cp, chmod, gzip, ln).

**Linux-specific:**
- **fakeroot** – Handles sandboxed file ownership (not needed on macOS/Termux).
- **systemctl** – Manages systemd service macros (not needed on macOS/Termux).
- **useradd / userdel** – Handles user account management macros (not needed on macOS/Termux).

**macOS:** Most requirements are already included with macOS. Additional tools can be installed via Homebrew if needed.

**Install dependencies:**

Linux:
```bash
alps install fakeroot coreutils tar unzip bash
```

### Quick User Guide

```bash
alps repo update                          # refresh cache from fastest mirror
alps repo list                            # list available packages for your distro
alps repo list install                    # list installed packages (alps-more + remote)
alps repo list remove                     # list stale packages no longer in repo
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

### Runtime Details & Requirements

- Build dir: `~/.cache/alps/more/<package>/`
- State file: `/var/lib/alps/installed.json` (Termux: `$PREFIX/var/lib/alps/installed.json`, macOS: `~/Library/Application Support/alps/installed.json`)
- Repo cache: `/var/cache/alps/more/main.txt` ((Termux: `$PREFIX/var/cache/alps/more/main.txt macOS: `~/Library/Caches/alps/more/main.txt`)
- Mirrors: [GitHub Pages](https://github.com/adrianpiza-ai/alps-more) and [Codeberg](https://codeberg.org/moreland/alps-more)

### Key Features

- Simple placeholders: `{ARCH}`, `{OS}`, `{DISTRO}`, `{VERSION}`, `{PKG_DIR}`, `{SERVER}`
- Practical macros: `{DOWNLOAD}`, `{BASH_RUN}` (no external curl)
- Mirror failover and server reachability checks
- Optional sudo handling (skipped on Termux)

### Validation & Safety

- Platform checks prevent mismatched installs
- `deps` validated via `exec.LookPath`
- Install preview + explicit confirmation required (no `-y` for repo installs)
- **Security-hardened downloads:**
  - HTTPS-only requirement for all remote content
  - SHA-256 digest verification for all script downloads
  - Response size limits (10MB for manifests, 100MB for scripts)
  - Signed APT repository with GPG key verification
  - Atomic cache writes to prevent partial corruption
  - Explicit host whitelist (no broad suffix matching)
  - Display of generated scripts before execution

### Full Authoring Guide

Complete documentation, including all package fields, command blocks, macros, placeholders, examples, and publishing instructions are in **[ALPSMORE.md](ALPSMORE.md)**.

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md).

## License

MIT
