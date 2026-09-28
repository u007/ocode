- id: pdf-model-01
  answer: 'A PDF has no table object. It contains a content stream of drawing operators: text-show operators
    (Tj/TJ) that paint glyphs at absolute coordinates using fonts, plus vector operators (re, m/l, f,
    S, sh) that paint the cell shading and the ruling lines. "The table" is only an emergent pattern of
    glyph runs and lines. Therefore you cannot delete a row as a structural edit; you must reconstruct
    the region: identify the row''s y-band from the horizontal rules, remove (redact or rewrite) the glyphs
    and shading and boundary rules in that band, move everything below the band up by the row height,
    redraw the rules/shading to close the gap, and update any dependent text such as totals. The data
    is not recoverable as a row — there is no row.

    '
- id: pdf-model-02
  answer: 'Painting a white rectangle over text is an overlay, not an edit. The original glyphs remain
    in the content stream, still extractable, searchable, copyable and accessible, and they will reappear
    if the overlay is removed, reordered, or rendered by a viewer that ignores it (or if the underlying
    text is a different colour). It also fights the page''s own drawing order and z-order. The correct
    approach is to actually remove the text — in PyMuPDF, `add_redact_annot` + `apply_redactions()` (which
    strips the glyphs from the content stream) or rewriting the content stream — and then insert the new
    text as real text. Note a redaction still fills the rectangle by default, so set the fill to the intended
    background colour (or none) to avoid covering shading.

    '
- id: pdf-model-03
  answer: 'Several reasons. (1) Compressed filters: page content is usually FlateDecode, so the literal
    bytes "22.50" are not present until decompressed. (2) Text is stored as font-specific character codes,
    not ASCII — a subset font may map the digit glyphs to arbitrary or multi-byte codes, possibly with
    a custom /Encoding or /Differences. (3) A logical string can be split across multiple Tj/TJ/show operators,
    or broken by kerning/spacing arrays (TJ numbers), so the digits are not contiguous. (4) Strings are
    escaped (parentheses, backslashes) and may live in object streams. (5) Any length change shifts byte
    offsets, so without rebuilding the xref and stream lengths the file corrupts — editing in place is
    unsafe. (6) Even a successful byte swap changes the advance width, so the number no longer lines up
    with the column unless you also fix the positioning/TJ adjustments. The robust route is to parse the
    text operators (or use a library that re-writes text), not to grep raw bytes.

    '
- id: pdf-model-04
  answer: 'Raw PDF space: origin at the lower-left of the MediaBox, y increasing upward, units 1/72 inch.
    PyMuPDF page space: origin at the top-left, y increasing downward, and it is anchored to the CropBox
    (the visible page), not the MediaBox. For a page with no rotation and CropBox = MediaBox at (0,0),
    the conversion is y_pymupdf = page.rect.height - y_pdf and x_pymupdf = x_pdf. With a CropBox offset,
    subtract the cropbox lower-left in raw space: x'' = x - cropbox.x0, y'' = cropbox.y1 - y. With /Rotate
    90/180/270, PyMuPDF reports and accepts coordinates in the unrotated page frame, while `page.rect`
    reflects the rotated displayed size; use `page.rotation_matrix` / `page.derotation_matrix` (or `page.rotation`)
    to convert between frames. Consequences: text bboxes from get_text and the points you pass to insert_text
    are in the same PyMuPDF frame, so mixing them is safe, but anything derived from raw content-stream
    numbers or from page.rect of a rotated/offset-cropped page must be converted, or your overlay lands
    in the wrong place.

    '
- id: pdf-locate-01
  answer: 'Tables are not stored, so you either use a library with table detection or reconstruct from
    primitives. PyMuPDF (1.23+) has `page.find_tables()`, which returns Table objects exposing `.bbox`,
    `.rows`, `.cells`, `.header` and `.extract()`; each row and cell has its own bbox. pdfplumber has
    `page.find_tables()` / `.extract_tables()` with line- and text-based strategies; Camelot/Tabula are
    heavier alternatives. Without detection, reconstruct manually: pull vector line segments from `page.get_drawings()`,
    classify horizontal vs vertical, cluster their y/x coordinates to find the rule grid, intersect them
    to get cell rectangles, then assign words (`page.get_text("words")`) into cells by containment. The
    line-based method gives exact row/cell bboxes when the table is ruled; for borderless tables you must
    cluster text column x-positions instead.

    '
- id: pdf-locate-02
  answer: '`search_for` returns quads for the matched text only. It gives you just the glyphs of "Widget
    C", possibly as several fragments if the text is split, and it ignores cell padding, the cell background/shading,
    the ruling lines that bound the row, and the right-hand numeric cells. It also cannot distinguish
    which occurrence if the string repeats. To get the full row region you need the row''s geometry: take
    the table (find_tables/get_drawings), find the row whose cell text contains "Widget C", and use that
    row''s bbox (y-range between the two bounding horizontal rules, x-range across the table width). Then
    redact that full band. Fixing only the search rect would leave the shading and the rules behind, and
    would not define what to reflow.

    '
