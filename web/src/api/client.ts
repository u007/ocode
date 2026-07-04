import type {
  ChatResponse,
  SessionInfo,
  SessionDetail,
  ModelInfo,
  AgentInfo,
  AgentRun,
  GitDiffFile,
  ThemeResponse,
  TUIStatus,
  LSPStatus,
  FileStatus,
  Project,
  BrowseResponse,
} from "./types";

// Base path for API calls. When the SPA is served under a tailscale --set-path
// prefix (e.g. /<sessionID>), API calls must include the prefix or the tailscale
// proxy routes them to whichever session owns the root path. The /rc command
// embeds the same prefix in the opened URL, so derive BASE from the current
// location: everything before the trailing "/session/<id>" is the prefix.
// The same value is passed to <BrowserRouter basename=...> in main.tsx so client
// navigation stays in sync.
export const _basePath = (() => {
  const m = window.location.pathname.match(/^(.*?)\/session\/[^/]+$/);
  return m && m[1] ? m[1] : "";
})();
const BASE = _basePath;

// Auth token embedded in URL by /rc command (?token=...). Stored at load time
// so navigation or hash changes don't lose it.
const _token = new URLSearchParams(window.location.search).get("token") ?? "";

/** Returns auth headers for fetch() calls. Exported for components that use raw
 *  fetch or EventSource (which cannot set headers). */
export function authHeaders(): Record<string, string> {
  return _token ? { Authorization: `Bearer ${_token}` } : {};
}

/** Returns the auth token string. Useful for EventSource URLs. */
export function authToken(): string {
  return _token;
}

/** Prepends the current SPA base path to an API or SSE path. */
export function apiPath(path: string): string {
  return `${BASE}${path}`;
}

async function fetchJSON<T>(path: string, init?: RequestInit): Promise<T> {
  const res = await fetch(apiPath(path), {
    headers: { "Content-Type": "application/json", ...authHeaders() },
    ...init,
  });
  if (!res.ok) {
    const err = await res.json().catch(() => ({ error: res.statusText }));
    throw new Error(err.error || res.statusText);
  }
  return res.json();
}

