- id: pdf-model-01
  answer: 'A PDF has no table object at all. What looks like a table is just a pile of

    independent drawing and text operators in one or more content streams: ruled

    lines/filled rectangles for the grid and shading, and absolutely-positioned

    glyph runs for the text (each string placed at an explicit x,y, in draw

    order). There are no rows, columns, cells, or "row 3" to address. So

    "delete the third row" cannot be a semantic delete: you must locate the

    region geometrically, remove the text and vector marks that occupy it (e.g.

    redaction), then re-create the appearance — shift the rows below up, redraw

    the rules/shading, and update anything derived (such as a Total). The edit

    has to be a geometric region operation plus a redraw, not a data operation.

    '
- id: pdf-model-02
  answer: 'Drawing a white rectangle only paints new content on top of the old; it does

    not remove anything. The original text is still in the content stream and

    text layer, so it stays selectable, copyable, searchable and extractable,

    and a screen reader or text extraction still sees it. Any later edit that

    re-sorts the page can expose it, and the white box can be stripped. It is

    also visually fragile (background shading, other layers). The correct

    approach is to actually delete the text with a redaction (`page.add_redact_annot`

    then `page.apply_redactions`), which removes the glyphs from the content

    stream, and then insert the new text. Redaction is the real removal;

    overpainting is only a visual patch.

    '
- id: pdf-model-03
  answer: "Several reasons a raw byte search-and-replace fails or corrupts:\n- The text is often not one\
    \ contiguous literal. It may be split across\n  multiple Tj/TJ show operators, shown glyph-by-glyph,\
    \ or broken up by\n  kerning/positioning (TJ arrays with numeric adjustments), so the bytes\n  \"\
    22.50\" never appear together.\n- The string in the stream is encoded through the font's encoding/CMap\n\
    \  (e.g. subset fonts, custom /Differences, CID fonts), so the bytes are\n  glyph indices, not ASCII.\
    \ \"22.50\" in the stream may look nothing like\n  \"22.50\".\n- Streams are usually compressed (FlateDecode);\
    \ you must decode, edit,\n  re-encode, and fix /Length (and often the xref/stream length), or the\n\
    \  page breaks.\n- Changing string length without updating the enclosing operators, /Length,\n  or\
    \ xref offsets corrupts the file; and even if the byte count matches, a\n  shorter/longer new string\
    \ changes spacing/advance and misaligns the line.\n- Whitespace and operators are positionally meaningful;\
    \ a blind replace can\n  hit the wrong occurrence elsewhere in the stream.\nCorrect approach: parse\
    \ the content stream/objects properly (pikepdf with\ndecoded streams, or a text-aware library), or\
    \ better, use a library like\nPyMuPDF that understands text and fonts and do a redact-and-replace.\n"
- id: pdf-model-04
  answer: "Raw PDF user space has its origin at the bottom-left, with y increasing\nupward. PyMuPDF's\
    \ page coordinate system has its origin at the top-left of\nthe visible page, with y increasing downward,\
    \ measured in points. `get_text`\n(\"words\", \"dict\", etc.) returns PyMuPDF (top-left) coordinates,\
    \ so drawing\nfunctions (insert_text, insert_textbox, redaction rects) take the same\nsystem — that\
    \ part is consistent.\nComplications:\n- CropBox offset: PyMuPDF coordinates are relative to the CropBox,\
    \ not the\n  MediaBox. If the CropBox origin differs from the MediaBox origin, raw PDF\n  coordinates\
    \ are offset by that amount; you must subtract/add the cropbox\n  origin when converting to/from raw\
    \ user space.\n- /Rotate: extraction and drawing coordinates are in the page's unrotated\n  (CropBox-relative)\
    \ space. PyMuPDF exposes `page.rotation`,\n  `page.rotation_matrix` and `page.derotation_matrix` to\
    \ map between the\n  rotated display space (what a viewer shows) and PyMuPDF's internal space.\n \
    \ If you compare to coordinates someone read off a rotated rendering, you\n  must derotate/rotate\
    \ (and account for width/height swap at 90/270).\nIn short: use PyMuPDF coordinates for both reading\
    \ and writing, and only\nconvert (via cropbox origin and rotation/transformation matrices) when\n\
    dealing with raw PDF operators or externally observed coordinates.\n"
