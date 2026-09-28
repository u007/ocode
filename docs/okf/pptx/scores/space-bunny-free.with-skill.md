---
model_id: space-bunny-free
model_version: "alpha"
evaluated_via: opencode-go
evaluated_on: 2026-09-28
stack: pptx
stack_corpus_rev: 1
threshold: 0.9
validation: with-skill
---

# Scorecard — space-bunny-free on pptx (with-skill validation)

> Valid ONLY for `space-bunny-free` @ `alpha`. A version bump invalidates
> this scorecard — re-benchmark.

> **WITH-SKILL VALIDATION RUN (threshold 0.9).** These answers were produced
> closed-book with the current derived skill
> `../derived/pptx.space-bunny-free.SKILL.md` (`pptx-tuning-space-bunny-free`;
> targets **pptx-model**, **table-locate**, **image-replace**,
> **verify-safety**) prepended to the answerer prompt. Grading is independent
> and held to the same strictness per point as the baseline
> `space-bunny-free.md`. No derived skill is written from this run.

Answers: `pptx/answers/space-bunny-free.with-skill.md` (closed-book + skill, audited zero tool calls).
Graded against `pptx/questions.yaml` corpus_rev 1 (python-pptx facts verified on 1.0.2).
Grading rule applied: rubric points are whole; fractions come only from a listed
`partial`. A `point` whose core concept is present earns full credit (minor
missing sub-items are noted); a `point` stated with a wrong API fact, or
satisfied only by a cover-up, earns 0. Cross-references to another answer count
(as in the baseline). pptx-locate-01 point 2 needs BOTH header/content table
selection (not `tables[0]` / first `has_table`) AND an exactly-one-row assert.

API facts checked on python-pptx 1.0.2 for this grade: `Table` has no
`add_row`; `_Cell` has no `width` property (assigning `cell.width` sets a plain
Python attribute and writes nothing); DrawingML `a:tcPr` has no `a:tcW`
(that is WordprocessingML `w:tcW`).

## Per-question results

