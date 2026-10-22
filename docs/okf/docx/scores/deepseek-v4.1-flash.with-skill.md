---
model_id: deepseek-v4.1-flash
model_version: "4.1-2026-10-21"
evaluated_via: opencode-go
evaluated_on: 2026-10-21
stack: docx
stack_corpus_rev: 1
threshold: 0.9
validation: with-skill
source_scorecard: deepseek-v4.1-flash.md
skill: ../derived/docx.deepseek-v4.1-flash.SKILL.md
---

# Scorecard — deepseek-v4.1-flash on docx (with-skill validation)

> Valid ONLY for `deepseek-v4.1-flash` @ `4.1-2026-10-21`. A version bump invalidates
> this scorecard — re-benchmark.

> **WITH-SKILL VALIDATION RUN.** Answers: `docx/answers/deepseek-v4.1-flash.with-skill.md`,
> produced closed-book with the re-derived skill
> `derived/docx.deepseek-v4.1-flash.SKILL.md` (`docx-tuning-deepseek-v4.1-flash`,
> `tuned_version: "4.1-2026-10-21"`) prepended to each question's prompt as active
> guidance. One fresh `ocode run` session per question, every tool denied, the other
> Kaizen skills for this model disabled, every session sqlite audited: zero tool calls.
> Baseline: `scores/deepseek-v4.1-flash.md` (93.5%). No new derived skill is produced
> from this run.

> Target tags for this skill (baseline < 0.9): **row-delete (0.83), table-relayout (0.86),
> table-insert (0.86, low-n), image-replace (0.89)**.

Graded against `docx/questions.yaml` corpus_rev 1. Disputed python-docx facts were
re-checked against the installed python-docx 1.2.0 on 2026-10-21.
Grading rule applied (same as the baseline): each `point` is awarded whole. A `point`
whose core concept is present earns full credit (minor missing sub-items are noted); a
`point` whose core is stated with a wrong API fact, or satisfied only by a cover-up
(hidden/white text, zero-height rows, white boxes), earns 0. A wrong API fact that sits
beside a correct core is noted but not deducted. Equivalent correct approaches in
another library earn the point.

## History

- 2026-09-28: with-skill validation of the earlier skill against the earlier `4.1` model,
  96.4% (66.5/69), targets row-delete and image-replace both passed. Superseded by this
  file (in git history); it does not describe `4.1-2026-10-21`.

## Per-question results

