package assayer

import (
	"path/filepath"
	"testing"

	"github.com/steveyegge/gastown/internal/constants"
)

func TestManagerUsesNativeAssayerSession(t *testing.T) {
	manager := NewManager(t.TempDir())
	if got, want := manager.SessionName(), "hq-assayer"; got != want {
		t.Fatalf("SessionName() = %q, want %q", got, want)
	}
}

func TestManagerBuildsFailClosedNativeSessionConfig(t *testing.T) {
	townRoot := t.TempDir()
	config := NewManager(townRoot).sessionConfig("codex")
	if config.SessionID != "hq-assayer" || config.Role != constants.RoleAssayer {
		t.Fatalf("unexpected Assayer identity: %#v", config)
	}
	if config.WorkDir != filepath.Join(townRoot, "assayer") || config.AgentOverride != "codex" {
		t.Fatalf("unexpected Assayer runtime config: %#v", config)
	}
	if !config.WaitForAgent || !config.WaitFatal || !config.AutoRespawn || config.AcceptBypass {
		t.Fatalf("Assayer session is not fail-closed: %#v", config)
	}
}
