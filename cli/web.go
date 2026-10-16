package main

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	neturl "net/url"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

// defaultWebPort is tried first so the UI keeps one origin across runs; a busy
// port falls back to a random free one.
const defaultWebPort = 7332

const sessionCookie = "pomdock_web"

func webCmd() *cobra.Command {
	var (
		name      string
		port      int
		noOpen    bool
		tester    string
		tokenFile string
	)
	cmd := &cobra.Command{
		Use:     "web",
		Aliases: []string{"tracker", "report"},
		Short:   "Web UI: activity tracker, recorded sessions, and container control",
		Long: styleAccent.Render("pomdock web") + " — a local web UI for engagements and containers:\n\n" +
			"  Tracker     atuin history as team-report rows; curation is saved per engagement\n" +
			"  Sessions    recorded shell captures, searchable across commands and output\n" +
			"  Containers  create, start, stop, recreate and remove pentest containers\n" +
			"  Shells      open recorded container shells in the pomdock tmux workspace\n\n" +
			styleMuted.Render("Binds 127.0.0.1 only. The printed URL carries a per-run login token;\n"+
				"--token-file keeps one token (and the login) across restarts.\n"+
				"Tracker curation is stored in <loot>/.pomdock/tracker.json."),
		RunE: func(_ *cobra.Command, _ []string) error {
			return serveWeb(webOptions{engagement: name, tester: tester, port: port, open: !noOpen, tokenFile: tokenFile})
		},
	}
	cmd.Flags().StringVarP(&name, "name", "n", "", "engagement to open first (default: pick in the UI)")
	cmd.Flags().StringVarP(&tester, "tester", "t", "LS", "default value for the Tester column")
	cmd.Flags().IntVarP(&port, "port", "p", defaultWebPort, "port to bind on 127.0.0.1 (0 = random)")
	cmd.Flags().BoolVar(&noOpen, "no-open", false, "do not auto-open the browser")
	cmd.Flags().StringVar(&tokenFile, "token-file", "", "read the login token from this file (created mode 0600 if missing)")
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
			"and store it under the loot dir so the web UI can search and browse it.",
		Args: cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			if name == "" {
				return fmt.Errorf("--name <engagement> is required (see: pomdock web, engagement picker)")
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

type webOptions struct {
	engagement string
	tester     string
	port       int
	open       bool
	tokenFile  string // stable login token; empty = random per run
}

type webServer struct {
	opts   webOptions
	token  string
	hosts  map[string]bool // Host header values this server answers to
	static fs.FS
	jobs   *jobRunner
}

func serveWeb(opts webOptions) error {
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", opts.port))
	if err != nil && opts.port == defaultWebPort {
		logWarn("port %d is busy, using a random free port", defaultWebPort)
		ln, err = net.Listen("tcp", "127.0.0.1:0")
	}
	if err != nil {
		return fmt.Errorf("bind 127.0.0.1:%d: %w", opts.port, err)
	}
	port := ln.Addr().(*net.TCPAddr).Port

	static, err := webDist()
	if err != nil {
		return err
	}
	token := randomToken()
	if opts.tokenFile != "" {
		if token, err = loadOrCreateToken(opts.tokenFile); err != nil {
			return err
		}
	}
	srv := &webServer{
		opts:   opts,
		token:  token,
		hosts:  map[string]bool{fmt.Sprintf("127.0.0.1:%d", port): true, fmt.Sprintf("localhost:%d", port): true},
		static: static,
		jobs:   newJobRunner(),
	}

	url := fmt.Sprintf("http://127.0.0.1:%d/?token=%s", port, srv.token)
	if opts.engagement != "" {
		url += "#/tracker?name=" + neturl.QueryEscape(opts.engagement)
	}
	logOK("web UI serving at %s", styleAccent.Render(url))
	logStep("the token in the URL logs the browser in; press Ctrl-C to stop")
	if opts.open {
		go openBrowser(url)
	}
	return http.Serve(ln, srv.handler())
}

func randomToken() string {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		panic(err) // crypto/rand never fails on supported platforms
	}
	return hex.EncodeToString(b)
}

// minTokenLen keeps a hand-written token file from weakening the login.
const minTokenLen = 32

// loadOrCreateToken returns the token stored in path, creating the file with a
// random token (mode 0600) when it does not exist. An existing file must not be
// readable by group or others: anyone holding the token controls containers.
func loadOrCreateToken(path string) (string, error) {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err == nil {
		tok := randomToken()
		_, err = io.WriteString(f, tok+"\n")
		if cerr := f.Close(); err == nil {
			err = cerr
		}
		if err != nil {
			return "", fmt.Errorf("write token file: %w", err)
		}
		logOK("created token file %s", path)
		return tok, nil
	}
	if !errors.Is(err, fs.ErrExist) {
		if errors.Is(err, fs.ErrNotExist) {
			return "", fmt.Errorf("create token file: %w (does its directory exist?)", err)
		}
		return "", fmt.Errorf("create token file: %w", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("token file %s is not a regular file", path)
	}
	if info.Mode().Perm()&0o077 != 0 {
		return "", fmt.Errorf("token file %s is accessible by group/others (mode %o); run: chmod 600 %s",
			path, info.Mode().Perm(), path)
	}
	buf, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	tok := strings.TrimSpace(string(buf))
	if len(tok) < minTokenLen {
		return "", fmt.Errorf("token in %s is shorter than %d characters", path, minTokenLen)
	}
	return tok, nil
}

func (s *webServer) handler() http.Handler {
	mux := http.NewServeMux()

	// Engagement data (tracker + sessions).
	mux.HandleFunc("GET /api/meta", s.handleMeta)
	mux.HandleFunc("GET /api/engagements", s.handleEngagements)
	mux.HandleFunc("GET /api/rows", s.handleRows)
	mux.HandleFunc("GET /api/tracker", s.handleTrackerGet)
	mux.HandleFunc("PUT /api/tracker", s.handleTrackerPut)
	mux.HandleFunc("GET /api/prefs", s.handlePrefsGet)
	mux.HandleFunc("PUT /api/prefs", s.handlePrefsPut)
	mux.HandleFunc("GET /api/sessions", s.handleSessions)
	mux.HandleFunc("GET /api/session", s.handleSession)
	mux.HandleFunc("DELETE /api/session", s.handleSessionDelete)
	mux.HandleFunc("POST /api/session/archive", s.handleSessionArchive(true))
	mux.HandleFunc("POST /api/session/restore", s.handleSessionArchive(false))
	mux.HandleFunc("GET /api/logsearch", s.handleLogSearch)

	// Container control (web_control.go).
	mux.HandleFunc("GET /api/containers", s.handleContainers)
	mux.HandleFunc("POST /api/containers", s.handleContainerCreate)
	mux.HandleFunc("POST /api/containers/{name}/start", s.handleContainerStart)
	mux.HandleFunc("POST /api/containers/{name}/stop", s.handleContainerStop)
	mux.HandleFunc("POST /api/containers/{name}/recreate", s.handleContainerRecreate)
	mux.HandleFunc("DELETE /api/containers/{name}", s.handleContainerRemove)
	mux.HandleFunc("POST /api/containers/{name}/shell", s.handleContainerShell)
	mux.HandleFunc("POST /api/containers/{name}/check/{tool}", s.handleContainerCheck)
	mux.HandleFunc("GET /api/shells", s.handleShells)
	mux.HandleFunc("DELETE /api/shells/{id}", s.handleShellClose)
	mux.HandleFunc("GET /api/jobs", s.handleJobs)
	mux.HandleFunc("GET /api/jobs/{id}", s.handleJob)

	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		httpError(w, http.StatusNotFound, fmt.Errorf("no such endpoint: %s %s", r.Method, r.URL.Path))
	})
	mux.Handle("/", s.staticHandler())

	return securityHeaders(s.guard(mux))
}

