---
model_id: cat-jam-free
model_version: "alpha"
evaluated_via: opencode-go
evaluated_on: 2026-10-14
stack: docx
stack_corpus_rev: 1
threshold: 0.9
validation: with-skill
iteration: 2
---

# Scorecard — cat-jam-free on docx (with-skill validation, iteration 2)

> Valid ONLY for `cat-jam-free` @ `alpha`. A version bump invalidates
> this scorecard — re-benchmark.

> **WITH-SKILL VALIDATION RUN — ITERATION 2.** Answers: `docx/answers/cat-jam-free.with-skill.md`,
> produced closed-book with the sharpened derived skill
> `derived/docx.cat-jam-free.SKILL.md` (`docx-tuning-cat-jam-free`) prepended to
> the answerer prompt as active guidance. Baseline: `scores/cat-jam-free.md`.
> Iteration 1: `scores/cat-jam-free.with-skill.iter1.md` (answers
> `answers/cat-jam-free.with-skill.iter1.md`). No new derived skill is produced
> from this run.

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
1.2.0), with the same strictness as the baseline and iteration 1: a `point` whose
core concept is present earns full credit (minor missing sub-items are noted); a
`point` that is absent, only name-dropped, or whose only concrete mechanism is
materially wrong earns 0. Points are whole; fractions come only from a listed partial.

## History

- Iteration 1 (`scores/cat-jam-free.with-skill.iter1.md`): 94.2%, **FAIL** —
  table-locate 0.89 (locate-01: no row-by-text / exactly-one-match), row-delete 0.83
  (rowdel-01: no "Word reflows"), image-replace 0.89 (imgrep-01: old rel/media never
  dropped).
- Skill sharpened for iteration 2: digest item 3 now demands both halves of a row
  delete (reflow + totals), item 7 says every replace ends with the old-rel cleanup,
  and new item 9 covers finding a row by cell text and asserting exactly one match.

## Per-question results

