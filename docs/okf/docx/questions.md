# Word Editing Benchmark — Human Render

> **Generated from `questions.yaml` (corpus_rev 1). Do not grade from this file
> — `questions.yaml` is the source of truth.** If they disagree, the YAML wins.

Legend: **W** = weight (1–3), **D** = difficulty. Rubric shows scored points
(`•`) and partial-credit levels (`~`).

---

### docx-model-01 · docx-model · W3 · easy
**Q:** What is a .docx file physically, and where does a table's data live? Name the XML elements that make up a Word table, from the table down to the text.
**A:** A .docx is a ZIP (OPC package) of XML parts. The body is word/document.xml. Headers, footers, footnotes and media are separate parts, linked by relationships (*.rels). A table is w:tbl, which contains w:tblPr (style, width, borders), w:tblGrid with one w:gridCol per grid column, then one w:tr per row. Each w:tr has optional w:trPr and one w:tc per cell. Each w:tc has optional w:tcPr (width, span, merge, shading) and one or more w:p paragraphs. Each paragraph holds w:r runs with w:t text.
• zip of XML parts; body in word/document.xml; other parts linked by rels • w:tbl > w:tblGrid/w:gridCol + w:tr > w:tc > w:p > w:r > w:t ~ says 'XML inside a zip' without the table element hierarchy

### docx-model-02 · docx-model, table-locate · W3 · medium
**Q:** You search a table cell for "Widget C" by iterating paragraph runs and checking `run.text == "Widget C"`, and find nothing, although the cell clearly shows "Widget C". Why, and how do you find and replace the text reliably?
**A:** Word splits a paragraph's text across several w:r runs, because of formatting changes, spell-check marks, revision ids or edit history. For example, "Wid" and "get C" can be separate runs. Match on paragraph.text, which joins the runs. To replace, write the new text into the first run(s) that covered the match and clear or remove the rest, so the first run's formatting is kept. Never compare run by run.
• text is split across multiple runs (formatting/proofing/rsid) • match on paragraph text, then rewrite runs keeping the first run's formatting ~ suggests regex on paragraph.text and paragraph.text = new (loses run formatting)

### docx-model-03 · docx-model, table-relayout · W2 · medium
**Q:** Which units does WordprocessingML use for table/cell widths and for image sizes, and how do they convert to points and inches?
**A:** Table, cell and grid widths (w:tblW, w:tcW with type "dxa", w:gridCol w:w) and page margins are in twentieths of a point (twips): 1440 per inch, 20 per point. Image extents (wp:extent cx/cy, a:ext) are in EMU: 914400 per inch, 12700 per point. python-docx Length objects convert between them (Inches, Pt, Emu, Twips, .twips, .pt).
• table/cell widths in twips (dxa): 1440/inch, 20/pt • images in EMU: 914400/inch, 12700/pt

### docx-locate-01 · table-locate · W3 · medium
**Q:** A report has several tables. How do you find "the invoice table" and the row for item "Gadget D" robustly with python-docx, and what should you check before editing?
**A:** Iterate doc.tables. Nested tables are inside cells, via cell.tables. Identify the table by its header row text (e.g. "Item" and "Qty"), not by its index. Find the row whose first-column cell text, stripped, equals "Gadget D". Assert exactly one match; zero or several means stop and ask. Remember that headers and footers have their own tables (section.header.tables), which doc.tables does not include.
• identify the table by header content, not a hard-coded index • match the row by cell text and assert exactly one match ~ doc.tables[0].rows[n] by position only

### docx-locate-02 · table-locate, docx-model · W2 · hard
**Q:** In python-docx, a table's first row has a cell that spans two columns. What does `row.cells` return for that row, and what goes wrong if you delete "the second cell" by index or iterate `row.cells` to count columns?
**A:** row.cells returns one entry per GRID column. A merged cell (w:gridSpan=2) is returned twice as the same _Cell object, over the same w:tc. The row holds fewer w:tc elements than there are grid columns. Indexes from row.cells therefore do not map to w:tc elements. Deleting "the second cell" deletes the merged cell, or deletes the wrong one. Deduplicate by the underlying _tc, or work on row._tr.tc_lst while accounting for gridSpan. Vertical merges (w:vMerge) likewise repeat the top cell.
• row.cells is per grid column; merged cell appears repeatedly (same _tc) • dedupe by _tc / use tc_lst + gridSpan before indexing or deleting

