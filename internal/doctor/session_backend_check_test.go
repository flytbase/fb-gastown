package doctor

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/steveyegge/gastown/internal/config"
)

func TestSessionBackendCheckHerdrAvailable(t *testing.T) {
	townRoot := t.TempDir()
	binDir := t.TempDir()
	herdrPath := filepath.Join(binDir, "herdr")
	if err := os.WriteFile(herdrPath, []byte("#!/bin/sh\n"), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir)

	settings := config.NewTownSettings()
	settings.SessionBackend = config.SessionBackendHerdr
	if err := config.SaveTownSettings(config.TownSettingsPath(townRoot), settings); err != nil {
		t.Fatal(err)
	}

	result := NewSessionBackendCheck().Run(&CheckContext{TownRoot: townRoot})
	if result.Status != StatusOK {
		t.Fatalf("Run() status = %v, message = %s", result.Status, result.Message)
	}
}

func TestSessionBackendCheckMissingExecutable(t *testing.T) {
	townRoot := t.TempDir()
	t.Setenv("PATH", t.TempDir())

	settings := config.NewTownSettings()
	settings.SessionBackend = config.SessionBackendHerdr
	if err := config.SaveTownSettings(config.TownSettingsPath(townRoot), settings); err != nil {
		t.Fatal(err)
	}

	result := NewSessionBackendCheck().Run(&CheckContext{TownRoot: townRoot})
	if result.Status != StatusError {
		t.Fatalf("Run() status = %v, want error", result.Status)
	}
}
