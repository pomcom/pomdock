#!/usr/bin/env bash
# Provision a minimal Linux server from a verified official cloud image.
set -euo pipefail

PROFILE="${1:-}"
VM_NAME="${2:-}"
IMAGE_URL="${3:-}"
CHECKSUM_URL="${4:-}"
CHECKSUM_TYPE="${5:-}"
IMAGE_FILE="${6:-}"
VM_USER="${7:-}"
ADMIN_GROUP="${8:-}"

IMAGE_DIR="${POMDOCK_IMAGE_DIR:-/var/lib/libvirt/images}"
DOWNLOAD_DIR="${POMDOCK_DOWNLOAD_DIR:-${HOME}/.cache/pomdock}"
SSH_PRIVATE_KEY="${HOME}/.ssh/pomdock"
SSH_PUBLIC_KEY="${SSH_PRIVATE_KEY}.pub"
VM_DISK="${IMAGE_DIR}/${VM_NAME}.qcow2"
SEED_DISK="${IMAGE_DIR}/${VM_NAME}-seed.iso"
VM_RAM="${POMDOCK_VM_RAM:-4096}"
VM_CPUS="${POMDOCK_VM_CPUS:-2}"
VM_DISK_SIZE="${POMDOCK_VM_DISK_SIZE:-40G}"
TMP_DIR=""
SUDO_KEEPER=""
CREATED_DISK=false
DEFINED_VM=false

fail() {
    printf '✗ %s\n' "$*" >&2
    exit 1
}

cleanup() {
    local status=$?
    [[ -n "$SUDO_KEEPER" ]] && kill "$SUDO_KEEPER" 2>/dev/null || true
    [[ -n "$TMP_DIR" ]] && rm -rf "$TMP_DIR"
    if [[ $status -ne 0 ]]; then
        if [[ "$DEFINED_VM" == true ]]; then
            virsh --connect qemu:///system destroy "$VM_NAME" >/dev/null 2>&1 || true
            virsh --connect qemu:///system undefine "$VM_NAME" --nvram >/dev/null 2>&1 \
                || virsh --connect qemu:///system undefine "$VM_NAME" >/dev/null 2>&1 || true
        fi
        if [[ "$CREATED_DISK" == true ]]; then
            sudo rm -f "$VM_DISK" "$SEED_DISK"
        fi
    fi
    exit "$status"
}
trap cleanup EXIT

