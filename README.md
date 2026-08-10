# pomdock

Kali Linux pentest environment manager. One Go CLI/TUI (`pomdock`) drives two
backends:

- **Docker** — a Kali container with VPN kill-switch (gluetun) and Tor routing
  (custom Whonix-style gateway), for day-to-day engagements.
- **libvirt/KVM** — a full Kali VM with i3, for GUI-heavy tooling (Burp, browser,
  RDP-based labs) or when a container isn't isolated enough.

The CLI is a thin wrapper: `pomdock docker ...` shells out to `pentest.sh`,
`pomdock vm ...` shells out to the scripts in `kali-vm/`. Both are plain bash
and can be run standalone if you don't want the Go binary.

---

## Install

```bash
cd cli && make build
sudo make install        # /usr/local/bin/pomdock
make completion-zsh
```

Requires Go (build only), Docker, and — for the VM side — `qemu-kvm`,
`libvirt-daemon-system`, `libvirt-clients`, `virt-viewer`, `libguestfs-tools`.

```bash
sudo apt install qemu-kvm libvirt-daemon-system libvirt-clients virt-viewer libguestfs-tools
sudo adduser $USER libvirt   # log out and back in after
```

### SSH key for VMs

VM SSH/RDP/Whonix-attach all expect an ed25519 key at exactly `~/.ssh/kali`:

```bash
ssh-keygen -t ed25519 -f ~/.ssh/kali -N ""
```

If present, `vm create` injects `~/.ssh/kali.pub` into the VM automatically and
key auth is used everywhere. Without it, commands fall back to password auth
(`kali`/`kali`) or plain `ssh`/`sshpass`, which is slower and less reliable —
create the key first.

---

## Docker

```bash
pomdock docker build

pomdock docker exec                                   # plain shell
pomdock docker exec --vpn ~/mullvad/de-ber-001.conf  # WireGuard/OpenVPN kill-switch
pomdock docker exec --whonix                          # transparent Tor routing
pomdock docker exec --whonix --vpn ~/tap.conf         # Tor over VPN (Tor circuits through VPN tunnel)
pomdock docker exec --name myengagement --vpn ~/tap.conf    # named engagement

pomdock docker status
pomdock docker stop   [--name NAME]
pomdock docker rm     [--name NAME]
pomdock docker logs   [--name NAME]
pomdock docker burp
```

`exec` is idempotent: if the container is already running it just attaches; if
it exists but stopped it restarts it; if it doesn't exist it builds the image
(if needed) and creates it with the requested network stack. Switching network
mode (e.g. adding `--vpn` to a container that was created plain) requires
recreating the container — `exec` detects this and does it automatically,
tearing down and rebuilding the sidecar as needed.

Named engagements (`--name`) get their own container, sidecars, and loot dir
at `~/pentest/<name>`, plus a separate atuin history — run several engagements
side by side without them colliding.

### Dotfiles

Set `PENTEST_DOTFILES_DIR` to your dotfiles directory (default: `~/dotfiles`):

```bash
export PENTEST_DOTFILES_DIR=~/pcm.dot
```

Your dotfiles are baked into the image at build time and mounted live at
runtime:

```
~/pcm.dot  ->  /home/kali/dotfiles
```

Inside the container `~/pcm.dot` is a symlink to `~/dotfiles`, so relative
paths in your configs work the same way. Changes on the host are immediately
visible without rebuilding.

If `setup-shell.sh` exists in your dotfiles dir, it runs during build to
install shell tooling (zsh plugins, atuin, starship, etc.). The image also
builds the vendored, patched `vendor/atuin/` source during `docker build` and
seeds that binary first so `setup-shell.sh` can reuse it instead of building
its own.

### Network stacks

| Flags | Path |
|-------|------|
| *(none)* | Docker bridge |
| `--vpn FILE` | Kali → gluetun → VPN |
| `--whonix` | Kali → Tor |
| `--whonix --vpn FILE` | Kali → Tor over VPN (Tor circuits through VPN) |

Kali shares the sidecar's network namespace. `--vpn` accepts any `.ovpn`
(OpenVPN) or `.conf` (WireGuard) file. Gluetun enforces an iptables kill
switch, so traffic is blocked outright if the VPN tunnel drops — nothing
leaks through the real IP.

### DNS per mode

- **plain** — host resolver, no tunnel
- **vpn** — gluetun runs unbound with DNS-over-TLS through the VPN; resolver is `127.0.0.1` inside the container
- **whonix** — resolver `127.0.0.1`, forwarded through the Tor DNSPort; all DNS exits via Tor
- **stack** (whonix + vpn) — same as whonix; both HTTP and DNS exit via the VPN

### Burp Suite

