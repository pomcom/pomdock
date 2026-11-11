package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestWebShellFocusScoped(t *testing.T) {
	realTmux, err := exec.LookPath("tmux")
	if err != nil {
		t.Skip("tmux is not installed")
	}
	dir := t.TempDir()
	socket := filepath.Join(dir, "tmux.sock")
	wrapper := "#!/bin/sh\nexec " + shellQuote(realTmux) + " -S " + shellQuote(socket) + " \"$@\"\n"
	if err := os.WriteFile(filepath.Join(dir, "tmux"), []byte(wrapper), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("TMUX", "")
	run := func(args ...string) string {
		t.Helper()
		out, err := exec.Command("tmux", args...).CombinedOutput()
		if err != nil {
			t.Fatalf("tmux %v: %s (%v)", args, out, err)
		}
		return strings.TrimSpace(string(out))
	}
	first := run("-f", "/dev/null", "new-session", "-d", "-P", "-F", "#{window_id}", "-s", "pomdock", "sleep 60")
	t.Cleanup(func() { _ = exec.Command(realTmux, "-S", socket, "kill-server").Run() })
	second := run("new-window", "-d", "-P", "-F", "#{window_id}", "-t", "pomdock:", "sleep 60")
	for _, id := range []string{first, second} {
		run("set-option", "-w", "-t", id, "@pomdock_shell_kind", "docker")
		run("set-option", "-w", "-t", id, "@pomdock_shell_target", "synthetic-container")
	}
	unrelated := run("new-session", "-d", "-P", "-F", "#{window_id}", "-s", "unrelated", "sleep 60")
	handler := testWebServer().handler()
	request := func(id string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("POST", "/api/shells/"+url.PathEscape(id)+"/focus", nil)
		req.Host = "127.0.0.1:7332"
		req.AddCookie(&http.Cookie{Name: sessionCookie, Value: "secret-token"})
		req.Header.Set("Origin", "http://127.0.0.1:7332")
		req.Header.Set("X-Pomdock", "1")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec
	}
	response := request(second)
	var body struct {
		WindowID string `json:"windowId"`
		Attached bool   `json:"attached"`
	}
	if response.Code != 200 {
		t.Fatal(response.Body.String())
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.WindowID != second || body.Attached {
		t.Fatalf("wrong focus or fabricated attachment: %+v", body)
	}
	if got := run("display-message", "-p", "-t", "pomdock:", "#{window_id}"); got != second {
		t.Fatalf("focused %s, wanted %s", got, second)
	}
	for _, id := range []string{unrelated, "@99999"} {
		if rec := request(id); rec.Code != 404 {
			t.Fatalf("unmanaged window %s accepted: %d", id, rec.Code)
		}
	}
	if rec := request("unsafe"); rec.Code != 400 {
		t.Fatalf("invalid id accepted: %d", rec.Code)
	}
	failed := httptest.NewRecorder()
	testWebServer().selectShellWindow(failed, "@99999")
	if failed.Code != 409 {
		t.Fatal("failed selection reported success")
	}
}
