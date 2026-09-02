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

var (
	repoRoot      string
	installedRoot = "/usr/local/lib/pomdock"
)

func main() {
	repoRoot = findRepoRoot()

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
	vm := &cobra.Command{Use: "vm", Short: "Manage libvirt engagement VMs"}

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
		vmProfile(),
		vmCreate(),
		vmFinalize(),
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

func findRepoRoot() string {
	var candidates []string
	if configured := os.Getenv("POMDOCK_ROOT"); configured != "" {
		candidates = append(candidates, configured)
	}
	if exe, err := os.Executable(); err == nil {
		if resolved, resolveErr := filepath.EvalSymlinks(exe); resolveErr == nil {
			exe = resolved
		}
		dir := filepath.Dir(exe)
		if filepath.Base(dir) == "cli" {
			candidates = append(candidates, filepath.Dir(dir))
		} else {
			candidates = append(candidates, dir)
		}
	}
	if cwd, err := os.Getwd(); err == nil {
		for dir := cwd; ; dir = filepath.Dir(dir) {
			candidates = append(candidates, dir)
			if parent := filepath.Dir(dir); parent == dir {
				break
			}
		}
	}
	candidates = append(candidates, installedRoot)
	if root, ok := firstRepoRoot(candidates); ok {
		return root
	}
	return installedRoot
}

func firstRepoRoot(candidates []string) (string, bool) {
	seen := make(map[string]bool, len(candidates))
	for _, candidate := range candidates {
		candidate = filepath.Clean(candidate)
		if seen[candidate] {
			continue
		}
		seen[candidate] = true
		if isRepoRoot(candidate) {
			return candidate, true
		}
	}
	return "", false
}

func isRepoRoot(dir string) bool {
	if info, err := os.Stat(filepath.Join(dir, "pentest.sh")); err != nil || info.IsDir() {
		return false
	}
	info, err := os.Stat(filepath.Join(dir, "kali-vm"))
	return err == nil && info.IsDir()
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
			fmt.Printf("  %-*s  %-20s  %-18s  %-18s  %s\n",
				nameW, hdr("NAME"), hdr("PROFILE"), hdr("STATE"), hdr("IP"), hdr("WHONIX"))
			fmt.Println("  " + styleMuted.Render(strings.Repeat("─", nameW+74)))
			for _, vm := range vms {
				profile := GuestProfileByID(vm.ProfileID)
				ip := vm.IP
				if ip == "" {
					ip = styleMuted.Render("—")
				}
				whonix := styleMuted.Render("no")
				if vm.HasWhonix {
					whonix = styleOK.Render("yes")
				}
				fmt.Printf("  %s %-*s  %-20s  %-28s  %-18s  %s\n",
					icon(vm.State), nameW-2, vm.Name,
					profile.Label, stateColor(vm.State), ip, whonix)
			}
			return nil
		},
	}
}

