- id: pdf-model-01
  answer: 'A PDF has no table object and no notion of "rows". What looks like a table is

    just: text-showing operators (Tj/TJ) with explicit coordinates for each run

    of glyphs, plus vector drawing operators (lines/rectangles) for the rules and

    shading, all in one or more content streams. There is no structure you can

    address as "row 3" and no reflow engine — nothing moves when you remove

    something. So the edit must be done geometrically: locate the glyphs of that

    row by their coordinates, remove them (e.g. redaction), remove/adjust the

    rule lines and shading rects that belong to the row, and then explicitly

    move or redraw every row below it one row-height up and fix the Total. In

    practice you either rebuild the affected region (re-typeset it) or

    mechanically shift an existing region of the page (e.g. via a clipped

    Form XObject with show_pdf_page), because the PDF will never reflow for you.

    '
- id: pdf-model-02
  answer: 'Because the original text is still fully present in the content stream under

    the rectangle. It can be extracted with any text extractor, selected and

    copied by the reader, read by screen readers and indexers, and it reappears

    if the overlay is moved, deleted, or made transparent. It also breaks

    re-flowed/print rendering oddly. A white-out patch is a visual cover-up, not

    a content edit. Use a real redaction instead: add a redaction annotation

    over the text (page.add_redact_annot) and apply it

    (page.apply_redactions()), which actually removes the covered characters

    from the content stream. After that, insert the replacement text as new

    text-showing operators. If a true edit of the document is required

    (auditable, reflowed), the honest alternative is regenerating the PDF from

    its source.

    '
- id: pdf-model-03
  answer: 'Several reasons: (1) the string may not exist as contiguous bytes — "22.50"

    can be split across multiple Tj/TJ operators, kerned with TJ number

    adjustments, split per glyph, or written as hex strings; (2) the bytes are

    usually not ASCII — fonts (especially subset and CID fonts) map the bytes to

    glyph codes through an /Encoding or CIDToGIDMap, so the visible characters

    are encoded differently in the stream; (3) streams are typically compressed

    (FlateDecode), so a byte search on the file sees only compressed garbage

    unless you decompress first (pikepdf can, but raw editing still hits 1 and

    2); (4) even if you find and replace it, a different-length string does not

    reflow — the extra/fewer glyphs draw at fixed advances and collide with or

    leave gaps before the next operators, and the font subset may not contain

    the glyphs you inject; (5) you may replace text that appears inside

    coordinates, resources or a different stream, corrupting the page. The

    reliable route is higher-level: find the word via text extraction, remove it

    (redaction) and insert new text with the proper font/size at the same

    baseline.

    '
- id: pdf-model-04
  answer: 'PyMuPDF presents coordinates in a top-left-origin system in points: (0,0)

    is the top-left of the page rect, y increases downward. Raw PDF coordinates

    are bottom-left-origin, y upward, relative to the (usually) MediaBox. The

    bboxes returned by get_text("words") are already in PyMuPDF''s

    normalized, rotation-corrected space, and drawing functions (insert_text,

    draw_rect...) expect exactly that same space — so text-extraction bbox →

    drawing works 1:1 with no conversion. Differences appear when: (a) /Rotate

    is set — the raw content stream is stored in unrotated coordinates; PyMuPDF

    hides this via page.rotation_matrix / derotation_matrix, so anything you

    take from raw stream parsing (or pikepdf) must be transformed, and page.rect

    has swapped width/height for 90/270; (b) the CropBox is offset from the

    MediaBox — PyMuPDF''s page.rect is the CropBox, so raw coordinates need the

    CropBox offset subtracted; get_text-derived coordinates are already

    CropBox-relative and need no adjustment. Rule of thumb: stay inside

    PyMuPDF''s coordinate space end-to-end; only convert (rotation + crop offset)

    when mixing in coordinates read from the raw PDF.

    '
