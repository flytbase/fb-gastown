// ABOUTME: Command to completely uninstall Gas Town from the system.
// ABOUTME: Removes shell integration, wrappers, state, and optionally workspace.

package cmd

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"github.com/steveyegge/gastown/internal/clerk"
	"github.com/steveyegge/gastown/internal/shell"
	"github.com/steveyegge/gastown/internal/state"
	"github.com/steveyegge/gastown/internal/style"
	"github.com/steveyegge/gastown/internal/workspace"
	"github.com/steveyegge/gastown/internal/wrappers"
)

var (
	uninstallWorkspace        bool
	uninstallForce            bool
	uninstallPurgeClerkMemory bool
)

var uninstallCmd = &cobra.Command{
	Use:     "uninstall",
	GroupID: GroupConfig,
	Short:   "Remove Gas Town from the system",
	Long: `Completely remove Gas Town from the system.

By default, removes:
  - Shell integration (~/.zshrc or ~/.bashrc)
  - Wrapper scripts (~/bin/gt-codex, ~/bin/gt-gemini, ~/bin/gt-opencode)
  - State directory (~/.local/state/gastown/)
  - Config directory (~/.config/gastown/)
  - Cache directory (~/.cache/gastown/)

The workspace (e.g., ~/gt) is NOT removed unless --workspace is specified.
Private Clerk memory is always preserved unless --purge-clerk-memory is explicit.

Use --force to skip confirmation prompts.

Examples:
  gt uninstall                    # Remove Gas Town, keep workspace
  gt uninstall --workspace        # Also remove workspace directory
  gt uninstall --force            # Skip confirmation`,
	RunE: runUninstall,
}

func init() {
	uninstallCmd.Flags().BoolVar(&uninstallWorkspace, "workspace", false,
		"Also remove the workspace directory (DESTRUCTIVE)")
	uninstallCmd.Flags().BoolVarP(&uninstallForce, "force", "f", false,
		"Skip confirmation prompts")
	uninstallCmd.Flags().BoolVar(&uninstallPurgeClerkMemory, "purge-clerk-memory", false,
		"Permanently delete this Town's private Clerk memory")
	rootCmd.AddCommand(uninstallCmd)
}

