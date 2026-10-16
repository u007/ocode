---
model_id: nvidia/nemotron-3.5-super-vl
model_version: "3.5"
evaluated_via: openrouter
evaluated_on: 2026-10-16
stack: docx
stack_corpus_rev: 1
threshold: 0.9
validation: with-skill
---

# Scorecard — nvidia/nemotron-3.5-super-vl on docx (with-skill validation)

> Valid ONLY for `nvidia/nemotron-3.5-super-vl` @ `3.5`. A version bump invalidates
> this scorecard — re-benchmark.

> **WITH-SKILL VALIDATION RUN.** Answers:
> `docx/answers/nvidia__nemotron-3.5-super-vl.with-skill.md`, produced closed-book
> with the current derived skill `derived/docx.nvidia__nemotron-3.5-super-vl.SKILL.md`
> (`docx-tuning-nvidia-nemotron-3.5-super-vl`, front matter stripped) prepended to
> the answerer prompt as active guidance (audited zero tool calls). Baseline:
> `scores/nvidia__nemotron-3.5-super-vl.md`. No new derived skill is produced from
> this run.

> Target tags for this skill (baseline < 0.9): **table-relayout (0.86),
> table-insert (0.86, low-n), image-replace (0.89)**.

## Provenance

Closed-book (Rule 0). Same setup as the baseline: `ocode run -m
openrouter/nvidia/nemotron-3.5-super-vl:free -effort med` under an isolated HOME
whose config denies every tool, in a scratch cwd holding only the `_prompts/docx.md`
sheet with the derived skill body prepended; one fresh session, 1 user + 1
assistant message, 0 tool calls. All 29 ids came back once, none empty. No answer
cites an ocode tool or skill the question never named; no thinking-aloud, no
fabricated tool output, no duplicated records. The OpenRouter `:free` route
variant is not part of the model id.

Graded against `docx/questions.yaml` corpus_rev 1 with the baseline's rule: a
`point` whose core concept is present earns full credit (minor missing sub-items
are noted); a `point` whose only concrete mechanism is a wrong API fact, or that
is contradicted by a materially wrong claim about the same construct, earns 0.

## Per-question results