- id: pdf-locate-03
  answer: 'Use `page.get_text("dict")` (or "rawdict" for per-char detail). Blocks → lines → spans give
    `font`, `size`, `flags`, `color` (an sRGB integer), `origin` (the baseline start point of the span),
    `bbox`, and ascender/descender. Locate the span(s) whose bbox covers the word, and filter chars in
    rawdict for exact glyphs. Convert the colour int to a PDF fill colour with `fitz.sRGB_to_pdf` (newer
    `pymupdf.sRGB_to_pdf`). The baseline is `span["origin"]`: its y is the baseline, its x is where the
    span''s text starts, so for a right-aligned cell you compute the start x yourself. The font name alone
    is not enough — it may be an embedded subset, so to reproduce it exactly extract the embedded font
    (`doc.extract_font(xref)`) and register it with `page.insert_font(...)`, or accept a close builtin
    like "helv". Then reinsert with insert_text using fontname, fontsize, color and the baseline origin.

    '
- id: pdf-locate-04
  answer: 'Column boundaries come from the table geometry: the vertical ruling lines (from find_tables
    cell bboxes, or by clustering vertical segments in `page.get_drawings()`), which give each column''s
    x0/x1. Failing rules, cluster the x0/x1 of words across all rows to infer column edges. The right
    edge a right-aligned number should end at is the column''s right boundary minus cell padding — you
    can confirm it by reading the x1 of the existing right-aligned numbers in that column (they will share
    nearly the same right x) or by using the cell''s bbox. To place a new value, compute its width with
    `fitz.get_text_length(text, fontname, fontsize)` and set the insertion x = right_edge - width (PyMuPDF
    insert_text takes the left baseline origin). Alternatively pass the cell rect to `insert_textbox(...,
    align=fitz.TEXT_ALIGN_RIGHT)`. Keep the same font and size as the existing numbers or the measured
    widths will not match.

    '
- id: pdf-rowdel-01
  answer: 'A workable procedure: (1) open the PDF, locate the table (find_tables or line detection) and
    get the target row''s bbox and the y of the rules above/below it. (2) Read the style of the row''s
    text and the background colour and the rule positions. (3) Add redaction annotations covering the
    full row band (text + shading + the rules you want gone), sized to the row only so neighbouring rows
    and the outer borders are untouched; set the annotation fill appropriately (or none) so it does not
    repaint the page. (4) Call `apply_redactions` with explicit image/graphics options (see next item)
    so you do not destroy unrelated vector art or punch holes in images. (5) Remove the leftover gap:
    shift the region below the row up by one row height. The clean way is `show_pdf_page` — clip the source
    region (the same page copied into a temp document) and stamp it into a target rect translated upward,
    which preserves exact appearance. Re-typing everything is the fallback. (6) Redraw anything you removed:
    the row separator rules, the vertical inner rules across the reclaimed band, and shading if the table
    is striped. (7) Update the Total cell: redact the old number and insert the recomputed one, matching
    style and right alignment. (8) Verify by extracting text and by rendering the page, and check that
    content below did not collide with the footer and that a page break is not now needed.

    '
- id: pdf-rowdel-02
  answer: '`apply_redactions(images=..., graphics=..., text=...)`. Defaults in current PyMuPDF are roughly:
    text removed (all characters whose bbox overlaps the rect are stripped), images = PDF_REDACT_IMAGE_PIXELS,
    i.e. image pixels inside the redacted area are blanked/pixelated, and graphics = PDF_REDACT_LINE_ART_NONE,
    i.e. vector line art is left untouched. (Older versions lacked the graphics parameter and removed
    overlapping vector art; never rely on the default — pass them explicitly.) Why it matters: a table
    row is full of vector art (the rules and the shaded background). With graphics=NONE the shading and
    border lines remain, leaving a ghost band and grid lines where the row was — you may want LINE_ART_REMOVE_IF_COVERED
    to drop exactly what the rect covers without harming rules elsewhere, but choosing it too aggressively
    can erase borders that merely overlap the rect. And with images=PIXELS, a full-width row rect that
    crosses a logo or signature image will destroy those pixels; if the table sits on a background image
    or the row overlays artwork, you must shrink the rect, use PDF_REDACT_IMAGE_NONE, or reposition the
    image. In short, the defaults can both leave junk (vector shading/rules) and destroy content (image
    pixels), so the rect and the options have to be matched deliberately.

    '
- id: pdf-rowdel-03
  answer: 'Yes. PyMuPDF has no "move region" call, but `Page.show_pdf_page(rect, src_doc, pno, clip=clip_rect)`
    renders a clipped region of a source page into a target rectangle on the destination page, preserving
    the exact appearance (text, vector art and images) as a Form XObject. The trick is that a page cannot
    be stamped onto itself, so copy the page (write it into a temporary in-memory document, or use `page.show_pdf_page`
    with the same document but a different page number). Then: clip the rectangle covering everything
    below the deleted row on the copy, and stamp it onto the working page shifted up by the row height;
    or build an overlay page and merge. This reproduces the below-table content exactly (and its text
    remains selectable). A lower-level alternative is to wrap the page content in q ... cm ... Q with
    a translation matrix via `page.get_contents` / `doc.update_stream`, but that shifts the whole content
    stream unless you split it, and it is easy to corrupt.

    '
