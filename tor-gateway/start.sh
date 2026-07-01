#!/bin/sh
set -e

TOR_UID=$(id -u tor)

# DNS bridge: dnsmasq listens on 127.0.0.1:53 and forwards to Tor's DNSPort (5353).
# Runs as root before exec-ing tor (root can bind port 53).
# Kali container's /etc/resolv.conf is set to nameserver 127.0.0.1 by pentest.sh
# so DNS goes directly to this bridge — no conntrack/REDIRECT needed for UDP.
dnsmasq --no-daemon --no-hosts --no-resolv \
    --listen-address=127.0.0.1 --port=53 \
    --server=127.0.0.1#5353 &

# Flush existing rules
iptables -F
iptables -t nat -F

# NAT table: exempt Tor's own traffic (prevents redirect loop)
iptables -t nat -A OUTPUT -m owner --uid-owner "$TOR_UID" -j RETURN

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

# FILTER table: kill switch — only Tor and loopback leave this namespace.
# After REDIRECT in nat OUTPUT, FILTER sees dest=127.x, so redirected TCP and
# DNS (via socat on 127.0.0.1:53) all pass the 127.0.0.0/8 rule.
iptables -A OUTPUT -m owner --uid-owner "$TOR_UID" -j ACCEPT
iptables -A OUTPUT -d 127.0.0.0/8    -j ACCEPT
iptables -A OUTPUT -d 10.0.0.0/8     -j ACCEPT
iptables -A OUTPUT -d 172.16.0.0/12  -j ACCEPT
iptables -A OUTPUT -d 192.168.0.0/16 -j ACCEPT
iptables -A OUTPUT -j DROP

exec tor -f /etc/tor/torrc
