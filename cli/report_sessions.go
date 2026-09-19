package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// A captured/imported terminal session: an ordered list of commands with their
// stdout. Stored as JSON under <lootdir>/sessions/<id>.json so it lives beside
// the engagement's atuin history and survives container removal.
type Session struct {
	ID         string         `json:"id"`
	Engagement string         `json:"engagement"`
	Source     string         `json:"source"` // "imported-paste" | "script"
	Title      string         `json:"title"`
	Tag        string         `json:"tag"`  // user label from `pdsession <tag>`, "" if none
	Date       string         `json:"date"` // YYYY-MM-DD
	Created    int64          `json:"created"`
	Entries    []SessionEntry `json:"entries"`
}

type SessionEntry struct {
	TS      int64  `json:"ts"`   // ns since epoch, 0 if unknown
	Date    string `json:"date"` // YYYY-MM-DD (from ts, or the --date fallback)
	Time    string `json:"time"` // HH:MM if ts known, else ""
	Cwd     string `json:"cwd"`
	Command string `json:"command"`
	Output  string `json:"output"`
}

// lootDirForEngagement resolves the loot dir (parent of .atuin) for an
// engagement, wherever it lives (~/pentest/<name> or a custom root).
func lootDirForEngagement(name string) (string, error) {
	db, err := engagementDB(name)
	if err != nil {
		return "", err
	}
	return filepath.Dir(filepath.Dir(db)), nil // <loot>/.atuin/history.db -> <loot>
}

func sessionsDir(name string) (string, error) {
	loot, err := lootDirForEngagement(name)
	if err != nil {
		return "", err
	}
	return filepath.Join(loot, "sessions"), nil
}

// --- parsing a pasted starship scrollback -------------------------------------

var (
	promptMarker = "❯ "
	cwdRe        = regexp.MustCompile(`(?:^|\s)in\s+(~?[^\s]+)(?:\s+via|\s+on|\s+took|\s*$)`)
	headerLineRe = regexp.MustCompile(`\sin\s.+\bvia\b|\sin\s+~?/`)
)

// parsePastedLog segments a pasted terminal scrollback into command entries.
// Each block is: a starship header line (carrying the cwd), a "❯ command"
// line, then that command's output up to the next block.
func parsePastedLog(text string) []SessionEntry {
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	var cmdIdx []int
	for i, l := range lines {
		if strings.HasPrefix(strings.TrimLeft(l, " "), promptMarker) {
			cmdIdx = append(cmdIdx, i)
		}
	}
	cwdBefore := func(i int) string {
		for j := i - 1; j >= 0 && j >= i-4; j-- {
			if m := cwdRe.FindStringSubmatch(lines[j]); m != nil {
				return m[1]
			}
		}
		return ""
	}
	var out []SessionEntry
	for k, i := range cmdIdx {
		cmd := strings.TrimSpace(strings.TrimLeft(lines[i], " ")[len(promptMarker):])
		if cmd == "" {
			continue
		}
		end := len(lines)
		if k+1 < len(cmdIdx) {
			end = cmdIdx[k+1] - 1 // drop the next block's header line
		}
		body := append([]string{}, lines[i+1:max(i+1, end)]...)
		// Drop leading ZLE redraw ghosts: when a command is *pasted*, zsh erases
		// and re-renders it one row up, which — with vertical moves flattened —
		// leaves a gappy echo of the command as the first "output" line (e.g.
		// "  python3            \"$WS/machine\""). Strip those and any blanks.
		for len(body) > 0 && (strings.TrimSpace(body[0]) == "" || isRedrawGhost(cmd, body[0])) {
			body = body[1:]
		}
		// trim trailing blanks and a dangling header line
		for len(body) > 0 {
			last := body[len(body)-1]
			if strings.TrimSpace(last) == "" || headerLineRe.MatchString(last) {
				body = body[:len(body)-1]
				continue
			}
			break
		}
		out = append(out, SessionEntry{
			Cwd:     cwdBefore(i),
			Command: cmd,
			Output:  strings.Join(body, "\n"),
		})
	}
	return out
}

