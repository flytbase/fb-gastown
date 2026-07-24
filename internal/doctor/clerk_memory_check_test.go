package doctor

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/steveyegge/gastown/internal/clerk"
)

func TestClerkMemoryCheckAcceptsDisabledMemory(t *testing.T) {
	townRoot := t.TempDir()
	if _, err := clerk.Provision(townRoot, false); err != nil {
		t.Fatal(err)
	}
	result := NewClerkMemoryCheck().Run(&CheckContext{TownRoot: townRoot})
	if result.Status != StatusOK {
		t.Fatalf("status = %s, message = %s", result.Status, result.Message)
	}
}

func TestClerkMemoryCheckRejectsMissingPrivateLayout(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(t.TempDir(), "config"))
	townRoot := t.TempDir()
	if _, err := clerk.Provision(townRoot, false); err != nil {
		t.Fatal(err)
	}
	policy := clerk.DefaultMemoryPolicy(townRoot, "town")
	policy.Root = filepath.Join(t.TempDir(), "missing")
	if _, err := clerk.ProvisionMemory(clerk.MemoryProvisionOptions{Policy: policy, SkipProviderInit: true}); err != nil {
		t.Fatal(err)
	}
	if err := clerk.SetMemoryPolicy(townRoot, policy); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(policy.Root); err != nil {
		t.Fatal(err)
	}
	result := NewClerkMemoryCheck().Run(&CheckContext{TownRoot: townRoot})
	if result.Status != StatusError {
		t.Fatalf("status = %s, want error", result.Status)
	}
}
