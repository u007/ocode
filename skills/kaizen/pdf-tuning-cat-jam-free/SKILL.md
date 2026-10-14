---
name: pdf-tuning-cat-jam-free
description: >
  Corrective PDF-editing guidance for cat-jam-free: rotated-page coordinates
  (get_text bboxes stay unrotated); apply_redactions defaults and keep flags;
  moving page regions with show_pdf_page(clip=) without flattening;
  insert_textbox's negative return; column relayout with measured widths and a
  full redraw; making room between paragraphs; page.replace_image; verifying
  edits on the text layer, not by render alone.
when_to_use: >
  Load when the provider-stripped model id (see stack-detection.md)
  resolves to exactly `cat-jam-free`. There is no repo/detection gate:
  the pdf corpus is universal (internal/skill universalStacks), because a
  *.pdf marker file cannot detect the create-from-scratch case, a PDF
  attached from outside the repo, or one deeper than the glob limit.
  For any other model, do not load.
tuned_for: cat-jam-free
tuned_version: "alpha"
stack: pdf
source_scorecard: ../scores/cat-jam-free.md
threshold: 0.9
revalidate_when: model_version changes
---
# PDF-editing corrections for cat-jam-free

<!-- kaizen:digest -->
**PDF edits (PyMuPDF, import `pymupdf`): exact API facts:**
1. On a `/Rotate` page, `get_text` bboxes and every insert/draw rect are in the UNROTATED page's coordinates; only `page.rect` swaps width and height. Pass extracted bboxes straight back; convert to displayed coordinates with `page.rotation_matrix` / back with `page.derotation_matrix`.
2. `apply_redactions()` defaults: `images=PDF_REDACT_IMAGE_PIXELS` (2, blanks overlapping pixels), `graphics=PDF_REDACT_LINE_ART_REMOVE_IF_COVERED` (1, removes line art lying entirely inside the rect), `text=PDF_REDACT_TEXT_REMOVE` (0). To change text and keep rules/shading/images, pass `graphics=0, images=0` explicitly.
3. Moving content never happens by itself: redact the deleted row AND every region that must move, then restamp it with `page.show_pdf_page(new_rect, untouched_copy_of_original, pno, clip=old_rect)`. The stamp is a Form XObject: vector text stays extractable, nothing is rasterized. Never use a pixmap for this.
4. `insert_textbox` returns a NEGATIVE number (height shortfall) when the text does not fit and writes NOTHING, with no exception. Check `rc < 0` and fail loudly; options are smaller font, wrap into a taller row and shift rows below, widen the column, or `insert_htmlbox` (scales down).
5. Adding a column: new widths sum to the SAME total width; measure every cell at its new width (`pymupdf.get_text_length`); redact the WHOLE old table, then redraw header fill, shading, every rule and all text at the new x positions.
6. Inserting where there is no gap: redact everything below the insertion point and restamp it lower with `show_pdf_page(clip=...)`; spill overflow onto an inserted page. Or use free space / a new page and tell the user. Never overlap. Before any PDF → DOCX/HTML → PDF round-trip, ask for the original source and regenerate from it.
7. Swap an image everywhere with `page.replace_image(xref, filename=...)` (placement unchanged; a different aspect ratio gets stretched into the old box). One occurrence only: redact its rect with `images=PDF_REDACT_IMAGE_REMOVE`, then `insert_image`.
8. Verify on the saved file's text layer: old value absent, new present, `find_tables().tables[0].extract()` equals expected rows, other pages' text unchanged. A render alone cannot tell a white-box overlay from a real edit.
<!-- /kaizen:digest -->

## pdf-model: coordinate frames and silent failures

- PyMuPDF uses a top-left origin with y down; extraction (`get_text`,
  `search_for`, `get_image_rects`) and insertion (`insert_text`,
  `insert_image`, `draw_*`, `add_redact_annot`) share one system.