- id: pdf-rowdel-04
  answer: 'Anything computed from the removed row: the Total, any subtotal, tax, discount, shipping and
    amount-due figures; an item-count or "N items" line; row numbers/line numbers if the table numbers
    its rows; zebra/alternating row shading parity for all rows below (stripes must be re-done); the table''s
    bottom border and any continuation/"continued on next page" header if the shortened table now paginates
    differently; a summary/index or TOC elsewhere in the document that mirrors the row; totals restated
    in words; anything below the table that should move up (signature block, notes, footer) so no gap
    or overlap remains; and cross-references, bookmarks or page numbering that depend on the layout. Also
    check whether freeing the row means the next page''s content could be pulled back, or whether a previously
    broken page now has a stray continuation header.

    '
- id: pdf-cell-01
  answer: 'Steps: (1) locate the cell: detect the table, or `search_for("22.50")` and expand the returned
    quad to the cell interior (or use the cell bbox from the table object). (2) read the existing number''s
    style from get_text("dict"/"rawdict"): font, size, colour int (convert with sRGB_to_pdf), and the
    span `origin` for the baseline. (3) create a redaction annotation covering the old glyph bboxes —
    tightly, so it removes every old character but not the adjacent cell text or the cell borders; if
    the cell has a non-white fill, set the redaction fill to that colour or to none so the shading is
    preserved. (4) `apply_redactions()` with explicit image/graphics options. (5) insert the new value
    at the baseline origin, right-aligned: x = cell_right - fitz.get_text_length("54.00", fontname, fontsize),
    then `page.insert_text((x, baseline_y), "54.00", fontname=..., fontsize=..., color=...)`. (6) reproduce
    the font faithfully — if the original is an embedded subset, extract it with doc.extract_font and
    register via page.insert_font, otherwise use the closest builtin. (7) verify by extracting text (no
    leftover "22.50", no stray digits) and rendering to confirm alignment and no overlap with the column
    rule.

    '
- id: pdf-cell-02
  answer: 'Because removal is by bbox overlap, the rectangle must fully contain every glyph of the old
    cell text (including ascenders/descenders and any overhang that get_text''s bbox may clip) yet not
    intersect any character you want to keep. So: use the union of the old word''s char bboxes, padded
    by a small epsilon (about 1pt) to catch antialiasing and to be sure the last glyph is caught — but
    stop before the neighbouring cell''s text and before the vertical rule if you intend to keep it. If
    the rect is too small, the edge characters survive as visible fragments or are missed; if too large,
    it deletes the adjacent cell''s first digits or, if line art is set to be removed, eats the cell border.
    Where the cell is tight, prefer redacting only the cell interior (cell rect inset by the border half-width)
    rather than the full cell, and keep per-cell redactions separate so each can be sized independently.

    '
- id: pdf-cell-03
  answer: '`insert_textbox` returns a float: >= 0 (the unused remaining height in the rect) if all text
    fit; a negative value if it did not fit, in which case it writes only what fits and the overflow is
    lost. It does not automatically shrink the font, and it does not overflow the rect. Options: reduce
    the font size until it fits (compute with `get_text_length` and the available width); use a narrower/condensed
    font; enable wrapping by giving the rect enough height and a smaller line height, though a single-row
    cell usually has no room; widen the column by re-laying out the table; shorten or round the value
    (fewer decimals, a unit suffix in the header instead of every cell); use `TextWriter` with scaling/clipping;
    or, if none fit, change the layout (landscape, smaller table-wide type) or regenerate the document
    from its source data. Never force overflow by drawing outside the cell.

    '
- id: pdf-cell-04
  answer: '`point` is the baseline origin of the first character — the left end of the baseline, which
    is what MuPDF uses to place the text (its y is the baseline, not the bottom of the ink box and not
    the top). If you pass the top-left corner of the old word''s bbox, y is the top of the glyphs, so
    the baseline is placed one ascender too high and the new text is drawn about one line above where
    it should be, colliding with the row above and misaligned with the rest of the column. x would also
    be wrong unless the bbox x0 happens to equal the intended start. The correct anchor is the span''s
    `origin` from get_text (or, for right-aligned numbers, the origin you computed from the right edge
    minus the measured width).

    '
- id: pdf-relayout-01
  answer: 'You must create the space, not just paint the row. The Total row and all content below it have
    to move down by the height of the new row, so: shift the region from the Total row to the bottom of
    the page down by that height (show_pdf_page with a translated clip is the appearance-preserving way),
    redraw the table''s vertical rules so they now run through the new row, add the new row''s horizontal
    top/bottom rules, reproduce its shading (matching the zebra pattern parity, which also changes for
    the rows below), and insert its cell text with matching fonts/alignment. If the new row is a line
    item, the Total itself must be recomputed and rewritten (redact + insert), plus subtotal/tax/amount
    due. If there is no room before the bottom margin, the overflow must be pushed to a continuation page
    with a repeated header, or the whole table re-paginated; otherwise you will stamp over the footer.
    Verify there is no overlap with the footer, signature block or page number, and that column alignment
    is preserved.

    '