- id: pdf-locate-01
  answer: 'The high-level way in PyMuPDF is page.find_tables() (the TableFinder): it

    detects ruled/aligned tables and returns Table objects with an overall bbox,

    and rows whose .cells are the bounding-box rects of each cell (None for

    merged/absent cells); row.bbox gives the full row rectangle. You can tune it

    with snap_tolerance, join_tolerance, etc. If find_tables fails (borderless

    tables), derive geometry yourself: page.get_drawings() gives every line/rect

    operator — horizontal segments cluster into row rule y-positions, vertical

    segments into column x-positions; intersect those with

    page.get_text("words")/("dict") span bboxes to assign each word to a row

    (y-range) and cell (x-range), and compute per-row and per-cell bboxes from

    the union. Third-party detectors (e.g. Camelot/camelot-style lattice or

    stream mode) work on the same principle: rules give the grid, text bboxes

    fill it.

    '
- id: pdf-locate-02
  answer: 'search_for("Widget C") returns only the bbox of that exact text run — the

    description cell. A "row" is the full-width band: the other cells, their

    text, the row shading rectangle and the rule lines above/below it. Removing

    just the found bbox leaves orphan rules, shading, and the rest of the row

    (prices, quantities) behind. To get the full row region: prefer the table

    model — page.find_tables() and take the row object whose first cell contains

    that text; row.bbox is the full-width rectangle. Without table detection:

    take the found bbox''s vertical span and expand it to the table''s outer x

    boundaries (from vertical rule lines via get_drawings(), or the union of

    all rows'' text extents), and to the neighboring horizontal rules (y of the

    lines just above and below). That rectangle defines what must be removed and

    what must move up.

    '
- id: pdf-locate-03
  answer: 'Use page.get_text("dict"): it returns blocks → lines → spans; each span

    carries "font" (font resource name, e.g. ABCDEF+Helvetica-Bold), "size",

    "color" (sRGB integer — convert with fitz.sRGB_to_rgb), "origin" (the

    baseline start point of the span, exactly what insert_text wants), "bbox",

    and "flags" (bitfield: bold, italic, serifed, monospaced, superscript).

    get_text("rawdict") goes one level deeper with per-character bboxes and

    origins, useful for baseline of a single word. From the font name strip the

    subset prefix (ABCDEF+) to get the base font for insert_text; if the exact

    font is embedded and not one of the base-14, you may need to load a

    matching font file or accept the closest standard font. Baseline: use

    span["origin"] y (or the char origins in rawdict) rather than the bbox,

    because bbox top is the ascender, not the baseline.

    '
- id: pdf-locate-04
  answer: 'Two sources: (1) If the table is ruled, page.get_drawings() gives the vector

    lines — vertical segments'' x-coordinates are the column boundaries

    (cluster near-duplicates); horizontal segments give row y-positions. This

    also tells you line width/color if you must redraw rules. (2)

    page.find_tables() returns the column x-boundaries directly in the cell

    bboxes. For right-alignment of numbers: extract spans with

    get_text("dict") (or words) for the existing numeric cells; the right edge

    of each number''s bbox (bbox.x1) is its alignment target — right-aligned

    numbers share (approximately) the same x1. New values should be inserted so

    their rendered width (fitz.get_text_length(text, fontname, fontsize))

    ends at that same x: start_x = target_right_x - width. If the column is

    cell-padded, derive the target from the rule-line x minus the observed

    padding of existing rows.

    '
