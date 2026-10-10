- id: pdf-model-01
  answer: |
    A PDF has no "table" object. A table is purely visual: text positioned at specific
    coordinates, plus vector lines (rules) and filled rectangles (shading). There is no
    semantic structure linking cells to rows or columns. This means "deleting the third
    row" must be done by: (1) identifying the rectangular region of that row via text
    coordinates, (2) removing the text glyphs in that region from the content stream,
    (3) moving all content below it upward, and (4) redrawing any rules/borders that
    spanned the deleted area. It is a low-level graphical edit, not a structural one.

- id: pdf-model-02
  answer: |
    Drawing a white rectangle over text does not remove the text from the content stream.
    The glyphs remain in the file — they are still extractable with get_text(), searchable
    with search_for(), and copyable. Anyone can recover the "hidden" text. A white box is
    purely cosmetic. Proper redaction (PyMuPDF's page.apply_redactions()) actually removes
    the text bytes from the content stream and replaces them with nothing (or a black
    rectangle), making the text unrecoverable from the saved file.

- id: pdf-model-03
  answer: |
    Plain search-and-replace on raw stream bytes can fail or corrupt because:
    1. The stream is usually compressed (FlateDecode) — you must decompress first.
    2. Text is often split across multiple Tj/TJ operators (kerning, ligatures, or just
       generator output) — "22.50" might be "22" + "." + "50" in separate operators.
    3. In subset fonts, text is encoded as glyph indices, not ASCII — the bytes for
       "22.50" may not exist as a contiguous literal string.
    4. The same byte sequence might appear elsewhere on the page (e.g., in a different
       context) and a blind replace would corrupt that too.
    5. Hex strings (<...>) vs literal strings (...) require different handling.
    6. Replacing with a longer string can overflow a fixed-width field or overlap
    neighboring content.

- id: pdf-model-04
  answer: |
    PyMuPDF uses a top-left origin coordinate system (y increases downward), while raw
    PDF uses a bottom-left origin (y increases upward). PyMuPDF automatically converts
    between them. If the page has /Rotate, PyMuPDF accounts for it — coordinates are
    returned in the rotated (visual) frame. If the CropBox is offset from the MediaBox,
    PyMuPDF's coordinates are relative to the CropBox (the visible area), not the
    MediaBox. This means positions you read from get_text() are already in the correct
    visual coordinate space for drawing operations like insert_text() and
    insert_image(), but if you work with raw PDF objects you must apply the CropBox
    offset and rotation transform yourself.

- id: pdf-locate-01
  answer: |
    Use PyMuPDF's page.find_tables() (available in recent versions). It detects table
    structures on the page and returns a Table object with .cells (list of cell bboxes),
    .rows (list of row bboxes), and .bbox (whole table bbox). Each cell bbox is a
    rectangle (x0, y0, x1, y1). If find_tables() is unavailable or unreliable, fall back
    to: page.get_text("words") to get all word bboxes, then cluster words by similar
    y-coordinate (rows) and x-coordinate (columns) to reconstruct the grid.

- id: pdf-locate-02
  answer: |
    page.search_for("Widget C") returns rectangles around just that text fragment. It
    tells you nothing about the full row extent — the row's left/right boundaries, the
    other cells in the same row, or the row's vertical extent (which may include padding
    or multi-line cells). To get the full row region: find the y-coordinate of the
    match, then collect all words whose vertical center falls within the row's y-range
    (typically determined by the table's rule positions or by clustering), and take the
    min x0 and max x1 across those words to define the row's horizontal extent.

- id: pdf-locate-03
  answer: |
    Use page.get_text("dict"). This returns a nested structure: blocks → lines → spans.
    Each span dict contains: "font" (font name), "size" (font size in points), "color"
    (integer RGB, convert with fitz.sRGB_to_rgb()), "bbox" (span bounding box), "text"
    (the actual text), and "flags" (bitfield indicating bold/italic). The baseline can
    be derived from the bbox: for most fonts, baseline ≈ y1 - descent, or you can use
    page.get_text("rawdict") which includes explicit baseline information per span.

- id: pdf-locate-04
  answer: |
    Column boundaries come from: (1) page.find_tables() which returns cell bboxes with
    explicit x0/x1 for each column, or (2) analyzing the x-coordinates of text spans —
    cluster the x0 values of left-aligned text and x1 values of right-aligned text to
    find column edges. For right-aligned numbers, look at the x1 (right edge) of number
    spans in each column; they should cluster at the column's right boundary. The gap
    between one column's right edge and the next column's left edge defines the gutter.

- id: pdf-rowdel-01
  answer: |
    1. Detect the table with page.find_tables() and identify the target row's bbox.
    2. Identify all content below the table (or below the row) that must shift up.
    3. Redact the row area: add a redaction annotation covering the full row bbox, then
       call page.apply_redactions() to remove the text.
    4. Move all content below the deleted row upward by the row height. In PyMuPDF this
       requires extracting the content (text, images, drawings) and re-inserting it at
       the new position — there is no single "shift region" command.
    5. Update the Total row: find the old total text, redact it, insert the new value.
    6. Redraw any horizontal rules that spanned the deleted row's vertical space.
    7. Verify by rendering the page to an image and inspecting visually.

- id: pdf-rowdel-02
  answer: |
    By default, page.apply_redactions() removes: text characters whose bboxes overlap
    the redaction rect, images that overlap, and vector graphics (drawings) that overlap.
    This matters because a table row redaction might also remove: table rules (which are
    vector drawings), cell background shading (filled rectangles), or parts of adjacent
    rows if the redaction rect is slightly too large. You can control this with the
    apply_redactions() parameters (graphics=..., images=...) but the defaults are
    aggressive — always check what else gets removed.

- id: pdf-rowdel-03
  answer: |
    PyMuPDF has no built-in "move region" operation. Practical approaches:
    1. Extract all content below the row (text via get_text("dict"), images via
       get_images(), drawings via get_drawings()), delete it from the page, then
       re-insert everything at the shifted position. This is complex and may lose
       formatting.
    2. Use a lower-level approach: manipulate the content stream directly to change
       the Tm (text matrix) or Td coordinates of operators below the deletion point.
    3. Use an external tool like pikepdf to edit the content stream, adjusting
       positioning operators.
    None of these are simple; the extract-and-reinsert approach is most common but
    risks losing exact formatting.

- id: pdf-rowdel-04
  answer: |
    Besides the row itself: (1) the Total/summary row value must be recalculated,
    (2) any subtotals or running totals, (3) page numbers or "Page X of Y" references
    if pagination changes, (4) cross-references to the deleted item elsewhere in the
    document, (5) the table's horizontal rules (they may need to be redrawn to close
    the gap), (6) any footnotes or annotations referencing the deleted row, (7) if the
    table now fits on fewer pages, subsequent content may need to shift.

