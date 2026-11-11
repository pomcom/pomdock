# Docker containers

```bash
pomdock docker build [--toolset full|minimal] [--fresh]
pomdock docker exec acme --vpn ~/vpn/client.conf
pomdock docker start tor-lab --route tor
pomdock docker exec acme                         # recorded shell, reuse saved route
pomdock docker attach acme                       # persistent tmux inside the container
pomdock docker attach acme --window @1           # resume a specific window
pomdock docker status [NAME]
pomdock docker stop [NAME]
pomdock docker rm NAME                          # keep loot
pomdock docker rm NAME --purge                   # confirm deleting loot too
pomdock docker logs [NAME]                      # container and VPN/Tor sidecar logs
pomdock docker burp [NAME]
```

All target commands accept a positional name or `--name NAME`. `attach` and `rm` require an
explicit target. Other actions retain the default container when no name is given.
`exec` starts/creates the container as needed and opens a recorded shell; `start`
leaves it in the background. Named engagements keep separate loot and Atuin history
in `~/pomdock/workspaces/NAME`, with their own sidecars.

## Persistent terminals

`pomdock docker attach NAME` resumes the container's tmux workspace, creating it on first
use. Start the container first with `pomdock docker start NAME`. Windows and panes run
inside that container; detaching leaves them running until the container stops.
`pomdock docker exec NAME` remains a separate recorded shell without tmux.

Use **Ctrl-b d** to detach, **Ctrl-b c** for a window, and **Ctrl-b %** or
**Ctrl-b "** to split. Inside a default host tmux, press **Ctrl-b Ctrl-b** before
the inner command key (for example, **Ctrl-b Ctrl-b d**). Ctrl-a remains readline's
beginning-of-line shortcut. Host tmux settings are unchanged.

Older host-managed Docker windows remain available as host terminals; they are
never automatically moved or restarted. VM terminals and jobs still use host tmux.
Container stop/recreation ends internal tmux processes; recordings persist in the
workspace. Older images need rebuilding to provide `pomdock-tmux`.

## Network modes

| Route | Setup |
|---|---|
| `--route direct` | Docker bridge, without VPN/Tor sidecars |
| `--route vpn --vpn FILE` | gluetun VPN |
| `--route tor` | Tor gateway |
| `--route tor-vpn --vpn FILE` | Tor gateway using gluetun's VPN network |

`--vpn FILE` accepts WireGuard `.conf` and OpenVPN `.ovpn` profiles and implies
VPN routing when `--route` is omitted. Legacy `--whonix` remains accepted for Tor.
An omitted route reuses the existing container's labels and saved VPN profile;
a new container defaults to direct networking. Stop a running container before
changing routes. A stopped container is recreated with its loot preserved when
the requested route changes. If a saved VPN profile moved, supply `--vpn` again.

For HTB/THM, use direct networking and start OpenVPN **inside** Kali:

```bash
pomdock docker start hackthebox --route direct
# Put the profile and any referenced files in ~/pomdock/workspaces/hackthebox/vpn/.
pomdock docker exec hackthebox
cd ~/workspace/vpn && sudo openvpn --config client.ovpn
```

Full and minimal images include OpenVPN, `ip`, and TUN access. Run the VPN in
one recorded terminal and tools in another. `--vpn FILE` instead creates a
gluetun sidecar; it is a different workflow.

If Docker's build resolver cannot reach package repositories while host DNS
works, build with `POMDOCK_BUILD_NETWORK=host pomdock docker build`. This changes
build networking only; engagement containers retain their selected route.

## Dotfiles

The container prompt shows Berlin time when drawn, the instance, directory, and
observed HTTPS exit IP. The IP refresh runs in the background with a 30-second
cache; unavailable or stale results stay hidden. Set `POMDOCK_EXITIP=0` to disable
the lookup. Recorder timestamps still capture command execution separately.
Directory listing aliases retain colors but omit font-dependent file icons.

`PENTEST_DOTFILES_DIR` (default `~/pcm.dot`) is mounted at `/home/kali/dotfiles` and
symlinked to `~/pcm.dot` inside the container. Dotfiles are baked into the image at
build time and live-mounted at runtime, so edits take effect without a rebuild.

## Tools

See [tools.md](tools.md) for what's installed. Edit the `PENTEST_*` arrays in
`setup-pentest.sh` and run `pomdock docker build`. `setup-pentest.sh` also runs standalone
on any Kali/Debian host.

## Burp Suite

Start Burp natively on the host. `pomdock docker burp NAME` reports that engagement's
running VPN proxy endpoint; it errors if unavailable and does not launch Burp.
Use either proxy direction below as needed.

### Container → Burp (intercept the container's traffic)

Point the container's tools at Burp as their HTTP proxy. Burp must listen on an address
the container can reach, so bind its listener to the **Docker bridge gateway**, not to
"All interfaces", which would expose Burp to the whole LAN.

1. Find the bridge gateway IP on the host (usually `172.17.0.1`):

   ```bash
   ip -4 addr show docker0 | awk '/inet /{print $2}'    # e.g. 172.17.0.1/16 → 172.17.0.1
   ```

2. In Burp: Proxy → Proxy settings → Proxy listeners → edit the listener → **Bind to
   address: Specific address → `172.17.0.1`**, keep the port (e.g. `8081`).

3. From the container, use that gateway as the proxy:

   ```bash
   curl    -x http://172.17.0.1:8081 http://target/
   curl -k -x http://172.17.0.1:8081 https://target/     # -k skips Burp's CA check
   ```

Only the host and containers on the bridge can reach this listener; the LAN cannot.

The Kali container reaches the host at this gateway in every network mode (it is a
directly connected route, not the default route the VPN/Tor tunnel replaces). Note that
Burp's own traffic to the target then leaves from the **host**, bypassing the container's
VPN or Tor. If the engagement requires all traffic through the tunnel, also chain Burp's
upstream to gluetun (below) so Burp's egress follows the VPN.

### Burp → gluetun (send Burp's own traffic through the VPN)

- Burp: Project options → Connections → Upstream proxy servers → `localhost:8888`

gluetun's HTTP proxy is published on `127.0.0.1:8888` (loopback only), so Burp reaches it
on the host and its requests exit via the VPN. When several VPN engagements run at once,
each one's gluetun takes the next free port (8888, 8889, …) instead of colliding; the exact
port is printed when the sidecar comes up and by `pomdock docker burp ENGAGEMENT`.

## Environment variables

| Var | Default | Purpose |
|---|---|---|
| `PENTEST_DOTFILES_DIR` | `~/pcm.dot` | Dotfiles for build context and runtime mount |
| `POMDOCK_WORKSPACES_DIR` | `~/pomdock/workspaces` | Host workspace root |
| `PENTEST_LOOT_DIR` | unset | Legacy exact workspace override |
