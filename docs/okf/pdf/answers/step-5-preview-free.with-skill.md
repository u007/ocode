# step-5-preview-free — WITH-SKILL validation answers for pdf (opencode-go, 2026-10-12); closed-book,
# derived/pdf.step-5-preview-free.SKILL.md body prepended to _prompts/pdf.md as active guidance.
- id: pdf-model-01
  answer: |
    A PDF contains no table object at all. It contains page content streams: a flat
    sequence of operators — BT/ET text objects with Tj/TJ strings, Tm/Td placement,
    rg/g colour fills, and re/l/c path painting for the rules and shaded rectangles
    that only *look* like a table. There is no row/column/cell entity anywhere in the
    file; a "table" is an emergent property of text coordinates plus vector lines.
    Implication: the edit is geometric, not structural. You must compute the target
    row's rectangle from the rule coordinates, erase the text and vector art inside
    that rectangle (redaction), move everything below up by one row height (or
    regenerate), then repair derived values (the Total) and the remaining rules.
    Nothing about the table is "defined" enough to delete as a unit.
- id: pdf-model-02
  answer: |
    Painting a white rectangle over text leaves the original glyphs in the content
    stream, untouched, underneath. The text is still fully present in the file:
    extraction, copy/paste, search, accessibility tools, and anything that removes or
    reorders your overlay will recover it. You have only stacked one opaque object on
    another — nothing was edited, and for redaction-style use this is a data-leak.
    Use real redaction instead: page.add_redact_annot(rect) followed by
    page.apply_redactions(), which actually removes the covered characters from the
    content stream (and, via the flags, covered line art and images), then insert the
    replacement text at the same baseline.
- id: pdf-model-03
  answer: |
    The text in a content stream is not a plain byte string, so naive replacement
    fails or corrupts:
    - Streams are usually Flate-compressed; the literal characters do not exist in
      the file until you decompress.
    - Even decompressed, a visible string is often split: ("22" ".50"), TJ arrays
      with per-element kerning offsets, or individually positioned glyphs.
    - Strings may be hex (<...>) and are in the font's own encoding, so the bytes
      for "2" are not ASCII 0x32.
    - The text may live in a Form XObject, not the page stream.
    - Changing string length breaks things: the numeric adjustments in a TJ array
      were tuned for the original glyph widths, so even a same-length swap shifts
      spacing, and a different-length one can corrupt operators you never intended
      to touch.
    - The new glyphs must exist in that font's encoding; subset fonts can map your
      codes to the wrong glyph or to nothing.
    - If the text is Type 3 or converted to outlines, there is no string to replace
      at all.
    Edit by redaction + re-insertion, not by raw byte surgery.
- id: pdf-model-04
  answer: |
    PyMuPDF works in points (1/72 in) with the origin at the top-left and y
    increasing downward. That is the same frame extraction returns in
    (get_text/search_for/get_image_rects) and the frame the insert/draw functions
    expect — so a bbox taken from extraction can be passed straight back in. Raw PDF
    user space is different: origin bottom-left, y up, MediaBox-based.
    On a /Rotate 90/270 page, page.rect reports the displayed size (width/height
    swapped), but extraction bboxes and the rects you hand to insert/draw calls are
    in the UNROTATED page frame. Convert with rect * page.rotation_matrix (and back
    with * page.derotation_matrix). Computing a corner from page.rect.width/height
    on a rotated page lands you outside the visible area; anchor to extracted
    content bboxes instead.
    With an offset CropBox, page.rect starts at (0,0) at the CropBox's top-left,
    while raw content-stream coordinates are PDF user space; convert with
    point * ~page.transformation_matrix and back with * page.transformation_matrix.
