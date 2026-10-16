---
name: pptx-tuning-nvidia-nemotron-3.5-super-vl
description: >
  Corrective pptx-editing guidance for nvidia/nemotron-3.5-super-vl: locate
  tables by header text with a unique-row assert, the real DrawingML merge
  model and element names (gridSpan on a:tc, a:txBody, a:tableStyleId), the
  TablePlaceholder-only insert_table, finishing a row or column insert (total,
  frame height, bottom check, copied cell formatting), re-applying explicit
  banding fills after a delete, and drop_rel's r:id-only reference count.
when_to_use: >
  Load when the provider-stripped model id (see stack-detection.md) resolves
  to exactly `nvidia/nemotron-3.5-super-vl` AND the repository contains a
  PowerPoint deck (`*.pptx` or `*.ppt` at the root or up to two directories
  deep, per meta.yaml detection). For any other model or a repo without decks,
  do not load.
tuned_for: nvidia/nemotron-3.5-super-vl
tuned_version: "3.5"
stack: pptx
source_scorecard: ../scores/nvidia__nemotron-3.5-super-vl.md
threshold: 0.9
revalidate_when: model_version changes
---
# PowerPoint-editing corrections for nvidia/nemotron-3.5-super-vl

<!-- kaizen:digest -->
**PowerPoint edits (python-pptx): find by content, use the real XML names, finish every edit:**
1. Pick the table by its HEADER text (first row reads e.g. "Item", "Qty"), never the first `has_table` shape or a shape index; recurse into group shapes. Match the row on exact stripped cell text (`cell.text_frame.text.strip()`), collect ALL hits and assert exactly one; zero or several = stop and ask.
2. Every `a:tr` has exactly one `a:tc` per `a:gridCol`, merged or not. The origin carries `gridSpan="2"` / `rowSpan="2"` as attributes of `a:tc` itself (never `colSpan`, never on `a:tcPr`); the covered cells are real `a:tc` elements with `hMerge="1"` / `vMerge="1"` and empty text. Deleting or inserting through a merge means adjusting the origin's span AND adding/removing the matching covered cell, then asserting `len(tr.tc_lst) == len(tblGrid.gridCol_lst)` for every row.
3. Element names: cell text lives in `a:tc/a:txBody` (not `a:textBody`); the style is `a:tblPr/a:tableStyleId` (not `a:tblStyleId`). Read `prs.slide_width` / `prs.slide_height` instead of assuming 16:9.
4. `insert_table(rows, cols)` exists only on a `TablePlaceholder`. A generic content placeholder is a `SlidePlaceholder` without it: `add_table()` at the placeholder's left/top/width/height, then remove the placeholder's `p:sp` so no "Click to add text" box remains.
5. A row insert is not done after the deepcopy: recompute the Total, set `gf.height = sum(r.height for r in gf.table.rows)`, then check `gf.top + gf.height <= prs.slide_height` and that no shape below is overlapped. A column insert deep-copies the neighbouring header cell for the header row and a body cell for body rows so the new cells keep fill, bold and colour.
6. After deleting a row from a table with explicit `a:tcPr/a:solidFill` shading, re-apply the alternating fills to every row after the gap (or clear them and rely on the style's `bandRow`); explaining the cause is not the fix.
7. `drop_rel` does reference-count, but it matches only `//@r:id`. Pictures reference images with `a:blip r:embed`, so an image rel always counts 0 and is always dropped, shared or not. Before calling it, scan the part yourself: `slide._element.xpath("//@r:embed | //@r:link")`; drop only when the rId has no other user, otherwise repoint the survivor to a new part first. After saving, list the zip's `ppt/media` and hash-compare to prove the old bytes are gone.
<!-- /kaizen:digest -->

## table-locate: choose by content, prove uniqueness

- A slide (or its groups) can hold several tables; "the" table is the one whose
  first-row cells, stripped, match the expected header. Filter `has_table`
  shapes, recurse into `GroupShape.shapes`, and assert exactly one table
  matches before touching it.
- Find the row on `cell.text_frame.text.strip() == "Gadget D"` in the key
  column (not a substring check on `cell.text`). Collect every match and assert
  `len(hits) == 1`; do not stop at the first index.
- Covered cells of a merge are empty; only the origin holds the text.

## pptx-model: merges, element names, placeholders

- Merge representation: `<a:tc gridSpan="2">` is followed in the same `a:tr`
  by `<a:tc hMerge="1">`; vertical merges use `rowSpan` on the origin and
  `vMerge="1"` on the covered cells below. There is no `colSpan` attribute and
  nothing span-related lives on `a:tcPr`. The covered cells are never omitted,
  so a 4-column grid always has four `a:tc` per row. python-pptx:
  `cell.is_merge_origin`, `cell.is_spanned`, `span_width`, `span_height`;
  `row.cells[c]` is `table.cell(r, c)`. Write text to the origin only.
- Editing through a merge is two edits that go together: change the origin's
  `gridSpan`/`rowSpan` AND add or remove the covered `a:tc`. "Never delete
  through a span" is not an answer when the user asked for that deletion; do
  both edits and assert the per-row `a:tc` count equals the `a:gridCol` count.
- Names that must be exact in xpath and lxml code: `a:txBody` (cell text
  body), `a:tableStyleId` (child element of `a:tblPr`, a GUID string),
  `a:tblGrid/a:gridCol/@w`, `a:tr/@h`.
- Units: EMU, 914400 per inch, 12700 per point. Take the slide size from
  `prs.slide_width` and `prs.slide_height`; python-pptx's default template is
  4:3 (9144000 × 6858000), a 16:9 deck is usually 12192000 × 6858000, and a
  deck can be anything else.
- `insert_table(rows, cols)` returns a `PlaceholderGraphicFrame` in the
  placeholder's geometry, but only a `TablePlaceholder` (placeholder type
  TABLE) has it. An "empty content placeholder" is a `SlidePlaceholder`:
  `slide.shapes.add_table(rows, cols, ph.left, ph.top, ph.width, ph.height)`
  and then `ph._element.getparent().remove(ph._element)`.

