BINARY = alps
PREFIX = /usr/local/bin
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")

GO := $(shell command -v go 2>/dev/null)

build:
	@if [ -z "$(GO)" ]; then \
		echo "✗ Go is not installed."; \
		echo "  Install it with your package manager:"; \
		echo "    Arch:          sudo pacman -S go"; \
		echo "    Debian/Ubuntu: sudo apt install golang-go"; \
		echo "    Fedora:        sudo dnf install golang"; \
		exit 1; \
	fi
	go build -ldflags="-s -w -X main.Version=$(VERSION)" -o alps .

install: build
	sudo cp $(BINARY) $(PREFIX)/$(BINARY)
	@if command -v fish > /dev/null 2>&1; then \
		mkdir -p ~/.config/fish/completions && \
		./$(BINARY) completion fish > ~/.config/fish/completions/alps.fish && \
		echo "  ✓ fish completion installed"; \
	fi
	@if command -v zsh > /dev/null 2>&1; then \
		sudo mkdir -p /usr/local/share/zsh/site-functions && \
		./$(BINARY) completion zsh | sudo tee /usr/local/share/zsh/site-functions/_alps > /dev/null && \
		echo "  ✓ zsh completion installed"; \
	fi
	@if command -v bash > /dev/null 2>&1; then \
		./$(BINARY) completion bash | sudo tee /etc/bash_completion.d/alps > /dev/null && \
		echo "  ✓ bash completion installed"; \
	fi
	@echo "  ✓ alps installed"

uninstall:
	sudo rm -f $(PREFIX)/$(BINARY)
	rm -f ~/.config/fish/completions/alps.fish
	sudo rm -f /usr/local/share/zsh/site-functions/_alps
	sudo rm -f /etc/bash_completion.d/alps
	@echo "  ✓ alps uninstalled"

clean:
	rm -f $(BINARY)

.PHONY: build install uninstall clean
