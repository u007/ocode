---
type: Decision
title: Local Speech-to-Text Runs in a Resident Worker
description: 'Decision: the local STT engine keeps the model loaded in one child process (JSON-lines over stdio) instead of spawning a process per utterance, and pins the onnxruntime CPU provider'
tags:
  - stt
  - speech-to-text
  - architecture
  - performance
  - onnxruntime
timestamp: 2026-10-20T00:00:00Z
---
# Local Speech-to-Text Runs in a Resident Worker

## The problem

Local dictation (the `parakeet-tdt-0.6b-v3` model, run through `onnx-asr`) was
2-3x slower than the same model in another app. Two independent causes made
every utterance expensive.

**1. The model was reloaded for every utterance.** `internal/stt/transcribe.go`
spawned a fresh `python3` per call, and `internal/stt/transcribe_onnx.py` called
`onnx_asr.load_model(...)` inside that process. Loading the model costs about a
second. The live partial preview (a decode every three seconds, see
`voicePartialInterval` in `internal/tui/voice.go` and `VOICE_PARTIAL_INTERVAL_MS`
in `web/src/components/Chat/useVoiceRecorder.ts`) multiplied that: a
twelve-second dictation triggered four previews plus a final transcription, so
five model loads.

**2. CoreML charged a ~12 s compile per process.** `onnx_asr.load_model()`
defaults to *every* available onnxruntime provider when `providers` is not
given, so on Apple Silicon it selected `CoreMLExecutionProvider` first. CoreML
compiles the model to the Apple Neural Engine format on every fresh process,
and onnx-asr's CoreML path only supports part of the graph anyway (16 of 42
nodes on the encoder), so the "GPU" mostly bought a compile bill. Passing
`providers=["CPUExecutionProvider"]` loads the same model in about a second.
CoreML's `ModelCacheDirectory` does **not** avoid the compile.

## The decision

**Pin the CPU provider, and keep the model loaded in one resident worker.**

`internal/stt/stt_worker.py` is embedded in the binary and spawned as a child
process. It loads the model once and serves a line-delimited JSON protocol on
stdin/stdout. `internal/stt/worker.go` owns the lifecycle;
`internal/stt/transcribe_local.go` routes `Transcribe` and `PartialTranscribe`
through it, with the original one-shot path kept as a narrow fallback.

Measured through `stt.Transcribe()` on a 2.7 s clip:

| | before | after |
|---|---|---|
| per utterance | 16.8-21.6 s | 2.2-3.2 s (CPU pin), then 0.15-0.19 s (warm worker) |
| 12 s dictation (4 previews + final) | ~9.5 s of loads | 0.83 s total |
| model loads | one per call, plus one per preview tick | one per process |

The transcript is unchanged.

## The protocol

One JSON request per line on stdin, one JSON response per line on stdout,
correlated by `id`.

```
-> {"op":"recognize","id":1,"path":"/tmp/clip.wav"}
<- {"id":1,"ok":true,"text":"...","pid":1234,"load_count":1}
<- {"id":1,"ok":false,"error":"..."}
```

Ops are `recognize`, `ping` and `quit`. The handshake is
`{"op":"ready","pid":N,"load_count":1}`.

**stdout is the protocol channel, so it is protected.** At startup the worker
`dup`s the real stdout onto a private descriptor and points fd 1 at stderr.
Any library that prints to stdout — a warning, a progress bar — would
otherwise inject a non-JSON line into the stream and desynchronise the reader.
Diagnostics go to stderr, which the Go side captures into a bounded buffer and
never writes to the terminal (the TUI runs in the alt-screen).

## Lifecycle

- **One worker per process.** The TUI and the web server are separate
  processes and each owns its own, which matches how a remote project's STT
  already runs on the host (`serve --remote`). Nothing is shared across
  processes, so there is no port to collide on and no socket to authenticate.
- **Keyed by model.** The key is (upstream, repo, modelDir, provider). A model
  change stops the old worker and starts a new one.
- **Idle unload.** The worker is stopped after 2 minutes without a request
  (`workerIdleTimeout`). The fp32 export holds roughly 2.4 GB of weights, so it
  must not outlive the user's session.
