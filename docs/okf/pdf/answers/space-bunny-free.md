- id: pdf-model-01
  answer: "A PDF has no notion of a table, a row, or a cell. It is a page-description\nformat: each page\
    \ holds a list of content streams containing drawing\noperators (text-showing ops `Tj`/`TJ` with positioned\
    \ glyphs, path operators\n`m`/`l`/`re`/`S`/`f`, colour ops, clipping) plus references to fonts, images\n\
    and graphics state, all in a 1/72-inch user space. There is no DOM, no\nstructure tree, no cell objects\
    \ — a \"table\" is just a visual arrangement of\nglyph runs and stroked/filled rectangles, often produced\
    \ by a layout engine\nand then flattened into coordinates.\n\nImplications for the edit:\n- There\
    \ is no \"third row\" to select. The model cannot address it; you must\n  locate it geometrically\
    \ (find the horizontal rules / text lines yourself).\n- Removing it is not deletion: nothing marks\
    \ the row as a discrete object.\n  You must either (a) overpaint/cover the region and redraw everything\n\
    \  below it shifted up, or (b) rewrite the content stream, which is fragile\n  because of re-use of\
    \ graphics state, XObjects, and shared operator data.\n- Any content below the row must be redrawn,\
    \ since the layout is absolute\n  — coordinates are fixed, so the page does not reflow. Real page\
    \ reflow in\n  an existing fixed-layout PDF is essentially impossible; true reflow means\n  re-generating\
    \ the page (e.g. re-render from source, or rebuild the table).\n- Therefore the practical PyMuPDF\
    \ workflow is geometry-based:\n  detect rules/text to get the row's bbox, erase/cover that rect, shift\
    \ the\n  content below up (copy the region as a new image or clip-and-blit), and\n  redraw the rules\
    \ and text. Robust re-typesetting requires a layout engine,\n  not a stream patch.\n"
- id: pdf-model-02
  answer: "Drawing an opaque white rectangle and typing new text on top is\n*overpainting*, not editing.\
    \ The original text objects remain fully intact\nunderneath: still selectable, still searchable, still\
    \ copyable, still\npresent in extracted text, still rendered by any consumer that honours the\noptional-content\
    \ or, more importantly, still there if the white fill is\nremoved. It also rasterises-on-look, leaves\
    \ the wrong colour if the page has\na non-white background or transparency, breaks text extraction\
    \ and any\ndownstream PDF search/index/accessibility or audit check, and is trivially\ndefeated by\
    \ a reader that ignores or reorders the overlay.\n\nUse a real edit path instead:\n- `page.add_redact_annot(rect,\
    \ fill=None)` followed by\n  `page.apply_redactions()` — PyMuPDF physically removes the underlying\n\
    \  text/glyph objects that overlap the rect and draws the fill, so the text is\n  gone from the content\
    \ stream and from extraction. Use `fill=(1,1,1)` only\n  if you need to paint over non-text artwork\
    \ or table shading.\n- For wholesale restructuring (deleting a row, adding a column, reflowing):\n\
    \  re-render the page region from a source model — e.g. re-generate the PDF\n  with a layout library\
    \ (reportlab, WeasyPrint) and replace the page, or\n  rebuild the table on a new page — rather than\
    \ compositing pixels.\nIn short: if the requirement is that the old text no longer exists, use\nredaction\
    \ (which deletes objects) or regenerate the document; white\nrectangles only hide text and leave it\
    \ recoverable.\n"
- id: pdf-model-03
  answer: "Text in a content stream is not stored as the source string. It is\npost-processed for the\
    \ specific font's encoding and subsetting:\n- Each character is mapped to one or more glyph CIDs.\
    \ For a Type0/CID font\n  the string is a sequence of 2-byte CIDs; for a simple font it is a\n  single-byte\
    \ code. \"22.50\" with a subsetted font will not appear as the\n  ASCII bytes `32 32 2E 35 30`.\n\
    - The bytes are usually split across several `Tj`/`TJ` show operators, with\n  kerning adjustments\
    \ inserted as TJ numbers, e.g.\n  `[(2) -20 (2.5) 30 (0)] TJ` or `2 2 . 5 0` inside a hex string\n\
    \  `<002A002E0035>` . A contiguous ASCII search for `22.50` will not match\n  even when the glyphs\
    \ are visually adjacent and correctly ordered.\n- Whitespace between characters may be a TJ displacement,\
    \ not a byte, and\n  some characters may be rendered by a different font resource entirely\n  (e.g.\
    \ a bold cell in one font, digits in another).\n- Font encodings may be a custom/standard encoding\
    \ or a Differences array,\n  so even the correct bytes are not the original characters.\n\nWhy it\
    \ corrupts: you are editing bytes inside a string that has a length\nprefix/consumption semantics\
    \ inside an operator, and any change in byte\ncount or glyph index shifts the glyph stream. A same-length\
    \ substitution can\nbe fine; a different-length replacement of \"22.50\" (5 chars) to \"54.00\"\n\
    (5 chars) is lucky, but if your replacement is longer or you match bytes\nthat are shared with other\
    \ text (e.g. the `2` used elsewhere, or digits\ninside a hex string), you can:\n  - change the wrong\
    \ occurrence (the same code may appear in many words),\n  - leave the byte count inconsistent so subsequent\
    \ glyphs in the same\n    `TJ` are misread, or produce glyph indices that do not exist in the\n  \
    \  subset -> a garbled or missing glyph, or a font error,\n  - break the stream syntax if your replacement\
    \ contains `)`, `(`, `>` or\n    whitespace inside a literal/hex string.\n\nSafer approaches: use\
    \ a library that understands the text layer (PyMuPDF\n`page.add_redact_annot` + `apply_redactions`\
    \ to delete, then\n`insert_text`/`insert_textbox` to redraw with a matched font), or decode\nglyph\
    \ IDs back to Unicode and rebuild the string properly with correct\noperator and byte-count handling,\
    \ and patch only the exact intended span.\n"
- id: pdf-model-04
  answer: "PyMuPDF presents a top-left-origin, y-down coordinate system (like a\nscreen/matplotlib), measured\
    \ in points (1/72 inch), for its Python API\n(`page.rect`, `page.get_text`, `insert_text`, `draw_rect`,\
    \ `add_redact_annot`).\nRaw PDF user space is bottom-left origin, y-up. PyMuPDF converts internally:\n\
    `py_y = page_rect.height - pdf_y` (for an unrotated page whose\n`page.rect` height equals the MediaBox\
    \ height), and it is a uniform flip —\nno scaling unless you choose a different unit system.\n\nWhat\
    \ changes:\n- `/Rotate`: the page's `page.rect` (and `.mediabox`, `.cropbox`,\n  `.rotation`) is reported\
    \ in the *rotated* (displayed) space, so\n  `get_text(\"words\")` bboxes are already in displayed\
    \ coordinates. Raw stream\n  coordinates are not, and if you were mixing them with a rotated page\
    \ the\n  flip origin is the rotated height, so a naive `height - y` will be wrong by\n  the swap of\
    \ width/height. Use only `page.rect`-relative coordinates and\n  let PyMuPDF handle the transform.\
    \ If you must write raw content-stream\n  operators, you have to apply the full page transform yourself:\n\
    \  `cm = page.rotation_matrix * page.cropbox_matrix` (or\n  `page.derotation_matrix` to go back),\
    \ and remember the base coordinate\n  system is the CropBox origin, not (0,0).\n- CropBox != MediaBox\
    \ (or non-zero origin): the \"default user space\"\n  origin is the **CropBox** lower-left corner,\
    \ and PDF y-up is measured from\n  the bottom edge of the *MediaBox*, not the CropBox. PyMuPDF normalises\
    \ this:\n  word bboxes come back relative to `page.rect` (the crop/visible area) with\n  y down from\
    \ its top-left. The scale is still 1:1 in points unless you\n  use a different unit (e.g. `Matrix(2,2)`),\
    \ so coordinates need translating\n  by the CropBox offset `(x0, y0)` in PDF space:\n  `x_pdf = x_py\
    \ + cropbox.x0`, `y_pdf = cropbox.y0 + page.rect.height - y_py`\n  (ignoring rotation, where the crop\
    \ matrix gives the transform).\n- Consequence for drawing: a word bbox from `get_text(\"words\")`\
    \ can be fed\n  directly to `page.draw_rect(bbox)` and `page.insert_textbox(bbox, ...)`\n  because\
    \ it is in the same PyMuPDF display space; only raw-stream editing\n  needs the conversion. Also note\
    \ `page.rect` may be non-zero-origin itself\n  (PyMuPDF typically reports a rect whose origin is the\
    \ CropBox top-left),\n  so prefer using `page.rect.height` and `page.rect.y0` rather than assuming\n\
    \  0/0.\n"
- id: pdf-locate-01
  answer: "Programmatic table detection on an existing PDF page, in rough order of\nreliability:\n\n1.\
    \ **Get the drawings (rules/shading) and text words first.**\n   - `page.get_drawings()` returns each\
    \ vector path with `.rect`,\n     `.type` (\"s\" stroke, \"f\" fill, \"fs\" fill+stroke), `.color`,\n\
    \     `.fill`, `.items` (the `l`/`re`/`c`/`qu` segments), `.width`, and\n     `.closePath`. Filter\
    \ for thin stroked rectangles/lines:\n     `d[\"rect\"].width > some_min and d[\"rect\"].height >\
    \ some_min and\n     d[\"type\"] in (\"s\",\"fs\")` → candidate horizontal/vertical rules.\n   - `page.get_text(\"\
    words\")` (or `page.get_text(\"dict\")`/`\"rawdict\"`)\n     returns words with `(x0,y0,x1,y1,word,block_no,line_no,word_no)`.\n\
    \   - Raster alternative: `page.get_pixmap(dpi=200)` then detect long\n     straight runs of dark\
    \ pixels (Hough / run-length) — robust to tables\n     drawn as images, but coordinates need scaling\
    \ back to points\n     (`x_pdf = x_px * 72/dpi`).\n\n2. **Cluster rules into a grid.**\n   - Horizontal\
    \ rules: keep drawings whose `rect.height <= ~2` and whose\n     `rect.width` is a large fraction\
    \ of the page width; collect their\n   `y` (use `rect.y0`). Sort and de-duplicate → list of `y_h`\
    \ values.\n   - Vertical rules: keep drawings whose `rect.width <= ~2` and\n     `height > threshold`;\
    \ collect `x` values → `x_v`.\n   - A ruled table is then the Cartesian product: rows are the horizontal\n\
    \   bands between consecutive `y_h`; columns are the vertical bands between\n   consecutive `x_v`.\
    \ Row bbox = `(min(x_v), y_h[i], max(x_v), y_h[i+1])`;\n   cell bbox = the intersection with each\
    \ column band.\n   - For borderless/ruled-only-one-axis tables, snap cells to the column\n   bands\
    \ by x-centre membership and set the row band from the text baselines\n   plus padding.\n\n3. **Snap\
    \ to the text.**\n   - Assign each word to the row band whose `y` range contains the word's\n    \
    \ vertical midpoint, and to the column band containing its\n     horizontal midpoint. This validates\
    \ the geometry and gives you the\n     content per cell. Check that the number of assigned columns\
    \ matches the\n     number of `x_v` bands; if not, the table may be a merged-cell layout.\n\n4. **Read\
    \ direction / rotation.** If `page.rotation` or the text order is\n   odd, de-rotate first (`pagederotation`\
    \ or transform the drawings) and\n   work in the display space that `page.rect` gives you. Do not\
    \ assume row\n   order equals increasing y; sort by `y0` yourself.\n\n5. **Better tooling for the\
    \ same job.**\n   - `page.find_tables()` (PyMuPDF ≥ 1.23) — a convenience wrapper that\n     returns\
    \ `TableFinder`/`Table` objects with `.bbox`, `.row_count`,\n     `.row.bbox`, `.cells`, and `rows`/`extract()`;\
    \ the underlying strategy\n     uses both the drawing lines and the text layout, and it also handles\n\
    \     ruled, borderless, and rotated tables. This is normally the first thing\n     to try; use the\
    \ manual drawing-based method when its heuristics fail\n     (e.g. heavy shading, missing rules, nested\
    \ tables).\n   - `camelot` (lattice/stream modes) and `pdfplumber`\n     (`page.find_tables()`, `page.extract_table()`,\
    \ `.lines`, `.rects`,\n     `.edges`) are dedicated alternatives that return precise bboxes; both\n\
    \     are built on the same idea of harvesting `page.lines/rects/curves`.\n\nAssumptions: the table\
    \ is drawn as vector strokes or separated text\nglyphs (not a flattened image); no non-uniform scaling\
    \ was applied to the\npage (a `ctm` from a nested XObject can be undone with the drawing's `matrix`\n\
    field, which `get_drawings()` reports per path).\n"
