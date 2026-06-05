import type {
  ChatResponse,
  SessionInfo,
  SessionDetail,
  ModelInfo,
  AgentInfo,
  AgentRun,
} from "./types";

const BASE = "";

async function fetchJSON<T>(path: string, init?: RequestInit): Promise<T> {
  const res = await fetch(`${BASE}${path}`, {
    headers: { "Content-Type": "application/json" },
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
  getSession: (id: string) => fetchJSON<SessionDetail>(`/api/sessions/${id}`),
  listModels: () => fetchJSON<ModelInfo[]>("/api/models"),
  listAgents: () => fetchJSON<AgentInfo[]>("/api/config/agents"),
  listAgentRuns: (session?: string) =>
    fetchJSON<AgentRun[]>(
      `/api/agents/runs${session ? `?session=${encodeURIComponent(session)}` : ""}`,
    ),
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
};

export type SSEEventHandler = (event: string, data: unknown) => void;

export function connectChatSSE(
  message: string,
  session: string | undefined,
  model: string | undefined,
  onEvent: SSEEventHandler,
  onError?: (err: Error) => void,
): () => void {
  const params = new URLSearchParams({ message });
  if (session) params.set("session", session);
  if (model) params.set("model", model);

  const es = new EventSource(`/api/chat/stream?${params}`);

  es.addEventListener("text", (e) => onEvent("text", JSON.parse(e.data)));
  es.addEventListener("tool_start", (e) => onEvent("tool_start", JSON.parse(e.data)));
  es.addEventListener("tool_result", (e) => onEvent("tool_result", JSON.parse(e.data)));
  es.addEventListener("tool_error", (e) => onEvent("tool_error", JSON.parse(e.data)));
  es.addEventListener("permission_required", (e) => onEvent("permission_required", JSON.parse(e.data)));
  es.addEventListener("session", (e) => onEvent("session", JSON.parse(e.data)));
  es.addEventListener("done", (e) => {
    onEvent("done", JSON.parse(e.data));
    es.close();
  });
  es.addEventListener("error", () => {
    onEvent("error", { error: "connection lost" });
    onError?.(new Error("SSE connection error"));
    es.close();
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

  const es = new EventSource(`/api/agents/runs/stream?${params}`);
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
