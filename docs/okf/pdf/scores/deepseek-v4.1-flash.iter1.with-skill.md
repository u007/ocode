---
model_id: deepseek-v4.1-flash
model_version: "4.1-2026-10-21"
evaluated_via: opencode-go
evaluated_on: 2026-10-21
stack: pdf
stack_corpus_rev: 1
threshold: 0.9
validation: with-skill
sample: full   # all 36 questions
---

# Scorecard — deepseek-v4.1-flash on pdf (WITH derived skill, threshold 0.9)

> Valid ONLY for `deepseek-v4.1-flash` @ `4.1-2026-10-21`. A version bump invalidates
> this scorecard — re-benchmark.

Answers: `../answers/deepseek-v4.1-flash.with-skill.md`, produced closed-book with the
body of `derived/pdf.deepseek-v4.1-flash.SKILL.md` prepended as "Active guidance". There
was one fresh `ocode run` session per question (36 sessions), every tool was denied, and
the sqlite audit showed zero tool calls. The other Kaizen skills for this id were
disabled, so the skill was injected once. Graded against `questions.yaml` (corpus_rev 1)
with the same rule and granularity as the baseline `deepseek-v4.1-flash.md`:

- An equivalent correct technique in another library earns the point.
- A cover-up earns 0 for the removal point.
- A materially wrong claim about the construct a point tests withholds that point.
- A point is still awarded when the answer offers a complete correct action alongside a
  wrong side claim (baseline precedent: cell-01, imgrep-03).

PyMuPDF facts were re-checked on 1.27.1 for this grading:
- `PDF_REDACT_TEXT_NONE` = 1 and `PDF_REDACT_IMAGE_REMOVE_UNLESS_INVISIBLE` = 3 both exist.
- There is no `Document.insert_image`.
- Inserting the same PNG bytes on three pages in one session returns ONE xref (`[9, 9, 9]`).
- After `set_rotation(90)`, `get_text("words")` bboxes are unchanged (unrotated
  coordinates), and so is `insert_text`'s point.
- Pixel-redacting a shared image on page 0 gives page 0 a new image xref and leaves page
  1 on the original.
- `add_redact_annot(fill=False)` is accepted.
- `insert_htmlbox(scale_low=0)` means unlimited down-scaling.
- `insert_textbox(fontsize=0)` writes nothing and returns the full rect height.

## Per-question results

