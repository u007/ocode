---
type: Decision
title: TTS Speech Playback Design Specification
description: 'User-approved design for TTS speech playback across desktop/web UI, covering model selection, playback semantics, UI, error handling, and testing. Updated with rendered-text extraction rule (DOM-based, never markdown source) and the fail-open spoken-summary contract (§10.2, 2026-09-28). Amended 2026-09-29: short plain-prose messages skip the summariser LLM entirely (§10.2 short-text gate). Amended 2026-09-29: client-side FIFO speak queue and now-playing `project · session` label (§7, §8).'
tags:
  - TTS
  - speech
  - design
  - local-model
  - supervisor
  - DOM-extraction
  - speech-summary
  - fail-open
timestamp: 2026-09-29T03:51:10Z
resource: "docs/superpowers/specs/2026-09-09-tts-speech-playback-design.md"
---
# TTS Speech Playback Design Specification

**Type:** Decision  
**Description:** User-approved design for TTS speech playback across desktop/web UI, covering model selection, playback semantics, UI, error handling, and testing. Updated with rendered-text extraction rule (DOM-based, never markdown source) and the fail-open spoken-summary contract (§10.2). Amended 2026-09-29: short plain-prose messages skip the summariser LLM entirely (§10.2). Amended 2026-09-29: client-side FIFO speak queue and now-playing `project · session` label (§7, §8).  
**Resource:** docs/superpowers/specs/2026-09-09-tts-speech-playback-design.md  
**Tags:** TTS, speech, design, local-model, supervisor, DOM-extraction  

---

# TTS Speech Playback Design Specification

**Status**: Active
**Last Updated**: 2026-09-29

## Overview

This specification defines the TTS (Text-to-Speech) speech playback system for the ocode desktop and web UI. It covers model selection, playback semantics, user interface, error handling, and testing across desktop and web platforms.

## Approved Decisions

### 1. Platform Scope
- Desktop/web shared React UI and Go server; no TUI in this phase.
- The implementation targets desktop (macOS, Linux) and web browsers, with a shared React frontend and Go backend.

### 2. Flat Voice Choices
- Browser Native (default), Piper, Kokoro (Fish Audio and Breeze removed 2026-09-27)
- No cloud TTS — all engines are locally hosted or browser-native only.

### 3. Browser Native
- Uses browser SpeechSynthesis API
- Bypasses server/model manager entirely
- State is browser-tab-local (not shared across tabs/sessions)

### 4. Local Engines via TTS Supervisor
- Local engines (Piper, Kokoro, Fish Audio, Breeze) are server-side through a shared TTS supervisor
- Reuse /localmodel primitives:
  - EnsureArtifact-style cache/checksum/atomic install
  - Process supervision, parent monitor, health checks
  - Adoption and cross-process startup protection
  - TTS-specific manifests/protocol/config/cache namespace
- Selecting a local engine downloads/sets it up immediately with no manual /localmodel command or install/daemon step
- Selection does not speak merely by being selected — shows progress and readiness state
- If a local engine fails, it remains selected with an error and Retry button; switching to Browser Native is a deliberate user action, never an automatic fallback

### 5. GPU First, CPU Fallback
- GPU first, CPU automatic fallback
- No fallback to Browser Native if local setup/inference fails after CPU
- Preserve the user's selection and show clear error + Retry button
- Selection generations prevent stale setup from becoming active

### 6. Global Download Lock & Single-Flight
- One global ownership-safe cross-process advisory download lock for all ocode model downloads
- Lock covers cache check → temp download → checksum → extraction → metadata → atomic install
- Lock ownership identity/token tracked; safe release; crash recovery without deleting a live holder
- Cache namespace: <global-data>/models/tts/<engine>/<voice-or-model-id>/<manifest-version>/<GOOS>-<GOARCH>/, with checksummed artifacts and metadata
- Distinct from chat/completion /localmodel model IDs and cache entries
- Waiters recheck after lock release; checksum-based artifact validity rather than mtime
- In-process keyed single-flight to prevent duplicate downloads
- Selection generations prevent stale setup from becoming active; stale setup may finish a verified cache install but must not activate, change selection, or affect playback
- Cancellation cooperative where supported
- Partial files never count as installed