- id: pdf-locate-01
  answer: |
    Two practical routes:
    1. Built-in table finder (PyMuPDF >= 1.23): tf = page.find_tables() — use
       strategy "lines" for a ruled table, "text" as a fallback. tf.tables gives
       each table's bbox, rows and cells; every cell exposes a bbox (and optional
       text). That is the whole grid without computing anything by hand.
    2. By hand: page.get_drawings() gives the vector paths. The horizontal strokes
       give the row y-boundaries, the vertical strokes the column x-boundaries.
       Cluster near-duplicate y values (strokes have width and are drawn as filled
       quads, so expect two edges per hairline) into unique band edges, intersect
       them into a grid, then assign each word from page.get_text("words") to a cell
       by its centre point; the union of a cell's word bboxes is the content box.
       Either way the cell you want is a geometric rectangle, found independently of
       the text inside it.
- id: pdf-locate-02
  answer: |
    search_for("Widget C") returns only the tight quads of the matched glyph run. It
    knows nothing about the row's other columns (the item name to the left, the
    quantity or price to the right) and nothing about the rules and fills that
    delimit the row, so redacting just that rect removes one cell's text and leaves
    the rest of the row — and possibly neighbouring glyphs you barely grazed.
    Get the full row region from the table geometry instead: locate the table
    (find_tables, or cluster the horizontal/vertical rules from get_drawings), use
    the matched quad's y-centre to select the row band between the horizontal rules
    above and below it, and take the x extent from the table's leftmost to rightmost
    column boundary. That full rect is what you redact (or shift).
- id: pdf-locate-03
  answer: |
    page.get_text("dict") returns blocks → lines → spans, and each span carries
    everything: "font" (e.g. "ABCDEF+Georgia-Bold" — the subset name still carries
    the base font and weight), "size" in points, "flags" (bit flags: 1 superscript,
    2 italic, 4 serifed, 8 monospaced, 16 bold), "color" as an sRGB integer (decode
    with pymupdf.sRGB_to_rgb), "ascender"/"descender" (font-relative), "bbox", the
    text itself, and "origin" — the (x, y) point where the baseline starts.
    Read the style from a sibling span in the same row (or the target cell's own
    span, before redacting it), then re-apply the same font, size and colour and put
    the new text on the same baseline y taken from "origin". If the font is an
    embedded subset, reuse it by extracting it (doc.extract_font(xref) gives you the
    font file buffer) and registering it with page.insert_font(fontfile=...).
- id: pdf-locate-04
  answer: |
    Column boundaries come from the table's vector rules: the x positions of the
    vertical strokes in page.get_drawings() (or the column objects of
    page.find_tables()). For the right edge where each right-aligned number ends,
    use the word bboxes from page.get_text("words") — the x1 of a number's word is
    exactly where it ends. Gather x1 for every number in a column; the shared value
    is the column's right-alignment edge.
    To place a new or edited value so it lines up: width = pymupdf.get_text_length(
    text, fontname, fontsize) (or fitz.Font(...).text_length), then
    x = right_edge - width, and y = the baseline from the original span's "origin"
    (or the same baseline as its sibling rows). Measuring existing right edges is
    what keeps column-wide alignment instead of guessing the padding.
- id: pdf-rowdel-01
  answer: |
    Correct procedure, in order:
    1. Back up the file and work on a copy opened with fitz.open().
    2. Locate the row: find_tables, or cluster horizontal rules from get_drawings.
       The row rect spans the table's x extent and the y band between the rules
       above and below the target. Record row_h.
    3. Compute the region below that must move up: from the row's bottom edge to the
       table bottom / end of following content.
    4. Open an untouched copy of the same document (src = fitz.open(path)) to stamp
       from later.
    5. Redact the target row AND the moving region on the working page with
       apply_redactions() — keep the default graphics removal so the row's rules and
       fill go with the text.
    6. Move the region in one step: page.show_pdf_page(
       old_rect - (0, row_h, 0, row_h), src, pno, clip=old_rect). It re-stamps the
       region one row height higher as a Form XObject; appearance is preserved and
       the text stays extractable.
    7. Fix the table's bottom rule at the new edge and re-check the header/banding.
    8. Recompute the Total from the remaining amounts, redact the old Total text with
       graphics=0, images=0 (keep the rules), and insert the new value right-aligned
       on the original baseline.
    9. Save to a new file and verify (extraction + render diff).
