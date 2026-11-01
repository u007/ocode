---
type: Design
title: browser_run — a Jev-driven step loop over the browser tool (design)
description: Design spec for browser_run, an agent-layer tool that drives a browser task to completion by letting Jev pick each click from an indexed controls list, re-entering the normal browser tool call path per step so every mutating sub-step keeps its permission ask, while the main model pays one tool call for N steps. Modelled on agent-labs-dev/fastbrowse (Jev picks, LLM reads and writes, code owns the gates) and adapted to ocode's existing cdp snapshot, RefTable, fill_form, Decider slots and state budget.
tags:
  - browser
  - jev
  - typesafe
  - decider
  - agent
  - tools
  - permissions
  - design-spec
timestamp: 2026-11-01T01:00:00Z
---
# browser_run — a Jev-driven step loop over the browser tool

Date: 2026-11-01
Status: approved in chat (option B chosen over delegating to the fastbrowse binary).

## Summary

Today every browser click is a full turn of the main model: `snapshot` (up to 32 KB) → model →
`click` → `snapshot` → model. Each snapshot stays in the transcript, so turn three re-sends turns
one and two. A ten-field form is ten frontier-model turns carrying ~300 KB of page text.

`browser_run` moves the per-click choice to Jev. The main model calls it once with a goal and the
data it may type. Inside, a loop indexes the page into the controls it has, asks Jev one batched
question (which operation, which target, which value), gates and runs that one action through the
**normal `browser` tool call path**, settles, and repeats. The main model gets back a compact step
log. No LLM is called inside the loop.

The reference design is `agent-labs-dev/fastbrowse` (MIT, Rust): Jev picks from indexed controls,
the LLM plans/reads/writes, code owns authorization, freshness and budgets. Their published
head-to-head: 10.7× cheaper per run, 99.2% vs 96.3% passed, 4.7 LLM calls a run (from 11.3),
3.6 s slower per task. Not independently verified; their task catalog differs from ours.

## Why in the agent layer, not inside the browser tool

The browser tool lives in `internal/server` and has no access to the agent's deciders or permission
layer. The permission flow (static `Decide` → `browserDecision` → network guard → judge → human ask)
is one inline block in `Agent.handleToolCallWithContext` (`internal/agent/agent.go`), and it is
load-bearing: the user-tab read gate, the sensitive-upload ask, `browser_page` for the judge and the
child-ask registry for subagents all hang off it.

So the loop is an `internal/agent` tool (`browserRunTool`, like `TaskTool`) and **each sub-step is a
real call to `a.handleToolCallWithContext(ctx, "browser", stepArgs, binding, syntheticID)`**. That
buys, with no refactor: refusals for read-only agent types, static rules, the per-click judge with
`browser_page`, human asks (including through the child-ask registry), the network guard on
`navigate`, the inbound content guard on page-derived results, Stop cancellation, and the snapshot
store's tool-call bookkeeping. Sub-step results are consumed by the loop and never appended to the
transcript; only `browser_run`'s own result is.

## What the main model sees

Tool `browser_run`, registered only when the session has a `browser` tool. Static definition (prompt
cache). Arguments:

| arg | type | meaning |
| --- | --- | --- |
| `goal` | string, required | what to achieve on the page, in plain words |
| `data` | object of string → string, optional | named values the loop may type (`{"email": "…"}`). Jev sees the **names** only and picks one per fill; values never enter a Jev request |
| `tab` | string, optional | as for `browser`; default the active tab, else the last tab this agent opened |
| `max_steps` | integer, optional | default 15, at most 40 |

Result (plain text, bounded, header says it is untrusted web content):

```
browser_run: status=done steps=4 jev_calls=5 page=https://shop.example/cart "Cart"
1. click @e12 button "Add to cart" → page changed
2. fill @e3 textbox "Email" ← data.email → ok
3. check @e9 checkbox "Agree to terms" → ok
4. click @e14 button "Continue" → page changed
```

Statuses: `done` (Jev judged the goal met, probability ≥ 0.85), `unsure` (Jev's operation choice
below the floor; the result lists the top controls so the main model can take over), `needs_input`
(Jev chose fill but no `data` key fits), `stalled` (page fingerprint unchanged for 3 steps, or Jev
chose `stuck`), `max_steps`, `denied` (a sub-step's permission ask was denied or refused: the loop
stops at once, the step log says which), `page_too_dense` (state cannot be brought under the per-step
budget), `error` (CDP or judge transport error, after at most one retry of a **read**; a mutation is
never retried).

Only `done` means the goal was judged met. The main model is told, in the tool description, to
verify with `browser` reads before claiming success, and that `data` values are typed into the page
so secrets must not be passed.

## One step

1. **Index.** Call `browser` action `controls` (new read action, below). It settles the page first:
   `document.readyState` not `loading`, then 200 ms with no DOM mutations, capped at 5 s. It
   returns JSON: page url, title, a fingerprint, the viewport text capped at 6 KB, and
   `controls[]`: `{ref, role, label, value?, states[], context?, operations[]}` for every interactive
   element of the top document, in document order, at most 160, plus `omitted`. `operations` are
   derived from the accessibility role: button/link/tab/menuitem → `click`; textbox/searchbox/
   combobox/spinbutton → `fill`, `click`; checkbox/radio/switch → `check`, `uncheck`;
   `context` is the nearest named ancestor's name (heading, group, form, region, dialog).
   The content guard scans this result (`browserGuardedOutputs`).
