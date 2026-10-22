---
model_id: deepseek-v4.1-flash
model_version: "4.1-2026-10-21"
evaluated_via: opencode-go
evaluated_on: 2026-10-21
stack: pdf
stack_corpus_rev: 1
threshold: 0.9
validation: with-skill
iteration: 2
sample: full   # all 36 questions
---

# Scorecard — deepseek-v4.1-flash on pdf (WITH derived skill, iteration 2, threshold 0.9)

> Valid ONLY for `deepseek-v4.1-flash` @ `4.1-2026-10-21`. A version bump invalidates
> this scorecard — re-benchmark.

Iteration 1 (97.7%, FAILED on image-insert) is preserved alongside as
`deepseek-v4.1-flash.iter1.with-skill.md`.

**What changed in the skill since iteration 1:**
- Digest item 6 and the image-insert section now cover these points:
  - xref reuse via `page.insert_image(rect, xref=xref)`;
  - in-document reuse of identical bytes;
  - `save(garbage=4)` merging duplicate streams (levels 0–3 do not);
  - "there is no `Document.insert_image`".
- New digest item 7 and a row-delete bullet: redact the stamp source only for content
  being DELETED, and stamp a pure move from an untouched copy.

Answers: `../answers/deepseek-v4.1-flash.with-skill.md` (iteration 2). They were produced
closed-book with the body of `derived/pdf.deepseek-v4.1-flash.SKILL.md` prepended as
"Active guidance":
- one fresh `ocode run` session per question (36 sessions);
- every tool denied, and the sqlite audit showed zero tool calls;
- the other Kaizen skills for this id disabled.

Graded against `questions.yaml` (corpus_rev 1) with the same rule and granularity as the
baseline and iteration 1:
- An equivalent correct technique in another library earns the point.
- A cover-up earns 0 for the removal point.
- A materially wrong claim about the construct a point tests withholds that point.
- A point is still awarded when the answer offers a complete correct action alongside a
  wrong side claim (precedents: cell-01, imgrep-03, iter1 tblins-03).
- A half-right point may get 0.5 (precedent: baseline rowdel-02).

PyMuPDF facts re-checked on 1.27.1 for this grading:
- After `set_rotation(90)`, `get_text("words")` bboxes are unchanged (unrotated
  coordinates); only `page.rect` swaps to 700×500.
- **`page.delete_image(xref)` is NOT page-scoped.** On a two-page doc sharing one xref,
  deleting it on page 0 turned page 1's image into a 1×1 blank too. It works by
  replacing the xref globally.
- `insert_text` at a word's bbox top-left draws the text about one line **too high**:
  the old word was at y 178.5–206.0 and the new text landed at y 157.0–184.5.
- `sRGB_to_rgb(0x808080)` returns `(128, 128, 128)` as 0–255 ints, and
  `insert_text(color=that)` raises `ValueError: need 1, 3 or 4 color components in
  range 0 to 1`. `sRGB_to_pdf` returns floats.
- `Rect + (0, -dy)` (a 2-tuple) raises `ValueError: Rect: bad seq len`.
- rowdel-03 geometry probe, using the answer's intended geometry with a 4-tuple shift
  because its 2-tuple raises: the moved block is stamped one row up and the target
  redaction covers only deleted row + destination. Result: words `Row0@103, Row2@123,
  Total@143, Total@163`, so **the Total is duplicated**.

## Per-question results

