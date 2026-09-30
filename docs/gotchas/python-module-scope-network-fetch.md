---
type: Gotcha
title: A module-scope model/corpus fetch turns a "local" Python engine into a networked one
description: 'Gotcha: a Python dependency that loads a Hugging Face model or NLTK corpus at module-import time turns a "local" engine into a networked one with silent failure modes — how to detect (blocked network + empty cache, execute don''t grep), the classmethod-redirection and bare-id traps, the shared install/runtime offline preamble, and the raise-named-error rule.'
resource: internal/tts/melo.go; internal/tts/manifest.go; internal/tts/piper.go; internal/tts/synth_env.go; internal/tts/melo_test.go
tags:
  - gotcha
  - python
  - tts
  - melo
  - huggingface
  - nltk
  - offline
  - module-import
  - classmethod
  - subprocess
  - child-process-env
timestamp: 2026-09-29T21:57:16Z
---
# A module-scope model/corpus fetch turns a "local" Python engine into a networked one

## The gotcha

A Python dependency that loads a Hugging Face model or an NLTK corpus **at
module import time** turns a "local", offline engine into a networked one, and
every failure mode is silent:

- `import` of the package reaches the network (MeloTTS's
  `melo/text/english_bert.py` calls `from_pretrained("bert-base-uncased")` at
  MODULE scope — a real synthesis downloaded ~440 MB from the Hub, into the
  user's cache, with no error and no UI signal that a "local" engine just
  performed a large download).
- `melo/text/cleaner.py` eagerly imports all six language backends, each
  loading a tokenizer at module scope — so even `import melo.api` (the
  install-time import check) reaches the Hub for five more models the engine
  will never use.
- `g2p_en` calls `nltk.download()` when its corpora are missing, writing into
  the **process working directory** — the same class of cwd side effect as the
  ONNX `:memory:.ses` telemetry file (see
  `gotchas/onnx-runtime-telemetry-memory-ses.md`).
- `nltk` was pinned at 3.8.1 for the same reason: from 3.9 the POS tagger
  resource was renamed `averaged_perceptron_tagger_eng` but g2p_en still
  probes the legacy name, so a newer nltk fails at synthesis *after silently
  re-downloading* — pin it (`internal/tts/manifest.go:444`, guarded by
  `TestMeloRequirementsPinTheNLTKRegression`, `internal/tts/melo_test.go:413`).

The engine works perfectly on the developer's machine (warm caches, open
network) and either stalls, downloads hundreds of MB, or fails on the user's
first playback. An install that "verifies the import" on a networked machine
proves nothing about offline behavior.

## How to detect it — run it, don't grep it

**Detect this by executing the code with the network blocked and an empty
cache — not by string-checking the source.** Reasons:

