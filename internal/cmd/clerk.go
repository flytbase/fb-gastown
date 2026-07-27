package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/steveyegge/gastown/internal/clerk"
	"github.com/steveyegge/gastown/internal/config"
	"github.com/steveyegge/gastown/internal/constants"
	"github.com/steveyegge/gastown/internal/herdr"
	"github.com/steveyegge/gastown/internal/specialist"
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
	clerkSessionAlias    string
	clerkSessionLines    int
	clerkContextLines    int
	clerkContextJSON     bool
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
		captureClerkEvent(townRoot, clerk.MemoryTypeProject,
			"Dispatch request "+request.ID,
			fmt.Sprintf("Open supervised dispatch request %s binds task %s to rig %s.", request.ID, request.BeadID, request.RigName),
			"gt clerk request")
		return nil
	},
}

var clerkSummonCmd = &cobra.Command{
	Use:   "summon <surveyor|scrutineer> <bead-id> <rig>",
	Short: "Create a supervised native specialist dispatch request",
	Args:  cobra.ExactArgs(3),
	RunE: func(_ *cobra.Command, args []string) error {
		townRoot, err := workspace.FindFromCwdOrError()
		if err != nil {
			return err
		}
		definition, err := specialist.Resolve(args[0])
		if err != nil {
			return err
		}
		request, err := clerk.NewAuthorizer(townRoot).CreateSpecialist(definition.Kind, args[1], args[2])
		if err != nil {
			return err
		}
		fmt.Printf("%s request created: %s\n", definition.DisplayName, request.ID)
		fmt.Printf("Bound report-only dispatch: %s → %s\n", request.BeadID, request.RigName)
		fmt.Printf("Human authorization required: gt clerk authorize %s\n", request.ID)
		captureClerkEvent(townRoot, clerk.MemoryTypeProject,
			definition.DisplayName+" request "+request.ID,
			fmt.Sprintf("Open supervised %s request %s binds task %s to rig %s.", definition.Kind, request.ID, request.BeadID, request.RigName),
			"gt clerk summon")
		return nil
	},
}

