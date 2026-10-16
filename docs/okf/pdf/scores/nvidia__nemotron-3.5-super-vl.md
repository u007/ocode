---
model_id: nvidia/nemotron-3.5-super-vl
model_version: "3.5"
evaluated_via: openrouter
evaluated_on: 2026-10-16
stack: pdf
stack_corpus_rev: 1
threshold: 0.9
---

<!-- Filename: model_id with "/" flattened to "__" so it is one valid path
     segment. `nvidia/nemotron-3.5-super-vl` → nvidia__nemotron-3.5-super-vl.md -->

# Scorecard — nvidia/nemotron-3.5-super-vl on pdf

> Valid ONLY for `nvidia/nemotron-3.5-super-vl` @ `3.5`. A version bump invalidates
> this scorecard — re-benchmark.

Answers: `pdf/answers/nvidia__nemotron-3.5-super-vl.md` (closed-book, see
Provenance). Graded against `pdf/questions.yaml` @ corpus_rev 1. PyMuPDF facts
in the key were verified on 1.27.1; answers stating a wrong PyMuPDF behaviour do
not earn the affected point. Where the answer states a concept correctly but also
makes a materially wrong claim about the same construct, the point is withheld
and the note says "(judgment)".

## Provenance

Closed-book (Rule 0). The answerer was
`ocode run -m openrouter/nvidia/nemotron-3.5-super-vl:free -effort med` under an
isolated HOME whose config denies every tool (no tools were advertised), in a
scratch cwd containing only the `_prompts/pdf.md` sheet, one fresh session; the
session sqlite shows exactly 1 user + 1 assistant message and 0 tool calls. All
36 ids came back once, none empty. Headless runs inject the bundled
skill-catalogue digest into the system prompt; no answer cites a tool or skill
the question never named (the only library names are PyMuPDF via the legacy
`fitz` alias, pdfplumber, and the APIs the questions themselves name). The
OpenRouter `:free` route is a serving detail, not part of the model id.

Observations: all 36 answers were produced in one pass over the sheet, and two
answers cross-reference another question by id ("relayout-04 method" in
pdf-tblins-01 and pdf-imgins-01), so the questions were not answered in fully
independent contexts. No duplicated answers, no thinking-aloud, no fabricated
tool output. Answers are compact single-paragraph prose in the model's own
structure and contradict the key on facts it states plainly (redaction defaults,
`show_pdf_page` region moves, `replace_image`, `insert_textbox` behaviour),
consistent with a genuine closed-book run.

## Per-question results