- id: pdf-locate-02
  answer: "`page.search_for(\"Widget C\")` returns only the rectangles of the *matched\nglyph run* — for\
    \ a multi-word phrase it returns one quad per contiguous\nspan, and it does not know anything about\
    \ the table. It will not include:\n  - the rest of the line in the other columns (a description, a\
    \ quantity, an\n    amount), because those are separate show operations at different x\n    positions\
    \ and often far from the hit;\n  - the leading text of the same cell if the cell wraps, or trailing\
    \ text\n    beyond the last matched glyph;\n  - the ruling lines, the cell's background fill/shading,\
    \ or the cell\n    padding;\n  - the vertical extent of the row, since the returned quad is tight\
    \ to the\n    glyph ink (and the glyph quad is the text bbox, not the row band).\nDeleting only that\
    \ quad therefore leaves the rest of the row visible — the\nrow looks half-erased and the column values\
    \ are still readable. Also, a\nsearch hit can be ambiguous if the same string appears in a header\
    \ or\nanother row.\n\nTo get the full row region:\n1. Find the text hit: `hits = page.search_for(\"\
    Widget C\")`; take\n   `hit = hits[0]`, giving `(x0,y0,x1,y1)` in display coordinates.\n2. Establish\
    \ the table's row and column bands from the page geometry (see\n   pdf-locate-01): horizontal rules\
    \ `y_h` and vertical rules `x_v` from\n   `page.get_drawings()` (or `page.find_tables()`).\n3. Snap\
    \ the hit to a row: find `i` with `y_h[i] <= hit.y0` and\n   `hit.y1 <= y_h[i+1]`; the row band is\
    \ `(x_left, y_h[i], x_right,\n   y_h[i+1])`. If there are no rules, use the neighbouring text lines'\n\
    \   baselines (group words by y-centre into lines) and pad by ~2–4 pt to\n   get the row band.\n4.\
    \ Intersect with the table's x extent to get the row rect:\n   `row_rect = fitz.Rect(x_left_of_table,\
    \ y_h[i], x_right_of_table,\n   y_h[i+1])`. Verify that every other word whose centre falls inside\
    \ that\n   rect is the content you intend to remove (e.g. check\n   `page.get_text(\"words\", clip=row_rect)`),\
    \ which is the reliable way to\n   confirm the row boundary is right.\n5. Use that row rect for the\
    \ redaction annot; optionally add a small\n   vertical inset (0.5–1 pt) so you catch descenders/ascenders\
    \ of the row's\n   own text without touching the adjacent rule lines.\n"
- id: pdf-locate-03
  answer: "PyMuPDF gives you both the geometry and the style of existing text. Two\nlayers:\n\n1. **Span-level\
    \ style (the reliable way).** `page.get_text(\"dict\")` gives\n   blocks → lines → spans, each span\
    \ carrying:\n   - `\"font\"` (e.g. \"Helvetica-Bold\", \"Arial,Bold\", or a subset name)\n   - `\"\
    size\"` (float, effective font size in points)\n   - `\"flags\"` (bit 1 superscript, 2 italic, 4 serif,\
    \ 8 monospaced, 16 bold)\n   - `\"color\"` (sRGB integer 0xRRGGBB) and `\"alpha\"`\n   - `\"bbox\"\
    `, `\"origin\"` (the **baseline start point** — exactly what you\n     need for `insert_text`), `\"\
    ascender\"`, `\"descender\"`\n   `page.get_text(\"rawdict\")` goes further and gives per-character\
    \ bboxes\n   and origins if you need one glyph at a time.\n   So the recipe is: locate the span/word\
    \ you want, read `span[\"font\"]`,\n   `span[\"size\"]`, `span[\"color\"]` → `(\"#%06x\" % span[\"\
    color\"])`, and\n   `span[\"origin\"]` for the baseline.\n\n2. **Rendering flags on the page.** `page.get_fonts(full=True)`\
    \ lists the\n   font resources on the page (xref, name, type/basefont/encoding) so you\n   can map\
    \ `\"font\"` to a real font name and pick a file for\n   `insert_font`. `page.get_texttrace()` (newer\
    \ PyMuPDF) returns per-span\n   glyph data with the font id, size, and the raw colour/space, which\
    \ is\n   useful when spans mix fonts.\n\n3. **Using it to redraw.** Insert the font explicitly and\
    \ reuse the values:\n   `page.insert_font(fontname=\"F1\", fontfile=\"/path/DejaVuSans.ttf\")`\n \
    \  (or `fontname=\"helv\"` for a built-in Base-14), then\n   `page.insert_text(origin, new_text, fontname=\"\
    F1\",\n   fontsize=span[\"size\"], color=hex_to_rgb(span[\"color\"]))` — `origin` must\n   be a **baseline**\
    \ point, so use `span[\"origin\"]` (adjusted by the\n   delta-x you want). For italic/bold, match\
    \ `flags` (or use the exact\n   font file/Base-14 name: `\"helv\"`/`\"hebo\"`/`\"heit\"`/`\"hebi\"\
    `,\n   `\"tiro\"`/`\"tibo\"`/…).\n\n4. **Colour subtlety.** `span[\"color\"]` is the fill colour in\
    \ sRGB;\n   `get_drawings()` colour is a float tuple 0–1. If the text is rendered in\n   a different\
    \ colour space (DeviceCMYK, ICC, or a Separation), the\n   reported sRGB may not round-trip exactly\
    \ — an approximation is usually\n   acceptable, and for exactness extract the raw operands with\n\
    \   `page.get_texttrace()` or read the content stream.\n"
- id: pdf-locate-04
  answer: "From the same page geometry, in two complementary places.\n\n**Column boundaries (the vertical\
    \ rules / cell edges).** Ruled tables give\nthem directly from the vector art:\n  `for d in page.get_drawings():`\n\
    \  `    if d[\"type\"] in (\"s\",\"fs\") and d[\"rect\"].width <= 2 and d[\"rect\"].height > 5:`\n\
    \  `        xs.append(d[\"rect\"].x0)`  # and x1\nKeep the unique sorted x positions — those are the\
    \ column separators. If\nthe table has no vertical rules, derive columns by clustering the word\n\
    bboxes' left edges / right edges into columns (e.g. 1-D clustering with a\ngap threshold), or from\
    \ the cell bboxes that `page.find_tables()` /\n`pdfplumber` already computed.\n\n**Right-aligned numbers\
    \ (where the value ends).** Alignment is a property\nof the *text*, not the geometry, so read it from\
    \ the text layer: the right\nedge of a right-aligned number is simply the `x1` of the last word/char\
    \ in\nthe cell, and the decimal points of a column will line up. So:\n  - For each data row, take\
    \ the cell's words\n    (`page.get_text(\"words\", clip=cell_rect)`) and compute\n    `right_x = max(w[2]\
    \ for w in words)`.\n  - Fit a single anchor for the whole column: collect `right_x` for the\n   \
    \ numeric column across all data rows and either take the median, or (more\n    robustly) right-align\
    \ to the column's right boundary = `max(xs)` (the\n    last vertical rule) minus a padding of 2–4\
    \ pt.\n  - For a decimal-aligned column, align on the decimal point: find the\n    bounding box of\
    \ the `.` glyphs (`page.get_text(\"rawdict\")` gives\n    per-char bboxes) and use that x as the anchor;\
    \ that reproduces\n    accounting-style alignment.\n  - For cell centre alignment, the anchor is `(cell_x0\
    \ + cell_x1)/2` with\n    the text centred, or the digit count + your own `font.text_length(s, fontsize)`\n\
    \    to compute the start x for a given end x:\n    `x_start = right_x - fitz.get_text_length(text,\
    \ fontname=\"helv\", fontsize=size)`\n    (or `fitz.Font(\"helv\").text_length(...)`), which is the\
    \ reliable way to\n    place text of a different length so it still ends at the same x.\n\n`page.find_tables()`\
    \ short-circuits this: each `Table` exposes `.bbox` and\nper-cell `cell.bbox`/`cell.x0,x1,y0,y1`,\
    \ so the column x-ranges and the\nright edge of each column are immediately available; `pdfplumber`\
    \ gives\n`table.columns[i][\"x0\"/\"x1\"]` and per-word `x1` values the same way. Either\nway the\
    \ alignment anchor comes from the existing glyphs, not from the page\nsetup — remember to leave ~1–2\
    \ pt of right padding inside the cell.\n"
- id: pdf-rowdel-01
  answer: "A correct, practical PyMuPDF procedure for deleting a ruled, shaded table\nrow (rows below\
    \ move up, Total recomputed):\n\n**0. Decide the scope.** Because a PDF cannot reflow, \"moving rows\
    \ up\"\nmeans re-compositing: you will blank the removed band and redraw the band\nof content below\
    \ it at a smaller y, or you will rebuild the table on a new\npage. The clip-blit approach (step 6)\
    \ is the way to get \"preserve exact\nappearance\".\n\n**1. Locate the table and the target row.**\n\
    \   - `page.find_tables()` → `tab = page.find_tables()[0]`, `y_h` from\n     `tab.rows[i].bbox[1]`\
    \ / `tab.rows[i+1].bbox[1]`, or harvest rules from\n     `page.get_drawings()`.\n   - `target = tab.rows[k]`;\
    \ `row_y0, row_y1 = target.bbox[1], target.bbox[3]`;\n     `x0, x1 = target.bbox[0], target.bbox[2]`\
    \ (extend to the table width).\n   - Verify the row's text (`page.get_text(\"clip=target.bbox\")`)\
    \ is the row\n     you expect before deleting.\n\n**2. Measure the content block that must move up.**\n\
    \   `h_del = row_y1 - row_y0`\n   `below = fitz.Rect(x0, row_y1, x1, page_bottom_of_content)` — the\n\
    \   region containing all remaining rows plus the Total row and any totals,\n   footnotes, or rule\
    \ lines beneath.\n   Optionally confirm nothing essential (e.g. a page number or an unrelated\n  \
    \ graphic) lives in `below` that must *not* move.\n\n**3. Snapshot the region as an image (preserves\
    \ appearance exactly).**\n   `clip_below = fitz.Rect(x0, row_y1, x1, page.rect.y1)`\n   `pix = page.get_pixmap(clip=clip_below,\
    \ dpi=300)`\n   Save it (to memory / a temp file) for step 6. Higher DPI keeps numbers\n   crisp when\
    \ re-blitted. If DPI < 300, prefer a vector-preserving method\n   (see pdf-rowdel-03) for the text.\n\
    \n**4. Erase the deleted row's band (and the vacated space at the bottom).**\n   `page.add_redact_annot(fitz.Rect(x0,\
    \ row_y0, x1, page.rect.y1),\n   fill=(1,1,1))` — redact from the top of the row all the way to the\
    \ bottom\n   of the page so *both* the row and the now-empty space at the bottom are\n   cleared of\
    \ the old content, then\n   `page.apply_redactions(images=fitz.PDF_REDACT_IMAGE_NONE, graphics=fitz.PDF_REDACT_LINE_ART_NONE)`\n\
    \   so you only remove the text and keep the rules/shading for the area you\n   intend to re-fill.\n\
    \   - `PDF_REDACT_IMAGE_NONE` leaves images alone; `PDF_REDACT_LINE_ART_NONE`\n     leaves vector\
    \ art (rules) alone. (Beware: text is removed if its bbox\n     *overlaps* the rect, so keep the rect\
    \ tight and verify.)\n\n**5. Redraw the background of the vacated band.**\n   Ruled/shaded tables\
    \ need the row shading re-established, otherwise you\n   get a white strip. Re-create it: for alternating\
    \ shading use the row's\n   fill colour sampled from the original (`d[\"fill\"]` of the row's rectangle\n\
    \   in `get_drawings()`), e.g.\n   `page.draw_rect(fitz.Rect(x0, y, x1, y+row_h), color=None, fill=(0.93,0.93,0.95))`\n\
    \   for the band now occupied by the first shifted row. Do this for each\n   band you re-fill.\n\n\
    **6. Blit the region below, shifted up by `h_del`.**\n   `page.insert_image(fitz.Rect(x0, row_y0,\
    \ x1, row_y0 + clip_below.height),\n   pixmap=pix, keep_proportion=False)`\n   (position it so the\
    \ first remaining row now starts at `row_y0`).\n   This re-paints rules, shading, glyphs and borders\
    \ exactly as they were,\n   one row-height higher.\n\n**7. Redraw the rules so the grid stays continuous.**\n\
    \   Vector art in the redacted band was preserved, but the *position* of the\n   lines must be re-emitted.\
    \ Simplest robust option: after the blit, draw\n   the horizontal rules for the whole table from the\
    \ original `y_h` list\n   minus the appropriate offsets (all rules below the deleted row shift up\n\
    \   by `h_del`), and the vertical rules spanning the table:\n   `for y in shifted_y_h: page.draw_line(fitz.Point(x0,y),\
    \ fitz.Point(x1,y), width=0.5)`\n   `for xv in x_v: page.draw_line(fitz.Point(xv, table_top), fitz.Point(xv,\
    \ table_bottom), width=0.5)`\n   (Alternatively keep them in the blitted image and don't redraw —\
    \ but then\n   the band's outer top/bottom borders may not line up; redrawing from the\n   measured\
    \ `y_h` list is deterministic.)\n\n**8. Update the Total.** The totals row moved with the blit, so\
    \ its value is\n   now in the wrong place textually? No — it is correct *positionally* but\n   its\
    \ *value* is unchanged. Recompute it from the data: read the surviving\n   numeric cells (`page.get_text(\"\
    words\", clip=cell_rect)` per cell, or\n   better, parse from the source data you used to build the\
    \ table), sum the\n   quantity × unit-price column, then redact the old total cell and insert\n  \
    \ the new one with the matched style from pdf-locate-03\n   (`insert_text` with the span's font/size/color\
    \ at the column's right\n   anchor, right-aligned via `fitz.get_text_length`).\n   If the totals row\
    \ has a shaded fill that is now in the wrong position,\n   refill the band as in step 5.\n\n**9. Clean\
    \ up and verify.**\n   - `page.clean_contents()` (optionally `subtract_fonts=True`) to drop\n    \
    \ orphaned/unused objects and shrink the file.\n   - Verify: `page.get_text()` no longer contains\
    \ the deleted row's text;\n     the surviving rows read in the expected order; the number of table\
    \ rows\n     decreased by one; the Total equals the recomputed sum; visually render\n     with `page.get_pixmap(dpi=150)`\
    \ and eyeball, and check\n     `page.find_tables()` still reports one table with the expected\n  \
    \   `row_count`.\n   - Save with garbage collection/deflate:\n     `doc.save(out, garbage=4, deflate=True,\
    \ clean=True)`; or\n     `doc.bake()` first if you need to resolve indirect objects.\n\nAssumptions:\
    \ single page table, uniform row heights, the deleted row's text\nis fully covered by the redaction\
    \ rect, and \"Total must be updated\" is\ncomputed from values you have access to (the source data)\
    \ rather than\nre-derived by OCR of the remaining cells. A cleaner alternative for\n\"re-typeset the\
    \ whole table correctly\" is to rebuild the table with\n`insert_textbox`/vector rules from scratch\
    \ and replace the page — but the\nblit procedure above preserves the original appearance exactly.\n"
