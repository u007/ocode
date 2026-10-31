# deepseek-v4.1-flash — xlsx answers

Model: `opencode-go/deepseek-v4.1-flash` (provider-stripped `deepseek-v4.1-flash`), run 2026-10-31.

Closed-book barrier:
- Each of the 38 questions in `docs/okf/_prompts/xlsx.md` was a fresh headless
  `ocode run -m opencode-go/deepseek-v4.1-flash -effort med` session (`bin/ocode` 0.9.27) under
  ocode's real agent system prompt.
- Fake HOME: no global CLAUDE.md/AGENTS.md, no memory. Its only config denies every builtin tool and
  DISABLES the seven existing `*-tuning-deepseek-*` Kaizen skills, so this is a no-skill baseline.
  The generic bundled skill-catalogue digest and the `[ocode:provider]` DeepSeek block were still present.
- cwd: an empty dir named `work`; stdin `/dev/null`; 3 sessions in parallel.
- Audit: every session sqlite checked after the sweep — 1 user + 1 assistant message, zero tool calls (39/39).
- Prompt per question: closed-book instruction + the one question; plain-text reply, wrapped into YAML
  below verbatim. No question was re-asked; no answer was edited.

```yaml
- id: xlsx-model-01
  answer: |
    Short answer: `None`.

    Why
    - openpyxl is a file-format library, not a calculation engine. It never evaluates formulas.
    - When the first script does `ws["B7"] = "=SUM(B1:B5)"` and saves, openpyxl writes the formula into the cell's `<f>` element. The XLSX format has room for two things per formula cell: the formula text (`<f>`) and the last value some application computed (`<v>`). openpyxl writes only the former; it has no cached result to write.
    - `data_only=True` means "ignore `<f>`, give me the cached `<v>`". There is no `<v>`, so B7 comes back as `None`.

    Caveats
    - If Excel (or LibreOffice with recalculation) opened and saved the file between the two scripts, a cached value exists and `data_only=True` returns it — e.g. `15`. That value can be stale if inputs changed without a recalculation.
    - The same `None` appears for any programmatically generated workbook that was never opened by a calculating app.
    - Setting `wb.calculation.fullCalcOnLoad = True` before saving makes Excel recalculate on open, but does nothing for an openpyxl reader.

    If the second script saves
    - It loaded with `data_only=True`, so every formula cell in the workbook is now held as its cached value (or `None`). openpyxl does not keep the formula text in that mode.
    - Saving writes those literals back: B7 becomes empty (or a static number if a cache existed), and every other formula in the workbook is destroyed the same way. This is a silent, permanent, workbook-wide loss of formulas. Treat `data_only=True` workbooks as read-only.
    - Related gotcha: even a normal load (`data_only=False`) followed by a save drops cached values, because openpyxl doesn't preserve `<v>` for formula cells. After any openpyxl round-trip, `data_only` reads return `None` until Excel or LibreOffice recalculates and saves.

    To get the actual value
    - Compute it in Python yourself, or
    - Force a recalculation with an app that calculates (open/save in Excel, or `libreoffice --headless --convert-to xlsx`), then read with `data_only=True`, or
    - Use a formula-evaluating library/tool (pycel, formulas, LibreOffice UNO) rather than openpyxl.

- id: xlsx-model-02
  answer: |
    Assumptions: the deliverable is a `.xlsx` workbook, and "prove" means showing the stored formulas evaluate to the same numbers a spreadsheet would produce. No Excel, no LibreOffice is the worst case; I cover both.

    PRODUCING THE FILE WITH REAL FORMULAS

    Use a writer library that stores formula text, not results. With openpyxl:

        from openpyxl import Workbook
        wb = Workbook(); ws = wb.active
        ws.append(["Item", "Q1", "Q2", "Q3", "Total"])
        for r in range(2, n + 2):
            ws.cell(r, 5).value = f"=SUM(B{r}:D{r})"
        ws.cell(n + 2, 5).value = f"=SUM(E2:E{n+1})"
        wb.calculation.fullCalcOnLoad = True
        wb.save("out.xlsx")

    Key facts:

    - openpyxl writes `<f>SUM(B2:D2)</f>` and no `<v>` cached value. It cannot calculate.
    - `fullCalcOnLoad = True` makes Excel and LibreOffice recalculate on open. It is a hint, not proof.
    - Anything that reads cached values (pandas `read_excel`, openpyxl `data_only=True`) will see `None` until a real spreadsheet saves the file. This is the trap.
    - xlsxwriter can write formula plus cached value: `worksheet.write_formula(row, col, "=SUM(B2:D2)", fmt, value)`. That gives both. The cached value must still be verified; a wrong cache is worse than none.
    - Formulas are stored in en-US syntax with commas regardless of the user's locale.

    PROVING THE FORMULAS CALCULATE

    Layer the checks. Each layer catches a different failure.

    1. Structural check (formula text). Reopen with `data_only=False`. Assert every Total cell is a string starting with `=` and references the exact intended range. Catch off-by-one rows, hardcoded constants, and missing fill-down.

    2. Independent recalculation. Pick whichever engine exists:
       - LibreOffice headless with a UNO/Basic macro: open, `calculateAll()`, store. Then read back with `data_only=True`.
       - `soffice --headless --convert-to csv` per sheet also forces evaluation and gives a diffable file.
       - Pure Python engine: `formulas` (or `pycel`) parses the formula text itself and computes it. `formulas.ExcelModel().loads(path).finish().calculate()` returns the evaluated cells. This needs no office suite and validates the formula, not a cache.

    3. Oracle comparison. Compute expected totals in Python separately and diff them against the recalculated values. Here Python is legitimate: it is the test oracle, not the cell content. Report exact mismatches, not "looks right".

    4. Liveness test. Copy the file, change one input cell, recalculate, confirm the Total changed by the expected delta. A static number fails this instantly. This is the check that actually proves "formula".

    5. Raw XML inspection. Unzip and confirm `xl/worksheets/sheet1.xml` contains `<f>` elements in column E and that any `<v>` matches the recomputed value.

    6. Cross-engine agreement. LibreOffice and `formulas` should agree with the Python oracle. Two independent engines agreeing is strong evidence; one engine alone can share the same bug.

    If no spreadsheet and no formula engine is available, say so plainly. Ship the formula file, state that cached values are absent, and mark "recalculated values verified" as unproven rather than claiming it.

    WHY PYTHON-COMPUTED NUMBERS ARE NOT ACCEPTABLE

    - It violates the requirement. The task says formulas. A checker inspecting cell values finds numbers, not `=SUM(...)`, and fails. The deliverable is the mechanism, not the arithmetic.
    - It is not live. Edit an input and the Total stays stale. The whole point of a Total column is recalculation and dependency tracking.
    - It destroys auditability. A reviewer cannot see how a number was derived. A formula is self-documenting; a constant is an assertion.
    - It hides range bugs. A Python `sum(row[1:4])` can be correct while the formula's range is wrong. The formula is the thing under review.
    - It breaks maintenance. Adding rows or columns means re-running a script. A spreadsheet user cannot extend it.
    - Semantics differ. Excel's SUM ignores text and booleans, applies 15-significant-digit rounding, and propagates `#REF!`/`#DIV/0!`. A Python float sum can silently disagree with what the sheet would display.
    - It is answer-writing instead of problem-solving. Writing the expected result to pass the check is the definition of gaming the test.

    The distinction that matters: Python as an oracle to verify formulas is correct and encouraged. Python as the source of the cell values is the shortcut being rejected.

- id: xlsx-model-03
  answer: |
    Assumption: "clerk's workbook task" = fill in / extend an existing monthly ledger template that already has formatting, formulas and a chart, then add a summary chart and maybe a pivot-style summary.

    Short answer
    - Edit an existing .xlsx: openpyxl only (真正 cell-level). pandas can only append/replace whole sheets through the openpyxl engine. XlsxWriter cannot.
    - Formulas: openpyxl and XlsxWriter both write them. pandas has no formula API; it just passes the string to the engine.
    - Charts: openpyxl and XlsxWriter both create native charts. pandas cannot.
    - Real PivotTable: none of the three can create one. openpyxl can read and preserve an existing one. pandas `pivot_table()` only computes static cells.

    Comparison
    ```
    Capability              openpyxl              XlsxWriter             pandas.to_excel
    ----------------------  --------------------  ---------------------  ----------------------------
    Read existing .xlsx     Yes                   No (write-only)        No (only via engine)
    Edit existing .xlsx     Yes, cell-level       No                     Append/replace sheet only
    Preserve formatting     Mostly                n/a                    Only what engine keeps
    Write formulas          Yes ("=..." string)   Yes (+ cached value)   No API; string passthrough
    Calculate formulas      No                    No                     No
    Create charts           Yes                   Yes (best coverage)    No
    Read/preserve charts    Yes, with losses      n/a                    No
    Create PivotTable       No                    No                     No
    Preserve PivotTable     Yes (read support)    n/a                    No
    ```

    Details that matter for the clerk job

    openpyxl
    - Opens an existing workbook, changes cells, saves. This is the only one of the three that does a real round-trip.
    - Writes formulas as strings beginning with "=". It does not evaluate them and stores no cached result, so a non-Excel viewer may show blank/0 until Excel recalculates.
    - `data_only=True` returns cached values instead of formulas, and returns `None` if the file was never opened/saved by Excel. You cannot get formulas and cached values in the same load.
    - Creates native charts (bar, line, pie, scatter, etc.) and reads/preserves charts it understands. Unsupported chart features can be dropped on save.
    - Reads existing PivotTables so they survive a save; it has no supported API for creating a new PivotTable.
    - Gotchas when editing a template: `insert_rows`/`delete_rows` do not translate formulas, and merged cells, conditional formatting and defined names can behave badly. Prefer writing into existing cells over restructuring.
    - Use `keep_vba=True` for .xlsm.

    XlsxWriter
    - Write-only by design. It cannot open or modify an existing .xlsx, so it is useless for "fill in the template".
    - Excellent formula support: `write_formula(row, col, formula, fmt, value)`. Without a cached `value` it stores 0 and sets a full-recalc flag so Excel fixes it on open.
    - Best chart coverage of the three, plus sparklines, data validation, conditional formatting, autofilter and Excel Tables (`add_table`, a ListObject).
    - No PivotTable support. `add_table` is an Excel Table, not a PivotTable — a common confusion.
    - Also no formula evaluation, and it cannot read back what it wrote.

    pandas.DataFrame.to_excel
    - Writes a new file. With `pd.ExcelWriter(path, engine="openpyxl", mode="a", if_sheet_exists="overlay")` it can overwrite a block of cells or replace a sheet in an existing workbook, but this is not general cell-level editing and it does not manage formatting, charts or pivots.
    - No formula concept. A string starting with "=" is handed to the engine; openpyxl turns it into a live formula, while XlsxWriter's string path may store it as literal text. Do not rely on this — write formulas with the engine directly.
    - No chart support. You can, however, keep the `ExcelWriter` open and add a chart through the underlying engine (`writer.book`, `writer.sheets["Sheet1"]`).
    - `pivot_table()` computes a pivot in Python and writes plain cells. It is not an Excel PivotTable: no field list, no refresh, no drill-down.
    - Default .xlsx engine is openpyxl; `mode="a"` requires openpyxl and does not work with fsspec URLs.

    Creating a real PivotTable
    None of these libraries does it. A genuine PivotTable needs the `pivotCacheDefinition` and `pivotTableDefinition` parts in the package. Practical routes:
    - Automate Excel itself: `xlwings` or `pywin32` COM (`PivotCaches().Create`, `CreatePivotTable`). Requires Windows and an installed Excel.
    - LibreOffice headless via UNO/a Basic macro.
    - Hand-author the pivot XML parts (fragile, effectively unsupported).

    Recommendation for the clerk scenario
    - Editing the existing template: openpyxl, writing values and formulas into known cells, then adding charts with openpyxl.
    - Building a fresh report from data: pandas for the tabular work, then either the openpyxl engine for the file or the XlsxWriter engine if you want richer charts and tables.
    - If the clerk truly needs a refreshable PivotTable, plan on Excel automation (xlwings/COM), not a pure-Python library.

    Uncertainties I would verify before shipping
    - Exactly which chart and PivotTable features openpyxl preserves on a given round-trip; this varies by version and by chart type.
    - Whether a given pandas/XlsxWriter version stores a leading "=" as a formula or as text; test one cell before trusting it.
    - pandas `mode="a"` / `if_sheet_exists` option names and minimum versions differ across releases.

- id: xlsx-model-04
  answer: |
    Short answer: openpyxl does not calculate formulas and does not translate function names. It writes your string verbatim into the file's `<f>` element. Excel then parses the stored string, so the stored name must match exactly what Excel expects.

    IFNA, MAXIFS and CONCAT are all "future functions": they were added to Excel after the original 2007 file format. In an .xlsx they must carry the `_xlfn.` prefix. Written bare, Excel sees an identifier it does not recognise and returns #NAME?. Corrected:

      =_xlfn.IFNA(VLOOKUP(A2,Customers!A:B,2,FALSE),"UNKNOWN")
      =_xlfn.MAXIFS(D:D,B:B,"Travel")
      =_xlfn.CONCAT(A2,B2)

    The prefix is only a marker telling Excel "look this up in the extended function table"; Excel hides it and shows plain IFNA/MAXIFS/CONCAT in the formula bar. If you read the file back with openpyxl you will see the prefixed text — that is correct, do not "clean it up".

    On why you saw two failures rather than three: strictly, all three bare names are invalid in stored form, so the count depends on what performed the recalculation and on whether the cell was really stored as a formula. Likely explanations, and I am flagging this because the premise is a little loose:

      - A LibreOffice recalculation is lenient: it accepts many bare names it knows, so you can get fewer #NAME? than Excel would give.
      - An older Excel (e.g. 2016) does not have MAXIFS at all, so even `_xlfn.MAXIFS` stays #NAME?. The prefix signals "newer function", it does not add support.
      - A cell formatted as Text, or a value written as a string, never evaluates and shows the raw text.

    So the rule is: every post-2007 function needs the prefix, and the target Excel must actually support it.

    If the workbook must open on Excel 2016/2019 or in LibreOffice/Sheets, prefer base functions:

      - IFERROR instead of IFNA: =IFERROR(VLOOKUP(...),"UNKNOWN"). Base in 2007, no prefix. Caveat: it also swallows #REF!/#VALUE!, not just #N/A.
      - & or CONCATENATE instead of CONCAT: =A2&B2, or =CONCATENATE(A2,B2). Base, no prefix. CONCAT is the one that accepts ranges; & and CONCATENATE do not.
      - For MAXIFS: =SUMPRODUCT(MAX((B2:B1000="Travel")*D2:D1000)) with bounded ranges, or =MAX(IF(...)) entered as a CSE array via openpyxl's ArrayFormula. Whole-column SUMPRODUCT is slow; bound the ranges.

    Other things that limit which functions you should use:

      1. openpyxl never evaluates. It writes no cached results, so a viewer that does not recalc shows 0/blank, and load_workbook(data_only=True) on your own output returns None. Set wb.calculation.fullCalcOnLoad = True, or open/save once in Excel/LibreOffice. If you need a stored value immediately, use xlsxwriter's write_formula(..., value=...) or inject the cached value yourself.

      2. English names, comma separators, always. The file format is locale-independent: en-US function names, comma arguments, `.` decimal, double-quoted strings with `""` escaping. Excel localises on open. Never write `;` or localised names.

      3. Prefix families. `_xlfn.` for later Excel functions. A few dynamic-array/worksheet functions also need `_xlfn._xlws.` (FILTER, SORT, SORTBY). `_xll.` for add-in functions, `_xludf.` for VBA/user-defined. Never prefix base functions (SUM, VLOOKUP, IFERROR, INDEX). Mistyped or undefined names also produce #NAME?.

      4. Future functions you will hit: IFS, SWITCH, TEXTJOIN, XLOOKUP, XMATCH, MAXIFS, MINIFS, CONCAT, IFNA, LET, LAMBDA, UNIQUE, SEQUENCE, FILTER, SORT, SORTBY, RANDARRAY.

      5. Arrays and spilling. Legacy CSE arrays need ArrayFormula with the right ref. Dynamic-array functions rely on the 365 spill engine; outside 365 they error or degrade. The `@` implicit-intersection operator is 365-only.

      6. Structured references look like Table1[Column] and are fine, but are version-sensitive, and openpyxl will not create the table for you.

      7. Cross-workbook references need a real external link part; openpyxl will not build it, so `[Book2.xlsx]Sheet1!A1` will not resolve.

      8. Sheet names with spaces or punctuation need quotes: `'My Sheet'!A1`. `Customers!A:B` is fine as written.

      9. Performance/recalc: whole-column references and volatile functions (NOW, TODAY, OFFSET, INDIRECT, RAND) recalc constantly; keep ranges bounded.

      10. Reading back: openpyxl returns the stored string including `_xlfn.`; do not strip it. `data_only=True` returns cached values only.

    Bottom line: put `_xlfn.` in front of any function newer than Excel 2007, confirm the target Excel version actually has it, and downgrade to base functions (IFERROR, &, SUMPRODUCT/array MAX) when the file must be portable.

