# pomdock

Disposable, network-isolated Kali pentest environments from one command. Run Kali as a
Docker container or a libvirt VM, with all traffic forced through a VPN kill-switch, Tor,
or Tor over VPN. The environment shares the tunnel's network namespace, so a crashed or
misconfigured tool still cannot leak your real IP. Each engagement keeps its own loot
directory, shell history, and recorded sessions. One Go binary, as a CLI or a TUI.

## Install

```bash
cd cli && make build
make install            # PREFIX sets the location
make completion-zsh
```

Needs Go (to build), Docker, and tmux. VMs also need libvirt and QEMU; the full package
list is in [docs/vms.md](docs/vms.md). The shell backends run standalone without the binary.

## Use

```bash
pomdock                                          # open the TUI
pomdock docker exec --vpn ~/vpn/mullvad.conf     # Kali shell, all traffic via VPN
pomdock docker exec --whonix --name acme         # Tor-routed, named engagement
pomdock vm create kali                           # disposable Kali VM
pomdock report                                   # browse recorded sessions
```

Command surface:

```bash
pomdock docker exec [--vpn FILE] [--whonix] [--name NAME]    # attach, or build+create
pomdock docker status | stop | rm | logs | burp [--name NAME]
pomdock vm create [name] [--profile PROFILE] [--iso PATH]
pomdock vm list | start | stop | ssh | rdp | console | reset | clone | delete NAME
pomdock report [--name NAME]                                 # sessions + tracker UI
```

`exec` is idempotent: it attaches if the container runs, restarts it if stopped, builds
and creates it if absent. It records each engagement's route, so a stopped VPN or Tor
engagement reconnects with just `exec --name NAME`. `--name` gives an engagement its own
container, sidecars, loot dir at `~/pentest/NAME`, and history.

## Docs

- [Docker](docs/docker.md) — network modes, named engagements, dotfiles, tools, Burp
- [VMs](docs/vms.md) — requirements, profiles, SSH keys, Windows, Whonix routing
- [Sessions & reporting](docs/sessions.md) — recording, tagging, the report UI
- [TUI](docs/tui.md) — tabs and keybindings
- [Testing](docs/testing.md) — unit, build, and network-isolation tests

`pomdock docker …` runs `pentest.sh`; `pomdock vm …` runs the scripts in `kali-vm/` and
`vm-profiles/`. `POMDOCK_ROOT` points an installed binary at a source checkout.
