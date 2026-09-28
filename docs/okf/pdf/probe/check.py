#!/usr/bin/env python3
"""Text-layer checker for the PDF edit probe.

usage: check.py <task> <workdir>
  workdir holds the original fixture (must be byte-identical to the pristine
  copy) and the model's output invoice_out.pdf.
Prints one line per check (PASS/FAIL) and a final score line.
"""
import hashlib
import pathlib
import sys

import pymupdf

HERE = pathlib.Path(__file__).resolve().parent
FIX = HERE / "fixtures"
ROW_H = 22
LEFT, RIGHT = 50.0, 545.28

BASE_ROWS = [
    ["Widget A", "2", "10.00", "20.00"],
    ["Widget B", "5", "4.50", "22.50"],
    ["Widget C", "1", "99.00", "99.00"],
    ["Gadget D", "3", "12.25", "36.75"],
    ["Gadget E", "10", "1.10", "11.00"],
    ["Service F", "1", "250.00", "250.00"],
    ["Service G", "4", "30.00", "120.00"],
]
SKUS = [f"SKU-00{i}" for i in range(1, 8)]

results = []


def check(name, ok, detail=""):
    results.append(ok)
    print(("PASS " if ok else "FAIL ") + name + (f"  [{detail}]" if detail and not ok else ""))


def sha(p):
    return hashlib.sha256(p.read_bytes()).hexdigest()


def lines_of(page):
    """Words grouped into visual lines (top→bottom), each line left→right."""
    words = page.get_text("words")
    words.sort(key=lambda w: ((w[1] + w[3]) / 2, w[0]))
    lines = []
    for w in words:
        yc = (w[1] + w[3]) / 2
        if lines and abs(lines[-1]["y"] - yc) < 3:
            lines[-1]["w"].append(w)
        else:
            lines.append({"y": yc, "w": [w]})
    for ln in lines:
        ln["w"].sort(key=lambda w: w[0])
        ln["text"] = " ".join(w[4] for w in ln["w"])
    return lines


def table_rows(lines):
    """Lines from the header ("Item …") through the "Total" line, inclusive."""
    start = next((i for i, l in enumerate(lines) if l["text"].startswith("Item")), None)
    end = next((i for i, l in enumerate(lines) if l["text"].startswith("Total")), None)
    if start is None or end is None or end < start:
        return None
    return lines[start:end + 1]


def cells(line):
    """Split a table line into cells: label = leading alpha words, rest = numbers."""
    toks = [w[4] for w in line["w"]]
    label = []
    while toks and not any(ch.isdigit() for ch in toks[0]):
        label.append(toks.pop(0))
    return [" ".join(label)] + toks


def overlaps(page):
    ws = page.get_text("words")
    bad = []
    for i in range(len(ws)):
        a = pymupdf.Rect(ws[i][:4])
        for j in range(i + 1, len(ws)):
            b = pymupdf.Rect(ws[j][:4])
            inter = a & b
            if not inter.is_empty and inter.width * inter.height > 0.25 * min(a.width * a.height, b.width * b.height):
                bad.append((ws[i][4], ws[j][4]))
    return bad


def hlines(page):
    ys = []
    for d in page.get_drawings():
        for it in d["items"]:
            if it[0] == "l" and abs(it[1].y - it[2].y) < 0.5 and abs(it[1].x - it[2].x) > 100:
                ys.append(round(it[1].y, 1))
            if it[0] == "re" and it[1].height < 1.6 and it[1].width > 100:
                ys.append(round((it[1].y0 + it[1].y1) / 2, 1))
            elif it[0] == "re" and "s" in d["type"] and it[1].width > 100:
                ys += [round(it[1].y0, 1), round(it[1].y1, 1)]  # stroked box: top + bottom edges
    return sorted(set(ys))


def common(task, work, fixture):
    out = work / "invoice_out.pdf"
    check("original fixture untouched", sha(work / "invoice.pdf") == sha(FIX / fixture))
    if not out.exists():
        check("output invoice_out.pdf exists", False)
        return None, None
    doc = pymupdf.open(out)
    orig = pymupdf.open(FIX / fixture)
    check("page count unchanged (2)", doc.page_count == 2, str(doc.page_count))
    if doc.page_count >= 2:
        check("page 2 text unchanged", doc[1].get_text() == orig[1].get_text())
    p = doc[0]
    r = p.rect
    outside = [w[4] for w in p.get_text("words") if w[0] < 0 or w[1] < 0 or w[2] > r.width or w[3] > r.height]
    check("no text outside page bounds", not outside, str(outside[:5]))
    ov = overlaps(p)
    check("no overlapping words on page 1", not ov, str(ov[:5]))
    for keep in ["Invoice INV-1042", "Bill to: Acme Pty Ltd, 1 Example Street",
                 "Notes: Payment due within 30 days. Bank transfer only.", "Thank you for your business."]:
        check(f"kept: {keep[:30]}", any(l["text"] == keep for l in lines_of(p)))
    return doc, p


