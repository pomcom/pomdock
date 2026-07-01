package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

// repoRoot is the pomdock repo root (one level up from the cli/ binary location).
var repoRoot string

func main() {
	exe, err := os.Executable()
	if err != nil {
		repoRoot, _ = os.Getwd()
	} else {
		// binary lives at <repo>/cli/pomdock or <repo>/pomdock (after install)
		dir := filepath.Dir(exe)
		if filepath.Base(dir) == "cli" {
			repoRoot = filepath.Dir(dir)
		} else {
			repoRoot = dir
		}
	}

	root := &cobra.Command{
		Use:   "pomdock",
		Short: "Kali pentest environment manager",
		Long: styleAccent.Render("pomdock") + " — manage Kali Docker containers and libvirt VMs for pentesting.\n\n" +
			styleMuted.Render("Run without arguments to open the interactive TUI."),
		CompletionOptions: cobra.CompletionOptions{HiddenDefaultCmd: true},
		RunE:              func(_ *cobra.Command, _ []string) error { return runTUI() },
	}

	// Groups
	docker := &cobra.Command{Use: "docker", Short: "Manage pentest Docker containers"}
	vm := &cobra.Command{Use: "vm", Short: "Manage Kali libvirt VMs"}

	docker.AddCommand(
		dockerBuild(),
		dockerExec(),
		dockerStop(),
		dockerRm(),
		dockerStatus(),
		dockerLogs(),
		dockerBurp(),
	)

	vm.AddCommand(
		vmTUI(),
		vmList(),
		vmCreate(),
		vmClone(),
		vmStart(),
		vmStop(),
		vmReset(),
		vmIP(),
		vmSSH(),
		vmRDP(),
		vmConsole(),
		vmDelete(),
		vmWhonixGateway(),
		vmWhonixAttach(),
		vmWhonixDetach(),
	)

	root.AddCommand(
		&cobra.Command{
			Use:   "tui",
			Short: "Open the interactive TUI (Docker + VMs)",
			RunE:  func(_ *cobra.Command, _ []string) error { return runTUI() },
		},
		docker,
		vm,
	)

	if err := root.Execute(); err != nil {
		os.Exit(1)
	}
}

// runInteractive runs a command with stdio attached.
func runInteractive(cmd *exec.Cmd) error {
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	return cmd.Run()
}

func pentest(args ...string) error {
	script := filepath.Join(repoRoot, "pentest.sh")
	if _, err := os.Stat(script); err != nil {
		return fmt.Errorf("pentest.sh not found at %s", script)
	}
	return runInteractive(exec.Command("bash", append([]string{script}, args...)...))
}

func vmScript(name string) string {
	return filepath.Join(repoRoot, "kali-vm", name)
}

func completeVMs(_ *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
	return vmNames(), cobra.ShellCompDirectiveNoFileComp
}

func completeContainers(_ *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
	return containerNames(), cobra.ShellCompDirectiveNoFileComp
}

// ── docker build ──────────────────────────────────────────────────────────────

func dockerBuild() *cobra.Command {
	return &cobra.Command{
		Use:   "build",
		Short: "Build the Kali Docker image",
		RunE:  func(_ *cobra.Command, _ []string) error { return pentest("build") },
	}
}

// ── docker exec ───────────────────────────────────────────────────────────────

func dockerExec() *cobra.Command {
	var vpnFile, name string
	var whonix bool
	cmd := &cobra.Command{
		Use:   "exec",
		Short: "Drop into a Kali shell (starts container if needed)",
		RunE: func(_ *cobra.Command, _ []string) error {
			var args []string
			if name != "" {
				args = append(args, "--name", name)
			}
			if vpnFile != "" {
				args = append(args, "--vpn", vpnFile)
			}
			if whonix {
				args = append(args, "--whonix")
			}
			args = append(args, "exec")
			return pentest(args...)
		},
	}
	cmd.Flags().StringVar(&vpnFile, "vpn", "", "VPN config file (.conf or .ovpn)")
	cmd.Flags().BoolVar(&whonix, "whonix", false, "Route through Tor")
	cmd.Flags().StringVar(&name, "name", "", "Named engagement")
	return cmd
}

