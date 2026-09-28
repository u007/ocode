---
model_id: mimo-v2.6-flash
model_version: "2.6"
evaluated_via: opencode-go
evaluated_on: 2026-09-28
stack: pptx
stack_corpus_rev: 1
threshold: 0.9
---

# Scorecard — mimo-v2.6-flash on pptx

> Valid ONLY for `mimo-v2.6-flash` @ `2.6`. A version bump invalidates
> this scorecard — re-benchmark.

Answers: `pptx/answers/mimo-v2.6-flash.md` (closed-book, audited zero tool calls).
Graded against `pptx/questions.yaml` corpus_rev 1 (python-pptx facts verified on 1.0.2).
Grading rule applied: a `point` whose core concept is present earns full credit
(minor missing sub-items are noted). A `point` stated with a wrong python-pptx fact
(invented `add_row()`/`add_column()`/`rows.remove()`, a wrong claim about `drop_rel`,
and so on), or satisfied only by a cover-up, earns 0. A point that is half present
earns 0.5.

## Per-question results

| id | tags | weight | full | awarded | normalized | notes |
|----|------|-------:|-----:|--------:|-----------:|-------|
| pptx-model-01 | pptx-model | 3 | 2 | 2 | 1.00 | briefly says banding flags are "on `<a:tr>`" before correcting to `a:tblPr` (minor) |
| pptx-model-02 | pptx-model | 2 | 2 | 1.5 | 0.75 | EMU factors and 16:9 size are correct; never says to read `prs.slide_width/height` instead of assuming the size |
| pptx-model-03 | pptx-model, image-replace | 2 | 2 | 2 | 1.00 | |
| pptx-locate-01 | table-locate | 3 | 2 | 1 | 0.50 | `has_table → .table` is correct, but no recursion into groups (minor). Uniqueness point missed: takes the first table with `next(...)` and does not pick it by its header; `break`s on the first "Gadget D" row with no assert that exactly one row matches |
| pptx-locate-02 | table-locate, pptx-model | 2 | 2 | 2 | 1.00 | |
| pptx-rowdel-01 | row-delete | 3 | 2 | 1 | 0.50 | point 1 = 0, wrong API: "`Table.rows` … supports … `add_row()`/`add_column()` (append at the end only)". python-pptx 1.0.2 has no add API for rows or columns. The `tbl.remove(tr)` technique itself is right. Frame-height fix is present; totals are not mentioned (minor) |
| pptx-rowdel-02 | row-delete, table-relayout | 3 | 2 | 2 | 1.00 | stale `cy`, fixed by setting it to the sum of `a:tr/@h` (`shape.height`). Downplays the problem: "It doesn't break the file … PowerPoint … render/shrink the table to fit its rows" |
| pptx-rowdel-03 | row-delete, cell-edit | 2 | 2 | 2 | 1.00 | |
| pptx-rowdel-04 | row-delete, verify-safety | 2 | 2 | 1.5 | 0.75 | text stays extractable (point 1). Says the row "can reappear/affect layout" but not that PowerPoint grows the row to fit its text (minor). Point 2 is half present: says to remove the `a:tr`, but never verifies that the old text is absent from every slide part |
| pptx-cell-01 | cell-edit | 3 | 2 | 2 | 1.00 | |
| pptx-cell-02 | cell-edit, table-relayout | 2 | 2 | 2 | 1.00 | |
| pptx-cell-03 | cell-edit | 2 | 2 | 2 | 1.00 | does not mention keeping each cell's number format (minor) |
| pptx-relayout-01 | table-relayout | 3 | 3 | 0.5 | 0.17 | point 1 = 0, invented API: "`table.add_row()` appends at the end", and the procedure is built on it (add_row, then move the `a:tr` with `addprevious`). The right approach is to `deepcopy` a body `a:tr`. Point 2 = 0.5: the frame height is updated but the Total is never recomputed. Point 3 = 0: no check of the frame's new bottom against the slide or shapes below (the banding re-check is present) |
| pptx-relayout-02 | table-relayout | 3 | 3 | 3 | 1.00 | |
| pptx-tblins-01 | table-insert | 3 | 2 | 2 | 1.00 | the free-space search does not clamp explicitly to the slide size (minor) |
| pptx-tblins-02 | table-insert | 2 | 2 | 2 | 1.00 | |
| pptx-tblins-03 | table-insert, pptx-model | 1 | 2 | 1 | 0.50 | `insert_table` with the placeholder geometry is correct. Wrong: "a content placeholder (PLACEHOLDER object type) exposes insert_table()". Only a `TablePlaceholder` has it, so the fallback (add the table at the placeholder bbox, then remove the empty placeholder) is missing |
| pptx-imgrep-01 | image-replace | 3 | 3 | 2 | 0.67 | geometry and aspect are handled, and z-order is kept with `addprevious` before removing the old pic. The unused image rel is never dropped |
| pptx-imgrep-02 | image-replace | 2 | 2 | 2 | 1.00 | |
| pptx-imgrep-03 | image-replace, verify-safety | 2 | 2 | 1 | 0.50 | point 1 = 0. Wrong mechanism: "drop_rel(rId) … does not reference-count usage"; blames the shared rId. Actually `drop_rel` does ref-count, but it counts only `r:id` attributes, so the `a:blip r:embed` users are invisible to it and image rels are always dropped. Point 2 is correct: counts `a:blip r:embed` users first. `r:link` is not checked (minor) |
| pptx-imgins-01 | image-insert | 3 | 2 | 2 | 1.00 | |
| pptx-imgins-02 | image-insert | 2 | 2 | 2 | 1.00 | |
| pptx-legacy-01 | legacy-ppt | 3 | 2 | 2 | 1.00 | says "binary", not OLE2 (minor). Delivers the .pptx and never mentions converting back to .ppt if the user needs it (minor) |
| pptx-legacy-02 | legacy-ppt, verify-safety | 2 | 2 | 2 | 1.00 | |
| pptx-verify-01 | verify-safety | 3 | 2 | 1.5 | 0.75 | new output file and original untouched (point 1). Point 2 is half present: re-opens the file, but only as a "smoke test" plus zip/XML well-formedness and slide/shape counts, followed by a render. Never asserts the exact cell texts and totals, never searches for text that should be gone, never diffs the other slides |
| pptx-verify-02 | verify-safety, pptx-model | 2 | 2 | 1 | 0.50 | point 1 missed: no mention of an `a:tc` count ≠ `gridCol` count in a row, an `a:tc` without `a:txBody`, or broken `gridSpan`/`hMerge`. Only "a tr without any tc" and "zero gridCol". Point 2 is correct: duplicate cNvPr ids, dangling r:embed, schema order |
| pptx-verify-03 | verify-safety, image-replace | 2 | 2 | 2 | 1.00 | no hash comparison of the media (minor) |

