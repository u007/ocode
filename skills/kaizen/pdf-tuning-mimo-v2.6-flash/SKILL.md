---
name: pdf-tuning-mimo-v2.6-flash
description: >
  Corrective PDF-editing guidance for mimo-v2.6-flash: PyMuPDF's real
  apply_redactions defaults and constants, keeping a cell's rules and shading,
  moving regions without duplicates, APIs that do not exist, subset fonts,
  rotated-page coordinates, and image replace/dedup details.
when_to_use: >
  Load when the provider-stripped model id (see stack-detection.md)
  resolves to exactly `mimo-v2.6-flash`. There is no repo/detection gate: the
  pdf corpus is universal (internal/skill universalStacks), because a
  *.pdf marker file cannot detect the create-from-scratch case, a PDF
  attached from outside the repo, or one deeper than the glob limit.
  For any other model, do not load.
tuned_for: mimo-v2.6-flash
tuned_version: "2.6"
stack: pdf
source_scorecard: ../scores/mimo-v2.6-flash.md
threshold: 0.9
revalidate_when: model_version changes
---
# PDF-editing corrections for mimo-v2.6-flash

<!-- kaizen:digest -->
**PDF edits (PyMuPDF): real redaction constants, no duplicates, full fonts:**
1. `page.apply_redactions()` defaults are `images=PDF_REDACT_IMAGE_PIXELS` (2), `graphics=PDF_REDACT_LINE_ART_REMOVE_IF_COVERED` (1), `text=PDF_REDACT_TEXT_REMOVE` (0). `PDF_REDACT_LINE_ART_REMOVE` and `PDF_REDACT_KEEP_LINES` do NOT exist; to keep rules and shading pass `graphics=PDF_REDACT_LINE_ART_NONE` (0), `images=PDF_REDACT_IMAGE_NONE` (0).
2. Editing one cell: `add_redact_annot(tight_rect)` with NO `fill` (never `fill=(1,1,1)` on a shaded cell), `apply_redactions(images=0, graphics=0)`, then `insert_text` at the old span `origin` (the baseline, not `bbox[3]`), right-aligned via `get_text_length`.
3. Moving a region: open a SEPARATE untouched copy (`show_pdf_page` refuses the same document), redact the deleted row AND the whole region that moves, then `page.show_pdf_page(new_rect, copy, pno, clip=old_rect)`. Without that redaction every moved line is duplicated.
4. `Page.insert_table` and `Page.swap_content_of_page` do NOT exist. Draw tables with `draw_rect`/`draw_line` + `insert_text` (fills FIRST, then text and rules), and never move content by splitting or editing the content stream.
5. `doc.extract_font()` of an `AAAAAA+` subset returns the subset: new characters come out as garbage. Embed the original full TTF/OTF (`insert_font(fontfile=...)`) or a base-14/Noto font, and check `pymupdf.Font(fontfile=...).has_glyph(ord(c))`.
6. On a `/Rotate` page, `get_text`, `search_for` and `insert_*` all use the UNROTATED frame; only `page.rect` is rotated. Visible coords = `rect * page.rotation_matrix`; back with `page.derotation_matrix`.
7. `replace_image(xref, ...)` changes every placement of that xref and keeps the old box, so a different-aspect image is stretched. For a repeated image reuse the returned xref (`insert_image(rect, xref=xref)`); to shrink an already bloated file save with `garbage=4, deflate=True`.
<!-- /kaizen:digest -->

## row-delete: real redaction defaults, redact before you move

- `apply_redactions(images=2, graphics=1, text=0)` are the defaults:
  - `images=PDF_REDACT_IMAGE_PIXELS` (2) blanks the overlapping image pixels;
    `PDF_REDACT_IMAGE_REMOVE` is 1, `PDF_REDACT_IMAGE_NONE` is 0.
  - `graphics=PDF_REDACT_LINE_ART_REMOVE_IF_COVERED` (1) removes vector shapes
    lying ENTIRELY inside the rect; shapes it only partly covers stay whole.
    Removing anything merely touched is `PDF_REDACT_LINE_ART_REMOVE_IF_TOUCHED`
    (2). `PDF_REDACT_LINE_ART_NONE` (0) keeps all line art.
  - `text=PDF_REDACT_TEXT_REMOVE` (0).
- `PDF_REDACT_LINE_ART_REMOVE` and `PDF_REDACT_KEEP_LINES` do not exist, and
  there is no "move" mode of `add_redact_annot`.
