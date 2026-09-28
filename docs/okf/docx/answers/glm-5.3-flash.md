- id: docx-model-01
  answer: 'A .docx file is physically a ZIP archive (an OPC/Office Open XML package), not a single

    document. It contains [Content_Types].xml, a _rels folder of relationship files, the main

    body part word/document.xml (this is where table data lives), plus supporting parts such as

    word/styles.xml, word/numbering.xml, word/settings.xml, header/footer parts, and a word/media

    folder for embedded images. All visible table content is in word/document.xml.

    Word table XML hierarchy:

    <w:tbl> (table) → <w:tblPr> (table properties: width, borders, layout) and <w:tblGrid>

    containing one <w:gridCol> per column → <w:tr> (table row, optionally with <w:trPr>) →

    <w:tc> (table cell, optionally with <w:tcPr> holding width, shading, merges) → <w:p>

    (paragraph, with <w:pPr>) → <w:r> (run, with <w:rPr> character formatting) → <w:t> (the

    actual text). Cells can hold multiple paragraphs and even nested tables.

    '
- id: docx-model-02
  answer: 'Because Word frequently splits visible text across multiple runs (<w:r> elements): each run

    can have different rsid, spell/grammar-check boundaries (proofErr), formatting changes, or

    editing history, so "Widget C" may be stored as runs "Wid", "get C" (or similar). A single

    run''s .text never equals the full string, so the equality check fails.

    Reliable find/replace: per paragraph, concatenate all runs'' text to get the paragraph''s full

    text (equivalent to paragraph.text, which python-docx builds by joining run texts). Search

    that. To replace while preserving formatting: locate the w:t nodes that overlap the match,

    put the (replaced) text into the first overlapping w:t, and blank out the matched portion of

    the remaining w:t nodes (set their text to ""), keeping each run''s rPr intact. Do this at

    the paragraph or cell level, not by comparing individual run.text strings.

    '
- id: docx-model-03
  answer: 'Widths: WordprocessingML measures table, column, and cell widths in twips, also called dxa

    (twentieths of a point): 20 twips = 1 pt, so 1 inch = 1440 twips. Example: a 3-inch column

    is w:w="4320" w:type="dxa".

    Images: sizes in the DrawingML are stored in EMU (English Metric Units): 914400 EMU per

    inch, 12700 EMU per point (1 inch = 72 pt). The wp:extent cx/cy attributes of an inline or

    anchored image are EMU values.

    In python-docx all lengths are EMU-based Length objects; helper constructors are

    Inches(), Pt(), Emu(), Twips() (1 twip = 635 EMU). Conversions: inches = twips/1440 =

    EMU/914400; points = twips/20 = EMU/12700.

    '
- id: docx-locate-01
  answer: 'Finding the table: iterate document.tables and fingerprint each table — read its first row

    (header) cell texts, or look for a caption/label paragraph immediately preceding it, or a

    distinctive known value inside it. Do not rely on document.tables[0] being "the invoice

    table"; header text like ["Item","Qty","Rate","Amount"] or a caption "Invoice" is a much

    stronger anchor.

    Finding the row: for each row in the candidate table, build a list of cell texts

    ([c.text for c in row.cells]) and match the row where the Item column equals "Gadget D".

    Match on normalized text (strip whitespace), and pick the column by header position rather

    than a hardcoded index.

    Before editing, check: the table is the one you think it is (headers + row count make

    sense); the target row is not part of a merged region (w:gridSpan / w:vMerge, which make

    cell indices misleading); the numbers you will touch are literal text vs Word fields

    (w:fldSimple / fldChar with cached results, e.g. SUM formulas); there is no Track Changes /

    revision markup already pending in the document; and keep a backup of the original file.

    Also confirm column order via the header row, not hardcoded indices.

    '