- id: xlsx-formulas-01
  answer: |
    SUMIF order: SUMIF(range, criteria, [sum_range])

    SUMIFS order: SUMIFS(sum_range, criteria_range1, criteria1, [criteria_range2, criteria2], ...)

    Note the reversal: SUMIF puts the sum range last and optional; SUMIFS puts it first and required. In SUMIFS every criteria range must match the size of sum_range.

    Formula, exact-date match:

    =SUMIFS(Ledger!$D:$D,Ledger!$B:$B,$A$2,Ledger!$A:$A,$B$1)

    Why the absolute refs: $D:$D and $B:$B keep the columns fixed when you fill down; $A$2 and $B$1 stay pinned to the category cell and the date cell.

    Assumption and caveat: I assume B1 holds a real date value, not text, and that Ledger!A contains plain dates with no time component. A true date serial will not equal a datetime such as 2026-10-31 09:15, so an exact match silently returns 0 (or misses partial-day rows). If Ledger!A may carry times, use a half-open range instead:

    =SUMIFS(Ledger!$D:$D,Ledger!$B:$B,$A$2,Ledger!$A:$A,">="&$B$1,Ledger!$A:$A,"<"&$B$1+1)

    That counts everything from B1 up to but not including the next day. If B1 is text rather than a date, neither form matches until you coerce it, e.g. DATEVALUE or a real date entry.

    SUMIF equivalent, for contrast: =SUMIF(Ledger!$B:$B,$A$2,Ledger!$D:$D) — but SUMIF takes only one criterion, so it cannot express the date condition without a helper column or array trick. That is exactly why SUMIFS is the right tool here.

- id: xlsx-formulas-02
  answer: |
    The `$` locks one half of the reference. `$` before the letter locks the column; `$` before the number locks the row. Unlocked halves shift when you fill.

    Filling down 12 rows and across 3 month columns:

    `$A2` — column locked, row free. Dragging down gives A3, A4 … A14; dragging right stays on column A. Use it when the row label lives in column A and must stay in column A for every month column. This is the classic lookup-key reference, e.g. `=VLOOKUP($A2, Data!$A:$D, ...)`.

    `B$1` — row locked, column free. Dragging right gives C1, D1, E1; dragging down stays on row 1. Use it when the month header sits in row 1 and must walk across columns while staying pinned to the header row. This is the reference that makes the 3 month columns work, e.g. `=INDEX(Data!$B$2:$D$14, MATCH($A2, Data!$A$2:$A$14, 0), MATCH(B$1, Data!$B$1:$D$1, 0))`.

    `$B$1` — both locked. It never moves in any direction. In a 12-row × 3-column fill, you only want this when all 36 cells must read the same single cell: a tax rate, FX rate, total budget, one parameter cell. If what you actually mean is "the month header," use `B$1`, not `$B$1`. That is the distinction worth getting right — `$B$1` is correct only for a true constant anchor, and using it for the header silently gives you January in all three columns.

    Quick cycle: F4 toggles A1 → $A$1 → A$1 → $A1 → A1 (on Mac Excel, Cmd+T or Fn+F4 depending on keyboard settings).

    Sheet references with spaces in the name:

    Wrap the sheet name in single quotes, then `!`, then the cell.

    `='Stock Count'!B2`
    `='Daily Report'!B2`
    `=SUM('Stock Count'!B2:B10)`
    `=VLOOKUP($A2, 'Stock Count'!$A:$D, 2, FALSE)`

    Quotes are required whenever the name contains a space or a special character. Names with no spaces work unquoted (`=Sheet2!A1`), but quoting is always safe and I would quote every time.

    Two extras that bite people:
    - If the sheet name itself contains an apostrophe, double it. Sheet named `James's Data` becomes `='James''s Data'!A1`.
    - You can span several contiguous sheets in one reference: `=SUM('Stock Count:Daily Report'!B2)` sums B2 on every sheet between those two tabs, inclusive. It only works if the tabs are adjacent and in that order.

    Uncertainty note: the exact Mac shortcut for toggling absolute references varies by Excel version and keyboard; the `$` semantics above do not.

- id: xlsx-formulas-03
  answer: |
    What happens

    System Qty = 0 makes the denominator zero, so the cell returns #DIV/0!. A blank System cell behaves the same way, because blanks are coerced to 0 in arithmetic — so a row nobody has filled in yet also blows up. The error does not stay put: any SUM, AVERAGE, COUNT, chart, or dashboard that touches the column inherits #DIV/0!, so one bad row takes out the total row with it.

    The deeper problem is mathematical, not just a spreadsheet quirk: variance against a zero baseline is undefined. x/0 is not 0% and not 100%. There is no "percentage of nothing," so no numeric answer is correct.

    What the cell should show

    Separate the cases instead of collapsing them:

    - System 0, Counted 0 — blank or "—". Nothing to report.
    - System 0, Counted > 0 — a genuine audit finding: the system has no record but stock exists. Label it NEW or "no baseline" and carry the absolute delta (Counted minus 0) in a units column. Do not force it into a percentage.
    - System 0, Counted < 0 — invalid input. Flag for investigation.

    A guarded formula:

    =IF(System=0, IF(Counted=0, "", "NEW"), (Counted-System)/System)

    Or keep the status out of the numeric column entirely:

    % column:   =IF(System=0, "", (Counted-System)/System)
    Status col: =IF(System=0, IF(Counted=0, "", "NEW"), "OK")

    Returning "" is deliberate, not cosmetic: AVERAGE, AVERAGEIF and AVERAGEIFS ignore text and blanks in a range, but they propagate errors. Blank output keeps aggregates clean without inventing data.

    If blank means "not yet entered" rather than a true zero, add ISBLANK() to tell those apart — otherwise an unfilled row gets filed as NEW.

    Why IFERROR(...,0) is a poor fix

    1. Zero is a meaningful value here. 0% variance means the count matched the system exactly — a perfect row. Writing 0 makes an undefined row indistinguishable from a validated one, so "rows within tolerance" counts, exception lists, and averages all skew in the flattering direction.
    2. It buries real findings. When System = 0 and Counted = 12, the 0 says "all good" on top of unrecorded stock. Surfacing that is the entire point of an audit sheet.
    3. It catches every error class, not just the division. #VALUE! from "3 cs" typed into a qty cell, #REF! from a deleted column, #NAME? from a typo — all silently become 0. A loud bug turns into a plausible-looking number, which is the worst failure mode in a reconciliation.
    4. It is unfilterable. Every row returns a number, so you can no longer isolate exceptions with a filter or COUNTIF without reconstructing which rows were fabricated.
    5. It propagates. Sums, weighted averages and dashboards built on that column inherit the fake zeros and understate variance.

    Better pattern: guard the denominator explicitly, return blank plus a status flag, and aggregate only over rows with a valid baseline using AVERAGEIF/COUNTIFS. If pre-existing errors must be tolerated, AGGREGATE(9,6,range) for SUM and AGGREGATE(1,6,range) for AVERAGE ignore errors — but the root fix is not to create them. Report zero-baseline rows separately as counts and absolute unit deltas.

    Confidence: high on Excel, Google Sheets and LibreOffice all returning #DIV/0! and on IFERROR's error class. Function names in other tools (Numbers, for instance) may differ slightly — I'd verify those before quoting them.

- id: xlsx-formulas-04
  answer: |
    Short answer: the formula never tests for absence, and a blank cell compares as midnight (0), so the "late" test fails and the formula falls through to its only other branch, "OK".

    Why the blank shows OK
    - The IF has two outcomes only: TRUE gives "Late", FALSE gives "OK". There is no third branch for missing data, so every row that is not late is labelled OK by construction.
    - In both Excel and Google Sheets, an empty cell used in a numeric comparison is coerced to 0. Settings!B1 is 09:00, stored as 0.375. So the test becomes 0 > 0.375, which is FALSE, and you get "OK".
    - A blank is therefore indistinguishable from a genuine 08:30 punch. The formula cannot tell "no data" from "early".
    - Related trap: if C2 holds text (e.g. "n/a" or a stray space), Excel ranks text above any number, so the same comparison returns TRUE and the row shows "Late" instead of OK. Google Sheets is inconsistent here too. Neither behaviour is what you want.
    - Second bug worth flagging: Settings!B1 is relative. Dragging the formula down row 3 makes it Settings!B2, row 4 makes it B3, and so on. It must be Settings!$B$1.

    Correct precedence
    Evaluate in this order, and stop at the first match:

    1. Absent / no-show — check-in is blank. This must be first, because a blank passes every numeric test downstream and would otherwise be silently classified as OK or Late.
    2. No check-out — check-in present, check-out blank. This is a data-completeness failure.
    3. Late — check-in present and greater than 09:00.
    4. OK — check-in present, on or before 09:00, check-out present.

    Rationale for putting "No check-out" ahead of "Late": missing data outranks a derived judgement, so you do not stamp someone Late off a half-finished record. Some payroll policies prefer Absent > Late > No check-out; either is defensible, but pick one, document it, and apply it consistently. The failure mode is an implicit precedence, which is exactly what produced the OK-for-blank bug.

    A single status column is the real design flaw. Prefer separate boolean flags (IsAbsent, IsLate, MissingCheckout, IsOK) so a late arrival with no check-out can show both conditions instead of one masking the other. If you must keep one column, an explicit nested form:

    =IF(COUNT(C2,D2)=0,"Absent",
     IF(C2="","Missing check-in",
     IF(D2="","No check-out",
     IF(C2>Settings!$B$1,"Late","OK"))))

    COUNT ignores text and blanks, so it reliably detects the both-empty case. Use C2="" rather than ISBLANK if the cell may contain a formula returning an empty string.

    Two more notes: keep the strict > if a 09:00 punch counts as on time, and switch to >= if the policy says late at 09:00. And if the sheet spans multiple days, include the date in the timestamp or the comparison will treat a 23:00 punch as before 09:00.

- id: xlsx-formulas-05
  answer: |
    Assumptions: the report is a spreadsheet (Excel or Sheets). The source data sits on another sheet or table with one row per transaction: a Date column and an Amount column. B1 is a real date the user types. "Total spent" means the sum of Amount for rows whose Date equals B1's day.

    WRONG DESIGN

    `=SUM(D2:D500)`

    or its disguised cousin, `=SUMIF(A2:A500,"1/15/2026",D2:D500)`.

    Why it fails: the first sums every row regardless of date, so the "daily" number is really an all-time number. The second bakes the date into the formula, so it is correct only on the day it was written and silently wrong forever after. A third wrong variant looks right and returns 0: `=SUMIF(A:A,B1,D:D)` when column A holds real timestamps (date plus time). Equality against a bare date never matches a timestamp, so the cell shows 0 with no error — the worst kind of wrong, because it looks like a quiet day.

    The deeper failure in all three: the cell is not driven by B1 and the data. It is driven by a literal, a fixed range, or an equality that the data can never satisfy.

    RIGHT DESIGN

    `=SUMIFS(Spend, Dates, ">="&$B$1, Dates, "<"&$B$1+1)`

    with Spend and Dates as structured references to an Excel Table (or whole-column references if you accept the cost). Key properties:

    - B1 is the only date input; nothing is hardcoded.
    - Half-open interval (>= B1, < B1+1) includes timestamped rows on that day and excludes the next day at 00:00.
    - Table references auto-expand when rows are added, so new transactions are picked up without editing the formula.
    - `$B$1` is absolute so the formula copies safely.

    Equivalent in Sheets: `=SUMIFS(Spend, Dates, ">="&$B$1, Dates, "<"&$B$1+1)` or `=SUMPRODUCT((INT(Dates)=INT($B$1))*Spend)` if the dates are messy.

    Prerequisites that make it correct: B1 and the Date column are true date serials (not text), Amount is numeric (not text with currency symbols), and the ranges are the same height.

    HOW TO TEST IT IS REALLY LIVE

    1. Formula-bar check. Select the cell. It must show a formula, not a bare number. If it shows a number, someone pasted values and it will never update.
    2. Perturb the input. Set B1 to a date whose known total is distinctive (say 1,234.56) and confirm the cell matches and differs from adjacent days. Then set B1 to a date with no rows and confirm it returns 0 — not blank, not an error, not the previous value.
    3. Perturb the source. Add a temporary row for B1's date with Amount +100. The total must rise by exactly 100. Delete the row; it must fall back. This proves the cell reads the data, not a cache.
    4. Timestamp test. Add a row dated B1 at 14:30. It must be included. Add one dated B1+1 at 00:00. It must be excluded. This catches the equality-match bug.
    5. Recalculation check. Confirm Calculation Options is Automatic (or press F9 / Ctrl+Alt+F9) and watch the value change. If it only updates on manual recalc, it is not live for a daily report.
    6. Undo test. Note the value, change B1, undo. The value must revert. A static paste will not.
    7. If the sheet is fed by a query, pivot, or external connection, change the source and hit Refresh. Confirm the number moves and that the connection is a real query, not a one-time extract.

    The single strongest test is 2 plus 3: change the input, change the source, and watch the number track both. A formula that survives both is live; anything that survives only one is a copy wearing a formula's clothes.

    Uncertainty: I am assuming the "right" design targets a spreadsheet formula. If "live" instead means a scheduled job or BI refresh, the same logic applies — perturb the source, confirm the rendered number moves, and verify the schedule actually ran — but the specific cell test becomes a pipeline test.

- id: xlsx-formulas-06
  answer: |
    Assumptions: D4:D9 is a normal range, not an Excel Table; row 10 is a full-row insert above the existing Total row; the Total formula is exactly `=SUM(D4:D9)`.

    Excel
    - The Total row moves down to row 11. The formula moves with it.
    - The formula remains `=SUM(D4:D9)`. Row 10 is immediately below the last referenced row, row 9, so the inserted row is outside the range. Excel does not expand a fixed range for an insert at its bottom edge.
    - If the clerk types a value in the new D10, the total ignores it.
    - Excel does expand the range when the insert is inside it. For example, insert at row 9 and `D4:D9` becomes `D4:D10`.
    - Any other formula that pointed to the old Total cell D10 follows it to D11.

    openpyxl
    - `ws.insert_rows(10)` inserts a blank row before the old row 10 and shifts rows 10 and below down. The Total formula moves from D10 to D11.
    - openpyxl does not rewrite the formula text. D11 still contains `=SUM(D4:D9)`, so the new D10 is excluded.
    - openpyxl does not manage dependencies: formula references, tables, merged cells, conditional formatting, data validation, charts, and defined names are not adjusted. If the script inserts inside the range instead, the unchanged references can point at shifted data and silently produce a wrong total.
    - openpyxl does not calculate formulas. Excel recalculates on open, but it recalculates the old range.

    How to design it
    1. Best for human clerks: use an Excel Table with a Totals Row. Inserting a row inside the table, including directly above the totals row, expands the table. The total uses a structured reference or `SUBTOTAL` and includes the new row. In openpyxl, update `table.ref` after any insert because openpyxl will not.
    2. Best for plain ranges and scripts: make the total self-adjusting, for example `=SUM(D4:INDEX(D:D,ROW()-1))`. It sums from D4 to the row immediately above the total. When the total moves down, `ROW()` changes and the inserted row is included. This survives `ws.insert_rows`. Keep the data column free of stray numbers. Avoid `=SUM(D4:OFFSET(D10,-1,0))`: it is volatile and the hardcoded D10 breaks when the formula moves.
    3. Best for scripted pipelines: keep raw data and totals on separate sheets. On the summary sheet use `=SUM(Data!D:D)` or a bounded whole-column range. There is no circular reference and no range to maintain.
    4. If you keep a fixed range: insert new rows inside the range, such as at row 9, not at the boundary above Total. Also have the script rewrite the formula or table reference after insertion. Moving the Total above the data by itself does not fix a fixed bottom edge; pair it with a table, a dynamic formula, or a whole-column sum.

    Bottom line: a fixed `=SUM(D4:D9)` does not grow when a row is inserted at row 10. Both Excel and openpyxl leave the new row out. Use a Table, a dynamic total, or a separate summary sheet.

