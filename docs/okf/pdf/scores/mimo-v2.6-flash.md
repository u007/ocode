---
model_id: mimo-v2.6-flash
model_version: "2.6"
evaluated_via: opencode-go
evaluated_on: 2026-09-28
stack: pdf
stack_corpus_rev: 1
threshold: 0.9
---

# Scorecard: mimo-v2.6-flash on pdf

> Valid ONLY for `mimo-v2.6-flash` @ `2.6`. A version bump invalidates
> this scorecard, so re-benchmark.

Answers: `pdf/answers/mimo-v2.6-flash.md`, produced closed-book (audited: zero
tool calls). Graded against `pdf/questions.yaml` at corpus_rev 1.

**Grading rule for invented APIs (applied consistently).** A point is docked
only when the invented constant or method is the mechanism that the point
grades. Otherwise the invention is noted but the point stands. PyMuPDF facts
were checked on 1.27.1 in this grading session:

- The signature is `Page.apply_redactions(images=2, graphics=1, text=0)`.
  - `PDF_REDACT_IMAGE_NONE=0`, `REMOVE=1`, `PIXELS=2`.
  - `PDF_REDACT_LINE_ART_NONE=0`, `REMOVE_IF_COVERED=1`, `REMOVE_IF_TOUCHED=2`.
- `PDF_REDACT_LINE_ART_REMOVE` and `PDF_REDACT_KEEP_LINES` do not exist.
- `Page.insert_table` and `Page.swap_content_of_page` do not exist.
- After `set_rotation(90)`, `search_for` and `get_text("words")` return the
  same bbox as before, so coordinates stay in the unrotated frame. Only
  `page.rect` changes.

## Per-question results

