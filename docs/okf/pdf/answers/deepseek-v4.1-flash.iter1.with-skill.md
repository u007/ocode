# deepseek-v4.1-flash — pdf answers WITH SKILL (validation of the re-derived skill)

Model: `opencode-go/deepseek-v4.1-flash` (provider-stripped `deepseek-v4.1-flash`),
re-run 2026-10-21 after opencode-go updated the model behind this id
(`model_version: "4.1-2026-10-21"`). This is the with-skill run for this id (any earlier one validated the previous model);
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
    ## What the PDF actually contains

    A PDF has no table. It has no row object, no cell object, no columns, and no reading order. A "table" in a PDF is a visual coincidence produced by the content stream of a page:

    - **Text** is a sequence of show-text operators (`Tj`, `TJ`, `'`, `"`) inside `BT`/`ET`. Each string is placed by a text matrix at absolute page coordinates. Words are frequently split into individual glyph runs, spaces are often encoded as `TJ` kerning offsets rather than space characters, and the order in the stream need not match visual order (it can be column-major, or back-to-front).
    - **The grid** is vector line art: `re`/`m`/`l` paths painted with `S` (stroke) or `f`/`f*` (fill).
    - **Row shading / zebra striping / header fills** are filled rectangles (`re ... f`), possibly with transparency groups.
    - **Cell borders** are usually one path per rule, sometimes one path for the whole grid. A horizontal rule between the deleted row and the row below is *shared* geometry, not two borders.
    - **Fonts** are often subset, sometimes without a usable `ToUnicode` map, so extracted text can be garbage even though it renders fine. Font metrics, not source semantics, define where glyphs sit.

    So the "third row" is an inference: a cluster of glyphs and rules between two horizontal y-coordinates. Tools like `find_tables` do exactly this — heuristic detection from rule positions and text bounding boxes. They can be wrong with merged cells, wrapped text, missing rules, or whitespace-separated columns.

    Two common special cases: the table may be a **single embedded image** (scanned), in which case there is no text and no vectors — deletion means editing pixels or rebuilding; or the PDF may be **tagged**, with `/Table`/`TR`/`TD` structure elements in the structure tree. The tags describe intent but are separate from the painted content, so removing the paint does not remove the tags, and vice versa.

    ## What that implies for the edit

    1. **There is nothing to delete, only to erase and re-lay-out.** PDF has no reflow. Removing a row's ink leaves a hole unless you move everything below it up by exactly one row height and redraw the grid and shading. "Delete row 3" almost always means "delete row 3 and close the gap" — state that assumption when it is ambiguous.

    2. **Structure must be derived first.** Get the row bands from the horizontal rule y-positions (or from clustered text baselines), not from paragraph intuition. Confirm the third data row vs. the header, and check for wrapped or multi-line cells that make a row taller than one line.

    3. **Removal must be destructive at the content-stream level — true redaction, not cover-up.** A white rectangle, a filled box, or drawing the new table on top leaves the original glyphs and rules in the file; they remain selectable, searchable, copyable, and extractable. Use `page.add_redact_annot(...)` (with quad points from the actual glyph bboxes) and `page.apply_redactions()`. Then save with `garbage=4, deflate=True` to drop the orphaned objects.

    4. **Know what `apply_redactions` touches.** Defaults are text removal by bbox overlap, `images=PDF_REDACT_IMAGE_PIXELS` (blanks overlapping pixels), and `graphics=PDF_REDACT_LINE_ART_REMOVE_IF_COVERED` (removes shapes lying *entirely* inside the rect; partly covered shapes survive whole). So a rect over a whole row deletes that row's shading and rules, but a rect over one cell also deletes that cell's fill unless you pass `graphics=0, images=0`. Do **not** use `REMOVE_IF_TOUCHED` on a band that clips vertical rules running into rows that stay — it deletes the entire rule.

    5. **Cover the whole moving region, then redraw.** The safe procedure: redact the deleted row *plus* every row and line below that will shift (or the whole table), apply, then redraw the remaining rows one row height higher — fills first, then text, then rules. Recompute any totals from extracted data rather than shifting a stale number.

    6. **Never fake the shift with clipping.** `show_pdf_page(rect, src, pno, clip=...)` embeds the entire source page as a Form XObject; the clip only controls visibility. Stamping from an unredacted copy, or rebuilding a page from clipped bands that skip the row, leaves the deleted row recoverable in the bytes, and `get_text()` won't even show it. If you stamp, the stamp source must itself have the row redacted and applied.

    7. **Handle the non-text furniture.** Cell images/logos, AcroForm widgets, and annotations inside the deleted row are not removed by text redaction; they must be handled explicitly, or the "deleted" row leaves a floating logo or a live form field.

    8. **Verify by both text and render.** Confirm the deleted row's strings are absent from `get_text()` of the output *and* render the page to an image to check that the grid closes cleanly, no leftover shading edge remains, no vertical rule is truncated, and neighbouring cells are intact. The text check alone cannot detect clipped-away content.

    In short: the PDF stores painted glyphs, lines, and rectangles at absolute coordinates, not rows. The edit is a geometry problem — detect the row's band, erase its ink destructively, shift and redraw everything below, fix the total, and verify visually as well as textually.
- id: pdf-model-02
  answer: |
    ## Why a white box is not an edit

    Painting a white rectangle (and then writing the new text on top) only changes **what the renderer draws**. It does not change the document's content:

    - **The old text is still there.** The original glyphs remain in the page's content stream and in the text layer. `page.get_text()`, copy/paste, search, screen readers, and indexing all still return the "deleted" words.
    - **The old text can come back.** Change the background colour, print with a different colour profile, select-all, or open the file in a viewer that renders layers differently, and the covered text shows through or is selectable.
    - **It is cosmetic layering, not deletion.** You have added a filled path over existing content. The bytes still contain the string, so the edit is trivially recoverable with any text extractor.
    - **It can be removed.** Anyone can delete the white rectangle object and the original text is intact again.
    - **Text order/accessibility is wrong.** Assistive tech and the content stream still read the old text; the new text may be in a different z-order or position than its logical reading order.

    The same applies to the related shortcuts: clipping with `show_pdf_page(clip=...)`, cropping the page (`set_cropbox`), or stamping a "clean" band from an unredacted copy. Those hide content, they do not remove it — the bytes remain and `get_text()` may not even report it, which makes the file look clean while the data is still recoverable.

    ## What to use instead

    **Real redaction**, i.e. removal of the glyphs from the content stream, followed by re-drawing the replacement content. In PyMuPDF:

    ```python
    import fitz

    doc = fitz.open("in.pdf")
    page = doc[0]

    # 1. Mark the exact area to remove (tight to the old glyphs)
    page.add_redact_annot(old_text_rect)

    # 2. Actually remove it
    page.apply_redactions(
        images=fitz.PDF_REDACT_IMAGE_PIXELS,   # or NONE if the image must survive
        graphics=fitz.PDF_REDACT_LINE_ART_REMOVE_IF_COVERED,  # or NONE to keep rules/shading
        text=fitz.PDF_REDACT_TEXT_REMOVE,
    )

    # 3. Draw the new text afterwards, matching font/size/colour
    page.insert_text(new_origin, "new text", fontname="helv", fontsize=10, color=(0, 0, 0))

    doc.save("out.pdf", garbage=4, deflate=True)
    ```

    Key points:

    - **Redaction removes characters whose bbox overlaps the redaction rectangle.** `apply_redactions()` defaults are `text=PDF_REDACT_TEXT_REMOVE`, `graphics=PDF_REDACT_LINE_ART_REMOVE_IF_COVERED` (deletes rules/shading lying entirely inside the rect), and `images=PDF_REDACT_IMAGE_PIXELS` (blanks overlapping image pixels). Pass `graphics=0, images=0` when you are replacing a single cell and must preserve the grid and shading.
    - **Keep the rectangle tight** to the glyphs and inside the cell; do not pad it outward "to be safe" — that eats borders and neighbouring content.
    - **Re-draw after applying**, never before: fills/rectangles first, then text, then rules, so the new content is not painted over.
    - **Save with `garbage=4, deflate=True`** so the removed objects and orphaned streams are actually dropped from the file.
    - **Verify** by extracting the output: `assert "old text" not in doc_out[0].get_text()`. Note this check cannot see content that was clipped or stamped away, which is exactly why a stamped/clipped approach is not proof of removal — only a redacted source is.
    - For rows/tables, redact the deleted row *and* everything that will move, apply, then redraw (or stamp from a copy in which the deleted row was also redacted and applied) one row higher; recompute any totals from the data.

    In short: **hiding ≠ editing**. Anything that only covers, clips, crops, or overlays leaves the original text in the file. Use redaction (content-stream removal) plus a redraw, then verify with text extraction and a garbage-collected save.

    ---

    **Recap**

    ASKED — Explain why a white rectangle over PDF text is not an edit, and what to use instead.

    WORKED — No tools run; answered from knowledge. Explained that overlay/clip/crop leave original glyphs in the content stream (extractable, searchable, recoverable), and specified PyMuPDF redaction (`add_redact_annot` + `apply_redactions`) followed by redraw, correct graphics/images flags, tight rects, and `garbage=4, deflate=True` save with text-extraction verification.

    FOUND — The failure mode generalizes: `show_pdf_page(clip=...)`, cropboxes, and white fills all hide bytes rather than remove them, and `get_text()` may not reveal clipped content, so "it looks clean" is not evidence.

    DECIDED — Real removal means content-stream redaction; a stamped page is only clean if its stamp source was itself redacted.

    BOTTOM LINE: A white box is a visual patch over intact text; redaction is the edit.

    NEXT — None — the question was answered in full and required no artifacts.
- id: pdf-model-03
  answer: |
    Short answer: the visible characters `22.50` are almost never stored as those literal bytes in the stream, and even when they are, the surrounding PDF syntax, text operators, font encoding, and layout metrics still have to agree with the replacement.

    Here is why a naive byte-level search-and-replace on the raw stream fails or corrupts the page.

    ## 1. The stream bytes are not the text bytes

    - **Compression.** Content streams are normally `FlateDecode` (often with `LZWDecode`, `ASCII85Decode`, or a `/Predictor`). The literal `22.50` does not appear in the file at all until the filters are decoded. A raw search either finds nothing or matches random compressed bytes and corrupts the stream.
    - **Object streams / xref streams.** The content may live inside a compressed object stream (`/Type /ObjStm`), so even uncompressed page streams aren't directly greppable.
    - **Multiple filters, multiple streams.** `/Contents` can be an array of streams; the target may straddle them or be in only one. Searching one stream misses it.
    - **Encryption.** With standard security, strings and streams are encrypted per object (RC4/AES). The plaintext bytes aren't present; a byte replace writes ciphertext garbage and breaks decryption/validation.
    - **Not in the page at all.** The value may be in an AcroForm field `/V` plus a separate appearance stream, in an annotation appearance, or the "text" may be vector outlines or an image. Editing page content changes nothing.

    ## 2. Even decoded, the string usually isn't contiguous

    - **`TJ` arrays and glyph positioning.** Generators frequently emit `[(22.5) -15 (0)] TJ`, or each glyph with its own `Tj`/`Td`, or use the `'` and `"` operators. The characters are split by kerning numbers and positioning operators, so `22.50` is not a single searchable token.
    - **Escaping.** String literals escape `(`, `)`, and `\`; octal escapes (`\062`) and hex strings (`<32322e3530>`) are legal. The visible text may be encoded in any of these forms.
    - **Font encoding.** With a simple font using `/Differences`, or a Type0/CID font, the bytes in the string are character codes, glyph indices, or CIDs — not ASCII. `22.50` on screen may be `\x12\x12\x0e\x13\x0e` (or arbitrary two-byte CIDs). Replacing ASCII bytes writes the wrong glyphs or nothing; the new characters must be mapped through the font's encoding/CMap, and the glyphs must exist in the (often subset) embedded font.
    - **ToUnicode is separate.** The `/ToUnicode` CMap, `/Info`, bookmarks, outlines, and metadata can contain the old string too; replacing only the content stream leaves an inconsistent document (or extraction still shows the old value).

    ## 3. The same bytes occur in places you must not touch

    - `22.50` can be a **number operand**, not text: a `cm`/`Tm` matrix entry (`22.50 0 0 22.50 ... cm`), a `Td`/`TD` offset, a line width, a colour component. Replacing it changes geometry, not content.
    - The same string can appear **many times** — hidden OCR layers, fake-bold/shadow double draws, repeated headers, shared Form XObjects. Editing a shared XObject changes every placement; editing one occurrence leaves the other.
    - The match can be a **substring** of a longer number (`122.50`, `22.500`), silently changing something else.

    ## 4. Syntactic and structural breakage

    - **Length and xref.** Any change in length requires updating the stream's `/Length` and rewriting the cross-reference table/stream. Patching file bytes in place without a real PDF writer leaves stale offsets; a parser then misreads objects. (Your example is same-length, which avoids this specific problem but not the others — and it's irrelevant if the stream is compressed.)
    - **Tokenisation.** Inserting an unescaped `(`, `)`, or `\`, splitting a string delimiter, matching across the boundary between a string and the next operator, or changing `BT`/`ET` structure produces an invalid content stream. In a hex string the digits must stay valid hex with an even count.
    - **Digital signatures** are invalidated by any byte change in the signed range.

    ## 5. It can be syntactically valid and still visually wrong

    - Replacing digits changes **advance widths**. Right-aligned or centred numbers shift; columns misalign; text overlaps the next cell or leaves a gap. `TJ` kerning adjustments and `Tm`/`Td` positions were computed for the old glyphs. You must recompute positioning (or rebuild the line) or the page looks broken even though it parses.
    - If the font is a subset that lacks the new glyph (or the new string uses characters outside the subset), the glyph is missing or substituted.

    ## Practical consequence

    Do the edit at the **operator level**, not the byte level: decode the filters, parse the content stream into operators (`pikepdf.parse_content_stream` / `ContentStreamProcessor`, or PyMuPDF's text/redaction APIs), find the `Tj`/`TJ`/`'`/`"` operand, decode it through the font's encoding/ToUnicode, re-encode and escape the replacement, adjust the `TJ` numbers or text matrix if widths change, write the stream back through the library (so `/Length` and xref are rebuilt), then verify by extracting text (`get_text()`) and rendering the page.

    A plain byte replace only has a chance when the content stream is uncompressed, unencrypted, the text is a single plain literal in the default encoding, the replacement has identical length and metrics, and the match is unique — conditions that are rare in real documents.
- id: pdf-model-04
  answer: |
    ## Two different frames

    Raw PDF user space (where `/MediaBox`, `/CropBox`, content streams and annotation `/Rect` live):

    - origin at the **bottom-left** of the page,
    - **y increases upward**,
    - unit = 1 point = 1/72 inch.

    PyMuPDF/MuPDF page space (what `page.rect`, `get_text`, `search_for`, `draw_*`, `get_pixmap` speak):

    - origin at the **top-left of the visible page** (i.e. the CropBox, as displayed),
    - **y increases downward**,
    - same point units.

    For an **unrotated** page whose raw CropBox is `[cx0 cy0 cx1 cy1]`:

    ```text
    W = cx1 - cx0        H = cy1 - cy0
    page.rect == Rect(0, 0, W, H)

    x_mupdf = x_pdf - cx0
    y_mupdf = cy1 - y_pdf          # flip about the CropBox's TOP edge

    x_pdf = x_mupdf + cx0
    y_pdf = cy1 - y_mupdf
    ```

    Example: CropBox = MediaBox = `[0 0 612 792]`. PDF bottom-left `(0,0)` → PyMuPDF `(0,792)`; PDF top-left `(0,792)` → PyMuPDF `(0,0)`. Note the flip is about `cy1`, not about `0` or `page.rect.height` — those only coincide when the CropBox starts at the origin.

    `page.transformation_matrix` is exactly this affine map (it folds in the CropBox offset **and** `/Rotate`); its inverse goes back:

    ```python
    p_py  = p_pdf * page.transformation_matrix
    p_pdf = p_py  * ~page.transformation_matrix
    ```

    For 90° multiples the image of an axis-aligned `Rect` is still axis-aligned, so transforming rects this way is exact.

    ## Reading a word and drawing it

    `page.get_text("words")` returns tuples:

    ```text
    (x0, y0, x1, y1, word, block_no, line_no, word_no)
    ```

    with `(x0,y0)` = top-left and `(x1,y1)` = bottom-right **in PyMuPDF page space**. So the bbox is already in the same frame as the page drawing methods:

    ```python
    w = page.get_text("words")[0]
    r = fitz.Rect(w[:4])
    page.draw_rect(r, color=(1, 0, 0), width=0.5)   # no flipping needed
    ```

    Do not apply `page.rect.height - y` "to convert to PDF" — that only works for an unrotated, zero-origin CropBox, and it produces page-relative coordinates, not real PDF coordinates if the CropBox is offset.

    Two placement details that are not coordinate-system issues: `insert_text` takes the **baseline origin** of the first glyph (bottom-left), not the bbox top-left, and bbox height is not the font size. Use `rawdict` span `origin`/`ascender`/`descender` if you need exact text placement.

    ## `/Rotate`

    `page.rotation` is 0/90/180/270 (clockwise display rotation; inherited `/Rotate` is resolved for you). MuPDF applies it when it defines page space:

    - `page.rect` **swaps width and height** for 90 and 270 (A4 portrait 595×842 becomes 842×595). The origin stays at the top-left of the *rotated* page.
    - Word boxes, search hits, pixmaps and the drawing methods are all in that rotated frame. The numbers for a word are therefore **not** its PDF-space numbers; they are rotated and re-origined.
    - The PDF content itself never moves: `/Rotate` is display metadata. Raw content streams, `doc.xref_stream`, and external parsers still see unrotated PDF coordinates. So when mixing PyMuPDF output with raw PDF values, convert with `page.transformation_matrix` (full map) or, for rotation alone, `page.rotation_matrix` / `page.derotation_matrix` (rotated frame → unrotated frame).
    - APIs documented as using **unrotated** coordinates ignore `/Rotate` — annotation rectangles are the classic case; also the page box attributes (`page.cropbox`, `page.mediabox`) are unrotated values, not the `page.rect` frame. For those, multiply a word rect by `page.derotation_matrix` (and account for the CropBox offset) before use, otherwise the annot/box lands in the wrong place on a rotated page. PyMuPDF has changed the exact presentation of box attributes across versions; the reliable bridge is the matrix, not eyeballing `page.cropbox` vs `page.rect`.

    ## CropBox offset from MediaBox

    - The CropBox, not the MediaBox, defines the visible page. `page.rect` is always `(0,0,crop_width,crop_height)`; the CropBox's lower-left in PDF space becomes the page origin (top-left in PyMuPDF terms).
    - If MediaBox is `[0 0 612 792]` and CropBox is `[50 100 562 742]`, then `page.rect` is `(0,0,512,642)`. PDF `(50,100)` → PyMuPDF `(0,642)`, and a word at PyMuPDF `(10,20)` is at PDF `(60, 722)`.
    - The MediaBox area outside the CropBox is still in the file and still in the content stream; it is merely clipped from display/print. Drawing at coordinates outside `page.rect` "works" but will not be visible in the rendered page.
    - Alternatively, `page.mediabox`/`page.cropbox` can be transformed into page space with `page.transformation_matrix`, or the offset can be derived from `page.cropbox` and applied as the x-shift and y-flip above.

    **Bottom line:** word bboxes and PyMuPDF drawing calls share the same top-left/y-down frame (CropBox-relative, `/Rotate` applied), so draw at `Rect(w[:4])` directly. Raw PDF coordinates are bottom-left/y-up in unrotated MediaBox space: convert with `page.transformation_matrix`, and remember that `/Rotate` changes only MuPDF's frame while a CropBox offset changes where the page origin sits.
