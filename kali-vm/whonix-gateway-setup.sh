#!/usr/bin/env bash
# whonix-gateway-setup.sh — download and import the official Whonix Gateway KVM image
# Called by: vm whonix-gateway
set -euo pipefail
export LIBVIRT_DEFAULT_URI="qemu:///system"

IMAGE_DIR="/var/lib/libvirt/images"
DOWNLOAD_DIR="${POMDOCK_DOWNLOAD_DIR:-${HOME}/.cache/pomdock}/whonix"
GW_DISK="${IMAGE_DIR}/Whonix-Gateway.qcow2"

# ── Version detection ─────────────────────────────────────────────

echo "→ Detecting latest Whonix version..."
WHONIX_VERSION=$(curl -s https://download.whonix.org/libvirt/ \
    | grep -oP 'href="\K[0-9]+\.[0-9]+\.[0-9]+\.[0-9]+(?=/)' \
    | sort -V | tail -1)
[[ -z "$WHONIX_VERSION" ]] && { echo "✗ Could not detect Whonix version"; exit 1; }
echo "  Version: ${WHONIX_VERSION}"

ARCHIVE="Whonix-CLI-${WHONIX_VERSION}.Intel_AMD64.qcow2.libvirt.xz"
URL="https://download.whonix.org/libvirt/${WHONIX_VERSION}/${ARCHIVE}"

# ── Prereqs ───────────────────────────────────────────────────────

if virsh dominfo Whonix-Gateway &>/dev/null; then
    echo "✗ Whonix-Gateway VM already exists."
    echo "  Start it:  vm start Whonix-Gateway"
    echo "  Delete it: vm delete Whonix-Gateway"
    exit 1
fi

echo "→ Requesting sudo credentials (needed for disk install)..."
sudo -v
( while true; do sudo -v; sleep 60; done ) &
SUDO_KEEPER=$!
trap 'kill "${SUDO_KEEPER}" 2>/dev/null || true' EXIT

# ── Download ──────────────────────────────────────────────────────

mkdir -p "${DOWNLOAD_DIR}"
ARCHIVE_PATH="${DOWNLOAD_DIR}/${ARCHIVE}"

echo "→ Downloading Whonix ${WHONIX_VERSION} (~2.2 GB)..."
echo "  Destination: ${DOWNLOAD_DIR}"
wget -c "${URL}" -O "${ARCHIVE_PATH}"

# ── Extract ───────────────────────────────────────────────────────

echo "→ Extracting..."
tar xJf "${ARCHIVE_PATH}" -C "${DOWNLOAD_DIR}"

# Locate extracted files (versioned names)
GW_QCOW2=$(find "${DOWNLOAD_DIR}" -maxdepth 1 -name "Whonix-Gateway-CLI-*.qcow2" | head -1)
GW_XML="${DOWNLOAD_DIR}/Whonix-Gateway.xml"
EXT_NET_XML="${DOWNLOAD_DIR}/Whonix_external_network.xml"
INT_NET_XML="${DOWNLOAD_DIR}/Whonix_internal_network.xml"

[[ -f "${GW_QCOW2}" ]]   || { echo "✗ Gateway qcow2 not found after extraction"; exit 1; }
[[ -f "${GW_XML}" ]]     || { echo "✗ Gateway XML not found after extraction"; exit 1; }
[[ -f "${EXT_NET_XML}" ]] || { echo "✗ Whonix_external_network.xml not found"; exit 1; }
[[ -f "${INT_NET_XML}" ]] || { echo "✗ Whonix_internal_network.xml not found"; exit 1; }

# ── Install disk ─────────────────────────────────────────────────

echo "→ Installing Gateway disk to ${GW_DISK}..."
sudo cp "${GW_QCOW2}" "${GW_DISK}"
sudo chown libvirt-qemu:libvirt-qemu "${GW_DISK}" 2>/dev/null || true

# ── Import networks ───────────────────────────────────────────────

for net_xml in "${EXT_NET_XML}" "${INT_NET_XML}"; do
    net_name=$(grep -oP '(?<=<name>)[^<]+' "${net_xml}" | head -1)
    if virsh net-info "${net_name}" &>/dev/null; then
        echo "  Network '${net_name}' already defined — skipping"
    else
        echo "→ Defining network: ${net_name}"
        virsh net-define "${net_xml}"
        virsh net-start   "${net_name}"
        virsh net-autostart "${net_name}"
    fi
done

# ── Patch and import Gateway VM ───────────────────────────────────
# Replace versioned disk path with our canonical Whonix-Gateway.qcow2

PATCHED_XML="/tmp/Whonix-Gateway-patched.xml"
sed "s|<source file='[^']*Whonix-Gateway[^']*\.qcow2'|<source file='${GW_DISK}'|g" \
    "${GW_XML}" > "${PATCHED_XML}"

echo "→ Defining Whonix-Gateway VM..."
virsh define "${PATCHED_XML}"
rm -f "${PATCHED_XML}"

echo "→ Starting Whonix-Gateway..."
virsh start Whonix-Gateway

echo "→ Cleaning up archive..."
rm -f "${ARCHIVE_PATH}"

# ── Done ──────────────────────────────────────────────────────────

INT_NET=$(grep -oP '(?<=<name>)[^<]+' "${INT_NET_XML}" | head -1)

cat <<EOF

✓ Whonix Gateway running.

  Internal network: ${INT_NET}  (10.152.152.0/24)
  Gateway IP:       10.152.152.10
  Tor SOCKS5:       10.152.152.10:9050

  Connect a Kali VM to route through Tor:
    vm whonix-attach <name>

  Access Gateway console:
    vm console Whonix-Gateway

  Note: first boot takes ~2 min for Tor to bootstrap.
  Watch: vm console Whonix-Gateway
EOF
