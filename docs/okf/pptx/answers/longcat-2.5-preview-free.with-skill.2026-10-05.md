- id: pptx-model-01
  answer: |
    A table is stored as a p:graphicFrame element in the slide XML. Inside it:
    - a:tbl is the table element
    - a:tblGrid holds a:gridCol elements (columns, each with a w attribute for width)
    - a:tr elements are rows (each with an h attribute for height)
    - a:tc elements are cells
    - Each a:tc contains a:txBody → a:p (paragraphs) → a:r (runs) → a:t (text content)

- id: pptx-model-02
  answer: |
    EMU (English Metric Units). 914400 EMU = 1 inch; 12700 EMU = 1 point.
    A default 16:9 slide is 12192000 × 6858000 EMU (13.333 × 7.5 inches).

- id: pptx-model-03
  answer: |
    The logo is on the slide layout (or slide master), not on individual slides. That is why slide.shapes does not list it. Editing it there changes every slide that uses that layout/master — a global change, not a per-slide one.

- id: pptx-locate-01
  answer: |
    Iterate slide 3's shapes, find the one with has_table == True (recursing into GroupShape if needed). Match the table whose first-row cell texts match the expected header. Then iterate table.rows and match on cell.text_frame.text.strip() == "Gadget D", asserting exactly one hit.

- id: pptx-locate-02
  answer: |
    The merge is represented by gridSpan="2" on the origin a:tc; the covered cell has hMerge="1" and stays in the XML. When editing, write only to the origin cell. When deleting rows, be careful not to break the merge structure — the covered cell must remain and the origin's gridSpan must stay correct.

- id: pptx-rowdel-01
  answer: |
    No, table.rows is read-only and there is no delete API. You must remove the XML element directly: tbl = table._tbl; tbl.remove(tbl.tr_lst[i]).

- id: pptx-rowdel-02
  answer: |
    Yes, it matters — the frame height does not follow XML edits. Fix it by setting gf.height = sum(r.height for r in gf.table.rows) after the removal.

- id: pptx-rowdel-03
  answer: |
    If banding is done via explicit a:tcPr/a:solidFill on each cell, deleting a row does not re-alternate — two adjacent rows end up with the same fill. If banding is done via the table style's bandRow property, it re-alternates automatically. Decks with explicit fills break; decks using style banding do not.

- id: pptx-rowdel-04
  answer: |
    The text is still in the slide XML — searchable, accessible, extractable. A white cover shape or zero-height row leaves the content in the file. It is not truly removed; it is merely hidden from view.

- id: pptx-cell-01
  answer: |
    cell.text = "SKU" replaces the entire text frame content, dropping all run formatting (bold, 14 pt, white). To keep formatting, set the first run's text instead: cell.text_frame.paragraphs[0].runs[0].text = "SKU", then remove any extra runs.

- id: pptx-cell-02
  answer: |
    The row grows taller, potentially pushing the table beyond the slide bottom. Control it by widening the column, shrinking the font size, or enabling word wrap. After the change, check the frame bottom against the slide height.

- id: pptx-cell-03
  answer: |
    Update the row's Amount (Qty × Unit Price), the Total row, and every other place the data repeats — summary slide, chart data, speaker notes.

- id: pptx-relayout-01
  answer: |
    Deep-copy an existing body a:tr, insert it with ref_tr.addnext(new_tr), set text through the first existing run, update the total, set the frame height to the row-height sum, and re-apply explicit banding on every row after the inserted one.

- id: pptx-relayout-02
  answer: |
    Add a new a:gridCol to a:tblGrid, add one a:tc to every a:tr (copying neighbour cell formatting, minding merged spans), shrink existing column widths so the sum fits the frame/slide, and set the frame width to the new sum.

- id: pptx-tblins-01
  answer: |
    Read slide size from prs.slide_width/height. Find empty space by checking existing shapes' bounding boxes. Create with slide.shapes.add_table(rows, cols, left, top, width, height) at a position that does not overlap.

- id: pptx-tblins-02
  answer: |
    Apply the same table style ID (set tbl.tblPr with the matching tableStyleId GUID), or copy formatting from an existing table — cell fills, fonts, borders — to match the deck's look.

- id: pptx-tblins-03
  answer: |
    Get the placeholder's position and size, add the table at that bbox with add_table, then remove the empty placeholder element.

- id: pptx-imgrep-01
  answer: |
    Use slide.shapes.add_picture(path, left, top, width, height) at the old geometry, then old_pic._element.addnext(new_pic._element) to maintain stacking order, then remove the old p:pic element. Alternatively, repoint the a:blip r:embed to a new image part.

- id: pptx-imgrep-02
  answer: |
    Image parts are shared across pictures and slides. The same image part (e.g., ppt/media/image1.png) is referenced by multiple pictures via relationships. Overwriting the part's blob changes every picture that references it — slide 1 and slide 7 alike.

- id: pptx-imgrep-03
  answer: |
    drop_rel only counts r:id references, not a:blip r:embed. An image rel is always dropped even when another picture still uses it via r:embed. Before calling drop_rel, check //@r:embed and //@r:link for the rId to confirm nothing else references it.

- id: pptx-imgins-01
  answer: |
    Read slide size. Compute left = slide_width - 2*914400 - margin, top = slide_height - img_height - margin. Get the image's pixel dimensions to compute its aspect ratio, then call slide.shapes.add_picture(path, left, top, width=2*914400) — python-pptx auto-computes height from aspect ratio when only width is given.

- id: pptx-imgins-02
  answer: |
    The placeholder has fixed dimensions. insert_picture stretches the image to fill the placeholder's aspect ratio, cropping or distorting a portrait image. The image is fitted to the placeholder, not to its own native aspect ratio.

- id: pptx-legacy-01
  answer: |
    No, python-pptx only opens .pptx (OOXML). Convert first: soffice --headless --convert-to pptx deck.ppt (LibreOffice) or use PowerPoint's Save As. Then edit the resulting .pptx.

- id: pptx-legacy-02
  answer: |
    Before editing: verify the conversion preserved tables, images, and formatting. After editing: verify the file opens correctly and content is intact. Tell the user the original .ppt is untouched; the .pptx is a converted copy and some formatting may have shifted.

- id: pptx-verify-01
  answer: |
    Save to a NEW file (keep the original untouched). Re-open and assert exact cell texts, totals, and frame geometry. Compare other slides' XML/text with the original. Search every slide part for text that should be gone. Render if possible.

- id: pptx-verify-02
  answer: |
    Common causes: a:tc count differing from a:gridCol count; an a:tc without a:txBody (or txBody without a:p); broken gridSpan/hMerge; elements out of schema order; duplicate p:cNvPr ids after copying shapes; dangling r:embed rIds.

- id: pptx-verify-03
  answer: |
    Not necessarily. The image part (ppt/media/imageN.*) and its relationship may still be in the zip. Check by listing the saved zip's media directory and hash-comparing against the old bytes. Also verify the rel is gone from the slide's rels part.