| id | tags | weight | full | awarded | normalized | notes |
|----|------|-------:|-----:|--------:|-----------:|-------|
| pdf-model-01 | pdf-model | 3 | 2 | 2 | 1.00 | |
| pdf-model-02 | pdf-model, verify-safety | 3 | 2 | 2 | 1.00 | |
| pdf-model-03 | pdf-model | 2 | 2 | 2 | 1.00 | |
| pdf-model-04 | pdf-model | 2 | 2 | 1 | 0.50 | The y-flip and CropBox math are right, and so is "get_text coords can be passed straight back". Point 2 withheld, the same call as iter1. It says words are "in that same rotated frame" and gives a raw→rotated mapping table, but the probe shows word bboxes stay unrotated. Non-target, same error as iter1 |
| pdf-locate-01 | table-locate | 3 | 2 | 2 | 1.00 | |
| pdf-locate-02 | table-locate | 2 | 2 | 2 | 1.00 | |
| pdf-locate-03 | table-locate, cell-edit | 2 | 2 | 1.5 | 0.75 | **Judgement call that decides the cell-edit margin.** Point 1 is earned (font, size, color, flags; the bits are right). On point 2, origin as the `insert_text` baseline is right. The only int→colour conversion is `fitz.sRGB_to_rgb(color)`, labelled "floats 0..1" and passed to `insert_text`; it returns 0–255 ints and raises. It is rescued only by the co-equal `get_texttrace()` route ("colour is a tuple of floats here"). 0.5 on point 2 (half right, rowdel-02 precedent). Full withholding would put cell-edit at 0.875 → FAIL. Awarding the full point would put it at 0.94 |
| pdf-locate-04 | table-locate, table-relayout | 2 | 2 | 2 | 1.00 | |
| pdf-rowdel-01 | row-delete | 3 | 3 | 3 | 1.00 | Stamp source redacts only the deleted row; target redacts the whole table (rebuild) or everything that moves (stamp), with fills → text → rules; Total recomputed; content below handled. Applies item 7 correctly. Code slip: `src_rect + (0, -dy)` 2-tuple raises |
| pdf-rowdel-02 | row-delete, cell-edit | 3 | 2 | 2 | 1.00 | Exact defaults (2/1/0) and semantics; `graphics=NONE, images=NONE` to keep the grid |
| pdf-rowdel-03 | row-delete | 2 | 2 | 1 | 0.50 | **Regression vs iter1 (1.00); it puts row-delete exactly at threshold.** Point 1 earned: `show_pdf_page(clip=)` from a copy with the deleted row redacted, one row higher. Point 2 withheld: on the target it redacts `Rect(x0, del.y0, x1, move.y1 - shift)`, which is exactly the destination, and it states the rule as "redact the union of deleted row + destination area". The moving block's old bottom row is never redacted, so it appears twice (probe: Total at 143 and 163). This is the only action offered, so the side-claim leniency does not apply. Baseline rowdel-03 lost the same point for a mis-scoped redaction |
| pdf-rowdel-04 | row-delete, verify-safety | 2 | 2 | 2 | 1.00 | |
| pdf-cell-01 | cell-edit | 3 | 3 | 3 | 1.00 | Tight `span["bbox"]` rect with `graphics/images=NONE`; x = old x1 − `Font.text_length`, on the old baseline; font/size/colour from the span (correct bit-shift colour). Side claim: "a rect that reaches the vertical rule deletes the rule" (under REMOVE_IF_COVERED a full-height rule is not covered). `except Exception: pass` in the font fallback |
| pdf-cell-02 | cell-edit | 2 | 2 | 2 | 1.00 | re-extracts neighbours |
| pdf-cell-03 | cell-edit, table-relayout | 2 | 2 | 2 | 1.00 | nothing written on negative return; options incl. htmlbox `scale_low` |
| pdf-cell-04 | cell-edit | 2 | 2 | 1 | 0.50 | **Regression vs baseline and iter1 (both 1.00); not from the skill.** Point 1 earned (baseline origin). Point 2 withheld: it says top-left as `point` draws the text "one ascent too LOW … collides with the line below". The probe shows it is about one line too HIGH. "Use `span["origin"]`" is present, but the stated consequence (the construct the point tests) is reversed. The skill says nothing about `insert_text`'s point |
| pdf-relayout-01 | table-relayout | 3 | 3 | 3 | 1.00 | redact band, redraw/stamp lower, recomputed Total, rules extended, striping, pagination; pure move stamped from an untouched copy (item 7 applied correctly) |
| pdf-relayout-02 | table-relayout | 3 | 3 | 3 | 1.00 | same total width, measure/wrap, redact whole table, redraw fills → text → rules |
| pdf-relayout-03 | table-relayout, pdf-model | 2 | 2 | 2 | 1.00 | |
| pdf-relayout-04 | table-relayout, table-insert | 2 | 2 | 2 | 1.00 | text + drawings + images + annots; intersects with pad; page/margin limit |
| pdf-tblins-01 | table-insert | 3 | 2 | 2 | 1.00 | direct drawing (fills → text → rules, checks `insert_textbox` return); reportlab → `show_pdf_page`; pypdf `merge_page` noted |
| pdf-tblins-02 | table-insert | 2 | 2 | 2 | 1.00 | style from spans/drawings; fills → text → rules; one line per boundary, snapped coords |
| pdf-tblins-03 | table-insert, table-relayout | 2 | 2 | 2 | 1.00 | **Iter1 problem fixed.** "leave the copy untouched (pure move — no redaction in the stamp source)", redact the moving band on the target, restamp lower, footer overflow → next page; alternatives (free space, relocate, rebuild). The "what not to do" list states both stamp-source cases correctly. No longer a judgement call |
| pdf-imgrep-01 | image-replace | 3 | 3 | 3 | 1.00 | `replace_image` + shared-xref catch + aspect stretch. **Wrong:** "`delete_image` is page-scoped, so the other pages keep the old xref" (probe: it blanks the shared xref everywhere). Point 2 kept on the co-equal "redaction (`images=PDF_REDACT_IMAGE_REMOVE`) + insert" route, per the iter1 tblins-03 precedent. Non-target |
| pdf-imgrep-02 | image-replace, verify-safety | 2 | 2 | 2 | 1.00 | |
| pdf-imgrep-03 | image-replace | 2 | 2 | 2 | 1.00 | inline / Form XObject / vector / Type3 / annotation / pattern; `get_image_info`, redact-graphics-then-insert for vector |
| pdf-imgins-01 | image-insert | 3 | 2 | 2 | 1.00 | right edge = body content x1, bottom = footer top − pad, height from pixel aspect, word/drawing/image collision check; xref reuse aside |
| pdf-imgins-02 | image-insert | 2 | 2 | 2 | 1.00 | **Iter1 problem fixed.** `insert_image(..., xref=xref)` on the other pages; "no `Document.insert_image`"; identical bytes reuse one xref in-document; "levels 0–3 do not merge duplicate streams; use `garbage=4`". All correct on 1.27.1 |
| pdf-imgins-03 | image-insert, pdf-model | 2 | 2 | 2 | 1.00 | |
| pdf-fonts-01 | fonts | 3 | 2 | 2 | 1.00 | has_glyph check named |
| pdf-fonts-02 | fonts | 2 | 2 | 2 | 1.00 | |
| pdf-fonts-03 | fonts, cell-edit | 2 | 2 | 2 | 1.00 | flags & 16, size, explicit bold font |
| pdf-verify-01 | verify-safety | 3 | 3 | 3 | 1.00 | It reopens and does negative/positive text assertions, a table-model diff with a recomputed Total, render diff, untouched-page hash compare, and a decompressed-stream grep. "A byte-level check, because a clip can hide a deleted row" covers "a visual check can't catch it" |
| pdf-verify-02 | verify-safety | 3 | 2 | 2 | 1.00 | |
| pdf-verify-03 | verify-safety | 2 | 2 | 2 | 1.00 | page count / metadata / encryption; "other pages" via page count only (same leniency as before) |
| pdf-verify-04 | verify-safety, pdf-model | 2 | 2 | 2 | 1.00 | empty match set, off-CropBox text, wrong redaction flags deleting grid lines; assert and exit non-zero |

