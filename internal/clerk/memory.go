package clerk

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/steveyegge/gastown/internal/atomicfile"
	"github.com/steveyegge/gastown/internal/state"
)

const (
	MemoryProviderGBrain = "gbrain"

	MemorySearchConservative = "conservative"
	MemorySearchBalanced     = "balanced"
	MemorySearchTokenMax     = "tokenmax"

	MemoryTypeExperience = "experiences"
	MemoryTypeDecision   = "decisions"
	MemoryTypeLesson     = "lessons"
	MemoryTypePreference = "preferences"
	MemoryTypeProject    = "projects"
	MemoryTypePerson     = "people"
	MemoryTypeCompany    = "companies"
	MemoryTypeInbox      = "inbox"

	MemorySourceObservation   = "observation"
	MemorySourceUserStatement = "user_statement"
	MemorySourceInference     = "inference"

	memoryManifestName = ".gastown-clerk-memory.json"
)

var (
	memoryTypes = []string{
		MemoryTypeExperience,
		MemoryTypeDecision,
		MemoryTypeLesson,
		MemoryTypePreference,
		MemoryTypeProject,
		MemoryTypePerson,
		MemoryTypeCompany,
		MemoryTypeInbox,
		"archive",
	}
	memorySecretPatterns = []*regexp.Regexp{
		regexp.MustCompile(`(?i)\b(api[ _-]?key|access[ _-]?token|auth[ _-]?token|password|passwd|client[ _-]?secret|private[ _-]?key)\b\s*(?:is|was|:|=)\s*\S+`),
		regexp.MustCompile(`(?i)\bsecret\b\s*(?:is|was|:|=)\s*\S+`),
		regexp.MustCompile(`(?i)\bbearer\s+[a-z0-9._~+/=-]{8,}`),
		regexp.MustCompile(`\b(?:gh[pousr]_[A-Za-z0-9_]{20,}|AKIA[0-9A-Z]{16}|sk-[A-Za-z0-9_-]{8,})\b`),
		regexp.MustCompile(`\bxox[baprs]-[A-Za-z0-9-]{8,}\b`),
		regexp.MustCompile(`\bglpat-[A-Za-z0-9_-]{8,}\b`),
		regexp.MustCompile(`\bnpm_[A-Za-z0-9]{8,}\b`),
		regexp.MustCompile(`\beyJ[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}\b`),
		regexp.MustCompile(`(?i)https?://[^/\s:@]+:[^@\s/]+@`),
		regexp.MustCompile(`-----BEGIN [A-Z ]*PRIVATE KEY-----`),
	}
	memorySlugPattern = regexp.MustCompile(`[^a-z0-9]+`)
)

// MemoryPolicy is the Town-owned Clerk memory configuration. Root is the
// private agent root; current GBrain appends ".gbrain" to GBRAIN_HOME.
type MemoryPolicy struct {
	Enabled          bool   `json:"enabled"`
	Provider         string `json:"provider,omitempty"`
	ID               string `json:"id,omitempty"`
	Root             string `json:"root,omitempty"`
	SearchMode       string `json:"search_mode,omitempty"`
	AutomaticCapture bool   `json:"automatic_capture,omitempty"`
	SharedReadOnly   bool   `json:"shared_readonly,omitempty"`
	SharedWrite      bool   `json:"shared_write"`
	GBrainBinary     string `json:"gbrain_binary,omitempty"`
	SharedRoot       string `json:"shared_root,omitempty"`
}

// DefaultMemoryPolicy returns a durable, private, Town-scoped policy. The
// memory root lives outside the disposable Gas Town cache and is intentionally
// preserved by normal uninstall.
func DefaultMemoryPolicy(townRoot, townName string) MemoryPolicy {
	sum := sha256.Sum256([]byte(filepath.Clean(townRoot)))
	name := memorySlug(townName)
	if name == "" || name == "memory" {
		name = "town"
	}
	id := fmt.Sprintf("clerk-%s-%x", name, sum[:4])
	dataRoot := os.Getenv("XDG_DATA_HOME")
	if dataRoot == "" {
		if home, err := os.UserHomeDir(); err == nil {
			dataRoot = filepath.Join(home, ".local", "share")
		}
	}
	return MemoryPolicy{
		Enabled:          true,
		Provider:         MemoryProviderGBrain,
		ID:               id,
		Root:             filepath.Join(dataRoot, "gastown", "clerk-memories", id),
		SearchMode:       MemorySearchConservative,
		AutomaticCapture: true,
		SharedWrite:      false,
	}
}

