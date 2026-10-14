---
model_id: cat-jam-free
model_version: "alpha"
evaluated_via: opencode-go
evaluated_on: 2026-10-14
stack: pptx
stack_corpus_rev: 1
threshold: 0.9
---

# Scorecard — cat-jam-free on pptx

> Valid ONLY for `cat-jam-free` @ `alpha`. A version bump invalidates
> this scorecard — re-benchmark.

## Provenance

Answers: `pptx/answers/cat-jam-free.md`, produced closed-book by
`ocode run -m opencode-go/cat-jam-free -effort med` in an empty scratch
directory with only the `_prompts/pptx.md` sheet attached. The session sqlite
shows exactly 1 user + 1 assistant message and 0 tool calls. Headless runs
inject ocode's bundled skill-catalogue digest into the system prompt. No
published version exists for this opencode-go free/alpha model, so the version
is recorded as `alpha`; the model self-identifies as "CatJam, developed by
LitterBox AI". Graded against `pptx/questions.yaml` corpus_rev 1 (python-pptx
facts verified on 1.0.2).

Grading rules applied (same as the other pptx scorecards): a `point` whose core
concept is present earns full credit (minor missing sub-items are noted); a
`point` only half present earns 0.5; a `point` stated alongside a materially
wrong claim about the same construct earns 0 and is marked "(judgment)";
tangential wrong asides go in the notes only. For table-locate, picking the
table by header AND asserting a unique row are both required for point 2. For
legacy-ppt, delivering the format the user asked for (convert back to `.ppt`
when required) is required for the second point of both questions; the answers
never offer it and legacy-02 states the result is `.pptx`, so both are halved
(judgment). This differs from relayout-02's missing frame-width update, which
is a mechanical consequence of a width rebalance that the answer does state.

## Per-question results

