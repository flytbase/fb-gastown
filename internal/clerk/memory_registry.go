package clerk

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/steveyegge/gastown/internal/atomicfile"
	"github.com/steveyegge/gastown/internal/state"
)

const memoryRegistryVersion = 1

type MemoryRegistryEntry struct {
	AgentID  string    `json:"agent_id"`
	Root     string    `json:"root"`
	TownRoot string    `json:"town_root"`
	SeenAt   time.Time `json:"seen_at"`
}

type memoryRegistry struct {
	Version int                   `json:"version"`
	Entries []MemoryRegistryEntry `json:"entries"`
}

func memoryRegistryPath() string {
	return filepath.Join(state.ConfigDir(), "clerk-memory-roots.json")
}

// RegisterMemoryPolicy records every managed root ever selected. Old roots are
// retained so a later global uninstall cannot erase preserved or relocated
// memory merely because the current Town policy points elsewhere.
func RegisterMemoryPolicy(townRoot string, policy MemoryPolicy) error {
	if policy.Root == "" || policy.ID == "" {
		return nil
	}
	registry, err := loadMemoryRegistry()
	if err != nil {
		return err
	}
	root := filepath.Clean(policy.Root)
	townRoot = filepath.Clean(townRoot)
	for i := range registry.Entries {
		entry := &registry.Entries[i]
		if filepath.Clean(entry.Root) == root {
			entry.AgentID = policy.ID
			entry.TownRoot = townRoot
			entry.SeenAt = time.Now().UTC()
			return saveMemoryRegistry(registry)
		}
	}
	registry.Entries = append(registry.Entries, MemoryRegistryEntry{
		AgentID:  policy.ID,
		Root:     root,
		TownRoot: townRoot,
		SeenAt:   time.Now().UTC(),
	})
	return saveMemoryRegistry(registry)
}

func RegisteredMemoryPolicies() ([]MemoryRegistryEntry, error) {
	registry, err := loadMemoryRegistry()
	if err != nil {
		return nil, err
	}
	return append([]MemoryRegistryEntry(nil), registry.Entries...), nil
}

func UnregisterMemoryRoot(root string) error {
	registry, err := loadMemoryRegistry()
	if err != nil {
		return err
	}
	root = filepath.Clean(root)
	kept := registry.Entries[:0]
	for _, entry := range registry.Entries {
		if filepath.Clean(entry.Root) != root {
			kept = append(kept, entry)
		}
	}
	registry.Entries = kept
	return saveMemoryRegistry(registry)
}

func loadMemoryRegistry() (*memoryRegistry, error) {
	data, err := os.ReadFile(memoryRegistryPath())
	if os.IsNotExist(err) {
		return &memoryRegistry{Version: memoryRegistryVersion}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading Clerk memory registry: %w", err)
	}
	var registry memoryRegistry
	if err := json.Unmarshal(data, &registry); err != nil {
		return nil, fmt.Errorf("decoding Clerk memory registry: %w", err)
	}
	if registry.Version != memoryRegistryVersion {
		return nil, fmt.Errorf("unsupported Clerk memory registry version %d", registry.Version)
	}
	return &registry, nil
}

func saveMemoryRegistry(registry *memoryRegistry) error {
	dir := filepath.Dir(memoryRegistryPath())
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("creating Clerk memory registry directory: %w", err)
	}
	if err := atomicfile.WriteJSONWithPerm(memoryRegistryPath(), registry, 0600); err != nil {
		return fmt.Errorf("writing Clerk memory registry: %w", err)
	}
	return nil
}