- id: docx-locate-02
  answer: 'In python-docx, row.cells returns one cell per grid column, and a cell that spans multiple

    columns (w:gridSpan) appears once for each grid column it covers — i.e. the same underlying

    w:tc (the same _Cell object) is repeated. So a first row with a 2-column-spanning cell in a

    4-column table yields 4 cell references but only 3 distinct w:tc elements.

    What goes wrong: (1) If you "delete the second cell by index" you may actually be deleting

    the merged cell itself (index 1 is the same tc as index 0), removing real content and

    leaving the row with fewer w:tc elements than grid columns — a broken table that Word may

    repair or refuse to open. (2) len(row.cells) counts grid positions, not physical cells, so

    using it as "the number of columns/cells in this row" is misleading: it equals the grid

    count, not the count of actual w:tc elements. If rows have different merge structures,

    different rows can report different cell counts. To inspect real cells, count/read the

    tr''s w:tc children directly in the XML.

    '
- id: docx-rowdel-01
  answer: "python-docx (through its released API) has no table.delete_row() and no row.delete()\nmethod\
    \ — row deletion was deliberately not exposed. You delete a row via the underlying\nlxml element:\n\
    \    row._element.getparent().remove(row._element)\nor equivalently\n    tbl._tbl.remove(row._tr)\n\
    (the getparent().remove() form is safer if the row could be nested inside another table).\nAfter removal,\
    \ verify len(table.rows) dropped by one and the target text no longer appears\nin any cell.\n"
- id: docx-rowdel-02
  answer: 'The w:vMerge mechanism makes the first row''s cell the merge "restart" and the following

    rows'' cells continuation cells of that same merged region. If you simply delete the restart

    row, the continuation rows are orphaned: their cells carry <w:vMerge/> (continue) with no

    restart above them, which is inconsistent merge state. Depending on the file, Word may

    repair the document, show a blank/incorrectly merged cell, or render the continuation

    content as a strange leftover — and any content that was only in the deleted restart row is

    gone from the merge.

    Correct approaches: (a) promote the next remaining row''s first cell to be the new restart —

    change its <w:vMerge/> to <w:vMerge w:val="restart"/>, or remove the vMerge continuation

    from it if the merge should now end there; (b) if the whole merged block should go, delete

    the restart row and all continuation rows; (c) or cancel the merge on the remaining rows by

    stripping their vMerge elements so each becomes an independent cell. Afterward, re-validate

    the merge structure (each continuation has a restart above it within the same column) and

    open the file to confirm Word doesn''t flag it for repair.

    '
- id: docx-rowdel-03
  answer: 'The { =SUM(ABOVE) } is a Word field. The file stores the field instruction (w:fldSimple or

    w:fldChar/w:instrText) plus a cached field result — the last value Word calculated. Deleting

    a row with python-docx does not recalculate anything: on open, Word displays the stale

    cached total, which no longer matches the remaining line items (until the field is

    refreshed, e.g. with F9, on print if "update fields before printing" is on, or at open if

    settings.xml has w:updateFields).

    Handle it by one of: (a) update the cached result run inside the field to the newly

    computed sum so the saved file is already consistent; (b) add/ensure <w:updateFields

    w:val="true"/> in word/settings.xml so Word recalculates when the document is opened; or

    (c) replace the field with a literal computed value if the document no longer needs a live

    formula. Either way, recompute the sum from the surviving rows and make the displayed value

    match.

    '
- id: docx-rowdel-04
  answer: 'It is not a deletion — the row still exists in the table''s XML. Zero height (trHeight

    exact 0), white text, or hidden runs (w:rPr with w:vanish) merely hide it visually. The

    table still contains the row: it still occupies a grid row, still counts in

    len(table.rows), the text is still in the file and searchable/extractable by any tool

    (grep on document.xml, python-docx cell.text, text extractors), it still affects table

    structure, cell references, and fields, it prints in some export paths, and screen readers

    or copy/paste can expose it. It is cosmetic concealment, and it makes the document

    misleading.

    Prove a real deletion: (1) unzip the .docx and confirm the row''s text no longer appears in

    word/document.xml (the strongest proof); (2) count the table''s w:tr elements before/after,

    or len(table.rows) in python-docx; (3) reopen in Word and confirm the row is absent from

    Table Properties / row selection; (4) re-extract all cell text and confirm no trace.

    '