| id | tags | weight | full | awarded | normalized | notes |
|----|------|-------:|-----:|--------:|-----------:|-------|
| pdf-model-01 | pdf-model | 3 | 2 | 2 | 1.00 | |
| pdf-model-02 | pdf-model, verify-safety | 3 | 2 | 2 | 1.00 | |
| pdf-model-03 | pdf-model | 2 | 2 | 2 | 1.00 | |
| pdf-model-04 | pdf-model | 2 | 2 | 1 | 0.50 | CropBox math and `transformation_matrix` right. Point 2 withheld: "word boxes, search hits, pixmaps and the drawing methods are all in that rotated frame … not its PDF-space numbers". On 1.27.1 the word bboxes do not change under `/Rotate`; they and `insert_text` share unrotated coordinates, and only `page.rect` swaps. This is wrong on the exact thing the question asks. Non-target |
| pdf-locate-01 | table-locate | 3 | 2 | 2 | 1.00 | |
| pdf-locate-02 | table-locate | 2 | 2 | 2 | 1.00 | |
| pdf-locate-03 | table-locate, cell-edit | 2 | 2 | 2 | 1.00 | flag bits correct |
| pdf-locate-04 | table-locate, table-relayout | 2 | 2 | 2 | 1.00 | |
| pdf-rowdel-01 | row-delete | 3 | 3 | 3 | 1.00 | The stamp source has the deleted row redacted and applied. On the target page it redacts the deleted row plus everything that moves (or the whole table), then redraws with fills → text → rules, recomputes the Total and handles the notes below. **Nit:** the Strategy B loop restamps the rows ABOVE the deletion at `dy=0`, which duplicates them unless the whole table was redacted. Awarded because Strategy A (rebuild from data) is complete |
| pdf-rowdel-02 | row-delete, cell-edit | 3 | 2 | 2 | 1.00 | All three defaults and their values are exact, including "partly covered paths are left whole". It passes `graphics=0, images=0` to keep the grid, and keeps the defaults when rebuilding. Fixes the baseline's `LINE_ART_NONE` error |
| pdf-rowdel-03 | row-delete | 2 | 2 | 2 | 1.00 | It stamps with `show_pdf_page(clip=band)` from a reopened copy in which the deleted row was redacted. On the target it redacts the deleted row AND the band before stamping one row higher. Fixes the baseline's redact-only-the-band error |
| pdf-rowdel-04 | row-delete, verify-safety | 2 | 2 | 2 | 1.00 | |
| pdf-cell-01 | cell-edit | 3 | 3 | 3 | 1.00 | Tight rect, `graphics=0, images=0`, `fill=False`, x = old x1 − width on the old baseline, and the span's font/size/colour. Code slip: `span["bbox"].x1` on a tuple. Its rotation note ("derotate before drawing") is wrong (see header) but scores nothing |
| pdf-cell-02 | cell-edit | 2 | 2 | 2 | 1.00 | re-extracts the neighbours afterwards |
| pdf-cell-03 | cell-edit, table-relayout | 2 | 2 | 2 | 1.00 | Says nothing is written on a negative return; lists the options. Says `scale_low` default 0 = "no scaling", which is backwards (0 = unlimited) |
| pdf-cell-04 | cell-edit | 2 | 2 | 2 | 1.00 | `bbox.y1 - descender` sign slip again; the numeric example `bbox.y1 - 0.2*fontsize` is right |
| pdf-relayout-01 | table-relayout | 3 | 3 | 3 | 1.00 | Redact, then redraw the shifted Total (recomputed) and the content below; extend the rules and keep the striping; paginate past the bottom margin |
| pdf-relayout-02 | table-relayout | 3 | 3 | 3 | 1.00 | Keeps the same total width and checks the fit. Redacts the WHOLE table and redraws it with fills → text → rules. Fixes baseline point 3 |
| pdf-relayout-03 | table-relayout, pdf-model | 2 | 2 | 2 | 1.00 | |
| pdf-relayout-04 | table-relayout, table-insert | 2 | 2 | 2 | 1.00 | |
| pdf-tblins-01 | table-insert | 3 | 2 | 2 | 1.00 | direct drawing with fills → text → rules; reportlab → `show_pdf_page` |
| pdf-tblins-02 | table-insert | 2 | 2 | 2 | 1.00 | Copies spans, rule width and colour; paint order fills → text → rules; snapped coordinates. Fixes the baseline miss. Skill bleed: "Height = width × (source h/w) for images" in a table answer (harmless) |
| pdf-tblins-03 | table-insert, table-relayout | 2 | 2 | 2 | 1.00 | **Judgement call; it decides the table-insert verdict.** Option 1 says to restamp the moved region "from a copy in which the same region was also redacted". That stamp would be EMPTY, and "don't stamp from an unredacted copy" is wrong for a pure move. Both are **skill-induced**: digest items 2–3, written for row deletion, applied to a move. Point 1 is still awarded because the answer gives a complete co-equal path, "re-rendering the extracted text/blocks at the new coordinates", plus page overflow (cell-01/imgrep-03 precedent). Withholding it would put table-insert at 8/9 = 0.89 → FAIL |
| pdf-imgrep-01 | image-replace | 3 | 3 | 3 | 1.00 | Uses `replace_image`, covers the shared-xref catch, does a single-occurrence copy-on-write resource remap (equivalent), and notes aspect/stretch. Wrongly says pixel-redacting a shared image can blank other pages (probe: it does not) |
| pdf-imgrep-02 | image-replace, verify-safety | 2 | 2 | 2 | 1.00 | |
| pdf-imgrep-03 | image-replace | 2 | 2 | 2 | 1.00 | |
| pdf-imgins-01 | image-insert | 3 | 2 | 2 | 1.00 | The helper still starts from `page.rect − 36`, but a dedicated section computes x1 from the content's right edge and y1 from the footer top ("not `page.rect.x1 - 36` blindly"). Aspect ratio and the word/image/drawing intersection check are present. Fixes the baseline's point 1. Side claims: `Pixmap(png)` "reads only the header" (it decodes the image) |
| pdf-imgins-02 | image-insert | 2 | 2 | 1 | 0.50 | **Regression vs baseline (1.00).** Point 1 earned via the manual "insert once, add the same xref to every page's `/Resources`" option. It never names `insert_image(..., xref=)`. Point 2 withheld: "GC … does **not** merge duplicates — prevention is the fix", but `garbage=4` merges identical streams (key, verified 1.27.1). Also wrong: "Identical bytes are not deduplicated" (the probe returned one xref for three inserts) and `Document.insert_image` (does not exist). None of this is in the skill |
| pdf-imgins-03 | image-insert, pdf-model | 2 | 2 | 2 | 1.00 | |
| pdf-fonts-01 | fonts | 3 | 2 | 2 | 1.00 | no `has_glyph` (same leniency as baseline) |
| pdf-fonts-02 | fonts | 2 | 2 | 2 | 1.00 | |
| pdf-fonts-03 | fonts, cell-edit | 2 | 2 | 2 | 1.00 | Flag masks right (16/2/4/8). Says `fontsize=0` makes `insert_textbox` auto-fit (probe: it writes nothing). `get_fonts(full=True)` is unpacked as a 6-tuple (it returns 7) |
| pdf-verify-01 | verify-safety | 3 | 3 | 3 | 1.00 | Re-extracts the reopened file, checks `find_tables` counts, renders, scans the raw streams for clipped content. Says render and text layer are both needed ("text layer catches invisible leftovers"). "Other pages unchanged" is not explicit (page count only), same leniency as baseline |
| pdf-verify-02 | verify-safety | 3 | 2 | 2 | 1.00 | |
| pdf-verify-03 | verify-safety | 2 | 2 | 2 | 1.00 | |
| pdf-verify-04 | verify-safety, pdf-model | 2 | 2 | 2 | 1.00 | Now names the silent no-ops: empty `search_for`, out-of-page/bogus rects, destructive redaction defaults. Fixes the baseline miss |

