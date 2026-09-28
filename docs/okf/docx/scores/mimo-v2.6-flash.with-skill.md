---
model_id: mimo-v2.6-flash
model_version: "2.6"
evaluated_via: opencode-go
evaluated_on: 2026-09-28
stack: docx
stack_corpus_rev: 1
threshold: 0.9
validation: with-skill
---

# Scorecard — mimo-v2.6-flash on docx (with-skill validation, threshold 0.9)

> Valid ONLY for `mimo-v2.6-flash` @ `2.6`. A version bump invalidates
> this scorecard — re-benchmark.

> **WITH-SKILL VALIDATION RUN.** Answers: `docx/answers/mimo-v2.6-flash.with-skill.md`,
> produced closed-book with the current derived skill
> `derived/docx.mimo-v2.6-flash.SKILL.md` (`docx-tuning-mimo-v2.6-flash`)
> prepended to the answerer prompt as active guidance (audited zero tool calls).
> Baseline: `scores/mimo-v2.6-flash.md`. No new derived skill is produced from
> this run.

> Target tags for this skill (baseline < 0.9): **row-delete**, **cell-edit**,
> **image-replace**.

Graded against `docx/questions.yaml` corpus_rev 1 (python-docx facts verified on 1.2.0).
Grading rule applied (same strictness as the baseline): a `point` whose core
concept is present earns full credit (minor missing sub-items are noted); a
`point` stated with a wrong API fact, or satisfied only by a cover-up
(hidden/white text, zero-height rows, white boxes), earns 0. Points are whole;
fractions come only from a listed partial.

