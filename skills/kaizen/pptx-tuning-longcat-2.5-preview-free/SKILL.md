---
name: pptx-tuning-longcat-2.5-preview-free
description: >
  Corrective pptx-editing guidance for longcat-2.5-preview-free: finish row
  deletes/inserts (frame height, totals, banding), pick tables by header text,
  drop replaced image rels safely, never overwrite the original, and know the
  OOXML repair causes.
when_to_use: >
  Load when the provider-stripped model id (see stack-detection.md) resolves
  to exactly `longcat-2.5-preview-free` AND the repository contains a PowerPoint deck (`*.pptx` or
  `*.ppt` at the root or up to two directories deep, per meta.yaml detection).
  For any other model or a repo without decks, do not load.
tuned_for: longcat-2.5-preview-free
tuned_version: "2.5-preview"
stack: pptx
source_scorecard: ../scores/longcat-2.5-preview-free.md
threshold: 0.9
revalidate_when: model_version changes
---
# PowerPoint-editing corrections for longcat-2.5-preview-free

<!-- kaizen:digest -->
**PowerPoint edits (python-pptx): finish every table edit, never trust the XML blindly:**
1. Delete a row with `tbl.remove(tbl.tr_lst[i])`, then ALWAYS set `graphic_frame.height = sum(r.height for r in table.rows)`, fix totals, and re-apply explicit row fills. The frame height does not follow XML edits.
2. Pick the table by its header text (recurse into groups), match the row on exact stripped key-cell text, assert exactly one hit.
3. `drop_rel(rId)` counts only `r:id`, never `a:blip r:embed`: it always drops image rels. Check `//@r:embed | //@r:link` yourself first; drop the old rel after replacing a picture.
4. Save to a NEW file; the original stays untouched. Re-open and assert changed content AND unchanged remainder.
5. Read slide size from `prs.slide_width/height`; never assume 16:9.
<!-- /kaizen:digest -->

## row-delete: finish the edit

- `table.rows` is read-only; remove the element: `tbl = table._tbl; tbl.remove(tbl.tr_lst[i])`.
- The frame height (`p:xfrm` ext cy) is not updated by XML edits and does matter: set `gf.height = sum(r.height for r in gf.table.rows)`. Never "leave it".
- Update the total/subtotal the removed row fed.
- Explicit `a:tcPr/a:solidFill` banding does not re-alternate; reassign fills on every row after the deleted one. Style banding (`bandRow`) re-alternates by itself.
- A white cover shape or zero-height row leaves the text in the slide XML. Remove the `a:tr`, then check the old text is absent from every slide part.

## table-relayout: row and column adds

- New row: `copy.deepcopy` a body `a:tr`, `ref_tr.addnext(new_tr)`, set text through the first existing run, then update the total, set the frame height to the row-height sum, and check the new bottom against `prs.slide_height` and shapes below. Keep explicit banding alternating.
- New column: new `a:gridCol` plus one `a:tc` in EVERY `a:tr` (mind merged spans), copy neighbour cell formatting, shrink widths so their sum fits the frame/slide, set the frame width to that sum.

## table-locate: choose by content

- Filter `has_table` shapes (recurse into `GroupShape`) and keep the one whose first-row texts match the expected header; never `tables[0]`.
- Match the row on `cell.text_frame.text.strip() == key` and assert `len(hits) == 1`.
- Merges: one `a:tc` per grid column always exists; the origin has `gridSpan`/`rowSpan`, covered cells have `hMerge`/`vMerge` and stay in the XML. Write to the origin only.

## image-replace: rels and shared parts

- Replace via `add_picture` at the old geometry + `old_el.addnext(new_el)` + remove the old `p:pic`, or repoint `a:blip r:embed` to a new image part. Then drop the old rel if nothing else uses it.
- `drop_rel` only counts `r:id` references, not `a:blip r:embed`, so an image rel is always dropped even when another picture still uses it. Test `//@r:embed` and `//@r:link` for the rId before calling it.
- Deleting `p:pic` alone leaves the rel and `ppt/media/imageN.*` in the zip. Verify by listing the saved zip's media and hash-comparing against the old bytes.
- Image parts are shared across pictures and slides; never overwrite a part's blob, give the target its own new part.
- A logo that `slide.shapes` does not list lives on the layout/master; editing there changes every slide using it.

## cell-edit: formatting and knock-on updates

- `cell.text = ...` drops run formatting; set the first run's text and remove extra runs.
- After changing a value, update the row Amount, Total, and every other place it repeats (summary slide, chart data, notes).
- A longer text grows the row; widen the column or shrink the font, then check the frame bottom against the slide.

## verify-safety: save and validate

- Write the result to a new file and keep the original untouched; do not replace it, even after verification passes.
- Re-open and assert exact cell texts, totals and frame geometry; compare the other slides' XML/text with the original; search every slide part for text that should be gone; render if possible.
- Repair-prompt causes: `a:tc` count differing from `a:gridCol` count, an `a:tc` without `a:txBody` (or `txBody` without `a:p`), broken `gridSpan`/`hMerge`, elements out of schema order, duplicate `p:cNvPr` ids after copying shapes, dangling `r:embed` rIds. python-pptx validates none of these.
- A table placeholder without `insert_table`: add the table at the placeholder bbox and remove the empty placeholder.
