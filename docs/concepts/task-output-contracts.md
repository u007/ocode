---
type: Concept
title: Task Output Contracts (expected_output)
description: How a task dispatch's optional expected_output contract is resolved, verified by a small-model check, and retried once in place; includes the CheckFailed-versus-not-satisfied distinction.
resource: CLAUDE.md
tags:
  - task
  - subagent
  - contract
  - verification
timestamp: 2026-09-30T07:37:07Z
---
# Task Output Contracts (expected_output)

A `task` dispatch may carry an optional `expected_output`: a short
natural-language description of the shape/content the caller requires of the
result. When set, the child's final result is verified against it before being
returned, and retried **once** in place if it does not match. The mechanism:

- **Contract resolution** (`resolveContract` in `internal/agent/subagent.go`):
  the call-supplied `expected_output` wins; otherwise the agent definition's
  `expected_output:` frontmatter (parsed in `internal/agent/agent_loader.go`)
  applies; neither → verification is skipped entirely (byte-identical to the
  pre-contract path, zero added cost).
- **Verification** (`internal/agent/task_contract.go`): one call against the
  contract + the child's final text, using the configured small model when
  enabled, else the session client. The prompt lives in
  `internal/agent/prompts/task_verifier.txt` with the other subagent prompts.
  The verifier checks **shape, not truth** — a result that claims completion
  satisfies a contract it never fulfilled. Do not present the badge as
  "verified correct". The result handed to the verifier is bounded
  (`truncateVerifierResult`, head+tail with a marker the prompt understands) so
  an oversized report does not needlessly slow the one call. **Do not add a
  hardcoded total-call deadline** — the verifier is bounded like every other LLM
  call (client pre-stream timeout + stream idle watchdog); `verifierContext`
  applies a deadline only when `Agent.RequestTimeout` is explicitly set, and when
  that deadline fires it is reported as a **timeout** (`TimedOut`, a subset of
  `CheckFailed`): the deficiency says `timed out after <d>`, status text says
  `Contract: timed out`, and the web tooltip says so. A malformed/unparseable
  verdict, LLM error, or timeout is a **check failure** (`CheckFailed`), never a
  silent "satisfied".
- **Retry must live inside `runSyncDispatch`, not via `resume_task_id`.**
  The retry steps the same still-live child (`executeSubAgentWithTranscript`)
  with the deficiency appended to its full transcript, then re-verifies. It
  must happen before the `defer subAgent.shutdownTransient()` fires in
  `runSyncDispatch` (and, for background runs, inside the dispatch goroutine
  after `executeSubAgentWithTranscript` and before `finishOK`/`finishErr`).
  Routing the retry through `TaskTool.Execute` would rebuild the child from
  scratch and trip the re-dispatch guard (`subagentDispatchLimit`); the public
  `resume_task_id` path requires a terminal run, which does not hold
  mid-dispatch.
- **Reporting:** satisfied → result returned unchanged (no decoration). Not
  satisfied after retry → result **prefixed** with an explicit warning naming
  the contract and the deficiency; the full child result stays present. The
  verdict is recorded on the `AgentRun` as a `ContractOutcome`
  (`Checked` / `Satisfied` / `CheckFailed` / `Deficiency`) via
  `SetContractVerdict` and surfaced in `agent_status` / `task_status`, the TUI
  agent strip + detail view, and the web Agents tab (DTO field `contract`).
- **`CheckFailed` is not "not satisfied".** When the verifier itself fails
  (timeout / LLM error / empty or unparseable response) the child's result was
  never judged; `Satisfied` is false only because no judgement exists. Every
  consumer must branch on `CheckFailed` **before** treating `!Satisfied` as a
  contract failure: the tool result is prefixed `Output contract NOT verified`
  (not `not met`), status text says `Contract NOT verified`, and the TUI/web
  badge is the muted `contract ?` (not the red `contract ✗`). Otherwise a slow
  or broken verifier reads as a failing child — this was the dominant cause of
  spurious `contract ✗` badges (verifier timeouts).
- **No built-in agent declares a default contract** — contracts are opt-in per
  call (or via a user-authored agent's frontmatter). This keeps built-ins
  (e.g. `knowledge_lookup` via the `context` agent) free of verification
  overhead.