- id: pdf-cell-01
  answer: |
    1. Find the cell text: use page.get_text("words") or page.search_for("22.50") to
       get the bbox of the old text.
    2. Determine the cell's right edge (from table detection or from the x1 of the
       old text if right-aligned).
    3. Add a redaction rect slightly larger than the old text bbox (but not overlapping
       adjacent cells) and call page.apply_redactions().
    4. Measure the new text width: use fitz.get_text_length(text, fontname=..., fontsize=...)
       to compute how wide "54.00" will be.
    5. For right-alignment: x = right_edge - new_text_width. Use
       page.insert_text((x, baseline_y), "54.00", fontname=..., fontsize=..., color=...).
    6. Match the original font, size, and color (read from get_text("dict")).
    7. Verify by rendering and comparing.

- id: pdf-cell-02
  answer: |
    The rectangle should be slightly larger than the text bbox to ensure complete glyph
    removal (glyphs may extend slightly beyond the measured bbox), but small enough to
    not overlap adjacent cells. A tight table has minimal gutters, so an oversized rect
    will remove fragments of neighboring cells. Practical approach: expand the text bbox
    by 1-2 points on each side, but clamp to the cell boundaries (from find_tables() or
    from the gap between columns). Always verify by extracting text after redaction to
    confirm no fragments remain and no neighbors were damaged.

- id: pdf-cell-03
  answer: |
    page.insert_textbox(rect, text, ...) returns the unused height (positive number) if
    the text fits, or a negative value (the deficit) if it does not fit. When text
    doesn't fit, it is silently truncated — only the portion that fits is drawn.
    Options: (1) reduce the font size until it fits, (2) widen the textbox (if space
    allows), (3) shorten the text, (4) use insert_textbox with align= and lineheight
    adjustments, or (5) split the text across multiple lines/rows if the table structure
    allows it.

