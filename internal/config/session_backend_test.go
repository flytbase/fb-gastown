package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseSessionBackend(t *testing.T) {
	tests := []struct {
		input   string
		want    string
		wantErr bool
	}{
		{"", SessionBackendTmux, false},
		{"tmux", SessionBackendTmux, false},
		{" TMUX ", SessionBackendTmux, false},
		{"herdr", SessionBackendHerdr, false},
		{"HERDR", SessionBackendHerdr, false},
		{"screen", "", true},
	}

	for _, test := range tests {
		got, err := ParseSessionBackend(test.input)
		if (err != nil) != test.wantErr {
			t.Fatalf("ParseSessionBackend(%q) error = %v, wantErr %v", test.input, err, test.wantErr)
		}
		if got != test.want {
			t.Fatalf("ParseSessionBackend(%q) = %q, want %q", test.input, got, test.want)
		}
	}
}

func TestResolveSessionBackendFromTownSettings(t *testing.T) {
	t.Setenv(SessionBackendEnv, "")
	townRoot := t.TempDir()
	settings := NewTownSettings()
	settings.SessionBackend = SessionBackendHerdr
	if err := SaveTownSettings(TownSettingsPath(townRoot), settings); err != nil {
		t.Fatal(err)
	}

	got, err := ResolveSessionBackend(townRoot)
	if err != nil {
		t.Fatal(err)
	}
	if got != SessionBackendHerdr {
		t.Fatalf("ResolveSessionBackend() = %q, want %q", got, SessionBackendHerdr)
	}
}

func TestResolveSessionBackendEnvironmentOverride(t *testing.T) {
	townRoot := t.TempDir()
	settings := NewTownSettings()
	settings.SessionBackend = SessionBackendTmux
	if err := SaveTownSettings(filepath.Join(townRoot, "settings", "config.json"), settings); err != nil {
		t.Fatal(err)
	}
	t.Setenv(SessionBackendEnv, SessionBackendHerdr)

	got, err := ResolveSessionBackend(townRoot)
	if err != nil {
		t.Fatal(err)
	}
	if got != SessionBackendHerdr {
		t.Fatalf("ResolveSessionBackend() = %q, want %q", got, SessionBackendHerdr)
	}
}

func TestResolveSessionBackendRejectsInvalidEnvironment(t *testing.T) {
	t.Setenv(SessionBackendEnv, "screen")
	if _, err := ResolveSessionBackend(t.TempDir()); err == nil {
		t.Fatal("ResolveSessionBackend() error = nil, want invalid backend error")
	}
}

func TestSessionBackendFromSettingsDefaultsToTmux(t *testing.T) {
	os.Unsetenv(SessionBackendEnv)
	if got := SessionBackendFromSettings(nil); got != SessionBackendTmux {
		t.Fatalf("SessionBackendFromSettings(nil) = %q, want tmux", got)
	}
	if got := SessionBackendFromSettings(NewTownSettings()); got != SessionBackendTmux {
		t.Fatalf("SessionBackendFromSettings(defaults) = %q, want tmux", got)
	}
}