- Indirection hides the calls: `from_pretrained` may be reached through a
  classmethod, a wrapper, a helper module, or a dependency of a dependency
  (MeloTTS's backends are three import levels below `melo.api`).
- A string search for `from_pretrained` or `nltk.download` finds *some* sites
  and misses aliased/derived ones; it also can't tell you whether the call
  actually executes at import versus only inside an unused function.

The practical recipe (as exercised by the MeloTTS tests):

1. Build the environment (venv + staged artifacts) exactly as installed.
2. Point the cache variables at **empty** directories (`HF_HOME`, `NLTK_DATA`)
   so a warm developer cache cannot mask a fetch.
3. Block egress (firewall/sandbox/`HF_HUB_OFFLINE` off + no route) so any
   fetch attempt fails loudly instead of silently succeeding.
4. Run the exact entry points: the install-time import check **and** a real
   synthesis. Any network attempt is now a hard error you can see.

The regression tests in `internal/tts/melo_test.go` go further and *execute*
the driver script against stub `transformers`/`melo` modules whose fetch path
raises — `TestMeloSynthScriptRedirectsBertToStagedFiles`
(`internal/tts/melo_test.go:465`) and
`TestMeloSynthScriptStubsUnbundledTokenizersLoudly`
(`internal/tts/melo_test.go:582`). The comment on the first names the reason
plainly: a string-presence assertion "cannot see" the failure and "passed
happily against the broken version". `TestMeloManifestStagesTheResourcesMeloFetchesSilently`
(`internal/tts/melo_test.go:386`) pins that every silently-fetched resource is
staged, so dropping one from the manifest fails with the reason rather than a
first-playback surprise.

## Trap 1: `from_pretrained` is a classmethod

`transformers.AutoTokenizer.from_pretrained` is a **classmethod**, so the
class object arrives as the first argument. A redirect wrapper must therefore
be declared `def _tokenizer(cls, name=None, ...)` and re-pass `cls` to the
saved unbound function (`internal/tts/melo.go:284`, comment at
`internal/tts/melo.go:258`):

```python
_orig_tokenizer = transformers.AutoTokenizer.from_pretrained.__func__

def _tokenizer(cls, name=None, *args, **kwargs):
    path = _resolve(_orig_tokenizer, name)
    if path is None:
        return _UnbundledTokenizer(name)
    return _orig_tokenizer(cls, path, *args, **kwargs)

transformers.AutoTokenizer.from_pretrained = classmethod(_tokenizer)
```

The failure mode of getting this wrong is subtle: name the first slot after
the model id (forgetting `cls`) and then prepend the class when re-passing,
and the arguments shift — transformers then resolves **the class object
itself as a Hub repo id**, producing a bizarre "repository not found"-style
error for `<class 'AutoTokenizer'>`. This is exactly the bug the test above
was written to catch; it is invisible to string checks. Same shape applies to
`AutoModelForMaskedLM.from_pretrained` (`internal/tts/melo.go:291`).

## Trap 2: "contains a slash" is not a valid offline test

A tempting offline gate is "only allow namespaced (namespaced/repo) ids and
stub anything bare". MeloTTS asks for **both** forms: namespaced ids like
`tohoku-nlp/bert-base-japanese-v3` *and* bare ones like
`bert-base-multilingual-uncased`. A slash test therefore lets bare ids
straight through to the network (comment in `_resolve`,
`internal/tts/melo.go:267`).

Decide on identity, not shape: allow only (a) the exact id whose artifacts are
staged (`bert-base-uncased` → the staged directory), or (b) an existing
filesystem path the driver legitimately passes. Everything else returns
"not bundled".

## Rule: a missing artifact must raise a named error, never fall back to the network

When a required model/tokenizer is not staged, fail **loudly and specifically**
on first use — never fall back to a download, and never return a silent dummy:

```python
raise RuntimeError(
    "MeloTTS tokenizer %r is not bundled; this engine is English-only" % name
)
```

(`_UnbundledTokenizer`, `internal/tts/melo.go:239`). A silent fallback
re-opens the network path the manifest closed; a silent dummy would produce
wrong audio with no error. The message must name the artifact so the fix
("add it to the manifest" / "pick a supported language") is obvious.

Two related invariants from the same fix:

- **Stub the tokenizer, never the module.** `english.py` genuinely calls
  `distribute_phone()` out of `japanese.py`, so the module must stay
  importable — only the tokenizer object is stubbed. Stubbing modules breaks
  English synthesis; stubbing tokenizers only breaks the unused languages
  (guard: `internal/tts/melo_test.go:579`).
- **English-only is stated in the error**, so a user who selects an
  unsupported voice gets the reason at the point of failure.

## Rule: the install-time check and the runtime must share one offline preamble

The import check that runs during install and the synthesis process that runs
on every playback must execute the **same offline setup**, or the check
passes while the runtime fails (or vice versa). In MeloTTS both entry points
are built from one `meloOfflinePreamble` constant
(`internal/tts/melo.go:229`):

- `meloImportCheckScript = meloOfflinePreamble + "import melo.api"`
  (`internal/tts/melo.go:305`), run by `verifyRuntime`
  (`internal/tts/piper.go:286`);
- `meloSynthScript = meloOfflinePreamble + <driver>` (`internal/tts/melo.go:315`),
  run by `meloSynth` (`internal/tts/melo.go:343`).

Both go through `runCmdEnv` (`internal/tts/piper.go:444`) with `meloEnv` and
the engine cache dir as cwd. Before this was shared, the check ran a bare
`python -c "import melo.api"`, reached the Hub, and **failed the install** —
while with `HF_HUB_OFFLINE=1` applied everywhere the failure is a named error
instead. If the two setups can drift, you get a green install and a
networked (or failing) first playback.

## Child-process environment (set by ocode, not the user)

The MeloTTS child process receives its offline configuration **from ocode on
the child**, via `meloEnv` (`internal/tts/melo.go:58`) — none of it is read
from the user's environment, so **no `.env` entry is needed**:

| Variable | Purpose |
|---|---|
| `PYTHONPATH` | Points at the unpacked `melo-src` (the package is not pip-installed) |
| `NLTK_DATA` | Staged `averaged_perceptron_tagger.zip` + `cmudict.zip` — satisfies g2p_en's probe so it never calls `nltk.download()` |
| `HF_HUB_OFFLINE=1` | Hard-fails any Hub attempt instead of silently downloading |
| `TRANSFORMERS_OFFLINE=1` | Belt-and-suspenders for older transformers readers |
| `HF_HOME` | Cache redirected into the engine cache dir, never the user's `~/.cache` |
| `TOKENIZERS_PARALLELISM=false` | Disables tokenizers fork-level parallelism (avoids the fork/thread warning noise in supervised children) |
| `ORT_DISABLE_TELEMETRY=1` | Stops onnxruntime writing `:memory:.ses` into the cwd |

Plus `applySynthProcessEnv` (`internal/tts/synth_env.go:22`), which pins
`cmd.Dir` to the engine cache dir — the shared **child-process env
hardening** all TTS synth paths use; its rationale (cwd side effects,
`ORT_DISABLE_TELEMETRY`) is documented in
`gotchas/onnx-runtime-telemetry-memory-ses.md`. That gotcha plus this one are
the two halves of the rule: *decide everything the child sees — cwd,
environment, cache locations — in the parent, explicitly.*

Pinned by `TestMeloEnvPinsOfflineAndCacheLocations`
(`internal/tts/melo_test.go:352`), which asserts each variable and that the
venv leads `PATH`.

## Checklist for the next Python-based local engine

1. Run the import check AND a real synthesis with network blocked, empty
   `HF_HOME`/`NLTK_DATA`, and a warm-cache-free environment.
2. Grep for module-scope loads only as a hint — trust the blocked run.
3. Redirect `from_pretrained` wrappers take `cls` first and re-pass it; test
   by executing the script, not by grepping it.
4. Gate loads on exact staged identity (or filesystem path), never on id
   shape (slash/bare).
5. Missing artifact ⇒ named `RuntimeError` naming the artifact; never a
   network fallback, never a silent dummy — and stub objects, not modules,
   when a sibling module genuinely imports across languages.
6. Build the install-time check from the same preamble constant as the
   runtime entry point.
7. Set every offline/cache/cwd variable on the child in one `*Env` helper,
   and pin the requirements that guard renamed/vanished upstream resources
   (e.g. `nltk==3.8.1`).
8. Pin hosts you have actually exercised end to end; report the others
   unavailable **with a reason** (`internal/tts/manifest.go:407`) instead of
   offering them and failing mid-install.

## Verification

- `internal/tts/melo_test.go`: `TestMeloEnvPinsOfflineAndCacheLocations`,
  `TestMeloManifestStagesTheResourcesMeloFetchesSilently`,
  `TestMeloRequirementsPinTheNLTKRegression`,
  `TestMeloSynthScriptRedirectsBertToStagedFiles`,
  `TestMeloSynthScriptStubsUnbundledTokenizersLoudly`.
- Manifest anchors: `internal/tts/manifest.go:331` (the ten pinned
  artifacts), `internal/tts/manifest.go:416` (host/Python range).
- User-facing install behavior: `tts-speech-playback.md`.
- Closely related: `gotchas/onnx-runtime-telemetry-memory-ses.md` (child
  process cwd/env hardening).