- id: xlsx-lookup-01
  answer: |
    **Pull the name**

    ```
    =VLOOKUP(B2, Roster!A:C, 2, FALSE)
    ```

    Returns the Name column (2nd column of A:C) for the EmpID in B2. `FALSE` can also be written `0`.

    **Fourth argument — `range_lookup`**

    It chooses the match mode:

    - `FALSE` / `0` → exact match. Scans until it finds the key exactly.
    - `TRUE` / `1` / omitted → approximate match. Assumes column A is sorted ascending, and returns the largest value **less than or equal to** the key.

    **What goes wrong if omitted**

    Excel defaults to `TRUE`. On unsorted roster data the binary search lands on an arbitrary row, so you get a plausible but wrong name — no error, just silent corruption. On sorted data it returns the nearest lower ID, so `B2` = 1042 with IDs 1040 and 1050 present returns the 1040 row's name. Classic "why is the lookup off by one record" bug, and it propagates into payslips and reports before anyone notices.

    **INDEX/MATCH equivalent**

    ```
    =INDEX(Roster!B:B, MATCH(B2, Roster!A:A, 0))
    ```

    `MATCH` finds the row position of B2 in column A (the `0` forces exact match), `INDEX` returns the value at that position in column B.

    **Why prefer it**

    It does not hardcode the return-column number. `VLOOKUP(..., 2, ...)` silently returns the wrong field the moment someone inserts or deletes a column inside `Roster!A:C` — and nothing errors. `INDEX/MATCH` binds to the column *reference* (`Roster!B:B`), so structural edits to the sheet leave it correct. Secondary wins: it can look leftward, does not require sorted keys, and is cheaper on large ranges because MATCH touches one column instead of the whole block.

    **Assumption:** `B2` holds a text or numeric EmpID in the same form as column A. Mixed types (text `"1042"` vs number `1042`) will fail exact match — wrap with `TRIM()` or coerce if the source is messy.

- id: xlsx-lookup-02
  answer: |
    Assumptions: Excel 365, Orders is a table with `CustID`, `OrderDate`, `CustName`, `TermsDays`, `DueDate`, `DaysOverdue`. Customers table has `ID`, `Name`, `TermsDays`. Default terms = 30 when the customer is missing.

    Core principle

    Fix the absence at the point where it happens — the lookup. Never let an `#N/A` leave the first cell of the chain. Every later column then does arithmetic on real values, so nothing propagates.

    Build the chain left to right, one column per step

    Step 1 — customer name
    ```
    = XLOOKUP([@CustID], Customers[ID], Customers[Name], "UNKNOWN")
    ```
    or, with VLOOKUP:
    ```
    = IFNA(VLOOKUP([@CustID], Customers, 2, FALSE), "UNKNOWN")
    ```

    Step 2 — payment terms
    ```
    = XLOOKUP([@CustID], Customers[ID], Customers[TermsDays], 30)
    ```
    This must return a number, never `""`. Empty text in a numeric column is what poisons the next step.

    Step 3 — due date
    ```
    = [@OrderDate] + [@TermsDays]
    ```
    Terms is always numeric now, so this cannot error. That is the whole point of staging the columns.

    Step 4 — days overdue
    ```
    = IF([@DueDate] = "", "", MAX(0, TODAY() - [@DueDate]))
    ```
    Use `TODAY() - [@DueDate]` without `MAX` if you want negative values to mean "not yet due". Guard the blank case, because blank minus date gives a nonsense serial number and text minus date gives `#VALUE!`.

    Optional — flag the row
    ```
    = [@CustName] = "UNKNOWN"
    ```
    so you can filter or exclude unknown customers from reporting.

    Single-cell version, if you insist

    ```
    = IFNA(TODAY() - ([@OrderDate] + IFNA(XLOOKUP([@CustID], Customers[ID], Customers[TermsDays]), 30)), "")
    ```
    Workable, but it collapses four columns into one and hides which assumption produced the date. Staged columns are easier to audit.

    IFERROR versus IFNA

    `IFNA(value, value_if_na)` catches exactly one error: `#N/A`. Added in Excel 2013. Its meaning is "the lookup found no match."

    `IFERROR(value, value_if_error)` catches all seven error types: `#N/A`, `#VALUE!`, `#REF!`, `#DIV/0!`, `#NUM!`, `#NAME?`, `#NULL!`. Available since Excel 2007. Its meaning is "something, somewhere in this expression, went wrong."

    Why the distinction matters here

    - A missing customer is an expected business state. `#N/A` is the correct signal for it, so `IFNA` is the semantically right catcher.
    - `#REF!`, `#VALUE!`, and `#NAME?` are defects: a deleted column, text in a numeric cell, a misspelled table name. `IFERROR` swallows those too, and you ship silently wrong numbers with no warning.
    - So default to `IFNA` on every lookup in this chain. Reserve `IFERROR` for a genuinely mixed bag, such as a free-text date import: `=IFERROR(TODAY()-DATEVALUE([@DueDateText]),"")`. Even there an explicit `IF` reads better.
    - Prefer XLOOKUP's built-in `if_not_found` argument over wrapping it in `IFNA`: one evaluation, self-documenting, no nested function to mis-edit.

    Two traps

    - The fallback type must match the column. `IFNA(lookup,"")` in a date column creates mixed types, and the next arithmetic step throws `#VALUE!`. Use a real date or a numeric default.
    - A missing customer silently inherits default terms and produces a plausible but invented due date. Decide the policy: either default the terms and flag the row, or leave the due date blank and exclude the row from aging. Do not bury the gap under a number.

    If you are on Excel 2010 or older, `IFNA` does not exist; use `IF(ISNA(x), fallback, x)`.

- id: xlsx-lookup-03
  answer: |
    Short answer: VLOOKUP compares the stored value and its data type, not the text you see on screen. Text "007" and the number 7 are different values. "C001 " with a trailing space is a five-character string, and "C001" is four. Excel finds no equal value and returns #N/A.

    ASSUMPTION: Excel semantics. The same root causes apply in Google Sheets and LibreOffice, with minor differences in TRIM and CLEAN.

    WHY IT HAPPENS

    1. Type mismatch.
    A cell that displays 007 is either the text string "007" or the number 7 with a custom format "000". A cell that displays 7 is usually a number. VLOOKUP does not coerce text to number or number to text. So "007" never equals 7.

    2. Hidden characters.
    The visible ID carries characters you cannot see:
    - trailing or leading space (CHAR 32)
    - non-breaking space (CHAR 160), common in web and ERP exports
    - tab (CHAR 9), line feed (CHAR 10), zero-width characters
    "C001 " looks identical to "C001" but is not equal to it.

    3. Numbers stored as text after import.
    CSV imports and pasted web tables often store digits as text. The cell may show a green triangle or an apostrophe prefix.

    4. Missing fourth argument.
    VLOOKUP defaults to approximate match when you omit the last argument. On unsorted data it returns #N/A or the wrong row. Always pass 0 or FALSE.

    5. Structural causes.
    The key column sits to the right of the return column, so VLOOKUP cannot reach it. Or the column index is wrong, which usually gives #REF! or a wrong value.

    NOT the cause: font, fill colour, bold, case (VLOOKUP is case-insensitive), hidden rows, or number format on its own. Formatting changes display only. It does not change the stored value.

    DIAGNOSE

    Test both the lookup cell and a known matching cell in the table. The table side is where most bugs hide.

    1. Test the type of each cell.
       =ISNUMBER(A2)
       =ISTEXT(B2)
    One TRUE and one FALSE explains the failure immediately.

    2. Test the length of each cell.
       =LEN(A2)
       =LEN(B2)
    "C001" returns 4. "C001 " returns 5. A different length means padding or hidden characters.

    3. Test the last character code.
       =CODE(RIGHT(A2,1))
    32 is a space, 160 is a non-breaking space, 9 is a tab.

    4. Test strict equality.
       =EXACT(A2,B2)
    Do not trust =A2=B2 here. That comparison coerces types and can return TRUE while VLOOKUP fails. That trap is why the bug looks impossible.

    5. Show the raw value with delimiters.
       ="["&A2&"]"
       ="["&B2&"]"
    Spaces and stray characters become visible inside the brackets.

    6. Count strict matches in the table.
       =SUMPRODUCT(--EXACT(range,A2))
    Zero means no true match exists, even though your eyes see one.

    7. Inspect the real values.
    Use Formulas > Evaluate Formula, or select part of the formula and press F9.

    FIX BY CAUSE

    Type mismatch, both sides should be numbers:
    - Convert with =VALUE(A2) or =A2*1 or =--A2.
    - Or select the column and run Data > Text to Columns > Finish.
    - Or use Paste Special > Multiply by 1.

    Type mismatch, both sides should be text:
    - Force the lookup value to text: =VLOOKUP(A2&"", range, col, 0), valid only when the table key is text.
    - Or convert the table key with TEXT.

    Leading zeros, "007" versus 7:
    - Pick one canonical form.
    - Keep the zeros with =TEXT(A2,"000") on both sides.
    - Or drop them with =VALUE(A2) on both sides.

    Trailing space or non-breaking space:
    - Plain space: =TRIM(A2).
    - Non-breaking space: =TRIM(SUBSTITUTE(A2,CHAR(160)," ")).
    - All common junk: =TRIM(SUBSTITUTE(CLEAN(A2),CHAR(160)," ")).
    - Clean the table column too. Cleaning only the lookup value is the most common failed fix.

    Robust pattern with a helper key column:
       key = TRIM(SUBSTITUTE(CLEAN(A2),CHAR(160)," "))
       =VLOOKUP(key, helperRange, col, 0)

    Always pass the fourth argument:
       =VLOOKUP(A2, range, 2, 0)

    Key is right of the return column:
       =INDEX(returnRange, MATCH(A2, keyRange, 0))

    Formatting changed after entry:
    - Formatting alone never converts a value. Re-enter it, or use Text to Columns.

    Wildcard band-aid:
       =VLOOKUP(A2&"*", range, col, 0)
    This absorbs trailing junk but can match the wrong row. Prefer cleaning the data.

    VERIFY

    1. =EXACT(cleanLeft, cleanRight) returns TRUE.
    2. =SUMPRODUCT(--EXACT(range, cleanKey)) is at least 1.
    3. VLOOKUP returns the expected value, not #N/A.

    PREVENT

    1. Store IDs as text in both systems, or as numbers in both. Never mix.
    2. On import, set the key column to Text with Text to Columns.
    3. Trim all key columns on load, not at lookup time.
    4. Use exact match everywhere, preferably XLOOKUP or INDEX/MATCH.
    5. Add a check formula or conditional format that flags LEN differences between the two keys.

- id: xlsx-dates-01
  answer: |
    Excel does not store a date as text. It stores a number.

    STORAGE MODEL

    A date is a serial integer: the count of days since the workbook epoch. The default (Windows, "1900 date system") epoch is 1899-12-30, so serial 1 is 1900-01-01. A time of day is the fractional part of the same number. 0.5 is noon, 0.25 is 06:00, 1 hour is 1/24, 1 second is 1/86400. A datetime is just date+time, e.g. 46323.604166... is 2026-10-28 14:30. One cell holds one float; the display comes from the number format.

    Two quirks matter. First, Excel keeps a fake 1900-02-29 at serial 60 (Lotus 1-2-3 compatibility), so serials 1..59 are one day ahead of the true calendar and serials 61+ use the 1899-12-30 epoch correctly. Second, Mac/legacy workbooks may use the "1904 date system" with epoch 1904-01-01. The two systems differ by 1462 days, so a file read under the wrong epoch shifts every date. openpyxl exposes this as `wb.epoch` (WINDOWS_EPOCH or MAC_EPOCH).

    WHY 46323

    46323 is not random. It is the correct 1900-system serial for 2026-10-28: 2026-01-01 is 46023, plus 300 days to Oct 28 gives 46323. So the cell holds a real date; the number format is General (or "0"). Excel is showing the date's underlying value. The fix is formatting, not data: set `number_format = "yyyy-mm-dd"`, or press Ctrl+Shift+# in Excel, or Format Cells > Date. You also see 46323 in Python when openpyxl reads a numeric cell whose format is not a date format — it returns the raw number instead of a datetime.

    WHAT THE STRING BREAKS

    Writing "2026-10-28" makes a text cell (type "s"), left-aligned. Consequences:

    - Subtraction: A2-A1 returns #VALUE!. So do YEAR, MONTH, DAY, WEEKDAY, EDATE, EOMONTH, NETWORKDAYS on those cells.
    - SUMIFS/COUNTIFS: a real-date criterion is numeric and never equals text, so exact-match sums return 0. Inequality criteria like ">="&DATE(2026,10,1) are worse: Excel ranks text above all numbers, so text rows can satisfy ">=" against any number and pollute totals.
    - Sorting: mixed text and numbers sort as two separate blocks (numbers first), so chronological order fails. ISO text sorts correctly only among itself and only if the format is consistent.
    - Filters, pivot date grouping, charts with date axes, conditional formatting rules keyed on dates, and data validation all treat the column as text.
    - Visual tell: text is left-aligned, dates are right-aligned. Excel's "number stored as text" warning does not fire for date-looking strings, so the break is silent.
    - Locale trap: DATEVALUE("2026-10-28") fails in US locale (expects m/d/y), so cleanup in Excel is unreliable.

    HOW TO WRITE IT WITH openpyxl

    Pass Python date/datetime/time objects. openpyxl converts them to serials and tags the cell as a date.

        from datetime import date, datetime, time
        from openpyxl import Workbook

        wb = Workbook()
        ws = wb.active

        ws["A1"] = date(2026, 10, 28)
        ws["A1"].number_format = "yyyy-mm-dd"

        ws["A2"] = datetime(2026, 10, 28, 14, 30)
        ws["A2"].number_format = "yyyy-mm-dd hh:mm"

        ws["A3"] = time(14, 30)
        ws["A3"].number_format = "hh:mm"

        wb.save("out.xlsx")

    Number format is not strictly required for correctness; openpyxl sets a default date format for datetime values. Set it explicitly anyway so the display matches the intent. If your source is a CSV or JSON string, parse first: `datetime.strptime(s, "%Y-%m-%d").date()`, or `datetime.fromisoformat(s)`. Never assign the string. If you must write a raw serial, use `openpyxl.utils.datetime.to_excel(dt)` and still set a date format.

    OTHER GOTCHAS

    Serials are IEEE doubles with about 15 significant digits. Date equality is exact, but time equality can fail on float noise; compare with ABS(a-b) < 1e-9 or round to the minute. Excel has no timezone, so openpyxl writes tz-aware datetimes by dropping tzinfo — normalize to UTC yourself. Also, a string starting with "=" is written as a formula, not text — escape it if that is not what you want. And if you are using pandas, `df.to_excel` preserves datetime64 columns correctly; a column of mixed strings silently becomes text.

    BOTTOM LINE: the string is inert text and breaks all date semantics; 46323 is the correct serial for 2026-10-28 and only means the cell's number format is General.

- id: xlsx-dates-02
  answer: |
    Assume a spreadsheet (Excel / Google Sheets) where Check-in is column A, Check-out is column B, row 2 is the first data row, and each cell holds a real time value.

    DAY SHIFT — hours to two decimals, blank if either punch is missing:

    =IF(OR(A2="",B2=""),"",ROUND((B2-A2)*24,2))

    - B2-A2 gives a fraction of a day; ×24 converts to hours.
    - OR(...) blanks the row when a punch is missing instead of showing a negative or an error.
    - ROUND(...,2) stores the value at two decimals.

    NIGHT SHIFT 22:00 → 06:00 — what changes:

    Check-out is on the next calendar day, so B2-A2 is negative (–16h) and the plain formula returns –16.00 or a ###### error. Wrap the difference in MOD:

    =IF(OR(A2="",B2=""),"",ROUND(MOD(B2-A2,1)*24,2))

    MOD(difference, 1) treats the times as a clock and returns the forward-wrapped elapsed time: MOD(–16/24, 1) = 8/24 → 8.00. The same formula still gives the correct day-shift result, so one column works for both shift types.

    Points to watch for the night case:

    - Real time values only. If "22:00" was typed as text, wrap with TIMEVALUE(A2) and TIMEVALUE(B2), or re-enter as time.
    - Exactly 24 hours reads as 0.00, because MOD(x, 1) cannot distinguish a zero-length shift from a full-day shift. If 24-hour shifts are possible, test for B2=A2 explicitly.
    - If the cells hold full date+time timestamps, do not use MOD. Plain (B2-A2)*24 is already correct across midnight, and MOD would corrupt any shift longer than 24 hours.
    - Break deduction goes after the MOD, e.g. ROUND(MOD(B2-A2,1)*24,2) - BreakHours, kept inside the IF.
    - If sums must stay exact, keep full precision and apply the number format 0.00 instead of ROUND; ROUND changes the stored value and can make totals drift from the displayed rows.
    - Displaying the raw difference as a duration needs the format [h]:mm, since h:mm wraps at 24 hours.
    - Neither form handles daylight-saving transitions or cross-midnight punches where the date is not implied; those need stored dates, not bare clock times.

    Uncertainty: the exact cell layout and whether your times are stored as serials or text are assumptions. The formulas above are correct for serial time values; text entry is the most common cause of a wrong result.

