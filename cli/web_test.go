package main

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"
)

func testWebServer() *webServer {
	return &webServer{
		opts:   webOptions{tester: "LS"},
		token:  "secret-token",
		hosts:  map[string]bool{"127.0.0.1:7332": true, "localhost:7332": true},
		static: fstest.MapFS{"index.html": {Data: []byte("<!doctype html>app")}},
		jobs:   newJobRunner(),
	}
}

func TestWebGuard(t *testing.T) {
	srv := testWebServer()
	h := srv.handler()
	cookie := &http.Cookie{Name: sessionCookie, Value: "secret-token"}

	tests := []struct {
		name       string
		method     string
		target     string
		host       string
		cookie     bool
		origin     string
		pomHeader  bool
		wantStatus int
	}{
		{"rebinding host rejected", "GET", "/api/meta", "evil.example:7332", true, "", false, http.StatusMisdirectedRequest},
		{"no cookie", "GET", "/api/meta", "127.0.0.1:7332", false, "", false, http.StatusUnauthorized},
		{"wrong token", "GET", "/?token=nope", "127.0.0.1:7332", false, "", false, http.StatusUnauthorized},
		{"token bootstraps cookie", "GET", "/?token=secret-token", "127.0.0.1:7332", false, "", false, http.StatusSeeOther},
		{"cookie grants read", "GET", "/api/meta", "localhost:7332", true, "", false, http.StatusOK},
		{"spa fallback", "GET", "/containers", "127.0.0.1:7332", true, "", false, http.StatusOK},
		{"write without origin", "PUT", "/api/prefs", "127.0.0.1:7332", true, "", true, http.StatusForbidden},
		{"write cross origin", "PUT", "/api/prefs", "127.0.0.1:7332", true, "http://evil.example", true, http.StatusForbidden},
		{"write without header", "PUT", "/api/prefs", "127.0.0.1:7332", true, "http://127.0.0.1:7332", false, http.StatusForbidden},
		{"unknown api", "GET", "/api/nope", "127.0.0.1:7332", true, "", false, http.StatusNotFound},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, tt.target, strings.NewReader("{}"))
			req.Host = tt.host
			if tt.cookie {
				req.AddCookie(cookie)
			}
			if tt.origin != "" {
				req.Header.Set("Origin", tt.origin)
			}
			if tt.pomHeader {
				req.Header.Set("X-Pomdock", "1")
			}
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d (body %q)", rec.Code, tt.wantStatus, rec.Body.String())
			}
			if rec.Header().Get("Content-Security-Policy") == "" {
				t.Error("missing CSP header")
			}
		})
	}
}

func TestWebTokenRedirectStaysLocal(t *testing.T) {
	h := testWebServer().handler()
	req := httptest.NewRequest("GET", "//evil.example/?token=secret-token", nil)
	req.Host = "127.0.0.1:7332"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if loc := rec.Header().Get("Location"); loc != "/" {
		t.Fatalf("Location = %q, want /", loc)
	}
	c := rec.Result().Cookies()
	if len(c) != 1 || !c[0].HttpOnly || c[0].SameSite != http.SameSiteStrictMode {
		t.Fatalf("unexpected cookie: %+v", c)
	}
}

func TestWebPrefsRoundTrip(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	h := testWebServer().handler()
	do := func(method, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, "/api/prefs", strings.NewReader(body))
		req.Host = "127.0.0.1:7332"
		req.AddCookie(&http.Cookie{Name: sessionCookie, Value: "secret-token"})
		req.Header.Set("Origin", "http://127.0.0.1:7332")
		req.Header.Set("X-Pomdock", "1")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec
	}
	if got := do("GET", "").Body.String(); got != "null" {
		t.Fatalf("empty prefs = %q, want null", got)
	}
	if rec := do("PUT", `{not json`); rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid JSON accepted: %d", rec.Code)
	}
	if rec := do("PUT", `{"theme":"light"}`); rec.Code != http.StatusOK {
		t.Fatalf("PUT = %d %s", rec.Code, rec.Body.String())
	}
	if got := do("GET", "").Body.String(); got != `{"theme":"light"}` {
		t.Fatalf("prefs = %q", got)
	}
	p, _ := webPrefsPath()
	if info, err := os.Stat(p); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("prefs file mode: %v %v", info.Mode(), err)
	}
}

func TestWriteJSONDocAtomic(t *testing.T) {
	p := filepath.Join(t.TempDir(), "a", "doc.json")
	if err := writeJSONDoc(p, []byte(`{"v":1}`)); err != nil {
		t.Fatal(err)
	}
	if err := writeJSONDoc(p, []byte(`{"v":`)); err == nil {
		t.Fatal("expected invalid JSON to be rejected")
	}
	got, err := readJSONDoc(p)
	if err != nil || string(got) != `{"v":1}` {
		t.Fatalf("doc = %q, %v (a rejected write must leave the old one)", got, err)
	}
	entries, _ := os.ReadDir(filepath.Dir(p))
	if len(entries) != 1 {
		t.Fatalf("temp files left behind: %v", entries)
	}
}