- id: pdf-rowdel-02
  answer: "`page.apply_redactions()`'s defaults are aggressive about *artwork*:\n- `images=fitz.PDF_REDACT_IMAGE_PIXELS`\
    \ (the default) — any image whose\n  bbox falls inside / intersects the redaction rectangle is **removed**,\
    \ and\n  for partial overlaps the surviving pixels are **blacked out** (the\n  default removes the\
    \ whole image object if it is fully covered, and\n  redacts the covered pixels if only partly). `PDF_REDACT_IMAGE_NONE`\
    \ keeps\n  images; `PDF_REDACT_IMAGE_REMOVE_UNLESS_INVISIBLE` removes only\n  fully-covered images.\n\
    - `graphics=fitz.PDF_REDACT_LINE_ART_REMOVE_IF_TOUCHED` (the default) —\n  **vector line art is deleted**\
    \ if it touches/overlaps the rect\n  (`PDF_REDACT_LINE_ART_NONE` keeps all vector art). Stroked/filled\
    \ shapes,\n  including table rules, cell backgrounds, borders, and fills, count as\n  line art.\n\
    - `text=fitz.PDF_REDACT_TEXT_REMOVE` (the default) — characters whose bbox\n  overlaps the rect are\
    \ removed; `PDF_REDACT_TEXT_NONE` keeps text.\n- `add_text=fitz.TEXT_PRESERVE_WHITESPACE` is the default\
    \ fill (no new text\n  written), and `fill` defaults to white, so redacting over a shaded cell\n \
    \ without specifying `fill=None` (or the cell's real colour) paints a white\n  patch.\n\nWhy it matters\
    \ for a table row/cell:\n- Redacting a row band with the default `graphics` value **deletes the\n\
    \  horizontal rules and the vertical cell borders** that pass through the\n  rect — you lose the grid,\
    \ so the table looks broken even if the text is\n  gone.\n- Any logo, signature, scanned stamp, checkbox\
    \ image, or a rasterised\n  header image inside the rect is deleted or blacked out.\n- A shaded (filled)\
    \ cell background drawn as vector art is removed, so the\n  row's grey band disappears (leaving a\
    \ white hole) if you don't redraw it.\n- Conversely, keeping art with `PDF_REDACT_LINE_ART_NONE` means\
    \ the old\n  rules stay in place at their old positions — which is exactly what you\n  want if you\
    \ are going to redraw the grid at shifted positions (see\n  pdf-rowdel-01 step 4), and harmful if\
    \ you expected the rules to be\n  removed with the row.\nRecommendation: for a text-only cell edit\
    \ use\n`apply_redactions(images=PDF_REDACT_IMAGE_NONE, graphics=PDF_REDACT_LINE_ART_NONE)`\nand specify\
    \ `fill` to match the cell background (or `fill=None` plus your own\n`draw_rect`); for a full row\
    \ deletion where you also want the rules gone,\nuse the default and then redraw the surrounding grid.\n"
- id: pdf-rowdel-03
  answer: "Yes — there are two ways, depending on whether you need true vector/text\nfidelity or a raster\
    \ is acceptable.\n\n**A. Raster clip-and-blit (one method, any content, preserves appearance\nexactly,\
    \ but is a picture).**\n```\nsrc  = fitz.Rect(x0, row_y1, x1, y0_target_bottom)   # the block to move\n\
    dest = fitz.Rect(x0, row_y0, x0 + src.width, row_y0 + src.height)\npix  = page.get_pixmap(clip=src,\
    \ dpi=300)             # snapshot\npage.add_redact_annot(fitz.Rect(x0, row_y0, x1, page.rect.y1),\
    \ fill=(1,1,1))\npage.apply_redactions(images=fitz.PDF_REDACT_IMAGE_NONE,\n                      graphics=fitz.PDF_REDACT_LINE_ART_NONE)\n\
    page.insert_image(dest, pixmap=pix, keep_proportion=False)\n```\nThis moves rules, shading, borders,\
    \ text and images in one shot with\nidentical appearance (modulo rasterisation at your chosen DPI).\
    \ Use\n300–600 dpi for numbers to stay sharp.\n\n**B. Vector-preserving \"insert_textbox of a copy\"\
    \ is not a thing** — PyMuPDF\nhas no built-in \"move a region\" that re-emits the original operators,\
    \ so if\nyou must keep text as text (selectable/searchable) and you cannot use\n`show_pdf_page`, you\
    \ have to re-typeset the moved block with\n`insert_text` per span using the styles from pdf-locate-03\n\
    (font/size/color/baseline from `get_text(\"dict\")`), and re-emit the vector\nrules with `draw_line`/`draw_rect`\
    \ at the shifted coordinates. The image\nblit above avoids that work and is the usual answer.\n\n\
    **C. `Page.show_pdf_page()` — move whole *other pages*, or an xref region,\nwithout rasterising.**\n\
    ```\n# draw a source page (or a clipped region) at a chosen position on this page\npage.show_pdf_page(dest_rect,\
    \ src_doc, pno,\n                   clip=src_rect, keep_proportion=False)\n```\nUseful when the block\
    \ you want to move lives on its own page, or when you\nhave rebuilt the moved block as a separate\
    \ Form XObject. It stays vector.\n\n**D. XObject reuse.** If the \"block\" is a Form XObject or a\
    \ repeated table\nbody, `doc.xref_get_key` / `page.get_xobjects()` shows the xrefs, and you can\n\
    place the same XObject twice with different transforms\n(`page.show_pdf_page` with the xref via `doc.xref_object`/`get_page_xobjects`)\n\
    — again vector, but only if the block is genuinely a reusable object.\n\nSo: the direct answer is\
    \ the clip → `get_pixmap` → `insert_image` blit for a\nsingle page (exact appearance, raster), and\
    \ `show_pdf_page` when you need a\nvector move (e.g. relocating a block between pages).\n"
- id: pdf-rowdel-04
  answer: "Beyond the row itself, an invoice table almost always implies a cascade of\nother changes:\n\
    \n- **Totals:** the Total row must be recomputed (line subtotal, tax/VAT/SST at\n  a given rate, discount,\
    \ shipping, grand total, amount due, balance due).\n  These may live in the Total row and/or in a\
    \ separate summary block to the\n  right or below, and any \"Amount Due\" / \"Balance Forward\" figure\
    \ must agree\n  with the new grand total.\n- **Sequential identifiers:** if invoice/table rows are\
    \ numbered (No. 1..n),\n  the sequence must be renumbered after the deletion so there is no gap.\n\
    - **Item cross-references:** quantities, unit-of-measure columns, and\n  per-row totals must stay\
    \ consistent with the master data; a removed\n  deliverable may also appear in a separate description,\
    \ terms, or scope\n  block that must be edited.\n- **Repeated/periodic occurrences:** the same line\
    \ item may appear again in a\n  \"recurring\", \"history\", \"previous invoice\", or continuation\
    \ table on the\n  next page, which needs the same deletion, or a cross-reference like\n  \"see p.2\
    \ line 4\" that must be updated.\n- **Layout that must move:** every element below the deleted row\
    \ — the\n  Total row, subtotal/summary block, footer note (\"Subtotal excludes\n  shipping\"), bank\
    \ details, terms & conditions, page number, signature\n  block, stamp — must shift up by exactly the\
    \ deleted row's height, and any\n  table rules/box borders must be re-issued to keep the grid closed.\n\
    - **Value-added / tax metadata:** taxable amount, tax rate applied per\n  line, tax-inclusive flags,\
    \ currency rounding and a \"rounding adjustment\"\n  line if the penny total no longer matches the\
    \ sum of lines.\n- **Metadata and consistency fields:** document properties, the PDF\n  /Metadata\
    \ title or XMP, bookmarks/outline entries pointing at the removed\n  row, link annotations and named\
    \ destinations anchored in the removed\n  region (these should be deleted along with it, and any cross-reference\n\
    \  links re-targeted), form field values (if the table is an AcroForm with\n  calculated totals fields,\
    \ those need recomputation), and attachments.\n- **Sign-off integrity:** page \"Page X of Y\" text\
    \ if the change alters page\n  count, and any approval initials/handwritten marks that referenced\
    \ the\n  removed row.\nIn practice the safe workflow is: extract the table data programmatically,\n\
    recompute totals in your own code, decide the new layout, then either rebuild\nthe table region or\
    \ do the clip-blit + redraw, and finally re-verify totals\nand cross-references by text extraction\
    \ (`page.get_text()` should show no\nstale numbers, no gaps in the numbering, and a Total matching\
    \ the sum).\n"
- id: pdf-cell-01
  answer: "Steps to replace a right-aligned number in a table cell so it looks native:\n\n**1. Locate\
    \ the cell precisely.** Get the cell bbox from the table\ngeometry — `page.find_tables()` (`tab.find_tables()`\
    \ → `tab.rows[k].cells[j]`\n→ `cell.bbox`) or from the rules in `page.get_drawings()` (intersect the\
    \ row\nband with the column band). Never rely on `search_for(\"22.50\")` alone; use it\nonly as a\
    \ hint.\n\n**2. Read the original style.** From the span covering the old number:\n```\nd = page.get_text(\"\
    dict\", clip=cell_rect)\nspan = d[\"blocks\"][0][\"lines\"][0][\"spans\"][0]\nfont, size, color, baseline\
    \ = span[\"font\"], span[\"size\"], span[\"color\"], span[\"origin\"]\n```\nKeep `font`, `size`, `color`\
    \ (as `(\"#%06x\" % color)`), and note the\n`flags` for bold/italic. Optionally `page.get_texttrace()`\
    \ to confirm.\n\n**3. Compute the insertion anchor (right-aligned).** Right-align to the\nright edge\
    \ of the old number (or to the column's right boundary minus\npadding), then convert the *width* of\
    \ the new text back into a start x:\n```\nright_x  = cell_rect.x1 - 3          # or max(w[2] for w\
    \ in page.get_text(\"words\", clip=cell_rect))\nfont_obj = fitz.Font(\"helv\")           # or fitz.Font(fontfile=...)\
    \ matching span[\"font\"]\nw_new    = font_obj.text_length(\"54.00\", fontsize=size)\nbaseline = baseline.y\
    \                 # same baseline as the original text\nstart_x  = right_x - w_new\npoint    = fitz.Point(start_x,\
    \ baseline)\n```\nKeeping the original baseline y preserves the row's vertical alignment;\nkeeping\
    \ the original right edge preserves the right alignment. (For\ndecimal-point alignment, anchor on\
    \ the `.` glyph's x from `rawdict` and add\nthe width of the fractional part.)\n\n**4. Redact the\
    \ old number only.**\n```\npage.add_redact_annot(fitz.Rect(old_bbox.x0, old_bbox.y0,\n           \
    \                     right_x + 1, old_bbox.y1), fill=None)\npage.apply_redactions(images=fitz.PDF_REDACT_IMAGE_NONE,\n\
    \                      graphics=fitz.PDF_REDACT_LINE_ART_NONE,\n                      text=fitz.PDF_REDACT_TEXT_REMOVE)\n\
    ```\nSize the rect to the *old text's* ink box (a little padding, e.g. 0.5–1 pt\neach side, and 0\
    \ on the side where the next column's rule sits), because\n`apply_redactions` deletes every character\
    \ whose bbox *overlaps* the rect —\na rect spanning the whole cell risks also removing the neighbouring\n\
    column's digits or the cell's own rules/shading (see pdf-cell-02).\n`fill=None` leaves the background\
    \ untouched (important on shaded rows);\nuse `fill=<cell colour>` if the redaction would otherwise\
    \ punch a white hole.\n\n**5. Insert the new text with the matched style.**\n```\npage.insert_font(fontname=\"\
    F1\", fontfile=\"DejaVuSans.ttf\")   # or fontname=\"hebo\" etc.\npage.insert_text(point, \"54.00\"\
    , fontname=\"F1\", fontsize=size,\n                 color=hex_to_rgb(color), render_mode=0, overlay=True)\n\
    ```\nUse the exact same face (bold/regular/italic) as `span[\"font\"]`; match\n`fontsize`; use `render_mode=0`\
    \ (fill) unless the original was stroked. If\nthe original used a subsetted font you cannot easily\
    \ reuse, load a metrically\nor visually similar file and re-derive `right_x` with *that* font's\n\
    `text_length` so the alignment still lands correctly.\n\n**6. Verify.** `page.get_text(\"words\",\
    \ clip=cell_rect)` shows only `54.00`\n(the old string is gone — confirming real deletion, not overpainting);\n\
    render `page.get_pixmap(dpi=150)` and compare glyph size/weight/position with\nthe neighbouring cells;\
    \ confirm the cell shading and rules are intact; check\nthe totals if the figure feeds them. Then\
    \ `page.clean_contents()` and\n`doc.save(out, garbage=4, deflate=True)`.\n\nSimplification: if you\
    \ can rebuild the whole number cell, `page.insert_textbox`\nwith a right-aligned baseline is fiddly\
    \ — `insert_text` with an explicitly\ncomputed `Point` is the correct tool here, because `insert_textbox`\
    \ does not\ntake a baseline point and does not guarantee right alignment.\n"