- id: pdf-cell-04
  answer: |
    page.insert_text(point, text) — point is the baseline origin (bottom-left of the
    first character), not the top-left. If you pass the top-left corner of the old
    word's bbox, the text will be drawn too high: the baseline will be at the top of
    where the old text was, causing the new text to overlap content above it and appear
    misaligned. The correct baseline y is approximately the old bbox's y1 minus the
    font descent (or read directly from get_text("rawdict") which gives the baseline
    explicitly).

- id: pdf-relayout-01
  answer: |
    1. Detect the table and find the Total row's bbox.
    2. The Total row must move down by the height of the new row.
    3. All content below the table must shift down by the same amount.
    4. The table's bottom rule must be extended/redrawn to close the gap.
    5. The new row must be inserted with matching formatting (font, size, shading,
       borders).
    6. If the table now overflows the page, content may need to flow to the next page
       or the layout must be adjusted.
    In practice this requires extracting and re-inserting all shifted content, as
    PyMuPDF has no "insert space" operation.

- id: pdf-relayout-02
  answer: |
    Correct approach: shrink the existing columns to make room for the new column,
    keeping the total table width within the page margins. This means recalculating
    all column widths and re-positioning all cell content. Common mistake: simply adding
    a new column without adjusting existing column widths, which causes the table to
    overflow the page margin or overlap content on the right side. Another mistake:
    not updating the table's grid/tblGrid definition (in the PDF's structural
    metadata) to reflect the new column count.

- id: pdf-relayout-03
  answer: |
    Acceptable when: the PDF was originally generated from a DOCX/HTML source (so the
    round-trip is lossless or near-lossless), the document is simple (no complex
    layouts, forms, or annotations), and you have the original source format available.
    Risks: (1) font substitution — the PDF's embedded fonts may not be available in
    the DOCX editor, (2) layout reflow — tables, columns, and page breaks may shift,
    (3) loss of PDF-specific features (annotations, form fields, metadata, digital
    signatures), (4) the conversion back to PDF may produce a visually different
    document, (5) text extraction and searchability may degrade, (6) incremental
    updates or edit history in the PDF is lost.

- id: pdf-relayout-04
  answer: |
    Use page.get_text("words") to get all text bboxes, page.get_drawings() to get all
    vector graphics bboxes, and page.get_images() to get image bboxes. Compute the
    union of all these bboxes to build a "content map". Then check whether the target
    region (where you want to add content) intersects any existing content bbox. You
    can also use page.get_text("dict") to find gaps between blocks. For a quick check,
    render the page to an image and visually inspect, or use page.get_text("blocks")
    which returns block-level bboxes that are easier to reason about.

- id: pdf-tblins-01
  answer: |
    Method 1 — Manual drawing: Use page.draw_line() and page.draw_rect() to draw the
    table rules and cell backgrounds, then page.insert_text() or page.insert_textbox()
    to add text in each cell. Compute cell positions based on column widths and row
    heights. This gives full control but requires manual layout calculations.
    Method 2 — ReportLab overlay: Create a new PDF page with ReportLab containing just
    the table, then use PyMuPDF's show_pdf_page() to overlay it onto the target
    page at the correct position. This leverages ReportLab's table layout engine.

- id: pdf-tblins-02
  answer: |
    1. Match the font family, size, and color used in the existing table (read via
       get_text("dict")).
    2. Match line weights: measure the thickness of existing rules (from get_drawings()
       or by rendering and measuring pixels) and use the same width in draw_line().
    3. Match shading colors: sample the fill color of existing shaded cells (from
       get_drawings() items with type "f") and use the same color in draw_rect().
    4. Match cell padding: observe the gap between cell text and cell borders in the
       existing table and replicate it.
    5. Ensure z-order is correct: draw backgrounds first, then rules, then text.
    6. Render and visually compare side by side.

- id: pdf-tblins-03
  answer: |
    Options: (1) Reduce the font size or line spacing of the existing paragraphs to
    create a gap, (2) move one of the paragraphs to the next page (if there is room),
    (3) reduce the margins or spacing around the paragraphs, (4) if the content is
    in a text box or column, resize it, (5) as a last resort, reflow the entire page
    layout. In PyMuPDF, creating space requires extracting the content below the
    insertion point and re-inserting it at a lower position — there is no "make room"
    command.

