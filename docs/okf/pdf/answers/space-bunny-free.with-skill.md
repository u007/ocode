- id: pdf-model-01
  answer: "A PDF has no concept of a table. There is no table object, no rows, no cells,\nno \"third row\"\
    \ and no structure tree. The table is only an emergent pattern of\ncontent-stream operators: text-showing\
    \ operators (Tj/TJ/Td/Tm) that paint glyphs\nat absolute coordinates, plus path operators that stroke/fill\
    \ the ruling lines\nand cell shading, plus optional images. That has direct consequences:\n\n- You\
    \ cannot \"delete row 3\" as a semantic edit. Deleting a row means erasing\n  that band of content\
    \ AND moving every visual element below it upward\n  (following content, the Total line, notes, footer),\
    \ then closing the gap with\n  the right rules and recomputing the Total.\n- There is no reflow. Unlike\
    \ a word processor, nothing will re-flow around the\n  gap; you must compute new coordinates yourself.\n\
    - Content streams are usually Flate-compressed and the xref/length metadata must\n  stay consistent,\
    \ so byte-patching the stream is unsafe.\n- \"Text\" is not a text object either. Editing a cell value\
    \ means removing the\n  glyphs of the old value and stamping new glyphs at a new position in matching\n\
    \  font, size, colour and alignment.\n\nSo: treat the table as geometry (row bands, cell rectangles,\
    \ rule lines) and do\na redact + restamp/draw edit with a PDF library such as PyMuPDF, or regenerate\n\
    the document from its source template.\n"
- id: pdf-model-02
  answer: "Drawing a filled rectangle over the text is not an edit; it is a cosmetic overlay.\nThe original\
    \ text objects remain in the content stream underneath. They are still:\n\n- extractable with get_text\
    \ / pdftotext,\n- findable with search_for / find_tables,\n- selectable, copyable and readable by\
    \ screen readers and by anyone re-rendering\n  the page with the rectangle removed or ignored,\n-\
    \ and the rectangle also obscures the ruling lines and cell shading beneath it,\n  which looks wrong\
    \ and breaks later table detection.\n\nIt also has no notion of which characters it hides, so it cannot\
    \ be verified —\nyou cannot confirm the old text is gone, only that something white is on top.\n\n\
    Use a real redaction instead: add redact annotations over the region and call\npage.apply_redactions()\
    \ (PyMuPDF) or the equivalent in your PDF library, which\nactually removes overlapping text characters,\
    \ and removes or blanks images and\nline art according to the images/graphics flags. Then insert the\
    \ replacement text\nwith insert_text / insert_textbox. Always verify afterwards by re-extracting the\n\
    text from that rect and asserting the old string is absent and the new one present.\n"
- id: pdf-model-03
  answer: "Several independent reasons, all of which bite in practice:\n\n- Compression. Content streams\
    \ are normally Flate-compressed, so the raw bytes\n  are binary; the ASCII you are searching for simply\
    \ is not there. You must decode\n  first (page.read_contents() / doc.xref_stream(xref)) — and then\
    \ your byte offsets\n  refer to the decoded buffer, not the file.\n- Text is not stored as the visible\
    \ string. A show-text operand may be a hex\n  string of glyph indices, or the encoding may be a subset/custom\
    \ encoding where\n  the stream bytes map to glyphs only through the font's CMap. What you see is\n\
    \  reconstructed via ToUnicode. So \"22.50\" may literally not appear in the stream.\n- Split runs.\
    \ Text is often split across multiple show-text operators and multiple\n  runs (kerning, subsetting,\
    \ per-span formatting, copy-protection). The digits may\n  live in two or three separate operators,\
    \ so a contiguous search fails — and a\n  naive text.replace can silently do nothing, leaving a file\
    \ that is byte-identical\n  to the input.\n- Length changes break offsets. Even if you find the literal,\
    \ \"54.00\" is the same\n  length as \"22.50\" but not in general; changing the length desynchronises\
    \ the\n  stream unless you also fix the /Length and re-compress. A mismatch usually\n  corrupts the\
    \ page (or the whole object graph) rather than editing it.\n- Editing at the stream level moves text\
    \ only. Ruling lines, fills and images stay\n  where they were, and the replacement is not re-flowed,\
    \ so alignment is lost.\n- pikepdf operates on objects/xrefs. Blind byte replacement of compressed\
    \ data\n  breaks the zlib stream and invalidates the object.\n\nCorrect approach: decode/analyse to\
    \ locate the glyph positions (or use\nget_text(\"rawdict\")/search_for to get bboxes), redact the\
    \ old characters, and\nstamp the new value with the matching font, size, colour and alignment.\n"
- id: pdf-model-04
  answer: 'PyMuPDF normalises to a top-left origin with y increasing downward, and it uses

    that same system for extraction and for insert/draw. So a bbox from

    page.get_text("words") can be passed straight back to insert_text, draw_rect,

    add_redact_annot, insert_image, etc. — no conversion needed for the common case.


    Raw PDF content-stream coordinates are different: they are PDF user space, origin

    at the bottom-left of the MediaBox, y increasing upward, and they are not shifted

    by the CropBox. To convert a PyMuPDF point to raw user space multiply by

    ~page.transformation_matrix; to go back multiply by page.transformation_matrix.


    /Rotate pages: page.rect reports the rotated (displayed) size — width and height

    swap — but get_text bboxes are NOT in displayed coordinates. They remain in the

    unrotated page''s system, which is the very system insert_text/draw_rect use, so

    you still pass them straight through. If you want displayed coordinates, use

    rect * page.rotation_matrix, and to come back rect * page.derotation_matrix.


    CropBox offset from the MediaBox: page.rect starts at (0, 0) at the CropBox''s

    top-left, so PyMuPDF coordinates are already relative to the CropBox. The offset

    only matters when converting to raw user space, and there the transformation

    matrix (which encodes the page''s /MediaBox, /CropBox and rotation) handles it.

    page.rect is the authoritative visible area — never draw outside it.

    '
- id: pdf-locate-01
  answer: "Use the table finder. In PyMuPDF, page.find_tables() returns a TableFinder whose\n.tables is\
    \ a list of Table objects. Each Table gives you:\n\n- table.bbox — the whole table rectangle,\n- table.row_count\
    \ / table.col_count,\n- table.rows — a list of Row objects, each with .bbox (the full-width band for\n\
    \  that row) and .cells,\n- table.cells — the list of cell bounding boxes in row-major order (in some\n\
    \  versions a dict keyed by (row, col); check the type), so cell (r, c) is\n  obtainable directly,\n\
    - table.extract() — the text of every cell as a list of row lists,\n- table.to_markdown() in recent\
    \ versions for a quick sanity read.\n\nBy default find_tables() uses the ruling lines (\"lines\");\
    \ you can pass\nstrategy=\"lines\", \"lines_strict\" or \"text\" (and vertical_strategy /\nhorizontal_strategy,\
    \ e.g. \"text\" for a borderless table), plus clip= to restrict\nthe search area. Options such as\
    \ snap_tolerance, join_tolerance, intersection_tolerance\nand edges=Shifted lines(...) control how\
    \ lines are grouped into cells.\n\nSo the row bands come from table.rows[i].bbox and the individual\
    \ cells from\ntable.cells / row.cells. For a ruled table, the alternative authority is\npage.get_drawings(),\
    \ which returns the stroked/filled vector objects — the actual\nhorizontal and vertical lines whose\
    \ y and x coordinates give you the row and column\nboundaries exactly, and which is what you later\
    \ have to redraw.\n"