| id | tags | weight | full | awarded | normalized | notes |
|----|------|-------:|-----:|--------:|-----------:|-------|
| pptx-model-01 | pptx-model | 3 | 2 | 2 | 1.00 | gridCol `w` / tr `h` attributes not named (minor) |
| pptx-model-02 | pptx-model | 2 | 2 | 1.5 | 0.75 | EMU 914400/inch and 16:9 size correct (12700/pt not named, minor); never says to read `prs.slide_width/height` instead of assuming the size |
| pptx-model-03 | pptx-model, image-replace | 2 | 2 | 2 | 1.00 | no single-slide override / tell-the-user scope decision (minor) |
| pptx-locate-01 | table-locate | 3 | 2 | 1 | 0.50 | `has_table` → `shape.table` met (no group recursion, minor); takes the slide's table without choosing it by header text and never asserts a unique "Gadget D" match → point 2 missed |
| pptx-locate-02 | table-locate, pptx-model | 2 | 2 | 0 | 0.00 | (judgment) point 1 withheld: gridSpan/rowSpan named, but "covered cells may be omitted or present … depending on the producer" — every `a:tr` has one `a:tc` per `a:gridCol`, covered cells carry hMerge/vMerge (never named). Point 2 = 0: "a spanned cell counts as multiple columns … keep each row's total grid width consistent" is the span-weighted coverage model; never says to write text to the origin or to adjust spans and covered cells together |
| pptx-rowdel-01 | row-delete | 3 | 2 | 2 | 1.00 | `table._tbl.remove(row._tr)` correct; totals not named (minor) |
| pptx-rowdel-02 | row-delete, table-relayout | 3 | 2 | 2 | 1.00 | aside: "clip the table if the table is now shorter" (a shorter table cannot be clipped; tangential) |
| pptx-rowdel-03 | row-delete, cell-edit | 2 | 2 | 1 | 0.50 | direct fills vs style banding explained (point 1); no fix — never says to re-apply fills to the following rows or switch to style banding → point 2 missed |
| pptx-rowdel-04 | row-delete, verify-safety | 2 | 2 | 1.5 | 0.75 | text stays extractable (point 1; rows growing to fit text not named, minor). Point 2 half: "a real row deletion removes the data", but no check that the old text is absent from every slide part |
| pptx-cell-01 | cell-edit | 3 | 2 | 2 | 1.00 | |
| pptx-cell-02 | cell-edit, table-relayout | 2 | 2 | 2 | 1.00 | neighbouring shapes not named (minor); aside: "fixed versus automatic table layout" / "unless autofit changes them" are not pptx table concepts (tangential) |
| pptx-cell-03 | cell-edit | 2 | 2 | 2 | 1.00 | keep number format not named (minor) |
| pptx-relayout-01 | table-relayout | 3 | 3 | 1.5 | 0.50 | deepcopy + insert before Total met. Point 2 = 0.5: totals and frame height updated, but "replace text" never says through the existing runs. Point 3 = 0: no check of the frame's new bottom against the slide height or the shapes below (fills recalculated) |
| pptx-relayout-02 | table-relayout | 3 | 3 | 3 | 1.00 | gridCol + a:tc in every row, Total span adjusted, copy an existing cell, shrink a column to fit the slide's text area; frame width update and header-vs-body copy not named (minor) |
| pptx-tblins-01 | table-insert | 3 | 2 | 2 | 1.00 | |
| pptx-tblins-02 | table-insert | 2 | 2 | 1.5 | 0.75 | point 1 half: copy the style, but calls it "a:tblPr tblStyle" (the element is `a:tableStyleId`), no default Medium Style 2, no firstRow/bandRow flags. Point 2 met (fonts, direct cell formatting) |
| pptx-tblins-03 | table-insert, pptx-model | 1 | 2 | 0.5 | 0.25 | (judgment) point 1 = 0: claims `insert_table` on a generic content placeholder; only `TablePlaceholder` has it. Point 2 = 0.5: add_table at the placeholder bbox, but the empty placeholder is never removed |
| pptx-imgrep-01 | image-replace | 3 | 3 | 2 | 0.67 | add_picture with old geometry (passes both width and height — stretches a different aspect, minor) or repoint blip; z-order restore + remove old met; old image rel never dropped → point 3 missed |
| pptx-imgrep-02 | image-replace | 2 | 2 | 2 | 1.00 | SHA1 dedupe not named (minor) |
| pptx-imgrep-03 | image-replace, verify-safety | 2 | 2 | 1 | 0.50 | mechanism point 0: says drop_rel removes "without checking whether other shapes still reference" it; it does ref-count but matches only `r:id`, so `a:blip r:embed` counts 0 and image rels are always dropped. Own r:embed check before dropping met (r:link not named) |
| pptx-imgins-01 | image-insert | 3 | 2 | 2 | 1.00 | |
| pptx-imgins-02 | image-insert | 2 | 2 | 2 | 1.00 | attributes the crop to "PowerPoint" rather than python-pptx's insert_picture (minor) |
| pptx-legacy-01 | legacy-ppt | 3 | 2 | 1.5 | 0.75 | point 1 met (OOXML only; OLE2 not named, minor). Point 2 half (judgment): convert with LibreOffice headless and edit, but no command and no convert-back to `.ppt` when the user needs it |
| pptx-legacy-02 | legacy-ppt, verify-safety | 2 | 2 | 1.5 | 0.75 | point 1 met. Point 2 half (judgment): tells the user and keeps the original, but declares "the result is .pptx" instead of returning the requested format |
| pptx-verify-01 | verify-safety | 3 | 2 | 1.5 | 0.75 | new output + original untouched (point 1). Point 2 half: re-open, assert expected text, render; never asserts the other slides/shapes are unchanged or searches for text that should be gone |
| pptx-verify-02 | verify-safety, pptx-model | 2 | 2 | 2 | 1.00 | grid/merge mismatch, missing children, schema order, dangling rels met; duplicate cNvPr ids and "python-pptx does not validate" not named (minor) |
| pptx-verify-03 | verify-safety, image-replace | 2 | 2 | 2 | 1.00 | unzip -l + rels inspection, remove orphans; no hash compare (minor) |

`normalized = min(awarded, full) / full`

## Per-tag subscores

