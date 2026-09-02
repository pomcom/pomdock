package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

type vmCreateMode int

const (
	vmCreateClone vmCreateMode = iota
	vmCreateFresh
)

type vmCreateForm struct {
	active    bool
	field     int
	mode      vmCreateMode
	source    string
	profileID string
	name      textinput.Model
	iso       textinput.Model
	err       string
}

type vmCreateOptions struct {
	name      string
	mode      vmCreateMode
	source    string
	profileID string
	iso       string
}

type vmCreatedMsg struct {
	name        string
	openConsole bool
	warning     string
	err         error
}

type vmProvisionStartedMsg struct {
	name    string
	window  string
	profile GuestProfile
	err     error
}

func newVMCreateForm() vmCreateForm {
	name := textinput.New()
	name.Prompt = ""
	name.Placeholder = "client-vm"
	name.CharLimit = 128
	iso := textinput.New()
	iso.Prompt = ""
	iso.Placeholder = "/path/to/windows.iso"
	iso.CharLimit = 1024
	return vmCreateForm{name: name, iso: iso, profileID: defaultGuestProfileID}
}

func (m *tuiModel) openVMCreateForm() tea.Cmd {
	m.vmCreateForm = newVMCreateForm()
	m.vmCreateForm.active = true
	if selected := m.selectedVM(); selected != nil {
		m.vmCreateForm.source = selected.Name
		m.vmCreateForm.name.Placeholder = selected.Name + "-engagement"
	} else {
		m.vmCreateForm.mode = vmCreateFresh
	}
	return m.vmCreateForm.name.Focus()
}

func (m *tuiModel) updateVMCreateForm(msg tea.KeyMsg) []tea.Cmd {
	switch msg.String() {
	case "esc":
		m.vmCreateForm = newVMCreateForm()
		return nil
	case "tab", "shift+tab":
		delta := 1
		if msg.String() == "shift+tab" {
			delta = -1
		}
		fields := m.vmCreateFieldCount()
		m.vmCreateForm.field = (m.vmCreateForm.field + delta + fields) % fields
		return []tea.Cmd{m.focusVMCreateField()}
	case "left", "h":
		if m.vmCreateForm.field == 1 {
			m.cycleVMCreateSource(-1)
			m.focusVMCreateField()
			m.vmCreateForm.err = ""
			return nil
		}
	case "right", "l":
		if m.vmCreateForm.field == 1 {
			m.cycleVMCreateSource(1)
			m.focusVMCreateField()
			m.vmCreateForm.err = ""
			return nil
		}
	case "enter":
		opts, err := m.vmCreateOptions()
		if err != nil {
			m.vmCreateForm.err = err.Error()
			return nil
		}
		m.vmCreateForm.active = false
		m.vmCreateForm.name.Blur()
		m.vmCreateForm.iso.Blur()
		m.busy = true
		profile := GuestProfileByID(opts.profileID)
		action := "Provisioning " + profile.Label
		if opts.mode == vmCreateClone {
			action = fmt.Sprintf("Cloning '%s'", opts.source)
		}
		return []tea.Cmd{
			emit(logMsg{level: "info", text: fmt.Sprintf("%s as '%s'...", action, opts.name)}),
			createVMCmd(opts),
		}
	}
	var cmd tea.Cmd
	if m.vmCreateForm.field == 2 {
		m.vmCreateForm.iso, cmd = m.vmCreateForm.iso.Update(msg)
	} else {
		m.vmCreateForm.name, cmd = m.vmCreateForm.name.Update(msg)
	}
	m.vmCreateForm.err = ""
	return []tea.Cmd{cmd}
}

func (m tuiModel) vmCreateFieldCount() int {
	if m.vmCreateForm.mode == vmCreateFresh && GuestProfileByID(m.vmCreateForm.profileID).Family == "windows" {
		return 3
	}
	return 2
}

func (m *tuiModel) focusVMCreateField() tea.Cmd {
	m.vmCreateForm.name.Blur()
	m.vmCreateForm.iso.Blur()
	if m.vmCreateForm.field == 0 {
		return m.vmCreateForm.name.Focus()
	}
	if m.vmCreateForm.field == 2 {
		return m.vmCreateForm.iso.Focus()
	}
	return nil
}

type vmCreateChoice struct {
	mode      vmCreateMode
	profileID string
	label     string
}

func (m tuiModel) vmCreateChoices() []vmCreateChoice {
	choices := make([]vmCreateChoice, 0, len(provisionableGuestProfileIDs)+1)
	if m.vmCreateForm.source != "" {
		choices = append(choices, vmCreateChoice{mode: vmCreateClone, label: "Clone " + m.vmCreateForm.source})
	}
	for _, profile := range ProvisionableGuestProfiles() {
		choices = append(choices, vmCreateChoice{mode: vmCreateFresh, profileID: profile.ID, label: profile.Label})
	}
	return choices
}