### docx-rowdel-01 · row-delete · W3 · easy
**Q:** How do you delete a row from a Word table with python-docx? Is there a table.delete_row() or row.delete() method?
**A:** No. python-docx has no row-delete API. Remove the row's XML element from its parent: `tr = row._tr; tr.getparent().remove(tr)`. Unlike a PDF, Word reflows, so the rows below move up by themselves. No manual shifting is needed. Then update any Total that depended on the row.
• no delete API; remove row._tr from its parent • Word reflows: no gap to close manually; update totals ~ invents a delete_row()/remove_row() method

### docx-rowdel-02 · row-delete, table-locate · W2 · hard
**Q:** The row you must delete begins a vertical merge: its first cell has `<w:vMerge w:val="restart"/>` and the next two rows' first cells have `<w:vMerge/>`. What happens if you just remove the row, and how do you do it correctly?
**A:** The restart marker leaves with the row. The continuation cells below then attach to whatever merged region is above them, or are left without a start, which gives a corrupted or wrong merge. Before deleting, move the merged cell's content to the next row's cell and set w:vMerge w:val="restart" on it. If only one row remains in the merge, drop vMerge. Then delete the row.
• continuation cells lose their restart → broken/wrong merge • promote the next row's cell to restart (move content) before deleting

### docx-rowdel-03 · row-delete, cell-edit · W2 · hard
**Q:** The invoice's Total cell contains a Word field `{ =SUM(ABOVE) }`. You delete a line-item row with python-docx and save. What does the Total show when the file is opened, and how should you handle it?
**A:** A field stores its instruction (w:instrText / w:fldSimple) plus a CACHED result: the runs between the separate and end fldChars. python-docx never recalculates, so the old sum stays displayed until fields are updated. Word does not recalculate on open by default. Compute the new total and write it into the cached result run, keeping the field. Optionally also set <w:updateFields w:val="true"/> in settings.xml, which makes Word offer to update fields on open (it prompts the user).
• field result is cached; python-docx/Word-on-open do not recompute it • write the recomputed value into the field's result run (optionally updateFields) ~ only says 'update the total' without noticing the field/cached result

### docx-rowdel-04 · row-delete, verify-safety · W2 · medium
**Q:** To "delete" a row quickly, someone sets its height to 0 (exact) and makes its text white, or marks its runs hidden. Why is that not a deletion, and how do you prove the row is really gone?
**A:** The row and its text are still in document.xml. They are extractable, searchable and copyable, they show when hidden text is displayed or the formatting changes, and they still count in fields like SUM(ABOVE). Remove the w:tr. To verify, re-open the saved file and check that no w:t in any part (document.xml, headers and footers) contains the row's text, and that the row count dropped by exactly one.
• hidden/white/zero-height text is still in the XML (extractable, counts in fields) • remove the w:tr; verify by re-opening and searching all XML text for the old value

### docx-cell-01 · cell-edit · W3 · easy
**Q:** A header cell is bold, 9 pt, white text on dark shading. You change its text with `cell.text = "SKU"` in python-docx. What formatting survives and what is lost?
**A:** cell.text replaces all the cell's content with ONE new plain run. Run formatting (bold, size, colour, font) is lost; the new run inherits only paragraph and style defaults. Cell-level formatting in w:tcPr (shading, width, borders) survives. To keep run formatting, set the text of the first existing run and clear the others, or copy the old run's rPr onto the new run.
• run formatting (bold/size/colour/font) is lost; tcPr shading/width kept • edit the existing first run's text (clear the rest) or copy rPr ~ says cell.text keeps formatting

### docx-cell-02 · cell-edit, table-locate · W2 · medium
**Q:** You change Qty for one invoice line from 5 to 12. What else in the document must change, and how do you make the numbers consistent?
**A:** Change the row's Amount (qty × unit price) and the Total, plus any subtotal, tax or grand total and any number repeated in the text (e.g. "Total due: …"). Compute these from the table data rather than editing strings by hand. Keep the existing number format (decimals, thousands separators, currency) and the alignment of each cell's paragraph. If the totals are fields, update their cached result.
• recompute dependent cells: row amount + total (and other derived numbers) • keep number format/alignment; compute from data, not ad-hoc strings

