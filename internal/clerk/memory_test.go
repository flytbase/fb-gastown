package clerk

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSetMemoryPolicyPreservesUnrelatedClerkConfiguration(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(t.TempDir(), "config"))
	townRoot := t.TempDir()
	if _, err := Provision(townRoot, false); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(townRoot, "clerk", "clerk.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}
	raw["clickup"] = map[string]any{"workspace_id": "workspace-1"}
	if err := os.WriteFile(path, mustJSON(t, raw), 0600); err != nil {
		t.Fatal(err)
	}

	root := filepath.Join(t.TempDir(), "clerk-memory")
	memory := MemoryPolicy{
		Enabled:          true,
		Provider:         MemoryProviderGBrain,
		ID:               "clerk-town-123",
		Root:             root,
		SearchMode:       MemorySearchConservative,
		AutomaticCapture: true,
		SharedReadOnly:   true,
		SharedWrite:      false,
	}
	if _, err := ProvisionMemory(MemoryProvisionOptions{Policy: memory, SkipProviderInit: true}); err != nil {
		t.Fatal(err)
	}
	if err := SetMemoryPolicy(townRoot, memory); err != nil {
		t.Fatal(err)
	}

	policy, err := LoadPolicy(townRoot)
	if err != nil {
		t.Fatal(err)
	}
	if policy.Memory != memory {
		t.Fatalf("memory policy = %#v, want %#v", policy.Memory, memory)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var preserved map[string]any
	if err := json.Unmarshal(after, &preserved); err != nil {
		t.Fatal(err)
	}
	if preserved["clickup"].(map[string]any)["workspace_id"] != "workspace-1" {
		t.Fatal("SetMemoryPolicy removed unrelated ClickUp configuration")
	}
}

func TestProvisionMemoryCreatesPrivateCanonicalLayout(t *testing.T) {
	root := filepath.Join(t.TempDir(), "clerk-memory")
	result, err := ProvisionMemory(MemoryProvisionOptions{
		Policy: MemoryPolicy{
			Enabled:          true,
			Provider:         MemoryProviderGBrain,
			ID:               "clerk-town-123",
			Root:             root,
			SearchMode:       MemorySearchBalanced,
			AutomaticCapture: true,
		},
		SkipProviderInit: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.LayoutCreated {
		t.Fatalf("ProvisionMemory() = %+v, want layout created", result)
	}

	for _, rel := range []string{
		"brain/RESOLVER.md",
		"brain/schema.md",
		"brain/index.md",
		"brain/log.md",
		"brain/experiences",
		"brain/decisions",
		"brain/lessons",
		"brain/preferences",
		"brain/projects",
		"brain/people",
		"brain/companies",
		"brain/inbox",
		"brain/archive",
		"bin",
	} {
		if _, err := os.Stat(filepath.Join(root, rel)); err != nil {
			t.Errorf("%s: %v", rel, err)
		}
	}
	info, err := os.Stat(root)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0700 {
		t.Fatalf("root mode = %o, want 700", info.Mode().Perm())
	}
	if _, err := os.Stat(filepath.Join(root, ".gbrain", ".gbrain")); !os.IsNotExist(err) {
		t.Fatal("private GBrain home was nested as .gbrain/.gbrain")
	}
}

func TestProvisionMemoryRefusesUnknownExistingDirectory(t *testing.T) {
	root := filepath.Join(t.TempDir(), "clerk-memory")
	if err := os.MkdirAll(root, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "unmanaged.txt"), []byte("keep me"), 0600); err != nil {
		t.Fatal(err)
	}

	_, err := ProvisionMemory(MemoryProvisionOptions{
		Policy: MemoryPolicy{
			Enabled:    true,
			Provider:   MemoryProviderGBrain,
			ID:         "clerk-town-123",
			Root:       root,
			SearchMode: MemorySearchConservative,
		},
		SkipProviderInit: true,
	})
	if err == nil || !strings.Contains(err.Error(), "unmanaged") {
		t.Fatalf("ProvisionMemory() error = %v, want unmanaged-directory refusal", err)
	}
}

func TestCaptureAndRecallUseCanonicalMarkdownWithoutProvider(t *testing.T) {
	root := filepath.Join(t.TempDir(), "clerk-memory")
	policy := MemoryPolicy{
		Enabled:          true,
		Provider:         MemoryProviderGBrain,
		ID:               "clerk-town-123",
		Root:             root,
		SearchMode:       MemorySearchConservative,
		AutomaticCapture: true,
	}
	if _, err := ProvisionMemory(MemoryProvisionOptions{Policy: policy, SkipProviderInit: true}); err != nil {
		t.Fatal(err)
	}

	record := MemoryRecord{
		Type:       MemoryTypeDecision,
		Title:      "Use supervised dispatch",
		Content:    "Clerk must require a one-use human authorization before dispatch.",
		Source:     "user",
		SourceKind: MemorySourceUserStatement,
		RecordedAt: time.Date(2026, 7, 24, 9, 30, 0, 0, time.UTC),
	}
	if _, err := CaptureMemory(policy, record); err != nil {
		t.Fatal(err)
	}

	output, err := RecallMemory(policy, "authorization dispatch", 5)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output, "Use supervised dispatch") || !strings.Contains(output, "one-use human authorization") {
		t.Fatalf("RecallMemory() = %q", output)
	}
}

func TestCaptureRejectsLikelySecrets(t *testing.T) {
	policy := MemoryPolicy{
		Enabled:    true,
		Provider:   MemoryProviderGBrain,
		ID:         "clerk-town-123",
		Root:       filepath.Join(t.TempDir(), "clerk-memory"),
		SearchMode: MemorySearchConservative,
	}
	if _, err := ProvisionMemory(MemoryProvisionOptions{Policy: policy, SkipProviderInit: true}); err != nil {
		t.Fatal(err)
	}
	_, err := CaptureMemory(policy, MemoryRecord{
		Type:       MemoryTypeInbox,
		Title:      "temporary access",
		Content:    "api_key=sk-super-secret-token",
		Source:     "hook",
		SourceKind: MemorySourceObservation,
		RecordedAt: time.Now(),
	})
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "secret") {
		t.Fatalf("CaptureMemory() error = %v, want secret rejection", err)
	}
}