- id: xlsx-dates-03
  answer: |
    Assumptions: Excel/Google-Sheets serial model (a day = 1.0, 09:00 = 0.375), time-only cells, and the rule "late = strictly after 09:00:00" (exactly 09:00 is on time).

    Short answer: `0.375` and `TIME(9,0,0)` are the *same* number, and both are exact. 9 h = 32400 s, and 32400/86400 = 3/8 = 0.375, which has a finite binary expansion. So the cutoff literal is not what makes the flag flip. The instability is on the *check-in* side, plus how the comparison is written.

    Why the boundary is fragile

    1. The input value is rarely exact. A typed `9:00` parses to exactly 0.375, but a value that comes from arithmetic (end minus break, start plus duration), from `NOW()`, from a time-clock export, or from a paste can land at 0.37499999999999994 or 0.37500000000000006. Any strict `>` then flips. One employee flagged "only sometimes" is the signature of a check-in that sits within one floating-point step of the cutoff on some days and not others, depending on entry path.

    2. Strict vs inclusive comparison. `>` flags exactly-09:00 as late if the stored value is one ulp above 0.375; `>=` flags exactly-09:00 always. Mixing `>` in one formula and `>=` in another produces different answers for the same row.

    3. Text vs number. A time imported as text (`"09:00"`) is not 0.375. In Excel, text compares greater than every number, so `="09:00">0.375` is TRUE. If only some rows are text, those rows are always flagged and the rest are not. The display looks identical.

    4. Date component. If the cell holds a full timestamp (date + time), the serial is huge and always greater than a bare fraction. Compare the fractional part only, with `MOD(A2,1)`.

    5. Display rounding. A cell formatted `hh:mm` can show `09:00` while holding 09:00:00.4 or 08:59:59.8. The formula sees the real value, not the displayed one.

    6. Locale and parsing. `0.375` pasted into a comma-decimal locale, or `"9:00 AM"` parsed under a 24-hour locale, can become text or a different value.

    How to make it robust

    - Single source of truth. Put the cutoff in one named cell, e.g. `Cutoff = TIME(9,0,0)`. Reference it everywhere. Never retype `0.375` in some formulas and `TIME(9,0,0)` in others.
    - Decide the granularity and quantize. If the policy is per-minute, convert to whole minutes and compare integers:
      `Late = IF(ROUND(MOD(A2,1)*1440,0) > 540, "Late", "On time")`
      If you need seconds: `ROUND(MOD(A2,1)*86400,0) > 32400`.
    - Or compare with an explicit tolerance instead of on the boundary:
      `Late = IF(MOD(A2,1) > Cutoff + 1/86400, "Late", "On time")` (one-second slack).
      For equality tests use `ABS(MOD(A2,1)-Cutoff) < 1/86400`, never `=`.
    - Normalize inputs. Validate that times are numeric (`ISNUMBER`), coerce text with `TIMEVALUE`/`VALUE`, strip dates with `MOD(A2,1)`, and use data validation or a Time number format on entry.
    - State the boundary rule in the sheet. Write "late if after 09:00:00; 09:00:00 exactly is on time" next to the cutoff cell so the `>` vs `>=` choice is deliberate, not accidental.

    The core point: exact decimal literals are fine here; tolerance and normalization are what remove the surprises. Compare quantized integers (seconds or minutes) against one named cutoff, or use a documented epsilon, and the same employee stops flickering.

    Uncertainty: if your platform is not Excel/Sheets (e.g., a database `TIME` type or a language with decimal floats), the exact representation of 0.375 still holds, but the text-coercion and display-rounding points differ.

- id: xlsx-dates-04
  answer: |
    Due Date formula

    Due Date = Order Date + Payment Term (in days)

    Excel/Sheets, with Order Date in B2 and term days in C2:
      =B2+C2
    Format the result as a date. Keep Order Date as a real date serial, not text, so the add works.

    Days Overdue formula

    Days Overdue = MAX(0, As Of - Due Date)

      =MAX(0, $AsOf - D2)

    With an outstanding-balance guard (paid orders are never overdue):
      =IF(Outstanding<=0, 0, MAX(0, $AsOf - D2))

    If you also want "days until due" for open orders, the same subtraction without MAX gives a negative number; keep the two metrics separate rather than overloading one column.

    When an order is overdue — precisely

    An order is overdue when both conditions hold:
      1. As Of > Due Date (strictly greater, i.e. at least one day past due), and
      2. Outstanding balance > 0 (not fully paid, no credit note clearing it).

    Consequences of that boundary:
      - As Of = Due Date: due today, Days Overdue = 0, not overdue.
      - As Of < Due Date: not yet due, Days Overdue = 0.
      - As Of = Due Date + 1: overdue by 1 day.

    State the assumption explicitly: this counts calendar days and treats the due date itself as still on time. Some shops use "day after due date" or business days; if the term is "Net 30" that usually means 30 calendar days from the invoice/order date, not 30 working days. Pick one convention and document it, because the boundary day changes every aging report.

    Why "as of" must not be TODAY()

    TODAY() is volatile: it recalculates on every open, so the same workbook produces different numbers tomorrow with no data change. That breaks four things:

      - Reproducibility: you cannot rerun last month's aging and get last month's answer.
      - Audit and reconciliation: a saved report drifts away from the GL snapshot it was tied to; reviewers cannot tie out figures they saw yesterday.
      - Point-in-time reporting: month-end close, 30/60/90 buckets, and "overdue at period end" all need a frozen date. TODAY() silently re-ages every open order.
      - Testing and automation: any check or scheduled export becomes flaky, and a long batch run can flip an order from "due" to "overdue" mid-run.

    Use one named "As Of" input cell (e.g. $AsOf, or a parameter sheet cell seeded from a close date) referenced absolutely by every row. That gives a single, backdatable snapshot, keeps all rows consistent, and lets you produce historical aging on demand. If you genuinely need a live view, drive $AsOf from a separate, clearly labeled "live" sheet — never hard-wire TODAY() into the calculation column.

    Assumptions stated: calendar-day terms, due date inclusive (on-time through the due date), overdue measured on outstanding balance only. If your policy differs on the boundary day or uses working days, adjust the comparison (> vs >=) and the day-count function accordingly.

- id: xlsx-charts-01
  answer: |
    Assumptions: one day's spend split into a handful of categories (≤6); attendance is one daily value across ~30 days, single series; the ranking is 10 reps ordered by one metric.

    1. Share of the day's expenses by category — pie or donut chart. It is genuinely part-to-whole with few slices. If you must compare several days side by side, use a 100% stacked bar instead; pies do not sit next to each other well.

    2. Budget vs actual per category — grouped (clustered) bar chart, one pair of bars per category. If the real question is "how far over or under," a diverging variance bar or a bullet chart per category is sharper.

    3. Attendance rate per day over a month — line chart, days on the x-axis, with a target/reference line. Columns are acceptable for ~30 discrete days, but the line shows trend and dips better.

    4. The 10 reps' ranking — horizontal bar chart, sorted by value, names on the y-axis so long labels fit.

    Pie chart is the wrong choice for the 10-rep ranking: ten slices produce thin wedges, near-equal values are indistinguishable by angle, and a ranking is not a part-to-whole relationship. Angle comparison is also weaker than bar-length comparison for ordering. The same objection kills a pie for attendance over a month — time is not a whole.

    Caveat: these are conventions, not laws. Some style guides ban pie charts outright; if yours does, use a sorted bar for case 1 too.

- id: xlsx-charts-02
  answer: |
    Assumptions: column A holds category names, column B holds spend, and row 8 "Total" equals the sum of rows 2–7. I am confident on the range and the 50% slice; moderately confident on the exact zero-label rendering, which varies by Excel version.

    WHAT THE CHART LOOKS LIKE

    `Reference(ws, min_col=2, min_row=2, max_row=8)` selects B2:B8. That is seven data points, not six. The Total row becomes a seventh slice.

    Because Total = sum of the six categories, the pie's implied whole is 2× the real spend. The Total slice is therefore exactly 50% of the pie, and every real category is drawn at half its true share. Visually the Total wedge dominates and looks like it "is" half the business.

    Also, `add_data` alone sets no categories. With no `set_categories` call, the legend shows 1–7, not your names. If you also pointed categories at A2:A8, the legend would show six names plus the literal word "Total".

    THE RIGHT RANGE

    Data: `Reference(ws, min_col=2, min_row=2, max_row=7)` — six values, Total excluded.

    Categories: `Reference(ws, min_col=1, min_row=2, max_row=7)` — the six matching names.

    Canonical form:

        labels = Reference(ws, min_col=1, min_row=2, max_row=7)
        data   = Reference(ws, min_col=2, min_row=1, max_row=7)
        pie.add_data(data, titles_from_data=True)
        pie.set_categories(labels)

    `min_row=1` plus `titles_from_data=True` uses B1 as the series title. Categories still start at row 2. Keep the point count and label count equal; a seven-point series with six labels is a mismatch Excel renders badly.

    A CATEGORY WITH 0 SPEND

    A zero value has a zero angle, so no wedge is drawn. The name still appears in the legend if it is in the categories range, but there is nothing to see and typically no 0% data label. Nothing is wrong with the chart; a pie simply cannot show a zero share.

    Practical choices:
    - Keep the zero row if the legend should list every category. Accept the empty wedge.
    - Filter zero rows out before building the chart if you want a clean legend and clean labels.
    - Use a bar chart instead when zeros (or small values) matter, because bars show "zero" honestly.
    - If all six values are zero, a pie is meaningless — percentage labeling has no whole to divide by. Guard that case.

    Pie charts also require non-negative values; negatives break the chart, so clamp or filter them too.

    BOTTOM LINE: B2:B8 turns the Total into a 50% slice and halves every real category. Use B2:B7 for data and A2:A7 for labels; a zero category simply produces no visible wedge.

- id: xlsx-charts-03
  answer: |
    Assumptions: the workbook/sheet already holds the table in A1:C7, and the sheet is the active one (adjust `ws` if not). Closed-book, so I flag the one spot where openpyxl versions differ.

    Steps
    1. Import the chart pieces.
       `from openpyxl.chart import BarChart, Reference, Legend`
    2. Create a column chart and force clustered grouping. The default `type` is horizontal (`"bar"`), so set it explicitly.
       `chart = BarChart()`
       `chart.type = "col"`
       `chart.grouping = "clustered"`
       `chart.overlap = -10`
       `chart.gapWidth = 60`
    3. Build the references. Include row 1 in the data reference, exclude it from the category reference.
       `data = Reference(ws, min_col=2, max_col=3, min_row=1, max_row=7)  # B1:C7`
       `cats = Reference(ws, min_col=1, min_row=2, max_row=7)            # A2:A7`
    4. Attach them, telling openpyxl row 1 holds the series names.
       `chart.add_data(data, titles_from_data=True)`
       `chart.set_categories(cats)`
    5. Set the title and both axis titles. Note `x_axis` is the category axis.
       `chart.title = "Spent vs Budget"`
       `chart.x_axis.title = "Category"`
       `chart.y_axis.title = "Amount"`
    6. Ensure a legend exists, then place it. Some versions already create one; the guard is harmless either way.
       `if chart.legend is None:`
       `    chart.legend = Legend()`
       `chart.legend.position = "b"`
    7. Size it and anchor it clear of the table. The table ends at C7; E2 leaves a spacer column, or anchor at A9 to sit below.
       `chart.width = 16`
       `chart.height = 9`
       `ws.add_chart(chart, "E2")`
    8. Save.
       `wb.save("budget.xlsx")`

    Two things that commonly go wrong
    1. Wrong series names and a junk category. If `titles_from_data=True` is omitted (or the header row is left out of the reference), Excel shows “Series1”/“Series2” and may plot the header text as a data point. If the category reference starts at row 1, the first category literally becomes “Spent”. The two references must be asymmetric: data from row 1, categories from row 2.
    2. Bars render horizontal, overlapping, or stacked instead of side-by-side columns. Forgetting `chart.type = "col"` gives the default horizontal bar chart, and on that layout the x/y axis titles look swapped. Leaving `overlap` unset lets Excel draw the series on top of each other instead of separated clusters.

    Also worth knowing: openpyxl writes no cached chart values, so thumbnails, LibreOffice, and some viewers show an empty plot until Excel recalculates; and the anchor is your only protection against covering the table, since the chart has no auto-placement.

- id: xlsx-summary-01
  answer: |
    No. openpyxl cannot create a native Excel PivotTable from scratch.

    openpyxl has read/preserve support for existing pivot tables: it can load a workbook containing a pivot table and write it back. It does not provide a supported high-level way to build the pivot cache, field layout, relationships, and pivot definition. It also has no calculation engine. You can hand-build the OOXML parts, but that is fragile and can make Excel repair the file.

    Assumption: the Data sheet has `Region`, `Month`, and a numeric measure such as `Amount`; you want Region on rows, Month on columns, and Sum of Amount as the value. Confirm whether Month is text or a real date, because date grouping behaves differently.

    OPTIONS AND WHAT TO TELL THE USER

    1. Native PivotTable through Excel automation (win32com/pywin32, xlwings, AppleScript)
    This is the most reliable way to get a real PivotTable with refresh, drill-down, grouping, and slicers.

    Tell the user: it requires desktop Excel installed and licensed on the machine doing the generation. COM is Windows-only; xlwings/AppleScript can reach macOS Excel but still require Excel. It is not suitable for a headless Linux server. Excel can prompt, hang, or lock the file. Release COM objects and quit Excel in a `finally` block. Version differences matter.

    2. Pre-built template plus openpyxl
    Build the PivotTable once in Excel. Use openpyxl only to write new data into the source sheet, then save. The existing pivot is preserved.

    Tell the user: openpyxl does not refresh or recalculate the pivot cache. Excel must refresh on open or the user must click Refresh. New rows appear only if the source range or Excel Table covers them. Slicers, timelines, and some pivot extensions may be lost when openpyxl rewrites the file, so test the output. The template must be maintained when columns or sheet names change. This is often the best server-side compromise when the final file will be opened in Excel.

    3. LibreOffice DataPilot / headless UNO
    LibreOffice can create a pivot-like DataPilot table without Excel.

    Tell the user: DataPilot is not identical to an Excel PivotTable. Layout, formatting, field behavior, and refresh semantics can differ after saving to `.xlsx`. Test the result in Excel. It requires LibreOffice on the host and may be slow or awkward to automate.

    4. Commercial library (for example Aspose.Cells, and similar products)
    These can create real pivot tables without Excel.

    Tell the user: this adds a paid license, vendor lock-in, and a larger dependency. Output fidelity and Excel-version compatibility must be validated. It is a reasonable choice for server-side native-pivot generation if there is budget.

    5. Static cross-tab with pandas and openpyxl
    Compute the aggregation with pandas, then write a normal formatted table with openpyxl.

    Tell the user: this is a summary report, not a PivotTable. Values are frozen; there is no refresh, drill-down, expand/collapse, slicers, or field rearrangement. New data or new regions/months require rerunning the script and possibly extending the layout. It is portable, deterministic, and good for a one-off report, email, or PDF. Label it “Region × Month Summary,” not “PivotTable.”

    6. Formula-driven cross-tab with SUMIFS
    Write a grid of `SUMIFS` formulas with openpyxl.

    Tell the user: it recalculates in Excel, so it can stay current. But openpyxl does not calculate formulas, so there are no cached values until Excel opens the file; a reader using `data_only=True` may see `None`. It needs fixed lists of regions and months, and new categories require extending the formulas. Performance degrades on large data. It is still not a PivotTable.

    7. Hand-built OOXML pivot
    Technically possible, not recommended.

    Tell the user: this is unsupported, high-maintenance, and carries a real risk of Excel repair prompts or a pivot that will not refresh. Do not promise this as a normal deliverable.

    WHAT I WOULD RECOMMEND

    If the boss needs a real, refreshable, sliceable PivotTable, use Excel automation or a maintained template. If the boss only needs the Region × Month totals, build a static cross-tab and say clearly that it is a summary, not a pivot. If there is no Excel available and a native pivot is mandatory, use a commercial library or LibreOffice and validate the output in Excel.

    Ask one clarifying question before choosing: “Do you need to refresh and slice this pivot, or do you just need the Region × Month totals?” That determines whether openpyxl is sufficient for the data-writing part or whether Excel automation is required.

