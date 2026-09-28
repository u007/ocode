# PDF Editing Benchmark — Human Render

> **Generated from `questions.yaml` (corpus_rev 1). Do not grade from this file
> — `questions.yaml` is the source of truth.** If they disagree, the YAML wins.

Legend: **W** = weight (1–3), **D** = difficulty. Rubric shows scored points
(`•`) and partial-credit levels (`~`).

---

### pdf-model-01 · pdf-model · W3 · easy
**Q:** A user asks you to "delete the third row of the table" in an existing PDF. What does a PDF actually contain for that table, and what does that imply about how the edit must be done?
**A:** A PDF has no table, row or cell objects. The page content stream holds positioned drawing operators: text shown at x/y coordinates (Tj/TJ), plus separate lines and filled rectangles for the rules and shading. "Deleting a row" means removing that row's text and graphics, then moving or redrawing everything below it so no gap is left. Nothing reflows by itself.
• no table/row/cell structure — content stream of positioned text + separate vector lines/rects • nothing reflows: must remove the row's content AND move/redraw content below it ~ says PDFs are 'hard to edit' / fixed layout without explaining positioned operators

### pdf-model-02 · pdf-model, verify-safety · W3 · medium
**Q:** A quick way to "remove" text from a PDF is to draw a white rectangle over it and type the new text on top. Why is that not an edit, and what should be used instead?
**A:** The original text is still in the content stream underneath: it can be extracted, searched, copied, and read by screen readers and parsers (and it leaks data if it was sensitive). The white box also hides the table rules and shading under it, and fails on non-white backgrounds. Actually remove the content: redaction (PyMuPDF add_redact_annot + apply_redactions) or content-stream editing, then draw the replacement.
• old text remains in the text layer: extractable/searchable/copyable (data leak) • use true removal: redaction apply (or content-stream edit), then insert new text ~ says overlay 'looks wrong' / hides lines but misses that text survives

### pdf-model-03 · pdf-model · W2 · medium
**Q:** You want to edit PDF text by modifying the raw content stream (e.g. with pikepdf) and replace the string "22.50" with "54.00". Why might a plain search-and-replace on the stream bytes fail or corrupt the page?
**A:** Streams are usually compressed (Flate), so the bytes must be decoded first. Even decoded, the visible text is often not stored as one literal: it can be split across several Tj/TJ operators or kerned TJ arrays, hex-encoded, or written as glyph ids through a font's encoding (Identity-H CID fonts, subset fonts), so "22.50" may never appear. A different-length replacement also shifts alignment (right-aligned numbers stop lining up). Parse the stream tokens properly, or use a higher-level tool (redact + insert).
• text may be split/kerned (TJ arrays), hex or glyph-id encoded (CID/subset) — literal may not exist • compression and/or alignment/length shift — prefer parsed tokens or redact+insert ~ mentions only compression

### pdf-model-04 · pdf-model · W2 · medium
**Q:** In PyMuPDF you read a word's bbox from `page.get_text("words")` and want to draw at that position. How do PyMuPDF coordinates relate to raw PDF coordinates, and what changes if the page has a /Rotate or a CropBox that is offset from the MediaBox?
**A:** PDF user space has its origin at the bottom-left with y going up. PyMuPDF exposes a top-left origin with y going down, relative to the (unrotated) page, so its extracted bboxes and its insert/draw methods share one system. Positions taken from get_text can be passed straight back. Rotated pages: page.rect reflects the rotation. Converting between systems needs page.rotation_matrix / derotation_matrix, and raw content-stream edits need the MediaBox/CropBox offset. Mixing raw PDF numbers with PyMuPDF rects puts things upside down or shifted.
• PDF origin bottom-left y-up; PyMuPDF top-left y-down (consistent between its extract and insert APIs) • rotation / CropBox offset must be accounted for (rotation/derotation matrix or box offsets) ~ knows y is flipped but nothing about rotation/cropbox

