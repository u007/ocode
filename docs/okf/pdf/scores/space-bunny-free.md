---
model_id: space-bunny-free
model_version: "alpha"
evaluated_via: opencode-go
evaluated_on: 2026-09-28
stack: pdf
stack_corpus_rev: 1
threshold: 0.9
---

<!-- Filename: model_id with "/" flattened to "__" so it is one valid path
     segment. `space-bunny-free` has no slash, so the filename is unchanged. -->

# Scorecard — space-bunny-free on pdf

> Valid ONLY for `space-bunny-free` @ `alpha`. A version bump invalidates this
> scorecard — re-benchmark.

Answers: `pdf/answers/space-bunny-free.md` (closed-book, audited zero tool
calls). Graded against `pdf/questions.yaml` @ corpus_rev 1. PyMuPDF facts in
the key were verified on 1.27.1; answers stating a wrong PyMuPDF behaviour do
not earn the affected point.

## Per-question results

| id | tags | weight | full | awarded | normalized | notes |
|----|------|-------:|-----:|--------:|-----------:|-------|
| pdf-model-01 | pdf-model | 3 | 2 | 2 | 1.00 | also floats "overpaint/cover the region" and a raster blit as the practical path (see row-delete) |
| pdf-model-02 | pdf-model, verify-safety | 3 | 2 | 2 | 1.00 | |
| pdf-model-03 | pdf-model | 2 | 2 | 2 | 1.00 | TJ split/hex/CID covered; never mentions Flate compression; length-shift point earned via byte-count corruption + redact/insert recommendation |
| pdf-model-04 | pdf-model | 2 | 2 | 1 | 0.50 | y-flip point ok; rotation point lost: **wrong behaviour** — says on a `/Rotate` page `get_text("words")` bboxes "are already in displayed coordinates" (verified on 1.27.1: they stay in unrotated-page coords while `page.rect` swaps); the matrix name `page.rotation_matrix * page.cropbox_matrix` is dubious |
| pdf-locate-01 | table-locate | 3 | 2 | 2 | 1.00 | |
| pdf-locate-02 | table-locate | 2 | 2 | 2 | 1.00 | |
| pdf-locate-03 | table-locate, cell-edit | 2 | 2 | 2 | 1.00 | flag bits listed correctly here (16 = bold) — contradicts pdf-fonts-01/03 |
| pdf-locate-04 | table-locate, table-relayout | 2 | 2 | 2 | 1.00 | |
| pdf-rowdel-01 | row-delete | 3 | 3 | 2 | 0.67 | moves the rows below as a **raster** `get_pixmap` → `insert_image` blit, so the remaining rows' text becomes an image (not extractable) — fails "keep text"; redacts with `graphics=LINE_ART_NONE`, leaving the old rules/shading under the blit. Removal + Total recompute ok |
| pdf-rowdel-02 | row-delete, cell-edit | 3 | 2 | 1 | 0.50 | **wrong default**: says `graphics` default is `PDF_REDACT_LINE_ART_REMOVE_IF_TOUCHED` (it is `REMOVE_IF_COVERED`, 1); says fully covered images are removed (default `IMAGE_PIXELS` blanks pixels); invents an `add_text=` param. Keep-graphics fix (graphics=0/images=0) correct |
| pdf-rowdel-03 | row-delete | 2 | 2 | 1 | 0.50 | calls the raster blit "the direct answer"; frames `show_pdf_page(clip=…)` as for "other pages"/"block on its own page", never as stamping a clipped region from a copy of the same original one row higher. Redact-before-move shown |
| pdf-rowdel-04 | row-delete, verify-safety | 2 | 2 | 2 | 1.00 | |
| pdf-cell-01 | cell-edit | 3 | 3 | 3 | 1.00 | |
| pdf-cell-02 | cell-edit | 2 | 2 | 2 | 1.00 | |
| pdf-cell-03 | cell-edit, table-relayout | 2 | 2 | 1 | 0.50 | **wrong API behaviour**: claims `insert_textbox` truncates, writes the part that fits and returns a *positive* number on misfit / ≤0 on fit (reality: nothing written, negative return, no exception); its "if > 0 the text did not fit" check is inverted. Options (shrink, widen, relayout) ok |
| pdf-cell-04 | cell-edit | 2 | 2 | 1 | 0.50 | baseline point correct; **wrong direction**: says top-left makes text drawn "one cap-height too low … into the row below" (it is drawn ~a line too high) |
| pdf-relayout-01 | table-relayout | 3 | 3 | 3 | 1.00 | uses raster blit again but explicitly caveats the text-searchability loss and offers rebuild |
| pdf-relayout-02 | table-relayout | 3 | 3 | 2 | 0.67 | redistribute-widths + re-fit ok; redacts the old table with `graphics=LINE_ART_NONE`, so old rules/header fill stay at the old x positions under the redrawn grid — not "remove old table"; "common mistake" paragraph garbled ("keeping sum equal to the old total … overflows") |
| pdf-relayout-03 | table-relayout, pdf-model | 2 | 2 | 1 | 0.50 | fidelity risks well covered; never says prefer regenerating from the original source / ask whether one exists; recommends "overlay/redaction on the original page" |
| pdf-relayout-04 | table-relayout, table-insert | 2 | 2 | 2 | 1.00 | |
| pdf-tblins-01 | table-insert | 3 | 2 | 2 | 1.00 | reportlab/WeasyPrint → `show_pdf_page` as third option; `insert_textbox` negative-on-misfit stated correctly here (contradicts pdf-cell-03) |
| pdf-tblins-02 | table-insert | 2 | 2 | 2 | 1.00 | |
| pdf-tblins-03 | table-insert, table-relayout | 2 | 2 | 1 | 0.50 | "move content down" only via raw `Td/Tm` rewriting (misses graphics, no page-overflow handling, no redact+restamp); false claims: `insert_htmlbox` "uses an annotation appearance stream", annotation+bake as "most reliable"; suggests "draw over it anyway". New page/after last paragraph alternative earns point 2 |
| pdf-imgrep-01 | image-replace | 3 | 3 | 3 | 1.00 | primary path redact+insert; names `doc.replace_image` (it is `page.replace_image`) and a nonexistent `doc.replace_xref` |
| pdf-imgrep-02 | image-replace, verify-safety | 2 | 2 | 2 | 1.00 | |
| pdf-imgrep-03 | image-replace | 2 | 2 | 2 | 1.00 | |
| pdf-imgins-01 | image-insert | 3 | 2 | 2 | 1.00 | anchors to page.rect − 36pt margin instead of real content edges, but asserts no overlap with words/drawings |
| pdf-imgins-02 | image-insert | 2 | 2 | 1 | 0.50 | `insert_image(xref=…)` reuse stated, but first code block is fabricated and destructive (`doc.update_object(page.get_contents()[0], "q 0 0 0 0 0 0 cm Q")` wipes the page, `page._replace_image_on_page`); **false**: "`garbage=4` … **cannot** merge 200 referenced copies" and "PyMuPDF does not deduplicate" |
| pdf-imgins-03 | image-insert, pdf-model | 2 | 2 | 1 | 0.50 | overlay point lost: **wrong behaviour** — after naming `overlay=False` it says "in current PyMuPDF `overlay=False` is a no-op/back-compat shim … treat `overlay` as unreliable". Coordinate point earned (empty/off-page rect; rotation/CropBox not named) |
| pdf-fonts-01 | fonts | 3 | 2 | 2 | 1.00 | no `has_glyph` coverage check (verifies by extraction); flag bits wrong again ("bit 1 = bold") |
| pdf-fonts-02 | fonts | 2 | 2 | 2 | 1.00 | |
| pdf-fonts-03 | fonts, cell-edit | 2 | 2 | 1 | 0.50 | **wrong flags**: "`2` = bold … `(flags & 2)` → bold", "`16` = monospaced" (bold is 16, 2 is italic) and calls flags "the authoritative" source → italic text detected as bold. Matching bold font + exact size ok |
| pdf-verify-01 | verify-safety | 3 | 3 | 3 | 1.00 | |
| pdf-verify-02 | verify-safety | 3 | 2 | 2 | 1.00 | |
| pdf-verify-03 | verify-safety | 2 | 2 | 2 | 1.00 | encryption + untouched regions covered; no signature mention; advises clearing metadata / `doc.scrub()`, and claims `clean=True` removes JavaScript |
| pdf-verify-04 | verify-safety, pdf-model | 2 | 2 | 2 | 1.00 | |