`normalized = min(awarded, full) / full`

## Per-tag subscores

| tag | subscore | n | trust | action |
|-----|---------:|--:|-------|--------|
| pptx-model | 0.83 | 6 | ok | **derive** |
| table-locate | 0.70 | 2 | low-n | **derive** (mark low-n) |
| row-delete | 0.80 | 4 | ok | **derive** |
| cell-edit | 1.00 | 4 | ok | omit (strong) |
| table-relayout | 0.77 | 4 | ok | **derive** |
| table-insert | 0.92 | 3 | low-n | omit |
| image-replace | 0.82 | 5 | ok | **derive** |
| image-insert | 1.00 | 2 | low-n | omit (strong) |
| legacy-ppt | 1.00 | 2 | low-n | omit (strong) |
| verify-safety | 0.75 | 6 | ok | **derive** |

`subscore = Σ(normalized×weight) / Σ(weight)` over that tag's questions.

Sums (Σ normalized×weight / Σ weight): pptx-model 10.0/12, table-locate 3.5/5,
row-delete 8.0/10, cell-edit 9.0/9, table-relayout 8.5/11, table-insert 5.5/6,
image-replace 9.0/11, image-insert 5/5, legacy-ppt 5/5, verify-safety 9.75/13.

## Stack score

```
stack_score = Σ(normalized×weight) / Σ(weight) = 54.25 / 65 = 83.5%
```

## Derivation targets

Tags below threshold (`< 0.9`): **pptx-model (0.83), table-locate (0.70, low-n),
row-delete (0.80), table-relayout (0.77), image-replace (0.82), verify-safety
(0.75)** → feed into `derived/pptx.mimo-v2.6-flash.SKILL.md`.

