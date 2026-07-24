// Package herdr provides a narrow adapter between Gas Town's logical agent
// sessions and Herdr panes. Gas Town remains the owner of worker lifecycle and
// worktrees; Herdr is only the terminal transport.
package herdr

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gofrs/flock"
	"github.com/steveyegge/gastown/internal/atomicfile"
)

const (
	BackendEnv   = "GT_SESSION_BACKEND"
	BackendName  = "herdr"
	registryFile = "herdr-sessions.json"
)

var (
	ErrUnavailable     = errors.New("Herdr session backend is unavailable")
	ErrSessionNotFound = errors.New("Herdr session not found")
)

// Runner executes a Herdr CLI command. It is intentionally small so the
// adapter can be tested without controlling live Herdr sessions.
type Runner interface {
	Run(args ...string) ([]byte, error)
}

type execRunner struct {
	binary string
}

func (r execRunner) Run(args ...string) ([]byte, error) {
	cmd := exec.Command(r.binary, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		message := strings.TrimSpace(stderr.String())
		if message == "" {
			message = err.Error()
		}
		return nil, fmt.Errorf("herdr %s: %s", strings.Join(args, " "), message)
	}
	return out, nil
}

// Session binds a stable Gas Town session name to opaque Herdr identifiers.
type Session struct {
	Name         string            `json:"name"`
	PaneID       string            `json:"pane_id"`
	TabID        string            `json:"tab_id"`
	WorkDir      string            `json:"work_dir"`
	Env          map[string]string `json:"env,omitempty"`
	CreatedAt    time.Time         `json:"created_at"`
	LastActivity time.Time         `json:"last_activity"`
}

type registry struct {
	Sessions map[string]Session `json:"sessions"`
}

// Pane describes the subset of Herdr pane state needed by Gas Town.
type Pane struct {
	PaneID      string `json:"pane_id"`
	TabID       string `json:"tab_id"`
	Cwd         string `json:"cwd"`
	Agent       string `json:"agent"`
	AgentStatus string `json:"agent_status"`
}

// Client manages the durable Gas Town-to-Herdr session mapping.
type Client struct {
	runner      Runner
	townRoot    string
	workspaceID string
	now         func() time.Time
	mu          sync.Mutex
}

// Enabled reports whether Herdr was selected as Gas Town's session backend.
func Enabled() bool {
	return strings.EqualFold(strings.TrimSpace(os.Getenv(BackendEnv)), BackendName)
}

// NewFromEnvironment creates a production client. It fails closed unless the
// caller is itself running under Herdr and has a workspace identifier.
func NewFromEnvironment() (*Client, error) {
	return NewFromEnvironmentWithTownRoot(strings.TrimSpace(os.Getenv("GT_ROOT")))
}

// NewFromEnvironmentWithTownRoot creates a client for management commands
// which have already resolved the Town from the current directory.
func NewFromEnvironmentWithTownRoot(townRoot string) (*Client, error) {
	if os.Getenv("HERDR_ENV") != "1" {
		return nil, fmt.Errorf("%w: HERDR_ENV=1 is required", ErrUnavailable)
	}
	workspaceID := strings.TrimSpace(os.Getenv("HERDR_WORKSPACE_ID"))
	if workspaceID == "" {
		return nil, fmt.Errorf("%w: HERDR_WORKSPACE_ID is required", ErrUnavailable)
	}
	townRoot = strings.TrimSpace(townRoot)
	if townRoot == "" {
		return nil, fmt.Errorf("%w: GT_ROOT is required", ErrUnavailable)
	}
	binary := strings.TrimSpace(os.Getenv("HERDR_BIN_PATH"))
	if binary == "" {
		binary = "herdr"
	}
	return NewClient(execRunner{binary: binary}, townRoot, workspaceID), nil
}

// NewClient creates a client with an injected command runner.
func NewClient(runner Runner, townRoot, workspaceID string) *Client {
	return &Client{
		runner:      runner,
		townRoot:    townRoot,
		workspaceID: workspaceID,
		now:         time.Now,
	}
}

func (c *Client) registryPath() string {
	return filepath.Join(c.townRoot, ".runtime", registryFile)
}

func (c *Client) lockPath() string {
	return c.registryPath() + ".lock"
}

