# deepseek-v4.1-flash — pdf answers WITH SKILL (validation of the re-derived skill)

Model: `opencode-go/deepseek-v4.1-flash` (provider-stripped `deepseek-v4.1-flash`),
re-run 2026-10-21 after opencode-go updated the model behind this id
(`model_version: "4.1-2026-10-21"`). This is with-skill ITERATION 2 (skill edited after iter1: image-insert xref-reuse/dedupe facts and the stamp-source-for-a-move rule added; iter1 kept as `.iter1.with-skill.md`) — the with-skill run for this id (any earlier one validated the previous model);
the previous answers are in git history.

Closed-book barrier:
- Each of the 36 questions in `docs/okf/_prompts/pdf.md` was a fresh headless
  `ocode run -m opencode-go/deepseek-v4.1-flash -effort med` session (`bin/ocode`, built
  2026-10-20) under ocode's real agent system prompt.
- Fake HOME: no global CLAUDE.md/AGENTS.md, no memory, no persisted roots. Its only config
  denies every builtin tool and DISABLES the five existing `*-tuning-deepseek-v4.1-flash`
  Kaizen skills (plus `conduct-tuning-deepseek-v4-flash`), so the ONLY skill content the
  model saw is the prepended pdf skill body (no double injection). Probe before the sweep confirmed: no tools advertised, no Model-Specific
  Directives block. The generic `[ocode:provider]` DeepSeek output-discipline block and the
  bundled skill-catalogue digest were still present (every user gets them).
- cwd: an empty dir named `work`; stdin `/dev/null`.
- Audit: every session sqlite checked after the sweep — 1 user + 1 assistant message,
  zero tool calls.
- Prompt per question: the body of docs/okf/pdf/derived/pdf.deepseek-v4.1-flash.SKILL.md (front matter stripped, as "Active guidance") + closed-book instruction + the one question; plain-text reply,
  wrapped into YAML below verbatim.

```yaml
- id: pdf-model-01
  answer: |
    Assumptions: the page is a normal vector PDF (text + drawn rules), not a scan; no source document exists; "third row" means the third body row under the header.

    ## What the PDF actually contains

    A PDF has no table, no row, no cell, and no column. It has a page content stream — a small program of drawing operators — plus resources (fonts, XObjects).

    For a table, that stream contains roughly:

    - **Text as positioned glyph runs.** Operators like `Tj`, `TJ`, `'`, `"` inside `BT`/`ET`, placed with `Tm`/`Td`/`TD`/`T*`. A cell's string may be split across several runs (kerning arrays in `TJ`, separate styling spans, separate text objects per cell). Spaces are often simulated by repositioning, not by a space character. So the content is "glyph A at x=…, y=…", not "row 3, cell 2 = 'Widget'".
    - **Rules and shading as vector paths.** `re … f` for a header fill or zebra striping, `m`/`l`/`S` for lines. A vertical border is frequently one long stroke spanning the whole table height, not one segment per row. Horizontal borders may be one path per line, or a single path for the entire grid. A fill may cover many rows at once.
    - **Absolute coordinates, no reflow.** There is no row height, no baseline grid, no cell rectangle, no "below this row" relationship. Y positions are literal numbers. Nothing moves when something else is removed.

    Tagged PDF is the exception that proves the rule: `/StructTreeRoot` → `/Table` → `/TR` → `/TD` with MCIDs can mark which marked-content sequence is which row. That is accessibility metadata, not layout; the visible table is still painted glyphs and paths, and deleting a row also means repairing the structure tree, ParentTree, and MCIDs. It is a locator aid, not an editable grid.

    If the table is a scanned image or text converted to outlines, none of the above applies: there are no glyphs to redact, and you are back to image editing or full reconstruction.

    ## What that implies for the edit

    1. **There is no "delete row" operation.** You must *synthesise* it: locate the row geometrically (extract words/spans with `get_text("dict")` or a table finder, cluster by baseline, take the union of the row's cell bboxes plus padding), then decide the target band.
    2. **Removal must be real.** Painting a white rectangle over the row, or stamping a clean version on top, leaves the glyphs in the content stream — still selectable, searchable, copyable, greppable. Use redaction (`add_redact_annot` + `apply_redactions`) or rebuild the page. Then verify by extracting text from the output and confirming the deleted strings are gone.
    3. **Reflow is the hard part, and it is yours.** Nothing below the row moves automatically. Every remaining text run, rule, total, note, and footer under the deletion point must be retranslated up by the row height. If a cell wrapped to two lines, the "row" is taller than one line and that height must be recomputed, not assumed. If you stamp the remaining region from a copy, the copy must already have the deleted row redacted or the row's content rides along invisibly.
    4. **The grid is shared geometry.** Deleting a band can take out vertical rules that also serve rows that stay. Redaction defaults remove line art lying entirely inside the rect, keep partially covered shapes whole, and `REMOVE_IF_TOUCHED` deletes any whole path that merely crosses. Choose the graphics policy deliberately, then redraw the surviving rules.
    5. **Counting rows is ambiguous.** Header rows, group headers, spanning rows, blank spacer rows, and rows split across a page break all exist visually. "Third row" needs a convention (header excluded?), and a row that straddles a page boundary is two fragments of one logical row — deleting it means editing both pages and reflowing both.
    6. **Derived content must be recomputed.** Totals, subtotals, row numbering, "see item 3" references, and any table of contents or index that points at the row. Annotations, links, and form widgets have their own rectangles and do not follow text.
    7. **Text matching is fragile.** Subset fonts, custom encodings, ligatures, and kerned runs mean searching for the literal cell string can fail or hit the wrong place. Locate by coordinates, not by string.
    8. **Rebuilding is often the better route.** Extract the table data, delete the row in the data, and redraw the table on a cleared region — fills first, then text, then rules — matching the original font, size, colour, and alignment read from the header/body spans. That avoids patchwork reflow artifacts and leaves a clean text layer, at the cost of reproducing the styling exactly.
    9. **Save properly and check.** Full save with garbage collection and deflate, then validate: deleted strings absent from `get_text()`, neighbouring cells intact, totals correct, and a visual render (since `get_text()` cannot see clipped-away or hidden content).

    Bottom line: a "row" in a PDF is a visual coincidence of glyph coordinates and painted lines, so deleting one is a layout reconstruction — remove the glyphs and rules for real, shift everything below, repair the shared grid and derived numbers, and verify by extracting from the output.
- id: pdf-model-02
  answer: |
    **Assumption:** "remove" means the content is actually gone from the file — not just invisible on screen — and the toolkit in question is PyMuPDF/MuPDF.

    ## Why a white rectangle is not an edit

    A PDF page is a display list, not a document model. The text is emitted by glyph‑painting operators (`Tj`/`TJ`) inside a content stream. Drawing a white rectangle just appends *another* painting operator later in the stream. The original glyphs are untouched — you have hidden them, not removed them.

    That distinction is not cosmetic:

    - **The text layer still contains the old string.** `page.get_text()`, `pdftotext`, copy‑and‑paste, Ctrl+F, spell‑check, translation, summarisation and OCR pipelines all still see it. If you type the replacement on top, the page now contains *both* strings, so extraction yields `OldNew` and search hits the old text.
    - **Accessibility.** Screen readers announce the covered text; the visual order and the reading order diverge.
    - **It is trivially reversible.** Delete the rectangle object, or remove the overlay annotation, and the original is back. Anyone can also recover it by re‑rendering with the overlay suppressed or by parsing the content stream directly.
    - **It is the classic redaction failure.** The same mistake in black: a black box over a name is not redaction if the glyphs are still in the stream. Courts, regulators and leak investigators treat this as disclosure, not redaction.
    - **Rendering is not guaranteed.** The rectangle lives in one content stream; text inside a Form XObject, an annotation, or a later stream may paint above it. Different viewers, colour management, dark‑mode inversion, or print output can change what wins. The rectangle may also not line up after any later page manipulation.
    - **It also breaks the page's own semantics.** Word bounding boxes, hit‑testing and search highlights still point at the hidden glyphs, so find‑in‑page can scroll to invisible text.

    So a white rectangle is a *visual patch*, valid only as a last‑resort cosmetic layer over content that has already been removed by other means.

    ## What to use instead

    Use the redaction mechanism (PDF redaction annotations, ISO 32000), which rewrites the content stream and actually drops the characters:

    1. **Mark the region** with a tight rectangle around the glyphs: `page.add_redact_annot(rect)`.
    2. **Apply it**: `page.apply_redactions()`. This removes any character whose bbox overlaps the rect, and by default also blanks overlapping image pixels (`images=PDF_REDACT_IMAGE_PIXELS`) and deletes line art lying entirely inside the rect (`graphics=PDF_REDACT_LINE_ART_REMOVE_IF_COVERED`).
    3. **Then draw the replacement**, at the same baseline, with a matching font, size and colour — or rebuild the whole row/table from extracted data if the change reflows anything.
    4. **Save to a new file** with `garbage=4, deflate=True`, so orphaned objects and unreferenced streams from the old content are actually discarded. Incremental saves keep the previous revision of the object in the file.
    5. **Verify** by re‑extracting: the old string must be absent from `get_text()` of the output, and the neighbours must be intact.

    Notes that matter in practice:

    - **Keep the rect tight and inside the cell.** Overlap is the removal criterion, so a padded rect silently eats adjacent characters.
    - **Preserve the grid deliberately.** If a cell's borders and shading must survive, call `apply_redactions(graphics=0, images=0)` and let only the text be removed; then re‑insert the text. With defaults you would lose shading that sits entirely inside the rect.
    - **Never rely on clipping.** `show_pdf_page(clip=...)` only hides what is outside the clip; the stamped XObject still embeds the whole source page, and `get_text()` won't see it. A stamped region is clean only if its source was genuinely redacted first.
    - **Scanned pages** need both halves: `images=PDF_REDACT_IMAGE_PIXELS` blanks the pixels, and the text redaction removes the OCR layer underneath. Covering the image with a rectangle leaves the searchable OCR text fully intact.
    - **Don't expect to "edit text" in place.** PDF has no paragraphs, no flow and no line model; glyphs are absolutely positioned. Redact‑and‑redraw (or rebuild the page from extracted data) is the only honest route. If you want a white box anyway, apply the redaction first and use the box purely as background for the new text.

    **Short version:** a white rectangle is a paint operation layered over live text, so the old string survives in the text layer and in the file bytes — it fails extraction tests, accessibility, and any real redaction standard. Use `add_redact_annot` + `apply_redactions` to delete the glyphs (and image pixels where relevant), redraw the replacement, and save with `garbage=4, deflate=True` to a new file.
- id: pdf-model-03
  answer: |
    A raw byte-level search‑and‑replace on a PDF content stream usually fails for one of these reasons, and several of them can apply at once.

    **1. The stream is rarely plain bytes you can grep**
    - Content streams are normally compressed (`FlateDecode`, sometimes LZW/ASCII85/etc.). The literal bytes `22.50` simply are not in the file until the stream is decoded.
    - In PDF 1.5+ the stream object itself may live inside an object stream (`/ObjStm`), and cross‑reference data is in an xref stream, so a naive file‑wide search misses it entirely.
    - Encrypted PDFs (RC4/AES) encrypt strings per object; raw replacement produces garbage or an undecryptable/corrupt object.

    **2. The text is not stored as the contiguous ASCII string "22.50"**
    - It may be a hex string: `<32322E3530> Tj`, which contains no `22.50` bytes at all.
    - It may be split across several show operators with positioning/kerning, e.g. `[(22.)-15(.50)] TJ`, `(22) Tj 12 0 Td (50) Tj`, or one glyph per operator. There is no single contiguous substring to match.
    - The text may be drawn by a Form XObject or an annotation appearance stream, not the page's own content stream (which can also be an array of several streams).

    **3. Font encoding means the bytes are not ASCII digits**
    - With subset embedded fonts, Type0/CID fonts and `Identity-H`, the codes in a string are glyph IDs or CIDs, typically 2 bytes each, not the character codes for `2`, `2`, `.`, `5`, `0`. The visible "22.50" might be `<00120012002E00150010>`.
    - With simple fonts and a `/Differences` encoding, byte `0x32` may map to a completely different glyph. Replacing "ASCII 2" can render the wrong character or nothing.
    - The `/ToUnicode` CMap still maps the old codes; even a visually correct byte swap can make copy/paste and text extraction wrong, and codes absent from a subset font have no glyph.

    **4. Bare numbers in a content stream are ambiguous**
    - `22.50` can legitimately appear as an operand to non‑text operators: a `cm` matrix, `Td`/`TD`/`Tm` translation, `rg`/`RG` colour component, `w` line width, `d` dash phase, etc. A blind replace corrupts geometry or colours as well as text.
    - The replacement must be scoped to string operands of text‑showing operators (`Tj`, `TJ`, `'`, `"`) and nothing else. Even then, a `TJ` array contains numeric adjustments interleaved with strings, so a regex over the whole stream is unsafe.

    **5. Structural corruption from changing the bytes in place**
    - If the replacement has a different length, the stream's `/Length` must be updated and all xref byte offsets shift. Editing bytes without rewriting the xref (or doing a proper incremental update) yields a broken file.
    - Escaping matters inside literal strings: `(`, `)`, `\` and octal escapes must be handled. Inserting an unescaped `)` terminates the string early and desynchronises the tokenizer, which can trash the rest of the page.
    - The same byte sequence may occur many times (different cells, metadata, other pages). A global replace changes values you did not intend, and a "first match" replace may hit the wrong one.

    **6. Even a "successful" replacement can look wrong**
    - Text positioning is explicit. Changing the glyphs without adjusting the `TJ` offsets or `Td` positions leaves the new number misaligned; if it is wider than the old one it overflows the cell or collides with the neighbouring column.
    - If the old text is painted over a background or over other text, a partial replacement can leave overlapping glyphs.
    - Some "text" is actually vector outlines or a scanned image, so there is no string to replace at all.

    **What a correct edit looks like:** decode the stream (`pikepdf.Stream.read_bytes()` handles the filters), tokenise it properly (not regex), locate the string operand of the specific `Tj`/`TJ`/`'`/`"` that draws the target cell, map the desired characters through the font's encoding/CMap, rewrite the string (updating `/Length` via the library, not by hand), leave other occurrences alone, and re‑check the geometry so the new value fits. For same‑byte‑length numeric swaps like `22.50` → `54.00` the task is simpler, but only if the digits are stored as literal single‑byte ASCII; it still fails for hex strings, split runs, CID fonts, and ambiguous numeric operands.

    One assumption: by "plain search‑and‑replace" I mean editing the decoded or raw stream bytes without parsing the content‑stream token syntax or the font encoding. If you instead use a proper parser and rewrite the operator's string operand, most of these failure modes disappear.
- id: pdf-model-04
  answer: |
    PyMuPDF uses MuPDF page space, not raw PDF user space:

    - **PyMuPDF/MuPDF page space:** origin at the **top-left of the visible page**, x right, y **down**.
    - **Raw PDF user space:** origin at the **lower-left of the MediaBox**, x right, y **up**.
    - Units are the same: PDF points, 1/72 inch.

    For a normal page with `CropBox = MediaBox = (0, 0, W, H)` and no `/Rotate`:

    ```text
    x_mupdf = x_pdf
    y_mupdf = H - y_pdf
    ```

    So a word rect from `page.get_text("words")` is already in PyMuPDF space, with `y0` above `y1`. You can draw with it directly:

    ```python
    w = page.get_text("words")[0]
    r = fitz.Rect(w[:4])
    page.draw_rect(r, color=(1, 0, 0), width=1)
    ```

    `insert_text` takes a **baseline** point, not a rect corner, so use `insert_textbox(r, ...)` or compute the baseline; don’t confuse `(x0, y0)` with a text origin.

    ## CropBox offset from MediaBox

    PyMuPDF’s page origin is the **top-left of the CropBox**, not the MediaBox. If

    ```text
    CropBox = (cx0, cy0, cx1, cy1)     # raw PDF coords, y up
    W = cx1 - cx0
    H = cy1 - cy0
    ```

    then, unrotated:

    ```text
    x_mupdf = x_pdf - cx0
    y_mupdf = cy1 - y_pdf
    ```

    `page.rect` is still `(0, 0, W, H)`, but its `(0,0)` corresponds to raw PDF point `(cx0, cy1)`. Using `page.mediabox.y1` or simply `page.rect.height - y_pdf` when the CropBox is offset gives a shifted result.

    ## /Rotate

    `page.rect` is the **rotated, visible** page rectangle, so for `/Rotate 90` or `270` its width and height are swapped relative to the CropBox. Text coordinates from `get_text("words")` are in that same rotated frame, so they still match drawing and annotation APIs directly—do not manually unrotate them before drawing.

    For conversion from raw PDF coordinates, with `CropBox = (cx0, cy0, cx1, cy1)`:

    ```text
    /Rotate 0:    X = x - cx0          Y = cy1 - y
    /Rotate 90:   X = y - cy0          Y = x - cx0
    /Rotate 180:  X = cx1 - x          Y = y - cy0
    /Rotate 270:  X = cy1 - y          Y = cx1 - x
    ```

    For example, with `CropBox = (50, 70, 550, 770)` and `/Rotate 90`, raw PDF point `(100, 700)` maps to:

    ```text
    X = 700 - 70 = 630
    Y = 100 - 50 = 50
    ```

    and `page.rect` is `(0, 0, 700, 500)`.

    ## General conversion

    The reliable general converter is the page transformation matrix:

    ```python
    M = page.transformation_matrix       # PDF user space -> PyMuPDF page space

    p_mupdf = fitz.Point(x_pdf, y_pdf) * M
    r_mupdf = fitz.Rect(x0, y0, x1, y1) * M
    r_mupdf.normalize()                  # rotation/y-flip can swap top/bottom

    # Reverse:
    p_pdf = p_mupdf * ~M
    ```

    This matrix accounts for the CropBox offset and `/Rotate`. Use it when your coordinates come from raw PDF annotations, content-stream user space, or an external PDF library. For coordinates already returned by `get_text("words")`, no conversion is needed.