// isRedrawGhost reports whether line is a leftover ZLE redraw of cmd rather than
// real output: its non-space characters appear in cmd in order (a subsequence),
// and it carries the tell-tale redraw spacing — leading indentation or a wide
// internal gap left by the cursor-forward jumps of the re-render.
func isRedrawGhost(cmd, line string) bool {
	if strings.HasPrefix(line, " ") == false && strings.Contains(line, "   ") == false {
		return false
	}
	compact := []rune(strings.ReplaceAll(line, " ", ""))
	if len(compact) == 0 {
		return false
	}
	target := []rune(strings.ReplaceAll(cmd, " ", ""))
	j := 0
	for _, r := range target {
		if j < len(compact) && compact[j] == r {
			j++
		}
	}
	return j == len(compact)
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// enrichTimestamps fills each entry's TS/Date/Time by matching its command text
// against the engagement's atuin history. Commands with no match keep TS=0 and
// fall back to dateOverride.
//
// floorNS steers which occurrence of a repeated command is chosen. A recorded
// session runs commands in order after a known start time, but a command like
// `ls` may appear in atuin many times across days; picking the global earliest
// stamps this session's `ls` with some unrelated older time. When floorNS > 0
// (the session's start, in ns) entries are matched monotonically: each takes the
// earliest occurrence at or after the running floor, then advances it — so the
// times land inside this session's window and never move backwards. floorNS == 0
// keeps the old global-earliest behavior (used for pasted scrollback, which has
// no reliable start time).
func enrichTimestamps(engagement string, entries []SessionEntry, dateOverride string, floorNS int64) int {
	db, err := engagementDB(engagement)
	hist := map[string][]int64{} // command -> ascending timestamps
	if err == nil {
		_ = withSnapshotDB(db, func(conn *sql.DB) error {
			rows, qerr := conn.Query(`select timestamp, command from history where deleted_at is null`)
			if qerr != nil {
				return qerr
			}
			defer rows.Close()
			for rows.Next() {
				var ts int64
				var cmd string
				if rows.Scan(&ts, &cmd) == nil {
					c := strings.TrimSpace(cmd)
					hist[c] = append(hist[c], ts)
				}
			}
			return nil
		})
	}
	for c := range hist {
		sort.Slice(hist[c], func(a, b int) bool { return hist[c][a] < hist[c][b] })
	}
	// pick returns the earliest timestamp for cmd at or after floor (or the
	// global earliest when floor is 0), and whether one was found.
	pick := func(cmd string, floor int64) (int64, bool) {
		list, ok := hist[cmd]
		if !ok || len(list) == 0 {
			return 0, false
		}
		if floor <= 0 {
			return list[0], true
		}
		for _, ts := range list {
			if ts >= floor {
				return ts, true
			}
		}
		return 0, false
	}
	matched := 0
	floor := floorNS
	for i := range entries {
		if ts, ok := pick(entries[i].Command, floor); ok {
			entries[i].TS = ts
			if floorNS > 0 {
				floor = ts // keep the session monotonic
			}
			t := time.Unix(0, ts).In(berlinLoc)
			entries[i].Date = t.Format("2006-01-02")
			entries[i].Time = t.Format("15:04")
			matched++
		} else if dateOverride != "" {
			entries[i].Date = dateOverride
		}
	}
	return matched
}

// importPastedLog parses srcPath, enriches timestamps, and writes a session file
// into the engagement's loot dir. Returns the session and the match count.
func importPastedLog(srcPath, engagement, dateOverride string) (*Session, int, error) {
	raw, err := os.ReadFile(srcPath)
	if err != nil {
		return nil, 0, err
	}
	entries := parsePastedLog(string(raw))
	if len(entries) == 0 {
		return nil, 0, fmt.Errorf("no commands found in %s (expected starship ❯ prompts)", srcPath)
	}
	matched := enrichTimestamps(engagement, entries, dateOverride, 0)

	date := dateOverride
	for _, e := range entries {
		if e.TS > 0 {
			date = time.Unix(0, e.TS).In(berlinLoc).Format("2006-01-02")
			break
		}
	}
	base := strings.TrimSuffix(filepath.Base(srcPath), filepath.Ext(srcPath))
	sess := &Session{
		ID:         fmt.Sprintf("import-%s-%d", sanitizeID(base), time.Now().Unix()),
		Engagement: engagement,
		Source:     "imported-paste",
		Title:      fmt.Sprintf("Imported: %s", filepath.Base(srcPath)),
		Date:       date,
		Created:    time.Now().Unix(),
		Entries:    entries,
	}

	dir, err := sessionsDir(engagement)
	if err != nil {
		return nil, 0, err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, 0, err
	}
	f := filepath.Join(dir, sess.ID+".json")
	buf, _ := json.MarshalIndent(sess, "", "  ")
	if err := os.WriteFile(f, buf, 0o644); err != nil {
		return nil, 0, err
	}
	return sess, matched, nil
}

var idSanitize = regexp.MustCompile(`[^a-zA-Z0-9._-]+`)

func sanitizeID(s string) string {
	return strings.Trim(idSanitize.ReplaceAllString(s, "-"), "-")
}

// --- reading sessions back ----------------------------------------------------

// sessionFiles returns every session artifact in the engagement's sessions dir:
// imported .json files and raw `script` .log captures (timing files ignored).
func sessionFiles(engagement string) ([]string, error) {
	dir, err := sessionsDir(engagement)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if strings.HasSuffix(e.Name(), ".json") || strings.HasSuffix(e.Name(), ".log") {
			out = append(out, filepath.Join(dir, e.Name()))
		}
	}
	return out, nil
}