- id: pdf-relayout-02
  answer: 'Correct approach: because the table already fills the full text width, adding a column means
    redistributing width — shrink the existing columns (proportionally, or take the space from the widest
    columns), recompute every column''s x boundaries, move or redraw the vertical rules at the new x positions,
    reflow each existing cell''s content to its new position (right-aligned and centred values must be
    recomputed, left-aligned ones may need shifting), re-wrap header labels, possibly reduce the font
    size, and then add the new column''s cells and rules. The common mistake is to assume there is spare
    width and simply overlap the new column into existing space or squeeze only the last column: the table
    then runs past the margin or onto the page edge, columns collide, or the rules no longer line up with
    the text. If shrinking cannot make it fit legibly, change the page to landscape, reduce type size,
    split the table, or abandon in-place PDF editing and regenerate the document from its source data
    (HTML/LaTeX/office) — which is the only clean way to truly re-layout.

    '
- id: pdf-relayout-03
  answer: 'Acceptable when the document is simple and semantically structured — mostly flowing text, straightforward
    tables, no critical absolute positioning, no forms, signatures, or fine typographic layout — and when
    a one-off "get the content roughly right, then re-export" workflow is good enough, or when you own
    both ends and can visually inspect the round-trip. It is also reasonable for producing a *new* derivative
    document rather than preserving the original.


    It is not acceptable when fidelity matters: regulated/archival/legal documents, signed PDFs, forms,
    documents with exact pagination or print constraints.


    Risks: pagination and line breaks change; fonts are substituted or lost; embedded fonts go missing;
    vector art, gradients, and precise positioning degrade; images may be recompressed, resized, or recolored;
    tables get restructured/mangled; headers/footers drift; page size and margins change; links, annotations,
    form fields, bookmarks, tags/accessibility, metadata, and the OCR/text layer are lost or altered;
    hidden or layered content can be dropped or exposed; digital signatures are invalidated (and any encryption/permissions
    are stripped or changed); DOCX/HTML libraries may inject their own styles. The round-trip also produces
    a brand-new document, not a patched original, so byte-level or provenance guarantees are gone. Treat
    it as lossy; always diff the result against the original and prefer direct PDF editing when exact
    layout must be preserved.

    '
- id: pdf-relayout-04
  answer: 'Enumerate everything already on the page and compute the complement of their bounding boxes.


    With PyMuPDF: take `page.rect` (or the CropBox) as your usable area, then collect occupied rectangles
    from `page.get_text("words")` / `page.get_text("blocks")` / `page.get_text("dict")` (text spans run
    through `span["bbox"]`), `page.get_drawings()` (vector paths, fills, rules — note white-on-white fills
    still occupy space), `page.get_images()` cross-referenced with `page.get_image_rects(xref)` / `page.get_image_info()`
    for placements, annotations (`page.annots()`), and form fields (`page.widgets()`). Then test candidate
    rectangles against all of these with `fitz.Rect.intersects()` / `intersection()` and require zero
    intersection plus a comfort margin.


    Practical method: pick the target rect, shrink/scan it, and test against every occupied rect plus
    a small padding. To grow a table, find the lowest occupied y below the insertion point and cap the
    new bottom above it; check horizontally against left/right margins and adjacent columns.


    When geometry is ambiguous (clipping paths, transparency groups, Form XObjects, art boxes), fall back
    to rasterizing the page region at a decent DPI (`page.get_pixmap(clip=...)`) and analyzing pixels
    for non-background content, e.g., variance / unique colors / a threshold mask. That catches content
    that bbox lists miss and confirms a region is truly blank.


    Also respect margins/bleed, the crop box vs media box difference, and page rotation (derive coordinates
    in the same space you''ll draw in, using `page.derotation_matrix`).

    '
- id: pdf-tblins-01
  answer: 'Two practical approaches:


    1. Draw it directly on the page with PyMuPDF primitives. Compute the target rect, then draw the ruling
    with `page.draw_line()` (or `page.draw_rect()` for full/partial grids) and place each cell''s text
    with `page.insert_text()` or `page.insert_textbox(fitz.Rect(...), text, fontname=..., fontsize=...,
    align=...)`. You control every line, font, and cell baseline, and the result stays vector. For a light
    gray grid use `color=(0.6,0.6,0.6)` on the lines and match the document''s existing text style.


    2. Author the table as a separate one-page document and stamp it in. Generate a one-page PDF (e.g.,
    with ReportLab, or by building a page with PyMuPDF/`TextWriter`, or by rendering an HTML table via
    `fitz.Story`) sized exactly to the table, then use `page.show_pdf_page(target_rect, src_doc, 0)` to
    embed it. This keeps vector text/lines, lets a table library handle row/column sizing, and makes it
    trivial to re-generate or reuse the table. You can also render it as a pixmap if you don''t need selectable
    text, but vector-stamping is preferable.


    Both are non-destructive overlays on an existing page. Option 1 is best for small tables and tight
    style matching; option 2 is best when the table is complex or you already have it in another format.
    Whichever you use, verify the target rect is actually free (see the empty-space question) before drawing.

    '
