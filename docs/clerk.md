# Native supervised Clerk

Clerk is a town-level, non-autonomous task coordinator. It shares Gas Town's
native session lifecycle but cannot dispatch without a one-use human capability.

## Installation

Clerk is compiled into the normal Gas Town binary and provisioned as a native
Town role. There is no role-specific installer.

```bash
# Build and install the gt binary from source.
make install

# New Town: provisions Mayor, Clerk, Deacon, and the other native defaults.
gt install ~/gt

# Existing Town: provisions or repairs Clerk without replacing clerk.json.
cd ~/gt
gt upgrade
```

The repository owns the Clerk implementation, role definition, prompt template,
default policy, validation, and upgrade behavior. `<town>/clerk/clerk.json` is
runtime data: it contains the local Town's routing and optional integration
choices and is preserved across upgrades.

## Start

Clerk uses the same Town-wide session backend as Mayor, Deacon, Witnesses,
Refineries, Crew, and Polecats:

```bash
gt up
# or start only Clerk
gt clerk start
gt clerk attach
```

The Clerk lives in the `hq-clerk` logical session. It is included in
`gt up`, `gt down`, and `gt status`.

## Dispatch

```bash
# Clerk prepares the request
gt clerk request gt-123 my-rig

# Human runs this outside every agent session
gt clerk authorize REQUEST_ID

# Clerk consumes the returned capability
gt clerk sling REQUEST_ID --token CAPABILITY --profile debugging
```

Authorization is bound to the request's bead and rig, expires after ten minutes
by default, and is consumed atomically before native sling can create a Polecat,
worktree, convoy, or session.

The dispatch then follows the normal native path:

1. Gas Town creates the Polecat rig worktree.
2. Native runtime hooks inject `gt prime` task context.
3. The automatic convoy tracks the work.
4. Witness monitors the Polecat.
5. Refinery processes merge work.
6. Existing native completion and cleanup apply.

Raw `gt sling` is rejected when `GT_ROLE=clerk`.

Routing profiles are configured in `<town>/clerk/clerk.json`. A fresh Town uses
portable `claude` and `codex` aliases; installations may bind profiles to exact
model aliases without changing or rebuilding Gas Town.

## Session backend

Select tmux or Herdr when creating the Town:

```bash
gt install ~/gt --session-backend herdr
```

The selection belongs to Town settings, not `clerk.json`; Clerk cannot choose a
different transport from the workers it supervises. See
[Session backends](session-backends.md) for lifecycle and migration details.

## Manual workers in another Herdr session

The Town may remain in its managed Herdr session while Clerk observes manual
workers in explicitly allow-listed sessions. From a neutral shell inside the
session containing those workers:

```bash
cd ~/gt
gt clerk sessions allow current --alias manual
```

For a task-head that must automatically know every local Herdr worker without
per-session registration:

```bash
gt clerk sessions auto-discover on
```

The Town's own Herdr session is excluded from this external-worker catalog.

Clerk can then list workspaces and panes, read output, and nudge a pane:

```bash
gt clerk sessions workspaces manual
gt clerk sessions panes manual w1
gt clerk sessions read manual w1:p7
gt clerk sessions nudge manual w1:p7 "Read the task context recorded by Clerk."
```

External pane IDs are scoped to their named session. The Clerk command surface
does not expose stop/delete/close operations for external Herdr resources.

The human does not prompt Clerk to run these discovery commands. Native
SessionStart priming and the Clerk UserPromptSubmit hook automatically refresh
the catalog, recent recognized-agent output, and status before Clerk receives
the conversation. The durable snapshot is stored at:

```text
<town>/clerk/state/external-workers.json
```
