#!/usr/bin/env bash
set -Eeuo pipefail

PROFILE=${1:-}
VM_NAME=${2:-}
WINDOWS_ISO=${3:-}
VIRTIO_ISO=${4:-}
VM_RAM=${POMDOCK_VM_RAM:-8192}
VM_CPUS=${POMDOCK_VM_CPUS:-4}
VM_DISK_SIZE=${POMDOCK_VM_DISK_SIZE:-80G}
IMAGE_DIR=${POMDOCK_VM_IMAGE_DIR:-/var/lib/libvirt/images}
VM_DISK="${IMAGE_DIR}/${VM_NAME}.qcow2"

fail() {
    printf 'error: %s\n' "$*" >&2
    exit 1
}

[[ "$PROFILE" == "windows-server-2025" || "$PROFILE" == "windows-server-2022" || "$PROFILE" == "windows-server-2019" || "$PROFILE" == "windows-11-enterprise" ]] \
    || fail "unsupported Windows profile: ${PROFILE:-<empty>}"
[[ "$VM_NAME" =~ ^[A-Za-z0-9._-]+$ ]] || fail "invalid VM name"
[[ -r "$WINDOWS_ISO" && -f "$WINDOWS_ISO" ]] || fail "Windows ISO is not readable: $WINDOWS_ISO"
[[ -z "$VIRTIO_ISO" || (-r "$VIRTIO_ISO" && -f "$VIRTIO_ISO") ]] \
    || fail "VirtIO ISO is not readable: $VIRTIO_ISO"
[[ "$VM_RAM" =~ ^[0-9]+$ && "$VM_CPUS" =~ ^[0-9]+$ ]] || fail "RAM and CPU values must be integers"

for command in virsh qemu-img sudo; do
    command -v "$command" >/dev/null || fail "required command not found: $command"
done
command -v swtpm >/dev/null || fail "swtpm is required for Windows TPM 2.0"

find_firmware() {
    local candidate
    for candidate in "$@"; do
        [[ -r "$candidate" ]] && { printf '%s\n' "$candidate"; return 0; }
    done
    return 1
}

OVMF_CODE=$(find_firmware \
    /usr/share/edk2/x64/OVMF_CODE.secboot.4m.fd \
    /usr/share/OVMF/OVMF_CODE_4M.secboot.fd \
    /usr/share/OVMF/OVMF_CODE.secboot.fd) || fail "Secure Boot OVMF firmware not found"
OVMF_VARS=$(find_firmware \
    /usr/share/edk2/x64/OVMF_VARS.4m.fd \
    /usr/share/OVMF/OVMF_VARS_4M.fd \
    /usr/share/OVMF/OVMF_VARS.fd) || fail "OVMF variable template not found"

virsh --connect qemu:///system dominfo "$VM_NAME" >/dev/null 2>&1 \
    && fail "VM already exists: $VM_NAME"
sudo test ! -e "$VM_DISK" || fail "VM disk already exists: $VM_DISK"