- id: pdf-cell-02
  answer: "Size the redaction rect to the **old glyphs' bounding box**, not to the\ncell's full box. Concretely:\
    \ take the span/word bbox of the text you want\ngone (`span[\"bbox\"]` from `get_text(\"dict\", clip=cell)`,\
    \ or\n`page.get_text(\"words\")` and pick the word, or\n`page.search_for(\"22.50\")` as a starting\
    \ hint) and add only a small padding,\ne.g. `±0.5–1 pt` on left/right and `±0.5 pt` vertically\n(`fitz.Rect(b.x0-0.5,\
    \ b.y0-0.5, b.x1+0.5, b.y1+0.5)`).\n\nWhy:\n- `apply_redactions` removes **every character whose bbox\
    \ overlaps the\n  rect**, so a rect covering the whole cell risks also deleting the\n  neighbouring\
    \ column's digits (cells are adjacent, only the rule line\n  between them) and any other glyph whose\
    \ box grazes the cell area.\n- Keeping the rect tight means the cell's rules, the cell background\
    \ fill\n  and shading are not intersected. If you pass `graphics` with the default\n  `PDF_REDACT_LINE_ART_REMOVE_IF_TOUCHED`,\
    \ a rect that overlaps a rule or\n  the shading rectangle will **delete the line art** (so pass\n\
    \  `graphics=fitz.PDF_REDACT_LINE_ART_NONE` and/or\n  `images=fitz.PDF_REDACT_IMAGE_NONE` to protect\
    \ the grid and any images).\n- On a tightly-packed table the rows are also close vertically; too much\n\
    \  vertical padding reaches into the row above/below. Glyph boxes already\n  include ascenders/descenders,\
    \ so a small pad suffices.\n- Use `fill=None` (or the cell's actual fill colour) so the redaction\
    \ does\n  not paint a white rectangle over a shaded row; `apply_redactions` fills\n  white by default.\n\
    - Because a text bbox from `get_text` is the *ink* box, and fonts sometimes\n  render combining marks\
    \ or decorations slightly outside it, a ~1 pt pad is\n  the safe margin; then verify with\n  `page.get_text(\"\
    words\", clip=cell_rect)` that only the intended string\n  disappeared and, visually, via `get_pixmap`,\
    \ that nothing else was harmed.\n"
- id: pdf-cell-03
  answer: "`page.insert_textbox(rect, text, fontname=..., fontsize=..., color=...,\nalign=...)` **only\
    \ writes what fits** and returns a value you must check:\n- If the text does not fit, the inserted\
    \ portion is truncated and the\n  function returns the **remaining vertical space** (a positive number)\
    \ in\n  the rect; if it cannot fit even a part, it returns that space and\n  inserts nothing. If it\
    \ fits, it returns a number ≤ 0 (the leftover space\n  below the text). It never overflows the rect,\
    \ never auto-shrinks the font,\n  and never wraps mid-word in a way that keeps the whole string (it\
    \ wraps on\n  spaces, which may be exactly what you do not want for a single number).\n- Importantly,\
    \ PyMuPDF's `insert_textbox` has no baseline argument and no\n  \"right-align and let it overflow\"\
    \ mode; `align=fitz.TEXT_ALIGN_RIGHT`\n  right-aligns each wrapped line inside the rect, which is\
    \ right for a\n  column but still won't let text exceed the cell width.\n\nYour options when the new\
    \ cell text is longer than the column is wide:\n1. **Use `insert_text` with a computed point (right-aligned,\
    \ may extend left\n   beyond the cell boundary if needed).** Compute the width with\n   `fitz.Font(fontname).text_length(text,\
    \ fontsize=size)` and set\n   `x = right_anchor - width`, `y = original_baseline`. This is the correct\n\
    \   default for numbers: alignment is preserved by geometry, and if it\n   really is wider than the\
    \ cell you can decide to let it overhang\n   slightly or to re-measure the column.\n2. **Widen the\
    \ column / the cell rect** (adjust the table layout): redraw\n   the vertical rules at new x positions,\
    \ or re-typeset the table with a\n   wider column, or reduce the right padding.\n3. **Shrink the font**\
    \ until it fits:\n   `while font.text_length(text, fontsize=size) > cell.width and size > 6:\n   size\
    \ -= 0.25`, then insert with that size. Acceptable only if the\n   difference is small — a visibly\
    \ smaller number in one cell looks wrong\n   in a native-looking table.\n4. **Compress spacing** (thin-space\
    \ / hair-space) or use a condensed face\n   for that cell, or abbreviate the value (e.g. \"1,234.5\"\
    \ instead of\n   \"1,234.50\") — keep the number's meaning intact for a financial document.\n5. **Use\
    \ `insert_textbox` deliberately** only for multi-line/wrapping text\n   (a description column): give\
    \ a rect that is the *row height × width*,\n   pass `fontsize` small enough to fit, and **check the\
    \ return value** —\n   if it is > 0 the text did not fit, so either enlarge the rect, reduce the\n\
    \   font, or shorten the text. Do not ignore the return code.\n6. **Replace the whole table** (regenerate\
    \ the page) if the new content\n   genuinely does not fit the original column width — the honest fix\
    \ when\n   the layout itself must change.\nIn all cases, after inserting, verify with\n`page.get_text(\"\
    words\", clip=cell_rect)` and a rendered pixmap that the\nvalue is complete, correctly aligned, and\
    \ not clipped or overlapping the\nneighbouring cell.\n"
- id: pdf-cell-04
  answer: "`point` in `page.insert_text(point, text, ...)` is the **text baseline\norigin**: the point\
    \ where the *first glyph's baseline starts* (left edge of\nthe first glyph on its baseline), in PyMuPDF\
    \ display coordinates (points,\ny increases downward, origin top-left of `page.rect` / CropBox). The\
    \ glyphs\nare then drawn with their ascenders/descenders above/below that baseline —\ne.g. a 10 pt\
    \ font's cap height sits roughly 7 pt above the point, and\ndescenders go below it.\n\nWhy passing\
    \ the top-left corner of the old word's bbox is wrong:\n- The bbox's `y0` is the **top of the ink**\
    \ (ascender/overshoot), not the\n  baseline. Using it as `point.y` places the new text with its baseline\
    \ at\n  the old text's top, so the whole string is drawn roughly one cap-height\n  *too low* — it\
    \ drops into the row below, breaks the row's baseline\n  alignment, and can collide with the next\
    \ row's text or sit below the\n  cell's rule line.\n- The bbox's `x0` is the left of the ink, which\
    \ is fine as a left-aligned\n  start (modulo the left side bearing of the new glyphs), but for a\n\
    \  right-aligned number the correct x is computed from the desired *right*\n  edge minus the new text's\
    \ width (using\n  `fitz.Font(...).text_length(text, fontsize=size)`), not the old `x0`.\n- Rule of\
    \ thumb: for the y coordinate use `span[\"origin\"][1]` (the baseline)\n  from `page.get_text(\"dict\"\
    )`, or, if you only have a word bbox,\n  `baseline ≈ bbox.y1 - descender` (for most fonts, `y1` is\
    \ at/below the\n  descender, so a small subtraction is needed) — the reliable source is\n  `\"origin\"\
    ` in the span dict. For the x coordinate, use\n  `x = right_edge - text_length(new_text, fontsize)`.\n"
- id: pdf-relayout-01
  answer: 'Adding a row above the Total in a fixed-layout PDF means *everything* from

    the insertion point downward must be re-issued; the page does not reflow.

    A correct procedure:


    **1. Measure the new row''s height and the content that must move.**

    Take the height of a comparable data row (e.g. `tab.rows[k+1].bbox[3] -

    tab.rows[k+1].bbox[1]`, or the band between two horizontal rules) — call it

    `h_new`. Identify the band from the top of the Total row to the bottom of

    the page''s content (`move = fitz.Rect(x0, y_total, x1,

    page_bottom)`), and snapshot it exactly like pdf-rowdel-01 step 3

    (`pix = page.get_pixmap(clip=move, dpi=300)`), plus capture the vector rules

    in that band from `get_drawings()` (or from `tab.rows`) so you can re-emit

    them.


    **2. Clear the insertion point down to the bottom.**

    `page.add_redact_annot(fitz.Rect(x0, y_total, x1, page.rect.y1), fill=None)`

    then `page.apply_redactions(images=PDF_REDACT_IMAGE_NONE,

    graphics=PDF_REDACT_LINE_ART_NONE, text=PDF_REDACT_TEXT_REMOVE)` so the

    old Total row and everything under it is removed as *content* while the

    rules/shading are preserved for re-drawing.


    **3. Re-insert the shifted block, moved down by `h_new`.**

    `page.insert_image(fitz.Rect(x0, y_total + h_new, x1, y_total + h_new +

    move.height), pixmap=pix, keep_proportion=False)` — Total row, totals

    block, footer, terms, signature, page number all land `h_new` lower.


    **4. Re-draw the table rules across the new layout.**

    From the original `y_h` (horizontal rule positions) and `x_v` (vertical

    rules), build the new list: all rules at or below `y_total` shift by

    `+h_new`; then insert the new row''s two horizontal rules at `y_total` and

    `y_total + h_new`. Redraw:

    `page.draw_line(fitz.Point(x0, y), fitz.Point(x1, y), width=0.5,

    color=(0,0,0))` for each `y` in the new `y_h`, and the verticals spanning

    the (now taller) table. Redraw the cell backgrounds/fills too, sampling the

    original shading colour so alternating rows keep banding.


    **5. Write the new row''s text.**

    For each column, get the cell rect from the column bands intersected with

    `[y_total, y_total + h_new]`, then `page.insert_text` (for right-aligned

    numbers) with the style read from an existing row

    (`get_text("dict", clip=some_existing_cell)` → `font`, `size`, `color`,

    `origin` baseline, flags) and the x anchored to the column''s right edge

    minus `font.text_length(value, fontsize)`. Match vertical alignment by

    reusing a sibling row''s baseline offset within its band, and match the

    horizontal padding from the neighbouring cell (~3 pt).


    **6. Recompute the Total (and everything downstream).**

    Add the new line''s amount to the subtotal, recompute tax/discount/shipping

    and the grand total / amount due, and rewrite those figures with the same

    matched style at the same anchors. If the new total text is longer than the

    cell, use `insert_text` with a computed point (see pdf-cell-03/04) or widen

    the column.


    **7. Deal with content below the table.**

    If `h_new` pushed the block past the page bottom, the content no longer

    fits: you must either shrink `h_new` (use a tighter row), reduce leading, or

    reflow to a new page (rebuild the table on a fresh page with

    `page.show_pdf_page`/`insert_textbox` and drop the trailing content there),

    then fix "Page X of Y". Also update any cross-references, row numbering, and

    form fields/bookmarks anchored in the moved region.


    **8. Verify.** `page.get_text()` shows the new row once, the Total equals the

    new sum, no duplicates of the old Total; `page.find_tables()` reports

    `row_count + 1` and the expected `bbox`; render at 150–200 dpi and inspect

    the rule grid, banding, alignment, and the bottom margin; then

    `page.clean_contents()` and `doc.save(out, garbage=4, deflate=True)`.

    Caveat: a raster blit re-draws text as image (not selectable/searchable) —

    if the document must stay text-searchable, re-typeset the moved block with

    `insert_text` per span instead (see pdf-rowdel-03 option B), or rebuild the

    table from your data model, which is the cleanest way to guarantee correct

    totals and a valid grid.

    '
- id: pdf-relayout-02
  answer: "**Correct approach.** Treat the new column as new layout and re-typeset the\nwhole table, since\
    \ the table's total width is fixed by the page margins and\nadding a column means the existing columns\
    \ must be narrowed (or the\nmargins/paper changed). Concretely:\n\n1. **Extract the existing table\
    \ data** with `page.find_tables()` /\n`pdfplumber` (`tab.extract()`) — or better, get it from your\
    \ source data so\nyou can also recompute totals and formatting. Empty cells come back as\n`None`/empty\
    \ strings; fill them per your source.\n2. **Define the new column layout** explicitly: total width\n\
    \   `W = page.rect.width - left_margin - right_margin`; if\n   `n_new_cols > n_old_cols`, shrink the\
    \ text columns proportionally\n   (`new_w_i = W * (old_w_i / sum(old_w))` after reserving a fixed\
    \ width for\n   the new column, e.g. 12–18% of `W` for a numeric column). Compute all\n   column x-boundaries\
    \ `x_v[0..n]` from the margins, and the row band\n   heights from the existing `y_h` (keeping the\
    \ header/total band heights so\n   the row structure matches the original).\n3. **Rebuild the page\
    \ region:** redact the entire original table area\n   (`add_redact_annot` over the table bbox, then\
    \ `apply_redactions` with\n   `images=NONE, graphics=LINE_ART_NONE, text=REMOVE`) and redraw it as\n\
    \   vector: `page.draw_rect` / `page.draw_line` for every cell box with the\n   original `width`/`color`,\
    \ plus the header fill (`fill=original_fill`) and\n   the banding. Fill the new column's cells (e.g.\
    \ a rate or discount) and\n   recompute any total that depends on it.\n4. **Re-typeset the text**\
    \ cell by cell with `page.insert_text` at a\n   per-column anchor: left-aligned at `x_v[i] + 3`, right-aligned\
    \ at\n   `x_v[i+1] - 3 - font.text_length(value, fontsize)`, centred at\n   `(x_v[i] + x_v[i+1])/2`;\
    \ vertical position from a baseline derived from the\n   band (`y_band.y0 + (y_band.height - cap)/2`\
    \ or reuse the original\n   baseline offset). Read `font`, `fontsize`, `color`, bold/italic from the\n\
    \   original spans so the new table looks identical. Use\n   `insert_textbox` for the header/description\
    \ cells if they must wrap\n   (and check its return value).\n5. **Redraw the totals** (subtotal/tax/grand\
    \ total) with the new column\n   included, and reflow or re-place anything below the table (footers,\n\
    \   signatures, page numbers) if the table height changed.\n6. **Verify:** `page.find_tables()` reports\
    \ the new column count and\n   cell rects; `page.get_text()` contains every cell's text exactly once;\n\
    \   render and inspect alignment, borders, banding, and page fit; then\n   `clean_contents()` and\
    \ save with `garbage=4, deflate=True`.\nIf the new column genuinely cannot fit legibly (too many columns\
    \ for the\ntext width), the correct alternative is to change the page geometry —\nlandscape (`page.set_rotation(90)`\
    \ or re-render at a larger MediaBox) or a\nwider paper size — and rebuild accordingly.\n\n**Common\
    \ mistake.** Trying to \"make room\" by editing/erasing one existing\nrule and shoving the new column\
    \ into the reclaimed space, or by inserting\nthe new column on top of existing text (drawing a vertical\
    \ rule through\ncells and writing a header over old content) without re-typesetting the\ncolumns that\
    \ shift. Also the classic errors: keeping the sum of the column\nwidths equal to the *old* total (so\
    \ the table overflows the right margin or\nthe last column is squeezed), forgetting that narrowing\
    \ a column means\nre-fitting its *existing* text (which may now overflow and get truncated or\ncollide\
    \ with the next column), not re-issuing the rules with the old\n`x_v` values, redrawing only the horizontal\
    \ rules and forgetting the vertical\nones, losing the header shading/banding, and leaving the totals\
    \ computed\nfrom the old column set. And above all: the PDF has no reflow, so you cannot\n\"add a\
    \ column and let it push things along\" — every row, every rule and\nevery total must be explicitly\
    \ re-issued, or the table must be rebuilt from\nthe data.\n"
