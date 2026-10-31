#!/usr/bin/env python3
"""Reference solutions for the checker self-test.

usage: ref.py <mode> <task> <workdir>
  good      a correct openpyxl solution (must PASS check.py)
  values    the good solution with every formula frozen to its value (must FAIL the live checks)
  badrange  chart tasks: Total row swept into the range / stacked / axes left deleted (must FAIL)
  naive     log-append: insert_rows() and type the values, nothing else adjusted (must FAIL)
  lazy      messy-clean: strip + str.title() only, no dedupe, no date conversion (must FAIL)
  hide      messy-clean: duplicates hidden instead of removed (must FAIL)
  partial   ledger-format: bold header only (must FAIL)
  today     orders-lookup: =TODAY() instead of the Settings as-of cell (must FAIL)
"""
import copy
import datetime as dt
import pathlib
import re
import sys

import openpyxl
from openpyxl.chart import BarChart, PieChart, Reference
from openpyxl.chart.label import DataLabelList
from openpyxl.formatting.rule import FormulaRule
from openpyxl.styles import Alignment, Font, PatternFill
from openpyxl.utils import get_column_letter as L

import lib
from lib import *  # noqa: F401,F403
from check import TASKS

RED = PatternFill("solid", bgColor="FFC7CE", fgColor="FFC7CE")


def style_like(dst, src):
    dst._style = copy.copy(src._style)


def attendance_status(wb, mode):
    ws = wb["Attendance"]
    for col, name in ((5, "Name"), (6, "Hours Worked"), (7, "Status")):
        ws.cell(1, col, name)
        style_like(ws.cell(1, col), ws["A1"])
    for r in range(2, 14):
        ws.cell(r, 5, f'=IFERROR(VLOOKUP(B{r},Roster!$A$2:$C$13,2,FALSE),"")')
        ws.cell(r, 6, f'=IF(OR(C{r}="",D{r}=""),"",ROUND((D{r}-C{r})*24,2))')
        ws.cell(r, 7, f'=IF(C{r}="","Absent",IF(C{r}>Settings!$B$1,"Late",IF(D{r}="","No check-out","OK")))')
        ws.cell(r, 6).number_format = "0.00"
    for c, w in ((5, 16), (6, 14), (7, 14)):
        ws.column_dimensions[L(c)].width = w


def attendance_summary(wb, mode):
    ws = wb.create_sheet("Summary")
    for i, h in enumerate(["Department", "Headcount", "Present", "Late", "Absent", "Attendance %"], 1):
        ws.cell(1, i, h).font = Font(bold=True)
    A = "Attendance!$D$2:$D$13"
    S = "Attendance!$H$2:$H$13"
    for r, d in enumerate(DEPTS, 2):
        ws.cell(r, 1, d)
        ws.cell(r, 2, f"=COUNTIF({A},A{r})")
        ws.cell(r, 3, f'=COUNTIFS({A},A{r},{S},"<>Absent")')
        ws.cell(r, 4, f'=COUNTIFS({A},A{r},{S},"Late")')
        ws.cell(r, 5, f'=COUNTIFS({A},A{r},{S},"Absent")')
        ws.cell(r, 6, f"=C{r}/B{r}")
    ws.cell(6, 1, "Total")
    for c in range(2, 6):
        ws.cell(6, c, f"=SUM({L(c)}2:{L(c)}5)")
    ws.cell(6, 6, "=C6/B6")
    for r in range(2, 7):
        ws.cell(r, 6).number_format = "0.0%"


