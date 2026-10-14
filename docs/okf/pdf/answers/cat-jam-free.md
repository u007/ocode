# cat-jam-free — closed-book answers for pdf (opencode-go, 2026-10-14); answerer saw only _prompts/pdf.md
# Delivery: one `ocode run -m opencode-go/cat-jam-free -effort med` session, sheet attached via -f, empty scratch cwd;
# session audited in sqlite: 1 user + 1 assistant message, 0 tool calls.

- id: pdf-model-01
  answer: |
    A PDF does not contain a table as an object. It contains painted content: text-showing operators, vector lines/fills, images, and possibly invisible text at coordinates. There are no semantic rows or cells. Therefore “delete the third row” cannot be a structural edit; you must identify the row’s visual region, remove or cover that painted content, shift subsequent content, and update derived values such as totals.

- id: pdf-model-02
  answer: |
    Drawing a white rectangle over text only visually hides it. The original text remains in the content stream and can still be searched, selected, copied, or extracted, which is both incorrect and a data-leak risk. Use a real redaction operation (for example PyMuPDF add_redact_annot/apply_redactions, or edit the content stream) to remove the underlying text, then redraw replacement text.

- id: pdf-model-03
  answer: |
    Plain replacement can fail because the stream may be compressed/encrypted, the target may be split across operators or TJ arrays, split between multiple text runs, combined with kerning, escaped parentheses, subset-font glyph IDs, or embedded in Form XObjects. It can also break PDF syntax or produce invalid Tj operands. Text may also be encoded in ways not matching the visible Unicode string.

- id: pdf-model-04
  answer: |
    PyMuFITZ uses a page-oriented coordinate space (commonly with origin at the top-left and y increasing downward), which differs from raw PDF user space (bottom-left origin, y upward). A /Rotate changes how displayed content maps; PyMuFITZ generally normalizes so coordinates refer to the oriented page view, not necessarily the unrotated content stream. A CropBox offset from the MediaBox means the visible area differs from the full page; coordinates you draw with should be relative to the page view after accounting for that offset, not assumed from MediaBox origin.

- id: pdf-locate-01
  answer: |
    Use a table-aware extractor rather than text-only search. In Python, pdfplumber’s find_tables()/extract_tables() returns table and cell bounding boxes; PyMuFITZ’s find_tables() returns table rectangles, rows, and cells; Camelot/Tabula can also provide geometry. If no tool is available, infer rows and cells from vector drawings (horizontal/vertical line paths) plus text spans.

- id: pdf-locate-02
  answer: |
    search_for returns only the rectangles of matching text spans. It does not identify row boundaries, column spans, ruling lines, row height, or which text belongs to the same row. To get the full row region, group all text spans by baseline/vertical position, combine their bounding boxes, and intersect with or expand by table ruling/border rectangles from vector drawings or a table extractor.

- id: pdf-locate-03
  answer: |
    Use PyMuFITZ get_text("dict") or get_text("rawdict"). For each span, read span["font"], span["size"], span["color"], and bbox; rawdict also gives per-character origin points, which identify baselines. PyMuFITZ also exposes span flags for bold/italic. Convert MuFITZ color integers into RGB as needed.

- id: pdf-locate-04
  answer: |
    Get cell/rectangle geometry from a table extractor or vector drawings. For right-aligned values, compute the maximum x1 of the text spans in that column and use it as the right alignment boundary. The left column boundary usually comes from the corresponding vertical rule or cell rectangle’s x0.

- id: pdf-rowdel-01
  answer: |
    Locate the row rectangle and its height. Add a redaction annotation covering only the row’s text and fill, apply redactions carefully, remove or adjust horizontal and vertical rules inside that band, then shift all content below the row upward by the row height. Redraw affected table borders, shaded fills, and row separators so alternation remains consistent. Recompute and update the Total, taxes, and any linked page-level totals.

- id: pdf-rowdel-02
  answer: |
    apply_redactions can remove or alter text, images, and vector graphics that intersect the redaction rectangles, depending on its image/graphics handling options. This matters for table rows because borders, shading rectangles, or shared logos clipped by the rectangle may disappear or change unexpectedly. Size redactions tightly and choose conservative graphics handling.

