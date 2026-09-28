- id: pdf-model-01
  answer: 'A PDF has no "table" object. The page content stream is just a flat list of

    drawing operators: text-show operators (Tj/TJ positioned by Tm/Td/Tm matrices)

    for the cell text, path/line operators for the rules, and filled rectangles

    (re f) for the shading, in arbitrary z-order, possibly spread across several

    content streams, Form XObjects, or compressed streams. There is no structure

    that knows where "row 3" begins or ends, and no object to delete.

    Implication: the edit must be positional, not structural. You must locate the

    geometry of the third row (its rules, shading fill and text bboxes), remove

    the glyphs and line-art in that band, shift the rows below up by one row

    height (re-typing them or stamping from an untouched copy), and redraw the

    affected grid lines. Everything is done against coordinates, not against a

    table model.

    '
- id: pdf-model-02
  answer: 'Drawing a white (or background-coloured) rectangle only paints above the

    content in z-order; the original glyphs are still in the content stream and

    still in the text layer, so the text remains extractable, searchable,

    copyable, and visible when the paint is removed or in text-based tools.

    It is a cover-up, not an edit — and if you then type the replacement on top

    (e.g. in a later move operation) the old text can be duplicated.

    The proper mechanism is redaction: in PyMuPDF, page.add_redact_annot(rect)

    followed by page.apply_redactions(), which actually removes the covered

    characters (and, by default, covered vector graphics and image pixels) from

    the content stream. Then insert the new text with insert_text/insert_textbox

    at the correct baseline. Apply redaction before placing new content, never

    after.

    '
- id: pdf-model-03
  answer: "Several reasons:\n1. Streams are usually compressed (FlateDecode), so the bytes you see are\
    \ not\n   the text at all until decoded; pikepdf can decode, but raw byte editing\n   must then re-encode\
    \ consistently.\n2. The string may not appear contiguously: it can be split across multiple\n   show\
    \ operators (e.g. \"22\" then \"50\", or a TJ kerning array with the digits\n   separated by adjustment\
    \ values), use hex strings <...>, or be split by\n   positioning operators between characters.\n3.\
    \ The font may use a subset encoding with custom glyph codes (only mapped to\n   Unicode via a /ToUnicode\
    \ CMap), so the bytes in the stream are not ASCII\n   \"22.50\" at all.\n4. Even if you find and replace\
    \ the bytes, the new text has a different width;\n   the glyph widths/kerning in the stream no longer\
    \ match, so the result can\n   overlap or misalign, and if you change the length of any operator's\
    \ byte\n   content incorrectly you can corrupt offsets (xref/stream lengths) and break\n   the page.\n\
    Practical result: plain search-and-replace on stream bytes usually fails to\nfind the target or produces\
    \ a visually/structurally broken page. Higher-level\nremoval (redaction) plus re-insertion at the\
    \ right baseline is the reliable\nroute.\n"
- id: pdf-model-04
  answer: "PyMuPDF deliberately normalizes coordinates: its page coordinate space has the\norigin at the\
    \ TOP-LEFT of the page's CropBox (the visible page), the y axis\nincreases DOWNWARD, and it accounts\
    \ for /Rotate — i.e. you work in the visual\n(rotated) orientation of the page as displayed, and PyMuPDF\
    \ applies the\nderotation/transformation internally when drawing. Raw PDF coordinates, in\ncontrast,\
    \ have the origin at the BOTTOM-LEFT of the MediaBox with y increasing\nupward, and are unaffected\
    \ by /Rotate.\nConsequences:\n- A bbox from get_text(\"words\") can be handed directly to drawing\
    \ functions;\n  they share the same PyMuPDF coordinate space, so no conversion is needed as\n  long\
    \ as everything stays in PyMuPDF coordinates.\n- If the CropBox is offset from the MediaBox, PyMuPDF\
    \ coordinates are relative\n  to the CropBox (the visible page), not the MediaBox; mixing in raw PDF\n\
    \  numbers would be shifted by the CropBox offset.\n- With /Rotate (e.g. 90), a raw PDF point and\
    \ a PyMuPDF point for the same\n  visual location differ by a rotation plus origin change; converting\
    \ between\n  the two requires the page's transformation matrices\n  (page.derotation_matrix, page.transformation_matrix,\
    \ or combining them).\nIf you ingest coordinates from a raw-PDF source, transform them into PyMuPDF\n\
    space first; otherwise text will land in the wrong place, especially on\nrotated or cropped pages.\n"
