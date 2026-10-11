---
model_id: longcat-2.5-preview-free
model_version: "2.5-preview"
evaluated_via: opencode-go
evaluated_on: 2026-10-05
stack: docx
stack_corpus_rev: 1
threshold: 0.9
run_type: with-skill validation
---

# Scorecard — longcat-2.5-preview-free on docx (with-skill validation run)

> Valid ONLY for `longcat-2.5-preview-free` @ `2.5-preview`. Closed-book answers produced with the derived skill body prepended to the sheet.

## Per-question results

| id | tags | weight | full | awarded | normalized | notes |
|----|------|-------:|-----:|--------:|-----------:|-------|
| docx-model-01 | docx-model | 3 | 2 | 2 | 1.00 |  |
| docx-model-02 | docx-model, table-locate | 3 | 2 | 2 | 1.00 |  |
| docx-model-03 | docx-model, table-relayout | 2 | 2 | 2 | 1.00 |  |
| docx-locate-01 | table-locate | 3 | 2 | 2 | 1.00 |  |
| docx-locate-02 | table-locate, docx-model | 2 | 2 | 2 | 1.00 |  |
| docx-rowdel-01 | row-delete | 3 | 2 | 1 | 0.50 | missed reflow/update totals |
| docx-rowdel-02 | row-delete, table-locate | 2 | 2 | 2 | 1.00 |  |
| docx-rowdel-03 | row-delete, cell-edit | 2 | 2 | 2 | 1.00 |  |
| docx-rowdel-04 | row-delete, verify-safety | 2 | 2 | 1 | 0.50 | no search of XML text for old value |
| docx-cell-01 | cell-edit | 3 | 2 | 1 | 0.50 | did not say edit existing first run/copy rPr |
| docx-cell-02 | cell-edit, table-locate | 2 | 2 | 2 | 1.00 |  |
| docx-cell-03 | cell-edit, verify-safety | 2 | 2 | 2 | 1.00 |  |
| docx-relayout-01 | table-relayout | 3 | 3 | 3 | 1.00 |  |
| docx-relayout-02 | table-relayout | 3 | 3 | 2 | 0.67 | claimed no add_column exists (it does; appends right, widens) |
| docx-relayout-03 | table-relayout, docx-model | 2 | 2 | 2 | 1.00 |  |
| docx-tblins-01 | table-insert | 3 | 2 | 2 | 1.00 |  |
| docx-tblins-02 | table-insert | 2 | 2 | 2 | 1.00 |  |
| docx-tblins-03 | table-insert, table-relayout | 2 | 2 | 2 | 1.00 |  |
| docx-imgrep-01 | image-replace, docx-model | 3 | 3 | 2 | 0.67 | no removal of old rel/media |
| docx-imgrep-02 | image-replace | 2 | 2 | 2 | 1.00 |  |
| docx-imgrep-03 | image-replace, table-locate | 2 | 2 | 2 | 1.00 |  |
| docx-imgins-01 | image-insert | 3 | 2 | 2 | 1.00 |  |
| docx-imgins-02 | image-insert, table-relayout | 2 | 2 | 2 | 1.00 |  |
| docx-legacy-01 | legacy-doc | 3 | 2 | 2 | 1.00 |  |
| docx-legacy-02 | legacy-doc, verify-safety | 2 | 2 | 2 | 1.00 |  |
| docx-legacy-03 | legacy-doc | 1 | 2 | 2 | 1.00 |  |
| docx-verify-01 | verify-safety | 3 | 2 | 1 | 0.50 | no save-to-new-file/keep original; no unchanged-remainder check |
| docx-verify-02 | verify-safety, docx-model | 2 | 2 | 2 | 1.00 |  |
| docx-verify-03 | verify-safety, image-replace | 2 | 2 | 1 | 0.50 | did not state drawing removal leaves rel+media |

`normalized = min(awarded, full) / full`

## Per-tag subscores

| tag | subscore | n | trust | action |
|-----|---------:|--:|-------|--------|
| cell-edit | 0.833 | 4 | ok | **derive** |
| docx-model | 0.941 | 7 | ok | omit (strong) |
| image-insert | 1.000 | 2 | low-n | omit (strong) |
| image-replace | 0.778 | 4 | ok | **derive** |
| legacy-doc | 1.000 | 3 | low-n | omit (strong) |
| row-delete | 0.722 | 4 | ok | **derive** |
| table-insert | 1.000 | 3 | low-n | omit (strong) |
| table-locate | 1.000 | 6 | ok | omit (strong) |
| table-relayout | 0.929 | 6 | ok | omit (strong) |
| verify-safety | 0.731 | 6 | ok | **derive** |

## Stack score

```
stack_score = Σ(normalized×weight) / Σ(weight) = 87.7%
```

## Derivation targets

Tags below threshold (`< 0.9`): **cell-edit, image-replace, row-delete, verify-safety**. Validation passes only if every target tag reaches 0.9.
