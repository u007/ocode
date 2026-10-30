#!/usr/bin/env python3
"""Structural + behavioural checker for the xlsx office-clerk probe.

usage: check.py <task> <workdir>
  workdir holds the pristine input (must be byte-identical to fixtures/) and the model's
  output <stem>_out.xlsx.
Excel itself is not available. Values are read after a LibreOffice headless recalculation, and
for every "live formula" task the checker EDITS an input in a copy of the model's file, recalculates
again and verifies the result moved. Pasted values, hard-coded dates/thresholds and cover-ups
(hidden rows, chart over the data, wrong chart range) fail.
Prints one line per check (PASS/FAIL) and a final "SCORE n/m PASS|FAIL" line.
"""
import datetime as dt
import hashlib
import pathlib
import re
import sys
import zipfile

import openpyxl

import lib
from lib import *  # noqa: F401,F403

HERE = pathlib.Path(__file__).resolve().parent
FIX = HERE / "fixtures"
ERRORS = ("#REF!", "#DIV/0!", "#NAME?", "#VALUE!", "#N/A", "#NUM!", "#NULL!", "Err:")

# task -> input fixture; output is <stem>_out.xlsx
TASKS = {
    "attendance-status": "attendance.xlsx",
    "attendance-summary": "attendance_status.xlsx",
    "expense-summary": "expenses.xlsx",
    "expense-pie": "expenses_summary.xlsx",
    "budget-bar": "expenses_summary.xlsx",
    "sales-pivot": "sales.xlsx",
    "sales-top3": "sales.xlsx",
    "messy-clean": "messy.xlsx",
    "audit-variance": "audit.xlsx",
    "ledger-format": "ledger_plain.xlsx",
    "orders-lookup": "orders.xlsx",
    "expense-daily": "expenses.xlsx",
    "log-append": "daily_log.xlsx",
}

results = []


def check(name, ok, detail=""):
    results.append(bool(ok))
    print(("PASS " if ok else "FAIL ") + name + (f"  [{detail}]" if detail and not ok else ""))


def finish():
    n = sum(results)
    print(f"SCORE {n}/{len(results)} {'PASS' if n == len(results) else 'FAIL'}")
    sys.exit(0)


def sha(p):
    return hashlib.sha256(pathlib.Path(p).read_bytes()).hexdigest()


def close(a, b, tol=0.01):
    try:
        return abs(float(a) - float(b)) <= tol
    except (TypeError, ValueError):
        return False


def blank(v):
    return v is None or (isinstance(v, str) and v.strip() == "")


def txt(v):
    return "" if v is None else str(v).strip()


def sheet(wb, name):
    for ws in wb.worksheets:
        if ws.title.strip().lower() == name.lower():
            return ws
    return None


def is_formula(c):
    return isinstance(c.value, str) and c.value.startswith("=")


def sheet_values(ws):
    return [[c.value for c in r] for r in ws.iter_rows()]


def unchanged(out_wb, in_wb, names=None):
    """Every input sheet in `names` (default all) has identical cell values in the output."""
    bad = []
    for ws in in_wb.worksheets:
        if names and ws.title not in names:
            continue
        o = sheet(out_wb, ws.title)
        if o is None:
            bad.append(f"{ws.title}: missing")
            continue
        for row in ws.iter_rows():
            for c in row:
                ov = o.cell(c.row, c.column).value
                if isinstance(ov, dt.datetime) and isinstance(c.value, (int, float)):
                    ov = openpyxl.utils.datetime.to_excel(ov)          # same serial, now date-formatted
                if c.value != ov and not (isinstance(c.value, (int, float)) and close(c.value, ov, 1e-9)):
                    bad.append(f"{ws.title}!{c.coordinate}: {c.value!r} -> {ov!r}")
                    break
    return not bad, "; ".join(bad[:3])


def no_errors(wv):
    bad = []
    for ws in wv.worksheets:
        for row in ws.iter_rows():
            for c in row:
                if isinstance(c.value, str) and c.value.startswith(ERRORS):
                    bad.append(f"{ws.title}!{c.coordinate}={c.value}")
    return not bad, ", ".join(bad[:4])


def no_hidden(ws):
    rows = [r for r, d in ws.row_dimensions.items() if d.hidden]
    cols = [c for c, d in ws.column_dimensions.items() if d.hidden]
    return not rows and not cols and ws.sheet_state == "visible", f"rows={rows[:3]} cols={cols[:3]} state={ws.sheet_state}"


def norm(s):
    return re.sub(r"[\s_]+", "", txt(s).lower())


def find_header(ws, wanted, row=1):
    """column index per wanted header (matched ignoring case/space), or None."""
    have = {norm(c.value): c.column for c in ws[row] if not blank(c.value)}
    return {w: have.get(norm(w)) for w in wanted}