- id: pdf-relayout-03
  answer: "**When it is acceptable**\n\nOnly when the round trip is *lossless enough* for the specific\
    \ document. In practice that\nmeans all of the following hold:\n\n- The source PDF is text-based (not\
    \ a scan), and ideally produced by a converter you have\n  tested before (Word/Acrobat export, ReportLab,\
    \ LaTeX, HTML→PDF).\n- Only a *local* region is being edited — one table, one heading — and you do\
    \ not need\n  byte-level or layout-level control of the rest of the document.\n- The document has\
    \ no features DOCX cannot represent: exact-position text, vector art,\n  annotations/links, form fields,\
    \ bookmarks, tags/accessibility structure, embedded fonts,\n  multi-column or tightly-set layouts,\
    \ or RTL text.\n- You have a verification step (page raster diff / text diff / bounding-box check)\
    \ confirming\n  the rest of the page is unchanged, and you can fall back to a different method if\
    \ it is not.\n- It is a one-off with no requirement for reproducibility or exact pagination.\n\nRound-tripping\
    \ is genuinely useful when the table is *large* and *flowing* (multi-page\ncontinuation, wrapped headers)\
    \ and hand-drawing rows in PDF space would take far longer.\n\n**Risks**\n\n- *Silent reflow*: page\
    \ count, page breaks, and line positions change. Anything anchored to\n  the original pagination (headers,\
    \ footers, \"Page 2 of 7\", cross-references, bookmarks) breaks.\n- *Losing non-text content*: vector\
    \ graphics, rules/shading, image cropping, rotated text,\n  annotations, hyperlinks, form fields,\
    \ and layers are frequently dropped or rasterized.\n- *Font substitution*: the exact font is not installed\
    \ or not embeddable, so metrics change\n  and the table no longer fits its column widths. Text may\
    \ re-wrap and grow the row height.\n- *Structural loss*: the text layer survives, but the reading\
    \ order, tagging, and any table\n  cell structure the original relied on (e.g. ruled borders drawn\
    \ as line art rather than\n  real table borders) may be regenerated differently, or flattened.\n-\
    \ *Quality loss*: if any part is scanned, DOCX conversion silently embeds the scan as a\n  single\
    \ image and the table becomes uneditable — you get no error, just a file that cannot\n  be changed.\n\
    - *Metadata and provenance*: title, author, custom fields, and incremental-update history are\n  often\
    \ stripped; XMP/ID mismatch can confuse downstream systems or digital-signature checks.\n- *Layout\
    \ drift from the document's own styling*: the regenerated table picks up the\n  converter's default\
    \ margins, font, and rules rather than the surrounding document's.\n- *Failure mode is easy to miss*:\
    \ the conversion usually does not raise an error. You only\n  find out by looking, which is why a\
    \ rasterized before/after comparison is mandatory.\n\n**Rule of thumb**: for a localized edit inside\
    \ an existing document whose layout must stay\nintact, overlay/redaction on the original page is the\
    \ right tool. Use DOCX/HTML round-trip\nonly when you accept a full relayout of the file.\n"
- id: pdf-relayout-04
  answer: "Determine the *free regions* of the page and check the candidate rectangle is inside one.\n\
    \n1. **Pick a candidate rect** (where you want the table or text to start, e.g. bottom margin\n  \
    \ band) and expand it to the target width/height.\n2. **Collect everything already drawn on the page**,\
    \ not just images:\n   - text: `page.get_text(\"rawdict\")` gives per-character bboxes; `page.get_text(\"\
    words\")`\n     gives word-level boxes. Use `page.get_text(\"blocks\")` for block-level boxes, but\
    \ block\n     boxes can be much larger than the actual ink, so character- or word-level is tighter.\n\
    \   - vector drawings: `page.get_drawings()` → each item's `rect` (lines, fills, curves).\n   - images:\
    \ `page.get_image_rects(xref, transform=True)` or `page.get_image_info()`.\n   - annotations: `page.annots()`\
    \ → `.rect` (or `page.annots(types=...)`).\n   - links/widgets are annotations too; `page.widgets()`\
    \ for form fields.\n3. **Build a list of occupied rects** and intersect each with your candidate rect.\
    \ Use\n   `fitz.Rect & other` — the result is the overlap; test `overlap.is_empty` (or\n   `overlap.get_area()\
    \ > tolerance` to allow sub-point rounding noise). `Rect.contains()` /\n   `Rect.intersects()` are\
    \ the convenience checks.\n4. **Confirm the candidate is free** when no occupied rect intersects it.\
    \ Also check it stays\n   inside `page.rect` (respect `page.cropbox` if the page is cropped) and inside\
    \ the intended\n   margins.\n5. **Do the same check for the vertical growth**: the table may start\
    \ in free space but extend\n   downward, so test the *whole* rect, not just the anchor point. For\
    \ a table, reserve\n   `n_rows * row_height` and verify that too.\n6. **Check the area below/above,\
    \ not just sideways** — headers, footers, page numbers and\n   rules live there even though they are\
    \ not in the text block you were looking at.\n7. **Rules of thumb**: leave a small margin (a few points)\
    \ around any detected content; treat\n   a 1–2 pt intersection as noise by setting a tolerance rather\
    \ than an exact zero test.\n8. **Finally, verify empirically**: render the page (`page.get_pixmap()`)\
    \ and look at it, and\n   after drawing, render again and diff against the original raster to catch\
    \ anything the\n   rect analysis missed. Character-level text bboxes in particular are often *smaller*\
    \ than the\n   visible glyph ink (ascenders/descenders), so the visual check is not optional.\n"
- id: pdf-tblins-01
  answer: "Two practical approaches:\n\n**1. `page.draw_rect` + `page.insert_text` (drawn primitives,\
    \ no content stream tricks)**\n\n```python\nimport fitz\n\ndoc = fitz.open(\"in.pdf\")\npage = doc[0]\n\
    \nx0, y0 = 60, 700          # top-left of the table\ncol_w  = [120, 120, 120]  # three columns\nrow_h\
    \  = 18\nrows   = [[\"Item\", \"Qty\", \"Price\"],\n          [\"Bolt\",  \"4\",  \"1.20\"],\n   \
    \       [\"Nut\",   \"8\",  \"0.35\"]]\n\ntotal_w = sum(col_w)\nfont, size = \"helv\", 9\n\n# horizontal\
    \ rules\nfor r in range(len(rows) + 1):\n    y = y0 + r * row_h\n    page.draw_line((x0, y), (x0 +\
    \ total_w, y), width=0.4)\n\n# vertical rules\ncx = x0\nfor w in col_w:\n    page.draw_line((cx, y0),\
    \ (cx, y0 + len(rows) * row_h), width=0.4)\n    cx += w\npage.draw_line((x0 + total_w, y0), (x0 +\
    \ total_w, y0 + len(rows) * row_h), width=0.4)\n\n# text, baseline nudged inside the row box\nfor\
    \ r, row in enumerate(rows):\n    cx = x0\n    for c, (w, cell) in enumerate(zip(col_w, row)):\n \
    \       fontname = \"hebo\" if r == 0 else font      # bold header\n        page.insert_text((cx +\
    \ 3, y0 + r * row_h + row_h * 0.72), str(cell),\n                         fontname=fontname, fontsize=size)\n\
    \        cx += w\n\ndoc.save(\"out.pdf\", garbage=4, deflate=True)\n```\n\nVariant: `page.insert_textbox(rect,\
    \ text, ...)` handles wrapping and returns a negative value\nif the text did not fit — a handy self-check\
    \ that the column is wide enough.\n\n**2. `page.draw_rect` with a grid of thin filled rectangles,\
    \ or use a helper that emits the\nwhole grid from one rule set**\n\nThe same primitives but expressed\
    \ as filled rules (`page.draw_rect(..., fill=(0,0,0),\ncolor=None)` with a hairline width) gives a\
    \ \"ruled\" look and avoids stroke-width rounding\nartefacts on the joins. If the table is large,\
    \ build the rule set once and reuse it, or\nconstruct the grid with `Shape` (`page.new_shape()`) which\
    \ batches drawing commands into a\nsingle content-stream operation — much faster and smaller output\
    \ than dozens of\n`draw_line` calls.\n\n**Third option (if you already have HTML/LaTeX)**: render\
    \ just the table to its own small PDF\n(e.g. ReportLab/WeasyPrint), then `page.show_pdf_page(target_rect,\
    \ src_doc, 0)` to stamp that\none-page PDF into the free area of the target page. Good when the table\
    \ is complex.\n"
- id: pdf-tblins-02
  answer: "To look consistent, *sample the document's own conventions rather than picking your own*:\n\
    \n- **Fonts**: read the fonts already on the page (`page.get_fonts(full=True)`) and reuse the\n  same\
    \ family and weight. If the text is an embedded subset, do **not** try to reuse the\n  subset file\
    \ for new glyphs (see `pdf-fonts-01`); instead use a metrically compatible\n  standard font (e.g.\
    \ Helvetica/Arial for a Helvetica/Arial document) or load a real font file\n  with `page.insert_font(fontfile=...)`\
    \ and use `fontname=` that alias.\n- **Size and colour**: take the body size from `page.get_text(\"\
    dict\")` spans; match the ink\n  colour via `page.get_texttrace()` (which reports the fill colour\
    \ as an sRGB int) or by\n  reading the span colour, rather than defaulting to black.\n- **Geometry**:\
    \ use the same line width as the existing rules (inspect `page.get_drawings()`),\n  the same row height\
    \ as neighbouring table rows, and align the table's left edge with the\n  body text margin (take it\
    \ from the text block bboxes, not the page edge).\n- **Line joins**: draw all rules of the same colour/width\
    \ as one batched `Shape`, and keep\n  stroke width ≥ ~0.4pt so lines survive PDF rasterization. Snap\
    \ coordinates to a consistent\n  grid; half-pixel alignment avoids a fuzzy, doubled-looking rule.\n\
    \nGlitches to avoid specifically:\n\n- **Z-order / content stream order**: PyMuPDF appends new content\
    \ *after* existing page\n  content, so a new opaque fill will cover text. Draw fills/backgrounds first\
    \ within your own\n  operation, and never fill a rect that spans existing text. If you need a background\
    \ behind\n  new text, draw the fill and the text together in one shape.\n- **Overlaps**: verify the\
    \ target rect is empty (see `pdf-relayout-04`) before drawing, and\n  check the whole table footprint\
    \ including its last row, not just the anchor point.\n- **Overlapping rules / double-drawn lines**\
    \ (two 0.4pt strokes at nearly the same position\n  render darker and slightly thick) — snap to a\
    \ grid and draw each rule exactly once.\n- **Text clipping / overflow**: use `insert_textbox` and\
    \ check its return value; shrink the\n  font or widen the column rather than letting a string run\
    \ into the next column.\n- **Baseline placement**: put the baseline inside the row box, not at the\
    \ row's top edge, or\n  glyphs will appear to sit in the row above.\n- **Transparency groups and clipped\
    \ content**: if the page has a transparency group or a\n  clipping path, appended content can be clipped\
    \ unexpectedly. `page.wrap_contents()` after\n  page manipulation is the standard remedy for fixing\
    \ a broken base state (e.g. after\n  `show_pdf_page`).\n- **Redaction artefacts**: if you redact a\
    \ region first and then insert, the redaction\n  fill boxes must be the right size or the new text\
    \ sits on a black patch.\n- **Compression/garbage collection**: save with `garbage=4, deflate=True,\
    \ clean=True` to keep\n  the file from ballooning, and re-open + re-render the result to confirm the\
    \ table is there\n  and nothing else moved.\n"
