---
model_id: nvidia/nemotron-3.5-super-vl
model_version: "3.5"
evaluated_via: openrouter
evaluated_on: 2026-10-16
stack: docx
stack_corpus_rev: 1
threshold: 0.9
---

# Scorecard — nvidia/nemotron-3.5-super-vl on docx

> Valid ONLY for `nvidia/nemotron-3.5-super-vl` @ `3.5`. A version bump invalidates
> this scorecard — re-benchmark.

## Provenance

Closed-book (Rule 0). Answers: `docx/answers/nvidia__nemotron-3.5-super-vl.md`.
The answerer was `ocode run -m openrouter/nvidia/nemotron-3.5-super-vl:free -effort med`
under an isolated HOME whose config denies every tool (no tools were advertised),
in a scratch cwd containing only the `_prompts/docx.md` sheet, one fresh session;
the session sqlite shows exactly 1 user + 1 assistant message and 0 tool calls.
All 29 ids came back once, none empty. Headless runs inject the bundled
skill-catalogue digest into the system prompt; no answer cites an ocode tool or
skill the question never named (the only external tools mentioned are
`soffice`, `textutil` and Word COM, all on-topic for the legacy-doc questions).
No thinking-aloud, no fabricated tool output, no duplicated records. The
OpenRouter `:free` route variant is not part of the model id.

Graded against `docx/questions.yaml` corpus_rev 1 (python-docx facts verified on
1.2.0). Grading rule applied: a `point` whose core concept is present earns full
credit (minor missing sub-items are noted); a `point` whose only concrete
mechanism is a wrong API fact, or that is contradicted by a materially wrong
claim about the same construct, earns 0.

## Per-question results