def find_row(ws, label, col=1, start=1):
    for r in range(start, ws.max_row + 1):
        if txt(ws.cell(r, col).value).lower() == label.lower():
            return r
    return None


def month_of(v):
    d = norm_date(v) if not isinstance(v, str) else None
    if d is not None:
        return f"{d.year}-{d.month:02d}"
    return txt(v)


def open_task(task, wd):
    fixture = TASKS[task]
    out = wd / (pathlib.Path(fixture).stem + "_out.xlsx")
    check("original input unchanged", (wd / fixture).exists() and sha(wd / fixture) == sha(FIX / fixture))
    if not out.exists():
        check(f"output {out.name} saved", False)
        finish()
    try:
        wf = openpyxl.load_workbook(out)
        wv = lib.values(out)
    except Exception as e:                       # corrupt file / recalc failure
        check("output opens and recalculates", False, repr(e)[:120])
        finish()
    check("output opens and recalculates", True)
    ok, d = no_errors(wv)
    check("no error values (#REF!/#DIV/0!/#NAME?/#N/A ...)", ok, d)
    return out, wf, wv, openpyxl.load_workbook(FIX / fixture)


# ---------------------------------------------------------------------- tasks

def attendance_status(out, wf, wv, fx):
    ws, wsv = sheet(wf, "Attendance"), sheet(wv, "Attendance")
    check("Attendance, Roster and Settings sheets kept", all(sheet(wf, n) for n in ("Attendance", "Roster", "Settings")))
    ok, d = unchanged(wf, fx, ["Attendance", "Roster", "Settings"])
    check("original columns and sheets untouched", ok, d)
    h = find_header(ws, ["Name", "Hours Worked", "Status"])
    check("columns Name / Hours Worked / Status exist", all(h.values()), str(h))
    if not all(h.values()):
        return

    def grade(wsv, shift):
        names = {e: n for e, n, _ in ROSTER}
        bad = {"name": [], "hours": [], "status": []}
        for r in range(2, 14):
            emp = wsv.cell(r, 2).value
            if wsv.cell(r, h["Name"]).value != names[emp]:
                bad["name"].append(f"{emp}:{wsv.cell(r, h['Name']).value!r}")
            hv, eh = wsv.cell(r, h["Hours Worked"]).value, att_hours(emp)
            if not (blank(hv) if eh is None else close(hv, eh)):
                bad["hours"].append(f"{emp}:{hv!r} want {eh}")
            if wsv.cell(r, h["Status"]).value != att_status(emp, shift):
                bad["status"].append(f"{emp}:{wsv.cell(r, h['Status']).value!r} want {att_status(emp, shift)!r}")
        return bad

    bad = grade(wsv, "09:00")
    check("Name looked up from Roster for all 12 rows", not bad["name"], str(bad["name"][:3]))
    check("Hours Worked = out - in (2 dp), blank when a punch is missing", not bad["hours"], str(bad["hours"][:3]))
    check("Status: Absent > Late > No check-out > OK (09:00 is not late)", not bad["status"], str(bad["status"][:3]))
    check("Name, Hours, Status are formulas in every row",
          all(is_formula(ws.cell(r, h[k])) for r in range(2, 14) for k in ("Name", "Hours Worked", "Status")))
    check("Status reads the shift start from Settings (no hard-coded time)",
          all("settings" in str(ws.cell(r, h["Status"]).value).lower() for r in range(2, 14)))
    ref = ws["A1"]
    same = all(ws.cell(1, h[k]).font.bold == ref.font.bold and ws.cell(1, h[k]).fill.fgColor.rgb == ref.fill.fgColor.rgb
               for k in ("Name", "Hours Worked", "Status"))
    check("new header cells look like the existing header", same)
    wm = lib.values_after_edit(out, lambda wb: setattr(wb["Settings"]["B1"], "value", hm("09:10")))
    bad = grade(sheet(wm, "Attendance"), "09:10")
    check("live: shift start 09:10 re-flags the late list", not bad["status"], str(bad["status"][:3]))