### pdf-locate-01 · table-locate · W3 · easy
**Q:** How do you programmatically find a table in an existing PDF page and get the bounding box of each row and cell (not just the text)?
**A:** Use a table finder that gives geometry. In PyMuPDF, page.find_tables() returns tables with .bbox, .rows[i].bbox, .rows[i].cells (a cell bbox each), .header and .extract(). pdfplumber's page.find_tables() or extract_tables() is similar. It finds tables from the ruling lines, or from text alignment when there are no lines. As a fallback, group get_text("words") by their y coordinate to rebuild rows.
• a table finder with geometry (PyMuPDF find_tables / pdfplumber) → table/row/cell bboxes • detects via ruling lines or text alignment; words-with-coordinates fallback ~ only extracts text (extract_text / pdftotext) with no coordinates

### pdf-locate-02 · table-locate · W2 · medium
**Q:** You have the text of the row you want ("Widget C"). Why is `page.search_for("Widget C")` alone not enough to define what to remove, and how do you get the full row region?
**A:** search_for only returns the rectangle of that phrase. The row also has other cells (qty, price, amount), shading and rules across the table's whole width, and the phrase may occur more than once. Take the row from the table finder (the row bbox that contains the hit), or span the table's x-range at the hit's y-range, bounded by the neighbouring horizontal rules. Check that the match is unique and inside the table.
• search hit covers only that phrase — row spans all cells + shading/rules across table width • use the table row bbox (or table x-range × rule-bounded y-range); check uniqueness ~ uses search_for rect and pads it without reasoning about other cells

### pdf-locate-03 · table-locate, cell-edit · W2 · medium
**Q:** Before replacing a cell's text you want to match its original style. How do you read the font, size, colour and baseline of existing text in PyMuPDF?
**A:** page.get_text("dict") (or "rawdict") gives blocks → lines → spans. Each span has font (name), size, color (an sRGB int; convert with pymupdf.sRGB_to_pdf), flags (bold/italic bits) and origin (the baseline point) plus bbox. Use origin as the baseline for insert_text. Map the font name to a usable font: a base-14 alias like helv/hebo, or the embedded font file.
• get_text('dict'/'rawdict') spans: font, size, color, flags • use span origin (baseline) / bbox for placement; convert color int ~ names get_text('dict') but no fields/placement

### pdf-locate-04 · table-locate, table-relayout · W2 · hard
**Q:** You need to know the table's column boundaries and the x position where each right-aligned number ends, so that new or edited values line up. Where do you get that information?
**A:** From the table geometry: the find_tables cell bboxes (the column x-edges), or the vertical rules from page.get_drawings(), which are line and rect items. For right-aligned cells, measure the existing values' word bbox x1: that is the right padding edge (cell x1 minus padding). Place new text at x = right_edge − text_length. Take the widths from the file. Don't assume them.
• column edges from cell bboxes or vertical rules via get_drawings • right-aligned: use existing values' x1 as the edge, x = edge − measured width ~ eyeballs/assumes column positions

### pdf-rowdel-01 · row-delete · W3 · medium
**Q:** Outline a correct procedure to delete one row from a ruled, shaded table in an existing PDF (rows below must move up, the Total must be updated) using PyMuPDF.
**A:** 1) Locate the table and rows (find_tables), and extract the data. 2) Redact the deleted row AND everything below it that has to move (the remaining rows, the Total row and any content under the table), or simply the whole table: add_redact_annot on those rects, then apply_redactions. 3) Redraw the remaining rows shifted up by one row height with the same style: text, alternating shading, horizontal and vertical rules. Put the Total row back with a recomputed value. 4) Move the content that was below the table up too, or leave it in place on purpose. 5) Save to a new file and verify. Rebuilding the whole table from extracted data is the simplest reliable version.
• true removal (redact/apply) of the row AND the content that must shift • redraw remaining rows shifted up one row height, keeping text + shading + rules • recompute Total from data; handle content below the table ~ removes the row text only, leaving a gap or orphan lines

