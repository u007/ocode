# cat-jam-free — WITH-SKILL closed-book answers for docx (opencode-go, 2026-10-14)
# Delivery: derived/docx.cat-jam-free.SKILL.md body (front matter stripped) prepended to _prompts/docx.md; one
# `ocode run -m opencode-go/cat-jam-free -effort med` session, isolated HOME with every tool denied, cwd holding only the sheet;
# session audited in sqlite: 1 user + 1 assistant message, 0 tool calls.

- id: docx-model-01
  answer: |
    A .docx is a ZIP/OOXML package. Its table data lives in word/document.xml (and can appear in header/footer parts). The table is `<w:tbl>`; from table downward: `<w:tbl>` -> `<w:tr>` row -> `<w:tc>` cell -> `<w:p>` paragraph -> `<w:r>` run -> `<w:t>` text. Also `<w:tblGrid>/<w:gridCol>`, `<w:tblPr>`, `<w:trPr>`, `<w:tcPr>`, etc.
- id: docx-model-02
  answer: |
    Word splits text into multiple `<w:r>` runs, so an exact run-text comparison misses matches that span run boundaries. Match on `paragraph.text`, determine which runs the match covers by character offsets, write the replacement into the first covered run, and set every other covered run's text to "". Do not use `cell.text = ...` or rebuild runs when formatting must be preserved.
- id: docx-model-03
  answer: |
    WordprocessingML widths such as `w:tblW`, `w:tcW`, `w:gridCol` are in twentieths of a point (dxa): 1440 dxa = 1 inch, 20 dxa = 1 point. Image/drawing extents such as `wp:extent`/`a:ext` are in EMU: 914400 EMU = 1 inch, and 12700 EMU = 1 point.
- id: docx-locate-01
  answer: |
    Do not hard-code table/row indices. Walk main-body tables plus section header/footer tables and nested `cell.tables`; identify the invoice table by stable evidence such as its header row/caption/text, then find the Gadget D row by exact/normalized cell text. Assert exactly one matching row before editing; zero or multiple matches means stop and report. Also check merged cells and Track Changes before editing.
- id: docx-locate-02
  answer: |
    For a horizontally merged row, `row.cells` returns one entry per grid column, repeating the same cell object for spanned columns. Its length reflects grid columns, not visible logical cells. Deleting "the second cell" by index or using `len(row.cells)` to count columns can therefore touch the same merged cell twice or choose the wrong logical cell.
- id: docx-rowdel-01
  answer: |
    python-docx has no `table.delete_row()` or `row.delete()`. Remove the underlying XML row with `tr = row._tr; tr.getparent().remove(tr)` (or `row._tr.getparent().remove(row._tr)`). Save the document. Word reflows the remaining rows; then recompute every dependent total/field value manually.
- id: docx-rowdel-02
  answer: |
    Removing it directly orphanes the continuation cells and breaks the vertical-merge chain. Correctly: move the deleted row's cell paragraphs into the next row's cell, make that next cell the new restart (`w:vMerge w:val="restart"`), remove `w:vMerge` entirely if no continuation remains, then remove only the requested row. Word reflows the rest.
- id: docx-rowdel-03
  answer: |
    The Total still shows the stale cached result because Word does not recompute fields on open. After deletion, compute the new sum, write it into the field's cached result run (between `w:fldChar` separate/end), keep the field, and optionally set `w:updateFields` (which only prompts the user). Also recompute row Amount, subtotal, tax, grand total, and any figures in text.
- id: docx-rowdel-04
  answer: |
    Hiding or zero-heighting a row only makes it visually disappear; the `<w:tr>` remains, affecting XML structure, row counts, extraction, and any dependent calculations. Prove deletion by checking the saved XML has no that row, table row count decreased, and the row/cell text cannot be found.
- id: docx-cell-01
  answer: |
    `cell.text = "SKU"` replaces the cell's paragraphs/runs with one plain run, so bold, size, colour, font, and other run formatting are lost. Table-cell properties such as shading, borders, and width (`w:tcPr`) survive.
- id: docx-cell-02
  answer: |
    Recompute the row Amount from Qty * Unit price, then the Total and every other derived number (subtotal, tax, grand total), plus any numbers mentioned in surrounding text. Write each value through the cell's existing first run, preserving number format, decimals, separators, currency, and alignment. Keep the `{ =SUM(ABOVE) }` field while updating its cached result.
- id: docx-cell-03
  answer: |
    With `w:trackRevisions` on, a direct python-docx rewrite is an untracked change and the cell may already contain `w:ins`/`w:del`. Decide with the user whether the edit must be tracked. For a clean edit, accept/reject existing revisions in that region first and verify no `w:delText` still contains the old value; for a tracked edit, generate the appropriate revision markup.
- id: docx-relayout-01
  answer: |
    `table.add_row()` appends after the last row, including after Total, and copies no `trPr`, shading, or run formatting. To insert before Total, deep-copy a styled body row's `_tr`, call `total_row._tr.addprevious(new_tr)`, then write each cell's text through its existing first run and preserve alternating shading. Rebuild row-dependent totals afterward.