- id: pdf-rowdel-02
  answer: |
    Defaults of page.apply_redactions(): images=PDF_REDACT_IMAGE_PIXELS (2) — it
    blanks only the pixels under the rect and keeps the image object and its
    placement; PDF_REDACT_IMAGE_REMOVE (1) is what deletes an overlapping image and
    must be requested explicitly. graphics=PDF_REDACT_LINE_ART_REMOVE_IF_COVERED (1)
    — removes line art lying entirely inside the rect (2 removes anything touched,
    0 keeps everything). text=PDF_REDACT_TEXT_REMOVE (0) — removes every character
    whose bbox overlaps.
    Why it matters: deleting a table row wants the defaults, so the row's rules and
    shading are removed along with the text. But when you are changing one cell's
    text and the borders/shading must survive, you must pass graphics=0, images=0
    explicitly — the default would strip the cell's rules and fill, and a small/tight
    rect is not a substitute for the flags; the flags are the actual control.
- id: pdf-rowdel-03
  answer: |
    Yes — nothing below has to be re-typed. Open an untouched copy of the document as
    the source, redact the deleted row together with the region below that must move,
    apply, then stamp the region back one row height higher:
    page.show_pdf_page(old_rect - (0, row_h, 0, row_h), src, pno, clip=old_rect).
    The stamped region becomes a Form XObject on the same page, so the text stays
    extractable and the appearance (fonts, exact glyph positions, shading) is
    preserved exactly. No temp document or temp page is needed.
- id: pdf-rowdel-04
  answer: |
    Besides the row itself:
    - The Total: sum the remaining line amounts and rewrite it.
    - Derived figures: subtotal, discount, tax/VAT, grand total, "n items" counts.
    - The table's rules/banding after the shift (bottom rule, last row's fill) and
      anything below the table that moved.
    - Cross-references to the deleted line elsewhere: notes, terms, PO or SKU numbers
      mentioned in footnotes or the body.
    - Pagination artefacts ("Page 1 of 2") if the table spans pages.
    - Invisible carriers of the data: the redacted text layer (verify it is truly
      gone), metadata, annotations, embedded files/attachments.
- id: pdf-cell-01
  answer: |
    Steps for "22.50" → "54.00":
    1. Work on a copy; open it with fitz.open().
    2. Read the original span with page.get_text("dict") for that cell: font, size,
       flags (bold), colour, and "origin" (the baseline point).
    3. Get the word rect via page.search_for() or get_text("words").
    4. Redact only that tight rect with page.add_redact_annot(rect) and
       page.apply_redactions(graphics=0, images=0) so the cell's borders and
       shading fill survive — only the glyphs are removed.
    5. Compute the new position for right alignment: x = right_edge -
       pymupdf.get_text_length("54.00", fontname, fontsize); baseline y = the
       original span's origin y. Keep the same right edge the old number ended on.
    6. Reuse the embedded subset font if the digits are present (doc.extract_font +
       page.insert_font(fontfile=...)), else a metrics-compatible base-14 substitute.
    7. page.insert_text((x, y), "54.00", fontname=..., fontsize=..., color=...).
    8. Save as a new file, then verify: extraction shows 54.00 and no 22.50, and a
       pixel-diff of a crop against the original shows a native-looking result.
- id: pdf-cell-02
  answer: |
    Size it to the glyphs you want gone and nothing else: the word bbox plus a small
    padding (a point or two) so ascenders, descenders and antialiasing edges are
    fully covered, but stopping short of the cell's rules and the neighbouring
    column's digits. Reason: text=0 removes every character whose bbox *overlaps*
    the rect, so grazing the adjacent number deletes it; and graphics=1 removes any
    line art lying entirely inside the rect, so brushing the cell's border erases
    it. The flags are the real control — pass graphics=0, images=0 for a text-only
    cell edit and keep the rect tight-but-complete. Do not try to achieve
    preservation by making the rect tiny, and do not inflate it to "be safe".