- Moving the rows below a deleted row up with `show_pdf_page`:
  1. `copy = pymupdf.open(path)`: a separate, untouched copy. Passing the
     document you are editing as the source raises "source document must not
     equal target".
  2. `add_redact_annot` over the deleted row AND the whole region that moves
     (remaining rows, Total, anything below), then `apply_redactions()`.
     Keep the default graphics removal so the old rules and shading go too.
  3. `page.show_pdf_page(old_rect - (0, h, 0, h), copy, pno, clip=old_rect)`.
  4. Recompute the Total, save to a new file, re-extract.
  Skipping step 2 leaves the originals in place, so the moved rows appear
  twice in the text layer.

## cell-edit: keep the grid, no white fill, place on the baseline

- Redact a rect that covers the old glyphs and stays inside the cell. Any
  character whose bbox overlaps the rect is removed whole, even when the rect
  covers only part of it.
- `add_redact_annot(rect)` defaults to no fill. Do not pass `fill=(1,1,1)`: on
  a shaded cell it paints a white box over the shading.
- Apply with `graphics=PDF_REDACT_LINE_ART_NONE, images=PDF_REDACT_IMAGE_NONE`
  so the rules and shading stay.
- Insert at the old span's `origin` (from `get_text("dict")`), which is the
  baseline. `bbox[3]` lies below the baseline by the descender. For a
  right-aligned number: `x = old_x1 - pymupdf.get_text_length(new, fontname,
  fontsize)`.
- Base-14 fonts (`helv`, `hebo`, ...) are never embedded
  (`doc.get_page_fonts(pno, full=True)` shows ext `n/a`).

## table-insert: no insert_table, paint order, make room by restamping

- `Page.insert_table` does not exist. Draw the table: fills with
  `draw_rect(..., color=None, fill=...)`, then text with `insert_text`, then the
  rules with `draw_line`. A fill drawn after the text hides it on screen while
  the text stays in the text layer.
- `Page.swap_content_of_page` does not exist. To make room for a table between
  paragraphs, redact everything below the insertion point and restamp it lower
  with `show_pdf_page(clip=...)` from an untouched copy; spill onto a new page
  if it runs past the bottom. Never split or rewrite the content stream.

## fonts: a subset stays a subset

- `doc.extract_font(xref)` of an `AAAAAA+Name` font returns only the subset
  glyphs. Re-embedding that buffer with `insert_font(fontbuffer=...)` does not
  add new glyphs: characters that were not in the subset re-extract as
  garbage (`\x00`).
- Use the original full font file (`page.insert_font(fontname=..., fontfile=
  "Font.ttf")`), else the closest base-14 or Noto font. Check coverage on the
  full font: `pymupdf.Font(fontfile=...).has_glyph(ord(c))` is non-zero for
  every new character. Then re-extract the saved output.
- Take the baseline from span `origin`, not `bbox[3]`.

## pdf-model: rotated pages keep the unrotated frame

- After `set_rotation(90)`, `search_for` and `get_text("words")` return the
  same bboxes as before, and `insert_text` at an extracted point lands next
  to that text. Only `page.rect` changes (it becomes the rotated size).
- To get what the viewer sees: `visible = rect * page.rotation_matrix`; back
  with `visible * page.derotation_matrix`. Never assume extraction is in the
  visible frame.

## table-relayout: regenerate or edit in place, never round-trip

- For a table change, first ask whether the original source (template, HTML,
  reportlab code, spreadsheet, Word file) exists, and regenerate the PDF from
  it. Otherwise do a surgical in-place edit with PyMuPDF.
- PDF→DOCX/HTML→PDF is a last resort only when exact fidelity does not matter.
- Moving content down to grow a table: redact the region and restamp it with
  `show_pdf_page(clip=...)` from an untouched copy, as above.

## image-replace: shared xrefs and aspect ratio

- `page.replace_image(xref, filename=...)` swaps the image object and keeps
  every placement's box: all pages that use that xref change, and an image of
  a different aspect is stretched into the old box.
- One placement only, or keep the aspect: `add_redact_annot(old_rect)`,
  `apply_redactions(images=PDF_REDACT_IMAGE_REMOVE)`, then
  `insert_image(old_rect, filename=..., keep_proportion=True)`, which centres
  the image in the rect. Other pages keep the old image.

## image-insert: one image object, dedup on save

- `xref = page.insert_image(rect, filename=...)` once, then
  `insert_image(rect, xref=xref)` on the other pages.
- A file already holding many identical copies: save with
  `garbage=4, deflate=True`, which merges identical image streams.
