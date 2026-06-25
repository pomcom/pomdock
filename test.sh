#!/bin/bash
# test.sh — verify pentest image has all expected tools
# Run inside the container: docker exec pcm-pentest bash ~/dotfiles/pentest/test.sh
# Or via test-build.sh for full build+test+teardown.

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BOLD='\033[1m'
RESET='\033[0m'

PASS=0; FAIL=0; WARN=0

pass() { echo -e "  ${GREEN}[PASS]${RESET} $1"; ((PASS++)); }
fail() { echo -e "  ${RED}[FAIL]${RESET} $*"; ((FAIL++)); }
warn() { echo -e "  ${YELLOW}[WARN]${RESET} $*"; ((WARN++)); }
info() { echo -e "         $*"; }
section() { echo -e "\n${BOLD}$1${RESET}"; }

has() { command -v "$1" &>/dev/null; }

find_bin() {
    local bin="$1"
    local found
    found=$(find /home/kali/go/bin /usr/local/bin /usr/bin ~/.local/bin \
                 -maxdepth 1 -name "$bin" 2>/dev/null | head -1)
    echo "${found:-not found in go/bin, /usr/local/bin, /usr/bin, ~/.local/bin}"
}

# ── Go tools (setup-pentest.sh: PENTEST_GO) ──────────────────────

section "── Go tools ────────────────────────────────────"

for bin in ffuf gobuster nuclei httpx subfinder katana naabu dnsx \
           alterx gitleaks gospider jsluice tlsx asnmap \
           mapcidr interactsh-client uncover cvemap; do
    if has "$bin"; then
        pass "$bin  ($(command -v "$bin"))"
    else
        fail "$bin not found"
        info "looked in: $(find_bin "$bin")"
        info "GOPATH=${GOPATH:-unset}  PATH contains go/bin: $(echo "$PATH" | grep -q go/bin && echo yes || echo NO)"
    fi
done

# ── Binary releases (setup-pentest.sh: PENTEST_BINS) ─────────────

section "── Binary releases ─────────────────────────────"

for bin in feroxbuster trufflehog gowitness rustscan; do
    if has "$bin"; then
        pass "$bin  ($(command -v "$bin"))"
    else
        fail "$bin not found"
        info "looked in: $(find_bin "$bin")"
    fi
done

# ── Apt tools (subset — verify the key ones) ─────────────────────

section "── Apt tools ───────────────────────────────────"

for bin in nmap ncat masscan whatweb sqlmap nikto wfuzz \
           wireshark tcpdump curl wget whois \
           smbclient crackmapexec enum4linux-ng \
           netexec hydra john hashcat; do
    if has "$bin"; then
        pass "$bin  ($(command -v "$bin"))"
    else
        fail "$bin not found"
        dpkg_status=$(dpkg -l "$bin" 2>/dev/null | grep "^ii" | awk '{print $3}' || true)
        if [[ -n "$dpkg_status" ]]; then
            info "dpkg shows installed ($dpkg_status) but binary missing from PATH"
        else
            info "not in dpkg either — install failed in setup-pentest.sh"
        fi
    fi
done

# ── Python tools (pipx) ──────────────────────────────────────────

section "── Python tools ────────────────────────────────"

for bin in impacket-secretsdump snallygaster; do
    if has "$bin"; then
        pass "$bin  ($(command -v "$bin"))"
    else
        fail "$bin not found"
        pipx_list=$(pipx list 2>/dev/null | grep -i "${bin%%-*}" || true)
        if [[ -n "$pipx_list" ]]; then
            info "pipx shows package installed but binary missing: $pipx_list"
            info "PATH contains ~/.local/bin: $(echo "$PATH" | grep -q '.local/bin' && echo yes || echo NO)"
        else
            info "not in pipx list either — install failed in setup-pentest.sh"
        fi
    fi
done

# ── Shell environment ─────────────────────────────────────────────

section "── Shell environment ────────────────────────────"

for bin in zsh tmux git fzf nvim; do
    if has "$bin"; then
        pass "$bin  ($(command -v "$bin"))"
    else
        fail "$bin not found"
        info "$(find_bin "$bin")"
    fi
done

if has atuin || [[ -f "$HOME/.atuin/bin/atuin" ]]; then
    pass "atuin  ($(command -v atuin 2>/dev/null || echo "$HOME/.atuin/bin/atuin"))"
else
    fail "atuin not found"
    info "expected at ~/.atuin/bin/atuin or in PATH"
    info "setup-shell.sh installs via: curl -LsSf https://setup.atuin.sh | sh"
fi

if has starship; then
    pass "starship  ($(command -v starship))"
else
    warn "starship not found — shell prompt will be plain"
fi

# ── Functional smoke tests ───────────────────────────────────────

section "── Smoke tests ─────────────────────────────────"

smoke() {
    local label="$1"; shift
    local out
    if out=$("$@" 2>&1); then
        pass "$label"
    else
        fail "$label"
        info "cmd: $*"
        echo "$out" | head -3 | sed 's/^/         /'
    fi
}

smoke "nmap --version"    nmap --version
# PD httpx lands in go/bin; /usr/bin/httpx (kali apt) is a different tool with different flags
if [[ -f /home/kali/go/bin/httpx ]]; then
    smoke "httpx --version"   /home/kali/go/bin/httpx --version
else
    warn "httpx (ProjectDiscovery) not in go/bin — rebuild required (apt version shadowed it during build)"
fi
smoke "nuclei --version"  nuclei --version
smoke "gobuster --help"   gobuster --help

if has java; then
    java_ver=$(java -version 2>&1 | head -1)
    pass "java — $java_ver"
else
    warn "java not found — Burp Pro won't work"
    info "expected from apt package: default-jre"
fi

# network — show egress IP or explain failure
curl_out=$(curl -sf --max-time 5 https://api.ipify.org 2>&1) && curl_ok=true || curl_ok=false
if $curl_ok; then
    pass "outbound HTTPS — egress IP: $curl_out"
else
    warn "outbound HTTPS failed"
    info "curl error: $curl_out"
    info "possible causes: no network, VPN kill switch active, DNS broken"
    dns_check=$(nslookup api.ipify.org 2>&1 | head -3 || true)
    info "DNS check: $dns_check"
fi

# ── Mounts ───────────────────────────────────────────────────────

section "── Mounts ──────────────────────────────────────"

if [[ -d /home/kali/pentest ]]; then
    pass "/home/kali/pentest mounted"
else
    warn "/home/kali/pentest not mounted"
    info "start via pentest.sh, not docker run directly"
fi

if [[ -d /home/kali/dotfiles ]]; then
    pass "/home/kali/dotfiles mounted"
else
    warn "/home/kali/dotfiles not mounted"
    info "pentest.sh mounts \$DOTFILES_DIR (~/dotfiles) automatically"
fi

if [[ -d /home/kali/.local/share/atuin ]]; then
    pass "atuin data dir present (/home/kali/.local/share/atuin)"
else
    warn "atuin data dir missing — history won't persist across container rm"
    info "pentest.sh mounts \${LOOT_DIR}/.atuin here automatically"
fi

# ── Summary ──────────────────────────────────────────────────────

echo ""
echo -e "${BOLD}────────────────────────────────────────────────${RESET}"
echo -e "  ${GREEN}${PASS} passed${RESET}  ${RED}${FAIL} failed${RESET}  ${YELLOW}${WARN} warnings${RESET}"
echo -e "${BOLD}────────────────────────────────────────────────${RESET}"
echo ""

[[ $FAIL -eq 0 ]]
