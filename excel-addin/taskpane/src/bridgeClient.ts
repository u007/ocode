import type { Runner } from "./runner";
import { errText, log } from "./log";
import { readSse } from "./sse";

/**
 * "evicted": another task pane for the same workbook took over. The client has stopped and does not
 * reconnect; only reloading this pane takes the workbook back.
 */
export type BridgeStatus = "connecting" | "connected" | "disconnected" | "evicted";

export interface BridgeOptions {
  /** Origin of the bridge (the pane's own origin), "" for same-origin relative URLs. */
  base: string;
  key: string;
  workbook: string;
  /** Identifies this page load of the pane. Defaults to a fresh id for this client (boot.ts passes one). */
  instance?: string;
  runner: Runner;
  fetchImpl?: typeof fetch;
  onStatus?: (s: BridgeStatus) => void;
  /** reconnect delay bounds in ms */
  backoff?: [number, number];
}

/** Keeps the tool-call stream to the bridge open and answers calls through the Runner. */
export class BridgeClient {
  private ac = new AbortController();
  private stopped = false;
  private instance: string;
  private f: typeof fetch;

  constructor(private o: BridgeOptions) {
    this.f = o.fetchImpl ?? ((...a) => fetch(...a));
    this.instance = o.instance ?? crypto.randomUUID();
  }

  start(): void {
    void this.loop();
  }

  stop(): void {
    this.stopped = true;
    this.ac.abort();
  }

  private headers(extra: Record<string, string> = {}) {
    return { "X-Bridge-Key": this.o.key, ...extra };
  }

  private async loop() {
    const [min, max] = this.o.backoff ?? [500, 10000];
    let delay = min;
    while (!this.stopped) {
      this.o.onStatus?.("connecting");
      log("info", "bridge", "connecting to the tool stream");
      try {
        const q = `workbook=${encodeURIComponent(this.o.workbook)}&instance=${encodeURIComponent(this.instance)}`;
        const resp = await this.f(`${this.o.base}/bridge/stream?${q}`, {
          headers: this.headers(), signal: this.ac.signal,
        });
        if (!resp.ok || !resp.body) throw new Error(`bridge stream ${resp.status}`);
        await readSse(resp.body, (e) => {
          if (e.event === "hello") {
            delay = min;
            log("info", "bridge", "connected");
            this.o.onStatus?.("connected");
          } else if (e.event === "call") {
            const call = JSON.parse(e.data) as { id: string; tool: string; args: unknown };
            log("info", "bridge", `call ${call.tool} (${call.id})`);
            void this.dispatch(call);
          } else if (e.event === "cancel") {
            const id = (JSON.parse(e.data) as { id: string }).id;
            log("warn", "bridge", `cancel ${id}`);
            this.o.runner.cancel(id);
          } else if (e.event === "evicted") {
            // Another pane owns the workbook now. Stop for good: reconnecting would evict that pane in turn.
            log("error", "bridge", "evicted: another pane owns this workbook");
            this.stop();
            this.o.onStatus?.("evicted");
          }
        }, this.ac.signal);
      } catch (e) {
        // network error, bridge restarted, or stop(): fall through to the reconnect wait
        if (!this.stopped) log("warn", "bridge", `stream error: ${errText(e)}; reconnecting in ${delay} ms`);
      }
      if (this.stopped) break; // stop() or an eviction: never report "disconnected" over "evicted"
      this.o.onStatus?.("disconnected");
      await new Promise((r) => setTimeout(r, delay));
      delay = Math.min(max, delay * 2);
    }
  }

  private async dispatch(call: { id: string; tool: string; args: unknown }) {
    const res = await this.o.runner.handle(call);
    try {
      await this.f(`${this.o.base}/bridge/result?workbook=${encodeURIComponent(this.o.workbook)}`, {
        method: "POST", headers: this.headers({ "Content-Type": "application/json" }), body: JSON.stringify({ id: call.id, ...res }),
      });
    } catch (e) {
      // The bridge will time the call out and report it to the model; there is nothing better to do here.
      log("error", "bridge", `could not post the result of ${call.tool} (${call.id}): ${errText(e)}`);
    }
  }
}
