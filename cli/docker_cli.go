package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

// A target is accepted in the same form by every Docker command. Names are
// never silently discarded, and removal never falls back to a default target.
func dockerTarget(args []string, flag string, required bool) (string, error) {
	if len(args) > 1 {
		return "", fmt.Errorf("specify one container name")
	}
	name := flag
	if len(args) == 1 {
		if name != "" && name != args[0] {
			return "", fmt.Errorf("conflicting targets: %q and --name %q", args[0], name)
		}
		name = args[0]
	}
	if name == "" && required {
		return "", fmt.Errorf("specify a container name or --name (use pcm-pentest for the default container)")
	}
	if name != "" && !containerNameRe.MatchString(name) {
		return "", fmt.Errorf("invalid container name %q (letters, digits, . _ -)", name)
	}
	return name, nil
}

func addDockerNameFlag(cmd *cobra.Command, name *string) {
	cmd.Flags().StringVar(name, "name", "", "container name (alternative to the positional name)")
	cmd.ValidArgsFunction = completeContainers
	_ = cmd.RegisterFlagCompletionFunc("name", completeContainers)
}

func dockerBuild() *cobra.Command {
	var toolset string
	var fresh bool
	cmd := &cobra.Command{Use: "build", Short: "Build the Kali image", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		if toolset != "full" && toolset != "minimal" {
			return fmt.Errorf("--toolset must be full or minimal")
		}
		args := []string{"--toolset", toolset}
		if fresh {
			args = append(args, "--fresh")
		}
		args = append(args, "build")
		return pentest(args...)
	}}
	cmd.Flags().StringVar(&toolset, "toolset", "full", "full: configured tools; minimal: shells, recording and Atuin")
	cmd.Flags().BoolVar(&fresh, "fresh", false, "rebuild without cached image layers")
	_ = cmd.RegisterFlagCompletionFunc("toolset", func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
		return []string{"full", "minimal"}, cobra.ShellCompDirectiveNoFileComp
	})
	return cmd
}

func dockerExec() *cobra.Command {
	return dockerConnectCmd("exec", "Open a recorded Kali shell (starts the container if needed)")
}
func dockerStart() *cobra.Command {
	return dockerConnectCmd("start", "Start a Kali container in the background")
}

func dockerConnectCmd(action, short string) *cobra.Command {
	var name, vpn, route string
	var whonix bool
	cmd := &cobra.Command{Use: action + " [name]", Short: short, Args: cobra.MaximumNArgs(1), Example: "  pomdock docker " + action + " acme --vpn ~/vpn/client.conf\n  pomdock docker " + action + " acme --route tor", RunE: func(_ *cobra.Command, args []string) error {
		target, err := dockerTarget(args, name, false)
		if err != nil {
			return err
		}
		if route != "" && route != "direct" && route != "vpn" && route != "tor" && route != "tor-vpn" {
			return fmt.Errorf("--route must be direct, vpn, tor or tor-vpn")
		}
		if vpn != "" && (route == "direct" || route == "tor") {
			return fmt.Errorf("--vpn requires --route vpn or tor-vpn, or omit --route")
		}
		if whonix && (route == "direct" || route == "vpn") {
			return fmt.Errorf("--whonix conflicts with --route %s", route)
		}
		backend := []string{}
		if target != "" {
			backend = append(backend, "--name", target)
		}
		if route != "" {
			backend = append(backend, "--route", route)
		}
		if vpn != "" {
			backend = append(backend, "--vpn", vpn)
		}
		if whonix {
			backend = append(backend, "--whonix")
		}
		return pentest(append(backend, action)...)
	}}
	addDockerNameFlag(cmd, &name)
	cmd.Flags().StringVar(&route, "route", "", "direct | vpn | tor | tor-vpn (omitted: reuse the existing route)")
	cmd.Flags().StringVar(&vpn, "vpn", "", "VPN config (.conf or .ovpn); implies vpn when --route is omitted")
	cmd.Flags().BoolVar(&whonix, "whonix", false, "route through Tor (compatibility flag; prefer --route tor)")
	_ = cmd.Flags().MarkHidden("whonix")
	_ = cmd.MarkFlagFilename("vpn", "conf", "ovpn")
	_ = cmd.RegisterFlagCompletionFunc("route", func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
		return []string{"direct", "vpn", "tor", "tor-vpn"}, cobra.ShellCompDirectiveNoFileComp
	})
	return cmd
}

