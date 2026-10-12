---
model_id: step-5-preview-free
model_version: "5-preview"
evaluated_via: opencode-go
evaluated_on: 2026-10-12
stack: pptx
stack_corpus_rev: 1
threshold: 0.9
validation: with-skill
---

# Scorecard — step-5-preview-free on pptx (with-skill validation, threshold 0.9)

> Valid ONLY for `step-5-preview-free` @ `5-preview`. A version bump invalidates
> this scorecard — re-benchmark.

> **WITH-SKILL VALIDATION RUN.** Answers: `pptx/answers/step-5-preview-free.with-skill.md`,
> produced closed-book with the current derived skill
> `derived/pptx.step-5-preview-free.SKILL.md` (`pptx-tuning-step-5-preview-free`)
> prepended to the `_prompts/pptx.md` sheet as active guidance (session audited:
> 1 user + 1 assistant message, 0 tool calls). Baseline:
> `scores/step-5-preview-free.md`. No new derived skill is produced from this run.

> Target tags for this skill (baseline < 0.9): **pptx-model, table-locate (low-n),
> table-relayout, image-replace**.

## Provenance

Answers: `pptx/answers/step-5-preview-free.with-skill.md`, closed-book,
`ocode run -m opencode-go/step-5-preview-free` in an empty scratch directory with
the derived skill body prepended to the `_prompts/pptx.md` sheet; no answer key.
Graded against `pptx/questions.yaml` corpus_rev 1 (python-pptx facts verified on
1.0.2).

