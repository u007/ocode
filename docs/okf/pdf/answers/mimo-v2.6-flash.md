- id: pdf-model-01
  answer: 'A PDF has no table object. It stores no rows, columns, or cells — only a

    page content stream of independent painting operators: text-showing

    operators (Tj/TJ) each with their own font, size, position and colour,

    plus graphics operators (re/f/S/f for ruling lines and shading fills).

    Row structure is implied purely by the geometric alignment of those

    separate objects. So "delete the third row" cannot be a structural

    operation; it must be reconstructed geometrically: find the y-band of the

    third row, redact/remove the glyphs and rules inside it, then shift

    everything below (text + lines + shading) up by one row height to close

    the gap, and finally repaint the shifted content if the underlying stream

    was modified. If the PDF was produced from a tagged source (e.g. an

    accessibility tag tree or an associated table structure) you may get real

    cell semantics, but that is metadata only — it does not drive rendering,

    so the geometry still has to be edited.

    '
- id: pdf-model-02
  answer: 'Because it changes only appearance, not content: the original characters

    are still in the content stream, still selectable, still copy-pasteable,

    still findable by search_for/extract, and still exposed to screen

    readers. The text layer lies about what is on the page, extraction

    returns stale values, and the redaction is trivially reversible by

    deleting the white rectangle''s drawing operators. It also breaks any

    downstream machine reading of the file. The correct approach is a real

    redaction: draw the annotation rect and call page.apply_redactions(),

    which removes the underlying characters (and, by default, images and

    vector graphics) inside the rect from the content stream before painting

    the white fill — so the data is genuinely gone — and then insert the

    replacement text at the right position with insert_text/insert_textbox

    using the matched font, size and colour.

    '
- id: pdf-model-03
  answer: 'Several things go wrong. (1) The visible string is not necessarily the

    bytes you see: text may be encoded with a CID/subset font where each

    glyph is an arbitrary 2-byte code, so "22.50" appears as codes like

    `\x005\x002\x00.\x005\x000` (or a custom/identity or even a per-document

    subset encoding), so a literal ASCII search finds nothing. (2) If it does

    match, the replacement must be the same byte length: streams use the

    /Length key, and TJ arrays use relative kerning adjustments; a length

    change desynchronises the stream and raises a decoding error or silently

    shifts subsequent operators. (3) Font subsetting: the glyph for the new

    digits may not even exist in the embedded subset, so the wrong or a

    missing glyph is rendered. (4) The string may be split across multiple

    show operators, or built by a Type3 font/ToUnicode mismatch. (5) The

    stream may be Flate-compressed, so raw byte search fails outright until

    you decompress, and re-compressing changes the bytes anyway. Correct

    approach: use a real PDF object/text API (pikepdf to rebuild the page

    content, or PyMuPDF''s span-level edit) so encoding, stream length and

    font coverage are handled for you.

    '
- id: pdf-model-04
  answer: 'PyMuPDF reports coordinates in a top-left origin, y increasing downward,

    measured in PDF user-space units (1/72 inch) — the opposite of raw PDF

    coordinates, which have a bottom-left origin with y increasing upward.

    Conversion for a page of height h is y_pymupdf = h − y_pdf (and

    vice-versa); x is the same. On a page with /Rotate, PyMuPDF exposes

    coordinates in the *rotated/visible* frame (the mediabox is adjusted to

    the view), so drawing with insert_text/insert_rect in that frame lands

    where the user sees it, but you must be careful mixing unrotated

    mediabox values from the raw dictionary. With a CropBox offset from the

    MediaBox, PyMuPDF''s page.rect is derived from the CropBox (clipped to

    the MediaBox), so all its coordinates are relative to the crop origin —

    any bbox you read from the raw dictionary (MediaBox-relative) will be

    offset by the CropBox''s lower-left corner and must be translated before

    use. Always take positions from page.get_text()/page.rect rather than

    from raw dictionary values, or normalise by rect.x0/rect.y0 explicitly.

    '
- id: pdf-locate-01
  answer: 'Three practical routes. (1) Rule/graphic detection: use

    page.get_drawings() to list every line/rect the page paints, filter for

    near-horizontal strokes at regular y intervals and near-vertical strokes

    at regular x intervals; the horizontal lines are the row separators and

    the vertical ones the column separators, so each row bbox is

    (x_table_left, y_top_line) → (x_table_right, y_bottom_line) and cells

    are the intersections. Shaded rows are filled rects from the same call.

    (2) Text-grid clustering: page.get_text("words") or

    page.get_text("dict") gives per-word/line bboxes; cluster lines by

    their y band and columns by x-gap histograms to synthesize a row/column

    grid, then intersect. (3) Ready-made detectors: pdfplumber

    (page.find_tables() → table.bbox, rows, cols), camelot-py (read_pdf with

    flavor="stream"/"lattice"), pdfminer''s LAParams, or PyMuPDF''s

    page.find_tables() (TableFinder returns .bbox, .rows[].bbox and each

    cell''s .bbox) — these combine the line and text approaches and hand back

    explicit row/cell rectangles.

    '
- id: pdf-locate-02
  answer: 'search_for returns one rect per *occurrence of that exact string* — here

    a narrow box around "Widget C" in the label column only. It does not

    know about the table: it misses the rest of the row (the other cells''

    numbers), the horizontal ruling lines above/below, any background

    shading, and it would also match the same text anywhere else on the page.

    To get the full row region you must grow the hit into the table grid:

    take the hit''s y0/y1, find the nearest horizontal ruling lines (or the

    neighbouring line-rows) immediately above and below via

    page.get_drawings(), and take the table''s full x extent (leftmost to

    rightmost vertical rule, or the table bbox from page.find_tables()).

    The row rect is then (table.x0, y_rule_above, table.x1, y_rule_below).

    If there is no ruling, cluster all words whose y-bands fall within the

    same line-row and take the union of their bboxes, padded to the table''s

    column edges. Verify there is exactly one such row before redacting.

    '
