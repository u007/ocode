---
model_id: deepseek-v4.1-flash
model_version: "4.1"
evaluated_via: opencode-go
evaluated_on: 2026-09-28
stack: docx
stack_corpus_rev: 1
threshold: 0.9
validation: with-skill
---

# Scorecard — deepseek-v4.1-flash on docx (with-skill validation)

> Valid ONLY for `deepseek-v4.1-flash` @ `4.1`. A version bump invalidates
> this scorecard — re-benchmark.

> **WITH-SKILL VALIDATION RUN.** Answers: `docx/answers/deepseek-v4.1-flash.with-skill.md`,
> produced closed-book with the current derived skill
> `derived/docx.deepseek-v4.1-flash.SKILL.md` (`docx-tuning-deepseek-v4.1-flash`)
> prepended to the answerer prompt as active guidance (audited zero tool calls).
> Baseline: `scores/deepseek-v4.1-flash.md`. No new derived skill is produced from
> this run.

> Target tags for this skill (baseline < 0.9): **row-delete, image-replace**.

Graded against `docx/questions.yaml` corpus_rev 1 (python-docx facts verified on 1.2.0).
Grading rule applied (same strictness as the baseline): a `point` whose core
concept is present earns full credit (minor missing sub-items are noted); a
`point` stated with a wrong API fact, or satisfied only by a cover-up
(hidden/white text, zero-height rows, white boxes), earns 0. Points are whole;
fractions come only from a listed partial.

## History

- Earlier with-skill scorecard: none (this is the first with-skill validation for
  deepseek-v4.1-flash on docx).

## Per-question results

| id | tags | weight | full | awarded | normalized | notes |
|----|------|-------:|-----:|--------:|-----------:|-------|
| docx-model-01 | docx-model | 3 | 2 | 2 | 1.00 | |
| docx-model-02 | docx-model, table-locate | 3 | 2 | 2 | 1.00 | |
| docx-model-03 | docx-model, table-relayout | 2 | 2 | 2 | 1.00 | |
| docx-locate-01 | table-locate | 3 | 2 | 1 | 0.50 | table by header content: yes. Row matched by cell text but never asserts exactly one matching row (only "no other table shares the same headers", which is about the table). The baseline answer had "the match is unique (count matches)"; this one lost it |
| docx-locate-02 | table-locate, docx-model | 2 | 2 | 2 | 1.00 | vMerge repeating the top cell not mentioned (minor) |
| docx-rowdel-01 | row-delete | 3 | 2 | 2 | 1.00 | remove `row._tr`; "no gap to close"; recompute and rewrite Totals. "nothing shifts" is loose wording for "rows move up by themselves" (minor) |
| docx-rowdel-02 | row-delete, table-locate | 2 | 2 | 2 | 1.00 | promotes next cell to restart and moves the anchor's text into it |
| docx-rowdel-03 | row-delete, cell-edit | 2 | 2 | 2 | 1.00 | cached result, python-docx doesn't evaluate; rewriting the cached result run is offered, but its "in practice" recommendation is to replace the field with plain text (drops the field; minor) |
| docx-rowdel-04 | row-delete, verify-safety | 2 | 2 | 2 | 1.00 | searches every XML part incl. headers/footers; row count −1 |
| docx-cell-01 | cell-edit | 3 | 2 | 2 | 1.00 | |
| docx-cell-02 | cell-edit, table-locate | 2 | 2 | 2 | 1.00 | computes from qty × rate with rounding convention; keeping number format/alignment not explicit (minor) |
| docx-cell-03 | cell-edit, verify-safety | 2 | 2 | 2 | 1.00 | no explicit "check w:delText no longer holds the old value" (minor) |
| docx-relayout-01 | table-relayout | 3 | 3 | 3 | 1.00 | deepcopy + addnext/addprevious; replaces the copied row's `w:t` text (existing runs) and fixes alternating `w:shd` |
| docx-relayout-02 | table-relayout | 3 | 3 | 3 | 1.00 | gridCol insertion at the Item index is implied rather than stated (minor) |
| docx-relayout-03 | table-relayout, docx-model | 2 | 2 | 2 | 1.00 | |
| docx-tblins-01 | table-insert | 3 | 2 | 2 | 1.00 | |
| docx-tblins-02 | table-insert | 2 | 2 | 1 | 0.50 | point 1 = 0: states lookup covers "styles physically present (plus a few built-ins such as "Table Grid")". Wrong: python-docx 1.2.0 raises `KeyError: "no style with name 'Table Grid'"` when styles.xml lacks it (verified). Point 2 earned via explicit `w:tblBorders` / an existing template style |
| docx-tblins-03 | table-insert, table-relayout | 2 | 2 | 2 | 1.00 | "no explicit fixed widths" is loose: python-docx writes gridCol and tcW dxa widths (2880 each for 3 cols), only tblW is auto (minor). Header formatting not mentioned (minor, same as baseline) |
| docx-imgrep-01 | image-replace, docx-model | 3 | 3 | 3 | 1.00 | owning-part rels; `get_or_add_image` + repoint `r:embed`, keep extent; no byte overwrite; drop old rel only when no blip still uses it |
| docx-imgrep-02 | image-replace | 2 | 2 | 2 | 1.00 | "check references first" not explicit (minor) |
| docx-imgrep-03 | image-replace, table-locate | 2 | 2 | 2 | 1.00 | header/footer parts, `wp:anchor`, that part's rels; no byte-swap option offered |
| docx-imgins-01 | image-insert | 3 | 2 | 2 | 1.00 | |
| docx-imgins-02 | image-insert, table-relayout | 2 | 2 | 2 | 1.00 | sizes to the full 1.2" cell width, not minus cell margins (minor) |
| docx-legacy-01 | legacy-doc | 3 | 2 | 2 | 1.00 | lists textutil as a converter without its lossiness; no "convert back to .doc if needed" (minor) |
| docx-legacy-02 | legacy-doc, verify-safety | 2 | 2 | 2 | 1.00 | |
| docx-legacy-03 | legacy-doc | 1 | 2 | 2 | 1.00 | |
| docx-verify-01 | verify-safety | 3 | 2 | 2 | 1.00 | |
| docx-verify-02 | verify-safety, docx-model | 2 | 2 | 2 | 1.00 | "python-docx doesn't validate" and duplicate docPr ids not stated (minor) |
| docx-verify-03 | verify-safety, image-replace | 2 | 2 | 2 | 1.00 | |

