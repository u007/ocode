#!/usr/bin/env python3
"""Structural checker for the pptx edit probe.

usage: check.py <task> <workdir>
  workdir holds the original fixture (must be byte-identical to the pristine
  copy) and the model's output deck_out.pptx.
PowerPoint is not available, so every check reads the saved XML: cover-ups
(hidden or off-slide shapes, white boxes, stale media) and schema breakage fail.
Prints one line per check (PASS/FAIL) and a final score line.
"""
import hashlib
import pathlib
import re
import sys
import zipfile

from lxml import etree
from pptx import Presentation
from pptx.oxml.ns import qn
from pptx.util import Inches

HERE = pathlib.Path(__file__).resolve().parent
FIX = HERE / "fixtures"
HEAD_FILL, BAND_FILL = "1F3B73", "E8EEF8"
M = Inches(0.6)
TOL = 12700          # 1pt
BASE_ROWS = [
    ["Widget A", "2", "10.00", "20.00"],
    ["Widget B", "5", "4.50", "22.50"],
    ["Widget C", "1", "99.00", "99.00"],
    ["Gadget D", "3", "12.25", "36.75"],
    ["Gadget E", "10", "1.10", "11.00"],
    ["Service F", "1", "250.00", "250.00"],
    ["Service G", "4", "30.00", "120.00"],
]
HEADER = ["Item", "Qty", "Unit Price", "Amount"]
SKUS = [f"SKU-00{i}" for i in range(1, 8)]
OTHER_TEXT = [
    "Invoice INV-1042", "Bill to: Acme Pty Ltd, 1 Example Street · Date: 2026-09-01",
    "Notes: Payment due within 30 days. Bank transfer only.", "Thank you for your business.",
]

results = []


def check(name, ok, detail=""):
    results.append(bool(ok))
    print(("PASS " if ok else "FAIL ") + name + (f"  [{detail}]" if detail and not ok else ""))


def sha(b):
    return hashlib.sha256(b).hexdigest()


def canon(b):
    """Canonical XML: a re-serialised but unchanged part compares equal."""
    return etree.tostring(etree.fromstring(b), method="c14n")


def expected(task):
    rows = [r[:] for r in BASE_ROWS]
    if task == "delete-row":
        rows = [r for r in rows if r[0] != "Widget C"]
    elif task == "edit-cell":
        rows[1] = ["Widget B", "12", "4.50", "54.00"]
    elif task == "add-row":
        rows.append(["Widget H", "2", "15.00", "30.00"])
    elif task == "rename-item":
        rows[3][0] = "Quokka Kit"
    tot = ["Total", f"{sum(float(r[3]) for r in rows):.2f}"]
    if task == "add-column":
        return [HEADER[:1] + ["SKU"] + HEADER[1:]] + [r[:1] + [s] + r[1:] for r, s in zip(rows, SKUS)] + [tot]
    return [HEADER] + rows + [tot]


def origins(tr):
    """a:tc elements that are not covered by a horizontal/vertical merge."""
    return [tc for tc in tr.tc_lst if tc.get("hMerge") != "1" and tc.get("vMerge") != "1"]


def tc_text(tc):
    return "".join(t.text or "" for t in tc.iter(qn("a:t"))).strip()


def tc_fill(tc):
    tcPr = tc.find(qn("a:tcPr"))
    c = tcPr.find(qn("a:solidFill") + "/" + qn("a:srgbClr")) if tcPr is not None else None
    v = c.get("val").upper() if c is not None else None
    return None if v in (None, "FFFFFF") else v


def run_props(r):
    rpr = r.find(qn("a:rPr"))
    latin = rpr.find(qn("a:latin")) if rpr is not None else None
    col = rpr.find(qn("a:solidFill") + "/" + qn("a:srgbClr")) if rpr is not None else None
    return {
        "bold": rpr is not None and rpr.get("b") == "1",
        "white": col is not None and col.get("val").upper() == "FFFFFF",
        "size": int(rpr.get("sz")) / 100 if rpr is not None and rpr.get("sz") else None,
        "font": latin.get("typeface") if latin is not None else None,
    }


def box(sh):
    return (sh.left, sh.top, sh.left + sh.width, sh.top + sh.height)


def overlap(a, b):
    return min(a[2], b[2]) - max(a[0], b[0]) > TOL and min(a[3], b[3]) - max(a[1], b[1]) > TOL


def pic_sha(sh):
    return sha(sh.image.blob)


