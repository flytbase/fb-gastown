package herdr

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
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
