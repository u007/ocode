---
name: docx-tuning-mimo-v2.6-flash
description: >
  Corrective Word-editing guidance for mimo-v2.6-flash: field results are
  cached (Word does not recompute SUM(ABOVE) on open), what row deletion and
  value changes need afterwards, the real python-docx call for replacing one
  picture and when its old relationship may go, and shading new column cells
  like their row.
when_to_use: >
  Load when the provider-stripped model id (see stack-detection.md) resolves
  to exactly `mimo-v2.6-flash` AND the repository contains a Word document (`*.docx` or
  `*.doc` at the root or up to two directories deep, per meta.yaml detection).
  For any other model or a repo without Word files, do not load.
tuned_for: mimo-v2.6-flash
tuned_version: "2.6"
stack: docx
source_scorecard: ../scores/mimo-v2.6-flash.md
threshold: 0.9
revalidate_when: model_version changes
---
# Word-editing corrections for mimo-v2.6-flash

<!-- kaizen:digest -->
**Word edits (python-docx): cached fields, totals, row shading:**
1. A field (`{ =SUM(ABOVE) }`, w:fldSimple / w:instrText) shows a CACHED result. Neither python-docx nor Word-on-open recomputes it. After deleting or changing rows, write the new value into the field's result run. `<w:updateFields w:val="true"/>` only makes Word ASK the user on open.
2. Deleting a row = `tr.getparent().remove(tr)`; Word reflows, no gap to close. Then update every dependent total.
3. After changing an input value, recompute every dependent number (row amount, subtotal, tax, total) from the table data and write each into its cell or field result `w:t`, in the existing number format; never `cell.text =` on a field cell (it deletes the field).
4. Replace one picture: `rId, image = doc.part.get_or_add_image(path)` (there is no `get_or_add_image_part`), set that `a:blip`'s `r:embed` to `rId`, leave `wp:extent` alone.
5. Delete the old image rel only when no `a:blip/@r:embed` in the part still uses it; never via `part.drop_rel()`, which ignores `r:embed` and drops a shared picture's rel.
6. A new column's cells take the shading (`w:tcPr/w:shd`) of the row they sit in, not of the cell you copied from another column.
<!-- /kaizen:digest -->

## row-delete: fields are cached, totals are yours

- `{ =SUM(ABOVE) }` stores its instruction plus the last computed result (runs
  between the `separate` and `end` fldChar, or the text inside `w:fldSimple`).
  Word does not recalculate it when the file opens; the old sum, including the
  deleted row, stays visible. Compute the new total from the table and write it
  into that result run, keeping the field. Setting `w:updateFields` in
  settings.xml is optional and only prompts the user.
- Removing `w:tr` needs no manual shifting: Word reflows. What remains is the
  Total and any other number derived from the deleted row.

## cell-edit: a changed value invalidates cached results

- After changing an input (for example Qty), recompute the row amount and
  every number derived from it (subtotal, tax, grand total, figures repeated in
  the text) from the table data, in dependency order.
- A dependent number that is a field keeps showing its old cached result until
  you write the new value into it: the `w:t` runs between the `separate` and
  `end` fldChar, or the `w:t` inside `w:fldSimple`. Edit that `w:t` directly.
  `cell.text = ...` on that cell removes the whole field (instruction and
  fldChars). The row-delete section's rule on `w:updateFields` applies here too.
- Write new figures in the cell's existing number format (decimals, thousands
  separator, currency) and keep the paragraph's alignment.

## image-replace: the real call and when the old rel may go

- Add the new image with `rId, image = doc.part.get_or_add_image(path_or_stream)`;
  it returns `(str, Image)` and reuses the existing rId when the bytes are
  already in the package. `DocumentPart` has no `get_or_add_image_part`.
- Set only the target picture's `a:blip` `r:embed` to that rId. `wp:extent`
  and the rest of the drawing stay, so position and size are kept.
- Identical images share one rel. Before removing the old rId, scan every
  `a:blip` in the part (`part.element.iter(qn('a:blip'))`); remove it with
  `del part.rels[old_rId]` only when none still embeds it. Do not use
  `part.drop_rel()`: it counts only `@r:id` references, sees 0 for images and
  drops a rel another picture still uses.
- Once no rel points to the old image, it is left out of the saved package;
  while any rel remains, the old media stays in the file.

## Observed in live editing: shade new cells like their row

- When inserting a column by deep-copying a neighbouring cell per row, copy from
  the SAME row (so banded `w:shd` matches), or set each new cell's `w:shd` to
  its row's fill afterwards. Check every body row has one fill across all cells.
