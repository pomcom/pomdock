#!/bin/sh
set -e

TOR_UID=$(id -u tor)

# Flush existing rules
iptables -F
iptables -t nat -F

# NAT table: exempt Tor's own traffic (prevents redirect loop)
iptables -t nat -A OUTPUT -m owner --uid-owner "$TOR_UID" -j RETURN

# NAT table: exempt loopback and RFC1918 (Docker internal, don't torify)
iptables -t nat -A OUTPUT -d 127.0.0.0/8   -j RETURN
iptables -t nat -A OUTPUT -d 10.0.0.0/8    -j RETURN
iptables -t nat -A OUTPUT -d 172.16.0.0/12 -j RETURN
iptables -t nat -A OUTPUT -d 192.168.0.0/16 -j RETURN

# NAT table: redirect all remaining TCP to Tor TransPort
iptables -t nat -A OUTPUT -p tcp -j REDIRECT --to-ports 9040

# NAT table: redirect DNS to Tor's DNSPort
iptables -t nat -A OUTPUT -p udp --dport 53 -j REDIRECT --to-ports 5353

# FILTER table: kill switch — only Tor and loopback leave this namespace
iptables -A OUTPUT -m owner --uid-owner "$TOR_UID" -j ACCEPT
# After REDIRECT in nat OUTPUT, FILTER sees dest=127.x before re-routing to lo
iptables -A OUTPUT -d 127.0.0.0/8   -j ACCEPT
iptables -A OUTPUT -d 10.0.0.0/8    -j ACCEPT
iptables -A OUTPUT -d 172.16.0.0/12 -j ACCEPT
iptables -A OUTPUT -d 192.168.0.0/16 -j ACCEPT
iptables -A OUTPUT -j DROP

exec tor -f /etc/tor/torrc
