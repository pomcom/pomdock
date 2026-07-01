package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/table"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// ── Messages ──────────────────────────────────────────────────────────────────

type tickMsg time.Time
type containersMsg []Container
type vmsMsg []VM
type logMsg struct {
	level string // "info" "ok" "warn" "err"
	text  string
}
type doneMsg struct {
	text string
	err  error
}

// ── Tab styles ────────────────────────────────────────────────────────────────

var (
	tabActive = lipgloss.NewStyle().
			Foreground(colorMauve).
			Bold(true).
			Padding(0, 2).
			BorderStyle(lipgloss.NormalBorder()).
			BorderBottom(false).
			BorderForeground(colorMauve)

	tabInactive = lipgloss.NewStyle().
			Foreground(colorMuted).
			Padding(0, 2).
			BorderStyle(lipgloss.HiddenBorder())

	tuiSep = lipgloss.NewStyle().Foreground(colorMuted)

	tuiHelp = lipgloss.NewStyle().
		Foreground(colorMuted).
		Padding(0, 1)

	tuiLogTime = lipgloss.NewStyle().Foreground(colorMuted)

	tuiConfirm = lipgloss.NewStyle().
			Foreground(colorRed).
			Bold(true).
			Padding(0, 1)

	tuiBusy = lipgloss.NewStyle().Foreground(colorYellow)
)

var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// ── Confirm ───────────────────────────────────────────────────────────────────

type confirmKind int

const (
	noConfirm confirmKind = iota
	confirmDeleteVM
	confirmDeleteContainer
)

// ── Model ─────────────────────────────────────────────────────────────────────

type tuiModel struct {
	activeTab int // 0 = Docker, 1 = VMs

	// Docker panel
	containers   []Container
	dockerCursor int

	// VM panel
	vms     []VM
	vmTable table.Model

	// Shared
	logs        []string
	width       int
	height      int
	busy        bool
	confirm     confirmKind
	confirmName string
	spinner     int
	lastRefresh time.Time
}

func newTUI() tuiModel {
	cols := []table.Column{
		{Title: "  NAME", Width: 20},
		{Title: "STATE", Width: 10},
		{Title: "IP", Width: 18},
		{Title: "WHONIX", Width: 8},
	}
	t := table.New(
		table.WithColumns(cols),
		table.WithFocused(true),
		table.WithHeight(6),
	)
	s := table.DefaultStyles()
	s.Header = s.Header.
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(colorMuted).
		BorderBottom(true).
		Foreground(colorMauve).
		Bold(true)
	s.Selected = s.Selected.
		Foreground(colorText).
		Background(colorOverlay).
		Bold(false)
	t.SetStyles(s)
	return tuiModel{vmTable: t}
}

func (m tuiModel) Init() tea.Cmd {
	return tea.Batch(refreshAll(), tickCmd())
}

// ── Update ────────────────────────────────────────────────────────────────────

