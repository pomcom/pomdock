package main

import (
	"errors"
	"fmt"
	"io"
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
	// version is the CLI version. It defaults to "dev" for a bare `go build`
	// and is stamped from `git describe` / the VERSION file at release build
	// time via -ldflags "-X main.version=...". See cli/Makefile.
	version = "dev"
)

func main() {
	repoRoot = findRepoRoot()
	if err := newRootCommand().Execute(); err != nil {
		os.Exit(reportCLIError(err, os.Stderr))
	}
}

func reportCLIError(err error, output io.Writer) int {
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		// Interactive scripts print their own diagnostics. Wrapped subprocess
		// errors carry Go context that would otherwise disappear entirely.
		if _, bare := err.(*exec.ExitError); !bare {
			fmt.Fprintln(output, "Error:", err)
		}
		return exitErr.ExitCode()
	}
	fmt.Fprintln(output, "Error:", err)
	return 1
}

func newRootCommand() *cobra.Command {
	root := &cobra.Command{
		Use:   "pomdock",
		Short: "Kali pentest environment manager",
		Long: styleAccent.Render("pomdock") + " — manage Kali Docker containers and libvirt VMs for pentesting.\n\n" +
			styleMuted.Render("Run 'pomdock tui' to open the interactive terminal UI."),
		Version:           version,
		CompletionOptions: cobra.CompletionOptions{HiddenDefaultCmd: true},
		Args:              cobra.NoArgs,
		RunE:              showRootHelp,
	}
	root.SetVersionTemplate("pomdock {{.Version}}\n")
	// Backend scripts (pentest.sh, kali-vm/*.sh) print their own diagnostics; a
	// failed run should not be followed by a cobra usage dump. Flag errors keep
	// the usage text, since there the user did get the invocation wrong.
	root.SilenceUsage = true
	root.SilenceErrors = true
	root.SetFlagErrorFunc(func(c *cobra.Command, err error) error {
		return fmt.Errorf("%w\n\n%s", err, c.UsageString())
	})

	// Groups
	docker := &cobra.Command{Use: "docker", Short: "Manage pentest Docker containers"}
	vm := &cobra.Command{Use: "vm", Short: "Manage libvirt engagement VMs"}

	docker.AddCommand(
		dockerBuild(),
		dockerExec(),
		dockerAttach(),
		dockerStart(),
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
			Args:  cobra.NoArgs,
			Short: "Open the interactive TUI in the current terminal (Docker + VMs)",
			RunE:  func(_ *cobra.Command, _ []string) error { return runTUI() },
		},
		&cobra.Command{
			Use:   "version",
			Args:  cobra.NoArgs,
			Short: "Print the pomdock version",
			Run:   func(_ *cobra.Command, _ []string) { fmt.Println("pomdock", version) },
		},
		docker,
		vm,
		webCmd(),
		sessionsCmd(),
		doctorCmd(),
	)

	return root
}

func showRootHelp(cmd *cobra.Command, _ []string) error {
	return cmd.Help()
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
	containers, err := ListContainers()
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	names := make([]string, 0, len(containers))
	for _, c := range containers {
		names = append(names, c.Name)
	}
	return names, cobra.ShellCompDirectiveNoFileComp
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
			profile := GuestProfileByID(GetVMProfileID(args[0]))
			if profile.Family != "linux" {
				logWarn("Hostname unchanged: source profile %q is not a known Linux guest; rename inside the clone if needed", profile.ID)
			}
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

func vmWhonixAttach() *cobra.Command {
	return &cobra.Command{Use: "whonix-attach <name>", Short: "Configure and verify the VM's default route through its Whonix NIC", Args: cobra.ExactArgs(1), ValidArgsFunction: completeVMs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return configureVMWhonix(cmd.Context(), args[0], cmd.OutOrStdout(), cmd.ErrOrStderr())
		}}
}

func vmWhonixDetach() *cobra.Command {
	return &cobra.Command{Use: "whonix-detach <name>", Short: "Restore management routing and remove the Whonix NIC", Args: cobra.ExactArgs(1), ValidArgsFunction: completeVMs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return restoreVMWhonix(cmd.Context(), args[0], cmd.OutOrStdout(), cmd.ErrOrStderr())
		}}
}
