package clerk

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

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
	Sessions struct {
		External []ExternalSession `json:"external,omitempty"`
	} `json:"sessions,omitempty"`
	Routing struct {
		Mode           string                  `json:"mode"`
		DefaultProfile string                  `json:"default_profile"`
		Profiles       map[string]RouteProfile `json:"profiles"`
	} `json:"routing"`
}

// ExternalSession is a manually managed Herdr session the Clerk may observe
// and nudge. Destructive session, workspace, tab, and pane operations are not
// exposed through the Clerk command surface.
type ExternalSession struct {
	Name       string `json:"name"`
	Alias      string `json:"alias,omitempty"`
	AllowRead  bool   `json:"allow_read"`
	AllowNudge bool   `json:"allow_nudge"`
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
	refs := make(map[string]bool)
	for _, external := range p.Sessions.External {
		if strings.TrimSpace(external.Name) == "" {
			return fmt.Errorf("external Herdr session name is required")
		}
		if refs[external.Name] {
			return fmt.Errorf("duplicate external Herdr session name or alias %q", external.Name)
		}
		refs[external.Name] = true
		if external.Alias != "" {
			if refs[external.Alias] {
				return fmt.Errorf("duplicate external Herdr session name or alias %q", external.Alias)
			}
			refs[external.Alias] = true
		}
	}
	return nil
}

// ResolveExternalSession resolves a configured session by concrete name or
// alias and verifies the requested non-destructive capability.
func (p *Policy) ResolveExternalSession(ref string, nudge bool) (ExternalSession, error) {
	for _, external := range p.Sessions.External {
		if ref != external.Name && ref != external.Alias {
			continue
		}
		if nudge && !external.AllowNudge {
			return ExternalSession{}, fmt.Errorf("nudge access is disabled for external Herdr session %q", ref)
		}
		if !nudge && !external.AllowRead {
			return ExternalSession{}, fmt.Errorf("read access is disabled for external Herdr session %q", ref)
		}
		return external, nil
	}
	return ExternalSession{}, fmt.Errorf("external Herdr session %q is not allow-listed in clerk.json", ref)
}

// AllowExternalSession adds or updates one allow-listed Herdr session while
// preserving unrelated clerk.json fields such as scoped external integrations.
func AllowExternalSession(townRoot string, external ExternalSession) error {
	path := filepath.Join(townRoot, "clerk", "clerk.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("reading Clerk policy %s: %w", path, err)
	}
	policy, err := LoadPolicy(townRoot)
	if err != nil {
		return err
	}
	updated := false
	for i := range policy.Sessions.External {
		if policy.Sessions.External[i].Name == external.Name || (external.Alias != "" && policy.Sessions.External[i].Alias == external.Alias) {
			policy.Sessions.External[i] = external
			updated = true
			break
		}
	}
	if !updated {
		policy.Sessions.External = append(policy.Sessions.External, external)
	}
	if err := policy.Validate(); err != nil {
		return err
	}

	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return fmt.Errorf("decoding Clerk policy for update: %w", err)
	}
	sessions, _ := raw["sessions"].(map[string]any)
	if sessions == nil {
		sessions = make(map[string]any)
	}
	sessions["external"] = policy.Sessions.External
	raw["sessions"] = sessions
	if err := atomicfile.WriteJSONWithPerm(path, raw, 0600); err != nil {
		return fmt.Errorf("writing Clerk external session access: %w", err)
	}
	return nil
}

type ProvisionResult struct {
	HomeCreated    bool
	ConfigCreated  bool
	ConfigMigrated bool
}

func (r ProvisionResult) Changed() int {
	changed := 0
	if r.HomeCreated {
		changed++
	}
	if r.ConfigCreated {
		changed++
	}
	if r.ConfigMigrated {
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

	var existingConfig map[string]any
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
		data, err := os.ReadFile(configPath)
		if err != nil {
			return result, fmt.Errorf("reading Clerk policy for migration: %w", err)
		}
		if err := json.Unmarshal(data, &existingConfig); err != nil {
			return result, fmt.Errorf("decoding Clerk policy for migration: %w", err)
		}
		if town, ok := existingConfig["town"].(map[string]any); ok {
			for _, key := range []string{"runtime_observation", "terminal_backend", "tmux_allowed"} {
				if _, exists := town[key]; exists {
					delete(town, key)
					result.ConfigMigrated = true
				}
			}
		}
		if workers, ok := existingConfig["worker_management"].(map[string]any); ok {
			if _, exists := workers["tmux_allowed"]; exists {
				delete(workers, "tmux_allowed")
				result.ConfigMigrated = true
			}
		}
		if access, ok := existingConfig["access"].(map[string]any); ok {
			for _, subject := range []string{"clerk", "worker"} {
				scope, ok := access[subject].(map[string]any)
				if !ok {
					continue
				}
				if value, exists := scope["herdr"]; exists {
					if _, hasGenericValue := scope["sessions"]; !hasGenericValue {
						scope["sessions"] = value
					}
					delete(scope, "herdr")
					result.ConfigMigrated = true
				}
			}
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
	} else if result.ConfigMigrated {
		if err := atomicfile.WriteJSONWithPerm(configPath, existingConfig, 0600); err != nil {
			return result, fmt.Errorf("migrating Clerk policy: %w", err)
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