- That shared system is the UNROTATED page. On a `/Rotate 90/270` page,
  `page.rect` reports the displayed size, but extracted bboxes and the rects
  you pass to insert/draw calls are not in that frame. Displayed =
  `rect * page.rotation_matrix`; back = `rect * page.derotation_matrix`.
- CropBox offset: `page.rect` starts at (0, 0) at the CropBox's top-left.
  Raw content-stream coordinates are PDF user space (y up, MediaBox-based);
  convert with `page.transformation_matrix` and its inverse.
- Many edit calls fail silently: `insert_textbox` returns < 0 and writes
  nothing, `search_for` returns `[]` so the loop does nothing,
  `apply_redactions` returns `False` when there was no annotation. Check each
  return value in the script and raise; "it printed done" proves nothing.
- Prefer regenerating from the original source (template, HTML, reportlab,
  spreadsheet) when it exists; ask for it. A PDF → DOCX → PDF round-trip is a
  last resort even for "simple" documents.

## row-delete: remove what moves, then restamp

- A row delete is: redact the deleted row AND the rows below, the Total row
  and any content under the table (or the whole table), apply, then redraw
  the remaining rows one row height higher with text, alternating shading and
  rules, and the Total recomputed from the remaining data. Shifting without
  first removing leaves the old copies in place.
- Region move preserving exact appearance: keep an untouched copy
  (`src = pymupdf.open(path)`), redact the old region on the target page, then
  `page.show_pdf_page(old_rect - (0, row_h, 0, row_h), src, pno, clip=old_rect)`.
  The result is vector and extractable; it is not flattened.
- When rebuilding the band, keep the default graphics removal so the row's
  fill and rules go too, then redraw them.

## cell-edit: redaction flags and insert_textbox

- Redact a single cell with a rect that covers the glyphs and stays inset in
  the cell, and pass `graphics=PDF_REDACT_LINE_ART_NONE,
  images=PDF_REDACT_IMAGE_NONE` (both 0) so borders and shading survive. The
  flags are the control, not the rect size.
- `insert_textbox(rect, text, ...)` returns the unused height: `>= 0` means
  written, `< 0` means nothing was written. It is not a character count.

## table-relayout: measure, remove, redraw

- Adding a column to a full-width table: shrink the other columns so the total
  width stays between the margins (never widen the page). Measure the widest
  text in each column at its new width and decide per column: shrink the font
  slightly or wrap into a taller row.
- Redact the whole old table, then redraw every column: header fill,
  alternating shading, vertical and horizontal rules, then text. Drawing only
  a new rule and new cells leaves the old layout underneath.
- Adding a row above the Total: redact the Total and everything below it,
  redraw the new row, then the Total one row lower with a recomputed value.

## table-insert: making room between paragraphs

- There is no reflow. To make room, redact the content below the insertion
  point and restamp it one table-height lower with
  `show_pdf_page(clip=...)` from an untouched copy; if it runs past the page
  bottom, `doc.new_page()` / `insert_page` and stamp the overflow there.
- Otherwise place the table in existing free space or on a new page and tell
  the user where it went. Never draw over existing content.
- A table stamped from reportlab or `insert_htmlbox` keeps its text real; an
  `insert_image` of a rendered table does not.

## image-replace: replace_image (low-n)

- Find the xref and placement with `page.get_images()` and
  `page.get_image_rects(xref)` / `page.get_image_info(xrefs=True)`, then
  `page.replace_image(xref, filename="new.png")`. This swaps the object in
  place: every page drawing that xref changes.
- The new image is stretched into the old rect; check the aspect ratio first
  and adjust the rect (via redact + `insert_image`) if it differs.

## verify-safety: prove it on the text layer

- Re-open the saved output and check: old value gone (`search_for` returns
  `[]`), new values present, `find_tables().tables[0].extract()` equals the expected
  rows in order, other pages' text and the page count unchanged.
- Then render (`get_pixmap`) for gaps, broken rules and misalignment. A render
  is the secondary check: it cannot distinguish an overlay from a real edit.
- If the PDF is signed or encrypted, say that editing invalidates the
  signature or needs the password.
