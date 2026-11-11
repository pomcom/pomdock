package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWebContainerConnectionsAreReadOnlyAndScoped(t *testing.T) {
	dir := t.TempDir()
	calls := filepath.Join(dir, "calls")
	docker := `#!/bin/sh
printf '%s\n' "$*" >> "$FIXTURE_CALLS"
if [ "$1" = ps ]; then
    printf '%s\n' '{"Names":"synthetic-lab","ID":"abc123","Status":"Up 1 minute","Image":"pcm-kali-pentest","Labels":"io.pomdock.role=pentest,io.pomdock.profile=kali,io.pomdock.route=direct"}'
    exit 0
fi
case "$*" in
  *'pomdock-tmux list') printf '@1\t0\t1\trecon\tworkspace\t2\t3\n'; exit 0;;
  *'pomdock-tmux close @1') exit 0;;
esac
exit 42
`
	tmux := "#!/bin/sh\nif [ \"$1\" = list-windows ]; then exit 0; fi\nexit 42\n"
	for name, body := range map[string]string{"docker": docker, "tmux": tmux} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("FIXTURE_CALLS", calls)
	handler := testWebServer().handler()
	request := func(method, path string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, nil)
		req.Host = "127.0.0.1:7332"
		req.AddCookie(&http.Cookie{Name: sessionCookie, Value: "secret-token"})
		req.Header.Set("Origin", "http://127.0.0.1:7332")
		req.Header.Set("X-Pomdock", "1")
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, req)
		return response
	}
	containers := request("GET", "/api/containers")
	var state struct {
		Containers []containerView `json:"containers"`
	}
	if containers.Code != 200 || json.Unmarshal(containers.Body.Bytes(), &state) != nil || len(state.Containers) != 1 {
		t.Fatal(containers.Body.String())
	}
	if state.Containers[0].Connect != "pomdock docker attach 'synthetic-lab'" {
		t.Fatalf("wrong connect command: %+v", state)
	}
	response := request("GET", "/api/shells")
	var sessions struct {
		Shells []shellView `json:"shells"`
	}
	if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &sessions) != nil || len(sessions.Shells) != 1 {
		t.Fatal(response.Body.String())
	}
	shell := sessions.Shells[0]
	if shell.ID != "docker:synthetic-lab:@1" || shell.Location != "container" || shell.Clients != 2 || shell.Panes != 3 || shell.Switch != "" {
		t.Fatalf("wrong shell scope/state: %+v", shell)
	}
	if shell.Attach != "pomdock docker attach 'synthetic-lab' --window '@1'" {
		t.Fatalf("wrong window connection: %s", shell.Attach)
	}
	// A stale UI must not start a detached shell under a button labeled Show.
	if got := request("POST", "/api/containers/synthetic-lab/shell"); got.Code != http.StatusConflict {
		t.Fatal(got.Code, got.Body.String())
	}
	if got := request("POST", "/api/shells/"+url.PathEscape(shell.ID)+"/focus"); got.Code != http.StatusConflict {
		t.Fatal(got.Code, got.Body.String())
	}
	log, err := os.ReadFile(calls)
	if err != nil {
		t.Fatal(err)
	}
	for _, mutation := range []string{" ensure", " attach", " close", "new-session", "select-window"} {
		if strings.Contains(string(log), mutation) {
			t.Fatalf("reading/copying connection information mutated tmux: %s", log)
		}
	}
	// Validating a foreign/invalid target cannot reach a container close call.
	for _, id := range []string{"docker:foreign:@1", "docker:synthetic-lab:@999", "docker:synthetic-lab:unsafe"} {
		got := request("DELETE", "/api/shells/"+url.PathEscape(id))
		if got.Code != http.StatusNotFound && got.Code != http.StatusBadRequest {
			t.Fatal(id, got.Code, got.Body.String())
		}
	}
	if got := request("DELETE", "/api/shells/"+url.PathEscape(shell.ID)); got.Code != http.StatusOK {
		t.Fatal(got.Code, got.Body.String())
	}
}
