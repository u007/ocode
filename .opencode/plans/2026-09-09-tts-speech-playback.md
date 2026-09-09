# Plan: Implement Desktop/Web TTS Speech Playback

**Created:** 2026-09-09
**Status:** Phase 0 complete; Browser Native and safe local-engine scaffolding implemented; local engine manifests blocked pending feasibility/licensing approval
**Design:** `docs/superpowers/specs/2026-09-09-tts-speech-playback-design.md`

## Goal

Add desktop/web speech playback for chat messages and terminal text with Browser Native speech plus fully managed server-side local TTS engines. Preserve the approved no-silent-fallback, replacement, autoplay, and cross-process download-safety policies.

## Constraints and invariants

- Shared React SPA powers web and desktop; no TUI changes.
- Voice selector: Browser Native (default), Piper, Kokoro, Fish Audio, Breeze.
- Browser Native runs only in the browser and bypasses the server/model manager.
- Local engine selection immediately prepares the model without `/localmodel` commands or manual daemon setup.
- Local setup tries GPU, then CPU. It never silently switches to Browser Native; failures remain visible with Retry.
- A global ownership-safe advisory lock serializes all ocode model downloads. The lock covers cache check, download, verification, extraction, metadata, and atomic install.
- TTS artifacts use a separate cache namespace keyed by engine, voice/model id, manifest version, OS, and architecture.
- Selection generations prevent stale setup from changing the active engine or playback. Corrupt/partial artifacts never become installed.
- A new speech request replaces current speech and clears the queue. One local playback generation is active per running ocode server process; Browser Native is browser-tab-local.
- Existing unrelated working-tree changes must not be reverted or included.

## Phase 0 — Feasibility and manifest decisions

1. Verify packaging, licensing, model formats, and platform support for Piper, Kokoro, Fish Audio, and Breeze.
2. Define pinned TTS manifest records for each supported engine/platform, including runtime artifacts, voice/model artifacts, SHA-256 checksums, launch arguments, health checks, synthesis protocol, and GPU/CPU capabilities.
3. Decide which engines have a real v1 manifest on each supported platform; unsupported combinations must be disabled with an explicit status rather than presented as ready.
4. Confirm the audio format is seekable and supports browser range playback.

**Exit condition:** Every selectable local entry has a pinned, verifiable manifest or an explicit unavailable state.

## Phase 1 — Shared backend TTS foundation

### New bounded package

Create an `internal/tts` package with focused responsibilities:

- `manifest.go` — engine/voice manifests and platform filtering.
- `cache.go` — TTS namespace resolution and checksum metadata.
- `download_lock.go` — global cross-process ownership-safe advisory lock.
- `artifacts.go` — wrapper around existing `/localmodel` artifact primitives; temp files, verification, cleanup, atomic install.
- `supervisor.go` — per-server TTS supervisor and engine lifecycle.
- `engine.go` — common engine interface plus engine-specific adapters.
- `playback.go` — generation IDs, replacement/cancel semantics, disconnect expiry.
- `audio.go` — seekable output storage/streaming and range-safe serving.

Reuse existing discovery/process infrastructure where safe, but do not reuse the chat/completion protocol as a TTS protocol.

### Lock and cache implementation

1. Use the global data directory for the lock and TTS cache.
2. Hold the global download lock across cache check → download → checksum → extraction → metadata → atomic rename.
3. Store lock owner identity/token and make stale recovery ownership-safe; never use bare mtime `Stat` followed by `Remove`.
4. Re-check cache after waiting for another process.
5. Delete incomplete/corrupt temporary and install artifacts before releasing the lock.
6. Implement in-process keyed single-flight for the same engine/model.
7. Use bounded download attempts with backoff, then expose manual Retry.
8. Add multi-process tests for same-model and different-model contention.

### Supervisor lifecycle

1. Select/prepare a model using a monotonically increasing generation.
2. Start or adopt a healthy process under a per-engine/model startup lock.
3. Try GPU first and retry using CPU only when GPU setup/inference fails.
4. Keep stale generations from activating or publishing playback events.
5. Supervise process death and parent death; cancel active generation on disconnect.
6. Re-adopt or restart the engine on a later request, without automatic playback resume.
7. Broadcast readiness, progress, hardware, error, and playback-generation events to connected clients.

## Phase 2 — Configuration and server API

1. Add persisted TTS configuration to `internal/config/ocodeconfig.go`:
   - selected engine
   - selected voice/model id
   - playback mode (manual or at-bottom autoplay)
   - optional voice parameters supported by the selected manifest
2. Add server-owned TTS supervisor construction and shutdown wiring without holding `Handler.mu` across downloads, process startup, synthesis, or audio work.
3. Add authenticated endpoints for:
   - configuration read/write
   - engine catalog and readiness status
   - immediate engine preparation with progress
   - synthesize/start/replace text
   - cancel/stop
   - status/progress/error event subscription
   - seekable audio retrieval with range support
