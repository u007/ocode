import { useCallback, useEffect, useRef, useState } from "react";
import { api } from "../../api/client";
import type { UpstashAccount, UpstashBox, UpstashBoxKind, UpstashBoxesResponse } from "../../api/types";
import { Badge } from "../ui/badge";
import { Button } from "../ui/button";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "../ui/select";
import ConfirmDialog from "../common/ConfirmDialog";

// Settings → Upstash Box → Boxes. Lists the boxes on one account and offers
// pause / resume / delete per row. Rendered by UpstashBoxForm below the accounts.
//
// Invariants this component owns:
//  - Rows are shown only for the account that produced them (`data.account_id`
//    must match the selected account), so a row action never reaches a box of
//    a different account after the selector changes.
//  - Each request is tagged with its own sequence number; only the latest one
//    may write state, so a slow response for a previous account or page cannot
//    overwrite the current view.
//  - Polling runs only while the document is visible and stops on unmount.

/** Boxes per page. The server echoes it back as `page_size`. */
const PAGE_SIZE = 20;
/** Refresh cadence while the component is mounted and the page is visible. */
const POLL_MS = 15_000;

const KIND_LABEL: Record<UpstashBoxKind, string> = {
  ephemeral: "Ephemeral",
  persistent: "Persistent",
  retained: "Retained",
};

function errorMessage(err: unknown): string {
  return err instanceof Error ? err.message : String(err);
}

/** Calendar date for a box timestamp, in the viewer's locale. An unparseable value is shown raw. */
function formatDate(iso: string): string {
  const t = new Date(iso);
  if (!Number.isFinite(t.getTime())) return iso;
  return t.toLocaleDateString(undefined, { year: "numeric", month: "short", day: "numeric" });
}

/** The Expires cell. A retained box is deleted if unused, so its date is said in those words. */
function expiryText(box: UpstashBox, now: number): string {
  if (!box.expires_at) return "—";
  if (box.kind === "retained") return `Deleted if unused until ${formatDate(box.expires_at)}`;
  return formatRelative(box.expires_at, now);
}

/** Shortens a long session id for the table; the full id stays in the title. */
function shortId(id: string): string {
  return id.length > 12 ? `${id.slice(0, 8)}…` : id;
}

/** Relative time for a box timestamp: "in 42 min", "in 3 h", "2 d ago". */
function formatRelative(iso: string, now: number): string {
  const t = Date.parse(iso);
  // Show the raw value rather than "NaN min" if the server ever sends a bad timestamp.
  if (!Number.isFinite(t)) return iso;
  const future = t > now;
  const minutes = Math.round(Math.abs(t - now) / 60_000);
  if (minutes < 1) return future ? "in <1 min" : "just now";
  let label: string;
  if (minutes < 60) label = `${minutes} min`;
  else if (minutes < 48 * 60) label = `${Math.round(minutes / 60)} h`;
  else label = `${Math.round(minutes / (24 * 60))} d`;
  return future ? `in ${label}` : `${label} ago`;
}