| id | tags | weight | full | awarded | normalized | notes |
|----|------|-------:|-----:|--------:|-----------:|-------|
| pdf-model-01 | pdf-model | 3 | 2 | 2 | 1.00 | |
| pdf-model-02 | pdf-model, verify-safety | 3 | 2 | 2 | 1.00 | |
| pdf-model-03 | pdf-model | 2 | 2 | 2 | 1.00 | Overstates things: says a length change "desynchronises the stream" through /Length, when a parser rewrites Length. The core points are present. |
| pdf-model-04 | pdf-model | 2 | 2 | 1 | 0.50 | Wrong rotation behaviour: "On a page with /Rotate, PyMuPDF exposes coordinates in the *rotated/visible* frame". The extract and insert APIs stay in the unrotated frame (verified), and the answer never mentions rotation_matrix or derotation_matrix. Its CropBox reasoning is fine. |
| pdf-locate-01 | table-locate | 3 | 2 | 2 | 1.00 | |
| pdf-locate-02 | table-locate | 2 | 2 | 2 | 1.00 | |
| pdf-locate-03 | table-locate, cell-edit | 2 | 2 | 2 | 1.00 | Omits `flags` here. Claims "PyMuPDF only embeds Base-14 fonts by default", but base-14 fonts are never embedded. |
| pdf-locate-04 | table-locate, table-relayout | 2 | 2 | 2 | 1.00 | |
| pdf-rowdel-01 | row-delete | 3 | 3 | 3 | 1.00 | Kept under the rule, because any real apply_redactions call removes the row. Invented `graphics=PDF_REDACT_LINE_ART_REMOVE` and "add_redact_annot + a 'move' alternative". **This tag's status hinges on this point:** docking it drops row-delete to 0.65. |
| pdf-rowdel-02 | row-delete, cell-edit | 3 | 2 | 1 | 0.50 | Wrong defaults: "images=PDF_REDACT_IMAGE_REMOVE (0), graphics=PDF_REDACT_LINE_ART_REMOVE (0)". The real defaults are PIXELS (2) and REMOVE_IF_COVERED (1). Says line art that merely *overlaps* is removed, which is the TOUCHED behaviour. The keep option `graphics=PDF_REDACT_KEEP_LINES` is invented. It knows about the side effect, so point 1 stands. Point 2 is lost. |
| pdf-rowdel-03 | row-delete | 2 | 2 | 1 | 0.50 | Gets show_pdf_page with a clip, shifted up. It never says to redact the *original* lower region on the target page (or to keep a pristine source copy), so the moved block is duplicated. It only mentions redacting the deleted row. Option (b) is invented (xref_get_key used "to import XObjects"). |
| pdf-rowdel-04 | row-delete, verify-safety | 2 | 2 | 2 | 1.00 | |
| pdf-cell-01 | cell-edit | 3 | 3 | 2 | 0.67 | Missed preserving the graphics. It applies with the defaults "(text remove, images remove, line art remove)", or with the invented `PDF_REDACT_KEEP_LINES`, instead of graphics=0 and images=0. It also defaults to a white fill=(1,1,1) on shaded cells. Right-align, baseline and style matching are correct. |
| pdf-cell-02 | cell-edit | 2 | 2 | 2 | 1.00 | Minor: says a rect that clips a glyph "can leave that character's portion behind". In fact any overlap removes the whole character. |
| pdf-cell-03 | cell-edit, table-relayout | 2 | 2 | 2 | 1.00 | |
| pdf-cell-04 | cell-edit | 2 | 2 | 2 | 1.00 | |
| pdf-relayout-01 | table-relayout | 3 | 3 | 3 | 1.00 | |
| pdf-relayout-02 | table-relayout | 3 | 3 | 3 | 1.00 | |
| pdf-relayout-03 | table-relayout, pdf-model | 2 | 2 | 1 | 0.50 | The fidelity risks are thorough. It never recommends the better paths: regenerate from the original source, or else make a surgical in-place edit. |
| pdf-relayout-04 | table-relayout, table-insert | 2 | 2 | 2 | 1.00 | |
| pdf-tblins-01 | table-insert | 3 | 2 | 2 | 1.00 | Kept under the rule. Invented `page.insert_table(rect, data=...)`, hedged with "if available", and a real manual draw path is also given. |
| pdf-tblins-02 | table-insert | 2 | 2 | 1 | 0.50 | Style sampling is fine. No paint order (fills first, then text and rules). It says only "overlay drawing (paint order on top)". |
| pdf-tblins-03 | table-insert, table-relayout | 2 | 2 | 1 | 0.50 | Its move-down mechanism is invented: `page.swap_content_of_page(...)`. It also assumes it can "split page contents at the insertion point", as if stream order were y order. It never uses redact + show_pdf_page restamp. The alternatives point (source, new page, free space, never overlap) is earned. |
| pdf-imgrep-01 | image-replace | 3 | 3 | 2 | 0.67 | Gets xref, replace_image and the shared-xref catch. No aspect-ratio or stretch warning for a PNG with a different aspect. Also offers "incremental=True on the original". |
| pdf-imgrep-02 | image-replace, verify-safety | 2 | 2 | 2 | 1.00 | |
| pdf-imgrep-03 | image-replace | 2 | 2 | 2 | 1.00 | Invented `swap_content_of_page` again. Not load-bearing for the point. |
| pdf-imgins-01 | image-insert | 3 | 2 | 2 | 1.00 | Anchors to page.rect minus a fixed margin, but then runs a real collision check against text, drawings and images. |
| pdf-imgins-02 | image-insert | 2 | 2 | 1 | 0.50 | Gets the xref reuse. Missed save with garbage≥3 (dedup) as the other remedy. |
| pdf-imgins-03 | image-insert, pdf-model | 2 | 2 | 2 | 1.00 | |
| pdf-fonts-01 | fonts | 3 | 2 | 1 | 0.50 | Wrong fix as the first recommendation: `doc.extract_font(original_font_name)`, then insert_font "with the full bytes ... PyMuPDF will subset it on save to include ALL characters you used". The extracted buffer IS the subset, so the new glyphs are still missing. The original-TTF route and has_glyph appear only as a secondary option. |
| pdf-fonts-02 | fonts | 2 | 2 | 2 | 1.00 | |
| pdf-fonts-03 | fonts, cell-edit | 2 | 2 | 2 | 1.00 | Minor: "`bbox[3]` ≈ baseline". bbox[3] sits below the baseline by the descender, so span origin is the correct source. Also repeats the subset re-embed belief. |
| pdf-verify-01 | verify-safety | 3 | 3 | 2 | 0.67 | Calls the render "the most reliable" check. It never says a render can't tell a white-box overlay from a real edit. The negative-search check is present. |
| pdf-verify-02 | verify-safety | 3 | 2 | 2 | 1.00 | |
| pdf-verify-03 | verify-safety | 2 | 2 | 2 | 1.00 | |
| pdf-verify-04 | verify-safety, pdf-model | 2 | 2 | 2 | 1.00 | |

