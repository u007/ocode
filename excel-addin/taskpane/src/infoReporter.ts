import { errText, log } from "./log";
import type { WorkbookInfo } from "./port";

/** How often the workbook state is collected. */
export const TICK_MS = 10_000;
/** A post at least this often, even when nothing changed, so the bridge can tell the pane is still there. */
export const HEARTBEAT_MS = 60_000;

export interface InfoReporterOptions {
  base: string;
  key: string;
  workbook: string;
  collect: () => Promise<WorkbookInfo>;
  fetchImpl?: typeof fetch;
}

/**
 * Keeps the bridge informed of the open workbook's state (see WorkbookInfo). Collects every TICK_MS and posts
 * when the state changed since the last successful post, or when HEARTBEAT_MS has passed since it.
 */
export class InfoReporter {
  private latest: WorkbookInfo | null = null;
  private lastPosted: WorkbookInfo | null = null;
  private lastPostedAt = 0;
  private busy = false;
  private f: typeof fetch;

  constructor(private o: InfoReporterOptions) {
    this.f = o.fetchImpl ?? ((...a) => fetch(...a));
  }

  /** The most recent state collected from the workbook, whether or not it was posted. null before the first collection. */
  current(): WorkbookInfo | null {
    return this.latest;
  }

  start(): void {
    void this.tick();
    setInterval(() => void this.tick(), TICK_MS);
  }

  /** One collection and at most one post. Never throws: a failure is logged and the next tick tries again. */
  async tick(): Promise<void> {
    if (this.busy) return;
    this.busy = true;
    try {
      const info = await this.o.collect();
      this.latest = info;
      const changed = JSON.stringify(info) !== JSON.stringify(this.lastPosted);
      const due = Date.now() - this.lastPostedAt >= HEARTBEAT_MS;
      if (!changed && !due) return;
      await this.post(info);
      this.lastPosted = info;
      this.lastPostedAt = Date.now();
    } catch (e) {
      log("warn", "info", `could not report the workbook state (retrying in ${TICK_MS / 1000} s): ${errText(e)}`);
    } finally {
      this.busy = false;
    }
  }

  private async post(info: WorkbookInfo): Promise<void> {
    const r = await this.f(`${this.o.base}/bridge/pane-info?workbook=${encodeURIComponent(this.o.workbook)}`, {
      method: "POST",
      headers: { "X-Bridge-Key": this.o.key, "Content-Type": "application/json" },
      body: JSON.stringify(info),
    });
    if (!r.ok) throw new Error(`the bridge answered HTTP ${r.status}`);
  }
}
