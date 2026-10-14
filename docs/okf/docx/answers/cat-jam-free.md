# cat-jam-free — closed-book answers for docx (opencode-go, 2026-10-14); answerer saw only _prompts/docx.md
# Delivery: one `ocode run -m opencode-go/cat-jam-free -effort med` session, sheet attached via -f, empty scratch cwd;
# session audited in sqlite: 1 user + 1 assistant message, 0 tool calls.

- id: docx-model-01
  answer: |
    A .docx is a ZIP archive of OOXML parts; the main document is word/document.xml.
    A table's data lives in word/document.xml as: w:tbl (table) -> w:tr (row) -> w:tc (cell)
    -> w:p (paragraph) -> w:r (run) -> w:t (text). w:tblGrid/w:gridCol describe columns; w:tblPr
    and w:tcPr carry table/cell properties.

- id: docx-model-02
  answer: |
    Word splits visible text arbitrarily across runs (also paragraphs), so run.text rarely equals a
    whole cell string. Reliable search should compare against the whole paragraph/cell text
    (paragraph.text or cell.text) and, for replacement, either collapse/rebuild runs or edit runs
    by walking the full text. Better helpers replace text across runs while preserving formatting.

- id: docx-model-03
  answer: |
    Table/cell widths are commonly in twentieths of a point (dxa): 1 pt = 20 dxa, 1 in = 1440 dxa.
    Percentage units (pct) also exist. Image sizes are in EMU: 1 in = 914400 EMU, 1 pt = 12700 EMU.

- id: docx-locate-01
  answer: |
    Iterate doc.tables, identify candidate by header row text (e.g. first row cells contain
    "Item"/"Qty"/"Amount" or "Invoice"), then find the row where the item column text == "Gadget D".
    Before editing, verify the table/row was found uniquely, re-read the cell text, check merges,
    and confirm totals/formulas so you do not edit the wrong table or row.

- id: docx-locate-02
  answer: |
    row.cells has one entry per grid column; a horizontally merged cell is repeated for each spanned
    column. Deleting "the second cell" by index can target the same merged _Cell or remove the wrong
    XML; len(row.cells) overstates logical cells. Handle merged ranges explicitly via gridSpan.

- id: docx-rowdel-01
  answer: |
    python-docx has no table.delete_row() or row.delete() method. Remove the underlying XML row:
    row._tr.getparent().remove(row._tr) (or table._tbl.remove(row._tr)).

- id: docx-rowdel-02
  answer: |
    Simply removing the restart row leaves later w:vMerge continuation cells without a restart,
    breaking the merge. Correctly either remove the whole merged group or change the next row's first
    cell from w:vMerge to w:vMerge w:val="restart" before removing the old restart row.

- id: docx-rowdel-03
  answer: |
    Word shows the stale cached total until the field recalculates. Set Word to recalc on load
    (w:calcPr fullCalcOnLoad in settings.xml) and/or update the cached field result; ideally recalc
    and replace the result text as well.

- id: docx-rowdel-04
  answer: |
    Hiding/whitening/zero-height text leaves the tr in word/document.xml, so the row still exists,
    counts in tables, exports, and may show in other viewers. Proof it is gone: reopen and check row
    count/indexes, inspect document.xml for the row, or compare text/ZIP output.

- id: docx-cell-01
  answer: |
    cell.text = "SKU" clears paragraphs and rebuilds one run, so cell-level formatting (shading,
    borders, width, margins) survives, but the original text run formatting (bold, size, color) is lost.

- id: docx-cell-02
  answer: |
    Qty*Unit Price = Amount must be updated, then Total and any tax/discount/summary totals must be
    recomputed and written. Either calculate in the script and overwrite the cells, or set fields to
    recalc on load.

- id: docx-cell-03
  answer: |
    python-docx overwrites w:t without adding revision markup, so the edit is not tracked and can
    corrupt existing revision nodes. Consider turning off trackRevisions/downloading a clean copy,
    or editing with proper w:ins/w:del markup; test the result.

