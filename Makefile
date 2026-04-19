BINARY = alps
PREFIX = /usr/local/bin

build:
	go build -o $(BINARY) .

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
