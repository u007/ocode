# step-5-preview-free — closed-book answers for pdf (opencode-go, 2026-10-12); answerer saw only _prompts/pdf.md
Closed-book answers below, each written independently from my own PDF/PyMuPDF knowledge (no corpus, no web, no repo files opened). Version-dependent details (e.g. exact `apply_redactions` constants) are flagged where they matter.

- id: pdf-model-01
  answer: |
    A PDF contains no rows or tables — only a content stream of operators: text-showing
    operators (Tj/TJ) with absolute positions and fonts, path operators for the rules
    (m/l/c + S), and filled rectangles for borders and cell shading, grouped in BT/ET
    blocks, possibly nested in Form XObjects. So the "third row" exists only as a visual
    coincidence: a horizontal band containing a few strings and matching line/shading
    operators. A PDF has no layout engine and never reflows. Deleting the row therefore
    means geometric surgery: find the band (text bboxes plus the ruling lines above/below),
    remove its text, remove its rules/shading, move every bit of content drawn below it up
    by the row height, redraw the affected rules, and recompute anything derived (the
    Total). There is no object you can simply "delete" — you rebuild the region.

- id: pdf-model-02
  answer: |
    Because covering is not removing: the original text operators remain in the content
    stream. The old text is still selectable, still extractable (get_text / pdftotext /
    copy-paste will return it), still searchable, and will show through any transparency
    in your new box or reappear if the overlay is removed or re-saved. It also leaves two
    contradictory copies of the information in the file and grows it. Instead: redact —
    page.add_redact_annot(rect) then page.apply_redactions(), which actually rewrites the
    content stream and deletes the covered characters — then insert the replacement text.
    If you only need a visual patch and explicitly accept that the old text remains, say
    so: it is a cover-up, not an edit.

- id: pdf-model-03
  answer: |
    Several reasons. (1) Content streams are usually compressed (FlateDecode, sometimes
    ASCII85+Flate, or living inside compressed object streams) — the plaintext bytes
    simply are not there, so a naive byte search fails. (2) With an embedded subset font,
    glyphs are addressed through a custom encoding/Differences or an Identity cmap, so the
    visible "22.50" is represented by arbitrary byte codes; searching for ASCII finds
    nothing and writing raw ASCII would map to the wrong glyphs. (3) Text is often split:
    "22." and "50" in separate Tj/TJ operators, hex literals, TJ kerning arrays, multiple
    BT/ET blocks, or in Form XObjects. (4) Even a successful replace changes glyph widths
    the following operators were positioned for (right-aligned columns, totals lines), so
    layout after the string can drift. (5) Editing a filtered stream in place breaks
    /Length, filters, and xref offsets — a library (pikepdf) fixes the container but not
    the semantic issues. Safer: decompress with a library, work on parsed operators (or
    use PyMuPDF redaction+insert), recompute positions from measured widths, then verify.

- id: pdf-model-04
  answer: |
    PyMuPDF uses a top-left-origin coordinate system: x right, y down, points, relative to
    page.rect (which already accounts for /CropBox and /Rotate). Raw PDF user space is
    bottom-left origin with y up, so a PyMuPDF rect (x0,y0,x1,y1) corresponds to PDF-space
    (x0, page.rect.height - y1, x1, page.rect.height - y0). With /Rotate 90/270 the axes
    swap and y direction flips — the visible page is the rotated MediaBox, and raw
    coordinates must be transformed through that rotation; PyMuPDF coordinates are always
    in the visual (rotated) frame. With a CropBox offset from the MediaBox, the visible
    origin is the CropBox's lower-left corner, so a visible position p corresponds to
    user space p + (cropbox.x0, cropbox.y0); page.rect starts at (0,0) at the visible
    corner, so PyMuPDF drawing APIs are safe, but anything written into the raw content
    stream needs that offset added. Read page.rect / page.mediabox / page.cropbox /
    page.rotation instead of assuming a letter-size unrotated page.

- id: pdf-locate-01
  answer: |
    Practical options, in order: (1) PyMuPDF's built-in finder: page.find_tables() (recent
    versions) returns tables whose rows/cells come back as rectangles, derived from the
    ruling lines and text. (2) pdfplumber: page.find_tables()/extract_table() with
    table_settings, using the drawn lines ("lines" strategy) or text alignment; it returns
    explicit cell bounding boxes. (3) DIY: get_text("words") or get_text("dict") gives word
    bboxes; cluster words into lines by y-overlap and into columns by x-gaps; then
    page.get_drawings() gives the actual ruling lines (strokes) and fill rects — the grid
    from the rulings is ground truth for cell boundaries, with text clustering filling in
    cells for borderless tables. (4) camelot ("lattice" for ruled tables, "stream" for
    borderless). For editing you also need the drawings, because the borders are separate
    operators that must line up with your text.

