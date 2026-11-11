ARG TOOLSET=full
FROM rust:slim AS atuin-builder

WORKDIR /src/atuin
COPY vendor/atuin/ ./
RUN apt-get update \
    && apt-get install -y --no-install-recommends \
        build-essential \
        ca-certificates \
        clang \
        cmake \
        libssl-dev \
        pkg-config \
    && rm -rf /var/lib/apt/lists/* \
    && cargo build --release --locked -p atuin

FROM golang:1.22-bookworm AS jsluice-builder

ARG TOOLSET
RUN mkdir -p /go/bin \
    && if [ "$TOOLSET" = full ]; then \
        CGO_ENABLED=1 go install github.com/BishopFox/jsluice/cmd/jsluice@v0.0.0-20240110145140-0ddfab153e06; \
    fi

FROM kalilinux/kali-rolling

ARG TOOLSET
ARG TARGETARCH
LABEL io.pomdock.atuin.variant="absolute-datetime"
LABEL io.pomdock.toolset="$TOOLSET" io.pomdock.recording-format="3"
LABEL io.pomdock.workspace="/home/kali/workspace" io.pomdock.prompt="local-tunnels-v1" io.pomdock.container-tmux="1"

ENV DEBIAN_FRONTEND=noninteractive
ENV LANG=en_US.UTF-8
ENV LANGUAGE=en_US:en
ENV LC_ALL=en_US.UTF-8

# ── Base shell + build deps ───────────────────────────────────────
# Apt pentest tools are installed here as root (avoids sudo issues in setup-pentest.sh).
# Go/bin/pip tools are installed by setup-pentest.sh as the kali user.
# Provider VPNs use gluetun; lab OpenVPN profiles can run inside the container.
COPY setup-pentest.sh /tmp/setup-pentest.sh
RUN printf 'http://eu.mirror.ionos.com/linux/distributions/kali/kali/\nhttp://mirror.pyratelan.org/kali/\n' \
        > /tmp/kali-mirrors \
    && echo "deb mirror+file:/tmp/kali-mirrors kali-rolling main non-free contrib" \
        > /etc/apt/sources.list \
    && apt-get update \
    && case "$TOOLSET" in full|minimal) ;; *) echo 'Invalid TOOLSET' >&2; exit 2 ;; esac \
    && if [ "$TOOLSET" = full ]; then bash /tmp/setup-pentest.sh --apt-only; fi \
    && apt-get install -y --fix-missing \
        zsh tmux git curl fzf python3 ca-certificates locales passwd sudo util-linux procps less \
        openvpn iproute2 \
    && if [ "$TOOLSET" = full ]; then \
        apt-get install -y --fix-missing wget bat eza python3-pip pipx golang-go bind9-dnsutils unzip; \
    fi \
    && printf 'en_US.UTF-8 UTF-8\n' > /etc/locale.gen \
    && locale-gen en_US.UTF-8 \
    && update-locale LANG=en_US.UTF-8 LANGUAGE=en_US:en LC_ALL=en_US.UTF-8 \
    && ln -sf /usr/share/zoneinfo/Europe/Berlin /etc/localtime \
    && echo "Europe/Berlin" > /etc/timezone \
    && rm -rf /var/lib/apt/lists/*

# ── Neovim (latest binary release) ───────────────────────────────
RUN if [ "$TOOLSET" = full ]; then \
        case "${TARGETARCH:-$(dpkg --print-architecture)}" in \
            amd64) nvim_arch=x86_64 ;; arm64) nvim_arch=arm64 ;; \
            *) echo 'Neovim requires amd64 or arm64' >&2; exit 1 ;; \
        esac \
        && curl -fLsS "https://github.com/neovim/neovim/releases/latest/download/nvim-linux-${nvim_arch}.tar.gz" -o /tmp/nvim.tar.gz \
        && tar xzf /tmp/nvim.tar.gz -C /opt \
        && ln -s "/opt/nvim-linux-${nvim_arch}/bin/nvim" /usr/local/bin/nvim \
        && rm /tmp/nvim.tar.gz; \
    fi

# ── Non-root user ─────────────────────────────────────────────────
ARG USERNAME=kali
RUN useradd -m -s /bin/zsh -G sudo "$USERNAME" \
    && echo "$USERNAME ALL=(ALL) NOPASSWD:ALL" >> /etc/sudoers

# Pomdock-owned shell entrypoint and session marker helpers. Keeping these out of
# the user's dotfiles makes recording work with any zsh configuration.
COPY scripts/pomdock-shell scripts/pomdock-tmux scripts/pomsession scripts/pomdock-help scripts/pomrdp scripts/pomdock-netcontext scripts/pomdock-exitip /usr/local/bin/
RUN chmod 0755 /usr/local/bin/pomdock-shell /usr/local/bin/pomdock-tmux /usr/local/bin/pomsession /usr/local/bin/pomdock-help /usr/local/bin/pomrdp /usr/local/bin/pomdock-netcontext /usr/local/bin/pomdock-exitip \
    && chmod 0644 /tmp/setup-pentest.sh \
    && ln -s pomsession /usr/local/bin/pdsession \
    && ln -s pomdock-help /usr/local/bin/help \
    && ln -s pomdock-help /usr/local/bin/pomhelp

COPY scripts/recording/ /usr/local/lib/pomdock/recording/
COPY scripts/prompt/ /usr/local/lib/pomdock/prompt/
COPY scripts/workspace-layout.py /usr/local/lib/pomdock/workspace-layout.py
RUN chmod -R a+rX /usr/local/lib/pomdock/recording \
 && chmod -R a+rX /usr/local/lib/pomdock/prompt \
 && chmod 0644 /usr/local/lib/pomdock/workspace-layout.py \
 && python3 /usr/local/lib/pomdock/workspace-layout.py "$USERNAME" \
 && printf '\n. /usr/local/lib/pomdock/recording/session-log.sh\n. /usr/local/lib/pomdock/recording/app-history.sh\n. /usr/local/lib/pomdock/recording/atuin-init.zsh\n. /usr/local/lib/pomdock/recording/cmdlog.zsh\n. /usr/local/lib/pomdock/prompt/prompt.zsh\n' >> /etc/zsh/zshrc \
 && printf '\n. /usr/local/lib/pomdock/recording/session-log.sh\n. /usr/local/lib/pomdock/recording/app-history.sh\n. /usr/local/lib/pomdock/recording/shell-help.sh\n. /usr/local/lib/pomdock/recording/atuin-init.bash\n. /usr/local/lib/pomdock/recording/cmdlog.bash\n. /usr/local/lib/pomdock/prompt/prompt.bash\n' >> /etc/bash.bashrc

USER $USERNAME
WORKDIR /home/$USERNAME

# Silence Kali's "minimal installation" MOTD (kali-motd honours ~/.hushlogin);
# a disposable pentest shell does not need the notice on every launch.
RUN touch /home/$USERNAME/.hushlogin

ENV GOPATH=/home/$USERNAME/go
ENV PATH=/home/$USERNAME/.atuin/bin:/home/$USERNAME/go/bin:/home/$USERNAME/.local/bin:/home/$USERNAME/.cargo/bin:$PATH

# ── Dotfiles (staged by scripts/build-image.sh) ───────────────────────
# Mounted at runtime to the same path — see pentest.sh (PENTEST_DOTFILES_DIR).
COPY --chown=$USERNAME:$USERNAME .pomdock-dotfiles/ /home/$USERNAME/dotfiles/
RUN python3 /usr/local/lib/pomdock/prompt/guard-dotfiles.py /home/$USERNAME/dotfiles/zsh/.zshrc

# ── Vendored patched atuin build ───────────────────────────────────
# Built from the vendored workspace in ./atuin so the container does not rely
# on an untracked local binary.
COPY --from=atuin-builder --chown=$USERNAME:$USERNAME /src/atuin/target/release/atuin /home/$USERNAME/.atuin/bin/atuin
RUN chmod 0755 /home/$USERNAME/.atuin/bin/atuin

# jsluice uses tree-sitter through CGO, so build it in a compiler image.
# setup-pentest.sh checks GOPATH/bin and skips rebuilding it in the Kali stage.
# Create writable parents as Kali; COPY --chown applies to copied entries but
# can otherwise create root-owned intermediate directories such as ~/go.
RUN mkdir -p /home/$USERNAME/go/bin /home/$USERNAME/.local/bin /home/$USERNAME/.cargo/bin
COPY --from=jsluice-builder --chown=$USERNAME:$USERNAME /go/bin/ /home/$USERNAME/go/bin/

# ── Pentest tools — single source of truth: setup-pentest.sh ──────
# Keep only the built Go binaries; drop the Go build/module and pip caches
# (several GB) that the tool builds leave behind in this layer.
RUN if [ "$TOOLSET" = full ]; then bash /tmp/setup-pentest.sh && go clean -cache -modcache; fi \
    && rm -rf /home/$USERNAME/.cache/pip /home/$USERNAME/.cache/go-build

# ── Shell setup (optional — runs setup-shell.sh if present in dotfiles) ──────
RUN mkdir -p /home/$USERNAME/.atuin/bin \
    && printf 'export PATH="$HOME/.atuin/bin:$PATH"\n' > /home/$USERNAME/.atuin/bin/env \
    && if [ "$TOOLSET" = full ] && [ -f /home/$USERNAME/dotfiles/setup-shell.sh ]; then \
           bash /home/$USERNAME/dotfiles/setup-shell.sh; \
       fi \
    && printf '# Recon Notes\n\n## Scope\n- Target:\n- Rules of engagement:\n\n## Hosts\n- \n\n## Findings\n- \n\n## Credentials\n- \n\n## Next Steps\n- \n' \
        > /home/$USERNAME/recon.md \
    && mkdir -p /home/$USERNAME/.config \
    && if [ -d /home/$USERNAME/dotfiles/atuin/.config/atuin ]; then \
           rm -rf /home/$USERNAME/.config/atuin \
           && ln -s /home/$USERNAME/dotfiles/atuin/.config/atuin /home/$USERNAME/.config/atuin; \
       else \
           mkdir -p /home/$USERNAME/.config/atuin \
           && printf 'auto_sync = false\nupdate_check = false\n' > /home/$USERNAME/.config/atuin/config.toml; \
       fi

WORKDIR /home/$USERNAME/workspace
CMD ["zsh"]
