- id: docx-model-01
  answer: |
    A .docx file is a ZIP archive containing XML parts. The main document text and structure live in word/document.xml. A table's data lives there as nested XML elements: w:tbl (table) contains w:tr (rows), each w:tr contains w:tc (cells), each w:tc contains w:p (paragraphs), each w:p contains w:r (runs), and each w:r contains w:t (text). Supporting parts include word/styles.xml, word/_rels/document.xml.rels (relationships to images, etc.), and [Content_Types].xml.

- id: docx-model-02
  answer: |
    Word splits text across multiple runs for various reasons (spell-check, formatting changes, editing history). "Widget C" might be split as "Widget" in one run and " C" in another, or even mid-word. Checking run.text == "Widget C" on each individual run fails because no single run contains the full string. To find text reliably, concatenate all w:t elements within the cell (or paragraph) and search the combined string. To replace reliably, identify which run(s) contain the target text, put the full replacement text in the first matching run, and clear the text of subsequent runs (or merge runs with identical formatting).

- id: docx-model-03
  answer: |
    Table and cell widths use twips (twentieths of a point): 1440 twips = 1 inch = 72 points. Image sizes use EMUs (English Metric Units): 914400 EMUs = 1 inch = 72 points. To convert: twips ÷ 20 = points, twips ÷ 1440 = inches. EMUs ÷ 914400 = inches, EMUs ÷ 914400 × 72 = points.

- id: docx-locate-01
  answer: |
    Iterate doc.tables and identify the invoice table by checking header row text (e.g., a cell containing "Item" or "Description") or by checking for characteristic content. To find the "Gadget D" row, iterate rows and check if any cell's concatenated text equals or contains "Gadget D". Before editing, verify: (1) the table has the expected number of columns, (2) the row is not a header or total row, (3) there are no unexpected merged cells in that row, (4) the cell text matches exactly (not a substring of a different item).

- id: docx-locate-02
  answer: |
    row.cells returns a list where a cell spanning two columns appears twice (the same cell object repeated for each grid column it covers). If you delete "the second cell" by index, you might delete a cell that is actually the merged continuation of the first cell, corrupting the table. Iterating row.cells to count columns overcounts because merged cells appear multiple times. The correct column count is len(table.columns), which accounts for the grid, not len(row.cells).

- id: docx-rowdel-01
  answer: |
    python-docx has no table.delete_row() or row.delete() method. To delete a row, remove its underlying XML element: row._element.getparent().remove(row._element). This removes the w:tr element from the w:tbl, which effectively deletes the row.

- id: docx-rowdel-02
  answer: |
    Removing the row with <w:vMerge w:val="restart"/> breaks the vertical merge. The subsequent rows' cells with <w:vMerge/> (continuation) will not display correctly because their restart anchor is gone—they may render as empty or cause display artifacts. To do it correctly: either remove all rows that are part of the merged group, or if keeping the merge, convert the restart to a continuation on the next row and remove the restart row, or unmerge the cells first (remove vMerge elements) then delete the row.

- id: docx-rowdel-03
  answer: |
    The Total may show a stale cached value until the field is recalculated. Word typically recalculates SUM(ABOVE) on open if updateFields is set in settings.xml, but if not, it shows the old value. To handle it: either set <w:updateFields w:val="true"/> in settings.xml so Word recalculates on open, or replace the field with a static computed value, or use a library that updates fields, or instruct the user to update fields (Ctrl+A, F9) after opening.

- idx-rowdel-04
  answer: |
    Setting height to 0, making text white, or marking runs hidden does not remove the row from the XML—the w:tr element and all its content remain in document.xml. The row still exists in the file structure, still occupies a row in the table's XML, and can be unhidden or recovered. To prove the row is really gone: unzip the .docx and verify the w:tr element is absent from document.xml, or open with python-docx and check that len(table.rows) decreased by one and the row's text no longer appears in any cell.

- id: docx-cell-01
  answer: |
    cell.text = "SKU" replaces all paragraph and run content with a single run containing "SKU". Cell-level formatting (shading, borders, width, vertical alignment) survives because it lives in w:tcPr. Run-level formatting is lost: bold, font size, color, font name, and the paragraph's formatting (alignment, spacing) revert to defaults. The new run inherits no formatting from the original content.

- id: docx-cell-02
  answer: |
    Changing Qty from 5 to 12 requires recalculating: (1) the line total (Qty × Unit Price) for that row, (2) the invoice subtotal/sum of all line totals, (3) any tax or grand total that depends on the subtotal, (4) any other cells that reference or summarize quantities. To make numbers consistent, recompute all dependent values and update those cells, or ensure fields will recalculate on open.