- id: pdf-locate-01
  answer: 'Use a table-aware extraction. In PyMuPDF (1.23+), `tabs = page.find_tables()`

    returns a TableFinder; iterate `tabs.tables`, each with `.bbox`, `.header`,

    `.rows` and `.extract()`. Each `table.rows[i]` has `.bbox` (the full row

    rectangle) and `.cells` — a list of cell bounding boxes (None for spanning

    gaps); `row.cells` gives per-cell rects directly. That is the row and cell

    geometry, not just text. Alternatively use pdfplumber

    (`page.find_tables()` / `page.extract_tables()` with `.bbox`), or camelot/

    tabula. Without such a library you can derive the grid yourself:

    `page.get_drawings()` yields the rule segments; cluster near-horizontal lines

    into row boundaries and near-vertical lines into column boundaries, intersect

    them to get cells; cluster `page.get_text("words")` into those cells. The

    find_tables path is the recommended one.

    '
- id: pdf-locate-02
  answer: '`search_for("Widget C")` returns only the tight bbox(es) of those glyphs —

    it covers the text, not the row. The row you must erase also includes the

    cell padding, the row''s full width (from the table''s left edge to its right

    edge, possibly other cells in that row) and the row''s full height (the

    vertical span between the row''s top and bottom rules), plus the row shading

    and the rules themselves. A redaction sized to the text bbox alone would

    leave the shading/rules and could miss neighboring cells to be removed.

    To get the full row region: use `page.find_tables()` and take

    `table.rows[i].bbox` (or, without a table finder, take the matched word''s

    y-range, snap it to the nearest horizontal rules above/below, and use the

    table''s left/right x-boundaries from the vertical rules or neighboring

    cells). Also search_for can return multiple hits or miss text split across

    spans, so you should confirm the hit is the right row.

    '
- id: pdf-locate-03
  answer: 'Use `page.get_text("dict")` (or "rawdict" for per-character detail) and walk

    blocks → lines → spans. Each span carries the style: `span["font"]` (font

    name, useful to identify base font/face), `span["size"]` (font size in

    points), `span["color"]` (sRGB color as an integer, 0xRRGGBB),

    `span["flags"]` (bold/italic/serif/monospaced bits), plus `span["ascender"]`

    and `span["descender"]`. The span''s `bbox` is its rectangle and

    `span["origin"]` is the text origin — the baseline start point (x, baseline

    y). Line-level `line["dir"]` gives the writing direction. So: font/size/

    color from the span, and the baseline from `origin[1]` (with x from

    `origin[0]`), which is what you want if you re-insert text on the same

    baseline. `get_text("words")` does not give font/size/color; use "dict".

    '
- id: pdf-locate-04
  answer: 'Column boundaries: `page.find_tables()` and read each cell rect (or the

    table''s column x-edges from the cells/rules), or inspect `page.get_drawings()`

    for near-vertical line segments and cluster their x coordinates. These give

    the left/right x of every column.

    Right edge for right-aligned numbers: extract the existing value with

    `page.get_text("words")` (or the span in "dict") and take its `x1` (the right

    edge of the actual glyph run), not the cell border. That x1, minus whatever

    padding the design uses, is where numbers should end. To place a new value

    so it ends at that edge, measure its width with

    `pymupdf.get_text_length(new_text, fontname, fontsize)` and set

    `x = right_edge - width`, drawing on the same baseline (`span["origin"][1]`).

    Matching font/size from the span keeps the measured width correct.

    '
