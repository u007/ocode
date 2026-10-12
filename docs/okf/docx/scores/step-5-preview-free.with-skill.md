---
model_id: step-5-preview-free
model_version: "5-preview"
evaluated_via: opencode-go
evaluated_on: 2026-10-12
stack: docx
stack_corpus_rev: 1
threshold: 0.9
validation: with-skill
---

# Scorecard — step-5-preview-free on docx (with-skill validation)

> Valid ONLY for `step-5-preview-free` @ `5-preview`. A version bump invalidates
> this scorecard — re-benchmark.

> **WITH-SKILL VALIDATION RUN.** Answers: `docx/answers/step-5-preview-free.with-skill.md`,
> produced closed-book with the current derived skill
> `derived/docx.step-5-preview-free.SKILL.md` (`docx-tuning-step-5-preview-free`)
> prepended to the answerer prompt as active guidance (audited zero tool calls).
> Baseline: `scores/step-5-preview-free.md`. No new derived skill is produced from
> this run.

> Target tags for this skill (baseline < 0.9): **row-delete (0.83), image-replace (0.78)**.

## Provenance

Answers: `docx/answers/step-5-preview-free.with-skill.md`, produced closed-book by
`ocode run -m opencode-go/step-5-preview-free` in a scratch directory
(session `ses_2026-10-12-125813-f15f31c3`) with only the `_prompts/docx.md` sheet
attached, the skill body under an "Active guidance" heading at the top of the
sheet; the session sqlite shows exactly 1 user + 1 assistant message, 0 tool calls,
no answer key in the prompt. The answers file was compared record-by-record against
the session's raw assistant message before grading: all 29 records are identical
(an earlier copy of the file was clipped at the first closing code fence in
rowdel-01, tblins-01, imgins-01 and legacy-03; that copy was repaired before this
grade and is not what was graded).

Graded against `docx/questions.yaml` corpus_rev 1 (python-docx facts verified on
1.2.0). Grading rule applied (same strictness as the baseline): a `point` whose
core concept is present earns full credit (minor missing sub-items are noted); a
`point` whose only concrete mechanism is a wrong API fact, or that is satisfied
only by a cover-up, earns 0. Points are whole; fractions come only from a listed
partial. Where the baseline grader awarded a point despite an overstated side
claim about the same construct, the same call is made here and marked
"(judgment, as baseline)".

## History

- Earlier with-skill scorecard: none (this is the first with-skill validation for
  step-5-preview-free on docx).

## Per-question results

