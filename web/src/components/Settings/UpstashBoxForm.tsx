import { useCallback, useEffect, useState } from "react";
import { Plus } from "lucide-react";
import { api } from "../../api/client";
import type {
  UpstashAccount,
  UpstashAccountInput,
  UpstashBackend,
  UpstashBoxSize,
} from "../../api/types";
import { Badge } from "../ui/badge";
import { Button } from "../ui/button";
import { Input } from "../ui/input";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "../ui/dialog";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "../ui/select";
import ConfirmDialog from "../common/ConfirmDialog";

// Settings → Upstash Box. Manages one or more Upstash accounts and the default
// browser backend. Mirrors VaultForm (list + Add/Edit dialog) and ConnectorsForm
// (confirm-gated delete, re-list after every mutation instead of patching state).
//
// Invariants this component owns:
//  - The raw API key is never shown. The list renders `key_masked` only, and the
//    key field is always empty on Edit.
//  - On Edit, an empty key field means "keep the stored key": `api_key` is then
//    ABSENT from the request body, not sent as an empty string.
//  - Bounds are checked here before any request is sent. The server's own error
//    is still shown when it rejects something the form let through.

/** Upper bound for the idle timeout and the hard cap, in minutes (3 days). */
const MAX_MINUTES = 4320;
/** Upper bound for the retained-box period, in days. */
const MAX_RETAIN_DAYS = 365;
/** Upper bound for an API key, in characters. Keys never contain spaces. */
const MAX_API_KEY_LENGTH = 512;

const SIZE_OPTIONS: { value: UpstashBoxSize; label: string }[] = [
  { value: "small", label: "Small" },
  { value: "medium", label: "Medium" },
  { value: "large", label: "Large" },
];

const SIZE_LABEL: Record<UpstashBoxSize, string> = {
  small: "Small",
  medium: "Medium",
  large: "Large",
};

interface Draft {
  name: string;
  apiKey: string;
  size: UpstashBoxSize;
  // Held as text so an empty or half-typed number is representable; parsed on save.
  idle: string;
  hardCap: string;
  retainDays: string;
}

type DraftField = "name" | "apiKey" | "idle" | "hardCap" | "retainDays";
type DraftErrors = Partial<Record<DraftField, string>>;

const EMPTY_DRAFT: Draft = {
  name: "",
  apiKey: "",
  size: "small",
  idle: "15",
  hardCap: "120",
  retainDays: "14",
};

function errorMessage(err: unknown): string {
  return err instanceof Error ? err.message : String(err);
}

/** A whole, non-negative number from user text, or null when it is not one. */
function parseWhole(text: string): number | null {
  const t = text.trim();
  return /^\d+$/.test(t) ? Number(t) : null;
}

function validateDraft(d: Draft, editing: boolean): DraftErrors {
  const errors: DraftErrors = {};
  if (!d.name.trim()) errors.name = "Enter a name.";

  // On Edit an empty key means "keep the stored key", so it is only checked
  // when the user typed one.
  const key = d.apiKey.trim();
  if (!key && !editing) {
    errors.apiKey = "Enter an API key.";
  } else if (key) {
    if (/\s/.test(key)) errors.apiKey = "An API key cannot contain spaces.";
    else if (key.length > MAX_API_KEY_LENGTH) {
      errors.apiKey = `An API key must be ${MAX_API_KEY_LENGTH} characters or fewer.`;
    }
  }

  const idle = parseWhole(d.idle);
  const cap = parseWhole(d.hardCap);
  const idleRange = `Enter whole minutes from 1 to ${MAX_MINUTES}.`;
  if (idle === null || idle < 1 || idle > MAX_MINUTES) {
    errors.idle = idleRange;
  } else if (cap !== null && cap >= 1 && cap <= MAX_MINUTES && idle > cap) {
    // Compared only when both are valid, so the two messages never compete.
    errors.idle = "The idle timeout cannot be longer than the hard cap.";
  }

  if (cap === null || cap < 1 || cap > MAX_MINUTES) {
    errors.hardCap = idleRange;
  }

  const retain = parseWhole(d.retainDays);
  if (retain === null || retain < 1 || retain > MAX_RETAIN_DAYS) {
    errors.retainDays = `Enter whole days from 1 to ${MAX_RETAIN_DAYS}.`;
  }
  return errors;
}

/** Builds the request body. `api_key` is added only when the user typed one. */
function draftToBody(d: Draft): UpstashAccountInput {
  const body: UpstashAccountInput = {
    name: d.name.trim(),
    size: d.size,
    idle_minutes: Number(d.idle.trim()),
    hard_cap_minutes: Number(d.hardCap.trim()),
    retain_days: Number(d.retainDays.trim()),
  };
  const key = d.apiKey.trim();
  if (key) body.api_key = key;
  return body;
}