- id: pdf-locate-03
  answer: 'Use the structured text API, not "words": page.get_text("dict") (or

    "rawdict" for per-character boxes) returns blocks → lines → spans, each

    span carrying font (name, e.g. "Helvetica-Bold" plus subset prefix like

    "ABCDEF+"), size, color as a single int (0xRRGGBB; PyMuPDF also

    documents the PDF-style 0x00RRGGBB packing), and origin (the actual

    text baseline insertion point) as well as bbox. So for the target cell,

    locate the span whose bbox contains/nearest-neighbours your search hit

    and read span["font"], span["size"], span["color"], span["origin"][1]

    (baseline y) and span["ascender"]/["descender"] if you need exact

    metrics. Match by nearest origin.y rather than exact equality, since

    baseline rounding varies. To reproduce it exactly you must also ensure

    the same font is available to insert_text — PyMuPDF only embeds

    Base-14 fonts by default, so for a custom/embedded face you need

    page.insert_font(fontname=..., fontbuffer=extracted_buffer) first, and

    fall back to a metrically similar Base-14 font otherwise.

    '
- id: pdf-locate-04
  answer: 'Both come from geometry already on the page. Column boundaries: from the

    vertical ruling lines in page.get_drawings() (x of each near-vertical

    stroke), or the table''s column x-list from page.find_tables() /

    pdfplumber''s table.cols / camelot''s table.cols; if there are no

    verticals, infer them from the x-gaps between words in the header row.

    The x where a right-aligned number ends: that is the right edge of its

    text — get it per string from page.search_for(value)[0].x1, or from the

    word/span bbox x1 in get_text("words")/get_text("dict"). Because the

    right edge sits on the cell''s inner padding, record the offset

    (column_right_edge − text.x1) once and reuse it, or simply place the

    new text so its right edge equals the old text''s x1 (insert with a

    right-aligned anchor computed from font text length via

    fitz.get_text_length(text, fontname, size)). PyMuPDF also offers the

    align parameter of insert_textbox (TEXT_ALIGN_RIGHT) anchored at the

    column rect, which does this for you.

    '
- id: pdf-rowdel-01
  answer: "1. Load: doc = fitz.open(path); page = doc[0].\n2. Locate the table and the target row: use\
    \ page.find_tables() for the\n   table bbox/rows, or build the row grid from get_drawings()\n   (horizontal\
    \ rules) + get_text(\"words\") (cell text), as in\n   pdf-locate-01/02. Confirm exactly one row matches\
    \ \"row 3\".\n3. Record row metrics: y_top, y_bottom, height h = y_bottom − y_top,\n   the shading\
    \ rect (if any) and the table's x0/x1.\n4. Find everything below the row that belongs to the table:\
    \ all rules,\n   shading fills and text lines with y > y_bottom, up to and including\n   the Total\
    \ row and its rule. Build that set explicitly (drawings whose\n   bbox intersects the x-range and\
    \ y >= y_bottom) so you don't drag\n   unrelated footnotes.\n5. Redact the deleted row: add redaction\
    \ annotations over\n   (x0, y_top, x1, y_bottom) covering the text, ruling lines and shading\n   for\
    \ that row only, then page.apply_redactions(images=PDF_REDACT_IMAGE_REMOVE, graphics=PDF_REDACT_LINE_ART_REMOVE,\
    \ text=PDF_REDACT_TEXT_REMOVE) so the glyphs and lines genuinely disappear.\n6. Move the region up\
    \ by h: for each remaining element, the reliable\n   trick is snapshot-and-repaint — capture the region\
    \ below as a new\n   page (page.get_pixmap / or better, copy the drawings list and text\n   spans),\
    \ then delete those operators and re-insert with a -h y offset.\n   In practice: collect drawings\
    \ (page.get_drawings()) and text spans\n   below the row, remove them (redaction rects or by rebuilding\
    \ the\n   content stream), then redraw each at y − h with the same stroke\n   colour/width, fill,\
    \ font, size and colour, using page.draw_line /\n   page.draw_rect / page.insert_text. PyMuPDF ≥1.24\
    \ has\n   page.add_redact_annot + a \"move\" alternative and also the\n   show_pdf_page trick (see\
    \ pdf-rowdel-03) which is far safer.\n7. Update the Total: search_for the old total, read its span\
    \ style, and\n   replace it with the recomputed value, right-aligned to the original\n   x1 so digits\
    \ line up (see pdf-cell-01).\n8. Check for other references: page numbers, \"continued\" notes, any\n\
    \   repeated Total/subtotal above the deleted row, and any second page\n   carrying the table.\n9.\
    \ Save with garbage/deflate: doc.save(out, garbage=3, deflate=True)\n   (not saveInPlace unless incremental\
    \ is intended), then re-open and\n   visually verify.\n"
- id: pdf-rowdel-02
  answer: 'Defaults: images=PDF_REDACT_IMAGE_REMOVE (0), graphics=

    PDF_REDACT_LINE_ART_REMOVE (0) — i.e. any image pixels whose area

    overlaps the rect are removed, and any vector line art that overlaps the

    rect is removed. (text defaults to PDF_REDACT_TEXT_REMOVE too.) That

    matters hugely for tables: ruling lines and zebra shading are vector

    graphics, so a redaction rect drawn only for the cell text will also

    delete the row''s horizontal rules and any shaded background it touches —

    and if the rect reaches the neighbouring cell it can delete part of the

    neighbouring vertical rule or even an overlapping logo image. You must

    therefore size the rect to exactly the cell interior (inset from the

    rules), or explicitly pass graphics=PDF_REDACT_KEEP_LINES

    / images=PDF_REDACT_IMAGE_NONE when you want lines preserved, and then

    repaint any rules you intentionally removed. Conversely, when deleting a

    whole row you *want* the default line-art removal so the rules and

    shading go with it — but you must then redraw the remaining grid.

    '
- id: pdf-rowdel-03
  answer: "Yes. The clean method is to treat the untouched lower part of the page as\nan image-of-a-page\
    \ and re-show it shifted:\n(a) Build a temporary PDF containing just the region below the deleted\n\
    \    row (e.g. create a new doc/page, use show_pdf_page with a clip rect\n    of the source page,\
    \ or copy the source and crop it), then\n    page.show_pdf_page(target_rect, src_doc, src_page, clip=lower_rect)\n\
    \    with target_rect = lower_rect translated up by the row height. This\n    reproduces glyphs, rules,\
    \ shading and images exactly, with no\n    retyping and no font-embedding problems.\n(b) Alternatively\
    \ use PyMuPDF's redaction-with-move pattern: after\n    apply_redactions, re-insert the preserved\
    \ region as a Form XObject.\n    PyMuPDF exposes page.show_pdf_page / page.new_shape and, for full\n\
    \    fidelity, pdf_page objects can be imported as XObjects via\n    doc.xref_get_key / page.get_contents,\
    \ so the moved block stays\n    vector, selectable text.\n(c) If the source page itself was generated\
    \ by you, the simplest exact\n    move is to rewrite the content stream's text/drawing matrix ty by\n\
    \    -h for that band (a translate on the CTM), which shifts everything\n    without rasterising.\n\
    Caveat: show_pdf_page re-shows the source page, so if the deleted row's\nglyphs still exist underneath\
    \ you must redact them first, and you should\navoid the moved region overlapping itself (clip precisely\
    \ to the row\nboundary so no text is duplicated or cut).\n"