- id: pdf-cell-03
  answer: |
    When the text does not fit, insert_textbox returns a negative value (the space
    deficit) and does not draw the overflow — the box ends up empty or partially
    filled. It never auto-shrinks, never warns, never raises.
    Options: measure first with pymupdf.get_text_length(text, fontname, fontsize)
    (or fitz.Font.text_length) against the rect width; loop the font size down until
    it fits; wrap deliberately with explicit newlines and a taller rect/row; keep
    the width fixed and choose per-column shrink vs wrap; abbreviate the text; or
    escalate to a proper relayout (wider column or different row height) if nothing
    fits inside the cell.
- id: pdf-cell-04
  answer: |
    point is the start of the *baseline* — effectively the bottom-left corner where
    the first glyph sits on the writing line (PyMuPDF calls it the bottom-left
    position of the text). If you pass the top-left corner of the old word's bbox
    instead, the new text drops down by roughly the ascender height: it renders too
    low, collides with the cell's descender space or the row below, and its baseline
    no longer aligns with the sibling numbers.
    Use the baseline point from the original span's "origin", or derive it from the
    bbox with the font's metrics (baseline_y = bbox.y1 - descender * size).
- id: pdf-relayout-01
  answer: |
    What must happen:
    - Everything from the insertion point down shifts by the new row's height: the
      Total row and any content below the table (totals block, terms, footer). Push
      it with the redact-then-show_pdf_page move, or reflow/repaginate if space runs
      out.
    - Two new horizontal rules (above and below the new row); the rule that used to
      sit above the Total becomes the new row's bottom rule.
    - Vertical rules extend down through the new row.
    - Shading/banding must continue consistently; inserting mid-table changes the
      alternating pattern for every row below, so recheck and refill.
    - The Total row's text moves down and the Total value is recomputed to include
      the new line.
    - The table's bottom rule lands one row lower; confirm the whole table stays
      inside the page's usable area and does not overlap the footer.
- id: pdf-relayout-02
  answer: |
    Correct approach when the table already spans the full width between the margins:
    choose new column widths that sum to the SAME total width (take the extra width
    from existing columns), then measure the widest text in every column at its new
    width with pymupdf.get_text_length and decide per column — shrink the font
    slightly or wrap into a taller row. Don't assume the old text still fits. Then
    redact the whole old table with the default graphics removal and redraw
    everything at the new x positions: header fill, alternating shading, all vertical
    and horizontal rules, then the text.
    Common mistake: shifting the columns' text (or only the vertical rules and text)
    while the old header fill and row shading stay where they were — leaving stale
    fills, doubled rules and text over the wrong background. Anything not fully
    erased reappears overlapped.
- id: pdf-relayout-03
  answer: |
    Acceptable only when no original source exists (template, HTML, reportlab/Excel
    code, Word file) and the change is structural enough that a surgical in-place
    edit is impractical, or when pixel fidelity genuinely does not matter. First
    always ask whether the source exists and regenerate from it.
    Risks: fonts get substituted or re-embedded (metrics and weight shift), table
    cells are re-derived with different widths and borders, pagination and line
    breaks change, images are recompressed/resized, hyperlinks, structure tags and
    metadata are lost, headers/footers drift, and the round-trip is lossy even when
    the PDF originally came from Word or HTML. If you must do it, verify by rendering
    both versions and pixel-diffing. Treat it as a last resort, not a shortcut.
- id: pdf-relayout-04
  answer: |
    Build the occupied-rectangle list and measure the gaps:
    - Text: page.get_text("blocks") (or "words") bboxes.
    - Vector art: page.get_drawings() path rects.
    - Images: page.get_image_info() bboxes.
    - Respect the margins (page.rect / CropBox) and any header/footer zones.
    Subtract the union of occupied rects from the page rect — the remaining free
    bands are where content can grow. For a table growing downward, check the
    vertical gap between the table's bottom rule and the first occupied rect below
    it, and confirm that after shifting, the last element still clears the bottom
    margin. A robust cross-check: render with page.get_pixmap(dpi=100-150) and scan
    pixel rows for non-white pixels over the x range you care about, since invisible
    or clipped items (white text, covered paths) can hide inside nominal bboxes.