- id: pdf-locate-02
  answer: |
    search_for("Widget C") returns only the rectangles of the matched glyphs — the bbox of
    that one string. The table row is a region: from the leftmost to the rightmost column
    rule, from the horizontal rule above the row to the rule below it, including the
    shading fill and the other cells' text (qty, price) that you must also move or re-set.
    The match rect is a subset of the row and typically does not touch the borders (cell
    padding), so using it as the delete rect leaves borders, shading and neighbouring cells
    behind. To get the full region: take the row's text y-band, then from get_drawings()
    select the horizontal strokes just above and below that band as the row's top/bottom,
    and the outermost vertical strokes (or the table's x-range) as left/right.

- id: pdf-locate-03
  answer: |
    Use page.get_text("dict") → blocks → lines → spans. Each span has "font" (e.g.
    "AAAAAA+Georgia" or "Helvetica-Bold"; the subset prefix before "+" is meaningless),
    "size" (float, often fractional like 10.5), "flags" (italic 2, serifed 4, monospaced 8,
    bold 16), "color" (sRGB integer), "bbox", and "origin" — the (x, y) of the baseline
    start, which is what insert_text expects. Use get_text("rawdict") for character-level
    detail. The font file itself: page.get_fonts() and doc.extract_font(xref) tell you what
    is embedded and its type. When re-inserting, reuse the same font only if it is a
    usable full font; otherwise substitute the metrics-compatible family and match the
    size, weight, color and baseline from these span values.

- id: pdf-locate-04
  answer: |
    From the vector layer: page.get_drawings() — the vertical stroke segments give the
    column rules (their x positions), the horizontal strokes give the row rules, and the
    fill rectangles give cell/row bounds. For right-aligned numbers, don't guess from the
    rules: get_text("words") (or "dict" spans) gives each glyph run's bbox; the right edges
    (x1) of the numbers in a numeric column are all right-aligned, so a new value must be
    inserted so its measured right edge equals that column's established x1, and
    left-aligned text starts at the column's text origin. Word bbox heights and span
    origins give you the baseline y per row. Summary: drawings for the grid, word bboxes
    for the actual text edges and baselines.

- id: pdf-rowdel-01
  answer: |
    Procedure: (1) Copy the file; never edit the only copy. (2) Locate the row: cluster the
    target row's words into a y-band; from get_drawings() take the horizontal rules above
    and below it and the table's left/right x — that is the row rect. (3) Measure the row
    height h (rule to rule). (4) Remove the row's content: page.add_redact_annot(rect) then
    page.apply_redactions() with explicit graphics/text/image options so the rules and
    shading in the band are also removed — otherwise orphan lines and fills survive. (5)
    Shift everything below the band up by h: PyMuPDF cannot move content directly, so
    either re-draw the rows below (re-typing with matched font/size/color) or use the
    preserved-copy technique — stamp the below-region into a temp page via
    show_pdf_page(clip=...), redact the region, then re-stamp it shifted up by h. (6)
    Redraw the row rules that must now bound the surviving rows (the shared border between
    the deleted row and the row below may be gone). (7) Update the Total: redact the old
    total text, insert the new one with matched style. (8) Save to a NEW file with
    garbage=4, deflate=True, then verify by re-extraction and rendering.

- id: pdf-rowdel-02
  answer: |
    apply_redactions() (modern PyMuPDF, 1.23+) takes images, graphics and text parameters.
    Defaults: text is removed — every character whose bbox OVERLAPS the rect is deleted,
    including partially covered glyphs; images default to removal — any image whose
    placement overlaps the rect is deleted entirely, even if only a corner overlaps (so a
    rect brushing a page-wide watermark or background image can destroy the whole image);
    graphics default to removing line art only when it is FULLY COVERED by the rect
    (PDF_REDACT_LINE_ART_REMOVE_IF_COVERED) — so a rule that merely touches the rect (the
    row's bottom rule that is the next row's top rule, or a vertical column rule passing
    through the row) survives as an orphan. For a ruled row/cell this matters twice: a
    text-sized rect leaves every rule and shading fill behind, while a wide rect that
    covers shared borders kills lines the surviving rows need. So choose rects per object:
    cover the band fully if you intend to remove and re-draw the grid, stay inside cell
    bounds if you want borders/shading to survive, and raise the graphics option to
    REMOVE_IF_TOUCHED when you do want shared rules removed. Check your PyMuPDF version —
    the constant names and defaults changed across releases.

- id: pdf-rowdel-03
  answer: |
    Not with a single "move region" call — PyMuPDF has no operator that translates an
    existing area of a page. The workable technique is copy-remove-restamp: (1) create a
    temp document with a page of the same size; (2) on it use show_pdf_page(rect, src_doc,
    pno, clip=region_rect) to capture the region below the deleted band exactly (text stays
    text, fonts/weights/rules carried over, appearance preserved); (3) redact the region on
    the real page to clear it; (4) stamp the captured region back with
    show_pdf_page(shifted_rect, tmp_doc, 0) so everything below sits one row height higher.
    Caveats: links, annotations and form fields inside the region do not move; content
    drawn on top of the region from elsewhere (e.g. a watermark operator after it) is left
    behind unless included in the clip; text remains selectable/searchable. The
    alternative — re-typing each row with insert_text — is explicit and controllable but
    risks font/size/color drift and is much more work; use it when the region is small or
    you are also changing content.

- id: pdf-rowdel-04
  answer: |
    Everything derived from the table and everything positioned after it: the Total row
    value (nothing computes it — you subtract the deleted line yourself), any
    subtotal/tax/discount/total-in-words lines, the line-item or "No. of items" count, the
    horizontal rules and banding around the removed row, and all content below the table on
    the page (another table, footer, signature block) which must move up. Also
    cross-references elsewhere in the document: a summary page, appendix, packing list or
    quoted amount; bookmarks/links pointing at moved content; tagged-structure parents if
    the PDF is tagged. Finally re-check the bottom margin so the shifted content does not
    overflow, and re-page if it does.

- id: pdf-cell-01
  answer: |
    Steps: (1) Copy the file. (2) Find the value: page.get_text("words") or search_for
    restricted to the row's y-band; take the word's rect, and read the containing span via
    get_text("dict") for font, size, flags, color and the span "origin" (baseline y). (3)
    Measure the new text: width = fitz.get_text_length("54.00", fontname=<same base font>,
    fontsize=<same size>) or a fitz.Font object's text_length. (4) Right-align: new x0 =
    column_right_edge - width, where column_right_edge is the x1 of the original word (or of
    the column's other numbers); baseline y = span origin y. (5) Remove the old text:
    add_redact_annot sized to cover the glyphs but not neighbours, then
    apply_redactions(). (6) Insert: page.insert_text((new_x0, baseline_y), "54.00",
    fontname=..., fontsize=..., color=...) with the same family/weight/color and
    overlay=True. (7) Save to a new file with garbage collection. (8) Verify: reopen the
    saved file, re-extract (assert "54.00" present, "22.50" absent), compare span
    attributes with the original, render to PNG and check alignment against the row and the
    column edge. If the original font is an embedded subset lacking the needed glyphs,
    substitute a metrics-compatible full font.

- id: pdf-cell-02
  answer: |
    apply_redactions deletes every character whose bbox OVERLAPS the rect — overlap, not
    containment — so a rect even a point too wide can eat the tail of the neighbouring
    cell's number or a descender from the row below; and the default graphics rule removes
    line art only when fully covered, so a sloppily large rect can wipe the cell's
    background fill or shared rules, while a sloppily small one leaves partial glyphs (the
    last period, the top of a digit) behind. Size the rect to the target word's bbox
    expanded by only a hair (about 0.5–1pt) and clamp it inside the cell boundaries derived
    from the vertical rules (x limits) and row rules (y limits), so you never touch the
    next column's text, the row separators, or the shading. If you want to keep a cell's
    background fill, ensure your rect does not fully cover the fill rect, or set
    graphics=PDF_REDACT_LINE_ART_NONE while redacting text only. Treat the redaction rect
    as cell-scoped geometry, never a loose guess.

- id: pdf-cell-03
  answer: |
    insert_textbox returns the unused rectangle height: positive means spare space, and a
    NEGATIVE value means the text did not fit — in which case nothing is written at all
    (the classic silent no-op). It does not clip or overflow for you; it either fits or
    does nothing. Options when the new text is too long for the column: (1) shrink the
    font size in a loop while fitz.get_text_length(text, fontname, size) > rect.width (or
    check the return value); (2) let it wrap to two lines if the row height allows —
    insert_textbox handles the breaks, but then re-check the row height and total; (3)
    widen the effective rect only if the neighbouring cell is genuinely empty; (4)
    abbreviate the text; (5) abandon insert_textbox and lay out manually with insert_text
    per line with explicit alignment; (6) if the column is structurally too narrow, treat
    it as a relayout problem — widen the column and reflow, taking width from another
    column. Always check the return value and raise when negative.

- id: pdf-cell-04
  answer: |
    point is the baseline origin — where the pen starts for the first glyph — in PyMuPDF
    coordinates: x from the left edge of page.rect, y measured DOWN from the top, in
    points. Passing the top-left corner of the old word's bbox starts the pen at the top of
    the glyphs, so the text draws a full ascent too high: it floats above the line,
    collides with the row above or the table rule, and no longer shares the baseline with
    the row's other cells. Correct: y from the span's "origin" (baseline y) via
    get_text("dict") — or y = word bbox y0 + ascent for that font — and for right-aligned
    numbers x = column_right_edge - get_text_length(text, fontname, fontsize). Also
    remember the y-axis direction differs from raw PDF space: if you ever hand coordinates
    to the raw content stream you must flip y.

- id: pdf-relayout-01
  answer: |
    Everything at and below the insertion point shifts. (1) Choose the new row height h
    matching the existing row rhythm. (2) The Total row and any content below the table
    (more tables, paragraphs, footers, page furniture) must move DOWN by h — using the
    copy-region-restamp technique (capture everything below the insertion y into a temp
    page via show_pdf_page with clip, redact the region, re-stamp shifted down by h) or by
    re-drawing. (3) Draw the new row into the freed band: borders matching the existing
    rules (stroke widths/colors from get_drawings), text with matched font/size/color, and
    right-aligned numbers ending at the column's x1. (4) Draw the two horizontal rules that
    delimit the new row, or re-draw the grid edges that were shifted. (5) Update the Total
    value and any counts (redact old, insert new, matched style). (6) Check the bottom
    margin: if the shifted content overflows, the table must reflow onto a new page — then
    fix pagination, page numbers and "continued" headers. The common failure is drawing
    the new row into the gap and leaving the Total row where it was, producing overlap.

- id: pdf-relayout-02
  answer: |
    Correct approach: take the new column's width out of an existing column (shrink the
    widest one, typically the description) and shift the columns to its right — their
    vertical rules and all their text — by that width, so the table still spans exactly
    margin-to-margin with its right edge unchanged; redraw the affected vertical rules and
    re-align the right-aligned numbers so the last column still ends at the same right
    edge. Common mistakes: adding the column outside the text area so the table overflows
    the right margin or the page; keeping all original column widths and making the table
    wider than the margin-to-margin box; moving the column boundaries in your layout but
    forgetting to move the drawn lines and the text; and disturbing the right-aligned
    columns so totals no longer line up.

- id: pdf-relayout-03
  answer: |
    Acceptable when: the PDF is a working copy whose exact appearance is not contractual;
    the original was itself produced from Word/HTML (a round trip is roughly lossless); you
    are happy with a fresh, clean re-layout rather than the original's exact geometry; and
    you will inspect and fix the result. Risks: conversion is lossy and drifts — fonts get
    substituted (Arial → Liberation Sans), metrics and therefore column widths and row
    heights change, cell padding, leading, hyphens, page breaks, headers/footers and
    pagination move; images are re-encoded or resized; hyperlinks, bookmarks, annotations,
    form fields and tagged structure/accessibility may vanish; and the output is a NEW
    document — you no longer have the original bytes. For invoices, contracts and legal
    documents, or wherever the requirement is "the same document with one small, precise
    delta", round-tripping through DOCX/HTML is the wrong tool: do surgical edits instead.
    If you must do it, diff renders page by page before accepting.

- id: pdf-relayout-04
  answer: |
    Build an occupancy map of the page and test the candidate rect against it. Sources:
    get_text("words")/get_text("dict") bboxes (text), get_drawings() bboxes (rules, fills,
    vector logos), page.get_image_info()/get_image_rects() (image placements, including
    inside Form XObjects), and annotation/widget rects. Union them, then check: (a) no
    overlap between the new table's rect and any existing bbox; (b) the rect stays within
    the printable area — derive margins from the extents of existing content; (c) enough
    height remains below for the whole table plus the footer. Because bboxes include
    padding, confirm with a render: page.get_pixmap(dpi≈150) and scan the target region
    for non-white ink — that catches thin rules and low-alpha watermarks the vector/text
    maps miss. Leave slack for future edits.

- id: pdf-tblins-01
  answer: |
    Two practical ways: (1) Generate-then-stamp (best for ruled tables): build a standalone
    one-page PDF with ReportLab platypus Table (grid lines, header shading, padding,
    fonts), save it to a BytesIO or temp file, open it with fitz and stamp it into the
    target page with page.show_pdf_page(rect, tbl_doc, 0) — the table keeps vector rules,
    shading and real text, and you control position/size via rect. (2) Draw directly with
    PyMuPDF: compute the cell rectangles, page.draw_rect for borders and header/banding
    fills, page.draw_line for open rules, page.insert_textbox for each cell (with align),
    using fonts/colors/line widths copied from the document's existing tables. A third
    option in newer PyMuPDF builds is insert_htmlbox / the Story API (HTML/CSS to box),
    convenient for complex tables. Either way, derive the free-space rect first and
    insert with overlay=True.

- id: pdf-tblins-02
  answer: |
    Consistency: copy the document's own values rather than inventing them — fonts and
    sizes from existing table spans (get_text("dict")), text color from span["color"], rule
    stroke widths, colors and dash patterns from get_drawings() items ("width", "color",
    "dashes"), fill colors for shading/banding from the drawing items' "fill", cell padding
    from the gap between existing text and its cell borders, alignment conventions (left
    for text, right for numbers) and the row-height rhythm. Glitches to avoid: overlapping
    existing ink (check free space first); hairline lines — round coordinates (or offset by
    0.5pt) so 1pt lines land on pixel boundaries at typical zoom; draw_rect defaults to
    filling AND stroking black, so always set colors/None explicitly; ignoring
    insert_textbox's return value (negative = nothing drawn); baseline drift vs the other
    rows (use span origins); overlay=False hiding your drawing under existing content; and
    using mediabox coordinates on a rotated or cropped page. Finally render at 100% and
    300% and compare against a neighbouring existing table.

- id: pdf-tblins-03
  answer: |
    Options in order of preference: (1) Make room by pushing the following content down:
    capture everything below the target y with the clip technique into a temp page, redact
    it, and re-stamp it shifted down by the table's height (the same move-region trick used
    for row insertion). If the pushed content would overflow the bottom margin, move only
    as many lines as fit and reflow the rest onto the next page — then fix page numbers and
    "continued" headers. (2) If the document tolerates it, insert a new page where the gap
    is needed and let content flow around it. (3) Rebuild the page: extract all content and
    redraw it with the table included — heavy but deterministic. What you should not do:
    overlay the table on the paragraph's own line spacing (it will collide with text at
    different zooms/fonts), or squeeze it into the leading between lines, or put it in the
    margin. Verify with a render and an occupancy check that nothing overlaps.

