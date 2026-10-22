# deepseek-v4.1-flash — pdf answers (re-benchmark after provider update)

Model: `opencode-go/deepseek-v4.1-flash` (provider-stripped `deepseek-v4.1-flash`),
re-run 2026-10-21 after opencode-go updated the model behind this id
(`model_version: "4.1-2026-10-21"`). This REPLACES the earlier baseline for this id;
the previous answers are in git history.

Closed-book barrier:
- Each of the 36 questions in `docs/okf/_prompts/pdf.md` was a fresh headless
  `ocode run -m opencode-go/deepseek-v4.1-flash -effort med` session (`bin/ocode`, built
  2026-10-20) under ocode's real agent system prompt.
- Fake HOME: no global CLAUDE.md/AGENTS.md, no memory, no persisted roots. Its only config
  denies every builtin tool and DISABLES the five existing `*-tuning-deepseek-v4.1-flash`
  Kaizen skills (plus `conduct-tuning-deepseek-v4-flash`), so this is a true no-skill
  baseline. Probe before the sweep confirmed: no tools advertised, no Model-Specific
  Directives block. The generic `[ocode:provider]` DeepSeek output-discipline block and the
  bundled skill-catalogue digest were still present (every user gets them).
- cwd: an empty dir named `work`; stdin `/dev/null`.
- Audit: every session sqlite checked after the sweep — 1 user + 1 assistant message,
  zero tool calls.
- Prompt per question: closed-book instruction + the one question; plain-text reply,
  wrapped into YAML below verbatim.

```yaml
- id: pdf-model-01
  answer: |
    ## What the PDF actually contains

    Nothing called a "table." PDF is a page-description format, not a document model. A PDF page is a **content stream** of drawing operators executed against a coordinate system:

    - Text is drawn as glyph runs inside `BT … ET` blocks, positioned with `Tm`/`Td`/`TD`/`T*` and emitted with `Tj`/`TJ`. Each cell's string is usually its own little text object at absolute coordinates. There is no notion that two strings are in the same row, or that a string is a "cell."
    - Rules and borders are vector paths: `re` (rectangle) or `m`/`l`/`c` followed by `S` (stroke) or `f` (fill). A single path can draw many grid lines at once.
    - Fonts are often **subsetted** with custom encodings; recoverable text depends on `ToUnicode` CMaps. Extraction order is content-stream order, which frequently does **not** match visual reading order.
    - Everything is wrapped in compressed object streams (`FlateDecode`), possibly with incremental updates, and a page may be `q`/`cm`-transformed and clipped.

    So a "row" is an emergent visual artifact: a horizontal band of y-coordinates where some glyph baselines happen to line up, bracketed by stroked horizontal segments. If the table arrived as a scan, it isn't even text — it's pixels.

    ## What that implies for the edit

    You cannot issue a structural "delete row 3." The edit has to be reconstructed geometrically and applied as content-stream surgery:

    1. **Locate the row.** Decompress the page, extract glyph bounding boxes (e.g. pdfplumber/PyMuPDF), cluster by baseline y, and identify the horizontal rule segments bounding that band. Cross-check against the drawn paths.
    2. **Decide the semantics.** Remove the row and close the gap, or blank the cells and leave the space? These produce different edits. If the table spans pages, removing a row may repaginate or leave a short page.
    3. **Excise the operators.** Drop the `Tj`/`TJ` operations whose glyph boxes intersect the row band, and drop the path segments for the row's rules — but note a rule is often **shared**: row 2's bottom border and row 3's top border may be the same stroke, so removing it damages the row above. You may have to rewrite the path rather than delete it.
    4. **Reflow.** Translate the remaining rows' text matrices and rule coordinates upward (`Tm`/`Td` edits or a `cm` translation), keeping baselines and column x-positions consistent, and re-stroke borders.
    5. **Fix the rest of the file.** If it's a tagged PDF, the `StructTreeRoot` (`/Table`, `/TR`, `/TD`) must be updated or it will disagree with the page. Anything downstream — bookmarks, links, annotations, page count — may shift. A digital signature is invalidated by any byte change.

    ## Practical caveats

    - **White-box "deletion" is not deletion.** Covering the row with a filled rectangle hides it visually while the glyphs remain in the content stream, fully extractable. If the requirement is real removal (redaction, privacy), the text operators must actually be removed and the file rewritten.
    - **Fragile paths.** Byte-level editing of a compressed, cross-referenced file is error-prone; go through a library that parses and re-serializes (pikepdf/qpdf, PyMuPDF, pdf-lib).
    - **Frequently the right answer is to not edit the PDF.** If the source (Word, LaTeX, HTML, spreadsheet) exists, edit that and re-export. If the table is scanned, you need OCR plus image inpainting or a full re-render — there are no rows to delete.

    **Assumption:** "delete" means remove the row from the visual flow, not merely hide it, and the PDF is vector/text rather than a scan. If it's a scan or a signed document, the answer changes materially and I'd say so up front.

    If you want the short version: a PDF gives you positioned ink, not rows — so "delete row 3" is really "find the glyphs and strokes that look like row 3, remove them, and redraw the grid."
- id: pdf-model-02
  answer: |
    ## Why a white rectangle isn't an edit

    A PDF is a page-description program, not a flat canvas. The "text" is a sequence of text-showing operators (`Tj`, `TJ`, `'`, `"`) inside `BT`/`ET` text objects in the page's content stream, referencing embedded font subsets. Drawing a white rectangle just appends more painting operators (a `re` + `f`, or an annotation appearance stream) later in paint order. Nothing in the original content is removed or changed.

    Consequences:

    - **The old text is still fully present.** It remains copyable, searchable, selectable, extractable (`pdftotext`, `get_text`, text-based indexing), translatable, and readable by screen readers and accessibility tools. Anyone can delete the rectangle, inspect the raw stream, or drag-select to recover it.
    - **The appearance and the text layer now disagree.** The page shows new text; the underlying content says something else. Search results, copy-paste, link/annotation rectangles, the tagged-PDF structure tree, alt text, bookmarks, and outlines all still point at the original wording.
    - **It isn't a layout operation.** A real edit reflows or repositions surrounding text; an overlay leaves the original glyph advances in place, producing gaps, misalignment, and kerning/font mismatches (you often have to substitute a font because the embedded subset lacks the glyphs you need).
    - **It creates no revision history and no semantic change.** It's compositing, not authoring. There's no tracked change, no version, no distinction between the amendment and the page.
    - **It's the classic failed-redaction failure.** "White box over the SSN" is not redaction; the data is recoverable, and this has caused real breaches and court/regulatory findings. Related failure modes: transparent or overprint fills that don't fully cover in every renderer/printer, blend modes and transparency groups, and annotations that can be toggled off or excluded from printing/flattening.
    - **Annotations are separate objects.** If the rectangle is a `Square`/`FreeText` annotation, it isn't page content at all — it can be moved, deleted, listed, or dropped on flatten/print.

    ## What to use instead

    **For real text changes:** a PDF editor that rewrites the page content stream — Adobe Acrobat Pro (Edit PDF / Edit Text & Images), Foxit, Nitro, PDF-XChange; programmatically PyMuPDF (`page.get_text("rawdict")` + content-stream rewriting, or its redaction API), pdfBox/iText content-stream editing, pikepdf/qpdf for structural surgery. This means removing or replacing the actual text operators, fixing font resources, encoding, glyph widths, and repositioning the following text.

    **For removal of sensitive content:** a true redaction tool (Acrobat Redact, PyMuPDF `add_redact_annot` + `apply_redactions`, iText redaction). Redaction must *delete* the covered text, images, and vector art from the content stream, strip the relevant metadata/XMP and optionally annotations/attachments, and rewrite the file — not cover it.

    **When text can't be cleanly reconstructed** (scans, broken fonts, complex layout): rasterize/flatten the affected page or region so the old text is genuinely gone, then OCR to restore a new, accurate text layer. That trades fidelity and file size for integrity; it's still an edit of the content, unlike a white patch.

    **For forms:** fill the `/AcroForm` field (widget) rather than painting text over the page. **For commentary:** use proper markup annotations — but don't mistake them for editing.

    **Always verify and keep provenance:** extract text again and search for the removed string, decompress streams (`qpdf --stream-data=uncompress`) and confirm the string is absent, check the structure tree/accessibility, and save the result as a new version. Never claim a document was edited when only its rendering was patched.

    **Where a white patch is acceptable:** disposable visual mockups or screenshots where no text layer matters — and only if the result is exported as an image or properly flattened, so no hidden text survives.
- id: pdf-model-03
  answer: |
    **Short answer:** because the bytes in a PDF content stream are not the characters you see. A PDF stores *glyph codes* chosen by the font, not ASCII, and the visible string is usually fragmented across several text-showing operators. So "22.50" often does not exist as a contiguous, literal, uncompressed byte sequence — and even when it does, changing its length breaks the stream, the layout, or the font.

    Assumption: the question is about editing the actual content-stream bytes (with pikepdf, after decoding), not about a hypothetical high-level text-editing API.

    Main reasons a naive byte replace fails or corrupts:

    1. **The stream is compressed / indirect.** Content streams are typically `FlateDecode`d, and may live inside an object stream. Searching the raw file bytes finds nothing; you must decode first. Replacing bytes in a compressed stream without re-encoding leaves the filter length and data inconsistent.

    2. **Text is encoded as font glyph codes, not ASCII.** For simple fonts the byte values come from the font's `/Encoding` (often with `/Differences`); for composite fonts (`Type0` + `Identity-H`) every glyph is a 2-byte CID unrelated to Unicode. The glyphs for `2`, `5`, `.`, `0` may be `<0013>`, `<0021>`, etc. — literal `"22.50"` never appears.

    3. **The font subset may not contain the new glyphs.** Subsetted fonts include only glyphs actually used. `22.50` uses `2`, `.`, `0`; writing `54.00` needs `5` and `4`, which may have no CID/glyph in the subset. You get blanks, `.notdef` boxes, or wrong characters. You also have to keep `/Widths`, `CIDToGIDMap`, and any `/ToUnicode` consistent.

    4. **The text is split across operators.** Real content often looks like `[(2)-18(2)-10(.)-5(5)-12(0)] TJ`, or uses several `Tj`/`'`/`"` calls, font switches (`Tf`), and explicit positioning (`Tm`/`Td`/`TD`), plus `Tc`/`Tw`/`Tz` spacing. There is no single string to find, and no single string to substitute.

    5. **String syntax has many equivalent forms.** Literal strings `( … )` use escapes (`\(`, `\)`, `\\`, `\ddd` octal, line continuations); the same text can be a hex string `<…>`. A blind replace can unbalance parentheses or break an escape, after which the parser misreads the rest of the stream as string data — a classic way to corrupt a page.

    6. **Length and structure must be updated.** Replacing `22.50` with `54.00` changes stream length. `/Length` must be corrected; the cross-reference table / xref stream must be regenerated (or an incremental update written); in-place edits inside object streams are not possible. Naively patching bytes leaves stale offsets and a broken file.

    7. **Binary collisions.** The byte pattern may occur inside an inline image (`BI … ID … EI`), an embedded ICC profile, or other binary payload in the same stream. Replacing it corrupts image data or non-text operators. The same substring can also match inside longer text (`122.50`, `22.500`) or in other objects (annotations, metadata, other pages).

    8. **The text may not be text at all.** It could be vector outlines, a Type3 font's glyph procedures, an image, or an annotation appearance stream (`/AP`) rather than the page content stream. There is no string to replace.

    9. **Layout consequences even when it "works".** Glyph advance widths differ for `2` vs `5`, so following text on the line shifts, overlaps, or falls out of a right-aligned/centered field or table column. With `TJ` kerning already baked in, manual adjustments fight the existing spacing.

    Practical implication: a correct edit means decode → parse the content operators → resolve the font and encoding (and `/ToUnicode`) → check that the target glyphs exist in the font (embedding/subsetting if not) → rewrite the `Tj`/`TJ` operands (and any width or kerning values) → fix `/Length`, widths, and xref via a library rather than byte patching. If you only control the raw bytes, the safe move is to keep the replacement the same byte length (same number of glyph codes) and only do it when you have verified the exact encoded form — otherwise the page will parse badly, show garbage/blank glyphs, or silently change layout.
- id: pdf-model-04
  answer: |
    ## Two coordinate systems

    **Raw PDF user space** — what's stored in the file and in content streams:

    - unit = 1 point = 1/72 inch
    - origin = bottom-left of the page box, **+y up**
    - boxes (`/MediaBox`, `/CropBox`, `/Rotate`) are expressed here; content is clipped to the CropBox (which defaults to the MediaBox)

    **PyMuPDF (MuPDF) page space** — what `get_text`, `search_for`, `get_drawings`, and the drawing/annotation APIs speak:

    - same unit
    - origin = **top-left of the CropBox**, **+y down**
    - rects are `(x0, y0, x1, y1)` with `y0` = visual top, `y1` = visual bottom

    For an unrotated page with CropBox `(cx0, cy0, cx1, cy1)`:

    ```text
    x_page = x_pdf - cx0
    y_page = cy1  - y_pdf          # the y flip

    x_pdf  = x_page + cx0
    y_pdf  = cy1  - y_page
    ```

    So a word tuple `(x0, y0, x1, y1, word, ...)` is already in page space and `y0` is the top of the word — `page.draw_rect(fitz.Rect(w[:4]))` lands exactly on it.

    `page.rect` is always `(0, 0, width, height)` derived from the **CropBox**, not the MediaBox. `page.transformation_matrix` (and its inverse `~page.transformation_matrix`) is the PDF→page mapping; for text insertion/annotation writing PyMuPDF does the inverse conversion for you.

    ## Offset CropBox

    This is the part that silently breaks code that mixes libraries. PyMuPDF re-bases everything on the **CropBox corner**:

    - word coordinates start at 0 at the visible top-left, even if the CropBox sits at, say, `(100, 50)` inside the MediaBox
    - the absolute numbers are still reachable via `page.cropbox`, `page.mediabox`, and `page.cropbox_position`
    - content that exists in the MediaBox margin outside the CropBox can come back with negative or `> page.rect` coordinates (and is invisible in renders)
    - pypdf / pdfplumber / qpdf / raw streams report MediaBox-based, y-up PDF coordinates — to compare you must add the CropBox offset and flip y, or go through `~page.transformation_matrix`
    - `page.set_cropbox(...)` re-bases the coordinate system again, so coordinates captured before the call are no longer valid afterwards

    ## /Rotate

    `/Rotate` is purely a display rotation (multiples of 90°, clockwise), exposed as `page.rotation`. PyMuPDF bakes it into page space:

    - `page.rect` becomes `(0, 0, h, w)` for 90/270 — width and height swap
    - `page.get_pixmap()` renders at that rotated size
    - text extraction and pixmaps agree: scale a word rect by `dpi/72` and it overlays the rendered image with **no extra rotation correction**

    Matrix helpers:

    - `page.rotation_matrix` — unrotated page coords → displayed (rotated) page coords
    - `page.derotation_matrix` — displayed → unrotated, i.e. `~page.rotation_matrix`
    - `page.transformation_matrix` — PDF coords → page coords (CropBox offset + y flip)

    For a 90° clockwise `/Rotate`, with `W, H` the unrotated CropBox dimensions:

    ```text
    display = (H - y_unrot, x_unrot)     page size becomes H × W
    180°    = (W - x,       H - y)
    270°    = (y,           W - x)
    ```

    or just `rect * page.rotation_matrix`.

    What this means in practice:

    1. **Drawing with extracted rects:** do nothing special. `get_text`/`search_for` and `draw_rect`/`add_*_annot`/`TextWriter` share the page space; PyMuPDF un-rotates when it writes into the (always unrotated) content stream. If something is offset or mirrored by exactly one page dimension, you're off by one `rotation_matrix` / `derotation_matrix`.
    2. **Coordinates coming from elsewhere** (raw PDF numbers, another library, a stored annot): they are unrotated — convert into page space first, applying the flip/offset and then the rotation.
    3. **Going the other way** (word rect → raw PDF): un-rotate with `page.derotation_matrix`, then undo the flip/offset with `~page.transformation_matrix`, or use the explicit formulas above (remember the flip swaps y0/y1).
    4. **`annot.rect` and content-stream coordinates are unrotated**, even when the page has `/Rotate`; use `page.rotation_matrix` to compare them with extracted text. `page.set_rotation()` only changes `/Rotate`, never the content.

    Caveat worth stating: the geometry above is stable, but a few individual PyMuPDF helpers have had version-dependent conventions (notably annotation rects and some image/xref geometry). If a result looks wrong only on rotated pages, check `page.rotation` and do a one-line round-trip — on an unrotated page `page.rect * ~page.transformation_matrix` should land inside `page.mediabox` — before trusting any assumption about which space that specific method used.
