import { useEffect, useRef, useState } from "react";
import * as XLSX from "xlsx";
import { api } from "../../api/client";
import { isDelimitedTextPath } from "../../lib/previewKind";
import { useResolvedEditorTheme } from "../../hooks/useResolvedEditorTheme";
import { errorMessage } from "./xlsxStyle";
import {
  assertWithinFileLimit,
  MAX_COLS,
  MAX_ROWS,
  SHEETJS_READ_ROWS,
  workbookFromExcelJs,
  workbookFromSheetJs,
  type ConvertedWorkbook,
} from "./univerData";
import type { UniverSheetHandle } from "./univerMount";

const OOXML_WORKBOOK = /\.(xlsx|xlsm)$/i;

/**
 * Spreadsheet viewer. Read-only by design (same as Word/PowerPoint/PDF): sheet
 * tabs across the top, the active sheet as a Univer grid (read-only, no
 * toolbar or formula bar). Dark or light follows the editor appearance.
 *
 * `.xlsx`/`.xlsm` go through ExcelJS for formatting. CSV/TSV and legacy `.xls`
 * go through SheetJS as display text. Both are converted to Univer data by
 * `univerData.ts`; the Univer runtime is loaded lazily by `univerMount.ts`.
 *
 * When `content` is provided (controlled mode from FileTabContent split view),
 * CSV/TSV is parsed from the live editor string instead of fetching from disk.
 */