- id: pdf-imgrep-01
  answer: |
    How: (1) On page 1 find the placements: page.get_image_info(xrefs=True) reports images
    actually drawn on the page, including those inside Form XObjects (page.get_images()
    alone only lists the page's direct resources); identify the logo by rect/size/xref and
    get its rect via page.get_image_rects(xref) or the info entry. (2) True replacement:
    remove the old image from the page, then place the new one — page.delete_image(xref)
    followed by page.insert_image(rect, filename="new.png", keep_proportion=True,
    overlay=True) at exactly the old rect. The alternative "true" replacement that leaves
    the draw operators untouched — overwriting the image XObject stream with
    doc.update_stream(xref, ...) — only works if the new PNG matches the old one's pixel
    geometry, color space and alpha layout; otherwise you must also fix /Width, /Height,
    /ColorSpace, /SMask and /Filter, and the drawn rect will stretch the wrong geometry.
    Catch with shared images: the image object lives in the document, not the page — one
    xref is typically referenced from shared resources and used on every page (a header
    logo). So delete_image or stream replacement changes the logo everywhere it appears
    (usually what you want, but it cannot give page 1 a different logo), and you must not
    delete an XObject other pages still reference. If the logo is drawn inside a Form
    XObject, edit that XObject's content/resources, not the page's. Verify by rendering
    every affected page and confirming the old xref is no longer placed.

- id: pdf-imgrep-02
  answer: |
    It is a cover-up, not a replacement. The old logo's bytes are still in the file — still
    extractable (dump images, or render with your layer removed), still declared in the
    resources — and it will show through anywhere your new image is transparent (most
    logos have transparent backgrounds, and antialiased edges let the old artwork bleed
    through). It grows the file by another full image copy, leaves duplicate/contradictory
    content, and any later operation that removes or re-orders content (a redaction, a
    flatten, a viewer re-save) can expose the old logo. For anything
    confidentiality-sensitive it is exactly the "white rectangle over text" anti-pattern.
    Do a real replacement: remove the old image from the page's content/resources
    (page.delete_image, or edit the XObject) and insert the new one; or replace the XObject
    stream when dimensions allow.

- id: pdf-imgrep-03
  answer: |
    Possible causes: (1) the logo is drawn inside a Form XObject — page.get_images() reads
    the page's direct /Resources only, so nested images are invisible to it; (2) it is an
    INLINE image (BI … ID … EI inside the content stream) — inline images are not XObjects
    and never appear in get_images(); (3) it is not an image at all but vector line art —
    get_drawings() will show the paths/fills (common for simple marks and for
    text-rendered-as-paths); (4) it lives in a shared/alternate resource dictionary, a
    Pattern, or a transparency group; (5) a Type-3 glyph trick (rare). How to find it:
    page.get_image_info(xrefs=True) walks the actual display list and reports every
    placement on the page, including inside Form XObjects (inline images come back with
    xref 0), and get_drawings() reveals vector artwork. To replace: for an XObject image
    inside a Form XObject, modify that XObject's content stream or resources; for an inline
    image you must edit the byte range between BI and EI (data and keys /W /H /CS /BPC /F,
    or replace the whole BI…EI block); for vector artwork, redact the region and re-draw,
    or replace the paths. Covering with insert_image works visually but has the same
    caveats as the previous question.

- id: pdf-imgins-01
  answer: |
    Steps: (1) Read the PNG dimensions — fitz.Pixmap("sig.png").width/.height, or PIL —
    call them w and h. (2) height = 150 * h / w, so the aspect ratio is preserved in
    points. (3) Anchor to page 1's rect (page.rect already accounts for rotation/cropbox):
    take margins from the document (or ~36pt), x1 = page.rect.width - margin, y1 =
    page.rect.height - margin, then rect = fitz.Rect(x1 - 150, y1 - height, x1, y1). (4)
    Check collisions: for each word in page.get_text("words") test
    fitz.Rect(w[:4]).intersects(rect), also check get_drawings() and image rects — move
    the rect up or left until it clears. (5) Insert: page.insert_image(rect,
    filename="sig.png", keep_proportion=True, overlay=True) — keep_proportion keeps the
    aspect ratio if your rect ratio is off, but pass the exact-ratio rect anyway. (6) Use
    a signature PNG with alpha; a white-box PNG will paint an opaque white rectangle over
    the page. (7) Verify by rendering page 1 and confirming placement and that no text is
    covered.

- id: pdf-imgins-02
  answer: |
    Each insert_image call embeds a new image XObject with the full image data, so 200
    calls can produce 200 copies (megabytes of bloat). Fixes: (1) Insert the image ONCE,
    then reuse that single XObject everywhere. With pikepdf you can share the object: assign
    the same image object into each page's /Resources /XObject (e.g.
    page.Resources.XObject.Im1 = img_object — sharing the reference, not copying the data)
    and append the drawing code "q <cm> /Im1 Do Q" to each page's content stream. With
    PyMuPDF do the equivalent at the xref level: doc.xref_set_key(page.xref,
    "Resources/XObject/Im", f"{xref} 0 R") to point every page at the one xref, and append
    the Do operator to the page content. (2) Alternative: keep the logo in a shared page
    template/background object that all pages inherit, so it is stored once and drawn once
    per reference. Then save with garbage=4 and deflate=True and confirm the image count
    dropped to one. Note that garbage collection alone will NOT merge duplicates — it only
    removes unreferenced objects.

