# cat-jam-free — WITH-SKILL closed-book answers for pptx (opencode-go, 2026-10-14)
# Delivery: derived/pptx.cat-jam-free.SKILL.md body (front matter stripped) prepended to _prompts/pptx.md; one
# `ocode run -m opencode-go/cat-jam-free -effort med` session, isolated HOME with every tool denied, cwd holding only the sheet;
# session audited in sqlite: 1 user + 1 assistant message, 0 tool calls.

- id: pptx-model-01
  answer: |
    A table is stored as a p:graphicFrame on the slide whose a:graphic contains an a:graphicData (table URI) containing an a:tbl. a:tblGrid holds the a:gridCol elements (column widths); a:tr holds rows; each a:tr contains one a:tc per grid column (cells); text lives in a:tc/a:txBody (a:p paragraphs, a:r runs, a:t text). The frame's p:xfrm gives position/size. Merged spans are represented by gridSpan/rowSpan on the origin cell and hMerge/vMerge on covered cells.
- id: pptx-model-02
  answer: |
    Positions and sizes are in EMUs (English Metric Units): 1 inch = 914400 EMU, 1 cm = 360000 EMU. A default 16:9 PowerPoint deck is usually 13.333 x 7.5 inches (12192000 x 6858000 EMU), but always read prs.slide_width / prs.slide_height rather than assuming.
- id: pptx-model-03
  answer: |
    It is on the slide master (or a slide layout), accessible via slide.slide_master.shapes / slide.slide_layout.shapes, not slide.shapes. Editing there changes every slide based on that master/layout, so the change is global and can affect placeholders; conversely slide-level automation that only scans slide.shapes will miss it.
- id: pptx-locate-01
  answer: |
    Open slides[2], recursively walk shapes (including GroupShape.shapes), keep shapes where shape.has_table, and choose the table by matching first-row cell text_frame.text.strip() to the expected header. For "Gadget D", iterate table.rows and collect rows where the key cell's stripped text exactly matches; assert exactly one hit before editing.
- id: pptx-locate-02
  answer: |
    A merge is an origin cell with gridSpan/rowSpan plus present but empty covered cells marked hMerge="1"/vMerge="1"; the row still has one a:tc per gridCol. When editing, write text only to the origin; when deleting/inserting rows or columns inside a merge, adjust gridSpan/rowSpan and the corresponding covered cells together; never address spanned cells as independent normal cells.
- id: pptx-rowdel-01
  answer: |
    python-pptx has no public table.rows.remove(). Remove the underlying a:tr via row._tr.getparent().remove(row._tr), then rebuild graphic-frame height. If the table has merges, adjust any affected gridSpan/rowSpan and covered cells too.
- id: pptx-rowdel-02
  answer: |
    Yes, the stale height matters: it can leave blank space or overlap shapes. Fix it by recomputing gf.height = sum(row.height for row in table.rows) (and update totals/frame geometry), then check slide bounds and shapes below.
- id: pptx-rowdel-03
  answer: |
    It happens when shading is applied as explicit a:tcPr solid fills on rows, which do not re-band after deletion. Decks where banding comes from the table style's bandRow flag may be recalculated by PowerPoint. Fix by re-applying alternating fills to rows after the deleted one, or clearing explicit fills and relying on bandRow.
- id: pptx-rowdel-04
  answer: |
    It leaves the confidential data in the XML (searchable, extractable, accessible), does not update totals or layout, can break across viewers/rendering, and defeats verification. It is cosmetic masking, not a data edit.
- id: pptx-cell-01
  answer: |
    cell.text = "SKU" replaces the text body content and loses the existing run formatting (bold, 14pt, white). Keep it by editing the existing run: cell.text_frame.paragraphs[0].runs[0].text = "SKU", preserving run properties; handle extra runs as needed.
- id: pptx-cell-02
  answer: |
    PowerPoint will wrap the longer text within the fixed column width and may auto-grow the row height, or clip if heights are fixed. Control layout by setting appropriate row height, font size, word wrap, column widths, and possibly table autofit behavior; ensure the frame still fits the slide.
