# PowerPoint Editing Benchmark — Human Render

> **Generated from `questions.yaml` (corpus_rev 1). Do not grade from this file
> — `questions.yaml` is the source of truth.** If they disagree, the YAML wins.

Legend: **W** = weight (1–3), **D** = difficulty. Rubric shows scored points
(`•`) and partial-credit levels (`~`).

---

### pptx-model-01 · pptx-model · W3 · easy
**Q:** How is a table stored in a .pptx slide, and which XML elements hold its columns, rows, cells and text?
**A:** A .pptx is a ZIP of XML parts: each slide is ppt/slides/slideN.xml, linked to a layout and master. A table is a p:graphicFrame (position and size in p:xfrm) whose a:graphic/a:graphicData holds an a:tbl. The a:tbl contains a:tblPr (style id and banding flags), then a:tblGrid with one a:gridCol w="…" per column, then one a:tr h="…" per row. Each row has one a:tc per grid column, and each a:tc holds an a:txBody (a:p/a:r/a:t) and an a:tcPr. A table is not a p:sp shape.
• graphicFrame (xfrm) → a:graphic/graphicData → a:tbl • a:tblGrid/a:gridCol w; a:tr h; a:tc with a:txBody (+ a:tcPr)

### pptx-model-02 · pptx-model · W2 · medium
**Q:** What units does python-pptx / DrawingML use for positions and sizes, and what are the slide's dimensions in a default 16:9 deck?
**A:** EMU (English Metric Units): 914400 per inch, 12700 per point, 360000 per cm. Shape left, top, width and height, the gridCol widths and the row heights are all in EMU. The slide size is prs.slide_width and prs.slide_height. A 16:9 deck is typically 12192000 × 6858000 EMU (13.333 × 7.5 in). Always read the size from the presentation instead of assuming it.
• EMU: 914400/inch, 12700/pt • read prs.slide_width/height (16:9 ≈ 13.333×7.5 in), don't assume

### pptx-model-03 · pptx-model, image-replace · W2 · medium
**Q:** A company logo appears on every slide, but iterating `slide.shapes` on any slide does not find it. Where is it, and what are the consequences of editing it there?
**A:** It lives on the slide LAYOUT or the slide MASTER (slide.slide_layout.shapes, or prs.slide_master.shapes / slide_layouts), which slides inherit. Editing it there changes every slide that uses that layout or master. If only one slide should change, override it on that slide, and tell the user the scope. Slide-level edits do not touch it.
• inherited from layout/master, not on the slide • editing layout/master affects all slides using it; scope decision

### pptx-locate-01 · table-locate · W3 · medium
**Q:** How do you find the table on slide 3 and the row for "Gadget D" with python-pptx?
**A:** Iterate slide.shapes and select shapes with has_table true: these are graphic frames, and shape.table gives the table. Group shapes need recursion. Pick the table by its header text, not by shape index, then find the row whose first cell's text_frame.text, stripped, equals "Gadget D". Assert exactly one match. Rows are table.rows or the a:tr elements, and cells come from table.cell(r, c).
• shape.has_table → shape.table (recurse into groups) • identify by header/cell text and assert a unique match

### pptx-locate-02 · table-locate, pptx-model · W2 · hard
**Q:** In a pptx table the Total label spans the first two columns. How is a merge represented, and what must you watch when editing or deleting cells around it?
**A:** Every row still has one a:tc per grid column. The merge origin carries gridSpan="2" (rowSpan for vertical merges), and the covered cells carry hMerge="1" (or vMerge="1") and are hidden. python-pptx shows this as cell.is_merge_origin and cell.is_spanned. Write text only to the origin. When you delete or insert a column or row inside a merge, adjust gridSpan/rowSpan and the covered cells, or the table renders wrongly or is invalid.
• one a:tc per column; origin gridSpan/rowSpan, covered cells hMerge/vMerge • edit the origin; fix spans when inserting/deleting through a merge

### pptx-rowdel-01 · row-delete · W3 · easy
**Q:** How do you delete a row from a pptx table with python-pptx? Is there a table.rows.remove() or a delete API?
**A:** No. python-pptx has no API for deleting (or adding) rows or columns; table.rows only reads. Remove the XML element: `tbl = table._tbl; tbl.remove(tbl.tr_lst[i])`. The rows below move up automatically. Then fix the frame height and update any total.
• no delete API; remove the a:tr from table._tbl • then fix frame height and totals ~ invents rows.remove()/delete_row()