- id: pdf-locate-02
  answer: "page.search_for(\"Widget C\") finds only the glyph rectangles of the characters that\nmatched,\
    \ on one page, on one line. It knows nothing about the table. Concretely:\n\n- A row typically has\
    \ several cells in separate columns; the search returns one\n  rect/quad per matched fragment, so\
    \ \"Widget C\" alone may give you only the label\n  cell and none of the quantity/price/amount cells\
    \ in the same row.\n- The match rect covers the characters' advance box, not the row band. It says\n\
    \  nothing about the row's vertical extent, the ruling lines, the cell boundaries\n  or the shading\
    \ — so redacting it would leave the row's other cells and its\n  horizontal rules behind.\n- Phrases\
    \ split across runs, ligatures, or differing whitespace can make the\n  search fail or match the wrong\
    \ occurrence when the label appears twice.\n\nTo get the full row region: get the table structure\
    \ first (page.find_tables()),\nthen take the row's bbox — table.rows[i].bbox, which already spans\
    \ the table's\nfull width — and cross-check it against the horizontal rules from\npage.get_drawings()\
    \ (the y values bracketing the row). Then pad the rect slightly\nto swallow the glyph ascenders/descenders\
    \ and the rule stroke width, and use that\nas the redaction region. If the row is inside a specific\
    \ column only, use that\ncell's bbox from table.cells instead. search_for is still useful to confirm\
    \ which\nrow/cell you landed in, and to locate the exact old value inside a cell.\n"
- id: pdf-locate-03
  answer: "Read the text as a dict and inspect the spans. page.get_text(\"dict\") returns\nblocks -> lines\
    \ -> spans, and each span is a dict containing:\n\n- \"font\" — the font name, e.g. \"Helvetica-Bold\"\
    \ or \"AAAAAA+TimesNewRomanPSMT\"\n  (the AAAAAA+ prefix means an embedded subset; that font only\
    \ has the glyphs the\n  document actually used, so check has_glyph() on your replacement font before\n\
    \  inserting new characters),\n- \"size\" — the font size,\n- \"color\" — an integer sRGB value (0xRRGGBB);\
    \ convert with\n  (r, g, b) = ((c >> 16) & 255, (c >> 8) & 255, c & 255) / 255.0,\n- \"origin\" —\
    \ the baseline start point (this is what you feed to insert_text),\n- \"bbox\" — the character box,\n\
    - \"flags\" — a bitfield hint: 1 superscript, 2 italic, 4 serif, 8 monospaced,\n  16 bold. Treat the\
    \ bits as a hint, not the authority; the font name and\n  rendering are the truth,\n- \"ascender\"\
    \ / \"descender\" if you need to reason about vertical placement.\n\npage.get_text(\"rawdict\") additionally\
    \ gives per-character bboxes and origin\npoints, which is what you want when a cell is split across\
    \ runs. If the content\ncame from a graphic layer or you need glyph-level detail,\npage.get_texttrace()\
    \ returns spans with origins plus the font used. Fill and\nstroke colours of the cell background/rules\
    \ come from page.get_drawings()\n(\"fill\"/\"color\"/\"width\" on each path).\n"
- id: pdf-locate-04
  answer: "Two sources, and for a ruled table the vector lines are the authority:\n\n- page.get_drawings()\
    \ returns the page's path objects. Vertical stroked lines\n  give you the column separators (their\
    \ rect.x0 / x1), horizontal stroked lines\n  give you the row boundaries (rect.y0 / y1), and the fill\
    \ objects give you the\n  cell shading. This is the geometry the table is actually drawn with.\n-\
    \ page.find_tables() gives the same information conveniently: table.cells (or\n  rows[i].cells) are\
    \ the cell rectangles, so column boundaries are the x0/x1 of\n  the cells and the row band is rows[i].bbox.\n\
    \nFor a right-aligned value, the end x is the right edge of its cell minus the\ncell's padding (typically\
    \ 2–5 pt, or whatever padding the rest of the column\nuses — measure it from an existing right-aligned\
    \ neighbour): x_right =\ncell.x1 - pad. To place new text, compute the left edge as\nx_left = x_right\
    \ - page.get_text_length(text, fontname=font, fontsize=size)\nand pass that with the original span's\
    \ baseline origin y. If the column is\ncentre- or left-aligned instead, use cell.x0 + pad or the cell\
    \ centre minus half\nthe text length. Because a PDF does not reflow, you must do this measurement\n\
    yourself — there is no alignment engine to fall back on.\n"
- id: pdf-rowdel-01
  answer: "A correct procedure (PyMuPDF, one page, ruled + shaded table):\n\n1. Open the document and\
    \ locate the table:\n   tabs = page.find_tables(); tab = tabs.tables[0]; row = tab.rows[i].bbox.\n\
    \   row_h = row.y1 - row.y0. Use tab.bbox for the table's x extent.\n2. Decide the region that must\
    \ move: everything from the bottom of the deleted\n   row down to the bottom of the last row, plus\
    \ (usually) the Total line and any\n   notes/footer directly beneath — region = fitz.Rect(tab.bbox.x0,\
    \ row.y1,\n   tab.bbox.x1, bottom_y). Determine bottom_y from the last row bbox and the\n   text blocks\
    \ below the table.\n3. Take an untouched copy for restamping: src = pymupdf.open(path) (a separate\n\
    \   handle to the same file, before any redaction has been applied).\n4. Redact the deleted row and\
    \ the moving region:\n   page.add_redact_annot(row); page.add_redact_annot(region)\n5. Apply with\
    \ the DEFAULT graphics/images handling so the old rules and shading\n   go with the content:\n   page.apply_redactions(images=fitz.PDF_REDACT_IMAGE_PIXELS,\n\
    \                         graphics=fitz.PDF_REDACT_LINE_ART_REMOVE_IF_COVERED)\n   (or just page.apply_redactions()\
    \ — the defaults are what you want here).\n6. Restamp the moved region one row-height higher, as vectors:\n\
    \   target = fitz.Rect(region.x0, region.y0 - row_h, region.x1, region.y1 - row_h)\n   page.show_pdf_page(target,\
    \ src, pno, clip=region)\n   This keeps the content searchable and its exact appearance.\n7. Handle\
    \ overflow: if region.y1 - row_h exceeds the page's bottom margin, create\n   a new page — new = doc.new_page(pno\
    \ + 1, width=..., height=...) — and stamp the\n   overflowing part there with show_pdf_page, then\
    \ RE-FETCH doc[pno] afterwards\n   because inserting a page invalidates existing Page objects.\n8.\
    \ Redraw the rules: the shifted content lost its bottom horizontal rule, so draw\n   the new bottom\
    \ rule at the new last-row y (page.draw_line / draw_rect with\n   the original width/colour taken\
    \ from page.get_drawings()), and re-draw any\n   vertical separators that were inside the redacted\
    \ band.\n9. Update the Total: extract the new column values, recompute, then redact the\n   Total\
    \ cell and insert the new right-aligned number in the matching font, size\n   and colour (see pdf-cell-01).\n\
    10. Save and verify: doc.save(out, garbage=4, deflate=True); re-extract\n    page.get_text() and diff\
    \ against the expected string, re-run find_tables()\n    to confirm the row count dropped by one and\
    \ the geometry is still a clean\n    table, and rasterise the page to eyeball the rules and shading.\n\
    \nNever rasterise the moved rows (get_pixmap -> insert_image): that destroys the\ntext layer and any\
    \ search/extraction check you want to do afterwards. The\nalternative to steps 4–6 is to rebuild the\
    \ rows from tab.extract().\n"
