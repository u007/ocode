---
type: Decision
title: Kyutai TTS Runs in a Resident Worker and Pins Pocket TTS 4.0.0
description: 'Decision: the Kyutai engine keeps the Pocket TTS model and voice states loaded in one child process (JSON-lines over stdio) instead of spawning a process per utterance, runs offline after install, and moves to pocket-tts 4.0.0'
tags:
  - tts
  - kyutai
  - pocket-tts
  - architecture
  - performance
timestamp: 2026-10-21T00:00:00Z
---
# Kyutai TTS Runs in a Resident Worker and Pins Pocket TTS 4.0.0

## The problem

Every utterance started a fresh `python` (`internal/tts/kyutai.go`, before this change). Each start paid four costs:

1. **Imports.** `torch` and `pocket_tts` take about 3 s to import.
2. **Model load.** `TTSModel.load_model()` resolves the pinned weights through the Hugging Face hub. On the network this was measured on, the `hf_hub_download` metadata request stalled in `socket.connect` for about 80 s per start. The same load with `HF_HUB_OFFLINE=1` took 0.16 s.
3. **Voice state.** `get_state_for_audio_prompt` runs on every start.
4. **Generation.** The only part that scales with the text.

The measured per-call cost with the cache warm and offline was 7.7 s for the first sentence and 3.6 s for the same sentence after that (6.3 s of audio). Online, every call paid the 80 s stall.

## The decision

**Keep the model and voice states resident in one worker, run it offline, and pin pocket-tts 4.0.0.**

- `internal/tts/kyutai_worker.py` is embedded and spawned through `tool.StartSupervised`. It loads the model on the first request, caches each voice state, and answers one JSON line per request.
- `internal/tts/kyutai.go` keeps a package singleton worker keyed by its cache directory (which contains the version). A different key replaces the worker. The worker stops after `kyutaiWorkerIdleTimeout` (2 minutes) with no checkout, the same idle rule as the speech-to-text worker (`docs/architecture/stt-resident-worker.md`).
- The install probe (`kyutaiPrefetchProbe`) loads the default model and voice once. That is what makes `HF_HUB_OFFLINE=1` safe for the worker: the weights are already cached, and a missing cache fails the synthesis instead of silently waiting on the network.
- `kyutaiVersion` moves to `kyutai-4.0.0`, with `pocket-tts==4.0.0` in the pinned requirements. The license text is unchanged, so the license hash and the accepted license stay valid.

## Protocol

```
-> {"id":1,"op":"synth","voice":"alba","text":"...","out":"/abs/clip.wav"}
<- {"id":1,"ok":true}
<- {"id":1,"ok":false,"error":"ValueError: ..."}
```

stdout is the protocol channel. The worker dups the real stdout onto a private descriptor and points fd 1 at stderr, as the STT worker does. Stderr is kept in an 8 KB tail buffer.

## Concurrency and cancellation

- **One request in flight.** A one-slot channel is held from the moment a request is written until its reply has been read. Playback and one-off samples share the worker, so they queue.
- **A cancelled request keeps its place in the pipe.** The caller returns `context.Canceled` at once. The slot stays held until the worker's reply is read, so the next request cannot read the stale reply. The worker finishes the stale rendering. Its output file is discarded by the existing playback cleanup (only the newest rendering is kept).
- **A reply-id mismatch marks the worker dead.** The pipes are out of step, so the next checkout starts a fresh worker.

Stopping a worker (idle, or a replacement after a directory change) waits for the supervisor record to become terminal. Otherwise the next worker, which registers under the same stable id, would fail its registration.

## Measurements

Same sentence, about 6.3 s of audio, macOS, CPU:

| | before (a process per call) | after (resident worker) |
|---|---|---|
| online, this network | ~85 s on every call (80 s hub stall, ~3.6 s work) | the stall is gone after install, since the worker runs offline |
| offline, cache warm | 3.6 s on every call (7.7 s the first time) | 1.4 s per call for the same sentence (about 2.6x); 7.1 s for the first utterance, which includes process start |

The Go path ran the real worker in `TestKyutaiWorkerSynthesizesWithInstalledRuntime` (opt-in with `KYUTAI_TTS_VENV`): 7.1 s for the first utterance, 0.6 s for a short follow-up clip (about 1.5 s of audio).

Generation speed was checked for the default model on pocket-tts 3.3.0 and 4.0.0 (about 1.4-1.6 s for the same clip). The 4.0.0 default `english` is `english_2026-09_2`, the re-aligned data release. The 4.0.0 release also normalises dashes and curly apostrophes the models never saw in training, which should matter for prose.

## Not decided here

- **Accuracy is not measured.** The planned ASR round-trip (synthesise, transcribe, compare words) was blocked by a full disk. Listen to the clips before trusting the "more accurate" claim.
- **The 7M English model is about 3x faster to generate** (real-time factor 0.07 against 0.22 for the default), but it is a smaller model. It is not the default. Offer it as an option only after the accuracy comparison.
- **Other engines still spawn a process per utterance** (Kokoro, Piper, MeloTTS, Paradee). They share the import and load cost. Tracked in `TODO.md`.