- id: pdf-tblins-02
  answer: 'Match the surrounding typography and use vector drawing consistently:


    - Sample the existing look: read `page.get_text("dict")` around the insertion region to recover the
    exact `span["font"]` name, `span["size"]`, `span["color"]`, and `span["flags"]`, and copy those values.
    Reuse the document''s real font (embed the same TTF/OTF via `page.insert_font(fontname=..., fontfile=...)`)
    rather than letting a base-14 substitute appear.

    - Rule lines: match existing stroke width (`page.get_drawings()` shows the widths used), color, and
    style. Use thin, consistent lines; avoid dash patterns unless the document uses them. Prefer `draw_line`/`draw_rect`
    with `fill=False` for grids.

    - Align to the document grid: snap cell edges and text baselines to the same positions/spacing as
    existing content (use span origins and the text''s bounding boxes as guides). Keep consistent padding
    between cell border and text.

    - Keep placement precise and unrotated unless the page is rotated; account for CropBox offset and
    rotation via `page.derotation_matrix` so your rect lands where you think.

    - Avoid glitches: don''t overlap or clip existing content (clip/choose a free rect), don''t let `insert_textbox`
    silently truncate (check its return value and reduce font), and don''t draw behind opaque fills —
    draw on top (`overlay=True`). Watch anti-aliasing/color mismatches by using the same color space/opacity.
    Avoid reusing an embedded subset font for new glyphs.

    - Verify visually: render the region to a pixmap and compare against neighboring rows, and re-extract
    text to confirm font/size/color.

    '
- id: pdf-tblins-03
  answer: 'If there is genuinely no gap, you cannot insert without overlapping existing content — so you
    must change the layout, not just paint pixels. Options, roughly in order of preference:


    1. Reflow the content below the insertion point. Rebuild the page: extract the content, shift the
    lower portion down by the table''s height, and re-stamp it, or regenerate the page from source. This
    is the correct but most invasive option and is fragile for arbitrary PDFs.

    2. Put the table on its own page immediately after the current one (or as an appendix/attachment).
    Safest and cleanest when a few points of displacement are acceptable.

    3. Reclaim space: tighten spacing, reduce paragraph leading or the new table''s font/row height so
    it fits in the existing gap, or extend into unused margin area if the document allows.

    4. Move the table to the nearest genuinely free area (top/bottom of the page, a facing page, or after
    the following paragraph) and add a reference/callout.

    5. Reduce content: shorten one paragraph to open the gap.

    6. If displacement is acceptable, create the gap by pushing subsequent content down — effectively
    option 1 implemented locally via redaction plus re-drawing of the affected blocks.


    Be honest with the user: overlaying a table on top of live paragraphs corrupts readability, so a reflow,
    a new page, or a space-reclaiming compromise is required. Never silently overlap text.

    '
- id: pdf-imgrep-01
  answer: 'Find the image and its placement, then replace the image object.


    Steps: enumerate `page.get_images(full=True)` to get `(xref, smask, width, height, bpc, colorspace,
    ...)`; use `page.get_image_rects(xref)` (or `page.get_image_info()`) to get the rect(s) where that
    image is drawn. If you only want to change the pixels while keeping the placement, call the document-level
    replacement, e.g. `doc.replace_image(xref, filename="new.png")` (or pass `stream=`/`pixmap=`), which
    swaps the image XObject for the given xref and preserves every existing placement, transform, and
    mask. If your PyMuPDF version lacks `replace_image`, cover the old image and insert the new one at
    the same rect, but see the caveat below. Then save.


    The catch — shared images: a single xref can be referenced by many pages, many times on one page,
    or inside shared Form XObjects. Replacing the xref changes *every* occurrence, not just page 1. Also
    `get_images()` may not show images nested inside Form XObjects or inline images, and the "logo" may
    be vector art. If the new PNG has a different aspect ratio, the existing placement matrix will stretch
    it. If the same logo appears elsewhere and you only want page 1 changed, you must instead: create
    a new, separate image object for page 1 and draw it, while hiding/removing the old one on that page
    only (redact the old rect, or edit the content stream/XObject accordingly). Keep `keep_proportion`/correct
    aspect in mind, and remember masks (SMask) and color spaces.

    '
- id: pdf-imgrep-02
  answer: 'The old image is still there. Drawing a new image at the same rect is an overlay, not a replacement:
    the original image object remains embedded in the file and is still painted in the content stream,
    with the new one simply drawn on top.


    Consequences:

    - File size grows (two copies of the logo).

    - The old logo is fully recoverable from the PDF (privacy/branding/compliance problem — deleting the
    visible one is not deletion).

    - If the PNG has any transparency, is inset, or has a different aspect ratio, the old logo shows through
    at the edges or beneath.

    - If the new image doesn''t fully cover the old rect (transforms, rotation, or a slightly different
    rect), remnants are visible.

    - The old image can still be extracted by tools that enumerate image XObjects.

    - Duplicate/odd rendering in some viewers.


    Correct approach: replace the image at its xref (`doc.replace_image(xref, ...)`) so all references
    point at the new pixels, or genuinely remove it (delete/redact the old placement and image object)
    before inserting, then save non-incrementally so the old bytes don''t linger in a previous revision.

    '