func (m tuiModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		tableH := m.height - 17
		if tableH < 3 {
			tableH = 3
		}
		m.vmTable.SetHeight(tableH)
		nameW := m.width - 10 - 18 - 8 - 8
		if nameW < 16 {
			nameW = 16
		}
		cols := m.vmTable.Columns()
		cols[0].Width = nameW
		m.vmTable.SetColumns(cols)

	case tickMsg:
		m.spinner = (m.spinner + 1) % len(spinnerFrames)
		cmds = append(cmds, tickCmd(), refreshAll())

	case containersMsg:
		m.containers = []Container(msg)
		m.lastRefresh = time.Now()
		if m.dockerCursor >= len(m.containers) && len(m.containers) > 0 {
			m.dockerCursor = len(m.containers) - 1
		}

	case vmsMsg:
		m.vms = []VM(msg)
		m.lastRefresh = time.Now()
		rows := make([]table.Row, len(m.vms))
		for i, vm := range m.vms {
			ip := vm.IP
			if ip == "" {
				ip = "—"
			}
			whonix := styleMuted.Render("no")
			if vm.HasWhonix {
				whonix = styleOK.Render("yes")
			}
			state := vm.State
			if state == "shut off" {
				state = "stopped"
			}
			rows[i] = table.Row{
				"  " + vm.Name,
				icon(state) + " " + state,
				ip,
				whonix,
			}
		}
		m.vmTable.SetRows(rows)

	case logMsg:
		ts := time.Now().Format("15:04:05")
		var prefix string
		switch msg.level {
		case "ok":
			prefix = styleOK.Render("✓")
		case "warn":
			prefix = styleWarn.Render("⚠")
		case "err":
			prefix = styleError.Render("✗")
		default:
			prefix = styleStep.Render("→")
		}
		line := fmt.Sprintf("  %s  %s  %s", tuiLogTime.Render(ts), prefix, msg.text)
		m.logs = append(m.logs, line)
		if len(m.logs) > 200 {
			m.logs = m.logs[len(m.logs)-200:]
		}

	case doneMsg:
		m.busy = false
		level := "ok"
		if msg.err != nil {
			level = "err"
		}
		text := msg.text
		if msg.err != nil {
			text = fmt.Sprintf("%s: %v", msg.text, msg.err)
		}
		newM, newCmd := m.Update(logMsg{level: level, text: text})
		return newM, tea.Batch(newCmd, refreshAll())

	case tea.KeyMsg:
		if m.confirm != noConfirm {
			switch msg.String() {
			case "y", "Y":
				kind, name := m.confirm, m.confirmName
				m.confirm, m.confirmName = noConfirm, ""
				m.busy = true
				switch kind {
				case confirmDeleteVM:
					cmds = append(cmds,
						emit(logMsg{level: "warn", text: fmt.Sprintf("Deleting VM '%s'...", name)}),
						bg(func() (string, error) {
							return fmt.Sprintf("Deleted VM '%s'", name), DeleteVM(name)
						}))
				case confirmDeleteContainer:
					cmds = append(cmds,
						emit(logMsg{level: "warn", text: fmt.Sprintf("Removing container '%s'...", name)}),
						bg(func() (string, error) {
							return fmt.Sprintf("Removed '%s'", name), RemoveContainer(name)
						}))
				}
			default:
				m.confirm, m.confirmName = noConfirm, ""
				cmds = append(cmds, emit(logMsg{level: "info", text: "Cancelled"}))
			}
			break
		}

		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
		case "1":
			m.activeTab = 0
		case "2":
			m.activeTab = 1
		case "tab":
			m.activeTab = (m.activeTab + 1) % 2
		case "?":
			cmds = append(cmds, emit(logMsg{level: "info",
				text: "1/2/tab: switch panels  s start  S stop  c ssh/exec  C console  r rdp  R reset  D delete  w/W whonix  q quit"}))

		default:
			if m.activeTab == 0 {
				cmds = append(cmds, m.handleDockerKey(msg.String())...)
			} else {
				cmds = append(cmds, m.handleVMKey(msg.String())...)
			}
		}
	}

	var tableCmd tea.Cmd
	m.vmTable, tableCmd = m.vmTable.Update(msg)
	cmds = append(cmds, tableCmd)
	return m, tea.Batch(cmds...)
}

func (m *tuiModel) handleDockerKey(key string) []tea.Cmd {
	var cmds []tea.Cmd
	switch key {
	case "up", "k":
		if m.dockerCursor > 0 {
			m.dockerCursor--
		}
	case "down", "j":
		if m.dockerCursor < len(m.containers)-1 {
			m.dockerCursor++
		}
	case "c", "enter":
		if c := m.selectedContainer(); c != nil && c.Status == "running" {
			cmds = append(cmds,
				emit(logMsg{level: "info", text: fmt.Sprintf("exec → '%s'", c.Name)}))
			return append(cmds, execContainerCmd(c.Name))
		}
	case "S":
		if c := m.selectedContainer(); c != nil && !m.busy {
			m.busy = true
			name := c.Name
			cmds = append(cmds,
				emit(logMsg{level: "info", text: fmt.Sprintf("Stopping '%s'...", name)}),
				bg(func() (string, error) {
					return fmt.Sprintf("Stopped '%s'", name), StopContainer(name)
				}))
		}
	case "D":
		if c := m.selectedContainer(); c != nil && !m.busy {
			m.confirm = confirmDeleteContainer
			m.confirmName = c.Name
		}
	}
	return cmds
}

