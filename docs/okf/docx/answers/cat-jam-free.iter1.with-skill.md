# cat-jam-free — WITH-SKILL closed-book answers for docx (opencode-go, 2026-10-14)
# Delivery: derived/docx.cat-jam-free.SKILL.md body (front matter stripped) prepended to _prompts/docx.md; one
# `ocode run -m opencode-go/cat-jam-free -effort med` session, isolated HOME with every tool denied, cwd holding only the sheet;
# session audited in sqlite: 1 user + 1 assistant message, 0 tool calls.

- id: docx-model-01
  answer: |
    A .docx is a ZIP package of Open Packaging Convention parts: XML documents plus binary media and relationships. The main table XML normally lives in word/document.xml, but tables can also live in headers, footers, text boxes, and nested cells. A Word table is w:tbl containing w:tblPr and w:tblGrid (w:gridCol entries), repeated w:tr rows; each row may have w:trPr and contains w:tc cells; each cell has w:tcPr and one or more w:p paragraphs; each paragraph has w:pPr and w:r runs; each run has w:rPr and w:t text nodes. Drawings and fields are also nested under runs.
- id: docx-model-02
  answer: |
    "Widget C" may be split across multiple runs, so no single run's text equals the whole phrase. Match against paragraph.text, compute character offsets across runs, then write the replacement into the first run covering the match and blank the text of the other covered runs, preserving the first covered run's rPr. Repeat for header/footer and nested-table paragraphs.
- id: docx-model-03
  answer: |
    WordprocessingML table/cell widths are in dxa (twips): 1440 per inch and 20 per point. Image sizes in drawing extent elements are in EMU: 914400 per inch and 12700 per point.
- id: docx-locate-01
  answer: |
    Do not rely only on doc.tables: also inspect section.header.tables, section.footer.tables, and nested cell.tables. Identify the invoice table by content such as its header row ("Item", "Qty", "Price", "Amount") or a nearby caption, not merely by index. Before editing, inspect merged cells (gridSpan/vMerge), run formatting, track-revision state, field totals, number formats, shading, widths, and whether the table is in a header/footer.
- id: docx-locate-02
  answer: |
    row.cells returns one _Cell for each grid position, and a horizontally merged cell is repeated across its spanned positions. Thus len(row.cells) can exceed the number of physical w:tc elements, and deleting or counting by index can target the same merged cell or misidentify columns. Use row._tr.findall(w:tc), gridSpan tcPr/gridSpan, and vMerge attributes to interpret real cells.
- id: docx-rowdel-01
  answer: |
    python-docx has no table.delete_row() or row.delete() in its usual public API. Remove the underlying row element directly: tr = row._tr; tr.getparent().remove(tr). Afterward recompute every dependent amount, total, subtotal, tax, grand total, and field cached result.
- id: docx-rowdel-02
  answer: |
    Simply removing the restart row breaks the vertical merge: the continuation cells below lose their restart anchor, and the deleted cell's text is lost. Correctly, move the restart cell's paragraphs into the next row's first cell, make that next cell the restart by setting w:vMerge w:val="restart" and removing its continuation state, drop vMerge entirely if no continuation follows, then remove only the requested row. Recompute dependent numbers afterward.
- id: docx-rowdel-03
  answer: |
    The Total displays the stale cached field result; Word does not recalculate fields merely by opening the file. Recompute the sum and write it into the cached result run between the separate and end fldChars while preserving the field, optionally add w:updateFields to settings.xml to prompt the user, and verify the displayed value.
- id: docx-rowdel-04
  answer: |
    Hidden or zero-height rows remain in the document XML and package, so they are not deleted; the content can be restored, copied, or counted. Prove a real deletion by checking len(table.rows), counting w:tr in the table XML, reopening the file, and confirming the row's text and formatting are absent. Also inspect the ZIP/document.xml for the removed content.
- id: docx-cell-01
  answer: |
    cell.text = "SKU" replaces the paragraph runs with one plain run, so run-level bold, font, size, and color are lost. Cell-level tcPr properties such as shading, borders, and width survive. Preserve formatting by writing into runs[0].text and blanking other covered runs, or copying the old run's rPr to the new run.
- id: docx-cell-02
  answer: |
    Recompute the row Amount, the table Total or subtotal, tax, grand total, any other derived values, and figures mentioned in body text. Write each value through the cell's existing first run in its existing number format, decimals, thousands separators, currency, and alignment, and refresh dependent field cached results. Then verify all values and totals.
- id: docx-cell-03
  answer: |
    With w:trackRevisions enabled, a direct python-docx edit is untracked and may not appear as a revision; existing revisions in that region also remain. Decide with the user whether the edit should be tracked. For a clean edit, accept or reject existing revisions in that region first, then edit and confirm no w:delText still contains the old value. For a tracked edit, write proper w:del/w:ins markup.
- id: docx-relayout-01
  answer: |
    table.add_row() appends after the last row, including after the Total, and does not reliably copy row trPr, shading, or run formatting. Insert a correctly formatted middle row by deep-copying a matching body row's w:tr, adding it before the Total with total_tr.addprevious(new_tr), preserving alternating shading, and writing new text through each cell's existing first run. Recompute dependent totals afterward.
