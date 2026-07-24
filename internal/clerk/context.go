package clerk

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/steveyegge/gastown/internal/atomicfile"
	"github.com/steveyegge/gastown/internal/config"
	"github.com/steveyegge/gastown/internal/herdr"
)

const externalContextFile = "external-workers.json"

// ExternalContext is the durable, automatically refreshed view of manually
// managed workers that live outside the Town's Herdr session.
type ExternalContext struct {
	GeneratedAt    time.Time                `json:"generated_at"`
	DiscoveryError string                   `json:"discovery_error,omitempty"`
	Sessions       []ExternalSessionContext `json:"sessions"`
}

type ExternalSessionContext struct {
	Name       string                     `json:"name"`
	Alias      string                     `json:"alias,omitempty"`
	Error      string                     `json:"error,omitempty"`
	Workspaces []ExternalWorkspaceContext `json:"workspaces,omitempty"`
}

type ExternalWorkspaceContext struct {
	WorkspaceID string                `json:"workspace_id"`
	Label       string                `json:"label,omitempty"`
	Cwd         string                `json:"cwd,omitempty"`
	Panes       []ExternalPaneContext `json:"panes,omitempty"`
}

type ExternalPaneContext struct {
	PaneID        string `json:"pane_id"`
	TabID         string `json:"tab_id,omitempty"`
	Label         string `json:"label,omitempty"`
	Cwd           string `json:"cwd,omitempty"`
	ForegroundCwd string `json:"foreground_cwd,omitempty"`
	Agent         string `json:"agent,omitempty"`
	AgentStatus   string `json:"agent_status,omitempty"`
	RecentOutput  string `json:"recent_output,omitempty"`
	ReadError     string `json:"read_error,omitempty"`
}

// RefreshExternalContext collects the allow-listed sessions without changing
// their topology or focus and persists the result for Clerk session recovery.
func RefreshExternalContext(townRoot string, recentLines int) (*ExternalContext, error) {
	if recentLines <= 0 {
		recentLines = 20
	}
	policy, err := LoadPolicy(townRoot)
	if err != nil {
		return nil, err
	}
	snapshot := &ExternalContext{
		GeneratedAt: time.Now().UTC(),
		Sessions:    make([]ExternalSessionContext, 0, len(policy.Sessions.External)),
	}
	allowedSessions := append([]ExternalSession(nil), policy.Sessions.External...)
	known := make(map[string]bool, len(allowedSessions))
	for _, allowed := range allowedSessions {
		known[allowed.Name] = true
	}
	if policy.Sessions.AutoDiscoverLocal {
		currentSession := strings.TrimSpace(os.Getenv("HERDR_SESSION"))
		if currentSession == "" {
			if configured, resolveErr := config.ResolveHerdrSession(townRoot); resolveErr == nil && configured != "" {
				currentSession = configured
			} else {
				currentSession = herdr.TownSessionName(townRoot)
			}
		}
		discovered, discoverErr := herdr.GetNamedSessions()
		if discoverErr != nil {
			snapshot.DiscoveryError = discoverErr.Error()
		} else {
			for _, candidate := range discovered {
				if !candidate.Running || candidate.Name == "" || candidate.Name == currentSession || known[candidate.Name] {
					continue
				}
				allowedSessions = append(allowedSessions, ExternalSession{
					Name:       candidate.Name,
					AllowRead:  true,
					AllowNudge: true,
				})
				known[candidate.Name] = true
			}
		}
	}
	for _, allowed := range allowedSessions {
		session := ExternalSessionContext{Name: allowed.Name, Alias: allowed.Alias}
		workspaces, listErr := herdr.GetExternalWorkspaces(allowed.Name)
		if listErr != nil {
			session.Error = listErr.Error()
			snapshot.Sessions = append(snapshot.Sessions, session)
			continue
		}
		for _, workspace := range workspaces {
			item := ExternalWorkspaceContext{
				WorkspaceID: workspace.WorkspaceID,
				Label:       workspace.Label,
				Cwd:         workspace.Cwd,
			}
			panes, panesErr := herdr.GetExternalPanes(allowed.Name, workspace.WorkspaceID)
			if panesErr != nil {
				session.Error = panesErr.Error()
				session.Workspaces = append(session.Workspaces, item)
				continue
			}
			for _, pane := range panes {
				paneContext := ExternalPaneContext{
					PaneID:        pane.PaneID,
					TabID:         pane.TabID,
					Label:         pane.Label,
					Cwd:           pane.Cwd,
					ForegroundCwd: pane.ForegroundCwd,
					Agent:         pane.Agent,
					AgentStatus:   pane.AgentStatus,
				}
				// Shell/server panes are catalogued, but only recognized agents
				// contribute transcript context to keep injection bounded.
				if pane.Agent != "" && allowed.AllowRead {
					out, readErr := herdr.ReadExternalPane(allowed.Name, pane.PaneID, recentLines)
					if readErr != nil {
						paneContext.ReadError = readErr.Error()
					} else {
						paneContext.RecentOutput = strings.TrimSpace(string(out))
					}
				}
				item.Panes = append(item.Panes, paneContext)
			}
			session.Workspaces = append(session.Workspaces, item)
		}
		snapshot.Sessions = append(snapshot.Sessions, session)
	}
	path := filepath.Join(townRoot, "clerk", "state", externalContextFile)
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, fmt.Errorf("creating Clerk context state directory: %w", err)
	}
	if err := atomicfile.WriteJSONWithPerm(path, snapshot, 0600); err != nil {
		return nil, fmt.Errorf("persisting Clerk external worker context: %w", err)
	}
	return snapshot, nil
}