func (p MemoryPolicy) Validate() error {
	if !p.Enabled {
		if p.SharedWrite {
			return fmt.Errorf("shared writes are not supported")
		}
		return nil
	}
	if p.Provider != MemoryProviderGBrain {
		return fmt.Errorf("unsupported provider %q", p.Provider)
	}
	if strings.TrimSpace(p.ID) == "" {
		return fmt.Errorf("id is required")
	}
	if !filepath.IsAbs(p.Root) {
		return fmt.Errorf("root must be an absolute path")
	}
	switch p.SearchMode {
	case MemorySearchConservative, MemorySearchBalanced, MemorySearchTokenMax:
	default:
		return fmt.Errorf("unsupported search mode %q", p.SearchMode)
	}
	if p.SharedWrite {
		return fmt.Errorf("shared writes are blocked; promotion requires a separate user-approved workflow")
	}
	return nil
}

type MemoryProvisionOptions struct {
	Policy           MemoryPolicy
	SkipProviderInit bool
}

type MemoryProvisionResult struct {
	LayoutCreated       bool
	ProviderInitialized bool
	Binary              string
	EmbeddingStatus     string
}

type memoryManifest struct {
	Version  int    `json:"version"`
	AgentID  string `json:"agent_id"`
	Provider string `json:"provider"`
}

// ProvisionMemory creates a private canonical Markdown store, a private GBrain
// home, and guarded launchers. It never modifies another GBrain home.
func ProvisionMemory(opts MemoryProvisionOptions) (MemoryProvisionResult, error) {
	var result MemoryProvisionResult
	policy := opts.Policy
	if err := policy.Validate(); err != nil {
		return result, err
	}
	if !policy.Enabled {
		return result, nil
	}

	root := filepath.Clean(policy.Root)
	manifestPath := filepath.Join(root, memoryManifestName)
	if entries, err := os.ReadDir(root); err == nil && len(entries) > 0 {
		data, readErr := os.ReadFile(manifestPath)
		if readErr != nil {
			return result, fmt.Errorf("memory root %s contains unmanaged data; refusing to overwrite it", root)
		}
		var manifest memoryManifest
		if jsonErr := json.Unmarshal(data, &manifest); jsonErr != nil || manifest.AgentID != policy.ID {
			return result, fmt.Errorf("memory root %s is managed for a different or invalid Clerk identity", root)
		}
	} else if err != nil && !os.IsNotExist(err) {
		return result, fmt.Errorf("checking memory root: %w", err)
	} else {
		result.LayoutCreated = true
	}

	if err := os.MkdirAll(root, 0700); err != nil {
		return result, fmt.Errorf("creating memory root: %w", err)
	}
	if err := os.Chmod(root, 0700); err != nil {
		return result, fmt.Errorf("restricting memory root: %w", err)
	}
	for _, dir := range append([]string{"brain", "bin", ".gbrain"}, prefixedMemoryDirs()...) {
		mode := fs.FileMode(0700)
		if strings.HasPrefix(dir, "brain/") || dir == "brain" {
			mode = 0755
		}
		if err := os.MkdirAll(filepath.Join(root, dir), mode); err != nil {
			return result, fmt.Errorf("creating private memory directory %s: %w", dir, err)
		}
	}
	if err := os.Chmod(filepath.Join(root, ".gbrain"), 0700); err != nil {
		return result, fmt.Errorf("restricting private GBrain home: %w", err)
	}

	files := map[string]string{
		"brain/RESOLVER.md": memoryResolver(policy.ID),
		"brain/schema.md":   memorySchema,
		"brain/index.md":    "# Clerk Memory Index\n\nCanonical private memories are filed by type. GBrain is a search index, not the source of truth.\n",
		"brain/log.md":      "# Clerk Memory Log\n\n",
	}
	for rel, content := range files {
		path := filepath.Join(root, rel)
		if _, err := os.Stat(path); os.IsNotExist(err) {
			if err := atomicfile.WriteFile(path, []byte(content), 0600); err != nil {
				return result, fmt.Errorf("writing %s: %w", rel, err)
			}
		} else if err != nil {
			return result, fmt.Errorf("checking %s: %w", rel, err)
		}
	}
	if err := atomicfile.WriteJSONWithPerm(manifestPath, memoryManifest{
		Version:  1,
		AgentID:  policy.ID,
		Provider: policy.Provider,
	}, 0600); err != nil {
		return result, fmt.Errorf("writing memory manifest: %w", err)
	}
	if err := ensureMemoryGitRepository(filepath.Join(root, "brain")); err != nil {
		return result, err
	}

	if opts.SkipProviderInit {
		return result, nil
	}
	binary, err := ResolveGBrainBinary(policy.GBrainBinary)
	if err != nil {
		return result, err
	}
	result.Binary = binary
	if err := writeMemoryLaunchers(policy, binary); err != nil {
		return result, err
	}
	if err := initializeGBrain(policy, binary); err != nil {
		return result, err
	}
	result.ProviderInitialized = true
	result.EmbeddingStatus = "keyword-only"
	return result, nil
}

