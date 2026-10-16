---
name: pdf-tuning-nvidia-nemotron-3.5-super-vl
description: >
  Corrective PDF-editing guidance for nvidia/nemotron-3.5-super-vl: rotated-page
  coordinates (get_text bboxes stay unrotated); PyMuPDF's real apply_redactions
  defaults and the explicit keep flags; never cover content with a fill rect;
  show_pdf_page region moves; insert_textbox is all-or-nothing; column relayout
  with measured widths and a full redraw; regenerate from source before any
  round-trip; page.replace_image and single-occurrence image swaps; insert_image
  xref reuse and garbage=4 dedup.
when_to_use: >
  Load when the provider-stripped model id (see stack-detection.md) resolves to
  exactly `nvidia/nemotron-3.5-super-vl`. There is no repo/detection gate: the
  pdf corpus is universal (internal/skill universalStacks), because a *.pdf
  marker file cannot detect the create-from-scratch case, a PDF attached from
  outside the repo, or one deeper than the glob limit. For any other model, do
  not load.
tuned_for: nvidia/nemotron-3.5-super-vl
tuned_version: "3.5"
stack: pdf
source_scorecard: ../scores/nvidia__nemotron-3.5-super-vl.md
threshold: 0.9
revalidate_when: model_version changes
---
# PDF-editing corrections for nvidia/nemotron-3.5-super-vl

<!-- kaizen:digest -->
**PDF edits (PyMuPDF): exact API facts:**
1. On a `/Rotate` page, `get_text` bboxes and every insert/draw rect are in the UNROTATED page's coordinates; only `page.rect` swaps width and height. Pass extracted bboxes straight back to insert/draw calls. Displayed coordinates are `rect * page.rotation_matrix`.
2. `apply_redactions()` defaults: `images=PDF_REDACT_IMAGE_PIXELS` (2, blanks overlapping pixels), `graphics=PDF_REDACT_LINE_ART_REMOVE_IF_COVERED` (1, deletes every line/fill lying inside the rect), `text=PDF_REDACT_TEXT_REMOVE` (0). Graphics and images are NOT kept by default. To change a cell's text and keep its borders and shading, pass `graphics=0, images=0` explicitly.
3. Never "cover" text, a cell value, a logo or vector art with a background-coloured fill rect and draw on top. That is not an edit: the old content stays extractable and shows through. Remove it (redaction, `replace_image`) and then insert.
4. PyMuPDF CAN move a region: redact it, then `page.show_pdf_page(new_rect, untouched_copy_of_original, pno, clip=old_rect)`. Use this to close the gap after a deleted row, to push content down for an inserted row or table, and to spill overflow onto a new page. Never leave the gap or re-type the rows.
5. `insert_textbox` is all-or-nothing: if the text does not fit it writes NOTHING, returns a negative number and raises nothing. Check the return value; then shrink the font, wrap into a taller row, or widen the column.
6. Adding a column: new widths sum to the same total width; measure every cell's text at its new width with `get_text_length` and shrink or wrap deliberately; redact the WHOLE old table with the default graphics removal, then redraw header fill, shading, rules and text at the new x positions.
7. No gap for a new table between two paragraphs: never shrink or re-typeset the existing paragraphs. Move everything below down (rule 4), or place the table in free space / on a new page and tell the user, or regenerate from the original source. Never overlap.
8. Before any PDF → DOCX/HTML → PDF round-trip, ask whether the original source exists and regenerate from it; otherwise edit in place. The round-trip is a last resort.
9. Replace an image everywhere with `page.replace_image(xref, filename=...)` (one shared XObject IS swapped in place). For ONE occurrence: redact that rect with `images=PDF_REDACT_IMAGE_REMOVE`, then `insert_image` there. A vector logo: redact with graphics removal, then `insert_image`. Find nested placements with `page.get_image_info(xrefs=True)` / `get_images(full=True)`.
10. `insert_image` returns the image xref; pass `xref=that_xref` on later pages. `doc.save(..., garbage=4, deflate=True)` DOES merge identical image objects (levels 0–3 keep the copies).
11. After deleting a row, recompute the Total, subtotal and tax from the remaining rows and shift any content below the table too.
<!-- /kaizen:digest -->

## pdf-model: rotated pages and the round-trip

- PyMuPDF uses a top-left origin with y down. Extraction (`get_text`,
  `search_for`, `get_image_rects`) and insertion (`insert_text`,
  `insert_image`, `draw_*`, `add_redact_annot`) share one system, so a bbox
  from extraction is passed straight back.
- That shared system is the UNROTATED page. On a `/Rotate 90/270` page
  `page.rect` reports the rotated (displayed) size, but `get_text("words")`
  bboxes and the rects you pass to insert/draw calls are not in that frame.
  Convert to displayed coordinates with `rect * page.rotation_matrix` and
  back with `rect * page.derotation_matrix`. Anchor new content to extracted
  content bboxes, not to `page.rect.width/height`.
- CropBox offset: `page.rect` starts at (0, 0) at the CropBox's top-left.
  Raw content-stream coordinates are PDF user space (y up, MediaBox-based).
  Convert with `point * ~page.transformation_matrix` and back with
  `* page.transformation_matrix`.
- When someone proposes PDF → DOCX/HTML → PDF, first ask whether the original
  source (template, HTML, reportlab code, spreadsheet) exists and regenerate
  from it. Otherwise make a surgical in-place edit. Name those two options
  before listing the round-trip's risks.

