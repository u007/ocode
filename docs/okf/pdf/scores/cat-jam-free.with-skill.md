---
model_id: cat-jam-free
model_version: "alpha"
evaluated_via: opencode-go
evaluated_on: 2026-10-14
stack: pdf
stack_corpus_rev: 1
threshold: 0.9
validation: with-skill
---

<!-- Filename: model_id with "/" flattened to "__" so it is one valid path
     segment. `cat-jam-free` has no slash, so the filename is unchanged. -->

# Scorecard — cat-jam-free on pdf (with-skill validation)

> Valid ONLY for `cat-jam-free` @ `alpha`. A version bump invalidates
> this scorecard — re-benchmark.

> **WITH-SKILL VALIDATION RUN.** Answers: `pdf/answers/cat-jam-free.with-skill.md`,
> produced closed-book with the current derived skill
> `derived/pdf.cat-jam-free.SKILL.md` (`pdf-tuning-cat-jam-free`) prepended to
> the `_prompts/pdf.md` sheet as active guidance (session audited: 1 user
> message, 1 assistant message, 0 tool calls). Baseline:
> `scores/cat-jam-free.md`. No new derived skill is produced from this run.

> Target tags for this skill (baseline < 0.9): **pdf-model, row-delete,
> cell-edit, table-relayout, table-insert, image-replace (low-n),
> verify-safety**.

Graded against `pdf/questions.yaml` @ corpus_rev 1 (PyMuPDF facts in the key
verified on 1.27.1). Grading rule applied (same strictness as the baseline): a
`point` whose core concept is genuinely present earns full credit (minor missing
sub-items are noted); a `point` stated together with a materially wrong claim
about the same construct is withheld and the note says "(judgment)". Points are
whole; fractions come only from a listed partial.

## History

- Earlier with-skill scorecard: none (this is the first with-skill validation for
  cat-jam-free on pdf).

## Per-question results