func (m *tuiModel) cycleVMCreateSource(delta int) {
	choices := m.vmCreateChoices()
	if len(choices) == 0 {
		return
	}
	current := 0
	for i, choice := range choices {
		if choice.mode == m.vmCreateForm.mode &&
			(choice.mode == vmCreateClone || choice.profileID == m.vmCreateForm.profileID) {
			current = i
			break
		}
	}
	current = (current + delta + len(choices)) % len(choices)
	m.vmCreateForm.mode = choices[current].mode
	if choices[current].profileID != "" {
		m.vmCreateForm.profileID = choices[current].profileID
	}
	if m.vmCreateForm.field >= m.vmCreateFieldCount() {
		m.vmCreateForm.field = 1
	}
}

func (m tuiModel) vmCreateOptions() (vmCreateOptions, error) {
	name := strings.TrimSpace(m.vmCreateForm.name.Value())
	if !dockerNamePattern.MatchString(name) {
		return vmCreateOptions{}, fmt.Errorf("name must use letters, numbers, '.', '_' or '-'")
	}
	for _, vm := range m.vms {
		if vm.Name == name {
			return vmCreateOptions{}, fmt.Errorf("VM '%s' already exists", name)
		}
	}
	if m.vmCreateForm.mode == vmCreateClone && m.vmCreateForm.source == "" {
		return vmCreateOptions{}, fmt.Errorf("select a source VM or guest profile")
	}
	if m.vmCreateForm.mode == vmCreateFresh {
		if _, _, err := PrepareVMProvision(VMProvisionOptions{
			ProfileID: m.vmCreateForm.profileID,
			Name:      name,
			ISO:       strings.TrimSpace(m.vmCreateForm.iso.Value()),
		}); err != nil {
			return vmCreateOptions{}, err
		}
	}
	return vmCreateOptions{
		name: name, mode: m.vmCreateForm.mode, source: m.vmCreateForm.source,
		profileID: m.vmCreateForm.profileID, iso: strings.TrimSpace(m.vmCreateForm.iso.Value()),
	}, nil
}

func createVMCmd(opts vmCreateOptions) tea.Cmd {
	if opts.mode == vmCreateClone {
		return func() tea.Msg {
			if err := CloneVM(opts.source, opts.name); err != nil {
				return vmCreatedMsg{name: opts.name, err: err}
			}
			msg := vmCreatedMsg{name: opts.name}
			if err := CopyVMProfileID(opts.source, opts.name); err != nil {
				msg.warning = "profile metadata could not be copied: " + err.Error()
			}
			return msg
		}
	}
	return func() tea.Msg {
		window, profile, err := startVMProvisionWindow(opts)
		return vmProvisionStartedMsg{name: opts.name, window: window, profile: profile, err: err}
	}
}

func startVMProvisionWindow(opts vmCreateOptions) (string, GuestProfile, error) {
	cmd, profile, err := PrepareVMProvision(VMProvisionOptions{
		ProfileID: opts.profileID,
		Name:      opts.name,
		ISO:       opts.iso,
	})
	if err != nil {
		return "", GuestProfile{}, err
	}
	executable, err := os.Executable()
	if err != nil {
		return "", GuestProfile{}, fmt.Errorf("resolve Pomdock executable: %w", err)
	}
	if resolved, resolveErr := filepath.EvalSymlinks(executable); resolveErr == nil {
		executable = resolved
	}
	script := vmProvisionShellCommand(cmd, executable, opts.name, profile.ID)
	windowName := vmProvisionWindowName(opts.name)
	out, err := exec.Command("tmux", "new-window", "-P", "-F", "#{window_id}",
		"-t", workspaceSession+":", "-n", windowName,
		"exec bash -lc "+shellQuote(script)).CombinedOutput()
	if err != nil {
		return "", GuestProfile{}, fmt.Errorf("create provisioning window: %s", strings.TrimSpace(string(out)))
	}
	windowID := strings.TrimSpace(string(out))
	_ = exec.Command("tmux", "set-option", "-w", "-t", windowID, "automatic-rename", "off").Run()
	_ = exec.Command("tmux", "set-option", "-w", "-t", windowID, "@pomdock_job", "vm-provision").Run()
	_ = exec.Command("tmux", "set-option", "-w", "-t", windowID, "@pomdock_vm", opts.name).Run()
	return windowID, profile, nil
}

func vmProvisionWindowName(name string) string {
	return strings.TrimPrefix(shellWindowName("vm-setup-"+name), "shell-")
}