function accountToDraft(a: UpstashAccount): Draft {
  return {
    name: a.name,
    apiKey: "",
    size: a.size,
    idle: String(a.idle_minutes),
    hardCap: String(a.hard_cap_minutes),
    retainDays: String(a.retain_days),
  };
}

export default function UpstashBoxForm() {
  const [accounts, setAccounts] = useState<UpstashAccount[]>([]);
  const [defaultBackend, setDefaultBackend] = useState<UpstashBackend>("local");
  const [loading, setLoading] = useState(true);
  const [loadError, setLoadError] = useState<string | null>(null);
  const [actionError, setActionError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  const [dialogOpen, setDialogOpen] = useState(false);
  const [editingId, setEditingId] = useState<string | null>(null);
  const [editingMasked, setEditingMasked] = useState<string>("");
  const [draft, setDraft] = useState<Draft>(EMPTY_DRAFT);
  const [draftErrors, setDraftErrors] = useState<DraftErrors>({});
  const [saveError, setSaveError] = useState<string | null>(null);
  const [saving, setSaving] = useState(false);

  const [pendingDelete, setPendingDelete] = useState<UpstashAccount | null>(null);

  const load = useCallback(async () => {
    setLoadError(null);
    try {
      const res = await api.upstashListAccounts();
      setAccounts(res.accounts ?? []);
      setDefaultBackend(res.default_backend);
    } catch (err) {
      // Deliberately shown, not swallowed: an empty list would read as "no
      // account is set up", which is a different and untrue state.
      setLoadError(errorMessage(err));
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    void load();
  }, [load]);

  // Every mutation goes through here: one place that clears and sets the
  // action error and re-lists the accounts, so the UI reflects the server.
  const runAction = async (fn: () => Promise<unknown>) => {
    setBusy(true);
    setActionError(null);
    try {
      await fn();
      await load();
    } catch (err) {
      setActionError(errorMessage(err));
    } finally {
      setBusy(false);
    }
  };

  const chooseBackend = (next: UpstashBackend) => {
    if (next === effectiveBackend) return;
    void runAction(() => api.upstashUpdateSettings({ default_backend: next }));
  };

  const makeDefault = (account: UpstashAccount) =>
    runAction(() => api.upstashUpdateSettings({ default_account_id: account.id }));

  const openAdd = () => {
    setEditingId(null);
    setEditingMasked("");
    setDraft(EMPTY_DRAFT);
    setDraftErrors({});
    setSaveError(null);
    setDialogOpen(true);
  };

  const openEdit = (account: UpstashAccount) => {
    setEditingId(account.id);
    setEditingMasked(account.key_masked);
    setDraft(accountToDraft(account));
    setDraftErrors({});
    setSaveError(null);
    setDialogOpen(true);
  };

  // Every close path (Save success, Cancel, Escape, overlay click) lands here.
  // The typed API key is part of the draft, so it is wiped with the rest
  // rather than kept until the next open.
  const closeDialog = () => {
    setDialogOpen(false);
    setEditingId(null);
    setEditingMasked("");
    setDraft(EMPTY_DRAFT);
    setDraftErrors({});
    setSaveError(null);
  };

  const updateDraft = (patch: Partial<Draft>) => {
    setDraft((d) => ({ ...d, ...patch }));
  };

  const save = async () => {
    const editing = editingId !== null;
    const errors = validateDraft(draft, editing);
    setDraftErrors(errors);
    setSaveError(null);
    if (Object.keys(errors).length > 0) return;

    setSaving(true);
    try {
      const body = draftToBody(draft);
      if (editingId !== null) {
        await api.upstashUpdateAccount(editingId, body);
      } else {
        await api.upstashCreateAccount(body);
      }
      closeDialog();
      await load();
    } catch (err) {
      // Stay open with the server's reason, so the user can correct and retry.
      setSaveError(errorMessage(err));
    } finally {
      setSaving(false);
    }
  };

  const noAccounts = !loading && !loadError && accounts.length === 0;
  // With no accounts the server resets the backend to local (it does so when the
  // last account is deleted), so Local is the effective choice whatever the
  // stored value says. Upstash is then disabled, never shown as selected.
  const effectiveBackend: UpstashBackend = accounts.length === 0 ? "local" : defaultBackend;

  const fieldError = (field: DraftField) => {
    const message = draftErrors[field];
    return message ? (
      <p id={`upstash-${field}-error`} className="text-xs text-destructive">
        {message}
      </p>
    ) : null;
  };

  const describedBy = (field: DraftField, extra?: string) => {
    const ids: string[] = [];
    if (extra) ids.push(extra);
    if (draftErrors[field]) ids.push(`upstash-${field}-error`);
    return ids.length > 0 ? ids.join(" ") : undefined;
  };

  return (
    <section className="p-6 max-w-2xl space-y-4">
      <div className="flex items-start justify-between gap-3">
        <div className="space-y-1">
          <h2 className="text-lg font-semibold">Upstash Box</h2>
          <p className="text-sm text-muted-foreground">
            Boxes are created only when a session starts one, and are deleted when idle. Usage is
            billed by Upstash.
          </p>
        </div>
        {!loadError && (
          <Button onClick={openAdd} disabled={busy || loading} className="shrink-0">
            <Plus className="w-4 h-4 mr-1" aria-hidden="true" />
            Add
          </Button>
        )}
      </div>

      {loadError && (
        <div role="alert" className="rounded border border-destructive/50 bg-destructive/10 p-3 text-sm">
          Could not load Upstash accounts: {loadError}
          <div className="mt-2">
            <Button size="sm" variant="outline" onClick={() => void load()}>
              Retry
            </Button>
          </div>
        </div>
      )}

      {loading && <div className="text-sm text-muted-foreground">Loading accounts…</div>}

      {!loading && !loadError && (
        <>
          {actionError && (
            <div role="alert" className="rounded border border-destructive/50 bg-destructive/10 p-3 text-sm">
              {actionError}
            </div>
          )}

          <div className="space-y-1">
            <div id="upstash-backend-label" className="text-sm font-medium">
              Default browser backend
            </div>
            <div
              role="group"
              aria-labelledby="upstash-backend-label"
              aria-describedby={noAccounts ? "upstash-backend-reason" : undefined}
              className="flex gap-2"
            >
              <Button
                size="sm"
                variant={effectiveBackend === "local" ? "default" : "outline"}
                aria-pressed={effectiveBackend === "local"}
                disabled={busy}
                onClick={() => chooseBackend("local")}
              >
                Local
              </Button>
              <Button
                size="sm"
                variant={effectiveBackend === "upstash" ? "default" : "outline"}
                aria-pressed={effectiveBackend === "upstash"}
                disabled={busy || noAccounts}
                onClick={() => chooseBackend("upstash")}
              >
                Upstash
              </Button>
            </div>
            {noAccounts && (
              <p id="upstash-backend-reason" className="text-xs text-muted-foreground">
                Add an account before choosing Upstash.
              </p>
            )}
          </div>

          {noAccounts ? (
            <p className="text-sm text-muted-foreground">No Upstash account is set up.</p>
          ) : (
            <ul aria-label="Upstash accounts" className="divide-y divide-border rounded border border-border">
              {accounts.map((a) => (
                <li key={a.id} className="flex flex-wrap items-start justify-between gap-3 p-3">
                  <div className="min-w-0 space-y-1">
                    <div className="flex items-center gap-2">
                      <span className="text-sm font-medium text-foreground">{a.name}</span>
                      {a.is_default && <Badge variant="secondary">Default</Badge>}
                    </div>
                    <div className="font-mono text-xs text-muted-foreground">{a.key_masked}</div>
                    <div className="text-xs text-muted-foreground">
                      {SIZE_LABEL[a.size] ?? a.size} box · Idle timeout {a.idle_minutes} min · Hard cap{" "}
                      {a.hard_cap_minutes} min · Unused retained boxes deleted after {a.retain_days} days
                    </div>
                  </div>
                  <div className="flex shrink-0 gap-2">
                    {!a.is_default && (
                      <Button
                        size="sm"
                        variant="outline"
                        disabled={busy}
                        aria-label={`Make default for ${a.name}`}
                        onClick={() => void makeDefault(a)}
                      >
                        Make default
                      </Button>
                    )}
                    <Button
                      size="sm"
                      variant="outline"
                      disabled={busy}
                      aria-label={`Edit ${a.name}`}
                      onClick={() => openEdit(a)}
                    >
                      Edit
                    </Button>
                    <Button
                      size="sm"
                      variant="outline"
                      disabled={busy}
                      aria-label={`Delete ${a.name}`}
                      onClick={() => setPendingDelete(a)}
                    >
                      Delete
                    </Button>
                  </div>
                </li>
              ))}
            </ul>
          )}
        </>
      )}

      <Dialog
        open={dialogOpen}
        onOpenChange={(open) => {
          if (!open) closeDialog();
        }}
      >
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{editingId ? "Edit Upstash account" : "Add Upstash account"}</DialogTitle>
            <DialogDescription>
              The API key is stored on the server and shown masked afterwards.
            </DialogDescription>
          </DialogHeader>

          <form
            noValidate
            className="space-y-3"
            onSubmit={(e) => {
              e.preventDefault();
              void save();
            }}
          >
            <div className="space-y-1">
              <label className="text-xs font-medium" htmlFor="upstash-name">
                Name
              </label>
              <Input
                id="upstash-name"
                value={draft.name}
                aria-invalid={draftErrors.name ? true : undefined}
                aria-describedby={describedBy("name")}
                onChange={(e) => updateDraft({ name: e.target.value })}
              />
              {fieldError("name")}
            </div>

            <div className="space-y-1">
              <label className="text-xs font-medium" htmlFor="upstash-api-key">
                API key
              </label>
              <Input
                id="upstash-api-key"
                type="password"
                autoComplete="off"
                spellCheck={false}
                placeholder={editingId ? editingMasked : undefined}
                value={draft.apiKey}
                aria-invalid={draftErrors.apiKey ? true : undefined}
                aria-describedby={describedBy(
                  "apiKey",
                  editingId ? "upstash-api-key-help" : undefined,
                )}
                onChange={(e) => updateDraft({ apiKey: e.target.value })}
              />
              {editingId && (
                <p id="upstash-api-key-help" className="text-xs text-muted-foreground">
                  Leave blank to keep the stored key.
                </p>
              )}
              {fieldError("apiKey")}
            </div>

            <div className="space-y-1">
              <label className="text-xs font-medium" htmlFor="upstash-size">
                Box size
              </label>
              <Select value={draft.size} onValueChange={(v) => updateDraft({ size: v as UpstashBoxSize })}>
                <SelectTrigger id="upstash-size" className="h-9 text-sm">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {SIZE_OPTIONS.map((o) => (
                    <SelectItem key={o.value} value={o.value}>
                      {o.label}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>

            {/* Two columns, top-aligned: a helper or error under one field must not
                push the other field's input down. */}
            <div className="grid grid-cols-2 items-start gap-3">
              <div className="space-y-1">
                <label className="text-xs font-medium" htmlFor="upstash-idle">
                  Idle timeout (min)
                </label>
                <Input
                  id="upstash-idle"
                  type="number"
                  inputMode="numeric"
                  min={1}
                  value={draft.idle}
                  aria-invalid={draftErrors.idle ? true : undefined}
                  aria-describedby={describedBy("idle")}
                  onChange={(e) => updateDraft({ idle: e.target.value })}
                />
                {fieldError("idle")}
              </div>

              <div className="space-y-1">
                <label className="text-xs font-medium" htmlFor="upstash-hard-cap">
                  Hard cap (min)
                </label>
                <Input
                  id="upstash-hard-cap"
                  type="number"
                  inputMode="numeric"
                  min={1}
                  max={MAX_MINUTES}
                  value={draft.hardCap}
                  aria-invalid={draftErrors.hardCap ? true : undefined}
                  aria-describedby={describedBy("hardCap", "upstash-hard-cap-help")}
                  onChange={(e) => updateDraft({ hardCap: e.target.value })}
                />
                <p id="upstash-hard-cap-help" className="text-xs text-muted-foreground">
                  Maximum 3 days
                </p>
                {fieldError("hardCap")}
              </div>
            </div>

            <div className="space-y-1">
              <label className="text-xs font-medium" htmlFor="upstash-retain-days">
                Delete retained boxes after (days)
              </label>
              <Input
                id="upstash-retain-days"
                type="number"
                inputMode="numeric"
                min={1}
                max={MAX_RETAIN_DAYS}
                value={draft.retainDays}
                aria-invalid={draftErrors.retainDays ? true : undefined}
                aria-describedby={describedBy("retainDays", "upstash-retain-days-help")}
                onChange={(e) => updateDraft({ retainDays: e.target.value })}
              />
              <p id="upstash-retain-days-help" className="text-xs text-muted-foreground">
                A retained box that is not used for this long is deleted.
              </p>
              {fieldError("retainDays")}
            </div>

            {saveError && (
              <div role="alert" className="rounded border border-destructive/50 bg-destructive/10 p-2 text-sm">
                {saveError}
              </div>
            )}

            <DialogFooter>
              <Button type="button" variant="outline" onClick={closeDialog}>
                Cancel
              </Button>
              <Button type="submit" disabled={saving}>
                {saving ? "Saving…" : "Save"}
              </Button>
            </DialogFooter>
          </form>
        </DialogContent>
      </Dialog>

      <ConfirmDialog
        open={pendingDelete !== null}
        title={`Delete ${pendingDelete?.name ?? "this account"}?`}
        description="The account and its stored API key are removed. This cannot be undone."
        confirmLabel="Delete"
        pendingLabel="Deleting…"
        onConfirm={async () => {
          if (!pendingDelete) return;
          // Throws on failure: ConfirmDialog keeps the dialog open and shows why.
          await api.upstashDeleteAccount(pendingDelete.id);
          setPendingDelete(null);
          await load();
        }}
        onCancel={() => setPendingDelete(null)}
      />
    </section>
  );
}