export const api = {
  listSessions: () => fetchJSON<SessionInfo[]>("/api/sessions"),
  getSession: (id: string, opts?: { limit?: number; offset?: number }) => {
    const params = new URLSearchParams();
    if (opts?.limit) params.set("limit", String(opts.limit));
    if (opts?.offset) params.set("offset", String(opts.offset));
    const qs = params.toString();
    return fetchJSON<SessionDetail>(
      `/api/sessions/${id}${qs ? `?${qs}` : ""}`,
    );
  },
  listModels: () => fetchJSON<ModelInfo[]>("/api/models"),
  listAgents: () => fetchJSON<AgentInfo[]>("/api/config/agents"),
  listAgentRuns: (session?: string) =>
    fetchJSON<AgentRun[]>(
      `/api/agents/runs${session ? `?session=${encodeURIComponent(session)}` : ""}`,
    ),
  getConfigModel: () => fetchJSON<{ model: string }>("/api/config/model"),
  setConfigModel: (model: string) =>
    fetchJSON<{ model: string }>("/api/config/model", {
      method: "PUT",
      body: JSON.stringify({ model }),
    }),
  getSmallModel: () =>
    fetchJSON<{ model: string; priority: string }>("/api/config/small-model"),
  setSmallModel: (model: string) =>
    fetchJSON<{ model: string; source: string }>("/api/config/small-model", {
      method: "PUT",
      body: JSON.stringify({ model }),
    }),
  getGitDiff: (path?: string) =>
    fetchJSON<GitDiffFile[]>(
      `/api/git/diff${path ? `?path=${encodeURIComponent(path)}` : ""}`,
    ),
  getTheme: () => fetchJSON<ThemeResponse>("/api/theme"),
  getAdvisor: () =>
    fetchJSON<{ model: string }>("/api/config/advisor"),
  setAdvisor: (model: string) =>
    fetchJSON<{ model: string }>("/api/config/advisor", {
      method: "PUT",
      body: JSON.stringify({ model }),
    }),
  // Runtime advisor on/off toggle — session-lifetime only, never persisted to config.
  getAdvisorEnabled: () =>
    fetchJSON<{ enabled: boolean }>("/api/config/advisor-enabled"),
  setAdvisorEnabled: (enabled: boolean) =>
    fetchJSON<{ enabled: boolean }>("/api/config/advisor-enabled", {
      method: "PUT",
      body: JSON.stringify({ enabled }),
    }),
  // TUI status (consolidated snapshot pushed by the TUI on every state
  // change). The web also subscribes to the "status" SSE event so the bar
  // updates live without polling.
  getTUIStatus: () => fetchJSON<TUIStatus>("/api/tui-status"),
  getSpending: () =>
    fetchJSON<{ spending_usd: number; records: number }>("/api/spending"),
  getLSPStatuses: () =>
    fetchJSON<{ lsp_servers: LSPStatus[] }>("/api/lsp/statuses"),
  getModifiedFiles: () =>
    fetchJSON<{ modified_files: FileStatus[] }>("/api/files/modified"),
  getSessionContext: (id: string) =>
    fetchJSON<{
      session_id: string;
      message_count: number;
      estimated_tokens: number;
      max_tokens?: number;
      model?: string;
    }>(`/api/sessions/${id}/context`),
  getSmallModelWithEnabled: () =>
    fetchJSON<{ model: string; enabled: boolean; priority: string }>(
      "/api/config/small-model",
    ),
  // ── OCR (new structured API) ──
  getOcrConfig: () =>
    fetchJSON<import("../api/types").OcrConfig>("/api/config/ocr"),
  setOcrConfig: (cfg: import("../api/types").OcrConfig) =>
    fetchJSON<import("../api/types").OcrConfig>("/api/config/ocr", {
      method: "PUT",
      body: JSON.stringify(cfg),
    }),
  getOcrModels: () =>
    fetchJSON<import("../api/types").OcrModelsResponse>("/api/ocr/models"),

  // ── OCR (legacy API, deprecated) ──
  getOcrEnabled: () =>
    fetchJSON<{ enabled: boolean; model: string }>("/api/config/ocr-enabled"),
  setOcrEnabled: (enabled: boolean) =>
    fetchJSON<{ enabled: boolean }>("/api/config/ocr-enabled", {
      method: "PUT",
      body: JSON.stringify({ enabled }),
    }),
  setOcrModel: (model: string) =>
    fetchJSON<{ model: string }>("/api/config/ocr-model", {
      method: "PUT",
      body: JSON.stringify({ model }),
    }),
  sendMessage: (sessionId: string, content: string) =>
    fetchJSON<ChatResponse>(`/api/sessions/${sessionId}/message`, {
      method: "POST",
      body: JSON.stringify({ content }),
    }),
  chat: (content: string, sessionId?: string, model?: string) =>
    fetchJSON<ChatResponse>("/api/chat", {
      method: "POST",
      body: JSON.stringify({ content, sessionId, model }),
    }),
  openFile: (path: string, line?: number) =>
    fetchJSON<{ path: string; status: string }>("/api/files/open", {
      method: "POST",
      body: JSON.stringify({ path, line }),
    }),
  shellCommand: (command: string, workDir?: string) =>
    fetchJSON<{ output: string; exitCode: number; error: string }>("/api/shell", {
      method: "POST",
      body: JSON.stringify({ command, workDir }),
    }),
  listProjects: () => fetchJSON<Project[]>("/api/projects"),
  addProject: (path: string) =>
    fetchJSON<{ status: string }>("/api/projects", {
      method: "POST",
      body: JSON.stringify({ path }),
    }),
  removeProject: (path: string) =>
    fetchJSON<{ status: string }>("/api/projects/" + encodeURIComponent(path), {
      method: "DELETE",
    }),
  listProjectSessions: (path: string) =>
    fetchJSON<SessionInfo[]>("/api/projects/sessions?path=" + encodeURIComponent(path)),
  // Monaco editor settings and extensions
  getMonacoSettings: () => fetchJSON<{ theme: string; font_size: number; tab_size: number; word_wrap: boolean; minimap: boolean; line_numbers: boolean }>("/api/monaco/settings"),
  setMonacoSettings: (settings: Record<string, unknown>) =>
    fetchJSON<{ status: string }>("/api/monaco/settings", {
      method: "PUT",
      body: JSON.stringify(settings),
    }),
  listMonacoExtensions: () =>
    fetchJSON<Array<{ name: string; label: string; enabled: boolean; builtin: boolean }>>("/api/monaco/extensions"),
  toggleMonacoExtension: (name: string) =>
    fetchJSON<{ name: string; label: string; enabled: boolean; builtin: boolean }[]>("/api/monaco/extensions/" + encodeURIComponent(name) + "/toggle", {
      method: "PUT",
    }),
  // Directory browser for the project sidebar folder picker.
  browseDirectory: (path?: string) =>
    fetchJSON<BrowseResponse>(
      "/api/browse" + (path ? "?path=" + encodeURIComponent(path) : ""),
    ),
  // Session operations
  compactSession: (id: string) =>
    fetchJSON<{ original_len: number; compacted_len: number }>(
      `/api/sessions/${encodeURIComponent(id)}/compact`, { method: "POST" },
    ),
  recapSession: (id: string) =>
    fetchJSON<{ recap: string }>(
      `/api/sessions/${encodeURIComponent(id)}/recap`,
    ),
  shareSession: (id: string) =>
    fetchJSON<{ markdown: string }>(
      `/api/sessions/${encodeURIComponent(id)}/share`,
    ),
  btwSession: (id: string, content: string) =>
    fetchJSON<{ status: string }>(
      `/api/sessions/${encodeURIComponent(id)}/btw`, {
        method: "POST",
        body: JSON.stringify({ content }),
      },
    ),

  // Mask (secret redaction) config
  getMaskConfig: () =>
    fetchJSON<{ enabled: boolean; mode: string; model: string }>("/api/config/mask"),
  setMaskEnabled: (enabled: boolean) =>
    fetchJSON<{ enabled: boolean }>("/api/config/mask/enabled", {
      method: "PUT",
      body: JSON.stringify({ enabled }),
    }),
  setMaskMode: (mode: string) =>
    fetchJSON<{ mode: string }>("/api/config/mask/mode", {
      method: "PUT",
      body: JSON.stringify({ mode }),
    }),
  setMaskModel: (model: string) =>
    fetchJSON<{ model: string }>("/api/config/mask/model", {
      method: "PUT",
      body: JSON.stringify({ model }),
    }),

};