- id: pdf-imgins-03
  answer: |
    Likely causes, in order: (1) overlay=False: insert_image(overlay=False) puts the image
    BEFORE existing content, so anything already painted there — especially a white or
    shaded background rectangle — covers it. Fix: overlay=True (default) or insert before
    the background is drawn. (2) Genuine z-order problem: a filled box painted after your
    insertion (e.g. you stamped content over the page afterwards) hides it. (3) Wrong
    geometry: coordinates computed from the mediabox on a page with /Rotate or a CropBox
    offset land off the visible area; always compute against page.rect. (4) A degenerate
    rect — zero width/height or beyond page.rect after a bad aspect calculation. (5) You
    inserted on a different page object than the one saved (doc opened twice, wrong pno).
    (6) An incremental save to a path a viewer has cached, or saved to a different file
    than the one reopened. Diagnose by checking the returned value, listing
    page.get_image_rects(xref) for the new xref, and rendering the page to a pixmap — if
    the pixmap does not show it, it is geometry/z-order, not the viewer.

- id: pdf-fonts-01
  answer: |
    A subset font only contains the glyphs actually used in the document: "AAAAAA+Georgia"
    means "subset (random tag) of Georgia", and the embedded font program has no glyphs for
    letters that never appeared. Two consequences: (1) the subset usually ships a custom
    encoding (Differences or an Identity cmap), so your Unicode text cannot even be mapped
    to codes in that font for glyphs that ARE present; (2) for missing glyphs nothing
    renders — the viewer substitutes a fallback or shows notdef/blank boxes, breaking the
    "looks native" requirement. Do not reuse the subset. Either (a) use a base-14 font with
    compatible metrics (Helvetica ≈ Arial, Times ≈ Times New Roman, Courier ≈ Courier New)
    and match size/color/weight, (b) embed a COMPLETE font file of the same family if
    legitimately available (page.insert_font(fontname=..., fontfile="Georgia.ttf") then
    insert_text — mind licensing and metrics), or (c) genuinely extend the subset with
    fontTools (add the missing glyphs from a full font, rebuild with the same encoding) —
    advanced and rarely worth it. Check the subset's real coverage first (doc.extract_font
    and inspect the embedded font's cmap) before assuming which characters are missing.

