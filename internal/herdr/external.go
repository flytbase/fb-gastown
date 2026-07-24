package herdr

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"github.com/steveyegge/gastown/internal/config"
)

// ListExternalWorkspaces lists workspaces in another named Herdr session.
func ListExternalWorkspaces(sessionName string) ([]byte, error) {
	return runExternal(sessionName, "workspace", "list")
}

// ListExternalPanes lists panes in one workspace in another named session.
func ListExternalPanes(sessionName, workspaceID string) ([]byte, error) {
	if strings.TrimSpace(workspaceID) == "" {
		return nil, fmt.Errorf("external Herdr workspace id is required")
	}
	return runExternal(sessionName, "pane", "list", "--workspace", workspaceID)
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
