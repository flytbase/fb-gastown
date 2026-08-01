// Package assayer implements the supervised backend-assurance Town head.
package assayer

import (
	"embed"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/steveyegge/gastown/internal/atomicfile"
)

const (
	CurrentPolicyVersion = 1
	PhaseOne             = "phase1"
)

// defaultAssets contains the repository-owned Assayer policy and worker
// profiles. Go provisions and validates these assets; it does not own their
// behavioral policy.
//
//go:embed defaults/*.json
var defaultAssets embed.FS

// Limits are the portable, fail-closed worker limits for Phase 1.
type Limits struct {
	MaxConcurrentWorkers int `json:"max_concurrent_workers"`
	MaxWorkerSpawns      int `json:"max_worker_spawns"`
	DelegationDepth      int `json:"delegation_depth"`
	MaxTestAuthors       int `json:"max_test_authors"`
	SchemaCorrections    int `json:"schema_corrections_per_worker"`
}

// PostWorkerAutomation configures the native advisory handoff that runs after
// a Polecat submits completed work and before Refinery is nudged.
type PostWorkerAutomation struct {
	Enabled             bool     `json:"enabled"`
	Mode                string   `json:"mode"`
	RequireMergeRequest bool     `json:"require_merge_request"`
	EligibleRigs        []string `json:"eligible_rigs"`
}

// Automation contains native lifecycle integrations owned by Gas Town.
type Automation struct {
	PostWorker PostWorkerAutomation `json:"post_worker"`
}

// Policy contains per-Town choices. Product truth, Oracle content, secrets,
// and FlytBase-specific absolute paths are never compiled into Gas Town.
type Policy struct {
	Version          int        `json:"version"`
	Enabled          bool       `json:"enabled"`
	Phase            string     `json:"phase"`
	Domain           string     `json:"domain"`
	ContextRoots     []string   `json:"context_roots"`
	Modes            []string   `json:"modes"`
	AuthorizationTTL string     `json:"authorization_ttl"`
	Limits           Limits     `json:"limits"`
	Automation       Automation `json:"automation"`
	Authority        struct {
		HumanRunAuthorization bool `json:"human_run_authorization"`
		MayorRunAuthorization bool `json:"mayor_run_authorization"`
		ProductionWrites      bool `json:"production_writes"`
		GitMutations          bool `json:"git_mutations"`
		RealBackendExecution  bool `json:"real_backend_execution"`
	} `json:"authority"`
}

// DefaultPolicy returns the synthetic, response-only Phase 1 policy.
func DefaultPolicy() *Policy {
	var policy Policy
	data, err := defaultAssets.ReadFile("defaults/policy.json")
	if err != nil {
		panic(fmt.Sprintf("reading embedded Assayer policy: %v", err))
	}
	if err := json.Unmarshal(data, &policy); err != nil {
		panic(fmt.Sprintf("decoding embedded Assayer policy: %v", err))
	}
	return &policy
}