// ── docker stop ───────────────────────────────────────────────────────────────

func dockerStop() *cobra.Command {
	var name string
	cmd := &cobra.Command{
		Use:               "stop [name]",
		Short:             "Stop container and sidecars",
		ValidArgsFunction: completeContainers,
		RunE: func(_ *cobra.Command, args []string) error {
			var pargs []string
			if name != "" {
				pargs = append(pargs, "--name", name)
			} else if len(args) > 0 {
				pargs = append(pargs, "--name", args[0])
			}
			pargs = append(pargs, "stop")
			return pentest(pargs...)
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "Named engagement")
	return cmd
}

// ── docker rm ─────────────────────────────────────────────────────────────────

func dockerRm() *cobra.Command {
	var name string
	cmd := &cobra.Command{
		Use:               "rm [name]",
		Short:             "Remove container and sidecars (prompts before deleting loot)",
		ValidArgsFunction: completeContainers,
		RunE: func(_ *cobra.Command, args []string) error {
			var pargs []string
			if name != "" {
				pargs = append(pargs, "--name", name)
			} else if len(args) > 0 {
				pargs = append(pargs, "--name", args[0])
			}
			pargs = append(pargs, "rm")
			return pentest(pargs...)
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "Named engagement")
	return cmd
}

// ── docker status ─────────────────────────────────────────────────────────────

func dockerStatus() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show container, VPN, and Tor status",
		RunE: func(_ *cobra.Command, _ []string) error {
			containers, err := ListContainers()
			if err != nil {
				return err
			}
			if len(containers) == 0 {
				fmt.Println(styleMuted.Render("  No pentest containers found."))
				fmt.Println(styleMuted.Render("  Build one: pomdock docker build"))
				return nil
			}
			hdr := func(s string) string { return styleAccent.Render(s) }
			nameW := 24
			for _, c := range containers {
				if len(c.Name)+2 > nameW {
					nameW = len(c.Name) + 2
				}
			}
			fmt.Printf("  %-*s  %-14s  %-6s  %s\n",
				nameW, hdr("NAME"), hdr("STATUS"), hdr("VPN"), hdr("TOR"))
			fmt.Println("  " + styleMuted.Render(strings.Repeat("─", nameW+32)))
			for _, c := range containers {
				vpn := styleMuted.Render("no ")
				if c.HasVPN {
					vpn = styleOK.Render("yes")
				}
				tor := styleMuted.Render("no ")
				if c.HasTor {
					tor = styleOK.Render("yes")
				}
				fmt.Printf("  %s %-*s  %-24s  %-6s  %s\n",
					icon(c.Status), nameW-2, c.Name,
					stateColor(c.Status),
					vpn, tor)
			}
			return nil
		},
	}
}

// ── docker logs ───────────────────────────────────────────────────────────────

