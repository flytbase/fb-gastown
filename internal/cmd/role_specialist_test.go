package cmd

import (
	"os"
	"path/filepath"
	"testing"
)

func TestGetRoleWithContextPreservesPolecatSpecialist(t *testing.T) {
	t.Setenv("GT_ROLE", "api/polecats/Worker")
	t.Setenv("GT_SPECIALIST", "surveyor")

	townRoot := t.TempDir()
	workDir := filepath.Join(townRoot, "api", "polecats", "Worker")
	if err := os.MkdirAll(workDir, 0755); err != nil {
		t.Fatal(err)
	}

	info, err := GetRoleWithContext(workDir, townRoot)
	if err != nil {
		t.Fatal(err)
	}
	if info.Role != RolePolecat || info.Specialist != "surveyor" {
		t.Fatalf("GetRoleWithContext() = %#v", info)
	}
}

func TestGetRoleWithContextRejectsUnknownSpecialist(t *testing.T) {
	t.Setenv("GT_ROLE", "api/polecats/Worker")
	t.Setenv("GT_SPECIALIST", "planner")

	if _, err := GetRoleWithContext(t.TempDir(), t.TempDir()); err == nil {
		t.Fatal("GetRoleWithContext accepted an unknown specialist kind")
	}
}
