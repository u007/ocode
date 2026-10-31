#!/bin/bash
# usage: probe.sh <provider/model> <task> <label> [rep]
# Fresh git repo per run; checker + fixtures stay outside it. ocode binary: $OCODE_BIN (default: ocode).
# The model needs whatever credentials `ocode run` normally uses; nothing is scraped from the shell.
set -u
SP=$(cd "$(dirname "$0")" && pwd)
MODEL=$1 TASK=$2 LABEL=$3 REP=${4:-1} BIN=${OCODE_BIN:-ocode}
[ -d "$SP/fixtures" ] || python3 "$SP/make_fixtures.py" > /dev/null
FX=$(cd "$SP" && python3 -c "import sys; from check import TASKS; print(TASKS[sys.argv[1]])" "$TASK") || { echo "unknown task $TASK"; exit 2; }
OUT="${FX%.xlsx}_out.xlsx"
W="$SP/runs/$LABEL/$(basename "$MODEL")/$TASK.$REP"
rm -rf "$W"; mkdir -p "$W"; cd "$W"
git init -q
cp "$SP/fixtures/$FX" .
git add -A; git -c user.email=p@x -c user.name=p commit -qm fixture

SAVE="Save the result as $OUT in this directory and leave $FX unchanged."
case $TASK in
  attendance-status) P="In $FX, on the Attendance sheet add three columns after Check-out: \"Name\" (the employee's name, looked up from the Roster sheet by EmpID), \"Hours Worked\" (check-out minus check-in in hours, 2 decimals, blank if either punch is missing) and \"Status\": \"Absent\" if there is no check-in; otherwise \"Late\" if the check-in is after the shift start in Settings!B1; otherwise \"No check-out\" if there is no check-out; otherwise \"OK\". Use formulas so the sheet keeps working when tomorrow's punches are pasted in, and do not use XLOOKUP (some of us run older Excel). Make the new headers look like the existing ones. $SAVE" ;;
  attendance-summary) P="In $FX add a new sheet \"Summary\" with one row per department (Sales, Ops, Finance, HR in that order) and the columns Department, Headcount, Present, Late, Absent, Attendance %. Present counts everyone who is not Absent (Late and No check-out people are present). Attendance % = Present / Headcount shown as a percentage. Add a Total row at the bottom. Use formulas that read the Attendance sheet, so tomorrow's data flows through. $SAVE" ;;
  expense-summary) P="In $FX add a sheet \"Summary\" with one row per category on the Budget sheet (same order) and the columns Category, Spent, Budget, Remaining, % Used, Status. Spent = the total of that category on the Ledger sheet; Budget comes from the Budget sheet; Remaining = Budget - Spent; % Used = Spent / Budget shown as a percentage; Status = \"Over budget\" when Spent is more than Budget, otherwise \"Within budget\". Add a Total row at the bottom (Spent, Budget, Remaining and the overall % Used). Use formulas, not typed numbers, so new ledger lines flow through. $SAVE" ;;
  expense-pie) P="In $FX, on the Summary sheet, add a pie chart of each category's share of total spend. Title it \"Spend by Category\", show percentage labels on the slices, and put it to the right of the table so it does not cover any data. Do not change the numbers. $SAVE" ;;
  budget-bar) P="In $FX, on the Summary sheet, add a clustered column chart comparing Spent with Budget for every category. Title it \"Budget vs Actual\", label the horizontal axis \"Category\" and the vertical axis \"Amount\", show a legend naming the two series Spent and Budget, and put the chart to the right of the table so it does not cover any data. Do not change the numbers. $SAVE" ;;
  sales-pivot) P="In $FX add a new sheet \"Pivot\" that summarises the Data sheet: regions down the side (North, South, East, West), months across (2026-07, 2026-08, 2026-09) holding the total Amount, plus a Total column and a Total row. It must update when the Data sheet changes. A real PivotTable or formulas are both fine; do not use XLOOKUP or dynamic-array functions. $SAVE" ;;
  sales-top3) P="In $FX add a sheet \"Top Reps\" listing the three reps with the highest total Amount over the whole Data sheet, best first, with the columns Rank (1-3), Rep and Total. The Total cells must be formulas on the Data sheet so they update when Data changes. $SAVE" ;;
  messy-clean) P="In $FX the Raw sheet is a messy export. Create a new sheet \"Clean\" with the same four columns (Date, Employee, Dept, Amount) containing the cleaned data: real dates shown as yyyy-mm-dd, names trimmed with single spaces and Title Case, Dept one of Sales / Ops / Finance / HR (fix the case), Amount as a number, blank rows removed, and duplicate entries (same date, employee, dept and amount) removed keeping the first. Leave the Raw sheet exactly as it is. $SAVE" ;;
  audit-variance) P="In $FX, on the Stock Count sheet add four columns after Unit Cost: Variance (Counted - System), Variance % (Variance / System, shown as a percentage; show \"n/a\" when System Qty is 0), Value Impact (Variance x Unit Cost) and Flag (\"Investigate\" when the variance is more than 2% either way, or when System Qty is 0 but something was counted; otherwise \"OK\"). Highlight the rows flagged Investigate with a red fill that updates automatically. Add a sheet \"Audit Summary\" with these labels in column A and the figures in column B, in this order: Total system units, Total counted units, Net variance (units), Items to investigate, Net value impact. Use formulas. $SAVE" ;;
  ledger-format) P="In $FX the Ledger sheet is unformatted and the dates show as numbers. Make it presentable for printing: bold header row with a fill colour, header frozen when scrolling, filter buttons on the header, dates shown as yyyy-mm-dd, Amount with 2 decimals, every column wide enough for its content, and printing in landscape, one page wide, with the header row repeated on every page. Do not change any values. $SAVE" ;;
  orders-lookup) P="In $FX, on the Orders sheet add four columns after Amount: Customer (name from the Customers sheet; \"UNKNOWN\" if the CustID is not there), Due Date (Order Date + the customer's Terms in days, shown as yyyy-mm-dd; blank for an unknown customer), Days Overdue (days past the due date as of the date in Settings!B1, never negative; 0 for an unknown customer) and Status (\"Overdue\" if Days Overdue is above 0, \"Check customer\" for an unknown customer, otherwise \"Not due\"). Use formulas and do not use TODAY() or XLOOKUP. $SAVE" ;;
  expense-daily) P="In $FX add a sheet \"Daily Report\" driven by a date. A1 = \"Report date\" and B1 = the date 2026-10-28 (a real date). Then A3:B6 hold: Total spent, Transactions (count), Largest expense, Average expense, all for the Ledger lines on the date in B1. From A9: a header row \"Category\" | \"Spent\", then one row per category from the Budget sheet (same order) with that category's spend on the date in B1 (0 when none). Everything must be formulas so changing B1 to another date updates the whole report. $SAVE" ;;
  log-append) P="In $FX add today's entry to the Log sheet: date 2026-10-29, item \"Courier fees\", category Misc, amount 85.50. It goes directly above the Total row, so the Total row moves down one and must still add up everything. The chart, the category dropdown, the over-100 highlighting, the merged title and the named cell LogTotal must keep working with the new row included. $SAVE" ;;
  *) echo "unknown task $TASK"; exit 2 ;;
esac

start=$(date +%s)
"$BIN" run -yolo -m "$MODEL" -effort med -timeout 900 -p "$P" < /dev/null > "$W.out" 2> "$W.err"
rc=$?
echo "rc=$rc secs=$(( $(date +%s) - start ))" > "$W.meta"
python3 "$SP/check.py" "$TASK" "$W" > "$W.check" 2>&1
echo "$LABEL $MODEL $TASK.$REP rc=$rc $(tail -1 "$W.check")"