- id: docx-cell-01
  answer: 'cell.text = "SKU" rewrites the cell''s paragraph content: it effectively replaces the runs

    with a single new run containing the plain string (and collapses multiple paragraphs).

    Lost: all run-level character formatting — the bold flag, the 9 pt font size, and the white

    font color — because the new run has no rPr. Also lost: any run-level special formatting

    and additional paragraphs'' distinct content/structure within the cell.

    Survives: cell-level properties in w:tcPr (the dark shading, cell width, borders, vertical

    alignment, merge flags) and paragraph-level properties in w:pPr (alignment, spacing, and

    the paragraph''s style reference, if any).

    Better: edit the existing first run''s w:t text (run.text = "SKU") and blank the other

    runs'' text — this keeps the run''s rPr, so bold/9pt/white survive.

    '
- id: docx-cell-02
  answer: 'Changing a quantity on one line item has arithmetic consequences: the line''s Amount (Qty ×

    Rate) must be recomputed, and everything downstream — subtotal, tax/VAT (if percentage-

    based), shipping, and the grand Total — changes. In Word terms: if those cells are Word

    fields (SUM, PRODUCT), their cached results must be updated (or w:updateFields set so Word

    refreshes on open); if they are plain text, you must recompute and rewrite them.

    Steps: recompute line amount from the new Qty and the stored Rate; recompute all totals

    from the surviving line items; keep number formatting identical to the rest of the invoice

    (same decimal places, thousands separator, currency symbol); check for other references to

    the quantities anywhere in the document (summary tables, narrative text, footnotes) and

    update those too. Finally, verify internal consistency (each line amount = qty × rate;

    totals = sum of lines; tax = rate × subtotal) before saving.

    '
- id: docx-cell-03
  answer: 'w:trackRevisions only governs edits made inside Word; python-docx ignores it. If you

    rewrite the w:t text programmatically, the change is an untracked, silent edit: no

    w:ins/w:del revision markup is generated, no author/date is recorded, and the text simply

    appears as if it had always been there. Meanwhile the document may still carry pre-existing

    pending revisions from Word, so your clean edit sits mixed inside a document whose other

    content is under review — reviewers may never see that the cell changed, and "accept all"

    won''t flag it either.

    Considerations: if a clean final edit is desired, it''s usually fine to just do it (and

    optionally remove the trackRevisions flag or accept all existing revisions first, so the

    delivered file is consistent); if the workflow requires visible review, real tracked

    changes would have to be authored manually as w:ins/w:del elements with author and

    timestamp, which python-docx does not support out of the box. At minimum, tell the user the

    edit bypassed Track Changes.

    '
- id: docx-relayout-01
  answer: 'table.add_row() appends a brand-new, empty row at the END of the table (cells sized to the

    tblGrid), with plain default formatting — it does not copy the formatting of neighboring

    rows and it cannot insert in the middle.

    To insert after "Service G" and before Total: work at the XML level. Deep-copy an existing

    template row (e.g. the "Service G" row itself, so trPr, cell widths tcW, shading, borders,

    fonts and paragraph styles come along), clear the copied text, then position it with

    lxml: template_tr.addnext(new_tr) to insert after it, or total_row._tr.addprevious(new_tr)

    to insert before the Total row. Then fill the new cells by editing existing runs (or

    copying a run and changing its w:t) rather than assigning cell.text, so character

    formatting is preserved. Finally re-check anything structural: the SUM(ABOVE) field''s cached

    total, grid alignment, and that the new row''s cell count matches the table''s grid.

    '