// Validate rejects any policy that broadens the activated Phase 1 boundary.
func (p *Policy) Validate() error {
	ceiling := DefaultPolicy()
	if p.Version != ceiling.Version {
		return fmt.Errorf("unsupported Assayer policy version %d", p.Version)
	}
	if p.Phase != ceiling.Phase || p.Domain != ceiling.Domain {
		return fmt.Errorf("Assayer Phase 1 is restricted to backend-testing")
	}
	if !p.Authority.HumanRunAuthorization {
		return fmt.Errorf("Assayer requires human run authorization")
	}
	if p.Authority.ProductionWrites || p.Authority.GitMutations || p.Authority.RealBackendExecution {
		return fmt.Errorf("Assayer Phase 1 is response-only; production, Git, and real backend execution are disabled")
	}
	if p.Limits.MaxConcurrentWorkers <= 0 || p.Limits.MaxConcurrentWorkers > ceiling.Limits.MaxConcurrentWorkers ||
		p.Limits.MaxWorkerSpawns <= 0 || p.Limits.MaxWorkerSpawns > ceiling.Limits.MaxWorkerSpawns ||
		p.Limits.DelegationDepth != ceiling.Limits.DelegationDepth ||
		p.Limits.MaxTestAuthors <= 0 || p.Limits.MaxTestAuthors > ceiling.Limits.MaxTestAuthors ||
		p.Limits.SchemaCorrections < 0 || p.Limits.SchemaCorrections > ceiling.Limits.SchemaCorrections {
		return fmt.Errorf("Assayer worker limits exceed the Phase 1 profile")
	}
	ttl, err := time.ParseDuration(p.AuthorizationTTL)
	if err != nil || ttl <= 0 {
		return fmt.Errorf("Assayer authorization TTL is invalid: %q", p.AuthorizationTTL)
	}
	ceilingTTL, err := time.ParseDuration(ceiling.AuthorizationTTL)
	if err != nil {
		return fmt.Errorf("embedded Assayer authorization TTL is invalid: %w", err)
	}
	if ttl > ceilingTTL {
		return fmt.Errorf("Assayer authorization TTL exceeds the Phase 1 profile")
	}
	if len(p.Modes) == 0 {
		return fmt.Errorf("Assayer requires at least one operating mode")
	}
	if p.Automation.PostWorker.Enabled {
		if !p.Authority.MayorRunAuthorization {
			return fmt.Errorf("Assayer post-worker automation requires Mayor run authorization")
		}
		if !contains(p.Modes, p.Automation.PostWorker.Mode) {
			return fmt.Errorf("unsupported Assayer post-worker mode %q", p.Automation.PostWorker.Mode)
		}
		if len(p.Automation.PostWorker.EligibleRigs) == 0 {
			return fmt.Errorf("Assayer post-worker automation requires at least one explicit eligible rig")
		}
		seenRigs := make(map[string]struct{}, len(p.Automation.PostWorker.EligibleRigs))
		for _, rigName := range p.Automation.PostWorker.EligibleRigs {
			rigName = strings.TrimSpace(rigName)
			if rigName == "" || rigName == "*" {
				return fmt.Errorf("Assayer post-worker eligible rigs must be explicit Town rig names")
			}
			if _, exists := seenRigs[rigName]; exists {
				return fmt.Errorf("duplicate Assayer post-worker eligible rig %q", rigName)
			}
			seenRigs[rigName] = struct{}{}
		}
	}
	allowedModes := make(map[string]struct{}, len(ceiling.Modes))
	for _, mode := range ceiling.Modes {
		allowedModes[mode] = struct{}{}
	}
	configuredModes := make(map[string]struct{}, len(p.Modes))
	for _, mode := range p.Modes {
		if _, ok := allowedModes[mode]; !ok {
			return fmt.Errorf("unsupported Assayer mode %q", mode)
		}
		if _, ok := configuredModes[mode]; ok {
			return fmt.Errorf("duplicate Assayer mode %q", mode)
		}
		configuredModes[mode] = struct{}{}
	}
	seen := make(map[string]struct{}, len(p.ContextRoots))
	for _, root := range p.ContextRoots {
		clean := filepath.Clean(strings.TrimSpace(root))
		if clean == "." || !filepath.IsAbs(clean) {
			return fmt.Errorf("Assayer context root must be absolute: %q", root)
		}
		if _, ok := seen[clean]; ok {
			return fmt.Errorf("duplicate Assayer context root %q", clean)
		}
		seen[clean] = struct{}{}
	}
	return nil
}

