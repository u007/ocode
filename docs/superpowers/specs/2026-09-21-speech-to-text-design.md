---
type: Design
title: Local Speech-to-Text (In-App Dictation) — Design
description: 'Design spec for adding local, offline speech-to-text dictation to ocode: mic button in chat composer, Settings surface for engine/model selection, reusing internal/tts machinery.'
timestamp: 2026-09-20T17:44:08Z
tags:
  - speech-to-text
  - dictation
  - stt
  - superpowers
  - design
resource: superpowers/specs/2026-09-21-speech-to-text-design.md
---
# Local Speech-to-Text (In-App Dictation) — Design

**Type:** Design  
**Description:** Design spec for adding local, offline speech-to-text dictation to ocode: mic button in chat composer, Settings surface for engine/model selection, reusing internal/tts machinery.  
**Tags:** speech-to-text, dictation, stt, superpowers, design  

---

# Local Speech-to-Text (In-App Dictation) — Design

**Date:** 2026-09-21
**Status:** Design approved; not yet implemented

## Context

ocode ships a mature local-model runtime for text-to-speech (`internal/tts/`, 3,084 LOC across 15 files) but has **no speech input**. The `web/src/components/Speech/` module is TTS playback only (`SpeechProvider`/`SpeechToolbar`/`speechUtils`); `getUserMedia` and `SpeechRecognition` appear nowhere in `web/src`.

This spec adds **local, offline speech-to-text for in-app dictation**: a mic button in the chat composer that transcribes speech into the prompt draft, plus a Settings surface to choose which speech-recognition engine/model is used.

## Goals

1. A small, local, offline STT model that runs on CPU on macOS, Windows and Linux.
2. A Settings UI (mirroring the existing TTS form) that lets the user select the STT engine/model, download it, and set language options.
3. Dictation into the ocode chat input, working in both the browser SPA and the Wails v3 desktop shell.
4. Reuse the proven `internal/tts` machinery (pinned manifests, per-manifest Python venv, license-accept gate, process supervision, `ORT_DISABLE_TELEMETRY` hardening) rather than inventing a parallel stack.

## Non-goals (v1)

- System-wide push-to-talk hotkey (Handy parity) — deferred to M3.
- Capturing audio while ocode is unfocused.
- Pasting transcripts into *other* applications (would require macOS Accessibility/CGEventPost permissions).
- Streaming/partial transcripts (deferred to M2).
- Voice input in the TUI.

## Decisions

| # | Decision | Rationale |
|---|----------|-----------|
| D1 | **Runtime = `sherpa-onnx` inside a pinned per-manifest Python venv.** | Mirrors the existing piper/kokoro engine mechanism exactly. One pip package covers canary, parakeet, whisper, SenseVoice, Moonshine, Silero VAD and streaming zipformer. Prebuilt CPU wheels bundle onnxruntime, so there are no system dependencies. Avoids cgo — the repo's only cgo file is `cmd/ocode-desktop/native_darwin.go`, and the Makefile cross-builds Windows + macOS + Linux, so adding cgo to the server build is expensive. |
| D2 | **Rejected: sherpa-onnx Go bindings (cgo).** | Would break the multi-platform cross-compilation pipeline. |
| D3 | **Rejected: whisper.cpp sidecar.** | Cannot run canary or parakeet. |
| D4 | **Rejected: `onnx-asr` venv.** | Viable but a narrower catalog with no VAD/streaming story. |
| D5 | **A warm, long-lived worker process — NOT one process per utterance.** | Model load is ~0.5–2 s. A cold process per utterance would make dictation feel broken. This is the one place STT structurally diverges from TTS. |
| D6 | **Capture in the webview (`getUserMedia` + AudioWorklet) as the single capture path.** | Works in the browser SPA and in the Wails webview on all three OSes, so there is one code path. AudioWorklet yields raw 16 kHz mono Float32 PCM, so the Go server needs **no audio decoder** (no ffmpeg, no cgo codec). |
| D7 | **Transcripts are inserted into the draft at the cursor; never auto-sent.** | Makes mis-recognition cheap to correct and keeps the user's send action intact. |
| D8 | **Artifact URLs pin the Hugging Face revision commit SHA, not `resolve/main`.** | `main` is mutable; a SHA256 recorded against a moving URL is not reproducible. Both the revision SHA and the file SHA-256 are pinned. |
| D9 | **v1 catalog = canary-180m-flash (default) + parakeet-tdt-0.6b-v3 + whisper-base + browser-native.** | Small multilingual default, best-in-class English dictation, and a long-tail language fallback, all on one runtime. |

## Model facts (verified)