### pdf-rowdel-02 · row-delete, cell-edit · W3 · hard
**Q:** In PyMuPDF, what do the default arguments of `page.apply_redactions()` do to images and vector graphics, and why does that matter when you redact a table row or cell?
**A:** The defaults are images=PDF_REDACT_IMAGE_PIXELS (2), which blanks the overlapping image pixels, graphics=PDF_REDACT_LINE_ART_REMOVE_IF_COVERED (1), which removes line-art covered by the redaction, and text=PDF_REDACT_TEXT_REMOVE (0). So a redaction over a row or cell also deletes the shading rectangles and rule segments it covers, and punches a hole in any image it touches. To change only text, pass graphics=PDF_REDACT_LINE_ART_NONE (0) and images=PDF_REDACT_IMAGE_NONE (0). If you are rebuilding the region anyway, keep the defaults and redraw the graphics.
• default removes covered vector graphics (lines/shading) and blanks overlapping image pixels • pass graphics=0 / images=0 (LINE_ART_NONE / IMAGE_NONE) to keep them, or redraw deliberately ~ knows redaction removes text but unaware of graphics/image side effects

### pdf-rowdel-03 · row-delete · W2 · medium
**Q:** Instead of re-typing every row below a deleted row, is there a way in PyMuPDF to move an existing region of a page up, preserving its exact appearance?
**A:** Yes. Keep a copy of the source document. Redact the region to be moved (plus the deleted row) on the target page. Then use page.show_pdf_page(target_rect, src_doc, pno, clip=source_rect) to stamp the original region at its new position, shifted up by the row height. The stamped region is a Form XObject: it looks identical and its text stays extractable. Alternatively, rebuild those rows from the extracted data.
• show_pdf_page with clip from a copy of the original, placed one row higher (or equivalent XObject) • must redact the old region first so it isn't duplicated ~ says 'just redraw everything' with no region-move technique

### pdf-rowdel-04 · row-delete, verify-safety · W2 · easy
**Q:** After deleting a row from an invoice table, what else in the document is likely to need changing besides the row itself?
**A:** Derived values: the Total, subtotal and tax figures (recompute them from the remaining rows; don't patch them by guesswork). Row numbering or alternating shading, which must stay consistent after the shift. Content positioned below the table, such as notes, and page references. If the task is ambiguous, confirm which totals should change.
• recompute totals/derived values from the remaining data • keep striping/numbering consistent and handle content below ~ mentions only the total

### pdf-cell-01 · cell-edit · W3 · medium
**Q:** Replace one right-aligned number in a table cell (e.g. "22.50" → "54.00") in an existing PDF with PyMuPDF so it looks native. Give the steps.
**A:** 1) Find the old value's bbox (search_for, or the find_tables cell) and read its style (get_text dict: font, size, color, origin). 2) Add a redact annotation on a rect slightly inset inside the cell, covering the glyphs but not neighbouring cells, and apply it with graphics=0 and images=0 so the rules and shading stay. 3) Measure the new text with pymupdf.get_text_length or Font.text_length. Insert it with page.insert_text at x = old_right_edge − width, keeping the old baseline y, font, size and colour. 4) Save to a new file and re-extract to verify.
• true removal with a tight rect + graphics/images preserved • measure new width and right-align to the old right edge at the old baseline • match font/size/colour from the original span ~ inserts at the old left x (misaligned) or overlays

### pdf-cell-02 · cell-edit · W2 · medium
**Q:** PyMuPDF's `apply_redactions` removes "characters whose bbox overlaps the redaction rectangle". How should you size the rectangle when redacting one cell of a tight table, and why?
**A:** Make it cover the target glyphs completely but stay inside the cell. Inset it from the cell borders and don't let it reach the next cell's or row's text. Any character whose bbox merely overlaps is removed, so a padded rect silently deletes characters from neighbouring cells. Then check that the neighbours are still intact.
• overlap rule → oversized rect deletes neighbouring characters • cover the glyphs, inset from borders, verify neighbours after ~ says 'make it a bit bigger to be safe'