- id: pdf-tblins-03
  answer: "\"No gap\" usually means the paragraphs are adjacent line boxes, or a text block's reported\
    \ bbox\nis taller than the visible ink. Options, roughly in order of preference:\n\n- **Widen the\
    \ gap by editing the content stream text positions.** Find the first baseline of\n  the second paragraph\
    \ and shift every span in that paragraph (and everything after it) down\n  by the amount you need:\
    \ `page.get_texttrace()` gives per-character positions, or use\n  `page.insert_text(..., render_mode=...)`/raw\
    \ content-stream editing. The classic PyMuPDF\n  approach is to read the content stream (`page.read_contents()`),\
    \ rewrite the `Td`/`TD`/`Tm`\n  numbers, and `page.clean_contents()`. This is precise but fiddly and\
    \ can reflow text.\n- **Use `page.insert_textbox` with a negative-height/offset trick**: place the\
    \ first table\n  row *inside* the second paragraph's leading (between the two baselines) if the gap\
    \ is a few\n  points — usually too tight to be legible, so treat this as a last resort.\n- **Use an\
    \ annotation with an appearance stream and then flatten it.**\n  `annot = page.add_rect_annot(rect)`\
    \ / `add_text_annot(...)` (which carries its own /AP\n  appearance XObject) or `page.insert_htmlbox(rect,\
    \ html)` (which internally uses an\n  appearance stream). The appearance is stored in the *annotation*\
    \ dictionary, so it is not\n  constrained by the page content stream's text positions, and after `page.delete_annot`\
    \ +\n  re-rasterization, or `doc.bake(annots=True)`, the content becomes ordinary page content.\n\
    \  This is the most reliable way to add arbitrary drawn content at an arbitrary position.\n- **Draw\
    \ over it anyway** — if the visual gap is really there (the block bbox is just padded),\n  you can\
    \ insert the table into the whitespace inside the block's bbox without moving\n  anything. Verify\
    \ by rendering, not by trusting the bbox.\n- **Split the page**: if the paragraphs are truly abutting,\
    \ insert the table on its own and\n  re-flow — e.g. `doc.delete_page`/`insert_page` plus copying content,\
    \ or use\n  `doc.manipulate()` to re-arrange, or convert to a single-page-per-sheet layout.\n- **Two-step\
    \ with redaction**: redact a rect spanning the boundary, then insert. Works but\n  destroys the original\
    \ characters if your rect is oversized, so size it from\n  `get_text(\"words\")` boxes and confirm\
    \ with a raster diff.\n- **Give up on the exact spot**: append the table after the last paragraph,\
    \ or on the next\n  page, and leave a cross-reference. Cheapest and safest when the content is reference\n\
    \  material rather than a signature line.\n\nWhatever you choose, finish with `page.wrap_contents()`\
    \ if the content stream state looks\nodd, save, and rasterize the page to confirm the paragraphs did\
    \ not move or overlap.\n"
- id: pdf-imgrep-01
  answer: "Measure the existing image's rect, then add the new PNG at exactly that rect (or redact first\n\
    so the old pixels are actually gone).\n\n```python\nimport fitz\n\ndoc = fitz.open(\"in.pdf\")\npage\
    \ = doc[0]\n\n# Locate the logo. Try the page's image xrefs first.\nold_rect = None\nfor info in page.get_image_info(xrefs=True):\n\
    \    r = fitz.Rect(info[\"bbox\"])\n    if r.width > 20 and r.height > 20 and r.y0 < 200:      # near\
    \ the top = header logo\n        old_rect = r\n        old_xref = info[\"xref\"]\n        break\n\n\
    if old_rect is None:                      # fall back: the logo may be vector or an annot\n    for\
    \ d in page.get_drawings():\n        r = d[\"rect\"]\n        if r.y0 < 200 and 20 < r.width < 400\
    \ and 20 < r.height < 200:\n            old_rect = r\n            break\n\n# Remove the old artwork\
    \ (covers both image and vector cases)\npage.add_redact_annot(old_rect, fill=(1, 1, 1))\npage.apply_redactions()\
    \                    # actually strips the pixels, not just hides them\n\n# Insert the replacement\n\
    page.insert_image(old_rect, filename=\"new_logo.png\", keep_proportion=True, overlay=True)\ndoc.save(\"\
    out.pdf\", garbage=4, deflate=True)\n```\n\nKey details:\n- `page.get_image_info(xrefs=True)` returns\
    \ the *placement* bbox (`info[\"bbox\"]`), which is\n  what you want; `page.get_images()` only returns\
    \ the stored xrefs with no geometry.\n- `insert_image(rect, ...)` scales the bitmap to fill `rect`;\
    \ pass `keep_proportion=True` to\n  avoid distortion, and remember it may then not fill the rect exactly.\n\
    - Use `overlay=True` only if you did *not* redact — redaction is what removes the old pixels.\n  Overlaying\
    \ leaves the old image underneath (see `pdf-imgrep-02`).\n\n**The catch with shared images (xref reuse):**\
    \ PDF images are stored once in the file and\nreferenced by many pages/objects. If you call `doc.replace_image(xref,\
    \ ...)` on an xref that is\nreferenced from 20 places, **every** occurrence changes — you cannot replace\
    \ it on page 1 only.\nConversely, `insert_image` creates a *new* xref, so the old shared one survives\
    \ and still\ncosts space in the file.\n\nThe robust approach for a page-local swap: redact the old\
    \ rect on that page (which only\nremoves that page's reference to it), insert the new image, then\n\
    `doc.save(..., garbage=4)` — garbage collection drops the now-unreferenced xref and the file\ndoes\
    \ not grow. To verify scope before you commit, enumerate references with\n`doc.xref_get_key(xref,\
    \ \"Referencer\")` or simply search the file for that xref\n(`doc.xref_length()` / inspect each page's\
    \ `get_image_info(xrefs=True)`) to see how many\npages use it. If you genuinely want to change it\
    \ everywhere, `doc.replace_image(xref, filename=...)`\nor `doc.replace_xref(...)` is the correct tool\
    \ — just accept the global effect.\n"
- id: pdf-imgrep-02
  answer: "The new image is *drawn on top of* the old one, but the old one is still in the file and still\n\
    visible in any context where the top image does not fully cover it.\n\nConcretely:\n\n- **The old\
    \ pixels remain in the PDF.** The file still contains the old logo's image data\n  and the original\
    \ placement operator. You have not replaced anything; you have stacked a\n  second copy on top. Extract\
    \ the page and you can still recover the original logo\n  (`page.get_images()` still lists it) — which\
    \ defeats the point if the goal was removal,\n  redaction, size reduction, or copyright/licence cleanup.\n\
    - **No transparency problem, but any mismatch shows.** If the new PNG has an alpha channel\n  and\
    \ the logo needs to be opaque, or if `keep_proportion` left gaps, the old image shows\n  through the\
    \ gaps and the two logos appear ghosted side by side. Same if the new rect is a\n  few points smaller\
    \ than the old one.\n- **The file grows** by the size of the new image on every page you do this to,\
    \ and\n  `garbage=4` will not help because the old xref is still referenced.\n- **Z-order fragility**:\
    \ content is drawn in stream order, so anything appended later sits\n  on top. The old image still\
    \ captures clicks/transparency in viewers, and later edits that\n  \"clean\" or reflow the page can\
    \ make the old logo reappear.\n- **Invisible redaction failure**: if the intent was to *remove* something,\
    \ overlaying is\n  exactly the mistake that leaks it — reviewers who open the file in an editor, or\
    \ who\n  recover earlier versions from the incremental-update history, still see it.\n\n**The correct\
    \ method**: use redaction, not overlay.\n`page.add_redact_annot(rect); page.apply_redactions()` deletes\
    \ the underlying content (and\nthe image reference from that page), then `page.insert_image(rect,\
    \ filename=..., overlay=True)`\nputs the new image in the freed area. That is a real replacement:\
    \ the old image data is\ndropped by garbage collection on save. Then verify with `page.get_images()`\
    \ (the old xref\nshould be gone) and a raster comparison, not by eyeballing the rendered page — the\
    \ rendered\npage looked \"right\" in your case precisely because it is hiding the bug.\n"
- id: pdf-imgrep-03
  answer: "If `page.get_images()` is empty but you can see the logo, the artwork is not an image XObject.\n\
    In order of likelihood:\n\n- **It is vector art** — a logo drawn with lines, curves and fills. This\
    \ is very common for\n  logos exported from Illustrator or produced by a vector-to-PDF workflow, and\
    \ it is why\n  `get_images()` returns nothing. Find it with `page.get_drawings()` and look for a small\n\
    \  cluster of items in the header area.\n- **It is a Type 3 font glyphs** used as a logo/monogram.\n\
    - **It is a Form XObject** (a nested content stream). `get_images()` only reports the page's\n  direct\
    \ resources; a form's images live one level down.\n- **It is an annotation appearance** — the logo\
    \ is the rendered content of a stamp, image\n  annotation, or a widget.\n- **It is a clipping path\
    \ + fill** that your viewer renders from an embedded shading/pattern.\n- **It is an inline image**\
    \ (`BI ... ID ... EI`) in the content stream rather than an XObject.\n- Rarely: the logo is a *masked*\
    \ image, or `get_images(full=...)` behaviour differs by version\n  — always cross-check with `page.get_image_info()`\
    \ (which also reports inline images) and\n  `doc.get_page_images(page.number, full=True)`.\n\n**How\
    \ to find it** — in order:\n\n1. `page.get_drawings()` — for each item take `d[\"rect\"]`, and cluster\
    \ the rects that are\n   small and in the same area (e.g. `y1 < 200`). That cluster is the vector\
    \ logo.\n2. `page.get_text(\"dict\")` — check for an odd text run where you expected art (a monogram\
    \ or\n   wordmark set as text).\n3. `page.annots()` and `page.widgets()` — check annotation rects\
    \ in the header area.\n4. Inspect the content stream directly: `page.read_contents()` and look for\
    \ `Do` operators\n   (form/image XObjects), `BI` (inline image), or a long run of path operators.\n\
    5. Raw resource listing: `doc.xref_object(page.xref)` then look at `/Resources /XObject`, and\n  \
    \ recurse into each form XObject's own resources.\n6. Empirically: rasterize just the region (`page.get_pixmap(clip=region_rect,\
    \ dpi=300)`) to\n   confirm the artwork is there and get its exact bbox.\n\n**How to replace it**:\n\
    \n- Vector: `page.add_redact_annot(rect, fill=(1,1,1))` + `page.apply_redactions()` to remove the\n\
    \  paths, then `page.insert_image(rect, filename=...)`. Note redaction removes drawing\n  operators\
    \ intersecting the annot, so size the rect from the drawing rects, and re-render to\n  confirm nothing\
    \ else was caught.\n- Form XObject / inline image: same redact-then-insert approach works, because\
    \ redaction\n  operates on the rendered content, not on the resource type.\n- Annotation: delete the\
    \ annotation (`page.delete_annot(annot)`) and insert the image, or\n  update the annotation's appearance\
    \ stream.\n- If you must not disturb layout, do not redact — instead draw an opaque filled rect over\
    \ the\n  old art (`page.draw_rect(rect, color=None, fill=(1,1,1), overlay=True)`) and then insert.\n\
    \  But that only hides the art, so reserve it for cosmetic swaps, never for removal.\n"
- id: pdf-imgins-01
  answer: "```python\nimport fitz\n\ndoc = fitz.open(\"in.pdf\")\npage = doc[0]\n\ntarget_w = 150.0\n\
    # Native pixel size / DPI of the PNG, so we can compute its true height\n# (or read the size directly:\
    \ p = fitz.Pixmap(\"sig.png\"); p.width, p.height)\np = fitz.Pixmap(\"signature.png\")\ndpi = 96 \
    \                      # fitz.Pixmap's default; adjust if the PNG stores a real DPI\niw, ih = p.width,\
    \ p.height\ntarget_h = target_w * (ih / iw)        # aspect ratio preserved exactly\n\n# Bottom-right\
    \ placement with an inset margin\nM = 36.0\nr = fitz.Rect(page.rect.width - M - target_w,\n      \
    \        page.rect.height - M - target_h,\n              page.rect.width - M,\n              page.rect.height\
    \ - M)\nr &= page.rect                          # never exceed the visible page area\n\n# Do not cover\
    \ text: verify the rect is free\nblocker = fitz.Rect(0, 0, 0, 0)\nfor w in page.get_text(\"words\"\
    ):\n    blocker |= (fitz.Rect(w[:4]) & r)\nfor d in page.get_drawings():\n    blocker |= (d[\"rect\"\
    ] & r)\nassert blocker.is_empty, f\"rect overlaps existing content: {blocker}\"\n\npage.insert_image(r,\
    \ filename=\"signature.png\", keep_proportion=True, overlay=True)\ndoc.save(\"out.pdf\", garbage=4,\
    \ deflate=True)\n```\n\nNotes:\n- `insert_image(rect, ...)` will scale the bitmap into `rect`; passing\n\
    \  `keep_proportion=True` makes it fit inside rather than stretch. Passing the aspect-correct\n  `rect`\
    \ computed above and leaving `keep_proportion=True` is belt-and-braces.\n- Honour `page.cropbox` (and\
    \ rotation) if the page is cropped or rotated — `page.rect` is\n  already rotation-aware, but a rotated\
    \ page can still surprise you; re-render to check.\n- `get_text(\"words\")` boxes are tight and sometimes\
    \ *smaller* than the visible glyph ink, so\n  also render the clipped region and look at it:\n  `page.get_pixmap(clip=r,\
    \ dpi=200)` — cheaper than diffing the whole page.\n- For a transparent PNG signature, `insert_image`\
    \ handles the alpha channel; use\n  `keep_proportion=True` so it does not get squashed.\n"
