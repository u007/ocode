import type { ExcelPort } from "./port";

const KEY_ID = "ocode.workbook_id";

/** Stable id stored inside the workbook so reopening the file finds its sessions again. */
export async function workbookId(port: ExcelPort, gen: () => string = () => crypto.randomUUID()): Promise<string> {
  const existing = await port.getSetting(KEY_ID);
  if (existing) return existing;
  const id = gen();
  await port.setSetting(KEY_ID, id);
  return id;
}

export interface SessionRef { id: string; title: string; created: string }

/** Sidecar index workbookId -> sessions, kept in the bridge so it survives pane reloads. */
export class SessionIndex {
  constructor(private base: string, private key: string, private wb: string, private f: typeof fetch = (...a) => fetch(...a)) {}
  private url() { return `${this.base}/bridge/state/${encodeURIComponent(this.wb)}/sessions`; }

  async list(): Promise<SessionRef[]> {
    const r = await this.f(this.url(), { headers: { "X-Bridge-Key": this.key } });
    if (r.status === 404) return [];
    if (!r.ok) throw new Error(`session index ${r.status}`);
    return (await r.json()) as SessionRef[];
  }

  async add(ref: SessionRef): Promise<void> {
    const all = (await this.list()).filter((s) => s.id !== ref.id);
    all.unshift(ref);
    const r = await this.f(this.url(), { method: "PUT", headers: { "X-Bridge-Key": this.key, "Content-Type": "application/json" }, body: JSON.stringify(all) });
    if (!r.ok) throw new Error(`session index write ${r.status}`);
  }
}

/** Title shown in ocode's own session list, so Excel sessions are recognisable there too. */
export const sessionTitle = (fileName: string) => `[Excel] ${fileName}`;

/** The per-workbook project directory the session is rooted in (see RESUME.md: allowed-roots question). */
export const workspaceFor = (dataDir: string, id: string) => `${dataDir.replace(/\/$/, "")}/excel/${id}`;
