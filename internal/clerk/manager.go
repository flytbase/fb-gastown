package clerk

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/steveyegge/gastown/internal/config"
	"github.com/steveyegge/gastown/internal/constants"
	"github.com/steveyegge/gastown/internal/session"
	"github.com/steveyegge/gastown/internal/tmux"
)

var (
	ErrNotRunning     = errors.New("Clerk not running")
	ErrAlreadyRunning = errors.New("Clerk already running")
)

// Manager owns the Clerk head session. Worker lifecycle remains owned by the
// native Polecat manager reached through executeSling.
type Manager struct {
	townRoot string
}

func NewManager(townRoot string) *Manager {
	return &Manager{townRoot: townRoot}
}

func SessionName() string {
	return session.ClerkSessionName()
}

func (m *Manager) SessionName() string {
	return SessionName()
}

func (m *Manager) Start(agentOverride string) error {
	t := tmux.NewTmux()
	sessionID := m.SessionName()
	if running, err := t.HasSession(sessionID); err != nil {
		return err
	} else if running {
		return ErrAlreadyRunning
	}

	workDir := filepath.Join(m.townRoot, "clerk")
	if err := os.MkdirAll(workDir, 0755); err != nil {
		return fmt.Errorf("creating Clerk directory: %w", err)
	}
	if policy, policyErr := LoadPolicy(m.townRoot); policyErr == nil && policy.Memory.Enabled {
		runtimeConfig, _, resolveErr := config.ResolveAgentConfigWithOverride(m.townRoot, workDir, agentOverride)
		if resolveErr != nil {
			return resolveErr
		}
		if runtimeConfig.Hooks == nil || runtimeConfig.Hooks.Provider != "claude" {
			return fmt.Errorf("private Clerk memory currently requires a Claude-compatible runtime with native Clerk policy hooks")
		}
	}

	theme := tmux.ResolveSessionTheme(m.townRoot, "", constants.RoleClerk, "")
	_, err := session.StartSession(t, session.SessionConfig{
		SessionID: sessionID,
		WorkDir:   workDir,
		Role:      constants.RoleClerk,
		TownRoot:  m.townRoot,
		AgentName: "Clerk",
		Beacon: session.BeaconConfig{
			Recipient: constants.RoleClerk,
			Sender:    "human",
			Topic:     "supervised-start",
		},
		AgentOverride: agentOverride,
		Theme:         theme,
		WaitForAgent:  true,
		WaitFatal:     true,
		AutoRespawn:   true,
		AcceptBypass:  true,
	})
	return err
}

func (m *Manager) Stop() error {
	t := tmux.NewTmux()
	running, err := t.HasSession(m.SessionName())
	if err != nil {
		return err
	}
	if !running {
		return ErrNotRunning
	}
	return t.KillSessionWithProcesses(m.SessionName())
}

func (m *Manager) IsRunning() (bool, error) {
	return tmux.NewTmux().HasSession(m.SessionName())
}

func (m *Manager) Status() (*tmux.SessionInfo, error) {
	t := tmux.NewTmux()
	running, err := t.HasSession(m.SessionName())
	if err != nil {
		return nil, err
	}
	if !running {
		return nil, ErrNotRunning
	}
	return t.GetSessionInfo(m.SessionName())
}

func (m *Manager) Attach() error {
	t := tmux.NewTmux()
	running, err := t.HasSession(m.SessionName())
	if err != nil {
		return err
	}
	if !running {
		return ErrNotRunning
	}
	return t.AttachSession(m.SessionName())
}