- id: pdf-locate-01
  answer: |
    A PDF has no table object. A "table" is only inferred from vector ruling lines and/or text alignment, so every library does geometric reconstruction and none is authoritative. The reliable pattern is: detect the table grid → read row/cell rectangles → extract text per rectangle yourself.

    ## Primary recipe: PyMuPDF (`page.find_tables`, 1.23+)

    `find_tables()` returns a `TableFinder`; its `.tables` are `Table` objects with a full bbox hierarchy: table → rows → cells.

    ```python
    import pymupdf as fitz   # or: import fitz

    doc  = fitz.open("in.pdf")
    page = doc[0]

    finder = page.find_tables(
        strategy="lines_strict",      # "lines" | "lines_strict" | "text" | "explicit"
        snap_tolerance=3,
        join_tolerance=3,
        intersection_tolerance=3,
        edge_min_length=3,
    )
    # or set the axes independently:
    # page.find_tables(vertical_strategy="text", horizontal_strategy="lines",
    #                  min_words_vertical=3)

    for ti, tab in enumerate(finder.tables):
        print("table", ti, "bbox", tuple(tab.bbox),
              "rows", tab.row_count, "cols", tab.col_count)

        # table header (may be outside tab.bbox when header.external is True)
        hdr = tab.header
        print("header bbox", tuple(hdr.bbox), "external", hdr.external, hdr.names)

        for ri, row in enumerate(tab.rows):
            print("  row", ri, "bbox", tuple(row.bbox))
            for ci, cell in enumerate(row.cells):
                if cell is None:          # gap / slot covered by a merged cell
                    continue
                r = fitz.Rect(cell)
                text = page.get_textbox(r)          # text clipped to that rectangle
                print("    cell", ci, tuple(r), repr(text.strip()))

        # convenience: structured text (uses the same cell rects)
        print(tab.extract())
        print(tab.to_markdown())
    ```

    What the objects expose:

    | Object | Geometry |
    |---|---|
    | `Table.bbox` | whole table, `fitz.Rect` |
    | `Table.rows` | list of `TableRow` |
    | `TableRow.bbox` | row rectangle |
    | `TableRow.cells` | list of cell `fitz.Rect` (or `None` for slots absent/covered) |
    | `Table.cells` | flat row-wise list of all cell rects |
    | `Table.header` | `TableHeader` with `.bbox`, `.cells`, `.names`, `.external` |
    | `Table.extract()` | list of rows of cell strings, aligned to `rows`/`cells` |

    Cells are per-cell rectangles, not just text extents — that is the part people miss. Spanned/merged cells are reported as one union rectangle; the covered slots come back as `None`, so index carefully rather than assuming `len(row.cells) == col_count`.

    ## Equivalent: pdfplumber

    Same idea, cell objects carry `.bbox`:

    ```python
    import pdfplumber

    with pdfplumber.open("in.pdf") as pdf:
        page = pdf.pages[0]
        for table in page.find_tables(
            table_settings={
                "vertical_strategy": "lines",
                "horizontal_strategy": "lines",
                "snap_tolerance": 3,
                "join_tolerance": 3,
            }
        ):
            print("table bbox", table.bbox)
            for row in table.rows:                 # Row
                print(" row bbox", row.bbox)
                for cell in row.cells:             # Cell
                    print("  cell bbox", cell.bbox, cell.extract())
            print(table.extract())
    ```

    `table.bbox` is `(x0, top, x1, bottom)`; `row.bbox` and `cell.bbox` are the same tuple shape. `cell.extract()` crops the text to that rectangle.

    ## Ruling-line tools: Camelot

    Lattice mode uses the drawn grid; cell boxes are exposed on the table object:

    ```python
    import camelot

    t = camelot.read_pdf("in.pdf", pages="1", flavor="lattice")[0]
    print(t._bbox)       # table bbox
    print(t.cells)       # per-cell [x1, y1, x2, y2]
    print(t.rows)        # row boxes
    print(t.df)          # text grid
    ```

    `flavor="stream"` falls back to whitespace alignment when there are no rules. For scanned pages every text-based method fails: run OCR (Tesseract/PaddleOCR) to get word boxes, then cluster words into rows/columns, or use a layout model such as img2table or Docling and map its boxes back to page coordinates.

    ## Practical caveats

    - **Strategy matters more than the library.** `lines`/`lines_strict` needs visible rules; `text` needs consistent column alignment. Mixed tables often need `vertical_strategy="text", horizontal_strategy="lines"` plus `min_words_vertical`.
    - **No rules at all** → text strategy, or detect columns from the x-histogram of word boxes and rows from the y-histogram.
    - **Merged cells** break naive `row.cells[col]` indexing; check for `None` and use `table.cells`/`extract()` shape as ground truth.
    - **Rotation**: normalize `page.rotation` first, otherwise cell rectangles are in unrotated page space while what you see is rotated.
    - **Coordinates**: PyMuPDF and pdfplumber use PDF points with y increasing downward from the top-left of the page. Camelot mixes in a different convention, so verify before cropping or redacting.
    - **Always verify**: re-extract text with the returned rects and compare against `tab.extract()`; a grid that looks right in a render can still have off-by-one column boundaries.
- id: pdf-locate-02
  answer: |
    `search_for()` returns ink-tight quads for the matched glyphs — one per line of the match — not a cell and not a row.

    **Why that's not enough**

    - The quads hug the glyphs of "Widget C" only. They exclude the cell's padding, the cell borders and fill, the horizontal rule under the row, and every other cell in that row (Qty, Price, …). Redacting those quads deletes the words "Widget C" and leaves the rest of the row: other cells' text stays in the text layer, the row's shading and rules stay drawn, and the row still occupies its slot.
    - A table row is a geometric band defined by the grid (two horizontal rules, or the midline between adjacent baselines) and by the table's left/right edges — none of which `search_for` knows about. A tall cell, a wrapped cell, or a cell with padded whitespace produces a glyph quad much smaller (and possibly much shorter) than the row.
    - It is also ambiguous. The same string can appear in the header, in another row, or in a note; `search_for` gives you a list of hits with no row identity. Repeated or wrapped text yields several quads, and the default `hit_max` cap can silently truncate long hit lists.
    - So the rect you would feed `apply_redactions` is wrong in width, wrong in height, misses the sibling cells, and may hit the wrong row.

    **Getting the real row region**

    1. **Preferred: table detection.**
       ```python
       tabs = page.find_tables()
       tab  = tabs[0]                      # pick the table whose bbox contains the hit
       data = tab.extract()                # text grid; find the row index containing "Widget C"
       r    = tab.rows[idx].bbox           # full-width, full-height row rect
       ```
       `TableRow.bbox` is the row band clipped to the table — width spans the table, height spans the grid. That is the region to redact (plus everything below it that will move, or the whole table).

    2. **Fallback without `find_tables`: build it from geometry.**
       - Horizontal extent: take `page.get_text("words")`, keep every word whose bbox overlaps the hit quad vertically (same row band), plus the row's other cells, then widen to the table edges — the outermost of `page.get_drawings()` vertical rules, or the header row's word extents, or the enclosing table bbox.
       - Vertical extent: use the nearest horizontal rule above and below from `get_drawings()`. If the grid is drawn as fills rather than rules, use the midpoints between adjacent row baselines: `y0 = (prev_baseline + baseline)/2`, `y1 = (baseline + next_baseline)/2`, and union in the row's fill rect if one exists.
       - Union everything into one rect, then **assert** it contains all of the row's words and none of the neighbouring rows' words (no word bbox may straddle `y0` or `y1`).

    3. **Then redact properly.** Redact that full band (and the moved region below, or the whole table), with the redaction defaults, then redraw/stamp the surviving rows and recompute totals — because the defaults also drop shading and any rule lying entirely inside the band.

    In short: `search_for` locates the string, `find_tables` (or word-clustering plus rule/baseline geometry) locates the row. Delete by row band, never by glyph quad.
- id: pdf-locate-03
  answer: |
    Read the style from the text-extraction dict, not from rendered pixels. Two levels are available: span-level (fast, one entry per run of same-style text) and character-level (exact origins when you must reproduce positions).

    ## Span level — `page.get_text("dict")`

    ```python
    import pymupdf

    page = doc[0]
    d = page.get_text("dict", sort=True)

    for block in d["blocks"]:
        if block["type"] != 0:      # 0 = text, 1 = image
            continue
        for line in block["lines"]:
            for span in line["spans"]:
                font   = span["font"]        # e.g. "ABCDEF+Helvetica-Bold"
                size   = span["size"]        # pt
                color  = span["color"]       # sRGB packed int, 0xRRGGBB
                r, g, b = pymupdf.sRGB_to_pdf(color)   # -> floats 0..1
                ox, oy = span["origin"]      # BASELINE origin of the first char
                asc, desc = span["ascender"], span["descender"]  # fraction of size
                bbox   = span["bbox"]
                flags  = span["flags"]       # bit0 superscript, bit1 italic,
                                             # bit2 serif, bit3 monospaced, bit4 bold
                direction = line["dir"]      # (cos, sin) of the baseline
    ```

    - **Baseline**: `span["origin"]` is the exact baseline point. Derived check: `baseline_y ≈ bbox.y0 + ascender*size` or `bbox.y1 + descender*size` (`descender` is negative). The origin is authoritative; the bbox formulas are approximations for fonts with odd metrics.
    - **Size**: already in points; it is the *effective* size (text matrix applied), not necessarily the raw `/Tf` operand.
    - **Colour**: packed sRGB. Text drawn in a non-sRGB space or with transparency may not round-trip exactly; for that use `get_texttrace()` below.
    - **Font**: the name can carry a subset prefix (`ABCDEF+`). The name alone is not enough to re-embed it.

    ## Character level — `page.get_text("rawdict")`

    Same structure, but each span has `span["chars"]`, and every char has its own `origin`, `bbox`, and `c`. Use this when you need per-glyph positions (kerning, justification, mixed advances):

    ```python
    raw = page.get_text("rawdict")
    for b in raw["blocks"]:
        for l in b.get("lines", []):
            for s in l["spans"]:
                for ch in s["chars"]:
                    ch["origin"]   # baseline point of this character
                    ch["bbox"]
    ```

    `page.get_text("words")` gives no style; `"html"`/`"xml"` also carry font-family/size in their markup.

    ## Drawing-order, transparency and stroke — `page.get_texttrace()`

    This is the most faithful record of what was actually painted. Colour here is a float tuple, and stroke/fill mode and opacity are reported:

    ```python
    for span in page.get_texttrace():
        span["font"], span["size"]
        span["color"]              # (r, g, b) floats
        span["type"]               # 0 = fill, 1 = stroke, 3 = invisible
        span["opacity"]            # alpha
        span["ascender"], span["descender"]
        span["dir"]                # writing direction
        for ch in span["chars"]:
            ch["origin"]
    ```

    Use `get_texttrace()` when `span["color"]` looks wrong, when the text is stroked or semi-transparent, or when several identical strings overlap.

    ## Resolving the real embedded font

    ```python
    for xref, ext, ftype, basefont, name, enc, ref in page.get_fonts(full=True):
        pass  # xref -> embedded font program

    basefont, ext, ftype, buf = doc.extract_font(xref)
    font = pymupdf.Font(fontbuffer=buf)   # true metrics + glyph widths,
                                          # independent of the subset name
    ```

    Prefer the extracted buffer over the reported name: subset names like `ABCDEF+Helvetica-Bold` often match nothing on the system, and `insert_text(fontname="Helvetica-Bold")` silently substitutes.

    ## Redrawing with the captured style

    Save the origin, size, colour and font buffer **before** you redact; redaction destroys the span you are reading.

    ```python
    page.insert_font(fontname="F0", fontbuffer=buf)
    page.insert_text(
        (ox, oy),                     # baseline origin, y grows downward
        new_text,
        fontname="F0",
        fontsize=size,
        color=pymupdf.sRGB_to_pdf(color),
        render_mode=0,                # 0 fill, 1 stroke, 2 fill+stroke
    )
    ```

    For rotated text, `insert_text(..., rotate=0|90|180|270)` only covers right angles; for arbitrary `dir` use `TextWriter`, whose `append()` point is also the baseline start:

    ```python
    tw = pymupdf.TextWriter(page.rect)
    tw.append((ox, oy), new_text, font=font, fontsize=size)
    tw.write_text(page, color=pymupdf.sRGB_to_pdf(color))
    ```

    Check the fit first with `font.text_length(new_text, fontsize=size)` against the cell width.

    ## Two traps

    1. **Colour encoding differs by API.** `get_text("dict")` gives a packed int; `get_texttrace()` gives float triples; `insert_text(color=...)` wants a sequence of 0..1 floats. Use `pymupdf.sRGB_to_pdf()` for the conversion.
    2. **The span colour is not styled text.** If the page uses a shading/pattern or a Type3 font, the extracted colour is a best effort. Trust `get_texttrace()` for anything unusual.

    When the goal is replacing one cell's text in place, keep the redaction rect tight to the glyph bboxes and call `page.apply_redactions(graphics=0, images=0)` so the cell's borders and fill survive — then redraw at the saved `origin`.

    ---

    ASKED — How to read font, size, colour, baseline of existing text in PyMuPDF.

    BUILT — No artefact; an API answer. Span-level (`get_text("dict")`), char-level (`rawdict`), paint-level (`get_texttrace`), plus font extraction via `get_fonts`/`extract_font` and redraw via `insert_text`/`TextWriter`.

    FOUND — All four attributes live on spans; `origin` is the exact baseline while bbox+ascender/descender is only an approximation. Colour is a packed sRGB int in dict output but float triples in texttrace — mixing them is the usual failure.

    DECIDED — Recommend `get_texttrace()` whenever colour, stroke or opacity looks off; recommend the extracted font buffer over the reported font name so redrawn text is not substituted.

    BOTTOM LINE: `span["origin"]` gives the baseline, `size` the points, `color` the packed sRGB, `font` the (possibly subset-named) face — capture all four before redacting, and re-embed via `doc.extract_font()`.

    NEXT — None — the question is informational and needs no follow-up work.
