package clerk

import "testing"

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
