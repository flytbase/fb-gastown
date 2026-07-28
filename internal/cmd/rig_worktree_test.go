package cmd

import (
	"strings"
	"testing"
)

func TestRigAddCommandExposesWorktreeFlag(t *testing.T) {
	flag := rigAddCmd.Flags().Lookup("worktree")
	if flag == nil {
		t.Fatal("gt rig add does not expose --worktree")
	}
	if !strings.Contains(flag.Usage, "persistent Mayor checkout") {
		t.Errorf("--worktree usage = %q, want persistent Mayor semantics", flag.Usage)
	}
}

func TestRunRigAddRequiresURLOrWorktree(t *testing.T) {
	previousAdopt, previousWorktree := rigAddAdopt, rigAddWorktree
	t.Cleanup(func() {
		rigAddAdopt = previousAdopt
		rigAddWorktree = previousWorktree
	})
	rigAddAdopt = false
	rigAddWorktree = ""

	err := runRigAdd(nil, []string{"testrig"})
	if err == nil || !strings.Contains(err.Error(), "--worktree") {
		t.Fatalf("runRigAdd error = %v, want missing source guidance", err)
	}
}

func TestRunRigAddRejectsAdoptWithWorktree(t *testing.T) {
	previousAdopt, previousWorktree := rigAddAdopt, rigAddWorktree
	t.Cleanup(func() {
		rigAddAdopt = previousAdopt
		rigAddWorktree = previousWorktree
	})
	rigAddAdopt = true
	rigAddWorktree = "/tmp/existing-worktree"

	err := runRigAdd(nil, []string{"testrig"})
	if err == nil || !strings.Contains(err.Error(), "cannot be used together") {
		t.Fatalf("runRigAdd error = %v, want incompatible flag error", err)
	}
}