### pptx-rowdel-02 · row-delete, table-relayout · W3 · medium
**Q:** After removing an a:tr from a table, the graphicFrame's height (p:xfrm ext cy) is unchanged. Does that matter, and how do you fix it?
**A:** Yes. The frame height no longer equals the sum of the row heights, so the selection box and layout are stale, and some renderers stretch or offset rows. python-pptx updates the frame only when you set row.height or column.width through its API, not when you edit the XML. Set graphic_frame.height = sum(row.height for row in table.rows), or re-set one row's height to trigger the update.
• frame cy stays at the old total; not auto-updated on XML removal • set graphic frame height = sum of row heights

### pptx-rowdel-03 · row-delete, cell-edit · W2 · medium
**Q:** The table alternates row shading. After deleting a middle row, two adjacent rows have the same colour. Why might that happen in one deck and not another?
**A:** With a table style and horz_banding (a:tblPr bandRow="1"), banding is computed at render time and re-alternates by itself after a delete. If the shading is explicit per cell (a:tcPr/a:solidFill), each row keeps its colour, and deleting one breaks the alternation. In that case reassign the fills of the rows after the deleted one, or clear the explicit fills and rely on the style's banding.
• style banding (bandRow) recomputes; explicit tcPr fills do not • re-apply fills to the following rows (or switch to style banding)

### pptx-rowdel-04 · row-delete, verify-safety · W2 · medium
**Q:** Instead of deleting a row, someone drags a white rectangle over it, or sets the row's text to white and its height to near zero. Why is that wrong?
**A:** The row and its text are still in slideN.xml. They are extractable and searchable, and show up in exported PDFs, accessibility readers and search. PowerPoint also grows a row to fit its text, so a "zero-height" row does not stay invisible. Remove the a:tr, fix the frame height, and verify by re-opening and checking that the old text is absent from every slide XML part.
• text remains in the XML (extractable); rows grow to fit text • remove a:tr; verify absence in all slide parts

### pptx-cell-01 · cell-edit · W3 · easy
**Q:** A header cell has bold 14 pt white text. What happens to its formatting when you do `cell.text = "SKU"` in python-pptx, and how do you keep it?
**A:** cell.text replaces the text frame's contents with a new run that has no explicit run properties, so bold, size and colour set on the old run (a:rPr) are lost and the text falls back to the table style or defaults. Keep the formatting by setting the text of the first existing run (cell.text_frame.paragraphs[0].runs[0].text = "SKU") and removing any extra runs, or by copying the old a:rPr onto the new run.
• cell.text drops explicit run formatting • edit the first run's text / copy rPr ~ says cell.text keeps formatting

### pptx-cell-02 · cell-edit, table-relayout · W2 · medium
**Q:** You replace "Gadget D" with a much longer product name in a cell. What happens to the table's layout in PowerPoint, and how do you control it?
**A:** PowerPoint wraps the text and GROWS the row height to fit (a:tr h is a minimum). The table gets taller and can run off the bottom of the slide or overlap shapes below it. python-pptx does not compute that growth. Estimate the width the text needs, then widen the column (and shrink others to stay within the slide), use a smaller font in that cell, or shorten the text after asking. Afterwards, check the frame's bottom against the slide height and the shapes below.
• row grows to fit wrapped text (h is a minimum) → overflow/overlap • measure/adjust widths or font; check the bottom vs slide and neighbours

### pptx-cell-03 · cell-edit · W2 · medium
**Q:** You change one line item's Qty from 5 to 12. What else must you update in the deck?
**A:** Update the row's Amount and the Total (and any subtotal or tax). Also update every other place the total is repeated: a summary slide, a chart that uses the data (its embedded workbook and the cached values), and the speaker notes. Compute from the data, and keep the number format and alignment of each cell's runs.
• row amount + total recomputed • other occurrences (summary slide, chart data, notes); keep format

### pptx-relayout-01 · table-relayout · W3 · medium
**Q:** Add a row after "Service G" and before the Total row of a pptx table, keeping the formatting. How?
**A:** There is no add_row API. Deep-copy a suitable body a:tr (copy.deepcopy), insert it with ref_tr.addnext(new_tr) before the Total, and set each cell's text through its existing first run so the formatting is kept. Then update the Total, set the frame height to the sum of the row heights, and check that the frame's new bottom stays on the slide and does not overlap shapes below it (move them or ask). Keep any explicit banding fills alternating.
• deepcopy an a:tr and insert with addnext at the right position • fill via existing runs; update total + frame height • check the bottom vs slide/neighbour shapes (and banding)