### docx-cell-03 · cell-edit, verify-safety · W2 · medium
**Q:** The document has Track Changes turned on (w:trackRevisions in settings) and the user wants a clean edit of a cell. What happens if you just rewrite the w:t text with python-docx, and what should you consider?
**A:** python-docx writes directly: no w:del/w:ins revision marks are produced, so the change is invisible in the review pane even though tracking is on. Existing revisions in the cell (w:del with w:delText, w:ins) are also part of the XML. Old deleted text can survive in w:delText, and python-docx 1.2.0's paragraph.text (and .runs) skips runs inside both w:del and w:ins, so pending inserted text is invisible to it too. Ask whether the edit must be tracked. If so, write w:del/w:ins markup. Otherwise accept or reject the existing revisions in that region first, and check that w:delText no longer holds the old value.
• direct edit bypasses tracking (no w:ins/w:del); existing revisions complicate text • decide tracked vs clean; resolve revisions and check w:delText for the old value

### docx-relayout-01 · table-relayout · W3 · medium
**Q:** You must insert a new row after "Service G" and before the Total row. What does python-docx's `table.add_row()` do, and how do you insert a correctly formatted row in the middle?
**A:** add_row() appends a new w:tr at the BOTTOM, after the Total. Its cells get only a width (tcW from the grid) and one empty paragraph. It copies no trPr (height, header flags), no tcPr shading or borders and no run formatting. For a mid-table row, deep-copy an existing body row that has the right style (copy.deepcopy(row._tr)), insert it with ref_tr.addnext(new_tr) or total_tr.addprevious(new_tr), then set each cell's text through its existing run. With alternating shading, choose or adjust the copied row so the banding still alternates.
• add_row appends at the end and copies no formatting (only widths) • deepcopy a styled row and insert with addnext/addprevious • fill text via existing runs; keep banding/shading consistent

### docx-relayout-02 · table-relayout · W3 · hard
**Q:** You add a "SKU" column after the Item column. What does `table.add_column(width)` do, and what must you do so the table stays inside the page margins and looks like the other columns?
**A:** add_column appends a w:gridCol and one new w:tc at the RIGHT end of every row. It never inserts in the middle and does not shrink the other columns, so the grid gets wider than the text width (section.page_width − left_margin − right_margin). To insert in the middle: add a w:gridCol after the Item column's gridCol, and in every row insert a deep copy of the Item cell after it (item_tc.addnext), taking care with gridSpan cells such as a merged Total. Then rebalance: new widths for all gridCols and each cell's tcW, summing to at most the text width. Copy the header cell's formatting (shading, white bold text) for the new header, and the body style for the new body cells.
• add_column appends at the far right and widens the table (no rebalance) • insert gridCol + a tc per row at the right index (mind gridSpan) • rebalance gridCol and tcW to fit text width; copy header/body cell formatting

### docx-relayout-03 · table-relayout, docx-model · W2 · medium
**Q:** You changed the w:gridCol widths of a table but Word still shows the old column widths. Why, and what must you update?
**A:** Each cell carries its own preferred width in w:tcPr/w:tcW, and Word, especially with autofit layout, lays out from the cell widths (and tblW) more than from tblGrid. LibreOffice leans more on the grid. Update gridCol, every cell's tcW (and tblW if set) consistently. Set a fixed layout (w:tblLayout w:type="fixed") if the widths must hold exactly.
• tcW per cell (and tblW) also define width; Word uses them • update gridCol + all tcW consistently (fixed layout if exact)

### docx-tblins-01 · table-insert · W3 · medium
**Q:** You must insert a new table right after the paragraph "Notes:" in the middle of a document. Where does python-docx's `doc.add_table()` put it, and how do you place it correctly?
**A:** doc.add_table() appends the table at the END of the body, before the final sectPr. Create it, then move its element: `notes_p._p.addnext(table._tbl)`. Add a heading paragraph the same way if one is needed. Word reflows the content after it. python-docx has no insert_paragraph_after; use next_paragraph.insert_paragraph_before, or addnext with a new w:p.
• add_table appends at the end of the body • move the w:tbl after the anchor paragraph (addnext)

