#!/bin/bash
# test-network.sh — network integration tests for all container stack modes
#
# For each mode: starts the stack, checks egress IP / DNS leak / Tor status,
# prints full network config, asserts no leaks, then tears down.
#
# Usage:
#   ./test-network.sh                          # plain (Docker bridge) only
#   ./test-network.sh --vpn /path/to/wg.conf  # plain + vpn
#   ./test-network.sh --whonix                 # plain + whonix (Tor)
#   ./test-network.sh --vpn FILE --whonix      # all 4 modes
#   ./test-network.sh --mode vpn --vpn FILE    # single mode
#   ./test-network.sh --no-teardown            # keep containers for inspection
#
# Modes:  plain | vpn | whonix | stack
# The 'stack' mode (Kali → Tor → VPN) requires both --vpn and --whonix.

set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
IMAGE="pcm-kali-pentest"
WHONIX_IMAGE="pcm-tor-gateway"
GLUETUN_IMAGE="qmcgaw/gluetun"
RUN_ID="nettest-$$"
INNER_SCRIPT="/tmp/netcheck-${RUN_ID}.sh"

VPN_FILE=""
USE_WHONIX=false
NO_TEARDOWN=false
MODE_FILTER=""

# ── Arg parsing ────────────────────────────────────────────────────

while [[ $# -gt 0 ]]; do
    case "$1" in
        --vpn)
            [[ -z "${2:-}" ]] && { echo "[!] --vpn requires a file path"; exit 1; }
            VPN_FILE="$2"; shift 2 ;;
        --whonix)
            USE_WHONIX=true; shift ;;
        --no-teardown)
            NO_TEARDOWN=true; shift ;;
        --mode)
            [[ -z "${2:-}" ]] && { echo "[!] --mode requires: plain|vpn|whonix|stack"; exit 1; }
            MODE_FILTER="$2"
            [[ "$MODE_FILTER" == "whonix" || "$MODE_FILTER" == "stack" ]] && USE_WHONIX=true
            shift 2 ;;
        -h|--help)
            sed -n '2,16p' "$0" | sed 's/^# \?//'; exit 0 ;;
        *) echo "[!] Unknown argument: $1"; exit 1 ;;
    esac
done

# ── Colors / output ────────────────────────────────────────────────

RED='\033[0;31m'; GREEN='\033[0;32m'; YELLOW='\033[1;33m'
CYAN='\033[0;36m'; BOLD='\033[1m'; RESET='\033[0m'

info() { echo -e "  [*] $*"; }
ok()   { echo -e "  ${GREEN}[+]${RESET} $*"; }
err()  { echo -e "  ${RED}[!]${RESET} $*"; }

# ── Cleanup ────────────────────────────────────────────────────────

cleanup() {
    rm -f "$INNER_SCRIPT"
    local remaining
    remaining=$(docker ps -a --filter "name=${RUN_ID}" --format "{{.Names}}" 2>/dev/null || true)
    [[ -z "$remaining" ]] && return
    if [[ "$NO_TEARDOWN" == true ]]; then
        echo ""
        info "Containers kept (--no-teardown):"
        echo "$remaining" | sed 's/^/    /'
        info "Remove: docker rm -f \$(docker ps -a --filter name=${RUN_ID} -q)"
    else
        echo ""
        info "Cleaning up..."
        echo "$remaining" | xargs -r docker rm -f >/dev/null 2>&1 || true
    fi
}

trap cleanup EXIT

teardown_stack() {
    local names=("$@")
    for name in "${names[@]}"; do
        [[ -n "$name" ]] && docker rm -f "$name" >/dev/null 2>&1 || true
    done
}

# ── Sidecar: gluetun ──────────────────────────────────────────────

