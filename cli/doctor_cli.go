package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

type doctorCheck struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Detail string `json:"detail"`
}

func doctorCmd() *cobra.Command {
	var jsonOutput bool
	cmd := &cobra.Command{Use: "doctor", Short: "Check local dependencies, runtime files and image compatibility", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		checks := collectDoctorChecks(cmd.Context())
		if jsonOutput {
			if err := writeCLIJSON(cmd.OutOrStdout(), checks); err != nil {
				return err
			}
		} else {
			for _, check := range checks {
				fmt.Fprintf(cmd.OutOrStdout(), "%-8s %-12s %s\n", strings.ToUpper(check.Status), check.Name, check.Detail)
			}
		}
		for _, check := range checks {
			if check.Status == "error" {
				return fmt.Errorf("local setup needs attention; see checks above")
			}
		}
		return nil
	}}
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "write structured JSON")
	return cmd
}

func doctorRun(ctx context.Context, binary string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, binary, args...).Output()
	if ctx.Err() != nil {
		return "", ctx.Err()
	}
	return strings.TrimSpace(string(out)), err
}

func pythonSupported(output string) bool {
	parts := strings.Fields(strings.TrimSpace(output))
	if len(parts) != 2 || parts[0] != "Python" {
		return false
	}
	numbers := strings.Split(parts[1], ".")
	if len(numbers) < 2 {
		return false
	}
	major, majorErr := strconv.Atoi(numbers[0])
	minor, minorErr := strconv.Atoi(numbers[1])
	return majorErr == nil && minorErr == nil && (major > 3 || (major == 3 && minor >= 11))
}

func collectDoctorChecks(ctx context.Context) []doctorCheck {
	checks := []doctorCheck{}
	add := func(name, status, detail string) { checks = append(checks, doctorCheck{name, status, detail}) }
	binary, err := os.Executable()
	if err != nil {
		add("binary", "error", err.Error())
	} else {
		add("binary", "ok", binary+" ("+version+")")
	}
	if isRepoRoot(repoRoot) {
		add("runtime", "ok", repoRoot)
	} else {
		add("runtime", "error", "backend files missing; set POMDOCK_ROOT to this checkout or reinstall")
	}
	for _, dep := range []struct{ name, flag string }{{"python3", "--version"}, {"tmux", "-V"}, {"docker", "--version"}} {
		path, err := exec.LookPath(dep.name)
		if err != nil {
			if dep.name == "tmux" {
				add(dep.name, "warning", "only needed for host/VM terminals; Docker attach uses tmux inside the container")
			} else {
				add(dep.name, "error", "not installed or absent from PATH")
			}
			continue
		}
		output, err := doctorRun(ctx, path, dep.flag)
		if err != nil {
			if dep.name == "tmux" {
				add(dep.name, "warning", "host tmux unavailable; only needed for host/VM terminals; Docker attach uses tmux inside the container")
			} else {
				add(dep.name, "error", "cannot read version: "+err.Error())
			}
		} else if dep.name == "python3" && !pythonSupported(output) {
			add(dep.name, "error", output+"; recording search/replay require Python 3.11+")
		} else {
			add(dep.name, "ok", output)
		}
	}
	static, err := webDist()
	if err == nil {
		_, err = fs.Stat(static, "index.html")
	}
	if err != nil {
		add("web assets", "error", "missing; build the bundled frontend before building pomdock")
	} else {
		add("web assets", "ok", "bundled locally")
	}
	missing := []string{}
	for _, relative := range []string{"scripts/recording/session-log.sh", "scripts/recording/cmdlog.zsh", "scripts/recording/cmdlog.bash", "scripts/pomdock-tmux"} {
		if _, err := os.Stat(filepath.Join(repoRoot, relative)); err != nil {
			missing = append(missing, relative)
		}
	}
	if len(missing) > 0 {
		add("recorder", "error", "missing: "+strings.Join(missing, ", "))
	} else {
		add("recorder", "ok", "terminal capture, Bash/Zsh hooks and container tmux helper present")
	}
	if _, err := exec.LookPath("docker"); err == nil {
		server, err := doctorRun(ctx, "docker", "version", "--format", "{{.Server.Version}}")
		if err != nil || server == "" {
			add("daemon", "error", "Docker daemon unavailable; check daemon and socket permissions")
		} else {
			add("daemon", "ok", server)
			image := os.Getenv("PENTEST_IMAGE")
			if image == "" {
				image = "pcm-kali-pentest"
			}
			output, err := doctorRun(ctx, "docker", "image", "inspect", image, "--format", "{{json .Config.Labels}}")
			if err != nil {
				add("image", "warning", image+" not available; run pomdock docker build")
			} else {
				var labels map[string]string
				if err := json.Unmarshal([]byte(output), &labels); err != nil {
					add("image", "warning", image+" labels could not be read")
				} else if labels["io.pomdock.recording-format"] != "3" {
					add("image", "warning", image+" uses an older/unknown recorder; rebuild and recreate containers to use format 3")
				} else if labels["io.pomdock.container-tmux"] != "1" {
					add("image", "warning", image+" has no container tmux marker; rebuild and recreate containers for docker attach")
				} else {
					toolset := labels["io.pomdock.toolset"]
					if toolset == "" {
						toolset = "unspecified"
					}
					add("image", "ok", image+" (recording format 3, toolset "+toolset+")")
				}
			}
		}
	}
	return checks
}