- id: pdf-imgrep-03
  answer: 'Several possibilities; `get_images()` only reports raster images in the page''s resources,
    so anything else is invisible to it.


    - The "logo" is vector art (paths/fills). Find it with `page.get_drawings()`.

    - The image lives inside a Form XObject (possibly nested). `get_images()` may not resolve behind it
    — enumerate page XObjects with `page.get_xobjects()` and inspect each form''s resources/objects (`doc.xref_object`,
    `doc.xref_get_key`), recursing as needed.

    - It is an inline image (BI/ID/EIxx in the content stream), which has no image XObject and no resource
    entry. Use `page.get_image_info()` (which reports rendered images, optionally with xrefs) or parse
    the content stream from `page.read_contents()`.

    - It is an annotation appearance or a stamp/watermark in an annotation, or a widget. Check `page.annots()`
    / appearance streams.

    - It is drawn by an optional-content (OCG) layer, or via an image pattern or tiling.

    - It is a separate PDF page overlaid with `show_pdf_page`, or an embedded file.


    To find it: use `page.get_image_info(xrefs=True)` first, then `get_drawings()`, then walk the content
    stream and XObjects for `Do` / `BI` operators and inspect referenced streams. To replace: for a true
    XObject anywhere in the chain, replace its image xref (`doc.replace_image`) or edit the owning Form
    XObject''s resources; for vector art, redact the area and insert the new raster; for inline images,
    rewrite the content stream to swap the inline data; for annotation appearances, update/remove the
    annotation. Verify by re-extracting image info and by rendering the page.

    '
- id: pdf-imgins-01
  answer: "Compute a rect that preserves aspect ratio and sits in free space, then insert.\n\n```python\n\
    import fitz\nfrom PIL import Image\n\ndoc = fitz.open(\"in.pdf\")\npage = doc[0]\nw, h = Image.open(\"\
    sig.png\").size\nW = 150.0\nH = W * h / w                      # keep aspect ratio\n\nmargin = 36.0\
    \                      # ~0.5 inch\nx1 = page.rect.x1 - margin         # right edge inside margin\n\
    y1 = page.rect.y1 - margin         # bottom edge inside margin\nx0 = x1 - W\ny0 = y1 - H\nrect = fitz.Rect(x0,\
    \ y0, x1, y1)\n\n# nudge up/left if it collides with text blocks\nfor b in page.get_text(\"blocks\"\
    ):\n    r = fitz.Rect(b[:4])\n    while rect.intersects(r):\n        rect.y0 -= 8; rect.y1 -= 8  \
    \ # or shift left; then recompute\n\npage.insert_image(rect, filename=\"sig.png\", keep_proportion=True,\
    \ overlay=True)\ndoc.save(\"out.pdf\", garbage=4, deflate=True)\n```\n\nNotes: `keep_proportion=True`\
    \ (default) fits the image inside the rect without distortion, so a correctly sized rect is enough.\
    \ Use the CropBox-aware `page.rect`; if the page is rotated, compute in unrotated coordinates or use\
    \ `page.derotation_matrix`. Check overlap against text blocks, drawings, and images (not just text).\
    \ If the available space is narrower/shorter than 150pt wide, reduce W and recompute H. Verify by\
    \ rendering the page and inspecting the corner.\n"
- id: pdf-imgins-02
  answer: "Reuse one image object instead of embedding the same bytes 200 times. When you call `page.insert_image(...)`\
    \ it returns the xref of the inserted image. Insert it once, capture that xref, then place it on the\
    \ remaining pages by passing the same `xref` rather than `filename`/`stream`:\n\n```python\nxref =\
    \ doc[0].insert_image(rect0, filename=\"logo.png\")\nfor page in doc[1:]:\n    page.insert_image(rect,\
    \ xref=xref)\n```\n\nAll pages then reference the same image XObject, so the file grows by a small\
    \ amount per page (just the placement/content), not by the full image each time. If your PyMuPDF build\
    \ deduplicates identical images by content hash on `insert_image`, passing the filename may already\
    \ collapse duplicates, but relying on `xref=` is the deterministic approach.\n\nAn alternative is\
    \ to create a small one-page PDF containing the logo and stamp it with `page.show_pdf_page(rect, logo_doc,\
    \ 0)` on every page; the form XObject is shared similarly (and PyMuPDF can reuse the generated form).\
    \ For repeating headers/watermarks, building one shared Form XObject is the canonical pattern.\n"
- id: pdf-imgins-03
  answer: 'Likely causes:


    - Drawing order: you used `overlay=False` (or drew the image, then a filled rectangle/background was
    painted later in the content stream). PyMuPDF''s `insert_image` defaults to `overlay=True`, so if
    the image is hidden the covering fill was likely appended after it or exists as an opaque Form XObject
    painted above.

    - Off-page/zero rect: the rect lies outside the CropBox or MediaBox (wrong coordinate space, or a
    CropBox smaller than the MediaBox), is zero/negative area, or is positioned in the page''s trimmed-away
    region.

    - Rotation: on rotated pages PyMuPDF coordinates are pre-rotation, so a rect computed from the *displayed*
    page lands off the visible area. Use `page.derotation_matrix` or compute against the unrotated `page.rect`.

    - Transform mismatch: the image was inserted with an unexpected rotation/mirror, or `keep_proportion`/aspect
    handling moved it outside the visible rect.

    - Opacity/alpha: the image is transparent or its alpha was set to 0, or it is black-on-black/white-on-white.

    - Background box: an existing opaque fill (a header band, table cell, or a white rectangle) covers
    it because the image was drawn underneath (overlay=False) or before the fill.

    - You saved the wrong document or didn''t save/flush; the viewer is showing a cached older file; or
    you inserted into a copy that wasn''t written to disk.


    Fixes: use `overlay=True`, ensure the rect is inside the CropBox and correctly derotated, draw after
    any background fills, confirm alpha/opacity, save to a new file, and render the page (`get_pixmap`)
    or reopen the saved file to confirm the image is actually present.

    '