start_gluetun() {
    local vpn_file="$1"
    local name="$2"

    [[ ! -f "$vpn_file" ]] && { err "VPN config not found: $vpn_file"; return 1; }

    local vpn_dir vpn_base vpn_type
    vpn_dir=$(dirname "$(realpath "$vpn_file")")
    vpn_base=$(basename "$vpn_file")
    vpn_type="openvpn"
    [[ "$vpn_base" == *.conf ]] && vpn_type="wireguard"

    info "Starting gluetun [$vpn_type] as $name..."

    if [[ "$vpn_type" == "wireguard" ]]; then
        local priv_key addresses pub_key psk endpoint_host endpoint_port endpoint_ip
        _wgval() { grep -E "^\s*$1\s*=" "$vpn_file" | sed 's/^[^=]*=[[:space:]]*//' | head -n1 | tr -d '\r\n' || true; }
        priv_key=$(_wgval PrivateKey)
        addresses=$(_wgval Address | tr ',' '\n' | grep -v ':' | tr -d ' ' | head -n1)
        pub_key=$(_wgval PublicKey)
        psk=$(_wgval PresharedKey)
        local raw_endpoint
        raw_endpoint=$(_wgval Endpoint)
        endpoint_host="${raw_endpoint%%:*}"
        endpoint_port="${raw_endpoint##*:}"
        if [[ "$endpoint_host" =~ ^[0-9]+\.[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
            endpoint_ip="$endpoint_host"
        else
            endpoint_ip=$(getent hosts "$endpoint_host" 2>/dev/null | awk '{print $1; exit}')
            [[ -z "$endpoint_ip" ]] && endpoint_ip=$(dig +short "$endpoint_host" 2>/dev/null | grep -E '^[0-9.]+$' | head -n1)
            [[ -z "$endpoint_ip" ]] && { err "Could not resolve WireGuard endpoint: $endpoint_host"; return 1; }
        fi
        info "Endpoint: $endpoint_host → $endpoint_ip:$endpoint_port"

        local gluetun_args=(
            -e VPN_SERVICE_PROVIDER=custom
            -e VPN_TYPE=wireguard
            -e WIREGUARD_PRIVATE_KEY="$priv_key"
            -e WIREGUARD_ADDRESSES="$addresses"
            -e WIREGUARD_ENDPOINT_IP="$endpoint_ip"
            -e WIREGUARD_ENDPOINT_PORT="$endpoint_port"
            -e WIREGUARD_PUBLIC_KEY="$pub_key"
        )
        [[ -n "$psk" ]] && gluetun_args+=(-e WIREGUARD_PRESHARED_KEY="$psk")

        docker run -d --name "$name" \
            --cap-add NET_ADMIN \
            --device /dev/net/tun \
            "${gluetun_args[@]}" \
            "$GLUETUN_IMAGE" >/dev/null
    else
        docker run -d --name "$name" \
            --cap-add NET_ADMIN \
            --device /dev/net/tun \
            -v "${vpn_dir}/${vpn_base}:/gluetun/custom.conf:ro" \
            -e VPN_SERVICE_PROVIDER=custom \
            -e VPN_TYPE=openvpn \
            "$GLUETUN_IMAGE" >/dev/null
    fi

    info "Waiting for VPN tunnel..."
    local i
    for i in $(seq 1 60); do
        if ! docker inspect -f '{{.State.Running}}' "$name" 2>/dev/null | grep -q true; then
            err "gluetun exited: $(docker logs --tail 5 "$name" 2>&1 | tr '\n' ' ')"
            return 1
        fi
        if docker logs "$name" 2>&1 | grep -q "Public IP address is"; then
            local vpn_ip
            vpn_ip=$(docker logs "$name" 2>&1 | grep "Public IP address is" | tail -1 | grep -oP '\d+\.\d+\.\d+\.\d+' | head -1)
            ok "VPN connected — exit IP: $vpn_ip"
            return 0
        fi
        echo -n "."
        sleep 1
    done
    echo ""
    err "VPN did not connect after 60s — docker logs $name"
    return 1
}

# ── Sidecar: Whonix/Tor ───────────────────────────────────────────

start_whonix() {
    local name="$1"
    local net_arg="${2:-}"

    if ! docker image inspect "$WHONIX_IMAGE" &>/dev/null; then
        info "Building Tor gateway image..."
        docker build -q -t "$WHONIX_IMAGE" "${SCRIPT_DIR}/tor-gateway"
    fi

    info "Starting Tor gateway as $name..."

    local run_args=(
        --name "$name"
        --cap-add NET_ADMIN
        --cap-add NET_RAW
        --device /dev/net/tun
    )
    [[ -n "$net_arg" ]] && run_args+=(--network "$net_arg")

    docker run -d "${run_args[@]}" "$WHONIX_IMAGE" >/dev/null

    info "Waiting for Tor bootstrap..."
    local i
    for i in $(seq 1 90); do
        if ! docker inspect -f '{{.State.Running}}' "$name" 2>/dev/null | grep -q true; then
            err "Whonix gateway exited: $(docker logs --tail 5 "$name" 2>&1 | tr '\n' ' ')"
            return 1
        fi
        if docker logs "$name" 2>&1 | grep -q "Bootstrapped 100%"; then
            ok "Tor connected"
            return 0
        fi
        echo -n "."
        sleep 1
    done
    echo ""
    err "Tor bootstrap timed out — docker logs $name"
    return 1
}

# ── Inner check script (runs inside the Kali container) ───────────
# Written to /tmp, volume-mounted read-only into each test container.

cat > "$INNER_SCRIPT" <<'INNER_EOF'
#!/bin/bash
set -uo pipefail

MODE="${1:-unknown}"
EXPECTED_TOR="${2:-false}"

RED='\033[0;31m'; GREEN='\033[0;32m'; YELLOW='\033[1;33m'; BOLD='\033[1m'; RESET='\033[0m'
INNER_PASS=0; INNER_FAIL=0

pass() { echo -e "    ${GREEN}✓${RESET} $1"; ((INNER_PASS++)); }
fail() { echo -e "    ${RED}✗${RESET} $1"; ((INNER_FAIL++)); }
warn() { echo -e "    ${YELLOW}⚠${RESET} $1"; }
hdr()  { echo -e "\n  ${BOLD}── $1 ──${RESET}"; }

echo -e "\n  ${BOLD}Mode: ${MODE}${RESET}"

# ── Network configuration ──────────────────────────────────────────

hdr "Interfaces"
ip -brief addr 2>/dev/null || ip addr show 2>/dev/null || true

hdr "Routes"
ip route show 2>/dev/null || true

hdr "DNS — /etc/resolv.conf"
cat /etc/resolv.conf

# ── Egress IP + identity (Mullvad) ───────────────────────────────

hdr "Egress IP"
EGRESS_IP=""
MULLVAD_JSON=$(curl -sf --max-time 15 https://am.i.mullvad.net/json 2>/dev/null || true)
if [[ -n "$MULLVAD_JSON" ]]; then
    EGRESS_IP=$(echo "$MULLVAD_JSON" | grep -o '"ip":"[^"]*"' | cut -d'"' -f4 || true)
    M_COUNTRY=$(echo "$MULLVAD_JSON" | grep -o '"country":"[^"]*"' | cut -d'"' -f4 || true)
    M_CITY=$(echo "$MULLVAD_JSON" | grep -o '"city":"[^"]*"' | cut -d'"' -f4 || true)
    M_ORG=$(echo "$MULLVAD_JSON" | grep -o '"organization":"[^"]*"' | cut -d'"' -f4 || true)
    M_EXIT=$(echo "$MULLVAD_JSON" | grep -o '"mullvad_exit_ip":[^,}]*' | cut -d: -f2 | tr -d ' ' || true)
    M_BLACK=$(echo "$MULLVAD_JSON" | grep -oP '"blacklisted":\{"blacklisted":\K[^,}]*' || true)
    pass "Outbound HTTPS reachable — egress IP: $EGRESS_IP"
    echo "    Location:       ${M_CITY}, ${M_COUNTRY}"
    echo "    Organization:   ${M_ORG}"
    echo "    Mullvad exit:   ${M_EXIT}"
    echo "    Blacklisted:    ${M_BLACK}"
    CONNECTED=$(curl -sf --max-time 10 https://am.i.mullvad.net/connected 2>/dev/null || true)
    [[ -n "$CONNECTED" ]] && echo "    Mullvad status: ${CONNECTED}"
else
    fail "Outbound HTTPS FAILED — no connectivity or kill switch blocking"
fi

# ── DNS resolver check ────────────────────────────────────────────

hdr "DNS Resolver"
NS=$(grep '^nameserver' /etc/resolv.conf | awk '{print $2}' | head -1 || true)
echo "  Configured nameserver: ${NS:-(none)}"

if [[ -z "$NS" ]]; then
    warn "No nameserver in resolv.conf"
elif [[ "$NS" == "127.0.0.11" ]]; then
    warn "Nameserver is Docker embedded DNS (127.0.0.11) — DNS may bypass tunnel and reach host resolver"
elif [[ "$NS" == "127."* ]]; then
    pass "Nameserver is loopback ($NS) — DNS routes through local tunnel resolver (gluetun DoT or Tor DNSPort)"
elif [[ "$NS" =~ ^(10\.|172\.(1[6-9]|2[0-9]|3[01])\.|192\.168\.) ]]; then
    warn "Nameserver is private IP ($NS) — DNS via internal gateway (may or may not be torified)"
else
    fail "Nameserver is public IP ($NS) — DNS NOT routed through tunnel (DNS LEAK)"
fi

if command -v dig &>/dev/null; then
    echo ""
    echo "  dig am.i.mullvad.net (server that answered, query time):"
    dig am.i.mullvad.net 2>/dev/null | grep -E "^;; (SERVER|Query time|ANSWER SECTION)" | sed 's/^/    /' || true
fi

# ── DNS leak check ────────────────────────────────────────────────
# Google's o-o.myaddr.l.google.com TXT returns the IP that queried Google's NS.
# If DNS is tunnelled, this should match (or be consistent with) the HTTP egress IP.

hdr "DNS Leak"
DNS_EGRESS=""
if command -v dig &>/dev/null; then
    echo "  Query: dig +short TXT o-o.myaddr.l.google.com @ns1.google.com"
    DNS_EGRESS=$(dig +short TXT o-o.myaddr.l.google.com @ns1.google.com 2>/dev/null | tr -d '"' || true)
    echo "  DNS egress IP (as seen by Google NS): ${DNS_EGRESS:-(no response)}"

    if [[ -n "$DNS_EGRESS" && -n "$EGRESS_IP" ]]; then
        if [[ "$DNS_EGRESS" == "$EGRESS_IP" ]]; then
            pass "DNS egress matches HTTP egress ($EGRESS_IP) — no DNS leak"
        elif [[ "$EXPECTED_TOR" == "true" ]]; then
            warn "DNS egress ($DNS_EGRESS) ≠ HTTP egress ($EGRESS_IP) — Tor may use different exit nodes for each (normal)"
        else
            fail "DNS egress ($DNS_EGRESS) ≠ HTTP egress ($EGRESS_IP) — potential DNS leak"
        fi
    elif [[ -z "$DNS_EGRESS" ]]; then
        warn "No response from Google NS — DNS query may have been blocked or timed out"
    fi
else
    warn "dig not available — skipping DNS leak check"
fi

# ── Tor check ─────────────────────────────────────────────────────

hdr "Tor Status"
TOR_JSON=$(curl -sf --max-time 30 https://check.torproject.org/api/ip 2>/dev/null || true)
if [[ -z "$TOR_JSON" ]]; then
    warn "Could not reach check.torproject.org (no network or timeout)"
else
    IS_TOR=$(echo "$TOR_JSON" | grep -o '"IsTor":[^,}]*' | cut -d: -f2 | tr -d ' ' || true)
    TOR_IP=$(echo "$TOR_JSON" | grep -o '"IP":"[^"]*"' | cut -d'"' -f4 || true)
    echo "  check.torproject.org → IsTor=${IS_TOR}, IP=${TOR_IP}"
    if [[ "$EXPECTED_TOR" == "true" ]]; then
        [[ "$IS_TOR" == "true" ]] && pass "Traffic confirmed to exit via Tor" || fail "Expected Tor exit — got IsTor=${IS_TOR}"
    else
        [[ "$IS_TOR" == "false" ]] && pass "Traffic does NOT exit via Tor (expected)" || warn "Unexpected Tor exit — IsTor=${IS_TOR}"
    fi
fi

# ── Summary ───────────────────────────────────────────────────────

echo ""
echo -e "  ──────────────────────────────────────────────────────"
echo -e "  ${GREEN}${INNER_PASS} passed${RESET}  ${RED}${INNER_FAIL} failed${RESET}"

[[ $INNER_FAIL -eq 0 ]]
INNER_EOF

chmod 644 "$INNER_SCRIPT"

# ── Mode runner ────────────────────────────────────────────────────

MODE_RESULTS=()
OVERALL_PASS=true

run_check() {
    local mode_label="$1"
    local net_arg="${2:-}"    # container:<name> or empty for plain bridge
    local expected_tor="$3"

    echo ""
    echo -e "${BOLD}${CYAN}══════════════════════════════════════════════════════${RESET}"
    echo -e "${BOLD}${CYAN}  $mode_label${RESET}"
    echo -e "${BOLD}${CYAN}══════════════════════════════════════════════════════${RESET}"

    local run_args=(
        --rm
        --cap-add NET_ADMIN
        --cap-add NET_RAW
        -v "${INNER_SCRIPT}:/tmp/netcheck.sh:ro"
    )
    [[ -n "$net_arg" ]] && run_args+=(--network "$net_arg")

    local exit_code=0
    docker run "${run_args[@]}" "$IMAGE" bash /tmp/netcheck.sh "$mode_label" "$expected_tor" \
        || exit_code=$?

    if [[ $exit_code -eq 0 ]]; then
        MODE_RESULTS+=("${GREEN}✓${RESET} $mode_label")
    else
        MODE_RESULTS+=("${RED}✗${RESET} $mode_label")
        OVERALL_PASS=false
    fi
}

# ── Main ──────────────────────────────────────────────────────────

echo ""
echo -e "${BOLD}════════════════════════════════════════════════════════${RESET}"
echo -e "${BOLD}  pomdock — Network Stack Integration Tests${RESET}"
echo -e "${BOLD}════════════════════════════════════════════════════════${RESET}"
echo ""

docker image inspect "$IMAGE" &>/dev/null || {
    err "Image $IMAGE not found — build first: ./pentest.sh build"
    exit 1
}

HOST_JSON=$(curl -sf --max-time 10 https://am.i.mullvad.net/json 2>/dev/null || true)
HOST_IP=$(echo "$HOST_JSON" | grep -o '"ip":"[^"]*"' | cut -d'"' -f4 || echo "unreachable")
HOST_ORG=$(echo "$HOST_JSON" | grep -o '"organization":"[^"]*"' | cut -d'"' -f4 || true)
info "Host egress IP: $HOST_IP — ${HOST_ORG}  (compare against container egress below)"

# Determine which modes to run
RUN_PLAIN=false; RUN_VPN=false; RUN_WHONIX=false; RUN_STACK=false

case "${MODE_FILTER:-all}" in
    plain)   RUN_PLAIN=true ;;
    vpn)     RUN_VPN=true ;;
    whonix)  RUN_WHONIX=true ;;
    stack)   RUN_STACK=true ;;
    all)
        RUN_PLAIN=true
        [[ -n "$VPN_FILE" ]] && RUN_VPN=true
        [[ "$USE_WHONIX" == true ]] && RUN_WHONIX=true
        [[ -n "$VPN_FILE" && "$USE_WHONIX" == true ]] && RUN_STACK=true
        ;;
esac

# Validate requirements
[[ "$RUN_VPN" == true || "$RUN_STACK" == true ]] && [[ -z "$VPN_FILE" ]] && {
    err "VPN mode requires --vpn FILE"
    exit 1
}
[[ "$RUN_STACK" == true ]] && [[ "$USE_WHONIX" != true ]] && {
    err "Stack mode requires --whonix"
    exit 1
}

info "Modes to run:$(
    [[ "$RUN_PLAIN"  == true ]] && echo -n " plain"
    [[ "$RUN_VPN"    == true ]] && echo -n " vpn"
    [[ "$RUN_WHONIX" == true ]] && echo -n " whonix"
    [[ "$RUN_STACK"  == true ]] && echo -n " stack"
)"

# ── plain ─────────────────────────────────────────────────────────

if [[ "$RUN_PLAIN" == true ]]; then
    run_check "plain — Docker bridge" "" "false"
fi

# ── vpn ──────────────────────────────────────────────────────────

if [[ "$RUN_VPN" == true ]]; then
    GT="${RUN_ID}-gluetun"
    if start_gluetun "$VPN_FILE" "$GT"; then
        run_check "vpn — Kali → gluetun → VPN" "container:$GT" "false"
        [[ "$NO_TEARDOWN" == false ]] && teardown_stack "$GT"
    else
        MODE_RESULTS+=("${RED}✗${RESET} vpn — sidecar failed to start")
        OVERALL_PASS=false
    fi
fi

# ── whonix ───────────────────────────────────────────────────────

if [[ "$RUN_WHONIX" == true ]]; then
    WN="${RUN_ID}-whonix"
    if start_whonix "$WN" ""; then
        run_check "whonix — Kali → Tor" "container:$WN" "true"
        [[ "$NO_TEARDOWN" == false ]] && teardown_stack "$WN"
    else
        MODE_RESULTS+=("${RED}✗${RESET} whonix — sidecar failed to start")
        OVERALL_PASS=false
    fi
fi

# ── stack ─────────────────────────────────────────────────────────

if [[ "$RUN_STACK" == true ]]; then
    GT="${RUN_ID}-stack-gluetun"
    WN="${RUN_ID}-stack-whonix"
    if start_gluetun "$VPN_FILE" "$GT" && start_whonix "$WN" "container:$GT"; then
        # Stack: Tor exits via the VPN, so check.torproject.org sees the VPN exit (IsTor=false)
        run_check "stack — Kali → Tor → VPN" "container:$WN" "false"
        [[ "$NO_TEARDOWN" == false ]] && teardown_stack "$WN" "$GT"
    else
        teardown_stack "$WN" "$GT"
        MODE_RESULTS+=("${RED}✗${RESET} stack — sidecar failed to start")
        OVERALL_PASS=false
    fi
fi

# ── Summary ───────────────────────────────────────────────────────

echo ""
echo -e "${BOLD}════════════════════════════════════════════════════════${RESET}"
echo -e "${BOLD}  Results${RESET}"
for r in "${MODE_RESULTS[@]}"; do
    echo -e "    $r"
done
echo -e "${BOLD}════════════════════════════════════════════════════════${RESET}"
echo ""

[[ "$OVERALL_PASS" == true ]]
