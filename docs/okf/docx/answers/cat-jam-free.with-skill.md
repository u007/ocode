# cat-jam-free — WITH-SKILL closed-book answers for docx (opencode-go, 2026-10-14)
# Delivery: derived/docx.cat-jam-free.SKILL.md body (front matter stripped) prepended to _prompts/docx.md; one
# `ocode run -m opencode-go/cat-jam-free -effort med` session, isolated HOME with every tool denied, cwd holding only the sheet;
# session audited in sqlite: 1 user + 1 assistant message, 0 tool calls.

- id: docx-model-01
  answer: |
    A .docx is an Open Packaging Conventions ZIP archive: XML parts plus relationships, with the main story usually in word/document.xml. Table data lives in that story part (or a header/footer part), as w:tbl. Structure from top down: w:tbl -> w:tr (row) -> w:tc (cell) -> w:p (paragraph) -> w:r (run) -> w:t (text). Table-level metadata is in w:tblPr and w:tblGrid.
- id: docx-model-02
  answer: |
    "Widget C" may be split across multiple runs, e.g. "Wid" and "get C", so no single run.text equals it. Search paragraph.text instead, find the character span of the match, then write the replacement into the first run covering that span and set the text of every other covered run to "". Do not assign paragraph.text or rebuild runs, or run formatting is lost.
- id: docx-model-03
  answer: |
    Table and cell widths in WordprocessingML are in twentieths of a point (twips/dxa): 20 twips per point, 1440 twips per inch. Image sizes/extents (wp:extent, a:ext) are in EMU: 12700 EMU per point, 914400 EMU per inch.
- id: docx-locate-01
  answer: |
    Check document tables plus section header/footer tables and nested cell.tables. Identify the invoice table by unique header labels rather than position, then find rows where a cell's paragraph text exactly equals "Gadget D". Assert exactly one matching table and exactly one matching row before editing; zero or multiple matches means stop and report. Also inspect merged cells and the cell's number format before changing values.
- id: docx-locate-02
  answer: |
    row.cells has one entry per grid column. A horizontally merged cell is returned multiple times in that list, so index-based deletion can target the wrong physical cell or repeatedly touch the same merged cell. Counting row.cells counts grid positions, not distinct logical cells. Compare w:tc identities/gridSpan when deleting or counting.
- id: docx-rowdel-01
  answer: |
    No, python-docx has no public table.delete_row() or row.delete(). Use the underlying XML: tr = row._tr; tr.getparent().remove(tr). Word reflows the remaining rows without manual shifting.
- id: docx-rowdel-02
  answer: |
    Simply removing the row removes the vMerge restart, leaving following cells as orphaned continuation cells and can make the document invalid or cause Word to repair it. Correctly: copy the restart cell's paragraphs into the next row's cell, make that next cell the restart (or remove vMerge if no continuation follows), then remove the original row only. Then recompute dependent totals.
- id: docx-rowdel-03
  answer: |
    The Total usually still shows the old cached value because Word does not recompute fields automatically on open and there is no Word recalc-on-load setting. After deleting the row, recompute the total manually and write the value into the field's cached result run between fldChar separate and end, preserving the field and its number format. w:updateFields in settings.xml only prompts the user.
- id: docx-rowdel-04
  answer: |
    Hiding or zero-height formatting leaves the w:tr in the document, so the row and its text remain in the XML and can be extracted, counted, shown after unhide, or affect totals. A real deletion removes the w:tr. Prove it by checking that len(table.rows) decreased, no cell text matches, and the w:tr element is absent in the saved XML/zip.
- id: docx-cell-01
  answer: |
    cell.text = "SKU" replaces all runs with one plain run, so the bold, size, color, and font of the original run are lost. The w:tcPr properties such as shading and width survive. Set cell.paragraphs[0].runs[0].text = "SKU", blank the other covered runs, or copy the old run's rPr onto the new run.
- id: docx-cell-02
  answer: |
    Update the row Amount, then every dependent number: subtotal/Total, tax, grand total, SUM field cached results, and any figures in body text. Recompute each from the table data and write it through the cell's existing first run in the existing number format, decimals, currency, and alignment. Assert the target row matched exactly before editing.
- id: docx-cell-03
  answer: |
    With trackRevisions enabled, a direct text rewrite is untracked but can leave revision markup such as w:delText holding the old value. Decide with the user whether the edit must be tracked. For a clean edit, accept or reject existing w:ins/w:del in that region first, then confirm no w:delText still contains the old value before rewriting through the run.
- id: docx-relayout-01
  answer: |
    table.add_row() appends a row at the end, after the Total, and does not copy trPr, shading, or run formatting. For a middle row, deep-copy a styled body row, insert it before the Total with total_tr.addprevious(new_tr), then set each cell through that row's existing first run. Adjust its shading so alternating row banding stays correct.