- id: pdf-fonts-02
  answer: |
    Regular Helvetica is "helv"; bold Helvetica is "hebo" (Helvetica-Bold). PyMuPDF's
    built-in base-14 reserved names: helv/heit/hebo/hebi (Helvetica family),
    cour/coit/cobo/cobi (Courier), tiro/tiit/tibo/tibi (Times-Roman), plus "symb" (Symbol)
    and "zadb" (ZapfDingbats). Limitation: base-14 fonts are never embedded — the viewer
    supplies them, so rendering varies by system/viewer (Linux substitutes Nimbus Sans,
    Windows Arial, etc.) — and they cover only Latin-1/WinAnsi encodings plus the
    Symbol/ZapfDingbats special sets. Anything outside that — accented Latin beyond
    Latin-1, Greek/Cyrillic/CJK/Arabic/Indic scripts, most technical symbols, emoji —
    cannot be rendered and yields wrong glyphs or nothing. For non-Latin or special
    characters you must embed a real TrueType/OpenType Type0 font covering the needed
    Unicode range (insert_font with a fontfile), using an Identity-H-style CMap (and an
    appropriate CJK font/cmap for East Asian scripts).

- id: pdf-fonts-03
  answer: |
    Detect by reading neighboring original cells with page.get_text("dict") and comparing
    span attributes: "font" (name including any subset prefix, e.g. "ABCDEF+Arial-Bold"),
    "size" (float — "slightly different size" almost always means you used insert_text's
    default 11 instead of the real 10.5 or 9), "flags" (bold = 16, italic = 2), "color"
    (sRGB int), and "origin" (baseline). Then match exactly: for base-14 use the bold
    variant (hebo, tibo, cobo); if the document's bold font is embedded and reusable, reuse
    that exact font name and size; otherwise pick the metrics-compatible bold family
    (Helvetica-Bold ≈ Arial-Bold). Insert at the span's baseline origin, never the bbox
    top. Verify by re-extracting your inserted span and asserting font/size/color/flags
    equal the original's, and by rendering a before/after crop — weight and size mistakes
    are measurable but the reader only sees them.