- id: xlsx-summary-02
  answer: |
    Assuming this layout: `A1` = "Region", `B1:M1` = month headers, `A2:A13` = region names, body `B2:M13`, totals in column `N` and row `14`.

    **Body — top-left cell `B2` (copy right and down):**

    ```
    =SUMIFS(Data!$F:$F,Data!$C:$C,$A2,Data!$B:$B,B$1)
    ```

    The two mixed references are the whole trick:

    - `$A2` — column locked, row free. Dragging right keeps the region on column A; dragging down advances the region.
    - `B$1` — row locked, column free. Dragging down keeps the month on row 1; dragging right advances the month.

    Both criteria ranges are full-column and absolute, so every copy stays aligned. SUMIFS requires equal-size ranges; full columns guarantee that.

    **Row total — `N2`, copied down:**

    ```
    =SUMIFS(Data!$F:$F,Data!$C:$C,$A2)
    ```

    **Column total — `B14`, copied across:**

    ```
    =SUMIFS(Data!$F:$F,Data!$B:$B,B$1)
    ```

    **Grand total — `N14`:**

    ```
    =SUM(Data!$F:$F)
    ```

    or, tying it to the grid itself (better self-check):

    ```
    =SUM(B2:M13)
    ```

    All four must agree: `SUM(N2:N13) = SUM(B14:M14) = SUM(Data!F:F)`. If they do not, a region or month label fails to match.

    **Traps that break the copy-down/copy-across**

    - Month type mismatch. If `Data!B:B` holds real dates, the `B1:M1` headers must also be real dates (formatted `mmm`). If the headers are text "Jan" and the data holds "Jan 2026", exact matching fails — use `B$1&"*"` as the criterion, or normalise both sides with `TEXT(...,"mmm")` / a helper column.
    - Trailing spaces. Tab-exported region names often carry one. SUMIFS does exact text match; clean the source or wrap criteria in `TRIM`.
    - Wildcards. A region literally named `A*` or containing `?` will match more than intended.
    - Anchor drift. Using `A2` instead of `$A2`, or `B1` instead of `B$1`, produces a grid that looks right on the first row and wrong everywhere else.
    - Blank amounts are fine (SUMIFS ignores them), but text in `Data!F:F` is silently skipped rather than flagged.

    **Equivalent if SUMIFS is unavailable (SUMPRODUCT, bounded ranges):**

    ```
    =SUMPRODUCT((Data!$C$2:$C$5000=$A2)*(Data!$B$2:$B$5000=B$1)*Data!$F$2:$F$5000)
    ```

    **Structured-reference variant (Excel Table named `tblData`):**

    ```
    =SUMIFS(tblData[Amount],tblData[Region],$A2,tblData[Month],B$1)
    ```

    That version auto-expands and needs no range edits.

    Confidence: high on the SUMIFS structure and mixed-reference anchoring. The one genuinely open variable is the month representation in `Data!B:B`, which I cannot see — if those cells are dates rather than text, the header row must be dates and the formula above is correct unchanged, but a text-header grid will show all zeros.

- id: xlsx-summary-03
  answer: |
    Assumption: raw rows, one per sale, in A2:A13 (rep name, repeated) and B2:B13 (amount). Goal: top 3 reps by summed sales. Everything below is single-cell, non-spilling, no Ctrl+Shift+Enter, and uses only functions that exist before Excel 2007 (SUMIF, LARGE, INDEX, MATCH, COUNTIF, ROWS, RANK).

    LAYOUT AND FORMULAS

    D2:D5 — the distinct rep names, typed once (or pulled from a master list). This is the only input that is not derived.

    E2 — total per rep, fill down E2:E5:
        =SUMIF($A$2:$A$13,$D2,$B$2:$B$13)

    F2 — k-th largest total, fill down F2:F4:
        =LARGE($E$2:$E$5,ROWS($F$2:F2))

    G2 — rep name for that value, fill down G2:G4:
        =INDEX($D$2:$D$5,MATCH(F2,$E$2:$E$5,0))

    That answers the question, but it is wrong the moment two reps have the same total.

    WHAT TIES BREAK

    1. LARGE returns equal numbers for equal totals. With totals 500, 500, 400, the output is 500, 500, 400 — the true third rep never appears.
    2. MATCH(...,0) always returns the first hit. Both slots resolve to the same rep, so you show one rep twice and silently drop the other tied rep.
    3. RANK gives 1, 2, 2, 4 (ties share a rank, the next rank is skipped). Filtering rank <= 3 returns two rows or four-plus, never a stable three.
    4. A PivotTable "Top 3" value filter keeps every item equal to the cutoff, so it can also return more than three rows.
    5. Genuine ambiguity: if three reps tie for first, "top 3" is undefined until you state a rule (row order, alphabetical, etc.).

    TIE-SAFE VERSION (keeps it formula-only)

    Add a helper key in H2:H5 that makes every total unique by a sub-unit amount:
        H2 =SUMIF($A$2:$A$13,$D2,$B$2:$B$13)+ROW()/1000000
    Fill down. The epsilon is far below the smallest real currency unit, so order among exact ties falls deterministically to row order.

    Then:
        F2 =LARGE($H$2:$H$5,ROWS($F$2:F2))
        G2 =INDEX($D$2:$D$5,MATCH(F2,$H$2:$H$5,0))
        H-display =ROUND(F2,0)      (or =SUMIF($A$2:$A$13,G2,$B$2:$B$13))

    If you must keep the true totals untouched, replace the name formula with a skip-used-reps lookup against a text header in G1:
        G2 =INDEX($D$2:$D$5,MATCH(1,INDEX(($E$2:$E$5=F2)*(COUNTIF($G$1:G1,$D$2:$D$5)=0),0),0))
    The expanding COUNTIF excludes reps already listed, so a second tied rep is found on the next row. This may need Ctrl+Shift+Enter on very old builds.

    WHICH PARTS CAN BE FORMULAS

    - Per-rep totals: yes — SUMIF.
    - k-th largest value: yes — LARGE.
    - Rep-name lookup: yes — INDEX/MATCH.
    - Tie-break key or occurrence counter: yes — ROW epsilon or COUNTIF helper.
    - Distinct rep list: this is the weak spot. Without UNIQUE you hardcode the roster, use a PivotTable (not a formula), or write a fragile INDEX/MATCH/COUNTIF extraction array. UNIQUE is exactly the function you cannot use.
    - The whole top-3 block: yes, once D2:D5 exists.

    If the sheet already has one row per rep with a Total column, drop the SUMIF and go straight to LARGE + INDEX/MATCH; the tie behaviour is identical.

- id: xlsx-cleanup-01
  answer: |
    Closed-book answer. Assumptions stated up front: one locale per file unless you detect otherwise; month names are English; "RM" means Malaysian ringgit; dates are calendar dates, not timestamps.

    DETECTION

    Do not trust display formatting. Read the raw stored value, then classify each cell.

    - Excel: `=ISTEXT(A1)`, `=ISNUMBER(A1)`, `=TYPE(A1)` (1 = number, 2 = text). A green triangle means "number stored as text".
    - openpyxl: `cell.data_type` is `'s'` for text, `'n'` for number, `'f'` for formula, `'d'` for a real date. A text date shows `'s'`.
    - pandas/CSV: everything is `str` unless you pass `dtype`; inspect with a regex, not with `dtype`.

    Then pattern-test the trimmed string, e.g. `^\s*[A-Za-z]{0,3}\s*[-+(]?\s*[\d.,\s]+\s*[)-]?\s*$` for money. Classify the column by parse rate: if most cells fail a numeric parse but match the pattern, the column is text-typed. Convert the whole column one way; do not mix per cell.

    AMOUNT CONVERSION

    Normalize, then parse, then validate.

    1. Normalize the string first. Trim leading/trailing whitespace. Then explicitly remove all Unicode whitespace, not just ASCII space. Excel `TRIM` only handles char 32. Excel `CLEAN` only removes control chars 0–31 and does NOT remove NBSP (U+00A0), zero-width space, BOM, or soft hyphen.
    2. Strip the currency token. Remove `RM`, `MYR`, `USD`, `$`, `€`, `£`, `¥`, `Rp`, `S$` and similar. Prefer an explicit allow-list of symbols and 3-letter codes over a blanket "remove all letters", so you do not eat a stray minus or unit.
    3. Remove grouping separators only with known locale. `1,200.50` and `1.200,50` are the same number. Decide by the position of the last separator, or by declared locale. Do not blindly delete commas.
    4. Handle signs: leading `-`, trailing `-` (`45.00-`), parentheses `(45.00)`, `CR`/`DR` suffixes, and the Unicode minus U+2212 and en dash U+2013 (both look like a hyphen).
    5. Parse to `Decimal`, not `float`, for money. Quantize to 2 dp with `ROUND_HALF_UP`. Python's `round()` is banker's rounding and will surprise you.
    6. In Excel: `=VALUE(SUBSTITUTE(TRIM(A1),UNICHAR(160),""))` after removing the currency token; or do it in Power Query with an explicit culture.

    Example failures to expect:
    - `"1200.50"` → 1200.50, fine.
    - `" 75 "` → 75, but if that space is NBSP, `TRIM`/`CLEAN` leaves it and `VALUE` errors.
    - `"RM 45.00"` → strip `RM`, strip space, get 45.00.

    DATE CONVERSION

    Never let a locale guess. Use an ordered list of explicit formats and take the first that parses.

    1. ISO 8601 first: `%Y-%m-%d` (`2026-10-01`). Also accept `%Y-%m-%dT%H:%M:%S%z` if times appear.
    2. Then day-month-name: `%d %b %Y` (`01 Oct 2026`), case-insensitive. Add variants: `%d %B %Y`, `Sept` for September, trailing period (`Oct.`), ordinal suffix (`1st Oct 2026`), and no-space or comma forms.
    3. Ambiguous numeric forms (`01/10/2026`) are the trap. DD/MM vs MM/DD is unresolvable when both parts are ≤12. Detect by scanning the column: if any day field is >12, infer the order and apply it to the whole column. If not, do not guess — require a configured order or flag the column.
    4. Excel `DATEVALUE` is locale-dependent and will silently misread. Prefer Power Query `Date.FromText` with an explicit culture, or `datetime.strptime` with a format list.
    5. Watch for dates that are actually numbers: an Excel serial like `46000` is a date only via formatting. Check `ISNUMBER` and the format. Also watch the 1900 leap-year bug and the Mac 1904 date system.
    6. Two-digit years: define the century window explicitly (e.g. 00–29 → 2000s).
    7. Validate: parsed date in a plausible range, and round-trip format back to a canonical ISO string.

    INVISIBLE AND LOOK-ALIKE CHARACTERS TO WATCH

    Whitespace:
    - U+00A0 no-break space (NBSP) — very common from HTML and web exports.
    - U+202F narrow no-break space — used as a thousands separator in some locales.
    - U+2009 thin space, U+2007 figure space.
    - U+3000 ideographic space.
    - U+0009 tab, U+000D CR, U+000A LF (quoted multi-line CSV fields).

    Zero-width and formatting:
    - U+FEFF BOM / zero-width no-break space — often at the start of the first CSV field; breaks header matching and number parsing.
    - U+200B zero-width space.
    - U+200C ZWNJ, U+200D ZWJ.
    - U+2060 word joiner.
    - U+00AD soft hyphen.
    - U+200E LRM, U+200F RLM, and U+202A–U+202E bidi embedding/override marks — appear in Arabic/Hebrew exports and can reorder digits visually.
    - U+0000–U+001F and U+007F control characters.

    Look-alike punctuation and digits:
    - U+2212 minus sign, U+2013 en dash, U+2014 em dash used as a minus.
    - U+2011 non-breaking hyphen.
    - Full-width digits U+FF10–U+FF19 and full-width period U+FF0E.
    - Apostrophe prefix `'1200.50` — Excel's text marker; it is not part of the value in the cell but is in the CSV.

    Normalization recipe: map all Unicode whitespace (category Zs plus tab/CR/LF) to ASCII space, delete zero-width and control characters, then apply NFKC. NFKC folds full-width forms and NBSP to ASCII and is safe for numeric/date fields. Use NFC elsewhere if you need to preserve distinctions.

    VALIDATION

    - Keep the raw column next to the parsed column.
    - Count unparseable rows and log samples; fail loudly above a threshold.
    - Reconcile: sum of parsed amounts equals the source total; row count matches.
    - Round-trip: format each parsed value back and compare to the normalized input.

    Where I am uncertain from memory alone: the exact set of characters a given tool trims (Excel `TRIM`, Power Query `Text.Trim`, pandas `str.strip`) varies by version and is easy to get wrong. Verify the trim behavior on your actual toolchain rather than assuming it handles NBSP.

- id: xlsx-cleanup-02
  answer: |
    Short answer: normalise first, then de-duplicate, and never hide rows.

    Assumptions: Excel or Google Sheets; "original untouched" means you work on a copy and keep a removal log; "duplicate" means the same entity under a defined key, not merely similar text.

    ORDER OF OPERATIONS

    1. Preserve the source. Copy the sheet to a new tab or file. Lock or mark the original read-only. Never clean in place.

    2. Fix structure before values. Unmerge cells, remove blank rows, promote the header, fix column types, and unpivot if the layout is cross-tab.

    3. Profile the data. Count rows, list distinct values per key column, and look at the dirty patterns: trailing spaces, mixed case, phone formats, date formats, stray punctuation, Unicode variants.

    4. Normalise the key columns. Trim whitespace, case-fold, strip punctuation, standardise phone and email, parse dates to one format, apply Unicode NFKC, and expand known abbreviations. Do this before any comparison.

    5. Define identity and survivorship. Decide which columns form the duplicate key. Decide, in writing, which record wins when keys collide.

    6. Flag, do not delete yet. Add helper columns (for example a COUNTIFS duplicate flag plus a rank per key) so every decision is visible and reversible.

    7. Remove duplicates on the clean copy, keeping the survivor your rule selected.

    8. Validate. Compare before/after counts, sample the removed rows, and write them to an archive tab with a reason column.

    Why normalise before de-duplicating: de-duplication compares values. If you de-dupe the raw text first, "Bob " and "bob" look distinct and both survive; normalisation then turns them into an identical pair, so you must de-dupe again. Normalising first means the comparison runs on the canonical form. Doing it the other way guarantees rework.

    WHICH ROWS SURVIVE

    Not "the first row." First-row survival is arbitrary and usually keeps the worst record. Use an explicit, documented rule, for example: most complete record (fewest blanks), then most recent timestamp, then highest source priority, then highest ID, with original row order as the final tie-break. The loser is archived, not discarded silently.

    Two cautions. Exact duplicates and near-duplicates are different problems. Fuzzy matches (typos, transposed digits, John Smith twice) need a scored threshold or human review; auto-deleting them destroys legitimate distinct records. And if survivorship is per field, merging best values from several rows is a merge, not a de-dupe, so label it as one.

    WHY HIDING IS NOT ACCEPTABLE

    - Hiding is not removing. Hidden rows still feed SUM, COUNTA, pivot caches, exports, and most downstream tools, so totals and joins stay wrong.
    - The state is fragile. Sort, filter, copy-paste, or another user unhides them, and the "clean" file silently becomes dirty again.
    - It is unauditable. Nothing records what was hidden or why, and there is no survivor rule, so the visible row may be the incomplete one.
    - It masks the cause. The dirty data remains, so the next import reintroduces the same duplicates.
    - It fails the requirement. The correct pattern is original preserved plus a cleaned copy plus a removal log; hiding alters a view without preserving provenance.

    BOTTOM LINE: copy, normalise, define the key and survivor rule, flag, remove, log the removed rows, validate counts — and treat hiding as a cosmetic non-fix.

