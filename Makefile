BINARY  = alps
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
GO      := $(shell command -v go 2>/dev/null)

color_setup = \
  SYM_INFO="[INFO]"; SYM_OK="[ OK ]"; SYM_WARN="[WARN]"; SYM_ERR="[ERRO]"; \
  if [ -t 1 ]; then \
    GREEN="\033[32m"; RED="\033[31m"; BLUE="\033[0;34m"; YELLOW="\033[33m"; RESET="\033[0m"; \
  else \
    GREEN=""; RED=""; BLUE=""; YELLOW=""; RESET=""; \
  fi

UNAME := $(shell uname -s 2>/dev/null)
IS_TERMUX := $(shell [ -d /data/data/com.termux/files ] && echo "yes" || echo "")

ifeq ($(PREFIX),)
  ifdef TERMUX_VERSION
    PREFIX = $(HOME)/../usr
  else ifeq ($(IS_TERMUX),yes)
    PREFIX = $(HOME)/../usr
  else ifeq ($(UNAME),Darwin)
    PREFIX = /usr/local
  else ifeq ($(UNAME),FreeBSD)
    PREFIX = /usr/local
  else ifeq ($(UNAME),OpenBSD)
    PREFIX = /usr/local
  else ifeq ($(UNAME),NetBSD)
    PREFIX = /usr/local
  else ifeq ($(UNAME),DragonFly)
    PREFIX = /usr/local
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

IS_SYSTEM := $(shell echo "$(PREFIX)" | grep -Eq "^(/usr|/etc|/var|/opt)" && echo "yes" || echo "")

UID := $(shell id -u)
ifeq ($(UID),0)
  SUDO =
else ifdef TERMUX_VERSION
  SUDO =
else ifneq ($(IS_SYSTEM),)
  ifeq ($(UNAME),OpenBSD)
    HAS_DOAS := $(shell command -v doas 2>/dev/null)
    ifneq ($(HAS_DOAS),)
      SUDO = doas
    else
      HAS_SUDO := $(shell command -v sudo 2>/dev/null)
      ifneq ($(HAS_SUDO),)
        SUDO = sudo
      else
        override PREFIX = $(HOME)/.local
        IS_SYSTEM =
        SUDO =
      endif
    endif
  else
    HAS_SUDO := $(shell command -v sudo 2>/dev/null)
    ifneq ($(HAS_SUDO),)
      SUDO = sudo
    else
      override PREFIX = $(HOME)/.local
      IS_SYSTEM =
      SUDO =
    endif
  endif
else
  SUDO =
endif

BINDIR = $(PREFIX)/bin

ifdef TERMUX_VERSION
  FISH_COMP = $(HOME)/.config/fish/completions
  ZSH_COMP  = $(HOME)/.zsh/completion
  BASH_COMP = $(HOME)/.local/share/bash-completion/completions
else ifneq ($(IS_SYSTEM),)
  ifeq ($(UNAME),Darwin)
    FISH_COMP = /usr/local/share/fish/vendor_completions.d
    ZSH_COMP  = /usr/local/share/zsh/site-functions
    BASH_COMP = /usr/local/etc/bash_completion.d
  else ifeq ($(filter $(UNAME),FreeBSD OpenBSD NetBSD DragonFly),$(UNAME))
    FISH_COMP = /usr/local/share/fish/vendor_completions.d
    ZSH_COMP  = /usr/local/share/zsh/site-functions
    BASH_COMP = /usr/local/etc/bash_completion.d
  else
    FISH_COMP = /usr/share/fish/vendor_completions.d
    ZSH_COMP  = /usr/share/zsh/site-functions
    BASH_COMP = /usr/share/bash-completion/completions
  endif
else
  FISH_COMP = $(HOME)/.config/fish/completions
  ZSH_COMP  = $(HOME)/.zsh/completion
  BASH_COMP = $(HOME)/.local/share/bash-completion/completions
endif

build:
	@if [ -z "$(GO)" ]; then \
		printf "  $$RED$$SYM_ERR$$RESET Go is not installed.\n"; \
		printf "     Install it with your package manager:\n"; \
		printf "       macOS (Homebrew):    brew install go\n"; \
		printf "       Arch Linux:          sudo pacman -S go\n"; \
		printf "       Debian/Ubuntu:       sudo apt install golang-go\n"; \
		printf "       Fedora:              sudo dnf install golang\n"; \
		printf "       Alpine Linux:        sudo apk add go\n"; \
		printf "       openSUSE:            sudo zypper install go\n"; \
		printf "       FreeBSD:             pkg install go\n"; \
		printf "       OpenBSD:             doas pkg_add go\n"; \
		printf "       NetBSD:              pkgin install go\n"; \
		printf "       Termux (Android):    pkg install golang\n"; \
		exit 1; \
	fi
	@$(color_setup); printf "  $$BLUE$$SYM_INFO$$RESET Building $(BINARY) $(VERSION)...\n"
	go build -ldflags="-s -w -X main.Version=$(VERSION)" -o $(BINARY) .
	@$(color_setup); printf "  $$GREEN$$SYM_OK$$RESET Build complete.\n"