// PurgeMemory permanently removes exactly one GT-managed private Clerk store.
// The manifest and agent identity are mandatory; broad or ambiguous roots are
// rejected even when the caller explicitly requested purge.
func PurgeMemory(policy MemoryPolicy, townRoot string) error {
	root := filepath.Clean(policy.Root)
	if !filepath.IsAbs(root) || strings.TrimSpace(policy.ID) == "" {
		return fmt.Errorf("refusing to purge invalid Clerk memory identity or root")
	}
	if info, err := os.Lstat(root); os.IsNotExist(err) {
		return nil
	} else if err != nil {
		return fmt.Errorf("checking Clerk memory root for purge: %w", err)
	} else if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return fmt.Errorf("refusing to purge a symlink or non-directory memory root: %s", root)
	}
	if err := verifyMemoryManifest(policy); err != nil {
		return fmt.Errorf("refusing to purge memory: %w", err)
	}
	if root == string(filepath.Separator) || filepath.Dir(root) == root {
		return fmt.Errorf("refusing to purge broad filesystem root %s", root)
	}
	if home, err := os.UserHomeDir(); err == nil && isSameOrAncestor(root, filepath.Clean(home)) {
		return fmt.Errorf("refusing to purge a home directory or its ancestor: %s", root)
	}
	if townRoot != "" && isSameOrAncestor(root, filepath.Clean(townRoot)) {
		return fmt.Errorf("refusing to purge a Town workspace or its ancestor: %s", root)
	}
	if err := os.RemoveAll(root); err != nil {
		return fmt.Errorf("purging private Clerk memory: %w", err)
	}
	return nil
}

func verifyMemoryManifest(policy MemoryPolicy) error {
	data, err := os.ReadFile(filepath.Join(filepath.Clean(policy.Root), memoryManifestName))
	if err != nil {
		return fmt.Errorf("managed manifest is not readable: %w", err)
	}
	var manifest memoryManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return fmt.Errorf("managed manifest is invalid: %w", err)
	}
	if manifest.AgentID != policy.ID || manifest.Provider != MemoryProviderGBrain {
		return fmt.Errorf("managed manifest does not match Clerk identity")
	}
	return nil
}

