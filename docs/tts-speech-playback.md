# Speech playback

Speech playback is shared by the web UI and the desktop app because desktop
embeds the same React application.

## Current engine availability

- **Browser Native** is the default. It uses the browser tab's
  `SpeechSynthesis` API and does not contact the ocode server for audio.
- **Piper, Kokoro, Fish Audio, and Breeze** are shown in Settings with an
  explicit unavailable status until a pinned runtime/model manifest, checksum,
  output protocol, platform matrix, and license review are complete. The UI
  does not silently switch to Browser Native when a local engine is selected or
  fails.

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

## Local-engine rollout requirements

Local artifacts belong under the TTS cache namespace keyed by engine, voice,
manifest version, and host. Downloads must use the global ownership-safe lock,
verify SHA-256 before an atomic install, and use a selection generation so stale
setup cannot become active. See
`docs/superpowers/plans/2026-09-09-tts-phase0-feasibility.md` for the current
engine blockers and `docs/superpowers/specs/2026-09-09-tts-speech-playback-design.md`
for the approved rollout behavior.