| id | tags | weight | full | awarded | normalized | notes |
|----|------|-------:|-----:|--------:|-----------:|-------|
| pdf-model-01 | pdf-model | 3 | 2 | 2 | 1.00 | "removing or covering them" offers a cover-up as an equal option; both points otherwise present |
| pdf-model-02 | pdf-model, verify-safety | 3 | 2 | 2 | 1.00 | |
| pdf-model-03 | pdf-model | 2 | 2 | 2 | 1.00 | Flate, split TJ, glyph-id/CMap codes, /Length recompute all covered; alignment shift not named |
| pdf-model-04 | pdf-model | 2 | 2 | 1 | 0.50 | y-flip and CropBox offset correct; rotation point withheld (judgment): "text bboxes are in rotated space" and "PyMuPDF already presents the rotated view". On 1.27.1 `get_text("words")` bboxes stay in UNROTATED page coordinates; only `page.rect` swaps. Names `derotation_matrix` but with the frames inverted |
| pdf-locate-01 | table-locate | 3 | 2 | 2 | 1.00 | point 1 earned via pdfplumber `find_tables()` → bbox/rows/cells, but **false claim**: "PyMuPDF has no built-in 'give me the table' API" (`page.find_tables()` exists since 1.23). Words + `get_drawings` reconstruction correct |
| pdf-locate-02 | table-locate | 2 | 2 | 2 | 1.00 | rule-bounded y-band across the table width; never says to check the hit is unique |
| pdf-locate-03 | table-locate, cell-edit | 2 | 2 | 2 | 1.00 | flags not mentioned; colour int conversion and baseline origin correct |
| pdf-locate-04 | table-locate, table-relayout | 2 | 2 | 2 | 1.00 | |
| pdf-rowdel-01 | row-delete | 3 | 3 | 3 | 1.00 | shift done by "translating" content-stream operators or re-drawing at y − row_h (the re-draw branch never says to redact the moved rows first); Total "updated", not explicitly recomputed from data; content under the table never mentioned |
| pdf-rowdel-02 | row-delete, cell-edit | 3 | 2 | 0.5 | 0.25 | **defaults inverted**: "Default apply_redactions() treats images and vector graphics as NONE ... rules/fills are left in place unless you pass graphics=...REMOVE". Real defaults: `images=PDF_REDACT_IMAGE_PIXELS` (2), `graphics=PDF_REDACT_LINE_ART_REMOVE_IF_COVERED` (1). Point 2 not reached (never says pass 0 to KEEP graphics). Partial 0.5: knows the flags exist and that rules/shading matter |
| pdf-rowdel-03 | row-delete | 2 | 2 | 0.5 | 0.25 | answers "No. PyMuPDF cannot move an existing page region" and suggests re-emitting operators or "accept the blank gap"; never offers `show_pdf_page(..., clip=)` from a copy of the original. Partial only |
| pdf-rowdel-04 | row-delete, verify-safety | 2 | 2 | 2 | 1.00 | totals/tax/counts, content below, signature, bookmarks; striping not mentioned |
| pdf-cell-01 | cell-edit | 3 | 3 | 2 | 0.67 | point 1 withheld (judgment): offers "or cover the glyph area with a fill rect in the cell background" as an equal alternative to redaction — the overlay the corpus forbids; `graphics=0, images=0` never passed. Measured right-alignment at the old baseline and style match correct |
| pdf-cell-02 | cell-edit | 2 | 2 | 2 | 1.00 | no explicit "verify neighbours afterwards" |
| pdf-cell-03 | cell-edit, table-relayout | 2 | 2 | 1 | 0.50 | point 1 withheld: says `insert_textbox` "renders as much as fits ... text that still does not fit is silently not drawn" — it is all-or-nothing: nothing is written and a negative value is returned; the return check is never stated. Options (shrink, wrap/taller, widen column) earn point 2 |
| pdf-cell-04 | cell-edit | 2 | 2 | 2 | 1.00 | |
| pdf-relayout-01 | table-relayout | 3 | 3 | 3 | 1.00 | "translate the Total row down" without naming redact-then-redraw; page overflow handled |
| pdf-relayout-02 | table-relayout | 3 | 3 | 2 | 0.67 | point 2 lost: never measures each column's content at its new width, no font-shrink fallback ("re-wrap headers if needed" only). Point 3 earned via "redraw the complete grid"; never says to redact the old table, header fill/shading not named |
| pdf-relayout-03 | table-relayout, pdf-model | 2 | 2 | 1 | 0.50 | fidelity risks thorough; point 2 lost: never says prefer regenerating from the original source / ask for it, nor names the surgical in-place edit as the alternative |
| pdf-relayout-04 | table-relayout, table-insert | 2 | 2 | 2 | 1.00 | includes annotations; good |
| pdf-tblins-01 | table-insert | 3 | 2 | 2 | 1.00 | stamp approach via a PyMuPDF-built page + `show_pdf_page` (not htmlbox/reportlab, still text-real) |
| pdf-tblins-02 | table-insert | 2 | 2 | 2 | 1.00 | fills before text, no double-drawn shared borders, pixmap compare |
| pdf-tblins-03 | table-insert, table-relayout | 2 | 2 | 1 | 0.50 | point 1 earned (move content below down, page break on overflow). Point 2 withheld (judgment): options 1–2 are "shrink the paragraphs (font size/line spacing)" and "make the table smaller" — the rubric's zero-credit anti-pattern; no "tell the user", no regenerate-from-source. Move-down is "rewrite/re-draw downstream content", not redact + restamp |
| pdf-imgrep-01 | image-replace | 3 | 3 | 2 | 0.67 | point 1 withheld: never names `page.replace_image`; says "You cannot simply swap one shared XObject in place" (that is exactly what `replace_image` does), and offers "cover with a background-colored fill rect" as an alternative. xref/rect lookup, shared-xref catch and aspect awareness earn points 2–3; claims `insert_image` "stretches to fill the rect" (default `keep_proportion=True`) |
| pdf-imgrep-02 | image-replace, verify-safety | 2 | 2 | 2 | 1.00 | no "verify exactly one image at that rect" |
| pdf-imgrep-03 | image-replace | 2 | 2 | 1 | 0.50 | causes (vector paths, outlined text, Form XObject, SMask) earn point 1. Point 2 withheld (judgment): primary fix is "cover the area with a background-colored filled rect and insert the new PNG"; delete-the-paths only as an afterthought; locates via manual /Resources traversal instead of `get_image_info` / `get_images(full=True)` |
| pdf-imgins-01 | image-insert | 3 | 2 | 2 | 1.00 | anchors to `page.rect` − margins rather than real content edges, then collision-checks words/drawings and nudges |
| pdf-imgins-02 | image-insert | 2 | 2 | 1 | 0.50 | xref reuse from the first `insert_image` earns point 1 (never names the `xref=` parameter). Point 2 lost: "Do not expect dedup to happen automatically"; garbage collection is mentioned only as a generic final step, never as the `garbage=4` merge of identical objects |
| pdf-imgins-03 | image-insert, pdf-model | 2 | 2 | 2 | 1.00 | paint order via content-stream order rather than the `overlay` flag; verifies by render rather than `get_image_info` bbox |
| pdf-fonts-01 | fonts | 3 | 2 | 2 | 1.00 | no glyph-coverage check (`Font.has_glyph`) and no render verification |
| pdf-fonts-02 | fonts | 2 | 2 | 2 | 1.00 | full reserved-name list correct; remedy (embed a TTF) not stated, only the limitation |
| pdf-fonts-03 | fonts, cell-edit | 2 | 2 | 2 | 1.00 | "exact same fontname" would be the subset name; corrected by the fontfile fallback it adds |
| pdf-verify-01 | verify-safety | 3 | 3 | 3 | 1.00 | other pages unchanged not stated |
| pdf-verify-02 | verify-safety | 3 | 2 | 2 | 1.00 | |
| pdf-verify-03 | verify-safety | 2 | 2 | 2 | 1.00 | signature named; other pages / encryption / metadata preservation not mentioned |
| pdf-verify-04 | verify-safety, pdf-model | 2 | 2 | 2 | 1.00 | empty `search_for` and cover-not-remove named; the negative `insert_textbox` return and "check return values" are not |