| id | tags | weight | full | awarded | normalized | notes |
|----|------|-------:|-----:|--------:|-----------:|-------|
| docx-model-01 | docx-model | 3 | 2 | 2 | 1.00 | full hierarchy incl. tblPr/tcPr children; also Content_Types and per-part .rels |
| docx-model-02 | docx-model, table-locate | 3 | 2 | 2 | 1.00 | splice into the first involved run keeping its rPr; adds "check the collapsed runs share rPr" |
| docx-model-03 | docx-model, table-relayout | 2 | 2 | 2 | 1.00 | integer-EMU gotcha kept |
| docx-locate-01 | table-locate | 3 | 2 | 2 | 1.00 | table by header text, exactly one candidate; row by normalized cell text, exactly one match; header-part tables not mentioned (minor) |
| docx-locate-02 | table-locate, docx-model | 2 | 2 | 2 | 1.00 | physical cells via `row._tr.xpath("./w:tc")` (= tc_lst); vMerge repetition not mentioned (minor) |
| docx-rowdel-01 | row-delete | 3 | 2 | 2 | 1.00 | remove `row._tr`; "contiguous siblings, Word reflows, no gap to close"; totals/SUM(ABOVE) cached result must be recomputed and rewritten. Baseline 0.50 — both gaps closed |
| docx-rowdel-02 | row-delete, table-locate | 2 | 2 | 2 | 1.00 | now MOVES the restart cell's paragraphs into the next row's cell before promoting it (baseline left the content behind) |
| docx-rowdel-03 | row-delete, cell-edit | 2 | 2 | 2 | 1.00 | cached result; rewrite result run keeping the field; updateFields "only prompts" |
| docx-rowdel-04 | row-delete, verify-safety | 2 | 2 | 2 | 1.00 | hidden/white/0-height still in XML; count w:tr −1 and grep every part incl. headers/footers and w:delText |
| docx-cell-01 | cell-edit | 3 | 2 | 2 | 1.00 | |
| docx-cell-02 | cell-edit, table-locate | 2 | 2 | 2 | 1.00 | |
| docx-cell-03 | cell-edit, verify-safety | 2 | 2 | 2 | 1.00 | names w:delText as the hiding place of deleted text; no explicit post-edit "check w:delText no longer holds the old value" (minor, as baseline) |
| docx-relayout-01 | table-relayout | 3 | 3 | 3 | 1.00 | deepcopy + addprevious; text via the copied runs' w:t; re-check alternating w:shd. "only widths" not stated for add_row's cells (minor) |
| docx-relayout-02 | table-relayout | 3 | 3 | 3 | 1.00 | pt2 now present: insert the tc at the correct grid position in every row and the gridCol at the matching index (baseline 0.67 lacked it). Repeats the baseline's wrong side claim that add_column "breaks merged rows' grid mapping" (it appends one tc per row, Σ gridSpan stays consistent) — pt1 awarded as baseline (judgment, as baseline). gridSpan handling at the insertion point and a distinct header-cell copy not spelled out (minor) |
| docx-relayout-03 | table-relayout, docx-model | 2 | 2 | 2 | 1.00 | |
| docx-tblins-01 | table-insert | 3 | 2 | 2 | 1.00 | correct "before w:sectPr"; `notes_paragraph._p.addnext(table._tbl)`; "Word needs a paragraph between tables" overstated (minor, as baseline) |
| docx-tblins-02 | table-insert | 2 | 2 | 2 | 1.00 | "Table Grid if present" — avoids the built-in-style trap; tblBorders fallback |
| docx-tblins-03 | table-insert, table-relayout | 2 | 2 | 2 | 1.00 | default correctly described, hedged on version; matching copies tblPr/tblGrid/tcW; header formatting not called out (minor, as baseline) |
| docx-imgrep-01 | image-replace, docx-model | 3 | 3 | 3 | 1.00 | owning-part rels; `rId, image = part.get_or_add_image(...)` with the correct tuple order (baseline had it reversed); repoint r:embed, keep extents, integer cy; no blob overwrite (SHA1 dedupe); "drop the old rel, then confirm from the saved zip" (baseline 0.67 believed it vanished on its own) |
| docx-imgrep-02 | image-replace | 2 | 2 | 2 | 1.00 | "check references first" implicit in "original part still referenced by the other picture" (minor) |
| docx-imgrep-03 | image-replace, table-locate | 2 | 2 | 2 | 1.00 | header/first/even parts, is_linked_to_previous, that part's rels, drop old rel per part after counting r:embed; anchored pictures not mentioned (minor). The baseline's byte-swap alternative is gone |
| docx-imgins-01 | image-insert | 3 | 2 | 2 | 1.00 | `run = paragraph.add_run(); run.add_picture(..., width=Inches(1.5))` — the baseline's broken `doc.add_picture()._r` code is gone |
| docx-imgins-02 | image-insert, table-relayout | 2 | 2 | 2 | 1.00 | overflow case given; still claims "the column does not grow" (autofit does widen; minor, as baseline). Sizes to tcW minus tcMar |
| docx-legacy-01 | legacy-doc | 3 | 2 | 2 | 1.00 | "not a ZIP"; OLE2 and "renaming doesn't help" not stated (minor); soffice convert, edit, convert back if required |
| docx-legacy-02 | legacy-doc, verify-safety | 2 | 2 | 2 | 1.00 | structural inspection list incl. fields; fallback to LibreOffice/Word not stated (minor, as baseline) |
| docx-legacy-03 | legacy-doc | 1 | 2 | 2 | 1.00 | running instance forwards to GUI; `-env:UserInstallation=file:///tmp/...` + `--outdir`, check the output exists |
| docx-verify-01 | verify-safety | 3 | 2 | 2 | 1.00 | backup + new file; re-open the saved file and assert; diff against original via independent readers |
| docx-verify-02 | verify-safety, docx-model | 2 | 2 | 2 | 1.00 | schema order, grid mismatch, orphan vMerge, empty tc, dangling rels, float EMU; "python-docx only needs well-formed XML"; duplicate docPr ids not named (minor, as baseline); "two adjacent w:tbl" overstated (minor) |
| docx-verify-03 | verify-safety, image-replace | 2 | 2 | 2 | 1.00 | pt1 now present: removing the drawing leaves the rel + media part; count `a:blip[@r:embed]` per part, `drop_rel` at zero, hash zip media (baseline 0.50) |