def attendance_summary(out, wf, wv, fx):
    ok, d = unchanged(wf, fx, ["Attendance"])
    check("Attendance sheet untouched", ok, d)
    ws, wsv = sheet(wf, "Summary"), sheet(wv, "Summary")
    check("sheet 'Summary' created", ws is not None)
    if ws is None:
        return
    want = ["Department", "Headcount", "Present", "Late", "Absent", "Attendance %"]
    h = find_header(ws, want)
    check("headers Department/Headcount/Present/Late/Absent/Attendance %", all(h.values()), str(h))
    if not all(h.values()):
        return

    def grade(wsv, override=None):
        exp = att_summary(override)
        bad = []
        for i, dept in enumerate(DEPTS, 2):
            if txt(wsv.cell(i, h["Department"]).value) != dept:
                bad.append(f"row{i} dept {wsv.cell(i, h['Department']).value!r}")
                continue
            hc, p, l, a = exp[dept]
            got = [wsv.cell(i, h[k]).value for k in ("Headcount", "Present", "Late", "Absent")]
            if not all(close(g, e, 1e-9) for g, e in zip(got, (hc, p, l, a))):
                bad.append(f"{dept}:{got} want {(hc, p, l, a)}")
            if not close(wsv.cell(i, h["Attendance %"]).value, p / hc, 0.001):
                bad.append(f"{dept} pct {wsv.cell(i, h['Attendance %']).value!r}")
        t = 6
        tot = [sum(exp[d][k] for d in DEPTS) for k in range(4)]
        got = [wsv.cell(t, h[k]).value for k in ("Headcount", "Present", "Late", "Absent")]
        if txt(wsv.cell(t, h["Department"]).value).lower() != "total" or not all(close(g, e, 1e-9) for g, e in zip(got, tot)):
            bad.append(f"total row {got} want {tot}")
        return bad

    bad = grade(wsv)
    check("rows Sales/Ops/Finance/HR + Total with correct counts and %", not bad, "; ".join(bad[:3]))
    check("Attendance % shown as a percentage format",
          all("%" in ws.cell(r, h["Attendance %"]).number_format for r in range(2, 7)))
    check("count cells are formulas", all(is_formula(ws.cell(r, h[k])) for r in range(2, 7) for k in ("Headcount", "Present", "Late", "Absent")))
    att = sheet(wf, "Attendance")
    row = next(r for r in range(2, att.max_row + 1) if att.cell(r, 2).value == "E006")
    wm = lib.values_after_edit(out, lambda wb: setattr(wb["Attendance"].cell(row, 8), "value", "OK"))
    bad = grade(sheet(wm, "Summary"), {"E006": "OK"})
    check("live: marking Farid present updates Finance and the totals", not bad, "; ".join(bad[:3]))
    ok, d = no_hidden(ws)
    check("no hidden rows/columns on Summary", ok, d)


def expense_summary(out, wf, wv, fx):
    ok, d = unchanged(wf, fx, ["Ledger", "Budget"])
    check("Ledger and Budget untouched", ok, d)
    ws, wsv = sheet(wf, "Summary"), sheet(wv, "Summary")
    check("sheet 'Summary' created", ws is not None)
    if ws is None:
        return
    h = find_header(ws, SUMMARY_HEAD)
    check("headers Category/Spent/Budget/Remaining/% Used/Status", all(h.values()), str(h))
    if not all(h.values()):
        return

    def grade(wsv, ledger=None, budgets=None):
        bad = []
        bud = dict(budgets or BUDGET)
        for i, (cat, bd) in enumerate(BUDGET, 2):
            s = spent(cat, ledger)
            b = bud[cat]
            exp = [cat, s, b, round(b - s, 2), s / b, "Over budget" if s > b else "Within budget"]
            got = [wsv.cell(i, h[k]).value for k in SUMMARY_HEAD]
            ok_row = (txt(got[0]) == cat and close(got[1], exp[1]) and close(got[2], exp[2]) and close(got[3], exp[3])
                      and close(got[4], exp[4], 0.0005) and txt(got[5]).lower() == exp[5].lower())
            if not ok_row:
                bad.append(f"{cat}: {got} want {exp}")
        t = 8
        ts = round(sum(spent(c, ledger) for c, _ in BUDGET), 2)
        tb = sum(bud.values())
        if txt(wsv.cell(t, h["Category"]).value).lower() != "total" or not close(wsv.cell(t, h["Spent"]).value, ts) \
                or not close(wsv.cell(t, h["Budget"]).value, tb) or not close(wsv.cell(t, h["Remaining"]).value, tb - ts):
            bad.append(f"total row {[wsv.cell(t, h[k]).value for k in SUMMARY_HEAD]} want spent={ts} budget={tb}")
        return bad

    bad = grade(wsv)
    check("six categories in Budget order (Utilities = 0) + Total row correct", not bad, "; ".join(bad[:2]))
    check("% Used formatted as a percentage", all("%" in ws.cell(r, h["% Used"]).number_format for r in range(2, 8)))
    check("Spent and Budget cells are formulas", all(is_formula(ws.cell(r, h[k])) for r in range(2, 8) for k in ("Spent", "Budget")))

    led = LEDGER[:]
    led[0] = led[0][:3] + (led[0][3] + 100,) + led[0][4:]

    def edit(wb):
        wb["Ledger"]["D2"].value = led[0][3]
        wb["Budget"]["B3"].value = 500                     # Meals budget 450 -> 500

    bud = [(c, 500 if c == "Meals" else b) for c, b in BUDGET]
    bad = grade(sheet(lib.values_after_edit(out, edit), "Summary"), led, bud)
    check("live: changing a Ledger amount and a Budget figure flows through", not bad, "; ".join(bad[:2]))
    ok, d = no_hidden(ws)
    check("no hidden rows/columns on Summary", ok, d)


