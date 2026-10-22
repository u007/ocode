---
model_id: deepseek-v4.1-flash
model_version: "4.1-2026-10-21"
evaluated_via: opencode-go
evaluated_on: 2026-10-21
stack: pdf
stack_corpus_rev: 1
threshold: 0.9
sample: full   # all 36 questions
---

# Scorecard — deepseek-v4.1-flash on pdf

> Valid ONLY for `deepseek-v4.1-flash` @ `4.1-2026-10-21`. A version bump invalidates
> this scorecard — re-benchmark.

Answers: `../answers/deepseek-v4.1-flash.md`, produced closed-book (see Provenance).
Graded against `questions.yaml` (corpus_rev 1; PyMuPDF facts verified on 1.27.1).
Per the corpus header, an equivalent correct technique in another library earns the
point; wrong PyMuPDF behaviour claims do not; a cover-up (white box, hidden or
clipped-away text) earns 0 for the removal point. A materially wrong claim about the
construct a point tests withholds that point. Every question was graded before the
previous scorecard was opened.

## Provenance

- Re-benchmark: opencode-go silently updated the model behind
  `opencode-go/deepseek-v4.1-flash`, which invalidated the 2026-09-28 baseline
  (`model_version: "4.1"`). This run is versioned `4.1-2026-10-21`.
- **One fresh headless `ocode run -m opencode-go/deepseek-v4.1-flash -effort med`
  session per question** (36 sessions) under ocode's real agent system prompt. The
  previous baseline sent the whole answer sheet in ONE session.
- Fake HOME: no global CLAUDE.md/AGENTS.md, no memory, no persisted roots. Every
  builtin tool denied; the five existing `*-tuning-deepseek-v4.1-flash` Kaizen skills
  (and `conduct-tuning-deepseek-v4-flash`) disabled, so this is a true no-skill
  baseline. A probe before the sweep confirmed no tools advertised and no
  Model-Specific Directives block. The generic `[ocode:provider]` DeepSeek
  output-discipline block and the bundled skill-catalogue digest were present, as for
  every user.
- cwd an empty dir named `work`, stdin `/dev/null`. Every session's sqlite audited
  after the sweep: 1 user + 1 assistant message, zero tool calls.

## Per-question results

