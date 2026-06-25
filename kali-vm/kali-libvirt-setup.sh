#!/usr/bin/env bash
# Sets up a clean Kali base VM in libvirt on the local host.
# Run as your regular user (sudo will be called where needed).
# Usage: ./tools/kali-vm/kali-libvirt-setup.sh [vm-name]
set -euo pipefail
export LIBVIRT_DEFAULT_URI="qemu:///system"

KALI_VERSION="2026.1"
KALI_IMAGE="kali-linux-${KALI_VERSION}-qemu-amd64"
KALI_URL="https://cdimage.kali.org/current/${KALI_IMAGE}.7z"
IMAGE_DIR="/var/lib/libvirt/images"
VM_NAME="${1:-kali-base}"
VM_DISK="${IMAGE_DIR}/${VM_NAME}.qcow2"
VM_RAM=16384
VM_CPUS=4
VM_DISK_SIZE="100G"
KALI_USER="kali"
KALI_PASSWORD="kali"
KALI_KEY="${HOME}/.ssh/kali"
KALI_KEY_PUB="${KALI_KEY}.pub"

if virsh dominfo "${VM_NAME}" >/dev/null 2>&1; then
    echo "✗ VM '${VM_NAME}' exists already."
    echo "  Use another name: ./tools/kali-vm/kali-libvirt-setup.sh kali-lab-1"
    exit 1
fi

if [[ ! -f "${KALI_KEY}" ]] && ! command -v sshpass >/dev/null 2>&1; then
    echo "✗ Neither SSH key (${KALI_KEY}) nor sshpass found."
    echo "  Install sshpass for password-based bootstrap:  sudo apt install sshpass"
    echo "  Or generate a key:  ssh-keygen -t ed25519 -f ${KALI_KEY} -N ''"
    exit 1
fi

# ── Download ──────────────────────────────────────────────────────────────────

echo "→ Downloading Kali ${KALI_VERSION} QEMU image..."
cd /tmp
if [[ ! -f "${KALI_IMAGE}.qcow2" ]]; then
    wget -c "${KALI_URL}" -O "${KALI_IMAGE}.7z"
    7z x "${KALI_IMAGE}.7z"
fi

# ── Install ───────────────────────────────────────────────────────────────────

echo "→ Installing VM disk to ${VM_DISK} (${VM_DISK_SIZE})..."
sudo qemu-img convert -f qcow2 -O qcow2 "${KALI_IMAGE}.qcow2" "${VM_DISK}"
sudo qemu-img resize "${VM_DISK}" "${VM_DISK_SIZE}"
sudo chown libvirt-qemu:libvirt-qemu "${VM_DISK}" 2>/dev/null || true

# Optional pre-boot customization for hands-off SSH bootstrap.
if command -v virt-customize >/dev/null 2>&1; then
    echo "→ Applying pre-boot guest customization (SSH bootstrap)..."
    CUSTOMIZE_ARGS=(
        -a "${VM_DISK}"
        --run-command "systemctl enable ssh || true"
        --firstboot-command "systemctl enable --now ssh || true"
    )
    if [[ -f "${KALI_KEY_PUB}" ]]; then
        CUSTOMIZE_ARGS+=(--ssh-inject "${KALI_USER}:file:${KALI_KEY_PUB}")
    fi
    sudo virt-customize "${CUSTOMIZE_ARGS[@]}"
else
    echo "→ virt-customize not found, continuing without pre-boot SSH injection."
fi

# ── Create VM via virsh XML (avoids virt-install python-gi bug on Arch) ───────

echo "→ Creating VM '${VM_NAME}'..."
cat > "/tmp/${VM_NAME}.xml" <<XMLEOF
<domain type="kvm">
  <name>${VM_NAME}</name>
  <memory unit="MiB">${VM_RAM}</memory>
  <vcpu>${VM_CPUS}</vcpu>
  <os>
    <type arch="x86_64" machine="q35">hvm</type>
    <boot dev="hd"/>
  </os>
  <features><acpi/><apic/></features>
  <cpu mode="host-passthrough"/>
  <clock offset="utc"/>
  <devices>
    <disk type="file" device="disk">
      <driver name="qemu" type="qcow2" discard="unmap"/>
      <source file="${VM_DISK}"/>
      <target dev="vda" bus="virtio"/>
    </disk>
    <interface type="network">
      <source network="default"/>
      <model type="virtio"/>
    </interface>
    <graphics type="spice" autoport="yes">
      <listen type="address" address="127.0.0.1"/>
    </graphics>
    <video>
      <model type="virtio"/>
    </video>
    <channel type="unix">
      <target type="virtio" name="org.qemu.guest_agent.0"/>
    </channel>
    <console type="pty"/>
  </devices>
</domain>
XMLEOF

virsh define "/tmp/${VM_NAME}.xml"

# Ensure default network is active (locale-independent check)
if ! virsh net-list --name | grep -Fxq "default"; then
    echo "→ Starting libvirt default network..."
    virsh net-start default
fi
virsh net-autostart default >/dev/null 2>&1 || true

virsh start "${VM_NAME}"

# ── Wait for IP ───────────────────────────────────────────────────────────────

echo "→ Waiting for VM to get an IP (this may take ~2-3 min on first boot)..."
VM_IP=""
VM_MAC="$(virsh domiflist "${VM_NAME}" 2>/dev/null | awk '/network/ && $5 ~ /:/ {print $5; exit}')"

