---
model_id: longcat-2.5-preview-free
model_version: "2.5-preview"
evaluated_via: opencode-go
evaluated_on: 2026-10-05
stack: docx
stack_corpus_rev: 1
threshold: 0.9
---

# Scorecard — longcat-2.5-preview-free on docx (2026-10-05 full re-run)

> Valid ONLY for `longcat-2.5-preview-free` @ `2.5-preview`. A version bump invalidates
> this scorecard — re-benchmark.

Answers: `docx/answers/longcat-2.5-preview-free.rerun-2026-10-05.md` (closed-book, full re-run).
Graded against `docx/questions.yaml` corpus_rev 1. The answer file holds a malformed
`idx-rowdel-04` record plus a full `docx-rowdel-04` record at the end; both are the same
answer concept and were graded once as `docx-rowdel-04`. Grading rule: a `point` whose core
concept is present earns credit; a missing sub-concept (e.g. "assert exactly one match",
"promote next cell to restart") forfeits that point.

## Per-question results

| id | tags | weight | full | awarded | normalized | notes |
|----|------|-------:|-----:|--------:|-----------:|-------|
| docx-model-01 | docx-model | 3 | 2 | 2 | 1.00 | |
| docx-model-02 | docx-model, table-locate | 3 | 2 | 2 | 1.00 | |
| docx-model-03 | docx-model, table-relayout | 2 | 2 | 2 | 1.00 | EMU/pt derived from 72pt/inch |
| docx-locate-01 | table-locate | 3 | 2 | 1 | 0.50 | no "assert exactly one match" |
| docx-locate-02 | table-locate, docx-model | 2 | 2 | 1 | 0.50 | no dedupe by _tc / tc_lst + gridSpan; says use len(table.columns) |
| docx-rowdel-01 | row-delete | 3 | 2 | 1 | 0.50 | right removal; no Word-reflows / totals follow-up |
| docx-rowdel-02 | row-delete, table-locate | 2 | 2 | 1 | 0.50 | says convert next row's cell to a continuation (wrong); should promote to restart |
| docx-rowdel-03 | row-delete, cell-edit | 2 | 2 | 1 | 0.50 | cached result noted; offers static replacement instead of writing the result run |
| docx-rowdel-04 | row-delete, verify-safety | 2 | 2 | 2 | 1.00 | |
| docx-cell-01 | cell-edit | 3 | 2 | 1 | 0.50 | loss/survival right; no first-run edit / copy rPr fix |
| docx-cell-02 | cell-edit, table-locate | 2 | 2 | 1 | 0.50 | no number format / alignment |
| docx-cell-03 | cell-edit, verify-safety | 2 | 2 | 1 | 0.50 | no existing w:del/w:ins / w:delText handling |
| docx-relayout-01 | table-relayout | 3 | 3 | 2 | 0.67 | no banding / fill via existing runs |
| docx-relayout-02 | table-relayout | 3 | 3 | 3 | 1.00 | gridSpan not mentioned |
| docx-relayout-03 | table-relayout, docx-model | 2 | 2 | 2 | 1.00 | |
| docx-tblins-01 | table-insert | 3 | 2 | 2 | 1.00 | |
| docx-tblins-02 | table-insert | 2 | 2 | 2 | 1.00 | |
| docx-tblins-03 | table-insert, table-relayout | 2 | 2 | 1 | 0.50 | wrong default: says auto-fit by content, not text width split evenly |
| docx-imgrep-01 | image-replace, docx-model | 3 | 3 | 1 | 0.33 | overwrites `_blob`; no new part + repoint; no old rel removal |
| docx-imgrep-02 | image-replace | 2 | 2 | 2 | 1.00 | |
| docx-imgrep-03 | image-replace, table-locate | 2 | 2 | 2 | 1.00 | |
| docx-imgins-01 | image-insert | 3 | 2 | 2 | 1.00 | doc.add_picture then move paragraph: equivalent |
| docx-imgins-02 | image-insert, table-relayout | 2 | 2 | 2 | 1.00 | |
| docx-legacy-01 | legacy-doc | 3 | 2 | 2 | 1.00 | |
| docx-legacy-02 | legacy-doc, verify-safety | 2 | 2 | 2 | 1.00 | |
| docx-legacy-03 | legacy-doc | 1 | 2 | 2 | 1.00 | |
| docx-verify-01 | verify-safety | 3 | 2 | 2 | 1.00 | |
| docx-verify-02 | verify-safety, docx-model | 2 | 2 | 2 | 1.00 | |
| docx-verify-03 | verify-safety, image-replace | 2 | 2 | 2 | 1.00 | |

`normalized = min(awarded, full) / full`

## Per-tag subscores

| tag | subscore | n | trust | action |
|-----|---------:|--:|-------|--------|
| docx-model | 0.82 | 7 | ok | **derive** |
| table-locate | 0.68 | 6 | ok | **derive** |
| row-delete | 0.61 | 4 | ok | **derive** |
| cell-edit | 0.50 | 4 | ok | **derive** |
| table-relayout | 0.86 | 6 | ok | **derive** |
| table-insert | 0.86 | 3 | low-n | derive (mark low-n) |
| image-replace | 0.78 | 4 | ok | **derive** |
| image-insert | 1.00 | 2 | low-n | omit (strong) |
| legacy-doc | 1.00 | 3 | low-n | omit (strong) |
| verify-safety | 0.92 | 6 | ok | omit (strong) |

`subscore = Σ(normalized×weight) / Σ(weight)` over that tag's questions.

## Stack score

```
stack_score = Σ(normalized×weight) / Σ(weight) = 55.5 / 69 = 80.4%
```

## Derivation targets

Tags below threshold (`< 0.9`): **docx-model, table-locate, row-delete, cell-edit,
table-relayout, table-insert (low-n), image-replace** → feed into
`derived/docx.longcat-2.5-preview-free.SKILL.md`.