## row-delete: redaction defaults and the direct region move

- `apply_redactions()` defaults, verified: `images=2` (`PDF_REDACT_IMAGE_PIXELS`)
  blanks the pixels under the rect; `graphics=1`
  (`PDF_REDACT_LINE_ART_REMOVE_IF_COVERED`) removes every line and fill lying
  entirely inside the rect; `text=0` removes every character whose bbox
  overlaps. A row redaction therefore already deletes the row's shading rect
  and inner rules without any extra flag. The opposite belief, that graphics
  and images survive by default, is wrong.
- To remove text while keeping a cell's borders and shading, pass
  `graphics=PDF_REDACT_LINE_ART_NONE, images=PDF_REDACT_IMAGE_NONE` (both 0).
- When removing a row and rebuilding the band, keep the default graphics
  removal so the row's rules and fill go too, then redraw.
- Move the rows below up one row height in one step: open an untouched copy
  of the source (`src = pymupdf.open(path)`), redact the deleted row plus the
  region that moves, apply, then
  `page.show_pdf_page(old_rect - (0, row_h, 0, row_h), src, pno, clip=old_rect)`.
  The stamped region is a Form XObject whose text stays extractable and whose
  appearance is identical. Do not re-emit operators by hand, do not re-type
  the rows, and never "accept the blank gap".
- Then fix the rule at the new bottom edge, recompute Total, subtotal and tax
  from the remaining rows (not by patching the old figure), keep striping
  consistent, and move or deliberately leave the content below the table.

## cell-edit: real removal, the keep flags, and insert_textbox

- Replacing a cell value: redact a rect that covers the glyphs and stays
  inside the cell, applied with `graphics=0, images=0` so the rules and
  shading stay. Covering the glyph area with a background-coloured fill rect
  is never an option: the old value remains in the text layer.
- Measure the new text with `pymupdf.get_text_length(text, fontname, fontsize)`
  and insert at `x = old_right_edge - width` on the old span's `origin`
  baseline, with the span's font, size and colour.
- `insert_textbox` writes nothing when the text does not fit. It returns a
  negative number (the height shortfall) and raises no exception. It never
  writes a partial line. Check the return value every time; on a negative
  value shrink the font slightly, wrap into a taller row and shift the rows
  below, widen the column by re-laying out the table, or use
  `insert_htmlbox` scaling.

## table-relayout: measure, remove, redraw; move with show_pdf_page

- Adding a column: choose new widths that sum to the SAME total width
  between the margins. Then measure the widest text in every column at its
  new width (`get_text_length`) and decide per column: shrink the font
  slightly or wrap into a taller row. Do not just shift the columns and
  hope the text still fits.
- Redact the whole old table with the default graphics removal and redraw
  every column at the new x positions: header fill, alternating shading,
  vertical and horizontal rules, then text. Shifting only text and vertical
  rules leaves the old header fill and shading where they were.
- Inserting a row above the Total: redact the Total row and everything below
  that moves, draw the new row in the right striping colour, redraw the Total
  one row lower with a recomputed value, extend the vertical rules, add the
  horizontal rule, and move the content below with
  `show_pdf_page(clip=)` from an untouched copy. If the page lacks room,
  continue on a new page.
- Before any round-trip through DOCX or HTML, ask for the original source
  and regenerate; else edit in place.

## table-insert: when there is no gap

- Existing paragraphs cannot be re-typeset in place. Never propose shrinking
  their font size or line spacing, and never shrink the new table below the
  document's own style to squeeze it into whitespace that does not exist.
- Make room: redact the region below the insertion point and restamp it
  lower with `page.show_pdf_page(new_rect, src, pno, clip=old_rect)`; if it
  then runs past the bottom margin, spill the remainder onto a newly inserted
  page the same way.
- Otherwise place the table in real free space or on a new page and tell the
  user where it went, or regenerate the document from its source. Never draw
  over existing content.

## image-replace: replace_image and single-occurrence swaps

- Find the placement with `page.get_image_info(xrefs=True)` or
  `page.get_image_rects(xref)`, then `page.replace_image(xref, filename=...)`.
  One shared XObject IS swapped in place, so every page that draws that xref
  shows the new image and the placement rect is unchanged. A
  different-aspect PNG is stretched into the old box.
- To change ONE occurrence of a shared image: redact that rect with
  `images=PDF_REDACT_IMAGE_REMOVE`, apply, then `insert_image(rect,
  filename=...)` there. `PDF_REDACT_IMAGE_PIXELS` only blanks pixels and keeps
  the object and placement; a background-coloured fill rect is a cover-up,
  not a replacement.
- A logo that `get_images()` does not list may be vector paths
  (`get_drawings`), outlined text, an image inside a Form XObject
  (`get_images(full=True)`, `get_image_info`), an inline image, or an
  annotation appearance. Vector art cannot be image-replaced: redact it with
  graphics removal, then `insert_image` the new file. Do not cover it.

## image-insert: reuse the xref, trust garbage=4

- `insert_image` returns the image xref. Insert once from the file, then pass
  `xref=that_xref` on every later page so all pages reference one object.
- `doc.save(path, garbage=4, deflate=True)` merges identical objects, so
  duplicate image streams collapse to one. Garbage levels 0–3 only drop
  unreferenced objects and keep the copies. Confirm with `doc.get_page_images`
  before and after.
