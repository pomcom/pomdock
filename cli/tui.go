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
	"github.com/charmbracelet/x/ansi"
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
type containerStartedMsg struct {
	name      string
	err       error
	openShell bool
}
type containerCreatedMsg struct {
	name string
	err  error
}
type containerCommandMsg struct {
	name    string
	command string
	output  string
	cwd     string
	err     error
}
type containerTransferMsg struct {
	name        string
	direction   transferDirection
	source      string
	destination string
	err         error
}

// ── Tab styles ────────────────────────────────────────────────────────────────

var (
	tabActive = lipgloss.NewStyle().
			Foreground(colorMauve).
			Background(colorOverlay).
			Bold(true).
			Padding(0, 2)

	tabInactive = lipgloss.NewStyle().
			Foreground(colorMuted).
			Padding(0, 2)

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
	confirmDeleteShell
)

// ── Model ─────────────────────────────────────────────────────────────────────

type tuiModel struct {
	activeTab int // 0 = Docker, 1 = VMs, 2 = Shells

	// Docker panel
	containers    []Container
	dockerCursor  int
	createForm    containerCreateForm
	transferForm  containerTransferForm
	console       containerConsole
	pendingSelect string

	// VM panel
	vms          []VM
	vmTable      table.Model
	vmCreateForm vmCreateForm
	pendingVM    string

	// Persistent tmux-backed container shells
	shells      []ShellSession
	shellCursor int

	// Shared
	logs         []string
	width        int
	height       int
	busy         bool
	confirm      confirmKind
	confirmName  string
	confirmLabel string
	spinner      int
	lastRefresh  time.Time
}

