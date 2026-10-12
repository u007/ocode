# step-5-preview-free — closed-book answers for docx (opencode-go, 2026-10-12); answerer saw only _prompts/docx.md
Answering closed-book from my own knowledge of OOXML/python-docx. No files opened, no lookups. Assumption stated: where I am not certain of an exact library default, I say so rather than guess.

# cleanup: dropped a hallucinated record `docx-imgins-03-placeholder` (answer: "placeholder"; no such question) and a second, near-identical `docx-verify-03` emitted by the model.
- id: docx-model-01
  answer: |
    A .docx is physically a ZIP archive (an OPC package): a set of XML parts plus binary parts, glued together by .rels relationship files and a [Content_Types].xml. The body content lives in word/document.xml; headers/footers are word/headerN.xml / word/footerN.xml; images are word/media/imageN.*; relationships are in word/_rels/document.xml.rels.

    Table data lives in w:tbl elements inside document.xml (or a header/footer part). Element hierarchy top-down:
      w:tbl
        w:tblPr  (table properties: tblStyle, tblW, jc, tblInd, tblBorders, tblLayout, tblCellMar, tblLook)
        w:tblGrid
          w:gridCol  (one per column, w:w = column width in twips)
        w:tr  (one per row)
          w:trPr  (cantSplit, trHeight, tblHeader, gridBefore/gridAfter)
          w:tc  (one per visual cell; a horizontally merged cell is ONE w:tc with w:gridSpan in its tcPr)
            w:tcPr  (tcW, gridSpan, vMerge, tcBorders, shd, tcMar, vAlign)
            block-level content: at least one w:p (paragraph); may end with w:tbl
              w:pPr (paragraph properties)
              w:r (run)
                w:rPr (run properties: bold, sz, color, fonts)
                w:t (the actual text); also w:tab, w:br, w:drawing
- id: docx-model-02
  answer: |
    Word splits visible text into runs arbitrarily — at spell/grammar-check boundaries, language changes, rsid revision boundaries, formatting changes, even mid-word. So "Widget C" can be stored as e.g. <w:t>Wid</w:t> + <w:t>get C</w:t>, or split by a w:tab, or spread across paragraphs. The literal string never exists in a single w:t, so run-level equality never matches.

    Reliable method: work one level up. Use each paragraph's .text (python-docx concatenates all w:t in the paragraph) or concatenate every w:t inside the w:tc to find the containing cell/paragraph, then splice across runs instead of replacing one run: locate the runs and offsets covering the match, set the first affected run's w:t to prefix + replacement + suffix, and delete the fully-covered runs (or trim them), after asserting the merged runs share compatible rPr/pPr so formatting is unchanged. Then read back and assert the new text appears exactly once and the old string appears nowhere in the package.
- id: docx-model-03
  answer: |
    WordprocessingML uses two different unit systems:
    - Table and cell widths: twips (dxa), twentieths of a point. w:w on w:tblW, w:gridCol, w:tcW, w:tblInd, w:trHeight.
    - Image and drawing sizes: EMUs (English Metric Units) in wp:extent/@cx,@cy and the a:ext inside a:xfrm.

    Conversions: 1 inch = 72 points = 1440 twips = 914400 EMU. 1 point = 20 twips = 12700 EMU. 1 cm = 567 twips; 1 inch = 2.54 cm. So twips->points divide by 20, twips->inches divide by 1440; EMU->inches divide by 914400, EMU->points divide by 12700.

    Critical gotcha: EMU attributes are xsd:long / integers. 1.5*914400 is the Python float 1371600.0; writing cx="1371600.0" is invalid OOXML and makes Word flag the file for repair. Always emit integers (round()). Make wp:extent and the inner a:ext agree.
- id: docx-locate-01
  answer: |
    Locate by content and structure, never by fixed index. Iterate doc.tables (note: body-level tables only — header/footer tables need section.header.tables), join each cell's text across all its paragraphs (runs are split, and cells may hold several paragraphs), and match on:
      - the header row's column labels (e.g. "Item", "Qty", "Unit Price", "Amount"),
      - then the row whose cell text contains "Gadget D".
    Assert exactly one table and exactly one row match; fail loudly otherwise rather than editing the first hit.

    Before editing: back up the file and record its hash; verify the row's cell layout (remember row.cells duplicates horizontally merged cells); confirm the Amount column sums to the printed Total; confirm the same numbers do not also appear in a second table, a header/footer, or plain paragraphs. Also check whether the document has Track Changes on (w:trackRevisions in settings.xml) before touching text.