- id: pdf-locate-04
  answer: |
    ## Where the numbers come from

    Two different sources, and you need both:

    **1. Column boundaries → `page.find_tables()`**

    `Table.rows[i].cells` gives one cell `Rect` per cell (per column, `col_count` cells per row; `None` for merged/missing cells). Each cell's `x0`/`x1` *is* the column boundary — the vertical rule positions. So:

    ```python
    tabs = page.find_tables()
    tab  = tabs[0]

    # column boundaries: collect and dedupe the cell edges
    xs0 = sorted({round(c.x0, 1) for row in tab.rows for c in row.cells if c})
    xs1 = sorted({round(c.x1, 1) for row in tab.rows for c in row.cells if c})
    # boundaries = xs0 + the last xs1  (left edges, plus the table's right edge)
    ```

    `tab.header.names`, `tab.col_count`, and `tab.extract()` tell you which column is which and give you the cell text to classify numeric vs label columns.

    **2. The actual right edge of a right-aligned number → the text layer, not the cell**

    The cell rect tells you the column's box; it does **not** tell you where the digits stop (aligned numbers usually sit a few points inside the right rule). Get that from the glyphs:

    ```python
    d = page.get_text("dict", clip=cell_rect)   # or "words", or "rawdict"
    # rightmost span bbox in the cell:
    x_right = max(s["bbox"][2] for b in d["blocks"] for l in b.get("lines", [])
                                 for s in l["spans"])
    ```

    or with words:

    ```python
    words = page.get_text("words", clip=cell_rect)   # x0,y0,x1,y1,word,...
    x_right = max(w[2] for w in words)
    ```

    Then take a stable value across the column — the *mode/median* of the per-row right edges — because one row (e.g. a currency symbol or a different font) can be off by a point. Same for the baseline (`bbox[3]`) if you're matching vertical placement.

    ## Why not just use the cell edge

    - The right rule is a *graphic*, not the text end; right-aligned numbers are inset by the cell's padding, so anchoring new text to `cell.x1` shifts every value right and collides with the rule.
    - Cells can contain padding, merged spans, or superscripts; `get_text("words", clip=...)` clipped to the cell gives you what's actually drawn where.
    - For style matching (font, size, colour) read `get_text("dict")` spans on the row/header line and copy `font`, `size`, `color`.

    ## Rule of thumb

    | You need | Get it from |
    |---|---|
    | Column x-boundaries, row y-boundaries | `find_tables()` → `rows[i].cells` rects |
    | Where a right-aligned value ends | rightmost span/word `x1` from `get_text("dict"/"words", clip=cell_rect)`, aggregated across rows |
    | Baseline / row pitch | span `bbox[3]`, or `row.bbox` heights |
    | Header style for a new column | spans on the header row (`color`, `font`, `size`, fill) |

    Then place new text with its right edge on that measured `x_right` (`insert_text` at `(x_right - fitz.get_text_length(s, fontname, fontsize), baseline)`, or a `TextWriter` with right alignment).

    So: **structure from `find_tables().rows[].cells`; exact alignment from the text spans/words inside those cells — never from the cell border or `page.rect` margins.**
- id: pdf-rowdel-01
  answer: |
    ## Assumptions

    Digital (real text) PDF, one ruled+shaded table on one page, the Total row sits below the data rows on the same page, and the rows are regular enough that one row height is the shift for everything below. If the table is a scanned image, skip to the note at the end — text redaction will not remove it.

    ## Procedure

    **1. Measure before touching anything.**
    `page.find_tables()` → pick the table → `tab.rows` (each has `.bbox`), `tab.extract()` for the data. Record:

    - `row_bbox` of the row to delete, and `h = row_bbox.height`
    - the table's full bbox, column x-boundaries, and the Total row's bbox
    - the header/body typography from `page.get_text("dict")` spans: font, size, colour (header is usually white on a dark fill), so anything you redraw matches
    - the numeric Total value and how it is formatted (decimals, separators, currency)

    **2. Decide the rebuild strategy.**
    - **A. Rebuild from data** (most robust; required if you must also close the height gap or restyle): redact the whole table, then redraw rows from `tab.extract()` with the recorded styles. Paint order: cell/header fills → text → rules.
    - **B. Stamp-and-shift** (keeps the original rendering byte-for-byte): requires a clean stamp source, see step 3.

    **3. Make the stamp source clean.**
    Take a copy of the document, redact the deleted row's bbox **in that copy too**, and `apply_redactions()` it. This is mandatory: `show_pdf_page(rect, src, pno, clip=...)` embeds the *entire* source page in a Form XObject — the clip only sets visibility, not content. Stamping from an unredacted copy leaves the deleted row recoverable in the output even though `get_text()` shows nothing.

    **4. Remove the old content on the target page.**
    Add a redaction annotation over the deleted row **and every line that will move** (remaining rows, Total, and any notes/footer that shifts) — or over the whole table. Then `apply_redactions()`.

    Know the defaults, because they are not "text only":
    - `text=PDF_REDACT_TEXT_REMOVE` (0) — any glyph whose bbox overlaps the rect goes
    - `images=PDF_REDACT_IMAGE_PIXELS` (2) — overlapping image pixels are blanked
    - `graphics=PDF_REDACT_LINE_ART_REMOVE_IF_COVERED` (1) — any rule/shading lying *entirely* inside the rect is deleted; partly covered shapes stay whole

    So a whole-row rect destroys that row's shading and its interior rules — correct here, since you are redrawing or re-stamping. Pass `graphics=0, images=0` only for a tight per-cell edit where the grid must survive.

    Never use `PDF_REDACT_LINE_ART_REMOVE_IF_TOUCHED` (2) on a band whose vertical rules continue into rows that stay — it deletes the whole path, not just the segment inside the rect.

    **5a. Strategy B — stamp rows one row-height up.**

    ```python
    for j, r in enumerate(tab.rows):
        if j == i:
            continue
        dy = -h if j > i else 0
        page.show_pdf_page(r.bbox + (0, dy, 0, dy), clean_src, 0, clip=r.bbox)
    ```

    Clip exactly to each row bbox, so horizontal rules sitting on the row boundary are cut in half; because all rows below shift by the same `h`, adjacent halves land on the same new y and reconstruct the full rule. Never stamp onto a region whose old content is still on the page — that is how the deleted row survives underneath.

    If the table must keep its original total height, skip stamping and use strategy A, redistributing `h` across the remaining rows.

    **5b. Strategy A — redraw.** Fills first, then text, then rules. Column x-positions and row heights come from `tab.rows`; text colour/font/size from the header spans. New body cells inherit their row's body style.

    **6. Recompute the Total.**
    Sum the numeric values of the rows that remain (per column if the Total spans columns), reformat with the original precision/separators, and place it with the Total row's original font, size, colour and alignment. If the Total row was inside a redacted/stamped band, redact its old text separately and re-extract the neighbours afterwards to confirm nothing else was clipped.

    **7. Save to a new file, never in place.**

    ```python
    doc.save(out_path, garbage=4, deflate=True)
    ```

    `garbage=4` drops the now-unreferenced objects; `deflate=True` compresses. Saving over the source while it is open is how half-written files happen.

    **8. Verify.**

    - Reopen the **saved file** (not the in-memory doc) and assert the deleted row's strings are absent from `page.get_text()`.
    - Run `find_tables()` on the output: row count is one lower, remaining values and the Total match your recomputation, column boundaries unchanged.
    - Render `page.get_pixmap(dpi=150)`: check there is no blank band, no doubled/missing horizontal rule at each seam, no text sitting under a stamp, and that the table bottom and anything below it look right.
    - If the table lives inside a shared Form XObject, page-level redaction may not remove it (and may alter other pages that share it) — flatten or rewrite that XObject first, then verify against a reopened copy.

    ## What not to do

    - Cover the row with a white rectangle — the text is still there and extractable.
    - Redact only the glyphs and leave the row in place — you get an empty ruled row, and everything below still has to move.
    - `show_pdf_page(clip=...)` from an unredacted copy, or rebuild the page from clipped bands that skip the row — the clip hides, it does not delete; `get_text()` will look clean while the content is still in the file.
    - `graphics=REMOVE_IF_TOUCHED` on the shift band.
    - Save in place.

    **Scanned-table note:** if the table is a bitmap, `apply_redactions` only blanks pixels; you cannot reflow it. Replace the page image region, or rebuild the table on top of a cleaned background — text-level redaction and restamping do not apply.
- id: pdf-rowdel-02
  answer: |
    ## Defaults

    ```python
    page.apply_redactions(
        images=PDF_REDACT_IMAGE_PIXELS,                    # 2
        graphics=PDF_REDACT_LINE_ART_REMOVE_IF_COVERED,    # 1
        text=PDF_REDACT_TEXT_REMOVE,                       # 0
    )
    ```

    ### Images (`PDF_REDACT_IMAGE_PIXELS` = 2)
    Image objects are **kept**, but every pixel whose position falls inside a redaction rectangle is blanked/filled. The image XObject survives; you get a hole, not a smaller image and not a removed image.

    Other values:
    - `PDF_REDACT_IMAGE_NONE` (0) — images untouched.
    - `PDF_REDACT_IMAGE_REMOVE` (1) — the **whole** image is dropped if it overlaps the rect at all (even a 1-pixel overlap).
    - `PDF_REDACT_IMAGE_REMOVE_UNLESS_INVISIBLE` (3) — removes unless fully hidden.

    ### Vector graphics (`PDF_REDACT_LINE_ART_REMOVE_IF_COVERED` = 1)
    Line art (rules, borders, shading rectangles, paths) is **removed only if it lies entirely inside** the redaction rectangle. Partially covered paths are left **whole** — they are not clipped and not "touched → removed". So a long horizontal rule that pokes out of the rect survives in full; a short shade rectangle that fits entirely inside vanishes.

    Other values:
    - `PDF_REDACT_LINE_ART_NONE` (0) — keep all drawings, including shading inside the rect.
    - `PDF_REDACT_LINE_ART_REMOVE_IF_TOUCHED` (2) — delete any path that merely intersects the rect. Dangerous on table bands: a vertical rule running down into rows you are keeping is a single path, so the whole rule (including the part above the band) is deleted.

    ### Text (`PDF_REDACT_TEXT_REMOVE` = 0)
    Any glyph whose bbox overlaps a redaction rect is removed — this happens independently of the graphics/images settings.

    ## Why it matters for table rows and cells

    **Cell edit.** A tight redaction rect around one cell's glyphs usually sits entirely inside that cell's background fill. With the default `graphics=1`, the fill is deleted (and any short rule fully inside), so the cell goes white and the grid looks broken even though the text is correctly gone. Use `page.apply_redactions(graphics=PDF_REDACT_LINE_ART_NONE, images=PDF_REDACT_IMAGE_NONE)` to replace text while preserving shading and borders, and keep the rect tight to the glyphs — padding it outward only catches more shapes.

    **Row deletion.** With defaults, redacting a whole row deletes that row's shading and any rules that fit entirely inside the band — but the horizontal rules that extend across the table are only partially covered, so they stay, possibly as stubs at the wrong y. If you are rebuilding the table anyway, defaults are acceptable **provided** you redraw fills, text, and then rules in that order; if you are stamping moved rows from a pre-redacted source copy, that source must also have been redacted and applied, otherwise the deleted row persists in the XObject bytes invisible to `get_text()`.

    **What to avoid.** `PDF_REDACT_LINE_ART_REMOVE_IF_TOUCHED` on a band that touches vertical rules extending into retained rows strips those rules wholesale. And the defaults do **not** mean "leave line art alone" — that mistaken assumption is the usual cause of a table losing its grid or shading after a redaction.
- id: pdf-rowdel-03
  answer: |
    Short answer: **there is no `page.move_region()` API** — a PDF has no row objects, only a content stream — but you can move a band with its exact vector/text appearance by *stamping a clipped region from a scratch copy of the document* one row higher. Rasterizing a clipped pixmap is the only other true "move", at the cost of losing selectable text.

    ## 1. The usual way: `show_pdf_page` with `clip` (vectors + text preserved)

    ```python
    import fitz

    doc  = fitz.open("in.pdf")
    page = doc[0]
    tab  = page.find_tables().tables[0]
    rows = tab.rows

    i     = 2                                    # row to delete
    row_h = rows[i].bbox[3] - rows[i].bbox[1]

    # band that must move up = everything below the deleted row
    band = fitz.Rect(rows[i].bbox[0], rows[i+1].bbox[1],
                     rows[-1].bbox[2], rows[-1].bbox[3])   # extend to Total/notes

    # --- scratch copy: the deleted row must be REALLY gone here too ---
    src = fitz.open("in.pdf")                    # reopen; do not stamp a page onto itself
    src[0].add_redact_annot(fitz.Rect(rows[i].bbox))
    src[0].apply_redactions()                    # keep defaults if you redraw rules

    # --- target page: erase the deleted row AND the whole band that moves ---
    page.add_redact_annot(fitz.Rect(rows[i].bbox))
    page.add_redact_annot(band)                  # or the whole table
    page.apply_redactions()

    # --- stamp the band one row higher ---
    dest = band + (0, -row_h, 0, -row_h)
    page.show_pdf_page(dest, src, 0, clip=band, keep_proportion=False)

    # recompute the Total from your data, then
    doc.save("out.pdf", garbage=4, deflate=True)
    ```

    Two traps, both covered above:

    - `clip` only **hides** the rest of the source page; the stamped XObject still embeds the entire source page. So the source copy must have the deleted row redacted *and applied* first — otherwise the deleted row is recoverable from the output bytes even though `get_text()` no longer shows it.
    - The deleted row must be redacted on the **target** page as well; never leave it on the page under the stamp. Same for the old position of the band, or the bottom row will appear twice (once moved, once in place).

    ## 2. Raster fallback (exact pixels, text becomes an image)

    ```python
    pix = src[0].get_pixmap(clip=band, dpi=300, alpha=False)
    page.insert_image(dest, pixmap=pix)
    ```

    Useful when the source copy is messy or fonts misbehave, but you lose text selection/editability and inflate the file.

    ## 3. Pure vector approach: transform the content stream (no clipping tricks)

    PDF content can be translated with a matrix, which keeps text and rules as-is:

    ```python
    page.wrap_contents()                 # guarantees q ... Q around the content
    xref = page.get_contents()[-1]
    s = doc.xref_stream(xref)
    # PDF coordinates are y-up: moving up on the page is +row_h in PDF y
    doc.update_stream(xref, b"q 1 0 0 1 0 %g cm\n" % row_h + s + b"\nQ")
    ```

    Caveats: this moves **everything** the wrapped stream draws (header included). To confine it you must split the content stream into segments (one per table row / band) so only the rows below the deletion sit inside the `q ... cm ... Q`; and it **does not delete anything** — you still have to redact the old positions and the deleted row. For a table built by a single drawing pass, practically nobody does this; `show_pdf_page` is the sane route.

    ## Bottom line

    `show_pdf_page(new_rect, scratch_doc, pno, clip=old_rect, keep_proportion=False)` is the PyMuPDF answer: it moves the region as a vector Form XObject, preserving exact appearance and text, provided the scratch document already had the deleted row redacted and applied, and the target region (deleted row + old band) was redacted before stamping.
- id: pdf-rowdel-04
  answer: |
    Deleting a row changes more than the row. In practice you should expect to update:

    **1. The money column — the main one**
    - Line-item subtotal and any per-row extended amount.
    - The table's subtotal → tax/VAT/GST line (tax is usually computed from the subtotal, so the tax amount itself changes) → shipping/discount lines → grand total / amount due.
    - Tax broken out per rate: removing a row can remove an entire tax band, not just change the tax value.
    - Amount in words, if the invoice spells the total out.
    - Any deposit / balance-due / amount-paid figures tied to the total.

    **2. Bottom-of-table and below-table content**
    - Everything below the deleted row moves up one row height: remaining rows, the Total/Subtotal block, notes, payment terms, bank details, the footer, and any image or stamp anchored below the table.
    - Page count / page numbering if the table spans pages, plus the "continued on next page" markers and the repeated header row.
    - Pagination of the whole document (footers like "Page 1 of 2", total-pages fields).

    **3. Row identity and references**
    - Row/line numbers (1, 2, 3 …) if the invoice numbers items — every row after the deleted one renumbers.
    - Item count / "N items" labels.
    - Product codes, SKUs, or serial numbers if they are sequential.
    - Cross-references in the body text or notes ("see item 3", "as per line 5", "per the attached schedule").
    - Totals quoted in the covering email/letter, the purchase-order reference, or a summary box that repeats quantities.

    **4. Document-level metadata**
    - The document's own text layer and any embedded/clipped copies: if a row is stamped, clipped, or rebuilt, the deleted strings must actually be gone from the file, not just invisible.
    - Anything computed elsewhere and cached: totals embedded in an invoice number sequence, a JSON/XML sidecar, an accounting export, or a linked summary page.

    **5. Structural / visual**
    - Table borders, zebra striping, and shading need redrawing for the shifted rows — not just text.
    - Alignment: right-aligned currency columns must stay aligned after re-layout.
    - Whitespace: once a row is gone, the table may no longer fill the frame, or content may now collide with the footer; spacing often needs redistributing rather than just shifting up.

    **Short answer:** the totals chain (subtotal → tax → grand total, and amount in words), everything positioned below the table that must move up, row numbering and item counts, and any internal or external references to that line. The row is the easy part; the derived figures and the layout shift are what actually break.