`normalized = min(awarded, full) / full`

## Per-tag subscores

| tag | subscore | n | trust | target? |
|-----|---------:|--:|-------|---------|
| docx-model | 1.00 | 7 | ok | no |
| table-locate | 0.89 | 6 | ok | no |
| row-delete | 1.00 | 4 | ok | **yes** |
| cell-edit | 1.00 | 4 | ok | no |
| table-relayout | 1.00 | 6 | ok | no |
| table-insert | 0.86 | 3 | low-n | no |
| image-replace | 1.00 | 4 | ok | **yes** |
| image-insert | 1.00 | 2 | low-n | no |
| legacy-doc | 1.00 | 3 | low-n | no |
| verify-safety | 1.00 | 6 | ok | no |

`subscore = Σ(normalized×weight) / Σ(weight)` over that tag's questions.

Arithmetic: docx-model 17/17; table-locate 12.5/14; row-delete 9/9;
cell-edit 9/9; table-relayout 14/14; table-insert 6/7; image-replace 9/9;
image-insert 5/5; legacy-doc 6/6; verify-safety 13/13.

## Stack score

```
stack_score = Σ(normalized×weight) / Σ(weight) = 66.5 / 69 = 96.4%
```

(Losses: locate-01 −1.5, tblins-02 −1.0.) Baseline 65.5 / 69 = 94.9%.

## Target tags: baseline → with-skill → pass(≥0.9)?

| tag | baseline | with-skill | pass (≥ 0.9)? |
|-----|---------:|-----------:|---------------|
| row-delete | 0.83 | 1.00 | **pass** |
| image-replace | 0.89 | 1.00 | **pass** |

Every target tag reaches 0.9. The skill is validated for deepseek-v4.1-flash @ 4.1.

No target tag is still below 0.9, so there is no remaining wrong or missing claim
to list for a target.

### Non-target drift (not a reason to edit the skill)

Per HOW-TO-EVALUATE.md ("Only trust TARGET-tag movement"), these single-sample
drops are resampling noise and must not be bolted onto this skill. Both tags were
1.00 at baseline:
- table-locate 1.00 → 0.89 (locate-01): the row is matched by text, but the answer
  never asserts exactly one matching row. The baseline answer did.
- table-insert 1.00 → 0.86, low-n (tblins-02): wrong API fact that python-docx has
  a few built-in styles such as "Table Grid" even when styles.xml lacks them.
  python-docx 1.2.0 raises KeyError.
If either recurs in a fresh baseline, derive a section there.
