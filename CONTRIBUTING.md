# Contributing to ALPS

Thanks for taking the time to contribute!

## Setup

```bash
git clone https://github.com/adrianpriza-ai/alps
cd alps
make build
```

> **Requirements:** Go 1.25.13+ (release builds use the version pinned in `go.mod`)
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
├── extra/          # snap, flatpak, winget support
├── backend/        # backend selection/routing + native package managers
├── more/           # alps-more script repo
├── moreplanner/    # alps-more planning, paths, and cache management
├── pack/           # package manager commands
├── priv/           # privilege escalation (sudo/su/root)
├── cli/            # CLI parsing and aliases
├── runner/         # process and privilege execution
└── completion/     # shell completion generator
```

## Submitting Changes

1. Fork the repo
2. Create a branch: `git checkout -b my-fix`
3. Make your changes
4. Build and test: `make build`
5. Run tests: `go test ./...` (or `go test -short ./...` for faster CI-friendly tests)
6. Open a pull request — use the PR template

## Guidelines

- Keep commits small and focused
- Test on at least one distro before submitting
- Follow existing code style — no external dependencies
- For AUR-related changes, test on Arch Linux
- For apt/snap changes, test on Debian or Ubuntu

## Testing

The project uses Go's testing framework with both unit and integration tests:

- **Unit tests**: `go test ./...` - Runs all tests including system-specific tests
- **CI-friendly tests**: `go test -short ./...` - Skips tests that require real system calls or specific platforms
- **Package-specific tests**: `go test ./more/` - Tests a specific package

### Test Philosophy

- Unit tests should avoid real system calls when possible (use `t.TempDir()`, mock interfaces, etc.)
- Tests that require real system calls are skipped in short mode for CI compatibility
- Platform-specific tests (macOS, Termux, etc.) are skipped on incompatible platforms
- Tests cover security-critical paths like manifest validation, command execution, and privilege escalation

### Adding New Tests

When adding new functionality:
1. Write unit tests that avoid real system calls (use short mode)
2. Add integration tests for platform-specific behavior in non-short mode
3. Ensure tests pass with `go test -short ./...` before submitting
4. For security-sensitive code, add comprehensive test coverage

## Security Considerations

ALPS handles package installation and remote script execution, so we take security seriously:

- **Manifest authentication**: All official manifests are authenticated with SHA-256 checksums
- **HTTPS-only**: All remote content downloads require HTTPS and approved hosts
- **Privilege escalation**: Centralized through structured privilege decisions
- **Command execution**: Uses typed command structures instead of raw shell strings when possible
- **APT signing**: Repository metadata is signed with GPG, no `trusted=yes`

When contributing:
- Never weaken security checks for convenience
- Always validate user input and file paths
- Use the existing privilege escalation mechanisms (don't add custom sudo calls)
- Test security paths (manifest validation, URL checking, command sanitization)
- Follow the principle of least privilege for code execution

## Reporting Bugs

Use the [bug report template](https://github.com/adrianpriza-ai/alps/issues/new?template=bug_report.md).

## Questions

Open a [discussion](https://github.com/adrianpriza-ai/alps/discussions) or email **coreygit1@gmail.com**.
