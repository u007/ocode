---
model_id: step-5-preview-free
model_version: "5-preview"
evaluated_via: opencode-go
evaluated_on: 2026-10-12
stack: docx
stack_corpus_rev: 1
threshold: 0.9
---

# Scorecard — step-5-preview-free on docx

> Valid ONLY for `step-5-preview-free` @ `5-preview`. A version bump invalidates
> this scorecard — re-benchmark.

## Provenance

Answers: `docx/answers/step-5-preview-free.md`, produced closed-book by
`ocode run -m opencode-go/step-5-preview-free -effort med` in an empty scratch
directory with only the `_prompts/docx.md` sheet attached; the session sqlite shows
exactly 1 user + 1 assistant message and 0 tool calls. The raw output contained one
hallucinated record id (`docx-imgins-03-placeholder`, answer "placeholder") and a
duplicate near-identical `docx-verify-03`; both were removed before grading (noted in
the answers file header). A stray trailing "Correction —" line from the model's own
self-repair remains after the YAML list and was ignored.

Graded against `docx/questions.yaml` corpus_rev 1 (python-docx facts verified on
1.2.0). Grading rule applied: a `point` whose core concept is present earns full
credit (minor missing sub-items are noted); a `point` whose only concrete mechanism
is a wrong API fact, or that is satisfied only by a cover-up, earns 0.

## Per-question results

