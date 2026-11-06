import { useCallback, useEffect, useRef, useState } from "react";
import { api as apiClient } from "../../api/client";
import { useResolvedEditorTheme } from "../../hooks/useResolvedEditorTheme";
import ConfirmDialog from "../common/ConfirmDialog";
import { errorMessage } from "./xlsxStyle";
import {
  MAX_COLS,
  MAX_ROWS,
  workbookFromDelimitedText,
  workbookFromFileBytes,
  type ConvertedWorkbook,
} from "./univerData";
import { rawContentHash, serializeWorkbookBytes } from "./univerSerialize";
import type { UniverSheetHandle } from "./univerMount";
import { useViewerReport } from "../../lib/viewerContext";

/** Files whose first Save the user has confirmed this session, keyed by host,
 *  root and path. Module scope so a remount (tab switch, split toggle) does not
 *  ask again. */
const confirmedSaveKeys = new Set<string>();

/** The selected cells as tab-separated lines, one line per row. */
function formatCells(values: string[][]): string {
  return values.map((row) => row.join("\t")).join("\n");
}

/**
 * Spreadsheet viewer: sheet tabs across the top, the active sheet as a Univer
 * grid (no ribbon, formula bar or toolbar). Dark or light follows the editor
 * appearance.
 *
 * `.xlsx` (uncontrolled mode, no truncation) is EDITABLE: the grid accepts
 * edits, a Save button appears once dirty, and save serializes the live
 * snapshot back to .xlsx (values-only — formulas/charts/images/pivots lost at
 * load are not recovered; see `univerSerialize.ts`) and PUTs it to
 * `/api/files/raw` with the pre-edit disk hash as the optimistic-concurrency
 * guard. `.xlsm`, CSV/TSV and legacy `.xls` stay read-only.
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
  onDirtyChange,
  onSave,
  onSaveRef,
  onSaveHandlerRegistered,
}: {
  path: string;
  projectRoot?: string;
  projectHost?: string;
  /** Live editor content for split-view controlled mode (CSV/TSV only). */
  content?: string;
  /** Called whenever the editable workbook's dirty flag changes. */
  onDirtyChange?: (dirty: boolean) => void;
  /** If provided, replaces the built-in Save button's handler (tests/embedders). */
  onSave?: () => Promise<void>;
  /** When set, the host (Files-tab preview-only toolbar) receives the save
   *  handler through this ref so it can invoke it from its own Save button.
   *  Returns true when the save completed successfully, false when it failed
   *  or was cancelled (e.g. the first-save confirm was declined). */
  onSaveRef?: { current: (() => Promise<boolean>) | null };
  /** Called after the external save handler (`onSaveRef.current`) is
   *  registered, so the host can pick it up (ref writes don't re-render). */
  onSaveHandlerRegistered?: () => void;
}) {
  const [book, setBook] = useState<ConvertedWorkbook | null>(null);
  const [active, setActive] = useState(0);
  const [host, setHost] = useState<UniverSheetHandle | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);
  // Editable-state: revision counts local mutations; dirty = revision past the
  // last successful save (or load). baseHashRef is the expected_hash for the
  // next save (hash of the bytes currently on disk).
  const [revision, setRevision] = useState(0);
  const [savedRevision, setSavedRevision] = useState(0);
  const [saving, setSaving] = useState(false);
  const [saveError, setSaveError] = useState<string | null>(null);
  const [confirmOpen, setConfirmOpen] = useState(false);
  const baseBytesRef = useRef<ArrayBuffer | null>(null);
  const baseHashRef = useRef<string>("");
  // The save in flight, if any. A second request joins it instead of sending
  // another PUT against the same base hash: Cmd/Ctrl+S can reach this viewer
  // through two handlers in one keypress, and the second PUT would 409.
  const inFlightSaveRef = useRef<Promise<boolean> | null>(null);
  // The first-save confirmation a request is waiting on, while its dialog is open.
  const confirmRef = useRef<{ answer: Promise<boolean>; settle: (ok: boolean) => void } | null>(null);
  // Latest `save`, kept in a ref so the (stable) external save handler below
  // always invokes the current revision-aware save instead of a stale closure.
  // Assigned after `save` is defined below (hook order).
  const saveRef = useRef<(() => Promise<boolean>) | null>(null);
  const containerRef = useRef<HTMLDivElement | null>(null);
  const { effectiveAppearance } = useResolvedEditorTheme();
  const dark = effectiveAppearance === "dark";
  // Read by the mount effect, so a mount never starts in the wrong polarity.
  const darkRef = useRef(dark);
  useEffect(() => {
    darkRef.current = dark;
  }, [dark]);
  // Read by the mount effect's selection listener. A ref keeps Univer from
  // re-mounting when the report function changes identity.
  const report = useViewerReport(path);
  const reportRef = useRef(report);
  useEffect(() => {
    reportRef.current = report;
  }, [report]);

  // Editable only for uncontrolled .xlsx with no truncation in any sheet.
  const editable =
    content === undefined && /\.xlsx$/i.test(path) && book !== null && !book.sheets.some((s) => s.rowCapped || s.colCapped);

  const dirty = revision > savedRevision;

  useEffect(() => {
    onDirtyChange?.(dirty);
  }, [dirty, onDirtyChange]);

  const saveKey = `${projectHost ?? ""}|${projectRoot ?? ""}|${path}`;

  const save = useCallback((): Promise<boolean> => {
    const inFlight = inFlightSaveRef.current;
    if (inFlight !== null) return inFlight;
    const run = (async (): Promise<boolean> => {
      const revAtSave = revision;
      setSaving(true);
      setSaveError(null);
      try {
        if (onSave) {
          await onSave();
        } else {
          if (!editable || host === null)
            throw new Error("Cannot save: spreadsheet is not editable or not loaded yet.");
          const bytes = await serializeWorkbookBytes(host.snapshot());
          const expectedHash = baseHashRef.current;
          await apiClient.saveFileRaw(path, bytes, {
            projectRoot,
            expectedHash,
            host: projectHost,
          });
          baseHashRef.current = rawContentHash(bytes);
        }
        // If the user edited during the save, revision advanced past revAtSave
        // and dirty stays true against the new base.
        setSavedRevision(revAtSave);
        return true;
      } catch (e) {
        const msg = errorMessage(e);
        const status = (e as { status?: number })?.status;
        if (status === 409) {
          setSaveError(`${msg} Reload the file and re-apply your edits.`);
        } else {
          setSaveError(msg);
        }
        console.error(`ExcelViewer: saving ${path} failed:`, e);
        return false;
      } finally {
        setSaving(false);
      }
    })().finally(() => {
      inFlightSaveRef.current = null;
    });
    inFlightSaveRef.current = run;
    return run;
  }, [editable, host, path, projectRoot, projectHost, revision, onSave]);

  // Keep the stable external handler pointed at the latest revision-aware save.
  saveRef.current = save;

  // The first Save of a file asks first: the save writes values only, so
  // formulas, charts, images and pivots not carried into the grid are lost.
  // The returned promise settles with the user's answer: the save's result
  // after Save, false after Cancel. Callers that await it therefore see the
  // real outcome, not a "dialog opened" false.
  const requestSave = useCallback((): Promise<boolean> => {
    if (confirmedSaveKeys.has(saveKey)) return save();
    const open = confirmRef.current;
    if (open !== null) return open.answer;
    let settle!: (ok: boolean) => void;
    const answer = new Promise<boolean>((resolve) => {
      settle = resolve;
    });
    confirmRef.current = { answer, settle };
    setConfirmOpen(true);
    return answer;
  }, [save, saveKey]);

  // A request still waiting on the dialog when the viewer unmounts is cancelled,
  // so no caller is left awaiting an answer that can never come.
  useEffect(
    () => () => {
      confirmRef.current?.settle(false);
      confirmRef.current = null;
    },
    [],
  );

  /** Takes the open first-save confirmation, so one answer settles it. */
  const takeConfirm = (): { answer: Promise<boolean>; settle: (ok: boolean) => void } => {
    const open = confirmRef.current;
    if (open === null) throw new Error(`ExcelViewer: no first-save confirmation is open for ${path}`);
    confirmRef.current = null;
    return open;
  };

  // Hand the host its save handler so it can drive Save from its own header.
  // `requestSave` (not the bare save) so the first-save confirm still gates.
  // Returns true when the save completed, false when cancelled or failed.
  useEffect(() => {
    if (onSaveRef) {
      onSaveRef.current = async () => requestSave();
      onSaveHandlerRegistered?.();
    }
    return () => {
      if (onSaveRef) onSaveRef.current = null;
    };
  }, [requestSave, onSaveRef, onSaveHandlerRegistered]);

  useEffect(() => {
    let cancelled = false;
    setLoading(true);
    setError(null);
    setBook(null);
    setActive(0);
    setRevision(0);
    setSavedRevision(0);
    setSaveError(null);
    baseBytesRef.current = null;
    baseHashRef.current = "";

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
        show(workbookFromDelimitedText(content));
      } catch (e) {
        fail(e, "parsing live CSV/TSV content");
      }
      return () => {
        cancelled = true;
      };
    }

    // Uncontrolled mode: fetch from disk
    const load = async (): Promise<void> => {
      const buf = await apiClient.fetchFileRaw(path, projectRoot, projectHost);
      if (cancelled) return;
      baseBytesRef.current = buf;
      baseHashRef.current = rawContentHash(buf);
      show(await workbookFromFileBytes(path, buf));
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
    const mounted: { handle: UniverSheetHandle | null; unsubscribe: (() => void) | null } = {
      handle: null,
      unsubscribe: null,
    };
    const mountEditable =
      content === undefined && /\.xlsx$/i.test(path) && !book.sheets.some((s) => s.rowCapped || s.colCapped);
    import("./univerMount")
      .then(({ mountUniverSheet }) =>
        mountUniverSheet(container, book.data, darkRef.current, {
          editable: mountEditable,
          onDirty: () => setRevision((r) => r + 1),
        }),
      )
      .then((handle) => {
        if (cancelled) {
          handle.dispose();
          return;
        }
        mounted.handle = handle;
        setHost(handle);
        // Reports the active sheet and selection now, and again on each change.
        const publish = () => {
          const sel = handle.selection();
          // Univer's default selection on a freshly activated sheet is one empty
          // cell (A1). Report it as the sheet alone, as a tab click does.
          const blankCell = sel !== null && !sel.range.includes(":") && formatCells(sel.values) === "";
          const shown = sel === null || blankCell ? null : sel;
          reportRef.current({
            location: { sheet: handle.activeSheetName(), range: shown === null ? undefined : shown.range },
            selection: shown === null ? null : formatCells(shown.values),
          });
        };
        mounted.unsubscribe = handle.onSelectionChange(publish);
        publish();
      })
      .catch((e: unknown) => {
        console.error(`ExcelViewer: mounting the spreadsheet grid failed for ${path}:`, e);
        if (!cancelled) setError(errorMessage(e));
      });
    return () => {
      cancelled = true;
      setHost(null);
      if (mounted.unsubscribe !== null) mounted.unsubscribe();
      if (mounted.handle !== null) mounted.handle.dispose();
    };
    // `content` is deliberately NOT a dep: the load effect turns a content
    // change into a new `book`, which re-mounts. Listing it here would also
    // fire with the stale book first, double-disposing the previous grid.
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
            onClick={() => {
              setActive(i);
              // The selection listener reports this sheet's selection once Univer activates it.
              report({ location: { sheet: s.name, range: undefined }, selection: null });
            }}
            className={`shrink-0 rounded px-2 py-0.5 ${i === active ? "bg-muted text-foreground" : "text-muted-foreground hover:text-foreground"}`}
            title={`Sheet ${s.name}`}
          >
            {s.name}
          </button>
        ))}
        {editable && dirty && (
          <button
            type="button"
            onClick={requestSave}
            disabled={saving}
            className="ml-auto shrink-0 rounded bg-muted px-2 py-0.5 text-foreground hover:bg-accent disabled:opacity-50"
            aria-label="Save spreadsheet"
          >
            {saving ? "Saving…" : "Save"}
          </button>
        )}
        {saveError !== null && <span className="shrink-0 text-red-400" role="alert">{saveError}</span>}
        {truncated.length > 0 && (
          <span className={editable && dirty ? "" : "ml-auto shrink-0 text-muted-foreground"}>Showing {truncated.join(" and ")} — large sheet truncated</span>
        )}
        {editable && !dirty && truncated.length === 0 && saveError === null && (
          <span className="ml-auto shrink-0 text-muted-foreground">Editable — edits are saved when you click Save</span>
        )}
      </div>
      <div className="relative min-h-0 flex-1">
        <div ref={containerRef} className="absolute inset-0" />
        {sheet.isEmpty && <div className="absolute inset-0 bg-background p-4 text-xs text-muted-foreground">(empty sheet)</div>}
      </div>
      <ConfirmDialog
        open={confirmOpen}
        title="Save spreadsheet?"
        description={`Saving rewrites ${path} from the grid. Formulas, charts, images and pivot tables in the original file may not be preserved. A one-time backup of the original is kept at ${path}.bak.`}
        confirmLabel="Save"
        confirmVariant="default"
        onConfirm={async () => {
          confirmedSaveKeys.add(saveKey);
          setConfirmOpen(false);
          const open = takeConfirm();
          open.settle(await save());
        }}
        onCancel={() => {
          setConfirmOpen(false);
          takeConfirm().settle(false);
        }}
      />
    </div>
  );
}