- id: pdf-rowdel-01
  answer: "Correct procedure with PyMuPDF:\n1. Find the table and the target row: `page.find_tables()`,\
    \ take\n   `table.rows[i].bbox` and its data/`extract()`.\n2. Decide the region to remove: the deleted\
    \ row's bbox PLUS the whole region\n   below it that will move (the remaining rows, the Total, notes,\
    \ anything\n   under the table), or simply the whole table if you intend to redraw it.\n3. Add redaction\
    \ annotations over those rectangles and call\n   `page.apply_redactions()`. With the defaults this\
    \ removes the text and\n   also deletes the shading rectangles and any rules lying entirely inside\n\
    \   the rects — which is what you want when you are going to redraw the grid.\n   (If you wanted to\
    \ keep the grid you would pass graphics=0, images=0, but\n   here you are rebuilding, so remove and\
    \ redraw.)\n4. Shift the content that moved up by exactly one row height, either by\n   redrawing\
    \ it at the new y, or by copying the untouched region from an\n   unmodified source page with\n  \
    \ `page.show_pdf_page(new_rect, src_doc, pno, clip=old_rect)`.\n   Do this AFTER redaction — stamping\
    \ the shifted region while the original\n   rows are still present would duplicate every moved row.\n\
    5. Recompute the Total from the row data and redraw it, and redraw the\n   horizontal rules and row\
    \ shading at the new positions (fill first, then\n   text, then rules).\n6. Save to a new file with\
    \ `garbage=4, deflate=True`, then re-extract the\n   table and verify the text and totals are correct.\n"
- id: pdf-rowdel-02
  answer: "Default `apply_redactions()` parameters:\n- `text=PDF_REDACT_TEXT_REMOVE` (0): removes any\
    \ character whose bbox\n  overlaps the redaction rectangle.\n- `images=PDF_REDACT_IMAGE_PIXELS` (2):\
    \ blanks the image pixels that overlap\n  the rect (it removes the covered pixel data, not the whole\
    \ image).\n- `graphics=PDF_REDACT_LINE_ART_REMOVE_IF_COVERED` (1): removes vector\n  line-art / filled\
    \ shapes that lie entirely inside the rect; shapes that are\n  only partly covered are left whole.\n\
    So the claim \"line art is left untouched\" is false. Redacting a table row\ntherefore deletes that\
    \ row's shading rectangle and any rule wholly inside the\nrect, and blots out any image pixels there.\
    \ This matters because after a\ndefault row redaction the grid/shading is gone and must be redrawn;\
    \ and for a\nsingle-cell text replacement the defaults will also destroy that cell's\nshading/borders,\
    \ so you pass `graphics=0, images=0` when you only want the\ncharacters gone and the cell intact.\n"
- id: pdf-rowdel-03
  answer: 'Yes. `page.show_pdf_page(rect, source_doc, pno, clip=clip_rect)` places a

    clipped region of a source page onto the target page as a Form XObject,

    preserving its exact appearance (text, fonts, vector art, even images) —

    vector, not a raster screenshot. You can use the same document (a preserved

    untouched copy) as the source, clip the region that must move (all rows below

    the deletion), and draw it at the destination rectangle shifted up by one row

    height. The crucial ordering rule: redact/erase the original region first (or

    else the moved copy is stamped over the still-present originals and the rows

    appear duplicated). Rasterizing with `get_pixmap` and re-inserting as an image

    is a fallback but loses text quality/selectability.

    '
- id: pdf-rowdel-04
  answer: 'Besides the row itself: the Total (and any subtotal / amount-due / balance),

    because it is computed from the rows; tax lines if present; any other derived

    figures or counts (item count, running balances); the table''s own outlines —

    the bottom border and the vertical rules must be shortened by one row height,

    and the remaining rows'' shading/rules redrawn at their new y positions; the

    line numbers/row ordering of the rows below if they are numbered; anything

    positioned directly under the table (notes, terms, signatures, footer) if it

    is meant to sit under the table; and any summary or continuation of the same

    invoice on a later page. Also verify no content shifted off the bottom margin.

    If the document repeats totals elsewhere, update those too.

    '