- id: xlsx-cleanup-03
  answer: |
    ## Why `str.title()` alone is wrong

    **For your three name samples, it appears to work — that is the trap.** `"ben lim".title()`, `"BEN LIM".title()`, `"Daniel Wong".title()` all give `Ben Lim` / `Ben Lim` / `Daniel Wong`. Pass rate 3/3. Then it breaks on the fourth row.

    `str.title()` uppercases the first cased character after **every non-alpha character** and lowercases everything else. That is a typographic rule, not a name rule. Three separate failure classes:

    **1. It destroys information you cannot reconstruct.**
    - `"McDonald".title()` → `Mcdonald`
    - `"DeSantis"` → `Desantis`
    - `"LeBron"` → `Lebron`
    - `"IBM"` → `Ibm`, `"iPhone"` → `Iphone`

    Once the internal caps are gone, no downstream step knows they existed. This is lossy, not normalising.

    **2. Its word boundaries are wrong for names.**
    - `"van der Berg".title()` → `Van Der Berg` (particles should stay lowercase: `van der Berg`)
    - `"3rd".title()` → `3Rd` (digit is treated as a word break)
    - `"istanbul".title()` → `Istanbul`, not `İstanbul` — it is not locale-aware
    - `"ß".title()` → `Ss`; `"ǆ"` and other digraphs also misbehave

    It does get apostrophes and hyphens right (`O'Neill`, `Anne-Marie`) — but only because those happen to be non-alpha. That is accidental, not designed.

    **3. It does not strip or collapse, so it does not normalise at all.**
    - `" Ops".title()` → `" Ops"` — leading space survives
    - `"hr ".title()` → `"Hr "` — trailing space survives, wrong case
    - `"ben  lim".title()` → `"Ben  Lim"` — double space survives
    - NBSP, zero-width, and trailing newlines all survive

    Your grouping key still splits on whitespace after `.title()`. You fixed the case and kept the bug.

    **4. For departments, title case is simply the wrong answer.**
    `"hr".title()` → `"Hr"`. The correct display is `HR`, not `Hr`. `"ops"` → `"Ops"` only by luck. Departments are **coded entities**, not prose: `HR`, `IT`, `QA`, `R&D`, `BI`, `PR`, `ML`. Title-casing an acronym always loses. And it cannot unify `ops` with `operations`, or `hr` with `human resources` / `people` — which is the actual matching problem.

    Also: `.capitalize()` is not the fix either. `"ben lim".capitalize()` → `"Ben lim"`. Under-normalises instead of over-normalising.

    ## How to normalise

    Two rules, applied everywhere:

    **Never store one string.** Carry a **match key** (mechanical, lossy, only for comparison) and a **display value** (human-facing, authoritative, never used as a join key). They are different data.

    ### Departments — alias table, not a transform

    Case-folding cannot produce a canonical code. Use an explicit lookup, and refuse to guess.

    ```python
    import re, unicodedata

    DEPT = {
        "ops":            ("OPS", "Operations"),
        "operations":     ("OPS", "Operations"),
        "operational":    ("OPS", "Operations"),
        "hr":             ("HR",  "Human Resources"),
        "human resources":("HR",  "Human Resources"),
        "people":         ("HR",  "Human Resources"),
        "people ops":     ("HR",  "Human Resources"),
    }

    def dept_key(raw: str) -> str:
        s = unicodedata.normalize("NFKC", raw).casefold()
        s = re.sub(r"[^a-z0-9 ]+", " ", s)
        return " ".join(s.split())

    def dept(raw: str):
        # returns (code, display) or None -> route to review queue
        return DEPT.get(dept_key(raw))
    ```

    `"ops"`, `" Ops"`, `"OPERATIONS"` → `("OPS", "Operations")`. `"hr"`, `"HR"`, `" Hr"` → `("HR", "Human Resources")`. Unknown value returns `None` and gets flagged — **do not** silently `.title()` the residue, or you re-introduce `Hr` and `Ops.` as fake canonicals.

    ### Names — display preserved, key derived

    ```python
    def name_key(raw: str) -> str:
        s = unicodedata.normalize("NFKD", raw)
        s = "".join(c for c in s if not unicodedata.combining(c))  # strip accents
        s = s.casefold()                                            # ß->ss, not lower()
        s = re.sub(r"[.'’`]", "", s)                                # O'Neill == ONeill
        s = re.sub(r"[^a-z0-9]+", " ", s)
        return " ".join(s.split())
    ```

    - `"ben lim"` / `"BEN LIM"` / `" Ben  Lim "` → `"ben lim"`
    - `"Daniel Wong"` / `"DANIEL WONG"` → `"daniel wong"`
    - `"O’Neill"` / `"o'neill"` → `"oneill"`

    **`.casefold()` not `.lower()`** — it handles `ß`→`ss`, and full Unicode folding. Do not `.title()` the key.

    For the display value, two safe policies:

    - **Preferred:** keep the display exactly as captured, human-confirmed. Do not transform it.
    - **Only when the source is demonstrably all-caps** (an HR export), rescale it with an explicit tokeniser, not `.title()`:

    ```python
    PARTICLES = {"de","del","der","den","di","da","dos","du","la","le",
                 "van","von","bin","binti","al","el"}

    def rescale_caps(raw: str) -> str:
        s = " ".join(unicodedata.normalize("NFKC", raw).split())
        if not s or not s.isupper():
            return s                      # mixed case: leave alone, do not guess
        out = []
        for i, tok in enumerate(s.split(" ")):
            low = tok.casefold()
            if i > 0 and low.strip(".,") in PARTICLES:
                out.append(low)
            else:
                out.append(re.sub(r"([-'’])([a-z])", lambda m: m.group(1)+m.group(2).upper(), low))
        return " ".join(out)
    ```

    Gate it on `s.isupper()`. `"Daniel Wong"` passes through untouched; `"BEN LIM"` becomes `"Ben Lim"`; `"VAN DER BERG"` becomes `"van der Berg"`. `Mc`/`Mac`/`Le`/`De`-prefixed surnames cannot be inferred from an all-caps source — keep a known-exceptions list or let a human fix the display and store that.

    ## Rules that make it hold

    1. `key(a) == key(b)` for every case/whitespace variant of the same entity. Test it as a property.
    2. Display is a separate, stored, human-correctable field. Never derive it from the key.
    3. Acronyms live in the alias table with their intended casing (`HR`, `QA`, `R&D`).
    4. Unknown department → `None` + review queue. No fallback transform.
    5. Unicode-normalise (NFKC) **before** stripping, and normalise whitespace last.
    6. Name equality is not identity. `"Dan Wong"` vs `"Daniel Wong"` needs fuzzy blocking (Metaphone/Soundex, edit distance) plus human merge — not a casing fix.

    ## Assumptions

    - Python / `str.title()` semantics. The principle transfers to JS `toTitleCase` helpers and SQL `INITCAP`, which fail on the same inputs.
    - ASCII output for match keys. If you must preserve diacritics for grouping, drop the combining-mark strip step.
    - One department per record. If a person holds several, the alias table must map to a code set, not a scalar.

    **Bottom line:** `.title()` passes your three samples by coincidence, then silently mangles `McDonald`, `HR`, and leading whitespace. Normalise by deriving a casefolded, punctuation-stripped match key, and resolve departments through an explicit alias table that preserves acronym casing — never by title-casing anything you intend to trust.

- id: xlsx-cleanup-04
  answer: |
    Assumptions: the IDs and phone numbers are identifiers, not quantities; pandas reads with the C engine and writes xlsx through openpyxl; "phone" means numbers that may carry leading zeros, `+`, spaces, dashes, or parentheses.

    WHAT GETS DAMAGED

    Damage happens at read time, before xlsx is even involved.

    1. Leading zeros die at `read_csv`.
       pandas infers `int64` for an all-digit ID column: `"007"` becomes `7`, `"010"` becomes `10`. Writing to Excel then stores the number 7, and no Excel number format can recover the real identifier (a `"000"` format is a display band-aid and breaks joins). If the column is mixed (`"007"`, `"A12"`), the parser can produce an object column holding some ints and some strings — 007 is corrupted, A12 is not. With chunked parsing (`low_memory=True`, the default) inference can even differ per chunk, so corruption is inconsistent row to row.

    2. Phone numbers suffer the same coercion, plus an Excel limit.
       Purely numeric phones become `int64`, or `float64` (`5551234567.0`) as soon as one value is missing. Values with `+`, spaces, dashes, or parentheses stay text only if every value has a non-digit. Separately, Excel stores numbers as IEEE-754 doubles with 15 significant digits: a 10–13 digit phone opens as `5.55123E+09`, and anything past 15 digits is silently rounded to zeros. So even an uncorrupted int64 phone is damaged when the file is opened.

    3. The BOM corrupts the first header.
       With the default `encoding="utf-8"`, the first column name keeps U+FEFF: `"\ufeffid"`. Then `df["id"]` raises `KeyError`, and `dtype={"id": str}` silently fails to apply to the real column — so the ID column keeps numeric inference and loses its zeros. Excel adds this BOM when it exports CSV.

    4. NA tokens disappear.
       With `keep_default_na=True` (default), `""`, `"NA"`, `"N/A"`, `"NULL"`, `"None"`, `"nan"` become `NaN`. An employee whose ID or phone is literally `"NA"` vanishes, and an empty phone becomes indistinguishable from a missing one on write (blank cell).

    5. Mixed dtypes survive into the sheet.
       An object column containing both ints and strings writes numbers and text into the same column: green "number stored as text" triangles, inconsistent alignment, and sort/filter treating `7` and `"007"` as different keys.

    6. The round trip re-damages.
       `read_excel` also infers. Re-reading the output without `dtype=str` re-coerces any cell that was written as numeric.

    HOW TO PREVENT IT

    Read everything as text, strip the BOM, and disable NA guessing:

    ```python
    df = pd.read_csv(
        "employees.csv",
        encoding="utf-8-sig",    # strips BOM; safe when absent
        dtype=str,               # no numeric inference
        keep_default_na=False,   # keep "", "NA", "NULL" as literal text
        na_values=[],            # belt and braces
    )
    ```

    Then convert only the genuinely numeric columns:

    ```python
    for c in ("salary", "age"):
        df[c] = pd.to_numeric(df[c], errors="coerce")
    ```

    A per-column `dtype={"id": str, "phone": str}` also works, but you must still add `keep_default_na=False` — `dtype=str` alone does not stop `""` and `"NA"` becoming `NaN`. If you cannot fix the BOM at read time:

    ```python
    df.columns = df.columns.str.replace("\ufeff", "", regex=False)
    ```

    Write with the identifier cells kept as Python `str`, so openpyxl stores them as text, not numbers:

    ```python
    with pd.ExcelWriter("employees.xlsx", engine="openpyxl") as xl:
        df.to_excel(xl, index=False, sheet_name="employees")
        ws = xl.sheets["employees"]
        for row in ws.iter_rows(min_row=2):
            row[0].number_format = "@"   # ID column, Text
            row[2].number_format = "@"   # phone column, Text
    ```

    The `"@"` format is the explicit guard that keeps Excel from reinterpreting the cell if someone edits or pastes into it; the essential part is that the value is a string. Read the result back the same way:

    ```python
    pd.read_excel("employees.xlsx", dtype=str, keep_default_na=False)
    ```

    If the target is CSV rather than xlsx, write with `encoding="utf-8-sig"` so Excel opens it without mojibake, and consider prefixing or quoting values that start with `=`, `+`, `-`, `@` to avoid spreadsheet formula injection.

    BOTTOM LINE: the corruption is dtype inference plus BOM handling at read time, not the xlsx writer. `encoding="utf-8-sig"` + `dtype=str` + `keep_default_na=False` preserves IDs and phones; keeping those values as text in the sheet avoids Excel's 15-digit precision and scientific-notation damage.

- id: xlsx-formatting-01
  answer: |
    ```python
    from openpyxl import Workbook
    from openpyxl.styles import Font, PatternFill, Alignment, Border, Side
    from openpyxl.utils import get_column_letter
    from datetime import date
    from decimal import Decimal

    wb = Workbook()
    ws = wb.active
    ws.title = "Ledger"

    headers = ["Date", "Description", "Account", "Amount", "Tax %"]
    ws.append(headers)

    rows = [
        (date(2026, 10, 1), "Invoice 1042", "Sales", Decimal("1250.00"), Decimal("0.20")),
        (date(2026, 10, 3), "Hosting",      "Ops",   Decimal("-89.99"),  Decimal("0.00")),
    ]
    for d, desc, acct, amt, pct in rows:
        # money -> float, percent -> FRACTION (0.20 means 20%)
        ws.append([d, desc, acct, float(amt), float(pct)])

    # --- header style ---
    head_fill = PatternFill("solid", fgColor="1F4E78")
    edge = Side(style="thin", color="D9D9D9")
    for c in ws[1]:
        c.font = Font(bold=True, color="FFFFFF", size=11)
        c.fill = head_fill
        c.alignment = Alignment(horizontal="center", vertical="center")
        c.border = Border(bottom=edge)
    ws.row_dimensions[1].height = 20

    # --- frozen header + filter buttons ---
    ws.freeze_panes = "A2"                        # "B2" also freezes column A
    ws.auto_filter.ref = f"A1:E{ws.max_row}"

    # --- number formats ---
    MONEY = '#,##0.00;[Red]-#,##0.00'
    MONEY_USD = '"$"#,##0.00;[Red]-"$"#,##0.00'
    for r in ws.iter_rows(min_row=2, max_row=ws.max_row):
        r[0].number_format = "yyyy-mm-dd"
        r[3].number_format = MONEY_USD
        r[4].number_format = "0.00%"

    # --- column widths (measured by hand; see note) ---
    widths = {"A": 12, "B": 34, "C": 12, "D": 16, "E": 9}
    for col, w in widths.items():
        ws.column_dimensions[col].width = w

    wb.save("ledger.xlsx")
    ```

    Character-count heuristic if you want to approximate auto-fit:

    ```python
    for col in ws.columns:
        letter = get_column_letter(col[0].column)
        longest = max(len(str(c.value)) for c in col if c.value is not None)
        ws.column_dimensions[letter].width = min(max(longest + 2, 8), 60)
    ```

    How percent values must be stored

    Store the ratio, not the percentage number. 20% is written as `0.20` (float) or `Decimal("0.20")`, and the cell gets `number_format = "0.00%"`. Excel multiplies by 100 only for display.

    - Correct: value `0.2`, format `0.00%` → shows `20.00%`, works in formulas (`D2*E2`).
    - Wrong: value `20`, format `0.00%` → shows `2000.00%`.
    - Wrong: value `"20%"` as a string → becomes text, breaks SUM and sorting.
    - Only if the cell must literally hold 20 as a number, use a suffix-only format `0.00"%"`. That is display decoration, not a real percent, and formulas will treat it as 20, not 0.2.

    Money and dates

    - Money: write `float` or `Decimal` (openpyxl converts), never a string like `"$1,250.00"`. Use `#,##0.00` or an accounting format. Floats carry binary noise, so round or keep Decimal until write time.
    - Dates: write `datetime.date` / `datetime.datetime` objects, not text. `yyyy-mm-dd` or `dd/mm/yyyy`. Timezone-aware datetimes raise an error — strip tzinfo first.

    What openpyxl cannot do automatically here

    1. Auto-fit column widths. There is no font-metrics engine. You set widths or use the character-count heuristic above, which ignores bold, font size, wrapping, and actual glyph widths.
    2. Compute formulas. openpyxl has no calculation engine. `=SUM(D2:D99)` is stored as a string with no cached result; the cell reads `None` until Excel or LibreOffice opens and saves it. Read back with `load_workbook(..., data_only=True)` only after that recalculation, or compute totals in Python.
    3. Infer number formats. It will not detect that a number is a date, currency, or percent. Every `number_format` is your job.
    4. Paint a real "Format as Table". `openpyxl.worksheet.table.Table` with `TableStyleInfo` writes table XML, but the banding and borders are applied by Excel on open — openpyxl does not style the cells. Also do not put a `Table` over the same range as `ws.auto_filter`; overlapping filters make Excel report a corrupted file. Pick one.
    5. Produce totals rows, subtotals, pivot tables, or sparklines. None are generated for you.
    6. Render or validate the result. No preview, no check that a format string looks right, no print-area/scale setup.
    7. Apply conditional formatting presets, number-format guessing on read, or data validation for you. All are manual API calls.

    Practical guardrails

    - `ws.freeze_panes = "A2"` freezes row 1 only; `"B2"` freezes row 1 and column A.
    - Set `auto_filter.ref` to an explicit range (`A1:E500`), not `ws.dimensions`, so the filter stays stable when rows are appended.
    - Save once, reopen in Excel/LibreOffice, save again if downstream tools need cached formula values.
    - Do not mix `Table` and `auto_filter` on one sheet.