// guard enforces the local-only trust model on every request:
//   - Host must be the exact loopback address we bound, which defeats DNS
//     rebinding (a hostile page re-pointing its own domain at 127.0.0.1).
//   - The browser must hold the session cookie, obtained by opening the
//     printed URL once (?token=...). The cookie is SameSite=Strict.
//   - State-changing requests must come from our own Origin and carry the
//     X-Pomdock header, which a cross-site form or no-cors fetch cannot send.
func (s *webServer) guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.hosts[r.Host] {
			http.Error(w, "unexpected Host header", http.StatusMisdirectedRequest)
			return
		}
		if tok := r.URL.Query().Get("token"); tok != "" {
			if !s.validToken(tok) {
				http.Error(w, "invalid token", http.StatusUnauthorized)
				return
			}
			cookie := &http.Cookie{
				Name: sessionCookie, Value: s.token, Path: "/",
				HttpOnly: true, SameSite: http.SameSiteStrictMode,
			}
			// A stable token outlives this process, so the login may outlive
			// the browser session too; a per-run token dies with the server.
			if s.opts.tokenFile != "" {
				cookie.MaxAge = int((30 * 24 * time.Hour).Seconds())
			}
			http.SetCookie(w, cookie)
			// Drop the token from the address bar; the #fragment survives the
			// redirect. Always "/" — echoing the request path could be abused
			// as a protocol-relative open redirect ("//elsewhere").
			http.Redirect(w, r, "/", http.StatusSeeOther)
			return
		}
		if c, err := r.Cookie(sessionCookie); err != nil || !s.validToken(c.Value) {
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = io.WriteString(w, "Not logged in. Open the URL printed by `pomdock web` (it carries the login token).\n")
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			if r.Header.Get("Origin") != "http://"+r.Host || r.Header.Get("X-Pomdock") != "1" {
				httpError(w, http.StatusForbidden, errors.New("cross-origin request rejected"))
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func (s *webServer) validToken(tok string) bool {
	return subtle.ConstantTimeCompare([]byte(tok), []byte(s.token)) == 1
}

// securityHeaders applies a strict CSP: the page may only load its own embedded
// assets and talk to this server.
func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy",
			"default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; "+
				"img-src 'self' data:; font-src 'self' data:; connect-src 'self'; "+
				"base-uri 'none'; form-action 'none'; frame-ancestors 'none'")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r)
	})
}

