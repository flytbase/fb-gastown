# Assayer

Assayer is Gas Town's supervised, Town-scoped backend assurance Lead. It is a
long-lived native role with the fixed session identity `hq-assayer`; it is not a
Polecat specialization and does not enter a rig's merge queue.

## Phase 1 boundary

Phase 1 proves bounded, session-native multi-agent orchestration with synthetic,
response-only inputs. It does not authorize real backend commands, repository
writes, Git operations, services, databases, containers, CI, infrastructure, or
production data.

The Assayer is the only spawner. Its managed worker catalog contains:

- Context Scout
- Oracle Analyst
- Risk Challenger
- Test Author
- Test Critic

Workers cannot delegate, message peers, inspect blinded attempts, approve an
Oracle, confirm a bug, waive a gate, or decide pass/fail. Claude and Codex worker
definitions are generated beneath `assayer/.claude/agents/` and
`assayer/.codex/agents/` from repository-owned definitions.

## Lifecycle

```bash
gt assayer start
gt assayer status
gt assayer attach
gt assayer stop
gt assayer workers
```

`gt up` starts Assayer with the other Town heads. The configured Town session
backend (tmux or Herdr) owns its terminal lifecycle.

## Context

Context roots are existing read-only backend assurance specifications. Add them
from a neutral human shell:

```bash
gt assayer context add /absolute/path/to/backend-assurance-specifications
```

Gas Town stores only the per-Town path in `assayer/assayer.json`; product truth,
credentials, and user-specific paths are not embedded in the binary.

## Run authorization

Humans can activate a run with a short-lived, one-use capability bound to one
task, rig, and mode:

```bash
gt assayer request <bead-id> <rig> --mode harden
gt assayer authorize <request-id>
gt assayer run <request-id> --token <capability>
```

The authorization command rejects agent-session identities and must be run by
the human from a neutral shell.

Mayor is the sole trusted agent principal and may authorize and consume a new
request directly without a token:

```bash
gt assayer request <bead-id> <rig> --mode harden
gt assayer run <request-id>
```

The request records `authorized_by: mayor`. Clerk, Assayer, Polecats, Crew, and
other roles cannot use this path. Phase 1 activation only notifies the Assayer
of the consumed request. A project assurance kernel and locked authority are
still required before any governed worker flow can claim deterministic evidence.

## Trust model

Agents interpret and propose. The project-local deterministic assurance control
kernel owns task packs, state, budgets, evidence integrity, stage transitions,
and decisions. If the kernel, locked authority, native workers, or containment
is unavailable, Assayer must return a blocked disposition instead of replacing
the missing control with model judgment.
