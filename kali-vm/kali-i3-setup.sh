#!/usr/bin/env bash
# Full Kali pentest environment — i3 + XFCE4 + pentest tools
# Run inside VM as kali user:
#   scp tools/kali-vm/kali-i3-setup.sh kali@<ip>:~ && ssh kali@<ip> bash kali-i3-setup.sh
set -euo pipefail

# ── Packages ──────────────────────────────────────────────────────────────────

echo "→ Updating package lists..."
sudo apt-get update -qq

# Passwordless sudo for kali user — needed for automated provisioning and pomdock scripts
echo 'kali ALL=(ALL) NOPASSWD:ALL' | sudo tee /etc/sudoers.d/kali-nopasswd
sudo chmod 440 /etc/sudoers.d/kali-nopasswd

echo "→ Installing i3, XFCE4, and base packages..."
sudo apt-get install -y \
    i3 i3status rofi picom feh \
    xfce4 thunar xfce4-terminal xfce4-power-manager \
    alacritty \
    fonts-firacode \
    xrdp openssh-server \
    tmux zsh curl git wget jq net-tools dnsutils whois wireguard-tools openvpn openresolv \
    python3-pip python3-venv pipx \
    build-essential libssl-dev libffi-dev \
    nmap gobuster feroxbuster nikto smbclient \
    enum4linux onesixtyone ldap-utils \
    wordlists \
    zsh-syntax-highlighting zsh-autosuggestions

# ── Mullvad VPN ───────────────────────────────────────────────────────────────

echo "→ Installing Mullvad VPN..."
if ! command -v mullvad &>/dev/null; then
    curl -fsSL https://repository.mullvad.net/deb/mullvad-keyring.asc \
        | sudo gpg --dearmor -o /usr/share/keyrings/mullvad-keyring.gpg
    echo "deb [signed-by=/usr/share/keyrings/mullvad-keyring.gpg arch=$(dpkg --print-architecture)] https://repository.mullvad.net/deb/stable $(lsb_release -cs) main" \
        | sudo tee /etc/apt/sources.list.d/mullvad.list
    sudo apt-get update -qq
    sudo apt-get install -y mullvad-vpn
fi

# ── Sublime Text ──────────────────────────────────────────────────────────────

echo "→ Installing Sublime Text..."
if ! command -v subl &>/dev/null; then
    wget -qO - https://download.sublimetext.com/sublimehq-pub.gpg \
        | sudo gpg --dearmor -o /etc/apt/trusted.gpg.d/sublimehq-archive.gpg
    echo "deb https://download.sublimetext.com/ apt/stable/" \
        | sudo tee /etc/apt/sources.list.d/sublime-text.list
    sudo apt-get update -qq
    sudo apt-get install -y sublime-text
fi

# ── Golang ────────────────────────────────────────────────────────────────────

echo "→ Installing Go..."
if ! command -v go &>/dev/null; then
    sudo apt-get install -y golang-go
fi

# ── Atuin ─────────────────────────────────────────────────────────────────────

echo "→ Installing atuin..."
if ! command -v atuin &>/dev/null; then
    if ! sudo apt-get install -y atuin; then
        # Fallback installer, kept non-interactive.
        curl --proto '=https' --tlsv1.2 -sSf https://setup.atuin.sh | bash -s -- --yes
    fi
fi

# ── Penelope ──────────────────────────────────────────────────────────────────

echo "→ Installing Penelope (reverse shell handler)..."
pipx install penelope-shell-handler 2>/dev/null || pip3 install --user penelope-shell-handler

# ── autotiling ────────────────────────────────────────────────────────────────

echo "→ Installing autotiling..."
pipx install autotiling 2>/dev/null || pip3 install --user autotiling

# ── AutoRecon dependencies ────────────────────────────────────────────────────

echo "→ Installing AutoRecon apt dependencies..."
sudo apt-get install -y \
    seclists curl dnsrecon enum4linux feroxbuster gobuster \
    impacket-scripts nbtscan nikto nmap onesixtyone oscanner \
    redis-tools smbclient smbmap snmp sslscan sipvicious \
    tnscmd10g whatweb

# ── AutoRecon ─────────────────────────────────────────────────────────────────

echo "→ Installing AutoRecon..."
pipx install git+https://github.com/Tib3rius/AutoRecon.git 2>/dev/null \
    || pip3 install --user git+https://github.com/Tib3rius/AutoRecon.git

# ── fzf + zoxide + syncthing ──────────────────────────────────────────────────