`normalized = min(awarded, full) / full`

## Per-tag subscores

| tag | Σ(norm×w) / Σw | subscore | n | trust | action |
|-----|---------------|---------:|--:|-------|--------|
| table-locate | 9 / 9 | 1.00 | 4 | ok | omit |
| row-delete | 10 / 10 | 1.00 | 4 | ok | TARGET |
| cell-edit | 16 / 16 | 1.00 | 7 | ok | TARGET |
| table-insert | 9 / 9 | 1.00 | 4 | ok | TARGET |
| table-relayout | 16 / 16 | 1.00 | 7 | ok | omit |
| verify-safety | 17 / 17 | 1.00 | 7 | ok | omit |
| image-replace | 7 / 7 | 1.00 | 3 | low-n | omit |
| fonts | 7 / 7 | 1.00 | 3 | low-n | omit |
| pdf-model | 15 / 16 | 0.94 | 7 | ok | omit |
| image-insert | 6 / 7 | 0.86 | 3 | low-n | TARGET — **below threshold** |

`subscore = Σ(normalized×weight) / Σ(weight)` over that tag's questions.

## Stack score

```
Σ(weight) = 86
losses: pdf-model-04 2×0.5 = 1, pdf-imgins-02 2×0.5 = 1
stack_score = Σ(normalized×weight) / Σ(weight) = 84 / 86 = 97.7%
```

(baseline without skill, same model version: 77.25 / 86 = 89.8%)

## Validation result — target tags: baseline → with-skill

| tag | baseline | with-skill | Δ | pass (≥ 0.9)? |
|-----|---------:|-----------:|---:|:-------------:|
| row-delete | 0.58 | 1.00 | +0.42 | **PASS** |
| cell-edit | 0.86 | 1.00 | +0.14 | **PASS** |
| table-insert | 0.89 | 1.00 | +0.11 | **PASS** (depends on the tblins-03 judgement call) |
| image-insert (low-n) | 0.79 | 0.86 | +0.07 | **FAIL** |

**3 of 4 targets pass, so the validation FAILS on image-insert.**

- **row-delete.** All three baseline failures are fixed:
  - rowdel-02 states the real defaults.
  - rowdel-01 redacts both the deleted row and the region that moves, instead of
    rebuilding from clipped bands.
  - rowdel-03 redacts the deleted row before stamping.
- **cell-edit.** Fixed through rowdel-02. The other cell answers stayed correct and now
  name the REMOVE_IF_COVERED default.