def expense_summary(wb, mode):
    ws = wb.create_sheet("Summary")
    for i, h in enumerate(SUMMARY_HEAD, 1):
        ws.cell(1, i, h).font = Font(bold=True)
    for r, (cat, _) in enumerate(BUDGET, 2):
        ws.cell(r, 1, cat)
        ws.cell(r, 2, f"=SUMIF(Ledger!$B$2:$B$24,A{r},Ledger!$D$2:$D$24)")
        ws.cell(r, 3, f"=VLOOKUP(A{r},Budget!$A$2:$B$7,2,FALSE)")
        ws.cell(r, 4, f"=C{r}-B{r}")
        ws.cell(r, 5, f"=B{r}/C{r}")
        ws.cell(r, 6, f'=IF(B{r}>C{r},"Over budget","Within budget")')
    ws.cell(8, 1, "Total")
    for c in (2, 3, 4):
        ws.cell(8, c, f"=SUM({L(c)}2:{L(c)}7)")
    ws.cell(8, 5, "=B8/C8")
    for r in range(2, 9):
        ws.cell(r, 5).number_format = "0.0%"
        for c in (2, 3, 4):
            ws.cell(r, c).number_format = "#,##0.00"


def expense_pie(wb, mode):
    ws = wb["Summary"]
    ch = PieChart()
    ch.title = "Spend by Category"
    last = 8 if mode == "badrange" else 7                       # the classic mistake: Total row as a slice
    ch.add_data(Reference(ws, min_col=2, min_row=2, max_row=last), titles_from_data=False)
    ch.set_categories(Reference(ws, min_col=1, min_row=2, max_row=last))
    ch.dataLabels = DataLabelList()
    ch.dataLabels.showPercent = True
    ws.add_chart(ch, "H2")


def budget_bar(wb, mode):
    ws = wb["Summary"]
    ch = BarChart()
    ch.type, ch.title = "col", "Budget vs Actual"
    ch.grouping = "clustered"
    ch.add_data(Reference(ws, min_col=2, max_col=3, min_row=1, max_row=7), titles_from_data=True)
    ch.set_categories(Reference(ws, min_col=1, min_row=2, max_row=7))
    ch.x_axis.title, ch.y_axis.title = "Category", "Amount"
    ch.x_axis.delete = ch.y_axis.delete = False
    if mode == "badrange":                                      # stacked, Total row included, axes left as openpyxl wrote them
        ch.grouping, ch.overlap = "stacked", 100
        ch.x_axis.delete = ch.y_axis.delete = None
    ws.add_chart(ch, "H2")


def sales_pivot(wb, mode):
    ws = wb.create_sheet("Pivot")
    ws.cell(1, 1, "Region")
    for c, m in enumerate(MONTHS, 2):
        ws.cell(1, c, m)
    ws.cell(1, 5, "Total")
    D = "Data!$F$2:$F$73"
    for r, reg in enumerate(REGIONS, 2):
        ws.cell(r, 1, reg)
        for c in range(2, 5):
            ws.cell(r, c, f"=SUMIFS({D},Data!$C$2:$C$73,$A{r},Data!$B$2:$B$73,{L(c)}$1)")
        ws.cell(r, 5, f"=SUM(B{r}:D{r})")
    ws.cell(6, 1, "Total")
    for c in range(2, 6):
        ws.cell(6, c, f"=SUM({L(c)}2:{L(c)}5)")
    for row in ws.iter_rows(min_row=2, min_col=2):
        for c in row:
            c.number_format = "#,##0.00"


def sales_top3(wb, mode):
    ws = wb.create_sheet("Top Reps")
    for i, h in enumerate(["Rank", "Rep", "Total"], 1):
        ws.cell(1, i, h).font = Font(bold=True)
    for r, (name, _) in enumerate(rep_totals()[:3], 2):
        ws.cell(r, 1, r - 1)
        ws.cell(r, 2, name)
        ws.cell(r, 3, f"=SUMIF(Data!$D$2:$D$73,B{r},Data!$F$2:$F$73)").number_format = "#,##0.00"


