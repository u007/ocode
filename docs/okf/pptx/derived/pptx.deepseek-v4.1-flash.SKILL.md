---
name: pptx-tuning-deepseek-v4.1-flash
description: >
  Corrective pptx-editing guidance for deepseek-v4.1-flash: locate tables by
  header text with a unique-row assert, python-pptx merge and placeholder API
  facts, XML row add/delete with run-preserving fills, totals and frame height,
  drop_rel on shared images, verification, and legacy .ppt round-trip.
when_to_use: >
  Load when the provider-stripped model id (see stack-detection.md) resolves
  to exactly `deepseek-v4.1-flash` AND the repository contains a PowerPoint deck (`*.pptx` or
  `*.ppt` at the root or up to two directories deep, per meta.yaml detection).
  For any other model or a repo without decks, do not load.
tuned_for: deepseek-v4.1-flash
tuned_version: "4.1"
stack: pptx
source_scorecard: ../scores/deepseek-v4.1-flash.md
threshold: 0.9
revalidate_when: model_version changes
---
# PowerPoint-editing corrections for deepseek-v4.1-flash

<!-- kaizen:digest -->
**PowerPoint edits (python-pptx): find by content, round-trip .ppt, sync the frame:**
1. Pick the table by its HEADER text (e.g. first row reads "Item", "Qty"), never `tables[0]` / the first `has_table` shape; recurse into group shapes. Match the row by exact stripped cell text and assert exactly one match; zero or several = stop and ask.
2. python-pptx merges are NOT python-docx's: every grid position is its own `_Cell` over its own `a:tc`; covered cells have `hMerge`/`vMerge`. Write text to the merge origin (`is_merge_origin`). `cell.merge()` / `cell.split()` exist.
3. python-pptx has NO row add or delete API (no `add_row`, no `rows.remove`): delete with `table._tbl.remove(tr)`, add by `copy.deepcopy` of a body `a:tr` + `addprevious`/`addnext`, then recompute the Total.
4. After removing or adding an `a:tr` through XML, set `graphic_frame.height = sum(row.height for row in table.rows)`: the frame height does not follow XML edits (only the `row.height` setter updates it).
5. Fill cells through the existing first run (`runs[0].text = ...`), never `cell.text = ...` (it drops the run's formatting); after an insert, keep the `new_tr` reference instead of re-indexing `rows[...]`.
6. `drop_rel(rId)` counts only `r:id` attributes, so an image rel (`a:blip r:embed`) is always dropped: first check the part has no other `//@r:embed` / `//@r:link` equal to that rId.
7. `.ppt` round-trips: `soffice --headless --convert-to pptx`, edit, then `soffice --headless --convert-to ppt` when the user needs `.ppt`. Keep the original.
<!-- /kaizen:digest -->

## pptx-model: API facts to get right

- Merge API exists: `cell.merge(other_cell)` and `cell.split()`; inspect with
  `is_merge_origin`, `is_spanned`, `span_width`, `span_height`.
- Placeholders: a table placeholder (`TablePlaceholder`) has
  `insert_table(rows, cols)`, which places the table at the placeholder's geometry.
  A generic "Title and Content" placeholder is a `SlidePlaceholder` without that
  method; only then add the table at the placeholder's left/top/width and remove
  the empty placeholder element.
- Slide size: read `prs.slide_width` / `prs.slide_height`, never assume one.
  python-pptx's own default template is 4:3 (9144000 × 6858000 EMU); a 16:9 deck
  is typically 12192000 × 6858000.
- python-pptx saves invalid XML without complaint (a row with fewer `a:tc` than
  `a:gridCol`, duplicate `p:cNvPr` ids). Other repair triggers to rule out after
  XML edits: `a:tc` without `a:txBody`, spans that don't add up, and an `r:embed`
  pointing at a dropped rel. Give every copied shape a fresh `cNvPr` id.

## table-locate: choose by content, prove uniqueness

- A slide (or its groups) can hold several tables; the first `has_table` shape is
  not "the invoice table". Filter `has_table` shapes (recurse into
  `GroupShape.shapes`) and keep the one whose first row's texts match the expected
  header.
- Match the row on `cell.text_frame.text.strip() == "Gadget D"`, not a substring.
  Collect all hits and assert `len(hits) == 1` before editing.
- Merges in python-pptx: every `a:tr` has one `a:tc` per `a:gridCol`. The origin
  carries `gridSpan`/`rowSpan` (`cell.is_merge_origin`, `span_width`), covered
  cells carry `hMerge`/`vMerge` (`cell.is_spanned`) and are separate `_Cell`
  objects. Text written to a covered cell lands in that hidden cell; write to the
  origin.

## row-delete: XML removal, then totals and a real check

- There is no row API in either direction: `Table`, `table.rows` and `_Row` offer no
  add, remove or delete. Delete with `tbl = table._tbl; tbl.remove(tbl.tr_lst[i])`.
- After the delete: recompute the Total (and any subtotal) from the remaining rows,
  and sync the frame height (see "Observed in live editing" below).
- Verify: save, re-open, and confirm the removed row's text no longer occurs in any
  `ppt/slides/*.xml` part of the zip.

## table-relayout: adding a row that keeps formatting

- `new_tr = copy.deepcopy(template_tr)`; `total_tr.addprevious(new_tr)`. After the
  insert the new row sits at the Total's old index, so `rows[total_idx - 1]` is the
  row before it; hold on to `new_tr` (or look up
  `table._tbl.tr_lst.index(new_tr)`).
- Set each cell's text via its existing first run (`runs[0].text = value`) and remove
  any further runs; `cell.text = value` rebuilds the run without its `rPr`
  (bold, size and colour are lost).
- Then recompute the Total, set the frame height to the sum of the row heights, and
  check the frame's new bottom against `prs.slide_height` and the shapes below it
  (move them or ask). Re-apply explicit banding fills if the table uses them.

## legacy-ppt: convert back when asked

- LibreOffice converts both ways: `soffice --headless --convert-to pptx --outdir out deck.ppt`, edit the
  `.pptx`, then `soffice --headless --convert-to ppt --outdir final out/deck.pptx`. Return `.ppt` if that is the
  format the user gave or asked for; keep the untouched original. Use a throwaway profile
  (`-env:UserInstallation=file:///tmp/lo_$$`) and check the output file exists.

## verify-safety: shared image rels and proof of removal

- `slide.part.drop_rel(rId)` drops when its reference count is under 2, and that
  count only looks at `//@r:id`. Pictures reference images with `a:blip/@r:embed`,
  so an image rel counts as 0 and is always dropped, even while another picture
  still uses it. Before dropping, query the part XML for `//@r:embed` and
  `//@r:link` equal to the rId; drop only if none remain.
- Removal is proven only by content: re-open the saved file and check the old text
  is absent from every slide XML part.
- For a `.ppt` source, tell the user the deck went through a conversion and deliver
  the format they asked for (`.ppt` back if required), plus the untouched original.

## Observed in live editing: keep the frame in sync with the rows

- `tbl.remove(tr)` / `addnext(copy)` leave `p:graphicFrame/p:xfrm/a:ext cy` at the
  old total. Always finish a row delete or insert with
  `gf.height = sum(r.height for r in gf.table.rows)`, then check the new bottom
  against `prs.slide_height` and the shapes below.
