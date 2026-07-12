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
> alone (no stack marker).
>
> **Delivery (both discovery states).** An admitted derived skill is always
> **advertised by name** (name + description) so the model can see it — the
> *body* is never force-loaded, and advertising never depends on the semantic
> embedder ranking it. With discovery OFF, `LoadContext` → `BuildCatalogForModel`
> lists it. With discovery ON, `discoveryDocs()` appends `KaizenSkillsForModel`
> to the always-visible names-index (and the fail-open path uses
> `BuildCatalogForModel`). The model loads the full `SKILL.md` on demand via the
> `skill` tool. See the repo `TODO.md`.
>
> **Delivery exception — directive digest (force-injected).** Advertising alone
> proved insufficient for an *overconfident* model: it sees the tuning skill in
> the catalog but never calls the `skill` tool to load the corrective rules,
> because it doesn't feel it needs them (observed on `tencent/hy3`). Because a
> per-model tuning skill is relevant on **every** turn that model is active (by
> definition), its hard rules must be *present*, not merely *offered*. So a
> tuning skill MAY carry a compact **digest** delimited by
> `<!-- kaizen:digest -->` … `<!-- /kaizen:digest -->` in its `SKILL.md` body.
> `skill.KaizenDigestBlock(root, activeModel)` collects the digests of all
> admitted tuning skills and `LoadContext` force-injects them into the base
> prompt as authoritative instructions — **unconditionally** (independent of the
> discovery flag), keyed on `(activeModel, root)` so the cached prefix stays
> stable. This is the *only* case a derived skill puts content beyond name +
> description into context, and it is a **compressed digest, never the full
> body**. Backward-compatible: a tuning skill with no digest section, and every
> non-matching model, yield an empty block — no prompt change. Keep the digest
> lossless on counterintuitive cruxes (e.g. "confidence is not an exemption",
> "bare `git reset` — the objection is scope, not tree-wiping"); a smoothed-over
> digest is a permanent regression, not a benchmark blip.