export default function UpstashBoxesList({ accounts }: { accounts: UpstashAccount[] }) {
  const [selectedId, setSelectedId] = useState<string | null>(null);
  const [page, setPage] = useState(1);
  const [data, setData] = useState<UpstashBoxesResponse | null>(null);
  const [loadError, setLoadError] = useState<string | null>(null);
  const [actionError, setActionError] = useState<string | null>(null);
  // Ids of boxes with a request in flight. A set, not one id: a second row's
  // action must not re-enable the first row's buttons while its request runs.
  const [busyIds, setBusyIds] = useState<ReadonlySet<string>>(() => new Set<string>());
  const [pendingDelete, setPendingDelete] = useState<UpstashBox | null>(null);
  const requestSeq = useRef(0);

  // The selected account, else the default one. The selector only appears with
  // more than one account, so with one account this is always that account.
  const active: UpstashAccount | undefined =
    accounts.find((a) => a.id === selectedId) ?? accounts.find((a) => a.is_default) ?? accounts[0];
  const accountId = active?.id;

  // Rows from another account (before the selector or default changed) are not shown.
  const shown = data !== null && accountId !== undefined && data.account_id === accountId ? data : null;

  const load = useCallback(async () => {
    if (accountId === undefined) return;
    const seq = ++requestSeq.current;
    try {
      const res = await api.upstashListBoxes(accountId, page, PAGE_SIZE);
      if (seq !== requestSeq.current) return;
      setData(res);
      setLoadError(null);
      // The last box of the last page was deleted: step back rather than sit on an empty page.
      if (res.boxes.length === 0 && page > 1) setPage(page - 1);
    } catch (err) {
      if (seq !== requestSeq.current) return;
      setLoadError(errorMessage(err));
    }
  }, [accountId, page]);

  useEffect(() => {
    void load();
    const timer = setInterval(() => {
      if (document.visibilityState === "visible") void load();
    }, POLL_MS);
    return () => clearInterval(timer);
  }, [load]);

  const onAccountChange = (id: string) => {
    setSelectedId(id);
    setPage(1);
    setLoadError(null);
    setActionError(null);
  };

  const markBusy = (id: string, busy: boolean) => {
    setBusyIds((prev) => {
      const next = new Set(prev);
      if (busy) next.add(id);
      else next.delete(id);
      return next;
    });
  };

  // Pause / resume: a failure shows inline above the table; success re-lists.
  const runRowAction = async (box: UpstashBox, fn: (acct: string) => Promise<void>) => {
    if (!shown) return;
    markBusy(box.id, true);
    setActionError(null);
    try {
      await fn(shown.account_id);
      await load();
    } catch (err) {
      setActionError(errorMessage(err));
    } finally {
      markBusy(box.id, false);
    }
  };

  const now = Date.now();
  const totalPages = shown ? Math.max(1, Math.ceil(shown.total / shown.page_size)) : 1;

  return (
    <div className="space-y-3">
      <div className="flex flex-wrap items-end justify-between gap-3">
        <h3 className="text-sm font-semibold">Boxes</h3>
        <div className="flex items-end gap-2">
          {accounts.length > 1 && (
            <div className="space-y-1">
              <label className="text-xs font-medium" htmlFor="upstash-boxes-account">
                Account
              </label>
              <Select value={accountId} onValueChange={onAccountChange}>
                <SelectTrigger id="upstash-boxes-account" className="h-9 text-sm">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {accounts.map((a) => (
                    <SelectItem key={a.id} value={a.id}>
                      {a.name}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
          )}
          <Button size="sm" variant="outline" onClick={() => void load()} disabled={busyIds.size > 0}>
            Refresh
          </Button>
        </div>
      </div>

      {loadError && (
        <div role="alert" className="rounded border border-destructive/50 bg-destructive/10 p-3 text-sm">
          Could not load boxes: {loadError}
          <div className="mt-2">
            <Button size="sm" variant="outline" onClick={() => void load()}>
              Retry
            </Button>
          </div>
        </div>
      )}

      {actionError && (
        <div role="alert" className="rounded border border-destructive/50 bg-destructive/10 p-3 text-sm">
          {actionError}
        </div>
      )}

      {shown === null && loadError === null && (
        <div className="text-sm text-muted-foreground">Loading boxes…</div>
      )}

      {shown !== null && shown.boxes.length === 0 && (
        <p className="text-sm text-muted-foreground">No boxes on this account.</p>
      )}

      {shown !== null && shown.boxes.length > 0 && (
        <>
          <div className="overflow-x-auto rounded border border-border">
            <table className="w-full text-left text-sm">
              <caption className="sr-only">Upstash boxes on this account</caption>
              <thead className="border-b border-border text-xs text-muted-foreground">
                <tr>
                  <th scope="col" className="px-3 py-2 font-medium">Name</th>
                  <th scope="col" className="px-3 py-2 font-medium">Status</th>
                  <th scope="col" className="px-3 py-2 font-medium">Size</th>
                  <th scope="col" className="px-3 py-2 font-medium">Kind</th>
                  <th scope="col" className="px-3 py-2 font-medium">Login state</th>
                  <th scope="col" className="px-3 py-2 font-medium">Session</th>
                  <th scope="col" className="px-3 py-2 font-medium">Last used</th>
                  <th scope="col" className="px-3 py-2 font-medium">Expires</th>
                  <th scope="col" className="sticky right-0 border-l border-border bg-background px-3 py-2 font-medium">
                    <span className="sr-only">Actions</span>
                  </th>
                </tr>
              </thead>
              <tbody className="divide-y divide-border">
                {shown.boxes.map((box) => {
                  const busy = busyIds.has(box.id);
                  // Every kind but ephemeral can be paused: a retained box is paused by ocode itself.
                  const canPause =
                    box.kind !== "ephemeral" && (box.status === "running" || box.status === "idle");
                  // A retained box is resumed by a browser start that uses its login state,
                  // never by this button: a resume here would leave its journal row paused.
                  const canResume = box.status === "paused" && box.kind !== "retained";
                  return (
                    <tr key={box.id} className="align-top">
                      <td className="min-w-[10rem] px-3 py-2">
                        <div className="font-medium text-foreground">{box.name}</div>
                        <div className="font-mono text-xs text-muted-foreground break-all">{box.id}</div>
                      </td>
                      <td className="whitespace-nowrap px-3 py-2">{box.status}</td>
                      <td className="whitespace-nowrap px-3 py-2">{box.size}</td>
                      <td className="whitespace-nowrap px-3 py-2">
                        {box.kind === "retained" ? (
                          <Badge variant="secondary">{KIND_LABEL.retained}</Badge>
                        ) : (
                          KIND_LABEL[box.kind]
                        )}
                      </td>
                      <td className="px-3 py-2">{box.login_state || "—"}</td>
                      <td className="px-3 py-2">
                        {box.session_id ? (
                          <span className="font-mono text-xs" title={box.session_id}>
                            {shortId(box.session_id)}
                          </span>
                        ) : (
                          "—"
                        )}
                        {!box.ours && (
                          <p className="text-xs text-muted-foreground">not created by ocode here</p>
                        )}
                      </td>
                      <td className="px-3 py-2 text-muted-foreground">
                        {box.last_used ? formatRelative(box.last_used, now) : "—"}
                      </td>
                      <td className="px-3 py-2 text-muted-foreground">
                        {expiryText(box, now)}
                      </td>
                      <td className="sticky right-0 border-l border-border bg-background px-3 py-2">
                        <div className="flex justify-end gap-2">
                          {canPause && (
                            <Button
                              size="sm"
                              variant="outline"
                              disabled={busy}
                              aria-label={`Pause ${box.name}`}
                              onClick={() =>
                                void runRowAction(box, (acct) => api.upstashPauseBox(box.id, acct))
                              }
                            >
                              Pause
                            </Button>
                          )}
                          {canResume && (
                            <Button
                              size="sm"
                              variant="outline"
                              disabled={busy}
                              aria-label={`Resume ${box.name}`}
                              onClick={() =>
                                void runRowAction(box, (acct) => api.upstashResumeBox(box.id, acct))
                              }
                            >
                              Resume
                            </Button>
                          )}
                          <Button
                            size="sm"
                            variant="outline"
                            disabled={busy}
                            aria-label={`Delete ${box.name}`}
                            onClick={() => setPendingDelete(box)}
                          >
                            Delete
                          </Button>
                        </div>
                      </td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
          </div>

          <div className="flex items-center justify-between text-xs text-muted-foreground">
            <span>
              Page {page} of {totalPages}
            </span>
            <div className="flex gap-2">
              <Button
                size="sm"
                variant="outline"
                disabled={page <= 1}
                onClick={() => setPage((p) => p - 1)}
              >
                Prev
              </Button>
              <Button
                size="sm"
                variant="outline"
                disabled={page >= totalPages}
                onClick={() => setPage((p) => p + 1)}
              >
                Next
              </Button>
            </div>
          </div>
        </>
      )}

      <ConfirmDialog
        open={pendingDelete !== null}
        title={`Delete ${pendingDelete?.name ?? "this box"}?`}
        description="The box is deleted permanently. This cannot be undone."
        confirmLabel="Delete"
        pendingLabel="Deleting…"
        onConfirm={async () => {
          const box = pendingDelete;
          if (!box || !shown) return;
          markBusy(box.id, true);
          try {
            // Throws on failure: ConfirmDialog keeps the dialog open and shows why.
            await api.upstashDeleteBox(box.id, shown.account_id);
            setPendingDelete(null);
            await load();
          } finally {
            markBusy(box.id, false);
          }
        }}
        onCancel={() => setPendingDelete(null)}
      />
    </div>
  );
}
