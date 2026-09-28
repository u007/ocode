- id: pdf-model-01
  answer: 'A PDF has no table object. The "table" is only (a) vector graphics — horizontal/vertical

    rules and shaded background rectangles drawn by path operators, and (b) individual text

    fragments, each positioned by its own text matrix and shown with Tj/TJ operators, one

    glyph run at a time. There are no rows, columns, or cells unless the producer emitted

    tagged-PDF structure (Table/TR/TD marked content), which most files lack.

    Implication: you cannot "delete row 3" as a data operation. You must locate the row''s

    region geometrically (text spans + the rules/shading bounding it), remove everything in

    that region for real (redaction annotation + apply_redactions, which also removes the

    rules and fills), then move all content below up by one row height — restamping it from

    an untouched copy with show_pdf_page — and recompute anything dependent (the Total).

    '
- id: pdf-model-02
  answer: 'Because a white rectangle is just another painted path: it only hides the text visually.

    The original glyphs remain in the content stream, so the text is still extractable,

    searchable, copy-pasteable and recoverable (even from a screenshot-plus-layer attack or

    by removing the white path), the file still carries the old fonts/strings, and printing

    or transparency/flattening behaviour can differ. It also fails on shaded backgrounds,

    where white visibly punches a hole, and it does not remove the old vector rules.

    Use actual redaction: page.add_redact_annot(rect) followed by page.apply_redactions(),

    which deletes the covered text, image pixels and (as configured) line art from the

    content stream, leaving a genuinely empty region to write into.

    '
- id: pdf-model-03
  answer: 'Several reasons: (1) content streams are usually FlateDecode-compressed, so the bytes

    you see are not plain until decompressed, and re-encoding changes stream length, which

    requires updating /Length or the xref. (2) The string rarely appears literally: text may

    be split across several Tj/TJ operators, kerned via TJ numeric arrays, or encoded with a

    subset font''s custom CMap / hex or CID codes, so "22.50" is not in the byte stream as

    ASCII at all. (3) A blind byte replace can hit the string in the wrong place (another

    page, a glyph-name table, metadata), can break operator/token boundaries, and can change

    byte count inside a stream whose declared length then no longer matches. (4) Even when

    it "works", the new glyphs may not exist in the embedded subset font, so widths render

    as garbage/tofu, and the right-aligned position is now wrong because it was computed for

    the old string''s advance width. Correct approach: locate the text object, replace it

    through the graphics layer (redact + reinsert with matching font/size/alignment).

    '
- id: pdf-model-04
  answer: "Units and origin. PyMuPDF works in points (1 pt = 1/72 in), the same\nunit as PDF user space,\
    \ but the axis convention is flipped: PyMuPDF's\norigin is the TOP-LEFT of the page area with y increasing\
    \ DOWNWARD,\nwhile raw PDF user space has its origin at the BOTTOM-LEFT with y\nincreasing UPWARD.\
    \ For a page whose visible area runs from (X0, Y0)\nto (X1, Y1) in raw PDF coordinates:\n\n    pdf_x\
    \ = X0 + x\n    pdf_y = Y1 - y              # flip about the top edge Y1\n    x = pdf_x - X0 ;  y\
    \ = Y1 - pdf_y\n\nIt is only a translation plus a reflection about the horizontal\ncentre line of\
    \ the box — no scaling — so lengths, angles and font\nsizes are identical in both frames. PyMuPDF\
    \ also normalises every\nRect so x0 <= x1 and y0 <= y1, which is why extracted boxes always\ncome\
    \ back \"top-left, bottom-right\" in its own frame.\n\nCropBox offset from MediaBox. PyMuPDF's working\
    \ rectangle,\npage.rect, is the /CropBox intersected with the /MediaBox,\ntranslated so its top-left\
    \ corner sits at (0, 0). Everything that\nreturns or accepts coordinates — get_text(\"words\"), get_text(\"\
    dict\"),\nsearch_for(), insert_text(), insert_image() — uses that shifted\norigin, never the MediaBox\
    \ corner. So X0 and Y1 above must be taken\nfrom the raw /CropBox (clipped by /MediaBox) values read\
    \ off the page\ndictionary, not assumed to be 0 and the MediaBox height. page.rect\ntells you the\
    \ size of the visible area, page.mediabox / page.cropbox\ngive the boxes themselves, and page.cropbox_position\
    \ reports how far\nthe CropBox is offset; check the pair against page.rect rather than\nassuming the\
    \ two coincide, and do any matrix arithmetic\n(e.g. rect * page.rotation_matrix) in this same frame.\n\
    \n/Rotate. Rotation affects only presentation, not the frame used for\ngeometry: get_text(\"words\"\
    ), search_for() and insert_text() all keep\nworking in the UNROTATED, top-left-origin frame; only\
    \ page.rect\n(plus page.rotation_matrix / page.derotation_matrix) reflects the\n/Rotate value. Consequently\
    \ insert_text() at a point taken from\nget_text(\"words\") lands exactly on that word with no derotation\n\
    needed, for any rotation. If instead you start from what the viewer\nsees (a screenshot, a mouse click,\
    \ a rendered-image pixel position),\nconvert first: visible = unrotated * page.rotation_matrix, and\
    \ back\nwith visible * page.derotation_matrix (Rect and Point support the *\noperator). Note also\
    \ that a \"words\" tuple's y0 is the TOP of the\nword's box, not its baseline; for drawing text use\
    \ the span origin\n(the baseline) from get_text(\"dict\"), and remember the y axis now\nruns downward,\
    \ so a larger y is lower on the page.\n\nEdge cases worth remembering: coordinates are floats in points\
    \ only\nwhile /UserUnit is at its default 1, and rotation is always a\nmultiple of 90 degrees, so\
    \ the matrices contain no scale or shear —\nonly the flip, the translation to the effective origin,\
    \ and the\nquarter-turn.\n"
