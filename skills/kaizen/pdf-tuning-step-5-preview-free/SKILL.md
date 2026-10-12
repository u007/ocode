---
name: pdf-tuning-step-5-preview-free
description: >
  Corrective PDF-editing guidance for step-5-preview-free: rotated-page
  coordinates (get_text bboxes stay unrotated); PyMuPDF's real apply_redactions
  image default and the explicit keep flags; direct show_pdf_page region moves;
  column relayout with measured widths and a full redraw; regenerate from
  source before any round-trip; page.replace_image and single-occurrence image
  swaps; insert_image xref reuse and garbage=4 dedup.
when_to_use: >
  Load when the provider-stripped model id (see stack-detection.md)
  resolves to exactly `step-5-preview-free`. There is no repo/detection gate:
  the pdf corpus is universal (internal/skill universalStacks), because a
  *.pdf marker file cannot detect the create-from-scratch case, a PDF
  attached from outside the repo, or one deeper than the glob limit.
  For any other model, do not load.
tuned_for: step-5-preview-free
tuned_version: "5-preview"
stack: pdf
source_scorecard: ../scores/step-5-preview-free.md
threshold: 0.9
revalidate_when: model_version changes
---
# PDF-editing corrections for step-5-preview-free

<!-- kaizen:digest -->
**PDF edits (PyMuPDF): exact API facts:**
1. On a `/Rotate` page, `get_text` bboxes and every insert/draw rect are in the UNROTATED page's coordinates; only `page.rect` swaps width and height. Pass extracted bboxes straight back to insert/draw calls; never anchor to `page.rect.width/height` on a rotated page. Displayed coordinates are `rect * page.rotation_matrix`.
2. `apply_redactions()` defaults: `images=PDF_REDACT_IMAGE_PIXELS` (2, blanks only the overlapping pixels, never deletes the image), `graphics=PDF_REDACT_LINE_ART_REMOVE_IF_COVERED` (1), `text=PDF_REDACT_TEXT_REMOVE` (0). To change a cell's text and keep its borders and shading, pass `graphics=0, images=0` explicitly; do not rely on the rect being small.
3. Move a region on the same page directly: redact it, then `page.show_pdf_page(new_rect, untouched_copy_of_original, pno, clip=old_rect)`. No temp document is needed.
4. Adding a column: new widths sum to the same total width; measure every cell's text at its new width with `get_text_length` and shrink or wrap deliberately; redact the WHOLE old table with the default graphics removal, then redraw header fill, shading, rules and text at the new x positions.
5. Before any PDF → DOCX/HTML → PDF round-trip, ask whether the original source exists and regenerate from it; otherwise edit in place. The round-trip is lossy even when the PDF came from Word.
6. Replace an image everywhere with `page.replace_image(xref, filename=...)`. For ONE occurrence: redact that rect with `images=PDF_REDACT_IMAGE_REMOVE`, then `insert_image` the new file there. Never `delete_image(xref)` + `insert_image` on one page: `delete_image` blanks the shared xref on every page.
7. `insert_image` returns the image xref; pass `xref=` on later pages. `doc.save(..., garbage=4, deflate=True)` DOES merge identical image objects (levels 0–3 keep the copies). Do not hand-edit `Resources/XObject` or append `Do` operators to content streams.
<!-- /kaizen:digest -->

## pdf-model: rotated pages and coordinate frames

- PyMuPDF uses a top-left origin with y down. Extraction (`get_text`,
  `search_for`, `get_image_rects`) and insertion (`insert_text`,
  `insert_image`, `draw_*`, `add_redact_annot`) share one system, so a bbox
  from extraction is passed straight back.
- That shared system is the UNROTATED page. On a `/Rotate 90/270` page
  `page.rect` reports the rotated (displayed) size, but `get_text("words")`
  bboxes and the rects you pass to insert/draw calls are not in that frame.
  Convert to displayed coordinates with `rect * page.rotation_matrix` and
  back with `rect * page.derotation_matrix`. Computing a "bottom-right" anchor
  from `page.rect.width` and `page.rect.height` on a rotated page lands off
  the visible area; anchor to extracted content bboxes instead.