- id: pdf-fonts-01
  answer: 'The name "AAAAAA+Georgia" marks an *embedded subset*: the font program contains only the glyphs
    that were used in the original document, and a custom cmap/CIDToGIDMap maps just those codes. There
    is no outline for a character that never appeared. If you insert new text using that subset, characters
    outside it have no glyph: they render as .notdef (blank boxes), get dropped, or map to the wrong glyph.
    Reusing the embedded font therefore fails specifically when the new text introduces previously-unused
    characters (and can also fail on encoding differences).


    What to do:

    - Prefer using the *original, full* font. If you have the licensed Georgia TTF/OTF, embed it yourself
    with `page.insert_font(fontname="F1", fontfile="Georgia.ttf")` (and its bold/italic faces) and use
    that fontname for the new text. The existing subset stays untouched for existing text.

    - You can extract what''s embedded with `doc.extract_font(xref)`; if it is a subset it will not contain
    the missing glyphs, so it is usually insufficient for new characters.

    - If the exact font is unavailable, choose a metrically/visually close substitute and embed it, or
    fall back to a base-14 font and accept substitution; match size/weight so the change is subtle.

    - Constrain the edit to characters already present in the subset if you must reuse it — but that is
    rarely acceptable.

    - Verify by re-extracting the text and by rendering to see no .notdef boxes, and respect the font''s
    license.

    '
- id: pdf-fonts-02
  answer: 'PyMuPDF''s built-in base-14 aliases:

    - Helvetica regular: `helv` (also accepts the full name "Helvetica")

    - Helvetica bold: `hebo` ("Helvetica-Bold")


    For completeness: `heit`/`hebi` are Helvetica oblique/bold-oblique; `tiro`/`tibo`/`tiit`/`tibi` are
    Times Roman/Bold/Italic/BoldItalic; `cour`/`cobo`/`coit`/`cobi` are Courier variants; `symb` is Symbol;
    `zadb` is ZapfDingbats. The full names ("Helvetica", "Helvetica-Bold", "Times-Roman", etc.) are also
    accepted.


    Limitation: base-14 fonts are not embedded — the viewer substitutes its own fonts, so rendering can
    vary. More importantly they use legacy single-byte encodings (WinAnsi/Standard/MacRoman, ~256 codes),
    so they cannot represent Unicode in general. Non-Latin scripts (CJK, Cyrillic, Greek, Arabic, Hebrew,
    Devanagari, emoji) and many special symbols are unavailable and appear as missing glyphs or fall back
    inconsistently. There is no proper subsetting, no OpenType shaping, and no reliable Unicode coverage.
    For any non-Latin or special characters you must embed a real TTF/OTF with `page.insert_font(fontfile=...)`
    and use that font name.

    '
- id: pdf-fonts-03
  answer: 'Detect the original styling from the page''s text structure, do not assume.


    Use `page.get_text("dict")` (or `"rawdict"`) and locate the span covering the header cell. Each `span`
    gives:

    - `span["size"]` — the font size in points (use this exactly).

    - `span["font"]` — the PostScript-ish name, e.g. "Georgia-Bold", "ArialMT", or a subset name like
    "AAAAAA+Georgia-Bold".

    - `span["flags"]` — bit field: 1 superscript, 2 italic, 4 serifed, 8 monospaced, 16 bold (so bold
    is quantifiable).

    - `span["color"]` — integer RGB.

    - `span["origin"]` and `span["bbox"]` — baseline and box, useful for matching position/leading.


    Then:

    - Insert with the matched `fontname`, `fontsize`, and `color`; align your text origin to the original
    `span["origin"]` (or use `TextWriter.append(pos, text, fontname=..., fontsize=...)`).

    - If the document''s bold face is embedded as a subset, extract it with `doc.extract_font(xref)`;
    if it lacks needed glyphs, embed the real bold TTF/OTF via `page.insert_font(fontname=..., fontfile=...)`.

    - Never rely on a base-14 `hebo` alias if the document uses a different family — that''s why weight/size
    look off.


    Verify: re-extract the edited region with `get_text("dict")` and confirm the new span reports the
    same font name, size, and flags/color; render the region to a pixmap and compare visually against
    a sibling header cell. `page.get_texttrace()` gives extra detail if the dict output isn''t enough.

    '
