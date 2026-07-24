# Native supervised Clerk

Clerk is a town-level, non-autonomous task coordinator. It shares Gas Town's
native session lifecycle but cannot dispatch without a one-use human capability.

## Start

Run from a Herdr-managed terminal:

```bash
gt clerk start
gt clerk attach
```

The Clerk lives in the `hq-clerk` logical session. The Herdr adapter stores the
opaque tab/pane mapping under `<town>/.runtime/herdr-sessions.json`.

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

## Herdr backend

`GT_SESSION_BACKEND=herdr` makes the existing `tmux.Tmux` type act as a
compatibility facade over Herdr. This keeps the Polecat, Witness, and Refinery
callers unchanged while replacing the terminal transport. Herdr IDs are treated
as opaque and persisted; Gas Town remains the owner of worktrees and worker
lifecycle.
