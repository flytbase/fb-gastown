package doctor

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/steveyegge/gastown/internal/config"
)

func setupExternalMayorRig(t *testing.T) (townRoot, rigPath, externalPath string) {
	t.Helper()

	townRoot = t.TempDir()
	rigPath = filepath.Join(townRoot, "attached")
	externalPath = t.TempDir()

	if out, err := exec.Command("git", "init", externalPath).CombinedOutput(); err != nil {
		t.Fatalf("git init external worktree: %v\n%s", err, out)
	}
	mayorDir := filepath.Join(rigPath, "mayor")
	if err := os.MkdirAll(mayorDir, 0755); err != nil {
		t.Fatalf("mkdir mayor: %v", err)
	}
	if err := os.Symlink(externalPath, filepath.Join(mayorDir, "rig")); err != nil {
		t.Fatalf("link external worktree: %v", err)
	}

	rigConfig := config.NewRigConfig("attached", "https://example.com/repo.git")
	rigConfig.WorktreePath = externalPath
	if err := config.SaveRigConfig(filepath.Join(rigPath, "config.json"), rigConfig); err != nil {
		t.Fatalf("save rig config: %v", err)
	}

	return townRoot, rigPath, externalPath
}

func TestStaleDoltPortCheckSkipsExternalMayorBeads(t *testing.T) {
	townRoot, rigPath, externalPath := setupExternalMayorRig(t)
	externalBeads := filepath.Join(externalPath, ".beads")
	managedBeads := filepath.Join(rigPath, ".beads")
	for _, dir := range []string{externalBeads, managedBeads} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatalf("mkdir beads: %v", err)
		}
		if err := os.WriteFile(filepath.Join(dir, "dolt-server.port"), []byte("3308\n"), 0644); err != nil {
			t.Fatalf("write port: %v", err)
		}
		if err := os.WriteFile(filepath.Join(dir, "metadata.json"), []byte(`{"dolt_server_port":3308}`), 0644); err != nil {
			t.Fatalf("write metadata: %v", err)
		}
	}
	rigsConfig := &config.RigsConfig{
		Version: 1,
		Rigs: map[string]config.RigEntry{
			"attached": {
				GitURL:       "https://example.com/repo.git",
				WorktreePath: externalPath,
				AddedAt:      time.Now(),
			},
		},
	}
	if err := config.SaveRigsConfig(filepath.Join(townRoot, "mayor", "rigs.json"), rigsConfig); err != nil {
		t.Fatalf("SaveRigsConfig: %v", err)
	}

	check := NewStaleDoltPortCheck()
	canonicalExternal, err := filepath.EvalSymlinks(externalPath)
	if err != nil {
		t.Fatalf("canonicalize external path: %v", err)
	}
	for _, path := range append(check.findPortFiles(townRoot), check.findMetadataFiles(townRoot)...) {
		resolved, err := filepath.EvalSymlinks(path)
		if err == nil && strings.HasPrefix(resolved, canonicalExternal+string(filepath.Separator)) {
			t.Errorf("stale Dolt check included external path %s", path)
		}
	}
}

func TestDoctorFixesLeaveExternalMayorWorktreeUnchanged(t *testing.T) {
	townRoot, rigPath, externalPath := setupExternalMayorRig(t)
	ctx := &CheckContext{TownRoot: townRoot, RigName: "attached"}

	excludePath := filepath.Join(externalPath, ".git", "info", "exclude")
	excludeBefore, err := os.ReadFile(excludePath)
	if err != nil {
		t.Fatalf("read initial git exclude: %v", err)
	}

	if err := os.MkdirAll(filepath.Join(externalPath, ".githooks"), 0755); err != nil {
		t.Fatalf("mkdir external hooks: %v", err)
	}
	setSparse := exec.Command("git", "-C", externalPath, "config", "core.sparseCheckout", "true")
	if out, err := setSparse.CombinedOutput(); err != nil {
		t.Fatalf("set sparse checkout: %v\n%s", err, out)
	}

	externalBeads := filepath.Join(externalPath, ".beads")
	if err := os.MkdirAll(externalBeads, 0755); err != nil {
		t.Fatalf("mkdir external beads: %v", err)
	}
	externalMarker := filepath.Join(externalBeads, "config.yaml")
	if err := os.WriteFile(externalMarker, []byte("user-owned: true\n"), 0644); err != nil {
		t.Fatalf("write external beads marker: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(rigPath, ".beads"), 0755); err != nil {
		t.Fatalf("mkdir managed beads: %v", err)
	}

	checks := []Check{
		NewGitExcludeConfiguredCheck(),
		NewHooksPathConfiguredCheck(),
		NewBeadsRedirectCheck(),
		NewSparseCheckoutCheck(),
	}
	for _, check := range checks {
		result := check.Run(ctx)
		if result.Status != StatusOK {
			t.Errorf("%s status = %s, message = %q", check.Name(), result.Status, result.Message)
		}
		if err := check.Fix(ctx); err != nil {
			t.Errorf("%s Fix: %v", check.Name(), err)
		}
	}

	excludeAfter, err := os.ReadFile(excludePath)
	if err != nil {
		t.Fatalf("read git exclude after fixes: %v", err)
	}
	if string(excludeAfter) != string(excludeBefore) {
		t.Errorf("external git exclude changed from %q to %q", excludeBefore, excludeAfter)
	}

	hooksPath := exec.Command("git", "-C", externalPath, "config", "--get", "core.hooksPath")
	if output, err := hooksPath.CombinedOutput(); err == nil || strings.TrimSpace(string(output)) != "" {
		t.Errorf("external core.hooksPath was configured: output=%q err=%v", output, err)
	}
	sparsePath := exec.Command("git", "-C", externalPath, "config", "--get", "core.sparseCheckout")
	if output, err := sparsePath.CombinedOutput(); err != nil {
		t.Fatalf("read sparse checkout: %v\n%s", err, output)
	} else if strings.TrimSpace(string(output)) != "true" {
		t.Errorf("external sparse checkout changed to %q", output)
	}
	if data, err := os.ReadFile(externalMarker); err != nil {
		t.Fatalf("external beads marker removed: %v", err)
	} else if string(data) != "user-owned: true\n" {
		t.Errorf("external beads marker changed to %q", data)
	}

	for _, clonePath := range findRigClones(rigPath) {
		if resolved, err := filepath.EvalSymlinks(clonePath); err == nil && resolved == externalPath {
			t.Errorf("all-rigs hook check included external Mayor path %s", clonePath)
		}
	}
}
