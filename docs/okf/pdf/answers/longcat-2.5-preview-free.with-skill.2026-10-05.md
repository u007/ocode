- id: pdf-model-01
  answer: |
    A PDF contains no table structure. A "table" is purely a visual arrangement of
    positioned text glyphs, vector ruling lines, and filled rectangles (shading).
    There is no semantic table object, no row objects, no cell objects. This means
    editing a table requires redacting/redrawing regions of the page (removing
    content and re-drawing rules, shading, and text), not updating a data structure.
    Every edit is a visual reconstruction.

- id: pdf-model-02
  answer: |
    Drawing a white rectangle over text is a visual cover-up, not a removal. The
    original text remains in the content stream underneath — it is still extractable
    by text tools, searchable via search_for(), and selectable/copyable by the user.
    It also increases file size (new content added, old content kept). The correct
    approach is to use page.add_redact_annot() followed by page.apply_redactions(),
    which actually removes the covered content from the content stream.

- id: pdf-model-03
  answer: |
    A plain byte-level search-and-replace on the content stream can fail or corrupt
    the page for several reasons: (1) Text may be split across multiple Tj/TJ
    operators with positioning adjustments between fragments, so the literal string
    "22.50" may not appear contiguously in the stream. (2) With subset fonts, the
    byte values in the stream are glyph codes, not ASCII — the character '2' may
    be encoded as an arbitrary byte value defined by the font's /Encoding or
    /ToUnicode CMap. (3) The string may be hex-encoded (<...>) rather than
    literal (...). (4) Replacing with a longer string can overflow the Tj operand
    or misalign subsequent positioning operators. (5) The same byte sequence might
    appear in a different context (e.g., inside an image stream or a font program).

- id: pdf-model-04
  answer: |
    PyMuPDF uses a top-left origin, y-down coordinate system for all its APIs
    (get_text, insert_text, draw_rect, etc.). Raw PDF uses a bottom-left origin,
    y-up. PyMuPDF handles this conversion automatically, so you work in y-down
    coordinates. For a page with /Rotate, PyMuPDF's coordinate system already
    accounts for rotation — page.rect gives the visible page dimensions and all
    API coordinates are in the rotated (view) space. For a CropBox offset from
    the MediaBox, PyMuPDF's (0,0) is the top-left of the CropBox, not the MediaBox.
    To convert to raw PDF coordinates you must add the CropBox offset and apply
    the rotation/derotation matrices (page.rotation_matrix / page.derotation_matrix).

- id: pdf-locate-01
  answer: |
    Use page.find_tables() (PyMuPDF 1.23+). It detects tables by analyzing ruling
    lines and text positions, returning a Table object with .cells (list of cell
    bboxes), .bbox (table bounding box), and .extract() (text per cell). For each
    cell you get a tuple (x0, y0, x1, y1, text, block_no, row_no, col_no). If
    find_tables() doesn't detect the table, fall back to page.get_text("words")
    and cluster words by y-coordinate (rows) and x-coordinate (columns), or use
    page.get_drawings() to find the ruling lines and infer cell boundaries from
    their intersections.

- id: pdf-locate-02
  answer: |
    page.search_for("Widget C") returns rectangles matching only that specific
    text — typically one rectangle per occurrence. A table row contains multiple
    cells (e.g., item name, quantity, unit price, amount). Removing only the
    "Widget C" match would leave the other cells' text behind. To get the full
    row region, take the matched word's y-coordinate (y0, y1), then use
    page.get_text("words") to find all words whose y-range overlaps that band,
    and compute the bounding box spanning from the leftmost word's x0 to the
    rightmost word's x1. Extend vertically to include any ruling lines.

