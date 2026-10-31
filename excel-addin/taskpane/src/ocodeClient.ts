import { errText, log } from "./log";
import { readSse } from "./sse";

/** Envelope published by `ocode serve` on GET /api/events (internal/server/event_bus.go). */
export interface Envelope {
  event: string;
  project?: string;
  session_id?: string;
  seq: number;
  data: any;
}

export interface SessionInfo { id: string; title: string; created_at: string; updated_at: string }
export interface TranscriptMessage {
  role: "system" | "user" | "assistant" | "tool";
  content: string;
  tool_calls?: { id: string; function: { name: string; arguments: string } }[];
  tool_call_id?: string;
}

export class HttpError extends Error {
  constructor(public status: number, message: string) { super(message); }
}

export interface OcodeClientOptions {
  base: string;
  key: string;
  fetchImpl?: typeof fetch;
}

/** Thin client over the ocode serve endpoints, reached through the bridge's /api proxy. */
export class OcodeClient {
  private f: typeof fetch;
  constructor(private o: OcodeClientOptions) {
    this.f = o.fetchImpl ?? ((...a) => fetch(...a));
  }

  private async req<T>(method: string, path: string, body?: unknown): Promise<T> {
    const t0 = performance.now();
    const took = () => Math.round(performance.now() - t0);
    let resp: Response;
    try {
      resp = await this.f(`${this.o.base}${path}`, {
        method,
        headers: { "X-Bridge-Key": this.o.key, ...(body !== undefined ? { "Content-Type": "application/json" } : {}) },
        body: body === undefined ? undefined : JSON.stringify(body),
      });
    } catch (e) {
      log("error", "ocode", `${method} ${path} network error: ${errText(e)}`, took());
      throw e;
    }
    if (!resp.ok) {
      let msg = `${resp.status}`;
      try { msg = ((await resp.json()) as { error?: string }).error ?? msg; } catch (e) { log("warn", "ocode", `${method} ${path}: error body is not JSON (${errText(e)})`); }
      log("error", "ocode", `${method} ${path} -> ${resp.status} ${msg}`, took());
      throw new HttpError(resp.status, msg);
    }
    log("info", "ocode", `${method} ${path} -> ${resp.status}`, took());
    const text = await resp.text();
    return (text ? JSON.parse(text) : undefined) as T;
  }

  /** Start a session with its first message. Output arrives over events(). */
  startChat(a: { content: string; projectPath: string; model?: string; permissionMode?: "normal" | "yolo" | "locked" | "sandbox" }) {
    return this.req<{ sessionId: string; model: string }>("POST", "/api/chat", {
      content: a.content, project_path: a.projectPath, model: a.model, permission_mode: a.permissionMode, async: true,
    });
  }
  send(id: string, content: string, rewindToken?: string) {
    return this.req<{ sessionId: string }>("POST", `/api/sessions/${enc(id)}/message`, { content, rewindToken, async: true });
  }
  /** Register a directory as an ocode project (idempotent). Directory-bound endpoints only accept registered projects. */
  addProject(path: string) { return this.req<{ status: string }>("POST", "/api/projects", { path }); }
  listSessions() { return this.req<{ sessions: SessionInfo[]; total: number }>("GET", "/api/sessions"); }
  getSession(id: string) { return this.req<{ id: string; title: string; messages: TranscriptMessage[]; total: number; revision?: string }>("GET", `/api/sessions/${enc(id)}`); }
  getState(id: string) { return this.req<Record<string, any>>("GET", `/api/sessions/${enc(id)}/state`); }
  recap(id: string) { return this.req<{ recap: string }>("GET", `/api/sessions/${enc(id)}/recap`); }
  compact(id: string) { return this.req<unknown>("POST", `/api/sessions/${enc(id)}/compact`, {}); }
  cancel(id: string) { return this.req<unknown>("POST", `/api/sessions/${enc(id)}/cancel`, {}); }
  retry(id: string) { return this.req<unknown>("POST", `/api/sessions/${enc(id)}/retry`, {}); }
  setTitle(id: string, title: string) { return this.req<unknown>("PUT", `/api/sessions/${enc(id)}/title`, { title }); }
  listModels() { return this.req<unknown>("GET", "/api/models"); }
  setModel(id: string, model: string) { return this.req<unknown>("PUT", `/api/sessions/${enc(id)}/model`, { model }); }
  resolvePermission(a: { requestId: string; sessionId: string; decision: "allow" | "deny" | "always_rule" }) {
    return this.req<unknown>("POST", "/api/permissions/resolve", { request_id: a.requestId, session_id: a.sessionId, decision: a.decision });
  }
  answerQuestion(a: { requestId: string; sessionId: string; answers: unknown[] }) {
    return this.req<unknown>("POST", "/api/questions", { request_id: a.requestId, session_id: a.sessionId, answers: a.answers });
  }
  /** Arm a rewind to a user message; the returned token goes into the next send(). */
  prepareRewind(id: string, a: { targetIndex: number; targetContent: string; userSeq: number }) {
    return this.req<{ token: string }>("POST", `/api/sessions/${enc(id)}/rewinds`, a);
  }

  /**
   * Subscribe to the server event stream until `signal` aborts, reconnecting with backoff.
   * `onReconnect` fires after every reconnect so the caller can resync via getState(): events
   * missed while disconnected are not replayed.
   */
  async events(a: { projects: string[]; signal: AbortSignal; onEnvelope: (e: Envelope) => void; onReconnect?: () => void; onStatus?: (connected: boolean) => void }) {
    let delay = 500;
    let first = true;
    while (!a.signal.aborted) {
      try {
        const q = a.projects.length ? `?projects=${encodeURIComponent(a.projects.join(","))}` : "";
        const resp = await this.f(`${this.o.base}/api/events${q}`, { headers: { "X-Bridge-Key": this.o.key }, signal: a.signal });
        if (!resp.ok || !resp.body) throw new HttpError(resp.status, "events");
        a.onStatus?.(true);
        log("info", "ocode", first ? "event stream connected" : "event stream reconnected");
        if (!first) a.onReconnect?.();
        first = false;
        delay = 500;
        await readSse(resp.body, (e) => {
          if (e.event !== "envelope") return;
          try { a.onEnvelope(JSON.parse(e.data) as Envelope); } catch (err) { log("warn", "ocode", `malformed event frame dropped: ${errText(err)}`); /* one malformed frame must not drop the stream */ }
        }, a.signal);
      } catch (e) {
        if (!a.signal.aborted) log("warn", "ocode", `event stream error: ${errText(e)}; reconnecting in ${delay} ms`);
      }
      if (a.signal.aborted) return;
      a.onStatus?.(false);
      await new Promise((r) => setTimeout(r, delay));
      delay = Math.min(10000, delay * 2);
    }
  }
}

const enc = encodeURIComponent;