// staticHandler serves the embedded SPA; unknown non-API paths get index.html.
func (s *webServer) staticHandler() http.Handler {
	files := http.FileServer(http.FS(s.static))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		if p != "" && p != "index.html" {
			if _, err := fs.Stat(s.static, p); err == nil {
				files.ServeHTTP(w, r)
				return
			}
		}
		index, err := fs.ReadFile(s.static, "index.html")
		if err != nil {
			http.Error(w, "web UI not built: run `make build` in cli/", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(index)
	})
}

// --- engagement handlers ------------------------------------------------------

func (s *webServer) handleMeta(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, map[string]any{
		"version": version, "tester": s.opts.tester, "engagement": s.opts.engagement,
		"workspace": workspaceSession,
	})
}

func (s *webServer) handleEngagements(w http.ResponseWriter, r *http.Request) {
	engs, err := cachedEngagements(r.URL.Query().Get("refresh") == "1")
	if err != nil {
		httpError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, map[string]any{"engagements": engs})
}

// engagementParam returns ?name=, writing a 400 when it is missing.
func engagementParam(w http.ResponseWriter, r *http.Request) (string, bool) {
	name := r.URL.Query().Get("name")
	if name == "" {
		httpError(w, http.StatusBadRequest, errors.New("missing ?name="))
		return "", false
	}
	return name, true
}

func (s *webServer) handleRows(w http.ResponseWriter, r *http.Request) {
	name, ok := engagementParam(w, r)
	if !ok {
		return
	}
	db, err := engagementDB(name)
	if err != nil {
		httpError(w, http.StatusNotFound, err)
		return
	}
	rows, err := loadRows(db, s.opts.tester)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err)
		return
	}
	if rows == nil {
		rows = []ReportRow{}
	}
	writeJSON(w, map[string]any{"rows": rows})
}

func (s *webServer) handleTrackerGet(w http.ResponseWriter, r *http.Request) {
	name, ok := engagementParam(w, r)
	if !ok {
		return
	}
	p, err := trackerStatePath(name)
	if err != nil {
		httpError(w, http.StatusNotFound, err)
		return
	}
	s.serveDoc(w, p)
}

