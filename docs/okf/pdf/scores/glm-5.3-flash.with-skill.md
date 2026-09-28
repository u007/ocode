---
model_id: glm-5.3-flash
model_version: "5.3"
evaluated_via: ollama-cloud
evaluated_on: 2026-09-28
stack: pdf
stack_corpus_rev: 1
threshold: 0.75
---

# Scorecard — glm-5.3-flash on pdf (WITH derived skill)

> Valid ONLY for `glm-5.3-flash` @ `5.3`. A version bump invalidates
> this scorecard — re-benchmark.

Validation run. Answers: `pdf/answers/glm-5.3-flash.with-skill.md` (closed-book,
derived skill `derived/pdf.glm-5.3-flash.SKILL.md` prepended, audited zero tool
calls). Graded against `pdf/questions.yaml` corpus_rev 1 (PyMuPDF facts verified
on 1.27.1) with the same strictness as the baseline `scores/glm-5.3-flash.md`:
a `point` whose core concept is present earns full credit (minor missing
sub-items are noted); a `point` stated with a wrong PyMuPDF fact, or satisfied
only by a cover-up (paint/white box) instead of real removal, earns 0.
No new derived skill is produced from this run.

## Per-question results

| id | tags | weight | full | awarded | normalized | notes |
|----|------|-------:|-----:|--------:|-----------:|-------|
| pdf-model-01 | pdf-model | 3 | 2 | 2 | 1.00 | |
| pdf-model-02 | pdf-model, verify-safety | 3 | 2 | 2 | 1.00 | now also states default redaction removes covered graphics/pixels |
| pdf-model-03 | pdf-model | 2 | 2 | 2 | 1.00 | |
| pdf-model-04 | pdf-model | 2 | 2 | 2 | 1.00 | |
| pdf-locate-01 | table-locate | 3 | 2 | 2 | 1.00 | |
| pdf-locate-02 | table-locate | 2 | 2 | 2 | 1.00 | no uniqueness check of the hit (minor, same as baseline) |
| pdf-locate-03 | table-locate, cell-edit | 2 | 2 | 2 | 1.00 | |
| pdf-locate-04 | table-locate, table-relayout | 2 | 2 | 2 | 1.00 | |
| pdf-rowdel-01 | row-delete | 3 | 3 | 3 | 1.00 | redact deleted row + moving region, apply with defaults (correctly says covered rules/shading are removed), then stamp/redraw one row higher; remove-then-stamp order correct; Total recomputed. Content below the table not addressed (minor) |
| pdf-rowdel-02 | row-delete, cell-edit | 3 | 2 | 2 | 1.00 | defaults correct (images=2, graphics=1, text=0); graphics=0/images=0 for cell edits; keep defaults for row deletion |
| pdf-rowdel-03 | row-delete | 2 | 2 | 2 | 1.00 | show_pdf_page+clip from untouched copy; redact first, else duplicated in extraction |
| pdf-rowdel-04 | row-delete, verify-safety | 2 | 2 | 2 | 1.00 | |
| pdf-cell-01 | cell-edit | 3 | 3 | 3 | 1.00 | inset rect, apply_redactions(graphics=0, images=0); right-align at old baseline; match font/size/colour |
| pdf-cell-02 | cell-edit | 2 | 2 | 2 | 1.00 | default-graphics fact now correct; no "verify neighbours after" (minor) |
| pdf-cell-03 | cell-edit, table-relayout | 2 | 2 | 2 | 1.00 | |
| pdf-cell-04 | cell-edit | 2 | 2 | 2 | 1.00 | baseline point; top-left → ~one line too HIGH (corrected); use span origin |
| pdf-relayout-01 | table-relayout | 3 | 3 | 3 | 1.00 | vertical rules now continued through the new row; spill to next page mentioned |
| pdf-relayout-02 | table-relayout | 3 | 3 | 2 | 0.67 | no check that widest content fits the narrower columns (same miss as baseline) |
| pdf-relayout-03 | table-relayout, pdf-model | 2 | 2 | 1 | 0.50 | fidelity-loss point met; never recommends regenerating from source / surgical in-place edit as the better route (baseline did) |
| pdf-relayout-04 | table-relayout, table-insert | 2 | 2 | 2 | 1.00 | |
| pdf-tblins-01 | table-insert | 3 | 2 | 2 | 1.00 | |
| pdf-tblins-02 | table-insert | 2 | 2 | 2 | 1.00 | fills before strokes/text now stated |
| pdf-tblins-03 | table-insert, table-relayout | 2 | 2 | 2 | 1.00 | redact + restamp lower via show_pdf_page clip; page-overflow handling not stated (minor) |
| pdf-imgrep-01 | image-replace | 3 | 3 | 2 | 0.67 | shared-xref catch now correct (global); no aspect-ratio/stretch awareness |
| pdf-imgrep-02 | image-replace, verify-safety | 2 | 2 | 2 | 1.00 | no "verify one image at that rect" (minor) |
| pdf-imgrep-03 | image-replace | 2 | 2 | 2 | 1.00 | |
| pdf-imgins-01 | image-insert | 3 | 2 | 2 | 1.00 | fixed 50pt page margin, but checks intersections and adjusts (same as baseline) |
| pdf-imgins-02 | image-insert | 2 | 2 | 1 | 0.50 | xref reuse correct; no garbage/dedup save (only "PyMuPDF may auto-dedup") |
| pdf-imgins-03 | image-insert, pdf-model | 2 | 2 | 2 | 1.00 | |
| pdf-fonts-01 | fonts | 3 | 2 | 2 | 1.00 | no glyph-coverage check (minor) |
| pdf-fonts-02 | fonts | 2 | 2 | 2 | 1.00 | |
| pdf-fonts-03 | fonts, cell-edit | 2 | 2 | 2 | 1.00 | |
| pdf-verify-01 | verify-safety | 3 | 3 | 2 | 0.67 | never says a render/visual check cannot tell an overlay from a real edit (same miss as baseline) |
| pdf-verify-02 | verify-safety | 3 | 2 | 2 | 1.00 | |
| pdf-verify-03 | verify-safety | 2 | 2 | 2 | 1.00 | |
| pdf-verify-04 | verify-safety, pdf-model | 2 | 2 | 1 | 0.50 | silent-failure point met; asserts on output but no "check return values in the script" (same miss as baseline) |