- id: pdf-cell-01
  answer: "Steps to replace one right-aligned number so it looks native:\n1. Locate the value: `page.get_text(\"\
    words\")` (or the table cell from\n   `find_tables()`) to get its bbox; from `get_text(\"dict\")`\
    \ get the\n   containing span for font, size, color and `origin` (the baseline).\n2. Build a redaction\
    \ rect that stays strictly INSIDE the cell — covering the\n   old glyphs' full height and width but\
    \ not crossing the cell borders (so it\n   does not clip neighboring characters, since redaction removes\
    \ any char\n   whose bbox overlaps).\n3. `page.add_redact_annot(rect, ...)` then `page.apply_redactions()`.\
    \ To keep\n   the cell's borders and shading, call it with `graphics=0, images=0`; if you\n   also\
    \ redraw the grid, defaults are fine but you must repaint.\n4. Insert the new text right-aligned:\
    \ right edge = the old value's bbox x1\n   (minus design padding); width = `pymupdf.get_text_length(new,\
    \ fontname,\n   fontsize)`; `x = right_edge - width`; y = the span's baseline (`origin[1]`).\n   Use\
    \ `insert_text` with the same font/size and `color`. If using\n   `insert_textbox`, check its return\
    \ (negative = nothing written).\n5. Save to a new file (`garbage=4, deflate=True`) and re-extract\
    \ to confirm the\n   new value and that the neighboring cells are intact.\n"
- id: pdf-cell-02
  answer: 'Keep the redaction rectangle strictly inside the target cell: cover the glyph

    bbox (including ascenders/descenders and the full run width) but not the cell

    border, and leave a small inset from the cell edges. Reason: `apply_redactions`

    removes EVERY character whose bbox merely overlaps the rectangle. A padded or

    oversized rect will therefore overlap and destroy characters in the adjacent

    cells (their edge glyphs), silently corrupting neighbors. Because ASCII digit

    bboxes are tight, pad vertically to the line height but keep horizontally

    within the old value''s own extent plus a hair. After applying, re-extract the

    adjacent cells to prove they were not damaged.

    '
- id: pdf-cell-03
  answer: '`insert_textbox(rect, text, ...)` returns a float. If it is >= 0 it is the

    unused (spare) height; if it is negative, the text did NOT fit, and then

    nothing is written at all — no truncated string and no exception. So you must

    check the return value every time. Options when it doesn''t fit: reduce the

    font size slightly; wrap the text onto more lines and make the row taller

    (shifting the rows below down); widen the column by re-laying out the table;

    or use `insert_htmlbox(rect, text, css=...)`, which scales the text down to

    fit (default `scale_low=0`) and returns `(spare_height, scale)`. Never let the

    text spill over the cell border.

    '
- id: pdf-cell-04
  answer: 'In PyMuPDF, `page.insert_text(point, ...)` treats `point` as the text ORIGIN,

    i.e. the baseline start point (left end of the baseline), not the top-left

    corner of the glyph box. The y coordinate is the baseline y, and glyphs sit

    above it by their ascender. If you pass the top-left corner of the old word''s

    bbox (`(x0, y0)`), the text is drawn one full ascender too high (roughly one

    line height up), so it will not sit on the original baseline — it floats above

    the line. Use `x = bbox.x0` and `y =` the original baseline (e.g. the span''s

    `origin[1]`, or bbox.y1 minus the descender).

    '
- id: pdf-relayout-01
  answer: "Inserting a row above the Total means everything from the Total downward must\nmove down by\
    \ one row height. Correct handling:\n- Redact the region that moves (the old Total row, notes, signatures,\
    \ whatever\n  sits below) so it isn't duplicated, apply redactions.\n- Redraw or stamp that content\
    \ one row lower. The Total row itself: recompute\n  it if the new row changes the total, and draw\
    \ it at its new y.\n- Extend the grid: add the new row's horizontal rules and its cell shading and\n\
    \  text; lengthen the table's vertical side rules and any column separators by\n  one row height at\
    \ the top, and move the table's bottom border down one row\n  so the table remains closed.\n- If shifting\
    \ down pushes content past the page's bottom margin, continue the\n  table on a new page via `doc.new_page(pno)`\
    \ (inserted after the current\n  page), repeating the header, instead of overflowing the margin.\n\
    - Update the total/tax/balance figures and re-extract to verify.\n"
