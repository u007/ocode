---
model_id: mimo-v2.6-flash
model_version: "2.6"
evaluated_via: opencode-go
evaluated_on: 2026-09-28
stack: pptx
stack_corpus_rev: 1
threshold: 0.9
validation: with-skill
---

# Scorecard — mimo-v2.6-flash on pptx (WITH derived skill, threshold 0.9, iteration 2)

> Valid ONLY for `mimo-v2.6-flash` @ `2.6`. A version bump invalidates
> this scorecard — re-benchmark.

Answers: `pptx/answers/mimo-v2.6-flash.with-skill.md`, regenerated closed-book
with the revised `derived/pptx.mimo-v2.6-flash.SKILL.md` (after iteration 1
failed) prepended as active guidance, audited zero tool calls. Graded against
`pptx/questions.yaml` corpus_rev 1 with the same per-point strictness as the
baseline `mimo-v2.6-flash.md` and iteration 1. Disputed API facts were checked
on python-pptx 1.0.2.

Grading rule applied: a `point` whose core concept is present earns full credit
(minor missing sub-items are noted). Points are whole; fractions come only from a
listed `partial`. A `point` stated with a wrong python-pptx or XML fact that the
point relies on, or satisfied only by a cover-up, earns 0. A wrong fact outside
the point's own content, or in an auxiliary line with a stated alternative, is
noted as minor. pptx-locate-01 point 2 requires BOTH picking the table by header
content (not `tables[0]` or the first `has_table`) AND asserting exactly one
matching row.

Facts verified on 1.0.2 for this grade: `Table` has no `_parent` (private
attribute is `_graphic_frame`); `cell.merge()` writes `<a:tc gridSpan="2">` /
`<a:tc hMerge="1">` attributes; `_RowCollection` has no `.index()`
(AttributeError); `_Row._tr`, `tbl.tblPr`, `is_merge_origin`, `span_width`,
`span_height`, `split`, `is_spanned` exist; `add_picture(path, left=0, top=0,
width=…)` works and keeps aspect; after `drop_rel` an unreachable media part is
not written on save.

## Per-question results

