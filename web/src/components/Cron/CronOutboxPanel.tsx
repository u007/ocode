import { Button } from "@/components/ui/button";
import type { CronDelivery } from "@/api/types";

interface Props {
  entries: CronDelivery[];
  onClear: () => Promise<void>;
}

export default function CronOutboxPanel({ entries, onClear }: Props) {
  return (
    <section className="rounded-lg border border-border bg-card/80 p-4">
      <div className="mb-4 flex items-center justify-between gap-4">
        <div>
          <h2 className="text-sm font-semibold text-foreground">Outbox</h2>
          <p className="text-xs text-muted-foreground">Recent scheduled-job deliveries and errors.</p>
        </div>
        <Button variant="outline" size="sm" onClick={() => void onClear()} disabled={entries.length === 0}>
          Clear
        </Button>
      </div>

      {entries.length === 0 ? (
        <div className="rounded-md border border-dashed border-border px-3 py-6 text-sm text-muted-foreground">
          No deliveries yet.
        </div>
      ) : (
        <div className="space-y-2 max-h-72 overflow-y-auto pr-1">
          {entries
            .slice()
            .reverse()
            .map((entry) => (
              <div key={`${entry.job_id}-${entry.at}`} className="rounded-md border border-border bg-background/70 p-3 text-sm">
                <div className="flex items-start justify-between gap-3">
                  <div className="min-w-0">
                    <div className="font-medium text-foreground">{entry.job_name || entry.job_id}</div>
                    <div className="text-xs text-muted-foreground">{entry.owner || "—"}{entry.delivered_to ? ` · ${entry.delivered_to}` : ""}</div>
                  </div>
                  <div className="text-xs text-muted-foreground whitespace-nowrap">{new Date(entry.at).toLocaleString()}</div>
                </div>
                <div className="mt-2 whitespace-pre-wrap text-foreground">{entry.result}</div>
                {entry.error && <div className="mt-2 whitespace-pre-wrap text-sm text-red-300">{entry.error}</div>}
              </div>
            ))}
        </div>
      )}
    </section>
  );
}