- id: pdf-locate-03
  answer: |
    Use page.get_text("dict") (or "rawdict" for more detail). This returns a
    nested structure: blocks → lines → spans. Each span is a dictionary with:
    "font" (font name string, e.g. "Georgia-Bold"), "size" (float, in points),
    "color" (integer RGB, convert with pymupdf.sRGB_to_rgb()), "text" (the span
    text), "origin" (tuple of the baseline start point, i.e. bottom-left of the
    first character), and "bbox" (the span's bounding box). The baseline y is
    origin[1], which is what you need for insert_text().

- id: pdf-locate-04
  answer: |
    Column boundaries come from two sources: (1) page.get_drawings() — the vertical
    ruling lines are drawn as line segments whose x-positions define column edges.
    (2) page.get_text("words") — for right-aligned numbers, the x1 values of words
    in the same column cluster at the column's right edge. The find_tables() method
    also returns cell bboxes that directly give column boundaries. For the x
    position where each right-aligned number ends, look at the x1 of the rightmost
    word in each column, or the x1 of the cell bbox from find_tables().

- id: pdf-rowdel-01
  answer: |
    1. Identify the row's full bounding box: use find_tables() to get the row's
    cell bboxes, or get_text("words") clustered by y-band. Extend the bbox to
    include the ruling lines above and below the row.
    2. Redact the entire row region: page.add_redact_annot(bbox) then
    page.apply_redactions(graphics=PDF_REDACT_LINE_ART_NONE,
    images=PDF_REDACT_IMAGE_NONE) to remove text while preserving rules/shading
    that extend beyond the row, or use defaults if the row is self-contained.
    3. Move all content below the deleted row up by the row height: keep an
    untouched copy of the document, redact the region below, then use
    page.show_pdf_page(new_rect, src_doc, pno, clip=old_rect) to stamp the
    original content one row higher with identical appearance.
    4. Recompute the Total from the remaining data rows.
    5. Redact the old Total row and redraw it at the new (higher) position with
    the recomputed value.
    6. Adjust vertical rules to span the new table height.
    7. Save to a new file with garbage=3 or 4.

- id: pdf-rowdel-02
  answer: |
    By default, page.apply_redactions() removes images that are entirely within
    the redaction rectangle (PDF_REDACT_IMAGE_REMOVE) and removes vector graphics
    (lines, curves) that are entirely within the rectangle
    (PDF_REDACT_LINE_ART_REMOVE). This matters because redacting a table row
    could remove the ruling lines and shading fills that define the table's
    appearance. If the row's horizontal rules fall entirely within the redaction
    rect, they will be deleted. You can control this with the graphics and
    images parameters — set graphics=PDF_REDACT_LINE_ART_NONE (0) and
    images=PDF_REDACT_IMAGE_NONE (0) to preserve them, or use the defaults if
    you plan to redraw the rules afterward.

- id: pdf-rowdel-03
  answer: |
    Yes. PyMuPDF's page.show_pdf_page(new_rect, src_doc, pno, clip=old_rect)
    stamps a region from another page (or a copy of the same page) onto the
    current page at a new position, preserving exact appearance. The procedure:
    (1) Keep an untouched copy of the original document (src_doc). (2) Redact the
    old region on the working page. (3) Call show_pdf_page with clip=old_rect
    (the source region) and new_rect (the destination, shifted up by one row
    height). This copies the original pixels/vector content exactly — no
    re-typing, no font matching, no layout changes.

- id: pdf-rowdel-04
  answer: |
    Besides the deleted row itself: (1) The Total row must be recomputed from
    the remaining data. (2) Any subtotals, tax calculations, or summary figures
    that referenced the deleted row's values. (3) If the table spans multiple
    pages, the pagination and any "continued" headers/footers. (4) Cross-references
    to the deleted item elsewhere in the document (e.g., "see Item 3" in notes).
    (5) The table's vertical rules may need shortening if the table shrinks.
    (6) Page numbers or "Page X of Y" if content shifts across pages.

- id: pdf-cell-01
  answer: |
    1. Find the cell's bbox using page.find_tables() (get the cell for the target
    row/column) or page.get_text("words") to locate the word and infer the cell
    extent from surrounding ruling lines.
    2. Read the original text's style: page.get_text("dict") → find the span →
    note font name, size, color, and baseline (origin[1]).
    3. Redact a rect inset inside the cell boundaries (a few points inside the
    ruling lines) with graphics=PDF_REDACT_LINE_ART_NONE and
    images=PDF_REDACT_IMAGE_NONE to preserve the cell's borders and shading.
    4. Measure the new text width: pymupdf.get_text_length(new_text, fontname,
    fontsize). Calculate the right-aligned x position: x = cell_right_edge -
    new_text_width - padding.
    5. Use page.insert_text((x, baseline_y), new_text, fontname=font,
    fontsize=size, color=color) or insert_textbox for multi-line.
    6. Check the return value of insert_textbox (negative = didn't fit).

- id: pdf-cell-02
  answer: |
    The redaction rectangle should be inset slightly inside the cell boundaries —
    typically 2-4 points inside each ruling line. It must fully cover the text
    glyphs to be removed but must not overlap the cell's border lines or adjacent
    cells. If the rect touches or crosses a ruling line, apply_redactions may
    remove that line (if it falls entirely within the rect) or leave a partial
    line segment. Insetting ensures the table grid stays intact while the old text
    is cleanly removed.

- id: pdf-cell-03
  answer: |
    page.insert_textbox() writes NOTHING when the text does not fit in the given
    rectangle. It returns a negative number (the shortfall in points — how much
    taller the text needed to be) and raises no exception. Your options:
    (1) Shrink the font size slightly until it fits. (2) Wrap the text into a
    taller row (which requires shifting all rows below it down and redrawing
    rules). (3) Widen the column (requires redrawing the entire table with new
    column widths). (4) Use page.insert_htmlbox() with scale_low=True, which
    auto-shrinks the content to fit. Always check the return value — a negative
    number means nothing was written.

- id: pdf-cell-04
  answer: |
    The point parameter is the baseline origin — the bottom-left of the first
    character (where the text "sits"). It is NOT the top-left of the text's
    bounding box. If you pass the top-left corner of the old word's bbox, the
    new text's baseline will be at the top of the old text's extent, causing the
    new text to be drawn too high — it will appear above the intended position,
    potentially overlapping the row above or the ruling line. The correct y is
    the baseline y, which you can get from get_text("dict") span["origin"][1] or
    approximately bbox.y1 - descent.

- id: pdf-relayout-01
  answer: |
    1. Redact the Total row and everything below it that will move.
    2. Draw the new row at the old Total row's position: fill the shading, insert
    the cell text, and draw the horizontal rule below it.
    3. Redraw the Total row one row lower (at the old Total position plus one row
    height) with the recomputed total value.
    4. Extend the vertical rules to span the new table height (from the top of
    the table to the new Total row bottom).
    5. Add the extra horizontal rule between the new row and the Total.
    6. If there is not enough space on the page for the shifted-down Total and
    content below, continue on a new page (insert a page and stamp the overflow
    content there).

- id: pdf-relayout-02
  answer: |
    Correct approach: The new column must fit within the existing margins, so all
    columns must be narrowed. (1) Choose new widths for every column that sum to
    the old total width. (2) Measure the widest header/cell text per column using
    pymupdf.get_text_length(text, fontname, fontsize) against the new column width
    minus padding. (3) Shrink the font slightly or wrap text into taller rows if
    needed. (4) Redact the entire old table. (5) Redraw every column (header, all
    cells, shading, vertical and horizontal rules) at the new x positions.
    Common mistake: trying to make the table wider than the page margins, or
    adding the new column without narrowing existing columns, causing the table
    to overflow the page or overlap other content.

- id: pdf-relayout-03
  answer: |
    Acceptable only as a last resort when: no original source file (LaTeX, Word,
    HTML) exists, the edit is too complex for surgical in-place editing, and the
    user accepts that the output will not be pixel-identical. Risks: (1) Fonts
    change — the converter may substitute fonts, altering metrics and appearance.
    (2) Layout shifts — text reflows, spacing changes, page breaks move. (3) Other
    pages may be affected even if you only intended to change one table. (4)
    Formatting is lost or altered — shading, rules, merged cells may not survive
    the round-trip. (5) The process is not deterministic — different converters
    produce different results. Prefer regenerating from the original source, or a
    surgical in-place PDF edit.

- id: pdf-relayout-04
  answer: |
    Use page.get_text("words") to find the lowest text on the page (maximum y1).
    Use page.get_drawings() to find the extent of vector content. Use
    page.get_image_rects() for any images. Compare the lowest content y-position
    against the page's bottom margin (page.rect.height - bottom_margin). The
    difference is the available free space. Also check for any annotations or
    form fields. For a more thorough check, render the page to a pixel array and
    inspect the bottom region for non-white pixels.

- id: pdf-tblins-01
  answer: |
    Method 1 — Draw manually: Compute cell positions (x, y, width, height for
    each cell). Draw shading first with page.draw_rect(rect, fill=color). Draw
    text with page.insert_textbox(cell_rect, text, fontname=..., fontsize=...,
    align=...). Draw rules with page.draw_line() for horizontal and vertical
    lines. This gives full control over appearance.
    Method 2 — Use insert_htmlbox: Build an HTML string with a <table> styled
    to match the document, then call page.insert_htmlbox(rect, html,
    scale_low=True). This handles text wrapping and table layout automatically,
    though you have less fine-grained control over the final appearance.

- id: pdf-tblins-02
  answer: |
    (1) Copy the font family, size, and color from the page's existing table —
    read them with page.get_text("dict"). (2) Match the rule width and color —
    read existing line properties from page.get_drawings(). (3) Match the shading
    pattern — copy the fill colors from existing cells. (4) Use the same padding
    (space between text and cell borders). (5) Draw in the correct order: fills
    first, then text, then rules on top. (6) Measure existing table's row heights
    and column widths to match the visual rhythm. (7) Render the result and
    compare visually with the existing table.

- id: pdf-tblins-03
  answer: |
    Options: (1) Redact everything below the insertion point, restamp it lower
    with page.show_pdf_page(new_rect, src_doc, pno, clip=old_rect) to make room,
    and put any overflow on an inserted page. (2) Use real free space elsewhere
    on the page (e.g., the bottom margin) and tell the user the table was placed
    there instead. (3) Insert a new page and place the table there. (4) If the
    original source file (LaTeX, Word, HTML) exists, regenerate from source.
    Never shrink the surrounding text or overlap existing content — the result
    will be unreadable and unprofessional.

- id: pdf-imgrep-01
  answer: |
    1. Find the image xref: page.get_images() returns a list of tuples where the
    first element is the xref. Or use page.get_image_info(xrefs=True) which
    gives xref, bounding box, and other metadata.
    2. Get the placement rect: page.get_image_rects(xref) returns the rectangles
    where the image is placed.
    3. Replace: page.replace_image(xref, filename="new_logo.png"). This swaps the
    image content at that xref, so the new PNG appears at every placement of
    that xref with the same dimensions.
    The catch with shared images: if the same xref is used on multiple pages or
    multiple times on the same page, replace_image changes ALL of them. If you
    need to change only one occurrence, redact that occurrence's rect with
    images=PDF_REDACT_IMAGE_REMOVE and graphics=0, then use page.insert_image()
    at that rect.

- id: pdf-imgrep-02
  answer: |
    The old logo image remains in the content stream underneath the new one. It
    is still in the file (increasing file size), still extractable, and still
    "present" in the PDF's object structure. If the new image has any transparency
    or does not fully cover the old image's rect, the old logo may show through.
    If the old image is later removed or the new one is moved, the old one becomes
    visible. The correct approach is to use page.replace_image(xref, ...) which
    swaps the image content, or redact the old image first with
    images=PDF_REDACT_IMAGE_REMOVE, then insert the new one.

- id: pdf-imgrep-03
  answer: |
    The logo is likely not a raster image but vector graphics — drawn with
    page.get_drawings() as filled paths, curves, and line segments. Logos are
    often created as vector artwork. To find and replace it: (1) Use
    page.get_drawings() to get all vector paths and their bounding boxes — look
    for a cluster of paths in the logo's position. (2) Check for Form XObjects:
    the logo might be in a Form XObject referenced in the page's /Resources.
    (3) Redact the vector paths' bounding box with graphics=PDF_REDACT_LINE_ART_REMOVE
    (or redact and redraw), then insert the new image at that rect.

- id: pdf-imgins-01
  answer: |
    1. Get the PNG's pixel dimensions: from PIL import Image; img =
    Image.open("sig.png"); pw, ph = img.size.
    2. Compute the fitted rect: target width = 150pt. height = 150 * (ph / pw).
    3. Find the bottom-right area: page_width = page.rect.width; page_height =
    page.rect.height. Set a margin (e.g., 36pt). x1 = page_width - margin; y1 =
    page_height - margin; x0 = x1 - 150; y0 = y1 - height.
    4. Check for overlapping text: page.get_text("words") — ensure no word's bbox
    intersects the target rect. If there is overlap, move the rect up or left.
    5. Insert: page.insert_image(pymupdf.Rect(x0, y0, x1, y1), filename="sig.png").
    The rect's aspect ratio matches the image's, so no distortion occurs.

- id: pdf-imgins-02
  answer: |
    Insert the image once on the first page and capture the returned xref:
    xref = page.insert_image(rect, filename="logo.png"). Then on subsequent
    pages, pass that xref: page.insert_image(rect, xref=xref). This references
    the same image object instead of embedding a new copy. Additionally, saving
    with garbage=4 (doc.save(..., garbage=4)) merges identical image streams
    across the document, deduplicating them at the file level.

- id: pdf-imgins-03
  answer: |
    Likely causes: (1) Draw order — a filled rectangle (background shading or
    cell fill) was drawn AFTER the image, covering it. Draw fills first, then
    images, then text, then rules. (2) The image rect is outside the visible page
    area (check against page.rect / CropBox). (3) The image was inserted on the
    wrong page. (4) The image file path was wrong or the image format is
    unsupported — insert_image may fail silently. (5) The image has a soft mask
    (transparency) that renders as fully transparent against the background.
    (6) The image was inserted but the page was subsequently redacted or stamped
    over.

- id: pdf-fonts-01
  answer: |
    A subset font (e.g., "AAAAAA+Georgia") contains only the glyphs for
    characters that actually appeared in the original document. If you insert
    text with characters that never appeared (e.g., changing "Gadget D" to
    "Quokka Kit" where Q, k, u, i, t may not be in the subset), those glyphs do
    not exist in the embedded font program. The result: blank spaces, empty
    boxes, or wrong characters. Solutions: (1) Use a different font that
    contains the needed glyphs (e.g., a built-in base-14 font like "helv" for
    Latin text). (2) Use page.insert_htmlbox() which can handle font fallback.
    (3) Embed a full font that covers the needed characters. (4) Check glyph
    availability with fontTools before inserting.

- id: pdf-fonts-02
  answer: |
    PyMuPDF's built-in base-14 font names: "helv" for Helvetica regular, "hebo"
    for Helvetica bold. (Also "heit" for Helvetica-Oblique, "hebi" for
    Helvetica-BoldOblique, and similarly for Times, Courier, Symbol, ZapfDingbats.)
    Limitation: base-14 fonts only support Latin characters and basic punctuation
    (essentially WinAnsi/StandardEncoding coverage). They cannot render CJK
    characters, Cyrillic, Arabic, Hebrew, mathematical symbols, or other
    non-Latin scripts. They are also not embedded — they rely on the PDF viewer
    having the font, which can lead to substitution differences across viewers.

- id: pdf-fonts-03
  answer: |
    Detect: use page.get_text("dict") and find the span for the target cell.
    Read span["font"] (e.g., "Georgia-Bold" — the weight is in the name) and
    span["size"] (a float). Match: when inserting new text, use the same font
    name string and size value. For bold, use the bold variant of the font
    (e.g., fontname="Georgia-Bold" or the appropriate bold font name). If using
    insert_textbox, pass fontname and fontsize matching the original. If the
    exact font is not available, use a visually similar font and verify by
    rendering and comparing.

- id: pdf-verify-01
  answer: |
    Re-open the saved output file and verify programmatically: (1)
    page.search_for(old_text) returns an empty list — the old text is gone.
    (2) page.get_text() does not contain the old value. (3)
    page.find_tables().extract() returns the expected rows with correct values.
    (4) Other pages' text is unchanged (compare get_text() on unedited pages
    against the original). (5) For redaction: the redacted region's content is
    absent from the content stream. Then render the page to an image and visually
    inspect for gaps, broken rules, misalignment, or overlapping text.