- id: pdf-cell-01
  answer: |
    ## Steps: replace one right‑aligned number in a cell

    The rule is: **remove the old glyphs for real, then re‑draw the new string with the original font, size, colour and the original right edge and baseline.** Don't paint an opaque patch over the number — the old text stays in the text layer.

    ### 1. Find the cell and read its style

    ```python
    import fitz

    doc  = fitz.open("in.pdf")
    page = doc[pno]

    # Locate the cell with find_tables (or pick the word from get_text("words"))
    tabs = page.find_tables()
    tab  = tabs.tables[0]
    cell_rect = None
    for row in tab.rows:
        for c in row.cells:
            if c and fitz.Rect(c).contains(fitz.Rect(word_rect)):
                cell_rect = fitz.Rect(c)
    ```

    Then get the exact span that holds `"22.50"`:

    ```python
    d = page.get_text("dict")
    for b in d["blocks"]:
        for l in b.get("lines", []):
            for s in l["spans"]:
                if "22.50" in s["text"]:
                    span = s
    ```

    Record from the span:

    - `bbox` — tight glyph box (the redaction target)
    - `origin[1]` — **baseline** y (use this for the new text)
    - `bbox.x1` — **right edge** of the old number; the new number should end here
    - `size` — font size
    - `font` — font name
    - `color` — sRGB int; convert with `fitz.sRGB_to_pdf(span["color"])`
    - `flags` — to detect bold/italic

    Using the old word's `x1` as the anchor (not `cell_rect.x1`) reproduces the cell's right padding exactly, which is what makes it look native.

    ### 2. Make sure the font is available

    - Base‑14 (`helv`, `hebo`, `heit`, `hebi`, `times-roman`, `cour`, …): use directly.
    - Embedded/custom font: extract it from the document and embed it in the page so glyph widths and shapes match:

    ```python
    name, ext, typ, buf = doc.extract_font(span_font_xref)   # or doc.get_page_fonts(pno)
    open("f.ttf", "wb").write(buf)
    page.insert_font(fontname="F0", fontfile="f.ttf")
    ```

    If the original is a subset font, the new glyphs may be missing. In that case use a visually compatible font (or the same family full‑weight file) and keep the same size/colour; the exact width matters less because the text is right‑aligned.

    ### 3. Redact only the digits, keeping grid and shading

    ```python
    r = fitz.Rect(span["bbox"])
    # Small vertical padding is fine; never pad horizontally into the cell rules.
    r.y0 -= 0.5 * (r.height * 0.15)
    r.y1 += 0.5 * (r.height * 0.15)

    page.add_redact_annot(r, fill=False)     # fill=False -> no white box over the cell fill
    page.apply_redactions(
        images=fitz.PDF_REDACT_IMAGE_NONE,       # 0
        graphics=fitz.PDF_REDACT_LINE_ART_NONE,  # 0
    )
    ```

    - `fill=False` is important: the default redaction fill is white/opaque and would cover the cell's background shading.
    - `graphics=0` keeps the cell borders and shading. The defaults (`graphics=1`) delete any line art lying entirely inside the rect, which can eat rule segments.
    - Do **not** use `REMOVE_IF_TOUCHED` here: it deletes whole paths that merely cross the band, including vertical rules belonging to rows that stay.

    ### 4. Draw the new number right‑aligned on the old baseline

    ```python
    new_text = "54.00"

    # Width with the actual font
    if embedded:
        w = fitz.Font(fontfile="f.ttf").text_length(new_text, fontsize=span["size"])
    else:
        w = fitz.get_text_length(new_text, fontname="helv", fontsize=span["size"])

    x = span["bbox"].x1 - w          # right edge fixed -> same alignment
    y = span["origin"][1]            # same baseline

    page.insert_text(
        (x, y),
        new_text,
        fontname="F0" if embedded else "helv",
        fontsize=span["size"],
        color=fitz.sRGB_to_pdf(span["color"]),
    )
    ```

    `insert_textbox(..., align=fitz.TEXT_ALIGN_RIGHT)` also works, but vertical placement via a textbox is less exact; `insert_text` at the original baseline is more reliable and keeps the number sitting where the old one sat.

    ### 5. Save and verify

    ```python
    doc.save("out.pdf", garbage=4, deflate=True)
    ```

    Checks that catch the usual failures:

    - `get_text("words")` in the cell now yields `"54.00"`; `"22.50"` is gone from the page text (redaction removed it, not covered it).
    - Neighbouring cells still extract unchanged (the rect was tight).
    - `len(page.get_drawings())` before/after is unchanged, and a render shows the gridlines and cell fill intact.
    - Render the cell at ~300 dpi and eyeball the baseline and right edge:

    ```python
    pix = page.get_pixmap(clip=cell_rect + (-2, -2, 2, 2), dpi=300)
    pix.save("cell.png")
    ```

    ### Notes / gotchas

    - If the page has `/Rotate`, map coordinates through `page.derotation_matrix` before drawing; otherwise the new text lands rotated relative to the visible page.
    - `show_pdf_page` is not a substitute for step 3: it stamps the whole source page and only clips the view, so the old number stays recoverable in the file.
    - Recompute nothing else unless the cell feeds a total; the question only changes one standalone cell.
    - If the font is a subset that lacks `5`, `4`, `0`, embed the full family file rather than letting PyMuPDF silently substitute a different face — that substitution is the main thing that makes an edit look non‑native.
- id: pdf-cell-02
  answer: |
    **Size it to the glyphs of that one cell — not to the cell's border box, and with no safety padding.**

    Practical recipe:

    1. Get the target cell's text rectangle from `page.find_tables()` cell bbox, but then **shrink it to the actual text**. The cell bbox includes the padding between the text and the rules. Use the union of the target cell's character/word bboxes (`get_text("words")`/`"rawdict"` spans clipped to the cell), or the cell bbox intersected with the words that fall inside it.
    2. Keep the rect **strictly inside the cell's interior**: stop short of the four rule lines by roughly half the rule width (plus a pixel or two). Do not pad outward "to be safe."
    3. Vertically, use the line's glyph bbox (ascender/descender included) rather than the visual ink, and check it doesn't poke into the row above/below if the leading is tight.
    4. Pass `apply_redactions(graphics=0, images=0)` if you want the cell's fill/shading and the surrounding grid preserved; with the defaults, any shading or rule segment lying entirely inside the rect is deleted.

    Why:

    - Removal is **overlap-based, not containment-based** — a character is deleted if its bbox touches the rect at all, and the *whole* character goes. That cuts both ways: a rect that only barely grazes a glyph still erases it, so you can be tight and still fully clear the cell; but a rect that spills even a sliver into a neighbouring cell erases that neighbour's character too, silently, with no visual trace in the render.
    - Character bboxes come from font metrics (ascender/descender, side bearings), so they are typically **larger than the visible ink** and can butt against or overlap the neighbouring cell's glyph boxes in a tight table. Cell padding is often smaller than this metric slop, which is exactly where spills happen.
    - The grid is a separate concern: default `apply_redactions` uses `graphics=PDF_REDACT_LINE_ART_REMOVE_IF_COVERED`, so a rect covering the cell's shading or a rule that lies wholly inside it deletes that artwork. Keeping the rect inside the cell and/or disabling graphics/image redaction avoids repainting the grid.

    Afterwards, re-extract the neighbouring cells (and the row above/below) to confirm their text is intact — that check is the only reliable way to catch a spill.

    If the cell's glyph boxes genuinely overlap the neighbour's (very tight columns), you cannot separate them with one rect; redact and redraw the whole row/table instead.