- id: pdf-verify-01
  answer: 'Reopen the *saved* file and inspect the artifact, not the script''s success.


    - Re-run the parser on the output (`fitz.open("out.pdf")`) and extract: `page.get_text()` / `get_text("words")`
    / `get_text("dict")` on the affected region. Confirm the new text is present, searchable, at the expected
    position/size/font, and that the old string no longer appears anywhere (`search_for(old)`).

    - For tables, confirm values in the right cells (extract by coordinates or use a table extractor)
    and that headers/rows line up.

    - Render the page/region to a pixmap at good DPI and look at it; compare with the original render
    (pixel diff, e.g., ImageMagick `compare`, or a mask) to confirm only the intended pixels changed.

    - Check file integrity with independent tools: `qpdf --check`, `pdfinfo`, `pdftotext`, `mutool info`,
    and open it in a second viewer.

    - Confirm structural expectations: page count unchanged, page size/CropBox unchanged, metadata sane,
    `doc.is_repaired` false, and no unintended change to images/links/annotations.

    - For redactions, prove the old content is gone — search text, and grep the raw/decompressed file
    (`strings`, `mutool`, or decompress streams) to ensure the confidential bytes don''t remain in any
    revision. If you saved incrementally, also check whether the earlier revision still holds the data.


    A passing script proves nothing; only inspection of the reopened output does.

    '
- id: pdf-verify-02
  answer: 'Not reliably — and with `incremental=True` to the same path, no, the data can still be in the
    file.


    `add_redact_annot` + `apply_redactions` removes the covered text/graphics from the *current* page
    content stream (and can remove images if configured), so the row usually stops rendering and stops
    being extractable in the latest revision. But an incremental save appends a new revision to the existing
    file; the original objects — including the prior content stream and the original page version — are
    still present earlier in the file. The confidential row can often be recovered by extracting the previous
    revision (e.g., `qpdf`, `mutool`, or reading the older xref/object versions). Metadata, other revisions,
    or copies inside Form XObjects/other pages can also retain it. Redaction also only affects the page
    content it covers; text drawn via an XObject, an annotation, or an image that wasn''t removed can
    survive.


    To actually remove it: save to a *new* file (not incremental) with garbage collection, e.g. `doc.save("clean.pdf",
    garbage=4, deflate=True, clean=True)`, which drops unreferenced/orphaned objects and rewrites the
    file without the stale revision; add `encryption`/permissions if appropriate. Then verify by searching
    the raw output for the sensitive string (decompressed) and by extracting text, and confirm with `qpdf
    --check`. If provenance or a prior revision must be preserved for legal reasons, handle that separately
    — but never assume an incremental redaction deleted the bytes.

    '
- id: pdf-verify-03
  answer: '- Never edit the original in place. Work on a copy; keep the original untouched and (ideally)
    read-only, and back it up before starting.

    - Verify you actually opened the copy: record the original''s hash/size, and confirm the original
    is byte-identical afterwards.

    - Save to a new filename first; only replace the original after you''ve validated the output, and
    do it atomically (write a temp file, then rename) so a crash can''t leave a half-written PDF.

    - Do not use `incremental=True` to the same path when removing content — it leaves the prior revision
    (and the old data) in the file. Save non-incrementally with `garbage=4, deflate=True, clean=True`
    to purge unused objects.

    - Preserve everything you weren''t asked to change: page count, page size, metadata, links, annotations,
    and form fields. Check whether the PDF is digitally signed — editing invalidates the signature, so
    flag that to the user rather than silently breaking it.

    - Validate the output: `qpdf --check`, `pdfinfo`, reopen with PyMuPDF (page count, `is_repaired`),
    extract text, render and visually confirm. Verify totals/invoice numbers/amounts are unchanged and
    that only intended edits were made.

    - Avoid leaking confidential data: after redaction/removal, search the raw file (decompressed streams)
    to confirm the old bytes are gone; scrub accidental metadata (author/producer) if required; don''t
    email or log the document contents.

    - Handle the file securely: sensible permissions, a clear output name (e.g., `invoice_edited.pdf`),
    and an audit note of what was changed, when, and by whom.

    - Confirm you are authorized to modify the document, and don''t overwrite the user''s only copy.

    '
- id: pdf-verify-04
  answer: 'Because "ran without errors" only means the process exited and no exception was raised — it
    says nothing about whether the PDF is semantically or visually correct. PDF libraries are deliberately
    tolerant and rarely validate the result.


    Common silent failures:

    - Text/footnote/image inserted outside the CropBox, off-page, or in the wrong coordinate space because
    the page is rotated or the CropBox differs from the MediaBox — no error, nothing visible.

    - New content drawn *underneath* an existing opaque fill/background, so it exists in the file but
    is invisible.

    - Text rendered with a substituted or wrong font, wrong weight, or missing glyphs (.notdef), because
    a base-14 fallback or an exhausted subset was used.

    - `insert_textbox` returning a negative value (text didn''t fit) while the code ignores the return;
    text is clipped or omitted.

    - The edit is an overlay, leaving old content (or old pixels) intact — the visible result may look
    fine while the file still contains the original data.

    - `doc.save(..., incremental=True)` leaving a prior revision with the stale content.

    - The script edited a different object/page than intended, saved to a different path, or saved nothing;
    a stale file is inspected.

    - The PDF is malformed but a viewer "repairs" it on open, hiding the problem; `doc.is_repaired` may
    be true.

    - The script prints "done" before the write is flushed or on a branch that skipped the work.


    Evidence of correctness requires reopening the output and checking the artifact: extract the text,
    confirm old content is gone and new content present at the right coordinates/font, render and diff
    pixels, run independent validators (`qpdf --check`, `pdfinfo`, another viewer), and check file size/page
    count. Only then is the PDF shown to be correct.

    '
