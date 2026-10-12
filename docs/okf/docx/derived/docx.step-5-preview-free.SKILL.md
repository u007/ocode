---
name: docx-tuning-step-5-preview-free
description: >
  Corrective Word-editing guidance for step-5-preview-free: a deleted row
  reflows on its own but its dependent totals do not, a vMerge restart's
  content moves down before the row goes, and a replaced or removed picture
  keeps its old media part in the package until the relationship is dropped.
when_to_use: >
  Load when the provider-stripped model id (see stack-detection.md) resolves
  to exactly `step-5-preview-free` AND the repository contains a Word document
  (`*.docx` or `*.doc` at the root or up to two directories deep, per meta.yaml
  detection). For any other model or a repo without Word files, do not load.
tuned_for: step-5-preview-free
tuned_version: "5-preview"
stack: docx
source_scorecard: ../scores/step-5-preview-free.md
threshold: 0.9
revalidate_when: model_version changes
---
# Word-editing corrections for step-5-preview-free

<!-- kaizen:digest -->
**Word edits (python-docx): a row delete is not finished until totals follow; an image swap is not finished until the old rel is dropped:**
1. Deleting a row is `tr = row._tr; tr.getparent().remove(tr)`. The remaining `w:tr` are contiguous siblings: Word reflows, nothing to shift. The deletion is NOT done until every number derived from that row (Total, subtotal, tax, grand total, figures repeated in the text) is recomputed and written back; a `{ =SUM(ABOVE) }` field keeps its cached result until you rewrite it.
2. Deleting a row whose cell has `w:vMerge w:val="restart"`: move its paragraphs into the next row's cell, set `w:val="restart"` there (drop `vMerge` if no continuation remains), then remove the row.
3. Old image relationships and media parts never leave on their own. python-docx saves every part still reachable through a rel, so after repointing a blip or removing a `w:drawing`, `word/media/imageN.*` stays in the zip until you drop the rel from the owning part.
4. Drop the old rel only when no `a:blip[@r:embed]` in that part still names it (`Part.drop_rel` counts only `r:id`, so count `r:embed` yourself). Do this in every part that showed the picture (document, each header/footer).
5. `rId, image = part.get_or_add_image(path)` returns the rId FIRST. Call it on the part that owns the blip, then set that blip's `r:embed` to `rId` and keep `wp:extent`.
6. Verify with the saved zip, not with assumptions: hash every `word/media/*` against the old image's bytes and confirm no `.rels` in any part still targets it.
<!-- /kaizen:digest -->

## row-delete: removal, reflow, and what depends on the row

- Remove the element: `tr = row._tr; tr.getparent().remove(tr)` (or
  `table._tbl.remove(tr)`). Word reflows the table; the rows below move up by
  themselves and there is no gap to close or shift manually.
- A deleted row usually feeds other numbers. Before saving, recompute from the
  surviving cells every value that depended on it: the Total, any subtotal,
  tax, grand total, amount-in-words, and any figure repeated in body text or a
  header/footer. Keep each cell's existing number format and alignment.
- When the Total is a field (`w:fldSimple`, or `w:fldChar begin` /
  `w:instrText` / `separate` / result runs / `end`), the displayed value is the
  CACHED result. python-docx never recalculates and Word does not recalculate on
  open. Write the recomputed value into the result run and keep the field;
  `<w:updateFields w:val="true"/>` only prompts the user and is not a substitute.
- Row starting a vertical merge (`w:vMerge w:val="restart"` with `<w:vMerge/>`
  continuations below): removing it alone leaves continuations with no restart.
  First move the restart cell's paragraphs into the next row's cell, set
  `w:vMerge w:val="restart"` on that cell, and if no further continuation
  remains drop `vMerge` from it entirely. Then remove the row. Afterwards check
  that no continuation in that column lacks a restart above it.
- Verify after saving: the `w:tr` count dropped by exactly one, the deleted
  row's text appears in no `w:t` of any part, and the recomputed totals equal
  the printed ones.

## image-replace: a part lives as long as its relationship

- The picture chain is `w:drawing` > `pic:pic` > `a:blip r:embed="rIdN"`; the
  rId is a relationship of the part that holds the drawing (document, header or
  footer part), targeting `word/media/imageN.*`. Size lives in `wp:extent` and
  the inner `a:ext` (EMU), not in the image file.
- Replace one picture without touching the others:
  1. `rId, image = part.get_or_add_image(path)` on the OWNING part
     (`doc.part`, `section.header.part`, ...). The tuple is `(rId, image)`.
  2. Set that blip's `r:embed` to `rId`. Keep `wp:extent`; if the aspect ratio
     changed, keep cx and set cy = cx * image.px_height // image.px_width as
     integers in both `wp:extent` and `a:ext`.
  3. Do not overwrite `image_part._blob`: image parts are deduplicated by SHA1
     across the whole package, so one part can back several pictures.
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
  from "the part count went down by one"; it does not go down until the rel is
  dropped.