- id: xlsx-formatting-02
  answer: |
    Short answer: don't paint the cells. Put a conditional-formatting **rule** on the row range that tests the flag column, and apply it to a range large enough (or an Excel Table) to cover tomorrow's rows. The rule re-evaluates on every recalculation; a fill does not.

    Assumptions: Excel or Google Sheets, header in row 1, data in `A2:H…`, flag lives in column `E`. Adjust letters to match your sheet.

    ## The mechanism

    1. Select the whole body range, e.g. `A2:H2000` — not just today's last row. Oversize it, or convert the range to an Excel Table so the formatting range grows with the data.
    2. Home → Conditional Formatting → New Rule → *Use a formula*.
    3. Formula: `=$E2="Investigate"`
    4. Format: red fill (and red font if you want contrast). Apply.

    The reference is deliberately mixed:
    - `$E` — absolute column, so the rule keeps looking at the flag column when it evaluates cells in columns A through H.
    - `2` — relative row, so row 2 tests `E2`, row 3 tests `E3`, and so on. The row number in the formula must equal the **first row of the applied range**; that off-by-one is the single most common failure (range starts at row 2, formula says `$E1`, everything shifts).
    - Comparison is case-insensitive by default. If trailing spaces are a risk, use `=TRIM($E2)="Investigate"`. If case matters, `=EXACT($E2,"Investigate")`.

    ## Making it survive the paste

    - **Paste values into the data columns only.** `Paste Special → Values` leaves destination formatting and the CF rule intact. A full paste carries the source's fills and can visually override or duplicate your rule.
    - **Keep the flag column consistent.** Best case, `E` is itself a formula (`=IF(ABS(variance)>threshold,"Investigate","")`) or a data-validated dropdown. Then the flag is the single source of truth and the CF just reads it. Don't re-derive the condition inside the CF rule *and* in a helper column — two predicates drift apart.
    - **Don't insert rows above the range** unless you're in a Table; a plain CF range does not auto-extend, and rows pushed outside it silently stop turning red.
    - **Check rule priority.** If another rule (banding, "no data", manual fill) has higher priority and *Stop If True*, your rule never fires. Move it up, or clear conflicting fills.
    - **Protect the sheet** (allow data entry, disallow formatting) so a well-meaning paste doesn't nuke the rule.
    - Test it: change one flag to `Investigate`, confirm the row goes red, change it back, confirm it clears. That proves it's a rule, not paint.

    ## Why filling cells red in Python is wrong

    1. **A fill is a style record, not a predicate.** `openpyxl`'s `cell.fill` writes "this cell is red." Nothing evaluates it. Excel's calculation engine recalculates formulas and conditional formats; it never recalculates fills. Tomorrow's data arrives, and the red is a fossil of yesterday's judgment.
    2. **It is a snapshot of a decision.** The moment a flag flips or a number changes, the file is lying, and it will keep lying until someone re-runs the script.
    3. **Pasting makes it worse, not better.** Paste values → old reds stay on the wrong rows. Paste all → source fills land on top. Either way formatting drifts away from meaning.
    4. **It turns a model into a report.** The workbook now depends on a generation step; anyone who edits it by hand, or opens last week's copy, has stale colour with no way to tell.
    5. **It's unmaintainable and unauditable.** The logic is buried in code, scattered across N cells, invisible in the UI. A CF rule is one visible object you can inspect, edit, and copy to another sheet.
    6. **It fights the tool.** Sorting, filtering, and row insertion move cells; relative-reference CF follows the data, static fills do not reliably.

    The honest framing: **Python isn't wrong — painting is wrong.** If the workbook must be generated in Python, write the *rule*:

    ```python
    from openpyxl.formatting.rule import FormulaRule
    ws.conditional_formatting.add(
        "A2:H2000",
        FormulaRule(formula=['$E2="Investigate"'],
                    fill=PatternFill("solid", start_color="FFFF0000", end_color="FFFF0000"),
                    stopIfTrue=True),
    )
    ```

    That ships the logic into the file, and tomorrow's paste is evaluated correctly with no script run at all.

    Decided: flag column is column E, data is A:H, and "Investigate" is an exact string. If your flag is a boolean or lives in a different column, only the formula changes — the principle does not.

- id: xlsx-preserve-01
  answer: |
    Assume the file is .xlsx or .xlsm, it is the only copy, and openpyxl is version 3.x. Those assumptions matter, because the risk changes with file type.

    WHAT OPENPYXL DROPS OR ALTERS

    Macros (.xlsm)
    - Default load drops vbaProject.bin. Save as .xlsx and every macro is gone.
    - keep_vba=True carries the binary blob forward. It does not carry the digital signature.
    - It does not carry ActiveX controls or form controls.
    - openpyxl cannot read VBA source. It only moves the blob.

    Charts
    - openpyxl rebuilds charts from its own model. The round-trip is lossy.
    - New chart types (chartex: waterfall, funnel, histogram, Pareto, box-and-whisker, map) are dropped entirely.
    - Combo charts, secondary axes, custom data labels, trendlines, and detailed formatting often degrade.
    - Series value caches are dropped. Charts can look empty until Excel recalculates.
    - Chartsheets can be altered or lost.
    - Pivot charts die with their pivot tables.

    Dropdowns (data validation)
    - Same-sheet list validations usually survive.
    - Validations that point at another sheet live in extLst (x14). openpyxl drops extLst. Those dropdowns vanish.
    - Whole-column ranges (A:A) have had range bugs. Check the range after save.

    Highlighting
    - Static fills, fonts, borders, and number formats usually survive.
    - Theme-color references can shift or convert. Colors may render differently.
    - Simple conditional formatting survives.
    - x14 conditional formatting (data bars with borders, some icon sets and color scales) is dropped.

    Named ranges
    - Most workbook-level defined names survive.
    - Sheet-local names have historically been lost or re-scoped.
    - Names built on tables or structured references can break.
    - Print areas and print titles usually survive.

    Other parts lost or altered
    - Pivot tables and pivot caches: lost.
    - Slicers and timelines: lost.
    - Power Query, connections, and the data model: lost.
    - Sparklines: lost.
    - Shapes, text boxes, buttons, SmartArt, and drawings other than charts/images: lost.
    - Form controls and ActiveX: lost.
    - Shared-workbook mode: dropped.
    - External links: dropped unless keep_links=True.
    - Rich text inside a cell: flattened unless rich_text=True.
    - Array formulas and dynamic-array spill formulas: not supported, can be rewritten or broken.
    - Tables (ListObjects): styles, calculated columns, and totals rows can break.
    - Cell comments: kept, but size and author can change.
    - Custom XML parts and some document properties: dropped.

    The two silent killers
    - data_only=True replaces every formula with its cached value. Saving then destroys all formulas. Never use it for an edit-and-save.
    - openpyxl rewrites the whole package on save. Editing one sheet does not protect the other sheets.

    HOW TO PROTECT THE WORK

    1. Never save over the original. Copy it first, keep a timestamped backup, and record a hash. Save output to a new name.

    2. Choose the right writer.
    - If the file has macros, pivot tables, slicers, Power Query, or shapes, do not use openpyxl save at all.
    - Use Excel itself through xlwings or win32com. Open, append the row, save. Excel preserves its own parts.
    - On macOS, use osascript or xlwings with Excel installed.
    - Alternative: edit the OOXML package in place. Unzip, append the row to sheetN.xml, update dimension and shared strings, rezip. All other parts stay byte-identical.
    - LibreOffice headless is not a safe fallback. It also alters Excel-specific parts.

    3. If you must use openpyxl, load defensively.
    - keep_vba=True for .xlsm, and keep the .xlsm extension on save.
    - keep_links=True.
    - rich_text=True.
    - Never data_only=True.
    - Append the row. Do not touch existing cells or styles.

    4. Verify before you trust the result.
    - Compare part lists: zipfile.namelist() before and after. Any missing part is a red flag.
    - Diff the key parts: xl/charts/, xl/drawings/, xl/pivotCache/, xl/vbaProject.bin, and extLst in each sheet.
    - Open the copy in Excel and check: charts render, dropdowns open, Name Manager lists all names, conditional formatting shows, macros run, pivots refresh.

    5. Design around it.
    - Keep the dashboard or report workbook separate and read-only. Put new data in a plain data workbook. Link or refresh.
    - Or keep an append-only data sheet that no chart depends on structurally.

    I am confident about the macro, formula, pivot, extLst, and chartex points. Specific chart-formatting and theme-color degradation varies by openpyxl version, so test on a copy rather than trusting a version claim.

- id: xlsx-preserve-02
  answer: |
    Assumptions: openpyxl 3.x, sheet has a Total row on row 10, and `insert_rows(10)` shifts rows 10 and below down by one (Total lands on row 11). The cell values themselves do move — openpyxl re-keys those cells. Almost everything else does not.

    The short version: openpyxl has no dependency graph. The docs say plainly that it "does not manage dependencies, such as formulae, tables, charts, etc." So `insert_rows` moves cell payloads and bumps `max_row`, and that is roughly all.

    What is NOT updated automatically, and you must fix by hand:

    1. Formulas — everywhere.
       - In this sheet: every relative/absolute reference at or below row 10 keeps its old coordinates. The Total formula still says `=SUM(B2:B9)` and now excludes the new row. Deleting a 9-row range instead of 10, off-by-one errors, etc.
       - In other sheets: any `Sheet1!B10` reference is stale.
       - Range operators (`B2:B9`), `SUMIF` criteria ranges, `INDEX/MATCH` lookups, `VLOOKUP` table arrays, `OFFSET`.
       - Cross-sheet 3D references and external workbook links.
       - Because Excel recalculates on open, you usually get silently wrong numbers, not a `#REF!`. That is the dangerous part.

    2. Merged cell ranges (`ws.merged_cells`). The `CellRange` coordinates do not move. Insert above or inside a merge and you get a merge sitting on the wrong cells, or a `MergedCell` desync. Rebuild the merges with shifted coordinates.

    3. Data validation (`ws.data_validations`, `sqref`). Dropdowns and validation rules stay on the old rows. Copy them to the new row, or re-target the `sqref`.

    4. Conditional formatting (`ws.conditional_formatting`). Both the range keys and rule priorities stay put. Re-register the shifted ranges.

    5. Defined names / named ranges (`wb.defined_names`, workbook scope). Their `attr_text` is not rewritten.

    6. Tables / ListObjects. The table `ref` is not expanded, the autofilter inside the table is not moved, calculated columns and totals-row settings are not applied to the new row. Inconsistent table refs are a common cause of Excel's "repair" prompt.

    7. AutoFilter (`ws.auto_filter.ref`) and the sort state.

    8. Charts. Series `Reference` objects, category ranges, axis ranges, and chart anchors are all fixed. Charts will plot the old range or drop the new row.

    9. Images, shapes, text boxes. Anchors (`OneCellAnchor`/`TwoCellAnchor`) do not follow the shift.

    10. Comments / notes and hyperlinks. These are cell-adjacent objects whose `ref` or anchor is stored separately in some paths. Treat them as suspect and verify; do not assume they moved with the cell.

    11. Row dimensions (`ws.row_dimensions`). Heights, `hidden`, `outlineLevel`, `collapsed` are keyed by row number and are not shifted. The new row gets default height and no hidden/group state, and the Total row's height now applies to the old index.

    12. Print settings: `ws.print_area`, `ws.print_title_rows` / `print_title_cols`, manual page breaks, and anything that encodes row numbers.

    13. Freeze panes / split panes and sheet view state (usually irrelevant for an insert at row 10, but still not maintained).

    14. Formatting of the new row. Styles are not copied down. You must copy font, fill, border (especially the top border of the Total row), number formats (currency, date), alignment, and any row height from the row above — typically via `copy(cell._style)` or `cell._style = above._style`.

    15. Content of the new row. It is blank. You must populate the date, description, amount, and any ID/formula.

    16. Pivot tables and pivot caches. Source ranges are not expanded; refresh and re-point the source yourself.

    17. Sparklines, array formulas / dynamic-array spill ranges, shared formulas, What-If data tables.

    18. Protection settings — "allow edit ranges" store `sqref` and are not shifted.

    19. Cached formula values. openpyxl writes formulas without results, so any consumer reading with `data_only=True` sees `None` until Excel saves the file.

    20. Anything that consumed the old row number implicitly: printed report layouts, named print areas, dashboard ranges, and validation lists that point at a fixed block.

    Practical consequences and the sane fixes:

    - Do not rely on `insert_rows` for a workbook with formulas, tables, or charts. Either rewrite the shifted references yourself, or drive real Excel via COM/pywin32 (or LibreOffice UNO) so the application performs the insert with full dependency tracking.
    - Make the Total robust so the insert cannot silently miss rows: put the data in an Excel Table and use `=SUBTOTAL(109,Table1[Amount])`, or `=SUM(B2:INDEX(B:B,ROW()-1))`, or `=SUM(OFFSET(...))` built from the Total row's own position. These self-adjust; a hardcoded `SUM(B2:B9)` does not.
    - After the insert, re-apply: merges, DV, CF, table ref, autofilter, chart refs, print area, row height, and styles — then verify by opening in Excel and checking the Total against a manual sum.

    Uncertainty note: I am confident about formulas, merges, DV, CF, tables, charts, print areas, row dimensions, and styles. I am less certain whether cell-attached hyperlinks and comments follow the shift cleanly in every openpyxl version — verify those two rather than trusting them.

- id: xlsx-verify-01
  answer: |
    Closed-book answer. Assumptions: the spreadsheet tooling isn't specified, no one can answer clarifying questions, and "budget.xlsx" lives in the current working directory unless you tell me otherwise. I'm treating the edit as non-destructive by default.

    Where the result goes
    - Write a new file next to the original: budget.with-summary.xlsx (or budget_summary.xlsx) in the same directory as budget.xlsx.
    - Never overwrite budget.xlsx in place unless the user explicitly asks for it. If the task truly requires the same path, keep a timestamped backup first (budget.backup-YYYYMMDD-HHMMSS.xlsx).
    - Don't dump it in /tmp or the home directory. The deliverable must sit beside the source so the user finds it.
    - Preserve the container format. If the source is .xlsm, write .xlsm and keep VBA. If it has external links or pivot caches, save in a format that keeps them.
    - Report the absolute path you actually wrote.

    What to check afterwards
    - Reopen the written file with an independent read and confirm it loads without a repair/“unreadable content” prompt.
    - Confirm the sheet set: original sheets still present, in original order, plus one new sheet named exactly "Summary" (no trailing space, not "Summary (2)").
    - Diff the untouched sheets against the original: cell values, formulas, number formats, merged cells, column widths, conditional formatting, charts, named ranges, data validation.
    - Check the Summary content: values match the source ranges, formulas evaluate to expected numbers, totals reconcile, no #REF!/#VALUE!/#DIV/0!, no hardcoded numbers where a formula belongs.
    - Confirm the Summary sheet has sane formatting: headers, number formats, column widths, no overflow into blank columns.
    - Verify nothing structural was lost: macros (if .xlsm), pivot tables, defined names, protection settings, hidden sheets.
    - If a computation was done externally (Python/openpyxl), spot-check those computed cells against the raw data.

    What to tell the user
    - The exact output path, and that the original budget.xlsx is unchanged.
    - What you added: sheet name, the range or table it summarizes, which columns/aggregations, and whether values are live formulas or static numbers.
    - The assumptions you made: which source sheet/range was treated as the budget, the summary grouping, currency/number format, and any rows excluded.
    - Anything not preserved or degraded, stated plainly rather than smoothed over.
    - One-line verification summary: what you reopened and what you confirmed, plus the residual gap (e.g. "not opened in real Excel, only re-parsed programmatically").

    The key point: save to a copy beside the source, validate by reopening and diffing the untouched sheets, and tell the user the path plus exactly what changed and what you assumed.

- id: xlsx-verify-02
  answer: |
    Assumption: "generated" means a chart written programmatically into an .xlsx file (openpyxl, xlsxwriter, or hand-rolled OOXML), and "checking" means verifying it is structurally valid and points at the right data before anyone opens it.

    The core idea: an xlsx is a zip of XML parts. A chart is not a picture; it is a small web of parts and references. You validate the web, not the pixels.

    How to check it

    1. Unzip the file and confirm the part chain exists. In order, the chain is:

       [Content_Types].xml declares an override for the chart part
       xl/worksheets/sheetN.xml contains a <drawing r:id="..."/> element
       xl/worksheets/_rels/sheetN.xml.rels resolves that r:id to ../drawings/drawingN.xml
       xl/drawings/drawingN.xml contains a graphicFrame whose c:chart r:id resolves, via xl/drawings/_rels/drawingN.xml.rels, to ../charts/chartN.xml
       xl/charts/chartN.xml is the chart body

       A break anywhere in that chain is the single most common reason a generated chart "doesn't show up" even though the script reported success.

    2. Parse every part with a real XML parser (lxml or ElementTree) and fail on parse errors. This catches unclosed tags, bad escaping, undeclared namespace prefixes, and invalid characters. Optionally validate against the ECMA-376 transitional/strict XSDs with xmllint if you have them.

    3. Inspect the chart body semantics:
       - c:chartSpace and c:chart exist, c:plotArea has at least one plot type, and the required axes are present for that type.
       - Every c:ser has c:idx and c:order, and those are unique and contiguous.
       - Every c:ser has c:val/c:numRef/c:f with a real range, plus c:cat for categories and c:tx for the series name.
       - The range strings parse as SheetName!$A$1:$B$10, the sheet name matches an actual sheet (and is quoted when it needs quoting, e.g. 'Q1 Sales'!$B$2), and the range is inside the used area.
       - Category count equals value count for every series, and all series have equal length.
       - The c:f ranges actually contain numbers, not text or blanks.
       - Axis IDs: every c:axId in the plot area has a matching c:catAx or c:valAx with the same val, and vice versa.
       - Scaling sanity: if c:min/c:max are set, min < max and the orientation/majorUnit are coherent.
       - Colors: srgbClr is 6 hex digits; schemeClr names a real theme slot (accent1-6, tx1, bg1, etc.).
       - Legend entries reference existing series indices.
       - For scatter/bubble, series use xVal/yVal rather than cat/val.
       - For stacked/percentStacked, grouping and overlap are consistent.

    4. Check the drawing anchor. The twoCellAnchor/oneCellAnchor from and to markers must sit inside the sheet grid, have positive width and height, and not overlap another chart or run off the printable area. Charts anchored at row 1048577 or with zero extent are effectively invisible.

    5. Round-trip with the generator's own reader. openpyxl load_workbook, then count ws._charts and compare chart type, series count, and references against what you intended. Useful but weak: openpyxl tolerates things Excel will not.

    6. If you need visual confirmation without Excel, render outside Excel: libreoffice --headless --convert-to pdf, then pdftoppm or a PDF-to-image step, then look at the image or diff it against a baseline. This is the only way to see actual layout.

    Structural mistakes this catches

    - Chart entirely absent: missing sheet <drawing>, missing rel, missing content-type override, or chart XML written to the wrong folder.
    - Dangling references: an r:id with no matching rel, or a rel pointing at a nonexistent part.
    - Excel "we found a problem with some content" repair prompts, usually caused by duplicate or mismatched axis IDs, malformed c:ser, or missing required children.
    - Empty chart: value or category ranges that resolve to blank or text-only cells.
    - Misaligned or truncated data: series ranges of unequal length, or categories shorter than values.
    - Wrong sheet: a reference to a sheet name that was renamed or never existed.
    - Wrong chart type for the data: scatter fed category references, bar with a category axis where a value axis was expected.
    - Series hidden or duplicated: conflicting c:idx/c:order, zero-length series, missing c:val.
    - Legend entries pointing at series that don't exist.
    - Invisible or overlapping charts: bad anchors, zero-size frames, charts stacked on the same cells.
    - Color or theme breakage: invalid hex, invalid scheme color names.
    - Namespace/parse errors that make the whole part unreadable.

    What this cannot tell you

    Structure validates plumbing, not appearance. It will not tell you whether labels collide, whether the font is too small, whether the aspect ratio looks wrong, whether a color is illegible, whether the axis tick density is ugly, or whether Excel and LibreOffice render it differently. Those need a real render (LibreOffice headless is the practical stand-in) or a human look. It also will not catch a chart that is structurally perfect but semantically wrong — right ranges, wrong columns — unless you separately assert the expected ranges against your data model.

    A reasonable default pipeline: unzip and parse every XML part, assert the full part-and-rel chain, assert axis ID symmetry and per-series range/count invariants against the source data, then optionally do a LibreOffice render for the visual layer. The XML assertions catch the failures that make Excel refuse or blank out; the render catches the ones that make humans wince.

    Confidence: high on the OOXML part chain, axis-ID and range invariants, and the openpyxl/LibreOffice workflow. Lower on exact schema-required-child minutiae per chart type — the ECMA-376 schema is the authority there, so if exact conformance matters, validate against the XSDs rather than trusting memory.