func (s *webServer) handleTrackerPut(w http.ResponseWriter, r *http.Request) {
	name, ok := engagementParam(w, r)
	if !ok {
		return
	}
	p, err := trackerStatePath(name)
	if err != nil {
		httpError(w, http.StatusNotFound, err)
		return
	}
	s.storeDoc(w, r, p)
}

func (s *webServer) handlePrefsGet(w http.ResponseWriter, _ *http.Request) {
	p, err := webPrefsPath()
	if err != nil {
		httpError(w, http.StatusInternalServerError, err)
		return
	}
	s.serveDoc(w, p)
}

func (s *webServer) handlePrefsPut(w http.ResponseWriter, r *http.Request) {
	p, err := webPrefsPath()
	if err != nil {
		httpError(w, http.StatusInternalServerError, err)
		return
	}
	s.storeDoc(w, r, p)
}

func (s *webServer) serveDoc(w http.ResponseWriter, p string) {
	doc, err := readJSONDoc(p)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(doc)
}

func (s *webServer) storeDoc(w http.ResponseWriter, r *http.Request, p string) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxStateBytes+1))
	if err != nil {
		httpError(w, http.StatusRequestEntityTooLarge, err)
		return
	}
	if err := writeJSONDoc(p, body); err != nil {
		httpError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}

func (s *webServer) handleSessions(w http.ResponseWriter, r *http.Request) {
	name, ok := engagementParam(w, r)
	if !ok {
		return
	}
	sessions, err := listSessions(name, r.URL.Query().Get("archived") == "1")
	if err != nil {
		httpError(w, http.StatusNotFound, err)
		return
	}
	writeJSON(w, map[string]any{"sessions": sessions})
}

func (s *webServer) handleSession(w http.ResponseWriter, r *http.Request) {
	name, ok := engagementParam(w, r)
	if !ok {
		return
	}
	id, ok := sessionIDParam(w, r)
	if !ok {
		return
	}
	sess, err := getSession(name, id, r.URL.Query().Get("archived") == "1")
	if err != nil {
		httpError(w, http.StatusNotFound, err)
		return
	}
	writeJSON(w, sess)
}

func sessionIDParam(w http.ResponseWriter, r *http.Request) (string, bool) {
	id := r.URL.Query().Get("id")
	if id == "" {
		httpError(w, http.StatusBadRequest, errors.New("missing ?id="))
		return "", false
	}
	return id, true
}

// handleSessionArchive moves a session's capture file into (archive=true) or
// out of sessions/archive/. The engagement's atuin history is not touched.
func (s *webServer) handleSessionArchive(archive bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		name, ok := engagementParam(w, r)
		if !ok {
			return
		}
		id, ok := sessionIDParam(w, r)
		if !ok {
			return
		}
		n, err := setSessionArchived(name, id, archive)
		if err != nil {
			httpError(w, http.StatusConflict, err)
			return
		}
		writeJSON(w, map[string]any{"ok": true, "sessions": n})
	}
}

func (s *webServer) handleSessionDelete(w http.ResponseWriter, r *http.Request) {
	name, ok := engagementParam(w, r)
	if !ok {
		return
	}
	id, ok := sessionIDParam(w, r)
	if !ok {
		return
	}
	n, err := deleteSession(name, id, r.URL.Query().Get("archived") == "1")
	if err != nil {
		httpError(w, http.StatusConflict, err)
		return
	}
	writeJSON(w, map[string]any{"ok": true, "sessions": n})
}

func (s *webServer) handleLogSearch(w http.ResponseWriter, r *http.Request) {
	name, ok := engagementParam(w, r)
	if !ok {
		return
	}
	hits, err := searchSessions(name, r.URL.Query().Get("q"))
	if err != nil {
		httpError(w, http.StatusNotFound, err)
		return
	}
	writeJSON(w, map[string]any{"hits": hits})
}

// --- helpers ------------------------------------------------------------------

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func httpError(w http.ResponseWriter, status int, err error) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
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