- id: pdf-rowdel-01
  answer: '1. Detect geometry: page.find_tables() → row bboxes, column x-edges, row

    height; also page.get_drawings() for the exact rule lines and shading rects

    (color, width) belonging to the table.

    2. Identify the row to delete and compute the "everything below" region R

    (from the row''s bottom rule down to the bottom of the table / page content

    that must move up).

    3. Shift region R up by one row height, preserving appearance exactly: make

    a copy of the document (or the page as its own source), then

    page.show_pdf_page(target_rect = R shifted up by row_height, src=copy,

    pno, clip=R). This embeds the original content stream as a Form XObject,

    so text, rules and shading reappear pixel-identical one row higher.

    4. Erase the originals: paint the whole affected band (old R plus the now

    vacated last row area) with the page background color (or redact it), so

    the original rows below are gone and only the shifted copies show.

    5. Redraw the table''s closing bottom rule at the new position (copy width

    and color from get_drawings), and fix alternating row shading if the shift

    breaks the pattern.

    6. Update the Total: redact the old total text tightly (glyphs only, fill

    matching the background), recompute the sum without the deleted row, and

    insert_text it at the same baseline origin (span["origin"] from

    get_text("dict")), same font/size/color, right-aligned to the same x edge

    (start_x = old_right_edge - get_text_length(new_total, font, size)).

    7. Caveats: keep redaction rects off images (default blanks overlapping

    pixels) and know vector rules are NOT removed by default redactions — cover

    them with background paint; annotations/links in the shifted region are not

    moved; verify by re-extracting text and rendering before/after.

    '
- id: pdf-rowdel-02
  answer: 'Defaults (modern PyMuPDF): apply_redactions(images=PDF_REDACT_IMAGE_PIXELS,

    graphics=PDF_REDACT_LINE_ART_NONE, text=PDF_REDACT_TEXT_REMOVE). That means:

    any raster image whose area overlaps the redaction rect has its overlapping

    pixels blanked out (set white) — the image object survives but is wiped

    where covered; vector graphics / line-art (the table''s rule lines, shading

    rectangles, borders) are left completely untouched by default; all text

    characters whose bbox intersects the rect are deleted from the content

    stream. (Older versions only had the images parameter, also defaulting to

    pixel-blanking.) Why it matters for a table row: the row''s text will be

    removed, but its horizontal rules and shading will NOT disappear — you must

    remove or repaint them yourself, or they''ll remain as orphaned lines. And

    if the row region overlaps an embedded image (logo, stamp, barcode,

    screenshot-style table), its pixels are silently blanked — so keep the

    redact rect tight, or choose PDF_REDACT_IMAGE_NONE when you must preserve

    images. Conversely, raising graphics to REMOVE_IF_TOUCHED can delete rules

    shared with adjacent rows.

    '
- id: pdf-rowdel-03
  answer: 'Yes — Page.show_pdf_page(). It draws a page (from any open document,

    including a copy of the current one) into a target rectangle, optionally

    clipped, preserving the original rendering exactly because the source

    page''s content is embedded as a Form XObject rather than re-typeset.

    Procedure for shifting a region up by height h: copy the document

    (doc2 = fitz.open(doc.tobytes()) or fullcopy_page), choose the source

    region rect R below the deleted row, then on the working page call

    page.show_pdf_page(rect=R shifted up by h, src=doc2, pno, clip=R) after

    painting/blanking the original band. Everything inside R — text, rules,

    shading, images — reappears identical, h points higher. Caveats: it

    rasterizes nothing (stays vector/text) but the moved content is a snapshot

    — interactive annotations, links and form fields inside R are not relocated

    (the link rectangles stay where they were); text inside the XObject is

    still extractable at the new drawn position, though some extractors may

    report it as part of an XObject layer; also watch render order (draw the

    background cover first, then show_pdf_page, then any new text on top).

    '
- id: pdf-rowdel-04
  answer: 'Almost certainly more: the Total/Subtotal row (recompute the sum), any Tax

    or VAT amounts computed from line items, the Grand Total / Amount Due /

    Balance figures (in the table footer or elsewhere on the page or later

    pages), per-page running totals and "continued" markers if the table spans

    pages, line-item numbering/serials, quantity or unit-price cross-references,

    and the document-level totals shown in summary boxes or payment stubs.

    Cosmetically: the table''s closing bottom rule, alternating row shading

    pattern, and any zebra banding or row count labels ("5 items"). Also page

    layout downstream: if rows below move up, footers, page numbers, and

    content on following pages may shift or leave whitespace. If the invoice is

    part of a batch or linked data (accounting export), the structured data

    behind it must be reconciled too.

    '