func dockerLogs() *cobra.Command {
	var name string
	cmd := &cobra.Command{
		Use:   "logs",
		Short: "Show gluetun / whonix logs",
		RunE: func(_ *cobra.Command, _ []string) error {
			var pargs []string
			if name != "" {
				pargs = append(pargs, "--name", name)
			}
			pargs = append(pargs, "logs")
			return pentest(pargs...)
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "Named engagement")
	return cmd
}

// ── docker burp ───────────────────────────────────────────────────────────────

func dockerBurp() *cobra.Command {
	return &cobra.Command{
		Use:   "burp",
		Short: "Print Burp Suite proxy setup instructions",
		RunE:  func(_ *cobra.Command, _ []string) error { return pentest("burp") },
	}
}

// ══════════════════════════════════════════════════════════════════════════════
// VM subcommands
// ══════════════════════════════════════════════════════════════════════════════

func vmTUI() *cobra.Command {
	return &cobra.Command{
		Use:   "tui",
		Short: "Open the interactive TUI (VMs only view)",
		RunE:  func(_ *cobra.Command, _ []string) error { return runTUI() },
	}
}

func vmList() *cobra.Command {
	return &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List all VMs",
		RunE: func(_ *cobra.Command, _ []string) error {
			vms, err := ListVMs()
			if err != nil {
				return err
			}
			if len(vms) == 0 {
				fmt.Println(styleMuted.Render("  No VMs defined."))
				return nil
			}
			nameW := 22
			for _, vm := range vms {
				if len(vm.Name)+2 > nameW {
					nameW = len(vm.Name) + 2
				}
			}
			hdr := func(s string) string { return styleAccent.Render(s) }
			fmt.Printf("  %-*s  %-18s  %-18s  %s\n",
				nameW, hdr("NAME"), hdr("STATE"), hdr("IP"), hdr("WHONIX"))
			fmt.Println("  " + styleMuted.Render(strings.Repeat("─", nameW+52)))
			for _, vm := range vms {
				ip := vm.IP
				if ip == "" {
					ip = styleMuted.Render("—")
				}
				whonix := styleMuted.Render("no")
				if vm.HasWhonix {
					whonix = styleOK.Render("yes")
				}
				fmt.Printf("  %s %-*s  %-28s  %-18s  %s\n",
					icon(vm.State), nameW-2, vm.Name,
					stateColor(vm.State), ip, whonix)
			}
			return nil
		},
	}
}

func vmCreate() *cobra.Command {
	return &cobra.Command{
		Use:   "create [name]",
		Short: "Download Kali, provision i3 + tools, snapshot",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			name := "kali-base"
			if len(args) > 0 {
				name = args[0]
			}
			script := vmScript("kali-libvirt-setup.sh")
			if _, err := os.Stat(script); err != nil {
				return fmt.Errorf("kali-libvirt-setup.sh not found at %s", script)
			}
			c := exec.Command("bash", script, name)
			c.Stdin, c.Stdout, c.Stderr = os.Stdin, os.Stdout, os.Stderr
			return c.Run()
		},
	}
}

func vmClone() *cobra.Command {
	return &cobra.Command{
		Use:               "clone <src> <new>",
		Short:             "Clone an existing VM",
		Args:              cobra.ExactArgs(2),
		ValidArgsFunction: completeVMs,
		RunE: func(_ *cobra.Command, args []string) error {
			logStep("Cloning '%s' → '%s'...", args[0], args[1])
			if err := CloneVM(args[0], args[1]); err != nil {
				return err
			}
			logOK("Cloned to '%s'", args[1])
			return nil
		},
	}
}

func vmStart() *cobra.Command {
	return &cobra.Command{
		Use:               "start <name>",
		Short:             "Start a stopped VM",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: completeVMs,
		RunE: func(_ *cobra.Command, args []string) error {
			logStep("Starting '%s'...", args[0])
			if err := StartVM(args[0]); err != nil {
				return err
			}
			logOK("Started '%s'", args[0])
			return nil
		},
	}
}

func vmStop() *cobra.Command {
	return &cobra.Command{
		Use:               "stop <name>",
		Short:             "Graceful shutdown",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: completeVMs,
		RunE: func(_ *cobra.Command, args []string) error {
			logStep("Shutting down '%s'...", args[0])
			if err := StopVM(args[0]); err != nil {
				return err
			}
			logOK("Shutdown signal sent")
			return nil
		},
	}
}

func vmReset() *cobra.Command {
	return &cobra.Command{
		Use:               "reset <name>",
		Short:             "Revert to post-setup snapshot and start",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: completeVMs,
		RunE: func(_ *cobra.Command, args []string) error {
			name := args[0]
			logStep("Reverting '%s' to snapshot '%s'...", name, snapshotName)
			_ = ForceOffVM(name)
			if err := RevertSnapshot(name, snapshotName); err != nil {
				return err
			}
			if err := StartVM(name); err != nil {
				return err
			}
			logOK("Reset done — '%s' booting", name)
			return nil
		},
	}
}

