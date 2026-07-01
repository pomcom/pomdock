#!/bin/sh
set -e

TOR_UID=$(id -u tor)

# Flush existing rules
iptables -F
iptables -t nat -F

# NAT table: exempt Tor's own traffic (prevents redirect loop)
iptables -t nat -A OUTPUT -m owner --uid-owner "$TOR_UID" -j RETURN

# NAT table: .onion virtual range MUST be redirected before the RFC1918 exemption.
# torrc sets VirtualAddrNetworkIPv4 10.192.0.0/10 — AutomapHostsOnResolve maps
# .onion hostnames into this range. Without this rule, 10.192.x.x falls into the
# 10.0.0.0/8 RETURN below and never reaches Tor's TransPort.
iptables -t nat -A OUTPUT -d 10.192.0.0/10 -p tcp -j REDIRECT --to-ports 9040

# NAT table: exempt loopback and RFC1918 (Docker internal, don't torify)
iptables -t nat -A OUTPUT -d 127.0.0.0/8    -j RETURN
iptables -t nat -A OUTPUT -d 10.0.0.0/8     -j RETURN
iptables -t nat -A OUTPUT -d 172.16.0.0/12  -j RETURN
iptables -t nat -A OUTPUT -d 192.168.0.0/16 -j RETURN

# NAT table: redirect all remaining TCP to Tor TransPort
iptables -t nat -A OUTPUT -p tcp -j REDIRECT --to-ports 9040

# NAT table: redirect DNS to Tor's DNSPort
iptables -t nat -A OUTPUT -p udp --dport 53 -j REDIRECT --to-ports 5353

# FILTER table: kill switch — only Tor and loopback leave this namespace.
# After REDIRECT in nat OUTPUT, FILTER sees dest=127.x (loopback), so redirected
# packets (.onion → 9040, DNS → 5353, TCP → 9040) all pass the 127.0.0.0/8 rule.
iptables -A OUTPUT -m owner --uid-owner "$TOR_UID" -j ACCEPT
iptables -A OUTPUT -d 127.0.0.0/8    -j ACCEPT
iptables -A OUTPUT -d 10.0.0.0/8     -j ACCEPT
iptables -A OUTPUT -d 172.16.0.0/12  -j ACCEPT
iptables -A OUTPUT -d 192.168.0.0/16 -j ACCEPT
iptables -A OUTPUT -j DROP

exec tor -f /etc/tor/torrc