- id: pdf-cell-01
  answer: '1. Locate the cell: page.find_tables() cell bbox, or

    page.search_for("22.50") to find the word, then read its span via

    page.get_text("dict") to get font, size, color, and origin (baseline).

    2. Remove the old number: add a redaction annot over a tight rect covering

    just those glyphs (per-character bboxes from get_text("rawdict") unioned,

    padded ~0.5–1pt, kept inside the cell away from rules) with fill set to

    the cell''s background (or fill=False if the cell has vector shading you

    want to keep); page.apply_redactions() with defaults (image pixel-blanking

    is fine as long as the rect doesn''t touch images; line-art untouched so

    rules survive).

    3. Insert the replacement natively: compute the width of the new string

    with fitz.get_text_length("54.00", fontname, fontsize); for a right-aligned

    column, start_x = old_number_right_edge - width (old right edge = previous

    bbox.x1 or the shared alignment x of other numbers); keep the same baseline

    y = old span origin y. Call page.insert_text((start_x, baseline_y), "54.00",

    fontname=<matching base font>, fontsize=<old size>,

    color=<converted from span color>). Insert AFTER the shading so it renders

    on top; if the cell has rotation, use the morph/rotate parameter

    accordingly.

    4. Verify: re-extract the text at that area and render the page to compare

    alignment and style against neighboring cells.

    '
- id: pdf-cell-02
  answer: 'Size it from the actual glyph bboxes, not the visual cell: get the

    per-character rects via page.get_text("rawdict") for the target cell, take

    their union, and pad it only slightly (≈0.5–1 pt) — enough to catch glyph

    edges (kerned/antialiased runs can sit a hair outside the nominal bbox)

    but not enough to reach the neighboring cell''s first/last character or the

    rule lines. Because the removal criterion is overlap: any character whose

    bbox intersects the rect is deleted, so a rectangle that bleeds into the

    adjacent cell silently deletes its text; conversely a too-tight rect may

    leave stray fragments (descenders, italic overhangs) behind. Keep the rect

    inside the cell interior, away from border lines (they''re vector art and

    survive by default anyway — but avoid enabling graphics removal), and away

    from any image bbox to avoid pixel-blanking. Also remember apply_redactions

    paints the rect area with a fill (white by default): to preserve cell

    shading, set fill to the shading color or fill=False in add_redact_annot.

    '
- id: pdf-cell-03
  answer: 'insert_textbox returns the unused vertical space; if the text does not fit

    inside the given rect, the return value is negative (the shortfall) and, by

    default, nothing at all is inserted. Your options: (1) reduce the font size

    until get_text_length / the textbox return ≥ 0; (2) widen the rect if

    there''s real slack (cell padding, adjacent empty cell) — but respect the

    column boundaries of a ruled table; (3) abbreviate or reformat the text

    (shorter unit, drop redundant decimals); (4) don''t use insert_textbox at

    all — insert_text does no wrapping and will simply overflow the rect, which

    may be acceptable visually but will collide with the neighbor; (5) the

    real fix if the content genuinely needs more room: widen that column, i.e.

    rebuild/relayout the table region (move the vertical rule, re-typeset the

    affected cells), or allow wrapping by inserting the text as multiple lines

    at smaller size. Also note insert_textbox''s align parameter (left/center/

    right) — right-align keeps numbers flush with the column edge.

    '
- id: pdf-cell-04
  answer: 'point is the baseline origin: the position where the pen starts — the left

    end of the baseline of the first glyph. PyMuPDF draws the string along that

    baseline; ascenders go above it, descenders below. If you pass the top-left

    corner of the old word''s bbox (which is the top of the ascender line), your

    new text lands roughly a full font-size too low: its baseline ends up near

    where the old text''s top was, so it overlaps/descends below the intended

    line, may collide with the rule below or the next row, and looks vertically

    misaligned with neighboring cells. The correct y is the baseline: take

    span["origin"] (or char origins) from get_text("dict"/"rawdict"), or

    approximate y = bbox.y1 - descent (bbox bottom minus the font''s descender).

    The x from bbox.x0 is fine for left-aligned text; for right-aligned

    numbers use x = target_right_edge - get_text_length(text, font, size).

    '