func newTUI() tuiModel {
	t := table.New(
		table.WithColumns(vmTableColumns(80)),
		table.WithFocused(true),
		table.WithHeight(6),
		table.WithWidth(80),
	)
	// The VM tab owns "f" for finalizing Windows installs.
	t.KeyMap.PageDown.SetKeys("pgdown", " ")
	s := table.DefaultStyles()
	// DefaultStyles() gives Header/Cell 1 column of padding on each side.
	// vmTableColumns() sizes columns to the exact rendered width with no
	// slack for that, so every cell ends up 2 columns wider than its
	// column.Width — strip it so the layout math is accurate.
	s.Header = s.Header.
		Padding(0, 0).
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(colorMuted).
		BorderBottom(true).
		Foreground(colorMauve).
		Bold(true)
	s.Cell = s.Cell.Padding(0, 0)
	s.Selected = s.Selected.
		Foreground(colorMauve).
		Bold(true)
	t.SetStyles(s)
	return tuiModel{
		vmTable: t,
		console: containerConsole{
			output: make(map[string][]string),
		},
		createForm:   newContainerCreateForm(),
		vmCreateForm: newVMCreateForm(),
	}
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
		m.vmTable.SetWidth(m.width)
		m.vmTable.SetRows(nil)
		m.vmTable.SetColumns(vmTableColumns(m.width))
		m.setVMRows()

	case tickMsg:
		m.spinner = (m.spinner + 1) % len(spinnerFrames)
		cmds = append(cmds, tickCmd(), refreshAll())

	case containersMsg:
		selectedName := ""
		if selected := m.selectedContainer(); selected != nil {
			selectedName = selected.Name
		}
		m.containers = []Container(msg)
		m.lastRefresh = time.Now()
		if m.pendingSelect != "" {
			for i, container := range m.containers {
				if container.Name == m.pendingSelect {
					m.dockerCursor = i
					m.pendingSelect = ""
					break
				}
			}
		} else if selectedName != "" {
			for i, container := range m.containers {
				if container.Name == selectedName {
					m.dockerCursor = i
					break
				}
			}
		}
		if m.dockerCursor >= len(m.containers) && len(m.containers) > 0 {
			m.dockerCursor = len(m.containers) - 1
		}

	case vmsMsg:
		selectedName := m.selectedVMName()
		m.vms = []VM(msg)
		m.lastRefresh = time.Now()
		m.setVMRows()
		if m.pendingVM != "" {
			for i, vm := range m.vms {
				if vm.Name == m.pendingVM {
					m.vmTable.SetCursor(i)
					m.pendingVM = ""
					break
				}
			}
		} else if selectedName != "" {
			for i, vm := range m.vms {
				if vm.Name == selectedName {
					m.vmTable.SetCursor(i)
					break
				}
			}
		}

	case shellsMsg:
		selectedName := ""
		if selected := m.selectedShell(); selected != nil {
			selectedName = selected.Name
		}
		if msg.err != nil {
			cmds = append(cmds, emit(logMsg{level: "err", text: msg.err.Error()}))
			break
		}
		m.shells = msg.sessions
		if selectedName != "" {
			for i, shell := range m.shells {
				if shell.Name == selectedName {
					m.shellCursor = i
					break
				}
			}
		}
		if m.shellCursor >= len(m.shells) && len(m.shells) > 0 {
			m.shellCursor = len(m.shells) - 1
		}

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
		line := formatLogLine(m.width, ts, prefix, msg.text)
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

	case containerStartedMsg:
		m.busy = false
		if msg.err != nil {
			cmds = append(cmds, emit(logMsg{level: "err", text: fmt.Sprintf("Could not start '%s': %v", msg.name, msg.err)}))
			break
		}
		cmds = append(cmds, emit(logMsg{level: "ok", text: fmt.Sprintf("Started '%s'", msg.name)}))
		if msg.openShell {
			m.busy = true
			cmds = append(cmds, openPersistentShellCmd(msg.name))
		}

	case containerCreatedMsg:
		m.busy = false
		if msg.err != nil {
			m.createForm.active = true
			m.createForm.err = msg.err.Error()
			cmds = append(cmds,
				emit(logMsg{level: "err", text: fmt.Sprintf("Could not create '%s': %v", msg.name, msg.err)}),
				m.focusCreateField(),
			)
			break
		}
		m.pendingSelect = msg.name
		m.createForm = newContainerCreateForm()
		cmds = append(cmds,
			emit(logMsg{level: "ok", text: fmt.Sprintf("Created '%s'", msg.name)}),
			refreshAll(),
		)

	case vmCreatedMsg:
		m.busy = false
		if msg.err != nil {
			cmds = append(cmds, emit(logMsg{level: "err", text: fmt.Sprintf("Could not create VM '%s': %v", msg.name, msg.err)}))
			break
		}
		m.pendingVM = msg.name
		cmds = append(cmds,
			emit(logMsg{level: "ok", text: fmt.Sprintf("Created VM '%s'", msg.name)}),
			refreshAll(),
		)
		if msg.warning != "" {
			cmds = append(cmds, emit(logMsg{level: "warn", text: msg.warning}))
		}
		if msg.openConsole {
			cmds = append(cmds, consoleVMCmd(msg.name))
		}

	case vmProvisionStartedMsg:
		m.busy = false
		if msg.err != nil {
			m.vmCreateForm.active = true
			m.vmCreateForm.err = msg.err.Error()
			cmds = append(cmds,
				emit(logMsg{level: "err", text: fmt.Sprintf("Could not start VM setup for '%s': %v", msg.name, msg.err)}),
				m.focusVMCreateField(),
			)
			break
		}
		m.vmCreateForm = newVMCreateForm()
		cmds = append(cmds, emit(logMsg{level: "ok", text: fmt.Sprintf(
			"Provisioning %s '%s' in tmux window %s; Ctrl+B 0 returns here",
			msg.profile.Label, msg.name, msg.window)}))

	case containerCommandMsg:
		m.busy = false
		m.appendConsoleResult(msg)

	case containerTransferMsg:
		m.busy = false
		if msg.err != nil {
			m.transferForm.active = true
			m.transferForm.err = msg.err.Error()
			cmds = append(cmds,
				emit(logMsg{level: "err", text: fmt.Sprintf("File transfer failed: %v", msg.err)}),
				m.focusTransferField(),
			)
			break
		}
		m.transferForm = containerTransferForm{}
		verb := "Uploaded"
		if msg.direction == transferDownload {
			verb = "Downloaded"
		}
		cmds = append(cmds, emit(logMsg{level: "ok", text: fmt.Sprintf(
			"%s %s to %s", verb, msg.source, msg.destination)}))

	case shellReadyMsg:
		m.busy = false
		if msg.err != nil {
			cmds = append(cmds, emit(logMsg{level: "err", text: fmt.Sprintf("Could not open shell for '%s': %v", msg.target, msg.err)}))
			break
		}
		cmds = append(cmds,
			emit(logMsg{level: "ok", text: fmt.Sprintf("%s shell window ready for '%s'", msg.kind, msg.target)}),
			selectShellWindowCmd(msg.window, msg.target),
			refreshAll(),
		)

	case tea.KeyMsg:
		if m.createForm.active {
			cmds = append(cmds, m.updateContainerCreateForm(msg)...)
			return m, tea.Batch(cmds...)
		}
		if m.vmCreateForm.active {
			cmds = append(cmds, m.updateVMCreateForm(msg)...)
			return m, tea.Batch(cmds...)
		}
		if m.transferForm.active {
			cmds = append(cmds, m.updateContainerTransferForm(msg)...)
			return m, tea.Batch(cmds...)
		}
		if m.confirm != noConfirm {
			switch msg.String() {
			case "y", "Y":
				kind, name := m.confirm, m.confirmName
				m.confirm, m.confirmName, m.confirmLabel = noConfirm, "", ""
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
						emit(logMsg{level: "warn", text: fmt.Sprintf("Removing container stack '%s'...", name)}),
						bg(func() (string, error) {
							return fmt.Sprintf("Removed '%s' and its sidecars; loot kept", name), RemoveContainerStack(name)
						}))
				case confirmDeleteShell:
					cmds = append(cmds,
						emit(logMsg{level: "warn", text: fmt.Sprintf("Closing shell window '%s'...", name)}),
						bg(func() (string, error) {
							return fmt.Sprintf("Closed shell window '%s'", name), KillShellSession(name)
						}))
				}
			default:
				m.confirm, m.confirmName, m.confirmLabel = noConfirm, "", ""
				cmds = append(cmds, emit(logMsg{level: "info", text: "Cancelled"}))
			}
			break
		}

		switch msg.String() {
		case "q", "ctrl+c":
			return m, quitWorkspaceCmd()
		case "1":
			if m.activeTab != 0 {
				m.activeTab = 0
				cmds = append(cmds, tea.ClearScreen)
			}
		case "2":
			if m.activeTab != 1 {
				m.activeTab = 1
				cmds = append(cmds, tea.ClearScreen)
			}
		case "3":
			if m.activeTab != 2 {
				m.activeTab = 2
				cmds = append(cmds, tea.ClearScreen)
			}
		case "tab":
			m.activeTab = (m.activeTab + 1) % 3
			cmds = append(cmds, tea.ClearScreen)
		case "?":
			cmds = append(cmds, emit(logMsg{level: "info",
				text: "Keys: n new engagement; Docker i identity, p ports, t Tor, u/d copy, C shell; VM c SSH, r RDP, C console; s/S start/stop; D delete; q quit"}))

		default:
			switch m.activeTab {
			case 0:
				cmds = append(cmds, m.handleDockerKey(msg.String())...)
			case 1:
				cmds = append(cmds, m.handleVMKey(msg.String())...)
			case 2:
				cmds = append(cmds, m.handleShellKey(msg.String())...)
			}
		}
	}

	if _, isKey := msg.(tea.KeyMsg); !isKey {
		if m.createForm.active {
			cmds = append(cmds, m.updateCreateInput(msg))
		}
		if m.vmCreateForm.active {
			var inputCmd tea.Cmd
			m.vmCreateForm.name, inputCmd = m.vmCreateForm.name.Update(msg)
			cmds = append(cmds, inputCmd)
		}
		if m.transferForm.active {
			cmds = append(cmds, m.updateTransferInput(msg))
		}
	}

	var tableCmd tea.Cmd
	m.vmTable, tableCmd = m.vmTable.Update(msg)
	cmds = append(cmds, tableCmd)
	return m, tea.Batch(cmds...)
}