- id: docx-locate-02
  answer: |
    python-docx's row.cells is grid-based, not cell-based: for a row whose first cell has w:gridSpan="2", row.cells returns the SAME _Cell object at both positions (the merged cell is repeated once per spanned grid column), so len(row.cells) equals the grid column count, not the number of w:tc elements.

    Consequences: deleting "the second cell" as row.cells[1] targets the same underlying w:tc as index 0 — you delete the merged cell once (or, if you also touched index 0, you operate on an already-detached element), not a second cell. Counting columns by iterating row.cells over-counts in merged rows and under-counts when gridBefore/gridAfter are used.

    Correct approach: iterate the raw XML, row._tr.tc_lst (or the lxml w:tc children), and read w:gridSpan / gridBefore / gridAfter from each cell's tcPr to know the real layout.
- id: docx-rowdel-01
  answer: |
    No. python-docx has neither table.delete_row() nor row.delete(). The only way is to remove the element from its parent:

        tbl = table._tbl
        row._tr.getparent().remove(row._tr)   # equivalently tbl.remove(row._tr)

    Doing it via lxml is exactly what the library exposes the underscore API for. After removal, verify the row is gone from document.xml (text no longer present) and that the table still has the expected row count and a consistent cell/grid structure.
- id: docx-rowdel-02
  answer: |
    The restart cell is the content owner of the vertical merge. If you simply remove that row, the following continuation cells (w:vMerge with no w:val, i.e. "continue") now have no restart above them — they render as empty cells, the merge is orphaned, and depending on the consumer the table may be flagged for repair; any content that belonged to the merged region is gone.

    Correct fix before deleting: promote the next continuation cell to be the new restart — take the first following continuation cell's w:vMerge and either remove it entirely or set it to w:val="restart" — then remove the original restart row. If no continuation rows remain, there is nothing left to merge; ensure no dangling w:vMerge remains anywhere in the table. Then verify: no w:vMerge continuation exists without a preceding restart in the same column.
