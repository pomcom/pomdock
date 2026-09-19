# pomdock

Disposable, network-isolated Kali pentest environments from one command.

pomdock spins up a Kali working environment as either a Docker container or a libvirt VM,
and can force all of its traffic through a VPN kill-switch, through Tor, or through Tor
over VPN. If a tool crashes or is misconfigured, it still cannot leak your real IP —
the environment shares the tunnel's network namespace, so there is no path around it.
Each engagement keeps its own loot directory, shell history, and recorded sessions.

One Go binary drives everything, as a CLI or an interactive TUI.

## Install

```bash
cd cli && make build
make install            # PREFIX sets the location
make completion-zsh
```

Requires Go (to build), Docker, and tmux. VMs additionally need qemu-kvm,
libvirt-daemon-system, libvirt-clients, virt-viewer, libguestfs-tools, genisoimage, and
curl; Windows guests also need swtpm and OVMF with Secure Boot. The shell backends run
standalone without the binary.

## Quick start

```bash
pomdock                                          # open the TUI
pomdock docker exec --vpn ~/vpn/mullvad.conf     # Kali shell, all traffic via VPN
pomdock vm create kali                           # disposable Kali VM
pomdock report                                   # browse recorded sessions
```

## Docker

```bash
pomdock docker build
pomdock docker exec [--vpn FILE] [--whonix] [--name NAME]
pomdock docker status
pomdock docker stop / rm / logs / burp [--name NAME]
```

`exec` is idempotent: it attaches if running, restarts if stopped, and builds and creates
if absent. The flags choose how traffic leaves:

| Flags | Path |
|---|---|
| none | Docker bridge |
| `--vpn FILE` | Kali → gluetun → VPN |
| `--whonix` | Kali → Tor |
| `--whonix --vpn FILE` | Kali → Tor → VPN |

`--vpn` takes an `.ovpn` or `.conf` from any provider. `--name` gives an engagement its
own container, sidecars, loot dir at `~/pentest/NAME`, and history. The route is
remembered: a stopped VPN/Tor engagement reconnects with just `exec --name NAME`.

Full detail: [docs/docker.md](docs/docker.md).

## VMs

```bash
pomdock vm create [name] [--profile PROFILE] [--iso PATH]
pomdock vm list
pomdock vm start / stop / ssh / rdp / console / reset / clone / delete / ip NAME
```

`create` defaults to Kali. Other profiles: `ubuntu-lts`, `debian-stable`, `rocky-9`, and
`windows-11-enterprise` / `windows-server-2019|2022|2025` (bring your own ISO). Every VM
gets a `post-setup` snapshot you can return to with `reset`.

Full detail, including Windows install and Whonix routing:
[docs/vms.md](docs/vms.md).

## Sessions

Shells opened through pomdock are recorded automatically into the engagement's loot dir,
and survive `docker rm`. Tag a long capture with `pomsession start <label>` inside the
shell, then browse and export on the host:

```bash
pomdock report [--name NAME]
```

The report UI has a Tracker tab (Atuin history to team-report rows, copyable as TSV) and
a searchable Sessions tab. It reads the loot dir read-only.

Full detail: [docs/sessions.md](docs/sessions.md).

## More

- [TUI keybindings](docs/tui.md)
- [Testing](docs/testing.md)
- `pomdock --version`

`pomdock docker ...` runs `pentest.sh`; `pomdock vm ...` runs the scripts in `kali-vm/`
and `vm-profiles/`. `POMDOCK_ROOT` points an installed binary at a source checkout.
