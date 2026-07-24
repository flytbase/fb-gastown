package doctor

import (
	"fmt"
	"os/exec"

	"github.com/steveyegge/gastown/internal/config"
)

// SessionBackendCheck validates the Town-wide terminal transport and its CLI.
type SessionBackendCheck struct {
	BaseCheck
}

func NewSessionBackendCheck() *SessionBackendCheck {
	return &SessionBackendCheck{
		BaseCheck: BaseCheck{
			CheckName:        "session-backend",
			CheckDescription: "Validate the Town session backend",
			CheckCategory:    CategoryInfrastructure,
		},
	}
}

func (c *SessionBackendCheck) Run(ctx *CheckContext) *CheckResult {
	settings, err := config.LoadOrCreateTownSettings(config.TownSettingsPath(ctx.TownRoot))
	if err != nil {
		return &CheckResult{
			Name:    c.Name(),
			Status:  StatusError,
			Message: "Could not load Town session backend",
			Details: []string{err.Error()},
		}
	}
	backend, err := config.ParseSessionBackend(settings.SessionBackend)
	if err != nil {
		return &CheckResult{
			Name:    c.Name(),
			Status:  StatusError,
			Message: err.Error(),
			FixHint: "Run 'gt config set session_backend tmux' or 'gt config set session_backend herdr'",
		}
	}
	if _, err := exec.LookPath(backend); err != nil {
		return &CheckResult{
			Name:    c.Name(),
			Status:  StatusError,
			Message: fmt.Sprintf("%s selected but executable is not on PATH", backend),
			FixHint: fmt.Sprintf("Install %s or select another backend with 'gt config set session_backend <name>'", backend),
		}
	}
	return &CheckResult{
		Name:    c.Name(),
		Status:  StatusOK,
		Message: fmt.Sprintf("%s selected and available", backend),
	}
}
