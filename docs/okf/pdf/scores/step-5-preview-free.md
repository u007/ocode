---
model_id: step-5-preview-free
model_version: "5-preview"
evaluated_via: opencode-go
evaluated_on: 2026-10-12
stack: pdf
stack_corpus_rev: 1
threshold: 0.9
---

<!-- Filename: model_id with "/" flattened to "__" so it is one valid path
     segment. `step-5-preview-free` has no slash, so the filename is unchanged. -->

# Scorecard — step-5-preview-free on pdf

> Valid ONLY for `step-5-preview-free` @ `5-preview`. A version bump invalidates
> this scorecard — re-benchmark.

Answers: `pdf/answers/step-5-preview-free.md` (closed-book, see Provenance).
Graded against `pdf/questions.yaml` @ corpus_rev 1. PyMuPDF facts in the key
were verified on 1.27.1; answers stating a wrong PyMuPDF behaviour do not earn
the affected point. Where the answer states a concept correctly but also makes a
materially wrong claim about the same construct, the point is withheld and the
note says "(judgment)".

## Provenance

Answerer: `ocode run -m opencode-go/step-5-preview-free -effort med` in an
empty scratch directory with only the `_prompts/pdf.md` sheet attached. The
session sqlite shows exactly 1 user message, 1 assistant message and 0 tool
calls, so no repo file, answer key or web page was read. Two earlier attempts
failed at the provider (a 429, then a 600 s stream deadline) with no output;
only the successful run is graded. The model appended a short self-summary
(ASKED/WORKED/FOUND/DECIDED/NEXT) after the YAML list; it is ignored for
grading.

## Per-question results

