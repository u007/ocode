---
model_id: deepseek-v4.1-flash
model_version: "unknown"
evaluated_via: opencode-go
evaluated_on: 2026-10-31
stack: xlsx
stack_corpus_rev: 1
threshold: 0.9
sample: full   # all 38 questions
---

# Scorecard — deepseek-v4.1-flash on xlsx

> Valid ONLY for `deepseek-v4.1-flash` at the model version served by `opencode-go` on
> 2026-10-31. The exact `model_version` is not known for this run (recorded as `unknown`);
> any provider-side model update invalidates this scorecard, so re-benchmark.

Answers: `../answers/deepseek-v4.1-flash.md`, produced closed-book (one fresh headless
`ocode run` session per question, every builtin tool denied, no Kaizen skills, audited to
zero tool calls).
Graded against `questions.yaml` (corpus_rev 1; facts verified on openpyxl 3.1.5, pandas 3.0.5,
LibreOffice 24.2.7). Per the corpus header, an equivalent correct technique in another
library earns the point. A materially wrong claim about the construct a point tests
withholds that point; wrong side claims that do not touch the tested construct are noted
but not penalised.

## Per-question results

| id | tags | weight | full | awarded | normalized | notes |
|----|------|-------:|-----:|--------:|-----------:|-------|
| xlsx-model-01 | xlsx-model, formulas | 3 | 2 | 2 | 1.00 | None from data_only; saving a data_only workbook flattens formulas; also notes a normal load+save drops cached values |
| xlsx-model-02 | xlsx-model, verify-safety | 3 | 2.5 | 2 | 0.80 | LibreOffice headless recalc then data_only read, and stale-numbers argument are right. Point 2 withheld: never scans the recalculated cells for `#REF!`/`#DIV/0!`/`#NAME?`/`#VALUE!`/`#N/A` |
| xlsx-model-03 | xlsx-model | 2 | 2.5 | 2.5 | 1.00 | only openpyxl edits; to_excel has no formula API; no library creates a PivotTable |
| xlsx-model-04 | xlsx-model, formulas, lookup | 3 | 2.5 | 2.5 | 1.00 | `_xlfn.` prefix, English names + commas, IFERROR/SUMPRODUCT for older Excel. Side claim that LibreOffice is "lenient" with bare names is contradicted by the key's verification (not scored) |
| xlsx-formulas-01 | formulas, summary | 3 | 2 | 2 | 1.00 | |
| xlsx-formulas-02 | formulas | 2 | 2 | 2 | 1.00 | |
| xlsx-formulas-03 | formulas | 3 | 2 | 2 | 1.00 | |
| xlsx-formulas-04 | formulas, dates-times | 3 | 2.5 | 1.5 | 0.60 | blank-compares-as-0 and Settings cell (absolute) right. Point 2 withheld: asserts precedence Absent > No check-out > Late and puts No check-out ahead of Late in its formula (a late person with no check-out must read Late). It mentions Absent > Late > No check-out only as an alternative "payroll policy" |
| xlsx-formulas-05 | formulas, verify-safety | 2 | 2 | 2 | 1.00 | date in an input cell; edit-input/edit-source test. Never names TODAY() as the wrong design (point 1 awarded on the input-cell concept; see ambiguities) |
| xlsx-formulas-06 | formulas, preserve | 2 | 2 | 2 | 1.00 | |
| xlsx-lookup-01 | lookup | 3 | 2 | 2 | 1.00 | |
| xlsx-lookup-02 | lookup, formulas | 2 | 2 | 2 | 1.00 | uses XLOOKUP (365 assumption) and defaults missing terms to 30 but flags the policy choice; does not mention `_xlfn.` for IFNA |
| xlsx-lookup-03 | lookup, dates-times | 2 | 2 | 2 | 1.00 | side claim that `=A2=B2` coerces types and can be TRUE while VLOOKUP fails is wrong (text vs number compares FALSE); diagnosis via ISNUMBER/LEN/EXACT still correct |
| xlsx-dates-01 | dates-times, xlsx-model | 3 | 2 | 2 | 1.00 | serial 46323 verified; side claims wrong: text rows satisfying `>=` numeric criteria, DATEVALUE("2026-10-28") failing in US locale |
| xlsx-dates-02 | dates-times, formulas | 2 | 2 | 2 | 1.00 | |
| xlsx-dates-03 | dates-times, formulas | 2 | 2 | 2 | 1.00 | argues 0.375 and TIME(9,0,0) are identical exact values (true) and locates instability in the punch side; one cutoff cell + whole-minute rounding right; money ROUND clause not discussed |
| xlsx-dates-04 | dates-times, formulas | 2 | 2 | 2 | 1.00 | |
| xlsx-charts-01 | charts | 2 | 2 | 2 | 1.00 | |
| xlsx-charts-02 | charts, summary | 3 | 1.5 | 1.5 | 1.00 | |
| xlsx-charts-03 | charts | 3 | 2.5 | 2 | 0.80 | Reference/titles_from_data/set_categories/anchor right; pitfall (header as data) valid. Point 2 withheld: no mention that axes may not render without `x_axis.delete = False` / `y_axis.delete = False`. Side claims wrong: BarChart default is `col` (not horizontal) and unset `overlap` does not stack clustered series |
| xlsx-summary-01 | summary | 3 | 2 | 2 | 1.00 | |
| xlsx-summary-02 | summary, formulas | 2 | 1.5 | 1.5 | 1.00 | row/column totals computed from Data via SUMIFS rather than SUM of the grid body; offers `SUM(B2:M13)` as the grand-total cross-check |
| xlsx-summary-03 | summary, formulas | 2 | 2 | 2 | 1.00 | |
| xlsx-cleanup-01 | cleanup, dates-times | 3 | 2 | 2 | 1.00 | does not explicitly say to write date objects with a date number_format |
| xlsx-cleanup-02 | cleanup | 3 | 2.5 | 2.5 | 1.00 | argues against bare "keep first" in favour of an explicit survivorship rule (first row as final tie-break); accepted |
| xlsx-cleanup-03 | cleanup | 2 | 2 | 2 | 1.00 | |
| xlsx-cleanup-04 | cleanup, xlsx-model | 2 | 1.5 | 1.5 | 1.00 | |
| xlsx-formatting-01 | formatting | 2 | 2 | 2 | 1.00 | |
| xlsx-formatting-02 | formatting, formulas | 2 | 2 | 2 | 1.00 | |
| xlsx-preserve-01 | preserve, verify-safety | 3 | 2.5 | 2.5 | 1.00 | concrete losses, copy + part diff, XML edit fallback. Does not mention Pillow; wrongly says openpyxl loses pivot tables (the same run says in model-03 that it preserves them). Awarded, see ambiguities |
| xlsx-preserve-02 | preserve, formulas, charts | 3 | 2.5 | 2.5 | 1.00 | |
| xlsx-verify-01 | verify-safety | 3 | 2.5 | 2.5 | 1.00 | |
| xlsx-verify-02 | verify-safety, charts | 2 | 1.5 | 1.5 | 1.00 | |
| xlsx-verify-03 | verify-safety, xlsx-model | 2 | 2.5 | 2.5 | 1.00 | csv point awarded on "no formulas/formatting"; does not say to write the result to a new .xlsx (see ambiguities) |
| xlsx-formatting-03 | formatting | 1 | 2 | 2 | 1.00 | |
| xlsx-formatting-04 | formatting, preserve | 2 | 2 | 2 | 1.00 | |
| xlsx-preserve-03 | preserve, cleanup | 2 | 2 | 2 | 1.00 | |
| xlsx-preserve-04 | preserve, formulas | 3 | 2 | 2 | 1.00 | |

