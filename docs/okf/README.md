# Kaizen — Per-Model Tech-Stack Benchmark

> **Naming:** this system is called **Kaizen** (continuous improvement). The
> folder is `docs/okf/` for historical reasons — do not expand "OKF" as a name
> anywhere; it collides with unrelated Google OKF features.

Kaizen scores an LLM **by exact version** on a given tech stack (React, Golang,
Rust, …), finds where that specific model is weak, and turns those weak spots
into a small, embeddable skill that ocode ships in its binary and activates only
when a repo actually uses that stack.

## Why exact model version matters

Models change dramatically between versions. `claude-opus-4-7` and
`claude-opus-4-8` are different graders' answers. Every scorecard and every
derived skill is keyed on the **exact** model id + version — never a family like
"claude". A version bump invalidates the scorecard; re-benchmark.

This is deliberately finer-grained than ocode's existing family-level routing in
`internal/agent/provider_prompts.go` (which matches `strings.Contains(model,
"claude")`). Kaizen sits on top of that, targeting the exact version.

## The workflow

```
1. BENCH   Ask a model every question in <stack>/questions.yaml.
2. SCORE   Grade each answer against its rubric → fill a scorecard in
           <stack>/scores/<exact-model-id>.md. Compute per-tag subscores.
3. DERIVE  For tags the model scored LOW on, write a corrective skill in
           <stack>/derived/<stack>.<exact-model-id>.SKILL.md.
           Say NOTHING about tags it already aced (DRY, cache-friendly).
4. EMBED   The derived skill ships via //go:embed all:skills and activates when
           (repo uses the stack) AND (active model == the tuned exact version).
```

Steps 1–3 are manual today. A future grader/harness can parse `questions.yaml`
(it is structured YAML for exactly this reason). The step-4 detection wiring is
a documented follow-up — see the repo `TODO.md`.

**To actually run steps 1–3 (or hand them to an AI), follow
[HOW-TO-EVALUATE.md](./HOW-TO-EVALUATE.md)** — it includes a paste-ready prompt.

## Available stacks

| stack | records | detection marker |
|-------|--------:|------------------|
| `react/` | 26 | `react` dep |
| `golang/` | 33 | `go.mod` |
| `rust/` | 31 | `Cargo.toml` |
| `tanstack/` | 31 | `@tanstack/react-query` or `-router` dep |
| `nextjs/` | 34 | `next` dep or `next.config.*` |
| `conduct/` | 39 | **universal** (any repo) |

`react/` also carries a worked example scorecard + derived skill (illustrative).
The others have corpora only — no evaluations run yet.

**`conduct/` is special:** it tests engineering *behavior* (fail-fast, no silent
fallbacks, no empty catch, always-log-errors, TDD, surgical changes, docs-first,
verification, code-review rigor, root-cause debugging, git/db safety) rather than
framework knowledge. It is anchored to this project's house rules
(`CLAUDE.md` + `AGENTS.md`) and applies to **every** repo — so its derived skill
activates for the tuned model universally, gated only on exact model id, not on a
stack marker. This is the corpus that catches "different models behave
differently while coding."

## Layout

```
docs/okf/
  README.md                 ← you are here
  _schema/                  ← the format specs + templates (read these first)
    question-format.md
    rubric-guide.md
    scorecard.template.md
    stack-detection.md
  react/                    ← the fully-built exemplar stack
    meta.yaml               ← stack id + detection markers + version scope
    questions.yaml          ← SOURCE OF TRUTH: structured Q&A + rubric
    questions.md            ← human-readable render of questions.yaml
    scores/
      README.md
      claude-opus-4-8.md    ← one worked example scorecard
    derived/
      react.claude-opus-4-8.SKILL.md  ← example derived skill
```

## Adding a new stack

Copy `react/` as a template:

1. `mkdir docs/okf/<stack>` with `meta.yaml`, `questions.yaml`, `questions.md`.
2. Fill `questions.yaml` following `_schema/question-format.md`.
3. Add detection markers to `meta.yaml` per `_schema/stack-detection.md`.

Subcategories are just nested folders (`react/hooks/`, `nextjs/app-router/`)
with their own `meta.yaml` + `questions.yaml`. The schema is recursive.

## Corpus quality bar

- **≥ 4 questions per tag** before a per-tag subscore is trustworthy. Fewer than
  that and a single miss swings the subscore too far.
- `answer` fields are **complete but KISS** — enough to grade against, short
  enough to scan. They are grading references, not tutorials.
