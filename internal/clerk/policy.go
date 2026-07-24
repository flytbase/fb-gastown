package clerk

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// Policy is the small native subset of clerk.json needed at dispatch time.
// The complete file remains the authority for ClickUp scope and tool guards.
type Policy struct {
	Routing struct {
		DefaultProfile string                  `json:"default_profile"`
		Profiles       map[string]RouteProfile `json:"profiles"`
	} `json:"routing"`
}

// RouteProfile selects an already configured Gas Town runtime alias.
type RouteProfile struct {
	Agent        string   `json:"agent"`
	Model        string   `json:"model"`
	RuntimeAlias string   `json:"runtime_alias"`
	Reasoning    string   `json:"reasoning"`
	Skills       []string `json:"skills"`
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
	return &policy, nil
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