func isSameOrAncestor(candidate, target string) bool {
	relative, err := filepath.Rel(filepath.Clean(candidate), filepath.Clean(target))
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

func prefixedMemoryDirs() []string {
	dirs := make([]string, 0, len(memoryTypes))
	for _, dir := range memoryTypes {
		dirs = append(dirs, filepath.Join("brain", dir))
	}
	return dirs
}

func ensureMemoryGitRepository(brainDir string) error {
	if _, err := os.Stat(filepath.Join(brainDir, ".git")); err == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", "init", "--quiet", brainDir)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("initializing private memory Git repository: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

func ResolveGBrainBinary(configured string) (string, error) {
	if configured != "" {
		path, err := filepath.Abs(configured)
		if err == nil {
			if info, statErr := os.Stat(path); statErr == nil && !info.IsDir() && info.Mode()&0111 != 0 {
				return path, nil
			}
		}
		return "", fmt.Errorf("configured GBrain binary is unavailable: %s", configured)
	}
	path, err := exec.LookPath("gbrain")
	if err == nil {
		return filepath.Abs(path)
	}
	home, homeErr := os.UserHomeDir()
	if homeErr == nil {
		path = filepath.Join(home, ".bun", "bin", "gbrain")
		if info, statErr := os.Stat(path); statErr == nil && !info.IsDir() && info.Mode()&0111 != 0 {
			return path, nil
		}
	}
	return "", fmt.Errorf("GBrain is not installed; Gas Town must provision its managed extension before enabling Clerk memory")
}

func gbrainEnv(policy MemoryPolicy) []string {
	// GBrain expects the parent here and resolves <GBRAIN_HOME>/.gbrain.
	env := os.Environ()
	path := os.Getenv("PATH")
	path = filepath.Join(state.CacheDir(), "extensions", "bun", "bin") + string(os.PathListSeparator) + path
	if home, err := os.UserHomeDir(); err == nil {
		path = filepath.Join(home, ".bun", "bin") + string(os.PathListSeparator) + path
	}
	if binaryDir := filepath.Dir(policy.GBrainBinary); policy.GBrainBinary != "" {
		path = binaryDir + string(os.PathListSeparator) + path
	}
	env = append(env, "PATH="+path)
	return append(env, "GBRAIN_HOME="+filepath.Clean(policy.Root))
}

func runGBrain(policy MemoryPolicy, binary string, timeout time.Duration, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary, args...)
	cmd.Env = gbrainEnv(policy)
	output, err := cmd.CombinedOutput()
	if err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return output, fmt.Errorf("gbrain %s timed out", strings.Join(args, " "))
		}
		return output, fmt.Errorf("gbrain %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(output)))
	}
	return output, nil
}

func initializeGBrain(policy MemoryPolicy, binary string) error {
	if _, err := runGBrain(policy, binary, 2*time.Minute, "init", "--pglite", "--no-embedding", "--non-interactive"); err != nil {
		return fmt.Errorf("initializing private GBrain: %w", err)
	}
	if _, err := runGBrain(policy, binary, 30*time.Second, "config", "set", "search.mode", policy.SearchMode); err != nil {
		return fmt.Errorf("configuring private GBrain search mode: %w", err)
	}
	if _, err := runGBrain(policy, binary, 2*time.Minute, "import", filepath.Join(policy.Root, "brain"), "--no-embed", "--json"); err != nil {
		return fmt.Errorf("indexing private Clerk memory: %w", err)
	}
	return nil
}