| id | tags | weight | full | awarded | normalized | notes |
|----|------|-------:|-----:|--------:|-----------:|-------|
| docx-model-01 | docx-model | 3 | 2 | 2 | 1.00 | |
| docx-model-02 | docx-model, table-locate | 3 | 2 | 2 | 1.00 | weaker than baseline: first suggests `"Widget C" in run.text` across runs (cannot match a split string), then concatenated paragraph/cell text or XPath over w:t; replace by editing the matching w:t nodes, "rebuilding runs" if spanned — first-run formatting not called out (minor) |
| docx-model-03 | docx-model, table-relayout | 2 | 2 | 2 | 1.00 | |
| docx-locate-01 | table-locate | 3 | 2 | 2 | 1.00 | now asserts exactly one match; still substring/case-insensitive (minor) |
| docx-locate-02 | table-locate, docx-model | 2 | 2 | 1 | 0.50 | same as baseline: pt1 withheld ("one Cell per w:tc … undercounts", the opposite of python-docx's per-grid-column repeat); pt2 awarded (gridCol count / sum gridSpan to map indexes) |
| docx-rowdel-01 | row-delete | 3 | 2 | 1 | 0.50 | pt1 ok. pt2 absent: removal + save only — no reflow statement, no dependent-total follow-up (baseline at least named the SUM field). Non-target drift |
| docx-rowdel-02 | row-delete, table-locate | 2 | 2 | 2 | 1.00 | as baseline: next cell promoted to restart, content not moved (minor); "delete the whole merged set" still offered as an alternative |
| docx-rowdel-03 | row-delete, cell-edit | 2 | 2 | 2 | 1.00 | cached result + write new result + mark dirty; "Never assume the field self-updates on save" |
| docx-rowdel-04 | row-delete, verify-safety | 2 | 2 | 2 | 1.00 | |
| docx-cell-01 | cell-edit | 3 | 2 | 1 | 0.50 | pt1 ok (run formatting lost, tcPr kept, also pPr). pt2 absent: no remedy at all this run — neither "edit the existing first run" nor "copy rPr" (baseline had all three options). Non-target drift |
| docx-cell-02 | cell-edit, table-locate | 2 | 2 | 2 | 1.00 | recompute in code and write; number format/alignment preservation not mentioned this run (minor) |
| docx-cell-03 | cell-edit, verify-safety | 2 | 2 | 2 | 1.00 | as baseline; no w:delText check (minor) |
| docx-relayout-01 | table-relayout | 3 | 3 | 3 | 1.00 | baseline gap closed: fills through each cell's first existing run, removes extra runs/paragraphs, "never cell.text on the clone", picks the template row whose shading fits the slot |
| docx-relayout-02 | table-relayout | 3 | 3 | 3 | 1.00 | |
| docx-relayout-03 | table-relayout, docx-model | 2 | 2 | 2 | 1.00 | "gridCol is only a default … does nothing while tcW exists" slightly overstated (minor) |
| docx-tblins-01 | table-insert | 3 | 2 | 2 | 1.00 | |
| docx-tblins-02 | table-insert | 2 | 2 | 2 | 1.00 | |
| docx-tblins-03 | table-insert, table-relayout | 2 | 2 | 2 | 1.00 | baseline gap closed: tblW auto, gridCol and tcW = text width / cols, "fixed even widths, not content-sized"; full matching list |
| docx-imgrep-01 | image-replace, docx-model | 3 | 3 | 2 | 0.67 | pt1, pt2 ok (chain, `rId, image = part.get_or_add_image`, repoint only this blip, keep extent, integer cy). pt3 ABSENT this run: nothing about removing the old rel/media when no other blip uses it — ends at "Never overwrite an existing part's blob". The baseline answer had this point; the gap moved from verify-03 to here |
| docx-imgrep-02 | image-replace | 2 | 2 | 2 | 1.00 | SHA1 dedupe now named as the cause |
| docx-imgrep-03 | image-replace, table-locate | 2 | 2 | 2 | 1.00 | header/footer parts, linked-to-previous, anchored, that part's get_or_add_image, drop the old rel once no a:blip names it |
| docx-imgins-01 | image-insert | 3 | 2 | 2 | 1.00 | |
| docx-imgins-02 | image-insert, table-relayout | 2 | 2 | 2 | 1.00 | |
| docx-legacy-01 | legacy-doc | 3 | 2 | 2 | 1.00 | OLE2 named now; textutil still offered as a converter without caveat (minor, contradicts its legacy-02) |
| docx-legacy-02 | legacy-doc, verify-safety | 2 | 2 | 2 | 1.00 | |
| docx-legacy-03 | legacy-doc | 1 | 2 | 2 | 1.00 | |
| docx-verify-01 | verify-safety | 3 | 2 | 2 | 1.00 | new file + reopen + assert edits + render check; "nothing else changed" diff not stated this run (minor); media-hash/rels checks carried over from the skill |
| docx-verify-02 | verify-safety, docx-model | 2 | 2 | 2 | 1.00 | now names the empty w:tc; dangling rIds / duplicate docPr not named (minor) |
| docx-verify-03 | verify-safety, image-replace | 2 | 2 | 2 | 1.00 | baseline gap closed: "a part no rel reaches is not written on save, so the old media file's absence alone is not proof until the rels and rIds are gone"; count r:embed, then drop the rel; hash every media part |

`normalized = min(awarded, full) / full`

## Per-tag subscores

| tag | baseline | with-skill | delta | n | trust | target? |
|-----|---------:|-----------:|------:|--:|-------|---------|
| docx-model | 0.94 | 0.88 | -0.06 | 7 | ok | no |
| table-locate | 0.93 | 0.93 | 0.00 | 6 | ok | no |
| row-delete | 1.00 | 0.83 | -0.17 | 4 | ok | no |
| cell-edit | 1.00 | 0.83 | -0.17 | 4 | ok | no |
| table-relayout | 0.86 | 1.00 | +0.14 | 6 | ok | **yes** |
| table-insert | 0.86 | 1.00 | +0.14 | 3 | low-n | **yes** |
| image-replace | 0.89 | 0.89 | 0.00 | 4 | ok | **yes** |
| image-insert | 1.00 | 1.00 | 0.00 | 2 | low-n | no |
| legacy-doc | 1.00 | 1.00 | 0.00 | 3 | low-n | no |
| verify-safety | 0.92 | 1.00 | +0.08 | 6 | ok | no |

`subscore = Σ(normalized×weight) / Σ(weight)` over that tag's questions.

Arithmetic: docx-model 15/17; table-locate 13/14; row-delete 7.5/9; cell-edit
7.5/9; table-relayout 14/14; table-insert 7/7; image-replace 8/9; image-insert
5/5; legacy-doc 6/6; verify-safety 13/13.

## Stack score

```
stack_score = Σ(normalized×weight) / Σ(weight) = 64 / 69 = 92.8%
```

(Σweight = 12×3 + 16×2 + 1×1 = 69; lost 1 on docx-locate-02, 1.5 on
docx-rowdel-01, 1.5 on docx-cell-01, 1 on docx-imgrep-01.) Baseline 65 / 69 = 94.2%.

## Target-tag verdict

| tag | baseline | with-skill | pass (≥ 0.9)? |
|-----|---------:|-----------:|---------------|
| table-relayout | 0.86 | 1.00 | **PASS** |
| table-insert | 0.86 | 1.00 | **PASS** (low-n) |
| image-replace | 0.89 | 0.89 | **FAIL** |

**table-relayout and table-insert pass.** Both baseline failure modes are gone:
relayout-01 now fills the copied row through its first existing run and keeps the
banding; tblins-03 now states the even gridCol/tcW split.

**image-replace stays at 0.89.** The baseline gap (verify-03 pt1, "the rel and
media part survive the drawing's removal") is closed: verify-03, imgrep-03 and
imgrep-02 all now say to count `r:embed` and drop the rel. But the same rule went
missing from imgrep-01, which the baseline had right:

- docx-imgrep-01 pt3 — "remove the old rel/media if unused so the old image is
  gone" — not stated. The answer repoints the blip, keeps the extent, and warns
  never to overwrite a blob, then stops. The old `word/media/imageN.*` is left in
  the package.

Digest items 4 and 5 state the rule, but only in the context of "after repointing
a blip or removing a w:drawing … verify". The model applied them to the removal
and verification questions, not to the plain "replace one logo" question. If the
lead iterates, the sharpest fix is a directive that makes the rel-drop an explicit
LAST STEP of every replacement recipe (repoint → count r:embed → drop rel), rather
than a separate verification rule. This scorecard does not modify the skill.

## Non-target drift

Three non-target tags moved down on this single sample: row-delete 1.00 → 0.83
(rowdel-01 pt2: no reflow/totals follow-up), cell-edit 1.00 → 0.83 (cell-01 pt2:
no preservation remedy), docx-model 0.94 → 0.88 (carried by imgrep-01). Each is one
question's point that the baseline answer had and this sample dropped; none is a
construct the skill touches. Per HOW-TO-EVALUATE.md ("Only trust TARGET-tag
movement") these are left alone; if row-delete or cell-edit reads low again on a
fresh baseline, derive their own sections then.

## Contamination check

Target-tag answers reuse the skill's own phrasing ("never cell.text on the
clone", "a part no rel reaches is not written on save", "count r:embed yourself").
Per HOW-TO-EVALUATE.md ("Overlap ≠ cheating") that is the absorption this run
measures; the prompt carried no answer key and the session made no tool calls.
Independent errors persist (row.cells per w:tc, textutil offered as a converter),
which a key-copying answer would not carry.