| id | tags | weight | full | awarded | normalized | notes |
|----|------|-------:|-----:|--------:|-----------:|-------|
| pptx-model-01 | pptx-model | 3 | 2 | 2 | 1.00 | graphicFrame → graphic/graphicData → a:tbl; gridCol w, tr h, one tc per gridCol, txBody/tcPr; gridSpan "on a:tc not a:tcPr". Minor wrong detail: tblPr contents are "Attributes, not child elements" (`a:tableStyleId` is a child element) |
| pptx-model-02 | pptx-model | 2 | 2 | 2 | 1.00 | 914400/in, 12700/pt; 16:9 = 12192000 × 6858000; "Never assume 16:9; read prs.slide_width / prs.slide_height", default template 4:3. Minor garbled repeat of the 16:9 size |
| pptx-model-03 | pptx-model, image-replace | 2 | 2 | 2 | 1.00 | layout/master, not in slide.shapes; global scope, detach per slide |
| pptx-locate-01 | table-locate | 3 | 2 | 2 | 1.00 | point 1: `has_table`, recursion into groups. Point 2: BOTH halves — header row texts matched and "Assert exactly one table matches"; rows by exact stripped equality, `assert len(hits) == 1`. Minor: `idx = tbl.rows.index(row)` raises AttributeError in 1.0.2 (auxiliary, "or track i while iterating" given; same class as iteration 1's minor `list(table.rows).index`) |
| pptx-locate-02 | table-locate, pptx-model | 2 | 2 | 2 | 1.00 | **fixed.** Merges are "ATTRIBUTES on the cell element itself": `<a:tc gridSpan="2">`, covered slots `hMerge="1"` / `vMerge="1"`, one tc per gridCol; "never children of a:tcPr, and never … continue". Point 2: text only on origin; on column insert/delete bump gridSpan and add/remove a covered `a:tc hMerge="1"`. python-pptx merge API names all real |
| pptx-rowdel-01 | row-delete | 3 | 2 | 2 | 1.00 | no add/insert/remove/delete API; `tbl.remove(tr)` via `table.rows[i]._tr` / `tbl.tr_lst[i]`; Total, frame height, banding, absence check |
| pptx-rowdel-02 | row-delete, table-relayout | 3 | 2 | 2 | 1.00 | **fixed.** Stale `cy` vs row sum; fix `gf.height = sum(r.height for r in table.rows)` on the frame shape, no `_parent`/`_graphicFrame` back-reference (locate-01 says keep the has_table shape because "Table has no _parent"); bottom/overlap re-check. Minor: `gf` not bound in this snippet; unverified "PowerPoint also rewrites cy when it saves" |
| pptx-rowdel-03 | row-delete, cell-edit | 2 | 2 | 2 | 1.00 | |
| pptx-rowdel-04 | row-delete, verify-safety | 2 | 2 | 2 | 1.00 | text extractable, rows auto-grow; remove a:tr, verify absence in every `ppt/slides/slide*.xml` |
| pptx-cell-01 | cell-edit | 3 | 2 | 2 | 1.00 | |
| pptx-cell-02 | cell-edit, table-relayout | 2 | 2 | 2 | 1.00 | h is a minimum → row grows → overflow/overlap; widen column/shrink others, smaller font; check bottom vs slide and shapes below |
| pptx-cell-03 | cell-edit | 2 | 2 | 2 | 1.00 | number format not mentioned (minor, as baseline) |
| pptx-relayout-01 | table-relayout | 3 | 3 | 3 | 1.00 | `deepcopy` + `ref_tr.addnext`, "never append-and-move"; fill via `runs[0].text`; Total, `gf.height` = row sum; bottom vs `prs.slide_height`, shapes below, banding. Minor: `i` and `gf` unbound, unused `qn` import |
| pptx-relayout-02 | table-relayout | 3 | 3 | 3 | 1.00 | gridCol + one tc per row at k+1, gridSpan bump + covered `<a:tc hMerge="1">`; deep-copied cells keep tcPr/rPr; scale widths to `prs.slide_width - gf.left - margin`, set `gf.width` |
| pptx-tblins-01 | table-insert | 3 | 2 | 2 | 1.00 | shape bboxes within slide size; `add_table(3, 2, left, top, width, height)` → `.table` |
| pptx-tblins-02 | table-insert | 2 | 2 | 2 | 1.00 | `tblPr.find(qn('a:tableStyleId'))` verified |
| pptx-tblins-03 | table-insert, pptx-model | 1 | 2 | 2 | 1.00 | only `TablePlaceholder` has `insert_table`; OBJECT → `SlidePlaceholder` fallback: add at bbox, remove placeholder element |
| pptx-imgrep-01 | image-replace | 3 | 3 | 3 | 1.00 | old geometry (aspect option), `addnext` + remove, drop rel only after `//@r:embed \| //@r:link` count is 0 |
| pptx-imgrep-02 | image-replace | 2 | 2 | 2 | 1.00 | minor unverified "hash key" aside |
| pptx-imgrep-03 | image-replace, verify-safety | 2 | 2 | 2 | 1.00 | drop_rel counts only `r:id`; blips use `r:embed` → always dropped; check `r:embed`/`r:link` first |
| pptx-imgins-01 | image-insert | 3 | 2 | 2 | 1.00 | `add_picture(..., left=0, top=0, width=Inches(2))` valid (iteration 1's `None` TypeError gone); height follows aspect; bottom-right from slide edges; overlap check. "both = stretch" not stated (minor, as iteration 1) |
| pptx-imgins-02 | image-insert | 2 | 2 | 2 | 1.00 | crops to fill aspect. Point 2 kept: "add the picture as a normal shape instead of insert_picture()" is a correct fix (`add_picture` without both dimensions keeps aspect), though one-dimension sizing is not spelled out. Wrong alternative alongside it: reset crops to 0 and the picture "will then not fill the frame" (it stretches to the frame unless resized). Not zeroed, because the point's core claim (a working fix) is not itself the wrong fact; if zeroed, only the non-target image-insert tag moves (0.80) and the stack reads 64/65 = 98.5% |
| pptx-legacy-01 | legacy-ppt | 3 | 2 | 2 | 1.00 | Compound File (OLE); soffice convert, edit, re-export if needed |
| pptx-legacy-02 | legacy-ppt, verify-safety | 2 | 2 | 2 | 1.00 | |
| pptx-verify-01 | verify-safety | 3 | 2 | 2 | 1.00 | new file; exact cells/order/Total, absence search in every slide part, diff untouched slides, render |
| pptx-verify-02 | verify-safety, pptx-model | 2 | 2 | 2 | 1.00 | tc≠gridCol, tc without txBody, spans (incl. merges as tcPr children); schema order, duplicate cNvPr, dangling rIds. Minor: "a:trPr must precede a:tc" (no `a:trPr` in DrawingML); "python-pptx does not validate" not stated (not required by the baseline's grading) |
| pptx-verify-03 | verify-safety, image-replace | 2 | 2 | 2 | 1.00 | rel + media survive; drop rel everywhere, list `ppt/media`. Minor imprecision: "python-pptx do[es] not garbage-collect unreferenced parts" is true while the rel survives, but once the rel is dropped python-pptx no longer writes the part, so the manual part removal is redundant (not harmful); no hash compare (minor, as baseline) |

`normalized = min(awarded, full) / full`

## Per-tag subscores

| tag | subscore | n | trust | note |
|-----|---------:|--:|-------|------|
| pptx-model | 1.00 | 6 | ok | TARGET — pass |
| table-locate | 1.00 | 2 | low-n | TARGET — pass |
| row-delete | 1.00 | 4 | ok | TARGET — pass |
| cell-edit | 1.00 | 4 | ok | |
| table-relayout | 1.00 | 4 | ok | TARGET — pass |
| table-insert | 1.00 | 3 | low-n | |
| image-replace | 1.00 | 5 | ok | TARGET — pass |
| image-insert | 1.00 | 2 | low-n | |
| legacy-ppt | 1.00 | 2 | low-n | |
| verify-safety | 1.00 | 6 | ok | TARGET — pass |

`subscore = Σ(normalized×weight) / Σ(weight)` over that tag's questions.

Sums (Σ normalized×weight / Σ weight): pptx-model 12/12, table-locate 5/5,
row-delete 10/10, cell-edit 9/9, table-relayout 11/11, table-insert 6/6,
image-replace 11/11, image-insert 5/5, legacy-ppt 5/5, verify-safety 13/13.

## Stack score

```
stack_score = Σ(normalized×weight) / Σ(weight) = 65 / 65 = 100.0%
```

(Baseline: 54.25 / 65 = 83.5%. Iteration 1: 61.0 / 65 = 93.8%.)

A full sweep is expected here, not a contamination flag: the answers' distinctive
fixes trace to the skill text itself (`derived/pptx.mimo-v2.6-flash.SKILL.md`
line 32: "`Table` has no `_parent` or `_graphicFrame`", merge markers are
"ATTRIBUTES of `a:tc` … never child elements of `a:tcPr`, never `"continue"`"),
per HOW-TO-EVALUATE "Overlap ≠ cheating", and the answer run was audited at zero
tool calls. Some minors sit inside a point's text (verify-02's `a:trPr` example
of schema order, imgins-02's crop-reset alternative, verify-03's GC aside). They
are not zeroed, by iteration 1's discriminator: a point is zeroed only when its
core claim is itself the wrong fact (iteration 1: merge representation, frame
fetch call, the only insert call).
Sensitivity: zeroing verify-02 point 2 alone gives verify-safety and pptx-model
0.92 (still pass); zeroing imgins-02 point 2 touches only the non-target
image-insert. verify-03's aside is an imprecision, not a wrong fact, and is not a
zeroing candidate.

## Target tags: baseline → iteration 1 → iteration 2 → pass(≥0.9)?

| tag | baseline | iteration 1 | iteration 2 | pass (≥0.9)? |
|-----|---------:|------------:|------------:|:------------:|
| pptx-model | 0.83 | 0.92 | 1.00 | **pass** |
| table-locate (low-n) | 0.70 | 0.80 | 1.00 | **pass** |
| row-delete | 0.80 | 0.85 | 1.00 | **pass** |
| table-relayout | 0.77 | 0.86 | 1.00 | **pass** |
| image-replace | 0.82 | 1.00 | 1.00 | **pass** |
| verify-safety | 0.75 | 1.00 | 1.00 | **pass** |

Validation verdict: 6 of 6 targets pass. No target is below 0.9, so there is no
remaining wrong or missing claim to report. Both iteration-1 failures are fixed:
merges are now `a:tc` attributes (locate-02, relayout-02, and named as a repair
cause in verify-02), and the frame is taken from the `has_table` shape instead of
a guessed `Table` back-reference (rowdel-02, relayout-01).

## History

- Iteration 1 (0.9 threshold, previous skill): stack 61.0/65 = 93.8%; targets
  pptx-model 0.92 pass, table-locate 0.80 fail (locate-02: merge markers written
  as child elements in `a:tcPr`), row-delete 0.85 fail and table-relayout 0.86
  fail (rowdel-02: `table._parent` does not exist), image-replace 1.00 pass,
  verify-safety 1.00 pass; non-target image-insert 0.70 (`add_picture(…, None,
  None, …)` TypeError).
- Earlier with-skill run (0.75 era, iteration 2, previous skill): target
  table-locate 0.70 → 1.00 (pass at 0.75); stack 56.0/65 = 86.2%; rowdel-02
  already lost point 2 to `table._graphicFrame`.