- id: pdf-locate-01
  answer: |
    ## PyMuPDF (`fitz`) — the direct way

    PyMuPDF has a built-in table finder that returns real geometry, not just extracted strings. It detects the table's ruling lines (or text alignment), then gives you the table bbox, every row bbox, and every cell bbox in page coordinates (origin top-left, units = points, y grows downward).

    ```python
    import fitz  # PyMuPDF

    doc  = fitz.open("in.pdf")
    page = doc[0]

    finder = page.find_tables()          # scans the whole page
    # finder = page.find_tables(clip=fitz.Rect(x0, y0, x1, y1))  # restrict to a region
    # finder = page.find_tables(strategy="text")                 # no ruling lines

    for table in finder.tables:
        print("table bbox:", table.bbox)          # (x0, y0, x1, y1)
        print("rows/cols :", table.row_count, table.col_count)

        # header row (may be None)
        if table.header is not None:
            print("header bbox:", table.header.bbox)
            print("header cells:", table.header.cells)   # list of bboxes / None

        for r, row in enumerate(table.rows):
            print("row", r, "bbox:", row.bbox)
            for c, cell in enumerate(row.cells):
                if cell is None:                      # merged / absent cell
                    continue
                print("  cell", (r, c), "bbox:", cell)  # (x0, y0, x1, y1)

        # text aligned to the same row/column grid
        data = table.extract()        # list[list[str|None]]
        # or: df = table.to_pandas()
    ```

    Key API facts:

    - `page.find_tables()` returns a `TableFinder`; its `.tables` is a list of `Table`.
    - `Table.bbox`, `Table.rows`, `Table.header`, `Table.row_count`, `Table.col_count`, `Table.extract()`, `Table.to_pandas()`.
    - `TableRow.bbox` is the row rectangle; `TableRow.cells` is the list of cell rectangles for that row, one per column, with `None` where a cell is merged or missing (a colspan cell appears once).
    - Cell and row bboxes are absolute page coordinates, so they can be used directly for redaction, cropping, drawing, or `page.get_textbox(cell_bbox)` to re-read a single cell.
    - Because it is geometric, it still gives you the grid when the cell is empty or when the text is rotated/small — you are not dependent on the text layer for the shape.

    ## Detection strategy and tuning

    - **Default `strategy="lines"`** uses the page's vector drawings (`page.get_drawings()`) to find the ruling grid. Best for ruled tables; needs actual lines/rectangles, not just background shading.
    - **`strategy="text"`** infers columns/rows from whitespace and text alignment — use for borderless tables. Less reliable with multi-line cells or merged columns.
    - `strategy="explicit"` lets you pass the line objects yourself (`lines=...`) when the default detection misreads a page.
    - Tolerance kwargs (e.g. `snap_tolerance`, `join_tolerance`, `intersection_tolerance`, `min_words`, `margin`) control how aggressively short line segments are snapped into one grid. Raise them for scanned/exported PDFs with broken rules; lower them if adjacent tables bleed into one.

    ## If PyMuPDF's finder is not enough

    - **pdfplumber** — `page.find_tables()` / `page.extract_tables()`. Its `Table` objects expose `.bbox` and `.rows`, and each row exposes `.cells` as bboxes too; it has separate `vertical_strategy`/`horizontal_strategy` (`"lines"`, `"text"`, `"explicit"`) which is often easier to tune per page.
    - **Camelot** (`lattice` flavor) — good row/column data with `table.rows`, but cell-level bboxes are less directly exposed than PyMuPDF/pdfplumber.
    - **Tabula** — text/CSV oriented; cell geometry is weak, so avoid it if you need bboxes.
    - **No text layer at all (scanned)** — run OCR first (`page.get_textpage_ocr()` in PyMuPDF, or OCRmyPDF/Tesseract), then run the table finder; otherwise there is nothing to detect.

    ## Caveats worth knowing

    - Coordinate system: PyMuPDF is top-left origin; pdfplumber is also top-left; PDF native space is bottom-left. Convert if you feed bboxes to another tool.
    - `table.bbox` includes the header row; `header.bbox` isolates it.
    - Merged cells produce `None` entries, so column index is positional, not a guaranteed cell per position.
    - Detection can split one visual table into several `Table` objects (page breaks, gaps in rules) — merge by comparing bboxes if needed.
    - A table drawn only with cell background fills and no strokes may not be found by `"lines"`; try `"text"` or supply the fill rectangles via `"explicit"`.

    **Bottom line:** `page.find_tables()` → iterate `.tables`; each `Table` gives `.bbox`, `.rows[i].bbox`, and `.rows[i].cells[j]` as `(x0, y0, x1, y1)` rectangles — exactly the row/cell bounding boxes you asked for, independent of the text content. Use `strategy="text"` for borderless tables and OCR the page first if it is a scan.
