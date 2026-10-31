import { useCallback, useEffect, useReducer, useRef, useState } from "react";
import { ChangesView } from "./ChangesView";
import { LogsView } from "./LogsView";
import { errText, log } from "../log";
import type { BridgeStatus } from "../bridgeClient";
import type { Deps } from "../boot";
import { initialState, reduce, resync, type ChatMsg, type PermissionReq } from "../chatState";
import { contextBlock, withContext } from "../context";
import { stripContext, summarizeTool } from "../format";
import type { SessionRef } from "../identity";
import { sessionTitle } from "../identity";
import type { Envelope } from "../ocodeClient";
import type { Step } from "../tools/journal";

type Action = { type: "env"; env: Envelope } | { type: "resync"; sid: string; transcript: any[]; pending?: any };
const reducer = (s: typeof initialState, a: Action) =>
  a.type === "env" ? reduce(s, a.env) : resync(s, a.sid, a.transcript, a.pending ?? {});
/** The journal keeps at most 100 steps, so this lists all of them; the chat strip shows the newest 8. */
const CHANGES_LIMIT = 100;

export function App({ deps }: { deps: Deps }) {
  const { ocode, index, journal, port, runner, workbook, fileName, workspace } = deps;
  const [chat, dispatch] = useReducer(reducer, initialState);
  const [sessions, setSessions] = useState<SessionRef[]>([]);
  const [active, setActive] = useState<string | null>(null);
  const [text, setText] = useState("");
  const [bridge, setBridge] = useState<BridgeStatus>("connecting");
  const [events, setEvents] = useState(false);
  const [tab, setTab] = useState<"chat" | "logs" | "changes">("chat");
  const [error, setError] = useState<string | null>(null);
  const [changes, setChanges] = useState<Step[]>([]);
  const [notice, setNotice] = useState<string | undefined>(deps.notice);
  const [open, setOpen] = useState<Record<string, boolean>>({});
  const activeRef = useRef(active);
  activeRef.current = active;
  // Epoch ms of the last Send. Steps written at or after it are "this turn" on the Changes tab.
  const turnStartRef = useRef<number | null>(null);

  const refreshChanges = useCallback(async () => setChanges(await journal.list(CHANGES_LIMIT)), [journal]);
  const guard = useCallback(async (f: () => Promise<unknown>) => { try { setError(null); await f(); } catch (e) { log("error", "ui", errText(e)); setError((e as Error).message); } }, []);

  const openSession = useCallback((sid: string) => guard(async () => {
    setActive(sid);
    const s = await ocode.getSession(sid);
    dispatch({ type: "resync", sid, transcript: s.messages });
  }), [guard, ocode]);

  useEffect(() => {
    deps.onBridgeStatus(setBridge);
    guard(async () => { setSessions(await index.list()); await refreshChanges(); });
    const ac = new AbortController();
    void ocode.events({
      projects: [workspace], signal: ac.signal, onStatus: setEvents,
      onEnvelope: (env) => { dispatch({ type: "env", env }); if (env.event === "tool_result") void refreshChanges(); },
      // events missed while disconnected are not replayed: reload the open session from the server
      onReconnect: () => { const sid = activeRef.current; if (sid) void openSession(sid); },
    });
    return () => ac.abort();
  }, [deps, guard, index, ocode, openSession, refreshChanges, workspace]);

  useEffect(() => { if (tab === "changes") void guard(refreshChanges); }, [tab, guard, refreshChanges]);

  const view = active ? chat.sessions[active] : undefined;

  const send = () => guard(async () => {
    const body = text.trim();
    if (!body) return;
    setText("");
    const msg = withContext(await contextBlock(port, workbook, fileName), body);
    turnStartRef.current = Date.now();
    if (active) { await ocode.send(active, msg); return; }
    const r = await ocode.startChat({ content: msg, projectPath: workspace, permissionMode: "normal" });
    const ref = { id: r.sessionId, title: sessionTitle(fileName), created: new Date().toISOString() };
    await index.add(ref);
    void ocode.setTitle(r.sessionId, ref.title).catch((e) => log("warn", "ui", `could not set the session title: ${errText(e)}`)); // best effort: only affects ocode's own list
    setSessions(await index.list());
    setActive(r.sessionId);
  });

  const resolve = (p: PermissionReq, decision: "allow" | "deny" | "always_rule") => {
    log("info", "ui", `permission ${p.tool}: ${decision}`);
    return guard(() => ocode.resolvePermission({ requestId: p.request_id, sessionId: active!, decision }));
  };
  const answer = (requestId: string, qq: { header?: string; question?: string }, label: string) =>
    guard(() => ocode.answerQuestion({ requestId, sessionId: active!, answers: [{ header: qq.header, question: qq.question ?? "", answers: [{ label }] }] }));
  // Undo goes through the Runner, like the agent's own calls, so it never interleaves with a write in
  // flight, and through the same tool, so the "cells changed since" conflict check is identical.
  const undo = (id: string) => guard(async () => {
    try {
      const r = await runner.handle({ id: `ui-undo-${Date.now()}`, tool: "undo_step", args: { step_id: id } });
      if (!r.ok) throw new Error(r.error ?? "Undo failed");
    } finally {
      await refreshChanges();
    }
  });
  const show = (s: Step) => guard(() => port.select(s.sheet, s.address));

  return (
    <div className="app">
      <header>
        <span title={`bridge: ${bridge}`} className={`dot ${bridge === "connected" ? "on" : ""}`} />
        <span title={`ocode events: ${events ? "connected" : "disconnected"}`} className={`dot ${events ? "on" : ""}`} />
        <select value={active ?? ""} onChange={(e) => (e.target.value ? void openSession(e.target.value) : setActive(null))} aria-label="Session">
          <option value="">New chat</option>
          {sessions.map((s) => <option key={s.id} value={s.id}>{new Date(s.created).toLocaleString()}</option>)}
        </select>
        <button onClick={() => setActive(null)}>New</button>
        <button disabled={!active} onClick={() => guard(async () => setText((await ocode.recap(active!)).recap))}>Recap</button>
        <button disabled={!active} onClick={() => guard(() => ocode.compact(active!))}>Compact</button>
      </header>
      <nav className="tabs">
        <button className={tab === "chat" ? "primary" : ""} onClick={() => setTab("chat")}>Chat</button>
        <button className={tab === "changes" ? "primary" : ""} onClick={() => setTab("changes")}>Changes</button>
        <button className={tab === "logs" ? "primary" : ""} onClick={() => setTab("logs")}>Logs</button>
      </nav>
      {tab === "logs" ? <LogsView /> : tab === "changes" ? (<>
        {error && <div className="err" role="alert">{error}</div>}
        <ChangesView steps={changes} turnStart={turnStartRef.current} onUndo={undo} onShow={show} />
      </>) : <>

      {bridge === "evicted" && (
        <div className="err" role="alert">This workbook is open in another ocode pane. Reload this pane to take over.</div>
      )}

      <div className="msgs">
        {notice && <div className="msg tool">{notice} <button onClick={() => setNotice(undefined)}>Dismiss</button></div>}
        {!view && <div className="msg tool">Ask about this workbook, or ask for a change. Every change is checkpointed and can be undone below.</div>}
        {view?.messages.map((m) => <Message key={m.id} m={m} open={!!open[m.id]} toggle={() => setOpen({ ...open, [m.id]: !open[m.id] })} />)}
        {view?.permissions.map((p) => (
          <div className="card" key={p.request_id}>
            <b>{p.tool}</b> wants to run{p.summary ? `: ${p.summary}` : ""}
            <div className="row">
              <button className="primary" onClick={() => resolve(p, "allow")}>Allow</button>
              <button onClick={() => resolve(p, "always_rule")}>Always allow</button>
              <button onClick={() => resolve(p, "deny")}>Deny</button>
            </div>
          </div>
        ))}
        {view?.questions.map((q) => (
          <div className="card" key={q.request_id}>
            {(q.questions as any[]).map((qq, i) => (
              <div key={i}><b>{qq.header}</b> {qq.question}<div className="row">{(qq.options ?? []).map((o: any) => <button key={o.label} onClick={() => answer(q.request_id, qq, o.label)}>{o.label}</button>)}</div></div>
            ))}
          </div>
        ))}
        {(error || view?.error) && <div className="err">{error ?? view?.error}</div>}
      </div>

      {changes.length > 0 && (
        <ul className="changes">
          {changes.slice(0, 8).map((c) => (
            <li key={c.id}><span>{c.undone ? <s>{c.tool} {c.sheet}!{c.address}</s> : <>{c.tool} {c.sheet}!{c.address}</>}</span>
              <button disabled={c.undone} onClick={() => undo(c.id)}>Undo</button></li>
          ))}
        </ul>
      )}

      <div className="composer">
        <textarea value={text} onChange={(e) => setText(e.target.value)} placeholder="Ask ocode about this workbook" aria-label="Message"
          onKeyDown={(e) => { if (e.key === "Enter" && !e.shiftKey) { e.preventDefault(); void send(); } }} />
        {view?.streaming
          ? <button onClick={() => guard(() => ocode.cancel(active!))}>Stop</button>
          : <button className="primary" disabled={!text.trim()} onClick={() => void send()}>Send</button>}
      </div>
      </>}
    </div>
  );
}

function Message({ m, open, toggle }: { m: ChatMsg; open: boolean; toggle: () => void }) {
  if (m.kind === "tool") {
    return (
      <div className="msg tool" onClick={toggle}>
        {m.status === "running" ? "… " : ""}{summarizeTool(m)}
        {open && <pre>{m.args}{m.output ? `\n→ ${m.output.slice(0, 2000)}` : ""}</pre>}
      </div>
    );
  }
  if (m.kind === "notice") {
    return (
      <div className="msg notice" onClick={toggle}>
        Earlier conversation was compacted. Tap to read the summary.
        {open && <pre>{m.text}</pre>}
      </div>
    );
  }
  return <div className={`msg ${m.kind}`}>{m.kind === "user" ? stripContext(m.text) : m.text}</div>;
}