- id: pdf-relayout-01
  answer: 'Inserting a row is a relayout, not an insertion — everything below the

    insertion point must move down by one row height: (1) the Total row''s text

    moves down (and its amount may change if the new row adds value);

    (2) every horizontal rule (the line above the Total row and the table''s

    bottom rule) moves down/redraws; (3) row shading bands below shift;

    (4) anything below the table — footnote lines, totals boxes, signature

    blocks, page footer — must shift down as well; if there''s no room, content

    must flow to the next page (real reflow). Mechanically in an existing PDF

    there is no "insert space": you either (a) capture the region from the

    insertion point down (page copy + show_pdf_page with clip, drawn at

    +row_height offset) after blanking the original band, then re-typeset the

    new row''s texts into the vacated strip and redraw its top rule; or (b)

    re-typeset the whole table. Also update the Total text itself

    (redact + insert_text at the new baseline, matching style), fix the

    closing bottom rule position, and remember annotations/links under the

    shifted region don''t move with show_pdf_page.

    '
- id: pdf-relayout-02
  answer: 'Correct approach: the table cannot just grow, so existing columns must be

    narrowed to free the width of the new column: (1) decide the new column

    widths (redistribute proportionally or give the new column a fixed share),

    so the outer edges still land on the page margins; (2) rebuild the table

    region: erase/redraw every vertical rule at the new x positions, re-typeset

    every cell''s text (header and all rows) to the new cell boxes — because

    old text sits at old x positions and cannot be nudged, each cell''s content

    must be removed (redaction) and re-inserted with the correct alignment

    (right-aligned numeric columns: end at new_right_edge - text width,

    preserving each row''s font/size/color from get_text("dict")); (3) move or

    re-draw anything that was outside but adjacent (nothing here, since edges

    stay on the margins). The common mistake: simply drawing one more vertical

    line and squeezing/overlapping existing text into narrower cells without

    re-typesetting — you get overlapping glyphs, text sticking out of cells or

    past the margins, misaligned numbers, and duplicated rules; another

    mistake is letting the new table exceed the margins, colliding with the

    page number/footer or triggering an unwanted reflow. In practice this is

    best done by regenerating the table (or the page) rather than in-place

    patching.

    '
- id: pdf-relayout-03
  answer: 'Acceptable when: the document layout is simple (flowing text + tables, minimal

    decoration), you are effectively regenerating the document and are happy for the

    DOCX/HTML version to become the new source of truth, you have the original

    authoring file available anyway, or exact pixel/point fidelity is not required.

    Risks: PDF is a fixed-layout print format; converters approximate, so you typically

    get reflowed text, changed pagination, font substitution, degraded or re-encoded

    images, loss of vector detail, broken or restructured tables, and lost form fields,

    annotations, links, bookmarks, and metadata. Round-tripping (PDF → DOCX → PDF)

    almost never reproduces the original exactly, and each conversion can compound

    errors. For brand-critical or legal-looking documents where "it must look the same"

    matters, in-place editing (redaction + redraw, overlay) or rebuilding from source is

    safer; conversion is fine when the converted artifact itself is acceptable as the

    new document.

    '
- id: pdf-relayout-04
  answer: 'In PyMuPDF, collect every occupied rectangle and test the candidate rect against

    them: text via page.get_text("dict"/"blocks") (block/line/span bboxes), vector

    graphics and table rules via page.get_drawings() (each item has a rect), images via

    page.get_image_rects(xref) or page.get_image_info(xrefs=True), plus annotations

    (page.annots()) and widgets. Build the list of rects, then check that your target

    rect does not intersect any of them (or compute free gaps between block bboxes).

    Also respect header/footer margins. As a sanity check, render the page with

    page.get_pixmap() and inspect the candidate area visually (or check it is uniformly

    background-colored) — this catches things the object lists miss, such as content

    drawn via Form XObjects or shadings.

    '