export default function ExcelViewer({
  path,
  projectRoot,
  projectHost,
  content,
}: {
  path: string;
  projectRoot?: string;
  projectHost?: string;
  /** Live editor content for split-view controlled mode (CSV/TSV only). */
  content?: string;
}) {
  const [book, setBook] = useState<ConvertedWorkbook | null>(null);
  const [active, setActive] = useState(0);
  const [host, setHost] = useState<UniverSheetHandle | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);
  const containerRef = useRef<HTMLDivElement | null>(null);
  const { effectiveAppearance } = useResolvedEditorTheme();
  const dark = effectiveAppearance === "dark";
  // Read by the mount effect, so a mount never starts in the wrong polarity.
  const darkRef = useRef(dark);
  useEffect(() => {
    darkRef.current = dark;
  }, [dark]);

  useEffect(() => {
    let cancelled = false;
    setLoading(true);
    setError(null);
    setBook(null);
    setActive(0);

    const show = (next: ConvertedWorkbook) => {
      if (cancelled) return;
      setBook(next);
      setLoading(false);
    };

    const fail = (e: unknown, attempted: string) => {
      console.error(`ExcelViewer: ${attempted} failed for ${path}:`, e);
      if (!cancelled) {
        setError(errorMessage(e));
        setLoading(false);
      }
    };

    // Controlled mode: parse CSV/TSV from live editor content. Synchronous, so
    // typing in the split-view editor does not flash the loading state.
    if (content !== undefined) {
      try {
        assertWithinFileLimit(new TextEncoder().encode(content).byteLength);
        // TSV is just CSV with tab delimiters — SheetJS handles both via type:"string".
        show(workbookFromSheetJs(XLSX.read(content, { type: "string", sheetRows: SHEETJS_READ_ROWS }), true));
      } catch (e) {
        fail(e, "parsing live CSV/TSV content");
      }
      return () => {
        cancelled = true;
      };
    }

    // Uncontrolled mode: fetch from disk
    const load = async (): Promise<void> => {
      const buf = await api.fetchFileRaw(path, projectRoot, projectHost);
      if (cancelled) return;
      // Refused before any parser runs: ExcelJS and SheetJS hold the whole file.
      assertWithinFileLimit(buf.byteLength);
      // Delimited text has no binary magic, so it must be decoded as UTF-8
      // before SheetJS sees it: `type: "array"` reads raw bytes as latin-1
      // and mojibakes every non-ASCII cell. See isDelimitedTextPath.
      if (isDelimitedTextPath(path)) {
        show(workbookFromSheetJs(XLSX.read(new TextDecoder().decode(buf), { type: "string", sheetRows: SHEETJS_READ_ROWS }), true));
        return;
      }
      if (OOXML_WORKBOOK.test(path)) {
        // Dynamic so ExcelJS stays out of the main bundle.
        const { Workbook: ExcelWorkbook } = await import("exceljs");
        const wb = new ExcelWorkbook();
        await wb.xlsx.load(buf);
        if (cancelled) return;
        show(workbookFromExcelJs(wb));
        return;
      }
      show(workbookFromSheetJs(XLSX.read(buf, { type: "array", sheetRows: SHEETJS_READ_ROWS }), false));
    };
    load().catch((e: unknown) => fail(e, `loading ${path}`));
    return () => {
      cancelled = true;
    };
  }, [path, projectRoot, projectHost, content]);

  // Mounts Univer once per converted workbook. The holder lets the cleanup
  // dispose a handle that resolved after the effect was cancelled.
  useEffect(() => {
    const container = containerRef.current;
    if (book === null || container === null) return;
    let cancelled = false;
    const mounted: { handle: UniverSheetHandle | null } = { handle: null };
    import("./univerMount")
      .then(({ mountUniverSheet }) => mountUniverSheet(container, book.data, darkRef.current))
      .then((handle) => {
        if (cancelled) {
          handle.dispose();
          return;
        }
        mounted.handle = handle;
        setHost(handle);
      })
      .catch((e: unknown) => {
        console.error(`ExcelViewer: mounting the spreadsheet grid failed for ${path}:`, e);
        if (!cancelled) setError(errorMessage(e));
      });
    return () => {
      cancelled = true;
      setHost(null);
      if (mounted.handle !== null) mounted.handle.dispose();
    };
  }, [book, path]);

  useEffect(() => {
    if (host === null) return;
    host.setDarkMode(dark);
  }, [host, dark]);

  useEffect(() => {
    if (host === null || book === null) return;
    const sheetId = book.data.sheetOrder[active];
    try {
      host.activateSheet(sheetId);
    } catch (e) {
      console.error(`ExcelViewer: activating sheet ${sheetId} of ${path} failed:`, e);
      setError(errorMessage(e));
    }
  }, [host, book, active, path]);

  if (loading) return <div className="p-4 text-xs text-muted-foreground">Loading spreadsheet…</div>;
  if (error) return <div className="p-4 text-xs text-red-400">Spreadsheet failed: {error}</div>;
  if (book === null || book.sheets.length === 0) return <div className="p-4 text-xs text-muted-foreground">No sheets found.</div>;

  const sheet = book.sheets[active];
  const truncated = [
    sheet.rowCapped ? `first ${MAX_ROWS} rows` : null,
    sheet.colCapped ? `first ${MAX_COLS} columns` : null,
  ].filter((part): part is string => part !== null);

  return (
    <div className="flex h-full min-h-0 flex-col">
      <div className="flex shrink-0 items-center gap-1 overflow-x-auto border-b border-border px-2 py-1 text-xs" role="tablist" aria-label="Sheets">
        {book.sheets.map((s, i) => (
          <button
            key={s.id}
            type="button"
            role="tab"
            aria-selected={i === active}
            onClick={() => setActive(i)}
            className={`shrink-0 rounded px-2 py-0.5 ${i === active ? "bg-muted text-foreground" : "text-muted-foreground hover:text-foreground"}`}
            title={`Sheet ${s.name}`}
          >
            {s.name}
          </button>
        ))}
        {truncated.length > 0 && (
          <span className="ml-auto shrink-0 text-muted-foreground">Showing {truncated.join(" and ")} — large sheet truncated</span>
        )}
      </div>
      <div className="relative min-h-0 flex-1">
        <div ref={containerRef} className="absolute inset-0" />
        {sheet.isEmpty && <div className="absolute inset-0 bg-background p-4 text-xs text-muted-foreground">(empty sheet)</div>}
      </div>
    </div>
  );
}
