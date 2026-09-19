# Docker containers

Full reference for `pomdock docker`. For the short version see the [README](../README.md).

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

The installed tool set is defined by the arrays `PENTEST_APT`, `PENTEST_GO`,
`PENTEST_BINS`, and `PENTEST_PIP` in `setup-pentest.sh`. Edit those, then run
`pomdock docker build`. `setup-pentest.sh` also runs standalone on any Kali/Debian host.

## Burp Suite

Burp runs natively on the host, not inside the container. `pomdock docker burp` just
prints the setup reminder: point Burp's upstream proxy at gluetun's HTTP proxy so its
traffic follows the VPN.

- Burp: Project options → Connections → Upstream proxy servers → `localhost:8888`

## Environment variables

| Var | Default | Purpose |
|---|---|---|
| `PENTEST_DOTFILES_DIR` | `~/pcm.dot` | Dotfiles for build context and runtime mount |
| `PENTEST_LOOT_DIR` | `~/pentest` | Loot directory |
