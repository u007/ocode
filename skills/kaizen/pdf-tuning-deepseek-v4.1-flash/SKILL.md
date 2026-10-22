---
name: pdf-tuning-deepseek-v4.1-flash
description: >
  Corrective PDF-editing guidance for deepseek-v4.1-flash: what PyMuPDF's
  apply_redactions actually removes by default, deleting a table row with real
  removal (a show_pdf_page clip hides content, it does not remove it), keeping
  a cell's grid when replacing its text, paint order for new tables, anchoring
  inserted images to real content, and matching header style in new columns.
when_to_use: >
  Load when the provider-stripped model id (see stack-detection.md)
  resolves to exactly `deepseek-v4.1-flash`. There is no repo/detection gate: the
  pdf corpus is universal (internal/skill universalStacks), because a
  *.pdf marker file cannot detect the create-from-scratch case, a PDF
  attached from outside the repo, or one deeper than the glob limit.
  For any other model, do not load.
tuned_for: deepseek-v4.1-flash
tuned_version: "4.1-2026-10-21"
stack: pdf
source_scorecard: ../scores/deepseek-v4.1-flash.md
threshold: 0.9
revalidate_when: model_version changes
---
# PDF-editing corrections for deepseek-v4.1-flash

<!-- kaizen:digest -->
**PDF edits (PyMuPDF): real removal, correct defaults, no leftovers:**
1. `page.apply_redactions()` defaults are `images=PDF_REDACT_IMAGE_PIXELS` (2), `graphics=PDF_REDACT_LINE_ART_REMOVE_IF_COVERED` (1) and `text=PDF_REDACT_TEXT_REMOVE` (0). It is NOT `LINE_ART_NONE`: it deletes every rule/shading shape lying ENTIRELY inside the rect (partly covered shapes stay whole, they are not "touched → removed") and blanks image pixels. To change only text (one cell), pass `graphics=0, images=0`; if you redraw the region anyway, keep the defaults.
2. Deleting a row: redact the deleted row AND every row or line that will move (or the whole table), apply, THEN redraw the moved rows one row higher, or stamp them with `show_pdf_page(new_rect, src_copy, pno, clip=old_rect)` where `src_copy` is a copy in which the deleted row was ALSO redacted and applied. Never leave the deleted row on the page under a stamp.
3. `show_pdf_page(clip=...)` HIDES the outside of the clip; it does not remove it. The stamped XObject embeds the whole source page, so stamping from an unredacted copy (or rebuilding a page from clipped bands) still carries the deleted row in its bytes, and `get_text()` will NOT show it. Real removal = redaction (or rebuilding the rows from extracted data).
4. Don't use `graphics=PDF_REDACT_LINE_ART_REMOVE_IF_TOUCHED` on a band that touches vertical rules running into rows that stay: the whole rule is deleted, not just the part inside the band.
5. Drawing a new table: fills first, then text, then rules.
6. Insert images relative to real content (right edge = table/text x1, bottom = above the footer's top), never `page.rect` minus a fixed margin. One image on many pages: insert it once, keep the returned xref, then `page.insert_image(rect, xref=xref)` on the other pages (one stored copy). Identical bytes inserted again into the same document already reuse one xref, and `save(garbage=4)` merges duplicate streams (levels 0-3 do not). There is no `Document.insert_image`.
7. In a stamp source, redact ONLY what is being deleted. For a pure move (no row deleted), stamp from an untouched copy; never redact the region you are about to stamp, or the stamp comes out empty.
8. A new table column copies the existing header style exactly: text colour (white on a dark header), font, size and fill, read from the header spans (`get_text("dict")`).
<!-- /kaizen:digest -->

## row-delete: what redaction removes, real removal, and the order of operations

- The defaults of `apply_redactions` are `images=2` (blank the overlapping
  pixels), `graphics=1` (`REMOVE_IF_COVERED`: remove line art lying entirely
  inside the rect; partly covered shapes stay whole) and `text=0` (remove any
  character whose bbox overlaps). "Line art is left untouched by default" is
  false, and so is "the default deletes every drawing touching the rect". With
  defaults, a redaction over a whole row deletes that row's shading rectangle
  and any rule lying entirely inside the rect.
- Pass `graphics=PDF_REDACT_LINE_ART_NONE, images=PDF_REDACT_IMAGE_NONE` when
  you want the grid and shading kept. When you rebuild the rows anyway, keep
  the defaults and redraw the rules and shading afterwards.
- `REMOVE_IF_TOUCHED` (2) deletes a whole path that merely crosses the rect.
  On a band below the deleted row it also deletes the table's vertical rules
  for the rows that stay above; don't use it there.
- `page.show_pdf_page(rect, src, pno, clip=...)` stamps a Form XObject that
  contains the entire source page; the clip only sets what is visible. Content
  outside the clip is still in the file and recoverable, and `get_text()` does
  not report it. Building a new page from clipped bands that skip the deleted
  row, or stamping from an unredacted copy, is therefore NOT removal.
- Delete-and-shift procedure:
  1. find_tables → rows and data. Make a copy of the document and redact +
     apply the deleted row in that copy too; this is the stamp source.
  2. On the target page, redact the deleted row plus the whole region below
     it that moves (remaining rows, Total, notes), or the whole table.
  3. apply_redactions.
  4. Redraw the rows shifted up one row height, or stamp the moved region
     from the redacted copy with `show_pdf_page(..., clip=...)` one row
     higher.
  5. Recompute the Total from the data.
  6. Save to a new file with `garbage=4, deflate=True`, then confirm the
     deleted row's strings are absent from `get_text()` of the output. That
     check cannot see clipped-away content, so a stamped page is clean only
     if its stamp source was redacted.
  Never stamp onto a region whose old content is still on the page.
- The stamp source is redacted only for what is being DELETED. When rows only
  move (an insert, a relayout), stamp from an untouched copy: redacting the
  region you are about to stamp produces an empty stamp.

## cell-edit: keep the grid when replacing one cell's text

- Replacing one cell's text keeps the cell's borders and shading only with
  `apply_redactions(graphics=0, images=0)`. The defaults delete shading that
  lies entirely inside the redaction rect.
- Keep the redaction rect tight to the glyphs and inside the cell; never pad
  it outward to catch rules or "be safe". Re-extract the neighbouring cells
  afterwards to confirm they are intact.

## table-insert: paint order

- Draw a new table in this order: cell and header fills first, then the
  text, then the rules. A fill drawn after the text hides it in the render,
  while the text is still in the text layer.

## image-insert: anchor to content

- The target rect comes from the document's content, not from page margins:
  x1 = the table's (or text's) right edge, and y1 = the top of the footer
  minus padding. Height = width × h/w of the source pixels. Before inserting,
  check that the rect intersects no word, drawing or image bbox.
- The same image on many pages is stored once: insert it on the first page,
  keep the returned xref, and call `page.insert_image(rect, xref=xref)` on the
  rest. Re-inserting identical bytes into the same document also reuses one
  xref, and `save(garbage=4, deflate=True)` merges duplicate streams (e.g.
  after `insert_pdf`); garbage levels 0-3 do not. There is no
  `Document.insert_image`; insertion is always per page.

## Observed in live editing: new cells inherit the neighbouring style

- When adding a column, give the new header cell the same text colour, font
  and size as the other header cells. Read them from the spans on the header
  line; the header text is white (`color` 0xFFFFFF) on a dark fill. Give new
  body cells the body style of their row.