- id: pdf-tblins-01
  answer: |
    Way 1 — draw it directly with PyMuPDF primitives: compute the grid rects, use
    page.draw_rect/draw_line for fills and rules (fills first, rules next, text
    last), and page.insert_textbox(rect, text, align=...) per cell, checking the
    return value so nothing silently overflows. Total control, no extra files.
    Way 2 — generate a purpose-built one-page PDF and stamp it into the free space:
    build the table with ReportLab's platypus Table (or fpdf2) sized exactly to the
    target rect, then page.show_pdf_page(target_rect, src_doc, 0). Row heights,
    wrapping and alignment come for free, and the stamped text stays extractable
    because it is placed as a Form XObject.
    Both keep real text (unlike an image of a table), which matters for accessibility
    and for any later edit.
- id: pdf-tblins-02
  answer: |
    To match the document and avoid glitches:
    - Copy the fonts (family/size/weight) and the exact colours of the header fill,
      banded rows, and rule stroke width/colour read from existing tables or spans;
      snap to the same margins and column grid and the same cell padding.
    - Measure before drawing: get_text_length per cell so text stays inside its cell,
      and treat a negative insert_textbox return as "does not fit".
    - Draw order: fills, then rules, then text — otherwise a fill painted later
      swallows the text.
    - Geometry: keep everything inside the printable area; verify the target band is
      genuinely empty first; use coordinates that avoid sub-pixel/hairline blur.
    - After inserting, render a crop to PNG and compare visually with the surrounding
      page (and keep the text extractable).
- id: pdf-tblins-03
  answer: |
    Options:
    - Make room: compute the needed height (header + rows) and shift the two
      paragraphs and everything below them down by that amount using the
      redact-then-show_pdf_page move — provided the freed space at the bottom still
      accommodates the last element of the page.
    - If the page is full, repaginate: push the overflow onto a newly inserted page,
      or place the table on the next page where space exists.
    - Fit the gap: fewer rows, tighter row height or smaller font so the table fits
      the actual space; or split the table across the page boundary.
    - Negotiate placement: e.g. move the paragraphs instead, or ask the user.
    What you must not do is draw the table on top of the existing text — overlapping
    glyphs are both visually broken and semantically wrong.
- id: pdf-imgrep-01
  answer: |
    1. Find the placement: info = page.get_image_info(xrefs=True) (or
       page.get_image_rects(xref)); take the bbox and the xref.
    2. page.replace_image(xref, filename="new.png"). The placement rect is unchanged
       because only the image object is swapped — and a new PNG with a different
       aspect ratio is stretched into the old box, so pad or resize it first.
    The catch: an xref is document-level. If the same image object is drawn on other
    pages (a header logo, a repeated graphic), replace_image changes every occurrence.
    If only page 1 should change, do a single-occurrence swap instead: redact that
    rect with images=PDF_REDACT_IMAGE_REMOVE, apply, then page.insert_image(rect,
    filename=...). Never delete_image(xref) + insert_image — delete_image replaces
    the shared object with a blank pixmap and blanks the logo on every page.
- id: pdf-imgrep-02
  answer: |
    The old logo is not gone. insert_image paints the new image over the top; the
    original image object is still in the file, intact, one layer below. If the new
    PNG has an alpha channel the old art can bleed through; and the moment the new
    image is moved, resized, transparently padded, or the drawing order changes, the
    old logo reappears. The file also still carries the dead bytes. Overlay is
    concealment, not replacement — use page.replace_image(xref, ...) for a global
    swap, or redact with images=PDF_REDACT_IMAGE_REMOVE and then insert, for one
    occurrence.