### pdf-cell-03 · cell-edit, table-relayout · W2 · medium
**Q:** The new cell text is longer than the column is wide. What does PyMuPDF's `insert_textbox` do when the text does not fit, and what are your options?
**A:** insert_textbox writes nothing when the text doesn't fit. It returns a negative number (the height shortfall) and raises no exception, so you must check the return value. Options: - Reduce the font size a little (within reason). - Wrap into more lines, making the row taller and shifting the rows below. - Widen the column by re-laying out the table. - Use insert_htmlbox, which can scale text down to fit (scale_low). Never let text spill over the cell border.
• negative return, nothing written, no exception — must check the value • options: shrink font, wrap + taller row + shift, widen column/relayout, htmlbox scaling ~ knows it may overflow but not the silent negative return

### pdf-cell-04 · cell-edit · W2 · easy
**Q:** `page.insert_text(point, text)` — what does `point` refer to, and what goes wrong if you pass the top-left corner of the old word's bbox?
**A:** point is the baseline start of the first character (bottom-left of the text baseline), not the top-left corner. Passing bbox.top_left draws the text roughly one line too high, since the glyphs sit above the baseline. Use the old span's origin, or approximately bbox.y1 minus the descender.
• point = baseline origin (bottom-left), not top-left • top-left → text drawn ~a line too high; use span origin

### pdf-relayout-01 · table-relayout · W3 · medium
**Q:** Add a new row to an existing ruled table just above its Total row in a PDF. What must happen to the Total row, the table rules and any content below the table?
**A:** Remove the Total row and everything below it that is in the way, then redraw. Draw the new row at the old Total position, in the right striping colour. Draw the Total one row lower, with a recomputed value. Extend the vertical rules and add the extra horizontal rule. Move the content below the table (notes) down by one row height, or check there is enough free space. If the page lacks room, the table must continue on a new page. You can't just draw a row over whatever sits underneath.
• shift Total (recomputed) and content below down one row; redact then redraw • extend vertical rules + extra horizontal rule + keep striping • check free space / page overflow → continue on next page ~ draws the new row into the gap below the table without moving anything

### pdf-relayout-02 · table-relayout · W3 · hard
**Q:** Add a new column to a table that already spans the full width between the page margins. What is the correct approach and what is the common mistake?
**A:** The common mistake is to append the column to the right, which overflows the margin or page, or to squeeze text into an existing column's space so it overlaps. Correct approach: re-lay out the whole table. Extract the data. Choose new column widths that sum to the same total width, shrinking the wide columns, and check the widest text in each still fits (measure it; reduce the font slightly or wrap if needed). Redact the old table and redraw every column, with header, cells, shading and vertical and horizontal rules, at the new x positions.
• appending right overflows margin; must redistribute widths within the same total width • measure content to fit new widths (font/wrap fallback deliberate) • remove old table and redraw all columns incl. header/rules/shading at new x ~ adds column at right edge / shrinks page scale

### pdf-relayout-03 · table-relayout, pdf-model · W2 · medium
**Q:** Someone proposes: convert the PDF to DOCX (or HTML), edit the table there, and convert back to PDF. When is that acceptable and what are the risks?
**A:** It is only acceptable as a last resort, and only if exact fidelity doesn't matter. PDF→DOCX converters guess the structure. Fonts, spacing, positions, headers and footers, images and other pages shift. Tables often come out as floating text boxes, and the result is a different document. The better choices are: edit the original source (template, HTML, reportlab code, spreadsheet) and regenerate, if it exists; or do a surgical in-place edit of the PDF. Ask whether a source is available.
• round-trip loses fidelity (layout/fonts/other pages change) — last resort • prefer regenerating from the original source if available, else surgical in-place edit ~ says it's fine / the easiest way

