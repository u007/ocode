---
model_id: cat-jam-free
model_version: "alpha"
evaluated_via: opencode-go
evaluated_on: 2026-10-14
stack: docx
stack_corpus_rev: 1
threshold: 0.9
---

# Scorecard — cat-jam-free on docx

> Valid ONLY for `cat-jam-free` @ `alpha`. A version bump invalidates
> this scorecard — re-benchmark.

## Provenance

Answers: `docx/answers/cat-jam-free.md`, produced closed-book by
`ocode run -m opencode-go/cat-jam-free -effort med` in an empty scratch directory
with only the `_prompts/docx.md` sheet attached; the session sqlite shows exactly
1 user + 1 assistant message and 0 tool calls. Headless runs inject ocode's bundled
skill-catalogue digest into the system prompt. The model leaked one line of
thinking-aloud after the YAML fence; the harvester excluded it. opencode-go
free/alpha model with no published version; it self-identifies as "CatJam,
developed by LitterBox AI". All 29 ids are present, with no extras or duplicates.

Graded against `docx/questions.yaml` corpus_rev 1 (python-docx facts verified on
1.2.0). Grading rule applied: a `point` whose core concept is present earns full
credit (missing sub-items are noted as minor). A `point` that is only name-dropped
with no mechanism, or whose only concrete mechanism is materially wrong, earns 0.
Answers are terse (2–4 lines each), so most losses are omissions of the second
half of a question rather than wrong facts.

## Per-question results

