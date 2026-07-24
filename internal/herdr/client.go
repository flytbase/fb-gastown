// Package herdr provides a narrow adapter between Gas Town's logical agent
// sessions and Herdr panes. Gas Town remains the owner of worker lifecycle and
// worktrees; Herdr is only the terminal transport.
package herdr

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gofrs/flock"
	"github.com/steveyegge/gastown/internal/atomicfile"
	"github.com/steveyegge/gastown/internal/config"
)

const (
	BackendEnv   = config.SessionBackendEnv
	BackendName  = "herdr"
	registryFile = "herdr-sessions.json"

	serverStartTimeout = 10 * time.Second
	serverPollInterval = 100 * time.Millisecond
	readyPollInterval  = 100 * time.Millisecond
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
	binary      string
	prefix      []string
	sessionName string
}

func (r execRunner) Run(args ...string) ([]byte, error) {
	fullArgs := append(append([]string(nil), r.prefix...), args...)
	cmd := exec.Command(r.binary, fullArgs...)
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

func (r execRunner) StartServer() error {
	if r.sessionName == "" {
		return fmt.Errorf("%w: managed Herdr session name is empty", ErrUnavailable)
	}
	args := []string{"--session", r.sessionName, "server"}
	cmd := exec.Command(r.binary, args...)
	cmd.Stdin = nil
	cmd.Stdout = nil
	cmd.Stderr = nil
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("starting Herdr session %q: %w", r.sessionName, err)
	}
	return cmd.Process.Release()
}

func (r execRunner) Attach() error {
	if r.sessionName == "" {
		return fmt.Errorf("%w: managed Herdr session name is empty", ErrUnavailable)
	}
	cmd := exec.Command(r.binary, "session", "attach", r.sessionName)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
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
	BackendSession string             `json:"backend_session,omitempty"`
	WorkspaceID    string             `json:"workspace_id,omitempty"`
	Sessions       map[string]Session `json:"sessions"`
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
	sessionName string
	autoStart   bool
	ownsServer  bool
	now         func() time.Time
	mu          sync.Mutex
	workspaceMu sync.Mutex
}

// Enabled reports whether Herdr was selected as Gas Town's session backend.
func Enabled() bool {
	return strings.EqualFold(strings.TrimSpace(os.Getenv(BackendEnv)), BackendName)
}

// NewFromEnvironment creates a production client for the resolved Town.
func NewFromEnvironment() (*Client, error) {
	return NewFromEnvironmentWithTownRoot(strings.TrimSpace(os.Getenv("GT_ROOT")))
}

