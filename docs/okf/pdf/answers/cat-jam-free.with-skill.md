# cat-jam-free — WITH-SKILL closed-book answers for pdf (opencode-go, 2026-10-14)
# Delivery: derived/pdf.cat-jam-free.SKILL.md body (front matter stripped) prepended to _prompts/pdf.md; one
# `ocode run -m opencode-go/cat-jam-free -effort med` session, isolated HOME with every tool denied, cwd holding only the sheet;
# session audited in sqlite: 1 user + 1 assistant message, 0 tool calls.

- id: pdf-model-01
  answer: |
    A PDF table is just page content: text-showing operators, line/fill drawing operators, image placements, and possibly shading patterns at absolute coordinates. There is no table object or row metadata. Deleting a row therefore requires removing the affected marks and shifting everything below up, then redrawing rules, shading, and recomputed totals; it is geometry editing, not a structural delete.
- id: pdf-model-02
  answer: |
    A white rectangle is only a visual overlay: the original text, vectors, and metadata remain extractable and can be uncovered. A real edit removes the content using redaction (add_redact_annot + apply_redactions, with appropriate flags), then redraws the replacement text and any needed rules/fills. When possible, regenerate from the original source instead.
- id: pdf-model-03
  answer: |
    Content streams can be compressed, use Tj/TJ operators, hex strings, font-specific encodings, kerning adjustments, subset-font ToUnicode maps, and shared objects. Byte-level substitution can break hex pairing, corrupt compressed streams, miss transformed text, or alter unrelated numeric tokens. Use a structural PDF editor such as PyMuPDF or regenerate from source.
- id: pdf-model-04
  answer: |
    PyMuPDF uses a top-left origin with y down and reports extraction and insertion in the same unrotated page coordinate system. Raw PDF content coordinates are user space with y up and MediaBox-based; convert using page.transformation_matrix and its inverse. page.rect starts at the CropBox top-left. On /Rotate pages, page.rect reports the displayed size, but extracted bboxes and insert/draw rects remain unrotated; convert with page.rotation_matrix and page.derotation_matrix.
- id: pdf-locate-01
  answer: |
    Use page.find_tables() (a heuristic table finder) and inspect each Table's bbox, rows, columns, cells, and cells' rectangles. Cell rectangles plus text extraction give row/cell geometry. Alternatively cluster words from page.get_text("words") by x/y baselines and intersect with detected rulings, but find_tables is the direct API for structured regions.
- id: pdf-locate-02
  answer: |
    search_for returns only one or more text-match rectangles, not the full row, cell, fill, borders, or column extents. Build the full row region from the table finder's cell/row geometry or cluster all words on the same baseline, then expand to the table's left/right margins and row height as needed.
- id: pdf-locate-03
  answer: |
    Use page.get_text("dict") or "rawdict": each span has font, size, color, bbox, and origin/baseline information. Match span['font'], span['size'], span['color'], and the baseline (span origin or bottom of bbox). get_texttrace can provide exact glyph placement if higher precision is needed.
- id: pdf-locate-04
  answer: |
    Column boundaries and right-aligned ending positions come from the detected table (find_tables().tables[0].cells/columns) and word rectangles from get_text("words") or "dict". For right alignment, a word's x1 / bbox right is where the number ends; compare these with column x1 boundaries.
- id: pdf-rowdel-01
  answer: |
    Keep an untouched copy (src = pymupdf.open(path)). Identify the deleted row, rows below, Total, and any content beneath the table. Redact those regions (default graphics removal so fills/rules go too), then apply_redactions(). Restamp the region that must move upward using page.show_pdf_page(new_rect_shifted_up, src, pno, clip=old_rect), or redraw rows with text, shading, and rules. Recompute and redraw the Total one row higher, update dependent totals, and verify text/table extraction.
- id: pdf-rowdel-02
  answer: |
    Defaults: images=PDF_REDACT_IMAGE_PIXELS (2) blanks overlapping image pixels, graphics=PDF_REDACT_LINE_ART_REMOVE_IF_COVERED (1) removes line art entirely inside the rect, and text=PDF_REDACT_TEXT_REMOVE (0) removes text. For a row/cell, defaults can delete borders, fills, and image content that should be retained; to preserve them, pass images=0 and graphics=0 explicitly.
