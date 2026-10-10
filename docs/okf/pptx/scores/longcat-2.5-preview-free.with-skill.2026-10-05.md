---
model_id: longcat-2.5-preview-free
model_version: "2.5-preview"
evaluated_via: opencode-go
evaluated_on: 2026-10-05
stack: pptx
stack_corpus_rev: 1
threshold: 0.9
---

# Scorecard — longcat-2.5-preview-free on pptx (with-skill validation run)

> Valid ONLY for `longcat-2.5-preview-free` @ `2.5-preview`. A version bump invalidates
> this scorecard — re-benchmark. This is a WITH-SKILL validation run: the closed-book
> answerer was given the derived skill body prepended to the sheet.

## Per-question results

| id | tags | weight | full | awarded | normalized | notes |
|----|------|-------:|-----:|--------:|-----------:|-------|
| pptx-model-01 | pptx-model | 3 | 2 | 2 | 1.00 | |
| pptx-model-02 | pptx-model | 2 | 2 | 1 | 0.50 | no "read size from prs, don't assume" |
| pptx-model-03 | pptx-model, image-replace | 2 | 2 | 2 | 1.00 | |
| pptx-locate-01 | table-locate | 3 | 2 | 2 | 1.00 | |
| pptx-locate-02 | table-locate, pptx-model | 2 | 2 | 2 | 1.00 | |
| pptx-rowdel-01 | row-delete | 3 | 2 | 1 | 0.50 | no frame height / totals follow-up |
| pptx-rowdel-02 | row-delete, table-relayout | 3 | 2 | 2 | 1.00 | |
| pptx-rowdel-03 | row-delete, cell-edit | 2 | 2 | 1 | 0.50 | no remedy (re-apply fills / use style banding) |
| pptx-rowdel-04 | row-delete, verify-safety | 2 | 2 | 1 | 0.50 | no remove-a:tr + verify absence |
| pptx-cell-01 | cell-edit | 3 | 2 | 2 | 1.00 | |
| pptx-cell-02 | cell-edit, table-relayout | 2 | 2 | 2 | 1.00 | |
| pptx-cell-03 | cell-edit | 2 | 2 | 2 | 1.00 | |
| pptx-relayout-01 | table-relayout | 3 | 3 | 2 | 0.67 | no check of new bottom vs slide/neighbours |
| pptx-relayout-02 | table-relayout | 3 | 3 | 3 | 1.00 | |
| pptx-tblins-01 | table-insert | 3 | 2 | 2 | 1.00 | |
| pptx-tblins-02 | table-insert | 2 | 2 | 2 | 1.00 | |
| pptx-tblins-03 | table-insert, pptx-model | 1 | 2 | 1 | 0.50 | no TablePlaceholder.insert_table |
| pptx-imgrep-01 | image-replace | 3 | 3 | 2 | 0.67 | no drop of unused old image rel |
| pptx-imgrep-02 | image-replace | 2 | 2 | 1 | 0.50 | sharing explained; no new-part remedy |
| pptx-imgrep-03 | image-replace, verify-safety | 2 | 2 | 2 | 1.00 | |
| pptx-imgins-01 | image-insert | 3 | 2 | 2 | 1.00 | |
| pptx-imgins-02 | image-insert | 2 | 2 | 1 | 0.50 | crop/distort hedge; no fix |
| pptx-legacy-01 | legacy-ppt | 3 | 2 | 2 | 1.00 | |
| pptx-legacy-02 | legacy-ppt, verify-safety | 2 | 2 | 2 | 1.00 | |
| pptx-verify-01 | verify-safety | 3 | 2 | 2 | 1.00 | |
| pptx-verify-02 | verify-safety, pptx-model | 2 | 2 | 2 | 1.00 | |
| pptx-verify-03 | verify-safety, image-replace | 2 | 2 | 2 | 1.00 | |

`normalized = min(awarded, full) / full`

## Per-tag subscores

| tag | subscore | n | trust | action |
|-----|---------:|--:|-------|--------|
| pptx-model | 0.85 | 5 | ok | **below 0.9** |
| table-locate | 1.00 | 2 | low-n | ok |
| row-delete | 0.65 | 4 | ok | **below 0.9** |
| cell-edit | 0.89 | 4 | ok | **below 0.9** |
| table-relayout | 0.91 | 4 | ok | ok |
| table-insert | 0.92 | 3 | low-n | ok |
| image-replace | 0.82 | 5 | ok | **below 0.9** |
| image-insert | 0.80 | 2 | low-n | **below 0.9** |
| legacy-ppt | 1.00 | 2 | low-n | ok |
| verify-safety | 0.92 | 6 | ok | ok |

## Stack score

```
stack_score = Σ(normalized×weight) / Σ(weight) = 56 / 65 = 86.2%
```

## Derivation targets

Tags still below threshold (`< 0.9`): **row-delete, image-replace, image-insert, pptx-model, cell-edit**.
The with-skill validation does not pass (requires every target tag >= 0.9).
