package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/steveyegge/gastown/internal/clerk"
	"github.com/steveyegge/gastown/internal/workspace"
)

var (
	clerkMemoryType       string
	clerkMemoryTitle      string
	clerkMemorySource     string
	clerkMemorySourceKind string
	clerkMemoryLimit      int
)

var clerkMemoryCmd = &cobra.Command{
	Use:   "memory",
	Short: "Use Clerk's private, Town-managed long-term memory",
	RunE:  requireSubcommand,
}

var clerkMemoryStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show private Clerk memory health and isolation paths",
	RunE: func(_ *cobra.Command, _ []string) error {
		_, policy, err := loadClerkMemoryPolicy()
		if err != nil {
			return err
		}
		health := clerk.HealthMemory(policy)
		if !health.Enabled {
			fmt.Println("Clerk memory: disabled")
			return nil
		}
		fmt.Printf("Clerk memory: enabled (%s)\n", policy.Provider)
		fmt.Printf("Agent ID: %s\n", policy.ID)
		fmt.Printf("Canonical Markdown: %s\n", health.CanonicalRoot)
		fmt.Printf("Private GBrain home: %s\n", health.GBrainHome)
		fmt.Printf("Search mode: %s\n", policy.SearchMode)
		fmt.Printf("Shared access: read-only=%t write=%t\n", policy.SharedReadOnly, policy.SharedWrite)
		fmt.Printf("Layout: %t\nProvider: %t\nEmbedding: %s\n", health.LayoutOK, health.ProviderOK, health.EmbeddingStatus)
		if health.Warning != "" {
			return fmt.Errorf("Clerk memory health warning: %s", health.Warning)
		}
		return nil
	},
}

var clerkMemoryRecallCmd = &cobra.Command{
	Use:   "recall [query]",
	Short: "Recall bounded private Clerk context",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(_ *cobra.Command, args []string) error {
		_, policy, err := loadClerkMemoryPolicy()
		if err != nil {
			return err
		}
		query := "open pending active preference decision"
		if len(args) == 1 {
			query = args[0]
		}
		output, err := clerk.RecallMemory(policy, query, clerkMemoryLimit)
		if err != nil {
			return err
		}
		fmt.Print(output)
		return nil
	},
}

var clerkMemoryRecallHookCmd = &cobra.Command{
	Use:    "recall-hook",
	Short:  "Inject bounded memory relevant to a submitted Clerk prompt",
	Hidden: true,
	RunE: func(_ *cobra.Command, _ []string) error {
		_, policy, err := loadClerkMemoryPolicy()
		if err != nil || !policy.Enabled {
			return nil // Agent hooks fail open.
		}
		var payload struct {
			Prompt string `json:"prompt"`
		}
		_ = json.NewDecoder(io.LimitReader(os.Stdin, 2<<20)).Decode(&payload)
		if strings.TrimSpace(payload.Prompt) == "" {
			return nil
		}
		output, recallErr := clerk.RecallMemory(policy, payload.Prompt, clerkMemoryLimit)
		if recallErr == nil {
			fmt.Print(output)
		}
		return nil
	},
}

var clerkMemoryCaptureCmd = &cobra.Command{
	Use:   "capture <content>",
	Short: "Capture one sourced durable memory in canonical Markdown",
	Args:  cobra.ExactArgs(1),
	RunE: func(_ *cobra.Command, args []string) error {
		_, policy, err := loadClerkMemoryPolicy()
		if err != nil {
			return err
		}
		title := strings.TrimSpace(clerkMemoryTitle)
		if title == "" {
			title = strings.TrimSpace(args[0])
			if len(title) > 80 {
				title = title[:80]
			}
		}
		result, err := clerk.CaptureMemory(policy, clerk.MemoryRecord{
			Type:       clerkMemoryType,
			Title:      title,
			Content:    args[0],
			Source:     clerkMemorySource,
			SourceKind: clerkMemorySourceKind,
			RecordedAt: time.Now().UTC(),
		})
		if err != nil {
			return err
		}
		fmt.Printf("Captured private Clerk memory: %s\n", result.Path)
		if result.ProviderError != "" {
			fmt.Printf("Provider indexing deferred: %s\n", result.ProviderError)
		}
		return nil
	},
}

