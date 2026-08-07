# 01 — Task output contracts (`expected_output` + verify-and-retry-once)

## How it currently works

`TaskTool.Execute` (`internal/agent/subagent.go`) resolves an agent definition,
builds a fresh sub-`Agent`, and dispatches it with the caller's `prompt` and
optional `context`. In synchronous mode it calls `runSyncDispatch`, which
acquires a concurrency slot, calls `executeSubAgentWithTranscript`, records the
child session, and returns the child's final assistant text as the tool result.
In background mode `runBackgroundDispatch` returns a run id immediately and the
result lands on the `AgentRun` for later polling.

Nothing between the child's last token and the parent's next turn inspects
*what* came back. The parent gets prose and must decide, from prose, whether the
job was done.

## Where it breaks

- A child that ran out of steps returns a partial summary that reads like a
  completed report. The parent proceeds on it.
- A child asked to produce a patch returns a description of a patch.
- A child asked for "the list of affected files" returns three files and a
  hedge, when the caller needed all of them.
- Background runs are worse: the parent polls, sees `done`, and treats `done`
  as `correct`.

CrewAI's `Task.expected_output` exists for exactly this: the contract is stated
up front, and (with `guardrail`) validated before the output is allowed to flow
into the next task.

## Proposed change

Add an optional output contract to a dispatch, and verify it once — cheaply —
before the result is handed back.

### Surface

- New optional property `expected_output` on the `task` tool schema: a short
  natural-language description of the shape/content the caller requires.
- New optional field `ExpectedOutput` on `AgentDefinition`, parsed from the
  agent's markdown frontmatter. This is the agent's *default* contract, used
  when the call does not supply one. A call-supplied contract wins.
- No contract from either source → verification is skipped entirely. This is
  the unchanged, zero-cost path.

### Verification

When a contract applies, after the child produces its final result and **before
the dispatch tears down**, run a single verification call:

- Use the small model when configured (`internal/agent/small_model.go`), else
  the session client. This is one short call against the contract text plus the
  child's final result — not the child's whole transcript.
- The verifier returns a verdict: satisfied yes/no, plus, on no, a one-or-two
  sentence statement of what is missing.
- The verdict prompt lives with the other subagent prompts, not inlined at the
  call site.

### Retry

On a `no` verdict, retry **once**, in-place:

- Append the deficiency as a new user-role message to the same child transcript
  and step the child again. The child sees its own prior work plus the specific
  gap, which is far more effective than re-running from a cold start.
- **Critical ordering constraint:** this must happen *inside* `runSyncDispatch`,
  before its `defer subAgent.shutdownTransient()` fires. The public
  `resume_task_id` path is not usable here — `resumeEligibleRun` requires the
  run to already be in `RunCancelled`/`RunDone` and to pass a dispatcher-
  ownership check, neither of which holds mid-dispatch. Reuse
  `executeSubAgentWithTranscript` directly on the still-live child.
- Re-verify after the retry. Two failures is the end of it.
- The retry must **not** be routed back through `TaskTool.Execute`. Besides
  rebuilding the child from scratch, `Execute` calls `NoteSubagentDispatch`,
  whose re-dispatch guard (`subagentDispatchLimit`) would count the retry as a
  repeat launch of the same agent without intervening user input.

### Reporting

- Contract satisfied (first try or after retry): return the result unchanged.
  Do not decorate success — noise in the tool result costs context.
- Contract not satisfied after retry: return the child's result **prefixed with
  an explicit warning** naming the contract and the missing piece. Do not return
  an error — the partial work is often still useful, and swallowing it is worse
  than surfacing it. The parent model must be able to see that the contract was
  not met.
- Record the verdict (satisfied / not-satisfied / not-applicable) and the
  deficiency text on the `AgentRun` so the TUI agent strip and the web Agents
  tab can badge a run that finished but did not meet its contract.

### Background runs