func (c *Client) withRegistry(fn func(*registry) error) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	dir := filepath.Dir(c.registryPath())
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("creating Herdr registry directory: %w", err)
	}
	if err := os.Chmod(dir, 0700); err != nil {
		return fmt.Errorf("securing Herdr registry directory: %w", err)
	}
	fileLock := flock.New(c.lockPath())
	if err := fileLock.Lock(); err != nil {
		return fmt.Errorf("locking Herdr registry: %w", err)
	}
	defer func() { _ = fileLock.Unlock() }()

	state := registry{Sessions: make(map[string]Session)}
	data, err := os.ReadFile(c.registryPath())
	if err == nil {
		if err := json.Unmarshal(data, &state); err != nil {
			return fmt.Errorf("reading Herdr registry: %w", err)
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("reading Herdr registry: %w", err)
	}
	if state.Sessions == nil {
		state.Sessions = make(map[string]Session)
	}
	if err := fn(&state); err != nil {
		return err
	}
	if err := atomicfile.WriteJSONWithPerm(c.registryPath(), state, 0600); err != nil {
		return fmt.Errorf("writing Herdr registry: %w", err)
	}
	return nil
}

func (c *Client) lookup(name string) (Session, error) {
	var result Session
	err := c.withRegistry(func(state *registry) error {
		session, ok := state.Sessions[name]
		if !ok {
			return fmt.Errorf("%w: %s", ErrSessionNotFound, name)
		}
		result = session
		return nil
	})
	return result, err
}

type tabCreateEnvelope struct {
	Result struct {
		Tab struct {
			TabID string `json:"tab_id"`
		} `json:"tab"`
		RootPane Pane `json:"root_pane"`
	} `json:"result"`
}

type paneGetEnvelope struct {
	Result struct {
		Pane Pane `json:"pane"`
	} `json:"result"`
}

// Create creates a background Herdr tab and starts the session command.
func (c *Client) Create(name, workDir, command string, env map[string]string) error {
	if c.workspaceID == "" {
		return fmt.Errorf("%w: empty workspace id", ErrUnavailable)
	}
	if _, err := c.lookup(name); err == nil {
		return fmt.Errorf("Herdr session already exists: %s", name)
	} else if !errors.Is(err, ErrSessionNotFound) {
		return err
	}

	args := []string{"tab", "create", "--workspace", c.workspaceID, "--cwd", workDir, "--label", name, "--no-focus"}
	keys := make([]string, 0, len(env))
	for key := range env {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		args = append(args, "--env", key+"="+env[key])
	}
	out, err := c.runner.Run(args...)
	if err != nil {
		return err
	}
	var created tabCreateEnvelope
	if err := json.Unmarshal(out, &created); err != nil {
		return fmt.Errorf("decoding Herdr tab creation: %w", err)
	}
	paneID := created.Result.RootPane.PaneID
	tabID := created.Result.Tab.TabID
	if paneID == "" || tabID == "" {
		return fmt.Errorf("Herdr tab creation returned empty pane or tab id")
	}
	if _, err := c.runner.Run("pane", "run", paneID, command); err != nil {
		_, _ = c.runner.Run("tab", "close", tabID)
		return fmt.Errorf("starting Herdr session command: %w", err)
	}

	now := c.now().UTC()
	return c.withRegistry(func(state *registry) error {
		state.Sessions[name] = Session{
			Name:         name,
			PaneID:       paneID,
			TabID:        tabID,
			WorkDir:      workDir,
			Env:          cloneMap(env),
			CreatedAt:    now,
			LastActivity: now,
		}
		return nil
	})
}

// Has reports whether the mapped Herdr pane still exists.
func (c *Client) Has(name string) (bool, error) {
	session, err := c.lookup(name)
	if err != nil {
		if errors.Is(err, ErrSessionNotFound) {
			return false, nil
		}
		return false, err
	}
	if _, err := c.runner.Run("pane", "get", session.PaneID); err != nil {
		return false, nil
	}
	return true, nil
}

// Close closes the entire tab created for a logical Gas Town session.
func (c *Client) Close(name string) error {
	session, err := c.lookup(name)
	if err != nil {
		if errors.Is(err, ErrSessionNotFound) {
			return nil
		}
		return err
	}
	if _, err := c.runner.Run("tab", "close", session.TabID); err != nil {
		return err
	}
	return c.withRegistry(func(state *registry) error {
		delete(state.Sessions, name)
		return nil
	})
}

// PaneID returns the opaque Herdr pane identifier for a logical session.
func (c *Client) PaneID(name string) (string, error) {
	session, err := c.lookup(name)
	if err != nil {
		return "", err
	}
	return session.PaneID, nil
}

// Get returns live pane metadata.
func (c *Client) Get(name string) (*Pane, error) {
	session, err := c.lookup(name)
	if err != nil {
		return nil, err
	}
	out, err := c.runner.Run("pane", "get", session.PaneID)
	if err != nil {
		return nil, err
	}
	var envelope paneGetEnvelope
	if err := json.Unmarshal(out, &envelope); err != nil {
		return nil, fmt.Errorf("decoding Herdr pane: %w", err)
	}
	if envelope.Result.Pane.PaneID == "" {
		return nil, fmt.Errorf("Herdr pane get returned empty pane id")
	}
	return &envelope.Result.Pane, nil
}