- id: pdf-imgrep-01
  answer: |
    1. Find the old image: use page.get_images() to list images, then
       page.get_image_rects(xref) to get the bbox of the logo on page 1.
    2. Redact the old image area: add a redaction annotation at the image rect and
       call page.apply_redactions(images=fitz.PDF_REDACT_IMAGE_REMOVE) to remove it.
    3. Insert the new image: page.insert_image(rect, filename="new_logo.png") where
       rect is the same bbox as the old image.
    Catch with shared images: if the same image xref is used on multiple pages
    (common for logos), replacing it via xref-level operations (e.g., in pikepdf)
    will change it on ALL pages. To replace on only one page, you must either
    create a new xref for that page or use page-level redaction + insertion.

- id: pdf-imgrep-02
  answer: |
    The old image is still in the content stream underneath the new one. It is still
    extractable (page.get_images() will show it), still searchable if it contains text,
    and if the new image has any transparency, the old image will show through. The
    file size has increased (both images are stored). This is not a true replacement —
    it is a cover-up. Proper replacement requires removing the old image from the
    content stream (via redaction or direct stream editing) before or instead of
    overlaying the new one.

- id: pdf-imgrep-03
  answer: |
    The logo might be: (1) part of a Form XObject (a reusable content stream) — check
    page.get_xobjects() which lists XObjects referenced by the page, (2) drawn as
    vector graphics (paths/curves) rather than a raster image — check
    page.get_drawings(), (3) embedded in an annotation's appearance stream, (4) part
    of a pattern or tiling. To find it: inspect page.get_xobjects() for Form XObjects
    that contain image operators, or render the page and check if the "logo" area
    contains vector drawing commands. If it is vector, you cannot replace it as an
    image — you must redact the area and draw a new image over it.

- id: pdf-imgins-01
  answer: |
    1. Get the image dimensions: img = fitz.open("signature.png"); w, h = img[0].rect.width,
       img[0].rect.height (or use PIL to get pixel dimensions).
    2. Compute height: h_pt = 150 * (h / w) to preserve aspect ratio.
    3. Compute rect: page_w = page.rect.width; margin = 36 (0.5in typical);
       x0 = page_w - margin - 150; y0 = page.rect.height - margin - h_pt;
       x1 = page_w - margin; y1 = page.rect.height - margin.
    4. Check for overlap: verify this rect does not intersect any existing content
       (use get_text("words") and get_drawings() bboxes).
    5. Insert: page.insert_image(fitz.Rect(x0, y0, x1, y1), filename="signature.png").
    6. If overlap exists, adjust y0 upward until the area is clear.

- id: pdf-imgins-02
  answer: |
    Insert the image once to get its xref: xref = page1.insert_image(rect, filename="logo.png").
    Then for subsequent pages, use page.insert_image(rect, xref=xref) — this references
    the same image object rather than embedding a new copy. Alternatively, in lower-level
    PDF editing (pikepdf), add the image as a shared XObject in the document's resource
    dictionary and reference it from each page's /Resources. The key is that the image
    bytes are stored once in the file, and each page's content stream contains only a
    reference (Do operator) to the shared object.

- id: pdf-imgins-03
  answer: |
    Likely causes: (1) Z-order — a filled rectangle or shading was drawn after the
       image, covering it. Fix: ensure the image is inserted after all background
       elements, or use a higher z-order. (2) The image rect is outside the visible
       page area (CropBox) — check that coordinates are within page.bounds(). (3) The
       image file is corrupt or in an unsupported format — verify it opens correctly.
       (4) The image has a transparent or white background and is invisible against
       the page background. (5) The image was inserted on the wrong page. (6) The
       save failed silently — check the return value of doc.save().

- id: pdf-fonts-01
  answer: |
    An embedded subset font (e.g., "AAAAAA+Georgia") only contains glyphs for the
    characters that were used when the PDF was generated. If the new text contains
    letters that never appeared in the document, those glyphs are not in the subset.
    Reusing the embedded font will result in missing glyphs (shown as blank spaces or
    boxes/notdef). Solutions: (1) use a different font that contains the needed glyphs
       (e.g., a system font or a full embedded font), (2) use PyMuPDF's insert_html()
       or Story API which can embed additional fonts as needed, (3) use a base-14 font
       if the characters are in Latin-1 range, (4) embed a full font file using
       page.insert_font(fontfile="path/to/font.ttf").

