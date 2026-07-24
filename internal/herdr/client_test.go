package herdr

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

type fakeRunner struct {
	calls     [][]string
	responses [][]byte
	errs      []error
}

func (f *fakeRunner) Run(args ...string) ([]byte, error) {
	f.calls = append(f.calls, append([]string(nil), args...))
	index := len(f.calls) - 1
	var response []byte
	if index < len(f.responses) {
		response = f.responses[index]
	}
	var err error
	if index < len(f.errs) {
		err = f.errs[index]
	}
	return response, err
}

type managedFakeRunner struct {
	fakeRunner
	starts int
}

func (f *managedFakeRunner) StartServer() error {
	f.starts++
	return nil
}

func TestCreateUsesOpaqueIDsAndPersistsMapping(t *testing.T) {
	runner := &fakeRunner{responses: [][]byte{
		[]byte(`{"result":{"type":"tab_created","tab":{"tab_id":"tab:opaque"},"root_pane":{"pane_id":"pane:opaque"}}}`),
		nil,
	}}
	client := NewClient(runner, t.TempDir(), "workspace:opaque")

	err := client.Create("gt-rig-Toast", "/worktree", "exec claude", map[string]string{
		"GT_ROLE": "rig/polecats/Toast",
		"A":       "first",
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	wantCreate := []string{
		"tab", "create", "--workspace", "workspace:opaque", "--cwd", "/worktree",
		"--label", "gt-rig-Toast", "--no-focus",
		"--env", "A=first", "--env", "GT_ROLE=rig/polecats/Toast",
	}
	if !reflect.DeepEqual(runner.calls[0], wantCreate) {
		t.Fatalf("create args = %#v, want %#v", runner.calls[0], wantCreate)
	}
	wantRun := []string{"pane", "run", "pane:opaque", "exec claude"}
	if !reflect.DeepEqual(runner.calls[1], wantRun) {
		t.Fatalf("run args = %#v, want %#v", runner.calls[1], wantRun)
	}

	paneID, err := client.PaneID("gt-rig-Toast")
	if err != nil {
		t.Fatalf("PaneID() error = %v", err)
	}
	if paneID != "pane:opaque" {
		t.Fatalf("PaneID() = %q, want pane:opaque", paneID)
	}
}

func TestCreateClosesTabWhenCommandFails(t *testing.T) {
	runner := &fakeRunner{
		responses: [][]byte{
			[]byte(`{"result":{"tab":{"tab_id":"tab-1"},"root_pane":{"pane_id":"pane-1"}}}`),
			nil,
			nil,
		},
		errs: []error{nil, os.ErrPermission, nil},
	}
	client := NewClient(runner, t.TempDir(), "workspace-1")

	if err := client.Create("session", "/tmp", "claude", nil); err == nil {
		t.Fatal("Create() error = nil, want failure")
	}
	wantClose := []string{"tab", "close", "tab-1"}
	if !reflect.DeepEqual(runner.calls[2], wantClose) {
		t.Fatalf("cleanup args = %#v, want %#v", runner.calls[2], wantClose)
	}
}

func TestRegistryPermissions(t *testing.T) {
	root := t.TempDir()
	runner := &fakeRunner{responses: [][]byte{
		[]byte(`{"result":{"tab":{"tab_id":"tab-1"},"root_pane":{"pane_id":"pane-1"}}}`),
		nil,
	}}
	client := NewClient(runner, root, "workspace-1")
	if err := client.Create("session", "/tmp", "claude", nil); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	path := filepath.Join(root, ".runtime", registryFile)
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0600 {
		t.Fatalf("registry permissions = %o, want 600", got)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var state registry
	if err := json.Unmarshal(data, &state); err != nil {
		t.Fatal(err)
	}
	if state.Sessions["session"].TabID != "tab-1" {
		t.Fatalf("stored session = %#v", state.Sessions["session"])
	}
}

func TestManagedClientStartsServerAndCreatesTownWorkspace(t *testing.T) {
	root := t.TempDir()
	runner := &managedFakeRunner{fakeRunner: fakeRunner{
		responses: [][]byte{
			nil,
			[]byte(`{"result":{"workspaces":[]}}`),
			[]byte(`{"result":{"workspace":{"workspace_id":"workspace-town"}}}`),
			[]byte(`{"result":{"tab":{"tab_id":"tab-1"},"root_pane":{"pane_id":"pane-1"}}}`),
			nil,
		},
		errs: []error{
			errors.New("server unavailable"),
			nil,
			nil,
			nil,
			nil,
		},
	}}
	client := NewClient(runner, root, "")
	client.sessionName = "gt-test-123456"
	client.autoStart = true

	if err := client.Create("hq-mayor", root, "claude", nil); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if runner.starts != 1 {
		t.Fatalf("StartServer() calls = %d, want 1", runner.starts)
	}
	wantWorkspaceCreate := []string{
		"workspace", "create",
		"--cwd", root,
		"--label", "Gas Town: " + filepath.Base(root),
		"--no-focus",
		"--env", "GT_ROOT=" + root,
		"--env", "GT_TOWN_ROOT=" + root,
		"--env", "GT_SESSION_BACKEND=herdr",
	}
	if !reflect.DeepEqual(runner.calls[2], wantWorkspaceCreate) {
		t.Fatalf("workspace create args = %#v, want %#v", runner.calls[2], wantWorkspaceCreate)
	}

	data, err := os.ReadFile(filepath.Join(root, ".runtime", registryFile))
	if err != nil {
		t.Fatal(err)
	}
	var state registry
	if err := json.Unmarshal(data, &state); err != nil {
		t.Fatal(err)
	}
	if state.BackendSession != "gt-test-123456" || state.WorkspaceID != "workspace-town" {
		t.Fatalf("managed registry = %#v", state)
	}
}

func TestTownSessionNameIsStableAndPathScoped(t *testing.T) {
	root := filepath.Join(t.TempDir(), "My Town")
	if err := os.MkdirAll(root, 0755); err != nil {
		t.Fatal(err)
	}
	first := TownSessionName(root)
	second := TownSessionName(root)
	if first != second {
		t.Fatalf("TownSessionName() is not stable: %q != %q", first, second)
	}
	if !strings.HasPrefix(first, "gt-my-town-") {
		t.Fatalf("TownSessionName() = %q, want sanitized Town prefix", first)
	}

	otherRoot := filepath.Join(t.TempDir(), "My Town")
	if first == TownSessionName(otherRoot) {
		t.Fatalf("same basename produced colliding session name %q", first)
	}
}