func vmIP() *cobra.Command {
	return &cobra.Command{
		Use:               "ip <name>",
		Short:             "Show VM IPv4 address",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: completeVMs,
		RunE: func(_ *cobra.Command, args []string) error {
			ip, err := GetVMIP(args[0])
			if err != nil {
				return err
			}
			fmt.Println(ip)
			return nil
		},
	}
}

func vmSSH() *cobra.Command {
	return &cobra.Command{
		Use:               "ssh <name>",
		Short:             "SSH into VM",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: completeVMs,
		RunE: func(_ *cobra.Command, args []string) error {
			name := args[0]
			logStep("Resolving IP for '%s'...", name)
			ip, err := WaitForVMIP(name, 30*time.Second)
			if err != nil {
				return err
			}
			logOK("Connecting to kali@%s", ip)
			keyPath := filepath.Join(os.Getenv("HOME"), ".ssh", "kali")
			sshArgs := []string{}
			if _, err := os.Stat(keyPath); err == nil {
				sshArgs = append(sshArgs, "-i", keyPath)
			}
			sshArgs = append(sshArgs,
				"-o", "StrictHostKeyChecking=no",
				"-o", "UserKnownHostsFile=/dev/null",
				"kali@"+ip)
			return runInteractive(exec.Command("ssh", sshArgs...))
		},
	}
}

func vmRDP() *cobra.Command {
	return &cobra.Command{
		Use:               "rdp <name>",
		Short:             "RDP into VM via xfreerdp3",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: completeVMs,
		RunE: func(_ *cobra.Command, args []string) error {
			rdpBin := ""
			if _, err := exec.LookPath("xfreerdp3"); err == nil {
				rdpBin = "xfreerdp3"
			} else if _, err := exec.LookPath("xfreerdp"); err == nil {
				rdpBin = "xfreerdp"
			}
			if rdpBin == "" {
				return fmt.Errorf("xfreerdp3 not found — install: sudo apt install freerdp3-x11")
			}
			name := args[0]
			logStep("Resolving IP for '%s'...", name)
			ip, err := WaitForVMIP(name, 30*time.Second)
			if err != nil {
				return err
			}
			logOK("RDP → kali@%s via %s", ip, rdpBin)
			return runInteractive(exec.Command(rdpBin,
				"/v:"+ip, "/u:kali",
				"/dynamic-resolution", "/gfx:avc444", "+clipboard", "/cert:tofu"))
		},
	}
}

func vmConsole() *cobra.Command {
	return &cobra.Command{
		Use:               "console <name>",
		Short:             "Open graphical (virt-viewer) or serial console",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: completeVMs,
		RunE: func(_ *cobra.Command, args []string) error {
			name := args[0]
			if _, err := exec.LookPath("virt-viewer"); err == nil {
				logStep("Opening virt-viewer for '%s'...", name)
				return runInteractive(exec.Command("virt-viewer", "--connect", libvirtURI, name))
			}
			logWarn("virt-viewer not found — falling back to serial console (Ctrl+] to exit)")
			return runInteractive(exec.Command("virsh", "--connect", libvirtURI, "console", name))
		},
	}
}

func vmDelete() *cobra.Command {
	var force bool
	cmd := &cobra.Command{
		Use:               "delete <name>",
		Short:             "Destroy VM, undefine, remove disk",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: completeVMs,
		RunE: func(_ *cobra.Command, args []string) error {
			name := args[0]
			if !force {
				fmt.Printf("%s Delete '%s' and its disk? [y/N] ",
					styleWarn.Render("⚠"), styleBold.Render(name))
				var resp string
				fmt.Scanln(&resp)
				if strings.ToLower(resp) != "y" {
					logStep("Cancelled")
					return nil
				}
			}
			logStep("Deleting '%s'...", name)
			if err := DeleteVM(name); err != nil {
				return err
			}
			logOK("Deleted '%s'", name)
			return nil
		},
	}
	cmd.Flags().BoolVarP(&force, "force", "f", false, "Skip confirmation")
	return cmd
}

