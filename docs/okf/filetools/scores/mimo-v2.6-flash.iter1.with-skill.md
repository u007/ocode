---
model_id: mimo-v2.6-flash
model_version: "2.6"
evaluated_via: opencode-go
evaluated_on: 2026-10-22
stack: filetools
stack_corpus_rev: 1
threshold: 0.9
sample: 19 live tasks x 3 runs = 57 runs
---

# Scorecard — mimo-v2.6-flash on filetools (WITH the filetools skill)

> Valid ONLY for `mimo-v2.6-flash` @ `2.6`. A version bump invalidates this scorecard — re-benchmark.

Live probe, not closed-book: every run is a real `ocode run -yolo` session in a fresh git repo, scored by
`probe/check.py` from the session's tool calls (see `probe/README.md`). Per-run evidence (violating commands,
outcome problems, tool sequence) is in `../answers/mimo-v2.6-flash.iter1.with-skill/*.check`.

## Provenance

- Binary `bin/ocode` built 2026-10-20; `-effort med`; `-yolo`.
- Isolated HOME as the baseline, but with `filetools-tuning-<model>` ENABLED (its digest is force-injected; confirmed
  by asking the model to quote it before the sweep). Probe binary built 2026-10-22 from this tree with the skill
  embedded (`scratchpad/ocode-ft`), not `bin/ocode`.
- No model-specific provider prompt block (generic small-model prompt).
- Checker hardened during this sweep (apply_patch attribution, quoted-string skeleton, scratch writes outside
  the repo are warnings, `.ocode/` state ignored); every run was re-graded with the final checker.

## Per-task results

| task | runs | clean | correct | score |
|------|-----:|------:|--------:|------:|
| append-test | 3 | 3 | 3 | 1.00 |
| big-bulk | 3 | 3 | 3 | 1.00 |
| big-edit | 3 | 3 | 3 | 1.00 |
| big-json | 3 | 2 | 3 | 0.67 |
| bulk-version | 3 | 3 | 3 | 1.00 |
| create-files | 3 | 3 | 3 | 1.00 |
| dup-anchor | 3 | 3 | 3 | 1.00 |
| generate-data | 3 | 3 | 3 | 1.00 |
| mf-rename | 3 | 3 | 3 | 1.00 |
| ml-rewrite | 3 | 3 | 3 | 1.00 |
| move-function | 3 | 3 | 3 | 1.00 |
| probe-cleanup | 3 | 3 | 3 | 1.00 |
| reindent | 3 | 3 | 3 | 1.00 |
| restore-deleted | 3 | 3 | 3 | 1.00 |
| restore-new-session | 3 | 3 | 3 | 1.00 |
| restore-own | 3 | 3 | 3 | 1.00 |
| restore-partial | 3 | 3 | 3 | 1.00 |
| sort-dedupe | 3 | 3 | 3 | 1.00 |
| try-and-restore | 3 | 3 | 3 | 1.00 |

## Per-tag subscores

| tag | subscore | n | action |
|-----|---------:|--:|--------|
| big-file | 1.00 | 15 | omit |
| bulk-replace | 0.89 | 9 | **derive** |
| create | 1.00 | 12 | omit |
| edit-recovery | 1.00 | 3 | omit (low-n) |
| multi-file | 1.00 | 9 | omit |
| multi-line | 1.00 | 9 | omit |
| restore | 1.00 | 18 | omit |
| transform | 1.00 | 6 | omit |

stack_score = mean(run score) = 0.982 over 57 runs

## Violations (every scripted write, verbatim, first 160 chars)

| task | kind | command or path |
|------|------|-----------------|
| big-json | shell redirect to a file | `cd . && echo "--- counts ---"; grep -cE '^      "timeout_ms": 5000,$' data/settings.json; grep -cE '^      "timeout_ms":' data/settings.json; grep -cE '^      "` |
| big-json | shell redirect to a file | `node -e 'const d=JSON.parse(require("fs").readFileSync("data/settings.json","utf8"));const s=Object.entries(d.services);const bad=s.filter(([k,v])=>Object.keys(` |

## Validation result (target tags = below 0.9 in the baseline)

| tag | baseline | with skill | result |
|-----|---------:|-----------:|--------|
| big-file | 0.53 | 1.00 | PASS |
| bulk-replace | 0.78 | 0.89 | FAIL |
| create | 0.67 | 1.00 | PASS |
| restore | 0.64 | 1.00 | PASS |
| transform | 0.67 | 1.00 | PASS |
