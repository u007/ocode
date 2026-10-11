---
model_id: longcat-2.5-preview-free
model_version: "2.5-preview"
evaluated_via: opencode-go
evaluated_on: 2026-10-05
stack: pdf
stack_corpus_rev: 1
threshold: 0.9
---

# Scorecard — longcat-2.5-preview-free on pdf (2026-10-05 full re-run)

> Valid ONLY for `longcat-2.5-preview-free` @ `2.5-preview`. A version bump invalidates
> this scorecard — re-benchmark.

Answers: `pdf/answers/longcat-2.5-preview-free.rerun-2026-10-05.md` (closed-book).
Graded against `pdf/questions.yaml` corpus_rev 1. Grading rule: a point whose core
concept is present earns full credit; a point stated with a wrong PyMuPDF fact or
satisfied only by a cover-up earns 0. The answer to pdf-verify-04 had a malformed YAML
indent; parsed by `- id:` record.

## Per-question results

| id | tags | weight | full | awarded | normalized | notes |
|----|------|-------:|-----:|--------:|-----------:|-------|
| pdf-model-01 | pdf-model | 3 | 2 | 2 | 1.00 |  |
| pdf-model-02 | pdf-model, verify-safety | 3 | 2 | 2 | 1.00 |  |
| pdf-model-03 | pdf-model | 2 | 2 | 2 | 1.00 |  |
| pdf-model-04 | pdf-model | 2 | 2 | 2 | 1.00 |  |
| pdf-locate-01 | table-locate | 3 | 2 | 2 | 1.00 |  |
| pdf-locate-02 | table-locate | 2 | 2 | 2 | 1.00 | uniqueness check not stated |
| pdf-locate-03 | table-locate, cell-edit | 2 | 2 | 2 | 1.00 | baseline taken from bbox/rawdict, not span origin |
| pdf-locate-04 | table-locate, table-relayout | 2 | 2 | 2 | 1.00 |  |
| pdf-rowdel-01 | row-delete | 3 | 3 | 2 | 0.67 | redacts only the row; content to shift not redacted |
| pdf-rowdel-02 | row-delete, cell-edit | 3 | 2 | 2 | 1.00 | names params, not the 0 values |
| pdf-rowdel-03 | row-delete | 2 | 2 | 0.5 | 0.25 | no show_pdf_page; only extract/reinsert (partial) |
| pdf-rowdel-04 | row-delete, verify-safety | 2 | 2 | 2 | 1.00 |  |
| pdf-cell-01 | cell-edit | 3 | 3 | 2 | 0.67 | no graphics=0/images=0, rect not inset |
| pdf-cell-02 | cell-edit | 2 | 2 | 2 | 1.00 |  |
| pdf-cell-03 | cell-edit, table-relayout | 2 | 2 | 1 | 0.50 | claims silent truncation; actual: nothing written, negative return |
| pdf-cell-04 | cell-edit | 2 | 2 | 2 | 1.00 |  |
| pdf-relayout-01 | table-relayout | 3 | 3 | 3 | 1.00 |  |
| pdf-relayout-02 | table-relayout | 3 | 3 | 1 | 0.33 | no measure-to-fit; no redact+redraw of whole table; invented tblGrid |
| pdf-relayout-03 | table-relayout, pdf-model | 2 | 2 | 1 | 0.50 | no prefer-regenerate-from-source |
| pdf-relayout-04 | table-relayout, table-insert | 2 | 2 | 2 | 1.00 |  |
| pdf-tblins-01 | table-insert | 3 | 2 | 2 | 1.00 |  |
| pdf-tblins-02 | table-insert | 2 | 2 | 2 | 1.00 |  |
| pdf-tblins-03 | table-insert, table-relayout | 2 | 2 | 1 | 0.50 | no overflow-page / free-space / regenerate alternatives; suggests shrinking |
| pdf-imgrep-01 | image-replace | 3 | 3 | 2 | 0.67 | no replace_image; no aspect ratio |
| pdf-imgrep-02 | image-replace, verify-safety | 2 | 2 | 2 | 1.00 |  |
| pdf-imgrep-03 | image-replace | 2 | 2 | 2 | 1.00 |  |
| pdf-imgins-01 | image-insert | 3 | 2 | 2 | 1.00 |  |
| pdf-imgins-02 | image-insert | 2 | 2 | 1 | 0.50 | no garbage dedup |
| pdf-imgins-03 | image-insert, pdf-model | 2 | 2 | 2 | 1.00 |  |
| pdf-fonts-01 | fonts | 3 | 2 | 2 | 1.00 |  |
| pdf-fonts-02 | fonts | 2 | 2 | 2 | 1.00 |  |
| pdf-fonts-03 | fonts, cell-edit | 2 | 2 | 2 | 1.00 |  |
| pdf-verify-01 | verify-safety | 3 | 3 | 2 | 0.67 | does not say visual check cannot catch overlays; no other-pages check |
| pdf-verify-02 | verify-safety | 3 | 2 | 2 | 1.00 | no garbage collection mentioned |
| pdf-verify-03 | verify-safety | 2 | 2 | 2 | 1.00 |  |
| pdf-verify-04 | verify-safety, pdf-model | 2 | 2 | 1 | 0.50 | generic failures; no return-value checks |

`normalized = min(awarded, full) / full`

## Per-tag subscores

| tag | subscore | n | trust | action |
|-----|---------:|--:|-------|--------|
| cell-edit | 0.88 | 7 | ok | **derive** |
| fonts | 1.00 | 3 | low-n | omit (strong) |
| image-insert | 0.86 | 3 | low-n | **derive** |
| image-replace | 0.86 | 3 | low-n | **derive** |
| pdf-model | 0.88 | 7 | ok | **derive** |
| row-delete | 0.75 | 4 | ok | **derive** |
| table-insert | 0.89 | 4 | ok | **derive** |
| table-locate | 1.00 | 4 | ok | omit (strong) |
| table-relayout | 0.69 | 7 | ok | **derive** |
| verify-safety | 0.88 | 7 | ok | **derive** |

`subscore = Σ(normalized×weight) / Σ(weight)` over that tag's questions.

## Stack score

```
stack_score = Σ(normalized×weight) / Σ(weight) = 85.5%
```

## Derivation targets

Tags below threshold (`< 0.9`): **cell-edit, image-insert (low-n), image-replace (low-n), pdf-model, row-delete, table-insert, table-relayout, verify-safety** → feed into
`derived/pdf.longcat-2.5-preview-free.SKILL.md`.