- id: pdf-rowdel-04
  answer: 'Everything derived from the table: (1) the Total/subtotals, and any tax,

    discount or percentage computed from it; (2) the ruling lines and any

    zebra shading, since the grid must be re-closed and the lines below

    shifted up; (3) anything below the table — footnotes, "items: N" counts,

    page footers, "continued on page 2", totals repeated on a summary page;

    (4) references elsewhere in the document: a cover summary, an

    accompanying email/JSON, a page count or section reference; (5) if the

    table crosses a page boundary, the carried-forward header/total on the

    next page and the running page count; (6) internal identifiers — row

    numbers (1,2,3…) that must be renumbered, and any cross-reference anchors

    or PDF outlines/bookmarks pointing at that region; (7) accessibility

    metadata: tag tree /ActualText /Alt text and any associated structure

    that still describes the removed row. Also verify no digital signature

    or certification is invalidated by the modification.

    '
- id: pdf-cell-01
  answer: "1. doc = fitz.open(in); page = doc[0].\n2. rects = page.search_for(\"22.50\"); pick the one\
    \ inside the target cell\n   (there may be several occurrences).\n3. Read the old style: locate the\
    \ span in page.get_text(\"dict\") whose\n   bbox contains that rect — record font, size, color and\
    \ origin\n   (baseline). If the font is embedded/custom, extract its buffer and\n   page.insert_font(fontname=oldname,\
    \ fontbuffer=buf); otherwise pick a\n   matching Base-14 (Helvetica/Times/Courier + Bold/Oblique).\n\
    4. Redact: page.add_redact_annot(rect, fill=(1,1,1)) — or fill=None to\n   leave the background untouched\
    \ for shaded cells; use a rect\n   tightened to the cell interior so neighbouring rules and glyphs\
    \ are\n   not caught (see pdf-cell-02). Then page.apply_redactions() with\n   defaults (text remove,\
    \ images remove, line art remove) — or\n   graphics=PDF_REDACT_KEEP_LINES if the rules pass through\
    \ the rect.\n   If the cell is shaded, note the shading may be removed: repaint it\n   with page.draw_rect(...,\
    \ color=None, fill=shading_colour) at the\n   cell rect.\n5. Position the new text right-aligned to\
    \ the old x1: compute\n   w = fitz.get_text_length(\"54.00\", fontname, size); origin =\n   (old_x1\
    \ − w, old_baseline_y). Do NOT use the bbox top-left.\n6. page.insert_text(fitz.Point(origin), \"\
    54.00\", fontname=..., fontsize=size, color=rgb_from_int(old_color), overlay=True).\n   (Or page.insert_textbox(cell_rect,\
    \ \"54.00\", ..., align=fitz.TEXT_ALIGN_RIGHT, fontsize=size) which right-aligns for you —\n   watch\
    \ the fit return value.)\n7. Sanity-check with page.get_text() that the new value is the only\n  \
    \ match, then doc.save(out, garbage=3, deflate=True) and re-open to\n   visually verify alignment.\n"
- id: pdf-cell-02
  answer: 'Size it as exactly the text region, not the whole cell and not a loose

    box: fit the characters'' union bbox (from search_for or the span bbox),

    optionally padded by ~1 pt horizontally, and inset vertically to the

    ascender/descender so you don''t cross into the ruling lines above or

    below. Reason: removal is by *bbox overlap*, so any character whose box

    intersects the rect is deleted — in a tight table, cells are only a few

    points apart and the rules run through the row, so an over-generous rect

    will silently delete the neighbouring cell''s first/last glyph, the

    column divider, or part of the row''s shading/line art (the defaults also

    remove images and vector graphics that overlap). Too *small* is also

    risky: a rect that clips a glyph''s bbox can leave that character''s

    portion behind (or, with text=PDF_REDACT_TEXT_REMOVE, delete it but

    leave an orphaned piece of an adjacent ligature). Practical rule: build

    the rect from the union of the per-character rects of exactly the string

    you''re replacing, expand a hair horizontally, and shrink it to stay

    inside the cell''s rule-to-rule vertical band — then confirm with a

    re-extract that nothing else vanished.

    '
- id: pdf-cell-03
  answer: 'insert_textbox returns a float: the *unused* vertical space (≥ 0) on

    success, or a **negative number** when the text does not fit. On failure

    PyMuPDF draws **nothing** (the whole call is discarded — no partial text

    is written), so a negative return means your cell is still empty and you

    must handle it. Options: (1) lower fontsize — ideally in a loop

    (size -= 0.25) until the return becomes non-negative, optionally

    auto-shrinking only as far as needed; (2) widen the rect horizontally

    into the cell''s padding (and/or reduce the leading) if the box, not the

    glyphs, is the constraint; (3) wrap the text with insert_textbox''s

    natural wrapping, or insert a manual line break and give the box more

    height; (4) for a single line, use insert_text with a computed origin

    and, if you must, a horizontal squeeze — PyMuPDF has no built-in

    condensation, so you can scale by re-inserting with a smaller size or by

    using shape.insert_textbox and transforming; (5) abbreviate/round the

    value; (6) at extreme overruns, merge cells or re-lay out the table

    (pdf-relayout-02). Always check the return value — ignoring it is the

    classic silent-failure bug.

    '
- id: pdf-cell-04
  answer: '`point` is the *text origin*: the point on the **baseline** where

    writing starts — the pen position for the first glyph, with y on the

    baseline and x at the left edge of the first character (for a horizontal

    writing direction). It is NOT the top-left of the text''s bounding box.

    If you pass the bbox''s top-left corner, the text is drawn with its

    baseline there, so the glyphs sit *above* the box — the new value floats

    one line-height too high, misaligned with its row and possibly

    overlapping the rule or the row above. Correct handling: take the old

    span''s `origin` (which PyMuPDF gives you as the true baseline point) and

    keep it, only adjusting x for right alignment; or convert a bbox to an

    origin using the font''s ascender, e.g. origin_y ≈ bbox.y0 + size *

    ascender (PyMuPDF spans expose ascender/descender, and

    span["origin"] is the reliable source). Also remember origin.y is in

    PyMuPDF''s top-left coordinate system, so don''t flip it yourself.

    '