- id: docx-relayout-02
  answer: "table.add_column(width) only appends a new column at the right edge of the table: it adds a\n\
    w:gridCol to the tblGrid and an empty cell to every row. There is no python-docx API for\ninserting\
    \ a column in the middle.\nTo put \"SKU\" after Item: manipulate the XML — insert a new w:gridCol\
    \ at the correct\nposition in w:tblGrid, and for every row insert a new w:tc at the matching position\n\
    (deep-copying an existing Item-column cell is a good way to inherit borders, shading and\nfonts, then\
    \ clear its text). Then make it fit and look right:\n- Widths: usable page width = page width minus\
    \ left/right margins (from sectPr). Keep\n  total column width within that; typically shrink the other\
    \ columns proportionally.\n- Set the new gridCol width AND each new cell's w:tcW consistently (Word\
    \ lays out from cell\n  widths/tblW, not gridCol alone).\n- Match visual style of the other columns:\
    \ copy tcPr properties, alignment, header shading\n  for the header cell, and apply the same run formatting\
    \ to the new text.\n- If the table uses fixed layout, keep grid and cell widths consistent; if autofit,\
    \ verify\n  Word doesn't rebalance widths oddly after the edit.\n"
- id: docx-relayout-03
  answer: 'Because Word does not lay out columns from w:tblGrid alone. Each cell carries its own

    preferred width (w:tcW, in dxa), and the table has its own width (w:tblW) and layout mode

    (w:tblLayout). When w:tblLayout is "fixed", Word uses the cells'' tcW values (especially

    the first row''s) and effectively ignores or overrides the gridCol widths you changed; with

    autofit ("autofit to contents/window"), Word recalculates widths from content and page

    width regardless of gridCol. So a tblGrid-only edit is cosmetic metadata that Word

    overrides.

    What must be updated: the w:tcW width of every cell in every row (or at minimum the first

    row, which Word tends to honor), the w:tblW table width, and w:gridCol entries — all kept

    mutually consistent with each other and with the usable page width. Also check w:tblLayout:

    if fixed, ensure grid and cell widths agree; if you want Word to recompute, set autofit and

    accept that content drives the final widths.

    '
- id: docx-tblins-01
  answer: 'doc.add_table() always appends the table at the very end of the document body

    (after the last paragraph). It has no position parameter. To place it

    mid-document you must move the table''s XML element afterwards: locate the

    anchor paragraph ("Notes:"), then reorder at the lxml level, e.g.

    anchor._p.addnext(table._tbl) to put the table immediately after that

    paragraph (addprevious() puts it before). Gotchas: Word merges two adjacent

    w:tbl elements, so if a table already follows, keep a w:p between them; a

    table at the very end should be followed by an empty paragraph; and verify by

    reopening the file and checking doc.tables order / surrounding text.

    '
- id: docx-tblins-02
  answer: 'KeyError means the style name is not defined in the document''s styles part

    (word/styles.xml). python-docx can only apply styles that physically exist in

    the file; it will not synthesize Word''s built-in table styles, and a company

    template often only embeds a built-in table style if it was actually used and

    saved. Fixes: (a) open the template in Word, apply "Grid Table 4 Accent 1" to

    a table once and save, embedding the style definition, then python-docx can

    reference it; (b) inject the w:style definition (w:type="table") into

    doc.styles.element from another document''s styles.xml; (c) use a style the

    template does define (often "Table Grid") and get ruled borders from it; or

    (d) set borders manually via tblBorders XML. Note doc.styles.add_style()

    creates a new empty style — it will not reproduce the built-in look.

    '
- id: docx-tblins-03
  answer: 'add_table() sets no explicit width: there is no meaningful tblW, so Word

    auto-fits the table based on content, and the result will generally NOT match

    the existing invoice table (it can end up narrow, or inconsistent across

    viewers). To match: measure the invoice table''s w:tblGrid / tcW values and

    copy them; set table.autofit = False, set explicit column/cell widths

    (table.columns[i].width and/or cell.width, e.g. Inches(...)), and optionally

    set a table-level w:tblW in twips plus w:tblLayout type="fixed" via XML. With

    fixed layout Word honors tcW per cell, so keep cell widths consistent with

    the grid.

    '