`normalized = min(awarded, full) / full`

## Per-tag subscores

| tag | Σ(norm×w) / Σw | subscore | n | trust | action |
|-----|---------------|---------:|--:|-------|--------|
| table-insert | 9 / 9 | 1.00 | 4 | ok | TARGET |
| table-relayout | 16 / 16 | 1.00 | 7 | ok | omit |
| verify-safety | 17 / 17 | 1.00 | 7 | ok | omit |
| image-insert | 7 / 7 | 1.00 | 3 | low-n | TARGET |
| image-replace | 7 / 7 | 1.00 | 3 | low-n | omit |
| fonts | 7 / 7 | 1.00 | 3 | low-n | omit |
| table-locate | 8.5 / 9 | 0.94 | 4 | ok | omit |
| pdf-model | 15 / 16 | 0.94 | 7 | ok | omit |
| cell-edit | 14.5 / 16 | 0.91 | 7 | ok | TARGET |
| row-delete | 9 / 10 | 0.90 | 4 | ok | TARGET (exactly at threshold) |

`subscore = Σ(normalized×weight) / Σ(weight)` over that tag's questions.

## Stack score

```
Σ(weight) = 86
losses: pdf-model-04 2×0.5 = 1, pdf-rowdel-03 2×0.5 = 1, pdf-cell-04 2×0.5 = 1,
        pdf-locate-03 2×0.25 = 0.5
stack_score = Σ(normalized×weight) / Σ(weight) = 82.5 / 86 = 95.9%
```