- id: pdf-verify-02
  answer: |
    No. Saving with incremental=True appends the new revision to the file but
    keeps the old revision intact. The redacted content is still present in the
    previous revision and can be recovered by anyone with a PDF tool that can
    access revision history. For real removal, save to a new file (not
    incremental) with garbage=3 or 4, which rewrites the file and drops the old
    content. Even then, forensic tools may recover data from free space — for
    sensitive content, use a proper redaction tool that also cleans metadata.

- id: pdf-verify-03
  answer: |
    (1) Always save to a new file — never overwrite the original. Work on a
    copy. (2) Use garbage=3 or 4 when saving to ensure redacted/removed content
    is actually dropped from the file. (3) Do not use incremental=True if you
    need to ensure removed content is unrecoverable. (4) Keep a backup of the
    original. (5) Verify the output by re-opening it and checking content,
    not just trusting the script's exit code. (6) Do not log or expose the
    file's contents in debug output if it contains sensitive data. (7) Check
    for silent failures (insert_textbox return value, apply_redactions result,
    search_for empty results).

- id: pdf-verify-04
  answer: |
    A script can complete "successfully" while the PDF is unchanged or corrupted
    because of silent failures: (1) insert_textbox returns a negative number when
    text doesn't fit — it writes nothing but raises no exception. (2)
    apply_redactions() returns False if there are no redaction annots to apply —
    no error, no redaction. (3) search_for() returns an empty list when the text
    isn't found — a loop over the result does nothing. (4) insert_image() may
    fail silently if the file path is wrong or the format is unsupported. (5) A
    redaction rect that is slightly wrong may remove nothing or remove the wrong
    content. The script's "done" print only means it reached the end of the
    code — not that any edit was applied. Verification requires re-opening the
    output and checking the actual content.