- id: docx-cell-03
  answer: |
    With Track Changes on (w:trackRevisions), python-docx edits are NOT recorded as tracked changes—python-docx does not create w:ins or w:del elements. The edit appears as a clean, untracked change. Consider: (1) whether the user wants the edit tracked (in which case you need to manually create w:ins/w:del elements or use a different approach), (2) whether to temporarily disable trackRevisions, (3) whether the edit will be visible in the review pane or appear as an accepted change.

- id: docx-relayout-01
  answer: |
    table.add_row() always appends a new row at the end of the table. To insert a row in the middle: create a new row (table.add_row() or deepcopy an existing row), then move its XML element to the correct position using addnext() or addprevious() on the target row's _element. For example: new_row = table.add_row(); target_row._element.addnext(new_row._element). Format the new row by copying formatting from an adjacent row.

- id: docx-relayout-02
  answer: |
    table.add_column(width) adds a new column at the end of the table: it adds a w:gridCol to w:tblGrid and appends a w:tc to every row. To add a column after Item (not at the end), you must manually insert the w:gridCol in the correct position in w:tblGrid and insert a w:tc in each w:tr at the correct position. To keep the table within margins: reduce the width of other columns so the total grid width does not exceed the text column width (page width minus left and right margins). Copy formatting from adjacent cells.

- id: docx-relayout-03
  answer: |
    Word may still show old widths because: (1) w:tblW (table width) is set to a specific value that overrides the grid, (2) w:tblLayout is not set to "fixed" so Word auto-fits and ignores gridCol, (3) individual cell widths (w:tcW) override the grid. To fix: set table.autofit = False, set w:tblLayout w:type="fixed" in w:tblPr, update w:tblW to the correct total, and ensure each cell's w:tcW matches its gridCol width.

- id: docx-tblins-01
  answer: |
    doc.add_table() appends the table at the end of the document body. To insert after a specific paragraph: create the table, then move its XML element. Find the target paragraph's _element, then use paragraph._element.addnext(table._element) to place the table immediately after that paragraph. Alternatively, create the table at the end and move it, or build the table element and insert it directly into the body at the correct position.

- id: docx-tblins-02
  answer: |
    KeyError occurs because the style "Grid Table 4 Accent 1" is not defined in the document's word/styles.xml. python-docx looks up styles by name in the document's style definitions. To get a ruled table: (1) use a style that already exists in the document (check table styles in the template), (2) add the style definition to styles.xml first, or (3) skip the style and apply direct formatting (borders via XML, shading, etc.) to achieve the desired look.

- id: docx-tblins-03
  answer: |
    A table from doc.add_table(rows, cols) defaults to auto-fit behavior with no explicit width set, meaning Word will size it based on content and available space. To match an existing invoice table: set table.autofit = False, set the table width explicitly (e.g., table.width = Inches(6.5)) or set w:tblW to the desired value in twips, set w:tblLayout to fixed, and set each column's width via w:gridCol and each cell's w:tcW.

- id: docx-imgrep-01
  answer: |
    A picture in document.xml references its image bytes via a relationship: the w:drawing element contains a blip with r:embed="rIdX", and word/_rels/document.xml.rels maps rIdX to the image part (e.g., word/media/image1.png). To replace one picture: find the blip's r:embed ID, look up the relationship to get the image part, and replace that part's blob (image_part._blob = new_bytes). Keep the same relationship ID and drawing element so position and size are preserved.

- id: docx-imgrep-02
  answer: |
    Two different pictures can share the same image part if they reference the same relationship target (e.g., both use rId5 pointing to word/media/image1.png). Overwriting image_part._blob changes the bytes for ALL references to that part. To replace only one picture: check if the image part is referenced by multiple relationships; if so, create a new image part, add a new relationship, and update only the target drawing's r:embed to point to the new relationship.

- id: docx-imgrep-03
  answer: |
    The logo is in the page header, not the document body. doc.inline_shapes only lists images in the main document flow. To find it: access the header via doc.sections[0].header (or doc.sections[0].first_page_header / even_page_header as appropriate), then look for inline shapes or drawing elements in the header's XML. Replace the image part referenced by the header's drawing element.

- id: docx-imgins-01
  answer: |
    doc.add_picture() appends the image at the end of the document as an inline shape in a new paragraph. To place it after "Approved by:": create the picture (which creates a drawing element), find the target paragraph's _element, and use addnext() to insert the picture's paragraph element after it. To keep aspect ratio: calculate height from width using the image's native aspect ratio (height = width × native_height / native_width).

