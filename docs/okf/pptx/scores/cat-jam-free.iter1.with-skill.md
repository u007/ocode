---
model_id: cat-jam-free
model_version: "alpha"
evaluated_via: opencode-go
evaluated_on: 2026-10-14
stack: pptx
stack_corpus_rev: 1
threshold: 0.9
validation: with-skill
---

# Scorecard — cat-jam-free on pptx (with-skill validation, threshold 0.9)

> Valid ONLY for `cat-jam-free` @ `alpha`. A version bump invalidates
> this scorecard — re-benchmark.

> **WITH-SKILL VALIDATION RUN.** Answers: `pptx/answers/cat-jam-free.with-skill.md`,
> produced closed-book with the derived skill `derived/pptx.cat-jam-free.SKILL.md`
> (`pptx-tuning-cat-jam-free`) prepended to the `_prompts/pptx.md` sheet as active
> guidance. Baseline: `scores/cat-jam-free.md`. No new derived skill is produced
> from this run.

> Target tags for this skill (baseline < 0.9): **pptx-model, table-locate (low-n),
> row-delete, cell-edit, table-relayout, table-insert (low-n), image-replace,
> legacy-ppt (low-n), verify-safety**. Only image-insert (1.00) is non-target.

## Provenance

Answers: `pptx/answers/cat-jam-free.with-skill.md`, produced by
`ocode run -m opencode-go/cat-jam-free -effort med` with the SKILL.md body (front
matter stripped) prepended to the `_prompts/pptx.md` sheet; isolated HOME with every
tool denied, cwd holding only the sheet; session audited in sqlite: 1 user + 1
assistant message, 0 tool calls. No answer key. The model appended a recap block
after the YAML fence; the harvester excluded it, and it is not graded. Graded against
`pptx/questions.yaml` corpus_rev 1 (python-pptx facts verified on 1.0.2).

Grading rules applied (same as the baseline): a `point` whose core concept is
present earns full credit (minor missing sub-items are noted); a `point` only half
present earns 0.5; a `point` stated alongside a materially wrong claim about the same
construct earns 0 and is marked "(judgment)"; tangential wrong asides go in the notes
only. Equivalent correct techniques earn the point. For `pptx-locate-01`, picking the
table by header text AND asserting a unique row are both required for point 2. For
legacy-ppt, delivering the requested format (convert back to `.ppt`) is required for
the second point of both questions.

## History

- Earlier with-skill scorecard: none (first with-skill validation for cat-jam-free
  on pptx).

## Per-question results