- id: pdf-rowdel-02
  answer: "apply_redactions() defaults are:\n\n- text = PDF_REDACT_TEXT_REMOVE (0) — remove every character\
    \ whose bbox overlaps\n  any redaction rectangle. This is the only default that is not \"do nothing\"\
    .\n- graphics = PDF_REDACT_LINE_ART_REMOVE_IF_COVERED (1) — remove line-art\n  (stroked/filled paths)\
    \ that lies entirely inside the rect. Partly covered\n  shapes are kept whole; the \"2\" variant (REMOVE_IF_TOUCHED)\
    \ removes anything\n  the rect touches at all.\n- images = PDF_REDACT_IMAGE_PIXELS (2) — blank/redact\
    \ the overlapping pixels of\n  images. It does not delete the image object itself.\n\nThere is no\
    \ add_text parameter; replacement text is always a separate\ninsert_text/insert_textbox call.\n\n\
    Why it matters for a table row or cell: a row's identity includes its ruling\nlines and its zebra\
    \ shading. With graphics left at NONE (0), redacting the row\nwould delete the glyphs but leave the\
    \ old horizontal and vertical rules and the\nheader/body fills behind, so you get a ghost grid with\
    \ the new content sitting\ninside it. With the default (1) the lines and fills inside the redacted\
    \ rect go\ntoo, which is exactly what you want when removing a row or removing a whole table\nto rebuild\
    \ it. The exception is a cell edit where you only want to change the\ntext: then pass graphics=0,\
    \ images=0 so the cell's borders and shading survive.\n"
- id: pdf-rowdel-03
  answer: "Yes — page.show_pdf_page(). It stamps a region of a PDF page into the target page\nas a Form\
    \ XObject, so the result is real vector content with a live text layer,\nbyte-for-byte identical in\
    \ appearance (fonts, rules, shading, images all preserved\nbecause you are copying the original drawing\
    \ operations, not a rendering).\n\nPattern:\n\n  src = pymupdf.open(path)          # untouched copy\n\
    \  page.add_redact_annot(moving_region)      # erase it from the original\n  page.apply_redactions()\
    \                    # defaults: text+line art+images\n  target = fitz.Rect(r.x0, r.y0 - row_h, r.x1,\
    \ r.y1 - row_h)\n  page.show_pdf_page(target, src, pno, clip=r)\n\nTwo caveats:\n\n- target and clip\
    \ are the same size if you want a pure translation. If the sizes\n  differ, show_pdf_page scales the\
    \ clip to the target rect, which will change the\n  font size and look wrong — so compute the shift\
    \ exactly.\n- It is still not reflow. Content that now crosses the page's bottom margin has\n  to\
    \ be stamped onto a new page (doc.new_page(pno + 1, ...), then re-fetch\n  doc[pno] since Page objects\
    \ are invalidated by the insertion). And the moved\n  region's outer rules are gone with the redaction,\
    \ so you must redraw the bottom\n  rule at the new position.\n\nFor moving content between pages,\
    \ the same call works — clip from the source page,\n  target on the destination page.\n"
- id: pdf-rowdel-04
  answer: "Beyond the deleted row itself, the usual knock-on edits are:\n\n- The Total: recompute from\
    \ the remaining line items and re-stamp the total cell\n  (and any subtotal, tax, discount, shipping,\
    \ deposit or balance-due figures that\n  are derived from the Total).\n- The table's geometry: the\
    \ bottom rule and the outer border move up by the deleted\n  height; the alternating/row shading pattern\
    \ re-strikes, and the row count\n  changes (so a \"no. of items\" or \"qty total\" line above/below,\
    \ if present, is\n  also wrong).\n- Everything positioned below the table: subtotal block, bank details,\
    \ terms and\n  conditions, notes, signature lines, \"page x of y\" footers — all of which must be\n\
    \  redacted and restamped lower, with any overflow moved to a new page.\n- Any prose that references\
    \ the deleted item or a total (\"Thank you for your order\n  of N items\", \"as per quotation ...\"\
    ).\n- Structural metadata: if the document has a bookmark/outline, a tagged-PDF\n  structure tree,\
    \ or a table repeated as a \"continued\" block on the next page,\n  those need to be corrected too.\n\
    - Accessibility/consistency: after any of this, re-extract the text to confirm the\n  deleted row's\
    \ text is genuinely gone and the document is still readable to a\n  text extractor.\n"
- id: pdf-cell-01
  answer: "Steps to make the replacement look native:\n\n1. Read the original style. From page.get_text(\"\
    dict\") take the span containing\n   the old value and note span[\"font\"], span[\"size\"], span[\"\
    color\"] and\n   span[\"origin\"] (the baseline start). Remember flags & 16 is bold, flags & 2\n \
    \  is italic; read the font name too, since flags are only a hint.\n2. Locate the old text. rects\
    \ = page.search_for(\"22.50\") gives the character rects;\n   cross-check against the cell bbox from\
    \ page.find_tables() to be sure you hit\n   the right cell when the value appears more than once.\n\
    3. Pad the target rect slightly (1–2 pt on each side, enough to cover ascender and\n   descender boxes)\
    \ so no character survives.\n4. Redact, keeping the table's chrome:\n     page.add_redact_annot(padded_rect)\n\
    \     page.apply_redactions(graphics=fitz.PDF_REDACT_LINE_ART_NONE,\n                           images=fitz.PDF_REDACT_IMAGE_NONE)\n\
    \   The non-default graphics/images values are the point here: the cell's borders\n   and shading\
    \ must survive while its text is removed.\n5. Compute the position for right alignment:\n     size,\
    \ fontname = span[\"size\"], span[\"font\"]   # map to an insertable name\n     pad = 3          \
    \                              # match the column's padding\n     x = cell.x1 - pad - page.get_text_length(\"\
    54.00\", fontname=fontname,\n                                              fontsize=size)\n     y\
    \ = span[\"origin\"].y\n   Baseline, not bbox top-left.\n6. If the font is not one of the built-ins\
    \ (\"helv\", \"hebo\", \"tiro\", ...), embed it\n   first with page.insert_font(fontname=\"F0\", fontfile=path),\
    \ and check\n   pymupdf.Font(fontfile=path).has_glyph(ord(c)) for every character you insert —\n \
    \  embedded subsets (AAAAAA+Name) may not contain the glyphs you need.\n7. Insert: page.insert_text((x,\
    \ y), \"54.00\", fontname=..., fontsize=size,\n   color=(r, g, b) converted from the integer span\
    \ colour, fill=fill,\n   render_mode=0, overlay=True).\n8. Verify: page.get_text(\"clip\") over the\
    \ cell returns exactly \"54.00\", the old\n   \"22.50\" is gone from the whole page, and a rasterised\
    \ crop looks identical in\n   weight, size and colour to its neighbours.\n"
- id: pdf-cell-02
  answer: 'Because text=PDF_REDACT_TEXT_REMOVE deletes ANY character whose bbox overlaps the

    rect, the rect has to be large enough to fully cover every glyph you want gone and

    no larger. Too small and you clip a glyph: search_for rects cover advance boxes,

    and glyph ink (and the bbox PyMuPDF reports for tall/descending characters) can

    extend beyond them, so you end up with a fragment — a stray comma, a "0" or the

    tail of a "5" — left behind. So pad the rect by a point or two on every side, and

    run it the full cell height, not just the reported match rect.


    Symmetrically, the rect must not overlap the neighbouring cells'' characters. In a

    tight table adjacent cells can be only 3–6 pt away, so there is very little slack:

    expand toward the target text''s own side and toward the cell''s interior, keep the

    far edges inside the cell, and never use the whole row or column rect for a

    single-cell edit.


    And because the neighbouring cell''s rules/shading must not disappear, apply with

    graphics=PDF_REDACT_LINE_ART_NONE and images=PDF_REDACT_IMAGE_NONE for a cell

    edit. The general rule: size the rect to the text, and verify by re-extracting

    the text of the cell and of its immediate neighbours.

    '