`normalized = min(awarded, full) / full`

## Per-tag subscores

| tag | subscore | n | trust | action |
|-----|---------:|--:|-------|--------|
| lookup | 1.00 | 4 | ok | omit |
| summary | 1.00 | 5 | ok | omit |
| preserve | 1.00 | 6 | ok | omit |
| cleanup | 1.00 | 5 | ok | omit |
| formatting | 1.00 | 4 | ok | omit |
| formulas | 0.97 | 17 | ok | omit |
| xlsx-model | 0.97 | 7 | ok | omit |
| verify-safety | 0.96 | 6 | ok | omit |
| charts | 0.95 | 5 | ok | omit |
| dates-times | 0.93 | 7 | ok | omit |

`subscore = Σ(normalized×weight) / Σ(weight)` over that tag's questions.

## Stack score

```
stack_score = Σ(normalized×weight) / Σ(weight) = 89.6 / 92 = 97.4%
```

## Derivation targets

Tags below threshold (`< 0.9`): **none**. No derived SKILL.md is warranted from this run.

Lowest tags, for the record (all above threshold): dates-times 0.93, charts 0.95,
verify-safety 0.96. The three points withheld were formulas-04 (precedence of Late vs
No check-out), model-02 (no error-value scan after recalculation) and charts-03 (openpyxl
axis `delete=False`).

## Odd things in the answers (not scored)

- Wrong side claims: LibreOffice "lenient" with bare `_xlfn` functions (model-04);
  `=A2=B2` coerces text and number (lookup-03); text satisfies numeric `>=` criteria and
  ISO DATEVALUE fails in US locale (dates-01); BarChart default is horizontal and unset
  `overlap` stacks series (charts-03); openpyxl loses pivot tables (preserve-01, contradicted
  by model-03 in the same run).
- Sessions contradict each other on whether openpyxl preserves pivot tables.
- A stray non-ASCII fragment appears in model-03 ("真正 cell-level").
- Several answers are far longer than the question needs (lookup-03, summary-01, cleanup-03).
