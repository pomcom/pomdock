# pomdock

Kali Linux pentest environment manager — Docker containers with VPN kill-switch / Tor routing, and libvirt KVM VMs. One CLI and TUI for everything.

> Convenience tool for **authorized** pentesting. Not an anonymity solution.

---

## Install

```bash
cd cli && make build        # compile
sudo make install           # → /usr/local/bin/pomdock
make completion-zsh         # zsh tab completion
```

---

## Usage

```bash
pomdock          # open TUI (Docker + VM tabs)
```

### Docker

```bash
pomdock docker build
pomdock docker exec                                    # plain shell
pomdock docker exec --vpn ~/tap-vpn/de-ber-001.conf   # VPN kill-switch
pomdock docker exec --whonix                           # route through Tor
pomdock docker exec --whonix --vpn ~/tap.conf          # Tor → VPN
pomdock docker exec --name nordea --vpn ~/tap.conf     # named engagement

pomdock docker status
pomdock docker stop  [--name NAME]
pomdock docker rm    [--name NAME]
pomdock docker logs  [--name NAME]
pomdock docker burp
```

### VMs

```bash
pomdock vm create [name]       # download Kali QEMU, provision, snapshot
pomdock vm list
pomdock vm start / stop / ssh / rdp / console / reset / clone / delete / ip <name>

# Whonix Gateway — route VM traffic through Tor
pomdock vm whonix-gateway              # download + import official Whonix KVM image (~2.2 GB)
pomdock vm whonix-attach <name>
pomdock vm whonix-detach <name>
```

### TUI keys

| Key | Action |
|-----|--------|
| `1` / `2` / `Tab` | Switch Docker / VM tab |
| `c` | exec (Docker) / SSH (VM) |
| `s` / `S` | start / stop |
| `r` / `C` | RDP / console (VM) |
| `R` | reset to snapshot (VM) |
| `w` / `W` | Whonix attach / detach (VM) |
| `D` | delete (with confirm) |
| `q` | quit |

---

## Network modes

### Docker

| Flags | Stack | Notes |
|-------|-------|-------|
| *(none)* | Docker bridge | plain egress |
| `--vpn FILE` | Kali → gluetun → VPN → target | WireGuard or OpenVPN; iptables kill-switch |
| `--whonix` | Kali → Tor gateway → target | transparent Tor proxy |
| `--whonix --vpn FILE` | Kali → Tor → VPN → target | Tor entry, VPN exit |

Kali shares the sidecar's network namespace. gluetun enforces an iptables kill-switch — traffic is blocked if the VPN drops. Named engagements (`--name`) get separate sidecars and a separate loot dir at `~/pentest/<name>`.

### VMs

| Command | Stack |
|---------|-------|
| `pomdock vm ssh <name>` | plain libvirt NAT |
| `pomdock vm whonix-attach <name>` | Kali VM → Whonix Gateway → Tor |

Whonix Gateway uses static IP `10.152.152.10` on the internal bridge. After attach, all VM traffic (including DNS) routes through Tor. SOCKS5 also available at `10.152.152.10:9050`.

---

## Tools

Edit `setup-pentest.sh` — four arrays at the top (`PENTEST_APT`, `PENTEST_GO`, `PENTEST_BINS`, `PENTEST_PIP`), then rebuild:

```bash
pomdock docker build
```

---

## Testing

Each test prints: egress IP (curl), network interfaces, routes, DNS resolver, DNS leak check, and Tor status.

```bash
./test-build.sh                        # build → tool checks → teardown

# Docker
./test-network.sh                                        # plain bridge
./test-network.sh --vpn ~/tap.conf                       # VPN
./test-network.sh --whonix                               # Tor
./test-network.sh --vpn ~/tap.conf --whonix              # all Docker modes

# VM (VM must be running; Whonix-Gateway must be running for --vm-whonix)
./test-network.sh --vm kali-base                         # VM plain
./test-network.sh --vm kali-base --vm-whonix             # VM via Tor

# Everything
./test-network.sh --vpn ~/tap.conf --whonix --vm kali-base --vm-whonix
```

---

## VM prerequisites

```bash
sudo apt install qemu-kvm libvirt-daemon-system libvirt-clients virt-viewer libguestfs-tools
sudo adduser $USER libvirt   # then log out and back in

ssh-keygen -t ed25519 -f ~/.ssh/kali -N ""   # key injected on create
```