// loadSessions dispatches by file type and can return several logical sessions
// from a single capture: imported JSON is one session; a raw `script` .log is
// split at `pdsession <tag>` markers into one tagged session per segment.
func loadSessions(path, engagement string) ([]*Session, error) {
	if strings.HasSuffix(path, ".json") {
		s, err := readSession(path)
		if err != nil {
			return nil, err
		}
		return []*Session{s}, nil
	}
	return loadCaptureLog(path, engagement)
}

func listSessions(engagement string) ([]Session, error) {
	files, err := sessionFiles(engagement)
	if err != nil {
		return nil, err
	}
	out := []Session{}
	for _, f := range files {
		sessions, err := loadSessions(f, engagement)
		if err != nil {
			continue
		}
		for _, s := range sessions {
			meta := *s
			meta.Entries = nil // list view: drop the heavy output bodies
			out = append(out, meta)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Created != out[j].Created {
			return out[i].Created > out[j].Created
		}
		return out[i].ID < out[j].ID
	})
	return out, nil
}

// --- raw `script` capture logs -------------------------------------------------
//
// A raw stream mixes the visible text with the escape sequences ZLE uses to draw
// it. renderLine (below) consumes those per physical line, including the string-
// type escapes — DCS (ESC P), SOS (ESC X), PM (ESC ^), APC (ESC _) and the
// screen/tmux window-title form (ESC k … ESC \) that zsh emits carrying the typed
// command — so the title text never leaks onto the next line's output (the
// `kpdsession\###POMDOCK…` contamination guarded by TestSessionMarkerContaminated).

var scriptHeaderRe = regexp.MustCompile(`(?m)^Script (started|done) on .*$`)

// cleanLog turns a raw `script` typescript into readable text. A `script` log
// is a raw terminal byte stream: zsh's line editor (ZLE) draws the prompt and
// the command you type using carriage returns, backspaces and cursor-movement
// escapes, redrawing the same physical line many times for syntax highlighting
// and autosuggestions. Taking only the text after the last \r (the old approach)
// discarded the whole prompt+command line — because ZLE ends it with a bare \r —
// so every typed command vanished and recordings held only prompts and output.
//
// Instead we replay each physical line through a tiny cursor model (renderLine):
// printable runes overwrite at the cursor, \r/\b and the horizontal cursor CSIs
// move it, and escape sequences are consumed. This reconstructs "❯ nmap -sV x"
// as it finally appeared on screen, which is what parsePastedLog needs.
func cleanLog(raw string) string {
	raw = strings.ReplaceAll(raw, "\r\n", "\n")
	var b strings.Builder
	for _, line := range strings.Split(raw, "\n") {
		b.WriteString(renderLine(line))
		b.WriteByte('\n')
	}
	// drop script's own header/footer lines
	return scriptHeaderRe.ReplaceAllString(b.String(), "")
}

