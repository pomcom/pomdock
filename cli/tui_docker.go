package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

type containerRoute int

const (
	routeDirect containerRoute = iota
	routeVPN
	routeTor
	routeTorVPN
)

func (r containerRoute) String() string {
	return [...]string{"direct", "VPN", "Tor", "Tor + VPN"}[r]
}

func (r containerRoute) needsVPN() bool {
	return r == routeVPN || r == routeTorVPN
}

func (r containerRoute) usesTor() bool {
	return r == routeTor || r == routeTorVPN
}

type createField int

const (
	createName createField = iota
	createRoute
	createVPNFile
)

type containerCreateForm struct {
	active  bool
	field   createField
	route   containerRoute
	name    textinput.Model
	vpnFile textinput.Model
	err     string
}

func newContainerCreateForm() containerCreateForm {
	name := textinput.New()
	name.Prompt = ""
	name.Placeholder = "client-a"
	name.CharLimit = 128
	vpnFile := textinput.New()
	vpnFile.Prompt = ""
	vpnFile.Placeholder = "~/vpn/client.ovpn"
	vpnFile.CharLimit = 1024
	return containerCreateForm{name: name, vpnFile: vpnFile}
}

type containerConsole struct {
	target       string
	output       map[string][]string
	cwd          map[string]string
	history      []string
	historyIndex int
}

var dockerNamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]*$`)

func (m *tuiModel) openContainerCreateForm() tea.Cmd {
	m.createForm = newContainerCreateForm()
	m.createForm.active = true
	m.createForm.field = createName
	return m.focusCreateField()
}

func (m *tuiModel) focusCreateField() tea.Cmd {
	m.createForm.name.Blur()
	m.createForm.vpnFile.Blur()
	switch m.createForm.field {
	case createName:
		return m.createForm.name.Focus()
	case createVPNFile:
		return m.createForm.vpnFile.Focus()
	default:
		return nil
	}
}

func (m *tuiModel) updateContainerCreateForm(msg tea.KeyMsg) []tea.Cmd {
	switch msg.String() {
	case "esc":
		m.createForm = newContainerCreateForm()
		return nil
	case "tab":
		m.createForm.field++
		if m.createForm.field > createVPNFile ||
			(m.createForm.field == createVPNFile && !m.createForm.route.needsVPN()) {
			m.createForm.field = createName
		}
		return []tea.Cmd{m.focusCreateField()}
	case "shift+tab":
		m.createForm.field--
		if m.createForm.field < createName {
			if m.createForm.route.needsVPN() {
				m.createForm.field = createVPNFile
			} else {
				m.createForm.field = createRoute
			}
		}
		return []tea.Cmd{m.focusCreateField()}
	case "left", "h":
		if m.createForm.field == createRoute {
			m.createForm.route = (m.createForm.route + 3) % 4
			m.createForm.err = ""
			return nil
		}
	case "right", "l":
		if m.createForm.field == createRoute {
			m.createForm.route = (m.createForm.route + 1) % 4
			m.createForm.err = ""
			return nil
		}
	case "enter":
		opts, err := m.createFormOptions()
		if err != nil {
			m.createForm.err = err.Error()
			return nil
		}
		m.createForm.active = false
		m.createForm.name.Blur()
		m.createForm.vpnFile.Blur()
		m.busy = true
		return []tea.Cmd{
			emit(logMsg{level: "info", text: fmt.Sprintf("Creating '%s' (%s)...", opts.Name, m.createForm.route)}),
			createContainerCmd(opts),
		}
	}
	return []tea.Cmd{m.updateCreateInput(msg)}
}

func (m *tuiModel) updateCreateInput(msg tea.Msg) tea.Cmd {
	var cmd tea.Cmd
	switch m.createForm.field {
	case createName:
		m.createForm.name, cmd = m.createForm.name.Update(msg)
	case createVPNFile:
		m.createForm.vpnFile, cmd = m.createForm.vpnFile.Update(msg)
	}
	if _, ok := msg.(tea.KeyMsg); ok {
		m.createForm.err = ""
	}
	return cmd
}

func (m *tuiModel) createFormOptions() (ContainerCreateOptions, error) {
	name := strings.TrimSpace(m.createForm.name.Value())
	if !dockerNamePattern.MatchString(name) {
		return ContainerCreateOptions{}, fmt.Errorf("name must use letters, numbers, '.', '_' or '-'")
	}
	for _, container := range m.containers {
		if container.Name == name {
			return ContainerCreateOptions{}, fmt.Errorf("container '%s' already exists", name)
		}
	}
	opts := ContainerCreateOptions{Name: name, Whonix: m.createForm.route.usesTor()}
	if m.createForm.route.needsVPN() {
		vpnFile := expandHome(strings.TrimSpace(m.createForm.vpnFile.Value()))
		info, err := os.Stat(vpnFile)
		if err != nil || info.IsDir() {
			return ContainerCreateOptions{}, fmt.Errorf("VPN config not found: %s", vpnFile)
		}
		opts.VPNFile = vpnFile
	}
	return opts, nil
}

func expandHome(path string) string {
	if path == "~" {
		return os.Getenv("HOME")
	}
	if strings.HasPrefix(path, "~/") {
		return filepath.Join(os.Getenv("HOME"), strings.TrimPrefix(path, "~/"))
	}
	return path
}

func createContainerCmd(opts ContainerCreateOptions) tea.Cmd {
	return func() tea.Msg {
		return containerCreatedMsg{name: opts.Name, err: CreatePentestContainer(opts)}
	}
}

func (m *tuiModel) focusContainerConsole(name string) tea.Cmd {
	m.console.target = name
	if m.console.cwd[name] == "" {
		m.console.cwd[name] = "/home/kali"
	}
	m.console.historyIndex = len(m.console.history)
	return m.commandInput.Focus()
}

func (m *tuiModel) updateContainerConsole(msg tea.KeyMsg) []tea.Cmd {
	switch msg.String() {
	case "esc":
		m.commandInput.Blur()
		return nil
	case "ctrl+l":
		m.console.output[m.console.target] = nil
		return nil
	case "up":
		if m.console.historyIndex > 0 {
			m.console.historyIndex--
			m.commandInput.SetValue(m.console.history[m.console.historyIndex])
			m.commandInput.CursorEnd()
		}
		return nil
	case "down":
		if m.console.historyIndex < len(m.console.history)-1 {
			m.console.historyIndex++
			m.commandInput.SetValue(m.console.history[m.console.historyIndex])
		} else {
			m.console.historyIndex = len(m.console.history)
			m.commandInput.SetValue("")
		}
		m.commandInput.CursorEnd()
		return nil
	case "enter":
		command := strings.TrimSpace(m.commandInput.Value())
		if command == "" || m.busy {
			return nil
		}
		name := m.console.target
		m.console.output[name] = append(m.console.output[name], "$ "+command)
		m.console.history = append(m.console.history, command)
		m.console.historyIndex = len(m.console.history)
		m.commandInput.SetValue("")
		m.busy = true
		return []tea.Cmd{runContainerCommandCmd(name, m.console.cwd[name], command)}
	}
	var cmd tea.Cmd
	m.commandInput, cmd = m.commandInput.Update(msg)
	return []tea.Cmd{cmd}
}

func runContainerCommandCmd(name, cwd, command string) tea.Cmd {
	return func() tea.Msg {
		output, nextCWD, err := RunContainerCommand(name, cwd, command)
		return containerCommandMsg{
			name: name, command: command, output: output, cwd: nextCWD, err: err,
		}
	}
}

func (m *tuiModel) appendConsoleResult(msg containerCommandMsg) {
	output := strings.ReplaceAll(ansi.Strip(msg.output), "\r\n", "\n")
	if output != "" {
		m.console.output[msg.name] = append(m.console.output[msg.name], strings.Split(output, "\n")...)
	}
	if msg.err != nil {
		m.console.output[msg.name] = append(m.console.output[msg.name], "[command failed] "+msg.err.Error())
	}
	if msg.cwd != "" {
		m.console.cwd[msg.name] = msg.cwd
	}
	lines := m.console.output[msg.name]
	if len(lines) > 300 {
		m.console.output[msg.name] = lines[len(lines)-300:]
	}
}

func (m tuiModel) containerCreateFormView() string {
	width := m.width - 6
	if width < 32 {
		width = 32
	}
	name := m.createForm.name
	vpnFile := m.createForm.vpnFile
	name.Width = width - 14
	vpnFile.Width = width - 14

	marker := func(field createField) string {
		if m.createForm.field == field {
			return styleAccent.Render("▶")
		}
		return " "
	}
	routes := make([]string, 4)
	for route := routeDirect; route <= routeTorVPN; route++ {
		label := route.String()
		if m.width < 72 && route == routeTorVPN {
			label = "Tor+VPN"
		}
		padding := " "
		if m.width >= 72 {
			padding = "  "
		}
		if m.createForm.route == route {
			routes[route] = styleAccent.Copy().Bold(true).Render("[" + padding + label + padding + "]")
		} else {
			routes[route] = styleMuted.Render(" " + label + " ")
		}
	}

	lines := []string{
		styleAccent.Copy().Bold(true).Render("  NEW CONTAINER"),
		"",
		fmt.Sprintf("  %s  %-9s %s", marker(createName), "Name", name.View()),
		fmt.Sprintf("  %s  %-9s %s", marker(createRoute), "Route", strings.Join(routes, " ")),
	}
	if m.createForm.route.needsVPN() {
		lines = append(lines, fmt.Sprintf("  %s  %-9s %s", marker(createVPNFile), "VPN file", vpnFile.View()))
	}
	if m.createForm.err != "" {
		for _, line := range wrapWords(m.createForm.err, m.width-4) {
			lines = append(lines, styleError.Render("  "+line))
		}
	}
	help := "  tab fields   ←/→ route   enter create   esc cancel"
	if m.width < 64 {
		help = "  tab field   enter create   esc cancel"
	}
	lines = append(lines, "", styleMuted.Render(help))
	return strings.Join(lines, "\n")
}

func (m tuiModel) dockerWorkspaceView() string {
	panelHeight := m.height - 14
	if panelHeight < 10 {
		panelHeight = 10
	}
	if m.width >= 110 {
		listWidth := m.width * 45 / 100
		consoleWidth := m.width - listWidth - 3
		left := m.dockerListView(listWidth, panelHeight)
		right := m.containerConsoleView(consoleWidth, panelHeight)
		divider := styleMuted.Render(strings.TrimSuffix(strings.Repeat("│\n", panelHeight), "\n"))
		return lipgloss.JoinHorizontal(lipgloss.Top, left, " "+divider+" ", right)
	}
	listHeight := panelHeight / 2
	if rows := len(m.containers) + 2; rows < listHeight {
		listHeight = rows
	}
	if listHeight < 4 {
		listHeight = 4
	}
	consoleHeight := panelHeight - listHeight - 1
	return strings.Join([]string{
		m.dockerListView(m.width, listHeight),
		styleMuted.Render(strings.Repeat("─", m.width)),
		m.containerConsoleView(m.width, consoleHeight),
	}, "\n")
}

func (m tuiModel) dockerListView(width, height int) string {
	nameWidth := width - 32
	if nameWidth < 12 {
		nameWidth = 12
	}
	header := "   " + padRight("NAME", nameWidth) + "  " + padRight("STATUS", 9) + "  VPN  TOR"
	lines := []string{styleAccent.Render(header), styleMuted.Render(strings.Repeat("─", width))}
	if len(m.containers) == 0 {
		return strings.Join(append(lines, styleMuted.Render("  No containers. Press n to create one.")), "\n")
	}

	maxRows := height - 2
	if maxRows < 1 {
		maxRows = 1
	}
	start := 0
	if m.dockerCursor >= maxRows {
		start = m.dockerCursor - maxRows + 1
	}
	end := start + maxRows
	if end > len(m.containers) {
		end = len(m.containers)
	}
	for i := start; i < end; i++ {
		container := m.containers[i]
		cursor := "  "
		if i == m.dockerCursor {
			cursor = styleAccent.Render("▶ ")
		}
		vpn, tor := styleMuted.Render("no "), styleMuted.Render("no ")
		if container.HasVPN {
			vpn = styleOK.Render("yes")
		}
		if container.HasTor {
			tor = styleOK.Render("yes")
		}
		name := padRight(ansi.Truncate(container.Name, nameWidth, "…"), nameWidth)
		status := containerStatusLabel(container.Status, 9)
		lines = append(lines, fmt.Sprintf("%s%s %s  %s  %s  %s",
			cursor, icon(container.Status), name, status, vpn, tor))
	}
	return strings.Join(lines, "\n")
}

func (m tuiModel) containerConsoleView(width, height int) string {
	container := m.selectedContainer()
	if container == nil {
		return styleMuted.Render("  COMMAND\n\n  Select or create a container.")
	}
	name := container.Name
	focused := m.commandInput.Focused() && m.console.target == name
	title := "  COMMAND · " + name
	if focused {
		title = styleAccent.Copy().Bold(true).Render(title)
	} else {
		title = styleMuted.Render(title)
	}
	lines := []string{title, styleMuted.Render(strings.Repeat("─", width))}

	bodyHeight := height - 3
	if bodyHeight < 1 {
		bodyHeight = 1
	}
	wrapped := wrapConsoleLines(m.console.output[name], width-2)
	if len(wrapped) > bodyHeight {
		wrapped = wrapped[len(wrapped)-bodyHeight:]
	}
	for len(wrapped) < bodyHeight {
		wrapped = append([]string{""}, wrapped...)
	}
	for _, line := range wrapped {
		lines = append(lines, "  "+line)
	}

	if container.Status != "running" {
		lines = append(lines, styleMuted.Render("  Start the container to run commands."))
		return strings.Join(lines, "\n")
	}
	cwd := m.console.cwd[name]
	if cwd == "" {
		cwd = "/home/kali"
	}
	prompt := fmt.Sprintf("  %s:%s $ ", name, shortContainerPath(cwd))
	input := m.commandInput
	input.Width = width - lipgloss.Width(prompt) - 1
	if input.Width < 8 {
		input.Width = 8
	}
	lines = append(lines, styleAccent.Render(prompt)+input.View())
	return strings.Join(lines, "\n")
}

func shortContainerPath(path string) string {
	if path == "/home/kali" {
		return "~"
	}
	if strings.HasPrefix(path, "/home/kali/") {
		return "~/" + strings.TrimPrefix(path, "/home/kali/")
	}
	return path
}

func wrapConsoleLines(lines []string, width int) []string {
	if width < 1 {
		width = 1
	}
	var wrapped []string
	for _, line := range lines {
		line = ansi.Strip(line)
		if line == "" {
			wrapped = append(wrapped, "")
			continue
		}
		for lipgloss.Width(line) > width {
			wrapped = append(wrapped, ansi.Cut(line, 0, width))
			line = ansi.Cut(line, width, lipgloss.Width(line))
		}
		wrapped = append(wrapped, line)
	}
	return wrapped
}

func padRight(value string, width int) string {
	padding := width - lipgloss.Width(value)
	if padding < 0 {
		padding = 0
	}
	return value + strings.Repeat(" ", padding)
}

func containerStatusLabel(status string, width int) string {
	label := status
	style := styleMuted
	switch status {
	case "running":
		style = styleOK
	case "exited", "stopped", "shut off":
		label = "stopped"
	case "paused":
		style = styleWarn
	}
	return style.Render(padRight(label, width))
}
