---
name: pptx-tuning-glm-5.3-flash
description: >
  Corrective pptx-editing guidance for glm-5.3-flash: locate tables by header
  text (searching groups) with a unique-row assert, drop replaced image rels
  safely (drop_rel does not see r:embed), sync the graphic frame height after
  XML row edits, and re-band explicit row fills.
when_to_use: >
  Load when the provider-stripped model id (see stack-detection.md) resolves
  to exactly `glm-5.3-flash` AND the repository contains a PowerPoint deck (`*.pptx` or
  `*.ppt` at the root or up to two directories deep, per meta.yaml detection).
  For any other model or a repo without decks, do not load.
tuned_for: glm-5.3-flash
tuned_version: "5.3"
stack: pptx
source_scorecard: ../scores/glm-5.3-flash.md
threshold: 0.9
revalidate_when: model_version changes
---
# PowerPoint-editing corrections for glm-5.3-flash

<!-- kaizen:digest -->
**PowerPoint edits (python-pptx): find by content, sync the frame, re-band:**
1. Pick the table by its HEADER text in code (first-row texts equal the expected header, e.g. "Item", "Qty"), never `tables[0]`; recurse into group shapes, since top-level `slide.shapes` misses a grouped table.
2. Match the row by exact `cell.text_frame.text.strip()` on the key column, not a substring; assert exactly one hit, zero or several = stop and ask.
3. `part.drop_rel(rId)` only counts `r:id` refs, never `a:blip r:embed`: it ALWAYS drops an image rel, even one another picture still uses. Check `//@r:embed | //@r:link` for other users yourself first.
4. After replacing or deleting a picture, drop the old image rel once nothing uses it: an orphan rel keeps the old image bytes in the saved .pptx.
5. After removing or adding an `a:tr` through XML, set `graphic_frame.height = sum(row.height for row in table.rows)`: the frame height does not follow XML edits.
6. Explicit per-cell banding fills do not re-alternate after a row delete/insert: reassign the fill of every body row afterwards.
<!-- /kaizen:digest -->

## table-locate: choose by content, prove uniqueness

- A slide (or its groups) can hold several tables; the first `has_table` shape is
  not "the invoice table". Filter `has_table` shapes and keep the one whose first
  row's texts match the expected header, in code, not as a comment or fallback.
- Top-level `slide.shapes` does not list shapes inside a group: recurse into
  `GroupShape.shapes` (`shape.shape_type == MSO_SHAPE_TYPE.GROUP`).
- Match the row on the key column with `cell.text_frame.text.strip() == "Gadget D"`,
  not `"Gadget D" in cell.text` across every cell. Collect all hits and assert
  `len(hits) == 1` before editing.
- Merges in python-pptx: every `a:tr` has one `a:tc` per `a:gridCol`. The origin
  carries `gridSpan`/`rowSpan` (`cell.is_merge_origin`, `span_width`), covered
  cells carry `hMerge`/`vMerge` (`cell.is_spanned`) and are separate `_Cell`
  objects. Text written to a covered cell lands in that hidden cell; write to the
  origin. `cell.merge()` / `cell.split()` exist.

## image-replace: drop the old image rel, but check r:embed yourself

- Replacing a picture (repoint `a:blip r:embed` to a new
  `part.get_or_add_image_part(...)` rId, or `add_picture` at the old geometry +
  `old_el.addnext(new_el)` + remove the old `p:pic`) is not finished until the
  old image rel is dropped when nothing else in that part uses it. An unused rel
  is not harmless: python-pptx saves every part reachable through rels, so the
  old `ppt/media/imageN.*` stays in the file.
- `XmlPart.drop_rel(rId)` drops when its reference count is < 2, and that count
  only matches `//@r:id`. Pictures reference images with `a:blip r:embed`, so an
  image rel always counts 0 and is always dropped, even while another `p:pic`
  still points at it (identical image bytes on one slide share one rId).
- Safe drop:
  `if not [v for v in part._element.xpath("//@r:embed | //@r:link") if v == rId]: part.drop_rel(rId)`
- Confirm on the saved file: list `ppt/media/*` in the zip and hash-compare
  against the old image bytes.
- Repointing `r:embed` keeps position, size and crop, but may distort an image
  with a different aspect ratio: check it and adjust the extents if needed.

## Observed in live editing: keep the frame in sync with the rows

- `tbl.remove(tr)` / `addnext(copy)` leave `p:graphicFrame/p:xfrm/a:ext cy` at the
  old total. Always finish a row delete or insert with
  `gf.height = sum(r.height for r in gf.table.rows)`, then check the new bottom
  against `prs.slide_height` and the shapes below.

## Observed in live editing: re-band explicit fills

- When rows carry explicit `a:tcPr/a:solidFill` banding (not the table style's
  `bandRow`), deleting or inserting a row breaks the alternation. Reassign the fill
  of every body row after the change (even rows one colour, odd rows the other).
