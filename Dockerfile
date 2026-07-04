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

FROM kalilinux/kali-rolling

ENV DEBIAN_FRONTEND=noninteractive
ENV LANG=en_US.UTF-8
ENV LANGUAGE=en_US:en
ENV LC_ALL=en_US.UTF-8

# ── Base shell + build deps ───────────────────────────────────────
# Apt pentest tools are installed here as root (avoids sudo issues in setup-pentest.sh).
# Go/bin/pip tools are installed by setup-pentest.sh as the kali user.
# VPN is handled by a gluetun sidecar, not inside this container.
COPY setup-pentest.sh /tmp/setup-pentest.sh
RUN printf 'http://eu.mirror.ionos.com/linux/distributions/kali/kali/\nhttp://mirror.pyratelan.org/kali/\n' \
        > /tmp/kali-mirrors \
    && echo "deb mirror+file:/tmp/kali-mirrors kali-rolling main non-free contrib" \
        > /etc/apt/sources.list \
    && apt-get update \
    && bash /tmp/setup-pentest.sh --apt-only \
    && apt-get install -y --fix-missing \
        zsh tmux git curl wget fzf bat eza \
        python3 python3-pip pipx \
        golang-go \
        bind9-dnsutils \
        unzip ca-certificates locales passwd sudo \
    && printf 'en_US.UTF-8 UTF-8\n' > /etc/locale.gen \
    && locale-gen en_US.UTF-8 \
    && update-locale LANG=en_US.UTF-8 LANGUAGE=en_US:en LC_ALL=en_US.UTF-8 \
    && ln -sf /usr/share/zoneinfo/Europe/Berlin /etc/localtime \
    && echo "Europe/Berlin" > /etc/timezone \
    && rm -rf /var/lib/apt/lists/*

# ── Neovim (latest binary release) ───────────────────────────────
RUN curl -sL https://github.com/neovim/neovim/releases/latest/download/nvim-linux-x86_64.tar.gz \
    | tar xz -C /opt \
    && ln -s /opt/nvim-linux-x86_64/bin/nvim /usr/local/bin/nvim

# ── Non-root user ─────────────────────────────────────────────────
ARG USERNAME=kali
RUN useradd -m -s /bin/zsh -G sudo "$USERNAME" \
    && echo "$USERNAME ALL=(ALL) NOPASSWD:ALL" >> /etc/sudoers

USER $USERNAME
WORKDIR /home/$USERNAME

ENV GOPATH=/home/$USERNAME/go
ENV PATH=/home/$USERNAME/go/bin:/home/$USERNAME/.local/bin:/home/$USERNAME/.cargo/bin:$PATH
ENV STARSHIP_CONFIG=/home/$USERNAME/dotfiles/starship/.config/starship-pentest.toml

# ── Dotfiles (pass --build-context dotfiles=~/your-dotfiles-dir) ──────
# Mounted at runtime to the same path — see pentest.sh (PENTEST_DOTFILES_DIR).
COPY --from=dotfiles --chown=$USERNAME:$USERNAME . /home/$USERNAME/dotfiles/

# ── Vendored patched atuin build ───────────────────────────────────
# Built from the vendored workspace in ./atuin so the container does not rely
# on an untracked local binary.
COPY --from=atuin-builder --chown=$USERNAME:$USERNAME --chmod=0755 /src/atuin/target/release/atuin /home/$USERNAME/.atuin/bin/atuin

# ── Pentest tools — single source of truth: setup-pentest.sh ──────
RUN bash /tmp/setup-pentest.sh

# ── Shell setup (optional — runs setup-shell.sh if present in dotfiles) ──────
RUN mkdir -p /home/$USERNAME/.atuin/bin \
    && printf 'export PATH="$HOME/.atuin/bin:$PATH"\n' > /home/$USERNAME/.atuin/bin/env \
    && if [ -f /home/$USERNAME/dotfiles/setup-shell.sh ]; then \
           bash /home/$USERNAME/dotfiles/setup-shell.sh; \
       fi \
    && printf '# Recon Notes\n\n## Scope\n- Target:\n- Rules of engagement:\n\n## Hosts\n- \n\n## Findings\n- \n\n## Credentials\n- \n\n## Next Steps\n- \n' \
        > /home/$USERNAME/recon.md \
    && mkdir -p /home/$USERNAME/.config \
    && if [ -d /home/$USERNAME/dotfiles/atuin/.config/atuin ]; then \
           rm -rf /home/$USERNAME/.config/atuin \
           && ln -s /home/$USERNAME/dotfiles/atuin/.config/atuin /home/$USERNAME/.config/atuin; \
       fi

CMD ["zsh"]
