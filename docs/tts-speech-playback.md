---
type: Guide
title: Speech playback
description: Speech playback — user-facing doc covering engine availability, installation, playback controls, and DOM-based rendered-text extraction. Updated with espeak-ng data-path length gotcha (2026-09-18).
tags:
  - speech
  - tts
  - playback
  - web
  - desktop
  - user-facing
  - DOM-extraction
timestamp: 2026-09-18T11:27:27Z
---
---
type: Guide
description: Speech playback — user-facing doc covering engine availability, installation, playback controls, and DOM-based rendered-text extraction. Updated with espeak-ng data-path length gotcha (2026-09-18).
tags: [speech, tts, playback, web, desktop, user-facing, DOM-extraction]
status: active
okf_version: "0.1"
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
  every file.

  **Supported Python range:** Each host declares a `MinPython` and `MaxPython`
  (inclusive `<major>.<minor>`) on the `PythonRuntime` struct. The ceiling
  comes from the most restrictive `Requires-Python` constraint among the
  pinned pip requirements for that host — usually onnxruntime. Concrete ranges:

  | Host | Piper range | Binding constraint |
  |------|-------------|---------------------|
  | darwin/arm64, linux/amd64, linux/arm64, windows/amd64 | 3.11–3.14 | onnxruntime 1.30.0 ships cp311–cp314 wheels; piper-tts is cp39-abi3 (wider) |
  | darwin/amd64 | 3.10–3.13 | onnxruntime 1.22.1 universal2 ships cp310–cp313 wheels only |

- **Kokoro** is installable on the same supported host matrix. Its manifest
  pins `kokoro-onnx==0.6.1`, a host-compatible onnxruntime release, the
  `kokoro-v1.0.onnx` model, and `voices-v1.0.bin`, with Apache-2.0/MIT
  license disclosures and SHA-256 checksums for the model artifacts.

  **Supported Python range:**

  | Host | Kokoro range | Binding constraint |
  |------|--------------|---------------------|
  | darwin/arm64, linux/amd64, linux/arm64, windows/amd64 | 3.11–3.13 | kokoro-onnx 0.6.1 declares `Requires-Python <3.14,>=3.10`; onnxruntime 1.30.0 requires >=3.11 |
  | darwin/amd64 | 3.10–3.13 | onnxruntime 1.22.0 ships cp310–cp313 wheels only |

  On a host where the highest available interpreter exceeds the ceiling (e.g.
  macOS arm64 with python@3.14 on PATH), the installer selects a versioned
  interpreter below the ceiling (e.g. `/opt/homebrew/bin/python3.13`) rather
  than the bare `python3` which may resolve to an unsupported version. If no
  suitable interpreter is found, the install fails with a message naming the
  selected interpreter and the supported range, plus advice such as
  `brew install python@<newest supported minor>`.

  **espeak-ng data-path length constraint:** Kokoro depends on
  `espeakng-loader==0.2.4` (which embeds espeak-ng 1.52.0) and
  `phonemizer==3.4.0`. espeak-ng stores the data directory in a fixed
  `N_PATH_HOME` buffer (160 bytes POSIX, 230 bytes Windows —
  `src/libespeak-ng/speech.h`). Paths that exceed this length are truncated
  silently, causing espeak to fall back to a compile-time default baked into
  the wheel (the CI builder's path, e.g. `/Users/runner/work/...`) and then
  call `exit(1)`. Symlinks do NOT help because phonemizer's
  `EspeakWrapper.data_path` applies `pathlib.Path.resolve()`, re-expanding
  the link. The engine mitigates this: if the bundled espeak data path
  exceeds the buffer limit, it copies the data to a short real directory
  (`<cache>/.espeak-data`) and passes that path to the synth process. See
  `gotchas/kokoro-espeak-ng-path-limit.md` for full details.

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

**Interpreters and install failures:** The installer probes candidate
interpreters newest-supported-minor first, including versioned names
(`python3.13`, `python3.12`, …, pyenv shims, framework installs), falling
back to bare `python3`/`python`. Versioned names matter because a user who
follows the install-error advice (`brew install python@3.13`) gets a binary
that is not reachable as bare `python3`. When pip fails with "requires a
different python version" or "No matching distribution found", the error
message appends an actionable hint naming the interpreter version, the
supported range, and a suggested install command. Other pip failures
(network, disk, wheel build) pass through unchanged.

See `gotchas/tts-pinned-python-upper-bound-needed.md` for the general
lesson about upper Python bounds on pinned requirement sets.

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

### Speak button placement (updated 2026-09-17)

The per-message Speak button (`onSpeak={requestSpeech}`) is passed on every
render path that shows assistant text:

- **Standalone assistant text** — the normal assistant message bubble.
- **Tool-group assistant text** — the assistant text inside a `tool-group`
  render entry (every assistant turn that issued tool calls). Previously
  omitted `onSpeak`, so tool-using turns showed Speak on "Thinking" but not
  on the answer. Fixed 2026-09-17.
- **Live streaming text** — the `kind: "text"` part while the assistant is
  streaming. Previously omitted `onSpeak`. Fixed 2026-09-17.

**Invariant:** speech is DOM-derived, never a `Message` field. Neither
`internal/agent/client.go` `Message` nor `web/src/api/types.ts` `Message`
has a speak/speech field. The button is rendered as a **sibling** of the
`[data-speech-content]` `.prose` subtree — not a child — and carries
`data-speech-exclude`. This ensures the button's own label is not read
aloud by the speech extractor (which skips `[data-speech-exclude]`
children).

**Relevant code:** `web/src/components/Chat/ChatPanel.tsx` ~line 866
(grouped assistant content) and ~line 889 (live `kind: "text"` part).

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
