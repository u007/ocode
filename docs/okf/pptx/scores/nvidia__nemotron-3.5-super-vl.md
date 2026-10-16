---
model_id: nvidia/nemotron-3.5-super-vl
model_version: "3.5"
evaluated_via: openrouter
evaluated_on: 2026-10-16
stack: pptx
stack_corpus_rev: 1
threshold: 0.9
---

# Scorecard — nvidia/nemotron-3.5-super-vl on pptx

> Valid ONLY for `nvidia/nemotron-3.5-super-vl` @ `3.5`. A version bump invalidates
> this scorecard — re-benchmark.

## Provenance

Closed-book (Rule 0). The answerer was
`ocode run -m openrouter/nvidia/nemotron-3.5-super-vl:free -effort med` under an
isolated HOME whose config denies every tool (no tools were advertised), in a
scratch cwd containing only the `_prompts/pptx.md` sheet, one fresh session; the
session sqlite shows exactly 1 user + 1 assistant message and 0 tool calls. All
27 ids came back once, none empty. Headless runs inject the bundled
skill-catalogue digest into the system prompt; no answer cites a tool or a skill
the question never named (the only externals mentioned are python-pptx, PIL,
LibreOffice, PowerPoint COM/UNO, all named or implied by the questions). Answers
file: `pptx/answers/nvidia__nemotron-3.5-super-vl.md`. Graded against
`pptx/questions.yaml` corpus_rev 1 (python-pptx facts verified on 1.0.2).

Grading rules applied (same as the other pptx scorecards): a `point` whose core
concept is present earns full credit (minor missing sub-items are noted); a
`point` stated correctly but accompanied by a materially wrong claim about the
same construct earns 0 and is marked "(judgment)"; a point whose two halves are
split gets 0.5 when exactly one half is present; tangential wrong asides go in
the notes only. For the table-locate question, picking the table by header AND
asserting a unique row are both required for the second point.

## Per-question results