- id: pdf-rowdel-03
  answer: |
    Yes: keep an untouched copy of the original, redact the old region on the target page, then use page.show_pdf_page(new_rect_shifted, src, pno, clip=old_rect). The result is a Form XObject that stays vector-based and extractable; it is not rasterized. Never use a pixmap for this.
- id: pdf-rowdel-04
  answer: |
    The Total, subtotals, tax, per-page subtotals, row numbers, continuation headers, footnotes, references in prose, and pagination or overflow placement may all need updates. Any downstream totals in the document should be recomputed, and page count or overflow should be checked.
- id: pdf-cell-01
  answer: |
    Find the cell and old span style. Add a redaction rect covering the glyphs but staying inset, then apply_redactions(images=0, graphics=0). Measure the new value with pymupdf.get_text_length, compute a right-aligned baseline point using the original ending x and baseline, and insert_text with the original font, size, color, and weight. Verify old value absent and new value present.
- id: pdf-cell-02
  answer: |
    Size the rect to cover the glyphs with a little padding, but keep it inside the cell and away from neighboring text and rulings. apply_redactions removes characters whose bbox overlaps the rect, so an oversized rect can delete adjacent cells; an undersized rect can leave descenders or stray marks. The flags preserve borders and shading; size controls which characters are removed.
- id: pdf-cell-03
  answer: |
    insert_textbox returns a negative height shortfall and writes nothing if the text does not fit; there is no exception. Options include using a smaller font, wrapping into a taller row and shifting rows below, widening the column, or using insert_htmlbox, which can scale the content down.
- id: pdf-cell-04
  answer: |
    point is the baseline origin (start point) for the text, not the top-left corner. Passing the top-left bbox corner places the text too high by roughly the font ascent and can shift or clip it. Use the original baseline point and adjust x for alignment.
- id: pdf-relayout-01
  answer: |
    Redact the Total row and everything below it (or the whole affected band/table), apply, redraw the new row with its shading and rules, then redraw the Total one row lower with a recomputed value. If content below the table exists, shift it down by the added row height using redaction plus show_pdf_page with clip; if it exceeds the page bottom, create or overflow to another page. Never overlap content.
- id: pdf-relayout-02
  answer: |
    Keep the total table width unchanged by shrinking other columns. Measure each column's widest text at its new width with get_text_length, then decide per column whether to reduce the font slightly or wrap into a taller row. Redact the whole old table and redraw header fill, shading, all vertical/horizontal rules, and all text at new x positions. The common mistake is widening the page or drawing only a new rule/column over the old layout.
- id: pdf-relayout-03
  answer: |
    Acceptable only as a last resort when the original source is unavailable and formatting fidelity is not critical. Risks include changed fonts, spacing, pagination, table structure, metadata, forms, accessibility, and possible content rasterization or reflow artifacts. Ask for the source and regenerate from it first; prefer direct targeted PDF edits.
- id: pdf-relayout-04
  answer: |
    Compute the proposed rect, then compare it against existing text blocks/words, drawings, images, and page margins using page.get_text("blocks"), page.get_text("words"), page.get_drawings(), and page.get_image_rects(). If any existing item intersects the proposed rect or it exceeds the bottom margin, there is not enough safe space. There is no automatic reflow.
- id: pdf-tblins-01
  answer: |
    Two practical ways: draw the table directly on the page with draw_rect/draw_line for rules and fills plus insert_text for cell text; or generate HTML/reportlab content and use page.insert_htmlbox or page.show_pdf_page from a generated PDF, which keeps text real and extractable. Do not rasterize the table as an image.
- id: pdf-tblins-02
  answer: |
    Match the document's font, size, weight, color, cell padding, fill colors, rule weights, dash styles, alignment, and grid baseline. Draw backgrounds before text, maintain z-order, avoid partial overdraw, keep the table within margins, and verify extraction and visual rendering afterward.
- id: pdf-tblins-03
  answer: |
    Redact the content below the insertion point and restamp it lower by the table height using show_pdf_page(clip=...) from an untouched copy; if it runs past the page bottom, create or insert a page and stamp the overflow there. Otherwise place the table in existing free space or on a new page and tell the user. Never overlap existing content.