`normalized = min(awarded, full) / full`

## Per-tag subscores

| tag | baseline | with-skill | delta | n | trust | target? |
|-----|---------:|-----------:|------:|--:|-------|---------|
| docx-model | 0.94 | 1.00 | +0.06 | 7 | ok | no |
| table-locate | 1.00 | 1.00 | 0.00 | 6 | ok | no |
| row-delete | 0.83 | 1.00 | +0.17 | 4 | ok | **yes** |
| cell-edit | 1.00 | 1.00 | 0.00 | 4 | ok | no |
| table-relayout | 0.93 | 1.00 | +0.07 | 6 | ok | no |
| table-insert | 1.00 | 1.00 | 0.00 | 3 | low-n | no |
| image-replace | 0.78 | 1.00 | +0.22 | 4 | ok | **yes** |
| image-insert | 1.00 | 1.00 | 0.00 | 2 | low-n | no |
| legacy-doc | 1.00 | 1.00 | 0.00 | 3 | low-n | no |
| verify-safety | 0.92 | 1.00 | +0.08 | 6 | ok | no |

`subscore = Σ(normalized×weight) / Σ(weight)` over that tag's questions.

Arithmetic: docx-model 17/17; table-locate 14/14; row-delete 9/9; cell-edit 9/9;
table-relayout 14/14; table-insert 7/7; image-replace 9/9; image-insert 5/5;
legacy-doc 6/6; verify-safety 13/13.

## Stack score

```
stack_score = Σ(normalized×weight) / Σ(weight) = 69 / 69 = 100.0%
```

(Σweight = 12×3 + 16×2 + 1×1 = 69; no losses.) Baseline 64.5 / 69 = 93.5%.

## Target-tag verdict

| tag | baseline | with-skill | pass (≥ 0.9)? |
|-----|---------:|-----------:|---------------|
| row-delete | 0.83 | 1.00 | **PASS** |
| image-replace | 0.78 | 1.00 | **PASS** |

Every target tag reaches 0.9. The skill is validated for step-5-preview-free @
5-preview. Both baseline failure modes are gone: rowdel-01 now states reflow and
the dependent-total rewrite; imgrep-01 and verify-03 now say the old rel/media
part stays until the rel is dropped, and the `get_or_add_image` tuple order is
correct. No remaining wrong or missing claim to list for a target tag.

The target-tag answers reuse the skill's own phrasing ("contiguous siblings",
"returns the rId FIRST", "count r:embed yourself"). Per HOW-TO-EVALUATE.md
("Overlap ≠ cheating") this is the absorption the run measures, not contamination;
the prompt carried no answer key and the session made no tool calls.

Non-target drift: none downward. docx-model, table-relayout and verify-safety rose
(+0.06, +0.07, +0.08) through questions that share a construct with the targets
(imgrep-01, verify-03) or resampled better (relayout-02 pt2); every other tag held
at 1.00. Nothing here is a reason to touch the skill.
