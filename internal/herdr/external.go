package herdr

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"github.com/steveyegge/gastown/internal/config"
)

// ExternalWorkspace describes one workspace in a named Herdr session.
type ExternalWorkspace struct {
	WorkspaceID string `json:"workspace_id"`
	Label       string `json:"label,omitempty"`
	Cwd         string `json:"cwd,omitempty"`
}

// ExternalPane describes one pane in a named Herdr session. Pane IDs are
// meaningful only together with the session name.
type ExternalPane struct {
	PaneID        string `json:"pane_id"`
	TabID         string `json:"tab_id,omitempty"`
	Label         string `json:"label,omitempty"`
	Cwd           string `json:"cwd,omitempty"`
	ForegroundCwd string `json:"foreground_cwd,omitempty"`
	Agent         string `json:"agent,omitempty"`
	AgentStatus   string `json:"agent_status,omitempty"`
}

// NamedSession is one local persistent Herdr server namespace.
type NamedSession struct {
	Name       string `json:"name"`
	Running    bool   `json:"running"`
	SocketPath string `json:"socket_path,omitempty"`
	SessionDir string `json:"session_dir,omitempty"`
}

// GetNamedSessions discovers local Herdr namespaces. This global catalog
// command does not attach to, focus, create, or stop a session.
func GetNamedSessions() ([]NamedSession, error) {
	binary := strings.TrimSpace(os.Getenv("HERDR_BIN_PATH"))
	if binary == "" {
		binary = "herdr"
	}
	resolvedBinary, err := exec.LookPath(binary)
	if err != nil {
		return nil, fmt.Errorf("%w: herdr executable not found: %v", ErrUnavailable, err)
	}
	cmd := exec.Command(resolvedBinary, "session", "list", "--json")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		message := strings.TrimSpace(stderr.String())
		if message == "" {
			message = err.Error()
		}
		return nil, fmt.Errorf("herdr session list --json: %s", message)
	}
	return decodeNamedSessions(out)
}

func decodeNamedSessions(data []byte) ([]NamedSession, error) {
	var direct []NamedSession
	if err := json.Unmarshal(data, &direct); err == nil {
		return direct, nil
	}
	var envelope struct {
		Sessions []NamedSession `json:"sessions"`
		Result   struct {
			Sessions []NamedSession `json:"sessions"`
		} `json:"result"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil {
		return nil, fmt.Errorf("decoding Herdr session list: %w", err)
	}
	if envelope.Result.Sessions != nil {
		return envelope.Result.Sessions, nil
	}
	return envelope.Sessions, nil
}

// ListExternalWorkspaces lists workspaces in another named Herdr session.
func ListExternalWorkspaces(sessionName string) ([]byte, error) {
	return runExternal(sessionName, "workspace", "list")
}

// GetExternalWorkspaces returns decoded workspace records.
func GetExternalWorkspaces(sessionName string) ([]ExternalWorkspace, error) {
	out, err := ListExternalWorkspaces(sessionName)
	if err != nil {
		return nil, err
	}
	var envelope struct {
		Result struct {
			Workspaces []ExternalWorkspace `json:"workspaces"`
		} `json:"result"`
	}
	if err := json.Unmarshal(out, &envelope); err != nil {
		return nil, fmt.Errorf("decoding external Herdr workspaces: %w", err)
	}
	return envelope.Result.Workspaces, nil
}

// ListExternalPanes lists panes in one workspace in another named session.
func ListExternalPanes(sessionName, workspaceID string) ([]byte, error) {
	if strings.TrimSpace(workspaceID) == "" {
		return nil, fmt.Errorf("external Herdr workspace id is required")
	}
	return runExternal(sessionName, "pane", "list", "--workspace", workspaceID)
}

// GetExternalPanes returns decoded pane records for one workspace.
func GetExternalPanes(sessionName, workspaceID string) ([]ExternalPane, error) {
	out, err := ListExternalPanes(sessionName, workspaceID)
	if err != nil {
		return nil, err
	}
	var envelope struct {
		Result struct {
			Panes []ExternalPane `json:"panes"`
		} `json:"result"`
	}
	if err := json.Unmarshal(out, &envelope); err != nil {
		return nil, fmt.Errorf("decoding external Herdr panes: %w", err)
	}
	return envelope.Result.Panes, nil
}

// ReadExternalPane returns recent unwrapped output from a pane in another
// named Herdr session.
func ReadExternalPane(sessionName, paneID string, lines int) ([]byte, error) {
	if lines <= 0 {
		lines = 120
	}
	return runExternal(sessionName, "pane", "read", paneID, "--source", "recent-unwrapped", "--lines", strconv.Itoa(lines))
}

// NudgeExternalPane submits text plus Enter to a pane in another named Herdr
// session. No close, delete, stop, or arbitrary command API is exposed.
func NudgeExternalPane(sessionName, paneID, message string) ([]byte, error) {
	if strings.TrimSpace(message) == "" {
		return nil, fmt.Errorf("nudge message is required")
	}
	return runExternal(sessionName, "pane", "run", paneID, message)
}

func runExternal(sessionName string, args ...string) ([]byte, error) {
	sessionName, err := config.ParseHerdrSession(sessionName)
	if err != nil {
		return nil, err
	}
	if sessionName == "" {
		return nil, fmt.Errorf("external Herdr session name is required")
	}
	binary := strings.TrimSpace(os.Getenv("HERDR_BIN_PATH"))
	if binary == "" {
		binary = "herdr"
	}
	resolvedBinary, err := exec.LookPath(binary)
	if err != nil {
		return nil, fmt.Errorf("%w: herdr executable not found: %v", ErrUnavailable, err)
	}
	fullArgs := append([]string{"--session", sessionName}, args...)
	cmd := exec.Command(resolvedBinary, fullArgs...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		message := strings.TrimSpace(stderr.String())
		if message == "" {
			message = err.Error()
		}
		return nil, fmt.Errorf("herdr %s: %s", strings.Join(fullArgs, " "), message)
	}
	return out, nil
}