- id: pdf-locate-01
  answer: 'Best built-in route: PyMuPDF''s table finder — page.find_tables() (or

    pymupdf.TableFinder with explicit strategy="lines"/"text") returns TableFinder.tables,

    and each Table object exposes .bbox, .rows[] (each row with .bbox), .cells (rectangles

    per cell) and .header/.bbox entries; you can pull per-cell text with .extract() and get

    row/cell rects directly. If that fails, derive it structurally: page.get_drawings()

    gives every vector path, so cluster the horizontal rules into row separators and the

    vertical rules into column separators, intersect them into a cell grid, then assign text

    spans/words from page.get_text("words") to whichever cell rectangle contains them.

    Alternatives outside PyMuPDF: pdfplumber''s find_tables()/page.lines+rects, camelot or

    tabula. For tagged PDFs the structure tree (mcid → Table/TR/TD) carries true cell bboxes.

    '
- id: pdf-locate-02
  answer: 'search_for returns only the rectangles (or quads) of the matched glyphs themselves —

    nothing about the row. That is insufficient because: the match may be only part of the

    row''s text (other cells are separate strings), the string may be split across spans so

    you get several fragments, the same string may occur elsewhere on the page, and it says

    nothing about the row''s rules or shaded background, which are graphics and would survive

    a text-only removal, leaving an empty banded row. To get the full row region: take the

    matched rect''s baseline y, group all page.get_text("words")/spans whose baseline lies on

    that y (same line) and on adjacent baselines if the row wraps, union them to get the

    text band, then extend the band vertically to the nearest horizontal rules found via

    get_drawings() above and below, and horizontally to the table''s outer left/right rules —

    that union rect is the row region to redact.

    '
- id: pdf-locate-03
  answer: 'page.get_text("dict") (or get_text("rawdict") for per-character bboxes) returns blocks →

    lines → spans, where each span carries bbox, the actual text, "font" (the PDF/base font

    name, e.g. "ABCDEF+Helvetica-Bold" or "helv"), "size", "flags" (bold/italic/serif/

    mono), "origin" (x,y of the baseline start), "ascender"/"descender", and "color"

    (PyMuPDF packs the RGB as an int, e.g. 0xFF0000 for red; alpha is separate). Match a

    colour via pymupdf.sRGB to/from int if needed. For width, use

    pymupdf.get_text_length(text, fontname, fontsize) with the base-14 name, or the span

    bbox width. To reproduce the look: insert_text at span["origin"] with the same

    fontname/size/color — origin is the baseline, which is what matters for vertical

    alignment.

    '
- id: pdf-locate-04
  answer: 'Column boundaries come from the graphics, not the text: page.get_drawings() and pick the

    vertical rule segments (or the cell rectangles returned by find_tables()), taking the

    union of their x positions as column edges. For right-aligned numbers, the answer''s

    anchor is the right edge of the existing glyphs: the span''s bbox x1 (or the word tuple''s

    x1 from page.get_text("words"), index 3) is exactly where the number ends. To place new

    text the same way, compute x = right_edge - pymupdf.get_text_length(new_text, fontname,

    fontsize) and insert at (x, span_origin.y). find_tables() cells give both at once: each

    cell rect''s left/right are the column boundaries.

    '
- id: pdf-rowdel-01
  answer: "1. Read the page: page.get_text(\"dict\") for spans, page.get_drawings() for rules and\n  \
    \ shading; identify the target row band (from rules + baselines) and the region below it\n   (all\
    \ remaining rows, the Total, anything else under the table).\n2. Open a separate untouched copy of\
    \ the file (copy = pymupdf.open(path)) to use as the\n   restamp source — a document cannot be its\
    \ own source.\n3. page.add_redact_annot(row_rect) for the deleted row, and another over the whole\n\
    \   region that must move up. No fill argument. Then page.apply_redactions() with the\n   defaults\
    \ (graphics=REMOVE_IF_COVERED) so the row's rules and shading are removed too.\n4. Move the lower\
    \ region up by the deleted row's height:\n   page.show_pdf_page(old_rect - (0, h, 0, h), copy, pno,\
    \ clip=old_rect), where h is the\n   row height. This preserves appearance exactly; skipping step\
    \ 3 would duplicate the\n   moved lines.\n5. Update the Total: redact just the old number's glyph\
    \ rect (tight, inside the cell, no\n   fill), apply_redactions(graphics=LINE_ART_NONE, images=IMAGE_NONE)\
    \ to keep the cell\n   rules/shading, then insert_text at the old baseline origin with the same font/size,\n\
    \   right-aligned so new_x = old_x1 - get_text_length(new, fontname, fontsize).\n6. Save to a NEW\
    \ file (optionally garbage=4, deflate=True) and re-extract the text to\n   verify: exactly one copy\
    \ of each moved row, correct Total, no leftover duplicates.\n"
- id: pdf-rowdel-02
  answer: 'Defaults: images=PDF_REDACT_IMAGE_PIXELS (2) — image pixels covered by the rectangle are

    blanked; graphics=PDF_REDACT_LINE_ART_REMOVE_IF_COVERED (1) — vector shapes lying

    ENTIRELY inside the rectangle are deleted, shapes only partly covered are left intact;

    text=PDF_REDACT_TEXT_REMOVE (0) — covered text is removed.

    This matters for tables because rules and shaded bands ARE vector graphics: to delete a

    row cleanly its rules/fills must go with it, which the default gives you when your rect

    fully contains them. The flip side is that a rect covering only part of a rule (e.g. a

    horizontal line running past your rect, or a full-width shading band) leaves that graphic

    behind, producing dangling rules or a stray colour band — so size the rect to the whole

    row, or pass graphics=PDF_REDACT_LINE_ART_REMOVE_IF_TOUCHED (2) to remove anything the

    rect merely touches, or graphics=NONE (0) when you intend to keep the grid (cell edits).

    Image-pixel blurring likewise destroys any bitmap background under the row.

    '
