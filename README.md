<div align="center">
  <img src="assets/alps.png" alt="ALPS" width="600"/>

  # ALPS
  **Advanced Linux Package System**

  *The most customizable package manager frontend*

  ![Release](https://img.shields.io/github/v/release/adrianpriza-ai/alps?style=flat-square&color=red)
  [![Go](https://img.shields.io/badge/Go-1.26+-00ADD8?style=flat-square&logo=go)](https://go.dev)
  [![License](https://img.shields.io/badge/License-MIT-green?style=flat-square)](LICENSE)
  [![AUR](https://img.shields.io/badge/AUR-built--in-1793D1?style=flat-square&logo=archlinux)](https://aur.archlinux.org)

</div>

---

ALPS is a Go-based frontend for `apt-get`, `dnf`, and `pacman` with built-in AUR support, fully customizable output styling, shell completion, and a unified command interface across distros.

## Features

- **Multi-distro** — works with `apt-get`, `dnf`, and `pacman`, auto-detected
- **Built-in AUR helper** — no dependency on `yay` or `paru`; queries AUR API, clones, and builds with `makepkg`
- **Fully customizable** — colors, symbols, progress style, header, aliases — all via config file
- **Custom title** — default ASCII mountain logo or define your own multi-line header
- **Per-backend progress** — different progress style for apt, dnf, pacman, and AUR
- **Shell completion** — fish, bash, and zsh with package name suggestions
- **Case-sensitive aliases** — supports `-S`, `-R`, and other flag-style shortcuts

## Installation

### Build from source

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

All other commands are mapped to the active backend (apt-get / dnf / pacman).

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
# sym_pkg    = "::"
# sym_arrow  = "->"
# sym_bullet = "::"

# ── Progress ──────────────────────────────────────────────────────
# Presets: pacman | bar | spinner | dots | none
# progress_style   = "pacman"   # global default

# Per-backend override (leave empty to use progress_style)
# progress_apt     = "bar"
# progress_dnf     = "dots"
# progress_pacman  = "pacman"
# progress_aur     = "spinner"  # default for AUR

# progress_bar_char   = "#"
# progress_bar_empty  = "-"
# progress_bar_width  = 30
# progress_spin_chars = "\|/-"

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

ALPS has a built-in AUR helper — no `yay` or `paru` needed.

When on Arch and running `alps install <package>`:
1. Tries `pacman -S` first
2. If not found in repo, queries AUR automatically
3. Shows PKGBUILD summary for review
4. Builds and installs with `makepkg -si`

Search also queries both repo and AUR simultaneously:
```bash
alps search neovim
```

**Requirements for AUR:**
```bash
sudo pacman -S git base-devel
```

## Project Structure

```
alps/
├── main.go          # entry point, backend dispatch
├── config/
│   └── config.go    # config loading and parsing
├── ui/
│   └── ui.go        # output, header, progress styles
├── aur/
│   └── aur.go       # built-in AUR helper
├── completion/
│   └── completion.go # shell completion generator
├── assets/
│   └── alps.png     # logo
├── go.mod
├── LICENSE
└── README.md
```

## License

MIT