func startVMRDPWindow(name string, cmd *exec.Cmd) (string, error) {
	if exec.Command("tmux", "has-session", "-t", "="+workspaceSession).Run() != nil {
		return "", fmt.Errorf("Pomdock tmux workspace is not running")
	}
	script := vmRDPShellCommand(cmd, name)
	windowName := strings.TrimPrefix(shellWindowName("rdp-"+name), "shell-")
	out, err := exec.Command("tmux", "new-window", "-d", "-P", "-F", "#{window_id}",
		"-t", workspaceSession+":", "-n", windowName,
		"exec bash -lc "+shellQuote(script)).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("create RDP window: %s", strings.TrimSpace(string(out)))
	}
	windowID := strings.TrimSpace(string(out))
	for key, value := range map[string]string{
		"@pomdock_job": "rdp",
		"@pomdock_vm":  name,
	} {
		if out, err := exec.Command("tmux", "set-option", "-w", "-t", windowID, key, value).CombinedOutput(); err != nil {
			_ = exec.Command("tmux", "kill-window", "-t", windowID).Run()
			return "", fmt.Errorf("label RDP window: %s", strings.TrimSpace(string(out)))
		}
	}
	_ = exec.Command("tmux", "set-option", "-w", "-t", windowID, "automatic-rename", "off").Run()
	if err := SelectShellWindow(windowID); err != nil {
		_ = exec.Command("tmux", "kill-window", "-t", windowID).Run()
		return "", err
	}
	return windowID, nil
}

func vmRDPShellCommand(cmd *exec.Cmd, name string) string {
	dashboard := shellQuote(workspaceSession + ":" + dashboardWindow)
	return fmt.Sprintf(`%s
status=$?
if [ "$status" -eq 0 ]; then
  printf '\nRDP session for %s closed. Returning to the dashboard...\n'
  sleep 1
  tmux select-window -t %s 2>/dev/null || true
  exit 0
fi
printf '\nFreeRDP for %s failed with status %%s. Returning to the dashboard in 8 seconds.\n' "$status"
sleep 8
tmux select-window -t %s 2>/dev/null || true
exit "$status"
`, shellJoin(cmd.Args), name, dashboard, name, dashboard)
}

func shellJoin(args []string) string {
	quoted := make([]string, len(args))
	for i, arg := range args {
		quoted[i] = shellQuote(arg)
	}
	return strings.Join(quoted, " ")
}

func vmProvisionShellCommand(cmd *exec.Cmd, executable, name, profileID string) string {
	provision := shellJoin(cmd.Args)
	setProfile := shellJoin([]string{executable, "vm", "profile", name, profileID})
	dashboard := shellQuote(workspaceSession + ":" + dashboardWindow)
	return fmt.Sprintf(`set +e
%s
status=$?
if [ "$status" -eq 0 ]; then
  %s
  status=$?
fi
printf '\n'
if [ "$status" -eq 0 ]; then
  printf 'Pomdock VM %s is ready. Returning to the dashboard...\n'
  sleep 3
  tmux select-window -t %s 2>/dev/null || true
  exit 0
fi
printf 'Pomdock VM setup failed with status %%s. This window remains open for diagnosis.\n' "$status"
exec "${SHELL:-/bin/bash}" -l
`, provision, setProfile, name, dashboard)
}

func (m tuiModel) vmCreateFormView() string {
	input := m.vmCreateForm.name
	input.Width = m.width - 18
	if input.Width < 16 {
		input.Width = 16
	}
	marker := func(field int) string {
		if m.vmCreateForm.field == field {
			return styleAccent.Render("▶")
		}
		return " "
	}
	selectedSource := GuestProfileByID(m.vmCreateForm.profileID).Label
	if m.vmCreateForm.mode == vmCreateClone {
		selectedSource = "Clone " + m.vmCreateForm.source
	}
	maxSource := m.width - 22
	if maxSource < 12 {
		maxSource = 12
	}
	selectedSource = ansi.Truncate(selectedSource, maxSource, "…")
	selector := styleMuted.Render("‹") + " " + styleAccent.Copy().Bold(true).Render(selectedSource) + " " + styleMuted.Render("›")
	lines := []string{
		styleAccent.Copy().Bold(true).Render("  NEW VM ENGAGEMENT"),
		"",
		fmt.Sprintf("  %s  %-8s %s", marker(0), "Name", input.View()),
		fmt.Sprintf("  %s  %-8s %s", marker(1), "Source", selector),
	}
	if m.vmCreateFieldCount() == 3 {
		iso := m.vmCreateForm.iso
		iso.Width = input.Width
		lines = append(lines, fmt.Sprintf("  %s  %-8s %s", marker(2), "ISO", iso.View()))
	}
	if m.vmCreateForm.err != "" {
		for _, line := range wrapWords(m.vmCreateForm.err, m.width-4) {
			lines = append(lines, styleError.Render("  "+line))
		}
	}
	help := "  tab fields   ←/→ source   enter create   esc cancel"
	if m.width < 64 {
		help = "  tab field   enter create   esc cancel"
	}
	lines = append(lines, "", styleMuted.Render(help))
	return strings.Join(lines, "\n")
}
