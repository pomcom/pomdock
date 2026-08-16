# pomdock

Kali Linux pentest environment manager. One Go CLI/TUI (`pomdock`) drives two
backends:

- **Docker** — a Kali container with VPN kill-switch (gluetun) and Tor routing
  (custom Whonix-style gateway), for day-to-day engagements.
- **libvirt/KVM** — disposable Kali, Linux server, and Windows engagement VMs
  for GUI-heavy tooling, enterprise labs, or stronger isolation.

The CLI is a thin wrapper: `pomdock docker ...` shells out to `pentest.sh`,
`pomdock vm ...` uses the scripts in `kali-vm/` and `vm-profiles/`. The
provisioners are plain Bash and can also be run standalone.

---

## Install

```bash
cd cli && make build
make install             # binary + runtime files under /usr/local
make completion-zsh
```

Set `PREFIX` to use another location, or set `POMDOCK_ROOT` when running the
binary against a source checkout explicitly.

Requires Go (build only), Docker, `tmux`, and — for the VM side — `qemu-kvm`,
`libvirt-daemon-system`, `libvirt-clients`, `virt-viewer`, `libguestfs-tools`,
`genisoimage`, and `curl`. Windows guests additionally require `swtpm` and a
Secure Boot-capable OVMF package.

### TUI workflow

Run `pomdock` without arguments. Pomdock creates or reattaches a dedicated
tmux workspace with the TUI in window `0:dashboard`. In the Docker tab, press `n` to create a
named engagement and choose direct, VPN, Tor, or combined routing. Press `c`
to focus the embedded command pane for the selected container; `Esc` returns
to navigation, and `C` opens a full interactive zsh when a command needs a
PTY. The command pane keeps its working directory per container and supports
Up/Down command history and `Ctrl+L` to clear its output.

Full shells are persistent windows in the same tmux workspace. The **Shells**
tab lists them; Enter switches windows and `D` closes only that shell window.
Use `Ctrl+B 0` to return to the dashboard, `Ctrl+B n`/`p` to move between
windows, or `Ctrl+B d` to detach the whole workspace without terminating the
TUI, zsh, Atuin history, or running jobs. `q` closes the dashboard; existing
shell windows remain and the next `pomdock` launch recreates the dashboard.

```bash
sudo apt install qemu-kvm libvirt-daemon-system libvirt-clients virt-viewer libguestfs-tools
sudo adduser $USER libvirt   # log out and back in after
```

### VM profiles and SSH keys

Pomdock stores a guest profile in libvirt metadata so connection actions use the
right protocol and username. Existing untagged VMs appear as `Unassigned` and
keep the legacy Kali connection defaults until assigned:

```bash
pomdock vm profile ubuntu-test ubuntu-lts
pomdock vm profile win11-testing windows-11-enterprise
pomdock vm profile provider_GOAD-DC01 windows-server-2025
```

Kali expects an ed25519 key at `~/.ssh/kali`; Linux cloud profiles use
`~/.ssh/pomdock`. Create them with:

```bash
ssh-keygen -t ed25519 -f ~/.ssh/kali -N ""
ssh-keygen -t ed25519 -f ~/.ssh/pomdock -N ""
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

New engagement shells identify themselves in the Starship hostname segment as
`<name>-<route>`, for example `acme-vpn` or `internal-tor-vpn`. Pomdock also sets
`POMDOCK_MACHINE`, `POMDOCK_KIND`, and `POMDOCK_ROUTE` for custom prompt modules.
This does not replace or reconfigure Atuin, fzf, zsh plugins, or aliases from
the mounted dotfiles. Existing containers must be recreated to gain the new
hostname and environment variables.

### Dotfiles

Pomdock uses `~/pcm.dot` by default. Override it with `PENTEST_DOTFILES_DIR`:

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
pomdock vm create [name]                         # Kali by default
pomdock vm create ubuntu-base --profile ubuntu-lts
pomdock vm create debian-base --profile debian-stable
pomdock vm create rocky-base --profile rocky-9
pomdock vm create win11-lab --profile windows-11-enterprise --iso ~/Downloads/Win11.iso
pomdock vm create dc-lab --profile windows-server-2025 --iso ~/Downloads/Server2025.iso
pomdock vm list
pomdock vm profile <name> [profile] # show or assign guest behavior
pomdock vm start <name>
pomdock vm stop  <name>
pomdock vm ssh   <name>
pomdock vm rdp   <name>
pomdock vm console <name> # SPICE (virt-viewer) or serial console fallback
pomdock vm finalize <name> # finish a powered-off Windows installation
pomdock vm reset <name>    # revert to post-setup snapshot
pomdock vm clone / delete / ip <name>

# Tor routing via Whonix Gateway
pomdock vm whonix-gateway         # one-time: download + import Whonix KVM image (~2.2 GB)
pomdock vm whonix-attach <name>   # add Whonix NIC, configure static routing inside VM
pomdock vm whonix-detach <name>
```

### What `vm create` does

Pomdock can automatically create Kali, Ubuntu 24.04 LTS, Debian 13, and Rocky
Linux 9 VMs. The Linux server profiles download official cloud images, verify
the current publisher checksum, inject `~/.ssh/pomdock.pub` with cloud-init,
install the QEMU guest agent, and create a clean `post-setup` snapshot. Downloads
are cached under `~/.cache/pomdock` and revalidated when an upstream `latest`
image changes.

Cloud-image guests are minimal enterprise test servers. They do not receive
the operator's dotfiles or Kali tooling.

Windows profiles create an 8 GiB, 4-vCPU, 80 GiB installer VM from a
user-supplied official Microsoft ISO. Pomdock configures UEFI Secure Boot, TPM
2.0, SPICE, an emulated SATA disk, and an `e1000e` NIC so Windows Setup does not
depend on extra drivers. A local `~/pcm.virt/virtio-win*.iso` is attached when
available. In the TUI, creation opens the graphical console automatically.
Install Windows, create the `pomdock` user on Windows 11 (Server uses
`Administrator`), enable Remote Desktop, and shut the guest down. Then press
`f` in the VM tab or run `pomdock vm finalize <name>` to remove installation
media, create the `post-setup` snapshot, and restart it. Press `r` for RDP and
`R` to reset the VM later. Pomdock does not download proprietary Microsoft
media or bypass license requirements.

For the default Kali profile:

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
| `1` / `2` / `3` / `Tab` | Switch Docker / VM / Shells tab |
| `↑`/`k`, `↓`/`j` | Move selection |
| `n` | Create a Docker/VM engagement; create a shell from Shells |
| `c` / `Enter` | Embedded Docker command / VM SSH / attach shell |
| `C` | Persistent Docker shell window / VM console |
| `s` / `S` | Start / stop the selected Docker container or VM |
| `r` / `R` | VM RDP / reset to snapshot |
| `f` | Finalize a powered-off Windows installation |
| `w` / `W` | Attach / detach VM Whonix routing |
| `D` | Delete container or VM; close shell (confirmation required) |
| `?` | help |
| `q` / `Ctrl+C` | quit |

In the VM tab, `n` clones the selected VM by default for a fast engagement
workspace. Move to **Source** and use Left/Right to choose Kali, Ubuntu, Debian,
Rocky, Windows Server, or Windows 11 instead. Windows choices add an ISO path
field.
Press `r` to start the selected VM if necessary, wait for its address, and
launch `xfreerdp3` (or `xfreerdp`) with dynamic resolution and clipboard support.

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