func (m *tuiModel) handleVMKey(key string) []tea.Cmd {
	var cmds []tea.Cmd
	switch key {
	case "up", "k":
		m.vmTable.MoveUp(1)
	case "down", "j":
		m.vmTable.MoveDown(1)
	case "s":
		if name := m.selectedVMName(); name != "" && !m.busy {
			m.busy = true
			cmds = append(cmds,
				emit(logMsg{level: "info", text: fmt.Sprintf("Starting '%s'...", name)}),
				bg(func() (string, error) {
					return fmt.Sprintf("Started '%s'", name), StartVM(name)
				}))
		}
	case "S":
		if name := m.selectedVMName(); name != "" && !m.busy {
			m.busy = true
			cmds = append(cmds,
				emit(logMsg{level: "info", text: fmt.Sprintf("Stopping '%s'...", name)}),
				bg(func() (string, error) {
					return fmt.Sprintf("Stopped '%s'", name), StopVM(name)
				}))
		}
	case "R":
		if name := m.selectedVMName(); name != "" && !m.busy {
			m.busy = true
			cmds = append(cmds,
				emit(logMsg{level: "info", text: fmt.Sprintf("Resetting '%s'...", name)}),
				bg(func() (string, error) {
					_ = ForceOffVM(name)
					if err := RevertSnapshot(name, snapshotName); err != nil {
						return "", err
					}
					return fmt.Sprintf("Reset '%s' — booting", name), StartVM(name)
				}))
		}
	case "D":
		if name := m.selectedVMName(); name != "" && !m.busy {
			m.confirm = confirmDeleteVM
			m.confirmName = name
		}
	case "c", "enter":
		if vm := m.selectedVM(); vm != nil && vm.State == "running" {
			cmds = append(cmds,
				emit(logMsg{level: "info", text: fmt.Sprintf("SSH → '%s' (%s)", vm.Name, vm.IP)}))
			return append(cmds, sshVMCmd(vm))
		}
	case "r":
		if vm := m.selectedVM(); vm != nil {
			cmds = append(cmds, rdpVMCmd(vm))
		}
	case "C":
		if name := m.selectedVMName(); name != "" {
			return append(cmds, consoleVMCmd(name))
		}
	case "w":
		if name := m.selectedVMName(); name != "" && !m.busy {
			if !NetworkExists("Whonix_internal") {
				cmds = append(cmds, emit(logMsg{level: "err",
					text: "Whonix_internal not found — run: pomdock vm whonix-gateway"}))
			} else {
				m.busy = true
				cmds = append(cmds,
					emit(logMsg{level: "info", text: fmt.Sprintf("Attaching Whonix to '%s'...", name)}),
					bg(func() (string, error) {
						if err := AttachWhonixNIC(name); err != nil {
							return "", err
						}
						return fmt.Sprintf("Whonix NIC attached to '%s'", name), nil
					}))
			}
		}
	case "W":
		if name := m.selectedVMName(); name != "" && !m.busy {
			m.busy = true
			cmds = append(cmds,
				emit(logMsg{level: "info", text: fmt.Sprintf("Detaching Whonix from '%s'...", name)}),
				bg(func() (string, error) {
					if err := DetachWhonixNIC(name); err != nil {
						return "", err
					}
					return fmt.Sprintf("Whonix NIC removed from '%s'", name), nil
				}))
		}
	}
	return cmds
}

// ── View ──────────────────────────────────────────────────────────────────────

