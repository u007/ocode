# Xlsx — Kaizen blind answer sheet (questions only)

> **CLOSED-BOOK.** Answer every question from your own knowledge alone. You MUST
> NOT open, search, or otherwise access the Kaizen corpus — `questions.yaml`,
> `questions.md`, `scores/`, `derived/`, `meta.yaml`, or any file in this repo —
> nor look the answers up online. Doing so invalidates the evaluation.
>
> Answer each question **independently** (treat every item as a fresh context —
> no memory of earlier answers). If you are unsure, say so; do not guess to look
> complete. This measures what you actually know, not what you can retrieve.
>
> **Return format** — one YAML record per question so the grader can map answers
> back by id:
>
> ```yaml
> - id: <question-id>
>   answer: |
>     <your answer>
> ```

Total questions: 38

---

### xlsx-model-01

A script writes `ws["B7"] = "=SUM(B1:B5)"` with openpyxl and saves. Another script then opens the file with `load_workbook(path, data_only=True)` and reads B7. What does it get, why, and what happens if that second script saves the workbook?

### xlsx-model-02

Your task says "add a Total column using formulas". There is no Excel on the machine. How do you produce the file AND prove the formulas calculate correctly, and why is computing the totals in Python and writing the numbers not an acceptable shortcut?

### xlsx-model-03

Compare openpyxl, XlsxWriter and `pandas.DataFrame.to_excel` for a clerk's workbook task. Which of them can edit an existing .xlsx, which can write formulas and charts, and which can create a real PivotTable?

### xlsx-model-04

With openpyxl you write `=IFNA(VLOOKUP(A2,Customers!A:B,2,FALSE),"UNKNOWN")`, and a colleague writes `=MAXIFS(D:D,B:B,"Travel")` and `=CONCAT(A2,B2)`. In the recalculated file two of the three show #NAME? or the raw error. What went wrong, how do you fix it, and what else limits which functions you should use?

### xlsx-formulas-01

State the argument order of SUMIF and of SUMIFS, and write the formula that totals Ledger!D by category (Ledger!B) for the category named in A2 on one specific date in B1 (Ledger!A).

### xlsx-formulas-02

You fill a formula down 12 rows and across 3 month columns. When do you need `$A2`, `B$1`, `$B$1`? And how do you reference a cell on a sheet named "Stock Count" or "Daily Report"?

### xlsx-formulas-03

A stock-audit sheet computes Variance % as `=(Counted-System)/System` and some rows have System Qty = 0. What happens, what should the cell show, and why is wrapping everything in IFERROR(...,0) a poor fix?

### xlsx-formulas-04

An attendance sheet has Check-in times. Status is `=IF(C2>Settings!B1,"Late","OK")` and Settings!B1 is 09:00. An employee who never punched in (C2 empty) shows as OK. Why, and what is the correct precedence of checks for Absent / Late / No check-out / OK?

### xlsx-formulas-05

A daily report must show "Total spent" for the date in cell B1. Give two designs: one that is wrong and one that is right, and say how you would test that the right one is really live.

### xlsx-formulas-06

A log has a Total row directly under the last entry: `=SUM(D4:D9)` in D10. Tomorrow a clerk inserts a row at row 10, above Total. What happens to the formula in Excel, what happens if a script does `ws.insert_rows(10)` with openpyxl, and how should the layout be designed to avoid it?

### xlsx-lookup-01

Write the formula that pulls an employee's name from Roster!A:C (EmpID, Name, Dept) for the ID in B2. What does the fourth argument of VLOOKUP do, and what goes wrong if it is omitted? Give the INDEX/MATCH equivalent and one reason to prefer it.

### xlsx-lookup-02

For an orders list, customers missing from the Customers sheet must show "UNKNOWN" and still not produce errors in the Due Date and Days Overdue columns that depend on them. How do you build the chain, and what is the difference between IFERROR and IFNA here?

### xlsx-lookup-03

VLOOKUP returns #N/A for an ID that visibly exists in both sheets ("007" in one, 7 in the other, or "C001 " with a trailing space). Why, and how do you diagnose and fix it?

### xlsx-dates-01

How does Excel store a date and a time of day? A script writes the string "2026-10-28" into a cell; what breaks later (SUMIFS by date, sorting, subtracting) and how do you write a proper date with openpyxl? Why might a date column show 46323?

### xlsx-dates-02

Compute hours worked from Check-in and Check-out in two decimals, blank when a punch is missing. What changes for a night shift 22:00 to 06:00?

### xlsx-dates-03