func TestCaptureRejectsCommonCredentialShapes(t *testing.T) {
	policy := DefaultMemoryPolicy(t.TempDir(), "town")
	policy.Root = filepath.Join(t.TempDir(), "clerk-memory")
	if _, err := ProvisionMemory(MemoryProvisionOptions{Policy: policy, SkipProviderInit: true}); err != nil {
		t.Fatal(err)
	}
	secrets := []string{
		"Please remember my password is hunter2",
		"The deployment secret is super-sensitive",
		"GitHub PAT " + "gh" + "p_123456789012345678901234567890123456",
		"Slack token " + "xo" + "xb-1234567890-secret",
		"GitLab token " + "gl" + "pat-1234567890abcdef",
		"npm token " + "npm" + "_1234567890abcdef",
		"AWS key " + "AK" + "IA1234567890ABCDEF",
		"Authorization: Bearer abcdefghijklmnopqrstuvwxyz",
		"JWT eyJabcdefghijk.abcdefghijkl.abcdefghijkl",
		"Use https://admin:hunter2@example.com/private",
		"-----BEGIN " + "PRIVATE KEY-----",
	}
	for _, secret := range secrets {
		_, err := CaptureMemory(policy, MemoryRecord{
			Type:       MemoryTypeInbox,
			Title:      "credential",
			Content:    secret,
			Source:     "test",
			SourceKind: MemorySourceUserStatement,
			RecordedAt: time.Now(),
		})
		if err == nil {
			t.Errorf("CaptureMemory(%q) accepted a likely credential", secret)
		}
	}
}

func TestCaptureNeverOverwritesSameTitleAndTimestamp(t *testing.T) {
	policy := DefaultMemoryPolicy(t.TempDir(), "town")
	policy.Root = filepath.Join(t.TempDir(), "clerk-memory")
	if _, err := ProvisionMemory(MemoryProvisionOptions{Policy: policy, SkipProviderInit: true}); err != nil {
		t.Fatal(err)
	}
	record := MemoryRecord{
		Type:       MemoryTypeDecision,
		Title:      "Same decision",
		Content:    "Keep both canonical records.",
		Source:     "test",
		SourceKind: MemorySourceObservation,
		RecordedAt: time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC),
	}
	first, err := CaptureMemory(policy, record)
	if err != nil {
		t.Fatal(err)
	}
	second, err := CaptureMemory(policy, record)
	if err != nil {
		t.Fatal(err)
	}
	if first.Path == second.Path {
		t.Fatalf("same-time captures used the same path %s", first.Path)
	}
}

func TestDefaultMemoryPolicyHasStableTownScopedIdentity(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", filepath.Join(t.TempDir(), "data"))
	first := DefaultMemoryPolicy("/tmp/my-town", "My Town")
	second := DefaultMemoryPolicy("/tmp/my-town", "My Town")
	other := DefaultMemoryPolicy("/tmp/other-town", "My Town")

	if first.ID != second.ID || first.Root != second.Root {
		t.Fatalf("DefaultMemoryPolicy is unstable: %#v != %#v", first, second)
	}
	if first.ID == other.ID {
		t.Fatalf("different Town roots received the same memory ID %q", first.ID)
	}
	if first.SearchMode != MemorySearchConservative || first.SharedWrite {
		t.Fatalf("unsafe default memory policy: %#v", first)
	}
	if !strings.HasPrefix(first.Root, os.Getenv("XDG_DATA_HOME")) {
		t.Fatalf("memory root = %q, want durable XDG data root", first.Root)
	}
}