func (m tuiModel) View() string {
	if m.width == 0 {
		return "  Loading..."
	}
	w := m.width
	sep := tuiSep.Render(strings.Repeat("─", w))

	// Header
	busyStr := ""
	if m.busy {
		busyStr = " " + tuiBusy.Render(spinnerFrames[m.spinner])
	}
	since := "—"
	if !m.lastRefresh.IsZero() {
		since = fmt.Sprintf("%ds ago", int(time.Since(m.lastRefresh).Seconds()))
	}
	title := styleAccent.Render("  pomdock") + busyStr
	right := styleMuted.Render("↻ " + since + "  ")
	pad := w - lipgloss.Width(title) - lipgloss.Width(right)
	if pad < 0 {
		pad = 0
	}
	header := title + strings.Repeat(" ", pad) + right

	// Tabs
	tab0, tab1 := tabInactive.Render("  Docker  "), tabInactive.Render("  VMs  ")
	if m.activeTab == 0 {
		tab0 = tabActive.Render("  Docker  ")
	} else {
		tab1 = tabActive.Render("  VMs  ")
	}
	tabs := "  " + tab0 + "  " + tab1

	// Panel content
	var panel string
	var helpLine string
	switch m.activeTab {
	case 0:
		panel = m.dockerView()
		helpLine = "  ↑↓/jk select  c exec  S stop  D delete  tab switch  ? help  q quit"
	case 1:
		panel = m.vmTable.View()
		helpLine = "  ↑↓/jk select  s start  S stop  c ssh  r rdp  C console  R reset  D delete  w whonix  W detach  tab switch  q quit"
	}

	// Confirm overlay
	confirmLine := ""
	if m.confirm != noConfirm {
		action := "Delete"
		confirmLine = "\n" + tuiConfirm.Render(
			fmt.Sprintf("  ⚠  %s '%s'?  [y] yes  [any] cancel", action, m.confirmName))
	}

	help := tuiHelp.Render(helpLine)

	// Logs (last 5 lines)
	logs := m.logs
	if len(logs) > 5 {
		logs = logs[len(logs)-5:]
	}
	logView := strings.Join(logs, "\n")
	if logView == "" {
		logView = styleMuted.Render("  No events yet")
	}

	return strings.Join([]string{
		header,
		sep,
		tabs,
		sep,
		panel,
		confirmLine,
		"",
		sep,
		help,
		sep,
		logView,
	}, "\n")
}

func (m tuiModel) dockerView() string {
	if len(m.containers) == 0 {
		return styleMuted.Render("  No pentest containers found.\n  Build one: pomdock docker build")
	}

	nameW := 24
	for _, c := range m.containers {
		if len(c.Name)+2 > nameW {
			nameW = len(c.Name) + 2
		}
	}

	var sb strings.Builder
	hdr := func(s string) string { return styleAccent.Render(s) }
	fmt.Fprintf(&sb, "  %s%-*s  %-18s  %-6s  %s\n",
		" ", nameW, hdr("NAME"), hdr("STATUS"), hdr("VPN"), hdr("TOR"))
	fmt.Fprintf(&sb, "  %s\n", styleMuted.Render(strings.Repeat("─", nameW+38)))

	for i, c := range m.containers {
		cursor := "  "
		if i == m.dockerCursor {
			cursor = styleAccent.Render("▶ ")
		}
		vpn := styleMuted.Render("no ")
		if c.HasVPN {
			vpn = styleOK.Render("yes")
		}
		tor := styleMuted.Render("no ")
		if c.HasTor {
			tor = styleOK.Render("yes")
		}
		fmt.Fprintf(&sb, "%s%s %-*s  %-28s  %-6s  %s\n",
			cursor,
			icon(c.Status),
			nameW-2, c.Name,
			stateColor(c.Status),
			vpn, tor,
		)
	}
	return sb.String()
}

// ── Selectors ─────────────────────────────────────────────────────────────────

func (m tuiModel) selectedContainer() *Container {
	if m.dockerCursor < 0 || m.dockerCursor >= len(m.containers) {
		return nil
	}
	return &m.containers[m.dockerCursor]
}

