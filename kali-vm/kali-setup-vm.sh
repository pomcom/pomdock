#!/usr/bin/env bash
# Minimal post-install setup for Kali libvirt VM.
# Run this INSIDE the VM as the kali user:
#   bash <(curl -s http://<host-ip>/kali-setup-vm.sh)
# Or copy and run:
#   scp scripts/kali-setup-vm.sh kali@<vm-ip>:~ && ssh kali@<vm-ip> bash kali-setup-vm.sh
set -euo pipefail

echo "→ Installing packages..."
sudo apt-get update -qq
sudo apt-get install -y tmux rofi curl zsh git xrdp

echo "→ Installing atuin..."
curl --proto '=https' --tlsv1.2 -sSf https://setup.atuin.sh | bash

echo "→ Writing ~/.tmux.conf..."
cat > ~/.tmux.conf <<'EOF'
# Prefix: Ctrl-a (easier than Ctrl-b)
unbind C-b
set -g prefix C-a
bind C-a send-prefix

# Mouse support
set -g mouse on

# Vi keys in copy mode
setw -g mode-keys vi
bind -T copy-mode-vi v send -X begin-selection
bind -T copy-mode-vi y send -X copy-pipe-and-cancel "wl-copy 2>/dev/null || xclip -selection clipboard"

# Split panes with | and -
bind | split-window -h -c "#{pane_current_path}"
bind - split-window -v -c "#{pane_current_path}"

# Switch panes with Alt-arrow (no prefix)
bind -n M-Left  select-pane -L
bind -n M-Right select-pane -R
bind -n M-Up    select-pane -U
bind -n M-Down  select-pane -D

# Switch windows with Shift-arrow
bind -n S-Left  previous-window
bind -n S-Right next-window

# Start windows/panes at 1
set -g base-index 1
setw -g pane-base-index 1

# Longer history
set -g history-limit 50000

# Status bar
set -g status-style bg=colour235,fg=colour136
set -g status-left "#[fg=colour166]#S "
set -g status-right "#[fg=colour166]%H:%M"
set -g window-status-current-style fg=colour166,bold
EOF

echo "→ Restoring Kali default .zshrc and appending extras..."
cp /etc/skel/.zshrc ~/.zshrc
cat >> ~/.zshrc <<'EOF'

# atuin
[[ -f "$HOME/.atuin/bin/env" ]] && . "$HOME/.atuin/bin/env"
eval "$(atuin init zsh)"

export PATH="$PATH:$HOME/.local/bin:$HOME/.atuin/bin"
EOF

echo "→ Setting zsh as default shell..."
sudo chsh -s /usr/bin/zsh kali

echo "→ Enabling SSH + xrdp..."
sudo systemctl enable --now ssh xrdp
sudo adduser xrdp ssl-cert 2>/dev/null || true
echo "startxfce4" > ~/.xsessionrc

echo ""
echo "✓ Done! Start a new shell or: exec zsh"
echo "  tmux        — start session"
echo "  Ctrl-a |    — split vertical"
echo "  Ctrl-a -    — split horizontal"
echo "  Alt-arrows  — navigate panes"
