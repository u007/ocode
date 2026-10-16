---
model_id: nvidia/nemotron-3.5-super-vl
model_version: "3.5"
evaluated_via: openrouter
evaluated_on: 2026-10-16
stack: pptx
stack_corpus_rev: 1
threshold: 0.9
validation: with-skill
---

# Scorecard — nvidia/nemotron-3.5-super-vl on pptx (with-skill validation, threshold 0.9)

> Valid ONLY for `nvidia/nemotron-3.5-super-vl` @ `3.5`. A version bump invalidates
> this scorecard — re-benchmark.

> **WITH-SKILL VALIDATION RUN.** Answers:
> `pptx/answers/nvidia__nemotron-3.5-super-vl.with-skill.md`, produced closed-book
> with the current derived skill `derived/pptx.nvidia__nemotron-3.5-super-vl.SKILL.md`
> (`pptx-tuning-nvidia-nemotron-3.5-super-vl`, front matter stripped) prepended to
> the `_prompts/pptx.md` sheet as active guidance (session audited: 1 user +
> 1 assistant message, 0 tool calls). Baseline:
> `scores/nvidia__nemotron-3.5-super-vl.md`. No new derived skill is produced from
> this run. This scorecard validates `../derived/pptx.nvidia__nemotron-3.5-super-vl.SKILL.md`.

> Target tags for this skill (baseline < 0.9): **pptx-model, table-locate (low-n),
> cell-edit, table-relayout, image-replace, verify-safety**.

## Provenance

Closed-book (Rule 0). Same setup as the baseline:
`ocode run -m openrouter/nvidia/nemotron-3.5-super-vl:free -effort med` under an
isolated HOME whose config denies every tool (no tools advertised), in a scratch
cwd containing only the prompt, one fresh session, with the derived skill body
prepended to the `_prompts/pptx.md` sheet; no answer key. All 27 ids came back
once, none empty. No answer cites a tool or a skill the question never named.
Graded against `pptx/questions.yaml` corpus_rev 1 (python-pptx facts verified on
1.0.2) with the baseline's rules: core concept present = full point, materially
wrong claim about the same construct = 0 (judgment), split point = 0.5 when one
half is present; the table-locate question needs header-based table choice AND a
unique-row assert for point 2.

## Per-question results

