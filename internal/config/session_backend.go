package config

import (
	"fmt"
	"os"
	"strings"
)

const (
	// SessionBackendEnv overrides the persisted Town backend for the current
	// process tree. Gas Town sets it after resolving settings so child agents
	// and the daemon inherit one consistent transport.
	SessionBackendEnv = "GT_SESSION_BACKEND"

	SessionBackendTmux  = "tmux"
	SessionBackendHerdr = "herdr"
)

// ParseSessionBackend validates and normalizes a session backend name.
// Empty values preserve backwards compatibility by resolving to tmux.
func ParseSessionBackend(value string) (string, error) {
	switch normalized := strings.ToLower(strings.TrimSpace(value)); normalized {
	case "", SessionBackendTmux:
		return SessionBackendTmux, nil
	case SessionBackendHerdr:
		return SessionBackendHerdr, nil
	default:
		return "", fmt.Errorf("invalid session backend %q (valid: %s, %s)",
			value, SessionBackendTmux, SessionBackendHerdr)
	}
}

// ResolveSessionBackend returns the backend for a Town. An explicit environment
// value wins so a process tree can remain pinned while settings are changed;
// otherwise the persisted Town setting is used.
func ResolveSessionBackend(townRoot string) (string, error) {
	if value := strings.TrimSpace(os.Getenv(SessionBackendEnv)); value != "" {
		return ParseSessionBackend(value)
	}

	settings, err := LoadOrCreateTownSettings(TownSettingsPath(townRoot))
	if err != nil {
		return "", fmt.Errorf("loading Town session backend: %w", err)
	}
	return ParseSessionBackend(settings.SessionBackend)
}

// SessionBackendFromSettings returns the effective persisted backend without
// consulting process environment overrides.
func SessionBackendFromSettings(settings *TownSettings) string {
	if settings == nil {
		return SessionBackendTmux
	}
	backend, err := ParseSessionBackend(settings.SessionBackend)
	if err != nil {
		return SessionBackendTmux
	}
	return backend
}