def chart_common(out, wf, fx, kind):
    ok, d = unchanged(wf, fx)
    check("sheet data untouched", ok, d)
    cs = lib.charts(out)
    check("exactly one chart in the workbook, on Summary", len(cs) == 1 and cs[0]["sheet"] == "Summary", f"{[(c['sheet'], c['type']) for c in cs]}")
    if len(cs) != 1:
        return None
    c = cs[0]
    check(f"chart is a {kind}", c["type"] == kind, str(c["type"]))
    check("chart placed clear of the table (A1:F8)", (c["col"] is not None and c["col"] >= 6) or (c["row"] is not None and c["row"] >= 8),
          f"anchor col={c['col']} row={c['row']} (0-based)")
    return c


def expense_pie(out, wf, wv, fx):
    c = chart_common(out, wf, fx, "pieChart")
    if c is None:
        return
    check("one series", len(c["series"]) == 1, str(len(c["series"])))
    s = c["series"][0] if c["series"] else {}
    check("slice labels = Category A2:A7 (Total row excluded)", s.get("cat") == ("Summary", 1, 2, 1, 7), str(s.get("cat")))
    check("slice values = Spent B2:B7 (Total row excluded)", s.get("val") == ("Summary", 2, 2, 2, 7), str(s.get("val")))
    check("title 'Spend by Category'", "spend by category" in (c["title"] or "").lower(), str(c["title"]))
    check("percentage data labels on", c["show_percent"])


def budget_bar(out, wf, wv, fx):
    c = chart_common(out, wf, fx, "barChart")
    if c is None:
        return
    check("vertical columns (barDir=col)", c["bar_dir"] == "col", str(c["bar_dir"]))
    check("clustered, not stacked", c["grouping"] in ("clustered", "standard"), str(c["grouping"]))
    check("two series", len(c["series"]) == 2, str(len(c["series"])))
    vals = sorted(s["val"] for s in c["series"] if s["val"])
    check("series values = Spent B2:B7 and Budget C2:C7", vals == [("Summary", 2, 2, 2, 7), ("Summary", 3, 2, 3, 7)], str(vals))
    check("categories = A2:A7 on every series", all(s["cat"] == ("Summary", 1, 2, 1, 7) for s in c["series"]))
    def series_name(s):
        if s["name_lit"]:
            return txt(s["name_lit"]).lower()
        ref = lib.parse_ref(s["name_ref"])
        return txt(wf[ref[0]].cell(ref[2], ref[1]).value).lower() if ref else ""

    names = sorted(series_name(s) for s in c["series"])
    check("series named Spent and Budget", names == ["budget", "spent"], str(names))
    check("title 'Budget vs Actual'", "budget vs actual" in (c["title"] or "").lower(), str(c["title"]))
    ax = c["axes"]
    check("axis titles 'Category' and 'Amount'",
          "category" in (ax.get("catAx", {}).get("title") or "").lower() and "amount" in (ax.get("valAx", {}).get("title") or "").lower(), str(ax))
    check("both axes visible (c:delete=0 written)", bool(ax) and all(not a["deleted"] for a in ax.values()), str(ax))
    check("legend shown", c["legend"])


def grid(wv, skip="Data"):
    """Find the Region x Month grid on any sheet but `skip`; return (sheet, {region:{month:v}}, rows, month_cols)."""
    for ws in wv.worksheets:
        if ws.title == skip:
            continue
        for hr in range(1, min(ws.max_row, 15) + 1):
            mc = {month_of(c.value): c.column for c in ws[hr] if not blank(c.value)}
            if all(m in mc for m in MONTHS):
                rows = {}
                for r in range(hr + 1, ws.max_row + 1):
                    for c in ws[r]:
                        if txt(c.value) in REGIONS and txt(c.value) not in rows:
                            rows[txt(c.value)] = r
                if len(rows) == 4:
                    return ws, hr, rows, mc
    return None


