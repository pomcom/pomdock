package main

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

type shellsMsg struct {
	sessions []ShellSession
	warnings []string
	err      error
}

type shellReadyMsg struct {
	kind   string
	target string
	window string
	err    error
}

type shellDetachedMsg struct {
	target   string
	switched bool // client was switched to the workspace session (TUI still inside tmux)
	err      error
}

func (m tuiModel) selectedShell() *ShellSession {
	if m.shellCursor < 0 || m.shellCursor >= len(m.shells) {
		return nil
	}
	return &m.shells[m.shellCursor]
}

func (m *tuiModel) handleShellKey(key string) []tea.Cmd {
	switch key {
	case "up", "k":
		if m.shellCursor > 0 {
			m.shellCursor--
			return []tea.Cmd{tea.ClearScreen}
		}
	case "down", "j":
		if m.shellCursor < len(m.shells)-1 {
			m.shellCursor++
			return []tea.Cmd{tea.ClearScreen}
		}
	case "c", "enter":
		if shell := m.selectedShell(); shell != nil && !m.busy {
			return []tea.Cmd{attachShellWindowCmd(shell.ID, shell.Target)}
		}
	case "n":
		if container := m.selectedContainer(); container != nil && !m.busy {
			m.busy = true
			if container.Status != "running" {
				return []tea.Cmd{
					emit(logMsg{level: "info", text: fmt.Sprintf("Starting '%s'...", container.Name)}),
					startContainerCmd(container.Name, true),
				}
			}
			return []tea.Cmd{openPersistentShellCmd(container.Name)}
		}
	case "D":
		if shell := m.selectedShell(); shell != nil && !m.busy {
			m.confirm = confirmDeleteShell
			m.confirmName = shell.ID
			m.confirmLabel = shell.Target
		}
	}
	return nil
}

func (m tuiModel) shellsView() string {
	width := m.width
	nameWidth := width - 43
	if nameWidth < 12 {
		nameWidth = 12
	}
	header := "   " + padRight("TARGET", nameWidth) + "  " + padRight("TYPE", 8) + "  " + padRight("STATE", 10) + "  WINDOW"
	lines := []string{styleAccent.Render(header), styleMuted.Render(strings.Repeat("─", width))}
	for _, warning := range m.shellWarnings {
		lines = append(lines, styleWarn.Render(ansi.Truncate("  Discovery incomplete: "+warning, width, "…")))
	}
	if len(m.shells) == 0 {
		if len(m.shellWarnings) > 0 {
			return strings.Join(lines, "\n")
		}
		lines = append(lines,
			styleMuted.Render("  No shell windows."),
			styleMuted.Render("  Open a Docker shell with C or connect to a VM with c."),
		)
		return strings.Join(lines, "\n")
	}
	for i, shell := range m.shells {
		cursor := "  "
		if i == m.shellCursor {
			cursor = styleAccent.Render("▶ ")
		}
		state := styleOK.Render(padRight("ready", 10))
		if shell.Active {
			state = styleWarn.Render(padRight("active", 10))
		}
		target := padRight(ansi.Truncate(shell.Target, nameWidth, "…"), nameWidth)
		kindLabel := shell.Kind
		if shell.Location == "host" && shell.Kind == "docker" {
			kindLabel = "host"
		}
		if shell.Location == "container" {
			state = styleMuted.Render(padRight("detached", 10))
			if shell.Clients > 0 {
				state = styleOK.Render(padRight("attached", 10))
			}
		}
		kind := padRight(kindLabel, 8)
		sessionWidth := width - nameWidth - 28
		if sessionWidth < 8 {
			sessionWidth = 8
		}
		session := ansi.Truncate(shell.Session+":"+shell.Name, sessionWidth, "…")
		lines = append(lines, fmt.Sprintf("%s● %s  %s  %s  %s", cursor, target, kind, state, session))
	}
	return strings.Join(lines, "\n")
}

func openPersistentShellCmd(container string) tea.Cmd {
	return func() tea.Msg {
		window, err := EnsureContainerShell(container)
		return shellReadyMsg{kind: "docker", target: container, window: window, err: err}
	}
}

func openVMShellCmd(vm VM) tea.Cmd {
	return func() tea.Msg {
		window, err := EnsureVMShell(vm)
		return shellReadyMsg{kind: "vm", target: vm.Name, window: window, err: err}
	}
}

// Resolve the owning backend before handing over the terminal. Container tmux
// attaches as a child client; only legacy host windows switch an existing client.
func attachShellWindowCmd(window, target string) tea.Cmd {
	return func() tea.Msg {
		process, err := attachShellWindowCommand(window)
		if err != nil {
			return shellDetachedMsg{target: target, err: err}
		}
		return tea.ExecProcess(process, func(err error) tea.Msg {
			return shellDetachedMsg{target: target, switched: !containerShellIDRe.MatchString(window) && insideTmux(), err: err}
		})()
	}
}