export type SSEEventHandler = (event: string, data: unknown) => void;

// connectSessionMirror opens the persistent live mirror of the bridged TUI
// session. It carries every event needed for a 2-way live view: full-list
// snapshots (`messages`), `user_message`, token deltas (`thinking`, `text`),
// tool activity (`tool_start`, `tool_result`), `turn_done`, and `error`. So
// activity originating in the TUI (or any other browser) appears here live.
// Returns a cleanup function.
export function connectSessionMirror(
  session: string | undefined,
  onEvent: SSEEventHandler,
): () => void {
  const params = new URLSearchParams();
  if (session) params.set("session", session);
  if (_token) params.set("token", _token);

  const es = new EventSource(apiPath(`/api/chat/messages?${params}`));
  const on = (name: string) =>
    es.addEventListener(name, (e) => {
      try {
        onEvent(name, JSON.parse((e as MessageEvent).data));
      } catch (err) {
        console.error(`failed to parse '${name}' mirror frame`, err);
      }
    });
  ["messages", "user_message", "thinking", "text", "tool_start", "tool_result", "turn_done", "status", "advisor_enabled"].forEach(on);
  // The "error" event is overloaded: a server-sent `event: error` carries data,
  // while a transport failure (EventSource auto-reconnects) carries none.
  es.addEventListener("error", (e) => {
    const data = (e as MessageEvent).data;
    if (!data) {
      console.error("session mirror SSE connection error");
      return;
    }
    try {
      onEvent("error", JSON.parse(data));
    } catch (err) {
      console.error("failed to parse 'error' mirror frame", err);
    }
  });

  return () => es.close();
}

// connectAgentRunsSSE opens a live stream of the agent-run tree (the web "agent
// preview"). The server pushes a full snapshot on every change. Returns a
// cleanup function that closes the stream.
export function connectAgentRunsSSE(
  session: string | undefined,
  onRuns: (runs: AgentRun[]) => void,
): () => void {
  const params = new URLSearchParams();
  if (session) params.set("session", session);
  if (_token) params.set("token", _token);

  const es = new EventSource(apiPath(`/api/agents/runs/stream?${params}`));
  es.addEventListener("runs", (e) => {
    try {
      onRuns(JSON.parse(e.data) as AgentRun[]);
    } catch (err) {
      console.error("failed to parse agent runs frame", err);
    }
  });
  // Native EventSource auto-reconnects on transient errors; only log so a
  // dropped stream is visible without tearing the UI down.
  es.addEventListener("error", () => {
    console.error("agent runs SSE connection error");
  });

  return () => es.close();
}