// renderLine replays one physical line of a raw terminal stream into the visible
// text it would leave on that row. It honours the cursor-affecting controls ZLE
// actually uses (\r, \b, and CSI cursor-forward/back/column plus erase-line) and
// swallows every other escape sequence (SGR colours, OSC/DCS/title strings, mode
// toggles) without emitting it. Vertical moves are out of scope — each physical
// line is handled independently — which is fine for prompts, commands and normal
// command output; only full-screen TUIs (less, htop) render imperfectly, and
// those were never usefully greppable from a transcript anyway.
func renderLine(line string) string {
	var buf []rune
	col := 0
	put := func(r rune) {
		for col > len(buf) {
			buf = append(buf, ' ')
		}
		if col < len(buf) {
			buf[col] = r
		} else {
			buf = append(buf, r)
		}
		col++
	}
	rs := []rune(line)
	for i := 0; i < len(rs); i++ {
		c := rs[i]
		switch {
		case c == '\r':
			col = 0
		case c == '\b':
			if col > 0 {
				col--
			}
		case c == '\a', c == '\t': // bell ignored; tab handled crudely as a space
			if c == '\t' {
				put(' ')
			}
		case c == 0x1b:
			i += escLen(rs[i:], &buf, &col)
		case c < 0x20: // other C0 controls: drop
		default:
			put(c)
		}
	}
	// trim trailing spaces the redraws left behind
	for len(buf) > 0 && buf[len(buf)-1] == ' ' {
		buf = buf[:len(buf)-1]
	}
	return string(buf)
}

// escLen consumes an escape sequence beginning at rs[0] (which is ESC), applying
// the few that move the cursor or erase, and returns how many runes AFTER the
// ESC were consumed (so the caller's loop index advances by exactly that much).
func escLen(rs []rune, buf *[]rune, col *int) int {
	if len(rs) < 2 {
		return 0
	}
	switch rs[1] {
	case '[': // CSI: ESC [ params intermediates final
		j := 2
		start := j
		for j < len(rs) && (rs[j] == ';' || rs[j] == '?' || (rs[j] >= '0' && rs[j] <= '9')) {
			j++
		}
		params := string(rs[start:j])
		for j < len(rs) && rs[j] >= 0x20 && rs[j] <= 0x2f { // intermediates
			j++
		}
		if j >= len(rs) {
			return j - 1
		}
		final := rs[j]
		n := 1
		if v, ok := firstParam(params); ok {
			n = v
		}
		switch final {
		case 'C': // cursor forward
			*col += n
		case 'D': // cursor back
			*col -= n
			if *col < 0 {
				*col = 0
			}
		case 'G': // cursor to column (1-based)
			*col = n - 1
			if *col < 0 {
				*col = 0
			}
		case 'K': // erase in line: 0/none = to end (truncate at cursor); 1/2 = clear line
			if !strings.HasPrefix(params, "1") && !strings.HasPrefix(params, "2") {
				if *col < len(*buf) {
					*buf = (*buf)[:*col]
				}
			} else {
				*buf = (*buf)[:0]
			}
		}
		return j
	case ']': // OSC: consume to BEL or ST (ESC \)
		return oscLen(rs)
	case 'P', 'X', '^', '_', 'k': // DCS/SOS/PM/APC/title string: to ST or BEL
		return strLen(rs)
	case '(', ')', '*', '+': // charset designation: ESC ( <one char>
		return 2
	case '=', '>', '7', '8', 'M', 'c', 'D', 'E', 'H': // single-char escapes (incl. reverse index M)
		return 1
	default:
		return 1
	}
}

// firstParam returns the first numeric parameter of a CSI param string.
func firstParam(params string) (int, bool) {
	params = strings.TrimPrefix(params, "?")
	if i := strings.IndexByte(params, ';'); i >= 0 {
		params = params[:i]
	}
	if params == "" {
		return 0, false
	}
	n := 0
	for _, c := range params {
		if c < '0' || c > '9' {
			return 0, false
		}
		n = n*10 + int(c-'0')
	}
	return n, true
}