- id: pdf-locate-01
  answer: 'In PyMuPDF the easiest route is page.find_tables(), which performs structural

    table detection and returns Table objects; each has table.bbox, a rows

    collection where each row carries its own bbox and the individual cell

    bboxes (row.cells), plus header/extent info — i.e. geometry, not just text.

    Alternatively (or as a fallback for odd tables) build it yourself from vector

    data: page.get_drawings() returns every path with its rect/line segments;

    collect horizontal segments to get the row boundary y values and vertical

    segments for the column x values, cluster them into a grid, then intersect

    the grid with the text word bboxes from page.get_text("words") to associate

    each word with a cell. The union of a row''s adjacent horizontal rules (and

    the table''s left/right extent) gives the full row region including borders

    and shading, not merely the text.

    '
- id: pdf-locate-02
  answer: 'search_for returns only the bbox of the matching text (the words "Widget C"),

    which is just one cell''s text. Deleting that rect alone would leave the

    row''s other cells, the horizontal rules bounding the row, and the row''s

    shading in place, and the rows below would still need to move up — so the

    text bbox is neither the row region nor the region that must be redacted.

    To get the full row region: use find_tables() and take the row''s bbox (which

    spans the table width between its rules); or derive the band from the vector

    layer — take the text''s y range from the search result and extend it to the

    nearest horizontal rules above and below (from get_drawings), spanning the

    table''s full width. The removal region is that whole band (plus, for a row

    deletion, the entire area below it that will move).

    '
- id: pdf-locate-03
  answer: 'Use page.get_text("dict") (or "rawdict"): it returns blocks → lines → spans,

    and each span carries font (name), size, flags (bit flags for bold/italic/

    superscript etc.), colour (an integer sRGB value — convert to RGB via

    bit-shifting), bbox, and origin — the baseline start point of the span, which

    is exactly what insert_text needs. Ascender/descender values are also

    included. For even lower-level detail (per-glyph, including exact fill

    colour) there is page.get_texttrace(). From a matching span you can then

    reproduce the style: same fontname/size/colour for insert_text, and the

    span''s origin as the baseline; for right-aligned numbers keep the same

    baseline y and set x so the new text''s right edge matches the old one

    (e.g. x = old_bbox.x1 - fitz.get_text_length(new_text, fontname, size)).

    '
- id: pdf-locate-04
  answer: 'Column boundaries come from the table geometry, not the text: either

    find_tables() (each cell/column carries its bbox and the column x edges), or

    the vector layer via page.get_drawings() — the vertical rule segments give

    the column x positions and the horizontal rules the row boundaries. The

    right-edge x of a right-aligned number is simply the right side of its text

    bbox: page.get_text("words") gives per-word bboxes (x1 is the right edge),

    and get_text("dict") gives span bboxes plus origins. For an edited value to

    line up, keep the old baseline y (span origin) and set the new start x as

    right_edge - fitz.get_text_length(new_text, fontname, fontsize). Consistency

    across rows can be verified by checking that all the numbers in a column

    share approximately the same x1 (or the same origin.x) — that shared value is

    the column''s alignment edge.

    '
- id: pdf-rowdel-01
  answer: "A correct procedure (PyMuPDF):\n1. Detect the table (page.find_tables() or the grid from get_drawings())\
    \ and\n   identify the row to delete, its bbox, the rows below it, and the Total\n   row/value.\n\
    2. Keep an untouched copy of the document open (a duplicate doc object) to\n   stamp from later.\n\
    3. add_redact_annot over the deleted row AND over the entire region that\n   moves (from just below\
    \ the deleted row down to the bottom of the\n   table/Total row — or simply the whole table) so old\
    \ text and old grid\n   disappear.\n4. apply_redactions() with defaults: default text removal clears\
    \ the glyphs,\n   and default graphics handling (remove line-art covered by the redaction)\n   clears\
    \ the rules/shading in the band, so no stale grid or text remains.\n5. Redraw the surviving rows one\
    \ row-height higher: either re-draw text,\n   rules and alternating shading manually, or stamp from\
    \ the untouched copy\n   with page.show_pdf_page(new_rect, src_doc, pno, clip=old_rect) for each\n\
    \   moved row (redact first, stamp second — never the reverse, or text gets\n   duplicated).\n6. Recompute\
    \ and rewrite the Total: redact the Total cell (inset, with\n   graphics/images preservation if you\
    \ keep its grid), then insert_text at\n   the original baseline, right-aligned to the column edge,\
    \ matching font/\n   size/colour.\n7. Save to a new file and re-extract the text to verify nothing\
    \ is duplicated\n   and the grid is clean.\n"
