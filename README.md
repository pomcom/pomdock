# pomdock

Kali Linux container with VPN kill-switch via gluetun. All traffic from inside the container is enforced through the VPN — nothing leaks if the tunnel drops.

Shell history (atuin) and loot are persisted per engagement. Dotfiles are volume-mounted at runtime.

> **This is not an OPSEC-hardened setup.** It's a tool to make engagements easier — organized loot, isolated environments, VPN kill-switch. If you need real anonymity, use Whonix or Tails.

---

## Structure

```
pomdock/
├── pentest.sh          # container lifecycle management
├── setup-pentest.sh    # tool installer — edit this to add/remove tools
├── Dockerfile          # Kali image definition
└── kali-vm/            # alternative: libvirt VM instead of Docker
```

---

## Quickstart

### Requirements

- Docker
- A WireGuard (`.conf`) or OpenVPN (`.ovpn`) VPN config
- A dotfiles directory (can be any dir, even empty — see below)

### Build

```bash
# Point to your dotfiles (defaults to ~/dotfiles if not set)
export PENTEST_DOTFILES_DIR=~/your-dotfiles

# Build the image (~10 min first time)
./pentest.sh build
```

If your dotfiles directory has a `setup-shell.sh` at the root, it runs during build (zsh plugins, prompt, etc.). If not — plain zsh.

### Run

```bash
# Shell without VPN
./pentest.sh exec

# Shell with VPN kill-switch
./pentest.sh --vpn ~/path/to/vpn.conf exec

# Named container — separate loot dir + shell history per engagement
./pentest.sh --name clientname --vpn ~/path/to/vpn.conf exec
```

---

## Commands

```
./pentest.sh [--name NAME] [--vpn FILE] <command>

exec      Drop into the container shell. Builds/starts if needed.
build     Build the Docker image.
stop      Stop container and gluetun sidecar.
rm        Remove container + gluetun. Prompts before deleting loot dir.
status    Show container, VPN, and image status.
logs      Show gluetun VPN logs.
```

`--vpn FILE` — Route all container traffic through gluetun. VPN down = all traffic blocked (kill switch). Supports WireGuard (`.conf`) and OpenVPN (`.ovpn`).

`--name NAME` — Named container instance. Each name gets its own loot dir and shell history.

`PENTEST_DOTFILES_DIR` — Override dotfiles path via env var (default: `~/dotfiles`).

`PENTEST_LOOT_DIR` — Override loot dir via env var.

---

## How it works

### With VPN (`--vpn`)

```
Host
 └── gluetun container  ← VPN tunnel, kill switch via iptables
      │                    HTTP proxy on :8888 (for host-side Burp)
      └── kali container  ← shares gluetun's network namespace
           └── curl, httpx, nuclei, nmap, ...  → exits via VPN
```

The kali container shares gluetun's network namespace (`--network container:gluetun`). If the VPN drops, gluetun's iptables kill switch blocks everything.

### Without VPN

Container uses Docker bridge networking. Useful for local lab work.

---

## Volumes

| Host path | Container path | Purpose |
|-----------|---------------|---------|
| `${LOOT_DIR}` | `/home/kali/pentest` | Loot, notes, scan output |
| `${LOOT_DIR}/.atuin` | `/home/kali/.local/share/atuin` | Shell history (persisted) |
| `${PENTEST_DOTFILES_DIR}` | `/home/kali/dotfiles` | Dotfiles (live mount) |

Shell history survives `pentest.sh rm`. Each named container has its own history.

---

## Burp Suite

Run Burp natively on your host. When started with `--vpn`, gluetun exposes an HTTP proxy on `localhost:8888` — configure Burp to use it as upstream proxy.

```
Firefox → Burp (:8080) → localhost:8888 → WireGuard → VPN exit IP
```

**In Burp:** Project options → Connections → Upstream proxy servers → `127.0.0.1:8888` for `*`

**Firefox:** disable IPv6 (`about:config` → `network.dns.disableIPv6=true`), proxy to `localhost:8080`

---

## Adding / removing tools

Edit `setup-pentest.sh`. The lists at the top are the only thing to touch:

```bash
PENTEST_APT=(...)   # apt packages
PENTEST_GO=(...)    # go install
PENTEST_BINS=(...)  # GitHub release binaries
PENTEST_PIP=(...)   # pipx
```

After editing: `./pentest.sh build`

---

## Rebuild vs. no rebuild

| Change | Rebuild needed? |
|--------|----------------|
| Tools in `setup-pentest.sh` | Yes |
| `Dockerfile` | Yes |
| `pentest.sh` | No — runs on host |
| Dotfiles | No — live mounted |

---

## VM alternative

> **Note:** `kali-vm/` is untested and unmaintained. Use at your own risk.

`kali-vm/` contains libvirt scripts for a local Kali VM. Use when tools don't work well in Docker.

```bash
kali-vm/vm create [name]
kali-vm/vm clone <src> <new>
kali-vm/vm reset <name>
kali-vm/vm ip <name>
kali-vm/vm delete <name>
```