| id | tags | weight | full | awarded | normalized | notes |
|----|------|-------:|-----:|--------:|-----------:|-------|
| docx-model-01 | docx-model | 3 | 2 | 2 | 1.00 | ZIP/OOXML package, word/document.xml, header/footer parts; full tbl/tblGrid/gridCol/tr/tc/p/r/t + Pr elements. Relationships not named (minor, as baseline) |
| docx-model-02 | docx-model, table-locate | 3 | 2 | 2 | 1.00 | split runs; match on paragraph.text by offsets, write into first covered run, blank the rest |
| docx-model-03 | docx-model, table-relayout | 2 | 2 | 2 | 1.00 | dxa 1440/in 20/pt; EMU 914400/in 12700/pt |
| docx-locate-01 | table-locate | 3 | 2 | 2 | 1.00 | pt1 ok (header row/caption, not index; header/footer + nested tables). pt2 now present: Gadget D row by cell text, assert exactly one match, else stop (iter1 0.50) |
| docx-locate-02 | table-locate, docx-model | 2 | 2 | 1 | 0.50 | pt1 ok (one entry per grid column, same cell object repeated). pt2 absent: describes what goes wrong but gives no fix — no dedupe by `_tc`, no `tc_lst`/`findall(w:tc)` + gridSpan. Baseline and iter1 both named a fix (1.00); resampling regression on a construct the skill does not cover |
| docx-rowdel-01 | row-delete | 3 | 2 | 2 | 1.00 | no delete API; `tr.getparent().remove(tr)`; "Word reflows the remaining rows; then recompute every dependent total" — both halves now present (iter1 0.50, baseline 0.50) |
| docx-rowdel-02 | row-delete, table-locate | 2 | 2 | 2 | 1.00 | move paragraphs down, promote to restart, drop vMerge if no continuation |
| docx-rowdel-03 | row-delete, cell-edit | 2 | 2 | 2 | 1.00 | Word does not recompute on open; write cached result between separate/end; updateFields only prompts |
| docx-rowdel-04 | row-delete, verify-safety | 2 | 2 | 2 | 1.00 | tr remains (extraction, counts, calculations); verify XML, row count, text search. Headers/footers not named (minor, as baseline) |
| docx-cell-01 | cell-edit | 3 | 2 | 1 | 0.50 | pt1 ok (run formatting lost, tcPr shading/borders/width survive). pt2 absent: no fix offered — no "write into the first run / clear the others" or "copy rPr", although skill digest item 1 and the cell-edit section both state it. The answer stops at the literal question (what survives / what is lost). Same omission as baseline; iter1 had it (1.00) |
| docx-cell-02 | cell-edit, table-locate | 2 | 2 | 2 | 1.00 | Amount, Total, subtotal/tax/grand total, text figures; existing number format/decimals/currency/alignment through the first run; field cached result |
| docx-cell-03 | cell-edit, verify-safety | 2 | 2 | 2 | 1.00 | untracked direct edit, existing w:ins/w:del; decide tracked vs clean, accept/reject, check w:delText |
| docx-relayout-01 | table-relayout | 3 | 3 | 3 | 1.00 | appends after Total, no trPr/shading/run formatting; deepcopy + `addprevious`; text through first run; alternating shading kept |
| docx-relayout-02 | table-relayout | 3 | 3 | 3 | 1.00 | right-end append widens table; gridCol after Item's + deep copy of Item's tc per row, gridSpan/merged Total; copy header/body formatting; rebalance gridCol + tcW to text width |
| docx-relayout-03 | table-relayout, docx-model | 2 | 2 | 2 | 1.00 | gridCol + every tcW + tblW consistently; fixed layout not mentioned (minor) |
| docx-tblins-01 | table-insert | 3 | 2 | 2 | 1.00 | `paragraph._p.addnext(table._tbl)` |
| docx-tblins-02 | table-insert | 2 | 2 | 2 | 1.00 | style absent from the document's styles part; existing style, import it, or tblBorders. No `doc.styles` listing (minor, as baseline) |
| docx-tblins-03 | table-insert, table-relayout | 2 | 2 | 2 | 1.00 | text width split evenly into explicit gridCol/tcW; copy style, widths, tblW, alignment, header-run formatting |
| docx-imgrep-01 | image-replace, docx-model | 3 | 3 | 3 | 1.00 | pt3 now present: "clean up the old rel if unused" ends the replace recipe (iter1 0.67, baseline 0.67). Owning part's rels, get_or_add_image, keep extents, aspect adjust |
| docx-imgrep-02 | image-replace | 2 | 2 | 2 | 1.00 | shared/deduplicated parts; new part + repoint; drop old rel if unreferenced. SHA1 not named (minor) |
| docx-imgrep-03 | image-replace, table-locate | 2 | 2 | 2 | 1.00 | default/first/even headers, wp:anchor not in inline_shapes; rId in that header/footer part's rels |
| docx-imgins-01 | image-insert | 3 | 2 | 2 | 1.00 | add_picture appends at end; run.add_picture in a paragraph after the anchor, width only |
| docx-imgins-02 | image-insert, table-relayout | 2 | 2 | 2 | 1.00 | overflow; width ≤ cell width, height unset. Cell margins and autofit widening not mentioned (minor, as baseline "fit to the cell's content width") |
| docx-legacy-01 | legacy-doc | 3 | 2 | 2 | 1.00 | python-docx cannot open binary .doc; LibreOffice headless, keep original, verify, convert back only if needed. OLE2 / renaming not stated (minor, as iter1) |
| docx-legacy-02 | legacy-doc, verify-safety | 2 | 2 | 2 | 1.00 | textutil flattens tables; compare counts; fall back to LibreOffice |
| docx-legacy-03 | legacy-doc | 1 | 2 | 2 | 1.00 | profile lock; UserInstallation + `--outdir`; check the output file |
| docx-verify-01 | verify-safety | 3 | 2 | 2 | 1.00 | new file + backup; reopen and assert counts/text/fields/images. Unchanged remainder not explicit (minor, as baseline) |
| docx-verify-02 | verify-safety, docx-model | 2 | 2 | 2 | 1.00 | schema order, gridSpan/vMerge vs cell counts, orphaned/duplicate rel ids; tc-without-p and docPr ids not named (minor) |
| docx-verify-03 | verify-safety, image-replace | 2 | 2 | 2 | 1.00 | drawing removal leaves rel + media; drop unused rel; verify rId gone and hash media |

`normalized = min(awarded, full) / full`

## Per-tag subscores

