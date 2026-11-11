package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

type cliRecordingTimeline struct {
	Commands []recordingCommand `json:"commands"`
	Shells   []recordingShell   `json:"shells"`
}
type cliRecordingHit struct {
	T      int64  `json:"t"`
	Text   string `json:"text"`
	Screen bool   `json:"screen"`
}
type cliRecordingGroup struct {
	Sub        string            `json:"sub"`
	SessionID  string            `json:"sessionId"`
	Label      string            `json:"label"`
	Command    *recordingCommand `json:"command"`
	CommandHit bool              `json:"commandHit"`
	Hits       []cliRecordingHit `json:"hits"`
}

func sessionsCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "sessions", Short: "List, search and replay engagement recordings"}
	restore := sessionRestoreCmd()
	restore.Use = "restore <file.tar.age>"
	cmd.AddCommand(sessionsListCmd(), sessionsLogsCmd(), sessionsSearchCmd(), sessionsShowCmd(), reportImportCmd(), restore)
	return cmd
}

func resolveRecordingEngagement(name string) (Engagement, error) {
	engagements, err := listEngagements()
	if err != nil {
		return Engagement{}, err
	}
	if name == "" {
		if len(engagements) != 1 {
			return Engagement{}, fmt.Errorf("specify an engagement name (see pomdock sessions list)")
		}
		return engagements[0], nil
	}
	for _, engagement := range engagements {
		if engagement.Name == name {
			return engagement, nil
		}
	}
	return Engagement{}, fmt.Errorf("engagement %q not found (see pomdock sessions list)", name)
}

func cliRecordingDir(name string, archived bool) (string, error) {
	dir, err := sessionsDir(name)
	if err != nil {
		return "", err
	}
	if archived {
		dir = filepath.Join(dir, archiveSubdir)
	}
	if _, err := os.Stat(dir); err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("no %srecordings for %q; restore an encrypted engagement before reading it", map[bool]string{true: "archived ", false: ""}[archived], name)
		}
		return "", err
	}
	return dir, nil
}

func writeCLIJSON(out io.Writer, data any) error {
	encoder := json.NewEncoder(out)
	encoder.SetIndent("", "  ")
	return encoder.Encode(data)
}

func sessionsListCmd() *cobra.Command {
	var archived, jsonOutput bool
	cmd := &cobra.Command{Use: "list [name]", Short: "List engagements, or the recordings of one engagement", Args: cobra.MaximumNArgs(1), ValidArgsFunction: completeEngagements, RunE: func(cmd *cobra.Command, args []string) error {
		if len(args) == 0 {
			if archived {
				return fmt.Errorf("--archived requires an engagement name")
			}
			engagements, err := listEngagements()
			if err != nil {
				return err
			}
			if jsonOutput {
				return writeCLIJSON(cmd.OutOrStdout(), engagements)
			}
			if len(engagements) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "No engagements. Start one: pomdock docker start NAME")
				return nil
			}
			fmt.Fprintln(cmd.OutOrStdout(), "ENGAGEMENT\tSTATE\tATUIN ROWS")
			for _, e := range engagements {
				state := "available"
				if e.Archived {
					state = "encrypted archive"
				}
				fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\t%d\n", e.Name, state, e.Rows)
			}
			return nil
		}
		engagement, err := resolveRecordingEngagement(args[0])
		if err != nil {
			return err
		}
		recordings, err := listSessions(engagement.Name)
		if archived {
			recordings, err = listArchivedSessions(engagement.Name)
		}
		if err != nil {
			return err
		}
		state, err := readSessionEdits(engagement.Name)
		if err != nil {
			return err
		}
		visible := make([]Session, 0, len(recordings))
		for _, s := range recordings {
			curated := curateSession(&s, state)
			if !curated.Deleted {
				visible = append(visible, *curated)
			}
		}
		if jsonOutput {
			return writeCLIJSON(cmd.OutOrStdout(), visible)
		}
		if len(visible) == 0 {
			fmt.Fprintln(cmd.OutOrStdout(), "No recordings in this scope.")
			return nil
		}
		fmt.Fprintln(cmd.OutOrStdout(), "RECORDING\tSTATE\tSTARTED (Berlin)\tLABEL")
		for _, s := range visible {
			label := s.Title
			if s.DisplayName != nil {
				label = *s.DisplayName
			}
			started := "unknown"
			if s.Created > 0 {
				started = time.Unix(s.Created, 0).In(berlinLoc).Format("2006-01-02 15:04:05 MST")
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\t%s\t%s\n", s.ID, s.State, started, strings.ReplaceAll(label, "\n", " "))
		}
		return nil
	}}
	cmd.Flags().BoolVar(&archived, "archived", false, "list archived recordings (requires an engagement name)")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "write structured JSON")
	return cmd
}

