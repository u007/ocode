#!/usr/bin/env python3
"""Reference edits for the checker self-test.

usage: ref.py good|cover <task> <workdir>
  good  = a correct python-pptx solution (must PASS check.py)
  cover = a cover-up / naive variant (must FAIL check.py)
"""
import copy
import pathlib
import sys

from pptx import Presentation
from pptx.dml.color import RGBColor
from pptx.oxml.ns import qn
from pptx.util import Inches, Pt

HERE = pathlib.Path(__file__).resolve().parent
FIX = HERE / "fixtures"
TXT = lambda tc: "".join(t.text or "" for t in tc.iter(qn("a:t"))).strip()


def set_text(tc, text):
    """Keep the first run's formatting, drop the other runs."""
    p = tc.find(qn("a:txBody")).find(qn("a:p"))
    rs = p.findall(qn("a:r"))
    rs[0].find(qn("a:t")).text = text
    for r in rs[1:]:
        p.remove(r)


def set_fill(tc, hexfill):
    tcPr = tc.find(qn("a:tcPr"))
    tcPr.find(qn("a:solidFill") + "/" + qn("a:srgbClr")).set("val", hexfill)


def parts(prs):
    s = prs.slides[0]
    inv = next(sh for sh in s.shapes if sh.has_table)
    return s, inv, inv.table._tbl


def row_by(tbl, name):
    [tr] = [tr for tr in tbl.tr_lst if TXT(tr.tc_lst[0]) == name]
    return tr


def reband(tbl):
    for i, tr in enumerate(tbl.tr_lst[1:-1]):
        for tc in tr.tc_lst:
            set_fill(tc, "E8EEF8" if i % 2 == 1 else "FFFFFF")


def fix_total(tbl):
    tot = sum(float(TXT(tr.tc_lst[-1])) for tr in tbl.tr_lst[1:-1])
    set_text(tbl.tr_lst[-1].tc_lst[-1], f"{tot:.2f}")


def fit_height(inv, tbl):
    inv.height = sum(int(tr.get("h")) for tr in tbl.tr_lst)


def shape_named(s, prefix):
    return next(sh for sh in s.shapes if sh.has_text_frame and sh.text_frame.text.startswith(prefix))


def good(task, prs):
    s, inv, tbl = parts(prs)
    if task == "delete-row":
        tbl.remove(row_by(tbl, "Widget C"))
        reband(tbl)
        fix_total(tbl)
        fit_height(inv, tbl)
    elif task == "edit-cell":
        tr = row_by(tbl, "Widget B")
        set_text(tr.tc_lst[1], "12")
        set_text(tr.tc_lst[3], "54.00")
        fix_total(tbl)
    elif task == "rename-item":
        set_text(row_by(tbl, "Gadget D").tc_lst[0], "Quokka Kit")
    elif task == "add-row":
        src = row_by(tbl, "Service G")
        new = copy.deepcopy(src)
        src.addnext(new)
        for tc, v in zip(new.tc_lst, ["Widget H", "2", "15.00", "30.00"]):
            set_text(tc, v)
        reband(tbl)
        fix_total(tbl)
        fit_height(inv, tbl)
        notes = shape_named(s, "Notes:")
        notes.top = inv.top + inv.height + Inches(0.15)
    elif task == "add-column":
        widths = [Inches(3.733), Inches(1.6), Inches(1.6), Inches(2.4), Inches(2.8)]
        g = tbl.tblGrid.findall(qn("a:gridCol"))
        g[0].addnext(copy.deepcopy(g[0]))
        for gc, w in zip(tbl.tblGrid.findall(qn("a:gridCol")), widths):
            gc.set("w", str(w))
        skus = [f"SKU-00{i}" for i in range(1, 8)]
        for ri, tr in enumerate(tbl.tr_lst):
            tcs = tr.tc_lst
            if ri == len(tbl.tr_lst) - 1:
                tcs[0].set("gridSpan", "4")
                tcs[1].addnext(copy.deepcopy(tcs[1]))       # one more hMerge cell
                continue
            new = copy.deepcopy(tcs[0])
            tcs[0].addnext(new)
            set_text(new, "SKU" if ri == 0 else skus[ri - 1])
        inv.width = sum(widths)
    elif task == "insert-table":
        notes = shape_named(s, "Notes:")
        y = notes.top + notes.height
        tb = s.shapes.add_textbox(notes.left, y, Inches(4), Inches(0.35))
        r = tb.text_frame.paragraphs[0].add_run()
        r.text, r.font.size, r.font.bold = "Payment schedule", Pt(12), True
        gf = s.shapes.add_table(3, 2, notes.left, y + Inches(0.4), Inches(4), Inches(0.28) * 3)
        for i, vals in enumerate([("Due date", "Amount"), ("2026-10-01", "279.63"), ("2026-11-01", "279.62")]):
            gf.table.rows[i].height = Inches(0.28)
            for j, v in enumerate(vals):
                c = gf.table.cell(i, j)
                c.text = v
                c.text_frame.paragraphs[0].runs[0].font.size = Pt(11)
    elif task == "replace-image":
        [old] = [sh for sh in s.shapes if sh.shape_type == 13]
        new = s.shapes.add_picture(str(FIX / "new_logo.png"), old.left, old.top, old.width, old.height)
        old._element.addnext(new._element)
        rid = old._element.blip_rId
        old._element.getparent().remove(old._element)
        if not s._element.xpath(f'.//@r:embed[.="{rid}"]'):
            s.part.drop_rel(rid)
    elif task == "insert-image":
        foot = shape_named(s, "Thank you")
        w = Inches(2)
        h = int(w * 100 / 300)
        s.shapes.add_picture(str(FIX / "signature.png"), inv.left + inv.width - w, foot.top - h - Inches(0.1), width=w)