func vmWhonixGateway() *cobra.Command {
	return &cobra.Command{
		Use:   "whonix-gateway",
		Short: "Download and import official Whonix Gateway KVM image",
		RunE: func(_ *cobra.Command, _ []string) error {
			script := vmScript("whonix-gateway-setup.sh")
			if _, err := os.Stat(script); err != nil {
				return fmt.Errorf("whonix-gateway-setup.sh not found at %s", script)
			}
			c := exec.Command("bash", script)
			c.Stdin, c.Stdout, c.Stderr = os.Stdin, os.Stdout, os.Stderr
			return c.Run()
		},
	}
}

// whonixRoutingScript configures Tor routing inside the VM over SSH.
// buildWhonixRoutingScript generates the script that configures static Tor routing
// inside the VM. Whonix uses static IPs — no DHCP server runs on the gateway.
func buildWhonixRoutingScript(gw string) string {
	// Derive workstation IP and prefix from gateway: same /18 subnet, host .100
	parts := strings.SplitN(gw, ".", 4)
	wsIP := parts[0] + "." + parts[1] + "." + parts[2] + ".100"
	prefix := "18"
	return fmt.Sprintf(`sudo bash -s <<'INNER'
set -e
GW="%s"
WS_IP="%s"
WS_PREFIX="%s"

whonix_dev=$(ip link show 2>/dev/null | awk -F': ' '/^[0-9]+: eth[1-9]/{print $2; exit}')
[ -z "$whonix_dev" ] && { echo "No secondary NIC found"; exit 1; }
echo "Whonix NIC: $whonix_dev"

nmcli connection delete whonix-internal 2>/dev/null || true
nmcli connection add type ethernet ifname "$whonix_dev" \
    con-name whonix-internal \
    ipv4.method manual \
    ipv4.addresses "${WS_IP}/${WS_PREFIX}" \
    ipv4.gateway "$GW" \
    ipv4.dns "$GW" \
    ipv4.never-default no \
    connection.autoconnect yes
nmcli connection up whonix-internal

mgmt_dev=$(ip route show default 2>/dev/null | awk '/192\.168\./{print $5; exit}')
if [ -n "$mgmt_dev" ]; then
    mgmt_con=$(nmcli -t -f NAME,DEVICE connection show --active 2>/dev/null \
        | awk -F: -v d="$mgmt_dev" '$2==d{print $1; exit}')
    [ -n "$mgmt_con" ] && nmcli connection modify "$mgmt_con" ipv4.never-default yes \
        && nmcli connection up "$mgmt_con"
fi
echo "Default route:"
ip route show default
INNER
`, gw, wsIP, prefix)
}

