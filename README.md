<div align="center">
  <img src="assets/alps.png" alt="ALPS" width="600"/>

  # ALPS
  **Advanced Linux Package System**

  *The customizable package manager frontend*

  ![Release](https://img.shields.io/github/v/release/adrianpriza-ai/alps?include_prereleases&style=flat-square&color=red)
  [![Go](https://img.shields.io/badge/Go-1.22+-00ADD8?style=flat-square&logo=go)](https://go.dev)
  [![License](https://img.shields.io/badge/License-MIT-green?style=flat-square)](LICENSE)
  [![AUR](https://img.shields.io/badge/AUR-built--in-1793D1?style=flat-square&logo=archlinux)](https://aur.archlinux.org)
  [![alps-more](https://img.shields.io/badge/alps--more-repo-orange?style=flat-square)](https://codeberg.org/moreland/alps-more)
  [![Build](https://github.com/adrianpriza-ai/alps/actions/workflows/build.yml/badge.svg)](https://github.com/adrianpriza-ai/alps/actions/workflows/build.yml)

</div>

---

ALPS is a Go-based frontend for `apt`, `dnf`, and `pacman` with built-in AUR support, a custom cross-distro script repo (alps-more), fully customizable output styling, shell completion, and a unified command interface across distros.

> **One tool. Every distro. Your style.**

## Features

| | |
|---|---|
| **Multi-distro** | Auto-detects `apt`, `dnf`, or `pacman` |
| **Built-in AUR** | Uses `yay` if available, falls back to `makepkg` |
| **alps-more** | Cross-distro script repo with arch/os/deps validation |
| **Fully customizable** | Colors, symbols, header, aliases — all via config |
| **Shell completion** | fish, bash, and zsh — auto-installs via Makefile |
| **Smart aliases** | Case-sensitive, pacman-style (`-S`, `-R`) supported |

## Installation

### Quick install

```bash
git clone https://github.com/adrianpriza-ai/alps
cd alps
make install
```

`make install` builds the binary, copies it to `/usr/local/bin`, and auto-installs shell completion for any detected shell (fish/zsh/bash).

### Manual

```bash
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

All other commands are mapped to the active backend (apt / dnf / pacman).

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
2. If not found in repo, queries AUR automatically
3. Uses `yay` if installed, otherwise clones and builds with `makepkg -si`
4. Shows PKGBUILD summary for review (makepkg fallback only)

Search queries both repo and AUR simultaneously:

```bash
alps search neovim
```

**Requirements for AUR (makepkg fallback):**
```bash
sudo pacman -S git base-devel
```

## alps-more Repo

alps-more is a cross-distro script repo for tools not available in standard package managers.
Cache is stored globally at `/var/cache/alps/more/` and expires after 90 days.

```bash
alps repo update          # download/refresh repo (requires sudo)
alps repo list            # list all available packages
alps repo install <pkg>   # install a package
alps repo remove <pkg>    # remove a package
```

Each entry in the repo specifies supported architectures, OS/distro, optional dependencies,
and install/remove commands. ALPS validates all of these before running anything.

**alps-more repo:** [codeberg.org/moreland/alps-more](https://codeberg.org/moreland/alps-more)

## Project Structure

```
alps/
├── main.go               # entry point, backend dispatch
├── config/
│   └── config.go         # config loading and parsing
├── ui/
│   └── ui.go             # output, header, progress styles
├── aur/
│   └── aur.go            # built-in AUR helper (yay + makepkg fallback)
├── more/
│   ├── more.go           # alps-more parser, validation, install logic
│   └── fetch.go          # cache download and management
├── completion/
│   └── completion.go     # shell completion generator
├── assets/
│   └── alps.png          # logo
├── Makefile
├── go.mod
├── LICENSE
└── README.md
```

## License

MIT