def expect_table(p, rows, total, header=("Item", "Qty", "Unit Price", "Amount")):
    lines = lines_of(p)
    tr = table_rows(lines)
    if tr is None:
        check("table found (Item … Total)", False)
        return
    hy = tr[0]["y"]
    hs = {(s["color"], s["font"], round(s["size"], 1)) for b in p.get_text("dict")["blocks"]
          for l in b.get("lines", []) for s in l["spans"]
          if s["text"].strip() and abs((s["bbox"][1] + s["bbox"][3]) / 2 - hy) < 3}
    check("header cells share one style (colour/font/size)", len(hs) == 1, str(hs))
    got = [cells(l) for l in tr[1:-1]]
    check("header text", tr[0]["text"] == " ".join(header), tr[0]["text"])
    check("body rows exact + in order", got == rows, f"got {got}")
    check("total row", cells(tr[-1]) == ["Total", total], str(cells(tr[-1])))
    ys = [l["y"] for l in tr]
    gaps = [round(b - a, 1) for a, b in zip(ys, ys[1:])]
    check("even row pitch (no hole/overlap)", all(abs(g - ROW_H) < 2.5 for g in gaps), str(gaps))
    xs = [w for l in tr for w in l["w"]]
    check("table text inside margins", min(w[0] for w in xs) >= LEFT - 1 and max(w[2] for w in xs) <= RIGHT + 1,
          f"{min(w[0] for w in xs):.1f}..{max(w[2] for w in xs):.1f}")
    hl = hlines(p)
    top, bot = ys[0] - ROW_H / 2, ys[-1] + ROW_H / 2
    inside = [y for y in hl if top - 3 <= y <= bot + 3]
    want = len(tr) + 1
    check(f"horizontal rules = rows+1 ({want})", len(inside) == want, f"{len(inside)}: {inside}")
    notes = next(l for l in lines if l["text"].startswith("Notes:")) if any(l["text"].startswith("Notes:") for l in lines) else None
    if notes:
        check("notes below table, not inside it", notes["y"] > bot + 5, f"notes {notes['y']:.1f} table bottom {bot:.1f}")


def ink(page, rect):
    pix = page.get_pixmap(matrix=pymupdf.Matrix(4, 4), clip=rect, colorspace=pymupdf.csGRAY)
    return sum(1 for v in pix.samples if v < 128)


def image_rects(p):
    return [(pymupdf.Rect(i["bbox"]), i["xref"], i["digest"]) for i in p.get_image_info(xrefs=True, hashes=True)]


def png_digest(path):
    # Rendered-content comparison: sample the embedded image vs source PNG pixels.
    return pymupdf.Pixmap(str(path))


def image_is(doc, xref, png):
    src = pymupdf.Pixmap(str(png))
    pix = pymupdf.Pixmap(doc, xref)
    if pix.n != src.n:
        pix = pymupdf.Pixmap(pymupdf.csRGB, pix)
        src = pymupdf.Pixmap(pymupdf.csRGB, src)
    if (pix.width, pix.height) != (src.width, src.height):
        return False
    return pix.samples == src.samples