- id: pdf-rowdel-03
  answer: "Yes — restamp it instead of retyping. PyMuPDF has no built-in \"move region\" primitive,\n\
    but you can copy an existing page region verbatim:\n1. Open a separate, untouched copy of the PDF\
    \ (pymupdf.open(path)); passing the document\n   you are editing raises \"source document must not\
    \ equal target\".\n2. Redact BOTH the row being deleted and the entire region that is about to move\n\
    \   (otherwise the originals stay in place and the moved block appears twice in the text\n   layer),\
    \ then apply_redactions() with default graphics removal.\n3. page.show_pdf_page(new_rect, copy, pno,\
    \ clip=old_rect) — clip the source copy to the\n   old region and stamp it at the shifted-up destination.\
    \ New appearance is pixel-for-\n   pixel identical to the original, fonts, shading and kerning included.\n\
    (Same technique: Page.swap_content_of_page does not exist, and one must never split or\nhand-edit\
    \ the content stream to achieve this.)\n"
- id: pdf-rowdel-04
  answer: "Dependent values and anything positioned relative to the table:\n- Totals/subtotals (invoice\
    \ Total, tax, quantity sums, balances, \"amount due\").\n- Counts and cross-references (\"3 items\"\
    , \"see row 3\"), summary boxes or a duplicate of\n  the figure elsewhere on the page or in later\
    \ pages.\n- Position of everything below: footer, page numbers, \"continued on page 2\", payment\n\
    \  terms block — if content moved up, check for a now-visible gap or a collision.\n- The table's own\
    \ grid: the removed row's horizontal rules, and alternating row shading\n  (deleting a band can break\
    \ the stripe parity for rows below).\n- Interactive elements: form field/widget rectangles, links,\
    \ annotations, bookmarks\n  anchored to positions — they don't move with the graphics.\n- Any digital\
    \ signature: editing invalidates it; it must be re-signed.\n- Multi-page/repeated headers: a table\
    \ that flowed to page 2 may need the row removed\n  there too, and page-2 continuation totals updated.\n"
- id: pdf-cell-01
  answer: "1. Locate the cell text: page.get_text(\"dict\"), find the span (or word via\n   get_text(\"\
    words\")) equal to \"22.50\". Record span[\"bbox\"], span[\"origin\"] (baseline!),\n   span[\"font\"\
    ], span[\"size\"], span[\"color\"], and the cell's right edge x1 = bbox.x1.\n2. Build a tight rectangle\
    \ that covers those glyphs and stays strictly inside the cell —\n   above the baseline by the ascender,\
    \ below by the descender, not touching neighbouring\n   cells or the cell's rules.\n3. page.add_redact_annot(rect)\
    \ with NO fill argument (never fill=(1,1,1): on a shaded\n   cell it paints a white box over the shading).\n\
    4. page.apply_redactions(images=PDF_REDACT_IMAGE_NONE, graphics=PDF_REDACT_LINE_ART_NONE)\n   so the\
    \ rules and shading survive; text=remove (default) clears the old number.\n5. Right-align the replacement:\
    \ x = old_x1 - pymupdf.get_text_length(\"54.00\", fontname,\n   fontsize); then page.insert_text(pymupdf.Point(x,\
    \ span_origin.y), \"54.00\",\n   fontname=same, fontsize=same, color=same). Use origin, the baseline\
    \ — not bbox bottom.\n   For base-14 fonts (\"helv\", \"hebo\", …) no embedding is needed; for a subset\
    \ font embed\n   the original full TTF or pick a base-14/Noto face that has every glyph.\n6. Save\
    \ to a new file (garbage=4, deflate=True if large), reopen and re-extract to\n   confirm the value,\
    \ position and that nothing else changed.\n"
- id: pdf-cell-02
  answer: 'Size it to cover all of the old glyphs'' bboxes (including ascenders/descenders, plus a

    hair of margin so a partially covered character is caught) while remaining strictly

    inside the cell — clear of the cell''s left/right/top/bottom rules and of the

    neighbouring cells'' text.

    Why: PyMuPDF removes any character whose bbox OVERLAPS the rectangle, whole, even if

    only a sliver overlaps. So the rect needn''t be glyph-exact — a slightly generous one is

    safe — but an oversized one is destructive: it will also delete neighbouring cells''

    characters (they overlap it), and with default graphics handling it will remove rules or

    shading bands lying fully inside it. Conversely a rect clipped by the cell edges is fine

    for text but with graphics=NONE the grid stays anyway, so the real risk is eating

    adjacent text. Tight-around-glyphs, inside-the-cell is the rule; pass no fill so you

    don''t paint white over a shaded cell.

    '
- id: pdf-cell-03
  answer: "insert_textbox lays the text out inside the rectangle with wrapping and returns the\nleftover\
    \ space: a positive float if it fit, a negative float equal to the overflow if it\ndid not — and when\
    \ it does not fit, no text is written (the call is a no-op for the\ncontent), so you silently get\
    \ an empty cell unless you check the return value.\nOptions:\n- Check the return value and branch\
    \ if it's negative (essential — otherwise the old text\n  is gone and nothing replaced it).\n- Reduce\
    \ the font size (recompute until the returned remainder ≥ 0), or use a narrower/\n  condensed font\
    \ face of the same family.\n- Tighten tracking with charwidths/letter spacing, or abbreviate the value\
    \ (\"54.0\" vs\n  \"54.00 USD\").\n- Widen the cell/column: redraw that column's rules and reposition\
    \ or re-render the\n  affected cells — a table-wide relayout rather than a single-cell edit.\n- For\
    \ a fixed one-liner, prefer page.insert_text at a computed x (right-aligned via\n  get_text_length)\
    \ instead of insert_textbox, and shrink the size until the measured\n  width fits the column.\n"