- **No controlling terminal.** The child is spawned with `Setsid`, so it can
  never open `/dev/tty` and steal the foreground process group from the TUI.
- **Parent death.** On Linux the child gets `Pdeathsig=SIGKILL`, so a killed
  ocode cannot orphan it. Go's `syscall` package exposes no parent-death
  signal on macOS or the BSDs, so there the guard is stdin EOF: the request
  loop ends at EOF and the process exits. `TestRealWorkerScriptExitsOnStdinEOF`
  runs the real script with stubbed dependencies and proves it.

## Concurrency

One request at a time per worker, guarded by a mutex. A live preview uses
`TryLock` and returns `ErrWorkerBusy` at once when the worker is busy, rather
than queueing. A preview is display-only and is superseded by the next one a
moment later, so a stale preview is worth nothing: it must never delay the
final transcription the user is actually waiting for.

## Liveness: never probe with a signal

An exited-but-unreaped child is a **zombie**, and a zombie still answers
`signal 0`. A signal probe therefore reports a crashed worker as alive.

Liveness comes from a reaper goroutine that owns `cmd.Wait()` and records the
exit; `isDead()` reads that flag. Reading `cmd.ProcessState` from another
goroutine is a data race, which the race detector catches.

## The install gate is unchanged

The 409 `stt_install_required` contract is untouched: the `installRequirement`
check in `internal/stt/transcribe.go` runs **before** any worker is started, so
a missing dependency still asks the user to approve the install instead of
failing obscurely. The gate is only partly covered by automated tests — see
TODO.md, "Open: local speech-to-text worker follow-ups".

## Gotchas

- **A GPU provider can be a per-process tax.** Any architecture that spawns a
  process per request pays the CoreML ANE compile every time. Onnx-asr's
  default provider list is "everything available", not "the fastest thing" —
  pass `providers` explicitly.
- **`python -I` ignores `PYTHONPATH`.** That is the point (nothing in the
  working directory may shadow `onnx_asr`), but it means a test that wants to
  stub a dependency has to drop `-I`.
- **The worker must receive absolute clip paths.** Its `cmd.Dir` is the model
  directory, not the caller's working directory, so a relative path does not
  resolve inside the child.
- **Pin the child's working directory.** ONNX Runtime writes a telemetry
  sidecar into the process working directory when it initialises its
  environment, and the working directory here would otherwise be the user's
  project. The worker sets `cmd.Dir` to the model directory and
  `ORT_DISABLE_TELEMETRY=1` — the same hardening the TTS path applies in
  `internal/tts/synth_env.go`.

## Prewarm

The worker still pays its cold load on the first utterance after an idle
unload. `stt.Prewarm` starts it when the recording begins, so the load
overlaps the user speaking.

- **TUI** — `internal/tui/voice.go` calls it from a `crashguard.Go` goroutine
  inside the existing start command. A goroutine rather than a second
  `tea.Cmd` on purpose: the start command must keep returning a single
  message, or the recording would wait for the model load.
- **Web/desktop** — `POST /api/stt/prewarm` answers as soon as the load is
  scheduled and continues in the background; `useVoiceRecorder` calls it from
  `start()`.

Prewarm is best-effort and never prompts for an install: the
install-approval contract belongs to the transcription path, and a prewarm
that silently started a pip install would be a surprise. A prewarm failure
costs a moment of latency, never a lost recording.

Measured with the real model: cold first transcription 9.49 s, prewarm
1.54 s, first transcription after the prewarm 0.17 s.

## Deferred

- **int8 quantization** — MEASURED AND REJECTED. Over a 217-clip corpus
  (6 en_US voices x 3 rates x 12 sentences + a silence clip, 940.5 s of
  audio, 3132 reference words) int8 scored **9.83 % WER against fp32's
  7.92 %** — a paired difference of +3.03 pp (t = 4.42 over 217 clips).
  The regression is not number-specific: number-related errors are equal
  (181 fp32 vs 180 int8) and int8's extra 23 errors are in ordinary words.
  int8 is also ~40 % slower per clip. Its only advantages (a 0.4 s faster
  load, ~1.2 GB less RSS) no longer matter, because the resident worker
  amortises the load. Keep fp32.

Both items are tracked in TODO.md.
