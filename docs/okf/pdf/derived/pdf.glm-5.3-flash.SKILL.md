---
name: pdf-tuning-glm-5.3-flash
description: >
  Corrective PDF-editing guidance for glm-5.3-flash: PyMuPDF's
  apply_redactions removes covered table rules and shading by default,
  painting boxes is not removal, redact before you move a region, where
  insert_text puts the baseline, growing and re-laying tables without broken
  rules, replace_image is global, and scripts must check return values.
when_to_use: >
  Load when the provider-stripped model id (see stack-detection.md) resolves
  to exactly `glm-5.3-flash` AND the repository contains a PDF (`*.pdf` at
  the root or up to two directories deep — per meta.yaml detection). For any
  other model or a repo without PDFs, do not load.
tuned_for: glm-5.3-flash
tuned_version: "5.3"
stack: pdf
source_scorecard: ../scores/glm-5.3-flash.md
threshold: 0.9
revalidate_when: model_version changes
---
# PDF-editing corrections for glm-5.3-flash

<!-- kaizen:digest -->
**PDF edits (PyMuPDF): redaction defaults, never paint over, baseline:**
1. `page.apply_redactions()` defaults are `images=PDF_REDACT_IMAGE_PIXELS` (2), `graphics=PDF_REDACT_LINE_ART_REMOVE_IF_COVERED` (1) and `text=PDF_REDACT_TEXT_REMOVE` (0). By default every rule/shading shape lying ENTIRELY inside the rect IS removed (partly covered shapes stay whole). To edit one cell and keep its borders and shading, pass `graphics=0, images=0`.
2. Never "remove" rows, rules, paragraphs or text by painting background-coloured boxes. The text stays in the text layer, a render looks identical, and moved content gets duplicated. Remove with `add_redact_annot` + `apply_redactions`, then redraw.
3. Moving content (rows below a deleted row, everything below where a new row or table goes): FIRST redact the whole region that moves, THEN stamp it at the new position with `show_pdf_page(new_rect, untouched_copy, pno, clip=old_rect)`. Content pushed past the page bottom goes onto an inserted page (`doc.new_page(pno + 1)` — it inserts BEFORE the given index).
4. `insert_text(point, ...)`: `point` is the BASELINE (bottom-left). Passing a word's bbox top-left draws the text about one line too HIGH. Use the old span's `origin`.
5. A new row needs the vertical rules extended through it and the striping continued. A new column means measuring the widest text in every narrowed column (`pymupdf.get_text_length`), then shrinking the font slightly or wrapping into taller rows if it does not fit. Draw fills first, then text and rules.
6. `page.replace_image(xref, ...)` changes EVERY page that uses that xref. To replace one occurrence, redact its rect with `images=PDF_REDACT_IMAGE_REMOVE` (1), `graphics=0`, then `insert_image` the new file there.
7. Check return values in the script and fail loudly: `insert_textbox` < 0 wrote nothing, `apply_redactions()` returns False when there were no redaction annots, `search_for` returns `[]` on a miss.
<!-- /kaizen:digest -->

## row-delete: remove, don't cover

- Redaction defaults on PyMuPDF are images=2 (blank overlapping pixels),
  graphics=1 (remove line-art lying entirely inside the rect; partly covered
  shapes stay whole) and text=0 (remove any character whose bbox overlaps).
  "Vector rules are not removed by default" is false.
- Covering a band with a background-colour rect only hides it. The old row
  text is still extractable, and after the rows below are moved, extraction
  shows every moved row twice.
- Procedure:
  1. find_tables → data.
  2. add_redact_annot on the deleted row + all rows/content that move (or the
     whole table).
  3. apply_redactions (defaults clear the old grid under the region).
  4. Redraw the rows one row height higher with rules and alternating
     shading, or stamp them from an untouched copy with
     `show_pdf_page(..., clip=...)`.
  5. Recompute the Total.
  6. Save to a new file and re-extract.
  The order is always remove first, then place.

## cell-edit: keep the grid, place on the baseline

- Replacing one cell's text: redact a rect slightly inset inside the cell and
  apply with `graphics=PDF_REDACT_LINE_ART_NONE, images=PDF_REDACT_IMAGE_NONE`
  so the borders and shading stay. The defaults would delete them.
- `insert_text` takes the baseline start point. Take `origin` from the
  original span (`get_text("dict")`). Right-aligned numbers go at
  `x = old_right_edge - get_text_length(new, fontname, fontsize)` on that
  baseline. A bbox top-left as the point puts the text roughly one line too
  high.

## table-insert: paint order and making room

- Draw in this order: cell and header fills first, then text, then rules. A
  fill drawn after the text hides it on screen while it stays in the text
  layer.
- No gap between two paragraphs: there is no reflow. Redact everything below
  the insertion point (not one paragraph, and never a white rect), restamp it
  lower from an untouched copy with `show_pdf_page(new_rect, src, pno,
  clip=old_rect)`, and put whatever no longer fits on a page inserted after
  it with `doc.new_page(pno + 1)` — it inserts BEFORE the given index. Otherwise place the table in real free
  space or on a new page and tell the user.

## table-relayout: complete grids and content that fits

- Adding a row: redact the Total row and everything below it that moves, draw
  the new row at the old Total position in the next striping colour, redraw
  the Total one row lower with a recomputed value, extend every vertical rule
  down through the new strip, and add the extra horizontal rule. Check free
  space below; if the page runs out, continue on a new page.
- Adding a column inside fixed margins: after choosing narrower widths that
  sum to the old total, measure the widest header and cell text in each
  column with `pymupdf.get_text_length(text, fontname, fontsize)` against the
  new width minus padding. If it does not fit, deliberately shrink the font a
  little or wrap into a taller row. Then redact the old table and redraw every
  column with header, cells, shading and rules at the new x positions.

## image-replace: replace_image is document-wide

- `page.replace_image(xref, filename=...)` swaps the image object itself, so
  every page and placement that references that xref shows the new image.
  Check which pages list the xref in `page.get_images()` before using it.
- To change a single occurrence: `add_redact_annot(rect)`, then
  `apply_redactions(images=PDF_REDACT_IMAGE_REMOVE, graphics=0)`, then
  `insert_image(rect, filename=...)`. Afterwards verify exactly one image sits
  at that rect.

## verify-safety: prove removal, fail fast

- A rendered page cannot tell a covering box from a real edit: both look the
  same. The proof is the text layer of the saved file: `search_for(old)` is
  empty and `get_text()` no longer contains the old value.
- Check every return value inside the script and raise on failure:
  `insert_textbox` returns a negative number when the text did not fit and
  wrote nothing; `apply_redactions()` returns False when the page had no
  redaction annots; `search_for` returns `[]` when the anchor text was not
  found, so a loop over it silently does nothing.