`normalized = min(awarded, full) / full`

## Per-tag subscores

| tag | subscore | n | trust | action |
|-----|---------:|--:|-------|--------|
| pdf-model | 0.81 | 7 | ok | **derive** |
| table-locate | 1.00 | 4 | ok | omit (strong) |
| row-delete | 0.65 | 4 | ok | **derive** |
| cell-edit | 0.72 | 7 | ok | **derive** |
| table-relayout | 0.75 | 7 | ok | **derive** |
| table-insert | 0.89 | 4 | ok | **derive** |
| image-replace | 1.00 | 3 | low-n | omit (strong) |
| image-insert | 0.71 | 3 | low-n | **derive** (mark low-n) |
| fonts | 0.86 | 3 | low-n | **derive** (mark low-n) |
| verify-safety | 1.00 | 7 | ok | omit (strong) |

`subscore = Σ(normalized×weight) / Σ(weight)` over that tag's questions.

## Stack score

```
stack_score = Σ(normalized×weight) / Σ(weight) = 73.5 / 86 = 85.5%
```

## Live behavioural probe — editing a real PDF (2026-09-28)

Model driven with `ocode run -yolo` in a fresh git repo containing an invoice PDF: a ruled, shaded table, a
logo, and a page 2 that must stay unchanged. One task per run. The output is checked on the **text layer** by
`docs/okf/pdf/probe/check.py`, which rejects white-box overlays, so passing requires real removal, not just a
correct-looking render. Renders were also inspected by eye. The harness is in `docs/okf/pdf/probe/`.

