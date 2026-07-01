package main

import (
	"fmt"
	"os"

	"github.com/charmbracelet/lipgloss"
)

// Catppuccin Mocha
var (
	colorBlue    = lipgloss.Color("#89b4fa")
	colorGreen   = lipgloss.Color("#a6e3a1")
	colorYellow  = lipgloss.Color("#f9e2af")
	colorRed     = lipgloss.Color("#f38ba8")
	colorMauve   = lipgloss.Color("#cba6f7")
	colorTeal    = lipgloss.Color("#94e2d5")
	colorMuted   = lipgloss.Color("#6c7086")
	colorOverlay = lipgloss.Color("#313244")
	colorText    = lipgloss.Color("#cdd6f4")

	styleStep   = lipgloss.NewStyle().Foreground(colorBlue).Bold(true)
	styleOK     = lipgloss.NewStyle().Foreground(colorGreen).Bold(true)
	styleWarn   = lipgloss.NewStyle().Foreground(colorYellow).Bold(true)
	styleError  = lipgloss.NewStyle().Foreground(colorRed).Bold(true)
	styleMuted  = lipgloss.NewStyle().Foreground(colorMuted)
	styleAccent = lipgloss.NewStyle().Foreground(colorMauve).Bold(true)
	styleBold   = lipgloss.NewStyle().Bold(true)
)

func logStep(f string, a ...any) {
	fmt.Fprintf(os.Stderr, "%s %s\n", styleStep.Render("→"), fmt.Sprintf(f, a...))
}
func logOK(f string, a ...any) {
	fmt.Fprintf(os.Stderr, "%s %s\n", styleOK.Render("✓"), fmt.Sprintf(f, a...))
}
func logWarn(f string, a ...any) {
	fmt.Fprintf(os.Stderr, "%s %s\n", styleWarn.Render("⚠"), fmt.Sprintf(f, a...))
}
func logErr(f string, a ...any) {
	fmt.Fprintf(os.Stderr, "%s %s\n", styleError.Render("✗"), fmt.Sprintf(f, a...))
}

func stateColor(s string) string {
	switch s {
	case "running":
		return styleOK.Render("● " + s)
	case "stopped", "shut off", "exited":
		return styleMuted.Render("○ stopped")
	case "paused":
		return styleWarn.Render("◐ paused")
	default:
		return styleMuted.Render("? " + s)
	}
}

func icon(s string) string {
	switch s {
	case "running":
		return styleOK.Render("●")
	case "stopped", "shut off", "exited":
		return styleMuted.Render("○")
	case "paused":
		return styleWarn.Render("◐")
	default:
		return styleMuted.Render("?")
	}
}
