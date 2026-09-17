---
type: Gotcha
title: Pinned pip requirement sets need an upper Python bound, not just a minimum
description: 'Gotcha: pinned pip requirement sets need an upper Python bound — an unconstrained upper lets pip resolve against an interpreter whose wheels don''t satisfy the most restrictive Requires-Python, producing a misleading resolver dump.'
tags:
  - python
  - pip
  - onnxruntime
  - tts
  - kokoro
  - piper
  - upper-bound
  - interpreter-selection
timestamp: 2026-09-17T04:49:41Z
---
---
type: Gotcha
description: Pinned pip requirement sets need an upper Python bound, not just a minimum — an unconstrained upper means pip resolves against an interpreter whose wheels don't satisfy the most restrictive Requires-Python, producing a misleading resolver dump.
resource: internal/tts/manifest.go; internal/tts/piper.go
tags: [python, pip, onnxruntime, tts, kokoro, piper, upper-bound, interpreter-selection]
status: active
okf_version: "0.1"
---

# Pinned pip requirement sets need an upper Python bound, not just a minimum

When a manifest pins `name==version` Python requirements, the per-host runtime
must declare the **range** of interpreters whose wheels actually satisfy every
requirement — not just a minimum version. The most restrictive `Requires-Python`
constraint among the pinned packages is the binding ceiling, and it is often an
upper bound rather than a lower bound.

## The bug

A user's Kokoro TTS install failed. `<data>/models/tts/install-state.json`
recorded:

```
state="failed", progress=60
error="pip install: exit status 1: ERROR: Ignored the following versions that
require a different python version: ... 0.6.1 Requires-Python >=3.10,<3.14 ...
ERROR: No matching distribution found for kokoro-onnx==0.6.1"
```

The raw pip resolver dump — which reads like a missing package — was the entire
user-visible error. The root cause was invisible: `findPython` selected the
venv interpreter using only `MinPython` (the lower bound). On this macOS arm64
host `/opt/homebrew/bin/python3` is python@3.14 (3.14.6) and is first on PATH,
so it was chosen; `pip install` then resolved the pinned requirements against
3.14 and died because **kokoro-onnx 0.6.1 declares `Requires-Python
<3.14,>=3.10`** (verified via the PyPI JSON API).

## Concrete evidence — how the range is determined

The binding constraint is always the **most restrictive** `Requires-Python`
among the pinned packages. Verify by reading `requires_python` from
`https://pypi.org/pypi/<pkg>/<ver>/json` and checking the wheel `cpXYZ` tags in
the release's `urls[]`.

**Kokoro:**
- `kokoro-onnx==0.6.1`: `Requires-Python >=3.10, <3.14` (PyPI JSON API)
- `onnxruntime==1.30.0` (linux/arm64, linux/amd64, darwin/arm64, windows/amd64):
  `Requires-Python >=3.11`, wheel tags cp311–cp314
- `onnxruntime==1.22.0` (darwin/amd64): `Requires-Python >=3.10`, wheel tags
  cp310–cp313

Result: **3.11–3.13** on most hosts (both `>=3.11` and `<3.14` apply); **3.10–3.13**
on darwin/amd64 (wheels cap at cp313).

**Piper:**
- `piper-tts==1.8.0`: cp39-abi3 (wide compatibility)
- `onnxruntime==1.30.0`: `>=3.11`, wheels cp311–cp314
- `onnxruntime==1.22.1` (darwin/amd64): `>=3.10`, wheels cp310–cp313

Result: **3.11–3.14** on most hosts (piper is cp39-abi3 so onnxruntime is the
binding constraint; onnxruntime 1.30.0 ships cp314 wheels); **3.10–3.13** on
darwin/amd64 (onnxruntime 1.22.1 caps at cp313). Note: a 3.14 piper install
was verified working on darwin/arm64 before this change, so capping piper at
3.13 would have been a regression.

## Operational notes

### Versioned interpreter names must be probed

When a user follows the install error's advice (`brew install python@3.13`),
the resulting binary is `/opt/homebrew/bin/python3.13` — **not** reachable as
bare `python3`. The installer must probe versioned interpreter names
(`python3.13`, `python3.12`, …, pyenv shims, framework installs) before falling
back to `python3`/`python`. On the failing host, `/opt/homebrew/bin/python3.13`
(3.13.14) was found this way, and the pinned kokoro set installed and imported
cleanly (`import kokoro_onnx, onnxruntime`).

### Cache directory is not interpreter-version-keyed

The cache path (`<data>/models/tts/<engine>/<voice>/<version>/<host>/venv`) is
not keyed by interpreter version. The upper bound is enforced when **selecting
an interpreter for a new install only** — deliberately not re-checked in
`Verify()`, because an existing venv keeps working regardless and the cache
would not be invalidated by changing the selection.

### Actionable failure messages

When pip fails with "requires a different python version" or "No matching
distribution found", the installer appends an actionable hint naming the
selected interpreter version, the supported range, and a suggested install
command (e.g. `brew install python@13`). Other pip failures (network, disk,
wheel build) pass through unchanged so the real cause stays legible.

## How to re-verify a requirement set's real range

1. Read `requires_python` from `https://pypi.org/pypi/<pkg>/<ver>/json` for
   each pinned package.
2. Check the wheel `cpXYZ` tags in the release's `urls[]` — the actual wheel
   availability is the ground truth, not just the declared metadata.
3. Take the intersection: the highest lower bound and the lowest upper bound
   across all pinned packages gives the valid interpreter range.
4. If any package has no explicit `Requires-Python` and no cp-tag ceiling, the
   upper bound is unbounded (set `MaxPython` to zero).