echo "→ Installing fzf, zoxide, syncthing, starship, rlwrap, xclip..."
sudo apt-get install -y fzf zoxide syncthing rlwrap xclip
curl -sS https://starship.rs/install.sh | sh -s -- --yes

echo "→ Installing goshs..."
sudo apt-get install -y goshs

# ── PATH additions ────────────────────────────────────────────────────────────

SHELL_RC="$HOME/.zshrc"
touch "$SHELL_RC"
sed -i '/# >>> kali-i3 managed >>>/,/# <<< kali-i3 managed <<</d' "$SHELL_RC"
cat >> "$SHELL_RC" <<'SH'

# >>> kali-i3 managed >>>
export PATH="$PATH:$HOME/.local/bin:$HOME/.atuin/bin"

[[ -f "$HOME/.atuin/bin/env" ]] && . "$HOME/.atuin/bin/env"
eval "$(atuin init zsh 2>/dev/null)"

# Keep fzf completion, but history search stays on atuin.
[ -f /usr/share/doc/fzf/examples/completion.zsh ] \
    && source /usr/share/doc/fzf/examples/completion.zsh

eval "$(zoxide init --cmd z zsh 2>/dev/null || true)"
command -v z >/dev/null 2>&1 || alias z='cd'

alias pcm.serve='goshs -p 8888'
alias pbcopy='xclip -selection clipboard'
alias pbpaste='xclip -selection clipboard -o'
alias nc='rlwrap nc'

# Force starship prompt in zsh sessions.
eval "$(starship init zsh)"

# zsh-syntax-highlighting — colors commands as you type (known=green, unknown=red)
[ -f /usr/share/zsh-syntax-highlighting/zsh-syntax-highlighting.zsh ] \
    && source /usr/share/zsh-syntax-highlighting/zsh-syntax-highlighting.zsh

# zsh-autosuggestions — ghost-text completion from history (→ or Ctrl+F to accept)
[ -f /usr/share/zsh-autosuggestions/zsh-autosuggestions.zsh ] \
    && source /usr/share/zsh-autosuggestions/zsh-autosuggestions.zsh
# <<< kali-i3 managed <<<
SH

# ── SSH key ───────────────────────────────────────────────────────────────────

echo "→ Skipping SSH key injection — add your own key to ~/.ssh/authorized_keys"
mkdir -p ~/.ssh
chmod 700 ~/.ssh

# ── Services ──────────────────────────────────────────────────────────────────

echo "→ Enabling SSH + xrdp..."
sudo systemctl enable --now ssh
sudo systemctl enable --now xrdp
sudo adduser xrdp ssl-cert 2>/dev/null || true

# ── tmux config ───────────────────────────────────────────────────────────────

echo "→ Writing ~/.tmux.conf..."
cat > ~/.tmux.conf <<'EOF'
# Sane defaults
set -g mouse on
set -g mode-keys vi
set -g set-clipboard on
set -g history-limit 50000
set -g base-index 1
setw -g pane-base-index 1
set -g renumber-windows on
set -sg escape-time 0

# vi copy mode: v=select, y=copy (OSC 52 via set-clipboard)
bind -T copy-mode-vi v send -X begin-selection
bind -T copy-mode-vi y send -X copy-selection-and-cancel
bind -T copy-mode-vi MouseDragEnd1Pane send -X copy-selection-and-cancel

# Splits: C-b | and C-b -
bind | split-window -h -c "#{pane_current_path}"
bind - split-window -v -c "#{pane_current_path}"

# Pane nav: Alt+arrow (no prefix)
bind -n M-Left  select-pane -L
bind -n M-Right select-pane -R
bind -n M-Up    select-pane -U
bind -n M-Down  select-pane -D

# Window nav: Shift+arrow
bind -n S-Left  previous-window
bind -n S-Right next-window

# Status bar
set -g status-style bg=#1a1a1a,fg=#cccccc
set -g status-left "#[fg=#1f5c3a,bold] #S "
set -g status-right "#[fg=#aaaaaa] %H:%M "
set -g window-status-style fg=#888888
set -g window-status-current-style fg=#ffffff,bold,bg=#0b3d2e
set -g window-status-format " #I:#W "
set -g window-status-current-format " #I:#W "
EOF

# ── alacritty config ──────────────────────────────────────────────────────────

echo "→ Writing ~/.config/alacritty/alacritty.toml..."
mkdir -p ~/.config/alacritty
cat > ~/.config/alacritty/alacritty.toml <<'EOF'
[env]
TERM = "xterm-256color"