- CropBox offset: `page.rect` starts at (0, 0) at the CropBox's top-left.
  Raw content-stream coordinates are PDF user space (y up, MediaBox-based).
  Convert with `point * ~page.transformation_matrix` and back with
  `* page.transformation_matrix`.

## row-delete: redaction defaults and the direct region move

- `apply_redactions()` defaults, verified: `images=2` (`PDF_REDACT_IMAGE_PIXELS`)
  blanks the pixels under the rect and keeps the image object and placement;
  `PDF_REDACT_IMAGE_REMOVE` (1) is what deletes an overlapping image, and it
  must be requested. `graphics=1` removes line art lying entirely inside the
  rect (2 removes anything touched, 0 keeps all). `text=0` removes every
  character whose bbox overlaps.
- To remove text while keeping a cell's borders and shading, pass
  `graphics=PDF_REDACT_LINE_ART_NONE, images=PDF_REDACT_IMAGE_NONE` (both 0).
  A tight rect alone is not the control; the flags are.
- When removing a row and rebuilding the band, keep the default graphics
  removal so the row's rules and fill go too, then redraw.
- Move the rows below up one row height in one step: open an untouched copy
  of the source (`src = pymupdf.open(path)`), redact the deleted row plus the
  region that moves, apply, then
  `page.show_pdf_page(old_rect - (0, row_h, 0, row_h), src, pno, clip=old_rect)`.
  The stamped region is a Form XObject whose text stays extractable. No temp
  document or temp page is needed. Then fix the rule at the new bottom edge
  and recompute the Total from the remaining rows.

## table-relayout: measure, remove, redraw; prefer the source

- Adding a column: choose new widths that sum to the SAME total width
  between the margins. Then measure the widest text in every column at its
  new width (`pymupdf.get_text_length(text, fontname, fontsize)`) and decide
  per column: shrink the font slightly or wrap into a taller row. Do not just
  shift the columns and hope the text still fits.
- Redact the whole old table with the default graphics removal (1) and redraw
  every column at the new x positions: header fill, alternating shading,
  vertical and horizontal rules, then text. Shifting only "the vertical rules
  and the text" leaves the old header fill and shading where they were.
- Before any PDF → DOCX/HTML → PDF round-trip, ask whether the original
  source (template, HTML, reportlab code, spreadsheet) exists and regenerate
  from it. Otherwise make a surgical in-place edit. The round-trip is a last
  resort and is lossy even when the PDF was produced from Word or HTML.

## image-replace: replace_image and single-occurrence swaps

- Find the placement with `page.get_image_info(xrefs=True)` or
  `page.get_image_rects(xref)`, then `page.replace_image(xref, filename=...)`.
  The placement rect is unchanged because only the image object is swapped.
  A different-aspect PNG is stretched into the old box.
- An xref is document-level: `replace_image` (and `delete_image`, which
  replaces the object with a blank pixmap) changes every page that draws it.
  `delete_image(xref)` followed by `insert_image` on page 1 therefore leaves
  every other page with a blank logo.
- To change ONE occurrence of a shared image: redact that rect with
  `images=PDF_REDACT_IMAGE_REMOVE`, apply, then `insert_image(rect,
  filename=...)` there. The other pages keep the old image.

## image-insert: reuse the xref, trust garbage=4

- `insert_image` returns the image xref. Insert once from the file, then pass
  `xref=that_xref` on every later page so all pages reference one object. Do
  not hand-edit `Resources/XObject` with `xref_set_key` or append `q cm /Im Do Q`
  to content streams.
- `doc.save(path, garbage=4, deflate=True)` merges identical objects, so
  duplicate image streams collapse to one. Garbage levels 0–3 only drop
  unreferenced objects and keep the copies. Confirm with `doc.get_page_images`
  or `len(doc.xref_length())` before and after.
