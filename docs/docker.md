# Docker containers

```bash
pomdock docker build
pomdock docker exec                                    # plain
pomdock docker exec --vpn FILE                          # VPN kill switch
pomdock docker exec --whonix                            # Tor
pomdock docker exec --whonix --vpn FILE                 # Tor over VPN
pomdock docker exec --name NAME --vpn FILE              # named engagement
pomdock docker status
pomdock docker stop [--name NAME]
pomdock docker rm   [--name NAME]
pomdock docker logs [--name NAME]
pomdock docker burp
```

`exec` is idempotent: it attaches if the container is running, restarts it if stopped,
and builds and creates it if it does not exist. Switching network mode recreates the
container automatically.

## Network modes

| Flags | Path |
|---|---|
| none | Docker bridge |
| `--vpn FILE` | Kali → gluetun → VPN |
| `--whonix` | Kali → Tor |
| `--whonix --vpn FILE` | Kali → Tor → VPN |

The Kali container always shares the sidecar's network namespace, so a crashed or
misconfigured tool cannot bypass the tunnel. `--vpn` accepts `.ovpn` (OpenVPN) or
`.conf` (WireGuard) from any provider.

The route is recorded on the container (labels `io.pomdock.route` and
`io.pomdock.vpn-file`), so a stopped engagement reconnects with just
`pomdock docker exec --name NAME`: the same VPN config and/or Tor routing is reused.
Passing `--vpn`/`--whonix` again overrides the recorded route. If the recorded config
file has since moved, `exec` asks for `--vpn` explicitly.

## Named engagements

`--name NAME` gives an engagement its own container, its own sidecars
(`NAME-gluetun` / `NAME-whonix`), its own loot dir at `~/pentest/NAME`, and a separate
Atuin history. Without `--name` everything lands in the default `~/pentest`.

## Dotfiles

`PENTEST_DOTFILES_DIR` (default `~/pcm.dot`) is mounted at `/home/kali/dotfiles` and
symlinked to `~/pcm.dot` inside the container. Dotfiles are baked into the image at
build time and live-mounted at runtime, so edits take effect without a rebuild.

## Tools

See [tools.md](tools.md) for what's installed. Edit the `PENTEST_*` arrays in
`setup-pentest.sh` and run `pomdock docker build`. `setup-pentest.sh` also runs standalone
on any Kali/Debian host.

## Burp Suite

Burp runs natively on the host, not inside the container. `pomdock docker burp` prints
the setup reminder. Wire it up in one of two directions depending on what you need.

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
on the host and its requests exit via the VPN.

## Environment variables

| Var | Default | Purpose |
|---|---|---|
| `PENTEST_DOTFILES_DIR` | `~/pcm.dot` | Dotfiles for build context and runtime mount |
| `PENTEST_LOOT_DIR` | `~/pentest` | Loot directory |
