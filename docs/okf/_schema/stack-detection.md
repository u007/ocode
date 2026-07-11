# Stack Detection

How ocode decides a repo "uses" a stack, so a derived skill can be gated on it.
Each stack declares its markers in its `meta.yaml` under `detection:`.

## Marker types

| type      | meaning                                          | example |
|-----------|--------------------------------------------------|---------|
| `dep`     | a dependency present in a manifest                | `react` in `package.json` deps/devDeps |
| `file`    | a marker file exists (glob allowed)               | `go.mod`, `Cargo.toml`, `next.config.*` |
| `content` | a regex matches inside a file                     | `from 'react'` in `**/*.tsx` |

A stack is "detected" when **any** of its markers matches (OR semantics), unless
`meta.yaml` sets `detection.mode: all`.

## `meta.yaml` detection block

```yaml
detection:
  mode: any            # any (default) | all
  markers:
    - { type: dep,  manifest: package.json, name: react }
    - { type: file, glob: "next.config.*" }        # (nextjs example)
    - { type: file, glob: "go.mod" }               # (golang example)
    - { type: file, glob: "Cargo.toml" }           # (rust example)
```

## Reference markers per planned stack

| stack    | primary marker                                   |
|----------|--------------------------------------------------|
| react    | `dep: react` in `package.json`                   |
| tanstack | `dep: @tanstack/*` in `package.json`             |
| nextjs   | `dep: next` OR `file: next.config.*`             |
| golang   | `file: go.mod`                                    |
| rust     | `file: Cargo.toml`                                |

## Activation gate (the whole point)

A derived skill `derived/<stack>.<model_id>.SKILL.md` activates when **both**:

1. the stack is detected in the current repo (rules above), AND
2. the active model's **canonical id** exactly equals the skill's `tuned_for`.

### Canonical model id (provider-independent)

The key is the model, not the host that serves it. ocode already parses its
`model` string by stripping a recognized provider prefix
(`internal/agent/client.go` — `SplitN(model, "/", 2)`), so the canonical id is
simply **ocode's resolved `model`** after that strip:

| runtime `model` string        | provider (stripped) | canonical id → `tuned_for` |
|-------------------------------|---------------------|----------------------------|
| `novita/tencent/hy3`          | `novita`            | `tencent/hy3`              |
| `openrouter/tencent/hy3`      | `openrouter`        | `tencent/hy3`  (same skill)|
| `anthropic/claude-opus-4-8`   | `anthropic`         | `claude-opus-4-8`          |

So one eval of `tencent/hy3` covers that model on **any** host. `tuned_for`
carries the canonical id verbatim (slashes kept, matches the runtime `model`
var); the filename flattens `/` → `__`. The provider is recorded in the
scorecard's `evaluated_via` for provenance only — with one exception: a host
serving a materially different **quantization** can shift behavior, so treat
that as a distinct eval.

> **Status:** WIRED. `internal/skill` (`LoadSkillsForModel` /
> `BuildCatalogForModel`) reads these markers via `stackdetect.Detect(root)` and
> admits a derived skill only when its `stack` is active AND the active model
> matches its `tuned_for` (case-insensitive exact, or provider-prefixed
> `.../tuned_for`). The universal `conduct` corpus is admitted on model match
> alone (no stack marker). See the repo `TODO.md`.
