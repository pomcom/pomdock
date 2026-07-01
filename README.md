# pomdock

Kali Linux pentest environment manager — Docker containers with VPN kill-switch / Tor routing, and libvirt VMs. Everything managed through one CLI and TUI.

> **This is not an OPSEC-hardened setup.** It is a convenience tool for authorized penetration testing — organized loot, isolated environments, VPN kill-switch, Tor routing. It does not make you anonymous. Container escapes, host DNS leaks, timing correlation, browser fingerprinting, and many other vectors are out of scope. If you need real anonymity, use Whonix or Tails. Use this only on engagements you are authorized to perform.

---

## CLI — `pomdock`

```bash
cd cli && make build    # produces ./pomdock
sudo make install       # installs to /usr/local/bin/pomdock
make completion-zsh     # install zsh tab completion
```

### TUI

```bash
pomdock          # or: pomdock tui
```

Two tabs (press `1`/`2` or `Tab` to switch):

| Tab | Shows | Actions |
|-----|-------|---------|
| Docker | Running pentest containers + VPN/Tor status | `c` exec, `S` stop, `D` delete |
| VMs | All libvirt VMs + state + IP + Whonix | `s` start, `S` stop, `c` SSH, `r` RDP, `C` console, `R` reset, `D` delete, `w`/`W` Whonix |

`q` quit, `?` help, auto-refreshes every 3s.

### Docker commands

```bash
pomdock docker build                                   # build Kali image (~10 min first time)
pomdock docker exec                                    # drop into shell
pomdock docker exec --vpn ~/tap-vpn/de-ber-001.conf   # with VPN kill-switch
pomdock docker exec --whonix                           # route through Tor
pomdock docker exec --whonix --vpn ~/tap.conf          # Kali → Tor → VPN → target
pomdock docker exec --name nordea --vpn ~/tap.conf     # named engagement
pomdock docker status                                  # show containers + VPN/Tor columns
pomdock docker stop [--name NAME]                      # stop container + sidecars
pomdock docker rm   [--name NAME]                      # remove container (prompts for loot)
pomdock docker logs [--name NAME]                      # gluetun / whonix logs
pomdock docker burp                                    # Burp proxy setup instructions
```

### VM commands

```bash
pomdock vm create [name]       # download Kali QEMU, provision i3 + tools, snapshot
pomdock vm list                # colored VM table
pomdock vm start <name>
pomdock vm stop  <name>
pomdock vm ssh   <name>        # SSH in (uses ~/.ssh/kali)
pomdock vm rdp   <name>        # RDP via xfreerdp3
pomdock vm console <name>      # virt-viewer or serial fallback (Ctrl+] to exit)
pomdock vm reset <name>        # revert to post-setup snapshot and boot
pomdock vm clone <src> <new>   # clone disk for lab isolation
pomdock vm delete <name>       # destroy + undefine + remove disk
pomdock vm ip <name>           # print current IP

# Route VM traffic through Tor (official Whonix Gateway KVM image)
pomdock vm whonix-gateway              # download + import (~2.2 GB, one-time)
pomdock vm whonix-attach <name>        # attach Whonix NIC, configure routing via SSH
pomdock vm whonix-detach <name>        # remove Whonix NIC, restore default routing
```

Tab completion for VM names works after running `make completion-zsh`.

---

## Docker network modes

| Flags | Stack | Sidecars |
|-------|-------|---------|
| *(none)* | Docker bridge | — |
| `--vpn FILE` | Kali → gluetun (VPN) → Target | `pcm-gluetun` |
| `--whonix` | Kali → Tor gateway → Target | `pcm-whonix` |
| `--whonix --vpn FILE` | Kali → Tor → VPN → Target | both |

All sidecars share network namespace with the pentest container. gluetun enforces a kill-switch via iptables — traffic is blocked if the VPN drops.

### With VPN (`--vpn`)

```
Host
 └── gluetun container  ← WireGuard/OpenVPN tunnel + iptables kill-switch
      │                    HTTP proxy on :8888 (for Burp)
      └── kali container
```

### With Tor (`--whonix`)

