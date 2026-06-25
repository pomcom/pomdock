# Kali VM (libvirt)

Minimal wrapper for fast local Kali VM handling with automatic i3/pentest provisioning.

## Quickstart

```bash
cd ~/dotfiles
chmod +x pentest/kali-vm/vm
```

Optional — hands-off SSH bootstrap on first login (`kali/kali`):

```bash
sudo apt install sshpass
```

Fully automatic bootstrap (recommended):

```bash
sudo apt install libguestfs-tools
```

If `~/.ssh/kali.pub` exists, it is injected automatically and key auth is preferred.

## Commands

```bash
pentest/kali-vm/vm create [name]   # default: kali-base
pentest/kali-vm/vm clone <src> <new>
pentest/kali-vm/vm reset <name>    # reverts to snapshot: post-setup
pentest/kali-vm/vm ip <name>
pentest/kali-vm/vm delete <name>   # destroy + undefine + remove qcow2
```

## Libvirt URI

Always use `qemu:///system`. The `vm` script sets this automatically.

Manual checks:

```bash
virsh --connect qemu:///system list --all
```

## What `create` does

1. Downloads Kali QEMU image.
2. Creates VM in local libvirt.
3. Waits for IP + SSH.
4. Copies and executes `kali-i3-setup.sh` in the VM.
5. Creates snapshot `post-setup`.

## Post-install inside VM

`kali-setup-vm.sh` is an alternative lightweight bootstrap (no i3, just zsh/atuin/tmux/xrdp)
for cases where you want a quick XFCE setup instead of a full i3 environment:

```bash
scp pentest/kali-vm/kali-setup-vm.sh kali@<vm-ip>:~ && ssh kali@<vm-ip> bash kali-setup-vm.sh
```