Burp runs natively on the host, not inside the container. `pomdock docker
burp` just prints the proxy setup: point Burp's upstream proxy at
`localhost:8888` (gluetun's HTTP proxy) to route Burp's own traffic through
whatever VPN/Tor stack the container is using.

### Tools

Edit `setup-pentest.sh` — four arrays at the top — then rebuild:

```bash
pomdock docker build
```

| Array | Contents |
|-------|----------|
| `PENTEST_APT` | nmap, masscan, metasploit-framework, hydra, hashcat, john, sqlmap, nikto, dirb, wfuzz, wireshark, responder, bettercap, smbclient, smbmap, crackmapexec, enum4linux-ng, impacket, netexec, wordlists, seclists, firefox-esr, ... |
| `PENTEST_GO` | ffuf, gobuster, nuclei, httpx, subfinder, katana, naabu, dnsx, alterx, gitleaks, gospider, jsluice, tlsx, asnmap, mapcidr, interactsh-client, uncover, cvemap |
| `PENTEST_BINS` | feroxbuster, trufflehog, gowitness, rustscan — resolved from latest GitHub releases |
| `PENTEST_PIP` | snallygaster (impacket ships via apt instead, exposing `impacket-*` binaries directly) |

Installs happen one-by-one with failures collected and printed as a summary,
so one bad package name or unreachable release doesn't kill the whole build.

---

## VMs

```bash
pomdock vm create [name]   # downloads current Kali QEMU image, provisions, snapshots
pomdock vm list
pomdock vm start <name>
pomdock vm stop  <name>
pomdock vm ssh   <name>
pomdock vm rdp   <name>
pomdock vm console <name> # SPICE (virt-viewer) or serial console fallback
pomdock vm reset <name>    # revert to post-setup snapshot
pomdock vm clone / delete / ip <name>

# Tor routing via Whonix Gateway
pomdock vm whonix-gateway         # one-time: download + import Whonix KVM image (~2.2 GB)
pomdock vm whonix-attach <name>   # add Whonix NIC, configure static routing inside VM
pomdock vm whonix-detach <name>
```

### What `vm create` does

1. Downloads the current official Kali QEMU image (cached for reuse).
2. Defines and starts the VM in `qemu:///system` libvirt.
3. Waits for DHCP + SSH to come up.
4. Copies and runs `kali-vm/kali-i3-setup.sh` inside the VM — i3 + autotiling
   + rofi + zsh/atuin/tmux + pentest tools.
5. Snapshots the result as `post-setup`. `vm reset` reverts to this snapshot,
   so a VM burned by an engagement is one command away from clean.

`kali-vm/kali-setup-vm.sh` is a lighter alternative (XFCE/xrdp instead of i3,
no tool provisioning) for when a full pentest desktop isn't needed:

```bash
scp kali-vm/kali-setup-vm.sh kali@<vm-ip>:~ && ssh kali@<vm-ip> bash kali-setup-vm.sh
```

### VM + Whonix setup

1. `pomdock vm whonix-gateway` — imports the official Whonix KVM image (one time, ~2.2 GB)
2. Start your VM: `pomdock vm start <name>`
3. `pomdock vm whonix-attach <name>` — hotplugs a second NIC on the
   Whonix-Internal bridge and configures inside the VM:
   - static IP `10.152.152.100/18` on the new NIC
   - default route via `10.152.152.10` (the Gateway)
   - DNS set to `10.152.152.10` (Tor-proxied)
   - management NIC (`192.168.122.x`) stays up for SSH/RDP
4. First boot: wait ~2 min for Tor to bootstrap. Whonix is fail-closed —
   nothing gets through until Tor is up.
5. `pomdock vm whonix-detach <name>` removes the NIC and restores the normal
   default route.

SOCKS5 proxy at `10.152.152.10:9050` if you need it without full transparent
routing.

### VPN in VMs

`wireguard-tools`, `openvpn`, `openresolv`, and `mullvad-vpn` are installed
during provisioning. Connect manually after `pomdock vm ssh <name>`.
WireGuard through libvirt NAT can have handshake issues — the Docker `--vpn`
mode is more reliable for automated VPN management.

---

## TUI

```bash
pomdock tui   # or just: pomdock
```

| Key | Action |
|-----|--------|
| `1` / `2` / `Tab` | Switch Docker / VM tab |
| `↑`/`k`, `↓`/`j` | Move selection |
| `c` / `Enter` | exec (Docker) / SSH (VM) |
| `s` / `S` | start / stop |
| `r` / `C` | RDP / console (VM) |
| `R` | reset to snapshot (VM) |
| `w` / `W` | Whonix attach / detach (VM) |
| `D` | delete (confirm required) |
| `?` | help |
| `q` / `Ctrl+C` | quit |

---

## Testing

Each test prints the egress IP, interfaces, routes, DNS resolver, DNS leak
check, and Tor status.

```bash
./test-build.sh                        # build + tool checks

# Docker
./test-network.sh                      # plain
./test-network.sh --vpn ~/tap.conf     # VPN
./test-network.sh --whonix             # Tor
./test-network.sh --vpn ~/tap.conf --whonix   # all modes

# VM (must be running; Whonix-Gateway must be running for --vm-whonix)
./test-network.sh --vm kali-base
./test-network.sh --vm kali-base --vm-whonix

# Everything
./test-network.sh --vpn ~/tap.conf --whonix --vm kali-base --vm-whonix
```

Expected warnings that are not real leaks:
- **VPN, DNS egress != HTTP egress** — gluetun DoT exits from the WireGuard peer IP, not the assigned exit IP. Same tunnel.
- **VM+Whonix, no response from Google NS** — Whonix blocks direct UDP to external nameservers by design. DNS still routes through Tor.
- **VM+Whonix, nameserver is private IP** — `10.152.152.10` is the Whonix Gateway; DNS is Tor-proxied.
