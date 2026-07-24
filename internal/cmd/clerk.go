package cmd

import (
	"errors"
	"fmt"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"
	"github.com/steveyegge/gastown/internal/clerk"
	"github.com/steveyegge/gastown/internal/style"
	"github.com/steveyegge/gastown/internal/workspace"
)

var (
	clerkAgentOverride string
	clerkToken         string
	clerkTTL           time.Duration

	clerkSlingArgs       string
	clerkSlingVars       []string
	clerkSlingMerge      string
	clerkSlingBaseBranch string
	clerkSlingAccount    string
	clerkSlingAgent      string
	clerkSlingFormula    string
	clerkSlingForce      bool
	clerkSlingNoConvoy   bool
	clerkSlingNoMerge    bool
	clerkSlingReviewOnly bool
	clerkSlingProfile    string
)

var clerkCmd = &cobra.Command{
	Use:     "clerk",
	GroupID: GroupAgents,
	Short:   "Manage the supervised Clerk task coordinator",
	Long: `The Clerk is a non-autonomous task coordinator.

It may prepare native Polecat dispatch requests, but every dispatch requires
a short-lived, one-use capability issued by the human from a neutral terminal.
The authorized path reuses native sling, worktrees, hooks, convoys, Witness,
Refinery, merge, and cleanup.`,
	RunE: requireSubcommand,
}

var clerkStartCmd = &cobra.Command{
	Use:   "start",
	Short: "Start Clerk using the Town session backend",
	RunE: func(_ *cobra.Command, _ []string) error {
		manager, err := getClerkManager()
		if err != nil {
			return err
		}
		if err := manager.Start(clerkAgentOverride); err != nil {
			if errors.Is(err, clerk.ErrAlreadyRunning) {
				return fmt.Errorf("Clerk is already running; use: gt clerk attach")
			}
			return err
		}
		fmt.Printf("%s Clerk started. Open it with: gt clerk attach\n", style.Bold.Render("✓"))
		return nil
	},
}

var clerkStopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Stop the Clerk session",
	RunE: func(_ *cobra.Command, _ []string) error {
		manager, err := getClerkManager()
		if err != nil {
			return err
		}
		if err := manager.Stop(); err != nil {
			if errors.Is(err, clerk.ErrNotRunning) {
				return fmt.Errorf("Clerk is not running")
			}
			return err
		}
		fmt.Printf("%s Clerk stopped.\n", style.Bold.Render("✓"))
		return nil
	},
}

var clerkStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show Clerk session status",
	RunE: func(_ *cobra.Command, _ []string) error {
		manager, err := getClerkManager()
		if err != nil {
			return err
		}
		info, err := manager.Status()
		if err != nil {
			if errors.Is(err, clerk.ErrNotRunning) {
				fmt.Println("Clerk: stopped")
				return nil
			}
			return err
		}
		fmt.Printf("Clerk: running (%s)\n", info.Name)
		return nil
	},
}

var clerkAttachCmd = &cobra.Command{
	Use:   "attach",
	Short: "Attach to or focus the Clerk session",
	RunE: func(_ *cobra.Command, _ []string) error {
		manager, err := getClerkManager()
		if err != nil {
			return err
		}
		return manager.Attach()
	},
}

var clerkRequestCmd = &cobra.Command{
	Use:   "request <bead-id> <rig>",
	Short: "Create a supervised native Polecat dispatch request",
	Args:  cobra.ExactArgs(2),
	RunE: func(_ *cobra.Command, args []string) error {
		townRoot, err := workspace.FindFromCwdOrError()
		if err != nil {
			return err
		}
		request, err := clerk.NewAuthorizer(townRoot).Create(args[0], args[1])
		if err != nil {
			return err
		}
		fmt.Printf("Clerk request created: %s\n", request.ID)
		fmt.Printf("Bound dispatch: %s → %s\n", request.BeadID, request.RigName)
		fmt.Printf("Human authorization required: gt clerk authorize %s\n", request.ID)
		return nil
	},
}

var clerkAuthorizeCmd = &cobra.Command{
	Use:   "authorize <request-id>",
	Short: "Issue a one-use Clerk dispatch capability from a neutral human shell",
	Args:  cobra.ExactArgs(1),
	RunE: func(_ *cobra.Command, args []string) error {
		townRoot, err := workspace.FindFromCwdOrError()
		if err != nil {
			return err
		}
		token, request, err := clerk.NewAuthorizer(townRoot).Authorize(args[0], clerkTTL)
		if err != nil {
			return err
		}
		fmt.Printf("Authorized %s → %s until %s\n", request.BeadID, request.RigName, request.ExpiresAt.Format(time.RFC3339))
		fmt.Println("Capability (shown once):")
		fmt.Println(token)
		return nil
	},
}

var clerkSlingCmd = &cobra.Command{
	Use:   "sling <request-id>",
	Short: "Consume human authorization and dispatch through native sling",
	Args:  cobra.ExactArgs(1),
	RunE:  runClerkSling,
}

