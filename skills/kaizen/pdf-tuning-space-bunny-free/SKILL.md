---
name: pdf-tuning-space-bunny-free
description: >
  Corrective PDF-editing guidance for space-bunny-free: move table regions as
  vectors (show_pdf_page clip), never as rasters; PyMuPDF's real
  apply_redactions defaults; insert_textbox's negative return; baseline
  placement; bold flag bits and subset-font glyph coverage; image reuse and
  overlay; rotated-page coordinates; relayout and make-room procedures.
when_to_use: >
  Load when the provider-stripped model id (see stack-detection.md)
  resolves to exactly `space-bunny-free`. There is no repo/detection gate: the
  pdf corpus is universal (internal/skill universalStacks), because a
  *.pdf marker file cannot detect the create-from-scratch case, a PDF
  attached from outside the repo, or one deeper than the glob limit.
  For any other model, do not load.
tuned_for: space-bunny-free
tuned_version: "alpha"
stack: pdf
source_scorecard: ../scores/space-bunny-free.md
threshold: 0.9
revalidate_when: model_version changes
---
# PDF-editing corrections for space-bunny-free

<!-- kaizen:digest -->
**PDF edits (PyMuPDF): vectors not rasters, exact API facts:**
1. Never move or rebuild table content by rasterising it (`get_pixmap` → `insert_image`). The text becomes a picture that can't be searched or extracted. To shift content on the same page (delete a row, make room for a table), redact the region, then `page.show_pdf_page(new_rect, untouched_copy_of_doc, pno, clip=old_rect)`; whatever would pass the page bottom goes onto a new page. Or redraw from the extracted data.
2. `apply_redactions()` defaults are `images=PDF_REDACT_IMAGE_PIXELS` (2), `graphics=PDF_REDACT_LINE_ART_REMOVE_IF_COVERED` (1) and `text=PDF_REDACT_TEXT_REMOVE` (0). Keep the default graphics removal when removing or rebuilding a region or a whole table, so the old rules and fills don't linger. Pass `graphics=0, images=0` only to keep a cell's borders and shading while changing its text.
3. `insert_textbox` returns a NEGATIVE number and writes NOTHING when the text doesn't fit (no exception). Check `rc < 0`.
4. `insert_text(point)`: `point` is the baseline. A bbox top-left draws the text about one line too HIGH. Use the span `origin`.
5. Span `flags`: bold = `flags & 16`, italic = `flags & 2`. Confirm with the span's font name.
6. Reuse an image with `xref = page.insert_image(...)` then `insert_image(rect, xref=xref)`. Saving with `garbage=4, deflate=True` merges identical images (lower levels keep the copies). `overlay=False` puts the image UNDER existing content (it is not a no-op).
7. On a `/Rotate` page, `get_text` bboxes stay in UNROTATED page coordinates (only `page.rect` swaps). Pass them straight back to insert/draw calls; use `rect * page.rotation_matrix` for displayed coordinates.
<!-- /kaizen:digest -->

## pdf-model: coordinates, rotation, raw streams

- PyMuPDF uses a top-left origin with y down. Its extraction and its
  insert/draw methods share that system, so a bbox from `get_text` can be
  passed straight back.
- `/Rotate` pages: `page.rect` shows the rotated (displayed) size, but
  `get_text("words")` bboxes are NOT in displayed coordinates. They stay in
  the unrotated page's system, the same one `insert_text`/`draw_rect` use.
  Convert to displayed coordinates with `rect * page.rotation_matrix` and
  back with `* page.derotation_matrix`.
- CropBox offset from the MediaBox: `page.rect` starts at (0, 0) at the
  CropBox's top-left corner. Raw content-stream coordinates are PDF user space
  (y up, not shifted by the CropBox). Convert a PyMuPDF point to raw stream
  coordinates with `point * ~page.transformation_matrix`, and back with
  `* page.transformation_matrix`.
- Content streams are usually Flate-compressed, so a byte search on the raw
  stream finds nothing. `page.read_contents()` or `doc.xref_stream(xref)`
  return decoded bytes. Even decoded, prefer redact + insert over editing the
  text operators.

## row-delete: move regions as vectors

- Rasterising the rows below a deleted row and pasting the pixmap is wrong.
  The moved rows lose their text layer, so search, copy and your own
  `get_text` verification all fail.