- id: pdf-cell-04
  answer: 'point is the text ORIGIN — the position of the first glyph''s baseline start (the left end

    of the baseline), not the top-left of the rendered box. Text is drawn upward from there

    by roughly the font''s ascent.

    If you pass the top-left corner of the old word''s bbox (bbox.y0), the baseline is placed

    at that y, so the new glyphs sit above it: the text lands visibly HIGHER than the

    original by about one ascent (roughly the cap height + leading), overlapping the row

    above instead of sitting on the cell''s baseline. The correct y is span["origin"].y (from

    get_text("dict")), or bbox.y0 + ascent if you only have the bbox.

    '
- id: pdf-relayout-01
  answer: "Inserting a row means the table grows by one row height, so:\n- The Total row (and any subtotal/notes\
    \ row) must move DOWN by one row height, and its\n  value must be recomputed for the new data.\n-\
    \ The grid must be re-formed: the new row needs a horizontal rule above (and below, or\n  it shares\
    \ the Total's top rule), the vertical column rules must be extended across the\n  new band, and any\
    \ alternating shading must continue the correct stripe parity; the\n  old Total-row rule can't just\
    \ be left where it was.\n- All content below the table must shift down by one row height (or spill\
    \ onto a new\n  page if it would cross the bottom margin): redact the whole region below the insertion\n\
    \  point and restamp it lower with page.show_pdf_page(new_rect, copy, pno, clip=old_rect)\n  from\
    \ an untouched copy — never by rewriting the content stream. Check the footer/\n  page-number band\
    \ so the shifted content doesn't collide with it.\n- Draw order matters: fills first, then text, then\
    \ rules, so a fill never covers text.\n- Then re-extract and verify: one copy of everything, aligned\
    \ rules, correct Total.\n"
- id: pdf-relayout-02
  answer: 'Correct approach: the table must be re-laid-out, not merely appended to. Prefer

    regenerating the PDF from its original source (HTML/CSS, template, reportlab/HTMLTable

    code, spreadsheet, Word file) where a fifth column fits by reflowing the grid. If you

    must edit in place: treat the whole table as one region — compute a new column grid that

    redistributes the fixed total width (shrink the existing columns), redact the entire

    table area, and redraw every rule plus every cell''s text at the new coordinates with the

    right alignment (fills first, then text, then rules), reusing the original fonts/size/

    colour so it matches the rest of the page.

    Common mistake: just drawing a new column header and cells to the right of the existing

    ones (or inserting rules at new x positions) while keeping the old column widths — the

    table then overflows the right margin/page edge, overprints or displaces the last

    column, and leaves stale rules of mismatched length, producing a grid whose rows don''t

    line up with its header.

    '
- id: pdf-relayout-03
  answer: 'It is acceptable only when exact fidelity does not matter and no better

    option exists: the original editable source (template, HTML, reportlab

    code, spreadsheet, Word file) is unavailable, the document is simple

    (plain text and simple tables, no complex vector art, no forms, no

    annotations, no exact pagination requirements), and you can visually

    check the result. It is a documented last resort.


    Risks: fonts may be substituted (metrics change, text reflows and page

    breaks move); layout and table geometry drift; vector graphics,

    gradients, rules and shading may be redrawn or lost; images can be

    resampled or recompressed; page size, margins, bleed and crop boxes may

    change; PDF-only features are dropped — form fields, AcroForms,

    annotations, bookmarks, tagged structure, optional content, encryption,

    hyperlinks, embedded files, metadata; hyphenation/kerning/ligatures and

    column structure change; colors can shift to a different color space;

    page count can change. The round trip (PDF -> DOCX/HTML -> PDF) often

    compounds these losses. A surgical in-place edit with PyMuPDF, or

    regenerating from the true source, is preferable.

    '
- id: pdf-relayout-04
  answer: "Measure, don't eyeball:\n\n1. Extract the text blocks/spans with page.get_text(\"dict\") or\n\
    \   get_text(\"blocks\") and sort them by vertical position (mind that on a\n   /Rotate page extraction\
    \ is in the UNROTATED frame — convert with\n   page.rotation_matrix / derotation_matrix if you need\
    \ visible\n   coordinates).\n2. Identify the gap you want to use: the y-extent between the bottom\
    \ of\n   the block above and the top of the block below (use bbox[3] of the\n   above and bbox[1]\
    \ of the below), restricted to the relevant column's\n   x-range if the page is multi-column.\n3.\
    \ Compare against what you need: required height = header height + sum\n   of row heights + rules/padding;\
    \ required width = sum of column\n   widths. Compute from font size x line count x line height.\n\
    4. Check the content boundary: page.rect (or mediabox/cropbox), minus\n   margins, minus any header/footer,\
    \ watermark, or standing elements.\n5. Account for decorations text extraction misses: vector rules\
    \ and\n   shading (scan drawings via page.get_drawings() within the gap) and\n   background images/logos.\n\
    6. If the gap is too small, make room: redact the region below the\n   insertion point and restamp\
    \ it lower with show_pdf_page(clip=...)\n   from an untouched copy, spilling onto a new page if it\
    \ would run past\n   the bottom.\n\nFinally render the page (page.get_pixmap()) and look at it — extraction\n\
    alone can miss overlapping non-text elements.\n"
- id: pdf-tblins-01
  answer: "Way 1 — draw it manually with PyMuPDF primitives (no insert_table\nexists; Page.insert_table\
    \ is not a real API):\n  - page.draw_rect(..., fill=...) for cell fills FIRST,\n  - page.insert_text((x,\
    \ baseline), text, fontname=\"helv\"/\"hebo\",\n    fontsize=...) for the text (baseline = row_top\
    \ + fontsize * ~0.8,\n    keeping x from get_text_length for alignment),\n  - page.draw_line(...)\
    \ / draw_rect(color=...) for the rules LAST,\n    after fills and text.\nCompute a grid from a starting\
    \ point: header height = 1.6 * fontsize,\ndata rows = 1.3 * fontsize; column widths from\npymupdf.get_text_length(header,\
    \ fontname, fontsize) plus padding.\nSave to a NEW file, re-open and re-extract to verify.\n\nWay\
    \ 2 — build the table in a separate document and stamp it in:\n  - Create a one-page PDF (pymupdf.open()\
    \ + new_page(width, height))\n    containing only the table, drawn with fitz Story/HTML+CSS or with\n\
    \    reportlab, then page.show_pdf_page(target_rect, tmp_doc, 0,\n    clip=...) to place it, or merge\
    \ it with pypdf/pdf-merger.\n  - Alternatively use fitz.Story (HTML + CSS) with a rect on the target\n\
    \    page to lay out the table with real table semantics, which handles\n    wrapping and column widths\
    \ for you.\n"