### docx-tblins-02 · table-insert · W2 · medium
**Q:** You set `table.style = "Grid Table 4 Accent 1"` on a table added to an existing company template, and python-docx raises KeyError. Why, and how do you get a ruled table?
**A:** python-docx can only use styles defined in the document's styles.xml. Built-in Word styles that the template never used are not present, and a missing name raises KeyError. List doc.styles and use an existing table style, such as "Table Grid", or the style of an existing table in the document. Otherwise set the borders explicitly in w:tblPr/w:tblBorders or per-cell w:tcBorders.
• style must exist in the document's styles.xml (KeyError otherwise) • use an existing style (e.g. Table Grid / an existing table's style) or explicit tblBorders

### docx-tblins-03 · table-insert, table-relayout · W2 · medium
**Q:** How wide is a table from `doc.add_table(rows, cols)` by default, and what should you set so it matches the existing invoice table?
**A:** python-docx splits the section's text width (page width minus the margins) evenly across the gridCols and cells. To match the existing table, reuse its style, its column widths (gridCol plus tcW for each cell), its alignment and its header formatting (shading, bold/white runs). Keep the total width at or below the text width. Put the text into runs whose formatting matches, not bare cell.text, when the header style is not applied by the table style.
• default = text width split evenly across columns • match existing: style, widths (gridCol+tcW), header formatting

### docx-imgrep-01 · image-replace, docx-model · W3 · medium
**Q:** How does a picture in document.xml reference its image bytes, and how do you replace ONE picture (a logo) with a new file while keeping its position and size?
**A:** The picture is a w:drawing (wp:inline or wp:anchor) that contains pic:pic/a:blip with an r:embed rId. The rId is a relationship of the document part that points to an image part (word/media/imageN.*). Size comes from wp:extent/a:ext (EMU), not from the file. To replace one picture: add the new image as a part (rId, _ = doc.part.get_or_add_image(path)), point that blip's r:embed at the new rId and keep the extents. If the aspect ratio differs, adjust one dimension. If nothing else uses the old rId, remove that rel so the old media leaves the package.
• a:blip r:embed → document-part rel → word/media image part; size from wp:extent • add new image part and repoint this blip's r:embed; keep extents (aspect) • remove the old rel/media if unused so the old image is gone

### docx-imgrep-02 · image-replace · W2 · hard
**Q:** You replace a logo by overwriting the bytes of its image part (`image_part._blob = new_bytes`). The logo changed, but so did a different picture on page 3. Why?
**A:** python-docx deduplicates images by SHA1: adding the same image twice reuses one image part, and several blips (or rIds) can point to it. Overwriting the part's blob changes every picture that references it. To change one picture, give it its own new image part and repoint its r:embed. Check first how many blips reference the rId or part.
• image parts are shared (SHA1 dedupe / several blips per part) • replace one instance by a new part + repointing its blip; check references first

### docx-imgrep-03 · image-replace, table-locate · W2 · medium
**Q:** The company logo is shown at the top of every page. `doc.inline_shapes` does not list it. Where is it, and how do you find and replace it?
**A:** It is in a header part (section.header, and first_page_header or even_page_header if those are enabled), not in the body. It may also be an anchored (floating, wp:anchor) picture; inline_shapes lists only body wp:inline pictures. Walk every section's header parts (and footers), find a:blip elements, and replace through that PART's relationships: the rId belongs to the header part, not the document part. Linked headers are shared across sections.
• logo lives in a header part and/or is anchored → not in inline_shapes • search header/footer parts' a:blip; use that part's rels for the replacement

### docx-imgins-01 · image-insert · W3 · medium
**Q:** Insert signature.png 1.5 inches wide right after the paragraph "Approved by:", keeping its aspect ratio. Where does `doc.add_picture()` put it, and how do you place it correctly?
**A:** doc.add_picture() appends a NEW paragraph at the end of the document. Instead, add a run in the target paragraph (or in a new paragraph inserted after it) and call run.add_picture(path, width=Inches(1.5)). Giving only width keeps the aspect ratio, because height is computed from the pixel dimensions. Set the paragraph alignment (e.g. right) if required.
• doc.add_picture appends at the end • run.add_picture in the target/new paragraph; width only keeps aspect

### docx-imgins-02 · image-insert, table-relayout · W2 · medium
**Q:** You put a 3-inch-wide image into a table cell that is 1.2 inches wide. What happens, and how do you size it?
**A:** An inline picture wider than its cell either stretches the column (autofit), which distorts the table, or overflows or is clipped (fixed layout). Size the picture from the cell's usable width: cell width (tcW or gridCol) minus the cell margins. Pass width=… only, so height follows the aspect ratio, and put it into the cell's paragraph via cell.paragraphs[0].add_run().add_picture().
• oversized inline image widens/distorts the table (or overflows) • size to the cell's usable width (minus margins), width only for aspect

