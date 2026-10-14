---
model_id: cat-jam-free
model_version: "alpha"
evaluated_via: opencode-go
evaluated_on: 2026-10-14
stack: docx
stack_corpus_rev: 1
threshold: 0.9
validation: with-skill
---

# Scorecard — cat-jam-free on docx (with-skill validation)

> Valid ONLY for `cat-jam-free` @ `alpha`. A version bump invalidates
> this scorecard — re-benchmark.

> **WITH-SKILL VALIDATION RUN.** Answers: `docx/answers/cat-jam-free.with-skill.md`,
> produced closed-book with the current derived skill
> `derived/docx.cat-jam-free.SKILL.md` (`docx-tuning-cat-jam-free`) prepended to
> the answerer prompt as active guidance. Baseline: `scores/cat-jam-free.md`. No new
> derived skill is produced from this run.

> Target tags for this skill (baseline < 0.9): **docx-model (0.85), table-locate (0.75),
> row-delete (0.72), cell-edit (0.50), table-relayout (0.71), table-insert (0.86, low-n),
> image-replace (0.78), legacy-doc (0.75, low-n)**.

## Provenance

Answers: `docx/answers/cat-jam-free.with-skill.md`, produced closed-book by
`ocode run -m opencode-go/cat-jam-free -effort med` with the SKILL.md body (front
matter stripped) prepended to the `_prompts/docx.md` sheet, in an isolated HOME with
every tool denied and a cwd holding only the sheet. The session sqlite shows exactly
1 user + 1 assistant message and 0 tool calls; no answer key in the prompt. All 29
ids are present, with no extras or duplicates.

Graded against `docx/questions.yaml` corpus_rev 1 (python-docx facts verified on
1.2.0), with the same strictness as the baseline: a `point` whose core concept is
present earns full credit (minor missing sub-items are noted); a `point` that is
absent, only name-dropped, or whose only concrete mechanism is materially wrong
earns 0. Points are whole; fractions come only from a listed partial.

## History

- Earlier with-skill scorecard: none (first with-skill validation for cat-jam-free
  on docx).

## Per-question results