### pdf-relayout-04 · table-relayout, table-insert · W2 · medium
**Q:** How do you check whether there is enough empty space on a page to grow a table or place new content without overlapping anything?
**A:** Collect the occupied rectangles: text block and word bboxes (get_text "blocks"/"words"), drawings (get_drawings rects), and images (get_image_info bboxes). Compute the free band, for example between the table bottom and the next element, and the page bottom margin. Test the planned rect against those bboxes with intersects(). Keep within the margins inferred from the existing content.
• gather text + drawing + image bboxes • test planned rect for intersection and stay within margins/page ~ only checks text, not drawings/images

### pdf-tblins-01 · table-insert · W3 · medium
**Q:** Insert a new small ruled table (header + 2 rows) into free space on page 1 of an existing PDF. Describe two practical ways to do it in Python.
**A:** 1) Draw it directly with PyMuPDF: page.insert_text for each cell (measure for alignment), page.draw_line or draw_rect for the rules and header fill, laid out on a computed grid inside the free rect. 2) Generate the table separately and stamp it: - insert_htmlbox(rect, "<table>…</table>", css=…), which lays out HTML tables in a rect, or - build a one-page PDF with reportlab (Table + TableStyle) sized to the rect, then page.show_pdf_page(rect, table_doc, 0). Both keep the text real (extractable). Pasting a raster image of a table does not.
• direct drawing: insert_text + draw_line/draw_rect on a computed grid • stamp approach: insert_htmlbox table or reportlab table → show_pdf_page (text stays real) ~ only 'insert an image of a table'

### pdf-tblins-02 · table-insert · W2 · medium
**Q:** When drawing a new table into an existing page, how do you make it look consistent with the document and avoid visual glitches?
**A:** Reuse the document's existing style: the font and size (read spans), rule width and colour (from get_drawings), header fill and padding, and the left margin (align with existing content). Draw fills first and text after, so the fills don't cover the text; draw the rules last or with care. Keep the cell padding consistent, and measure text so numbers right-align.
• copy existing font/size/rule width/colour/margins from the page • paint order fills → text/lines; consistent padding + measured alignment ~ generic 'make it look nice'

### pdf-tblins-03 · table-insert, table-relayout · W2 · hard
**Q:** The new table must go between two existing paragraphs, but there is no gap there. What are your options?
**A:** There is no reflow. Either: - Make room by moving everything below the insertion point down (redact that region and restamp it lower with show_pdf_page(clip=...), or re-insert it). If content then runs past the page bottom, spill it onto a new inserted page. - Or place the table where there is free space, or on a new page, and tell the user. - Or regenerate from source if available. Don't overlap existing content.
• no reflow: must move content below down (redact + restamp/reinsert) handling page overflow • alternatives: new page / free space (tell user) / regenerate from source; never overlap ~ draws table over the paragraph or shrinks everything