- id: pdf-imgins-02
  answer: "The cause is that `page.insert_image` stores a **new image XObject on every page**. PyMuPDF\n\
    does not deduplicate, so 200 pages means 200 copies of the bytes in the file (and 200\nseparate objects).\
    \ The fix is to insert the image **once** and have every page reference the\nsame xref.\n\n```python\n\
    import fitz\n\ndoc = fitz.open(\"in.pdf\")\n# 1. Insert the logo once, on page 0, at the position\
    \ you want.\npage0 = doc[0]\nrect = fitz.Rect(page0.rect.width - 250, 20, page0.rect.width - 20, 80)\n\
    page0.insert_image(rect, filename=\"logo.png\", keep_proportion=True)\n\n# 2. Reuse that xref for\
    \ all remaining pages by adding an <img> placeholder and\n#    swapping its content for the same xref.\n\
    xref = page0.get_images(full=True)[0][0]\n\nfor page in doc:\n    if page.number == 0:\n        continue\n\
    \    # A zero-size marker marks where the image should appear\n    page.insert_image(fitz.Rect(0,\
    \ 0, 0, 0), xref=xref)   # placeholder, no bytes\n    doc.update_object(page.get_contents()[0], \"\
    q 0 0 0 0 0 0 cm Q\")  # no-op guard\n    # Now rewrite the placeholder's name to point at the shared\
    \ xref\n    page._replace_image_on_page(page.get_images(full=True)[-1][0], xref)  # internal helper\n\
    ```\n\nThe idiomatic, version-independent way to do the same thing:\n\n```python\n# Insert once to\
    \ create the XObject, then stamp the *same* xref elsewhere\n# by creating a tiny 1x1 placeholder image\
    \ and re-pointing it in /Resources.\n```\n\nSimpler and officially supported alternatives:\n\n- **Stamp\
    \ a one-page \"template\" PDF.** Render the logo onto a blank one-page PDF once, then\n  `page.show_pdf_page(target_rect,\
    \ logo_doc, 0, overlay=True)` on every page. The image XObject\n  lives in the source document and\
    \ each page gets only a form-XObject reference. For a logo\n  this is fine; it also lets you scale/rotate\
    \ the logo per page.\n- **Put the logo in the page's shared resources yourself** with `doc.update_object`\
    \ on\n  `/Resources /XObject`, inserting a `/ImN xref 0 R` entry for each page and then drawing it\n\
    \  with `page.insert_image(rect, xref=xref)` (passing `xref=` — not `filename=` — tells\n  PyMuPDF\
    \ to reuse existing image data rather than add a new copy). Note `insert_image` with an\n  `xref`\
    \ still needs a placeholder to size against; passing the target `rect` handles it.\n- **`doc.bake()`**\
    \ to flatten annotations to page content, then apply a `Shape` once and\n  re-`insert_image` only\
    \ where needed.\n\nWhatever route you take, confirm the win:\n\n```python\nimport os\nbefore = os.path.getsize(\"\
    in.pdf\")\n# ... insert on 200 pages ...\ndoc.save(\"out.pdf\", garbage=4, deflate=True)\nprint(before,\
    \ os.path.getsize(\"out.pdf\"))\n# and check the object count / how many pages reference the image:\n\
    print(len(page.get_images(full=True)), doc.xref_length())\n```\n\n`garbage=4, deflate=True` will drop\
    \ orphaned images but **cannot** merge 200 legitimately\nreferenced copies — deduplication has to\
    \ happen at insertion time. Related: never call\n`doc.subset_fonts()` or re-save per page in a loop,\
    \ and avoid `insert_image` with\n`filename=` on every page.\n"
- id: pdf-imgins-03
  answer: "**Causes for \"did not appear\":**\n\n- **Content is behind existing content.** PyMuPDF appends\
    \ new content at the end of the page\n  content stream, so it draws *on top* of existing content by\
    \ default — but if the page has a\n  transparent/white filled rectangle drawn *after* your insertion\
    \ in stream order, or you\n  passed `overlay=False` with an opaque box above, the image is painted\
    \ over. Check the\n  drawing order with `page.get_drawings()` and re-insert with `overlay=True`.\n\
    - **Broke the content stream.** A malformed stream (e.g. an unbalanced `q`/`Q` or an\n  unclosed `BT`/`ET`\
    \ from a hand edit) can make a viewer discard the trailing part of the\n  content, which is exactly\
    \ where appended content lives. Fix with `page.clean_contents()`\n  and `page.wrap_contents()`.\n\
    - **Image failed to load / alpha mismatch.** Bad or unsupported PNG, or a CMYK/16-bit PNG that\n \
    \ needs conversion — `fitz.Pixmap` will complain, but a soft failure can be silent. Convert to\n \
    \ 8-bit RGB/RGBA first, or use `pix.tobytes(\"png\")`.\n- **Rect is degenerate or off-page.** `insert_image`\
    \ with a zero-area rect, or a rect that\n  falls outside `page.rect` (e.g. negative y, beyond the\
    \ media box), puts it outside the\n  visible area or is rejected.\n- **Wrong page / wrong object.**\
    \ You edited `doc[0]` but looked at another page, or the\n  insert raised and you never checked the\
    \ return.\n- **The image is there but the text is on top** — see the \"underneath\" cases below.\n\
    \n**Causes for \"appears underneath a filled background box\":**\n\n- **A background rectangle was\
    \ drawn after your insert** (later in the content stream), so it\n  paints over the image. Fix: insert\
    \ with `overlay=True`, or draw the background *before* the\n  image (use one `Shape`: fill first,\
    \ then `shape.finish()`, then the image).\n- **The image is inside a transparency group or clipped\
    \ region** that restricts it — check\n  `/Group` and `/SMask` on the parent and the clip path in the\
    \ stream.\n- **`overlay=False`** explicitly sends the content to the *underlay*, which is drawn before\n\
    \  the page's own content — so any filled box on the page covers it. (In current PyMuPDF,\n  `overlay=False`/`False`\
    \ is a no-op/back-compat shim, which is exactly why people see\n  inconsistent results across versions\
    \ — treat `overlay` as unreliable and control order with a\n  `Shape` instead.)\n- **Z-order vs. annotation\
    \ stacking** — a form field or stamp annotation's appearance is\n  painted above page content regardless\
    \ of insert order.\n- **Opaque white fill from a redaction** applied *after* your insert.\n\n**Diagnose\
    \ by looking, not by assuming:**\n\n```python\np = page.get_pixmap(clip=target_rect, dpi=200)   #\
    \ crop the region — is the image there?\npage.get_texttrace()                             # what is\
    \ drawn, and in what order\npage.get_drawings()                              # is there a big filled\
    \ rect over your area?\nprint(page.get_images(full=True))                # did the xref get created?\n\
    doc.xref_object(page.xref)                       # is it in /Resources /XObject?\n```\n\nThen fix\
    \ by rebuilding the page content in the order you want with `page.new_shape()`:\nfill backgrounds,\
    \ `shape.finish()`, insert the image, then `page.wrap_contents()` and\nre-render to confirm.\n"
- id: pdf-fonts-01
  answer: "A PDF embedded font is normally a **subset**: only the glyphs actually used in the document\n\
    were included, and the subset is tagged with a six-letter prefix (`AAAAAA+Georgia` —\nthe \"subset\
    \ tag\"). The font's internal `cmap` therefore maps only those character codes to\nthe handful of\
    \ glyphs present. Characters you add that were never used are either **absent\nfrom the cmap** or\
    \ map to **glyph 0 (`.notdef`)**. PyMuPDF will not add glyphs to an already\nembedded font; reusing\
    \ it means your new characters render as nothing, as an empty box, or as\na wrong glyph (and `insert_text`\
    \ may silently drop them or draw `.notdef`). Font subsetting is\nalso what makes embedding small,\
    \ so the PDF genuinely does not contain the outlines you need —\nthe data is simply not in the file.\n\
    \n**What to do**\n\n- **Do not try to extend the subset.** Instead, embed a *full* font that contains\
    \ the\n  characters, and use it for the new text.\n- **Load the real font file** (e.g. `/usr/share/fonts/.../Georgia.ttf`\
    \ or a font you have a\n  licence to embed) and register it once per document:\n  ```python\n  fontname\
    \ = page.insert_font(fontname=\"georgia\", fontfile=\"Georgia.ttf\")\n  page.insert_text(point, \"\
    Zażółć gęślą jaźń\", fontname=fontname, fontsize=8)\n  ```\n  `fontfile` causes PyMuPDF to subset\
    \ *on write* from the full TTF — so you get the glyphs you\n  need, still compact.\n- **Prefer a metrically\
    \ compatible substitution when the exact font is unavailable.** If the\n  document's face is Georgia,\
    \ a serif, use a metrically similar font; for the ubiquitous\n  Helvetica/Times/Courier cases, use\
    \ the corresponding base-14 name (`helv`, `tiro`, `cour`).\n  Expect small differences in width and\
    \ hinting.\n- **Match the styling manually** — read the original size, weight, and fill colour from\n\
    \  `page.get_text(\"dict\")` (span `flags`: bit 0 = superscript, bit 1 = **bold**,\n  bit 2 = *italic*,\
    \ bit 4 = monospace) and `page.get_texttrace()` (colour as sRGB int).\n- **Watch the font's encoding/ToUnicode.**\
    \ Text you add with a different font will not be\n  part of the document's ToUnicode map automatically;\
    \ verify by *extracting text* after\n  saving (`page.get_text()`) — if your new text is not extractable/searchable,\
    \ that is a real\n  defect (e.g. wrong font without a usable cmap) and you should load the font as\n\
    \  `fontfile` with PyMuPDF's own embedding rather than reusing an XObject.\n- **Never silently drop\
    \ characters**: after inserting, extract the text of the region and\n  assert your new string is present.\n\
    - Related: if you do want to *retext* while keeping the original look, consider\n  `page.insert_htmlbox(rect,\
    \ html=...)`, which handles font fallback and subsetting for you.\n"
- id: pdf-fonts-02
  answer: "**Base-14 names in PyMuPDF**\n\n| Face         | Regular | Bold  | Italic | BoldItalic |\n\
    |--------------|---------|-------|--------|------------|\n| Helvetica    | `helv`  | `hebo` | `heit`\
    \ | `hebi`     |\n| Times        | `tiro`  | `tibo` | `tiit` | `tibi`     |\n| Courier      | `cour`\
    \  | `cobo` | `coit` | `cobi`     |\n| Symbol       | `symb`  | —     | —      | —          |\n| ZapfDingbats\
    \ | `zadb`  | —     | —      | —          |\n\nSo Helvetica regular is **`\"helv\"`** and Helvetica\
    \ bold is **`\"hebo\"`**. In `insert_text` you\ncan also pass the full PostScript names `\"Helvetica\"\
    `, `\"Helvetica-Bold\"`, `\"Helvetica-Oblique\"`,\n`\"Helvetica-BoldOblique\"`, `\"Times-Roman\"`,\
    \ `\"Times-Bold\"`, `\"Courier\"`, etc.\n\n**Limitations of base-14 fonts**\n\n- **Standard-Encoding\
    \ only (for the text faces).** They are WinAnsi/Latin-1-ish, roughly 225\n  glyphs. Any character\
    \ outside Latin-1 plus a handful of typographic extras — Greek, Cyrillic,\n  Hebrew, Arabic, Devanagari,\
    \ CJK, Thai, emoji — is unavailable. It will be missing, become\n  `.notdef` (blank/box), or be dropped;\
    \ no fallback happens automatically.\n- **Special symbols are patchy.** Smart quotes, en/em dashes\
    \ and ellipsis exist; many typographic\n  characters (maths operators, arrows, superscript digits,\
    \ `≤`, `∑`, `€` depending on viewer)\n  are not. Symbol and ZapfDingbats are single-purpose and offer\
    \ nothing outside their glyph set.\n- **Non-Latin scripts cannot be shaped.** No complex-script handling:\
    \ no Arabic joining forms,\n  no contextual forms, no bidi/reordering, no Indic reordering or conjuncts.\
    \ Those scripts need\n  a real embedded font plus HarfBuzz-style shaping, which PyMuPDF does not do\
    \ for you.\n- **Not embedded.** Base-14 fonts are referenced by name only, so the file stays small\
    \ — but\n  rendering depends on the viewer having the font. Most viewers substitute, which changes\n\
    \  metrics and appearance (Helvetica may be replaced by Arial or Nimbus Sans, for example).\n  This\
    \ is a real risk for byte-identical visual output.\n- **No subsetting benefit, and hinted metrics\
    \ differ** from the original embedded face, so\n  text can reflow relative to the surrounding content.\n\
    \n**What to use instead**: for anything beyond Latin-1, load a TTF/OTF with\n`page.insert_font(fontname=\"\
    myname\", fontfile=\"path/to/font.ttf\")` and use that alias —\nPyMuPDF subsets it on write, so you\
    \ keep the file small *and* get the glyphs. If you only need\nto *read* a non-Latin document, `page.get_text()`\
    \ can still extract it via the ToUnicode CMap\neven though the base-14 limitation is about writing.\n"
