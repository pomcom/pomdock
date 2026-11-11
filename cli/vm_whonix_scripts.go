package main

import (
	"fmt"
	"net"
	"strings"
)

// The guest executes this only through the authenticated management SSH session.
// MACs come from libvirt, never assumptions about eth*/ens*/enp* device names.
func whonixScriptValues(gateway, address, torMAC, managementMAC, managementIP string) (string, error) {
	for _, address := range []string{gateway, address, managementIP} {
		if net.ParseIP(address).To4() == nil {
			return "", fmt.Errorf("invalid Whonix/management IPv4 address")
		}
	}
	for _, mac := range []string{torMAC, managementMAC} {
		if parsed, err := net.ParseMAC(mac); err != nil || len(parsed) != 6 {
			return "", fmt.Errorf("invalid VM interface MAC")
		}
	}
	if strings.EqualFold(torMAC, managementMAC) {
		return "", fmt.Errorf("Whonix and management NICs must differ")
	}
	return fmt.Sprintf("GW='%s'\nWS_IP='%s'\nTOR_MAC='%s'\nMGMT_MAC='%s'\nMGMT_IP='%s'\n", gateway, address, strings.ToLower(torMAC), strings.ToLower(managementMAC), managementIP), nil
}

const whonixNICFunctions = `
nic_for_mac() {
    ip -o link show | awk -v mac="$1" '
        {for (i=1;i<=NF;i++) if (tolower($i)==mac) {sub(/:$/,"",$2); sub(/@.*/,"",$2); print $2; exit}}'
}
mgmt_dev=$(nic_for_mac "$MGMT_MAC")
[ -n "$mgmt_dev" ] || { echo 'Management NIC MAC not found' >&2; exit 1; }
mgmt_uuid=$(nmcli -g GENERAL.CON-UUID device show "$mgmt_dev")
[ -n "$mgmt_uuid" ] && [ "$mgmt_uuid" != '--' ] || { echo 'Management connection unavailable' >&2; exit 1; }
case "$mgmt_uuid" in *[!a-fA-F0-9-]*|'') echo 'Invalid management connection UUID' >&2; exit 1;; esac
state_dir=/var/lib/pomdock
state_file="$state_dir/whonix-$MGMT_MAC.state"
`

func buildWhonixRoutingScript(gateway, address, torMAC, managementMAC, managementIP string) (string, error) {
	values, err := whonixScriptValues(gateway, address, torMAC, managementMAC, managementIP)
	if err != nil {
		return "", err
	}
	return "sudo -n bash -s <<'INNER'\nset -euo pipefail\n" + values + whonixNICFunctions + `
whonix_dev=$(nic_for_mac "$TOR_MAC")
[ -n "$whonix_dev" ] || { echo 'Attached Whonix NIC MAC not found' >&2; exit 1; }
[ "$whonix_dev" != "$mgmt_dev" ] || { echo 'Whonix NIC is the management NIC' >&2; exit 1; }

# Preserve settings once, including on repeated attach/reconfigure operations.
umask 077
mkdir -p "$state_dir"
if [ ! -e "$state_file" ]; then
    previous4=$(nmcli -g ipv4.never-default connection show uuid "$mgmt_uuid")
    previous6=$(nmcli -g ipv6.never-default connection show uuid "$mgmt_uuid")
    case "$previous4:$previous6" in yes:yes|yes:no|no:yes|no:no) ;; *) echo 'Cannot preserve management route settings' >&2; exit 1;; esac
    temporary=$(mktemp "$state_file.XXXXXX")
    printf '%s\n' "$mgmt_uuid" "$previous4" "$previous6" > "$temporary"
    mv "$temporary" "$state_file"
fi

if nmcli connection show id whonix-internal >/dev/null 2>&1; then
    existing_mac=$(nmcli -g 802-3-ethernet.mac-address connection show id whonix-internal)
    [ -z "$existing_mac" ] || [ "$existing_mac" = '--' ] || [ "${existing_mac,,}" = "$TOR_MAC" ] || { echo 'Whonix connection belongs to another NIC' >&2; exit 1; }
else
    nmcli connection add type ethernet ifname "$whonix_dev" con-name whonix-internal
fi
nmcli connection modify id whonix-internal \
    connection.interface-name "$whonix_dev" 802-3-ethernet.mac-address "$TOR_MAC" \
    ipv4.method manual ipv4.addresses "$WS_IP/18" ipv4.gateway "$GW" \
    ipv4.dns "$GW" ipv4.dns-priority -50 ipv4.never-default no ipv4.route-metric 10 \
    ipv6.method disabled connection.autoconnect yes
nmcli --wait 20 connection up id whonix-internal

# Reapply route flags in place: keep the management address and SSH connection.
nmcli connection modify uuid "$mgmt_uuid" ipv4.never-default yes ipv6.never-default yes
nmcli device reapply "$mgmt_dev"
ip -o -4 addr show dev "$mgmt_dev" | awk -v address="$MGMT_IP" '$3=="inet" {split($4,a,"/"); if(a[1]==address) found=1} END {exit !found}'

# These are route assertions, not a claim that Tor has bootstrapped or that
# arbitrary policy-routing tables, application proxies or VPNs were inspected.
ip -4 route show default | awk -v gateway="$GW" -v device="$whonix_dev" '
    {count++; via=""; dev=""; for(i=1;i<=NF;i++){if($i=="via")via=$(i+1);if($i=="dev")dev=$(i+1)}; if(via!=gateway || dev!=device) wrong=1}
    END {exit count!=1 || wrong}'
[ -z "$(ip -6 route show default)" ] || { echo 'An IPv6 default route remains; Whonix routing was not verified' >&2; exit 1; }
printf 'Verified IPv4 default via %s dev %s; management address retained; IPv6 has no default route.\n' "$GW" "$whonix_dev"
INNER
`, nil
}

func buildWhonixRestoreScript(managementMAC string) (string, error) {
	parsed, err := net.ParseMAC(managementMAC)
	if err != nil || len(parsed) != 6 {
		return "", fmt.Errorf("invalid management interface MAC")
	}
	return "sudo -n bash -s <<'INNER'\nset -euo pipefail\nMGMT_MAC='" + strings.ToLower(managementMAC) + "'\n" + whonixNICFunctions + `
previous4=no
previous6=no
if [ -f "$state_file" ]; then
    mapfile -t previous < "$state_file"
    [ "${#previous[@]}" = 3 ] && [ "${previous[0]}" = "$mgmt_uuid" ] || { echo 'Saved management routing belongs to another connection' >&2; exit 1; }
    previous4=${previous[1]}
    previous6=${previous[2]}
    case "$previous4:$previous6" in yes:yes|yes:no|no:yes|no:no) ;; *) echo 'Invalid saved management route settings' >&2; exit 1;; esac
else
    echo 'Legacy Whonix setup has no saved route settings; restoring management defaults.'
fi
nmcli connection modify uuid "$mgmt_uuid" ipv4.never-default "$previous4" ipv6.never-default "$previous6"
nmcli device reapply "$mgmt_dev"
if nmcli connection show id whonix-internal >/dev/null 2>&1; then
    nmcli connection delete id whonix-internal
fi
if [ "$previous4" = no ]; then
    ip -4 route show default | awk -v device="$mgmt_dev" '{for(i=1;i<=NF;i++) if($i=="dev" && $(i+1)==device) found=1} END {exit !found}'
fi
rm -f "$state_file"
echo 'Previous management default-route settings restored.'
INNER
`, nil
}