4. Keep pause, resume, seek, timeline, and skip in the frontend audio/speech controller where possible; backend owns synthesis, cancellation, and state publication.
5. Scope local playback state to one server process. Do not imply separate ocode server processes share playback state.
6. Add API types and client wrappers in `web/src/api/types.ts` and `web/src/api/client.ts`.

## Phase 3 — Frontend speech controller and toolbar

1. Add a shared `SpeechController` abstraction/context that supports:
   - Browser `SpeechSynthesis`
   - server-generated seekable local audio
   - play/pause/stop/replace
   - ±10 seconds
   - timeline seek
   - generation-aware late-event rejection
2. Add the shared playback toolbar with active source, engine, status, elapsed/total, and error/retry state.
3. Implement Browser Native estimated text-offset seek/skip and clearly represent unavailable/estimated duration.
4. Ensure browser user-gesture restrictions produce a visible Play action instead of a silent failure.
5. Keep Browser Native selectable at all times; a local error must not mutate the selection.

## Phase 4 — Settings UI

1. Add a Voice/TTS section to `web/src/components/Settings/SettingsPanel.tsx`.
2. Add a dedicated settings form for the flat selector:
   - Browser Native (default)
   - Piper
   - Kokoro
   - Fish Audio
   - Breeze
3. Selecting a local engine immediately starts preparation and displays waiting, download progress, verification, starting, ready/hardware, or error/retry states.
4. Add playback mode selection:
   - Manual / Speak visible (default)
   - Auto-play when at bottom
5. Do not add a manual `/localmodel` setup step or separate daemon control.
6. Disable only unsupported engines/platforms, not Browser Native after local selection.

## Phase 5 — Chat integration

1. Add message-level Speak actions to `MessageBubble` and selection-aware actions.
2. Add a chat selection action that speaks selected text and replaces current playback.
3. Implement Speak visible from the chat viewport, top-to-bottom, excluding UI chrome and empty content.
4. Bound/chunk large visible content while preserving replacement generation semantics.
5. Track whether the chat is at the bottom and implement at-bottom autoplay only for newly completed assistant messages.
6. Prevent duplicate autoplay from rerenders, reconnects, or historical message loading.

## Phase 6 — Terminal integration

1. Extend `web/src/components/Terminal/TerminalPanel.tsx` or its terminal context-menu layer.
2. Preserve existing copy/context actions.
3. Add right-click Play selection using xterm's current selection.
4. Add Speak visible using active viewport buffer rows, stripping ANSI/control sequences.
5. Ensure a terminal request replaces chat/local playback according to the shared server generation policy.
6. Test empty selection, multiline selection, ANSI text, scroll position, and right-click behavior.

## Phase 7 — Tests and validation

### Backend

- Lock serialization across processes, including different models.
- Cache re-check after lock wait.
- Ownership-safe crash recovery.
- Checksum mismatch and partial-file cleanup.
- Atomic install and metadata validity.
- Single-flight same-model setup.
- Selection generation stale-completion rejection.
- GPU-to-CPU fallback and no Browser Native fallback.
- Process adoption, restart, disconnect expiry, and no automatic resume.
- Audio range/seek behavior.

### Frontend

- Browser Native bypasses the backend.
- Settings selection immediately begins local preparation.
- Browser Native remains selectable after local selection/failure.
- Progress, ready, GPU/CPU, and error/retry states.
- Chat message, selection, visible text, and at-bottom autoplay.
- Terminal selection, right-click action, visible buffer, ANSI stripping.
- Replacement clears queue and rejects late events.
- Toolbar controls and estimated Browser Native seek.
- User-gesture blocked autoplay behavior.

### Integration/manual

- Two ocode processes request the same model.
- Two ocode processes request different models.
- Two tabs share local playback but Browser Native remains tab-local.
- Model switch during download and during playback.
- Server/process/browser disconnect and subsequent restart/adoption.
- Desktop embedded webview uses the same API/UI as web.

Run focused Go tests, frontend typecheck/tests/build, then relevant full-suite checks. Use `gofmt`, `go vet`, and LSP diagnostics on changed Go files. Review the final diff and do not include unrelated working-tree changes.

## Documentation and rollout

1. Update user-facing Settings/help documentation with model statuses, immediate download behavior, GPU/CPU fallback, error/no-fallback policy, and Browser Native limitations.
2. Document the TTS cache namespace, lock invariant, engine manifest requirements, and supported platform matrix.
3. Keep unsupported engine/platform combinations visible as unavailable rather than pretending they are ready.
4. Land Browser Native and the shared frontend controls independently from local engines where practical.
5. Enable local engines only after their manifests, licensing, checksums, and focused integration tests are complete.

## Risks

- Engine packaging and licensing may prevent all four local engines from being available on every platform.
- Browser SpeechSynthesis has inconsistent duration/boundary events, so seek is approximate.
- A server-global local playback generation means tabs can replace one another; the UI must show the active source.
- Download locking must not regress existing `/localmodel` behavior or the known stale-slot race class.
- Existing working-tree changes are unrelated; implementation must be isolated to feature files and tests.