| task | baseline | with skill (digest injected) |
|---|---|---|
| delete-row | PASS (1135s) | PASS (916s) |
| edit-cell | PASS (219s) | PASS (323s) |
| add-row | PASS (1076s) | PASS (477s) |
| add-column | PASS (1135s) | PASS (308s) |
| insert-table | PASS (636s) | PASS (297s) |
| edit-cell-subset | PASS (244s) | PASS (328s) |
| replace-image | PASS (279s) | PASS (463s) |
| insert-image | PASS (259s) | PASS (5995s) |

Several runs hit opencode-go 529 "Endpoint is unavailable" and were rerun; the results shown are the completed reruns. Loaded the bundled `pdf` skill once in 8 runs. Baseline runs used a pre-fix binary, so no Kaizen digest was injected. The with-skill runs used a
binary built after the `ocode run` bundled-skills fix; digest injection was verified by having the model quote
the digest from its system prompt. Times above ~5,800s are wall clock across a host sleep; the session's
monotonic clock shows about 500s of actual work.

## Derivation targets

Tags below threshold (`< 0.9`): **pdf-model, row-delete, cell-edit,
table-relayout, table-insert, image-insert (low-n), fonts (low-n)** → feed into
`derived/pdf.space-bunny-free.SKILL.md`.

**pdf-model (0.81)** — concrete wrong/missing claims:
- Says that on a `/Rotate` page `get_text("words")` bboxes "are already in
  displayed coordinates". Verified on 1.27.1: they stay in the unrotated
  page's coordinates while `page.rect` swaps width and height. The displayed
  position is `rect * page.rotation_matrix`. Its raw-stream transform name
  `page.rotation_matrix * page.cropbox_matrix` is dubious; PyMuPDF → raw PDF
  user space is `point * ~page.transformation_matrix`, which includes the
  CropBox offset.
- Raw-stream text replacement: never mentions that content streams are
  usually Flate-compressed, so a byte search on the raw stream finds nothing.
- PDF → DOCX → PDF round-trip: never says to prefer regenerating from the
  original source or to ask whether one exists. Recommends "overlay/redaction
  on the original page" instead.
- `overlay=False` called "a no-op/back-compat shim … treat `overlay` as
  unreliable". False: it puts the image beneath the existing content.

**row-delete (0.65)** — concrete wrong/missing claims:
- Moves the rows below a deleted row by rasterising them:
  `pix = page.get_pixmap(clip=clip_below, dpi=300)` … `page.insert_image(…, pixmap=pix)`,
  and calls this "the direct answer" / "the usual answer".
  The moved rows' text becomes a picture: not searchable, not extractable, and
  its own verify step (`get_text` rows in order) would then fail.
- Does not know that `page.show_pdf_page(new_rect, copy_of_original, pno, clip=old_rect)`
  on the *same* page is the vector region-move; frames it as for "other pages"
  or "a block on its own page".
- Redacts the region to be moved with `graphics=PDF_REDACT_LINE_ART_NONE`
  "so you … keep the rules/shading", leaving the old grid and shading at the old
  positions (they only disappear because an opaque raster covers them).
- Wrong `apply_redactions` defaults: "`graphics=PDF_REDACT_LINE_ART_REMOVE_IF_TOUCHED`
  (the default)" — the default is `REMOVE_IF_COVERED` (1); says a fully covered
  image is removed (default `IMAGE_PIXELS`, 2, blanks the overlapping pixels);
  invents "`add_text=fitz.TEXT_PRESERVE_WHITESPACE` is the default".

**cell-edit (0.72)** — concrete wrong/missing claims:
- `insert_textbox` semantics inverted: "the inserted portion is truncated and
  the function returns the remaining vertical space (a positive number) … If it
  fits, it returns a number ≤ 0"; "if it is > 0 the text did not fit". Reality:
  on misfit nothing is written, the return is negative, no exception.