// Read returns recent unwrapped terminal output.
func (c *Client) Read(name string, lines int) (string, error) {
	session, err := c.lookup(name)
	if err != nil {
		return "", err
	}
	if lines <= 0 {
		lines = 100
	}
	out, err := c.runner.Run("pane", "read", session.PaneID, "--source", "recent-unwrapped", "--lines", strconv.Itoa(lines))
	if err != nil {
		return "", err
	}
	return string(out), nil
}

// Prompt submits an agent prompt atomically.
func (c *Client) Prompt(name, message string) error {
	session, err := c.lookup(name)
	if err != nil {
		return err
	}
	_, err = c.runner.Run("agent", "prompt", session.PaneID, message)
	if err == nil {
		_ = c.touch(name)
	}
	return err
}

// SendKeys sends low-level encoded keys to a pane.
func (c *Client) SendKeys(name string, keys ...string) error {
	session, err := c.lookup(name)
	if err != nil {
		return err
	}
	args := append([]string{"pane", "send-keys", session.PaneID}, keys...)
	_, err = c.runner.Run(args...)
	return err
}

// WaitReady waits until Herdr observes a usable agent lifecycle state.
func (c *Client) WaitReady(name string, timeout time.Duration) error {
	session, err := c.lookup(name)
	if err != nil {
		return err
	}
	timeoutMS := timeout.Milliseconds()
	if timeoutMS < 1 {
		timeoutMS = 1
	}
	_, err = c.runner.Run(
		"agent", "wait", session.PaneID,
		"--until", "idle",
		"--until", "done",
		"--until", "working",
		"--timeout", strconv.FormatInt(timeoutMS, 10),
	)
	return err
}

// IsAlive reports whether the pane exists and Herdr still recognizes its state.
func (c *Client) IsAlive(name string) bool {
	pane, err := c.Get(name)
	if err != nil {
		return false
	}
	switch pane.AgentStatus {
	case "", "idle", "done", "working", "blocked", "unknown":
		return true
	default:
		return false
	}
}

// IsIdle reports whether the agent is settled and ready for input.
func (c *Client) IsIdle(name string) bool {
	pane, err := c.Get(name)
	if err != nil {
		return false
	}
	return pane.AgentStatus == "idle" || pane.AgentStatus == "done"
}

// Environment returns a copy of the launch environment recorded for a session.
func (c *Client) Environment(name string) (map[string]string, error) {
	session, err := c.lookup(name)
	if err != nil {
		return nil, err
	}
	return cloneMap(session.Env), nil
}

// CreatedAt returns the registry creation time for a logical session.
func (c *Client) CreatedAt(name string) (time.Time, error) {
	session, err := c.lookup(name)
	if err != nil {
		return time.Time{}, err
	}
	return session.CreatedAt, nil
}

// SetEnvironment updates the environment recorded for future Gas Town lookups.
// Herdr launch environments are immutable for a running process.
func (c *Client) SetEnvironment(name, key, value string) error {
	return c.withRegistry(func(state *registry) error {
		session, ok := state.Sessions[name]
		if !ok {
			return fmt.Errorf("%w: %s", ErrSessionNotFound, name)
		}
		if session.Env == nil {
			session.Env = make(map[string]string)
		}
		session.Env[key] = value
		state.Sessions[name] = session
		return nil
	})
}

// List returns all mapped session names in stable order.
func (c *Client) List() ([]string, error) {
	var names []string
	err := c.withRegistry(func(state *registry) error {
		names = make([]string, 0, len(state.Sessions))
		for name := range state.Sessions {
			names = append(names, name)
		}
		sort.Strings(names)
		return nil
	})
	return names, err
}

// Focus asks Herdr to focus the tab backing a logical session.
func (c *Client) Focus(name string) error {
	session, err := c.lookup(name)
	if err != nil {
		return err
	}
	_, err = c.runner.Run("tab", "focus", session.TabID)
	return err
}

func (c *Client) touch(name string) error {
	return c.withRegistry(func(state *registry) error {
		session, ok := state.Sessions[name]
		if !ok {
			return fmt.Errorf("%w: %s", ErrSessionNotFound, name)
		}
		session.LastActivity = c.now().UTC()
		state.Sessions[name] = session
		return nil
	})
}

func cloneMap(input map[string]string) map[string]string {
	if input == nil {
		return nil
	}
	output := make(map[string]string, len(input))
	for key, value := range input {
		output[key] = value
	}
	return output
}