`normalized = min(awarded, full) / full`

## Per-tag subscores

| tag | subscore | n | trust | action |
|-----|---------:|--:|-------|--------|
| pdf-model | 0.88 | 7 | ok | **derive** |
| table-locate | 1.00 | 4 | ok | omit (strong; note the false "no find_tables in PyMuPDF" claim) |
| row-delete | 0.63 | 4 | ok | **derive** |
| cell-edit | 0.73 | 7 | ok | **derive** |
| table-relayout | 0.75 | 7 | ok | **derive** |
| table-insert | 0.89 | 4 | ok | **derive** (borderline: 8/9; single loss is the tblins-03 judgment) |
| image-replace | 0.71 | 3 | low-n | **derive** (mark low-n) |
| image-insert | 0.86 | 3 | low-n | **derive** (mark low-n) |
| fonts | 1.00 | 3 | low-n | omit (strong) |
| verify-safety | 1.00 | 7 | ok | omit (strong) |

`subscore = Σ(normalized×weight) / Σ(weight)` over that tag's questions.
Exact fractions: pdf-model 14/16, table-locate 9/9, row-delete 6.25/10,
cell-edit 11.75/16, table-relayout 12/16, table-insert 8/9, image-replace 5/7,
image-insert 6/7, fonts 7/7, verify-safety 17/17.

## Stack score

```
stack_score = Σ(normalized×weight) / Σ(weight) = 73.25 / 86 = 85.2%
```

## Derivation targets

Tags below threshold (`< 0.9`): **pdf-model, row-delete, cell-edit,
table-relayout, table-insert, image-replace (low-n), image-insert (low-n)** →
feed into `derived/pdf.nvidia__nemotron-3.5-super-vl.SKILL.md`.

**pdf-model (0.88)** — concrete wrong/missing claims:
- "If /Rotate is set, PyMuPDF already presents the rotated view (page
  width/height swapped; text bboxes are in rotated space)". Verified on 1.27.1:
  `get_text` bboxes and the rects passed to insert/draw calls are in the
  UNROTATED page's coordinates; only `page.rect` swaps width and height.
- PDF → DOCX/HTML → PDF round-trip (shared with table-relayout): lists the
  risks but never says to regenerate from the original source if it exists or
  to make a surgical in-place edit instead.

