## 📄 `ALPS.md` (Markdown)

```markdown
# ALPS — Advanced Linux Package System

> **One tool. Every distro. Your style.**  
> v0.9 • Go 1.22+ • MIT License

[GitHub](https://github.com/adrianpriza-ai/alps) • [Releases](https://github.com/adrianpriza-ai/alps/releases) • [alps-more](https://github.com/adrianpriza-ai/alps-more)

---

## ✨ Features

| Feature | Description |
|---------|-------------|
| **Multi-distro** | Auto-detects `apt`, `apt-get`, `dnf`, or `pacman` |
| **Built-in AUR** | Uses `yay` if available; falls back to `makepkg -si` with dep resolution |
| **Snap fallback** | Auto-offers `snap install` on Ubuntu/Debian when apt fails |
| **Flatpak support** | First-class `alps flatpak` subcommand via Flathub |
| **alps-more** | Cross-distro script repo with arch/os/deps validation |
| **Fully customizable** | Colors, symbols, header, aliases via `~/.config/alps/config` |
| **Smart completion** | fish/bash/zsh — auto-installed per detected shell |
| **Smart aliases** | Case-sensitive, pacman-style `-S`, `-R` supported |

---

## 🚀 Installation

### Quick Install (Recommended)
```bash
git clone https://github.com/adrianpriza-ai/alps
cd alps && make install
# → builds binary, installs to /usr/local/bin, sets up shell completion
```

### Manual Build
```bash
git clone https://github.com/adrianpriza-ai/alps
cd alps
go build -o alps .
sudo cp alps /usr/local/bin/alps
```

### Shell Completion
```bash
# Fish
alps completion fish > ~/.config/fish/completions/alps.fish

# Bash
alps completion bash > /etc/bash_completion.d/alps

# Zsh
alps completion zsh > "${fpath[1]}/_alps"
autoload -U compinit && compinit
```

---

## 🛠️ Commands

| Command | Description |
|---------|-------------|
| `alps install <pkg>` | Install from repo, AUR, or alps-more (auto-detected) |
| `alps search <query>` | Search repo + AUR simultaneously |
| `alps upgrade` | Upgrade system + AUR packages |
| `alps autoremove` | Remove orphaned packages |
| `alps repo update` | Refresh alps-more cache (requires sudo) |
| `alps repo install <pkg>` | Validate + install from alps-more |
| `alps repo list` | List all available alps-more packages |
| `alps completion <shell>` | Generate shell completion script |
| `alps config-show` | Show active config, paths, style preview |
| `alps aliases` | List all active aliases |

### Default Aliases
```
ins → install    • rm → remove    • pu → purge
up → update      • ug → upgrade   • fug → full-upgrade
se → search      • sh → show      • ls → list
au → autoremove  • ac → autoclean • cl → clean
```

---

## ⚙️ Configuration

**Paths** (user overrides global):
- Global: `/etc/alps/config`
- User: `~/.config/alps/config`

**Example config** (`~/.config/alps/config`):
```ini
# Colors (ANSI)
color_primary   = "\e[36m"   # cyan
color_success   = "\e[32m"   # green
color_warning   = "\e[33m"   # yellow
color_error     = "\e[31m"   # red

# Symbols
sym_ok    = "✓"
sym_err   = "✗"
sym_warn  = "⚠"
sym_info  = "◆"

# Header
show_header   = true
title_style   = "default"   # or "custom"

# Aliases
alias_-S  = "install"
alias_-R  = "remove"
alias_i   = "install"
alias_fu  = "full-upgrade"
```

---

## 🧩 AUR Support (Arch Only)

Workflow for `alps install <pkg>` on Arch:
1. Try `pacman -S` first
2. If not found → query AUR
3. Use `yay` if installed; else clone + build with `makepkg -si`
4. Resolve deps; stop if any dep is AUR-only (manual install required)
5. Show PKGBUILD summary for review (makepkg fallback only)
6. Post-build: ask to remove makedepends + build cache

**Direct AUR commands**:
```bash
alps aur install <pkg>   # install from AUR
alps aur search <query>  # search AUR only
alps aur list            # list installed AUR packages
alps aur clean           # clear ~/.cache/alps/aur/
```

**Requirements**: `sudo pacman -S git base-devel`

---

## 📦 Snap & Flatpak

### Snap (Ubuntu/Debian)
Auto-fallback when apt fails. Direct commands:
```bash
alps snap install|search|list|update|remove <pkg>
```
*Checks `/etc/apt/preferences.d/nosnap.pref` (Linux Mint) before offering snap.*

### Flatpak (All distros)
Uses Flathub by default:
```bash
alps flatpak install|search|list|update|remove <pkg>
```

---

## 🗂️ alps-more Repo

Cross-distro script repo for tools outside standard package managers.

**Cache**: `/var/cache/alps/more/` • Expires: 90 days  
**Hosted**: [GitHub](https://github.com/adrianpriza-ai/alps-more) / [Codeberg](https://codeberg.org/moreland/alps-more)

**Commands**:
```bash
alps repo update          # refresh cache (sudo)
alps repo list            # list packages
alps repo install <pkg>   # validate + install
alps repo remove <pkg>    # remove
```

**Entry format** (`[pkg@distro]`):
```ini
[ollama]
desc   = Run LLMs locally
arch   = x86_64, aarch64
os     = linux
deps   = curl
sudo   = true
cmd_begin
  curl -fsSL https://ollama.com/install.sh | sh
cmd_end
remove_begin
  sudo systemctl disable ollama --now
  sudo rm -f /usr/local/bin/ollama
remove_end
```

---

## 🗂️ Project Structure

```
alps/
├── main.go               # entry point, backend dispatch
├── config/config.go      # config loading
├── ui/ui.go              # output, header, help
├── aur/aur.go            # AUR helper (yay + makepkg)
├── more/                 # alps-more parser + installer
├── snap/snap.go          # snap support
├── flatpak/flatpak.go    # flatpak support
├── priv/priv.go          # privilege escalation (sudo/su/root)
├── completion/           # shell completion (fish/bash/zsh)
├── Makefile
├── go.mod
└── README.md
```

---

## ⚠️ Notes to Self (Dev)

- `apt-get` needs separate cmdMap (`apt search` ≠ `apt-cache search`)
- Never run `pacman -Sy` or `-Su` alone → partial upgrade = bad
- `/tmp` is RAM on Arch → use `~/.cache` for AUR builds
- `priv.Command()` priority: `root > sudo > su > error`
- `LANG=C` only for `pacman -Sp` (check), never for output
- Snap fallback checks Linux Mint's `nosnap.pref`
- alps-more cache: `/var/cache/alps/more/`, 90-day expiry
- Completion is distro-aware: `aur` only on Arch, `snap` only on Debian/Ubuntu

---

## 🤝 Contributing & License

- See [CONTRIBUTING.md](https://github.com/adrianpriza-ai/alps/blob/main/CONTRIBUTING.md)
- **License**: MIT
```
