- id: pptx-model-01
  answer: |
    A table is stored as a p:graphicFrame element containing an a:tbl element. Columns are defined by a:gridCol elements inside a:tblGrid. Rows are a:tr elements. Cells are a:tc elements within each row. Text lives in a:txBody inside each cell, structured as a:p (paragraph) > a:r (run) > a:t (text).

- id: pptx-model-02
  answer: |
    python-pptx and DrawingML use EMUs (English Metric Units). 1 inch = 914,400 EMU; 1 point = 12,700 EMU. A default 16:9 slide is 12,192,000 × 6,858,000 EMU (13.333 × 7.5 inches).

- id: pptx-model-03
  answer: |
    The logo is on the slide layout (or slide master), not on the slide itself. slide.shapes only iterates shapes placed directly on that slide. Editing it on the layout/master changes every slide that uses that layout. You must access it via slide.slide_layout.shapes or the master.

- id: pptx-locate-01
  answer: |
    Access slide 3 via prs.slides[2]. Iterate slide.shapes to find the shape with shape.has_table == True. Then iterate table.rows and check each row's cells' text (e.g., row.cells[0].text == "Gadget D").

- id: pptx-locate-02
  answer: |
    A merge is represented by gridSpan="2" on the first cell's a:tc element (and the second cell either omitted or marked with hMerge="1"). When editing, you must identify the merge origin (the cell with gridSpan) and not treat the covered cell as independent. Deleting or inserting cells around it requires adjusting gridSpan values.

- id: pptx-rowdel-01
  answer: |
    There is no table.rows.remove() or delete API in python-pptx. You must manipulate the XML directly: get the a:tr element via row._tr, then remove it from its parent a:tbl using lxml (e.g., tbl.remove(tr) or tr.getparent().remove(tr)).

- id: pptx-rowdel-02
  answer: |
    The graphicFrame's ext cy does not auto-adjust. It usually does not matter because PowerPoint recalculates row heights on open, but for consistency you should manually reduce the graphicFrame height by the removed row's height, or leave it—PowerPoint will handle it.

- id: pptx-rowdel-03
  answer: |
    If shading is applied as an explicit fill on each row (based on position parity), deleting a middle row shifts all subsequent rows' positions, causing two adjacent rows to share the same colour. If shading comes from a table style (banding), PowerPoint recalculates and the problem does not occur. It depends on whether the deck uses explicit per-row fills or table-style banding.

- id: pptx-rowdel-04
  answer: |
    The row still exists in the XML data model. It still exports to other formats, still appears in accessibility trees, and its data remains in the file. A white rectangle is an overlay shape that can shift or obscure other content. Setting text to white and height to near-zero leaves a phantom row that confuses data extraction and future edits.

- id: pptx-cell-01
  answer: |
    cell.text = "SKU" replaces all cell content with a single paragraph and single run, discarding the original run properties (font, size, bold, colour). To keep formatting, modify the existing run's text instead: cell.text_frame.paragraphs[0].runs[0].text = "SKU".

- id: pptx-cell-02
  answer: |
    The longer text wraps within the cell, which increases the row height and may push the table beyond the slide edge. To control it: reduce font size, widen the column (adjust a:gridCol w value), enable word wrap, or set cell margins. You may also need to shrink other columns to compensate.

- id: pptx-cell-03
  answer: |
    You must update any totals or subtotals that sum the Qty column, any calculated fields (Amount = Qty × Unit Price for that row), and any text elsewhere in the deck that references the quantity or total.

- id: pptx-relayout-01
  answer: |
    Deep-copy an existing a:tr element (e.g., the Service G row) using copy.deepcopy(), insert it into the a:tbl at the correct position (before the Total row) via lxml addnext/addprevious, then set the new row's cell texts. The copy carries all formatting (fills, fonts, borders) from the source row.

