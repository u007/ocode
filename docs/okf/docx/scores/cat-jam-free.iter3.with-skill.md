---
model_id: cat-jam-free
model_version: "alpha"
evaluated_via: opencode-go
evaluated_on: 2026-10-14
stack: docx
stack_corpus_rev: 1
threshold: 0.9
validation: with-skill
iteration: 3
---

# Scorecard — cat-jam-free on docx (with-skill validation, iteration 3)

> Valid ONLY for `cat-jam-free` @ `alpha`. A version bump invalidates
> this scorecard — re-benchmark.

> **WITH-SKILL VALIDATION RUN, ITERATION 3.** Answers: `docx/answers/cat-jam-free.with-skill.md`,
> produced closed-book with the sharpened derived skill
> `derived/docx.cat-jam-free.SKILL.md` (`docx-tuning-cat-jam-free`) prepended to
> the answerer prompt as active guidance. Baseline: `scores/cat-jam-free.md`.
> Iteration 1: `scores/cat-jam-free.iter1.with-skill.md`; iteration 2:
> `scores/cat-jam-free.iter2.with-skill.md` (answers under the same names in
> `answers/`). No new derived skill is produced from this run.

> Target tags for this skill (baseline < 0.9): **docx-model (0.85), table-locate (0.75),
> row-delete (0.72), cell-edit (0.50), table-relayout (0.71), table-insert (0.86, low-n),
> image-replace (0.78), legacy-doc (0.75, low-n)**.

## Provenance

Answers: `docx/answers/cat-jam-free.with-skill.md`, produced closed-book by
`ocode run -m opencode-go/cat-jam-free -effort med` with the SKILL.md body (front
matter stripped) prepended to the `_prompts/docx.md` sheet, in an isolated HOME with
every tool denied and a cwd holding only the sheet. The session sqlite shows exactly
1 user + 1 assistant message and 0 tool calls; no answer key in the prompt. The model
emitted the YAML list twice, byte-identical, separated by one leaked thinking line;
the harvester kept one copy. All 29 ids are present, with no extras or duplicates.

Graded against `docx/questions.yaml` corpus_rev 1 (python-docx facts verified on
1.2.0), with the same strictness as the baseline and iterations 1–2: a `point` whose
core concept is present earns full credit (minor missing sub-items are noted); a
`point` that is absent, only name-dropped, or whose only concrete mechanism is
materially wrong earns 0. Points are whole; fractions come only from a listed partial.

## History

- Iteration 1 (`scores/cat-jam-free.iter1.with-skill.md`): 94.2%, **FAIL**:
  table-locate 0.89, row-delete 0.83 (rowdel-01: no "Word reflows"), image-replace 0.89.
- Iteration 2 (`scores/cat-jam-free.iter2.with-skill.md`): 96.4%, **FAIL**:
  cell-edit 0.83 (cell-01: diagnosis without the fix).
- Skill sharpened for iteration 3: digest item 1 now opens "Whenever you explain what
  `cell.text`/`paragraph.text` assignment loses, give the fix in the same answer."

## Per-question results