### pptx-relayout-02 · table-relayout · W3 · hard
**Q:** Add a "SKU" column after the Item column in a pptx table that already spans nearly the whole slide width. What XML changes are required, and how do you keep the table on the slide?
**A:** Insert a new a:gridCol after the Item column's gridCol in a:tblGrid, and in EVERY a:tr insert a new a:tc after the Item cell. Deep-copy the neighbouring cell so the header keeps its style (fill, bold white text) and body cells keep theirs, then set the text. Watch gridSpan cells such as a merged Total. The frame then gets wider, so shrink the column widths until their sum is at most the available width (within the original frame, or slide_width minus the margins), and set the frame width to that sum. Columns can shrink until their widest text no longer fits, so check wrapping.
• new a:gridCol + one a:tc in every row at the right index (mind spans) • copy header/body cell formatting for the new cells • rebalance widths so the sum fits the slide/frame; update frame width

### pptx-tblins-01 · table-insert · W3 · medium
**Q:** Insert a new 3×2 table on an existing slide without overlapping existing content. How do you pick the position and create it with python-pptx?
**A:** Collect the bounding boxes (left, top, width, height) of all shapes on the slide. Include placeholders that hold content, and be aware of layout decorations. Find a free rectangle that is large enough inside the slide (prs.slide_width/height, minus the margins). Then call slide.shapes.add_table(rows, cols, left, top, width, height). It returns a GraphicFrame; use .table to fill the cells. If no space is free, ask, or propose a new slide or making room.
• compute free space from existing shape bboxes within slide size • shapes.add_table(rows, cols, left, top, width, height) → frame.table

### pptx-tblins-02 · table-insert · W2 · medium
**Q:** A table added with `add_table` looks different from the deck's existing tables (a blue default style). How do you make it match?
**A:** add_table applies python-pptx's default table style GUID (a:tableStyleId, Medium Style 2 – Accent 1) with the first-row and banding flags on. Copy the existing table's a:tblPr: its tableStyleId and its firstRow/bandRow flags. If the existing table uses explicit cell fills or fonts, copy those too (tcPr fills, run properties). Match the column widths and row heights.
• style comes from a:tableStyleId + tblPr flags (default = Medium Style 2) • copy the existing table's tblPr / explicit fills & run props

### pptx-tblins-03 · table-insert, pptx-model · W1 · medium
**Q:** The slide layout has an empty content placeholder where the table should go. What is the idiomatic way to put a table there?
**A:** Use the placeholder: a table placeholder has insert_table(rows, cols), which returns a PlaceholderGraphicFrame at the placeholder's position. A generic content placeholder may not offer it, since only a TablePlaceholder does. In that case add the table at the placeholder's left, top and width, then remove the empty placeholder element so no "Click to add text" box is left behind.
• TablePlaceholder.insert_table(rows, cols) uses the placeholder geometry • otherwise add at placeholder bbox and remove the empty placeholder

### pptx-imgrep-01 · image-replace · W3 · medium
**Q:** python-pptx has no picture.replace(). How do you replace one picture on a slide with a new image file at the same position and size, and keep its stacking order?
**A:** Add the new picture with slide.shapes.add_picture(path, left, top, width, height), using the old picture's geometry (or fitting the new aspect inside that box). add_picture appends the shape at the TOP of the z-order, so move its element to the old picture's position with old_el.addnext(new_el). Then remove the old p:pic element and drop its image relationship if nothing else uses that rId. An alternative is to add the new image part and repoint the old pic's a:blip r:embed. That keeps the crop (a:srcRect) and the size, but may distort an image with a different aspect ratio.
• add_picture with old geometry (aspect-aware) or repoint blip r:embed • restore z-order (addnext next to old element) and remove the old p:pic • drop the old image rel if unused

### pptx-imgrep-02 · image-replace · W2 · hard
**Q:** To replace a logo you overwrite the picture's image-part bytes. The logo on slide 1 changes, but so does the one on slide 7, which should have stayed. Why?
**A:** python-pptx deduplicates image parts by SHA1, and several pictures, on the same slide or other slides, can reference one image part. Overwriting the part's blob changes every picture that uses it. Replace only the intended picture by giving it a new part (add_picture, or repoint its r:embed to a newly added image part). Check first which shapes and parts reference the old image.
• image parts are shared across pictures/slides (SHA1 dedupe) • give the target picture its own new image part

### pptx-imgrep-03 · image-replace, verify-safety · W2 · hard
**Q:** After removing an old picture you call `slide.part.drop_rel(rId)` to get its image out of the file. Another picture on the same slide used the same image and is now broken. Why did python-pptx drop a relationship that was still in use?
**A:** drop_rel removes the relationship when its reference count is under 2, but the count only looks for r:id attributes in the part's XML. Pictures reference images with a:blip r:embed, so an image rel always counts as 0 and is always dropped. Before dropping it, check yourself that no other element in the part refers to the rId (xpath //@r:embed and //@r:link equal to rId).
• drop_rel's ref count only sees r:id, not a:blip r:embed → always drops image rels • check //@r:embed (and r:link) for other users before dropping ~ says shared images must not be dropped without explaining why drop_rel did it