func (m *tuiModel) setVMRows() {
	rows := make([]table.Row, len(m.vms))
	width := m.width
	if width == 0 {
		width = 80
	}
	for i, vm := range m.vms {
		profile := GuestProfileByID(vm.ProfileID)
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
		rows[i] = table.Row{"  " + vm.Name, profile.Label, icon(state) + " " + state}
		if width >= 64 {
			rows[i] = append(rows[i], ip)
		}
		if width >= 96 {
			rows[i] = append(rows[i], whonix)
		}
	}
	m.vmTable.SetRows(rows)
	if len(rows) > 0 && m.vmTable.Cursor() < 0 {
		m.vmTable.SetCursor(0)
	}
}

func vmTableColumns(width int) []table.Column {
	if width < 64 {
		nameWidth := width - 30 // profile + state + two cells of padding per column
		if nameWidth < 12 {
			nameWidth = 12
		}
		return []table.Column{
			{Title: "  NAME", Width: nameWidth},
			{Title: "PROFILE", Width: 14},
			{Title: "STATE", Width: 10},
		}
	}
	if width < 96 {
		nameWidth := width - 49
		return []table.Column{
			{Title: "  NAME", Width: nameWidth},
			{Title: "PROFILE", Width: 16},
			{Title: "STATE", Width: 10},
			{Title: "IP", Width: 15},
		}
	}
	return []table.Column{
		{Title: "  NAME", Width: width - 64},
		{Title: "PROFILE", Width: 18},
		{Title: "STATE", Width: 10},
		{Title: "IP", Width: 18},
		{Title: "WHONIX", Width: 8},
	}
}