[window]
dynamic_padding = true
decorations = "full"
opacity = 1.0
title = "Alacritty"

[window.padding]
x = 6
y = 6

[scrolling]
history = 10000
multiplier = 3

[font]
size = 13.5

[font.normal]
family = "Fira Code"
style = "Regular"

[font.bold]
family = "Fira Code"
style = "Medium"

[selection]
save_to_clipboard = true

[cursor]
style = { shape = "Underline" }
unfocused_hollow = true

[mouse]
hide_when_typing = true

[colors.primary]
background = "#000000"
foreground = "#D8DEE9"

[colors.normal]
black   = "#3B4252"
red     = "#BF616A"
green   = "#A3BE8C"
yellow  = "#EBCB8B"
blue    = "#81A1C1"
magenta = "#B48EAD"
cyan    = "#88C0D0"
white   = "#E5E9F0"

[colors.bright]
black   = "#4C566A"
red     = "#BF616A"
green   = "#A3BE8C"
yellow  = "#EBCB8B"
blue    = "#81A1C1"
magenta = "#B48EAD"
cyan    = "#8FBCBB"
white   = "#ECEFF4"

[[keyboard.bindings]]
key = "V"
mods = "Control|Shift"
action = "Paste"

[[keyboard.bindings]]
key = "C"
mods = "Control|Shift"
action = "Copy"

[[keyboard.bindings]]
key = "Equal"
mods = "Control"
action = "IncreaseFontSize"

[[keyboard.bindings]]
key = "Minus"
mods = "Control"
action = "DecreaseFontSize"

[[keyboard.bindings]]
key = "Key0"
mods = "Control"
action = "ResetFontSize"
EOF

# ── starship config ───────────────────────────────────────────────────────────

echo "→ Writing ~/.config/starship.toml..."
mkdir -p ~/.config
cat > ~/.config/starship.toml <<'EOF'
add_newline = true
command_timeout = 1000

format = """
$username\
$hostname\
$directory\
$git_branch\
$git_status\
$git_state\
$python\
$golang\
$cmd_duration\
$line_break\
$character"""

[package]
disabled = true

[gcloud]
disabled = true

[azure]
disabled = true

[kubernetes]
disabled = true

[docker_context]
disabled = true

[aws]
disabled = true

[python]
detect_extensions = ["py"]
format = 'via [${symbol}${pyenv_prefix}(${version} )(\($virtualenv\) )]($style)'

[golang]
format = "via [⚙ $version](bold cyan) "

[git_branch]
format = "on [$symbol$branch]($style) "
symbol = "🌱 "
style = "bold purple"

[git_status]
format = '([\[$all_status$ahead_behind\]]($style))'
style = "bold red"

[directory]
format = "in [$path]($style)[$read_only]($read_only_style) "
style = "bold cyan"
truncation_length = 3
truncate_to_repo = true

[cmd_duration]
min_time = 2_000
format = "took [$duration]($style) "
style = "bold yellow"

[character]
success_symbol = "[❯](bold green)"
error_symbol = "[❯](bold red)"
EOF

# ── rofi tmux session picker ──────────────────────────────────────────────────

echo "→ Writing ~/.local/bin/rofi-tmux..."
mkdir -p ~/.local/bin
cat > ~/.local/bin/rofi-tmux <<'EOF'
#!/usr/bin/env bash
# Rofi tmux session picker — Alt+T in i3

NEW="  + New session..."

if tmux list-sessions &>/dev/null 2>&1; then
    sessions=$(tmux list-sessions -F "#{session_name}   (#{session_windows} win, attached: #{?session_attached,yes,no})")
else
    sessions=""
fi

menu="$NEW"
[ -n "$sessions" ] && menu="${menu}\n${sessions}"

selected=$(printf "%b" "$menu" | rofi -dmenu -i -p "Tmux:" -width 60)
[ -z "$selected" ] && exit 0

if [ "$selected" = "$NEW" ]; then
    name=$(printf "" | rofi -dmenu -i -p "Session name:" -width 40)
    [ -z "$name" ] && exit 0
    alacritty -e tmux new-session -s "$name" &
else
    name=$(echo "$selected" | awk '{print $1}')
    alacritty -e tmux attach-session -t "$name" &
fi
EOF
chmod +x ~/.local/bin/rofi-tmux

# ── Clean up default home dirs ────────────────────────────────────────────────

