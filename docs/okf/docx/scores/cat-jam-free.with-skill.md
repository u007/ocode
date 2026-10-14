---
model_id: cat-jam-free
model_version: "alpha"
evaluated_via: opencode-go
evaluated_on: 2026-10-14
stack: docx
stack_corpus_rev: 1
threshold: 0.9
validation: with-skill
iteration: 4
---

# Scorecard — cat-jam-free on docx (with-skill validation, iteration 4)

> Valid ONLY for `cat-jam-free` @ `alpha`. A version bump invalidates
> this scorecard — re-benchmark.

> **WITH-SKILL VALIDATION RUN, ITERATION 4.** Answers: `docx/answers/cat-jam-free.with-skill.md`,
> produced closed-book with the sharpened derived skill
> `derived/docx.cat-jam-free.SKILL.md` (`docx-tuning-cat-jam-free`) prepended to
> the answerer prompt as active guidance. Baseline: `scores/cat-jam-free.md`.
> Iterations 1–3: `scores/cat-jam-free.iter{1,2,3}.with-skill.md` (answers under the
> same names in `answers/`). No new derived skill is produced from this run.

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
1.2.0), with the same strictness as the baseline and iterations 1–3: a `point` whose
core concept is present earns full credit (minor missing sub-items are noted); a
`point` that is absent, only name-dropped, or whose only concrete mechanism is
materially wrong earns 0. Points are whole; fractions come only from a listed partial.

## History

- Iteration 1: 94.2%, **FAIL**: table-locate 0.89, row-delete 0.83 (rowdel-01: no
  "Word reflows"), image-replace 0.89.
- Iteration 2: 96.4%, **FAIL**: cell-edit 0.83 (cell-01: diagnosis without the fix).
- Iteration 3: 97.8%, **FAIL**: row-delete 0.83 (rowdel-01: "recompute totals"
  present, "Word reflows" absent).
