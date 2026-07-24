package cmd

import (
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"
	"github.com/steveyegge/gastown/internal/clerk"
)

func TestResolveClerkMemoryInstallDisablePreservesPrivateIdentity(t *testing.T) {
	townRoot, existing := provisionMemoryPolicyForInstallTest(t)
	cmd := newMemoryInstallTestCommand(t)
	if err := cmd.Flags().Set("clerk-memory", "none"); err != nil {
		t.Fatal(err)
	}
	decision, err := resolveClerkMemoryInstall(cmd, townRoot, "town")
	if err != nil {
		t.Fatal(err)
	}
	if decision.Policy.Enabled {
		t.Fatal("disabled memory policy remained enabled")
	}
	if decision.Policy.Root != existing.Root || decision.Policy.ID != existing.ID {
		t.Fatalf("disable orphaned private identity: %#v, want root/id from %#v", decision.Policy, existing)
	}
}

func TestResolveClerkMemoryInstallAppliesExplicitChangesToExistingPolicy(t *testing.T) {
	townRoot, existing := provisionMemoryPolicyForInstallTest(t)
	cmd := newMemoryInstallTestCommand(t)
	newRoot := filepath.Join(t.TempDir(), "moved-memory")
	if err := cmd.Flags().Set("clerk-memory-root", newRoot); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Flags().Set("gbrain-search-mode", clerk.MemorySearchTokenMax); err != nil {
		t.Fatal(err)
	}
	decision, err := resolveClerkMemoryInstall(cmd, townRoot, "town")
	if err != nil {
		t.Fatal(err)
	}
	if !decision.Policy.Enabled || decision.Policy.Root != newRoot || decision.Policy.SearchMode != clerk.MemorySearchTokenMax {
		t.Fatalf("explicit changes ignored: %#v", decision.Policy)
	}
	if decision.Policy.ID != existing.ID {
		t.Fatalf("existing identity changed from %q to %q", existing.ID, decision.Policy.ID)
	}
}

func TestResolveClerkMemoryInstallExplicitAskPreservesDisabledPolicy(t *testing.T) {
	townRoot, policy := provisionMemoryPolicyForInstallTest(t)
	policy.Enabled = false
	if err := clerk.SetMemoryPolicy(townRoot, policy); err != nil {
		t.Fatal(err)
	}
	cmd := newMemoryInstallTestCommand(t)
	if err := cmd.Flags().Set("clerk-memory", "ask"); err != nil {
		t.Fatal(err)
	}

	decision, err := resolveClerkMemoryInstall(cmd, townRoot, "town")
	if err != nil {
		t.Fatal(err)
	}
	if !decision.Apply || decision.Policy.Enabled {
		t.Fatalf("explicit ask changed disabled stored policy: %#v", decision)
	}
}

func TestResolveClerkMemoryInstallExplicitFalseClearsSharedMemory(t *testing.T) {
	townRoot, policy := provisionMemoryPolicyForInstallTest(t)
	policy.SharedReadOnly = true
	policy.SharedRoot = filepath.Dir(policy.Root)
	if err := clerk.SetMemoryPolicy(townRoot, policy); err != nil {
		t.Fatal(err)
	}
	cmd := newMemoryInstallTestCommand(t)
	if err := cmd.Flags().Set("shared-gbrain-readonly", "false"); err != nil {
		t.Fatal(err)
	}

	decision, err := resolveClerkMemoryInstall(cmd, townRoot, "town")
	if err != nil {
		t.Fatal(err)
	}
	if decision.Policy.SharedReadOnly || decision.Policy.SharedRoot != "" {
		t.Fatalf("explicit false did not clear shared memory: %#v", decision.Policy)
	}
}

func provisionMemoryPolicyForInstallTest(t *testing.T) (string, clerk.MemoryPolicy) {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(t.TempDir(), "config"))
	townRoot := t.TempDir()
	if _, err := clerk.Provision(townRoot, false); err != nil {
		t.Fatal(err)
	}
	policy := clerk.DefaultMemoryPolicy(townRoot, "town")
	policy.Root = filepath.Join(t.TempDir(), "existing-memory")
	if _, err := clerk.ProvisionMemory(clerk.MemoryProvisionOptions{Policy: policy, SkipProviderInit: true}); err != nil {
		t.Fatal(err)
	}
	if err := clerk.SetMemoryPolicy(townRoot, policy); err != nil {
		t.Fatal(err)
	}
	return townRoot, policy
}

func newMemoryInstallTestCommand(t *testing.T) *cobra.Command {
	t.Helper()
	oldChoice, oldRoot, oldMode, oldShared := installClerkMemory, installMemoryRoot, installGBrainMode, installSharedMemory
	t.Cleanup(func() {
		installClerkMemory, installMemoryRoot, installGBrainMode, installSharedMemory = oldChoice, oldRoot, oldMode, oldShared
	})
	cmd := &cobra.Command{Use: "test"}
	cmd.Flags().StringVar(&installClerkMemory, "clerk-memory", "ask", "")
	cmd.Flags().StringVar(&installMemoryRoot, "clerk-memory-root", "", "")
	cmd.Flags().StringVar(&installGBrainMode, "gbrain-search-mode", clerk.MemorySearchConservative, "")
	cmd.Flags().BoolVar(&installSharedMemory, "shared-gbrain-readonly", false, "")
	return cmd
}