Harvest note: the first publication of the answers file cut each record at the
first indented ```` ``` ```` line followed by a blank line, truncating the four
answers that contain a fenced code block (`pptx-rowdel-01`, `pptx-relayout-01`,
`pptx-tblins-01`, `pptx-imgins-01`) after the code. The harvester was fixed and the
file re-published with all 27 records complete before grading closed; this
scorecard grades the complete repo file. On the truncated copy the stack would have
read 60.0 / 65 = 92.3% and table-relayout 9.0 / 11 = 0.82 (FAIL), measuring the
harvester rather than the model.

Grading rules applied (same as the baseline): a `point` whose core concept is
present earns full credit (minor missing sub-items are noted); a `point` stated
correctly but accompanied by a materially wrong claim about the same construct
earns 0 and is marked "(judgment)"; tangential wrong asides go in the notes only.
Points are whole; fractions come only from a listed partial. Equivalent correct
techniques earn the point. For `pptx-locate-01`, picking the table by header text
AND asserting a unique row are both required for the second point.

## History

- Earlier with-skill scorecard: none (first with-skill validation for
  step-5-preview-free on pptx).

## Per-question results

| id | tags | weight | full | awarded | normalized | notes |
|----|------|-------:|-----:|--------:|-----------:|-------|
| pptx-model-01 | pptx-model | 3 | 2 | 2 | 1.00 | graphicFrame/xfrm → graphicData → a:tbl; tblGrid/gridCol w, tr h, tc txBody + tcPr; now states one a:tc per gridCol with hMerge/vMerge covered cells |
| pptx-model-02 | pptx-model | 2 | 2 | 2 | 1.00 | EMU 914400/in, 12700/pt; 16:9 = 12192000 × 6858000; "never hard-code; read prs.slide_width/height"; aside that a float EMU trips repair is correct |
| pptx-model-03 | pptx-model, image-replace | 2 | 2 | 2 | 1.00 | layout/master via slide.slide_layout.shapes; edit there changes every inheriting slide; per-slide override on the slide's spTree |
| pptx-locate-01 | table-locate | 3 | 2 | 2 | 1.00 | has_table with GroupShape recursion; table picked by header-row text with a unique-table assert; row by exact stripped key-cell text, all hits collected, `len(hits) == 1` ("never break on the first hit"); covered merge cells empty. Baseline's four gaps all closed |
| pptx-locate-02 | table-locate, pptx-model | 2 | 2 | 2 | 1.00 | one a:tc per gridCol stated as the invariant ("a span does not reduce the cell count"); origin gridSpan/rowSpan, covered hMerge/vMerge, is_merge_origin/is_spanned/span_width/span_height; write to origin; column insert through a merge = bump gridSpan AND insert a covered cell. Baseline "coverage after accounting for spans" misconception gone |
| pptx-rowdel-01 | row-delete | 3 | 2 | 2 | 1.00 | no rows.remove/delete API, `tbl.remove(tr)`; then fix the graphicFrame height and row shading; totals not named (minor, as baseline) |
| pptx-rowdel-02 | row-delete, table-relayout | 3 | 2 | 2 | 1.00 | cy not recomputed on XML removal; `gf.height = sum(row.height …)`; also checks the new bottom vs slide_height |
| pptx-rowdel-03 | row-delete, cell-edit | 2 | 2 | 2 | 1.00 | bandRow + tableStyleId recomputes vs explicit tcPr/solidFill; recompute fills of every surviving body row |
| pptx-rowdel-04 | row-delete, verify-safety | 2 | 2 | 2 | 1.00 | text still extractable; remove the a:tr + frame height + shading; "rows grow to fit text" and "verify absence in every slide part" not stated (minor, as baseline). Aside: one garbled token ("conthNormalPaste") |
| pptx-cell-01 | cell-edit | 3 | 2 | 2 | 1.00 | cell.text replaces the txBody with a default run; set the first run's text / deepcopy the run |
| pptx-cell-02 | cell-edit, table-relayout | 2 | 2 | 2 | 1.00 | row grows taller to fit wrapped text (h is a minimum); rebalance gridCol widths keeping Σ = frame cx. Checks left + width vs slide_width rather than the new bottom vs slide height / shapes below (minor); "if the row height is fixed, text can be clipped" is hedged and wrong for PowerPoint tables; `a:bodyPr` normAutofit on a cell offered again (tangential, as baseline) |
| pptx-cell-03 | cell-edit | 2 | 2 | 2 | 1.00 | row amount + subtotal/Total recomputed and asserted; summary slides, other tables, footnotes, alt text named; chart data, speaker notes and keep-format not named (minor, as baseline) |
| pptx-relayout-01 | table-relayout | 3 | 3 | 3 | 1.00 | deepcopy body a:tr + `total_tr.addprevious`; text via existing runs' a:t keeping a:rPr; Total recomputed and asserted; `gf.height = sum(row.height …)`; `gf.top + gf.height <= prs.slide_height` and no overlap with shapes below → point 3 now met. Banding not mentioned (minor) |
| pptx-relayout-02 | table-relayout | 3 | 3 | 3 | 1.00 | gridCol + one a:tc in every a:tr at the index; through a merge do both (bump gridSpan AND insert an hMerge copy), assert `len(tr.tc_lst) == len(gridCol_lst)` per row; deepcopy neighbour cells for formatting; take the width from Item so Σ gridCol = frame cx, else adjust cx/off within the margins. Baseline (judgment) withhold no longer applies |
| pptx-tblins-01 | table-insert | 3 | 2 | 2 | 1.00 | shape bboxes → free band within margins and slide bounds; `add_table(...)` → `gf.table`; overlap verify against every shape bbox |
| pptx-tblins-02 | table-insert | 2 | 2 | 2 | 1.00 | default style from add_table; copy the existing table's a:tblPr (tableStyleId GUID + firstRow/bandRow flags), match widths/heights/position. Medium Style 2 / default GUID not named this time (minor); explicit fills/run props not named (minor); aside "make sure the GUID exists in tableStyles.xml" (tangential, as baseline) |
| pptx-tblins-03 | table-insert, pptx-model | 1 | 2 | 2 | 1.00 | `TablePlaceholder.insert_table(rows, cols)` → PlaceholderGraphicFrame in the placeholder geometry; SlidePlaceholder lacks it → add_table at the ph bbox and remove the ph element. Baseline "no insert_table on placeholders" gone |
| pptx-imgrep-01 | image-replace | 3 | 3 | 3 | 1.00 | add_picture with the old geometry; z-order restored by `spTree.insert(old_idx, …)`; old p:pic removed; old image rel dropped only after an own `//@r:embed | //@r:link` scan → point 3 now met. Aspect-ratio caveat for a differently shaped image not given (minor) |
| pptx-imgrep-02 | image-replace | 2 | 2 | 2 | 1.00 | shared/deduplicated image part referenced from both slides; give slide 1 its own new part and repoint only its r:embed |
| pptx-imgrep-03 | image-replace, verify-safety | 2 | 2 | 2 | 1.00 | `_rel_ref_count` matches `//@r:id` only, a:blip r:embed counts 0 → image rels always dropped; own r:embed/r:link check before the call. Baseline "no reference counting / cloned pic" mechanism gone |
| pptx-imgins-01 | image-insert | 3 | 2 | 2 | 1.00 | aspect kept by computing h from the native pixel ratio (equivalent to width-only add_picture; the width-only shortcut itself is not stated, minor); left/top from slide edges minus margins; bounds + no-intersection check against every shape bbox, "reposition rather than cover content" |
| pptx-imgins-02 | image-insert | 2 | 2 | 2 | 1.00 | insert_picture crops via a:srcRect to the placeholder aspect; resize to the photo's ratio, or add_picture + remove the placeholder, or reset the srcRect crop |
| pptx-legacy-01 | legacy-ppt | 3 | 2 | 2 | 1.00 | binary .ppt not openable; `soffice --headless --convert-to pptx`, edit, keep the original, warn lossy; "convert back to .ppt if required" not stated (minor, as baseline) |
| pptx-legacy-02 | legacy-ppt, verify-safety | 2 | 2 | 2 | 1.00 | counts/tables/images + render before and after; tell the user it was converted, keep the original; "return the requested format" not stated (minor, as baseline) |
| pptx-verify-01 | verify-safety | 3 | 2 | 2 | 1.00 | back up / save to a new path; reopen and assert content, totals, tc/gridCol invariant, bounds; render smoke test; "unchanged remainder" compare not explicit (minor) |
| pptx-verify-02 | verify-safety, pptx-model | 2 | 2 | 2 | 1.00 | tc count ≠ gridCol (incl. gridSpan bumped without the hMerge cell), broken spans, hMerge cell with text; dangling r:embed / drop_rel'd rId, schema order, invalid EMU values, namespace breakage. tc-without-txBody, duplicate cNvPr ids and "python-pptx doesn't validate" not named (minor, as baseline). Baseline "after accounting for spans" phrasing gone |
| pptx-verify-03 | verify-safety, image-replace | 2 | 2 | 2 | 1.00 | rel + ppt/media survive a p:pic delete; check other `//@r:embed | //@r:link` users, drop_rel, re-save, list and hash ppt/media. Hash compare now present |

