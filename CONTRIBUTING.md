# Contributing to ALPS

Thanks for taking the time to contribute!

## Setup

```bash
git clone https://github.com/adrianpriza-ai/alps
cd alps
make build
```

> **Requirements:** Go 1.22+
> Install Go with your package manager:
> - Arch: `sudo pacman -S go`
> - Debian/Ubuntu: `sudo apt install golang-go`
> - Fedora: `sudo dnf install golang`
> - OpenSUSE: `sudo zypper install go`
> - Alpine Linux: `apk add --no-cache go`

## Project Structure

```
alps/
├── main.go         # entry point, backend dispatch
├── config/         # config loading and parsing
├── ui/             # output, header, colors
├── aur/            # AUR helper (yay + makepkg, dep resolution)
├── snap/           # snap support
├── flatpak/        # flatpak support
├── more/           # alps-more script repo
├── pack/           # package manager commands
├── priv/           # privilege escalation (sudo/su/root)
└── completion/     # shell completion generator
```

## Submitting Changes

1. Fork the repo
2. Create a branch: `git checkout -b my-fix`
3. Make your changes
4. Build and test: `make build`
5. Open a pull request — use the PR template

## Guidelines

- Keep commits small and focused
- Test on at least one distro before submitting
- Follow existing code style — no external dependencies
- For AUR-related changes, test on Arch Linux
- For apt/snap changes, test on Debian or Ubuntu

## Reporting Bugs

Use the [bug report template](https://github.com/adrianpriza-ai/alps/issues/new?template=bug_report.md).

## Questions

Open a [discussion](https://github.com/adrianpriza-ai/alps/discussions) or email **coreygit1@gmail.com**.
