- id: docx-model-01
  answer: |
    A .docx file is a ZIP archive containing XML parts (primarily word/document.xml, word/styles.xml, word/_rels/document.xml.rels, [Content_Types].xml, and word/media/ for images). A table's data lives in word/document.xml. The XML element hierarchy from table to text is: w:tbl (table) contains w:tr (rows), each w:tr contains w:tc (cells), each w:tc contains w:p (paragraphs), each w:p contains w:r (runs), and each w:r contains w:t (text). Properties elements include w:tblPr, w:tblGrid, w:tcPr, w:pPr, and w:rPr at their respective levels.

- id: docx-model-02
  answer: |
    Word splits text across multiple runs arbitrarily (e.g., "Widget C" might be "Wid" + "get C" or "Widget" + " C"), so no single run's text equals the full string. To find text reliably, concatenate all w:t elements within the cell (or use cell.text which does this). To replace, set the first run's text to the new value and clear the remaining runs' text, preserving the first run's formatting (rPr).

- id: docx-model-03
  answer: |
    Table and cell widths use twips (dxa): 1440 twips = 1 inch = 72 points. Image sizes use EMUs (English Metric Units): 914400 EMUs = 1 inch = 72 points. To convert: points = twips / 20; inches = twips / 1440; inches = EMUs / 914400; points = EMUs / 12700.

- id: docx-locate-01
  answer: |
    Iterate all tables in the document (doc.tables) and identify the invoice table by checking header row cell text (e.g., a cell containing "Item" or "Description"). Then iterate rows and match by stripped cell text (e.g., row.cells[0].text.strip() == "Gadget D"). Before editing, assert exactly one matching row was found (zero or several means stop and investigate). Also verify the table's column count and structure match expectations.

- id: docx-locate-02
  answer: |
    row.cells returns one entry per grid column, not per w:tc element. A cell with w:gridSpan="2" appears twice as the same _Cell/_tc object. If you delete "the second cell" by index from row.cells, you may delete the wrong element or the same _tc twice. If you count columns from row.cells, you get the grid width, not the actual number of w:tc elements. To work correctly, deduplicate by _tc identity, or iterate row._tr.tc_lst and accumulate gridSpan values to map grid positions to actual tc elements.

- id: docx-rowdel-01
  answer: |
    There is no table.delete_row() or row.delete() method in python-docx. To delete a row, get the underlying w:tr element and remove it from its parent: tr = row._tr; tr.getparent().remove(tr). This removes the row element from the XML tree entirely.

- id: docx-rowdel-02
  answer: |
    If you just remove the restart row, the continuation cells below lose their merge start point—they become independent cells with no vMerge restart, breaking the vertical merge. To do it correctly: first move the content from the restart row's cell into the next row's cell in that column, set w:vMerge w:val="restart" on that next cell (or drop vMerge entirely if only one row remains in the merge), then remove the w:tr element.

- id: docx-rowdel-03
  answer: |
    The Total field keeps its cached result (the text between the separate and end fldChar runs), so it will show the old sum. To handle it: compute the new total from the remaining line items, find the field's result run (the w:r between w:fldChar w:fldCharType="separate" and w:fldChar w:fldCharType="end"), and write the new value into that run's w:t text, preserving the field code itself. Optionally set w:updateFields w:val="true" in settings.xml so Word recalculates on open.

- id: docx-rowdel-04
  answer: |
    Setting height to 0, making text white, or marking runs hidden does not remove the row from the XML—the w:tr element and all its content remain in document.xml. The row still exists structurally and can be unhidden or recovered. To prove the row is really gone, unzip the .docx and verify the w:tr element is absent from word/document.xml, or use python-docx to confirm the row count decreased and the specific row object is no longer in table.rows.

- id: docx-cell-01
  answer: |
    cell.text = "SKU" replaces all content in the cell with a single plain run. What survives: tcPr-level formatting (cell shading, width, borders, vertical alignment). What is lost: run-level formatting (bold, font size, font color, font family, italics) and paragraph-level formatting (alignment, spacing). The new run has no rPr, so it inherits the document's default style.