`normalized = min(awarded, full) / full`

## Per-tag subscores

| tag | subscore | n | trust | baseline | Δ | target? |
|-----|---------:|--:|-------|---------:|---:|---------|
| pptx-model | 1.00 | 6 | ok | 0.88 | +0.12 | **yes** |
| table-locate | 1.00 | 2 | low-n | 0.50 | +0.50 | **yes** (low-n) |
| row-delete | 1.00 | 4 | ok | 1.00 | 0.00 | no |
| cell-edit | 1.00 | 4 | ok | 1.00 | 0.00 | no |
| table-relayout | 1.00 | 4 | ok | 0.82 | +0.18 | **yes** |
| table-insert | 1.00 | 3 | low-n | 0.92 | +0.08 | no |
| image-replace | 1.00 | 5 | ok | 0.82 | +0.18 | **yes** |
| image-insert | 1.00 | 2 | low-n | 1.00 | 0.00 | no |
| legacy-ppt | 1.00 | 2 | low-n | 1.00 | 0.00 | no |
| verify-safety | 1.00 | 6 | ok | 0.92 | +0.08 | no |

`subscore = Σ(normalized×weight) / Σ(weight)` over that tag's questions.

Arithmetic: pptx-model (3+2+2+2+1+2)/12 = 12/12; table-locate (3+2)/5 = 5/5;
row-delete (3+3+2+2)/10 = 10/10; cell-edit (2+3+2+2)/9 = 9/9; table-relayout
(3+2+3+3)/11 = 11/11; table-insert (3+2+1)/6 = 6/6; image-replace (2+3+2+2+2)/11 =
11/11; image-insert (3+2)/5 = 5/5; legacy-ppt (3+2)/5 = 5/5; verify-safety
(2+2+2+3+2+2)/13 = 13/13.

## Stack score

```
stack_score = Σ(normalized×weight) / Σ(weight) = 65.0 / 65 = 100.0%
```

(No lost weight.) Baseline 58.0 / 65 = 89.2%.

## Target-tag verdict

| tag | baseline | with-skill | n | pass (≥ 0.9)? |
|-----|---------:|-----------:|--:|---------------|
| pptx-model | 0.88 | 1.00 | 6 | **PASS** |
| table-locate | 0.50 | 1.00 | 2 (low-n) | **PASS** |
| table-relayout | 0.82 | 1.00 | 4 | **PASS** |
| image-replace | 0.82 | 1.00 | 5 | **PASS** |

Every target tag reaches 0.9. The skill is validated for step-5-preview-free @
5-preview. Each baseline misconception the skill cites is absent from the with-skill
answers: first-`has_table`/`shape.name` table selection and `break` on first hit
(locate-01), span-weighted "coverage" tc count (locate-02, relayout-02, verify-02),
"no `insert_table` on placeholders" (tblins-03), missing bottom-vs-slide check
(relayout-01), old image rel never dropped (imgrep-01), "`drop_rel` has no reference
counting" (imgrep-03). No target tag remains below threshold, so there is no remaining
wrong or missing target claim to list.

Non-target drift: none downward — every non-target tag held at 1.00 or rose
(table-insert 0.92 → 1.00, verify-safety 0.92 → 1.00); per HOW-TO-EVALUATE.md these
single-sample moves are not acted on.

## Anomalies

- The first publication of the answers file truncated four answers after their
  fenced code block (see Provenance); re-published complete before grading closed.
  Not a model defect.
- No verbatim overlap with the reference answers; wording and code shapes are
  independent of the key. Overlap with the derived skill's phrasing (one-tc-per-gridCol
  invariant, `//@r:embed | //@r:link`, `len(tr.tc_lst) == len(gridCol_lst)`) is the
  expected absorption of the injected guidance, not contamination.
- All 27 ids answered; no self-summary this time. One garbled token in rowdel-04.
