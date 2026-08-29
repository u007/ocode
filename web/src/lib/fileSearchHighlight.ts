export type Highlight = { query: string; line?: number; ts: number; projectRoot?: string };

function keyFor(path: string, projectRoot?: string): string {
  return `${projectRoot ?? ""}::${path}`;
}

const pending = new Map<string, Highlight>();

export function setPendingHighlight(path: string, query: string, line?: number, projectRoot?: string) {
  const ts = Date.now();
  const key = keyFor(path, projectRoot);
  pending.set(key, { query, line, ts, projectRoot });
  // Expire after 30s only if no newer highlight replaced it
  setTimeout(() => {
    const cur = pending.get(key);
    if (cur && cur.ts === ts) pending.delete(key);
  }, 30000);
}

export function consumePendingHighlight(path: string, projectRoot?: string): Highlight | null {
  const key = keyFor(path, projectRoot);
  const h = pending.get(key) || null;
  if (h) pending.delete(key);
  return h;
}

export function peekPendingHighlight(path: string, projectRoot?: string): Highlight | null {
  return pending.get(keyFor(path, projectRoot)) || null;
}
