# Session backends

Gas Town owns agent identity, worktrees, hooks, convoys, supervision, merging,
and cleanup. A session backend only supplies the terminal panes in which those
agents run.

Each Town selects one backend in `<town>/settings/config.json`:

```json
{
  "type": "town-settings",
  "version": 1,
  "session_backend": "herdr"
}
```

Supported values are:

- `tmux` — the default and backwards-compatible backend.
- `herdr` — a Town-owned named Herdr server and workspace, or an explicitly
  bound existing named session.

## Select during installation

```bash
gt install ~/gt --session-backend tmux
gt install ~/gt --session-backend herdr
# Run inside an existing Herdr session to place Town roles there:
gt install ~/gt --session-backend herdr --herdr-session current
```

Gas Town validates the selected executable before changing the target
directory. Omitting the flag selects tmux for a new Town and preserves the
current setting when re-running install with `--force`.

## Change an existing Town

Stop the existing sessions before changing transports:

```bash
cd ~/gt
gt down
gt config set session_backend herdr
gt up
```

An existing Herdr Town can either use its managed session or join the session
where the command is being run:

```bash
gt config set herdr_session managed
gt config set herdr_session current
gt config get herdr_session
```

Inspect the durable selection with:

```bash
gt config get session_backend
gt status
gt doctor
```

`GT_SESSION_BACKEND` remains an expert process-tree override. Normal users
should use the persisted Town setting so the CLI, daemon, Mayor, Clerk,
Witnesses, Refineries, Crew, and Polecats all resolve the same backend.

## Town-wide command contract

The persisted setting applies to the whole Town, not to one role or one shell:

- A new Town defaults to `tmux`.
- A Town configured with `herdr` creates, lists, inspects, nudges, focuses,
  restarts, and stops its logical sessions through Herdr.
- Commands must not silently fall back to tmux when Herdr is selected.
- Gas Town session names and user-facing commands remain the same.

For example, these commands query the selected backend:

```bash
gt agents list -a
gt status
gt cycle next
gt nudge clerk "Review the backlog"
gt clerk attach
gt mayor attach
gt down
```

`gt agents list -a` includes the Clerk and Polecats. `gt agents menu` uses the
tmux popup on tmux Towns and a numbered Herdr tab selector on Herdr Towns.
`gt feed --window` uses a tmux window on tmux Towns and the managed
`hq-feed` Herdr tab on Herdr Towns.

## Herdr lifecycle

No tmux session or manually opened Herdr UI is required before `gt up`.
By default Gas Town derives a path-scoped Herdr session name, starts its headless server
on first use, creates one Town workspace, and records opaque workspace, tab,
and pane IDs in:

```text
<town>/.runtime/herdr-sessions.json
```

Logical Gas Town session names such as `hq-mayor`, `hq-clerk`, and
`<rig-prefix>-witness` remain unchanged. This lets the existing native
lifecycle keep working without teaching worktree, convoy, Witness, or Refinery
code about Herdr IDs.

Use the ordinary role commands:

```bash
gt up
gt mayor attach
gt clerk attach
gt down
```

Attach commands focus the matching Herdr tab and attach the terminal to the
Town's named Herdr session when called from a normal shell.

`gt down` stops Gas Town sessions but leaves the backend server available for a
later `gt up`. `gt down --nuke` also stops the Town-owned Herdr server. As with
the tmux form of `--nuke`, do not use it if you placed unrelated work in the
Town-managed backend.

When `herdr_session` binds the Town to an existing shared session, Gas Town may
start that named server if it is absent but never stops the whole server.
Manual workers in other sessions can instead be exposed to Clerk through its
explicit external-session allow-list; those resources are read/nudge only.

Process-level orphan scanning is deliberately skipped for Herdr until Herdr
exposes reliable pane process-tree identifiers. Normal role stop, handoff,
restart, `gt down`, and `gt down --nuke` cleanup still use Herdr directly.
This safety rule prevents a tmux-only orphan scan from killing a healthy Herdr
agent.

## Architecture

The `internal/tmux.Tmux` type is retained as the session compatibility facade
for now. It delegates backend-neutral lifecycle operations to
`internal/herdr.Client` when the Town selects Herdr. Gas Town remains the
authority for:

- runtime startup and native `gt prime` hooks;
- Polecat and Crew worktrees;
- supervised Clerk authorization;
- Witness and Refinery monitoring;
- merge and cleanup behavior.

Herdr remains the authority only for its server, workspace, tab, pane, focus,
input, and terminal-output operations.