- id: pdf-locate-01
  answer: |
    ## Short answer

    Use a table-structure library that keeps geometry, not a plain text extractor. For native (vector) PDFs the two practical options are **pdfplumber** and **PyMuPDF**; both return a table bbox plus per-row and per-cell rectangles. If the table has no ruling lines, switch the detection strategy to text clustering; if the PDF is scanned, you need raster table detection + OCR (or a cloud document API).

    ## pdfplumber (pure Python, most tunable)

    `page.find_tables()` returns `Table` objects; each table has `.bbox`, `.rows`, and each row exposes `.cells` as `(x0, top, x1, bottom)` tuples (or `None` for a merged/absent slot). Coordinates are PDF points, origin top-left, y increasing downward.

    ```python
    import pdfplumber

    with pdfplumber.open("in.pdf") as pdf:
        page = pdf.pages[0]

        found = page.find_tables({
            "vertical_strategy": "lines",      # "lines_strict" | "lines" | "text" | "explicit"
            "horizontal_strategy": "lines",
            "snap_tolerance": 3,
            "join_tolerance": 3,
            "intersection_tolerance": 3,
            # for borderless tables:
            # "vertical_strategy": "text", "min_words_vertical": 3,
        })

        for t in found.tables:
            print("table bbox:", t.bbox)        # (x0, top, x1, bottom)

            for row in t.rows:
                cells = [c for c in row.cells if c is not None]
                row_bbox = (
                    min(c[0] for c in cells), min(c[1] for c in cells),
                    max(c[2] for c in cells), max(c[3] for c in cells),
                )
                print(" row:", row_bbox)
                for c in row.cells:
                    print("  cell:", c)         # (x0, top, x1, bottom) or None

            print(t.extract())                  # text grid, aligned to the cells
    ```

    Notes:
    - `page.extract_tables()` gives you text only — use `find_tables()` when you need boxes.
    - `table.cells` is also available as a flat list if you do not care about row grouping.
    - If detection fails, try `"lines_strict"` first, then `"lines"`, then `"text"` — mixed strategies via `explicit` line arrays are the last resort.
    - Rotated pages and non-zero CropBox origins can shift coordinates; check `page.rotation` and `page.cropbox`.

    ## PyMuPDF / fitz (fast, also cell-level)

    `page.find_tables()` (available since ~1.23) returns a `TableFinder`; each table has `.bbox`, `.rows`, and each `TableRow` has `.cells` as `pymupdf.Rect` objects (or `None`). Coordinates are points with top-left origin, same convention as pdfplumber.

    ```python
    import pymupdf

    doc = pymupdf.open("in.pdf")
    page = doc[0]

    finder = page.find_tables(strategy="lines")   # try "text" for borderless tables
    for tab in finder.tables:
        print("table:", tab.bbox)                 # Rect(x0, y0, x1, y1)

        for r in tab.rows:
            print(" row:", r.bbox)
            for cell in r.cells:
                if cell is not None:
                    print("  cell:", cell)        # Rect(x0, y0, x1, y1)

        data = tab.extract()                      # list[list[str]]
    ```

    The exact keyword names (`strategy=`, `clip=`) and the presence of `TableRow.bbox` vary slightly by release; if `r.bbox` is missing, union the non-`None` `r.cells`. You can also overlay results for validation:

    ```python
    for tab in finder.tables:
        page.draw_rect(tab.bbox, color=(1, 0, 0), width=0.5)
        for r in tab.rows:
            for c in r.cells:
                if c: page.draw_rect(c, color=(0, 0, 1), width=0.3)
    page.get_pixmap(dpi=150).save("check.png")
    ```

    ## Camelot

    Camelot returns table-level and cell-level boxes, but in **PDF coordinates with origin bottom-left, y up** — the opposite convention from the two libraries above. `lattice` is for ruled tables, `stream` for whitespace-separated ones.

    ```python
    import camelot

    t = camelot.read_pdf("in.pdf", pages="1", flavor="lattice")[0]
    print(t._bbox)     # table bbox
    print(t.cells)     # cell bboxes
    print(t.df)        # pandas DataFrame of text
    ```

    Per-row/per-cell object attributes have changed across Camelot versions, so verify against your installed release; the table `_bbox` and the flat `cells` list are the stable parts. Remember to flip y when comparing with pdfplumber/PyMuPDF boxes.

    ## Tabula

    - `tabula-py` / CLI JSON: table region only (`top`, `left`, `width`, `height`) plus text grid — **no per-cell boxes**.
    - `tabula-java` directly: `Table.getRows()` gives `RectangularTextContainer` cells with `getTop()/getLeft()/getWidth()/getHeight()`, so cell boxes are available from the JVM API, not the Python wrapper.

    ## If there are no ruling lines (borderless tables)

    Detection has to come from text geometry:

    1. Extract words/chars with boxes (`pdfplumber` `extract_words()` / `page.chars`; PyMuPDF `page.get_text("words")`).
    2. Cluster x-coordinates: consistent whitespace gutters that persist across most lines define column boundaries.
    3. Cluster y-coordinates (top/bottom of words or baselines) to define rows.
    4. Treat the Cartesian product as cells, then assign each word by its center point (or majority overlap) to a cell.
    5. Validate: column count stable across rows, header row present, no cell containing implausible text runs.

    This is exactly what `vertical_strategy="text"` / PyMuPDF `strategy="text"` do; use them before writing your own clustering.

    ## DIY from vector primitives (when libraries misdetect)

    For ruled tables you can reconstruct the grid yourself:

    - Collect horizontal and vertical segments (`page.lines`, `page.rects`, `page.edges` in pdfplumber; `page.get_drawings()` in PyMuPDF). Thin filled rectangles are often rulings too.
    - Snap/merge collinear segments within a tolerance (1–3 pt).
    - Cluster the y-values of horizontals into row boundaries and the x-values of verticals into column boundaries.
    - A cell exists at `(x0,y0,x1,y1)` only if all four boundary segments actually cover that span — this naturally handles merged cells and gaps.
    - Map text by bbox overlap/center containment, then optionally snap to detected grid lines.

    ## Scanned / image-only PDFs

    There is no vector geometry, so:

    1. Rasterize at 200–300 DPI (`page.get_pixmap(dpi=300)`).
    2. Run table structure recognition: img2table, PaddleOCR PP-Structure, Docling (TableFormer), Microsoft Table Transformer, or a cloud API.
    3. OCR each region (Tesseract `image_to_data` gives word boxes).
    4. Convert pixel boxes back to PDF points by multiplying by `72 / dpi`.

    Cloud/OCR services are often the most reliable here because they return cell geometry directly: AWS Textract `AnalyzeDocument(FeatureTypes=["TABLES"])` gives each `Cell` a `RowIndex`, `ColumnIndex`, `RowSpan`, `ColumnSpan`, and a `Geometry.BoundingPoly`; Azure Document Intelligence and Google Document AI return analogous per-cell polygons/bounding regions.

    ## Practical caveats

    - **Coordinate systems differ**: pdfplumber and PyMuPDF are top-left/y-down; Camelot and raw PDF user space are bottom-left/y-up. Mixing them silently produces mirrored boxes.
    - **CropBox/MediaBox offsets and `/Rotate`** can shift or rotate boxes; normalize to the visible page before storing.
    - **Merged cells**: pdfplumber returns `None` for missing slots; PyMuPDF similarly. Store spans explicitly rather than assuming a full rectangular grid.
    - **Tolerances matter**: 1–3 pt snapping/intersection tolerances handle slightly misaligned rules; too large merges adjacent rows/columns.
    - **Always validate visually** by drawing the rects back onto the page and rendering a PNG — table detection is heuristic, and silent off-by-one-column errors are common.
    - If you only need text, `extract_tables()`/`tabula` is fine; the moment you need boxes, use the object model (`find_tables()` or tabula-java).
- id: pdf-locate-02
  answer: |
    Assuming PyMuPDF (`fitz`) / `page.search_for` semantics.

    ## Why `search_for("Widget C")` is not enough

    `page.search_for()` returns a **tight bbox around the matched glyphs only** — not a row. Concretely:

    1. **It's text-only, not layout.** The rect spans the characters of "Widget C" (roughly ascender-to-descender). It excludes cell padding, the rest of the row's cells, column separators, row background fill, and the table borders. Redacting that rect leaves everything else in the row intact — which is usually not what "remove the row" means.

    2. **It matches contiguous text runs, not visual rows.** A cell like "Widget C" may be split across spans/font runs, have different spacing, be hyphenated, or be laid out as separate words. `search_for` can return empty (or several fragments) even though the row visibly says "Widget C". Multi-line cells never match as one rect.

    3. **No row identity or disambiguation.** The string may appear in a header, footnote, repeated table on another page, or a legend. `search_for` gives you a list of rects with no notion of which row/table they belong to. You can't tell page/table context from the rect alone.

    4. **No vertical extent.** A row's height is defined by its tallest cell and the surrounding rules/padding, not by one cell's text bbox. Without neighboring text you don't know `row_y0`/`row_y1`, so you can't safely clear the whole band.

    5. **Glyph bbox can under-cover.** Descenders, accents, superscripts, and rotated text can fall outside the returned rect (use `quads=True` for rotated text), so a naive redaction can clip or miss pixels.

    6. **Redaction ≠ deletion of graphics.** `add_redact_annot(rect)` + `apply_redactions()` removes text/graphics *intersecting* the rect. If the row's border lines, shading, or background image extend beyond your tight rect, they survive. Conversely, the tight rect's interaction with ascenders/descenders is fragile.

    ## How to get the full row region

    Pick one of these, depending on whether the PDF has real table structure:

    **A. Use the table API (preferred when it works)**

    ```python
    tabs = page.find_tables()
    for tab in tabs:
        for row in tab.rows:
            cells = [c for c in row.cells if c]          # cell bboxes
            text = " ".join(tab.extract()[row.row][i] or "" for i in range(len(cells)))
            if "Widget C" in text:
                row_rect = fitz.Rect(row.bbox)            # full row, table width
                # or: rects_to_redact = [fitz.Rect(c) for c in cells if c]
    ```

    `row.bbox` gives the full row band; individual `row.cells` let you clear only selected columns.

    **B. Build the row band from words (works without table detection)**

    ```python
    words = page.get_text("words")   # (x0,y0,x1,y1, word, block, line, wordno)

    # 1. anchor: the match, as a sanity check
    anchors = page.search_for("Widget C")
    assert anchors, "text not found as a run"

    # 2. anchor vertical band (use the anchor's y-range, not its x-range)
    a = anchors[0]
    y0, y1 = a.y0, a.y1
    tol = 1.0                         # or ~20-30% of line height

    # 3. every word whose vertical center sits in that band = same visual row
    row_words = [w for w in words
                 if (w[1] + w[3]) / 2 >= y0 - tol and (w[1] + w[3]) / 2 <= y1 + tol]
    ```

    Then union those word rects and expand:

    ```python
    row_rect = fitz.Rect(row_words[0][:4])
    for w in row_words[1:]:
        row_rect |= fitz.Rect(w[:4])

    # expand to table/page extent and add padding
    pad = 2
    row_rect = fitz.Rect(row_rect.x0 - pad, row_rect.y0 - pad,
                         row_rect.x1 + pad, row_rect.y1 + pad)
    ```

    If you want the full table width rather than just the words present, clamp to `tab.bbox` or page margins:

    ```python
    row_rect.x0 = tab.bbox.x0
    row_rect.x1 = tab.bbox.x1
    ```

    **C. Use line/block structure**

    `page.get_text("dict")` → `block["lines"][i]["bbox"]` gives the line band; find the span whose text contains "Widget C", take that line's bbox, and union with other lines at the same y-range for multi-line rows.

    ## Redact and verify

    ```python
    page.add_redact_annot(row_rect, fill=(1, 1, 1))
    page.apply_redactions(images=fitz.PDF_REDACT_IMAGE_PIXELS,
                          graphics=fitz.PDF_REDACT_LINE_ART_REMOVE_IF_COVERED)
    ```

    - If borders/shading sit outside the text, either widen `row_rect` to the table bbox or remove the relevant `page.get_drawings()` explicitly.
    - Always verify: `page.get_text()` should no longer contain "Widget C" *and* the row band should be visually clean; also scan for leftover fragments (e.g. `"Widget"` or `"C"` alone) since spans can split.
    - If multiple occurrences exist, filter by page index and compare `anchor` positions against the candidate row rects before redacting, so you don't nuke a header or another page's table.

    **Assumptions:** PyMuPDF API, text-based (not scanned) PDF, horizontal text. For rotated text use `search_for(..., quads=True)` and treat quads as the anchor; for scanned pages you'd need OCR word boxes as the "words" source.
- id: pdf-locate-03
  answer: |
    ## Where the styling lives

    PyMuPDF doesn't keep a "text object" model like python-docx. Every style attribute is attached to the **span** returned by the text extractor, so the answer is: extract the dict/rawdict (or trace) for the page and read the span fields.

    ```python
    import pymupdf  # or: import fitz

    page = doc[0]
    d = page.get_text("dict")          # blocks -> lines -> spans
    ```

    Container shape:

    - `block`: `type` (0 = text, 1 = image), `bbox`, `number`
    - `line`: `bbox`, `wmode` (0 horizontal, 1 vertical), `dir` (writing direction vector), `spans`
    - `span`: the style record (below)

    ## The span fields you need

    | Field | Meaning |
    |---|---|
    | `font` | Base font name as stored in the PDF, e.g. `"Helvetica-Bold"`, usually with a sub-set prefix like `ABCDEF+TimesNewRomanPSMT` |
    | `size` | Font size in points (glyph scale, not line height) |
    | `color` | Packed sRGB integer `0xRRGGBB` |
    | `alpha` | Opacity 0–255 (present in newer versions; `.get("alpha", 255)`) |
    | `flags` | Bit field: superscript, italic, serif, monospaced, bold |
    | `origin` | `(x, y)` of the **baseline** origin of the span's first glyph |
    | `bbox` | Span rectangle |
    | `ascender`, `descender` | Normalised vertical extents relative to `size` |
    | `text` | The span string |

    `rawdict` gives you the same span fields plus a `chars` list, where each char has its own `origin` and `bbox` — use it when you need a per-glyph baseline inside a line with mixed superscripts or kerning shifts.

    ```python
    s = ...  # a span
    r, g, b = (s["color"] >> 16) & 255, (s["color"] >> 8) & 255, s["color"] & 255
    color = (r/255, g/255, b/255)          # floats, as insert_text wants
    baseline = s["origin"]                  # (x, y) in points, page space
    ```

    Vertical metrics if you need the full line box:

    ```python
    ascent  = s["origin"][1] - s["ascender"]  * s["size"]
    descent = s["origin"][1] - s["descender"] * s["size"]
    ```

    ## Minimal "find span under/near a rect" helper

    ```python
    def span_at(page, needle):
        for blk in page.get_text("dict")["blocks"]:
            if blk["type"] != 0:
                continue
            for ln in blk["lines"]:
                for sp in ln["spans"]:
                    if needle in sp["text"]:
                        return sp, ln
        return None, None
    ```

    For a position rather than a string, intersect the click point with `line["bbox"]` / `span["bbox"]` — there is no direct hit-test API, so a small overlap test against the rects is the normal approach.

    ## Matching the actual font file

    `span["font"]` is only a name. To get a byte-identical face, walk the page font resources and pull the program out:

    ```python
    for xref, ext, ftype, basefont, name, enc in page.get_fonts(full=True):
        base, ext, ftype, buf = doc.extract_font(xref)   # buf = font program bytes
        # base strips the "ABCDEF+" subset prefix
    ```

    Then insert with `fontbuffer=buf` (or write `buf` to disk and use `fontfile=`). That reproduces the exact face, including a subsetted one — though a subset may lack glyphs for your new text, in which case you must map to a full font.

    ## Replacing while keeping the style

    ```python
    sp, ln = span_at(page, "Old label")
    origin  = sp["origin"]
    size    = sp["size"]
    col     = ((sp["color"]>>16&255)/255, (sp["color"]>>8&255)/255, (sp["color"]&255)/255)

    page.add_redact_annot(sp["bbox"], fill=False)      # or the line bbox
    page.apply_redactions(images=pymupdf.PDF_REDACT_IMAGE_NONE)
    page.insert_text(origin, "New label",
                     fontname="F0", fontbuffer=buf,
                     fontsize=size, color=col,
                     render_mode=0)                     # 3 = invisible/text-only
    ```

    `insert_text` places text **by baseline origin**, which is exactly why `span["origin"]` is the right anchor — you get identical vertical alignment without arithmetic.

    ## Extra: `get_texttrace()`

    When `dict` is not enough (per-character colour, Type3 fonts, text drawn with fill+stroke, shading fills, exact render modes), use:

    ```python
    for span in page.get_texttrace():
        span["font"], span["size"], span["color"], span["opacity"], span["type"]
        for ch in span["chars"]:      # (unicode, glyph_id, origin, bbox)
            ...
    ```

    This is the low-level, closest-to-content-stream view and is the most reliable source for colour when `dict` reports something odd.

    ## Caveats worth knowing

    - **Colour space**: `dict`/`trace` give sRGB. If the original used an ICC/DeviceN/Separation space, the value is a conversion; round-tripping may shift. `xref_get_key(page.xref, "Resources/ColorSpace")` tells you the truth.
    - **Coordinates are in unrotated page space.** If `page.rotation != 0`, use `page.derotation_matrix` for display mapping — but both reading and `insert_text` are in that same unrotated space, so it stays consistent.
    - **`flags` matters for semantics**: superscript/subscript and bold/italic are often flags rather than distinct fonts, and some producers encode faux-bold by drawing text twice with a tiny offset.
    - **`alpha` / `opacity`** is dropped unless you pass it through; `insert_text` has no alpha argument, so use `render_mode` or a `Shape`/`TextWriter` if transparency must be preserved.
    - **`TextWriter`** (`pymupdf.TextWriter`) is better than `insert_text` when you want to reuse an already-loaded `pymupdf.Font`/`Font`+fill and avoid re-embedding duplicates.
    - **Do not trust `span["bbox"]` as the redaction rect** for descenders/ascenders — it is the tight glyph box; the line box is safer so you don't leave fragments of the old glyphs.
    - **`get_text("words")` gives no style at all** — only bbox, word, block/line/word numbers. Don't reach for it when style matters.

    If you can't recall a field name, the one-line probe is `page.get_text("dict", clip=rect)` and print the span dict — every key above will be there.