- id: docx-relayout-01
  answer: |
    table.add_row() appends a row at the end. To insert in the middle, deepcopy an existing w:tr,
    optionally blank its cell text, and insert it at the desired index with element.insert(index,
    new_tr), then modify the cells.

- id: docx-relayout-02
  answer: |
    table.add_column(width) appends a column on the right (adds cells to each row and a gridCol).
    To stay within margins and match other columns, reduce another column or table width, set the
    new cells' widths, update w:tblGrid, and use fixed table layout rather than autofit.

- id: docx-relayout-03
  answer: |
    Word also caches preferred widths in w:tcW and may use autofit/content rules. Update w:tcW (and
    tblW/tblLayout fixed) for every cell and disable autofit; ensure gridCol widths total to the
    intended table width.

- id: docx-tblins-01
  answer: |
    doc.add_table() appends the table at the end of the body. Place it after a paragraph by inserting
    the table element into the XML: paragraph._p.addnext(table._tbl).

- id: docx-tblins-02
  answer: |
    The named style does not exist in that document's styles part. Use an existing built-in style
    such as "Table Grid", or define borders/CSS-like formatting manually.

- id: docx-tblins-03
  answer: |
    By default widths are unset/auto (fills available width approximately). To match an existing
    table, copy its column widths (gridCol/tcW), table width, and style/layout settings.

- id: docx-imgrep-01
  answer: |
    document.xml references the image via w:drawing -> a:blip with r:embed="rIdX"; document.xml.rels
    maps rIdX to media/imageN. To keep position/size, either overwrite that specific image part's
    bytes (if not shared) or create a new part and point a new blip at it while preserving
    wp:inline/extent.

- id: docx-imgrep-02
  answer: |
    Multiple drawings can share the same image part/relationship, so overwriting its bytes updates
    every reference. Use a unique part/relationship for the replacement or check sharing first.

- id: docx-imgrep-03
  answer: |
    The logo is likely in a header/footer part (word/header*.xml or footer*.xml). Search
    section.header/footer XML for w:drawing/a:blip and replace that image part/reference.

- id: docx-imgins-01
  answer: |
    doc.add_picture() creates a new paragraph at end of document. To place after "Approved by:",
    use paragraph.add_run().add_picture(path, width=Inches(1.5)); python-docx computes height from
    aspect ratio.

- id: docx-imgins-02
  answer: |
    The image overflows/will not fit the cell; resize it to the cell width (or less), keeping
    aspect ratio: set width=Inches(1.2) or fit to the cell's content width.

- id: docx-legacy-01
  answer: |
    python-docx cannot open .doc (binary OLE2; it only handles OOXML .docx). Convert with
    LibreOffice/Pandoc/textutil, edit the .docx, then convert back if a .doc is required.

- id: docx-legacy-02
  answer: |
    textutil conversion can alter formatting, tables, styles, or fields. Detect by comparing parsed
    content, table counts/widths, and renders; keep the original and diff outputs.

- id: docx-legacy-03
  answer: |
    A running LibreOffice instance/profile lock prevents headless conversion. Use a separate
    user installation profile (-env:UserInstallation=file:///tmp/...) and/or close other instances.

- id: docx-verify-01
  answer: |
    Save to a new file, run unzip -t, reopen with python-docx and assert structure/tables/totals,
    validate XML/relations if tools exist, render with LibreOffice/Pandoc, and keep the original for
    rollback.

- id: docx-verify-02
  answer: |
    Common causes: wrong element order in tblPr/tcPr, missing tblGrid, unbalanced w:tc (no w:p),
    mismatched gridSpan/tcW, invalid attributes, broken vMerge/restart sequences, orphaned runs,
    or malformed namespaces/relationships.

- id: docx-verify-03
  answer: |
    List ZIP parts and hashes; ensure the old media part is unreferenced and remove orphan image
    files/relationships; also check headers, footers, and docProps thumbnails.
