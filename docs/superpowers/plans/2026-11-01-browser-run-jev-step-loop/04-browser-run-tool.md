# Part 04 — the `browser_run` tool

Files: new `internal/agent/browser_run.go`; `internal/agent/agent.go` (registration next to the
`task` tool and the session-tool wiring), `internal/agent/content_guard_typesafe.go` (guard the
tool's own result). Depends on parts 02 and 03. Read the spec's "One step" section before starting,
and read `relevance_typesafe.go`, `goal.go` (`decideGoal`) and `state_budget.go` for the house
pattern of a judge call: `resolveDecider` per call, `prepareDecisionState`, `DecideCtx`,
`RecordSideUsage(..., deciderLabel(client))`, `validateAnswer` on every answer, `emitDebug` lines.

## Tool

`browserRunTool` holds a back-pointer to the agent (like `TaskTool`). `Name()` is `browser_run`,
`Parallel()` false, `Definition` static with `goal` (required), `data` (object of strings), `tab`,
`max_steps` (default 15, max 40). The description tells the model: Jev picks each click from the
page's controls, nothing is typed except `data` values so no secrets, only `status=done` means the
goal was judged met, verify with `browser` reads before claiming success, and use plain `browser`
actions for pick/select/upload which the loop cannot do.

Implements `tool.ContextualTool` (the turn context carries Stop) and `tool.AgentScopedTool`
(`ForAgent` returns an instance bound to the same agent; the sub-steps run on that agent's own
`browser` instance because they go through its tool map). Registered in the same place the session's
tools are assembled, only when `a.tools["browser"]` exists at that time.

## The loop (per spec)

1. `controls` sub-step through `a.handleToolCallWithContext(ctx, "browser", args, b, id)` where `id`
   is `<toolCallID>_run<N>` (the parent's tool-call id is available on the binding path used by
   `TaskTool`; follow it). Parse the JSON after the header line; a parse failure is `error`.
2. Build the state and questions exactly as the spec lists them. State size: after
   `prepareDecisionState`, measure the marshalled state; above 30 KB trim `page.text` to 2 KB, then
   drop elements from the end (raising `omitted_elements`), still above → `page_too_dense`.
3. Decide floors: `browserRunDoneAcceptFrom = 0.85`, `browserRunMinConfidenceDefault = 0.55`,
   `browserRunMaxControls = 160`, `browserRunStateBudgetBytes = 30_000`, `browserRunStallSteps = 3`.
   A `validateAnswer` rejection on `operation` is `unsure`; on `done` it means "not done".
4. Act: translate the pick into `browser` args (`click`/`fill`/`check`/`uncheck` with `target`;
   `press_enter` → `key` `Enter`; `scroll_down` → `scroll` `down`) plus the `run_step` object. Run
   it through `handleToolCallWithContext`. A result that is a permission denial (the tool message
   produced by `denyToolMessage`), a refusal error, or any error ends the loop with `denied` or
   `error`; a `stale ref` error re-reads `controls` once and ends with `error` if the ref is gone.
5. Fingerprint stall rule and `max_steps` as in the spec.

The loop never calls an LLM, never calls `navigate`/`open`, never retries a mutation, and never
touches the transcript: sub-step results are local values.

## Result

Plain text: a header line `browser_run: status=<s> steps=<n> jev_calls=<m> page=<url> "<title>"`,
then one numbered line per step (`<op> @ref <role> "<label>" [← data.<key>] → <outcome>`), then for
`unsure`/`stalled`/`needs_input` a short `next:` list of up to 10 controls with refs so the main model
can continue by hand. Prefix the whole result with the browser tool's untrusted-content sentence.
Add `browser_run` to the content guard's tool set (its result quotes labels and the title) in the way
`webfetch` is registered, not via `browserGuardedOutputs` which is keyed by `browser` action.

## Verify

```
go build ./... && go vet ./internal/agent/ && go test ./internal/agent/ -run 'BrowserRun' -count=1
```

(Tests themselves are part 05; this part must at least compile and keep the existing agent suite
green: `go test ./internal/agent/ -count=1 -run 'Tool|Browser|Guard'`.)