Contamination check: CLEAN. Target answers track the skill's wording
(rowdel-03 "Word does not recalculate it merely on open", "it only prompts";
imgrep-01 "DocumentPart has NO get_or_add_image_part", "part.drop_rel() … counts
@r:id references only"), which is the intended absorption. Non-target answers
still diverge from the key where the skill is silent (tblins-03 wrong
column-width API, verify-01 no unchanged-remainder diff, locate-01 no explicit
single-row assertion), consistent with a blind run.

## Per-question results

| id | tags | weight | full | awarded | normalized | notes |
|----|------|-------:|-----:|--------:|-----------:|-------|
| docx-model-01 | docx-model | 3 | 2 | 2 | 1.00 | |
| docx-model-02 | docx-model, table-locate | 3 | 2 | 2 | 1.00 | first replace recipe uses "the paragraph's original rPr", but the run-offset split ("replacement lands in one run") is the formatting-preserving method |
| docx-model-03 | docx-model, table-relayout | 2 | 2 | 2 | 1.00 | |
| docx-locate-01 | table-locate | 3 | 2 | 2 | 1.00 | table by header/column signature; row by stripped cell text with a second-column confirm; no explicit "assert exactly one row" (minor, same as baseline) |
| docx-locate-02 | table-locate, docx-model | 2 | 2 | 2 | 1.00 | `[c0, c0, c2]`; `row._tr.tc_lst` or dedupe by object identity |
| docx-rowdel-01 | row-delete | 3 | 2 | 2 | 1.00 | no delete API, `tr.getparent().remove(tr)`; Word reflows, no gap; re-derive SUM(ABOVE)/subtotals/totals — baseline gap closed |
| docx-rowdel-02 | row-delete, table-locate | 2 | 2 | 2 | 1.00 | orphaned continuations; promote next cell to restart and carry content |
| docx-rowdel-03 | row-delete, cell-edit | 2 | 2 | 2 | 1.00 | cached result; "Word does not recalculate it merely on open; python-docx certainly does not"; write the value into the result `w:t`, keep the field; updateFields only prompts — baseline error gone |
| docx-rowdel-04 | row-delete, verify-safety | 2 | 2 | 2 | 1.00 | searches document.xml only, not headers/footers (minor, same as baseline) |
| docx-cell-01 | cell-edit | 3 | 2 | 2 | 1.00 | |
| docx-cell-02 | cell-edit, table-locate | 2 | 2 | 2 | 1.00 | dependency-ordered recompute from data; field result `w:t`, never `cell.text`; keeps number format and alignment — baseline minor gap closed |
| docx-cell-03 | cell-edit, verify-safety | 2 | 2 | 2 | 1.00 | direct edit untracked; hidden w:ins/w:del/w:delText; decide tracked vs clean, accept/reject first; no explicit "check w:delText for the old value" after resolving (minor) |
| docx-relayout-01 | table-relayout | 3 | 3 | 3 | 1.00 | add_row appends, no inherited formatting (width not mentioned, minor); deepcopy + addnext; fill runs keeping rPr; check band position |
| docx-relayout-02 | table-relayout | 3 | 3 | 3 | 1.00 | |
| docx-relayout-03 | table-relayout, docx-model | 2 | 2 | 2 | 1.00 | |
| docx-tblins-01 | table-insert | 3 | 2 | 2 | 1.00 | |
| docx-tblins-02 | table-insert | 2 | 2 | 2 | 1.00 | |
| docx-tblins-03 | table-insert, table-relayout | 2 | 2 | 1 | 0.50 | point 1 = 1: text width split evenly (side claim "w:tblW set to 100%" is wrong — 1.2.0 writes `tblW type="auto" w="0"`; noted). point 2 = 0: widths copied via `table.columns[i].width = ref_col.width` "(setting a column width writes w:tcW on every cell in it …)" — wrong API fact, verified on 1.2.0: `_Column.width` sets only `w:gridCol/@w:w`, every `tcW` keeps the even split, so Word keeps the old widths; header formatting not mentioned |
| docx-imgrep-01 | image-replace, docx-model | 3 | 3 | 3 | 1.00 | `rId, image = doc.part.get_or_add_image(...)` correct; repoint this blip, leave wp:extent, rescale on aspect change; `del rels[old]` only when no a:blip uses it — baseline invented API gone |
| docx-imgrep-02 | image-replace | 2 | 2 | 2 | 1.00 | shared part via get_or_add_image dedupe (SHA1 not named); new part + repoint one blip, scan references first |
| docx-imgrep-03 | image-replace, table-locate | 2 | 2 | 2 | 1.00 | header part, own rels, `part.part.get_or_add_image` per part; "anchored" conflated with VML w:pict, and iterating `first_page_header/_element` creates missing header definitions (side effect, noted) |
| docx-imgins-01 | image-insert | 3 | 2 | 2 | 1.00 | alt `new_p._p.addnext(p._p)` is inverted (moves the anchor), noted |
| docx-imgins-02 | image-insert, table-relayout | 2 | 2 | 2 | 1.00 | |
| docx-legacy-01 | legacy-doc | 3 | 2 | 2 | 1.00 | "renaming doesn't help" not stated (minor, same as baseline) |
| docx-legacy-02 | legacy-doc, verify-safety | 2 | 2 | 2 | 1.00 | |
| docx-legacy-03 | legacy-doc | 1 | 2 | 2 | 1.00 | |
| docx-verify-01 | verify-safety | 3 | 2 | 2 | 1.00 | new file + backup, re-open, assert edit; no diff of the unchanged remainder (minor, same as baseline) |
| docx-verify-02 | verify-safety, docx-model | 2 | 2 | 2 | 1.00 | cell-count/gridSpan vs gridCol, schema order, dangling rIds, lenient parser; cell without w:p and duplicate docPr ids not named (minor) |
| docx-verify-03 | verify-safety, image-replace | 2 | 2 | 2 | 1.00 | |

`normalized = min(awarded, full) / full`

## Per-tag subscores

| tag | subscore | n | trust | action |
|-----|---------:|--:|-------|--------|
| docx-model | 1.00 | 7 | ok | non-target |
| table-locate | 1.00 | 6 | ok | non-target |
| row-delete | 1.00 | 4 | ok | **target — PASS** |
| cell-edit | 1.00 | 4 | ok | **target — PASS** |
| table-relayout | 0.93 | 6 | ok | non-target |
| table-insert | 0.86 | 3 | low-n | non-target (single-sample drift, see below) |
| image-replace | 1.00 | 4 | ok | **target — PASS** |
| image-insert | 1.00 | 2 | low-n | non-target |
| legacy-doc | 1.00 | 3 | low-n | non-target |
| verify-safety | 1.00 | 6 | ok | non-target |

`subscore = Σ(normalized×weight) / Σ(weight)` over that tag's questions.

- row-delete: (3×1 + 2×1 + 2×1 + 2×1) / 9 = 9 / 9 = 1.00
- cell-edit: (2×1 [rowdel-03] + 3×1 + 2×1 + 2×1) / 9 = 9 / 9 = 1.00
- image-replace: (3×1 + 2×1 + 2×1 + 2×1 [verify-03]) / 9 = 9 / 9 = 1.00
- table-relayout: (2 + 3 + 3 + 2 + 2×0.5 + 2) / 14 = 13 / 14 = 0.93
- table-insert: (3 + 2 + 2×0.5) / 7 = 6 / 7 = 0.86

## Stack score

```
stack_score = Σ(normalized×weight) / Σ(weight) = (69 − 2×0.5) / 69 = 68 / 69 = 98.6%
```

(Baseline: 65.5 / 69 = 94.9%.)

## Target tags: baseline → with-skill → pass(≥0.9)?

| tag | baseline | with-skill | pass (≥0.9)? |
|-----|---------:|-----------:|--------------|
| row-delete | 0.72 | 1.00 | **PASS** |
| cell-edit | 0.89 | 1.00 | **PASS** |
| image-replace | 0.89 | 1.00 | **PASS** |

All three targets cross 0.9; no target has a remaining wrong or missing claim.
The field-on-open belief is gone everywhere it appeared in the baseline
(rowdel-03, locate-01, cell-02), rowdel-01 carries reflow + update-totals,
cell-02 now keeps number format and alignment, and imgrep-01 uses the real
`get_or_add_image` with the reference-scan before deleting the old rel.

Non-target drift, not a skill failure: table-insert (low-n) 1.00 → 0.86 via
tblins-03's wrong `_Column.width` claim (it sets only gridCol, not tcW). This is
a single-sample regression on a tag the skill never mentions; per
HOW-TO-EVALUATE.md it is not bolted onto this skill. Catch it in a fresh
baseline if it recurs.

## History

- 0.75-era with-skill run (earlier scorecard, same answers path): target row-delete 0.72 → 1.00 PASS; stack 100.0% (69/69).
