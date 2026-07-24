import { Button } from "@/components/ui/button";
import type { CronDelivery } from "@/api/types";

interface Props {
  entries: CronDelivery[];
  onClear: () => Promise<void>;
}

export default function CronOutboxPanel({ entries, onClear }: Props) {
  return (
    <section className="rounded-lg border border-zinc-700 bg-zinc-900/80 p-4">
      <div className="mb-4 flex items-center justify-between gap-4">
        <div>
          <h2 className="text-sm font-semibold text-zinc-100">Outbox</h2>
          <p className="text-xs text-zinc-500">Recent scheduled-job deliveries and errors.</p>
        </div>
        <Button variant="outline" size="sm" onClick={() => void onClear()} disabled={entries.length === 0}>
          Clear
        </Button>
      </div>

      {entries.length === 0 ? (
        <div className="rounded-md border border-dashed border-zinc-700 px-3 py-6 text-sm text-zinc-500">
          No deliveries yet.
        </div>
      ) : (
        <div className="space-y-2 max-h-72 overflow-y-auto pr-1">
          {entries
            .slice()
            .reverse()
            .map((entry) => (
              <div key={`${entry.job_id}-${entry.at}`} className="rounded-md border border-zinc-700 bg-zinc-950/70 p-3 text-sm">
                <div className="flex items-start justify-between gap-3">
                  <div className="min-w-0">
                    <div className="font-medium text-zinc-100">{entry.job_name || entry.job_id}</div>
                    <div className="text-xs text-zinc-500">{entry.owner || "—"}{entry.delivered_to ? ` · ${entry.delivered_to}` : ""}</div>
                  </div>
                  <div className="text-xs text-zinc-500 whitespace-nowrap">{new Date(entry.at).toLocaleString()}</div>
                </div>
                <div className="mt-2 whitespace-pre-wrap text-zinc-300">{entry.result}</div>
                {entry.error && <div className="mt-2 whitespace-pre-wrap text-sm text-red-300">{entry.error}</div>}
              </div>
            ))}
        </div>
      )}
    </section>
  );
}
