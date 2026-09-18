---
type: Gotcha
title: ONNX Runtime POSIX telemetry writes `:memory:.ses` into process cwd
description: ONNX Runtime POSIX telemetry writes `:memory:.ses` device-id sidecar into process cwd
tags:
  - gotcha
  - onnxruntime
  - ort
  - telemetry
  - python
  - cwd
  - cmd.Dir
  - subprocess
  - tts
timestamp: 2026-09-18T12:37:42Z
---
---
type: Gotcha
description: ONNX Runtime >= 1.29 on POSIX persists a `:memory:.ses` telemetry device-id sidecar into the process cwd on first `import onnxruntime`, causing untracked files in user projects.
resource: internal/tts/synth_env.go; internal/tts/kokoro.go; internal/tts/piper.go
tags: [gotcha, onnxruntime, ort, telemetry, python, cwd, cmd.Dir, subprocess, tts]
status: active
okf_version: "0.1"
---

# ONNX Runtime POSIX telemetry writes `:memory:.ses` into process cwd

## The Gotcha

ONNX Runtime >= 1.29 on POSIX platforms writes a file named exactly
`:memory:.ses` into the **process working directory** whenever
`onnxruntime` is first imported in a Python subprocess. In ocode this
file appeared at the **repo root** — untracked, visible as a dirty-file
warning, and recreated on every TTS synthesis run.

The sidecar is ~51 bytes: `<epoch-millis>\n<uuid>\n`. On POSIX (macOS,
Linux) it is written by `core/platform/posix/telemetry.cc` during
`OrtEnv` initialization — the first `import onnxruntime` in any Python
process.

## Why ocode hits this

ocode's TTS synth children (`kokoroSynth`, `piperSynth`) launched the
venv Python via `exec.Command(python, ...)` **without setting
`cmd.Dir`**. The child inherited the ocode server's cwd — which is
the user's project directory. Every synthesis run dropped
`:memory:.ses` there.

## The fix

`internal/tts/synth_env.go` provides `applySynthProcessEnv(cmd, dir)`
which does two things:

1. **Pins `cmd.Dir`** to the engine cache directory (`<data>/models/tts/<engine>/<voice>/<version>/<os>/<arch>`), well outside any user project.
2. **Injects `ORT_DISABLE_TELEMETRY=1`** into `cmd.Env`, stripping any inherited value.

Both synth paths (`kokoroSynth`, `piperSynth`) now call this before
starting the Python subprocess. A `.gitignore` entry also covers
`:memory:.ses` at any depth as a belt-and-suspenders measure.

## The general rule

**A missing `cmd.Dir` on a subprocess makes the entire app cwd the dump
target.** When spawning a child process that may write files (telemetry,
caches, temp artifacts), always:

- Set `cmd.Dir` to an explicit, project-external path.
- Audit the child's environment for known file-dropping behaviors.
- If the child imports `onnxruntime`, always set
  `ORT_DISABLE_TELEMETRY=1` in its environment.

Never assume the child will write only to a named path — libraries like
ONNX Runtime write sidecars into the cwd silently.

## Opt-out

The documented opt-out is the environment variable:

```
ORT_DISABLE_TELEMETRY=1
```

This string is present in `libonnxruntime.1.30.0.dylib` and confirmed
via upstream issue ORT #32173.

## Verification

Reproduced directly: with an ORT 1.30.0 venv, running

```bash
python -c "import onnxruntime"
```

in an empty directory creates `:memory:.ses`. With

```bash
ORT_DISABLE_TELEMETRY=1 python -c "import onnxruntime"
```

it does not.

In ocode: before the fix, any TTS synthesis (Kokoro or Piper) created
the file at the repo root. After the fix, the file appears only inside
the engine cache directory (or not at all, if telemetry is disabled).