- id: docx-imgins-02
  answer: |
    A 3-inch image in a 1.2-inch cell will overflow the cell, potentially distorting the table layout or pushing content outside margins. To size it: resize the image to fit within the cell width (1.2 inches or less), maintaining aspect ratio by calculating the proportional height. Set the image width explicitly and derive height from the image's native pixel dimensions.

- id: docx-legacy-01
  answer: |
    python-docx cannot open .doc (Word 97–2003) files—it only works with .docx (OOXML). Workflow: (1) convert .doc to .docx using LibreOffice (soffice --headless --convert-to docx file.doc) or macOS textutil (textutil -convert docx file.doc), (2) open the converted .docx with python-docx, (3) perform the table row deletion, (4) save. Note that conversion may alter some formatting.

- id: docx-legacy-02
  answer: |
    textutil conversion risks: (1) loss or alteration of complex formatting, (2) changes to table structure (merged cells, borders, shading may shift), (3) image quality degradation or format changes, (4) field codes may be converted to static text or broken, (5) custom styles may be renamed or lost. To detect: compare table count, row count, cell count, and cell text before and after conversion; check that images are still present; verify that the specific table and row you need to edit survived conversion intact.

- id: docx-legacy-03
  answer: |
    soffice --headless fails when a LibreOffice GUI window is open because they share the same user profile/lock file, causing a conflict. To run reliably from a script: use a separate user profile directory via -env:UserInstallation=file:///tmp/lo_profile (or a unique path), which prevents the lock conflict. Example: soffice --headless -env:UserInstallation=file:///tmp/lo_profile --convert-to docx file.doc.

- id: docx-verify-01
  answer: |
    Safe procedure: (1) Save to a NEW file (never overwrite the original until verified). (2) Open the new file with python-docx and verify table count, row count, cell contents, and that the target row is gone. (3) Unzip the .docx and verify the w:tr element is absent from document.xml. (4) If possible, render to PDF or open in LibreOffice to visually confirm. (5) Check that no other content was altered by comparing cell texts across the document. (6) Only replace the original after all checks pass.

- id: docx-verify-02
  answer: |
    Common structural mistakes: (1) incorrect element order within w:tc (w:tcPr must come before w:p), (2) missing w:tcPr when required, (3) removing a w:tc that is part of a merge without handling the merge, (4) leaving orphaned w:gridCol entries that no longer match the actual columns, (5) missing w:tblPr or w:tblGrid, (6) malformed XML (unclosed tags, wrong namespace), (7) removing a row that is the restart of a vertical merge without fixing continuations, (8) duplicate or missing w:tr elements.

- id: docx-verify-03
  answer: |
    To ensure the old image is not still inside the .docx: (1) unzip the file and list word/media/—check that no unreferenced image parts remain (though Word tolerates unreferenced parts). (2) Check word/_rels/document.xml.rels and any header rels files to confirm no relationship still points to the old image part. (3) Search document.xml and header XML for any remaining r:embed or r:id that referenced the old image. (4) If the old image part is still present but unreferenced, remove it from the zip and remove its relationship. (5) Verify the new image renders correctly by converting to PDF or opening in an application.
- id: docx-rowdel-04
  answer: >-
    Setting a row's height to 0 (exact) and making its text white or marking runs
    hidden is not a deletion because the `<w:tr>` element and all its child
    `<w:tc>` cells remain in `word/document.xml`. The row still exists in the
    document object model, still occupies a position in the table's row sequence,
    still contributes to the table's structural properties (tblGrid, column
    definitions), and its content is still present in the file bytes—merely
    visually concealed. Word, python-docx, pandoc, and any other OOXML parser
    will still see the row. It is a rendering trick, not a structural change;
    the row can be recovered by clearing the height override or un-hiding the
    runs.

    To prove a row is really gone:
    1. Parse `word/document.xml` and count the `<w:tr>` elements within the
       target `<w:tbl>`—the count must have decreased by one and the specific
       row element must be entirely absent from the XML tree.
    2. Use python-docx: `len(table.rows)` must reflect the deletion, and
       iterating `table.rows` must not yield the removed row.
    3. Confirm the row's unique content (e.g., a distinctive string from one of
       its cells) no longer appears anywhere in `document.xml`.
    4. Verify with an independent parser (e.g., `pandoc file.docx -t plain` or
       LibreOffice headless conversion) that the row's text does not appear in
       the extracted output.
    5. Optionally, diff the XML before/after to show the `<w:tr>...</w:tr>`
       subtree was removed rather than modified.