echo "→ Removing default home directories..."
rm -rf ~/Desktop ~/Documents ~/Downloads ~/Music ~/Pictures ~/Public ~/Templates ~/Videos

# ── i3 config ─────────────────────────────────────────────────────────────────

echo "→ Writing i3 session entry..."
echo "exec i3" > ~/.xsessionrc

echo "→ Writing ~/.config/i3/config..."
mkdir -p ~/.config/i3
cat > ~/.config/i3/config <<'EOF'
# ── Variables ──────────────────────────────────────────────────────────────
set $mod Mod1
set $left h
set $down j
set $up k
set $right l
set $term alacritty
set $menu rofi -show drun -show-icons

font pango:JetBrains Mono 10

# ── Autostart ──────────────────────────────────────────────────────────────
exec --no-startup-id xfsettingsd --daemon
exec --no-startup-id xfce4-power-manager
exec --no-startup-id picom --daemon
exec --no-startup-id autotiling
exec_always --no-startup-id feh --bg-solid '#0b0b0b' 2>/dev/null || true

# ── Key bindings ───────────────────────────────────────────────────────────
bindsym $mod+Return exec $term
bindsym $mod+q kill
bindsym $mod+space exec $menu
bindsym $mod+m exec rofi -show window -show-icons
bindsym $mod+t exec ~/.local/bin/rofi-tmux
bindsym $mod+e exec thunar
bindsym $mod+f fullscreen toggle
bindsym $mod+v floating toggle
bindsym $mod+Shift+r restart
bindsym $mod+Shift+e exec i3-msg exit

# ── Focus — vim keys + arrows ──────────────────────────────────────────────
bindsym $mod+$left  focus left
bindsym $mod+$down  focus down
bindsym $mod+$up    focus up
bindsym $mod+$right focus right
bindsym $mod+Left   focus left
bindsym $mod+Down   focus down
bindsym $mod+Up     focus up
bindsym $mod+Right  focus right

# ── Move windows ───────────────────────────────────────────────────────────
bindsym $mod+Shift+$left  move left
bindsym $mod+Shift+$down  move down
bindsym $mod+Shift+$up    move up
bindsym $mod+Shift+$right move right
bindsym $mod+Shift+Left   move left
bindsym $mod+Shift+Down   move down
bindsym $mod+Shift+Up     move up
bindsym $mod+Shift+Right  move right

# ── Resize ─────────────────────────────────────────────────────────────────
mode "resize" {
    bindsym $left  resize shrink width  15px
    bindsym $right resize grow   width  15px
    bindsym $up    resize shrink height 15px
    bindsym $down  resize grow   height 15px
    bindsym Left   resize shrink width  15px
    bindsym Right  resize grow   width  15px
    bindsym Up     resize shrink height 15px
    bindsym Down   resize grow   height 15px
    bindsym Return mode "default"
    bindsym Escape mode "default"
}
bindsym $mod+r mode "resize"

# ── Layout ─────────────────────────────────────────────────────────────────
bindsym $mod+s layout stacking
bindsym $mod+w layout tabbed
bindsym $mod+Shift+t layout toggle split
bindsym $mod+period workspace next
bindsym $mod+comma  workspace prev

# ── Workspaces 1–9 ─────────────────────────────────────────────────────────
bindsym $mod+1 workspace number 1
bindsym $mod+2 workspace number 2
bindsym $mod+3 workspace number 3
bindsym $mod+4 workspace number 4
bindsym $mod+5 workspace number 5
bindsym $mod+6 workspace number 6
bindsym $mod+7 workspace number 7
bindsym $mod+8 workspace number 8
bindsym $mod+9 workspace number 9

# Move + follow to workspace (like Hyprland Ctrl+Shift)
bindsym $mod+Ctrl+1 move container to workspace number 1; workspace number 1
bindsym $mod+Ctrl+2 move container to workspace number 2; workspace number 2
bindsym $mod+Ctrl+3 move container to workspace number 3; workspace number 3
bindsym $mod+Ctrl+4 move container to workspace number 4; workspace number 4
bindsym $mod+Ctrl+5 move container to workspace number 5; workspace number 5
bindsym $mod+Ctrl+6 move container to workspace number 6; workspace number 6
bindsym $mod+Ctrl+7 move container to workspace number 7; workspace number 7
bindsym $mod+Ctrl+8 move container to workspace number 8; workspace number 8
bindsym $mod+Ctrl+9 move container to workspace number 9; workspace number 9