| id | tags | weight | full | awarded | normalized | notes |
|----|------|-------:|-----:|--------:|-----------:|-------|
| docx-model-01 | docx-model | 3 | 2 | 2 | 1.00 | full hierarchy incl. tblPr/trPr/tcPr/pPr/rPr children, nested tables |
| docx-model-02 | docx-model, table-locate | 3 | 2 | 2 | 1.00 | split attributed to br/fields/hyperlinks rather than formatting/proofing/rsid (minor); joins all w:t, isolates the match, keeps surrounding rPr |
| docx-model-03 | docx-model, table-relayout | 2 | 2 | 2 | 1.00 | |
| docx-locate-01 | table-locate | 3 | 2 | 2 | 1.00 | row match allows "contains" (could hit "Gadget DX") and never asserts exactly one match, only "confirm it is the right row" (minor); merges, trackRevisions, SUM refresh, backup all noted |
| docx-locate-02 | table-locate, docx-model | 2 | 2 | 1 | 0.50 | pt1 withheld: states the opposite of the real behaviour — "row.cells returns one _Cell per w:tc … ONE cell for the span, len undercounts". python-docx returns one entry per GRID column with the merged _Cell repeated. pt2 awarded: "count w:gridCol for the column count, inspect gridSpan per cell" is a sound technique (judgment call: the remedy is right, the model of row.cells is wrong) |
| docx-rowdel-01 | row-delete | 3 | 2 | 2 | 1.00 | removal via `table._tbl.remove(row._tr)` correct; says to handle the SUM field separately (totals) but never says Word reflows / no gap to close (minor) |
| docx-rowdel-02 | row-delete, table-locate | 2 | 2 | 2 | 1.00 | promotes the next continuation to restart (drops vMerge if standalone) but never moves the origin cell's content down, only "fix any text" (minor); also offers "delete every row in the merged group" as an alternative, which deletes more than asked |
| docx-rowdel-03 | row-delete, cell-edit | 2 | 2 | 2 | 1.00 | cached result + rewrite result runs; says `w:updateFields` makes Word "refresh on open" (it prompts the user) — minor |
| docx-rowdel-04 | row-delete, verify-safety | 2 | 2 | 2 | 1.00 | |
| docx-cell-01 | cell-edit | 3 | 2 | 2 | 1.00 | also notes extra paragraphs/alignment lost |
| docx-cell-02 | cell-edit, table-locate | 2 | 2 | 2 | 1.00 | |
| docx-cell-03 | cell-edit, verify-safety | 2 | 2 | 2 | 1.00 | bypass-tracking + decide tracked vs clean + accept revisions first; never mentions that existing w:del/w:ins complicate paragraph.text, no w:delText check (minor) |
| docx-relayout-01 | table-relayout | 3 | 3 | 2 | 0.67 | pt1, pt2 ok (append-at-end, deepcopy + addnext). pt3 withheld: "clear the clone's cell texts and fill the new values" never says to write through the existing runs — done with `cell.text` it discards the rPr the deepcopy preserved; banding/shading alternation not addressed (judgment call) |
| docx-relayout-02 | table-relayout | 3 | 3 | 3 | 1.00 | gridCol + tc after Item in every row (gridSpan/vMerge noted), rebalance to text width, fixed layout, tcW = gridCol, copy Item column formatting |
| docx-relayout-03 | table-relayout, docx-model | 2 | 2 | 2 | 1.00 | "Word ignores tblGrid when autofit" slightly overstated (minor) |
| docx-tblins-01 | table-insert | 3 | 2 | 2 | 1.00 | |
| docx-tblins-02 | table-insert | 2 | 2 | 2 | 1.00 | |
| docx-tblins-03 | table-insert, table-relayout | 2 | 2 | 1 | 0.50 | pt1 withheld: "no fixed size … columns are not fixed to a known width" — python-docx DOES write gridCol and tcW widths: the section text width split evenly across the columns (tblW is auto, but the grid is set). pt2 ok (tblW, fixed layout, gridCol, tcW, style, borders, shading copied; header formatting not called out specifically) |
| docx-imgrep-01 | image-replace, docx-model | 3 | 3 | 3 | 1.00 | chain, new part + new rel + repoint only this blip, keep extent, drop the old part only if no other rId use; aspect-ratio adjustment not mentioned (minor); library-agnostic (no get_or_add_image) |
| docx-imgrep-02 | image-replace | 2 | 2 | 2 | 1.00 | shared part/rel identified and new-part fix given; never explains WHY they share (SHA1 dedupe on add) — minor |
| docx-imgrep-03 | image-replace, table-locate | 2 | 2 | 2 | 1.00 | header/first/even parts, linked-to-previous, that part's rels, wp:anchor matching |
| docx-imgins-01 | image-insert | 3 | 2 | 2 | 1.00 | both placements (run in a new paragraph after the anchor, or add-then-move the paragraph); width-only aspect |
| docx-imgins-02 | image-insert, table-relayout | 2 | 2 | 2 | 1.00 | |
| docx-legacy-01 | legacy-doc | 3 | 2 | 2 | 1.00 | OLE2 not named, "renaming doesn't help" not stated (minor); offers textutil as a converter here with no caveat, which its own legacy-02 answer contradicts |
| docx-legacy-02 | legacy-doc, verify-safety | 2 | 2 | 2 | 1.00 | does not say to fall back to LibreOffice/Word (minor) |
| docx-legacy-03 | legacy-doc | 1 | 2 | 2 | 1.00 | |
| docx-verify-01 | verify-safety | 3 | 2 | 2 | 1.00 | |
| docx-verify-02 | verify-safety, docx-model | 2 | 2 | 2 | 1.00 | seven causes incl. grid mismatch, schema order, vMerge, dangling/duplicate rIds; "cell without w:p" and duplicate wp:docPr ids not named |
| docx-verify-03 | verify-safety, image-replace | 2 | 2 | 1 | 0.50 | pt2 ok (list media, check every part's .rels, grep rIds/bytes, also docProps/thumbnail). pt1 absent: never states that removing the w:drawing leaves the rel and media part in the zip, nor that the rel must be dropped — everything is phrased as "confirm … is absent" as if it leaves on its own |

`normalized = min(awarded, full) / full`

## Per-tag subscores

| tag | subscore | n | trust | action |
|-----|---------:|--:|-------|--------|
| docx-model | 0.94 | 7 | ok | omit (strong) |
| table-locate | 0.93 | 6 | ok | omit (strong) |
| row-delete | 1.00 | 4 | ok | omit (strong) |
| cell-edit | 1.00 | 4 | ok | omit (strong) |
| table-relayout | 0.86 | 6 | ok | **derive** |
| table-insert | 0.86 | 3 | low-n | **derive** (mark low-n) |
| image-replace | 0.89 | 4 | ok | **derive** |
| image-insert | 1.00 | 2 | low-n | omit (strong) |
| legacy-doc | 1.00 | 3 | low-n | omit (strong) |
| verify-safety | 0.92 | 6 | ok | omit (strong) |

`subscore = Σ(normalized×weight) / Σ(weight)` over that tag's questions.

- docx-model: (3+3+2+0.5×2+2+3+2) / 17 = 16 / 17
- table-locate: (3+3+0.5×2+2+2+2) / 14 = 13 / 14
- table-relayout: (2+0.67×3+3+2+0.5×2+2) / 14 = 12 / 14
- table-insert: (3+2+0.5×2) / 7 = 6 / 7
- image-replace: (3+2+2+0.5×2) / 9 = 8 / 9
- verify-safety: (2+2+2+3+2+0.5×2) / 13 = 12 / 13

## Stack score

```
stack_score = Σ(normalized×weight) / Σ(weight) = 65 / 69 = 94.2%
```

(Σweight = 12×3 + 16×2 + 1×1 = 69; lost 1 each on docx-locate-02,
docx-relayout-01, docx-tblins-03, docx-verify-03.)

## Derivation targets

Tags below threshold (`< 0.9`): **table-relayout**, **table-insert** (low-n),
**image-replace** → feed into `derived/docx.nvidia__nemotron-3.5-super-vl.SKILL.md`.

Two of the three are borderline and each rides on one judgment call: table-relayout
on relayout-01 pt3 (awarding it gives 13/14 = 0.93), image-replace on verify-03
pt1 (awarding it gives 9/9). Both were withheld because the concept is genuinely
absent, not merely terse. table-insert is below threshold on a wrong fact.

### table-relayout (0.86)
- relayout-01 (weight 3, 0.67): the deepcopy-and-addnext technique is right, but
  the fill step is "clear the clone's cell texts and fill the new values". The
  point of copying a styled row is the run formatting it carries; filling through
  `cell.text` (the obvious reading of "clear the cell texts") replaces every run
  with one default run and throws that away. The answer never says to set the
  text on the existing first run and clear the others, and never considers
  alternating banding.
- tblins-03 (weight 2, 0.50, shared with table-insert): see below.

### table-insert (0.86, low-n)
- tblins-03 (weight 2, 0.50): claims a fresh `add_table` has "no fixed size" and
  "columns are not fixed to a known width". python-docx writes a `w:gridCol` and
  a `w:tcW` for every column, each equal to the section text width divided by the
  column count; only `w:tblW` is `auto`. The matching advice (copy tblW, layout,
  gridCol, tcW, style, borders) is sound.

### image-replace (0.89)
- verify-03 (weight 2, 0.50): the same gap other models show on this question.
  The answer checks that the old media and rel are "absent" after removing the
  `w:drawing`, but never says that they are NOT absent on their own: python-docx
  saves every part still reachable through a rel, so the old `word/media/imageN.*`
  stays in the zip until the rel is dropped from the owning part. The model's
  imgrep-01 does say "drop the old part only if nothing else references its rId",
  so it half-knows the rule; it does not connect it to the remove-drawing case.
- imgrep-02 (awarded, minor): identifies the shared part but not the cause
  (SHA1 dedupe on add), so it cannot predict when a part will be shared.

## Contamination check

No verbatim overlap with the reference answers. Answers are in the model's own
structure, generally longer than the key, and carry independent errors the key
does not have (row.cells described as per-w:tc, `add_table` described as having
no set column widths, `updateFields` described as refreshing on open, textutil
offered as a converter in legacy-01). The throwaway profile path
`/tmp/lo-profile-$$` resembles the key's `/tmp/lo_profile_$$` but is a common
naming pattern; not treated as contamination given the audited zero tool calls.