```
Host
 └── tor-gateway container  ← Alpine + tor, transparent proxy (TransPort :9040)
      └── kali container    ← all TCP → Tor, all DNS → Tor
```

### Stacked (Tor + VPN)

```
Host
 └── gluetun  ←  tor-gateway  ←  kali
                  tor exits through VPN interface
```

---

## Volumes (Docker)

| Host path | Container path | Purpose |
|-----------|---------------|---------|
| `~/pentest` (or `~/pentest/<name>`) | `/home/kali/pentest` | Loot, notes, scan output |
| `${LOOT_DIR}/.atuin` | `/home/kali/.local/share/atuin` | Shell history (per engagement) |
| `${PENTEST_DOTFILES_DIR}` | `/home/kali/dotfiles` | Dotfiles (live mount) |

`PENTEST_DOTFILES_DIR` defaults to `~/dotfiles`. `PENTEST_LOOT_DIR` overrides the loot path.

---

## Burp Suite

Burp runs natively on the host. With `--vpn`, gluetun exposes an HTTP proxy on `localhost:8888`.

```
Firefox → Burp (:8080) → localhost:8888 → WireGuard → VPN exit
```

**Burp:** Project options → Connections → Upstream proxy → `127.0.0.1:8888` for `*`  
**Firefox:** `about:config` → `network.dns.disableIPv6=true`, proxy to `localhost:8080`

Burp jar: `${PENTEST_DOTFILES_DIR}/tools/burpsuite_pro*.jar`

---

## Adding / removing tools

Edit `setup-pentest.sh`. Four arrays at the top:

```bash
PENTEST_APT=(...)   # apt packages
PENTEST_GO=(...)    # go install
PENTEST_BINS=(...)  # GitHub release binaries
PENTEST_PIP=(...)   # pipx
```

After editing: `pomdock docker build`

---

## Testing

### Tool presence

```bash
./test-build.sh           # full build + test + teardown
./test-build.sh --no-build
./test-build.sh --keep    # keep container on failure
```

### Network stack

```bash
./test-network.sh                              # plain Docker bridge
./test-network.sh --vpn ~/tap.conf             # + VPN
./test-network.sh --whonix                     # + Tor
./test-network.sh --vpn ~/tap.conf --whonix    # all four modes
./test-network.sh --no-teardown                # keep containers for inspection
```

Per mode checks: egress IP via `am.i.mullvad.net/json`, DNS leak via `dig @ns1.google.com`, Tor status via `check.torproject.org/api/ip`.

---

## VM prerequisites

```bash
# Ubuntu/Debian
sudo apt install qemu-kvm libvirt-daemon-system libvirt-clients virt-viewer libguestfs-tools
sudo adduser $USER libvirt
# log out and back in

# Recommended: passwordless SSH key injection
ssh-keygen -t ed25519 -f ~/.ssh/kali -N ""
```

`vm create` uses `libguestfs-tools` to inject your SSH key before first boot. Without it, falls back to `sshpass` with default `kali/kali` credentials.

---

## Structure

```
pomdock/
├── cli/                  # unified CLI (pomdock binary)
│   ├── main.go           # cobra commands — docker + vm
│   ├── docker_ops.go     # docker container operations
│   ├── virsh.go          # libvirt operations
│   ├── tui.go            # bubbletea TUI (Docker + VM tabs)
│   ├── style.go          # Catppuccin Mocha lipgloss styles
│   └── Makefile          # make build / install / completion-zsh
├── pentest.sh            # Docker container lifecycle (called by pomdock docker)
├── setup-pentest.sh      # tool installer — edit to add/remove tools
├── Dockerfile            # Kali image
├── tor-gateway/          # Alpine Tor gateway image
├── test-network.sh       # network integration tests
├── test.sh               # tool presence checks (runs inside container)
├── test-build.sh         # full build → test → teardown
└── kali-vm/              # VM provisioning scripts (called by pomdock vm)
    ├── kali-libvirt-setup.sh
    ├── kali-i3-setup.sh
    ├── kali-setup-vm.sh
    └── whonix-gateway-setup.sh
```
