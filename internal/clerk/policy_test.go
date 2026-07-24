package clerk

import (
	"encoding/json"
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

func TestProvisionMigratesLegacyBackendFieldsAndPreservesIntegrations(t *testing.T) {
	townRoot := t.TempDir()
	if _, err := Provision(townRoot, false); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(townRoot, "clerk", "clerk.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}
	town := raw["town"].(map[string]any)
	town["runtime_observation"] = "herdr"
	town["terminal_backend"] = "herdr"
	town["tmux_allowed"] = false
	raw["clickup"] = map[string]any{"workspace_id": "workspace-1"}
	raw["worker_management"] = map[string]any{"tmux_allowed": false}
	raw["access"] = map[string]any{
		"clerk":  map[string]any{"herdr": "supervised"},
		"worker": map[string]any{"herdr": "self_only"},
	}
	if err := os.WriteFile(path, mustJSON(t, raw), 0600); err != nil {
		t.Fatal(err)
	}

	result, err := Provision(townRoot, false)
	if err != nil {
		t.Fatal(err)
	}
	if !result.ConfigMigrated {
		t.Fatalf("Provision() = %+v, want ConfigMigrated", result)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var migrated map[string]any
	if err := json.Unmarshal(after, &migrated); err != nil {
		t.Fatal(err)
	}
	migratedTown := migrated["town"].(map[string]any)
	for _, key := range []string{"runtime_observation", "terminal_backend", "tmux_allowed"} {
		if _, exists := migratedTown[key]; exists {
			t.Fatalf("legacy town key %q was preserved", key)
		}
	}
	if migrated["clickup"].(map[string]any)["workspace_id"] != "workspace-1" {
		t.Fatal("ClickUp integration was not preserved")
	}
	if _, exists := migrated["worker_management"].(map[string]any)["tmux_allowed"]; exists {
		t.Fatal("legacy worker_management.tmux_allowed was preserved")
	}
	access := migrated["access"].(map[string]any)
	if access["clerk"].(map[string]any)["sessions"] != "supervised" {
		t.Fatal("Clerk Herdr access was not migrated to backend-neutral sessions access")
	}
	if access["worker"].(map[string]any)["sessions"] != "self_only" {
		t.Fatal("worker Herdr access was not migrated to backend-neutral sessions access")
	}
}

func TestAllowExternalSessionPreservesIntegrationsAndResolvesAlias(t *testing.T) {
	townRoot := t.TempDir()
	if _, err := Provision(townRoot, false); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(townRoot, "clerk", "clerk.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}
	raw["clickup"] = map[string]any{"workspace_id": "workspace-1"}
	if err := os.WriteFile(path, mustJSON(t, raw), 0600); err != nil {
		t.Fatal(err)
	}

	external := ExternalSession{
		Name:       "my-workers",
		Alias:      "manual",
		AllowRead:  true,
		AllowNudge: true,
	}
	if err := AllowExternalSession(townRoot, external); err != nil {
		t.Fatal(err)
	}
	policy, err := LoadPolicy(townRoot)
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := policy.ResolveExternalSession("manual", true)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Name != "my-workers" {
		t.Fatalf("resolved session = %#v", resolved)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var preserved map[string]any
	if err := json.Unmarshal(after, &preserved); err != nil {
		t.Fatal(err)
	}
	if preserved["clickup"].(map[string]any)["workspace_id"] != "workspace-1" {
		t.Fatal("AllowExternalSession removed unrelated ClickUp configuration")
	}
}

func TestResolveExternalSessionEnforcesCapabilities(t *testing.T) {
	policy := DefaultPolicy(t.TempDir())
	policy.Sessions.External = []ExternalSession{{
		Name:       "workers",
		AllowRead:  true,
		AllowNudge: false,
	}}
	if _, err := policy.ResolveExternalSession("workers", false); err != nil {
		t.Fatal(err)
	}
	if _, err := policy.ResolveExternalSession("workers", true); err == nil {
		t.Fatal("ResolveExternalSession nudge error = nil, want disabled capability")
	}
	if _, err := policy.ResolveExternalSession("unknown", false); err == nil {
		t.Fatal("ResolveExternalSession unknown error = nil, want allow-list rejection")
	}
}

func mustJSON(t *testing.T, value any) []byte {
	t.Helper()
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	return data
}
