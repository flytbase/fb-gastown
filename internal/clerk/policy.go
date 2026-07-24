package clerk

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/steveyegge/gastown/internal/atomicfile"
)

const CurrentPolicyVersion = 2

// Policy is the native Clerk configuration persisted in the Town. Product
// defaults and validation live in this package; clerk.json contains only the
// per-Town choices that may legitimately differ between installations.
type Policy struct {
	Version  int      `json:"version"`
	Identity Identity `json:"identity"`
	Town     Town     `json:"town"`
	Routing  struct {
		Mode           string                  `json:"mode"`
		DefaultProfile string                  `json:"default_profile"`
		Profiles       map[string]RouteProfile `json:"profiles"`
	} `json:"routing"`
}

type Identity struct {
	Name                      string `json:"name"`
	BeadID                    string `json:"bead_id"`
	Home                      string `json:"home"`
	Mode                      string `json:"mode"`
	DispatchAllowed           bool   `json:"dispatch_allowed"`
	AutonomousDispatchAllowed bool   `json:"autonomous_dispatch_allowed"`
}

type Town struct {
	Root                string `json:"root"`
	LocalStateAuthority string `json:"local_state_authority"`
}

// RouteProfile selects an already configured Gas Town runtime alias.
type RouteProfile struct {
	Agent        string   `json:"agent"`
	Model        string   `json:"model"`
	RuntimeAlias string   `json:"runtime_alias"`
	Reasoning    string   `json:"reasoning"`
	Skills       []string `json:"skills"`
	Matches      []string `json:"matches,omitempty"`
}

func LoadPolicy(townRoot string) (*Policy, error) {
	path := filepath.Join(townRoot, "clerk", "clerk.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading Clerk policy %s: %w", path, err)
	}
	var policy Policy
	if err := json.Unmarshal(data, &policy); err != nil {
		return nil, fmt.Errorf("decoding Clerk policy: %w", err)
	}
	if err := policy.Validate(); err != nil {
		return nil, fmt.Errorf("validating Clerk policy: %w", err)
	}
	return &policy, nil
}

// DefaultPolicy returns a portable Clerk policy. Integrations such as ClickUp
// are configured in the Town after installation and are never compiled with a
// user's workspace IDs or credentials.
func DefaultPolicy(townRoot string) *Policy {
	policy := &Policy{
		Version: CurrentPolicyVersion,
		Identity: Identity{
			Name:                      "Town Clerk",
			BeadID:                    "hq-clerk",
			Home:                      filepath.Join(townRoot, "clerk"),
			Mode:                      "supervised_dispatch",
			DispatchAllowed:           true,
			AutonomousDispatchAllowed: false,
		},
		Town: Town{
			Root:                townRoot,
			LocalStateAuthority: "beads",
		},
	}
	policy.Routing.Mode = "supervised_spawn"
	policy.Routing.DefaultProfile = "maintenance"
	policy.Routing.Profiles = map[string]RouteProfile{
		"debugging": {
			Agent:        "claude",
			RuntimeAlias: "claude",
			Reasoning:    "high",
			Matches:      []string{"debugging", "bug"},
		},
		"feature": {
			Agent:        "codex",
			RuntimeAlias: "codex",
			Reasoning:    "high",
			Matches:      []string{"feature"},
		},
		"maintenance": {
			Agent:        "codex",
			RuntimeAlias: "codex",
			Reasoning:    "medium",
			Matches:      []string{"maintenance", "tech_debt", "refactor"},
		},
		"documentation": {
			Agent:        "claude",
			RuntimeAlias: "claude",
			Reasoning:    "medium",
			Matches:      []string{"documentation"},
		},
		"code_review": {
			Agent:        "codex",
			RuntimeAlias: "codex",
			Reasoning:    "high",
			Matches:      []string{"code_review"},
		},
	}
	return policy
}

func (p *Policy) Validate() error {
	if p.Version <= 0 || p.Version > CurrentPolicyVersion {
		return fmt.Errorf("unsupported version %d", p.Version)
	}
	if p.Identity.AutonomousDispatchAllowed {
		return fmt.Errorf("autonomous dispatch must remain disabled")
	}
	if p.Routing.DefaultProfile == "" {
		return fmt.Errorf("routing.default_profile is required")
	}
	if _, ok := p.Routing.Profiles[p.Routing.DefaultProfile]; !ok {
		return fmt.Errorf("default routing profile %q is not defined", p.Routing.DefaultProfile)
	}
	for name, profile := range p.Routing.Profiles {
		if profile.RuntimeAlias == "" && profile.Agent == "" {
			return fmt.Errorf("routing profile %q requires runtime_alias or agent", name)
		}
	}
	return nil
}

type ProvisionResult struct {
	HomeCreated   bool
	ConfigCreated bool
}

func (r ProvisionResult) Changed() int {
	changed := 0
	if r.HomeCreated {
		changed++
	}
	if r.ConfigCreated {
		changed++
	}
	return changed
}

// Provision creates the native Clerk home and default policy without
// overwriting per-Town configuration. It is safe for both gt install and
// repeated gt upgrade runs.
func Provision(townRoot string, dryRun bool) (ProvisionResult, error) {
	var result ProvisionResult
	home := filepath.Join(townRoot, "clerk")
	configPath := filepath.Join(home, "clerk.json")

	if info, err := os.Stat(home); os.IsNotExist(err) {
		result.HomeCreated = true
	} else if err != nil {
		return result, fmt.Errorf("checking Clerk home: %w", err)
	} else if !info.IsDir() {
		return result, fmt.Errorf("Clerk home exists but is not a directory: %s", home)
	}

	if info, err := os.Stat(configPath); os.IsNotExist(err) {
		result.ConfigCreated = true
	} else if err != nil {
		return result, fmt.Errorf("checking Clerk policy: %w", err)
	} else if !info.Mode().IsRegular() {
		return result, fmt.Errorf("Clerk policy exists but is not a regular file: %s", configPath)
	} else {
		if _, err := LoadPolicy(townRoot); err != nil {
			return result, err
		}
	}

	if dryRun {
		return result, nil
	}
	if err := os.MkdirAll(filepath.Join(home, "state"), 0755); err != nil {
		return result, fmt.Errorf("creating Clerk home: %w", err)
	}
	if result.ConfigCreated {
		if err := atomicfile.WriteJSONWithPerm(configPath, DefaultPolicy(townRoot), 0600); err != nil {
			return result, fmt.Errorf("writing Clerk policy: %w", err)
		}
	}
	return result, nil
}

// ResolveProfile returns a dispatch profile. RuntimeAlias is preferred because
// it can bind both the agent and exact model in Gas Town's agent registry.
func (p *Policy) ResolveProfile(name string) (RouteProfile, error) {
	if name == "" {
		name = p.Routing.DefaultProfile
	}
	profile, ok := p.Routing.Profiles[name]
	if !ok {
		return RouteProfile{}, fmt.Errorf("unknown Clerk routing profile %q", name)
	}
	return profile, nil
}