- id: pdf-rowdel-03
  answer: |
    Strictly, PyMuFITZ has no native move-rectangle operator that preserves editable vector text and preserves all properties. The closest approximation is rendering/capturing the lower region as a pixmap or Form XObject and reinserting it, which preserves appearance but may flatten content into an image or add page complexity. Alternatively, reconstruct the affected section with new drawing/text operations.

- id: pdf-rowdel-04
  answer: |
    Besides the removed row, check and update totals, subtotal, tax, discounts, balance due, page numbers, cross-page table continuation headers, footnotes, and anything below the table that shifts position. If the document has signatures, links, or annotations, verify they remain valid.

- id: pdf-cell-01
  answer: |
    Find the old word and span, read its rect, font, size, color, and baseline. Size a tight redaction rectangle around the old glyphs and apply it. Compute the text width of the new value with the chosen font and size; right-align by setting x = old_right_edge - new_text_width. Insert the text at the baseline start point using insert_text with the same font, size, and color. If the original subset font lacks glyphs, embed a full font. Verify visually and by text extraction.

- id: pdf-cell-02
  answer: |
    The rectangle should tightly cover the complete glyph bbox of the target text, with only minimal padding. It must not extend into adjacent cells, row shading bands, or ruling lines. Because removal is overlap-based, an oversized redaction can delete neighboring text and vector graphics, while an undersized one can leave fragments.

- id: pdf-cell-03
  answer: |
    insert_textbox returns the number of characters that did not fit; zero means all text fit. Options include reducing the font size, choosing a narrower font, abbreviating the value, increasing the cell/column width, wrapping text manually, or moving content. Do not blindly let text overflow the cell.

- id: pdf-cell-04
  answer: |
    point is the start position of text on the baseline, not the top-left corner of the text bbox. Passing bbox top-left shifts text vertically, often making it too high or too low because bbox top is above the baseline by the font ascent. Use the original span origin/baseline and adjust x for alignment.

- id: pdf-relayout-01
  answer: |
    The Total row must move downward by one row height. Table rules must be redrawn: the new row needs top and bottom rulings, the Total row’s old top rule is removed or shifted, and any fill/shading rectangles are extended or repositioned. All content below the table must shift downward to avoid overlap; if it reaches the page bottom, move excess content to another page or adjust layout.

- id: pdf-relayout-02
  answer: |
    Since the table already fills the text width, you cannot simply append a column. Reduce the widths of existing columns or add a page with larger effective width, then draw vertical rule at the new boundary and write the new header/cells. The common mistake is drawing beyond the right margin, causing clipping or overlap with page-edge content.

- id: pdf-relayout-03
  answer: |
    Conversion is acceptable when the document is simple, semantic conversion preserves the needed structure, and visual exactness is not critical. Risks include loss or distortion of fonts, spacing, pagination, tables, vectors, annotations, metadata, form fields, and hidden text. It is not suitable for precise forensic or legal PDFs.

- id: pdf-relayout-04
  answer: |
    Compare the proposed insertion/growth rectangle against occupied regions: text blocks from page.get_text("blocks"), vector drawings from page.get_drawings(), image rectangles from page.get_image_info(), and existing annotations. Also enforce page margins and check the gap between the table’s current bottom and the next content or footer.

- id: pdf-tblins-01
  answer: |
    Two practical ways are to draw primitives directly with PyMuFITZ (insert_text/insert_textbox for cell text, draw_rect and draw_line for borders and shading) or generate the small table in a separate PDF using a layout engine such as reportlab and overlay it with show_pdf_page or insert_image. A third option is a table-producing helper/form XObject, but direct drawing and overlay are the common practical approaches.

- id: pdf-tblins-02
  answer: |
    Match the existing document’s fonts, sizes, weights, colors, line styles, fill colors, padding, and alignment. Draw on a grid using coordinates from existing table geometry if available. Avoid overlapping text, images, or annotations, snap coordinates to a consistent scale, and render the resulting page to verify no visual glitches.