(baseline without skill: 77.25 / 86 = 89.8%; iteration 1: 84 / 86 = 97.7%)

## Validation result — target tags: baseline → iter1 → iter2

| tag | baseline | iter1 | iter2 | pass (≥ 0.9)? |
|-----|---------:|------:|------:|:-------------:|
| row-delete | 0.58 | 1.00 | 0.90 | **PASS** (exactly at threshold) |
| cell-edit | 0.86 | 1.00 | 0.91 | **PASS** (decided by the locate-03 call) |
| table-insert | 0.89 | 1.00 | 1.00 | **PASS** (no longer depends on a judgement call) |
| image-insert (low-n) | 0.79 | 0.86 | 1.00 | **PASS** |

**All four targets reach 0.9, so the iteration-2 validation PASSES.** The margin is thin
on two tags:

- **row-delete = 0.90.** rowdel-03 lost point 2 because its target-page redaction
  covers deleted row + destination instead of the old position of the moving block.
  - That leaves the last moved row (the Total) duplicated.
  - One more half-point lost on any row-delete question would fail the tag.
- **cell-edit = 0.906.** Two things pulled it down:
  - cell-04 reversed the direction of the top-left-as-baseline error (not from the skill);
  - the locate-03 call (`sRGB_to_rgb` mislabelled as floats).
  - If locate-03 point 2 were withheld fully, cell-edit would be 0.875 and **FAIL**.
  - If it were awarded fully, cell-edit would be 0.94.

**Iteration-1 problems:**
- **imgins-02: fixed.** The answer now gives `xref=` reuse, in-document dedupe,
  `garbage=4` versus levels 0–3, and no `Document.insert_image`. All of these are
  correct, and image-insert rises 0.86 → 1.00.
- **tblins-03: fixed.** It explicitly stamps a pure move from an untouched copy, and
  its "what not to do" list covers both the empty-stamp case and the
  deleted-content-survives case. relayout-01 and rowdel-01/03 also apply item 7
  correctly in the stamp source.

### New wrong facts and whether the skill caused them

