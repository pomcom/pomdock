# VMs

Full reference for `pomdock vm`. For the short version see the [README](../README.md).

## Requirements

On top of the base install (Go, Docker, tmux), VMs need: `qemu-kvm`,
`libvirt-daemon-system`, `libvirt-clients`, `virt-viewer`, `libguestfs-tools`,
`genisoimage`, and `curl`. Windows guests additionally need `swtpm` and OVMF with Secure
Boot.

```bash
pomdock vm create [name]                          # Kali by default
pomdock vm create NAME --profile ubuntu-lts
pomdock vm create NAME --profile debian-stable
pomdock vm create NAME --profile rocky-9
pomdock vm create NAME --profile windows-11-enterprise --iso PATH
pomdock vm create NAME --profile windows-server-2025 --iso PATH
pomdock vm create NAME --profile windows-server-2022 --iso PATH
pomdock vm create NAME --profile windows-server-2019 --iso PATH
pomdock vm list
pomdock vm profile NAME [profile]
pomdock vm start NAME
pomdock vm stop NAME
pomdock vm ssh NAME
pomdock vm rdp NAME
pomdock vm console NAME
pomdock vm finalize NAME    # after a manual Windows install
pomdock vm reset NAME       # revert to the post-setup snapshot
pomdock vm clone / delete / ip NAME
pomdock vm whonix-gateway
pomdock vm whonix-attach NAME
pomdock vm whonix-detach NAME
```

## Profiles

| Profile | Guest | Provisioning |
|---|---|---|
| `kali` (default) | Kali | Official image, i3 + tools + patched Atuin |
| `ubuntu-lts` | Ubuntu 24.04 LTS | Cloud image |
| `debian-stable` | Debian 13 | Cloud image |
| `rocky-9` | Rocky Linux 9 | Cloud image |
| `windows-11-enterprise` | Windows 11 Enterprise | Your ISO |
| `windows-server-2019/2022/2025` | Windows Server | Your ISO |

## SSH keys

Kali uses `~/.ssh/kali`; Linux cloud profiles use `~/.ssh/pomdock`.

```bash
ssh-keygen -t ed25519 -f ~/.ssh/kali -N ""
ssh-keygen -t ed25519 -f ~/.ssh/pomdock -N ""
```

## Kali

Downloads the official image, provisions i3 plus tools plus a patched Atuin, and takes
a `post-setup` snapshot you can return to with `pomdock vm reset`.

## Linux cloud profiles

Ubuntu, Debian, and Rocky download the official cloud image (checksum verified), inject
your SSH key via cloud-init, install the QEMU guest agent, and snapshot `post-setup`.
No dotfiles, no pentest tools.

## Windows

You supply your own official ISO. VMs boot with UEFI Secure Boot, TPM 2.0, SPICE, an
emulated SATA disk, and an e1000e NIC. Install manually, enable RDP, shut down, then run
`pomdock vm finalize NAME`. The RDP user is `pomdock` on Windows 11 and `Administrator`
on Windows Server.

## Whonix routing

`pomdock vm whonix-gateway` imports the Whonix KVM image once (about 2.2 GB).
`pomdock vm whonix-attach NAME` hot-plugs a Tor-routed NIC onto a running VM and keeps
the management NIC for SSH and RDP. SOCKS5 is available at `10.152.152.10:9050`.

| Detail | Value |
|---|---|
| Internal network | `Whonix_internal` / `10.152.152.0/24` |
| Gateway IP | `10.152.152.10` |
| SOCKS5 | `10.152.152.10:9050` |
