package cmd

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/steveyegge/gastown/internal/assayer"
	"github.com/steveyegge/gastown/internal/constants"
	"github.com/steveyegge/gastown/internal/session"
	"github.com/steveyegge/gastown/internal/style"
	"github.com/steveyegge/gastown/internal/tmux"
	"github.com/steveyegge/gastown/internal/workspace"
)

var (
	assayerAgentOverride string
	assayerMode          string
	assayerToken         string
	assayerTTL           time.Duration
)

var assayerCmd = &cobra.Command{
	Use:     "assayer",
	GroupID: GroupAgents,
	Short:   "Manage the supervised backend-assurance head",
	Long: `Assayer is a Town-scoped backend-testing Lead. Phase 1 uses response-only,
session-native sub-workers and requires a short-lived human capability before
every run. It cannot execute real backend tests or modify repositories.`,
	RunE: requireSubcommand,
}

var assayerStartCmd = &cobra.Command{
	Use:   "start",
	Short: "Start Assayer using the Town session backend",
	RunE: func(_ *cobra.Command, _ []string) error {
		manager, err := getAssayerManager()
		if err != nil {
			return err
		}
		if err := manager.Start(assayerAgentOverride); err != nil {
			if errors.Is(err, assayer.ErrAlreadyRunning) {
				return fmt.Errorf("Assayer is already running; use: gt assayer attach")
			}
			return err
		}
		fmt.Printf("%s Assayer started. Open it with: gt assayer attach\n", style.Bold.Render("✓"))
		return nil
	},
}

var assayerStopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Stop the Assayer session",
	RunE: func(_ *cobra.Command, _ []string) error {
		manager, err := getAssayerManager()
		if err != nil {
			return err
		}
		if err := manager.Stop(); err != nil {
			if errors.Is(err, assayer.ErrNotRunning) {
				return fmt.Errorf("Assayer is not running")
			}
			return err
		}
		fmt.Printf("%s Assayer stopped.\n", style.Bold.Render("✓"))
		return nil
	},
}

var assayerStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show Assayer session and policy status",
	RunE: func(_ *cobra.Command, _ []string) error {
		townRoot, err := workspace.FindFromCwdOrError()
		if err != nil {
			return err
		}
		policy, err := assayer.LoadPolicy(townRoot)
		if err != nil {
			return err
		}
		running, err := assayer.NewManager(townRoot).IsRunning()
		if err != nil {
			return err
		}
		fmt.Printf("Assayer: %s\n", map[bool]string{true: "running", false: "stopped"}[running])
		fmt.Printf("Phase: %s (real backend execution: %t)\n", policy.Phase, policy.Authority.RealBackendExecution)
		fmt.Printf("Run authorization: human capability=%t, mayor=%t\n", policy.Authority.HumanRunAuthorization, policy.Authority.MayorRunAuthorization)
		fmt.Printf("Post-worker automation: enabled=%t, mode=%s, advisory=%t\n", policy.Automation.PostWorker.Enabled, policy.Automation.PostWorker.Mode, !policy.Authority.RealBackendExecution)
		fmt.Printf("Context roots: %d\n", len(policy.ContextRoots))
		return nil
	},
}

var assayerAttachCmd = &cobra.Command{
	Use:   "attach",
	Short: "Attach to or focus the Assayer session",
	RunE: func(_ *cobra.Command, _ []string) error {
		manager, err := getAssayerManager()
		if err != nil {
			return err
		}
		return manager.Attach()
	},
}

var assayerWorkersCmd = &cobra.Command{
	Use:   "workers",
	Short: "List native Assayer sub-worker roles",
	RunE: func(_ *cobra.Command, _ []string) error {
		for _, worker := range assayer.Workers() {
			fmt.Printf("%-30s %s\n", worker.Kind, worker.Purpose)
		}
		return nil
	},
}

var assayerContextCmd = &cobra.Command{Use: "context", Short: "Manage read-only Assayer specification roots", RunE: requireSubcommand}

var assayerContextAddCmd = &cobra.Command{
	Use:   "add <absolute-directory>",
	Short: "Add an existing read-only backend assurance specification root",
	Args:  cobra.ExactArgs(1),
	RunE: func(_ *cobra.Command, args []string) error {
		if err := assayer.RequireNeutralHumanShell(); err != nil {
			return fmt.Errorf("Assayer context policy must be changed from a neutral human shell: %w", err)
		}
		townRoot, err := workspace.FindFromCwdOrError()
		if err != nil {
			return err
		}
		if err := assayer.AddContextRoot(townRoot, args[0]); err != nil {
			return err
		}
		fmt.Printf("%s Added read-only Assayer context root: %s\n", style.Bold.Render("✓"), args[0])
		return nil
	},
}

var assayerRequestCmd = &cobra.Command{
	Use:   "request <bead-id> <rig>",
	Short: "Create a bounded Phase 1 assurance request",
	Args:  cobra.ExactArgs(2),
	RunE: func(_ *cobra.Command, args []string) error {
		townRoot, err := workspace.FindFromCwdOrError()
		if err != nil {
			return err
		}
		request, err := assayer.NewAuthorizer(townRoot).Create(args[0], args[1], assayerMode)
		if err != nil {
			return err
		}
		fmt.Printf("Assayer request created: %s\n", request.ID)
		fmt.Printf("Bound request: %s → %s (%s)\n", request.BeadID, request.RigName, request.Mode)
		fmt.Printf("Human authorization required: gt assayer authorize %s\n", request.ID)
		return nil
	},
}

