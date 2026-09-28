package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// Colors follow the active PCM desktop theme (`pcm theme set`, which writes
// $XDG_STATE_HOME/pcm/theme/palette.env). Without that file, e.g. on a machine
// without pcm.dot, they fall back to Catppuccin Mocha. Read once at startup.
var pcmPalette = loadPCMPalette()

var (
	colorBlue    = themeColor("PCM_BLUE", "#89b4fa")
	colorGreen   = themeColor("PCM_GREEN", "#a6e3a1")
	colorYellow  = themeColor("PCM_WARNING", "#f9e2af")
	colorRed     = themeColor("PCM_URGENT", "#f38ba8")
	colorMauve   = themeColor("PCM_ACCENT", "#cba6f7")
	colorMuted   = themeColor("PCM_DIM", "#6c7086")
	colorOverlay = themeColor("PCM_SELECTION", "#313244")

	styleStep   = lipgloss.NewStyle().Foreground(colorBlue).Bold(true)
	styleOK     = lipgloss.NewStyle().Foreground(colorGreen).Bold(true)
	styleWarn   = lipgloss.NewStyle().Foreground(colorYellow).Bold(true)
	styleError  = lipgloss.NewStyle().Foreground(colorRed).Bold(true)
	styleMuted  = lipgloss.NewStyle().Foreground(colorMuted)
	styleAccent = lipgloss.NewStyle().Foreground(colorMauve).Bold(true)
	styleBold   = lipgloss.NewStyle().Bold(true)
)

func loadPCMPalette() map[string]string {
	state := os.Getenv("XDG_STATE_HOME")
	if state == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil
		}
		state = filepath.Join(home, ".local", "state")
	}
	data, err := os.ReadFile(filepath.Join(state, "pcm", "theme", "palette.env"))
	if err != nil {
		return nil
	}
	return parsePalette(string(data))
}

// parsePalette keeps only KEY=#rrggbb lines; other palette.env keys are not colors.
func parsePalette(data string) map[string]string {
	palette := map[string]string{}
	for _, line := range strings.Split(data, "\n") {
		key, value, ok := strings.Cut(strings.TrimSpace(line), "=")
		if ok && len(value) == 7 && strings.HasPrefix(value, "#") {
			palette[key] = value
		}
	}
	return palette
}

func themeColor(key, fallback string) lipgloss.Color {
	if value, ok := pcmPalette[key]; ok {
		return lipgloss.Color(value)
	}
	return lipgloss.Color(fallback)
}

func logStep(f string, a ...any) {
	fmt.Fprintf(os.Stderr, "%s %s\n", styleStep.Render("→"), fmt.Sprintf(f, a...))
}
func logOK(f string, a ...any) {
	fmt.Fprintf(os.Stderr, "%s %s\n", styleOK.Render("✓"), fmt.Sprintf(f, a...))
}
func logWarn(f string, a ...any) {
	fmt.Fprintf(os.Stderr, "%s %s\n", styleWarn.Render("⚠"), fmt.Sprintf(f, a...))
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