- id: pdf-relayout-01
  answer: "1. Measure the gap: locate the new row's intended slot — the y-band\n   between the current\
    \ last data row's bottom rule and the Total row's\n   top rule; let the new row height be h (match\
    \ a data row).\n2. Shift everything from the Total row downward by +h: the Total row's\n   text, its\
    \ top/bottom rules and shading, plus all content below the\n   table (footnotes, footer, page numbers)\
    \ — either by translating the\n   content-stream region or by the snapshot-and-reinsert technique\
    \ in\n   pdf-rowdel-03, this time moving *down*. Content that would run past\n   the page bottom means\
    \ the table must move to a new page instead.\n3. Create the new slot: paint a background fill (if\
    \ rows are shaded) and\n   draw the two horizontal rules bounding the new row with the same\n   stroke\
    \ colour, width and dash as the existing ones; ensure vertical\n   column rules extend through the\
    \ new band.\n4. Populate the cells: insert each value with the row's font/size/colour\n   at the correct\
    \ baseline y, right-aligning numbers to their column's\n   established x1 and text to the column's\
    \ x0, so the grid lines up.\n5. Update the Total: recompute it, replace the old value (redact +\n\
    \   insert) keeping its baseline and right-alignment, and update any\n   associated tax/subtotal.\n\
    6. Renumber/check: row numbering, item counts, \"N items\" text, repeated\n   headers/footers, cross-page\
    \ continuations, and any bookmarks/anchors.\n7. Save with garbage/deflate and verify visually that\
    \ rules meet cleanly\n   (no doubled or gapped lines) and nothing collides below.\n"
- id: pdf-relayout-02
  answer: 'Correct approach: treat it as a full re-layout. Re-measure the table''s

    column grid from scratch — get the table bbox and the page margins, lay

    out n+1 column boundaries within that same total width (typically by

    shrinking existing column widths, or taking width from the widest/

    least-constrained column, always keeping the table''s x0/x1 unchanged so

    it still spans margin-to-margin) — then reposition *every* cell''s text

    in every row (each number re-right-aligned to its column''s new x1, each

    label re-left-aligned to its new x0), and redraw the entire rule grid

    (all verticals and horizontals) to the new boundaries. In other words:

    move the existing content, don''t just add a column''s worth of text.

    Common mistake: simply inserting the new column''s header and values at

    the right edge (or between two existing columns) without shifting/

    compressing anything — this overlaps or pushes text into neighbouring

    cells, runs the table past the right margin, and leaves the old vertical

    rules in the wrong places, so the new column has no real boundary. A

    related mistake is adding the column only in the data rows while the

    header row, shading rects and rules still reflect the old grid. If

    shrinking is impossible (content too wide), the honest options are

    reducing font size, shortening labels/abbreviating, landscape

    orientation, or splitting the table — not squeezing text past its rules.

    '
- id: pdf-relayout-03
  answer: 'Acceptable only when: (a) fidelity is not critical (draft/internal copy, not archival or print-master);
    (b) the document is text-heavy with simple, linear layout — single-column flow, simple tables, no
    complex vector artwork; (c) you can tolerate re-pagination (page count and breaks changing); (d) a
    re-editable source exists, so this is a stopgap rather than the only copy; (e) you will visually diff
    every page afterward and the lossy round-trip is explicitly approved.


    Risks: fonts are substituted (metrics change → line wraps, table column widths, and page breaks all
    shift); text reflows so tables may split, wrap, or lose merged-cell structure; vector drawings, line
    art, and precise rules can be approximated or lost; images are re-encoded (quality loss, color space/CMYK
    changes, transparency flattened); PDF-specific features are dropped or broken — form fields, annotations,
    links/bookmarks, optional-content layers, tags/accessibility structure, PDF/A compliance, embedded
    files, signatures; headers/footers that were positioned absolutely can collide with reflowed body
    text; special characters, ligatures, and subset-font glyphs can be mangled (tofu/missing glyph boxes);
    metadata and print/pagination intent are lost. In short: the second conversion rarely reproduces the
    first PDF — treat it as a regeneration, not a round-trip, and only use it when the source-of-truth
    document is the editable format.

    '
- id: pdf-relayout-04
  answer: "Do it geometrically, on the actual page coordinate system:\n\n1. Get the page box: `page.rect`\
    \ (MediaBox/CropBox in PyMuPDF — note rotation: use `page.rotation_matrix`/`page.derotation_matrix`\
    \ if the page is rotated so extracted bboxes and your target rect are in the same frame).\n2. Inventory\
    \ everything already on the page, each as a bbox:\n   - text: `page.get_text(\"blocks\")` or `page.get_text(\"\
    dict\")` → block/line/span bboxes (use blocks, not lines, for occupancy);\n   - vector graphics: `page.get_drawings()`\
    \ → each path's `rect`;\n   - images: `page.get_image_rects(xref)` (and any full-page background rect);\n\
    \   - annotations/widgets: `page.annots()` / `page.widgets()` bboxes;\n   - headers/footers/running\
    \ rules that repeat per page.\n3. Compute the candidate rect for the new content (table top y, bottom\
    \ y = top + needed height, left/right from the text column).\n4. Assert non-intersection: for every\
    \ occupied rect `r`, require `not (candidate & r)` (empty intersection). Check against the rect of\
    \ the *next block down* (the thing you would overlap when growing downward), and against the page\
    \ bottom margin / footer region, not just the page edge.\n5. Add safety padding: extracted text bboxes\
    \ can under-report descenders, italics, and stroke width of rules, so pad ~1–2 pt (more for leading)\
    \ and keep the normal page margins.\n6. When growing an existing table, compute the current table's\
    \ bbox (union of its cell rules/text), add the new row height, and re-run the intersection test against\
    \ everything below it.\n7. Cross-check visually: `page.get_pixmap(dpi=150)` and confirm the whitespace\
    \ region really is blank (catches content the extractor misses — e.g. inline images, Form XObjects,\
    \ clipping groups, annotations).\n\nIf nothing intersects and margins hold, there is room; otherwise\
    \ you must reflow, shrink, or move content.\n"
