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
  every file. The host needs Python >= 3.11 on PATH (or in the usual
  Homebrew / python.org / pyenv locations). Other hosts show an explicit
  unavailable reason.
- **Kokoro, Fish Audio, and Breeze** are shown in Settings with an explicit
  unavailable status until a pinned runtime/model manifest, checksum, output
  protocol, platform matrix, and license review are complete. The UI does not
  silently switch to Browser Native when a local engine is selected or fails.

## Installing Piper

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

## Controls

Assistant messages expose **Speak**. Chat supports speaking the current text
selection or visible message content. The terminal context menu preserves Copy
and adds **Play selection** and **Speak visible**; terminal control bytes are
removed before speech. New speech replaces current speech and clears queued
chunks.

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
