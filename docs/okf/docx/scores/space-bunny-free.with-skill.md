---
model_id: space-bunny-free
model_version: "alpha"
evaluated_via: opencode-go
evaluated_on: 2026-09-28
stack: docx
stack_corpus_rev: 1
threshold: 0.9
validation: with-skill
---

# Scorecard — space-bunny-free on docx (with-skill validation, threshold 0.9)

> Valid ONLY for `space-bunny-free` @ `alpha`. A version bump invalidates
> this scorecard — re-benchmark.

Answers: `docx/answers/space-bunny-free.with-skill.md` (closed-book, with the
current `derived/docx.space-bunny-free.SKILL.md` prepended as active guidance;
audited zero tool calls). This is a validation re-grade, so no new derived skill
is written. Graded against `docx/questions.yaml` corpus_rev 1 (python-docx facts
verified on 1.2.0). Grading rule (same strictness as the baseline
`scores/space-bunny-free.md`): rubric points are whole; fractions come only from
a listed partial. A `point` stated with a wrong API fact, or satisfied only by a
cover-up (hidden/white text, zero-height rows, white boxes), earns 0.

## Per-question results

| id | tags | weight | full | awarded | normalized | notes |
|----|------|-------:|-----:|--------:|-----------:|-------|
| docx-model-01 | docx-model | 3 | 2 | 2 | 1.00 | one tblGrid "immediately after w:tblPr" stated correctly |
| docx-model-02 | docx-model, table-locate | 3 | 2 | 2 | 1.00 | match on concatenated w:t, write into the first covering w:t after asserting equal rPr |
| docx-model-03 | docx-model, table-relayout | 2 | 2 | 2 | 1.00 | twips + EMU correct; px only as a ratio; `Emu(...)` re-wrap of Length subtraction |
| docx-locate-01 | table-locate | 3 | 2 | 2 | 1.00 | header-label scoring; "Require exactly one match; if zero or many, stop" |
| docx-locate-02 | table-locate, docx-model | 2 | 2 | 2 | 1.00 | |
| docx-rowdel-01 | row-delete | 3 | 2 | 2 | 1.00 | reflow not stated here (it is in legacy-01: "shifts the rest up"), no manual shifting proposed; SUM(ABOVE) + recomputed total covered |
| docx-rowdel-02 | row-delete, table-locate | 2 | 2 | 1 | 0.50 | pt1 ok (orphaned continuation). pt2 = 0: the "promote" recipe moves the restart cell's tcPr **minus the vMerge** and its paragraphs into the next cell (never sets `w:vMerge w:val="restart"` on it) and then "remove[s] the now-redundant continuation `w:tc` from the remaining rows", which leaves those rows one grid column short (contradicts its own step 3 grid check). The alternative "unmerge then delete" also dissolves the merge instead of keeping it |
| docx-rowdel-03 | row-delete, cell-edit | 2 | 2 | 2 | 1.00 | updateFields option + recompute-and-write (as a literal with the old rPr). Wrong rationale: "never hand-edit the cached result and leave the field in place — guarantees the document is wrong the moment a field update happens" (after the row is gone, an update yields the same correct sum). Kept at 2 deliberately: the wrong claim is reasoning, not an API fact, both offered recipes leave the correct total displayed, and the 0.75-era run got 2 for the same muddle |
| docx-rowdel-04 | row-delete, verify-safety | 2 | 2 | 2 | 1.00 | |
| docx-cell-01 | cell-edit | 3 | 2 | 2 | 1.00 | |
| docx-cell-02 | cell-edit, table-locate | 2 | 2 | 2 | 1.00 | |
| docx-cell-03 | cell-edit, verify-safety | 2 | 2 | 2 | 1.00 | no explicit "check w:delText no longer holds the old value" (minor, as before); minor wrong: "runs in w:del are visible as ordinary runs" (`paragraph.runs` returns only direct `w:r` children) |
| docx-relayout-01 | table-relayout | 3 | 3 | 3 | 1.00 | now correct: add_row "takes no arguments … giving each new cell a tcW equal to its gridCol's dxa width — and nothing else"; deepcopy + addnext/addprevious; edit existing w:t; flip banding |
| docx-relayout-02 | table-relayout | 3 | 3 | 3 | 1.00 | add_column requires a Length, appends at the far right, sets gridCol + each new tcW, returns `_Column` (verified on 1.2.0). Grid-offset insertion with `gridSpan + 1` for a straddling cell; rebalance gridCol + every tcW; copy header shd/rPr; fixed layout |
| docx-relayout-03 | table-relayout, docx-model | 2 | 2 | 2 | 1.00 | minor wrong (unscored): "`w:tblW w:type="auto"` … Word … ignores the stored widths entirely" contradicts its own tblins-03 (add_table writes tblW auto and the dxa gridCol/tcW are honoured); the tcW point itself is correct |
| docx-tblins-01 | table-insert | 3 | 2 | 2 | 1.00 | "at the very end of the document body, just before the final w:sectPr"; `para._p.addnext(table._tbl)` |
| docx-tblins-02 | table-insert | 2 | 2 | 2 | 1.00 | KeyError from styles.xml, "Table Grid" behaves identically; `new.style = existing.style`, list `doc.styles`, explicit tblBorders in schema position. Minor: "`tblPr.append(borders_el)` at the right index" is self-contradictory wording (append has no index) |
| docx-tblins-03 | table-insert, table-relayout | 2 | 2 | 2 | 1.00 | pt1 correct: tblW auto, every gridCol + every tcW = text_width / cols. pt2: style copy, gridCol + every `cell.width` ("`column.width` only sets the gridCol"), edit the one tblGrid in place, header shd + run formatting, never assign tblPr. Minor: source table not located by content |
| docx-imgrep-01 | image-replace, docx-model | 3 | 3 | 3 | 1.00 | pt1 ok. pt2 now ok: `rId, image = part.get_or_add_image(...)` on the owning part, repoint that blip's r:embed, keep cx, `cy = cx * px_height // px_width` in wp:extent and a:ext. pt3 ok: count `a:blip[@r:embed]` then `drop_rel` ("drop_rel only counts r:id"). Minor: placeholder line `target_drawing.find(qn('a') and ...)` is not valid code; the snippet writes only wp:extent (prose says both) |
| docx-imgrep-02 | image-replace | 2 | 2 | 2 | 1.00 | |
| docx-imgrep-03 | image-replace, table-locate | 2 | 2 | 2 | 1.00 | header part's own `get_or_add_image` + repoint; linked headers noted. Minor: loop repoints every drawing in every header/footer, not only the logo; `if hf is None` never triggers |
| docx-imgins-01 | image-insert | 3 | 2 | 2 | 1.00 | |
| docx-imgins-02 | image-insert, table-relayout | 2 | 2 | 2 | 1.00 | says it overflows only (autofit widening not mentioned; the rubric accepts "or overflows") |
| docx-legacy-01 | legacy-doc | 3 | 2 | 2 | 1.00 | OLE2 + LibreOffice workflow. Minor wrong: names `ValueError … content type is 'application/msword'` first (1.2.0 raises `PackageNotFoundError` on an OLE2 file, verified); "renaming doesn't help" not stated |
| docx-legacy-02 | legacy-doc, verify-safety | 2 | 2 | 2 | 1.00 | |
| docx-legacy-03 | legacy-doc | 1 | 2 | 2 | 1.00 | |
| docx-verify-01 | verify-safety | 3 | 2 | 2 | 1.00 | |
| docx-verify-02 | verify-safety, docx-model | 2 | 2 | 2 | 1.00 | grid-sum mismatch, tblPr schema order, bad rels. Minor wrong: "w:tblGrid must be the first child" (tblPr comes first; model-01 has it right) and "a w:tc with no w:tcPr" (tcPr is optional); no cell-without-w:p, duplicate docPr ids not named |
| docx-verify-03 | verify-safety, image-replace | 2 | 2 | 2 | 1.00 | rels + media check, hash media, count r:embed before drop_rel, shared-part case |