- **table-insert.** tblins-02 now states fills → text → rules.
  - The pass is fragile. tblins-03 contains a skill-induced error: it says to stamp the
    moved region from a copy in which that same region was redacted.
  - The point was kept only because the answer also gives a correct "re-render the
    extracted blocks" path.
  - If that point were withheld, table-insert would be 0.89 and fail.
- **image-insert.** The targeted failure is fixed: imgins-01 now anchors to the content
  x1 and the footer top.
  - The tag fails on imgins-02, a question the skill does not cover, which the baseline
    answered fully.
  - This sample denies that `garbage=4` merges duplicates and claims identical inserts are
    never deduplicated.
  - The tag is low-n (3 questions), so this may be resampling variance. Still, every
    target must reach 0.9, and wrong API facts are not noise.

### New wrong facts and whether the skill caused them

| where | claim | true behaviour (1.27.1) | skill-induced? |
|-------|-------|-------------------------|:--------------:|
| tblins-03 | restamp a moved region from a copy where that region was redacted; never stamp from an unredacted copy | that stamp is empty; a pure move (nothing deleted) may stamp from an untouched copy | **yes** — digest items 2–3 (row-deletion rule over-generalised to any move) |
| imgins-02 | `garbage=4` does not merge duplicates; identical inserts not deduped; `Document.insert_image` | garbage=4 merges identical streams; same bytes → same xref; no such method | no |
| model-04 | word bboxes / drawing methods are in the rotated frame | both are unrotated; only `page.rect` swaps | no (same contradiction the baseline noted elsewhere) |
| cell-01 | derotate coordinates before `insert_text` on a rotated page | extract and insert share unrotated coords | no |
| imgrep-01 | pixel redaction of a shared image blanks other pages | page gets its own new image xref; others untouched | no |
| fonts-03 | `insert_textbox(fontsize=0)` auto-fits | writes nothing | no |
| cell-03 | `insert_htmlbox` `scale_low=0` = no scaling | 0 = unlimited down-scaling | no |

The skill's own statements were all checked against the key and the 1.27.1 probes:
redaction defaults, REMOVE_IF_COVERED semantics, `show_pdf_page` embedding the whole
page, paint order, and content-anchored image placement. All are correct. Its only
measured harm is the tblins-03 over-generalisation. Skill bleed with no cost:
- the image-aspect sentence in tblins-02;
- the "intersects no bbox" check in relayout-02;
- the white-header-style sentence repeated in relayout-02.

### Non-target drift (not acted on)

| tag | baseline | with-skill | note |
|-----|---------:|-----------:|------|
| table-relayout | 0.94 | 1.00 | relayout-02 now redacts the whole old table |
| verify-safety | 0.94 | 1.00 | verify-04 now names the silent no-ops |
| pdf-model | 0.94 | 0.94 | model-04 dropped to 0.50 (rotation-frame claim); verify-04 recovered |
| table-locate / image-replace / fonts | 1.00 | 1.00 | = |

### Proposed skill fixes (for the next iteration; not applied here)

1. **image-insert.** Add these directives:
   - "To put the same image on many pages, insert it once and keep the returned xref,
     then call `page.insert_image(rect, xref=xref)` on the other pages."
   - "`doc.save(garbage=4)` merges identical streams (levels 0–3 do not)."
   - "There is no `Document.insert_image`."
   - "PyMuPDF already reuses an identical image within a session; still pass `xref=`
     explicitly."
2. **Moving a region (tblins-03 / digest items 2–3).** Scope the "redact the stamp source"
   rule to the content being DELETED: "In the stamp source, redact only what is being
   deleted (e.g. the deleted row). For a pure move with nothing deleted, stamp from an
   untouched copy. Never redact the region you are about to stamp, because the stamp
   comes out empty."

## Odd things in the answers (not scored)

- Recap blocks (`ASKED / BUILT / FOUND / DECIDED / NEXT`) still leak into model-02,
  locate-03 and imgrep-01.
- Code slips:
  - `span["bbox"].x1` on a tuple (cell-01);
  - `get_fonts(full=True)` unpacked as a 6-tuple (fonts-03);
  - tblins-01 says "don't use `page.rect` minus a fixed margin", then does exactly that.