| id | tags | weight | full | awarded | normalized | notes |
|----|------|-------:|-----:|--------:|-----------:|-------|
| docx-model-01 | docx-model | 3 | 2 | 2 | 1.00 | full hierarchy incl. tblPr/trPr/tcPr children |
| docx-model-02 | docx-model, table-locate | 3 | 2 | 2 | 1.00 | splice across runs keeping first run; asserts old string absent afterwards |
| docx-model-03 | docx-model, table-relayout | 2 | 2 | 2 | 1.00 | adds the float-EMU gotcha (cx="1371600.0" invalid) |
| docx-locate-01 | table-locate | 3 | 2 | 2 | 1.00 | row match uses "contains" (could hit "Gadget DX") — minor; header tables, merged cells, trackRevisions all noted |
| docx-locate-02 | table-locate, docx-model | 2 | 2 | 2 | 1.00 | |
| docx-rowdel-01 | row-delete | 3 | 2 | 1 | 0.50 | removal via `row._tr.getparent().remove()` correct; never says Word reflows (no gap to close) nor that a dependent Total must be updated — only "verify row count and grid structure" |
| docx-rowdel-02 | row-delete, table-locate | 2 | 2 | 2 | 1.00 | promotes the next continuation cell to restart but never moves the origin cell's content (notes only that it "is gone"); "remove vMerge entirely" is only valid when no further continuation follows (minor) |
| docx-rowdel-03 | row-delete, cell-edit | 2 | 2 | 2 | 1.00 | cached result + rewrite result run + updateFields; also tax/grand total/amount-in-words |
| docx-rowdel-04 | row-delete, verify-safety | 2 | 2 | 2 | 1.00 | |
| docx-cell-01 | cell-edit | 3 | 2 | 2 | 1.00 | correctly adds that pPr (alignment) is lost too |
| docx-cell-02 | cell-edit, table-locate | 2 | 2 | 2 | 1.00 | |
| docx-cell-03 | cell-edit, verify-safety | 2 | 2 | 2 | 1.00 | three options given; no explicit "check w:delText no longer holds the old value" (minor) |
| docx-relayout-01 | table-relayout | 3 | 3 | 3 | 1.00 | deepcopy + addprevious, text via first run; alternating banding not addressed; "two tables must not be adjacent" overstated (schema-valid, Word merges them visually) |
| docx-relayout-02 | table-relayout | 3 | 3 | 2 | 0.67 | pt1, pt3 ok (take width from an existing column, update tcW incl. gridSpan sums, tblW, fixed layout, copy sibling tcPr). pt2 absent: never describes inserting the gridCol after Item's gridCol and a tc after the Item cell in each row — only discusses add_column's rightmost placement; also wrongly claims add_column leaves merged rows with a cell/grid mismatch (it appends one tc to every row, so Σ gridSpan still equals the grid) |
| docx-relayout-03 | table-relayout, docx-model | 2 | 2 | 2 | 1.00 | |
| docx-tblins-01 | table-insert | 3 | 2 | 2 | 1.00 | correct "before w:sectPr"; "two tables cannot be adjacent (invalid)" overstated (minor) |
| docx-tblins-02 | table-insert | 2 | 2 | 2 | 1.00 | avoids the "Table Grid is guaranteed" trap; lists doc.styles, tblBorders, or import the style |
| docx-tblins-03 | table-insert, table-relayout | 2 | 2 | 2 | 1.00 | default correctly described (even gridCol split, tblW auto), hedged honestly on exact tblW; matching: copies tblPr (incl. style), tblGrid, tcW per cell, a data row as template — header row formatting not called out specifically; mechanism for "copy tblPr/tblGrid" unspecified (minor) |
| docx-imgrep-01 | image-replace, docx-model | 3 | 3 | 2 | 0.67 | pt1 ok; pt2 ok on technique (new part + repoint this blip, keep extents, integer EMU scaling) but tuple order wrong: `image_part, rId = document.part.get_or_add_image(path)` (real: `rId, image = …`); pt3 absent: says "confirm the old relationship/part is gone" as if it leaves on its own — nothing removes it; the rel must be dropped explicitly, and only when no other `r:embed` in that part still uses it |
| docx-imgrep-02 | image-replace | 2 | 2 | 2 | 1.00 | |
| docx-imgrep-03 | image-replace, table-locate | 2 | 2 | 2 | 1.00 | header/first/even parts walked, that part's rels used; anchored pictures not mentioned; offers "or replace the bytes of the part it references" as an alternative — acceptable only because every page should change, contradicts its own imgrep-02 otherwise (minor) |
| docx-imgins-01 | image-insert | 3 | 2 | 2 | 1.00 | technique (add at end, move the w:p after the anchor, width only) correct, but code is broken: `run = doc.add_picture(...)` returns an `InlineShape`, which has no `._r` — use `doc.paragraphs[-1]._p` (or `run.add_picture` on the target paragraph) |
| docx-imgins-02 | image-insert, table-relayout | 2 | 2 | 2 | 1.00 | claims the column never widens ("Word does not shrink it … column width does not" grow) — autofit layout does widen the column; overflow is the fixed-layout case (minor) |
| docx-legacy-01 | legacy-doc | 3 | 2 | 2 | 1.00 | "renaming doesn't help" not stated (minor); stray "Advanced Warning:" phrase |
| docx-legacy-02 | legacy-doc, verify-safety | 2 | 2 | 2 | 1.00 | structural before/after diff is thorough; does not say to fall back to LibreOffice/Word (minor) |
| docx-legacy-03 | legacy-doc | 1 | 2 | 2 | 1.00 | |
| docx-verify-01 | verify-safety | 3 | 2 | 2 | 1.00 | includes byte-identical check of untouched parts and independent-parser read-back |
| docx-verify-02 | verify-safety, docx-model | 2 | 2 | 2 | 1.00 | nine causes incl. schema order, grid mismatch, vMerge, dangling rIds, float EMU; duplicate wp:docPr ids not named (w:id duplicates given instead) |
| docx-verify-03 | verify-safety, image-replace | 2 | 2 | 1 | 0.50 | pt2 ok (hash every word/media part, check every .rels for a stale target, grep rIds, also docProps/thumbnail.jpeg). pt1 absent: never states that removing the w:drawing leaves the rel and media part in the zip and that the rel must be dropped; "confirm the old image part count is exactly one less than before" assumes it disappears on its own |

