---
model_id: deepseek-v4.1-flash
model_version: "4.1"
evaluated_via: opencode-go
evaluated_on: 2026-09-28
stack: pdf
stack_corpus_rev: 1
threshold: 0.9
validation: with-skill
sample: full   # all 36 questions
---

# Scorecard — deepseek-v4.1-flash on pdf (WITH derived skill, threshold 0.9)

> Valid ONLY for `deepseek-v4.1-flash` @ `4.1`. A version bump invalidates
> this scorecard — re-benchmark.

Answers: `../answers/deepseek-v4.1-flash.with-skill.md`, produced closed-book
with the current `derived/pdf.deepseek-v4.1-flash.SKILL.md` prepended as active
guidance (answerer saw only `_prompts/pdf.md` + the skill; audited zero tool
calls). Graded against `questions.yaml` (corpus_rev 1) with the same strictness
per point as the baseline `deepseek-v4.1-flash.md`. Rubric points are whole; a
point stated with a wrong API fact earns 0. Checked on PyMuPDF 1.27.1:
`Document.new_page(pno)` inserts the new page BEFORE index `pno` (3-page doc,
`new_page(1)` → `[P0, NEW, P1, P2]`).

## Per-question results

| id | tags | weight | full | awarded | normalized | notes |
|----|------|-------:|-----:|--------:|-----------:|-------|
| pdf-model-01 | pdf-model | 3 | 2 | 2 | 1.00 | |
| pdf-model-02 | pdf-model, verify-safety | 3 | 2 | 2 | 1.00 | |
| pdf-model-03 | pdf-model | 2 | 2 | 2 | 1.00 | |
| pdf-model-04 | pdf-model | 2 | 2 | 2 | 1.00 | CropBox-relative coords + rotation/derotation matrices |
| pdf-locate-01 | table-locate | 3 | 2 | 2 | 1.00 | |
| pdf-locate-02 | table-locate | 2 | 2 | 2 | 1.00 | |
| pdf-locate-03 | table-locate, cell-edit | 2 | 2 | 2 | 1.00 | |
| pdf-locate-04 | table-locate, table-relayout | 2 | 2 | 2 | 1.00 | |
| pdf-rowdel-01 | row-delete | 3 | 3 | 3 | 1.00 | redacts deleted row + moving region first, stamps/redraws after, recomputes Total, handles notes |
| pdf-rowdel-02 | row-delete, cell-edit | 3 | 2 | 2 | 1.00 | defaults exact; "partly covered are left whole" now correct; graphics=0/images=0 to keep |
| pdf-rowdel-03 | row-delete | 2 | 2 | 2 | 1.00 | show_pdf_page+clip from untouched copy; redact original first |
| pdf-rowdel-04 | row-delete, verify-safety | 2 | 2 | 2 | 1.00 | |
| pdf-cell-01 | cell-edit | 3 | 3 | 3 | 1.00 | tight in-cell rect, graphics=0/images=0, x = old x1 − width on origin baseline, span style |
| pdf-cell-02 | cell-edit | 2 | 2 | 2 | 1.00 | re-extracts neighbours afterwards |
| pdf-cell-03 | cell-edit, table-relayout | 2 | 2 | 2 | 1.00 | "nothing is written at all — no truncated string and no exception"; all four options |
| pdf-cell-04 | cell-edit | 2 | 2 | 2 | 1.00 | baseline origin; top-left → drawn too HIGH (direction now right) |
| pdf-relayout-01 | table-relayout | 3 | 3 | 2 | 0.67 | points 1–2 full. **Point 3 lost to a wrong API fact**: "continue the table on a new page via `doc.new_page(pno)` (inserted after the current page)". `new_page(pno)` inserts BEFORE `pno`; after the current page is `new_page(pno + 1)` |
| pdf-relayout-02 | table-relayout | 3 | 3 | 3 | 1.00 | same total width, measures widest text per column per candidate font/size, redacts WHOLE old table, redraws header fill/shading/rules/text |
| pdf-relayout-03 | table-relayout, pdf-model | 2 | 2 | 2 | 1.00 | last resort; ask for the source and regenerate |
| pdf-relayout-04 | table-relayout, table-insert | 2 | 2 | 2 | 1.00 | |
| pdf-tblins-01 | table-insert | 3 | 2 | 2 | 1.00 | |
| pdf-tblins-02 | table-insert | 2 | 2 | 2 | 1.00 | style from spans/get_drawings; fills → text → rules; measured alignment |
| pdf-tblins-03 | table-insert, table-relayout | 2 | 2 | 2 | 1.00 | redact + restamp lower is right; own-page / regenerate-from-source alternatives. Repeats "`doc.new_page(pno)` … after the current one" in one bullet, but neither point depends on it |
| pdf-imgrep-01 | image-replace | 3 | 3 | 2 | 0.67 | xref + rects + `page.replace_image` right; shared-xref catch + redact/insert for one occurrence right. **No aspect-ratio/stretch point** (baseline had it; non-target drift) |
| pdf-imgrep-02 | image-replace, verify-safety | 2 | 2 | 2 | 1.00 | no "verify exactly one image at that rect" (same leniency as baseline) |
| pdf-imgrep-03 | image-replace | 2 | 2 | 2 | 1.00 | |
| pdf-imgins-01 | image-insert | 3 | 2 | 2 | 1.00 | anchored to table/text x1 and footer top; aspect ratio; word/drawing/image intersection check |
| pdf-imgins-02 | image-insert | 2 | 2 | 2 | 1.00 | xref reuse + `garbage=4, deflate=True` |
| pdf-imgins-03 | image-insert, pdf-model | 2 | 2 | 2 | 1.00 | |
| pdf-fonts-01 | fonts | 3 | 2 | 2 | 1.00 | no explicit has_glyph check (same leniency as baseline) |
| pdf-fonts-02 | fonts | 2 | 2 | 2 | 1.00 | |
| pdf-fonts-03 | fonts, cell-edit | 2 | 2 | 2 | 1.00 | flag bits now correct (16 bold, 2 italic, 4 serif, 8 mono) |
| pdf-verify-01 | verify-safety | 3 | 3 | 2 | 0.67 | re-extract + render + page count/diff; still never says a render alone can't tell an overlay from a real edit |
| pdf-verify-02 | verify-safety | 3 | 2 | 2 | 1.00 | |
| pdf-verify-03 | verify-safety | 2 | 2 | 2 | 1.00 | |
| pdf-verify-04 | verify-safety, pdf-model | 2 | 2 | 2 | 1.00 | |

