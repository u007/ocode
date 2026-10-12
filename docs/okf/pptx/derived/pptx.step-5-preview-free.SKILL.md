---
name: pptx-tuning-step-5-preview-free
description: >
  Corrective pptx-editing guidance for step-5-preview-free: locate tables by
  header text with a unique-row assert, one a:tc per gridCol even through
  merges, TablePlaceholder.insert_table, bottom-of-slide check after a row
  insert, and dropping the replaced image's relationship.
when_to_use: >
  Load when the provider-stripped model id (see stack-detection.md) resolves
  to exactly `step-5-preview-free` AND the repository contains a PowerPoint deck (`*.pptx` or
  `*.ppt` at the root or up to two directories deep, per meta.yaml detection).
  For any other model or a repo without decks, do not load.
tuned_for: step-5-preview-free
tuned_version: "5-preview"
stack: pptx
source_scorecard: ../scores/step-5-preview-free.md
threshold: 0.9
revalidate_when: model_version changes
---
# PowerPoint-editing corrections for step-5-preview-free

<!-- kaizen:digest -->
**PowerPoint edits (python-pptx): find by content, count cells per gridCol, finish the edit:**
1. Pick the table by its HEADER text (first row reads e.g. "Item", "Qty"), never the first `has_table` shape, `shape.name` or position; recurse into group shapes. Match the row on exact stripped cell text, collect ALL hits and assert exactly one; zero or several = stop and ask.
2. Every `a:tr` has exactly one `a:tc` per `a:gridCol`, spans included. A `gridSpan="2"` origin is still followed by its own `hMerge="1"` cell; covered cells are empty, never a copy of the origin text. Spans never reduce the cell count.
3. Inserting a column through a merge: bump the origin's `gridSpan` AND insert a deep copy of a covered `a:tc` (`hMerge="1"`) after it, in the same edit. Then assert `len(tr.tc_lst) == len(tblGrid.gridCol_lst)` for every row.
4. `TablePlaceholder.insert_table(rows, cols)` exists and returns a `PlaceholderGraphicFrame` in the placeholder's geometry. Only a generic content placeholder (`SlidePlaceholder`) lacks it: there, `add_table()` at the placeholder bbox and remove its `p:sp`.
5. After inserting a row (or widening a table), check `gf.top + gf.height <= prs.slide_height` and that the new frame does not overlap the shapes below it; move them or ask.
6. After repointing a picture's `a:blip r:embed` or removing a `p:pic`, drop the OLD image rel when nothing else in that part uses it. Check `//@r:embed | //@r:link` yourself: `drop_rel` counts only `r:id`, so it always drops image rels, including shared ones.
<!-- /kaizen:digest -->

## table-locate: choose by content, prove uniqueness

- A slide (or its groups) can hold several tables; the first `has_table` shape is
  not "the invoice table", and `shape.name` or position are not identities
  either. Filter `has_table` shapes (recurse into `GroupShape.shapes`) and keep
  the one whose first row's `cell.text_frame.text.strip()` values match the
  expected header.
- Match the row on `cell.text_frame.text.strip() == "Gadget D"` in the key
  column. Do not `break` on the first hit: collect every match and assert
  `len(hits) == 1` before editing.
- Covered cells of a merge (`hMerge`/`vMerge`) are empty; the text lives only
  in the origin. Do not expect a label to be repeated across spanned columns.

## pptx-model: merges and placeholders

- Every `a:tr` has one `a:tc` per `a:gridCol`, merged or not. The origin carries
  `gridSpan`/`rowSpan` (`cell.is_merge_origin`, `span_width`, `span_height`);
  the covered cells are separate `a:tc` elements with `hMerge`/`vMerge`
  (`cell.is_spanned`). "Enough cells after accounting for spans" is wrong: a row
  of a 4-column grid with a `gridSpan="2"` Total label still has four `a:tc`.
  `row.cells[c]` is `table.cell(r, c)`. Write text to the origin only.
- Deleting or inserting a row or column inside a merge is two edits that go
  together: adjust the origin's `gridSpan`/`rowSpan` AND add or remove the
  matching covered cells. Never one or the other.
- `insert_table(rows, cols)` is a real method on a `TablePlaceholder` and is
  the idiomatic way to fill a table placeholder; the result is a
  `PlaceholderGraphicFrame` in the placeholder's position and size. Only when
  the placeholder is a generic content placeholder (a `SlidePlaceholder`, no
  `insert_table`) use `slide.shapes.add_table(rows, cols, ph.left, ph.top,
  ph.width, ph.height)` and then `ph._element.getparent().remove(ph._element)`
  so no empty "Click to add text" box remains.

## table-relayout: finish a row insert, keep merged rows valid

- After `total_tr.addprevious(copy.deepcopy(body_tr))`, editing the runs and
  updating the total and `gf.height = sum(r.height for r in gf.table.rows)`,
  the edit is not finished: compare `gf.top + gf.height` with
  `prs.slide_height` (minus the margin the deck uses) and the new bottom with
  the top of every shape below the table. If it overflows or overlaps, shrink
  row heights, move the shapes below, or ask.
- Adding a column: insert the `a:gridCol`, then insert one `a:tc` in EVERY
  `a:tr` at that index. In a row where the new index falls inside a merge, do
  both: `origin.set("gridSpan", str(span + 1))` and insert a deep copy of an
  existing covered cell (`hMerge="1"`) right after the origin. A row-level
  "span coverage" count is not the invariant; `len(tr.tc_lst) ==
  len(tblGrid.gridCol_lst)` for every row is. Assert it before saving.

## image-replace: the old image must leave the package

- Repointing `pic._element.blipFill.blip.rEmbed` (python-pptx names the
  attribute `rEmbed`) or removing the `p:pic` leaves the slide part's
  relationship to the old image intact, so `ppt/media/imageN.*` is still saved.
  After the swap, find the old rId's other users in the part
  (`slide._element.xpath("//@r:embed | //@r:link")`) and, when none remain, drop
  it (`slide.part.drop_rel(old_rId)`). Then list the saved zip's `ppt/media`
  and confirm the old bytes are gone (hash compare).
- `drop_rel` does have a reference count, but `_rel_ref_count` matches only
  `//@r:id`. Pictures reference images with `a:blip r:embed`, so an image rel
  always counts 0 and is always dropped, even when another picture on the slide
  still uses it. A broken second picture after `drop_rel` is this, not a cloned
  `p:pic` sharing an rId. The guard is your own `r:embed`/`r:link` check before
  the call.