### pdf-imgrep-01 · image-replace · W3 · medium
**Q:** Replace an existing logo image on page 1 of a PDF with a new PNG at the same position and size using PyMuPDF. How, and what is the catch with shared images?
**A:** Find the image xref with page.get_images() and its placement with page.get_image_info(xrefs=True) or get_image_rects(xref). Then call page.replace_image(xref, filename="new.png"). Placement stays the same because only the image object is swapped. The catch: an xref can be referenced from many places (every page's header logo, for example), and replace_image changes all of them. To replace a single occurrence, remove that placement (redact its rect with images=PDF_REDACT_IMAGE_REMOVE) and insert_image the new file at that rect. Keep the aspect ratio in mind: a different-aspect PNG gets stretched into the old box.
• find xref + rect (get_images / get_image_info/get_image_rects); replace_image(xref, filename) • xref shared → replace_image changes every use; single occurrence = remove that placement + insert_image • aspect ratio/stretch awareness ~ inserts the new image on top of the old one

### pdf-imgrep-02 · image-replace, verify-safety · W2 · medium
**Q:** You put a new image on top of an old logo (insert_image at the same rect) and it looks right. What is wrong with that?
**A:** The old image is still in the file and on the page, just covered. It shows through if the new image has transparency or a different aspect. It can still be extracted (data leak if the logo was confidential), and the file keeps both copies. Replace the object or remove the old placement, and verify there is exactly one image at that rect.
• old image still present/extractable/shows through transparency; bloats file • replace or remove old placement; verify one image at that rect

### pdf-imgrep-03 · image-replace · W2 · hard
**Q:** `page.get_images()` shows no image, yet the logo is visible on the page. What could be going on, and how do you find and replace it?
**A:** The logo may be: - inside a Form XObject: get_images(full=True) shows the referencer, and get_image_info / get_image_rects still find placements; - vector artwork built from paths, not a raster image (see get_drawings); - an inline image in the content stream; - part of an annotation's appearance stream. Identify which kind it is. Vector art can't be image-replaced: remove it by redacting with graphics removal, then insert the new image.
• Form XObject / inline image / annotation appearance / vector paths as causes • vector: redact graphics then insert; else locate via get_image_info/full=True ~ assumes the PDF is corrupt / scanned

### pdf-imgins-01 · image-insert · W3 · easy
**Q:** Insert a signature PNG at the bottom right of page 1, 150pt wide, keeping its aspect ratio, without covering text. How do you compute the rect and insert it with PyMuPDF?
**A:** Read the pixel size (Pixmap or PIL). Height = 150 × h/w. Right-align x1 to the content's right edge (for example the table's x1). Put y1 above the footer's top with some padding. rect = Rect(x1 − 150, y1 − height, x1, y1) in PyMuPDF's top-left coordinates. Check the rect doesn't intersect any text or drawing bboxes. Then call page.insert_image(rect, filename="signature.png") (keep_proportion=True by default).
• compute height from aspect ratio; place relative to real content edges in top-left coords • insert_image(rect, filename=…) and check no intersection with text ~ hard-codes coordinates without checking content

### pdf-imgins-02 · image-insert · W2 · medium
**Q:** You insert the same logo on every page of a 200-page PDF with insert_image and the file grows by 200 copies of the image. How do you avoid that?
**A:** insert_image returns the image xref. Insert once from the file and pass xref=that_xref on the later pages, so every page references the same image object. Saving with garbage≥3 (deduplication) also merges identical objects. PyMuPDF may already reuse an identical image within one session, but pass xref explicitly.
• reuse via xref returned by first insert_image (xref= param) • or save with garbage/dedup to merge identical streams

### pdf-imgins-03 · image-insert, pdf-model · W2 · medium
**Q:** You inserted an image with PyMuPDF but it does not appear, or appears underneath a filled background box. Name the likely causes.
**A:** Likely causes: - overlay=False, so it went under the existing content (e.g. under a filled rect); - the rect is off the visible page: a y-origin mix-up (bottom-left vs top-left), a rotated page, or a CropBox offset; - a zero-size or inverted rect; - the page was saved without the change: saved to the original without incremental, or never saved; - a transparency/mask issue. Check page.get_image_info() for its bbox.
• overlay flag / paint order under a filled shape • coordinate mistake (origin/rotation/cropbox/empty rect) — verify via get_image_info bbox

### pdf-fonts-01 · fonts · W3 · hard
**Q:** The table text uses an embedded subset font (name like "AAAAAA+Georgia"). You change an item name to text with letters that never appeared in the document. Why can reusing the embedded font fail, and what should you do?
**A:** A subset font contains only the glyphs that the document used. New letters have no glyph (or no code mapping), so they render blank or as .notdef boxes, and may not extract. Use a full font: the original font file if you have it (insert_font(fontfile=…)), or the closest system or base-14 font. Check glyph coverage, for example Font(fontfile).has_glyph(ord(c)), and verify by rendering and re-extracting.
• subset contains only used glyphs → new chars render blank/.notdef • embed a full font (original TTF) or closest base-14; check glyph coverage + verify render ~ knows subset fonts exist but proposes reusing it anyway

