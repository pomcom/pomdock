# kali-vm

Low-level libvirt scripts backing `pomdock vm ...`. See the main
[README](../README.md) for the full command reference and Whonix setup — this
file only covers details specific to running the scripts directly.

## Standalone usage

```bash
chmod +x kali-vm/vm
kali-vm/vm create [name]       # default: kali-base
```

Optional — hands-off SSH bootstrap on first login (`kali`/`kali`):

```bash
sudo apt install sshpass
```

Fully automatic bootstrap (recommended):

```bash
sudo apt install libguestfs-tools
```

If `~/.ssh/kali.pub` exists, it's injected automatically and key auth is
preferred over password auth.

## Libvirt URI

Always `qemu:///system` — every script sets this explicitly. Manual checks:

```bash
virsh --connect qemu:///system list --all
```

## Scripts

| Script | Purpose |
|--------|---------|
| `vm` | lifecycle: create/clone/reset/ip/delete (used by the Go CLI) |
| `kali-libvirt-setup.sh` | full VM create + SSH bootstrap + i3 setup + snapshot |
| `kali-i3-setup.sh` | i3wm + autotiling + rofi + zsh/atuin/tmux, run inside the VM |
| `kali-setup-vm.sh` | lightweight XFCE/xrdp/zsh/atuin/tmux bootstrap, run manually inside the VM |
| `whonix-gateway-setup.sh` | downloads + imports the official Whonix Gateway KVM image |
