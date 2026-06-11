export interface ToolCall {
  id: string;
  type?: string;
  function: { name: string; arguments: string };
}

export interface Message {
  role: "user" | "assistant" | "system" | "tool";
  content: string;
  tool_calls?: ToolCall[];
  tool_call_id?: string;
  reasoning_content?: string;
}

// A part of the in-progress turn, streamed live before the authoritative
// snapshot lands at turn_done. Ordered as produced by the agent.
export type LivePart =
  | { kind: "thinking"; text: string }
  | { kind: "text"; text: string }
  | { kind: "tool"; tool: string; command?: string; output?: string };

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

export interface SessionDetail extends SessionInfo {
  messages: Message[];
}

export interface ModelInfo {
  name: string;
  model: string;
  provider: string;
  active: boolean;
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

export interface AgentRunMessage {
  role: string;
  content: string;
  toolCalls?: { name: string; arguments: string }[];
  toolCallId?: string;
}

export interface GitDiffFile {
  path: string;
  status: string;
  patch: string;
}

export interface ThemeColors {
  user: string;
  assistant: string;
  header: string;
  border: string;
  hint: string;
  text: string;
  background: string;
  status_bg: string;
  status_fg: string;
  selected_fg: string;
  selected_bg: string;
  success: string;
  error: string;
  accent: string;
  dim: string;
  thinking: string;
}

export interface ThemeResponse {
  name: string;
  colors: ThemeColors;
}

export interface AgentRun {
  id: string;
  name: string;
  status: "running" | "done" | "failed" | string;
  result?: string;
  err?: string;
  model?: string;
  startedAt: string;
  endedAt?: string;
  inputTokens: number;
  outputTokens: number;
  messages: AgentRunMessage[];
  children: AgentRun[];
}
