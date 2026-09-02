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

type transferDirection int

const (
	transferUpload transferDirection = iota
	transferDownload
)

type containerTransferForm struct {
	active      bool
	direction   transferDirection
	field       int
	container   string
	source      textinput.Model
	destination textinput.Model
	err         string
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
	output map[string][]string
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

func runContainerToolCmd(name, tool string) tea.Cmd {
	return func() tea.Msg {
		output, err := RunContainerTool(name, tool)
		return containerCommandMsg{
			name: name, command: tool, output: output, err: err,
		}
	}
}

func (m *tuiModel) appendConsoleResult(msg containerCommandMsg) {
	lines := []string{"[" + strings.ToUpper(msg.command) + "]"}
	output := strings.ReplaceAll(ansi.Strip(msg.output), "\r\n", "\n")
	if output != "" {
		lines = append(lines, strings.Split(output, "\n")...)
	}
	if msg.err != nil {
		lines = append(lines, "[check failed] "+msg.err.Error())
	}
	if len(lines) > 300 {
		lines = lines[len(lines)-300:]
	}
	m.console.output[msg.name] = lines
}

func (m *tuiModel) openContainerTransferForm(container string, direction transferDirection) tea.Cmd {
	source := textinput.New()
	source.Prompt = ""
	source.CharLimit = 2048
	destination := textinput.New()
	destination.Prompt = ""
	destination.CharLimit = 2048
	if direction == transferUpload {
		source.Placeholder = "~/loot/report.txt"
		destination.SetValue("/home/kali/pentest/")
	} else {
		source.SetValue("/home/kali/pentest/")
		destination.SetValue("~/pentest/" + container + "/")
	}
	m.transferForm = containerTransferForm{
		active: true, direction: direction, container: container,
		source: source, destination: destination,
	}
	return m.focusTransferField()
}

func (m *tuiModel) focusTransferField() tea.Cmd {
	m.transferForm.source.Blur()
	m.transferForm.destination.Blur()
	if m.transferForm.field == 0 {
		return m.transferForm.source.Focus()
	}
	return m.transferForm.destination.Focus()
}

func (m *tuiModel) updateContainerTransferForm(msg tea.KeyMsg) []tea.Cmd {
	switch msg.String() {
	case "esc":
		m.transferForm = containerTransferForm{}
		return nil
	case "tab", "shift+tab":
		m.transferForm.field = (m.transferForm.field + 1) % 2
		return []tea.Cmd{m.focusTransferField()}
	case "enter":
		source := strings.TrimSpace(m.transferForm.source.Value())
		destination := strings.TrimSpace(m.transferForm.destination.Value())
		if source == "" || destination == "" {
			m.transferForm.err = "source and destination are required"
			return nil
		}
		form := m.transferForm
		m.transferForm.active = false
		m.busy = true
		return []tea.Cmd{func() tea.Msg {
			var err error
			if form.direction == transferUpload {
				err = CopyToContainer(form.container, source, destination)
			} else {
				err = CopyFromContainer(form.container, source, destination)
			}
			return containerTransferMsg{
				name: form.container, direction: form.direction,
				source: source, destination: destination, err: err,
			}
		}}
	}
	return []tea.Cmd{m.updateTransferInput(msg)}
}

func (m *tuiModel) updateTransferInput(msg tea.Msg) tea.Cmd {
	var cmd tea.Cmd
	if m.transferForm.field == 0 {
		m.transferForm.source, cmd = m.transferForm.source.Update(msg)
	} else {
		m.transferForm.destination, cmd = m.transferForm.destination.Update(msg)
	}
	if _, ok := msg.(tea.KeyMsg); ok {
		m.transferForm.err = ""
	}
	return cmd
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

func (m tuiModel) containerTransferFormView() string {
	source := m.transferForm.source
	destination := m.transferForm.destination
	inputWidth := m.width - 22
	if inputWidth < 16 {
		inputWidth = 16
	}
	source.Width, destination.Width = inputWidth, inputWidth
	marker := func(field int) string {
		if m.transferForm.field == field {
			return styleAccent.Render("▶")
		}
		return " "
	}
	title := "UPLOAD TO " + m.transferForm.container
	if m.transferForm.direction == transferDownload {
		title = "DOWNLOAD FROM " + m.transferForm.container
	}
	lines := []string{
		styleAccent.Copy().Bold(true).Render("  " + title),
		"",
		fmt.Sprintf("  %s  %-11s %s", marker(0), "Source", source.View()),
		fmt.Sprintf("  %s  %-11s %s", marker(1), "Destination", destination.View()),
	}
	if m.transferForm.err != "" {
		lines = append(lines, "", styleError.Render("  "+m.transferForm.err))
	}
	lines = append(lines, "", styleMuted.Render("  tab field   enter transfer   esc cancel"))
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
		if container.Legacy {
			status = styleWarn.Render(padRight("upgrade", 9))
		}
		lines = append(lines, fmt.Sprintf("%s%s %s  %s  %s  %s",
			cursor, icon(container.Status), name, status, vpn, tor))
	}
	return strings.Join(lines, "\n")
}

func (m tuiModel) containerConsoleView(width, height int) string {
	container := m.selectedContainer()
	if container == nil {
		return styleMuted.Render("  ENGAGEMENT\n\n  Select or create a container.")
	}
	name := container.Name
	title := styleAccent.Copy().Bold(true).Render("  ENGAGEMENT · " + name)
	lines := []string{title, styleMuted.Render(strings.Repeat("─", width))}

	menu := []string{
		"  " + styleAccent.Render("i") + "  Identity and egress",
		"  " + styleAccent.Render("p") + "  Listening and published ports",
		"  " + styleAccent.Render("t") + "  Tor exit check",
		"  " + styleAccent.Render("u") + "  Upload file",
		"  " + styleAccent.Render("d") + "  Download file",
		"  " + styleAccent.Render("C") + "  Full tmux shell",
	}
	if width < 42 {
		menu[0] = "  i  Identity / egress"
		menu[1] = "  p  Ports"
		menu[2] = "  t  Tor check"
	}
	lines = append(lines, menu...)
	lines = append(lines, styleMuted.Render(strings.Repeat("─", width)))

	bodyHeight := height - len(menu) - 4
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
		lines = append(lines, styleMuted.Render("  Start the container to run checks."))
	}
	return strings.Join(lines, "\n")
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
