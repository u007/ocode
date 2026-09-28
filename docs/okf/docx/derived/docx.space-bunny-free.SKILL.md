---
name: docx-tuning-space-bunny-free
description: >
  Corrective Word-editing guidance for space-bunny-free: what doc.add_table,
  add_row and add_column really set for widths, making a new table match an
  existing one, inserting a column across merged cells, and replacing one
  picture without touching others that share its image part.
when_to_use: >
  Load when the provider-stripped model id (see stack-detection.md) resolves
  to exactly `space-bunny-free` AND the repository contains a Word document (`*.docx` or
  `*.doc` at the root or up to two directories deep, per meta.yaml detection).
  For any other model or a repo without Word files, do not load.
tuned_for: space-bunny-free
tuned_version: "alpha"
stack: docx
source_scorecard: ../scores/space-bunny-free.md
threshold: 0.9
revalidate_when: model_version changes
---
# Word-editing corrections for space-bunny-free

<!-- kaizen:digest -->
**Word edits (python-docx): new tables have real widths; copy style correctly:**
1. `doc.add_table(r, c)` splits the section text width evenly: every `w:gridCol` AND every cell's `w:tcW` (dxa) get width/c; only `tblW` is auto. `add_row()` takes no arguments and copies each gridCol width into the new `tcW`.
2. `_Column.width` sets only the gridCol; set each cell's `cell.width` too. `add_column(width)` appends at the far right and returns a `_Column`.
3. To match an existing table: `new.style = existing.style` (never guess a style name: a missing name, "Table Grid" included, raises KeyError), copy gridCol + tcW widths, and copy the header cells' `w:shd` and run formatting. Never assign `tblPr` or insert a second `w:tblGrid`.
4. Insert a column by GRID position, not tc index: a row with `w:gridSpan` has fewer `w:tc` than `w:gridCol`. A cell spanning the insert point gets `gridSpan + 1` instead of a new tc.
5. Image parts are shared (SHA1 dedupe across the whole package, headers included): never overwrite `image_part._blob` to change one picture.
6. Replace one picture: `rId, image = part.get_or_add_image(path)` on the part that owns the blip, set that blip's `r:embed` to `rId`, keep cx and set cy = cx * px_height // px_width.
7. Drop the old image rel only when no `r:embed` in that part still names it: `Part.drop_rel` counts only `r:id` and deletes an image rel that is still in use.
<!-- /kaizen:digest -->

## docx-model: image parts, relationships and units

- A blip's `r:embed` is an rId of the part that holds the drawing (document,
  header or footer part), pointing to an image part under `word/media/`.
- Image parts are deduplicated by SHA1 for the whole package. The same bytes
  added in the body and in a header resolve to ONE part, so a part's bytes can
  back pictures anywhere in the document.
- `part.drop_rel(rId)` deletes the rel whenever fewer than two `r:id` attributes
  name it; it never looks at `r:embed`. Count
  `part.element.xpath(f'//a:blip[@r:embed="{rId}"]')` yourself and drop only at
  zero. An image part that no rel reaches is not written on save.
- Units: widths in twips (1440/inch, 20/pt), image extents in EMU (914400/inch,
  12700/pt), image files in pixels. Pixels only enter as a ratio
  (`px_height / px_width`), never multiplied with EMU.
- Subtracting Lengths gives a plain `int` (EMU): wrap it (`Emu(section.page_width
  - section.left_margin - section.right_margin).twips`) before reading `.twips`.

## table-relayout: add_row, add_column and merged cells

- `add_row()` takes no arguments. It appends at the bottom; each new cell gets a
  dxa `tcW` equal to its `gridCol` and nothing else (no trPr, shading or runs).
- `add_column(width)` requires a width, appends a gridCol plus one `w:tc` at the
  right end of every row (also after a merged cell), and returns a `_Column`.
  The other columns do not shrink.
- Inserting a column at grid index `i`:
  1. Deep-copy a neighbouring `gridCol` and insert it at `i`.
  2. In each row, find the cell by grid offset (`tc.grid_offset`, which counts
     `gridBefore` and preceding `gridSpan`s). If a cell starts at `i`, insert a
     deep copy of its left neighbour from the SAME row before it. If a cell spans
     across `i` (offset < i < offset + span), set its `grid_span` to span + 1
     instead of adding a cell.
  3. Rebalance every `gridCol` and every `tcW` (a spanning cell's tcW = sum of
     its columns) to fit the text width.
- Check afterwards: in every row, gridBefore + Σ gridSpan + gridAfter equals the
  number of `gridCol`s. Do not compare the `w:tc` count with the grid.

## table-insert: widths and style copying

- `doc.add_table()` sets `tblW` to auto, but every `gridCol` and every cell's
  `tcW` gets `text_width / cols` in dxa. It is inserted before the final
  `w:sectPr`; move it with `anchor_p._p.addnext(table._tbl)`.
- Match an existing table:
  1. `new.style = existing.style`; list `doc.styles` rather than guessing names.
     "Table Grid" raises KeyError like any other name when the template's
     styles.xml lacks it. The last resort is explicit `w:tblBorders`.
  2. Widths: set each `gridCol` `w:w` (edit the existing `tblGrid` in place,
     never insert a second one) and each `cell.width`.
  3. Header look: copy the existing header cells' `w:shd` and write text through
     runs that carry the same `rPr` (bold, colour, font, size).
- `tblPr` has no setter (AttributeError); edit its children or `deepcopy` its
  child elements into the existing `tblPr`.
- `w:tblBorders` goes after `tblW`, `jc`, `tblCellSpacing`, `tblInd` and before
  `shd`, `tblLayout`, `tblCellMar`, `tblLook` inside `tblPr`; do not append it
  at the end.

## image-replace: one picture, its own part

- To change one picture, keep its `w:drawing` and swap only the reference:
  1. `rId, image = part.get_or_add_image(path)` where `part` owns the blip
     (`doc.part`, `section.header.part`, ...).
  2. Set that blip's `r:embed` to `rId`.
  3. Keep the displayed width; if the aspect ratio changed, set
     cy = cx * image.px_height // image.px_width in both `wp:extent` and the
     picture's `a:ext` (`InlineShape.height = ...` writes both for inline shapes).
  4. Drop the old rId only when no `r:embed` in that part still uses it (see
     docx-model).
- Overwriting `image_part._blob` is never the way to replace one picture: it
  changes every picture, in any part, that shares the image part.
- Read the extent of the drawing being replaced, not the first `wp:extent` in
  the part.
