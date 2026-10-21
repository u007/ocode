# Suggested responses — Design

- **Date:** 2026-10-20
- **Status:** Approved in chat (2026-10-20) — design presented and accepted; implementation pending
- **Classification:** Architectural (new cross-layer contract: model output → backend → SSE/bus → web store → composer; new config setting with prompt-cache implications; new persisted artifact)
- **Owners:** ocode web/desktop
- **Artifact location:** `docs/superpowers/specs/` is the superpowers process convention for
  design/plan artifacts (8+ existing specs there), separate from the curated OKF bundle under
  `docs/concepts/` that only the `context` sub-agent writes. This is a working spec, not a
  bundle page.

## 1. Problem

After ocode answers, the user is left staring at a blank composer with no guidance
about what to do next. The web/desktop composer has a quick-action strip
(`web/src/components/Chat/QuickActionsBar.tsx`) below the send row, but those chips
are STATIC: user-configured canned messages, not suggestions derived from what the
agent just did.

The user wants the agent to propose, at the end of each turn, up to five short
follow-up options the user can click to continue. The feature must be controllable
from the Settings UI and be on by default.

## 2. Decision summary

1. A new config toggle `suggested_responses_enabled`, default **true**.
2. When enabled, a static system-prompt fragment is injected into the agent's
   cached system prompt telling the model to emit up to five suggested follow-ups
   in a machine-readable block at the end of its reply.
3. The model emits the suggestions as a marked block inside its normal assistant
   text. The backend strips the block from the visible text and publishes it as a
   separate `suggestions` event.
4. The web client renders the suggestions in a row **immediately above** the
   quick-action strip. Clicking one **fills the composer** (it does not send).
5. The suggestions are **persisted** with the assistant message, so a page reload
   still shows them.
6. Scope: **web/desktop only** for this cut. The TUI has no quick-action strip
   today (`grep -rn "QuickAction" internal/tui` returns nothing), so a TUI parity
   gap already exists for quick actions; the TUI is not extended here.

### Answers to the open questions (decided 2026-10-20)

| # | Question | Decision |
|---|----------|----------|
| Q1 | Click behaviour | **Fill the composer** (quick-action "fill" mode). Never auto-send. |
| Q2 | Persistence | **Persisted** on the assistant message → survives reload. |
| Q3 | Config placement | **Extend the existing Features form + `/api/config/ocode/features`** with a third bool. |
| Q4 | TUI parity | **Web/desktop only** for this cut. |
| Q5 | Streaming | The row is hidden while a turn is streaming; the new set appears when the turn ends. |

## 3. Why this transport (and not the alternatives)

The marked-block-in-assistant-text transport was chosen over three alternatives, and
the reasons are load-bearing.

- **A dedicated `suggest_responses` TOOL was rejected.** Per the repo's prompt-cache
  stability rules (`CLAUDE.md`, `docs/concepts/prompt-cache-stability.md`), Anthropic
  caches one linear prefix in the fixed order `tools` → `system` → `messages`, so ANY
  change to the tools array invalidates the `system` block AND the message prefix. A
  tool that exists in some turns and not others would re-cache the whole prefix every
  time. The tool result would also render as a tool bubble in the transcript.
- **A separate post-turn LLM call was rejected.** It costs an extra model call and an
  extra round-trip of latency on every turn, and the suggestions would come from a
  second model context rather than the one that just did the work.
- **A system-role message in the transcript was rejected.** Provider builders hoist
  every system-role message into the cached system field
  (`collectAndRemoveSystemMessages` in `internal/agent/client.go`), so a
  per-turn-varying system message re-caches the entire prefix every turn.

The assistant's OWN OUTPUT is not part of the cached prefix, so a marker inside the
assistant's reply is cache-safe. The block is also stripped from the PERSISTED
transcript content, so the model never sees its own suggestions echoed back on the
next turn (which would waste tokens and risk re-emitting them).

## 4. The wire format

The model emits, at the very end of its reply:

```
<ocode:suggestions>
["Run the test suite", "Open a PR", "Explain the diff"]
</ocode:suggestions>
```

Rules:

- At most **five** entries. More are truncated server-side.
- Each entry is a short, single-line, imperative phrase (the user clicks it to fill
  the composer, so it must read as something to SAY or DO).
- The block is optional: the model may omit it (e.g. a purely informational answer),
  and the row then simply does not render.
- Parsing is tolerant: a malformed block is discarded silently (the visible text is
  kept intact) rather than failing the turn.
- The block is anchored on the FINAL closing marker and the last opening marker before
  it. A prose mention of the opening marker earlier in the reply cannot swallow the
  text between the mention and the real block. The live stream applies the same rule,
  so the streamed text matches the persisted message.

## 5. Configuration