- id: pdf-cell-03
  answer: "insert_textbox(rect, text, ...) returns a float. When the text does not fit it\nreturns a NEGATIVE\
    \ number — the number of missing lines' worth of height, roughly\n— and it writes absolutely NOTHING.\
    \ There is no exception, no warning and no\npartial text. So the only way to find out is to check\
    \ the return value:\n\n  rc = page.insert_textbox(rect, text, fontname=..., fontsize=...)\n  if rc\
    \ < 0:\n      ... the cell still shows the old/empty content ...\n\nOptions when it doesn't fit:\n\
    \n- Shrink the font size until rc >= 0 (and check it still matches the column's\n  visual style).\n\
    - Widen the cell / the column, or wrap the text over a taller rect — which means\n  relaying out the\
    \ table (redact and redraw the grid) rather than a cell edit.\n- Use insert_text() with manual line\
    \ breaks and computed per-line positions, which\n  gives you control over wrapping.\n- Abbreviate\
    \ or truncate the value, or switch units (e.g. \"1,234.5k\").\n- Rotate the text (morph/rotate) or\
    \ reduce inter-character spacing.\n- Let the row grow taller and redact/restamp the content below\
    \ with\n  show_pdf_page — more work, but keeps the value intact.\n\nEither way, assert rc >= 0 before\
    \ moving on, and re-extract the cell text to\nconfirm what was actually written.\n"
- id: pdf-cell-04
  answer: 'page.insert_text(point, text) places the point at the BASELINE start of the

    inserted text — that is, the origin of the first glyph''s baseline, the same

    coordinate as a span''s "origin" from get_text("dict"). It is not a top-left corner,

    not a centre and not a bounding box anchor.


    If you pass the top-left of the old word''s bbox (x0, y0) the text is drawn about

    one line too high: the glyphs hang below the point, so the new value sits above

    where the old one was, with a visible gap to the cell''s bottom border and it may

    even clip against the top rule. Roughly, the correction is the ascender — about

    0.7–0.8 x fontsize below the bbox top (or y0 + span["ascender"] if the span

    reports it).


    So: reuse the original span''s origin for y. For x, use span["origin"].x for

    left-aligned text, or, for right-aligned numbers, compute

    x = right_edge - page.get_text_length(text, fontname, fontsize) so the new value

    ends on exactly the same right margin as its neighbours.

    '
- id: pdf-relayout-01
  answer: "A PDF does not reflow, so every one of these must be handled explicitly:\n\n1. Make room. Redact\
    \ the existing Total row (and the strip immediately below the\n   last data row) so the new row's\
    \ band is free.\n2. Stamp the new row. Either rebuild it from the extracted data\n   (tab.extract())\
    \ and draw the text with the matching font/size/colour/alignment\n   and the correct cell shading,\
    \ or copy an existing body row with\n   page.show_pdf_page(new_row_rect, src, pno, clip=existing_row_bbox)\
    \ and then\n   overwrite its text cells — the copy route preserves the borders, shading and\n   alignment\
    \ for free.\n3. Move the Total row down by the new row's height: redact it, then restamp it\n   with\
    \ show_pdf_page at target = old_rect - (0, row_h, 0, row_h). Alternatively\n   redraw the Total label\
    \ and figure in place at the new y, and update the figure\n   itself (new subtotal/total; and tax/discount/balance\
    \ if those are present).\n4. Rules. Redraw every horizontal rule that bounds the inserted band and\
    \ the\n   shifted Total row, keep the table's outer top/bottom borders where they are, and\n   re-draw\
    \ the vertical separators across the inserted band — the old rules were\n   consumed by the redaction\
    \ (with the default graphics removal) and will not\n   reappear by themselves.\n5. Shading. If the\
    \ table is zebra-striped, replicate the correct fill for the new\n   row's parity (page.draw_rect\
    \ with fill= the sampled colour and fill_opacity).\n6. Content below the table (subtotal block, notes,\
    \ terms, signature, footer) must\n   be redacted and restamped lower with show_pdf_page(shifted_rect,\
    \ src, pno,\n   clip=region). Whatever crosses the page bottom goes onto a new page created with\n\
    \   doc.new_page(pno + 1, ...) — and re-fetch doc[pno] afterwards because inserting\n   a page invalidates\
    \ existing Page objects.\n7. Verify: re-extract text, re-run find_tables() to confirm the grid is\
    \ still\n   detected with the extra row, and rasterise to eyeball the rules and shading.\n"
- id: pdf-relayout-02
  answer: "Correct approach:\n\n1. Keep the total width. Compute the table's current x extent (tab.bbox\
    \ or the\n   vertical rules from page.get_drawings()) and split the SAME total width into\n   the\
    \ new column count. Take the new column's width out of the existing columns\n   (usually from the\
    \ widest one, or proportionally) — never let the table grow past\n   the margins.\n2. Redact the entire\
    \ old table — the whole tab.bbox — and apply with the DEFAULT\n   graphics removal (graphics=PDF_REDACT_LINE_ART_REMOVE_IF_COVERED).\
    \ With\n   graphics=PDF_REDACT_LINE_ART_NONE the old rules, header fill and cell borders\n   stay\
    \ in place at the old x positions and you end up with a ghost grid showing\n   through the new one.\n\
    3. Redraw everything: the table's new column grid (vertical rules at the new\n   boundaries, horizontal\
    \ rules per row, the outer border), the header cells\n   including the new column's header, every\
    \ cell's text, and the shading/fills.\n4. Measure before writing. Use page.get_text_length(text, fontname,\
    \ fontsize) for\n   every cell at its NEW width and deliberately shrink the font, wrap onto extra\n\
    \   lines, or reformat (e.g. thousands separators, dates) wherever it no longer\n   fits. Because\
    \ a PDF has no reflow, whatever you decide must be done by hand.\n5. Move what is below: redact the\
    \ content under the table and restamp it with\n   show_pdf_page(shifted_rect, src, pno, clip=region),\
    \ spilling onto a new page if\n   it passes the page bottom (re-fetch doc[pno] after new_page).\n\
    6. Verify with find_tables() and a rasterised render.\n\nCommon mistakes: only inserting the new column\
    \ while leaving the old grid, header\n  fill and row rules in place; using graphics=PDF_REDACT_LINE_ART_NONE\
    \ so the old\n  header shading and rules show through; making the table wider than the margins\n \
    \ instead of redistributing existing width; not re-measuring cell text at the new\n  widths so values\
    \ overflow or collide; and rasterising the table (get_pixmap ->\n  insert_image) which destroys the\
    \ text layer.\n"
- id: pdf-relayout-03
  answer: "Acceptable when: the PDF was itself generated from a source you can still\nget at (a reportlab/WeasyPrint\
    \ script, a template, a spreadsheet, an HTML\npage) — in that case the right move is to change the\
    \ source and re-render,\nnot to round-trip. A round-trip is also defensible when the edit is\nlarge\
    \ (re-laying out most of a page, restyling every cell, changing the\npage count) and the user accepts\
    \ a visual change, or when the PDF is a\nthin wrapper and fidelity of the untouched parts is genuinely\
    \ not\nimportant.\n\nRisks:\n- Fonts get substituted or re-embedded; subset fonts lose their original\n\
    \  glyph set and metrics, so text width and line breaks change.\n- Positions shift. Margins, baselines,\
    \ leading, hyphenation and table\n  borders all get re-laid out; pages reflow and the page count changes.\n\
    - Headers/footers, page numbers, stamps and vector rules are often lost\n  or duplicated by the converter.\n\
    - Vector line art, fills, borders and clipping paths usually survive badly\n  or not at all; images\
    \ may be downsampled or re-compressed.\n- Bookmarks, links, form fields, annotations, tagged-PDF structure\
    \ and\n  metadata are usually lost.\n- Round-tripping is lossy in a way you cannot easily audit, so\
    \ a\n  \"minimal\" edit can silently rewrite the other 20 pages.\n- Some converters render the result\
    \ slightly differently depending on the\n  fonts available on the machine, so the output is not reproducible.\n\
    \nThe guidance: before any PDF -> DOCX/HTML -> PDF round trip, ask whether\nthe original source exists\
    \ and regenerate from it. Otherwise do a surgical\nin-place edit (redact + restamp with `show_pdf_page`,\
    \ or redraw from\nextracted data). The round trip is a last resort.\n"
