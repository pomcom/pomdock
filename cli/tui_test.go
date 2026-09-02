package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/table"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
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

func TestTabSwitchRequestsFullScreenClear(t *testing.T) {
	m := newTUI()
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'2'}})
	if updated.(tuiModel).activeTab != 1 {
		t.Fatal("VM tab was not selected")
	}
	if cmd == nil {
		t.Fatal("tab switch did not request a renderer command")
	}
	msg := cmd()
	if strings.Contains(fmt.Sprintf("%T", msg), "clearScreenMsg") {
		return
	}
	batch, ok := msg.(tea.BatchMsg)
	if !ok || len(batch) == 0 {
		t.Fatalf("tab switch command = %T, want tea.ClearScreen", msg)
	}
	foundClear := false
	for _, item := range batch {
		if item != nil && strings.Contains(fmt.Sprintf("%T", item()), "clearScreenMsg") {
			foundClear = true
			break
		}
	}
	if !foundClear {
		t.Fatal("tab switch batch does not contain tea.ClearScreen")
	}
}

func TestVMNavigationMovesExactlyOneRow(t *testing.T) {
	m := newTUI()
	m.activeTab = 1
	m.vms = []VM{{Name: "one"}, {Name: "two"}, {Name: "three"}}
	m.setVMRows()
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyDown})
	got := updated.(tuiModel)
	if got.vmTable.Cursor() != 1 {
		t.Fatalf("one Down key moved VM cursor to %d, want 1", got.vmTable.Cursor())
	}
	if cmd == nil {
		t.Fatal("VM selection movement did not request a full redraw")
	}
}

