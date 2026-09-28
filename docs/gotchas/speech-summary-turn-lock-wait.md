---
type: Gotcha
title: 'Speech summary is fail-open: skip while the turn holds as.mu, cancellable summariser, unlocked config write'
description: 'Speech-summary endpoint is fail-open: skips summarising while the session''s turn holds as.mu (empty summary → web speaks full text), the summariser''s LLM call is cancellable by its 60s timeout, and the config PUT releases h.mu around the cross-process config write.'
tags:
  - gotcha
  - speech
  - tts
  - speech-summary
  - locking
  - as.mu
  - h.mu
  - fail-open
  - timeout
  - server
timestamp: 2026-09-28T12:56:29Z
---
# Speech summary is fail-open: skip while the turn holds `as.mu`, cancellable summariser, unlocked config write

**Status:** Active
**Last Updated:** 2026-09-28

## The Gotcha

`POST /api/sessions/{id}/speech-summary` (and its config sibling
`PUT /api/config/ocode/speech-summary`) sit on the hot path of a busy server.
Three separate locks were being held across slow work, and each one turns a
side task into a stall for unrelated code:

1. **Waiting behind the turn.** `runTurn` does `as.mu.Lock(); defer
   as.mu.Unlock()` (`internal/server/agent_session.go:1036-1038`) and holds it
   for the **entire** turn. The handler's later `as.mu.Lock()` to read
   `as.agent` therefore used to block until the turn finished — the summariser
   only started after the turn ended, with the user waiting the whole time.
2. **Holding `h.mu` across the config write.** `h.mu` is documented as "a
   short-lived map lock, never a work lock"
   (`internal/server/agent_session.go:40-46`). `SaveOcodeSpeechSummary` does a
   cross-process read-modify-write bounded at ~5s on a contended lock file
   (`lockOcodeConfig`, `internal/config/ocodeconfig.go:2269-2301`). Holding
   `h.mu` across it stalls every other session's send, the run-state polls and
   the config endpoints — the original "session doesn't run while another
   session is running" bug class.
3. **A timeout that didn't cancel anything.** `SummarizeForSpeech` used the
   contextless `Chat` (`internal/agent/client.go:769-771`,
   `context.Background()`), so `speechSummaryTimeout` (60s) only *abandoned the
   wait*: the goroutine and the provider request kept running.

## Intended behaviour (as landed)

### 1. Empty summary while the session's turn is active — fail-open

`HandleSessionSpeechSummary` checks `h.sessions.IsTurnActive(id)` **before**
touching `as.mu` (`internal/server/handler_speech_summary.go:113-116`) and
returns `200 {"summary": ""}` immediately when the session is mid-turn. The
guard reads the session registry, which never blocks, so the guard itself
cannot stall.

An **empty summary in a 200 is the documented fail-open signal** — never an
error. The web layer (`resolveSpeechText`,
`web/src/components/Speech/SpeechProvider.tsx`) treats empty/whitespace-only
summaries and request failures identically: it speaks the original full text.
A side task degrading must never silence speech.

The same empty-summary 200 covers "session has no buildable agent"
(`as.agent == nil`), mirroring title generation.

**Pinned by:** `TestHandleSessionSpeechSummarySkipsWhileTheTurnIsActive`
(`internal/server/handler_speech_summary_test.go`) — asserts an empty summary
and **zero** summariser calls while the turn is active, then normal
summarising once the turn ends.

### 2. The summariser call is cancellable by its timeout

`SummarizeForSpeech` now issues its LLM call through
`chatWithOptionalContext(ctx, client, messages)`
(`internal/agent/speech_summary.go`), which prefers `ChatWithContext(ctx, …)`
when the client implements it — the real `*GenericClient` does — so the 60s
timeout actually cancels the in-flight provider request instead of just
walking away from it. Contextless clients and test stubs fall back to `Chat`.

`speechSummaryTimeout` is a package **var** (not a const) precisely so tests
can shorten it and prove cancellation is observed rather than waiting a full
minute.

**Pinned by:**

- `TestSummarizeForSpeechCancelsTheProviderCallOnTimeout` — asserts
  `ChatWithContext` is used, `Chat` is not, and cancellation is observed
  (stable at `-count=20`).
- `TestSummarizeForSpeechFallsBackToChatForContextlessClients` — keeps the
  fallback path honest.

### 3. The config PUT does not hold `h.mu` across the disk write

`HandleSetSpeechSummaryConfig` reads the current `SpeechSummaryModel` /
`SpeechSummaryEnabled` pair under `h.mu`, **releases it**, calls
`config.SaveOcodeSpeechSummary` (serialized by the cross-process config lock,
bounded ~5s), then re-acquires `h.mu` only to update `h.cfg`. The write is
already serialized and made atomic by the config file lock, so `h.mu` buys
nothing there and costs the whole server.

## Invariants that still hold

- **No server-global lock in the speech path.** Blocking scope is one request
  / one session. Other endpoints and other chat sessions are unaffected.
- **`POST /api/tts/speak` returns immediately;** synthesis runs in a
  background goroutine (`internal/tts/supervisor.go` `replaceWithLocked` →
  `go s.runSynth`).
- **One active playback generation per server process,** shared across
  tabs/sessions — a new speak replaces another session's audio. By design, see
  `superpowers/specs/2026-09-09-tts-speech-playback-design.md` §7.
- **Local models with `max_parallel` can still contend** on the cross-process
  slot semaphore (`internal/agent/local_model_limiter.go`); remote providers
  do not.

## General lesson

When a side task (summarise, title, pulse snapshot) touches a session, never
queue it behind the session's turn lock: check turn state on the non-blocking
registry first and degrade to a documented "nothing" signal. Same for `h.mu` —
read the value, release, do the slow work, re-lock only to store the result.
And a timeout on a side LLM call only means something if the call takes a
context.

## Cross-Reference

- `tts-speech-playback.md` → "Spoken summaries (fail-open)" section
- `superpowers/specs/2026-09-09-tts-speech-playback-design.md` §10.2
- `gotchas/speech-rendered-text-extraction.md` (what gets extracted before the
  summariser sees it)
