package cmd

import (
	"path/filepath"
	"testing"

	"github.com/steveyegge/gastown/internal/clerk"
)

func TestValidateClerkMemoryUninstallProtectsOtherTowns(t *testing.T) {
	cache := filepath.Join(t.TempDir(), "cache")
	current := filepath.Join(cache, "current-memory")
	other := filepath.Join(cache, "other-memory")
	registered := []clerk.MemoryRegistryEntry{
		{AgentID: "current", Root: current, TownRoot: "/town/current"},
		{AgentID: "other", Root: other, TownRoot: "/town/other"},
	}

	if err := validateClerkMemoryUninstall(registered, current, true, []string{cache}); err == nil {
		t.Fatal("purging one Town allowed global uninstall to erase another Town's memory")
	}
}

func TestValidateClerkMemoryUninstallAllowsOnlyExactPurgedRoot(t *testing.T) {
	cache := filepath.Join(t.TempDir(), "cache")
	current := filepath.Join(cache, "current-memory")
	registered := []clerk.MemoryRegistryEntry{
		{AgentID: "current", Root: current, TownRoot: "/town/current"},
	}

	if err := validateClerkMemoryUninstall(registered, current, true, []string{cache}); err != nil {
		t.Fatalf("exact explicitly purged root was not allowed: %v", err)
	}
}
