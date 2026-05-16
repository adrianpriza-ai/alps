<div align="center">
  <img src="https://adrianpriza-ai.github.io/alps/alps.png" alt="ALPS" style="width:100%;max-width:800px"/>

  # ALPS
  **Advanced Linux Package System**

  *The customizable package manager frontend*

  [![Release](https://img.shields.io/github/v/release/adrianpriza-ai/alps?include_prereleases&style=flat&color=red)](https://github.com/adrianpriza-ai/alps/releases)
  [![License](https://img.shields.io/badge/License-MIT-green?style=flat)](LICENCE)
  [![Go](https://img.shields.io/badge/Go-1.22+-00ADD8?style=flat&logo=go)](https://go.dev)
  [![Build](https://github.com/adrianpriza-ai/alps/actions/workflows/build.yml/badge.svg)](https://github.com/adrianpriza-ai/alps/actions/workflows/build.yml)
  [![Go Report Card](https://goreportcard.com/badge/github.com/adrianpriza-ai/alps?v=1)](https://goreportcard.com/report/github.com/adrianpriza-ai/alps)

  [![AUR](https://img.shields.io/badge/AUR-built--in-1793D1?style=flat&logo=archlinux)](https://aur.archlinux.org)
  [![alps-more](https://img.shields.io/badge/alps--more-repo-orange?style=flat)](https://github.com/adrianpriza-ai/alps-more)

</div>

---

ALPS is a Go-based frontend for `apt`, `apt-get`, `dnf`, and `pacman` with built-in AUR, Flatpak, and Snap support, a custom cross-distro script repo (alps-more), fully customizable output styling, shell completion, and a unified command interface across distros.

> **One tool. Every distro. Your style.**

## Features

| | |
|---|---|
| **Multi-distro** | Auto-detects `apt`, `apt-get`, `dnf`, or `pacman` |
| **Built-in AUR** | Uses `yay` if available, falls back to `makepkg` with dep resolution |
| **Snap fallback** | Auto-falls back to snap on Ubuntu/Debian if apt can't find a package |
| **Flatpak support** | First-class `alps flatpak` subcommand for all distros |
| **alps-more** | Cross-distro script repo with version tracking, auto-cleanup on failure, stale entry detection |
| **Fully customizable** | Colors, symbols, header, aliases — all via config |
| **Smart completion** | fish, bash, zsh — auto-configured per distro |
| **Smart aliases** | Case-sensitive, pacman-style (`-S`, `-R`) supported |

## Installation

### Quick install

> **Requirements:** [Go 1.22+](https://go.dev/dl/) and `sudo` access

```bash
git clone https://github.com/adrianpriza-ai/alps
cd alps
make install
```

`make install` requires `sudo` to copy the binary to `/usr/local/bin` and install bash/zsh completions.

### Manual

```bash
git clone https://github.com/adrianpriza-ai/alps
cd alps
go build -o alps .
sudo cp alps /usr/local/bin/alps
```

### Shell completion

```bash
# Fish
alps completion fish > ~/.config/fish/completions/alps.fish

# Bash
alps completion bash > /etc/bash_completion.d/alps

# Zsh
alps completion zsh > "${fpath[1]}/_alps"
autoload -U compinit && compinit
```

## Usage

```
alps <command> [args]
```

| Command | Description |
|---|---|
| `help` | Show help |
| `aliases` | Show active aliases |
| `config-show` | Show active config and paths |
| `version` | Show version |
| `completion <shell>` | Generate shell completion (fish/bash/zsh) |
| `repo <subcommand>` | Manage alps-more repo |
| `aur <subcommand>` | Manage AUR packages directly (Arch only) |
| `flatpak <subcommand>` | Manage Flatpak packages |
| `snap <subcommand>` | Manage Snap packages |

All other commands are mapped to the active backend (apt / apt-get / dnf / pacman).

### Default aliases

| Alias | Command |
|---|---|
| `ins` | install |
| `rm` | remove |
| `pu` | purge |
| `up` | update |
| `ug` | upgrade |
| `fug` | full-upgrade |
| `se` | search |
| `sh` | show |
| `ls` | list |
| `au` | autoremove |
| `ac` | autoclean |
| `cl` | clean |

## Configuration

| Path | Scope |
|---|---|
| `/etc/alps/config` | Global default |
| `~/.config/alps/config` | Per-user override |

User config overrides global. Both are optional.

### Full config reference

```ini
# ── Colors (ANSI escape codes) ────────────────────────────────────
# color_primary  = "\e[36m"    # cyan (default)
# color_success  = "\e[32m"    # green
# color_warning  = "\e[33m"    # yellow
# color_error    = "\e[31m"    # red
# color_info     = "\e[34m"    # blue

# ── Symbols ───────────────────────────────────────────────────────
# sym_ok     = "✓"
# sym_err    = "✗"
# sym_warn   = "⚠"
# sym_info   = "◆"

# ── Header ────────────────────────────────────────────────────────
# show_header = true

# title_style = "default"   # shows built-in ASCII mountain
# title_style = "custom"    # uses title_line* below

# title_line1 = "\e[1;97m  ╔══════════════════╗"
# title_line2 = "\e[1;97m  ║  ALPS  /\/\ /\   ║"
# title_line3 = "\e[1;97m  ╚══════════════════╝"

# ── Aliases ───────────────────────────────────────────────────────
# alias_i   = "install"
# alias_-S  = "install"    # pacman-style flag aliases
# alias_-R  = "remove"
# alias_fu  = "full-upgrade"
```

## AUR Support (Arch Linux only)

When on Arch and running `alps install <package>`:

1. Tries `pacman -S` first
2. If not found, queries AUR automatically
3. Uses `yay` if installed, otherwise clones and builds with `makepkg -si`
4. Resolves and checks dependencies — stops if any dep is AUR-only (must install manually)
5. Shows PKGBUILD summary for review (makepkg fallback only)
6. After build, asks to remove makedepends and build cache

Direct AUR management:

```bash
alps aur install <pkg>   # install directly from AUR
alps aur search <query>  # search AUR only
alps aur list            # list installed AUR packages
alps aur clean           # remove build cache (~/.cache/alps/aur/)
```

Search queries both repo and AUR simultaneously:

```bash
alps search neovim
```

**Requirements for AUR (makepkg fallback):**
```bash
sudo pacman -S git base-devel
```

## Snap Support (Ubuntu/Debian)

On Ubuntu/Debian, if a package is not found in apt, alps automatically offers to install via snap (if snapd is available and not blocked).

Direct snap management:

```bash
alps snap install <pkg>
alps snap search <query>
alps snap list
alps snap update
alps snap remove <pkg>
```

## Flatpak Support

Available on all distros if flatpak is installed. Uses Flathub by default.

```bash
alps flatpak install <pkg>
alps flatpak search <query>
alps flatpak list
alps flatpak update
alps flatpak remove <pkg>
```

## alps-more Repo

alps-more is a cross-distro script repo for tools not available in standard package managers.
Cache is stored globally at `/var/cache/alps/more/` and expires after 90 days.

### Commands

```bash
alps repo update          # refresh cache from GitHub/Codeberg (requires sudo)
alps repo list            # list all available packages for your distro
alps repo install <pkg>   # install a package with validation & preview
alps repo remove <pkg>    # remove a package
alps repo search <query>  # search by name/desc (like pacman -Ss)
alps repo upgrade [pkg]   # upgrade installed package(s)
```

### Package tracking

ALPS tracks installed packages and their versions in `/var/cache/alps/more/installed.json`. When upgrading:
- If a newer version is available → runs `upgrade_cmd` (or falls back to `cmd_begin`)
- If already at latest version → reinstalls
- If install fails → auto-runs `remove_cmd` cleanup, user can retry with `alps repo install`

Stale entries (packages removed from repo) are reported but not auto-removed. Remove manually with `alps repo remove <pkg>`.

### Package format

Each entry specifies:
- `desc`, `author`, `version` — metadata
- `arch` (required) — x86_64, aarch64, etc.
- `os` (required) — linux, arch, debian, etc.
- `deps` (optional) — required binaries to check
- `sudo` (optional) — true if commands need privilege
- `cmd_begin`...`cmd_end` — install commands
- `upgrade_begin`...`upgrade_end` — upgrade commands (optional, fallback to install)
- `remove_begin`...`remove_end` — cleanup commands

Example:

```ini
[ollama]
desc       = Run LLMs locally
author     = ollama.com
version    = 0.3.12
arch       = x86_64, aarch64
os         = linux
deps       = curl
sudo       = true
cmd_begin
  curl -fsSL https://ollama.com/install.sh | sh
cmd_end
upgrade_begin
  curl -fsSL https://ollama.com/install.sh | sh
upgrade_end
remove_begin
  sudo systemctl disable ollama --now
  sudo rm -f /usr/local/bin/ollama
remove_end
```

### Reliability

- **Network retries:** 3 attempts with backoff (timeout 15s per request)
- **Cache validation:** corrupted cache is detected; old cache is kept if download fails
- **JSON recovery:** corrupted `installed.json` is backed up and reset
- **Install safety:** shows full preview (commands + cleanup) before confirming
- **Failure recovery:** if install fails mid-way, cleanup is run automatically

**Repo:** [github.com/adrianpriza-ai/alps-more](https://github.com/adrianpriza-ai/alps-more)

## Project Structure

```
alps/
├── main.go               # entry point, backend dispatch, CLI handlers
├── config/
│   └── config.go         # config loading and parsing
├── ui/
│   └── ui.go             # output formatting, headers, helpers
├── aur/
│   └── aur.go            # AUR helper (yay + makepkg, dep resolution)
├── snap/
│   └── snap.go           # snap package manager support
├── flatpak/
│   └── flatpak.go        # flatpak support
├── more/
│   ├── more.go           # alps-more parser, validation, install/upgrade/remove logic
│   ├── fetch.go          # cache download with retry, validation, timeout
│   └── state.go          # installed.json tracking with corruption recovery
├── priv/
│   └── priv.go           # privilege escalation (sudo/su/root handling)
├── completion/
│   └── completion.go     # shell completion generator (distro-aware)
├── Makefile
├── go.mod
├── CODE_OF_CONDUCT.md
├── CONTRIBUTING.md
├── LICENSE
├── README.md
└── SECURITY.md
```

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md).

## License

MIT