- id: pdf-tblins-01
  answer: 'First confirm free space on page 1 (see bounding boxes of text/drawings/images).

    Way 1 — draw directly with PyMuPDF: compute row/column geometry, then use

    page.draw_rect()/page.draw_line() for the grid and page.insert_text() or

    page.insert_textbox() for cell text, choosing a font/size consistent with the doc.

    Way 2 — overlay PDF: build a small one-page PDF with reportlab (a platypus.Table

    with TableStyle, or manual canvas lines/text), then stamp it onto page 1 at the

    target rect with page.show_pdf_page(rect, overlay_doc, 0) (or merge with pypdf''s

    PageObject.merge_page). The overlay approach gives you nice table styling almost for

    free; the direct-draw approach avoids a second library.

    '
- id: pdf-tblins-02
  answer: 'Sample the document''s existing style first: font names, sizes and colors from

    page.get_text("dict") spans; stroke widths, stroke/fill colors from

    page.get_drawings() items. Reuse those values: same font (or its base-14/full-file

    equivalent), same font size, same rule thickness and color, and align your table

    edges/columns to the page''s existing margins or column boundaries. Use consistent

    cell padding and text baseline placement, draw with overlay=True (default) so it

    sits on top, and make sure the rect does not overlap existing content. Finally

    render before/after images and compare — look for doubled lines, misaligned text,

    color mismatches, or text that collides with existing content.

    '
- id: pdf-tblins-03
  answer: 'PDF has no reflow — text positions are baked in, so you cannot "insert" and push

    content down. Options: (1) white-out one paragraph (redaction with white fill or a

    white rect) and redraw it lower with insert_text/insert_textbox using a matching

    font — but you need a full font (embedded subsets usually lack the glyphs), and

    links/form fields/background art behind the box are masked; (2) find genuinely free

    space elsewhere on the page and place the table there; (3) put the table on a new

    page; (4) rebuild the page/document from an original source (DOCX/HTML/generator)

    if one exists — usually the only clean way to truly reflow; (5) make surrounding

    content more compact (redact + redraw tighter) to create a gap. Simply overlaying

    the table on top of the paragraphs is not a real option.

    '
- id: pdf-imgrep-01
  answer: 'Find the image and its placement: imgs = page.get_images(full=True) gives the xref;

    rects = page.get_image_rects(xref) gives where it is drawn. Then use PyMuPDF''s

    page.replace_image(xref, filename="newlogo.png") (or pixmap= / stream=), which

    inserts the new image and rebinds this page''s reference to it, keeping the same

    rect/size. The catch with shared images: a single image xref can be referenced from

    several pages or Form XObjects. If you replace the image by rewriting the shared

    xref''s stream (e.g. doc.update_stream), every occurrence in the document changes;

    replace_image avoids that by pointing only this page at a new object, but you should

    still check whether the logo appears elsewhere and whether that matters. Also be

    aware the new PNG is scaled into the existing rect — mismatched aspect ratio means

    distortion unless the rect is adjusted or keep_proportion semantics are considered.

    '
- id: pdf-imgrep-02
  answer: 'The old logo is still fully present in the file — you only painted the new one on

    top (z-order trick). Consequences: the original image remains extractable

    (page.get_images still lists it; anyone can pull it out), the file carries both

    images, and behavior differs across viewers/layers — if the new image is

    transparent, mis-sized by a hair, or content is reordered, the old logo shows

    through or around it. Text/image extraction and OCR still see the stale content,

    and it grows the file. The proper fix is to actually remove/replace the underlying

    image (page.replace_image or page.delete_image), not to cover it.

    '
