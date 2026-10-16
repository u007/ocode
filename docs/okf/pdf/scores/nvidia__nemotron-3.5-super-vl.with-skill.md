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
     segment; `.with-skill` marks the validation run of the derived skill. -->

# Scorecard — nvidia/nemotron-3.5-super-vl on pdf (with-skill validation)

> Valid ONLY for `nvidia/nemotron-3.5-super-vl` @ `3.5`. A version bump invalidates
> this scorecard — re-benchmark.

This is the **validation** run for `../derived/pdf.nvidia__nemotron-3.5-super-vl.SKILL.md`
(HOW-TO-EVALUATE.md "Validate the derived skill"). It derives no new skill.
Baseline: `nvidia__nemotron-3.5-super-vl.md` (85.2%). Answers:
`pdf/answers/nvidia__nemotron-3.5-super-vl.with-skill.md`. Graded against
`pdf/questions.yaml` @ corpus_rev 1 with the same standards as the baseline:
PyMuPDF facts verified on 1.27.1; a materially wrong claim about the construct a
point names withholds the point ("(judgment)").

## Provenance

Closed-book (Rule 0) with the derived skill active. The answerer was
`ocode run -m openrouter/nvidia/nemotron-3.5-super-vl:free -effort med` under an
isolated HOME whose config denies every tool, in a scratch cwd containing only
the `_prompts/pdf.md` sheet with the derived skill body (front matter stripped)
prepended as active guidance; one fresh session, 1 user + 1 assistant message,
0 tool calls. All 36 ids came back once, none empty. No answer cites a tool or
skill the question never named; the words "skill"/"guidance" occur only in the
answer file's header line. One answer cross-references another question by id
("(pdf-fonts-01)" in pdf-fonts-03), so the answers were produced in one pass.
Still uses the legacy `fitz` alias. No duplicated answers, no thinking-aloud, no
fabricated tool output.

Absorption check: the answers now state the skill's facts in the model's own
words and apply them to questions the skill never addressed directly
(`show_pdf_page` restamping reused for pdf-relayout-01, pdf-relayout-04 and
pdf-tblins-01; the `insert_textbox` return check reused in pdf-tblins-02 and
pdf-verify-04). Overlap with the rubric on target tags is expected: this
measures absorption, not independent knowledge.

## Per-question results

| id | tags | weight | full | awarded | normalized | notes |
|----|------|-------:|-----:|--------:|-----------:|-------|
| pdf-model-01 | pdf-model | 3 | 2 | 2 | 1.00 | cover-up wording from the baseline is gone |
| pdf-model-02 | pdf-model, verify-safety | 3 | 2 | 2 | 1.00 | |
| pdf-model-03 | pdf-model | 2 | 2 | 2 | 1.00 | |
| pdf-model-04 | pdf-model | 2 | 2 | 2 | 1.00 | baseline 0.50 → fixed: unrotated frame for bboxes, `rotation_matrix`/`derotation_matrix`, `transformation_matrix` for the CropBox offset |
| pdf-locate-01 | table-locate | 3 | 2 | 1 | 0.50 | **regressed from 1.00**: repeats the false "PyMuPDF has no ... built-in table detector" (`page.find_tables()` exists) and this time names no finder at all (baseline earned the point via pdfplumber). Rules + words reconstruction earns point 2 only |
| pdf-locate-02 | table-locate | 2 | 2 | 2 | 1.00 | still no uniqueness check |
| pdf-locate-03 | table-locate, cell-edit | 2 | 2 | 2 | 1.00 | |
| pdf-locate-04 | table-locate, table-relayout | 2 | 2 | 2 | 1.00 | x1 as the right edge; the `x = edge − measured width` placement step is not restated this run |
| pdf-rowdel-01 | row-delete | 3 | 3 | 3 | 1.00 | redact + `show_pdf_page(clip=)` from an untouched copy, Total recomputed from rows, content below handled |
| pdf-rowdel-02 | row-delete, cell-edit | 3 | 2 | 2 | 1.00 | baseline 0.25 → fixed: real defaults (images 2, graphics 1, text 0) and `graphics=0, images=0` to keep the frame |
| pdf-rowdel-03 | row-delete | 2 | 2 | 2 | 1.00 | baseline 0.25 → fixed: `show_pdf_page(new_rect, src, pno, clip=old_rect)` paired with redaction |
| pdf-rowdel-04 | row-delete, verify-safety | 2 | 2 | 2 | 1.00 | striping now included |
| pdf-cell-01 | cell-edit | 3 | 3 | 3 | 1.00 | baseline 0.67 → fixed: no fill-rect alternative; `apply_redactions(graphics=0, images=0)` |
| pdf-cell-02 | cell-edit | 2 | 2 | 2 | 1.00 | still no "verify neighbours afterwards" |
| pdf-cell-03 | cell-edit, table-relayout | 2 | 2 | 2 | 1.00 | baseline 0.50 → fixed: all-or-nothing, negative return, no exception, check it |
| pdf-cell-04 | cell-edit | 2 | 2 | 2 | 1.00 | |
| pdf-relayout-01 | table-relayout | 3 | 3 | 3 | 1.00 | |
| pdf-relayout-02 | table-relayout | 3 | 3 | 3 | 1.00 | baseline 0.67 → fixed: measures with `get_text_length`, redacts the whole table, redraws header fill/shading/rules at new x |
| pdf-relayout-03 | table-relayout, pdf-model | 2 | 2 | 2 | 1.00 | baseline 0.50 → fixed: ask for the source and regenerate, else surgical in-place edit |
| pdf-relayout-04 | table-relayout, table-insert | 2 | 2 | 2 | 1.00 | |
| pdf-tblins-01 | table-insert | 3 | 2 | 2 | 1.00 | |
| pdf-tblins-02 | table-insert | 2 | 2 | 2 | 1.00 | |
| pdf-tblins-03 | table-insert, table-relayout | 2 | 2 | 2 | 1.00 | baseline 0.50 → fixed: redact + restamp, spill to a new page, tell the user, regenerate from source; explicitly refuses to shrink the paragraphs |
| pdf-imgrep-01 | image-replace | 3 | 3 | 3 | 1.00 | baseline 0.67 → fixed: `replace_image(xref, filename=)`, single occurrence via `images=PDF_REDACT_IMAGE_REMOVE` + `insert_image`, aspect caveat |
| pdf-imgrep-02 | image-replace, verify-safety | 2 | 2 | 2 | 1.00 | still no "verify exactly one image at that rect" |
| pdf-imgrep-03 | image-replace | 2 | 2 | 2 | 1.00 | baseline 0.50 → fixed: all four causes, vector → redact graphics then insert, nested via `get_image_info(xrefs=True)`/`get_images(full=True)` |
| pdf-imgins-01 | image-insert | 3 | 2 | 2 | 1.00 | still anchors to `page.width/height` − margins rather than content edges, then collision-checks |
| pdf-imgins-02 | image-insert | 2 | 2 | 2 | 1.00 | baseline 0.50 → fixed: `xref=` reuse and `garbage=4` merge |
| pdf-imgins-03 | image-insert, pdf-model | 2 | 2 | 2 | 1.00 | paint order via call order rather than the `overlay` flag; off-page/crop/zero rect named; verification via render, not `get_image_info` |
| pdf-fonts-01 | fonts | 3 | 2 | 2 | 1.00 | still no glyph-coverage check; "render as outlines" is an odd extra option |
| pdf-fonts-02 | fonts | 2 | 2 | 2 | 1.00 | remedy (embed a TTF) now stated |
| pdf-fonts-03 | fonts, cell-edit | 2 | 2 | 2 | 1.00 | |
| pdf-verify-01 | verify-safety | 3 | 3 | 3 | 1.00 | text-layer absence check + render both required; "render alone can't catch overlays" is implicit rather than stated |
| pdf-verify-02 | verify-safety | 3 | 2 | 2 | 1.00 | |
| pdf-verify-03 | verify-safety | 2 | 2 | 1 | 0.50 | **regressed from 1.00**: point 2 lost — nothing about other pages untouched, signatures, encryption or metadata (baseline at least named the signature) |
| pdf-verify-04 | verify-safety, pdf-model | 2 | 2 | 2 | 1.00 | negative `insert_textbox` return now named |

