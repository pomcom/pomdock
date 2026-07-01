# pomdock

Kali Linux pentest environment manager. Wraps Docker containers (VPN kill-switch, Tor routing) and libvirt KVM VMs under one CLI.

---

## Install

```bash
cd cli && make build
sudo make install        # /usr/local/bin/pomdock
make completion-zsh
```

---

## Docker

```bash
pomdock docker build

pomdock docker exec                                   # plain shell
pomdock docker exec --vpn ~/mullvad/de-ber-001.conf  # WireGuard/OpenVPN kill-switch
pomdock docker exec --whonix                          # transparent Tor routing
pomdock docker exec --whonix --vpn ~/tap.conf         # Tor in, VPN out
pomdock docker exec --name myengagement --vpn ~/tap.conf    # named engagement

pomdock docker status
pomdock docker stop   [--name NAME]
pomdock docker rm     [--name NAME]
pomdock docker logs   [--name NAME]
pomdock docker burp
```

Named engagements get their own sidecar containers and loot dir at `~/pentest/<name>`, plus a separate atuin history.

### Dotfiles

Set `PENTEST_DOTFILES_DIR` to your dotfiles directory (default: `~/dotfiles`):

```bash
export PENTEST_DOTFILES_DIR=~/pcm.dot
```

Your dotfiles are baked into the image at build time and mounted live at runtime:

```
~/pcm.dot  ->  /home/kali/dotfiles
```

Inside the container `~/pcm.dot` is a symlink to `~/dotfiles`, so relative paths in your configs work the same way. Changes on the host are immediately visible without rebuilding.

If `setup-shell.sh` exists in your dotfiles dir, it runs during build to install shell tooling (zsh plugins, atuin, starship, etc.).

The image ships a custom-built atuin binary (`bin/atuin`) that shows absolute timestamps instead of relative ones. If your dotfiles include their own `atuin/bin/atuin`, that takes priority. Falls back to the upstream installer if neither is present.

### Network stacks

| Flags | Path |
|-------|------|
| *(none)* | Docker bridge |
| `--vpn FILE` | Kali -> gluetun -> VPN |
| `--whonix` | Kali -> Tor gateway |
| `--whonix --vpn FILE` | Kali -> Tor -> VPN |

Kali shares the sidecar's network namespace. gluetun enforces an iptables kill-switch so traffic is blocked if the VPN drops.

### DNS per mode

- **plain** -- host resolver, no tunnel
- **vpn** -- gluetun runs unbound with DNS-over-TLS through the VPN; nameserver is `127.0.0.1` inside the container
- **whonix** -- nameserver `127.0.0.1`; DNS forwarded through the Tor DNSPort; all DNS exits via Tor
- **stack** -- same as whonix; both HTTP and DNS exit via the VPN

---

## VMs

```bash
# Prerequisites (once)
sudo apt install qemu-kvm libvirt-daemon-system libvirt-clients virt-viewer libguestfs-tools
sudo adduser $USER libvirt   # log out and back in after
ssh-keygen -t ed25519 -f ~/.ssh/kali -N ""

# VM lifecycle
pomdock vm create [name]   # downloads current Kali QEMU image, provisions, snapshots
pomdock vm list
pomdock vm start <name>
pomdock vm stop  <name>
pomdock vm ssh   <name>
pomdock vm rdp   <name>
pomdock vm reset <name>    # revert to post-setup snapshot
pomdock vm clone / delete / ip <name>

# Tor routing via Whonix Gateway
pomdock vm whonix-gateway         # one-time: download + import Whonix KVM image (~2.2 GB)
pomdock vm whonix-attach <name>   # add Whonix NIC, configure static routing inside VM
pomdock vm whonix-detach <name>
```

### VM + Whonix setup

1. `pomdock vm whonix-gateway` -- imports the official Whonix KVM image (one time, ~2.2 GB)
2. Start your VM: `pomdock vm start <name>`
3. `pomdock vm whonix-attach <name>` -- hotplugs a second NIC on the Whonix-Internal bridge and configures inside the VM:
   - static IP `10.152.152.100/18` on `eth1`
   - default route via `10.152.152.10` (the Gateway)
   - DNS set to `10.152.152.10` (Tor-proxied)
   - management NIC (`eth0 / 192.168.122.x`) stays up for SSH/RDP
4. First boot: wait ~2 min for Tor to bootstrap. Whonix is fail-closed, nothing gets through until Tor is up.

SOCKS5 proxy at `10.152.152.10:9050` if you need it without full transparent routing.

### VPN in VMs

`wireguard-tools`, `openvpn`, `openresolv`, and `mullvad-vpn` are installed during provisioning. Connect manually after `pomdock vm ssh <name>`. WireGuard through libvirt NAT can have handshake issues -- the Docker `--vpn` mode is more reliable for automated VPN management.

---

## Tools

Edit `setup-pentest.sh` -- four arrays at the top (`PENTEST_APT`, `PENTEST_GO`, `PENTEST_BINS`, `PENTEST_PIP`) -- then rebuild:

```bash
pomdock docker build
```

---

## TUI

```bash
pomdock tui   # or just: pomdock
```

| Key | Action |
|-----|--------|
| `1` / `2` / `Tab` | Switch Docker / VM tab |
| `c` | exec (Docker) / SSH (VM) |
| `s` / `S` | start / stop |
| `r` / `C` | RDP / console (VM) |
| `R` | reset to snapshot |
| `w` / `W` | Whonix attach / detach |
| `D` | delete (confirm required) |
| `q` | quit |

---

## Testing

Each test prints the egress IP, interfaces, routes, DNS resolver, DNS leak check, and Tor status.

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
- **VPN, DNS egress != HTTP egress** -- gluetun DoT exits from the WireGuard peer IP, not the assigned exit IP. Same tunnel.
- **VM+Whonix, no response from Google NS** -- Whonix blocks direct UDP to external nameservers by design. DNS still routes through Tor.
- **VM+Whonix, nameserver is private IP** -- `10.152.152.10` is the Whonix Gateway; DNS is Tor-proxied.
