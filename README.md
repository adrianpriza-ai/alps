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
| **Multi-distro** | Auto-detects `apt`, `apt-get`, `dnf`, or `pacman` |
| **Termux support** | Full support on Android — no sudo, native `$PREFIX` paths |
| **WSL support** | Works on WSL; alps-more entries can target `os = wsl` |
| **Built-in AUR** | Uses `yay` if available, falls back to `makepkg` with dep resolution |
| **Snap & Flatpak** | First-class subcommands; snap auto-offered on Ubuntu/Debian |
| **alps-more** | Cross-distro script repo with version tracking, mirror failover, and purge support |
| **Fully customizable** | Colors, symbols, header, aliases — all via config file |
| **Smart completion** | fish, bash, zsh — distro-aware with live package name completion |

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

Requires Go 1.22+ and `sudo`.

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

Completion is environment-aware — AUR subcommands only appear on Arch, snap only on Debian/Ubuntu, neither on Termux. `alps repo install [TAB]` completes live from the local cache.

## Usage

```
alps <command> [args]
```

| Command | Description |
|---|---|
| `install <pkg>` | Install from repo, AUR, or alps-more (auto-detected) |
| `search <query>` | Search repo + AUR simultaneously |
| `upgrade` | Upgrade system + AUR packages |
| `repo <subcommand>` | Manage alps-more packages |
| `aur <subcommand>` | AUR management (Arch only) |
| `flatpak <subcommand>` | Flatpak management |
| `snap <subcommand>` | Snap management (Ubuntu/Debian) |
| `completion <shell>` | Generate shell completion |
| `config-show` | Show active config and paths |
| `aliases` | Show active aliases |
| `version` | Show version |

All other commands pass directly to the active backend.

### Default aliases

| Alias | Command | Alias | Command |
|---|---|---|---|
| `ins` | install | `se` | search |
| `rm` | remove | `sh` | show |
| `pu` | purge | `ls` | list |
| `up` | update | `au` | autoremove |
| `ug` | upgrade | `ac` | autoclean |
| `fug` | full-upgrade | `cl` | clean |

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

`alps install <pkg>` on Arch tries `pacman -S` first, then AUR automatically. Uses `yay` if available, otherwise `makepkg -si` with PKGBUILD review and dep checking.

```bash
alps aur install <pkg>
alps aur search <query>
alps aur list
alps aur clean
```

Requires `git` and `base-devel`.

## Flatpak & Snap

```bash
alps flatpak install <pkg>    alps snap install <pkg>
alps flatpak search <query>   alps snap search <query>
alps flatpak update           alps snap update
alps flatpak remove <pkg>     alps snap remove <pkg>
```

Snap is only available on Debian/Ubuntu and auto-offered as a fallback when apt can't find a package.

## alps-more

Cross-distro script repo for tools not in standard package managers. Both mirrors are hit simultaneously — whichever responds first wins.

### Commands

```bash
alps repo update            # refresh cache
alps repo list              # list available packages for your distro
alps repo search <query>    # search by name or description
alps repo install <pkg>     # install with preview and validation
alps repo upgrade [pkg]     # upgrade one or all installed packages
alps repo remove <pkg>      # remove package
alps repo purge <pkg>       # remove package and delete config/data
```

### Package format

```ini
[my-tool]
desc    = Does something useful
author  = someone
version = 1.0.0
arch    = x86_64, aarch64
os      = linux               # linux, termux, wsl, arch, debian, ubuntu, fedora, ...
deps    = curl                # binaries that must exist before install
sudo    = true
servers = https://adrianpriza-ai.github.io/alps-more/, https://moreland.codeberg.page/alps-more/
cmd_begin
  {CURL_RUN}scripts/my-tool/install.sh
cmd_end
upgrade_begin
  {CURL_RUN}}scripts/my-tool/install.sh
upgrade_end
remove_begin
  sudo rm -f /usr/local/bin/my-tool
remove_end
purge_begin
  sudo rm -rf ~/.config/my-tool
purge_end
```

`{CURL_RUN}<path>` fetches and executes a script from the resolved mirror
at install time. The fastest responding server from `servers=` wins.
If `servers=` is omitted, the default GitHub/Codeberg mirrors are used.

`remove` runs `remove_begin` only. `purge` runs `remove_begin` then `purge_begin` — mirrors `apt remove` vs `apt purge`.

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md).

## License

MIT