// NewFromEnvironmentWithTownRoot creates a client for management commands
// which have already resolved the Town from the current directory. Every call
// targets either the Town's explicitly bound named Herdr session or a
// deterministic Town-owned session. The server starts lazily when the first
// agent session is created.
func NewFromEnvironmentWithTownRoot(townRoot string) (*Client, error) {
	townRoot = strings.TrimSpace(townRoot)
	if townRoot == "" {
		return nil, fmt.Errorf("%w: GT_ROOT is required", ErrUnavailable)
	}
	binary := strings.TrimSpace(os.Getenv("HERDR_BIN_PATH"))
	if binary == "" {
		binary = "herdr"
	}
	resolvedBinary, err := exec.LookPath(binary)
	if err != nil {
		return nil, fmt.Errorf("%w: herdr executable not found: %v", ErrUnavailable, err)
	}

	sessionName, err := config.ResolveHerdrSession(townRoot)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	ownsServer := sessionName == ""
	if ownsServer {
		sessionName = TownSessionName(townRoot)
	}
	client := NewClient(execRunner{
		binary:      resolvedBinary,
		prefix:      []string{"--session", sessionName},
		sessionName: sessionName,
	}, townRoot, "")
	client.sessionName = sessionName
	client.autoStart = true
	client.ownsServer = ownsServer
	return client, nil
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

var invalidSessionNameChars = regexp.MustCompile(`[^a-z0-9_-]+`)

// TownSessionName returns a stable Herdr named-session identifier for a Town.
// The path hash prevents collisions between Towns with the same directory name.
func TownSessionName(townRoot string) string {
	canonical, err := filepath.EvalSymlinks(townRoot)
	if err != nil {
		canonical, err = filepath.Abs(townRoot)
		if err != nil {
			canonical = townRoot
		}
	}
	base := strings.ToLower(filepath.Base(canonical))
	base = invalidSessionNameChars.ReplaceAllString(base, "-")
	base = strings.Trim(base, "-_")
	if base == "" {
		base = "town"
	}
	if len(base) > 40 {
		base = base[:40]
	}
	hash := sha256.Sum256([]byte(canonical))
	return fmt.Sprintf("gt-%s-%x", base, hash[:3])
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

type workspaceCreateEnvelope struct {
	Result struct {
		Workspace struct {
			WorkspaceID string `json:"workspace_id"`
		} `json:"workspace"`
	} `json:"result"`
}

type serverStarter interface {
	StartServer() error
}

type sessionAttacher interface {
	Attach() error
}

func (c *Client) workspaceLockPath() string {
	return c.registryPath() + ".workspace.lock"
}

// ensureWorkspace returns the workspace used by this Town. When gt is invoked
// outside Herdr, it lazily starts a deterministic named Herdr server and
// creates one durable Town workspace. A separate file lock prevents concurrent
// `gt up` service starts from creating multiple workspaces.
func (c *Client) ensureWorkspace() (string, error) {
	c.workspaceMu.Lock()
	defer c.workspaceMu.Unlock()

	if c.workspaceID != "" {
		return c.workspaceID, nil
	}
	if !c.autoStart {
		return "", fmt.Errorf("%w: empty workspace id", ErrUnavailable)
	}

	if err := os.MkdirAll(filepath.Dir(c.registryPath()), 0700); err != nil {
		return "", fmt.Errorf("creating Herdr runtime directory: %w", err)
	}
	workspaceLock := flock.New(c.workspaceLockPath())
	if err := workspaceLock.Lock(); err != nil {
		return "", fmt.Errorf("locking Herdr workspace creation: %w", err)
	}
	defer func() { _ = workspaceLock.Unlock() }()

	var storedWorkspace string
	if err := c.withRegistry(func(state *registry) error {
		if state.BackendSession == c.sessionName {
			storedWorkspace = state.WorkspaceID
		}
		return nil
	}); err != nil {
		return "", err
	}
	if err := c.ensureServer(); err != nil {
		return "", err
	}
	if storedWorkspace != "" {
		if _, err := c.runner.Run("workspace", "get", storedWorkspace); err == nil {
			c.workspaceID = storedWorkspace
			return storedWorkspace, nil
		}
	}

	label := "Gas Town"
	if base := filepath.Base(c.townRoot); base != "" && base != "." {
		label += ": " + base
	}
	args := []string{
		"workspace", "create",
		"--cwd", c.townRoot,
		"--label", label,
		"--no-focus",
		"--env", "GT_ROOT=" + c.townRoot,
		"--env", "GT_TOWN_ROOT=" + c.townRoot,
		"--env", BackendEnv + "=" + BackendName,
	}
	out, err := c.runner.Run(args...)
	if err != nil {
		return "", fmt.Errorf("creating Town Herdr workspace: %w", err)
	}
	var created workspaceCreateEnvelope
	if err := json.Unmarshal(out, &created); err != nil {
		return "", fmt.Errorf("decoding Herdr workspace creation: %w", err)
	}
	workspaceID := strings.TrimSpace(created.Result.Workspace.WorkspaceID)
	if workspaceID == "" {
		return "", fmt.Errorf("Herdr workspace creation returned empty workspace id")
	}

	if err := c.withRegistry(func(state *registry) error {
		state.BackendSession = c.sessionName
		state.WorkspaceID = workspaceID
		// A replacement workspace cannot contain panes from the old mapping.
		state.Sessions = make(map[string]Session)
		return nil
	}); err != nil {
		return "", err
	}
	c.workspaceID = workspaceID
	return workspaceID, nil
}

func (c *Client) ensureServer() error {
	if _, err := c.runner.Run("workspace", "list"); err == nil {
		return nil
	}
	starter, ok := c.runner.(serverStarter)
	if !ok {
		return fmt.Errorf("%w: runner cannot start a Herdr server", ErrUnavailable)
	}
	if err := starter.StartServer(); err != nil {
		return err
	}

	deadline := time.Now().Add(serverStartTimeout)
	var lastErr error
	for time.Now().Before(deadline) {
		if _, err := c.runner.Run("workspace", "list"); err == nil {
			return nil
		} else {
			lastErr = err
		}
		time.Sleep(serverPollInterval)
	}
	return fmt.Errorf("waiting for Town Herdr server: %w", lastErr)
}

// Create creates a background Herdr tab and starts the session command.
func (c *Client) Create(name, workDir, command string, env map[string]string) error {
	workspaceID, err := c.ensureWorkspace()
	if err != nil {
		return err
	}
	if _, err := c.lookup(name); err == nil {
		return fmt.Errorf("Herdr session already exists: %s", name)
	} else if !errors.Is(err, ErrSessionNotFound) {
		return err
	}

	args := []string{"tab", "create", "--workspace", workspaceID, "--cwd", workDir, "--label", name, "--no-focus"}
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
	if c.autoStart {
		if err := c.ensureServer(); err != nil {
			return false, err
		}
	}
	if _, err := c.runner.Run("pane", "get", session.PaneID); err != nil {
		if registryErr := c.withRegistry(func(state *registry) error {
			delete(state.Sessions, name)
			return nil
		}); registryErr != nil {
			return false, registryErr
		}
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
	if c.autoStart {
		if err := c.ensureServer(); err != nil {
			return err
		}
	}
	if _, err := c.runner.Run("tab", "close", session.TabID); err != nil {
		// A manually closed tab is already stopped. Prune its stale mapping;
		// preserve the error only when the pane still exists.
		if _, paneErr := c.runner.Run("pane", "get", session.PaneID); paneErr == nil {
			return err
		}
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

// SessionForPane resolves an opaque Herdr pane ID back to its stable Gas Town
// logical session name.
func (c *Client) SessionForPane(paneID string) (string, error) {
	var name string
	err := c.withRegistry(func(state *registry) error {
		for sessionName, session := range state.Sessions {
			if session.PaneID == paneID {
				name = sessionName
				return nil
			}
		}
		return fmt.Errorf("%w: pane %s", ErrSessionNotFound, paneID)
	})
	return name, err
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
	// pane run is Herdr's atomic text-plus-Enter operation for both initial
	// commands and follow-up prompts.
	_, err = c.runner.Run("pane", "run", session.PaneID, message)
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

// RestartPane replaces the tab that owns paneID and runs command in a fresh
// shell. Creating the replacement before closing the old tab makes self-handoff
// safe: the new session exists before Herdr terminates the caller's tab.
func (c *Client) RestartPane(paneID, workDir, command string) error {
	var previous Session
	if err := c.withRegistry(func(state *registry) error {
		for _, session := range state.Sessions {
			if session.PaneID == paneID {
				previous = session
				return nil
			}
		}
		return fmt.Errorf("%w: pane %s", ErrSessionNotFound, paneID)
	}); err != nil {
		return err
	}
	if workDir == "" {
		workDir = previous.WorkDir
	}
	oldTabID := previous.TabID
	workspaceID, err := c.ensureWorkspace()
	if err != nil {
		return err
	}

	args := []string{
		"tab", "create",
		"--workspace", workspaceID,
		"--cwd", workDir,
		"--label", previous.Name,
		"--no-focus",
	}
	keys := make([]string, 0, len(previous.Env))
	for key := range previous.Env {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		args = append(args, "--env", key+"="+previous.Env[key])
	}
	out, err := c.runner.Run(args...)
	if err != nil {
		return err
	}
	var created tabCreateEnvelope
	if err := json.Unmarshal(out, &created); err != nil {
		return fmt.Errorf("decoding replacement Herdr tab: %w", err)
	}
	newPaneID := created.Result.RootPane.PaneID
	newTabID := created.Result.Tab.TabID
	if newPaneID == "" || newTabID == "" {
		return fmt.Errorf("replacement Herdr tab returned empty pane or tab id")
	}
	if _, err := c.runner.Run("pane", "run", newPaneID, command); err != nil {
		_, _ = c.runner.Run("tab", "close", newTabID)
		return fmt.Errorf("starting replacement Herdr command: %w", err)
	}

	now := c.now().UTC()
	if err := c.withRegistry(func(state *registry) error {
		current, ok := state.Sessions[previous.Name]
		if !ok || current.PaneID != paneID {
			return fmt.Errorf("Herdr session changed during restart: %s", previous.Name)
		}
		previous.PaneID = newPaneID
		previous.TabID = newTabID
		previous.WorkDir = workDir
		previous.LastActivity = now
		state.Sessions[previous.Name] = previous
		return nil
	}); err != nil {
		_, _ = c.runner.Run("tab", "close", newTabID)
		return err
	}

	_, err = c.runner.Run("tab", "close", oldTabID)
	return err
}

// WaitReady waits until Herdr observes a usable agent lifecycle state.
func (c *Client) WaitReady(name string, timeout time.Duration) error {
	if timeout < time.Millisecond {
		timeout = time.Millisecond
	}
	deadline := time.Now().Add(timeout)
	var lastStatus string
	var lastErr error

	for {
		pane, err := c.Get(name)
		if err == nil {
			lastStatus = pane.AgentStatus
			switch pane.AgentStatus {
			case "idle", "done", "working":
				return nil
			}
		} else {
			lastErr = err
		}

		remaining := time.Until(deadline)
		if remaining <= 0 {
			if lastErr != nil {
				return fmt.Errorf("waiting for Herdr agent readiness: %w", lastErr)
			}
			return fmt.Errorf("waiting for Herdr agent readiness: timed out in status %q", lastStatus)
		}
		sleepFor := readyPollInterval
		if remaining < sleepFor {
			sleepFor = remaining
		}
		time.Sleep(sleepFor)
	}
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

// Rename changes the stable Gas Town name for a mapped Herdr tab. Herdr's
// opaque tab and pane identifiers remain unchanged.
func (c *Client) Rename(oldName, newName string) error {
	return c.withRegistry(func(state *registry) error {
		session, ok := state.Sessions[oldName]
		if !ok {
			return fmt.Errorf("%w: %s", ErrSessionNotFound, oldName)
		}
		if _, exists := state.Sessions[newName]; exists {
			return fmt.Errorf("Herdr session already exists: %s", newName)
		}
		delete(state.Sessions, oldName)
		session.Name = newName
		state.Sessions[newName] = session
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
	if err == nil && len(names) > 0 && c.autoStart {
		if serverErr := c.ensureServer(); serverErr != nil {
			return nil, serverErr
		}
	}
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

// Attach focuses a logical session and, when gt is running outside Herdr,
// attaches the terminal to the Town's named Herdr session.
func (c *Client) Attach(name string) error {
	if err := c.Focus(name); err != nil {
		return err
	}
	if os.Getenv("HERDR_ENV") == "1" {
		if current := strings.TrimSpace(os.Getenv("HERDR_SESSION")); current != "" && current != c.sessionName {
			return fmt.Errorf("Gas Town is bound to Herdr session %q, but this shell is inside %q; attach from a shell outside Herdr", c.sessionName, current)
		}
		return nil
	}
	attacher, ok := c.runner.(sessionAttacher)
	if !ok {
		return fmt.Errorf("%w: runner cannot attach a Herdr session", ErrUnavailable)
	}
	return attacher.Attach()
}

// StopServer stops only a Town-owned named Herdr server. A Town bound to an
// existing shared session must never stop that server or its manual workers.
func (c *Client) StopServer() error {
	if c.sessionName == "" || !c.ownsServer {
		return nil
	}
	if _, err := c.runner.Run("workspace", "list"); err != nil {
		return nil
	}
	_, err := c.runner.Run("server", "stop")
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