| id | tags | weight | full | awarded | normalized | notes |
|----|------|-------:|-----:|--------:|-----------:|-------|
| pdf-model-01 | pdf-model | 3 | 2 | 2 | 1.00 | |
| pdf-model-02 | pdf-model, verify-safety | 3 | 2 | 2 | 1.00 | |
| pdf-model-03 | pdf-model | 2 | 2 | 2 | 1.00 | |
| pdf-model-04 | pdf-model | 2 | 2 | 2 | 1.00 | rotation/CropBox detail correct; later answers (locate-03, cell-01) contradict it on whether extracted coords are rotated, not scored here |
| pdf-locate-01 | table-locate | 3 | 2 | 2 | 1.00 | |
| pdf-locate-02 | table-locate | 2 | 2 | 2 | 1.00 | |
| pdf-locate-03 | table-locate, cell-edit | 2 | 2 | 2 | 1.00 | manual bit-shift colour conversion accepted for "convert color int" |
| pdf-locate-04 | table-locate, table-relayout | 2 | 2 | 2 | 1.00 | |
| pdf-rowdel-01 | row-delete | 3 | 3 | 2 | 0.67 | point 1 withheld: the row is never redacted/applied — the page is rebuilt from two `show_pdf_page(clip=...)` bands of an unredacted copy that skip the row. Supporting evidence, probe on 1.27.1: each band XObject embeds the WHOLE source page clipped by `/BBox`, so the deleted row's text survives in the saved file (absent from `get_text`, present in the stream). Shift/redraw and Total recompute + content below are right. Also claims redaction defaults "delete every drawing touching the rect" (wrong; see rowdel-02) |
| pdf-rowdel-02 | row-delete, cell-edit | 3 | 2 | 0.5 | 0.25 | **factual error**: says the default is `graphics=PDF_REDACT_LINE_ART_NONE` ("vector line art is completely untouched"); the real default is REMOVE_IF_COVERED (1). Image-pixel default right. Point 2 withheld: it never says to pass `graphics=0, images=0` to keep the grid — it recommends REMOVE_IF_COVERED plus a padded rect. **Judgement call**: partial 0.5 (aware of the image side effect, wrong on graphics). The previous grader awarded point 2 for the same error |
| pdf-rowdel-03 | row-delete | 2 | 2 | 1 | 0.50 | `show_pdf_page(clip=...)` from a pre-redaction snapshot is right. Point 2 withheld: the worked example redacts only the band BELOW the deleted row, so the deleted row is never removed and the stamp lands on top of it; `REMOVE_IF_TOUCHED` on a full-width band also deletes vertical rules that run up into the rows above |
| pdf-rowdel-04 | row-delete, verify-safety | 2 | 2 | 2 | 1.00 | |
| pdf-cell-01 | cell-edit | 3 | 3 | 3 | 1.00 | tight rect + `graphics=NONE, images=NONE` + right-align to old x1 + span style. Says omitting `graphics=` "deletes any vector line crossing the rect" (wrong: only fully covered) but acts correctly. Font-fallback code is broken (`if False else ...`) |
| pdf-cell-02 | cell-edit | 2 | 2 | 2 | 1.00 | no explicit re-check of neighbours afterwards; awarded on the sizing reasoning |
| pdf-cell-03 | cell-edit, table-relayout | 2 | 2 | 2 | 1.00 | now correct: nothing written, negative return, no exception |
| pdf-cell-04 | cell-edit | 2 | 2 | 2 | 1.00 | descender sign slip in the bbox-baseline formula |
| pdf-relayout-01 | table-relayout | 3 | 3 | 3 | 1.00 | Total + rules + content below are translated (moved, not overlaid) and the vertical rules extended; page overflow → next page with repeated header. No explicit redact step; awarded because the content is moved, not drawn over |
| pdf-relayout-02 | table-relayout | 3 | 3 | 2 | 0.67 | assumes regeneration "from a layout model"; never removes the old table before redrawing at the new x positions (point 3) |
| pdf-relayout-03 | table-relayout, pdf-model | 2 | 2 | 2 | 1.00 | prefer the true source; targeted in-place edit over a round trip |
| pdf-relayout-04 | table-relayout, table-insert | 2 | 2 | 2 | 1.00 | |
| pdf-tblins-01 | table-insert | 3 | 2 | 2 | 1.00 | reportlab overlay + pypdf `merge_page` accepted as the stamp approach |
| pdf-tblins-02 | table-insert | 2 | 2 | 1 | 0.50 | style borrowing good; never orders the table's own fills before its text and rules (its only ordering remark is overlay-after-page-content) |
| pdf-tblins-03 | table-insert, table-relayout | 2 | 2 | 2 | 1.00 | |
| pdf-imgrep-01 | image-replace | 3 | 3 | 3 | 1.00 | single-occurrence fix by re-pointing page 1's resource to a new xref (equivalent) |
| pdf-imgrep-02 | image-replace, verify-safety | 2 | 2 | 2 | 1.00 | |
| pdf-imgrep-03 | image-replace | 2 | 2 | 2 | 1.00 | repeats "default graphics is usually LINE_ART_NONE" but explicitly sets removal, so the action is right |
| pdf-imgins-01 | image-insert | 3 | 2 | 1 | 0.50 | anchors to `page.rect.x1 - 36` (hard-coded margin), not the real content edges; aspect ratio, top-left coords and the collision check are right |
| pdf-imgins-02 | image-insert | 2 | 2 | 2 | 1.00 | xref reuse; dedup after the fact via `mutool clean -gggg` / Ghostscript accepted as equivalent of a `garbage=4` save |
| pdf-imgins-03 | image-insert, pdf-model | 2 | 2 | 2 | 1.00 | |
| pdf-fonts-01 | fonts | 3 | 2 | 2 | 1.00 | |
| pdf-fonts-02 | fonts | 2 | 2 | 2 | 1.00 | |
| pdf-fonts-03 | fonts, cell-edit | 2 | 2 | 2 | 1.00 | bold = flags & 16 right; says flag 1 = serif (it is superscript; serif is 4) |
| pdf-verify-01 | verify-safety | 3 | 3 | 3 | 1.00 | |
| pdf-verify-02 | verify-safety | 3 | 2 | 2 | 1.00 | |
| pdf-verify-03 | verify-safety | 2 | 2 | 2 | 1.00 | |
| pdf-verify-04 | verify-safety, pdf-model | 2 | 2 | 1 | 0.50 | strong on verifying the saved output; never names the edit-call silent failures (negative `insert_textbox` return, empty `search_for`, off-page text, collateral redaction) |

`normalized = min(awarded, full) / full`

## Per-tag subscores

| tag | subscore | n | trust | action |
|-----|---------:|--:|-------|--------|
| table-locate | 1.00 | 4 | ok | omit |
| image-replace | 1.00 | 3 | low-n | omit |
| fonts | 1.00 | 3 | low-n | omit |
| verify-safety | 0.94 | 7 | ok | omit |
| pdf-model | 0.94 | 7 | ok | omit |
| table-relayout | 0.94 | 7 | ok | omit |
| table-insert | 0.89 | 4 | ok | **derive** |
| cell-edit | 0.86 | 7 | ok | **derive** |
| image-insert | 0.79 | 3 | low-n | **derive** (low-n) |
| row-delete | 0.58 | 4 | ok | **derive** |

`subscore = Σ(normalized×weight) / Σ(weight)` over that tag's questions.

## Stack score

