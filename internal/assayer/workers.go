package assayer

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Worker defines one response-only, backend-testing sub-worker provisioned for
// the Assayer. Only the Assayer Lead may spawn these workers.
type Worker struct {
	Kind         string
	DisplayName  string
	Purpose      string
	Domain       string
	MayDelegate  bool
	Instructions string `json:"instructions"`
}

// Workers returns the immutable native Phase 1 worker catalog.
func Workers() []Worker {
	data, err := defaultAssets.ReadFile("defaults/workers.json")
	if err != nil {
		panic(fmt.Sprintf("reading embedded Assayer workers: %v", err))
	}
	var workers []Worker
	if err := json.Unmarshal(data, &workers); err != nil {
		panic(fmt.Sprintf("decoding embedded Assayer workers: %v", err))
	}
	return workers
}

// EnsureWorkerDefinitions refreshes the managed Claude and Codex role files.
func EnsureWorkerDefinitions(home string) error {
	codexDir := filepath.Join(home, ".codex", "agents")
	claudeDir := filepath.Join(home, ".claude", "agents")
	for _, dir := range []string{codexDir, claudeDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	for _, worker := range Workers() {
		if err := os.WriteFile(filepath.Join(codexDir, worker.Kind+".toml"), []byte(renderCodexWorker(worker)), 0o644); err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(claudeDir, worker.Kind+".md"), []byte(renderClaudeWorker(worker)), 0o644); err != nil {
			return err
		}
	}
	return nil
}

func renderCodexWorker(worker Worker) string {
	instructions := strings.ReplaceAll(worker.Instructions, `"""`, `\"\"\"`)
	return fmt.Sprintf("name = %q\ndescription = %q\ndeveloper_instructions = \"\"\"\n%s\"\"\"\n", worker.Kind, worker.Purpose, instructions)
}

func renderClaudeWorker(worker Worker) string {
	return fmt.Sprintf("---\nname: %s\ndescription: %s\n---\n\n%s", worker.Kind, worker.Purpose, worker.Instructions)
}
