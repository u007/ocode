# nvidia/nemotron-3.5-super-vl — closed-book answers for pptx (openrouter, 2026-10-16); answerer saw only _prompts/pptx.md
- id: pptx-model-01
  answer: |
    As a p:graphicFrame on the slide's shape tree. Inside it, p:graphic/p:graphicData (table namespace) holds an a:tbl element. a:tbl contains a:tblPr (table-level properties such as firstRow/bandRow plus the a:tblStyleId style GUID), a:tblGrid with one a:gridCol per column (w = width in EMUs), then one a:tr per row (h = row height). Each a:tr holds a:tc cells; merges are rowSpan/colSpan attributes on a:tcPr, and fills/borders also live in a:tcPr. Cell text is in a:textBody -> a:p -> a:r -> a:t, with run formatting in a:rPr.
- id: pptx-model-02
  answer: |
    EMUs (English Metric Units): 914,400 per inch, 12,700 per point. python-pptx stores lengths internally as EMUs but its Inches/Pt/Cm/Emu helpers accept other units. A default 16:9 deck is 13.333 in x 7.5 in = 12,192,000 x 6,858,000 EMU (960 x 540 pt).
- id: pptx-model-03
  answer: |
    It lives on the slide master or the slide layout, not on the slide itself, so inherited master/layout shapes do not appear in slide.shapes. Edit it on the master/layout: the change propagates to every slide using that master or layout (unless a slide overrides or hides it), you cannot select it on a slide, and deleting it removes it from all inheriting slides.
- id: pptx-locate-01
  answer: |
    Zero-based: slide = prs.slides[2]. Loop slide.shapes; where shape.has_table is True, table = shape.table is the table (check all shapes if there may be more than one). Then scan table.rows / table.cell(r, c), comparing cell.text to "Gadget D", to get the row index, and use table.rows[i] / table.cell(i, c) afterward. Allow for an offset if the first row is a header.
- id: pptx-locate-02
  answer: |
    A merge is the anchor cell's a:tcPr carrying rowSpan and/or colSpan; the covered grid positions have no separate a:tc, and the anchor's text fills the merged area. Watch that spans stay inside a:tblGrid, never delete a row or column that cuts through a span (dangling span corrupts the table), remember text exists only in the anchor cell, python-pptx cell(r,c) resolves spans to the anchor, and a merged cell's width/height equals the sum of the spanned columns/rows.
- id: pptx-rowdel-01
  answer: |
    No. python-pptx has no table.rows.remove() or row delete API; rows are positional. Delete the a:tr element directly: tr = table.rows[i]._tr; tr.getparent().remove(tr) (equivalently table._tbl.remove(tr)). Renumbering is then automatic, but you must fix row heights/total height and banding/fills yourself, since python-pptx does none of that.
- id: pptx-rowdel-02
  answer: |
    It usually still renders, but the graphicFrame (p:xfrm ext cy) is now larger than the table grid (sum of remaining row heights), leaving blank space inside the frame. Fix by reducing the frame's cy by the removed row's height (adjust off y if the table is bottom-anchored), or add the freed height to remaining rows via their a:tr h attributes so the total height is unchanged.
- id: pptx-rowdel-03
  answer: |
    Deck-dependent cause: shading from table-style banding (bandRow plus a table style) is recomputed by the renderer and survives deletion, while hard-coded per-cell fills (a:solidFill in a:tcPr) stay exactly where they were, so deleting a middle row can leave two adjacent rows with identical color. One deck uses style banding, the other uses explicit fills.
- id: pptx-rowdel-04
  answer: |
    It is only visual hiding: the row and its text remain in the XML, so they are still searchable, selectable, copy-pasteable, screen-reader-visible, and recoverable by removing the rectangle or restoring height; near-zero height can still perturb table layout, and confidential data stays in the file. The row must be removed by deleting its a:tr, not painted over.
