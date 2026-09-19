package main

import (
	"database/sql"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

// Engagement is one ~/pentest/<name> loot dir that carries an atuin history DB.
type Engagement struct {
	Name string `json:"name"`
	DB   string `json:"db"`
	Rows int    `json:"rows"`
}

// ReportRow is one derived candidate line for the team pentest-report sheet.
// The manual columns (External/Internal, Source, Outcome) are filled in the UI.
type ReportRow struct {
	ID      string `json:"id"`
	TS      int64  `json:"ts"`    // start timestamp, ns since epoch — for sort/merge
	EndTS   int64  `json:"endTs"` // end timestamp, ns — for merge max
	Started string `json:"started"`
	Ended   string `json:"ended"`
	Measure string `json:"measure"` // draft: intent if set, else the raw command
	Command string `json:"command"` // raw command, kept for reference in the UI
	Targets string `json:"targets"`
	Tester  string `json:"tester"`
	Exit    int    `json:"exit"`
	Noise   bool   `json:"noise"` // trivial/connectivity — pre-hidden in the UI
}

const reportDateLayout = "02/01/2006 15:04" // DD/MM/YYYY HH:MM

var berlinLoc = mustLoadLocation("Europe/Berlin")

func mustLoadLocation(name string) *time.Location {
	loc, err := time.LoadLocation(name)
	if err != nil {
		return time.Local
	}
	return loc
}

// pentestLootDir is the root that holds per-engagement loot dirs.
// Mirrors pentest.sh: PENTEST_LOOT_DIR, default ~/pentest.
func pentestLootDir() string {
	if d := os.Getenv("PENTEST_LOOT_DIR"); d != "" {
		return d
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "pentest"
	}
	return filepath.Join(home, "pentest")
}

// listEngagements finds every atuin history.db it can: the loot dirs under
// PENTEST_LOOT_DIR (default ~/pentest), plus every pomdock pentest container's
// mounted atuin dir — which is how engagements launched with a custom loot root
// (e.g. ~/projects/<engagement>/pentest) become selectable.
func listEngagements() ([]Engagement, error) {
	var out []Engagement
	byDB := map[string]bool{}   // dedup by resolved DB path
	byName := map[string]bool{} // keep engagement names unique in the dropdown

	add := func(name, db string) {
		info, err := os.Stat(db)
		if err != nil || info.IsDir() {
			return
		}
		key := resolvePath(db)
		if byDB[key] {
			return
		}
		byDB[key] = true
		// Disambiguate a name clash that points at a different DB.
		if byName[name] {
			name = name + " (docker)"
		}
		byName[name] = true
		out = append(out, Engagement{Name: name, DB: db, Rows: countRows(db)})
	}

	// Loot dirs under the configured root.
	root := pentestLootDir()
	if entries, err := os.ReadDir(root); err == nil {
		for _, e := range entries {
			if e.IsDir() {
				add(e.Name(), filepath.Join(root, e.Name(), ".atuin", "history.db"))
			}
		}
		add("(default)", filepath.Join(root, ".atuin", "history.db"))
	}

	// Running/known pentest containers, wherever their loot dir lives.
	for _, e := range engagementsFromContainers() {
		add(e.Name, e.DB)
	}

	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func resolvePath(p string) string {
	if abs, err := filepath.Abs(p); err == nil {
		p = abs
	}
	if real, err := filepath.EvalSymlinks(p); err == nil {
		return real
	}
	return filepath.Clean(p)
}

// engagementsFromContainers reads each pomdock pentest container's mounted atuin
// directory and turns it into an engagement named after the container. Best
// effort: if docker is unavailable it returns nil and the loot-dir scan stands.
func engagementsFromContainers() []Engagement {
	cs, err := ListContainers()
	if err != nil || len(cs) == 0 {
		return nil
	}
	names := make([]string, 0, len(cs))
	for _, c := range cs {
		names = append(names, c.Name)
	}
	var out []Engagement
	for name, src := range atuinMountSources(names) {
		if src == "" {
			continue
		}
		db := filepath.Join(src, "history.db")
		if info, err := os.Stat(db); err == nil && !info.IsDir() {
			out = append(out, Engagement{Name: name, DB: db})
		}
	}
	return out
}

// atuinMountSources maps container name -> host path bound to the container's
// atuin data dir (/home/kali/.local/share/atuin), via a single docker inspect.
func atuinMountSources(names []string) map[string]string {
	if len(names) == 0 {
		return nil
	}
	format := `{{.Name}}` + "\t" +
		`{{range .Mounts}}{{if eq .Destination "/home/kali/.local/share/atuin"}}{{.Source}}{{end}}{{end}}`
	args := append([]string{"inspect", "--format", format}, names...)
	out, err := exec.Command("docker", args...).Output()
	if err != nil {
		return nil
	}
	res := map[string]string{}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		parts := strings.SplitN(line, "\t", 2)
		if len(parts) != 2 {
			continue
		}
		name := strings.TrimPrefix(strings.TrimSpace(parts[0]), "/")
		res[name] = strings.TrimSpace(parts[1])
	}
	return res
}

func engagementDB(name string) (string, error) {
	engs, err := listEngagements()
	if err != nil {
		return "", err
	}
	for _, e := range engs {
		if e.Name == name {
			return e.DB, nil
		}
	}
	return "", fmt.Errorf("engagement %q not found under %s", name, pentestLootDir())
}

func countRows(db string) int {
	var n int
	_ = withSnapshotDB(db, func(conn *sql.DB) error {
		return conn.QueryRow("select count(*) from history where deleted_at is null").Scan(&n)
	})
	return n
}

// withSnapshotDB copies the atuin DB (and its -wal/-shm sidecars) to a private
// temp file and runs fn against the COPY. The engagement's real history.db is
// only ever read byte-for-byte by the file copy — the SQLite engine never opens
// it, so there is zero chance of mutating or corrupting it. Reading the copy
// (which carries the WAL) also means we see the very latest commands, including
// ones a live container has only written to the WAL and not yet checkpointed.
func withSnapshotDB(db string, fn func(*sql.DB) error) error {
	tmp, err := os.MkdirTemp("", "pomdock-report-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)

	dst := filepath.Join(tmp, "history.db")
	if err := copyFileRO(db, dst); err != nil {
		return err
	}
	// Sidecars may not exist (DB checkpointed / never in WAL) — best effort.
	for _, ext := range []string{"-wal", "-shm"} {
		_ = copyFileRO(db+ext, dst+ext)
	}

	// The copy is disposable, so a plain (non-immutable) open is fine and lets
	// SQLite replay the WAL into it for an up-to-date, consistent read.
	conn, err := sql.Open("sqlite", "file:"+dst+"?_pragma=busy_timeout(3000)")
	if err != nil {
		return err
	}
	defer conn.Close()
	return fn(conn)
}

// copyFileRO copies src to dst, opening src strictly read-only.
func copyFileRO(src, dst string) error {
	in, err := os.OpenFile(src, os.O_RDONLY, 0)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}

// loadRows reads and derives every non-deleted command from the DB, oldest
// first. tester is applied verbatim to every row (the team default, e.g. "LS").
func loadRows(db, tester string) ([]ReportRow, error) {
	var out []ReportRow
	err := withSnapshotDB(db, func(conn *sql.DB) error {
		var lerr error
		out, lerr = scanRows(conn, tester)
		return lerr
	})
	return out, err
}

func scanRows(conn *sql.DB, tester string) ([]ReportRow, error) {
	rows, err := conn.Query(`select id, timestamp, duration, exit, command, intent
		from history where deleted_at is null order by timestamp asc`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []ReportRow
	for rows.Next() {
		var (
			id      string
			ts, dur int64
			exit    int
			command string
			intent  sql.NullString
		)
		if err := rows.Scan(&id, &ts, &dur, &exit, &command, &intent); err != nil {
			return nil, err
		}
		cmd := strings.TrimSpace(command)
		if cmd == "" {
			continue
		}
		measure := cmd
		if intent.Valid && strings.TrimSpace(intent.String) != "" {
			measure = strings.TrimSpace(intent.String)
		}
		out = append(out, ReportRow{
			ID:      id,
			TS:      ts,
			EndTS:   ts + dur,
			Started: fmtTS(ts),
			Ended:   fmtTS(ts + dur),
			Measure: measure,
			Command: cmd,
			Targets: extractTargets(cmd),
			Tester:  tester,
			Exit:    exit,
			Noise:   isNoise(cmd),
		})
	}
	return out, rows.Err()
}

func fmtTS(ns int64) string {
	return time.Unix(0, ns).In(berlinLoc).Format(reportDateLayout)
}

var (
	urlRe  = regexp.MustCompile(`https?://[^\s"'<>|]+`)
	ipv4Re = regexp.MustCompile(`\b(?:\d{1,3}\.){3}\d{1,3}\b`)
	fqdnRe = regexp.MustCompile(`\b(?:[a-zA-Z0-9](?:[a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?\.)+[a-zA-Z]{2,}\b`)

	// Tokens that are almost always tooling/package noise, not engagement targets.
	falseTargetRe = regexp.MustCompile(`(?i)(github\.com|gitlab\.com|kali\.org|debian\.org|ubuntu\.com|pypi\.org|golang\.org|\.py|\.sh|\.txt|\.json|\.git)$`)

	// Trivial navigation / UI verbs — almost never a report row, even during a
	// pentest. Deliberately EXCLUDES dual-use enumeration commands (whoami, id,
	// cat, echo, env, history, export): those are often real evidence, so they
	// stay visible by default and you can add them as your own noise rule if
	// they're just clutter in a given engagement.
	trivialRe = regexp.MustCompile(`(?i)^\s*(cd|ls|ll|la|l\.|clear|exit|pwd|q|:q|:wq|fg|bg|jobs|reset|which|type|man|help|less|more|tmux|htop|top|nano|vim|vi|code|source|alias|unalias)\b`)

	// Connectivity self-checks that clutter every engagement but aren't findings.
	connHostRe = regexp.MustCompile(`(?i)\b(ifconfig\.me|api\.ipify\.org|ipify\.org|icanhazip\.com|checkip\.\w+|am\.i\.mullvad\.net|www\.google\.(?:de|com)|google\.(?:de|com)|1\.1\.1\.1|8\.8\.8\.8)\b`)
	connCmdRe  = regexp.MustCompile(`(?i)^\s*(ping|curl|wget|dig|host|nslookup|traceroute|mtr)\b`)
)

// extractTargets pulls URLs, IPv4 addresses and FQDNs out of a command line,
// de-duplicated in first-seen order. It is intentionally generous; the UI lets
// you trim what it catches.
func extractTargets(cmd string) string {
	var found []string
	seen := map[string]bool{}
	push := func(s string) {
		k := strings.ToLower(s)
		if k == "" || seen[k] {
			return
		}
		seen[k] = true
		found = append(found, s)
	}

	for _, u := range urlRe.FindAllString(cmd, -1) {
		push(strings.TrimRight(u, ".,);"))
	}
	rest := urlRe.ReplaceAllString(cmd, " ")
	for _, ip := range ipv4Re.FindAllString(rest, -1) {
		if validIPv4(ip) && ip != "127.0.0.1" && ip != "0.0.0.0" {
			push(ip)
		}
	}
	for _, d := range fqdnRe.FindAllString(rest, -1) {
		if ipv4Re.MatchString(d) || falseTargetRe.MatchString(d) {
			continue
		}
		push(d)
	}
	return strings.Join(found, ", ")
}

func validIPv4(ip string) bool {
	for _, p := range strings.Split(ip, ".") {
		n := 0
		for _, c := range p {
			if c < '0' || c > '9' {
				return false
			}
			n = n*10 + int(c-'0')
		}
		if n > 255 {
			return false
		}
	}
	return true
}

// isNoise flags trivial shell verbs and connectivity self-checks so the UI can
// pre-hide them. Nothing is dropped — the row is still available behind the
// "show noise" toggle.
func isNoise(cmd string) bool {
	if trivialRe.MatchString(cmd) {
		return true
	}
	if connCmdRe.MatchString(cmd) && connHostRe.MatchString(cmd) {
		return true
	}
	return false
}