| id | tags | weight | full | awarded | normalized | notes |
|----|------|-------:|-----:|--------:|-----------:|-------|
| pptx-model-01 | pptx-model | 3 | 2 | 2 | 1.00 | graphicFrame → graphicData → a:tbl; tblGrid/gridCol, a:tr, a:tc txBody; xfrm, gridCol `w` / tr `h` not named (minor, as baseline) |
| pptx-model-02 | pptx-model | 2 | 2 | 2 | 1.00 | EMU 914400/in, 12700/pt; 16:9 = 12192000 × 6858000; "read prs.slide_width/prs.slide_height instead of assuming either size" → baseline gap closed |
| pptx-model-03 | pptx-model, image-replace | 2 | 2 | 2 | 1.00 | master/layout inheritance; edit there changes every slide; per-slide exceptions need own shapes or layout variants |
| pptx-locate-01 | table-locate | 3 | 2 | 2 | 1.00 | GroupShape recursion + has_table; table chosen by stripped header text; all hits collected, `assert len(hits) == 1` → baseline point 2 now met |
| pptx-locate-02 | table-locate, pptx-model | 2 | 2 | 2 | 1.00 | origin gridSpan/rowSpan, covered cells present with hMerge/vMerge, one a:tc per gridCol; write to origin; adjust spans and covered cells together + per-row tc-count assert. Baseline "covered cells may be omitted" misconception gone |
| pptx-rowdel-01 | row-delete | 3 | 2 | 1 | 0.50 | no rows.remove API, `table._tbl.remove(row._tr)` (point 1). Point 2 missed: neither frame height nor totals are named (only merge fixing); baseline named the frame height |
| pptx-rowdel-02 | row-delete, table-relayout | 3 | 2 | 2 | 1.00 | stale frame height matters; `gf.height = sum(row.height …)`; plus overlap check below |
| pptx-rowdel-03 | row-delete, cell-edit | 2 | 2 | 1 | 0.50 | explicit tcPr/solidFill vs style bandRow explained (point 1). Point 2 missed again: ends "it depends on explicit fills versus style-driven banding" — no re-apply fills / switch to style banding, despite skill rule 5 |
| pptx-rowdel-04 | row-delete, verify-safety | 2 | 2 | 1.5 | 0.75 | text stays in the XML, searchable/exportable (point 1; rows growing to fit not named, minor). Point 2 half, same as baseline: "a real delete removes the a:tr and its text", no check that the old text is absent from every slide part |
| pptx-cell-01 | cell-edit | 3 | 2 | 2 | 1.00 | cell.text discards run formatting; edit first run's text, remove extra runs |
| pptx-cell-02 | cell-edit, table-relayout | 2 | 2 | 2 | 1.00 | text wraps and row grows; widths/font size; verify frame height and overlaps. Asides "overflows/clips if wrapping is disabled" and "autofit/table layout" are hedged/not pptx table concepts (tangential, as baseline) |
| pptx-cell-03 | cell-edit | 2 | 2 | 1.5 | 0.75 | point 1 met (totals; the row's own Amount not named explicitly, minor). Point 2 half (judgment): only in-table dependents plus an ambiguous "notes"; no summary slide, chart data, or keep-number-format — baseline met this point |
| pptx-relayout-01 | table-relayout | 3 | 3 | 2.5 | 0.83 | deepcopy + insert before Total (point 1). Point 2 half: text through first run and gf.height updated, but the Total is never updated. Point 3 met: vertical fit and overlap with shapes below (banding not named, minor) |
| pptx-relayout-02 | table-relayout | 3 | 3 | 3 | 1.00 | gridCol + a:tc in every a:tr including merge origins/covered cells; copy style/formatting; redistribute widths to fit the slide, ask if no room; frame width update not named (minor, as baseline) |
| pptx-tblins-01 | table-insert | 3 | 2 | 2 | 1.00 | free box from shape bboxes + slide size with margins; add_table; bounds and overlap verify |
| pptx-tblins-02 | table-insert | 2 | 2 | 2 | 1.00 | copy a:tblPr incl. tableStyleId + firstRow/bandRow, widths/heights, tcPr fills, run props; default Medium Style 2 – Accent 1 → baseline element-name error gone |
| pptx-tblins-03 | table-insert, pptx-model | 1 | 2 | 2 | 1.00 | only TablePlaceholder has insert_table; generic placeholder → add_table at ph bbox, then remove ph._element. Baseline misconception gone |
| pptx-imgrep-01 | image-replace | 3 | 3 | 3 | 1.00 | old geometry with one dimension (aspect kept); new p:pic at old index, old removed; old rel dropped only if no other `//@r:embed or //@r:link` → point 3 now met |
| pptx-imgrep-02 | image-replace | 2 | 2 | 2 | 1.00 | shared image part; give the replacement its own part. SHA1 dedupe not named (minor, as baseline) |
| pptx-imgrep-03 | image-replace, verify-safety | 2 | 2 | 2 | 1.00 | drop_rel counts only `//@r:id`, a:blip r:embed missed → count 0, dropped; own `//@r:embed \| //@r:link` check. Baseline "doesn't check" mechanism gone |
| pptx-imgins-01 | image-insert | 3 | 2 | 2 | 1.00 | width=Inches(2) with derived height; bottom-right from slide edges − margin; overlap/bounds check. "both = stretch" not stated (minor) |
| pptx-imgins-02 | image-insert | 2 | 2 | 2 | 1.00 | placeholder imposes geometry and crop; add_picture with fitted size or adjust crop |
| pptx-legacy-01 | legacy-ppt | 3 | 2 | 2 | 1.00 | python-pptx opens only OOXML (OLE2 not named, minor); `soffice --headless --convert-to pptx`, inspect, edit, convert back and deliver .ppt, keep original → baseline halving lifted |
| pptx-legacy-02 | legacy-ppt, verify-safety | 2 | 2 | 2 | 1.00 | compare tables/images/fonts/layout before and after (render not named, minor); tell user it may be lossy, keep original, deliver .ppt |
| pptx-verify-01 | verify-safety | 3 | 2 | 2 | 1.00 | new output file; reopen, assert exact cells/order/totals/geometry, compare every other slide with the original, search all parts for removed text, hash media → baseline point 2 gap closed |
| pptx-verify-02 | verify-safety, pptx-model | 2 | 2 | 2 | 1.00 | tc vs gridCol, bad spans/missing covered cells, missing children, element order, broken rels, duplicate IDs; "python-pptx does not validate" not named (minor, as baseline) |
| pptx-verify-03 | verify-safety, image-replace | 2 | 2 | 2 | 1.00 | rel + media survive p:pic delete; search for remaining r:embed/r:link users, remove rel only when unused; zip media listing (no hash compare, minor, as baseline) |

`normalized = min(awarded, full) / full`

## Per-tag subscores

| tag | subscore | n | trust | baseline | Δ | target? |
|-----|---------:|--:|-------|---------:|---:|---------|
| pptx-model | 1.00 | 6 | ok | 0.73 | +0.27 | **yes** |
| table-locate | 1.00 | 2 | low-n | 0.30 | +0.70 | **yes** (low-n) |
| row-delete | 0.70 | 4 | ok | 0.85 | −0.15 | **yes** |
| cell-edit | 0.83 | 4 | ok | 0.89 | −0.06 | **yes** |
| table-relayout | 0.95 | 4 | ok | 0.86 | +0.09 | **yes** |
| table-insert | 1.00 | 3 | low-n | 0.79 | +0.21 | **yes** (low-n) |
| image-replace | 1.00 | 5 | ok | 0.82 | +0.18 | **yes** |
| image-insert | 1.00 | 2 | low-n | 1.00 | 0.00 | no |
| legacy-ppt | 1.00 | 2 | low-n | 0.75 | +0.25 | **yes** (low-n) |
| verify-safety | 0.96 | 6 | ok | 0.79 | +0.17 | **yes** |

`subscore = Σ(normalized×weight) / Σ(weight)` over that tag's questions.

- pptx-model: (3+2+2+2+1+2) / 12 = 12 / 12 = 1.00
- table-locate: (3+2) / 5 = 1.00
- row-delete: (1.5+3+1+1.5) / 10 = 7 / 10 = 0.70
- cell-edit: (1+3+2+1.5) / 9 = 7.5 / 9 = 0.833
- table-relayout: (3+2+2.5+3) / 11 = 10.5 / 11 = 0.955
- table-insert: (3+2+1) / 6 = 1.00
- image-replace: (2+3+2+2+2) / 11 = 11 / 11 = 1.00
- image-insert: (3+2) / 5 = 1.00
- legacy-ppt: (3+2) / 5 = 1.00
- verify-safety: (1.5+2+2+3+2+2) / 13 = 12.5 / 13 = 0.962

## Stack score

```
stack_score = Σ(normalized×weight) / Σ(weight) = 61.0 / 65 = 93.8%
```

Baseline 52.75 / 65 = 81.2%. (Lost weight: rowdel-01 1.5, rowdel-03 1.0,
rowdel-04 0.5, cell-03 0.5, relayout-01 0.5 = 4.0.)

## Target-tag verdict

| tag | baseline | with-skill | n | pass (≥ 0.9)? |
|-----|---------:|-----------:|--:|---------------|
| pptx-model | 0.73 | 1.00 | 6 | **PASS** |
| table-locate | 0.30 | 1.00 | 2 (low-n) | **PASS** |
| row-delete | 0.85 | 0.70 | 4 | **FAIL** |
| cell-edit | 0.89 | 0.83 | 4 | **FAIL** |
| table-relayout | 0.86 | 0.95 | 4 | **PASS** |
| table-insert | 0.79 | 1.00 | 3 (low-n) | **PASS** |
| image-replace | 0.82 | 1.00 | 5 | **PASS** |
| legacy-ppt | 0.75 | 1.00 | 2 (low-n) | **PASS** |
| verify-safety | 0.79 | 0.96 | 6 | **PASS** |

**Verdict: FAIL.** Seven of nine target tags reach 0.9, and every concept-level
misconception the skill cites is gone (optional covered merge cells, `insert_table`
on a generic placeholder, `drop_rel` "doesn't check", assumed slide size,
`.pptx` handed back for a `.ppt`). The two failing tags fail on the same
follow-through habit the baseline flagged — the diagnosis is right, the finishing
step is omitted:

- **row-delete 0.70** — rowdel-03: explains explicit fills vs bandRow but again gives
  no fix (skill rule 5 not absorbed); rowdel-01: drops the "fix frame height and
  totals" step the baseline had; rowdel-04: "real delete removes the text" without
  the verify-absence-in-every-slide-part check (skill rule 5, second half).
- **cell-edit 0.83** — rowdel-03 (shared, above); cell-03: lists only in-table
  dependents, no summary slide / chart data / keep number format.

Iterate suggestion (for whoever tweaks the skill; not done here): rule 5 is phrased
as a fix list after the diagnosis and the model still stops at the diagnosis. A
sharper directive would cite the exact failure ("when asked *why* rows share a colour,
the answer must end with the fix: re-assign fills to every following row") and add a
row-delete checklist (remove a:tr → frame height → totals → verify absence). cell-03
(other occurrences of a changed number) has no rule in the skill at all.

Non-target drift: image-insert held at 1.00. relayout-01 lost half a point for not
updating the Total although the tag still passes.

## Anomalies

- All 27 ids answered; answers are short (one paragraph each), as in the baseline.
- The model appended a recap block after the YAML fence; the harvester excluded it,
  so it is neither in the answers file nor graded.
- No verbatim overlap with the reference answers. Overlap with the skill's phrasing
  (`len(tr.tc_lst) == len(tbl.tblGrid.gridCol_lst)`, `//@r:embed | //@r:link`,
  `ph._element`, Medium Style 2 – Accent 1) is the expected absorption of the injected
  guidance, not contamination.
- The row-delete regression (0.85 → 0.70) is partly single-sample variance
  (rowdel-01 lost a step the baseline included), but rowdel-03 and rowdel-04 repeat
  the baseline's exact omissions with the skill present, which is the real signal.
