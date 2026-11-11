.PHONY: build build-go web clean install completion-zsh test-recordings test-build-context

BINARY := pomdock
PREFIX ?= /usr/local
DESTDIR ?=
SUDO ?= sudo
LIBDIR := $(PREFIX)/lib/pomdock
INSTALL_LIBDIR := $(DESTDIR)$(LIBDIR)
SCRIPT_SOURCES := ../scripts/build-image.sh ../scripts/build-atuin.sh ../scripts/stage-atuin.sh \
	../scripts/engagement-meta.py ../scripts/pomdock-shell ../scripts/pomdock-tmux ../scripts/pomsession \
	../scripts/pomdock-help ../scripts/pomrdp ../scripts/pomdock-netcontext ../scripts/pomdock-exitip \
	../scripts/workspace-layout.py ../scripts/update-runtime-image.sh
RECORDING_SOURCES := $(wildcard ../scripts/recording/*.sh ../scripts/recording/*.bash ../scripts/recording/*.zsh)
PROMPT_SOURCES := $(wildcard ../scripts/prompt/*.bash ../scripts/prompt/*.zsh ../scripts/prompt/*.py)
VERSION ?= $(shell git describe --tags --dirty 2>/dev/null || cat ../VERSION 2>/dev/null || echo dev)
LDFLAGS := -X 'main.installedRoot=$(LIBDIR)' -X 'main.version=$(VERSION)'

build: web
	$(MAKE) build-go

# Go only, reusing the last web/dist (fast iteration on backend code).
build-go:
	go mod tidy
	go build -ldflags "$(LDFLAGS)" -o $(BINARY) .
	@echo "Built: ./$(BINARY)"
	@echo "Usage: ./$(BINARY) --help"

# The Svelte UI is embedded from web/dist; Node is needed at build time only.
web:
	cd web && npm ci && npm run build

clean:
	rm -f $(BINARY)
	rm -rf web/dist/assets web/dist/index.html web/node_modules

install: build
	$(SUDO) install -d $(DESTDIR)$(PREFIX)/bin $(INSTALL_LIBDIR)
	$(SUDO) install -m 755 $(BINARY) $(DESTDIR)$(PREFIX)/bin/$(BINARY)
	$(SUDO) install -m 644 ../Dockerfile $(INSTALL_LIBDIR)/Dockerfile
	$(SUDO) install -m 755 ../pentest.sh ../setup-pentest.sh $(INSTALL_LIBDIR)/
	$(SUDO) install -d $(INSTALL_LIBDIR)/scripts/recording $(INSTALL_LIBDIR)/scripts/prompt $(INSTALL_LIBDIR)/tor-gateway $(INSTALL_LIBDIR)/kali-vm $(INSTALL_LIBDIR)/vm-profiles $(INSTALL_LIBDIR)/vendor/atuin
	$(SUDO) install -m 755 $(SCRIPT_SOURCES) $(INSTALL_LIBDIR)/scripts/
	$(SUDO) install -m 644 $(RECORDING_SOURCES) $(INSTALL_LIBDIR)/scripts/recording/
	$(SUDO) install -m 644 $(PROMPT_SOURCES) $(INSTALL_LIBDIR)/scripts/prompt/
	$(SUDO) cp -a ../tor-gateway/. $(INSTALL_LIBDIR)/tor-gateway/
	$(SUDO) cp -a ../kali-vm/. $(INSTALL_LIBDIR)/kali-vm/
	$(SUDO) cp -a ../vm-profiles/. $(INSTALL_LIBDIR)/vm-profiles/
	$(SUDO) bash ../scripts/stage-atuin.sh ../vendor/atuin $(INSTALL_LIBDIR)/vendor/atuin
	@echo "Installed: $(DESTDIR)$(PREFIX)/bin/$(BINARY)"
	@echo "Resources: $(INSTALL_LIBDIR)"

completion-zsh: build
	@mkdir -p ~/.zfunc
	./$(BINARY) completion zsh > ~/.zfunc/_pomdock
	@echo "Completion installed. Add to ~/.zshrc if not already present:"
	@echo '  fpath=(~/.zfunc $$fpath)'
	@echo '  autoload -U compinit && compinit'

# Optional pytest is development-only; terminal-processing dependencies are bundled.
test-recordings:
	PYTHONPATH=recording_engine PYTHONDONTWRITEBYTECODE=1 python3 -m pytest recording_tests -q -p no:cacheprovider

# Standard-library tests exercise both Docker builder paths without a daemon.
test-build-context:
	PYTHONDONTWRITEBYTECODE=1 python3 -m unittest discover -s ../scripts/tests -p 'test_*.py'
