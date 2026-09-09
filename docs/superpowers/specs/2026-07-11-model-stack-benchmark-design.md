# Kaizen — Per-Model Tech-Stack Benchmark (Design)

Date: 2026-07-11
Status: approved-pending-review

> Branded **"Kaizen"** (continuous improvement). Lives under `docs/okf/`. Do
> **not** use the acronym "OKF" as a name (avoid collision with Google OKF);
> the folder path is `docs/okf/` but the system is called Kaizen in prose.

## Purpose

A per-tech-stack benchmark corpus that scores any LLM (by **exact version**),
then converts that model's weak areas into a small, embeddable, per-(model ×
stack) skill that activates only when a repo actually uses that stack. The goal
is continuous per-model tuning: as models evolve, re-benchmark and regenerate
the skill.

This extends three mechanisms ocode already has:

- Embedded model-family prompt fragments (`internal/agent/prompts/*.txt`, routed
  by `modelFamilyPrompt` in `provider_prompts.go`).
- Embedded skills (`//go:embed all:skills` in `main.go`).
- The `{MODEL}.OCODE.md` per-model context loader (`LoadModelContext`).

The benchmark is **finer-grained than `provider_prompts.go`**: that file routes
by family (`strings.Contains(model, "claude")`); the benchmark keys on the
**exact** model id + version, because model behavior changes dramatically across
versions.

## Non-goals (this round)

- Auto-wiring stack detection into the live agent to auto-activate derived
  skills. The detection contract is *documented*; the wiring is a follow-up
  (logged in `TODO.md`).
- An automated grader/harness. The scoring model is fully specified so a future
  tool can parse it, but grading this round is manual.
- Populating tanstack / nextjs / golang / rust. React is the exemplar; those
  drop into the identical schema later.

## Folder layout (`docs/okf/`)

```
docs/okf/
  README.md                 — what this is + full workflow (bench → score → skill → binary)
  _schema/
    question-format.md      — the YAML question-record spec (field meanings)
    rubric-guide.md         — scoring model: full/partial/zero, weights, aggregation
    scorecard.template.md   — blank per-model score sheet (exact-version keyed)
    stack-detection.md      — how a repo's stack is detected (marker files/deps)
  react/                    — fully-built EXEMPLAR stack
    meta.yaml               — stack id, detection markers, version scope
    questions.yaml          — ~25 structured Q&A records w/ rubric (source of truth)
    questions.md            — human-readable render of the same corpus
    scores/
      README.md             — drop {exact-model-id}.md scorecards here
      claude-opus-4-8.md    — one worked, filled example scorecard
    derived/
      react.claude-opus-4-8.SKILL.md — example derived skill (embeddable artifact)
```

Subcategories are deeper folders (e.g. `react/hooks/`, `nextjs/app-router/`)
reusing the same `meta.yaml` + `questions.yaml` shape. The schema is recursive,
so new stacks/subcategories need zero new format work.

## Question record schema (YAML)

`questions.yaml` is a list of records:

```yaml
- id: react-keys-01          # stable, unique within the stack
  difficulty: medium         # easy | medium | hard
  weight: 2                  # 1–3, importance in the stack score
  tags: [reconciliation]     # topic tags → drive per-tag subscores
  question: >
    Why does React need `key` on list items, and why is the array index a bad key?
  answer: >                  # complete but KISS — the grading reference
    Keys give each element a stable identity across renders so React matches
    old/new children instead of re-creating them. Index keys break on
    reorder/insert: state and DOM attach to the wrong item.
  rubric:                    # scored points; grader matches an answer against these
    - { point: "stable identity across renders", score: 1 }
    - { point: "index breaks on reorder/insert", score: 1 }
    - { partial: "says only 'performance'", score: 0.5 }
```

Field rules:

- `answer` is the reference: **complete enough to grade against, KISS enough to
  read fast.** Not an essay.
- `rubric` entries are either `point` (a required concept, full credit) or
  `partial` (a common half-right answer, partial credit). `score` is the points.
- `tags` are the mechanism that turns a flat score into *actionable* weakness
  targeting — see Derivation.

## Scoring model (`_schema/rubric-guide.md`)

- **Per question:** sum the `score` of every rubric point the model's answer
  satisfies, cap at the sum of `point` scores (full marks), normalize to 0–1,
  multiply by `weight`.
- **Stack score** = Σ(normalized × weight) / Σ(weight) → a 0–1 (or %) number.
- **Per-tag subscore** = the same aggregation restricted to questions carrying
  that tag. This is the key output: it localizes *where* a model is weak
  (e.g. strong hooks, weak RSC boundaries).

## Scorecard (`_schema/scorecard.template.md`)

One file per **exact model version**, filename = the provider-stripped model id
with `/` flattened to `__` (e.g. `claude-opus-4-8.md`, `tencent__hy3.md`). The
key is the model, NOT the host that serves it: ocode already resolves its
`model` string by stripping a recognized provider prefix
(`client.go` `SplitN(model, "/", 2)`), so `novita/tencent/hy3` and
`openrouter/tencent/hy3` share one key `tencent/hy3` and one scorecard. Front
matter carries:

```yaml
model_id: tencent/hy3        # provider-stripped, exact — never a family
model_version: "3.0"
evaluated_via: novita        # host the eval ran through — provenance only, NOT
                             # part of the key. Exception: a materially different
                             # quantization on a host is a distinct eval.
evaluated_on: 2026-07-11
stack: react
stack_corpus_rev: 1          # which questions.yaml revision was used
```

Body: a per-question table (id, weight, awarded, notes), a per-tag subscore
table, and a total. The per-tag table is what feeds derivation.

**Version discipline:** a scorecard is only valid for the exact `model_id` +
`model_version` it names. A version bump invalidates it — re-benchmark.

## Derivation principle (the important one)

The derived skill encodes **only the model's weak tags for that stack — not the
whole stack.** If a model already nails hooks, the skill says nothing about
hooks. This:

- keeps the artifact small + prefix-cache friendly (matches the "intentionally
  small fragments" contract in `provider_prompts.go`);
- is DRY — never restate what the model already knows;
- is targeted — the skill is a corrective, not a textbook.

Rule of thumb: a tag with subscore below a threshold (start at 0.75) earns a
section in the derived skill; tags at/above threshold are omitted.

## Derived skill (`derived/react.<exact-model-id>.SKILL.md`)

Standard ocode skill format (`name`, `description`, `when_to_use` front matter),
plus benchmark metadata:

```yaml
tuned_for: claude-opus-4-8   # provider-stripped model id (slashes kept; matches
                             # ocode's runtime `model` var). e.g. tencent/hy3
tuned_version: "4.8"
stack: react
source_scorecard: scores/claude-opus-4-8.md
revalidate_when: model_version changes   # marker: stale on version bump
```

`when_to_use` gates activation on **(repo uses React) AND (active model is the
tuned exact version)**. Body = corrective guidance for the weak tags only.

## Embedding + activation

- Embedding: derived skills live under the existing `//go:embed all:skills`
  tree (or a `skills/okf/` subtree), so they ship in the binary with no new
  embed directive.
- Activation gate = **stack detected in repo** AND **active model's canonical
  (provider-stripped) id matches `tuned_for`**.
- Stack detection contract (`_schema/stack-detection.md`): marker files/deps per
  stack — React = `package.json` with a `react` dependency; golang = `go.mod`;
  rust = `Cargo.toml`; etc.
- The live wiring that reads markers and injects the matching skill is a
  **follow-up** — documented here, logged in `TODO.md`, not half-implemented
  this round.

### Update (2026-07-11): detection built, enforcement deferred

- **Detection engine shipped:** `internal/stackdetect.Detect(root) []string`
  detects react/tanstack/nextjs/golang/rust from package.json deps + marker
  files. Tested. It is the runtime mirror of the `meta.yaml` detection blocks.
- **Enforcement approach chosen = skill-catalog filter** (keeps full SKILL.md,
  the user's artifact choice). The skill parser gains `tuned_for`/`stack`
  fields; `skill.BuildCatalog` takes the active model id + detected stacks and
  admits a Kaizen skill only when `stack ∈ detected` AND `tuned_for ==
  provider-stripped(model)`.
  This enforces in code what a bare catalog (advisory `when_to_use`) cannot.
- **Deferred on purpose:** the hook is NOT built yet, because every derived
  artifact today is an illustrative placeholder. Wiring enforcement before a
  real evaluation exists would ship dead code and invite demoing "it works" on
  known-fake content. Order: corpora → one real eval → one real derived skill →
  wire. Tracked in `TODO.md`.

## Deliverables this round

1. `docs/okf/README.md`
2. `docs/okf/_schema/{question-format,rubric-guide,stack-detection}.md` +
   `scorecard.template.md`
3. `docs/okf/react/{meta.yaml, questions.yaml, questions.md}` — **~25** records
   spanning difficulty tiers and ~6–8 tags (hooks, reconciliation, rsc, effects,
   context, perf, refs, suspense).
4. `docs/okf/react/scores/README.md` + one worked `claude-opus-4-8.md` scorecard.
5. `docs/okf/react/derived/react.claude-opus-4-8.SKILL.md` — example derived skill.
6. `TODO.md` entry for the deferred detection-wiring + remaining stacks.

## Open risks

- **questions.yaml ↔ questions.md drift.** Two representations of one corpus.
  Mitigation: `questions.yaml` is the single source of truth; `questions.md` is
  a generated render and says so at the top. (A generator script is out of scope
  this round; the md is hand-synced and marked.)
- **Subscore reliability at ~25 questions.** Some tags will have only 2–3
  questions, so their subscore is coarse. Acceptable for an exemplar; the README
  notes that production stacks want ≥4 questions/tag before trusting a subscore.