- id: pdf-relayout-04
  answer: "Build a picture of the free space and test it against real content\ngeometry, not eyeballed\
    \ pixels.\n\nPractical steps:\n1. Get the text geometry: `page.get_text(\"words\")` (or `\"dict\"\
    ` /\n   `\"blocks\"`) gives bboxes for every word, span and block. If the page has\n   a `/Rotate`,\
    \ remember these bboxes are in unrotated coordinates while\n   `page.rect` is the displayed size —\
    \ convert with\n   `rect * page.rotation_matrix` and back with `* page.derotation_matrix`.\n2. Get\
    \ the non-text content too: `page.get_drawings()` for vector line\n   art (borders, rules, fills),\
    \ and `page.get_images()` /\n   `page.get_image_info()` for image bboxes.\n3. Build a list of occupied\
    \ rects (optionally inflated by a small padding,\n   a few points, so you do not butt up against existing\
    \ text) and compute\n   the free rects inside the content area — inside the margins, not the\n   whole\
    \ `page.rect`.\n4. Test whether a candidate rect is clear: it must not intersect any\n   occupied\
    \ rect, and must lie inside the content box. If it intersects\n   something, it is not free space.\n\
    5. Check the vertical budget: the bottom of the new content must be above\n   the footer/header band,\
    \ and must not run past the bottom margin. A table\n   that is taller than the remaining space must\
    \ be clipped, wrapped to a\n   new page, or resized.\n6. Do a visual sanity check at the end: render\
    \ the page\n   (`page.get_pixmap(...)`) and look at it, or convert to PNG and view it.\n   Geometry\
    \ checks catch overlaps; only looking catches visual glitches.\n\nCaveat: annotations, form fields,\
    \ optional content, and content hidden\nbehind a white filled rectangle can fool a purely geometric\
    \ check, so a\nrendered look is the backstop.\n"
- id: pdf-tblins-01
  answer: "Two practical ways, in Python:\n\n1. Draw it with the drawing primitives yourself.\n   `p =\
    \ doc[0]`; pick a `rect` in genuinely free space; `p.draw_rect(cell,\n   color=(0,0,0), width=0.6)`\
    \ per cell plus the outer border and horizontal\n   separators, and `p.insert_text((x, baseline),\
    \ \"Header\", fontname=\"hebo\",\n   fontsize=9)` or `p.insert_textbox(cell, text, fontname=\"helv\"\
    ,\n   fontsize=8, align=1)` for the labels. Check the `insert_textbox` return\n   value: it is negative\
    \ and writes nothing when the text does not fit.\n   This gives exact control over rules, shading\
    \ and alignment.\n\n2. Let PyMuPDF lay the table out for you with `insert_htmlbox`.\n   `p.insert_htmlbox(rect,\
    \ \"<table><tr><th>...</th></tr>...</table>\",\n   css=my_css, scale_low=0.8)` writes ordinary page\
    \ content and creates no\n   annotation. Point `rect` at free space, size it to the columns, and tune\n\
    \   the CSS (border-collapse, font-size, padding) to match the document. The\n   same method also\
    \ works on a fresh page you create with `doc.new_page()`.\n\nChoose 1 when you need the table to look\
    \ exactly like a hand-drawn\nvector table already in the file; choose 2 when the content is more\n\
    irregular and you want the engine to do the box math.\n"
- id: pdf-tblins-02
  answer: "Consistency and glitch avoidance both come from measuring the document\nrather than guessing.\n\
    \n- Match the existing strokes: read `page.get_drawings()` and copy the\n  `width` and `color` (and\
    \ `dashes`, `fill`) of the existing table rules.\n  Draw with the same line width so a 0.5pt grid\
    \ does not sit next to a\n  hairline.\n- Match type: read the spans (`page.get_text(\"dict\")`) and\
    \ copy the font\n  name / base-14 name, `span[\"size\"]`, colour, and weight (bold is\n  `flags &\
    \ 16`, italic is `flags & 2`). The flag bits are a hint, not the\n  authority — also read the font\
    \ name (\"Helvetica-Bold\").\n- Match geometry: use the same left margin, the same row height, the\
    \ same\n  inner padding, and vertical alignment (top / baseline / bottom) that the\n  existing table\
    \ uses. Header shading should match: copy the existing\n  fill colour, and if the document alternates\
    \ row shading, set\n  `fill=` per row rather than leaving the new table flat.\n- Respect the existing\
    \ text: do not draw a new table over existing content\n  even inside a generously padded block bbox.\n\
    \nGlitches to watch for:\n- Snapping thin rules to whole points avoids half-covered 0.5pt lines;\n\
    \  keep the outer border from double-drawing against the inner rules.\n- A fill painted after the\
    \ rules hides them, so set fills first, or use\n  `shape.finish(fill=..., color=...)` in the right\
    \ order.\n- Text that does not fit: `insert_textbox` returns a negative number and\n  writes nothing\
    \ — test `rc < 0`, then shrink the font, wrap, widen the\n  column or grow the row rather than leaving\
    \ a blank cell.\n- Right-aligned numbers should go at\n  `x = right_edge - get_text_length(text, fontname,\
    \ fontsize)`, not at the\n  cell's left edge, or the column looks ragged.\n"
- id: pdf-tblins-03
  answer: "A PDF does not reflow, so you cannot simply \"make room\" in place. Your\noptions:\n\n1. Shift\
    \ the content below the insertion point down on the same page:\n   redact everything from the insertion\
    \ point to the bottom of the page\n   (with the default graphics removal so old rules/fills go too),\
    \ then\n   restamp it lower with\n   `page.show_pdf_page(shifted_rect, untouched_copy, pno, clip=region)`.\n\
    \   Then stamp whatever would pass the page bottom onto a new page inserted\n   after it — `doc.new_page(pno\
    \ + 1, width=..., height=...)` — and re-fetch\n   `doc[pno]` afterwards, because inserting a page\
    \ invalidates existing\n   `Page` objects. Redrawing only the text operators (`Td`/`Tm`) is not\n\
    \   enough: the rules, fills and images stay where they were.\n\n2. Put the table in real free space\
    \ elsewhere on the page (or on a later\n   page) and tell the user where it went.\n\n3. Go back to\
    \ the source. If the document came from a template, HTML,\n   reportlab code or a spreadsheet, add\
    \ the table there and regenerate —\n   this is the clean answer when it is available.\n\nDo not use\
    \ `insert_htmlbox` with an annotation and then \"bake\" it as a\ntrick to place the table; `insert_htmlbox`\
    \ writes ordinary page content\nand creates no annotation anyway, so the annotation route is not a\n\
    workaround. And never draw the table over existing text, even inside a\npadded block bbox.\n"