- id: pdf-rowdel-02
  answer: 'In current PyMuPDF, apply_redactions() defaults are images=

    PDF_REDACT_IMAGE_PIXELS (=2: blank out the image pixels that fall inside the

    redaction rect), graphics=PDF_REDACT_LINE_ART_REMOVE_IF_COVERED (=1: vector

    line-art whose covered rect falls within the redaction rect is REMOVED), and

    text=PDF_REDACT_TEXT_REMOVE (=0: characters whose bbox overlaps the rect are

    removed). (Older versions defaulted images to PDF_REDACT_IMAGE_NONE; the

    pixel default came later.)

    Why it matters: when you redact a table row or cell, the rect usually also

    covers parts of the grid — the horizontal/vertical rules and the shading.

    With the defaults, that line-art and shading are deleted along with the

    text: exactly what you want when deleting a whole row (the old grid must

    vanish before rows move up), but harmful when editing a single cell, where

    the default would eat the cell''s borders and fill. For cell edits pass

    graphics=0 (PDF_REDACT_LINE_ART_NONE) and images=0 (PDF_REDACT_IMAGE_NONE)

    and inset the rect so it doesn''t touch the rules. "Vector rules are not

    removed by default" is a common misconception — they are.

    '
- id: pdf-rowdel-03
  answer: 'Yes — Page.show_pdf_page(). It embeds a region of another page (typically an

    untouched copy of the same document) into a target rectangle of the current

    page, using the source page as an XObject; with the clip parameter you select

    exactly the source region. So: redact (remove) the deleted row and the whole

    region that moves on the working page; then, for each row that must shift,

    call page.show_pdf_page(rect_one_row_higher, src_doc, pno, clip=old_row_rect)

    on the untouched copy. The stamped region keeps its original appearance —

    fonts, rules, shading — because it is the original content rendered into the

    new position. The order matters strictly: remove first, then stamp; stamping

    before redacting leaves the original text underneath and every moved row

    appears twice in extraction.

    '
- id: pdf-rowdel-04
  answer: 'Besides the row itself: the Total row (the grand total must be recomputed,

    and the Total row moves up one row height); any per-column subtotals or tax

    lines that depend on line amounts; the row numbering or item references

    elsewhere in the document (e.g. "3 items", line numbers on later pages);

    footers/headers if the table is paginated and content reflows (page count,

    "continued" markers, totals repeated per page); running balances or credit

    limit/amount-due fields tied to the total; and any cross-references such as

    payment schedules or summary pages. Also the table''s alternating shading and

    the bottom rule must move with the Total row so the grid stays coherent. In

    short: anything computed from the deleted line''s amount, plus everything

    positioned below the table on the page.

    '
- id: pdf-cell-01
  answer: "Steps for a native-looking single-cell number replacement:\n1. Read the current cell's style\
    \ and position: page.get_text(\"dict\") → find\n   the span for \"22.50\"; record font, size, colour,\
    \ origin (baseline),\n   bbox, and the cell's right alignment edge (span bbox x1 or the column\n \
    \  edge from the table geometry).\n2. Redact only the text: add_redact_annot on a rect slightly INSET\
    \ inside\n   the cell (covering the number's bbox but not the borders), then\n   apply_redactions(graphics=PDF_REDACT_LINE_ART_NONE\
    \ (0),\n   images=PDF_REDACT_IMAGE_NONE (0)) so the cell borders and shading\n   survive; the glyphs\
    \ are removed from the text layer.\n3. Insert the new text with page.insert_text(point, \"54.00\"\
    , fontname=old\n   font, fontsize=old size, color=old colour) at the OLD origin (baseline)\n   — for\
    \ a right-aligned value set x = old_right_edge -\n   fitz.get_text_length(\"54.00\", fontname, fontsize),\
    \ y = old baseline y.\n4. Save to a new file and re-extract to confirm the old value is gone, the\n\
    \   new value is in place, and the borders are intact.\n"
