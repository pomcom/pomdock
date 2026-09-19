package main

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
)

//go:embed report.html
var reportHTML []byte

//go:embed sessions.html
var sessionsHTML []byte

func reportCmd() *cobra.Command {
	var (
		name   string
		port   int
		noOpen bool
		tester string
	)
	cmd := &cobra.Command{
		Use:     "tracker",
		Aliases: []string{"report"},
		Short:   "Activity tracker: turn atuin engagement logs into team-report rows",
		Long: styleAccent.Render("pomdock tracker") + " — browse an engagement's atuin history in a\n" +
			"local web grid, pick and merge the report-worthy commands, and copy them\n" +
			"as TSV straight into the team Excel sheet. Includes the Sessions log viewer.\n\n" +
			styleMuted.Render("Reads ~/pentest/<name>/.atuin read-only. Never touches the container image."),
		RunE: func(_ *cobra.Command, _ []string) error {
			return serveReport(name, tester, port, !noOpen)
		},
	}
	cmd.Flags().StringVarP(&name, "name", "n", "", "engagement to open first (default: pick in the UI)")
	cmd.Flags().StringVarP(&tester, "tester", "t", "LS", "default value for the Tester column")
	cmd.Flags().IntVarP(&port, "port", "p", 0, "port to bind (default: random free port)")
	cmd.Flags().BoolVar(&noOpen, "no-open", false, "do not auto-open the browser")
	_ = cmd.RegisterFlagCompletionFunc("name", completeEngagements)
	cmd.AddCommand(reportImportCmd())
	return cmd
}

func reportImportCmd() *cobra.Command {
	var name, date string
	cmd := &cobra.Command{
		Use:   "import <file>",
		Short: "Import a pasted terminal scrollback as a searchable session",
		Long: "Parse a pasted terminal log (starship ❯ prompts) into a session of\n" +
			"commands + output, recover timestamps from the engagement's atuin history,\n" +
			"and store it under the loot dir so the report UI can search and browse it.",
		Args: cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			if name == "" {
				return fmt.Errorf("--name <engagement> is required (see: pomdock tracker, engagement dropdown)")
			}
			sess, matched, err := importPastedLog(args[0], name, date)
			if err != nil {
				return err
			}
			logOK("imported %d commands into engagement %s", len(sess.Entries), styleAccent.Render(name))
			logStep("recovered timestamps for %d/%d commands from atuin history", matched, len(sess.Entries))
			if matched < len(sess.Entries) && date == "" {
				logWarn("%d commands had no atuin match and no date — pass --date YYYY-MM-DD to tag them", len(sess.Entries)-matched)
			}
			dir, _ := sessionsDir(name)
			logStep("saved: %s", filepath.Join(dir, sess.ID+".json"))
			return nil
		},
	}
	cmd.Flags().StringVarP(&name, "name", "n", "", "engagement to import into (required)")
	cmd.Flags().StringVarP(&date, "date", "d", "", "fallback date YYYY-MM-DD for commands with no atuin timestamp")
	_ = cmd.RegisterFlagCompletionFunc("name", completeEngagements)
	return cmd
}

func completeEngagements(_ *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
	engs, err := listEngagements()
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	var names []string
	for _, e := range engs {
		names = append(names, e.Name)
	}
	return names, cobra.ShellCompDirectiveNoFileComp
}

func serveReport(name, tester string, port int, open bool) error {
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		return fmt.Errorf("bind 127.0.0.1:%d: %w", port, err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(reportHTML)
	})
	mux.HandleFunc("/api/engagements", func(w http.ResponseWriter, r *http.Request) {
		engs, err := listEngagements()
		if err != nil {
			httpErr(w, err)
			return
		}
		writeJSON(w, map[string]any{"engagements": engs, "default": name, "tester": tester})
	})
	mux.HandleFunc("/sessions", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(sessionsHTML)
	})
	mux.HandleFunc("/api/sessions", func(w http.ResponseWriter, r *http.Request) {
		eng := r.URL.Query().Get("name")
		if eng == "" {
			httpErr(w, fmt.Errorf("missing ?name="))
			return
		}
		sessions, err := listSessions(eng)
		if err != nil {
			httpErr(w, err)
			return
		}
		writeJSON(w, map[string]any{"sessions": sessions})
	})
	mux.HandleFunc("/api/session", func(w http.ResponseWriter, r *http.Request) {
		eng, id := r.URL.Query().Get("name"), r.URL.Query().Get("id")
		if eng == "" || id == "" {
			httpErr(w, fmt.Errorf("missing ?name= and ?id="))
			return
		}
		s, err := getSession(eng, id)
		if err != nil {
			httpErr(w, err)
			return
		}
		writeJSON(w, s)
	})
	mux.HandleFunc("/api/logsearch", func(w http.ResponseWriter, r *http.Request) {
		eng, q := r.URL.Query().Get("name"), r.URL.Query().Get("q")
		if eng == "" {
			httpErr(w, fmt.Errorf("missing ?name="))
			return
		}
		hits, err := searchSessions(eng, q)
		if err != nil {
			httpErr(w, err)
			return
		}
		writeJSON(w, map[string]any{"hits": hits})
	})
	mux.HandleFunc("/api/rows", func(w http.ResponseWriter, r *http.Request) {
		eng := r.URL.Query().Get("name")
		if eng == "" {
			httpErr(w, fmt.Errorf("missing ?name="))
			return
		}
		db, err := engagementDB(eng)
		if err != nil {
			httpErr(w, err)
			return
		}
		rows, err := loadRows(db, tester)
		if err != nil {
			httpErr(w, err)
			return
		}
		writeJSON(w, map[string]any{"rows": rows})
	})

	url := fmt.Sprintf("http://%s/", ln.Addr().String())
	logOK("tracker UI serving at %s", styleAccent.Render(url))
	logStep("press Ctrl-C to stop")
	if open {
		go openBrowser(url)
	}
	return http.Serve(ln, mux)
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func httpErr(w http.ResponseWriter, err error) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusBadRequest)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
}

func openBrowser(url string) {
	for _, bin := range []string{"xdg-open", "sensible-browser", "firefox"} {
		if _, err := exec.LookPath(bin); err == nil {
			_ = exec.Command(bin, url).Start()
			return
		}
	}
	logWarn("could not find a browser opener; open %s manually", strings.TrimSpace(url))
}