- id: pdf-imgrep-01
  answer: "Find the existing image's placement, then either redact it and stamp the\nnew one, or use `page.delete_image`\
    \ / replace the xref.\n\nTypical approach:\n1. Locate it: `page.get_image_info(xrefs=True)` (or\n\
    \   `page.get_images(full=True)` plus `page.get_image_rects(xref)`) to get\n   the xref and the bbox\
    \ each image is drawn at. `get_image_info` reports\n   the placement rect directly and also covers\
    \ images inside form\n   XObjects and inline images that `get_images` can miss.\n2. Keep that exact\
    \ rect, or `page.get_image_rects(xref)` for a specific\n   xref.\n3. Remove the old one: `page.add_redact_annot(rect)`\
    \ + `page.apply_redactions()`\n   (graphics=1 / the default so any border or fill under it goes too),\
    \ or\n   `page.delete_image(page.get_images()[0][0])` if you want the object\n   gone rather than\
    \ just painted over.\n4. Insert the replacement:\n   `page.insert_image(rect, filename=\"new_logo.png\"\
    , keep_proportion=True)`\n   — or compute the rect yourself from the PNG's width/height with\n   `pix.width`\
    \ / `pix.height`.\n\nThe catch with shared images: a single image xref is very often drawn on\nmany\
    \ pages (and sometimes many times on one page). `insert_image` without\n`keep_proportion` will distort\
    \ the new logo; and if you simply delete or\nreplace the xref you will change the logo on *every*\
    \ page that shares it.\nRedacting the rect on one page is page-local and safe. Also note that\n`insert_image`\
    \ adds a *new* xref while the old one may still be referenced\nelsewhere, so the file can carry two\
    \ copies — save with\n`garbage=4, deflate=True` to merge identical streams.\n"
- id: pdf-imgrep-02
  answer: "Several real problems, even though the page *looks* right:\n\n- The old image is still in the\
    \ file. It is still stored, still in the\n  content stream, and still extractable (`get_text` does\
    \ not see it, but\n  `get_images`, `doc.extract_image` or any other tool can pull it out). If\n  the\
    \ old logo is a confidential or superseded asset, the replacement\n  hides nothing.\n- Anything drawn\
    \ on top of the old logo is now covered. A white\n  background rectangle baked into your new PNG,\
    \ or the old logo's own\n  opaque background, will bury text, rules, or a later stamp that should\n\
    \  appear above the logo.\n- The new image may not align with the old one's baseline or optical\n\
    \  centre even if the bounding box matches, so it reads as \"off\" on close\n  inspection.\n- With\
    \ `overlay=True` (the default) your image is on top; that is what you\n  want here, but if the page\
    \ has a filled background box drawn after it\n  you need `overlay=False` to sit underneath — the two\
    \ directions are easy\n  to confuse.\n- It is invisible in the file structure: `get_images()` will\
    \ report two\n  images where the design says one, and anyone auditing the PDF (or any\n  future redraw,\
    \ reflow or re-compression) will be surprised.\n\nRight approach: redact the old rect and insert the\
    \ new one, so the old\nxref is actually gone from that page's content.\n"
- id: pdf-imgrep-03
  answer: "Likely causes:\n- The logo is a *vector* drawing, not a raster image — an SVG-style logo\n\
    \  built from `draw_rect`/`draw_curve`/fills shows up in\n  `page.get_drawings()` and in `get_text`,\
    \ but `get_images()` only reports\n  image XObjects, so it is empty.\n- It is an inline image (`BI\
    \ ... ID ... EI`) in the content stream, or it\n  lives in an image inside a Form XObject (common\
    \ for logos placed in\n  header/footer templates). `get_images()` on the page may not descend\n  into\
    \ the form.\n- It is a raster drawn as a tiling pattern, a shading (gradient) pattern,\n  or a mask/stencil\
    \ used as a logo.\n- You are looking at the wrong page object, or the image is on a page whose\n \
    \ content is inherited/rotated.\n- It is text — the logo is a styled text run, not an image at all.\n\
    \nHow to find it:\n- `page.get_image_info(xrefs=True)` — this reports placement rects and\n  xrefs,\
    \ including inline and form-nested cases that `get_images()` misses.\n- `page.get_drawings()` for\
    \ the vector case; note the bboxes so you can\n  redact that region and redraw it.\n- `page.get_text(\"\
    dict\")` to check whether it is really text.\n- Unpack: `page.read_contents()` (decoded bytes — raw\
    \ streams are usually\n  Flate-compressed, so a byte search on the file finds nothing) or\n  `doc.xref_stream(xref)`\
    \ to inspect the operators, and walk\n  `doc.xref_get_key(xref, \"Resources\")` for form XObjects.\n\
    - Or just `page.get_pixmap(clip=that_rect, dpi=300)` to see the region and\n  work out its bounds\
    \ empirically.\n\nThen replace it by redacting the rect (default graphics removal) and\ninserting\
    \ the new image at the same rect.\n"
- id: pdf-imgins-01
  answer: "Compute the height from the PNG's own aspect ratio, then find a clear\nband at the bottom right.\n\
    \n1. Read the dimensions: `pix = pymupdf.Pixmap(\"signature.png\")`; the\n   aspect is `pix.height\
    \ / pix.width` (or use `pymupdf.Rect` from\n   `pymupdf.Pixmap` plus `pymupdf.utils` — do not hard-code\
    \ 72 dpi\n   assumptions; the ratio is what matters). For width 150pt:\n   `h = 150 * pix.height /\
    \ pix.width`.\n\n2. Pick the rect. With a margin `m` (say 36pt):\n   `rect = pymupdf.Rect(page.rect.width\
    \ - m - 150, page.rect.height - m - h,\n                        page.rect.width - m, page.rect.height\
    \ - m)`.\n   On a `/Rotate` page, `page.rect` is the displayed size, which is what\n   you want for\
    \ placement; but the bboxes from `get_text` are in\n   *unrotated* coordinates, so compare like with\
    \ like (convert with\n   `rect * page.rotation_matrix` / `* page.derotation_matrix`).\n\n3. Check\
    \ it does not cover text: gather the occupied rects\n   (`page.get_text(\"words\")` bboxes, plus `page.get_drawings()`\
    \ and\n   `page.get_image_info()`), and verify `rect` does not intersect any of\n   them. If it does,\
    \ shrink, move left, or reduce the width and retry.\n   150pt wide is ~2 inches, so on a letter page\
    \ the bottom right is often\n   above a footer — the check is what tells you, not the assumption.\n\
    \n4. Insert: `page.insert_image(rect, filename=\"signature.png\")` with the\n   rect already at the\
    \ right aspect. If you only know the width, use\n   `keep_proportion=True` and pass a rect with a\
    \ generous height, letting\n   PyMuPDF fit the image inside and preserve the ratio.\n\n5. Verify:\
    \ `page.get_image_info()` should list the new image with that\n   rect, and a `get_pixmap` render\
    \ of the region should show it. Save with\n  `garbage=4, deflate=True`.\n"
- id: pdf-imgins-02
  answer: "Do not let each `insert_image` call create its own copy. Two mechanisms:\n\n1. Reuse the image\
    \ object. The first insertion returns the xref:\n   `xref = page.insert_image(rect, filename=\"logo.png\"\
    )`; on every later\n   page call `page.insert_image(rect, xref=xref)`. That references the\n   same\
    \ image object rather than embedding it again.\n\n2. Let the save dedupe. `doc.save(path, garbage=4,\
    \ deflate=True)` merges\n   identical image streams and drops unreferenced objects. Lower garbage\n\
    \   levels keep the copies. Note PyMuPDF may reuse an identical image\n   within a session anyway,\
    \ so 200 calls can still yield one object.\n\nAlso worth checking: if the document already has the\
    \ logo on some pages,\nreuse *that* xref rather than embedding your own copy at all.\n\nAnd never\
    \ try to fix this by rewriting a page's content stream by hand\n(`update_object`) to \"reuse\" an\
    \ image — that is the fragile path. And the\nreal fix for a logo on every page is a template: put\
    \ the logo in the\ndocument's header/footer (or a form XObject) and stamp it, rather than\nstamping\
    \ page content 200 times.\n\nVerify with `len({i[0] for p in doc for i in p.get_images()})` — one\
    \ xref\nfor one image — and compare the saved file size against the original.\n"
