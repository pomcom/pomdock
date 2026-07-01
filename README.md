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

# Whonix Gateway — route VM traffic through Tor (one-time import ~2.2 GB)
pomdock vm whonix-gateway              # download + import official Whonix KVM image
pomdock vm whonix-attach <name>        # configure Tor routing inside VM
pomdock vm whonix-detach <name>        # restore plain routing
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

**DNS per mode:**
- **plain** — inherits host resolver (router/ISP DNS, no tunnel)
- **vpn** — gluetun runs unbound with DNS-over-TLS through the VPN tunnel; nameserver is `127.0.0.1` inside the container
- **whonix** — nameserver `127.0.0.1`; DNS forwarded via socat → Tor DNSPort; all DNS exits via Tor
- **stack (Tor → VPN)** — same as whonix DNS; HTTP and DNS both exit via the VPN

### VMs

| State | Stack |
|-------|-------|
| plain | libvirt NAT → host egress |
| after `whonix-attach` | Kali VM → Whonix Gateway (10.152.152.10) → Tor |

**Whonix first-time setup:**
1. `pomdock vm whonix-gateway` — downloads and imports the official Whonix KVM image (~2.2 GB, one-time)
2. Start the target VM: `pomdock vm start <name>`
3. `pomdock vm whonix-attach <name>` — hotplugs a second NIC on the Whonix-Internal bridge, configures static IP `10.152.152.100/18` on `eth1`, sets default route via `10.152.152.10`, and sets DNS to `10.152.152.10` (Tor-proxied). Management NIC (`eth0 / 192.168.122.x`) stays up for SSH/RDP.
4. Wait ~2 min on first Whonix boot for Tor to fully bootstrap before traffic flows.

**DNS in VM+Whonix:** nameserver `10.152.152.10` — the Whonix Gateway proxies all DNS through Tor. Direct UDP to external nameservers is blocked by the Whonix firewall (by design — this is not a leak, it's a feature).

**VPN in VMs:** `wireguard-tools`, `openvpn`, `openresolv`, and `mullvad-vpn` are installed during VM provisioning. Configure and connect manually after `pomdock vm ssh <name>`. WireGuard through libvirt NAT may have handshake issues depending on the host NAT config — use the Docker `--vpn` mode for automated VPN management.

**SOCKS5 proxy** (VM+Whonix): `10.152.152.10:9050` — usable from any app that supports SOCKS5 without full transparent routing.

---

## Tools

Edit `setup-pentest.sh` — four arrays at the top (`PENTEST_APT`, `PENTEST_GO`, `PENTEST_BINS`, `PENTEST_PIP`), then rebuild:

```bash
pomdock docker build
```

---

## Testing

Each test prints: egress IP (curl), network interfaces, routes, DNS resolver, DNS leak check, and Tor status — so you can confirm the correct IP is in use and there is no DNS leak.

```bash
./test-build.sh                        # build → tool checks → teardown

# Docker
./test-network.sh                                        # plain bridge
./test-network.sh --vpn ~/tap.conf                       # VPN
./test-network.sh --whonix                               # Tor
./test-network.sh --vpn ~/tap.conf --whonix              # all Docker modes

# VM (VM must be running; Whonix-Gateway must be running for --vm-whonix)
./test-network.sh --vm kali-base                         # VM in current state
./test-network.sh --vm kali-base --vm-whonix             # VM + Tor via Whonix Gateway

# Everything
./test-network.sh --vpn ~/tap.conf --whonix --vm kali-base --vm-whonix
```

**Expected warnings (not real leaks):**
- **VPN mode** — DNS egress IP ≠ HTTP egress IP: gluetun's DoT resolver exits from the WireGuard peer IP (not the assigned exit IP). Different IP, same tunnel. Expected.
- **VM+Whonix DNS leak check** — "No response from Google NS": Whonix blocks direct UDP to external nameservers. DNS still routes through Tor via `10.152.152.10`. Expected.
- **VM+Whonix DNS resolver** — "Nameserver is private IP (10.152.152.10)": this is the Whonix Gateway; DNS is Tor-proxied. Expected.

---

## VM prerequisites

```bash
sudo apt install qemu-kvm libvirt-daemon-system libvirt-clients virt-viewer libguestfs-tools
sudo adduser $USER libvirt   # then log out and back in

ssh-keygen -t ed25519 -f ~/.ssh/kali -N ""   # key injected on create
```