2. **Pick.** One `DecideCtx` on slot `browser_run` with questions:
   `done` (noul: goal met on this page, given the step log), `operation` (choice over the operations
   the page offers plus `scroll_down`, `press_enter`, `stuck`), `target_click`, `target_fill`,
   `target_check`, `target_uncheck` (one choice each over the controls offering that operation; only
   for operations that have candidates), `value_fill` (choice over `data` key names plus `none`).
   Answers cannot see each other, which is why target and value are asked alongside the operation.
   State: `goal`, `date`, `page{url,title,text}`, `data_keys`, `recent_actions` (last 8),
   `elements` (the `controls[]` as offered), `omitted_elements`. The state goes through
   `prepareDecisionState` **and** a stricter per-step target: marshalled ≤ 30 KB, trimming viewport
   text first, then elements from the end, else `page_too_dense`. Larger Jev requests 503 (fastbrowse
   measured 27k-token steps answered 4 in 10).
3. **Decide.** `done` ≥ 0.85 → stop `done` before acting. Else take `operation`; confidence below
   0.55 → stop `unsure`. `stuck` → stop `stalled`. `fill` with `value_fill` = `none` → `needs_input`.
   Every answer passes `validateAnswer`; a rejected answer is treated as `unsure`, never as a pick.
4. **Act.** Build the `browser` args for the sub-step (`click`/`fill`/`check`/`uncheck`/`key`/`scroll`
   with `target` = the ref) and run it through `handleToolCallWithContext`. The args also carry
   `run_step` (`{"goal":…,"n":3,"of":15,"label":"Add to cart"}`) which only the permission dialog
   summary reads, so a human sees "browser_run step 3/15: click button “Add to cart”" and the goal.
   A permission denial, a refusal or a tool error ends the loop (`denied` / `error`). A `stale ref`
   error ends the step as `error` only after one fresh `controls` read and one retry of the **read**;
   the mutation itself is never re-sent.
5. **Loop** until a stop status or `max_steps`. Fingerprint = url + hash of the controls listing;
   three consecutive identical fingerprints after a mutation → `stalled`.

Per-call wall-clock bound: each sub-step keeps the browser tool's 30 s deadline; the whole run is
bounded by `max_steps` and by the turn context (Stop cancels it like any tool call).

## Permissions, refusals, guards

- `browser_run` itself is `PermissionAllow` by default: it performs nothing on its own. Every mutating
  sub-step asks exactly as a hand-written `browser` call would. In normal mode that can be one ask
  per click; auto mode lets the judge decide each with `browser_page`; YOLO and an "always allow
  browser" rule lift them, as today. Locked mode denies `browser` and so denies every sub-step.
- Read-only agent types (`explore`, `context`) are refused at the first mutating sub-step by the
  browser tool's own `Refusal`; the loop reports `denied`. `browser_run` is not main-agent-only.
- `navigate`/`open` are **not** operations the loop can choose. Jev may only act on controls the page
  has. Following a link is a click on it.
- The content guard scans `controls` results (page text and labels) through the normal tool path, and
  `browser_run`'s own result is added to the guarded outputs (it quotes labels and the title).
- `data` values are typed verbatim; the dialog shows only the key name and length, like `fill`.

## Jev slot and config

New `judgeSlot` `slotBrowserRun = "browser_run"`, config `ocode.browser_run.judge_model`
(`JudgeModelConfig`, default `typesafe/jev-latest`), resolved per call with `resolveDecider`
(never cached nil). Own floors: `browserRunMinConfidenceDefault = 0.55` for the operation choice,
`browserRunDoneAcceptFrom = 0.85` for the done noul. Neither reuses the permission floor. Usage is
booked with `deciderLabel(client)`. An unset or keyless judge makes `browser_run` return `error:
no decision judge configured`; it never falls back to another backend and never to the main model.

## Out of scope (v2, tracked in TODO.md)

- Relevance pre-filter (one noul per control) when a page offers more than 160 controls; v1 offers
  the first 160 in document order and reports `omitted`.
- `pick`/`select`/`upload`/`type` operations and option lists; `frame` targeting.
- Model-written field text (fastbrowse's `FIELD_TEXT` call); v1 types only caller-supplied `data`.
- Verbatim-quote citations and a cited answer.
- Cross-tab operations and dialogs.

## Measurement

Run the kaizen webforms corpus with and without `browser_run`; compare main-model input tokens per
task, Jev calls, wall-clock and the checker's pass rate. Thresholds (0.55 / 0.85 / 160 / 30 KB) are
starting points copied from fastbrowse and are to be re-tuned on that corpus.

## Code anchors (verified 2026-11-01, re-verify before commit)

- `internal/server/browser_tool.go`: `browserActions` (line 57), `browserSurface` interface (74),
  `browserArgs` (152), `ExecuteCtx` (332), `perform` (405), `PageContext` (559).
- `internal/browse/cdp/pagedom_read.go`: `Snapshot` (89), `snapshotWriter.walk` (134),
  `snapshotStates` (85). `refs.go`: `RefTable.Mint` (38). `target_agent.go`: `ReadyState` (142).
- `internal/agent/agent.go`: `handleToolCallWithContext` (3965), `a.tools["task"]` registration
  (1452), `executeToolCallWithContext` (5768).
- `internal/agent/decider.go`: slots (16–26), `slotModel` (62), `resolveDecider` (121).
- `internal/agent/relevance_typesafe.go`: the judge-call pattern (`judgeRelevanceQuestions`).
- `internal/agent/state_budget.go`: `prepareDecisionState` (105), `validateAnswer` (274).
- `internal/agent/permissions.go`: `browserObserveActions` (5708), `browserAction` (5740).
- `internal/agent/content_guard_typesafe.go`: `browserGuardedOutputs` (265).
- `internal/config/ocodeconfig.go`: `JudgeModelConfig` (927), `OcodeConfig` (995).