def main():
    task, wd = sys.argv[1], pathlib.Path(sys.argv[2])
    orig, out = wd / "deck.pptx", wd / "deck_out.pptx"
    check("original unchanged", orig.exists() and sha(orig.read_bytes()) == sha((FIX / "deck.pptx").read_bytes()))
    if not out.exists():
        check("output exists", False)
        return finish()
    try:
        prs = Presentation(str(out))
        z = zipfile.ZipFile(out)
    except Exception as e:  # a corrupt output is a scored failure, not a crash
        check("output opens", False, f"{type(e).__name__}: {e}")
        return finish()
    fz = zipfile.ZipFile(FIX / "deck.pptx")
    logo, new_logo, sig = (sha((FIX / f).read_bytes()) for f in ("logo.png", "new_logo.png", "signature.png"))
    SW, SH = prs.slide_width, prs.slide_height
    check("two slides", len(prs.slides) == 2, f"{len(prs.slides)}")
    s1 = prs.slides[0]
    shapes = list(s1.shapes)

    # --- structure
    bad = []
    for sh in shapes:
        if not sh.has_table:
            continue
        tbl = sh.table._tbl
        ng = len(tbl.tblGrid.findall(qn("a:gridCol")))
        for ri, tr in enumerate(tbl.tr_lst):
            if len(tr.tc_lst) != ng:
                bad.append(f"r{ri}:{len(tr.tc_lst)}!={ng} tc")
            for tc in tr.tc_lst:
                tb = tc.find(qn("a:txBody"))
                if tb is None or tb.find(qn("a:p")) is None:
                    bad.append(f"r{ri}: tc without txBody/p")
            ci = 0
            tcs = tr.tc_lst
            while ci < len(tcs):
                span = int(tcs[ci].get("gridSpan", "1"))
                if any(tc.get("hMerge") != "1" for tc in tcs[ci + 1:ci + span]):
                    bad.append(f"r{ri}c{ci}: gridSpan {span} not covered by hMerge")
                ci += span
    check("table XML: tc per gridCol, txBody, spans", not bad, "; ".join(bad[:4]))
    ids = [e.get("id") for e in s1._element.iter(qn("p:cNvPr"))]
    check("unique shape ids on slide 1", len(ids) == len(set(ids)), f"{ids}")
    hidden = [e.get("name") for e in s1._element.iter(qn("p:cNvPr")) if e.get("hidden") in ("1", "true")]
    check("no hidden shapes", not hidden, f"{hidden}")
    off = [sh.name for sh in shapes if sh.left < 0 or sh.top < 0 or sh.left + sh.width > SW + TOL or sh.top + sh.height > SH + TOL]
    check("every shape inside the slide", not off, f"{off}")
    dang = [rid for rid in (b.get(qn("r:embed")) for b in s1._element.iter(qn("a:blip"))) if rid not in s1.part.rels]
    check("no dangling image rIds", not dang, f"{dang}")
    ov = [(a.name, b.name) for i, a in enumerate(shapes) for b in shapes[i + 1:] if overlap(box(a), box(b))]
    check("no overlapping shapes on slide 1", not ov, f"{ov}")

    # --- the invoice table
    tables = [sh for sh in shapes if sh.has_table]
    inv = next((sh for sh in tables if tc_text(sh.table._tbl.tr_lst[0].tc_lst[0]) == "Item"), None)
    if inv is None:
        check("invoice table found", False)
        return finish()
    tbl = inv.table._tbl
    got = [[tc_text(tc) for tc in origins(tr)] for tr in tbl.tr_lst]
    want = expected(task)
    check("table rows exact", got == want, f"got {got}")
    rows_h = sum(int(tr.get("h")) for tr in tbl.tr_lst)
    check("frame height = sum of row heights", abs(inv.height - rows_h) <= TOL, f"{inv.height} vs {rows_h}")
    cols_w = sum(int(g.get("w")) for g in tbl.tblGrid.findall(qn("a:gridCol")))
    check("frame width = sum of column widths", abs(inv.width - cols_w) <= TOL, f"{inv.width} vs {cols_w}")
    check("table within the slide margins", inv.left >= M - TOL and inv.left + cols_w <= SW - M + TOL,
          f"left={inv.left} right={inv.left + cols_w} max={SW - M}")

    hdr = origins(tbl.tr_lst[0])
    runs = [run_props(r) for tc in hdr for r in tc.iter(qn("a:r")) if (r.findtext(qn("a:t")) or "").strip()]
    check("header cells: same dark fill + bold white runs",
          all(tc_fill(tc) == HEAD_FILL for tc in hdr) and runs and all(x["bold"] and x["white"] for x in runs),
          f"fills={[tc_fill(tc) for tc in hdr]} runs={runs}")
    band = []
    body = tbl.tr_lst[1:-1]
    for i, tr in enumerate(body):
        fs = {tc_fill(tc) for tc in origins(tr)}
        if fs != {BAND_FILL if i % 2 == 1 else None}:
            band.append(f"{i}:{sorted(map(str, fs))}")
    check("banded fills alternate across all body rows", not band, ",".join(band))
    fmt, align = [], []
    for tr in body:
        cs = origins(tr)
        for tc in cs:
            for r in tc.iter(qn("a:r")):
                if (r.findtext(qn("a:t")) or "").strip():
                    rp = run_props(r)
                    if rp["size"] != 12 or rp["font"] != "Arial" or rp["bold"] or rp["white"]:
                        fmt.append(f"{r.findtext(qn('a:t'))}:{rp}")
        for tc in cs[-3:]:
            p = tc.find(qn("a:txBody")).find(qn("a:p"))
            ppr = p.find(qn("a:pPr"))
            if ppr is None or ppr.get("algn") != "r":
                align.append(tc_text(tc))
    check("body runs keep Arial 12pt regular", not fmt, "; ".join(fmt[:3]))
    check("numbers stay right-aligned", not align, ",".join(align[:4]))
    if task == "add-column":
        lab = tbl.tr_lst[-1].tc_lst[0]
        check("Total label spans every column before Amount", lab.get("gridSpan") == "4", f"{lab.get('gridSpan')}")

    # --- old text gone from every slide part
    gone = {"delete-row": "Widget C", "rename-item": "Gadget D"}.get(task)
    if gone:
        where = [n for n in z.namelist() if n.startswith("ppt/") and n.endswith(".xml")
                 and gone in "".join(re.findall(r"<a:t>([^<]*)</a:t>", z.read(n).decode("utf8", "replace")))]
        check(f'"{gone}" absent from every XML part', not where, ",".join(where))

    # --- rest unchanged
    texts = [sh.text_frame.text.strip() for sh in shapes if sh.has_text_frame and sh.text_frame.text.strip()]
    want_t = OTHER_TEXT + (["Payment schedule"] if task == "insert-table" else [])
    check("other text boxes unchanged", sorted(texts) == sorted(want_t), f"got {texts}")
    check("slide 2 unchanged", canon(z.read("ppt/slides/slide2.xml")) == canon(fz.read("ppt/slides/slide2.xml")))
    s2pics = [pic_sha(sh) for sh in prs.slides[1].shapes if sh.shape_type == 13]
    check("slide 2 logo is still the old logo", s2pics == [logo], f"{s2pics}")

    # --- images on slide 1
    pics = [sh for sh in shapes if sh.shape_type == 13]
    fpics = [sh for sh in Presentation(str(FIX / "deck.pptx")).slides[0].shapes if sh.shape_type == 13]
    logos = [sh for sh in pics if pic_sha(sh) in (logo, new_logo)]
    if task == "replace-image":
        check("slide 1 logo is new_logo.png", [pic_sha(sh) for sh in logos] == [new_logo], f"{[pic_sha(sh)[:8] for sh in logos]}")
        check("logo position and size unchanged", len(logos) == 1 and all(abs(a - b) <= TOL for a, b in zip(box(logos[0]), box(fpics[0]))))
        stale = [r.rId for r in s1.part.rels.values() if "image" in r.reltype and sha(r.target_part.blob) == logo]
        check("slide 1 holds no rel to the old logo", not stale, f"{stale}")
    else:
        check("slide 1 logo untouched", [pic_sha(sh) for sh in logos] == [logo] and box(logos[0]) == box(fpics[0]))
    sigs = [sh for sh in pics if pic_sha(sh) == sig]
    if task == "insert-image":
        check("signature inserted once", len(sigs) == 1, f"{len(sigs)}")
        if sigs:
            s = sigs[0]
            footer = next(sh for sh in shapes if sh.has_text_frame and sh.text_frame.text.startswith("Thank you"))
            check("signature 2in wide", abs(s.width - Inches(2)) <= Inches(2) * 0.01, f"{s.width}")
            check("signature aspect kept", abs(s.height / s.width - 100 / 300) < 0.01, f"{s.height / s.width:.3f}")
            check("right edge = table right edge", abs(s.left + s.width - (inv.left + inv.width)) <= Inches(0.02),
                  f"{s.left + s.width} vs {inv.left + inv.width}")
            check("above the footer", s.top + s.height <= footer.top + TOL)
    else:
        check("no image added", not sigs and len(pics) == len(fpics))

    if task == "insert-table":
        new = [sh for sh in tables if sh is not inv]
        check("one new table", len(new) == 1, f"{len(new)}")
        if len(new) == 1:
            nt = new[0]
            cells = [[tc_text(tc) for tc in origins(tr)] for tr in nt.table._tbl.tr_lst]
            check("new table content exact", cells == [["Due date", "Amount"], ["2026-10-01", "279.63"], ["2026-11-01", "279.62"]], f"{cells}")
            notes = next(sh for sh in shapes if sh.has_text_frame and sh.text_frame.text.startswith("Notes:"))
            head = next((sh for sh in shapes if sh.has_text_frame and sh.text_frame.text.strip() == "Payment schedule"), None)
            check("heading below Notes, table below heading", head is not None and head.top >= notes.top + notes.height - TOL
                  and nt.top >= head.top + head.height - TOL)
            ntbl = nt.table._tbl
            sid = ntbl.tblPr.findtext(qn("a:tableStyleId")) if ntbl.tblPr is not None else None
            lines = any(ntbl.iter(qn("a:lnB")))
            check("new table has a style or cell borders", bool(sid) or lines)
            nh = sum(int(tr.get("h")) for tr in ntbl.tr_lst)
            check("new table frame height = sum of rows", abs(nt.height - nh) <= TOL, f"{nt.height} vs {nh}")
    finish()


def finish():
    n = sum(results)
    print(f"SCORE {n}/{len(results)} {'PASS' if n == len(results) else 'FAIL'}")
    sys.exit(0)


if __name__ == "__main__":
    main()