`normalized = min(awarded, full) / full`

## Per-tag subscores

| tag | subscore | n | trust | action |
|-----|---------:|--:|-------|--------|
| pdf-model | 0.88 | 7 | ok | omit |
| table-locate | 1.00 | 4 | ok | omit (strong) |
| row-delete | 1.00 | 4 | ok | omit (strong) — TARGET |
| cell-edit | 1.00 | 7 | ok | omit (strong) — TARGET |
| table-relayout | 0.88 | 7 | ok | omit |
| table-insert | 1.00 | 4 | ok | omit (strong) |
| image-replace | 0.86 | 3 | low-n | omit |
| image-insert | 0.86 | 3 | low-n | omit |
| fonts | 1.00 | 3 | low-n | omit (strong) |
| verify-safety | 0.88 | 7 | ok | omit |

`subscore = Σ(normalized×weight) / Σ(weight)` over that tag's questions.

## Stack score

```
stack_score = Σ(normalized×weight) / Σ(weight) = 80 / 86 = 93.0%
```

(baseline 72.75 / 86 = 84.6%)

## Derivation targets

None — no tag is below threshold. This is a validation run; no new derived
skill is written.

## Validation

Skill under test: `derived/pdf.glm-5.3-flash.SKILL.md`. TARGET tags: row-delete,
cell-edit. Success = target tag ≥ 0.75.

| tag | baseline | with-skill | Δ | target | verdict |
|-----|---------:|-----------:|---:|--------|---------|
| row-delete | 0.58 | 1.00 | +0.42 | yes | **PASS** |
| cell-edit | 0.73 | 1.00 | +0.27 | yes | **PASS** |
| pdf-model | 0.94 | 0.88 | −0.06 | no | — |
| table-locate | 1.00 | 1.00 | 0.00 | no | — |
| table-relayout | 0.81 | 0.88 | +0.07 | no | — |
| table-insert | 0.78 | 1.00 | +0.22 | no | — |
| image-replace (low-n) | 0.86 | 0.86 | 0.00 | no | — |
| image-insert (low-n) | 1.00 | 0.86 | −0.14 | no | — |
| fonts (low-n) | 1.00 | 1.00 | 0.00 | no | — |
| verify-safety | 0.88 | 0.88 | 0.00 | no | — |

Absorption: every corrected fact in the skill was applied.
- Redaction defaults stated correctly (images=2, graphics=1, text=0) and the
  "vector rules are not removed by default" misconception explicitly rejected
  (rowdel-02, rowdel-01, cell-02, model-02).
- `graphics=0, images=0` passed for single-cell edits (cell-01, rowdel-02).
- No background-paint "removal" anywhere; row deletion and region moves redact
  first, then stamp/redraw (rowdel-01, rowdel-03, relayout-01, tblins-03).
- `insert_text` point = baseline; bbox top-left lands ~one line too HIGH
  (cell-04).

No target tag remains below 0.75, so there are no outstanding wrong claims to
correct and no SKILL.md iteration is needed.

Non-target drift (single-sample resampling; do NOT tweak the skill for it): all
non-target tags stay ≥ 0.75. Drops are from new omissions, not wrong facts —
pdf-model 0.94→0.88 (relayout-03 no longer recommends regenerate-from-source /
in-place edit), image-insert 1.00→0.86 low-n (imgins-02 omitted garbage/dedup
save). The baseline's `replace_image` error (claimed per-page) did not recur:
imgrep-01 now correctly says replacement is global across the xref, but dropped
aspect-ratio awareness, so image-replace is unchanged at 0.86.