### docx-legacy-01 · legacy-doc · W3 · easy
**Q:** The user gives you report.doc (Word 97–2003) and asks you to delete a table row. Can python-docx open it? What is the workflow?
**A:** No. .doc is the binary OLE2 compound-file format, not a ZIP of XML, and python-docx raises an error (PackageNotFoundError / not a zip). Renaming the extension does not help. Convert it with LibreOffice headless (soffice --headless --convert-to docx --outdir out report.doc), edit the .docx, and convert back to .doc only if the user needs .doc (--convert-to doc). Keep the original, and check the converted file before editing, since the conversion can change the layout.
• .doc is binary OLE2, python-docx cannot open it (renaming doesn't help) • convert with LibreOffice headless → docx, edit, convert back if needed

### docx-legacy-02 · legacy-doc, verify-safety · W2 · medium
**Q:** On macOS without LibreOffice you convert a .doc with `textutil -convert docx`. What risk does that carry for a table-editing task, and how do you detect it?
**A:** textutil goes through a rich-text model and loses Word structure. Tables can be flattened into plain paragraphs (no w:tbl), and styles, fields and images may be dropped. After any conversion, open the result and count the tables, rows and images against what the document should contain before editing. If they are missing, use a faithful converter (LibreOffice or Word) or ask the user.
• textutil/lossy converters can drop tables/styles/images • verify tables/rows/images after conversion before editing; use LibreOffice/Word

### docx-legacy-03 · legacy-doc · W1 · hard
**Q:** `soffice --headless --convert-to docx file.doc` exits immediately and no output file appears, while a LibreOffice window is open on the desktop. Why, and how do you run it reliably from a script?
**A:** A running LibreOffice instance holds the user profile. The headless call hands the job to that instance, or fails because of the profile lock, and produces nothing. Use a separate throwaway profile, e.g. -env:UserInstallation=file:///tmp/lo_profile_$$, pass an explicit --outdir, and check that the output file exists afterwards (the exit code alone isn't reliable).
• running instance / profile lock swallows the headless conversion • separate -env:UserInstallation profile + --outdir, verify output exists

### docx-verify-01 · verify-safety · W3 · easy
**Q:** What is the safe save-and-verify procedure after editing invoice.docx with a script?
**A:** Never overwrite the source. Write to a new file (or keep a copy of the original). Re-open the output with python-docx to prove it parses. Check the intended change: row count, the exact cell texts in order, totals. Check that nothing else changed, for example by diffing the other paragraphs, tables and parts against the original. Search every XML part for text that should be gone.
• save to a new file / keep the original untouched • re-open and assert exact expected content + unchanged remainder

### docx-verify-02 · verify-safety, docx-model · W2 · medium
**Q:** After raw-XML edits a .docx still opens in python-docx, but Word says "Word found unreadable content". Name common structural mistakes in table edits that cause this.
**A:** Common causes: - a w:tc with no w:p (every cell must end with a paragraph); - the number of gridCols not matching the cells per row, counting gridSpan; - a broken vMerge sequence; - elements out of schema order (e.g. tcPr not first in w:tc, trPr not first in w:tr); - a blip rId with no relationship; - duplicate drawing ids (wp:docPr id) after copying pictures. python-docx does not validate these, so check them yourself.
• cell without w:p / gridCol-vs-cells mismatch / broken vMerge • schema order, dangling rIds or duplicate docPr ids; python-docx doesn't validate ~ only 'check the XML is well-formed'

### docx-verify-03 · verify-safety, image-replace · W2 · medium
**Q:** You replaced a confidential logo by removing its w:drawing and inserting a new picture. How do you make sure the old image is not still inside the .docx?
**A:** Removing the w:drawing does not remove the relationship or the media part. python-docx saves every part that is still reachable through rels, so word/media/imageN.* stays in the zip. Remove the rel from the owning part once no other blip uses that rId. Then list the saved zip and check that no entry still contains the old image bytes (hash each word/media file).
• removing the drawing leaves the rel + media part in the zip • drop the unused rel; verify by listing/hashing zip media
