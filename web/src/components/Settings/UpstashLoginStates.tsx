import { useCallback, useEffect, useState } from "react";
import { api } from "../../api/client";
import type { UpstashLoginState } from "../../api/types";
import { Button } from "../ui/button";
import { Input } from "../ui/input";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "../ui/select";
import ConfirmDialog from "../common/ConfirmDialog";

// Settings → Upstash Box → Login states. A login state is the cookies and site
// storage of a box browser, saved on this machine. Each project is assigned at
// most one. The agent cannot choose a login state; only this form changes one.
//
// Invariants this component owns:
//  - The list shows counts only. Cookie and storage values never reach the page.
//  - A new name is checked here with the same rule as the server, and a name the
//    rule refuses is never sent. The server's own error is still shown.
//  - The assignment is for the ACTIVE project. Without one, or with a remote
//    project (the server only assigns local projects), the row is disabled.
//  - Delete asks first. A 409 from the server (a live session holds the state)
//    is thrown back to ConfirmDialog, which shows the server's message and stays open.

/** The one login-state name rule, mirrored from internal/config (ValidLoginStateName). */
const NAME_PATTERN = /^[a-z0-9][a-z0-9_-]{0,39}$/;
const NONE = "none";
/** Select value for "New name…". It starts with "_", which no valid name does. */
const NEW_NAME = "__new__";
const RULE_MESSAGE =
  'Use 1 to 40 characters: a-z, 0-9, _ or -, starting with a letter or digit. "none" is reserved.';

/** The rule's message for a name, or null when the name is valid. */
function loginStateNameError(name: string): string | null {
  if (name === NONE || !NAME_PATTERN.test(name)) return RULE_MESSAGE;
  return null;
}

function errorMessage(err: unknown): string {
  return err instanceof Error ? err.message : String(err);
}

/** Bytes as a short size: "812 B" or "14.2 KB". */
function formatBytes(n: number): string {
  if (n < 1024) return `${n} B`;
  return `${(n / 1024).toFixed(1)} KB`;
}

/** A saved-at timestamp for the table, or the raw value when it does not parse. */
function formatSaved(iso: string): string {
  if (iso === "") return "—";
  const t = Date.parse(iso);
  if (!Number.isFinite(t)) return iso;
  return new Date(t).toLocaleString();
}

interface Props {
  /** The active project root. Undefined when no project is open. */
  project?: string;
  /** The active project's host. Set only for a remote project. */
  host?: string;
}

