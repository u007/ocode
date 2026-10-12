---
model_id: step-5-preview-free
model_version: "5-preview"
evaluated_via: opencode-go
evaluated_on: 2026-10-12
stack: pptx
stack_corpus_rev: 1
threshold: 0.9
---

# Scorecard — step-5-preview-free on pptx

> Valid ONLY for `step-5-preview-free` @ `5-preview`. A version bump invalidates
> this scorecard — re-benchmark.

## Provenance

Answers: `pptx/answers/step-5-preview-free.md`, produced closed-book by
`ocode run -m opencode-go/step-5-preview-free -effort med` in an empty scratch
directory with only the `_prompts/pptx.md` sheet attached. The session sqlite
shows exactly 1 user + 1 assistant message and 0 tool calls. A first attempt
returned an empty assistant message and was re-run; only the successful run is
graded. The model appended a short self-summary after the YAML list; it is not
graded. Graded against `pptx/questions.yaml` corpus_rev 1 (python-pptx facts
verified on 1.0.2).

Grading rules applied (same as the other pptx scorecards): a `point` whose core
concept is present earns full credit (minor missing sub-items are noted); a
`point` stated correctly but accompanied by a materially wrong claim about the
same construct earns 0 and is marked "(judgment)"; tangential wrong asides go in
the notes only. For the table-locate question, picking the table by header AND
asserting a unique row are both required for the second point.

## Per-question results

