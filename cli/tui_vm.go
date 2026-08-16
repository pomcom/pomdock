package main

import (
	"fmt"
	"os"
	"os/exec"
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
	active bool
	field  int
	mode   vmCreateMode
	source string
	name   textinput.Model
	err    string
}

type vmCreateOptions struct {
	name   string
	mode   vmCreateMode
	source string
}

type vmCreatedMsg struct {
	name string
	err  error
}

func newVMCreateForm() vmCreateForm {
	name := textinput.New()
	name.Prompt = ""
	name.Placeholder = "client-vm"
	name.CharLimit = 128
	return vmCreateForm{name: name}
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
		m.vmCreateForm.field = (m.vmCreateForm.field + 1) % 2
		if m.vmCreateForm.field == 0 {
			return []tea.Cmd{m.vmCreateForm.name.Focus()}
		}
		m.vmCreateForm.name.Blur()
		return nil
	case "left", "h", "right", "l":
		if m.vmCreateForm.field == 1 && m.vmCreateForm.source != "" {
			m.vmCreateForm.mode = (m.vmCreateForm.mode + 1) % 2
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
		m.busy = true
		action := "Provisioning fresh Kali VM"
		if opts.mode == vmCreateClone {
			action = fmt.Sprintf("Cloning '%s'", opts.source)
		}
		return []tea.Cmd{
			emit(logMsg{level: "info", text: fmt.Sprintf("%s as '%s'...", action, opts.name)}),
			createVMCmd(opts),
		}
	}
	var cmd tea.Cmd
	m.vmCreateForm.name, cmd = m.vmCreateForm.name.Update(msg)
	m.vmCreateForm.err = ""
	return []tea.Cmd{cmd}
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
		return vmCreateOptions{}, fmt.Errorf("select a source VM or choose Fresh Kali")
	}
	if m.vmCreateForm.mode == vmCreateFresh {
		if _, err := os.Stat(vmScript("kali-libvirt-setup.sh")); err != nil {
			return vmCreateOptions{}, fmt.Errorf("VM setup script is not installed")
		}
	}
	return vmCreateOptions{name: name, mode: m.vmCreateForm.mode, source: m.vmCreateForm.source}, nil
}

func createVMCmd(opts vmCreateOptions) tea.Cmd {
	if opts.mode == vmCreateClone {
		return func() tea.Msg {
			return vmCreatedMsg{name: opts.name, err: CloneVM(opts.source, opts.name)}
		}
	}
	cmd := exec.Command("bash", vmScript("kali-libvirt-setup.sh"), opts.name)
	return tea.ExecProcess(cmd, func(err error) tea.Msg {
		return vmCreatedMsg{name: opts.name, err: err}
	})
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
	cloneLabel := "Clone selected"
	if m.vmCreateForm.source != "" {
		if m.width >= 64 {
			maxSource := m.width - 52
			if maxSource < 8 {
				maxSource = 8
			}
			cloneLabel = "Clone " + ansi.Truncate(m.vmCreateForm.source, maxSource, "…")
		}
	}
	modes := []string{cloneLabel, "Fresh Kali"}
	for i := range modes {
		if vmCreateMode(i) == m.vmCreateForm.mode {
			modes[i] = styleAccent.Copy().Bold(true).Render("[ " + modes[i] + " ]")
		} else {
			modes[i] = styleMuted.Render("  " + modes[i] + "  ")
		}
	}
	lines := []string{
		styleAccent.Copy().Bold(true).Render("  NEW VM ENGAGEMENT"),
		"",
		fmt.Sprintf("  %s  %-8s %s", marker(0), "Name", input.View()),
		fmt.Sprintf("  %s  %-8s %s", marker(1), "Source", strings.Join(modes, "  ")),
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