| id | tags | weight | full | awarded | normalized | notes |
|----|------|-------:|-----:|--------:|-----------:|-------|
| docx-model-01 | docx-model | 3 | 2 | 2 | 1.00 | ZIP of OPC parts incl. relationships; full tbl/tblGrid/tr/tc/p/r/t hierarchy with Pr children; header/footer/textbox/nested locations |
| docx-model-02 | docx-model, table-locate | 3 | 2 | 2 | 1.00 | pt2 now present: match on paragraph.text, write into the first covered run, blank the rest, keeping its rPr (baseline 0.50) |
| docx-model-03 | docx-model, table-relayout | 2 | 2 | 2 | 1.00 | |
| docx-locate-01 | table-locate | 3 | 2 | 1 | 0.50 | pt1 ok (table by header-row content, not index; header/footer and nested tables). pt2 absent: the answer never addresses finding the "Gadget D" row by cell text, nor asserting exactly one match; it moves straight to "what to check before editing" (merges, revisions, fields). Baseline 1.00 — a resampling regression |
| docx-locate-02 | table-locate, docx-model | 2 | 2 | 2 | 1.00 | physical cells via `row._tr.findall(w:tc)` + gridSpan/vMerge; "same _Cell object" not stated (minor) |
| docx-rowdel-01 | row-delete | 3 | 2 | 1 | 0.50 | pt1 ok (no delete API; `tr.getparent().remove(tr)`). pt2 withheld (judgment): the lead concept of the point, "Word reflows, no gap to close manually", is still absent even though the skill states it; only the "recompute dependent totals/field results" half is present. If awarded, row-delete would read 1.00 — see verdict |
| docx-rowdel-02 | row-delete, table-locate | 2 | 2 | 2 | 1.00 | now moves the restart cell's paragraphs into the next row's cell before promoting it; drops vMerge if no continuation follows |
| docx-rowdel-03 | row-delete, cell-edit | 2 | 2 | 2 | 1.00 | pt1 now correct: Word does not recompute on open; the `fullCalcOnLoad` myth is gone. Writes the cached result between separate/end; updateFields "to prompt" (baseline 0.50) |
| docx-rowdel-04 | row-delete, verify-safety | 2 | 2 | 2 | 1.00 | headers/footers not searched (minor, as baseline) |
| docx-cell-01 | cell-edit | 3 | 2 | 2 | 1.00 | pt2 now present: runs[0].text + blank the others, or copy rPr (baseline 0.50) |
| docx-cell-02 | cell-edit, table-locate | 2 | 2 | 2 | 1.00 | pt2 now present: existing number format, decimals, separators, currency, alignment, written through the first run (baseline 0.50) |
| docx-cell-03 | cell-edit, verify-safety | 2 | 2 | 2 | 1.00 | pt2 now present: decide tracked vs clean, accept/reject existing revisions, check w:delText; "turn off trackRevisions" is gone (baseline 0.50) |
| docx-relayout-01 | table-relayout | 3 | 3 | 3 | 1.00 | deepcopy + `total_tr.addprevious`, alternating shading, text through the existing first run (baseline 0.67) |
| docx-relayout-02 | table-relayout | 3 | 3 | 3 | 1.00 | gridCol after Item's + deep copy of Item's tc in every row, gridSpan sums; header/body formatting copied; rebalance gridCol/tcW/tblW to the text width (baseline 0.33) |
| docx-relayout-03 | table-relayout, docx-model | 2 | 2 | 2 | 1.00 | |
| docx-tblins-01 | table-insert | 3 | 2 | 2 | 1.00 | `paragraph._p.addnext(table._tbl)` |
| docx-tblins-02 | table-insert | 2 | 2 | 2 | 1.00 | "a style that exists in the document such as Table Grid" — no `doc.styles` listing (minor, as baseline) |
| docx-tblins-03 | table-insert, table-relayout | 2 | 2 | 2 | 1.00 | pt1 now correct: explicit gridCol/tcW, text width split evenly (baseline "unset/auto" myth gone); header run formatting now named |
| docx-imgrep-01 | image-replace, docx-model | 3 | 3 | 2 | 0.67 | pt1 ok (a:blip r:embed → owning part's rel → image part). pt2 ok (`get_or_add_image` on the owning part, repoint r:embed, keep extents, adjust one dimension for aspect); the byte-overwrite route is gone. pt3 absent: the old rel/media is never removed — the answer ends at "Never overwrite an image part's bytes". Unchanged from baseline on this point |
| docx-imgrep-02 | image-replace | 2 | 2 | 2 | 1.00 | sharing/dedupe; new part + repoint; drop old rel only if no other blip uses it |
| docx-imgrep-03 | image-replace, table-locate | 2 | 2 | 2 | 1.00 | pt2 now present: rId belongs to that header part's rels; default/first/even headers and footers; wp:anchor not in inline_shapes; drop old rel if unused (baseline 0.50) |
| docx-imgins-01 | image-insert | 3 | 2 | 2 | 1.00 | `paragraph.add_run().add_picture(path, width=…)`, width only |
| docx-imgins-02 | image-insert, table-relayout | 2 | 2 | 2 | 1.00 | overflow; width ≤ cell, accounting for cell margins; autofit column widening not mentioned (minor, as baseline) |
| docx-legacy-01 | legacy-doc | 3 | 2 | 2 | 1.00 | pt2 now correct: LibreOffice headless only, verify tables/rows/images, keep original, convert back only if asked; Pandoc/textutil avoided (baseline 0.50). "OLE2" / "renaming doesn't help" not stated (minor) |
| docx-legacy-02 | legacy-doc, verify-safety | 2 | 2 | 2 | 1.00 | now falls back to LibreOffice when tables are flattened |
| docx-legacy-03 | legacy-doc | 1 | 2 | 2 | 1.00 | separate UserInstallation + `--outdir`, check the expected output path |
| docx-verify-01 | verify-safety | 3 | 2 | 2 | 1.00 | backup + new filename; reopen and assert; compare against a saved baseline |
| docx-verify-02 | verify-safety, docx-model | 2 | 2 | 2 | 1.00 | element order, tc without p, missing tblGrid, gridSpan mismatch, dangling rIds, orphan vMerge; duplicate docPr ids not named (minor, as baseline) |
| docx-verify-03 | verify-safety, image-replace | 2 | 2 | 2 | 1.00 | rel + media remain; drop rel when unreferenced; list/hash zip media |

`normalized = min(awarded, full) / full`

## Per-tag subscores

| tag | baseline | with-skill | delta | n | trust | target? |
|-----|---------:|-----------:|------:|--:|-------|---------|
| docx-model | 0.85 | 0.94 | +0.09 | 7 | ok | **yes** |
| table-locate | 0.75 | 0.89 | +0.14 | 6 | ok | **yes** |
| row-delete | 0.72 | 0.83 | +0.11 | 4 | ok | **yes** |
| cell-edit | 0.50 | 1.00 | +0.50 | 4 | ok | **yes** |
| table-relayout | 0.71 | 1.00 | +0.29 | 6 | ok | **yes** |
| table-insert | 0.86 | 1.00 | +0.14 | 3 | low-n | **yes** |
| image-replace | 0.78 | 0.89 | +0.11 | 4 | ok | **yes** |
| image-insert | 1.00 | 1.00 | 0.00 | 2 | low-n | no |
| legacy-doc | 0.75 | 1.00 | +0.25 | 3 | low-n | **yes** |
| verify-safety | 0.92 | 1.00 | +0.08 | 6 | ok | no |

`subscore = Σ(normalized×weight) / Σ(weight)` over that tag's questions.

- docx-model: (3+3+2+2+2+0.667×3+2) / 17 = 16 / 17 = 0.941
- table-locate: (3+0.5×3+2+2+2+2) / 14 = 12.5 / 14 = 0.893
- row-delete: (0.5×3+2+2+2) / 9 = 7.5 / 9 = 0.833
- cell-edit: 9 / 9
- table-relayout: 14 / 14
- table-insert: 7 / 7
- image-replace: (0.667×3+2+2+2) / 9 = 8 / 9 = 0.889
- image-insert: 5 / 5
- legacy-doc: 6 / 6
- verify-safety: 13 / 13

## Stack score

```
stack_score = Σ(normalized×weight) / Σ(weight) = 65 / 69 = 94.2%
```

(Σweight = 69. Lost 1.5 on docx-locate-01, 1.5 on docx-rowdel-01, 1 on
docx-imgrep-01.) Baseline 54 / 69 = 78.3%.

## Target-tag verdict

| tag | baseline | with-skill | pass (≥ 0.9)? |
|-----|---------:|-----------:|---------------|
| docx-model | 0.85 | 0.94 | PASS |
| table-locate | 0.75 | 0.89 | **FAIL** |
| row-delete | 0.72 | 0.83 | **FAIL** |
| cell-edit | 0.50 | 1.00 | PASS |
| table-relayout | 0.71 | 1.00 | PASS |
| table-insert (low-n) | 0.86 | 1.00 | PASS |
| image-replace | 0.78 | 0.89 | **FAIL** |
| legacy-doc (low-n) | 0.75 | 1.00 | PASS |

**FAIL.** Five of eight target tags pass; three remain just under 0.9. Every
baseline error the skill names is gone (first-run rewrite, `fullCalcOnLoad`,
"turn off trackRevisions", mid-grid column insertion, `add_table` widths, Pandoc
/ textutil, header-part rels), but three single-point omissions keep tags below:

- **table-locate 0.89** — docx-locate-01: the answer never finds the "Gadget D" row
  by cell text or asserts exactly one match (baseline got this right; resampling
  regression, not a skill-addressed construct). The skill has no line on it.
- **row-delete 0.83** — docx-rowdel-01: "Word reflows the remaining rows" is still
  missing despite digest item 3. This is a judgment call: the point's "update
  totals" half is present. Awarding it would make row-delete 1.00, but the
  verdict stays FAIL on the other two tags either way.
- **image-replace 0.89** — docx-imgrep-01: still no "drop the old rel/media" inside
  the replace recipe, although the model applies it in imgrep-02, imgrep-03 and
  verify-03. Same pattern as baseline: it knows the cleanup when asked directly
  but does not finish the replace with it.

Per HOW-TO-EVALUATE.md the iterate loop applies: sharpen the image-replace step
("a replace is not finished until the old rel is dropped") and the row-delete
reflow line, and consider a table-locate line for "find the row by cell text,
assert exactly one match".

The answers reuse the skill's phrasing ("first covered run", "owning part",
"recompute every dependent amount"). Per HOW-TO-EVALUATE.md ("Overlap ≠ cheating")
this is the absorption the run measures; the prompt carried no answer key and the
session made no tool calls.

Non-target drift: none downward. verify-safety rose 0.92 → 1.00 (cell-03 now
resolves revisions); image-insert held at 1.00.