var assayerAuthorizeCmd = &cobra.Command{
	Use:   "authorize <request-id>",
	Short: "Issue a one-use Assayer run capability from a neutral human shell",
	Args:  cobra.ExactArgs(1),
	RunE: func(_ *cobra.Command, args []string) error {
		townRoot, err := workspace.FindFromCwdOrError()
		if err != nil {
			return err
		}
		token, request, err := assayer.NewAuthorizer(townRoot).Authorize(args[0], assayerTTL)
		if err != nil {
			return err
		}
		fmt.Printf("Authorized Assayer Phase 1 request %s until %s\n", request.ID, request.ExpiresAt.Format(time.RFC3339))
		fmt.Println("Capability (shown once):")
		fmt.Println(token)
		return nil
	},
}

var assayerRunCmd = &cobra.Command{
	Use:   "run <request-id>",
	Short: "Activate a response-only run using human capability or Mayor authority",
	Args:  cobra.ExactArgs(1),
	RunE: func(_ *cobra.Command, args []string) error {
		townRoot, err := workspace.FindFromCwdOrError()
		if err != nil {
			return err
		}
		manager := assayer.NewManager(townRoot)
		running, err := manager.IsRunning()
		if err != nil {
			return err
		}
		if !running {
			return fmt.Errorf("Assayer is not running; start it before consuming authorization with gt assayer start")
		}
		authorizer := assayer.NewAuthorizer(townRoot)
		var request *assayer.Request
		if assayerToken == "" {
			request, err = authorizer.ConsumeMayor(args[0])
		} else {
			request, err = authorizer.Consume(args[0], assayerToken)
		}
		if err != nil {
			return err
		}
		notice := fmt.Sprintf("Authorized Phase 1 backend-assurance request %s is ready. Inspect it with: gt assayer inspect %s", request.ID, request.ID)
		if err := tmux.NewTmux().NudgeSessionWithOpts(session.AssayerSessionName(), notice, tmux.NudgeOpts{TownRoot: townRoot}); err != nil {
			if recordErr := assayer.NewAuthorizer(townRoot).MarkDeliveryFailed(request.ID, err); recordErr != nil {
				return fmt.Errorf("delivering Assayer activation: %v (also failed to seal delivery failure: %w)", err, recordErr)
			}
			return err
		}
		fmt.Printf("%s Activated response-only Assayer request %s (authorized by %s)\n", style.Bold.Render("✓"), request.ID, request.AuthorizedBy)
		return nil
	},
}

var assayerInspectCmd = &cobra.Command{
	Use:   "inspect <request-id>",
	Short: "Print one Assayer request envelope",
	Args:  cobra.ExactArgs(1),
	RunE: func(_ *cobra.Command, args []string) error {
		townRoot, err := workspace.FindFromCwdOrError()
		if err != nil {
			return err
		}
		request, err := assayer.NewAuthorizer(townRoot).Get(args[0])
		if err != nil {
			return err
		}
		encoder := json.NewEncoder(os.Stdout)
		encoder.SetIndent("", "  ")
		return encoder.Encode(request)
	},
}

var assayerGuardBashCmd = &cobra.Command{
	Use:    "guard-bash",
	Short:  "Enforce the Phase 1 Assayer shell allowlist",
	Hidden: true,
	RunE: func(_ *cobra.Command, _ []string) error {
		var input struct {
			ToolInput struct {
				Command string `json:"command"`
			} `json:"tool_input"`
		}
		if err := json.NewDecoder(os.Stdin).Decode(&input); err != nil {
			return NewSilentExit(2)
		}
		if !assayerBashAllowed(input.ToolInput.Command) {
			fmt.Fprintln(os.Stderr, "BLOCKED: Assayer Phase 1 permits only read-only Assayer inspection commands.")
			return NewSilentExit(2)
		}
		return nil
	},
}

func init() {
	assayerStartCmd.Flags().StringVar(&assayerAgentOverride, "agent", "", "Agent alias to run Assayer with")
	assayerRequestCmd.Flags().StringVar(&assayerMode, "mode", "harden", "Assurance mode")
	assayerAuthorizeCmd.Flags().DurationVar(&assayerTTL, "ttl", 0, "Authorization lifetime (default from assayer.json)")
	assayerRunCmd.Flags().StringVar(&assayerToken, "token", "", "One-use human authorization capability")
	assayerContextCmd.AddCommand(assayerContextAddCmd)
	assayerCmd.AddCommand(assayerStartCmd, assayerStopCmd, assayerStatusCmd, assayerAttachCmd)
	assayerCmd.AddCommand(assayerWorkersCmd, assayerContextCmd, assayerRequestCmd, assayerAuthorizeCmd, assayerRunCmd, assayerInspectCmd, assayerGuardBashCmd)
	rootCmd.AddCommand(assayerCmd)
}

func assayerBashAllowed(command string) bool {
	command = strings.TrimSpace(command)
	if command == "" || strings.ContainsAny(command, ";&|><\n\r`$\\") {
		return false
	}
	fields := strings.Fields(command)
	if len(fields) == 2 && fields[0] == "gt" && fields[1] == "prime" {
		return true
	}
	if len(fields) == 3 && fields[0] == "gt" && fields[1] == "assayer" {
		return fields[2] == "status" || fields[2] == "workers"
	}
	if len(fields) == 4 && fields[0] == "gt" && fields[1] == "assayer" && fields[2] == "inspect" {
		decoded, err := hex.DecodeString(fields[3])
		return err == nil && len(decoded) == 16
	}
	return false
}

func assayerDispatchDenied() bool {
	return os.Getenv("GT_ROLE") == constants.RoleAssayer || os.Getenv("BD_ACTOR") == constants.RoleAssayer
}

func getAssayerManager() (*assayer.Manager, error) {
	townRoot, err := workspace.FindFromCwdOrError()
	if err != nil {
		return nil, fmt.Errorf("not in a Gas Town workspace: %w", err)
	}
	return assayer.NewManager(townRoot), nil
}
