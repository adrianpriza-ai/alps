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

ALPS is a Go-based frontend for `apt`, `apt-get`, `dnf`, and `pacman` with built-in AUR, Flatpak, and Snap support, a custom cross-distro script repo (alps-more), fully customizable output styling, and a unified command interface across distros — including Termux on Android and WSL on Windows.

> **One tool. Every distro. Your style.**

## Features

| | |
|---|---|
| **Multi-distro** | Auto-detects `apt`, `apt-get`, `dnf`, `pacman`, `zypper`, or `apk` |
| **Termux support** | Full support on Android — no sudo, native `$PREFIX` paths |
| **WSL support** | Works on WSL; alps-more entries can target `os = wsl` |
| **Built-in AUR** | Full recursive dep resolution, PKGBUILD review, yay fallback |
| **Snap & Flatpak** | First-class subcommands; snap auto-offered on Ubuntu/Debian |
| **alps-more** | Cross-distro script repo with version tracking, mirror failover, purge support, and remote installs from GitHub/GitLab |
| **3-tier aliases** | Hard commands → config aliases → default shorts. Unknown commands error cleanly |
| **Fully customizable** | Colors, symbols, header, aliases — all via config file |
| **Smart completion** | fish, bash, zsh — distro-aware, AUR name cache, live package completion |

## Installation

### One-line install

```bash
curl -fsSL https://alps-project.pages.dev/install.sh | sh
```

Auto-detects Termux, Debian/Ubuntu (.deb), or generic Linux (binary). Handles upgrades too — safe to re-run.

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

## Usage

```
alps <command> [args]
```

| Command | Description |
|---|---|
| `install <pkg>` | Install from repo, AUR, or alps-more (auto-detected) |
| `search <query>` | Search repo + AUR simultaneously (multi-word narrowing) |
| `upgrade` | Upgrade system + AUR packages |
| `repo <subcommand>` | Manage alps-more packages |
| `aur <subcommand>` | AUR management (Arch only) |
| `flatpak <subcommand>` | Flatpak management |
| `snap <subcommand>` | Snap management (Ubuntu/Debian) |
| `completion <shell>` | Generate shell completion |
| `config-show` | Show active config and paths |
| `aliases` | Show active aliases |
| `version` | Show version |

Unknown commands produce a clear error instead of being passed silently to the backend.

## Command resolution

Every command goes through a 3-tier check before anything runs:

1. **Hard command** — built-in name (`install`, `repo`, `aur`, `flatpak`, …)
2. **Config alias** — defined by you in the config file (`alias_i = install`)
3. **Default short** — built-in short names (`ins`, `rm`, `se`, `fp`, …)
4. Anything else → `unknown command "x" — run 'alps help' for available commands`

The same 3-tier system applies to subcommands inside `aur`, `repo`, `flatpak`, and `snap`.

### Default aliases

**Top-level:**

| Alias | Command | Alias | Command |
|---|---|---|---|
| `ins` | install | `se` | search |
| `rm` | remove | `sh` | show |
| `pu` | purge | `ls` | list |
| `up` | update | `au` | autoremove |
| `ug` | upgrade | `ac` | autoclean |
| `fug` | full-upgrade | `cl` | clean |
| `fp` | flatpak | `sk` | snap | |

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

## Configuration

| Path | Scope |
|---|---|
| `/etc/alps/config` | Global |
| `~/.config/alps/config` | Per-user (overrides global) |

```ini
# Colors
color_primary = "\e[36m"    # cyan
color_success = "\e[32m"    # green
color_warning = "\e[33m"    # yellow
color_error   = "\e[31m"    # red

# Symbols
sym_ok   = "✓"    sym_err  = "✗"
sym_warn = "⚠"    sym_info = "◆"

# Header
show_header  = true
title_style  = "default"   # or "custom"
title_line1  = "\e[1;97m  ╔══════════════╗"
title_line2  = "\e[1;97m  ║  ALPS  /\/\  ║"
title_line3  = "\e[1;97m  ╚══════════════╝"

# Aliases
alias_i   = "install"
alias_-S  = "install"
alias_-R  = "remove"
```

