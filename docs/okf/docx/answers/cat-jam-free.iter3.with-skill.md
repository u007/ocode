# cat-jam-free — WITH-SKILL closed-book answers for docx (opencode-go, 2026-10-14)
# Delivery: derived/docx.cat-jam-free.SKILL.md body (front matter stripped) prepended to _prompts/docx.md; one
# `ocode run -m opencode-go/cat-jam-free -effort med` session, isolated HOME with every tool denied, cwd holding only the sheet;
# session audited in sqlite: 1 user + 1 assistant message, 0 tool calls.

- id: docx-model-01
  answer: |
    A .docx is a ZIP (Open Packaging Convention) package of XML parts plus media and relationship files. Table data lives in a part such as word/document.xml (or a header/footer part), as a `w:tbl` containing `w:tblPr`, `w:tblGrid` with `w:gridCol` entries, repeating `w:tr` rows; each row contains `w:tc` cells; each cell contains `w:p` paragraphs; each paragraph contains `w:r` runs; each run contains `w:t` text. Cells also have `w:tcPr`, rows `w:trPr`.

- id: docx-model-02
  answer: |
    The text is split across multiple `w:r` runs, so a run's `text` need not equal the whole cell text. Match against `paragraph.text`, map the match to the runs by character offsets, then write the replacement into the first covered run and set the other covered runs' text to `""`. Preserve any unmatched prefix or trailing text in the first/last covered run as needed; never use `cell.text = ...` or `paragraph.text = ...` on formatted text and never rebuild the runs.

- id: docx-model-03
  answer: |
    Table and cell widths are in twentieths of a point (dxa/twips): 1440 dxa per inch and 20 dxa per point. Image sizes are in EMU: 914400 EMU per inch and 12700 EMU per point. Convert dxa by dividing by 20 for points or 1440 for inches; EMU by 12700 for points or 914400 for inches. Font sizes are separate half-point units.

- id: docx-locate-01
  answer: |
    Do not rely on table index alone. Recursively inspect `doc.tables` plus `section.header.tables` and `section.footer.tables`, and nested tables through `cell.tables`. Identify the invoice table by stable structural clues such as header labels, captions, or surrounding paragraph text, and find the Gadget D row by matching the relevant cell's text exactly. Assert that exactly one table and exactly one row match; if zero or multiple match, stop and report. Before editing, verify expected rows/cells, merged cells, style, grid widths, and any dependent totals or fields.

- id: docx-locate-02
  answer: |
    For a horizontally merged cell, `row.cells` returns one `_Cell` per grid column, so a cell spanning two columns appears twice as the same underlying `w:tc`. Deleting "the second cell" by index can target the duplicate alias rather than a real cell, and `len(row.cells)` overcounts merged columns. Count or address columns from `w:tc` elements and `gridSpan`, not from `row.cells`.

- id: docx-rowdel-01
  answer: |
    python-docx has no `table.delete_row()` or `row.delete()`. Remove the underlying XML row directly: `tr = row._tr; tr.getparent().remove(tr)`. Then recompute all dependent numbers (row Amount, Total, subtotal, tax, grand total, and text figures).

- id: docx-rowdel-02
  answer: |
    Removing only the restart row leaves the following rows' cells as dangling `w:vMerge` continuations with no restart, which can corrupt the table or produce odd rendering. Instead, move the deleted restart cell's paragraphs into the next row's cell, make that next cell the restart (change its continuation `w:vMerge` to `w:vMerge w:val="restart"`, or add it; drop `w:vMerge` only if no continuation follows), then remove only the requested row.

- id: docx-rowdel-03
  answer: |
    Word does not recompute fields on open, so the Total shows its cached result, likely the old value. Compute the new sum yourself and write it into the field's cached result run between `w:fldChar separate` and `w:fldChar end`, while keeping the field. Optionally add `w:updateFields w:val="true"` to settings.xml, but that only prompts the user. Also recompute every other dependent number in its existing number format.

- id: docx-rowdel-04
  answer: |
    Setting row height to zero, white text, or hidden runs does not remove the `w:tr`; the row remains in the package and still counts in indexing, XML, export, and accessibility. To prove deletion, reopen the file and check the row count and XML, or inspect the saved zip for the `w:tr`; a truly deleted row has no corresponding `w:tr` element.

- id: docx-cell-01
  answer: |
    `cell.text = "SKU"` replaces the cell's paragraphs/runs with one plain run, so bold, size, color, and font are lost. The tcPr properties such as shading, borders, and width survive. Write `cell.paragraphs[0].runs[0].text = "SKU"` and clear the other runs' text instead, preserving run `rPr`.

- id: docx-cell-02
  answer: |
    Recompute the row Amount, then Total and every other derived number such as subtotal, tax, grand total, quantities, counts, and figures in the text. Write each value in the cell's existing number format, decimals, thousands separators, currency, and alignment without changing alignment. For Word fields, update their cached result runs too.

- id: docx-cell-03
  answer: |
    With `w:trackRevisions` enabled, a direct python-docx text rewrite is not tracked as a revision. Existing `w:ins`/`w:del` in that region may still contain old and new values, including `w:delText`. First decide whether the edit should be tracked; if not, accept or reject existing revisions in that region, then edit through the existing run and verify no `w:delText` still contains the old value.