- id: pdf-verify-01
  answer: |
    Not "the script exited 0" — verify the saved artifact. (1) Re-open the SAVED file with a
    fresh fitz.open, never the in-memory document you edited. (2) Assert on extracted text:
    the new value is present, the old value absent, the row count and Total correct — use
    assertions that raise, not print statements. (3) Assert geometry: the new span's
    bbox/baseline is where it should be (right-aligned to the column edge) with matching
    font/size/color/flags. (4) Confirm removed content is truly gone: search the document
    AND the raw file bytes for the deleted string (redaction without apply_redactions, or
    incremental saves, leaves copies behind). (5) Render each affected page to PNG at 100%
    plus a zoomed crop of the edit, and compare against the pre-edit render, with
    differences confined to the expected region. (6) Cross-check with an independent tool —
    pdftotext, pdfplumber, qpdf --check, pdfinfo — do not trust only the library that made
    the edit. (7) Check nothing else moved, e.g. ink diff per page. (8) Open in a real
    viewer if possible.

- id: pdf-verify-02
  answer: |
    No. An incremental save appends a new revision to the existing file and leaves every
    earlier object untouched. The row's original text stream (and anything the redaction
    replaced) is still physically in the file as part of the previous revision, and any
    tool that walks the xref chain — qpdf, most PDF libraries, a hex editor — can recover
    it. Redaction via incremental update is a classic leak. To actually remove the data:
    make sure apply_redactions() was really applied (annotations alone remove nothing),
    then save FULLY to a NEW file (garbage=4, deflate=True), and for thoroughness
    linearize afterwards (qpdf --linearize) so no stale revision survives. Verify by
    scanning the output bytes for the confidential string and by re-extracting text. As a
    rule: never incremental-save onto the original for redaction jobs.

