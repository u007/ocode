---
model_id: cat-jam-free
model_version: "alpha"
evaluated_via: opencode-go
evaluated_on: 2026-10-14
stack: pptx
stack_corpus_rev: 1
threshold: 0.9
validation: with-skill
iteration: 2
---

# Scorecard — cat-jam-free on pptx (with-skill validation, ITERATION 2, threshold 0.9)

> Valid ONLY for `cat-jam-free` @ `alpha`. A version bump invalidates
> this scorecard — re-benchmark.

> **WITH-SKILL VALIDATION RUN — ITERATION 2.** Answers:
> `pptx/answers/cat-jam-free.with-skill.md`, produced closed-book with the
> sharpened derived skill `derived/pptx.cat-jam-free.SKILL.md`
> (`pptx-tuning-cat-jam-free`) prepended to the `_prompts/pptx.md` sheet as active
> guidance. The skill was sharpened after iteration 1
> (`scores/cat-jam-free.iter1.with-skill.md`, FAIL on row-delete 0.70 and cell-edit
> 0.83): rule 5 became an explicit row-delete checklist ("say this fix, never stop at
> explaining the cause"; frame height + totals; verify absence) and rule 5b
> (dependent figures: summary slides, chart data, notes, same number format) was
> added. Baseline: `scores/cat-jam-free.md`. No new derived skill is produced from
> this run.

> Target tags for this skill (baseline < 0.9): **pptx-model, table-locate (low-n),
> row-delete, cell-edit, table-relayout, table-insert (low-n), image-replace,
> legacy-ppt (low-n), verify-safety**. Only image-insert (1.00) is non-target.

## Provenance

Answers: `pptx/answers/cat-jam-free.with-skill.md`, produced by
`ocode run -m opencode-go/cat-jam-free -effort med` with the SKILL.md body (front
matter stripped) prepended to the `_prompts/pptx.md` sheet; isolated HOME with every
tool denied, cwd holding only the sheet; session audited: 1 user + 1 assistant
message, 0 tool calls. No answer key. Graded against `pptx/questions.yaml`
corpus_rev 1 (python-pptx facts verified on 1.0.2).

Grading rules applied (same as the baseline and iteration 1): a `point` whose core
concept is present earns full credit (minor missing sub-items are noted); a `point`
only half present earns 0.5; a `point` stated alongside a materially wrong claim
about the same construct earns 0 and is marked "(judgment)"; tangential wrong asides
go in the notes only. Equivalent correct techniques earn the point. For
`pptx-locate-01`, picking the table by header text AND asserting a unique row are
both required for point 2. For legacy-ppt, delivering the requested format (convert
back to `.ppt`) is required for the second point of both questions.

## History

- Iteration 1: `scores/cat-jam-free.iter1.with-skill.md` — 61.0 / 65 = 93.8%,
  **FAIL** (row-delete 0.70, cell-edit 0.83; other seven targets ≥ 0.9).
- Iteration 2 (this file): sharpened skill (rule 5 checklist, new rule 5b).

## Per-question results

| id | tags | weight | full | awarded | normalized | notes |
|----|------|-------:|-----:|--------:|-----------:|-------|
| pptx-model-01 | pptx-model | 3 | 2 | 2 | 1.00 | graphicFrame → graphicData → a:tbl, p:xfrm; tblGrid/gridCol (widths), a:tr, one a:tc per gridCol, a:txBody; tcPr and the `w`/`h` attribute names not named (minor) |
| pptx-model-02 | pptx-model | 2 | 2 | 2 | 1.00 | EMU 914400/in (gives 360000/cm instead of 12700/pt, minor as baseline); 12192000 × 6858000; "always read prs.slide_width / prs.slide_height rather than assuming" |
| pptx-model-03 | pptx-model, image-replace | 2 | 2 | 2 | 1.00 | master/layout shapes; editing there is global; per-slide override / tell-the-user not named (minor, as baseline) |
| pptx-locate-01 | table-locate | 3 | 2 | 2 | 1.00 | GroupShape recursion + has_table; table chosen by stripped first-row header text; all exact hits collected, assert exactly one |
| pptx-locate-02 | table-locate, pptx-model | 2 | 2 | 2 | 1.00 | origin gridSpan/rowSpan, covered cells present with hMerge/vMerge, one a:tc per gridCol; write to origin; adjust spans and covered cells together |
| pptx-rowdel-01 | row-delete | 3 | 2 | 2 | 1.00 | no public rows.remove(); remove the a:tr via getparent().remove; "then rebuild graphic-frame height" → point 2 met (totals not named, minor, as baseline). Iteration-1 miss closed |
| pptx-rowdel-02 | row-delete, table-relayout | 3 | 2 | 2 | 1.00 | stale height matters (blank space/overlap); `gf.height = sum(row.height …)`; then slide bounds and shapes below. "Not auto-updated on XML removal" implicit (minor) |
| pptx-rowdel-03 | row-delete, cell-edit | 2 | 2 | 2 | 1.00 | explicit a:tcPr fills vs style bandRow; "Fix by re-applying alternating fills to rows after the deleted one, or clearing explicit fills and relying on bandRow" → point 2 met for the first time (missed in baseline and iteration 1) |
| pptx-rowdel-04 | row-delete, verify-safety | 2 | 2 | 1.5 | 0.75 | point 1: data stays in the XML, searchable/extractable (rows grow to fit not named, minor). Point 2 half (judgment, same as baseline and iteration 1): "cosmetic masking, not a data edit" implies a real delete, but neither "remove the a:tr" nor the verify-absence-in-every-slide-part check is stated — skill rule 5(d) still not absorbed on this question (third run in a row) |
| pptx-cell-01 | cell-edit | 3 | 2 | 2 | 1.00 | cell.text loses run formatting; set paragraphs[0].runs[0].text, handle extra runs |
| pptx-cell-02 | cell-edit, table-relayout | 2 | 2 | 2 | 1.00 | wraps and auto-grows the row; widths/font/row height; "ensure the frame still fits the slide" (neighbour shapes not named, minor). Asides "clip if heights are fixed" and "table autofit behavior" are not pptx table concepts (tangential, as baseline and iteration 1) |
| pptx-cell-03 | cell-edit | 2 | 2 | 2 | 1.00 | in-table totals/subtotals (row Amount not named explicitly, minor); summary/agenda slides, chart data (embedded workbook + XML cache), speaker notes, same number format → iteration-1 half lifted (rule 5b absorbed) |
| pptx-relayout-01 | table-relayout | 3 | 3 | 3 | 1.00 | deepcopy an a:tr, insert before Total; fill through existing first run, remove extra runs; totals + gf.height; `gf.top + gf.height` vs slide height and shapes below. Banding not named (minor) |
| pptx-relayout-02 | table-relayout | 3 | 3 | 2 | 0.67 | point 1: a:gridCol + one a:tc in every row, merges handled (index "after Item" implicit, minor). Point 2 missed: no copy of the neighbouring header/body cell formatting for the new cells (iteration 1 had it). Point 3: redistribute widths, update gf width, verify `gf.left + width <= slide_width` |
| pptx-tblins-01 | table-insert | 3 | 2 | 2 | 1.00 | free space from shape bboxes in EMU; add_table(rows, cols, left, top, width, height); overlap verify. Slide-bounds/margins not explicit (minor) |
| pptx-tblins-02 | table-insert | 2 | 2 | 2 | 1.00 | default Medium Style 2 – Accent 1 with firstRow/bandRow; copy a:tblPr (style id + flags), tcPr fills, run props, widths, heights |
| pptx-tblins-03 | table-insert, pptx-model | 1 | 2 | 2 | 1.00 | TablePlaceholder.insert_table; generic placeholder → add_table at ph bbox, then remove ph._element |
| pptx-imgrep-01 | image-replace | 3 | 3 | 3 | 1.00 | old geometry, fit by one dimension / inside the box; new p:pic placed at the old one's spTree position, old removed; old rel dropped only if no other `//@r:embed`/`@r:link` uses it |
| pptx-imgrep-02 | image-replace | 2 | 2 | 2 | 1.00 | shared image part across pictures/slides; new image part/relationship for the target. SHA1 dedupe not named (minor, as baseline) |
| pptx-imgrep-03 | image-replace, verify-safety | 2 | 2 | 2 | 1.00 | drop_rel counts only `//@r:id`, a:blip r:embed → always dropped; check `//@r:embed \| //@r:link` first |
| pptx-imgins-01 | image-insert | 3 | 2 | 2 | 1.00 | width 2 in, add_picture with width only keeps aspect ("both = stretch" not stated, minor); bottom-right from slide edges − margin; overlap check |
| pptx-imgins-02 | image-insert | 2 | 2 | 2 | 1.00 | insert_picture crops to the placeholder aspect; "adjust dimensions/crop behavior" is vague but covers reset-crop + resize (minor) |
| pptx-legacy-01 | legacy-ppt | 3 | 2 | 2 | 1.00 | cannot open binary .ppt (OLE2 not named, minor); soffice headless → pptx, inspect, edit, convert back to .ppt if needed, keep original |
| pptx-legacy-02 | legacy-ppt, verify-safety | 2 | 2 | 2 | 1.00 | check tables/images/fonts/sizes/text before and after (render not named, minor); tell the user it may be lossy, original kept, deliver requested format |
| pptx-verify-01 | verify-safety | 3 | 2 | 2 | 1.00 | new file, original not overwritten; reopen, assert exact cells/order/totals/geometry, compare every other slide, search for removed text, hash ppt/media (render not named, minor) |
| pptx-verify-02 | verify-safety, pptx-model | 2 | 2 | 1.5 | 0.75 | point 1: tc vs gridCol, missing covered cells, wrong spans. Point 2 half: only "invalid relationship targets" (dangling rIds) of the three; schema order and duplicate cNvPr ids (both present in iteration 1) and "python-pptx does not validate" missing; "malformed XML or namespaces" and "inconsistent table style flags" add nothing |
| pptx-verify-03 | verify-safety, image-replace | 2 | 2 | 2 | 1.00 | p:pic delete leaves rel + ppt/media; check r:embed usage, rels and the zip's ppt/media; drop the unused rel, remove the part. No hash compare (minor, as baseline) |

`normalized = min(awarded, full) / full`

## Per-tag subscores

| tag | subscore | n | trust | baseline | iter 1 | iter 2 | Δ vs baseline | target? |
|-----|---------:|--:|-------|---------:|-------:|-------:|---:|---------|
| pptx-model | 0.96 | 6 | ok | 0.73 | 1.00 | 0.96 | +0.23 | **yes** |
| table-locate | 1.00 | 2 | low-n | 0.30 | 1.00 | 1.00 | +0.70 | **yes** (low-n) |
| row-delete | 0.95 | 4 | ok | 0.85 | 0.70 | 0.95 | +0.10 | **yes** |
| cell-edit | 1.00 | 4 | ok | 0.89 | 0.83 | 1.00 | +0.11 | **yes** |
| table-relayout | 0.91 | 4 | ok | 0.86 | 0.95 | 0.91 | +0.05 | **yes** |
| table-insert | 1.00 | 3 | low-n | 0.79 | 1.00 | 1.00 | +0.21 | **yes** (low-n) |
| image-replace | 1.00 | 5 | ok | 0.82 | 1.00 | 1.00 | +0.18 | **yes** |
| image-insert | 1.00 | 2 | low-n | 1.00 | 1.00 | 1.00 | 0.00 | no |
| legacy-ppt | 1.00 | 2 | low-n | 0.75 | 1.00 | 1.00 | +0.25 | **yes** (low-n) |
| verify-safety | 0.92 | 6 | ok | 0.79 | 0.96 | 0.92 | +0.13 | **yes** |

`subscore = Σ(normalized×weight) / Σ(weight)` over that tag's questions.

- pptx-model: (3+2+2+2+1+1.5) / 12 = 11.5 / 12 = 0.958
- table-locate: (3+2) / 5 = 1.00
- row-delete: (3+3+2+1.5) / 10 = 9.5 / 10 = 0.95
- cell-edit: (2+3+2+2) / 9 = 9 / 9 = 1.00
- table-relayout: (3+2+3+2) / 11 = 10 / 11 = 0.909
- table-insert: (3+2+1) / 6 = 1.00
- image-replace: (2+3+2+2+2) / 11 = 11 / 11 = 1.00
- image-insert: (3+2) / 5 = 1.00
- legacy-ppt: (3+2) / 5 = 1.00
- verify-safety: (1.5+2+2+3+1.5+2) / 13 = 12 / 13 = 0.923

## Stack score

```
stack_score = Σ(normalized×weight) / Σ(weight) = 63.0 / 65 = 96.9%
```

Baseline 52.75 / 65 = 81.2%; iteration 1 61.0 / 65 = 93.8%. (Lost weight:
rowdel-04 0.5, relayout-02 1.0, verify-02 0.5 = 2.0.)

## Target-tag verdict

| tag | baseline | iter 1 | iter 2 | n | pass (≥ 0.9)? |
|-----|---------:|-------:|-------:|--:|---------------|
| pptx-model | 0.73 | 1.00 | 0.96 | 6 | **PASS** |
| table-locate | 0.30 | 1.00 | 1.00 | 2 (low-n) | **PASS** |
| row-delete | 0.85 | 0.70 | 0.95 | 4 | **PASS** |
| cell-edit | 0.89 | 0.83 | 1.00 | 4 | **PASS** |
| table-relayout | 0.86 | 0.95 | 0.91 | 4 | **PASS** |
| table-insert | 0.79 | 1.00 | 1.00 | 3 (low-n) | **PASS** |
| image-replace | 0.82 | 1.00 | 1.00 | 5 | **PASS** |
| legacy-ppt | 0.75 | 1.00 | 1.00 | 2 (low-n) | **PASS** |
| verify-safety | 0.79 | 0.96 | 0.92 | 6 | **PASS** |

**Verdict: PASS (iteration 2).** Every target tag reaches 0.9. The sharpening fixed
both iteration-1 failures: rowdel-03 now ends with the fix (re-apply fills to the
following rows or rely on bandRow), rowdel-01 names the frame-height step again, and
cell-03 lists summary slides, chart data and notes in the same number format
(rule 5b). All concept-level misconceptions from the baseline remain absent.

Margins are thin on two tags, and the verdict is sensitive to one judgment:

- **table-relayout 0.909** — relayout-02 dropped "copy the neighbouring cell's
  formatting" (present in iteration 1). One more lost point on that tag fails it.
- **verify-safety 0.923** — rowdel-04 point 2 is awarded 0.5 for "cosmetic masking,
  not a data edit", graded the same as the baseline's and iteration 1's "a real
  delete removes the data". If that half were withheld (no explicit "remove the
  a:tr", no verify-absence check), verify-safety would read 11.5 / 13 = 0.885 and
  the run would FAIL. Rule 5(d) ("confirm the deleted text is absent from every slide
  XML part") has now been skipped on rowdel-04 in all three runs; it is the one
  skill directive this model has never applied there.
- verify-02 lost half a point it had in iteration 1 (schema order, duplicate ids).

These drops (relayout-02, verify-02) are on questions iteration 1 answered fully,
so they read as single-sample variance on target tags that still pass, not as a
skill defect.

Non-target drift: image-insert held at 1.00.

## Anomalies

- All 27 ids answered; answers remain short (one paragraph each), as in the
  baseline and iteration 1. No recap block noted in the answers file.
- No verbatim overlap with the reference answers. Overlap with the skill's phrasing
  ("re-applying alternating fills … or relying on bandRow", "embedded workbook and
  chart XML cache", `//@r:embed | //@r:link`, `ph._element`, Medium Style 2 – Accent
  1) is the expected absorption of the injected guidance, not contamination.
