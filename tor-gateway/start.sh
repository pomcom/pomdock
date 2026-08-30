#!/bin/sh
set -e

TOR_UID=$(id -u tor)

# STACK_MODE=1 means this container shares gluetun's network namespace
# (Tor-over-VPN). gluetun owns that namespace's kill switch already (its
# FILTER OUTPUT chain ends in an unconditional ACCEPT on the tun0 interface,
# so anything routed through the tunnel — Tor's traffic included, once
# NAT-redirected — is already allowed). Flushing FILTER here would wipe
# gluetun's own rules (its VPN-endpoint handshake ACCEPT, its tun0 ACCEPT),
# and re-running gluetun's health checks would keep reasserting them out
# from under Tor, so in stack mode we leave FILTER alone entirely and only
# touch NAT (to redirect traffic into Tor). See kali-vm docs / README for
# the "stack" network mode.
if [ -z "$STACK_MODE" ]; then
    STACK_MODE=0
fi

# DNS bridge: dnsmasq listens on 127.0.0.1:53 and forwards to Tor's DNSPort (5353).
# Runs as root before exec-ing tor (root can bind port 53).
# Kali container's /etc/resolv.conf is set to nameserver 127.0.0.1 by pentest.sh
# so DNS goes directly to this bridge — no conntrack/REDIRECT needed for UDP.
# In stack mode, gluetun already binds 127.0.0.1:53/[::]:53 for its own DNS
# forwarder, so this bind fails harmlessly — DNS then rides through Tor via
# whatever gluetun forwards, same as its other TCP/UDP traffic.
dnsmasq --no-daemon --no-hosts --no-resolv \
    --listen-address=127.0.0.1 --port=53 \
    --server=127.0.0.1#5353 &

# Flush existing NAT rules (always ours to own — gluetun does not use the NAT table)
iptables -t nat -F

# NAT table: exempt Tor's own traffic (prevents redirect loop)
iptables -t nat -A OUTPUT -m owner --uid-owner "$TOR_UID" -j RETURN

# Stack mode: exempt gluetun's own traffic (it runs as root) from the Tor
# redirect below. Without this, gluetun's healthcheck/DNS-over-TLS/public-IP
# lookups get redirected into Tor's TransPort before Tor has bootstrapped,
# fail, and gluetun restarts the tunnel (reasserting its firewall) in a loop
# that never lets Tor finish a handshake.
if [ "$STACK_MODE" = "1" ]; then
    iptables -t nat -A OUTPUT -m owner --uid-owner 0 -j RETURN
fi

# NAT table: .onion virtual range MUST be redirected before the RFC1918 exemption.
# torrc sets VirtualAddrNetworkIPv4 10.192.0.0/10 — AutomapHostsOnResolve maps
# .onion hostnames into this range.
iptables -t nat -A OUTPUT -d 10.192.0.0/10 -p tcp -j REDIRECT --to-ports 9040

# NAT table: exempt loopback and RFC1918 (Docker internal, don't torify)
iptables -t nat -A OUTPUT -d 127.0.0.0/8    -j RETURN
iptables -t nat -A OUTPUT -d 10.0.0.0/8     -j RETURN
iptables -t nat -A OUTPUT -d 172.16.0.0/12  -j RETURN
iptables -t nat -A OUTPUT -d 192.168.0.0/16 -j RETURN

# NAT table: redirect all remaining TCP to Tor TransPort
iptables -t nat -A OUTPUT -p tcp -j REDIRECT --to-ports 9040

if [ "$STACK_MODE" != "1" ]; then
    # Standalone (own network namespace): FILTER table kill switch — only
    # Tor and loopback leave this namespace. After REDIRECT in nat OUTPUT,
    # FILTER sees dest=127.x, so redirected TCP and DNS (via dnsmasq on
    # 127.0.0.1:53) all pass the 127.0.0.0/8 rule.
    iptables -F
    iptables -A OUTPUT -m owner --uid-owner "$TOR_UID" -j ACCEPT
    iptables -A OUTPUT -d 127.0.0.0/8    -j ACCEPT
    iptables -A OUTPUT -d 10.0.0.0/8     -j ACCEPT
    iptables -A OUTPUT -d 172.16.0.0/12  -j ACCEPT
    iptables -A OUTPUT -d 192.168.0.0/16 -j ACCEPT
    iptables -A OUTPUT -j DROP
fi

exec tor -f /etc/tor/torrc
