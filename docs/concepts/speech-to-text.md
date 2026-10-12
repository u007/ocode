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
| `parakeet-tdt-0.6b-v3` (default) | local | this machine | Python 3.10+ + `onnx-asr[cpu,hub]` |
| `parakeet-tdt-0.6b-v2` | local | this machine | Python 3.10+ + `onnx-asr[cpu,hub]` |
| `parakeet-unified-en-0.6b` | local | this machine | Python 3.10+ + `onnx-asr[cpu,hub]` + `sentencepiece` |
| `gpt-4o-transcribe` | openai | OpenAI | OpenAI API key |
| `whisper-1` | openai | OpenAI | OpenAI API key |

Availability is resolved per request (`stt.StatusFor`), never guessed. A local
model is ready only when some Python 3.10+ on this host imports `onnx_asr` plus
the model's extra modules (checked with `python -I -c "import ..."`). The
interpreter scan is cached 60 s and shared with transcription, so the probe and
the run use the same Python. The scan runs outside the cache lock: concurrent
callers for one key share one in-flight scan, and status polling for another key
never waits behind it. `StatusFor` probes the local models concurrently, so a
status request waits for the slowest distinct extras set, not the sum. A scan
that hits its 60 s deadline is not cached. An OpenAI model is ready only when `auth.ResolveKey("openai")` is non-empty.
The first local transcription downloads the model weights from Hugging Face.

### Dependency install (asks first)

When no Python 3.10+ imports `onnx_asr`, `Transcribe` does not fail with a hint
and does not install silently: it returns `*stt.InstallRequiredError` (packages
and venv path) and touches nothing. The host asks the user, then retries with
`Options.InstallApproved` (or calls `stt.Install`). The install
(`internal/stt/autoinstall.go`) builds a managed venv at
`<user cache>/ocode/stt-venv` from the newest base Python 3.10+, runs
`pip install onnx-asr[cpu,hub] <model extras>` into it, verifies the imports, then
drops the probe cache. The managed venv is probed first on every later scan, and
installs are additive, so switching to the unified model only adds
`sentencepiece`. A venv sidesteps Homebrew/Debian's "externally-managed
environment" refusal.

| surface | how it asks |
| --- | --- |
| web / desktop | `POST /api/stt/transcribe` answers `409 {code: "stt_install_required", packages, venv}`; the UI shows the shared `ConfirmDialog` (never `window.confirm`: it silently returns false in the Wails desktop webview) and re-uploads with `?install=1` |
| TUI | the status note says `run /voice install to approve it, then dictate again`; `/voice install` is the confirmation and runs `stt.Install` in a `tea.Cmd` (12 min cap), reporting in the transcript |

Rules:

- A pinned `Options.Python` is never installed into; it keeps the old
  error and install hint, and `Install` refuses it.
- `StatusFor` reports a missing-deps model as `available` with a reason saying
  a one-time install is needed (you are asked first) whenever a base Python
  3.10+ exists, so it is selectable. With no Python 3.10+ at all it stays
  unavailable with the full list of rejected candidates.
- `PartialTranscribe` clears `InstallApproved`: live partials are polled and a
  failure is silent, so they can neither install nor nag.
- **Install failures are shown, not swallowed.** The error carries the tail of
  pip's/venv's own output (`runInstallCmd`, last 800 bytes). The web composer
  shows it verbatim in a scrollable `role="alert"` banner prefixed "Voice
  dependency install failed:", and shows "Installing voice dependencies…" while
  it runs; `/voice install` posts it into the transcript. Live previews never
  prompt (`transcribe(..., preview=true)`), so a partial cannot pop the confirm.
- `ModelInfo.needs_install` marks an available-but-uninstalled local model. The
  web composer skips live previews for it and the TUI refuses to open the mic
  (`/voice install` first), so no speech is recorded just to be dropped at the
  prompt. The client treats a 409 as "install required" only when its JSON
  `code` is `stt_install_required`.
- A venv is reused only if `<venv>/python -m pip --version` works
  (`venvHealthy`); a half-built one is deleted and rebuilt, and a failed
  `python -m venv` removes what it left. An approved install runs detached from
  the request context (a tab reload must not SIGKILL pip mid-write) under
  `installTimeout`, and `runInstallCmd` sets `WaitDelay` so a pip child holding
  the output pipe cannot hang it. The base-interpreter scan is cached for
  `probeTTL` (`baseCache`) because status polls and previews ask constantly.
- Installs are serialised by a channel semaphore (`installSem`; a waiter gives
  up when its context ends) and bounded by `installTimeout` (10 min). All pip/venv output is captured, never inherited.
  There is no cross-process lock: two ocode processes on one fresh host can both
  build; the second `pip install` is then a no-op.

## Engines

- **local**: `internal/stt/transcribe_onnx.py` (embedded in the binary) runs
  under the chosen interpreter with `-I`, so no module in the working directory
  can shadow `onnx_asr`. The interpreter is the first candidate that is Python
  3.10+ and imports every required module. The search is shared with TTS
  (`internal/pyinterp`): versioned names newest-first, then bare `python3`, then
  Homebrew, `/usr/local/bin`, `/usr/bin`, pyenv shims and Framework installs.
  Candidates that resolve to the same binary with the same launcher arguments are
  probed once (`py -3.9` and `py -3.11` are distinct). A pinned `Options.Python`
  replaces the search; if that path is not an executable, the reason names it. A failure lists each candidate and why it
  was rejected, for example `(ModuleNotFoundError: No module named 'onnx_asr')`
  or `(3.9)`. A too-old interpreter such as the
  system `/usr/bin/python3` (3.9) is skipped by the version check, so it no
  longer decides the outcome. Install into any one of the
  listed interpreters with `pip install "onnx-asr[cpu,hub]"` (plus
  `sentencepiece` for the unified model). It downloads the model into `ModelDir/<id>` (default
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

- Local Parakeet v3 and Parakeet Unified EN are verified end to end on Linux:
  `TestLiveParakeetTranscribes` and `TestLiveParakeetUnifiedTranscribes`
  (gated by `OCODE_STT_LIVE=<model dir>`) transcribe a spoken clip correctly.
  They are not run in CI because they need the models.
- Parakeet Unified EN comes from `bobNight/parakeet-unified-en-0.6b-onnx`, a
  third-party ONNX conversion of NVIDIA's model (CC-BY-4.0). The helper renames
  its graph files, rebuilds its vocab from the SentencePiece tokenizer, and sets
  the 128 mel-bin feature size, once per download (marked by `.ocode-ready`).
  The unified model is English-only and offline here; NVIDIA's streaming mode is
  not wired.
- Remote (SSH/WSL) projects transcribe on whichever server the browser is
  talking to, not on the remote host.