[[ "$PROFILE" =~ ^(ubuntu-lts|debian-stable|rocky-9)$ ]] || fail "Unsupported cloud profile: ${PROFILE:-missing}"
[[ "$VM_NAME" =~ ^[A-Za-z0-9][A-Za-z0-9._-]*$ ]] || fail "Invalid VM name: ${VM_NAME:-missing}"
[[ "$IMAGE_URL" == https://* && "$CHECKSUM_URL" == https://* ]] || fail "Image URLs must use HTTPS"
[[ "$CHECKSUM_TYPE" == sha256 || "$CHECKSUM_TYPE" == sha512 ]] || fail "Unsupported checksum: $CHECKSUM_TYPE"
[[ -n "$IMAGE_FILE" && -n "$VM_USER" && -n "$ADMIN_GROUP" ]] || fail "Incomplete profile data"
[[ -f "$SSH_PRIVATE_KEY" && -f "$SSH_PUBLIC_KEY" ]] \
    || fail "SSH keypair not found: $SSH_PRIVATE_KEY (create it with: ssh-keygen -t ed25519 -f ~/.ssh/pomdock -N '')"

checksum_command="${CHECKSUM_TYPE}sum"
for command in curl qemu-img genisoimage virsh sudo ssh "$checksum_command"; do
    command -v "$command" >/dev/null 2>&1 || fail "Required command not found: $command"
done
virsh --connect qemu:///system dominfo "$VM_NAME" >/dev/null 2>&1 \
    && fail "VM '$VM_NAME' already exists"
[[ ! -e "$VM_DISK" && ! -e "$SEED_DISK" ]] || fail "Storage already exists for '$VM_NAME'"

mkdir -p "$DOWNLOAD_DIR"
CACHE_IMAGE="${DOWNLOAD_DIR}/${IMAGE_FILE}"
CACHE_PART="${CACHE_IMAGE}.part"
CHECKSUM_FILE="${DOWNLOAD_DIR}/${IMAGE_FILE}.${CHECKSUM_TYPE}"

curl -fsSL --output "$CHECKSUM_FILE" "$CHECKSUM_URL"
hash_length=64
[[ "$CHECKSUM_TYPE" == sha512 ]] && hash_length=128
checksum_line=$(grep -F "$IMAGE_FILE" "$CHECKSUM_FILE" \
    | grep -Ei "[0-9a-f]{${hash_length}}" | head -n1 || true)
expected=$(printf '%s\n' "$checksum_line" | grep -Eo "[0-9a-fA-F]{${hash_length}}" | head -n1 || true)
[[ -n "$expected" ]] || fail "No $CHECKSUM_TYPE checksum found for $IMAGE_FILE"

cache_valid=false
if [[ -f "$CACHE_IMAGE" ]]; then
    actual=$("$checksum_command" "$CACHE_IMAGE" | awk '{print $1}')
    [[ "${actual,,}" == "${expected,,}" ]] && cache_valid=true
fi
if [[ "$cache_valid" != true ]]; then
    rm -f "$CACHE_IMAGE"
    printf '→ Downloading %s cloud image...\n' "$PROFILE"
    curl -fL --retry 3 --continue-at - --output "$CACHE_PART" "$IMAGE_URL"
    actual=$("$checksum_command" "$CACHE_PART" | awk '{print $1}')
    if [[ "${actual,,}" != "${expected,,}" ]]; then
        rm -f "$CACHE_PART"
        printf '→ Cached partial did not match; retrying from the beginning...\n'
        curl -fL --retry 3 --output "$CACHE_PART" "$IMAGE_URL"
        actual=$("$checksum_command" "$CACHE_PART" | awk '{print $1}')
        if [[ "${actual,,}" != "${expected,,}" ]]; then
            rm -f "$CACHE_PART"
            fail "Checksum mismatch for downloaded image"
        fi
    fi
    mv "$CACHE_PART" "$CACHE_IMAGE"
else
    printf '→ Using verified cached image: %s\n' "$CACHE_IMAGE"
fi
printf '✓ Verified %s checksum\n' "$CHECKSUM_TYPE"

printf '→ Requesting sudo credentials for libvirt storage...\n'
sudo -v
( while true; do sudo -n true; sleep 60; done ) &
SUDO_KEEPER=$!

TMP_DIR=$(mktemp -d)
hostname=$(printf 'vm-%s' "$VM_NAME" | tr '[:upper:]_.' '[:lower:]--' | sed -E 's/[^a-z0-9-]+/-/g; s/-+$//' | cut -c1-63)
public_key=$(<"$SSH_PUBLIC_KEY")

cat > "${TMP_DIR}/user-data" <<EOF
#cloud-config
hostname: ${hostname}
manage_etc_hosts: true
users:
  - name: ${VM_USER}
    groups: [${ADMIN_GROUP}]
    sudo: ALL=(ALL) NOPASSWD:ALL
    shell: /bin/bash
    lock_passwd: true
    ssh_authorized_keys:
      - ${public_key}
ssh_pwauth: false
disable_root: true
package_update: true
package_upgrade: false
packages:
  - qemu-guest-agent
  - curl
  - git
growpart:
  mode: auto
  devices: ['/']
resize_rootfs: true
runcmd:
  - [systemctl, enable, --now, qemu-guest-agent]
EOF

cat > "${TMP_DIR}/meta-data" <<EOF
instance-id: pomdock-${VM_NAME}
local-hostname: ${hostname}
EOF

genisoimage -quiet -output "${TMP_DIR}/seed.iso" -volid cidata -joliet -rock \
    "${TMP_DIR}/user-data" "${TMP_DIR}/meta-data"

printf '→ Installing VM disk (%s)...\n' "$VM_DISK_SIZE"
sudo qemu-img convert -f qcow2 -O qcow2 "$CACHE_IMAGE" "$VM_DISK"
CREATED_DISK=true
sudo qemu-img resize "$VM_DISK" "$VM_DISK_SIZE"
sudo install -m 0644 "${TMP_DIR}/seed.iso" "$SEED_DISK"
sudo chown libvirt-qemu:libvirt-qemu "$VM_DISK" "$SEED_DISK" 2>/dev/null || true

cat > "${TMP_DIR}/domain.xml" <<EOF
<domain type="kvm">
  <name>${VM_NAME}</name>
  <memory unit="MiB">${VM_RAM}</memory>
  <vcpu>${VM_CPUS}</vcpu>
  <os firmware="efi">
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
    <disk type="file" device="cdrom">
      <driver name="qemu" type="raw"/>
      <source file="${SEED_DISK}"/>
      <target dev="sda" bus="sata"/>
      <readonly/>
    </disk>
    <interface type="network">
      <source network="default"/>
      <model type="virtio"/>
    </interface>
    <graphics type="spice" autoport="yes"><listen type="address" address="127.0.0.1"/></graphics>
    <video><model type="virtio"/></video>
    <channel type="unix"><target type="virtio" name="org.qemu.guest_agent.0"/></channel>
    <console type="pty"/>
  </devices>
</domain>
EOF

if ! virsh --connect qemu:///system net-list --name | grep -Fxq default; then
    virsh --connect qemu:///system net-start default
fi
virsh --connect qemu:///system net-autostart default >/dev/null 2>&1 || true
virsh --connect qemu:///system define "${TMP_DIR}/domain.xml"
DEFINED_VM=true
virsh --connect qemu:///system start "$VM_NAME"

printf '→ Waiting for DHCP and SSH...\n'
vm_ip=""
vm_mac=$(virsh --connect qemu:///system domiflist "$VM_NAME" | awk '/network/ && $5 ~ /:/ {print $5; exit}')
for _ in $(seq 1 120); do
    vm_ip=$(virsh --connect qemu:///system net-dhcp-leases default 2>/dev/null \
        | awk -v mac="$vm_mac" 'tolower($0) ~ tolower(mac) {print $5; exit}' | cut -d/ -f1)
    [[ -n "$vm_ip" ]] && ssh -i "$SSH_PRIVATE_KEY" -o BatchMode=yes -o StrictHostKeyChecking=no \
        -o UserKnownHostsFile=/dev/null -o ConnectTimeout=3 "${VM_USER}@${vm_ip}" true >/dev/null 2>&1 && break
    sleep 3
done
[[ -n "$vm_ip" ]] || fail "VM did not receive a DHCP address"

ssh_args=(-i "$SSH_PRIVATE_KEY" -o BatchMode=yes -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null)
ssh "${ssh_args[@]}" "${VM_USER}@${vm_ip}" 'sudo cloud-init status --wait --long'

printf '→ Creating clean snapshot...\n'
ssh "${ssh_args[@]}" "${VM_USER}@${vm_ip}" 'sudo shutdown -h now' >/dev/null 2>&1 || true
for _ in $(seq 1 90); do
    [[ "$(virsh --connect qemu:///system domstate "$VM_NAME" 2>/dev/null)" == "shut off" ]] && break
    sleep 2
done
if [[ "$(virsh --connect qemu:///system domstate "$VM_NAME" 2>/dev/null)" != "shut off" ]]; then
    virsh --connect qemu:///system destroy "$VM_NAME" >/dev/null
fi
virsh --connect qemu:///system detach-disk "$VM_NAME" sda --config >/dev/null
sudo rm -f "$SEED_DISK"
virsh --connect qemu:///system snapshot-create-as "$VM_NAME" post-setup \
    --description "$PROFILE cloud image ready" --atomic
virsh --connect qemu:///system start "$VM_NAME" >/dev/null

printf '\n✓ %s ready\n  User: %s\n  IP: %s\n  Snapshot: post-setup\n' "$VM_NAME" "$VM_USER" "$vm_ip"
