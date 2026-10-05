---
name: pdf-tuning-longcat-2.5-preview-free
description: >
  Corrective PDF-editing guidance for longcat-2.5-preview-free: move regions
  with show_pdf_page after redacting, keep grid lines when redacting a cell,
  insert_textbox writes nothing on overflow, re-laying out columns and rows,
  replace_image is global, and verify the saved text layer.
when_to_use: >
  Load when the provider-stripped model id (see stack-detection.md)
  resolves to exactly `longcat-2.5-preview-free`. There is no repo/detection
  gate: the pdf corpus is universal (internal/skill universalStacks).
  For any other model, do not load.
tuned_for: longcat-2.5-preview-free
tuned_version: "2.5-preview"
stack: pdf
source_scorecard: ../scores/longcat-2.5-preview-free.md
threshold: 0.9
revalidate_when: model_version changes
---
# PDF-editing corrections for longcat-2.5-preview-free

## row-delete: remove, then stamp the moved region

- Redact the deleted row AND every row/content that will move (or the whole
  table) with `add_redact_annot` + `apply_redactions`. Redacting only the row
  and re-inserting the rest duplicates the moved content.
- PyMuPDF does have a region move: keep an untouched copy of the document,
  then `page.show_pdf_page(new_rect, src_doc, pno, clip=old_rect)` stamps the
  original region one row higher with identical appearance. Redact the old
  region first.
- Then recompute the Total from the remaining data and save to a new file.

## cell-edit: keep the grid, overflow returns negative

- Redact a rect inset inside the cell (glyphs covered, neighbours untouched)
  and pass `graphics=PDF_REDACT_LINE_ART_NONE, images=PDF_REDACT_IMAGE_NONE`
  (both 0). The defaults remove rules and shading lying entirely inside the
  rect.
- `insert_textbox` writes NOTHING when the text does not fit. It returns a
  negative number (the shortfall) and raises no exception, so check the value.
  Then shrink the font slightly, wrap into a taller row (shifting rows below),
  widen the column, or use `insert_htmlbox` with `scale_low`.

## table-relayout: whole-table redraw with measured widths

- New column inside fixed margins: choose narrower widths summing to the old
  total, measure the widest header/cell text per column with
  `pymupdf.get_text_length(text, fontname, fontsize)` against width minus
  padding, then shrink the font slightly or wrap into taller rows. Redact the
  old table and redraw every column (header, cells, shading, vertical and
  horizontal rules) at the new x positions. PDFs have no grid metadata to
  update.
- New row: redact the Total row and everything below that moves, draw the new
  row at the old Total position, redraw the Total one row lower with a
  recomputed value, extend the vertical rules, add the extra horizontal rule,
  continue on a new page if space runs out.
- PDF to DOCX/HTML round-trip is a last resort that changes fonts, layout and
  other pages. Prefer regenerating from the original source if one exists,
  otherwise a surgical in-place edit.

## table-insert: no reflow, make room or relocate

- Between two paragraphs with no gap: redact everything below the insertion
  point, restamp it lower with `show_pdf_page(..., clip=...)`, and put
  overflow on an inserted page. Otherwise use real free space or a new page and
  tell the user, or regenerate from source. Never shrink the surrounding text
  or overlap existing content.
- Draw fills first, then text, then rules; copy font, rule width, colour and
  padding from the page.

## image-replace: replace_image and aspect ratio

- `page.replace_image(xref, filename=...)` swaps the image object, so every
  placement of that xref changes. Find the xref and rect with
  `get_images()` / `get_image_info(xrefs=True)` / `get_image_rects`.
- To change one occurrence: redact its rect with
  `images=PDF_REDACT_IMAGE_REMOVE, graphics=0`, then `insert_image` at the rect.
- A PNG with a different aspect ratio is stretched into the old box. Compute
  the fitted rect from the pixel size.

## image-insert: dedupe repeated images

- Insert once, keep the returned xref, pass `xref=` on later pages. Saving with
  `garbage=4` also merges identical image streams (levels 0-3 keep copies).

## pdf-model: coordinates and structure

- Raw PDF is y-up from the bottom-left; PyMuPDF is y-down from the top-left of
  the unrotated page, shared by extraction and insert APIs. Convert raw numbers
  with `page.rotation_matrix` / `page.derotation_matrix` and account for a
  CropBox offset.

## verify-safety: prove it on the saved file

- A render cannot tell a covering box from a real edit. Re-open the saved
  output: `search_for(old)` is empty, `get_text()` lacks the old value,
  `find_tables().extract()` equals the expected rows, other pages' text is
  unchanged. Then render for gaps, broken rules and misalignment.
- Silent failures in a script: `insert_textbox` < 0 wrote nothing,
  `apply_redactions()` returns False with no redaction annots, `search_for`
  returns `[]` so a loop does nothing. Check each return value and raise.
- Save to a new file, using `garbage=3` or `4` for real removal.
  `incremental=True` keeps the old revision, so redacted content stays
  recoverable.
