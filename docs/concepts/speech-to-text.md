# Speech-to-text (voice input)

Dictate a chat message in the TUI, the web UI, or the desktop app. The transcript
is sent as the next message automatically. The model is chosen once in Settings
(web/desktop) or with `/voice model <id>` (TUI).

## Model catalog

`internal/stt/catalog.go` is the single list. It follows Handy's approach
(github.com/cjpais/Handy): a fixed set of offline models, a default, and a
per-model size and language note. Handy's own docs list Parakeet V3 as the
default (about 478 MB, 25 European languages) and Parakeet V2 as the English-only
option (about 473 MB). The catalog also carries two hosted OpenAI models, which
need no local install.

| id | engine | runs on | needs |
| --- | --- | --- | --- |
| `parakeet-tdt-0.6b-v3` (default) | local | this machine | Python 3 + `onnx-asr[cpu,hub]` |
| `parakeet-tdt-0.6b-v2` | local | this machine | Python 3 + `onnx-asr[cpu,hub]` |
| `gpt-4o-transcribe` | openai | OpenAI | OpenAI API key |
| `whisper-1` | openai | OpenAI | OpenAI API key |

Availability is resolved per request (`stt.StatusFor`), never guessed. A local
model is ready only when `python3 -I -c "import onnx_asr"` succeeds (probe cached
60 s). An OpenAI model is ready only when `auth.ResolveKey("openai")` is non-empty.
The first local transcription downloads the model weights from Hugging Face.

## Engines

- **local**: `internal/stt/transcribe_onnx.py` (embedded in the binary) runs
  under `python3 -I` so no module in the working directory can shadow
  `onnx_asr`. It downloads the model into `ModelDir/<id>` (default
  `<user cache>/ocode/stt-models`) as real files and loads from there: onnx-asr's
  own HF cache stores symlinks into `blobs/`, and onnxruntime rejects the
  external weight file (`External data path escapes model directory`). Non-WAV input (browser webm/mp4) is converted to 16 kHz mono WAV
  with `ffmpeg`. Only stdout is parsed; stderr carries download progress.
- **openai**: multipart `POST {base}/audio/transcriptions`, returns `text`.

## Capture

- **Browser (web and desktop)**: `MediaRecorder` on `getUserMedia`. The blob is
  uploaded to `POST /api/stt/transcribe`.
- **TUI**: `stt.StartRecording` runs `arecord` (Linux, if installed) or `ffmpeg`
  (Linux pulse, macOS avfoundation, Windows dshow) to a 16 kHz mono WAV. Output is
  captured, never inherited, so nothing paints over the alt-screen.

## HTTP

- `GET /api/stt` returns `{selected, models[]}` with availability and reasons.
- `PUT /api/stt` with `{"model": id}` saves the selection (unknown id → 400).
- `POST /api/stt/transcribe` (multipart field `audio`, max 25 MB) returns
  `{text, model}`. 503 means the selected model is not available, 502 means the
  engine failed.

Config lives under the `stt` section of `ocodeconfig.json`
(`config.SaveOcodeSTTConfig`).

## Not done yet

- Local Parakeet v3 is verified end to end on Linux: `TestLiveParakeetTranscribes`
  (gated by `OCODE_STT_LIVE=<model dir>`) transcribes a spoken clip correctly.
  It is not run in CI because it needs the ~3 GB model.
- Remote (SSH/WSL) projects transcribe on whichever server the browser is
  talking to, not on the remote host.