- id: pdf-relayout-02
  answer: 'Because the table already spans the full text width, you cannot add width; you

    must reclaim it by shrinking the existing columns so the new set of widths sums

    to the same total width (keeping the same left and right table edges).

    Correct approach: extract all the data; choose new column widths that sum to

    the original total; measure the widest text in each column (using

    `pymupdf.get_text_length` per candidate font/size) to validate the widths; then

    redact the WHOLE old table — text layer, shading and grid — and redraw

    everything: header fill, each row''s shading, all vertical and horizontal rules,

    and every cell''s text at the new x positions, matching the original font, size,

    colors and alignment (right-align numbers by measured width). The common

    mistake is to draw the new column/lines on top of the existing table without

    redacting first: the old text remains in the text layer (so the page still

    extracts the old numbers), the old and new text overlap visually, and the new

    column either spills over the margin or collides with the last column.

    '
- id: pdf-relayout-03
  answer: 'Acceptable only as a last resort, when exact visual fidelity does not matter (you just need
    editable text), or as a one-off conversion whose output you can fully re-verify. Before doing it,
    ask whether the original source exists (LaTeX/HTML/Word/reportlab/markdown/spreadsheet); regenerating
    from source is always better than PDF -> DOCX -> PDF.

    Risks: reflow and repagination; font substitution and metric changes (symbols/metrics change, line
    breaks move); loss or rasterisation of vector art, logos and rules; table borders/shading approximated;
    embedded images recompressed or duplicated; text reordered into different reading order; hyperlinks,
    bookmarks, forms, annotations, accessibility tags and metadata lost; the two-step conversion (e.g.
    via LibreOffice) silently changes layout in ways that are easy to miss. So treat the output as a new
    document to be re-checked, never assume a faithful round-trip.

    '
- id: pdf-relayout-04
  answer: 'Build the set of occupied rectangles and test your candidate rect against it. In PyMuPDF, collect
    everything on the page that could be covered:

    - text: page.get_text("words") (or "blocks"/"dict") -> word/line bboxes;

    - vector art: page.get_drawings() -> item["rect"];

    - images: page.get_image_info() / page.get_image_rects() (and page.get_bboxlog(), which also reveals
    nested/inline images and text);

    - annotations, links, form widgets: page.annots(), page.links(), page.widgets().

    Then pick a candidate rect (anchored to real content, not page.rect minus a fixed margin) and require
    that it does not intersect any of those bboxes (fitz.Rect.intersects) and lies inside the page''s
    usable area above the footer. Be conservative: add padding, respect page rotation/MediaBox, and re-check
    after any shift because moving content changes the free space. Rendering the page to a pixmap and
    looking at it is the final confirmation.

    '
- id: pdf-tblins-01
  answer: 'Two practical approaches in PyMuPDF:

    1) Draw it by hand with primitives: page.draw_rect(..., fill=...) for the header/body cell fills,
    insert_textbox / insert_text for the cell text, and page.draw_line for the rules, in the order fills
    -> text -> rules (a fill drawn after the text hides it). Compute x positions from the existing content''s
    left/right edges and measure/right-align numbers with pymupdf.get_text_length. Check every insert_textbox
    return value (negative = nothing written).

    2) Render the table as HTML and let PyMuPDF lay it out: build an HTML string with a <table> and call
    page.insert_htmlbox(rect, html, css=...) (or the Story API, story.place(rect)), which wraps text and
    scales it down to fit. A heavier variant is to render the table in a separate PDF (reportlab/HTML->PDF)
    and stamp it with page.show_pdf_page(rect, src_doc, pno, clip=...).

    Both must first establish a free rect (see pdf-relayout-04) that does not overlap existing content.

    '