- id: pdf-imgrep-03
  answer: |
    page.get_images() only lists XObjects in the page's own resource dictionary, so
    a visible image need not appear there. Likely causes:
    - It is nested inside a Form XObject (a header/logo drawn as a nested stream):
      check page.get_xobjects() and inspect each Form's resources; get_image_info()
      walks the page's display list and normally reports these.
    - It is an inline image (BI ... ID ... EI) embedded in a content stream — never
      an XObject, so inspect the decoded stream or get_image_info().
    - It is not an image at all: vector art — page.get_drawings() will show paths.
    - It may be drawn by a Type 3 font as glyphs, or you are inspecting the wrong
      page.
    To find and replace: page.get_image_info(xrefs=True) gives both the bbox and the
    xref; then page.replace_image(xref, ...) for every occurrence, or
    redact-with-images=REMOVE + insert_image for a single occurrence.
- id: pdf-imgins-01
  answer: |
    1. Get the PNG's pixel size (fitz.Pixmap(path).width/height).
    2. Width is 150pt, so height = 150 * img_h / img_w — the rect then has exactly
       the image's aspect ratio (better than relying on keep_proportion centring).
    3. Choose the position from actual free space: take the lowest occupied content
       (max y1 across get_text("blocks") bboxes, drawings and image rects) and place
       the rect just below it; right edge at the right margin (or aligned to the text
       block's right edge). Verify the candidate rect does not intersect any occupied
       bbox and stays inside the margins.
    4. On a /Rotate page do the arithmetic in the unrotated frame (page.rect reports
       the displayed size) and anchor to extracted bboxes, not page.rect corners.
    5. page.insert_image(rect, filename="signature.png") with overlay=True (default)
       so it paints on top.
- id: pdf-imgins-02
  answer: |
    Insert once and reuse the returned xref. On the first page:
    xref = page.insert_image(rect, filename="logo.png"); on every later page call
    page.insert_image(rect_i, xref=xref) so all 200 pages reference one image object.
    Do not hand-edit Resources/XObject with xref_set_key or append "q cm /Im Do Q"
    operators to content streams. As a safety net, doc.save(path, garbage=4,
    deflate=True) merges identical objects, but reusing the xref is the real fix.
    Confirm with doc.get_page_images() (same xref on every page) or by comparing
    len(doc.xref_length()) before and after.
- id: pdf-imgins-03
  answer: |
    Likely causes:
    - It is being drawn behind existing content: insert_image's overlay parameter —
      pass overlay=False (background) and the image sits under an opaque fill or box
      painted earlier in the stream and disappears.
    - Wrong coordinate frame: on a /Rotate page, computing the rect from the
      displayed width/height puts it off the visible area; a rect outside the
      CropBox is clipped; a zero or negative-size rect draws nothing.
    - Transparency problems: an RGB image with an alpha channel/mask that renders
      as fully transparent, or a light/white image on a white page.
    - Inserted onto a different Page object than the one you are viewing (wrong loop
      index), so the visible page was never touched.
    - Saved under a new filename while the viewer shows the old file — a stale-view
      red herring.
    Diagnose by re-extracting from the saved file: page.get_image_info() should list
    your bbox, and a pixmap render shows what a viewer sees.
- id: pdf-fonts-01
  answer: |
    A subset font (the "AAAAAA+" prefix marks it) contains only the glyphs that were
    used in the document. The subset's cmap/encoding maps only those character
    codes, and the glyphs for your new letters simply are not in the file. Reusing
    it produces missing glyphs (blank cells or .notdef boxes) — or wrong glyphs if
    the encoding maps your codes onto whatever happens to be present.
    What to do: check coverage first (extract the font and inspect its cmap with
    fontTools — note a subset may carry only a Mac-platform cmap, and PyMuPDF's
    Font.has_glyph can report false "missing" for those). Then either embed the full
    original font (page.insert_font(fontname=..., fontfile=...)) and use it, or fall
    back to a visually close base-14 font and accept the difference, or restrict the
    edit to characters the subset actually contains. Never assume the embedded
    subset can render new letters.
- id: pdf-fonts-02
  answer: |
    PyMuPDF's base-14 aliases: Helvetica regular is "helv"; Helvetica-Bold is
    "hebo" (obliques are "heit" and "hebi").
    Limitation: the base-14 are not embedded — they rely on the viewer's fonts — and
    are restricted to the Latin/Western repertoire (StandardEncoding/WinAnsi).
    Non-Latin scripts (CJK, Arabic, Hebrew, Indic) and many special characters have
    no glyphs and render as blanks or notdefs. Even within Latin-1, appearance is not
    guaranteed identical across viewers, and you cannot embed them for archival
    fidelity. For non-Latin or symbol-heavy text you must embed a font that actually
    carries the Unicode coverage.
- id: pdf-fonts-03
  answer: |
    Detect by reading the original span before redacting it: page.get_text("dict")
    gives the span's "font" (e.g. "ABCDEF+Georgia-Bold" — the base-font name carries
    the weight), "flags" (bit value 16 = bold, 2 = italic), "size", "color" and
    "origin". Compare those with what you passed to insert_text.
    The usual cause is using the generic alias ("helv") or a default size instead of
    the document's real font and the span's exact size. Fix: reuse the embedded font
    (doc.extract_font(xref) + page.insert_font(fontfile=...)) and insert with the
    exact fontname, fontsize, colour and baseline y from the span. Verify afterwards
    by extracting the new span and diffing font/size/flags against an untouched
    header cell, plus a rendered crop.