install: build
	@$(color_setup); printf "  $$BLUE$$SYM_INFO$$RESET Installing $(BINARY)-pm to $(BINDIR)..."
	@$(SUDO) mkdir -p $(BINDIR)
	@$(SUDO) rm -f $(BINDIR)/$(BINARY) $(BINDIR)/$(BINARY)-pm 2>/dev/null || true
	@$(SUDO) cp $(BINARY) $(BINDIR)/$(BINARY)-pm
	@$(SUDO) ln -sf $(BINARY)-pm $(BINDIR)/$(BINARY)
	@$(color_setup); printf "\r  $$GREEN$$SYM_OK$$RESET Installed to $(BINDIR)/$(BINARY)-pm (and symlinked $(BINARY))\n"
	
	@if command -v fish > /dev/null 2>&1; then \
		$(SUDO) mkdir -p $(FISH_COMP) && \
		./$(BINARY) completion fish | $(SUDO) tee $(FISH_COMP)/$(BINARY)-pm.fish > /dev/null && \
		$(SUDO) ln -sf $(BINARY)-pm.fish $(FISH_COMP)/$(BINARY).fish 2>/dev/null && \
		$(color_setup); printf "  $$GREEN$$SYM_OK$$RESET Fish completions installed to $(FISH_COMP)\n"; \
	fi
	
	@if command -v zsh > /dev/null 2>&1; then \
		$(SUDO) mkdir -p $(ZSH_COMP) && \
		./$(BINARY) completion zsh | $(SUDO) tee $(ZSH_COMP)/_$(BINARY)-pm > /dev/null && \
		$(SUDO) ln -sf _$(BINARY)-pm $(ZSH_COMP)/_$(BINARY) 2>/dev/null && \
		$(color_setup); printf "  $$GREEN$$SYM_OK$$RESET Zsh completions installed to $(ZSH_COMP)\n"; \
	fi
	
	@if command -v bash > /dev/null 2>&1; then \
		$(SUDO) mkdir -p $(BASH_COMP) && \
		./$(BINARY) completion bash | $(SUDO) tee $(BASH_COMP)/$(BINARY)-pm > /dev/null && \
		$(SUDO) ln -sf $(BINARY)-pm $(BASH_COMP)/$(BINARY) 2>/dev/null && \
		$(color_setup); printf "  $$GREEN$$SYM_OK$$RESET Bash completions installed to $(BASH_COMP)\n"; \
	fi
	
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
	@$(color_setup); printf "  $$BLUE$$SYM_INFO$$RESET Uninstalling $(BINARY) and $(BINARY)-pm...\n"
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
	@$(color_setup); printf "  $$BLUE$$SYM_INFO$$RESET Cleaning build artifacts...\n"
	rm -f $(BINARY)
	@$(color_setup); printf "  $$GREEN$$SYM_OK$$RESET Clean complete.\n"

help:
	@$(color_setup); printf "  $$BLUE$$SYM_INFO$$RESET ALPS Makefile Help\n"
	@$(color_setup); printf "\n  $$GREEN$$SYM_OK$$RESET Available targets:\n"
	@$(color_setup); printf "     make build      - Build the ALPS binary\n"
	@$(color_setup); printf "     make install    - Install ALPS and completions\n"
	@$(color_setup); printf "     make uninstall  - Remove ALPS and completions\n"
	@$(color_setup); printf "     make clean      - Clean build artifacts\n"
	@$(color_setup); printf "     make help       - Show this help message\n"
	@$(color_setup); printf "\n  $$BLUE$$SYM_INFO$$RESET Platform-specific notes:\n"; \
	if [ -n "$(TERMUX_VERSION)" ] || [ -d /data/data/com.termux/files ]; then \
		printf "     Termux: Install to ~/../usr (Termux prefix)\n"; \
	elif [ "$(UNAME)" = "Darwin" ]; then \
		printf "     macOS: Uses Homebrew paths (/usr/local)\n"; \
	elif [ "$(UNAME)" = "FreeBSD" ] || [ "$(UNAME)" = "OpenBSD" ] || [ "$(UNAME)" = "NetBSD" ] || [ "$(UNAME)" = "DragonFly" ]; then \
		printf "     $(UNAME): Uses /usr/local prefix\n"; \
	else \
		printf "     Linux: Uses system paths or ~/.local\n"; \
	fi
	@$(color_setup); printf "\n  $$BLUE$$SYM_INFO$$RESET Current settings:\n"
	@$(color_setup); printf "     PREFIX = $(PREFIX)\n"
	@$(color_setup); printf "     BINDIR = $(BINDIR)\n"
	@$(color_setup); printf "     SUDO   = $(SUDO)\n"

.PHONY: build install uninstall clean help