func init() {
	clerkStartCmd.Flags().StringVar(&clerkAgentOverride, "agent", "", "Agent alias to run the Clerk with")
	clerkAuthorizeCmd.Flags().DurationVar(&clerkTTL, "ttl", 10*time.Minute, "Authorization lifetime")
	clerkSlingCmd.Flags().StringVar(&clerkToken, "token", "", "One-use human authorization capability")
	clerkSlingCmd.Flags().StringVarP(&clerkSlingArgs, "args", "a", "", "Instructions for the Polecat")
	clerkSlingCmd.Flags().StringArrayVar(&clerkSlingVars, "var", nil, "Formula variable (key=value), repeatable")
	clerkSlingCmd.Flags().StringVar(&clerkSlingMerge, "merge", "", "Merge strategy: direct, mr, or local")
	clerkSlingCmd.Flags().StringVar(&clerkSlingBaseBranch, "base-branch", "", "Override Polecat base branch")
	clerkSlingCmd.Flags().StringVar(&clerkSlingAccount, "account", "", "Runtime account handle")
	clerkSlingCmd.Flags().StringVar(&clerkSlingAgent, "agent", "", "Worker runtime/agent override")
	clerkSlingCmd.Flags().StringVar(&clerkSlingFormula, "formula", "", "Formula override")
	clerkSlingCmd.Flags().BoolVar(&clerkSlingForce, "force", false, "Force an otherwise blocked re-dispatch")
	clerkSlingCmd.Flags().BoolVar(&clerkSlingNoConvoy, "no-convoy", false, "Skip automatic convoy creation")
	clerkSlingCmd.Flags().BoolVar(&clerkSlingNoMerge, "no-merge", false, "Keep work off the merge queue")
	clerkSlingCmd.Flags().BoolVar(&clerkSlingReviewOnly, "review-only", false, "Review and report without merging")
	clerkSlingCmd.Flags().StringVar(&clerkSlingProfile, "profile", "", "Clerk routing profile from clerk.json")

	clerkCmd.AddCommand(clerkStartCmd, clerkStopCmd, clerkStatusCmd, clerkAttachCmd)
	clerkCmd.AddCommand(clerkRequestCmd, clerkAuthorizeCmd, clerkSlingCmd)
	rootCmd.AddCommand(clerkCmd)
}

func getClerkManager() (*clerk.Manager, error) {
	townRoot, err := workspace.FindFromCwdOrError()
	if err != nil {
		return nil, fmt.Errorf("not in a Gas Town workspace: %w", err)
	}
	return clerk.NewManager(townRoot), nil
}

func runClerkSling(_ *cobra.Command, args []string) (retErr error) {
	if clerkToken == "" {
		return fmt.Errorf("--token is required")
	}
	townRoot, err := workspace.FindFromCwdOrError()
	if err != nil {
		return err
	}
	authorizer := clerk.NewAuthorizer(townRoot)

	// This atomic transition happens before executeSling can create a Polecat,
	// worktree, convoy, hook, or session.
	request, err := authorizer.Consume(args[0], clerkToken)
	if err != nil {
		return err
	}
	defer func() {
		if completeErr := authorizer.Complete(request.ID, retErr); completeErr != nil && retErr == nil {
			retErr = completeErr
		}
	}()

	workerAgent := clerkSlingAgent
	if workerAgent == "" {
		if policy, policyErr := clerk.LoadPolicy(townRoot); policyErr == nil {
			if profile, profileErr := policy.ResolveProfile(clerkSlingProfile); profileErr != nil {
				return profileErr
			} else if profile.RuntimeAlias != "" {
				workerAgent = profile.RuntimeAlias
			} else {
				workerAgent = profile.Agent
			}
		} else if clerkSlingProfile != "" {
			return policyErr
		}
	}

	formula := resolveFormula(clerkSlingFormula, false, townRoot, request.RigName)
	result, err := executeSling(SlingParams{
		BeadID:           request.BeadID,
		RigName:          request.RigName,
		FormulaName:      formula,
		Args:             clerkSlingArgs,
		Vars:             clerkSlingVars,
		Merge:            clerkSlingMerge,
		BaseBranch:       clerkSlingBaseBranch,
		Account:          clerkSlingAccount,
		Agent:            workerAgent,
		NoConvoy:         clerkSlingNoConvoy,
		NoMerge:          clerkSlingNoMerge,
		ReviewOnly:       clerkSlingReviewOnly,
		Force:            clerkSlingForce,
		FormulaFailFatal: true,
		CallerContext:    "clerk-supervised-sling",
		TownRoot:         townRoot,
		BeadsDir:         filepath.Join(townRoot, ".beads"),
		ClerkRequestID:   request.ID,
	})
	if err != nil {
		return err
	}
	wakeRigAgents(request.RigName)
	fmt.Printf("%s Clerk dispatched %s to native Polecat %s in %s\n",
		style.Bold.Render("✓"), request.BeadID, result.PolecatName, request.RigName)
	return nil
}