- id: docx-relayout-02
  answer: |
    `table.add_column(width)` appends a grid column and a cell at the right end of every row and widens the table. To insert after Item, manually insert a `w:gridCol` after Item's gridCol and a deep copy of Item's `w:tc` after Item's cell in every row, account for `gridSpan`/merged Total rows, copy header/body formatting, then set every `w:gridCol` and `w:tcW` so all widths fit text width minus margins.
- id: docx-relayout-03
  answer: |
    Word uses both `w:tblGrid/w:gridCol` and each cell's `w:tcPr/w:tcW`; if only gridCol changes, the cached cell widths remain. Update gridCol, every tcW, and `w:tblW` consistently. Otherwise Word can revert to the old cached cell widths.
- id: docx-tblins-01
  answer: |
    `doc.add_table()` appends the table at the end of the document body. To place it after a specific paragraph, create/add the table, then move its `<w:tbl>` element after that paragraph's `<w:p>` (for example `paragraph._p.addnext(table._tbl)`).
- id: docx-tblins-02
  answer: |
    KeyError occurs because the named built-in style is not present in that document's styles part (common with templates that do not include it). Use a style that exists in the document, add/import the style definition, or construct the ruled table manually with `w:tblBorders` (top/bottom/left/right/insideH/insideV) in `w:tblPr`.
- id: docx-tblins-03
  answer: |
    By default, `doc.add_table(rows, cols)` creates a table spanning the full text width and splits that width evenly among columns as explicit gridCol/tcW values. To match an existing invoice table, copy its style, gridCol/tcW widths, tblW, alignment, and header-run formatting.
- id: docx-imgrep-01
  answer: |
    Document XML references an image part indirectly: `a:blip` has `r:embed="rId"` and the owning part's relationship file maps rId to `word/media/...`. To replace one picture, call `part.get_or_add_image(path)` on the part that owns the blip, set that blip's `r:embed` to the new rId, keep `wp:extent`/`a:ext` (adjust one dimension if aspect ratio differs), and clean up the old rel if unused.
- id: docx-imgrep-02
  answer: |
    Image parts are deduplicated/shared in the package. Overwriting `image_part._blob` changes every XML reference to that part. Instead add a new image part and repoint only the target blip's `r:embed`, then drop the old rel from the owning part if no other `a:blip[@r:embed]` there references it.
- id: docx-imgrep-03
  answer: |
    A per-page logo usually lives in header parts (default, first-page, even-page), not `doc.inline_shapes`; it may be an anchored (`wp:anchor`) picture. Walk each section's `header`/`footer` parts and their paragraphs for `w:drawing`/`a:blip`; its rId is in that header/footer part's relationships. Replace the same way and repeat for every header/footer variant.
- id: docx-imgins-01
  answer: |
    `doc.add_picture()` appends at the end of the document body. Insert it after "Approved by:" by creating/ moving a paragraph after that paragraph, adding a run, and calling `run.add_picture(path, width=Inches(1.5))` with only width set to preserve aspect ratio. Do not set height.
- id: docx-imgins-02
  answer: |
    The image can overflow the 1.2-inch cell visually because Word does not automatically fit an inline image to the cell. Set the picture width to the cell width (or less), leave height unset so aspect ratio is preserved, and verify the result.
- id: docx-legacy-01
  answer: |
    No. python-docx cannot open binary `.doc`. Convert with LibreOffice headless to `.docx`, keep the original, verify tables/rows/images survived, edit the `.docx`, and convert back to `.doc` only if the user needs `.doc`.
- id: docx-legacy-02
  answer: |
    `textutil` conversion can go through rich text and flatten tables into paragraphs, losing table structure needed for row edits. Detect the damage by comparing converted table count, row counts, cell contents, and images against the source; otherwise convert with LibreOffice headless.
- id: docx-legacy-03
  answer: |
    A running LibreOffice instance locks the default user profile, so the headless command exits without producing output. Use a unique profile, e.g. `soffice -env:UserInstallation=file:///tmp/lo_profile_$$ --headless --convert-to docx --outdir out report.doc`, ensure the output directory exists, and check the resulting file.
- id: docx-verify-01
  answer: |
    Save to a new file instead of overwriting the original; keep a backup. Reopen with python-docx and check package validity, table/row/cell counts, expected cell text, field cached values, image counts/shapes, and possibly convert to PDF for visual sanity. Confirm no errors are raised on load.
- id: docx-verify-02
  answer: |
    Common table-edit causes: schema-invalid element order; mismatched `gridSpan`/`vMerge` with actual cell counts; changed `w:tblGrid` without matching `w:tcW`/`w:tblW`; orphaned or duplicate relationship IDs; bad namespace declarations; unclosed tags; leftover tracked-change text (`w:delText`) or unmatched field characters; invalid value types. These make Word report unreadable content.
- id: docx-verify-03
  answer: |
    After replacing the picture, drop the old relationship from the owning part if no other `a:blip[@r:embed]` there references it, then verify no part contains that old rId and no package media part contains the old image bytes/hash. Removing the drawing alone is not enough because the old image part can remain in the zip.