- id: pdf-tblins-02
  answer: 'Borrow the document''s real style instead of inventing one:

    - font, size and colour from page.get_text("dict") spans on an existing table line (header text is
    often white, color 0xFFFFFF, on a dark fill);

    - rule width/colour and header fill from page.get_drawings() (keys width, color, fill);

    - x positions from the existing content''s left/right edges, with consistent cell padding.

    Use the same fill for header cells and the row shading for body rows, and right-align numeric columns
    by measuring width with pymupdf.get_text_length (right edge = column x1 minus padding). Draw fills
    first, then text, then rules, and check every insert_textbox return value so a cell is never silently
    empty. Avoid glitches by not stamping over existing content without redacting it first, by using insert_htmlbox
    when text does not fit, and by rendering the page afterwards to confirm nothing overlaps, is clipped,
    or is hidden behind a fill.

    '
- id: pdf-tblins-03
  answer: 'Options, roughly in order of preference:

    - Displace the following content: redact the region that moves and redraw/stamp it lower (or rebuild
    the page), making room for the table. Do it before drawing the table so the table is not duplicated.

    - Push the following content to a new page: insert a page after the current one (doc.new_page(pno))
    and move the paragraph(s) there, leaving the table at the current position.

    - Shrink the surrounding space: reduce font size/leading or paragraph spacing slightly, or tighten
    the preceding paragraph, until a gap exists (acceptable only if it does not harm the document).

    - Place the table on its own page and add a "see table on page N" reference if the layout cannot be
    disturbed.

    - If the original source is available, regenerate the document with the table inserted between the
    paragraphs.

    Whichever you choose, re-check for overlap (pdf-relayout-04) and re-extract/render to confirm.

    '
- id: pdf-imgrep-01
  answer: 'Locate the logo: page.get_images(full=True) gives xrefs; page.get_image_rects(xref) (or get_image_info())
    gives the rect(s) it is displayed at. Then either page.insert_image(rect, filename="new.png", overlay=True)
    at the same rect, or page.replace_image(xref, filename="new.png") to swap the underlying stream while
    keeping the same placement. If the old logo must actually be removed rather than covered, redact its
    rect with images=PDF_REDACT_IMAGE_REMOVE (and graphics as needed) before inserting the new one.

    The catch: images are shared by xref. The same logo xref is often reused on many pages/positions (or
    in a Form XObject), so replace_image changes every occurrence, not just page 1. Also a plain overlay
    leaves the old image bytes in the file (bloat and recoverable), and if the new PNG has transparency
    the old logo can show through. Check page.get_image_rects(xref) and image usage before deciding replace
    vs overlay.

    '
- id: pdf-imgrep-02
  answer: 'It is only an overlay: the new image is drawn on top, but the old logo is still present in
    the content stream and in the file. Consequences: the file is larger (duplicate image data), the old
    logo can be recovered by editing/removing the overlay, the original remains in the text/image layer
    for tools that enumerate images, and if the new PNG is transparent the old logo may partially show
    through. Z-order is fragile: any later redraw or reordering can reveal the old image. The correct
    fix is to redact/remove the old image (or replace its xref), then insert the new one, and save with
    garbage=4, deflate=True.

    '
- id: pdf-imgrep-03
  answer: 'get_images() only lists image XObjects in the page''s immediate /Resources /XObject, so a visible
    logo can be:

    - vector line art (page.get_drawings());

    - real text using a glyph/icon font (page.get_text("dict"), char c = a glyph);

    - an inline image (BI...EI) in the content stream, which get_images() does not report;

    - an image nested inside a Form XObject or inside an annotation''s appearance stream / a stamp annotation,
    which get_images() does not recurse into;

    - on an optional-content layer (OCG) or drawn by a shading pattern.

    Find it with page.get_bboxlog() (reports "image", "fill-path", "text" including nested content), page.get_drawings(),
    page.get_image_info(), page.get_text("rawdict"), and page.get_xobjects(). To replace: if it is vector/text,
    redact its bbox and redraw the new logo; if it is a nested/inline image, either redact the region
    and insert the new image on top, or edit the owning Form XObject/annotation appearance directly.

    '