- id: pdf-fonts-02
  answer: |
    PyMuPDF's built-in base-14 font names: "helv" (Helvetica regular), "hebo"
    (Helvetica bold), "heit" (Helvetica italic), "hebi" (Helvetica bold-italic),
    plus "cour" (Courier), "tiro" (Times-Roman), "symb" (Symbol), "zadb" (ZapfDingbats).
    Limitation: base-14 fonts only support the Latin-1 character set (basic Latin plus
    common Western European characters). They cannot render CJK characters, Cyrillic,
    Arabic, Hebrew, mathematical symbols outside the basic set, or any Unicode
    characters outside the Latin-1 range. For non-Latin text you must embed a font
    file that supports the needed glyphs.

- id: pdf-fonts-03
  answer: |
    Detect: use page.get_text("dict") on the original text to read the span's "font"
    name (which includes weight info like "Georgia-Bold") and "size" value. Match:
    (1) For weight: if the original font name contains "Bold" or the span's flags
    indicate bold (flags & 16), use a bold font variant (e.g., "hebo" instead of "helv",
    or select the bold font from the same family). (2) For size: use the exact size
    value from the original span. (3) If using insert_textbox, pass fontname and
    fontsize explicitly. (4) Render and visually compare — weight differences are
    often subtle and best confirmed by rendering at high DPI and comparing pixel
    patterns.

- id: pdf-verify-01
  answer: |
    1. Render the edited page to a high-resolution image (page.get_pixmap(dpi=200)) and
       visually inspect it — this catches positioning, font, and overlap issues.
    2. Extract text (page.get_text()) and verify the new content is present and old
       content is gone.
    3. Check that removed text is truly gone: search_for() should return no results.
    4. Verify positions: get_text("words") should show content at expected coordinates.
    5. Check for content overlap: compare bboxes of adjacent elements.
    6. Verify the file opens correctly in multiple PDF readers (pdftotext, qpdf --check).
    7. If redaction was used, verify with a hex dump or pikepdf that the text bytes are
       not present in the content stream.

- id: pdf-verify-02
  answer: |
    No, the row is not truly gone. With incremental=True, PyMuPDF appends a new
    revision to the file. The old content (including the confidential row) is still
    present in the earlier revision — it is just not visible in the current view.
    Anyone with a PDF editor or text extraction tool that reads previous revisions
    can recover it. To truly remove the content: save without incremental (full save:
    doc.save("output.pdf") or doc.save("output.pdf", incremental=False, encryption=...)),
    which rewrites the file from scratch and omits the redacted content. Even then,
    verify with pikepdf or a hex search that the text is absent.

- id: pdf-verify-03
  answer: |
    1. Always work on a copy of the original file, never the original itself.
    2. Keep a backup of the original before any edits.
    3. Verify the output before overwriting or delivering: render pages, extract text,
       check positions.
    4. Use a temporary output file, verify it, then replace the original.
    5. Check that the PDF is not encrypted or password-protected in a way that
       prevents editing (or handle decryption appropriately).
    6. Verify the file is not corrupted after saving: open it with pikepdf or
       qpdf --check.
    7. If the PDF has digital signatures, be aware that edits will invalidate them.
    8. Document what was changed for audit purposes.

- id: pdf-verify-04
    answer: |
    "Done" and no errors only means the Python script executed without raising an
    exception. It does not verify: (1) that the PDF file is valid and not corrupted,
    (2) that content appears at the correct position, (3) that fonts render correctly,
    (4) that no content was accidentally removed or overlapped, (5) that the file
    opens correctly in PDF readers, (6) that text is still extractable/searchable,
    (7) that images are visible and correctly sized, (8) that the file size is
    reasonable. A script can silently produce a broken PDF — e.g., wrong coordinates,
    missing glyphs, corrupted streams, or content drawn outside the visible area.
    Verification requires: rendering to images and visual inspection, text extraction
    and comparison, opening with independent tools (pdftotext, qpdf), and checking
    specific expected properties of the output.
