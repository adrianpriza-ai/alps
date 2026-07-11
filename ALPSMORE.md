# ALPSMORE — Package Authoring & User Guide

ALPSMORE files are INI-style metadata files for the **alps-more** cross-distro script repository. This guide covers both package creation and usage.

---

## Quick Start

### Users
```bash
alps repo update              # refresh cache
alps repo search <pkg>        # search
alps repo list                # list available
alps repo list install        # list installed
alps repo install <pkg>       # install
alps repo install github.com/user/repo  # install from GitHub
alps repo upgrade [pkg]       # upgrade
alps repo remove <pkg>        # remove
alps repo purge <pkg>         # remove + delete config
alps repo clean               # remove build cache
```

### Authors
Create `ALPSMORE` at repo root. **Minimal example:**
```ini
[my-tool]
desc = My command-line tool
version = 1.0.0
arch = x86_64, aarch64
os = linux, debian, ubuntu
safety = strict  # default mode

cmd_begin
  {DOWNLOAD} https://github.com/me/tool/releases/download/v{VERSION}/tool-{ARCH}
  {EXTRACT} tool-{ARCH}
  {INSTALL_BIN} tool /usr/bin/
cmd_end

# remove_begin is optional - auto-generated from INSTALL macros
```

---

## Package Format

### Structure
```ini
[package-name]
desc = Description
author = Name
version = 1.0.0
arch = x86_64, aarch64, armv7l, i686
os = linux, debian, ubuntu, arch, fedora, alpine, termux, wsl
servers = https://my-mirror.example.com/
deps = curl, git
safety = strict  # strict (default) or free

cmd_begin
  # install commands
cmd_end

upgrade_begin
  # upgrade commands (falls back to cmd_begin if omitted)
upgrade_end

remove_begin
  # uninstall commands (optional - auto-generated from macros)
remove_end

purge_begin
  # uninstall + clean config/data
purge_end
```

### Required Fields
| Field | Values |
|-------|--------|
| `[name]` | Package name (header) |
| `arch` | `x86_64`, `aarch64`, `armv7l`, `i686` |
| `os` | `linux`, `debian`, `ubuntu`, `arch`, `fedora`, `alpine`, `termux`, `wsl` |
| `cmd_begin`/`cmd_end` | Install commands (required) |
| `remove_begin`/`remove_end` | Required in free mode, optional in strict mode (auto-generated) |

### Optional Fields
| Field | Values | Description |
|-------|--------|-------------|
| `safety` | `strict`, `free` | Safety mode (default: `strict`) |
| `desc` | Text | Package description |
| `author` | Text | Package author/maintainer |
| `version` | Semantic version | Version string for upgrades |
| `servers` | URLs | Mirror servers for `{BASH_RUN}` |
| `deps` | Binary names | Required system dependencies |
| `upgrade_begin`/`upgrade_end` | Commands | Custom upgrade commands |
| `purge_begin`/`purge_end` | Commands | Config/data cleanup commands |

---

## Safety Modes

### Strict Mode (Default)
- Uses structured helper macros for file operations
- Validates commands for dangerous patterns
- Safer permissions and protected paths
- **Uses `fakeroot` during build** if available for file operations
- **Defers file operations** (`{INSTALL_*}`, `{SYMLINK}`) until after build completes
- **Auto-generates remove commands** from macros (no manual `remove_begin` needed)
- Recommended for most packages

### Free Mode
- Allows manual scripts and full control
- No command validation
- **Does not use fakeroot** - allows direct access
- File operations execute immediately
- **Requires manual `remove_begin`/`remove_end`** blocks
- For complex packages requiring custom behavior

---

## Variables & Macros

### Placeholders (expanded before execution)
| Placeholder | Example | Description |
|------------|---------|-------------|
| `{ARCH}` | `x86_64` | Normalized architecture |
| `{OS}` | `linux` | Operating system (`runtime.GOOS`) |
| `{DISTRO}` | `ubuntu` | Linux distribution ID from `/etc/os-release` |
| `{VERSION}` | `1.0.0` | Package version from entry definition |
| `{PKG_DIR}` | `~/.cache/alps/more/myapp/` | Build directory path for the package |
| `{SERVER}` | `https://mirror.example.com/` | Resolved mirror base URL |
| `{PKGNAME}` | `myapp` | Package name |
| `{DISVER}` | `22.04` | Distro version |

### Build Phase Macros (build_env)

Execute during the build phase, before installation. In strict mode, these run under `fakeroot` for safe file operations.

| Macro | Syntax | Behavior |
|-------|--------|----------|
| `{DOWNLOAD}` | `{DOWNLOAD} URL [FILE]` | Download file to build directory using Go's HTTP client |
| `{BASH_RUN}` | `{BASH_RUN} /path/to/script [args]` | Download (if URL) and execute shell script via `bash` |
| `{SH}` | `{SH} PATH` | Execute script with `bash` (or `sh` as fallback) |
| `{EXTRACT}` | `{EXTRACT} ARCHIVE` | Extract archive (`.tar.gz`, `.tar.xz`, `.tar.bz2`, `.zip`) |

