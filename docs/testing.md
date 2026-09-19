# Testing

## Go unit tests

```bash
cd cli && go test ./...
```

## Build test

```bash
./test-build.sh
```

## Network isolation tests

`test-network.sh` verifies each network mode actually isolates traffic: it brings up the
sidecars, runs egress-IP, DNS-leak, and Tor checks inside a throwaway container, then
tears everything down.

```bash
./test-network.sh                       # Docker bridge
./test-network.sh --vpn FILE            # VPN kill switch
./test-network.sh --whonix              # Tor
./test-network.sh --vpn FILE --whonix   # Tor over VPN
./test-network.sh --vm NAME             # a VM
./test-network.sh --vm NAME --vm-whonix # a VM with Whonix routing
```

What each check confirms:

- **Egress IP** via `am.i.mullvad.net/json`, the exit is the tunnel, not your ISP.
- **DNS leak** via a TXT lookup, resolution goes through the tunnel.
- **Tor** via `check.torproject.org`, `IsTor` is true where it should be. In the
  Tor-over-VPN stack `IsTor` is false, because the site sees the VPN exit.
