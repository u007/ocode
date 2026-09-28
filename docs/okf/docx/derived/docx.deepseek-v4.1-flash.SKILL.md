---
name: docx-tuning-deepseek-v4.1-flash
description: >
  Corrective Word-editing guidance for deepseek-v4.1-flash: what is left to do
  after removing a table row, and how to replace one picture without touching
  shared image parts or leaving the old image in the package.
when_to_use: >
  Load when the provider-stripped model id (see stack-detection.md) resolves
  to exactly `deepseek-v4.1-flash` AND the repository contains a Word document (`*.docx` or
  `*.doc` at the root or up to two directories deep, per meta.yaml detection).
  For any other model or a repo without Word files, do not load.
tuned_for: deepseek-v4.1-flash
tuned_version: "4.1"
stack: docx
source_scorecard: ../scores/deepseek-v4.1-flash.md
threshold: 0.9
revalidate_when: model_version changes
---
# Word-editing corrections for deepseek-v4.1-flash

<!-- kaizen:digest -->
**Word edits (python-docx): row removal follow-up, one-picture replacement:**
1. After `tr.getparent().remove(tr)` the rows below move up by themselves; do not shift or blank anything. Recompute and rewrite every Total that used the deleted row.
2. Never replace a picture by overwriting its image part's bytes: image parts are shared (SHA1 dedupe), so every picture using that part changes.
3. Replace ONE picture: `rId, _ = part.get_or_add_image(path)` on the part that owns the blip, set that `a:blip`'s `r:embed` to the new rId, keep `wp:extent`.
4. Then drop the old rel only if no `a:blip` in that part's XML still has `r:embed` = old rId. `part.drop_rel()` counts only `r:id`, never `r:embed`, so it deletes the rel even when a blip still uses it.
5. Prove the old image is gone: after saving, hash every `word/media/*` in the zip. One media part can back rels from several parts (body and header).
6. Header/footer pictures and anchored (`wp:anchor`) pictures are not in `doc.inline_shapes`. Search each section's header/footer parts, and use that part's rels.
<!-- /kaizen:digest -->

## row-delete: removal is not the whole edit

- python-docx has no row-delete API. Remove the element:
  `tr = row._tr; tr.getparent().remove(tr)`. The remaining rows stay contiguous;
  there is no gap to close and nothing to shift.
- Removal updates nothing else. A Total cell that summed the deleted row keeps its
  old text. Recompute it from the remaining rows and write it back.
- When proving the row is gone, search every XML part (document.xml plus
  header/footer parts), not only document.xml.

## image-replace: new part, repoint, drop the old rel safely

- `a:blip/@r:embed` is an rId in the rels of the part that contains the blip
  (document part, or a header/footer part). Several blips, and rels from several
  parts, can resolve to one image part because python-docx dedupes identical bytes.
  Overwriting that part's `_blob` changes every one of them. Do not use it as a
  replacement method.
- To replace one picture: `new_rId, _ = owning_part.get_or_add_image(path)`
  (`doc.part` for the body, `section.header.part` etc. for headers), set this
  blip's `r:embed` to `new_rId`, and leave `wp:extent`/`a:ext` as they are (adjust
  one dimension only if the aspect ratio differs).
- Removing the old picture from view does not remove its rel or media: the part
  is saved while any rel reaches it. Count the blips in the owning part's XML
  whose `r:embed` equals the old rId. Call `owning_part.drop_rel(old_rId)` only
  when that count is 0. `drop_rel` itself only counts `r:id` attributes, so it
  will delete a rel that other blips still use and break those pictures.
- A logo shown on every page lives in a header part (`section.header`,
  `first_page_header`, `even_page_header`) and may be anchored (`wp:anchor`).
  `doc.inline_shapes` lists only body `wp:inline` pictures. Walk every section's
  header and footer parts for `a:blip` and replace through that part's rels.
- Verify: re-open the saved zip and confirm no `word/media/*` entry still hashes
  to the old image bytes.