`normalized = min(awarded, full) / full`

## Per-tag subscores

| tag | Σ(norm×w) / Σw | subscore | n | trust | action |
|-----|---------------|---------:|--:|-------|--------|
| pdf-model | 16 / 16 | 1.00 | 7 | ok | omit |
| table-locate | 9 / 9 | 1.00 | 4 | ok | omit |
| row-delete | 10 / 10 | 1.00 | 4 | ok | omit — TARGET |
| cell-edit | 16 / 16 | 1.00 | 7 | ok | omit — TARGET |
| table-insert | 9 / 9 | 1.00 | 4 | ok | omit — TARGET |
| image-insert | 7 / 7 | 1.00 | 3 | low-n | omit — TARGET |
| fonts | 7 / 7 | 1.00 | 3 | low-n | omit |
| verify-safety | 16 / 17 | 0.94 | 7 | ok | omit |
| table-relayout | 15 / 16 | 0.94 | 7 | ok | omit — TARGET |
| image-replace | 6 / 7 | 0.86 | 3 | low-n | non-target drift (see below) |

`subscore = Σ(normalized×weight) / Σ(weight)` over that tag's questions.

## Stack score

```
Σ(weight) = 86
losses: pdf-relayout-01 3×(1/3) = 1, pdf-imgrep-01 3×(1/3) = 1, pdf-verify-01 3×(1/3) = 1
stack_score = Σ(normalized×weight) / Σ(weight) = (86 − 3) / 86 = 83 / 86 = 96.5%
```

(baseline without skill: 75.0 / 86 = 87.2%)

## Target tags: baseline → with-skill → pass(≥0.9)?

| tag | baseline | with-skill | Δ | pass (≥ 0.9)? |
|-----|---------:|-----------:|---:|:-------------:|
| row-delete | 0.65 | 1.00 | +0.35 | **PASS** |
| image-insert (low-n) | 0.64 | 1.00 | +0.36 | **PASS** |
| table-relayout | 0.81 | 0.94 | +0.13 | **PASS** |
| cell-edit | 0.84 | 1.00 | +0.16 | **PASS** |
| table-insert | 0.89 | 1.00 | +0.11 | **PASS** |

**All five target tags reach 0.9.** No target remains below threshold.

Residual wrong claim inside a passing target (costs pdf-relayout-01 point 3,
repeated harmlessly in pdf-tblins-03): "continue the table on a new page via
`doc.new_page(pno)` (inserted after the current page)". On 1.27.1
`new_page(pno)` inserts before index `pno`, so the page after the current one
is `doc.new_page(pno + 1)`. The derived skill states the same call (digest item
4 and the table-relayout section: "a new page inserted after the current one
(`doc.new_page(pno)`)"), so the error was skill-induced; the skill should say
`doc.new_page(pno + 1)`.

Non-target: image-replace fell 1.00 → 0.86 (low-n, one sample) because
pdf-imgrep-01 omits the aspect-ratio/stretch caveat the baseline had. Per
HOW-TO-EVALUATE's variance trap this is not bolted onto this skill; catch it in
a fresh baseline if it recurs.

## History

- Earlier with-skill run (0.75 era, targets row-delete + image-insert): row-delete 0.65→0.85, image-insert 0.64→1.00 (both passed 0.75); at the 0.9 lens that run also had table-relayout 0.88, cell-edit 0.78, table-insert 1.00, stack 93.6%.