| id | tags | weight | full | awarded | normalized | notes |
|----|------|-------:|-----:|--------:|-----------:|-------|
| pdf-model-01 | pdf-model | 3 | 2 | 2 | 1.00 | no "cover" hedge this time: remove marks, shift below, redraw |
| pdf-model-02 | pdf-model, verify-safety | 3 | 2 | 2 | 1.00 | |
| pdf-model-03 | pdf-model | 2 | 2 | 2 | 1.00 | now offers a remedy (structural editor / regenerate) |
| pdf-model-04 | pdf-model | 2 | 2 | 2 | 1.00 | baseline rotation error fixed: bboxes and insert rects stay unrotated, `rotation_matrix`/`derotation_matrix`, CropBox origin, `transformation_matrix` |
| pdf-locate-01 | table-locate | 3 | 2 | 2 | 1.00 | |
| pdf-locate-02 | table-locate | 2 | 2 | 2 | 1.00 | still never checks the hit is unique (same as baseline) |
| pdf-locate-03 | table-locate, cell-edit | 2 | 2 | 2 | 1.00 | colour-int conversion not mentioned |
| pdf-locate-04 | table-locate, table-relayout | 2 | 2 | 2 | 1.00 | x1 as the right edge; "x = edge − width" left implicit (baseline equivalent earned 2) |
| pdf-rowdel-01 | row-delete | 3 | 3 | 3 | 1.00 | baseline point-1 loss fixed: redacts deleted row AND rows below, Total and content beneath, then restamps via `show_pdf_page(clip=)` or redraws; Total recomputed |
| pdf-rowdel-02 | row-delete, cell-edit | 3 | 2 | 2 | 1.00 | defaults stated exactly (2 / 1 / 0) and `images=0, graphics=0` keep flags |
| pdf-rowdel-03 | row-delete | 2 | 2 | 2 | 1.00 | baseline "no native move / pixmap" error gone: untouched copy, redact old region, `show_pdf_page(..., clip=)`, Form XObject stays extractable |
| pdf-rowdel-04 | row-delete, verify-safety | 2 | 2 | 2 | 1.00 | row numbers, footnotes, overflow; striping not named |
| pdf-cell-01 | cell-edit | 3 | 3 | 3 | 1.00 | now passes `images=0, graphics=0` |
| pdf-cell-02 | cell-edit | 2 | 2 | 2 | 1.00 | "a little padding" but bounded inside the cell and away from neighbours; not the "bigger to be safe" partial. No explicit verify-neighbours step |
| pdf-cell-03 | cell-edit, table-relayout | 2 | 2 | 2 | 1.00 | baseline wrong claim fixed: negative height shortfall, writes nothing, no exception; all four options incl. `insert_htmlbox` |
| pdf-cell-04 | cell-edit | 2 | 2 | 2 | 1.00 | "too high by roughly the font ascent" — direction now unhedged |
| pdf-relayout-01 | table-relayout | 3 | 3 | 3 | 1.00 | redact Total + below, redraw, Total recomputed one row lower, content below restamped, overflow to another page. Striping colour of the new row only implied by "its shading" |
| pdf-relayout-02 | table-relayout | 3 | 3 | 3 | 1.00 | baseline points 2 and 3 fixed: measures at new widths with `get_text_length`, per-column shrink/wrap, redacts whole table, redraws header/shading/rules/text. Mistake named as widening the page / drawing only a new rule |
| pdf-relayout-03 | table-relayout, pdf-model | 2 | 2 | 2 | 1.00 | last resort; ask for source and regenerate first |
| pdf-relayout-04 | table-relayout, table-insert | 2 | 2 | 2 | 1.00 | text + drawings + images + bottom margin |
| pdf-tblins-01 | table-insert | 3 | 2 | 2 | 1.00 | now says stamped text stays real; rejects rasterizing |
| pdf-tblins-02 | table-insert | 2 | 2 | 2 | 1.00 | paint order now stated (backgrounds before text) |
| pdf-tblins-03 | table-insert, table-relayout | 2 | 2 | 2 | 1.00 | baseline point-1 loss fixed: redact + restamp lower with `show_pdf_page(clip=)`, overflow onto inserted page; free space / new page + tell user; never overlap. Regenerate-from-source not mentioned here |
| pdf-imgrep-01 | image-replace | 3 | 3 | 3 | 1.00 | baseline points 1 and 3 fixed: `replace_image(xref, filename=)`, single occurrence via redact `IMAGE_REMOVE` + `insert_image`, aspect-ratio stretch caveat |
| pdf-imgrep-02 | image-replace, verify-safety | 2 | 2 | 2 | 1.00 | no transparency/bloat mention; no "verify exactly one image" (same as baseline) |
| pdf-imgrep-03 | image-replace | 2 | 2 | 2 | 1.00 | vector / inline / Form XObject; annotation appearance not named |
| pdf-imgins-01 | image-insert | 3 | 2 | 2 | 1.00 | anchors to page.rect − margin rather than real content edges (same as baseline), then collision-checks |
| pdf-imgins-02 | image-insert | 2 | 2 | 1 | 0.50 | point 1 earned loosely (reuse one image xref across pages; cites "doc.new_image", not a PyMuPDF API, hedged "or related xref handling"; never names `insert_image(..., xref=)`). Point 2 lost: no garbage/dedup save — the baseline's "dedup optimizer" mention is gone. Non-target drift |
| pdf-imgins-03 | image-insert, pdf-model | 2 | 2 | 2 | 1.00 | overlay/paint order; rect, CropBox/MediaBox, rotation |
| pdf-fonts-01 | fonts | 3 | 2 | 2 | 1.00 | no glyph-coverage check or render verification (same as baseline) |
| pdf-fonts-02 | fonts | 2 | 2 | 2 | 1.00 | no "embed a TTF" remedy stated |
| pdf-fonts-03 | fonts, cell-edit | 2 | 2 | 2 | 1.00 | flags named, bold bit not |
| pdf-verify-01 | verify-safety | 3 | 3 | 3 | 1.00 | baseline point-3 loss fixed: text layer first (old [] / new present / `find_tables().extract()` / other pages + page count), render second, "a render alone cannot prove a real edit versus an overlay" |
| pdf-verify-02 | verify-safety | 3 | 2 | 2 | 1.00 | |
| pdf-verify-03 | verify-safety | 2 | 2 | 2 | 1.00 | signatures/encryption now present; other pages untouched and metadata not mentioned |
| pdf-verify-04 | verify-safety, pdf-model | 2 | 2 | 1 | 0.50 | point 1 earned (negative `insert_textbox`, empty `search_for`, `apply_redactions` False, stale/duplicated text). Point 2 withheld, same as baseline: lists the failing return values but never says to check them in the script and fail loudly; only "reopen and inspect" |

`normalized = min(awarded, full) / full`

## Per-tag subscores

