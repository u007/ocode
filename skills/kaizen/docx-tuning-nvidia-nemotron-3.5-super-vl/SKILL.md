---
name: docx-tuning-nvidia-nemotron-3.5-super-vl
description: >
  Corrective Word-editing guidance for nvidia/nemotron-3.5-super-vl: a copied
  table row is filled through its existing runs, a fresh python-docx table
  already carries even gridCol/tcW widths that must be overwritten to match an
  existing table, and a replaced or removed picture keeps its old media part in
  the package until the relationship is dropped.
when_to_use: >
  Load when the provider-stripped model id (see stack-detection.md) resolves
  to exactly `nvidia/nemotron-3.5-super-vl` AND the repository contains a Word
  document (`*.docx` or `*.doc` at the root or up to two directories deep, per
  meta.yaml detection). For any other model or a repo without Word files, do
  not load.
tuned_for: nvidia/nemotron-3.5-super-vl
tuned_version: "3.5"
stack: docx
source_scorecard: ../scores/nvidia__nemotron-3.5-super-vl.md
threshold: 0.9
revalidate_when: model_version changes
---
# Word-editing corrections for nvidia/nemotron-3.5-super-vl

<!-- kaizen:digest -->
**Word edits (python-docx): fill a copied row through its runs, overwrite a new table's default widths, and drop the old image rel yourself:**
1. A mid-table row is `new_tr = copy.deepcopy(ref_row._tr); ref_tr.addnext(new_tr)`. Fill it by setting `.text` on each cell's FIRST existing run and clearing the other runs. Never `cell.text = value` on the clone: that replaces every run with one default run and discards the bold/size/colour the copy was made for. Also drop extra paragraphs the template row carried.
2. With alternating row shading, copy the row whose `w:shd` matches the slot (or swap the shading) so the banding still alternates after the insert.
3. `doc.add_table(rows, cols)` is not "unsized": python-docx writes a `w:gridCol` and a `w:tcW` for every column, each = section text width / cols, with `w:tblW` auto. To match an existing table, overwrite them: copy its `w:tblW`, `w:tblLayout`, every `w:gridCol w:w`, every cell's `w:tcW`, its style and its header-row formatting (shading, bold/white runs).
4. Old image relationships and media parts never leave on their own. python-docx saves every part still reachable through a rel, so after repointing a blip or removing a `w:drawing`, `word/media/imageN.*` stays in the zip until you drop the rel from the owning part.
5. Drop the old rel only when no `a:blip[@r:embed]` in that part still names it (`Part.drop_rel` counts only `r:id`, so count `r:embed` yourself). Do this in every part that showed the picture (document, each header/footer).
6. Image parts are deduplicated by SHA1 on add, so one part can back several pictures: always give a replaced picture its own new part (`rId, image = part.get_or_add_image(path)`, rId FIRST) and repoint only that blip.
7. Verify from the saved zip, not from assumptions: hash every `word/media/*` against the old image's bytes and confirm no `.rels` in any part still targets it.
<!-- /kaizen:digest -->

## table-relayout: a copied row is filled through its runs

- Insert a row in the middle with `new_tr = copy.deepcopy(ref_row._tr)` and
  `ref_row._tr.addnext(new_tr)` (or `total_tr.addprevious(new_tr)`). The copy
  carries `w:trPr`, each cell's `w:tcPr` and each run's `w:rPr`; that is the
  reason to copy instead of `add_row()`.
- Write the new values through the copied runs: for each cell, set the text of
  the first `w:r` and remove the remaining runs (and any extra paragraphs the
  template row had). Do not assign `cell.text` on the clone; it replaces the
  cell's content with one plain run and the copied bold/size/colour is gone.
- If the body rows alternate shading (`w:shd` per cell, or `tblLook` banding
  with a style), pick the template row whose shading fits the new position, or
  swap the `w:shd` fill, so the rows still alternate after the insert.
- A new row does not change the grid; a new column does: insert the
  `w:gridCol` and one `w:tc` per row at the right index, then rebalance every
  `w:gridCol` and `w:tcW` so the sum stays at or below the section text width.

## table-insert: a new table already has widths, so overwrite them

- `doc.add_table(rows, cols)` appends a `w:tbl` whose `w:tblGrid` has one
  `w:gridCol` per column and whose cells each have a `w:tcW`, all equal to the
  section text width (page width minus left and right margins) divided by the
  column count. `w:tblW` is `type="auto"`. The columns therefore ARE fixed to a
  known, evenly split width; they are not sized from content.
- To match an existing table, read and copy from it: `w:tblW` (type and value),
  `w:tblLayout`, the `w:gridCol w:w` list, each column's `w:tcW`, the table
  style (it must exist in this document's `styles.xml`, else `KeyError`), the
  borders, and the header row's cell shading and run formatting. Then set the
  text through runs whose `w:rPr` matches.
- Keep the total width at or below the text width; set
  `<w:tblLayout w:type="fixed"/>` when the widths must hold exactly.

## image-replace: a part lives as long as its relationship

- The picture chain is `w:drawing` > `pic:pic` > `a:blip r:embed="rIdN"`; the
  rId is a relationship of the part that holds the drawing (document, header or
  footer part), targeting `word/media/imageN.*`. Size lives in `wp:extent` and
  the inner `a:ext` (EMU), not in the image file.
- python-docx deduplicates image parts by SHA1 when an image is added, so one
  `word/media` part can back several pictures. Never overwrite a part's blob
  to change one picture. Replace one picture with
  `rId, image = part.get_or_add_image(path)` on the OWNING part (`doc.part`,
  `section.header.part`, ...; the tuple is `(rId, image)`), set that blip's
  `r:embed` to `rId`, and keep `wp:extent` (if the aspect ratio changed, keep
  cx and set cy = cx * image.px_height // image.px_width as integers in both
  `wp:extent` and `a:ext`). **The recipe has a mandatory last step:** count the
  remaining `a:blip[@r:embed="<old rId>"]` in that part and, at zero, drop the
  old relationship so the old media part is not written on save. A "replace
  one logo" answer that stops at repointing the blip is incomplete — the old
  image is still in the file.
- The old relationship and media part do NOT disappear when nothing displays
  them any more. python-docx writes every part reachable through a rel, so
  after repointing a blip or deleting a `w:drawing` the old
  `word/media/imageN.*` is still in the saved zip. Remove it deliberately:
  count `part.element.xpath(f'//a:blip[@r:embed="{old_rId}"]')`; at zero, drop
  the rel from that part (`Part.drop_rel` looks only at `r:id`, so do the
  `r:embed` count yourself). Repeat for every header/footer part that carried
  the picture. A part no rel reaches is not written on save.
- Prove it from the saved file: unzip, hash every `word/media/*` and assert the
  old image's hash is absent, assert no `.rels` entry in any part still targets
  the old media, and grep every XML part for the old rId. Do not infer removal
  from "the media file is absent"; it is not absent until the rel is dropped.
