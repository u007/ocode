# xlsx live behavioural probe (office-clerk tasks)

A closed-book sheet only measures what a model *states*. This probe gives the model a real workbook and a
request phrased the way an office manager would phrase it, runs `ocode run -yolo`, then checks the saved
file. `check.py` works from the saved XML plus a **Microsoft Excel recalculation** (`lib.recalc`: AppleScript on
macOS, COM/PowerShell on Windows; Excel must be installed, LibreOffice backend is a TODO). For every "live formula" task it also **edits an input in a copy of the model's file,
recalculates again and checks the answer moved**, so pasted numbers, hard-coded dates and `TODAY()` fail.

```
pip install openpyxl lxml            # + Microsoft Excel installed (macOS or Windows)
python3 test_recalc.py               # recalc plumbing, NO Excel needed (fake backend); runs on Linux/CI
python3 make_fixtures.py             # fixtures/*.xlsx (generated, gitignored)
./selftest.sh                        # 13 "good" references must PASS, 15 cover-up/naive variants must FAIL
OCODE_BIN=/path/to/ocode ./sweep.sh <label> <provider/model> [reps=2] [parallel=2]
python3 summarize.py runs/<label>    # per task, per tag (the Kaizen subscore axes), overall
./probe.sh <provider/model> <task> <label> [rep]     # one task, e.g. to reproduce a failure
```

Copy this folder to a scratch directory before a sweep if you do not want `runs/` next to the sources. Each
run gets a fresh git repo holding only the fixture; the checker and fixtures stay outside it. Per run you get
`runs/<label>/<model>/<task>.<rep>{,.out,.err,.meta,.check}` and the model's workbook in that directory.

## The test cases

Each is a request a clerk or admin assistant really makes. "Trap" is what separates a capable model from one
that merely produces a plausible-looking file.

| task | the request | traps (what a weak model gets wrong) |
|---|---|---|
| `attendance-status` | Add Name / Hours Worked / Status columns to the daily attendance sheet | blank check-in compares as 00:00 so absent people read "OK"; a punch at exactly 09:00 is not late; Late + no check-out must be Late; shift start must come from `Settings!B1`; attendance order ≠ roster order; no XLOOKUP |
| `attendance-summary` | Department summary: headcount, present, late, absent, attendance % + total | "present" includes Late and No check-out; percent stored as a fraction with `%` format; COUNTIFS reading the sheet, not typed counts |
| `expense-summary` | Category summary vs budget: spent, budget, remaining, % used, status, total | a category with no spend (Utilities) must show 0 not blank; SUMIF vs SUMIFS argument order; budget pulled by lookup; "over" is strictly greater |
| `expense-pie` | Pie chart of spend share by category | Total row swept into the range (a 50% slice); labels/values off by a row; no percentage labels; chart on top of the table |
| `budget-bar` | Clustered column chart, Spent vs Budget | stacked instead of clustered; header row counted as data; series names; axis titles; legend; **axes absent** (openpyxl 3.1 writes no `c:delete val=0`) |
| `sales-pivot` | Region × Month pivot with totals | openpyxl cannot create a PivotTable: a live SUMIFS grid is the honest answer; mixed refs `$A2` / `B$1`; totals not double-counted; static pandas values fail the liveness edit |
| `sales-top3` | Top 3 reps by total sales | totals must be live (`SUMIF`); ordering; a 4th row left behind |
| `messy-clean` | Clean a messy export into a new sheet | two text-date styles; `str.title()` turns HR into "Hr"; "Daniel  Wong" double space; text amounts; four duplicates only visible after normalising; blank rows; hiding duplicates ≠ removing them; Raw must stay untouched |
| `audit-variance` | Daily stock-count audit: variance, %, value impact, flag, summary, red highlight | System Qty = 0 (div by zero → "n/a", but counted > 0 still flagged); +2.0% exactly is *not* flagged; conditional format that updates, not a static fill; five labelled summary figures |
| `ledger-format` | Make the ledger presentable for printing | dates display as 46321; bold+fill header, freeze, filter, 2-decimals, widths, landscape, fit one page wide (needs `fitToPage`), repeat header row; **no value may change** |
| `orders-lookup` | Customer name, due date, days overdue, status from a customer sheet | unknown customer → "UNKNOWN" with no `#N/A` flowing downstream; due exactly on the as-of date is *not* overdue; `TODAY()` banned (as-of date lives in Settings); no XLOOKUP |
| `expense-daily` | Date-driven daily expenses report (KPIs + category table) | a real date in B1; `SUMIFS` by date; largest expense without `MAXIFS` (bare `MAXIFS` gives `#NAME?` via openpyxl, needs `_xlfn.`); zero-spend categories; changing B1 must update everything |
| `log-append` | Add today's entry to an existing log without breaking it | `insert_rows` rewrites nothing: Total range, chart series, defined name `LogTotal`, dropdown, highlight and merged title must all be repaired/kept |

