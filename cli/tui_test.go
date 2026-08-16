package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/table"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

func TestHelpLinesFitTerminal(t *testing.T) {
	for _, width := range []int{48, 63, 64, 80, 99, 100, 120, 160} {
		for tab := 0; tab < 3; tab++ {
			line := tuiHelp.Render(helpLineForTab(tab, width))
			if got := lipgloss.Width(line); got > width {
				t.Errorf("tab %d width %d: rendered help is %d cells: %q", tab, width, got, line)
			}
		}
	}
}

func TestTabCyclesAcrossAllWorkspaces(t *testing.T) {
	m := newTUI()
	for want := 1; want <= 3; want++ {
		updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyTab})
		m = updated.(tuiModel)
		if got := m.activeTab; got != want%3 {
			t.Fatalf("cycle %d: active tab = %d, want %d", want, got, want%3)
		}
	}
}

func TestShellViewFitsTerminal(t *testing.T) {
	for _, width := range []int{48, 80, 120} {
		m := newTUI()
		m.width = width
		m.shells = []ShellSession{{
			Name:      "shell-an-extremely-long-window-name-0123456789",
			Container: "an-extremely-long-engagement-container-name",
		}}
		for _, line := range strings.Split(m.shellsView(), "\n") {
			if got := lipgloss.Width(line); got > width {
				t.Errorf("width %d: shell line is %d cells: %q", width, got, line)
			}
		}
	}
}

func TestLogLinesWrapToTerminal(t *testing.T) {
	line := formatLogLine(80, "12:34:56", "->",
		"Keys: 1/2/tab switch panels; s/S start/stop; c connect; C console; r RDP; R reset; D delete; w/W attach/detach Whonix; q quit")
	for _, rendered := range strings.Split(line, "\n") {
		if got := lipgloss.Width(rendered); got > 80 {
			t.Errorf("rendered log is %d cells: %q", got, rendered)
		}
	}
}

func TestConnectStartsStoppedContainer(t *testing.T) {
	m := newTUI()
	m.containers = []Container{{Name: "engagement-pentest", Status: "exited"}}
	cmds := m.handleDockerKey("c")
	if !m.busy {
		t.Fatal("model should be busy while the container starts")
	}
	if len(cmds) != 2 {
		t.Fatalf("got %d commands, want start log and start command", len(cmds))
	}
}

func TestConnectFocusesEmbeddedConsole(t *testing.T) {
	m := newTUI()
	m.containers = []Container{{Name: "engagement", Status: "running"}}
	cmds := m.handleDockerKey("c")
	if len(cmds) != 1 {
		t.Fatalf("got %d commands, want cursor focus command", len(cmds))
	}
	if !m.commandInput.Focused() {
		t.Fatal("command input should be focused")
	}
	if m.console.target != "engagement" {
		t.Fatalf("console target = %q, want engagement", m.console.target)
	}
}

func TestContainerStartStopIgnoreInvalidStates(t *testing.T) {
	tests := []struct {
		name   string
		status string
		key    string
	}{
		{name: "start running", status: "running", key: "s"},
		{name: "stop stopped", status: "exited", key: "S"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := newTUI()
			m.containers = []Container{{Name: "engagement-pentest", Status: tt.status}}
			if cmds := m.handleDockerKey(tt.key); len(cmds) != 0 {
				t.Fatalf("got %d commands, want none", len(cmds))
			}
			if m.busy {
				t.Fatal("model should not become busy")
			}
		})
	}
}

func TestWhonixNetworkName(t *testing.T) {
	if whonixNetwork != "Whonix-Internal" {
		t.Fatalf("unexpected Whonix network name %q", whonixNetwork)
	}
}

func TestContainerCreateFormOptions(t *testing.T) {
	vpnFile := filepath.Join(t.TempDir(), "client.ovpn")
	if err := os.WriteFile(vpnFile, []byte("client"), 0o600); err != nil {
		t.Fatal(err)
	}
	m := newTUI()
	m.createForm.name.SetValue("client-a")
	m.createForm.vpnFile.SetValue(vpnFile)
	m.createForm.route = routeTorVPN
	opts, err := m.createFormOptions()
	if err != nil {
		t.Fatal(err)
	}
	if opts.Name != "client-a" || opts.VPNFile != vpnFile || !opts.Whonix {
		t.Fatalf("unexpected options: %#v", opts)
	}
}

func TestContainerCreateFormRejectsInvalidName(t *testing.T) {
	m := newTUI()
	m.createForm.name.SetValue("client a")
	if _, err := m.createFormOptions(); err == nil {
		t.Fatal("expected invalid Docker name to fail")
	}
}