func dockerTargetAction(action, short string, required bool) *cobra.Command {
	var name string
	use := action + " [name]"
	if required {
		use = action + " <name>"
	}
	cmd := &cobra.Command{Use: use, Short: short, Args: cobra.MaximumNArgs(1), RunE: func(_ *cobra.Command, args []string) error {
		target, err := dockerTarget(args, name, required)
		if err != nil {
			return err
		}
		backend := []string{}
		if target != "" {
			backend = append(backend, "--name", target)
		}
		return pentest(append(backend, action)...)
	}}
	addDockerNameFlag(cmd, &name)
	return cmd
}
func dockerStop() *cobra.Command {
	return dockerTargetAction("stop", "Stop a container and its sidecars", false)
}
func dockerRm() *cobra.Command {
	var name string
	var purge, yes bool
	cmd := &cobra.Command{Use: "rm <name>", Short: "Remove a named container and sidecars, keeping loot", Args: cobra.MaximumNArgs(1), RunE: func(_ *cobra.Command, args []string) error {
		target, err := dockerTarget(args, name, true)
		if err != nil {
			return err
		}
		if yes && !purge {
			return fmt.Errorf("--yes requires --purge")
		}
		backend := []string{"--name", target}
		if purge {
			backend = append(backend, "--purge")
		}
		if yes {
			backend = append(backend, "--yes")
		}
		return pentest(append(backend, "rm")...)
	}}
	addDockerNameFlag(cmd, &name)
	cmd.Flags().BoolVar(&purge, "purge", false, "also delete loot after confirmation")
	cmd.Flags().BoolVar(&yes, "yes", false, "confirm --purge without prompting")
	return cmd
}
func dockerLogs() *cobra.Command {
	return dockerTargetAction("logs", "Show container and sidecar logs (recordings: sessions logs)", false)
}
func dockerBurp() *cobra.Command {
	return dockerTargetAction("burp", "Show a running engagement VPN proxy's native Burp setup", false)
}

func dockerStatus() *cobra.Command {
	var name string
	cmd := &cobra.Command{Use: "status [name]", Short: "Show container runtime and sidecar states", Args: cobra.MaximumNArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		target, err := dockerTarget(args, name, false)
		if err != nil {
			return err
		}
		containers, err := ListContainers()
		if err != nil {
			return err
		}
		if target != "" {
			selected := []Container{}
			for _, c := range containers {
				if c.Name == target {
					selected = append(selected, c)
				}
			}
			containers = selected
			if len(containers) == 0 {
				return fmt.Errorf("no pomdock container named %q", target)
			}
		}
		if len(containers) == 0 {
			fmt.Fprintln(cmd.OutOrStdout(), "No pentest containers. Start one: pomdock docker start NAME")
			return nil
		}
		width := 4
		for _, c := range containers {
			if len(c.Name) > width {
				width = len(c.Name)
			}
		}
		fmt.Fprintf(cmd.OutOrStdout(), "%-*s  %-10s  %-8s  %-12s  %s\n", width, "NAME", "STATE", "ROUTE", "VPN SIDECAR", "TOR SIDECAR")
		for _, c := range containers {
			route := c.Route
			if route == "" {
				route = "unknown"
			}
			vpn, tor := "not running", "not running"
			if c.HasVPN {
				vpn = "running"
			}
			if c.HasTor {
				tor = "running"
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%-*s  %-10s  %-8s  %-12s  %s\n", width, c.Name, c.Status, route, vpn, tor)
		}
		return nil
	}}
	addDockerNameFlag(cmd, &name)
	return cmd
}

func dockerAttach() *cobra.Command {
	var name, window string
	cmd := &cobra.Command{Use: "attach <name>", Short: "Resume tmux inside an already running container", Long: "Attach to the container's persistent workspace, creating it on first use.\nThe container must already be running. Ctrl-b d detaches; under default host\ntmux, use Ctrl-b Ctrl-b d. Direct shells remain available with pomdock docker exec NAME.", Args: cobra.MaximumNArgs(1), RunE: func(_ *cobra.Command, args []string) error {
		target, err := dockerTarget(args, name, true)
		if err != nil {
			return err
		}
		process, err := containerAttachCommand(target, window)
		if err != nil {
			return err
		}
		return runInteractive(process)
	}}
	addDockerNameFlag(cmd, &name)
	cmd.Flags().StringVar(&window, "window", "", "specific container tmux window ID, such as @0")
	return cmd
}