| id | tags | weight | full | awarded | normalized | notes |
|----|------|-------:|-----:|--------:|-----------:|-------|
| docx-model-01 | docx-model | 3 | 2 | 2 | 1.00 | ZIP/OPC package of XML parts + media + relationship files; word/document.xml or header/footer part; full tbl/tblPr/tblGrid/gridCol/tr/tc/p/r/t. "Parts linked by rels" only implied (minor) |
| docx-model-02 | docx-model, table-locate | 3 | 2 | 2 | 1.00 | split runs; match on paragraph.text by offsets, write into first covered run, blank the rest, never rebuild runs. No reason for the split (minor) |
| docx-model-03 | docx-model, table-relayout | 2 | 2 | 2 | 1.00 | dxa 1440/in 20/pt; EMU 914400/in 12700/pt |
| docx-locate-01 | table-locate | 3 | 2 | 2 | 1.00 | not by index; body + header/footer + nested tables; identify by header labels/captions; Gadget D row by exact cell text, assert exactly one table and one row, else stop |
| docx-locate-02 | table-locate, docx-model | 2 | 2 | 2 | 1.00 | one `_Cell` per grid column, same `w:tc` repeated; "count or address columns from `w:tc` elements and `gridSpan`, not from `row.cells`" (tc_lst + gridSpan). Recovered from iter2 0.50 |
| docx-rowdel-01 | row-delete | 3 | 2 | 1 | 0.50 | pt1 ok (no delete API; `tr.getparent().remove(tr)`). pt2 withheld (same judgment as iter1): "recompute all dependent numbers" is present but the lead concept of the point, "Word reflows, no gap to close manually", is absent, although digest item 3 says "always say both halves". Iter2 had both halves (1.00); iter1 had the same omission (0.50) |
| docx-rowdel-02 | row-delete, table-locate | 2 | 2 | 2 | 1.00 | dangling vMerge continuations; move paragraphs down, promote next cell to restart, drop vMerge if no continuation |
| docx-rowdel-03 | row-delete, cell-edit | 2 | 2 | 2 | 1.00 | Word does not recompute on open; write cached result between `separate`/`end`; updateFields only prompts |
| docx-rowdel-04 | row-delete, verify-safety | 2 | 2 | 2 | 1.00 | hidden/white/zero-height row keeps its `w:tr` (indexing, export, accessibility); reopen, check row count and XML. No search of all parts for the old text (minor, as baseline) |
| docx-cell-01 | cell-edit | 3 | 2 | 2 | 1.00 | pt1 ok (bold/size/colour/font lost; tcPr shading/borders/width survive). pt2 now present: `cell.paragraphs[0].runs[0].text = "SKU"`, clear the other runs, keep `rPr` (iter2 0.50, baseline 0.50) |
| docx-cell-02 | cell-edit, table-locate | 2 | 2 | 2 | 1.00 | Amount, Total, subtotal/tax/grand total, counts, text figures; existing number format/decimals/separators/currency/alignment; field cached results |
| docx-cell-03 | cell-edit, verify-safety | 2 | 2 | 2 | 1.00 | direct edit untracked; existing w:ins/w:del incl. w:delText; decide tracked vs clean, accept/reject, verify no w:delText holds the old value |
| docx-relayout-01 | table-relayout | 3 | 3 | 3 | 1.00 | appends at end, no trPr/shading/run formatting; deepcopy + `total_tr.addprevious`; text through first run; alternating shading kept |
| docx-relayout-02 | table-relayout | 3 | 3 | 3 | 1.00 | right-end append widens table; gridCol after Item's + deep copy of Item's tc per row, gridSpan/merged Total; copy header/body formatting; rebalance gridCol + tcW to text width |
| docx-relayout-03 | table-relayout, docx-model | 2 | 2 | 2 | 1.00 | tcW/tblW override gridCol, autofit vs fixed; update all three consistently, set layout explicitly |
| docx-tblins-01 | table-insert | 3 | 2 | 2 | 1.00 | appends at end; `notes_paragraph._p.addnext(table._tbl)` |
| docx-tblins-02 | table-insert | 2 | 2 | 2 | 1.00 | style absent from the styles part → KeyError; existing style / copy an existing table / manual borders. No `doc.styles` listing (minor, as baseline) |
| docx-tblins-03 | table-insert, table-relayout | 2 | 2 | 2 | 1.00 | text width split evenly; copy style, tblGrid, tcW, tblW, alignment, header run formatting |
| docx-imgrep-01 | image-replace, docx-model | 3 | 3 | 3 | 1.00 | full drawing → blip r:embed → owning part's rels → word/media chain; `get_or_add_image`, repoint only this blip, keep extents / adjust aspect; drop old rel if no other blip in the part uses it |
| docx-imgrep-02 | image-replace | 2 | 2 | 2 | 1.00 | shared/deduplicated parts, `_blob` overwrite hits every reference; new part + repoint. SHA1 and "check references first" not named (minor) |
| docx-imgrep-03 | image-replace, table-locate | 2 | 2 | 2 | 1.00 | header part, default/first/even, `wp:anchor` not in inline_shapes; walk every header/footer, use that part's own rels |
| docx-imgins-01 | image-insert | 3 | 2 | 2 | 1.00 | add_picture appends at end; picture run in a paragraph moved after the anchor via `addnext`, width only. How the run is created (`run.add_picture`) only implied (minor) |
| docx-imgins-02 | image-insert, table-relayout | 2 | 2 | 2 | 1.00 | overflow / disrupts layout; width ≤ cell minus margins, width only |
| docx-legacy-01 | legacy-doc | 3 | 2 | 2 | 1.00 | binary OLE2, python-docx cannot open; LibreOffice headless, keep original, convert back only if needed, verify tables/rows/images. Renaming not stated (minor) |
| docx-legacy-02 | legacy-doc, verify-safety | 2 | 2 | 2 | 1.00 | textutil flattens tables; check table/row/cell/image counts; fall back to LibreOffice headless |
| docx-legacy-03 | legacy-doc | 1 | 2 | 2 | 1.00 | running instance / locked profile; unique UserInstallation; check output exists. `--outdir` not named (minor) |
| docx-verify-01 | verify-safety | 3 | 2 | 2 | 1.00 | keep original, new file; reopen and validate counts/text/widths/fields/images; zip integrity, open in Word. "Unchanged remainder" not explicit (minor, as baseline) |
| docx-verify-02 | verify-safety, docx-model | 2 | 2 | 2 | 1.00 | cell count vs grid, gridSpan, dangling vMerge, cell without w:p; misplaced elements (schema order), unmatched fldChar. Dangling rIds, docPr ids and "python-docx doesn't validate" not named (minor) |
| docx-verify-03 | verify-safety, image-replace | 2 | 2 | 2 | 1.00 | borderline (judgment, awarded as the baseline awarded "knows the orphan media/rel must be removed"): answers as if the image had been repointed rather than the drawing removed, and never states outright that removing the drawing leaves rel + media behind; but makes dropping the old rel a required step and verifies via the old rId search and the media listing. If pt1 were withheld, image-replace would read 0.89 |

