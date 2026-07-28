package doctor

import (
	"path/filepath"

	"github.com/steveyegge/gastown/internal/config"
)

func isExternalMayorWorktree(rigPath string) bool {
	cfg, err := config.LoadRigConfig(filepath.Join(rigPath, "config.json"))
	return err == nil && cfg.WorktreePath != ""
}

func rigBeadsDir(rigPath string) string {
	if isExternalMayorWorktree(rigPath) {
		return filepath.Join(rigPath, ".beads")
	}
	return filepath.Join(rigPath, "mayor", "rig", ".beads")
}

func rigBeadsWorkDir(rigPath string) string {
	if isExternalMayorWorktree(rigPath) {
		return rigPath
	}
	return filepath.Join(rigPath, "mayor", "rig")
}