var clerkSpecialistsCmd = &cobra.Command{
	Use:   "specialists",
	Short: "List native Clerk-supervised specialist kinds",
	RunE: func(_ *cobra.Command, _ []string) error {
		for _, definition := range specialist.List() {
			fmt.Printf("%-12s %s\n", definition.Kind, definition.Summary)
		}
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
		dispatchType := "Polecat"
		if request.Specialist != "" {
			if definition, resolveErr := specialist.Resolve(request.Specialist); resolveErr == nil {
				dispatchType = definition.DisplayName
			}
		}
		fmt.Printf("Authorized %s dispatch: %s → %s until %s\n", dispatchType, request.BeadID, request.RigName, request.ExpiresAt.Format(time.RFC3339))
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

var clerkSessionsCmd = &cobra.Command{
	Use:   "sessions",
	Short: "Access allow-listed external Herdr worker sessions",
	RunE:  requireSubcommand,
}

var clerkSessionsAllowCmd = &cobra.Command{
	Use:   "allow <current|session-name>",
	Short: "Allow Clerk to read and nudge an external Herdr session",
	Args:  cobra.ExactArgs(1),
	RunE: func(_ *cobra.Command, args []string) error {
		if role := strings.TrimSpace(os.Getenv("GT_ROLE")); role != "" {
			return fmt.Errorf("external session access must be configured from a neutral human shell (GT_ROLE=%s)", role)
		}
		sessionName := strings.TrimSpace(args[0])
		if strings.EqualFold(sessionName, "current") {
			sessionName = strings.TrimSpace(os.Getenv("HERDR_SESSION"))
			if sessionName == "" || os.Getenv("HERDR_ENV") != "1" {
				return fmt.Errorf("current must be run inside the Herdr session containing the manual workers")
			}
		}
		sessionName, err := config.ParseHerdrSession(sessionName)
		if err != nil {
			return err
		}
		townRoot, err := workspace.FindFromCwdOrError()
		if err != nil {
			return err
		}
		external := clerk.ExternalSession{
			Name:       sessionName,
			Alias:      strings.TrimSpace(clerkSessionAlias),
			AllowRead:  true,
			AllowNudge: true,
		}
		if err := clerk.AllowExternalSession(townRoot, external); err != nil {
			return err
		}
		fmt.Printf("%s Clerk may read and nudge Herdr session %s", style.Bold.Render("✓"), sessionName)
		if external.Alias != "" {
			fmt.Printf(" (%s)", external.Alias)
		}
		fmt.Println()
		fmt.Println("  Destructive session, workspace, tab, and pane operations remain unavailable.")
		return nil
	},
}

var clerkSessionsListCmd = &cobra.Command{
	Use:   "list",
	Short: "List Clerk's external Herdr session allow-list",
	RunE: func(_ *cobra.Command, _ []string) error {
		townRoot, err := workspace.FindFromCwdOrError()
		if err != nil {
			return err
		}
		policy, err := clerk.LoadPolicy(townRoot)
		if err != nil {
			return err
		}
		if len(policy.Sessions.External) == 0 {
			fmt.Println("No explicit external Herdr sessions are allow-listed.")
		}
		fmt.Printf("Automatic local discovery: %t\n", policy.Sessions.AutoDiscoverLocal)
		for _, external := range policy.Sessions.External {
			ref := external.Name
			if external.Alias != "" {
				ref = external.Alias + " → " + external.Name
			}
			fmt.Printf("%s  read=%t nudge=%t\n", ref, external.AllowRead, external.AllowNudge)
		}
		return nil
	},
}

var clerkSessionsAutoDiscoverCmd = &cobra.Command{
	Use:   "auto-discover <on|off>",
	Short: "Automatically synchronize workers from all local named Herdr sessions",
	Args:  cobra.ExactArgs(1),
	RunE: func(_ *cobra.Command, args []string) error {
		if role := strings.TrimSpace(os.Getenv("GT_ROLE")); role != "" {
			return fmt.Errorf("session discovery policy must be configured from a neutral human shell (GT_ROLE=%s)", role)
		}
		var enabled bool
		switch strings.ToLower(strings.TrimSpace(args[0])) {
		case "on", "true", "enabled":
			enabled = true
		case "off", "false", "disabled":
			enabled = false
		default:
			return fmt.Errorf("expected on or off")
		}
		townRoot, err := workspace.FindFromCwdOrError()
		if err != nil {
			return err
		}
		if err := clerk.SetAutoDiscoverLocal(townRoot, enabled); err != nil {
			return err
		}
		fmt.Printf("%s Clerk automatic local Herdr discovery: %t\n", style.Bold.Render("✓"), enabled)
		return nil
	},
}

var clerkSessionsWorkspacesCmd = &cobra.Command{
	Use:   "workspaces <session-or-alias>",
	Short: "List workspaces in an allow-listed external Herdr session",
	Args:  cobra.ExactArgs(1),
	RunE: func(_ *cobra.Command, args []string) error {
		external, err := resolveClerkExternalSession(args[0], false)
		if err != nil {
			return err
		}
		out, err := herdr.ListExternalWorkspaces(external.Name)
		if err != nil {
			return err
		}
		fmt.Print(string(out))
		return nil
	},
}

var clerkSessionsPanesCmd = &cobra.Command{
	Use:   "panes <session-or-alias> <workspace-id>",
	Short: "List panes in one external Herdr workspace",
	Args:  cobra.ExactArgs(2),
	RunE: func(_ *cobra.Command, args []string) error {
		external, err := resolveClerkExternalSession(args[0], false)
		if err != nil {
			return err
		}
		out, err := herdr.ListExternalPanes(external.Name, args[1])
		if err != nil {
			return err
		}
		fmt.Print(string(out))
		return nil
	},
}

var clerkSessionsReadCmd = &cobra.Command{
	Use:   "read <session-or-alias> <pane-id>",
	Short: "Read a pane in an allow-listed external Herdr session",
	Args:  cobra.ExactArgs(2),
	RunE: func(_ *cobra.Command, args []string) error {
		external, err := resolveClerkExternalSession(args[0], false)
		if err != nil {
			return err
		}
		out, err := herdr.ReadExternalPane(external.Name, args[1], clerkSessionLines)
		if err != nil {
			return err
		}
		fmt.Print(string(out))
		return nil
	},
}

var clerkSessionsNudgeCmd = &cobra.Command{
	Use:   "nudge <session-or-alias> <pane-id> <message>",
	Short: "Send a supervised prompt to an external worker pane",
	Args:  cobra.ExactArgs(3),
	RunE: func(_ *cobra.Command, args []string) error {
		external, err := resolveClerkExternalSession(args[0], true)
		if err != nil {
			return err
		}
		if _, err := herdr.NudgeExternalPane(external.Name, args[1], args[2]); err != nil {
			return err
		}
		fmt.Printf("%s Nudged %s/%s\n", style.Bold.Render("✓"), external.Name, args[1])
		return nil
	},
}

var clerkContextCmd = &cobra.Command{
	Use:   "context",
	Short: "Refresh and print Clerk's automatically injected worker context",
	RunE: func(_ *cobra.Command, _ []string) error {
		townRoot, err := workspace.FindFromCwdOrError()
		if err != nil {
			return err
		}
		snapshot, err := clerk.RefreshExternalContext(townRoot, clerkContextLines)
		if err != nil {
			// Prompt injection must fail open when an external session is
			// temporarily unavailable; the snapshot records per-session errors.
			if strings.TrimSpace(os.Getenv("GT_ROLE")) == string(constants.RoleClerk) {
				fmt.Printf("\n## External worker synchronization warning\n\n%s\n", err)
				return nil
			}
			return err
		}
		if clerkContextJSON {
			encoder := json.NewEncoder(os.Stdout)
			encoder.SetIndent("", "  ")
			return encoder.Encode(snapshot)
		}
		fmt.Print(clerk.FormatExternalContext(snapshot))
		return nil
	},
}

func resolveClerkExternalSession(ref string, nudge bool) (clerk.ExternalSession, error) {
	townRoot, err := workspace.FindFromCwdOrError()
	if err != nil {
		return clerk.ExternalSession{}, err
	}
	policy, err := clerk.LoadPolicy(townRoot)
	if err != nil {
		return clerk.ExternalSession{}, err
	}
	return policy.ResolveExternalSession(ref, nudge)
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
	clerkSessionsAllowCmd.Flags().StringVar(&clerkSessionAlias, "alias", "", "Short stable name for the external session")
	clerkSessionsReadCmd.Flags().IntVar(&clerkSessionLines, "lines", 120, "Recent unwrapped lines to read")
	clerkContextCmd.Flags().IntVar(&clerkContextLines, "lines", 20, "Recent lines captured per recognized external agent")
	clerkContextCmd.Flags().BoolVar(&clerkContextJSON, "json", false, "Print the persisted snapshot as JSON")

	clerkCmd.AddCommand(clerkStartCmd, clerkStopCmd, clerkStatusCmd, clerkAttachCmd)
	clerkCmd.AddCommand(clerkRequestCmd, clerkSummonCmd, clerkSpecialistsCmd, clerkAuthorizeCmd, clerkSlingCmd)
	clerkSessionsCmd.AddCommand(clerkSessionsAllowCmd, clerkSessionsListCmd, clerkSessionsAutoDiscoverCmd)
	clerkSessionsCmd.AddCommand(clerkSessionsWorkspacesCmd, clerkSessionsPanesCmd, clerkSessionsReadCmd, clerkSessionsNudgeCmd)
	clerkCmd.AddCommand(clerkSessionsCmd)
	clerkCmd.AddCommand(clerkContextCmd)
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
	dispatchedPolecat := ""

	// This atomic transition happens before executeSling can create a Polecat,
	// worktree, convoy, hook, or session.
	request, err := authorizer.Consume(args[0], clerkToken)
	if err != nil {
		return err
	}
	defer func() {
		if completeErr := authorizer.Complete(request.ID, dispatchedPolecat, retErr); completeErr != nil && retErr == nil {
			retErr = completeErr
		}
	}()

	var specialistDefinition *specialist.Definition
	if request.Specialist != "" {
		definition, resolveErr := specialist.Resolve(request.Specialist)
		if resolveErr != nil {
			return resolveErr
		}
		specialistDefinition = &definition
		if clerkSlingFormula != "" {
			return fmt.Errorf("native specialists are report-only and do not accept --formula")
		}
	}

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
	params := SlingParams{
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
	}
	if specialistDefinition != nil {
		applySpecialistDispatch(&params, *specialistDefinition)
	}
	result, err := executeSling(params)
	if err != nil {
		return err
	}
	dispatchedPolecat = result.PolecatName
	wakeRigAgents(request.RigName)
	workerLabel := "Polecat"
	if specialistDefinition != nil {
		workerLabel = specialistDefinition.DisplayName
	}
	fmt.Printf("%s Clerk dispatched %s to native %s %s in %s\n",
		style.Bold.Render("✓"), request.BeadID, workerLabel, result.PolecatName, request.RigName)
	captureClerkEvent(townRoot, clerk.MemoryTypeExperience,
		"Dispatched "+request.BeadID,
		fmt.Sprintf("Supervised request %s dispatched task %s to Polecat %s in rig %s.", request.ID, request.BeadID, result.PolecatName, request.RigName),
		"gt clerk sling")
	return nil
}

func applySpecialistDispatch(params *SlingParams, definition specialist.Definition) {
	params.Specialist = definition.Kind
	if definition.ReportOnly {
		params.NoMerge = true
		params.ReviewOnly = true
		params.HookRawBead = true
		params.FormulaName = ""
	}
}
