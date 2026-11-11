package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func runCLICommand(cmd *cobra.Command, args ...string) (string, error) {
	var output bytes.Buffer
	cmd.SetOut(&output)
	cmd.SetErr(&output)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return output.String(), err
}

func TestSessionsCLIOriginalSearchAndCuration(t *testing.T) {
	_, dir, sub := indexedFixture(t)
	original, err := os.ReadFile(filepath.Join(dir, sub+".log"))
	if err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"SYNTHETIC_RESULT", "acme"},
		{"synthetic_result", "acme", "--ignore-case"},
		{"SYNTHETIC_[A-Z]+", "--regex", "--all"},
		{"SCREEN_RESULT", "acme"},
	} {
		output, err := runCLICommand(sessionsSearchCmd(), args...)
		if err != nil || !strings.Contains(output, "acme") {
			t.Fatalf("%q: %v\n%s", args, err, output)
		}
	}
	if _, err := runCLICommand(sessionsSearchCmd(), "synthetic_result", "acme"); err == nil {
		t.Fatal("default search must be case-sensitive")
	}
	if _, err := runCLICommand(sessionsSearchCmd(), "[", "acme", "--regex"); err == nil {
		t.Fatal("invalid regex accepted")
	}
	if _, err := runCLICommand(sessionsSearchCmd(), "synthetic", "acme", "--all"); err == nil {
		t.Fatal("ambiguous scope accepted")
	}
	var data cliRecordingTimeline
	if err := runRecordingEngine(context.Background(), "acme", dir, "timeline", nil, &data); err != nil {
		t.Fatal(err)
	}
	if err := writeSessionEdits("acme", sessionEdits{Version: 1, Sessions: map[string]sessionEdit{sub: {Entries: map[string]entryEdit{data.Commands[0].ID: {Deleted: true}}}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := runCLICommand(sessionsSearchCmd(), "SYNTHETIC_RESULT", "acme"); err == nil {
		t.Fatal("GUI-hidden command leaked into default search")
	}
	if _, err := runCLICommand(sessionsSearchCmd(), "SYNTHETIC_RESULT", "acme", "--original"); err != nil {
		t.Fatalf("original search unavailable: %v", err)
	}
	command, output := "edited synthetic command", "curated result marker"
	if err := writeSessionEdits("acme", sessionEdits{Version: 1, Sessions: map[string]sessionEdit{sub: {Entries: map[string]entryEdit{data.Commands[0].ID: {Command: &command, Output: &output}}}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := runCLICommand(sessionsSearchCmd(), "curated result", "acme"); err != nil {
		t.Fatalf("edited output missing: %v", err)
	}
	if _, err := runCLICommand(sessionsSearchCmd(), "edited synthetic", "acme"); err != nil {
		t.Fatalf("edited command missing: %v", err)
	}
	if _, err := runCLICommand(sessionsSearchCmd(), "SYNTHETIC_RESULT", "acme"); err == nil {
		t.Fatal("old output retained after GUI edit")
	}
	if got, _ := os.ReadFile(filepath.Join(dir, sub+".log")); !bytes.Equal(got, original) {
		t.Fatal("CLI changed original recording")
	}
}

func TestSessionsCLILogsFullCapture(t *testing.T) {
	_, dir, sub := indexedFixture(t)
	original, err := os.ReadFile(filepath.Join(dir, sub+".log"))
	if err != nil {
		t.Fatal(err)
	}
	output, err := runCLICommand(sessionsLogsCmd(), "acme", sub)
	if err != nil || output != string(original) {
		t.Fatalf("raw capture was altered: %v", err)
	}
	output, err = runCLICommand(sessionsLogsCmd(), "acme", "--clean")
	if err != nil || !strings.Contains(output, "SYNTHETIC_RESULT") || !strings.Contains(output, "SCREEN_RESULT") || strings.Contains(output, "\x1b") {
		t.Fatalf("clean transcript incomplete: %v\n%s", err, output)
	}
	if _, err := runCLICommand(sessionsLogsCmd(), "acme", "../capture"); err == nil {
		t.Fatal("unlisted shell accepted")
	}
}

func TestSessionsCLIListScopes(t *testing.T) {
	indexedFixture(t)
	output, err := runCLICommand(sessionsListCmd())
	if err != nil || !strings.Contains(output, "acme") {
		t.Fatalf("engagement list: %v %s", err, output)
	}
	output, err = runCLICommand(sessionsListCmd(), "acme")
	if err != nil || !strings.Contains(output, "RECORDING") {
		t.Fatalf("recording list: %v %s", err, output)
	}
	if _, err := runCLICommand(sessionsListCmd(), "--archived"); err == nil {
		t.Fatal("ambiguous archive scope accepted")
	}
}

func TestSessionShowURLPreservesQueryScope(t *testing.T) {
	result := webLaunchURL(7332, "synthetic-token", webOptions{engagement: "example", initialView: "sessions", initialQuery: "cmd && output?#"})
	parsed, err := url.Parse(result)
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.SplitN(parsed.EscapedFragment(), "?", 2)
	if len(parts) != 2 || parts[0] != "/sessions" {
		t.Fatalf("wrong route: %s", result)
	}
	params, err := url.ParseQuery(parts[1])
	if err != nil || params.Get("q") != "cmd && output?#" || params.Get("name") != "example" || params.Get("mode") != "timeline" {
		t.Fatalf("query changed: %v %s", err, result)
	}
}

func TestCanonicalSessionsHelpKeepsLegacyPathsHidden(t *testing.T) {
	output, err := runCLICommand(newRootCommand(), "sessions", "--help")
	if err != nil {
		t.Fatal(err)
	}
	for _, action := range []string{"list", "logs", "search", "show", "import", "restore"} {
		if !strings.Contains(output, action) {
			t.Fatalf("missing %s: %s", action, output)
		}
	}
	output, err = runCLICommand(newRootCommand(), "web", "--help")
	if err != nil || strings.Contains(output, "session-restore") || strings.Contains(output, "  import ") {
		t.Fatalf("legacy help bloat: %v %s", err, output)
	}
	if _, err := runCLICommand(newRootCommand(), "web", "session-restore", "--help"); err != nil {
		t.Fatalf("compatibility path removed: %v", err)
	}
}

func TestSessionsCLIImportedSearchUsesCurationAndSameRegex(t *testing.T) {
	loot, session := sessionFixture(t)
	path := filepath.Join(loot, "sessions", "capture.json")
	original, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"original ports", "acme"}, {"ORIGINAL (?=ports)", "--all", "--regex", "-i"}} {
		output, err := runCLICommand(sessionsSearchCmd(), args...)
		if err != nil || !strings.Contains(output, "nmap example.test") || !strings.Contains(output, "unknown time") {
			t.Fatalf("paste search: %v\n%s", err, output)
		}
	}
	updated := "curated service list"
	if _, err := editSession("acme", session.ID, sessionChanges{Entries: []entryChange{{ID: session.Entries[2].ID, Output: &updated}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := runCLICommand(sessionsSearchCmd(), "original ports", "acme"); err == nil {
		t.Fatal("old import text remained in curated search")
	}
	if _, err := runCLICommand(sessionsSearchCmd(), "curated service", "acme"); err != nil {
		t.Fatalf("edited import not searchable: %v", err)
	}
	if _, err := runCLICommand(sessionsSearchCmd(), "original ports", "acme", "--original"); err != nil {
		t.Fatalf("original import not searchable: %v", err)
	}
	if data, _ := os.ReadFile(path); !bytes.Equal(data, original) {
		t.Fatal("paste source modified")
	}
}

func TestSessionsCLIListInheritsCaptureDeletion(t *testing.T) {
	_, _, sub := taggedIndexedFixture(t)
	state := sessionEdits{Version: 1, Sessions: map[string]sessionEdit{sub: {Deleted: true}}}
	if err := writeSessionEdits("acme", state); err != nil {
		t.Fatal(err)
	}
	output, err := runCLICommand(sessionsListCmd(), "acme", "--json")
	if err != nil {
		t.Fatal(err)
	}
	var sessions []Session
	if err := json.Unmarshal([]byte(output), &sessions); err != nil {
		t.Fatal(err)
	}
	for _, session := range sessions {
		if session.CaptureID == sub {
			t.Fatalf("hidden capture resurfaced as section: %+v", session)
		}
	}
	label := "Curated recon"
	state.Sessions = map[string]sessionEdit{sub + "#0": {DisplayName: &label}}
	if err := writeSessionEdits("acme", state); err != nil {
		t.Fatal(err)
	}
	output, err = runCLICommand(sessionsListCmd(), "acme")
	if err != nil || !strings.Contains(output, label) {
		t.Fatalf("curated section name missing: %v %s", err, output)
	}
}