- id: pptx-cell-01
  answer: |
    cell.text = "SKU" wipes the cell: all paragraphs and runs are removed and replaced by one default run, so the bold 14 pt white formatting is lost. To keep it, edit the existing run in place: cell.text_frame.paragraphs[0].runs[0].text = "SKU" (that preserves the run's font), or create a new run copying the old run's rPr before replacing the text.
- id: pptx-cell-02
  answer: |
    The cell wraps and PowerPoint auto-grows the row height to fit, so the table grows taller and can run off the slide bottom or overlap objects below; column widths stay fixed and PowerPoint will not shrink the font. Control it by setting column widths, a fixed or minimum row height, word wrap, cell margins, font size, and vertical anchor, and check that top + height stays within the slide after the edit.
- id: pptx-cell-03
  answer: |
    Any calculated dependents: that row's line total (Qty x unit price), column subtotals, the Total row or grand total, and any chart or summary text built from those figures. Update them so the deck stays consistent, and check notes or other slides quoting the old quantity.
- id: pptx-relayout-01
  answer: |
    python-pptx has no rows.insert, so do it in XML: find the row index of "Service G", deep-copy its a:tr (inherits height and fill formatting), and insert the clone immediately after it, before the Total row. Then clear the new cells' text while preserving run/tcPr formatting (set existing run text to "" or rewrite runs keeping rPr) and type the new values. Cloning the row above or below keeps banding consistent.
- id: pptx-relayout-02
  answer: |
    Add one a:gridCol to a:tblGrid with the new column's width, and insert a new a:tc into every a:tr (header plus data rows) at the position after Item. Because the table already spans nearly the full slide, subtract the new width from existing columns (narrow Item or distribute the reduction) so the grid still fits; update any colSpan on merges crossing the insertion point; keep the graphicFrame's width/left so its right edge stays within the slide, moving or shrinking the frame if necessary. Keep gridCols consistent with the cells per row.
- id: pptx-tblins-01
  answer: |
    slide.shapes.add_table(3, 2, left, top, width, height). Pick left/top from prs.slide_width/slide_height minus margins, then verify the rectangle (left, top, width, height) does not intersect any existing shape's bounding box (shape.left/top/width/height) and stays on the slide; choose a gap such as below the lowest existing content. Then set column widths/row heights and fill the cells.
- id: pptx-tblins-02
  answer: |
    Copy the a:tblStyleId GUID from a:tblPr of an existing deck table and write the same GUID into the new table's a:tblPr, also matching firstRow/bandRow flags. python-pptx does not expose the table style id, so edit the XML directly. PowerPoint then renders the new table with the deck's table style; an unknown GUID falls back to the default blue style.
- id: pptx-tblins-03
  answer: |
    Use the placeholder, not add_table: ph = slide.placeholders[idx] for the empty content/object placeholder, then graphic_frame = ph.insert_table(rows, cols). The placeholder positions and sizes the table per the layout, which is the idiomatic approach and keeps it aligned with the deck's design.
- id: pptx-imgrep-01
  answer: |
    Record the old pic's left/top/width/height, flip, rotation, its index among the p:spTree children (z-order), name, and any hyperlink. Add the replacement with slide.shapes.add_picture(path, left, top, width, height) at the same rect; to keep stacking order, insert the new p:pic element at the old element's index in the tree, then remove the old element (add_picture appends, which would change z-order). Re-attach click/hyperlink if needed. The old image part becomes unreferenced; drop its relationship/media part separately if you want it out of the file.
- id: pptx-imgrep-02
  answer: |
    The same image part is shared: both slides' pictures point at the same image via their blip r:embed (same part, possibly different rels or the same rel). Overwriting that part's bytes changes every picture that references it, so slide 7's logo changes too. Fix by adding a new image part/relationship for slide 7 and re-pointing its blip to the new r:id instead of overwriting shared bytes.
- id: pptx-imgrep-03
  answer: |
    Both pictures referenced the same relationship id on the slide part; drop_rel(rId) removed that relationship entirely, so the surviving picture's a:blip r:embed now dangles and shows as broken. Relationships are shared, not per-picture. Drop a rel only when nothing references it; otherwise re-point the remaining picture to a new or existing rel first.
- id: pptx-imgins-01
  answer: |
    Get the image aspect ratio (PIL or pptx.image.Image): height = 2 in x (img_height / img_width). Place at left = slide_width - 2 in - right_margin, top = slide_height - height - bottom_margin, then verify the rect is inside the slide and does not intersect existing shape bounding boxes; nudge it if it overlaps. slide.shapes.add_picture(path, left, top, width=Inches(2)) derives height automatically, preserving aspect ratio.
- id: pptx-imgins-02
  answer: |
    placeholder.insert_picture scales the photo to fill the placeholder rectangle, so a portrait photo in a landscape placeholder is cropped (top/bottom cut off) rather than letterboxed; the missing part is still in the file, just not displayed. Fix by inserting with add_picture sized to fit inside the placeholder bounds (scale = min of width/height ratios, centered) or by adjusting picture.crop_* to reveal the whole photo.
- id: pptx-legacy-01
  answer: |
    No. python-pptx only reads/writes .pptx (OOXML); it cannot open a binary .ppt. Workflow: convert deck.ppt -> deck.pptx (LibreOffice headless: soffice --headless --convert-to pptx, or PowerPoint Save As), edit with python-pptx, save .pptx. Deliver .pptx, or convert back to .ppt with a warning about fidelity loss (formatting, tables, charts can change). Binary .ppt editing requires PowerPoint COM or LibreOffice UNO, not python-pptx.
- id: pptx-legacy-02
  answer: |
    Before: open the converted .pptx and diff against the .ppt — tables, text, fonts (substitution), merged cells, image positions, slide size (4:3 -> 16:9 resizing), and note lost features (animations, transitions, some charts/SmartArt). After editing: reopen, confirm edits render, the file opens without repair, and nothing shifted. Tell the user: the original .ppt is untouched; work was done on a converted copy; conversion may alter layout/formatting and drops some features; the deliverable is .pptx (convert back only expecting further changes); keep the original .ppt.
- id: pptx-verify-01
  answer: |
    Back up the original, then save to a new filename (never overwrite). Reopen with python-pptx to confirm readability; unzip the package and check parts/relationships ([Content_Types].xml, slide .rels, no dangling r:id, media present); extract text/tables and compare key content before vs. after; open in PowerPoint and/or LibreOffice to confirm no repair prompt and correct rendering; only then replace the original.
- id: pptx-verify-02
  answer: |
    Dangling or duplicate relationship IDs (r:id referenced but missing from .rels); XML child order violating the schema; missing required elements (a:tblPr, a:tblGrid, a:graphicData); rowSpan/colSpan exceeding the table grid or referencing removed rows/cols; unescaped & < > in text; media parts referenced but missing; corrupt [Content_Types].xml; a graphicFrame missing its graphicData/table. Anything that fails PowerPoint's strict OOXML validation triggers repair.
- id: pptx-verify-03
  answer: |
    No — only the slide reference is removed. The image part (ppt/media/*) and often its relationship usually remain as orphans, so the bytes are still in the .pptx. Check: rename to .zip, list ppt/media/, grep ppt/slides/_rels/slideN.xml.rels (and other .rels) for the image r:id, and confirm no other slide references the same image. To fully remove: drop the relationship and delete the media part.