- id: pdf-tblins-01
  answer: 'Two practical Python approaches:


    (1) Draw it directly with PyMuPDF (single pass, in place).

    Compute the target rect from free space (blocks/drawings/images bboxes), then either use PyMuPDF''s
    built-in table insertion if available (`page.insert_table(rect, data=[["Header1","Header2"],["a","b"],["c","d"]],
    ...)`), or draw it manually: for each row/column compute cell rects, draw horizontal/vertical rules
    with `page.draw_line(...)` / `page.draw_rect(..., color=..., width=0.5)`, and place text with `page.insert_text((x,
    y), text, fontname=..., fontsize=...)` or `page.insert_textbox(cell_rect, text, align=...)`. Extract
    the fonts/colors/sizes from neighboring content first so the table matches. Save with `doc.save(out,
    deflate=True)`.


    (2) Build the table separately and stamp it on (two-document overlay).

    Render the table in ReportLab platypus (`Table` + `TableStyle` for grid, header font, padding) into
    a small one-page PDF sized to the target rect, then composite that page onto page 1 of the original:

    - via pypdf/PyPDF2: `overlay.pages[0].merge_page` / `PageObject.merge_page(orig_page, over=True)`;
    or

    - via PyMuPDF: `page.show_pdf_page(rect, overlay_doc, 0, keep_proportion=True)`.


    ReportLab gives much finer table control (borders, per-cell padding, wrapping, alignment) while PyMuPDF
    keeps you in one file; the overlay route is best when the table is complex or you want ReportLab''s
    layout engine.

    '
- id: pdf-tblins-02
  answer: 'Match the existing table/document, then avoid rendering artifacts:


    Sampling first: pull the old table''s properties from the page — fonts (`page.get_text("dict")` spans:
    `font`, `size`, `flags`, `color`), rule stroke color/width from `page.get_drawings()` (pen width and
    RGB of the existing cell lines), cell padding (compare text origin to cell rule), column alignment,
    and row height.


    Consistency:

    - Use the same embedded font (extract it with `doc.extract_font(name)` and re-embed as a new fontname
    if it is a subset) or a close metric match; same size, same bold/italic variant, same text color (`color=`
    from the sampled span).

    - Same rule style: same stroke width (usually 0.5–1 pt), same color, same full-grid vs. header-only-horizontal-line
    convention, same header treatment (bold, maybe light fill).

    - Same padding/margins: align left edge to the text column''s left edge, keep the same cell inset
    (e.g. 3–4 pt), same row height rhythm, same alignment per column (numbers right, text left).

    - Keep the table within the column width so it lines up with paragraphs above/below.


    Avoiding glitches:

    - Draw rules on half-point coordinates (x+0.5) at 1 pt width so they render as crisp 1-device-pixel
    lines instead of blurry 2-pixel smears; or use exact integer coords with thin strokes consistently.

    - Don''t overdraw: don''t draw both a full outer rect and four lines that double up; avoid 0-width
    or hairline strokes that vanish at low zoom.

    - Keep everything inside the page rect and don''t let strokes straddle the table edge; respect clipping
    paths (don''t draw inside an existing clip group).

    - Don''t overlap existing content; ensure text baselines fit the row (use `insert_textbox` and check
    its return value — a negative return means overflow/clipping).

    - Use overlay drawing (paint order on top), same color space (avoid a CMYK-vs-RGB color shift), and
    don''t introduce transparency/opacity differences.

    - Re-render the page to a pixmap at 150–300 dpi and eyeball it for misalignment, doubled lines, and
    clipped descenders.

    '