- id: pdf-fonts-03
  answer: "The new text came out regular because you passed a plain font name (`\"helv\"`) instead of\
    \ the\ndocument's bold face, and the size differed because you guessed it. Detect the real weight\
    \ and\nsize from the document rather than by eye.\n\n**1. Read the spans near the cell (weight + size\
    \ + colour in one pass)**\n\n```python\nd = page.get_text(\"dict\", flags=fitz.TEXTFLAGS_DICT & ~fitz.TEXT_PRESERVE_IMAGES)\n\
    for b in d[\"blocks\"]:\n    for l in b.get(\"lines\", []):\n        for s in l[\"spans\"]:\n    \
    \        print(s[\"text\"], \"| font:\", s[\"font\"], \"| size:\", round(s[\"size\"], 2),\n      \
    \            \"| flags:\", s[\"flags\"], \"| color:\", s[\"color\"], \"| bbox:\", s[\"bbox\"])\n```\n\
    \n- `s[\"font\"]` is the PostScript font name, e.g. `AAAAAA+Georgia-Bold`, `Helvetica-Bold`,\n  `Arial,Bold`.\
    \ The `,Bold` / `-Bold` / `Bold` substring is the giveaway; PyMuPDF may\n  normalize to `ABCDEF+Arial,Bold`.\n\
    - `s[\"flags\"]` is the authoritative, font-independent bitfield:\n  `1` = superscript, `2` = **bold**,\
    \ `4` = *italic*, `8` = serif, `16` = monospaced,\n  `32` = smallcaps. `(flags & 2)` → bold, `(flags\
    \ & 4)` → italic.\n- `s[\"size\"]` is the actual rendered size in points — use it verbatim rather\
    \ than rounding to\n  a \"nice\" number.\n- `s[\"color\"]` is the fill colour as an sRGB integer (`0`\
    \ = black, `0xffffff` = white).\n- `s[\"bbox\"]` is the span's box, which tells you the baseline position\
    \ and how the cell's\n  existing text is aligned.\n\n**2. Confirm the rendered size, not just the\
    \ declared one**\n\nFont size in the span can differ from the visual size when the text was scaled\
    \ by the text\nmatrix. `page.get_texttrace()` reports per-character `size` and `color` after all transforms,\n\
    which is the ground truth:\n\n```python\nfor sp in page.get_texttrace():\n    if sp[\"chars\"]:\n\
    \        print(sp[\"font\"], sp[\"size\"], sp[\"color\"], sp[\"type\"], sp.get(\"seqno\"))\n```\n\n\
    Compare the size of a character *inside the header cell* with a character in a body cell —\nheaders\
    \ are often 0.5–1 pt larger or bolder, and the two may come from different font\nobjects.\n\n**3.\
    \ Match the original font program, not just a lookalike**\n\n```python\nfor f in page.get_fonts(full=True):\
    \     # (xref, ext, type, basefont, name, encoding, ...)\n    print(f)\n```\n\nIf the header uses\
    \ an embedded subset `AAAAAA+Georgia-Bold`, the safest match is a full\n`Georgia-Bold.ttf` (or a metric-compatible\
    \ face) loaded with\n`page.insert_font(fontname=\"hdr\", fontfile=\"Georgia-Bold.ttf\")`. If it is\
    \ not available and the\noriginal is one of the standard faces, map to the base-14 equivalent (`Helvetica-Bold`\
    \ →\n`hebo`, `Times-Bold` → `tibo`, `Courier-Bold` → `cobo`).\n\n**4. Then match the geometry**\n\n\
    Use the original span's `bbox` to place the baseline: `baseline_y ≈ bbox[1] + ascender_size`\n(or\
    \ derive it from `get_texttrace`'s per-character `origin`/`bbox`, which gives the true\nbaseline),\
    \ and the same `bbox[0]` for the left edge. Reuse the original `color`, and match the\nhorizontal\
    \ alignment of the rest of the column (left, centred, or right — infer from the other\ncells' `bbox`\
    \ x-positions and column widths).\n\n**5. Verify**\n\nExtract the text back and assert the weight\
    \ and size landed as intended\n(`get_text(\"dict\")` on the saved file, checking `flags & 2` and `s[\"\
    size\"]` against the\noriginal values), and rasterize the cell and compare against the original crop.\
    \ Font\nsubstitution is the usual reason a \"matching\" bold comes out regular — check that the\n\
    registered `fontname` you passed to `insert_text` is actually the one you loaded.\n"
- id: pdf-verify-01
  answer: "\"The script ran\" only proves no exception was raised. Verify the *output artefact* against\n\
    the *input*, in layers:\n\n1. **Text round-trip.** Extract the text of the edited region and assert\
    \ your new value is\n   present and the old one is gone:\n   ```python\n   t = doc[0].get_text()\n\
    \   assert \"New Item Name\" in t\n   assert \"Old Item Name\" not in t\n   ```\n   Extraction failures\
    \ (missing ToUnicode, `.notdef` glyphs, glyphs dropped by a reused\n   subset) surface here and nowhere\
    \ else.\n2. **Structural round-trip.** Re-open the saved file and check geometry and content stream:\n\
    \   `page.get_text(\"words\")` for the expected word boxes, `page.get_images()` for expected\n   xrefs,\
    \ `page.get_drawings()` for expected rules, and the page count\n   (`len(doc) == original_pages`).\
    \ Also confirm nothing else moved: compare the full\n   `get_text(\"words\")` list before and after\
    \ and diff it — any coordinate that changed\n   outside the edited region is a regression.\n3. **Visual\
    \ diff.** Render before and after at the same DPI and compare only the intended\n   region, so a pre-existing\
    \ rasterizer difference elsewhere does not mask the result:\n   ```python\n   a = src[0].get_pixmap(dpi=150);\
    \ b = out[0].get_pixmap(dpi=150)\n   # compare clip rects: changed inside the target rect, identical\
    \ outside it\n   ```\n   A pixel diff is the only way to catch overlap, a black patch, a mis-sized\
    \ glyph, or text\n   that the extractor sees but the renderer draws on top of something.\n4. **File-level\
    \ sanity.** Confirm it is a valid, non-corrupt PDF: `fitz.open()` succeeds,\n   `doc.is_pdf` / no\
    \ `is_repaired` warning, `doc.xref_length()` sane, and no errors from\n   `page.get_text()` on *every*\
    \ page (one bad page is often missed). Optionally run a\n   validator (`pikepdf`/`qpdf --check`) if\
    \ the file will be distributed.\n5. **Sizing check.** Confirm the file did not blow up: `os.path.getsize`\
    \ before/after, and\n   that `garbage=4, deflate=True` were used.\n6. **Spot-check the specific risk\
    \ of the edit type**: after a font-embedded insert, confirm the\n   new text is *searchable* and *extractable*\
    \ (not just drawn); after an image insert,\n   confirm `get_images()` shows one new xref and no unintended\
    \ growth; after a redaction,\n   confirm the removed content is **absent from the raw file** (`raw\
    \ = open(path,'rb').read()`,\n   then search for the removed string / old image bytes) and not merely\
    \ painted over.\n7. **Assert in the script, not by eye.** Turn the checks into explicit assertions\
    \ so the\n   script fails loudly, and record the before/after evidence alongside the output file.\n\
    8. **Finally, look at it.** Open the rendered pages (a contact sheet of all pages) — human\n   review\
    \ catches the class of problems no assertion anticipates.\n"
- id: pdf-verify-02
  answer: "No. The row is gone from the *page*, but it is still in the *file*.\n\n`doc.save(\"same.pdf\"\
    , incremental=True)` appends the modified page and the new object\ngenerations to the end of the **existing\
    \ file**, and the original bytes stay physically\npresent as the earlier revision. The previous version\
    \ of the page content stream — which\ncontains the confidential row's text operators — is still in\
    \ the byte stream. So:\n\n- `pdftotext`, `strings`, a hex search, or any tool reading earlier revisions\
    \ can still\n  recover the row. Reopening the PDF shows the redacted page, but that is just the *current*\n\
    \  revision, not the file's history.\n- This is the classic \"redaction failed\" data leak. A redacted\
    \ PDF must be produced by\n  **rewriting** the file, never by incrementally updating the original.\n\
    \n**Correct procedure**\n\n```python\ndoc = fitz.open(\"confidential.pdf\")          # or open first,\
    \ then save to a NEW path\npage = doc[0]\npage.add_redact_annot(rect, fill=(1, 1, 1))\npage.apply_redactions(images=fitz.PDF_REDACT_IMAGE_PIXELS,\n\
    \                      graphics=fitz.PDF_REDACT_LINE_ART_REMOVE_IF_TOUCHED,\n                    \
    \  text=fitz.PDF_REDACT_TEXT_REMOVE)\ndoc.save(\"clean.pdf\", garbage=4, deflate=True, clean=True)\
    \   # full rewrite, no incremental\n```\n\nEven a full rewrite can leave the removed text in the file\
    \ if the redaction rect was too\nsmall to cover the glyphs, so **verify on the actual bytes**:\n\n\
    ```python\ndata = open(\"clean.pdf\", \"rb\").read()\nassert b\"Confidential\" not in data       \
    \     # and any other sensitive strings/names\ntxt = fitz.open(\"clean.pdf\")[0].get_text()\nassert\
    \ \"Confidential\" not in txt\n```\n\nAlso check for the old *image* bytes (if the row contained a\
    \ scan or stamp) and for any\nincremental-update sections left over (`/Prev` keys, which indicate\
    \ incremental saves). If the\nconfidential material ever existed in that file, the safest move is\
    \ to treat the *original* as\ncompromised: purge it from backups, do not ship the original, and re-issue\
    \ from a clean\nsource. And remember redaction must be applied to *all* pages and *all* occurrences\n\
    (including annotations, form fields, embedded files, and metadata/XMP) — one unredacted\nheader or\
    \ comment is enough.\n"
- id: pdf-verify-03
  answer: "**Never modify the user's file in place.**\n\n- **Open read-only-ish and always save to a new\
    \ path.** `doc = fitz.open(\"invoice.pdf\")` then\n  `doc.save(\"invoice_edited.pdf\")`. Do not use\
    \ the original filename. Guard against the\n  footgun of `doc.save(p)` where `p` is the open file's\
    \ own path.\n- **Keep a pristine copy.** Before touching anything, copy the original to a timestamped\n\
    \  backup (`invoice.pdf.orig-<date>`), or compute a checksum (`hashlib.sha256`) of the input and\n\
    \  record it so you can prove the source was unchanged. If the user supplied the file, never\n  write\
    \ to its directory.\n- **Work on a copy, and use a deterministic output name** so re-runs are idempotent\
    \ and\n  reviewable.\n- **Never save incrementally over the source.** `incremental=True` on the original\
    \ keeps the\n  pre-edit content in the file — for a confidential redaction that is a leak, and it\
    \ also\n  accumulates revisions. Use `garbage=4, deflate=True` for a full clean rewrite.\n- **Sanitize\
    \ on save.** `clean=True` (or `doc.scrub()`) to remove embedded JavaScript,\n  launch actions, and\
    \ metadata; `doc.delete_embfile_names()` / `doc.embfile_count()` check for\n  attachments you may\
    \ be unintentionally distributing; `doc.set_metadata({})` to clear\n  author/producer fields if confidentiality\
    \ matters. Remove or disable annotations and form\n  fields you did not intend to keep.\n- **Respect\
    \ encryption and permissions.** If the PDF is password-protected, do not strip\n  encryption to make\
    \ your life easier; report that you cannot process it and ask the user for\n  a decrypted copy or\
    \ the password.\n- **Do not invent data.** Never fabricate amounts, dates, tax IDs, or line items.\
    \ If a required\n  field is illegible or missing, leave it unchanged and flag it to the user. Editing\
    \ a\n  financial document's figures without an explicit instruction is a serious error.\n- **Change\
    \ the minimum.** Restrict redactions and edits to the exact rects requested; verify\n  the untouched\
    \ regions are byte-for-byte visually identical (raster diff) so you can show the\n  user nothing else\
    \ moved.\n- **Write to a scratch/working directory** rather than next to the source, use an atomic\n\
    \  rename (write to `.tmp`, then `os.replace`) so a crash never leaves a half-written file, and\n\
    \  do not overwrite an existing output without confirming (or version it: `-v2`).\n- **Document and\
    \ hand back clearly**: name the input, the output, what you changed, what you\n  deliberately did\
    \ not change, and the verification evidence (page count, text diff, raster\n  diff, file size). Get\
    \ explicit confirmation before overwriting or replacing anything the user\n  already has.\n"
- id: pdf-verify-04
  answer: "\"No exception\" only tells you the *code ran*, not that it did the *right thing*. Every failure\n\
    mode below is silent:\n\n- **A no-op edit.** Your search string never matched, so the replace loop\
    \ did nothing and\n  reported success. Very common: wrong case, a soft hyphen or ligature, a different\
    \ Unicode\n  normal form, the text split across spans.\n- **The new text was dropped or mapped to\
    \ `.notdef`.** `insert_text` with a subset embedded\n  font cannot render characters outside the font's\
    \ cmap. No error; the glyph is blank or a box.\n- **It drew, but is invisible.** Appended content\
    \ can be painted over by later content,\n  clipped by a transparency group, or hidden behind a filled\
    \ background. `get_images()` and\n  `get_image_info()` can even return nothing for vector logos (see\
    \ `pdf-imgrep-03`).\n- **You proved nothing about the file.** The script may have saved to the wrong\
    \ path, overwrote\n  nothing, or written a file the user never opens. `doc.save()` returning is not\
    \ a check.\n- **Geometry is wrong.** Text is off-position, a few points out of its cell, overlapping\
    \ a\n  neighbouring column, or a size/weight mismatch that only shows in a raster.\n- **Collateral\
    \ damage.** Page count changed, content reflowed, fonts got substituted, the\n  file grew by 20× because\
    \ the image was duplicated per page, or the edit broke the content\n  stream (`q`/`Q` imbalance) and\
    \ a viewer now drops part of the page.\n- **Redaction that only hides.** Content painted over but\
    \ still present in the bytes — the\n  file \"looks\" correct and silently leaks.\n- **No baseline.**\
    \ Without a before-state you cannot tell whether a diff is your change or\n  pre-existing.\n- **The\
    \ wrong target.** You edited the page/document you assumed, not the one asked about.\n- **\"done\"\
    \ is just a `print`.** It reflects your intent, not the file's state. It cannot fail\n  when the assertion\
    \ it should have been never ran.\n\n**Fix it by making the script assert on the output artefact, not\
    \ on its own execution:**\n\n```python\nimport fitz, os\nsrc, out = \"invoice.pdf\", \"invoice_edited.pdf\"\
    \nbefore_text = fitz.open(src)[0].get_text()\ndoc = fitz.open(src)\n# ... the edit ...\ndoc.save(out,\
    \ garbage=4, deflate=True)\n\nchk = fitz.open(out)\nassert chk.page_count == doc.page_count, \"page\
    \ count changed\"\nt = chk[0].get_text()\nassert \"NEW VALUE\" in t, \"edit not present in extracted\
    \ text\"\nassert \"OLD VALUE\" not in t, \"old value still present\"\n# unchanged regions: compare\
    \ words outside the edited rect\na = sorted(w for w in doc[0].get_text(\"words\") if not in_rect(w,\
    \ target_rect))\nb = sorted(w for w in chk[0].get_text(\"words\") if not in_rect(w, target_rect))\n\
    assert a == b, \"content outside the edit moved\"\n# visual check on the edited region, and a file-size\
    \ sanity check\nassert chk[0].get_pixmap(clip=target_rect, dpi=150).n > 0\nprint(\"verified\", os.path.getsize(out))\n\
    ```\n\nAnd then look at the rendered pages yourself. Assertions turn a hopeful script into a\nverifiable\
    \ one; without them, \"done\" is a statement of intent, not evidence.\n"