- Vector region move on the same page:
  1. Open an untouched copy of the source (`src = pymupdf.open(path)`).
  2. Redact the deleted row + the region that moves.
  3. Apply with the default graphics removal (1), so the old rules and
     shading go too.
  4. `page.show_pdf_page(old_rect - (0, row_h, 0, row_h), src, pno,
     clip=old_rect)`.
  5. Fix the rules at the new bottom edge and recompute the Total.
  The alternative is to rebuild the rows from `find_tables().extract()`.
- `apply_redactions` defaults: images=2 blanks the overlapping pixels (it does
  not remove the whole image), graphics=1 removes line-art lying entirely
  inside the rect (partly covered shapes stay whole; 2 removes anything
  touched), and text=0 removes any character whose bbox overlaps. There is no
  `add_text` parameter.

## cell-edit: the exact return values and placement

- `insert_textbox(rect, text, ...)` returns a float. It is >= 0 when the text
  fit (spare height) and negative when it did not. On a negative return
  NOTHING was written. Test `rc < 0` and then shrink the font, wrap into a
  taller row, or widen the column.
- `insert_text` places the baseline start at `point`. Use the original span's
  `origin`. Right-aligned numbers go at `x = right_edge -
  get_text_length(text, fontname, fontsize)`.
- Match the weight from the span: bold is `flags & 16` (or a "Bold" font
  name). Italic is `flags & 2`. The size is `span["size"]`.

## table-relayout: remove the old table, keep the width, prefer the source

- Adding a column: pick new widths that sum to the SAME total width between
  the margins, then measure every cell's text at its new width
  (`get_text_length`) and shrink or wrap deliberately where it no longer fits.
- Redact the whole old table with the default graphics removal (1), not
  `PDF_REDACT_LINE_ART_NONE`. With NONE the old rules and header fill stay at
  the old x positions under the redrawn grid. Then redraw every column: header,
  cells, shading, vertical and horizontal rules.
- Before any PDF → DOCX/HTML → PDF round-trip, ask whether the original source
  (template, HTML, reportlab code, spreadsheet) exists and regenerate from it.
  Otherwise do a surgical in-place edit. The round-trip is a last resort
  because it changes fonts, positions and other pages.
- To move content that sits below a grown table, redact it and restamp it with
  `show_pdf_page(clip=...)`. Rewriting `Td`/`Tm` numbers in the stream moves
  text only; the rules, fills and images stay where they were.

## table-insert: no reflow, so make room or go elsewhere

- A PDF does not reflow. To fit a table between two paragraphs:
  1. Redact everything below the insertion point.
  2. Restamp it lower with `show_pdf_page(shifted_rect, untouched_copy, pno,
     clip=region)`.
  3. Stamp whatever would pass the page bottom onto a new page inserted after
     it (`doc.new_page(pno + 1, width=..., height=...)`). Re-fetch `doc[pno]`
     afterwards: inserting a page invalidates existing `Page` objects.
- Other options: put the table in real free space or on a new page and tell
  the user, or regenerate from source. Never draw the table over existing
  text, even inside a padded block bbox.
- `insert_htmlbox` writes ordinary page content and creates no annotation.
  Adding annotations and baking them is not a better way to place a table.

## fonts: subset coverage and weight detection

- An embedded subset font (`AAAAAA+Name`) only holds the glyphs the document
  used. Check coverage of the replacement font before inserting:
  `pymupdf.Font(fontfile=path).has_glyph(ord(c))` returns 0 for a missing
  glyph. Then embed it with `page.insert_font(fontname=..., fontfile=path)`
  and confirm by re-extracting the text.
- Span flags: 1 superscript, 2 italic, 4 serif, 8 monospaced, 16 bold. The
  flag bits are a hint, not the authority. Also read the span's font name
  ("Helvetica-Bold") before choosing the matching font (for example `hebo`)
  and `span["size"]`.

## image-insert: reuse, dedupe, layering

- `insert_image` returns the image xref. Pass `xref=` on later pages to
  reference the same object. `doc.save(..., garbage=4, deflate=True)`
  merges identical streams, and PyMuPDF may reuse an identical image within
  a session anyway. Never rewrite a page's content stream by hand
  (`update_object`) to "reuse" an image.
- `overlay=True` (the default) draws on top. `overlay=False` puts the image
  beneath the existing page content, for example under a filled background
  box, where it can be hidden.
