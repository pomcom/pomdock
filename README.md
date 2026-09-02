# pomdock

Kali pentest environment manager. Go CLI/TUI. Two backends:

- Docker: Kali container, VPN kill switch (gluetun), Tor routing
- libvirt/KVM: disposable Kali, Linux, Windows VMs

`pomdock docker ...` runs `pentest.sh`. `pomdock vm ...` runs scripts in `kali-vm/` and
`vm-profiles/`. Scripts also run standalone without the binary.

## Install

```bash
cd cli && make build
make install
make completion-zsh
```

`PREFIX` sets install location. `POMDOCK_ROOT` points the binary at a source checkout.

Requires: Go (build), Docker, tmux. For VMs: qemu-kvm, libvirt-daemon-system,
libvirt-clients, virt-viewer, libguestfs-tools, genisoimage, curl. Windows guests also
need swtpm and OVMF with Secure Boot.

## Run

```bash
pomdock          # TUI
pomdock tui       # same
```

Opens/reattaches a tmux workspace, dashboard in window `0`.

## Docker

```bash
pomdock docker build
pomdock docker exec                                    # plain
pomdock docker exec --vpn FILE                          # VPN kill switch
pomdock docker exec --whonix                             # Tor
pomdock docker exec --whonix --vpn FILE                  # Tor over VPN
pomdock docker exec --name NAME --vpn FILE                # named engagement
pomdock docker status
pomdock docker stop [--name NAME]
pomdock docker rm   [--name NAME]
pomdock docker logs [--name NAME]
pomdock docker burp
```

`exec` is idempotent: attaches if running, restarts if stopped, builds+creates if not.
Switching network mode recreates the container automatically.

Named engagements get their own container, sidecars, loot dir at `~/pentest/<name>`,
separate Atuin history.

VPN accepts `.ovpn` (OpenVPN) or `.conf` (WireGuard), any provider.

| Flags | Path |
|---|---|
| none | Docker bridge |
| `--vpn FILE` | Kali -> gluetun -> VPN |
| `--whonix` | Kali -> Tor |
| `--whonix --vpn FILE` | Kali -> Tor -> VPN |

Dotfiles: `PENTEST_DOTFILES_DIR` (default `~/pcm.dot`), mounted at `/home/kali/dotfiles`,
symlinked to `~/pcm.dot` inside the container. Baked in at build, live-mounted at runtime.

Tools: edit `setup-pentest.sh` (arrays `PENTEST_APT`, `PENTEST_GO`, `PENTEST_BINS`,
`PENTEST_PIP`), then `pomdock docker build`.

`pomdock docker burp` launches Burp Suite Pro inside the container via X11, using the
jar in `<dotfiles>/tools/burpsuite_pro*.jar`. Proxy listens on `localhost:8080` once
Burp starts. Container must already be running.

## VMs

```bash
pomdock vm create [name]                          # Kali by default
pomdock vm create NAME --profile ubuntu-lts
pomdock vm create NAME --profile debian-stable
pomdock vm create NAME --profile rocky-9
pomdock vm create NAME --profile windows-11-enterprise --iso PATH
pomdock vm create NAME --profile windows-server-2025 --iso PATH
pomdock vm create NAME --profile windows-server-2019 --iso PATH
pomdock vm list
pomdock vm profile NAME [profile]
pomdock vm start NAME
pomdock vm stop NAME
pomdock vm ssh NAME
pomdock vm rdp NAME
pomdock vm console NAME
pomdock vm finalize NAME    # after manual Windows install
pomdock vm reset NAME       # revert to post-setup snapshot
pomdock vm clone / delete / ip NAME
pomdock vm whonix-gateway
pomdock vm whonix-attach NAME
pomdock vm whonix-detach NAME
```

SSH keys: Kali uses `~/.ssh/kali`, Linux cloud profiles use `~/.ssh/pomdock`.

```bash
ssh-keygen -t ed25519 -f ~/.ssh/kali -N ""
ssh-keygen -t ed25519 -f ~/.ssh/pomdock -N ""
```

Kali: downloads official image, provisions i3 + tools + patched Atuin, snapshots
`post-setup`.

Linux cloud profiles (Ubuntu/Debian/Rocky): downloads official cloud image, checksum
verified, cloud-init SSH key injection, QEMU guest agent, snapshots `post-setup`. No
dotfiles, no pentest tools.

Windows: needs your own official ISO. UEFI Secure Boot, TPM 2.0, SPICE, emulated SATA,
e1000e NIC. Install manually, enable RDP, shut down, then `pomdock vm finalize NAME`.
RDP user: `pomdock` on Windows 11, `Administrator` on Windows Server 2025.

Whonix VM routing: `vm whonix-gateway` imports the Whonix KVM image once. `vm
whonix-attach NAME` hotplugs a Tor-routed NIC. SOCKS5 at `10.152.152.10:9050`.

## TUI

Global: `1`/`2`/`3`/`Tab` switch tabs, arrows/`j`/`k` move selection, `?` help, `q` quit.

Docker tab:

| Key | Action |
|---|---|
| `n` | New engagement |
| `i`/`c`/`Enter` | Identity/egress check |
| `p` | Listeners and published ports |
| `t` | Tor exit check |
| `u`/`d` | Upload / download file |
| `C` | Persistent shell |
| `s`/`S` | Start / stop |
| `D` | Delete (confirm) |

VM tab:

| Key | Action |
|---|---|
| `n` | New/clone VM |
| `c`/`Enter` | SSH |
| `C` | Console (SPICE/serial) |
| `r` | RDP |
| `R` | Reset to snapshot |
| `f` | Finalize Windows install |
| `w`/`W` | Attach / detach Whonix routing |
| `s`/`S` | Start / stop |
| `D` | Delete (confirm) |

Shells tab: `c`/`Enter` attach, `n` new shell, `D` close.

`Ctrl+B 0` back to dashboard. `Ctrl+B d` detach workspace, jobs keep running.

## Test

```bash
./test-build.sh
./test-network.sh
./test-network.sh --vpn FILE
./test-network.sh --whonix
./test-network.sh --vpn FILE --whonix
./test-network.sh --vm NAME
./test-network.sh --vm NAME --vm-whonix
```