- id: pdf-verify-03
  answer: |
    (1) Never touch the original: copy invoice.pdf to a working name and keep the pristine
    file — record its checksum before and after to prove it is unchanged. (2) Always write
    output to a NEW path; never save (especially incrementally) over the input. (3) Make
    the script fail loudly and be idempotent: assert the target text/structure exists
    before editing (a silent no-op on a re-run double-shifts or double-redacts) and raise
    instead of printing "done". (4) Sanity-check the document's own arithmetic before
    editing: recompute the line and column totals from the cells and assert they equal the
    printed Total. (5) After saving, reopen the saved file and verify — text extraction,
    re-render, an independent parser, qpdf --check. (6) Record what changed (old → new
    values, rows moved) and keep the pre-edit render for comparison. (7) Use unique temp
    filenames/dirs (never a shared scratch dir) and do not leave stray copies of invoices
    around. (8) Respect path scoping/permissions; ask before overwriting anything you did
    not create. (9) If the content is confidential, remember incremental saves leave
    recoverable data.

- id: pdf-verify-04
  answer: |
    Because exit code 0 plus a print statement only means "no exception was raised" — not
    that the intended change happened. Common silent no-ops: search_for/get_text returned
    nothing (zero iterations, nothing redacted); insert_textbox returned a negative value
    and wrote NOTHING because the text did not fit; insert_image with a bad or off-page
    rect; apply_redactions called when no redaction annotations existed; edits made on a
    second in-memory document that was never the one saved; saved with incremental=True to
    the same path (old content survives); saved to a different filename than inspected; or
    the string is extractable but drawn with the wrong font/size/position so it looks wrong
    at 300% zoom. Real evidence: in-script assertions on the post-save state (reopen the
    file you actually wrote and assert extracted text and geometry), a render of the edited
    page compared against the pre-edit render, an independent parser reading the file, and
    a visual inspection. The script's log is not evidence; the verified contents of the
    saved file are.
```

ASKED — 36 closed-book PDF questions, own knowledge only, YAML by id.
WORKED — BUILT full answer set: PDF internals model, locating tables/rows/cells, row deletion/redaction semantics, cell replacement/alignment, relayout (rows/columns/round-trip/space), table insert, image replace/insert/shared-XObject, subset vs base-14 fonts, and verification/safety.
FOUND — High-value distinctions that decide correctness: PDF has no row objects (geometry surgery only); cover-up ≠ edit (old bytes remain extractable); redaction kills any overlapping character and whole overlapping images, but line art only when fully covered; insert_textbox writes nothing when text overflows; insert_image bloat from repeated embeds vs shared XObject; incremental redaction saves leave recoverable revisions.
DECIDED — Answered each item independently, flagged version-sensitive details (apply_redactions constants, PyMuPDF feature availability) rather than asserting exact values. BOTTOM LINE: 36/36 answered; uncertain constants explicitly labelled.
NEXT — Feed to grader as-is. If graded low on fonts/imgins, re-verify exact PyMuPDF version constants — that is the only known soft spot.