## cell-edit: finish a shading repair

- When the deck shades rows with explicit `a:tcPr/a:solidFill` (not
  `bandRow` + a table style), a deleted middle row leaves two neighbours with
  the same colour. Fix it: walk the body rows after the deleted one and
  re-assign the alternating fill to every cell (copy the `a:solidFill` of the
  two reference rows), or remove the explicit fills and set `bandRow="1"` so
  the style banding recomputes. Diagnosing which deck is which is only the
  first half of the task.

## table-relayout: finish a row insert, dress a new column

- Row insert: `ref_tr.addnext(copy.deepcopy(ref_tr))`, set the new cells'
  text through their existing first run (keep `a:rPr`), then: recompute the
  Total row, set `gf.height = sum(r.height for r in gf.table.rows)`, compare
  `gf.top + gf.height` with `prs.slide_height` minus the deck's margin, and
  compare the new bottom with the top of every shape below the table. Move
  them, shrink row heights, or ask. Re-apply explicit banding fills if the deck
  uses them.
- Column insert: after the new `a:gridCol` and one new `a:tc` per `a:tr`, the
  new cells must look like their neighbours. Deep-copy the Item header cell
  for the header row (fill, bold, white text) and the Item body cell for each
  body row, then replace the run text. A bare `<a:tc><a:txBody>…` renders with
  default formatting and breaks the header band.
- Through a merged Total row, do both: bump the origin's `gridSpan` and insert
  a deep copy of a covered `a:tc hMerge="1"` after it. Then assert the per-row
  `a:tc` count equals the `a:gridCol` count, rebalance the widths so their sum
  stays within the frame (or `slide_width` minus margins), and set `gf.width`
  to that sum.

## image-replace: why drop_rel breaks a shared picture

- `Part.drop_rel(rId)` removes the relationship when `_rel_ref_count(rId) < 2`,
  and that count is an xpath for `//@r:id`. A picture references its image
  through `a:blip/@r:embed`, which the count never sees, so an image rel is
  reported as unused and dropped even when another `p:pic` on the slide still
  points at it. The broken second picture is this, not a quirk of shared
  rIds in general.
- Replacing a picture: give the target its own image part (`add_picture`, or
  `slide.part.get_or_add_image_part(path)` and repoint `blip.rEmbed`), keep
  the old `p:pic`'s spTree index for z-order, remove the old element, and only
  then decide about the old rel: drop it when no other `r:embed`/`r:link` in
  the part uses it, otherwise leave it.

## verify-safety: check references yourself, prove the bytes are gone

- Before any `drop_rel`, run
  `slide._element.xpath("//@r:embed | //@r:link")` and count the users of that
  rId; the library's own guard cannot do this for images. If another element
  uses it, repoint that element to a new image part first.
- After saving to a new file, open the zip, list `ppt/media/`, and compare
  hashes against the removed image's bytes; also grep every `.rels` for the
  old target. A deleted `p:pic` alone leaves the part reachable and saved.