### Installation Phase Macros (after_env)

Execute after the build phase completes, using `sudo` for real system access (skipped on Termux). All paths are tracked for automatic uninstall.

| Macro | Syntax | Behavior |
|-------|--------|----------|
| `{INSTALL_BIN}` | `{INSTALL_BIN} SOURCE [DEST]` | Install binary to `/usr/bin/` or DEST (755) |
| `{INSTALL_LIB}` | `{INSTALL_LIB} SOURCE [DEST]` | Install library to `/usr/lib/` or DEST (644) |
| `{INSTALL_CONF}` | `{INSTALL_CONF} SOURCE [DEST]` | Install config to `/etc/` or DEST (644) |
| `{INSTALL_MAN}` | `{INSTALL_MAN} SOURCE [DEST]` | Install man page to `/usr/share/man/man1/` or DEST, gzipped |
| `{INSTALL_SERVICE}` | `{INSTALL_SERVICE} SOURCE [DEST]` | Install systemd service to `/etc/systemd/system/` or DEST |
| `{INSTALL_DIR}` | `{INSTALL_DIR} DIRECTORY` | Create directory (755) |
| `{SYMLINK}` | `{SYMLINK} TARGET LINK_NAME` | Create symbolic link |
| `{ENABLE_SERVICE}` | `{ENABLE_SERVICE} SERVICE` | Enable systemd service (`systemctl enable`) |
| `{DISABLE_SERVICE}` | `{DISABLE_SERVICE} SERVICE` | Disable systemd service (`systemctl disable`) |
| `{START_SERVICE}` | `{START_SERVICE} SERVICE` | Start systemd service (`systemctl start`) |
| `{STOP_SERVICE}` | `{STOP_SERVICE} SERVICE` | Stop systemd service (`systemctl stop`) |
| `{RESTART_SERVICE}` | `{RESTART_SERVICE} SERVICE` | Restart systemd service (`systemctl restart`) |
| `{CREATE_USER}` | `{CREATE_USER} USERNAME` | Create system user (`useradd -r -s /bin/false`) |
| `{REMOVE_USER}` | `{REMOVE_USER} USERNAME` | Remove system user (`userdel`) |

**Notes:**
- Service/user macros are no-ops on Termux (no systemd/useradd).
- `{CURL_RUN}` is **deprecated** (replaced by `{BASH_RUN}`).


## Complete Examples

### Example 1: Strict Mode (Recommended)
```ini
[myapp]
desc = My command-line application
version = 2.0.0
arch = x86_64, aarch64
os = linux, debian, ubuntu, arch
safety = strict  # uses fakeroot during build, no sudo

cmd_begin
  # Download and extract (run during build with fakeroot)
  {DOWNLOAD} https://example.com/myapp-{VERSION}-{ARCH}.tar.gz
  {EXTRACT} myapp-{VERSION}-{ARCH}.tar.gz
  cd myapp-{VERSION}
  
  # Build (runs during build with fakeroot)
  make
  
  # File operations (deferred - run after build without fakeroot)
  {INSTALL_BIN} myapp /usr/bin/
  {INSTALL_CONF} myapp.conf /etc/myapp/
  {INSTALL_MAN} myapp.1 /usr/share/man/man1/
  {INSTALL_DIR} /var/lib/myapp
  {SYMLINK} /usr/bin/myapp /usr/local/bin/myapp
  
  # Service and user management (run during build)
  {INSTALL_SERVICE} myapp.service /etc/systemd/system/
  {CREATE_USER} myappuser
  {ENABLE_SERVICE} myapp.service
  {START_SERVICE} myapp.service
cmd_end

# No remove_begin needed - auto-generated from INSTALL macros
# Remove will: stop/disable service, remove user, delete tracked files
```

### Example 2: Free Mode
```ini
[complex-app]
desc = Complex application with custom install script
version = 3.0.0
arch = x86_64
os = linux
safety = free  # no fakeroot, full control

cmd_begin
  {DOWNLOAD} https://example.com/app.tar.gz
  {EXTRACT} app.tar.gz
  ./configure --prefix=/usr
  make
  make install
  {INSTALL_DIR} /var/lib/app
  {CREATE_USER} appuser
cmd_end

remove_begin
  make uninstall | true
  {REMOVE_USER} appuser
  rm -rf /var/lib/app
remove_end
```

---

## Automatic Owned Items Tracking

When using structured macros (`{INSTALL_BIN}`, `{INSTALL_LIB}`, etc.), ALPS automatically tracks installed files and directories in `installed.json`. This enables safe, automatic removal without manual `remove_begin` blocks.

### Benefits
- **Safety**: No dangerous `rm -rf` commands in state files
- **Control**: Each item type removed with appropriate commands
- **Transparency**: Clear visibility into what files/packages own
- **Error Handling**: Graceful handling if items don't exist