def main():
    task, work = sys.argv[1], pathlib.Path(sys.argv[2])
    fixture = "invoice_subset.pdf" if task == "edit-cell-subset" else "invoice.pdf"
    doc, p = common(task, work, fixture)
    if doc is None:
        print(f"SCORE {sum(results)}/{len(results)} FAIL")
        return
    rows = [r[:] for r in BASE_ROWS]
    if task == "delete-row":
        del rows[2]
        check("'Widget C' text gone", "Widget C" not in p.get_text() and "99.00" not in p.get_text())
        expect_table(p, rows, "460.25")
    elif task == "edit-cell":
        rows[1] = ["Widget B", "12", "4.50", "54.00"]
        check("old values gone", "22.50" not in p.get_text() and "559.25" not in p.get_text())
        expect_table(p, rows, "590.75")
    elif task == "add-row":
        rows.append(["Widget H", "2", "15.00", "30.00"])
        check("old total gone", "559.25" not in p.get_text())
        expect_table(p, rows, "589.25")
    elif task == "add-column":
        rows = [[r[0], s] + r[1:] for r, s in zip(rows, SKUS)]
        expect_table(p, rows, "559.25", header=("Item", "SKU", "Qty", "Unit Price", "Amount"))
    elif task == "insert-table":
        expect_table(p, rows, "559.25")
        lines = lines_of(p)
        want = ["Payment schedule", "Due date Amount", "2026-10-01 279.63", "2026-11-01 279.62"]
        idx = [next((i for i, l in enumerate(lines) if l["text"] == w), None) for w in want]
        check("new table text present", None not in idx, str(list(zip(want, idx))))
        if None not in idx:
            check("new table rows in order", idx == sorted(idx))
            notes = next(i for i, l in enumerate(lines) if l["text"].startswith("Notes:"))
            foot = next(i for i, l in enumerate(lines) if l["text"].startswith("Thank you"))
            check("new table between Notes and footer", notes < idx[0] and idx[-1] < foot)
            hl = hlines(p)
            y0, y1 = lines[idx[1]]["y"], lines[idx[3]]["y"]
            check("new table is ruled (>=4 horizontal rules)", len([y for y in hl if y0 - 20 <= y <= y1 + 20]) >= 4)
    elif task == "edit-cell-subset":
        rows[3][0] = "Quokka Kit"
        check("'Gadget D' gone", "Gadget D" not in p.get_text())
        expect_table(p, rows, "559.25")
        # every glyph must actually render: compare ink in the output against a
        # clean render of the same string in the real font. Missing glyphs in a
        # reused subset font render blank/.notdef and lose most of the ink.
        spans = [s for b in p.get_text("dict")["blocks"] for l in b.get("lines", [])
                 for s in l["spans"] if "Quokka" in s["text"]]
        if not spans:
            check("new text glyphs render", False, "no span")
        else:
            s0 = spans[0]
            got = ink(p, pymupdf.Rect(s0["bbox"]))
            tmp = pymupdf.open()
            tp = tmp.new_page()
            tp.insert_font(fontname="g", fontfile="/System/Library/Fonts/Supplemental/Georgia.ttf")
            tp.insert_text((50, 100), s0["text"], fontname="g", fontsize=s0["size"])
            want = ink(tp, pymupdf.Rect(tp.get_text("dict")["blocks"][0]["lines"][0]["spans"][0]["bbox"]))
            ratio = got / want if want else 0
            check("new text glyphs render (ink ratio 0.6-1.6)", 0.6 <= ratio <= 1.6, f"{ratio:.2f}")
    elif task == "replace-image":
        imgs = image_rects(p)
        check("exactly one image on page 1", len(imgs) == 1, str(len(imgs)))
        if imgs:
            r, xref, _ = imgs[0]
            check("image at logo position", abs(r.x0 - 425.28) < 3 and abs(r.y0 - 50) < 3 and abs(r.x1 - 545.28) < 3 and abs(r.y1 - 90) < 3, str(r))
            check("image content is new_logo.png", image_is(doc, xref, FIX / "new_logo.png"))
        expect_table(p, rows, "559.25")
    elif task == "insert-image":
        imgs = image_rects(p)
        check("two images on page 1", len(imgs) == 2, str(len(imgs)))
        logo = [i for i in imgs if abs(i[0].y0 - 50) < 3]
        check("logo unchanged", len(logo) == 1 and image_is(doc, logo[0][1], FIX / "logo.png"))
        sig = [i for i in imgs if i not in logo]
        if sig:
            r, xref, _ = sig[0]
            check("signature content", image_is(doc, xref, FIX / "signature.png"))
            check("signature width ~150pt, aspect kept", abs(r.width - 150) < 3 and abs(r.height - 50) < 3, str(r))
            check("signature bottom-right, above footer", r.x1 <= RIGHT + 1 and r.x1 > 400 and r.y1 < 842 - 60 - 10 and r.y0 > 420, str(r))
            hit = [w[4] for w in p.get_text("words") if pymupdf.Rect(w[:4]).intersects(r)]
            check("signature covers no text", not hit, str(hit[:5]))
        expect_table(p, rows, "559.25")
    else:
        sys.exit(f"unknown task {task}")
    ok = all(results)
    print(f"SCORE {sum(results)}/{len(results)} {'PASS' if ok else 'FAIL'}")


if __name__ == "__main__":
    main()