- `insert_text` from the bbox top-left: says the text is drawn "roughly one
  cap-height *too low* — it drops into the row below". It is drawn about a line
  too high (the baseline lands at the old text's top).
- Span flags: "`2` = **bold** … `16` = monospaced … `(flags & 2)` → bold" and
  calls flags "the authoritative" weight signal. Bold is `flags & 16`; 2 is
  italic. It gave the right bits elsewhere, so the knowledge is unstable.
- (shared with row-delete) the wrong `apply_redactions` graphics default above.

**table-relayout (0.75)** — concrete wrong/missing claims:
- Adding a column: redacts the old table with
  `images=NONE, graphics=LINE_ART_NONE, text=REMOVE`, so the old rules and
  header fill stay at the old x positions under the redrawn grid. The default
  graphics removal (1) is what clears them. The "common mistake" paragraph is
  garbled ("keeping the sum of the column widths equal to the *old* total … so
  the table overflows"); the rule is that the new widths sum to the same total
  width between the margins.
- PDF → DOCX round-trip: no "regenerate from the original source if it
  exists / ask for it"; recommends overlay/redaction as "the right tool".
- `insert_textbox` misfit semantics inverted (shared with cell-edit).
- Making room below: only via rewriting `Td`/`TD`/`Tm` numbers in the content
  stream, which moves text but not rules, fills or images, with no handling of
  content pushed past the page bottom (shared with table-insert).

**table-insert (0.89)** — concrete wrong/missing claims:
- "Move content down" offered only as raw `Td/Tm` rewriting. Missing: redact
  the region below the insertion point, restamp it lower with
  `show_pdf_page(clip=…)`, and spill what passes the page bottom onto a new
  page.
- False: `insert_htmlbox` "internally uses an appearance stream" and
  annotation + `doc.bake` is "the most reliable way to add arbitrary drawn
  content". Verified: `insert_htmlbox` writes page content and creates no
  annotation.
- Suggests "draw over it anyway" inside a padded block bbox, i.e. overlapping
  existing content.

**image-insert (0.71, low-n)** — concrete wrong/missing claims:
- "`page.insert_image` stores a new image XObject on every page. PyMuPDF does
  not deduplicate" and "`garbage=4, deflate=True` will drop orphaned images but
  **cannot** merge 200 legitimately referenced copies". Both false: garbage ≥ 3
  merges identical objects, and PyMuPDF may reuse an identical image within a
  session.
- Offers a fabricated, destructive xref-reuse snippet:
  `doc.update_object(page.get_contents()[0], "q 0 0 0 0 0 0 cm Q")` (overwrites
  the page's content stream) and `page._replace_image_on_page(...)` (not a
  public API). Takes the xref from `get_images()[0][0]` instead of the
  `insert_image` return value.
- "In current PyMuPDF, `overlay=False` is a no-op/back-compat shim … treat
  `overlay` as unreliable". False: `overlay=False` puts the image in the
  underlay, beneath existing content.

**fonts (0.86, low-n)** — concrete wrong/missing claims:
- Span flag bits wrong in two answers: "bit 1 = **bold**, bit 2 = *italic*,
  bit 4 = monospace" and "`2` = bold … `16` = monospaced". Verified values:
  1 superscript, 2 italic, 4 serif, 8 monospaced, 16 bold. It also calls flags
  "the authoritative" weight source, so italic text would be matched as bold.
- Subset font: explains the missing glyphs and embeds a full TTF, but never
  checks glyph coverage (`Font(fontfile=…).has_glyph(ord(c))`); verifies only
  by extraction.

Other factual errors outside the derived tags (worth the live probe):
`doc.replace_image` / `doc.replace_xref` (the method is `page.replace_image`);
`clean=True` said to remove JavaScript.

Contamination check: answers are long, heavily hedged and structured unlike
the reference, contradict themselves across questions (flag bits,
`insert_textbox` return sign) and miss points the reference states plainly.
Consistent with a genuine closed-book run.

## Live behavioural probe — with the 0.9-threshold skill (2026-09-28)

The model is driven with `ocode run -yolo` in a fresh git repo, one task per run, and the output is checked by `docs/okf/pdf/probe/check.py`. "not run" means that model had no skill for that phase, or its provider was out of credit. The 0.9 runs used a binary built before two late skill edits (pdf `new_page(pno + 1)`, mimo pptx iteration 2).

| task | with 0.9 skill |
|---|---|
| delete-row | PASS (1031s) |
| edit-cell | PASS (451s) |
| add-row | PASS (915s) |
| add-column | PASS (358s) |
| insert-table | PASS (216s) |
| edit-cell-subset | PASS (565s) |
| replace-image | PASS (424s) |
| insert-image | PASS (161s) |