- id: pdf-tblins-03
  answer: |
    Options include moving later content down (difficult and risky), reducing the table size/font, placing the table in nearby existing whitespace, adding a page break, or overlaying with a separate page plus continuation content. If no gap exists, the safest is usually to add a new page or create space by moving whole downstream blocks.

- id: pdf-imgrep-01
  answer: |
    Locate the image xref and its rectangles using get_images and get_image_rects. Insert the new PNG into the same rectangle with insert_image, then remove or redact the old image XObject or its content. The catch is that PDF images can be shared across pages or XObjects; replacing or deleting the xref may change every use, while an overlay alone leaves the old image in the file.

- id: pdf-imgrep-02
  answer: |
    It looks correct visually, but the original image remains in the PDF as an extractable underlying object. This leaves stale confidential data, increases file size, can break accessibility and accessibility text alternatives, and may show through if transparency changes. The old image must be removed or the XObject replaced, not merely covered.

- id: pdf-imgrep-03
  answer: |
    The visible graphic may be a vector drawing, text used as artwork, an image inside a Form XObject, transparency group, or pattern rather than a direct page image. Inspect page.get_drawings(), get_image_info(), annotations, and XObjects. To replace it, redact the region and insert the new image, or edit the relevant XObject/content stream.

- id: pdf-imgins-01
  answer: |
    Read the PNG pixel dimensions and compute height_pt = 150 * pixel_height / pixel_width, preserving aspect ratio. Compute a Rect at the bottom-right, leaving margins: x0 = page_width - right_margin - 150 and y0 = page_height - bottom_margin - height_pt; include x1/y1 accordingly. Verify the rect does not intersect existing text/drawings, then use page.insert_image(rect, filename=path, overlay=True, keep_proportion=True).

- id: pdf-imgins-02
  answer: |
    Avoid adding 200 separate image copies by reusing the same image XObject across pages or generating once and referencing it repeatedly. Practical methods include adding the image to a shared resource dictionary, using an existing xref, or post-processing with an optimizer that deduplicates identical streams. Otherwise the file remains correct but bloated.

- id: pdf-imgins-03
  answer: |
    Common causes include an off-page or CropBox-relative wrong rect, use of overlay=False placing the image beneath page content, an opaque filled rectangle drawn after the image, insufficient/ignored rect due to keep_proportion, image alpha/format issues, or a color/rendering problem. Verify rect, page visible area, z-order, and render output.

- id: pdf-fonts-01
  answer: |
    An embedded subset font contains only glyphs for characters already used in the PDF. New letters may have no corresponding glyph, so rendering can show missing-glyph boxes or substitute incorrectly. Use a full font, embed a font with the needed glyph coverage, or choose another available font for the new text.

- id: pdf-fonts-02
  answer: |
    PyMuFITZ commonly uses "helv" for Helvetica regular and "hebo" for Helvetica-Bold. Base-14 fonts are not embedded and depend on the viewer; they generally cover only Latin/StandardEncoding text and lack reliable support for non-Latin scripts and many special characters.

- id: pdf-fonts-03
  answer: |
    Read the original span’s font name, flags, size, and color. Match weight by detecting bold in fontname/flags, then use the same full/built-in font and set the same fontsize and color. Use text measurement to confirm alignment and rendering.

- id: pdf-verify-01
  answer: |
    Render the edited pages to images and inspect them visually; extract text and assert old text is gone, new text exists, and values (including totals) are correct; check geometric positions against expected rules; open in independent PDF viewers or tools; and compare before/after screenshots if useful.

- id: pdf-verify-02
  answer: |
    Not necessarily. With incremental=True, old document objects can remain accessible in prior file revisions, so sensitive data may still be recoverable. Save a full non-incremental rewrite with garbage collection/deflation after redacting, and verify no old content stream remains.

- id: pdf-verify-03
  answer: |
    Never overwrite the original until the edit is validated. Work from a copy, keep a backup, save to a temporary output, compare page counts/metadata, validate rendering and text extraction, then replace the original only after checks pass. Preserve permissions and consider audit logging.

- id: pdf-verify-04
  answer: |
    Successful execution only means no raised exception. It does not prove text is visible, coordinates are valid, fonts are present, redaction removed underlying content, totals are consistent, or layout has no overlap. Explicit output checks are required.