- id: pdf-locate-04
  answer: |
    You get it from the PDF's own geometry, extracted with a text-extraction library that reports coordinates — not from the rendered pixels or by eyeballing the page.

    Concretely:

    - **Column boundaries** come from two possible sources:
      - The page's vector graphics: `page.lines` / `page.rects` / `page.curves` (pdfplumber) or `page.get_drawings()` (PyMuPDF). Vertical rule x-positions define the columns directly. Also `page.find_tables()` / `extract_tables()` returns `table.bbox` and per-cell bboxes built from those rules.
      - When there are no rules, derive them by clustering the words' x-coordinates across rows (group word `x0`/`x1` into histogram peaks or gaps).

    - **The x where a right-aligned number ends** is the right edge of its text span/word: `x1` in pdfplumber (`word["x1"]`), or `bbox[2]` (the third element, max-x) in PyMuPDF spans from `page.get_text("dict")` or `page.get_text("words")`. For a right-aligned column, that max-x is essentially constant across rows — that shared value is your alignment anchor. Confirm it by taking the max/median `x1` over the column's existing cells.

    So: `pdfplumber` → `page.lines`/`page.rects` (or `find_tables()`) for boundaries, and `word["x1"]` for the right-aligned end x. `PyMuPDF` → `page.get_drawings()` for rules, and `bbox[2]` from `get_text("dict")`/`get_text("words")` spans for the end x. Then place new text so its right edge lands on that anchor (e.g. compute width via the font metrics and set the insertion x = anchor − text_width).
