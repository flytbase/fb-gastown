package doltserver

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/steveyegge/gastown/internal/config"
)

func TestRigBeadsResolversPreferManagedPathForExternalWorktree(t *testing.T) {
	townRoot := t.TempDir()
	rigPath := filepath.Join(townRoot, "attached")
	externalPath := t.TempDir()

	externalBeads := filepath.Join(externalPath, ".beads")
	if err := os.MkdirAll(externalBeads, 0755); err != nil {
		t.Fatalf("mkdir external beads: %v", err)
	}
	externalMarker := filepath.Join(externalBeads, "metadata.json")
	if err := os.WriteFile(externalMarker, []byte(`{"user_owned":true}`), 0644); err != nil {
		t.Fatalf("write external marker: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(rigPath, "mayor"), 0755); err != nil {
		t.Fatalf("mkdir mayor: %v", err)
	}
	if err := os.Symlink(externalPath, filepath.Join(rigPath, "mayor", "rig")); err != nil {
		t.Fatalf("link external worktree: %v", err)
	}
	rigConfig := config.NewRigConfig("attached", "https://example.com/repo.git")
	rigConfig.WorktreePath = externalPath
	if err := config.SaveRigConfig(filepath.Join(rigPath, "config.json"), rigConfig); err != nil {
		t.Fatalf("save rig config: %v", err)
	}

	want := filepath.Join(rigPath, ".beads")
	if got := FindRigBeadsDir(townRoot, "attached"); got != want {
		t.Errorf("FindRigBeadsDir = %q, want %q", got, want)
	}
	got, err := FindOrCreateRigBeadsDir(townRoot, "attached")
	if err != nil {
		t.Fatalf("FindOrCreateRigBeadsDir: %v", err)
	}
	if got != want {
		t.Errorf("FindOrCreateRigBeadsDir = %q, want %q", got, want)
	}
	if data, err := os.ReadFile(externalMarker); err != nil {
		t.Fatalf("external metadata removed: %v", err)
	} else if string(data) != `{"user_owned":true}` {
		t.Errorf("external metadata changed to %q", data)
	}
}
