import { useEffect, useRef, useState } from "react";
import * as XLSX from "xlsx";
import { api } from "../../api/client";
import { SelectionToolbar, usePreviewSelection } from "./SelectionToolbar";

// Caps so a huge spreadsheet can't freeze the tab: we render the leading
// window and say so, instead of dumping 100k rows into the DOM. Full
// fidelity is one click away via "Open in app".
const MAX_ROWS = 1000;
const MAX_COLS = 100;

/**
 * Spreadsheet viewer (SheetJS `xlsx` — stable/maintained, Apache-2.0).
 * Read-only by design (same as Word/PowerPoint/PDF): sheet tabs across
 * the top, the active sheet as a plain HTML table with selectable text
 * for Copy / Ask-LLM. Never routed through the Monaco editor.
 */
export default function ExcelViewer({ path, projectRoot }: { path: string; projectRoot?: string }) {
  const [sheets, setSheets] = useState<string[]>([]);
  const [active, setActive] = useState(0);
  const [rows, setRows] = useState<string[][]>([]);
  const [rowCapped, setRowCapped] = useState(false);
  const [colCapped, setColCapped] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);
  const bookRef = useRef<XLSX.WorkBook | null>(null);
  const { ref, sel, clear } = usePreviewSelection<HTMLDivElement>(() => `sheet ${sheets[active] ?? ""}`);

  useEffect(() => {
    let cancelled = false;
    setLoading(true);
    setError(null);
    setSheets([]);
    setRows([]);
    setActive(0);
    api
      .fetchFileRaw(path, projectRoot)
      .then((buf) => {
        if (cancelled) return;
        // sheetRows caps parsing itself (not just rendering) so a 100k-row
        // workbook can't freeze the tab during SheetJS parsing either.
        // CSV has no binary magic for format sniffing — decode and parse
        // it as text instead.
        const wb = path.toLowerCase().endsWith(".csv")
          ? XLSX.read(new TextDecoder().decode(buf), { type: "string", sheetRows: MAX_ROWS })
          : XLSX.read(buf, { type: "array", sheetRows: MAX_ROWS });
        bookRef.current = wb;
        setSheets(wb.SheetNames);
        loadSheet(wb, 0);
        setLoading(false);
      })
      .catch((e) => {
        if (!cancelled) {
          setError(e instanceof Error ? e.message : String(e));
          setLoading(false);
        }
      });
    return () => {
      cancelled = true;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [path, projectRoot]);

  const loadSheet = (wb: XLSX.WorkBook, idx: number) => {
    const name = wb.SheetNames[idx];
    const ws = wb.Sheets[name];
    if (!ws || !ws["!ref"]) {
      setRows([]);
      setRowCapped(false);
      setColCapped(false);
      setActive(idx);
      return;
    }
    // Totals come from the sheet's own range (not just the first row), so
    // wide later rows can't get silently clipped without a notice.
    const range = XLSX.utils.decode_range(ws["!ref"]);
    const totalR = range.e.r - range.s.r + 1;
    const totalC = range.e.c - range.s.c + 1;
    // sheetRows already capped parsing at MAX_ROWS: hitting the boundary
    // means "at least this many" — word the notice accordingly.
    setRowCapped(totalR >= MAX_ROWS);
    setColCapped(totalC > MAX_COLS);
    const aoa = XLSX.utils.sheet_to_json<string[]>(ws, { header: 1, raw: false, defval: "" });
    setRows(aoa.slice(0, MAX_ROWS).map((r) => (Array.isArray(r) ? r : []).slice(0, MAX_COLS).map(String)));
    setActive(idx);
    clear();
  };

  const switchSheet = (idx: number) => {
    const wb = bookRef.current;
    if (wb) loadSheet(wb, idx);
  };

  if (loading) return <div className="p-4 text-xs text-muted-foreground">Loading spreadsheet…</div>;
  if (error) return <div className="p-4 text-xs text-red-400">Spreadsheet failed: {error}</div>;
  if (sheets.length === 0) return <div className="p-4 text-xs text-muted-foreground">No sheets found.</div>;

  return (
    <div className="flex h-full min-h-0 flex-col">
      <div className="flex shrink-0 items-center gap-1 overflow-x-auto border-b border-border px-2 py-1 text-xs" role="tablist" aria-label="Sheets">
        {sheets.map((name, i) => (
          <button
            key={name}
            type="button"
            role="tab"
            aria-selected={i === active}
            onClick={() => switchSheet(i)}
            className={`shrink-0 rounded px-2 py-0.5 ${i === active ? "bg-muted text-foreground" : "text-muted-foreground hover:text-foreground"}`}
            title={`Sheet ${name}`}
          >
            {name}
          </button>
        ))}
        {(rowCapped || colCapped) && (
          <span className="ml-auto shrink-0 text-muted-foreground">
            {rowCapped ? `First ${rows.length} rows` : `${rows.length} rows`}
            {colCapped ? ` × first ${MAX_COLS} cols` : ""} — large sheet capped
          </span>
        )}
      </div>
      <div ref={ref} className="min-h-0 flex-1 overflow-auto p-2">
        {rows.length === 0 ? (
          <div className="p-4 text-xs text-muted-foreground">(empty sheet)</div>
        ) : (
          <table className="w-full border-collapse text-xs select-text">
            <tbody>
              {rows.map((row, ri) => (
                <tr key={ri} className={ri === 0 ? "bg-muted/60 font-medium" : undefined}>
                  {row.map((cell, ci) => (
                    <td key={ci} className="whitespace-nowrap border border-border px-2 py-0.5">
                      {cell}
                    </td>
                  ))}
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </div>
      {sel && <SelectionToolbar sel={sel} path={path} label={`sheet ${sheets[active] ?? ""}`} projectRoot={projectRoot} onDone={clear} />}
    </div>
  );
}