- id: pptx-cell-03
  answer: |
    Also update dependent figures: in-table totals/subtotals, summary/agenda slides quoting the value, chart data (embedded workbook and chart XML cache), speaker notes, and any text mentions, using the same number format.
- id: pptx-relayout-01
  answer: |
    Deep-copy an existing a:tr (such as the Service G row), insert it before the Total row, fill each cell through the existing first run while removing extra runs, update totals and gf.height, then check gf.top + gf.height against slide height and shapes below.
- id: pptx-relayout-02
  answer: |
    Add an a:gridCol to a:tblGrid and add one a:tc (adjusted for merges: span counts and covered cells) to every row, then redistribute column widths so the table width fits; update gf width/height and verify gf.left + width <= slide_width with no overlap.
- id: pptx-tblins-01
  answer: |
    Inspect existing shape bounding boxes in EMUs to find unused space, then call slide.shapes.add_table(rows, cols, left, top, width, height) with EMU values, set column widths/row heights, and verify no overlap before saving.
- id: pptx-tblins-02
  answer: |
    add_table defaults to Medium Style 2 - Accent 1 with firstRow and bandRow. To match, copy the existing table's a:tblPr (style id plus flags), explicit tcPr fills, run properties, column widths, and row heights.
- id: pptx-tblins-03
  answer: |
    If it is a TablePlaceholder, use ph.insert_table(rows, cols). If it is a generic content placeholder, call slide.shapes.add_table(rows, cols, ph.left, ph.top, ph.width, ph.height), then remove ph._element from its parent so no empty placeholder remains.
- id: pptx-imgrep-01
  answer: |
    Record the old picture's position/size, add the new picture, set its geometry, insert the new p:pic immediately before the old one in the spTree to preserve stacking order, then remove the old p:pic. Fit the new image by one dimension or inside the old box to avoid stretching. Drop the old image rel only if no other //@r:embed/@r:link uses it.
- id: pptx-imgrep-02
  answer: |
    Multiple pictures (possibly across slides) share the same image part/relationship target, so overwriting that part's bytes changes every referencing picture. Replace by creating a new image part/relationship instead of mutating shared bytes.
- id: pptx-imgrep-03
  answer: |
    drop_rel ref-counts only //@r:id, but pictures reference images via a:blip r:embed, so an image rel always appears unused and is dropped even when still referenced. You must manually check //@r:embed | //@r:link before calling drop_rel.
- id: pptx-imgins-01
  answer: |
    Set width to 2 inches (1828800 EMU), compute height from the source aspect ratio, place at left = slide_width - width - margin and top = slide_height - height - margin, call add_picture with width only to preserve aspect, then verify no overlap with existing shapes.
- id: pptx-imgins-02
  answer: |
    insert_picture scales/crops the image to the placeholder's fixed dimensions/aspect ratio, so a mismatch causes part of the photo to be cropped rather than letterboxed. Use a better-fitting placeholder or adjust dimensions/crop behavior.
- id: pptx-legacy-01
  answer: |
    No, python-pptx cannot open binary .ppt. Convert first with soffice --headless --convert-to pptx, inspect tables/images/fonts, edit the pptx, and if the user needs .ppt, convert back with soffice --headless --convert-to ppt, keeping the untouched original.
- id: pptx-legacy-02
  answer: |
    Before editing, check converted tables, images, fonts, sizes, and text integrity. After editing, verify against the original and deliver the requested format. Tell the user the file went through a conversion that may not be lossless and that the original was kept.
- id: pptx-verify-01
  answer: |
    Save to a new file, reopen it, assert the exact changed cell texts, their order, totals, and frame geometry. Compare every other slide and shape against the original, search all parts for text that should be gone, and inspect ppt/media (hash-compare if an image was removed). Do not overwrite the original until verified.
- id: pptx-verify-02
  answer: |
    Common causes include a row's a:tc count differing from a:gridCol count, missing covered cells in merges, wrong gridSpan/rowSpan, malformed XML or namespaces, invalid relationship targets, and inconsistent table style/table property flags.
- id: pptx-verify-03
  answer: |
    No, deleting p:pic does not remove the image part or relationship; ppt/media still contains it. Check the slide XML for r:embed usage, part relationships, and the zip's ppt/media contents. To truly remove it, drop the unused rel and remove the media part.
