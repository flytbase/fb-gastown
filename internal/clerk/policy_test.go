package clerk

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveProfilePrefersExplicitRuntimeAlias(t *testing.T) {
	var policy Policy
	policy.Routing.DefaultProfile = "debugging"
	policy.Routing.Profiles = map[string]RouteProfile{
		"debugging": {
			Agent:        "claude",
			Model:        "claude-opus-4-8",
			RuntimeAlias: "claude-opus-4-8",
		},
	}

	profile, err := policy.ResolveProfile("")
	if err != nil {
		t.Fatal(err)
	}
	if profile.RuntimeAlias != "claude-opus-4-8" {
		t.Fatalf("RuntimeAlias = %q", profile.RuntimeAlias)
	}
}

func TestProvisionCreatesPortableDefault(t *testing.T) {
	townRoot := t.TempDir()

	result, err := Provision(townRoot, false)
	if err != nil {
		t.Fatal(err)
	}
	if !result.HomeCreated || !result.ConfigCreated {
		t.Fatalf("Provision() = %+v, want home and config created", result)
	}

	policy, err := LoadPolicy(townRoot)
	if err != nil {
		t.Fatal(err)
	}
	if policy.Identity.Home != filepath.Join(townRoot, "clerk") {
		t.Fatalf("identity.home = %q", policy.Identity.Home)
	}
	if policy.Town.Root != townRoot {
		t.Fatalf("town.root = %q, want %q", policy.Town.Root, townRoot)
	}
	if policy.Identity.AutonomousDispatchAllowed {
		t.Fatal("default policy must not allow autonomous dispatch")
	}
}

func TestProvisionPreservesExistingPolicy(t *testing.T) {
	townRoot := t.TempDir()
	if _, err := Provision(townRoot, false); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(townRoot, "clerk", "clerk.json")
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	result, err := Provision(townRoot, false)
	if err != nil {
		t.Fatal(err)
	}
	if result.Changed() != 0 {
		t.Fatalf("second Provision() changed %d items", result.Changed())
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatal("existing policy was overwritten")
	}
}

func TestProvisionDryRunDoesNotWrite(t *testing.T) {
	townRoot := t.TempDir()

	result, err := Provision(townRoot, true)
	if err != nil {
		t.Fatal(err)
	}
	if result.Changed() != 2 {
		t.Fatalf("dry-run changes = %d, want 2", result.Changed())
	}
	if _, err := os.Stat(filepath.Join(townRoot, "clerk")); !os.IsNotExist(err) {
		t.Fatal("dry-run created Clerk home")
	}
}