`normalized = min(awarded, full) / full`

## Per-tag subscores

| tag | subscore | n | trust | action |
|-----|---------:|--:|-------|--------|
| docx-model | 1.00 | 7 | ok | target, validated |
| table-locate | 0.93 | 6 | ok | omit |
| row-delete | 0.89 | 4 | ok | omit (non-target) |
| cell-edit | 1.00 | 4 | ok | omit (strong) |
| table-relayout | 1.00 | 6 | ok | target, validated |
| table-insert | 1.00 | 3 | low-n | target, validated |
| image-replace | 1.00 | 4 | ok | target, validated |
| image-insert | 1.00 | 2 | low-n | omit (strong) |
| legacy-doc | 1.00 | 3 | low-n | omit (strong) |
| verify-safety | 1.00 | 6 | ok | omit (strong) |

`subscore = Σ(normalized×weight) / Σ(weight)` over that tag's questions.

- docx-model: (3+3+2+2+2+3+2) / 17 = 17 / 17 = 1.00
- table-locate: (3+3+2+0.5×2+2+2) / 14 = 13 / 14 = 0.93
- row-delete: (3+0.5×2+2+2) / 9 = 8 / 9 = 0.89
- table-relayout: (2+3+3+2+2+2) / 14 = 14 / 14 = 1.00
- table-insert: (3+2+2) / 7 = 7 / 7 = 1.00
- image-replace: (3+2+2+2) / 9 = 9 / 9 = 1.00

