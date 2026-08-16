package main

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

type shellsMsg struct {
	sessions []ShellSession
	err      error
}

type shellReadyMsg struct {
	container string
	window    string
	err       error
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
		}
	case "down", "j":
		if m.shellCursor < len(m.shells)-1 {
			m.shellCursor++
		}
	case "c", "enter":
		if shell := m.selectedShell(); shell != nil && !m.busy {
			return []tea.Cmd{selectShellWindowCmd(shell.ID, shell.Container)}
		}
	case "n":
		if container := m.selectedContainer(); container != nil && !m.busy {
			m.busy = true
			if container.Status != "running" {
				return []tea.Cmd{
					emit(logMsg{level: "info", text: fmt.Sprintf("Starting '%s'...", container.Name)}),
					startContainerCmd(container.Name, false, true),
				}
			}
			return []tea.Cmd{openPersistentShellCmd(container.Name)}
		}
	case "D":
		if shell := m.selectedShell(); shell != nil && !m.busy {
			m.confirm = confirmDeleteShell
			m.confirmName = shell.ID
			m.confirmLabel = shell.Container
		}
	}
	return nil
}

func (m tuiModel) shellsView() string {
	width := m.width
	nameWidth := width - 35
	if nameWidth < 16 {
		nameWidth = 16
	}
	header := "   " + padRight("CONTAINER", nameWidth) + "  " + padRight("STATE", 10) + "  WINDOW"
	lines := []string{styleAccent.Render(header), styleMuted.Render(strings.Repeat("─", width))}
	if len(m.shells) == 0 {
		lines = append(lines,
			styleMuted.Render("  No shell windows."),
			styleMuted.Render("  Select a Docker container and press C, or press n here."),
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
		container := padRight(ansi.Truncate(shell.Container, nameWidth, "…"), nameWidth)
		sessionWidth := width - nameWidth - 19
		if sessionWidth < 8 {
			sessionWidth = 8
		}
		session := ansi.Truncate(shell.Name, sessionWidth, "…")
		lines = append(lines, fmt.Sprintf("%s● %s  %s  %s", cursor, container, state, session))
	}
	return strings.Join(lines, "\n")
}

func openPersistentShellCmd(container string) tea.Cmd {
	return func() tea.Msg {
		window, err := EnsureContainerShell(container)
		return shellReadyMsg{container: container, window: window, err: err}
	}
}

func selectShellWindowCmd(window, container string) tea.Cmd {
	return func() tea.Msg {
		if err := SelectShellWindow(window); err != nil {
			return logMsg{level: "err", text: fmt.Sprintf("Could not open shell '%s': %v", container, err)}
		}
		return logMsg{level: "info", text: fmt.Sprintf("Switched to shell window '%s'", container)}
	}
}