- id: docx-imgrep-01
  answer: 'The picture is a w:drawing (wp:inline or wp:anchor) in document.xml containing

    pic:pic/pic:blipFill/a:blip with an r:embed relationship id; that id maps in

    word/_rels/document.xml.rels to a media part like word/media/image1.png. The

    displayed position and size live in the drawing XML itself (wp:extent,

    positioning), not in the image part. To replace one logo: find the specific

    drawing (iterate doc.inline_shapes and match on docPr name or dimensions via

    shape._inline.docPr), resolve its blip r:embed through the rels to the image

    part, confirm that part is referenced by only this drawing (scan all a:blip

    elements in body and headers), then overwrite the part''s bytes

    (image_part._blob = new_data) keeping the same image format, or add the new

    image as a fresh part and repoint only this blip''s r:embed. Because the

    drawing XML is untouched, position and size are preserved; if the new image''s

    aspect ratio differs it will be stretched into the old extent, so regenerate

    the image at a matching aspect or update wp:extent too.

    '
- id: docx-imgrep-02
  answer: 'Image parts can be shared. Word dedupes identical images, so the logo

    drawing and the page-3 picture can both have blips whose relationships

    resolve to the same media part (possibly via different rIds pointing at the

    same target). image_part._blob is the stored bytes of that shared part, so

    overwriting it changes every rendering that resolves to it. Diagnose by

    enumerating all a:blip r:embed references across the document (and header)

    parts and mapping each to its rel target/part. Fix: add the new logo as a new

    image part (e.g. via run.add_picture or part.get_or_add_image) and point only

    the logo''s r:embed at it, leaving the old shared part intact for the other

    picture.

    '
- id: docx-imgrep-03
  answer: 'A logo repeated at the top of every page lives in a header/footer part

    (word/header1.xml, possibly first-page/even headers), not the document body,

    so doc.inline_shapes (body-only) never lists it. Find it per section:

    for section in doc.sections, inspect section.header,

    section.first_page_header, section.even_page_header (and footers); watch

    is_linked_to_previous — an inherited header''s XML physically lives in an

    earlier section''s part. python-docx exposes no header.inline_shapes, so

    search the header part''s XML with xpath (//a:blip), read its r:embed, and

    resolve it through the header part''s own relationships

    (word/_rels/headerN.xml.rels) to the media part. Then replace the referenced

    part''s bytes if that part is unique to the logo, or repoint the blip''s

    r:embed to a new image added to the header part.

    '
- id: docx-imgins-01
  answer: 'doc.add_picture() appends a new paragraph containing the image at the very end

    of the document body. To place it after "Approved by:", get that paragraph and

    use the run-level API: run = para.add_run(); run.add_picture(''signature.png'',

    width=Inches(1.5)). Run.add_picture inserts the image inline exactly at that

    run''s position, and supplying only the width lets python-docx compute the

    height from the image''s native pixel dimensions, preserving the aspect ratio.

    If the signature must be on its own line below the paragraph, insert a new

    empty paragraph immediately after it (para._p.addnext(new_p) or

    insert_paragraph_before on the following paragraph) and add the run/picture

    there.

    '
- id: docx-imgins-02
  answer: 'Word does not clip or auto-shrink pictures to their cell: the image renders at

    its declared 3-inch size and overflows the 1.2-inch cell, overlapping adjacent

    cells and breaking the table layout. Size it to the usable cell width: compute

    target = cell.width minus the cell''s left/right margins (default about 0.08"

    each side), then insert with width=Inches(target) (height auto-derived, aspect

    preserved), or after insertion set shape.width and scale shape.height by the

    same factor. Alternatively widen the column/cell (cell.width) to fit the

    intended image size. Verify by reopening and checking shape width vs cell

    width.

    '
- id: docx-legacy-01
  answer: 'No. python-docx reads and writes only .docx (OOXML: a zip of XML parts); the

    binary Word 97-2003 .doc (OLE compound file) cannot even be opened as a zip.

    Workflow: keep the original .doc untouched; convert a copy to .docx with a

    real converter (LibreOffice headless: soffice --headless --convert-to docx

    --outdir out report.doc; or Word/textutil as fallback), edit the .docx with

    python-docx (to delete a row: table.rows[i]._tr.getparent().remove(row._tr),

    minding merged cells), reopen the saved file to verify the table, then, if the

    user needs .doc, convert back with soffice --convert-to doc and check the

    round-trip preserved the table content.

    '
- id: docx-legacy-02
  answer: 'textutil is a lightweight, text-oriented converter, not a Word-fidelity

    engine. Tables can lose merged cells, explicit widths, borders, nested tables,

    images or shading, or be flattened — so the .docx you then edit may not

    structurally match the real document, and your "row deletion" can hit the

    wrong table, drop content, or produce broken output. Detect it: convert the

    original .doc to txt as a content baseline (textutil -convert txt) and compare

    against what python-docx sees in the converted .docx — number of tables

    (len(doc.tables)), row/column counts, and cell texts. A mismatch (missing or

    oddly empty/merged tables) means the conversion was lossy: redo it with a

    full-fidelity converter such as LibreOffice or Word.

    '
- id: docx-legacy-03
  answer: 'soffice is effectively single-instance per user profile. With a LibreOffice

    window already open using the default profile, a new soffice invocation

    forwards to/defers to the running instance and exits immediately without

    performing the conversion — often with exit code 0 and no output file. The

    reliable scripted fix is to give the headless run its own user profile, which

    lets it start as an independent instance even while the desktop copy runs:

    soffice -env:UserInstallation=file:///tmp/lo_convert_profile --headless

    --convert-to docx --outdir out file.doc (put the -env option before the other

    arguments). Also check the real exit status and that the expected output file

    exists afterward; the alternative of killing the desktop instance is brittle.

    '
- id: docx-verify-01
  answer: '1) Always work on a copy; never edit the only original in place. 2) Save with

    doc.save() to the target path. 3) Reopen the saved file with

    Document(saved_path) and assert the expected state: table count, row/column

    counts, key cell texts, picture count, order of content. 4) Sanity-check the

    package itself: it is a zip — run unzip -t or zipfile.testzip, list parts, and

    parse the key XML parts (document.xml, the .rels files) for well-formedness.

    5) Open it in a real consumer to make sure Word does not show a repair

    prompt — e.g. convert it with soffice --headless --convert-to pdf/txt into a

    scratch directory and check it succeeds. 6) Keep a backup of the pre-edit

    file for rollback. Only then hand the file over.

    '
