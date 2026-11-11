# TUI

`pomdock tui` runs in the current terminal, including inside host tmux. Running
`pomdock` alone prints the command overview.

New Docker terminals use tmux **inside the container**. Reconnect through the TUI
or `pomdock docker attach NAME`; `pomdock docker exec NAME` opens a direct recorded
shell. VM shells, provisioning jobs and FreeRDP use the host `pomdock` session.
Existing host Docker windows remain accessible without being moved or restarted.

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

`c` / `Enter` attach, `n` opens the selected container workspace, `D` closes the
selected window and its panes. Discovery warnings identify unavailable targets;
readable terminals remain available.

For container tmux, **Ctrl-b d** detaches; under default host tmux use
**Ctrl-b Ctrl-b d**. Detaching preserves processes while the container runs.
Closing a pane or stopping the container ends its processes. For legacy host/VM
windows, **Ctrl-b d** returns to the TUI, or **Ctrl-b L** after switching from an
existing host tmux client. Ctrl-a remains available to readline.