def sales_pivot(out, wf, wv, fx):
    ok, d = unchanged(wf, fx)
    check("Data sheet untouched", ok, d)
    with zipfile.ZipFile(out) as z:
        real = any(n.startswith("xl/pivotTables/") for n in z.namelist())
    g = grid(wv)
    check("a Region x Month grid exists on a new sheet", g is not None)
    if g is None:
        return

    def grade(wsv, hr, rows, mc, data):
        mat, bad = sales_matrix(data), []
        for reg, r in rows.items():
            for m in MONTHS:
                if not close(wsv.cell(r, mc[m]).value, mat[reg][m]):
                    bad.append(f"{reg}/{m}: {wsv.cell(r, mc[m]).value!r} want {mat[reg][m]}")
        return bad

    ws, hr, rows, mc = g
    bad = grade(ws, hr, rows, mc, sales_rows())
    check("all 12 Region x Month totals correct", not bad, "; ".join(bad[:3]))
    mat = sales_matrix()
    mcols = {mc[m] for m in MONTHS}
    rt_ok = all(any(close(ws.cell(r, c).value, sum(mat[reg].values())) for c in range(1, ws.max_column + 1) if c not in mcols)
                for reg, r in rows.items())
    check("row totals per region present and correct", rt_ok)
    below = max(rows.values()) + 1
    ct_ok = all(any(close(ws.cell(r, mc[m]).value, sum(mat[x][m] for x in REGIONS)) for r in range(below, below + 3)) for m in MONTHS)
    check("column totals per month present and correct", ct_ok)
    gt = round(sum(sum(v.values()) for v in mat.values()), 2)
    check("grand total present and correct", any(close(c.value, gt) for row in ws.iter_rows() for c in row if isinstance(c.value, (int, float))))
    if real:
        check("real PivotTable part present (static until refreshed; liveness not testable here)", True)
    else:
        rows_d = sales_rows()
        edited = rows_d[0][:5] + (rows_d[0][5] + 1000,)
        wm = lib.values_after_edit(out, lambda wb: setattr(wb["Data"]["F2"], "value", edited[5]))
        g2 = grid(wm)
        bad = grade(g2[0], g2[1], g2[2], g2[3], [edited] + rows_d[1:]) if g2 else ["grid vanished"]
        check("live: adding 1000 to a Data row changes the grid", not bad, "; ".join(bad[:2]))
    ok, d = no_hidden(ws)
    check("no hidden rows/columns on the pivot sheet", ok, d)


def sales_top3(out, wf, wv, fx):
    ok, d = unchanged(wf, fx)
    check("Data sheet untouched", ok, d)
    ws, wsv = sheet(wf, "Top Reps"), sheet(wv, "Top Reps")
    check("sheet 'Top Reps' created", ws is not None)
    if ws is None:
        return
    h = find_header(ws, ["Rank", "Rep", "Total"])
    check("headers Rank/Rep/Total", all(h.values()), str(h))
    if not all(h.values()):
        return
    top = rep_totals()[:3]

    def grade(wsv, top):
        return [f"row{i}: {[wsv.cell(i, h[k]).value for k in ('Rank', 'Rep', 'Total')]} want {i - 1},{n},{t}"
                for i, (n, t) in enumerate(top, 2)
                if not (close(wsv.cell(i, h["Rank"]).value, i - 1, 1e-9) and txt(wsv.cell(i, h["Rep"]).value) == n
                        and close(wsv.cell(i, h["Total"]).value, t))]

    bad = grade(wsv, top)
    check("top 3 reps, best first, ranks 1-3, totals correct", not bad, "; ".join(bad[:2]))
    check("no 4th row listed", all(blank(wsv.cell(5, c).value) for c in range(1, 4)))
    data = sheet(wf, "Data")
    r0 = next(r for r in range(2, data.max_row + 1) if data.cell(r, 4).value == top[0][0])

    def edit(wb):
        wb["Data"].cell(r0, 6).value = wb["Data"].cell(r0, 6).value + 500

    wm = sheet(lib.values_after_edit(out, edit), "Top Reps")
    new = [(top[0][0], round(top[0][1] + 500, 2))] + top[1:]
    bad = grade(wm, new)
    check("live: +500 on the leader's Data row moves their Total", not bad, "; ".join(bad[:2]))
    ok, d = no_hidden(ws)
    check("no hidden rows/columns", ok, d)


