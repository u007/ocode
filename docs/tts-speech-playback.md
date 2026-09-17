---
type: Guide
title: Speech playback
description: Speech playback — user-facing doc covering engine availability, installation, playback controls, and DOM-based rendered-text extraction
tags:
  - speech
  - tts
  - playback
  - web
  - desktop
  - user-facing
  - DOM-extraction
timestamp: 2026-09-16T13:04:47Z
---
# Speech playback

Speech playback is shared by the web UI and the desktop app because desktop
embeds the same React application.

## Current engine availability

- **Browser Native** is the default. It uses the browser tab's
  `SpeechSynthesis` API and does not contact the ocode server for audio.
- **Piper** is installable on darwin/arm64, darwin/amd64, linux/amd64,
  linux/arm64 and windows/amd64. Its manifest (`internal/tts/manifest.go`)
  pins the `piper-tts==1.8.0` Python runtime (GPL-3.0-or-later,
  OHF-Voice/piper1-gpl) plus `onnxruntime` per host, and the CC0-dataset
  `en_US-joe-medium` voice from rhasspy/piper-voices with SHA-256 + size for
  every file. Hosts using onnxruntime 1.30.0 need Python >= 3.11; Intel macOS
  uses the 1.22.1 universal2 runtime and accepts Python >= 3.10. Other hosts
  show an explicit unavailable reason.
- **Kokoro** is installable on the same supported host matrix. Its manifest
  pins `kokoro-onnx==0.6.1`, a host-compatible onnxruntime release, the
  `kokoro-v1.0.onnx` model, and `voices-v1.0.bin`, with Apache-2.0/MIT license
  disclosures and SHA-256 checksums for the model artifacts. Hosts using
  onnxruntime 1.30.0 need Python >= 3.11; Intel macOS uses onnxruntime 1.22.0
  and accepts Python >= 3.10.
- **Fish Audio and Breeze** remain unavailable until their runtime, artifact,
  output protocol, platform matrix, and license review are complete. The UI
  does not silently switch to Browser Native when a local engine is selected or
  fails.

## Installing a local engine

Settings > Speech playback walks the per-engine state machine
(`not-accepted → license-accepted → pinned → downloading → installed →
enabled`):

1. **Accept License** — `POST /api/tts/license` records acceptance.
2. **Install** — `POST /api/tts/pin` (must match the manifest version) then
   `POST /api/tts/download`, which starts one background job under the global
   download lock: verify/download voice files atomically, create
   `<data>/models/tts/piper/<voice>/<version>/<host>/venv`, `pip install` the
   pinned requirements, verify the import, and only then write
   `installed.json`. Downloads retry three times with backoff; any failure
   moves the engine to `failed` with the error shown inline and a Retry
   Install button. `GET /api/tts/state` returns progress and step while the
   job runs.
3. **Enable** — `POST /api/tts/enable` selects the engine, persists it to
   `ocodeconfig.json`, and sets the manifest voice.

State persists in `<data>/models/tts/install-state.json`. At startup the
cache is re-verified: a complete cache is recognized as installed and a
missing/corrupt cache demotes an installed/enabled record to `failed`.

Local synthesis: `POST /api/tts/speak` returns `playback.status =
"synthesizing"` with an `audio_id`; the client polls `GET /api/tts/status`
until `ready` (or `error`), then fetches `GET /api/tts/audio/{audio_id}`
(WAV, 22.05 kHz mono) and plays it through an `<audio>` element. Each run is
`python -m piper` spawned via the shared process supervisor
  (`ProcessKindTTS`) with a 10-minute timeout; stop/replace/engine-switch
cancel it. Only the active playback's audio id is served.

## Rendered-text extraction (DOM, not markdown source)

Speech reads the message's **rendered** text — what is on screen, extracted
from the DOM — never the raw markdown source string. The extraction helpers
in `web/src/components/Speech/speechUtils.ts` implement this:

| Helper | Purpose |
|---|---|
| `renderedSpeechText(root)` | Walk a rendered DOM subtree; collapse whitespace; skip `[data-speech-exclude]`, `aria-hidden`, script/style/svg; insert line breaks at block-level tags. Used by per-message Speak button. |
| `renderedSpeechTexts(root)` | Collect all `[data-speech-content]` blocks in DOM order into a single string. Used by "Speak visible" viewport button. |
| `lastRenderedSpeechText(root)` | Return the last `[data-speech-content]` block. Used by auto-speak at-bottom. |

Because the extractor walks the rendered DOM rather than reading markdown
source, heading hashes, `**` emphasis markers, backticks and link URLs are
not read aloud. Adjacent paragraphs, list items and table cells are
separated by block-level breaks rather than run together. The Skip rules:

- `[data-speech-exclude]` children are omitted (e.g. the Speak button
  itself).
- `aria-hidden` elements, `<script>`, `<style>`, and `<svg>` are skipped.

Component contract: every markdown-rendered message block must set
`data-speech-content` on its subtree. `ThinkingBlock` reasoning and terminal
selections are plain text and pass through unchanged — no DOM extraction
needed.

For the full rationale (formatting markers, block-level structure, why
markdown source fails) see `gotchas/speech-rendered-text-extraction.md` and
`superpowers/specs/2026-09-09-tts-speech-playback-design.md` §10.1.

## Controls

Assistant messages expose **Speak**. Chat supports speaking the current text
selection or visible message content. The terminal context menu preserves Copy
and adds **Play selection** and **Speak visible**; terminal control bytes are
removed before speech. New speech replaces current speech and clears
queued chunks.

Server-side stop, selection, and synthesis mutations are serialized by the
frontend and supervisor. A late stop cannot cancel a newer speech request, and
changing engines invalidates playback from the previous selection.

Settings provides **Manual / Speak** (default) and **Auto-play when at bottom**.
Auto-play applies only to newly completed assistant messages while the chat is
at the bottom. Browser Native pause, resume, stop, and replay are local to the
browser tab; duration and seek are intentionally best-effort because browser
speech implementations do not expose a reliable audio timeline.

## Local-engine rollout requirements (remaining engines)

Local artifacts belong under the TTS cache namespace keyed by engine, voice,
manifest version, and host. Downloads must use the global ownership-safe lock,
verify SHA-256 before an atomic install, and use a selection generation so stale
setup cannot become active. See
`docs/superpowers/plans/2026-09-09-tts-phase0-feasibility.md` for the current
engine blockers and `docs/superpowers/specs/2026-09-09-tts-speech-playback-design.md`
for the approved rollout behavior.