**row-delete (0.63)** — concrete wrong/missing claims:
- `apply_redactions()` defaults inverted: claims images and vector graphics
  default to NONE and survive unless removal is requested. The defaults are
  `images=PDF_REDACT_IMAGE_PIXELS` (2) and
  `graphics=PDF_REDACT_LINE_ART_REMOVE_IF_COVERED` (1), so a row redaction
  already deletes the row's shading rect and any rule inside it and blanks
  overlapping image pixels. The model therefore believes a text-only redaction
  is safe for borders and shading when it is not, and never names
  `graphics=0, images=0` as the way to keep them.
- Region move: "No. PyMuPDF cannot move an existing page region up while
  preserving its exact appearance"; proposes re-emitting operators or
  accepting the gap. Never offers `page.show_pdf_page(new_rect, src, pno,
  clip=old_rect)` from an untouched copy after redacting.
- Total is "updated" rather than recomputed from the remaining rows; content
  under the table (notes) is never considered when shifting.

**cell-edit (0.73)** — concrete wrong/missing claims:
- Offers "cover the glyph area with a fill rect in the cell background" as an
  equal alternative to redaction when replacing a cell value (the same cover-up
  recurs for images in pdf-imgrep-01 and pdf-imgrep-03).
- Never passes `graphics=0, images=0` on the cell redaction; with the real
  defaults a rect that fully covers a cell's shading rect removes it.
- `insert_textbox` described as writing "as much as fits" and silently dropping
  the rest. It writes nothing when the text does not fit, returns a negative
  number and raises nothing; the return value must be checked.

**table-relayout (0.75)** — concrete wrong/missing claims:
- Adding a column: shifts text and vertical rules to the new boundaries but
  never measures each column's content at its new width
  (`get_text_length`) nor plans a font-shrink/wrap fallback; never says to
  redact the old table; header fill and shading not redrawn.
- No reflow means content below an insertion point must be removed and
  re-stamped (`show_pdf_page(clip=)`); the model only knows "rewrite or
  re-draw downstream content".
- Round-trip: no "regenerate from the original source if it exists / ask for
  it", no surgical in-place edit named as the alternative.

**table-insert (0.89, borderline)** — concrete wrong/missing claims:
- When no gap exists between two paragraphs, its first options are to shrink
  the existing paragraphs' font size / line spacing or shrink the table to fit
  existing whitespace. Existing paragraphs cannot be re-typeset in place; the
  options are move-down (redact + restamp, spill to a new page), place
  elsewhere or on a new page and tell the user, or regenerate from source.

**image-replace (0.71, low-n)** — concrete wrong/missing claims:
- Does not know `page.replace_image(xref, filename=...)`; states "You cannot
  simply swap one shared XObject in place for all pages", which is exactly
  what `replace_image` does.
- Replaces a single placement by redacting with `images=PDF_REDACT_IMAGE_PIXELS`
  (blanks pixels, keeps the object and placement) or by covering with a
  background-coloured fill rect, then inserting on top. The single-occurrence
  technique is redact with `images=PDF_REDACT_IMAGE_REMOVE`, then
  `insert_image`.
- A vector logo: "cover the area with a background-colored filled rect" first,
  delete the paths as an afterthought. Vector art is removed by redacting with
  graphics removal, then the new image is inserted.
- Locates nested images by walking `/Resources /XObject` by hand instead of
  `page.get_image_info(xrefs=True)` / `get_images(full=True)`.

**image-insert (0.86, low-n)** — concrete wrong/missing claims:
- Knows to reuse the xref from the first `insert_image` but never names the
  `xref=` parameter.
- "Do not expect dedup to happen automatically": never states that
  `doc.save(..., garbage=4)` merges identical objects (levels 0–3 keep the
  copies).

Other factual errors outside the derived tags: "PyMuPDF has no built-in 'give
me the table' API" (`page.find_tables()` exists; table-locate stays at 1.00 via
the pdfplumber answer); `insert_image` "stretches to fill the rect" (default
`keep_proportion=True` letterboxes); glyph-coverage check and render
verification missing from the subset-font answer (fonts stays at 1.00).

Contamination check: all 36 ids present once, none empty or truncated. Answers
are the model's own compact prose, name APIs the key never mentions
(`get_image_rects`, `fitz.Pixmap`, manual `/Resources` traversal, the full
base-14 alias list) and contradict the key on plainly stated facts (redaction
defaults, `show_pdf_page` region move, `replace_image`, `insert_textbox`
all-or-nothing, `garbage=4`). No verbatim or near-verbatim overlap with the
reference answers. Consistent with a genuine closed-book run.