Late is defined as "check-in after 09:00". A clerk types the cutoff into formulas as 0.375 in one place and `TIME(9,0,0)` in another and sees one employee flagged only sometimes. What causes boundary surprises with times and decimals, and how do you make the comparison robust?

### xlsx-dates-04

Orders have an Order Date and a customer payment term in days. Give the Due Date and Days Overdue formulas, define precisely when an order is overdue, and explain why the "as of" date should not be TODAY().

### xlsx-charts-01

Which chart for each: share of the day's expenses by category; budget vs actual per category; attendance rate per day over a month; the 10 reps' ranking? Name one case where a pie chart is the wrong choice.

### xlsx-charts-02

Your Summary sheet has the header in row 1, six category rows in 2-7 and a Total row in row 8. You build a pie chart with `Reference(ws, min_col=2, min_row=2, max_row=8)`. What does the chart look like, what is the right range, and what about a category with 0 spend?

### xlsx-charts-03

Write the openpyxl steps for a clustered column chart of Spent vs Budget (columns B and C, header in row 1, categories A2:A7) with a title, axis titles and legend, placed so it does not cover the table. Name two things that commonly go wrong in the rendered file.

### xlsx-summary-01

The boss asks for a Region x Month pivot of the Data sheet. Can you create a PivotTable with openpyxl? What are your options, and what must you tell the user about each?

### xlsx-summary-02

Give the formula for the top-left body cell of a Region (rows, column A) x Month (columns, row 1) grid over Data!C:C (Region), Data!B:B (Month text), Data!F:F (Amount) that can be copied across and down, and the row/column/grand total formulas.

### xlsx-summary-03

Show the three best reps by total sales without FILTER/SORT/UNIQUE (older Excel must work). What problem do ties cause, and which parts can be formulas?

### xlsx-cleanup-01

An export has Amount values like "1200.50", " 75 " and "RM 45.00" stored as text, and dates like "2026-10-01" and "01 Oct 2026" as text. How do you detect and convert them, and what invisible characters do you watch for?

### xlsx-cleanup-02

You must remove duplicate entries from a messy sheet and keep the original untouched. In what order do you normalise and de-duplicate, which rows survive, and why is hiding the duplicate rows not acceptable?

### xlsx-cleanup-03

Names arrive as "ben lim", "BEN LIM" and "Daniel Wong"; departments as "ops", " Ops", "HR", "hr". Why is `str.title()` alone wrong, and how do you normalise them?

### xlsx-cleanup-04

A CSV of employees has IDs like 007 and 010, phone numbers and a UTF-8 BOM. You load it with `pd.read_csv` and save to xlsx. What gets damaged and how do you prevent it?

### xlsx-formatting-01

Make a ledger presentable with openpyxl: header style, frozen header, filter buttons, date and money formats, and column widths. Name what openpyxl cannot do automatically here, and how percent values must be stored.

### xlsx-formatting-02

Rows flagged "Investigate" must turn red and stay correct when tomorrow's numbers are pasted in. How do you do it, and why is filling the cells red in Python wrong?

### xlsx-preserve-01

You must add one row to a workbook that a colleague built over the years (charts, dropdowns, highlighting, named ranges, maybe macros). You plan `load_workbook` → edit → `save`. What can be lost or altered, and how do you protect the colleague's work?

### xlsx-preserve-02

With openpyxl you call `ws.insert_rows(10)` to add an entry above the Total row. List everything that is NOT updated automatically and must be fixed by hand.

### xlsx-verify-01

You are asked to "add a Summary sheet to budget.xlsx". Where do you save the result, what do you check afterwards, and what do you tell the user?

### xlsx-verify-02

How do you check a chart you generated without opening Excel, and which structural mistakes can you detect that way?

### xlsx-verify-03

The user hands you `old_report.xls`, `quarterly.xlsm` and `export.csv` and wants edits. What do you do differently for each, and what must you warn about?

### xlsx-formatting-03

A 6-column ledger prints across two pages with the header only on page 1. Which print settings fix that in openpyxl, and what does "fit to one page wide" need besides the width value?

### xlsx-formatting-04

Add a Category dropdown (Travel, Meals, Supplies, Misc) to a Log sheet with openpyxl so that new rows entered later also get it. How, what limits the inline list, and what does validation not protect against?

### xlsx-preserve-03

A report has a merged title in A1:D1 and merged department labels in column A. What happens to the cells inside a merge, and what problems do merges cause when you read, sort, filter or append rows programmatically?

### xlsx-preserve-04

Each day a clerk's script must append the day's entries to a Log sheet that has a title, a header, data rows and a Total row. How do you find the last data row with openpyxl, why can `ws.max_row` mislead, and where must the new rows go?