- id: docx-rowdel-03
  answer: |
    Word fields cache their result in the document: the displayed Total is stored as field-result runs (w:fldChar begin / w:instrText " =SUM(ABOVE) " / separate / result runs / end). python-docx only removes the w:tr; it does not recalculate anything, so on opening the file Word shows the STALE cached total (the old, higher number) unless something forces a recalculation. Word does not automatically recalc fields on open by default (it updates on print or F9, or if the reader's settings say so).

    Handle it explicitly:
      1. Recompute the total from the surviving Amount cells yourself and write it into the field's cached result, formatted identically (same decimals, thousands separator, currency symbol).
      2. And/or set <w:updateFields w:val="true"/> in word/settings.xml so Word recalculates when the document is opened.
    Cross-check: assert the recomputed total equals the printed value before saving. If other derived numbers exist (tax, grand total, amount in words), fix those too.
- id: docx-rowdel-04
  answer: |
    It is concealment, not deletion. The w:tr, its w:tc and all w:t text remain fully in document.xml: the row still occupies the table grid, still participates in layout, still counts in SUM(ABOVE) (Word sums row values regardless of hidden text), is still searchable/copyable, is exposed to accessibility tools, prints if "print hidden text" is enabled, and can be restored by anyone with an unzip. It also breaks row-band alternation and pagination semantics.

    Proof of real deletion: the w:tr element is gone from the XML. Verify by unzipping and asserting (a) the table's w:tr count decreased by one, (b) the deleted row's identifying text appears nowhere in any part of the package (grep the unzipped bytes / search all XML parts), (c) the remaining rows' text order is as expected, and (d) the recomputed total matches — a live extractor (pandoc, textutil, or a fresh python-docx read) must not show the row.
- id: docx-cell-01
  answer: |
    cell.text = "SKU" in python-docx clears ALL block-level content of the cell and inserts one new paragraph with one plain run. What survives is only the cell-level properties in w:tcPr (shading/fill, borders, cell width tcW, vertical alignment, cell margins). What is LOST: every run property (bold, 9 pt size, white color, font) AND all paragraph properties (alignment, spacing, paragraph style), plus any tabs, line breaks, or additional paragraphs that were in the cell.

    To preserve formatting, edit in place rather than overwriting: find the existing paragraph and its runs, redistribute the new text across the runs you are replacing (set the first affected run's text to the new value and delete extra runs), after asserting the runs share compatible rPr and the paragraph has a single formatting. Then re-read and assert bold/size/color are still present in the XML.
- id: docx-cell-02
  answer: |
    A Qty change is never local. It propagates to: the Amount for that line (qty x unit price), any per-line discount/subtotal, the table Total (and its cached field result), any tax/VAT and grand-total lines, an amount-in-words line if present, other tables or summaries that reference the quantity, and headers/footers or body paragraphs that state the numbers.

    Make it consistent by deriving, not typing: recompute Amount = Qty x Unit Price for the edited line, then recompute every downstream total from the Amount column using the same arithmetic as the original document. Preserve the existing number formatting exactly (decimal places, thousands separators, currency symbol, right alignment). Update cached field results (or set updateFields). Before saving, assert the recomputed values are internally consistent — e.g. the sum of the Amount column equals the printed Total — so a wrong input table cannot be silently propagated. Finally grep the whole package for the old values to catch stragglers.
- id: docx-cell-03
  answer: |
    python-docx is entirely revision-unaware: it edits the XML it is told to edit and nothing else. Rewriting w:t while w:trackRevisions is set produces an UNTRACKED change — no w:ins/w:del wrappers are created, so the edit is indistinguishable from original content in the revision stream. Worse, if the runs you rewrite sit inside an existing w:ins or w:del (or the paragraph contains w:delText), removing them corrupts the revision structure, and Word may report the document as needing repair.

    Decide deliberately:
      - Accept a clean untracked edit and tell the user it will not appear in the markup (and that future edits will still be tracked).
      - Or emit proper tracked-change markup yourself: wrap removed content in <w:del><w:r><w:delText> and added content in <w:ins><w:r><w:t>, each with unique w:id, w:author, w:date.
      - Or temporarily remove w:trackRevisions from settings.xml for the operation, then restore it, again making the untracked nature explicit.
    Whichever you choose, parse around existing w:ins/w:del elements rather than deleting through them.
- id: docx-relayout-01
  answer: |
    table.add_row() APPENDS the row at the end of the table — after the Total row in your case. (Its new cells are empty/plain; it does not carry over sibling rows' run or cell formatting, and it adds no trPr. If you need certainty about a given version's behavior, inspect the produced XML.)

    Correct middle insertion: build the row by deep-copying an existing data row (e.g. the "Service G" row) so all tcPr, pPr, rPr, borders and banding come along verbatim, then reposition it with lxml:

        new_tr = copy.deepcopy(src_row._tr)
        total_row._tr.addprevious(new_tr)   # or serviceG_row._tr.addnext(new_tr)

    then clear the copied row's cell texts to the new values (set the first run's w:t and drop the surplus runs). A table must also not end the body without a trailing paragraph, and two tables must not be adjacent without an intervening paragraph.
- id: docx-relayout-02
  answer: |
    table.add_column(width) appends one new w:gridCol to w:tblGrid with that width and adds one empty w:tc to every existing row (width is a Length/EMU value in python-docx, not twips). The new cells are plain — no borders, shading or alignment — and every row gets the cell at the rightmost position, which is wrong for rows whose cells are merged (their w:gridSpan must be incremented or a covered cell inserted, otherwise cell count no longer matches the grid and Word repairs the file).

    To keep the table inside the page margins and looking native:
      - Take the new width out of an existing column so the sum of all w:gridCol widths stays exactly page width minus left and right margins (or the invoice table's own total width).
      - Update every existing cell's w:tcW in the shrunken column, and set the new column's w:tcW in every row; for gridSpan cells use the sum of the spanned gridCols.
      - Update the table's w:tblW to the new total and use w:tblLayout type="fixed" so Word honors the widths.
      - Copy a sibling cell's tcPr (borders, shading, vAlign, tcMar) into each new cell instead of hand-writing formatting.
- id: docx-relayout-03
  answer: |
    Because the grid is not the only source of truth. Two things commonly override it:
      - With the default w:tblLayout type="autofit", Word recalculates column widths from the content and effectively ignores your gridCol edits.
      - Each cell carries its own w:w in w:tcW; under fixed layout Word drives column widths from tcW + tblGrid, and the table's preferred width w:tblW may still total the old value.

    Update all of them consistently: set each w:gridCol/@w:w, set the w:tcW of every cell in that column (gridSpan cells: the sum of the spanned columns), set w:tblW to the new table width, and set w:tblLayout to fixed. Then assert sum(gridCol widths) equals w:tblW and equals page width minus margins.
- id: docx-tblins-01
  answer: |
    doc.add_table() appends the table at the END of the document body (as the last block element before w:sectPr). It does not insert at the cursor.

    To place it after the "Notes:" paragraph, create it and then MOVE the element — an lxml element has one parent, so addnext reparents it in place:

        tbl = doc.add_table(rows, cols)
        notes_par._p.addnext(tbl._tbl)

    Mind the block-level rules: two tables cannot be adjacent (a table immediately followed by another table is invalid/merged visually) so insert an empty w:p between them if needed, a table must not be the last body element without a trailing paragraph, and w:sectPr must remain the final child of w:body.
- id: docx-tblins-02
  answer: |
    Because that style is not defined in the document. python-docx resolves table styles by NAME against the styles present in the target document's styles.xml; "Grid Table 4 Accent 1" exists in Word's default template, not in the company template you were handed, so the lookup raises KeyError.

    To get a ruled table without that style:
      - List doc.styles and use a table style the document actually defines.
      - Or apply direct formatting: w:tblBorders (top/left/bottom/right/insideH/insideV, single, auto, sz=4 for 0.5 pt) in w:tblPr, with per-cell w:tcBorders if you need selective rules. This always renders, regardless of template.
      - Or import the style: copy the w:style (and its basedOn/link chain) definition from a template that contains it into this document's styles part, then apply it.
- id: docx-tblins-03
  answer: |
    Plainly: python-docx builds the table spanning the body text width — page width minus left and right margins — divided evenly into equal w:gridCol widths, with an autofit preferred width (w:tblW type auto). Because the layout is autofit, Word may render it narrower, to content, unless you pin it. It has no borders unless you set a style.

    I am not fully certain of the exact tblW value emitted by the library version you are using, so verify it by inspecting the generated XML rather than trusting a default.

    To match the existing invoice table, do not rely on defaults at all: set table.autofit = False, copy the invoice table's w:tblPr (borders, tblLayout fixed, jc, tblCellMar, tblLook), copy its w:tblGrid so every gridCol width matches, set each new cell's w:tcW to the corresponding column width, and copy one of its data rows as a formatting template for the new rows.
- id: docx-imgrep-01
  answer: |
    A picture is referenced indirectly. In document.xml the chain is:
      w:drawing > wp:inline (or wp:anchor, which carries position/anchor properties)
        > a:graphic > a:graphicData uri=".../picture" > pic:pic
        > pic:blipFill > a:blip r:embed="rIdN"
    rIdN is resolved in word/_rels/document.xml.rels to a Target like media/imageN.png — that part holds the actual bytes. Size/position is NOT in the image file: it lives in wp:extent cx/cy, wp:effectExtent, the a:xfrm/a:ext, and the anchor's position offsets.

    To replace ONE logo while keeping position and size: (a) add a NEW image part for the new file (in python-docx, image_part, rId = document.part.get_or_add_image(path)) and repoint only that blip: blip.set(qn('r:embed'), rId) — leaving wp:extent, a:ext, docPr and every anchor property untouched, or (b) if the aspect ratio differs, scale cx/cy and the inner a:ext proportionally in integer EMU. Never rebuild the drawing. Then confirm the old relationship/part is gone and the drawing still shows the same dimensions on read-back.
- id: docx-imgrep-02
  answer: |
    Because image parts are shared and de-duplicated by content: two drawings whose images have identical bytes reference the SAME media part (and the same rId). When you overwrite image_part._blob you mutate that one shared part, so every drawing that points at it — the page-3 picture included — changes.

    Fix: do not mutate the part. Add a new image part for the new bytes (get_or_add_image) and repoint only the target blip's r:embed to the new rId, leaving the original part and all other relationships intact. Before assuming two drawings are distinct, compare the SHA1 of their target parts.
- id: docx-imgrep-03
  answer: |
    doc.inline_shapes only enumerates shapes in the main document body, so a per-page logo in docx means it lives in a header or footer part (word/header1.xml, word/header2.xml, ... , word/footerN.xml), reached through each section's w:sectPr: w:headerReference / w:footerReference r:id, resolved in word/_rels/document.xml.rels to the header/footer part. There can be a default, first-page and even-page header, and several sections may share one part or each have their own.

    Find and replace by walking the parts, not the body API: iterate doc.sections, then each of section.header, section.first_page_header, section.even_page_header (and footers), and for each of those parts list its relationships / inline_shapes, identify the drawing by resolving its blip r:embed to the image part and matching the part's SHA1 (or the drawing's docPr name/descr), then either repoint r:embed to a newly added part or replace the bytes of the part it references. Handle every section that references it, and verify none of the old image references remain.
- id: docx-imgins-01
  answer: |
    doc.add_picture() appends a NEW PARAGRAPH containing the inline drawing at the END of the document body, after the last paragraph.

    Place it correctly by moving that paragraph after "Approved by:":

        run = doc.add_picture(path, width=Inches(1.5))   # aspect ratio preserved: height scaled automatically
        pic_par = run._r.getparent()                     # the w:p holding the drawing
        approved_par._p.addnext(pic_par)

    Passing width= alone scales height proportionally, which is what you want for a 1.5-inch-wide signature. Align it (e.g. right) via pic_par.paragraph_format.alignment. Notes: wp:extent and the inner a:ext must stay in agreement as integers in EMU; check the source PNG's true pixel dimensions (a built-in border means ink is smaller than the canvas, so a pixel measurement will not match the nominal inches).
- id: docx-imgins-02
 answer: |
    Word does not shrink it. The drawing keeps its wp:extent/a:ext, so a 3-inch image in a 1.2-inch cell spills out across the neighbouring cell(s) and outside the table edge, overlapping text and breaking layout/pagination; the cell height grows to fit the image but the column width does not. It looks fine in a preview only if something else happens to be blank next to it.

    Size it to fit before inserting: measure the available width = cell tcW minus the cell's left and right margins (w:tcMar, default about 0.08 inch each) and, where the cell has a usable height budget, the available height. Scale = min(available_w / img_w, available_h / img_h). Insert with that computed width as a Length (e.g. width=Emu(...)), or set both wp:extent and a:ext consistently in integer EMU afterwards. Get the real pixel size from the image file (PIL or struct on the PNG header) rather than assuming.
- id: docx-legacy-01
  answer: |
    No. Advanced Warning: a .doc (Word 97-2003) is a binary OLE2/CFB container, not a ZIP of XML parts, so python-docx cannot open it — it raises an error (PackageNotFoundError / "not a docx") when the file fails the OPC package check.

    Workflow: convert first, edit second. Use a real converter (LibreOffice headless is the reliable one; Microsoft Word if available; textutil only as a last resort) to produce .docx, inspect the RESULT to confirm the table you must edit survived intact (row/cell counts, merges, widths, borders, fields), make the row deletion with python-docx on the .docx, verify, and convert back to .doc only if the user requires that format. Never edit the original .doc: keep it read-only and work on copies.
- id: docx-legacy-02
  answer: |
    textutil does not perform a Word-compatible conversion; it re-renders the document through macOS's text engine. For table editing the risks are real: column widths, tblGrid, cell margins, tcW, borders, shading, merged cells (gridSpan/vMerge), row heights, fonts, tracked changes, headers/footers, fields such as SUM, and even the number of rows/columns can all change or be dropped. A table edit script written against the converted geometry may be editing a table that no longer means the same thing.

    Detect it by comparing structurally before and after, not by trusting the exit code: convert, then assert table count, per-table row counts, per-row w:tc counts, gridSpan/vMerge occurrences, the sum of gridCol widths against page width minus margins, presence of tcW and borders on every cell, and equality of the SUM field. Diff the extracted text (e.g. pandoc -t plain) of the original and the conversion, and render both to images to compare visually. Any structural drift means the conversion is not trustworthy for the edit.
- id: docx-legacy-03
  answer: |
    Because LibreOffice is a single-instance application. The new soffice process tries to hand its arguments to the already-running desktop instance through its user-profile socket; that running instance does not run headless conversions, so the new process exits immediately and silently, having produced nothing. (Stale soffice.bin processes and a locked profile produce the same symptom.)

    Run it reliably from a script with an isolated profile and strict output checks:

        soffice --headless --norestore --convert-to docx --outdir /tmp/out \
                -env:UserInstallation=file:///tmp/lo_profile_unique \
                /path/to/file.doc

    Then verify the exit code AND that the output file exists, is non-empty, is newer than the input, and unzips cleanly. Use a fresh profile directory per invocation (or per script run) so concurrent conversions do not share a profile, and kill leftover soffice.bin processes first.
- id: docx-verify-01
  answer: |
    Safe save-and-verify procedure:
      1. Never edit the original. Copy invoice.docx to a unique scratch working copy, or keep the original read-only; record the original's sha256.
      2. Save the script's output to a NEW path (or .new), never in place on the first pass.
      3. Archive integrity: unzip -t the output; confirm the part list matches expectations.
      4. Byte-level minimality: assert every part other than word/document.xml (or the ones you intended to change) is byte-identical to the original; diff the changed part with difflib and confirm only the intended hunks changed.
      5. Independent read-back with a DIFFERENT parser than the one that wrote it: reopen with python-docx, parse document.xml with lxml, and also extract text with pandoc or textutil; assert the new content is present and the old content is absent.
      6. Structural assertions: row/cell counts, tcW vs gridCol sums, total table width equals text width, no gridSpan/vMerge inconsistency, every tc ends with a w:p, no adjacent tables, relationships and content types resolve, EMU attributes are integers.
      7. Render if a renderer is available and eyeball it; compare against a render of the original.
      8. Only after all checks pass, move the verified file over the destination and keep the backup until the user confirms.
- id: docx-verify-02
  answer: |
    Common structural mistakes in raw-XML table edits that make Word report "unreadable content" (note Word's repair dialog often hides the real cause, so prevent these structurally):

      1. Wrong element ORDER inside w:tcPr, w:tblPr, w:trPr — OOXML requires a fixed sequence (e.g. tcPr: tcW, gridSpan, vMerge, tcBorders, shd, tcMar, vAlign; tblPr: tblStyle, tblW, jc, tblInd, tblBorders, tblLayout, tblCellMar, tblLook). Appending in the wrong order is the single most common cause.
      2. Cell count does not match the grid: a row whose cells (accounting for gridSpan, gridBefore/gridAfter) do not cover exactly the gridCols.
      3. Broken merges: a w:vMerge continuation with no restart above it, a gridSpan larger than the columns that remain, or an orphaned restart.
      4. A w:tc with no w:p at all, or a cell whose last block-level child is a w:tbl instead of a paragraph.
      5. Two adjacent w:tbl elements with no intervening w:p, or a table as the final body element with no trailing paragraph before w:sectPr.
      6. Invalid attribute types: floats where xsd:long/integer is required (e.g. extent cx="1371600.0"), negative or non-numeric dims.
      7. Broken relationships: r:embed/r:id pointing at a removed rId, a media part missing from [Content_Types].xml, duplicate rIds.
      8. Namespace damage: undeclared prefixes still listed in mc:Ignorable, or prefixes lost by re-serialising.
      9. Duplicate w:id values on revisions/bookmarks.

    Best defence: after every edit, reopen the file in an independent parser and assert the specific structural invariants above.
- id: docx-verify-03
  answer: |
    Treat "not visibly present" as insufficient — prove the bytes are absent from the whole package.

      1. Unzip the saved .docx and list every word/media/* part; record each part's SHA-1.
      2. Compare against the ORIGINAL file's parts: the old logo's hash must not be present anywhere in the output package.
      3. Check the relationship layer: no entry in word/_rels/document.xml.rels (or any header/footer .rels) still targets the old media part, and no r:embed / r:id attribute in any XML part still resolves to a removed target.
      4. Grep every XML part for the old rId string, and confirm the old image part count is exactly one less than before.
      5. Also inspect docProps/thumbnail.jpeg — Word's saved preview can still show the old logo even after the document is fixed (delete or regenerate it).
      6. Read back with an independent parser (fresh python-docx / lxml walk) and assert the drawing you inserted is the only image in the expected place, with the expected dimensions.
```

Correction — the placeholder entry above (`docx-imgins-03-placeholder`) does not exist in your set and must be ignored. The real final item is `docx-verify-03`; its answer is the placeholder row's text, repeated here cleanly:

