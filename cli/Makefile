.PHONY: build clean install completion-zsh

BINARY := pomdock

build:
	go mod tidy
	go build -o $(BINARY) .
	@echo "Built: ./$(BINARY)"
	@echo "Usage: ./$(BINARY) --help"

clean:
	rm -f $(BINARY)

install: build
	sudo install -m 755 $(BINARY) /usr/local/bin/$(BINARY)
	@echo "Installed: /usr/local/bin/$(BINARY)"

completion-zsh: build
	@mkdir -p ~/.zfunc
	./$(BINARY) completion zsh > ~/.zfunc/_pomdock
	@echo "Completion installed. Add to ~/.zshrc if not already present:"
	@echo '  fpath=(~/.zfunc $$fpath)'
	@echo '  autoload -U compinit && compinit'
