import { useEffect, useMemo, useState } from "react";
import type { ReactNode } from "react";
import { AlertTriangle, Loader2, Plus, Trash2, X } from "lucide-react";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "../ui/dialog";
import type { DBCell, DBCellInput, DBColumnDef, DBTableSchema } from "../../api/client";

/**
 * Confirmation and editing dialogs for the SQLite browser (phases P2–P4).
 * Kept in their own module so the viewer stays readable and the dialog focus
 * policy lives in one place: a destructive confirm annotates Cancel with
 * `data-dialog-default-action`, everything else annotates its primary button.
 */

const BTN = "rounded border border-border px-2 py-0.5 text-xs hover:bg-muted disabled:opacity-50";
const BTN_PRIMARY =
  "rounded bg-primary px-2 py-0.5 text-xs text-primary-foreground hover:opacity-90 disabled:opacity-50";
const BTN_DANGER =
  "rounded bg-destructive px-2 py-0.5 text-xs text-destructive-foreground hover:opacity-90 disabled:opacity-50";
const INPUT = "w-full rounded border border-border bg-transparent px-2 py-1 font-mono text-xs";

/**
 * A column whose declared type has BLOB affinity.
 *
 * SQLite matches the word anywhere in the declaration (BLOB, MEDIUMBLOB,
 * VARBINARY is NOT a blob though — affinity comes from the substring "BLOB"), so
 * the check is a substring test rather than an equality.
 */
export function isBlobColumn(declType: string): boolean {
  return (declType || "").toUpperCase().includes("BLOB");
}

/** Read a File as bare base64 (no data: prefix), which is what `$blob` wants. */
export function fileToBase64(file: File): Promise<string> {
  return new Promise((resolve, reject) => {
    const reader = new FileReader();
    reader.onerror = () => reject(new Error(`Could not read ${file.name}`));
    reader.onload = () => {
      const result = String(reader.result ?? "");
      const comma = result.indexOf(",");
      resolve(comma >= 0 ? result.slice(comma + 1) : result);
    };
    reader.readAsDataURL(file);
  });
}

/** A grid cell rendered as editable text. NULL and BLOBs become empty. */
export function cellToInput(v: DBCell): string {
  if (v === null) return "";
  if (typeof v === "object" && v !== null && "$blob" in v) return "";
  return String(v);
}

/** Convert an input string back to a value using the column's declared type.
 * An empty input means NULL (there is no way to type an empty string and NULL
 * distinctly in a single-line field; NULL is the more common intent). */
export function inputToCell(text: string, declType: string): DBCell {
  const t = text.trim();
  if (t === "") return null;
  const up = (declType || "").toUpperCase();
  if (up.includes("INT")) {
    const n = Number(t);
    return Number.isFinite(n) ? Math.trunc(n) : text;
  }
  if (
    up.includes("REAL") ||
    up.includes("FLOA") ||
    up.includes("DOUB") ||
    up.includes("NUM") ||
    up.includes("DEC")
  ) {
    const n = Number(t);
    return Number.isFinite(n) ? n : text;
  }
  return text;
}