`normalized = min(awarded, full) / full`

## Per-tag subscores

| tag | subscore | n | trust | action |
|-----|---------:|--:|-------|--------|
| table-locate | 1.00 | 4 | ok | omit (strong) |
| verify-safety | 0.94 | 7 | ok | omit (strong) |
| pdf-model | 0.88 | 7 | ok | **derive** |
| table-relayout | 0.88 | 7 | ok | **derive** |
| image-replace | 0.86 | 3 | low-n | **derive** |
| image-insert | 0.86 | 3 | low-n | **derive** |
| cell-edit | 0.84 | 7 | ok | **derive** |
| fonts | 0.79 | 3 | low-n | **derive** |
| table-insert | 0.78 | 4 | ok | **derive** |
| row-delete | 0.75 | 4 | ok | **derive** |

`subscore = Σ(normalized×weight) / Σ(weight)` over that tag's questions.

## Stack score

```
stack_score = Σ(normalized×weight) / Σ(weight) = 74.0 / 86 = 86.0%
```

## Live behavioural probe — editing a real PDF (2026-09-28)

Model driven with `ocode run -yolo` in a fresh git repo containing an invoice PDF: a ruled, shaded table, a
logo, and a page 2 that must stay unchanged. One task per run. The output is checked on the **text layer** by
`docs/okf/pdf/probe/check.py`, which rejects white-box overlays, so passing requires real removal, not just a
correct-looking render. Renders were also inspected by eye. The harness is in `docs/okf/pdf/probe/`.

| task | baseline |
|---|---|
| delete-row | PASS (393s) |
| edit-cell | PASS (644s) |
| add-row | PASS (865s) |
| add-column | PASS (593s) |
| insert-table | PASS (1081s) |
| edit-cell-subset | PASS (872s) |
| replace-image | PASS (455s) |
| insert-image | PASS (293s) |

One baseline insert-image run hit an HTTP/2 PROTOCOL_ERROR and was rerun. Loaded the bundled `pdf` skill in 7 of 8 runs. Baseline runs used a pre-fix binary, so no Kaizen digest was injected. The with-skill runs used a
binary built after the `ocode run` bundled-skills fix; digest injection was verified by having the model quote
the digest from its system prompt.

## Derivation targets

Tags below threshold (`< 0.9`): **pdf-model (0.88), table-relayout (0.88),
image-replace (0.86, low-n), image-insert (0.86, low-n), cell-edit (0.84),
fonts (0.79, low-n), table-insert (0.78), row-delete (0.75)** → feed into
`derived/pdf.mimo-v2.6-flash.SKILL.md`.

Root cause shared by row-delete and cell-edit: the model does not know the real
`apply_redactions` defaults and invents constants for them, so it cannot say how
to keep a cell's rules and shading.

### row-delete (0.75)
- Wrong defaults (rowdel-02): "images=PDF_REDACT_IMAGE_REMOVE (0),
  graphics=PDF_REDACT_LINE_ART_REMOVE (0)". The real defaults are
  images=2 (PIXELS), graphics=1 (REMOVE_IF_COVERED) and text=0. It also says
  line art that merely *overlaps* is removed, which is the TOUCHED (2)
  behaviour; the default removes only shapes lying entirely inside the rect.
