# Part 01 — The `Decider` seam

Introduce the interface that lets any decision backend serve any judge. **No behaviour change**: every slot still resolves to Jev.

## Constraints (apply to every part; duplicated here because parts do not cross-reference)

- No new module dependencies.
- Decision-only, permanently: never reachable from chat, compaction, small-model, recap, or task-contract paths.
- Fail-open is a hard invariant; a judge may only hide a result or defer to a human.
- No silent backend fallback.
- Missing credential must be visible, not a silent nil.
- `permissions.auto.model` and `auto_continue_model` are not refactored or aliased.
- Confidence floors stay shared: 0.85 permission, 0.5 relevance. No per-backend thresholds.
- `TypesafeQuestion` and `TypesafeResponse` shapes are unchanged; `Criteria` stays `map[string]string`.
- Never call a resolver under a cache lock, and never hold a lock across a network call.
- `gofmt` clean, `go vet` clean. Tests named `Test<Subject>_<Behaviour>`.
- Do **not** touch `discoveryAllows` (`internal/agent/discovery_glue.go`) — the MCP tool gate is the one place that must never fail open. Only the relevance veto path may change here.
- Four files this plan modifies are `MM` in git (a peer has staged work): `internal/agent/agent.go`, `internal/agent/client.go`, `internal/agent/permission_typesafe.go`, `internal/config/ocodeconfig.go`. `content_guard_typesafe.go` is `AM`. Never blanket-commit those paths; stage only your own hunks. Never `git stash`, `reset`, `checkout --`, or `clean`.

## Files

- Create `internal/agent/decider.go`
- Create `internal/agent/decider_test.go`
- Modify `internal/agent/permission_typesafe.go` — `askPermissionModelTypesafe`
- Modify `internal/agent/agent.go` — `consultPermissionModel`
- Modify `internal/agent/relevance_typesafe.go` — `judgeRelevanceQuestions`
- Modify `internal/agent/search_typesafe.go` — `judgeSearchResults`
- Modify `internal/agent/doc_search_typesafe.go` — `judgeDocSearchResults`
- Modify `internal/agent/autocontinue_typesafe.go` — `runAutoContinueJudgeTypesafe`
- Modify `internal/agent/network_guard_typesafe.go` — `networkGuardJudgeClient`
- Modify `internal/agent/content_guard_typesafe.go` — `contentGuardClient`
- Modify `internal/agent/discovery_typesafe.go` — `judgeDiscoveryCandidates`
- Modify `internal/agent/discovery_glue.go` — the `discoveryState` judge cache
- Modify `internal/agent/typesafe.go` — comments only

## Interface introduced

`Decider` — the four methods `TypesafeClient` already declares at `internal/agent/typesafe.go` (`Decide`, `DecideCtx`, `GetProvider`, `GetModel`). Narrower than `LLMClient` on purpose, so a decision backend cannot reach a chat path by accident.

Also introduces: the `judgeSlot` type with seven constants (`permission`, `auto_continue`, `discovery`, `doc_search`, `code_search`, `network_guard`, `content_guard`); a `slotModel` method; and a `resolveDecider` method on `Agent`.

## Steps

1. Write `decider_test.go` first. Pin that `*TypesafeClient` satisfies `Decider` with a compile-time assertion — this is the property that makes the whole plan cheap, so a rename or re-sign must fail loudly. Pin that `slotModel` returns `typesafe/jev-latest` for all seven slots. Run the tests; they fail because the interface does not exist.

2. Write `decider.go` with the interface, the slot type and constants, a `defaultJudgeModel` constant, `slotModel` (returns the shared default unconditionally in this part; a later change makes it read the per-slot config keys), and `resolveDecider`. `resolveDecider` builds the client outside any lock and returns nil when the model is unset or the provider has no credential, matching how `NewClient` already refuses to build a keyless client. Run the tests; they pass.

3. Widen the seven judge functions from `*TypesafeClient` to `Decider`. In `consultPermissionModel`, change the type assertion on the result of `newClientFn` from `*TypesafeClient` to `Decider`. Replace the bodies of `networkGuardJudgeClient` and `contentGuardClient` with calls to `resolveDecider` for their slots, so they stop hardcoding their model constant.

4. Replace the `discoveryState` judge cache. It currently memoises with a `sync.Once`, which would pin the first model resolved for the life of the session and make a later config change look like it did nothing. Replace it with a mutex plus a stored model id: read the current slot model, check the cache, and only rebuild on a mismatch. Read the slot model **once** and pass it in — reading it twice lets a concurrent config reload return two different values and cache a client under the wrong key.

5. **Compute the client before taking the cache lock.** The lock protects the cache fields only. Calling the resolver while holding it contradicts the plan's own lock rule and widens the critical section for no benefit.

6. Add a comment to `typesafe.go` recording that `TypesafeClient` satisfies `Decider` unchanged, and that Jev's documented budget is 64k per request but only 32k for `state` — the stricter of the two backends, and therefore the value a shared state-size budget must be sized to.

7. Run the full agent and tool package suites. **Every existing judge test must pass unchanged** — that is the proof this part altered no behaviour.

## Checks

- `go build ./...` and `go vet ./...` clean.
- `gofmt -l` prints nothing for touched files.
- `go test ./internal/agent/ ./internal/tool/ -count=1` passes with no edits to existing test expectations.
- `go test -race ./internal/agent/ -count=1` passes (≈2 min; let it finish).

## Review focus

Confirm the cache invalidation actually works: a test that changes the slot's model and asserts the next `discoveryJudgeClient` call returns a client for the new model. Without it, the `sync.Once` bug can return.

Confirm `resolveDecider` is never called while `judgeMu` is held.

## Done when

`Decider` exists, all seven judges accept it, every slot still resolves to Jev, and no existing judge test was modified.