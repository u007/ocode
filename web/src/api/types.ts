export interface Message {
  role: "user" | "assistant" | "system";
  content: string;
}

export interface ChatRequest {
  content: string;
  sessionId?: string;
  model?: string;
}

export interface ChatResponse {
  content: string;
  sessionId: string;
  model: string;
}

export interface SessionInfo {
  id: string;
  title: string;
  created_at: string;
  updated_at: string;
}

export interface ModelInfo {
  name: string;
  provider: string;
}

export interface AgentInfo {
  name: string;
  description: string;
  mode: string;
}

export interface SSETextEvent {
  delta: string;
}

export interface SSEToolStartEvent {
  tool: string;
  command?: string;
  content?: string;
}

export interface SSEToolResultEvent {
  tool: string;
  output: string;
}

export interface SSEToolErrorEvent {
  tool: string;
  error: string;
}

export interface SSEPermissionEvent {
  tool: string;
  command?: string;
  request_id: string;
}

export interface SSEDoneEvent {
  session_id: string;
  model: string;
}

export interface SSESessionEvent {
  session_id: string;
}