| tag | subscore | n | trust | target? | BASELINE | delta |
|-----|---------:|--:|-------|---------|---------:|------:|
| pdf-model | 0.94 | 7 | ok | **yes** | 0.81 | +0.13 |
| table-locate | 1.00 | 4 | ok | no | 1.00 | 0.00 |
| row-delete | 1.00 | 4 | ok | **yes** | 0.60 | +0.40 |
| cell-edit | 1.00 | 7 | ok | **yes** | 0.84 | +0.16 |
| table-relayout | 1.00 | 7 | ok | **yes** | 0.69 | +0.31 |
| table-insert | 1.00 | 4 | ok | **yes** | 0.89 | +0.11 |
| image-replace | 1.00 | 3 | low-n | **yes** | 0.71 | +0.29 |
| image-insert | 0.86 | 3 | low-n | no | 1.00 | −0.14 |
| fonts | 1.00 | 3 | low-n | no | 1.00 | 0.00 |
| verify-safety | 0.94 | 7 | ok | **yes** | 0.88 | +0.06 |

`subscore = Σ(normalized×weight) / Σ(weight)` over that tag's questions.

Arithmetic: pdf-model 15/16 (verify-04 −1.0); table-locate 9/9; row-delete
10/10; cell-edit 16/16; table-relayout 16/16; table-insert 9/9; image-replace
7/7; image-insert 6/7 (imgins-02 −1.0); fonts 7/7; verify-safety 16/17
(verify-04 −1.0).

## Stack score

```
stack_score = Σ(normalized×weight) / Σ(weight) = 84 / 86 = 97.7%
```

(Losses: imgins-02 −1.0, verify-04 −1.0.) Baseline 72 / 86 = 83.7%.

## Target-tag verdict

| tag | baseline | with-skill | pass (≥ 0.9)? |
|-----|---------:|-----------:|---------------|
| pdf-model | 0.81 | 0.94 | **PASS** |
| row-delete | 0.60 | 1.00 | **PASS** |
| cell-edit | 0.84 | 1.00 | **PASS** |
| table-relayout | 0.69 | 1.00 | **PASS** |
| table-insert | 0.89 | 1.00 | **PASS** |
| image-replace (low-n) | 0.71 | 1.00 | **PASS** |
| verify-safety | 0.88 | 0.94 | **PASS** |

**PASS.** Every target tag reaches 0.9. The skill is validated for
cat-jam-free @ alpha. Every concrete wrong claim listed under "Derivation
targets" in the baseline scorecard is absent from the with-skill answers:
rotated-page coordinates (model-04), shift-without-removal (rowdel-01),
apply_redactions defaults and keep flags (rowdel-02, cell-01), the
pixmap/"no native move" region move (rowdel-03), the `insert_textbox` return
value (cell-03), column relayout without measuring or redrawing (relayout-02),
round-trip for "simple" documents (relayout-03), no-gap insertion (tblins-03),
`replace_image` and the aspect caveat (imgrep-01), and render-first
verification (verify-01).

One target-tag point is still missing, and it is the same one as in the
baseline: verify-04 point 2 (check return values in the script and fail
loudly). The skill states it ("Check each return value in the script and
raise"), but the answer only enumerates the silent failures and says to reopen
and inspect. It costs pdf-model and verify-safety 1/16 and 1/17 each; both stay
above the threshold, so no skill tweak is required. If a future rerun drops
either tag below 0.9 on this question, sharpen digest item 4 / the pdf-model
section into an explicit "assert on every return value" directive.

Non-target drift (not a reason to edit the skill): image-insert 1.00 → 0.86 on
imgins-02, which lost the garbage/dedup point the baseline earned and cites a
non-existent `doc.new_image`. The skill body says nothing about image dedup, so
this is single-sample resampling noise; per HOW-TO-EVALUATE.md, catch it in a
fresh baseline before deriving anything.

## Provenance

Answerer: `ocode run -m opencode-go/cat-jam-free -effort med` closed-book with
an isolated HOME (every tool denied) and a cwd holding only the `_prompts/pdf.md`
sheet, with the body of `derived/pdf.cat-jam-free.SKILL.md` (front matter
stripped) prepended as active guidance; the session sqlite shows exactly 1 user
message, 1 assistant message and 0 tool calls, so no repo file, answer key or web
page was read. All 36 ids present, none truncated. The baseline's
"PyMuFITZ" misspelling does not recur; the with-skill answers echo the skill's
wording closely where it applies (expected: the skill is in context) but do not
reproduce reference-answer phrasing for questions the skill does not cover.
Graded 2026-10-14 against `pdf/questions.yaml` corpus_rev 1 by the same rubric
and strictness as `scores/cat-jam-free.md`.