- Skill sharpened for iteration 4: new digest item 3b, a fixed sentence to say on
  every row delete ("Remove the `w:tr`; Word reflows the remaining rows itself, so
  nothing is shifted manually; then recompute every dependent total.").

## Per-question results

| id | tags | weight | full | awarded | normalized | notes |
|----|------|-------:|-----:|--------:|-----------:|-------|
| docx-model-01 | docx-model | 3 | 2 | 2 | 1.00 | OPC ZIP of XML parts + relationships, main story in word/document.xml (or header/footer part); tbl → tr → tc → p → r → t, tblPr/tblGrid named. `w:gridCol` not named and tblGrid given only as "table-level metadata" (minor; if pt2 were withheld docx-model would read 0.91, still passing) |
| docx-model-02 | docx-model, table-locate | 3 | 2 | 2 | 1.00 | split runs ("Wid" + "get C"); match on paragraph.text by character span, write into first covered run, blank the rest, never assign paragraph.text or rebuild runs. No reason for the split (minor, as iter3) |
| docx-model-03 | docx-model, table-relayout | 2 | 2 | 2 | 1.00 | twips/dxa 20/pt 1440/in; EMU 12700/pt 914400/in |
| docx-locate-01 | table-locate | 3 | 2 | 2 | 1.00 | body + header/footer + nested tables; identify by header labels not position; exact "Gadget D" cell match; assert exactly one table and one row, else stop and report |
| docx-locate-02 | table-locate, docx-model | 2 | 2 | 2 | 1.00 | one entry per grid column, merged cell returned repeatedly ("same _Cell" only implied, minor); fix: "compare w:tc identities/gridSpan when deleting or counting" (dedupe by tc + gridSpan) |
| docx-rowdel-01 | row-delete | 3 | 2 | 2 | 1.00 | pt1 ok (no public delete API; `tr.getparent().remove(tr)`). pt2 awarded (judgment): the lead concept "Word reflows the remaining rows without manual shifting" is now present; the "update totals" sub-item is absent from this answer (minor sub-item; the totals step appears in rowdel-02/-03). This is the mirror of iter3, where totals were present and the reflow lead was absent (withheld). The digest 3b sentence was only half reproduced. If pt2 were withheld, row-delete would read 0.83 |
| docx-rowdel-02 | row-delete, table-locate | 2 | 2 | 2 | 1.00 | orphaned continuation cells, invalid / Word repair; copy paragraphs into next cell, make it the restart, drop vMerge if no continuation, then delete |
| docx-rowdel-03 | row-delete, cell-edit | 2 | 2 | 2 | 1.00 | cached value, Word does not recompute on open; write into result run between separate/end, keep field and number format; updateFields only prompts |
| docx-rowdel-04 | row-delete, verify-safety | 2 | 2 | 2 | 1.00 | w:tr stays: extractable, counted, shown after unhide, affects totals; remove w:tr; verify row count dropped, no cell text matches, w:tr absent in saved XML/zip. Search of header/footer parts not explicit (minor) |
| docx-cell-01 | cell-edit | 3 | 2 | 2 | 1.00 | bold/size/colour/font lost, tcPr shading/width survive; `cell.paragraphs[0].runs[0].text = "SKU"`, blank other runs, or copy rPr |
| docx-cell-02 | cell-edit, table-locate | 2 | 2 | 2 | 1.00 | Amount, subtotal/Total, tax, grand total, SUM cached results, body figures; recompute from table data, existing number format/decimals/currency/alignment |
| docx-cell-03 | cell-edit, verify-safety | 2 | 2 | 2 | 1.00 | direct rewrite untracked, w:delText can hold the old value; decide tracked vs clean, accept/reject w:ins/w:del, confirm no w:delText holds the old value |
| docx-relayout-01 | table-relayout | 3 | 3 | 3 | 1.00 | appends after Total, no trPr/shading/run formatting; deepcopy + `total_tr.addprevious`; text via existing first run; banding kept |
| docx-relayout-02 | table-relayout | 3 | 3 | 3 | 1.00 | appends on the right and widens; gridCol after Item's + deep copy of Item's tc per row, gridSpan cells; copy header/body formatting; rebalance gridCol + tcW to page_width − margins |
| docx-relayout-03 | table-relayout, docx-model | 2 | 2 | 2 | 1.00 | tcW/tblW also define width; update gridCol + every tcW + tblW; tblLayout consistent (fixed layout implied, minor) |
| docx-tblins-01 | table-insert | 3 | 2 | 2 | 1.00 | appends at end of body; `paragraph._p.addnext(table._tbl)` |
| docx-tblins-02 | table-insert | 2 | 2 | 2 | 1.00 | style looked up in the document's styles part → KeyError; "Table Grid", copy style from a template, or w:tblBorders. No `doc.styles` listing (minor, as baseline) |
| docx-tblins-03 | table-insert, table-relayout | 2 | 2 | 2 | 1.00 | text width split evenly; copy style, gridCol, tcW, tblW, alignment, header run formatting |
| docx-imgrep-01 | image-replace, docx-model | 3 | 3 | 3 | 1.00 | drawing → blip r:embed → owning part's rels → media part; `part.get_or_add_image`, repoint this blip, keep wp:extent/a:ext; drop old rel if no other r:embed in that part uses it. Aspect adjustment not mentioned (minor) |
| docx-imgrep-02 | image-replace | 2 | 2 | 2 | 1.00 | deduplicated/shared parts, `_blob` overwrite hits every picture; new part + repoint only the target blip. SHA1 and "check references first" not named (minor, as iter3) |
| docx-imgrep-03 | image-replace, table-locate | 2 | 2 | 2 | 1.00 | header parts (default/first/even), wp:anchor not in inline_shapes; walk every header/footer, use that header part's own rels |
| docx-imgins-01 | image-insert | 3 | 2 | 2 | 1.00 | add_picture appends a new paragraph at the end; new paragraph moved after the target or a run after it; width only. `run.add_picture` only implied (minor, as iter3) |
| docx-imgins-02 | image-insert, table-relayout | 2 | 2 | 2 | 1.00 | overflows / distorts layout; width ≤ cell width, "slightly less for padding" (≈ minus margins), height proportional |
| docx-legacy-01 | legacy-doc | 3 | 2 | 2 | 1.00 | binary OLE2, python-docx cannot open; LibreOffice headless with --outdir, verify tables/rows/images, convert back only if required, keep original. Renaming not stated (minor) |
| docx-legacy-02 | legacy-doc, verify-safety | 2 | 2 | 2 | 1.00 | textutil flattens tables; compare table/row/cell/image counts; discard and use LibreOffice |
| docx-legacy-03 | legacy-doc | 1 | 2 | 2 | 1.00 | running instance locks the profile; unique UserInstallation, --outdir, verify output exists |
| docx-verify-01 | verify-safety | 3 | 2 | 2 | 1.00 | new or backup-protected file; well-formedness, reopen, exact target row/text/counts/totals/fields/formats/images/rels, render in LibreOffice. "Unchanged remainder" not explicit (minor, as baseline) |
| docx-verify-02 | verify-safety, docx-model | 2 | 2 | 2 | 1.00 | tc without w:p, gridSpan sums vs tblGrid, orphaned vMerge; leftover/removed relationship rIds (dangling rIds). Schema order, docPr ids, "python-docx doesn't validate" not named (minor) |
| docx-verify-03 | verify-safety, image-replace | 2 | 2 | 2 | 1.00 | borderline, same judgment as iter3: answers as if the blip had been repointed rather than the drawing removed and never says outright that removing the drawing leaves rel + media behind; but makes dropping the old rel a required step, scans all parts for the old rId, lists the package and removes orphaned media. If pt1 were withheld, image-replace would read 0.89 |

`normalized = min(awarded, full) / full`

## Per-tag subscores

| tag | baseline | iter1 | iter2 | iter3 | with-skill (iter4) | delta vs baseline | n | trust | target? |
|-----|---------:|------:|------:|------:|-------------------:|------------------:|--:|-------|---------|
| docx-model | 0.85 | 0.94 | 0.94 | 1.00 | 1.00 | +0.15 | 7 | ok | **yes** |
| table-locate | 0.75 | 0.89 | 0.93 | 1.00 | 1.00 | +0.25 | 6 | ok | **yes** |
| row-delete | 0.72 | 0.83 | 1.00 | 0.83 | 1.00 | +0.28 | 4 | ok | **yes** |
| cell-edit | 0.50 | 1.00 | 0.83 | 1.00 | 1.00 | +0.50 | 4 | ok | **yes** |
| table-relayout | 0.71 | 1.00 | 1.00 | 1.00 | 1.00 | +0.29 | 6 | ok | **yes** |
| table-insert | 0.86 | 1.00 | 1.00 | 1.00 | 1.00 | +0.14 | 3 | low-n | **yes** |
| image-replace | 0.78 | 0.89 | 1.00 | 1.00 | 1.00 | +0.22 | 4 | ok | **yes** |
| image-insert | 1.00 | 1.00 | 1.00 | 1.00 | 1.00 | 0.00 | 2 | low-n | no |
| legacy-doc | 0.75 | 1.00 | 1.00 | 1.00 | 1.00 | +0.25 | 3 | low-n | **yes** |
| verify-safety | 0.92 | 1.00 | 1.00 | 1.00 | 1.00 | +0.08 | 6 | ok | no |

`subscore = Σ(normalized×weight) / Σ(weight)` over that tag's questions. Every
question scored 1.00, so every tag is Σweight / Σweight:

- docx-model: 17 / 17; table-locate: 14 / 14; row-delete: 9 / 9; cell-edit: 9 / 9;
  table-relayout: 14 / 14; table-insert: 7 / 7; image-replace: 9 / 9;
  image-insert: 5 / 5; legacy-doc: 6 / 6; verify-safety: 13 / 13.

## Stack score

```
stack_score = Σ(normalized×weight) / Σ(weight) = 69 / 69 = 100.0%
```

Baseline 54 / 69 = 78.3%; iteration 1 65 / 69 = 94.2%; iteration 2 66.5 / 69 = 96.4%;
iteration 3 67.5 / 69 = 97.8%.

## Target-tag verdict

| tag | baseline | iter1 | iter2 | iter3 | iter4 | pass (≥ 0.9)? |
|-----|---------:|------:|------:|------:|------:|---------------|
| docx-model | 0.85 | 0.94 | 0.94 | 1.00 | 1.00 | PASS |
| table-locate | 0.75 | 0.89 | 0.93 | 1.00 | 1.00 | PASS |
| row-delete | 0.72 | 0.83 | 1.00 | 0.83 | 1.00 | PASS |
| cell-edit | 0.50 | 1.00 | 0.83 | 1.00 | 1.00 | PASS |
| table-relayout | 0.71 | 1.00 | 1.00 | 1.00 | 1.00 | PASS |
| table-insert (low-n) | 0.86 | 1.00 | 1.00 | 1.00 | 1.00 | PASS |
| image-replace | 0.78 | 0.89 | 1.00 | 1.00 | 1.00 | PASS |
| legacy-doc (low-n) | 0.75 | 1.00 | 1.00 | 1.00 | 1.00 | PASS |

**PASS.** All eight target tags reach the 0.9 threshold in a single sample, for the
first time across the four iterations.

Caveats, read honestly:

- **The pass rests on one judgment call.** docx-rowdel-01 now states the reflow
  half ("Word reflows the remaining rows without manual shifting") but drops the
  "recompute totals" half that the new digest sentence 3b also asks for. Under the
  rule used throughout (core concept present → full credit, missing sub-item
  noted) the reflow statement is the lead concept of the point, so it is awarded;
  iter3 withheld the mirror case for that reason. A stricter reading that demands
  both halves would leave row-delete at 0.83 and the run a FAIL. The model still
  reproduces the 3b sentence only partially: it answers the literal question and
  treats the second clause as optional.
- **docx-verify-03 is awarded on the same borderline as iter3**; withheld, it would
  put image-replace at 0.89 (target FAIL).
- Four iterations each passed seven or more targets; each previous miss was one
  half of a two-part directive on one question. This pass shows the iter4 wording
  can pass all targets in one sample, not that it does so reliably. A second
  confirming sample would make the result firmer.

The answers reuse the skill's phrasing ("first run covering that span", "owning
part", "dependent totals"). Per HOW-TO-EVALUATE.md ("Overlap ≠ cheating") this is the
absorption the run measures. The prompt carried no answer key and the session made
no tool calls.

Non-target drift: none. verify-safety 1.00 (baseline 0.92), image-insert 1.00.