## Stack score

```
stack_score = Σ(normalized×weight) / Σ(weight) = 68 / 69 = 98.6%
```

(Σweight = 12×3 + 16×2 + 1×1 = 69. Lost 1 on docx-rowdel-02 only.
Baseline: 64 / 69 = 92.8%.)

## Target tags: baseline → with-skill → pass(≥0.9)?

| tag | baseline | with-skill | n | pass (≥0.9)? |
|-----|---------:|-----------:|--:|--------------|
| docx-model | 0.88 | 1.00 | 7 | pass |
| table-relayout | 0.79 | 1.00 | 6 | pass |
| table-insert | 0.71 | 1.00 | 3 (low-n) | pass |
| image-replace | 0.78 | 1.00 | 4 | pass |

Every target tag reaches 0.9. No target has a remaining wrong or missing claim.

Absorbed corrections: add_table widths (gridCol + tcW = text_width / cols, tblW
auto); add_row takes no arguments and writes only tcW; add_column appends at the
right and returns `_Column`; `_Column.width` sets only the gridCol; style copy by
object; one tblGrid edited in place; grid-offset column insertion with
`gridSpan + 1`; new image part via the owning part's `get_or_add_image` +
repointed `r:embed`, never `_blob`; aspect as a pixel ratio; `drop_rel` only after
counting `r:embed`.

## Non-target movement (LLM resampling, not acted on)

- row-delete 0.89 (baseline 1.00), rowdel-02: the promote recipe drops `vMerge`
  instead of setting `restart` and removes continuation `w:tc` elements from the
  remaining rows. The same question lost a point in the 0.75-era with-skill run
  too (it deleted continuation rows then), so it is a recurring weakness on a
  non-target tag, not a skill regression. Catch it in a fresh baseline before
  deriving anything for it.

## History

- 0.75-era with-skill run (same day, earlier skill): stack 66/69 = 95.7%; target
  table-insert 0.71 → 1.00 (pass); docx-model 0.88, image-replace 0.78 (imgrep-01
  still overwrote `_blob`), table-relayout 1.00 — the last three were not targets
  at 0.75.