- **`nvidia/canary-180m-flash`**: 180M-param FastConformer encoder + transformer decoder. 4 languages (English, German, French, Spanish) plus speech **translation** en↔de/fr/es, with punctuation and capitalisation. License **CC-BY-4.0**. It is **non-streaming (offline only)** — so it suits push-to-talk / VAD-segmented dictation, not live captions.
- `sherpa-onnx` supports it as `sherpa_onnx.OfflineRecognizer.from_nemo_canary(encoder=, decoder=, tokens=)`; source/target language are set via `OfflineCanaryModelConfig(src_lang=, tgt_lang=)`.
- Individual model files are hosted on Hugging Face at `csukuangfj/sherpa-onnx-nemo-canary-180m-flash-en-es-de-fr-int8`: `encoder.int8.onnx` (133 MB), `decoder.int8.onnx` (74.4 MB), `tokens.txt` (53.6 kB) — ~207 MB total. Because they are individually URL-addressable, they fit the existing `Artifact{Name, URL, SHA256, Size}` struct with **no tar/bzip2 extraction support needed**.
- The same runtime serves **parakeet-tdt-0.6b-v3** (`csukuangfj/sherpa-onnx-nemo-parakeet-tdt-0.6b-v3-int8`), **whisper** (`sherpa-onnx-whisper-base`), SenseVoice, Moonshine, **Silero VAD**, and streaming zipformer models.
- The Handy app (Tauri) ships **whisper.cpp + parakeet ONNX** — it does **not** ship canary. "Handy parity" therefore means the *UX* (push-to-talk), and the model overlap is parakeet/whisper.

## Architecture / data flow

```
ChatInput mic button
  → useDictation().start()            // web/src/components/Dictation/DictationProvider.tsx
  → getUserMedia({audio:{channelCount:1, echoCancellation, noiseSuppression}})
  → AudioContext({sampleRate:16000}) → AudioWorklet → Float32 frames
  → stop → Int16 PCM
  → POST /api/stt/transcribe          // raw octet-stream; ?engine=&language=&target_language=
  → internal/server/handler_stt.go → stt.Supervisor.Transcribe(ctx, pcm)
  → warm Python worker (sherpa-onnx)  → {text, engine, language, duration_ms}
  → inserted into the ChatInput draft at the cursor
```

## Package layout

```
internal/stt/
  manifest.go     Engine, Artifact, PythonRuntime, catalog (canary/parakeet/whisper)
  installer.go    not-accepted → license-accepted → pinned → downloading → installed
  cache.go        <datadir>/models/stt/<engine>/ + per-manifest venv + venvPython
  download.go     verified download (size check first, then SHA256)
  worker.go       warm worker lifecycle + line-delimited JSON protocol
  supervisor.go   Catalog / Select / Prepare / Transcribe / InstallStates
  stt_worker.py   go:embed'd sherpa-onnx loop
internal/server/handler_stt.go (+ handler_stt_test.go)
web/src/components/Dictation/DictationProvider.tsx
web/src/components/Dictation/dictationUtils.ts
web/src/components/Dictation/sttWorklet.ts
web/src/components/Settings/STTForm.tsx (+ test)
```

Routes are registered beside the existing TTS block at `internal/server/server.go:356-369`.

## Engine catalog

| id | runtime call | artifacts | languages |
|---|---|---|---|
| `browser-native` | Web Speech API | none | many (Chrome/Edge); **unavailable in the Wails webview** — modeled as `availability: "unavailable"`, mirroring TTS `browser_only` |
| `canary-180m-flash` | `OfflineRecognizer.from_nemo_canary` | encoder + decoder int8 + tokens ≈ 207 MB | en/de/fr/es + translation |
| `parakeet-tdt-0.6b-v3` | NeMo TDT | encoder/decoder/joiner int8 + tokens | 25 |
| `whisper-base` | Whisper | encoder/decoder int8 + tokens | 99 |

Runtime pinning mirrors `piperRequirements(onnxruntime)` in `internal/tts/manifest.go`: exact `name==version` requirements plus a per-host-triplet `MinPython`/`MaxPython` range (including the existing precedent of pinning an older onnxruntime on `darwin/amd64`).

## Warm worker protocol

- One worker process per engine, started lazily on first transcription, registered with `tool.ProcessSupervisor.Register` (`internal/tool/process_supervisor.go:125`).
- Line-delimited JSON on stdin/stdout: request `{"id":…, "pcm_b64":…, "sample_rate":16000, "language":…, "target_language":…, "pnc":true}`, response `{"id":…, "text":…, "error":…}`.
- Idle eviction after 5–10 minutes; worker failure surfaces as a `status.error` so the UI can retry.
- The child inherits the TTS hardening from `internal/tts/synth_env.go`: `cmd.Dir` pinned to the engine cache dir and `ORT_DISABLE_TELEMETRY=1` dropped-and-reset, because sherpa-onnx also bundles onnxruntime and would otherwise write a `:memory:.ses` telemetry sidecar into the process cwd.

## HTTP API

| Method | Route |
|---|---|
| GET | `/api/stt/engines` |
| GET | `/api/stt/status` |
| GET | `/api/stt/state` |
| POST | `/api/stt/select` |
| POST | `/api/stt/transcribe` |
| POST | `/api/stt/license` |
| POST | `/api/stt/pin` |
| POST | `/api/stt/download` |
| POST | `/api/stt/install` |
| POST | `/api/stt/enable` |