def messy_clean(wb, mode):
    ws = wb.create_sheet("Clean")
    ws.append(MESSY_HEAD)
    if mode == "lazy":
        for r in wb["Raw"].iter_rows(min_row=2, values_only=True):
            if any(v is not None for v in r):
                ws.append([str(v).strip().title() if v is not None else None for v in r])
        return
    if mode == "hide":
        for r in wb["Raw"].iter_rows(min_row=2, values_only=True):
            ws.append([v for v in r])
        seen = set()
        for i, e in enumerate([e for e in CLEAN], 2):
            pass
        for i in (4, 8, 12, 13, 15, 3, 7):                      # hide blanks/dups by position, keep the dirty text
            ws.row_dimensions[i].hidden = True
        return
    for i, (d, name, dept, amt) in enumerate(CLEAN, 2):
        ws.append([d, name, dept, amt])
        ws.cell(i, 1).number_format = "yyyy-mm-dd"


def audit_variance(wb, mode):
    ws = wb["Stock Count"]
    for c, h in zip((6, 7, 8, 9), ("Variance", "Variance %", "Value Impact", "Flag")):
        ws.cell(1, c, h)
        style_like(ws.cell(1, c), ws["A1"])
    for r in range(2, 18):
        ws.cell(r, 6, f"=D{r}-C{r}")
        ws.cell(r, 7, f'=IF(C{r}=0,"n/a",F{r}/C{r})').number_format = "0.0%"
        ws.cell(r, 8, f"=F{r}*E{r}").number_format = "#,##0.00"
        ws.cell(r, 9, f'=IF(C{r}=0,IF(D{r}<>0,"Investigate","OK"),IF(ABS(F{r}/C{r})>0.02,"Investigate","OK"))')
    ws.conditional_formatting.add("A2:I17", FormulaRule(formula=['$I2="Investigate"'], fill=RED))
    sm = wb.create_sheet("Audit Summary")
    f = ["=SUM('Stock Count'!C2:C17)", "=SUM('Stock Count'!D2:D17)", "=B2-B1",
         "=COUNTIF('Stock Count'!I2:I17,\"Investigate\")", "=SUM('Stock Count'!H2:H17)"]
    for r, (lab, fx) in enumerate(zip(AUDIT_SUMMARY_LABELS, f), 1):
        sm.cell(r, 1, lab)
        sm.cell(r, 2, fx)
    sm.column_dimensions["A"].width = 24


def ledger_format(wb, mode):
    ws = wb["Ledger"]
    for c in ws[1]:
        c.font = Font(bold=True, color="FFFFFF")
        if mode != "partial":
            c.fill = PatternFill("solid", fgColor=HEAD_FILL)
    if mode == "partial":
        return
    ws.freeze_panes = "A2"
    ws.auto_filter.ref = "A1:F24"
    for r in range(2, ws.max_row + 1):
        ws.cell(r, 1).number_format = "yyyy-mm-dd"
        ws.cell(r, 4).number_format = "#,##0.00"
    for col in range(1, 7):
        longest = max(len(str(ws.cell(r, col).value or "")) for r in range(1, ws.max_row + 1))
        ws.column_dimensions[L(col)].width = max(10, longest + 2)
    ws.print_title_rows = "1:1"
    ws.page_setup.orientation = "landscape"
    ws.sheet_properties.pageSetUpPr = openpyxl.worksheet.properties.PageSetupProperties(fitToPage=True)
    ws.page_setup.fitToWidth, ws.page_setup.fitToHeight = 1, 0


def orders_lookup(wb, mode):
    ws = wb["Orders"]
    for c, h in zip((5, 6, 7, 8), ("Customer", "Due Date", "Days Overdue", "Status")):
        ws.cell(1, c, h)
        style_like(ws.cell(1, c), ws["A1"])
    asof = "TODAY()" if mode == "today" else "Settings!$B$1"
    for r in range(2, 17):
        ws.cell(r, 5, f'=IFERROR(VLOOKUP(C{r},Customers!$A$2:$C$6,2,FALSE),"UNKNOWN")')
        ws.cell(r, 6, f'=IFERROR(B{r}+VLOOKUP(C{r},Customers!$A$2:$C$6,3,FALSE),"")').number_format = "yyyy-mm-dd"
        ws.cell(r, 7, f'=IF(F{r}="",0,MAX(0,{asof}-F{r}))')
        ws.cell(r, 8, f'=IF(F{r}="","Check customer",IF(G{r}>0,"Overdue","Not due"))')
    for c, w in ((5, 20), (6, 12), (7, 14), (8, 16)):
        ws.column_dimensions[L(c)].width = w