- id: pdf-tblins-02
  answer: "- Reuse the document's existing fonts: extract the fonts actually used\n  (page.get_fonts(full=True))\
    \ and insert the SAME embedded font file\n  (page.insert_font(fontname=..., fontfile=...)) so metrics\
    \ and look\n  match. Beware subset fonts (AAAAAA+...): the subset lacks glyphs, so\n  fall back to\
    \ the original full TTF/OTF or the closest base-14/Noto\n  font, and check has_glyph for every character\
    \ you insert.\n- Match sizes, weights, and colors: sample the neighbouring text's\n  size, font (regular\
    \ vs bold) and fill color via get_text(\"dict\")\n  spans, and sample rule color/thickness and cell\
    \ shading from\n  page.get_drawings().\n- Match margins/column grid: align the table's left/right\
    \ edges to the\n  text column edges of surrounding paragraphs; reuse existing row\n  height, cell\
    \ padding and header style conventions.\n- Correct paint order: fills first, then text, then rules\
    \ — drawing a\n  fill after text hides the text on screen while leaving it in the text\n  layer.\n\
    - No white fill boxes over existing shading: when clearing space, use\n  add_redact_annot without\
    \ fill (never fill=(1,1,1) on a shaded cell),\n  and apply with graphics=PDF_REDACT_LINE_ART_NONE,\n\
    \  images=PDF_REDACT_IMAGE_NONE to keep rules and shading.\n- Keep alignment consistent: right-align\
    \ numbers using\n  x = right_edge - get_text_length(text, font, size); place text at\n  baselines,\
    \ not bbox bottoms (bbox[3] includes descender).\n- Render the page to a pixmap and visually diff\
    \ before/after; check the\n  text layer too.\n"
- id: pdf-tblins-03
  answer: "Options, in preferred order:\n1. Regenerate: if the original source exists (template, HTML,\n\
    \   reportlab, Word, spreadsheet), edit the source and rebuild the PDF —\n   the cleanest way to insert\
    \ between paragraphs.\n2. Make room by restamping: redact everything from the insertion point\n  \
    \ downward (add_redact_annot over the region, apply_redactions with\n   default graphics removal so\
    \ rules/shading below go too), then\n   re-stamp the displaced content lower using\n   page.show_pdf_page(new_rect,\
    \ copy, pno, clip=old_rect) from a\n   SEPARATE, untouched copy of the document (passing the document\
    \ you\n   are editing raises \"source document must not equal target\"). Insert\n   the new table\
    \ in the created gap. Critically, redact the region that\n   moves before restamping, otherwise the\
    \ moved lines are duplicated in\n   the text layer.\n3. Spill to a new page: if shifting content would\
    \ run past the bottom\n   margin, move the trailing paragraph(s) to a new page (create a page,\n \
    \  restamp them there).\n4. Compress existing content: shrink surrounding font sizes or reduce\n \
    \  leading to open a gap — only if typography changes are acceptable.\n5. Last resort: PDF -> DOCX/HTML\
    \ -> edit -> PDF round trip, only when\n   exact fidelity does not matter.\n\nNever try to make room\
    \ by splitting or rewriting the content stream.\n"
- id: pdf-imgrep-01
  answer: "Approach A (single placement, correct aspect):\n  rect = page.search_for(\"old logo text or\
    \ known rect\") or reuse the\n  old placement's rect from page.get_image_info(xrefs=True) /\n  page.get_images(full=True)\
    \ + image placement lookup.\n  add_redact_annot(rect); apply_redactions(images=PDF_REDACT_IMAGE_REMOVE)\n\
    \  (removes the image entirely) — then\n  page.insert_image(rect, filename=\"new.png\", keep_proportion=True),\n\
    \  which centres the new image inside the rect while preserving aspect.\n  Save to a new file, re-open\
    \ and re-extract.\n\nApproach B (bulk swap): page.replace_image(old_xref, filename=\"new.png\").\n\
    \nThe catch with shared images: replace_image swaps the IMAGE XREF, so\nevery placement of that xref\
    \ — on every page — changes to the new logo.\nIt also keeps the old placement box, so an image with\
    \ a different\naspect ratio is stretched/distorted to fit. If only one placement should\nchange, or\
    \ the aspect differs, redact that rect and insert_image into\nit instead. Also note a new image may\
    \ be RGBA/PNG with a different\ncolorspace/SMask; check transparency renders as expected.\n"
- id: pdf-imgrep-02
  answer: "The old logo is still in the file underneath. insert_image draws the new\nimage on top but\
    \ does not remove the old one — so:\n  - The original image object (xref), its pixels and metadata,\
    \ remain\n    extractable from the PDF (confidential or stale branding is still\n    recoverable —\
    \ a real problem if the swap was for legal/privacy\n    reasons).\n  - The file grows (both images\
    \ stored), and if the new image has any\n    transparency or is later moved/deleted, the old one shows\
    \ through.\n  - The text/image layer and get_images() still list both; extraction\n    tools will\
    \ return two images at overlapping rects.\n  - Order-dependent: if drawing order changes (e.g. a later\
    \ content\n    edit, or a viewer honoring z-order differently) the old logo can\n    reappear.\n\n\
    Fix: redact the old rect first with\nadd_redact_annot(rect); apply_redactions(images=PDF_REDACT_IMAGE_REMOVE)\n\
    (which truly deletes the pixels), then insert the new image.\n"