- id: pptx-relayout-02
  answer: |
    XML changes: (1) add a new a:gridCol to a:tblGrid, (2) add a new a:tc to every a:tr (deep-copy an existing cell from the same column position), (3) adjust existing column widths to free space for the new column so the total table width stays within the slide. Keep sum(gridCol w values) ≤ graphicFrame width.

- id: pptx-tblins-01
  answer: |
    Use slide.shapes.add_table(rows=3, cols=2, left, top, width, height). Pick left/top by inspecting existing shapes' positions (shape.left, shape.top, shape.width, shape.height) to find empty space. Convert Inches/Emu as needed.

- id: pptx-tblins-02
  answer: |
    add_table applies a default table style (usually a blue accent). To match existing tables: (1) set the table's style ID to match (tbl.tblPr with a:tableStyleId), or (2) manually apply formatting—cell fills, fonts, borders—to match the deck's existing tables.

- id: pptx-tblins-03
  answer: |
    Use placeholder.insert_table(rows, cols) if the placeholder supports it (idx must be set). Alternatively, read the placeholder's position and size (left, top, width, height) and pass them to slide.shapes.add_table().

- id: pptx-imgrep-01
  answer: |
    Get the old picture's position (pic.left, pic.top) and size (pic.width, pic.height). Remove the old p:pic element from the slide's spTree. Add the new image with slide.shapes.add_picture(path, left, top, width, height) using the same coordinates. Stacking order is determined by element order in spTree; insert the new pic at the same position in the XML if order matters.

- id: pptx-imgrep-02
  answer: |
    Both slides reference the same image part (same rId relationship target). Overwriting the part's bytes changes it everywhere. To have different logos, each slide must reference a distinct image part (different rId pointing to a different part).

- id: pptx-imgrep-03
  answer: |
    python-pptx's drop_rel removes the relationship but does not check whether the same part is referenced by other relationships (other pictures). The part is removed from the package, breaking all pictures that shared it. You must check for other references to the same part before dropping.

- id: pptx-imgins-01
  answer: |
    Calculate left = slide_width - Inches(2) - margin, top = slide_height - calculated_height - margin. Use slide.shapes.add_picture(path, left, top, width=Inches(2)). python-pptx auto-calculates height from the image's aspect ratio when height is omitted. Check existing shape bboxes to avoid overlap.

- id: pptx-imgins-02
  answer: |
    The placeholder has a fixed size. insert_picture fits the image within the placeholder's bounds, cropping overflow. To avoid cropping: resize the image to match the placeholder's aspect ratio before inserting, or insert without the placeholder and manually size/position.

- id: pptx-legacy-01
  answer: |
    No, python-pptx cannot open .ppt (PowerPoint 97–2003 binary format). Workflow: convert to .pptx first using LibreOffice (soffice --headless --convert-to pptx) or PowerPoint (Save As), then edit with python-pptx.

- id: pptx-legacy-02
  answer: |
    Before: back up the original .ppt. After conversion: check that tables, images, fonts, and shapes survived; verify formatting fidelity. Tell the user that conversion may lose some features (certain animations, embedded objects, or precise positioning) and that the .pptx is a new file—the original .ppt is unchanged.

- id: pptx-verify-01
  answer: |
    Save to a temporary file first. Reopen with python-pptx and verify key properties (slide count, shape count, table contents, image presence). Optionally render to images (e.g., via LibreOffice or aspose-slides) for visual confirmation. Only replace the original after verification passes.

- id: pptx-verify-02
  answer: |
    Common causes: malformed XML (unclosed tags, invalid characters), missing required elements or attributes, incorrect namespace declarations, broken relationship IDs (rId pointing to non-existent parts), invalid attribute values (e.g., non-integer EMU values), and schema violations in the OOXML structure.

- id: pptx-verify-03
  answer: |
    Not necessarily. The p:pic element is removed from the slide, but the image part (in ppt/media/) may still exist in the package if other slides reference it or if the relationship wasn't dropped. Check: (1) unzip the .pptx and look in ppt/media/, (2) check slide rels for remaining references to the image part, (3) search the raw XML for the image filename.
