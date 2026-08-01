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

## Automatic post-worker advisory

By default, a successful `gt done` submission with a merge request activates one
deterministic Assayer request before Gas Town nudges Refinery. The activation is
authorized by the native Mayor lifecycle policy, so neither the human nor the
Polecat needs to copy a request ID or capability token.

```text
Polecat gt done
  -> verified merge-request bead
  -> idempotent Mayor-authorized Assayer advisory
  -> Refinery nudge and normal merge processing
```

The request identity is stable for the bead, rig, merge request, and assurance
mode. Retries and Witness recovery therefore do not create duplicate Assayer
runs. Delivery failures are sealed in the request audit record. Because Phase 1
cannot execute a real backend, this advisory never blocks or approves a merge.

Per-Town behavior is controlled by `assayer/assayer.json`:

```json
{
  "automation": {
    "post_worker": {
      "enabled": true,
      "mode": "harden",
      "require_merge_request": true
    }
  }
}
```

## Trust model

Agents interpret and propose. The project-local deterministic assurance control
kernel owns task packs, state, budgets, evidence integrity, stage transitions,
and decisions. If the kernel, locked authority, native workers, or containment
is unavailable, Assayer must return a blocked disposition instead of replacing
the missing control with model judgment.
