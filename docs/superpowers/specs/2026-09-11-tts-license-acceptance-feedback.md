# Speech Engine License Acceptance Feedback

**Date:** 2026-09-11
**Status:** Approved for implementation

## Problem

The speech-engine license pipeline currently reaches `POST /api/tts/enable` with HTTP 200, but the settings UI does not expose the resulting install state. The local engines remain unavailable because their verified runtime/model artifacts are not implemented, so the existing button misleadingly runs a complete placeholder pipeline and attempts to enable an unavailable engine.

## Design

Make license acceptance an explicit, observable action rather than an all-in-one fake pipeline:

- The per-engine action calls only `POST /api/tts/license`.
- The UI loads and refreshes `/api/tts/state` after the request and renders the engine's install state inline.
- Successful acceptance is reported as `License accepted` even while the engine remains unavailable.
- The UI explains that pin/download/install/enable are blocked until a verified runtime and model manifest exists.
- Remove the misleading automatic calls to pin, download, install, and enable, and do not use a success alert that claims pipeline completion.
- Keep the backend acceptance endpoint and state machine behavior intact; this fix addresses the visibility and false-success behavior.

## Error handling

License request failures remain visible inline on the affected engine card. Loading and submission are disabled only for that card while its request is active. State-fetch failures use the existing settings error presentation.

## Validation

- Frontend component tests verify the license request is sent, the returned install state is rendered, and the follow-up pipeline endpoints are not called.
- The web typecheck and build must pass.
- Existing Go TTS tests and focused server tests must pass; no backend behavior is changed.

## Scope boundary

This does not make Piper, Kokoro, Fish Audio, or Breeze runnable. Enabling those engines requires the separate verified runtime/model artifact work described by the TTS design and feasibility documents.