- id: pdf-cell-02
  answer: 'Shrink the rectangle so it covers the text but not the cell''s borders or the

    neighbouring cells: inset it inside the cell by a small margin (a couple of

    points, or just beyond the glyph bbox), keeping clear of the horizontal and

    vertical rules and of any text that spills near the cell edge. Reasons:

    (a) apply_redactions removes any character whose bbox overlaps the rect — a

    rect that reaches into a neighbouring cell or touches a glyph hanging over

    the edge can delete characters that should stay; (b) with the default

    graphics setting, any rule covered by the rect is removed — a full-cell rect

    would delete the cell''s own borders and shading; with graphics=0 you avoid

    that, but an inset rect is still the safer shape because it leaves the

    borders untouched regardless of the graphics option. Tight tables leave very

    little slack, so prefer the smallest rect that fully overlaps every glyph

    you want gone.

    '
- id: pdf-cell-03
  answer: 'insert_textbox fits text inside a given rectangle with word wrapping; when

    the text cannot fit, it returns a NEGATIVE value (the negative of the

    leftover deficit / an error indicator) and does NOT insert the text — the

    call fails gracefully rather than overflowing the box. Your options:

    enlarge the rectangle (if the layout allows), reduce the font size (compute

    a size that fits, e.g. by iterating or using fitz.get_text_length for a

    single line), shorten or abbreviate the text, wrap the content yourself

    across multiple cells/lines, or abandon the textbox and use insert_text

    (TextWriter) which places a single line without fitting constraints — but

    then YOU are responsible for any overflow, which in a table usually means

    overlapping the next column, so a smaller font or a wrapped multi-line cell

    (with increased row height, i.e. a relayout) is the correct table-safe

    choice.

    '
- id: pdf-cell-04
  answer: 'point is the BASELINE start: the bottom-left of the first glyph measured on

    the text baseline (PyMuPDF places text so that this point sits on the

    baseline; ascenders rise above it). If you pass the top-left corner of the

    old word''s bbox instead, the new text is drawn with its baseline roughly one

    line-height too HIGH — the bbox top is at the top of the ascender box, not

    on the baseline — so the replacement floats visibly above the row (about

    one full line of offset), misaligned with the neighbouring cells'' baselines.

    The fix is to use the original span''s origin from get_text("dict") (or

    baseline = bbox.y1 - descender) as the point.

    '
- id: pdf-relayout-01
  answer: "Adding a row just above the Total row forces a cascade:\n- The Total row itself must move DOWN\
    \ by one row height, and its Total\n  value must be recomputed to include the new row's amount.\n\
    - The table rules must be redrawn: the new row needs its horizontal rules\n  (top and bottom) and\
    \ the vertical column rules continued through it so\n  the grid connects; the Total row's rules move\
    \ with it, and the table's\n  bottom rule ends at the Total row's new lower position.\n- Any alternating\
    \ row shading pattern must be continued consistently\n  through the inserted row and the shifted Total\
    \ row.\n- Content below the table (notes, footers, signature blocks, following\n  sections, or rows\
    \ that spill to the next page) must shift down by one row\n  height, and page-dependent artefacts\
    \ (footers, page numbers, \"continued\"\n  markers, per-page subtotals) may need updating if the reflow\
    \ pushes\n  content across a page boundary.\nMechanically in PyMuPDF: redact the Total row (and anything\
    \ below it that\nmoves) to remove the old text and grid, then stamp/redraw the Total row one\nrow\
    \ lower from an untouched copy, draw the new row's text/rules/shading in\nthe vacated band, and rewrite\
    \ the Total number at its new baseline, right-\naligned to its column edge.\n"
- id: pdf-relayout-02
  answer: 'Since the table already fills the full width between the margins, a new

    column must be created by REDISTRIBUTING the existing width: narrow the

    existing columns (proportionally, or weighted by their content) so the sum

    of column widths equals the old table width, recompute all column x

    boundaries, and then rebuild the affected region — redact the whole table

    area (or the columns that changed) and redraw rules and text, or stamp the

    untouched content into the new column positions with show_pdf_page clip

    regions. The new column''s header and values are then drawn in their new

    cell, right-aligned to the new column''s alignment edge.

    The common mistake: trying to squeeze the new column into existing space by

    drawing it ON TOP of the current table — the new column''s text overlaps the

    existing cells'' text, the vertical rule cuts through existing values, the

    table now exceeds the margins or collides with the next column, and

    right-aligned numbers in shifted columns no longer line up under their

    headers. In short: re-layout the columns first (remove, then redraw/stamp),

    never overlay.

    '
