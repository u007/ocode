---
type: Schema
title: Stack Detection
description: How ocode detects repo stacks from meta.yaml markers, and how derived Kaizen skills are gated — marker types, universal corpora (conduct/hallucination/pdf), canonical model ids, and delivery/admission rules.
tags:
  - okf
  - skill
  - stack
  - detection
  - kaizen
  - pdf
  - schema
timestamp: 2026-09-29T07:09:35Z
---
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
  mode: any            # any (default) | all | universal
  markers:
    - { type: dep,  manifest: package.json, name: react }
    - { type: file, glob: "next.config.*" }        # (nextjs example)
    - { type: file, glob: "go.mod" }               # (golang example)
    - { type: file, glob: "Cargo.toml" }           # (rust example)
```

`mode: universal` opts the corpus OUT of marker gating: the skill gate admits it
on model match alone (see the activation gate below). `conduct`, `hallucination`
and `pdf` set it. Markers listed under a universal block are **information
only** — `internal/stackdetect` still globs them (other consumers may read
`Detect` output), but the skill gate ignores them. Nothing in
`docs/okf/_tools/` derives `internal/stackdetect` from `meta.yaml`, so the two
cannot drift silently in either direction.

## Reference markers per planned stack

| stack    | primary marker                                   |
|----------|--------------------------------------------------|
| react    | `dep: react` in `package.json`                   |
| tanstack | `dep: @tanstack/*` in `package.json`             |
| nextjs   | `dep: next` OR `file: next.config.*`             |
| golang   | `file: go.mod`                                    |
| rust     | `file: Cargo.toml`                                |
| pdf      | `file: *.pdf`, `*/*.pdf`, `*/*/*.pdf` (no `**` in Go glob) — **information only**, `mode: universal`, not a gate |
| webforms | none — `mode: universal` (web forms live on any URL; nothing in a repo can detect the task) |
| docx     | `file: *.docx`, `*.doc`, each at root, `*/`, `*/*/` (legacy `.doc` edited after conversion) |
| pptx     | `file: *.pptx`, `*.ppt`, each at root, `*/`, `*/*/` (legacy `.ppt` edited after conversion) |

## Activation gate (the whole point)

A derived skill `derived/<stack>.<model_id>.SKILL.md` activates when **both**:

1. the stack is detected in the current repo (rules above), AND
2. the active model's **canonical id** exactly equals the skill's `tuned_for`.

**Exception — universal corpora.** Gate 1 does not apply to a corpus with
`detection.mode: universal` (`conduct`, `hallucination`, `pdf`, `webforms`): it is admitted
on gate 2 alone. The universal set is the named package-level `universalStacks`
map in `internal/skill/loader.go`, consulted by `stackActive` — a new universal
corpus must be added there. The two reasons a corpus earns a place:

- `conduct` / `hallucination` are **not tech stacks at all** — they probe model
  behaviour that applies in every repo, so no marker file could detect them.
- `pdf` **is** a real document format, but its marker answers the wrong
  question. See the status note below.

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
> `.../tuned_for`). The universal `conduct`, `hallucination`, `pdf` **and `webforms`**
> corpora are admitted on model match alone (no stack marker; the named
> `universalStacks` set consulted by `stackActive` in `internal/skill/loader.go`
> — a new universal corpus must be added there).
>
> **Why `pdf` is universal despite being a real document format.**
> `conduct`/`hallucination` are universal because they are not stacks at all.
> `pdf` differs: its only marker is a `*.pdf` file on disk (root, `*/`, `*/*/`),
> which answers "is there already a PDF in this repo" — not "will this session
> touch one". So the marker gate missed exactly the cases the tuning corrections
> exist for:
>
> - generating a PDF from scratch — no `.pdf` exists yet;
> - a PDF attached or streamed in from outside the repo — the globs only see
>   disk;
> - a PDF deeper than the glob limit (`filepath.Glob` has no `**`);
> - the session that FIRST writes the PDF, because the prompt context is built
>   once and cached for prefix stability **before** the request exists.
>
> In all four cases the model received neither a catalogue line nor the
> force-injected digest, so it had no way to know the corrections were
> available. Un-gating costs ~1.7KB of extra base prompt per session for the
> four models that have a pdf corpus, which is accepted (the `conduct` digest is
> already unconditional at ~2.2KB). A request-conditional gate is deliberately
> NOT used: the prompt context is built and cached before the request exists, so
> keying on request content would re-break prefix stability and reintroduce the
> hole. Authoring side: `docs/okf/pdf/meta.yaml` now reads
> `detection: mode: universal` (matching `docs/okf/hallucination/meta.yaml`),
> with its `markers` list retained and explicitly marked INFORMATION ONLY. The
> four `docs/okf/pdf/derived/*.SKILL.md` `when_to_use` lines were rewritten from
> "load only when the model matches AND the repository contains a PDF" (which
> contradicted the runtime) to the model-only gate.
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

## Regression tests

`internal/skill/kaizen_pdf_universal_test.go`:

- `TestKaizenPDFIsModelGatedOnly` — hermetic temp root with a digest-bearing
  synthetic pdf skill; asserts admission + digest injection while asserting
  `stackdetect.Detect` does NOT report `pdf` there (so the test cannot pass via
  a stray marker), that a non-tuned model gets nothing, and that a `golang`-stack
  skill in the same root is still refused (no fail-open).
- `TestPDFIsUniversalButNotGloballyWildcarded` — `pdf` is universal and
  case-insensitive, but the lookup is exact set membership, not a
  prefix/substring match; conduct/hallucination/empty preserved;
  `golang`/`docx`/`pptx` remain gated.
- `TestKaizenPDFShippedSkillIsAdmittedWithoutAPDFMarker` — the REAL shipped pdf
  digest is force-injected for `opencode-go/space-bunny-free` in a repo root
  with no PDFs, the catalogue advertises it, and the catalogue no longer carries
  the stale "repository contains a PDF" prose (the model-visible half of the
  same bug).