def expense_daily(wb, mode):
    ws = wb.create_sheet("Daily Report")
    ws["A1"], ws["B1"] = "Report date", dt.datetime(2026, 10, 28)
    ws["B1"].number_format = "yyyy-mm-dd"
    A, B, D = "Ledger!$A$2:$A$24", "Ledger!$B$2:$B$24", "Ledger!$D$2:$D$24"
    ws["A3"], ws["B3"] = "Total spent", f"=SUMIFS({D},{A},$B$1)"
    ws["A4"], ws["B4"] = "Transactions", f"=COUNTIFS({A},$B$1)"
    ws["A5"], ws["B5"] = "Largest expense", f"=SUMPRODUCT(MAX(({A}=$B$1)*{D}))"
    ws["A6"], ws["B6"] = "Average expense", "=IFERROR(B3/B4,0)"
    ws["A9"], ws["B9"] = "Category", "Spent"
    for r, (cat, _) in enumerate(BUDGET, 10):
        ws.cell(r, 1, cat)
        ws.cell(r, 2, f"=SUMIFS({D},{B},A{r},{A},$B$1)")
    for r in (3, 5, 6, *range(10, 16)):
        ws.cell(r, 2).number_format = "#,##0.00"
    ws.column_dimensions["A"].width = 18


def log_append(wb, mode):
    ws = wb["Log"]
    ws.insert_rows(10)
    for c, v in enumerate(NEW_ENTRY, 1):
        ws.cell(10, c, v)
        style_like(ws.cell(10, c), ws.cell(9, c))
    if mode == "naive":
        return
    ws["D11"] = "=SUM(D4:D10)"
    for s in ws._charts[0].series:
        s.val.numRef.f = "'Log'!$D$4:$D$10"
        if s.cat is not None:
            (s.cat.numRef or s.cat.strRef).f = "'Log'!$B$4:$B$10"
    wb.defined_names["LogTotal"].attr_text = "Log!$D$11"


FUNCS = {
    "attendance-status": attendance_status, "attendance-summary": attendance_summary,
    "expense-summary": expense_summary, "expense-pie": expense_pie, "budget-bar": budget_bar,
    "sales-pivot": sales_pivot, "sales-top3": sales_top3, "messy-clean": messy_clean,
    "audit-variance": audit_variance, "ledger-format": ledger_format, "orders-lookup": orders_lookup,
    "expense-daily": expense_daily, "log-append": log_append,
}


def freeze(path):
    """Replace every formula with its recalculated value (the 'pasted numbers' cover-up)."""
    vals = openpyxl.load_workbook(lib.recalc(path), data_only=True)
    wb = openpyxl.load_workbook(path)
    for ws in wb.worksheets:
        for row in ws.iter_rows():
            for c in row:
                if isinstance(c.value, str) and c.value.startswith("="):
                    v = vals[ws.title][c.coordinate].value
                    c.value = None if v == "" else v
    wb.save(path)


if __name__ == "__main__":
    mode, task, wd = sys.argv[1], sys.argv[2], pathlib.Path(sys.argv[3])
    fixture = TASKS[task]
    wb = openpyxl.load_workbook(wd / fixture)
    FUNCS[task](wb, "good" if mode == "values" else mode)
    out = wd / (pathlib.Path(fixture).stem + "_out.xlsx")
    wb.save(out)
    if mode == "values":
        freeze(out)
