package main

import (
	"strings"
	"testing"
	"time"
)

func TestParsePastedLog(t *testing.T) {
	paste := "host in ~/pentest/enum via 🐍 v3.13\n" +
		"❯ curl ifconfig.me\n" +
		"192.0.2.5\n" +
		"\n" +
		"host in ~/pentest/enum via 🐍 v3.13 took 2s\n" +
		"❯ python3 auth.py --env dev call get_user\n" +
		"{\n  \"mfaToken\": \"abc\"\n}\n" +
		"\n" +
		"host in ~/pentest/enum via 🐍 v3.13\n" +
		"❯ whoami\n" +
		"kali\n"
	got := parsePastedLog(paste)
	if len(got) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(got))
	}
	if got[0].Command != "curl ifconfig.me" || got[0].Output != "192.0.2.5" {
		t.Errorf("entry0 wrong: %+v", got[0])
	}
	if got[0].Cwd != "~/pentest/enum" {
		t.Errorf("cwd not parsed: %q", got[0].Cwd)
	}
	if got[1].Command != "python3 auth.py --env dev call get_user" {
		t.Errorf("entry1 command wrong: %q", got[1].Command)
	}
	if !contains(got[1].Output, "mfaToken") {
		t.Errorf("entry1 output missing token: %q", got[1].Output)
	}
	if got[2].Command != "whoami" || got[2].Output != "kali" {
		t.Errorf("entry2 wrong: %+v", got[2])
	}
}

func TestSnippet(t *testing.T) {
	s := snippet("aaaa SECRET bbbb", "secret")
	if !contains(s, "SECRET") {
		t.Errorf("snippet missing match: %q", s)
	}
}

func contains(h, n string) bool {
	return len(h) >= len(n) && (indexOf(h, n) >= 0)
}
func indexOf(h, n string) int {
	for i := 0; i+len(n) <= len(h); i++ {
		if h[i:i+len(n)] == n {
			return i
		}
	}
	return -1
}

// TestSessionMarkerContaminated guards the regression where zsh emits a
// terminal-title escape (ESC k … ESC \) carrying the typed command right before
// pomsession's marker print. cleanLog used to strip only the bare ESC bytes,
// leaving "kpdsession\" glued onto the marker so the ^### anchor never matched
// and the tag was lost. See report_sessions.go: ansiStrRe + the loosened anchor.
func TestSessionMarkerContaminated(t *testing.T) {
	raw := "some output\r\n" +
		"\x1bkpdsession start recon\x1b\\###POMDOCK-SESSION::recon::1700000000###\r\n" +
		"host in ~ via 🐍 v3.13\n" +
		"❯ nmap -sV target\n" +
		"22/tcp open ssh\n"
	segs := splitSessions(cleanLog(raw))
	var got *logSegment
	for i := range segs {
		if segs[i].tag != "" {
			got = &segs[i]
		}
	}
	if got == nil {
		t.Fatalf("no tagged segment found; segments=%d", len(segs))
	}
	if got.tag != "recon" {
		t.Fatalf("tag = %q, want %q", got.tag, "recon")
	}
	if got.epoch != 1700000000 {
		t.Fatalf("epoch = %d, want %d", got.epoch, 1700000000)
	}
	entries := parsePastedLog(got.text)
	if len(entries) != 1 || entries[0].Command != "nmap -sV target" {
		t.Fatalf("entries = %+v, want one nmap command", entries)
	}
}

// TestCleanLogStripsTitleSequence checks the ESC k … ESC \ screen/tmux title
// form is removed whole, not partially.
func TestCleanLogStripsTitleSequence(t *testing.T) {
	got := cleanLog("\x1bkvim /etc/hosts\x1b\\real output\n")
	if strings.Contains(got, "vim") || strings.Contains(got, "\x1b") {
		t.Fatalf("title sequence not fully stripped: %q", got)
	}
	if !strings.HasPrefix(got, "real output") {
		t.Fatalf("cleanLog = %q, want it to start with %q", got, "real output")
	}
}

// TestCleanLogRecoversInlineCommand is the core regression for the "recorded
// sessions had no commands" bug: zsh echoes the typed command on the SAME
// physical line as the ❯ prompt (ZLE redraws it with backspaces and
// syntax-highlight rewrites) and ends that line with a bare CR. The old
// last-CR heuristic discarded the whole line, so every command vanished. The
// cursor model in renderLine must reconstruct "❯ ls".
func TestCleanLogRecoversInlineCommand(t *testing.T) {
	raw := "\x1b[1;31m4841 in \x1b[1;36m~\x1b[0m \r\n" +
		"\x1b[1;32m❯\x1b[0m \x1b[K\x1b[?2004hl\x08\x1b[32ml\x1b[39m\x1b[90ms\x1b[39m\x08\x08\x1b[32ml\x1b[32ms\x1b[39m\x1b[?2004l\r\r\n" +
		"\x1bkls\x1b\\total 0\r\n"
	clean := cleanLog(raw)
	if !strings.Contains(clean, "❯ ls") {
		t.Fatalf("command not recovered from clean log:\n%q", clean)
	}
	entries := parsePastedLog(clean)
	if len(entries) != 1 || entries[0].Command != "ls" {
		t.Fatalf("entries = %+v, want one 'ls' command", entries)
	}
	if entries[0].Output != "total 0" {
		t.Fatalf("output = %q, want %q", entries[0].Output, "total 0")
	}
}

// TestCleanLogCursorMoves checks the horizontal cursor CSIs renderLine relies on:
// a forward jump (C), backspace overwrite, and erase-to-end (K).
func TestCleanLogCursorMoves(t *testing.T) {
	cases := map[string]string{
		"abc\x1b[2Dxy\r\n":       "axy",  // back 2, overwrite b,c
		"hello\x1b[3D\x1b[K\r\n": "he",   // back 3 then erase to end
		"abXX\x08\x08cd\r\n":     "abcd", // two backspaces then overwrite
	}
	for raw, want := range cases {
		if got := strings.TrimRight(cleanLog(raw), "\n"); got != want {
			t.Errorf("cleanLog(%q) = %q, want %q", raw, got, want)
		}
	}
}

func TestIsRedrawGhost(t *testing.T) {
	cmd := "python3 poc.py --url \"$WS/x\""
	if !isRedrawGhost(cmd, "  python3            \"$WS/x\"") {
		t.Error("gappy re-render of the command should be flagged as a ghost")
	}
	if isRedrawGhost(cmd, "python3: can't open file '/home/kali/poc.py'") {
		t.Error("real error output must not be flagged as a ghost")
	}
	if isRedrawGhost("ls", "total 0") {
		t.Error("normal output must not be flagged as a ghost")
	}
}

func TestScriptStartNS(t *testing.T) {
	raw := "Script started on 2026-09-14 19:07:03+02:00 [COMMAND=\"exec zsh\"]\nfoo\n"
	got := scriptStartNS(raw)
	want := time.Date(2026, 9, 14, 19, 7, 3, 0, time.FixedZone("", 2*3600)).UnixNano()
	if got != want {
		t.Fatalf("scriptStartNS = %d, want %d", got, want)
	}
	if scriptStartNS("no header here\n") != 0 {
		t.Fatal("missing header should yield 0")
	}
}