`normalized = min(awarded, full) / full`

## Per-tag subscores

| tag | subscore | n | trust | action |
|-----|---------:|--:|-------|--------|
| docx-model | 0.94 | 7 | ok | omit (strong) |
| table-locate | 1.00 | 6 | ok | omit (strong) |
| row-delete | 0.83 | 4 | ok | **derive** |
| cell-edit | 1.00 | 4 | ok | omit (strong) |
| table-relayout | 0.93 | 6 | ok | omit (strong) |
| table-insert | 1.00 | 3 | low-n | omit (strong) |
| image-replace | 0.78 | 4 | ok | **derive** |
| image-insert | 1.00 | 2 | low-n | omit (strong) |
| legacy-doc | 1.00 | 3 | low-n | omit (strong) |
| verify-safety | 0.92 | 6 | ok | omit (strong) |

`subscore = Σ(normalized×weight) / Σ(weight)` over that tag's questions.

- docx-model: (3+3+2+2+2+0.67×3+2) / 17 = 16 / 17
- row-delete: (0.5×3+2+2+2) / 9 = 7.5 / 9
- table-relayout: (2+3+0.67×3+2+2+2) / 14 = 13 / 14
- image-replace: (0.67×3+2+2+0.5×2) / 9 = 7 / 9
- verify-safety: (2+2+2+3+2+0.5×2) / 13 = 12 / 13

## Stack score

```
stack_score = Σ(normalized×weight) / Σ(weight) = 64.5 / 69 = 93.5%
```

(Σweight = 12×3 + 16×2 + 1×1 = 69; lost 1.5 on docx-rowdel-01, 1 on
docx-relayout-02, 1 on docx-imgrep-01, 1 on docx-verify-03.)

## Derivation targets

Tags below threshold (`< 0.9`): **row-delete**, **image-replace** → feed into
`derived/docx.step-5-preview-free.SKILL.md`.

### row-delete (0.83)
- rowdel-01 (weight 3, 0.50): the removal itself is right, but the answer stops
  at "verify the row is gone". It never says that the remaining rows reflow by
  themselves (no manual shifting), and never says that a Total or any number
  derived from the deleted row must be recomputed and rewritten. The model
  knows about cached field totals (rowdel-03 is perfect) but does not connect
  the plain delete to that follow-up.
- rowdel-02 (awarded, minor): promotes the next continuation cell to restart but
  leaves the merged cell's content behind; it acknowledges the loss instead of
  moving the paragraphs down.

### image-replace (0.78)
- imgrep-01 (weight 3, 0.67) and verify-03 (0.50) share one gap: the model
  believes the old image relationship and media part vanish once nothing
  displays them ("confirm the old relationship/part is gone"; "old image part
  count is exactly one less than before"). python-docx saves every part still
  reachable through a rel, so after repointing a blip or removing a w:drawing
  the old `word/media/imageN.*` stays in the package until the rel is dropped
  from the owning part, and only when no other `r:embed` in that part still
  names it.
- imgrep-01 API slip: `image_part, rId = document.part.get_or_add_image(path)`.
  The method returns `(rId, image)`.
- imgrep-03 (awarded, minor): offers "replace the bytes of the part it
  references" as an alternative to repointing, which the model itself rules out
  in imgrep-02 for shared parts.

## Contamination check

No verbatim overlap with the reference answers. The answers are longer, in
the model's own structure, and carry independent errors the key does not have
(get_or_add_image tuple order, `doc.add_picture` treated as returning a run,
"add_column breaks merged rows", "old rel is gone on its own"). Two weak
coincidences noted for the record: the run-split example uses the same
"Wid" + "get C" boundary as the key (a natural split of the question's own
"Widget C"), and the throwaway profile is named `lo_profile_unique` vs the key's
`lo_profile_$$` (a common naming pattern). Neither is treated as contamination
given the audited zero tool calls.