| id | tags | weight | full | awarded | normalized | notes |
|----|------|-------:|-----:|--------:|-----------:|-------|
| pptx-model-01 | pptx-model | 3 | 2 | 2 | 1.00 | now `a:txBody`, `a:tableStyleId`, one `a:tc` per column incl. covered cells; residual parenthetical "a:tcPr (… merge attributes)" (spans sit on `a:tc`, as its own locate-02 states); graphic/graphicData and xfrm not named (minor) |
| pptx-model-02 | pptx-model | 2 | 2 | 2 | 1.00 | EMU factors, 4:3 default 9144000 × 6858000, 16:9 12192000 × 6858000; "always read prs.slide_width/height" → baseline gap closed |
| pptx-model-03 | pptx-model, image-replace | 2 | 2 | 2 | 1.00 | master/layout inheritance; global effect of editing there; adds the shared-image-part caveat (correct) |
| pptx-locate-01 | table-locate | 3 | 2 | 2 | 1.00 | `has_table` with GroupShape recursion; table picked by stripped first-row header with a unique-table assert; row by exact stripped key-cell text, all hits collected, `len(hits) == 1` → both baseline gaps closed |
| pptx-locate-02 | table-locate, pptx-model | 2 | 2 | 2 | 1.00 | `<a:tc gridSpan="2">` + covered `<a:tc hMerge="1">`, rowSpan/vMerge, one tc per gridCol; write to origin; change span AND covered cell together, per-row count assert. Baseline "colSpan on a:tcPr / no covered a:tc" gone |
| pptx-rowdel-01 | row-delete | 3 | 2 | 2 | 1.00 | no API; `tr.getparent().remove(tr)`; Totals, banding fills, frame height named |
| pptx-rowdel-02 | row-delete, table-relayout | 3 | 2 | 2 | 1.00 | frame still sized for the old row count; `gf.height = sum(r.height …)`, bottom vs slide and shapes below |
| pptx-rowdel-03 | row-delete, cell-edit | 2 | 2 | 2 | 1.00 | style bandRow vs explicit `a:tcPr/a:solidFill`; fix: re-assign alternating fills after the gap or clear fills and set `bandRow="1"` → baseline point 2 now met |
| pptx-rowdel-04 | row-delete, verify-safety | 2 | 2 | 2 | 1.00 | row and text remain in the XML, extractable, screen readers; delete the `a:tr` and fix height/fills/Totals; absence check across parts not named (minor, as baseline) |
| pptx-cell-01 | cell-edit | 3 | 2 | 2 | 1.00 | |
| pptx-cell-02 | cell-edit, table-relayout | 2 | 2 | 2 | 1.00 | row height is a minimum, row grows; widths, word_wrap, font; frame height, bottom vs slide and shapes below |
| pptx-cell-03 | cell-edit | 2 | 2 | 1.5 | 0.75 | line total + Total recomputed (point 1). Point 2 half: other occurrences only as "any subtitle or shape repeating it"; summary slide, chart data and notes (all named in the baseline) are gone, keep-format not named; adds banding/overflow re-checks instead |
| pptx-relayout-01 | table-relayout | 3 | 3 | 3 | 1.00 | `ref_tr.addnext(copy.deepcopy(ref_tr))` before Total; text via existing first run; Total recomputed, `gf.height` = row sum; bottom vs `prs.slide_height` and shapes below; banding re-applied; per-row tc assert → both baseline gaps closed |
| pptx-relayout-02 | table-relayout | 3 | 3 | 3 | 1.00 | gridCol + one `a:tc` per `a:tr` after Item; header cell deep-copied from the Item header, body from an Item body cell (baseline gap closed); merged Total: gridSpan bump + covered hMerge copy; widths rebalanced, `gf.width` set, per-row count assert |
| pptx-tblins-01 | table-insert | 3 | 2 | 2 | 1.00 | |
| pptx-tblins-02 | table-insert | 2 | 2 | 2 | 1.00 | `a:tableStyleId` now named correctly; copies flags, explicit solidFill and header formatting; "horzBanding" is the python-pptx property, the attribute is `bandRow` (minor) |
| pptx-tblins-03 | table-insert, pptx-model | 1 | 2 | 2 | 1.00 | only `TablePlaceholder` has `insert_table` → `PlaceholderGraphicFrame`; `SlidePlaceholder` → `add_table` at the ph bbox and remove `ph._element` → baseline gap closed |
| pptx-imgrep-01 | image-replace | 3 | 3 | 3 | 1.00 | old geometry + spTree index; add_picture, move new `p:pic` to the old index, remove old; drop the old rel only after an `r:embed`/`r:link` scan; aspect caveat not given (minor, as baseline) |
| pptx-imgrep-02 | image-replace | 2 | 2 | 2 | 1.00 | shared image part behind several blips; new part + repoint only the target blip (SHA1 dedupe not named, minor) |
| pptx-imgrep-03 | image-replace, verify-safety | 2 | 2 | 2 | 1.00 | ref count is an xpath for `//@r:id`, `a:blip r:embed` invisible → image rels always dropped; own `//@r:embed \| //@r:link` scan, repoint survivors first → baseline 0.25 to full |
| pptx-imgins-01 | image-insert | 3 | 2 | 2 | 1.00 | width-only keeps aspect; bottom-right from the real slide size; overlap check with group recursion |
| pptx-imgins-02 | image-insert | 2 | 2 | 2 | 1.00 | "cropped/stretched … fills rather than letterboxes" hedges on stretch (insert_picture crops, never stretches); fix via add_picture with one dimension met; `crop_*` reset not named this time (minor) |
| pptx-legacy-01 | legacy-ppt | 3 | 2 | 2 | 1.00 | OLE2 compound file named; soffice headless convert, edit, verify; convert-back not spelled out (minor) |
| pptx-legacy-02 | legacy-ppt, verify-safety | 2 | 2 | 2 | 1.00 | before/after checks incl. repair-free open; tells the user, original untouched, .pptx deliverable with the round-trip caveat |
| pptx-verify-01 | verify-safety | 3 | 2 | 2 | 1.00 | new file, re-open, exact cells/merges/geometry asserts, render open, media hash + rels grep; still ends "only then replace the original" (graded as baseline: original preserved until verified, noted) |
| pptx-verify-02 | verify-safety, pptx-model | 2 | 2 | 2 | 1.00 | tc count ≠ gridCol after merge edits, schema order, dangling/duplicate rIds, missing content types, "python-pptx tolerates XML PowerPoint rejects"; lists the skill's element-name corrections as causes (valid: unknown elements do trigger repair); tc without txBody and duplicate cNvPr not named (minor) |
| pptx-verify-03 | verify-safety, image-replace | 2 | 2 | 2 | 1.00 | rel + media survive; zip listing + hash compare + rels grep + r:embed scan; drop rel only when no other user, delete the part, re-verify |

`normalized = min(awarded, full) / full`

## Per-tag subscores

