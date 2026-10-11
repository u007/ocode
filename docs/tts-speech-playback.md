---
type: Guide
title: Speech playback
description: 'Speech playback — user-facing doc covering engine availability (Browser Native, Piper, Paradee, MeloTTS, Kokoro), installation, playback controls, DOM-based rendered-text extraction, and the fail-open spoken-summary pipeline (turn-active skip, cancellable summariser, unlocked config write). Amended 2026-09-29: short plain-prose messages skip the summariser LLM and are spoken verbatim; long messages open with a one-line recap (recap threshold above the short-text skip gate); the prompt version is salted into the summary cache key. Amended 2026-09-30: added the MeloTTS local engine (id `melo`, darwin/arm64 only, card order Piper → MeloTTS → Kokoro), its per-host Python range, install specifics (unpacked source, shared offline import check) and child-process environment (set by ocode, no .env entry).'
tags:
  - speech
  - tts
  - playback
  - web
  - desktop
  - user-facing
  - DOM-extraction
  - speech-summary
  - fail-open
  - melo
  - local-engine
timestamp: 2026-09-29T21:53:33Z
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

- **Paradee** (engine id `paradee`) is installable on darwin/arm64 only. It
  is an 8.07M-parameter English TTS distilled from Kokoro-82M (single voice
  `af_heart`, Apache-2.0, model card `sahilmahendrakar/Paradee-8M-v1.0`). It
  appears in Settings > Speech playback **between the Piper and MeloTTS
  cards**. Its placement is pinned by `TestMeloCatalogSitsBetweenPiperAndKokoro`
  (the expected order lists Paradee), and its availability by
  `TestParadeeCatalogEntryIsInstallableOnlyWhereVerified`.

  The manifest (`internal/tts/manifest.go`, `paradeeManifest`) pins the model
  repository by commit `f662642d…` (not `main` or the `v1.0` tag) and three
  artifacts, each with SHA-256 and exact size: `paradee_int8.onnx` (9 MB int8
  graph), `config.json` (vocabulary), and the `en_core_web_sm-3.8.0` spaCy
  wheel from the spacy-models GitHub release.

  **The spaCy wheel is staged, not resolved.** misaki's English G2P loads
  `en_core_web_sm` and, when the package is missing, calls
  `spacy.cli.download` — a network fetch at synthesis time. The model is not on
  PyPI, so it is a pinned artifact installed from its verified cache copy
  (`PythonRuntime.Wheels`), which keeps the install offline after download.

  **Runtime:** `misaki==0.9.4` is installed without its `[en]` extra, because
  that extra adds `spacy-curated-transformers` and with it PyTorch; Paradee
  uses misaki's lexicon mode only. The rest of the stack is `spacy==3.8.16`
  (the model's 3.8 line), `num2words`, `phonemizer-fork`, `espeakng-loader`,
  `onnxruntime==1.30.0` and `soundfile`, all exact pins. misaki<3.13 bounds
  the interpreter to **Python 3.11–3.12**.

  **Install check:** after pip, `paradeeVerifyRuntime` runs one probe
  synthesis, not a bare import. misaki builds its spaCy pipeline and espeak
  backend on first use, so a broken model or espeak path fails at install
  rather than on the first playback.

  **Synthesis:** `paradeeSynth` runs `paradeeSynthScript`, a reimplementation
  of the Paradee Space's inference core (sentence split, misaki phonemes,
  token ids, one ONNX graph, 24 kHz). It does not import `paradee_tts` or call
  the Hugging Face Hub. The espeak data path goes through the same
  length-checked helper as Kokoro (`espeakDataPath`), because misaki points
  espeak at the wheel's bundled data on import.

  **License disclosure** (hashed for consent): Paradee model and code
  Apache-2.0, distilled from Kokoro-82M (Apache-2.0); misaki Apache-2.0;
  en_core_web_sm MIT; phonemizer-fork GPL-3.0; espeak-ng GPL-3.0-or-later.

  **End-to-end check (opt-in):** `PARADEE_TTS_E2E=1 go test ./internal/tts/
  -run TestParadeeInstallAndSynthesizeReal -timeout 20m` runs the real
  download, verified install, and offline synthesis (~22 MB, one venv). It
  uses a `/tmp` root because a deep macOS `TMPDIR` overflows espeak's path
  buffer; see `gotchas/kokoro-espeak-ng-path-limit.md`.

