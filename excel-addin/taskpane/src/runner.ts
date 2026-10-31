import type { ExcelPort } from "./port";
import { Schema, validate, withDefaults } from "./schema";
import { TOOLS, ToolError } from "./tools";
import { log } from "./log";
import { Journal } from "./tools/journal";

export interface ContractTool { name: string; risk: string; description: string; inputSchema: Schema }
export interface Contract { version: number; tools: ContractTool[] }
export interface CallResult { ok: boolean; content?: string; error?: string }

/**
 * Executes tool calls from the bridge one at a time (so a write and its read-back never interleave
 * with another call), validates arguments against the shared contract, and supports cancellation.
 */
export class Runner {
  private inflight = new Map<string, AbortController>();
  private chain: Promise<unknown> = Promise.resolve();
  private byName: Map<string, ContractTool>;

  constructor(private port: ExcelPort, private journal: Journal, contract: Contract) {
    this.byName = new Map(contract.tools.map((t) => [t.name, t]));
  }

  cancel(id: string) {
    this.inflight.get(id)?.abort();
  }

  handle(call: { id: string; tool: string; args: unknown }): Promise<CallResult> {
    const ac = new AbortController();
    this.inflight.set(call.id, ac);
    const run = this.chain.then(async () => {
      const t0 = performance.now();
      const res = await this.exec(call, ac.signal);
      const ms = Math.round(performance.now() - t0);
      if (res.ok) log("info", "tool", `${call.tool} ok`, ms);
      else log("error", "tool", `${call.tool} failed: ${res.error}`, ms);
      return res;
    });
    this.chain = run.catch(() => undefined);
    return run.finally(() => this.inflight.delete(call.id));
  }

  private async exec(call: { tool: string; args: unknown }, signal: AbortSignal): Promise<CallResult> {
    const def = this.byName.get(call.tool);
    const fn = TOOLS[call.tool];
    if (!def || !fn) return { ok: false, error: `Unknown tool "${call.tool}"` };
    if (signal.aborted) return { ok: false, error: "The call was cancelled before it started" };
    const args = (call.args ?? {}) as Record<string, unknown>;
    const errs = validate(def.inputSchema, args);
    if (errs.length) return { ok: false, error: `Invalid arguments: ${errs.join("; ")}` };
    // "workbook" only routes the call inside the bridge; the tools never see it.
    const { workbook: _routing, ...rest } = args;
    void _routing;
    try {
      const out = await fn({ port: this.port, journal: this.journal, signal }, withDefaults(def.inputSchema, rest));
      return { ok: true, content: JSON.stringify(out) };
    } catch (e) {
      if (e instanceof ToolError) return { ok: false, error: e.message };
      // An Office.js failure (bad range, protected sheet, ...) carries a useful code and message: pass it on.
      const err = e as { code?: string; message?: string };
      return { ok: false, error: `Excel error${err.code ? ` (${err.code})` : ""}: ${err.message ?? String(e)}` };
    }
  }
}
