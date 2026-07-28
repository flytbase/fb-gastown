package state

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/steveyegge/gastown/internal/atomicfile"
)

const worktreeRegistryVersion = 1

// WorktreeRegistration identifies the Town and rig owning path discovery.
type WorktreeRegistration struct {
	TownRoot string `json:"town_root"`
	RigName  string `json:"rig_name"`
}

type worktreeRegistry struct {
	Version int                             `json:"version"`
	Entries map[string]WorktreeRegistration `json:"entries"`
}

// WorktreeRegistryPath returns the machine-local external worktree registry.
func WorktreeRegistryPath() string {
	return filepath.Join(StateDir(), "worktrees.json")
}

// RegisterWorktree associates a user-managed worktree with one rig and Town.
// A live registration owned by another rig is never overwritten.
func RegisterWorktree(worktreePath, townRoot, rigName string) error {
	worktreePath, err := canonicalPath(worktreePath)
	if err != nil {
		return err
	}
	townRoot, err = canonicalPath(townRoot)
	if err != nil {
		return err
	}

	registry, err := loadWorktreeRegistry()
	if err != nil {
		return err
	}
	if existing, ok := registry.Entries[worktreePath]; ok &&
		(existing.TownRoot != townRoot || existing.RigName != rigName) {
		if _, statErr := os.Stat(filepath.Join(existing.TownRoot, "mayor", "town.json")); statErr == nil {
			return fmt.Errorf("worktree is already registered to rig %q in Town %s", existing.RigName, existing.TownRoot)
		}
	}
	registry.Entries[worktreePath] = WorktreeRegistration{
		TownRoot: townRoot,
		RigName:  rigName,
	}
	if err := os.MkdirAll(StateDir(), 0755); err != nil {
		return err
	}
	return atomicfile.WriteJSONWithPerm(WorktreeRegistryPath(), registry, 0600)
}

// UnregisterWorktree removes path discovery metadata without touching the worktree.
func UnregisterWorktree(worktreePath string) error {
	worktreePath, err := canonicalPath(worktreePath)
	if err != nil {
		return err
	}
	registry, err := loadWorktreeRegistry()
	if err != nil {
		return err
	}
	delete(registry.Entries, worktreePath)
	if err := os.MkdirAll(StateDir(), 0755); err != nil {
		return err
	}
	return atomicfile.WriteJSONWithPerm(WorktreeRegistryPath(), registry, 0600)
}

// FindWorktree returns the most specific registered worktree containing path.
// Stale entries whose Town no longer exists are ignored.
func FindWorktree(path string) (WorktreeRegistration, bool) {
	path, err := canonicalPath(path)
	if err != nil {
		return WorktreeRegistration{}, false
	}
	registry, err := loadWorktreeRegistry()
	if err != nil {
		return WorktreeRegistration{}, false
	}

	var bestRoot string
	var best WorktreeRegistration
	for worktreeRoot, entry := range registry.Entries {
		relative, err := filepath.Rel(worktreeRoot, path)
		if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(os.PathSeparator)) {
			continue
		}
		if len(worktreeRoot) > len(bestRoot) {
			bestRoot = worktreeRoot
			best = entry
		}
	}
	if bestRoot == "" {
		return WorktreeRegistration{}, false
	}
	if _, err := os.Stat(filepath.Join(best.TownRoot, "mayor", "town.json")); err != nil {
		return WorktreeRegistration{}, false
	}
	return best, true
}

func loadWorktreeRegistry() (*worktreeRegistry, error) {
	registry := &worktreeRegistry{
		Version: worktreeRegistryVersion,
		Entries: make(map[string]WorktreeRegistration),
	}
	data, err := os.ReadFile(WorktreeRegistryPath())
	if os.IsNotExist(err) {
		return registry, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(data, registry); err != nil {
		return nil, err
	}
	if registry.Entries == nil {
		registry.Entries = make(map[string]WorktreeRegistration)
	}
	return registry, nil
}

func canonicalPath(path string) (string, error) {
	path, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(path)
}