- id: pdf-tblins-03
  answer: 'Options, roughly in order of preference:


    1. Fix it at the source: edit the original authoring document (Word/LaTeX/Markdown/HTML) and re-export
    the PDF. Always the cleanest answer.

    2. Push the following content down (translate it): split page contents at the insertion point and
    prepend a translation to the downstream stream — with PyMuPDF, `contents = page.read_contents()`,
    wrap the portion after the target in `q ... cm 0 dy 0 ...` (a `cm` translate before the ops), then
    `page.swap_content_of_page(...)`. Everything below the insertion point moves down by the table height;
    then check it doesn''t run off the page bottom (if it does, split content across to a new page or
    shrink).

    3. Make room by compressing: reduce line spacing / font size slightly of the surrounding paragraphs,
    trim the paragraph, or reduce the new table''s row heights/padding so it fits in existing leading
    or margin whitespace.

    4. Scale the existing page content slightly (a uniform `cm` scale about the top of the insertion region)
    to free vertical space — risky for consistency, acceptable in a pinch; remember all coordinates, rule
    widths, and font sizes scale too.

    5. Move the table: put it in real free space (bottom of page, a sidebar area) and reference it ("see
    table below/overleaf"), or place it on a new page inserted after this one (`doc.new_page(pno+1)`).

    6. Rework pagination: insert a page and reflow the tail content so the table sits in natural flow
    — more work, but avoids overlap.

    7. Last resort: draw the table over an existing element only if you simultaneously delete/redact that
    element — overlapping old content is never acceptable.


    Any option that shifts content requires re-verifying that nothing overflowed the page bottom or collided
    with headers/footers.

    '
- id: pdf-imgrep-01
  answer: 'Steps in PyMuPDF:

    1. Locate it: `info = page.get_images(full=True)` → xref is `info[0][0]`; then `rects = page.get_image_rects(xref)`
    gives every placement of that image on the page (an image xref can be drawn more than once). Pick/confirm
    the rect at the logo position.

    2. Replace in place: `page.replace_image(xref, filename="new.png")` (also accepts `stream=...` bytes),
    which keeps the existing placement/dimensions; or delete + re-insert: `page.delete_image(xref)` then
    `page.insert_image(old_rect, filename="new.png", keep_proportion=True, overlay=True)`.

    3. `doc.save(out, deflate=True)` to a NEW file (or `incremental=True` on the original) and re-open
    to verify `page.get_image_rects()` shows the new rect and the render looks right.


    Catch with shared images: an image xref is a document-level object — the same xref may be referenced
    by other pages, by more than one placement on this page, or from inside a Form XObject. Replacing/deleting
    it changes (or removes) the logo EVERYWHERE it is used, not just on page 1. Also, if the "logo" is
    drawn inside a Form XObject, `page.get_images()` may be empty and `replace_image` won''t find it.
    So before touching it: enumerate all usages (`page.get_image_rects(xref)` per page, plus `page.get_xobjects()`),
    decide whether the shared usage is intended, and if the pages must differ, give page 1 its own new
    xref (`page.insert_image(...)` after removing the reference) rather than mutating the shared one.

    '
- id: pdf-imgrep-02
  answer: 'Nothing is visually wrong, but the edit is only cosmetic — the old logo is still in the file:


    - The original image bytes remain in the PDF (old xref still in the object table), so anyone can extract
    the replaced/confidential logo with any PDF tool (`page.get_images()`, pdfimages, PyMuPDF `extract_image(xref)`).

    - File size still grows; you now carry both images.

    - Extraction/OCR/search-layer results can still surface the old content; the old image may also remain
    in the document thumbnail/XObject resources.

    - If the new PNG has any transparency, the old logo shows through; if it is opaque you are just hiding
    it.

    - It is a cover-up, not a replacement — the correct operation is to remove the old object, not paint
    over it.


    Fix: delete or redact the old image — `page.delete_image(xref)` (or `page.add_redact_annot(rect);
    page.apply_redactions(images=fitz.PDF_REDACT_IMAGE_REMOVE)`) — then insert the new one at that rect;
    save non-incrementally with garbage collection (`doc.save(out, garbage=4, deflate=True)`) so the old
    object is actually purged, then re-verify by extracting images and confirming the old one''s hash
    is gone.

    '
- id: pdf-imgrep-03
  answer: 'Causes for `page.get_images()` being empty while a logo is visible:


    - The logo lives inside a Form XObject (grouped content). `get_images()` only lists images in the
    page''s own resources; recurse via `page.get_xobjects()` (or `page.get_resources()`) and list images
    in the form''s resources, then get its placement with the form''s matrix/clip rect (or by reading
    the content stream''s `cm` before `/Fm`).

    - It is an inline image (`BI ... ID <bytes> EI`) embedded in the content stream — inline images never
    appear in `get_images()`. Find it by reading `page.read_contents()` for `BI/ID/EI` (and its preceding
    `cm` transform).

    - It is actually vector art (logo flattened to paths/fills) — then there is no image object at all;
    it is drawings (`page.get_drawings()`).

    - It is not on the page but inside an annotation''s appearance stream (a stamp/comment/logo widget)
    — check `page.annots()` and their `/AP` streams.

    - It is painted via a pattern, an XObject on another page referenced through a group, or hidden in
    an optional-content group; or you are calling `get_images()` on a different page than the one displayed
    (shared pages / duplicate pages).


    How to find and replace it:

    1. Enumerate everything: `page.get_images(full=True)`, `page.get_xobjects()`, `page.annots()`, `page.get_drawings()`,
    and grep the content stream (`page.read_contents()`) for `Do`, `BI`, `cm`.

    2. Pin down which operator paints the logo by rendering a pixmap and/or progressively testing (e.g.
    delete the xref and re-render to see what disappears); get its rect from the transform matrix or `page.get_image_rects(xref)`
    for nested images.

    3. Replace: mutate the nested image xref (`page.replace_image` after resolving it), or replace the
    form''s stream, or rewrite the content stream''s inline-image bytes (`page.swap_content_of_page()`
    after editing the stream), or (for annotation logos) replace the appearance stream. If it is vector,
    you must delete the drawing ops and re-insert an image at that rect.

    4. Save to a new file and re-render to confirm the change and that nothing else moved.

    '
- id: pdf-imgins-01
  answer: '1. Get the PNG''s pixel dimensions to keep aspect ratio: `pix = fitz.Pixmap("sig.png"); w,
    h = pix.width, pix.height` (or PIL: `Image.open(...).size`), so `ratio = h / w`.

    2. Choose width 150 pt → `height = 150 * ratio`.

    3. Anchor bottom-right with a margin, e.g. `m = 36` (0.5 in): `x1 = page.rect.width - m; x0 = x1 -
    150; y1 = page.rect.height - m; y0 = y1 - height` → `rect = fitz.Rect(x0, y0, x1, y1)` (flip y direction
    if anchoring to a custom spot: signature baseline near the bottom margin).

    4. Collision check before inserting: build occupied rects from `page.get_text("blocks")`, `page.get_drawings()`,
    `page.get_image_rects(...)`, `page.annots()`; if `rect & occupied` is non-empty, move the rect up
    (or shrink width) until it clears — e.g. `y1 = min(y1, topmost_blocked_y0 - 4)` — or place it below
    the last content block. Also keep it inside `page.rect` margins.

    5. Insert: `xref = page.insert_image(rect, filename="sig.png", keep_proportion=True, overlay=True)`
    — `keep_proportion=True` guarantees aspect ratio even if the rect isn''t exact; `overlay=True` puts
    it on top.

    6. Verify the actual drawn area: `page.get_image_rects(xref)` (the used rect can differ slightly from
    the requested one when keeping proportion), and re-render the page to a pixmap to confirm no text
    is covered.

    '
- id: pdf-imgins-02
  answer: 'Insert the image once and reuse its xref on every page instead of embedding it 200 times:


    - On the first page: `xref = page0.insert_image(rect, filename="logo.png")` — this creates one image
    object in the document.

    - On all other pages: `page.insert_image(rect, xref=xref, keep_proportion=True, overlay=True)` — PyMuPDF
    supports passing an existing `xref`, so each page only adds a small reference/placement, not a copy
    of the bytes.


    Alternatives that also store one copy:

    - Put the image inside a Form XObject once and invoke that form on each page (PyMuPDF does this for
    you if you use `page.show_pdf_page()` from a single-page "stamp" document, or via `page_wrap_contents`-style
    grouping): one image object, N tiny references.

    - Build a one-page stamp PDF containing the logo and call `page.show_pdf_page(rect, stamp_doc, 0,
    ...)` for each page.


    Also worth doing: keep the PNG optimized/downscaled to the rendered size (150 dpi at display size
    is plenty), strip metadata, and avoid `insert_image` in a loop with `filename=` (that re-embeds each
    time). Result: file grows by ~1 image plus per-page placement data, instead of 200 copies.

    '
- id: pdf-imgins-03
  answer: 'Likely causes:


    Paint order / z-order (most common):

    - The image was inserted with `overlay=False`, so it is painted before (underneath) existing content.

    - A filled background rectangle or other drawing is painted AFTER the image in the content stream,
    covering it (content streams paint strictly in order — a later opaque fill hides everything below).


    Geometry:

    - The rect has zero or near-zero size (e.g. you passed a point/degenerate rect, or computed height
    from a wrong aspect ratio → `height = 0`), so nothing visible is drawn.

    - The rect is off-page or outside the visible CropBox/MediaBox (wrong coordinate origin, forgetting
    `page.rect` vs. paper size, or not accounting for page rotation — the image lands outside the visible
    area).

    - The image is scaled down to sub-point size, or is white-on-white / fully transparent / an empty
    PNG (alpha channel makes it invisible), or a CMYK-vs-RGB mismatch makes it near-invisible.


    Clipping / structure:

    - It was inserted inside an existing clipping path or inside a Form XObject with a clip, so only a
    sliver (or none) shows.

    - It was drawn into an optional-content layer (OCG) that is hidden, or onto a hidden/print-excluded
    layer.

    - The region is covered by an annotation appearance or an overlaying page box/background.


    Process mistakes:

    - Inserted on the wrong page index (`doc[0]` vs. the page you looked at), or into a different document
    object than the one saved.

    - Not saved/flushed: changes live only in memory, or you saved to a different filename than the one
    opened/viewed (or the viewer is showing a cached copy).

    - Saved with `incremental=True` to a file that some readers fail to overlay correctly; or the page
    contents need `page.clean_contents()`/`swap_content_of_page()` after manual stream edits.


    Debug: check the returned xref and `page.get_image_rects(xref)` (empty/zero-size rect → geometry),
    render `page.get_pixmap()` and inspect, and read the content stream tail to see which ops are painted
    after the image''s `Do`.

    '
- id: pdf-fonts-01
  answer: 'Why it fails: an embedded font in a PDF is normally a SUBSET — only the glyph outlines actually
    used when the document was created are embedded (that''s what the `AAAAAA+` prefix signals, along
    with the `/CharSet` entry or the subset tag in the font program). The font''s encoding/CMap and the
    ToUnicode CMap likewise only map the characters present. When you insert new text using that same
    font object, any letter that never appeared has no outline in the subset: the renderer has no glyph
    to draw, so you get a blank, a `.notdef` box/tofu, a wrong glyph from a fallback mapping, or garbled
    CID values; PyMuPDF may also fail silently because the font''s built-in encoding cannot represent
    the new character. Reusing the subset name without re-embedding also breaks the subset prefix convention
    (`AAAAAA+` must describe a freshly generated subset).


    What to do:

    1. Get the original font file and re-embed it: `name, ext, type, buf = doc.extract_font(original_font_name)`,
    then insert a fresh font on the page with the full bytes — `page.insert_font(fontname="GeorgiaNew",
    fontbuffer=buf)` and draw with that `fontname`; PyMuPDF will subset it on save to include ALL characters
    you used.

    2. Better, if you have the original TTF/OTF on disk: `page.insert_text(..., fontname="F0", fontfile="/fonts/Georgia.ttf")`
    (or `fitz.Font(fontfile=...)` / an HTML `@font-face` via `fitz.Story`) so the full charset is available
    and subsetting happens automatically.

    3. Confirm the substitute font actually contains the new glyphs (`fitz.Font(fontbuffer=buf).has_glyph(ord(ch))`),
    and that its license permits embedding.

    4. Verify after saving: re-open, extract the text, and check the new characters survived; render at
    high dpi to confirm no tofu boxes.

    '
- id: pdf-fonts-02
  answer: 'PyMuPDF built-in base-14 names:

    - Helvetica regular → `"helv"` (the standard alias `"Helvetica"` also works).

    - Helvetica bold → `"hebo"` (alias `"Helvetica-Bold"`).


    For completeness, the other two Helvetica variants are `"heit"` (Helvetica-Oblique/italic) and `"hebi"`
    (Helvetica-BoldOblique); other base-14 pairs include `"tiro"`/`"tibo"` (Times), `"cour"`/`"cobo"`
    (Courier), `"symb"` (Symbol), `"zadb"` (ZapfDingbats).


    Limitation for non-Latin/special characters: base-14 fonts are non-embedded standard fonts whose text
    is encoded with a single-byte, legacy encoding (WinAnsi/MacRoman/Standard, or the built-in encodings
    for Symbol/ZapfDingbats). They therefore only cover a small Latin (plus a little accented) repertoire
    — no CJK, no Arabic/Hebrew/Devanagari/etc., no most Unicode symbols/emoji, and even some "special"
    characters outside code page 1252 (e.g. ℠, €-adjacent glyphs, many arrows/dashes outside 1252) will
    map to the wrong glyph or nothing. Because they are not embedded, output also depends on the viewer''s
    local substitute font, so metrics/glyphs can differ between readers. For anything outside that encoding
    you must embed an external Unicode font (`fontfile=`/`fitz.Font(fontfile=...)`, or a Story/HTML `@font-face`).

    '
- id: pdf-fonts-03
  answer: 'Detect the original properties — extract the actual span from the page rather than guessing:


    `d = page.get_text("dict")` → for each block/line/span in the header cell region, read:

    - `span["font"]` — the font name, e.g. `AAAAAA+Georgia-Bold` (subset prefix + real name; the suffix
    after `+` tells you the weight/style);

    - `span["size"]` — the rendered point size (compare against your output);

    - `span["flags"]` — bit 16 = bold, bit 2 = italic, bit 4 = serifed, bit 8 = monospaced (PyMuPDF''s
    synthetic bold flag), so you can confirm bold even if the name is ambiguous;

    - `span["color"]` and the span bbox/baseline (`bbox[3]` ≈ baseline) for color and vertical position.

    Cross-check with `doc.get_page_fonts(pno, full=True)` which reports base font, type (Type1/TrueType),
    and whether it is embedded.


    Match it:

    - Use the SAME font, weight and size: if the bold face is embedded in the document, extract it (`name,
    ext, typ, buf = doc.extract_font("AAAAAA+Georgia-Bold")` — use the full name reported by get_page_fonts)
    and re-embed on the page (`page.insert_font(fontname="HdrBold", fontbuffer=buf)`), then `page.insert_text(...,
    fontname="HdrBold", fontsize=size, color=color)`. Re-embedding is required because a subset may not
    contain your new characters (see subset issue).

    - If no bold face is available anywhere, either add the matching external font file (`fontfile=` with
    the correct Bold family) or synthesize weight: draw with `render_mode=2` (fill+stroke) and a small
    `border_width` (e.g. 0.3 pt) to fake bold — approximate, so prefer a real face.

    - Match size exactly from `span["size"]` (do not rely on the nominal font size), and place the baseline
    at the original `bbox[3]` (or `y` from the origin) so your text does not sit visually higher/lower
    than the neighbors.

    - Finally re-extract the span and assert `font`/`size`/`flags` of your new text equal the originals.

    '
- id: pdf-verify-01
  answer: "Do not trust the script's exit status — verify the SAVED FILE, from disk, in several independent\
    \ ways:\n\n1. Re-open the written file: `doc = fitz.open(\"out.pdf\")` (not the in-memory doc) and\
    \ confirm it opens without errors and page count is as expected.\n2. Text assertion (positive + negative):\n\
    \   - `page.search_for(\"New Item Name\")` must return at least one rect inside the intended table\
    \ cell region;\n   - `page.search_for(\"Old Item Name\")` must return NOTHING (the old value must\
    \ be gone — otherwise you may have added a duplicate instead of editing).\n3. Structural check: `page.get_text(\"\
    dict\")` → inspect the spans/bboxes of the table region: confirm the new string exists with the right\
    \ font/size/position, that it did not overflow into the neighboring cell, and that no leftover span\
    \ of the old value remains.\n4. Visual check (the most reliable for layout): render the page — `pix\
    \ = page.get_pixmap(dpi=200)` → save PNG and eyeball it; better, render before and after and diff\
    \ the two images (pixel-diff or compare the cropped table region) so you see exactly what changed\
    \ and that nothing else shifted, overlapped, or clipped.\n5. Content is on the right page: locate\
    \ the match's page number and rect, and confirm the rect lies within the table's bbox (catches \"\
    edited page 3 instead of page 1\").\n6. File integrity: open with an independent tool (e.g. `qpdf\
    \ --check`, or re-parse with pypdf/pikepdf) to ensure no corruption; confirm the file size/timestamp\
    \ changed, i.e. the save actually wrote.\n7. For redactions/confidential removal: additionally grep\
    \ the raw bytes/extracted text (and check images/annotations) to prove the removed content is not\
    \ recoverable.\n"
- id: pdf-verify-02
  answer: 'No — not safely. `incremental=True` writes an incremental update: the new revision is appended
    and the previous revision''s bytes are retained in the file (that is exactly what makes incremental
    save possible and allows rollback/undo). So the redacted row''s original text and content stream are
    still physically present in the file and recoverable forensically — e.g. by tools that read earlier
    revisions, by carving the old page object/`Contents` stream, from uncompressed remnants, object streams,
    or a linearized/old xref. Most ordinary extractors read only the latest revision and will not show
    the row, which gives a false sense of removal, but "hidden by the latest revision" ≠ "deleted from
    the file".


    To actually purge it:

    - Save non-incrementally to a NEW file with garbage collection and compression, e.g. `doc.save("clean.pdf",
    garbage=4, deflate=True, incremental=False)` (garbage=4 also rewrites/deduplicates and drops unreferenced
    objects); or round-trip through pikepdf and drop unused objects.

    - Apply the redaction first (`page.apply_redactions(...)`) so the text/graphics are removed from the
    content stream, not merely covered by a drawn box — a visual cover alone never removes data.

    - Then verify at the byte level: run `strings`/grep or extract all text and images from the new file
    and confirm the confidential row''s content is absent; also check metadata, annotations, and thumbnails.

    - Keep the original file untouched (and treat it as still containing the secret — secure-delete it
    if the goal is confidentiality).

    '
- id: pdf-verify-03
  answer: '- Never modify the original in place: copy `invoice.pdf` to a working file (or open read-only)
    and write the result to a separate output name (e.g. `invoice_edited.pdf`), only overwriting/renaming
    after verification and explicit user confirmation.

    - Keep a backup of the original before any save; do not delete or replace it until the edited copy
    passes checks.

    - Avoid `incremental=True` on the user''s only copy until verified — a crash mid-save can leave a
    corrupt file; prefer `doc.save(out, deflate=True)` to a fresh path, then verify, then optionally move
    into place atomically (write temp → verify → rename).

    - Open/parse defensively: check the file is a valid PDF, note if it is encrypted/password-protected,
    check read permissions and that you have write access to the target directory, and ensure enough disk
    space.

    - Close handles and confirm the save actually wrote (file mtime/size changed); make sure you save
    the document you edited, not a different path.

    - Preserve everything you didn''t intend to change: don''t strip annotations, form fields, links,
    bookmarks, metadata, signatures, or page count; beware that editing invalidates any digital signature
    on the file.

    - Verify the result before delivering (re-open, extract text, render and visually inspect); keep the
    working file and report what changed.

    - Respect confidentiality: invoices contain PII/financial data — don''t upload to third-party services,
    don''t log contents, and clean up temp copies afterwards.

    '
- id: pdf-verify-04
  answer: 'Because "no exception raised" only proves the interpreter reached the end of the script — it
    says nothing about the PDF''s content. Specifically:


    - PyMuPDF/pypdf operations often fail silently: `page.search_for("X")` returning an empty list, a
    regex that matches nothing, `insert_textbox` returning a negative value (text clipped/overflow), or
    a loop over the wrong page index all complete without raising. The script can happily edit zero occurrences.

    - The edit may be in memory only: `doc.save()` never called, or saved to a different filename than
    the one the user opens, or the original file handle/viewer still shows the old cached version.

    - The target may be wrong: right text on the wrong page, the wrong table instance, a duplicate of
    the value rather than a replacement (old text still present).

    - Correct data, broken presentation: overlap with neighboring content, clipped/overflowing cell text,
    mismatched font/size, z-order problems, image covering text, rules drawn at wrong coordinates.

    - The file could even be structurally damaged (truncated save, broken xref) — many PDF readers silently
    repair or show blank pages instead of erroring.

    - "done" is an unconditional print, not an assertion; it reflects the program''s control flow, not
    the state of the artifact.


    Evidence requires independent inspection of the written file: re-open it from disk, assert the new
    string is found and the old string is absent (and on the correct page/region), check spans/bboxes
    for overflow, render the page to an image and visually/diff-check it, and validate the PDF with a
    second tool.

    '