xml_escape() {
    local value=$1
    value=${value//&/&amp;}
    value=${value//</&lt;}
    value=${value//>/&gt;}
    value=${value//\"/&quot;}
    value=${value//\'/&apos;}
    printf '%s' "$value"
}

tmp_dir=$(mktemp -d)
defined=false
disk_created=false
cleanup() {
    local status=$?
    if (( status != 0 )); then
        if [[ "$defined" == true ]]; then
            virsh --connect qemu:///system destroy "$VM_NAME" >/dev/null 2>&1 || true
            virsh --connect qemu:///system undefine "$VM_NAME" --nvram >/dev/null 2>&1 \
                || virsh --connect qemu:///system undefine "$VM_NAME" >/dev/null 2>&1 || true
        fi
        [[ "$disk_created" == true ]] && sudo rm -f "$VM_DISK"
    fi
    rm -rf "$tmp_dir"
    exit "$status"
}
trap cleanup EXIT

printf 'Creating %s (%s RAM, %s CPUs, %s disk)...\n' "$VM_NAME" "$VM_RAM MiB" "$VM_CPUS" "$VM_DISK_SIZE"
sudo install -d -m 0755 "$IMAGE_DIR"
sudo qemu-img create -f qcow2 "$VM_DISK" "$VM_DISK_SIZE"
disk_created=true
sudo chown libvirt-qemu:libvirt-qemu "$VM_DISK" 2>/dev/null || true
sudo chmod 0640 "$VM_DISK"

windows_iso_xml=$(xml_escape "$WINDOWS_ISO")
virtio_disk_xml=""
if [[ -n "$VIRTIO_ISO" ]]; then
    virtio_iso_xml=$(xml_escape "$VIRTIO_ISO")
    virtio_disk_xml=$(cat <<EOF
    <disk type="file" device="cdrom">
      <driver name="qemu" type="raw"/>
      <source file="$virtio_iso_xml"/>
      <target dev="sdc" bus="sata"/>
      <readonly/>
    </disk>
EOF
)
fi

cat >"${tmp_dir}/domain.xml" <<EOF
<domain type="kvm">
  <name>$VM_NAME</name>
  <memory unit="MiB">$VM_RAM</memory>
  <vcpu placement="static">$VM_CPUS</vcpu>
  <os firmware="efi">
    <type arch="x86_64" machine="q35">hvm</type>
    <firmware>
      <feature enabled="no" name="enrolled-keys"/>
      <feature enabled="yes" name="secure-boot"/>
    </firmware>
    <loader readonly="yes" secure="yes" type="pflash">$(xml_escape "$OVMF_CODE")</loader>
    <nvram template="$(xml_escape "$OVMF_VARS")"/>
    <boot dev="cdrom"/>
    <boot dev="hd"/>
  </os>
  <features>
    <acpi/><apic/><hyperv mode="passthrough"/><vmport state="off"/><smm state="on"/>
  </features>
  <cpu mode="host-passthrough" check="none"/>
  <clock offset="localtime">
    <timer name="rtc" tickpolicy="catchup"/>
    <timer name="pit" tickpolicy="delay"/>
    <timer name="hpet" present="no"/>
    <timer name="hypervclock" present="yes"/>
  </clock>
  <on_poweroff>destroy</on_poweroff>
  <on_reboot>restart</on_reboot>
  <on_crash>destroy</on_crash>
  <devices>
    <disk type="file" device="disk">
      <driver name="qemu" type="qcow2" cache="none" discard="unmap"/>
      <source file="$(xml_escape "$VM_DISK")"/>
      <target dev="sda" bus="sata"/>
    </disk>
    <disk type="file" device="cdrom">
      <driver name="qemu" type="raw"/>
      <source file="$windows_iso_xml"/>
      <target dev="sdb" bus="sata"/>
      <readonly/>
    </disk>
$virtio_disk_xml
    <interface type="network">
      <source network="default"/>
      <model type="e1000e"/>
    </interface>
    <controller type="usb" model="qemu-xhci"/>
    <controller type="sata" index="0"/>
    <tpm model="tpm-crb"><backend type="emulator" version="2.0"/></tpm>
    <graphics type="spice" autoport="yes"><listen type="address" address="127.0.0.1"/></graphics>
    <video><model type="qxl" primary="yes"/></video>
    <sound model="ich9"/>
    <channel type="spicevmc"><target type="virtio" name="com.redhat.spice.0"/></channel>
    <channel type="unix"><target type="virtio" name="org.qemu.guest_agent.0"/></channel>
    <console type="pty"/>
  </devices>
</domain>
EOF

if command -v virt-xml-validate >/dev/null; then
    virt-xml-validate "${tmp_dir}/domain.xml" domain
fi

if ! virsh --connect qemu:///system net-list --name | grep -Fxq default; then
    virsh --connect qemu:///system net-start default
fi
virsh --connect qemu:///system net-autostart default >/dev/null 2>&1 || true
virsh --connect qemu:///system define "${tmp_dir}/domain.xml"
defined=true
virsh --connect qemu:///system start "$VM_NAME"

printf '\n%s is running the Windows installer.\n' "$VM_NAME"
printf 'Open it with: pomdock vm console %s\n' "$VM_NAME"
printf 'After Windows is installed and shut down: pomdock vm finalize %s\n' "$VM_NAME"