- **MeloTTS** (engine id `melo`) is installable on darwin/arm64 only. It
  appears in Settings > Speech playback **between the Paradee and Kokoro
  cards**: the card order is the `Catalog()` return order
  (`internal/tts/manifest.go:646`) because `TTSForm` maps the server response
  without re-sorting (`web/src/components/Settings/TTSForm.tsx:235`). The
  placement is pinned by `TestMeloCatalogSitsBetweenPiperAndKokoro`
  (`internal/tts/melo_test.go:169`) and the web test `renders Paradee and
  MeloTTS between Piper and Kokoro` (`web/src/components/Settings/TTSForm.test.tsx:61`).

  Like the other local engines it follows the same lifecycle — license
  acceptance (MIT) → pin → download → install → enable — and voice selection
  is per model via the existing `model_voice` override. Voices are the
  speakers in the MeloTTS-English-v2 config: EN-US (default), EN-BR,
  EN_INDIA, EN-AU, EN-Default.

  The manifest (`internal/tts/manifest.go:410`) pins TEN artifacts, each with
  an exact byte size and SHA-256: the MeloTTS source archive pinned to commit
  `209145371cff8fc3bd60d7be902ea69cbdb7965a`, the MeloTTS-English-v2
  `config.json` + `checkpoint.pth`, five `bert-base-uncased` files, and two
  NLTK archives (`averaged_perceptron_tagger.zip`, `cmudict.zip`) — about
  650 MB of artifacts plus a torch venv of install footprint.

  MeloTTS is **not pip-installed**: its setup.py has a post-install hook
  running `python -m unidic download` (unpinned, writes into the user's home)
  and its install_requires reads requirements.txt verbatim (torch unpinned).
  The verified archive is unpacked into the cache and imported via PYTHONPATH;
  dependencies are pinned exactly in the manifest (`meloRequirements()`,
  `internal/tts/manifest.go:514`), including `nltk==3.8.1` — deliberately:
  from nltk 3.9 the POS tagger resource was renamed
  `averaged_perceptron_tagger_eng` but g2p_en still probes the legacy name, so
  a newer nltk fails at synthesis after silently re-downloading.

  **Supported Python range:**

  | Host | MeloTTS range | Binding constraint |
  |------|---------------|--------------------|
  | darwin/arm64 | 3.11–3.12 | The only host where the full install was exercised end to end (venv build, checksum-verified artifacts, offline synthesis); 3.13+ was never verified and is excluded by `TestMeloInstallUsesAPinnedPythonRange` |
  | all other hosts | — | No pinned runtime verified: reported unavailable **with a reason** rather than offered and failing mid-install (in particular linux/* and windows/* default torch wheels drag in multi-gigabyte CUDA runtimes) |

  The engine is English-only: only `bert-base-uncased` is staged, and every
  other language tokenizer MeloTTS loads at import time is served by a stub
  that raises a named error on first use. The module-import networking traps
  this engine closed (and how to detect them) are documented in
  `gotchas/python-module-scope-network-fetch.md`.

- **Kokoro** is installable on the same supported host matrix. Its manifest
  pins `kokoro-onnx==0.6.1`, a host-compatible onnxruntime release, the
  `kokoro-v1.0.onnx` model, and `voices-v1.0.bin`, with Apache-2.0/MIT
  license disclosures and SHA-256 checksums for the model artifacts.

  **Supported Python range:**

  | Host | Kokoro range | Binding constraint |
  |------|-------------|---------------------|
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

- The UI does not silently switch to Browser Native when a local engine is
  selected or fails.

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

**MeloTTS install specifics:** The source tree is not pip-installed; the
checksum-verified archive is unpacked into `<cache>/melo-src` and imported via
PYTHONPATH (only the pinned requirements are pip-installed into the venv). The
install-time import check (`verifyRuntime`, `internal/tts/piper.go:291`) runs
`melo_import_check.py` — the **same offline preamble** synthesis will use —
under `meloEnv` via `runCmdEnv` (`internal/tts/piper.go:452`), so the check
and the runtime cannot drift apart. A bare `python -c "import melo.api"` would
reach the Hugging Face Hub, because `melo/text/cleaner.py` eagerly imports all
six language backends and each loads a tokenizer at module scope. Because the
MeloTTS package lives outside the venv, `Verify()` additionally checks that
`melo/api.py` exists in the unpacked source — an artifact set plus a venv
alone does not prove this engine runnable.

**MeloTTS child-process environment (no `.env` entry needed):** The MeloTTS
synthesis child gets `PYTHONPATH`, `NLTK_DATA`, `HF_HUB_OFFLINE=1`,
`TRANSFORMERS_OFFLINE=1`, `TOKENIZERS_PARALLELISM=false`, `HF_HOME` and
`ORT_DISABLE_TELEMETRY=1` from ocode itself (`meloEnv`,
`internal/tts/melo.go:58`, pinned by `TestMeloEnvPinsOfflineAndCacheLocations`)
plus a pinned `cmd.Dir` from the shared cwd/telemetry hardening
(`applySynthProcessEnv`, `internal/tts/synth_env.go:22`). These are set **on
the child process by ocode**, not read from the user's environment, so no
`.env` entry is needed. `ORT_DISABLE_TELEMETRY` and the pinned cwd belong to
the broader child-process env hardening covered by
`gotchas/onnx-runtime-telemetry-memory-ses.md`.

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

### Speak button states (added 2026-09-29)

Every Speak surface goes through the shared `SpeakButton` (`web/src/components/Speech/SpeakButton.tsx`, with `useSpeakAction`): the per-message button in `AssistantText`, the thinking button in `ThinkingBlock`, and ChatPanel's "Speak selection" / "Speak visible". Pressing it disables the button (`aria-busy="true"`), swaps the icon for a spinner and the label for "Speaking…"; it re-enables on the request's outcome and shows the failure reason inline (`role="status"`) and in the `title`. A handler-level `pendingRef` guard drops a second click before React re-renders. There is no silent engine fallback — a failed local-engine read reports the error.

This relies on an outcome contract:
- `requestSpeech(text)` (and `useSpeech().speak` / `replay`) returns `Promise<SpeechOutcome>` where `SpeechOutcome = { ok: boolean, error?: string }` (exported from `SpeechProvider.tsx`). It RESOLVES rather than rejects, so fire-and-forget callers (terminal selections, "Speak visible") cannot leak unhandled rejections.
- The queue item settles the caller's promise when playback STARTS — browser-native: after the first utterance is queued; local engine: after `audio.play()` resolves — not at the end of a long read. A failure before playback resolves `{ ok: false, error }`.
- `stop` / `next` / `clearQueue` and provider unmount settle all outstanding waiters (resolve), so a button can never stay disabled.
- Summariser failures remain non-fatal (the full text is spoken, `ok: true`).
- The `ocode:speak` window event detail carries `handled` (set synchronously by the provider) and `onOutcome`; with no provider mounted, `requestSpeech` resolves `{ ok: false, error: "Speech is unavailable" }`.

Regression tests: `web/src/components/Speech/SpeakButton.test.tsx` and the `speak outcomes` block in `web/src/components/Speech/SpeechProvider.queue.test.tsx`.

## Spoken summaries (fail-open) — updated 2026-09-28, amended 2026-09-29

Optional prose rewriting sits between text extraction and synthesis. When the
speech-summary gate is on (`SpeechSummaryEnabled`, reported by
`GET /api/config/ocode/speech-summary`), the web layer first asks
`POST /api/sessions/{id}/speech-summary` to rewrite the extracted text into
spoken prose using that session's own agent — same credentials, active
profile and usage attribution as its turns. Settings surfaces: the sidebar
on/off checkbox, the model picker (`SpeechSummaryModel`) and the speak
toolbar checkbox; `PUT /api/config/ocode/speech-summary` writes the pair.

**The contract is fail-open: a summary is an optimisation, never a
prerequisite for speech.** Concretely, since the 2026-09-28 fix:

- **`200 {"summary": ""}` is the "no summary, speak the full text" signal**,
  not an error. `resolveSpeechText` (`web/src/components/Speech/SpeechProvider.tsx`)
  returns the original text for an empty or whitespace-only summary *and* for
  a failed request — a side task failing must never silence speech.
- **A short message can produce that same empty summary (added 2026-09-29):
  it is spoken verbatim.** `SummarizeForSpeech`
  (`internal/agent/speech_summary.go`) skips the summariser LLM entirely for
  a message that is already short plain prose — at most `speechSummarySkipChars
  = 400` runes (roughly 20 seconds of speech at 160 wpm) **and** carrying no
  speakable artifact. The gate runs *before* the model client is resolved and
  *before* the 24h summary cache is read, logs one debug line
  (`speaking N chars verbatim; no summary needed`) and returns `""`. This is
  a cost/latency fix only: an empty summary already meant "speak the full
  text", so nothing changed for the client — no new config key, no new
  endpoint, no web-layer change. Length alone is not the test, though: a
  short code block, diff, URL, table row, shell prompt, crash trace or
  path/filename token still goes to the model, because reading source code
  aloud is the exact failure the summariser exists to prevent. Two known
  user-visible limits: a table that arrived as *rendered* text has no pipes
  left to detect, so a short table is read as its bare cell contents; and a
  bare version/abbreviation ("e.g", "U.S", "1.2.3", "3.14") is deliberately
  not treated as a file (a bare filename like "main.go" is). Regression:
  `TestSummarizeForSpeechSkipsTheLLMForShortPlainProse`,
  `TestSummarizeForSpeechStillSummarisesShortTextCarryingArtifacts`,
  `TestSpeechTextNeedsRewrite`.
- **A long message opens with a one-line recap before the detail (added
  2026-09-29).** When the message is at or above
  `speechSummaryRecapMinChars = 800` runes (roughly half a minute of speech at
  160 wpm), `speechSummaryPromptFor` appends `speechSummaryRecapClause` to
  the system prompt. The clause opens with the big picture: one sentence on
  what the whole reply did or decided AND what it means for the listener, in
  words that stand on their own — then the usual two-to-five detail sentences,
  whose detail must not restate it. The threshold was lowered from 1200 (about
  a minute of speech) because by that point the listener has stopped
  orienting and is only waiting for the point. Two wording choices are
  load-bearing: without the "detail must not restate it" clause a model treats
  the opener as sentence one and shrinks the detail to compensate, losing more
  than the opener gains; and the opener must be unlabelled plain prose with no
  heading, colon or line break, because
  `cleanSpeechSummary` strips a recognised "Summary:"-style first line and
  would delete a labelled recap along with its label. Below the threshold no
  recap is requested at all — a recap of a short reply just restates the
  summary's only sentence, and a short reply carrying an artifact needs the
  description, not an orientation line.
- **The recap threshold must sit ABOVE the short-text skip gate.** The
  800-rune recap threshold is deliberately well above the 400-rune
  `speechSummarySkipChars` gate, and that ordering is an invariant: at or
  below the skip gate a message is spoken verbatim without ever reaching the
  summariser, so a recap threshold there would be unreachable on length and
  could only fire for a short artifact-carrying message — precisely the case
  that does not want a recap. `speechSummaryPromptFor` is the single home for
  the length test, and it only runs once the skip gate has let the message
  through. Regression: `TestSpeechSummaryRecapMinCharsSitsAboveTheSkipGate`,
  `TestSummarizeForSpeechAsksForAnOpeningRecapOnALongMessage`,
  `TestSummarizeForSpeechOmitsTheRecapInstructionOnAShorterMessage`.
- **The prompt version is salted into the summary cache key (added
  2026-09-29).** `speechSummaryCacheKey` hashes `speechSummaryPromptVersion`
  (currently `"3"`) together with the model id and the message text —
  sha256 over `version || 0x00 || modelID || 0x00 || text`, via
  `speechSummaryCacheKeyFor` (`internal/agent/speech_summary.go`). The cache
  is otherwise keyed on text + model, which cannot see a PROMPT change:
  without the salt, every summary written in the last `speechSummaryCacheTTL`
  (24h) would keep serving output from the previous prompt — here, the
  no-recap prompt — until the TTL expired. Bump the version whenever the
  prompt or the recap threshold changes; the cost is one paid round trip per
  recently summarised message, which is the intended trade. Regression:
  `TestSpeechSummaryCacheKeyIncludesThePromptVersion`,
  `TestSpeechSummaryCacheKeyShape`.
- **The endpoint degrades immediately while that session's turn is running.**
  `runTurn` holds the session's `as.mu` for the entire turn, so the handler
  checks `h.sessions.IsTurnActive(id)` on the (non-blocking) registry first
  and returns the empty summary instead of queueing behind the turn
  (`internal/server/handler_speech_summary.go`). Regression:
  `TestHandleSessionSpeechSummarySkipsWhileTheTurnIsActive` — empty summary
  and zero summariser calls mid-turn, normal summarising afterwards.
- **The summariser's LLM call is cancellable.** `SummarizeForSpeech` issues
  the call through `chatWithOptionalContext` so the 60s
  `speechSummaryTimeout` cancels the in-flight provider request rather than
  abandoning the wait (`internal/agent/speech_summary.go`). Pinned by
  `TestSummarizeForSpeechCancelsTheProviderCallOnTimeout`; contextless
  clients still fall back to `Chat`
  (`TestSummarizeForSpeechFallsBackToChatForContextlessClients`).
- **The config PUT does not hold `h.mu` across the disk write.** It reads the
  pair under `h.mu`, releases it, saves via the cross-process config lock
  (~5s bound), then re-locks only to update `h.cfg` — `h.mu` is a short-lived
  map lock, never a work lock.

These are per-request/per-session scopes only: there is **no server-global
lock** anywhere in the speech path, so a Speak click never stalls other
sessions or the config/run-state endpoints. Full detail:
`gotchas/speech-summary-turn-lock-wait.md` and
`superpowers/specs/2026-09-09-tts-speech-playback-design.md` §10.2.

## Local-engine rollout requirements (remaining engines)

Local artifacts belong under the TTS cache namespace keyed by engine, voice,
manifest version, and host. Downloads must use the global ownership-safe lock,
verify SHA-256 before an atomic install, and use a selection generation so stale
setup cannot become active. See
`docs/superpowers/plans/2026-09-09-tts-phase0-feasibility.md` for the current
engine blockers and `docs/superpowers/specs/2026-09-09-tts-speech-playback-design.md`
for the approved rollout behavior.