| id | tags | weight | full | awarded | normalized | notes |
|----|------|-------:|-----:|--------:|-----------:|-------|
| pdf-model-01 | pdf-model | 3 | 2 | 2 | 1.00 | |
| pdf-model-02 | pdf-model, verify-safety | 3 | 2 | 2 | 1.00 | |
| pdf-model-03 | pdf-model | 2 | 2 | 2 | 1.00 | Flate, subset-font byte codes, split Tj/TJ, width drift and /Length corruption all covered |
| pdf-model-04 | pdf-model | 2 | 2 | 1 | 0.50 | y-flip point ok; rotation point withheld (judgment): says "PyMuPDF coordinates are always in the visual (rotated) frame" and that `page.rect` "already accounts for /Rotate". Verified on 1.27.1: `get_text("words")` bboxes stay in UNROTATED page coordinates while only `page.rect` swaps; never names `rotation_matrix`/`derotation_matrix`. CropBox offset for raw-stream edits is correct |
| pdf-locate-01 | table-locate | 3 | 2 | 2 | 1.00 | |
| pdf-locate-02 | table-locate | 2 | 2 | 2 | 1.00 | rule-bounded row region from `get_drawings` ok; never says to check the hit is unique |
| pdf-locate-03 | table-locate, cell-edit | 2 | 2 | 2 | 1.00 | flag bits correct (italic 2, serif 4, mono 8, bold 16); names the sRGB int but no conversion helper |
| pdf-locate-04 | table-locate, table-relayout | 2 | 2 | 2 | 1.00 | |
| pdf-rowdel-01 | row-delete | 3 | 3 | 3 | 1.00 | region move goes through a temp page (capture → redact → restamp): roundabout but valid, text stays vector; Total "updated" rather than explicitly recomputed from data |
| pdf-rowdel-02 | row-delete, cell-edit | 3 | 2 | 1 | 0.50 | graphics default (`REMOVE_IF_COVERED`) correct, but point 1 withheld (judgment): **wrong image default** — "images default to removal — any image whose placement overlaps the rect is deleted entirely, even if only a corner overlaps". Default is `PDF_REDACT_IMAGE_PIXELS` (2), which blanks only the overlapping pixels. Point 2 earned via "cover the band if you redraw the grid / stay inside the cell to keep borders" and raising to `REMOVE_IF_TOUCHED`; never names `LINE_ART_NONE`/`IMAGE_NONE` here; hedges "constant names and defaults changed across releases" |
| pdf-rowdel-03 | row-delete | 2 | 2 | 2 | 1.00 | same temp-page detour as rowdel-01 instead of stamping straight from an untouched copy of the original; caveats (annotations/links don't move) are right |
| pdf-rowdel-04 | row-delete, verify-safety | 2 | 2 | 2 | 1.00 | |
| pdf-cell-01 | cell-edit | 3 | 3 | 3 | 1.00 | tight glyph rect inside the cell; never passes `graphics=0, images=0` explicitly, relies on the rect not fully covering shading/rules (works under the default `REMOVE_IF_COVERED`) |
| pdf-cell-02 | cell-edit | 2 | 2 | 2 | 1.00 | no explicit "verify neighbours afterwards" |
| pdf-cell-03 | cell-edit, table-relayout | 2 | 2 | 2 | 1.00 | negative return + nothing written stated correctly; `insert_htmlbox` scaling not offered |
| pdf-cell-04 | cell-edit | 2 | 2 | 2 | 1.00 | |
| pdf-relayout-01 | table-relayout | 3 | 3 | 3 | 1.00 | striping colour of the new row not mentioned |
| pdf-relayout-02 | table-relayout | 3 | 3 | 2 | 0.67 | point 2 lost: never measures the shrunk column's content against its new width, no font-shrink/wrap fallback. Point 3 earned via "shift the columns to its right — vertical rules and text — redraw the affected rules", but never says to redact the old table and does not name header fill/shading |
| pdf-relayout-03 | table-relayout, pdf-model | 2 | 2 | 1 | 0.50 | fidelity risks thorough, surgical edit recommended; never says prefer regenerating from the original source / ask whether one exists. Dubious aside: a round trip is "roughly lossless" when the PDF came from Word/HTML |
| pdf-relayout-04 | table-relayout, table-insert | 2 | 2 | 2 | 1.00 | adds a pixmap ink scan of the target region — good |
| pdf-tblins-01 | table-insert | 3 | 2 | 2 | 1.00 | |
| pdf-tblins-02 | table-insert | 2 | 2 | 2 | 1.00 | paint order (fills before text) not stated; wrong aside: `draw_rect` "defaults to filling AND stroking black" (default `fill=None`, stroke only) |
| pdf-tblins-03 | table-insert, table-relayout | 2 | 2 | 2 | 1.00 | "tell the user" / regenerate-from-source not offered; new page + never overlap present |
| pdf-imgrep-01 | image-replace | 3 | 3 | 2 | 0.67 | never names `page.replace_image`; point 1 earned via xref+rect lookup and `update_stream` with correct /Width /Height /SMask caveats. Point 2 withheld (judgment): recommends `page.delete_image(xref)` + `insert_image` on page 1 while stating that `delete_image` "changes the logo everywhere it appears", so a shared logo would vanish from every other page; says the xref-level methods "cannot give page 1 a different logo" and never offers redact-that-placement (`images=PDF_REDACT_IMAGE_REMOVE`) + `insert_image` for a single occurrence |
| pdf-imgrep-02 | image-replace, verify-safety | 2 | 2 | 2 | 1.00 | no "verify exactly one image at that rect" |
| pdf-imgrep-03 | image-replace | 2 | 2 | 2 | 1.00 | annotation appearance stream not listed; `get_image_info(xrefs=True)` with inline images as xref 0 is right |
| pdf-imgins-01 | image-insert | 3 | 2 | 2 | 1.00 | anchors to `page.rect` − margin rather than real content edges, then collision-checks words/drawings/images and nudges; claims `page.rect` "already accounts for rotation" (see pdf-model-04) |
| pdf-imgins-02 | image-insert | 2 | 2 | 1 | 0.50 | point 1 earned via pikepdf object sharing and a hand-rolled PyMuPDF `xref_set_key("Resources/XObject/Im")` + appended `Do`; does not know `insert_image` returns the xref and takes `xref=`. Point 2 withheld: **false** — "garbage collection alone will NOT merge duplicates — it only removes unreferenced objects" (garbage=4 merges identical objects; levels 0–3 keep copies) |
| pdf-imgins-03 | image-insert, pdf-model | 2 | 2 | 2 | 1.00 | `overlay=False` explained correctly; "always compute against `page.rect`" repeats the rotation confusion |
| pdf-fonts-01 | fonts | 3 | 2 | 2 | 1.00 | checks the SUBSET's coverage (`extract_font` + cmap), not the replacement font's (`Font.has_glyph`); no render verification; fontTools subset-extension offered as a rare option |
| pdf-fonts-02 | fonts | 2 | 2 | 2 | 1.00 | full reserved-name list correct |
| pdf-fonts-03 | fonts, cell-edit | 2 | 2 | 2 | 1.00 | |
| pdf-verify-01 | verify-safety | 3 | 3 | 3 | 1.00 | |
| pdf-verify-02 | verify-safety | 3 | 2 | 2 | 1.00 | "linearize afterwards so no stale revision survives" is unnecessary: a full save already drops old revisions |
| pdf-verify-03 | verify-safety | 2 | 2 | 1 | 0.50 | keep original / new output / verify / record changes ok; point 2 lost: nothing about leaving other pages untouched (page count, other pages' text), nor signatures, encryption or metadata preservation |
| pdf-verify-04 | verify-safety, pdf-model | 2 | 2 | 2 | 1.00 | |

`normalized = min(awarded, full) / full`

## Per-tag subscores

| tag | subscore | n | trust | action |
|-----|---------:|--:|-------|--------|
| pdf-model | 0.88 | 7 | ok | **derive** |
| table-locate | 1.00 | 4 | ok | omit (strong) |
| row-delete | 0.85 | 4 | ok | **derive** |
| cell-edit | 0.91 | 7 | ok | omit (borderline: 14.5/16; only loss is the rowdel-02 image default) |
| table-relayout | 0.88 | 7 | ok | **derive** |
| table-insert | 1.00 | 4 | ok | omit (strong) |
| image-replace | 0.86 | 3 | low-n | **derive** (mark low-n) |
| image-insert | 0.86 | 3 | low-n | **derive** (mark low-n) |
| fonts | 1.00 | 3 | low-n | omit (strong) |
| verify-safety | 0.94 | 7 | ok | omit |

`subscore = Σ(normalized×weight) / Σ(weight)` over that tag's questions.
Exact fractions: pdf-model 14/16, row-delete 8.5/10, cell-edit 14.5/16,
table-relayout 14/16, image-replace 6/7, image-insert 6/7, verify-safety 16/17.

## Stack score

```
stack_score = Σ(normalized×weight) / Σ(weight) = 78.5 / 86 = 91.3%
```

## Derivation targets

Tags below threshold (`< 0.9`): **pdf-model, row-delete, table-relayout,
image-replace (low-n), image-insert (low-n)** → feed into
`derived/pdf.step-5-preview-free.SKILL.md`.

**pdf-model (0.88)** — concrete wrong/missing claims:
- "PyMuPDF coordinates are always in the visual (rotated) frame" and
  `page.rect` "already accounts for /Rotate". Verified on 1.27.1:
  `get_text("words")` bboxes stay in the unrotated page's coordinates while
  `page.rect` swaps width and height; the displayed position is
  `rect * page.rotation_matrix`. The same belief recurs in pdf-imgins-01 and
  pdf-imgins-03 ("always compute against `page.rect`"), so a bottom-right
  anchor computed from `page.rect` on a rotated page lands off the visible area.
- PDF → DOCX → PDF round-trip (shared with table-relayout): never says to
  prefer regenerating from the original source or to ask whether one exists.

**row-delete (0.85)** — concrete wrong/missing claims:
- `apply_redactions()` image default: "any image whose placement overlaps the
  rect is deleted entirely, even if only a corner overlaps". The default is
  `PDF_REDACT_IMAGE_PIXELS` (2), which blanks the overlapping pixels only;
  `PDF_REDACT_IMAGE_REMOVE` (1) is opt-in.
- Keeps borders/shading only by rect geometry ("stay inside cell bounds");
  never names `graphics=PDF_REDACT_LINE_ART_NONE` / `images=PDF_REDACT_IMAGE_NONE`
  as the explicit way to preserve them while removing text.
- Region move always detours through a temp document (capture with
  `show_pdf_page(clip=…)` onto a temp page, redact, restamp from the temp doc)
  and says "PyMuPDF cannot move content directly". Valid, but the direct form
  is `page.show_pdf_page(new_rect, untouched_copy_of_original, pno, clip=old_rect)`
  after redacting.

**table-relayout (0.88)** — concrete wrong/missing claims:
- Adding a column: takes the width out of the widest column and shifts the
  others, but never measures each cell's text at its new width
  (`get_text_length`) nor plans a font-shrink/wrap fallback; never says to
  redact the old table; header fill and shading are not redrawn.
- PDF → DOCX round-trip: no "regenerate from the original source if it
  exists / ask for it"; calls the round trip "roughly lossless" when the PDF
  was produced from Word/HTML.

**image-replace (0.86, low-n)** — concrete wrong/missing claims:
- Does not know `page.replace_image(xref, filename=…)`. Its "true replacement"
  is `page.delete_image(xref)` then `insert_image` at the old rect on page 1,
  while correctly noting that `delete_image` changes the logo everywhere: on a
  shared header logo this blanks every other page.
- Says the xref-level methods "cannot give page 1 a different logo" and never
  offers the single-occurrence technique: redact that placement with
  `images=PDF_REDACT_IMAGE_REMOVE`, then `insert_image` the new file there.

**image-insert (0.86, low-n)** — concrete wrong/missing claims:
- "garbage collection alone will NOT merge duplicates — it only removes
  unreferenced objects". False: `doc.save(..., garbage=4)` merges identical
  objects (levels 0–3 keep the copies).
- Does not know `insert_image` returns the image xref and accepts `xref=` on
  later calls; instead hand-edits `Resources/XObject` with `xref_set_key` and
  appends a `Do` operator to each page's content stream.

Other factual errors outside the derived tags: `draw_rect` "defaults to
filling AND stroking black" (default `fill=None`); file-safety answer omits
other pages / signatures / encryption / metadata (verify-safety stays above
threshold at 0.94).

Contamination check: all 36 ids present, none truncated. Answers are long,
numbered-step prose in the model's own structure, name APIs the key never
mentions (`delete_image`, `update_stream`, `xref_set_key`, camelot, fontTools)
and contradict the key on facts it states plainly (image redaction default,
garbage=4 dedup, rotated-page coordinates, `replace_image`). No verbatim or
near-verbatim overlap with the reference answers. Consistent with a genuine
closed-book run. Formatting anomaly: the file closes with an unmatched
` ``` ` fence after pdf-verify-04 and a self-summary block; neither affects
the 36 graded entries.