### pdf-fonts-02 · fonts · W2 · medium
**Q:** What are PyMuPDF's built-in base-14 font names for Helvetica regular and bold, and what limitation do base-14 fonts have for non-Latin or special characters?
**A:** helv (Helvetica) and hebo (Helvetica-Bold). Others include tiro/tibo (Times), cour (Courier), symb and zadb. Base-14 fonts are not embedded and use a simple 8-bit encoding (WinAnsi/Latin), so characters outside it are missing. That includes CJK, many symbols, and € in some setups. For those, embed a TTF/OTF via fontfile or use a Noto font (for example fontname="china-s" or pymupdf-fonts).
• helv / hebo (base-14 short names) • 8-bit/Latin only, not embedded → embed a TTF for other chars ~ gives full names only (Helvetica-Bold) with no limitation

### pdf-fonts-03 · fonts, cell-edit · W2 · medium
**Q:** You edited a bold header cell but your new text comes out in regular weight and slightly different size. How do you detect and match the original weight and size?
**A:** Read the original span from get_text("dict"). Its "size" is the exact font size. For the weight, check the span font name (for example "Helvetica-Bold") and the flags bit for bold (flags & 16). Then choose the matching font (hebo, or the bold TTF) and size in insert_text. Don't assume 11 (the insert_text default) or regular weight.
• span size + font name/bold flag (flags & 16) from get_text dict • pick matching bold font + exact size (don't rely on defaults)

### pdf-verify-01 · verify-safety · W3 · easy
**Q:** After editing a table in a PDF, how do you verify the edit actually worked (not just that the script ran)?
**A:** Re-open the saved output and check the text layer: - search_for or get_text on the old value returns nothing; - the new values are present; - find_tables().extract() on the output equals the expected rows in order; - the other pages' text is unchanged; - nothing overlaps or falls outside the page. Render the page (get_pixmap or pdftoppm) and look at it for gaps, broken rules, misalignment and stretched images. A render alone can't tell a white-box overlay from a real edit.
• re-extract from saved output: old gone, new present, table rows equal expected • render to image for visual check (rules/gaps/alignment) + other pages unchanged • knows a visual check alone can't catch overlays ~ only 'open it and look' or 'script didn't error'

### pdf-verify-02 · verify-safety · W3 · medium
**Q:** You removed a confidential row with redaction and saved with `doc.save("same.pdf", incremental=True)`. Is the row gone from the file?
**A:** Not necessarily. An incremental save appends the changes and keeps the previous revision in the file, so the old content stream (with the row) can be recovered by truncating to the earlier %%EOF or with a forensic tool. For real removal, do a full save to a new file with garbage collection (garbage=3 or 4, deflate). PyMuPDF refuses to save to the original path without incremental=True anyway ("save to original must be incremental"), so write a new file and replace it deliberately.
• incremental keeps the old revision → old row recoverable • full save to a new file with garbage collection for real removal ~ says yes it's gone

### pdf-verify-03 · verify-safety · W2 · easy
**Q:** A user asks you to edit invoice.pdf. What file-safety practices should you follow?
**A:** Don't destroy the original. Write to a new output file (or keep a backup and only replace after verifying). Leave pages you weren't asked to touch unchanged, and check the page count and the other pages' text. Preserve metadata, bookmarks and form fields unless told otherwise. If the PDF is encrypted or signed, say that editing breaks the signature or needs the password. Report exactly what changed.
• keep original / write new file, replace only after verifying • other pages untouched + signatures/encryption/metadata awareness ~ only 'make a backup'

### pdf-verify-04 · verify-safety, pdf-model · W2 · medium
**Q:** You wrote a Python script that edits a PDF. The script ran without errors and printed "done". Why is that not evidence the PDF is correct?
**A:** Many PDF edit calls fail silently or partially: - insert_textbox returns a negative value and writes nothing; - apply_redactions returns False when there was no annotation; - search_for returns [] and the loop does nothing; - text lands off-page (coordinate mistakes); - redaction removes neighbouring glyphs or rules. Check return values in the script (fail loudly) and verify the saved output by re-extracting and rendering.
• silent failures: negative return / empty search / off-page / collateral removal • check return values (fail fast) + verify saved output ~ generic 'always test'