```
stack_score = Σ(normalized×weight) / Σ(weight) = 77.25 / 86 = 89.8%
```

## Change vs previous baseline

Previous: `model_version "4.1"`, 2026-09-28, whole sheet in one session, **87.2%**.
Now: one session per question, **89.8%** (+2.6 pts).

| tag | new | old | delta | notes |
|-----|----:|----:|------:|-------|
| table-locate | 1.00 | 1.00 (old) | = | |
| image-replace | 1.00 | 1.00 (old, low-n) | = | |
| fonts | 1.00 | 1.00 (old, low-n) | = | |
| verify-safety | 0.94 | 0.94 (old) | = | verify-01 now names "render alone can't catch overlays"; verify-04 now misses the edit-call silent failures |
| pdf-model | 0.94 | 0.94 (old) | = | relayout-03 now earns "prefer the source"; verify-04 drop |
| table-relayout | 0.94 | 0.81 (old) | +0.13 | cell-03 and relayout-03 now correct; relayout-02 still never removes the old table |
| table-insert | 0.89 | 0.89 (old) | = | tblins-02 still has no fill→text→rules order |
| cell-edit | 0.86 | 0.84 (old) | +0.02 | cell-03 fixed; rowdel-02 graded stricter (see below) |
| image-insert | 0.79 | 0.64 (old, low-n) | +0.15 | imgins-02 now covers a dedup pass; imgins-01 still uses a fixed page margin |
| row-delete | 0.58 | 0.65 (old) | −0.07 | see below |

The row-delete drop is mostly **grading, not model regression**. The rowdel-02 error is
the same wrong default (`LINE_ART_NONE`) the old answer made; this grader awards only
the partial (0.5) where the old one awarded point 2. rowdel-01 changed technique
(page rebuilt from clipped bands instead of stamp-without-redact) but still fails true
removal, now verified by a byte-level probe. rowdel-03 still leaves part of the old
content on the page (this time the deleted row itself).

Real gains: cell-03 (`insert_textbox` writes nothing), verify-01 (render can't tell an
overlay), imgins-02 (dedup), relayout-03 (regenerate from source first).

The previous scorecard's live behavioural probe tables (2026-09-28) measured the
earlier `4.1` model and are not carried forward; they remain in git history.

## Derivation targets

Tags below threshold (`< 0.9`): **row-delete (0.58), image-insert (0.79, low-n),
cell-edit (0.86), table-insert (0.89)** → feed into
`derived/pdf.deepseek-v4.1-flash.SKILL.md`. table-relayout (0.94) is now above
threshold, and its section and the `insert_textbox` overflow rule are dropped.

### row-delete (0.58)

- **Wrong `apply_redactions` default** (rowdel-02, repeated in imgrep-03; rowdel-01
  and cell-01 say "touching/crossing" instead). The defaults are `images=2` (blank
  overlapping pixels), `graphics=1` (REMOVE_IF_COVERED: delete line art lying
  entirely inside the rect), `text=0`. Keep the grid with `graphics=0, images=0`.
- **Clipping is not removal** (rowdel-01). `show_pdf_page(clip=...)` embeds the whole
  source page and only hides the outside via `/BBox`; a page rebuilt from clipped bands
  still carries the deleted row in its bytes.
- **Redaction scope** (rowdel-03). Redact the deleted row AND the region that moves,
  then stamp; and don't use `REMOVE_IF_TOUCHED` on a band whose vertical rules run
  into rows that stay.

### image-insert (0.79, low-n)

- Placement anchored to `page.rect` minus a fixed 36pt margin (imgins-01) instead
  of the content's right edge and the footer's top.

### cell-edit (0.86)

- The rowdel-02 default error (shared with row-delete): a cell redaction with
  defaults deletes shading lying inside the rect.

### table-insert (0.89)

- No paint order inside the new table (tblins-02): fills, then text, then rules.

## Odd things in the answers (not scored)

- Recap blocks (`ASKED / BUILT / FOUND / DECIDED / NEXT`) appear in relayout-01,
  tblins-01 and tblins-02 — apparently leaking from the output-discipline block.
- Sessions contradict each other on the redaction default (`LINE_ART_NONE` in
  rowdel-02/imgrep-03 vs "deletes touching/crossing lines" in rowdel-01/cell-01) —
  neither is the real REMOVE_IF_COVERED behaviour.
- Broken font-fallback code in cell-01; flag 1 called "serif" in fonts-03; descender
  sign slip in cell-04.
- **Corpus note (key, not this model):** the pdf-rowdel-03 reference technique stamps
  from an untouched copy of the original with `show_pdf_page(clip=...)`. Per the
  1.27.1 probe, the stamped XObject then carries the whole original page, deleted row
  included, hidden only by `/BBox`. Redacting the deleted row in the stamp source too
  removes it. `questions.yaml` was left unchanged; rowdel-03 point 1 was graded per the key.