# Move without following
bindsym $mod+Shift+1 move container to workspace number 1
bindsym $mod+Shift+2 move container to workspace number 2
bindsym $mod+Shift+3 move container to workspace number 3
bindsym $mod+Shift+4 move container to workspace number 4
bindsym $mod+Shift+5 move container to workspace number 5
bindsym $mod+Shift+6 move container to workspace number 6
bindsym $mod+Shift+7 move container to workspace number 7
bindsym $mod+Shift+8 move container to workspace number 8
bindsym $mod+Shift+9 move container to workspace number 9


# ── Colors — British Racing Green ──────────────────────────────────────────
# class                 border  background text    indicator child_border
client.focused          #0b3d2e #0b3d2e    #ffffff #0b3d2e   #0b3d2e
client.focused_inactive #2b2b2b #2b2b2b    #888888 #2b2b2b   #2b2b2b
client.unfocused        #1a1a1a #1a1a1a    #888888 #1a1a1a   #1a1a1a
client.urgent           #a54242 #a54242    #ffffff #a54242   #a54242
client.placeholder      #000000 #000000    #ffffff #000000   #000000

gaps inner 4
gaps outer 2

# ── Status bar ─────────────────────────────────────────────────────────────
bar {
    font pango:JetBrains Mono 11
    status_command i3status
    position top
    colors {
        background #111111
        statusline #eeeeee
        separator  #333333
        focused_workspace  #0b3d2e #0b3d2e #ffffff
        active_workspace   #1f5c3a #1f5c3a #ffffff
        inactive_workspace #222222 #222222 #888888
        urgent_workspace   #a54242 #a54242 #ffffff
    }
}
EOF

# ── i3status config ────────────────────────────────────────────────────────

echo "→ Writing i3status config..."
mkdir -p ~/.config/i3status
cat > ~/.config/i3status/config <<'EOF'
general {
    colors = true
    interval = 5
    color_good    = "#1f5c3a"
    color_degraded = "#ffaa00"
    color_bad     = "#a54242"
}

order += "ethernet tun0"
order += "disk /"
order += "memory"
order += "tztime local"

ethernet tun0 {
    format_up   = "VPN %ip"
    format_down = "VPN --"
}

disk "/" {
    format = "Disk %avail"
}

memory {
    format             = "RAM %used/%total"
    threshold_degraded = "10%"
}

tztime local {
    format   = "%H:%M"
    timezone = "Europe/Berlin"
}
EOF

# ── rofi config ────────────────────────────────────────────────────────────

echo "→ Writing rofi config..."
mkdir -p ~/.config/rofi
cat > ~/.config/rofi/config.rasi <<'EOF'
configuration {
    modi: "drun,run,window";
    show-icons: true;
    font: "JetBrains Mono 12";
    kb-cancel: "Escape";
}
@theme "Arc-Dark"
EOF

# ── Done ───────────────────────────────────────────────────────────────────

cat <<'DONE'

✓ Done! Reconnect via RDP — you'll be in i3.

Key bindings (Alt = modifier):
  Alt+Enter       alacritty
  Alt+Space       Rofi app launcher
  Alt+M           Rofi window switcher
  Alt+T           Rofi tmux session picker
  Alt+Q           Close window
  Alt+H/J/K/L     Focus (vim keys)
  Alt+Shift+H/J/K/L  Move window
  Alt+1-9         Switch workspace
  Alt+Ctrl+1-9    Move window + follow
  Alt+Shift+1-9   Move window (stay)
  Alt+R           Resize mode
  Alt+F           Fullscreen
  Alt+,/.         Prev/next workspace
  Alt+E           Thunar

Tools installed:
  penelope        reverse shell handler
  autorecon       multi-threaded recon
  golang          via apt
  sublime-text    subl
  atuin           shell history
  fzf             fuzzy finder (Ctrl+R, Ctrl+T)
  zoxide          smart cd (alias: cd)
  syncthing       file sync daemon

Status bar: VPN (tun0 IP) | Disk free | RAM | Time

DONE

if [[ -n "${ZSH_VERSION:-}" ]]; then
    exec zsh
fi

VM_IP=$(hostname -I | awk '{print $1}')
cat <<SSHCONF
── SSH config — paste into ~/.ssh/config on your host ──────────────────────

Host kali
  HostName ${VM_IP}
  User kali
  IdentityFile ~/.ssh/kali
  StrictHostKeyChecking no

────────────────────────────────────────────────────────────────────────────
SSHCONF
