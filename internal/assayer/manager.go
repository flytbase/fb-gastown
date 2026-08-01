package assayer

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/steveyegge/gastown/internal/constants"
	"github.com/steveyegge/gastown/internal/session"
	"github.com/steveyegge/gastown/internal/tmux"
)

var (
	ErrNotRunning     = errors.New("Assayer not running")
	ErrAlreadyRunning = errors.New("Assayer already running")
)

// Manager owns the long-lived Assayer Lead session. Its LLM sub-workers are
// created only through the active host's native agent facilities.
type Manager struct {
	townRoot string
}

func NewManager(townRoot string) *Manager {
	return &Manager{townRoot: townRoot}
}

func (m *Manager) SessionName() string {
	return session.AssayerSessionName()
}

func (m *Manager) Start(agentOverride string) error {
	t := tmux.NewTmux()
	if running, err := t.HasSession(m.SessionName()); err != nil {
		return err
	} else if running {
		return ErrAlreadyRunning
	}
	home := filepath.Join(m.townRoot, "assayer")
	if err := os.MkdirAll(home, 0o755); err != nil {
		return fmt.Errorf("creating Assayer directory: %w", err)
	}
	if err := EnsureHome(m.townRoot, ""); err != nil {
		return err
	}
	policy, err := LoadPolicy(m.townRoot)
	if err != nil {
		return err
	}
	if !policy.Enabled {
		return fmt.Errorf("Assayer is disabled in assayer/assayer.json")
	}
	_, err = session.StartSession(t, m.sessionConfig(agentOverride))
	if err != nil {
		return err
	}
	if _, deliveryErr := newPostWorkerDispatcher(m.townRoot, &sessionActivationSink{townRoot: m.townRoot}).DeliverPendingAfterRestart(); deliveryErr != nil {
		fmt.Fprintf(os.Stderr, "Assayer started with pending advisory delivery errors: %v\n", deliveryErr)
	}
	return nil
}

// DeliverPending retries queued native advisory transport while the Assayer
// session remains healthy. Claimed work is recovered only by Start after a
// session restart, so active runs are never interrupted by a heartbeat.
func (m *Manager) DeliverPending() (int, error) {
	return newPostWorkerDispatcher(m.townRoot, &sessionActivationSink{townRoot: m.townRoot}).DeliverPending()
}

func (m *Manager) sessionConfig(agentOverride string) session.SessionConfig {
	home := filepath.Join(m.townRoot, "assayer")
	theme := tmux.ResolveSessionTheme(m.townRoot, "", constants.RoleAssayer, "")
	return session.SessionConfig{
		SessionID: m.SessionName(),
		WorkDir:   home,
		Role:      constants.RoleAssayer,
		TownRoot:  m.townRoot,
		AgentName: "Assayer",
		Beacon: session.BeaconConfig{
			Recipient: constants.RoleAssayer,
			Sender:    "human",
			Topic:     "supervised-start",
		},
		AgentOverride: agentOverride,
		Theme:         theme,
		WaitForAgent:  true,
		WaitFatal:     true,
		AutoRespawn:   true,
		AcceptBypass:  false,
	}
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