// FormatExternalContext produces context suitable for native prime and prompt
// hook injection. It is state, not a request for the Clerk to do discovery.
func FormatExternalContext(snapshot *ExternalContext) string {
	var out strings.Builder
	out.WriteString("\n## Automatically synchronized external workers\n\n")
	if snapshot == nil || len(snapshot.Sessions) == 0 {
		if snapshot != nil && snapshot.DiscoveryError != "" {
			fmt.Fprintf(&out, "Automatic local Herdr discovery failed: %s\n", snapshot.DiscoveryError)
		} else {
			out.WriteString("No external Herdr workers are currently visible.\n")
		}
		return out.String()
	}
	fmt.Fprintf(&out, "Snapshot: %s\n", snapshot.GeneratedAt.Format(time.RFC3339))
	out.WriteString("This catalog was refreshed by Gas Town. Do not ask the human to rediscover or restate it.\n\n")
	if snapshot.DiscoveryError != "" {
		fmt.Fprintf(&out, "Discovery warning: %s\n\n", snapshot.DiscoveryError)
	}
	for _, session := range snapshot.Sessions {
		ref := session.Name
		if session.Alias != "" {
			ref = session.Alias + " (" + session.Name + ")"
		}
		fmt.Fprintf(&out, "### Session %s\n", ref)
		if session.Error != "" {
			fmt.Fprintf(&out, "- unavailable: %s\n", session.Error)
			continue
		}
		for _, workspace := range session.Workspaces {
			fmt.Fprintf(&out, "- workspace `%s`", workspace.WorkspaceID)
			if workspace.Label != "" {
				fmt.Fprintf(&out, " %q", workspace.Label)
			}
			if workspace.Cwd != "" {
				fmt.Fprintf(&out, " cwd=`%s`", workspace.Cwd)
			}
			out.WriteString("\n")
			for _, pane := range workspace.Panes {
				fmt.Fprintf(&out, "  - pane `%s`", pane.PaneID)
				if pane.Agent != "" {
					fmt.Fprintf(&out, " agent=%s status=%s", pane.Agent, pane.AgentStatus)
				}
				if pane.ForegroundCwd != "" {
					fmt.Fprintf(&out, " cwd=`%s`", pane.ForegroundCwd)
				} else if pane.Cwd != "" {
					fmt.Fprintf(&out, " cwd=`%s`", pane.Cwd)
				}
				out.WriteString("\n")
				if pane.RecentOutput != "" {
					out.WriteString("    Recent terminal context:\n\n")
					out.WriteString("    ```text\n")
					for _, line := range strings.Split(pane.RecentOutput, "\n") {
						out.WriteString("    " + line + "\n")
					}
					out.WriteString("    ```\n")
				} else if pane.ReadError != "" {
					fmt.Fprintf(&out, "    transcript unavailable: %s\n", pane.ReadError)
				}
			}
		}
	}
	return out.String()
}