| id | tags | weight | full | awarded | normalized | notes |
|----|------|-------:|-----:|--------:|-----------:|-------|
| pptx-model-01 | pptx-model | 3 | 2 | 2 | 1.00 | correct `a:txBody` then `a:tcPr` order; point 2 prose correct, but the sample XML shows a 2-gridCol table whose row holds one `a:tc gridSpan="2"` and no `hMerge="1"` sibling (the old omitted-covered-cell picture; residual risk, not a lost point); odd claim that a table "only appears" in a layout content placeholder or on the slide (minor) |
| pptx-model-02 | pptx-model | 2 | 2 | 2 | 1.00 | |
| pptx-model-03 | pptx-model, image-replace | 2 | 2 | 2 | 1.00 | |
| pptx-locate-01 | table-locate | 3 | 2 | 2 | 1.00 | recursive group walk + `has_table`; table picked by full header row with `assert len(tables) == 1`; row by exact stripped text with `assert len(hits) == 1` — both halves of point 2 met |
| pptx-locate-02 | table-locate, pptx-model | 2 | 2 | 2 | 1.00 | one `a:tc` per `a:gridCol` incl. covered cells; "the n-th `a:tc` of a row is grid column n", `row.cells[c]` ≡ `table.cell(r, c)`; origin gridSpan/rowSpan, covered hMerge/vMerge; write to origin, fix spans on row/column insert/delete |
| pptx-rowdel-01 | row-delete | 3 | 2 | 2 | 1.00 | `tbl.remove(tr)`; frame height via rowdel-02; totals not named (minor, as baseline). Side claim "`Table` gives you `add_row()`" is wrong (no such method; minor, not the point's statement) |
| pptx-rowdel-02 | row-delete, table-relayout | 3 | 2 | 2 | 1.00 | overlap-check snippet uses `other is frame` (inverted; minor) |
| pptx-rowdel-03 | row-delete, cell-edit | 2 | 2 | 1 | 0.50 | point 1 met only by the closing summary (style parity recomputed from row order, explicit fills baked on rows); the opening wrongly blames style banding for the two same-coloured rows. Point 2 0: no fix given (no re-apply fills to the following rows, no switch to style banding; only "re-banding deliberately" in a garbled sentence). The baseline answer had both |
| pptx-rowdel-04 | row-delete, verify-safety | 2 | 2 | 2 | 1.00 | now includes "old text absent from *all* `ppt/slides/slideN.xml`"; `w:t` typo (minor) |
| pptx-cell-01 | cell-edit | 3 | 2 | 2 | 1.00 | |
| pptx-cell-02 | cell-edit, table-relayout | 2 | 2 | 1 | 0.50 | point 1 0: states the row grows, but also "the row does not auto-grow at all in some cases (a fixed `a:tr/@h`) … text is clipped" — wrong, `h` is a minimum; no run-off-the-slide / overlap consequence. Point 2 met via smaller font / `table.columns[i].width` / shorten; but its width recipe also writes a non-existent `a:tcPr/a:tcW` and `cell.width`, and there is no bottom-vs-slide check (right edge only). The baseline answer had both points cleanly |
| pptx-cell-03 | cell-edit | 2 | 2 | 2 | 1.00 | notes not named; `w:t` typo (minor) |
| pptx-relayout-01 | table-relayout | 3 | 3 | 3 | 1.00 | deepcopy body `a:tr` + `addprevious` on the Total row; fill via first run; frame height; `assert bottom <= slide_height` + banding parity. Total not updated; opening claims `table.add_row()` exists (minor) |
| pptx-relayout-02 | table-relayout | 3 | 3 | 2 | 0.67 | point 1 met (gridCol after Item; one `a:tc` per row at the grid index; merged Total: gridSpan+1 and an `hMerge="1"` `a:tc`; per-row count assert). Point 2 met (deepcopy donor cells; header-specific donor not named, minor). Point 3 0 (wrong API facts): the width rebalance mandates writing each cell's `a:tcPr/a:tcW/@w` (element does not exist in DrawingML; inserting it breaks the schema) and says `table.columns[i].width` plus `cell.width` "does both `a:gridCol` and `a:tcW`" (`_Cell` has no `width`) |
| pptx-tblins-01 | table-insert | 3 | 2 | 2 | 1.00 | |
| pptx-tblins-02 | table-insert | 2 | 2 | 2 | 1.00 | |
| pptx-tblins-03 | table-insert, pptx-model | 1 | 2 | 2 | 1.00 | `insert_table` only on `TablePlaceholder`, result still a placeholder (`PlaceholderGraphicFrame`); content placeholder is a `SlidePlaceholder` → AttributeError → `add_table` at `ph.left/top/width/height` + remove the placeholder element |
| pptx-imgrep-01 | image-replace | 3 | 3 | 3 | 1.00 | old geometry (aspect note), z-order restored by re-inserting at the old spTree index, old `p:pic` removed; drop point met by its pointer "see pptx-imgrep-03 before doing this" (the sample itself calls `drop_rel` unguarded, and `slide = pic.part` is wrong; minor) |
| pptx-imgrep-02 | image-replace | 2 | 2 | 2 | 1.00 | fallback "copy the bytes to a new part via `get_or_add_image_part`" would dedupe to the same part (minor; main fix is a new picture) |
| pptx-imgrep-03 | image-replace, verify-safety | 2 | 2 | 2 | 1.00 | `_rel_ref_count` counts only `//@r:id`, `a:blip/@r:embed` counts 0, `< 2` → always dropped; same image on one slide shares one rId; own `//@r:embed \| //@r:link` check before dropping |
| pptx-imgins-01 | image-insert | 3 | 2 | 2 | 1.00 | |
| pptx-imgins-02 | image-insert | 2 | 2 | 2 | 1.00 | "picture shape is bigger than the frame … negative crop" is wrong (shape keeps placeholder size, crop is positive; minor); zero-crop and add_picture(+"scale to contain") options given |
| pptx-legacy-01 | legacy-ppt | 3 | 2 | 2 | 1.00 | |
| pptx-legacy-02 | legacy-ppt, verify-safety | 2 | 2 | 2 | 1.00 | |
| pptx-verify-01 | verify-safety | 3 | 2 | 2 | 1.00 | work on a copy, keep the original; re-open; text absent from all slide parts, unique cNvPr, dangling rIds, media hash, render. Missing `import re` (minor) |
| pptx-verify-02 | verify-safety, pptx-model | 2 | 2 | 2 | 1.00 | tc/gridCol mismatch, broken spans, duplicate cNvPr, dangling rIds, schema order, "python-pptx will save all of these silently". Wrong side claims: "`a:tcPr` must be first, then `a:txBody`" (reversed; contradicts its own model-01), schema-order example uses Word `a:shd`/`a:tcW`, `mc:Ignorable w14` Word/Excel aside (minor; each point met by correct items) |
| pptx-verify-03 | verify-safety, image-replace | 2 | 2 | 2 | 1.00 | now hash-compares saved `ppt/media/*` |

`normalized = min(awarded, full) / full`

`full` = Σ point scores (partials excluded), so imgrep-03 and verify-02 are 2, not 3.
The baseline scorecard lists them as 3; with full = 2 its imgrep-03 would be 0.50
(not 0.33), baseline image-replace 0.909 and verify-safety 0.923. Flagged only;
the baseline is not edited here.

Sensitivity: zeroing imgrep-01 point 3 (unguarded `drop_rel` in the sample) gives
image-replace 0.909; zeroing verify-02 point 1 (reversed `tcPr`/`txBody` order)
gives verify-safety 0.923 and pptx-model 0.917. Neither changes a pass.

## Per-tag subscores

| tag | subscore | n | trust | status |
|-----|---------:|--:|-------|--------|
| pptx-model | 1.00 | 6 | ok | target — pass |
| table-locate | 1.00 | 2 | low-n | target — pass |
| row-delete | 0.90 | 4 | ok | non-target |
| cell-edit | 0.78 | 4 | ok | non-target (below 0.9) |
| table-relayout | 0.82 | 4 | ok | non-target (below 0.9) |
| table-insert | 1.00 | 3 | low-n | non-target |
| image-replace | 1.00 | 5 | ok | not a target after recompute |
| image-insert | 1.00 | 2 | low-n | non-target |
| legacy-ppt | 1.00 | 2 | low-n | non-target |
| verify-safety | 1.00 | 6 | ok | not a target after recompute |

`subscore = Σ(normalized×weight) / Σ(weight)` over that tag's questions.

- pptx-model: (3+2+2+2+1+2) / 12 = 12 / 12 = 1.00
- table-locate: (3+2) / 5 = 5 / 5 = 1.00
- row-delete: (3+3+1.0+2) / 10 = 9 / 10 = 0.90
- cell-edit: (1.0+3+1.0+2) / 9 = 7 / 9 = 0.778
- table-relayout: (3+1.0+3+2.0) / 11 = 9 / 11 = 0.818
- table-insert: (3+2+1) / 6 = 6 / 6 = 1.00
- image-replace: (2+3+2+2+2) / 11 = 11 / 11 = 1.00
- verify-safety: (2+2+2+3+2+2) / 13 = 13 / 13 = 1.00

## Stack score

```
stack_score = Σ(normalized×weight) / Σ(weight) = 62 / 65 = 95.4%
```

(Lost weight: rowdel-03 1.0, cell-02 1.0, relayout-02 1.0.) Baseline was
61.0 / 65 = 93.8% (recomputed: pptx-imgrep-03 and pptx-verify-02 have full = 2).

## Target tags: baseline → with-skill → pass(≥0.9)?

| tag | baseline | with-skill | pass (≥0.9)? |
|-----|---------:|-----------:|--------------|
| pptx-model | 0.88 | 1.00 | pass |
| table-locate (low-n) | 0.50 | 1.00 | pass |
| image-replace | 0.91 (recomputed; not a target) | 1.00 | — |
| verify-safety | 0.92 (recomputed; not a target) | 1.00 | — |

Every target tag reaches 0.9; no target has a remaining wrong or missing claim.

- pptx-locate-01 0.50 → 1.00, pptx-locate-02 0.50 → 1.00 (the "a:tc index =
  grid column" belief left over from the 0.75-era run is gone),
  pptx-tblins-03 0.50 → 1.00, pptx-imgrep-03 0.33 → 1.00.

Non-target movement (single-sample variance; not a reason to tweak this skill):
row-delete 1.00 → 0.90, cell-edit 1.00 → 0.78, table-relayout 1.00 → 0.82. The
deductions are regressions absent from the baseline answer: a muddled
banding explanation with no fix (rowdel-03), "fixed `h` can clip" (cell-02),
and WordprocessingML leakage into DrawingML (`a:tcW`, `cell.width`,
`a:shd`) in the width recipes (cell-02, relayout-02). If the Word-namespace
leakage recurs in a fresh baseline, it earns its own section there.

## History

- 0.75-era with-skill run (earlier skill, target table-locate only): table-locate 0.50 → 0.80 (crossed 0.75); stack 91.0%.