func writeMemoryLaunchers(policy MemoryPolicy, binary string) error {
	managedBunBin := filepath.Join(state.CacheDir(), "extensions", "bun", "bin")
	private := fmt.Sprintf("#!/bin/sh\nset -eu\nexport PATH=%s:\"$PATH\"\nexport GBRAIN_HOME=%s\nexec %s \"$@\"\n",
		shellQuote(managedBunBin), shellQuote(policy.Root), shellQuote(binary))
	if err := atomicfile.WriteFile(filepath.Join(policy.Root, "bin", "gbrain-private"), []byte(private), 0700); err != nil {
		return fmt.Errorf("writing private GBrain launcher: %w", err)
	}
	if !policy.SharedReadOnly || policy.SharedRoot == "" {
		return nil
	}
	shared := fmt.Sprintf(`#!/bin/sh
set -eu
case "${1:-}" in
  search|query|get|list|stats|graph-query|backlinks) ;;
  *) echo "BLOCKED: shared GBrain is read-only for Clerk" >&2; exit 2 ;;
esac
export PATH=%s:"$PATH"
export GBRAIN_HOME=%s
exec %s "$@"
`, shellQuote(managedBunBin), shellQuote(policy.SharedRoot), shellQuote(binary))
	if err := atomicfile.WriteFile(filepath.Join(policy.Root, "bin", "gbrain-shared-readonly"), []byte(shared), 0700); err != nil {
		return fmt.Errorf("writing shared read-only GBrain launcher: %w", err)
	}
	return nil
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

type MemoryRecord struct {
	Type       string
	Title      string
	Content    string
	Source     string
	SourceKind string
	RecordedAt time.Time
}

type MemoryCaptureResult struct {
	Path          string
	ProviderError string
}

type MemoryHealth struct {
	Enabled         bool
	LayoutOK        bool
	ProviderOK      bool
	CanonicalRoot   string
	GBrainHome      string
	EmbeddingStatus string
	Warning         string
}

func CaptureMemory(policy MemoryPolicy, record MemoryRecord) (MemoryCaptureResult, error) {
	var result MemoryCaptureResult
	if err := policy.Validate(); err != nil {
		return result, err
	}
	if !policy.Enabled {
		return result, fmt.Errorf("Clerk memory is disabled")
	}
	if !containsMemoryType(record.Type) || record.Type == "archive" {
		return result, fmt.Errorf("unsupported memory type %q", record.Type)
	}
	if strings.TrimSpace(record.Title) == "" || strings.TrimSpace(record.Content) == "" {
		return result, fmt.Errorf("title and content are required")
	}
	if strings.TrimSpace(record.Source) == "" || record.RecordedAt.IsZero() {
		return result, fmt.Errorf("source and recorded date are required")
	}
	switch record.SourceKind {
	case MemorySourceObservation, MemorySourceUserStatement, MemorySourceInference:
	default:
		return result, fmt.Errorf("unsupported source kind %q", record.SourceKind)
	}
	if containsLikelySecret(record.Title + "\n" + record.Content) {
		return result, fmt.Errorf("memory rejected because it appears to contain a credential or secret")
	}

	slug := memorySlug(record.Title)
	body := fmt.Sprintf("---\ntitle: %q\ntype: %s\nsource: %q\nsource_kind: %s\nrecorded_at: %s\n---\n\n%s\n",
		record.Title, record.Type, record.Source, record.SourceKind, record.RecordedAt.UTC().Format(time.RFC3339), strings.TrimSpace(record.Content))
	path, err := writeMemoryCreateOnly(filepath.Join(policy.Root, "brain", record.Type), record.RecordedAt, slug, []byte(body))
	if err != nil {
		return result, fmt.Errorf("writing canonical Clerk memory: %w", err)
	}
	result.Path = path

	if binary, err := ResolveGBrainBinary(policy.GBrainBinary); err == nil {
		if _, importErr := runGBrain(policy, binary, 90*time.Second, "import", filepath.Join(policy.Root, "brain"), "--no-embed", "--json"); importErr != nil {
			result.ProviderError = importErr.Error()
		}
	} else {
		result.ProviderError = err.Error()
	}
	return result, nil
}

func writeMemoryCreateOnly(dir string, recordedAt time.Time, slug string, body []byte) (string, error) {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", err
	}
	for range 5 {
		filename := recordedAt.UTC().Format("2006-01-02T150405.000000000Z") + "-" + slug + "-" + uuid.NewString() + ".md"
		path := filepath.Join(dir, filename)
		file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if os.IsExist(err) {
			continue
		}
		if err != nil {
			return "", err
		}
		if _, err := file.Write(body); err != nil {
			_ = file.Close()
			_ = os.Remove(path)
			return "", err
		}
		if err := file.Close(); err != nil {
			_ = os.Remove(path)
			return "", err
		}
		return path, nil
	}
	return "", fmt.Errorf("could not allocate a unique memory filename")
}

func containsLikelySecret(value string) bool {
	for _, pattern := range memorySecretPatterns {
		if pattern.MatchString(value) {
			return true
		}
	}
	return false
}

