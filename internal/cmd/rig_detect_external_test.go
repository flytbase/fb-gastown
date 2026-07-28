package cmd

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/steveyegge/gastown/internal/config"
)

func TestDetectRigFromExternalWorktreePath(t *testing.T) {
	townRoot := t.TempDir()
	worktreePath := t.TempDir()
	nestedPath := filepath.Join(worktreePath, "src", "feature")
	if err := os.MkdirAll(nestedPath, 0755); err != nil {
		t.Fatalf("mkdir nested worktree path: %v", err)
	}

	rigsConfig := &config.RigsConfig{
		Version: 1,
		Rigs: map[string]config.RigEntry{
			"attached": {
				GitURL:       "https://example.com/repo.git",
				WorktreePath: worktreePath,
				AddedAt:      time.Now(),
			},
		},
	}
	if err := config.SaveRigsConfig(filepath.Join(townRoot, "mayor", "rigs.json"), rigsConfig); err != nil {
		t.Fatalf("SaveRigsConfig: %v", err)
	}

	if got := detectRigFromPath(townRoot, nestedPath); got != "attached" {
		t.Errorf("detectRigFromPath = %q, want attached", got)
	}
}