`normalized = min(awarded, full) / full`

## Per-tag subscores

| tag | baseline | iter1 | iter2 | with-skill (iter3) | delta vs baseline | n | trust | target? |
|-----|---------:|------:|------:|-------------------:|------------------:|--:|-------|---------|
| docx-model | 0.85 | 0.94 | 0.94 | 1.00 | +0.15 | 7 | ok | **yes** |
| table-locate | 0.75 | 0.89 | 0.93 | 1.00 | +0.25 | 6 | ok | **yes** |
| row-delete | 0.72 | 0.83 | 1.00 | 0.83 | +0.11 | 4 | ok | **yes** |
| cell-edit | 0.50 | 1.00 | 0.83 | 1.00 | +0.50 | 4 | ok | **yes** |
| table-relayout | 0.71 | 1.00 | 1.00 | 1.00 | +0.29 | 6 | ok | **yes** |
| table-insert | 0.86 | 1.00 | 1.00 | 1.00 | +0.14 | 3 | low-n | **yes** |
| image-replace | 0.78 | 0.89 | 1.00 | 1.00 | +0.22 | 4 | ok | **yes** |
| image-insert | 1.00 | 1.00 | 1.00 | 1.00 | 0.00 | 2 | low-n | no |
| legacy-doc | 0.75 | 1.00 | 1.00 | 1.00 | +0.25 | 3 | low-n | **yes** |
| verify-safety | 0.92 | 1.00 | 1.00 | 1.00 | +0.08 | 6 | ok | no |

`subscore = Σ(normalized×weight) / Σ(weight)` over that tag's questions.

- docx-model: (3+3+2+2+2+3+2) / 17 = 17 / 17
- table-locate: (3+3+2+2+2+2) / 14 = 14 / 14
- row-delete: (0.5×3+2+2+2) / 9 = 7.5 / 9 = 0.833
- cell-edit: (2+3+2+2) / 9 = 9 / 9
- table-relayout: (2+3+3+2+2+2) / 14 = 14 / 14
- table-insert: 7 / 7
- image-replace: (3+2+2+2) / 9 = 9 / 9
- image-insert: 5 / 5
- legacy-doc: 6 / 6
- verify-safety: 13 / 13

## Stack score

```
stack_score = Σ(normalized×weight) / Σ(weight) = 67.5 / 69 = 97.8%
```

(Σweight = 69. Lost 1.5 on docx-rowdel-01 only.) Baseline 54 / 69 = 78.3%;
iteration 1 65 / 69 = 94.2%; iteration 2 66.5 / 69 = 96.4%.

## Target-tag verdict

| tag | baseline | iter1 | iter2 | iter3 | pass (≥ 0.9)? |
|-----|---------:|------:|------:|------:|---------------|
| docx-model | 0.85 | 0.94 | 0.94 | 1.00 | PASS |
| table-locate | 0.75 | 0.89 | 0.93 | 1.00 | PASS |
| row-delete | 0.72 | 0.83 | 1.00 | 0.83 | **FAIL** |
| cell-edit | 0.50 | 1.00 | 0.83 | 1.00 | PASS |
| table-relayout | 0.71 | 1.00 | 1.00 | 1.00 | PASS |
| table-insert (low-n) | 0.86 | 1.00 | 1.00 | 1.00 | PASS |
| image-replace | 0.78 | 0.89 | 1.00 | 1.00 | PASS |
| legacy-doc (low-n) | 0.75 | 1.00 | 1.00 | 1.00 | PASS |

**FAIL.** Seven of eight target tags pass. The iteration-3 sharpening worked for its
target: docx-cell-01 now gives the first-run fix, and cell-edit is back to 1.00.

- **row-delete 0.83**: docx-rowdel-01 gives the XML removal and "recompute all
  dependent numbers", but drops the "Word reflows the remaining rows, no manual
  shifting" half, which digest item 3 explicitly demands ("always say both halves").
  Iteration 2 stated it, so this is the same sampling flip seen on cell-01 in
  iteration 2: the model answers the literal question ("is there a delete
  method?") and omits the guidance it treats as optional. The directive sits at the
  end of a long item about dependent numbers. One possible tweak: give the row-delete
  answer its own digest line with both halves as one fixed sentence ("remove `row._tr`;
  Word reflows the rows below by itself; then recompute every dependent total").

Across the three iterations, every target tag has reached ≥ 0.9 at least once, and
each miss is a single question in which the model leaves out one half of a two-part
directive that the skill already states. Each iteration has fallen short on exactly
one question, so no wording has yet shown it can pass all targets in one sample.

The answers reuse the skill's phrasing ("first covered run", "owning part",
"dependent numbers"). Per HOW-TO-EVALUATE.md ("Overlap ≠ cheating") this is the
absorption the run measures. The prompt carried no answer key and the session made
no tool calls.

Non-target drift: none. verify-safety 1.00 (baseline 0.92), image-insert 1.00.
docx-locate-02 recovered its dedupe fix (iter2 0.50 → 1.00), confirming that the
iteration-2 dip was resampling noise.
