# TUI

`pomdock` (or `pomdock tui`) opens the interactive terminal UI. It runs directly in your
terminal and is fine inside an existing tmux session. Persistent shells, VM provisioning,
and FreeRDP live in a detached tmux session named `pomdock` and are attached on demand.

## Global keys

`1` / `2` / `3` / `Tab` switch tabs, arrows or `j` / `k` move the selection, `?` shows
help, `q` quits.

## Docker tab

| Key | Action |
|---|---|
| `n` | New engagement |
| `i` / `c` / `Enter` | Identity / egress check |
| `p` | Listeners and published ports |
| `t` | Tor exit check |
| `u` / `d` | Upload / download file |
| `C` | Persistent shell |
| `s` / `S` | Start / stop |
| `D` | Delete (confirm) |

## VM tab

| Key | Action |
|---|---|
| `n` | New / clone VM |
| `c` / `Enter` | SSH |
| `C` | Console (SPICE / serial) |
| `r` | RDP |
| `R` | Reset to snapshot |
| `f` | Finalize Windows install |
| `w` / `W` | Attach / detach Whonix routing |
| `s` / `S` | Start / stop |
| `D` | Delete (confirm) |

## Shells tab

`c` / `Enter` attach, `n` new shell, `D` close.

Attaching hands the terminal to tmux. `Ctrl+B d` (or `Ctrl+B L` when pomdock itself runs
inside tmux) returns to the TUI; shells and jobs keep running.
