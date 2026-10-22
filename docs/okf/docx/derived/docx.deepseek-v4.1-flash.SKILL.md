---
name: docx-tuning-deepseek-v4.1-flash
description: >
  Corrective Word-editing guidance for deepseek-v4.1-flash: what is left to do
  after removing a table row, which python-docx width setters write which XML,
  how to make a new table match an existing one, and how to replace one picture
  without touching shared image parts or leaving the old image in the package.
when_to_use: >
  Load when the provider-stripped model id (see stack-detection.md) resolves
  to exactly `deepseek-v4.1-flash` AND the repository contains a Word document (`*.docx` or
  `*.doc` at the root or up to two directories deep, per meta.yaml detection).
  For any other model or a repo without Word files, do not load.
tuned_for: deepseek-v4.1-flash
tuned_version: "4.1-2026-10-21"
stack: docx
source_scorecard: ../scores/deepseek-v4.1-flash.md
threshold: 0.9
revalidate_when: model_version changes
---
# Word-editing corrections for deepseek-v4.1-flash

<!-- kaizen:digest -->
**Word edits (python-docx): row removal follow-up, column widths, matching tables, one-picture replacement:**
1. After `tr.getparent().remove(tr)` the rows below move up by themselves. Recompute and rewrite every Total that used the deleted row; removal updates nothing else.
2. `table.columns[i].width = w` writes ONLY `w:gridCol/@w:w`. Word lays out from each cell's `w:tcW`, so also set `cell.width = w` on every cell in that column (mind `gridSpan` cells).
3. `add_row()` and `add_column(width)` already write `tcW` on the new cells; what they never copy is shading, borders, `trPr` or run formatting.
4. To match an existing table: reuse its table style, copy each column's width onto both gridCol and every cell's `tcW`, and copy its header cell formatting (shading, bold/colour runs).
5. Never replace a picture by overwriting its image part's bytes (`_blob`): image parts are shared (SHA1 dedupe), so every picture using that part changes.
6. Replace ONE picture: `rId, _ = part.get_or_add_image(path)` on the part that owns the blip, set that `a:blip`'s `r:embed` to the new rId, keep `wp:extent`. Then drop the old rel only if no `a:blip` in that part still has `r:embed` = old rId (`part.drop_rel()` counts only `r:id`, never `r:embed`).
7. Prove the old image is gone: after saving, hash every `word/media/*` in the zip.
<!-- /kaizen:digest -->

## row-delete: removal is not the whole edit

- python-docx has no row-delete API. Remove the element:
  `tr = row._tr; tr.getparent().remove(tr)`. The remaining rows stay contiguous;
  there is no gap to close and nothing to shift.
- Removal updates nothing else. A Total cell (plain text, or a field's cached
  result) that summed the deleted row keeps its old value. Recompute it from the
  remaining rows and write it back through the cell's existing run.

## table-relayout: which call writes which width

- `_Column.width` (`table.columns[i].width = w`) sets only `w:gridCol/@w:w`. It does
  not touch any cell. Every cell's `w:tcW` keeps its old value, and Word renders from
  `tcW`, so the column does not change in Word.
- To resize or rebalance a column, set both: `table.columns[i].width = w` and
  `cell.width = w` for every cell in that grid column, in every row. A cell with
  `gridSpan` gets the sum of the grid columns it covers. Keep the gridCol total at or
  below `section.page_width - section.left_margin - section.right_margin`, and set
  `table.autofit = False` (fixed layout) when the widths must hold.
- `table.add_row()` gives each new cell `tcW` from its gridCol; `table.add_column(width)`
  sets the new gridCol and each new cell's `tcW` to `width`. Neither copies `tcPr`
  shading/borders, `trPr`, paragraph or run formatting, and neither moves or shrinks
  other columns.

## table-insert: matching an existing table

- A table from `doc.add_table()` gets the section text width split evenly, the
  document's default table style, and no header formatting.
- To make it match an existing table, copy all three:
  - style: `new.style = src.style`;
  - widths: for each column, `new.columns[i].width = src.columns[i].width` AND
    `cell.width` on every cell of that column (the column setter alone leaves `tcW`
    at the even split); copy `tblW`/`tblInd` if the source sets them;
  - header row: copy the source header cells' `tcPr` shading and write header text
    through runs with the same bold/size/colour (or deep-copy the source header
    `w:tr` and change its text through its existing runs).

## image-replace: new part, repoint, drop the old rel safely

- `a:blip/@r:embed` is an rId in the rels of the part that contains the blip
  (document part, or a header/footer part). Several blips, and rels from several
  parts, can resolve to one image part because python-docx dedupes identical bytes.
  Overwriting that part's `_blob` changes every one of them. Do not use it as a
  replacement method, including for header logos.
- To replace one picture: `new_rId, _ = owning_part.get_or_add_image(path)`
  (`doc.part` for the body, `section.header.part` etc. for headers), set this
  blip's `r:embed` to `new_rId`, and leave `wp:extent`/`a:ext` as they are (adjust
  one dimension only if the aspect ratio differs).
- Repointing does not remove the old rel or media: the part is saved while any rel
  reaches it. Count the blips in the owning part's XML whose `r:embed` equals the old
  rId. Call `owning_part.drop_rel(old_rId)` only when that count is 0. `drop_rel`
  itself only counts `r:id` attributes, so it will delete a rel that other blips
  still use and break those pictures.
- Verify: re-open the saved zip and confirm no `word/media/*` entry still hashes
  to the old image bytes. One media part can back rels from several parts (body and
  header).