var clerkMemoryCaptureHookCmd = &cobra.Command{
	Use:    "capture-hook",
	Short:  "Checkpoint memories selected by the Clerk during the completed turn",
	Hidden: true,
	RunE: func(_ *cobra.Command, _ []string) error {
		_, policy, err := loadClerkMemoryPolicy()
		if err != nil || !policy.Enabled || !policy.AutomaticCapture {
			return nil // Agent hooks fail open.
		}
		// Go performs lifecycle transport only. The Clerk agent makes the
		// durable-memory judgment and calls `gt clerk memory capture` during
		// its turn; this hook refreshes the index and Git checkpoint.
		_ = clerk.MaintainMemory(policy)
		return nil
	},
}

var clerkMemoryGuardCommandCmd = &cobra.Command{
	Use:    "guard-command",
	Short:  "Block raw GBrain commands from the Clerk role",
	Hidden: true,
	RunE: func(_ *cobra.Command, _ []string) error {
		var payload struct {
			ToolInput struct {
				Command string `json:"command"`
			} `json:"tool_input"`
		}
		if err := json.NewDecoder(io.LimitReader(os.Stdin, 2<<20)).Decode(&payload); err != nil {
			return fmt.Errorf("BLOCKED: could not validate Clerk shell command: %w", err)
		}
		if _, policy, err := loadClerkMemoryPolicy(); err == nil && policy.Enabled {
			return clerk.GuardClerkShellCommand(payload.ToolInput.Command)
		}
		return clerk.GuardRawGBrainCommand(payload.ToolInput.Command)
	},
}

var clerkMemoryMaintainCmd = &cobra.Command{
	Use:   "maintain",
	Short: "Refresh the private index and checkpoint canonical Markdown",
	RunE: func(_ *cobra.Command, _ []string) error {
		_, policy, err := loadClerkMemoryPolicy()
		if err != nil {
			return err
		}
		if err := clerk.MaintainMemory(policy); err != nil {
			return err
		}
		fmt.Println("Private Clerk memory maintained.")
		return nil
	},
}

func init() {
	clerkMemoryRecallCmd.Flags().IntVar(&clerkMemoryLimit, "limit", 5, "Maximum recalled memories")
	clerkMemoryRecallHookCmd.Flags().IntVar(&clerkMemoryLimit, "limit", 5, "Maximum recalled memories")
	clerkMemoryCaptureCmd.Flags().StringVar(&clerkMemoryType, "type", clerk.MemoryTypeInbox, "Memory type")
	clerkMemoryCaptureCmd.Flags().StringVar(&clerkMemoryTitle, "title", "", "Short memory title")
	clerkMemoryCaptureCmd.Flags().StringVar(&clerkMemorySource, "source", "manual Clerk capture", "Memory source")
	clerkMemoryCaptureCmd.Flags().StringVar(&clerkMemorySourceKind, "source-kind", clerk.MemorySourceUserStatement, "observation, user_statement, or inference")
	clerkMemoryCmd.AddCommand(clerkMemoryStatusCmd, clerkMemoryRecallCmd, clerkMemoryRecallHookCmd, clerkMemoryGuardCommandCmd)
	clerkMemoryCmd.AddCommand(clerkMemoryCaptureCmd, clerkMemoryCaptureHookCmd, clerkMemoryMaintainCmd)
	clerkCmd.AddCommand(clerkMemoryCmd)
}

func loadClerkMemoryPolicy() (string, clerk.MemoryPolicy, error) {
	townRoot, err := workspace.FindFromCwdOrError()
	if err != nil {
		return "", clerk.MemoryPolicy{}, err
	}
	policy, err := clerk.LoadPolicy(townRoot)
	if err != nil {
		return "", clerk.MemoryPolicy{}, err
	}
	return townRoot, policy.Memory, nil
}

func captureClerkEvent(townRoot, memoryType, title, content, source string) {
	policy, err := clerk.LoadPolicy(townRoot)
	if err != nil || !policy.Memory.Enabled || !policy.Memory.AutomaticCapture {
		return
	}
	_, _ = clerk.CaptureMemory(policy.Memory, clerk.MemoryRecord{
		Type:       memoryType,
		Title:      title,
		Content:    content,
		Source:     source,
		SourceKind: clerk.MemorySourceObservation,
		RecordedAt: time.Now().UTC(),
	})
}
