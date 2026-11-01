---
type: Design
title: Chat Verbosity Display — Design Spec
description: Approved design spec for chat verbosity display with as-built status notes, including the 2026-11-01 as-shipped agent-run card note.
tags:
  - design
  - spec
  - web
  - chat
  - settings
  - verbosity
  - superpowers
timestamp: 2026-11-01T01:23:28Z
status: deprecated
deprecated_reason: 'Accidental near-duplicate: a doc_write was issued with the wrong path (specs/ instead of superpowers/specs/) and a truncated body. The authoritative page is superpowers/specs/2026-09-24-chat-verbosity-display-design.md.'
---
# Chat Verbosity Display — Design Spec

**Type:** Design  
**Description:** Approved design spec for chat verbosity display, implemented 2026-09-25 (local web/desktop SPA only): chat_verbosity config (full/balanced/quiet presets + per-category overrides), GET/PUT /api/config/ocode/chat-verbosity with chat_verbosity_changed as an invalidation-only event (payload never applied, reconnect refetch), Settings → Chat display form, ChatPanel-owned virtualized disclosure state with policy revision, single ToolBlock outer/inner gate model, latest-thinking invariant, and strict no-silent-normalization failure handling. Design record with as-built status notes.  
**Tags:** design, spec, web, chat, settings, verbosity, superpowers  

---

# Chat Verbosity Display — Design Spec

**Status:** Approved design (2026-09-24), **amended the same day after advisor review**; **implemented 2026-09-25** (local web/desktop SPA only — TUI rendering and remote-host config fetching unchanged, per the v1 scope in §3). The body below remains the design record as approved, plus amendment 7 below: future-tense wording describes what was built, and the shipped implementation follows it (shared chat_verbosity config, authenticated GET/PUT /api/config/ocode/chat-verbosity, invalidation-only chat_verbosity_changed, ChatPanel-owned disclosure with policy revision, single ToolBlock outer/inner gate, latest-thinking invariant, strict 400 failure handling with no silent normalization, and virtualizer scroll anchoring), except as corrected by **amendment 7** (the §8 dynamic `Follow preset` label ships in the same change, and the shipped resolver's `balanced`/`tool_calls` cell — which had diverged to `expanded` — is realigned to §9's `collapsed`); and except that §11 search force-open ships **transient** — a `forceOpen` render prop derived from the current match, never written into the disclosure map — so a collapsed block reverts to its manual/policy state when the match cursor moves away and a manual choice is never overwritten. For the as-built file map and regression tests see skills/ocode-web/SKILL.md item 35 and the 2026-09-25 CHANGES.md entry.

**Amendment note (2026-09-24):** six advisor-reviewed changes are incorporated throughout: (1) `chat_verbosity_changed` is an invalidation signal only — its payload is never applied (§5, §6, §7); (2) disclosure state is owned by `ChatPanel` in a policy-versioned map because virtualized rows unmount (§7, §10, §11); (3) precise "latest thinking" semantics (§9, §10); (4) a single `ToolBlock` implementation with one outer call/details gate and one inner output gate (§10); (5) explicit virtualizer anchoring on policy changes (§11); (6) strict failure handling with no server-side normalization of malformed values (§6, §7, §12). **Amendment 7 (2026-09-25)** adds a seventh change: (7) §8's per-category override selects show a dynamic `Follow preset — <Expanded|Collapsed>` label whose suffix is the §9 cell for the currently selected draft preset (the option value stays `preset`), and the `balanced`/`tool_calls` resolver cell is aligned to §9 (`collapsed`) — §9 itself is unchanged.

**Amendment 7 — resolver divergence note:** the shipped resolver — `web/src/lib/chatVerbosity.ts` (`resolveChatDisplayPolicy`) and `internal/config.ResolveChatVerbosityPolicy` (`internal/config/ocodeconfig.go`) — had diverged from §9 for the `balanced`/`tool_calls` cell: both resolved `expanded` under `balanced` (collapsing tool-call details only under `quiet`) instead of §9's `collapsed`. The code is being aligned to the spec — §9 is the source of truth and is not amended — and `skills/ocode-web/SKILL.md` item 35(b) is updated in the same change.

**As-shipped note (2026-11-01) — agent-run cards follow the Chat display policy:** the agent-run surfaces (the chat's Agents rail, the Agents tab list + detail, and the inline `task`/`agent` tool card in the transcript) now render their run transcripts through the chat's own blocks, so the policy this spec defines applies to them too: `web/src/components/Agents/RunNode.tsx` renders each transcript entry via `RunTranscriptEntry`, reusing `AssistantText` (markdown), `ThinkingBlock`, and `ToolBlock` — including both `tool_calls`/`tool_output` gates and the 20-line tail preview. Two rules shipped with it: (a) a tool call and the role-`"tool"` message answering it are grouped into ONE block keyed by the tool-call id — `internal/server/handler_runs.go` (`runMessagesDTO`, `runToolCallDTO.ID`) carries that id to the client (and keeps reasoning-only transcript entries, which are visible content in the chat UI); (b) `web/src/components/Agents/agentRunDisplay.tsx` (`AgentRunDisplayScope`) supplies the `useChatVerbosity()` policy plus a `createChatDisclosureStore()` when no `ChatDisplayContext` provider exists (rail, Agents tab), and is a strict pass-through when a provider already exists (the inline task card keeps ChatPanel's policy AND disclosure store, so expanding a block there stays one shared choice). §9 and the rest of this design record are unchanged.

---