### Example installed.json Structure
```json
{
  "myapp": {
    "version": "2.0.0",
    "owned_items": [
      {"path": "/usr/bin/myapp", "type": "file"},
      {"path": "/etc/myapp/myapp.conf", "type": "file"},
      {"path": "/usr/share/man/man1/myapp.1.gz", "type": "file"},
      {"path": "myapp.service", "type": "service"},
      {"path": "myappuser", "type": "user"}
    ],
    "safety": "strict",
  }
}
```

### Removal Process
1. **Remove**: Stops services, removes owned items, then runs manual remove lines
2. **Purge**: Removes owned items, manual remove lines, then config/data cleanup
3. Items are removed in reverse order (children before parents)
4. Individual failures don't stop the entire removal process

---

## Runtime Behavior

- **Build directory**: All commands run in `~/.cache/alps/more/<package>/`
- **Working dir**: Commands run in build directory, no manual `cd` needed
- **Mirror resolution**: Uses `servers=` field or defaults to GitHub Pages + Codeberg
- **Fakeroot**: 
  - **Strict mode**: Build commands run in fakeroot if available, file operations run after without fakeroot
  - **Free mode**: Never uses fakeroot
  - **Termux**: Skips fakeroot (owns its prefix)
- **Deferred execution**: File installation macros run after all build commands complete
- **Immediate execution**: Archives, services, user management run during build phase
- **Cleanup**: On install/upgrade failure, removes tracked owned items automatically

---

## Validation & Safety

**Pre-install checks:**
1. `arch` matches system
2. `os` matches detected distro  
3. `deps` binaries exist (via `exec.LookPath`)
4. `cmd_begin`/`cmd_end` defined (required)
5. **Strict mode**: `remove_begin`/`remove_end` optional (auto-generated from macros)
6. **Free mode**: `remove_begin`/`remove_end` required

**Safety features:**
- **Strict mode**: Validates commands for dangerous patterns (`rm -rf /`, etc.)
- **Fakeroot usage**: Safe file operations during build without full root privileges
- **Protected paths**: Warns about operations on sensitive system paths
- **No `-y` flag**: For repo/aur/flatpak/snap (explicit confirmation required)
- **Full install preview**: Before execution
- **Snapshot saved**: To `installed.json` for remove/purge even if repo disappears
- **Owned items tracking**: Safe removal without dangerous commands

---

## State Files

| File | Location | Purpose |
|------|----------|---------|
| `main.txt` | `/var/cache/alps/more/main.txt` | Cached repo index (Termux: `$PREFIX/var/cache/alps/more/`) |
| `installed.json` | `/var/lib/alps/installed.json` | Installed packages + owned items (Termux: `$PREFIX/var/lib/alps/`) |

Cache expires after 90 days; run `alps repo update` to refresh.

---

## Publishing

**Option 1:** Submit PR to [adrianpriza-ai/alps-more](https://github.com/adrianpriza-ai/alps-more)

**Option 2:** Self-host on GitHub/GitLab
- Place `ALPSMORE` at repo root
- Users install: `alps repo install github.com/you/repo`
- ALPS tries HEAD → main → master branches

**Checklist:**
- [ ] `arch` + `os` fields defined
- [ ] `cmd_begin`/`cmd_end` blocks present (required)
- [ ] **Strict mode**: Remove lines optional (auto-generated from macros)
- [ ] **Free mode**: Remove lines required (no auto-generation)
- [ ] `purge_begin` for config/data cleanup (optional)
- [ ] Set appropriate `safety` mode

---

## Best Practices

1. **Always** define `arch` and `os` — prevents install on unsupported systems
2. **Use structured macros** (`{INSTALL_BIN}`, etc.) for automatic cleanup
3. **Set `safety = strict`** for most packages — safer and more predictable
4. **Make scripts idempotent** — safe to re-run
5. **Set `version`** — enables upgrade detection
6. **List dependencies** in `deps` — pre-install validation
7. **Prefer structured macros** over manual install commands for better tracking

---

## Troubleshooting

| Issue | Solution |
|-------|----------|
| Package not found | Run `alps repo update` |
| Cache missing/expired | Run `alps repo update` |
| Wrong architecture | Check `arch` field supports your CPU |
| Missing deps | Install listed binaries first |
| Items not removed on uninstall | Check if using structured macros for tracking |
| Permission errors in strict mode | Install fakeroot package |
| Service not starting | Check `{ENABLE_SERVICE}` and `{START_SERVICE}` usage |
| Missing remove commands in free mode | Add `remove_begin`/`remove_end` blocks (required in free mode) |

**Debug locations:**
- Installed: `/var/lib/alps/installed.json` (contains `owned_items`)
- Cache: `/var/cache/alps/more/main.txt`
- Build: `~/.cache/alps/more/<package>/`

### Requirements

- GNU coreutils (mkdir, cp, chmod, gzip, ln)
- tar (for .tar.gz, .tar.xz, .tar.bz2 archives)
- unzip (for .zip archives)
- bash (for running scripts)
- fakeroot (not needed on Termux)
- systemctl (for systemd services, not needed on Termux)
- useradd/userdel (for user management, not needed on Termux)

Install using your system package manager, or ALPS itself:

```bash
alps install fakeroot coreutils tar unzip bash
```