- Invented constants (rowdel-01, rowdel-02): `PDF_REDACT_LINE_ART_REMOVE` and
  `PDF_REDACT_KEEP_LINES`. The real values to keep graphics are
  `graphics=PDF_REDACT_LINE_ART_NONE` (0) and `images=PDF_REDACT_IMAGE_NONE` (0).
- Invented "add_redact_annot + a 'move' alternative" (rowdel-01).
- In the show_pdf_page region move (rowdel-03), it never redacts the original
  region on the target page and never keeps a pristine source copy, so the
  moved content is duplicated. It only redacts the deleted row.

### cell-edit (0.84)
- For an in-cell replace (cell-01) it applies with the defaults "(text remove,
  images remove, line art remove)", or with the invented KEEP_LINES, and passes
  a white `fill=(1,1,1)`. That wipes the rules and shading, or paints white
  over a shaded cell, instead of passing graphics=0 and images=0 with no fill.
- Wrong apply_redactions defaults (rowdel-02, shared with row-delete).
- Believes "PyMuPDF only embeds Base-14 fonts by default" (locate-03); base-14
  fonts are never embedded.
- Minor (cell-02): says a rect that clips part of a glyph "can leave that
  character's portion behind". Any overlap removes the whole character.
- Minor (fonts-03): "`bbox[3]` ≈ baseline". bbox[3] sits below the baseline
  by the descender; use span `origin`.

### table-insert (0.78)
- Invented `page.insert_table(...)` (tblins-01).
- Invented `page.swap_content_of_page(...)`, together with "split page
  contents at the insertion point" as the way to push content down
  (tblins-03). The correct route is to redact the region, then restamp it
  lower with `show_pdf_page(clip=...)` from an untouched copy.
- No paint-order rule (tblins-02): fills first, then text and rules. It only
  says "overlay drawing (paint order on top)".

### fonts (0.79, low-n)
- Believes re-embedding the `doc.extract_font()` buffer of an `AAAAAA+`
  subset yields a full font ("PyMuPDF will subset it on save to include ALL
  characters", fonts-01, repeated in fonts-03). The extracted buffer is the
  subset: you need the original full TTF/OTF, or a base-14 or Noto
  substitute, with a has_glyph check on that full font.
- Treats "`bbox[3]` ≈ baseline" (fonts-03). Use span `origin`.

### pdf-model (0.88)
- "On a page with /Rotate, PyMuPDF exposes coordinates in the
  *rotated/visible* frame" (model-04). get_text, search_for and insert all use
  the unrotated frame; only `page.rect` is rotated. Convert with
  `page.rotation_matrix` / `page.derotation_matrix`, which it never mentions.
- Shares relayout-03's miss (below).

### table-relayout (0.88)
- PDF→DOCX→PDF round-trip (relayout-03): lists the fidelity risks but never
  recommends the better paths: regenerate from the original source if one
  exists, otherwise do a surgical in-place edit.
- Shares tblins-03's invented `swap_content_of_page` / content-stream split
  for moving content down.

### image-replace (0.86, low-n)
- No aspect-ratio or stretch warning (imgrep-01): `replace_image` keeps the
  old placement, so a PNG with a different aspect is stretched into the old
  box.
- Invented `swap_content_of_page` again (imgrep-03, not load-bearing).

### image-insert (0.86, low-n)
- Missed save-time deduplication as the other remedy for a file already
  bloated by repeated copies of one image (imgins-02).

## Live behavioural probe — with the 0.9-threshold skill (2026-09-28)

The model is driven with `ocode run -yolo` in a fresh git repo, one task per run, and the output is checked by `docs/okf/pdf/probe/check.py`. "not run" means that model had no skill for that phase, or its provider was out of credit. The 0.9 runs used a binary built before two late skill edits (pdf `new_page(pno + 1)`, mimo pptx iteration 2).

| task | with 0.9 skill |
|---|---|
| delete-row | PASS (838s) |
| edit-cell | PASS (837s) |
| add-row | PASS (1151s) |
| add-column | PASS (422s) |
| insert-table | PASS (864s) |
| edit-cell-subset | PASS (1426s) |
| replace-image | PASS (894s) |
| insert-image | PASS (246s) |