- id: pdf-verify-01
  answer: |
    "The script ran" proves nothing about pixels. Verify by:
    - Re-extraction: page.get_text()/"words" and assert the new string is present,
      the old one is gone, and the bbox/baseline is where you intended (compared to
      the span you recorded before redacting).
    - Style check: get_text("dict") span font/size/colour matches its neighbours.
    - Render: page.get_pixmap(dpi=150) of the saved file, pixel-diffed against the
      original, with the diff confined to the edited bbox.
    - Independent parser read-back: pdftotext -layout, pdfplumber or pypdf.
    - Structural integrity: qpdf --check (or reopen and round-trip) on the saved
      file.
    - For redactions: search the decoded content streams and the raw bytes of the
      saved file for the removed string.
- id: pdf-verify-02
  answer: |
    No. incremental=True appends a new revision to the file and leaves every original
    object in place — the previous content stream containing the confidential row is
    still in the file, so anyone who strips the appended portion (or opens the old
    objects directly) recovers the text. Applying redactions is not enough if you
    then save incrementally.
    Correct approach: do a full save — doc.save("clean.pdf", garbage=4,
    deflate=True) — so the superseded objects are dropped, then verify by searching
    the saved file's raw bytes and its decompressed streams for the confidential
    string, and check other hiding places (annotations, attachments, metadata/XMP,
    duplicate page copies). Only overwrite the original after that verification
    passes.
- id: pdf-verify-03
  answer: |
    File-safety practices:
    - Never edit the original in place: work on a copy, keep the original untouched,
      and record its checksum (sha256) before starting.
    - Save to a new filename until the output is verified; only then replace the
      original, keeping a backup.
    - Never use incremental saving when content is being removed.
    - Verify the saved artefact (not the in-memory doc): extraction assertions,
      qpdf --check, an independent-parser read-back, and a rendered pixel diff.
    - Clean up scratch files and check the target file is not already modified or
      owned by a concurrent worker before overwriting it.
    - If the document is sensitive, strip metadata/annotations/attachments as part
      of the job.
- id: pdf-verify-04
  answer: |
    Because PDF-editing failures are silent: PyMuPDF returns rather than raises for
    the common ones — insert_textbox returns a negative deficit and simply does not
    draw; a rect computed from page.rect on a rotated page lands off the visible
    area; glyphs missing from a subset font render as blanks; insert_image with
    overlay=False hides under an existing fill; search_for on a wrong string returns
    [] and the script continues; saving to the path you opened (or never saving)
    leaves the original untouched while the script prints "done". None of that is an
    exception.
    "done" only proves the API accepted your arguments. Evidence comes from
    inspecting the artefact: extract the text, render the page, diff the pixels, and
    read the file back with a different parser.