| id | tags | weight | full | awarded | normalized | notes |
|----|------|-------:|-----:|--------:|-----------:|-------|
| pptx-model-01 | pptx-model | 3 | 2 | 1.5 | 0.75 | (judgment) point 2 half: tblGrid/gridCol w, tr h, tc + tcPr right, but the text container is called `a:textBody` (it is `a:txBody`) and merges are said to be "rowSpan/colSpan attributes on a:tcPr" (they are `gridSpan`/`rowSpan` on `a:tc`); also `a:tblStyleId` for `a:tableStyleId`; xfrm not named (minor) |
| pptx-model-02 | pptx-model | 2 | 2 | 1.5 | 0.75 | EMU 914400/in, 12700/pt; 16:9 = 12192000 × 6858000 (960 × 540 pt) correct; never says to read `prs.slide_width/height` instead of assuming the size (does read it in tblins-01) |
| pptx-model-03 | pptx-model, image-replace | 2 | 2 | 2 | 1.00 | layout/master inheritance and "propagates to every slide" met; no explicit one-slide override / tell-the-user scope decision (minor) |
| pptx-locate-01 | table-locate | 3 | 2 | 1 | 0.50 | `has_table` → `shape.table` met (no group recursion, minor). Point 2 missed: "check all shapes if there may be more than one" but never picks the table by header text; scans for `cell.text == "Gadget D"` to get an index with no unique-match assert |
| pptx-locate-02 | table-locate, pptx-model | 2 | 2 | 0.5 | 0.25 | representation point 0: merge is "the anchor cell's a:tcPr carrying rowSpan and/or colSpan; the covered grid positions have no separate a:tc" — wrong on all three counts (attributes are `gridSpan`/`rowSpan` on `a:tc`; every row has one `a:tc` per gridCol; covered cells exist with `hMerge`/`vMerge`). Point 2 half: text only in the origin met; for spans the advice is "never delete a row or column that cuts through a span", never how to adjust gridSpan/rowSpan and the covered cells |
| pptx-rowdel-01 | row-delete | 3 | 2 | 2 | 1.00 | no API; `tr.getparent().remove(tr)` / `table._tbl.remove(tr)` (`_Row._tr` is real); "fix row heights/total height and banding/fills" read as the frame height; data totals not named (minor) |
| pptx-rowdel-02 | row-delete, table-relayout | 3 | 2 | 2 | 1.00 | frame cy stale vs sum of rows; reduce cy by the removed row's height (equivalent), or grow the remaining rows |
| pptx-rowdel-03 | row-delete, cell-edit | 2 | 2 | 1 | 0.50 | style bandRow recomputed vs explicit `a:tcPr/a:solidFill` explained (point 1); no fix — never says to re-apply fills to the following rows or switch to style banding → point 2 missed |
| pptx-rowdel-04 | row-delete, verify-safety | 2 | 2 | 2 | 1.00 | text stays in the XML (searchable, screen readers, recoverable); remove the `a:tr`. "Rows grow to fit text" and an absence check across slide parts not named (minor) |
| pptx-cell-01 | cell-edit | 3 | 2 | 2 | 1.00 | |
| pptx-cell-02 | cell-edit, table-relayout | 2 | 2 | 2 | 1.00 | row auto-grows, overflow/overlap; widths, font size, `top + height` vs slide. Shapes below not named; "fixed row height" is not a thing (h is a minimum) (minor) |
| pptx-cell-03 | cell-edit | 2 | 2 | 2 | 1.00 | line total, subtotals, Total, charts/summary text, notes/other slides; keep number format not named (minor) |
| pptx-relayout-01 | table-relayout | 3 | 3 | 1.5 | 0.50 | deepcopy the Service G `a:tr` and insert after it, before Total (point 1). Point 2 half: text through existing runs keeping rPr, but the Total is never updated and the frame height is never touched. Point 3 0: no check of the frame's new bottom against the slide or the shapes below (banding parity via cloning named) |
| pptx-relayout-02 | table-relayout | 3 | 3 | 2 | 0.67 | gridCol + one `a:tc` in every `a:tr` after Item, merges adjusted ("colSpan" misnomer), "keep gridCols consistent with the cells per row" (point 1); point 2 missed: nothing about copying the header/body cell formatting into the new cells; widths subtracted from existing columns, frame right edge kept inside the slide (point 3) |
| pptx-tblins-01 | table-insert | 3 | 2 | 2 | 1.00 | |
| pptx-tblins-02 | table-insert | 2 | 2 | 2 | 1.00 | copy the style GUID + firstRow/bandRow flags into the new tblPr; element again named `a:tblStyleId` (real: `a:tableStyleId`); explicit fills/run props and widths not named (minor) |
| pptx-tblins-03 | table-insert, pptx-model | 1 | 2 | 1 | 0.50 | `ph.insert_table(rows, cols)` with the placeholder geometry (point 1), but claimed for "the empty content/object placeholder" — only a `TablePlaceholder` has it; so the fallback (add_table at the placeholder bbox and remove the empty placeholder) is missing → point 2 = 0 |
| pptx-imgrep-01 | image-replace | 3 | 3 | 3 | 1.00 | old rect recorded, add_picture at the same rect, new `p:pic` inserted at the old spTree index, old removed, hyperlink re-attached; "drop its relationship/media part separately" assumes the old part is unreferenced with no other-user check; aspect caveat not given (minor) |
| pptx-imgrep-02 | image-replace | 2 | 2 | 2 | 1.00 | shared image part behind both blips (SHA1 dedupe not named, minor); fix by a new part + repointed blip |
| pptx-imgrep-03 | image-replace, verify-safety | 2 | 2 | 0.5 | 0.25 | mechanism point 0: "both pictures referenced the same rId; drop_rel removed that relationship entirely" — never says the ref count matches only `r:id`, so `a:blip r:embed` counts 0 and image rels are always dropped. Point 2 0: "drop a rel only when nothing references it" with no `//@r:embed`/`r:link` scan → this is exactly the 0.5 partial |
| pptx-imgins-01 | image-insert | 3 | 2 | 2 | 1.00 | |
| pptx-imgins-02 | image-insert | 2 | 2 | 2 | 1.00 | crops to fill; fix by add_picture fitted inside the bounds or `crop_*` reset (resize not named, minor) |
| pptx-legacy-01 | legacy-ppt | 3 | 2 | 2 | 1.00 | binary .ppt not openable (OLE2 not named, minor); soffice headless convert, edit, convert back with a fidelity warning |
| pptx-legacy-02 | legacy-ppt, verify-safety | 2 | 2 | 2 | 1.00 | diff tables/fonts/merges/images/slide size before, re-open and check after; tell the user, keep the original; "convert back only expecting further changes" is garbled but the convert-back option is present (minor) |
| pptx-verify-01 | verify-safety | 3 | 2 | 2 | 1.00 | backup + new filename, then "only then replace the original" (the backup survives); reopen, zip/rels check, text/table compare before vs after, PowerPoint/LibreOffice open; unchanged-remainder diff not explicit (minor) |
| pptx-verify-02 | verify-safety, pptx-model | 2 | 2 | 2 | 1.00 | broken spans ("rowSpan/colSpan exceeding the grid"), dangling rIds, schema order, missing required children; tc-count vs gridCol, tc without txBody, duplicate cNvPr ids and "python-pptx doesn't validate" not named; "unescaped & < >" cannot come out of lxml (minor) |
| pptx-verify-03 | verify-safety, image-replace | 2 | 2 | 2 | 1.00 | rel + `ppt/media` survive; list media, grep all `.rels`, check other slides, drop rel + part; hash compare not named (minor) |

