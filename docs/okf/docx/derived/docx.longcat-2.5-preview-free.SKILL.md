---
name: docx-tuning-longcat-2.5-preview-free
description: >
  Corrective Word-editing guidance for longcat-2.5-preview-free: merged-cell
  indexing, unique row matching, vertical-merge and field-total handling on row
  delete, run-preserving cell edits, table/column layout, default table width,
  and replacing one picture without touching shared parts.
when_to_use: >
  Load when the provider-stripped model id (see stack-detection.md) resolves
  to exactly `longcat-2.5-preview-free` AND the repository contains a Word document (`*.docx` or
  `*.doc` at the root or up to two directories deep, per meta.yaml detection).
  For any other model or a repo without Word files, do not load.
tuned_for: longcat-2.5-preview-free
tuned_version: "2.5-preview"
stack: docx
source_scorecard: ../scores/longcat-2.5-preview-free.md
threshold: 0.9
revalidate_when: model_version changes
---
# Word-editing corrections for longcat-2.5-preview-free

<!-- kaizen:digest -->
**Word edits (python-docx): merges, fields, runs, widths, pictures:**
1. `row.cells` is one entry per GRID column; a `gridSpan` cell repeats as the same `_tc`. Dedupe by `_tc` or walk `row._tr.tc_lst` with `gridSpan` before indexing or deleting; never count columns from `row.cells`.
2. Match a row by cell text and assert exactly one match; zero or several means stop.
3. Deleting a vMerge `restart` row: move its content into the next row's cell and set `w:vMerge w:val="restart"` there (drop vMerge if one row remains), then remove the `w:tr`. Word reflows; then fix totals.
4. A field's result is cached text between the `separate` and `end` fldChars. Write the recomputed value into that result run, keeping the field. `w:updateFields` only makes Word ask on open.
5. `cell.text =` drops run formatting. Set the first run's text and clear the rest, or copy the old `rPr`. Recompute every dependent number in the existing format and alignment.
6. With tracking on, direct edits make no `w:ins`/`w:del`; resolve existing revisions and check `w:delText` for the old value.
7. `add_row` appends below Total with width-only cells. Deepcopy a styled body row, insert with `addnext`/`addprevious`, fill via its existing runs, keep banding alternating.
8. `doc.add_table(rows, cols)` splits the section text width evenly across columns. Match an existing table by its style, gridCol + tcW widths and header formatting.
9. Replace ONE picture: `rId, _ = doc.part.get_or_add_image(path)`, repoint that blip's `r:embed`, keep `wp:extent`. Never overwrite `image_part._blob` (the part may be shared). Drop the old rel only when no blip still uses it.
<!-- /kaizen:digest -->

## table-locate: grid vs cells, unique matches

- `row.cells` returns one entry per grid column. A `w:gridSpan="2"` cell
  appears twice as the same `_Cell`/`_tc`, and a row holds fewer `w:tc` than
  grid columns. Indexes from `row.cells` do not map to `w:tc` elements. Dedupe
  by `_tc`, or iterate `row._tr.tc_lst` and accumulate `gridSpan`, before
  indexing or deleting. `len(table.columns)` gives the grid width, which is not
  a substitute for resolving the cell you mean.
- Identify the table by header text, then match the row by stripped cell text
  and assert exactly one match. Zero or several: stop and ask.

## row-delete: merges and cached totals

- Row removal is `tr = row._tr; tr.getparent().remove(tr)`; Word reflows, so
  there is no gap to close. What remains is every total that depended on it.
- If the row's cell holds `<w:vMerge w:val="restart"/>`, the continuation cells
  below lose their start. First move the content to the next row's cell in that
  column and mark it `restart` (drop `vMerge` if only one row remains in the
  merge). Do not turn the restart into a continuation.
- A `{ =SUM(ABOVE) }` field keeps its cached result (runs between `separate`
  and `end`). Compute the new total and write it into that result run, keeping
  the field. `<w:updateFields w:val="true"/>` is optional and only prompts the
  user; replacing the field with a static number loses the field.

## cell-edit: keep run formatting, update derived values

- `cell.text = "x"` replaces everything with one plain run: bold, size, colour
  and font are lost while `tcPr` shading and width stay. Instead set the first
  existing run's text and clear the rest, or copy the old run's `rPr`.
- After changing an input, recompute row amount, subtotal, tax, grand total and
  repeated figures from the table data, in the existing number format and cell
  alignment; field results get the new value written into their result run.
- With `w:trackRevisions` on, python-docx writes no `w:ins`/`w:del`. Decide
  whether the edit must be tracked; otherwise resolve existing revisions in the
  region first and check that `w:delText` no longer holds the old value.

## table-relayout: rows, columns and widths

- `table.add_row()` appends below the Total and copies only widths, no `trPr`,
  shading or run formatting. For a mid-table row, `copy.deepcopy(row._tr)` from
  a correctly styled body row, insert with `total_tr.addprevious(new_tr)` or
  `ref_tr.addnext(new_tr)`, and set each cell's text through its existing run.
  Choose the source row so alternating shading still alternates.
- Widths live in `tblGrid/gridCol`, every cell's `tcW` and `tblW`; change them
  together and use `w:tblLayout w:type="fixed"` when they must hold exactly.

## table-insert: default width and matching

- `doc.add_table(rows, cols)` is not content-sized: the section text width
  (page width minus margins) is split evenly across the columns. To match an
  existing table, reuse its style, its `gridCol` and per-cell `tcW` widths, its
  alignment, and its header formatting (shading, bold/white runs).

## image-replace: new part, repoint, never overwrite

- A picture is `a:blip r:embed="rIdX"`; the rel in the owning part (document
  or header) points at `word/media/imageN.*`; size comes from `wp:extent`.
- Do not assign `image_part._blob`: image parts are shared and deduplicated, so
  every blip using the part changes. Add a new part with
  `rId, _ = doc.part.get_or_add_image(path)` (use the header part's `.part`
  for header pictures), set this blip's `r:embed` to `rId`, keep the extents.
- Remove the old relationship only when no `a:blip/@r:embed` in that part
  still uses it, so the old media does not stay in the zip.
