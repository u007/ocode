export type LogLevel = "info" | "warn" | "error";
export interface LogEntry { id: number; at: number; level: LogLevel; source: string; msg: string; ms?: number }

const CAP = 500;
let entries: LogEntry[] = [];
let nextId = 1;
const subs = new Set<() => void>();

/** In-pane activity log shown on the Logs tab: bridge, ocode requests, tool calls, boot steps and errors. */
export function log(level: LogLevel, source: string, msg: string, ms?: number): void {
  entries = [...entries.slice(-(CAP - 1)), { id: nextId++, at: Date.now(), level, source, msg, ms }];
  subs.forEach((f) => f());
}

export const logSnapshot = (): readonly LogEntry[] => entries;
export function logSubscribe(f: () => void): () => void {
  subs.add(f);
  return () => { subs.delete(f); };
}
export function logClear(): void {
  entries = [];
  subs.forEach((f) => f());
}
export const errText = (e: unknown): string => (e instanceof Error ? e.message : String(e));

export function formatEntry(e: LogEntry): string {
  const t = new Date(e.at).toISOString().slice(11, 23);
  return `${t} ${e.level.toUpperCase().padEnd(5)} ${e.source}: ${e.msg}${e.ms === undefined ? "" : ` (${e.ms} ms)`}`;
}
