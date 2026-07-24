package hooks

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/steveyegge/gastown/internal/atomicfile"
)

// ManagedSyncResult describes a centralized settings synchronization.
type ManagedSyncResult int

const (
	ManagedSyncUnchanged ManagedSyncResult = iota
	ManagedSyncUpdated
	ManagedSyncCreated
)

// SyncManagedTarget applies the base and role overrides for a managed Claude
// settings target while preserving unknown settings fields. This is the single
// synchronization seam used by explicit hook sync and town-head startup.
func SyncManagedTarget(target Target, dryRun bool) (ManagedSyncResult, error) {
	expected, err := ComputeExpected(target.Key)
	if err != nil {
		return 0, fmt.Errorf("computing expected config: %w", err)
	}

	current, err := LoadSettings(target.Path)
	if err != nil {
		return 0, fmt.Errorf("loading current settings: %w", err)
	}

	_, statErr := os.Stat(target.Path)
	fileExists := statErr == nil
	if statErr != nil && !os.IsNotExist(statErr) {
		return 0, fmt.Errorf("checking settings target: %w", statErr)
	}

	if fileExists && HooksEqual(expected, &current.Hooks) {
		return ManagedSyncUnchanged, nil
	}
	if dryRun {
		if fileExists {
			return ManagedSyncUpdated, nil
		}
		return ManagedSyncCreated, nil
	}

	current.Hooks = *expected
	if current.EnabledPlugins == nil {
		current.EnabledPlugins = make(map[string]bool)
	}
	current.EnabledPlugins["beads@beads-marketplace"] = false

	if err := os.MkdirAll(filepath.Dir(target.Path), 0755); err != nil {
		return 0, fmt.Errorf("creating settings directory: %w", err)
	}
	data, err := MarshalSettings(current)
	if err != nil {
		return 0, fmt.Errorf("marshaling settings: %w", err)
	}
	data = append(data, '\n')
	if err := atomicfile.WriteFile(target.Path, data, 0644); err != nil {
		return 0, fmt.Errorf("writing settings: %w", err)
	}

	if fileExists {
		return ManagedSyncUpdated, nil
	}
	return ManagedSyncCreated, nil
}