// LoadPolicy reads and validates assayer/assayer.json.
func LoadPolicy(townRoot string) (*Policy, error) {
	path := filepath.Join(townRoot, "assayer", "assayer.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading Assayer policy %s: %w", path, err)
	}
	var policy Policy
	if err := json.Unmarshal(data, &policy); err != nil {
		return nil, fmt.Errorf("decoding Assayer policy: %w", err)
	}
	// Policies created before Mayor trust was introduced do not contain this
	// key. Preserve explicit false while enabling the new native default for
	// legacy Towns without rewriting their policy file.
	var raw struct {
		Authority  map[string]json.RawMessage `json:"authority"`
		Automation map[string]json.RawMessage `json:"automation"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("decoding Assayer policy authority: %w", err)
	}
	if _, present := raw.Authority["mayor_run_authorization"]; !present {
		policy.Authority.MayorRunAuthorization = true
	}
	if _, present := raw.Automation["post_worker"]; !present {
		policy.Automation.PostWorker = DefaultPolicy().Automation.PostWorker
		if !policy.Authority.MayorRunAuthorization {
			policy.Automation.PostWorker.Enabled = false
		}
	}
	if err := policy.Validate(); err != nil {
		return nil, fmt.Errorf("validating Assayer policy: %w", err)
	}
	return &policy, nil
}

// EnsureHome creates the portable Assayer home and refreshes managed native
// worker definitions. Existing assayer.json choices are preserved.
func EnsureHome(townRoot, contextRoot string) error {
	home := filepath.Join(townRoot, "assayer")
	if err := os.MkdirAll(home, 0o755); err != nil {
		return fmt.Errorf("creating Assayer home: %w", err)
	}
	policyPath := filepath.Join(home, "assayer.json")
	if _, err := os.Stat(policyPath); os.IsNotExist(err) {
		policy := DefaultPolicy()
		if strings.TrimSpace(contextRoot) != "" {
			clean, cleanErr := validatedContextRoot(contextRoot)
			if cleanErr != nil {
				return cleanErr
			}
			policy.ContextRoots = []string{clean}
		}
		if err := atomicfile.WriteJSONWithPerm(policyPath, policy, 0o600); err != nil {
			return fmt.Errorf("writing Assayer policy: %w", err)
		}
	} else if err != nil {
		return err
	}
	return EnsureWorkerDefinitions(home)
}

// AddContextRoot adds one existing read-only specification root to the policy.
func AddContextRoot(townRoot, contextRoot string) error {
	clean, err := validatedContextRoot(contextRoot)
	if err != nil {
		return err
	}
	policy, err := LoadPolicy(townRoot)
	if err != nil {
		return err
	}
	for _, existing := range policy.ContextRoots {
		if filepath.Clean(existing) == clean {
			return nil
		}
	}
	policy.ContextRoots = append(policy.ContextRoots, clean)
	sort.Strings(policy.ContextRoots)
	if err := policy.Validate(); err != nil {
		return err
	}
	return atomicfile.WriteJSONWithPerm(filepath.Join(townRoot, "assayer", "assayer.json"), policy, 0o600)
}

// ConfigurePostWorkerAutomation explicitly scopes automatic backend assurance
// to named Town rigs. An empty rig set is valid only when disabling it.
func ConfigurePostWorkerAutomation(townRoot string, enabled bool, rigNames []string) error {
	policy, err := LoadPolicy(townRoot)
	if err != nil {
		return err
	}
	if enabled {
		unique := make(map[string]struct{}, len(rigNames))
		policy.Automation.PostWorker.EligibleRigs = policy.Automation.PostWorker.EligibleRigs[:0]
		for _, rigName := range rigNames {
			rigName = strings.TrimSpace(rigName)
			if rigName == "" {
				continue
			}
			if _, exists := unique[rigName]; exists {
				continue
			}
			unique[rigName] = struct{}{}
			policy.Automation.PostWorker.EligibleRigs = append(policy.Automation.PostWorker.EligibleRigs, rigName)
		}
		sort.Strings(policy.Automation.PostWorker.EligibleRigs)
	}
	policy.Automation.PostWorker.Enabled = enabled
	if err := policy.Validate(); err != nil {
		return err
	}
	return atomicfile.WriteJSONWithPerm(filepath.Join(townRoot, "assayer", "assayer.json"), policy, 0o600)
}

func validatedContextRoot(root string) (string, error) {
	clean := filepath.Clean(strings.TrimSpace(root))
	if clean == "." || !filepath.IsAbs(clean) {
		return "", fmt.Errorf("Assayer context root must be an absolute path")
	}
	info, err := os.Stat(clean)
	if err != nil {
		return "", fmt.Errorf("checking Assayer context root: %w", err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("Assayer context root is not a directory: %s", clean)
	}
	return clean, nil
}