| where | claim | true behaviour (1.27.1) | skill-induced? |
|-------|-------|-------------------------|:--------------:|
| rowdel-03 | on the target, redact "the union of deleted row + destination area" before stamping | the moving block's OLD position must be redacted; the destination ⊂ it; the answer's rect leaves the last moved row duplicated | **plausibly** — its notes repeat item 7 almost verbatim. Item 7's second sentence ("never redact the region you are about to stamp") does not repeat "in the stamp source". The row-delete bullet "never stamp onto a region whose old content is still on the page" is about the destination. Either can push the model to redact the destination instead of the old block |
| imgrep-03 | "lower garbage levels may leave the old image bytes in the file" | an orphaned stream is dropped at garbage ≥ 1; level 4 is needed only to MERGE duplicates | mild over-generalisation of item 6 (unscored) |
| cell-01 | a rect reaching the vertical rule deletes the rule | REMOVE_IF_COVERED only deletes a path entirely inside the rect | possibly (the "don't pad to catch rules" bullet); unscored |
| cell-04 | top-left as `point` draws the text one ascent too LOW | about one line too HIGH | no |
| locate-03 | `sRGB_to_rgb` gives floats 0..1 for `insert_text` | returns 0–255 ints; `insert_text` raises | no |
| model-04 | word bboxes are in the rotated frame | unrotated; only `page.rect` swaps | no (same as iter1) |
| imgrep-01 | `delete_image` is page-scoped | it replaces the xref globally (other pages blanked) | no |

Every other use of skill content checks out against the key and the probes:
- redaction defaults;
- REMOVE_IF_TOUCHED caution;
- clip ≠ removal;
- fills → text → rules;
- content-anchored image rects;
- xref reuse and `garbage=4`;
- stamp source untouched for a pure move.

Skill bleed with no cost:
- the empty-stamp blank-cell check in verify-01;
- the white-header-text reminders in tblins-02, relayout-02 and verify-01;
- the "garbage=4 also deduplicates" aside in rowdel-01.

### Non-target drift (not acted on)

| tag | baseline | iter1 | iter2 | note |
|-----|---------:|------:|------:|------|
| table-relayout | 0.94 | 1.00 | 1.00 | |
| verify-safety | 0.94 | 1.00 | 1.00 | |
| pdf-model | 0.94 | 0.94 | 0.94 | model-04 rotation-frame error again |
| table-locate | 1.00 | 1.00 | 0.94 | locate-03 colour conversion (`sRGB_to_rgb`) |
| image-replace / fonts | 1.00 | 1.00 | 1.00 | imgrep-01 kept its point despite the wrong `delete_image` scope claim |

### Proposed skill hardening (optional — every target passes; not applied)

The validation passes, so per HOW-TO-EVALUATE no tweak is required. If row-delete's
0.90 is judged too thin to ship, sharpen the following.

1. **row-delete (rowdel-03).** Replace the row-delete bullet "Never stamp onto a region
   whose old content is still on the page" with:

   > On the TARGET page, redact the moving block at its OLD position, from the deleted
   > row's top to the old bottom of the last moving element. The destination lies inside
   > that span. Redacting only the deleted row plus the destination leaves the last moved
   > row (often the Total) in place, and it shows twice.

   Also scope both sentences of digest item 7 to the copy: "In the stamp-SOURCE copy,
   redact only what is being deleted … never redact, in that copy, the region you are
   about to stamp." The target page still always gets the moving block redacted.
2. **cell-edit.** These two errors are not from the skill. Add them only if cell-edit's
   margin matters:
   - "A span's `color` is an sRGB int: `pymupdf.sRGB_to_pdf(c)` gives the 0–1 floats
     `insert_text` needs, and `sRGB_to_rgb` returns 0–255 ints, which `insert_text`
     rejects."
   - "`insert_text`'s point is the baseline; passing a bbox top-left draws the text about
     one line too HIGH."

## Odd things in the answers (not scored)

- `Rect + (0, -dy)` with a 2-tuple, which raises `ValueError: Rect: bad seq len`
  (rowdel-01 stamp route, rowdel-03).
- imgrep-01 says `get_image_info` bboxes are in unrotated coordinates; imgrep-03 says
  they "already account for /Rotate". The two contradict each other.
- Recap block (`ASKED / WORKED / FOUND / DECIDED / NEXT`) in cell-01.
- locate-03 font-reuse snippet: `"FX" if buf else "helv"` references `buf` when no font
  matched (NameError).
- tblins-01: `assert is_free(...) or True` is a no-op check.
