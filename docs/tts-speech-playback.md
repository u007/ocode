---
type: Concept
title: TTS speech playback
description: 'How TTS speech playback works in ocode — engines, the active playback state, and one-off sample synthesis for installed engines.'
tags: []
timestamp: 2026-10-16T15:04:19Z
---

## TTS samples

A one-off short sample can be synthesised for any installed (ready) engine without affecting active playback state.

- `POST /api/tts/sample` — body `{engine, voice?, text?}`. Synthesises a short sample using the engine's default voice or the chosen `voice`. Only installed engines are accepted; unknown engines, unsafe voices, and text exceeding 300 chars are rejected.
- `GET /api/tts/sample-audio/{id}` — serves the resulting audio. The `{id}` is `sample-<nonce>`; the nonce is validated (alphanumeric, hyphen, underscore — no path traversal) before the file is served.

Sample synthesis uses a dedicated slot and does **not** take over the active playback TTS state.

The Web Settings TTS section shows **Play sample** / **Stop sample** buttons, with a voice dropdown for multi-voice engines.