- id: pdf-imgins-03
  answer: "It does not appear:\n- The rect is wrong. It is off-page, has zero or negative width/height,\n\
    \  or lies outside `page.rect` / the CropBox, so the content is clipped or\n  simply not on the page.\n\
    - It landed under other content. With `overlay=True` (the default) you\n  draw on top; if you passed\
    \ `overlay=False` you put the image *beneath*\n  existing page content, which is not a no-op — under\
    \ a filled\n  background box or an opaque rectangle the image is completely hidden.\n- It went to\
    \ a stale `Page` object. Inserting a page with `doc.new_page()`\n  invalidates existing `Page` objects;\
    \ if you then insert onto the old\n  handle you may write to a page that is no longer where you think.\n\
    - It is a broken or unsupported file: an unusual format, a corrupt PNG, or\n  a colour space issue.\
    \ Check that `insert_image` returned a sensible\n  result and re-extract it.\n- It was written but\
    \ is invisible at the viewing zoom / DPI you rendered\n  at, or it is white-on-white.\n- You saved\
    \ before the insertion, or saved a different path than the one\n  you are looking at.\n\nAppears underneath\
    \ a filled background box: the content stream order puts\n  the box after the image, and the box is\
    \ opaque. Fixes: insert with\n  `overlay=True` so the image is appended last, or — if the image is\n\
    \  supposed to sit *under* a translucent/solid panel by design — keep\n  `overlay=False` but make\
    \ sure the box really is above it and is\n  semi-transparent.\n\nDiagnose by rendering the region\n\
    (`page.get_pixmap(clip=rect, dpi=200)`) and by checking\n`page.get_image_info()` for the expected\
    \ rect.\n"
- id: pdf-fonts-01
  answer: "Why it fails: the embedded font is a *subset* (`AAAAAA+Georgia`). A subset\ncontains only the\
    \ glyphs the original document actually used. Characters\nthat never appeared have no glyph, so inserting\
    \ them either draws nothing,\ndraws a blank/notdef box, gets substituted by a fallback font, or is\n\
    silently dropped — even though the font object is present and the code\n\"succeeds\".\n\nWhat to do:\n\
    1. Check coverage before you commit to a font:\n   `pymupdf.Font(fontfile=path).has_glyph(ord(c))`\
    \ returns 0 for a missing\n   glyph. Run it over every character in your new string.\n2. If coverage\
    \ is missing, use a different font that has the glyphs — an\n   installed system font (e.g. a TTF/OTF)\
    \ or a base-14 font where the\n   characters are in WinAnsi. Accept the small metric difference in\
    \ the\n   cell, or pick the closest available face.\n3. Embed it explicitly: `page.insert_font(fontname=\"\
    F0\", fontfile=path)`,\n   then `page.insert_text` / `insert_textbox` with `fontname=\"F0\"`. Never\n\
    \  rely on an already-embedded name for a new glyph.\n4. Confirm by re-extracting the text: after\
    \ inserting, `page.get_text()`\n   must contain the new string, and the new span's font name should\
    \ be\n   your `F0` (or the expected base-14 name), not the subset name.\n5. If the document's font\
    \ is licensed/embedded-only and you cannot get a\n   full version, the pragmatic answer is to keep\
    \ the edit inside the\n   existing glyph repertoire, or replace the item name with something the\n\
    \   subset can render.\n\nAlso: verify the new text *fits* — a longer name in a different face may\n\
    overflow the cell, and `insert_textbox` will return a negative value and\nwrite nothing.\n"
- id: pdf-fonts-02
  answer: "Base-14 names in PyMuPDF / PDF standard fonts:\n- Helvetica regular: `\"helv\"`, bold: `\"\
    hebo\"`.\n- Also in the same family: `\"heit\"` (Helvetica-Oblique),\n  `\"hebi\"` (Helvetica-BoldOblique).\n\
    - Times: `\"tiro\"`, `\"tibo\"`, `\"tiit\"`, `\"tibi\"`.\n- Courier: `\"cour\"`, `\"cobo\"`, `\"coit\"\
    `, `\"cobi\"`.\n- The generic `\"sans-serif\"`, `\"serif\"`, `\"monospace\"` aliases also work.\n\n\
    Limitation: base-14 fonts are *not embedded* — they are referenced by\nname and the viewer supplies\
    \ the glyphs, so they only cover Latin-1 /\nWinAnsi characters. Anything outside that repertoire (Cyrillic,\
    \ Greek,\nCJK, Arabic, most emoji, curly quotes in some viewers, and many\ntypographic symbols) has\
    \ no glyph and will render as blanks, notdef boxes\nor be dropped. For non-Latin or special text you\
    \ must embed a real font\nfile with `page.insert_font(fontname=\"F0\", fontfile=path)` and check\n\
    coverage with `pymupdf.Font(fontfile=path).has_glyph(ord(c))` first. Note\nalso that base-14 metrics\
    \ are the Adobe standard metrics, so text measured\nwith `get_text_length` against a substituted font\
    \ may not match what the\nviewer draws.\n"
- id: pdf-fonts-03
  answer: "Inspect the span you are replacing, and copy from it rather than assuming.\n\n1. Get the original\
    \ span's properties:\n   `d = page.get_text(\"dict\")` (or `\"rawdict\"`), walk to the block/line,\n\
    \   and read the span: `span[\"size\"]` for the size, `span[\"font\"]` for the\n   font name, `span[\"\
    color\"]` for the colour, and `span[\"flags\"]`.\n2. Weight from the flags: bold is `flags & 16`,\
    \ italic is `flags & 2`\n   (superscript 1, serif 4, monospaced 8). These bits are a *hint*, not\n\
    \   the authority — also read `span[\"font\"]`. A span named\n   \"Helvetica-Bold\" or \"hebo\" tells\
    \ you the base-14 face to use; a name\n   containing \"Bold\" is the reliable signal when the flag\
    \ disagrees.\n3. Insert with the matching face: `page.insert_text(origin, text,\n   fontname=\"hebo\"\
    , fontsize=span[\"size\"])`, or an embedded font\n   `page.insert_font(fontname=\"F0\", fontfile=path)`\
    \ first. Use\n   `fontsize = span[\"size\"]` exactly, not a rounded or guessed value —\n   that is\
    \ what fixes the \"slightly different size\".\n4. Use the original span's `origin` (the baseline start)\
    \ rather than the\n   bbox top-left: `insert_text(point)` places the baseline at `point`, so a\n \
    \  bbox corner draws the text about one line too high.\n5. Redact the old cell text first (text=0\
    \ removes any character whose bbox\n   overlaps) and pass `graphics=0, images=0` if you want to keep\
    \ the cell's\n   borders and shading while changing its text; keep the default\n   `graphics=1` if\
    \ you are removing the cell entirely.\n6. Right-aligned numbers:\n   `x = right_edge - page.get_text_length(text,\
    \ fontname, fontsize)`.\n7. Check `insert_textbox`'s return value (`rc < 0` means nothing was\n  \
    \ written) and verify by re-extracting the text and comparing the new\n   span's size and font name\
    \ to the originals.\n"