### pptx-model (0.83)
- **Placeholder API (tblins-03).** Claims "a content placeholder (PLACEHOLDER
  object type) exposes insert_table()". Only a `TablePlaceholder` has it; the
  body/content placeholder of a "Title and Content" layout is a
  `SlidePlaceholder` without it. The fallback (add the table at the
  placeholder's left/top/width, then remove the empty placeholder element) is
  missing.
- **Slide size assumed (model-02).** Gives the 16:9 EMU size but never says to
  read `prs.slide_width` / `prs.slide_height` instead of assuming it
  (`Presentation()`'s default template is 4:3, 9144000 × 6858000).
- **Table repair causes (verify-02).** Does not name a row whose `a:tc` count
  differs from the `a:gridCol` count, an `a:tc` without `a:txBody`, or broken
  `gridSpan`/`hMerge`, and does not say python-pptx saves such tables without
  validating them.
- Briefly places the banding flags "on `<a:tr>`" before correcting to `a:tblPr`
  (minor, model-01).

### table-locate (0.70, low-n)
- Picks the table positionally: `table = next(sh.table for sh in slide.shapes if sh.has_table)`.
  It takes the first table on the slide and never identifies the right one by its
  header or cell text.
- Takes the first matching row (`break` on the first `row.cells[0].text.strip() == "Gadget D"`)
  and never asserts that exactly one row matches, so a duplicate label is edited silently.
- Does not recurse into group shapes (`p:grpSp`) when looking for `has_table` frames (minor).

### row-delete (0.80)
- **Invented row/column add API (rowdel-01).** States "`Table.rows` … supports
  … `add_row()`/`add_column()` (append at the end only)". python-pptx 1.0.2
  has no add, insert, remove or delete API on `Table`, `table.rows` or
  `table.columns`. The `tbl.remove(tr)` technique itself is right.
- Totals are not mentioned after a row delete (minor, rowdel-01).
- **Cover-up rows (rowdel-04).** Does not say PowerPoint grows a near-zero row
  to fit its text, and never verifies that the removed row's text is absent
  from every slide XML part.
- Downplays the stale frame height: "It doesn't break the file … PowerPoint …
  render/shrink the table to fit its rows" (rowdel-02, fix itself correct).

### table-relayout (0.77)
- **Add-row built on the invented `table.add_row()` (relayout-01).** The
  procedure appends with `add_row()` and then moves the `a:tr`. The right way
  is to `copy.deepcopy` an existing body `a:tr` and insert it with
  `ref_tr.addnext(new_tr)` before the Total, then fill each cell through its
  existing first run to keep the formatting.
- After the insert, the Total is not recomputed (the frame height is).
- No check of the frame's new bottom against `prs.slide_height` or the shapes
  below it (the banding re-check is present).

### image-replace (0.82)
- **Unused image rel never dropped (imgrep-01).** Replaces the `p:pic` with
  correct geometry and z-order but leaves the old image relationship in place.
- **Wrong `drop_rel` mechanism (imgrep-03).** Says "drop_rel(rId) … does not
  reference-count usage" and blames the shared rId. In fact `drop_rel` drops
  when its reference count is < 2, but the count reads only `//@r:id`; pictures
  use `a:blip/@r:embed`, so an image rel always counts 0 and is always dropped,
  even when another picture (python-pptx reuses one rId for an identical image)
  still uses it. The proposed `r:embed` usage check is correct; `r:link` is not
  checked (minor).

### verify-safety (0.75)
- **Structural-only verification (verify-01).** Re-opens the output as a "smoke
  test" with zip/XML well-formedness, slide/shape counts and a render. Never
  asserts the exact cell texts and their order and totals, never searches all
  slide parts for text that should be gone, never diffs the unchanged slides
  against the original.
- **rowdel-04:** no absence check of the removed text across every slide part.
- **verify-02:** misses the table-level repair causes (`a:tc` count ≠
  `a:gridCol` count, `a:tc` without `a:txBody`, broken spans).
- **imgrep-03:** `r:link` users not checked before dropping a rel (minor).

## Live behavioural probe — editing a real .pptx (2026-09-28)

The model is driven with `ocode run -yolo` in a fresh git repo, one task per run, and the output is checked by `docs/okf/pptx/probe/check.py`. "not run" means that model had no skill for that phase, or its provider was out of credit. The 0.9 runs used a binary built before two late skill edits (pdf `new_page(pno + 1)`, mimo pptx iteration 2).

| task | baseline | 0.75-era skill | 0.9 skill |
|---|---|---|---|
| delete-row | PASS (915s) | PASS (814s) | PASS (965s) |
| edit-cell | PASS (281s) | PASS (145s) | PASS (185s) |
| add-row | PASS (1066s) | PASS (641s) | PASS (1135s) |
| add-column | PASS (373s) | PASS (906s) | PASS (2163s) |
| insert-table | PASS (581s) | PASS (499s) | PASS (581s) |
| rename-item | PASS (215s) | PASS (255s) | PASS (440s) |
| replace-image | PASS (173s) | PASS (327s) | PASS (492s) |
| insert-image | PASS (175s) | PASS (544s) | PASS (244s) |