- id: pdf-imgins-01
  answer: "Read the source PNG's pixel size to keep aspect ratio, e.g. with page.insert_image's keep_proportion=True,\
    \ or compute h = 150 * h_px / w_px. Anchor to content rather than page.rect: set the right edge to\
    \ the existing content's right edge (table/text x1) or the page's right margin, and the bottom just\
    \ above the footer's top (top of footer minus padding). rect = fitz.Rect(x1 - 150, y1 - h, x1, y1).\
    \ Then:\n  page.insert_image(rect, filename=\"sig.png\", keep_proportion=True)\nBefore inserting,\
    \ verify rect does not intersect any word, drawing or image bbox (see pdf-relayout-04); nudge up/left\
    \ if it does. overlay=True (default) puts it on top; check the return xref and re-render the page\
    \ to confirm placement.\n"
- id: pdf-imgins-02
  answer: "Insert the image once and reference it by xref on the other pages:\n  xref = page0.insert_image(rect,\
    \ filename=\"logo.png\")   # stores one image object\n  for p in doc[1:]:\n      p.insert_image(rect,\
    \ xref=xref)\n(Or draw it once into a Form XObject and reference that from every page.) If the file\
    \ is already bloated with identical copies, save with garbage=4, deflate=True, which merges identical\
    \ image streams and removes unused objects. Avoid passing filename= on every page, which creates a\
    \ new image object each time.\n"
- id: pdf-imgins-03
  answer: 'Likely causes:

    - Z-order: a filled background rectangle was drawn (or exists) after/over the image, so it hides it.
    insert_image defaults to overlay=True (on top); if you passed overlay=False the image goes under existing
    content and a background fill covers it. Draw the image after the fill, or use overlay=True.

    - The rect is wrong: zero/negative width or height, off-page, outside the visible CropBox, or inverted
    (x0>x1/y0>y1); keep_proportion with a mismatched aspect can also place it oddly.

    - Wrong page object, or the page is rotated and coordinates must be expressed in unrotated space.

    - The image stream is invalid/unsupported or the path/stream was not loaded (often raises, but a bad
    stream can render blank).

    - Invisible image: transparent PNG, same colour as background, or scaled to near-zero.

    Always check the xref returned by insert_image and render page.get_pixmap() to confirm the image actually
    appears and is on top.

    '
- id: pdf-fonts-01
  answer: '"AAAAAA+Georgia" is a subset (the six-letter prefix plus + marks it). A subset font embeds
    only the glyphs that were used when it was written, so any letter not in the original document has
    no glyph in that font. Reusing the subset for new text therefore produces missing glyphs/tofu, fallback
    substitution, or wrong spacing. Options:

    - Insert full text with a real font file: page.insert_font(fontfile="Georgia.ttf", fontname="Geo")
    (or insert_textbox with fontfile=..., or insert_htmlbox with a @font-face/system font), so all needed
    glyphs exist.

    - If exact match matters, obtain the original full font (from the OS/templates) and match size/colour;
    extract the embedded font with doc.extract_font(xref) only for reference, since the subset still lacks
    the new glyphs.

    - Re-render the whole affected cell/table with the full font so the new and old text match.

    Then re-extract the text and render to confirm the new letters show correctly.

    '
- id: pdf-fonts-02
  answer: 'PyMuPDF''s built-in base-14 shorthand names for Helvetica are "helv" (Helvetica, regular) and
    "hebo" (Helvetica-Bold). The full names "Helvetica" and "Helvetica-Bold" are also accepted. Related
    shorthands: heit/hebi (oblique/bold-oblique), tiro/tibo/tiit/tibi (Times), cour/cobo/coit/cobi (Courier),
    symb, zadb.

    Limitation: the base-14 fonts are not embedded and support only the PDF standard encodings (essentially
    WinAnsi/Latin-1). They cannot render arbitrary Unicode and have no glyphs for most non-Latin scripts
    (CJK, Cyrillic, Greek beyond the standard set) or many special/typographic characters; missing glyphs
    give wrong characters or blanks and rendering depends on the viewer''s own font. For non-Latin or
    special characters you must embed a real font (insert_font(fontfile=...) / insert_textbox(fontfile=...)
    / insert_htmlbox) whose cmap contains the needed codepoints.

    '