func TestGBrainPrivateIntegration(t *testing.T) {
	binary := os.Getenv("GT_TEST_GBRAIN")
	if binary == "" {
		t.Skip("set GT_TEST_GBRAIN to run the isolated provider integration")
	}
	root := filepath.Join(t.TempDir(), "private-clerk")
	policy := DefaultMemoryPolicy(t.TempDir(), "integration")
	policy.Root = root
	policy.GBrainBinary = binary
	result, err := ProvisionMemory(MemoryProvisionOptions{Policy: policy})
	if err != nil {
		t.Fatal(err)
	}
	if !result.ProviderInitialized {
		t.Fatalf("ProvisionMemory() = %+v, want initialized provider", result)
	}
	if _, err := os.Stat(filepath.Join(root, ".gbrain")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, ".gbrain", ".gbrain")); !os.IsNotExist(err) {
		t.Fatal("GBrain integration created a nested .gbrain/.gbrain home")
	}
	health := HealthMemory(policy)
	if !health.LayoutOK || !health.ProviderOK {
		t.Fatalf("HealthMemory() = %+v", health)
	}
}

func TestPurgeMemoryRequiresMatchingManagedManifest(t *testing.T) {
	townRoot := t.TempDir()
	policy := DefaultMemoryPolicy(townRoot, "town")
	policy.Root = filepath.Join(t.TempDir(), "clerk-memory")
	if _, err := ProvisionMemory(MemoryProvisionOptions{Policy: policy, SkipProviderInit: true}); err != nil {
		t.Fatal(err)
	}
	wrong := policy
	wrong.ID = "different-clerk"
	if err := PurgeMemory(wrong, townRoot); err == nil {
		t.Fatal("PurgeMemory accepted a mismatched agent identity")
	}
	if _, err := os.Stat(policy.Root); err != nil {
		t.Fatalf("mismatched purge changed memory root: %v", err)
	}
	if err := PurgeMemory(policy, townRoot); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(policy.Root); !os.IsNotExist(err) {
		t.Fatalf("managed root still exists after explicit purge: %v", err)
	}
	if err := PurgeMemory(policy, townRoot); err != nil {
		t.Fatalf("already-absent purge must be idempotent: %v", err)
	}
}

func TestGuardRawGBrainCommandRejectsAbsoluteAndLauncherPaths(t *testing.T) {
	for _, command := range []string{
		"gbrain search task",
		"$HOME/.bun/bin/gbrain search task",
		"/tmp/private/bin/gbrain-private put x",
		"env GBRAIN_HOME=/tmp/private bun src/cli.ts",
		"bun /tmp/gbrain/src/cli.ts search task",
		"x=gbrain; \"$x\" search task",
		"env G B R A I N _ H O M E=/tmp/private bun cli.ts",
	} {
		if err := GuardRawGBrainCommand(command); err == nil {
			t.Errorf("GuardRawGBrainCommand(%q) = nil, want blocked", command)
		}
	}
	if err := GuardRawGBrainCommand("gt clerk memory recall task"); err != nil {
		t.Fatalf("native memory command was blocked: %v", err)
	}
}

func TestGuardClerkShellCommandUsesPositiveNativeAllowlist(t *testing.T) {
	allowed := []string{
		"gt clerk memory recall task",
		"gt clerk request bead rig",
		"gt agents list -a",
		"bd update gt-123 --status in_progress",
		`bd create "Task with spaces"`,
	}
	for _, command := range allowed {
		if err := GuardClerkShellCommand(command); err != nil {
			t.Errorf("GuardClerkShellCommand(%q) = %v, want allowed", command, err)
		}
	}

	blocked := []string{
		`a=g; b=brain; "$a$b" search task`,
		"bun /tmp/gbrain/src/cli.ts search task",
		"sh -c 'gbrain search task'",
		"/tmp/gt clerk memory recall task",
		"gt clerk authorize request",
		"gt sling bead rig",
		"bd ready | gbrain import -",
		"bd $(printf ready)",
	}
	for _, command := range blocked {
		if err := GuardClerkShellCommand(command); err == nil {
			t.Errorf("GuardClerkShellCommand(%q) = nil, want blocked", command)
		}
	}
}
