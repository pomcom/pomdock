package main

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTrackerWorksForRecordingsWithoutAtuinDatabase(t *testing.T) {
	loot, _, _ := indexedFixture(t)
	db := filepath.Join(loot, ".atuin", "history.db")
	if err := os.Remove(db); err != nil {
		t.Fatal(err)
	}
	s := testWebServer()
	response := httptest.NewRecorder()
	s.handleRows(response, httptest.NewRequest("GET", "/api/rows?name=acme", nil))
	if response.Code != 200 {
		t.Fatal(response.Body.String())
	}
	var result struct {
		Rows []ReportRow `json:"rows"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if len(result.Rows) == 0 || result.Rows[0].Command != "printf synthetic" {
		t.Fatalf("indexed commands unavailable without Atuin: %+v", result)
	}
	response = httptest.NewRecorder()
	s.handleTrackerPut(response, httptest.NewRequest("PUT", "/api/tracker?name=acme", strings.NewReader(`{"rows":{}}`)))
	if response.Code != 200 {
		t.Fatalf("recording-only tracker cannot save: %d %s", response.Code, response.Body.String())
	}
	if _, err := os.Stat(db); !os.IsNotExist(err) {
		t.Fatal("tracker operation created an Atuin database")
	}
}

func TestInterfacesUseSharedLifecycleBackend(t *testing.T) {
	for _, test := range []struct {
		action string
		run    func(string) error
	}{
		{"start", StartContainer}, {"stop", StopContainer}, {"rm", RemoveContainerStack},
	} {
		t.Run(test.action, func(t *testing.T) {
			log := mockCLIBackend(t)
			if err := test.run("synthetic"); err != nil {
				t.Fatal(err)
			}
			got, err := os.ReadFile(log)
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != "--name\nsynthetic\n"+test.action+"\n" {
				t.Fatalf("backend arguments: %s", got)
			}
		})
	}
}