- id: pdf-imgrep-03
  answer: "Likely causes:\n  - The logo is not an XObject image on that page: it may be a vector\n   \
    \ drawing (paths/shapes from Illustrator etc.), so get_images() on the\n    page shows nothing — check\
    \ page.get_drawings().\n  - get_images() with default args doesn't decode/expand; some images\n  \
    \  live inside Form XObjects, so page.get_images() at the page level\n    misses them — use page.get_images(full=True,\
    \ xref=True),\n    get_image_info(xrefs=True), or doc.get_page_images(pno, full=True).\n  - The logo\
    \ is on a different page (or annotation), or is a stamp/\n    annotation appearance (widget/stamp\
    \ annots), not page content.\n  - It's an inline image (BI...ID...EI in the content stream) — no xref\n\
    \    in the XObject dict; get_images may not list it; handle by redacting\n    the rect (apply_redactions\
    \ with images=PDF_REDACT_IMAGE_REMOVE\n    blanks/removes overlapping image pixels) rather than by\
    \ xref.\n  - It's a tiled/shaded pattern fill or a soft-masked image.\n\nHow to find and replace:\
    \ render the page (page.get_pixmap()), locate the\nlogo's visible rect — page.get_image_info(xrefs=True)\
    \ gives bbox per\nplacement; search_for() only finds text, so for vector art use\nget_drawings() and\
    \ match the path whose bbox contains the logo, or\nclick-detect coordinates. Then either, for a real\
    \ image xref:\npage.replace_image(xref, filename=...) — but note this changes EVERY\nplacement of\
    \ that xref; or, for placement-specific / vector / inline\ncontent: add_redact_annot(rect), apply_redactions(images=2\
    \ default,\ngraphics default REMOVE_IF_COVERED), then\npage.insert_image(rect, filename=..., keep_proportion=True).\n"
- id: pdf-imgins-01
  answer: "1. Get the image's natural pixel size to preserve aspect:\n     with pymupdf.open(\"sig.png\"\
    ) as im: w0, h0 = im[0].rect.width, im[0].rect.height\n   (or pymupdf.Pixmap(\"sig.png\") -> pix.width\
    \ / pix.height; PIL also\n   works). Aspect = w0/h0.\n2. width = 150; height = 150 * h0 / w0.\n3.\
    \ Place bottom-right inside the printable area:\n     page = doc[0]\n     margin, bottom_gap = 50,\
    \ 50\n     x1 = page.rect.width - margin            # mind rotation: use the\n     y1 = page.rect.height\
    \ - bottom_gap       # unrotated frame + matrices\n     rect = pymupdf.Rect(x1 - 150, y1 - height,\
    \ x1, y1)\n4. Check it doesn't cover text: extract with page.get_text(\"words\") or\n   get_text(\"\
    blocks\") and verify no block bbox intersects rect; if it\n   does, raise the rect (move up) or shrink.\
    \ Also check drawings/headers\n   if the footer has rules.\n5. Insert: page.insert_image(rect, filename=\"\
    sig.png\",\n   keep_proportion=True)  # keep_proportion centres and never stretches;\n   # since we\
    \ derived the rect from the real aspect it fits exactly.\n   Use overlay=True (default) to draw on\
    \ top, or overlay=False to put it\n   behind content.\n6. Save to a NEW file and re-open to verify.\n"
- id: pdf-imgins-02
  answer: "Store the image xref once and reuse it: on the first page\n    xref = page.insert_image(rect,\
    \ filename=\"logo.png\")\nthen on every other page\n    page.insert_image(rect, xref=xref)\nso all\
    \ 200 placements reference the single shared image object instead\nof embedding 200 copies.\n\nExtra\
    \ steps:\n  - Compute the rect once (per-page rect if margins/page sizes differ)\n    and pass the\
    \ same xref; PyMuPDF maps the xref into each page's\n    resources.\n  - For an existing bloated file,\
    \ re-save with\n    doc.save(out, garbage=4, deflate=True) — garbage=4 garbage-collects\n    and MERGES\
    \ identical image streams, deflate compresses them, which\n    deduplicates copies already in the\
    \ file.\n  - Avoid calling insert_image with filename= in the loop: each call\n    with a filename\
    \ creates (or may create) a new image object.\n"
- id: pdf-imgins-03
  answer: "Likely causes:\n  - Draw order: the image was drawn before (under) an opaque filled\n    rectangle/background\
    \ box — PDF paints in content-stream order, so a\n    later fill covers it. Fix: insert it again AFTER\
    \ the background\n    (overlay=True, which is the default), or accept it behind with\n    overlay=False.\n\
    \  - Rect degenerate or zero-area (wrong coordinates, width/height 0, or\n    points vs other units\
    \ confusion) — insert silently succeeds but\n    nothing is visible.\n  - Wrong page index or wrong\
    \ frame: on a /Rotate page insert_* uses the\n    UNROTATED frame while only page.rect is rotated;\
    \ visible coords must\n    be converted with page.derotation_matrix, otherwise the image lands\n \
    \   off-page.\n  - Rect outside the visible page/crop box or beyond the mediabox, or\n    clipped\
    \ by a clipping path (W/n) in the content stream.\n  - The image is fully transparent (empty alpha/SMask,\
    \ or a white logo\n    on white background), or inserted with colorspace/alpha that renders\n    blank.\n\
    \  - It was inserted on a copy/other document and saved to a different\n    file, or inserted after\
    \ doc was closed / not saved (no save, or\n    saved incrementally to a file you're not viewing).\n\
    \  - Transparency group / blend mode interaction, or the image is a JPEG\n    with an unexpected colorspace\
    \ appearing wrong in some viewers.\n  - Failure was swallowed: insert_image returns an xref; check\
    \ it is\n    truthy (a non-zero xref), and confirm via get_image_info.\n"