- Config key: `suggested_responses_enabled` in `ocodeconfig.json`.
- Default: **true** (unlike `doc_prompt_enabled`, which defaults false).
- Exposed in the web/desktop Settings UI. The toggle lives in the EXISTING **Features**
  form and the EXISTING `GET/PUT /api/config/ocode/features` endpoint, extended with a
  third boolean — not a new endpoint, not a new form group, and not on the
  quick-actions form.
- Profile-scoped? **NO** — it is a machine-level presentation preference, not a
  model/provider profile override. It is therefore NOT part of `ProfileDelta`.

### Config plumbing (mirrors `doc_prompt_enabled`)

| Location | Change |
|----------|--------|
| `internal/config/ocodeconfig.go` — `OcodeConfig` | `SuggestedResponsesEnabled bool` |
| `internal/config/ocodeconfig.go` — `ocodeConfigFile` | `SuggestedResponsesEnabled *bool \`json:"suggested_responses_enabled,omitempty"\`` |
| `internal/config/ocodeconfig.go` — `defaultOcodeConfig()` | `SuggestedResponsesEnabled: true` |
| `internal/config/ocodeconfig.go` — file load | apply the file value when present |
| `internal/config/ocodeconfig.go` — config payload dump | emit the key |
| `internal/config/ocodeconfig.go` — save | `SaveOcodeFeatures(mem, doc, suggested)` |

## 6. Prompt injection

A new static const fragment in `internal/agent/prompt.go`, injected into
`BasePromptMessages()` exactly like `docPromptContent` (the doc-prompt fragment at
`prompt.go:230`), gated on a new `Agent.SuggestedResponsesEnabled()` accessor.

The fragment is a `const`, not a per-turn value, so it is a function of
construction-time state only and never varies per turn — the requirement the repo's
`internal/agent/append_stable.go` cache-stability contract imposes. Adding it re-caches
the system prefix once for existing sessions, which is the accepted cost of a new
system fragment (same as the recap fragment).

The fragment is injected for PRIMARY agents only. Sub-agents, ask helpers and the
advisor turn it off (the same pattern as `SetRecapPromptEnabled` at
`internal/agent/subagent.go:433`, `ask.go:223`, `advisor_tool.go:326`), because their
output is a tool result consumed by the model, not a response shown to a user.

## 7. Runtime wiring

- `internal/agent/agent.go`: new `suggestedResponsesEnabled` field,
  `SetSuggestedResponsesEnabled` / `SuggestedResponsesEnabled` accessors, and the
  `NewAgent` read from `cfg.Ocode.SuggestedResponsesEnabled`.
- Live agents pick the setting up the same way the doc-prompt toggle does: the TUI
  carries it across agent rebuilds (`internal/tui/model.go:1280`), and the server
  rebuilds resident agents through the existing `rebuildAgentForPreferenceChange`
  seam (`internal/server/handler_skills.go:225`) so a just-saved setting takes effect
  without a server restart.

## 8. Server → client transport

- New session-scoped event type `suggestions`, added to `sessionScopedEvents` in
  `internal/server/event_bus.go`. The Go list and the web `SESSION_SCOPED_EVENTS` set
  in `web/src/lib/sessionEvents.ts` MUST stay in step (the web list mirrors the Go one).
- The headless (web/desktop) path publishes it from the agent's `OnMessage` callback in
  `wireHeadlessAgentCallbacks` (`internal/server/handler.go:891`), which is the single
  place that already translates agent messages into bus frames.
- The legacy SSE path (`internal/server/handler_sse.go`) publishes the same event so
  the older client contract stays intact.

## 9. Web client

- `web/src/stores/chatStore.tsx`: the session slice gains a `suggestions` field, plus a
  `SET_SUGGESTIONS` action.
- `web/src/components/Chat/SuggestedResponses.tsx` (new): the row. Purely
  presentational, like `QuickActionsBar`.
- `web/src/components/Chat/ChatInput.tsx`: renders the row immediately above
  `<QuickActionsBar>` (the quick-actions block is the LAST element inside the
  composer's root div, so "before the quick actions" means directly above it, inside
  the same padded container).
- Clicking a suggestion fills the composer (the quick-action "fill" mode behaviour) and
  focuses the textarea. It never auto-sends.

## 10. Persistence

The suggestions ride on the assistant message that produced them, so a transcript
reload restores them. The persisted assistant content has the block STRIPPED (the
transcript shows only the human-readable reply), and the suggestions are carried in a
dedicated field on the message.

## 11. Validation

- Go: config round-trip and default; prompt injection present/absent per the flag;
  block parse/strip (well-formed, malformed, over-five, empty); event published with
  the right payload.
- Web: features-form toggle round-trip; store reducer; row rendering; click fills the
  composer and does not send; row hidden while a turn is streaming.