- id: pdf-cell-03
  answer: |
    ## What `insert_textbox` does

    `Page.insert_textbox(rect, text, ...)` lays the text out inside `rect`, wrapping at spaces (no automatic hyphenation), and returns a **float**:

    - **`>= 0`** — success; the value is the unused vertical space left in the box.
    - **`< 0`** — the text did **not** fit. The magnitude is roughly the missing height.

    The important part: the write is **all-or-nothing**. On a negative return, **no text is written at all** (the box stays as it was), so a "failed" cell is silently empty unless you check the return value. It also does not clip and does not auto-shrink. Widening doesn't help a "long" string much — it wraps into more lines, so the real constraint is height.

    (Caveat: I'm confident about the negative-return-means-nothing-written semantics; I'm less sure whether an individual word wider than the rect is broken mid-word or overflows. Verify by extracting the text afterwards.)

    ## Options

    1. **Check and react to the return value.** This is the baseline:
       ```python
       spare = page.insert_textbox(rect, txt, fontsize=9, fontname="helv", color=(0, 0, 0))
       if spare < 0:
           ...  # nothing was drawn — retry smaller / taller
       ```
       Since a failure draws nothing, retrying is safe.

    2. **Shrink-to-fit manually** — step the font size down until it fits:
       ```python
       size = 9.0
       while size >= 5.0:
           if page.insert_textbox(rect, txt, fontsize=size, ...) >= 0:
               break
           size -= 0.25
       else:
           ...  # fall back to truncation / taller cell
       ```
       Tie the floor to the table's smallest body size so the cell doesn't look broken.

    3. **`insert_htmlbox(rect, html, scale_low=...)`** does the shrinking for you: it scales the content down to fit, down to `scale_low` (default `0` = no scaling). It returns `(spare_height, scale)`, so a negative `spare_height` still tells you it overflowed. Good when a small font change is acceptable and you want styling (bold spans, etc.).

    4. **Tighten vertical metrics** — `lineheight=0.85–0.9` (and, if you control it, the rect's padding) buys a line or two without touching the font size.

    5. **Partial write with `TextWriter.fill_textbox(rect, text, ...)`** — writes what fits and **returns the list of lines that didn't fit**. Use it when truncation is acceptable, then redraw the leftovers elsewhere.

    6. **Give it more room** — make the row taller (and shift the rows below, redrawing their rules/fills), or span the text across two lines/cells. If the table can't grow, this becomes a design decision, not a layout one.

    7. **Shorten the content** — abbreviation, or ellipsis-truncate to the measured width (`fitz.get_text_length(s, fontname="helv", fontsize=size)`), and put the full text in a note/footnote.

    8. **`insert_text()` / `TextWriter.append()`** draws unconditionally and can overflow the rect — only acceptable if overflow is visually harmless, which in a bordered cell it usually isn't.

    9. **`Story`** for flowable content across multiple rects/pages: `story.place(rect)` returns `(filled, more)`, so you know exactly when to continue on another page/box.

    ## Practical recipe for a table cell

    Redact the old glyphs with `graphics=0`/`images=0`, then: try body size → step down to a floor → try `lineheight=0.9` → if still negative, truncate with ellipsis and log a warning. Always re-extract the cell (`get_text("dict")`) afterwards to confirm the glyphs are actually there — a negative return means the cell renders blank, not "slightly crowded".

    *Assumption: PyMuPDF ≥ 1.23, a fixed-height cell rect, and that a failed `insert_textbox` writes nothing (retry-safe).*
- id: pdf-cell-04
  answer: |
    `point` is the **baseline origin** of the first character — `point.x` is roughly the left edge where the first glyph starts, and `point.y` is the **baseline** the text sits on. It is *not* the top-left of the text box. Ascenders/uppercase go above `point.y` (by roughly the font ascender, ~0.7–0.8 × fontsize), descenders go below it.

    If you pass the top-left corner of the old word's bbox (`(bbox.x0, bbox.y0)`):

    - The replacement is drawn **too high**, shifted upward by about one ascender height (typically ~0.7–0.8 × fontsize). The glyphs end up straddling or above the cell's top edge instead of filling the old word's vertical slot.
    - Typical visible symptoms: text overlaps the row above / the header / the top rule, the lower half of the cell looks empty, and near the page top the text can be clipped off the page.
    - The horizontal position is approximately right (x0 is close to the advance origin), but any left side bearing, justification, or a different font size still makes it not line up exactly.

    Correct approaches:

    - Reuse the original span's baseline: `span["origin"]` from `get_text("dict")` — `origin[1]` is exactly the baseline y the old word used.
    - Or compute it from the bbox: `baseline_y ≈ bbox.y1 - descender`, e.g. `bbox.y1 - 0.2 * fontsize` (font-dependent), rather than `bbox.y0`.
    - Or use `insert_textbox(rect, ...)` if you want to hand it the cell rectangle and let it position/wrap the text — but note that's a different API with different semantics (rect, not a point).
- id: pdf-relayout-01
  answer: |
    When a row is inserted above the Total row, the table must grow by exactly one row height. That means:

    - **Total row:** move down by one row height, keeping its style and alignment. Its former top rule becomes the bottom rule of the inserted row. If the Total is a sum over the table rows, recompute it so it includes the new row; if it is a fixed subtotal, leave the value but still move the row.
    - **Table rules:** extend the vertical column rules through the new row band, and add the new row’s top and bottom horizontal rules. The Total row keeps its own bottom/outer border, now one row lower. Avoid double-drawing the boundary rules, and preserve any zebra shading or fills consistently, including any alternation shift in later rows.
    - **Content below the table:** shift everything below the table — notes, source lines, captions, footer-adjacent content — down by the same row height. The table’s outer bottom border also moves down. If that would collide with the footer or run past the bottom margin, reflow or paginate rather than overlapping; do not squeeze content into the footer.

    In PDF terms, the table is only text and vector rules, so this is a layout rebuild: remove the old Total row and the region below it from the page content, then redraw them shifted down. A white rectangle over the old Total is not enough — the old text and rules remain in the content stream and text layer. Redact the moving region, apply the redactions, then redraw the new row, the shifted Total, the adjusted rules, and the shifted content below. Verify afterward that the old Total position no longer contains the old text.
- id: pdf-relayout-02
  answer: |
    ## Adding a column to a full-width table

    The table has no free horizontal space, so the new column must be paid for by the existing columns. That single fact drives both the correct procedure and the usual failure.

    ### Correct approach

    1. **Measure from the table, not the page.** Take the real span: `x0` of the first column's cell to `x1` of the last column's cell (from `find_tables()` or the cell/drawing bboxes). This normally equals the margin-to-margin width. Use those coordinates as the available width — never `page.rect` minus a fixed margin, because headers, indents and footers make the true edges differ.

    2. **Re-divide the available width.** The new column gets its width by shrinking the existing ones — proportionally (preserving their relative ratios) is the safe default, with a minimum width per column and a check that each cell's text still fits (wrap or reduce font size if not). The sum of all column widths must stay exactly equal to the original table width. If any single column cannot shrink enough, the answer is to wrap text or shrink the font, not to widen the table.

    3. **Rebuild the whole table, don't patch it.** Because every column boundary moves, every row shifts. Redact the entire table region (header + body + rules + shading), call `apply_redactions()`, and redraw from extracted data. Editing only the strip where the new column goes is not a relayout.

    4. **Paint in the right order: fills → text → rules.** Cell and header fills first, then the text, then the grid lines. A fill drawn after the text hides it visually while leaving it in the text layer.

    5. **Copy the existing header style for the new column.** Read it from the header spans via `get_text("dict")`: the header text is white (`color` 0xFFFFFF) on the dark fill, with the same font, size and fill. Give body cells the body style of their row. A new column that defaults to black text on a light fill is the tell-tale sign of a hand-rolled patch.

    6. **Recompute derived values** (Totals, subtotals) from the row data, not by copying the old text.

    7. **Validate:** save to a new file with `garbage=4, deflate=True`, then confirm `get_text()` has no old column strings, no duplicated header text, and that every cell's text fits its new cell rect. Render and eyeball it; verify the right edge sits on the same x as before and intersects no word/drawing/image bbox.

    ### The common mistake

    **Appending the new column outside the existing right edge** — i.e. keeping every existing column at its old width and drawing the new one to the right of the last column. Since the table already runs margin to margin, this pushes the new column past the right margin (or off the page), producing overflow, clipped text, or a table that no longer aligns with the rest of the layout. The correct move is to reclaim the width from the existing columns first.

    Two closely related mistakes that usually accompany it:

    - **Patching in place instead of redrawing.** Drawing the new column over the old layout while the old cell text/rules remain underneath — or "removing" old content with `show_pdf_page(clip=...)`, which only hides content outside the clip while the full source page stays embedded. Real removal requires redaction of the whole table (or a rebuild from data).
    - **Deriving geometry from page margins** (`page.rect` minus a fixed inset) instead of the table's actual cell edges, so the "full width" is mis-measured and the columns don't line up with the original layout.

    Assumption: "full width between the page margins" means there is zero slack, so no column can be added without re-weighting the others; if the table were narrower than the text column there would be free space to consume, and the mistake would instead be failing to notice the available slack.
- id: pdf-relayout-03
  answer: |
    Short answer: it is acceptable when the PDF is disposable output rather than the artifact, the edit is large or structural, and nobody requires the result to match the old pagination pixel for pixel. It is not acceptable as a routine way to change one cell, or anywhere the PDF itself is the legal/visual record.

    ## When the round-trip is the right call

    - **The PDF is born-digital and structurally clean.** Real text objects, real table markup (or at least reliable `find_tables`-grade structure), no scanned pages. If the file is a scan, conversion mostly produces positioned text boxes over an image and you have gained nothing.
    - **You have, or can get, the original source.** DOCX/InDesign/LaTeX/HTML → re-export is the correct path, not PDF → DOCX → PDF. The best version of this proposal is "stop editing the PDF, edit what produced it." Ask for the source before agreeing to a round-trip.
    - **The change is bulk or reflowing.** Translation, restyling, adding a column that must re-wrap every row, repagination, splitting one document into many, or generating many variants from one master. Hand-patching a PDF for hundreds of edits is worse than re-rendering.
    - **Fidelity tolerance is explicit and accepted.** "Same content, approximate layout" is a valid spec. "Same page breaks, same line breaks, same fonts, same table geometry" is not achievable through a converter.
    - **The output becomes the new source of truth.** Then do a one-time migration, commit the DOCX, and regenerate PDFs from it. Never keep ping-ponging.
    - **Internal/prototype use** where a slightly different look costs nothing.

    ## The risks, in rough order of how often they bite

    1. **Non-idempotent, cumulative drift.** PDF→DOCX is heuristic reconstruction; DOCX→PDF is a different layout engine with different font metrics, hyphenation and table-width algorithms. Each cycle shifts line breaks, row heights except where fixed, page breaks and total page count. Two rounds rarely equal one.
    2. **Table structure is the first casualty.** Merged cells, rowspans/colspans, repeating header rows, nested tables, rotated headers, multi-line cells, cell shading and rules all get reinterpreted. Column widths get recomputed from content. What looked like a faithful import renders differently.
    3. **Typography substitution.** If the embedded fonts aren't installed or licensed on the converting machine, you get substitutes with different metrics — which is what silently causes the reflow. Embedded-subset fonts cannot be reused by the converter.
    4. **Silent content loss or duplication.** Headers/footers become body text or vanish; footnotes detach; captions reflow away from figures; vector art, equations and charts get rasterised or dropped; text in odd containers gets skipped entirely. Nothing errors — you just ship a document missing a clause.
    5. **Redaction is not fixed by conversion.** A black rectangle drawn over text in DOCX is not redaction; the text survives and can be extracted or uncovered. Conversely, a real redaction done in the PDF before conversion is fine — but if you convert first, "delete" the text in Word, and export, you must re-verify the output's text layer. This is exactly the failure mode the direct-PyMuPDF workflow avoids.
    6. **Lost PDF furniture.** Bookmarks/outline, internal and external links, named destinations, metadata/XMP, attachments, layers/OCGs, form fields, annotations and comments, reading order and accessibility tags. Forms and annotations usually do not survive at all.
    7. **Digital signatures and archival claims die.** Any edit invalidates a signature (correctly — it should). If the file is PDF/A for records retention, a round-trip through a word processor generally breaks conformance.
    8. **Fidelity is asserted, not measured.** People check page 1 and ship. The damage is usually on page 7 in a spanned table.
    9. **Supply chain and confidentiality.** Cloud converters see the whole document. For contracts, medical, financial or personal data, that is a policy decision, not a convenience.
    10. **Ambiguous provenance.** Afterwards nobody knows whether the PDF or the DOCX is authoritative, and the two diverge with the next edit.

    ## How to decide, concretely

    Ask four questions:

    - Is the text real and the table a real table? (If no → don't convert; OCR or rebuild.)
    - Is the requirement "same content" or "same appearance"? (If appearance-exact → don't convert.)
    - Is this one surgical change or a document-wide rewrite? (One cell → edit the PDF directly, tight redaction rect, `graphics=0, images=0` so the grid survives; whole-table/whole-doc → regenerate from source.)
    - Does anything downstream depend on the PDF bytes — signature, PDF/A, page-numbered references, printed cross-references? (If yes → don't convert.)

    If you do convert, validate like you don't trust it:

    - Word-level text diff between old and new extraction, both directions (missing *and* duplicated content).
    - Page count and per-page word counts; table content compared cell by cell against the original data.
    - Render every page and compare images; inspect the pages where the diff is non-zero.
    - Re-extract the output's text layer to confirm anything you intended to remove is genuinely absent — remembering that a visible-render check cannot see content hidden by clipping.
    - Check bookmarks, links, metadata, fonts and page size explicitly.

    ## The practical position

    For one cell, one row, or one inserted image, edit the PDF in place — it preserves everything you are not touching, and the diff is auditable. For a new table or a document-wide restructure, go back to the source format and re-export; that is the round-trip done properly. The PDF→DOCX→PDF loop is a migration tool for when no source exists and exact fidelity is not required — treat its output as a new document that needs review, not as the same document with a correction.
- id: pdf-relayout-04
  answer: |
    ## The short version

    Build an **occupancy map** of every rect already on the page, build the **candidate rect** for the grown table / new content, then assert two things: the candidate lies inside the allowed content area, and its intersection with every occupied rect has zero area (after padding). Don't eyeball it and don't trust a single `get_text()` call — text, images, vector drawings, annotations and widgets are separate sources.

    ## 1. Decide the coordinate system first

    `get_text()`, `get_drawings()` and `get_image_bbox()` all report in the page's **unrotated** coordinate space, while `page.rect` is the visible rect adjusted for rotation. Pick one space — the unrotated one — and convert everything else into it:

    ```python
    m = page.derotation_matrix          # unrotated -> rotated (and inverse)
    allowed = fitz.Rect(page.cropbox)   # content limit, not mediabox/bleed
    ```

    Using `page.rect` and `page.cropbox` interchangeably is the classic source of false "there's room" results on landscape/rotated pages.

    ## 2. Collect every occupied rect

    ```python
    def occupied(page, skip=fitz.Rect()):
        rs = []
        for w in page.get_text("words"):          # x0,y0,x1,y1,word,...
            r = fitz.Rect(w[:4])
            if not r.intersects(skip):
                rs.append(r)
        for b in page.get_text("dict")["blocks"]:
            if b["type"] == 1:                    # image block
                rs.append(fitz.Rect(b["bbox"]))
        for d in page.get_drawings():             # rules, shading, fills, strokes
            rs.append(fitz.Rect(d["rect"]))
        for a in page.annots() or []:
            rs.append(a.rect)
        for wd in page.widgets() or []:
            rs.append(wd.rect)
        return rs
    ```

    Notes that matter in practice:

    - `get_text("words")` gives tight glyph boxes and ignores trailing/inter-word whitespace. Pad each by roughly `0.2 * fontsize` vertically and a few points horizontally, or you'll accept a gap that visually crowds.
    - `get_image_bbox()` needs the image's xref/name from `get_images()`; the `"dict"` image block bbox is usually simpler and sufficient.
    - `get_drawings()` returns the bbox of the **whole path**. A table's grid is often one path, so its bbox is the entire table. That's fine if you treat the table as solid, but if you're growing *inside* a region crossed by one long path, inspect `d["items"]` (individual `"l"` line segments, `"re"` rects) instead of the path bbox.
    - Cross-check completeness with `page.get_bboxlog()`, which enumerates every painted object (paths, text objects, images) as `(type, rect)`. Its text boxes are loose, but as a superset test it catches things the loop above missed.
    - Exclude the object you're modifying: pass the current table bbox as `skip`, otherwise the table always "occupies" the space you want to grow into.

    ## 3. Build the candidate rect and test it

    For growing a table downward, the candidate is `table.bbox` extended to the needed height; for a new block, it's the rect you intend to draw.

    ```python
    def free_enough(cand, occ, allowed, pad=2.0, min_area=0.25):
        cand = fitz.Rect(cand) + (-pad, -pad, pad, pad)   # safety margin
        if not allowed.contains(cand):
            return False                                   # hits margin/footer/edge
        for r in occ:
            inter = cand & r
            if not inter.is_empty and inter.get_area() > min_area:
                return False
        return True
    ```

    Use the intersection rect and check it is empty (or negligible area) rather than `Rect.intersects`, so that merely touching edges — a table rule at exactly `y1`, a neighbouring cell border — doesn't veto a valid placement. The `min_area` guard absorbs float noise and hairline overlaps.

    ## 4. The cheap "grow downward" shortcut

    If the question is only "is there vertical room below existing content?", the gap test is enough:

    ```python
    limit_y   = footer_top            # or allowed.y1
    content_y = max(r.y1 for r in occ)   # after excluding the table itself
    needed    = new_table_height - current_table.bbox.height
    ok        = (limit_y - content_y) >= needed + clearance
    ```

    Then still run the rect test on the band `(x0, content_y, x1, content_y + needed)`, because a footnote, page number, or image can sit below the main block and be missed by a single "max y1" if you computed it from text blocks only.

    For a new table column, do the horizontal analogue: `x` extent from the table's right edge, and check the vertical band of each row.

    ## 5. Beware of things that are invisible to these checks

    - **Clipped content.** Objects outside the CropBox, or inside a Form XObject's clip, are not painted but are still in the file. A rect can look free on the page and still collide with content that becomes visible if the clip changes. If you care, check the clip path in `get_drawings()` / the XObject bbox, not just the page.
    - **White or covered text** is still returned by `get_text()`, so it correctly shows as occupied.
    - **Annotations/widgets** are not in `get_text()` or `get_drawings()`; skip them and you can place content under a stamp.
    - **Shading with no visible border** appears only in `get_drawings()`; a text-only check will happily place a table over a filled header band.

    ## 6. Verify empirically, don't just compute

    After placing, re-extract and re-test: confirm no word bbox and no drawing rect in the output intersects the new content's rect (except the new content's own), and render `page.get_pixmap(clip=candidate, dpi=150)` to look at it. Best practice is to do the insertion on a **copy** of the document first, run the occupancy assertion on the copy, and only then commit — that catches both geometry mistakes and paint-order mistakes (e.g. a fill drawn after the text hides it while the text layer still reports it).

    **Bottom line:** enough space = candidate rect inside the allowed area, zero positive-area intersection with the union of word/spans, image, drawing, annotation and widget rects (padded, excluding the object being edited), plus the gap to the footer exceeding the needed growth. Rotation, path-bbox lumping, clipped content and annotations are the four ways this check lies.
- id: pdf-tblins-01
  answer: |
    Two practical routes: draw the table directly on the page with PyMuPDF's drawing API, or build it as a tiny separate PDF and stamp it in. Both keep the existing page content intact.

    ## Shared first step: locate genuinely free space

    A table needs a rect that hits no text, no existing rules/shading, and no image. Derive it from the content, then assert the rect is clear:

    ```python
    import pymupdf

    doc  = pymupdf.open("in.pdf")
    page = doc[0]

    occupied = [pymupdf.Rect(b[:4]) for b in page.get_text("blocks")]
    occupied += [d["rect"] for d in page.get_drawings()]
    occupied += [pymupdf.Rect(i["bbox"]) for i in page.get_image_info()]

    x0, w   = 72, 260          # left margin, table width
    row_h   = 16
    n_rows  = 3                # header + 2 body rows
    y1      = page.rect.y1 - 72
    rect    = pymupdf.Rect(x0, y1 - n_rows*row_h, x0 + w, y1)

    assert not any(r.intersects(rect) for r in occupied), "chosen area is not free"
    ```

    If nothing is free at the bottom, scan downward from the last content bbox, or place it in a margin column. Don't merely use `page.rect` minus a fixed margin — that assumes a layout the page may not have.

    ## Way 1 — Draw it natively with PyMuPDF (shapes + text)

    Paint order matters: **fills → text → rules**. A fill drawn after text hides the glyphs in the render while leaving them in the text layer.

    ```python
    cols = [0, 120, 200, w]                     # column x-offsets
    head = ["Item", "Qty", "Price"]
    body = [["Widget", "2", "9.50"], ["Gadget", "1", "12.00"]]
    size, fs = 9, "helv"

    # 1) fills
    hdr = pymupdf.Rect(x0, rect.y0, x0 + w, rect.y0 + row_h)
    page.draw_rect(hdr, color=None, fill=(0.20, 0.25, 0.30), width=0)

    # 2) text (white on the dark header, black in the body)
    for r, cells in enumerate([head] + body):
        for c, txt in enumerate(cells):
            cell = pymupdf.Rect(x0 + cols[c], rect.y0 + r*row_h,
                                x0 + cols[c+1], rect.y0 + (r+1)*row_h)
            inner = cell + (4, 1, -4, -1)       # tight padding, stays inside the cell
            rc = page.insert_textbox(
                inner, txt, fontsize=size, fontname=fs,
                color=(1, 1, 1) if r == 0 else (0, 0, 0),
                align=pymupdf.TEXT_ALIGN_LEFT,
            )
            assert rc >= 0, f"text does not fit: {txt}"   # negative = overflow, nothing drawn

    # 3) rules last
    for r in range(n_rows + 1):
        y = rect.y0 + r*row_h
        page.draw_line((x0, y), (x0 + w, y), color=(0, 0, 0), width=0.6)
    for c in range(len(cols)):
        x = x0 + cols[c]
        page.draw_line((x, rect.y0), (x, rect.y1), color=(0, 0, 0), width=0.6)

    doc.save("out.pdf", garbage=4, deflate=True)
    ```

    Notes:
    - `insert_textbox` returns a negative number when the text doesn't fit — check it, or the cell silently comes out empty.
    - `helv` is a built-in font, so nothing extra is embedded. For a house font, `page.insert_font(fontname="F0", fontfile="Inter.ttf")` and pass `fontname="F0"`.
    - Because the table is pure page content, it is selectable/searchable and there is no hidden source object.

    ## Way 2 — Generate the table as its own PDF, then stamp it in

    Useful when you already have table layout code (reportlab, matplotlib, an HTML→PDF renderer) or want to reuse the same table on many pages.

    ```python
    import io
    from reportlab.pdfgen import canvas
    from reportlab.lib.colors import HexColor

    w, h = 260, 3*16
    buf = io.BytesIO()
    c = canvas.Canvas(buf, pagesize=(w, h))     # reportlab origin is also bottom-left
    c.setLineWidth(0.6)
    c.setFillColor(HexColor("#333F4D"))
    c.rect(0, h-16, w, 16, stroke=0, fill=1)    # header fill, top band
    c.setFillColor(HexColor("#FFFFFF"))
    c.setFont("Helvetica", 9)
    for x, t in zip((4, 124, 204), ("Item", "Qty", "Price")):
        c.drawString(x, h-12, t)
    c.setFillColor(HexColor("#000000"))
    for i, (a, b, d) in enumerate([("Widget","2","9.50"), ("Gadget","1","12.00")]):
        y = h - 16*(i+1) - 12
        for x, t in zip((4, 124, 204), (a, b, d)):
            c.drawString(x, y, t)
    for r in range(4):                          # rules
        c.line(0, h-r*16, w, h-r*16)
    for x in (0, 120, 200, w):
        c.line(x, 0, x, h)
    c.showPage(); c.save(); buf.seek(0)

    src = pymupdf.open(stream=buf.read(), filetype="pdf")
    page.show_pdf_page(rect, src, 0, overlay=True)   # rect = destination on page 1
    doc.save("out.pdf", garbage=4, deflate=True)
    ```

    Notes:
    - Make the source page exactly the table's size. `show_pdf_page` embeds a Form XObject containing the **whole source page**; a clip only controls what is visible, so a larger source page carries invisible extra content in the file.
    - `overlay=True` draws on top of existing content rather than replacing the page. Keep the source background transparent (don't fill the full page white) or you'll blank whatever sits under the table.
    - Text stays selectable because the XObject keeps the text operators.
    - `pypdf` equivalent: build the overlay with reportlab, then `page.merge_page(overlay_page, over=True)` after mapping coordinates with `pypdf.Transformation().translate(dx, dy)` — remember its origin handling differs from PyMuPDF's.

    ## Either way, validate

    Re-open the output and confirm both the new content and the old:

    ```python
    out = pymupdf.open("out.pdf")
    words = out[0].get_text("words")
    inside = [wd[4] for wd in words if pymupdf.Rect(wd[:4]).intersects(rect)]
    print(inside)                     # expect Widget/Gadget/Item/Qty/Price...
    assert "Widget" in " ".join(inside)
    ```

    Also render `out[0].get_pixmap(dpi=150)` and look at it — a fill painted over text, or a rule through a glyph, only shows up visually. And diff the pre-existing text on page 1 to prove you didn't shift or damage it.
- id: pdf-tblins-02
  answer: |
    **Short answer:** copy the geometry and typography from the table that is already on the page, paint in the order *fills → text → rules*, and place the new table against real content edges (not page margins) so nothing overlaps or doubles up.

    **1. Read the existing style before drawing anything**

    - `page.find_tables()` gives the existing table's bbox and cell grid — reuse its `x0`/`x1`, column boundaries, row pitch, and cell padding so the new table lines up pixel-for-pixel with the document's rhythm.
    - `page.get_text("dict")` on a header cell gives the exact span properties: font name, size, `color` (often `0xFFFFFF` white on a dark fill), and flags. Use those for the new header cells; use the body-row span style for body cells.
    - `page.get_drawings()` gives the header/row fill colour and the rule colour and **line width**. Copy the line width exactly — a hairline that differs from the rest of the table is the most visible giveaway.
    - If the document's font is not a base-14 font, extract it (`doc.extract_font`) and register it with `page.insert_font(...)` so glyphs and metrics match instead of silently falling back to Helvetica.

    **2. Choose the target rectangle from content, not margins**

    - `x1` = the existing table's or text block's right edge; `y1` = the top of the footer minus padding. Height = width × (source height/width) for images; for a table, derive row heights from the font size + padding you measured.
    - Before inserting, verify the rect intersects **no** word, drawing, or image bbox (`get_text("words")`, `get_drawings()`, `get_image_info()`). Overlap is what causes text-on-text and smudged rules.

    **3. Paint order (this is where most glitches come from)**

    1. Cell and header **fills** first (rectangles, same fill colour and any border radius/inset as the original).
    2. **Text** second, via `insert_textbox` with the measured font/size/colour, clipped to the cell interior so long strings either wrap or are truncated instead of bleeding over the border.
    3. **Rules** last — horizontal and vertical lines at the measured width and colour.

    A fill drawn after the text hides the glyphs in the render while leaving them in the text layer; that is the classic "invisible but selectable text" bug.

    **4. Avoid the usual visual glitches**

    - **Shared edges:** where the new table butts against an existing one, don't draw the same border twice — two 0.5 pt lines at slightly different coordinates read as a fat, fuzzy rule. Snap coordinates to the same values and draw the shared line once.
    - **Rounding drift:** compute column x-positions cumulatively and round consistently, otherwise the last column is 0.3 pt narrower and the vertical rules look wobbly.
    - **Hairlines:** use a line width ≥ ~0.5 pt with a stroke colour from the source table; zero-width strokes can vanish or alias differently across renderers.
    - **Overflow:** clamp the table's bottom to above the footer and make sure no row extends past `page.rect` or into the margin; check the rendered page, not just the text layer.
    - **Transparency/z-order:** if the source uses alpha fills, keep the same alpha and draw fills before anything that must sit on top.
    - **Replacing existing content:** if the new table occupies space where something already is, redact that region and `apply_redactions()` (with the appropriate `graphics`/`images` flags) *before* drawing — otherwise old text/rules show through under the new fills.

    **5. Verify**

    Re-extract text and drawings from the output (`get_text("dict")`, `get_drawings()`) and render the page to an image to confirm: header text colour matches, no glyph is hidden under a fill, borders are single and aligned, and nothing collides with surrounding content.
- id: pdf-tblins-03
  answer: |
    There is no way to just draw the table there — the paragraphs occupy that space, and a table drawn on top would overlap them visually while leaving both texts in the text layer. So the real options all involve either making room or changing where the table lives:

    **1. Make room by shifting the content below down (most common).**
    - Measure the needed height: header row + body rows + cell padding + a small gap above and below.
    - Measure the actual gap between the two paragraphs.
    - `delta = needed − gap`. If `delta > 0`, everything from the bottom of paragraph 1 downward must move down by `delta`.
    - Implement by redacting that lower region on the page, `apply_redactions()`, then redrawing it at `y + delta` — either by re-stamping from a copy in which the same region was also redacted (so the old content isn't smuggled along in the XObject), or by re-rendering the extracted text/blocks at the new coordinates.
    - Then draw the table in the freed band, in the order fills → text → rules.

    **2. Push the table (and possibly paragraph 2) to the next page.**
    - If shifting down would collide with the footer or run past the page bottom, don't squeeze. Insert a page break: keep paragraph 1 where it is, put the table at the top of a new page, and move paragraph 2 after it.
    - This can cascade: if paragraph 2 no longer fits on the new page after the table, it continues onto the following page, and so on. Each moved block has to be removed from its old page, not just covered.

    **3. Rebuild the affected page(s) from extracted content.**
    - When the layout is dense or the shift ripples through several blocks, extract the text with positions/fonts/styles, discard the old page content for the affected region (or the whole page), and re-lay it out programmatically around the table. Most robust, most work, and you must preserve fonts, sizes, colours and leading to avoid a visible mismatch.

    **4. Regenerate the PDF from its source.**
    - If you have the HTML/DOCX/Markdown/LaTeX original, insert the table there and re-export. Best fidelity by far; only viable when the source exists.

    **5. Avoid the reflow entirely by relocating or reshaping the table.**
    - Place it before paragraph 1 or after paragraph 2 if the document's meaning allows it. Often the cheapest correct answer.
    - Or reduce the table to fit the existing whitespace: smaller font, tighter padding, fewer/narrower columns, landscape or rotated table, or an appendix/footnote reference. Legitimate when "no gap" really means "a few points of gap" and the table can be compressed — but if the gap is truly zero you still need at least a minimal shift or a page break.

    **What not to do:** drawing the table over the paragraphs, or shrinking it until it is unreadable. Also don't stamp the shifted region from an unredacted copy — the clip only hides the old content; it stays in the file and in the page's resources.

    **Practical decision rule:** compute `available_gap` and `required_height`. If `required_height ≤ available_gap`, just draw. If it exceeds the gap but the shift still fits above the footer, do option 1. If it doesn't fit, do option 2 (page break/reflow) or option 4/5.
- id: pdf-imgrep-01
  answer: |
    ## Short answer

    Don't re-draw the logo. Find the image XObject's `xref` and swap its stream in place — `Page.replace_image()` does exactly that, and because the page's content stream (`cm … /ImN Do`) is untouched, the new PNG lands in the identical rectangle.

    The catch: an image XObject is **document-level**, not page-level. The same `xref` is routinely shared by several pages, by the same page twice, or by a Form XObject (a header/watermark stamped everywhere). `replace_image()` is global: it changes **every** reference. If you only want page 1 changed, you must not touch the shared object.

    ## 1. Find the logo and its placement

    ```python
    import fitz  # PyMuPDF

    doc  = fitz.open("in.pdf")
    page = doc[0]

    # (xref, smask, w, h, bpc, colorspace, alt_cs, name, filter, referencer)
    for img in page.get_images(full=True):
        print(img)

    # per-occurrence position/transform on this page
    for info in page.get_image_info(xrefs=True):
        print(info["xref"], info["bbox"], info["width"], info["height"])

    logo = page.get_images(full=True)[0]   # pick the right one
    xref, name = logo[0], logo[7]
    rect = page.get_image_bbox(logo)       # where it actually sits
    ```

    ## 2a. Whole-document swap (simplest, when the image is only the logo)

    ```python
    page.replace_image(xref, filename="new_logo.png")
    doc.save("out.pdf", garbage=4, deflate=True)
    ```

    Signature: `replace_image(xref, filename=None, pixmap=None, stream=None)` (PyMuPDF ≥ 1.21). It works on the xref itself, so:

    - position and size are preserved automatically — the existing `cm` matrix governs the displayed rectangle, not the PNG's pixel dimensions;
    - therefore the new PNG is **stretched into the old box**. Match the aspect ratio (ideally the old pixel `w × h`, or resample with PIL) or it will be distorted. Some versions are strict about identical dimensions; resampling first avoids that;
    - alpha is handled by creating an `SMask`, but if the *old* image carried an `SMask`, check `doc.xref_get_key(xref, "SMask")` — a stale mask can clip the new art;
    - orphaned image bytes are only reclaimed on save with `garbage=4` (add `clean=True` if content streams need tidying).

    ## 2b. Per-page swap (copy-on-write resource remap)

    This is the correct route when the xref is shared and you want only page 1 changed. The trick: mint a **new** image object, then repoint only page 1's resource *name* at it. The content stream still says `/Im0 Do` with the same `cm`, so placement is byte-identical.

    ```python
    # mint a new image xref on a throw-away page
    scratch = doc.new_page(width=10, height=10)
    scratch.insert_image(fitz.Rect(0, 0, 1, 1), filename="new_logo.png")
    new_xref = scratch.get_images(full=True)[0][0]
    doc.delete_page(scratch.number)

    # re-point only this page's /XObject entry
    doc.xref_set_key(page.xref, f"Resources/XObject/{name}", f"{new_xref} 0 R")
    doc.save("out.pdf", garbage=4, deflate=True)
    ```

    Caveats:
    - If the page's `/Resources` is an indirect object shared with other pages, editing it leaks to them. Check `doc.xref_get_key(page.xref, "Resources")`; if it's `xref`, clone it (`doc.xref_set_key(page.xref, "Resources", f"{new} 0 R")`) first.
    - If the logo is painted by a Form XObject, the name lives in that XObject's own resource dictionary — edit that `xref`, not the page.
    - `Page.insert_image()` deduplicates: it may return an existing xref if the same image bytes are already in the file, which defeats the copy-on-write plan. Use a genuinely different file, or build the XObject yourself with `doc.add_stream` + an image dict.

    ## 3. The lazy alternative (and why it's not a replacement)

    ```python
    page.insert_image(rect, filename="new_logo.png", overlay=True)
    ```

    Hits the right spot, but it only paints over. The old image stays in the file and is still extractable (`get_images`, `extract_image`); with a transparent PNG the old logo ghosts through; the file grows. Fine for a visual fix, not for redaction-grade replacement.

    ## The catch with shared images — in detail

    1. **One object, many references.** `xref` is a document object referenced by name from resource dictionaries. `replace_image()` mutates the object, so:
       - two occurrences on page 1 (e.g. logo + a small mark) both change;
       - the same logo on pages 2–10 all change;
       - a header/watermark inside a Form XObject used by every page changes everywhere.
    2. **Diagnose before you write.** Count references before deciding:

    ```python
    from collections import Counter
    c = Counter()
    for p in doc:
        for im in p.get_images(full=True):
            c[im[0]] += 1
        for info in p.get_image_info(xrefs=True):
            c[info["xref"]] += 1        # occurrences, not just pages
    print(c)                            # c[xref] > 1 ⇒ shared
    ```

       Also check `doc.xref_get_key(xref, "SMask")` and whether the xref appears inside `/XObject` Form dicts (`doc.xref_get_keys`), not just on pages.
    3. **Decision rule.** Shared + you want all instances changed → `replace_image()` is ideal (one call, every page correct, each occurrence keeps its own scale). Shared + you want only page 1 → copy-on-write remap (2b). Do **not** reach for redaction here: `apply_redactions(images=PDF_REDACT_IMAGE_REMOVE)` removes the occurrence from that page's content, but if the object is shared you can end up blanking pixels that other pages also draw (the pixel-blanking mode is the risky one) — verify the other pages afterwards if you go that way.
    4. **"Same size" is a property of the content stream, not the image.** Replacing the stream keeps the size; inserting a new image or editing the `cm` changes it. So use `replace_image` or the remap for same-place/same-size, and only rebuild the `cm` if you actually want a different box.
    5. **Verify.** Render `page.get_pixmap(dpi=150)` and eyeball page 1, re-run `get_images` on page 1 and page 2 (page 2 must still point at the old xref), and confirm the file shrank or the orphan is gone after `garbage=4`.

    ---

    ### Recap

    **ASKED** — How to replace page-1 logo in place with a new PNG, and the shared-image pitfall.

    **BUILT** — Two working routes: `page.replace_image(xref, filename=…)` for a document-wide swap, and a mint-new-xref + `xref_set_key("Resources/XObject/<name>")` copy-on-write remap for a page-1-only swap. Plus bbox/xref discovery and a reference counter.

    **FOUND** — Placement/size lives in the content stream's `cm`, not in the image object, so swapping the stream is inherently same-position/same-size — and a differing aspect ratio gets stretched. Image xrefs are shared at document level via resource dicts and Form XObjects, so `replace_image` is global by construction, not a per-page edit.

    **DECIDED** — Recommend `replace_image` when all references should change; recommend the resource remap whenever the xref is shared but only page 1 should move. Treat `insert_image` overlay as a cosmetic non-replacement and redaction as the risky path for shared objects. BOTTOM LINE: swap the XObject, not the pixels on top — and check the reference count before you do.

    **NEXT** — If you need the actual script run against a real PDF, point me at the file so xref discovery, remap, and a render check can be validated; otherwise verify aspect ratio and `SMask` on the new PNG before shipping, since those are the two silent-failure modes.
- id: pdf-imgrep-02
  answer: |
    **The old logo is still there.** `page.insert_image(rect, ...)` only *adds* a new image object to the page — it does not delete, mask, or replace the pixels underneath. You have stacked two images, and the visual result hides the problem completely.

    What that actually means:

    - **The old image is still in the content stream and still in the file.** `page.get_images()` / `page.get_image_info()` will list both, and the old one can be extracted byte-for-byte with `doc.extract_image(xref)` — a "logo replacement" that leaks the original logo is not a replacement.
    - **Layering is fragile.** If the new image has alpha, a soft mask, or a slightly different aspect ratio, the old logo bleeds through or peeks out at the edges. Re-rendering at a different DPI, printing, or downsampling can also change how the stack composites.
    - **The file grows:** both images are carried, and the old one may keep its full-size pixel data.
    - **Clipping is not removal.** Same trap as `show_pdf_page(clip=...)`: hiding is not deleting.

    To genuinely replace it, remove the old image first:

    1. Locate the old image's bbox (from `get_image_info()`), then draw a redaction rect over exactly that region.
    2. `page.apply_redactions(images=PDF_REDACT_IMAGE_REMOVE)` — `REMOVE` drops the image object entirely (prefer this over `PIXELS`, which only blanks the overlapping pixels). Keep `graphics`/`text` at defaults only if you intend to redraw anything the rect covers; pass `graphics=PDF_REDACT_LINE_ART_NONE, text=PDF_REDACT_TEXT_NONE` when the logo sits on a rule or near text you need to keep.
    3. Then `insert_image` the new one.
    4. Save with `garbage=4, deflate=True`.
    5. Verify: the old image xref is gone from `get_images()`, `extract_image` on it fails, and the page render still looks right.

    So: a correct-looking page is not evidence of a correct edit. "Looks right" only proves the top layer is opaque.
- id: pdf-imgrep-03
  answer: |
    ## Why `get_images()` comes up empty while the logo still renders

    `page.get_images()` only reports **raster image XObjects referenced directly from the page's own `/Resources`**. It does not walk everything that can put pixels on the page, and it does not prove an image isn't drawn. Typical reasons the logo is invisible to it:

    1. **The image lives inside a Form XObject.** A page stamped with `show_pdf_page()`, an imported header/watermark, an Office/InDesign export, or any assembled PDF often has the page content wrapped in `/Fm0 Do`, with the real image in the form's own `/Resources/XObject`. `get_images()` looks at the page dict, not the form's dict.
    2. **It is an inline image.** `BI … ID … EI` in the content stream has no xref and no resource entry at all, so it can never appear in `get_images()`.
    3. **It is an annotation appearance.** Stamp, watermark, FreeText and widget annotations live in `/Annots`, not in the page content. Their appearance streams (which may contain an image) are ignored by `get_images()` and are not removable by redaction.
    4. **It is not a raster image.** Vector paths (`get_drawings()`), a Type3/icon-font glyph, a tiling pattern that draws an image in the pattern's resources, or a shading.
    5. **The page you inspected isn't the page being shown** — wrong index, or an optional-content/overlay situation.
    6. Conversely, `get_images()` can also list images that are in the resources but never painted, so its output is not authoritative in either direction.

    ## Finding it

    Start with the method that reports *drawn* images, including those inside forms:

    ```python
    infos = page.get_image_info(xrefs=True)   # older versions: hashes=True
    for i in infos:
        print(i["xref"], i["bbox"], i["width"], i["height"], i.get("digest"))
    ```

    - `xref == 0` → inline image (repaint/redaction only; no object to edit).
    - The `bbox` is the placement rectangle you need for replacement.
    - `digest` lets you match an entry against the tuples from `get_images(full=True)` and spot duplicates.
    - If the image is nested, `get_image_info(xrefs=True)` still gives you the leaf image's xref (that's its main advantage over `get_images()`).

    If `get_image_info()` is also empty, check in this order:

    ```python
    page.get_bboxlog()          # low-level draw ops: ("fill-image", rect), ("fill-path", ...), etc.
    list(page.annots())         # stamp / watermark / widget appearance?
    page.get_xobjects()         # form XObjects placed on the page: (xref, name, invoker, bbox)
    page.get_drawings()         # vector logo?
    page.get_text("dict")       # type-1 blocks carry width/height/xref/"image"
    ```

    A fast discriminator for annotations: render with and without them — if the logo vanishes when `annots=False`, it's an annotation, not page content.

    ```python
    page.get_pixmap(clip=rect, annots=False).save("noannots.png")
    ```

    To reach the xref of an image buried in a form, walk the resource chain: take each form from `get_xobjects()`, scan its stream for `/(\w+)\s+Do`, resolve `doc.xref_get_key(form_xref, f"Resources/XObject/{name}")`, and recurse when the target's `/Subtype` is `/Form`; an `/Image` subtype is the leaf. `doc.extract_image(xref)` confirms it's the logo.

    Watch page rotation: with `page.rotation != 0`, verify the bbox by rendering `page.get_pixmap(clip=rect)` before you rely on it, and transform with `page.rotation_matrix` if the render doesn't line up.

    ## Replacing it

    **Default route — redact the old pixels, draw the new logo:**

    ```python
    rect = fitz.Rect(info["bbox"])
    page.add_redact_annot(rect)
    page.apply_redactions(
        images=fitz.PDF_REDACT_IMAGE_REMOVE,        # not the PIXELS default
        graphics=fitz.PDF_REDACT_LINE_ART_NONE,     # keep grid/rules unless the logo is vector
        text=fitz.PDF_REDACT_TEXT_NONE,
    )
    page.insert_image(rect, filename="logo.png", keep_proportion=True, overlay=True)
    doc.save("out.pdf", garbage=4, deflate=True)
    ```

    Notes that matter:

    - `PDF_REDACT_IMAGE_REMOVE` deletes the image object. The default `PDF_REDACT_IMAGE_PIXELS` only blanks the overlapping pixels, so the old image object and its bytes survive — easy to mistake for a replacement when your new logo covers the region.
    - If the logo is **vector**, set `graphics=fitz.PDF_REDACT_LINE_ART_REMOVE_IF_TOUCHED` on a rect tight around the logo only: that setting deletes an entire path that touches the rect, which can take out table rules if the rect is padded.
    - If the logo is a **glyph**, redact with `text=PDF_REDACT_TEXT_REMOVE, graphics=0, images=0` (keeps cell shading/borders), then insert the image.
    - If it is an **annotation**, redaction won't touch it: `page.delete_annot(annot)` and insert the image at `annot.rect`, or rewrite the `/AP /N` appearance stream.
    - If the image sits in a **form shared by several pages**, redaction may either miss it or change it everywhere. The surgical alternative is to replace the leaf image object itself: `doc.update_stream(img_xref, raw_rgb, new=True)` plus matching `/Width`, `/Height`, `/ColorSpace /DeviceRGB`, `/BitsPerComponent 8`, `/Filter /FlateDecode`; remove any inherited `/SMask` (and `DecodeParms`) or the old alpha mask will clip your new logo. Keep the same pixel dimensions/aspect or the placement will distort.
    - If the logo came from `show_pdf_page()` and you want the stamp gone entirely, removing the `/FmN Do` invocation (with its preceding `q … cm`) from the content stream is the real removal; covering it with a white rect only hides it.

    **Verify, don't assume:** reopen the output and check that `page.get_image_info(xrefs=True)` shows exactly the new image at `rect`, that the old xref no longer appears anywhere (including other pages sharing the form), and that `page.get_pixmap(clip=rect)` matches the new logo. Saving with `garbage=4` then drops the orphaned old objects.
- id: pdf-imgins-01
  answer: |
    ## Geometry first

    PDF units are points, and PyMuPDF's `page.rect` uses top-left origin with **y growing downward**, so "bottom" is `page.rect.y1`.

    - Width is fixed: `w = 150`.
    - Aspect ratio comes from the pixels, not from the file's DPI: `h = 150 * pix.height / pix.width`.
    - Anchor the bottom-right corner with a margin, then walk the rect upward until it hits nothing.

    ```python
    import pymupdf

    def place_signature(doc, png, page_no=0, width=150, margin=36, pad=4, gap=6):
        page = doc[page_no]
        pr = page.rect                      # bottom = pr.y1

        # pixel aspect ratio (page units scale uniformly, so pixels are fine)
        pix = pymupdf.Pixmap(png)           # or PIL: Image.open(png).size
        h = width * pix.height / pix.width

        # obstacles: text words, existing images, vector line art/rules
        obs  = [pymupdf.Rect(w[:4]) for w in page.get_text("words")]
        obs += [pymupdf.Rect(i["bbox"]) for i in page.get_image_info()]
        obs += [d["rect"] for d in page.get_drawings()]
        # pad so the image does not touch glyph ink / a footer rule
        obs = [pymupdf.Rect(o.x0-pad, o.y0-pad, o.x1+pad, o.y1+pad) for o in obs]

        x1 = pr.x1 - margin
        y1 = pr.y1 - margin
        r = pymupdf.Rect(x1 - width, y1 - h, x1, y1)   # exact aspect: no letterboxing

        # slide upward in `gap` steps until the rect is clear
        while r.y0 > pr.y0 + margin:
            if not any(not (r & o).is_empty for o in obs):
                break
            r = pymupdf.Rect(r.x0, r.y0 - gap, r.x1, r.y1 - gap)
        else:
            raise ValueError("no free space for the signature")

        page.insert_image(r, filename=png, keep_proportion=True, overlay=True)
        return r
    ```

    Why the details matter:

    - `keep_proportion=True` fits the image *inside* the rect, centred, with no distortion. Since the rect was computed with the exact pixel ratio, it fills the rect exactly and never letterboxes.
    - `(r & o).is_empty` tests real overlap; touching edges count as empty, so the `pad` is what supplies visual breathing room.
    - `overlay=True` (the default) puts the image above existing content. That is safe **because the rect is already clear**; `overlay=False` is not a substitute, since a white footer/background fill can hide the signature instead.
    - If `y0` runs past the top margin, fall back to shrinking `width` or moving left along the bottom band — do not silently let it overlap.

    ## Better anchoring than a fixed margin

    The fixed `margin` is only the starting corner. Per the content-anchored rule:

    - `x1` = the right edge of the actual content in that band (table/text `x1`), not `page.rect.x1 - 36` blindly.
    - `y1` = the top of the footer block minus padding, not `page.rect.y1 - 36`.

    ```python
    # right edge of content in the lower quarter of the page
    lower = pr.y1 - 0.25 * pr.height
    content = [pymupdf.Rect(b[:4]) for b in page.get_text("blocks") if b[3] > lower]
    x1 = max([c.x1 for c in content], default=pr.x1 - margin)

    # footer top = highest y0 among bottom-band blocks; place above it
    footer = [pymupdf.Rect(b[:4]) for b in page.get_text("blocks")
              if b[1] > pr.y0 + 0.85 * pr.height]
    y1 = (min(f.y0 for f in footer) - gap) if footer else pr.y1 - margin
    ```

    Also reserve any existing images and drawings in that band; footer rules and shading rects count as collisions.

    ## Insert, then verify

    ```python
    r = place_signature(doc, "sig.png")
    print(page.get_image_info())          # bbox should equal r (rounded to 0.01)
    doc.save("out.pdf", garbage=4, deflate=True)
    ```

    Checks worth doing:

    1. Re-extract `page.get_text("words")` after insertion and confirm nothing was clipped — `insert_image` never removes text, so a missing word means the rect decision, not the insert, was wrong.
    2. Re-open the saved file, compare `page.get_image_info()[0]["bbox"]` to `r`, and confirm `h/w == pix.height/pix.width` within float error.
    3. Render at 150 dpi (`page.get_pixmap(dpi=150)`) and eyeball the bottom-right corner: image fully inside the page, no glyph ink under the PNG's transparent or dark area.
    4. Confirm the PNG's alpha became an SMask (`get_image_info(xrefs=True)` / `get_images(full=True)` shows the smask xref); otherwise a transparent signature will render on a black or white block.

    ## Caveats

    - **Rotated page** (`page.rotation != 0`): `page.rect` is in displayed orientation, while the content-stream coordinates used by `insert_image` are the unrotated ones. Convert with `r * page.derotation_matrix` before inserting (and convert the obstacle rects the same way, or normalize by setting `page.set_rotation(0)` on a working copy), then verify with a render.
    - **CropBox offset**: use `page.rect` (PyMuPDF normalizes the visible page rectangle), not raw `/MediaBox` values from the PDF dictionary.
    - **Big transparent PNG**: `Pixmap(png)` reads only the header for dimensions, so this is cheap; but embedding a 4000-px image to draw 150 pt is wasteful — downsample to ~300 dpi of the target size (≈625 px wide at 150 pt) before inserting.
    - If the page genuinely has no free bottom-right area, prefer moving the signature into the nearest clear band (above the footer, or left of the signature line) over reducing it below ~2 cm wide or covering text.

    ASSUMPTIONS: portrait single-page target not rotated, `page.rect` top-left origin in points, no prior signature, and "bottom right" means the content's bottom-right, not the paper corner.
- id: pdf-imgins-02
  answer: |
    **Short answer:** stop inserting the pixels 200 times. Put the logo in the file **once** as an image object and have every page *reference* it, instead of asking `Page.insert_image` to encode a new image stream per page.

    ### Why it blows up
    `page.insert_image()` is a page-level call: each invocation creates a new image XObject (a new `/Subtype /Image` stream with its own encoded bytes) plus a new `/Do` in that page's content. Identical bytes are not deduplicated — a 400 KB PNG × 200 pages ≈ 80 MB of the same logo. That's also 200 decodes when a viewer renders.

    ### Ways to avoid it, most idiomatic first

    1. **Document-level insert (if your PyMuPDF version has it).** `Document.insert_image(rect, filename=...)` is meant exactly for "same image on every page"; in versions that implement it properly the image object is stored once and only referenced per page. Verify, don't assume — count image xrefs afterwards (below). If your version is just a loop over `Page.insert_image`, fall through to 2 or 3.

    2. **One-page template + `show_pdf_page`.** Create a tiny `logo.pdf` with the image inserted **once**, then on each target page:
       ```python
       page.show_pdf_page(rect, logo_doc, 0)   # logo_doc opened once, reused
       ```
       Each page gets a small Form XObject whose resources point at the one image. This also lets you scale/position without touching pixels. Caveat: `show_pdf_page` embeds the *source page* as an XObject per call, so check the image count; if the graft copies the image each time in your version, use 3.

    3. **Guaranteed: share the resource dict manually.** Insert once on page 0, read that image's xref and resource name, then add the same xref to every other page's `/Resources /XObject` under the same name and append a draw operator to each content stream:
       ```
       q  W 0 0 H  x y  cm  /Logo Do  Q
       ```
       (set the resource via `doc.xref_set_key(page.xref, "Resources/XObject", ...)` or `xref_get_key`/`xref_set_key`). Result: exactly **one** image object in the file, 200 tiny `/Do` references. This is the only approach that cannot silently duplicate, so it's the safe fallback.

    ### Also do
    - **Downsample to placed size first.** A logo displayed at 60×60 pt doesn't need 2000 px. Resize/flatten to RGB (or gray), encode once (JPEG for photos, Flate/PNG for flat logo art), then share that single stream. Size of the one stored image dominates the file once dedup is fixed.
    - Keep the source `pixmap`/`stream`/`Document` object alive and reused — re-reading and re-encoding the file per page defeats any sharing.
    - Save with `garbage=4, deflate=True` (and `clean=True` if content streams are messy) to drop leftovers; note GC removes unreferenced objects, it does **not** merge duplicates — prevention is the fix.

    ### Verify, don't trust file size
    Count image objects and references:
    ```python
    imgs = [x for x in range(1, doc.xref_length())
            if doc.xref_get_key(x, "Subtype") == ("name", "/Image")]
    ```
    Expect ~1 (plus any pre-existing images), and every page's `/XObject` should name that same xref. If the count grows with page count, you're still copying.

    **Honest uncertainty:** I'm confident about the mechanism and the low-level sharing recipe (option 3); I'm less certain which PyMuPDF versions make `Document.insert_image` and `show_pdf_page` truly dedupe rather than graft-per-call — hence the xref-count check rather than a claim.
- id: pdf-imgins-03
  answer: |
    Answering from knowledge only (no files, no web). Assumption: PyMuPDF/`fitz`, one document, normal unencrypted PDF.

    ## 1. Paint order — the "underneath the box" case

    PDF content is painted in stream order; later operators cover earlier ones.

    - `page.insert_image(..., overlay=False)` puts the image at the **start** of the content stream, so every existing fill — a full-page white rectangle, table cell shading, a `draw_rect(..., fill=...)` — paints over it. `overlay=True` (the default) appends, so it lands on top.
    - You drew the background **after** inserting: `page.draw_rect(..., fill=...)`, a `Shape` committed with `overlay=True`, or a table redraw. Fix: draw fills first, image last, or re-insert it.
    - Same trap with `show_pdf_page(..., overlay=False)` for a stamp.
    - A form widget or annotation with a white fill, drawn later, will also sit on top.

    ## 2. Geometry — the "doesn't appear at all" case

    - **Coordinate flip.** PyMuPDF uses a top-left origin, y increasing downward. PDF-native bottom-left coordinates (from another lib, or raw content-stream values) put the image at the mirrored y — often off the visible area or behind something.
    - **Off-page / clipped.** A rect outside `page.rect` (or the CropBox) is clipped away. Remember `page.rect` follows the **CropBox**, which may be offset from the MediaBox; mixing MediaBox coords with `insert_image` shifts everything.
    - **Bad rect.** Zero/negative width or height, or pixel dimensions passed as points (a 1200×800 px image becomes 1200×800 pt and lands mostly off-page). Normalize with `keep_proportion=True` and an explicit target rect.
    - **`/Rotate` pages.** On a rotated page the rect may be applied before the derotation, so the image lands off the visible area or sideways; test `page.rotation` and `page.derotation_matrix`.
    - **`show_pdf_page` with `clip`** entirely outside the source page yields a blank stamp; `clip` only sets visibility, not content.

    ## 3. The image data itself

    - PNG/Pixmap whose **alpha channel is all zero**, or a broken/inverted `/SMask`, renders as nothing. Check the Pixmap's `alpha`, and try a JPEG or `alpha=0` pixmap to isolate it.
    - **Stream at EOF.** Passing `stream=f` where `f` was just written and never `seek(0)`ed, or a closed `BytesIO`, gives an unusable image.
    - Corrupt bytes, unsupported colorspace/component count, or a `mask=` argument that doesn't match the samples.
    - Image xref present but with zero width/height.

    ## 4. Redaction steps after insertion

    `apply_redactions()` defaults to `images=PDF_REDACT_IMAGE_PIXELS` (2), which **blanks image pixels** under any redact annotation on the page. An image inserted before a redaction pass that overlaps a redact rect silently disappears. Either insert after redaction, or use `images=PDF_REDACT_IMAGE_NONE` if that is genuinely intended.

    ## 5. It was never inserted / never saved

    - Edited `doc[n]` on a copy, then saved the original `doc`.
    - `doc.save()` called before `insert_image`, or saved to a different path than the one you opened.
    - Saving over the source without `incremental=True, encryption=...` — PyMuPDF refuses or you inspect the pre-edit file.
    - Inserted on page 0 while looking at another page.

    ## Fast triage

    ```python
    print(page.rect, page.mediabox, page.cropbox, page.rotation)
    print(page.get_images(full=True))        # in /Resources
    print(page.get_image_info(xrefs=True))    # actually painted, with bbox
    page.get_pixmap(dpi=100).save("check.png")
    ```

    - xref listed and `get_image_info` shows a sensible bbox → **z-order or alpha** (cause 1 or 3).
    - xref listed but no image-info entry or a bad bbox → **geometry/clip** (cause 2).
    - Nothing listed → **insertion or save** never happened (cause 5), or a redaction wiped it (cause 4).

    Fix in practice: keep `overlay=True`, insert images **after** all fills and redactions, build the rect from `page.rect`/content bboxes in PyMuPDF's top-left coordinates, and verify with `get_image_info()` plus a rendered pixmap before saving (`garbage=4, deflate=True`).
- id: pdf-fonts-01
  answer: |
    ## Why reusing the embedded subset fails

    The `AAAAAA+` prefix is the tell: that font is a **subset**, not the real Georgia. The producer kept only the glyphs the document actually used (often plus a couple of extras), and threw the rest away. So:

    - **No outlines for new characters.** The glyph program (`glyf`/`CFF` table) contains only the used shapes. A letter that never appeared has no glyph to draw — you get `.notdef`: blank space, a box, or a fallback shape. The text may still "exist" in the content stream but render as nothing.
    - **No usable character mapping.** Subset fonts are typically Type0/CIDFont with `Identity-H` encoding, where the bytes in the string are **glyph IDs assigned by the subsetter**, and `ToUnicode` only maps the characters that were used. There is no general Unicode→GID table for letters that were dropped, so you cannot even address a new character correctly. This is the core reason "just reuse the font resource" doesn't work.
    - **Missing metrics.** The `/W` widths array only covers included glyphs. New glyphs get zero or wrong advance widths, so spacing and positioning break even if a shape somehow appeared.
    - **You often can't reference it by name anyway.** PyMuPDF's `insert_text`/`insert_htmlbox` need a font it can address as a real font; an embedded CID subset is not a reusable font program. Attempts to pass the font name from `get_fonts()` usually fail or silently produce blanks.
    - **Stripped metadata.** Subsets frequently have no `post` glyph names and a trimmed `cmap`, so tools cannot reconstruct which glyph is which.

    Also consider the font's `fsType` embedding-permission flags — some fonts forbid reuse/derivation — but glyph coverage is the practical blocker.

    ## What you should do

    **Embed a fresh, complete font of the same family and draw the new text with it**, matching the original span's size, colour, and baseline. In PyMuPDF:

    ```python
    page.insert_font(fontname="georgia_new", fontfile="/path/to/Georgia.ttf")
    page.insert_text((x, y_baseline), new_name,
                     fontname="georgia_new",
                     fontsize=span_size,
                     color=span_color)   # e.g. (0,0,0) or a float RGB triple
    ```

    For HTML/`Story` paths, register the full font in an `Archive` and declare it with `@font-face` in the CSS.

    Practical notes:

    1. **Read the style from the original span** via `page.get_text("dict")` — font name, size, colour, origin — so the replacement is visually seamless. Don't guess the size.
    2. **Redact the old glyphs tightly** with `page.apply_redactions(graphics=0, images=0)` so the table grid and shading survive; then draw the new text.
    3. **If an exact typeface match is unavailable,** either use the closest metric-compatible font, or fall back to a base-14 font (Helvetica/Times) when the cell won't be scrutinised — no embedding needed, but expect a visible mismatch.
    4. **Do not try to hand-extend the subset with fontTools** unless you have the original full font; it's far more work than re-embedding.
    5. **Subset afterwards, not before.** Embed the full font, draw all your text, then call `doc.subset_fonts()` at the end so the new font is trimmed to the glyphs you actually used. That keeps file size down and is safe because the subsetter sees every glyph you drew.
    6. **Verify**: re-render the page to a pixmap and inspect for blanks/tofu, confirm `page.get_text()` returns the new string, and re-run `page.get_fonts()` to check the new font is embedded. A text-layer check alone is not enough — a `.notdef` glyph can still extract as the right character.

    Short version: a subset is a **glyph inventory, not a font**. New letters aren't in it, and its CID encoding can't address them. Re-embed the complete font (then re-subset on save) and redraw the text in the original style.
- id: pdf-fonts-02
  answer: |
    **Base-14 names**

    PyMuPDF uses the short PostScript-ish codes for the built-in base-14 fonts:

    - Helvetica regular → `"helv"`
    - Helvetica bold → `"hebo"`

    (The rest of the family: `"heit"` = Helvetica-Oblique, `"hebi"` = Helvetica-BoldOblique. Full names such as `"Helvetica"` / `"Helvetica-Bold"` are accepted as aliases in some APIs, but the canonical built-in identifiers are the four-letter codes.)

    **Limitation for non-Latin / special characters**

    Base-14 fonts are single-byte, non-embedded fonts restricted to a legacy encoding (StandardEncoding / WinAnsiEncoding, or the Symbol/ZapfDingbats repertoires). That means:

    - They only cover the Latin/WinAnsi repertoire (ASCII plus a limited set of Latin-1 and typographic characters such as `é`, `—`, `€` depending on encoding).
    - Non-Latin scripts — Cyrillic, Greek (outside the few Symbol glyphs), Arabic, Hebrew, Devanagari, CJK, emoji — have no glyphs at all. Unsupported characters are dropped, rendered as `.notdef`/blank/`?`, or otherwise not represented; text may also come back wrong on extraction/copy-paste because there is no reliable Unicode mapping for out-of-encoding characters.
    - Because base-14 fonts are referenced, not embedded, the actual glyph shapes and metrics depend on the viewer's substitute font, so appearance is not guaranteed across viewers.

    Workaround: embed a real TTF/OTF with `insert_font(fontname=..., fontfile=...)` or a `Font(fontfile=...)`/`Font("notos")` from the `pymupdf-fonts` package (Noto/Fira, plus CJK fonts) and write with that `Font` object — embedded fonts support full Unicode and give consistent metrics and extraction.

    Caveat: the exact failure mode for an out-of-encoding character depends on the call path (silent skip vs. replacement glyph), but in all cases a base-14 font cannot faithfully render non-Latin text.
- id: pdf-fonts-03
  answer: |
    ## What actually went wrong

    Two separate defaults cause this, not one:

    1. **Weight** — `insert_text`/`insert_textbox` default to `fontname="helv"` (Helvetica *Regular*). If you don't pass a font, the new cell is regular no matter what the original was. It is not "inheriting" anything.
    2. **Size** — default `fontsize=11`. Worse, passing `fontsize=0` makes `insert_textbox` *auto-fit* the text to the box, which produces exactly the "slightly different size" symptom. Also a default: `color=(0,0,0)`, which silently turns white-on-dark header text black.

    So the fix is: read the original span, then pass font, size, colour and baseline explicitly.

    ## Detect the original

    `page.get_text("dict")` → pick the span whose bbox covers the header cell:

    ```python
    d = page.get_text("dict")
    for b in d["blocks"]:
        for l in b.get("lines", []):
            for s in l["spans"]:
                if header_rect.contains(fitz.Rect(s["bbox"])):
                    print(s["font"], s["size"], s["flags"], hex(s["color"]),
                          s["origin"], s["ascender"], s["descender"])
    ```

    - `s["font"]` — e.g. `"Helvetica-Bold"`, `"Arial,Bold"`, or a subset tag `"ABCDEF+Inter-Bold"`. Strip the six-letter prefix plus `+` to get the real base name.
    - `s["flags"]` — bit 16 = bold, bit 2 = italic, bit 4 = serifed, bit 8 = monospaced (`flags & 16`). Treat this as a hint, not gospel: some producers lie. Cross-check the basefont name for `Bold|Black|Heavy|Semi|Demi|Medium`. If name and flags disagree, trust the name and confirm with a stem-width measurement on a high-DPI render.
    - `s["size"]` — the font size in points. If the original looks bigger than the number suggests, the page may be applying horizontal scaling (`Tz`) or a scaled text matrix; get glyph-level truth from `page.get_texttrace()`, which reports per-span `size`, `origin`, `color` and `dir` directly.
    - `s["color"]` — sRGB integer. Convert for drawing: `fitz.sRGB_to_pdf(s["color"])` → `(r,g,b)` floats. Never assume black.
    - `s["origin"]` — the baseline start point. `insert_text(point, …)` also takes a baseline origin, so pass this verbatim; don't reconstruct it from the bbox top.

    For the actual embedded font, use the resources, not the display name:

    ```python
    for xref, ext, typ, basefont, name, enc in page.get_fonts(full=True):
        print(xref, ext, typ, basefont, name, enc)
    name, ext, typ, buf = doc.extract_font(xref)   # buf = the real font program
    ```

    ## Match it

    **Base-14 originals** — map the basefont to PyMuPDF's built-in aliases: `helv`/`hebo` (Helvetica regular/bold), `heit`/`hebi`, `tiro`/`tibo` (Times), `cour`/`cobo` (Courier), `symb`, `zadb`. `hebo` is the fix for a bold Helvetica header.

    **Embedded originals** — reuse the embedded program so glyph widths and weight match exactly:

    ```python
    tw = fitz.TextWriter(page.rect, color=fitz.sRGB_to_pdf(s["color"]))
    font = fitz.Font(fontbuffer=buf)          # from doc.extract_font
    assert font.has_glyph(ord("H"))           # subset may lack glyphs
    tw.append(s["origin"], new_text, font=font, fontsize=s["size"])
    tw.write_text(page)
    ```

    Or register once and use `insert_text`: `page.insert_font(fontname="F0", fontbuffer=buf)` then `page.insert_text(s["origin"], new_text, fontname="F0", fontsize=s["size"], color=…)`.

    Rules: pass `fontsize=s["size"]` exactly; never `0`; don't use `insert_textbox` for a single cell (it wraps/auto-fits); keep the rect tight and inside the cell; redact with `graphics=0, images=0` so you don't delete the header fill.

    ## Verify

    Re-extract the dict for that cell and compare against the values you captured — `font` base name, `size` within ~0.1 pt, `flags & 16`, `color`, and bbox width/height within ~1 pt. Then render at ~300 dpi and compare ink coverage / stroke thickness against a neighbouring untouched header cell; a size or weight miss shows up as a bbox-width delta long before it's visible to the eye. If the check fails, it's almost always the default `helv`, a default 11 pt, `fontsize=0`, or a colour that defaulted to black.
- id: pdf-verify-01
  answer: |
    Verification has to be done on the **saved output file**, reopened from disk — not on the in-memory document you just modified. Do at least the following, and treat them as independent because each one can pass while the others fail.

    ## 1. Text-layer check on the reopened file

    ```python
    doc = pymupdf.open("out.pdf")
    txt = doc[pno].get_text()
    assert "deleted row string" not in txt          # row really gone
    assert "next row string" in txt                 # surviving row still there
    assert "new total" in txt                       # recomputed, not stale
    ```

    Also count occurrences, not just presence — duplicates are a classic failure (old text left under a stamp plus new text drawn on top).

    This check proves nothing about hidden content: `show_pdf_page(clip=...)` hides content from `get_text()` while keeping the bytes. So **the deleted string being absent from `get_text()` is only conclusive if the page was redacted, not stamped from an unredacted copy.**

    ## 2. Structural check with `find_tables`

    ```python
    tabs = doc[pno].find_tables()
    t = tabs[0]
    assert t.row_count == expected_rows
    assert t.col_count == expected_cols
    data = t.extract()
    assert data[deleted_idx] == ["", ...] or row absent  # depending on your approach
    ```

    Confirm the header row still has the right labels and column count, and that the surviving rows' cells match what you intended — this catches a row that shifted into the wrong slot or a cell whose content moved with the geometry.

    ## 3. Verify the shift geometrically, not just textually

    With `page.get_text("words")` (or `"dict"` spans), check the y-coordinates of the surviving rows: each should sit one row-height higher than before, and none should overlap the Total line or the footer. A row can be textually correct and still be drawn on top of the wrong rule.

    ## 4. Check what the text layer can't show

    - Search the raw content streams for the deleted strings (walk the page and its XObjects), since clipped-away content is invisible to `get_text()`.
    - Check for text drawn under a stamp: if you stamped, confirm the stamp source itself was redacted. Otherwise the old row is still recoverable in the PDF bytes even though every text check passes.
    - `page.get_drawings()`: confirm the table's vertical/horizontal rules and row shading still exist in the expected count and positions — redaction defaults delete line art that lies entirely inside the rect, and a fill drawn after the text hides glyphs while leaving them in the text layer.

    ## 5. Render and look at it

    ```python
    pix = doc[pno].get_pixmap(dpi=150)
    pix.save("check.png")
    ```

    Visual inspection is the only check that catches paint-order bugs (fill over text, text over rules), overlapping rows, and a stamp whose clip cut the row in half. It is also the only check that catches *visible* duplication; the text layer catches *invisible* leftovers. You need both.

    ## 6. Re-check the neighbours you didn't mean to touch

    Re-extract the cells adjacent to the edit (above, below, left, right) and confirm their text is unchanged. For a single-cell edit, also confirm the cell's border and shading are still present in `get_drawings()` — that is exactly the case where default redaction settings silently strip the box.

    ## 7. Sanity-check the output file itself

    Open it fresh in a second viewer or re-run the extraction after `doc.close()`; save with `garbage=4, deflate=True` and confirm the file opens cleanly and the page count is unchanged. A file that only reads correctly because of stale in-memory objects is not verified.

    **Bottom line:** the minimum credible verification is: reopen the saved file, assert the deleted strings are absent *and* the surviving strings are present and correctly positioned, re-extract the table to confirm row/column counts and the recomputed total, confirm no hidden copy of the old row survives in the content streams, and render a page image to inspect it. A script exiting 0 proves only that the script ran.
- id: pdf-verify-02
  answer: |
    **No — the row is not gone from the file.**

    `doc.save("same.pdf", incremental=True)` does not rewrite the PDF. It appends the changed objects (a new page content stream, new page object, new xref/trailer) to the end of the existing file and chains the old cross-reference table via `/Prev`. Everything that was already in the file stays in the file:

    - the page's **original content stream**, with the deleted row's text operators, is still there byte-for-byte;
    - the original page object, fonts, and any images that were referenced still exist;
    - the original xref section is still intact.

    `apply_redactions()` only edits the *current* version's content stream. The previous revision — reachable by following the old `/Prev` xref or simply by extracting objects with a forensic tool — still contains the row. `get_text()` on the reopened file will look clean, because it reads only the latest revision; that check proves nothing about the bytes on disk.

    Practical consequences:

    - **Incremental = no real removal.** Redaction is only as strong as the save mode. Incremental saves are for signatures/append-only workflows, not for sanitising data.
    - **The file also grows**, and the leaked text may be trivially recoverable with `mutool clean -d`, `qpdf --qdf`, `strings`, or any tool that enumerates objects across revisions.
    - **To actually remove it:** save non-incrementally to a *new* file with `garbage=4, deflate=True` (plus `clean=True` where appropriate), which drops unreferenced objects and does not carry the old revision. If the original path must be overwritten, write to a temp file first and then replace it — overwriting the source in place re-exposes the shrunken-but-old file if you get the ordering wrong.
    - **Verify against the bytes, not the API:** confirm the deleted strings are absent from the output file's raw content (e.g. search object streams / run `pdftotext` and a raw byte grep), and that there is no prior revision left (a single xref section, no suspicious `/Prev` chain).

    So: redaction + incremental save leaves the confidential row recoverable. It is only removed after a full (garbage-collecting) rewrite to a fresh file.
- id: pdf-verify-03
  answer: |
    **Assume you cannot edit `invoice.pdf` in place.** An invoice is both a document someone depends on and a file with legal/financial value — the original must survive any mistake.

    **1. Never write to the input path**
    - Open `invoice.pdf` read-only. Write the result to a new path (`invoice-edited.pdf`, or an output dir), not over the source.
    - Don't use "save" semantics that rewrite the original; PyMuPDF's `doc.save(out)` to a different name is the safe pattern.
    - Before doing anything, confirm the exact path (`ls -l`, absolute path) so a later `mv`/`rm` can't hit the wrong file.

    **2. Keep a pristine copy**
    - Treat the original as immutable: copy it aside (`invoice.orig.pdf`) if there's any chance the working copy gets clobbered.
    - After saving, verify the original is unchanged (size/mtime, or a hash). If the source changed, something wrote through the wrong path.

    **3. Write atomically**
    - Save to a temp file first, then rename into place. A crash or exception mid-save then leaves the original (and any previous output) intact instead of a truncated PDF.
    - Catch errors and report them; never leave a half-written output behind as if it succeeded.

    **4. Sanity-check the input before editing**
    - Confirm it opens, report page count, and check for encryption/password — don't silently strip protection.
    - Identify what you're changing (table/rows/cells) before mutating; a wrong page or wrong coordinates is a data-integrity bug, not just a visual one.

    **5. Make removal real, not hidden**
    - If content is deleted (a row, a total), redact and `apply_redactions`, then save with `garbage=4, deflate=True` so orphaned objects are purged.
    - Don't rely on `show_pdf_page(clip=...)` to hide something — the clipped-away content stays in the file bytes. Stamping must come from a source copy where the deleted content was also redacted.
    - Verify with `get_text()` that deleted strings are gone, and that neighbouring cells/rules survived.

    **6. Validate the output before declaring success**
    - Reopen the saved file; check page count, re-extract text/tables, and ideally render to an image to confirm the layout and totals.
    - Recompute derived values (e.g. Total) from the data rather than leaving a stale number.

    **7. Handle the sensitive content carefully**
    - Invoices carry PII and payment details. Don't upload the file, don't paste full text into logs or transcripts, and don't leave extra copies in shared/temp locations — delete intermediate artifacts you created.
    - Don't alter metadata, encryption, or signatures unless asked; if it's digitally signed, flag that any edit invalidates the signature instead of silently breaking it.

    **8. Fail safe**
    - If any step is uncertain (ambiguous row, unreadable table, unexpected encryption), stop and report rather than guessing. The original stays untouched, so the user can retry.
    - Never run destructive commands (`rm`, overwrite `>`) against the source path as part of cleanup.

    Bottom line: read the original, write a new file atomically, purge what you removed, verify the result, and leave the source byte-identical — with a backup if there's any doubt.
- id: pdf-verify-04
  answer: |
    Because "ran without errors" only proves the Python process reached the print statement — it says nothing about the PDF's *content*. In a PDF edit there is no compiler, no type checker, and no semantic model that can reject a wrong edit. PyMuPDF will happily accept almost any call and produce a structurally valid file that is visually or semantically wrong.

    Concretely, "done" fails as evidence for all of these reasons:

    **1. Silent no-ops are normal.**
    - `page.search_for()` finds nothing → `add_redact_annot` never called → redaction does nothing, no error.
    - A rect with wrong coordinates (points vs pixels, top-left vs bottom-left origin, a rect outside `page.rect`, `x0 > x1`) is accepted; it just covers nothing or covers the wrong band.
    - `show_pdf_page(clip=...)` with a bogus clip draws nothing or the wrong region without complaint.
    - `doc.save()` to the path you didn't intend, or an exception swallowed by a broad `try/except`, still prints "done".

    **2. The API defaults are destructive in ways that don't raise.**
    `apply_redactions()` defaults to `images=2` (blanks overlapping pixels), `graphics=1` (`REMOVE_IF_COVERED`, deletes line art wholly inside the rect), `text=0` (removes overlapping glyphs). So a "cell edit" silently deletes the cell's shading, and a whole-row redaction silently deletes the grid, while the script reports success. Conversely, wrong flags leave the old text in place — also no error.

    **3. Removal is not what it looks like.**
    `show_pdf_page(rect, src, pno, clip=old)` stamps a form XObject containing the *entire* source page; the clip only hides the rest. The deleted row can still be in the file's bytes and recoverable, yet `get_text()` on the output won't show it. A page can look correct and fail a bytes-level audit — or look correct in one viewer and be wrong.

    **4. `get_text()` and rendering disagree, and both can lie about your intent.**
    Text present under a later-drawn fill is invisible but still extractable; text you think you moved may be duplicated (old copy left in place plus new stamp); moved rows may overlap the Total; recomputed totals may be arithmetically wrong; fonts/sizes/colours may silently fall back. None of that throws.

    **5. PyMuPDF repairs and tolerates.**
    It opens damaged PDFs, substitutes missing fonts, ignores out-of-range values, and emits warnings rather than exceptions. `garbage=4, deflate=True` produces a clean-looking file even if the content is wrong. Readers are lenient, so "it opens" is a very weak test.

    **6. Exception-free ≠ correct postcondition.**
    The script proves only that each call matched its signature. The postconditions you actually care about — deleted strings gone, remaining rows intact and shifted correctly, grid and shading preserved, total consistent with rows, inserted image not overlapping content, page count and geometry unchanged — are never asserted anywhere.

    **What would count as evidence** (reopen the saved file and check, don't trust the log):
    - Re-extract text per page; assert the deleted row's strings are absent and the kept rows are present exactly once (catches both failed deletion and duplication).
    - Re-run `find_tables()`; compare row count, cell text, and column x-positions to the expected post-edit table.
    - Check totals by recomputing from extracted cell values and comparing to the printed Total.
    - Render before/after to images (`get_pixmap`) at known DPI and diff the changed region; confirm no overlap of the inserted image rect with any word/drawing/image bbox.
    - For real removal, verify the *bytes*/objects, not just the clip: confirm the stamp source was itself redacted and applied, and that the deleted content isn't sitting in an unclipped XObject.
    - Assert file facts: output path exists, mtime/size changed, page count and `page.rect` unchanged, `doc.is_pdf`/reopen succeeds.
    - Spot-check in a second, independent renderer, since one viewer's tolerance can mask breakage.

    Bottom line: "no exceptions, printed done" is evidence about control flow only. A PDF edit needs postcondition checks on the reopened output — text set, geometry, table structure, totals, and (for deletions) the underlying objects — because every failure mode above is silent, and several of them leave a file that looks fine while still containing the thing you meant to remove.
```
