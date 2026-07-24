package cmd

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"github.com/steveyegge/gastown/internal/clerk"
	"golang.org/x/term"
)

type clerkMemoryInstall struct {
	Apply  bool
	Policy clerk.MemoryPolicy
}

func resolveClerkMemoryInstall(cmd *cobra.Command, townRoot, townName string) (clerkMemoryInstall, error) {
	var decision clerkMemoryInstall
	explicitChoice := cmd.Flags().Changed("clerk-memory")
	choice := strings.ToLower(strings.TrimSpace(installClerkMemory))

	existing, existingErr := clerk.LoadPolicy(townRoot)
	hasStoredPolicy := existingErr == nil && existing.Memory.Root != "" && existing.Memory.ID != ""
	dependentOptionsChanged := cmd.Flags().Changed("clerk-memory-root") ||
		cmd.Flags().Changed("gbrain-search-mode") ||
		cmd.Flags().Changed("shared-gbrain-readonly")
	if choice == "ask" && hasStoredPolicy && !dependentOptionsChanged {
		decision.Apply = true
		decision.Policy = existing.Memory
		return decision, nil
	}
	if choice == "ask" && hasStoredPolicy && dependentOptionsChanged {
		if !existing.Memory.Enabled {
			return decision, fmt.Errorf("Clerk memory options require --clerk-memory gbrain when stored memory is disabled")
		}
		choice = clerk.MemoryProviderGBrain
	}
	switch choice {
	case "ask":
		if !term.IsTerminal(int(os.Stdin.Fd())) {
			return decision, nil
		}
		fmt.Println()
		fmt.Println("Optional Clerk long-term memory")
		fmt.Println("  Gas Town can install and manage a private, isolated GBrain for Clerk.")
		fmt.Println("  Canonical memories remain private Markdown and are preserved on uninstall.")
		if !promptInstallYesNo("Enable private long-term Clerk memory?", true) {
			return decision, nil
		}
		choice = clerk.MemoryProviderGBrain
	case "none":
		decision.Apply = explicitChoice
		decision.Policy = clerk.MemoryPolicy{Enabled: false, SharedWrite: false}
		if hasStoredPolicy {
			decision.Policy = existing.Memory
			decision.Policy.Enabled = false
			decision.Policy.SharedWrite = false
		}
		if cmd.Flags().Changed("clerk-memory-root") || cmd.Flags().Changed("gbrain-search-mode") || cmd.Flags().Changed("shared-gbrain-readonly") {
			return decision, fmt.Errorf("Clerk memory options require --clerk-memory gbrain")
		}
		return decision, nil
	case clerk.MemoryProviderGBrain:
	default:
		return decision, fmt.Errorf("invalid --clerk-memory %q (want ask, none, or gbrain)", installClerkMemory)
	}

	policy := clerk.DefaultMemoryPolicy(townRoot, townName)
	if hasStoredPolicy {
		policy = existing.Memory
	}
	policy.Enabled = true
	policy.Provider = clerk.MemoryProviderGBrain
	policy.SharedWrite = false
	mode := strings.ToLower(strings.TrimSpace(installGBrainMode))
	modeSelected := cmd.Flags().Changed("gbrain-search-mode")
	switch mode {
	case clerk.MemorySearchConservative, clerk.MemorySearchBalanced, clerk.MemorySearchTokenMax:
	default:
		return decision, fmt.Errorf("invalid --gbrain-search-mode %q", installGBrainMode)
	}
	if !hasStoredPolicy && !modeSelected && term.IsTerminal(int(os.Stdin.Fd())) {
		fmt.Println("  Search modes: conservative (4K/10), balanced (12K/25), tokenmax (uncapped/50).")
		fmt.Printf("  Search mode [%s]: ", clerk.MemorySearchConservative)
		reader := bufio.NewReader(os.Stdin)
		if answer, _ := reader.ReadString('\n'); strings.TrimSpace(answer) != "" {
			mode = strings.ToLower(strings.TrimSpace(answer))
			modeSelected = true
			switch mode {
			case clerk.MemorySearchConservative, clerk.MemorySearchBalanced, clerk.MemorySearchTokenMax:
			default:
				return decision, fmt.Errorf("invalid GBrain search mode %q", mode)
			}
		}
	}
	if modeSelected || policy.SearchMode == "" {
		policy.SearchMode = mode
	}

	if installMemoryRoot != "" {
		root, err := expandInstallMemoryRoot(installMemoryRoot)
		if err != nil {
			return decision, err
		}
		policy.Root = root
	}
	home, _ := os.UserHomeDir()
	sharedExists := home != ""
	if sharedExists {
		_, err := os.Stat(filepath.Join(home, ".gbrain"))
		sharedExists = err == nil
	}
	if cmd.Flags().Changed("shared-gbrain-readonly") {
		if installSharedMemory {
			if !sharedExists {
				return decision, fmt.Errorf("--shared-gbrain-readonly requested but %s does not exist", filepath.Join(home, ".gbrain"))
			}
			policy.SharedReadOnly = true
			policy.SharedRoot = home // GBrain resolves <GBRAIN_HOME>/.gbrain.
		} else {
			policy.SharedReadOnly = false
			policy.SharedRoot = ""
		}
	} else if !hasStoredPolicy && sharedExists && term.IsTerminal(int(os.Stdin.Fd())) && promptInstallYesNo("Allow read-only search of the existing shared GBrain?", false) {
		policy.SharedReadOnly = true
		policy.SharedRoot = home
	}
	decision.Apply = true
	decision.Policy = policy
	return decision, nil
}

func promptInstallYesNo(question string, defaultYes bool) bool {
	suffix := "[y/N]"
	if defaultYes {
		suffix = "[Y/n]"
	}
	fmt.Printf("  %s %s ", question, suffix)
	reader := bufio.NewReader(os.Stdin)
	answer, _ := reader.ReadString('\n')
	answer = strings.ToLower(strings.TrimSpace(answer))
	if answer == "" {
		return defaultYes
	}
	return answer == "y" || answer == "yes"
}

func expandInstallMemoryRoot(root string) (string, error) {
	root = strings.TrimSpace(root)
	if strings.HasPrefix(root, "~") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolving Clerk memory root: %w", err)
		}
		root = filepath.Join(home, strings.TrimPrefix(root, "~/"))
	}
	path, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("resolving Clerk memory root: %w", err)
	}
	return path, nil
}
