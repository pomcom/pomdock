package main

import (
	"strings"
	"testing"
)

func TestDashboardCommandCarriesWorkspaceEnvironment(t *testing.T) {
	original := repoRoot
	repoRoot = "/tmp/pom dock's"
	t.Cleanup(func() { repoRoot = original })

	command, err := dashboardCommand()
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{
		"exec 'env'",
		shellQuote(workspaceEnv + "=1"),
		shellQuote("POMDOCK_ROOT=" + repoRoot),
		"'tui'",
	} {
		if !strings.Contains(command, expected) {
			t.Fatalf("dashboard command %q does not contain %q", command, expected)
		}
	}
}

func TestWorkspaceNamesAreStable(t *testing.T) {
	if workspaceSession != "pomdock" || dashboardWindow != "dashboard" {
		t.Fatalf("unexpected workspace names: %q %q", workspaceSession, dashboardWindow)
	}
}
