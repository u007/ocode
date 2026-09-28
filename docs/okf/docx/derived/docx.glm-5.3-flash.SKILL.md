---
name: docx-tuning-glm-5.3-flash
description: >
  Corrective Word-editing guidance for glm-5.3-flash: cell.text wipes
  paragraph formatting too, doc.add_table already sets widths, field totals
  are cached, carrying a vMerge restart down, and removing the old image
  relationship after a picture swap.
when_to_use: >
  Load when the provider-stripped model id (see stack-detection.md) resolves
  to exactly `glm-5.3-flash` AND the repository contains a Word document (`*.docx` or
  `*.doc` at the root or up to two directories deep, per meta.yaml detection).
  For any other model or a repo without Word files, do not load.
tuned_for: glm-5.3-flash
tuned_version: "5.3"
stack: docx
source_scorecard: ../scores/glm-5.3-flash.md
threshold: 0.9
revalidate_when: model_version changes
---
# Word-editing corrections for glm-5.3-flash

<!-- kaizen:digest -->
**Word edits (python-docx): cell.text wipes pPr, real table widths, cached totals, drop old image rels:**
1. `cell.text = "..."` deletes every child of the cell except `w:tcPr`: run formatting AND paragraph `w:pPr` (alignment, spacing, style) are lost. To keep them, set `p.runs[0].text` and remove the other `w:r`.
2. `doc.add_table(r, c)` is not width-less: every `w:gridCol` and every cell's `w:tcW` get text_width / c (dxa). To match an existing table, set `new.style = old.style`, copy gridCol + tcW, and copy the header's `w:shd` and run formatting.
3. A total like `{ =SUM(ABOVE) }` keeps its CACHED result after you delete or change rows. python-docx never recomputes it. Write the new value into the result run and keep the field. Do not rely on `w:updateFields`.
4. Deleting a row: `tr = row._tr; tr.getparent().remove(tr)`. The remaining `w:tr` stay contiguous, so there is no gap to shift. Then update every total that depended on the row.
5. Deleting a row that holds `w:vMerge w:val="restart"`: move its paragraphs into the next row's cell and set `restart` there, then delete the row.
6. After repointing a picture's `a:blip r:embed`, check that no `a:blip` in that part still uses the old rId, then `part.drop_rel(old_rId)`. Repeat in every header/footer part that shows it, or the old media stays.
7. Tracked documents: python-docx writes no `w:ins`/`w:del`, and the old value can survive in `w:delText`. Check `.//w:delText` via xpath, not `paragraph.text`.
<!-- /kaizen:digest -->

## row-delete: close-up, totals, merges, cached fields

- Remove the element: `tr = row._tr; tr.getparent().remove(tr)`. The remaining `w:tr` are
  contiguous siblings; there is no gap to shift or close.
- Then recompute every number that depended on the removed row (Total,
  subtotal, tax) and write it back.
- Totals held in a field (`w:fldSimple`, or a complex field: `w:instrText`
  between the `begin` and `separate` `w:fldChar`, result runs between
  `separate` and `end`) store a cached result. Deleting rows and
  saving leaves the old sum in the file. Compute the new value and write it into
  the result: the `w:t` inside `w:fldSimple`, or the runs between `separate` and
  `end`. Keep the instruction. Do not treat `<w:updateFields w:val="true"/>` in
  settings.xml as a substitute for writing the value.
- Row starting a vertical merge (`w:vMerge w:val="restart"` with continuations
  below): removing it alone leaves continuations with no restart. They
  silently join the merged cell above them, or, at the top of the table,
  python-docx's `table.cell()` raises IndexError. Before removing it, move
  the restart cell's `w:p` children into the next row's cell (replacing that
  cell's empty paragraph) and set `w:val="restart"` on its `w:vMerge`. If only
  one row remains in the merge, drop its `w:vMerge`. Then remove the row.

## cell-edit: what cell.text really destroys

- `cell.text = "SKU"` clears ALL cell content except `w:tcPr`, then adds one
  plain paragraph with one plain run. Lost: run `w:rPr` (bold, size, colour,
  font) AND paragraph `w:pPr` (alignment, spacing, paragraph style). Kept: only
  `w:tcPr` (shading, width, borders, merge flags).
- To keep formatting: `p = cell.paragraphs[0]; p.runs[0].text = "SKU"`, then
  remove the other runs (`r._r.getparent().remove(r._r)`). Both rPr and pPr stay.
- Track Changes: a python-docx edit produces no `w:ins`/`w:del`. Deleted text
  stays in `w:delText`, which `paragraph.text` and `cell.text` do not show.
  Decide whether the edit must be tracked (then write `w:del`/`w:ins` yourself)
  or clean. For a clean edit, resolve the existing revisions in that cell and
  confirm `cell._tc.xpath(".//w:delText")` no longer holds the old value.

## table-insert: default widths and matching an existing table

- `doc.add_table(rows, cols)` sets `tblW` to auto, but writes explicit widths:
  every `w:gridCol` and every cell's `w:tcW` get `text_width / cols` in dxa
  (text width = page width minus left and right margins). It does not come out
  narrow or content-fitted.
- To match an existing table:
  1. `new.style = existing.style`. A style name the document lacks raises
     KeyError, so take the style from the existing table, not a guessed name.
  2. Widths: set each `w:gridCol` `w:w` AND each cell's width.
     `table.columns[i].width` changes only the gridCol, not the cells' `tcW`.
  3. Header look: copy the existing header cells' `w:shd` and write the text
     into runs that carry the same `rPr` (bold, colour, size, font).

## image-replace: remove the old image, not just repoint

- `rId, image = doc.part.get_or_add_image(path)` adds the new picture part (or
  returns the existing rId when the same bytes are already there: python-docx
  dedupes by SHA1, so two pictures of the same bytes share one rId and part).
  Set this blip's `r:embed` to `rId`. `wp:extent` is untouched, so position and
  size stay.
- The old part stays in the saved package until its relationship is gone. After
  repointing, collect `part.element.xpath("//a:blip/@r:embed")`. If the old rId
  is not among them, call `part.drop_rel(old_rId)`. `drop_rel`'s own reference
  check counts only `r:id` attributes, not `r:embed`, so it removes an image
  rel even while other pictures still use it. Always run the scan first.
- A header or footer has its own part and rels to the same media. Its
  pictures keep the old image in the package. Repoint and drop there too if
  that logo must go as well.
- `doc.inline_shapes` lists only `wp:inline` pictures. To find a floating logo,
  search `.//a:blip` in the body, headers and footers (this covers `wp:anchor`).
