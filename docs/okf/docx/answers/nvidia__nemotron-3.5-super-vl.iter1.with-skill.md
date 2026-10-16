# nvidia/nemotron-3.5-super-vl — with-skill answers for docx (openrouter, 2026-10-16); answerer saw only _prompts/docx.md; derived/docx.nvidia__nemotron-3.5-super-vl.SKILL.md body (front matter stripped) prepended to the sheet as active guidance
- id: docx-model-01
  answer: |
    A .docx is physically a ZIP package (Open Packaging Convention) holding parts such as word/document.xml, word/styles.xml, relationships, and word/media/*. A table's data lives as WordprocessingML, mainly in word/document.xml (the same table XML can also appear in header/footer parts). From the table down to the text the elements are: w:tbl > w:tblPr (with w:tblBorders/w:tblLayout/w:tblW) and w:tblGrid/w:gridCol > w:tr (+ w:trPr) > w:tc (+ w:tcPr: w:tcW, w:shd, vMerge, gridSpan) > w:p (+ w:pPr) > w:r (+ w:rPr) > w:t.
- id: docx-model-02
  answer: |
    Because "Widget C" can span multiple runs — any formatting change mid-string splits it so no single run.text equals the whole phrase. It may also be split across paragraphs, hidden inside a field (w:instrText / w:separate), use non-breaking spaces, or live outside the body (header/footer/text box). Find it reliably by substring matching ("Widget C" in run.text) across all runs in cell.paragraphs, or by concatenating paragraph.text / cell.text, or by XPath over all w:t in the cell and normalizing whitespace; then replace by editing the matching w:t node(s) or run.text, rebuilding runs if the phrase spans runs. Also check fields, headers, and footers.
- id: docx-model-03
  answer: |
    Table and cell widths use twips (dxa): 1 twip = 1/20 point = 1/1440 inch, so w:tblW, w:tcW, and w:gridCol @w are in twips (1440 twips = 1 inch, 20 twips = 1 point). Image sizes use EMU in wp:extent cx/cy and the inner a:ext: 914400 EMU = 1 inch, and 12700 EMU = 1 point.
- id: docx-locate-01
  answer: |
    Do not trust table indexes. Identify the invoice table by content: iterate doc.tables and pick the one whose header/first-row cells (or the paragraph immediately preceding it) contain expected labels such as "Invoice", "Item", "Qty", "Total". Then iterate that table's rows and match "Gadget D" across cell text/runs using substring, case-insensitive comparison. Before editing, confirm there is exactly one match within that table, account for merged cells (w:gridSpan / w:vMerge) so cell indexes map to real columns, verify the row is not a header or total row, check document protection/track-changes state, and back up the file.
- id: docx-locate-02
  answer: |
    row.cells returns one Cell per w:tc element, not one per logical column. With gridSpan=2 on the first cell, row.cells has one fewer entry than the logical column count, so index [1] is actually the third logical column and deleting/editing "the second cell" by index hits the wrong cell. Iterating row.cells to count columns undercounts. Use the w:tblGrid w:gridCol count, or sum each cell's w:gridSpan (default 1) from w:tcPr to map cell index to logical column.
- id: docx-rowdel-01
  answer: |
    python-docx has no table.delete_row() and no row.delete(). Remove the row at the XML level: table._tbl.remove(row._tr) (equivalently row._tr.getparent().remove(row._tr)), which requires lxml, then save the document.
- id: docx-rowdel-02
  answer: |
    Removing only the restart row breaks the vertical merge: the following rows still carry w:vMerge (continue) but now have no restart cell to anchor to, so Word may mis-render the span, lose the merged text, or flag the document for repair. Do it correctly by deleting the whole merged set — the restart row plus all following continue rows — together, or before deleting the restart row switch the next row's first cell w:vMerge to val="restart" so the chain stays valid, then delete the original restart row. Apply the same care to horizontal merges (w:gridSpan).
- id: docx-rowdel-03
  answer: |
    Word fields store a cached result, so deleting the line-item row does not recompute { =SUM(ABOVE) }; the Total cell still shows the old cached value until fields are updated (Word may prompt, or require F9 / print / a macro). Handle it by recomputing the dependent values yourself (line total, then invoice total), writing the new cached result, and marking the field dirty so Word recalculates on open — or recalc via Word/LibreOffice automation. Never assume the field self-updates on save.
- id: docx-rowdel-04
  answer: |
    It is not a deletion: the w:tr remains in the table grid, still participates in layout and merges, and its hidden/white text is still present in the XML and searchable; it only looks gone. Prove real removal by reopening document.xml and confirming the w:tr and its cell text are absent, that table.rows has one fewer row, and that searching the unzipped package finds neither the row's text nor its w:tr. Verify in Python by iterating table.rows after saving.
- id: docx-cell-01
  answer: |
    cell.text = "SKU" wipes the cell's paragraphs and writes one new paragraph containing a single default run. What survives: cell-level properties — w:tcPr shading (w:shd dark fill), borders, vMerge/gridSpan, vertical alignment, and cell margins — plus the table style. What is lost: the run formatting (bold, 9 pt, white color, font) and paragraph formatting (alignment, spacing), which revert to defaults. The result is default-sized default-colored text still sitting on the dark shading.
- id: docx-cell-02
  answer: |
    Dependent numbers must change too: that line's line total (Qty × unit price), and the invoice total (sum of line totals / { =SUM(ABOVE) }), plus tax, grand total, and balance due if present. Make it consistent by recomputing each dependent cell in Python and writing the new values; for the Word field, update its cached result and mark the field dirty so Word recalculates on open (or recalc via Word/LibreOffice). Also update any item-count text — never edit Qty alone.
- id: docx-cell-03
  answer: |
    python-docx writes plain text with no revision markup, so despite w:trackRevisions the edit is silent: no w:ins/w:del, no author/date, no revision id; reviewers cannot see it as a tracked change or accept/reject it. trackRevisions only governs edits made through the Word UI. If a tracked change is required, you must hand-build w:ins/w:del (with author/date/id run properties) in raw XML — python-docx has no API for it. Consider accepting existing revisions first and whether the document is edit-protected.
- id: docx-relayout-01
  answer: |
    table.add_row() only appends a row at the end of the table and cannot insert at an index. To insert between "Service G" and the Total row, deepcopy a suitable template row's _tr (one whose shading fits the slot, or fix w:shd after copying) and link it with ref_tr.addnext(new_tr) or total_tr.addprevious(new_tr). Fill it by writing into each cell's first existing run and removing extra runs/paragraphs — never cell.text on the clone, which would discard bold/size/color. A new row does not change the grid; set the text through runs whose w:rPr matches the template.
- id: docx-relayout-02
  answer: |
    table.add_column(width) appends a column at the end only (one new w:gridCol plus one cell per row) and cannot insert at an index; its width handling is minimal. To insert "SKU" after Item, insert a w:gridCol at that index in w:tblGrid and one w:tc per row at that index (copy a neighbor cell to carry w:tcPr/shading/runs), set each new cell's w:tcW, then rebalance every w:gridCol w:w and w:tcW so the total stays at or below the section text width. Set w:tblLayout fixed, match the table style and the header row's shading/bold-white runs, and recompute widths so columns look uniform and stay within the margins.
- id: docx-relayout-03
  answer: |
    Word honors per-cell w:tcW (and fixed w:tblLayout) over w:gridCol; gridCol is only a default, so changing gridCol alone does nothing while w:tcW values exist. Fix it by setting every cell's w:tcW in every row to match the new gridCol widths, adding <w:tblLayout w:type="fixed"/>, keeping w:tblW consistent, and ensuring the sum of gridCol (and per-row tcW) stays at or below the section text width. Then Word displays the new widths.
- id: docx-tblins-01
  answer: |
    doc.add_table() appends the table at the end of the document body — not at the "Notes:" paragraph. Place it correctly by creating the table, then moving its w:tbl element immediately after the target paragraph: notes_paragraph._p.addnext(table._tbl). Then set widths, style, and cell text. Equivalently, build the w:tbl and insert it with addnext directly after the paragraph.
- id: docx-tblins-02
  answer: |
    The KeyError happens because table.style looks up the style_id in this document's styles.xml, and "Grid Table 4 Accent 1" is not defined in the company template — built-in Word table styles are not automatically present in every docx. Get a ruled table by using a style that exists (commonly "Table Grid"), or add borders directly with w:tblBorders in w:tblPr (per-edge sz/space/color), or import the missing style into styles.xml from another docx. Check doc.styles names first.
- id: docx-tblins-03
  answer: |
    By default doc.add_table() writes w:tblW type="auto" and makes every w:gridCol w:w and every cell w:tcW equal to the section text width divided by the column count (page width minus left/right margins, evenly split) — fixed even widths, not content-sized. To match the existing invoice table, copy its w:tblW (type and value), w:tblLayout (fixed), every w:gridCol w:w, every cell w:tcW, the table style (it must exist in this document's styles.xml or KeyError), borders, and header-row shading/run formatting, then write text through runs with matching w:rPr. Keep the total width at or below the text width.
- id: docx-imgrep-01
  answer: |
    The reference chain is w:drawing > wp:anchor/wp:inline > pic:pic > pic:blipFill > a:blip r:embed="rIdN"; the rId is a relationship of the part holding the drawing (document, header, or footer) targeting word/media/imageN.*. Size lives in wp:extent cx/cy and the inner a:ext in EMU, not in the image file. Replace one logo on the owning part: call rId, image = part.get_or_add_image(path) (deduplicated by SHA1, giving a new part), then set only that drawing's a:blip r:embed to the new rId. Keep wp:extent; if the aspect ratio changed, set cy = cx * image.px_height // image.px_width as integers in both wp:extent and a:ext. Never overwrite an existing part's blob.
- id: docx-imgrep-02
  answer: |
    python-docx deduplicates image parts by SHA1, so one word/media part can back several pictures; page 3's picture referenced the same part. Overwriting image_part._blob therefore changes every picture using that part — not just the logo. Never mutate a shared part's blob: add a new part via get_or_add_image and repoint only the target blip's r:embed.
- id: docx-imgrep-03
  answer: |
    It is in a header or footer part (section.header, possibly first-page/even-odd headers, or a header linked to previous), not the body — which is why doc.inline_shapes (body-only) does not list it; a floating wp:anchor would also be missed. Find it by iterating doc.sections and each header/footer part, inspecting part.element for w:drawing / a:blip (and wp:anchor). Replace through that part's get_or_add_image and repoint the blip; do this in every part that carried the picture, then drop the old rel when no a:blip[@r:embed=old_rId] remains in any part.
- id: docx-imgins-01
  answer: |
    doc.add_picture() appends an inline image paragraph at the end of the document body — not after "Approved by:". Place it correctly by adding a run to the "Approved by:" paragraph and calling run.add_picture("signature.png", width=Inches(1.5)) with height unset, so python-docx computes the height from the image's aspect ratio; or append via document.add_picture then move the resulting paragraph/w:drawing with _p.addnext after the target paragraph. Compute cy = cx * px_height // px_width in EMU and keep wp:extent and a:ext consistent.
- id: docx-imgins-02
  answer: |
    A 3-inch image in a 1.2-inch cell overflows: with auto layout the cell/column may widen, pushing the table beyond the margins; with fixed layout it overflows the cell visually or is constrained awkwardly. Size it by computing usable width = cell width minus cell left/right margins (about 1.2 in minus margins), setting the image width to that in EMU and height = cx * px_height // px_width, inserting via a run in the cell (run.add_picture) so wp:extent/a:ext match the aspect. Optionally widen the cell/grid and rebalance, keeping total table width at or below the text width with w:tblLayout fixed.
- id: docx-legacy-01
  answer: |
    No — python-docx reads only OPC .docx files, not legacy binary .doc (OLE2 Compound File) documents. Workflow: convert report.doc to .docx first (LibreOffice soffice --headless --convert-to docx, Word, or macOS textutil), open the result with python-docx, delete the row via table._tbl.remove(row._tr), and save as .docx. Convert back to .doc only if required, expecting formatting loss.
- id: docx-legacy-02
  answer: |
    textutil uses Apple's importers and may lose or alter table features: merged cells (w:gridSpan / w:vMerge), column widths and cell margins, borders and shading, fields, headers/footers, floating images, and text wrapping. Detect it by round-trip checking: reopen the converted .docx with python-docx and compare table count, row/column counts, merged-cell markers, tcW widths, header text, and fields against the original (extract the original text via catdoc/antiword or compare PDF renders before and after).
- id: docx-legacy-03
  answer: |
    A running LibreOffice GUI instance holds the user profile, so the headless convert cannot claim the profile and exits silently with no output file. Run it reliably by closing all LibreOffice instances and using a fresh isolated profile per run with -env:UserInstallation=file:///tmp/lo-<unique> plus --headless --norestore --outdir <dir>, then check the exit code and confirm the output file actually appeared; clean up or retry if the profile is locked.
- id: docx-verify-01
  answer: |
    Save to a new file first and keep the original as backup; never overwrite the only copy until verified. Verify by reopening with python-docx (round-trip parse), asserting the expected edits (row present/absent, cell text, table count), then unzip the package: confirm word/document.xml is well-formed, check media hashes and every .rels for leftover relationships to old media, grep XML parts for stale rIds, and ideally render in Word or LibreOffice (or convert to PDF) to confirm no "unreadable content" repair and correct layout. Replace the original only after all checks pass.
- id: docx-verify-02
  answer: |
    Common table mistakes: removing a w:tc or w:tr without rebalancing w:tblGrid w:gridCol count versus cells per row (accounting for gridSpan); schema child-order violations (w:tbl: w:tblPr, w:tblGrid, then w:tr+; w:tr: w:trPr then w:tc+; w:tc: w:tcPr then content such as w:p); an empty w:tc with no paragraph content (needs <w:p/>); mismatched or missing w:tblGrid/w:tblPr and unbalanced gridCol versus tcW sums; a broken w:vMerge restart/continue chain after row deletion; w:gridSpan exceeding the column count; and malformed XML from string edits (unclosed tags, missing namespaces). python-docx is lenient; Word is strict and will report unreadable content.
- id: docx-verify-03
  answer: |
    Prove it from the saved zip, not from assumptions: unzip and sha1-hash every word/media/* and assert the old image's hash is absent; parse every .rels (document, headers, footers) and assert no relationship Target still points to the old media file; grep every XML part for the old rId and old media filename — zero hits; and confirm no a:blip[@r:embed="old_rId"] remains in any part (count r:embed yourself, then drop the rel with Part.drop_rel on r:id). A part no rel reaches is not written on save, so the old media file's absence alone is not proof until the rels and rIds are gone too.