def messy_clean(out, wf, wv, fx):
    ok, d = unchanged(wf, fx, ["Raw"])
    check("Raw sheet exactly as it was", ok, d)
    ws = sheet(wv, "Clean")
    check("sheet 'Clean' created", ws is not None)
    if ws is None:
        return
    check("header Date/Employee/Dept/Amount", [txt(c.value) for c in ws[1]][:4] == MESSY_HEAD)
    rows = [[c.value for c in r][:4] for r in ws.iter_rows(min_row=2) if any(not blank(c.value) for c in r)]
    check("8 data rows: duplicates and blank rows gone, no gaps", len(rows) == 8 and ws.max_row == 9, f"rows={len(rows)} max_row={ws.max_row}")
    check("dates are real dates (not text)", all(isinstance(r[0], (dt.datetime, dt.date)) for r in rows), str([type(r[0]).__name__ for r in rows][:3]))
    check("date cells display as a date format", all(re.search(r"y", ws.cell(i, 1).number_format) and re.search(r"d", ws.cell(i, 1).number_format) for i in range(2, 10)))
    check("dates correct", [norm_date(r[0]) for r in rows] == [e[0] for e in CLEAN], str([norm_date(r[0]) for r in rows]))
    check("names trimmed, single-spaced, Title Case", [r[1] for r in rows] == [e[1] for e in CLEAN], str([r[1] for r in rows]))
    check("Dept normalised to Sales/Ops/Finance/HR (HR stays HR)", [r[2] for r in rows] == [e[2] for e in CLEAN], str([r[2] for r in rows]))
    check("amounts are numbers with the right values",
          all(isinstance(r[3], (int, float)) for r in rows) and all(close(r[3], e[3]) for r, e in zip(rows, CLEAN)), str([r[3] for r in rows]))
    check("first occurrence kept, original order preserved", [(norm_date(r[0]), r[1]) for r in rows] == [(e[0], e[1]) for e in CLEAN])
    ok, d = no_hidden(sheet(wf, "Clean"))
    check("no hidden rows/columns (hiding duplicates is not removing them)", ok, d)


def audit_variance(out, wf, wv, fx):
    ws, wsv = sheet(wf, "Stock Count"), sheet(wv, "Stock Count")
    ok, d = unchanged(wf, fx)
    check("original five columns untouched", ok, d)
    h = find_header(ws, ["Variance", "Variance %", "Value Impact", "Flag"])
    check("columns Variance / Variance % / Value Impact / Flag", all(h.values()), str(h))
    if not all(h.values()):
        return

    def grade(wsv, rows):
        calc, summ = audit_calc(rows)
        bad = []
        for i, (c, row) in enumerate(zip(calc, rows), 2):
            var, pct, imp, flag = c
            gv, gp, gi, gf = (wsv.cell(i, h[k]).value for k in ("Variance", "Variance %", "Value Impact", "Flag"))
            pct_ok = (txt(gp).lower() == "n/a") if pct is None else close(gp, pct, 0.0005)
            if not (close(gv, var, 1e-9) and pct_ok and close(gi, imp) and txt(gf) == flag):
                bad.append(f"{row[0]}: {[gv, gp, gi, gf]} want {[var, pct, imp, flag]}")
        sw = sheet(wsv.parent, "Audit Summary")
        if sw is None:
            bad.append("Audit Summary sheet missing")
        else:
            for lab, exp in zip(AUDIT_SUMMARY_LABELS, summ):
                r = find_row(sw, lab)
                if r is None or not close(sw.cell(r, 2).value, exp):
                    bad.append(f"summary '{lab}': {sw.cell(r, 2).value if r else 'missing'} want {exp}")
        return bad

    bad = grade(wsv, AUDIT)
    check("all 16 rows: variance, %, value impact, flag (n/a for System 0, +2.0% not flagged)", not bad, "; ".join(bad[:3]))
    check("Audit Summary figures correct", sheet(wf, "Audit Summary") is not None and not any("summary" in b.lower() for b in bad), "; ".join(bad[-2:]))
    check("Variance % shown as a percentage", all("%" in ws.cell(r, h["Variance %"]).number_format for r in range(2, 18) if AUDIT[r - 2][2] != 0))
    check("the four new columns are formulas in every row", all(is_formula(ws.cell(r, h[k])) for r in range(2, 18) for k in h))
    cf_ok = False
    for cf in ws.conditional_formatting:
        covers = any(rng.min_row <= 17 and rng.max_row >= 2 for rng in cf.sqref.ranges)
        for rule in cf.rules:
            blob = " ".join(map(str, (rule.formula or []) + [rule.text, rule.operator])).lower()
            if covers and "investigate" in blob:
                cf_ok = True
    check("conditional formatting highlights 'Investigate' rows", cf_ok)
    rows = [list(r) for r in AUDIT]
    rows[2][3] = 30                                                       # Stapler counted 40 -> 30

    def edit(wb):
        wb["Stock Count"]["D4"].value = 30

    bad = grade(sheet(lib.values_after_edit(out, edit), "Stock Count"), [tuple(r) for r in rows])
    check("live: a new Counted Qty re-flags the row and updates the summary", not bad, "; ".join(bad[:2]))


