---
type: Gotcha
title: espeak-ng N_PATH_HOME buffer truncates long data paths → exit(1)
description: espeak-ng N_PATH_HOME buffer truncates long data paths, causing silent fallback to CI-baked path and exit(1) — Kokoro speech fails on deep cache layouts
tags:
  - gotcha
  - TTS
  - espeak-ng
  - kokoro
  - phonemizer
  - espeakng-loader
  - path-length
  - buffer-overflow
timestamp: 2026-09-18T11:26:11Z
---
---
type: Gotcha
description: espeak-ng's `N_PATH_HOME` fixed buffer truncates long data paths, causing silent fallback to the wheel's CI-baked path and exit(1) — Kokoro speech synthesis fails on deep cache layouts.
resource: internal/tts/kokoro.go; internal/tts/manifest.go
tags: [gotcha, TTS, espeak-ng, kokoro, phonemizer, espeakng-loader, path-length, buffer-overflow]
status: active
okf_version: "0.1"
---

# espeak-ng N_PATH_HOME buffer truncates long data paths → exit(1)

## The Gotcha

Kokoro TTS speech synthesis fails with a misleading error pointing at a
non-existent path from the espeakng-loader CI builder:

```
Error processing file '/Users/runner/work/espeakng-loader/espeakng-loader/espeak-ng/_dynamic/share/espeak-ng-data/phontab': No such file or directory.
```

The real problem is not a missing file on the user's machine — it is that
espeak-ng silently truncated a long data path, fell back to a compile-time
default baked into the wheel, and then called `exit(1)` when that default
path did not exist on the current host.

## Root cause — the N_PATH_HOME buffer

espeak-ng 1.52.0 stores the data directory in a fixed-size buffer:

```c
// src/libespeak-ng/speech.h
#define N_PATH_HOME 160   // POSIX
#define N_PATH_HOME 230   // Windows
```

When `espeak_Initialize(0x02, 0, data_path, 0)` is called, it copies
`data_path` into this buffer. If the path exceeds `N_PATH_HOME` bytes, the
copy truncates silently. The subsequent `check_data_path` call fails because
the truncated path does not resolve, so espeak falls back to `PATH_ESPEAK_DATA`
— a compile-time constant baked into the wheel on the espeakng-loader CI
builder (e.g. `/Users/runner/work/...`). That path does not exist on the
user's machine, so `espeak_Initialize` calls `exit(1)`.

## Why ocode hits this

ocode's Kokoro engine writes a one-shot `synthesize.py` into the engine cache
dir and runs it with the venv python. The bundled espeak data directory lives
at:

```
<dataRoot>/models/tts/kokoro/<voice>/<version>/<host>/venv/lib/pythonX.Y/site-packages/espeakng_loader/espeak-ng-data
```

With the default `~/.local/share/ocode` data root, this path is exactly 160
bytes — at the buffer boundary. Any non-default data root, deeper voice path,
or longer host string pushes it past 160 bytes.

The manifest (`internal/tts/manifest.go` `kokoroRequirements`) pins
`espeakng-loader==0.2.4` (which embeds espeak-ng 1.52.0 via its git submodule)
and `phonemizer==3.4.0`. Both call into espeak-ng's C library:

- `kokoro_onnx`'s Tokenizer calls `EspeakWrapper.set_data_path(espeakng_loader.get_data_path())`
- phonemizer's `EspeakAPI` calls `espeak_Initialize(0x02, 0, data_path, 0)`

Both paths resolve to the same long bundle path, which hits the buffer limit.

## Why symlinks are NOT a fix

A short symlink to the real data directory works for a direct
`espeak_Initialize` call, but fails through the kokoro/phonemizer call path:

1. espeak-ng's own `check_data_path` follows symlinks (it calls `stat`, not
   `lstat`), so the resolved target is what gets validated against the buffer.
2. phonemizer's `EspeakWrapper.data_path` applies `pathlib.Path.resolve()`,
   re-expanding the symlink to the full real path before passing it to
   espeak-ng.

A symlink `~/.espeak-data → /long/real/path` yields `resolve()` returning
`/long/real/path`, which truncates in the buffer just as before.

## The fix — real copy to a short path

`internal/tts/kokoro.go` implements a two-tier strategy:

1. **Probe the bundled path.** `bundledEspeakDataPath(ctx, venv)` runs the
   venv python:
   ```
   python -c "import espeakng_loader; print(espeakng_loader.get_data_path())"
   ```
   This returns the canonical data path inside the venv's site-packages.

2. **Shorten if needed.** `espeakDataPath(dir, bundled)`:
   - If `len(bundled) < 160` (230 on Windows), return it directly — no copy
     needed.
   - Otherwise, copy the data directory to `<cache>/.espeak-data` using a
     temp-dir + atomic rename. Reuse the existing copy if
     `<dest>/phontab` already exists. Fail with a clear error if even the
     short destination path does not fit within the buffer.

3. **Pass via argv.** The synth command passes the resolved espeak data path
   as `argv[5]` to the python process. The generated `synthesize.py` reads it:
   ```python
   Kokoro(model, voices, espeak_config=EspeakConfig(data_path=sys.argv[5]))
   ```

Because `synthesize.py` is rewritten on every synth call, existing installs
recover automatically — no reinstall required.

## Diagnostic

To check whether a host is affected, run from the Kokoro venv:

```python
import espeakng_loader
path = espeakng_loader.get_data_path()
print(f"Path length: {len(path)} bytes (limit: 160 POSIX / 230 Windows)")
print(f"Path: {path}")
```

If `len(path) >= 160`, the path will be truncated by espeak-ng on POSIX.

To confirm espeak-ng can initialise with a given path:

```c
#include <espeak-ng/speech.h>
// espeak_Initialize returns sample rate (22050) on success, -1 on failure
int rate = espeak_Initialize(AUDIO_OUTPUT_RETRIEVABLE, 0, path, 0);
// rate == 22050 → success; exit(1) → path was truncated
```

A direct C call with a 160-byte path returns exit(1); the same call with a
short path returns 22050. The path must be tested through the full
kokoro/phonemizer call path (which calls `pathlib.Path.resolve()`), not just
a raw `espeak_Initialize`, to account for symlink expansion.

## Related

- `gotchas/tts-pinned-python-upper-bound-needed.md` — another Kokoro manifest
  gotcha (Python version range).
- `tts-speech-playback.md` — the user-facing TTS guide, including Kokoro
  engine details.
- `internal/tts/manifest.go` — `kokoroRequirements` pins espeakng-loader and
  phonemizer versions.
- `internal/tts/kokoro.go` — `bundledEspeakDataPath`, `espeakDataPath`, and
  the synth argv plumbing.
