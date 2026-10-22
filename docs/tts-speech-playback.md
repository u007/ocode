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

## Kyutai engine runtime

The Kyutai engine (Pocket TTS, pinned `pocket-tts==4.0.0`, manifest `kyutai-4.0.0`) runs in one resident Python worker, `internal/tts/kyutai_worker.py`, managed by `internal/tts/kyutai.go`. The worker loads the model once and caches each voice state, so a synthesis costs only generation. Decision and measurements: `docs/architecture/tts-resident-worker.md`.

- **Install fetches the weights.** The install probe loads the default model and the default voice once, so the pinned weights land in the Hugging Face cache.
- **The worker runs offline** (`HF_HUB_OFFLINE=1`). Without it every worker start waits on the hub, which measured about 80 s per start on a network where the hub connect stalls.
- **One request at a time.** A cancelled request returns at once, but its reply is still read before the next request is sent.
- **Idle unload.** The worker stops after 2 minutes without a request. An engine switch or an uninstall leaves the worker to the same idle timer.
- **Version bump.** Existing `kyutai-3.3.0` installs no longer verify. Reinstall Kyutai from Settings. The accepted license hash does not change, so the license does not need accepting again.