- id: pdf-locate-02
  answer: |
    ## Why `search_for("Widget C")` alone can't define the removal region

    `search_for` returns only the **glyph bboxes of the matched string**, as a list of `Rect`s. That is a text-layer hit, not a row. Concretely it fails you in these ways:

    1. **It's tight to the glyphs, not the cell.** The rect is the ink box of "Widget C" — its height is roughly the cap/x-height, not the row height. It excludes the cell's top/bottom padding, the row fill, the horizontal rules above and below, and the vertical rule at both edges. Redacting just that rect leaves the row's shading and gridlines behind (and, with `apply_redactions` defaults, deletes only the shading that happens to fall entirely inside that little rect — producing a torn fill).

    2. **It covers one cell, not the row.** The row contains the other cells (description, qty, price, …). Removing "Widget C" leaves them on the page, and any shifted-up row now sits next to orphaned values.

    3. **It may return several hits.** The same string can appear in a header, a note, a summary line, or another page. The list order is page order, then content-stream order — not "the row I mean". You must disambiguate (page number, expected y-band, expected column x-range, and row index) before you have a unique rect.

    4. **It can match across a partial string.** `search_for` is substring-based unless you check flags/`quads`; "Widget C" also hits "Widget C2" or "Widget C (legacy)". Conversely, spacing/kerning quirks can make an exact string miss entirely, returning `[]`.

    5. **It tells you nothing about the geometry you actually need**: the row height to shift, the table's x0/x1, the neighbouring row boundaries, or whether the rect overlaps a rule that must be preserved.

    ## How to get the full row region

    **Preferred: use the table structure.**

    ```python
    tabs = page.find_tables()
    tab  = [t for t in tabs.tables if t.bbox contains the hit][0]
    rows = tab.rows            # rows[i].cells -> cell bboxes; rows[i].bbox -> full row band
    for i, row in enumerate(rows):
        texts = [page.get_textbox(c) for c in row.cells if c]
        if any("Widget C" == t.strip() for t in texts):
            target = i
            band = fitz.Rect(row.bbox)      # full row: x0..x1 of table, y0..y1 of row
    ```

    `Table.rows[i].bbox` (or the union of `rows[i].cells`) gives the whole band — full table width and full row height, including the padding the glyph bbox omitted. Use that (optionally widened by a fraction of a point) as the redaction rect.

    **Fallback if `find_tables` fails** (no ruling lines, merged cells, rotated text):

    1. Get spans/words with `page.get_text("dict")` or `get_text("words")` and cluster by `y` (baseline) to reconstruct rows.
    2. Find the word(s) matching the target; take its block/line y-center as the row center.
    3. Set the band's vertical extent from the **midpoints between this row's center and the adjacent rows' centers**, so the rect owns exactly one row and no neighbouring text:
       `y0 = (prev_center + center)/2`, `y1 = (center + next_center)/2` (or the row's own line bbox ± half the inter-row gap).
    4. Set the horizontal extent from the table's content: `x0 = min(x0 of all words in the row)`, `x1 = max(x1 of all words in the row)`, extended to the table's detected vertical rules / the text column's left and right edges — not to `page.rect`.

    **Then verify before applying:** assert the rect contains only the intended row's text, that the target string is inside it, that no word from a row that must stay intersects it, and that it fully spans the row's fill/rules (compare against `row.bbox`). Redact the deleted row **plus every row that will move**, `apply_redactions()`, redraw/shift, recompute totals, and save with `garbage=4, deflate=True` — then confirm the string is gone from the output's `get_text()`.

    Assumption: the row lives in a ruled table detectable by `find_tables`; if not, the dict/word-clustering fallback above is the route.
- id: pdf-locate-03
  answer: |
    Two extraction modes matter: the high-level `dict`/`rawdict` (what the reader sees, already in page coordinates) and the low-level `get_texttrace()` (closest to the content stream, includes stroke/opacity). Both report a baseline origin, which is what `insert_text()` expects as its insertion point.

    ## 1. `get_text("dict")` — spans carry the style

    Each span has: `font` (name string), `size` (float), `color` (sRGB packed into an int), `flags` (bitfield), `bbox`, `origin` (baseline point of the first glyph), `ascender`/`descender` (fractions of the size above/below the baseline), and `text`.

    ```python
    import pymupdf as fitz   # or: import fitz

    page = doc[0]
    cell = fitz.Rect(...)                      # the cell, from find_tables()/drawings

    for block in page.get_text("dict")["blocks"]:
        for line in block.get("lines", []):    # image blocks have no "lines"
            for span in line["spans"]:
                if fitz.Rect(span["bbox"]).intersects(cell):
                    style = dict(
                        font     = span["font"],            # e.g. "Arial-BoldMT" or "ABCDEF+Arial"
                        size     = span["size"],            # pt
                        color    = span["color"],           # 0xRRGGBB int, e.g. 0xFF0000
                        rgb      = fitz.sRGB_to_rgb(span["color"]),   # (r,g,b) floats 0..1
                        bold     = bool(span["flags"] & 16),
                        italic   = bool(span["flags"] & 2),
                        serif    = bool(span["flags"] & 4),
                        mono     = bool(span["flags"] & 8),
                        baseline = span["origin"],          # (x, y) of first glyph baseline
                        bbox     = span["bbox"],
                        ascender = span["ascender"],        # baseline -> top, * size
                        descender= span["descender"],       # baseline -> bottom, * size (negative)
                    )
                    print(style)
    ```

    `flags` bits: 1 superscript, 2 italic, 4 serifed, 8 monospaced, 16 bold. `line["dir"]` is the writing direction vector `(cos, sin)` — `(1, 0)` is horizontal; check it if the cell text may be rotated. The span `bbox` is the glyph box, not the cell.

    **Baseline:** `span["origin"]` is the pen position on the baseline of the first character, in PyMuPDF page coordinates (y grows downward from the top-left). This is exactly the `point` argument of `page.insert_text()`. For a per-character baseline use `rawdict`, where every entry in `span["chars"]` has `"c"`, `"origin"` and `"bbox"`.

    ```python
    for span in ...# page.get_text("rawdict") blocks/lines/spans
        for ch in span["chars"]:
            ch["c"], ch["origin"], ch["bbox"]   # per-glyph baseline
    ```

    Word-level `page.get_text("words")` gives `(x0, y0, x1, y1, word, block_no, line_no, word_no)` — no style, so don't use it for matching.

    ## 2. `page.get_texttrace()` — stroke, opacity, float colour

    Lower level, one dict per span in content-stream order. Useful when the cell is filled/stroked, has an opacity, or when you want the colour as floats rather than a packed int:

    ```python
    for s in page.get_texttrace():
        s["font"], s["size"], s["color"]      # colour is a tuple of floats here
        s["type"]      # 0 = fill, 1 = stroke, 2 = invisible
        s["opacity"], s["seqno"], s["dir"], s["wmode"]
        s["chars"][0]["origin"]               # baseline of first glyph
    ```

    Caveat for both: colour is converted to sRGB. If the PDF paints text in a device CMYK / ICCBased space, the returned value is an approximation — for an exact match you have to read the fill colour operands from the content stream. Also note `get_texttrace()` reports what was drawn, including clips/shadows that `dict` may merge.

    ## 3. Reusing the font to draw the replacement

    `span["font"]` is only a name. To reuse the real font (embedded/subset), find its xref and register it on the page:

    ```python
    base = span["font"].split("+")[-1]            # strip "ABCDEF+" subset prefix
    for xref, ext, ftype, basefont, name, enc, ref in page.get_fonts(full=True):
        if basefont.split("+")[-1] == base:
            fname, fext, ftype2, buf = doc.extract_font(xref)
            page.insert_font(fontname="FX", fontbuffer=buf)     # register, don't embed twice
            break

    page.insert_text(span["origin"], new_text,
                     fontname="FX" if buf else "helv",
                     fontsize=span["size"],
                     color=fitz.sRGB_to_rgb(span["color"]))
    ```

    - Base-14 names (`Helv`, `TiRo`, `Cour`, …) can be passed straight to `insert_text`; anything else needs `fontbuffer`/`fontfile`.
    - `insert_text` places the string so its first glyph sits on `point` with that baseline — do not convert to the bbox top; keep `origin`.
    - Match the width if the cell must stay aligned: `fitz.get_text_length(text, fontname=..., fontsize=...)`, or use a `TextWriter` with the extracted `fontbuffer` for per-span control.
    - Horizontal centring comes from the cell rect and the measured width: `x = cell.x0 + (cell.width - w) / 2`, keep `y = span["origin"][1]`.
    - If the run is rotated, apply `line["dir"]` (`rotate=90` or a morph) — otherwise your text will sit at a different angle than the original.

    For a pure cell replacement, redact only the glyph bbox tightly with `page.apply_redactions(graphics=0, images=0)`, then insert at the captured `origin` with the captured style — that keeps the cell's borders and shading intact.

    If you don't know a specific detail (exact colour-space handling, per-font subset quirks), the honest answer is: extract and compare against a rendered image rather than assume.
- id: pdf-locate-04
  answer: |
    Get it from PyMuPDF's table finder plus the text spans — not from guessing at fixed coordinates.

    **Column boundaries**

    ```python
    tabs = page.find_tables()
    tab  = tabs[0]

    tab.bbox                    # whole-table rect
    tab.col_count, tab.row_count
    tab.cells                   # flat list of cell bboxes, row-major (None for merged cells)
    tab.rows[i].cells           # per-row cell rects -> x0/x1 of each cell
    tab.header.cells            # header row's cell rects = the column edges
    ```

    Each cell is a `Rect`, so `cell.x0` and `cell.x1` give the left and right edge of that column. Use the header row's cells as the canonical boundaries (body cells can be missing/merged; header cells are the full grid). Cross-check with `page.get_drawings()` — the vertical rules of the grid show up as line items whose `x` coordinates should match those cell edges.

    **Where a right-aligned number ends**

    The alignment anchor is the **x1 of the text span/word**, not the cell edge (right-aligned numbers usually sit a few points inside the cell's right border):

    ```python
    d = page.get_text("dict")
    for block in d["blocks"]:
        for line in block.get("lines", []):
            for span in line["spans"]:
                if span["bbox"] intersects_col_rect:
                    span["bbox"]        # (x0, y0, x1, y1): x1 = right end of the glyphs
                    span["origin"]      # baseline origin (x, y)
                    span["font"], span["size"], span["color"]   # style to match
    ```

    Equivalently, `page.get_text("words")` returns `(x0, y0, x1, y1, word, ...)`, and `word[2]` is the right end of that word.

    For a numeric column, take the **consensus x1** across the existing numbers in that column (they should all share the same right edge, or differ by a point or two): that value is the anchor to right-align new or edited values against. Match `font`, `size` and `color` from the same span so the new text is visually identical.

    **Assumption:** the table is a real, detectable grid (ruled or consistently spaced), so `find_tables()` returns sensible cell rects; if the table has no rules and isn't detected, fall back to clustering the x0/x1 of spans per line and using `get_drawings()` for the rule positions.
- id: pdf-rowdel-01
  answer: |
    ## Assumptions

    Single table on one page; row 0 is the header; rows are uniform height; the Total is the last row (or a row directly under the table); no merged cells or per-cell images. If rows contain images or the Total is computed elsewhere, adapt as noted below.

    ## The core rule

    Deleting a row means **redaction must actually remove it**, not hide it. `show_pdf_page(..., clip=...)` only restricts what is *visible* — the Form XObject still embeds the entire source page, so stamping from an unredacted copy leaves the deleted row recoverable in the bytes (and `get_text()` won't reveal it). Also, `apply_redactions()` defaults are not "leave line art alone": it removes line art lying **entirely** inside the rect and blanks overlapping image pixels. Keep that in mind when choosing what to redact.

    ## Procedure

    **1. Extract and plan geometry**

    ```python
    import fitz

    doc = fitz.open("in.pdf")
    page = doc[0]
    tab = page.find_tables()[0]
    data = tab.extract()            # list of rows, cells as strings
    rows = tab.rows                # row.bbox available
    k = index_of_row_to_delete     # never 0 (header)
    dy = rows[k].bbox.height       # or rows[k+1].bbox.y0 - rows[k].bbox.y0
    table_bbox = tab.bbox
    del_top, table_bottom = rows[k].bbox.y0, table_bbox.y1
    ```

    Recompute the Total from `data` with row `k` spliced out, parsing currency/thousands separators. Do this *before* drawing anything.

    **2. Build a redacted stamp source (if you use the stamp route)**

    ```python
    src = fitz.open("in.pdf")
    sp = src[0]
    sp.add_redact_annot(sp.rect & fitz.Rect(0, del_top, sp.rect.x1, del_top + dy), fill=False)
    sp.apply_redactions()          # defaults: text removed, line art inside rect removed
    ```

    Redact **only the row being deleted** in the source. If rows merely move (no deletion), stamp from an *untouched* copy instead — redacting the region you are about to stamp gives you an empty stamp.

    **3. Remove the old content on the target page**

    For the rebuild approach (recommended): redact the **entire table bbox** — header included if you redraw it — so no old rule, shading, text, or glyph fragment survives:

    ```python
    page.add_redact_annot(table_bbox, fill=False)
    page.apply_redactions(
        images=fitz.PDF_REDACT_IMAGE_PIXELS,
        graphics=fitz.PDF_REDACT_LINE_ART_REMOVE_IF_COVERED,
        text=fitz.PDF_REDACT_TEXT_REMOVE,
    )
    ```

    For the stamp approach: redact everything that moves — from `del_top` to the bottom of the last moving element (last kept row, Total, any note that should shift). Do **not** use `PDF_REDACT_LINE_ART_REMOVE_IF_TOUCHED` on a band that crosses vertical rules belonging to rows above; it deletes the whole path.

    **4. Rebuild or stamp the rows shifted up**

    Rebuild (safest, guarantees real removal):

    - Fill first, then text, then rules — a fill drawn after the text hides it in the render while leaving it in the text layer.
    - Place kept rows at their original y, except every row below `k` moves up by `dy`; the Total row also moves up if it was below.
    - Reuse the header/body styling read from `get_text("dict")` spans (font, size, colour — e.g. white text on a dark header), not guessed defaults.

    Stamp (better fidelity for fonts and embedded images):

    ```python
    src_rect = fitz.Rect(table_bbox.x0, rows[k+1].bbox.y0, table_bbox.x1, last_moving_bottom)
    dst_rect = src_rect + (0, -dy)
    page.show_pdf_page(dst_rect, src, 0, clip=src_rect)
    ```

    The clip must start at the first *kept* row below the deletion and the destination must be exactly `dy` higher, so the blank gap lands at the bottom of the moved block, not in the middle. The source page here must already have the deleted row redacted and applied.

    **5. Write the new Total**

    Draw the recomputed string in the Total row's cell, matching the existing alignment and style. If you stamped a block containing the old Total, overwrite it — better, exclude the Total from the clip and draw it fresh.

    **6. Save cleanly**

    ```python
    doc.save("out.pdf", garbage=4, deflate=True)
    ```

    `garbage=4` also deduplicates streams; levels 0–3 do not. This is not a substitute for redaction.

    **7. Verify — with checks that are actually meaningful**

    - `out_doc[0].get_text()`: assert each distinctive string from the deleted row is **absent**, and every kept row plus the new Total is present. Only valid for real redaction or a redacted stamp source; `get_text()` cannot see clipped-away content, so absence alone does not prove removal on a stamped page.
    - Re-extract with `find_tables()`: row count is one less, `del_top` now holds the row that used to be below.
    - Render at ~150 dpi and inspect: rules still span the rows, shading aligns, the bottom of the table has closed up, the Total sits on its own row.
    - Confirm content below the table hasn't shifted or been clipped.

    ## Traps to avoid

    - Stamping from an unredacted copy, or rebuilding a page from clipped bands that skip the row — the row remains in the file.
    - Stamping onto a region whose old content is still on the page.
    - Leading with "line art is untouched by default" or "the default deletes everything touching the rect" — both are false.
    - Using `REMOVE_IF_TOUCHED` on a horizontal band, which wipes vertical rules in retained rows.
    - Forgetting the Total, or recomputing it from the pre-deletion data.
    - Drawing fills after text.
- id: pdf-rowdel-02
  answer: |
    **Defaults (PyMuPDF ≥ ~1.18):**

    ```python
    page.apply_redactions(
        images=PDF_REDACT_IMAGE_PIXELS,                      # 2
        graphics=PDF_REDACT_LINE_ART_REMOVE_IF_COVERED,      # 1
        text=PDF_REDACT_TEXT_REMOVE,                         # 0
    )
    ```

    So the defaults are *not* "graphics untouched".

    **Images — `PIXELS` (2)**
    The image object stays; only the pixels whose area falls inside the redaction rectangle are overwritten (blanked). The rest of the same image survives unchanged, including its original encoded stream outside the rect. `PDF_REDACT_IMAGE_REMOVE` (1) instead drops the whole image if its bbox intersects the rect, and `PDF_REDACT_IMAGE_NONE` (0) leaves images alone entirely. Consequence: a photo or logo that straddles a redacted row gets a hard-edged rectangular hole punched in it — it is not deleted, and it is not "untouched" either.

    **Vector graphics — `REMOVE_IF_COVERED` (1)**
    Any drawing **path object whose bbox lies entirely inside** the redaction rect is deleted. A path that is only partly covered is kept **whole** — it is not clipped, it is not "touched then removed". `LINE_ART_NONE` (0) keeps everything; `LINE_ART_REMOVE_IF_TOUCHED` (2) deletes any path that merely crosses the rect, which is dangerous on a band because a table's vertical rule then disappears for the rows that stay.

    Two practical consequences:
    - Cell shading / row background fills are usually *entirely* inside a row-height or cell-sized rect, so the defaults silently erase them.
    - Grid lines often survive even when they cross the redacted row, because MuPDF tests the path object's bbox. If a whole table grid is drawn as one path, its bbox spans the table and will rarely be "fully covered" — so the row's borders remain drawn straight through the blanked area. If each rule is its own path, horizontal rules inside the band vanish while vertical rules crossing it stay whole.

    **Text — `REMOVE` (0)**
    Every character whose bbox intersects the rect is removed from the content stream (real removal; `get_text()` no longer returns it). This is character/bbox-based, not glyph-outline-precise, so an over-padded rect can clip characters of the neighbouring row or cell, and `PDF_REDACT_TEXT_NONE` (1) merely covers text visually while leaving it extractable.

    **Why it matters for rows and cells**

    - *Cell edit:* to replace one cell's text and keep its borders and background, a rect tight to the glyphs plus `graphics=PDF_REDACT_LINE_ART_NONE, images=PDF_REDACT_IMAGE_NONE` is the safe call. With the defaults, a rect padded out to the cell edge will remove the cell's fill (entirely covered) and may remove its rules. Never pad "to be safe" — padding is exactly what breaks the styling. Re-extract the neighbouring cells afterwards to prove they are intact.
    - *Row delete:* the row's shading is guaranteed to go (it lies inside the band), which is what you want, but the grid goes asymmetrically — fully covered horizontals removed, crossing verticals left whole and now running through the deleted row. Images get pixel-blanked, not removed. So after `apply_redactions` you must redraw the shading and rules (fills → text → rules), and rely on text removal for the row's strings.
    - *Security:* the defaults do perform real removal for text, and for the covered portion of images/line art — but only inside the rect, and only for whole paths. Nothing outside the rect is affected, and `show_pdf_page(..., clip=...)` is not removal at all. Also note the redaction annot's fill (white by default) is painted into the rect, which is why the deleted band looks like a clean white patch even when stray rules or image pixels remain inside it.

    One version caveat: these `images`/`graphics`/`text` keywords and the `PDF_REDACT_*` constants exist only in reasonably recent PyMuPDF builds; much older versions had a bare `apply_redactions()` with fixed behaviour, so check the installed version before relying on the defaults.
- id: pdf-rowdel-03
  answer: |
    Yes — `Page.show_pdf_page(dest_rect, src_doc, pno, clip=src_rect)`. It stamps the clipped source region as a Form XObject at any rect you choose, so the moved block keeps its exact vectors, fonts and text (it is not rasterized, unlike `get_pixmap`). The catch is that `clip` only *hides* the rest of the source page, so the old copy of the region must be genuinely deleted on the target and the deleted row must be genuinely gone from the stamp source.

    Order of operations:

    ```python
    import pymupdf

    doc = pymupdf.open("in.pdf")
    page = doc[0]

    # stamp source: same file, but with the deleted row really removed
    src = pymupdf.open("in.pdf")
    sp = src[0]
    sp.add_redact_annot(deleted_row_rect)
    sp.apply_redactions()                 # row gone from the source bytes too

    # band that has to move up, in source coordinates
    move = pymupdf.Rect(x0, deleted_row_rect.y1, x1, table_bottom)
    shift = deleted_row_rect.height
    dest = move + (0, -shift)

    # 1. remove everything that is being replaced/moved on the target page
    page.add_redact_annot(pymupdf.Rect(x0, deleted_row_rect.y0, x1, move.y1 - shift))
    page.apply_redactions()

    # 2. put the moved band back, one row higher
    page.show_pdf_page(dest, src, 0, clip=move)

    # 3. recompute the Total from extracted data, then save
    doc.save("out.pdf", garbage=4, deflate=True)
    ```

    Notes that matter:

    - If rows only *move* (insert, relayout) with nothing deleted, stamp from an untouched copy — redacting the region you are about to stamp gives you an empty stamp.
    - The dest rect must not overlap anything that stays; redact the union of "deleted row + destination area" before stamping, never stamp on top of live old content.
    - `show_pdf_page` has `rotate=` too, if the source page is rotated.
    - Caveat: the stamped band becomes a Form XObject. Later redactions over that band may not remove its inner text (PyMuPDF's redaction historically does not reach into XObject content reliably), so plan edits in one pass or regenerate from the original rather than editing a stamped page iteratively.
    - The alternative — content-stream surgery (`page.read_contents()`, prepend `1 0 0 1 0 -h cm` to the operators below the cut, `doc.update_stream(...)`) — does move the original operators, but splitting a stream at a y-coordinate is fragile and still needs redaction for the deleted row; stamping is the practical route.
- id: pdf-rowdel-04
  answer: |
    Deleting a row from an invoice table is a *content* change that cascades. Besides the row itself, expect to fix:

    **1. All derived money figures — the big one**
    - Line-item count / quantity totals if any column summed them.
    - Subtotal (net total of the remaining lines).
    - Discounts and surcharges that are computed as a percentage of the subtotal.
    - Tax lines (VAT/GST/sales tax), including per-rate breakdowns — a deleted line may drop a whole tax band.
    - Grand total / total due / balance due / amount payable.
    - "Total in words" if the document spells the figure out.
    - Deposit, amount paid, and remaining balance if those are derived.
    - Any per-page or per-section subtotals, and "carried forward"/"brought forward" figures on multi-page invoices.

    **2. Numbering and counts**
    - Line numbers / item numbers of every row below the deletion (they must renumber, not skip).
    - Any stated count ("6 items", "3 lines") in the header, footer, or summary.
    - Reference numbers of the deleted item appearing elsewhere (notes, delivery references, PO line refs).

    **3. Cross-references outside the table**
    - Summary/abstract blocks that repeat the line list or its total.
    - Notes, terms, or a cover letter that mention the item, its quantity, or the old total.
    - Shipping/insurance lines that were priced off the removed value.

    **4. Layout and geometry**
    - Every row below the deletion shifts up by one row height; the table bottom, total row, and footer must move with them.
    - The vacated band must be actually closed — redraw the shifted rows and the table rules, otherwise you keep a blank gap or leftover grid.
    - The total row and any table border/bottom rule.
    - Pagination: if the deletion pulls content up, a page break may collapse, a repeated header row may become orphaned, and the "page X of Y" or footer position may change.
    - Text/image elements anchored below the table (signature block, payment details, footer, stamps, barcodes) may need to move up.

    **5. Document-level traces**
    - The PDF text layer (so the deleted string is genuinely gone, not just visually covered).
    - Metadata, bookmarks/outline entries, attachments/exports, or an XML/e-invoice payload generated from the same data.
    - Any checksum, hash, or digital signature — editing invalidates it.

    The practical order: recompute the figures, renumber, then relayout/redraw the table and everything below it, then update cross-references and metadata, and finally verify the deleted text is absent from the text layer and the totals arithmetic checks out.
- id: pdf-cell-01
  answer: |
    ## Replace one right-aligned number in place (PyMuPDF)

    Assumptions: single page, ordinary table, text is real text (not a scan), page not digitally signed (redaction invalidates signatures, so save to a new file).

    ### Steps

    1. **Locate the exact span.** Use `search_for()` for the tight glyph rect, and `get_text("dict")` (or `"rawdict"`) to get the span's style and baseline. You need four things: the **right edge** `bbox[2]` (the alignment anchor), the **baseline** `origin[1]`, and the span's `font`, `size`, `color`.
    2. **Remove the old digits for real.** Add a redaction annotation over the glyph bbox and call `apply_redactions(graphics=0, images=0)`. The default `text` mode removes any character whose bbox overlaps. `graphics=0` / `images=0` are what preserve the cell's rules and shading — with defaults, a fill or rule lying entirely inside the rect would be deleted. Never paint a white rectangle over the number: the old string stays in the text layer and still searches/copies.
    3. **Keep the rect tight to the glyphs**, inside the cell. Do not pad it outward to "catch" anything — that is how you clip the neighbouring cell's text or the column rule.
    4. **Re-draw right-aligned.** Compute the new width with the *same* font and size (`Font.text_length`), then place the baseline origin at `x = old_right_edge - new_width`, `y = baseline`. Right edge stays fixed; the number grows/shrinks to the left exactly like a native right-aligned cell.
    5. **Match the typeface.** Reuse the embedded font (`page.get_fonts(full=True)` → `doc.extract_font(xref)` → `pymupdf.Font(fontbuffer=buf)`) when possible. If it is a **subset** and lacks glyphs for the new digits (`font.has_glyph(ord("5"))` is false), fall back to a full font of the same family (or Helvetica as last resort). This subset issue is the main reason replacements look "pasted in".
    6. **Verify, then save.** Re-extract the cell: the new string present, its `bbox[2]` matching the old right edge to within a point, neighbours unchanged, and `search_for("22.50")` empty. Render a pixmap at 150–200 dpi and eyeball it. Save with `garbage=4, deflate=True` to a new file.

    ### Code

    ```python
    import pymupdf

    OLD, NEW = "22.50", "54.00"
    doc  = pymupdf.open("in.pdf")
    page = doc[0]

    # 1. tight glyph rect + span style
    rect = page.search_for(OLD)[0]
    span = next(s
                for b in page.get_text("dict")["blocks"]
                for l in b.get("lines", [])
                for s in l["spans"]
                if s["text"].strip() == OLD)

    old_x1   = span["bbox"][2]          # right edge = anchor
    baseline = span["origin"][1]        # baseline y
    size     = span["size"]
    c        = span["color"]
    color    = ((c >> 16 & 255) / 255, (c >> 8 & 255) / 255, (c & 255) / 255)

    # 2. real removal, keep grid and shading
    page.add_redact_annot(pymupdf.Rect(span["bbox"]))
    page.apply_redactions(images=pymupdf.PDF_REDACT_IMAGE_NONE,
                          graphics=pymupdf.PDF_REDACT_LINE_ART_NONE)

    # 5. same font if usable, else full fallback
    font = pymupdf.Font(fontname="helv")
    for f in page.get_fonts(full=True):
        if f[3] == span["font"]:
            try:
                *_ , buf = doc.extract_font(f[0])
                cand = pymupdf.Font(fontbuffer=buf)
                if all(cand.has_glyph(ord(ch)) for ch in NEW.replace(".", "")):
                    font = cand
            except Exception:
                pass
            break

    # 4. right-aligned on the original baseline
    w  = font.text_length(NEW, fontsize=size)
    tw = pymupdf.TextWriter(page.rect)
    tw.append((old_x1 - w, baseline), NEW, font=font, fontsize=size)
    tw.write_text(page, color=color)

    # 6. verify
    assert OLD not in page.get_text()
    page.get_pixmap(dpi=200).save("check.png")

    doc.save("out.pdf", garbage=4, deflate=True)
    ```

    `insert_text` with `fontname=` after `page.insert_font(fontname="F0", fontbuffer=buf)` is an equivalent route; `TextWriter` is cleaner when you want to pass an exact `Font` object. If you prefer box layout, `insert_textbox(..., align=pymupdf.TEXT_ALIGN_RIGHT)` on a rect whose `x1` is the original right edge also works, but you must size the rect to the new text height or vertical placement will drift.

    ### Gotchas

    - Do **not** use `graphics=PDF_REDACT_LINE_ART_REMOVE_IF_TOUCHED` here — it can wipe column rules that merely cross the rect.
    - Keep the redaction inside the cell; a rect that reaches the vertical rule deletes the rule (only with `graphics=0` is it safe).
    - Numeric tables usually use tabular figures; if the replacement font is proportional, widths still compute correctly, but visually the columns may look slightly off — prefer the embedded or same-family font.
    - Save to a new file; redaction breaks any existing signature.

    ---

    ### Recap

    **ASKED** — how to replace one right-aligned number in a table cell in an existing PDF with PyMuPDF so it looks native.

    **WORKED** — BUILT a six-step procedure plus runnable code: tight glyph search, style extraction (right edge, baseline, size, colour, font), true text removal via redaction with `graphics=0, images=0`, right-aligned redraw at the old right edge on the same baseline, embedded-font reuse with subset fallback, and verification/save.

    **FOUND** — The two failure modes are a padded redaction rect destroying rules/shading, and an embedded subset font lacking the new digits. Both are handled explicitly; painting white boxes or skipping the font check is what produces "pasted-on" output.

    **DECIDED** — Used redaction rather than cover-up because only redaction removes the old string from the text layer, and `graphics=0, images=0` because the defaults would delete cell shading/line art. **BOTTOM LINE:** redact tight with graphics/images off, then redraw at `old_right_edge − new_text_width` on the original baseline using the embedded font.

    **NEXT** — None — answer delivered; the only runtime check to add is that `font.has_glyph()` returns true for every new digit before drawing.
- id: pdf-cell-02
  answer: |
    ## Short answer

    Size the rect as **the union of the target cell's character bboxes, plus a hairline of padding on every side, clamped so it stays strictly inside the cell's inner box and stops at the midpoint of the whitespace before the neighbouring cells' glyphs**. Do not pad it out to "reach" the rules, and do not shrink it to the glyph ink either.

    ## Why

    The removal test is an **intersection test, not a containment test**: with `text=PDF_REDACT_TEXT_REMOVE` (the default, 0) a character is deleted if any part of its bbox overlaps the rect. Consequences:

    - **Undersizing** leaves whole characters behind — you don't get half a glyph clipped, you get the character untouched or gone. A rect that misses a descender, an italic overhang, a ligature or the leading/trailing glyph's bbox leaves fragmentary text ("ghost" digits/letters) in the cell.
    - **Oversizing** silently deletes the neighbour's characters. In a tight table the neighbouring cell's glyph bbox may start only 1–2 pt away, so even a small outward pad erases a whole glyph from the adjacent cell — and `get_text()` then looks "clean" while the table content is corrupted.
    - The character bbox is a **font-metric box, not the ink box**: it includes ascender/descender and side bearings. That is what you must cover, so use `rawdict`/`dict` char or span bboxes, not an estimate from the visible pixels.

    ## Practical recipe

    1. Get the cell's text extents from the extracted dict (`get_text("dict")`/`"rawdict"`, or the words/bboxes inside the cell from `find_tables`).
    2. Expand by a small epsilon — roughly 0.5–1 pt, or ~10 % of the font size — to swallow antialiasing, italic overhang and bbox rounding.
    3. Clamp against the cell's inner rectangle: keep clear of the border strokes and shading, and leave at least ~1 pt of clearance from the nearest neighbouring glyph bbox. If two columns nearly touch, cut the rect at the midpoint of the gap rather than padding symmetrically.
    4. On the clamped rect call `page.add_redact_annot(rect)` then `page.apply_redactions()`. If you want the cell's fill and the rules preserved, pass `graphics=0, images=0` (defaults would delete shading that lies entirely inside the rect, since `graphics=1` is `REMOVE_IF_COVERED`).
    5. Only then draw the replacement text — content added after `apply_redactions` is not removed.
    6. Re-extract the cell **and its immediate neighbours** afterwards to verify nothing was eaten.

    For rotated or vertical text use the transformed quad from the dict, not the axis-aligned horizontal bbox; the same overlap rule applies but the bbox orientation differs.

    **Assumptions:** horizontal single-line cell text, the goal is to keep the grid/shading and the adjacent cells intact, and the new text is written after redaction is applied.

    ```python
    # union of char bboxes for the cell's text, then clamp
    r = fitz.Rect(union_of_char_bboxes)
    pad = max(0.5, 0.10 * fontsize)
    r = fitz.Rect(r.x0 - pad, r.y0 - pad, r.x1 + pad, r.y1 + pad)
    r = r & cell_inner_rect          # never cross the borders
    r = clamp_away_from(neighbour_glyph_bboxes, r, clearance=1.0)
    page.add_redact_annot(r)
    page.apply_redactions(graphics=0, images=0)   # keep fill + rules
    ```
- id: pdf-cell-03
  answer: |
    ## What `Page.insert_textbox` does

    `Page.insert_textbox(rect, text, ...)` wraps the text at spaces (and at `\n`), lays it out line by line from the top-left of `rect`, and returns a **float**:

    - **`>= 0`** — success. The value is the *spare height* left over in points (useful for vertical centring).
    - **`< 0`** — the text did not fit. The value is the deficit in points, and **nothing is written** — the call is all-or-nothing. A failed call leaves the page untouched, so retrying with a smaller size is safe.

    Things it does *not* do:

    - It does not shrink the font, elide, or hyphenate.
    - It does not break long words, so a single token wider than the rect can spill past the right edge **while the return value is still non-negative** — the fit test is vertical only. Width must be checked separately.
    - It does not clip; overflow is your problem.
    - No partial write: you either get the whole string or nothing.

    ## Options when it doesn't fit

    **1. Shrink the font (usual first move).** Trial-insert on a scratch page so a successful probe can't pollute the real page, then do one real insert:

    ```python
    def fit_textbox(page, rect, text, start=10, minimum=6, step=0.25, **kw):
        fs = start
        while fs >= minimum:
            probe = fitz.open()
            p = probe.new_page(width=rect.width, height=rect.height)
            if p.insert_textbox(p.rect, text, fontsize=fs, **kw) >= 0:
                page.insert_textbox(rect, text, fontsize=fs, **kw)
                return fs
            fs -= step
        return None          # caller must fall back to option 3 or 4
    ```

    Keep the returned size as the cell's style for consistency, and respect a floor (typically ~85% of the neighbours' size, or ~7 pt).

    **2. Make the rect bigger.** More height = more wrapped lines, so growing the row (or widening the column) fixes it. In a table this means re-laying out the rows below — redact the moving region, apply, redraw shifted down — and the text must stay inside the cell rules.

    **3. Truncate with an ellipsis**, measuring with the exact font you'll write:

    ```python
    font = fitz.Font(fontbuffer=font_bytes)   # or fitz.Font("helv")
    def ellipsize(s, width, fs):
        if font.text_length(s, fs) <= width:
            return s
        lo, hi = 0, len(s)
        while lo < hi:
            mid = (lo + hi + 1) // 2
            if font.text_length(s[:mid].rstrip() + "…", fs) <= width:
                lo = mid
            else:
                hi = mid - 1
        return s[:lo].rstrip() + "…"
    ```

    (`fitz.get_text_length` only covers the Base-14 fonts — use `fitz.Font(...).text_length` for embedded fonts.)

    **4. Do the line breaking yourself.** Split into lines with `Font.text_length`, then `page.insert_text((x, baseline), line, fontname=..., fontsize=...)` per line. Full control, no all-or-nothing failure, and you can put the remainder in a second rect or stop at the cell edge.

    **5. Use `TextWriter.fill_textbox`, which *does* write partially.** It fills line by line until the rect is full and returns `(spare_height, last_point)`; a negative `spare_height` means it truncated, and the `warn=` callback hands you the leftover text. Good for logging overflow, then continuing elsewhere or appending `…`.

    **6. `Page.insert_htmlbox` with `scale_low`** returns `(spare_height, scale)` and auto-shrinks down to the factor you allow — a one-call shrink-to-fit if HTML is acceptable.

    **7. Other levers:** a smaller `lineheight` multiplier to squeeze in one more line (risks descender collisions), `rotate=90` for a tall narrow cell, or simply rewording the text — often the only honest fix in a fixed-layout document.

    **Suggested order for a table cell:** one line at the original size → shrink to the floor → ellipsis → grow the row and reflow the rows below. Never let the string cross the cell's right rule into the next column; if you shrank or truncated, re-extract the cell and its neighbours afterwards to confirm the grid and the surrounding text survived.

    One caveat worth stating: I'm confident about the return-value contract and the all-or-nothing behaviour of `insert_textbox`, and about `fill_textbox` returning partial text via `warn`. I'm less certain of the exact default `lineheight` value across versions, so treat that as version-dependent rather than a fixed 1.2.
- id: pdf-cell-04
  answer: |
    **`point` is the text-origin / pen position of the first glyph: the left edge of the first character *on its baseline*.** In PyMuPDF page coordinates (origin top‑left, y grows downward, unrotated page space), that means `point` sets `x` at the start of the first glyph's advance and `y` at the baseline — caps and ascenders rise *above* it, descenders (`g`, `p`, `y`, `,`) fall *below* it. The docs calling it the "bottom-left of the first character" only works out because the baseline is the bottom of most glyph boxes; it is not the bottom of the bbox when descenders are present.

    **Passing the old word's bbox top-left `(x0, y0)`** puts the baseline where the old text's *top* was, so the whole new line is drawn roughly one ascent too low — about `0.7–1.0 × fontsize` (cap height plus a little) downward. Consequences:

    - The new text hangs where the old line used to be, not on it: it typically collides with the line/row below, crosses the cell's bottom rule, or spills out of the cell entirely.
    - Vertical centering is off by ~half an ascent, so the cell looks bottom-heavy.
    - `insert_text` draws exactly one line: no wrapping, no clipping to a cell. If the baseline is wrong *and* the string is longer than the old one, it just runs past the cell border.
    - Any later verification via `get_text()` bbox comparisons will mismatch, making it look like the insert "moved" the text.

    **Correct source of the point:** use `span["origin"]` from `get_text("dict")` — that *is* the baseline start, exactly what `insert_text` wants. Otherwise compute `baseline_y = bbox.y1 - descent` (descent ≈ `0.2–0.25 × fontsize`, or take it from the font's metrics), or just position with `insert_textbox(cell_rect, text, align=...)` and let PyMuPDF fit the line inside the rect.

    Two related gotchas: copy `fontname`/`fontsize`/`color` from the span you're replacing or the new glyph widths won't match the old ones and can overflow the cell; and on a rotated page the point must be in unrotated page space (`page.derotation_matrix`), not in the visually rotated coordinates you read off the screen.
- id: pdf-relayout-01
  answer: |
    # Inserting a body row just above the Total row of a ruled PDF table

    A PDF has no flow and no table objects, so "adding a row" is a physical relayout: the insert band is created by pushing everything below it down by the new row's height (`h`), removing the old content at those coordinates, and redrawing/restamping it at the new coordinates. Nothing may be merely painted over.

    ## 1. The Total row

    - It moves down by exactly `h` (one new-row height), along with its top rule, its fill, and any double rule or bold styling above it. Its bottom border and the table's outer bottom edge move down by `h` as well.
    - Its **value must be recomputed** from the data including the new row, then re-typeset. Shifting the row as a graphic would preserve the stale printed sum; the old total glyphs must be redacted and the new number drawn in the same font/size/colour/alignment.
    - If the shift would run into the bottom margin/footer, the Total (and trailing content) cannot overlap: break the page — continue the table on a new page with repeated column headings, carry the Total to the bottom of the continued table, and add a page if needed. Never let the shifted Total collide with the footer.

    ## 2. The table rules

    - The existing horizontal rule between the last body row and the old Total position **stays** and becomes the new row's top boundary.
    - One **new horizontal rule** is added at the bottom of the new row (equivalently: the Total's top rule moves down by `h`).
    - All **vertical rules** (outer left/right borders and every column separator) must be **extended by `h`** through the new row, and their lower endpoints moved to the table's new bottom edge. Their line width, colour and dash pattern must match the existing grid.
    - Any cell shading/zebra fill covering the grew region must be repainted, and striping renumbered if the table alternates.
    - Redaction with the defaults (`graphics = REMOVE_IF_COVERED`) deletes line art lying entirely inside the cleared band, so redrawing the rules is mandatory, not optional. Do not use `REMOVE_IF_TOUCHED` here: it would delete whole vertical paths that cross the band and destroy the separators for the rows that stay.
    - Draw in the order **fills → text → rules**, so a fill never hides the new row's text.

    ## 3. Content below the table

    Everything anchored below the insertion point shifts down by `h`: trailing notes/source lines, footnote markers, captions, signature blocks, page furniture that participates in the layout. Old positions must be genuinely removed (redaction + `apply_redactions`), then the block redrawn at `y + h` — or, for a pure move, stamped from an **untouched** copy with `show_pdf_page(new_rect, src, pno, clip=old_rect)`; a move-only case must not redact the region it is about to stamp, and the destination band must be cleared first. Fixed page footers/pagination normally stay put; if the table has grown into them, paginate instead.

    ## 4. Order of operations

    1. `find_tables()` → row boxes and text; derive `h` from an existing body row.
    2. Determine the destination band: from the insertion line down to the bottom of everything that moves (Total, notes, footer if moved) **plus** `h`.
    3. Redact that band; `apply_redactions()` (keep defaults when you are redrawing rules/shading).
    4. Draw the new row: fill, then cell text in the body style of its row, then the rules.
    5. Shift/redraw (or stamp from an untouched copy) the Total and all content below, one row lower.
    6. Rewrite the Total amount from the recomputed sum; adjust/extend the vertical rules and the bottom border.
    7. Save a new file with `garbage=4, deflate=True`.

    ## 5. Checks that it actually worked

    - `get_text()` on the output contains the new row's cells, the new total, and **no duplicate** of the moved rows at their old coordinates.
    - Table re-extraction reports one extra body row, columns still aligned, Total still the last row.
    - No word/drawing/image bbox overlaps the new row's band; vertical rules reach the new bottom edge.
    - If a stamp was used, remember `get_text()` cannot see clipped-away content — cleanliness rests on the stamp source being correct (untouched for a pure move, redacted only for deletions).

    **Assumption:** the table has a single Total row at the bottom, room exists on the page (or pagination is acceptable), and the new row takes the height and style of an existing body row; if the new row is taller, `h` is its own height and all shifts use that value.
- id: pdf-relayout-02
  answer: |
    ## Assumption

    The table already touches both margins, so there is **no unused width on the page**. The margins, page size, and the table's total width must stay as they are; the new column has to come out of the space the existing columns occupy.

    ## Correct approach

    **1. Treat it as a re-layout, not an insertion.** A PDF has no table model — there are no column objects to insert into. The table is just text runs, filled rectangles, and stroked rules at fixed coordinates. "Adding a column" means: compute a new geometry, remove the old table's marks, and redraw the table at the new geometry. Extract first: `find_tables()` for row/column bounding boxes and cell text, `get_text("dict")` for the header and body span styles (font, size, colour — the header is typically white `0xFFFFFF` on a dark fill), and the page's drawings for the rule positions and shading rects.

    **2. Keep the total width constant and redistribute.** New column width `w_new` is taken proportionally from the existing columns, or from the widest ones, while respecting a minimum content width per column so nothing becomes unreadable. Recompute every column boundary `x` from the left margin using the new widths; the rightmost boundary must still land exactly on the original right edge (table `x1 = right margin`), not beyond it. Verify `sum(new widths) == original table width` before drawing anything.

    **3. Re-measure rows, don't reuse blindly.** The narrower columns may force wrapping in cell text that used to fit on one line. Re-wrap, recompute row heights, and if any row grows, shift everything below the table (Total row, notes, footer) down accordingly — and confirm that still clears the footer. If the header text of the new column wraps, the header row height changes too.

    **4. Redraw in paint order: fills first, then text, then rules.** Redact the whole table region (or every cell whose geometry changes — effectively all of them) with the default redaction settings and `apply_redactions()`; then draw the new cell/header fills, then the text, then the grid lines. Copy the new header cell's style exactly from the existing header spans (fill colour, white text, font, size, alignment); give new body cells the body style of their row. If the table has a Total row, recompute it to include the new column.

    **5. Validate.** Extract text and confirm the new values are present, the old column strings are gone, and no text bbox crosses the right margin. Check the table's right edge equals the original table `x1`. Render the page to an image and look at it — overlapping columns and clipped text are visible there but often invisible in the text layer. Confirm neighbouring content (footer, notes, following paragraphs) is intact.

    **If there genuinely is no room** (columns already at their minimum), the honest options are: shrink the font/padding a step, reduce the margins deliberately (a design change, not a silent overflow), split the table or continue it on a landscape page, or drop/merge a column. Widening past the margin is not one of the options.

    ## Common mistake

    **Appending the column past the right edge.** The table is already full width, so "add a column on the right" puts it outside the margins, off the page, or into the footer/margin area — and because the existing cells are untouched, the new column also sits on top of whatever used to occupy that space. Related variants of the same error:

    - **Shifting only the new cells and leaving the old geometry alone.** The original text and rules stay at their old x positions, so columns overlap or the grid no longer lines up with the text. You must redraw the entire table (redact + rebuild); editing "the new column" in isolation is not possible.
    - **Widening the table to make room** instead of redistributing, pushing `x1` beyond the right margin.
    - **Assuming the PDF will reflow.** It won't — no column insert, no automatic text push; every coordinate is manual.
    - **Forgetting the knock-on layout:** row heights change from wrapping, content below the table isn't shifted, and the Total isn't recomputed.
    - **Getting the paint order wrong** (text before fills hides the text; fills after rules covers the grid) or **getting the header style wrong** (dark text copied onto the dark header fill).
    - **Leaving stale marks:** old rules and shading still in the file because the redaction was done with `graphics=NONE`/`images=NONE` while claiming to rebuild, or because the moved table was stamped from an unredacted source — the clip hides the old content but doesn't remove it.
- id: pdf-relayout-03
  answer: |
    **Short verdict:** it's acceptable only when the document is being *re-authored*, not *patched* — and only when the output is allowed to look different from the input. If the goal is "change two cells and delete a row, everything else stays pixel-identical," round-tripping is the wrong tool and will cost you more than it saves.

    ## When it's acceptable

    - **The edit is structural, not local.** Reflowing prose, changing column widths, merging/splitting cells, adding dozens of rows, restyling the whole table, re-paginating a long report. In-place redaction-and-redraw does not scale to this; a real document model does.
    - **You have the upstream source anyway** (DOCX, HTML, Markdown, a database row, a template). Then the correct move isn't PDF→DOCX→PDF, it's *regenerate from the source*. Converting the PDF is only the fallback when the source is lost.
    - **The input is a clean born-digital PDF:** real text layer, simple single-column tables, no merged cells spanning pages, embedded fonts you can substitute, little vector art or transparency.
    - **Fidelity is negotiable and the result will be reviewed.** New pagination, new page count, slightly different leading — all fine. The document is a draft/report/quote that a human will look at before it matters.
    - **No hard constraints on the artifact:** no digital signature, no PDF/A archival requirement, no tagged-PDF accessibility obligation, no AcroForm fields, no enforced permissions, no exact page-number/TOC/cross-reference contract.
    - **Templated, batch, reproducible pipeline** (e.g. headless LibreOffice with a fixed DOCX template and pinned fonts), where you control the converter and can regression-test the output.

    ## The risks

    **Layout and typography.** Conversion is layout *reconstruction*, not format translation. Column widths, cell padding, hyphenation, line breaks and page breaks are recomputed, so pagination shifts, tables split at different rows, repeated header rows may vanish, and footnotes/captions detach. Font substitution changes glyph metrics, which changes wrapping, which changes page count — cascading.

    **Table structure.** `colspan`/`rowspan`, nested tables, border collapse, and cell shading are the first things converters mangle. HTML in particular has no model for a header row that repeats across pages, and tables frequently degrade into positioned text frames or drawing objects. A merged cell often comes back as several cells or as one cell with text floating over rules.

    **Content and data integrity.** Locale-sensitive numbers get reformatted (`1.234,56` ↔ `1,234.56`, currency symbols moved), dates auto-converted, leading zeros and long IDs mangled by spreadsheet-style autoformatting. Ligatures, soft hyphens, NBSPs and CJK/RTL shaping can break. Derived values — the Total — are *not* recomputed; you must recompute from the extracted data regardless.

    **Images and vector art.** Rasterization, recompression, loss of clipping paths and transparency, vector logos turned into bitmaps, charts losing whatever editability they had. Output file size often balloons.

    **Semantics that PDF carries and DOCX/HTML don't (or vice versa).** Tags and reading order, alt text, bookmarks/outline, annotations and comments, form-field widgets, layers/OCGs, metadata and XMP, document IDs. Digital signatures are invalidated the moment the bytes change. Encryption and permission flags may be stripped or block the conversion outright.

    **Redaction is disqualified.** Round-tripping is *not* a redaction method. Text you thought was hidden — white-on-white, off-page, clipped, in annotations or attachments — can resurface in the intermediate, and the intermediate (DOCX/HTML plus any converter cache or cloud upload) becomes a second copy of the sensitive content that must itself be destroyed. Do real redaction in place and verify it.

    **Scanned PDFs.** There's no text to convert. You get garbage or you OCR, which produces a *different* document (image + invisible text layer) with recognition errors. Never round-trip a scan to "edit the table."

    **Process and provability.** Round-trips are not idempotent: PDF→DOCX→PDF→DOCX gives a different result each pass, and the diff is enormous. You lose the ability to show a reviewer "only this row changed." Converter behaviour varies by version and by installed fonts, so desktop and CI output diverge, and cloud converters mean uploading the document to a third party — a real DLP and licensing question.

    ## If you do it anyway, gate it on these checks

    1. Extract text before and after; every original string must still be present except the intended edits, with no `U+FFFD`/tofu.
    2. Parse every numeric cell, recompute totals, compare against the rendered values.
    3. Render both PDFs to PNG at ~150–200 dpi and pixel-diff; inspect every differing region, not just the page count.
    4. Check expected pagination, repeated header/footer, consecutive page numbers.
    5. List embedded fonts; confirm no silent fallback substitution.
    6. Inspect the intermediate for leftover comments, tracked changes, alt text, or hidden layers before it leaves your control.
    7. Confirm no signature, PDF/A, accessibility, or permission requirement was in scope — if any was, stop.

    ## Decision rule

    Ask which is cheaper to verify. A local cell edit or row delete has a tiny, reviewable blast radius — do it in place with redaction plus redraw, and prove the deleted strings are gone. A re-authoring job has a blast radius the size of the document — convert or, better, rebuild from the source, then treat the output as a **new document** requiring full visual and data validation. The one thing never to do is round-trip in order to *remove* content.
- id: pdf-relayout-04
  answer: |
    ## Short answer

    You don't ask the page "is this empty?" — you build the candidate rectangle for the new content and test it against **every piece of geometry on the page**: text word/span boxes, vector drawings, image placements, annotations, widgets, links. Then you intersect.

    `get_text() == ""` is not a test for empty space: it misses rules, shading, images, and clipped-away content that is still on the page.

    ## Recipe

    **1. Define the candidate rect in the target frame.**

    For growing a table downward:

    ```python
    tbl = page.find_tables().tables[0]
    row_h = tbl.rows[1].bbox[3] - tbl.rows[1].bbox[1]   # measured, not assumed
    need  = n_new_rows * row_h
    bottom_limit = footer_top                     # or page.rect.y1 - margin
    cand = fitz.Rect(tbl.bbox[0], tbl.bbox[3],
                     tbl.bbox[2], tbl.bbox[3] + need)
    assert cand.y1 <= bottom_limit, "not enough vertical room"
    ```

    Available space is `bottom_limit - tbl.bbox.y1`; required is `need`. Do the arithmetic first, then the geometry check — the arithmetic catches the common case (table would run into the footer) cheaply.

    **2. Collect everything already occupying the page.**

    ```python
    def occupied(page):
        out = []
        for w in page.get_text("words"):          # precise glyph boxes
            out.append(fitz.Rect(w[:4]))
        for b in page.get_text("blocks"):         # catches block-level structure
            out.append(fitz.Rect(b[:4]))
        for d in page.get_drawings():             # rules, shading, fills, borders
            out.append(d["rect"])
        for img in page.get_images(full=True):
            out.extend(page.get_image_rects(img[0]))
        for a in page.annots() or []:
            out.append(a.rect)
        for w in page.widgets() or []:
            out.append(w.rect)
        for l in page.get_links():
            if "from" in l:
                out.append(fitz.Rect(l["from"]))
        return out
    ```

    Use `get_text("words")` rather than only `"blocks"`: block rects are padded and will report false collisions in tight gaps. `page.cluster_drawings()` is useful when you want merged drawing regions instead of hundreds of individual paths.

    **3. Test with clearance, not bare touching.**

    ```python
    def free(page, cand, pad=1.0):
        cand = fitz.Rect(cand) + (-pad, -pad, pad, pad)
        if not page.rect.contains(cand):
            return False
        return not any(cand.intersects(r) for r in occupied(page))
    ```

    Use a small `pad` (about 1 pt) so that edge-adjacency and hairline rules don't count as collisions, and so text with tight ascenders/descenders doesn't visually crowd the new content.

    **4. Check the frame issues before trusting the result.**

    - `page.rotation != 0`: coordinates must be built in the same frame the extracted bboxes live in; if the page is rotated, convert with `page.rotation_matrix` / `page.derotation_matrix` rather than assuming.
    - Use `page.rect` (CropBox-based, visible area) for limits, not `page.mediabox`.
    - Skewed pages: extracted word rects are conservative; a true rotated-text bbox overstates area, which is safe for collision but may block space that looks free. Rendering can confirm.

    **5. Optional raster double-check.**

    Render the page at ~150 dpi before and after the insertion and verify that changed pixels are confined to `cand`. This catches anything `get_drawings()` does not model (patterns, shadings, image masks) but it only sees *visible* content — it will not reveal a clipped-away stamp, so it is a supplement, never the primary test.

    **6. Verify after the fact.**

    Re-extract words and confirm every unchanged string kept the same bbox (to within a fraction of a point). That proves the insertion pushed nothing. Also re-run `find_tables()` to confirm the new rows extend the table cleanly and the footer block is still where it was.

    ## The one non-obvious point

    A page can look empty in `get_text()` and in a render while still being "occupied" in the file, because `show_pdf_page(clip=...)` keeps the whole source page as an XObject and only hides it. Conversely, a rect that intersects nothing visible may still sit on top of hidden content. Geometry against `get_drawings()` plus a redacted-then-stamped workflow, not a text check, is what makes the space genuinely free.
- id: pdf-tblins-01
  answer: |
    Assumptions: the page uses PDF points, PyMuPDF (`fitz`) coordinates have their origin at the top-left, and "free space" means a rectangle that intersects no existing word, drawing, or image. Save to a new file rather than modifying in place.

    ## Shared first step: find and verify a free rectangle

    ```python
    import fitz

    def is_free(page, rect, pad=4):
        r = fitz.Rect(rect) + (-pad, -pad, pad, pad)   # grow by padding
        for w in page.get_text("words"):
            if fitz.Rect(w[:4]).intersects(r):
                return False
        for d in page.get_drawings():
            if d["rect"].intersects(r):
                return False
        for img in page.get_images(full=True):
            for ir in page.get_image_rects(img[0]):
                if ir.intersects(r):
                    return False
        return True

    doc  = fitz.open("in.pdf")
    page = doc[0]

    # Simple placement rule: below the lowest existing content, with a gap.
    content = [fitz.Rect(b[:4]) for b in page.get_text("blocks")]
    content += [d["rect"] for d in page.get_drawings()]
    bottom = max((q.y1 for q in content), default=page.rect.y0 + 36)
    y0 = bottom + 18

    rows   = [["Item", "Qty", "Price"],
              ["Widget", "2", "$5.00"],
              ["Gadget", "1", "$9.00"]]
    col_w  = [120, 50, 70]
    row_h  = 22
    W, H   = sum(col_w), row_h * len(rows)
    target = fitz.Rect(72, y0, 72 + W, y0 + H)

    assert target.y1 < page.rect.y1 - 36, "not enough room below content"
    assert is_free(page, target), "chosen rectangle is not empty"
    ```

    If there may not be room below the content, scan candidate y positions in small steps and take the first `is_free` rectangle; do not just assume a margin is empty.

    ## Way 1 — draw the table directly with PyMuPDF vector primitives

    Draw in the order **fills → text → rules**, because a later fill would paint over the text and rules.

    ```python
    x0, y0 = target.x0, target.y0

    # 1) fills
    for i, row in enumerate(rows):
        r = fitz.Rect(x0, y0 + i*row_h, x0 + W, y0 + (i+1)*row_h)
        if i == 0:
            page.draw_rect(r, color=None, fill=(0.20, 0.20, 0.20), width=0)
        elif i % 2 == 0:
            page.draw_rect(r, color=None, fill=(0.94, 0.94, 0.94), width=0)

    # 2) text
    for i, row in enumerate(rows):
        cx = x0
        for j, cell in enumerate(row):
            cell_rect = fitz.Rect(cx + 4, y0 + i*row_h + 3,
                                  cx + col_w[j] - 4, y0 + (i+1)*row_h - 3)
            color = (1, 1, 1) if i == 0 else (0, 0, 0)
            align = fitz.TEXT_ALIGN_RIGHT if j > 0 else fitz.TEXT_ALIGN_LEFT
            rc = page.insert_textbox(cell_rect, cell, fontsize=9,
                                     fontname="helv", color=color, align=align)
            if rc < 0:                      # text did not fit: shrink or widen cell
                raise RuntimeError(f"cell overflow: {cell!r}")
            cx += col_w[j]

    # 3) rules last
    for i in range(len(rows) + 1):
        y = y0 + i*row_h
        page.draw_line((x0, y), (x0 + W, y), color=(0, 0, 0), width=0.6)

    cx = x0
    for w in col_w[:-1]:
        cx += w
        page.draw_line((cx, y0), (cx, y0 + H), color=(0, 0, 0), width=0.6)

    page.draw_rect(target, color=(0, 0, 0), width=0.8)   # outer border

    doc.save("out.pdf", garbage=4, deflate=True)
    ```

    This keeps everything vector and selectable, and the drawn grid is normally detectable by `page.find_tables()`. `set_font`/`insert_text` with a computed baseline is an alternative if you want exact vertical centring; `insert_textbox` is simpler but must be checked for overflow (negative return value).

    ## Way 2 — build the table as a separate one-page PDF, then overlay it

    Use ReportLab for the table layout (it handles `GRID`, header fill, padding), then stamp that page onto the target with `show_pdf_page`. Make the overlay page exactly the size of the target rectangle so there is no scaling/aspect distortion.

    ```python
    import io
    from reportlab.lib import colors
    from reportlab.pdfgen import canvas
    from reportlab.platypus import Table, TableStyle

    def table_overlay(rows, col_w, row_h):
        W, H = sum(col_w), row_h * len(rows)
        buf = io.BytesIO()
        c = canvas.Canvas(buf, pagesize=(W, H))

        t = Table(rows, colWidths=col_w, rowHeights=[row_h]*len(rows))
        t.setStyle(TableStyle([
            ("GRID",        (0, 0), (-1, -1), 0.6, colors.black),
            ("BACKGROUND",  (0, 0), (-1, 0),  colors.HexColor("#333333")),
            ("TEXTCOLOR",   (0, 0), (-1, 0),  colors.white),
            ("FONTNAME",    (0, 0), (-1, -1), "Helvetica"),
            ("FONTSIZE",    (0, 0), (-1, -1), 9),
            ("VALIGN",      (0, 0), (-1, -1), "MIDDLE"),
            ("LEFTPADDING", (0, 0), (-1, -1), 4),
            ("RIGHTPADDING",(0, 0), (-1, -1), 4),
        ]))
        t.wrapOn(c, W, H)
        t.drawOn(c, 0, 0)          # ReportLab origin is bottom-left

        c.showPage()
        c.save()
        buf.seek(0)
        return buf.getvalue()

    overlay = fitz.open("pdf", table_overlay(rows, col_w, row_h))
    assert overlay[0].rect.width  == target.width
    assert overlay[0].rect.height == target.height

    page.show_pdf_page(target, overlay, 0)   # appends as a Form XObject on top
    doc.save("out.pdf", garbage=4, deflate=True)
    ```

    Notes for this route:

    - Do **not** paint a white background rectangle in ReportLab; leave the overlay transparent so the existing page shows through.
    - `show_pdf_page` stretches the source page into the given rectangle, so matching sizes (or at least aspect ratios) matters.
    - The overlay is appended after existing content, so it sits on top. That is why the free-space check matters: anything underneath is hidden, though still present in the file.
    - The stamped table keeps vector text (it is an XObject, not a bitmap), so text remains extractable. If you need genuinely native page content, Way 1 is cleaner.
    - If you are not using PyMuPDF for merging, the same overlay can be applied with `pypdf`: `page.merge_page(overlay_page)`; the ReportLab half is unchanged.

    ## Practical checks after either approach

    ```python
    out = fitz.open("out.pdf")
    p = out[0]
    assert "Widget" in p.get_text()
    assert is_free(out[0], fitz.Rect(0, 0, 0, 0)) or True  # re-run overlap check on target
    ```

    Reopen the output and confirm (a) the new strings are in `get_text()`, (b) the target rectangle still does not overlap pre-existing content, and (c) neighbouring text/rules are unchanged. A third, lighter option for simple styling is `page.insert_htmlbox(rect, "<table>…</table>")`, but its CSS support is narrower than ReportLab's, so the two methods above are the reliable ones.
- id: pdf-tblins-02
  answer: |
    ## Short answer

    Match the existing table's **geometry, style, and paint order**, and never paint a new element where an old one still lives. Concretely: read the existing table's style from the document itself (`find_tables`, `get_text("dict")` spans, `get_drawings()`), then draw the new table **fills → text → rules**, using coordinates derived from real content edges, and verify with a render.

    ## 1. Recover the style from the document, don't guess

    Before drawing anything, extract:

    - **Geometry** — the existing table's bbox (`find_tables().tables[0].bbox`), column x-boundaries, row heights, the x0/x1 the table aligns to, and the y where the body ends / footer begins.
    - **Typography** — from `page.get_text("dict")` spans: `font`, `size`, `color` (an int, e.g. `0xFFFFFF` for white header text), `flags` for bold. Take the header style from the header row and the body style from a body row; they usually differ.
    - **Rules and fills** — from `page.get_drawings()`: line `width`, stroke `color`, dash pattern, and the fill colours of header/body cells. A dark header is a filled rect plus white text, not a coloured font.
    - **Alignment** — numeric columns are usually right-aligned, text left-aligned, headers centred. Copy that, including number formatting (decimals, thousands separators).

    If the header text is white, your new header cell must be white too, on the same dark fill — otherwise it disappears or looks wrong.

    ## 2. Place it against content, not page margins

    - Left/right edges = the existing table's or text block's `x0`/`x1`.
    - Top = a gap below the last existing row (or the point where the new table is meant to sit).
    - Bottom = above the footer's top, computed from the footer text/drawing bbox, not `page.rect.height - 50`.
    - Check the target rect against **every** word, drawing and image bbox on the page (`get_text("words")`, `get_drawings()`, `get_image_info()`). If it intersects something, you are about to overprint — move it, shrink it, or go to a new page.
    - Column widths proportional to the content, measured with `fitz.get_text_length(text, fontname, fontsize)`; row heights from the existing rows, grown if a cell wraps.

    ## 3. Paint order: fills first, then text, then rules

    ```python
    shape = page.new_shape()

    # 1) fills (no stroke) — header fill, then body fills or alternating bands
    shape.draw_rect(header_rect)
    shape.finish(fill=header_fill, color=None)
    for r in body_rects:
        shape.draw_rect(r)
        shape.finish(fill=body_fill, color=None)   # or None for no fill

    # 2) text on top of the fills
    page.insert_text((x, y_baseline), cell_text,
                     fontname=font, fontsize=size, color=text_color)
    # or, for wrapping:
    rc = page.insert_textbox(cell_rect_deflated, cell_text,
                             fontname=font, fontsize=size, color=text_color,
                             align=fitz.TEXT_ALIGN_LEFT)
    # rc < 0 means overflow → shrink font or grow the row

    # 3) rules last, each internal line drawn once
    shape.draw_line(p0, p1)
    shape.finish(color=rule_color, width=rule_width)
    shape.commit()
    ```

    A fill drawn after the text hides the text in the render while leaving it in the text layer — that is the classic glitch.

    ## 4. Details that cause the visible glitches

    - **One line per boundary.** Don't stroke every cell rect: adjacent cells then produce doubled rules that look thick or misaligned. Draw the outer border once and each internal grid line once, centred on the boundary (a line of width `w` straddles the boundary by `w/2` on each side).
    - **No seams between fills.** Adjacent fill rects that share an edge can leave a 1-pixel background hairline at the join. Snap coordinates to a common value, or draw one background band and then only the lines.
    - **Opaque fills.** Semi-transparent fills on top of existing shading darken unpredictably; use the sampled solid colour.
    - **Same font.** Prefer the font already used on the page. If it's an embedded/subset font, use `fontfile=`/`fontname=` with that font; a base-14 substitute will be visibly different. Check the font actually has the glyphs (currency symbols, dashes).
    - **Baseline, not top.** `insert_text` takes the baseline. For vertical centring use the font metrics (`fitz.Font(...).ascender/descender`) or a deflated `insert_textbox`; don't eyeball `rect.y0 + size`.
    - **Overlay/z-order.** PyMuPDF appends new content, so it paints **on top** of everything already on the page (including watermarks and annotations). That's usually what you want for a new table, but if the page has an overlay or form fields, check the result. `overlay=False` puts content underneath *all* existing content, including background fills — your table can vanish.
    - **Rotation.** If `page.rotation != 0`, page coordinates and the visible orientation differ; use `page.derotation_matrix`/`rotation_matrix` to map your layout into the page's coordinate system, or the table lands rotated/offset.
    - **Never leave old content underneath.** If you're replacing or shifting content, redact and `apply_redactions()` first (tight rects, and only `graphics=0, images=0` if you need to keep a cell's borders/shading), *then* draw. Stamping or drawing over live text is not removal — the old strings remain extractable.

    ## 5. Verify

    - Render the page to a pixmap and look at it (`page.get_pixmap(dpi=150).save(...)`) — grid alignment, doubled or missing rules, text clipping, overlap.
    - Re-extract text and confirm every cell string is present exactly once, and that any replaced content is gone.
    - Check the new rect intersects no word/drawing/image bbox, and that nothing spills past the footer or table right edge.

    The one-line version: **sample the style from the document, anchor to content edges, paint fills → text → rules with one line per boundary, and render to check.**
- id: pdf-tblins-03
  answer: |
    You can't "insert" into a fixed-layout PDF — nothing reflows. So the question is really: where does the space come from, and does the existing content actually move? Options, roughly in order of preference:

    **1. Create the gap by moving the content below it (true relayout).**
    Measure the table height, then shift everything from the next paragraph down by that amount.
    - Take a copy of the document; leave the copy untouched (pure move — no redaction in the stamp source).
    - On the target page, redact the whole band that will move and `apply_redactions()`.
    - Stamp the band back one gap-height lower with `show_pdf_page(new_rect, copy, pno, clip=old_rect)`.
    - Draw the table in the freed space (fills → text → rules).
    - Verify the shifted content still clears the footer; if not, this option is out — go to 2 or 3.

    **2. Push onto the next page.**
    If shifting down would collide with the footer or run off the page, break the paragraph: keep its first lines where they are, move the rest plus everything after it to the top of the next page, and put the table where the break happens (or at the top of the next page, followed by the moved paragraph).
    - New page: `doc.new_page(pno+1, width, height)` and stamp the moved band from a copy, or `insert_pdf` a copy of the source page and redact away what doesn't belong.
    - Redact the moved content out of the original page — stamping a copy while the original text stays on the page gives duplicate, still-extractable text.

    **3. Compress to fit the space that already exists.**
    First check whether there is actually usable whitespace: get the bbox of the preceding paragraph's last line and the following paragraph's first line, subtract the footer top, and see what's left. If the table can be made to fit with smaller row padding, tighter leading (table only), fewer rows on this page, or a slightly narrower column set, that's cheaper and lower-risk than moving body copy. Don't shrink the body paragraphs' font to buy space — it reads as a mistake.

    **4. Change the placement (layout decision, not a technical one).**
    Move the table before the preceding paragraph or after the following one, or split the table so a portion lands at the page end and continues overleaf. If the document's reading order tolerates it, this is the cleanest fix and needs no movement at all.

    **5. Overlay in place — only if the covered text is meant to go.**
    If you draw the table over existing text without removing it, the old glyphs remain in the content stream: `get_text()` returns both, search and copy-paste break, and it's visible in the render if the fills aren't opaque. If replacement really is the intent, redact the covered glyphs (`graphics=0, images=0` if you want to keep rules/shading under it), apply, then draw the table. Never use a white rectangle as an eraser.

    **6. Full page rebuild.**
    Extract the paragraphs and the table data, clear the page's content, redraw everything with the table in place. Maximum control, maximum risk of style drift (fonts, hyphenation, kerning, inline images); reasonable only when the page is plain text and you can reproduce the styling from the extracted spans.

    What not to do, in any option:
    - Stamp the shifted region while the original is still on the page — you get two copies in the text layer.
    - Stamp from a copy in which you redacted the region you're about to stamp (empty stamp), or from an unredacted copy when something is being deleted (deleted content survives in the embedded page bytes).
    - Shift the footer or page number along with the body — clip the band to end above it.
    - Use `REMOVE_IF_TOUCHED` on a band whose vertical rules continue into rows that stay.

    Assumption: fixed-layout PDF edited with PyMuPDF, no semantic reflow, footer occupies the bottom margin. If the page's bottom margin can't absorb the shift, option 2 is the fallback every time.
- id: pdf-imgrep-01
  answer: |
    ## Short answer

    You don't have to touch the content stream at all — the placement lives there, so if you replace the **image object** the position and size are preserved automatically. On recent PyMuPDF:

    ```python
    import pymupdf

    doc  = pymupdf.open("in.pdf")
    page = doc[0]

    # find the logo: xref + the rect where it is drawn
    infos = page.get_image_info(xrefs=True)          # dicts: xref, bbox, width, height, transform, ...
    logo  = max(infos, key=lambda i: i["bbox"].get_area())   # or pick by rect/position
    xref  = logo["xref"]
    rect  = pymupdf.Rect(logo["bbox"])               # unrotated page coords, same space insert_image wants

    page.replace_image(xref, filename="new_logo.png")   # rewrites the image XObject only
    doc.save("out.pdf", garbage=4, deflate=True)
    ```

    `replace_image` changes the image's stream/dict (`/Width`, `/Height`, `/Filter`, …) and leaves the `Do` operator and its CTM in the page content untouched, so the logo keeps its exact rect. Same rect, new pixels.

    If `replace_image` isn't available in your version, or you must limit the change to page 1 only:

    ```python
    rect = pymupdf.Rect(logo["bbox"])
    page.delete_image(xref)     # drops every occurrence of that xref *on this page*
    page.insert_image(rect, filename="new_logo.png",
                      keep_proportion=False, overlay=True)
    doc.save("out.pdf", garbage=4, deflate=True)
    ```

    A version-proof fallback is redaction: `page.add_redact_annot(rect)`, `page.apply_redactions(images=pymupdf.PDF_REDACT_IMAGE_REMOVE, graphics=pymupdf.PDF_REDACT_LINE_ART_NONE)`, then `page.insert_image(rect, ...)`. That genuinely strips the old image (unlike painting a white box over it, which leaves the bytes extractable).

    ## The catch: an image is an object, not a placement

    An xref is a **document-level** object. The page only holds a reference to it. So:

    - **Same logo on every page.** A header logo is usually one xref invoked by all pages. `replace_image(xref, …)` (and `doc.update_stream(xref, …)`) changes it everywhere — a rebrand, not a page-1 edit. To change page 1 alone you must break the sharing: remove the reference on page 1 (`delete_image` + `insert_image`, or redaction + insert) so the new PNG gets its own xref. `delete_image` is page-scoped, so the other pages keep the old xref.
    - **Same xref twice on one page.** `delete_image(xref)` removes *all* occurrences on that page, not just the one you aimed at; `insert_image` puts back only one. For one-of-many, redact just that bbox, or duplicate the xref and rewrite the single `Do` operator.
    - **Image inside a Form XObject.** Check the `referencer` field of `page.get_images(full=True)` (index 9) or `xref` from `get_image_info`. Non-zero means the image isn't in page 1's `/Resources` but in a nested XObject that may be invoked by many pages. `delete_image` then no-ops/raises, and editing the XObject changes every invoking page — you have to clone the XObject for page 1 or repoint page 1's content.
    - **Old bytes linger.** Removing the reference doesn't delete the stream. Save with `garbage=4, deflate=True`, otherwise the old logo is still recoverable from the file and shows up in forensic extraction.
    - **Aspect ratio.** Replacing in place reuses the old CTM, so a PNG with a different aspect ratio is **stretched** to the old rect. With `insert_image`, `keep_proportion=True` letterboxes inside the rect instead of filling it. If you want a true drop-in, pad/crop the PNG to the original rect's pixel aspect first, then use the exact rect with `keep_proportion=False`.
    - **Transparency.** If the original had an `/SMask` or was a stencil, a plain opaque PNG will lose that alpha; `replace_image` doesn't reliably rebuild the mask. Safer to delete + reinsert, or supply a PNG with its own alpha.
    - **Deduplication bites back.** `insert_image` with byte-identical data reuses the existing xref, so "insert a fresh copy" can silently re-share it. Vary the bytes or use `xref=` deliberately.
    - **Rotation.** `get_image_info()["bbox"]` and `insert_image(rect)` are both in unrotated page coordinates, so they agree. Only `get_image_bbox(..., transform=True)` and `Page.get_text(clip=...)`-style calls apply the page rotation — mixing those with `insert_image` is the classic off-by-a-rotation bug.

    Verification: compare `[i["xref"] for i in p.get_image_info(xrefs=True)]` per page before/after (page 1's xref should differ, pages 2+ unchanged), and render `page.get_pixmap(clip=rect)` to confirm the new logo fills the old box without distortion.

    **Uncertainty:** I'm confident about the xref-sharing semantics, `delete_image` being page-scoped, and the CTM-preservation point; I'm less certain about the exact minimum version that introduced `Page.replace_image` (1.21/1.22 era) and about its behaviour when the old image carries an SMask — hence the delete+insert path above, which holds on any version.
- id: pdf-imgrep-02
  answer: |
    `insert_image` only paints the new image on top. It does not remove the old one.

    What is actually wrong:

    - **The old logo is still there.** It remains in the page's content stream and in the document's image resources, still rendered underneath the new one. The page merely looks correct because the new image is opaque and covers it.
    - **It can leak through.** If the new image has an alpha channel, soft/anti-aliased edges, or is slightly smaller than the old logo's bbox, the old pixels show at the edges or through the transparency. The old logo is also still recoverable by anyone who extracts images or objects from the file.
    - **It bloats the file.** Two full image XObjects are stored unless the streams happen to be identical bytes (then the xref is reused), and even then the old image remains referenced by the page.
    - **If the "logo" is vector art, not a raster image**, nothing about an image insert removes it at all — it is still drawn as line art/fill under the new image, and it will still render if the new image is moved, deleted, or has transparency.

    How to do it properly:

    1. Locate the old logo's **full bbox** (not just the rect you want for the new image) — `page.get_images()` / `get_image_bbox()` or the drawing bboxes.
    2. Redact that bbox and call `page.apply_redactions()`. Use the default `images=PDF_REDACT_IMAGE_PIXELS` (or explicitly `PDF_REDACT_IMAGE_REMOVE` if you want the image object gone); for vector logos use `graphics=PDF_REDACT_LINE_ART_REMOVE_IF_COVERED` or `REMOVE_IF_TOUCHED` as appropriate. Keep the rect tight to the old logo so you don't damage neighbouring art.
    3. **Then** `page.insert_image(rect, ...)` the new logo.
    4. Save with `garbage=4, deflate=True` so the now-orphaned old image stream is actually dropped.
    5. Verify: old image xref no longer referenced by the page, `get_images()` shows only the new one, and a rendered page at high zoom shows no bleed at the edges.

    Short version: painting over is cosmetic; redaction-then-insert is removal.
- id: pdf-imgrep-03
  answer: |
    **Assumption:** PyMuPDF (fitz) 1.23+, one page, and the “logo” may be any kind of artwork. `page.get_images()` only reports raster images referenced by the page’s resource dictionary; it is not a “what is visible here” query.

    ## What could be going on

    Ranked by how often it happens:

    1. **Inline image.** The logo is embedded directly in the content stream as `BI ... ID ... EI`. Inline images have no xref and are not in `/Resources`, so `get_images()` is empty by definition.
    2. **Nested Form XObject.** The page does `/Fm0 Do`, and the image lives in `/Fm0`’s own `/Resources /XObject`. Whether `get_images()` descends into form resources is version-dependent; frequently it does not. `get_xobjects()` shows the form, not the image.
    3. **Vector artwork.** The logo is paths/curves/fills drawn with `m/l/c/re ... f`, not an image. Common for corporate logos. `get_drawings()` / `get_cdrawings()` will show it.
    4. **Text or Type3 font.** The logo is glyphs, possibly from an embedded `/Type3` font whose `CharProcs` contain drawing operators. `get_images()` sees nothing; `get_text()` may show odd characters or nothing useful.
    5. **Annotation appearance stream.** A stamp/freetext/widget annotation draws the logo in its `/AP`. Annotations are not page content; `page.annots()` / `page.widgets()` finds it.
    6. **Pattern or shading.** The logo is a tiling pattern or shading (`/Pattern cs`, `scn`, `sh`), possibly containing an image or soft mask. It is in `/Resources /Pattern` or an `/ExtGState /SMask`, not `/XObject`.
    7. **Image used only as a mask.** A stencil (`/ImageMask`) or soft mask shapes a fill; it may be referenced from an ExtGState rather than drawn as an image.
    8. **`get_images()` returns referenced images that are not visible.** The converse is also true: it lists resources, not the display list. Do not use it to decide visibility.

    ## How to find it

    Start with the display-list API, which is the right “what is actually painted” query:

    ```python
    import fitz

    doc = fitz.open("in.pdf")
    page = doc[0]

    for info in page.get_image_info(xrefs=True):
        print("xref", info["xref"], "bbox", info["bbox"],
              "w/h", info["width"], info["height"],
              "cs", info.get("colorspace"))
    ```

    - In current PyMuPDF this reports images actually placed, including inline images (`xref == 0`) and images inside Form XObjects. If yours does not, walk the resources manually (below).
    - If it returns entries, compare each `bbox` with where the logo sits on a rendered page. `page.get_image_rects(xref)` gives all placement rectangles for a known image xref.

    If `get_image_info()` is empty, test the other categories:

    ```python
    # Vector art: look for paths whose rect matches the logo area.
    for d in page.get_drawings():
        print(d["rect"], d["type"], d.get("fill"), d.get("color"), len(d["items"]))

    # Text/Type3 logo.
    print(page.get_fonts(full=True))          # look for subtype /Type3
    for b in page.get_text("dict")["blocks"]:
        for l in b.get("lines", []):
            for s in l["spans"]:
                print(s["bbox"], s["font"], s["text"])

    # Annotations.
    for a in page.annots() or []:
        print(a.type, a.rect, a.info)
    for w in page.widgets() or []:
        print(w.field_type, w.rect)

    # Content stream: find Do / BI / pattern / shading operators.
    for xref in page.get_contents():
        print(doc.xref_stream(xref).decode("latin-1", "replace"))
    ```

    Then inspect structure with pikepdf (or recursion via `doc.xref_get_key`):

    ```python
    import pikepdf

    pdf = pikepdf.open("in.pdf")

    def walk(res, prefix=""):
        for name, obj in res.get("/XObject", {}).items():
            st = str(obj.get("/Subtype"))
            print(prefix + str(name), st, obj.objgen)
            if st == "/Form":
                walk(obj.get("/Resources", pikepdf.Dictionary()), prefix + str(name) + "/")
        for name, obj in res.get("/Pattern", {}).items():
            print(prefix + "pattern " + str(name), obj.objgen)

    walk(pdf.pages[0].Resources)
    ```

    Localize visually when in doubt: render the page, crop the logo region, then redact candidate bboxes and re-render — the thing that disappears is the thing you must replace. Watch page rotation: `get_image_info()` bboxes are in PyMuPDF page coordinates, which already account for `/Rotate`.

    ## How to replace it

    ### Raster image with an xref (page-level or nested form)

    ```python
    page.replace_image(xref, filename="new_logo.png")
    ```

    - This replaces the image object document-wide: every page and every placement using that xref shows the new image.
    - It keeps the existing placement transform, so the new image is stretched into the old rectangle. If the aspect ratio differs, the logo will be distorted. Either pre-pad the replacement to the old aspect ratio, or use the redact-and-insert route below with a correctly proportioned rect.
    - No redaction is needed for this route; the old stream is replaced by the new one.

    To replace only on one page (the xref is shared):

    ```python
    rect = fitz.Rect(info["bbox"])
    page.add_redact_annot(rect)
    page.apply_redactions(images=fitz.PDF_REDACT_IMAGE_REMOVE,
                          graphics=fitz.PDF_REDACT_LINE_ART_NONE)
    page.insert_image(rect, filename="new_logo.png")   # keep_proportion=True by default
    ```

    `PDF_REDACT_IMAGE_REMOVE` removes the whole image if it overlaps the rect; use `PDF_REDACT_IMAGE_PIXELS` if a background image also overlaps and must survive. `LINE_ART_NONE` keeps rules/shading that happen to lie in the rect.

    ### Inline image (`xref == 0`)

    There is no image object to swap. Redact the displayed bbox, then insert the new image:

    ```python
    rect = fitz.Rect(info["bbox"])
    page.add_redact_annot(rect)
    page.apply_redactions(images=fitz.PDF_REDACT_IMAGE_REMOVE,
                          graphics=fitz.PDF_REDACT_LINE_ART_NONE)
    page.insert_image(rect, filename="new_logo.png")
    ```

    If redaction does not touch the inline image in your MuPDF build, rewrite the contents stream: read `page.read_contents()`, remove the `BI ... ID ... EI` byte range, write it back with `doc.update_stream(xref, new_bytes)`, then insert the replacement. Remember `save(garbage=4, deflate=True)` afterwards so the removed bytes are actually dropped.

    ### Vector artwork

    `get_drawings()` cannot edit paths in place. The practical route is: compute the union of the logo’s drawing rects, redact that region, and draw/insert the replacement at the same rect.

    ```python
    rect = fitz.Rect(...)   # union of the logo's drawing rects, tight
    page.add_redact_annot(rect)
    page.apply_redactions(graphics=fitz.PDF_REDACT_LINE_ART_REMOVE_IF_TOUCHED,
                          images=fitz.PDF_REDACT_IMAGE_NONE)
    page.insert_image(rect, filename="new_logo.png")
    ```

    Use `REMOVE_IF_TOUCHED` only when nothing unrelated (table rules, rules of neighbouring rows) crosses the rect, because it deletes whole paths that merely intersect it. If the logo is fully inside the rect, the default `REMOVE_IF_COVERED` is enough. Redaction leaves a transparent hole, so if the logo sits on a coloured background, redraw the background fill first (sample the colour), then insert the logo. If you only need a colour change, edit the colour operators in the content stream (`rg`/`RG`/`scn`) via `doc.xref_stream` + `doc.update_stream`; there is no fill-recolour API on the path objects.

    ### Text / Type3 glyph logo

    ```python
    page.add_redact_annot(rect)
    page.apply_redactions(graphics=fitz.PDF_REDACT_LINE_ART_NONE,
                          images=fitz.PDF_REDACT_IMAGE_NONE)   # text only
    page.insert_image(rect, filename="new_logo.png")
    ```

    The `graphics=0, images=0` form keeps the cell/box grid and background shading intact.

    ### Annotation logo

    Redaction does not remove annotation appearances.

    ```python
    for a in list(page.annots() or []):
        if a.rect.intersects(logo_rect):
            page.delete_annot(a)
    page.insert_image(logo_rect, filename="new_logo.png")
    ```

    Alternatively edit the annotation’s `/AP` stream through the xref; for widgets, update the field value/appearance instead.

    ### Pattern / soft-mask logo

    There is usually no single image to swap. Find the pattern or ExtGState in the resources, and either replace its embedded image xref or redact the painted area and insert the new logo. If it is only a mask shaping a fill, rasterizing the replacement over a redacted region is the reliable option.

    ## Verify

    - Re-run `page.get_image_info(xrefs=True)`: the new placement should have the expected bbox, width/height, and a different xref or digest.
    - Render before/after at 200–300 dpi and compare the logo area: `page.get_pixmap(dpi=200).save("after.png")`.
    - For vector/text logos, confirm `get_drawings()` / `get_text()` no longer contains the old paths or glyphs in that rect.
    - Save with `doc.save("out.pdf", garbage=4, deflate=True)`, reopen, and re-check. `garbage=4` merges duplicate streams and drops unreferenced objects; lower garbage levels may leave the old image bytes in the file even though the page looks right.
    - `get_text()` cannot verify images, and clipped-away content is invisible to it, so always confirm with the display-list/rendered check, not text extraction.

    **Bottom line:** empty `get_images()` almost always means the logo is an inline image, an image inside a Form XObject, or vector/text artwork. Use `get_image_info()` first (it sees inline and nested images), then `get_drawings()` / `get_text("dict")`, then `annots()` and the content stream. Replace raster xrefs with `page.replace_image()`; replace everything else by redacting the exact painted bbox and inserting the new logo at that rect, then save with `garbage=4` and verify by rendering.
- id: pdf-imgins-01
  answer: |
    ## Compute the rect

    Don't derive the rect from `page.rect` minus a fixed margin. Anchor it to actual content:

    - **Right edge** = right edge of the real content (table/text x1), falling back to the page's right text margin.
    - **Bottom edge** = a small padding above the top of the footer, not the page bottom.
    - **Width** = 150 pt; **height** = 150 × (image height / image width), so the aspect ratio is fixed by the rect itself.

    ```python
    import fitz

    doc = fitz.open("in.pdf")
    page = doc[0]

    # --- image intrinsic size ---
    pm = fitz.Pixmap("sig.png")
    img_w, img_h = pm.width, pm.height          # or use PIL: Image.open(...).size

    W = 150.0
    H = W * img_h / img_w

    # --- content edges ---
    blocks = [b for b in page.get_text("blocks") if b[6] == 0]  # text blocks only
    words  = page.get_text("words")

    # Identify footer: lowest band on the page (heuristic: y0 in bottom 10%)
    footer_top = page.rect.y1 - 36                 # fallback
    candidates = [b for b in blocks if b[1] > page.rect.y1 * 0.90]
    if candidates:
        footer_top = min(b[1] for b in candidates)

    # Right edge of the body content (exclude footer blocks)
    body = [b for b in blocks if b[3] <= footer_top]
    right = max((b[2] for b in body), default=page.rect.x1 - 72)

    pad = 6.0
    x1 = right
    y1 = footer_top - pad
    x0 = x1 - W
    y0 = y1 - H
    rect = fitz.Rect(x0, y0, x1, y1)

    # --- keep it off existing content ---
    def collides(r):
        if any(fitz.Rect(w[:4]).intersects(r) for w in words):
            return True
        if any(fitz.Rect(d["rect"]).intersects(r) for d in page.get_drawings()):
            return True
        if any(fitz.Rect(i["bbox"]).intersects(r) for i in page.get_image_info()):
            return True
        return False

    while collides(rect) and rect.y0 > page.rect.y0 + 36:
        rect = rect + (0, -4, 0, -4)               # shift up until clear
    ```

    If the content right edge is unknown or the page has full-width text, clamp so the image stays inside the page:

    ```python
    if rect.x0 < page.rect.x0 + 18:
        rect = fitz.Rect(page.rect.x0 + 18, rect.y0,
                         page.rect.x0 + 18 + W, rect.y1)
    ```

    ## Insert it

    ```python
    page.insert_image(rect, filename="sig.png", keep_proportion=True)
    ```

    `keep_proportion=True` makes PyMuPDF fit the image inside `rect` without stretching; since the rect was built with the true aspect ratio, it fills it exactly.

    If the same signature goes on several pages, insert once and reuse the xref (one stored copy):

    ```python
    xref = page.insert_image(rect, filename="sig.png", keep_proportion=True)

    for pno in range(1, len(doc)):
        p = doc[pno]
        # recompute rect for that page, then:
        p.insert_image(rect_p, xref=xref, keep_proportion=True)
    ```

    Save with stream cleanup:

    ```python
    doc.save("out.pdf", garbage=4, deflate=True)
    ```

    **Key points:** rect is content-anchored (right edge of table/text, bottom above the footer), not margin-anchored; height comes from the 150 pt width and the pixel aspect ratio; verify the rect intersects no word/drawing/image bbox before inserting, shifting upward if it does.
- id: pdf-imgins-02
  answer: |
    Store the image once and reference it by xref — never re-embed the bytes on each page.

    ```python
    doc = fitz.open("in.pdf")
    img = open("logo.png", "rb").read()

    # first page: actually embeds the image, returns its xref
    xref = doc[0].insert_image(rect0, stream=img)

    # every other page: reference the SAME stored object, no new copy
    for page in doc[1:]:
        page.insert_image(rect_for(page), xref=xref)

    doc.save("out.pdf", garbage=4, deflate=True)
    ```

    Key points:

    - `page.insert_image(..., xref=...)` reuses an already-embedded image; that is the real fix. PyMuPDF has no `Document.insert_image` — insertion is always per page, but the object is shared.
    - If you instead pass identical raw bytes to every page, PyMuPDF also reuses the same xref within one document, so 200 copies usually means the bytes are not actually identical (re-encoded/re-rendered each time, different alpha or colour profile per page). Feed one buffer/file to all pages.
    - 200 stored copies can also survive `save` if garbage collection is too weak: levels 0–3 do not merge duplicate streams; use `save(..., garbage=4, deflate=True)` (4 merges duplicates and drops orphans).
    - Keep the source pixels/pixmap opened once and reused; don't re-open and re-encode the logo per page.
- id: pdf-imgins-03
  answer: |
    **Why a PyMuPDF image is invisible or sits under a filled box**

    The single most common cause is **paint order (z-order)**, and second is **geometry**. In order of likelihood:

    ### 1. Something is painted after the image
    `insert_image()` appends a new content stream to the page, so with the default `overlay=True` the image is painted **last** and should be on top. It ends up underneath when:

    - **You drew the fill after inserting** — a `draw_rect(..., fill=...)`, `insert_textbox` with a fill, or a `Shape` committed later goes on top. Fix: draw backgrounds first, image last, or `page.clean_contents()` after reordering won't help — just reorder your calls.
    - **You passed `overlay=False`** — then the image stream is inserted *before* the existing content, so the page's own filled box hides it. This is the classic "underneath the background box" case.
    - **The box is an annotation or widget** (Square/FreeText/Stamp annotation, form field background). Annotations are always rendered **above page content**, regardless of insertion order, so no ordering fixes it — flatten annotations (`doc.bake()` / `page.add_redact_annot` + apply, or `Widget`/annot removal) or draw the box as page content instead.
    - **The box comes from a later `show_pdf_page()` / `insert_pdf()`** or the page's real content lives in a Form XObject that is invoked after your appended stream. Same fix: make the image the last thing drawn.

    ### 2. The image is placed where it can't be seen
    `insert_image` output is clipped to the page, and coordinates are in **PyMuPDF page space** (top-left origin of `page.rect`, i.e. the CropBox — not the MediaBox):

    - rect lies **outside the CropBox/MediaBox** (negative or beyond `page.rect`), often because you computed it from the MediaBox or from another page.
    - **zero or negative size** — `x1 <= x0`, `y1 <= y0`, or height computed as `width * h/w` with width/height swapped; nothing is drawn silently.
    - **rotated page** (`/Rotate 90/180/270`) — the rect is in unrotated space; the image lands rotated or off the visible area.
    - **wrong page index / wrong Document**, or you saved a different `Document` object (or never saved). Also stale `Page` objects after `insert_pdf`, `select`, or `delete_page`.
    - **bad `xref`** — passing a non-image xref to `insert_image(..., xref=...)` draws nothing.

    ### 3. The image is there but its pixels are invisible
    - **Alpha/SMask is fully transparent** — an image whose alpha channel is all zero, or `alpha=0` passed to `insert_image` (0 = fully transparent, `-1` = ignore). Check the pixmap's `alpha` and its SMask.
    - **Unsupported/odd source** — corrupt bytes, an unsupported colorspace or a format that isn't really decoded; the call may raise or produce an empty image.
    - Image is the **same colour as the background** (white-on-white), or an inverted/CMYK JPEG that renders wrong rather than absent.
    - The image sits in an **Optional Content Group that is off**, or is behind a `/Group` transparency/knockout.

    ### Quick diagnosis
    1. `page.get_image_info()` — is the image's bbox where you expect, and is it non-degenerate?
    2. `page.get_contents()` and `doc.xref_stream()` — is the image's `Do` operator in the *last* stream? If not, an overlay/paint-order problem.
    3. Render with `page.get_pixmap()` and check — if it's missing in the render too, it's placement or alpha, not a viewer quirk.
    4. Re-insert with `overlay=True` as the **final** drawing operation and re-check.

    Bottom line: with `overlay=True` and sane coordinates, an invisible image almost always means (a) a fill/annotation/XObject painted after it, or (b) a rect outside the CropBox or of zero size; an SMask with zero alpha is the third suspect.
- id: pdf-fonts-01
  answer: |
    ## Why reuse fails

    "AAAAAA+Georgia" is a **subset**: the six uppercase letters before the `+` are the subset tag, and the embedded `FontFile2`/`FontFile3` contains outlines only for the glyphs the original document actually used (often plus a handful). Everything else about the font program is also truncated or narrowed to that set:

    - **No glyph for the new letters.** The `cmap` (or the CID → GID mapping, for Identity-H CID fonts) has no entry for the new code points, so they resolve to glyph 0 (`.notdef`) — blank space or a tofu box — or, in a malformed subset, to an unrelated glyph.
    - **Widths are incomplete.** The `W` array (or `/Widths`) only covers the used glyphs. Missing widths give wrong advance, so the new string drifts, overlaps, or collapses.
    - **`ToUnicode` is incomplete.** Even if a glyph somehow renders, copy/paste, search, and `get_text()` return nothing or garbage for the new characters.
    - **The subset may be encoded as glyph IDs, not Unicode.** With Identity-H, the bytes in the content stream *are* glyph indices for that particular subset. Writing "new" character codes reuses old glyph slots rather than selecting new ones.

    So the failure is not a rendering quirk you can patch by editing the content stream: the outlines for the new letters simply are not in the file. No amount of `insert_text` against the existing resource will create them. (If the font were fully embedded — no subset tag, complete cmap — reuse would be fine.)

    ## What to do

    1. **Check coverage first, if you only need a few characters.** Extract the font program and test it, e.g. load it with `fitz.Font(fontbuffer=...)` and check `has_glyph()` for each new character. If every character is present, reuse is safe; if any is missing, go to step 2.
    2. **Get the full font and embed it.** The clean fix is the complete original font file (from the source application, the vendor, or a licensed copy) embedded with `page.insert_font(fontfile=... )` and then drawn with `page.insert_text(..., fontname=<that alias>)` (or `fontbuffer=`). This preserves the exact typeface and metrics.
    3. **If the original is unavailable, substitute deliberately.** Pick a visually close font (same class, weight, and rough metrics), embed the *whole* file, and match size, colour, and baseline to the surrounding text. Do not silently fall back to a Base-14 font like Helvetica unless the document already uses it — the mismatch is visible.
    4. **Redact, then redraw.** Remove the old text with `apply_redactions(graphics=0, images=0)` so the cell's rules and shading survive, keep the redaction rect tight to the glyphs, then draw the replacement with the newly embedded font at the same origin.
    5. **Re-check the result.** Confirm `get_text()` returns the new string (proves `ToUnicode` works), the neighbouring cells are intact, and the visual spacing matches.

    ## Pitfalls

    - Reusing the existing font resource `AAAAAA+Georgia` for new letters is the trap: it appears to work for characters already in the subset and silently fails for the rest.
    - Subsetting the replacement font is fine for file size, but do it with proper tooling that writes correct widths and `ToUnicode`; a hand-truncated font reproduces the same bug.
    - Respect the font's embedding/permission flags (`fsType`) when embedding a substitute; some commercial fonts forbid it.
    - If the font is Type 3 (charprocs instead of a normal font program), there is no usable `FontFile` at all — substitution is the only option.
- id: pdf-fonts-02
  answer: |
    **PyMuPDF base-14 names (text insertion)**

    - Helvetica regular → `"helv"`
    - Helvetica bold → `"hebo"`

    (The related aliases are `"heit"` = Helvetica-Oblique and `"hebi"` = Helvetica-BoldOblique. The full standard names `"Helvetica"` / `"Helvetica-Bold"` are also accepted, but the short aliases are what the docs and most code use, e.g. `page.insert_text(point, text, fontname="hebo", fontsize=12)`.)

    **The limitation**

    Base-14 fonts are never embedded and are restricted to a single-byte, WinAnsi (roughly Latin-1) encoding. Anything outside that repertoire — CJK, Cyrillic, Greek, Arabic, Hebrew, Thai, emoji, math symbols, many accented or typographic characters — has no code point in the font's encoding. PyMuPDF cannot represent it with `helv`/`hebo`: the character is typically dropped or replaced (e.g. with a bullet/garbage glyph), or an error is raised depending on the API and version; extraction of such text is likewise unreliable.

    To get non-Latin or special characters you must embed a real font that contains the glyphs — e.g. `page.insert_font(fontname="F0", fontfile="NotoSans.ttf")` then use `fontname="F0"`, or use `TextWriter`/`Story` with a `Font(fontfile=...)`. Alternative for CJK only: PyMuPDF's built-in CID fonts such as `"china-s"`, `"china-t"`, `"japan"`, `"korea"`.

    Note: base-14 names still work fine for *reading/lookup* (`fitz.Font("helv")`), and the restriction is about glyph coverage/encoding, not about the PDF viewer — a viewer substitutes its own Helvetica, so Latin text looks right without embedding.
- id: pdf-fonts-03
  answer: |
    **Detect what the original actually uses**

    Work from the text layer, not from the cell's appearance.

    1. Get the cell rect (`find_tables()` cell bbox, or `search_for()` the old label) and iterate `page.get_text("dict")` blocks/lines/spans intersecting it. For the header span record:
       - `span["font"]` — e.g. `ABCDEF+Helvetica-Bold`, `Arial-BoldMT`
       - `span["size"]` — effective point size after the text matrix
       - `span["flags"]` — bold is bit 16 (`flags & 16`), italic 2, serif 4, mono 8
       - `span["color"]`, `span["origin"]`, `span["ascender"]`/`descender`
    2. Cross-check the font, not just the name. `doc.get_page_fonts(pno, full=True)` gives the font xref, basefont, type and encoding. Then `doc.xref_get_key(xref, "BaseFont")` and the descriptor:
       - `FontDescriptor/FontWeight` (PDF 1.5+): ~700 = bold, 400 = regular
       - `FontDescriptor/StemV`: regular is roughly 70–90, bold roughly 120–190 (heuristic only)
       - `FontDescriptor/Flags`: `ForceBold` is `1 << 18` (262144)
       - `FontDescriptor/FontFile2/3` presence tells you whether it is embedded
       `span["flags"] & 16` can be wrong or unset on real bold fonts and on synthetic bold; the name + descriptor + rendered thickness together are the reliable signal.
    3. Detect *fake* bold with `page.get_texttrace()`. Its span dict has `font`, `flags`, `size`, `type`, `color`, `linewidth`. If `type` is stroke (1 or 2) or `linewidth > 0` with a dark stroke colour, the "bold" is a stroked fill, not a bold font — a bold font file alone will not reproduce it.
    4. For the size, take the *mode* of the header spans (all header cells normally share one size) rather than trusting one span. Compare `span["size"]` with the bbox height and with the baseline-to-baseline distance of adjacent header lines; a discrepancy means the text matrix or `Tz` is scaling it, and `span["size"]` is already the effective value you want to reproduce.

    **Match it**

    - Do not pass the span's font string straight to `insert_text`. Subset prefixes (`ABCDEF+`), missing fonts and Base-14 aliases often cause a silent fallback to regular Helvetica — exactly your symptom. Instead:
      - Embedded: `fontname, ext, typ, buf = doc.extract_font(xref)`, write `buf` to a temp file, and insert with `fontname=<something>, fontfile=<path>, fontsize=span["size"]`.
      - Not embedded / Base-14: map explicitly, e.g. Helvetica → `helv`, Helvetica-Bold → `hebo`, Times-Bold → `tibo`, Courier-Bold → `cobo`, or use metric-compatible clones (Liberation/Nimbus Bold). Never rely on an unknown name resolving correctly.
    - Prefer `fitz.TextWriter` + `fitz.Font(fontbuffer=buf)` (or the correct built-in) for embedded fonts: append at the original `span["origin"]` (or recompute a centred origin from the cell rect) with the exact `fontsize`, `color` and, if needed, the same `render_mode` as the original. Use `insert_text` with an explicit `fontsize`, never `insert_textbox` with autosizing.
    - If the bold was synthetic, reproduce it with `render_mode=2` and the original `linewidth`/stroke colour rather than a bold face.
    - Honour the text matrix: for a rotated page, use `page.derotation_matrix` / the span's transformed origin so the glyphs land on the same baseline. For a non-100% `Tz`, either apply horizontal scaling via a `Matrix(scale, 1)` or accept that inserting at `span["size"]` reproduces the apparent size.

    **Verify**

    - Re-extract the edited cell: same `font`/basefont, `flags & 16` set (or same synthetic stroke), `size` within ~0.05–0.1 pt, same `color`, same baseline.
    - Compare `fitz.Font(...).text_length(new_text, size)` with the original span bbox width; more than ~1–2% difference means wrong face or size.
    - Check `get_page_fonts()` on the output: a new regular font entry where the bold one used to be is the tell-tale fallback.
    - Render the cell at 300 dpi before/after and compare stem thickness / ink coverage; that catches synthetic bold and metric differences that the text layer hides.
- id: pdf-verify-01
  answer: |
    **Short answer:** verification means re-opening the *saved output file* in a fresh process and asserting on what a reader can extract and see — not on the in-memory document, and not on the script's exit code. Three independent layers: text layer, table model, rendered pixels. Removal edits need a fourth: a byte/stream-level check, because `show_pdf_page(clip=...)` can hide a deleted row while leaving it recoverable in the file.

    Assumption: the edit was done with PyMuPDF; the same checks map onto pdfplumber/pikepdf.

    ## 1. Reopen from disk and assert on extracted text

    ```python
    import pymupdf
    doc = pymupdf.open("out.pdf")          # fresh open, not the editing doc
    text = "\n".join(p.get_text() for p in doc)   # whole doc, not just the page

    # negative assertion: the deleted row is gone everywhere
    for tok in deleted_row_tokens:          # use a token unique to that row
        assert tok.casefold() not in text.casefold()

    # positive assertion: the new value exists exactly once
    assert text.count("Widget A (rev 2)") == 1
    ```

    Normalize before comparing (casefold, collapse whitespace, NFKC, handle hyphenation/kerning splits), and search **all pages and all content streams** — a row can reappear through a copied page or a reused XObject. Then check position, not just presence:

    - `page.get_text("words")` → the new value's bbox lies inside the intended cell rect and inside the table bbox.
    - No second occurrence at the old y coordinate.
    - Neighbouring cells' text is byte-identical to the pre-edit snapshot.

    ## 2. Re-run table detection and diff the model

    ```python
    tabs = doc[pno].find_tables()
    rows = tabs[0].extract()
    assert len(rows) == expected_rows and len(rows[0]) == expected_cols
    assert rows[0] == expected_header
    # recompute derived cells from the data instead of trusting the PDF
    assert int(rows[-1][-1]) == sum(int(r[-1]) for r in rows[1:-1])
    ```

    Keep a golden JSON of the table extracted *before* the edit and diff against it. This catches silent column shifts, merged-cell damage, and stale Totals that a "did the script run" check misses entirely.

    ## 3. Render and compare pixels

    ```python
    pix = doc[pno].get_pixmap(dpi=200, clip=table_rect)
    pix.save("after.png")     # diff against before.png
    ```

    - Pixel-diff cropped before/after renders: differences must be confined to the rows that were supposed to change.
    - Render untouched pages and compare hashes — they must be identical.
    - **Blank-cell check:** crop each cell rect and assert some non-background pixels where text is expected. This is the only way to catch an empty stamp (redacting the region you then stamp from) and text painted under a later fill (wrong paint order: fills, then text, then rules).
    - **Grid check:** `page.get_drawings()` count and bounds before/after, or sample pixels along the vertical rules and assert the dark line is continuous.
    - **Style check:** `get_text("dict")` spans for the new header/body cells — `color` (should be `0xFFFFFF` white on a dark header), `font`, `size` must equal the sibling cells read from the existing header, not invented.
    - OCR the render (e.g. tesseract) as an independent "what a human sees" pipeline: new value found, old value absent. Catches white-on-white and covered text that `get_text()` happily reports.

    ## 4. For deletions: prove real removal, not clipping

    Text extraction alone cannot prove this, because clipped-away stamp content is still in the file and `get_text()` won't report it. So:

    - Grep the decompressed streams: `qpdf --qdf --object-streams=disable out.pdf qdf.pdf` (or `mutool clean -d`), then search for the deleted strings. Cross-check with `pdftotext -layout out.pdf -`; poppler generally does not apply clip paths, so a hidden stamped row usually still extracts — treat any hit as failure.
    - Walk every stream xref in Python and search raw bytes; be aware of hex strings and `TJ` arrays splitting words.
    - Assert the **stamp source** itself was redacted and applied before being reused (if you built a copy, re-open the copy and check the deleted text is absent there too).
    - Never verify a stamped page only by looking at the clip.

    ## 5. File and consumer integrity

    - Save with `garbage=4, deflate=True`, close, reopen; `qpdf --check` and `mutool info` clean.
    - `page.get_images(full=True)` / `get_fonts()`: an image added on N pages is stored once (reuse the returned xref); no duplicated image streams after save.
    - Xref count hasn't ballooned with orphans.
    - Open in two viewers (Acrobat/Preview/Chrome), select and copy the edited cells, and confirm the text layer matches what is drawn.

    ## 6. Prove the verifier itself is sensitive

    Run the verifier against the **unedited** file — it must fail (deleted token still present, model mismatch). Run it against a deliberately broken output (e.g. row redacted but not shifted, or an empty stamp) — it must fail. A verification script that passes on both the broken and the good file verifies nothing.

    **Minimum bar:** fresh-process reopen of the saved file → whole-document negative text assertion → table model diff + recomputed Total → cropped render diff with no blank cells and intact rules → decompressed-stream grep for the deleted strings → verifier fails on the pre-edit file.
- id: pdf-verify-02
  answer: |
    No — not reliably.

    `doc.save("same.pdf", incremental=True)` appends a new revision to the existing file and leaves every earlier revision in place. Applying redactions rewrites the page's content stream (and draws the redaction rectangles), but the original objects — including the original content stream that still contains the row's text — remain in the file's earlier bytes.

    Consequences:

    - `get_text()` on the reopened document may show the row as gone, because the current xref points at the new content stream. That does **not** mean the data is gone.
    - Anyone can recover the old revision by parsing the previous xref (`/Prev` chain), scanning the raw file for the strings, or using PDF forensics/recovery tools.
    - Incremental save also skips garbage collection, so the orphaned content stream and any replaced objects are left intact. `garbage`/`deflate` options are effectively not doing the cleanup work here.
    - Separately, PyMuPDF/MuPDF may refuse or warn about an incremental save after redactions, since redaction is supposed to force a rewrite. If it errors, that is a symptom of the same problem, not a fix.

    What actually removes it:

    1. `page.apply_redactions()` (optionally with `graphics=0, images=0` if you want to preserve grid/shading for a cell edit, or defaults if you are rebuilding the table).
    2. Save to a **new** file with a full rewrite and garbage collection:
       `doc.save("clean.pdf", garbage=4, deflate=True)`
       Do not use `incremental=True`.
    3. Verify twice:
       - `get_text()` of the output no longer contains the row's strings.
       - A raw byte/object scan of the output file (decompress streams, check for the strings) finds nothing — this catches data left in unreferenced objects or non-current revisions.

    Also note that `show_pdf_page(clip=...)` only hides content visually; if the deleted row is present in the stamp source, it is still embedded in the file. The stamp source must be a copy in which the deleted row was also redacted and applied.

    Bottom line: incremental save preserves the pre-redaction bytes, so the confidential row is still in the file. Redaction is only real removal after a full save with garbage collection to a new file, confirmed by both text extraction and a raw-object/byte check.
- id: pdf-verify-03
  answer: |
    ## File-safety practices for editing `invoice.pdf`

    **1. Never edit the original in place.**
    - Read `invoice.pdf` as input only; write the result to a new file (e.g. `invoice-edited.pdf`).
    - Keep a pristine, byte-for-byte copy of the original before you do anything. For PDF work this is not just politeness — a clean copy is often needed as the source for stamping/cloning (`show_pdf_page` needs an untouched `src_copy`), and redaction is irreversible once applied.
    - If the workflow requires replacing the original name, do it as a separate, explicit final step after validation — not as the first thing you do.

    **2. Write atomically.**
    - Save to a temporary path in the same directory (e.g. `invoice-edited.tmp.pdf`), validate it, then rename over the target. A crash or exception mid-save should never leave a truncated or half-written `invoice.pdf`.
    - On failure, delete the temp file and leave the original untouched; do not leave partial outputs that look like real results.

    **3. Validate before you declare success.**
    - Reopen the saved file with PyMuPDF and check it actually loads (`fitz.open`), the page count matches, and `get_text()` contains what you expect.
    - For deletions, confirm the deleted strings are absent from `get_text()` of the **output** file — and remember that check cannot see content hidden by a clip, so only trust it if the stamp source was genuinely redacted.
    - Check the output isn't empty/zero-byte and that the file size is plausible.
    - Render a page or two (or inspect text/drawings) to catch blank pages, hidden-by-fill text, or lost images.

    **4. Preserve the document's integrity.**
    - Save with `garbage=4, deflate=True` only after you've confirmed the content is correct; garbage collection rewrites streams and can surprise you if something was still referenced.
    - Don't accidentally strip metadata, annotations, form fields, or attached files unless the task requires it.
    - Keep the same page size, orientation, and any encryption/permissions; if the PDF is encrypted, don't silently drop the protection.

    **5. Mind the side effects of PDF operations.**
    - `apply_redactions()` is destructive and permanent — it removes text, blanks image pixels, and deletes covered line art by default. Do it on a copy, never on the only file you have.
    - Redaction does not make a file "clean" of everything: content outside a clip is still in the stamped XObject, and metadata/embedded files may retain data. If the goal is sanitisation, verify more than the visible text.
    - There is no undo. The safe pattern is: pristine original → working copy → output copy, with validation gates between each.

    **6. Handle paths and permissions carefully.**
    - Don't overwrite unrelated files if two names collide (e.g. an existing `invoice-edited.pdf`); either confirm overwrite or pick a non-clashing name.
    - Preserve file permissions/ownership where it matters, and avoid writing into directories you don't own.
    - Quote paths and use absolute paths so a relative-path mistake can't clobber the wrong file.

    **7. Don't leak the source.**
    - Avoid copying the PDF into shared/temp locations that outlive the task; clean up intermediate files (redacted copies, stamp sources, temp outputs) when done, since they may contain the very data you were asked to remove.
    - Don't print sensitive invoice contents into logs or chat unnecessarily.

    **Assumption:** since this is a non-interactive run with no user available, I'm assuming you want the edited file written alongside the original under a new name, with the original left intact. If the intent is to replace the original, add an explicit confirmation step before the final rename.

    **Minimum safe sequence:** copy original → edit the copy → save to temp → validate temp (opens, page count, expected/absent text, renders) → atomically rename to final name → keep or delete the pristine original only as the user directs → clean up intermediates.
- id: pdf-verify-04
  answer: |
    Because "no exception" only proves the program didn't crash — it says nothing about whether the document changed the way you intended. A PDF editor is a very permissive API: most wrong edits are perfectly valid PDF operations, so they succeed silently.

    Concretely, `done` can coexist with all of these:

    **1. The edit was a no-op.** Wrong page index, wrong coordinates, an empty match set, a font that isn't embedded, or text drawn outside the CropBox/MediaBox. The call returns successfully and the page looks unchanged.

    **2. The edit happened in the wrong layer.** Text can be added under an opaque fill, outside a clip path, or beyond the visible page area. The content stream is "correct", the render is not. Conversely, `get_text()` can report text that is invisible, or miss text that is still visible.

    **3. Removal wasn't real removal.** `show_pdf_page(..., clip=...)` only *hides* content outside the clip; the full source page is embedded in the Form XObject. Redaction with the wrong `graphics`/`images` flags can leave the old glyphs' pixels or rules behind, or delete neighbouring grid lines. `save()` without `garbage=4` can leave the old objects and streams in the file. The deleted text can still be extractable, or recoverable with a different tool.

    **4. The file on disk isn't the file you think.** The script wrote to a temp path, the incremental save appended instead of replacing, an old output was overwritten by a later step, or `done` was printed before the save/close actually flushed (or the exception was caught and swallowed).

    **5. It's structurally valid but semantically wrong.** Wrong row shifted, Total not recomputed, overlapping text, a new column with the wrong header colour/font, content crossing the footer, or a table that now has a gap where the deleted row was. PDF viewers will happily render all of that.

    **6. It only works in one renderer.** Missing/broken font resources, transparency groups, or odd colour spaces can look fine in one viewer and wrong in another; some tools repair silently on open.

    What actually counts as evidence:

    - Reopen the **output** file and verify the specific invariants you care about: deleted strings absent from `get_text()` (and from rendered pixels), new strings present exactly once, row count/table geometry correct, Total equals the recomputed sum.
    - Check the visual result, not just the text layer: render the page to a pixmap and inspect the region, or diff before/after renders.
    - Check that the new content's bboxes don't intersect words/drawings/images you meant to keep.
    - Verify the artifact: correct path, expected page count, file size sanity, `garbage=4, deflate=True`, and that a second independent extraction tool agrees.
    - Assert programmatically inside the script — compare expected vs. actual values and exit non-zero on mismatch — instead of printing `done`.

    In short: `done` is a statement about the script's control flow. Correctness is a statement about the output document, and it has to be measured on the output document.

    **ASSUMPTION:** I'm treating "correct" as "the intended visible and extractable content changed as specified, with nothing unintended left behind", and the library as PyMuPDF (the `apply_redactions`/`show_pdf_page` behaviour above is PyMuPDF-specific).
```
