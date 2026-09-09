---
type: Decision
title: TTS Speech Playback Design Specification
description: User-approved design for TTS speech playback across desktop/web UI, covering model selection, playback semantics, UI, error handling, and testing.
tags:
  - TTS
  - speech
  - design
  - local-model
  - supervisor
timestamp: 2026-09-09T04:41:33Z
resource: docs/superpowers/specs/2026-09-09-tts-speech-playback-design.md
---
# TTS Speech Playback Design Specification

**Status**: Active
**Last Updated**: 2026-09-09

## Overview

This specification defines the TTS (Text-to-Speech) speech playback system for the ocode desktop and web UI. It covers model selection, playback semantics, user interface, error handling, and testing across desktop and web platforms.

## Approved Decisions

### 1. Platform Scope
- Desktop/web shared React UI and Go server; no TUI in this phase.
- The implementation targets desktop (macOS, Linux) and web browsers, with a shared React frontend and Go backend.

### 2. Flat Voice Choices
- Browser Native (default), Piper, Kokoro, Fish Audio, Breeze
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
- New selection/text immediately replaces current playback and clears queue
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

### 9. Settings Playback Modes
- Manual / Speak visible (default; button always present)
- Auto-play when at bottom: autoplay only newly completed assistant messages when chat is at bottom
- Respect browser user-gesture restrictions

### 10. Chat TTS
- Speak any message, selected text, or visible viewport text top-to-bottom without UI chrome
- Reject empty text
- Bound/chunk large text to reasonable sizes

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

### Frontend Tests
- Settings / progress / error handling
- VoiceSelector: Browser Native remains selectable; local engine failure retains selection with Retry
- Chat selection / visible text / chunking
- Terminal context / play / ANSI stripping
- Toolbar replacement / modes / browser bypass
- Playback control ownership: frontend controls drive playback, backend reports state

### Integration Tests
- Same-model-process scenarios
- Different-model-process scenarios
- Cross-process download lock contention with ownership identity/token
- Engine switch mid-playback
- Selection generation prevention of stale setup activation
- Playback state expiration on disconnect

## Summary

This specification provides a complete, active design for TTS speech playback in ocode. It establishes a flat voice choice model, clear upgrade paths from Browser Native to local engines, global download coordination via advisory locks with ownership-safe semantics, and robust error handling with no silent fallbacks. The design is intentionally scoped to desktop/web with no TUI or cloud TTS components in this phase.

Key revisions incorporated from the 2026-09-09 review:
- Browser Native remains selectable at all times; local engine failure retains selection with error+Retry; switching to Browser Native is a deliberate user action
- Global download lock with ownership identity/token, checksum-based artifact validity, cache namespace <global-data>/models/tts/<engine>/<voice-or-model-id>/<manifest-version>/<GOOS>-<GOARCH>/
- Selection generations prevent stale setup from activating; stale setup may finish install but must not activate/change selection/affect playback
- Local playback scope: one active local playback generation per server process, shared across its tabs/sessions; separate processes are not shared
- On disconnect, cancel/expire active generation; do not resume automatically
- Partial/corrupt artifacts deleted before lock release; never marked installed; next Retry starts cleanly
- Retry: three bounded download attempts with backoff, then manual Retry creates new generation; no unbounded retry, no Browser Native fallback
- Engine-specific pinned verified manifests, engine-specific startup/health/inference adapters, packaging/license/platform validation; common supervisor interface only
- Playback control ownership: frontend Audio/SpeechController operations where possible; backend generates/serves seekable audio and reports progress/state; POST /tts/seek not required as backend operation
- All other approved requirements preserved exactly
