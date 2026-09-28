---
name: pptx-tuning-space-bunny-free
description: >
  Corrective pptx-editing guidance for space-bunny-free: locate tables by
  header text with a unique-row assert, python-pptx merge and placeholder
  model, widening merged cells when inserting a column, and syncing the
  frame height.
when_to_use: >
  Load when the provider-stripped model id (see stack-detection.md) resolves
  to exactly `space-bunny-free` AND the repository contains a PowerPoint deck (`*.pptx` or
  `*.ppt` at the root or up to two directories deep, per meta.yaml detection).
  For any other model or a repo without decks, do not load.
tuned_for: space-bunny-free
tuned_version: "alpha"
stack: pptx
source_scorecard: ../scores/space-bunny-free.md
threshold: 0.9
revalidate_when: model_version changes
---
# PowerPoint-editing corrections for space-bunny-free

<!-- kaizen:digest -->
**PowerPoint edits (python-pptx): find by content, keep merges valid:**
1. Pick the table by its HEADER text (e.g. first row reads "Item", "Qty"), never `tables[0]`, the first `has_table` shape, its name or its position; recurse into group shapes. Match the row by exact stripped cell text and assert exactly one match; zero or several = stop and ask.
2. Every `a:tr` has exactly one `a:tc` per `a:gridCol`, covered merge cells included (`hMerge="1"`/`vMerge="1"`), so `row.cells[c]` IS `table.cell(r, c)`; write text to the merge origin.
3. Inserting a column through a merged cell: raise the origin's `gridSpan` AND insert one more `hMerge="1"` `a:tc` in that row. Every row must end with as many `a:tc` as there are `a:gridCol`.
4. `insert_table()` exists only on a `TablePlaceholder`; a generic content placeholder is a `SlidePlaceholder` and raises AttributeError. There, `add_table()` at the placeholder's left/top/width/height and remove the placeholder's `p:sp`.
5. After removing or adding an `a:tr` through XML, set `graphic_frame.height = sum(row.height for row in table.rows)`: the frame height does not follow XML edits (only the `row.height` setter updates it).
<!-- /kaizen:digest -->

## table-locate: choose by content, prove uniqueness

- A slide (or its groups) can hold several tables; the first `has_table` shape is
  not "the invoice table", and neither is the one picked by `gf.name` or by its
  position on the slide. Filter `has_table` shapes (recurse into
  `GroupShape.shapes`) and keep the one whose first row's
  `cell.text_frame.text.strip()` values match the expected header.
- Match the row on `cell.text_frame.text.strip() == "Gadget D"`, not a substring.
  Collect all hits and assert `len(hits) == 1` before editing.

## pptx-model: merges and placeholders

- Every `a:tr` has one `a:tc` per `a:gridCol`, merged or not; no row ever has
  fewer. The origin carries `gridSpan`/`rowSpan` (`cell.is_merge_origin`,
  `span_width`, `span_height`); covered cells carry `hMerge`/`vMerge`
  (`cell.is_spanned`) and are separate `_Cell` objects. `row.cells[c]` and
  `table.cell(r, c)` are the same grid position. Text written to a covered cell
  lands in that hidden cell; write to the origin. `cell.merge()` /
  `cell.split()` exist.
- When deleting or inserting a row or column inside a merge, adjust the
  origin's `gridSpan`/`rowSpan` and add or remove the covered cells to match.
- `insert_table(rows, cols)` is a `TablePlaceholder` method only. The usual
  "Click to add content" placeholder is a `SlidePlaceholder` without it. The
  frame it returns is a `PlaceholderGraphicFrame` that is still a placeholder
  (`is_placeholder` is True). For a content placeholder, use
  `slide.shapes.add_table(rows, cols, ph.left, ph.top, ph.width, ph.height)`
  and then `ph._element.getparent().remove(ph._element)` so no empty prompt
  remains.

## Observed in live editing: merged rows need a covered cell for the new column

- When a new `a:gridCol` falls inside a merged cell's span (e.g. a Total label
  spanning Item..Unit Price), set the origin's `gridSpan` +1 and insert a
  deep copy of a covered `a:tc` (with `hMerge="1"`) after the origin. Then assert
  `len(tr.tc_lst) == len(gridCols)` for every row before saving.

## Observed in live editing: keep the frame in sync with the rows

- `tbl.remove(tr)` / `addnext(copy)` leave `p:graphicFrame/p:xfrm/a:ext cy` at the
  old total. Always finish a row delete or insert with
  `gf.height = sum(r.height for r in gf.table.rows)`, then check the new bottom
  against `prs.slide_height` and the shapes below.
