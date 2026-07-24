import { useEffect, useMemo, useState } from "react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";

interface Props {
  targets: Record<string, number>;
  onSave: (targets: Record<string, number>) => Promise<void>;
}

type Row = {
  workdir: string;
  chatId: string;
};

function rowsFromTargets(targets: Record<string, number>): Row[] {
  return Object.entries(targets)
    .sort(([a], [b]) => a.localeCompare(b))
    .map(([workdir, chatId]) => ({ workdir, chatId: String(chatId) }));
}

export default function CronTargetsPanel({ targets, onSave }: Props) {
  const [rows, setRows] = useState<Row[]>(() => rowsFromTargets(targets));
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    setRows(rowsFromTargets(targets));
  }, [targets]);

  const preview = useMemo(() => {
    const next: Record<string, number> = {};
    for (const row of rows) {
      const workdir = row.workdir.trim();
      const chatId = Number(row.chatId);
      if (!workdir || !Number.isFinite(chatId) || chatId <= 0) continue;
      next[workdir] = Math.floor(chatId);
    }
    return next;
  }, [rows]);

  const handleSave = async () => {
    setError(null);
    try {
      setSaving(true);
      await onSave(preview);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to save targets");
    } finally {
      setSaving(false);
    }
  };

  return (
    <section className="rounded-lg border border-zinc-700 bg-zinc-900/80 p-4">
      <div className="mb-4 flex items-center justify-between gap-4">
        <div>
          <h2 className="text-sm font-semibold text-zinc-100">Targets</h2>
          <p className="text-xs text-zinc-500">Map project workdirs to Telegram chat IDs.</p>
        </div>
        <div className="flex items-center gap-2">
          <Button variant="outline" size="sm" onClick={() => setRows((prev) => [...prev, { workdir: "", chatId: "" }])}>
            Add row
          </Button>
          <Button size="sm" onClick={() => void handleSave()} disabled={saving}>
            {saving ? "Saving…" : "Save"}
          </Button>
        </div>
      </div>

      <div className="space-y-2">
        {rows.length === 0 ? (
          <div className="rounded-md border border-dashed border-zinc-700 px-3 py-6 text-sm text-zinc-500">
            No targets configured.
          </div>
        ) : (
          rows.map((row, idx) => (
            <div key={`${row.workdir}-${idx}`} className="grid gap-2 md:grid-cols-[minmax(0,1fr)_160px_auto] items-center rounded-md border border-zinc-700 bg-zinc-950/70 p-2">
              <Input
                value={row.workdir}
                onChange={(e) =>
                  setRows((prev) => prev.map((item, itemIdx) => (itemIdx === idx ? { ...item, workdir: e.target.value } : item)))
                }
                placeholder="/path/to/project"
              />
              <Input
                type="number"
                value={row.chatId}
                onChange={(e) =>
                  setRows((prev) => prev.map((item, itemIdx) => (itemIdx === idx ? { ...item, chatId: e.target.value } : item)))
                }
                placeholder="123456789"
              />
              <Button
                variant="outline"
                size="sm"
                onClick={() => setRows((prev) => prev.filter((_, itemIdx) => itemIdx !== idx))}
              >
                Remove
              </Button>
            </div>
          ))
        )}
      </div>

      <div className="mt-3 text-xs text-zinc-500">
        Blank rows are ignored on save. Non-zero chat IDs are persisted; missing rows remove existing mappings.
      </div>
      <div className="mt-1 text-xs text-zinc-600">
        Saving {Object.keys(preview).length} target{Object.keys(preview).length === 1 ? "" : "s"}.
      </div>
      {error && <div className="mt-3 rounded-md border border-red-900 bg-red-950/60 px-3 py-2 text-sm text-red-200">{error}</div>}
    </section>
  );
}