export default function UpstashLoginStates({ project, host }: Props) {
  const remote = Boolean(host);
  const canAssign = project !== undefined && project !== "" && !remote;

  const [states, setStates] = useState<UpstashLoginState[] | null>(null);
  const [loadError, setLoadError] = useState<string | null>(null);
  // The name the server has stored for the project ("" when none).
  const [assigned, setAssigned] = useState("");
  // What the select shows: NONE, a saved or assigned name, or NEW_NAME.
  const [selection, setSelection] = useState(NONE);
  const [newName, setNewName] = useState("");
  const [assignError, setAssignError] = useState<string | null>(null);
  const [saving, setSaving] = useState(false);
  const [pendingDelete, setPendingDelete] = useState<UpstashLoginState | null>(null);

  const load = useCallback(async () => {
    try {
      const res = await api.upstashListLoginStates();
      setStates(res.login_states);
      setLoadError(null);
    } catch (err) {
      setLoadError(errorMessage(err));
    }
  }, []);

  const applyAssignment = useCallback((name: string) => {
    setAssigned(name);
    setSelection(name === "" ? NONE : name);
    setNewName("");
  }, []);

  useEffect(() => {
    void load();
  }, [load]);

  useEffect(() => {
    if (!canAssign || project === undefined) return;
    let cancelled = false;
    api
      .upstashGetLoginState(project)
      .then((res) => {
        if (!cancelled) applyAssignment(res.name);
      })
      .catch((err: unknown) => {
        if (!cancelled) setAssignError(errorMessage(err));
      });
    return () => {
      cancelled = true;
    };
  }, [canAssign, project, applyAssignment]);

  const trimmedNew = newName.trim();
  const newNameError =
    selection === NEW_NAME && trimmedNew !== "" ? loginStateNameError(trimmedNew) : null;
  const nameBlocked =
    selection === NEW_NAME && (trimmedNew === "" || loginStateNameError(trimmedNew) !== null);
  const targetName = selection === NEW_NAME ? trimmedNew : selection;
  const targetStored = targetName === NONE ? "" : targetName;
  const canSave = canAssign && !saving && !nameBlocked && targetStored !== assigned;

  const save = async () => {
    if (project === undefined || !canAssign || nameBlocked) return;
    setSaving(true);
    setAssignError(null);
    try {
      const res = await api.upstashSetLoginState(project, targetName);
      applyAssignment(res.name);
    } catch (err) {
      setAssignError(errorMessage(err));
    } finally {
      setSaving(false);
    }
  };

  const confirmDelete = async () => {
    const target = pendingDelete;
    if (!target) return;
    // Throws on failure: ConfirmDialog keeps the dialog open and shows the
    // server's message, which for a 409 names the session holding the state.
    await api.upstashDeleteLoginState(target.name);
    setPendingDelete(null);
    await load();
    if (canAssign && project !== undefined) {
      const res = await api.upstashGetLoginState(project);
      applyAssignment(res.name);
    }
  };

  // A saved name list plus the name the project already holds. That name may
  // not be saved yet, and the select still has to show it.
  const savedNames = (states ?? []).map((s) => s.name);
  const assignedUnsaved = assigned !== "" && !savedNames.includes(assigned);

  return (
    <div className="space-y-3">
      <div className="space-y-1">
        <h3 className="text-sm font-semibold">Login states</h3>
        <p className="text-sm text-muted-foreground">
          Cookies and site storage of a box browser, saved on this machine. Assigned per project. The
          agent cannot choose one.
        </p>
      </div>

      <div className="space-y-2">
        {!canAssign && (
          <p className="text-sm text-muted-foreground">
            {remote
              ? "Login states are assigned to projects on this machine. Open a local project to assign one."
              : "Open a project to assign a login state"}
          </p>
        )}
        <div className="flex flex-wrap items-end gap-2">
          <div className="space-y-1">
            <label className="text-xs font-medium" htmlFor="upstash-login-state-select">
              Login state for this project
            </label>
            <Select value={selection} onValueChange={setSelection} disabled={!canAssign || saving}>
              <SelectTrigger id="upstash-login-state-select" className="h-9 w-56 text-sm">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value={NONE}>None</SelectItem>
                {assignedUnsaved && <SelectItem value={assigned}>{`${assigned} (not saved yet)`}</SelectItem>}
                {savedNames.map((name) => (
                  <SelectItem key={name} value={name}>
                    {name}
                  </SelectItem>
                ))}
                <SelectItem value={NEW_NAME}>New name…</SelectItem>
              </SelectContent>
            </Select>
          </div>
          {selection === NEW_NAME && (
            <div className="space-y-1">
              <label className="text-xs font-medium" htmlFor="upstash-login-state-new">
                New name
              </label>
              <Input
                id="upstash-login-state-new"
                aria-label="New login state name"
                aria-invalid={newNameError !== null}
                className="h-9 w-56 text-sm"
                placeholder="work-laptop"
                value={newName}
                disabled={!canAssign || saving}
                onChange={(e) => setNewName(e.target.value)}
              />
            </div>
          )}
          <Button size="sm" onClick={() => void save()} disabled={!canSave}>
            {saving ? "Saving…" : "Save"}
          </Button>
        </div>
        {newNameError && (
          <p role="alert" className="text-xs text-destructive">
            {newNameError}
          </p>
        )}
        {assignError && (
          <div role="alert" className="rounded border border-destructive/50 bg-destructive/10 p-3 text-sm">
            {assignError}
          </div>
        )}
      </div>

      {loadError && (
        <div role="alert" className="rounded border border-destructive/50 bg-destructive/10 p-3 text-sm">
          Could not load login states: {loadError}
          <div className="mt-2">
            <Button size="sm" variant="outline" onClick={() => void load()}>
              Retry
            </Button>
          </div>
        </div>
      )}

      {states === null && loadError === null && (
        <div className="text-sm text-muted-foreground">Loading login states…</div>
      )}

      {states !== null && states.length === 0 && (
        <p className="text-sm text-muted-foreground">No login states saved yet.</p>
      )}

      {states !== null && states.length > 0 && (
        <div className="overflow-x-auto rounded border border-border">
          <table className="w-full text-left text-sm">
            <caption className="sr-only">Saved login states</caption>
            <thead className="border-b border-border text-xs text-muted-foreground">
              <tr>
                <th scope="col" className="px-3 py-2 font-medium">Name</th>
                <th scope="col" className="px-3 py-2 font-medium">Saved</th>
                <th scope="col" className="px-3 py-2 font-medium">Cookies</th>
                <th scope="col" className="px-3 py-2 font-medium">Origins</th>
                <th scope="col" className="px-3 py-2 font-medium">Size</th>
                <th scope="col" className="px-3 py-2 font-medium">In use by</th>
                <th scope="col" className="px-3 py-2 font-medium">
                  <span className="sr-only">Actions</span>
                </th>
              </tr>
            </thead>
            <tbody className="divide-y divide-border">
              {states.map((s) => (
                <tr key={s.name} className="align-top">
                  <td className="px-3 py-2 font-medium text-foreground">{s.name}</td>
                  <td className="px-3 py-2 text-muted-foreground">{formatSaved(s.saved_at)}</td>
                  {s.corrupt ? (
                    <td className="px-3 py-2 text-muted-foreground" colSpan={2}>
                      unreadable
                    </td>
                  ) : (
                    <>
                      <td className="px-3 py-2">{s.cookies}</td>
                      <td className="px-3 py-2">{s.origins}</td>
                    </>
                  )}
                  <td className="px-3 py-2">{formatBytes(s.bytes)}</td>
                  <td className="px-3 py-2">
                    {s.in_use_by === "" ? (
                      "—"
                    ) : (
                      <span className="font-mono text-xs" title={s.in_use_by}>
                        {s.in_use_by}
                      </span>
                    )}
                  </td>
                  <td className="px-3 py-2">
                    <div className="flex justify-end">
                      <Button
                        size="sm"
                        variant="outline"
                        aria-label={`Delete login state ${s.name}`}
                        onClick={() => setPendingDelete(s)}
                      >
                        Delete
                      </Button>
                    </div>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}

      <ConfirmDialog
        open={pendingDelete !== null}
        title={`Delete login state ${pendingDelete?.name ?? ""}?`}
        description="Its cookies and site storage are removed from this machine. Projects assigned to it go back to a clean browser."
        confirmLabel="Delete"
        pendingLabel="Deleting…"
        onConfirm={confirmDelete}
        onCancel={() => setPendingDelete(null)}
      />
    </div>
  );
}