func vmWhonixAttach() *cobra.Command {
	return &cobra.Command{
		Use:               "whonix-attach <name>",
		Short:             "Add Whonix internal NIC → routes all VM traffic through Tor",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: completeVMs,
		RunE: func(_ *cobra.Command, args []string) error {
			name := args[0]
			if !NetworkExists("Whonix-Internal") {
				return fmt.Errorf("Whonix-Internal not found — run: pomdock vm whonix-gateway")
			}
			state, _ := GetVMState("Whonix-Gateway")
			if state != "running" {
				return fmt.Errorf("Whonix-Gateway not running — start it: pomdock vm start Whonix-Gateway")
			}
			alreadyHasNIC := vmHasWhonixNIC(name)
			vmState, _ := GetVMState(name)
			if vmState != "running" {
				logStep("Starting '%s'...", name)
				if err := StartVM(name); err != nil {
					return err
				}
			}
			logStep("Waiting for management IP...")
			mgmtIP, err := WaitForVMIP(name, 2*time.Minute)
			if err != nil {
				return err
			}
			logOK("Management IP: %s", mgmtIP)
			if alreadyHasNIC {
				logStep("Whonix NIC already attached — reconfiguring routing...")
			} else {
				logStep("Attaching Whonix-Internal NIC...")
				if err := AttachWhonixNIC(name); err != nil {
					return err
				}
			}
			keyPath := filepath.Join(os.Getenv("HOME"), ".ssh", "kali")
			var sshBase []string
			if _, err := os.Stat(keyPath); err == nil {
				sshBase = []string{"ssh", "-i", keyPath}
			} else if _, err := exec.LookPath("sshpass"); err == nil {
				sshBase = []string{"sshpass", "-p", "kali", "ssh"}
			} else {
				sshBase = []string{"ssh"}
			}
			opts := append(sshBase,
				"-o", "StrictHostKeyChecking=no",
				"-o", "UserKnownHostsFile=/dev/null",
				"-o", "ConnectTimeout=5",
				"kali@"+mgmtIP, "bash", "-s")
			logStep("Waiting for SSH...")
			for i := 0; i < 12; i++ {
				probe := exec.Command(opts[0], opts[1:]...)
				probe.Stdin = strings.NewReader("true")
				if probe.Run() == nil {
					break
				}
				time.Sleep(5 * time.Second)
			}
			gwIP := WhonixGatewayIP()
			logStep("Configuring Tor routing inside VM (gateway: %s)...", gwIP)
			sshCmd := exec.Command(opts[0], opts[1:]...)
			sshCmd.Stdin = strings.NewReader(buildWhonixRoutingScript(gwIP))
			sshCmd.Stdout, sshCmd.Stderr = os.Stdout, os.Stderr
			_ = sshCmd.Run()
			fmt.Println()
			logOK("All traffic from '%s' now routes through Tor", name)
			fmt.Printf("  %s  Management: %s\n", styleMuted.Render("→"), mgmtIP)
			fmt.Printf("  %s  SOCKS5:     %s:9050\n", styleMuted.Render("→"), gwIP)
			return nil
		},
	}
}

const whonixRestoreScript = `sudo bash -s <<'INNER'
set -e
nmcli connection delete whonix-internal 2>/dev/null || true
mgmt_dev=$(ip -4 addr show 2>/dev/null | awk '/192\.168\./{print $NF; exit}')
[ -n "$mgmt_dev" ] && mgmt_con=$(nmcli -t -f NAME,DEVICE connection show --active 2>/dev/null \
    | awk -F: -v d="$mgmt_dev" '$2==d{print $1; exit}')
[ -n "$mgmt_con" ] && nmcli connection modify "$mgmt_con" ipv4.never-default no \
    && nmcli connection up "$mgmt_con" && echo "Default route restored on '$mgmt_con'"
INNER
`

func vmWhonixDetach() *cobra.Command {
	return &cobra.Command{
		Use:               "whonix-detach <name>",
		Short:             "Remove Whonix NIC, restore normal routing",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: completeVMs,
		RunE: func(_ *cobra.Command, args []string) error {
			name := args[0]
			if !vmHasWhonixNIC(name) {
				logWarn("'%s' has no Whonix NIC", name)
				return nil
			}
			mgmtIP, _ := GetVMIP(name)
			logStep("Detaching Whonix NIC from '%s'...", name)
			if err := DetachWhonixNIC(name); err != nil {
				return err
			}
			if mgmtIP != "" {
				keyPath := filepath.Join(os.Getenv("HOME"), ".ssh", "kali")
				var sshBase []string
				if _, err := os.Stat(keyPath); err == nil {
					sshBase = []string{"ssh", "-i", keyPath}
				} else {
					sshBase = []string{"ssh"}
				}
				opts := append(sshBase,
					"-o", "StrictHostKeyChecking=no",
					"-o", "UserKnownHostsFile=/dev/null",
					"-o", "ConnectTimeout=5",
					"kali@"+mgmtIP, "bash", "-s")
				logStep("Restoring default route...")
				cmd := exec.Command(opts[0], opts[1:]...)
				cmd.Stdin = strings.NewReader(whonixRestoreScript)
				cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
				_ = cmd.Run()
			}
			logOK("Whonix NIC removed — '%s' back on normal routing", name)
			return nil
		},
	}
}
