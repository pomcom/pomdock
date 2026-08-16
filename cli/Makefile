.PHONY: build clean install completion-zsh

BINARY := pomdock
PREFIX ?= /usr/local
DESTDIR ?=
SUDO ?= sudo
LIBDIR := $(PREFIX)/lib/pomdock
INSTALL_LIBDIR := $(DESTDIR)$(LIBDIR)
LDFLAGS := -X 'main.installedRoot=$(LIBDIR)'

build:
	go mod tidy
	go build -ldflags "$(LDFLAGS)" -o $(BINARY) .
	@echo "Built: ./$(BINARY)"
	@echo "Usage: ./$(BINARY) --help"

clean:
	rm -f $(BINARY)

install: build
	$(SUDO) install -d $(DESTDIR)$(PREFIX)/bin $(INSTALL_LIBDIR)
	$(SUDO) install -m 755 $(BINARY) $(DESTDIR)$(PREFIX)/bin/$(BINARY)
	$(SUDO) install -m 644 ../Dockerfile $(INSTALL_LIBDIR)/Dockerfile
	$(SUDO) install -m 755 ../pentest.sh ../setup-pentest.sh $(INSTALL_LIBDIR)/
	$(SUDO) install -d $(INSTALL_LIBDIR)/scripts $(INSTALL_LIBDIR)/tor-gateway $(INSTALL_LIBDIR)/kali-vm $(INSTALL_LIBDIR)/vendor/atuin
	$(SUDO) cp -a ../scripts/. $(INSTALL_LIBDIR)/scripts/
	$(SUDO) cp -a ../tor-gateway/. $(INSTALL_LIBDIR)/tor-gateway/
	$(SUDO) cp -a ../kali-vm/. $(INSTALL_LIBDIR)/kali-vm/
	$(SUDO) cp -a ../vendor/atuin/. $(INSTALL_LIBDIR)/vendor/atuin/
	@echo "Installed: $(DESTDIR)$(PREFIX)/bin/$(BINARY)"
	@echo "Resources: $(INSTALL_LIBDIR)"

completion-zsh: build
	@mkdir -p ~/.zfunc
	./$(BINARY) completion zsh > ~/.zfunc/_pomdock
	@echo "Completion installed. Add to ~/.zshrc if not already present:"
	@echo '  fpath=(~/.zfunc $$fpath)'
	@echo '  autoload -U compinit && compinit'