- id: pdf-fonts-01
  answer: "Because the embedded font is a SUBSET: font names like \"AAAAAA+Georgia\"\nmean only the glyphs\
    \ used somewhere in the document were embedded. When\nyou extract that font (doc.extract_font(xref))\
    \ and re-embed the buffer,\nit still contains only those subset glyphs — any character not already\n\
    in the subset has no glyph, so:\n  - new characters render as .notdef (blank, box, or garbage), and\n\
    \  - re-extracting the saved PDF returns garbage such as \\x00 for them,\n    which looks like the\
    \ edit silently failed.\n\nWhat to do:\n  1. Preferred: find and embed the ORIGINAL full TTF/OTF font\
    \ file\n     (or the font shipped with the document/source) with\n     page.insert_font(fontname=\"\
    ...\", fontfile=\"FullFont.ttf\").\n  2. Otherwise use a base-14 font (helv, hebo, heit, hebi, cour,\
    \ ...) or\n     a Noto font bundled with PyMuPDF — the closest match in style and\n     metrics.\n\
    \  3. Check coverage before/after inserting:\n     pymupdf.Font(fontfile=\"FullFont.ttf\").has_glyph(ord(c))\
    \ must be\n     non-zero for every character in the new text.\n  4. Take the baseline from the original\
    \ span's origin (get_text\n     (\"dict\")), not bbox[3].\n  5. Save to a NEW file, re-open it, and\
    \ re-extract the text to confirm\n     the new characters come back correctly (no \\x00).\n\nThere\
    \ is no safe way to \"extend\" a subset from inside PyMuPDF —\nsubsetting is done at embed time by\
    \ the producer, not by the library.\n"
- id: pdf-fonts-02
  answer: "In PyMuPDF the base-14 aliases are:\n  - Helvetica regular:  \"helv\"  (alias for Helvetica;\
    \ also \"Helvetica\",\n    \"Helvetica-Narrow\" is \"heit\"? no — Helvetica-Narrow is not base-14;\n\
    \    the standard 14 use short names)\n  - Helvetica bold:     \"hebo\"  (Helvetica-Bold)\n\nThe other\
    \ base-14 short aliases for completeness: cour / cobo / coit /\ncobi (Courier family), tiro / tibo\
    \ / tiit / tibi (Times family), and\nsymb / zadb (Symbol, ZapfDingbats).\n\nLimitation: base-14 fonts\
    \ are Standard 14 fonts — they are NOT embedded\nin the PDF (get_page_fonts shows ext \"n/a\", source\
    \ \"builtin\") and\ncontain only their standard Latin/Western-Europe character sets (Latin-1\nplus\
    \ a few extras per the Adobe standard encoding). They have limited or\nno coverage for non-Latin scripts\
    \ — Cyrillic, Greek accents beyond\nbasic, CJK (Chinese, Japanese, Korean), Arabic, Hebrew, Devanagari,\
    \ etc.\n— and many special/typographic characters (curly quotes, em dashes,\narrows, emoji, many symbols)\
    \ are missing or render as the wrong glyph,\ndepending on the viewer's substitute font. For such text\
    \ you must embed\na full Unicode font (e.g. a Noto family font) via insert_font(fontfile=).\nThey\
    \ also can't be subset/embedded, so glyph rendering relies on the\nreader having the metrics right.\n"
- id: pdf-fonts-03
  answer: "Detect:\n  - Extract the original cell's span(s) with page.get_text(\"dict\")\n    (or get_text(\"\
    rawdict\")) and read span[\"font\"], span[\"size\"],\n    span[\"flags\"] (bit 4 = 16 set means bold;\
    \ also check bit 2 = 4 for\n    serif) and span[\"color\"].\n  - Distinguish name from weight: the\
    \ font NAME tells you the real face\n    — e.g. \"AAAAAA+Georgia-Bold\", \"...-BoldMT\", \"...IBMPlexSans-SemiBold\"\
    \n    — while flags only hint. If your inserted text used \"helv\" (or\n    another base-14/regular\
    \ alias) while the original was a bold\n    embedded face, you get regular weight and a different\
    \ metrics/size.\n  - Compare sizes: your insert_text fontsize may simply differ from the\n    extracted\
    \ span size (or the original has a span size like 9.7 not\n    10).\n  - Verify by re-extracting the\
    \ saved file and diffing the new span's\n    font/size/flags against the old ones; render a pixmap\
    \ crop for a\n    visual check.\n\nMatch:\n  - font: insert_font the original font's FILE (full, not\
    \ the subset —\n    see the subset caveat) under its real name and insert_text with that\n    fontname;\
    \ or use the correct base-14 bold alias \"hebo\" for Helvetica\n    Bold (and \"tibo\" for Times-Bold,\
    \ \"cobo\" for Courier-Bold); if the\n    original is a Noto/PDF bundle font, use the matching Noto\
    \ bold.\n  - size: use exactly the extracted span size (span[\"size\"], rounding\n    only if the\
    \ document clearly rounds).\n  - color: pass the extracted span color to insert_text\n    (color=span[\"\
    color\"] as an int 0xRRGGBB, or a (r,g,b) tuple with\n    0..1 floats via a Shape).\n  - position:\
    \ baseline at span[\"origin\"] (not bbox[3]); for right or\n    centre alignment compute with\n  \
    \  pymupdf.get_text_length(new_text, fontname, fontsize).\n  - Redact the old glyphs first (tight\
    \ rect, no fill, apply with\n    graphics=PDF_REDACT_LINE_ART_NONE, images=PDF_REDACT_IMAGE_NONE to\n\
    \    keep rules/shading) so the old and new text don't overlap.\n"
