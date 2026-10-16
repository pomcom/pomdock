package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

// Container control for the web UI. Handlers are thin: validation here, the
// actual work in docker_ops.go / shell_ops.go (shared with the TUI), and
// anything slow runs as a background job (web_jobs.go).

var containerNameRe = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,62}$`)

// tmux window ids look like "@12".
var tmuxWindowIDRe = regexp.MustCompile(`^@[0-9]+$`)

type containerView struct {
	Name       string `json:"name"`
	State      string `json:"state"` // running | exited
	Image      string `json:"image"`
	Route      string `json:"route"` // vpn | direct | tor | tor-vpn | ""
	VPNFile    string `json:"vpnFile,omitempty"`
	HasVPN     bool   `json:"hasVpn"`
	HasTor     bool   `json:"hasTor"`
	Legacy     bool   `json:"legacy"`
	Engagement string `json:"engagement"`      // tracker engagement name for this container
	Shell      string `json:"shell,omitempty"` // tmux window id of its open shell
	Job        string `json:"job,omitempty"`   // id of a running job on it
}

type shellView struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Kind   string `json:"kind"`
	Target string `json:"target"`
	Active bool   `json:"active"`
	Index  int    `json:"index"`
}

// containerEngagement mirrors pentest.sh's loot-dir naming: the default
// container uses ~/pentest itself, every other one ~/pentest/<name>.
func containerEngagement(name string) string {
	if name == "pcm-pentest" {
		return "(default)"
	}
	return name
}

func (s *webServer) handleContainers(w http.ResponseWriter, _ *http.Request) {
	cs, err := ListContainers()
	if err != nil {
		httpError(w, http.StatusServiceUnavailable, fmt.Errorf("%w (is the Docker daemon running?)", err))
		return
	}
	shells := map[string]string{}
	if list, err := ListShellSessions(); err == nil {
		for _, sh := range list {
			if sh.Kind == "docker" {
				shells[sh.Target] = sh.ID
			}
		}
	}
	running := map[string]string{}
	for _, j := range s.jobs.list() {
		if j.Status == "running" {
			running[j.Target] = j.ID
		}
	}
	out := make([]containerView, 0, len(cs))
	for _, c := range cs {
		out = append(out, containerView{
			Name: c.Name, State: c.Status, Image: c.Image, Route: c.Route, VPNFile: c.VPN,
			HasVPN: c.HasVPN, HasTor: c.HasTor, Legacy: c.Legacy,
			Engagement: containerEngagement(c.Name), Shell: shells[c.Name], Job: running[c.Name],
		})
	}
	writeJSON(w, map[string]any{"containers": out})
}

type routeRequest struct {
	Name    string `json:"name"`
	VPNFile string `json:"vpnFile"`
	Whonix  bool   `json:"whonix"`
}

// createOptions validates a create/recreate request into pentest.sh options.
func (req routeRequest) createOptions(name string) (ContainerCreateOptions, error) {
	if !containerNameRe.MatchString(name) {
		return ContainerCreateOptions{}, fmt.Errorf("invalid container name %q (letters, digits, . _ -)", name)
	}
	opts := ContainerCreateOptions{Name: name, Whonix: req.Whonix}
	if vpn := strings.TrimSpace(req.VPNFile); vpn != "" {
		vpn, err := filepath.Abs(expandHome(vpn))
		if err != nil {
			return opts, err
		}
		info, err := os.Stat(vpn)
		if err != nil {
			return opts, fmt.Errorf("VPN file: %w", err)
		}
		if !info.Mode().IsRegular() {
			return opts, fmt.Errorf("VPN file %s is not a regular file", vpn)
		}
		opts.VPNFile = vpn
	}
	return opts, nil
}

func decodeBody(w http.ResponseWriter, r *http.Request, v any) bool {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		httpError(w, http.StatusBadRequest, fmt.Errorf("bad request body: %w", err))
		return false
	}
	return true
}

func (s *webServer) handleContainerCreate(w http.ResponseWriter, r *http.Request) {
	var req routeRequest
	if !decodeBody(w, r, &req) {
		return
	}
	opts, err := req.createOptions(req.Name)
	if err != nil {
		httpError(w, http.StatusBadRequest, err)
		return
	}
	for _, existing := range containerNames() {
		if existing == opts.Name {
			httpError(w, http.StatusConflict, fmt.Errorf("container %s already exists — start or recreate it instead", opts.Name))
			return
		}
	}
	s.startJob(w, "create", opts.Name, func(out io.Writer) error {
		return createPentestContainerTo(opts, out)
	})
}

// pentestContainer resolves {name} to an existing pomdock pentest container,
// so the control endpoints never act on unrelated containers.
func (s *webServer) pentestContainer(w http.ResponseWriter, r *http.Request) (string, bool) {
	name := r.PathValue("name")
	cs, err := ListContainers()
	if err != nil {
		httpError(w, http.StatusServiceUnavailable, err)
		return "", false
	}
	for _, c := range cs {
		if c.Name == name {
			return name, true
		}
	}
	httpError(w, http.StatusNotFound, fmt.Errorf("no pomdock container named %q", name))
	return "", false
}

func (s *webServer) handleContainerStart(w http.ResponseWriter, r *http.Request) {
	if name, ok := s.pentestContainer(w, r); ok {
		s.startJob(w, "start", name, func(out io.Writer) error { return startContainerTo(name, out) })
	}
}

func (s *webServer) handleContainerStop(w http.ResponseWriter, r *http.Request) {
	if name, ok := s.pentestContainer(w, r); ok {
		s.startJob(w, "stop", name, func(out io.Writer) error { return stopContainerTo(name, out) })
	}
}

// handleContainerRemove removes the container and its sidecars. The loot dir
// (history, recordings, tracker curation) lives on the host and is kept.
func (s *webServer) handleContainerRemove(w http.ResponseWriter, r *http.Request) {
	if name, ok := s.pentestContainer(w, r); ok {
		s.startJob(w, "remove", name, func(out io.Writer) error {
			fmt.Fprintf(out, "removing %s and its sidecars (loot dir is kept)\n", name)
			return RemoveContainerStack(name)
		})
	}
}

// handleContainerRecreate is "edit": a container's network route is fixed at
// creation, so changing it means removing the stack and creating it again
// under the same name — and therefore the same loot dir.
func (s *webServer) handleContainerRecreate(w http.ResponseWriter, r *http.Request) {
	name, ok := s.pentestContainer(w, r)
	if !ok {
		return
	}
	var req routeRequest
	if !decodeBody(w, r, &req) {
		return
	}
	opts, err := req.createOptions(name)
	if err != nil {
		httpError(w, http.StatusBadRequest, err)
		return
	}
	s.startJob(w, "recreate", name, func(out io.Writer) error {
		fmt.Fprintf(out, "removing %s and its sidecars (loot dir is kept)\n", name)
		if err := RemoveContainerStack(name); err != nil {
			return err
		}
		if err := createPentestContainerTo(opts, out); err != nil {
			return fmt.Errorf("%s was removed but could not be created again (%w); fix the cause, then create it under the same name: the loot dir is kept", name, err)
		}
		return nil
	})
}

func (s *webServer) startJob(w http.ResponseWriter, kind, target string, fn func(io.Writer) error) {
	job, err := s.jobs.start(kind, target, fn)
	var busy errJobBusy
	if errors.As(err, &busy) {
		httpError(w, http.StatusConflict, err)
		return
	}
	if err != nil {
		httpError(w, http.StatusInternalServerError, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(job)
}

// handleContainerShell opens (or reuses) the container's recorded shell window
// in the pomdock tmux workspace and selects it, so any terminal attached to
// the workspace shows it immediately.
func (s *webServer) handleContainerShell(w http.ResponseWriter, r *http.Request) {
	name, ok := s.pentestContainer(w, r)
	if !ok {
		return
	}
	windowID, err := EnsureContainerShell(name)
	if err != nil {
		httpError(w, http.StatusConflict, err)
		return
	}
	_ = exec.Command("tmux", "select-window", "-t", windowID).Run()
	clients, _ := exec.Command("tmux", "list-clients", "-t", "="+workspaceSession).Output()
	writeJSON(w, map[string]any{
		"windowId": windowID,
		"attached": strings.TrimSpace(string(clients)) != "",
		"attach":   "tmux attach -t =" + workspaceSession,
		"switch":   "tmux switch-client -t =" + workspaceSession,
	})
}

func (s *webServer) handleContainerCheck(w http.ResponseWriter, r *http.Request) {
	name, ok := s.pentestContainer(w, r)
	if !ok {
		return
	}
	output, err := RunContainerTool(name, r.PathValue("tool"))
	resp := map[string]any{"output": output}
	if err != nil {
		resp["error"] = err.Error()
	}
	writeJSON(w, resp)
}

func (s *webServer) handleShells(w http.ResponseWriter, _ *http.Request) {
	list, err := ListShellSessions()
	if err != nil {
		httpError(w, http.StatusServiceUnavailable, err)
		return
	}
	out := make([]shellView, 0, len(list))
	for _, sh := range list {
		out = append(out, shellView{ID: sh.ID, Name: sh.Name, Kind: sh.Kind, Target: sh.Target, Active: sh.Active, Index: sh.Index})
	}
	writeJSON(w, map[string]any{
		"shells": out, "attach": "tmux attach -t =" + workspaceSession,
		"switch": "tmux switch-client -t =" + workspaceSession,
	})
}

// handleShellClose kills a shell window, but only one pomdock itself manages in
// its workspace — never an arbitrary tmux window.
func (s *webServer) handleShellClose(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !tmuxWindowIDRe.MatchString(id) {
		httpError(w, http.StatusBadRequest, fmt.Errorf("invalid window id %q", id))
		return
	}
	list, err := ListShellSessions()
	if err != nil {
		httpError(w, http.StatusServiceUnavailable, err)
		return
	}
	for _, sh := range list {
		if sh.ID == id {
			if err := KillShellSession(id); err != nil {
				httpError(w, http.StatusInternalServerError, err)
				return
			}
			writeJSON(w, map[string]any{"ok": true})
			return
		}
	}
	httpError(w, http.StatusNotFound, fmt.Errorf("no pomdock shell window %s", id))
}

func (s *webServer) handleJobs(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, map[string]any{"jobs": s.jobs.list()})
}

func (s *webServer) handleJob(w http.ResponseWriter, r *http.Request) {
	job, ok := s.jobs.get(r.PathValue("id"))
	if !ok {
		httpError(w, http.StatusNotFound, fmt.Errorf("no job %q", r.PathValue("id")))
		return
	}
	writeJSON(w, job)
}