- id: pdf-relayout-03
  answer: 'Acceptable when the original appearance does not need to be preserved: the user

    really wants new content/data, the "document" will be regenerated from a true

    source anyway (Word, InDesign, a template), or fidelity loss is explicitly

    tolerable (draft, internal reuse). Also acceptable if you can round-trip and

    the result is judged acceptable against a visual diff.

    Risks: PDF is a fixed-layout render format; DOCX/HTML are flow formats, so the

    conversion re-flows everything — fonts get substituted or re-embedded, spacing,

    kerning, page breaks, page count and margins shift; tables can be mangled

    (merged cells, borders, shading, column widths), images recompressed or

    repositioned, vector art degraded, annotations/links/metadata/form fields

    dropped or altered. The result is a new document, not the same document, and

    it is rarely pixel- or even layout-faithful. For invoices/records where the

    look must match, it is not acceptable.

    '
- id: pdf-relayout-04
  answer: 'PyMuPDF has no built-in "free space" detector, so you collect the bounding

    boxes of everything on the page and test your candidate rect against them:

    text (page.get_text("blocks")/"dict"), vector art (page.get_drawings()),

    images (page.get_image_info(xrefs=True) or get_image_rects per xref), and

    annotations (page.annots()). Build a list of rects, then check the proposed

    rect with rect.intersects(other) for every one; also clip against page.rect

    (CropBox) and respect the document''s visual margins. For a quick bottom-of-page

    check, take the maximum y1 over all content rects and compare it to the bottom

    margin. For gaps mid-page, look at vertical bands where no bbox crosses, or

    scan column-by-column. Remember that white-looking space may still contain

    invisible content (white text, covered images), so check the object list, not

    just a rendered screenshot.

    '
- id: pdf-tblins-01
  answer: 'Way 1 — draw it natively with PyMuPDF primitives: compute row/column rects,

    page.draw_line()/draw_rect() for the rules and any cell shading, and

    insert_text()/insert_textbox() for the cell text (base-14 or embedded font).

    Full control, but you must handle padding, alignment and baseline yourself.

    Way 2 — stamp or render a prepared table: either page.insert_htmlbox(rect,

    "<table>…</table>", css=…) (PyMuPDF 1.23+, supports borders, shading, real

    table layout), or build the table with reportlab (Table/ TableStyle) into a

    small overlay PDF and stamp it onto the page with page.show_pdf_page(rect,

    overlay_doc, 0). The stamping approach keeps the table drawing logic out of

    the target document.

    '
- id: pdf-tblins-02
  answer: 'Match the document''s existing style: read fonts/sizes/colors of nearby text

    with page.get_text("dict") spans and reuse the same font (embedded subset only

    if it contains your glyphs; otherwise the closest base-14/embedded match);

    read the line widths and stroke colors of existing tables via

    page.get_drawings() and reuse them for rules; align column x-positions to the

    document grid/margins; keep consistent cell padding and row heights; use the

    same color space conventions; right-align numbers on a shared baseline.

    Avoid glitches: place the rect in verified free space (no overlaps), keep

    coordinates sensible (avoid sub-pixel jitter from odd floats), draw fills

    before strokes/text so they don''t cover, and verify by re-extracting the text

    and rendering the page to a pixmap for visual comparison.

    '
- id: pdf-tblins-03
  answer: 'Options, roughly in order of preference: (1) insert a new page after the

    current one and put the table there (cleanest if a page break is acceptable);

    (2) free space by shifting content down — redact/remove the region below the

    insertion point and re-stamp it lower (show_pdf_page with clip from an

    untouched copy), i.e. the remove-then-redraw pattern, which is complex and

    risky; (3) rewrite/shrink the surrounding text (shorten the paragraphs) so a

    gap appears; (4) place the table elsewhere on the page (margin/white space)

    with a pointer note; (5) reduce the table so it fits an existing gap.

    If none is acceptable, go back to the user — silently overlapping text is

    never an option.

    '