- id: pdf-verify-01
  answer: "Don't trust the script's exit status — verify the OUTPUT FILE:\n  1. Save to a new file, re-open\
    \ it with pymupdf, and re-extract text\n     from the saved document (not the in-memory one):\n  \
    \   doc2 = pymupdf.open(out); doc2[pno].get_text() — assert the new\n     string is present and the\
    \ old string/row is gone (search with\n     search_for and get_text).\n  2. Check structural details:\
    \ the affected spans' font, size, weight\n     and bbox match the intended style; the row count/table\
    \ numbers\n     (e.g. recomputed Total) are correct; check every page if a global\n     change was\
    \ made (get_images(xrefs) counts, fonts list).\n  3. Visual check: render the page (page.get_pixmap(dpi=150))\
    \ to a PNG\n     and look at it — catches overlaps, missing fills, white boxes over\n     shading,\
    \ wrong paint order, text hidden behind a rect, and\n     duplicated content that a text check can\
    \ miss.\n  4. Check for duplicate/leftover content: for restamped regions make\n     sure the moved\
    \ lines appear exactly ONCE (double text = forgot to\n     redact before show_pdf_page); verify with\
    \ word positions.\n  5. For edits: compare before/after — extract both and diff; optionally\n    \
    \ compare word bboxes to confirm nothing shifted unexpectedly.\n  6. Open the file in a real viewer\
    \ (or run doc.scrub/repair checks) and\n     confirm it opens without repair prompts; verify page\
    \ count, page\n     size, rotation and links/annotations are unchanged elsewhere.\n  7. For deleted/confidential\
    \ content: assert the removed text cannot be\n     found anywhere in the saved file (e.g. search raw\
    \ bytes/decompressed\n     streams), since a normal (non-redacted) delete can leave text in\n    \
    \ the content stream.\n"
- id: pdf-verify-02
  answer: 'Almost certainly no — it is a trap. Redaction only takes effect when the

    annotation is APPLIED: page.apply_redactions() must be called after

    add_redact_annot. Adding the annot and saving without

    apply_redactions() leaves the original text in the content stream —

    still selectable, copyable and extractable; the redaction annotation is

    just an annotation on top.


    Even with apply_redactions() done, saving with incremental=True appends

    to the original file: the updated content stream is written as a new

    revision at the end of the file, but the OLD objects — including the

    original content stream with the confidential row''s text — remain in

    the earlier part of the file, and anyone can recover them (e.g. by

    reading the previous revision, or with PDF object/version tools). The

    image pixels also remain if they weren''t redacted (images= default

    blanks overlapping pixels, but only after apply_redactions).


    Correct procedure: add_redact_annot(rect) with no fill, then

    page.apply_redactions(images=PDF_REDACT_IMAGE_PIXELS, graphics=...,

    text=PDF_REDACT_TEXT_REMOVE) (the defaults), then save to a NEW,

    non-incremental file (doc.save(out) — PyMuPDF even refuses some

    otherwise), re-open it, and verify the string is absent from every page

    and from raw bytes/decompressed streams. For strong confidentiality also

    consider doc.scrub() to strip metadata/attachments and save with

    garbage=4.

    '
- id: pdf-verify-03
  answer: "- Never edit in place: copy the file first (or open read-only and save\n  to a new output path),\
    \ keeping the original untouched as a backup.\n- Work on the copy; save to a new file rather than\
    \ overwriting; only\n  replace the original after verification, and atomically (write\n  temp then\
    \ rename).\n- Keep a versioned backup (invoice-YYYYMMDD-original.pdf) and be able\n  to reproduce\
    \ the edit from the script — keep the script and its\n  inputs/params so the change is auditable and\
    \ repeatable.\n- Preserve the original metadata/permissions/encryption; if the PDF is\n  password-protected,\
    \ handle credentials carefully.\n- Verify after saving (re-extract text, render and visually check,\n\
    \  confirm page count/size/rotation unchanged, confirm other pages\n  untouched) before delivering.\n\
    - Be aware of confidential content: don't leave intermediate files with\n  partial edits; when deleting\
    \ content use proper redaction (apply\n  redactions, non-incremental save to a new file) so the data\
    \ is really\n  gone.\n- Check file integrity (the file opens without repair warnings), and\n  note\
    \ any assumptions about what the client wanted changed; if anything\n  is ambiguous (e.g. which invoice\
    \ number/row), ask before editing.\n- Prefer regenerating from the source document if one exists,\
    \ and avoid\n  lossy PDF->DOCX->PDF round trips.\n"
- id: pdf-verify-04
  answer: "Because \"the script ran\" only proves no exception was raised — PDF\nlibraries perform most\
    \ operations silently and don't validate meaning.\nSpecific reasons:\n  - Edits may have been applied\
    \ to the wrong page, position, or even the\n    in-memory doc that was never saved (or saved to a\
    \ different path /\n    opened file was a different copy).\n  - Operations that look successful can\
    \ be no-ops: inserting text with a\n    subset font produces \\x00/garbage instead of visible glyphs;\n\
    \    inserting at a degenerate/off-page rect puts content nowhere visible;\n    insert_image xref\
    \ errors can be swallowed; an insert under an opaque\n    fill is invisible; text drawn then covered\
    \ by a later fill.\n  - Layout can be wrong even when the content stream is right:\n    overlapping\
    \ text, missing fills, wrong paint order, white boxes over\n    shading, clipped content, wrong font\
    \ weight/size/alignment, shifted\n    rules.\n  - For deletions: without apply_redactions() the text\
    \ is still in the\n    file; with incremental save the old revision still contains it.\n  - For restamping:\
    \ forgetting to redact before show_pdf_page duplicates\n    every moved line.\n  - Print \"done\"\
    \ is just a message at the end of a try/except-free\n    path; it says nothing about whether the assertions\
    \ one should write\n    (new text present, old text absent, counts correct) hold.\n\nEvidence of correctness\
    \ must come from the OUTPUT: re-open the saved\nfile, re-extract text and assert expectations, and\
    \ render pages to\nimages for visual inspection.\n"