// GuardRawGBrainCommand rejects direct provider access from the Clerk role.
// Native gt clerk memory commands are the only supported surface.
func GuardRawGBrainCommand(command string) error {
	normalized := strings.ToLower(command)
	normalized = strings.NewReplacer(
		" ", "",
		"\t", "",
		"\r", "",
		"\n", "",
		"'", "",
		`"`, "",
		"\\", "",
	).Replace(normalized)
	if strings.Contains(normalized, "gbrain") || strings.Contains(normalized, "gbrain_home=") || strings.Contains(normalized, "gbrainhome=") {
		return fmt.Errorf("Clerk memory must use the guarded gt clerk memory interface, never raw GBrain")
	}
	return nil
}

// GuardClerkShellCommand is the strict shell policy used while private memory
// is enabled. A denylist cannot secure a shell because an executable name can
// be assembled dynamically; Clerk therefore receives only the native command
// surfaces required for supervised task coordination.
func GuardClerkShellCommand(command string) error {
	argv, err := parseSimpleShellCommand(command)
	if err != nil {
		return fmt.Errorf("BLOCKED: Clerk shell commands must be a single, literal native command: %w", err)
	}
	if len(argv) == 0 {
		return fmt.Errorf("BLOCKED: empty Clerk shell command")
	}
	switch argv[0] {
	case "gt":
		if len(argv) < 2 || !allowedClerkGTSubcommand(argv[1]) {
			return fmt.Errorf("BLOCKED: Clerk may use only supervised task-coordination gt commands")
		}
		if argv[1] == "clerk" && len(argv) >= 3 && argv[2] == "authorize" {
			return fmt.Errorf("BLOCKED: Clerk cannot authorize its own dispatch request")
		}
	case "bd":
		// Beads is Clerk's native durable task surface. The shell parser has
		// already excluded composition, expansion, redirection, and execution
		// through another interpreter.
	default:
		return fmt.Errorf("BLOCKED: Clerk shell access is limited to native gt and bd task-coordination commands")
	}
	return nil
}

func allowedClerkGTSubcommand(subcommand string) bool {
	switch subcommand {
	case "agents", "clerk", "convoy", "doctor", "hook", "mail", "maintain", "prime", "show", "status":
		return true
	default:
		return false
	}
}

func parseSimpleShellCommand(command string) ([]string, error) {
	var (
		argv       []string
		current    strings.Builder
		quote      rune
		tokenStart bool
	)
	flush := func() {
		if tokenStart {
			argv = append(argv, current.String())
			current.Reset()
			tokenStart = false
		}
	}
	runes := []rune(command)
	for i := 0; i < len(runes); i++ {
		char := runes[i]
		switch quote {
		case '\'':
			if char == '\'' {
				quote = 0
			} else {
				current.WriteRune(char)
			}
			tokenStart = true
			continue
		case '"':
			switch char {
			case '"':
				quote = 0
			case '$', '`':
				return nil, fmt.Errorf("shell expansion is not allowed")
			case '\\':
				if i+1 >= len(runes) {
					return nil, fmt.Errorf("trailing escape")
				}
				i++
				current.WriteRune(runes[i])
			default:
				current.WriteRune(char)
			}
			tokenStart = true
			continue
		}

		switch char {
		case ' ', '\t':
			flush()
		case '\n', '\r', ';', '|', '&', '<', '>', '(', ')', '{', '}':
			return nil, fmt.Errorf("shell composition is not allowed")
		case '$', '`':
			return nil, fmt.Errorf("shell expansion is not allowed")
		case '\'', '"':
			quote = char
			tokenStart = true
		case '\\':
			if i+1 >= len(runes) {
				return nil, fmt.Errorf("trailing escape")
			}
			i++
			current.WriteRune(runes[i])
			tokenStart = true
		default:
			current.WriteRune(char)
			tokenStart = true
		}
	}
	if quote != 0 {
		return nil, fmt.Errorf("unterminated quote")
	}
	flush()
	return argv, nil
}