- id: pdf-imgrep-03
  answer: 'The logo may not be an image XObject at all: it can be vector artwork (paths) —

    get_images() only lists image XObjects; or it can be nested inside a Form XObject

    that plain get_images() doesn''t descend into; or drawn via a Type 3 font glyph or a

    shading/pattern. How to find it: page.get_image_info(xrefs=True, hashes=True)

    reports every image actually rendered on the page with its bbox and xref (this

    sees through nesting); page.get_drawings() reveals vector logos; you can also walk

    doc.xref_object of the page''s Resources and recurse into XObjects. Replace: if it

    is a real image (even nested), use its xref with page.replace_image(); if it is

    vector art, there is nothing to "replace" — you must remove/cover it (redaction

    with fill color over its bbox, or redraw) and place the new logo on top, or

    regenerate the page.

    '
- id: pdf-imgins-01
  answer: "Get the PNG's pixel dimensions (fitz.Pixmap(\"sig.png\") or PIL Image.size) and\ncompute height\
    \ = 150 * (h / w) to preserve aspect. Pick margins (e.g. 36pt) and\nbuild the rect in PyMuPDF's top-left-origin\
    \ coordinates:\nr = fitz.Rect(page.rect.width - 36 - 150, page.rect.height - 36 - height,\n      \
    \        page.rect.width - 36, page.rect.height - 36).\nCheck overlap before inserting: iterate page.get_text(\"\
    blocks\") (and drawings)\nand confirm no bbox intersects r; if it does, raise the rect (decrease y)\
    \ until\nclear. Then page.insert_image(r, filename=\"sig.png\") — keep_proportion defaults\nto True,\
    \ and since the rect already encodes the correct aspect, nothing is\ndistorted. Save to a new file\
    \ and verify visually.\n"
- id: pdf-imgins-02
  answer: 'Don''t insert the raw image 200 times — that embeds 200 copies of the stream.

    Insert it once, then find its xref (page.get_images(full=True) on that page), and

    on every other page reuse that object: page.insert_image(rect, xref=xref). All

    pages then reference one shared image object, adding only the small per-page

    reference instead of the image bytes. (Newer PyMuPDF versions may internally

    deduplicate identical image data by digest, but relying on that is fragile —

    passing xref explicitly is the safe pattern.) Afterwards a save with garbage

    collection (garbage=4) keeps the file lean.

    '
- id: pdf-imgins-03
  answer: 'Likely causes: (1) overlay=False was used (underlay), so the image is inserted

    beneath existing page content and a later-drawn filled rectangle paints over it —

    overlay=True (default) draws on top; (2) wrong rect: zero/negative area, entirely

    off-page, or y-axis confusion (PyMuPDF origin is top-left, PDF''s is bottom-left);

    (3) the image was inserted on a different page object or into a different Document

    instance than the one saved, or the wrong file was saved/inspected; (4) the image

    is fully transparent or has an alpha/colorspace problem making it invisible;

    (5) it landed in a hidden optional-content group (OCG) or is clipped by an

    enclosing clip path; (6) content drawn after the insertion point (appended later

    to the content stream) covers it — insertion order matters.

    '
- id: pdf-fonts-01
  answer: 'The AAAAAA+ prefix marks an embedded subset: only the glyph outlines for the

    characters actually used in that document were embedded. Text containing letters

    (or even spacing/kerning data) that never appeared cannot be rendered from that

    subset — the glyphs simply don''t exist, so characters render blank/boxes or the

    operation fails, and you cannot extend the subset program. What to do: draw the

    replacement text with a full version of the same typeface loaded from the system

    (insert_font(fontfile=..., fontname=...) then use it with insert_textbox/

    TextWriter), which visually matches; if the exact font isn''t available, fall back

    to a close built-in (base-14) font and accept a small look change — never reuse

    the subset expecting new glyphs. If many edits are needed, regenerate from source.

    '