def ledger_format(out, wf, wv, fx):
    ws, fs = sheet(wf, "Ledger"), sheet(fx, "Ledger")
    ok, d = unchanged(wf, fx)
    check("every value unchanged (formatting only)", ok, d)
    hdr = [ws.cell(1, c) for c in range(1, 7)]
    check("header row bold with a fill colour",
          all(c.font.bold and c.fill.fill_type == "solid" and (c.fill.fgColor.rgb or "") not in ("00000000", "FFFFFFFF") for c in hdr))
    check("header row frozen (freeze panes at A2)", ws.freeze_panes == "A2", str(ws.freeze_panes))
    check("AutoFilter on A1:F24", ws.auto_filter.ref == "A1:F24", str(ws.auto_filter.ref))
    n = ws.max_row
    check("Date column shows dates, not 46321",
          all(re.search(r"y", ws.cell(r, 1).number_format) and re.search(r"d", ws.cell(r, 1).number_format) for r in range(2, n + 1)),
          ws.cell(2, 1).number_format)
    check("Amount column has 2 decimals", all("0.00" in ws.cell(r, 4).number_format for r in range(2, n + 1)), ws.cell(2, 4).number_format)
    bad = []
    for col, minw in ((1, 10), (2, None), (3, None), (4, 8), (5, None), (6, 6)):
        longest = max(len(txt(ws.cell(r, col).value)) for r in range(1, n + 1))
        need = minw if minw and col not in (2, 3, 5) else longest * 0.9
        w = ws.column_dimensions[openpyxl.utils.get_column_letter(col)].width or 8.43
        if w < need:
            bad.append(f"col {col}: width {w} < {need:.0f}")
    check("columns wide enough for their content", not bad, "; ".join(bad[:3]))
    check("row 1 repeats on every printed page", str(ws.print_title_rows).replace("$", "") == "1:1", str(ws.print_title_rows))
    check("landscape", ws.page_setup.orientation == "landscape", str(ws.page_setup.orientation))
    fit = ws.sheet_properties.pageSetUpPr is not None and ws.sheet_properties.pageSetUpPr.fitToPage
    check("fit to one page wide", bool(fit) and ws.page_setup.fitToWidth in (None, 1), f"fitToPage={fit} fitToWidth={ws.page_setup.fitToWidth}")


def orders_lookup(out, wf, wv, fx):
    ws, wsv = sheet(wf, "Orders"), sheet(wv, "Orders")
    ok, d = unchanged(wf, fx)
    check("original columns and sheets untouched", ok, d)
    h = find_header(ws, ["Customer", "Due Date", "Days Overdue", "Status"])
    check("columns Customer / Due Date / Days Overdue / Status", all(h.values()), str(h))
    if not all(h.values()):
        return

    def grade(wsv, asof, terms=None):
        bad = []
        cust = [(c[0], c[1], (terms or {}).get(c[0], c[2])) for c in CUSTOMERS]
        saved = lib.CUSTOMERS
        lib.CUSTOMERS = cust
        try:
            calc = orders_calc(asof)
        finally:
            lib.CUSTOMERS = saved
        for i, ((name, due, late, status), o) in enumerate(zip(calc, ORDERS), 2):
            g = [wsv.cell(i, h[k]).value for k in ("Customer", "Due Date", "Days Overdue", "Status")]
            due_ok = blank(g[1]) if due is None else norm_date(g[1]) == due
            if not (g[0] == name and due_ok and close(g[2], late, 1e-9) and g[3] == status):
                bad.append(f"{o[0]}: {g} want {[name, due, late, status]}")
        return bad

    bad = grade(wsv, ASOF)
    check("15 orders: customer, due date, days overdue, status (unknown customer, due-today boundary)", not bad, "; ".join(bad[:2]))
    check("Due Date displays as a date", all(re.search(r"y", ws.cell(r, h["Due Date"]).number_format) for r in range(2, 17) if ORDERS[r - 2][2] != "C999"))
    check("all four columns are formulas", all(is_formula(ws.cell(r, h[k])) for r in range(2, 17) for k in h))
    check("does not use TODAY() (as-of date comes from Settings)", not any("TODAY(" in str(c.value).upper() for row in ws.iter_rows() for c in row))
    asof = dt.date(2026, 11, 6)

    def edit(wb):
        wb["Settings"]["B1"].value = dt.datetime(2026, 11, 6)
        wb["Customers"]["C3"].value = 30                       # Contoso terms 14 -> 30

    bad = grade(sheet(lib.values_after_edit(out, edit), "Orders"), asof, {"C002": 30})
    check("live: new as-of date and new customer terms recompute everything", not bad, "; ".join(bad[:2]))