func RecallMemory(policy MemoryPolicy, query string, limit int) (string, error) {
	if err := policy.Validate(); err != nil {
		return "", err
	}
	if !policy.Enabled {
		return "", nil
	}
	if limit <= 0 {
		limit = 5
	}
	var recalled strings.Builder
	if binary, err := ResolveGBrainBinary(policy.GBrainBinary); err == nil && strings.TrimSpace(query) != "" {
		if output, searchErr := runGBrain(policy, binary, 30*time.Second, "search", query, "--limit", fmt.Sprint(limit), "--mode", policy.SearchMode); searchErr == nil && strings.TrimSpace(string(output)) != "" {
			recalled.WriteString("\n## Relevant private Clerk memory\n\n")
			recalled.WriteString(strings.TrimSpace(string(output)))
			recalled.WriteString("\n")
		}
		if policy.SharedReadOnly && policy.SharedRoot != "" {
			shared := policy
			shared.Root = policy.SharedRoot
			shared.SharedReadOnly = false
			if output, sharedErr := runGBrain(shared, binary, 30*time.Second, "search", query, "--limit", fmt.Sprint(limit), "--mode", policy.SearchMode); sharedErr == nil && strings.TrimSpace(string(output)) != "" {
				recalled.WriteString("\n## Relevant shared memory (read-only)\n\n")
				recalled.WriteString(strings.TrimSpace(string(output)))
				recalled.WriteString("\n")
			}
		}
		if recalled.Len() > 0 {
			return recalled.String(), nil
		}
	}
	// GBrain output formats can evolve; canonical Markdown keeps recall
	// available and deterministic whenever the provider is unavailable.
	return recallMarkdown(filepath.Join(policy.Root, "brain"), query, limit)
}

// HealthMemory verifies the configured private paths and asks GBrain to inspect
// only the private home selected by GBRAIN_HOME.
func HealthMemory(policy MemoryPolicy) MemoryHealth {
	health := MemoryHealth{
		Enabled:         policy.Enabled,
		CanonicalRoot:   filepath.Join(policy.Root, "brain"),
		GBrainHome:      filepath.Join(policy.Root, ".gbrain"),
		EmbeddingStatus: "keyword-only",
	}
	if !policy.Enabled {
		return health
	}
	if err := policy.Validate(); err != nil {
		health.Warning = err.Error()
		return health
	}
	for _, path := range []string{
		filepath.Join(policy.Root, memoryManifestName),
		health.CanonicalRoot,
		health.GBrainHome,
	} {
		if _, err := os.Stat(path); err != nil {
			health.Warning = fmt.Sprintf("%s: %v", path, err)
			return health
		}
	}
	health.LayoutOK = true
	binary, err := ResolveGBrainBinary(policy.GBrainBinary)
	if err != nil {
		health.Warning = err.Error()
		return health
	}
	if _, err := runGBrain(policy, binary, 45*time.Second, "doctor", "--json"); err != nil {
		health.Warning = err.Error()
		return health
	}
	health.ProviderOK = true
	return health
}

