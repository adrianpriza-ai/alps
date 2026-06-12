BINARY  = alps
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
GO      := $(shell command -v go 2>/dev/null)

# Shell helper to dynamically enable colors for aligned 6-character logs based on TTY presence
color_setup = \
  SYM_INFO="[INFO]"; SYM_OK="[ OK ]"; SYM_WARN="[WARN]"; SYM_ERR="[ERRO]"; \
  if [ -t 1 ]; then \
    GREEN="\033[32m"; RED="\033[31m"; CYAN="\033[36m"; YELLOW="\033[33m"; RESET="\033[0m"; \
  else \
    GREEN=""; RED=""; CYAN=""; YELLOW=""; RESET=""; \
  fi

ifeq ($(PREFIX),)
  ifdef TERMUX_VERSION
    PREFIX = $(HOME)/../usr
  else
    UID := $(shell id -u)
    HAS_SUDO := $(shell command -v sudo 2>/dev/null)
    ifeq ($(UID),0)
      PREFIX = /usr/local
    else ifneq ($(HAS_SUDO),)
      PREFIX = /usr/local
    else
      PREFIX = $(HOME)/.local
    endif
  endif
endif

# Normalize prefix paths and check if it's a system folder
IS_SYSTEM := $(shell echo "$(PREFIX)" | grep -Eq "^(/usr|/etc|/var|/opt)" && echo "yes" || echo "")

# Determine SUDO privilege and handle no-sudo fallback
UID := $(shell id -u)
ifeq ($(UID),0)
  SUDO =
else ifneq ($(IS_SYSTEM),)
  HAS_SUDO := $(shell command -v sudo 2>/dev/null)
  ifneq ($(HAS_SUDO),)
    SUDO = sudo
  else
    # Fallback to user directory if system prefix is requested but no sudo is available
    override PREFIX = $(HOME)/.local
    IS_SYSTEM =
    SUDO =
  endif
else
  SUDO =
endif

BINDIR = $(PREFIX)/bin

# Setup shell completion paths
ifneq ($(IS_SYSTEM),)
  FISH_COMP = /usr/share/fish/vendor_completions.d
  ZSH_COMP  = /usr/share/zsh/site-functions
  BASH_COMP = /usr/share/bash-completion/completions
else
  FISH_COMP = $(HOME)/.config/fish/completions
  ZSH_COMP  = $(HOME)/.zsh/completion
  BASH_COMP = $(HOME)/.local/share/bash-completion/completions
endif

build:
	@if [ -z "$(GO)" ]; then \
		printf "  $$RED$$SYM_ERR$$RESET Go is not installed.\n"; \
		printf "     Install it with your package manager:\n"; \
		printf "       Arch:          sudo pacman -S go\n"; \
		printf "       Debian/Ubuntu: sudo apt install golang-go\n"; \
		printf "       Fedora:        sudo dnf install golang\n"; \
		exit 1; \
	fi
	@$(color_setup); printf "  $$CYAN$$SYM_INFO$$RESET Building $(BINARY) $(VERSION)...\n"
	go build -ldflags="-s -w -X main.Version=$(VERSION)" -o $(BINARY) .
	@$(color_setup); printf "  $$GREEN$$SYM_OK$$RESET Build complete.\n"

install: build
	@$(color_setup); printf "  $$CYAN$$SYM_INFO$$RESET Installing $(BINARY)-pm to $(BINDIR)..."
	@$(SUDO) mkdir -p $(BINDIR)
	@$(SUDO) rm -f $(BINDIR)/$(BINARY) $(BINDIR)/$(BINARY)-pm 2>/dev/null || true
	@$(SUDO) cp $(BINARY) $(BINDIR)/$(BINARY)-pm
	@$(SUDO) ln -sf $(BINARY)-pm $(BINDIR)/$(BINARY)
	@$(color_setup); printf "\r  $$GREEN$$SYM_OK$$RESET Installed to $(BINDIR)/$(BINARY)-pm (and symlinked $(BINARY))\n"
	
	@# Shell completions: Fish
	@if command -v fish > /dev/null 2>&1; then \
		$(SUDO) mkdir -p $(FISH_COMP) && \
		./$(BINARY) completion fish | $(SUDO) tee $(FISH_COMP)/$(BINARY)-pm.fish > /dev/null && \
		$(SUDO) ln -sf $(BINARY)-pm.fish $(FISH_COMP)/$(BINARY).fish 2>/dev/null && \
		$(color_setup); printf "  $$GREEN$$SYM_OK$$RESET Fish completions installed to $(FISH_COMP)\n"; \
	fi
	
	@# Shell completions: Zsh
	@if command -v zsh > /dev/null 2>&1; then \
		$(SUDO) mkdir -p $(ZSH_COMP) && \
		./$(BINARY) completion zsh | $(SUDO) tee $(ZSH_COMP)/_$(BINARY)-pm > /dev/null && \
		$(SUDO) ln -sf _$(BINARY)-pm $(ZSH_COMP)/_$(BINARY) 2>/dev/null && \
		$(color_setup); printf "  $$GREEN$$SYM_OK$$RESET Zsh completions installed to $(ZSH_COMP)\n"; \
	fi
	
	@# Shell completions: Bash
	@if command -v bash > /dev/null 2>&1; then \
		$(SUDO) mkdir -p $(BASH_COMP) && \
		./$(BINARY) completion bash | $(SUDO) tee $(BASH_COMP)/$(BINARY)-pm > /dev/null && \
		$(SUDO) ln -sf $(BINARY)-pm $(BASH_COMP)/$(BINARY) 2>/dev/null && \
		$(color_setup); printf "  $$GREEN$$SYM_OK$$RESET Bash completions installed to $(BASH_COMP)\n"; \
	fi
	
	@# Check PATH if local installation
	@case ":$(PATH):" in \
		*:"$(BINDIR)":*) ;; \
		*) \
			$(color_setup); \
			printf "\n  $$YELLOW$$SYM_WARN$$RESET $(BINDIR) is not in your PATH!\n"; \
			printf "     Please add it to your shell configuration:\n"; \
			printf "       export PATH=\"\$$PATH:$(BINDIR)\"\n\n"; \
			;; \
	esac
	@$(color_setup); printf "  $$GREEN$$SYM_OK$$RESET Done! Run '$(BINARY) help' or '$(BINARY)-pm help' to get started.\n"

uninstall:
	@$(color_setup); printf "  $$CYAN$$SYM_INFO$$RESET Uninstalling $(BINARY) and $(BINARY)-pm...\n"
	@$(SUDO) rm -f $(BINDIR)/$(BINARY)
	@$(SUDO) rm -f $(BINDIR)/$(BINARY)-pm
	@$(SUDO) rm -f $(FISH_COMP)/$(BINARY).fish
	@$(SUDO) rm -f $(FISH_COMP)/$(BINARY)-pm.fish
	@$(SUDO) rm -f $(ZSH_COMP)/_$(BINARY)
	@$(SUDO) rm -f $(ZSH_COMP)/_$(BINARY)-pm
	@$(SUDO) rm -f $(BASH_COMP)/$(BINARY)
	@$(SUDO) rm -f $(BASH_COMP)/$(BINARY)-pm
	@$(color_setup); printf "  $$GREEN$$SYM_OK$$RESET ALPS uninstalled successfully.\n"

clean:
	@$(color_setup); printf "  $$CYAN$$SYM_INFO$$RESET Cleaning build artifacts...\n"
	rm -f $(BINARY)
	@$(color_setup); printf "  $$GREEN$$SYM_OK$$RESET Clean complete.\n"

.PHONY: build install uninstall clean