### 7. Selection & Playback Replacement
- Superseded 2026-09-29: new speak requests no longer replace the current playback (previously "New selection/text immediately replaces current playback and clears queue"). They are APPENDED to a client-side FIFO queue in `SpeechProvider` (`web/src/components/Speech/SpeechProvider.tsx`) and play one at a time, in click order (`enqueue`, `SpeechProvider.tsx:598`).
- Only an explicit Stop (`stop`, `SpeechProvider.tsx:278`) or an engine switch clears the queue. Stop also cancels the in-flight item, including one still awaiting its summary.
- The queue is client-side, not server state. The per-server-process playback generation rule below is UNCHANGED: each queued item gets a fresh generation as it starts, so there is still exactly one active local playback generation per process.
- Summarising resolves when an item reaches the HEAD of the queue, not when it is enqueued (`drain`, `SpeechProvider.tsx:544`). A burst of N speak requests therefore costs one in-flight summariser call at a time, never N concurrent 60s model calls racing to land out of order.
- A failed item (synthesis error, audio error) does not strand the queue: the worker moves on to the next item and the error stays visible in the toolbar. Errors are still surfaced, never silently swallowed — consistent with §13.
- Each queued item carries the project title and the chat session title captured AT ENQUEUE TIME, plus the session id, host and speak-mode gate used for summarising. A queue outlives a tab switch, so these are snapshotted per item: an item queued in session A is summarised against session A and labelled with A's project/session even if the user is now looking at B. The toolbar shows the now-playing item's `project · session` label for exactly this reason.
- Browser Native remains tab-local, and disconnect behaviour is unchanged: cancel the active generation, do not resume automatically.
- One active local playback generation per running ocode server process, shared across its tabs/sessions
- Separate ocode server processes are not one shared playback state
- Browser Native remains tab-local
- On server/process/browser disconnect, cancel/expire the active generation and do not resume automatically; a later request re-adopts or restarts the model
- Late events ignored

### 8. Shared Toolbar
- Play / Pause
- Stop
- -10s, +10s skip
- Timeline seek
- Elapsed / Total time display
- Local audio is seekable
- Browser Native: pause/resume/cancel + best-effort estimated text-offset skip/seek
- A now-playing label showing `projectTitle · sessionTitle` for the item currently playing, omitted when neither is known. Truncated with the full value in the tooltip (`speechItemLabel`, `web/src/components/Speech/SpeechToolbar.tsx:20`; label span at `SpeechToolbar.tsx:55`).
- A `+N queued` badge counting only the items still WAITING (not the one playing), with an accessible name of "N queued" (`SpeechToolbar.tsx:63`).
- **Next** — abandon the current item and promote the next queued one. Disabled/absent when the queue is empty (`SpeechToolbar.tsx:66`).
- **Clear queue** — drop every waiting item but let the current one finish. Distinct from Stop, which silences the current item too (`SpeechToolbar.tsx:67`).

### 9. Settings Playback Modes
- Manual / Speak visible (default; button always present)
- Auto-play when at bottom: autoplay only newly completed assistant messages when chat is at bottom
- Respect browser user-gesture restrictions

### 10. Chat TTS
- Speak any message, selected text, or visible viewport text top-to-bottom without UI chrome
- Reject empty text
- Bound/chunk large text to reasonable sizes

#### 10.1 Rendered-Text Extraction (DOM) — web SPA

Speech text must always be extracted from the **rendered DOM**, never from the raw markdown source. Speaking markdown source produces audible artefacts ("hash Title", "asterisk asterisk bold", backtick-delimited code, raw link URLs) that degrade the user experience.

**Why DOM, not a markdown-source stripper:** the rendered DOM is the single source of truth for what the user sees — ReactMarkdown + remark-gfm + rehypeFileLinks transform the source before paint. A source-level stripper cannot stay in sync with the renderer's output and will silently drift as the pipeline evolves.

