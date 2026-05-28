import type {
  ChatResponse,
  SessionInfo,
  ModelInfo,
  AgentInfo,
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
  getSession: (id: string) => fetchJSON<SessionInfo>(`/api/sessions/${id}`),
  listModels: () => fetchJSON<ModelInfo[]>("/api/models"),
  listAgents: () => fetchJSON<AgentInfo[]>("/api/agents"),
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
};

export type SSEEventHandler = (event: string, data: unknown) => void;

export function connectChatSSE(
  message: string,
  session: string | undefined,
  onEvent: SSEEventHandler,
  onError?: (err: Error) => void,
): () => void {
  const params = new URLSearchParams({ message });
  if (session) params.set("session", session);

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