`normalized = min(awarded, full) / full`

## Per-tag subscores

| tag | baseline | with-skill | n | trust | target? | result |
|-----|---------:|-----------:|--:|-------|---------|--------|
| pdf-model | 0.88 | 1.00 | 7 | ok | yes | **pass** (≥ 0.9) |
| table-locate | 1.00 | 0.83 | 4 | ok | no | non-target drift (single sample; see below) |
| row-delete | 0.63 | 1.00 | 4 | ok | yes | **pass** |
| cell-edit | 0.73 | 1.00 | 7 | ok | yes | **pass** |
| table-relayout | 0.75 | 1.00 | 7 | ok | yes | **pass** |
| table-insert | 0.89 | 1.00 | 4 | ok | yes | **pass** |
| image-replace | 0.71 | 1.00 | 3 | low-n | yes | **pass** (low-n) |
| image-insert | 0.86 | 1.00 | 3 | low-n | yes | **pass** (low-n) |
| fonts | 1.00 | 1.00 | 3 | low-n | no | unchanged |
| verify-safety | 1.00 | 0.94 | 7 | ok | no | non-target drift (single sample) |

`subscore = Σ(normalized×weight) / Σ(weight)` over that tag's questions.
Exact fractions: pdf-model 16/16, table-locate 7.5/9, row-delete 10/10,
cell-edit 16/16, table-relayout 16/16, table-insert 9/9, image-replace 7/7,
image-insert 7/7, fonts 7/7, verify-safety 16/17.

## Stack score

```
stack_score = Σ(normalized×weight) / Σ(weight) = 83.5 / 86 = 97.1%   (baseline 73.25 / 86 = 85.2%)
```

## Validation verdict

All seven target tags reached the 0.9 threshold (each at 1.00). Every rubric
point lost in the baseline on a target tag was earned this run; no target-tag
point was missed. The derived skill is validated as written and is **not**
modified by this run.

Non-target movement, read per HOW-TO-EVALUATE.md ("only trust TARGET-tag
movement"):

- **table-locate 1.00 → 0.83** on one question (pdf-locate-01). The false
  belief "PyMuPDF has no built-in table detector" was present in the baseline
  too; it was masked there by a pdfplumber `find_tables()` mention that this
  run omitted. It is a consistent belief rather than sampling noise, but it is
  outside the skill's targets and cannot be bolted onto this skill. Candidate
  for a fresh baseline eval of this model; if it reproduces, it earns its own
  `table-locate` section (`page.find_tables()` → `.rows[i].cells` geometry).
- **verify-safety 1.00 → 0.94** on one question (pdf-verify-03): other pages /
  signature / encryption / metadata awareness dropped out. Single-sample
  drift; left alone.

Other factual slips outside the targets (no score impact): `insert_image`
placement still anchored to `page.width/height` instead of real content edges
(pdf-imgins-01); glyph-coverage check still absent (pdf-fonts-01).

## Derivation targets

None. This is a validation scorecard; the derived skill
`derived/pdf.nvidia__nemotron-3.5-super-vl.SKILL.md` stands as derived from the
baseline.
