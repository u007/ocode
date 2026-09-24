import { useCallback, useEffect, useState } from "react";
import { api } from "../../api/client";
import type { VaultItemMeta } from "../../api/types";
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
import { Copy, Eye, Loader2, Lock, Pencil, Plus, Trash2, Wand2 } from "lucide-react";

// The Settings surface id. The server tracks unlock state per surface, so this
// must match the id used by the api.vault* calls below.
const SURFACE = "settings";

interface Draft {
  site: string;
  url: string;
  title: string;
  username: string;
  password: string;
  notes: string;
}

const EMPTY_DRAFT: Draft = {
  site: "",
  url: "",
  title: "",
  username: "",
  password: "",
  notes: "",
};

/**
 * Settings → Passwords. Server-side password vault management: create/unlock,
 * list, reveal, add/edit/delete, generate, lock. The master password lives only
 * in the two inputs (and is cleared after init/unlock); revealed passwords are
 * transient per-row state, never persisted client-side.
 */
export default function VaultForm() {
  const [status, setStatus] = useState<{ exists: boolean; unlocked: boolean } | null>(null);
  const [items, setItems] = useState<VaultItemMeta[]>([]);
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const [master, setMaster] = useState("");
  const [confirm, setConfirm] = useState("");
  const [revealed, setRevealed] = useState<Record<string, string>>({});

  const [dialogOpen, setDialogOpen] = useState(false);
  const [editingId, setEditingId] = useState<string | null>(null);
  const [draft, setDraft] = useState<Draft>(EMPTY_DRAFT);

  const loadItems = useCallback(async () => {
    try {
      // An explicit sort is sent (not omitted): the server rejects unknown sort
      // keys rather than defaulting, so the client states its intent.
      const res = await api.vaultList(SURFACE, { sort: "site" });
      setItems(res.items);
    } catch (e) {
      console.error("vault: list", e);
      setError(e instanceof Error ? e.message : String(e));
    }
  }, []);

  const loadStatus = useCallback(async () => {
    try {
      const s = await api.vaultStatus(SURFACE);
      setStatus(s);
      if (s.unlocked) {
        await loadItems();
      }
    } catch (e) {
      console.error("vault: status", e);
      setError(e instanceof Error ? e.message : String(e));
    }
  }, [loadItems]);

  useEffect(() => {
    void loadStatus();
  }, [loadStatus]);

  // Every action routes through here: one place that surfaces + logs failures
  // and always clears `busy`.
  const run = async (op: string, fn: () => Promise<void>) => {
    setBusy(true);
    setError(null);
    try {
      await fn();
    } catch (e) {
      console.error(`vault: ${op}`, e);
      setError(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy(false);
    }
  };

  const createVault = () =>
    run("init", async () => {
      await api.vaultInit(master, SURFACE);
      setMaster("");
      setConfirm("");
      await loadStatus();
    });

  const unlockVault = () =>
    run("unlock", async () => {
      await api.vaultUnlock(master, SURFACE);
      setMaster("");
      await loadStatus();
    });

  const lockVault = () =>
    run("lock", async () => {
      await api.vaultLock(SURFACE);
      setMaster("");
      setRevealed({});
      setItems([]);
      setStatus((s) => (s ? { ...s, unlocked: false } : s));
    });

  const openAdd = () => {
    setEditingId(null);
    setDraft(EMPTY_DRAFT);
    setDialogOpen(true);
  };

  const openEdit = (id: string) =>
    run("edit", async () => {
      const it = await api.vaultReveal(id, SURFACE);
      setEditingId(id);
      setDraft({
        site: it.site,
        url: it.url,
        title: it.title,
        username: it.username,
        password: it.password,
        notes: it.notes,
      });
      setDialogOpen(true);
    });

  const saveDraft = () =>
    run("save", async () => {
      if (editingId) {
        await api.vaultUpdate(editingId, draft, SURFACE);
      } else {
        await api.vaultCreate(draft, SURFACE);
      }
      setDialogOpen(false);
      await loadItems();
    });

  const revealItem = (id: string) =>
    run("reveal", async () => {
      const it = await api.vaultReveal(id, SURFACE);
      setRevealed((r) => ({ ...r, [id]: it.password }));
    });

  const copyItem = (id: string) =>
    run("copy", async () => {
      let password = revealed[id];
      if (password === undefined) {
        password = (await api.vaultReveal(id, SURFACE)).password;
        setRevealed((r) => ({ ...r, [id]: password as string }));
      }
      // jsdom has no clipboard; the optional call keeps the UI inert there.
      await navigator.clipboard?.writeText(password);
    });

  const deleteItem = (id: string) =>
    run("delete", async () => {
      await api.vaultDelete(id, SURFACE);
      setRevealed((r) => {
        const next = { ...r };
        delete next[id];
        return next;
      });
      await loadItems();
    });

  const generatePassword = () =>
    run("generate", async () => {
      const { password } = await api.vaultGenerate({
        length: 20,
        upper: true,
        digits: true,
        symbols: true,
      });
      setDraft((d) => ({ ...d, password }));
    });

  if (status === null) {
    return (
      <div className="flex items-center justify-center py-12">
        <Loader2 className="w-5 h-5 text-muted-foreground animate-spin" />
      </div>
    );
  }

  const errorBanner = error ? (
    <div role="alert" className="rounded-md border border-red-500/40 bg-red-500/10 px-3 py-2 text-sm text-red-500">
      {error}
    </div>
  ) : null;

  if (!status.exists) {
    return (
      <section className="p-6 space-y-4">
        <div>
          <h2 className="text-lg font-semibold">Create password vault</h2>
          <p className="text-sm text-muted-foreground">
            Choose a master password. It is never stored — if you forget it, the vault cannot be
            recovered.
          </p>
        </div>
        {errorBanner}
        <div className="space-y-3 max-w-sm">
          <div className="space-y-1">
            <label className="text-sm font-medium" htmlFor="vault-master">
              Master password
            </label>
            <Input
              id="vault-master"
              type="password"
              autoComplete="new-password"
              value={master}
              onChange={(e) => setMaster(e.target.value)}
            />
          </div>
          <div className="space-y-1">
            <label className="text-sm font-medium" htmlFor="vault-confirm">
              Confirm master password
            </label>
            <Input
              id="vault-confirm"
              type="password"
              autoComplete="new-password"
              value={confirm}
              onChange={(e) => setConfirm(e.target.value)}
            />
          </div>
          <Button onClick={createVault} disabled={busy || !master || master !== confirm}>
            Create
          </Button>
        </div>
      </section>
    );
  }

  if (!status.unlocked) {
    return (
      <section className="p-6 space-y-4">
        <div>
          <h2 className="text-lg font-semibold">Unlock passwords</h2>
          <p className="text-sm text-muted-foreground">Enter your master password to unlock.</p>
        </div>
        {errorBanner}
        <div className="space-y-3 max-w-sm">
          <div className="space-y-1">
            <label className="text-sm font-medium" htmlFor="vault-master">
              Master password
            </label>
            <Input
              id="vault-master"
              type="password"
              autoComplete="current-password"
              value={master}
              onChange={(e) => setMaster(e.target.value)}
            />
          </div>
          <Button onClick={unlockVault} disabled={busy || !master}>
            Unlock
          </Button>
        </div>
      </section>
    );
  }

  return (
    <section className="p-6 space-y-4">
      <div className="flex items-center justify-between">
        <h2 className="text-lg font-semibold">Passwords</h2>
        <div className="flex gap-2">
          <Button onClick={openAdd} disabled={busy}>
            <Plus className="w-4 h-4 mr-1" />
            Add
          </Button>
          <Button variant="outline" onClick={lockVault} disabled={busy}>
            <Lock className="w-4 h-4 mr-1" />
            Lock
          </Button>
        </div>
      </div>
      {errorBanner}

      {items.length === 0 ? (
        <p className="text-sm text-muted-foreground">No saved passwords yet.</p>
      ) : (
        <table className="w-full text-sm">
          <tbody>
            {items.map((m) => (
              <tr key={m.id} className="border-b border-border">
                <td className="py-2 pr-2">{m.site}</td>
                <td className="py-2 pr-2 text-muted-foreground">{m.username}</td>
                <td className="py-2">
                  {revealed[m.id] !== undefined ? (
                    <span className="font-mono text-xs" data-testid={`vault-password-${m.id}`}>
                      {revealed[m.id]}
                    </span>
                  ) : null}
                </td>
                <td className="py-2 text-right whitespace-nowrap">
                  <Button size="sm" variant="ghost" aria-label="Reveal" onClick={() => revealItem(m.id)}>
                    <Eye className="w-4 h-4" />
                  </Button>
                  <Button size="sm" variant="ghost" aria-label="Copy" onClick={() => copyItem(m.id)}>
                    <Copy className="w-4 h-4" />
                  </Button>
                  <Button size="sm" variant="ghost" aria-label="Edit" onClick={() => openEdit(m.id)}>
                    <Pencil className="w-4 h-4" />
                  </Button>
                  <Button size="sm" variant="ghost" aria-label="Delete" onClick={() => deleteItem(m.id)}>
                    <Trash2 className="w-4 h-4" />
                  </Button>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      )}

      <Dialog open={dialogOpen} onOpenChange={setDialogOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{editingId ? "Edit password" : "Add password"}</DialogTitle>
            <DialogDescription>Stored encrypted on the server.</DialogDescription>
          </DialogHeader>
          <div className="grid grid-cols-2 gap-3">
            <div className="space-y-1">
              <label className="text-xs font-medium" htmlFor="vault-site">
                Site
              </label>
              <Input
                id="vault-site"
                value={draft.site}
                onChange={(e) => setDraft((d) => ({ ...d, site: e.target.value }))}
              />
            </div>
            <div className="space-y-1">
              <label className="text-xs font-medium" htmlFor="vault-username">
                Username
              </label>
              <Input
                id="vault-username"
                value={draft.username}
                onChange={(e) => setDraft((d) => ({ ...d, username: e.target.value }))}
              />
            </div>
            <div className="space-y-1 col-span-2">
              <label className="text-xs font-medium" htmlFor="vault-url">
                URL
              </label>
              <Input
                id="vault-url"
                value={draft.url}
                onChange={(e) => setDraft((d) => ({ ...d, url: e.target.value }))}
              />
            </div>
            <div className="space-y-1 col-span-2">
              <label className="text-xs font-medium" htmlFor="vault-title">
                Title
              </label>
              <Input
                id="vault-title"
                value={draft.title}
                onChange={(e) => setDraft((d) => ({ ...d, title: e.target.value }))}
              />
            </div>
            <div className="space-y-1 col-span-2">
              <label className="text-xs font-medium" htmlFor="vault-password">
                Password
              </label>
              <div className="flex gap-2">
                <Input
                  id="vault-password"
                  type="password"
                  value={draft.password}
                  onChange={(e) => setDraft((d) => ({ ...d, password: e.target.value }))}
                />
                <Button type="button" variant="outline" onClick={generatePassword} disabled={busy}>
                  <Wand2 className="w-4 h-4 mr-1" />
                  Generate
                </Button>
              </div>
            </div>
            <div className="space-y-1 col-span-2">
              <label className="text-xs font-medium" htmlFor="vault-notes">
                Notes
              </label>
              <textarea
                id="vault-notes"
                rows={2}
                className="flex w-full rounded-md border border-input bg-background px-3 py-2 text-sm"
                value={draft.notes}
                onChange={(e) => setDraft((d) => ({ ...d, notes: e.target.value }))}
              />
            </div>
          </div>
          <DialogFooter>
            <Button variant="outline" onClick={() => setDialogOpen(false)}>
              Cancel
            </Button>
            <Button data-dialog-default-action onClick={saveDraft} disabled={busy || !draft.site}>
              Save
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </section>
  );
}