// oscLen consumes an OSC string (ESC ] … BEL | ST) and returns runes consumed
// after the ESC.
func oscLen(rs []rune) int {
	for j := 2; j < len(rs); j++ {
		if rs[j] == '\a' {
			return j
		}
		if rs[j] == 0x1b && j+1 < len(rs) && rs[j+1] == '\\' {
			return j + 1
		}
	}
	return len(rs) - 1
}

// strLen consumes a string-type escape (DCS/SOS/PM/APC/title: ESC X … ST | BEL)
// and returns the local index of the sequence's final rune.
func strLen(rs []rune) int {
	for j := 2; j < len(rs); j++ {
		if rs[j] == '\a' {
			return j
		}
		if rs[j] == 0x1b && j+1 < len(rs) && rs[j+1] == '\\' {
			return j + 1
		}
	}
	return len(rs) - 1
}

// sessionMarkerRe matches the sentinel printed by the `pomsession start <tag>`
// shell function, which starts a new logical session inside the running capture.
// The leading `.*?` tolerates any residual terminal-escape junk that cleanLog
// left at the start of the marker's physical line (belt-and-suspenders with
// renderLine's title-escape handling and the shell function's own leading
// newline).
var sessionMarkerRe = regexp.MustCompile(`(?m)^.*?#{3}POMDOCK-SESSION::(.*?)::(\d+)#{3}\s*$`)

type logSegment struct {
	tag   string
	epoch int64
	text  string
}

// scriptStartHeaderRe pulls the wall-clock start time out of `script`'s own
// first line, e.g. "Script started on 2026-09-14 19:07:03+02:00 [...]".
var scriptStartHeaderRe = regexp.MustCompile(`^Script started on (\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}[+-]\d{2}:\d{2})`)

// scriptStartNS returns the capture's start time in ns since epoch, or 0.
func scriptStartNS(raw string) int64 {
	m := scriptStartHeaderRe.FindStringSubmatch(raw)
	if m == nil {
		return 0
	}
	t, err := time.Parse("2006-01-02 15:04:05-07:00", m[1])
	if err != nil {
		return 0
	}
	return t.UnixNano()
}

func loadCaptureLog(path, engagement string) ([]*Session, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	startNS := scriptStartNS(string(raw))
	clean := cleanLog(string(raw))
	stem := strings.TrimSuffix(filepath.Base(path), ".log")
	info, _ := os.Stat(path)
	fileCreated := time.Now().Unix()
	if info != nil {
		fileCreated = info.ModTime().Unix()
	}

	segments := splitSessions(clean)
	out := make([]*Session, 0, len(segments))
	for idx, seg := range segments {
		entries := parsePastedLog(seg.text)
		if len(entries) == 0 {
			if strings.TrimSpace(seg.text) == "" {
				continue
			}
			entries = []SessionEntry{{Command: "(full transcript)", Output: seg.text}}
		}
		// Floor atuin matching at this segment's start so repeated commands get
		// this session's timestamp, not their global-earliest. A tagged segment's
		// pomsession epoch is exact; otherwise fall back to the capture's start.
		floorNS := startNS
		if seg.epoch > 0 {
			floorNS = seg.epoch * int64(time.Second)
		}
		enrichTimestamps(engagement, entries, "", floorNS)

		created := fileCreated
		date := time.Unix(fileCreated, 0).In(berlinLoc).Format("2006-01-02")
		if seg.epoch > 0 {
			created = seg.epoch
			date = time.Unix(seg.epoch, 0).In(berlinLoc).Format("2006-01-02")
		}
		for _, e := range entries {
			if e.TS > 0 {
				date = time.Unix(0, e.TS).In(berlinLoc).Format("2006-01-02")
				break
			}
		}
		id := stem
		title := "Recorded session " + stem
		if len(segments) > 1 || seg.tag != "" {
			id = fmt.Sprintf("%s#%d", stem, idx)
		}
		if seg.tag != "" {
			title = seg.tag
		}
		out = append(out, &Session{
			ID: id, Engagement: engagement, Source: "script", Tag: seg.tag,
			Title: title, Date: date, Created: created, Entries: entries,
		})
	}
	if len(out) == 0 {
		out = append(out, &Session{
			ID: stem, Engagement: engagement, Source: "script",
			Title:   "Recorded session " + stem,
			Date:    time.Unix(fileCreated, 0).In(berlinLoc).Format("2006-01-02"),
			Created: fileCreated, Entries: []SessionEntry{{Command: "(empty capture)"}},
		})
	}
	return out, nil
}