def expense_daily(out, wf, wv, fx):
    ok, d = unchanged(wf, fx, ["Ledger", "Budget"])
    check("Ledger and Budget untouched", ok, d)
    ws, wsv = sheet(wf, "Daily Report"), sheet(wv, "Daily Report")
    check("sheet 'Daily Report' created", ws is not None)
    if ws is None:
        return
    check("A1 'Report date', B1 a real date 2026-10-28",
          txt(ws["A1"].value).lower() == "report date" and norm_date(wsv["B1"].value) == dt.date(2026, 10, 28), repr(wsv["B1"].value))

    def grade(wsv, day):
        st, bad = day_stats(day), []
        for lab, key in (("Total spent", "total"), ("Transactions", "count"), ("Largest expense", "max"), ("Average expense", "avg")):
            r = find_row(wsv, lab)
            if r is None or not close(wsv.cell(r, 2).value, st[key]):
                bad.append(f"{lab}: {wsv.cell(r, 2).value if r else 'missing'} want {st[key]}")
        hr = find_row(wsv, "Category")
        if hr is None:
            bad.append("Category table missing")
        else:
            for i, (cat, _) in enumerate(BUDGET, hr + 1):
                if txt(wsv.cell(i, 1).value) != cat or not close(wsv.cell(i, 2).value, st["by_cat"][cat]):
                    bad.append(f"{cat}: {wsv.cell(i, 2).value!r} want {st['by_cat'][cat]}")
        return bad

    bad = grade(wsv, dt.date(2026, 10, 28))
    check("KPIs and the six-category table correct for 2026-10-28", not bad, "; ".join(bad[:3]))
    wm = lib.values_after_edit(out, lambda wb: setattr(wb["Daily Report"]["B1"], "value", dt.datetime(2026, 10, 27)))
    bad = grade(sheet(wm, "Daily Report"), dt.date(2026, 10, 27))
    check("live: changing B1 to 2026-10-27 re-computes every figure", not bad, "; ".join(bad[:3]))
    ok, d = no_hidden(ws)
    check("no hidden rows/columns", ok, d)


def log_append(out, wf, wv, fx):
    ws, wsv = sheet(wf, "Log"), sheet(wv, "Log")
    check("title still merged A1:D1 and intact", "A1:D1" in [str(r) for r in ws.merged_cells.ranges] and ws["A1"].value == LOG_TITLE)
    new = [ws.cell(10, c).value for c in range(1, 5)]
    check("new entry sits on row 10, above Total",
          norm_date(new[0]) == NEW_ENTRY[0] and new[1:3] == list(NEW_ENTRY[1:3]) and close(new[3], NEW_ENTRY[3]), str(new))
    old = [[ws.cell(r, c).value for c in range(1, 5)] for r in range(4, 10)]
    check("the six original entries unchanged", [[norm_date(a[0])] + a[1:] for a in old] == [[e[0]] + list(e[1:]) for e in LOG], str(old[:1]))
    check("Total moved to row 11", txt(ws.cell(11, 1).value).lower() == "total")
    want = round(sum(r[3] for r in LOG) + NEW_ENTRY[3], 2)
    check("Total includes the new entry", close(wsv.cell(11, 4).value, want), f"{wsv.cell(11, 4).value} want {want}")
    cs = [c for c in lib.charts(out) if c["sheet"] == "Log"]
    check("bar chart survived", len(cs) == 1 and cs[0]["type"] == "barChart", str([c["type"] for c in cs]))
    ends = [s["val"][4] for c in cs for s in c["series"] if s["val"]]
    check("chart series extended to include row 10", bool(ends) and all(e >= 10 for e in ends), str(ends))
    dvs = [dv for dv in ws.data_validations.dataValidation if dv.type == "list"]
    check("Category dropdown survived and covers C10",
          any(LOG_CATEGORIES in (dv.formula1 or "") and "C10" in dv.sqref for dv in dvs))
    cfs = [cf for cf in ws.conditional_formatting if "D10" in cf.sqref and any(r.operator == "greaterThan" for r in cf.rules)]
    check("'over 100' highlighting survived and covers D10", bool(cfs))
    dn = wf.defined_names.get("LogTotal")
    check("defined name LogTotal follows the Total cell to D11", dn is not None and dn.attr_text.replace("'", "").endswith("Log!$D$11"),
          getattr(dn, "attr_text", "missing"))


RUN = {
    "attendance-status": attendance_status, "attendance-summary": attendance_summary,
    "expense-summary": expense_summary, "expense-pie": expense_pie, "budget-bar": budget_bar,
    "sales-pivot": sales_pivot, "sales-top3": sales_top3, "messy-clean": messy_clean,
    "audit-variance": audit_variance, "ledger-format": ledger_format, "orders-lookup": orders_lookup,
    "expense-daily": expense_daily, "log-append": log_append,
}


def main():
    if len(sys.argv) != 3 or sys.argv[1] not in RUN:
        sys.exit(f"usage: check.py <{'|'.join(RUN)}> <workdir>")
    task, wd = sys.argv[1], pathlib.Path(sys.argv[2])
    out, wf, wv, fx = open_task(task, wd)
    RUN[task](out, wf, wv, fx)
    finish()


if __name__ == "__main__":
    main()
