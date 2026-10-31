import type { Envelope, TranscriptMessage } from "./ocodeClient";

export interface ChatMsg {
  id: string;
  kind: "user" | "assistant" | "tool" | "notice";
  text: string;
  tool?: string;
  args?: string;
  output?: string;
  status?: "running" | "done";
  userSeq?: number;
}
export interface PermissionReq { request_id: string; tool: string; command?: string; summary?: string; args?: unknown }
export interface QuestionReq { request_id: string; questions: unknown[] }

export interface SessionView {
  messages: ChatMsg[];
  streaming: boolean;
  permissions: PermissionReq[];
  questions: QuestionReq[];
  error?: string;
  lastSeq: number;
}

export interface ChatState { sessions: Record<string, SessionView> }
export const emptyView = (): SessionView => ({ messages: [], streaming: false, permissions: [], questions: [], lastSeq: 0 });
export const initialState: ChatState = { sessions: {} };

const COMPACTION_MARKER = "[ocode:compaction-summary]";

/** Rebuild the visible conversation from a stored transcript (history / restore / reconnect). */
export function fromTranscript(msgs: TranscriptMessage[]): ChatMsg[] {
  const out: ChatMsg[] = [];
  const byCall = new Map<string, ChatMsg>();
  msgs.forEach((m, i) => {
    if (m.role === "user") out.push({ id: `h${i}`, kind: "user", text: m.content });
    else if (m.role === "system" && m.content.startsWith(COMPACTION_MARKER)) {
      out.push({ id: `h${i}`, kind: "notice", text: m.content.slice(COMPACTION_MARKER.length).trim() });
    } else if (m.role === "assistant") {
      if (m.content) out.push({ id: `h${i}`, kind: "assistant", text: m.content });
      for (const tc of m.tool_calls ?? []) {
        const t: ChatMsg = { id: tc.id, kind: "tool", text: "", tool: tc.function.name, args: tc.function.arguments, status: "running" };
        byCall.set(tc.id, t);
        out.push(t);
      }
    } else if (m.role === "tool") {
      const t = m.tool_call_id ? byCall.get(m.tool_call_id) : undefined;
      if (t) { t.output = m.content; t.status = "done"; }
    }
  });
  return out;
}

export function reduce(state: ChatState, env: Envelope): ChatState {
  const sid = env.session_id;
  if (!sid) return state;
  const v = state.sessions[sid] ?? emptyView();
  if (env.seq && env.seq <= v.lastSeq && env.event !== "user_message") return state; // duplicate / replayed frame
  const next = apply({ ...v, lastSeq: Math.max(v.lastSeq, env.seq ?? 0) }, env);
  return { sessions: { ...state.sessions, [sid]: next } };
}

function apply(v: SessionView, env: Envelope): SessionView {
  const d = env.data ?? {};
  switch (env.event) {
    case "user_message": {
      if (d.user_seq !== undefined && v.messages.some((m) => m.kind === "user" && m.userSeq === d.user_seq)) return v;
      return { ...v, messages: [...v.messages, { id: `u${env.seq}`, kind: "user", text: String(d.content ?? ""), userSeq: d.user_seq }] };
    }
    case "text": {
      const last = v.messages[v.messages.length - 1];
      if (last && last.kind === "assistant" && last.status === "running") {
        return { ...v, streaming: true, messages: [...v.messages.slice(0, -1), { ...last, text: last.text + String(d.delta ?? "") }] };
      }
      return { ...v, streaming: true, messages: [...v.messages, { id: `a${env.seq}`, kind: "assistant", text: String(d.delta ?? ""), status: "running" }] };
    }
    case "tool_start":
      return { ...v, messages: [...closeAssistant(v.messages), { id: d.call_id ?? `t${env.seq}`, kind: "tool", text: "", tool: d.tool, args: d.command, status: "running" }] };
    case "tool_result":
      return { ...v, messages: v.messages.map((m) => (m.kind === "tool" && m.id === d.call_id ? { ...m, output: String(d.output ?? ""), status: "done" as const } : m)) };
    case "turn_started": return { ...v, streaming: true, error: undefined };
    case "turn_done": return { ...v, streaming: false, messages: closeAssistant(v.messages) };
    case "turn_error": return { ...v, streaming: false, error: String(d.error ?? d.message ?? "The turn failed"), messages: closeAssistant(v.messages) };
    case "permission": return { ...v, permissions: [...v.permissions.filter((p) => p.request_id !== d.request_id), d as PermissionReq] };
    case "permission_resolved": return { ...v, permissions: v.permissions.filter((p) => p.request_id !== d.request_id) };
    case "question": return { ...v, questions: [...v.questions.filter((q) => q.request_id !== d.request_id), d as QuestionReq] };
    case "question_resolved": return { ...v, questions: v.questions.filter((q) => q.request_id !== d.request_id) };
    default: return v;
  }
}

const closeAssistant = (msgs: ChatMsg[]): ChatMsg[] =>
  msgs.map((m) => (m.kind === "assistant" && m.status === "running" ? { ...m, status: "done" as const } : m));

/** Resync a session after a reconnect: the stored transcript wins; pending asks come from /state. */
export function resync(state: ChatState, sid: string, transcript: TranscriptMessage[], pending: { permissions?: PermissionReq[]; questions?: QuestionReq[] }): ChatState {
  const v = state.sessions[sid] ?? emptyView();
  return { sessions: { ...state.sessions, [sid]: { ...v, messages: fromTranscript(transcript), permissions: pending.permissions ?? [], questions: pending.questions ?? [], streaming: false } } };
}
