---
name: pptx-tuning-mimo-v2.6-flash
description: >
  Corrective pptx-editing guidance for mimo-v2.6-flash: locate tables by header
  text and assert a unique matching row, python-pptx has no row/column add or
  delete API (deepcopy/remove the a:tr), sync the frame and totals, content
  placeholders lack insert_table, drop_rel ignores r:embed, and verify content
  rather than structure.
when_to_use: >
  Load when the provider-stripped model id (see stack-detection.md) resolves
  to exactly `mimo-v2.6-flash` AND the repository contains a PowerPoint deck (`*.pptx` or
  `*.ppt` at the root or up to two directories deep, per meta.yaml detection).
  For any other model or a repo without decks, do not load.
tuned_for: mimo-v2.6-flash
tuned_version: "2.6"
stack: pptx
source_scorecard: ../scores/mimo-v2.6-flash.md
threshold: 0.9
revalidate_when: model_version changes
---
# PowerPoint-editing corrections for mimo-v2.6-flash

<!-- kaizen:digest -->
**PowerPoint edits (python-pptx): find the table and row by content:**
1. Select the table IN CODE by comparing each `has_table` frame's first-row texts to the expected header, recursing into group shapes; never `next(...)`/`tables[0]`. Match the row by exact stripped text and assert exactly one hit.
2. python-pptx has NO `add_row()`, `add_column()`, `rows.remove()` or delete API: add a row with `copy.deepcopy(ref_tr)` + `ref_tr.addnext(new_tr)`, delete with `tbl.remove(tr)`.
3. After any `a:tr` insert/delete: fill new cells through their existing first run, recompute the Total, set `gf.height = sum(r.height for r in table.rows)`, and check the new bottom against `prs.slide_height` and the shapes below.
4. `slide.part.drop_rel(rId)` counts only `r:id` refs, so it ALWAYS drops an image rel (pictures use `a:blip r:embed`). Drop the old image rel yourself only after confirming no `//@r:embed` or `//@r:link` in the part still names it.
5. Only a `TablePlaceholder` has `insert_table()`; a content placeholder does not. Otherwise add the table at the placeholder's left/top/width and remove the empty placeholder element.
6. Verify content, not structure: re-open the new file, assert exact cell texts, order and totals, search every `ppt/slides/slide*.xml` for text that must be gone, and diff the untouched slides against the original.
7. Read `prs.slide_width`/`prs.slide_height`; never assume 16:9 (`Presentation()`'s default template is 4:3).
8. Keep the frame from the locate step (`gf` = the shape whose `has_table` is True) and use `gf.height`/`gf.top`; `Table` has no `_parent` or `_graphicFrame`. Merge markers are ATTRIBUTES of `a:tc` (`<a:tc gridSpan="2" rowSpan="2">`, covered `<a:tc hMerge="1">` / `vMerge="1"`), never child elements of `a:tcPr`, never `"continue"`.
<!-- /kaizen:digest -->

## table-locate: choose by content, prove uniqueness

- A slide (or its groups) can hold several tables; the first `has_table` shape is
  not "the invoice table". Filter `has_table` shapes (recurse into
  `GroupShape.shapes` when `shape.shape_type == MSO_SHAPE_TYPE.GROUP`) and keep
  the one whose first row's texts match the expected header. Assert exactly one
  table matches.
- Match the row on `cell.text_frame.text.strip() == "Gadget D"`, not a substring.
  Collect all hits and assert `len(hits) == 1` before editing; zero or several =
  stop and ask.
- Merges in python-pptx: every `a:tr` has one `a:tc` per `a:gridCol`. The origin
  carries `gridSpan`/`rowSpan` (`cell.is_merge_origin`, `span_width`), covered
  cells carry `hMerge`/`vMerge` (`cell.is_spanned`) and are separate `_Cell`
  objects. Text written to a covered cell lands in that hidden cell; write to the
  origin. `cell.merge()` / `cell.split()` exist.

## pptx-model: placeholders, slide size, what PowerPoint rejects

- `insert_table(rows, cols)` exists only on `TablePlaceholder`. The content
  placeholder of a "Title and Content" layout (type OBJECT) is a
  `SlidePlaceholder` with no `insert_table`. For it: read the placeholder's
  `left`, `top`, `width`, `add_table` there, then remove the empty placeholder
  (`ph._element.getparent().remove(ph._element)`) so no "Click to add text" box
  remains.
- Positions and sizes are EMU. Take the slide size from `prs.slide_width` /
  `prs.slide_height`; decks differ (16:9 is 12192000 × 6858000, while
  `Presentation()`'s default template is 4:3, 9144000 × 6858000).
- Banding flags (`firstRow`, `bandRow`, …) live on `a:tblPr`
  (`table.first_row`, `table.horz_banding`), not on `a:tr`.
- python-pptx saves malformed tables without complaint. PowerPoint's "needs
  repair" comes from: a row whose `a:tc` count differs from the `a:gridCol`
  count, an `a:tc` without `a:txBody`, `gridSpan`/`hMerge` that don't add up,
  elements out of schema order, duplicate `p:cNvPr` ids (a deep-copied shape
  keeps its id), and an `r:embed` pointing at a dropped relationship.

## row-delete: there is no row API

- python-pptx 1.0.2 has no `add_row`, `add_column`, `insert`, `remove` or
  `delete` on `Table`, `table.rows` or `table.columns`. Delete with
  `tbl = table._tbl; tbl.remove(tbl.tr_lst[i])`; the rows below move up.
- Then set the frame height to the sum of the row heights and recompute any
  Total that included the row.
- Hiding a row (white text, white box, near-zero height) is not deleting: the
  text stays in the slide XML and extractable, and PowerPoint grows a row to fit
  its text. Remove the `a:tr`, then check the removed text is absent from every
  slide XML part.

## table-relayout: insert a row by copying one

- Add a row by `new_tr = copy.deepcopy(ref_tr)` of a body row and
  `ref_tr.addnext(new_tr)` (or `total_tr.addprevious(new_tr)`) at the exact
  position. Never append and move.
- Fill the copy through each cell's existing first run
  (`cell.text_frame.paragraphs[0].runs[0].text = v`); `cell.text = v` rebuilds
  the paragraph and drops the run's `a:rPr` formatting.
- Recompute the Total row. XML inserts and removes leave the frame's `cy` at the
  old value (only the `row.height` setter updates it): set
  `gf.height = sum(r.height for r in table.rows)`.
- Check `gf.top + gf.height <= prs.slide_height` and that the frame does not now
  overlap shapes below it; move them or ask. Re-alternate explicit banding fills.

## image-replace: drop the old rel yourself, correctly

- After swapping the picture (old geometry, `old_el.addnext(new_el)` for
  z-order, remove the old `p:pic`), the old image relationship is still on the
  slide part. Drop it when nothing else uses it.
- `part.drop_rel(rId)` drops when its reference count is < 2, but it counts only
  `//@r:id`. Pictures reference images through `a:blip/@r:embed`, so an image
  rel always counts 0 and is always dropped. python-pptx gives an identical
  image the same rId on a slide, so another picture can still depend on it.
- Before dropping: count `part._element.xpath("//@r:embed | //@r:link")` values
  equal to the rId; drop only when the count is 0.

## verify-safety: assert content, not just well-formedness

- Save to a new file; keep the original untouched.
- Re-open the output with python-pptx and assert the intended change exactly:
  every cell text of the edited table in order, the Total, frame geometry within
  the slide. Zip/XML well-formedness and shape counts are not enough.
- Search every `ppt/slides/slide*.xml` part for text that should be gone.
- Compare every other slide's XML (or text and shape geometry) with the
  original to prove it is unchanged. Render for a visual check when possible.