- id: pdf-fonts-03
  answer: 'Detect the original style from the text layer, not by eye: page.get_text("dict") gives spans
    with span["font"] (e.g. "AAAAAA+Georgia-Bold"), span["size"], span["color"] and span["flags"] (bit
    16 = bold, 2 = italic, 4 = serif, 8 = monospaced); get_text("rawdict") gives per-character detail.
    Read those values from the header cell''s existing spans (or a neighbouring header) and reuse the
    same font name, size, colour and weight. If the original is a subset bold font whose glyphs you need
    are missing, embed a full Georgia-Bold via insert_font(fontfile=..., fontname=...) and use that; do
    not simulate bold by overprinting. Set the same size (and leading) as the original and check via get_text("dict")
    after redrawing that the new span reports the same font, size and flags.

    '
- id: pdf-verify-01
  answer: 'Re-extract from the saved file, don''t trust the script. Concretely:

    - reopen the output PDF and run page.find_tables()/get_text() to assert the new text is present and
    the old text is absent (a simple "old string not in page.get_text()" check catches most failures);

    - verify positions/geometry against the surrounding content (does the cell/row sit where expected,
    no overlap);

    - render page.get_pixmap(dpi=150) and inspect it visually (and/or OCR) for overlap, clipping, tofu
    and hidden text behind fills;

    - confirm the numeric total, that no duplicate rows were stamped, and that neighbouring cells are
    intact;

    - validate the file structurally (qpdf --check / mutool / the docx skill''s validate for docx) and
    open it in more than one viewer (Acrobat/Chrome/pdf.js) to ensure no "repair" prompt;

    - compare page count and, for text edits, use difflib on the extracted text to confirm only the intended
    change.

    '
- id: pdf-verify-02
  answer: 'No, not reliably. incremental=True appends a new revision to the same file and keeps the original
    bytes; the earlier revision (including the confidential row) is still physically present and can be
    recovered by tools that read previous revisions, by carving the file, or by opening with a parser
    that does not follow the update chain. Redaction does remove the characters from the current content
    stream, but the old content survives in the file''s earlier revision. To actually remove it, save
    a full rewrite to a NEW file with garbage collection, e.g. doc.save("clean.pdf", garbage=4, deflate=True,
    clean=True) (non-incremental), then verify by extracting text and grepping the file''s raw bytes/hidden
    streams for the removed string.

    '
- id: pdf-verify-03
  answer: '- Never edit the original in place. Open it read-only, save the result to a new file (or a
    temp file), and only replace the original after verification (atomic rename); keep a backup copy of
    the original bytes (and a hash) until the user accepts the result.

    - Make the source read-only if there is any risk of accidental overwrite; avoid `doc.save(same_path)`
    on the file currently open, especially with incremental writes that leave old revisions.

    - Work in a unique scratch directory (avoid shared /tmp) and don''t clobber other files.

    - Confirm destructive changes (row deletion, redaction, overwriting the invoice) with the user; redaction
    is irreversible and changes totals/meaning.

    - After saving, re-open and verify (text extraction, render, structural check, page count) before
    declaring success; report exactly what changed.

    - Handle passwords/permissions explicitly (doc.needs_pass, authenticate) and preserve metadata only
    if appropriate.

    '
- id: pdf-verify-04
  answer: 'Because "no exception + printed done" only proves the script reached the end, not that the
    PDF content is right. PyMuPDF fails quietly in several ways: insert_textbox returns a negative value
    and writes NOTHING (no exception) when text doesn''t fit; a text.replace on the raw bytes can no-op
    because strings are split across runs; a redaction may remove too little (neighbouring chars survive)
    or too much (shading/rules or adjacent cell characters eaten); stamping a shifted region while the
    original is still on the page duplicates rows; text can be present in the text layer but hidden behind
    a fill; a wrong font/encoding can render tofu; and the wrong page or file may have been written. Evidence
    must come from the saved file: re-open it, re-extract text (assert new present, old absent), check
    find_tables/geometry, render a pixmap and look at it, run a structural validator (qpdf/mutool), and
    open in a viewer. Only then is the edit verified.

    '