Three helpers in `web/src/components/Speech/speechUtils.ts` cover the extraction surfaces:

| Helper | Purpose | Used by |
|--------|---------|---------|
| `renderedSpeechText(root)` | Walk a single rendered DOM subtree; collapse whitespace; skip `[data-speech-exclude]`, `aria-hidden="true"`, script/style/svg; insert line breaks at block-level tags so adjacent paragraphs don't merge. | Per-message Speak button |
| `renderedSpeechTexts(root)` | Collect all `[data-speech-content]` blocks in DOM order into a single string. | "Speak visible" viewport button |
| `lastRenderedSpeechText(root)` | Return the last `[data-speech-content]` block (fallback to markdown source if virtualised node isn't mounted). | Auto-speak at-bottom |

**Component contract:**

- `data-speech-content` — marks the element whose text content should be spoken. Set on the markdown subtree (e.g. `AssistantText`'s rendered block) so the extractor knows where to walk.
- `data-speech-exclude` — marks elements inside a speech-content subtree that must NOT be spoken (e.g. the Speak button itself, which had its own label read aloud before this rule was enforced). The extractor skips these and all their descendants.

**Scope note:** `ThinkingBlock` reasoning and terminal selections are plain text (not markdown) and are passed through unchanged — no DOM extraction needed.

#### 10.2 Spoken Summaries — fail-open (added 2026-09-28; short-text gate added 2026-09-29)

Optional prose rewriting sits between extraction (§10.1) and synthesis. When
`SpeechSummaryEnabled` is on, `resolveSpeechText`
(`web/src/components/Speech/SpeechProvider.tsx`) first asks
`POST /api/sessions/{id}/speech-summary` to rewrite the extracted text into
spoken prose using that session's own agent — same credentials, active
profile and usage attribution as its turns. Settings pair is read/written via
`GET`/`PUT /api/config/ocode/speech-summary` (`SpeechSummaryModel`,
`SpeechSummaryEnabled`).

**Contract: a summary is an optimisation, never a prerequisite for speech.**

- **`200 {"summary": ""}` is the fail-open signal** — speak the original full
  text. Empty/whitespace-only summaries and request failures resolve to the
  original text identically; speech is never silenced by a side task failing.
- **A further cause of the same empty summary (added 2026-09-29): the message
  is short plain prose and is spoken verbatim.** `SummarizeForSpeech` returns
  `""` — the established "speak the original" signal — *before* resolving a
  client and *before* reading the 24h disk cache (nothing ever caches a
  summary for such text, so there is nothing to look up), gated by
  `speechTextNeedsRewrite` (`internal/agent/speech_summary.go:290`): the text
  must be at most `speechSummarySkipChars = 400` runes
  (`speech_summary.go:61`, the const block shared with
  `speechSummaryMaxInputChars`, `speechSummaryMaxOutputChars`,
  `speechSummaryTimeoutSeconds`, `speechSummaryCacheTTL` and
  `speechSummaryPruneInterval` — roughly 20 seconds of speech at 160 wpm)
  **and** carry no speakable artifact. Length alone is not the test:
  `speechTextHasSpeakableArtifact` (`speech_summary.go:153`) scans for a
  backtick (fence or inline code), `http://`/`https://`, a `@@` diff hunk
  header, crash markers (`panic:`, `Traceback (most recent call last)`,
  `Exception in thread`, `fatal error:`), and per line a `+`/`-` marker with
  no space after it (markdown bullets require the space, so that is a diff
  and not a list), a `$ ` shell prompt, two or more pipes (a table row), and
  any whitespace-delimited token that looks like a path or filename. The
  rationale is already written into the code: the summariser's own prompt
  (`speechSummarySystemPrompt`, `speech_summary.go:24`) tells the model to
  return an already-short plain-prose reply "almost unchanged"
  (`speech_summary.go:31`), so the round trip buys no rewrite — but *not*
  calling the model on a short code block or diff would read source code
  aloud, the exact failure the summariser was added to prevent, so the
  artifact scan errs towards firing. The skip logs one debug line
  (`speaking N chars verbatim; no summary needed`), consistent with the other
  early exits. No new config key, no new endpoint, no client change: an
  empty summary already meant "speak the full text". Two known,
  user-visible limits: (1) a table that reached the server as RENDERED text
  has no pipes left to detect, so a short table is read as its bare cell
  contents; (2) a bare filename ("I edited main.go") IS detected, but a bare
  version/abbreviation is not — "e.g", "U.S", "1.2.3" and "3.14" are
  deliberately not treated as files.
- **Degrade while the turn is active.** `runTurn` holds the session's `as.mu`
  for the entire turn, so `HandleSessionSpeechSummary` checks
  `h.sessions.IsTurnActive(id)` on the non-blocking registry *before* taking
  `as.mu` and returns the empty summary immediately instead of queueing
  behind the turn (`internal/server/handler_speech_summary.go`). The same
  empty summary covers a session with no buildable agent.
- **The summariser's LLM call is cancellable.** `SummarizeForSpeech` issues
  its call through `chatWithOptionalContext`, preferring
  `ChatWithContext(ctx, …)` so the 60s `speechSummaryTimeout` cancels the
  in-flight provider request rather than abandoning the wait; contextless
  clients fall back to `Chat` (`internal/agent/speech_summary.go`).
- **No work lock.** The config PUT releases `h.mu` around
  `config.SaveOcodeSpeechSummary` (cross-process config lock, ~5s bound) and
  re-locks only to update `h.cfg` — `h.mu` is a short-lived map lock, never a
  work lock (`internal/server/agent_session.go:39-46`).
- **Scope stays local.** No server-global lock anywhere in the speech path;
  blocking scope is one request / one session. `POST /api/tts/speak` still
  returns immediately with synthesis in a background goroutine, and §7's one
  playback generation per server process is unchanged.

Regression tests: `TestHandleSessionSpeechSummarySkipsWhileTheTurnIsActive`,
`TestSummarizeForSpeechCancelsTheProviderCallOnTimeout`,
`TestSummarizeForSpeechFallsBackToChatForContextlessClients`, plus the
short-text gate: `TestSummarizeForSpeechSkipsTheLLMForShortPlainProse`,
`TestSummarizeForSpeechStillSummarisesShortTextCarryingArtifacts`,
`TestSpeechTextNeedsRewrite`.
See `gotchas/speech-summary-turn-lock-wait.md` for the full write-up.

### 11. Terminal TTS
- xterm selection right-click → Play selection
- Visible terminal action
- Strip ANSI/control sequences
- Preserve copy/context menu

### 12. Data Flow
- Common frontend SpeechController
- Browser Native: local (browser SpeechSynthesis)
- Local requests → authenticated backend/TTS supervisor → generated seekable audio + progress/state events

### 13. Error Handling
- No silent fallback — errors are always surfaced
- No partial installs — lock release guaranteed
- Retry/status flow; process recovery only per supervisor policy
- Exception: the speech-summary side task is explicitly fail-open (§10.2) — an empty summary or failed request degrades to speaking the full text, never to silence or an error

### 14. Retry Semantics
- Three bounded download attempts with backoff, then surface a manual Retry action
- Manual Retry creates a new setup generation; no unbounded retry and no Browser Native fallback
- If a local engine fails after CPU fallback, user gets a clear error + Retry button; switching to Browser Native requires explicit user action

### 15. Engine-Specific Manifests & Runtime Hooks
- Piper, Kokoro, Fish Audio, Breeze each get pinned verified manifests
- Each engine has engine-specific startup/health/inference adapters
- Each engine has packaging/license/platform validation
- Common supervisor interface only — engine-specific details confined to their respective adapters
- Manifests are verified at install; runtime hooks enforce engine-specific behavior through the common interface

### 16. Playback Control Ownership
- seek/pause/play/timeline controls are frontend Audio/SpeechController operations where possible
- Backend generates/serves seekable local audio and reports progress/state
- Do not present POST /tts/seek as a required backend operation; list conceptual configuration/status/synthesis/cancel/event surfaces instead
- Playback state reported via events; frontend controls drive playback

## Contradiction Self-Review

| Potential Contradiction | Resolution |
|---|---|
| Flat voice selector vs auto hardware fallback | Flat selector means user explicitly chooses; no auto-hardware fallback overrides the choice. Browser Native is the default but user can override. |
| Browser Native control limitations | Browser Native state is tab-local by design; this is documented, not a bug. Local engines share state server-wide. |
| Global vs tab-local playback | Local engines = one active playback per server process (global). Browser Native = tab-local (expected limitation). The UI clearly distinguishes between the two. |
| No Browser Native fallback | Correct — if local engine fails after CPU fallback, user gets a clear error + Retry, not silent fallback to Browser Native. This preserves the selected engine's state. Switching to Browser Native is always a deliberate user action. |
| Global download lock vs selection generations | Lock ownership identity/token tracked; selection generations prevent stale setup from becoming active. Stale setup may finish a verified cache install but must not activate, change selection, or affect playback. |
| mtime-based vs checksum-based artifact validity | Checksum-based artifact validity replaces mtime-only stale removal. Lock ownership identity/token, safe release, crash recovery without deleting a live holder. |
| Partial/corrupt artifact handling | Delete incomplete/corrupt temp/install artifacts before releasing the lock; never mark installed; next automatic or manual Retry starts cleanly; waiters re-check after lock release. |
| Retry bounded vs unbounded | Three bounded download attempts with backoff, then surface a manual Retry action. Manual Retry creates a new setup generation. No unbounded retry and no Browser Native fallback. |
| Engine-specific manifests/runtime hooks | Piper/Kokoro/Fish Audio/Breeze each get pinned verified manifests, engine-specific startup/health/inference adapters, packaging/license/platform validation; common supervisor interface only. |
| Playback control ownership | seek/pause/play/timeline controls are frontend Audio/SpeechController operations where possible; backend generates/serves seekable local audio and reports progress/state. Do not present POST /tts/seek as a required backend operation; list conceptual configuration/status/synthesis/cancel/event surfaces instead. |
| "No silent fallback" (§13) vs fail-open summaries (§10.2) | Not a contradiction: §13 covers engine/install/playback errors, which must always surface. The speech summary is an optional side rewrite — its failure degrades to the full text (visible in what is spoken) and never silences speech. |
| Client-side queue vs §7's one-generation-per-process rule and the §8 global-vs-tab-local split | The queue is a CLIENT-side playback SEQUENCE over items, not shared server state. Each item still takes the single server playback generation in turn, so the global one-active-playback rule is unchanged; Browser Native items queue within the tab as before. |

## Frontend (React) Architecture

### SpeechController
- Unified interface for all TTS modes
- Handles voice selection, playback control, and event forwarding
- Maintains current engine state (Browser Native / Piper / Kokoro / Fish Audio / Breeze)
- Tracks selection generation to prevent stale setups from activating

### Toolbar Component
- Play/pause/toggle button
- Stop button
- Skip buttons (-10s, +10s)
- Timeline seek input
- Elapsed/total time display

### VoiceSelector
- Flat dropdown/radio group with the 5 choices
- Shows setup/readiness status for local engines
- Browser Native remains selectable at all times; if a local engine fails, it remains selected with an error and Retry; switching to Browser Native is a deliberate user action, never an automatic fallback

### PlaybackState
- Tracks: engine type, current text, position, duration, queue state, error state, selection generation
- Queue state is concrete: a ref of items `{id, text, prepared, sessionId, host, summaryEnabled, speakMode, projectTitle, sessionTitle}` (`queue`, `SpeechProvider.tsx:174`), plus `queuedCount` and `nowPlaying` for rendering (`SpeechProvider.tsx:180`), and a single-flight `draining` latch (`SpeechProvider.tsx:179`) so an item's natural end and a user pressing Next in the same tick cannot start two workers
- Emits progress events for UI updates
- On server/process/browser disconnect, cancels/expires active generation; does not resume automatically

## Backend (Go) Architecture

### TTS Supervisor
- Global singleton managing all TTS engine instances
- Handles engine startup, health checks, lifecycle
- Enforces the global download lock and single-flight
- Lock ownership identity/token tracked; safe release; crash recovery without deleting a live holder
- Serves as the authenticated gateway for local TTS requests

### Engine Abstraction
- Each engine (Piper, Kokoro, Fish Audio, Breeze) implements a common interface
- Download/checksum/extract/install lifecycle via EnsureArtifact pattern
- Engine-specific manifests with pinned verification; engine-specific startup/health/inference adapters; packaging/license/platform validation
- GPU detection and preference; CPU fallback path
- Common supervisor interface only — engine-specific details confined to their respective adapters

### API Endpoints
- POST /tts/select — select/activate a TTS engine
- POST /tts/speak — speak text with current engine
- POST /tts/stop — stop current playback
- POST /tts/seek not presented as a required backend operation; list conceptual configuration/status/synthesis/cancel/event surfaces instead
- GET /tts/status — current playback state and engine status
- GET /tts/engines — list available engines and their status
- POST /api/sessions/{id}/speech-summary — rewrite one message into spoken prose; fail-open, returns `{"summary": ""}` immediately while that session's turn is active (§10.2)
- GET/PUT /api/config/ocode/speech-summary — read/write the summary model + gate; the PUT holds `h.mu` only to read and to store the result, never across the config write (§10.2)

## Testing Strategy

### Backend Tests
- Multi-process download serialization & cache recheck
- Atomic install verification
- Lock recovery scenarios with ownership identity/token
- Single-flight under concurrent requests
- Generation GPU/CPU fallback behavior — stale setup cannot activate
- Adoption of newly downloaded engines
- Cache namespace verification: <global-data>/models/tts/<engine>/<voice-or-model-id>/<manifest-version>/<GOOS>-<GOARCH>/
- Late-event ignoring verification
- Speech summary skipped while the turn is active, summarised afterwards (`TestHandleSessionSpeechSummarySkipsWhileTheTurnIsActive`)
- Speech summariser cancelled by its timeout via `ChatWithContext`, with a `Chat` fallback for contextless clients (`TestSummarizeForSpeechCancelsTheProviderCallOnTimeout`, `TestSummarizeForSpeechFallsBackToChatForContextlessClients`)
- Short plain-prose messages skip the summariser LLM entirely (zero model calls), while equally short text carrying a code/diff/path/URL/table/crash artifact is still summarised (`TestSummarizeForSpeechSkipsTheLLMForShortPlainProse`, `TestSummarizeForSpeechStillSummarisesShortTextCarryingArtifacts`, `TestSpeechTextNeedsRewrite`)

### Frontend Tests
- `speechUtils.test.ts` — extractor unit tests for `renderedSpeechText`, `renderedSpeechTexts`, `lastRenderedSpeechText` (whitespace collapse, exclusion attributes, block-level line breaks)
- `MessageBubble.speak.test.tsx` — the Speak button speaks rendered text, not `**`/`#`/backticks/URLs
- `SpeechProvider.speechSummary.test.tsx` — empty/whitespace/failed summary resolves to the original text (fail-open); its "plays only the newest message when two summaries resolve out of order" case was REPLACED by "plays BOTH messages in click order when two speaks are queued", because newest-wins is precisely what the queue removes
- `SpeechProvider.queue.test.tsx` — items play in click order; the badge counts only waiting items; Stop clears the queue; Next promotes the next item and is a no-op on an empty queue; Clear drops the backlog but lets the current item finish; one summary in flight at a time; at-bottom auto-speak queues behind the playing item and ignores the event when not at the bottom; a failed item does not strand the queue; labels and the summariser's session are snapshotted at enqueue and survive a tab switch
- `SpeechToolbar.queue.test.tsx` — `speechItemLabel` joining/skipping halves, the now-playing label and its tooltip, the `+N queued` badge and its accessible name, and the Next / Clear queue controls (including that Clear does not call Stop)
- Voice selector state transitions
- Toolbar playback control
- Error surfacing without fallback
- Browser Native vs local engine state isolation
- Selection generation lifecycle
- Edge cases: empty text, boundary conditions, rapid switching