| id | tags | weight | full | awarded | normalized | notes |
|----|------|-------:|-----:|--------:|-----------:|-------|
| docx-model-01 | docx-model | 3 | 2 | 2 | 1.00 | |
| docx-model-02 | docx-model, table-locate | 3 | 2 | 2 | 1.00 | offset map over `w:t` nodes, replacement into the first matched node (keeps its run's rPr) |
| docx-model-03 | docx-model, table-relayout | 2 | 2 | 2 | 1.00 | |
| docx-locate-01 | table-locate | 3 | 2 | 2 | 1.00 | header signature (several keys), asserts exactly one table AND exactly one row; header/footer and nested tables covered |
| docx-locate-02 | table-locate, docx-model | 2 | 2 | 2 | 1.00 | `tc_lst` + `grid_span`; vMerge repeat covered |
| docx-rowdel-01 | row-delete | 3 | 2 | 2 | 1.00 | remove `row._tr`; "no gap to close"; "Removal updates nothing else ... Recompute and rewrite" the Total. Side claim "Merged cells are fine ... no extra cleanup needed" is wrong for a vMerge restart row (its own rowdel-02 says so); noted, not deducted |
| docx-rowdel-02 | row-delete, table-locate | 2 | 2 | 2 | 1.00 | orphaned continuation; promote the next cell to restart with the moved content; drop vMerge when one row remains |
| docx-rowdel-03 | row-delete, cell-edit | 2 | 2 | 2 | 1.00 | cached result; writes recomputed value between `separate` and `end`, keeps the field; updateFields optional with caveats |
| docx-rowdel-04 | row-delete, verify-safety | 2 | 2 | 2 | 1.00 | scans every story part; `w:tr` count N−1 |
| docx-cell-01 | cell-edit | 3 | 2 | 2 | 1.00 | write through the existing run or copy pPr/rPr |
| docx-cell-02 | cell-edit, table-locate | 2 | 2 | 2 | 1.00 | recompute from inputs with Decimal; keeps number format via the existing run |
| docx-cell-03 | cell-edit, verify-safety | 2 | 2 | 2 | 1.00 | wrong fact beside a correct core: says `paragraph.text = …` "clears the direct content ... leaving the revision-wrapped runs in place"; 1.2.0 `CT_P.clear_content` removes every non-`pPr` child, `w:ins` included. Not deducted. `w:delText` check is phrased as "no w:ins/w:del remain in the edited cell" (minor) |
| docx-relayout-01 | table-relayout | 3 | 3 | 3 | 1.00 | `add_row` source quoted exactly (`tc.width = gridCol.w`); deepcopy + `addnext`; text through existing runs. Banding is not handled: its verify step asserts the new row's `tcPr` shading equals the anchor's, which breaks alternating banding (minor; whole-point rule, core of point 3 present, same as baseline) |
| docx-relayout-02 | table-relayout | 3 | 3 | 3 | 1.00 | `add_column` writes `tcW = width` (correct now); `columns[i].width` "writes only gridCol/@w:w" plus `cell.width` on every cell, gridSpan sums, autofit off; copies header runs/shading. gridSpan at the insertion index (merged Total row) not called out (minor) |
| docx-relayout-03 | table-relayout, docx-model | 2 | 2 | 2 | 1.00 | overgeneralises "gridCol alone is inert for rendering" (LibreOffice uses the grid; minor) |
| docx-tblins-01 | table-insert | 3 | 2 | 2 | 1.00 | |
| docx-tblins-02 | table-insert | 2 | 2 | 2 | 1.00 | likely-wrong fact beside a correct core: claims the stored name is "Grid Table 4 - Accent 1" so "Grid Table 4 Accent 1" can miss even when present. The hyphen is Word's UI label; stored names follow the unhyphenated pattern of python-docx's own latent styles ("Light Grid Accent 1"), not verified against a Word 2013+ styles.xml here. Its import example would then fail (`StopIteration` from `next()`). Option 4's claim that Word resolves an undefined built-in styleId is unverified. Not deducted |
| docx-tblins-03 | table-insert, table-relayout | 2 | 2 | 2 | 1.00 | even split correct; style + gridCol AND every cell's tcW + header formatting. Wrong fact beside a correct core: "sets tblW to the full block width"; 1.2.0 writes `<w:tblW w:type="auto" w:w="0"/>` (probed). Not deducted |
| docx-imgrep-01 | image-replace, docx-model | 3 | 3 | 3 | 1.00 | owning-part rels, dedupe; `get_or_add_image` + repoint, keep extents; drops old rel when no `r:embed` still uses it. Code uses `shape._parent.part` — `InlineShape` has no `_parent` in 1.2.0 (AttributeError, probed); the comment names the right parts. Closing line "leaving the orphan rel is harmless and is the conservative choice" muddles the point but the code drops it. Not deducted |
| docx-imgrep-02 | image-replace | 2 | 2 | 2 | 1.00 | |
| docx-imgrep-03 | image-replace, table-locate | 2 | 2 | 2 | 1.00 | header/footer parts, `wp:anchor`, VML; that part's rels; explicitly no `_blob` overwrite |
| docx-imgins-01 | image-insert | 3 | 2 | 2 | 1.00 | wrong fact beside a correct core: "`paragraph.text` concatenates only direct `w:r` children, so text inside `w:hyperlink` ... is invisible"; 1.2.0 includes hyperlink text. Not deducted |
| docx-imgins-02 | image-insert, table-relayout | 2 | 2 | 2 | 1.00 | sizes to cell width minus margins; `cell.left_margin` is not a 1.2.0 API, used as a formula (minor) |
| docx-legacy-01 | legacy-doc | 3 | 2 | 2 | 1.00 | |
| docx-legacy-02 | legacy-doc, verify-safety | 2 | 2 | 2 | 1.00 | |
| docx-legacy-03 | legacy-doc | 1 | 2 | 2 | 1.00 | |
| docx-verify-01 | verify-safety | 3 | 2 | 2 | 1.00 | width invariant "gridCol equals the sum of tcW over that column's cells" is garbled (minor) |
| docx-verify-02 | verify-safety, docx-model | 2 | 2 | 2 | 1.00 | python-docx doesn't validate; duplicate `wp:docPr` ids not mentioned (minor) |
| docx-verify-03 | verify-safety, image-replace | 2 | 2 | 2 | 1.00 | |

`normalized = min(awarded, full) / full`

## Per-tag subscores

| tag | subscore | n | trust | target? |
|-----|---------:|--:|-------|---------|
| docx-model | 1.00 | 7 | ok | no |
| table-locate | 1.00 | 6 | ok | no |
| row-delete | 1.00 | 4 | ok | **yes** |
| cell-edit | 1.00 | 4 | ok | no |
| table-relayout | 1.00 | 6 | ok | **yes** |
| table-insert | 1.00 | 3 | low-n | **yes** |
| image-replace | 1.00 | 4 | ok | **yes** |
| image-insert | 1.00 | 2 | low-n | no |
| legacy-doc | 1.00 | 3 | low-n | no |
| verify-safety | 1.00 | 6 | ok | no |

`subscore = Σ(normalized×weight) / Σ(weight)` over that tag's questions.

Arithmetic: docx-model 17/17; table-locate 14/14; row-delete 9/9; cell-edit 9/9;
table-relayout 14/14; table-insert 7/7; image-replace 9/9; image-insert 5/5;
legacy-doc 6/6; verify-safety 13/13.

## Stack score

```
stack_score = Σ(normalized×weight) / Σ(weight) = 69 / 69 = 100.0%
```

Baseline 64.5 / 69 = 93.5%. Sanity check: the +4.5 is exactly the four baseline losses,
all on target questions (rowdel-01 +1.5, relayout-02 +1.0, tblins-03 +1.0, imgrep-01
+1.0). No non-target question moved, so the gain is the skill's intended overlap, not
contamination elsewhere.

## Validation result

Target tags, baseline → with-skill (pass = reaches 0.9):

| tag | baseline | with-skill | pass (≥ 0.9)? | what changed |
|-----|---------:|-----------:|---------------|--------------|
| row-delete | 0.83 | 1.00 | **PASS** | rowdel-01 now says removal updates nothing else and the Total must be recomputed and rewritten |
| table-relayout | 0.86 | 1.00 | **PASS** | relayout-02 now states `columns[i].width` writes only gridCol and sets `cell.width` on every cell; `add_row`/`add_column` tcW facts now correct |
| table-insert | 0.86 | 1.00 | **PASS** (low-n) | tblins-03 now copies style, gridCol + every tcW, and header formatting |
| image-replace | 0.89 | 1.00 | **PASS** | imgrep-01 drops the old rel after an `r:embed` count; imgrep-03 no longer offers `_blob` overwrite |

Every target tag reaches 0.9. The skill is validated for deepseek-v4.1-flash @
4.1-2026-10-21. table-insert has n = 3, so its pass is low-n.

### Did the skill introduce wrong facts?

No. Every python-docx claim the answers took from the skill was re-probed with
`inspect.getsource` on the installed 1.2.0 (2026-10-21): `_Column.width` setter is
`self._gridCol.w = value` only; `add_row` sets `tc.width = gridCol.w`; `add_column(width)`
sets `gridCol.w` and each new cell's `tc.width = width`; `drop_rel` deletes when
`_rel_ref_count(rId) < 2`, and that count is `//@r:id` only (never `r:embed`);
`StoryPart.get_or_add_image` returns `(rId, image)` via the package-wide
`get_or_add_image_part` (SHA1 dedupe) and reuses an existing rel. The one skill-driven
overstatement is relayout-03's "gridCol alone is inert for rendering" (the skill scopes
it to Word; the answer generalised it). It is not an API error and is not deducted.

The wrong facts that remain are not from the skill and none are on a target point's
core: `paragraph.text =` leaving `w:ins` runs (cell-03), the (likely wrong, unverified)
hyphenated "Grid Table 4 - Accent 1" stored name (tblins-02), `tblW` set to the block width (tblins-03),
`InlineShape._parent` (imgrep-01), `paragraph.text` skipping hyperlinks (imgins-01),
and "merged cells are fine" when removing a row (rowdel-01). Three of these sit in
target-tag answers (tblins-03, imgrep-01, rowdel-01), but only as side remarks next to a
correct core.

### Non-target drift (not a reason to edit the skill)

Per HOW-TO-EVALUATE.md ("Only trust TARGET-tag movement"), non-target tags are
single-sample resamples. Every non-target tag was already ≥ 0.9 at baseline and stays
there: docx-model 0.94 → 1.00 (its only baseline loss was imgrep-01, which carries the
image-replace tag), all others 1.00 → 1.00. No drift below threshold.