### What is checked on every run
- The original input is byte-identical; the output workbook opens and recalculates.
- No `#REF!` / `#DIV/0!` / `#NAME?` / `#VALUE!` / `#N/A` anywhere in the recalculated file.
- Task-specific values (every row, not a sample), formats, chart type / series ranges / title / labels / axes /
  placement, conditional formats, validations, defined names, merged ranges.
- Result sheets have no hidden rows or columns.
- **Liveness edit** (most formula tasks): change an input in a copy, recalculate, require the new correct
  answer. Edits used: shift start 09:00 → 09:10; one ledger amount +100 and one budget 450 → 500; one Data
  amount +1000 / +500; one counted qty; as-of date and one customer's terms; report date → 2026-10-27.

## Scoring
`check.py` prints `PASS`/`FAIL` per check and `SCORE n/m PASS|FAIL`. A run **passes** only when every check
passes; the *fraction* is kept as partial credit. `summarize.py` rolls tasks up into the tag vocabulary of
`../meta.yaml` (`formulas`, `lookup`, `dates-times`, `charts`, `summary`, `cleanup`, `formatting`,
`preserve`) so a live subscore can be set against the closed-book subscore of the same tag. Treat a tag with
fewer than 3 runs as low-n. Run each task at least twice: a small model's first attempt is noisy.

## Known limits (read before trusting a number)
- **Structural, not visual.** Charts are judged from `chart*.xml` and the drawing anchor, not rendered.
  Export a sample to PDF from Excel for an eye check.
- **Excel-only recalculation, serialised.** Excel is one instance, so `lib.recalc` holds a file lock (with a
  900 s deadline) and runs one recalculation at a time; every platform stages a COPY of the input and the
  output in `~/xlsx-probe-excel`, so Excel never opens the artifact being graded. Sandboxed Excel asks for
  access per folder — one folder means one "Grant Access" click, though restarting Excel can ask again.
  Staging names are unique per call and removed afterwards. The prompts still forbid
  `XLOOKUP`/dynamic arrays ("some of us run older Excel"). Functions written by a library need the `_xlfn.`
  prefix (`IFNA`, `MAXIFS`, `CONCAT`, `TEXTJOIN`, `IFS`) or Excel returns `#NAME?`. The Windows script
  (`excel_recalc.ps1`) has never been run.
- **A broken environment is not a model failure.** When Excel is missing, refuses access or times out,
  `check.py` prints a final `ENV-FAIL <reason>` line and exits 3; `summarize.py` excludes those runs from
  every denominator and reports them separately.
- **Real PivotTables cannot be authored by openpyxl.** `sales-pivot` accepts a live formula grid or a real
  pivot part; for a real pivot the liveness edit is skipped (its cache is static until refreshed).
- **Legacy `.xls` / macro `.xlsm` handling** is closed-book only (`xlsx-verify-03`): no LibreOffice-free way
  to build those fixtures portably.
- The fixtures are small and deterministic (`lib.py`); they will not reveal performance problems on
  50,000-row files.

## Self-test = checker integrity
`selftest.sh` runs `ref.py good <task>` (a correct openpyxl solution) and the cover-ups the checker must
reject: `values` (formulas frozen to pasted numbers), `badrange` (Total row in the pie / stacked bar / axes
left deleted), `naive` (`insert_rows` and nothing else), `lazy` (strip + `title()` only), `hide` (duplicates
hidden), `partial` (bold header only), `today` (`TODAY()` as the as-of date). Run it after touching
`check.py`, `lib.py` or `make_fixtures.py`; the three checker bugs found while writing it were all of the
"correct answer rejected" kind, which is the failure that would otherwise be blamed on the model.