- id: docx-verify-02
  answer: 'Common table-edit mistakes that trigger Word''s "unreadable content" repair

    (python-docx, being lenient, still opens them): a w:tbl left with zero w:tr

    rows, or a w:tr with zero w:tc cells; a w:tc containing no block-level element

    (every cell must contain at least one w:p); w:gridSpan or w:vMerge values

    inconsistent with the w:tblGrid column count (spanning more columns than

    exist); a broken vMerge chain (a "continue" cell with no restart cell above);

    deleting rows by removing w:tr elements but breaking merged-cell logic;

    schema-order violations (w:tblPr must be first child of w:tbl, w:tblGrid

    before the rows, pPr first inside w:p); references to relationship ids that no

    longer exist (drawings/hyperlinks after rel edits); malformed attributes

    (non-integer twips, duplicate ids); or raw XML inserted without the required

    namespace declarations. Word validates strictly, so any of these cause the

    repair dialog even though python-docx parses the file.

    '
- id: docx-verify-03
  answer: 'Removing the w:drawing only deletes the reference — the media part and its

    relationship remain in the package because python-docx serializes every part

    reachable through relationships. To purge it: find the old blip''s r:embed id

    before/while removing the drawing; confirm no other blip anywhere (body,

    headers, footers, other rels) resolves to that same image part; then drop the

    relationship from the owning part (part.drop_rel(rId)). With the last

    reference gone, python-docx will not write that media part into the saved

    package. Verify the artifact afterward by opening the saved .docx as a zip

    (zipfile.namelist() or unzip -l) and asserting that word/media no longer

    contains the old image file (compare names and byte sizes/hashes), and grep

    the .rels and document XML to confirm the old rId and filename appear nowhere.

    If it is still present, the rel was dropped from the wrong part or another

    reference remains.

    '
