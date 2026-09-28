#!/usr/bin/env python3
"""Reference edits for the checker self-test.

usage: ref.py good|cover <task> <workdir>
  good  = a correct python-docx solution (must PASS check.py)
  cover = a cover-up / naive variant (must FAIL check.py)
"""
import copy
import hashlib
import pathlib
import sys

import docx
from docx.enum.text import WD_ALIGN_PARAGRAPH
from docx.oxml.ns import qn
from docx.shared import Inches

HERE = pathlib.Path(__file__).resolve().parent
FIX = HERE / "fixtures"


def uniq(row):
    seen, out = set(), []
    for c in row.cells:
        if id(c._tc) not in seen:
            seen.add(id(c._tc))
            out.append(c)
    return out


def set_text(cell, text):
    """Keep the first run's formatting, drop the other runs."""
    p = cell.paragraphs[0]
    runs = p.runs
    runs[0].text = text
    for r in runs[1:]:
        r._r.getparent().remove(r._r)


def set_fill(cell, fill):
    tcPr = cell._tc.get_or_add_tcPr()
    for s in tcPr.findall(qn("w:shd")):
        tcPr.remove(s)
    if fill:
        tcPr.append(tcPr.makeelement(qn("w:shd"), {qn("w:val"): "clear", qn("w:color"): "auto", qn("w:fill"): fill}))


def reband(t):
    for i, r in enumerate(t.rows[1:-1]):
        for c in uniq(r):
            set_fill(c, "E8EEF8" if i % 2 == 1 else None)


def fix_total(t):
    tot = sum(float(uniq(r)[-1].text) for r in t.rows[1:-1])
    set_text(uniq(t.rows[-1])[-1], f"{tot:.2f}")


def row_by(t, name):
    [r] = [r for r in t.rows if uniq(r)[0].text.strip() == name]
    return r


def good(task, d):
    t = d.tables[0]
    if task == "delete-row":
        tr = row_by(t, "Widget C")._tr
        tr.getparent().remove(tr)
        reband(t)
        fix_total(t)
    elif task == "edit-cell":
        cs = uniq(row_by(t, "Widget B"))
        set_text(cs[1], "12")
        set_text(cs[3], "54.00")
        fix_total(t)
    elif task == "rename-item":
        set_text(uniq(row_by(t, "Gadget D"))[0], "Quokka Kit")
    elif task == "add-row":
        src = row_by(t, "Service G")._tr
        new = copy.deepcopy(src)
        src.addnext(new)
        cs = uniq(t.rows[8])
        for c, v in zip(cs, ["Widget H", "2", "15.00", "30.00"]):
            set_text(c, v)
        reband(t)
        fix_total(t)
    elif task == "add-column":
        widths = [Inches(2.0), Inches(1.0), Inches(0.9), Inches(1.3), Inches(1.3)]
        g0 = t._tbl.tblGrid.findall(qn("w:gridCol"))[0]
        g0.addnext(copy.deepcopy(g0))
        for g, w in zip(t._tbl.tblGrid.findall(qn("w:gridCol")), widths):
            g.set(qn("w:w"), str(w.twips))
        skus = [f"SKU-00{i}" for i in range(1, 8)]
        for ri, row in enumerate(t.rows):
            tcs = row._tr.tc_lst
            if ri == len(t.rows) - 1:
                tcs[0].tcPr.find(qn("w:gridSpan")).set(qn("w:val"), "4")
                continue
            new = copy.deepcopy(tcs[0])
            tcs[0].addnext(new)
        for ri, row in enumerate(t.rows[:-1]):
            cs = uniq(row)
            set_text(cs[1], "SKU" if ri == 0 else skus[ri - 1])
            for c, w in zip(cs, widths):
                c.width = w
        cs = uniq(t.rows[-1])
        cs[0].width = sum(widths[:4])
        cs[1].width = widths[4]
    elif task == "insert-table":
        notes = next(p for p in d.paragraphs if p.text.startswith("Notes:"))
        approved = next(p for p in d.paragraphs if p.text == "Approved by:")
        head = approved.insert_paragraph_before("Payment schedule")
        nt = d.add_table(rows=3, cols=2)
        nt.style = "Table Grid"
        for r, vals in zip(nt.rows, [("Due date", "Amount"), ("2026-10-01", "279.63"), ("2026-11-01", "279.62")]):
            for c, v in zip(r.cells, vals):
                c.text = v
        head._p.addnext(nt._tbl)
        assert notes._p.getnext() is head._p
    elif task == "replace-image":
        hp = d.sections[0].header.part
        [blip] = list(hp._element.iter(qn("a:blip")))
        old = blip.get(qn("r:embed"))
        rid, _ = hp.get_or_add_image(str(FIX / "new_logo.png"))
        blip.set(qn("r:embed"), rid)
        del hp.rels[old]
    elif task == "insert-image":
        approved = next(p for p in d.paragraphs if p.text == "Approved by:")
        after = approved._p.getnext()
        p = docx.text.paragraph.Paragraph(after, approved._parent).insert_paragraph_before()
        p.alignment = WD_ALIGN_PARAGRAPH.RIGHT
        p.add_run().add_picture(str(FIX / "signature.png"), width=Inches(1.5))


def cover(task, d):
    """Naive/cover-up variants that a correct checker must reject."""
    t = d.tables[0]
    if task == "delete-row":            # hide instead of delete
        for c in uniq(row_by(t, "Widget C")):
            for r in c.paragraphs[0].runs:
                r.font.hidden = True
        fix_total(t)
    elif task in ("edit-cell", "rename-item"):   # cell.text drops run formatting
        if task == "edit-cell":
            cs = uniq(row_by(t, "Widget B"))
            cs[1].text, cs[3].text = "12", "54.00"
            fix_total(t)
        else:
            uniq(row_by(t, "Gadget D"))[0].text = "Quokka Kit"
    elif task == "add-row":             # add_row(): bottom + unformatted
        r = t.add_row()
        for c, v in zip(r.cells, ["Widget H", "2", "15.00", "30.00"]):
            c.text = v
        fix_total(t)
    elif task == "add-column":          # add_column(): far right, overflows
        t.add_column(Inches(1.2))
    elif task == "insert-table":        # add_table(): lands at the end
        d.add_paragraph("Payment schedule")
        nt = d.add_table(rows=1, cols=2)
        nt.style = "Table Grid"
    elif task == "replace-image":       # blob swap: footer badge changes too
        hp = d.sections[0].header.part
        [blip] = list(hp._element.iter(qn("a:blip")))
        hp.rels[blip.get(qn("r:embed"))].target_part._blob = (FIX / "new_logo.png").read_bytes()
    elif task == "insert-image":        # doc.add_picture(): end of document
        d.add_picture(str(FIX / "signature.png"), width=Inches(1.5))


if __name__ == "__main__":
    mode, task, wd = sys.argv[1], sys.argv[2], pathlib.Path(sys.argv[3])
    d = docx.Document(str(wd / "invoice.docx"))
    (good if mode == "good" else cover)(task, d)
    d.save(str(wd / "invoice_out.docx"))