func sessionsLogsCmd() *cobra.Command {
	var clean, archived bool
	cmd := &cobra.Command{Use: "logs <name> [shell]", Short: "List recorded shells, or print one original capture", Long: "List the engagement's recorded shells. With SHELL, print its original terminal capture.\nUse --clean to print the terminal-aware transcript instead of raw escape sequences.", Args: cobra.RangeArgs(1, 2), ValidArgsFunction: completeFirstEngagement, RunE: func(cmd *cobra.Command, args []string) error {
		engagement, err := resolveRecordingEngagement(args[0])
		if err != nil {
			return err
		}
		dir, err := cliRecordingDir(engagement.Name, archived)
		if err != nil {
			return err
		}
		var data cliRecordingTimeline
		if err = runRecordingEngine(cmd.Context(), engagement.Name, dir, "timeline", nil, &data); err != nil {
			return err
		}
		sub := ""
		if len(args) > 1 {
			sub = args[1]
		}
		if sub == "" && !clean {
			fmt.Fprintln(cmd.OutOrStdout(), "SHELL\tSTATE\tBYTES\tSTARTED (Berlin)")
			for _, s := range data.Shells {
				state := "completion unknown"
				if s.Finished {
					state = "ended"
				}
				started := "unknown"
				if s.Start != nil {
					started = time.UnixMilli(*s.Start).In(berlinLoc).Format("2006-01-02 15:04:05 MST")
				}
				fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\t%d\t%s\n", s.Sub, state, s.Bytes, started)
			}
			return nil
		}
		if sub == "" {
			for i := len(data.Shells) - 1; i >= 0; i-- {
				if !data.Shells[i].Background {
					sub = data.Shells[i].Sub
					break
				}
			}
			if sub == "" && len(data.Shells) > 0 {
				sub = data.Shells[len(data.Shells)-1].Sub
			}
		}
		known := false
		for _, s := range data.Shells {
			if s.Sub == sub {
				known = true
				break
			}
		}
		if !known {
			return fmt.Errorf("recorded shell %q not found (see pomdock sessions logs %s)", sub, engagement.Name)
		}
		if !clean {
			file, err := os.Open(filepath.Join(dir, sub+".log"))
			if err != nil {
				return err
			}
			defer file.Close()
			_, err = io.Copy(cmd.OutOrStdout(), file)
			return err
		}
		var result struct {
			Lines []struct {
				T    int64  `json:"t"`
				Kind string `json:"kind"`
				Text string `json:"text"`
			} `json:"lines"`
		}
		if err = runRecordingEngine(cmd.Context(), engagement.Name, dir, "transcript", map[string]any{"sub": sub}, &result); err != nil {
			return err
		}
		for _, line := range result.Lines {
			if line.Kind == "S" {
				fmt.Fprintf(cmd.OutOrStdout(), "--- final screen at %s ---\n", time.UnixMilli(line.T).In(berlinLoc).Format("2006-01-02 15:04:05 MST"))
			} else {
				fmt.Fprintln(cmd.OutOrStdout(), line.Text)
			}
		}
		return nil
	}}
	cmd.Flags().BoolVar(&clean, "clean", false, "print the newest shell, or SHELL, as a readable transcript")
	cmd.Flags().BoolVar(&archived, "archived", false, "read archived recordings")
	return cmd
}