- id: pdf-imgrep-01
  answer: |
    Find the xref with page.get_images() and its placement with page.get_image_rects(xref) or get_image_info(xrefs=True), then call page.replace_image(xref, filename="new.png"). This changes every occurrence of that xref. If only one occurrence is desired, redact its rect with images=PDF_REDACT_IMAGE_REMOVE and then insert_image. Check aspect ratio first, because a different ratio is stretched into the old rect.
- id: pdf-imgrep-02
  answer: |
    The old image remains embedded and extractable behind the new one, so the file is not truly edited and the logo can still be recovered or appear if the overlay is removed. Use replace_image or redact the original image rect before inserting the new image.
- id: pdf-imgrep-03
  answer: |
    The visible logo may be vector artwork, an inline image, or content painted through a Form XObject rather than a separate image xref. Inspect page.get_drawings(), XObjects, and possibly full image lists or content streams; otherwise redact the visible region and insert_image, or edit the vector paths directly.
- id: pdf-imgins-01
  answer: |
    Start from the page's bottom-right coordinate inside its margins. Compute height = width * original aspect ratio, then rect = (page.rect.width - margin - 150, page.rect.height - margin - height, page.rect.width - margin, page.rect.height - margin). Use page.insert_image(rect, filename="signature.png") and verify it does not overlap text or drawings.
- id: pdf-imgins-02
  answer: |
    Creating or inserting image objects on each page duplicates the image data. Instead, obtain the image xref and place it through XObject reuse (for example create image once with doc.new_image or related xref handling, then reference/reuse it across pages), or use a workflow that shares the image object rather than independently inserting it 200 times.
- id: pdf-imgins-03
  answer: |
    Possible causes include drawing the image before an opaque fill that covers it, using a wrong rectangle or clip, an image outside the CropBox/MediaBox, opacity/blend handling, a missing or invalid file, or incorrect overlay ordering. Draw it with overlay=True after background fills and verify coordinates against page.rect and rotation.
- id: pdf-fonts-01
  answer: |
    An embedded subset includes only glyphs used in the document, so new characters may be missing and render as boxes or fail to insert. Use a full font file or an installed font with the needed glyphs, or choose existing text; do not rely on the subsetted embedded font.
- id: pdf-fonts-02
  answer: |
    PyMuPDF's built-in base-14 Helvetica regular and bold names are "helv" and "hebo" (Helvetica-Bold); italic/bold-italic are "heio" and "hebi". Base-14 fonts cover only limited Latin glyphs (roughly Latin-1) and have no CJK, Arabic, or emoji support; they are not embedded and must be encoded in a compatible single-byte encoding.
- id: pdf-fonts-03
  answer: |
    Read page.get_text("dict") and compare span['font'] (often contains "Bold" or weight names), span['size'], and span flags; also measure the original bbox/ascender. Then insert with the matching font name/fontfile, size, and color instead of assuming regular weight.
- id: pdf-verify-01
  answer: |
    Reopen the saved file and check: old value returns [] from search_for, new values are present, find_tables().tables[0].extract() equals expected rows in order, other pages' text and page count are unchanged. Then render with get_pixmap to inspect spacing, alignment, and rules. A render alone cannot prove a real edit versus an overlay.
- id: pdf-verify-02
  answer: |
    No. Incremental saving appends changes and can leave earlier revisions or original objects recoverable; the redacted content may not be physically purged. Save a full rewrite to a new file with garbage collection/cleaning to remove old data, and note that encryption or signatures may be affected.
- id: pdf-verify-03
  answer: |
    Work on a copy, never overwrite the original, keep a backup or versioned output, use temporary output files, validate before replacing, and save with a clear naming convention. If the PDF is encrypted or signed, obtain the password and warn that editing can invalidate the signature.
- id: pdf-verify-04
  answer: |
    Many PyMuPDF calls fail silently: insert_textbox can return negative and write nothing, search_for can return an empty list, apply_redactions returns False with no annotation, and layout can look correct while text is stale or duplicated. Successful execution is not content verification; reopen and inspect text, tables, and rendered pages.
