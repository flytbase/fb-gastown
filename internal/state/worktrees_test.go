package state

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWorktreeRegistryLifecycle(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	townRoot := t.TempDir()
	if err := os.MkdirAll(filepath.Join(townRoot, "mayor"), 0755); err != nil {
		t.Fatalf("mkdir town mayor: %v", err)
	}
	if err := os.WriteFile(filepath.Join(townRoot, "mayor", "town.json"), []byte("{}"), 0644); err != nil {
		t.Fatalf("write town marker: %v", err)
	}
	canonicalTownRoot, err := filepath.EvalSymlinks(townRoot)
	if err != nil {
		t.Fatalf("canonicalize town root: %v", err)
	}
	worktreePath := t.TempDir()
	nested := filepath.Join(worktreePath, "src", "nested")
	if err := os.MkdirAll(nested, 0755); err != nil {
		t.Fatalf("mkdir nested: %v", err)
	}

	if err := RegisterWorktree(worktreePath, townRoot, "attached"); err != nil {
		t.Fatalf("RegisterWorktree: %v", err)
	}
	registration, ok := FindWorktree(nested)
	if !ok {
		t.Fatal("FindWorktree did not resolve nested external path")
	}
	if registration.TownRoot != canonicalTownRoot {
		t.Errorf("TownRoot = %q, want %q", registration.TownRoot, canonicalTownRoot)
	}
	if registration.RigName != "attached" {
		t.Errorf("RigName = %q, want attached", registration.RigName)
	}

	otherTown := t.TempDir()
	if err := os.MkdirAll(filepath.Join(otherTown, "mayor"), 0755); err != nil {
		t.Fatalf("mkdir other town mayor: %v", err)
	}
	if err := os.WriteFile(filepath.Join(otherTown, "mayor", "town.json"), []byte("{}"), 0644); err != nil {
		t.Fatalf("write other town marker: %v", err)
	}
	if err := RegisterWorktree(worktreePath, otherTown, "other"); err == nil {
		t.Fatal("RegisterWorktree allowed the same worktree in another Town")
	}

	if err := UnregisterWorktree(worktreePath); err != nil {
		t.Fatalf("UnregisterWorktree: %v", err)
	}
	if _, ok := FindWorktree(nested); ok {
		t.Fatal("FindWorktree resolved unregistered worktree")
	}
}