- id: docx-cell-02
  answer: |
    Changing Qty from 5 to 12 requires recomputing: the line item's Amount (Qty × Unit Price), the Subtotal (sum of all line amounts), the Tax (Subtotal × tax rate), and the Grand Total (Subtotal + Tax). Any other dependent figures (e.g., a deposit balance, a repeated total in a summary section) must also be updated. Write each new value into the existing cell's run(s) preserving the current number format and alignment.

- id: docx-cell-03
  answer: |
    With w:trackRevisions enabled, python-docx writes no w:ins or w:del elements—your edit silently replaces text with no revision markup, so the change is invisible to reviewers and cannot be rejected. You should decide whether the edit must be tracked; if a clean edit is required, first resolve existing revisions in the region (accept or reject them), check that w:delText no longer holds the old value, and then make the edit. If tracking is desired, you must manually insert w:ins elements around the new text.

- id: docx-relayout-01
  answer: |
    table.add_row() appends a new row at the end of the table (below any Total row) and copies only column widths—no row properties (trPr), cell shading, or run formatting. To insert a correctly formatted row in the middle: deepcopy an existing styled body row's w:tr element, insert it using target_row._tr.addprevious(new_tr) or reference_row._tr.addnext(new_tr), then set each cell's text through its existing runs. Choose the source row so that alternating row shading (banding) remains correct.

- id: docx-relayout-02
  answer: |
    python-docx has no table.add_column() method. To add a column you must: (1) add a new w:gridCol to w:tblGrid, (2) add a new w:tc to every existing w:tr (deepcopy a cell from the same column position in that row to carry formatting), (3) set w:tcW on the new cell, (4) update w:tblW if needed. To keep the table inside page margins, reduce existing column widths to fund the new column's width, ensure the sum of gridCol widths equals the section text width (page width minus left and right margins), and set w:tblLayout w:type="fixed" so widths hold exactly.

- id: docx-relayout-03
  answer: |
    Word uses w:tcW (cell width) on each w:tc and w:tblW (table width) on w:tblPr for actual rendering, not just w:tblGrid/gridCol. If you change only gridCol widths, the per-cell tcW values still hold the old widths. You must update every cell's w:tcW to match the new gridCol widths, update w:tblW, and set w:tblLayout w:type="fixed" in w:tblPr so Word uses the fixed widths rather than auto-fitting.

- id: docx-tblins-01
  answer: |
    doc.add_table(rows, cols) appends the table at the end of the document body. To place it after a specific paragraph: create the table (it will be at the end), then move its XML element using paragraph._p.addnext(table._tbl). This inserts the table immediately after the target paragraph in the document flow.

- id: docx-tblins-02
  answer: |
    The KeyError occurs because the style name "Grid Table 4 Accent 1" is not defined in the template's styles.xml—python-docx looks up styles by name in the document's style definitions and raises KeyError if not found. To get a ruled table: use a style that exists in the template (check available styles via doc.styles), copy the style/formatting from an existing table in the document, or add the style definition to styles.xml first.

- id: docx-tblins-03
  answer: |
    A table from doc.add_table(rows, cols) defaults to the full section text width (page width minus left and right margins) split evenly across all columns. To match an existing invoice table: set the table's style to match, set w:tblW to the existing table's width, copy the w:tblGrid gridCol widths, set each cell's w:tcW to the corresponding existing cell width, and copy header formatting (shading, bold/white runs, alignment).

- id: docx-imgrep-01
  answer: |
    A picture in document.xml references its image bytes via an a:blip element with r:embed="rIdX", where rIdX is a relationship ID in word/_rels/document.xml.rels pointing to word/media/imageN.ext. The image size comes from wp:extent (cx, cy in EMUs). To replace one picture: add a new image part via doc.part.get_or_add_image(new_path), repoint the specific a:blip's r:embed to the new relationship ID, and keep the existing wp:extent (or update it if the aspect ratio differs). Never overwrite image_part._blob directly.

- id: docx-imgrep-02
  answer: |
    Image parts in a .docx are shared and deduplicated—multiple a:blip elements can reference the same image part via the same relationship. Overwriting image_part._blob changes the bytes for every blip that references that part, so all pictures using that image part (including the different picture on page 3) are affected. The correct approach is to add a new image part and repoint only the specific blip you want to change.