// splitSessions cuts a cleaned transcript at pdsession markers. Text before the
// first marker becomes one untagged segment; each marker begins a tagged one.
func splitSessions(text string) []logSegment {
	locs := sessionMarkerRe.FindAllStringSubmatchIndex(text, -1)
	if len(locs) == 0 {
		return []logSegment{{text: text}}
	}
	var segs []logSegment
	if pre := text[:locs[0][0]]; strings.TrimSpace(pre) != "" {
		segs = append(segs, logSegment{text: pre})
	}
	for i, m := range locs {
		tag := strings.TrimSpace(text[m[2]:m[3]])
		var epoch int64
		fmt.Sscan(text[m[4]:m[5]], &epoch)
		start := m[1] // end of the marker line
		end := len(text)
		if i+1 < len(locs) {
			end = locs[i+1][0]
		}
		segs = append(segs, logSegment{tag: tag, epoch: epoch, text: text[start:end]})
	}
	return segs
}

func readSession(path string) (*Session, error) {
	buf, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var s Session
	if err := json.Unmarshal(buf, &s); err != nil {
		return nil, err
	}
	return &s, nil
}

func getSession(engagement, id string) (*Session, error) {
	files, err := sessionFiles(engagement)
	if err != nil {
		return nil, err
	}
	for _, f := range files {
		sessions, err := loadSessions(f, engagement)
		if err != nil {
			continue
		}
		for _, s := range sessions {
			if s.ID == id {
				return s, nil
			}
		}
	}
	return nil, fmt.Errorf("session %q not found", id)
}

// LogHit is one search match inside a session's commands/output.
type LogHit struct {
	SessionID string `json:"sessionId"`
	Title     string `json:"title"`
	Index     int    `json:"index"`
	TS        int64  `json:"ts"`
	Date      string `json:"date"`
	Time      string `json:"time"`
	Command   string `json:"command"`
	Snippet   string `json:"snippet"`
	InOutput  bool   `json:"inOutput"`
}

func searchSessions(engagement, q string) ([]LogHit, error) {
	q = strings.TrimSpace(q)
	files, err := sessionFiles(engagement)
	if err != nil {
		return nil, err
	}
	lower := strings.ToLower(q)
	hits := []LogHit{}
	for _, f := range files {
		sessions, err := loadSessions(f, engagement)
		if err != nil {
			continue
		}
		for _, s := range sessions {
			for i, en := range s.Entries {
				inCmd := q != "" && strings.Contains(strings.ToLower(en.Command), lower)
				inOut := q != "" && strings.Contains(strings.ToLower(en.Output), lower)
				if !inCmd && !inOut {
					continue
				}
				hits = append(hits, LogHit{
					SessionID: s.ID, Title: s.Title, Index: i, TS: en.TS,
					Date: en.Date, Time: en.Time, Command: en.Command,
					Snippet: snippet(en.Output, q), InOutput: inOut,
				})
			}
		}
	}
	sort.Slice(hits, func(i, j int) bool { return hits[i].TS < hits[j].TS })
	return hits, nil
}

// snippet returns a short window of output around the first match of q.
func snippet(text, q string) string {
	if q == "" {
		return ""
	}
	i := strings.Index(strings.ToLower(text), strings.ToLower(q))
	if i < 0 {
		return ""
	}
	start, end := i-60, i+len(q)+80
	if start < 0 {
		start = 0
	}
	if end > len(text) {
		end = len(text)
	}
	s := strings.ReplaceAll(text[start:end], "\n", " ")
	if start > 0 {
		s = "…" + s
	}
	if end < len(text) {
		s = s + "…"
	}
	return s
}