func (m tuiModel) selectedVMName() string {
	row := m.vmTable.SelectedRow()
	if len(row) == 0 {
		return ""
	}
	return strings.TrimSpace(row[0])
}

func (m tuiModel) selectedVM() *VM {
	name := m.selectedVMName()
	for i := range m.vms {
		if m.vms[i].Name == name {
			return &m.vms[i]
		}
	}
	return nil
}

// ── Tea Commands ──────────────────────────────────────────────────────────────

func tickCmd() tea.Cmd {
	return tea.Tick(3*time.Second, func(t time.Time) tea.Msg { return tickMsg(t) })
}

func refreshAll() tea.Cmd {
	return tea.Batch(
		func() tea.Msg {
			c, _ := ListContainers()
			return containersMsg(c)
		},
		func() tea.Msg {
			v, _ := ListVMs()
			return vmsMsg(v)
		},
	)
}

func emit(msg tea.Msg) tea.Cmd { return func() tea.Msg { return msg } }

func bg(fn func() (string, error)) tea.Cmd {
	return func() tea.Msg {
		text, err := fn()
		return doneMsg{text: text, err: err}
	}
}

func execContainerCmd(name string) tea.Cmd {
	c := exec.Command("docker", "exec", "-it", name, "bash", "-l")
	return tea.ExecProcess(c, func(err error) tea.Msg {
		return logMsg{level: "info", text: fmt.Sprintf("exec '%s' ended", name)}
	})
}

func sshVMCmd(vm *VM) tea.Cmd {
	if vm.IP == "" {
		return emit(logMsg{level: "err", text: fmt.Sprintf("'%s' has no IP", vm.Name)})
	}
	keyPath := filepath.Join(os.Getenv("HOME"), ".ssh", "kali")
	args := []string{}
	if _, err := os.Stat(keyPath); err == nil {
		args = append(args, "-i", keyPath)
	}
	args = append(args,
		"-o", "StrictHostKeyChecking=no",
		"-o", "UserKnownHostsFile=/dev/null",
		"kali@"+vm.IP)
	c := exec.Command("ssh", args...)
	return tea.ExecProcess(c, func(err error) tea.Msg {
		return logMsg{level: "info", text: fmt.Sprintf("SSH '%s' ended", vm.Name)}
	})
}

func consoleVMCmd(name string) tea.Cmd {
	if _, err := exec.LookPath("virt-viewer"); err == nil {
		return func() tea.Msg {
			exec.Command("virt-viewer", "--connect", libvirtURI, name).Start()
			return logMsg{level: "ok", text: fmt.Sprintf("virt-viewer launched for '%s'", name)}
		}
	}
	c := exec.Command("virsh", "--connect", libvirtURI, "console", name)
	return tea.ExecProcess(c, func(err error) tea.Msg {
		return logMsg{level: "info", text: fmt.Sprintf("console '%s' ended", name)}
	})
}

func rdpVMCmd(vm *VM) tea.Cmd {
	return func() tea.Msg {
		rdpBin := ""
		if _, err := exec.LookPath("xfreerdp3"); err == nil {
			rdpBin = "xfreerdp3"
		} else if _, err := exec.LookPath("xfreerdp"); err == nil {
			rdpBin = "xfreerdp"
		}
		if rdpBin == "" {
			return logMsg{level: "err", text: "xfreerdp3 not found — install: sudo apt install freerdp3-x11"}
		}
		if vm.IP == "" {
			return logMsg{level: "err", text: fmt.Sprintf("'%s' has no IP", vm.Name)}
		}
		exec.Command(rdpBin, "/v:"+vm.IP, "/u:kali",
			"/dynamic-resolution", "/gfx:avc444", "+clipboard", "/cert:tofu").Start()
		return logMsg{level: "ok", text: fmt.Sprintf("RDP '%s' launched", vm.Name)}
	}
}

func runTUI() error {
	m := newTUI()
	p := tea.NewProgram(m, tea.WithAltScreen())
	_, err := p.Run()
	return err
}