func TestJobRunner(t *testing.T) {
	r := newJobRunner()
	release := make(chan struct{})
	job, err := r.start("stop", "acme", func(w io.Writer) error {
		_, _ = io.WriteString(w, "\x1b[32mstopping\x1b[0m\n")
		<-release
		return errors.New("boom")
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.start("start", "acme", func(io.Writer) error { return nil }); !errors.As(err, new(errJobBusy)) {
		t.Fatalf("second job on same target: err = %v, want errJobBusy", err)
	}
	close(release)
	deadline := time.Now().Add(2 * time.Second)
	for {
		got, _ := r.get(job.ID)
		if got.Status != "running" {
			if got.Status != "failed" || got.Error != "boom" || !strings.Contains(got.Output, "stopping") || strings.Contains(got.Output, "\x1b") {
				t.Fatalf("unexpected finished job: %+v", got)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("job did not finish")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestLabelValue(t *testing.T) {
	labels := "io.pomdock.role=pentest,io.pomdock.route=tor-vpn,io.pomdock.vpn-file=/home/u/vpn/x.conf"
	if got := labelValue(labels, "io.pomdock.route"); got != "tor-vpn" {
		t.Errorf("route = %q", got)
	}
	if got := labelValue(labels, "io.pomdock.vpn-file"); got != "/home/u/vpn/x.conf" {
		t.Errorf("vpn = %q", got)
	}
	if got := labelValue(labels, "missing"); got != "" {
		t.Errorf("missing = %q", got)
	}
}

func TestRouteRequestValidation(t *testing.T) {
	dir := t.TempDir()
	vpn := filepath.Join(dir, "wg.conf")
	if err := os.WriteFile(vpn, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name    string
		req     routeRequest
		wantErr bool
	}{
		{"plain", routeRequest{}, false},
		{"vpn file", routeRequest{VPNFile: vpn, Whonix: true}, false},
		{"vpn dir", routeRequest{VPNFile: dir}, true},
		{"vpn missing", routeRequest{VPNFile: filepath.Join(dir, "nope")}, true},
		{"lab", routeRequest{VPNFile: vpn, Lab: true}, false},
		{"lab without file", routeRequest{Lab: true}, true},
		{"lab with tor", routeRequest{VPNFile: vpn, Lab: true, Whonix: true}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts, err := tt.req.createOptions("acme")
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if err == nil && (opts.Whonix != tt.req.Whonix || opts.Lab != tt.req.Lab) {
				t.Errorf("route not carried over: %#v", opts)
			}
		})
	}
	for _, bad := range []string{"", "-x", "a b", "../x", strings.Repeat("a", 64)} {
		if _, err := (routeRequest{}).createOptions(bad); err == nil {
			t.Errorf("name %q accepted", bad)
		}
	}
}

func TestLoadOrCreateToken(t *testing.T) {
	dir := t.TempDir()

	t.Run("creates missing file 0600 and reuses it", func(t *testing.T) {
		path := filepath.Join(dir, "token")
		tok, err := loadOrCreateToken(path)
		if err != nil {
			t.Fatal(err)
		}
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o600 {
			t.Errorf("mode = %o, want 600", info.Mode().Perm())
		}
		again, err := loadOrCreateToken(path)
		if err != nil {
			t.Fatal(err)
		}
		if again != tok {
			t.Errorf("token changed between runs: %q != %q", again, tok)
		}
	})

	tests := []struct {
		name    string
		content string
		mode    os.FileMode
		wantErr string
	}{
		{"readable by others", strings.Repeat("a", 48), 0o644, "chmod 600"},
		{"too short", "short", 0o600, "shorter than"},
		{"trims whitespace", "  " + strings.Repeat("b", 48) + "\n", 0o600, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(dir, strings.ReplaceAll(tt.name, " ", "-"))
			if err := os.WriteFile(path, []byte(tt.content), tt.mode); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(path, tt.mode); err != nil { // WriteFile honours umask
				t.Fatal(err)
			}
			tok, err := loadOrCreateToken(path)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("err = %v, want containing %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if tok != strings.TrimSpace(tt.content) {
				t.Errorf("token = %q", tok)
			}
		})
	}

	if _, err := loadOrCreateToken(filepath.Join(dir, "missing", "token")); err == nil {
		t.Error("expected error for missing parent directory")
	}
}

func TestWebGuardStableTokenCookie(t *testing.T) {
	for _, tokenFile := range []string{"", "/some/token"} {
		srv := testWebServer()
		srv.opts.tokenFile = tokenFile
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/?token=secret-token", nil)
		req.Host = "127.0.0.1:7332"
		srv.handler().ServeHTTP(rec, req)
		cookies := rec.Result().Cookies()
		if len(cookies) != 1 {
			t.Fatalf("tokenFile=%q: got %d cookies", tokenFile, len(cookies))
		}
		persistent := cookies[0].MaxAge > 0
		if persistent != (tokenFile != "") {
			t.Errorf("tokenFile=%q: MaxAge = %d", tokenFile, cookies[0].MaxAge)
		}
	}
}