def cover(task, prs):
    """Naive/cover-up variants that a correct checker must reject."""
    s, inv, tbl = parts(prs)
    if task == "delete-row":            # remove the row but leave the frame height stale
        tbl.remove(row_by(tbl, "Widget C"))
        fix_total(tbl)
    elif task == "edit-cell":           # cell.text resets run formatting
        tr = row_by(tbl, "Widget B")
        inv.table.cell(2, 1).text, inv.table.cell(2, 3).text = "12", "54.00"
        fix_total(tbl)
    elif task == "rename-item":         # white box over the old text + new textbox
        c = inv.table.cell(4, 0)
        box = s.shapes.add_shape(1, inv.left, inv.top + Inches(0.36) * 4, Inches(3), Inches(0.36))
        box.fill.solid()
        box.fill.fore_color.rgb = RGBColor(0xFF, 0xFF, 0xFF)
        box.text_frame.text = "Quokka Kit"
    elif task == "add-row":             # row added but Notes left overlapped
        src = row_by(tbl, "Service G")
        new = copy.deepcopy(src)
        src.addnext(new)
        for tc, v in zip(new.tc_lst, ["Widget H", "2", "15.00", "30.00"]):
            set_text(tc, v)
        reband(tbl)
        fix_total(tbl)
        fit_height(inv, tbl)
    elif task == "add-column":          # widths not rebalanced: runs off the slide
        g = tbl.tblGrid.findall(qn("a:gridCol"))
        g[0].addnext(copy.deepcopy(g[0]))
        for ri, tr in enumerate(tbl.tr_lst):
            tcs = tr.tc_lst
            new = copy.deepcopy(tcs[0])
            tcs[0].addnext(new)
        inv.width = sum(int(x.get("w")) for x in tbl.tblGrid.findall(qn("a:gridCol")))
    elif task == "insert-table":        # dropped on top of existing content
        gf = s.shapes.add_table(3, 2, inv.left, inv.top, Inches(4), Inches(0.84))
    elif task == "replace-image":       # blob swap: slide 2 logo changes too
        [old] = [sh for sh in s.shapes if sh.shape_type == 13]
        s.part.related_part(old._element.blip_rId)._blob = (FIX / "new_logo.png").read_bytes()
    elif task == "insert-image":        # width AND height given: stretched
        foot = shape_named(s, "Thank you")
        s.shapes.add_picture(str(FIX / "signature.png"), inv.left + inv.width - Inches(2), foot.top - Inches(1.1),
                             Inches(2), Inches(1))


if __name__ == "__main__":
    mode, task, wd = sys.argv[1], sys.argv[2], pathlib.Path(sys.argv[3])
    prs = Presentation(str(wd / "deck.pptx"))
    (good if mode == "good" else cover)(task, prs)
    prs.save(str(wd / "deck_out.pptx"))