func runUninstall(cmd *cobra.Command, args []string) error {
	workspaceDir := findWorkspaceForUninstall()
	memoryRoot := ""
	var memoryPolicy clerk.MemoryPolicy
	registeredMemories, err := clerk.RegisteredMemoryPolicies()
	if err != nil {
		return err
	}
	if workspaceDir != "" {
		if policy, err := clerk.LoadPolicy(workspaceDir); err == nil && policy.Memory.Root != "" {
			memoryPolicy = policy.Memory
			memoryRoot = policy.Memory.Root
		}
	}
	if memoryRoot == "" && workspaceDir != "" {
		for _, registered := range registeredMemories {
			if samePath(registered.TownRoot, workspaceDir) {
				memoryPolicy = clerk.MemoryPolicy{
					Enabled:  true,
					Provider: clerk.MemoryProviderGBrain,
					ID:       registered.AgentID,
					Root:     registered.Root,
				}
				memoryRoot = registered.Root
				break
			}
		}
	}
	if uninstallPurgeClerkMemory && memoryRoot == "" {
		return fmt.Errorf("--purge-clerk-memory requires an identifiable current Town with managed Clerk memory")
	}
	deletionTargets := []string{state.StateDir(), state.ConfigDir(), state.CacheDir()}
	if uninstallWorkspace && workspaceDir != "" {
		deletionTargets = append(deletionTargets, workspaceDir)
	}
	if err := validateClerkMemoryUninstall(registeredMemories, memoryRoot, uninstallPurgeClerkMemory, deletionTargets); err != nil {
		return err
	}
	if memoryRoot != "" && !uninstallPurgeClerkMemory {
		for _, target := range deletionTargets {
			if pathWithin(memoryRoot, target) {
				return fmt.Errorf("private Clerk memory %s is inside uninstall target %s; move it first or explicitly use --purge-clerk-memory", memoryRoot, target)
			}
		}
	}
	if !uninstallForce {
		fmt.Println("This will remove Gas Town from your system.")
		fmt.Println()
		fmt.Println("The following will be removed:")
		fmt.Printf("  • Shell integration (%s)\n", shell.RCFilePath(shell.DetectShell()))
		fmt.Printf("  • Wrapper scripts (%s)\n", wrappers.BinDir())
		fmt.Printf("  • State directory (%s)\n", state.StateDir())
		fmt.Printf("  • Config directory (%s)\n", state.ConfigDir())
		fmt.Printf("  • Cache directory (%s)\n", state.CacheDir())
		if memoryRoot != "" && !uninstallPurgeClerkMemory {
			fmt.Printf("  • Private Clerk memory will be preserved (%s)\n", memoryRoot)
		}
		if memoryRoot != "" && uninstallPurgeClerkMemory {
			fmt.Printf("  %s PRIVATE CLERK MEMORY WILL BE DELETED (%s)\n", style.Warning.Render("⚠"), memoryRoot)
		}

		if uninstallWorkspace {
			fmt.Println()
			fmt.Printf("  %s WORKSPACE WILL BE DELETED\n", style.Warning.Render("⚠"))
			fmt.Println("     This cannot be undone!")
		}

		fmt.Println()
		fmt.Print("Continue? [y/N] ")

		reader := bufio.NewReader(os.Stdin)
		response, _ := reader.ReadString('\n')
		response = strings.TrimSpace(strings.ToLower(response))

		if response != "y" && response != "yes" {
			fmt.Println("Aborted.")
			return nil
		}
	}

	var errors []string

	fmt.Println()
	fmt.Println("Removing Gas Town...")
	if uninstallPurgeClerkMemory && memoryRoot != "" {
		if err := clerk.PurgeMemory(memoryPolicy, workspaceDir); err != nil {
			return err
		}
		if err := clerk.UnregisterMemoryRoot(memoryRoot); err != nil {
			return fmt.Errorf("private Clerk memory was purged but its registry entry could not be removed: %w", err)
		}
		fmt.Printf("  %s Removed private Clerk memory: %s\n", style.Success.Render("✓"), memoryRoot)
	}

	if err := shell.Remove(); err != nil {
		errors = append(errors, fmt.Sprintf("shell integration: %v", err))
	} else {
		fmt.Printf("  %s Removed shell integration\n", style.Success.Render("✓"))
	}

	if err := wrappers.Remove(); err != nil {
		errors = append(errors, fmt.Sprintf("wrapper scripts: %v", err))
	} else {
		fmt.Printf("  %s Removed wrapper scripts\n", style.Success.Render("✓"))
	}

	if err := os.RemoveAll(state.StateDir()); err != nil && !os.IsNotExist(err) {
		errors = append(errors, fmt.Sprintf("state directory: %v", err))
	} else {
		fmt.Printf("  %s Removed state directory\n", style.Success.Render("✓"))
	}

	if err := os.RemoveAll(state.ConfigDir()); err != nil && !os.IsNotExist(err) {
		errors = append(errors, fmt.Sprintf("config directory: %v", err))
	} else {
		fmt.Printf("  %s Removed config directory\n", style.Success.Render("✓"))
	}

	if err := os.RemoveAll(state.CacheDir()); err != nil && !os.IsNotExist(err) {
		errors = append(errors, fmt.Sprintf("cache directory: %v", err))
	} else {
		fmt.Printf("  %s Removed cache directory\n", style.Success.Render("✓"))
	}

	if uninstallWorkspace {
		if workspaceDir != "" {
			if err := os.RemoveAll(workspaceDir); err != nil {
				errors = append(errors, fmt.Sprintf("workspace: %v", err))
			} else {
				fmt.Printf("  %s Removed workspace: %s\n", style.Success.Render("✓"), workspaceDir)
			}
		}
	}

	if len(errors) > 0 {
		fmt.Println()
		fmt.Printf("%s Some components could not be removed:\n", style.Warning.Render("⚠"))
		for _, e := range errors {
			fmt.Printf("  • %s\n", e)
		}
		return fmt.Errorf("uninstall incomplete")
	}

	fmt.Println()
	fmt.Printf("%s Gas Town has been uninstalled\n", style.Success.Render("✓"))
	fmt.Println()
	fmt.Println("To reinstall, run:")
	fmt.Printf("  %s\n", style.Dim.Render("go install github.com/steveyegge/gastown/cmd/gt@latest"))
	fmt.Printf("  %s\n", style.Dim.Render("gt install ~/gt --shell"))

	return nil
}

func validateClerkMemoryUninstall(registered []clerk.MemoryRegistryEntry, purgeRoot string, purge bool, deletionTargets []string) error {
	for _, memory := range registered {
		if purge && samePath(memory.Root, purgeRoot) {
			continue
		}
		for _, target := range deletionTargets {
			if pathWithin(memory.Root, target) {
				return fmt.Errorf("private Clerk memory %s for Town %s is inside uninstall target %s; move it before uninstalling", memory.Root, memory.TownRoot, target)
			}
		}
	}
	return nil
}

func samePath(left, right string) bool {
	if strings.TrimSpace(left) == "" || strings.TrimSpace(right) == "" {
		return false
	}
	return filepath.Clean(left) == filepath.Clean(right)
}

func pathWithin(path, root string) bool {
	relative, err := filepath.Rel(filepath.Clean(root), filepath.Clean(path))
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

func findWorkspaceForUninstall() string {
	if cwd, err := os.Getwd(); err == nil {
		if root, findErr := workspace.Find(cwd); findErr == nil && root != "" {
			return root
		}
	}
	for _, envName := range []string{"GT_TOWN_ROOT", "GT_ROOT"} {
		if root := strings.TrimSpace(os.Getenv(envName)); root != "" {
			if found, err := workspace.Find(root); err == nil && found != "" {
				return found
			}
		}
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}

	candidates := []string{
		filepath.Join(home, "gt"),
		filepath.Join(home, "gastown"),
	}

	for _, path := range candidates {
		mayorDir := filepath.Join(path, "mayor")
		if _, err := os.Stat(mayorDir); err == nil {
			return path
		}
	}

	return ""
}