- id: docx-relayout-02
  answer: |
    table.add_column(width) only appends a column on the right and widens the table. To add a column after Item, insert a w:gridCol after Item's gridCol and a deep copy of Item's w:tc after Item's cell in every row, accounting for gridSpan cells so spans still sum to the grid. Copy header/body formatting, then rebalance every gridCol and tcW so the full table fits page_width minus left and right margins.
- id: docx-relayout-03
  answer: |
    Word uses cell-level w:tcW widths and possibly tblW in addition to w:gridCol, so changing only gridCol leaves stale cell preferred widths. Update gridCol widths, every row's tcW, and tblW together. Keeping tblLayout consistent also helps avoid fallback to autofit.
- id: docx-tblins-01
  answer: |
    doc.add_table() places the table at the end of the document body. To place it after "Notes:", create the table then move its tbl element after the target paragraph element with paragraph._p.addnext(table._tbl), or insert at the corresponding body index.
- id: docx-tblins-02
  answer: |
    python-docx looks up the style by name in the target document's styles part. If the company template does not define "Grid Table 4 Accent 1", KeyError occurs. Use an available style such as "Table Grid", copy the style from a template that has it, or define borders via w:tblBorders.
- id: docx-tblins-03
  answer: |
    By default doc.add_table writes explicit widths across the full available text width, split evenly among columns. To match the invoice table, copy its style, gridCol widths, tcW values, tblW, alignment, and header run formatting rather than using evenly split defaults.
- id: docx-imgrep-01
  answer: |
    document.xml references the picture through w:drawing/wp:inline or wp:anchor, a:graphic, a:graphicData, pic:pic, then pic:blip with r:embed="rId". The rId resolves in the owning part's relationships to the media part. Replace one picture by calling part.get_or_add_image(path) on the part owning that blip, setting the blip's r:embed to the new rId, and keeping wp:extent/a:ext. Finish by dropping the old rel if no other r:embed in that part uses it.
- id: docx-imgrep-02
  answer: |
    Image parts are deduplicated and shared, so multiple r:embed references can resolve to the same bytes. Overwriting part._blob changes every picture using that part. Create a new image part instead and repoint only the target blip, then clean up the old rel if unused.
- id: docx-imgrep-03
  answer: |
    A page-wide logo is usually in header parts (default, first-page, even-page), often as a wp:anchor drawing, which doc.inline_shapes does not enumerate. Walk every section's header parts and footers if applicable, parse each part's XML for a:blip r:embed values, then replace through that header part's own rels. Clean up old rels per header part.
- id: docx-imgins-01
  answer: |
    doc.add_picture() appends a new paragraph with the picture at the end of the document. To place it after "Approved by:", add the picture to a new paragraph and move that paragraph after the target, or add a run to a paragraph immediately after it. Set only the width to Inches(1.5); let height follow the image's aspect ratio. Internally the size is stored in EMU.
- id: docx-imgins-02
  answer: |
    The image overflows the cell and can distort table layout; Word does not automatically crop it to the cell. Set the picture width to the cell width or less, for example Inches(1.2) or slightly less for padding, and let height remain proportional.
- id: docx-legacy-01
  answer: |
    python-docx cannot open .doc; it is binary OLE2. Convert it with LibreOffice headless only, for example soffice --headless --convert-to docx --outdir out report.doc, verify tables, rows, and images survived, edit the .docx, convert back to .doc only if required, and keep the original .doc.
- id: docx-legacy-02
  answer: |
    textutil can flatten tables into paragraphs or otherwise alter structure, losing rows/cells needed for a table edit. Detect this by opening the converted .docx and comparing table count, row counts, cell text, and image count/content with the source; if table structure is missing or altered, discard that conversion and use LibreOffice instead.
- id: docx-legacy-03
  answer: |
    An already-running LibreOffice instance can lock the user profile, causing headless conversion to exit without output. Use an isolated profile with -env:UserInstallation=file:///tmp/lo_profile_$$, avoid concurrent GUI instances, set --outdir, wait for conversion, and verify the output path exists.
- id: docx-verify-01
  answer: |
    Save to a new or backup-protected file, then validate programmatically: unzip/XML well-formedness, open with python-docx, confirm exact target table/row, expected text, row counts, dependent totals and field cached results, number formats, and images/rels. Also render with LibreOffice to catch corruption before replacing the original.
- id: docx-verify-02
  answer: |
    Common table-related structural mistakes include w:tc elements without w:p, w:tr elements without w:tc, gridSpan sums not matching w:tblGrid, orphaned w:vMerge continuation cells, invalid or empty rPr/run markup, mismatched XML namespaces, and leftover or removed relationship rIds. Also ensure no blunt text replacement removed required XML.
- id: docx-verify-03
  answer: |
    After repointing the blip, drop the old rel from the owning part if no other a:blip there references it. Then verify by scanning all document/header/footer XML for the old rId and listing the package; remove the orphaned media entry from the zip if necessary, because deleting only the rel may leave old bytes in word/media.
