package clerk

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRefreshExternalContextWithoutConfiguredSessions(t *testing.T) {
	townRoot := t.TempDir()
	if _, err := Provision(townRoot, false); err != nil {
		t.Fatal(err)
	}
	snapshot, err := RefreshExternalContext(townRoot, 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Sessions) != 0 {
		t.Fatalf("sessions = %#v, want none", snapshot.Sessions)
	}
	path := filepath.Join(townRoot, "clerk", "state", externalContextFile)
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0600 {
		t.Fatalf("snapshot permissions = %o, want 600", got)
	}
}

func TestFormatExternalContextIncludesStableSessionPaneAndWork(t *testing.T) {
	snapshot := &ExternalContext{
		GeneratedAt: time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC),
		Sessions: []ExternalSessionContext{{
			Name:  "manual-session",
			Alias: "flytbase",
			Workspaces: []ExternalWorkspaceContext{{
				WorkspaceID: "w1",
				Cwd:         "/Users/admin/flytbase",
				Panes: []ExternalPaneContext{{
					PaneID:        "w1:p7",
					Agent:         "claude",
					AgentStatus:   "working",
					ForegroundCwd: "/Users/admin/flytbase/frontend",
					RecentOutput:  "Investigating login failure",
				}},
			}},
		}},
	}
	output := FormatExternalContext(snapshot)
	for _, want := range []string{
		"flytbase (manual-session)",
		"`w1:p7`",
		"agent=claude status=working",
		"Investigating login failure",
		"Do not ask the human to rediscover or restate it",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("formatted context missing %q:\n%s", want, output)
		}
	}
}