| tag | subscore | n | trust | baseline | Δ | target? |
|-----|---------:|--:|-------|---------:|---:|---------|
| pptx-model | 1.00 | 6 | ok | 0.73 | +0.27 | **yes** |
| table-locate | 1.00 | 2 | low-n | 0.40 | +0.60 | **yes** (low-n) |
| row-delete | 1.00 | 4 | ok | 0.90 | +0.10 | no |
| cell-edit | 0.94 | 4 | ok | 0.89 | +0.05 | **yes** |
| table-relayout | 1.00 | 4 | ok | 0.77 | +0.23 | **yes** |
| table-insert | 1.00 | 3 | low-n | 0.92 | +0.08 | no |
| image-replace | 1.00 | 5 | ok | 0.86 | +0.14 | **yes** |
| image-insert | 1.00 | 2 | low-n | 1.00 | 0.00 | no |
| legacy-ppt | 1.00 | 2 | low-n | 1.00 | 0.00 | no |
| verify-safety | 1.00 | 6 | ok | 0.88 | +0.12 | **yes** |

`subscore = Σ(normalized×weight) / Σ(weight)` over that tag's questions.

Arithmetic: pptx-model (3+2+2+2+1+2)/12 = 12/12; table-locate (3+2)/5 = 5/5;
row-delete (3+3+2+2)/10 = 10/10; cell-edit (2+3+2+1.5)/9 = 8.5/9 = 0.944;
table-relayout (3+2+3+3)/11 = 11/11; table-insert (3+2+1)/6 = 6/6; image-replace
(2+3+2+2+2)/11 = 11/11; image-insert (3+2)/5 = 5/5; legacy-ppt (3+2)/5 = 5/5;
verify-safety (2+2+2+3+2+2)/13 = 13/13.

## Stack score

```
stack_score = Σ(normalized×weight) / Σ(weight) = 64.5 / 65 = 99.2%
```

(Lost weight: cell-03 0.5.) Baseline 55.25 / 65 = 85.0%.

## Target-tag verdict

| tag | baseline | with-skill | n | pass (≥ 0.9)? |
|-----|---------:|-----------:|--:|---------------|
| pptx-model | 0.73 | 1.00 | 6 | **PASS** |
| table-locate | 0.40 | 1.00 | 2 (low-n) | **PASS** |
| cell-edit | 0.89 | 0.94 | 4 | **PASS** |
| table-relayout | 0.77 | 1.00 | 4 | **PASS** |
| image-replace | 0.86 | 1.00 | 5 | **PASS** |
| verify-safety | 0.88 | 1.00 | 6 | **PASS** |

Every target tag reaches 0.9. The skill is validated for
nvidia/nemotron-3.5-super-vl @ 3.5. Each baseline misconception the skill cites is
absent from the with-skill answers: first-`has_table` table choice without a header
match or unique-row assert (locate-01); "rowSpan/colSpan on a:tcPr, covered positions
have no a:tc" (locate-02, relayout-02, verify-02); `a:textBody` / `a:tblStyleId`
(model-01, tblins-02); no "read prs.slide_width/height" (model-02); `insert_table`
on a generic content placeholder with no fallback (tblins-03); no banding fix
(rowdel-03); row insert without Total, frame height or bottom check (relayout-01);
new column cells without copied formatting (relayout-02); "shared rIds" instead of the
`r:id`-only reference count and no `r:embed`/`r:link` scan (imgrep-03). No target tag
remains below threshold, so there is no remaining missed target point to list.

The one remaining loss inside a target tag is cell-03 point 2 (0.5): the with-skill
answer names only "any subtitle or shape repeating it" where the baseline named charts,
summary text, notes and other slides. The skill has no cell-03 rule (that point was met
at baseline), so this is single-sample variance, not a skill defect; cell-edit still
passes at 0.94.

Non-target drift: none downward — row-delete 0.90 → 1.00, table-insert 0.92 → 1.00,
image-insert and legacy-ppt held at 1.00. Per HOW-TO-EVALUATE.md these single-sample
moves are not acted on.

## Anomalies

- All 27 ids answered once; no empty, duplicated or truncated records; no
  thinking-aloud, no fabricated tool output, no tool or skill cited.
- No verbatim overlap with the reference answers. Heavy overlap with the derived
  skill's phrasing (`len(tr.tc_lst) == len(tblGrid.gridCol_lst)`,
  `//@r:embed | //@r:link`, "never colSpan, never on a:tcPr") is the expected
  absorption of the injected guidance, not contamination.
- The skill's rules bleed into questions that did not ask for them: rowdel-02,
  cell-02, cell-03 and tblins-01 all append the banding re-apply / bottom-check /
  tc-count assert checklist; verify-02 recites the skill's element-name corrections
  as repair causes. Harmless here (all correct), but it is the over-application
  pattern to watch for in live use.
- `pptx-imgins-02` now hedges "cropped/stretched" where the baseline said cropped;
  non-target, graded full on the correct fill-not-letterbox mechanism.
- `pptx-verify-01` keeps the baseline's "only then replace the original" ending.