The same verify-and-retry sequence runs in `runBackgroundDispatch`, **inside the
dispatch goroutine**, after `executeSubAgent` returns and before
`finishOK`/`finishErr`. That window is correct: the goroutine's teardown
(`shutdownTransient` + `markTeardownDone`) is a `defer` that only fires when the
goroutine exits, so the child is still live there, and the terminal status has
not yet been published. A polled `done` then genuinely means "done and checked",
and a resume waiting on `awaitTeardown` is unaffected. `agent_status` /
`task_status` output includes the verdict.

## Constraints and non-goals

- **Cache:** `expected_output` is a static schema property (one-time tools
  change). The contract text travels in call arguments only. It must never be
  interpolated into the `task` tool description.
- **Cost:** one extra small-model call per contracted dispatch, plus at most one
  extra child run. Uncontracted dispatches cost nothing extra.
- **Ship with no built-in agent declaring a default contract.** The
  `ExpectedOutput` field on `AgentDefinition` exists for user-authored agents;
  populating it on `explore` / `general` / `context` would silently attach a
  verify call to every `knowledge_lookup` and every exploratory dispatch in the
  product. Contracts start opt-in, per call.
- **This checks shape, not truth.** The verifier sees the contract and the
  child's final text — nothing else. A child that reports "wrote the changes to
  X" satisfies a contract it never actually fulfilled. This catches truncated,
  off-target, and wrong-format results; it does not catch lying or
  hallucinated completion. Do not let the badge be read as "verified correct",
  and do not describe it that way in the TUI or the Agents tab.
- **Not a guardrail framework.** No pluggable validator functions, no schema
  languages, no structured-output coercion. One natural-language contract, one
  LLM check, one retry.
- **Not recursive.** A retried child's own nested dispatches are unaffected;
  contracts do not cascade.

## Files touched

| File | Change |
|------|--------|
| `internal/agent/subagent.go` | `expected_output` in schema + `taskToolParams`; contract resolution (call → def); verify/retry inside `runSyncDispatch` and `runBackgroundDispatch` |
| `internal/agent/agent_registry.go` | `ExpectedOutput` on `AgentDefinition` |
| `internal/agent/agent_loader.go` | parse `expected_output` from agent markdown frontmatter |
| `internal/agent/agent_runs.go` | verdict + deficiency fields on `AgentRun`; surface in status output |
| `internal/agent/prompts/` | verifier prompt |
| `internal/tui/` (agent strip / detail view) | badge a contract-failed run |
| `web/src/components/…/Agents` | same badge in the Agents tab |
| `AGENTS.md` | document the contract mechanism and the retry-inside-dispatch constraint |

## Tasks

1. Add `ExpectedOutput` to `AgentDefinition` and parse it in the markdown loader
   → verify: loader test asserts an agent md with `expected_output:` frontmatter
   round-trips into the definition, and one without it yields empty.
2. Add `expected_output` to the `task` tool schema and `taskToolParams`; resolve
   call-value-else-definition-value → verify: unit test over the resolution
   precedence, including both-empty (skip) case.
3. Write the verifier: contract + result in, verdict + deficiency out, small
   model when configured → verify: table test with a fake client covering
   satisfied, not-satisfied-with-reason, and malformed-verdict (which must be
   treated as not-satisfied and logged, never as satisfied).
4. Wire verify + single in-place retry into `runSyncDispatch` ahead of the
   teardown defer → verify: test asserts the child is stepped a second time with
   the deficiency appended, and that `shutdownTransient` runs exactly once,
   after the retry.
5. Wire the same into the `runBackgroundDispatch` goroutine, after
   `executeSubAgent` and before `finishOK`/`finishErr` → verify: test asserts a
   background run is not marked `done` until the verdict exists, and that
   teardown still happens exactly once on goroutine exit.
6. Warning-prefix the result on a failed contract; store verdict on `AgentRun`
   → verify: test asserts the prefix text names the contract and the deficiency,
   and that the underlying child result is still present in full.
7. Surface the verdict badge in the TUI agent detail and the web Agents tab
   → verify: existing component tests extended for the failed-contract state.
8. Update `AGENTS.md` with the mechanism and the "retry must live inside
   runSyncDispatch, not via resume_task_id" constraint → verify: re-read the
   section against the shipped code paths.