The `engine`, `language`, and `target_language` query parameters default to the persisted STT config and are optional per-request overrides only; the request body is raw PCM (octet-stream), with no JSON envelope.

## Config

`STTConfig{Engine, Language, TargetLanguage, UsePnC, SilenceMs}`, persisted under the `"stt"` key in `OcodeConfig` next to `TTS` (`internal/config/ocodeconfig.go:805`), with a `SaveOcodeSTTConfig` helper mirroring `SaveOcodeTTSConfig` (`:1828`) and the same legacy-key handling as `"tts"` (`:1203`, `:1219`).

## Web UI

- `STTForm.tsx` mirrors `TTSForm.tsx`: engine cards with availability badges, license acceptance (the CC-BY-4.0 gate), 1 s install-progress polling, per-engine errors, plus language and target-language selectors.
- `SettingsPanel.tsx`: add `{ id: "stt", label: "Speech input" }` to `OCODE_GROUPS` and a `case "stt"` to `renderGroup`.
- `ChatInput.tsx`: a mic button in the composer row with recording state (timer, input level, stop/cancel); the transcript is written into the existing `input`/`setInput` state at the cursor.

## Cross-platform capture requirements

| OS | Requirement | Status |
|---|---|---|
| macOS | `NSMicrophoneUsageDescription` in Info.plist + `com.apple.security.device.audio-input` entitlement | **Missing today** — `scripts/bundle-macos.sh` generates a minimal Info.plist (name/id/icon/LSUIElement). Also the Wails `Permissions` map is **ignored on macOS** (wails#6067), so the TCC prompt is the only gate. Must be tested inside the `.app`, not the bare dev binary. |
| Windows | WebView2 `PermissionRequested`; Wails honours `Permissions` (wails#5567) | Works; auto-allow only ocode's own origin. |
| Linux | WebKitGTK permission handler; Wails honours `Permissions` (wails#5552) | Works under Wails; the weakest platform for raw-browser use. |

Wails v3 does have a system-wide global-shortcut API, so M3 push-to-talk needs no native audio code — but the hotkey should **focus the window and capture in-view** rather than capture while hidden, because macOS throttles WKWebView JS when the window is hidden and the webview is the only capture path.

## Phasing

- **M1** — `internal/stt` (manifest/installer/cache/download/worker/supervisor) + `/api/stt/*` + `STTForm.tsx` + mic button in `ChatInput.tsx` + catalog metadata for all four engines, but only canary-180m-flash installable and validated end-to-end in M1 (parakeet and whisper installable in M2) + `POST /api/stt/transcribe` + macOS/Windows/Linux permission plumbing.
- **M2** — `DictationProvider` context, streaming WebSocket + Silero VAD partials, catalog expansion (parakeet, whisper), language/target-language UI, config persistence.
- **M3** — desktop push-to-talk hotkey, model preload/warm-on-start, optional TUI voice input.

## Risks

1. macOS entitlement/Info.plist work — mic silently denied in the `.app` vs the bare dev binary.
2. Linux WebKitGTK `getUserMedia` reliability.
3. Host `python3` availability and wheel coverage on newer Python versions (TTS already accepts this constraint; pin `MinPython`/`MaxPython` per host as it does).
4. User expectation mismatch: canary is offline-only, so no live captions — mitigate with push-to-talk plus silence auto-stop.
5. Model download size and CC-BY-4.0 attribution — the existing license-accept flow covers this.
6. `ChatInput.tsx` has no mic affordance today; the recording state must not interfere with the existing submit/send logic.

## Verification plan

- Unit tests: manifest/runtime selection per host triplet, install state machine transitions, download size + SHA256 verification, Float32→Int16 and PCM→WAV conversion, cursor insertion helpers.
- Protocol test: a fake worker process exercising the JSON line protocol, plus worker crash/idle-eviction paths.
- Handler tests: each `/api/stt/*` route, following `handler_tts_test.go`.
- Web tests: `STTForm`, `DictationProvider` (worklet stub + mocked getUserMedia), `SettingsPanel` group wiring.
- End-to-end: decode the canary model's own bundled `test_wavs/en.wav` through the real worker and assert the known transcript.

## Implementation-time verifications (unresolved knowledge gaps)

These are **not guessed** in this spec and must be confirmed before or during implementation:

1. The exact `sherpa-onnx` wheel version to pin, and wheel availability for each host triplet and Python range.
2. The HF repo file lists and SHA-256 values for **parakeet-tdt-0.6b-v3-int8** and **whisper-base** (canary is confirmed; the other two are likely but unpinned). Canary artifact sizes: encoder 133 MB, decoder 74.4 MB, tokens 53.6 kB.
3. Whether the Web Speech API is usable at all inside the Wails WebKit webview for `browser-native` (expected: no → `availability: "unavailable"`).
4. Expected CPU latency (real-time factor) per engine on each platform, to set the idle-eviction window and UI timeouts.