- id: xlsx-verify-03
  answer: |
    Assumption: "edits" means changing values, formulas, or layout while keeping the rest of the file intact. I work on copies, never the originals.

    **old_report.xls (legacy binary, BIFF8)**
    Do: read it only, with xlrd or pandas. Do not try openpyxl; it cannot open .xls. For real editing, convert to .xlsx first (LibreOffice headless is the safest converter), edit the .xlsx, and deliver that. If the user needs .xls back, convert at the end and verify.
    Warn:
    - Hard limits: 65,536 rows by 256 columns. Appending past that fails or truncates.
    - Conversion is lossy: styles, merged cells, charts, named ranges, pivot tables, data validation, and VBA are dropped or mangled.
    - xlrd returns cached formula results, not formula text. You cannot see or preserve the original formulas.
    - Dates arrive as serial numbers; the 1900/1904 date-system flag matters.
    - Password-protected or encrypted .xls is often unreadable by pure-Python libraries.
    - Writing .xls with xlwt is write-only and very limited. Prefer producing .xlsx.

    **quarterly.xlsm (OOXML with VBA)**
    Do: load with openpyxl using `keep_vba=True`, edit, and save with the .xlsm extension. If fidelity matters more than convenience, drive real Excel via COM (win32com) or LibreOffice. Never route it through `pandas.to_excel`, which builds a fresh workbook.
    Warn:
    - Saving as .xlsx silently strips `vbaProject.bin`. The macros are gone with no error.
    - Any edit invalidates a digital signature.
    - openpyxl may not preserve ActiveX controls, form controls, some charts, slicers, and some conditional formatting.
    - Formulas written by openpyxl have no cached value. `data_only=True` returns None until Excel recalculates. Excel recalculates on open; other readers may not.
    - Test the macros in Excel after the edit. Check Trust Center settings.
    - Do not rename .xlsm to .xlsx to "fix" a warning.

    **export.csv (plain text)**
    Do: treat it as untyped text. Read and write with the `csv` module or `pandas` with `dtype=str`. Detect and preserve encoding (UTF-8 vs cp1252), BOM, delimiter, quote character, and line terminator. Use `\r\n` if Excel will open it. Diff before and after.
    Warn:
    - No types: leading zeros, IDs longer than 15 digits, and long decimals get mangled. Excel rounds to 15 significant digits.
    - Date ambiguity: DD/MM vs MM/DD, and locale decimal separators.
    - Formula injection: fields starting with `=`, `+`, `-`, or `@` can execute when opened in Excel.
    - Embedded commas, quotes, and newlines need correct quoting.
    - No formatting, formulas, multiple sheets, or macros. One encoding only.
    - Opening and re-saving in Excel can silently alter the data. Keep the original bytes.

    **For all three**
    - Verify content by magic bytes, not extension. OLE2 header means .xls; ZIP `PK` means .xlsx/.xlsm.
    - Check for hidden sheets, external links, protected structure, and defined names before editing.
    - Keep a checksum of the original and open the result in the real application before delivery.
    - Tell the user which file you will return and in which format. Conversion is a decision, not a detail.

- id: xlsx-formatting-03
  answer: |
    Assumption: "header" means the column-header row at the top of the ledger, not an Excel page header. I cover both.

    Case 1 — column-header row repeats on every page
    Set print titles, not a page break:

      ws.print_title_rows = "1:1"

    This is the openpyxl equivalent of Excel's "Rows to repeat at top". If the header is more than one row, use "1:2". For split columns use ws.print_title_cols = "A:A". Also pin the print range so stray cells do not widen the output:

      ws.print_area = "A1:F200"   # 6 columns, last data row

    Case 2 — an actual page header (oddHeader text)
    Excel normally repeats page headers on every page. It appears only on page 1 when "Different first page" is on:

      ws.oddHeader.differentFirst = True
      ws.firstHeader.center.text = "..."

    To fix, set differentFirst = False, or write the same text into firstHeader. Note openpyxl only writes these XML flags; it does not paginate, so verify in Excel or LibreOffice.

    "Fit to one page wide" needs two settings
    fitToWidth alone is ignored. You must also enable fit-to-page on the sheet properties:

      ws.page_setup.fitToWidth = 1
      ws.page_setup.fitToHeight = 0        # 0 = as many pages tall as needed
      ws.sheet_properties.pageSetUpPr = PageSetupProperties(fitToPage=True)

      from openpyxl.worksheet.properties import PageSetupProperties

    fitToHeight = 0 matters. Setting it to 1 forces everything onto one page and shrinks text badly. The pageSetUpPr object is often None by default, so assign a new PageSetupProperties rather than setting .fitToPage on it in place.

    Related gotchas
    Explicit row_breaks or col_breaks can override or fight fit-to-page; clear them. A too-tight print_area makes Excel scale to include junk cells. Orientation and paper size change the result but do not enable the feature: ws.page_setup.orientation = "landscape" and ws.page_setup.paperSize = ws.PAPERSIZE_A4 help a 6-column ledger.

    Uncertainty: I am confident about print_title_rows and the fitToPage pairing. The exact attribute names on oddHeader.differentFirst and firstHeader are the parts I would double-check against the openpyxl docs before shipping.

- id: xlsx-formatting-04
  answer: |
    Assumptions: the workbook is .xlsx, the sheet is named Log, Category is column C, and row 1 is the header. Change C if your column differs.

    How to add it
    -------------
    Use an openpyxl DataValidation of type "list" and apply it to a range that extends far enough down the sheet.

        from openpyxl import load_workbook
        from openpyxl.worksheet.datavalidation import DataValidation

        wb = load_workbook("book.xlsx")
        ws = wb["Log"]

        dv = DataValidation(
            type="list",
            formula1='"Travel,Meals,Supplies,Misc"',
            allow_blank=True,
            showErrorMessage=True,
            showInputMessage=True,
            errorStyle="stop",
            errorTitle="Invalid category",
            error="Choose Travel, Meals, Supplies, or Misc.",
            promptTitle="Category",
            prompt="Pick a category.",
        )

        ws.add_data_validation(dv)
        dv.add("C2:C10000")      # or C2:C1048576 for the whole column
        wb.save("book.xlsx")

    The key to future rows is the sqref, the range passed to dv.add(). Any cell inside that range gets the dropdown when someone types in it later. Cells outside it do not. So either use a generous range or the whole column below the header.

    If openpyxl appends rows after the range, the validation does not automatically grow. Extend it before saving:

        dv.add("C10001:C20000")
        # or replace the range:
        # dv.sqref = "C2:C20000"

    Rows inserted inside the range in Excel normally remain covered because the reference shifts. Rows added below the range are not covered. An Excel Table is not a reliable substitute: tables auto-extend formatting and calculated columns, but do not depend on them to extend data validation.

    One openpyxl/Excel gotcha: leave showDropDown unset. In OOXML the showDropDown flag is inverted, so setting it True can hide the in-cell dropdown arrow instead of showing it.

    Inline-list limits
    ------------------
    The inline form, formula1='"Travel,Meals,Supplies,Misc"', has a hard limit of about 255 characters for the joined list. Commas count toward that limit. Four short items are nowhere near it.

    The comma is also the separator, so an item cannot contain a comma. "Meals, client" cannot be represented inline. Values containing commas, quotes, or a long list need a range instead.

    The inline list is static. It is embedded in the validation rule, so changing the choices means editing the rule. A range on a sheet is easier to maintain and has no 255-character practical limit.

    For a longer or comma-containing list, put the values on a hidden sheet and point formula1 at that range or at a defined name. A same-sheet range is the safest; older Excel versions reject direct cross-sheet validation references, so a defined name such as Categories is the portable workaround.

    What data validation does not protect against
    ---------------------------------------------
    It is data-entry UI, not a database constraint.

    - Paste bypasses it. Copying an invalid value from another cell and pasting it into a validated cell usually stores the value without Excel running the rule.
    - Programmatic writes bypass it. openpyxl, pandas, VBA, COM, Power Query, and external refreshes can write anything into the cell.
    - Existing values are not cleaned or flagged. Adding validation does not remove or mark values already in the column. Excel's Circle Invalid Data is a manual visual check.
    - New rows outside the sqref get nothing. This is the most common failure when someone assumes validation follows the table or the used range forever.
    - Formula results are not reliably validated. The rule governs entry, not every recalculated result.
    - allow_blank=True permits blanks. Even with allow_blank=False, do not treat the rule as a full required-field guarantee; clearing and other operations can still leave gaps.
    - Users can clear or replace the rule. Data validation is not worksheet protection and is not a security boundary.
    - It does not normalize values. It does not trim whitespace or canonicalize casing, and it does not maintain referential integrity if the source list changes. Existing entries can become stale when the list is edited.

    Treat the dropdown as guidance for humans. If the data feeds an import or report, validate the same allowed set again in Python or in the ETL step, and reject or normalize anything outside Travel, Meals, Supplies, and Misc.

- id: xlsx-preserve-03
  answer: |
    Assumptions: A1 notation, Excel or Google Sheets, read via libraries such as openpyxl, pandas, or xlsxwriter. Behaviour differs slightly by tool; the points below are the common core.

    WHAT HAPPENS INSIDE A MERGE

    - The merge is presentation metadata, not a container. The grid still has individual cells at A1, B1, C1, D1.
    - Only the top-left (anchor) cell keeps a value. Excel and Sheets discard the other values, usually after a warning.
    - Non-anchor cells become empty placeholders. In openpyxl they load as read-only MergedCell objects, so assigning a value raises an error.
    - Formatting is inconsistent. Borders apply to the outer edge of the range; fill and font often survive only on the anchor.
    - The merge range is recorded separately, as a ref such as A1:D1 or A2:A15. It is not part of the cell values.
    - Row and column dimensions grow to the merge. A merge A1:A500 makes max_row report 500 even with one real value.

    READING

    - Row-by-row reads return the title once, in A1, then blanks in B1:D1.
    - Vertical department merges return the label only on the first row of each block. Later rows are empty.
    - Naive pandas reads turn those blanks into NaN or empty strings. Grouping, pivots, and joins then drop or misclassify rows.
    - A merged multi-row header breaks header detection. header=0 captures only the anchor text.
    - CSV export is lossy. It writes the anchor value and blanks, with no record of the merge.
    - Phantom rows and columns from oversized merges inflate shapes and break row counts.

    SORTING

    - Excel refuses to sort a range with merges of different sizes. The error asks for identically sized merges.
    - Programmatic sorts usually move only cell values, not the merge ranges. Labels stay anchored to the wrong rows and the sheet looks scrambled.
    - Sorting rows under a vertical merged label separates rows from their department. The association exists only by position, not by data.

    FILTERING

    - AutoFilter and merged cells interact badly. Hiding the anchor row hides the whole label; hiding other rows leaves a label floating over unrelated data.
    - Filtering by department matches only the anchor row. The remaining rows of that department look blank and disappear from the result.
    - Some tools disable filtering outright on ranges that contain merges.

    APPENDING AND WRITING

    - Appending computes the next row from max_row, which merges can overstate. New data can land far below the visible table.
    - Appending into an existing merge produces a read-only cell error in openpyxl, or silently does nothing.
    - New rows never inherit the merge. Formatting and grouping drift out of sync.
    - Inserting rows shifts merges in Excel, but many libraries do not adjust the stored ranges.

    PRACTICAL RULES

    - Store data flat and tidy: one value per row, department repeated in every row.
    - Unmerge and fill down before any read, sort, filter, or append.
    - Apply merges last, in a reporting or styling layer, after all data work finishes.
    - Validate by checking merged_cells.ranges, not by trusting visible values.

- id: xlsx-preserve-04
  answer: |
    Closed-book answer, from knowledge only.

    **Finding the last data row**

    Do not trust `ws.max_row`. Scan a column you know is always populated (the date/ID column, usually A or B) starting below the header, and stop at the Total marker:

    ```
    header_row = 2          # title in 1, header in 2
    last_data = header_row
    total_row = None
    for row in ws.iter_rows(min_row=header_row + 1, min_col=1, max_col=1):
        c = row[0]
        v = c.value
        if v is None or str(v).strip() == "":
            continue                      # or break, if blanks mean end of log
        if str(v).strip().lower() in ("total", "totals", "grand total"):
            total_row = c.row
            break
        last_data = c.row
    ```

    Then new records start at `total_row` (the Total row is pushed down), or at `last_data + 1` if there is no Total row. A two-pass variant is also fine: find the Total row first, treat `total_row - 1` as the last data row, and verify it is non-empty.

    **Why `ws.max_row` misleads**

    - It reports the highest row index that *exists* in the sheet, not the highest row that holds data. A cell with only a fill, border, or number format still counts.
    - The Total row, footers, notes, and blank spacer rows below the data are included, so `max_row` lands on the Total row, not the last record.
    - A stray value in a distant column (say a comment in Z100) inflates it.
    - Merged ranges and data-validation or formatting artifacts can extend the used range.
    - On a file produced or edited by another tool, the stored `<dimension>`/`calculate_dimension()` may be stale or padded; openpyxl trusts it.
    - An empty sheet returns 1, which is indistinguishable from "data on row 1".
    - After `insert_rows`/`delete_rows` it is relative to whatever cells remain, so it is not a stable data boundary.

    For the same reason `ws.append()` is wrong here: it writes after the last existing row, i.e. *below* the Total row.

    **Where the new rows must go**

    Between the last data row and the Total row — never below the Total, never overlapping title or header. Concretely: `ws.insert_rows(total_row, amount=n)`, write the n records into rows `total_row .. total_row + n - 1`, and let the Total row move to `total_row + n`.

    Caveats that matter in practice:

    - openpyxl does not adjust formulas, tables, charts, or named ranges on insert/delete. Rewrite the Total formula explicitly, e.g. `=SUM(B3:B{last_data})` using the new last data row, and fix the autofilter ref, `ws.tables` ref, print area, and any validation/conditional-format ranges by hand.
    - `insert_rows` gives the new cells the default style; copy `number_format`, font, border, and alignment from the previous data row so the Log stays uniform.
    - If the layout is fragile, the sturdier design is: keep the log data-only and put the Total on a separate sheet or in an Excel Table totals row, so appends never push a summary around.

    **Assumptions:** title in row 1, header in row 2, data from row 3, literal text "Total" in column A of the last row, tab named "Log". If your Total marker sits in another column or is a formula-only cell, scan that column instead; if the Total formula is the only content of its row, identify the Total row as the first row below the data whose cells contain formulas.
```