func sessionsSearchCmd() *cobra.Command {
	var all, regex, ignoreCase, archived, jsonOutput, original bool
	cmd := &cobra.Command{Use: "search <pattern> [name]", Short: "Search recorded commands, output and paste imports", Long: "Search literal text in one engagement, or every engagement with --all.\nSearch is case-sensitive by default; --regex enables regular expressions.\nGUI edits and deletions apply by default; --original searches original content.", Args: cobra.RangeArgs(1, 2), RunE: func(cmd *cobra.Command, args []string) error {
		if len(args[0]) > 1024 {
			return fmt.Errorf("search is limited to 1024 characters")
		}
		if all && len(args) > 1 {
			return fmt.Errorf("use an engagement name or --all, not both")
		}
		engagements, err := listEngagements()
		if err != nil {
			return err
		}
		if !all {
			name := ""
			if len(args) > 1 {
				name = args[1]
			}
			selected, err := resolveRecordingEngagement(name)
			if err != nil {
				return err
			}
			engagements = []Engagement{selected}
		}
		type resultGroup struct {
			Engagement string `json:"engagement"`
			cliRecordingGroup
		}
		found := []resultGroup{}
		truncated := false
		skipped := 0
		for _, engagement := range engagements {
			dir, err := cliRecordingDir(engagement.Name, archived)
			if err != nil {
				if all {
					skipped++
					continue
				}
				return err
			}
			state, err := readSessionEdits(engagement.Name)
			if err != nil {
				return err
			}
			imports, err := cliPasteImports(dir, state, original)
			if err != nil {
				return err
			}
			request := map[string]any{"query": args[0], "regex": regex, "case": !ignoreCase, "imports": imports}
			if !original {
				request["edits"] = state.Sessions
			}
			var result struct {
				Groups    []cliRecordingGroup `json:"groups"`
				Error     string              `json:"error"`
				Truncated bool                `json:"truncated"`
			}
			if err = runRecordingEngine(cmd.Context(), engagement.Name, dir, "search", request, &result); err != nil {
				return err
			}
			if result.Error != "" {
				return fmt.Errorf("invalid search pattern: %s", result.Error)
			}
			for _, g := range result.Groups {
				entryID, sessionID := "", g.SessionID
				if g.Command != nil {
					entryID = g.Command.ID
					if g.Command.SessionID != "" {
						sessionID = g.Command.SessionID
					}
				}
				hidden, _ := recordingEntryEdit(state, g.Sub, sessionID, entryID)
				if !original && hidden {
					continue
				}
				found = append(found, resultGroup{Engagement: engagement.Name, cliRecordingGroup: g})
			}
			truncated = truncated || result.Truncated
		}
		sort.SliceStable(found, func(i, j int) bool {
			timestamp := func(g resultGroup) int64 {
				if g.Command != nil {
					return g.Command.T
				}
				if len(g.Hits) > 0 {
					return g.Hits[0].T
				}
				return 0
			}
			return timestamp(found[i]) < timestamp(found[j])
		})
		if skipped > 0 {
			fmt.Fprintf(cmd.ErrOrStderr(), "Skipped %d engagement(s) without recordings in this scope.\n", skipped)
		}
		if jsonOutput {
			if err := writeCLIJSON(cmd.OutOrStdout(), map[string]any{"groups": found, "truncated": truncated}); err != nil {
				return err
			}
		} else {
			for _, g := range found {
				fmt.Fprintf(cmd.OutOrStdout(), "== %s · %s\n", g.Engagement, g.Sub)
				if c := g.Command; c != nil {
					status := "completion unknown"
					if c.Exit != nil {
						status = fmt.Sprintf("exit %d", *c.Exit)
					}
					fmt.Fprintf(cmd.OutOrStdout(), "%s  $ %s  [%s, %s]\n", cliRecordingTime(c.T), strings.ReplaceAll(c.Cmd, "\n", " ⏎ "), status, c.Cwd)
				}
				for _, hit := range g.Hits {
					screen := ""
					if hit.Screen {
						screen = "[screen] "
					}
					fmt.Fprintf(cmd.OutOrStdout(), "%s  > %s%s\n", cliRecordingTime(hit.T), screen, hit.Text)
				}
			}
			if truncated {
				fmt.Fprintln(cmd.ErrOrStderr(), "Results limited; narrow the search for more specific matches.")
			}
		}
		if len(found) == 0 {
			return fmt.Errorf("no matches")
		}
		return nil
	}}
	cmd.Flags().BoolVar(&all, "all", false, "search all engagements with recordings in this scope")
	cmd.Flags().BoolVar(&regex, "regex", false, "interpret PATTERN as a regular expression")
	cmd.Flags().BoolVarP(&ignoreCase, "ignore-case", "i", false, "ignore letter case")
	cmd.Flags().BoolVar(&archived, "archived", false, "search archived recordings; encrypted engagements must be restored first")
	cmd.Flags().BoolVar(&original, "original", false, "search unedited content, including GUI-hidden items")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "write structured JSON")
	cmd.ValidArgsFunction = func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		if len(args) == 1 {
			return completeEngagements(cmd, args, toComplete)
		}
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	return cmd
}

func cliRecordingTime(ms int64) string {
	if ms == 0 {
		return "unknown time"
	}
	return time.UnixMilli(ms).In(berlinLoc).Format("2006-01-02 15:04:05.000 MST")
}

func sessionsShowCmd() *cobra.Command {
	var port int
	var noOpen bool
	cmd := &cobra.Command{Use: "show [name] [query]", Short: "Open an engagement's command timeline and replay", Args: cobra.MaximumNArgs(2), RunE: func(cmd *cobra.Command, args []string) error {
		name := ""
		if len(args) > 0 {
			name = args[0]
		}
		engagement, err := resolveRecordingEngagement(name)
		if err != nil {
			return err
		}
		query := ""
		if len(args) > 1 {
			query = args[1]
		}
		return serveWeb(webOptions{engagement: engagement.Name, initialView: "sessions", initialQuery: query, tester: "LS", port: port, open: !noOpen})
	}}
	cmd.Flags().IntVar(&port, "port", defaultWebPort, "local web port (0 = random)")
	cmd.Flags().BoolVar(&noOpen, "no-open", false, "print the local URL without opening a browser")
	cmd.ValidArgsFunction = completeFirstEngagement
	return cmd
}

func completeFirstEngagement(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	if len(args) == 0 {
		return completeEngagements(cmd, args, toComplete)
	}
	return nil, cobra.ShellCompDirectiveNoFileComp
}

// Paste imports are already parsed into Session JSON by the existing importer.
// Search them with the same Python pattern as timed captures, retaining entry IDs.
func cliPasteImports(dir string, state sessionEdits, original bool) ([]*Session, error) {
	files, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	result := []*Session{}
	for _, file := range files {
		if !file.Type().IsRegular() || !strings.HasSuffix(file.Name(), ".json") {
			continue
		}
		session, err := readSession(filepath.Join(dir, file.Name()))
		if err != nil || session.Source != "imported-paste" || session.ID == "" {
			continue
		}
		identifyEntries(session)
		if !original {
			session = curateSession(session, state)
		}
		if !session.Deleted {
			result = append(result, session)
		}
	}
	return result, nil
}