// MaintainMemory refreshes the provider index and commits canonical Markdown
// changes locally. It never deletes or promotes memories.
func MaintainMemory(policy MemoryPolicy) error {
	if !policy.Enabled {
		return nil
	}
	if err := policy.Validate(); err != nil {
		return err
	}
	binary, err := ResolveGBrainBinary(policy.GBrainBinary)
	if err != nil {
		return err
	}
	if _, err := runGBrain(policy, binary, 2*time.Minute, "import", filepath.Join(policy.Root, "brain"), "--no-embed", "--json"); err != nil {
		return err
	}
	brainDir := filepath.Join(policy.Root, "brain")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	add := exec.CommandContext(ctx, "git", "-C", brainDir, "add", ".")
	if output, err := add.CombinedOutput(); err != nil {
		return fmt.Errorf("staging canonical Clerk memory: %w: %s", err, strings.TrimSpace(string(output)))
	}
	status := exec.CommandContext(ctx, "git", "-C", brainDir, "diff", "--cached", "--quiet")
	if err := status.Run(); err == nil {
		return nil
	}
	commit := exec.CommandContext(ctx, "git", "-C", brainDir,
		"-c", "user.name=Gas Town Clerk",
		"-c", "user.email=clerk@localhost",
		"commit", "-m", "chore(memory): checkpoint Clerk memory")
	if output, err := commit.CombinedOutput(); err != nil {
		return fmt.Errorf("committing canonical Clerk memory: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

type memoryMatch struct {
	path    string
	content string
	score   int
}

func recallMarkdown(brainRoot, query string, limit int) (string, error) {
	terms := strings.Fields(strings.ToLower(query))
	var matches []memoryMatch
	err := filepath.WalkDir(brainRoot, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || filepath.Ext(path) != ".md" {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		lower := strings.ToLower(string(data))
		score := 0
		for _, term := range terms {
			score += strings.Count(lower, term)
		}
		if len(terms) == 0 && strings.Contains(path, string(filepath.Separator)+"projects"+string(filepath.Separator)) {
			score = 1
		}
		if score > 0 {
			matches = append(matches, memoryMatch{path: path, content: string(data), score: score})
		}
		return nil
	})
	if err != nil {
		return "", fmt.Errorf("searching canonical Clerk memory: %w", err)
	}
	sort.Slice(matches, func(i, j int) bool {
		if matches[i].score != matches[j].score {
			return matches[i].score > matches[j].score
		}
		return matches[i].path > matches[j].path
	})
	if len(matches) > limit {
		matches = matches[:limit]
	}
	if len(matches) == 0 {
		return "", nil
	}
	var out strings.Builder
	out.WriteString("\n## Relevant private Clerk memory\n\n")
	for _, match := range matches {
		title, content := memoryDocumentSummary(match.content)
		fmt.Fprintf(&out, "- **%s**: %s\n", title, content)
	}
	return out.String(), nil
}

func memoryDocumentSummary(document string) (string, string) {
	title := "Memory"
	var body []string
	scanner := bufio.NewScanner(strings.NewReader(document))
	inFrontMatter := false
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "---" {
			inFrontMatter = !inFrontMatter
			continue
		}
		if inFrontMatter && strings.HasPrefix(line, "title:") {
			title = strings.Trim(strings.TrimSpace(strings.TrimPrefix(line, "title:")), `"`)
			continue
		}
		if !inFrontMatter && line != "" && !strings.HasPrefix(line, "#") {
			body = append(body, line)
		}
	}
	content := strings.Join(body, " ")
	if len(content) > 400 {
		content = content[:400] + "…"
	}
	return title, content
}

func containsMemoryType(value string) bool {
	for _, item := range memoryTypes {
		if item == value {
			return true
		}
	}
	return false
}

func memorySlug(value string) string {
	slug := strings.Trim(memorySlugPattern.ReplaceAllString(strings.ToLower(value), "-"), "-")
	if slug == "" {
		slug = "memory"
	}
	if len(slug) > 60 {
		slug = strings.TrimRight(slug[:60], "-")
	}
	return slug
}

func memoryResolver(agentID string) string {
	return fmt.Sprintf(`# Clerk Private Memory

Agent ID: %s

Lookup order:

1. Search this private memory for Clerk experiences, decisions, corrections, preferences, and open work.
2. If configured, search shared company memory read-only.
3. Use external sources only after checking relevant memory.

Write only durable, sourced knowledge. Never store credentials, secret values,
raw chain-of-thought, every conversation message, or unverified guesses as fact.
Private-to-shared promotion requires explicit user approval of the exact content.
`, agentID)
}

const memorySchema = `# Clerk Memory Schema

Every memory must include:

- title
- type
- source
- source_kind: observation, user_statement, or inference
- recorded_at

Directories:

- experiences: dated observations from Clerk work
- decisions: decisions made by or for this Clerk
- lessons: reusable lessons from outcomes and corrections
- preferences: operating preferences specific to this Clerk
- projects: active private project context and open threads
- people and companies: relationship context only when needed
- inbox: temporary unclassified captures
- archive: inactive memories; never deleted automatically
`
