package config

import (
	"fmt"
	"os"
	"regexp"
	"strings"
)

const (
	// SessionBackendEnv overrides the persisted Town backend for the current
	// process tree. Gas Town sets it after resolving settings so child agents
	// and the daemon inherit one consistent transport.
	SessionBackendEnv = "GT_SESSION_BACKEND"

	// HerdrSessionEnv pins all Gas Town processes to the same named Herdr
	// session. It contains a concrete name; "current" is only a CLI selector.
	HerdrSessionEnv = "GT_HERDR_SESSION"

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

// ResolveHerdrSession returns an explicitly bound named Herdr session.
// Empty means the Herdr adapter should use its deterministic Town-owned name.
func ResolveHerdrSession(townRoot string) (string, error) {
	if value := strings.TrimSpace(os.Getenv(HerdrSessionEnv)); value != "" {
		return ParseHerdrSession(value)
	}
	settings, err := LoadOrCreateTownSettings(TownSettingsPath(townRoot))
	if err != nil {
		return "", fmt.Errorf("loading Town Herdr session: %w", err)
	}
	return ParseHerdrSession(settings.HerdrSession)
}

// ParseHerdrSession validates a concrete named-session binding. Empty and
// "managed" both select the deterministic Town-owned Herdr session.
func ParseHerdrSession(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" || strings.EqualFold(value, "managed") {
		return "", nil
	}
	if strings.EqualFold(value, "current") {
		return "", fmt.Errorf("%q is a selector, not a concrete Herdr session name", value)
	}
	if !regexp.MustCompile(`^[A-Za-z0-9._-]+$`).MatchString(value) || value == "." || value == ".." {
		return "", fmt.Errorf("invalid Herdr session name %q (use ASCII letters, numbers, '.', '_', or '-')", value)
	}
	return value, nil
}