func TestShellViewFitsTerminal(t *testing.T) {
	for _, width := range []int{48, 80, 120} {
		m := newTUI()
		m.width = width
		m.shells = []ShellSession{{
			Name:   "shell-an-extremely-long-window-name-0123456789",
			Kind:   "docker",
			Target: "an-extremely-long-engagement-container-name",
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
	if m.busy {
		t.Fatal("identity check must not run against a stopped container")
	}
	if len(cmds) != 1 {
		t.Fatalf("got %d commands, want a start warning", len(cmds))
	}
}

func TestConnectRunsIdentityCheck(t *testing.T) {
	m := newTUI()
	m.containers = []Container{{Name: "engagement", Status: "running"}}
	cmds := m.handleDockerKey("c")
	if len(cmds) != 1 {
		t.Fatalf("got %d commands, want identity check", len(cmds))
	}
	if !m.busy {
		t.Fatal("model should be busy while the identity check runs")
	}
}

func TestDockerPanelOffersEngagementTools(t *testing.T) {
	m := newTUI()
	m.width, m.height = 120, 30
	m.containers = []Container{{Name: "engagement", Status: "running"}}
	view := ansi.Strip(m.containerConsoleView(64, 18))
	for _, want := range []string{"ENGAGEMENT · engagement", "Identity and egress", "Listening and published ports", "Tor exit check", "Full tmux shell"} {
		if !strings.Contains(view, want) {
			t.Fatalf("engagement panel missing %q:\n%s", want, view)
		}
	}
	if strings.Contains(view, "run a command") {
		t.Fatalf("legacy command input is still visible:\n%s", view)
	}
}

func TestEngagementChecksReplacePreviousResult(t *testing.T) {
	m := newTUI()
	m.appendConsoleResult(containerCommandMsg{name: "client-a", command: "identity", output: "first"})
	m.appendConsoleResult(containerCommandMsg{name: "client-a", command: "ports", output: "second"})
	got := strings.Join(m.console.output["client-a"], "\n")
	if strings.Contains(got, "first") || !strings.Contains(got, "[PORTS]\nsecond") {
		t.Fatalf("engagement output was not replaced:\n%s", got)
	}
}

func TestFitPanelClearsFullRectangle(t *testing.T) {
	view := fitPanel(styleAccent.Render("short"), 20, 4)
	lines := strings.Split(view, "\n")
	if len(lines) != 4 {
		t.Fatalf("panel height = %d, want 4", len(lines))
	}
	for _, line := range lines {
		if got := lipgloss.Width(line); got != 20 {
			t.Fatalf("panel line width = %d, want 20: %q", got, line)
		}
	}
}

func TestContainerTransferFormDefaults(t *testing.T) {
	m := newTUI()
	m.openContainerTransferForm("client-a", transferDownload)
	if !m.transferForm.active || !m.transferForm.source.Focused() {
		t.Fatal("download form should open on the source field")
	}
	if got := m.transferForm.source.Value(); got != "/home/kali/pentest/" {
		t.Fatalf("download source = %q", got)
	}
	if got := m.transferForm.destination.Value(); got != "~/pentest/client-a/" {
		t.Fatalf("download destination = %q", got)
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
	if m.vmCreateForm.mode != vmCreateFresh || m.vmCreateForm.source != "" || m.vmCreateForm.profileID != "kali" {
		t.Fatalf("unexpected empty-list VM form: %#v", m.vmCreateForm)
	}
}

func TestVMCreateFormCyclesProfiles(t *testing.T) {
	m := newTUI()
	m.openVMCreateForm()
	m.vmCreateForm.field = 1
	m.cycleVMCreateSource(1)
	if m.vmCreateForm.mode != vmCreateFresh || m.vmCreateForm.profileID != "ubuntu-lts" {
		t.Fatalf("first profile cycle = %#v", m.vmCreateForm)
	}
	m.cycleVMCreateSource(-1)
	if m.vmCreateForm.profileID != "kali" {
		t.Fatalf("reverse profile cycle = %#v", m.vmCreateForm)
	}
}

func TestWindowsCreateFormRequiresISO(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	scriptDir := filepath.Join(home, "vm-profiles")
	if err := os.Mkdir(scriptDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(scriptDir, "windows-iso-setup.sh"), []byte("#!/bin/bash\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	oldRoot := repoRoot
	repoRoot = home
	t.Cleanup(func() { repoRoot = oldRoot })
	m := newTUI()
	m.openVMCreateForm()
	m.vmCreateForm.mode = vmCreateFresh
	m.vmCreateForm.profileID = "windows-11-enterprise"
	m.vmCreateForm.name.SetValue("win11-lab")
	if fields := m.vmCreateFieldCount(); fields != 3 {
		t.Fatalf("Windows form fields = %d, want 3", fields)
	}
	if _, err := m.vmCreateOptions(); err == nil {
		t.Fatal("expected missing ISO to fail")
	}
	iso := filepath.Join(home, "windows.iso")
	if err := os.WriteFile(iso, []byte("test"), 0o600); err != nil {
		t.Fatal(err)
	}
	m.vmCreateForm.iso.SetValue(iso)
	if _, err := m.vmCreateOptions(); err != nil {
		t.Fatalf("valid Windows form failed: %v", err)
	}
}

func TestVMProvisionShellCommandIsQuotedAndValid(t *testing.T) {
	provision := exec.Command("bash", "/tmp/setup it's.sh", "lab-one")
	script := vmProvisionShellCommand(provision, "/tmp/pom dock", "lab-one", "kali")
	for _, expected := range []string{
		shellQuote("/tmp/setup it's.sh"),
		shellQuote("/tmp/pom dock"),
		shellQuote("pomdock:dashboard"),
		"This window remains open for diagnosis",
	} {
		if !strings.Contains(script, expected) {
			t.Fatalf("provision script does not contain %q:\n%s", expected, script)
		}
	}
	check := exec.Command("bash", "-n")
	check.Stdin = strings.NewReader(script)
	if out, err := check.CombinedOutput(); err != nil {
		t.Fatalf("invalid provision shell command: %v: %s\n%s", err, out, script)
	}
}

func TestVMRDPShellCommandReturnsAfterFailure(t *testing.T) {
	command := exec.Command("xfreerdp3", "/v:192.0.2.10", "/u:kali", "/dynamic-resolution", "/gfx:avc444", "+clipboard", "/cert:tofu")
	script := vmRDPShellCommand(command, "client-a")
	for _, want := range []string{
		shellQuote("/gfx:avc444"),
		shellQuote("+clipboard"),
		shellQuote(workspaceSession + ":" + dashboardWindow),
		"Returning to the dashboard in 8 seconds",
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("RDP script missing %q:\n%s", want, script)
		}
	}
	check := exec.Command("bash", "-n")
	check.Stdin = strings.NewReader(script)
	if out, err := check.CombinedOutput(); err != nil {
		t.Fatalf("invalid RDP shell command: %v: %s", err, out)
	}
}

func TestVMProvisionWindowNameIsSafe(t *testing.T) {
	name := vmProvisionWindowName("client.example/very long engagement name")
	if name == "" || nonSessionChar.MatchString(name) || len(name) > 40 {
		t.Fatalf("unsafe provisioning window name %q", name)
	}
}

func TestVMCreateFormCyclesFromCloneToProfiles(t *testing.T) {
	m := newTUI()
	m.vms = []VM{{Name: "base"}}
	m.vmTable.SetRows([]table.Row{{"  base", "Unassigned", "stopped", ""}})
	m.openVMCreateForm()
	m.cycleVMCreateSource(1)
	if m.vmCreateForm.mode != vmCreateFresh || m.vmCreateForm.profileID != "kali" {
		t.Fatalf("clone cycle = %#v", m.vmCreateForm)
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
	m.vms = []VM{{Name: "kali-base", State: "shut off", ProfileID: "kali"}}
	m.vmTable.SetRows([]table.Row{{"  kali-base", "stopped", "", "no"}})
	cmds := m.handleVMKey("r")
	if !m.busy {
		t.Fatal("model should be busy while RDP starts and resolves the VM")
	}
	if len(cmds) != 2 {
		t.Fatalf("got %d commands, want progress log and RDP command", len(cmds))
	}
}

func TestRDPRequiresAssignedProfile(t *testing.T) {
	m := newTUI()
	m.vms = []VM{{Name: "legacy", State: "shut off", ProfileID: fallbackGuestProfileID}}
	m.vmTable.SetRows([]table.Row{{"  legacy", "Unassigned", "stopped", ""}})
	cmds := m.handleVMKey("r")
	if m.busy || len(cmds) != 1 {
		t.Fatalf("unassigned RDP action: busy=%v commands=%d", m.busy, len(cmds))
	}
	msg, ok := cmds[0]().(logMsg)
	if !ok || msg.level != "warn" || !strings.Contains(msg.text, "vm profile") {
		t.Fatalf("unexpected profile warning: %#v", msg)
	}
}

func TestWindowsProfileDoesNotOfferSSH(t *testing.T) {
	m := newTUI()
	m.vms = []VM{{Name: "win11", State: "running", IP: "192.0.2.10", ProfileID: "windows-11-enterprise"}}
	m.vmTable.SetRows([]table.Row{{"  win11", "Windows 11 Enterprise", "running", "192.0.2.10"}})
	cmds := m.handleVMKey("c")
	if len(cmds) != 1 {
		t.Fatalf("got %d commands, want warning", len(cmds))
	}
	if m.busy {
		t.Fatal("model should not become busy for unsupported SSH")
	}
	msg, ok := cmds[0]().(logMsg)
	if !ok || msg.level != "warn" || !strings.Contains(msg.text, "RDP") {
		t.Fatalf("unexpected message: %#v", msg)
	}
}

func TestWindowsProfileBlocksWhonixMutation(t *testing.T) {
	m := newTUI()
	m.vms = []VM{{Name: "server", State: "shut off", ProfileID: "windows-server-2025"}}
	m.vmTable.SetRows([]table.Row{{"  server", "Windows Server 2025", "stopped", ""}})
	cmds := m.handleVMKey("w")
	if len(cmds) != 1 || m.busy {
		t.Fatalf("unsupported Whonix action: commands=%d busy=%v", len(cmds), m.busy)
	}
	msg, ok := cmds[0]().(logMsg)
	if !ok || msg.level != "warn" || !strings.Contains(msg.text, "unavailable") {
		t.Fatalf("unexpected message: %#v", msg)
	}
}

func TestVMViewFitsTerminalWithProfileColumn(t *testing.T) {
	for _, width := range []int{48, 80, 120} {
		m := newTUI()
		m.activeTab = 1
		updated, _ := m.Update(tea.WindowSizeMsg{Width: width, Height: 24})
		m = updated.(tuiModel)
		updated, _ = m.Update(vmsMsg{{
			Name: "provider_GOAD-DC01", State: "shut off", ProfileID: "windows-server-2025",
		}})
		m = updated.(tuiModel)
		for _, line := range strings.Split(m.View(), "\n") {
			if got := lipgloss.Width(line); got > width {
				t.Errorf("width %d: VM view line is %d cells: %q", width, got, line)
			}
		}
	}
}

func TestVMViewResizesAcrossColumnLayouts(t *testing.T) {
	m := newTUI()
	m.activeTab = 1
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 48, Height: 24})
	m = updated.(tuiModel)
	updated, _ = m.Update(vmsMsg{{Name: "win11", State: "running", ProfileID: "windows-11-enterprise"}})
	m = updated.(tuiModel)
	for _, width := range []int{120, 80, 48} {
		updated, _ = m.Update(tea.WindowSizeMsg{Width: width, Height: 24})
		m = updated.(tuiModel)
		for _, line := range strings.Split(m.View(), "\n") {
			if got := lipgloss.Width(line); got > width {
				t.Fatalf("width %d: resized VM view line is %d cells: %q", width, got, line)
			}
		}
	}
}

func TestVMTableUsesTerminalWidthAndKeepsEveryName(t *testing.T) {
	m := newTUI()
	m.activeTab = 1
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 188, Height: 40})
	m = updated.(tuiModel)
	vms := []VM{
		{Name: "dwyc"},
		{Name: "kali-base"},
		{Name: "provider_GOAD-DC01"},
		{Name: "ubuntu-test"},
		{Name: "win11-testing"},
	}
	updated, _ = m.Update(vmsMsg(vms))
	m = updated.(tuiModel)
	if got := m.vmTable.Width(); got != 188 {
		t.Fatalf("table viewport width = %d, want 188", got)
	}
	view := ansi.Strip(m.vmTable.View())
	for _, vm := range vms {
		if !strings.Contains(view, vm.Name) {
			t.Errorf("VM table dropped part of %q:\n%s", vm.Name, view)
		}
	}
}

func TestVMFinalizeKeyDoesNotPageTable(t *testing.T) {
	m := newTUI()
	m.activeTab = 1
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 24})
	m = updated.(tuiModel)
	updated, _ = m.Update(vmsMsg{{Name: "linux-a", ProfileID: "ubuntu-lts"}, {Name: "linux-b", ProfileID: "ubuntu-lts"}})
	m = updated.(tuiModel)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'f'}})
	m = updated.(tuiModel)
	if got := m.vmTable.Cursor(); got != 0 {
		t.Fatalf("finalize key moved table cursor to %d", got)
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

func TestVMRefreshPreservesSelectionByName(t *testing.T) {
	m := newTUI()
	m.vms = []VM{{Name: "alpha"}, {Name: "bravo"}}
	m.setVMRows()
	m.vmTable.SetCursor(1)
	updated, _ := m.Update(vmsMsg{{Name: "bravo"}, {Name: "alpha"}})
	got := updated.(tuiModel)
	if selected := got.selectedVM(); selected == nil || selected.Name != "bravo" {
		t.Fatalf("VM selection changed after refresh: %#v", selected)
	}
}

func TestCreatedContainerSelectsEngagement(t *testing.T) {
	m := newTUI()
	updated, _ := m.Update(containerCreatedMsg{name: "client-a"})
	got := updated.(tuiModel)
	if got.pendingSelect != "client-a" {
		t.Fatalf("pending selection = %q, want client-a", got.pendingSelect)
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
