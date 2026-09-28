---
name: pdf-tuning-deepseek-v4.1-flash
description: >
  Corrective PDF-editing guidance for deepseek-v4.1-flash: what PyMuPDF's
  apply_redactions actually removes by default, deleting a table row without
  duplicating the rows you move, what insert_textbox does when text doesn't
  fit, removing the old table before a re-layout, paint order for new tables,
  anchoring inserted images to real content, deduplicating repeated images,
  and matching header style in new columns.
when_to_use: >
  Load when the provider-stripped model id (see stack-detection.md) resolves
  to exactly `deepseek-v4.1-flash` AND the repository contains a PDF
  (`*.pdf` at the root or up to two directories deep — per meta.yaml
  detection). For any other model or a repo without PDFs, do not load.
tuned_for: deepseek-v4.1-flash
tuned_version: "4.1"
stack: pdf
source_scorecard: ../scores/deepseek-v4.1-flash.md
threshold: 0.9
revalidate_when: model_version changes
---
# PDF-editing corrections for deepseek-v4.1-flash

<!-- kaizen:digest -->
**PDF edits (PyMuPDF): real removal, correct defaults, no duplicates:**
1. `page.apply_redactions()` defaults are `images=PDF_REDACT_IMAGE_PIXELS` (2), `graphics=PDF_REDACT_LINE_ART_REMOVE_IF_COVERED` (1) and `text=PDF_REDACT_TEXT_REMOVE` (0): it DOES delete every rule/shading shape lying ENTIRELY inside the rect (partly covered shapes stay whole) and blanks image pixels. To change only text, pass `graphics=0, images=0`; if you redraw the region anyway, keep the defaults.
2. Deleting a row: redact the deleted row AND every row or line that will move (or the whole table) BEFORE you redraw or stamp it one row higher (`show_pdf_page(new_rect, untouched_copy, pno, clip=old_rect)`); stamping first duplicates every moved row.
3. `insert_textbox` that doesn't fit writes NOTHING and returns a negative number (no exception): always check the return value, then shrink the font, wrap into a taller row, widen the column, or use `insert_htmlbox`, which scales the text down to fit.
4. Re-laying out a table (new column or row): redact the whole old table first, then redraw every column with header fill, row shading, rules and text at the new positions; content pushed past the page bottom goes onto a new page (`doc.new_page(pno + 1)` — it inserts BEFORE the given index).
5. Drawing a new table: fills first, then text, then rules; take font and size from the page's spans and rule width, colour and header fill from `page.get_drawings()`.
6. Insert images relative to real content (right edge = table/text x1, bottom = above the footer's top), never `page.rect` minus a fixed margin; reuse the xref `insert_image` returns and save with `garbage=4, deflate=True` to merge identical image streams.
7. A new table column copies the existing header style exactly: text colour (white on a dark header), font, size and fill, read from the header spans (`get_text("dict")`).
<!-- /kaizen:digest -->

## row-delete: what redaction removes, and the order of operations

- The defaults of `apply_redactions` are `images=2` (blank the overlapping
  pixels), `graphics=1` (remove line-art lying entirely inside the rect;
  partly covered shapes stay whole) and `text=0` (remove any character whose
  bbox overlaps). "Line art is left untouched" is false. With defaults, a
  redaction over a whole row deletes that row's shading rectangle and any rule
  lying entirely inside the rect.
- Use `graphics=PDF_REDACT_LINE_ART_NONE, images=PDF_REDACT_IMAGE_NONE` only
  when you want the grid and shading kept, for example when replacing text
  inside one cell. When you rebuild the rows anyway, removing the old grid is
  what you want. Redraw the rules and shading afterwards.
- Delete-and-shift procedure:
  1. find_tables → rows and data.
  2. Redact the deleted row plus the whole region below it that moves
     (remaining rows, Total, notes), or the whole table.
  3. apply_redactions.
  4. Redraw the rows shifted up one row height, or stamp the region from an
     untouched copy of the original with `show_pdf_page(..., clip=...)`.
  5. Recompute the Total from the data.
  6. Save to a new file and re-extract.
  Never stamp a shifted copy while the original rows are still on the page.

## cell-edit: text that doesn't fit, and keeping the grid

- `insert_textbox(rect, text, ...)` returns a float. `>= 0` is the unused
  height; a negative value means the text did not fit, and then **nothing is
  written** (not a truncated string) and no exception is raised. Check the
  return value every time.
- Options when it doesn't fit: reduce the font size a little; wrap into more
  lines and make the row taller, shifting the rows below; widen the column by
  re-laying out the table; or use `insert_htmlbox(rect, text, css=...)`, which
  scales the text down to fit (default `scale_low=0`) and returns
  `(spare_height, scale)`. Never let text spill over the cell border.
- Replacing one cell's text keeps the cell's borders and shading only with
  `apply_redactions(graphics=0, images=0)`; the defaults delete shading that
  lies inside the redaction rect.
- Right-aligned values: the right edge is the existing value's word x1 (cell
  x1 minus padding), not the cell border. Place new text at
  `x = right_edge - pymupdf.get_text_length(new, fontname, fontsize)` on the
  old baseline.
- A redaction removes every character whose bbox merely overlaps the rect, so
  a padded rect eats the neighbouring cell's edge characters. Keep the rect
  inside the cell, and re-extract the neighbouring cells afterwards to confirm
  they are intact.

## table-relayout: remove the old table, redraw everything, prefer the source

- Adding a column to a full-width table: extract the data, choose new column
  widths that sum to the same total width, measure the widest text per column,
  then redact the WHOLE old table and redraw every column: header fill, row
  shading, vertical and horizontal rules, and cell text at the new x
  positions. Redrawing over the old table without redacting it leaves the old
  text in the text layer.
- Adding a row or moving content down: redact the region that moves, then
  redraw or stamp it lower. If it now runs past the page bottom margin,
  continue the table on a new page inserted after the current one
  (`doc.new_page(pno + 1)` — it inserts BEFORE the given index), with the header repeated.
- Converting PDF → DOCX/HTML → PDF to edit a table is a last resort, only when
  exact fidelity doesn't matter. First ask whether the original source
  (template, HTML, reportlab code, spreadsheet) exists and regenerate from it;
  otherwise make a surgical in-place edit of the PDF.

## table-insert: paint order and borrowed style

- Draw a new table in this order: cell and header fills first, then the
  text, then the rules. A fill drawn after the text hides it in the render,
  while the text is still in the text layer.
- Copy the style from the page: font and size from `get_text("dict")` spans,
  rule width and colour and the header fill from `page.get_drawings()`
  (`width`, `color`, `fill`), and the left edge from the existing content. Keep
  cell padding consistent and right-align numbers by measured width.
- Check every `insert_textbox` return value (see cell-edit); a negative result
  means an empty cell.

## image-insert: anchor to content, reuse the image

- The target rect comes from the document's content, not from page margins:
  x1 = the table's (or text's) right edge, and y1 = the top of the footer
  minus padding. Height = width × h/w of the source pixels. Before inserting,
  check that the rect intersects no word, drawing or image bbox.
- Inserting the same image on many pages: `xref = page.insert_image(rect,
  filename=...)` once, then `insert_image(rect, xref=xref)` on the other pages.
  A file that is already bloated with identical image copies is fixed by
  saving with `garbage=4, deflate=True`, which merges identical streams.

## Observed in live editing: new cells inherit the neighbouring style

- When adding a column, give the new header cell the same text colour, font
  and size as the other header cells. Read them from the spans on the header
  line; the header text is white (`color` 0xFFFFFF) on a dark fill. Give new
  body cells the body style of their row.