- id: pdf-rowdel-01
  answer: |
    ## Assumptions

    Unencrypted, unsigned PDF; the table is real text + vector rules/shading (not a scan); page `/Rotate` is 0 (or normalised first); one table on one page; the Total is a single numeric cell on that page. If a signature exists, any content change invalidates it — regenerate from source instead.

    ## The core reality check

    PyMuPDF has no "delete row" and no "delete text" primitive. A PDF row is nothing but glyphs at coordinates plus `re`/`l` paint operators, so the operation is really: **recompose the page from horizontal bands, skipping the row's band, then rewrite the Total.** Do not attempt to cover the row with a white rectangle — the old glyphs stay in the text layer, so search, copy-paste, extraction and audit all still see the deleted data.

    ## 1. Measure, don't guess

    ```python
    import pymupdf
    from decimal import Decimal

    doc  = pymupdf.open("in.pdf")
    page = doc[0]
    assert page.rotation == 0
    R = page.rect                      # top-left origin, y grows downward

    tabs = page.find_tables(strategy="lines")      # ruled table -> use the ruling
    tl   = getattr(tabs, "tables", tabs)
    tab  = max(tl, key=lambda t: t.bbox.get_area())
    rows, data = tab.rows, tab.extract()

    k      = 3                                     # row to delete
    top    = rows[k].bbox.y0
    bottom = rows[k].bbox.y1
    d      = bottom - top          # MUST be rule-to-rule, not text-bbox height
    ```

    `d` must come from the horizontal rules (cell bboxes from `strategy="lines"` sit on them; cross-check against the y-values of horizontal items in `page.get_drawings()`). Using the row's *text* height instead leaves a growing gap or overlap on every edit. For several deletions, merge adjacent rows into one gap and compute each shift in **original** coordinates (`sum` of heights above), never by repeated incremental shifts.

    Also grab: the remaining column values (parse with `Decimal`, handling currency symbols, thousands separators, parentheses/locale commas), the old total string, its cell rect, and the span style from `page.get_text("dict")` (`font`, `size`, `color`, `origin`).

    ## 2. Rebuild the page as bands

    `Page.show_pdf_page` copies a clipped region of a page into a target rect as a Form XObject — appearance-preserving, and the text stays text. That is the tool for "move everything below up".

    ```python
    src = pymupdf.open(stream=doc.tobytes())        # never read a page into itself
    new = doc.new_page(pno=1, width=R.width, height=R.height)

    # band above the row: untouched
    new.show_pdf_page(pymupdf.Rect(R.x0, R.y0, R.x1, top),
                      src, 0, clip=pymupdf.Rect(R.x0, R.y0, R.x1, top),
                      keep_proportion=False)

    # band below the row, shifted up by exactly d (same width/height -> pure translation)
    new.show_pdf_page(pymupdf.Rect(R.x0, bottom - d, R.x1, R.y1 - d),
                      src, 0, clip=pymupdf.Rect(R.x0, bottom, R.x1, R.y1),
                      keep_proportion=False)

    doc.delete_page(0)          # new page slides into index 0
    page = doc[0]
    ```

    Notes that matter:

    - Use **full page width** for the bands, not the table bbox — otherwise headers, margins and full-width shading get clipped. Use the table bbox only to compute `top`/`bottom`.
    - `keep_proportion=False` is mandatory; equal source/target dimensions make it an exact translation with no scaling.
    - The row's rules are simply never copied. The rule at `bottom` lands exactly on the rule at `top`, which is an exact stroke-on-stroke overlap (invisible); if a viewer ever darkens it, clip the lower band from `bottom + linewidth/2` instead.
    - Keep bands disjoint; overlapping clips duplicate glyphs and you get doubled extracted text.
    - `show_pdf_page` works in unrotated coordinates: normalise rotation on a working copy and reapply afterwards; also account for a CropBox whose origin isn't (0,0).

    ## 3. Update the Total

    The copied Total band still carries the old glyphs, so remove them *without* destroying the ruling/shading:

    ```python
    r = page.search_for(old_total, clip=total_cell_rect)[0]
    page.add_redact_annot(pymupdf.Rect(r.x0-1, r.y0-1, r.x1+1, r.y1+1), fill=False)
    page.apply_redactions(images=pymupdf.PDF_REDACT_IMAGE_NONE,
                          graphics=pymupdf.PDF_REDACT_LINE_ART_NONE)
    ```

    Default arguments would delete every drawing touching the rect — i.e. the shading and the borders. `graphics=PDF_REDACT_LINE_ART_NONE` and `images=PDF_REDACT_IMAGE_NONE` restrict it to glyph removal, and `fill=False` (or `fill=None`) avoids painting an opaque patch over the cell.

    Then re-typeset in the original font at the original baseline (the source span's `origin.y` shifted by `-d`):

    ```python
    xref = next(f[0] for f in doc[0].get_fonts(full=True) if f[3] == basefont)
    name, ext, typ, buf = doc.extract_font(xref)
    font = pymupdf.Font(fontbuffer=buf)          # fall back to base-14 helv/tiro/cour if ext == "n/a"

    tw = pymupdf.TextWriter(page.rect)
    x  = cell.x1 - pad - font.text_length(new_total, fontsize=size)
    tw.append(pymupdf.Point(x, baseline_y - d), new_total, font=font, fontsize=size)
    tw.write_text(page, color=pymupdf.sRGB_to_pdf(span_color))
    ```

    `insert_textbox(..., align=TEXT_ALIGN_RIGHT)` also works and returns a value you can assert on (negative = did not fit), but `TextWriter` reproduces the baseline exactly. Its origin is the *baseline*, not the top of the cell — a classic off-by-a-descender bug. If redaction fails to reach inside the copied XObject on your version, fall back to filling the cell with the exact `fill` colour sampled from the matching `get_drawings()` entry and redrawing the cell's own border segments.

    ## 4. Page furniture

    Links, annotations and widgets below the row keep their old coordinates and will be misaligned: re-add them shifted by `-d` (`insert_link` with `from` translated; recreate annots via `add_*_annot` then `set_rect`; shift widget `/Rect` through `doc.xref_set_key`). Page labels survive because the page index is unchanged. The logical structure tree / `/StructParents` and reading order are now stale — flag accessibility as degraded. Optional: crop the page to `H - d` (or leave the trailing whitespace, which is the usual look for a shrunken table).

    ## 5. Verify

    ```python
    doc.save("out.pdf", garbage=4, deflate=True)
    chk = pymupdf.open("out.pdf"); p = chk[0]
    assert old_total not in p.get_text()
    assert p.get_text().find(deleted_row_strings) == -1
    t2 = getattr(p.find_tables(strategy="lines"), "tables")
    assert t2[0].row_count == tab.row_count - 1
    assert sum(Decimal(c) for c in remaining_col) == Decimal(new_total)
    p.get_pixmap(dpi=150).save("check.png")     # visual pass; open in two viewers
    ```

    Also check that `get_text()` still reads top-to-bottom and that the horizontal-rule count from `get_drawings()` dropped by exactly the deleted row's rules.

    ## Why the tempting shortcuts are wrong

    - **White box over the row** — visually fixed, semantically not; the text layer still contains the row.
    - **Redaction alone** — cannot move anything, and defaults also delete the ruling/shading it touches.
    - **Hand-editing the content stream** (`doc.xref_stream` / `clean_contents`) — requires parsing `BT/ET`, `Tm`, `Tj/TJ`, clip and paint ops; breaks on compressed object streams and unusual producers. Only defensible when you must preserve in-place page identity, and it still kills signatures.
    - **Rasterise and repaint** — destroys the text layer; unusable for searchable/auditable documents.
    - **Redraw the whole table** with `draw_rect` + `insert_text` — the most predictable option *if* you can reproduce fills (watch non-RGB colourspaces and `fill_opacity`), line joins, cell padding and kerning; good for in-house generated tables, risky for unknown producers. Band copying preserves all of that for free.

    **Bottom line:** measure `d` from the ruling, rebuild the page from two disjoint full-width bands with `show_pdf_page` so the row is never copied, then strip only the Total's glyphs with a text-only redaction and retypeset the new value in the embedded font at the shifted baseline.
- id: pdf-rowdel-02
  answer: |
    ## The default signature

    In current PyMuPDF releases the method is:

    ```python
    page.apply_redactions(
        images=PDF_REDACT_IMAGE_PIXELS,
        graphics=PDF_REDACT_LINE_ART_NONE,
        text=PDF_REDACT_TEXT_REMOVE,
    )
    ```

    So the defaults are asymmetric: **images are processed, vector line art is ignored, text is removed.**

    ### What each default actually does

    - **`images=PDF_REDACT_IMAGE_PIXELS`** — images are *not* deleted as whole objects. Only the pixels that fall inside a redaction rectangle are erased (blanked/neutralised); the rest of the image stays intact. The alternatives are `PDF_REDACT_IMAGE_NONE` (leave images alone) and `PDF_REDACT_IMAGE_REMOVE` (delete any image touched by a redaction rectangle, even if only a corner overlaps).
    - **`graphics=PDF_REDACT_LINE_ART_NONE`** — vector graphics (paths, lines, rectangles, fills) are **completely untouched**. Every line, border, and coloured rectangle survives the redaction. The other values are `PDF_REDACT_LINE_ART_REMOVE_IF_COVERED` and `PDF_REDACT_LINE_ART_REMOVE_IF_TOUCHED`, which delete a path when the rect covers it or merely intersects it.
    - **`text=PDF_REDACT_TEXT_REMOVE`** — glyphs hit by the rectangles are removed from the content stream (`PDF_REDACT_TEXT_NONE` would leave them).

    Remember that `apply_redactions()` only acts on redaction annotations previously placed with `add_redact_annot()`; it is destructive and irreversible, and it does not reach everything (annotations, widgets, some optional-content or hidden layers need separate handling).

    ## Why this bites when redacting a table row or cell

    1. **Table grids are almost always vector art.** Cell borders, row rules, column separators, zebra striping, and header fills are drawn paths, not images or text. With `graphics=NONE` the default leaves all of them in place. You blank the cell's text and still ship a PDF showing the full grid, the row's geometry, and any colour coding — often enough to reveal that a row existed, how wide it was, and sometimes what category the cell belonged to.
    2. **"Removed" text can leave a visible ghost frame.** Text-fill colour, highlights, and background shading behind a cell are vector fills; they stay. Conversely, content that *is* vector art (outlined/curves-converted text, stamps, barcodes, drawn tick marks) is not removed at all, so a cell can still be fully readable after `apply_redactions()` returns success.
    3. **Cell content drawn as an image is only partly neutralised.** If the table is a scan, a screenshot pasted from Excel, or a raster logo inside a cell, `PDF_REDACT_IMAGE_PIXELS` erases only the pixels under your rectangle. Slightly undersized rects — missing padding, a half-point row height error, edge coordinates — leave slivers of glyphs, and JPEG compression artefacts around the erased region can hint at what was there. Conversely, a rect that spills into the next column silently damages content you meant to keep.
    4. **The two failure modes are opposite and both silent.** A redaction rect that covers the *text* but not the *border/fill* leaves the structure; one that covers the *cell rectangle* but not the full row leaves neighbours' pixels clipped. And if you "fix" it by switching to `PDF_REDACT_IMAGE_REMOVE`, a scanned table loses the entire page image because the row touches it — you cannot remove one cell of a raster table that way.

    ## Practical guidance

    For a table row/cell, pass explicit arguments rather than relying on defaults, e.g.:

    ```python
    page.apply_redactions(
        images=PDF_REDACT_IMAGE_PIXELS,
        graphics=PDF_REDACT_LINE_ART_REMOVE_IF_COVERED,
        text=PDF_REDACT_TEXT_REMOVE,
    )
    ```

    and build the annotation rectangles from the union of the row's span (text blocks **plus** `page.get_drawings()` bboxes), with a small outward margin so borders and glyph edges are fully enclosed. Use `REMOVE_IF_TOUCHED` if you want any intersecting rule gone; be aware it can delete whole table frames. For raster/scanned tables, treat per-cell redaction as unreliable — crop-and-replace or redact the whole image region instead.

    One honest caveat: the exact enum names and defaults are stable in recent versions, but older releases had a different (and evolving) set of options, so verify the signature in the version you deploy.
- id: pdf-rowdel-03
  answer: |
    **Short answer: yes — not with a "move" primitive (PyMuPDF has none), but with a vector re-placement.** You can lift the region as a *Form XObject* via `Page.show_pdf_page()` using its `clip` argument, draw it at a shifted rectangle, then delete the original pixels/text with a redaction. Nothing is rasterized: fonts, vector rules, images, and real text all survive.

    ```python
    import pymupdf  # a.k.a. fitz

    def move_band_up(doc, page, band: pymupdf.Rect, dy: float):
        """Move everything inside `band` up by `dy` points, exact appearance."""
        # 1) Snapshot the page. show_pdf_page wants a *separate* Document as source.
        src = pymupdf.open("pdf", doc.tobytes())

        # 2) Target rect = same box, translated up (PyMuPDF: origin top-left, y grows down)
        dest = pymupdf.Rect(band.x0, band.y0 - dy, band.x1, band.y1 - dy)

        # 3) Erase the original copy (text + table rules)
        page.add_redact_annot(band)
        page.apply_redactions(
            images=pymupdf.PDF_REDACT_IMAGE_NONE,                      # keep bitmaps intact
            graphics=pymupdf.PDF_REDACT_LINE_ART_REMOVE_IF_TOUCHED,    # v1.22+; drop old rules
        )

        # 4) Paste the pristine clipped region 1:1 (keep_proportion=False = pure translation)
        page.show_pdf_page(dest, src, page.number, clip=band, keep_proportion=False)
    ```

    Usage for "delete one table row and pull the rest up":

    ```python
    doc = pymupdf.open("in.pdf")
    page = doc[0]

    row_top, row_bottom = 300.0, 320.0          # the row you are deleting
    row_h = row_bottom - row_top
    band = pymupdf.Rect(0, row_bottom, page.rect.width, page.rect.height)  # everything below
    move_band_up(doc, page, band, row_h)

    doc.save("out.pdf", garbage=4, deflate=True)
    ```

    That leaves a blank strip of height `row_h` at the bottom of the page — usually fine; if you want to close it, shrink the page with `page.set_mediabox()`/`set_cropbox()` or move a header up as well.

    **Why this preserves appearance**
    - `show_pdf_page` embeds the clipped source as a Form XObject — vector art stays vector, embedded fonts stay embedded, images stay images, and text remains selectable/searchable (not a screenshot).
    - `clip=` is a `Rect` in the source page's own coordinate space, i.e. the same numbers you get from `page.get_text("words")`, `find_tables()`, or `page.rect`.
    - `keep_proportion=False` is what makes it an exact translation instead of a fit/scale.

    **Caveats worth knowing**
    - Source must be a different `Document` object than the target in supported versions — hence the `open("pdf", doc.tobytes())` snapshot. Do the snapshot *before* redacting, so you copy from the untouched page.
    - Annotations, links, widgets, and OC (optional-content) membership are **not** carried by `show_pdf_page`; it copies page content only.
    - Redaction removes text by bounding-box intersection, so keep `band` boundaries tight to the row edges (use the line positions from `page.get_drawings()` or `find_tables()`), otherwise it can nick descenders/ascenders in adjacent rows.
    - `apply_redactions(graphics=...)` only exists in newer PyMuPDF; on older builds vector rules are not removed and you'd have to white them out or draw over them.
    - If you don't want redaction at all, `page.draw_rect(band, color=None, fill=(1,1,1))` hides the old copy — but it only *covers* it: the text is still extractable and anything underneath (background tints, watermarks) is also obscured.

    **Other options, ranked**
    1. `show_pdf_page(clip=...)` + redaction — best balance of fidelity and effort.
    2. Raw content-stream edit: `page.clean_contents()`, then prepend `1 0 0 1 0 -dy cm` inside a `q ... Q` with an even-odd clip (`w h re  x0 y0 ww hh re  W* n`) to translate only the band. Preserves everything and needs no redaction, but the content gets duplicated in the stream, so text extraction can return the band twice — only worth it if you must avoid redaction side effects.
    3. `page.get_pixmap(clip=band, dpi=300)` → `page.insert_image(dest, pixmap=...)`. Simplest, but rasterizes: lost text, possible seams, big files. Fine for scanned pages, wrong for digital ones.

    Re-typing rows with `insert_text`/`insert_htmlbox` is the option you're trying to avoid — it never matches the original font metrics and spacing exactly.
- id: pdf-rowdel-04
  answer: |
    At minimum, the money math: line-item/subtotal recalculation, tax (VAT/sales tax) recomputation, discounts or shipping if they interact with the subtotal, and the grand total / amount due / balance due.

    Also likely to need updating:

    - Line-item numbering or references to “item 3,” footnotes, and cross-references.
    - Any per-page subtotal or “carried forward” figure if the table spans pages.
    - Pagination and continued-table markers, plus repeated headers/footers.
    - Table structure: row spans, borders, zebra striping, and column alignment.
    - PDF accessibility tags and reading order, plus any form-field calculations or bookmarks/links anchored to the deleted row.
    - Category summaries, quantity totals, or approval/signature blocks that depend on the removed line.

    Assumption: the invoice table has computed totals and standard line numbering; if it is a purely static scanned image, “changing” means re-typesetting or overlaying the affected text, not recalculating fields.
- id: pdf-cell-01
  answer: |
    ## Assumptions

    - The number is real text (not a scanned image, not an AcroForm field).
    - It sits in one span, page rotation is 0, PyMuPDF ≥ 1.23.
    - "Looks native" = same font file, size, color, baseline, and right edge as the original.

    If any assumption breaks, see *Gotchas* at the end.

    ---

    ## Steps

    ### 1. Find the span, not just the string

    `page.search_for()` gives you rectangles but no font info. Use the dict extractor to get the span that owns the number:

    ```python
    import fitz

    doc  = fitz.open("in.pdf")
    page = doc[0]
    OLD, NEW = "22.50", "54.00"

    span = None
    for blk in page.get_text("dict")["blocks"]:
        for line in blk.get("lines", []):
            for s in line["spans"]:
                if s["text"].strip() == OLD:      # exact cell content
                    span = s
                    break
            if span: break
        if span: break
    if span is None:
        raise SystemExit("span not found")

    x0, y0, x1, y1 = span["bbox"]
    size     = span["size"]
    base_y   = span["origin"][1]                  # baseline of the glyphs
    c        = span["color"]                       # packed sRGB int
    color    = ((c >> 16 & 255)/255, (c >> 8 & 255)/255, (c & 255)/255)
    ```

    `origin` is the baseline origin of the first glyph — reusing its `y` is what keeps the new text sitting on exactly the same baseline.

    ### 2. Load the *same* font

    "Native" means the embedded font, not Helvetica. Match the span's `font` name against the page's font list and pull the font program out:

    ```python
    def span_font(doc, page, span):
        want = span["font"].split("+")[-1].lower()
        for xref, ext, ftype, basefont, name, enc in page.get_fonts(full=True):
            base = basefont.split("+")[-1].lower()
            if base == want or want in base or base in want:
                try:
                    _, _, _, buf = doc.extract_font(xref)
                    if buf:
                        return fitz.Font(fontbuffer=buf)
                except Exception:
                    pass
        # fallback: nearest Base-14 face using span flags
        f = span["flags"]
        serif, bold, ital = bool(f & 4), bool(f & 16), bool(f & 2)
        table = {(0,0):"helv", (0,1):"heit", (0,2):"hebo", (0,3):"hebi",
                 (1,0):"tiro", (1,1):"tiit", (1,2):"tibo", (1,3):"tibi"}
        return fitz.Font(fontname=table[(int(serif), int(bold) + int(ital)*0)]) \
               if False else fitz.Font(fontname=table[(int(serif), (2 if bold and ital else 1 if bold else 0) if not ital else (3 if bold else 1))])
    ```

    Simpler, less clever version:

    ```python
        key = (1 if serif else 0, 3 if (bold and ital) else 2 if bold else 1 if ital else 0)
    ```

    ### 3. Compute the right-aligned x

    The original was right-aligned to `x1`. Measure the new string with the same font and size, then left-position it so it *ends* at `x1`:

    ```python
    font = span_font(doc, page, span)
    new_x = x1 - font.text_length(NEW, fontsize=size)
    ```

    `font.text_length` uses the real glyph advances, so it stays exact even for proportional fonts.

    ### 4. Remove the old glyphs without wrecking the table

    Use a redaction annotation with a rect that hugs the number. Do **not** let it touch the cell rules:

    ```python
    pad  = 0.5                                   # keep tiny, or borders get clipped
    rect = fitz.Rect(x0 - pad, y0 - pad, x1 + pad, y1 + pad)

    page.add_redact_annot(rect, fill=None)       # fill=None: no white patch painted
    page.apply_redactions(
        images=fitz.PDF_REDACT_IMAGE_NONE,       # don't nuke overlapping images
        graphics=fitz.PDF_REDACT_LINE_ART_NONE,  # don't erase table rules
    )
    ```

    `fill=None` matters on shaded cells — the default paints a white box. Omitting `graphics=...` silently deletes any vector line crossing the rect, which is exactly your table border.

    ### 5. Draw the new number on the old baseline

    `TextWriter` handles embedded/CFF fonts and avoids `insert_text`'s encoding limits:

    ```python
    tw = fitz.TextWriter(page.rect)
    tw.append((new_x, base_y), NEW, font=font, fontsize=size)
    tw.write_text(page, color=color)
    ```

    No extra `insert_text` call, no separate `insert_font` — one text object, one render pass, same appearance stream shape as a normal PDF writer would produce.

    ### 6. Save without disturbing anything else

    ```python
    doc.save("out.pdf", garbage=3, deflate=True,
             encryption=fitz.PDF_ENCRYPT_KEEP)   # keep the password if any
    ```

    Use `incremental=True` on the same path only if you must preserve every other object byte-for-byte; a fresh `garbage=3` save is usually cleaner.

    ---

    ## Verify

    ```python
    # 1. old string gone, new one present exactly once
    assert not page.search_for(OLD)
    hits = page.search_for(NEW)
    assert len(hits) == 1

    # 2. right edge and baseline unchanged
    assert abs(hits[0].x1 - x1) < 0.5

    # 3. visual diff at high dpi
    clip = fitz.Rect(x0 - 40, y0 - 10, x1 + 10, y1 + 10)
    page.get_pixmap(dpi=300, clip=clip).save("after.png")
    ```

    Render the same clip from the source file and overlay them; a 1 px shift or a hinting difference is visible immediately, a wrong font or a leftover white patch jumps out.

    ---

    ## Gotchas

    - **Subsetted fonts.** `extract_font` often returns a subset containing only the glyphs the original document used. If the new digits aren't in it, you get blanks or tofu. Verify by rendering; fall back to a metric-compatible system font (`fitz.Font(fontfile="/System/Library/Fonts/Supplemental/Arial.ttf")`) if the subset fails.
    - **Number is part of a longer span** (`"Total 22.50"` in one span). Find the sub-rect with `page.search_for(OLD, clip=span["bbox"])[0]`, and keep the redaction rect strictly inside that.
    - **Letter-spaced digits.** Some generators emit each digit as its own span or with big `TJ` offsets. Then `x1` is much wider than `text_length` predicts; you must position per glyph or accept a mismatch.
    - **Form field.** If the cell is a widget, don't redact — `for w in page.widgets(): w.field_value = NEW; w.update()`.
    - **Scanned page.** No text layer, no span. You'd need OCR or an image-region replacement (cover with a patch sampled from the background, then draw text on top).
    - **Rotated pages.** `get_text` and `insert_text` use unrotated coordinates while `page.rect` is rotated. Temporarily `page.set_rotation(0)`, do the edit, restore, or build the `TextWriter` with the mediabox.
    - **Older PyMuPDF.** The `graphics=` kwarg on `apply_redactions` arrived in 1.23; before that, either avoid rects that touch rules or accept that line art may be dropped.
- id: pdf-cell-02
  answer: |
    **Assumption:** the goal is to remove only the text in one cell of a bordered table while preserving neighboring cell text and borders.

    Use an **inner rectangle**, not the full cell rectangle. Start with the cell bounds and inset all four sides by a small margin — typically about **0.5–1 pt**, or roughly a fraction of the narrowest gap between the text and the cell border. Better still, intersect that inner rectangle with the target text’s actual bboxes from `get_text("words")` or `search_for(...)`, and for multi-line cells use separate per-line rectangles or a union of the target character bboxes.

    The reason is that PyMuPDF’s criterion is **any overlap**, not containment. A character is removed if its bbox intersects the redaction rectangle even slightly. Character bboxes are font-metric boxes: they can include ascender/descender space and side bearings beyond the visible ink, and italic or overhanging glyphs can extend into the neighboring cell. A cell-sized rectangle can therefore clip the bbox of a character in the adjacent cell or row and delete that whole character, even though visually the text looked safely separated. It can also damage shared cell borders.

    At the same time, do not enlarge the rectangle “to be safe.” It only needs to overlap every target character’s bbox, so a tight or slightly inset rectangle is enough. If exact text bboxes are available, use those with zero or negative padding and clip them to the cell interior.
- id: pdf-cell-03
  answer: |
    **Short answer:** `insert_textbox` does **not** raise, clip, auto-shrink, or spill onto another page. It returns a `float`: the unused height of the box if everything fit, or a **negative number** (roughly the height deficit) if it did not. If you ignore the return value, you get silently missing text — that's the #1 bug with this call.

    In current PyMuPDF the call is effectively all-or-nothing: on overflow **none** of the text is drawn. (Some APIs/older paths lay down the lines that fit, so always treat the negative return as "check me," never as "partial content is fine.")

    **Your options when it returns negative:**

    1. **Shrink the font** — loop/binary-search `fontsize` (and `lineheight`) until `rc >= 0`. Simple, and fine for cell-sized text.
    2. **Grow the rectangle** — taller row, wider column, landscape orientation. For tables this means a two-pass layout: measure first, then set row height.
    3. **Measure first** — `fitz.get_text_length()` or `TextWriter.text_rect()` to compute wrapping and required height before drawing, instead of trial-and-error.
    4. **Reduce content** — truncate with an ellipsis (`…`) or write a shorter label/tooltip.
    5. **Flow/paginate instead of one box** — `Story.place()` / `Story.draw()` across multiple rects or pages is the real flow-layout tool; `Page.insert_htmlbox(rect, html, scale_low=...)` will auto-downscale the font to fit (scale_low bounds how far it may shrink).
    6. **`TextWriter.fill_textbox()`** — returns `(spare_height, line_count)`; it writes the lines that fit and tells you how many, so you can continue on the next box/page. This is the pragmatic path for multi-page cell content.
    7. **Manual line layout** — `TextWriter` + `get_text_length`/`text_rect` to break lines yourself, then `insert_text()` / `TextWriter.append()` per line.

    Assumption: modern PyMuPDF (1.2x). The stable contract across versions is the sign of the return value; don't rely on partial rendering of `insert_textbox` itself.
- id: pdf-cell-04
  answer: |
    `point` is the **baseline origin** of the first character — the left end of the text baseline, not a bounding-box corner. In PyMuPDF's page coordinate system (origin top-left, y increasing downward), `insert_text((x, y), ...)` puts the glyph baseline at `y`, so the visible body of the text sits *above* that line by roughly the font ascender (~0.75–0.85 × fontsize), and descenders hang slightly below it.

    If you pass the **top-left of the old word's bbox**, i.e. `(bbox.x0, bbox.y0)`:

    - `x` is fine — `bbox.x0` is close to the glyph origin (modulo left side bearing).
    - `y` is wrong by roughly one ascender. The new text is drawn **about 0.8 × fontsize too high**, typically overlapping the line above or overflowing the top of the page, while the old word's glyph bodies remain uncovered below it.

    Concretely, for an 11 pt font the error is ~8–9 pt — enough to land the new text in the previous line, and to make the "replacement" visibly not cover the original.

    Also, regardless of coordinates: `insert_text` **draws, it does not erase**. The original glyphs stay in the content stream underneath, so even with the correct baseline the old word shows through unless you first redact/cover it (e.g. `add_redact_annot` + `apply_redactions`, or a white filled rect).

    Correct baseline for a word with bbox `(x0, y0, x1, y1)`: place the baseline at `y1` minus the descender, e.g. `y = y1 - descender * fontsize` (or use the font's ascender: `y = y0 + ascender * fontsize`). PyMuPDF's `insert_textbox`/`TextWriter.append` with a rect avoids the manual baseline math; `TextWriter.append(point, text)` has the same baseline semantics.

    Assumption: current PyMuPDF/fitz semantics where page coordinates are top-left origin with y down and `point.y` is the baseline.
- id: pdf-relayout-01
  answer: |
    ## What has to happen when you insert a row above a Total row

    Inserting a row is not a local edit in a PDF. A PDF has no table objects — only glyphs positioned by absolute coordinates, plus vector line segments for the rules. So "adding a row" is really a **coordinate-space relayout** with four coupled effects.

    ### 1. The Total row moves down
    - It cannot stay where it is. It must be translated vertically by the inserted row's height (row height + any inter-row padding), keeping horizontal position.
    - Everything that a Total row *anchors* to moves with it: the "Total" label, the summed numeric value on the right, any currency symbol, and any trailing emphasis (bold rule above the total, shading, leader dots).
    - If the total value itself changes because the new row carries an amount, the number must be **recomputed and re-rendered**, not merely moved — and the new value may change its rendered width (right-aligned totals must be re-measured and re-positioned so they stay flush against the same right edge).

    ### 2. The table rules must be redrawn, not just shifted
    This is the part people get wrong. Rules split into three classes, each handled differently:
    - **Rules above the insertion point** — unchanged (no-op).
    - **Rules below the insertion point, including the rule under the new row and the rule above/under the Total row** — must be translated down by the same delta as the rows they belong to.
    - **Vertical rules / outer table borders** — these must be *extended* (or re-stroked) to cover the larger table body. A pure translation of a vertical segment would move its endpoints but not lengthen it, leaving a visible gap at the bottom border or a rule that no longer spans the new height.
    - **Stroking style must be preserved**: line width, colour, dash pattern, and whether it was a filled rectangle versus a stroked line. Many generated tables draw rules as thin filled rects; you must match primitive type or the new rule will look subtly bolder or fainter.
    - Any **zebra/shading fills** or cell background rectangles in the affected rows must be re-emitted with the new geometry, and must sit *under* the text in the content stream (painting order matters).

    ### 3. Content below the table must be pushed down and reflowed
    - Everything anchored below the table — captions, footnotes, paragraphs, the next heading, signature blocks, page footers excepted — must be translated down by the net table growth.
    - This is a **cascade**, not a one-hop translate: reflowing one block can change its line count, which changes its own height, which shifts everything under it. You have to re-run layout from the insertion point to the end of the flowable region rather than apply a single offset.
    - **Page overflow** is the real constraint. If the pushed content no longer fits:
      - break the page and carry the remainder to the next page,
      - **repeat the table header** on the continuation page if the table spans the break,
      - re-emit the bottom table rule at the break point so each page carries a closed table,
      - update page numbers / "continued" markers,
      - and if the document has cross-references (page X of Y, "see page 4"), those must be regenerated too — a strong argument for doing this in the *layout model* and re-rendering the page, not by patching the finished PDF.

    ### 4. Ancillary structures that must stay consistent
    - **Cell contents**: the new row's text needs font, size, baseline, and alignment consistent with sibling rows; numeric columns should be aligned on the same decimal/right edge.
    - **Accessibility/tagging**: tagged PDF tables need the new `<TR>`/`<TD>` structure elements and regenerated reading order and `/StructParents`; the marked-content IDs must be renumbered. A coordinate-only edit silently desynchronises the tag tree.
    - **Annotations / links** in the shifted region need their rects moved too, otherwise a link rectangle floats off its text.

    ### Assumptions I'm making
    - "Ruled table" means visible horizontal and vertical rules, likely generated content, single-page or spanning.
    - The inserted row's height is not identical to existing rows (variable-height content such as wrapped text); if it were uniform, the delta is a constant and the cascade is still required but trivial to compute.
    - The Total is a *sum* over the rows above it, so it is recomputed, not just relocated.

    ### Short version
    The Total row and all table rules below the insertion point shift down by the new row's height; vertical rules and the table border must be **lengthened**, not translated; the total value must be **recomputed and re-rendered**; and all following content must be **reflowed downward**, with page breaks, repeated headers, footers, tags, and link rects regenerated as needed. The honest engineering answer is that this must be done by rebuilding the layout model and re-rendering the affected pages — patching glyph and line coordinates in place will be wrong at the first page break.

    ---

    ### Recap

    **ASKED** — Explain what happens to the Total row, table rules, and content below when a row is inserted above a Total row in a PDF.

    **BUILT** — Not applicable; conceptual answer.

    **FOUND** — PDFs store no table structure, so insertion is a coordinate-space relayout. The non-obvious failure is treating the edit as a constant-offset translation: vertical rules need lengthening, not moving, and the total needs recomputation, and reflow can cascade into page breaks.

    **DECIDED** — Recommended rebuilding the layout model and re-rendering pages rather than patching finished-PDF coordinates, because the first page overflow makes patching unworkable. BOTTOM LINE: it's a full reflow-plus-redraw problem, not a local edit.

    **NEXT** — None — this was a knowledge question with no follow-on work pending.
- id: pdf-relayout-02
  answer: |
    Assuming the PDF is being regenerated from a layout model rather than edited as a scanned image:

    **Correct approach**

    - Treat the table as a fixed-width layout grid. Compute the usable width as `page width − left margin − right margin`.
    - Reallocate that width across **all** columns rather than adding width for the new column. Usually reduce the widest or most flexible text columns, or scale existing widths proportionally, while keeping narrow numeric/ID columns at their minimum readable width.
    - Recompute column x-positions, cell widths, padding, and border positions from the new grid.
    - Reflow cell content: enable wrapping, recalculate row heights, and re-check page breaks and repeated headers.
    - Verify that the table’s right edge, borders, and any overflow indicators stay inside the right margin.
    - If the result is too cramped, change the layout instead of forcing it: smaller font, landscape page, fewer columns, abbreviated headers, or splitting the table.

    **Common mistake**

    Adding the column while keeping the existing column widths unchanged — or simply widening the table — so the last column extends past the right margin and gets clipped, overlaps other content, or forces the whole table to shrink/scale illegibly. In coordinate-based PDF generation, the equivalent mistake is inserting a new column at the right without shifting/recomputing every existing x-coordinate and redrawing the full grid.

    The key distinction: **adding a column must consume existing width, not create new width.**
- id: pdf-relayout-03
  answer: |
    Short answer: it's acceptable when the PDF is a digital-born document you effectively own the content of, the table is simple, and layout fidelity is not the deliverable. It's a bad idea when the PDF *is* the artifact — signed, archived, pixel-exact, or machine-generated — because the round trip is lossy, non-deterministic, and quietly destructive.

    Assumption: the PDF has a real text layer (not a scan) and the goal is to change table content, not to preserve exact pagination.

    ## When it's acceptable

    - **The PDF is a printout of something else.** If you have (or can rebuild) the source, regenerating is correct; the "convert back" step is just a poor substitute for the real authoring tool.
    - **Simple, regular tables.** Uniform grid, one header row, no merged/spanned cells, no nested tables, no rotated or vertical text, no math, minimal styling.
    - **Content matters, layout doesn't.** Internal reports, drafts, data updates, documents that will be re-reviewed anyway.
    - **You will visually verify.** Round-tripping is only safe if a human compares the output against the original page by page.
    - **Low volume, one-off.** Manual touch-up after conversion is cheaper than building a pipeline.
    - **Non-authoritative copies.** Working copies, extracts, redistribution where the original is retained unchanged.

    ## When it's not acceptable

    - **Scanned/image PDFs.** Requires OCR; table cells get misread, columns merge, and errors compound silently. Rebuild from the image, don't round-trip.
    - **Signed, certified, or encrypted PDFs.** Any edit invalidates the signature; permissions may block export entirely. Redaction also fails — hidden text and metadata can survive conversion.
    - **Archival/regulatory conformance.** PDF/A, PDF/UA tagging, bookmarks, alt text, form fields — all typically lost. Round-tripped output usually no longer conforms.
    - **Layout-exact deliverables.** Contracts, typeset publications, forms, anything where pagination is referenced ("see page 4", TOC, figure numbers).

    ## The risks, concretely

    **Structure loss in the table itself**
    - Merged cells get split or duplicated; the header row gets repeated as data.
    - Multi-line cells collapse into one line or split into new rows.
    - Column detection fails on multi-column pages: text interleaves across columns.
    - Cell widths become approximations; font metric differences shift alignment. Right-aligned numbers drift.
    - Footnotes and captions detach from the table and land in random reading order.

    **Global text damage**
    - Font substitution (missing/embedded/licensed fonts), lost ligatures and kerning.
    - Soft hyphens and hard line breaks become paragraph breaks; or all breaks are removed.
    - Reading order is reconstructed by heuristic and often wrong.
    - Non-Latin scripts, RTL, CJK, equations, chemical structures, and vector art degrade badly.

    **Document-level damage**
    - Pagination changes; the table now splits across pages differently.
    - Headers, footers, page numbers, section starts, and cross-references drift.
    - Bookmarks and metadata lost; DOCX author/revision history can leak into the output.
    - No idempotence: each round trip is generationally lossy. Doing it twice is worse than once.

    **Process risk**
    - Silent corruption is worse than visible breakage. A table that converts "successfully" but has swapped two columns will pass a casual glance.
    - Non-reproducible: different tool versions (Acrobat export, LibreOffice, pandoc, pdf2htmlEX) give different results, so it can't be a reliable pipeline step.

    ## How to do it if you must

    1. Never overwrite the original; work on a copy and keep a hash.
    2. Prefer the true source. Failing that, choose the direction deliberately: HTML for data-heavy tables (you control markup and print CSS), DOCX for prose-with-a-table where a human will edit in Word.
    3. Rebuild the table rather than "converting" it: extract to CSV/structured data, regenerate, then diff.
    4. Validate: extracted-text diff against the original, cell count and per-cell content checksum, page count, and a visual overlay or side-by-side screenshot compare.
    5. Check for signatures, encryption, and PDF/A conformance before and after.
    6. If only a cell's values change, prefer a targeted content-stream edit or regenerating from data over a full document round trip.

    Rule of thumb: **if the PDF is the product, don't round-trip it — regenerate it. If the PDF is just a container for data you own, round-tripping is fine as long as a human checks the table and you keep the original.**
- id: pdf-relayout-04
  answer: |
    **Short answer:** a PDF has no notion of "empty" — you have to reconstruct occupancy yourself from every painted object's bounding box, then do rectangle-intersection tests between those boxes and the rectangle you want to occupy (grown by a safety margin), plus a check that the candidate stays inside the page's usable area.

    ## The procedure

    **1. Build an occupancy set.** Walk the page and collect a bbox for *everything* that paints:
    - text — use span/line bboxes, but verify with char boxes because font bboxes routinely under-report ascenders/descenders and italics overhang
    - images and inline images
    - vector graphics — `get_drawings()` / `page.lines`, `page.rects`, `page.curves`, including table rules and cell borders
    - annotations, form widgets, links
    - the table's own geometry (cell grid + rules)

    Group them by the structural element they belong to, so you can exclude the object you're editing.

    **2. Define the candidate rect.** For growth: the table's current bbox expanded by the delta in the growth direction(s). For insertion: the box the new content would occupy. Also reserve header/footer bands and page margins; clamp against `CropBox`/`MediaBox`.

    **3. Normalize coordinates.** PDF native space is bottom-left origin and may be offset by `CropBox`, rotated by `/Rotate`, scaled by `/UserUnit`. Convert everything into one space (I usually normalize to top-left, y-down, points) before testing.

    **4. Inflate by a margin, then test.** Grow every existing bbox by `t` (1–3 pt, or ~half a line height) to absorb stroke width, anti-aliasing, and sloppy font metrics. Then:

    ```
    def overlaps(a, b):
        return a.x0 < b.x1 and a.x1 > b.x0 and a.y0 < b.y1 and a.y1 > b.y0
    ```

    Enough space ⇔ the candidate intersects **none** of the remaining boxes, and is fully contained in the usable page area. Exclude the table's own boxes and any white/no-op fills you're willing to paint over.

    **5. For "can the table grow?" specifically, measure the free run, not just a boolean.** Take the table's x-range, collect every object whose x-interval intersects that band, sort by y, and find the gap between the table's bottom edge and the next obstacle below (or top edge and the next above). Capacity = gap − margin. That number, not a yes/no, is what you actually want.

    **6. Fallback: raster free-space map.** When vector extraction is unreliable (clipping paths, weird art, unknown object types), render at ~144 dpi, threshold ink, morphologically dilate by the margin, and test whether the candidate region's mask contains any ink. It's the ground truth for "does it *look* empty," at the cost of resolution and memory.

    **7. Also compute the maximal empty rectangle** if placement is flexible — interval sweep over y with an x-interval set, or a largest-rectangle-in-binary-matrix pass on the raster. That tells you *where* it fits, not just whether one spot works.

    ## Things that bite

    - **Invisible text still counts** if it's selectable — render mode 3 text and OCR layers occupy space and will overlap/garble. White-on-white text is the same problem.
    - **White filled rectangles** are often used as masks. Visually empty, but they *are* painted; decide deliberately whether to treat them as free.
    - **Clipped content** outside the `CropBox`, or hidden by a clip path, doesn't show but exists in the stream — judge by visual truth (raster check) or you'll refuse valid space.
    - **Hidden OCG layers** and annotations toggled off.
    - **Zero-height/zero-width boxes** from hairline rules and degenerate paths — an intersection test with strict `<` can miss them; use `<=` with the inflated margin.
    - **Downstream flow**: growing a table can push subsequent content off the page or into the footer. Checking the immediate rectangle is necessary but not sufficient — you also need to verify the reflow chain.

    Pseudocode:

    ```python
    obstacles = [inflate(b, t) for b in page_objects(page)
                 if b.element_id != table.id]

    cand = grow(table.bbox, dy=needed, dx=0)
    blockers = [b for b in obstacles if overlaps(cand, b)]

    ok = not blockers and contains(usable_area(page), cand)
    ```

    **Assumption I'm making:** you're working with a library that exposes object geometry (PyMuPDF's `get_text("dict")` / `get_drawings()` / `get_images()`, or pdfplumber's `chars` / `rects` / `curves` / `images` / `annots`). If all you have is raw page content streams, you must interpret `Td/Tm/Tj`, `re`, `m/l/c`, and `Do` yourself to derive the same boxes.
- id: pdf-tblins-01
  answer: |
    Assumptions: the PDF is unencrypted, the blank region is known in PDF points (`1 pt = 1/72 in`), Helvetica is acceptable, and "ruled" means cell borders (a grid), not just a header rule. Nothing here creates an *interactive* table — both approaches flatten drawn lines + text onto the page.

    The key coordinate gotcha: PDF user space and `pypdf`/`pikepdf`/ReportLab use a **bottom-left origin with y increasing upward**, while **PyMuPDF uses a top-left origin with y increasing downward**. Pick one and convert deliberately.

    ---

    ## Way 1 — Draw directly on the page with PyMuPDF (`fitz`)

    Best when you just want ink on the page: no intermediate file, one save, precise placement.

    ```python
    import fitz  # PyMuPDF

    doc  = fitz.open("in.pdf")
    page = doc[0]

    # --- free rectangle, PyMuPDF coords (origin TOP-LEFT, y grows down) ---
    x0, y0 = 72, 380
    col_w  = [90, 150]
    row_h  = 18
    rows   = [["Item", "Value"], ["Alpha", "1"], ["Beta", "2"]]

    n_rows = len(rows)
    tw, th = sum(col_w), row_h * n_rows
    xs = [x0]
    for w in col_w:
        xs.append(xs[-1] + w)

    # --- ruled grid ---
    shape = page.new_shape()
    for i in range(n_rows + 1):                       # horizontals
        y = y0 + i * row_h
        shape.draw_line(fitz.Point(x0, y), fitz.Point(x0 + tw, y))
    for x in xs:                                      # verticals
        shape.draw_line(fitz.Point(x, y0), fitz.Point(x, y0 + th))
    shape.finish(color=(0, 0, 0), width=0.6)          # 0.6 pt rule
    shape.commit()

    # --- cell text (point = baseline, args in points) ---
    for r, row in enumerate(rows):
        for c, cell in enumerate(row):
            page.insert_text(fitz.Point(xs[c] + 3, y0 + r * row_h + 13),
                             cell, fontsize=9, fontname="helv")

    doc.save("out.pdf", garbage=3, deflate=True)
    ```

    Notes:
    - `fontname="helv"` is a built-in Standard-14 font, so nothing is embedded. For non-ASCII, load a real TTF (`fitz.Font(fontfile=...)` + `page.insert_font`), otherwise glyphs go missing.
    - `page.rect` already accounts for `/Rotate`, but content-stream coordinates do not. If the page is rotated 90/180/270, map your box through `page.derotation_matrix` (or un-rotate, draw, re-rotate) or you will land in the wrong corner.
    - Find the free area programmatically by subtracting occupied boxes: `page.get_text("blocks")`, `page.get_drawings()`, `page.get_image_info()`. On a scanned page everything is "free" to the API, so also eyeball a render (`page.get_pixmap(dpi=150).save("check.png")`).

    ## Way 2 — Build a vector overlay and stamp it (`ReportLab` + `pypdf`/`pikepdf`)

    Best when you want the original page content stream left untouched, or you want the table drawn by a layout engine instead of by hand.

    ```python
    import io
    from reportlab.pdfgen import canvas
    from reportlab.lib import colors
    from pypdf import PdfReader, PdfWriter

    reader = PdfReader("in.pdf")
    page   = reader.pages[0]
    W, H   = float(page.mediabox.width), float(page.mediabox.height)

    # --- overlay page, EXACTLY the same page size, PDF coords (y up) ---
    buf = io.BytesIO()
    c = canvas.Canvas(buf, pagesize=(W, H))

    x0, ytop = 72, H - 380          # ytop = TOP edge of the table
    col_w, row_h = [90, 150], 18
    rows = [["Item", "Value"], ["Alpha", "1"], ["Beta", "2"]]
    tw, th = sum(col_w), row_h * len(rows)

    c.setStrokeColor(colors.black); c.setLineWidth(0.6)
    for i in range(len(rows) + 1):
        y = ytop - i * row_h
        c.line(x0, y, x0 + tw, y)
    xs = [x0]
    for w in col_w:
        xs.append(xs[-1] + w)
    for x in xs:
        c.line(x, ytop, x, ytop - th)

    c.setFillColor(colors.black); c.setFont("Helvetica", 9)
    for r, row in enumerate(rows):
        for ci, cell in enumerate(row):
            c.drawString(xs[ci] + 3, ytop - (r + 1) * row_h + 5, cell)

    c.showPage(); c.save()
    buf.seek(0)

    overlay = PdfReader(buf).pages[0]
    page.merge_page(overlay, over=True)      # over=True => table drawn on top

    writer = PdfWriter()
    writer.add_page(page)
    with open("out.pdf", "wb") as f:
        writer.write(f)
    ```

    If you'd rather author a *small* table PDF and position it, use `page.merge_transformed_page(overlay, Transformation().translate(tx, ty))` instead of making the overlay page-size. With `pikepdf` the equivalent is `page.add_overlay(overlay_page)` (optionally with a `rect`), which wraps the overlay in a Form XObject and leaves the original `/Contents` bytes completely intact — the safer choice for structurally odd PDFs.

    Optional nicety: `reportlab.platypus.Table(data, colWidths=col_w)` with `TableStyle([("GRID", (0,0), (-1,-1), 0.6, colors.black)])`, then `wrapOn/drawOn` — gets you automatic grid + cell padding.

    ---

    ## Which to pick

    | | Way 1 (PyMuPDF) | Way 2 (overlay) |
    |---|---|---|
    | Code volume | smallest | needs reportlab + pypdf |
    | Original stream | rewritten on save | untouched (pikepdf) |
    | Layout engine | manual | platypus Table available |
    | Rotation handling | must derotate yourself | overlay inherits page `/Rotate` automatically |

    Whichever you use: never save over the input, and be aware that appending content to a page invalidates any existing digital signature (the byte range no longer covers the page). For encrypted files, authenticate first (`reader.decrypt(...)` / `doc.authenticate(...)`).

    ---

    **ASKED** — Two practical Python routes to add a small ruled table into blank space on page 1 of an existing PDF.

    **WORKED** — BUILT: two runnable recipes — PyMuPDF in-place drawing (grid lines via `new_shape`, text via `insert_text`) and a ReportLab overlay stamped with `pypdf.merge_page` (or `pikepdf.add_overlay`).

    **FOUND** — The real failure modes are not the drawing calls: they are origin-flip errors (PyMuPDF is top-left/y-down, PDF is bottom-left/y-up), `/Rotate` not being applied to content-stream coordinates, and Standard-14 fonts silently dropping non-ASCII glyphs.

    **DECIDED** — Overlay approach is the safer default for unusual/signed PDFs; PyMuPDF when you want a single-file, low-ceremony edit. BOTTOM LINE: author the table as plain vector lines + text and overlay it; only edit content streams when you need to.

    **NEXT** — None — answer delivered from knowledge, no files or web consulted as instructed.
- id: pdf-tblins-02
  answer: |
    Assumption: I'm answering for the usual Python PDF stack — `pypdf` + `reportlab` (or PyMuPDF/`fitz`) — where you generate the table separately and overlay it onto an existing page.

    **1. Derive the style from the page, don't invent it**
    - Pull the source page's fonts, sizes, leading, and colors first (e.g. `page.get_fonts()`, text extraction, or `page.get_text("dict")` in PyMuPDF) and reuse them. Register/embed the same font file with `pdfmetrics.registerFont(TTFont(...))`; if it isn't available, use a metrically compatible substitute, never a random default.
    - Sample stroke/fill colors from the page (headers, rules, zebra fills) instead of defaulting to pure black/white.
    - Match line weight (~0.5–1 pt), padding, and row height to whatever the document already uses.

    **2. Match the page's geometry exactly**
    - Work in PDF points (1/72"), not pixels, and build the overlay canvas at the page's real `MediaBox`/`CropBox` size — never resize or re-render the page.
    - Account for page rotation and crop offsets. If `page.rotation != 0`, either draw the table in an unrotated coordinate space and apply the inverse rotation matrix, or draw onto a page with identical rotation. Otherwise the table lands sideways or shifted.
    - Place the table inside the existing margins and in whitespace that doesn't collide with body text; if there isn't room, add a new page rather than overprinting.

    **3. Draw on a transparent overlay, then merge**
    - Create a fresh `reportlab` canvas of identical dimensions, draw only the table (no background rectangle), save to a buffer, and merge it with `base_page.merge_page(overlay_page)` (pypdf) or `page.show_pdf_page(rect, doc, pno)` (PyMuPDF).
    - Keep it vector: real lines, rects, and text objects. Don't rasterize the table into an image — that causes resolution mismatch, fuzzy rules, and font inconsistency.
    - Watch content-stream order: the overlay must be appended after the existing content so it isn't painted under opaque elements, but avoid drawing over text.

    **4. Eliminate the classic visual glitches**
    - **Seams/hairlines between cells:** don't fill or stroke each cell independently; adjacent fills leave 1-px white gaps from rounding. Snap all coordinates to a consistent grid (round to 0.01–0.1 pt) or add a tiny overlap (~0.25 pt) on shared edges.
    - **Double-stroked borders:** draw the outer frame once and each internal gridline once. Per-cell `rect()` calls stroke shared edges twice, making them visibly darker/thicker.
    - **Text overflow:** measure with `pdfmetrics.stringWidth(text, font, size)` and wrap/truncate/clip per column; don't let text bleed into the next cell.
    - **Vertical alignment:** center on actual font ascent/descent (`getAscent`/`getDescent`), not a guessed baseline.
    - **White box covering content:** make sure the overlay page/canvas has no opaque background fill; ReportLab canvases are transparent by default, but any `setFillColor(white)` + `rect()` will punch a hole.
    - **Blurry or aliased lines:** keep stroke widths ≥ 0.5 pt and set sane line caps/joins; sub-pixel widths render inconsistently across viewers.

    **5. Verify by rendering**
    Render the merged page to an image at ~150–300 DPI (PyMuPDF `get_pixmap(dpi=200)` or `pdftoppm -r 200`) and eyeball font match, rule weight, alignment, and any seams. Compare against an untouched page of the same document to confirm no existing content was altered.

    If you don't control the origin document, the honest limit is that "consistent" means matching sampled fonts/colors/metrics — you can't perfectly reproduce an unknown embedded font, so pick the closest match and check it rendered.

    ---

    ASKED — How to draw a new table into an existing PDF page so it matches the document and doesn't glitch.

    WORKED — Gave a concrete overlay workflow: sample fonts/colors/line weights, build a same-size vector overlay in points, handle rotation/crop boxes, merge with pypdf/PyMuPDF, then fix seams, double strokes, overflow, and rasterization.

    FOUND — Most glitches are self-inflicted: rasterizing the table, per-cell strokes doubling shared edges, ignoring page rotation/offset, or drawing an opaque white background over existing content.

    DECIDED — Recommended transparent vector overlay + `merge_page`/`show_pdf_page` over regenerating the page; verified visually at 200 DPI rather than trusting code alone.

    BOTTOM LINE: Match sampled style, draw vector on a transparent same-size overlay in page coordinates, single-stroke the grid, then render to confirm.

    NEXT — None — the answer is self-contained; add a concrete code sample only if you want executable steps.
- id: pdf-tblins-03
  answer: |
    PDFs have no flow model — a page is just absolutely-positioned text and graphics, so there is no "slot" to insert into. When the boundary between the two paragraphs has no free vertical space, your real options are:

    **1. Create the gap by moving content (edit the content stream)**
    - Parse the page content stream (pikepdf/qpdf/mutool) and wrap everything below the insertion y-coordinate in `q 1 0 0 1 0 -Δ cm … Q` to translate it down by the table height Δ, then draw the table in the opened region.
    - Only works if the page has slack at the bottom. Otherwise you must also push bottom content to the next page, reduce leading/font size, or enlarge the MediaBox/CropBox.
    - Preserves original fonts and the text layer, but is fiddly: text positioning, clip paths, and any content that spans the boundary can break.

    **2. Reflow / rebuild the affected pages**
    - Extract the text and re-typeset the affected page(s) with a layout engine (ReportLab Platypus, LaTeX, WeasyPrint, etc.), treating the table as a normal block between the paragraphs.
    - Cleanest visually and semantically, and the only option that gives correct text extraction and accessibility. Cost: you lose original fidelity unless you match fonts, sizes, and margins, and it's a page-level rewrite, not a patch.

    **3. Overlay and obscure (destructive patch)**
    - Stamp the table at the boundary and cover whatever it overlaps with an opaque white rectangle (or a true redaction), then redraw the displaced paragraph text at a new position.
    - Quick, but it either hides real content or leaves a text layer that no longer matches what's visible. Acceptable only if you also rebuild/remove the covered text objects.

    **4. Put the table on its own page or layer**
    - If the paragraph boundary falls near a page break, insert a new page containing just the table between the two pages, or move the second paragraph (and following content) onto a new page after the table.
    - Or attach the table as a page annotation (FreeText/stamp/widget). Annotations float above the content without reflowing anything — fine for a visual mark, wrong if the table must be part of the document body.

    **5. Scale the existing content**
    - Slightly shrink the page content (e.g. 95–97% via the CTM) to free a few lines of space, then insert the table. A cheap variant of option 1, but it visibly changes every glyph size on the page and can hurt legibility.

    Practical ranking: reflow/rebuild (option 2) if fidelity can be sacrificed; content-stream translation (option 1) if the page has bottom slack and you must preserve the original; new page (option 4) if moving the following paragraph is acceptable; overlay+redaction (option 3) only as a last resort, and never stamp a table on top of live text without removing or redrawing it.

    Assumption: "no gap" means no whitespace at the paragraph boundary and no spare room at the page bottom — if there *is* bottom slack, option 1 alone is sufficient.
- id: pdf-imgrep-01
  answer: |
    ## Fast path

    If the logo is a normal image XObject and you're on a PyMuPDF version that has `Page.replace_image` (1.21+):

    ```python
    import pymupdf as fitz          # or: import fitz

    doc  = fitz.open("in.pdf")
    page = doc[0]

    # 1) find the logo's xref
    for xref, smask, w, h, bpc, cs, altcs, name, filt, ref in page.get_images(full=True):
        rects = page.get_image_rects(xref)      # where/how big it is drawn
        print(xref, name, f"{w}x{h}", "smask:", smask, rects)

    logo = 12                                    # pick from the list above
    before = page.get_image_rects(logo)          # save for verification

    # 2) swap the image data, keep the placement
    page.replace_image(logo, filename="logo_new.png")

    doc.save("out.pdf", garbage=4, deflate=True, clean=True)
    ```

    Position and size are preserved *because they were never stored on the image*. A PDF image is just a pixel buffer; where and how big it appears is decided by the CTM (`cm`) in the content stream at the `Do` operator that draws it. `replace_image` swaps the object's stream/dimensions and leaves the content stream and the display rectangle untouched — the new PNG is squeezed into the existing box, whatever its pixel dimensions.

    Verify with `page.get_image_rects(logo)` (should be identical to `before`) and a render: `page.get_pixmap(dpi=150).save("check.png")`.

    ## The catch: an image is a *document* object, not a page object

    An XObject xref is owned by the document catalog, not the page. Pages merely point at it from `/Resources /XObject /Im0`. So:

    - **A header logo repeated on every page is one xref referenced by every page.** `replace_image(logo, ...)` mutates that single object — every page's logo changes, not just page 1. Same for a logo tiled/duplicated several times on page 1: all occurrences change.
    - Conversely, images that look identical can be *different* xrefs (duplicated at authoring time), so picking "the first image on page 1" can be wrong.

    ### Page-1-only workaround: re-point the resource name

    Give page 1 its own pointer to a new image object instead of editing the shared one:

    ```python
    old_xref, name = 12, "Im0"          # name from page.get_images()

    # PyMuPDF has no public doc.insert_image, so create the object on a scratch page
    tmp      = doc.new_page()                                   # appended at end
    new_xref = tmp.insert_image(fitz.Rect(0, 0, 10, 10), filename="logo_new.png")
    doc.delete_page(tmp.number)                                 # xref survives until GC

    # point page 1's resource entry at the new object; other pages keep the old one
    doc.xref_set_key(page.xref, f"Resources/XObject/{name}", f"{new_xref} 0 R")

    doc.save("out.pdf", garbage=4, deflate=True)
    ```

    Two traps in that snippet:

    1. **`/Resources` can itself be a shared indirect object.** If `doc.xref_get_key(page.xref, "Resources")` returns `("xref", "123 0 R")` and other pages reference `123`, editing `Resources/XObject/Im0` changes them too. Check who else points at it, or give page 1 a private copy of the dict first.
    2. `insert_image` may return `None` on older versions — then read the xref back from `tmp.get_images(full=True)`.

    Cheap-but-dirty alternative: `page.insert_image(rect, filename=..., keep_proportion=True, overlay=True)` for each rect, drawing on top of the old one. Fine only for an opaque, same-aspect replacement — the old bytes stay in the file and show through anywhere the new image doesn't cover.

    ## Other things that bite

    - **Aspect ratio is not preserved.** `replace_image` keeps the old rectangle and has no `keep_proportion`. A new logo with a different aspect gets stretched. Recompute the fitted rect yourself and insert fresh if that matters.
    - **Soft masks.** If `get_images` reports `smask != 0`, transparency is a *separate* object. Replacing only the base image leaves the old mask in charge; the new PNG's alpha may be ignored or double-applied. Verify visually, and update the smask xref's stream too (`doc.update_stream(smask_xref, ...)`) or insert the image anew instead.
    - **Nested / inline images.** If `get_images()` is empty or `replace_image` fails, the logo is probably inside a Form XObject (header template) or stored inline (`BI … ID … EI`). Use `page.get_image_info(xrefs=True)` (it reflects the rendered display list, so it sees nested forms) to locate it, then patch that form's stream/resource — `replace_image` on the page won't reach it.
    - **Baked-in transforms.** Flip/rotate in the image matrix is preserved, so a mirrored or rotated placement stays mirrored after the swap.
    - **Old bytes linger** unless you save with `garbage=3/4` (`deflate=True` to recompress). With `garbage=0` the file grows by the old image.
    - **Encrypted / signed PDFs:** authenticate first, save incrementally (`doc.save(..., incremental=True, encryption=fitz.PDF_ENCRYPT_KEEP)`) — a full rewrite breaks signatures.
    - **Resolution:** placement is resolution-independent, so a higher-DPI PNG is safe to drop in and only costs file size.

    **Assumption:** the logo is a regular raster XObject referenced by page 1's own content stream, and you're on a PyMuPDF build exposing `Page.replace_image`. On older builds, do it manually: `rects = page.get_image_rects(xref)` → `page.delete_image(xref)` (fails if drawn more than once) → `page.insert_image(rect, filename=...)`.
- id: pdf-imgrep-02
  answer: |
    **The old logo is still there — you covered it, you didn't replace it.**

    `insert_image` only appends a new image-drawing operation to the page's content stream. The original image XObject remains in the file, still referenced by the page's resources, still drawn (just painted over). "Looks right" is a rendering accident, not a correct document.

    What's actually wrong:

    - **Not a real replacement.** `pdfimages`/extraction still yields both images. Anyone editing the PDF can delete your overlay and the original reappears. Any redaction or sanitization step that only touches the visible layer leaves the old logo intact.
    - **File bloat and duplicate resources.** Two full images, two XObject entries, often two color-space/ICC blobs. Repeated "updates" compound this.
    - **Z-order fragility.** The overlay is only correct because it's drawn later. Reordering content, flattening, form-XObject wrapping, or an annotation/stamp layer can bring the old logo back on top.
    - **Show-through and edge bleed.** If the new image has alpha, or doesn't match the old rect exactly after aspect-ratio fitting, the old logo's edges/background peek out. `insert_image` preserves aspect ratio and centers by default, so the new artwork is frequently slightly scaled or letterboxed inside the old rect — the visible result "looks right" at 100% zoom but isn't pixel-aligned.
    - **Geometry mismatch.** If the old logo was inside a transformed/clipped container (rotated page, form XObject, non-identity CTM), the same `rect` in page space may not map to where the old image actually landed.
    - **Downstream metadata lies.** Alt text, tagging/accessibility structure, and OCR results still describe the old image.

    Correct approaches: remove the original resource (PyMuPDF `page.delete_image(xref)` / low-level XObject deletion, or redact the region with the image-removal option so the content stream is rewritten), *then* insert the replacement; or rebuild the page/overlay from scratch. If you truly must overlay, at minimum verify the old XObject is gone and the new one is pixel-aligned to the old bbox.

    **Assumption:** the operation is a PyMuPDF-style `page.insert_image(rect, ...)` over a raster logo in the page content stream, not an annotation or a genuine resource replacement.
- id: pdf-imgrep-03
  answer: |
    Assuming you mean PyMuPDF (`fitz`): `page.get_images()` is a **resource-dictionary query**, not a “what is painted on the page” query. It returns raster image XObjects reachable from the page’s `/Resources`. A visible logo can be something else entirely.

    ## What it can be

    1. **Vector artwork** — very common for logos. The logo is a set of paths (`re`, `m`, `l`, `c`, `f`, `S` operators), so there is no image object at all.
    2. **Text or a font glyph** — including a symbol font, a custom embedded font, or a **Type 3 font** whose glyphs are small content streams. `get_images()` sees nothing.
    3. **An inline image** — `BI ... ID ... EI` inside the content stream. Inline images are not XObjects, so `get_images()` never returns them.
    4. **An image inside a Form XObject** — the page only has `/Fm0 Do`; the actual image is in the form’s resources. Try `page.get_images(full=True)`; the last field is the **referencer** (page or form XObject). The same applies to images inside annotation appearance streams and tiling patterns.
    5. **An annotation appearance** — stamp, watermark, widget, etc. Annotations are not page content and are not in the page’s `/Resources`.
    6. **A shading or pattern** — a gradient or tiling pattern can paint a logo without a conventional image XObject.
    7. **Not in the PDF at all** — a viewer-added watermark/overlay, or the logo is on a different page.

    ## How to find it

    Match the logo’s visual rectangle against what the PDF says is painted there.

    ```python
    import fitz

    page = doc[pno]
    logo_rect = fitz.Rect(...)   # from a render / visual inspection

    # Image XObjects, including those inside Form XObjects.
    for img in page.get_images(full=True):
        xref, smask = img[0], img[1]
        referencer = img[9] if len(img) > 9 else 0
        print("XObject image:", xref, "smask:", smask, "referencer:", referencer)

    # Images actually placed on the page, with bboxes.
    for info in page.get_image_info(xrefs=True):
        print("painted image:", info.get("xref"), fitz.Rect(info["bbox"]),
              info["width"], info["height"])

    # What is painted at the logo location, in paint order.
    for kind, bb in page.get_bboxlog():
        if fitz.Rect(bb).intersects(logo_rect):
            print("primitive:", kind, bb)

    # Vector paths.
    for d in page.get_drawings():
        if d["rect"].intersects(logo_rect):
            print("vector:", d["rect"], d["type"], d["fill"], d["color"])

    # Text / Type 3 fonts.
    for f in page.get_fonts(full=True):
        if f[2] == "Type3":
            print("Type3 font:", f)

    # Annotations.
    for a in page.annots():
        if a.rect.intersects(logo_rect):
            print("annotation:", a.type, a.rect)
            print("appearance:", doc.xref_get_key(a.xref, "AP/N"))
    ```

    Interpretation:

    - `get_bboxlog()` says `fill-image`, and `get_image_info()` has an `xref`: normal image or Form XObject image.
    - `fill-image` but `get_images()` is empty and `xref` is 0/missing: inline image.
    - `fill-path`: vector. `get_drawings()` gives the paths.
    - `fill-text`: text/Type 3 glyph. Use `get_text("rawdict")`, `get_texttrace()`, and `get_fonts()`.
    - `fill-shade`: shading/pattern. Inspect `/Resources /Pattern`.
    - Nothing in `get_bboxlog()`, but `page.annots()` finds something: annotation appearance.
    - Nothing anywhere: check another renderer (`pdftoppm`, `mutool draw`). If the logo disappears there, it was a viewer overlay, not file content.

    You can also inspect the raw content stream:

    ```python
    data = b"".join(doc.xref_stream(x) for x in page.get_contents())
    # look for b"Do" (XObject), b"BI"/b"ID"/b"EI" (inline image),
    # b"sh" (shading), text operators, and path-painting operators
    ```

    For objects nested in patterns or annotation appearances, walk the object graph with `pikepdf` or `doc.xref_object()` / `doc.xref_stream()` through `/XObject`, `/Pattern`, and `/AP`.

    A quick confirmation trick: render just the logo rectangle, cover or remove the candidate object, render the same rectangle again, and compare.

    ## How to replace it

    ### 1. It really is an image XObject

    Use the xref:

    ```python
    page.replace_image(xref, filename="new_logo.png")
    ```

    This is the cleanest path because it preserves the image’s placement in the content stream.

    Caveats:

    - The xref may be shared by other pages or Form XObjects. Replacing it can change every occurrence. Test the whole document.
    - The old placement matrix is retained, so a different aspect ratio will be stretched.
    - If `smask != 0`, the image has an old soft mask. Check transparency after replacement; the old mask may need replacing or removing.

    If `replace_image()` is unavailable or refuses a nested xref, use `pikepdf` or `doc.update_stream()` — but you must also get `/Width`, `/Height`, `/ColorSpace`, `/BitsPerComponent`, `/Filter`, and `/SMask` right. Do not just overwrite the stream with PNG bytes.

    ### 2. There is no image xref: vector, text, inline, or annotation interior

    Erase the old mark and insert the new logo:

    ```python
    page.add_redact_annot(logo_rect, fill=(1, 1, 1))  # use the real background color
    page.apply_redactions(
        images=fitz.PDF_REDACT_IMAGE_REMOVE,
        graphics=fitz.PDF_REDACT_LINE_ART_REMOVE_IF_TOUCHED,
        text=fitz.PDF_REDACT_TEXT_REMOVE,
    )
    page.insert_image(logo_rect, filename="new_logo.png",
                      keep_proportion=True, overlay=True)
    ```

    Important details:

    - The default for `graphics` is usually `PDF_REDACT_LINE_ART_NONE`, so vector art will survive unless you explicitly set removal.
    - `PDF_REDACT_LINE_ART_REMOVE_IF_TOUCHED` can remove a whole path that merely crosses the rectangle. Try `PDF_REDACT_LINE_ART_REMOVE_IF_COVERED` or edit the content stream if that removes too much.
    - Redaction is blunt. If the logo sits on a photo or gradient, a flat fill leaves a visible patch; `images=REMOVE` may delete an underlying background image. For those cases, remove only the logo’s drawing operators from the content stream, or patch the background in an image editor and insert that.
    - Redaction generally does not rewrite content inside Form XObjects or non-redaction annotation appearances. You can still cover a Form XObject visually with a page-level image, but actual removal requires editing/forking the form.
    - If the new logo is vector, put it on a one-page PDF and use `page.show_pdf_page(rect, logo_doc, 0, overlay=True)` rather than rasterizing it.

    ### 3. Text/Type 3

    If the logo is extractable text:

    ```python
    for r in page.search_for("OLD LOGO TEXT"):
        page.add_redact_annot(r)
    page.apply_redactions()
    page.insert_image(rect, filename="new_logo.png", overlay=True)
    ```

    If it is outlined or Type 3, `search_for()` may find nothing; use the visual rectangle or `get_texttrace()` bboxes. If the replacement is text rather than an image, use `insert_text()` / `insert_htmlbox()` with a suitable embedded font.

    ### 4. Inline image

    There is no xref. Either:

    - redact with `images=PDF_REDACT_IMAGE_REMOVE` and insert the new image, or
    - patch the `BI ... ID ... EI` block in the content stream, keeping the inline image dictionary and byte length consistent, or
    - normalize the PDF first so inline images become XObjects, then use `replace_image()`.

    ### 5. Annotation

    Page-level `insert_image(..., overlay=True)` will **not** cover an annotation, because annotations paint above page content. Either update the annotation’s `/AP /N` appearance stream, or remove the annotation and put a normal page image in its place:

    ```python
    for a in list(page.annots()):
        if a.rect.intersects(logo_rect) and a.type[1] in ("Stamp", "Watermark"):
            page.delete_annot(a)

    page.insert_image(logo_rect, filename="new_logo.png", overlay=True)
    ```

    For widgets, update the field appearance or flatten the form first.

    ### 6. Pattern or shading

    Edit the pattern/shading object and its resources if you need a true replacement; otherwise cover it with the page-level image. Traverse `/Pattern` and the pattern’s `/Resources` manually.

    ## Verify

    Render the page at high DPI before and after, with the same clip rectangle, and compare. Also check:

    - other pages, if the image xref was shared;
    - text extraction, if the old logo was text (covering it with an image does not remove the text layer);
    - transparency and aspect ratio;
    - file size and whether the old image object is still referenced.

    The practical rule: **first ask what primitive is painted, then replace that primitive** — do not assume a visible logo is an image XObject.
- id: pdf-imgins-01
  answer: |
    ## Short answer

    Work in page points (PyMuPDF units, 1/72"), with the origin at the **top-left** and y growing downward. The signature's height follows from the PNG's own aspect ratio:

    ```
    w = 150
    h = 150 * png_pixel_height / png_pixel_width

    x1 = page.rect.x1 - margin
    y1 = page.rect.y1 - margin      # page.rect.y1 is the bottom edge
    rect = fitz.Rect(x1 - w, y1 - h, x1, y1)
    ```

    Then check that rect against the text blocks (and images/links). If it collides, lift the box upward so its bottom sits `gap` above the topmost thing it hit. Insert with `page.insert_image(rect, filename=..., keep_proportion=True)`.

    ## Code

    ```python
    import fitz  # PyMuPDF

    def place_signature(src_pdf, sig_png, out_pdf,
                        width=150.0, margin=36.0, gap=6.0):
        doc  = fitz.open(src_pdf)
        page = doc[0]                              # page 1 (0-based)

        # 1) aspect ratio from the PNG itself (pixel ratio is unitless)
        pm     = fitz.Pixmap(sig_png)
        height = width * pm.height / pm.width

        # 2) bottom-right candidate box in page coordinates
        pr = page.rect                             # top-left origin, y down
        x1 = pr.x1 - margin
        x0 = x1 - width
        y1 = pr.y1 - margin
        rect = fitz.Rect(x0, y1 - height, x1, y1)

        # 3) collect what must not be covered
        occupied = []
        for b in page.get_text("blocks"):          # (x0,y0,x1,y1,text,block_no,type)
            if b[4].strip():
                occupied.append(fitz.Rect(b[:4]))
        for info in page.get_image_info():         # existing images
            occupied.append(fitz.Rect(info["bbox"]))
        for link in page.get_links():              # clickable areas
            if link.get("from"):
                occupied.append(fitz.Rect(link["from"]))

        def overlap(a, b):
            ix0, iy0 = max(a.x0, b.x0), max(a.y0, b.y0)
            ix1, iy1 = min(a.x1, b.x1), min(a.y1, b.y1)
            return max(0.0, ix1 - ix0) * max(0.0, iy1 - iy0)

        # 4) move the box up until it is clear (monotonic, so it terminates)
        for _ in range(len(occupied) + 1):
            blockers = [o for o in occupied if overlap(rect, o) > 0]
            if not blockers:
                break
            top  = min(o.y0 for o in blockers)     # highest thing we hit
            rect = fitz.Rect(x0, top - gap - height, x1, top - gap)
            if rect.y0 < pr.y0 + margin:
                raise RuntimeError("no free area at the bottom right")

        # 5) insert; the rect already has the exact image ratio
        page.insert_image(rect, filename=sig_png,
                          keep_proportion=True, overlay=True)

        doc.save(out_pdf, garbage=4, deflate=True)
        doc.close()
        return rect
    ```

    ## Why each part

    - **`doc[0]`** — "page 1" is index 0.
    - **`fitz.Pixmap(png).width/height`** — gives the intrinsic pixel size; the ratio is what matters, so DPI metadata is irrelevant for geometry. (Use a higher-resolution PNG — e.g. ≥ 625 px wide for 150 pt at 300 dpi — so it doesn't look pixelated.)
    - **`page.rect.x1 / .y1`** — right and bottom edges. Using `x1`/`y1` rather than `width`/`height` is safer if the page box has a nonzero origin.
    - **`rect = (x1-w, y1-h, x1, y1)`** — width is pinned to 150 pt and height comes from the ratio, so `keep_proportion=True` is just a safety net; it has nothing left to correct.
    - **Text check** — `page.get_text("blocks")` returns text block bboxes in page coordinates (same space as `insert_image`). `get_image_info()` and `get_links()` catch non-text collisions. Overlap is computed by explicit intersection area so edge-touching doesn't count.
    - **Sliding upward** — each pass moves `rect.y1` to the top of the highest blocker (minus a gap), so the box only ever moves up and the loop cannot spin.

    ## Practical refinements

    - **Transparent padding:** a signature PNG usually has empty alpha margins, so the *visible* ink is narrower than 150 pt. Trim first if 150 pt means the ink width:
      ```python
      from PIL import Image
      im = Image.open("sig.png").convert("RGBA")
      im.crop(im.getbbox()).save("sig_trim.png")
      ```
      Then compute the ratio from the trimmed file.
    - **No room at the bottom:** if you'd rather keep it bottom-right than push it up mid-page, compute the free band `avail = (pr.y1 - margin) - (lowest_text_y1 + gap)` and scale down: `h = avail; w = h * pm.width / pm.height`.
    - **Scanned/OCR-less pages:** `get_text("blocks")` is empty because the text is pixels. Run `page.get_textpage_ocr()` or just rely on a reserved bottom margin.
    - **Rotated pages (`/Rotate ≠ 0`):** `page.rect` is the visible (rotated) box and `insert_image` accounts for page rotation, so the same visible-page rect normally works. If a particular version places it oddly, convert with `rect * page.derotation_matrix` (rotated → unrotated coordinates).
    - **Verify:** reopen the output and check `page.get_image_info()[0]["bbox"]` matches `rect`, or render `page.get_pixmap(dpi=150)` and eyeball the corner.
    - **Saving:** `doc.save(out_pdf, ...)` writes a new file; if you must modify in place, use `doc.saveIncr()` after opening the original path (don't `save` onto the input).
- id: pdf-imgins-02
  answer: |
    **Short answer:** don't call `insert_image` 200 times as if each call were independent. Embed the logo **once** as a single image XObject, then have every page *reference* that same object. The file then grows by a few hundred bytes per page (content-stream ops + a resource entry), not by 200 × the logo's bytes.

    ## Why it happens

    In PDF, a picture on a page is an *Image XObject* — a stream of pixel data identified by an object number. A page's `/Resources /XObject` dictionary maps a name (`/Im0`) to that object, and the content stream draws it with `q … cm /Im0 Do Q`. Most convenience APIs, when handed a filename or bytes, create a **brand-new** image object on every call. 200 calls → 200 pixel streams → linear file growth.

    ## Fix at build time (the right fix)

    **1. Share one XObject across pages.** Create the image object once, register it in each page's resource dictionary under the same name, and emit only the transform + `Do` operator per page. Because PDF objects are referenced by number, all 200 pages point at the same bytes. Growth ≈ content stream + 1 dictionary entry/page.

    **2. Or wrap it in a Form XObject.** Make a one-page form containing the logo once, put that form in the resources of every page, draw it. Same effect; also lets you add text/watermark around it.

    **Tool-specific knobs**

    - **PyMuPDF (fitz):** `insert_image` accepts an `xref=` argument. Insert once with `filename=...`, read the xref back via `page.get_images()`, then pass `xref=xref` on the other 199 pages. `show_pdf_page()` across a source doc likewise reuses the source page's objects rather than re-embedding pixels.
    - **ReportLab:** build one `ImageReader("logo.png")` and pass *that same object* to every `drawImage()` call — it caches the XObject by reader identity. Passing the path string each time can re-embed.
    - **pikepdf / qpdf / raw writing:** construct the `PdfImage`/stream once, add it to each page's `/Resources`, and append a small content stream. qpdf alone won't dedupe for you.
    - **InDesign / Illustrator / LaTeX:** placing a linked graphic or `\includegraphics` of the same file normally does exactly this already — PDF's native model is "define once, reference many."

    **Caveat:** dedupe only works if the bytes and the image parameters are identical. Re-encoding, resizing, or changing alpha/colorspace per page produces genuinely different objects and defeats it. Also watch the opposite trap: a soft mask or SMask per page can multiply too — share that as well.

    ## Fix after the fact

    If you already have the bloated file: Ghostscript's `-dDetectDuplicateImages=true` (with `-sDEVICE=pdfwrite`) merges byte-identical images, and `cpdf -squeeze` / `mutool clean -gggg` do garbage collection and resource consolidation. These help but are lossy about intent — better to construct it correctly the first time.
- id: pdf-imgins-03
  answer: |
    Assuming PyMuPDF (`fitz`) and the usual `page.insert_image(rect, filename=…/pixmap=…)` path, here are the realistic causes, roughly in order of likelihood.

    ## 1. Z-order — the image is genuinely *under* something

    PDF paints content in stream order; later content covers earlier content. `insert_image` defaults to `overlay=True` (append on top), so this bites when:

    - You passed **`overlay=False`**. The image is prepended *below* all existing page content. If the page has a white/filled background rectangle (very common in generated templates), the image is now behind it and invisible.
    - You inserted the image **first**, then drew the background: `page.draw_rect(rect, color=None, fill=(1,1,1))`, a header/footer band, a watermark, or a `show_pdf_page()` stamp **after** the `insert_image()` call with `overlay=True`. The later call covers the image.
    - A template/stamp page was composited on top of the page afterwards.
    - You inserted into a page whose `/Contents` is an array and something else appended a stream later in the pipeline.

    Fix: insert the image last, or keep `overlay=True`, or draw backgrounds with `overlay=False` *before* inserting.

    ## 2. Geometry — the image is off-page, clipped, or zero-sized

    - **Rect outside the CropBox/MediaBox.** Anything outside the visible page area is clipped away and renders as nothing. Very easy to do with mixed units: image pixel dimensions treated as points (a 4000 px image placed at x=4000 lands off an A4 page), or millimetres vs points.
    - **Y-axis flip.** PDF origin is bottom-left; image editors are top-left. Using top-left coordinates directly can push the rect below y=0 (off-page) or mirror placement.
    - **Empty/inverted rect.** `Rect(x0,y0,x1,y1)` with `x1<=x0` or `y1<=y0`, or a rect built from a width/height of 0. Nothing visible.
    - **Page `/Rotate` not accounted for.** With a rotated page the visible area is transformed; your rect may map outside the displayed region or be rotated oddly. Set rotation to 0 and restore, or use the derotation matrix.
    - If only part is missing, suspect clipping rather than z-order.

    ## 3. Wrong page or wrong file

    - **0-based page index.** `doc[1]` is page 2. Inserting into a cover page or a page that isn't the one you're looking at.
    - **Stale page object.** You grabbed `page` before inserting/deleting pages; it can now point at a different page or a detached one.
    - **Not saved / saved to another path.** Insertions are in memory only until `doc.save(out)`. Saving over the opened file needs incremental save or a new path; also, a viewer showing the old file may cache it — reopen the output.
    - Exceptions swallowed: a bad filename or unsupported format raises, and a bare `except: pass` makes it look like a silent no-op.

    ## 4. Existing content stream is in a bad graphics state

    If the page's content leaves an open `q` without `Q`, a leftover `cm`, or an active clip path, your appended content inherits that CTM/clip and can end up scaled to nothing, translated off-page, or clipped out. This is a classic "image inserted but invisible" case for messy third-party PDFs.

    Fix: `page.wrap_contents()` (or `page.clean_contents()`) before inserting, so your stream starts from a clean graphics state.

    ## 5. Transparency / alpha

    - A `Pixmap` with an alpha channel that is fully (or effectively) transparent, or a zeroed/uninitialised SMask → renders as nothing.
    - Bad or mismatched soft mask when constructing the pixmap manually.
    - Unusual colour spaces (DeviceN, some ICC/CMYK JPEGs) can render black or blank in some viewers, though rarely fully invisible.

    ## 6. Hidden optional content

    `insert_image(..., oc=…)` puts the image in an Optional Content Group. If that OCG is off by default or toggled off in the viewer, the image exists in the file but is not drawn. Drop the `oc` argument to test.

    ## 7. Annotations paint above page content

    Anything in the annotation layer — a filled Square/Circle, FreeText, stamp, or a redaction-style overlay — always draws above page content, even with `overlay=True`. If the "background box" is an annotation, your image can never cover it; you must remove or reorder the annotation, or flatten it into page content first.

    ## Fast triage

    1. Insert as the very last operation with default `overlay=True`; still invisible? → not z-order.
    2. Print the rect and the page's `page.rect` / `page.cropbox`; confirm it's inside and non-degenerate.
    3. `page.get_images(full=True)` / `page.get_contents()` — is the XObject actually referenced, and in which stream?
    4. `page.wrap_contents()` then reinsert.
    5. Save to a *new* file, close, reopen, and render with `page.get_pixmap()` to check independently of your viewer.
    6. If `get_pixmap()` shows it but the viewer doesn't → viewer caching or an OCG.

    Most common single answer: **`overlay=False` (or a background drawn after the image) putting it beneath a filled background**, followed by **a rect outside the CropBox** and **an unbalanced/clipped existing content stream**.

    BOTTOM LINE: the two dominant causes are paint order (image below the background) and geometry (rect clipped/off-page); content-stream clipping, wrong page/unsaved output, alpha, hidden OCGs, and annotation layering cover the rest.
- id: pdf-fonts-01
  answer: |
    The short version: a subset font is not the whole font. It contains only the glyphs the original document happened to use, plus a character map (cmap) and, for composite fonts, glyph-ID/CID mappings that cover only those characters. Writing new letters into the content stream does not add glyphs to the font program.

    Why reuse fails:

    - The new characters have no entry in the font's cmap (simple TrueType/Type1 fonts) or no valid CID-to-GID mapping (Type0/CIDFontType2 fonts). The renderer resolves them to glyph 0, `.notdef`, so you get blanks, boxes, or nothing.
    - Subset fonts are often renumbered: glyph IDs in the embedded program do not match the original full font's glyph IDs. Copying codes or GIDs from the full font into the subset gives the wrong glyph or no glyph.
    - The `/Widths` array (simple fonts) or `/W` array (composite fonts) only lists the old characters. Even if a glyph existed, spacing/positioning would be wrong or missing.
    - `/ToUnicode` maps only the original characters, so copy/paste and text extraction of the new name break even if it renders.
    - The font may have been subset under an embedding license that permits only the original subset; adding glyphs can be a licensing violation. It may also have had tables (glyph names, kerning, hinting) stripped, so patching it is unreliable.
    - Some subsets are stored with truncated or nonstandard tables that make naive byte-level patching corrupt the font.

    What to do instead:

    1. Replace the font resource for that text with a full font that contains the needed glyphs. Embed the complete font (or re-subset it from the full font to include the old and new characters). Use a PDF library or font tool that does proper embedding — e.g., reportlab/pikepdf plus the full TTF, or fontTools to build a new subset from the original full font — rather than editing the embedded font bytes.
    2. Register the new font correctly: add a new font object/resource, set `/Widths` or `/W` from the full font's metrics, provide a correct `/Encoding` and `/ToUnicode` CMap, and for Type0 fonts a correct `/CIDToGIDMap`.
    3. Rewrite the affected text-showing operators (`Tj`, `TJ`) to select the new font resource and use codes valid for that font. Do not assume the old character codes still mean the same letters.
    4. Keep using the old subset for unchanged text if you like, or re-embed all text with the full font and drop the subset. Mixing is fine as long as each text run selects the right font resource.
    5. If the original font is unavailable or not licensed for full embedding, substitute a metrically compatible font or a standard base-14 font (Helvetica/Times) for the changed item, and check that the layout still fits. Adjust the text position/width if the new name is longer.
    6. Validate: render the page, verify no `.notdef` boxes, check text extraction/copy-paste, and run a PDF validator (e.g., qpdf --check, veraPDF) if PDF/A or PDF/UA compliance matters.

    Assumption: the case is a typical embedded subset (simple TrueType or Type0/CIDFontType2) with a six-letter subset prefix, as in "AAAAAA+Georgia". The same reasoning applies to CFF/OpenType subsets, where the CFF charset and charstrings are also reduced.
- id: pdf-fonts-02
  answer: |
    **PyMuPDF base‑14 names**

    - Helvetica (regular) → `"helv"`
    - Helvetica‑Bold → `"hebo"`

    (The other Helvetica variants are `"heit"` for oblique and `"hebi"` for bold‑oblique; the rest of the base‑14 set is `"tiro"`, `"tibo"`, `"cour"`, `"symb"`, `"zadb"`, etc.)

    **Limitation**

    Base‑14 fonts are the standard, *non‑embedded* PDF fonts. Their usable glyph set is fixed by their built‑in encoding (effectively the Latin‑1 / WinAnsi range — basic Latin letters, digits, common punctuation, and a limited set of accented/special characters). Anything outside that range — CJK, Cyrillic, Greek beyond the encoded few, Arabic/Hebrew, emoji, most arbitrary Unicode code points — has no glyph, so PyMuPDF cannot render it correctly. Depending on version, such characters raise an error (e.g. a "character not in font"/encoding failure) or come out as missing/incorrect glyphs.

    To show non‑Latin or arbitrary Unicode text you must embed a real font file, e.g. `page.insert_font(fontname="F0", fontfile="...")` (or `pymupdf.Font(fontfile=...)` with `insert_text(..., fontname="F0", fontfile=...)`), or use one of the CJK font helpers. Also note base‑14 fonts are not embedded in the PDF, so the viewer substitutes its own copy — fine for standard Latin text, but not a guarantee of identical metrics/appearance.
- id: pdf-fonts-03
  answer: |
    ## Short answer

    You don't guess the style from what it looks like — you read it out of the PDF: the `Tf` operator gives the font resource and nominal size, the page (or Form XObject) `/Resources /Font` dict resolves that resource to a font object, and that font's `/BaseFont`, `/FontDescriptor` (`/FontWeight`, `/StemV`, `/Flags`), and `/FontFile*` entries tell you the real weight and which program to reuse. The apparent "different size" almost always means your insertion used a rounded/default size, or the original encoded size in the text matrix (`Tm`) rather than in `Tf`.

    ## Detect the original weight

    1. **Find the text run, not the cell.** Extract characters with font metadata to confirm which font the header actually uses:
       - pdfplumber: `page.chars` → `fontname`, `size`, `matrix`.
       - PyMuPDF: `page.get_text("dict")` → span `font`, `size`, `flags` (bit 16 = bold, 2 = italic, 1 = serif, 8 = monospaced).
    2. **Get the raw content stream** (`qpdf --qdf --object-streams=disable`, `mutool show file.pdf pages`, `pdftk … output - uncompress`, or PyMuPDF `page.read_contents()`). Locate the `BT … ET` block containing the header string and read `/F2 9.96 Tf`.
    3. **Resolve `/F2`** in `page /Resources /Font` (or the containing Form XObject's resources — headers are often inside an XObject with its own font dict).
    4. **Classify the weight** from the font object:
       - `/BaseFont /ABCDEF+Arial-BoldMT` — strip the 6-letter subset tag and the `+`; suffix `Bold`, `Bd`, `Black`, `Heavy`, `Semi` is the giveaway.
       - `/FontDescriptor /FontWeight 700` (PDF 1.5+, often missing).
       - `/Flags` bit 19 (`1 << 18` = 262144) = ForceBold; bit 7 = Italic.
       - `/StemV` — regular Helvetica ≈ 88, Helvetica-Bold ≈ 140; a high value relative to the family means bold.
       - Check `/FontFile` (Type1), `/FontFile2` (TrueType), `/FontFile3` (CFF/OpenType) — that is the program to reuse. `pdffonts file.pdf` shows name, type, embedded/subset in one shot.
    5. **Rule out faux bold.** If the text uses render mode `Tr 2` (fill+stroke) with a nonzero `w`, or the same string is drawn twice with a small offset, the bold is synthetic and the font is regular. Also check `Tr 3` (invisible) — if this is an OCR text layer, the visible header is an image and font matching changes nothing visually.
    6. **Verify numerically if the metadata is ambiguous:** render the cell at 300–600 dpi and measure vertical stem thickness or ink density against candidate regular/bold fonts. Metadata lies in generated PDFs; pixels don't.

    ## Detect the original size

    The size you see is not necessarily the `Tf` number.

    - Effective size ≈ `Tf_size × sqrt(a² + b²)` from the current text matrix `a b c d e f Tm`, also scaled by the `cm` CTM and page `/UserUnit`. Some generators write `Tf 1` and put the size entirely in `Tm` (e.g. `0.996 0 0 0.996 … Tm`), so comparing raw `Tf` values between cells is misleading.
    - Keep the **exact float**, e.g. 9.9626 — not 10. Rounding is the single most common cause of "slightly different size."
    - Check text state that changes apparent extent: `Tz` (horizontal scaling), `Tc`/`Tw` (char/word spacing), `Ts` (rise), `/Rotate` on the page.
    - Cross-check with the extractor: pdfplumber `char["size"]` and PyMuPDF span `size` report the effective size; if they disagree with the raw `Tf`, the matrix is doing the scaling.

    ## Match it

    - **Reuse the existing font resource/program**, don't register a lookalike. In PyMuPDF: `page.get_fonts(full=True)` → `doc.extract_font(xref)` → `page.insert_font(fontname="F2", fontbuffer=buf)` → `insert_text(..., fontname="F2", fontsize=9.9626)`. That preserves glyph widths, hinting, and the embedded program.
    - If the font is **subsetted**, the subset may not contain your new glyphs; extraction then fails or falls back silently. In that case embed a full font of the same family/weight (or the base-14 equivalent, e.g. `Helvetica-Bold`) and expect minor metric drift; confirm with `/Widths` or the `/W` array for Type0 fonts.
    - Set the size explicitly and, if the original used a scaled `Tm`, replicate both `Tf` and `Tm` — don't let the library apply its scale a second time.
    - If bold is synthetic, reproduce it: `Tr 2` plus `w ≈ size/25–size/30` stroke, or a 0.2–0.3 pt double-draw. Don't fake bold when a real bold program exists, and don't use a real bold program where the original was faux-bold.
    - Copy `Tc`/`Tw`/`Tz` and the baseline origin if the header is letter-spaced or precisely aligned.

    ## Verify

    1. Re-extract: the new span's `fontname` and `size` must equal the original's, and PyMuPDF's bold flag (16) must match.
    2. `pdffonts` again — same font, still embedded, subset state unchanged.
    3. Render before/after at 300 dpi and diff the cell region; only the intended glyphs should change. Compare stroke width and ink density.
    4. Check in the actual target viewer (Acrobat, Preview, Chrome) — missing fonts get synthesized differently per viewer, which can reintroduce the weight/size mismatch even when the PDF is correct.

    **Assumption:** this is a text cell in a tagged or simple content stream, not an image-only scan. If the header is raster, there is no font to match — you're editing an OCR overlay (`Tr 3`) and appearance work belongs in the image.
- id: pdf-verify-01
  answer: |
    Verify at the artifact level: **reopen the saved file from disk in a fresh process, extract the table, and assert on the exact cell content and geometry — then look at rendered pixels.** A green exit code only proves the script didn't throw.

    Assumptions: the edit was done programmatically (PyMuPDF/pdfplumber/reportlab/etc.) and written to a new file; "worked" means the cell shows and extracts as intended, with no collateral damage.

    **Checks that actually catch failures**

    1. **Fresh read.** Open the output path with a new handle. Asserting against the in-memory `Document` you just wrote hides incremental-save and flush problems. Confirm the file size/xref actually changed and that the edit is on disk.

    2. **Assert on the cell, and assert the old value is gone.** Use a table extractor (`pdfplumber.extract_tables()` or `page.find_tables()`), index `tables[0][row][col]`, and compare to the expected string exactly. Then search the page text for the *previous* value. A white rectangle drawn over old text, or text drawn on top, passes a visual glance while leaving the old string extractable — that's the most common false pass.

    3. **Confirm it's page content, not an annotation.** FreeText/stamp overlays look like edits in a viewer but don't appear in `get_text()`, can be hidden or non-printing, and get stripped by downstream tools. Check `page.annots()` is empty (or only what you expect) *and* that the string appears in `page.get_text()` and lives in the content stream.

    4. **Check encoding round-trip.** If the embedded font has no `/ToUnicode` CMap, the viewer draws the right glyphs but extraction returns garbage or nothing. That's why "it looks right in Preview" is not verification. Assert the extracted string, not visual similarity.

    5. **Check geometry.** The word's bbox must sit inside the cell bbox, not cross a ruling line, not overlap a neighboring cell. Catches "text is present but misplaced, clipped, or spilling out."

    6. **Render and compare pixels.** Rasterize the target page at 200–300 DPI, crop the edited cell plus margin, and diff against a baseline render (or at minimum inspect at high zoom). Separately hash per-page rendered images to prove untouched pages didn't change.

    7. **Structural validity.** `qpdf --check`, `mutool`, and a strict `pypdf` open; page count and `MediaBox` unchanged; table ruling lines (vector content) intact.

    8. **Form-field tables:** verify the widget `/V` changed **and** the `/AP` appearance stream was regenerated — a stale `/AP` means the value is updated but the page still paints the old text.

    **Minimal check**

    ```python
    import fitz, pdfplumber

    path = "out.pdf"

    # fresh parse of the saved file
    with pdfplumber.open(path) as pdf:
        page = pdf.pages[target]
        cell = page.extract_tables()[0][row][col]
        assert cell == "EXPECTED", cell
        assert "OLD VALUE" not in (page.extract_text() or "")

    doc = fitz.open(path)
    p = doc[target]
    assert "EXPECTED" in p.get_text()          # real content, not an annot
    assert not list(p.annots() or [])          # no fake overlay edit
    for w in p.get_text("words"):
        if w[4] == "EXPECTED":
            assert fitz.Rect(w[:4]) in cell_rect   # geometry sane

    p.get_pixmap(dpi=300).save("verify.png")   # eyeball / baseline diff
    ```

    **Not verification:** script printed "done", the writer object returned success, the file opens in a viewer, or a text search finds *some* text on the page. Bake checks 1–3 into the pipeline as hard assertions so a bad edit fails the job instead of shipping.
- id: pdf-verify-02
  answer: |
    No — not reliably. `incremental=True` writes a new revision by appending to the existing file, and the original page content stream (and the old cross-reference entries) are left intact in the file. The redaction only changes what the current revision renders; the confidential row's text is still recoverable from the file with a text/strings scan, an older-revision read, or a PDF forensic tool.

    To actually remove it you need a full rewrite to a fresh file with garbage collection, e.g.:

    ```python
    doc.save("clean.pdf", garbage=4, deflate=True)   # new file, no incremental
    ```

    and then replace the original. PyMuPDF's own redaction guidance warns against incremental saves for exactly this reason: redaction removes content from the page's current content stream, but the old stream objects and any orphaned data survive unless the file is rebuilt with garbage collection.

    Practical rule: after applying redactions, save non-incrementally to a new path with `garbage=4` (and consider `clean=True`), verify by extracting text from the result, then delete/overwrite the original. If you must overwrite in place, write to a temp file and atomically rename over it — a plain incremental save to the same path is not a valid redaction.

    Uncertainty caveat: the precise object-level behavior depends on the PyMuPDF version and whether `apply_redactions()` was actually called before saving, but the security conclusion holds for all versions — incremental save is unsafe for redaction.
- id: pdf-verify-03
  answer: |
    **File-safety practices for editing `invoice.pdf`**

    1. **Never edit the original in place.** Copy it first (`invoice.pdf` → `invoice.working.pdf` or a timestamped backup), and keep the original read-only for the duration of the task. If you must modify the original, make a sibling backup first and confirm it exists and is non-zero in size.

    2. **Confirm the file is what you think it is.** Check the path is unambiguous (absolute path, one match), confirm it's a PDF (magic bytes `%PDF-`, not just the extension), and check size/mtime before touching it. If several files match the name, stop and ask rather than guessing.

    3. **Write to a new path, then swap.** Produce output at a temporary/new filename, validate it, then atomically replace the target. Never truncate the destination before the replacement file is verified — a failed conversion should leave the original intact.

    4. **Back up before destructive operations.** Copy, fill, flatten, redact, merge, split, compress — any of these get a backup. For redaction specifically, verify the underlying text is actually removed, not just visually covered.

    5. **Preserve what you're not asked to change.** Keep metadata, page count, page size/orientation, form field values, embedded fonts, signatures, and attachments unless the user explicitly asked to alter them. Don't silently re-encode or re-save through a lossy pipeline.

    6. **Verify the output.** Reopen the result, confirm it parses, confirm page count and the specific changed content, and diff/inspect the fields you touched. A PDF that opens is not proof it's correct.

    7. **Don't leak or exfiltrate.** Invoices contain PII and payment data. Keep processing local, don't upload to third-party services or paste contents into external tools without the user's explicit consent, and don't leave stray temp copies containing sensitive data — clean them up.

    8. **Respect permissions and scope.** Don't `chmod`, move, delete, or rename unrelated files. Don't touch other invoices in the same directory. If the file is read-only or owned by another user, report it instead of forcing it.

    9. **Fail safe and report.** If any step fails, leave the original untouched and say exactly what failed, which file is authoritative, and where the backup lives. Make it obvious to the user which file is the edited one and which is the original.

    **Assumption stated:** since no live user is available, I'm assuming the safe default is "preserve the original, edit a copy, verify, and report" rather than overwriting `invoice.pdf` in place.
- id: pdf-verify-04
  answer: |
    **Assumption:** non-interactive run, so I'm answering from general knowledge about PDF internals and verification, not from any specific script.

    A successful exit and a "done" print only prove the **code path completed** — not that the artifact is valid or that it preserves what was there. Concretely:

    ## 1. "Done" is the script testifying about itself
    The `print("done")` is emitted by your own code at the end of the block you wrote. It means "control reached my last statement." It doesn't read the output file back, parse it, render it, or compare it to anything. It's an assertion of intent, not of result. A script that writes to the wrong path, writes to a temp file, writes then fails to flush/close, or writes an empty/near-empty file will happily print "done."

    ## 2. PDF libraries are deliberately lenient
    Most tooling (pypdf/PyPDF2, pdfrw, pikepdf, PyMuPDF, pdf-lib) is forgiving: it patches offsets, rebuilds or drops the xref, lazily resolves objects, and emits *something* structure-shaped. They will "successfully" save output that:
    - has a broken or dropped xref table / xref stream, or wrong startxref offset;
    - leaves dangling references or orphaned objects;
    - corrupts the page tree (bad `/Kids`, `/Count`, `/Parent`, missing `MediaBox`/`Resources`/`Contents`);
    - double-compresses or drops content streams and filters;
    - loses `/Outlines`, `/Annots`, `/AcroForm` fields, `/Metadata`, `/Names`, tagging/structure;
    - silently drops encryption/permissions, or breaks subset fonts and encodings (→ garbled or tofu text);
    - flattens form fields that must remain fillable, or vice versa.

    None of that raises an exception at write time. Some of it only fails in a specific viewer, and some only fails when a human tries to interact.

    ## 3. Errors are often deferred, not absent
    Lazy object resolution means the bad reference is only touched when something renders. Warnings frequently go to `logging`/`stderr` or are suppressed by flags like `strict=False`, `ignore_errors=True`, or `warnings.filterwarnings("ignore")`. Exit code 0 is compatible with a file full of warnings.

    ## 4. "No errors" ≠ round-trip fidelity
    Read-modify-write is inherently lossy for features the library doesn't model. The absence of a crash says nothing about dropped bookmarks, changed coordinates, reflowed text, altered colorspaces, or re-encoded images that now look wrong.

    ## 5. Correctness is multi-dimensional
    A PDF can be structurally parseable but visually wrong, or visually right but semantically broken (unsearchable text, dead form fields, stripped accessibility tags, invalid PDF/A conformance). One green signal can't cover: file structure, object graph integrity, page tree, content syntax, fonts/encodings, images, annotations/forms, metadata, and visual equivalence.

    ## What actually counts as evidence
    Independent, artifact-level checks:
    - **Strict structural validation:** `qpdf --check`, `mutool clean -d` + re-parse, `pdfcpu validate`, `veraPDF` for PDF/A. These re-read and report structural defects.
    - **Render and diff:** rasterize before/after pages (PyMuPDF/`pdftoppm`) and compare pixels or perceptual hashes per page; flag any page with a diff above threshold.
    - **Text extraction diff:** compare extracted text (and page count, word count) before vs after; catches garbled fonts and lost content.
    - **Object/semantic inventory:** counts and spot-checks of pages, annotations, bookmarks, form fields, and metadata.
    - **Cross-viewer check:** open in at least two independent engines (PDFium/Chrome, Poppler, Adobe, macOS Preview) — corruption often shows up in one and not another.
    - **Sanity checks:** output exists, non-zero size, expected page count, header `%PDF-` and trailing `%%EOF`, and that it's the file you meant to write.

    **Bottom line:** "ran without errors, printed done" is a statement about your program's control flow. PDF correctness is a property of the bytes on disk and how they render and behave — it has to be measured by re-parsing, rendering, and diffing the output, ideally with a strict validator plus a visual/text comparison against the original.

    If you can't validate the output independently, the honest status is "unverified," not "correct."
```