func quitWorkspaceCmd() tea.Cmd {
	return func() tea.Msg {
		_ = exec.Command("tmux", "detach-client").Run()
		return tea.Quit()
	}
}

func (m *tuiModel) handleDockerKey(key string) []tea.Cmd {
	var cmds []tea.Cmd
	switch key {
	case "n":
		if !m.busy {
			cmds = append(cmds, m.openContainerCreateForm())
		}
	case "up", "k":
		if m.dockerCursor > 0 {
			m.dockerCursor--
			cmds = append(cmds, tea.ClearScreen)
		}
	case "down", "j":
		if m.dockerCursor < len(m.containers)-1 {
			m.dockerCursor++
			cmds = append(cmds, tea.ClearScreen)
		}
	case "c", "enter", "i":
		if c := m.selectedContainer(); c != nil && !m.busy {
			if c.Status != "running" {
				return append(cmds, emit(logMsg{level: "warn", text: fmt.Sprintf(
					"Start '%s' before running engagement checks", c.Name)}))
			}
			m.busy = true
			return append(cmds, runContainerToolCmd(c.Name, "identity"))
		}
	case "p", "t":
		if c := m.selectedContainer(); c != nil && c.Status == "running" && !m.busy {
			tool := "ports"
			if key == "t" {
				tool = "tor"
			}
			m.busy = true
			return append(cmds, runContainerToolCmd(c.Name, tool))
		}
	case "u", "d":
		if c := m.selectedContainer(); c != nil && c.Status == "running" && !m.busy {
			direction := transferUpload
			if key == "d" {
				direction = transferDownload
			}
			return append(cmds, m.openContainerTransferForm(c.Name, direction))
		}
	case "C":
		if c := m.selectedContainer(); c != nil && !m.busy {
			if c.Legacy {
				return append(cmds, emit(logMsg{level: "warn", text: fmt.Sprintf(
					"'%s' predates contextual shells; stop and start it once to upgrade (loot is kept)", c.Name)}))
			}
			if c.Status != "running" {
				m.busy = true
				cmds = append(cmds,
					emit(logMsg{level: "info", text: fmt.Sprintf("Starting '%s'...", c.Name)}),
					startContainerCmd(c.Name, true),
				)
				break
			}
			m.busy = true
			cmds = append(cmds, emit(logMsg{level: "info", text: fmt.Sprintf("Opening shell window for '%s'...", c.Name)}))
			return append(cmds, openPersistentShellCmd(c.Name))
		}
	case "s":
		if c := m.selectedContainer(); c != nil && c.Status != "running" && !m.busy {
			m.busy = true
			name := c.Name
			cmds = append(cmds,
				emit(logMsg{level: "info", text: fmt.Sprintf("Starting '%s'...", name)}),
				bg(func() (string, error) {
					return fmt.Sprintf("Started '%s'", name), StartContainer(name)
				}))
		}
	case "S":
		if c := m.selectedContainer(); c != nil && c.Status == "running" && !m.busy {
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
	case "n":
		if !m.busy {
			cmds = append(cmds, m.openVMCreateForm())
		}
	case "up", "k":
		if m.vmTable.Cursor() > 0 {
			cmds = append(cmds, tea.ClearScreen)
		}
	case "down", "j":
		if m.vmTable.Cursor() < len(m.vms)-1 {
			cmds = append(cmds, tea.ClearScreen)
		}
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
		if vm := m.selectedVM(); vm != nil && !m.busy {
			m.busy = true
			name := vm.Name
			profile := GuestProfileByID(vm.ProfileID)
			cmds = append(cmds,
				emit(logMsg{level: "info", text: fmt.Sprintf("Resetting '%s'...", name)}),
				bg(func() (string, error) {
					_ = ForceOffVM(name)
					if err := RevertSnapshot(name, profile.Snapshot); err != nil {
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
			profile := GuestProfileByID(vm.ProfileID)
			if !profile.SupportsSSH {
				return append(cmds, emit(logMsg{level: "warn", text: fmt.Sprintf("%s uses RDP or console, not SSH", profile.Label)}))
			}
			m.busy = true
			cmds = append(cmds,
				emit(logMsg{level: "info", text: fmt.Sprintf("Opening VM shell for %s@%s...", profile.SSHUser, vm.IP)}))
			return append(cmds, openVMShellCmd(*vm))
		}
	case "r":
		if vm := m.selectedVM(); vm != nil && !m.busy {
			if vm.ProfileID == "" || vm.ProfileID == fallbackGuestProfileID {
				return append(cmds, emit(logMsg{level: "warn", text: fmt.Sprintf(
					"Assign a VM profile before using RDP: pomdock vm profile %s <profile>", vm.Name)}))
			}
			m.busy = true
			name := vm.Name
			cmds = append(cmds,
				emit(logMsg{level: "info", text: fmt.Sprintf("Starting '%s' if needed and opening RDP...", name)}),
				bg(func() (string, error) {
					cmd, ip, err := PrepareVMRDP(name, 90*time.Second)
					if err != nil {
						return "RDP failed", err
					}
					if isFreeRDPCommand(cmd) {
						window, err := startVMRDPWindow(name, cmd)
						if err != nil {
							return "RDP failed", err
						}
						return fmt.Sprintf("RDP '%s' opened at %s via %s in tmux window %s",
							name, ip, filepath.Base(cmd.Path), window), nil
					}
					if err := launchDesktopClient(cmd, 1500*time.Millisecond); err != nil {
						return "RDP failed", err
					}
					return fmt.Sprintf("RDP '%s' opened at %s via %s", name, ip, filepath.Base(cmd.Path)), nil
				}),
			)
		}
	case "C":
		if name := m.selectedVMName(); name != "" {
			return append(cmds, consoleVMCmd(name))
		}
	case "f":
		if vm := m.selectedVM(); vm != nil && !m.busy {
			profile := GuestProfileByID(vm.ProfileID)
			if profile.Family != "windows" {
				return append(cmds, emit(logMsg{level: "warn", text: "Finalize only applies to Windows ISO installs"}))
			}
			m.busy = true
			name := vm.Name
			cmds = append(cmds,
				emit(logMsg{level: "info", text: fmt.Sprintf("Finalizing Windows install '%s'...", name)}),
				bg(func() (string, error) {
					if err := FinalizeWindowsVM(name, profile.Snapshot); err != nil {
						return "", err
					}
					return fmt.Sprintf("Finalized '%s' and created snapshot '%s'", name, profile.Snapshot), nil
				}),
			)
		}
	case "w":
		if vm := m.selectedVM(); vm != nil && !m.busy {
			name := vm.Name
			profile := GuestProfileByID(vm.ProfileID)
			if !profile.SupportsWhonix {
				return append(cmds, emit(logMsg{level: "warn", text: fmt.Sprintf("Whonix routing is unavailable for %s", profile.Label)}))
			}
			if !NetworkExists(whonixNetwork) {
				cmds = append(cmds, emit(logMsg{level: "err",
					text: fmt.Sprintf("%s not found — run: pomdock vm whonix-gateway", whonixNetwork)}))
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
		if vm := m.selectedVM(); vm != nil && !m.busy {
			name := vm.Name
			profile := GuestProfileByID(vm.ProfileID)
			if !profile.SupportsWhonix {
				return append(cmds, emit(logMsg{level: "warn", text: fmt.Sprintf("Whonix routing is unavailable for %s", profile.Label)}))
			}
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
	tab0 := tabInactive.Render("  Docker  ")
	tab1 := tabInactive.Render("  VMs  ")
	tab2 := tabInactive.Render("  Shells  ")
	switch m.activeTab {
	case 0:
		tab0 = tabActive.Render("  Docker  ")
	case 1:
		tab1 = tabActive.Render("  VMs  ")
	case 2:
		tab2 = tabActive.Render("  Shells  ")
	}
	tabs := "  " + lipgloss.JoinHorizontal(lipgloss.Top, tab0, "  ", tab1, "  ", tab2)

	// Panel content
	var panel string
	var helpLine string
	switch m.activeTab {
	case 0:
		panel = m.dockerView()
		helpLine = helpLineForTab(0, w)
	case 1:
		if m.vmCreateForm.active {
			panel = m.vmCreateFormView()
		} else {
			panel = m.vmTable.View()
		}
		helpLine = helpLineForTab(1, w)
	case 2:
		panel = m.shellsView()
		helpLine = helpLineForTab(2, w)
	}
	panelHeight := m.height - 14
	if panelHeight < 3 {
		panelHeight = 3
	}
	panel = fitPanel(panel, w, panelHeight)

	// Confirm overlay
	confirmLine := ""
	if m.confirm != noConfirm {
		action := "Delete"
		if m.confirm == confirmDeleteContainer {
			action = "Delete stack (loot kept)"
		} else if m.confirm == confirmDeleteShell {
			action = "Close shell"
		}
		label := m.confirmName
		if m.confirmLabel != "" {
			label = m.confirmLabel
		}
		confirmLine = "\n" + tuiConfirm.Render(
			fmt.Sprintf("  ⚠  %s '%s'?  [y] yes  [any] cancel", action, label))
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

func fitPanel(view string, width, height int) string {
	lines := strings.Split(view, "\n")
	if len(lines) > height {
		lines = lines[:height]
	}
	for len(lines) < height {
		lines = append(lines, "")
	}
	for i, line := range lines {
		if lipgloss.Width(line) > width {
			line = ansi.Truncate(line, width, "")
		}
		padding := width - lipgloss.Width(line)
		if padding > 0 {
			line += strings.Repeat(" ", padding)
		}
		lines[i] = line
	}
	return strings.Join(lines, "\n")
}

func (m tuiModel) dockerView() string {
	if m.createForm.active {
		return m.containerCreateFormView()
	}
	if m.transferForm.active {
		return m.containerTransferFormView()
	}
	return m.dockerWorkspaceView()
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
		func() tea.Msg {
			shells, err := ListShellSessions()
			return shellsMsg{sessions: shells, err: err}
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

func startContainerCmd(name string, openShell bool) tea.Cmd {
	return func() tea.Msg {
		return containerStartedMsg{
			name:      name,
			err:       StartContainer(name),
			openShell: openShell,
		}
	}
}

func helpLineForTab(tab, width int) string {
	if width < 72 {
		return "  ↑↓ select  tab switch  ? help  q quit"
	}
	if tab == 0 {
		if width < 88 {
			return "  ↑↓ select  n new  i info  C shell  s/S power  ? help  q quit"
		}
		if width < 112 {
			return "  ↑↓ select  n new  i info  p ports  t tor  C shell  s/S power  tab switch  q quit"
		}
		return "  ↑↓ select  n new  i info  p ports  t tor  u/d copy  C shell  s/S power  D delete  tab switch  q quit"
	}
	if tab == 2 {
		if width < 100 {
			return "  ↑↓ select  enter switch  n new  D close  tab switch  q quit"
		}
		return "  ↑↓ select  enter switch  n selected container  D close  Ctrl-b 0 dashboard  q quit"
	}
	if width < 112 {
		return "  ↑↓ select  n new  s/S power  c ssh  r rdp  tab switch  ? help  q quit"
	}
	return "  ↑↓ select  n new  s/S power  c ssh  r rdp  C console  f finish  R reset  D delete  w/W whonix  tab switch  q quit"
}

func formatLogLine(width int, timestamp, prefix, message string) string {
	lead := fmt.Sprintf("  %s  %s  ", tuiLogTime.Render(timestamp), prefix)
	if width <= 0 {
		width = 80
	}
	available := width - lipgloss.Width(lead)
	if available < 20 {
		available = 20
	}
	lines := wrapWords(message, available)
	indent := strings.Repeat(" ", lipgloss.Width(lead))
	return lead + strings.Join(lines, "\n"+indent)
}

func wrapWords(text string, width int) []string {
	words := strings.Fields(text)
	if len(words) == 0 {
		return []string{""}
	}
	lines := []string{words[0]}
	for _, word := range words[1:] {
		last := len(lines) - 1
		if lipgloss.Width(lines[last])+1+lipgloss.Width(word) <= width {
			lines[last] += " " + word
		} else {
			lines = append(lines, word)
		}
	}
	return lines
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

func runTUI() error {
	if os.Getenv(workspaceEnv) != "1" {
		return enterPomdockWorkspace()
	}
	m := newTUI()
	p := tea.NewProgram(m, tea.WithAltScreen())
	_, err := p.Run()
	return err
}
