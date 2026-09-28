#!/usr/bin/env python3
"""Structural checker for the docx edit probe.

usage: check.py <task> <workdir>
  workdir holds the original fixture (must be byte-identical to the pristine
  copy) and the model's output invoice_out.docx.
Word is not available, so every check reads the saved XML: cover-ups (hidden
runs, white text, zero-height rows, stale media) and schema breakage fail.
Prints one line per check (PASS/FAIL) and a final score line.
"""
import hashlib
import pathlib
import sys
import zipfile

import docx
from lxml import etree
from docx.oxml.ns import qn
from docx.shared import Inches

HERE = pathlib.Path(__file__).resolve().parent
FIX = HERE / "fixtures"
HEAD_FILL, BAND_FILL = "1F3B73", "E8EEF8"
TEXT_W = Inches(6.5).twips
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
OTHER_PARAS = [
    "Invoice INV-1042", "Bill to: Acme Pty Ltd, 1 Example Street", "Date: 2026-09-01",
    "Notes: Payment due within 30 days. Bank transfer only.", "Approved by:",
    "Thank you for your business.", "Terms and Conditions",
    "1. Goods remain the property of the seller until paid in full.",
    "2. Late payments incur interest at 2% per month.",
    "3. Disputes must be raised within 14 days of the invoice date.",
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


def total_row(rows):
    return ["Total", f"{sum(float(r[3]) for r in rows):.2f}"]


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
    tot = total_row(rows)
    if task == "add-column":
        return [HEADER[:1] + ["SKU"] + HEADER[1:]] + [r[:1] + [s] + r[1:] for r, s in zip(rows, SKUS)] + [tot]
    return [HEADER] + rows + [tot]


def uniq_cells(row):
    seen, out = set(), []
    for c in row.cells:
        if id(c._tc) not in seen:
            seen.add(id(c._tc))
            out.append(c)
    return out


def fill(tc):
    shd = tc.tcPr.find(qn("w:shd")) if tc.tcPr is not None else None
    f = shd.get(qn("w:fill")) if shd is not None else None
    return None if f in (None, "auto", "FFFFFF") else f.upper()


def run_props(r):
    rpr = r._r.rPr
    fonts = rpr.find(qn("w:rFonts")) if rpr is not None else None
    return {
        "bold": bool(r.bold),
        "white": r.font.color is not None and r.font.color.type is not None and str(r.font.color.rgb) == "FFFFFF",
        "size": r.font.size.pt if r.font.size else None,
        "font": fonts.get(qn("w:ascii")) if fonts is not None else None,
    }


def invoice_table(d):
    for t in d.tables:
        if t.rows and uniq_cells(t.rows[0])[0].text.strip() == "Item":
            return t
    return None


def xml_texts(z):
    """Concatenated w:t + w:delText + w:instrText text per XML part."""
    import re
    out = {}
    for n in z.namelist():
        if n.endswith(".xml") and n.startswith("word/"):
            x = z.read(n).decode("utf8", "replace")
            out[n] = "".join(re.findall(r"<w:(?:t|delText|instrText)(?:\s[^>]*)?>([^<]*)</w:", x))
    return out


def blip_shas(part):
    shas = []
    for b in part._element.iter(qn("a:blip")):
        rid = b.get(qn("r:embed"))
        shas.append(sha(part.rels[rid].target_part.blob) if rid in part.rels else "DANGLING")
    return shas


def extents(part):
    return [(int(e.get("cx")), int(e.get("cy"))) for e in part._element.iter(qn("wp:extent"))]


def main():
    task, wd = sys.argv[1], pathlib.Path(sys.argv[2])
    orig, out = wd / "invoice.docx", wd / "invoice_out.docx"
    check("original unchanged", orig.exists() and sha(orig.read_bytes()) == sha((FIX / "invoice.docx").read_bytes()))
    if not out.exists():
        check("output exists", False)
        return finish()
    try:
        d = docx.Document(str(out))
        z = zipfile.ZipFile(out)
    except Exception as e:  # a corrupt output is a scored failure, not a crash
        check("output opens", False, f"{type(e).__name__}: {e}")
        return finish()
    body = d.element.body
    logo, new_logo, sig = (sha((FIX / f).read_bytes()) for f in ("logo.png", "new_logo.png", "signature.png"))

    # --- structure (things Word rejects or that are cover-ups)
    bad_tc = sum(1 for tc in body.iter(qn("w:tc")) if tc.find(qn("w:p")) is None)
    check("every w:tc has a w:p", bad_tc == 0, f"{bad_tc} empty")
    grid_bad = []
    for ti, tbl in enumerate(body.iter(qn("w:tbl"))):
        ng = len(tbl.tblGrid.findall(qn("w:gridCol")))
        for ri, tr in enumerate(tbl.tr_lst):
            span = sum(tc.grid_span for tc in tr.tc_lst)
            if span != ng:
                grid_bad.append(f"t{ti}r{ri}:{span}!={ng}")
    check("gridCol count = cells per row (spans counted)", not grid_bad, ",".join(grid_bad[:4]))
    vanish = sum(1 for _ in d.element.iter(qn("w:vanish")))
    check("no hidden (w:vanish) runs", vanish == 0, f"{vanish}")
    zero = [h for h in body.iter(qn("w:trHeight")) if int(h.get(qn("w:val"), "1") or 1) < 100]
    check("no collapsed rows", not zero)
    ids = [p.get("id") for p in d.element.iter(qn("wp:docPr"))]
    check("unique drawing ids in body", len(ids) == len(set(ids)), f"{ids}")

    # --- the invoice table
    t = invoice_table(d)
    if t is None:
        check("invoice table found", False)
        return finish()
    grid = [int(g.get(qn("w:w"))) for g in t._tbl.tblGrid.findall(qn("w:gridCol"))]
    check("table grid within margins", sum(grid) <= TEXT_W * 1.005, f"{sum(grid)} > {TEXT_W}")
    over = []
    for ri, row in enumerate(t.rows):
        ws = [c._tc.tcPr.find(qn("w:tcW")) for c in uniq_cells(row) if c._tc.tcPr is not None]
        tw = sum(int(w.get(qn("w:w"))) for w in ws if w is not None and w.get(qn("w:type")) in ("dxa", None))
        if tw > TEXT_W * 1.005:
            over.append(f"r{ri}:{tw}")
    check("cell widths (tcW) within margins", not over, ",".join(over[:3]))
    got = [[c.text.strip() for c in uniq_cells(r)] for r in t.rows]
    want = expected(task)
    check("table rows exact", got == want, f"got {got}")

    hdr = uniq_cells(t.rows[0])
    hdr_ok = all(fill(c._tc) == HEAD_FILL for c in hdr)
    runs = [run_props(r) for c in hdr for p in c.paragraphs for r in p.runs if r.text.strip()]
    check("header cells: same dark fill + bold white runs",
          hdr_ok and runs and all(x["bold"] and x["white"] for x in runs),
          f"fills={[fill(c._tc) for c in hdr]} runs={runs}")
    body_rows = t.rows[1:-1]
    band = []
    for i, r in enumerate(body_rows):
        fs = {fill(c._tc) for c in uniq_cells(r)}
        want_f = BAND_FILL if i % 2 == 1 else None
        if fs != {want_f}:
            band.append(f"{i}:{sorted(map(str, fs))}")
    check("banded shading alternates across all body rows", not band, ",".join(band))
    fmt = []
    for i, r in enumerate(body_rows):
        for c in uniq_cells(r):
            for p in c.paragraphs:
                for run in p.runs:
                    if run.text.strip():
                        rp = run_props(run)
                        if rp["size"] != 10 or rp["font"] != "Arial" or rp["bold"] or rp["white"]:
                            fmt.append(f"{run.text}:{rp}")
    check("body runs keep Arial 10pt regular", not fmt, "; ".join(fmt[:3]))
    align = []
    ncol = len(want[0])
    for r in body_rows:
        cs = uniq_cells(r)
        for j in range(ncol - 3, ncol):     # the three numeric columns
            if j < len(cs) and cs[j].paragraphs[0].alignment != 2:   # WD_ALIGN_PARAGRAPH.RIGHT
                align.append(cs[j].text)
    check("numbers stay right-aligned", not align, ",".join(align[:4]))
    if task == "add-column":
        tot = uniq_cells(t.rows[-1])
        check("Total label spans every column before Amount", tot[0]._tc.grid_span == 4, f"span={tot[0]._tc.grid_span}")

    # --- old text gone from every part (no stale runs, delText or cached copies)
    texts = xml_texts(z)
    gone = {"delete-row": "Widget C", "rename-item": "Gadget D"}.get(task)
    if gone:
        where = [n for n, s in texts.items() if gone in s]
        check(f'"{gone}" absent from every XML part', not where, ",".join(where))

    # --- rest of the document unchanged
    paras = [p.text.strip() for p in d.paragraphs if p.text.strip()]
    want_p = OTHER_PARAS[:]
    if task == "insert-table":
        want_p.insert(want_p.index("Approved by:"), "Payment schedule")
    check("other paragraphs unchanged, in order", paras == want_p, f"got {paras}")
    fz = zipfile.ZipFile(FIX / "invoice.docx")
    for n in ("word/footer1.xml", "word/styles.xml") + (() if task == "replace-image" else ("word/header1.xml",)):
        if n in fz.namelist():
            check(f"{n} unchanged", n in z.namelist() and canon(z.read(n)) == canon(fz.read(n)))

    # --- images
    sec = d.sections[0]
    hdr_shas, ftr_shas = blip_shas(sec.header.part), blip_shas(sec.footer.part)
    check("footer badge still the old logo", ftr_shas == [logo], f"{ftr_shas}")
    if task == "replace-image":
        check("header logo is new_logo.png", hdr_shas == [new_logo], f"{hdr_shas}")
        check("header logo size unchanged", extents(sec.header.part) == extents(docx.Document(str(FIX / "invoice.docx")).sections[0].header.part))
        stale = [r.rId for r in sec.header.part.rels.values() if "image" in r.reltype and sha(r.target_part.blob) == logo]
        check("header part holds no rel to the old logo", not stale, f"{stale}")
    else:
        check("header logo untouched", hdr_shas == [logo], f"{hdr_shas}")
    body_shas = blip_shas(d.part)
    if task == "insert-image":
        check("signature inserted once in the body", body_shas == [sig], f"{body_shas}")
        ps = d.paragraphs
        ai = next((i for i, p in enumerate(ps) if p.text.strip() == "Approved by:"), None)
        host = None
        for p in ps[ai:ai + 3] if ai is not None else []:
            if p._p.find(".//" + qn("w:drawing")) is not None:
                host = p
                break
        check("signature placed right after \"Approved by:\"", host is not None)
        ex = extents(d.part)
        if ex:
            cx, cy = ex[0]
            check("signature 1.5in wide", abs(cx - Inches(1.5)) <= Inches(1.5) * 0.01, f"cx={cx}")
            check("signature aspect kept", abs(cy / cx - 100 / 300) < 0.01, f"{cy / cx:.3f}")
        check("signature paragraph right-aligned", host is not None and host.alignment == 2)
    else:
        check("no image added to the body", body_shas == [], f"{body_shas}")

    if task == "insert-table":
        els = [e for e in body if e.tag in (qn("w:p"), qn("w:tbl"))]
        txt = lambda e: "".join(x.text or "" for x in e.iter(qn("w:t"))).strip()
        ni = next((i for i, e in enumerate(els) if txt(e).startswith("Notes:")), None)
        rest = [e for e in els[ni + 1:] if not (e.tag == qn("w:p") and not txt(e))] if ni is not None else []
        check('"Payment schedule" right after Notes', bool(rest) and txt(rest[0]) == "Payment schedule")
        nt = rest[1] if len(rest) > 1 and rest[1].tag == qn("w:tbl") else None
        check("new table right after the heading", nt is not None)
        if nt is not None:
            tbl = docx.table.Table(nt, d._body)
            cells = [[c.text.strip() for c in uniq_cells(r)] for r in tbl.rows]
            check("new table content exact", cells == [["Due date", "Amount"], ["2026-10-01", "279.63"], ["2026-11-01", "279.62"]], f"{cells}")
            style = tbl.style
            ruled = nt.tblPr.find(qn("w:tblBorders")) is not None
            while style is not None and not ruled:
                ruled = style.element.find(".//" + qn("w:tblBorders")) is not None
                style = style.base_style
            check("new table has borders", ruled)
            gw = sum(int(g.get(qn("w:w"))) for g in tbl._tbl.tblGrid.findall(qn("w:gridCol")))
            check("new table within margins", gw <= TEXT_W * 1.005, f"{gw}")
    finish()


def finish():
    n = sum(results)
    print(f"SCORE {n}/{len(results)} {'PASS' if n == len(results) else 'FAIL'}")
    sys.exit(0)


if __name__ == "__main__":
    main()