| tag | baseline | iter1 | with-skill (iter2) | delta vs baseline | n | trust | target? |
|-----|---------:|------:|-------------------:|------------------:|--:|-------|---------|
| docx-model | 0.85 | 0.94 | 0.94 | +0.09 | 7 | ok | **yes** |
| table-locate | 0.75 | 0.89 | 0.93 | +0.18 | 6 | ok | **yes** |
| row-delete | 0.72 | 0.83 | 1.00 | +0.28 | 4 | ok | **yes** |
| cell-edit | 0.50 | 1.00 | 0.83 | +0.33 | 4 | ok | **yes** |
| table-relayout | 0.71 | 1.00 | 1.00 | +0.29 | 6 | ok | **yes** |
| table-insert | 0.86 | 1.00 | 1.00 | +0.14 | 3 | low-n | **yes** |
| image-replace | 0.78 | 0.89 | 1.00 | +0.22 | 4 | ok | **yes** |
| image-insert | 1.00 | 1.00 | 1.00 | 0.00 | 2 | low-n | no |
| legacy-doc | 0.75 | 1.00 | 1.00 | +0.25 | 3 | low-n | **yes** |
| verify-safety | 0.92 | 1.00 | 1.00 | +0.08 | 6 | ok | no |

`subscore = Σ(normalized×weight) / Σ(weight)` over that tag's questions.

- docx-model: (3+3+2+0.5×2+2+3+2) / 17 = 16 / 17 = 0.941
- table-locate: (3+3+0.5×2+2+2+2) / 14 = 13 / 14 = 0.929
- row-delete: (3+2+2+2) / 9 = 9 / 9
- cell-edit: (2+0.5×3+2+2) / 9 = 7.5 / 9 = 0.833
- table-relayout: (2+3+3+2+2+2) / 14 = 14 / 14
- table-insert: 7 / 7
- image-replace: (3+2+2+2) / 9 = 9 / 9
- image-insert: 5 / 5
- legacy-doc: 6 / 6
- verify-safety: 13 / 13

## Stack score

```
stack_score = Σ(normalized×weight) / Σ(weight) = 66.5 / 69 = 96.4%
```

(Σweight = 69. Lost 1.5 on docx-cell-01, 1 on docx-locate-02.) Baseline 54 / 69 =
78.3%; iteration 1 65 / 69 = 94.2%.

## Target-tag verdict

| tag | baseline | iter1 | iter2 | pass (≥ 0.9)? |
|-----|---------:|------:|------:|---------------|
| docx-model | 0.85 | 0.94 | 0.94 | PASS |
| table-locate | 0.75 | 0.89 | 0.93 | PASS |
| row-delete | 0.72 | 0.83 | 1.00 | PASS |
| cell-edit | 0.50 | 1.00 | 0.83 | **FAIL** |
| table-relayout | 0.71 | 1.00 | 1.00 | PASS |
| table-insert (low-n) | 0.86 | 1.00 | 1.00 | PASS |
| image-replace | 0.78 | 0.89 | 1.00 | PASS |
| legacy-doc (low-n) | 0.75 | 1.00 | 1.00 | PASS |

**FAIL.** Seven of eight target tags pass. All three iteration-1 failures are fixed
by the sharpened lines: locate-01 now finds the row by text and asserts one match,
rowdel-01 states both "Word reflows" and "recompute totals", and imgrep-01 ends with
the old-rel cleanup.

- **cell-edit 0.83** — docx-cell-01: the answer lists what `cell.text` loses and
  keeps but gives no fix (write into the first run / copy rPr). The question asks
  only "what survives and what is lost", and the model answered it literally even
  though skill digest item 1 and the cell-edit section both state the fix.
  Iteration 1 included the fix, so this is a sampling flip on a construct the skill
  already covers. The digest phrases the fix as a replace procedure; it never says
  "whenever you name a formatting loss, state the fix in the same answer".

Secondary loss (table-locate and docx-model still pass): docx-locate-02 names the
repeated-cell problem but no dedupe (`_tc` / `tc_lst` + gridSpan). Baseline and
iteration 1 both had it. The skill does not mention merged `row.cells` dedupe at all,
and neither tag is below the threshold, so this is resampling noise, not a skill gap.

Per HOW-TO-EVALUATE.md the iterate loop applies to cell-edit only: make the
cell-text line diagnosis-plus-fix ("when `cell.text` / `paragraph.text` formatting
loss comes up, always give the fix: `runs[0].text = new`, other runs `""`, or copy
`rPr`"). Every other target tag has now reached 0.9 at least once with the current
wording.

The answers reuse the skill's phrasing ("first covered run", "owning part",
"dependent total"). Per HOW-TO-EVALUATE.md ("Overlap ≠ cheating") this is the
absorption the run measures; the prompt carried no answer key and the session made
no tool calls.

Non-target drift: none. verify-safety 1.00 (baseline 0.92), image-insert 1.00.