| id | tags | weight | full | awarded | normalized | notes |
|----|------|-------:|-----:|--------:|-----------:|-------|
| docx-model-01 | docx-model | 3 | 2 | 2 | 1.00 | zip + word/document.xml + full tbl/tblGrid/tr/tc/p/r/t hierarchy; never says other parts are linked by rels (minor) |
| docx-model-02 | docx-model, table-locate | 3 | 2 | 1 | 0.50 | pt1 ok (split across runs; no reason given). pt2 withheld: "collapse/rebuild runs or edit runs by walking the full text … better helpers replace across runs while preserving formatting" names the goal with no mechanism; never says to write the match into the first run and clear the rest so that run's formatting is kept, and "collapse/rebuild" is the formatting-losing route |
| docx-model-03 | docx-model, table-relayout | 2 | 2 | 2 | 1.00 | dxa 20/pt 1440/in; EMU 914400/in 12700/pt; mentions pct |
| docx-locate-01 | table-locate | 3 | 2 | 2 | 1.00 | header-text identification, unique-match check; header/footer tables and nested `cell.tables` not mentioned (minor) |
| docx-locate-02 | table-locate, docx-model | 2 | 2 | 2 | 1.00 | per-grid-column, repeated merged `_Cell`; fix is only "handle merged ranges explicitly via gridSpan", no `_tc` dedupe / `tc_lst` (thin, awarded) |
| docx-rowdel-01 | row-delete | 3 | 2 | 1 | 0.50 | pt1 ok (`row._tr.getparent().remove(row._tr)`). pt2 absent: nothing about Word reflowing the remaining rows, nothing about updating the Total |
| docx-rowdel-02 | row-delete, table-locate | 2 | 2 | 2 | 1.00 | promotes next cell to restart; does not move the restart cell's content down (minor); also offers "remove the whole merged group", which deletes rows nobody asked to delete (noted, not penalised as the correct route is given) |
| docx-rowdel-03 | row-delete, cell-edit | 2 | 2 | 1 | 0.50 | pt1 withheld: recommends `w:calcPr fullCalcOnLoad` in settings.xml to make Word recalc on load. `calcPr`/`fullCalcOnLoad` is SpreadsheetML (Excel workbook.xml), not a Word setting; the claim contradicts the point that Word-on-open does not recompute. The Word mechanism (`w:updateFields`, which prompts) is never named. pt2 ok ("update the cached field result … replace the result text") |
| docx-rowdel-04 | row-delete, verify-safety | 2 | 2 | 2 | 1.00 | tr still in document.xml, counts, exports; verify by reopen + inspect document.xml; does not search headers/footers for the old text (minor) |
| docx-cell-01 | cell-edit | 3 | 2 | 1 | 0.50 | pt1 ok (run formatting lost, tcPr kept). pt2 absent: no fix offered (edit the first run / copy rPr) |
| docx-cell-02 | cell-edit, table-locate | 2 | 2 | 1 | 0.50 | pt1 ok (Amount, Total, tax/discount). pt2 absent: nothing on keeping number format, decimals, currency or alignment; "or set fields to recalc on load" repeats the rowdel-03 non-mechanism |
| docx-cell-03 | cell-edit, verify-safety | 2 | 2 | 1 | 0.50 | pt1 ok (no revision markup; existing revision nodes affected). pt2 withheld: "turn off trackRevisions / download a clean copy, or write w:ins/w:del" — turning tracking off does not resolve the pending revisions already in the cell, and nothing about accepting/rejecting them or checking `w:delText` for the old value |
| docx-relayout-01 | table-relayout | 3 | 3 | 2 | 0.67 | pt1 ok (appends at end; "copies no formatting" not stated, minor). pt2 ok (deepcopy w:tr, `element.insert(index, …)`). pt3 absent: "optionally blank its cell text … then modify the cells" — no write-through-existing-run, no banding |
| docx-relayout-02 | table-relayout | 3 | 3 | 1 | 0.33 | pt1 ok (appends right, adds gridCol + a tc per row). pt2 absent: the question asks for the column AFTER Item; the answer accepts the right-end append and never inserts a gridCol + tc at Item's index (gridSpan not mentioned). pt3 withheld: width rebalance is covered (reduce another column, tcW, tblGrid, fixed layout) but "look like the other columns" is unanswered — no copying of header/body cell formatting |
| docx-relayout-03 | table-relayout, docx-model | 2 | 2 | 2 | 1.00 | |
| docx-tblins-01 | table-insert | 3 | 2 | 2 | 1.00 | `paragraph._p.addnext(table._tbl)` |
| docx-tblins-02 | table-insert | 2 | 2 | 2 | 1.00 | style missing from the document's styles part; "Table Grid" or manual borders; does not say to list `doc.styles` (and "Table Grid" may be absent too) (minor) |
| docx-tblins-03 | table-insert, table-relayout | 2 | 2 | 1 | 0.50 | pt1 withheld: "widths are unset/auto (fills available width approximately)" is wrong — python-docx writes explicit gridCol/tcW widths that split the text width evenly. pt2 ok (copy gridCol/tcW, tblW, style, layout; header formatting not called out, minor) |
| docx-imgrep-01 | image-replace, docx-model | 3 | 3 | 2 | 0.67 | pt1 ok (drawing → a:blip r:embed → document.xml.rels → media; extent). pt2 ok (new part + repoint, keep extent; aspect not mentioned) though it first offers overwriting the part's bytes "if not shared". pt3 absent: old rel / media never removed |
| docx-imgrep-02 | image-replace | 2 | 2 | 2 | 1.00 | sharing explained (no SHA1 dedupe mention, minor); unique part or check sharing first |
| docx-imgrep-03 | image-replace, table-locate | 2 | 2 | 1 | 0.50 | pt1 ok (header/footer part). pt2 withheld: "search section.header/footer XML for a:blip and replace that image part/reference" — never says the rId belongs to the header part's rels, and "replace that image part" again invites a byte overwrite; first-page/even headers and anchored pictures not mentioned |
| docx-imgins-01 | image-insert | 3 | 2 | 2 | 1.00 | |
| docx-imgins-02 | image-insert, table-relayout | 2 | 2 | 2 | 1.00 | overflow named (column-widening under autofit not mentioned, minor); "width=Inches(1.2)" would still overflow by the cell margins, but "or fit to the cell's content width" carries the point |
| docx-legacy-01 | legacy-doc | 3 | 2 | 1 | 0.50 | pt1 ok (binary OLE2; renaming not mentioned). pt2 withheld: "convert with LibreOffice/Pandoc/textutil" lists them as interchangeable — Pandoc cannot read .doc at all and textutil flattens tables (the model's own legacy-02 answer warns of it); no headless invocation, no "keep the original / check the conversion" |
| docx-legacy-02 | legacy-doc, verify-safety | 2 | 2 | 2 | 1.00 | does not say to fall back to LibreOffice/Word (minor) |
| docx-legacy-03 | legacy-doc | 1 | 2 | 2 | 1.00 | separate UserInstallation profile; no `--outdir`, no "check the output file exists" (minor) |
| docx-verify-01 | verify-safety | 3 | 2 | 2 | 1.00 | new file, unzip -t, reopen + assert, keep original; "unchanged remainder" not explicit (minor) |
| docx-verify-02 | verify-safety, docx-model | 2 | 2 | 2 | 1.00 | element order, missing tblGrid, tc without p, gridSpan mismatch, vMerge, relationships; docPr ids not named |
| docx-verify-03 | verify-safety, image-replace | 2 | 2 | 2 | 1.00 | knows the orphan media/rel must be removed; list + hash parts; headers/footers/thumbnail checked |

`normalized = min(awarded, full) / full`

## Per-tag subscores

| tag | subscore | n | trust | action |
|-----|---------:|--:|-------|--------|
| docx-model | 0.85 | 7 | ok | **derive** |
| table-locate | 0.75 | 6 | ok | **derive** |
| row-delete | 0.72 | 4 | ok | **derive** |
| cell-edit | 0.50 | 4 | ok | **derive** |
| table-relayout | 0.71 | 6 | ok | **derive** |
| table-insert | 0.86 | 3 | low-n | derive (mark low-n) |
| image-replace | 0.78 | 4 | ok | **derive** |
| image-insert | 1.00 | 2 | low-n | omit (strong) |
| legacy-doc | 0.75 | 3 | low-n | derive (mark low-n) |
| verify-safety | 0.92 | 6 | ok | omit (strong) |

`subscore = Σ(normalized×weight) / Σ(weight)` over that tag's questions.

- docx-model: (3+0.5×3+2+2+2×0.667+2+2) / 17 = 14.5 / 17
- table-locate: (0.5×3+3+2+2+0.5×2+0.5×2) / 14 = 10.5 / 14
- row-delete: (0.5×3+2+0.5×2+2) / 9 = 6.5 / 9
- cell-edit: (0.5×2+0.5×3+0.5×2+0.5×2) / 9 = 4.5 / 9
- table-relayout: (2+0.667×3+0.333×3+2+0.5×2+2) / 14 = 10 / 14
- table-insert: (3+2+0.5×2) / 7 = 6 / 7
- image-replace: (0.667×3+2+0.5×2+2) / 9 = 7 / 9
- legacy-doc: (0.5×3+2+1) / 6 = 4.5 / 6
- verify-safety: (2+0.5×2+2+3+2+2) / 13 = 12 / 13

## Stack score

```
stack_score = Σ(normalized×weight) / Σ(weight) = 54 / 69 = 78.3%
```

(Σweight = 69. Lost 1.5 on docx-model-02, rowdel-01, cell-01, legacy-01 each;
1 on rowdel-03, cell-02, cell-03, relayout-01, tblins-03, imgrep-01, imgrep-03 each;
2 on relayout-02.)

## Derivation targets

Tags below threshold (`< 0.9`): **docx-model**, **table-locate**, **row-delete**,
**cell-edit**, **table-relayout**, **table-insert** (low-n), **image-replace**,
**legacy-doc** (low-n) → feed into `derived/docx.cat-jam-free.SKILL.md`.

### cell-edit (0.50)
- Every cell-edit question loses its second point. cell-01 knows `cell.text`
  drops run formatting but offers no fix. docx-model-02 only says "better helpers
  preserve formatting". The model diagnoses formatting loss without ever stating
  the write-into-the-first-run technique.
- rowdel-03 / cell-02: believes a `fullCalcOnLoad` / "recalc on load" switch makes
  Word recompute fields. That is SpreadsheetML; Word has no silent recompute on
  open (`w:updateFields` only prompts). The cached result must be written.
- cell-03: "turn off trackRevisions" offered as the clean-edit route; pending
  revisions and `w:delText` left untouched.

### table-relayout (0.71)
- relayout-02: accepts `add_column`'s right-end append when the column was asked
  for after Item; no mid-grid gridCol + tc insertion, no gridSpan care, no copying
  of header/body cell formatting.
- relayout-01: copied row text "blanked then modified" rather than written through
  the existing runs; banding ignored.
- tblins-03: thinks `add_table` leaves widths unset/auto.

### row-delete (0.72)
- rowdel-01 stops at the XML removal: no reflow note, no Total update.
- rowdel-03 as above (field recompute myth).

### table-locate (0.75) / docx-model (0.85)
- Both pulled down by docx-model-02 (no first-run rewrite), cell-02 (number
  format), imgrep-03 (header part rels). No locate-specific error beyond those.

### image-replace (0.78)
- imgrep-01: never removes the old rel/media after repointing; offers byte
  overwrite of the part first.
- imgrep-03: does not say the header's rId lives in the header part's rels.
- (verify-03 is correct: the model does know orphan media must be removed once
  asked about it directly, but does not apply it inside a replace.)

### legacy-doc (0.75, low-n)
- legacy-01 lists LibreOffice, Pandoc and textutil as interchangeable .doc
  converters. Pandoc cannot read .doc; textutil flattens tables.

### table-insert (0.86, low-n)
- Only tblins-03's width myth (shared with table-relayout).

## Contamination check

No verbatim overlap with the reference answers. The answers are much shorter than
the key, use their own structure, and carry independent errors the key does not
have (`fullCalcOnLoad` for Word, Pandoc as a .doc converter, `add_table` widths
"unset/auto", "turn off trackRevisions"). Consistent with the audited zero tool
calls.