`normalized = min(awarded, full) / full`

## Per-tag subscores

| tag | subscore | n | trust | action |
|-----|---------:|--:|-------|--------|
| pptx-model | 0.73 | 6 | ok | **derive** |
| table-locate | 0.40 | 2 | low-n | **derive** (low-n) |
| row-delete | 0.90 | 4 | ok | omit |
| cell-edit | 0.89 | 4 | ok | **derive** |
| table-relayout | 0.77 | 4 | ok | **derive** |
| table-insert | 0.92 | 3 | low-n | omit |
| image-replace | 0.86 | 5 | ok | **derive** |
| image-insert | 1.00 | 2 | low-n | omit (strong) |
| legacy-ppt | 1.00 | 2 | low-n | omit (strong) |
| verify-safety | 0.88 | 6 | ok | **derive** |

`subscore = Σ(normalized×weight) / Σ(weight)` over that tag's questions.

- pptx-model: (2.25+1.5+2+0.5+0.5+2) / 12 = 8.75 / 12 = 0.729
- table-locate: (1.5+0.5) / 5 = 2.0 / 5 = 0.40
- row-delete: (3+3+1+2) / 10 = 9 / 10 = 0.90 (at threshold, not below)
- cell-edit: (1+3+2+2) / 9 = 8 / 9 = 0.889
- table-relayout: (3+2+1.5+2) / 11 = 8.5 / 11 = 0.773
- table-insert: (3+2+0.5) / 6 = 5.5 / 6 = 0.917
- image-replace: (2+3+2+0.5+2) / 11 = 9.5 / 11 = 0.864
- image-insert: (3+2) / 5 = 1.00
- legacy-ppt: (3+2) / 5 = 1.00
- verify-safety: (2+0.5+2+3+2+2) / 13 = 11.5 / 13 = 0.885

## Stack score

```
stack_score = Σ(normalized×weight) / Σ(weight) = 55.25 / 65 = 85.0%
```

(Lost weight: model-01 0.75, model-02 0.5, locate-01 1.5, locate-02 1.5,
rowdel-03 1.0, relayout-01 1.5, relayout-02 1.0, tblins-03 0.5, imgrep-03 1.5.)

## Derivation targets

Tags below threshold (`< 0.9`): **pptx-model** (0.73), **table-locate** (0.40,
low-n), **cell-edit** (0.89), **table-relayout** (0.77), **image-replace**
(0.86), **verify-safety** (0.88) →
`derived/pptx.nvidia__nemotron-3.5-super-vl.SKILL.md`.

- **table-locate:** takes whichever shape has `has_table` without choosing it by
  header text, finds the row by a plain `cell.text` comparison with no
  unique-match assert, and does not recurse into groups.
- **pptx-model (merges):** believes a merge is "rowSpan/colSpan on a:tcPr" and
  that covered grid positions have no `a:tc` at all (repeated in model-01,
  locate-02, relayout-02, verify-02 as "colSpan"). Every `a:tr` has one `a:tc`
  per `a:gridCol`; the origin carries `gridSpan`/`rowSpan` as attributes of
  `a:tc`, the covered cells carry `hMerge`/`vMerge`.
- **pptx-model (element names):** `a:textBody` for `a:txBody`, `a:tblStyleId`
  for `a:tableStyleId` (twice); never reads `prs.slide_width/height` in the
  units question; claims `insert_table` on a generic content placeholder.
- **cell-edit:** explains why explicit `tcPr` fills break the alternation after
  a delete but never re-applies the fills (or switches to style banding).
- **table-relayout:** after inserting a row, never updates the Total or the
  frame height and never checks the new bottom against the slide and the shapes
  below; after inserting a column, never copies the header/body cell formatting
  into the new cells.
- **image-replace / verify-safety:** attributes the `drop_rel` breakage to
  "relationships are shared", not to the `r:id`-only reference count that makes
  every image rel count 0; the only guard offered is "drop only when nothing
  references it", with no `//@r:embed | //@r:link` scan before the call.

## Anomalies

- No truncation: all 27 ids answered once, every answer complete, no empty
  records, no duplicated answers.
- No thinking-aloud, no fabricated tool output, no tool or skill cited.
- Answers are terse single paragraphs with no code blocks; nothing shares
  wording with the reference answers. The recurring misnomers (`colSpan`,
  `a:tblStyleId`, `a:textBody`, "covered positions have no a:tc") are
  independent of the key. No contamination signal.
- `pptx-verify-01` ends with "only then replace the original" after a backup;
  graded as original preserved, noted because a literal follow-through still
  overwrites the user's file path.
