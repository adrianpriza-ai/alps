BINARY  = alps
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
GO      := $(shell command -v go 2>/dev/null)

GREEN  := \033[32m
RED    := \033[31m
RESET  := \033[0m

ifdef TERMUX_VERSION
  PREFIX    = $(HOME)/../usr/bin
  SUDO      =
  FISH_COMP = $(HOME)/../usr/share/fish/vendor_completions.d
  ZSH_COMP  = $(HOME)/../usr/share/zsh/site-functions
  BASH_COMP = $(HOME)/../usr/etc/bash_completion.d
else
  PREFIX    = /usr/local/bin
  SUDO      = sudo
  FISH_COMP = /usr/share/fish/vendor_completions.d
  ZSH_COMP  = /usr/share/zsh/site-functions
  BASH_COMP = /etc/bash_completion.d
endif

build:
	@if [ -z "$(GO)" ]; then \
		printf "  $(RED) !! $(RESET) Go is not installed.\n"; \
		printf "     Install it with your package manager:\n"; \
		printf "       Arch:          sudo pacman -S go\n"; \
		printf "       Debian/Ubuntu: sudo apt install golang-go\n"; \
		printf "       Fedora:        sudo dnf install golang\n"; \
		exit 1; \
	fi
	go build -ldflags="-s -w -X main.Version=$(VERSION)" -o $(BINARY) .

install: build
	$(SUDO) cp $(BINARY) $(PREFIX)/$(BINARY)
	@if command -v fish > /dev/null 2>&1; then \
		$(SUDO) mkdir -p $(FISH_COMP) && \
		./$(BINARY) completion fish | $(SUDO) tee $(FISH_COMP)/alps.fish > /dev/null && \
		printf "  $(GREEN) OK $(RESET) fish completion installed\n"; \
	fi
	@if command -v zsh > /dev/null 2>&1; then \
		$(SUDO) mkdir -p $(ZSH_COMP) && \
		./$(BINARY) completion zsh | $(SUDO) tee $(ZSH_COMP)/_alps > /dev/null && \
		printf "  $(GREEN) OK $(RESET) zsh completion installed\n"; \
	fi
	@if command -v bash > /dev/null 2>&1; then \
		$(SUDO) mkdir -p $(BASH_COMP) && \
		./$(BINARY) completion bash | $(SUDO) tee $(BASH_COMP)/alps > /dev/null && \
		printf "  $(GREEN) OK $(RESET) bash completion installed\n"; \
	fi
	@printf "  $(GREEN) OK $(RESET) alps installed\n"

uninstall:
	$(SUDO) rm -f $(PREFIX)/$(BINARY)
	$(SUDO) rm -f $(FISH_COMP)/alps.fish
	$(SUDO) rm -f $(ZSH_COMP)/_alps
	$(SUDO) rm -f $(BASH_COMP)/alps
	@printf "  $(GREEN) OK $(RESET) alps uninstalled\n"

clean:
	rm -f $(BINARY)

.PHONY: build install uninstall clean