- id: docx-imgrep-03
  answer: |
    A logo shown at the top of every page is in the section header (header1.xml or similar), not in document.xml. doc.inline_shapes only lists images in the main document body. To find it: access doc.sections[0].header (or .first_page_header / .even_page_header), then look for inline shapes or w:drawing elements in the header's paragraphs. Replace it using the header part's get_or_add_image and repoint the blip's r:embed in the header's relationships.

- id: docx-imgins-01
  answer: |
    doc.add_picture() appends the image at the end of the document as a new inline shape in a new paragraph. To place it after a specific paragraph: either add a new paragraph after the target paragraph and insert the picture there, or add a run to an existing paragraph and add the picture to that run. To keep aspect ratio at 1.5 inches wide: set width to Inches(1.5) and compute height as width * (original_height_px / original_width_px), or set only the width and let python-docx preserve aspect ratio automatically.

- id: docx-imgins-02
  answer: |
    A 3-inch-wide image in a 1.2-inch-wide cell will overflow the cell boundaries, potentially breaking the table layout or extending into margins. To size it correctly: resize the image to fit within the cell width (1.2 inches), maintaining aspect ratio by computing the proportional height. Set the image width to Inches(1.2) (or slightly less to account for cell padding) and calculate height = width * (img_height_px / img_width_px).

- id: docx-legacy-01
  answer: |
    python-docx cannot open .doc (Word 97–2003 binary format)—it only works with .docx (OOXML). The workflow is: (1) convert the .doc to .docx using LibreOffice (soffice --headless --convert-to docx file.doc) or on macOS using textutil -convert docx file.doc, (2) open the converted .docx with python-docx, (3) perform the table row deletion, (4) save. Note that conversion may lose some formatting or features.

- id: docx-legacy-02
  answer: |
    textutil -convert docx on macOS carries the risk of losing or altering formatting, fields, images, styles, and table structure during conversion. To detect issues: (1) compare the converted file's content with the original by extracting text from both (e.g., using textutil -convert txt on the original and pandoc or python-docx on the converted), (2) check that all tables, rows, and key formatting survived, (3) verify that fields (like SUM formulas) are still present and not flattened to static values, (4) confirm images are still embedded.

- id: docx-legacy-03
  answer: |
    soffice --headless exits immediately when a LibreOffice GUI window is already open because it attempts to connect to the existing instance rather than starting a new headless process. To run it reliably from a script: use a separate user installation profile with -env:UserInstallation=file:///tmp/lo_profile (or a unique path), which forces soffice to start an independent instance. Alternatively, close the GUI LibreOffice window before running the command.

- id: docx-verify-01
  answer: |
    Safe save-and-verify procedure: (1) after saving, run unzip -t file.docx to verify ZIP integrity, (2) reopen with python-docx to confirm it parses without errors, (3) verify the XML is well-formed (e.g., using lxml.etree.parse on document.xml), (4) check that all expected parts are present in the zip, (5) extract and inspect document.xml for the intended changes, (6) optionally validate against the OOXML schema, (7) cross-check by converting to text with pandoc or textutil and diffing against expected content.

- id: docx-verify-02
  answer: |
    Common structural mistakes in table edits that cause "unreadable content": (1) incorrect element order within w:tcPr (e.g., w:shd must come after w:tcW), (2) missing w:tblGrid or gridCol count mismatching actual cells, (3) omitting required child elements like w:tcPr in a cell, (4) invalid attribute values (e.g., float instead of integer for EMU coordinates in wp:extent), (5) orphaned or mismatched namespace declarations, (6) w:tblPr element order violations (tblStyle before tblW before jc before tblBorders before tblLayout before tblLook), (7) missing w:p or w:rPr where required by the content model.

- id: docx-verify-03
  answer: |
    To ensure the old image is not still inside the .docx: (1) unzip the .docx and list all files in word/media/—verify the old image file is absent, (2) check word/_rels/document.xml.rels (and header rels if applicable) to confirm the old relationship ID is gone, (3) search document.xml (and header XML) for any remaining a:blip with r:embed pointing to the old relationship ID, (4) if the old image part is still referenced by any blip, do not remove it—only remove the relationship and media file when no blip still uses it.