- id: docx-relayout-02
  answer: |
    table.add_column(width) appends a gridCol and cell at the right edge and widens the table. For a column after Item, insert a w:gridCol immediately after Item's gridCol and deep-copy Item's w:tc after Item's cell in every row, handling gridSpan merged rows so the spans still sum to the grid. Copy header/body formatting, then rebalance every gridCol and tcW so the total fits page width minus margins; also update tblW and layout settings.
- id: docx-relayout-03
  answer: |
    gridCol widths alone are not decisive because per-cell tcW, tblW, and autofit/fixed layout settings also control rendering. Update tcW for every cell, including spanned cells, adjust gridCol and tblW consistently, and ensure the table layout mode makes those widths authoritative. Word then uses the updated XML widths.
- id: docx-tblins-01
  answer: |
    doc.add_table() appends the table at the end of the document body, not after the target paragraph. Create the table, then move its w:tbl element immediately after the "Notes:" paragraph element with paragraph._p.addnext(table._tbl), keeping placement within the body and section structure. Then set style, widths, and content.
- id: docx-tblins-02
  answer: |
    The KeyError occurs because the named style is not present in the document's styles.xml. Use a style that exists in the document such as Table Grid, copy the table style and formatting from an existing table, or define the style first. For ruled lines without a missing style, create tblBorders manually.
- id: docx-tblins-03
  answer: |
    python-docx's add_table splits the text width evenly across columns, creating explicit gridCol and tcW values for that table width. To match an existing invoice table, copy its style, gridCol and tcW values, tblW, alignment, and header run formatting instead of using the default even split.
- id: docx-imgrep-01
  answer: |
    A picture is referenced by a drawing in document.xml whose a:blip has an r:embed attribute pointing to a relationship ID in the owning part's rels; that relationship targets the image part. To replace one picture, call part.get_or_add_image(path) on the part that owns the blip, set that blip's r:embed to the new rId, and keep wp:extent/a:ext, adjusting one dimension if the aspect ratio differs. Never overwrite an image part's bytes.
- id: docx-imgrep-02
  answer: |
    Image parts are shared/deduplicated; overwriting an image part's bytes changes every picture that resolves to that same part. The safe approach is to add a new image part, repoint only the target blip's r:embed, keep its extent, and drop the old relationship only if no other a:blip in that part references it.
- id: docx-imgrep-03
  answer: |
    A logo appearing on every page is usually in header parts (default, first-page, even-page), where its rId belongs to that header part's own relationships. It may be a wp:anchor image, which doc.inline_shapes does not list. Walk every section's default, first-page, and even-page headers and footers, iterate w:drawing/a:blip, and replace r:embed using each part's rels; then remove the old rel if unused.
- id: docx-imgins-01
  answer: |
    doc.add_picture() appends a new paragraph containing the image at the end of the document. Place it after "Approved by:" by adding it to that paragraph's run, e.g. paragraph.add_run().add_picture(path, width=Inches(1.5)), leaving height unspecified so the aspect ratio is preserved. Move or construct the paragraph correctly rather than leaving it at the end.
- id: docx-imgins-02
  answer: |
    A 3-inch image in a 1.2-inch cell overflows the cell visually; Word does not reliably shrink it automatically. Resize it to no wider than the cell before insertion, set width=Inches(1.2), leave height unspecified for aspect ratio, and account for cell margins. Use fixed cell widths or table layout if necessary.
- id: docx-legacy-01
  answer: |
    python-docx cannot open binary .doc files. Convert faithfully with LibreOffice headless (soffice --headless --convert-to docx --outdir output report.doc), verify the output's tables, rows, and images, edit the .docx, keep the original, and convert back to .doc only if requested. Avoid Pandoc and textutil for table editing.
- id: docx-legacy-02
  answer: |
    textutil converts through rich text and can flatten tables into paragraphs, destroying editable table structure. Detect this by opening the output and comparing table count, row counts, cell text, and image count against the source, and by inspecting document.xml for w:tbl. If tables are missing or flattened, use LibreOffice conversion instead.
- id: docx-legacy-03
  answer: |
    The conversion likely fails because another LibreOffice instance or a locked profile is already in use. Run it reproducibly with a unique temporary user installation: soffice -env:UserInstallation=file:///tmp/lo_profile_$$ --headless --convert-to docx --outdir out file.doc. The script should wait for completion, check the expected output path, and retry with a fresh profile if needed.
- id: docx-verify-01
  answer: |
    Keep a backup, edit a working copy, save under a new filename, then reopen and verify table counts, row counts, cell texts, derived numbers, field cached results, run formatting, and image relationships. Validate the ZIP and XML, check relationship targets, and inspect in Word or LibreOffice when available; compare against a saved baseline or PDF rendering.
- id: docx-verify-02
  answer: |
    Common mistakes include invalid element order (such as tcPr after p), missing required children (a w:tc without w:p, or missing w:tblGrid), gridSpan totals that do not match the table grid, invalid or dangling relationship IDs, broken namespace prefixes, improperly closed XML, and invalid merge state such as w:vMerge without a restart. These make python-docx parse while Word rejects the package.
- id: docx-verify-03
  answer: |
    Removing the drawing is not enough because the old bytes and relationship may remain. Drop the old relationship from the owning part only when no other a:blip references it, then inspect the ZIP for the old media filename and any references to its rId. Delete orphaned media parts and confirm by hashing or listing media entries that the confidential bytes are gone.