export function ConfirmDialog({
  open,
  title,
  message,
  confirmLabel = "Confirm",
  destructive = true,
  busy = false,
  onCancel,
  onConfirm,
}: {
  open: boolean;
  title: string;
  message: ReactNode;
  confirmLabel?: string;
  destructive?: boolean;
  busy?: boolean;
  onCancel: () => void;
  onConfirm: () => void;
}) {
  return (
    <Dialog open={open} onOpenChange={(o) => !o && onCancel()}>
      <DialogContent className="max-w-md" data-testid="sqlite-confirm">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2 text-sm">
            <AlertTriangle className="h-4 w-4 text-destructive" />
            {title}
          </DialogTitle>
          <DialogDescription asChild>
            <div className="text-xs text-muted-foreground">{message}</div>
          </DialogDescription>
        </DialogHeader>
        <DialogFooter>
          {/* Destructive: the safe action is the default, not Confirm. */}
          <button
            type="button"
            onClick={onCancel}
            className={BTN}
            {...(destructive ? { "data-dialog-default-action": true } : {})}
          >
            Cancel
          </button>
          <button
            type="button"
            onClick={onConfirm}
            disabled={busy}
            className={destructive ? BTN_DANGER : BTN_PRIMARY}
            data-testid="sqlite-confirm-action"
            {...(destructive ? {} : { "data-dialog-default-action": true })}
          >
            {busy ? <Loader2 className="mr-1 inline h-3 w-3 animate-spin" /> : null}
            {confirmLabel}
          </button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

/** Insert (initial=null) or edit one row. Returns the new values; the caller
 * supplies the original-row key for an update. */
export function RowEditorDialog({
  open,
  schema,
  initial,
  busy,
  error,
  onCancel,
  onSubmit,
}: {
  open: boolean;
  schema: DBTableSchema;
  initial: Record<string, DBCell> | null;
  busy: boolean;
  error: string | null;
  onCancel: () => void;
  onSubmit: (values: Record<string, DBCellInput>) => void;
}) {
  const editable = useMemo(
    () => schema.columns.filter((c) => !c.generated && c.name !== "_rowid_"),
    [schema],
  );
  const isEdit = initial != null;
  const [draft, setDraft] = useState<Record<string, string>>({});
  // A BLOB cannot be typed into a text field, so it gets its own draft: the
  // base64 the file picker produced, or "clear" to store NULL.
  const [blobDraft, setBlobDraft] = useState<
    Record<string, { name: string; size: number; data: string } | null>
  >({});
  const [blobError, setBlobError] = useState<string | null>(null);

  useEffect(() => {
    if (!open) return;
    const next: Record<string, string> = {};
    for (const c of editable) next[c.name] = isEdit ? cellToInput(initial?.[c.name] ?? null) : "";
    setDraft(next);
    setBlobDraft({});
    setBlobError(null);
  }, [open, isEdit, editable, initial]);

  const submit = () => {
    const values: Record<string, DBCellInput> = {};
    for (const c of editable) {
      if (isBlobColumn(c.decl_type)) {
        // Three states, keyed on PRESENCE rather than truthiness, because a
        // cleared blob (null) and an untouched one (absent) are both falsy but
        // mean opposite things:
        //   absent  → leave the stored value alone on an update; NULL on insert
        //   null    → the user cleared it, so store NULL
        //   object  → the user picked a file, so store its bytes
        if (!Object.prototype.hasOwnProperty.call(blobDraft, c.name)) {
          if (!isEdit) values[c.name] = null;
          continue;
        }
        const picked = blobDraft[c.name];
        values[c.name] = picked ? { $blob: true, data: picked.data } : null;
        continue;
      }
      values[c.name] = inputToCell(draft[c.name] ?? "", c.decl_type);
    }
    onSubmit(values);
  };

  return (
    <Dialog open={open} onOpenChange={(o) => !o && onCancel()}>
      <DialogContent className="max-w-lg" data-testid="sqlite-row-dialog">
        <DialogHeader>
          <DialogTitle className="text-sm">
            {isEdit ? "Edit row" : "Add row"} · {schema.name}
          </DialogTitle>
          <DialogDescription className="text-xs">
            Empty fields are stored as NULL.
          </DialogDescription>
        </DialogHeader>
        <div className="max-h-[60vh] space-y-2 overflow-auto pr-1">
          {editable.map((c) => (
            <label key={c.name} className="block">
              <span className="flex items-center gap-1 text-[11px] text-muted-foreground">
                {c.name}
                <span className="opacity-70">{c.decl_type}</span>
                {c.pk ? <span className="text-amber-500">pk</span> : null}
                {c.not_null ? <span className="text-destructive">not null</span> : null}
              </span>
              {isBlobColumn(c.decl_type) ? (
                <span className="flex items-center gap-1">
                  <input
                    type="file"
                    aria-label={`Choose file for ${c.name}`}
                    data-testid={`db-blob-${c.name}`}
                    className="block w-full text-[11px]"
                    onChange={(e) => {
                      const f = e.target.files?.[0];
                      if (!f) return;
                      setBlobError(null);
                      fileToBase64(f)
                        .then((data) => setBlobDraft((d) => ({ ...d, [c.name]: { name: f.name, size: f.size, data } })))
                        .catch((err: unknown) =>
                          setBlobError(err instanceof Error ? err.message : String(err)),
                        );
                    }}
                  />
                  {blobDraft[c.name] ? (
                    <>
                      <span className="shrink-0 text-[10px] text-muted-foreground">
                        {blobDraft[c.name]!.name} · {blobDraft[c.name]!.size} B
                      </span>
                      <button
                        type="button"
                        aria-label={`Clear ${c.name}`}
                        onClick={() => setBlobDraft((d) => ({ ...d, [c.name]: null }))}
                        className="shrink-0 rounded p-0.5 hover:bg-muted"
                      >
                        <X className="h-3 w-3" />
                      </button>
                    </>
                  ) : (
                    <span className="shrink-0 text-[10px] text-muted-foreground">
                      {isEdit ? "unchanged" : "NULL"}
                    </span>
                  )}
                </span>
              ) : (
                <input
                  value={draft[c.name] ?? ""}
                  onChange={(e) => setDraft((d) => ({ ...d, [c.name]: e.target.value }))}
                  placeholder="NULL"
                  aria-label={c.name}
                  data-testid={`db-field-${c.name}`}
                  className={INPUT}
                />
              )}
            </label>
          ))}
        </div>
        {blobError ? (
          <p role="alert" className="text-xs text-destructive" data-testid="db-blob-error">
            {blobError}
          </p>
        ) : null}
        {error ? (
          <p role="alert" className="text-xs text-destructive">
            {error}
          </p>
        ) : null}
        <DialogFooter>
          <button type="button" onClick={onCancel} className={BTN}>
            Cancel
          </button>
          <button
            type="button"
            onClick={submit}
            disabled={busy}
            data-dialog-default-action
            data-testid="sqlite-row-save"
            className={BTN_PRIMARY}
          >
            {busy ? <Loader2 className="mr-1 inline h-3 w-3 animate-spin" /> : null}
            Save
          </button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

export type SchemaAction = "add_column" | "create_table" | "create_index";

const TYPE_OPTIONS = [
  "TEXT",
  "INTEGER",
  "REAL",
  "BLOB",
  "NUMERIC",
  "BOOLEAN",
  "DATETIME",
  "DATE",
  "VARCHAR(255)",
];

/** Guided DDL: add a column, create a table, or create an index. The server
 * builds the SQL from these validated parts. */
export function SchemaDialog({
  open,
  action,
  schema,
  busy,
  error,
  onCancel,
  onSubmit,
}: {
  open: boolean;
  action: SchemaAction | null;
  schema: DBTableSchema | null;
  busy: boolean;
  error: string | null;
  onCancel: () => void;
  onSubmit: (payload: {
    column?: DBColumnDef;
    columns?: DBColumnDef[];
    table?: string;
    index?: string;
    indexColumns?: string[];
    unique?: boolean;
  }) => void;
}) {
  const [name, setName] = useState("");
  const [type, setType] = useState("TEXT");
  const [notNull, setNotNull] = useState(false);
  const [dflt, setDflt] = useState("");
  const [tableName, setTableName] = useState("");
  const [cols, setCols] = useState<DBColumnDef[]>([{ name: "id", type: "INTEGER", pk: true }]);
  const [indexName, setIndexName] = useState("");
  const [indexCols, setIndexCols] = useState("");
  const [unique, setUnique] = useState(false);

  useEffect(() => {
    if (!open) return;
    setName("");
    setType("TEXT");
    setNotNull(false);
    setDflt("");
    setTableName("");
    setCols([{ name: "id", type: "INTEGER", pk: true }]);
    setIndexName("");
    setIndexCols("");
    setUnique(false);
  }, [open, action]);

  const submit = () => {
    if (action === "add_column") {
      onSubmit({ column: { name, type, not_null: notNull, default: dflt === "" ? null : dflt } });
    } else if (action === "create_table") {
      onSubmit({ table: tableName, columns: cols.filter((c) => c.name.trim() !== "") });
    } else if (action === "create_index") {
      onSubmit({
        index: indexName,
        indexColumns: indexCols
          .split(",")
          .map((s) => s.trim())
          .filter(Boolean),
        unique,
      });
    }
  };

  const title =
    action === "add_column"
      ? `Add column · ${schema?.name ?? ""}`
      : action === "create_table"
        ? "Create table"
        : action === "create_index"
          ? `Create index · ${schema?.name ?? ""}`
          : "";

  return (
    <Dialog open={open} onOpenChange={(o) => !o && onCancel()}>
      <DialogContent className="max-w-lg" data-testid="sqlite-schema-dialog">
        <DialogHeader>
          <DialogTitle className="text-sm">{title}</DialogTitle>
        </DialogHeader>

        {action === "add_column" && (
          <div className="space-y-2">
            <label className="block text-[11px] text-muted-foreground">
              Name
              <input
                value={name}
                onChange={(e) => setName(e.target.value)}
                aria-label="Column name"
                data-testid="db-col-name"
                className={INPUT}
              />
            </label>
            <label className="block text-[11px] text-muted-foreground">
              Type
              <input
                value={type}
                onChange={(e) => setType(e.target.value)}
                list="sqlite-type-options"
                aria-label="Column type"
                className={INPUT}
              />
            </label>
            <label className="block text-[11px] text-muted-foreground">
              Default (optional)
              <input
                value={dflt}
                onChange={(e) => setDflt(e.target.value)}
                aria-label="Column default"
                className={INPUT}
              />
            </label>
            <label className="flex items-center gap-2 text-xs">
              <input type="checkbox" checked={notNull} onChange={(e) => setNotNull(e.target.checked)} />
              Not null (requires a default)
            </label>
          </div>
        )}

        {action === "create_table" && (
          <div className="space-y-2">
            <label className="block text-[11px] text-muted-foreground">
              Table name
              <input
                value={tableName}
                onChange={(e) => setTableName(e.target.value)}
                aria-label="Table name"
                data-testid="db-table-name"
                className={INPUT}
              />
            </label>
            <div className="space-y-1">
              {cols.map((c, i) => (
                <div key={i} className="flex items-center gap-1">
                  <input
                    value={c.name}
                    onChange={(e) =>
                      setCols((cs) => cs.map((x, j) => (j === i ? { ...x, name: e.target.value } : x)))
                    }
                    placeholder="name"
                    aria-label={`Column ${i + 1} name`}
                    className={INPUT}
                  />
                  <input
                    value={c.type ?? ""}
                    onChange={(e) =>
                      setCols((cs) => cs.map((x, j) => (j === i ? { ...x, type: e.target.value } : x)))
                    }
                    list="sqlite-type-options"
                    placeholder="type"
                    aria-label={`Column ${i + 1} type`}
                    className={INPUT}
                  />
                  <label className="flex shrink-0 items-center gap-1 text-[10px]">
                    <input
                      type="checkbox"
                      checked={!!c.pk}
                      onChange={(e) =>
                        setCols((cs) => cs.map((x, j) => (j === i ? { ...x, pk: e.target.checked } : x)))
                      }
                    />
                    pk
                  </label>
                  <button
                    type="button"
                    aria-label={`Remove column ${i + 1}`}
                    onClick={() => setCols((cs) => cs.filter((_, j) => j !== i))}
                    className="shrink-0 rounded p-0.5 hover:bg-muted"
                  >
                    <Trash2 className="h-3 w-3" />
                  </button>
                </div>
              ))}
              <button
                type="button"
                onClick={() => setCols((cs) => [...cs, { name: "", type: "TEXT" }])}
                className="inline-flex items-center gap-1 text-[11px] text-muted-foreground hover:text-foreground"
              >
                <Plus className="h-3 w-3" /> Add column
              </button>
            </div>
          </div>
        )}

        {action === "create_index" && (
          <div className="space-y-2">
            <label className="block text-[11px] text-muted-foreground">
              Index name
              <input
                value={indexName}
                onChange={(e) => setIndexName(e.target.value)}
                aria-label="Index name"
                data-testid="db-index-name"
                className={INPUT}
              />
            </label>
            <label className="block text-[11px] text-muted-foreground">
              Columns (comma separated)
              <input
                value={indexCols}
                onChange={(e) => setIndexCols(e.target.value)}
                aria-label="Index columns"
                className={INPUT}
              />
            </label>
            <label className="flex items-center gap-2 text-xs">
              <input type="checkbox" checked={unique} onChange={(e) => setUnique(e.target.checked)} />
              Unique
            </label>
          </div>
        )}

        <datalist id="sqlite-type-options">
          {TYPE_OPTIONS.map((t) => (
            <option key={t} value={t} />
          ))}
        </datalist>

        {error ? (
          <p role="alert" className="text-xs text-destructive">
            {error}
          </p>
        ) : null}
        <DialogFooter>
          <button type="button" onClick={onCancel} className={BTN}>
            Cancel
          </button>
          <button
            type="button"
            onClick={submit}
            disabled={busy}
            data-dialog-default-action
            data-testid="sqlite-schema-save"
            className={BTN_PRIMARY}
          >
            {busy ? <Loader2 className="mr-1 inline h-3 w-3 animate-spin" /> : null}
            Apply
          </button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
