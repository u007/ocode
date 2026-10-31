import type { JournalStore, Step } from "./tools/journal";

/** Journal persisted in the bridge so undo survives closing and reopening the task pane. */
export class BridgeJournalStore implements JournalStore {
  constructor(private base: string, private key: string, private wb: string, private f: typeof fetch = (...a) => fetch(...a)) {}
  private url() { return `${this.base}/bridge/state/${encodeURIComponent(this.wb)}/journal`; }

  async load(): Promise<Step[]> {
    const r = await this.f(this.url(), { headers: { "X-Bridge-Key": this.key } });
    if (r.status === 404) return [];
    if (!r.ok) throw new Error(`journal load ${r.status}`);
    return (await r.json()) as Step[];
  }

  async save(steps: Step[]): Promise<void> {
    const r = await this.f(this.url(), { method: "PUT", headers: { "X-Bridge-Key": this.key, "Content-Type": "application/json" }, body: JSON.stringify(steps) });
    // Losing the journal silently would make a later undo lie, so a failed save fails the write.
    if (!r.ok) throw new Error(`journal save ${r.status}`);
  }
}