- id: pdf-imgrep-01
  answer: 'Find the image: page.get_images(full=True) gives items with xrefs; get the

    placement rect(s) with page.get_image_rects(xref) or get_image_bbox to confirm

    it is the logo and where it sits. Then swap the image object in place:

    page.replace_image(xref, filename="new.png") (PyMuPDF ≥1.21) — this replaces

    the underlying image stream, so position, size and transform are preserved

    automatically.

    The catch with shared images: PDFs deduplicate, so one image xref may be

    referenced by many pages (or by Form XObjects). replace_image replaces the

    object itself, so every page that references that xref gets the new logo too.

    If only one page must change, you need a different approach: create a copy of

    the image object (e.g. insert a new image first, then delete/blank the old

    reference on that page only) rather than mutating the shared xref.

    '
- id: pdf-imgrep-02
  answer: 'The old logo is still in the file — you only covered it. It remains in the

    image objects and page resources, so get_images()/extractors still see it, the

    file keeps the extra data, the old logo can be recovered (a problem if it was

    confidential or wrong branding), and anything that changes z-order, uses

    transparency, or is smaller than the rect lets the old logo show through.

    The correct method is replace_image (removes/swaps the object) or deleting the

    old image reference properly, not layering a new image on top.

    '
- id: pdf-imgrep-03
  answer: 'get_images() only lists images in the page''s immediate resource dictionary.

    The logo may be: inside a nested Form XObject (very common for headers/logos),

    an inline image (BI/ID/EI) in the content stream, or not a raster image at all

    but vector line-art (in which case it appears in page.get_drawings() and you

    must redact/redraw it, not replace_image).

    To find it: page.get_image_info(xrefs=True) walks the display list and reports

    shown images with their bboxes and xrefs even when nested in XObjects (also

    try get_images(full=True) first, and doc.xref_object checks if needed). Once

    you have the real xref and bbox, use page.replace_image(xref, filename=…) or,

    for a vector logo, remove it via redaction and stamp the replacement with

    show_pdf_page at the same rect.

    '
- id: pdf-imgins-01
  answer: 'Compute the rect from the image''s aspect ratio: open the PNG (fitz.Pixmap or

    PIL) to get native width/height, aspect = h/w; target width w = 150, so

    h = 150 * aspect. Place it with margins, e.g. x1 = page.rect.width - 50,

    x0 = x1 - 150, y1 = page.rect.height - 50, y0 = y1 - h → rect = fitz.Rect(x0,

    y0, x1, y1). Then check it doesn''t cover content: collect text blocks,

    drawings and image bboxes on the page and confirm no intersection; shift it

    up/left if needed. Insert with page.insert_image(rect, filename="sig.png")

    (keep_proportion=True is the default; PNG alpha/SMask is honored). Finally

    save to a new file and verify by rendering the page.

    '
- id: pdf-imgins-02
  answer: 'Insert the image only once and reuse its xref: call page.insert_image(rect,

    filename="logo.png") on the first page and capture the returned xref; for

    every other page call page.insert_image(rect, xref=that_xref). This stores

    one image object in the file and just adds new references, so the file grows

    by a few bytes per page instead of 200 copies. (Recent PyMuPDF versions also

    deduplicate identical images automatically, but relying on the explicit xref

    reuse is the safe pattern — and if your version duplicated, the xref approach

    fixes it.)

    '
- id: pdf-imgins-03
  answer: 'Not appearing: the rect is invalid (zero/negative width-height), outside the

    visible page area (CropBox/MediaBox mismatch or wrong page), the image data

    is unsupported/corrupt, an alpha/SMask handling problem, or the insert

    actually landed on a different page. Also check you saved the document and

    are looking at the saved file.

    Appearing underneath a filled background box: z-order — insert_image places

    the image at the end of the page''s content stream only when overlay=True;

    with overlay=False it prepends the content and the image is drawn underneath

    everything. Also, if the page background is painted by a Form XObject, a

    later content stream, or an annotation/OCG layered above, the appended image

    can still end up underneath; and a page with multiple content streams may

    receive the image in an earlier one. Fix: insert with overlay=True, verify

    content-stream order, and check nothing later (XObjects, annotations,

    OCGs/optional content) paints over it.

    '
- id: pdf-fonts-01
  answer: 'The "+" prefix means the font is a subset: only the glyphs actually used in

    the document were embedded. Text containing a character that never appeared

    has no glyph in that subset, so reusing the font renders nothing/garbage/notdef

    boxes for those characters — often silently. Do not reuse the subset for new

    text. Instead: use a visually close base-14 font (e.g. helv/tiro) if close

    enough, or embed the real (full) font file from the system with

    fitz.Font(fontfile=…) and insert text with it; optionally call

    doc.subset_fonts() at the end to re-subset and keep file size down.

    '