for i in $(seq 1 90); do
    # 1) Guest agent path (works when qemu-guest-agent is available)
    VM_IP="$(virsh domifaddr "${VM_NAME}" 2>/dev/null | awk '/ipv4/ {print $4}' | cut -d/ -f1 | head -n1)"

    # 2) DHCP leases path (works without guest agent) — MAC-matched only
    if [[ -z "${VM_IP}" && -n "${VM_MAC}" ]]; then
        VM_IP="$(virsh net-dhcp-leases default 2>/dev/null \
            | awk -v mac="${VM_MAC}" 'tolower($0) ~ tolower(mac) {print $5}' \
            | cut -d/ -f1 | head -n1)"
    fi

    # 3) ARP/neigh cache — MAC-matched only
    if [[ -z "${VM_IP}" && -n "${VM_MAC}" ]]; then
        VM_IP="$(ip neigh 2>/dev/null \
            | awk -v mac="${VM_MAC}" 'tolower($0) ~ tolower(mac) {print $1; exit}')"
    fi

    [[ -n "$VM_IP" ]] && break
    sleep 3
done

if [[ -z "$VM_IP" ]]; then
    echo "✗ Could not detect VM IP automatically."
    echo "  VM MAC: ${VM_MAC:-unknown}"
    echo "  Leases seen on default network:"
    virsh --connect qemu:///system net-dhcp-leases default || true
    echo "  Try:"
    echo "    virsh --connect qemu:///system domifaddr ${VM_NAME}"
    echo "    virsh --connect qemu:///system net-dhcp-leases default"
    exit 1
fi

echo "→ VM IP: ${VM_IP}"

# ── Post-install via SSH ──────────────────────────────────────────────────────

SSH_OPTS=(
    -o StrictHostKeyChecking=no
    -o UserKnownHostsFile=/dev/null
    -o ConnectTimeout=5
)

if [[ -f "${KALI_KEY}" ]]; then
    SSH_CMD=(ssh -i "${KALI_KEY}" "${SSH_OPTS[@]}")
    SCP_CMD=(scp -i "${KALI_KEY}" "${SSH_OPTS[@]}")
    echo "→ Using SSH key bootstrap (${KALI_KEY})..."
elif command -v sshpass >/dev/null 2>&1; then
    SSH_CMD=(sshpass -p "${KALI_PASSWORD}" ssh "${SSH_OPTS[@]}")
    SCP_CMD=(sshpass -p "${KALI_PASSWORD}" scp "${SSH_OPTS[@]}")
    echo "→ Using password bootstrap via sshpass (${KALI_USER}/${KALI_PASSWORD})..."
else
    SSH_CMD=(ssh "${SSH_OPTS[@]}")
    SCP_CMD=(scp "${SSH_OPTS[@]}")
    echo "→ sshpass not found, trying key-based SSH auth..."
fi

echo "→ Waiting for SSH login..."
for i in $(seq 1 30); do
    if "${SSH_CMD[@]}" "${KALI_USER}@${VM_IP}" "true" >/dev/null 2>&1; then
        break
    fi
    sleep 5
done

if ! "${SSH_CMD[@]}" "${KALI_USER}@${VM_IP}" "true" >/dev/null 2>&1; then
    echo "✗ Could not log in to ${KALI_USER}@${VM_IP}."
    if timeout 2 bash -c "cat < /dev/null > /dev/tcp/${VM_IP}/22" 2>/dev/null; then
        echo "  Port 22 is reachable, so this is likely an auth issue."
        echo "  Check credentials and SSH auth settings in the VM."
    else
        echo "  Port 22 is not reachable (SSH service likely not up yet)."
        echo "  Open VM console and run: sudo systemctl enable --now ssh"
    fi
    exit 1
fi

# ── Run setup script ──────────────────────────────────────────────────────────

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

echo "→ Copying and running kali-i3-setup.sh..."
"${SCP_CMD[@]}" \
    "${SCRIPT_DIR}/kali-i3-setup.sh" \
    "${KALI_USER}@${VM_IP}:~/kali-i3-setup.sh"

"${SSH_CMD[@]}" "${KALI_USER}@${VM_IP}" \
    "echo ${KALI_PASSWORD} | sudo -S bash ~/kali-i3-setup.sh"

# ── Snapshot ──────────────────────────────────────────────────────────────────

echo "→ Creating post-setup snapshot..."
if virsh snapshot-info "${VM_NAME}" post-setup >/dev/null 2>&1; then
    virsh snapshot-delete "${VM_NAME}" post-setup --metadata >/dev/null 2>&1 || true
fi
virsh snapshot-create-as "${VM_NAME}" post-setup \
    --disk-only --atomic \
    --description "Kali ${KALI_VERSION} — i3 + pentest tools ready"

# ── Done ──────────────────────────────────────────────────────────────────────

cat <<EOF

✓ Done!

VM:        ${VM_NAME}
IP:        ${VM_IP}
Snapshot:  post-setup (disk-only, survives reboots)
RDP:       xfreerdp /v:${VM_IP} /u:kali /dynamic-resolution /gfx:avc444 +clipboard /cert:tofu
SSH:       ssh kali@${VM_IP}  (key: ~/.ssh/kali)

Clone for a new lab:
  virt-clone --original ${VM_NAME} --name kali-lab-1 \\
    --file ${IMAGE_DIR}/kali-lab-1.qcow2

Revert to clean state:
  virsh snapshot-revert ${VM_NAME} post-setup
EOF