- id: docx-relayout-01
  answer: |
    `table.add_row()` appends a row at the end of the table and does not copy trPr, shading, or run formatting. To insert after "Service G" and before Total, deep-copy a matching styled body row, insert it before the Total row with `total_tr.addprevious(new_tr)`, then write text through each cell's existing first run and keep alternating shading alternating.

- id: docx-relayout-02
  answer: |
    `table.add_column(width)` appends a column at the right end of every row and widens the table; it does not insert after Item. To add a column after Item, insert a `w:gridCol` after Item's gridCol and deep-copy Item's `w:tc` after Item's cell in every row, accounting for `gridSpan` cells such as merged Total rows. Copy header/body formatting, then rebalance every `w:gridCol` and `w:tcW` so the total fits `page_width - left_margin - right_margin`.

- id: docx-relayout-03
  answer: |
    Per-cell `w:tcW` and `w:tblW` can override or disagree with `w:gridCol`, and a table may use autofit or fixed layout. Update all three consistently: `w:tblGrid` columns, every cell's `w:tcW`, and `w:tblW`. If needed, set the table layout explicitly so Word does not recalculate from content.

- id: docx-tblins-01
  answer: |
    `doc.add_table()` always appends the table at the end of the document body. To place it after "Notes:", create the table, then move its `w:tbl` element directly after that paragraph's element, for example `notes_paragraph._p.addnext(table._tbl)`.

- id: docx-tblins-02
  answer: |
    The style name is not present in that template's styles part, so python-docx raises KeyError. Use a style that exists, such as `Table Grid` if available, or build the ruled table by copying an existing table's style, grid, widths, and header formatting, or by adding table borders manually in the XML.

- id: docx-tblins-03
  answer: |
    By default, `doc.add_table` creates a table spanning the available text width with columns split evenly. To match the existing invoice table, copy its style, `w:tblGrid`, `w:tcW`, `w:tblW`, alignment, and header run formatting rather than relying on the default.

- id: docx-imgrep-01
  answer: |
    A picture is referenced by `w:drawing`, then `wp:inline`/`wp:anchor`, `a:graphic`, `a:graphicData`, `pic:pic`, and `pic:blip` with `r:embed`, which points to a relationship in the owning part's rels; that relationship targets the image bytes in `word/media`. To replace one picture, call `rId, _ = owning_part.get_or_add_image(new_path)`, set only that blip's `r:embed` to the new rId, and keep `wp:extent`/`a:ext` (adjusting one dimension if the aspect ratio differs). Then drop the old rel from the owning part only if no other `a:blip[@r:embed]` in that part still uses it.

- id: docx-imgrep-02
  answer: |
    Image parts and bytes can be shared or deduplicated, so overwriting `image_part._blob` changes every picture that references the same part, not just the intended one. Instead, add a new image part and repoint only the target blip's `r:embed`.

- id: docx-imgrep-03
  answer: |
    The logo is in header parts, not the document body; it may be in default, first-page, or even-page headers, and may be anchored in `wp:anchor`, which `doc.inline_shapes` does not list. Walk every section's header and footer (default, first, even), inspect their XML for `w:drawing`/`a:blip`, and use each header/footer part's own rels for the `r:embed` replacement.

- id: docx-imgins-01
  answer: |
    `doc.add_picture()` appends an inline picture at the end of the document body. To place it after "Approved by:", create and size the picture run, then move its paragraph directly after that paragraph's element with `paragraph._p.addnext(picture_paragraph._p)`. Set width to 1.5 inches and leave height automatic to preserve aspect ratio.

- id: docx-imgins-02
  answer: |
    An image wider than the cell overflows the cell and may disrupt table layout or extend into margins. Size it no wider than the cell, accounting for cell margins, and set only the width so python-docx preserves the aspect ratio.

- id: docx-legacy-01
  answer: |
    python-docx cannot open `.doc` binary OLE2 files. Convert first with LibreOffice headless, keep the original, edit the resulting `.docx`, and convert back to `.doc` only if needed. Verify that tables, rows, and images survived the conversion.

- id: docx-legacy-02
  answer: |
    `textutil` converts through rich text and can flatten tables into paragraphs, losing the table structure needed for row edits. Detect this by inspecting the converted file's table count, row/cell counts, and images; if tables are missing or flattened, use LibreOffice headless instead.

- id: docx-legacy-03
  answer: |
    A running LibreOffice desktop instance or a locked user profile can prevent headless conversion or leave no output. Use a unique `UserInstallation` such as `-env:UserInstallation=file:///tmp/lo_profile_$$`, make sure `--headless` is used, and avoid conflicting instances; then check that the output file exists.

- id: docx-verify-01
  answer: |
    Keep the original, write to a new file, then reopen it with python-docx and validate the expected structure: table/row counts, target row, text, widths, formatting, fields, and images. Also verify package integrity by inspecting the zip and, if possible, render or open it in a viewer/Word to confirm no repair prompt.

- id: docx-verify-02
  answer: |
    Common structural mistakes include a `w:tr` whose cell count does not match the table grid, mishandled `gridSpan` in merged rows, dangling `w:vMerge` continuations, invalid or misplaced XML elements or namespaces, missing required child elements such as `w:p` in a cell, and malformed field markup such as unmatched `w:fldChar` elements.

- id: docx-verify-03
  answer: |
    After repointing the blip and dropping the old rel, reopen and search all XML parts for the old `r:embed` value; it should be absent. Also inspect the saved package media listing and, if the old image part is otherwise unreferenced, confirm its file is no longer present. Do not merely overwrite image bytes.