## AUR (Arch Linux)

`alps install <pkg>` on Arch tries `pacman -S` first, then falls through to AUR automatically. Uses `yay` if available, otherwise the built-in makepkg path.

### Built-in AUR features

- Full recursive dep resolution — AUR deps are resolved and built in topological order
- All PKGBUILDs shown up-front for review before any build starts
- Single confirm prompt covers the entire build plan
- Unknown dep with no exact match → presents up to 5 candidates to pick a provider
- `pacman -T` used for accurate dep satisfier checking (handles virtual packages and provides)
- Makedep removal offered once at the very end

```bash
alps aur install <pkg>          # install with full dep resolution
alps aur search <query>         # multi-word narrowing search
alps aur list                   # list installed AUR packages
alps aur remove <pkg>           # remove via pacman -R
alps aur clean                  # remove build cache (~/.cache/alps/aur/)
alps aur build-local [dir]      # build from a local PKGBUILD directory
alps aur fetch-abs <pkg>        # fetch official PKGBUILD (asp or Arch GitLab)
```

Requires `git` and `base-devel`. `sudo` is required — `su` fallback is not supported for AUR operations.

## Flatpak & Snap

```bash
alps flatpak install <pkg>    alps snap install <pkg>
alps flatpak search <query>   alps snap search <query>
alps flatpak update           alps snap update
alps flatpak remove <pkg>     alps snap remove <pkg>
```

Snap is only available on Debian/Ubuntu and auto-offered as a fallback when apt can't find a package. `sudo` is required for both — `su` fallback is not supported.

## alps-more

Cross-distro script repo for tools not in standard package managers. Both mirrors are raced simultaneously — fastest valid response wins.

### Commands

```bash
alps repo update                        # refresh cache
alps repo list                          # list available packages for your distro
alps repo list install                  # list installed packages (alps-more + remote)
alps repo list remove                   # list stale packages no longer in repo
alps repo search <query>                # search by name or description
alps repo install <pkg>                 # install with preview and validation
alps repo install github.com/user/repo  # install from a GitHub ALPSMORE file
alps repo install gitlab.com/user/repo  # install from a GitLab ALPSMORE file
alps repo upgrade [pkg]                 # upgrade one or all installed packages
alps repo remove <pkg>                  # remove package
alps repo purge <pkg>                   # remove package and delete config/data
```

`remove` runs `remove_begin` only. `purge` runs `remove_begin` then `purge_begin` — mirrors `apt remove` vs `apt purge`.

Remote installs fetch an `ALPSMORE` file from the repo root. Official alps-more entries always win on name conflict. The `[name]` header is required (recommended) — falls back to repo name if missing. Installed remote packages are tracked in state just like alps-more packages and support `upgrade`, `remove`, and `purge`.

### Package format

```ini
[my-tool]
desc    = Does something useful
author  = someone
version = 1.0.0
arch    = x86_64, aarch64
os      = linux               # linux (native distros only), termux, wsl, arch, debian, ubuntu, fedora, suse, alpine, ...
deps    = curl                # binaries that must exist before install
sudo    = true
servers = https://my-mirror.example.com/
cmd_begin
  {CURL_RUN}scripts/my-tool/install.sh
cmd_end
upgrade_begin
  {CURL_RUN}scripts/my-tool/install.sh
upgrade_end
remove_begin
  sudo rm -f /usr/local/bin/my-tool
remove_end
purge_begin
  sudo rm -rf ~/.config/my-tool
purge_end
```

**Macros available in command blocks:**

| Macro | Expands to | Use for |
|---|---|---|
| `{CURL_RUN}<path>` | `curl -fsSL <mirror><path> \| sh` | fetch and execute a script |
| `{SERVER}` | the resolved mirror URL | wget, passing the URL to a variable |

If `servers=` is omitted, the default GitHub/Codeberg mirrors are used. Mirror is resolved at install time by racing all listed servers. `sudo` is required for alps-more entries that need root — `su` fallback is not supported.

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md).

## License

MIT