- id: pdf-verify-01
  answer: "Verify the *content*, not the exit code. Layered checks:\n\n1. Text extraction: `page.get_text()`\
    \ and `page.get_text(\"words\")` must\n   contain the new string, and the old string must be gone.\
    \ If you renamed\n   a line item, assert on the parsed value, not on a substring.\n2. Geometry: assert\
    \ the new text's bbox is inside the intended cell\n   (`page.search_for(\"new text\")` gives you rects)\
    \ and that it does not\n   overlap neighbouring cells' text rects. Also assert the total row value\n\
    \   was recomputed and equals the sum of the Amount column — recomputing and\n   comparing catches\
    \ a bad input table instead of silently propagating it.\n3. Structure: `page.find_tables()` / `page.get_drawings()`\
    \ should still\n   find the expected number of rows/columns and the rules should be where\n   you\
    \ expect. A vanished border or a duplicated `w:tblGrid`-style rule is\n   a common failure that text\
    \ extraction will not show.\n4. Round-trip through a consumer: extract with a second, independent\
    \ tool\n   (pandoc to markdown, python-docx-style read-back, a different PDF\n   library) and diff\
    \ the result against the original — exactly one changed\n   line/item is what you want.\n5. Package\
    \ integrity: `unzip -t` equivalent / reopen the saved file with\n   `pymupdf.open()` and confirm page\
    \ count, file size sanity, and that no\n   page fails to render.\n6. Render and *look*: `page.get_pixmap(dpi=150)`\
    \ (or a clip of the edited\n   region) and actually view the image. Only a rendered look catches\n\
    \   half-covered rules, misplaced baselines and shading glitches. Absence of\n   a renderer (e.g.\
    \ no LibreOffice) is a real gap you should report rather\n   than paper over.\n7. Minimality: diff\
    \ the changed part against the original bytes to confirm\n   nothing else moved. For a text replacement,\
    \ a byte-level diff of the\n   changed span should be no wider than the span you edited.\n\nAnd compare\
    \ the total before and after, plus a `get_text` read-back, as\nthe two cheap checks that catch most\
    \ mistakes.\n"
- id: pdf-verify-02
  answer: "No. The row may be gone from the *displayed* page, but it is almost\ncertainly still in the\
    \ file.\n\nTwo separate problems:\n\n1. The old bytes are still there. An incremental save appends\
    \ the updated\n   objects to the end of the original file and only updates the xref\n   table; the\
    \ previous revision is never removed. The original content\n   stream — including the confidential\
    \ row's text operators — is still\n   physically present in the file and can be recovered by parsing\
    \ the old\n   revision or by searching the raw bytes. Redaction hides content; it does\n   not shred\
    \ it. A redaction only reliably removes content if the\n   surrounding graphics are also removed and\
    \ the file is written fresh\n   (full save with garbage collection, not incremental).\n\n2. Even in\
    \ the new revision, a redaction annotation is a *cover-up* unless\n   the text was actually removed.\
    \ `page.add_redact_annot(rect)` followed by\n   `page.apply_redactions()` is what removes the characters\
    \ (text=0 removes\n   any character whose bbox overlaps). If you only added the annotation\n   and\
    \ did not apply it, you have just drawn a black box.\n\nCorrect practice for a confidential row:\n\
    - redact + `apply_redactions()` with the default graphics removal so the\n  old rules and fills do\
    \ not linger;\n- `doc.save(path, garbage=4, deflate=True)` — a *full* save, not\n  `incremental=True`,\
    \ so unreachable objects are dropped;\n- then verify: `page.get_text()` no longer contains the string,\
    \ and a\n  byte search of the saved file for the removed text returns nothing.\n  `doc.save(..., incremental=True)`\
    \ should be reserved for\n  signing/appending, never for redaction.\n"
- id: pdf-verify-03
  answer: "Never edit the user's original in place. Concretely:\n\n- Work on a copy. Read-only intent:\
    \ open the source, write to a new path\n  (for example `edited_invoice.pdf`), and leave the original\
    \ untouched.\n- Use a unique scratch/output directory per run, not a shared `/tmp`\n  location — a\
    \ stale script or output from a *different* task sitting in\n  the same directory is a real hazard.\n\
    - Do not save over the file you opened with `incremental=True`; that\n  rewrites history in a way\
    \ that cannot be undone and, as above, keeps the\n  old content in the file.\n- Before you edit, establish\
    \ the source of truth: does a template, HTML\n  page, reportlab script or spreadsheet exist? If so,\
    \ edit that and\n  regenerate rather than patching the PDF.\n- Verify before overwriting anything:\
    \ reopen the saved file, extract the\n  text, check the page count, and `unzip -t`/reopen it as an\
    \ independent\n  reader would. If the verification fails, you still have the original.\n- Preserve\
    \ the metadata you should preserve and be explicit about what you\n  changed; if you must re-save,\
    \ keep the original as `*.orig`.\n- Check the arithmetic: recompute the total from the Amount column\
    \ and\n  assert it equals the printed total before editing. A bad input table\n  should be reported,\
    \ not propagated.\n- For anything confidential or regulated, remember that redaction is not\n  shredding:\
    \ a full (non-incremental) save plus a byte-level check is the\n  minimum, and a \"sanitised\" PDF\
    \ should be treated as needing the same\n  scrutiny as the original.\n- Do not leave a half-written\
    \ or partially-applied file where the user\n  might pick it up; write to a temp name and rename on\
    \ success.\n"
- id: pdf-verify-04
  answer: "\"No exception, printed done\" only tells you the code path executed. It\nsays nothing about\
    \ whether the PDF is correct. The failure modes that\nproduce a clean run and a wrong file:\n\n- **Silent\
    \ no-ops that are not errors.** `insert_textbox` returns a\n  *negative* number and writes nothing\
    \ when the text does not fit — no\n  exception. You check nothing, so the cell is silently blank.\n\
    - **The wrong thing was written.** A raw `str.replace` on a run that has\n  been split across several\
    \ `<w:t>`-style runs finds no literal match and\n  no-ops, leaving a file byte-identical to the input\
    \ while reporting\n  success. DOCX/PDF runs are split arbitrarily — search the *parsed*\n  text, not\
    \ the bytes.\n- **You edited the wrong object.** A redaction annotation was added but\n  `apply_redactions()`\
    \ was never called, so you drew a black box and left\n  the text underneath. Or you wrote to a stale\
    \ `Page` handle after\n  `doc.new_page()` invalidated it.\n- **Byte search finds nothing even though\
    \ the text is there.** Content\n  streams are usually Flate-compressed, so a search on the raw file\
    \ (or on\n  `f.read()`) returns nothing and you conclude \"the removal worked\" when\n  you never\
    \ checked. Use `page.read_contents()` / `doc.xref_stream(xref)`\n  or, better, `get_text()`.\n- **Rendering\
    \ differs from the model.** The layout maths can be correct on\n  paper and wrong on the page: a rule\
    \ half-covered by a fill, a baseline\n  one line too high, a table that overflows the margin. Only\
    \ a rendered\n  look catches that.\n- **The output is not the file you are looking at.** Wrong path,\
    \ an\n  unsaved `Page`, an incremental save that left the old revision in place.\n- **Arithmetic is\
    \ wrong.** A row added or removed without recomputing the\n  Total: the script is green and the invoice\
    \ is wrong.\n- **The edit landed on a shared object.** Changing an image xref changed\n  it on all\
    \ 20 pages that share it; a single-page check looks fine.\n\nThe fix is to make the script assert\
    \ on the *result*: re-open the saved\nfile, extract the text, assert the new value is present and\
    \ the old one is\nabsent, assert the recomputed total equals the printed total, check the\nreturn\
    \ value of every `insert_textbox` for `< 0`, confirm the zip/stream\nintegrity, and render the page\
    \ and look at it. Print the diff you\nactually verified, not the string \"done\".\n"