### pptx-imgins-01 · image-insert · W3 · easy
**Q:** Insert signature.png 2 inches wide at the bottom-right of a slide, keeping its aspect ratio, without covering existing content. How?
**A:** Call slide.shapes.add_picture(path, left, top, width=Inches(2)). With only width given, the height follows the image's aspect ratio (give both and it is stretched). Compute left = right edge − width, taken from the slide width minus the margin, or from the right edge of the content it should align with. Compute top from the bottom limit minus the height. Then check the resulting box against every existing shape's bbox and move it if they overlap.
• add_picture with width only keeps aspect (both = stretch) • position from slide/content edges and check overlap with shape bboxes

### pptx-imgins-02 · image-insert · W2 · medium
**Q:** You insert a portrait photo into a picture placeholder with `placeholder.insert_picture(path)`. Part of the photo disappears. Why?
**A:** insert_picture fits the image to the placeholder's size and aspect by CROPPING (it sets the crop so the picture fills the placeholder). A portrait image in a landscape placeholder loses its top and bottom. To show the whole image, adjust crop_top, crop_bottom, crop_left and crop_right back to 0 and resize the shape to the image's aspect, or use add_picture with width or height only.
• insert_picture crops to fill the placeholder aspect • reset crop + resize, or use add_picture with one dimension

### pptx-legacy-01 · legacy-ppt · W3 · easy
**Q:** The user gives you deck.ppt (PowerPoint 97–2003) and asks you to change a table. Can python-pptx open it? What is the workflow?
**A:** No. .ppt is the binary OLE2 compound-file format, and python-pptx opens only OOXML ZIP packages. It raises PackageNotFoundError / not-a-zip, and renaming the file doesn't help. Convert with LibreOffice headless (soffice --headless --convert-to pptx --outdir out deck.ppt), edit the .pptx, and convert back to .ppt only if the user needs that format. Keep the original, and check the converted slides (tables, images, fonts) before editing.
• .ppt is binary OLE2; python-pptx can't open it • LibreOffice headless convert → pptx, edit, convert back if required

### pptx-legacy-02 · legacy-ppt, verify-safety · W2 · medium
**Q:** After converting a .ppt to .pptx with LibreOffice, what should you check before and after editing, and what must you tell the user?
**A:** Conversion is not lossless. Tables, fonts, animations, embedded OLE objects and exact positions can change. Before editing, compare the converted deck with the original: slide count, the tables with their rows and cells, the images, and ideally rendered previews. After editing, check again. Tell the user the file went through a conversion, and deliver the format they asked for (.ppt back if required), keeping the untouched original.
• conversion can alter content/layout; verify counts/tables/images (render) • tell the user; keep original; return requested format

### pptx-verify-01 · verify-safety · W3 · easy
**Q:** What is the safe save-and-verify procedure after editing deck.pptx with a script?
**A:** Keep the original untouched and save to a new file. Re-open the output with python-pptx. Assert the intended change: the exact table cell texts and their order, totals, and frame geometry within the slide. Assert that the other slides and shapes are unchanged, for example by comparing their XML or text with the original. Search every slide part for text that should be gone. Render the slide to an image (LibreOffice or Quick Look) for a visual check when possible.
• new output file; original untouched • re-open and assert exact content + unchanged remainder (render if possible)

### pptx-verify-02 · verify-safety, pptx-model · W2 · medium
**Q:** After XML edits to a table, PowerPoint says the file "needs to be repaired". Name common causes.
**A:** Common causes: - a row with a different number of a:tc than there are gridCols; - an a:tc without a:txBody (or a txBody without an a:p); - broken merge attributes (gridSpan/hMerge that don't add up); - elements out of schema order; - duplicate shape ids (p:cNvPr id) after copying shapes; - an r:embed pointing to a dropped relationship. python-pptx does not validate these.
• tc count vs gridCol mismatch / tc without txBody / broken spans • duplicate cNvPr ids, dangling rIds, schema order; no validation by python-pptx ~ only 'check the XML is well-formed'

### pptx-verify-03 · verify-safety, image-replace · W2 · medium
**Q:** You removed a confidential image from a slide by deleting its p:pic element. Is it gone from the .pptx? How do you check?
**A:** Not necessarily. The slide's relationship to the image part survives, and python-pptx saves every part reachable through rels, so ppt/media/imageN.* stays in the zip. The image may also be used by other slides, a layout, or notes. Drop the rel when nothing else in that part uses it (checking r:embed yourself). Then list the saved zip and confirm that no media entry still has the old bytes (hash compare).
• deleting p:pic leaves rel + media in the zip • drop unused rel; verify by listing/hashing zip media