- id: pdf-fonts-02
  answer: 'PyMuPDF reserved names: Helvetica regular = "helv", Helvetica bold = "hebo"

    (the full names "Helvetica"/"Helvetica-Bold" are also accepted). Other base-14

    reserved names: heit/hebi (Helvetica italic/bold-italic), tiro/tibo/tiit/tibi

    (Times family), cour/coit/cobo/cobi (Courier family), symb (Symbol),

    zadb (ZapfDingbats).

    Limitation: base-14 fonts have no embedded glyph data beyond the standard

    encoding — effectively Latin-1/WinAnsi text only. Non-Latin scripts (CJK,

    Arabic, Cyrillic, Devanagari…) and many special characters/emoji are not

    available; you must embed a suitable font file (e.g. a TTF via

    fitz.Font(fontfile=…) or PyMuPDF''s optional font packages) for anything

    outside that range.

    '
- id: pdf-fonts-03
  answer: 'Detect the original style from page.get_text("dict") spans: span["font"] shows

    the font name (a bold variant is usually in the name, e.g. "…+Georgia-Bold",

    or indicated by span["flags"], where the bold bit is 16), span["size"] gives

    the exact fontsize, and span["color"] the fill color.

    Match it: use the same fontsize from span["size"]; for weight, reuse the

    document''s bold font only if it is an embedded subset that contains your new

    glyphs — otherwise use the matching base-14 bold name (hebo for Helvetica

    bold, tibo for Times bold) or embed a matching bold font file. Take the

    insertion point from span["origin"] (baseline) and use

    get_text_length(text, fontname, fontsize) to right-align if needed. Verify by

    re-extracting spans and comparing font/flags/size to neighbors.

    '
- id: pdf-verify-01
  answer: 'Verify against the saved output file, not your in-memory doc: (1) re-open the

    saved PDF fresh; (2) extract text (get_text) and assert the new value is

    present and the old value is absent, with no duplicated rows/lines; (3)

    re-run find_tables() and compare the extracted table data to the expected

    table; (4) check geometry via get_text("dict") spans (position, font, size,

    baseline); (5) render the page to a pixmap and visually inspect (or pixel-diff

    against an expected render) to catch layout/shading/rule problems that text

    extraction misses; (6) optionally check structure (get_fonts/get_images) for

    leftovers. Automate these as assertions in the script so "done" means

    "verified".

    '
- id: pdf-verify-02
  answer: 'No. An incremental save appends an update to the same file; earlier revisions''

    objects — including the original content stream with the confidential row —

    remain in the file and can be recovered with simple parsing tools. The text is

    gone from the *current* content stream, but not from the file as a whole.

    Instead: save to a NEW file with doc.save("out.pdf", garbage=4, deflate=True,

    clean=True) so unreferenced/old objects are removed, then verify by extracting

    text from the saved file to confirm the row is truly absent. Also note the

    original unredacted file still exists as delivered, so it must be removed from

    circulation too.

    '
- id: pdf-verify-03
  answer: 'Never edit the user''s original file in place: copy it to a working area, edit

    the copy, and save the result under a NEW name (never overwrite the input,

    and avoid incremental saves into the original). Keep the original immutable as

    the source of truth/audit reference. Write output atomically (temp file +

    rename) so a crash doesn''t leave a corrupt PDF. Verify the output (text/table

    extraction, render) before delivering. Watch for signed/encrypted PDFs —

    edits invalidate signatures or may be prohibited; flag rather than silently

    break them. If confidential content was removed, do a full save with garbage

    collection and prove removal by re-extracting. Keep a change log of what was

    edited, by which script, from which input.

    '
- id: pdf-verify-04
  answer: '"Script ran without errors" only proves the code executed, not that anything

    changed correctly. PyMuPDF rarely errors on logical misses: a redact_annot on

    an empty region, insert_text at wrong coordinates, an edit on the wrong page,

    a save to the wrong path, or a swallowed exception can all end with "done".

    The only meaningful evidence is the state of the output file: re-open the

    saved PDF and assert on extracted text/tables, span fonts/sizes/positions,

    rendered pixels, and absence of the old content. Make those assertions part of

    the script so success is defined by verified output, not by the absence of

    exceptions.

    '