- id: pdf-fonts-02
  answer: 'Helvetica regular is "helv" and Helvetica bold is "hebo" (PyMuPDF base-14 aliases;

    also "heit"/"hebi" for italic/bold-italic, Times "tiro/tibo/tiit/tibi", Courier

    "cour/cobo/coit/cobi", plus "symb" and "zadb"). Limitation: the base-14 fonts

    cover only Latin scripts (roughly Latin-1 plus a few extras). They have no glyphs

    for non-Latin writing systems — CJK, Arabic, Hebrew, Cyrillic, Devanagari, etc. —

    and only limited special characters/symbols. For those you must supply a font file

    (fontfile= parameter) or use PyMuPDF''s bundled CJK built-ins (e.g. "china-s",

    "japan", "korea"), otherwise text renders as missing glyphs or raises errors.

    '
- id: pdf-fonts-03
  answer: 'Inspect the original span: d = page.get_text("dict") → block/line/span dicts carry

    "font" (name, often containing "Bold"), "size", "flags", and "color". The flags

    bit 16 means bold, 2 means italic. So: detect weight from the font name and/or the

    bold flag; take size = span["size"] (float pt). Then draw the replacement with a

    matching bold face — "hebo" if you''re using base-14, or load the bold member of

    the family via fontfile if the header used an embedded font — at exactly

    span["size"], and copy span["color"] too. You can pre-measure with

    fitz.Font("hebo").text_length(text, fontsize) to check the box fits. Verify by

    rendering before/after and comparing visually.

    '
- id: pdf-verify-01
  answer: 'Verify the saved artifact, not the in-memory script state: (1) reopen the output

    file fresh with a new Document instance; (2) extract the text in the edited region

    (page.get_text(clip=rect)) and assert the new value is present and the old value is

    gone — ideally with an automated check, not eyeballing; (3) confirm the old string

    no longer appears anywhere it shouldn''t (whole-document get_text / search_for);

    (4) render the page to an image (get_pixmap) and inspect, or diff against an

    expected render; (5) sanity-check that nothing else changed: page count, other

    pages'' text, table geometry. Optionally cross-check with an independent tool

    (pdftotext) so you''re not just trusting the same library that made the edit.

    '
- id: pdf-verify-02
  answer: 'Not reliably. An incremental save appends a new revision to the same file; the

    original body — including the confidential row''s content — remains in the file as

    a prior revision and can be recovered with ordinary tools. So after

    apply_redactions the row is gone from the rendered/current document, but it is not

    gone from the file bytes. For confidential data you must save a fresh full file

    (e.g. doc.save("clean.pdf", garbage=4, deflate=True) to a new name, then replace

    the original) so that only the cleaned revision exists. Also note the redaction

    must actually be applied (apply_redactions) before saving — merely adding

    redaction annotations without applying removes nothing even visually.

    '
- id: pdf-verify-03
  answer: 'Never edit the user''s only copy in place: copy the PDF to a scratch/working

    directory, edit the copy, verify the result, and only then deliver/replace —

    keeping the original untouched (ideally read-only) and keeping a backup of the

    pre-edit version. Save destructive outputs (redactions, deletions) to a new file

    with garbage collection rather than incremental-saving over the original. Check

    the file opens and isn''t encrypted (doc.needs_pass) and handle permissions

    appropriately. Verify checksums/sizes before and after if integrity matters. Don''t

    ship the document''s contents to third-party services. Preserve metadata,

    annotations, and form data unless asked; and confirm the delivered file actually

    contains the requested change before telling the user it''s done.

    '
- id: pdf-verify-04
  answer: '"No exception + printed done" only proves the code path ran — it says nothing

    about whether the edit landed where and how intended. Silent failure modes are

    common: the target text never matched so nothing was changed, coordinates were

    wrong so content was drawn off-page or over something else, the wrong page or

    wrong Document instance was edited, the save went to a different path, or a

    no-op path executed. Verification must operate on the output artifact: reopen the

    saved PDF and assert expected content (text extraction in the target region,

    search for old/new strings), render pages and compare, check geometry. Evidence

    comes from automated checks against the saved file, not from the script''s stdout.

    '