| id | tags | weight | full | awarded | normalized | notes |
|----|------|-------:|-----:|--------:|-----------:|-------|
| pptx-model-01 | pptx-model | 3 | 2 | 2 | 1.00 | |
| pptx-model-02 | pptx-model | 2 | 2 | 2 | 1.00 | also notes python-pptx's default template is 4:3 (correct) |
| pptx-model-03 | pptx-model, image-replace | 2 | 2 | 2 | 1.00 | aside: `p:hideSp` is not a real attribute (`showMasterSp` is); tangential |
| pptx-locate-01 | table-locate | 3 | 2 | 1 | 0.50 | takes the first `has_table` shape and offers `shape.name`/position to disambiguate, never the header text; `break` on first row hit, no unique-match assert → point 2 missed. No group recursion (minor). Aside: claims merged cells "repeat their text across the spanned columns" (covered cells are empty) |
| pptx-locate-02 | table-locate, pptx-model | 2 | 2 | 1 | 0.50 | (judgment) representation point 0: gridSpan/hMerge/rowSpan/vMerge named correctly, but says a row needs "enough a:tc cells to cover the gridCol count *after accounting for spans*" and that inserting a column means "bump gridSpan *or* add an hMerge cell" — every a:tr has exactly one a:tc per a:gridCol, spans never reduce the count; fix-spans/edit-origin point met |
| pptx-rowdel-01 | row-delete | 3 | 2 | 2 | 1.00 | frame height fixed; totals not named (only shading/merges) (minor) |
| pptx-rowdel-02 | row-delete, table-relayout | 3 | 2 | 2 | 1.00 | |
| pptx-rowdel-03 | row-delete, cell-edit | 2 | 2 | 2 | 1.00 | |
| pptx-rowdel-04 | row-delete, verify-safety | 2 | 2 | 2 | 1.00 | removes a:tr and scrubs thumbnail/notes/docProps; no explicit "old text absent from every slide part" check (minor) |
| pptx-cell-01 | cell-edit | 3 | 2 | 2 | 1.00 | |
| pptx-cell-02 | cell-edit, table-relayout | 2 | 2 | 2 | 1.00 | aside: offers `a:normAutofit`/`wrap="none"` on the cell bodyPr as levers; table cells do not autofit (tangential) |
| pptx-cell-03 | cell-edit | 2 | 2 | 2 | 1.00 | chart `c:numCache` named; speaker notes and "keep number format/alignment" not named (minor) |
| pptx-relayout-01 | table-relayout | 3 | 3 | 2 | 0.67 | deepcopy + `addprevious` before Total, runs edited, total + frame cy updated, banding re-fill named; no check of the new bottom against the slide height or the shapes below → point 3 missed |
| pptx-relayout-02 | table-relayout | 3 | 3 | 2 | 0.67 | (judgment) point 1 withheld: gridCol + a:tc in every row at the right index stated, but for merged rows says "add a covered tc with hMerge, *or* extend the merge by bumping gridSpan" and verifies "cell/span coverage equal to the gridCol count" — both must be done and the tc count must equal the gridCol count regardless of spans; formatting copy and width rebalance (take width from the Item column, `sum(w) == cx`, `left + cx <= slide_width`) met |
| pptx-tblins-01 | table-insert | 3 | 2 | 2 | 1.00 | |
| pptx-tblins-02 | table-insert | 2 | 2 | 2 | 1.00 | default GUID correct; aside: says a copied built-in style GUID only works if defined in `tableStyles.xml` (PowerPoint knows the built-ins) (tangential) |
| pptx-tblins-03 | table-insert, pptx-model | 1 | 2 | 1 | 0.50 | insert_table point 0: states "there is no `insert_table` on placeholders in python-pptx (only `insert_picture`)" — `TablePlaceholder.insert_table(rows, cols)` exists; add_table-at-bbox + remove placeholder point met |
| pptx-imgrep-01 | image-replace | 3 | 3 | 2 | 0.67 | repoints `a:blip r:embed` to a new image part (aspect caveat given) and describes re-inserting a new p:pic at the old index as the alternative; never drops the old image rel → point 3 missed. Aside: `blip.embed` is not the python-pptx attribute (`rEmbed`) |
| pptx-imgrep-02 | image-replace | 2 | 2 | 2 | 1.00 | one muddled sentence ("relating to it and swapping the rId … changes the single shared blob"); conclusion and fix correct |
| pptx-imgrep-03 | image-replace, verify-safety | 2 | 2 | 1 | 0.50 | mechanism point 0: says `drop_rel` "has no reference counting" and blames a cloned p:pic sharing one rId; in fact `_rel_ref_count` exists but matches only `//@r:id`, so `a:blip r:embed` counts 0 and image rels are always dropped. Own r:embed count before dropping met; save-time "unreachable part not written" correct |
| pptx-imgins-01 | image-insert | 3 | 2 | 2 | 1.00 | |
| pptx-imgins-02 | image-insert | 2 | 2 | 2 | 1.00 | hedges on the exact crop arithmetic; mechanism and fix correct |
| pptx-legacy-01 | legacy-ppt | 3 | 2 | 2 | 1.00 | aside: ".ppt … BIFF-based records" (BIFF is Excel's record format) (tangential) |
| pptx-legacy-02 | legacy-ppt, verify-safety | 2 | 2 | 2 | 1.00 | tells user + keeps original; for a .ppt deliverable recommends redoing the edit in PowerPoint rather than converting back (minor) |
| pptx-verify-01 | verify-safety | 3 | 2 | 2 | 1.00 | |
| pptx-verify-02 | verify-safety, pptx-model | 2 | 2 | 2 | 1.00 | schema order + dangling rId present; no duplicate cNvPr ids, no tc-without-txBody, no "python-pptx doesn't validate" (minor); row/grid mismatch again phrased "after accounting for spans" |
| pptx-verify-03 | verify-safety, image-replace | 2 | 2 | 2 | 1.00 | lists `ppt/media` and maps every r:embed; no hash compare (minor); thumbnail named |

`normalized = min(awarded, full) / full`

## Per-tag subscores

| tag | subscore | n | trust | action |
|-----|---------:|--:|-------|--------|
| pptx-model | 0.88 | 6 | ok | **derive** |
| table-locate | 0.50 | 2 | low-n | **derive** (low-n) |
| row-delete | 1.00 | 4 | ok | omit (strong) |
| cell-edit | 1.00 | 4 | ok | omit (strong) |
| table-relayout | 0.82 | 4 | ok | **derive** |
| table-insert | 0.92 | 3 | low-n | omit |
| image-replace | 0.82 | 5 | ok | **derive** |
| image-insert | 1.00 | 2 | low-n | omit (strong) |
| legacy-ppt | 1.00 | 2 | low-n | omit (strong) |
| verify-safety | 0.92 | 6 | ok | omit |

`subscore = Σ(normalized×weight) / Σ(weight)` over that tag's questions.

- pptx-model: (3+2+2+1.0+0.5+2) / 12 = 10.5 / 12 = 0.875
- table-locate: (1.5+1.0) / 5 = 2.5 / 5 = 0.50
- row-delete: (3+3+2+2) / 10 = 1.00
- cell-edit: (2+3+2+2) / 9 = 1.00
- table-relayout: (3+2+2.0+2.0) / 11 = 9.0 / 11 = 0.818
- table-insert: (3+2+0.5) / 6 = 5.5 / 6 = 0.917
- image-replace: (2+2.0+2+1.0+2) / 11 = 9.0 / 11 = 0.818
- image-insert: (3+2) / 5 = 1.00
- legacy-ppt: (3+2) / 5 = 1.00
- verify-safety: (2+1.0+2+3+2+2) / 13 = 12.0 / 13 = 0.923

## Stack score

```
stack_score = Σ(normalized×weight) / Σ(weight) = 58.0 / 65 = 89.2%
```

(Lost weight: locate-01 1.5, locate-02 1.0, relayout-01 1.0, relayout-02 1.0,
tblins-03 0.5, imgrep-01 1.0, imgrep-03 1.0.)

## Derivation targets

Tags below threshold (`< 0.9`): **pptx-model** (0.88), **table-locate** (0.50,
low-n), **table-relayout** (0.82), **image-replace** (0.82) →
`derived/pptx.step-5-preview-free.SKILL.md`.

- **table-locate:** takes the first `has_table` shape (disambiguating by
  `shape.name` or position, never by header text), breaks on the first row hit
  without asserting exactly one match, does not recurse into groups, and
  believes covered merge cells repeat the origin's text.
- **pptx-model (merges):** recurring span-weighted "coverage" model — a row
  needs enough `a:tc` "after accounting for spans", a column insert through a
  merge is "bump gridSpan OR add an hMerge cell". Every `a:tr` has exactly one
  `a:tc` per `a:gridCol`; the covered cell carries `hMerge`/`vMerge` and the
  origin's span is bumped in the same edit.
- **pptx-model (placeholders):** asserts `insert_table` does not exist on any
  placeholder. `TablePlaceholder.insert_table(rows, cols)` exists and returns
  a `PlaceholderGraphicFrame`; only a generic content placeholder lacks it.
- **table-relayout:** after inserting a row, never checks the frame's new
  bottom against `prs.slide_height` and the shapes below; column insert through
  a merge inherits the coverage misconception above.
- **image-replace:** after repointing `r:embed` (or removing a `p:pic`) the old
  image rel is never dropped, so the old bytes stay in `ppt/media`; believes
  `drop_rel` has no reference counting at all (it does, but it only matches
  `r:id`, so image rels are always dropped) and blames a cloned pic sharing an
  rId.

## Anomalies

- No truncation: all 27 ids answered, every answer complete.
- No verbatim overlap with the reference answers; wording, code shapes and
  the recurring misconceptions (span-weighted tc coverage, "no `insert_table`",
  "`drop_rel` has no ref count") are independent of the key. No contamination
  signal.
- Two answers carry an explicit "verify against your version" hedge
  (`pptx-imgins-02`, `pptx-imgrep-03`); neither was penalised for hedging, the
  `imgrep-03` loss is the wrong mechanism.
- The file ends with a stray ```` ``` ```` fence and a five-line self-summary
  after the YAML list, outside any record; ignored per the run notes.
