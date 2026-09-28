package main

import "testing"

func TestParsePalette(t *testing.T) {
	got := parsePalette("PCM_THEME=jade\nPCM_THEME_MODE=dark\nPCM_ACCENT=#6ee7a0\n  PCM_BLUE=#88c0d0  \nPCM_BAD=#12\n\n")
	want := map[string]string{"PCM_ACCENT": "#6ee7a0", "PCM_BLUE": "#88c0d0"}
	if len(got) != len(want) {
		t.Fatalf("parsePalette() = %v, want %v", got, want)
	}
	for key, value := range want {
		if got[key] != value {
			t.Errorf("parsePalette()[%q] = %q, want %q", key, got[key], value)
		}
	}
}

func TestThemeColorFallback(t *testing.T) {
	saved := pcmPalette
	t.Cleanup(func() { pcmPalette = saved })

	pcmPalette = map[string]string{"PCM_ACCENT": "#6ee7a0"}
	if got := themeColor("PCM_ACCENT", "#cba6f7"); got != "#6ee7a0" {
		t.Errorf("themeColor(PCM_ACCENT) = %q, want theme value", got)
	}
	if got := themeColor("PCM_BLUE", "#89b4fa"); got != "#89b4fa" {
		t.Errorf("themeColor(PCM_BLUE) = %q, want fallback", got)
	}
}
