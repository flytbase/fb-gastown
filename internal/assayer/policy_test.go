package assayer

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEnsureHomeCreatesBoundedPhaseOnePolicyAndNativeWorkers(t *testing.T) {
	townRoot := t.TempDir()
	contextRoot := filepath.Join(t.TempDir(), "Backend Testing")
	if err := os.MkdirAll(contextRoot, 0o755); err != nil {
		t.Fatal(err)
	}

	if err := EnsureHome(townRoot, contextRoot); err != nil {
		t.Fatalf("EnsureHome() error = %v", err)
	}

	policy, err := LoadPolicy(townRoot)
	if err != nil {
		t.Fatalf("LoadPolicy() error = %v", err)
	}
	if policy.Phase != PhaseOne || len(policy.ContextRoots) != 1 || policy.ContextRoots[0] != contextRoot {
		t.Fatalf("unexpected policy: %#v", policy)
	}
	if policy.Limits.MaxConcurrentWorkers != 3 || policy.Limits.MaxWorkerSpawns != 6 || policy.Limits.DelegationDepth != 1 {
		t.Fatalf("unexpected limits: %#v", policy.Limits)
	}

	for _, worker := range Workers() {
		for _, path := range []string{
			filepath.Join(townRoot, "assayer", ".codex", "agents", worker.Kind+".toml"),
			filepath.Join(townRoot, "assayer", ".claude", "agents", worker.Kind+".md"),
		} {
			data, readErr := os.ReadFile(path)
			if readErr != nil {
				t.Fatalf("reading generated worker %s: %v", path, readErr)
			}
			if len(data) == 0 {
				t.Fatalf("generated worker %s is empty", path)
			}
		}
	}
}

func TestGeneratedWorkerDefinitionsPreserveCanonicalIdentityAndInstructions(t *testing.T) {
	townRoot := t.TempDir()
	if err := EnsureHome(townRoot, ""); err != nil {
		t.Fatal(err)
	}
	for _, worker := range Workers() {
		codex, err := os.ReadFile(filepath.Join(townRoot, "assayer", ".codex", "agents", worker.Kind+".toml"))
		if err != nil {
			t.Fatal(err)
		}
		claude, err := os.ReadFile(filepath.Join(townRoot, "assayer", ".claude", "agents", worker.Kind+".md"))
		if err != nil {
			t.Fatal(err)
		}
		for provider, rendered := range map[string]string{"codex": string(codex), "claude": string(claude)} {
			for _, canonical := range []string{worker.Kind, worker.Purpose, worker.Instructions} {
				if !strings.Contains(rendered, canonical) {
					t.Errorf("%s %s definition lost canonical content %q", worker.Kind, provider, canonical)
				}
			}
		}
	}
}

func TestEnsureHomePreservesExistingPolicy(t *testing.T) {
	townRoot := t.TempDir()
	home := filepath.Join(townRoot, "assayer")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(home, "assayer.json")
	original := []byte("{\"version\":1,\"phase\":\"phase1\",\"enabled\":false,\"context_roots\":[]}")
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatal(err)
	}

	if err := EnsureHome(townRoot, ""); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(original) {
		t.Fatalf("EnsureHome overwrote existing policy\ngot:  %s\nwant: %s", got, original)
	}
}

func TestWorkersAreBackendOnlyAndCannotDelegate(t *testing.T) {
	workers := Workers()
	if len(workers) != 5 {
		t.Fatalf("len(Workers()) = %d, want 5", len(workers))
	}
	for _, worker := range workers {
		if worker.MayDelegate {
			t.Fatalf("worker %s may delegate", worker.Kind)
		}
		if worker.Domain != "backend-testing" {
			t.Fatalf("worker %s domain = %q", worker.Kind, worker.Domain)
		}
	}
}
