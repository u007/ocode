---
model_id: longcat-2.5-preview-free
model_version: "2.5-preview"
evaluated_via: opencode-go
evaluated_on: 2026-10-05
stack: pdf
stack_corpus_rev: 1
threshold: 0.9
---

# Scorecard — longcat-2.5-preview-free on pdf (with-skill validation run)

> Valid ONLY for `longcat-2.5-preview-free` @ `2.5-preview`. A version bump invalidates
> this scorecard — re-benchmark.
> With-skill validation run: the closed-book answerer was given the derived skill
> (`derived/pdf.longcat-2.5-preview-free.SKILL.md` body) prepended to the sheet.

## Per-question results

| id | tags | weight | full | awarded | normalized | notes |
|----|------|-------:|-----:|--------:|-----------:|-------|
| pdf-model-01 | pdf-model | 3 | 2 | 1 | 0.50 | no explicit "move content below up" |
| pdf-model-02 | pdf-model, verify-safety | 3 | 2 | 2 | 1.00 | |
| pdf-model-03 | pdf-model | 2 | 2 | 2 | 1.00 | |
| pdf-model-04 | pdf-model | 2 | 2 | 2 | 1.00 | |
| pdf-locate-01 | table-locate | 3 | 2 | 2 | 1.00 | |
| pdf-locate-02 | table-locate | 2 | 2 | 2 | 1.00 | uniqueness check not stated |
| pdf-locate-03 | table-locate, cell-edit | 2 | 2 | 2 | 1.00 | |
| pdf-locate-04 | table-locate, table-relayout | 2 | 2 | 2 | 1.00 | |
| pdf-rowdel-01 | row-delete | 3 | 3 | 3 | 1.00 | |
| pdf-rowdel-02 | row-delete, cell-edit | 3 | 2 | 2 | 1.00 | image default described imprecisely (not PIXELS) |
| pdf-rowdel-03 | row-delete | 2 | 2 | 2 | 1.00 | |
| pdf-rowdel-04 | row-delete, verify-safety | 2 | 2 | 1 | 0.50 | missed striping/numbering and content below |
| pdf-cell-01 | cell-edit | 3 | 3 | 3 | 1.00 | |
| pdf-cell-02 | cell-edit | 2 | 2 | 1 | 0.50 | missed overlap rule deleting neighbour glyphs; no neighbour check |
| pdf-cell-03 | cell-edit, table-relayout | 2 | 2 | 2 | 1.00 | |
| pdf-cell-04 | cell-edit | 2 | 2 | 2 | 1.00 | |
| pdf-relayout-01 | table-relayout | 3 | 3 | 3 | 1.00 | |
| pdf-relayout-02 | table-relayout | 3 | 3 | 3 | 1.00 | |
| pdf-relayout-03 | table-relayout, pdf-model | 2 | 2 | 2 | 1.00 | |
| pdf-relayout-04 | table-relayout, table-insert | 2 | 2 | 2 | 1.00 | no explicit intersects() test |
| pdf-tblins-01 | table-insert | 3 | 2 | 2 | 1.00 | |
| pdf-tblins-02 | table-insert | 2 | 2 | 2 | 1.00 | |
| pdf-tblins-03 | table-insert, table-relayout | 2 | 2 | 2 | 1.00 | |
| pdf-imgrep-01 | image-replace | 3 | 3 | 2 | 0.67 | no aspect-ratio/stretch awareness |
| pdf-imgrep-02 | image-replace, verify-safety | 2 | 2 | 2 | 1.00 | |
| pdf-imgrep-03 | image-replace | 2 | 2 | 2 | 1.00 | |
| pdf-imgins-01 | image-insert | 3 | 2 | 2 | 1.00 | |
| pdf-imgins-02 | image-insert | 2 | 2 | 2 | 1.00 | |
| pdf-imgins-03 | image-insert, pdf-model | 2 | 2 | 2 | 1.00 | |
| pdf-fonts-01 | fonts | 3 | 2 | 2 | 1.00 | |
| pdf-fonts-02 | fonts | 2 | 2 | 2 | 1.00 | |
| pdf-fonts-03 | fonts, cell-edit | 2 | 2 | 2 | 1.00 | flags&16 not named; font name used |
| pdf-verify-01 | verify-safety | 3 | 3 | 2 | 0.67 | missed "render alone can't catch overlays" |
| pdf-verify-02 | verify-safety | 3 | 2 | 2 | 1.00 | |
| pdf-verify-03 | verify-safety | 2 | 2 | 1 | 0.50 | missed other-pages-untouched, signature/encryption/metadata |
| pdf-verify-04 | verify-safety, pdf-model | 2 | 2 | 2 | 1.00 | |

`normalized = min(awarded, full) / full`

## Per-tag subscores

| tag | subscore | n | trust | action |
|-----|---------:|--:|-------|--------|
| pdf-model | 0.906 | 7 | ok | omit (strong) |
| table-locate | 1.000 | 4 | ok | omit (strong) |
| row-delete | 0.900 | 4 | ok | omit (at threshold) |
| cell-edit | 0.938 | 7 | ok | omit (strong) |
| table-relayout | 1.000 | 7 | ok | omit (strong) |
| table-insert | 1.000 | 4 | ok | omit (strong) |
| image-replace | 0.857 | 3 | low-n | **below threshold** (low-n) |
| image-insert | 1.000 | 3 | low-n | omit (strong, low-n) |
| fonts | 1.000 | 3 | low-n | omit (strong, low-n) |
| verify-safety | 0.824 | 7 | ok | **below threshold** |

`subscore = Σ(normalized×weight) / Σ(weight)` over that tag's questions.

## Stack score

```
stack_score = Σ(normalized×weight) / Σ(weight) = 79.5 / 86 = 92.4%
```

## Derivation targets

Tags still below threshold (`< 0.9`): **verify-safety (0.824), image-replace (0.857, low-n)**.
With-skill validation does NOT pass: every target tag must reach 0.9.
