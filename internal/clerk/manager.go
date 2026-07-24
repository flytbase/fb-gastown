package clerk

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/steveyegge/gastown/internal/constants"
	"github.com/steveyegge/gastown/internal/herdr"
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
	if os.Getenv("HERDR_ENV") != "1" || os.Getenv("HERDR_WORKSPACE_ID") == "" {
		return fmt.Errorf("Clerk must be started from a Herdr-managed terminal (HERDR_ENV=1 and HERDR_WORKSPACE_ID are required)")
	}

	// Select Herdr before creating the compatibility facade. The same value is
	// inherited by Witness, Refinery, and Polecats launched by Clerk.
	oldBackend := os.Getenv(herdr.BackendEnv)
	if err := os.Setenv(herdr.BackendEnv, herdr.BackendName); err != nil {
		return err
	}
	defer func() {
		if oldBackend == "" {
			_ = os.Unsetenv(herdr.BackendEnv)
		} else {
			_ = os.Setenv(herdr.BackendEnv, oldBackend)
		}
	}()

	t := tmux.NewHerdr(m.townRoot)
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
		ExtraEnv: map[string]string{
			herdr.BackendEnv: herdr.BackendName,
		},
		Theme:        theme,
		WaitForAgent: true,
		WaitFatal:    true,
		AutoRespawn:  true,
		AcceptBypass: true,
	})
	return err
}

func (m *Manager) Stop() error {
	t := tmux.NewHerdr(m.townRoot)
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
	return tmux.NewHerdr(m.townRoot).HasSession(m.SessionName())
}

func (m *Manager) Status() (*tmux.SessionInfo, error) {
	t := tmux.NewHerdr(m.townRoot)
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
	t := tmux.NewHerdr(m.townRoot)
	running, err := t.HasSession(m.SessionName())
	if err != nil {
		return err
	}
	if !running {
		return ErrNotRunning
	}
	return t.AttachSession(m.SessionName())
}