func vmCreate() *cobra.Command {
	var profileID, iso, virtioISO string
	cmd := &cobra.Command{
		Use:   "create [name]",
		Short: "Create a VM from a supported guest profile",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			profile := GuestProfileByID(profileID)
			if profile.ID != profileID || profile.Provisioner == "" {
				return fmt.Errorf("profile %q does not support creation", profileID)
			}
			name := profile.ID + "-base"
			if len(args) > 0 {
				name = args[0]
			}
			if VMExists(name) {
				return fmt.Errorf("VM %q already exists", name)
			}
			c, profile, err := PrepareVMProvision(VMProvisionOptions{
				ProfileID: profileID,
				Name:      name,
				ISO:       iso,
				VirtioISO: virtioISO,
			})
			if err != nil {
				return err
			}
			c.Stdin, c.Stdout, c.Stderr = os.Stdin, os.Stdout, os.Stderr
			if err := c.Run(); err != nil {
				return err
			}
			if err := SetVMProfileID(name, profile.ID); err != nil {
				logWarn("VM created, but its profile metadata could not be saved: %v", err)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&profileID, "profile", defaultGuestProfileID,
		"Guest profile: "+strings.Join(provisionableGuestProfileIDs, ", "))
	cmd.Flags().StringVar(&iso, "iso", "", "Official Windows installation ISO (required for Windows profiles)")
	cmd.Flags().StringVar(&virtioISO, "virtio-iso", "", "VirtIO Windows driver ISO (auto-detected when omitted)")
	_ = cmd.MarkFlagFilename("iso", "iso")
	_ = cmd.MarkFlagFilename("virtio-iso", "iso")
	_ = cmd.RegisterFlagCompletionFunc("profile", func(_ *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
		return append([]string(nil), provisionableGuestProfileIDs...), cobra.ShellCompDirectiveNoFileComp
	})
	return cmd
}

func vmFinalize() *cobra.Command {
	return &cobra.Command{
		Use:               "finalize <name>",
		Short:             "Finish a Windows install and create its clean snapshot",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: completeVMs,
		RunE: func(_ *cobra.Command, args []string) error {
			name := args[0]
			profile := GuestProfileByID(GetVMProfileID(name))
			if profile.Family != "windows" {
				return fmt.Errorf("finalize is only required for Windows ISO installs")
			}
			logStep("Finalizing '%s'...", name)
			if err := FinalizeWindowsVM(name, profile.Snapshot); err != nil {
				return err
			}
			logOK("Installer media removed and snapshot '%s' created", profile.Snapshot)
			return nil
		},
	}
}

func vmProfile() *cobra.Command {
	return &cobra.Command{
		Use:   "profile <name> [profile]",
		Short: "Show or set a VM guest profile",
		Args:  cobra.RangeArgs(1, 2),
		ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			if len(args) == 0 {
				return completeVMs(cmd, args, toComplete)
			}
			return GuestProfileIDs(), cobra.ShellCompDirectiveNoFileComp
		},
		RunE: func(_ *cobra.Command, args []string) error {
			if !VMExists(args[0]) {
				return fmt.Errorf("VM %q does not exist", args[0])
			}
			if len(args) == 1 {
				profile := GuestProfileByID(GetVMProfileID(args[0]))
				fmt.Printf("%s\t%s\n", profile.ID, profile.Label)
				return nil
			}
			if err := SetVMProfileID(args[0], args[1]); err != nil {
				return err
			}
			logOK("'%s' now uses profile %s", args[0], args[1])
			return nil
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
			if err := CopyVMProfileID(args[0], args[1]); err != nil {
				return fmt.Errorf("VM cloned, but profile metadata could not be copied: %w", err)
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
			profile := GuestProfileByID(GetVMProfileID(name))
			logStep("Reverting '%s' to snapshot '%s'...", name, profile.Snapshot)
			_ = ForceOffVM(name)
			if err := RevertSnapshot(name, profile.Snapshot); err != nil {
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
			profile := GuestProfileByID(GetVMProfileID(name))
			if !profile.SupportsSSH {
				return fmt.Errorf("%s profile does not provide SSH", profile.Label)
			}
			logStep("Resolving IP for '%s'...", name)
			ip, err := WaitForVMIP(name, 30*time.Second)
			if err != nil {
				return err
			}
			logOK("Connecting to %s@%s", profile.SSHUser, ip)
			keyPath := profileSSHKey(profile)
			sshArgs := []string{}
			if keyPath != "" {
				if _, err := os.Stat(keyPath); err == nil {
					sshArgs = append(sshArgs, "-i", keyPath)
				}
			}
			sshArgs = append(sshArgs,
				"-o", "StrictHostKeyChecking=no",
				"-o", "UserKnownHostsFile=/dev/null",
				profile.SSHUser+"@"+ip)
			return runInteractive(exec.Command("ssh", sshArgs...))
		},
	}
}

func vmRDP() *cobra.Command {
	return &cobra.Command{
		Use:               "rdp <name>",
		Short:             "RDP into VM via FreeRDP or Remmina fallback",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: completeVMs,
		RunE: func(_ *cobra.Command, args []string) error {
			name := args[0]
			logStep("Starting '%s' if needed and resolving its IP...", name)
			cmd, ip, err := PrepareVMRDP(name, 90*time.Second)
			if err != nil {
				return err
			}
			profile := GuestProfileByID(GetVMProfileID(name))
			logOK("RDP → %s@%s via %s", profile.RDPUser, ip, cmd.Path)
			return runInteractive(cmd)
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
			profile := GuestProfileByID(GetVMProfileID(name))
			if !profile.SupportsWhonix {
				return fmt.Errorf("Whonix routing is not supported for the %s profile", profile.Label)
			}
			if !NetworkExists(whonixNetwork) {
				return fmt.Errorf("%s not found — run: pomdock vm whonix-gateway", whonixNetwork)
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
			profile := GuestProfileByID(GetVMProfileID(name))
			if !profile.SupportsWhonix {
				return fmt.Errorf("Whonix routing is not supported for the %s profile", profile.Label)
			}
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