func TestOpeningCreateFormDoesNotInsertShortcut(t *testing.T) {
	m := newTUI()
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	got := updated.(tuiModel)
	if !got.createForm.active {
		t.Fatal("create form should be active")
	}
	if value := got.createForm.name.Value(); value != "" {
		t.Fatalf("name input = %q, want empty", value)
	}
}

func TestVMCreateFormDefaultsToSelectedClone(t *testing.T) {
	m := newTUI()
	m.vms = []VM{{Name: "kali-base", State: "shut off"}}
	m.vmTable.SetRows([]table.Row{{"  kali-base", "stopped", "", "no"}})
	m.openVMCreateForm()
	if !m.vmCreateForm.active || m.vmCreateForm.mode != vmCreateClone {
		t.Fatalf("unexpected VM form state: %#v", m.vmCreateForm)
	}
	if m.vmCreateForm.source != "kali-base" {
		t.Fatalf("clone source = %q", m.vmCreateForm.source)
	}
}

func TestVMCreateFormWithoutVMDefaultsToFresh(t *testing.T) {
	m := newTUI()
	m.openVMCreateForm()
	if m.vmCreateForm.mode != vmCreateFresh || m.vmCreateForm.source != "" {
		t.Fatalf("unexpected empty-list VM form: %#v", m.vmCreateForm)
	}
}

func TestVMCreateOptionsRejectsDuplicate(t *testing.T) {
	m := newTUI()
	m.vms = []VM{{Name: "client-vm"}}
	m.vmCreateForm.name.SetValue("client-vm")
	m.vmCreateForm.source = "kali-base"
	if _, err := m.vmCreateOptions(); err == nil {
		t.Fatal("expected duplicate VM name to fail")
	}
}

func TestVMCreateFormFitsTerminal(t *testing.T) {
	for _, width := range []int{48, 80, 120} {
		m := newTUI()
		m.width = width
		m.vmCreateForm = newVMCreateForm()
		m.vmCreateForm.active = true
		m.vmCreateForm.source = "an-extremely-long-source-virtual-machine-name"
		for _, line := range strings.Split(m.vmCreateFormView(), "\n") {
			if got := lipgloss.Width(line); got > width {
				t.Errorf("width %d: VM form line is %d cells: %q", width, got, line)
			}
		}
	}
}

func TestRDPStartsFromVMTab(t *testing.T) {
	m := newTUI()
	m.vms = []VM{{Name: "kali-base", State: "shut off"}}
	m.vmTable.SetRows([]table.Row{{"  kali-base", "stopped", "", "no"}})
	cmds := m.handleVMKey("r")
	if !m.busy {
		t.Fatal("model should be busy while RDP starts and resolves the VM")
	}
	if len(cmds) != 2 {
		t.Fatalf("got %d commands, want progress log and RDP command", len(cmds))
	}
}

func TestContainerRefreshPreservesSelectionByName(t *testing.T) {
	m := newTUI()
	m.containers = []Container{{Name: "alpha"}, {Name: "bravo"}}
	m.dockerCursor = 1
	updated, _ := m.Update(containersMsg{{Name: "bravo"}, {Name: "alpha"}})
	got := updated.(tuiModel)
	if selected := got.selectedContainer(); selected == nil || selected.Name != "bravo" {
		t.Fatalf("selection changed after refresh: %#v", selected)
	}
}

func TestCreatedContainerOpensCommandPane(t *testing.T) {
	m := newTUI()
	updated, _ := m.Update(containerCreatedMsg{name: "client-a"})
	got := updated.(tuiModel)
	if !got.commandInput.Focused() || got.console.target != "client-a" {
		t.Fatalf("console was not focused for new container: target=%q focused=%v",
			got.console.target, got.commandInput.Focused())
	}
}

func TestDockerViewsFitTerminal(t *testing.T) {
	for _, width := range []int{48, 80, 120} {
		m := newTUI()
		m.width, m.height = width, 24
		m.containers = []Container{{Name: "engagement-with-a-long-name", Status: "running", HasVPN: true}}
		m.console.output[m.containers[0].Name] = []string{strings.Repeat("output", 30)}
		for _, view := range []string{m.dockerWorkspaceView(), func() string {
			m.createForm.active = true
			return m.containerCreateFormView()
		}()} {
			for _, line := range strings.Split(view, "\n") {
				if got := lipgloss.Width(line); got > width {
					t.Errorf("width %d: rendered line is %d cells: %q", width, got, line)
				}
			}
		}
	}
}
