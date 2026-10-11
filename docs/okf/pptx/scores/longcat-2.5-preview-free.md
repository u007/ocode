---
model_id: longcat-2.5-preview-free
model_version: "2.5-preview"
evaluated_via: opencode-go
evaluated_on: 2026-10-05
stack: pptx
stack_corpus_rev: 1
threshold: 0.9
---

# Scorecard — longcat-2.5-preview-free on pptx (2026-10-05 full re-run)

> Valid ONLY for `longcat-2.5-preview-free` @ `2.5-preview`. A version bump invalidates
> this scorecard — re-benchmark.

## Per-question results

| id | tags | weight | full | awarded | normalized | notes |
|----|------|-------:|-----:|--------:|-----------:|-------|
| pptx-model-01 | pptx-model | 3 | 2 | 2 | 1.00 |  |
| pptx-model-02 | pptx-model | 2 | 2 | 1 | 0.50 | no 'read prs.slide_width/height, do not assume' |
| pptx-model-03 | pptx-model, image-replace | 2 | 2 | 2 | 1.00 |  |
| pptx-locate-01 | table-locate | 3 | 2 | 1 | 0.50 | no group recursion; identified row only, no header-based table pick, no unique-match assert |
| pptx-locate-02 | table-locate, pptx-model | 2 | 2 | 2 | 1.00 | said covered cell 'omitted or hMerge' (hedge) |
| pptx-rowdel-01 | row-delete | 3 | 2 | 1 | 0.50 | correct no-API + a:tr removal; no frame height/totals follow-up |
| pptx-rowdel-02 | row-delete, table-relayout | 3 | 2 | 1 | 0.50 | said 'usually does not matter ... or leave it'; hedged on setting frame height |
| pptx-rowdel-03 | row-delete, cell-edit | 2 | 2 | 1 | 0.50 | style banding vs explicit fills ok; no re-apply fills step |
| pptx-rowdel-04 | row-delete, verify-safety | 2 | 2 | 1 | 0.50 | no remove-a:tr + verify absence in all slide parts |
| pptx-cell-01 | cell-edit | 3 | 2 | 2 | 1.00 |  |
| pptx-cell-02 | cell-edit, table-relayout | 2 | 2 | 2 | 1.00 |  |
| pptx-cell-03 | cell-edit | 2 | 2 | 2 | 1.00 |  |
| pptx-relayout-01 | table-relayout | 3 | 3 | 1 | 0.33 | no total/frame-height update, no bottom/neighbour check |
| pptx-relayout-02 | table-relayout | 3 | 3 | 3 | 1.00 |  |
| pptx-tblins-01 | table-insert | 3 | 2 | 2 | 1.00 |  |
| pptx-tblins-02 | table-insert | 2 | 2 | 2 | 1.00 |  |
| pptx-tblins-03 | table-insert, pptx-model | 1 | 2 | 1 | 0.50 | did not remove empty placeholder |
| pptx-imgrep-01 | image-replace | 3 | 3 | 2 | 0.67 | did not drop old image rel |
| pptx-imgrep-02 | image-replace | 2 | 2 | 2 | 1.00 |  |
| pptx-imgrep-03 | image-replace, verify-safety | 2 | 2 | 1 | 0.50 | wrong cause: drop_rel ref count only sees r:id, not a:blip r:embed |
| pptx-imgins-01 | image-insert | 3 | 2 | 2 | 1.00 |  |
| pptx-imgins-02 | image-insert | 2 | 2 | 2 | 1.00 |  |
| pptx-legacy-01 | legacy-ppt | 3 | 2 | 2 | 1.00 |  |
| pptx-legacy-02 | legacy-ppt, verify-safety | 2 | 2 | 2 | 1.00 |  |
| pptx-verify-01 | verify-safety | 3 | 2 | 1 | 0.50 | temp file then replace original (original not kept untouched); no unchanged-remainder check |
| pptx-verify-02 | verify-safety, pptx-model | 2 | 2 | 1 | 0.50 | missed tc/gridCol mismatch, tc without txBody, duplicate cNvPr ids |
| pptx-verify-03 | verify-safety, image-replace | 2 | 2 | 1 | 0.50 | did not say drop unused rel; no hash compare |

`normalized = min(awarded, full) / full`

## Per-tag subscores

| tag | subscore | n | trust | action |
|-----|---------:|--:|-------|--------|
| row-delete | 0.50 | 4 | ok | **derive** |
| verify-safety | 0.58 | 6 | ok | **derive** |
| table-relayout | 0.68 | 4 | ok | **derive** |
| table-locate | 0.70 | 2 | low-n | **derive** |
| image-replace | 0.73 | 5 | ok | **derive** |
| pptx-model | 0.79 | 6 | ok | **derive** |
| cell-edit | 0.89 | 4 | ok | **derive** |
| table-insert | 0.92 | 3 | low-n | omit (strong) |
| image-insert | 1.00 | 2 | low-n | omit (strong) |
| legacy-ppt | 1.00 | 2 | low-n | omit (strong) |

`subscore = Σ(normalized×weight) / Σ(weight)` over that tag's questions.

## Stack score

```
stack_score = Σ(normalized×weight) / Σ(weight) = 76.2%
```

## Derivation targets

Tags below threshold (`< 0.9`): **row-delete, verify-safety, table-relayout, table-locate, image-replace, pptx-model, cell-edit** → feed into
`derived/pptx.longcat-2.5-preview-free.SKILL.md`.