| tag | subscore | n | trust | action |
|-----|---------:|--:|-------|--------|
| pptx-model | 0.73 | 6 | ok | **derive** |
| table-locate | 0.30 | 2 | low-n | **derive** (low-n) |
| row-delete | 0.85 | 4 | ok | **derive** |
| cell-edit | 0.89 | 4 | ok | **derive** |
| table-relayout | 0.86 | 4 | ok | **derive** |
| table-insert | 0.79 | 3 | low-n | **derive** (low-n) |
| image-replace | 0.82 | 5 | ok | **derive** |
| image-insert | 1.00 | 2 | low-n | omit (strong) |
| legacy-ppt | 0.75 | 2 | low-n | **derive** (low-n) |
| verify-safety | 0.79 | 6 | ok | **derive** |

`subscore = Σ(normalized×weight) / Σ(weight)` over that tag's questions.

- pptx-model: (3+1.5+2+0+0.25+2) / 12 = 8.75 / 12 = 0.729
- table-locate: (1.5+0) / 5 = 0.30
- row-delete: (3+3+1+1.5) / 10 = 8.5 / 10 = 0.85
- cell-edit: (1+3+2+2) / 9 = 8 / 9 = 0.889
- table-relayout: (3+2+1.5+3) / 11 = 9.5 / 11 = 0.864
- table-insert: (3+1.5+0.25) / 6 = 4.75 / 6 = 0.792
- image-replace: (2+2+2+1+2) / 11 = 9 / 11 = 0.818
- image-insert: (3+2) / 5 = 1.00
- legacy-ppt: (2.25+1.5) / 5 = 3.75 / 5 = 0.75
- verify-safety: (1.5+1+1.5+2.25+2+2) / 13 = 10.25 / 13 = 0.788

## Stack score

```
stack_score = Σ(normalized×weight) / Σ(weight) = 52.75 / 65 = 81.2%
```

(Lost weight: model-02 0.5, locate-01 1.5, locate-02 2.0, rowdel-03 1.0,
rowdel-04 0.5, relayout-01 1.5, tblins-02 0.5, tblins-03 0.75, imgrep-01 1.0,
imgrep-03 1.0, legacy-01 0.75, legacy-02 0.5, verify-01 0.75 = 12.25.)

## Derivation targets

Tags below threshold (`< 0.9`): **pptx-model** (0.73), **table-locate** (0.30,
low-n), **row-delete** (0.85), **cell-edit** (0.89), **table-relayout** (0.86),
**table-insert** (0.79, low-n), **image-replace** (0.82), **legacy-ppt** (0.75,
low-n), **verify-safety** (0.79) → `derived/pptx.cat-jam-free.SKILL.md`.

- **pptx-model / table-locate (merges):** believes covered merge cells may be
  omitted and that a span "counts as multiple columns" toward a row's width;
  never names hMerge/vMerge or writing to the origin. Every `a:tr` has one
  `a:tc` per `a:gridCol`.
- **table-locate:** takes the table without matching its header text and never
  asserts a unique row match.
- **table-insert / pptx-model (placeholders):** puts `insert_table` on a generic
  content placeholder (only `TablePlaceholder` has it); never removes the empty
  placeholder after `add_table` at its bbox; misnames the style element and
  does not know the default style GUID/flags.
- **row-delete / cell-edit:** explains why explicit fills break banding but
  gives no fix; no "old text gone from every slide part" check after a delete.
- **table-relayout:** after inserting a row, no bottom-vs-slide/neighbour check;
  text not written through existing runs.
- **image-replace / verify-safety:** never drops the old image rel; wrong
  `drop_rel` mechanism; save-and-verify never checks the unchanged remainder or
  searches for removed text.
- **legacy-ppt:** no convert-back to `.ppt`; tells the user the deliverable is
  `.pptx`.
- **pptx-model (units):** quotes 16:9 dimensions instead of reading
  `prs.slide_width/height`.

## Anomalies

- No truncation: all 27 ids answered, every answer complete.
- No verbatim overlap with the reference answers; wording and the recurring
  misconceptions (optional covered cells, `insert_table` on any content
  placeholder, "`drop_rel` doesn't check") are independent of the key. No
  contamination signal.
- Answers are short (about 9.5 KB total) and consistently omit the
  follow-through step (fix, verify, deliver) after a correct diagnosis.
