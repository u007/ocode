---
name: docx-tuning-cat-jam-free
description: >
  Corrective Word-editing guidance for cat-jam-free: keep run formatting by
  writing into the first run, write field results yourself (Word does not
  recompute on open), insert rows and columns at the right place with copied
  formatting, drop the old image rel after a swap, and convert .doc only with
  LibreOffice headless.
when_to_use: >
  Load when the provider-stripped model id (see stack-detection.md) resolves
  to exactly `cat-jam-free` AND the repository contains a Word document
  (`*.docx` or `*.doc` at the root or up to two directories deep, per meta.yaml
  detection). For any other model or a repo without Word files, do not load.
tuned_for: cat-jam-free
tuned_version: "alpha"
stack: docx
source_scorecard: ../scores/cat-jam-free.md
threshold: 0.9
revalidate_when: model_version changes
---
# Word-editing corrections for cat-jam-free

<!-- kaizen:digest -->
**Word edits (python-docx): diagnosing the trap is not enough, finish the fix:**
1. Whenever you explain what `cell.text`/`paragraph.text` assignment loses, give the fix in the same answer. Replacing text keeps formatting only one way: find the match in `paragraph.text`, write the new text into the FIRST run that covered it, and set the other covered runs' text to "". Never `cell.text = …` / `paragraph.text = …` on formatted text, and never rebuild the runs.
2. Word does NOT recompute fields on open, and there is no "recalc on load" setting for Word (`calcPr`/`fullCalcOnLoad` is Excel). After any change that affects a `{ =SUM(ABOVE) }` or similar field, compute the value and write it into the field's cached result run. `w:updateFields` in settings.xml only makes Word prompt the user.
3. A row or value change is not done until every dependent number (row Amount, Total, subtotal, tax, grand total, figures in the text) is recomputed and written in the cell's existing number format, decimals, currency and alignment. 
3b. Row delete, say this sentence every time: "Remove the `w:tr`; Word reflows the remaining rows itself, so nothing is shifted manually; then recompute every dependent total."
4. Mid-table row: `deepcopy` a styled body row, `addprevious`/`addnext` it, then set each cell's text through that row's existing first run. Keep alternating shading alternating.
5. Column "after X": `add_column` only appends on the right. Insert a `w:gridCol` after X's gridCol and a deep copy of X's `w:tc` after X's cell in every row (mind `gridSpan` cells), copy header/body cell formatting, then rebalance every gridCol and tcW to fit the text width.
6. `doc.add_table` writes explicit widths: the text width split evenly across columns. To match a table, copy its style, gridCol + tcW, tblW and header formatting.
7. Image swap: add a new image part, repoint only this blip's `r:embed`, keep `wp:extent`. Every replace procedure you write ends with the cleanup step: drop the old rel from the owning part if no other `r:embed` there uses it, or the old media stays in the zip. Never overwrite a part's bytes. A header/footer picture's rId belongs to that header part's rels; walk every header (default, first, even) and footer.
8. Track Changes on: turning `trackRevisions` off does not clean the cell. Ask whether the edit must be tracked; if not, accept or reject the existing `w:ins`/`w:del` in that region and confirm no `w:delText` still holds the old value.
9. Find a row by its cell text (the cell that reads e.g. "Gadget D"), never by index, and assert exactly one row matches before editing; zero or several matches → stop and report.
10. `.doc` → convert only with LibreOffice headless (`soffice --headless --convert-to docx --outdir …`), keep the original, check tables/rows/images survived. Pandoc cannot read `.doc`; `textutil` flattens tables.
<!-- /kaizen:digest -->

## cell-edit: keep the formatting, write the numbers

- `cell.text = "SKU"` replaces every run with one plain run: bold, size, colour
  and font are gone (the tcPr shading/width survives). Instead, set
  `cell.paragraphs[0].runs[0].text = "SKU"` and set the other runs' text to "",
  or copy the old run's `rPr` onto the new run.
- After changing a quantity, recompute the row Amount, the Total and every other
  derived number from the table data, and write each value in the cell's existing
  format (decimals, thousands separators, currency) without touching paragraph
  alignment.
- A field's displayed value is a cached result (the runs between the `separate`
  and `end` fldChars). Neither python-docx nor Word on open recomputes it. Write
  the recomputed value into that result run and keep the field. Optionally add
  `<w:updateFields w:val="true"/>` to settings.xml, which only prompts the user.
- With `w:trackRevisions` on, a direct python-docx edit is untracked. Decide with
  the user: tracked edit (write `w:del`/`w:ins`) or clean edit (accept or reject
  existing revisions in that region first). Then check that no `w:delText` still
  contains the old value.

## docx-model / table-locate: split runs

- "Widget C" can be the runs "Wid" + "get C". Match on `paragraph.text`, then
  write the replacement into the first run of the match and blank the remaining
  covered runs, so the first run's formatting carries over.
- Tables in headers and footers are not in `doc.tables`; walk
  `section.header.tables` / `section.footer.tables` too, and nested tables via
  `cell.tables`.

## row-delete: what follows the XML removal

- `tr = row._tr; tr.getparent().remove(tr)`. Word reflows the remaining rows.
- Then update every number that depended on the row (Totals, field results as
  above).
- Deleting a `w:vMerge w:val="restart"` row: move its cell paragraphs into the
  next row's cell, make that cell the restart (drop `vMerge` if no continuation
  follows), then delete only the requested row.

## table-relayout / table-insert: placement and formatting

- `table.add_row()` appends after the Total and copies no trPr, shading or run
  formatting. Deep-copy a matching body row, insert it before the Total with
  `total_tr.addprevious(new_tr)`, and write text through each cell's existing run.
- `table.add_column(width)` adds a gridCol and a tc at the right end of every row
  and widens the table. For a column after Item, insert the gridCol and a deep copy
  of the Item tc at Item's position in every row, handle `gridSpan` cells (a merged
  Total row) so each row's spans still sum to the grid, copy the header cell's
  shading/bold/colour for the new header and the body style for body cells, then
  set every gridCol and tcW so the total fits
  `page_width − left_margin − right_margin`.
- `doc.add_table(rows, cols)` splits the text width evenly into explicit
  gridCol/tcW values. To match an existing table, copy its style, gridCol and tcW
  values, tblW and alignment, and format header runs to match.

## image-replace: rels belong to a part, and they do not leave on their own

- Replace one picture: `rId, _ = part.get_or_add_image(path)` on the part that owns
  the blip, set that blip's `r:embed` to `rId`, keep `wp:extent`/`a:ext` (adjust
  one dimension if the aspect ratio differs).
- Do not overwrite an image part's bytes: parts are deduplicated and shared, so
  every picture using it changes.
- The old rel and `word/media/imageN.*` stay in the package until you drop the rel
  from the owning part, which is safe only when no other `a:blip[@r:embed]` in that
  part still references it.
- A logo on every page lives in header parts (default, first-page, even-page). Its
  rId is in the header part's own rels, not the document part's. It may be a
  `wp:anchor` picture, which `doc.inline_shapes` never lists.

## legacy-doc: one faithful converter (low-n)

- `.doc` is binary OLE2; python-docx cannot open it and renaming does not help.
- Convert with LibreOffice headless only:
  `soffice -env:UserInstallation=file:///tmp/lo_profile_$$ --headless --convert-to docx --outdir out report.doc`,
  check that the output exists and its tables, rows and images match the source,
  edit the .docx, and convert back with `--convert-to doc` only if the user needs
  .doc. Keep the original.
- Pandoc cannot read `.doc`. `textutil` goes through rich text and can flatten
  tables into paragraphs; do not use either for a table edit.
