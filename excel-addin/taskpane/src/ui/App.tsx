import { useCallback, useEffect, useMemo, useReducer, useRef, useState } from "react";
import { ChangesView, isThisTurn } from "./ChangesView";
import { LogsView } from "./LogsView";
import { ContextGauge, FileChips, ModeControl, ModelPicker, OverflowMenu } from "./controls";
import { errText, log } from "../log";
import type { BridgeStatus } from "../bridgeClient";
import type { Deps } from "../boot";
import { initialState, reduce, resync, type ChatMsg, type PermissionReq } from "../chatState";
import { contextBlock, withContext } from "../context";
import { stripContext, summarizeTool } from "../format";
import type { SessionRef } from "../identity";
import { sessionTitle } from "../identity";
import { choiceFor, permissionFor, savedModeView, type ModeChoice } from "../modes";
import { connectedModels, type ModelInfo } from "../models";
import type { Envelope } from "../ocodeClient";
import type { WorkbookInfo } from "../port";
import type { Step } from "../tools/journal";

type Action = { type: "env"; env: Envelope } | { type: "resync"; sid: string; transcript: any[]; pending?: any };
const reducer = (s: typeof initialState, a: Action) =>
  a.type === "env" ? reduce(s, a.env) : resync(s, a.sid, a.transcript, a.pending ?? {});
/** The journal keeps at most 100 steps, so this lists all of them; the chat strip shows the newest 8. */
const CHANGES_LIMIT = 100;
/** The open session's context window as ocode reported it on the last refresh. `sid` says which session that was. */
interface Gauge { sid: string; current: number; max: number; model: string }

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
  // The permission picker. Auto-permission is server-wide, so `autoOn` mirrors what ocode last reported.
  const [choice, setChoice] = useState<ModeChoice>("ask");
  const [autoOn, setAutoOn] = useState(false);
  const [confirmYolo, setConfirmYolo] = useState(false);
  const [providers, setProviders] = useState<string[]>([]);
  const [models, setModels] = useState<ModelInfo[]>([]);
  // undefined: no model picked, so the next chat uses ocode's default. Otherwise the open session's model, or the pick.
  const [model, setModel] = useState<string>();
  const [gauge, setGauge] = useState<Gauge | null>(null);
  // A locked or sandbox chat. The picker has no segment for these, so the open chat's mode is a read-only chip. `sid` says which chat.
  const [savedChip, setSavedChip] = useState<{ sid: string; mode: "locked" | "sandbox" } | null>(null);
  const [recap, setRecap] = useState<string | null>(null);
  // The file state the bridge is sent (InfoReporter collects it every 10 s); re-read here for the strip chips.
  const [file, setFile] = useState<WorkbookInfo | null>(() => deps.info());
  const [confirmCompact, setConfirmCompact] = useState(false);
  // Whether this workbook opens the pane by itself. Read from the workbook at mount; changed only from the ⋯ menu.
  const [autoOpenOn, setAutoOpenOn] = useState(false);
  const [compacting, setCompacting] = useState(false);
  const activeRef = useRef(active);
  activeRef.current = active;
  // Epoch ms of the last Send. Steps written at or after it are "this turn" on the Changes tab and in its badge.
  const [turnStart, setTurnStart] = useState<number | null>(null);
  const connected = useMemo(() => connectedModels(models, providers), [models, providers]);

  const refreshChanges = useCallback(async () => setChanges(await journal.list(CHANGES_LIMIT)), [journal]);
  const guard = useCallback(async (f: () => Promise<unknown>) => { try { setError(null); await f(); } catch (e) { log("error", "ui", errText(e)); setError((e as Error).message); } }, []);

  const refreshContext = useCallback(async (sid: string) => {
    const t0 = performance.now();
    const c = await ocode.getContext(sid);
    // A model ocode's registry does not know reports no window (agent.ModelWindow returns 0). That is not an error: the
    // gauge simply has nothing to show, so hide it rather than failing the whole open flow.
    if (!(c.max_tokens > 0)) {
      setGauge(null);
      setModel(c.model);
      log("warn", "ui", `ocode reported no context window for session ${sid}; hiding the gauge`);
      return;
    }
    setGauge({ sid, current: c.current_tokens, max: c.max_tokens, model: c.model });
    setModel(c.model);
    log("info", "ui", `context ${c.current_tokens} of ${c.max_tokens} tokens`, Math.round(performance.now() - t0));
  }, [ocode]);

  // The chat's saved permission mode (the bridge's GET /bridge/session-mode/{id}) sets the picker. A failed read is logged and leaves
  // the picker as it is: the chat itself opened, so no error is shown.
  const refreshSavedMode = useCallback(async (sid: string) => {
    try {
      const [status, auto] = await Promise.all([ocode.getSavedMode(sid), ocode.getAutoPermission()]);
      setAutoOn(auto.enabled);
      // A read that finishes after the user moved to another chat must not set this one's picker.
      if (activeRef.current !== sid) return;
      const view = savedModeView(status.permission_mode, auto.enabled);
      if ("chip" in view) {
        setSavedChip({ sid, mode: view.chip });
      } else {
        setSavedChip(null);
        setChoice(view.choice);
      }
      log("info", "ui", `chat ${sid} is in ${status.permission_mode} mode`);
    } catch (e) {
      log("warn", "ui", `could not read the chat's permission mode: ${errText(e)}`);
    }
  }, [ocode]);

  const openSession = useCallback((sid: string) => guard(async () => {
    const t0 = performance.now();
    // A reload of the same session (reconnect, compaction) keeps the recap card open.
    if (sid !== activeRef.current) { setRecap(null); setConfirmCompact(false); }
    setActive(sid);
    const s = await ocode.getSession(sid);
    dispatch({ type: "resync", sid, transcript: s.messages });
    // Before the gauge: a failed context read must not leave the picker showing the default for a YOLO chat.
    await refreshSavedMode(sid);
    await refreshContext(sid);
    log("info", "ui", `opened session ${sid}`, Math.round(performance.now() - t0));
  }), [guard, ocode, refreshContext, refreshSavedMode]);

  useEffect(() => {
    deps.onBridgeStatus(setBridge);
    guard(async () => { setSessions(await index.list()); await refreshChanges(); });
    // With no chat open, the picker shows the server-wide switch as Ask or Auto. A chat's saved mode is read when it opens.
    guard(async () => {
      const auto = await ocode.getAutoPermission();
      setAutoOn(auto.enabled);
      setChoice(choiceFor({ mode: "normal", auto: auto.enabled }));
    });
    guard(async () => setProviders((await ocode.connectedProviders()).providers));
    guard(async () => setModels(await ocode.listModels()));
    const ac = new AbortController();
    void ocode.events({
      projects: [workspace], signal: ac.signal, onStatus: setEvents,
      onEnvelope: (env) => {
        dispatch({ type: "env", env });
        if (env.event === "tool_result") void refreshChanges();
        if ((env.event === "turn_done" || env.event === "turn_error") && env.session_id && env.session_id === activeRef.current) {
          void refreshContext(env.session_id).catch((e) => log("warn", "ui", `could not refresh the context gauge: ${errText(e)}`));
        }
      },
      // events missed while disconnected are not replayed: reload the open session from the server
      onReconnect: () => { const sid = activeRef.current; if (sid) void openSession(sid); },
    });
    return () => ac.abort();
  }, [deps, guard, index, ocode, openSession, refreshChanges, refreshContext, workspace]);

  useEffect(() => {
    void port.autoOpen().then(setAutoOpenOn, (e) => log("warn", "ui", `could not read the auto-open setting: ${errText(e)}`));
  }, [port]);
  useEffect(() => { if (tab === "changes") void guard(refreshChanges); }, [tab, guard, refreshChanges]);
  useEffect(() => {
    const t = setInterval(() => setFile(deps.info()), 2000);
    return () => clearInterval(t);
  }, [deps]);

  const view = active ? chat.sessions[active] : undefined;
  const thisTurn = changes.filter((s) => isThisTurn(s, turnStart)).length;

  // Auto-permission is server-wide, so it is written only when it differs from what ocode last reported.
  const applyAuto = async (enabled: boolean) => {
    if (enabled === autoOn) return;
    const r = await ocode.setAutoPermission(enabled);
    setAutoOn(r.enabled);
    log("info", "ui", `auto-permission ${r.enabled ? "on" : "off"}, for every ocode session on this server`);
  };

  const send = () => guard(async () => {
    const body = text.trim();
    if (!body) return;
    setText("");
    const msg = withContext(await contextBlock(port, workbook, fileName), body);
    setTurnStart(Date.now());
    if (active) { await ocode.send(active, msg); return; }
    // The picker applies to the first message. YOLO leaves auto-permission alone, since it is not consulted.
    if (choice !== "yolo") await applyAuto(permissionFor(choice).auto);
    const r = await ocode.startChat({ content: msg, projectPath: workspace, model, permissionMode: choice === "yolo" ? "yolo" : "normal" });
    setModel(r.model);
    const ref = { id: r.sessionId, title: sessionTitle(fileName), created: new Date().toISOString() };
    setSessions(await index.add(ref));
    void ocode.setTitle(r.sessionId, ref.title).catch((e) => log("warn", "ui", `could not set the session title: ${errText(e)}`)); // best effort: only affects ocode's own list
    setActive(r.sessionId);
  });

  // Each switch starts from the default (Ask, or Auto if the server-wide switch is on), so YOLO never carries over to another
  // chat. openSession then applies that chat's saved mode.
  const resetPicker = () => { setChoice(choiceFor({ mode: "normal", auto: autoOn })); setConfirmYolo(false); };
  const newChat = () => { setActive(null); setRecap(null); setConfirmCompact(false); resetPicker(); };
  const switchSession = (sid: string) => { resetPicker(); void openSession(sid); };

  // Ask and Auto write the session's mode and the server-wide switch now; YOLO waits for the confirm each time.
  const pickMode = (next: ModeChoice) => {
    if (next === "yolo") { setConfirmYolo(true); return; }
    setConfirmYolo(false);
    void guard(async () => {
      const t0 = performance.now();
      const p = permissionFor(next);
      if (active) {
        // Always sent when a session is open, even if the picker may not match the session (a failed mode read leaves the default showing).
        await ocode.setPermissionMode(active, p.mode);
        setSavedChip(null);
        await applyAuto(p.auto);
      }
      setChoice(next);
      log("info", "ui", `permission mode ${next}${active ? "" : " (for the next chat)"}`, Math.round(performance.now() - t0));
    });
  };
  const enableYolo = () => guard(async () => {
    const t0 = performance.now();
    if (active) {
      await ocode.setPermissionMode(active, "yolo");
      setSavedChip(null);
    }
    setChoice("yolo");
    setConfirmYolo(false);
    log("info", "ui", `permission mode yolo${active ? "" : " (for the next chat)"}`, Math.round(performance.now() - t0));
  });
  const pickModel = (name: string) => guard(async () => {
    const t0 = performance.now();
    if (active) {
      await ocode.setModel(active, name);
      await refreshContext(active); // the context window size depends on the model
    }
    setModel(name);
    log("info", "ui", `model ${name}${active ? "" : " (for the next chat)"}`, Math.round(performance.now() - t0));
  });
  const showRecap = () => guard(async () => {
    const t0 = performance.now();
    const r = await ocode.recap(active!);
    setRecap(r.recap);
    log("info", "ui", "recap ready", Math.round(performance.now() - t0));
  });
  // Compaction is synchronous on ocode's side: the POST returns when it is done, so the reload after it sees the result.
  const compact = () => guard(async () => {
    const t0 = performance.now();
    const sid = active!;
    setConfirmCompact(false);
    setCompacting(true);
    try {
      await ocode.compact(sid);
      await openSession(sid);
    } finally {
      setCompacting(false);
    }
    log("info", "ui", "compacted", Math.round(performance.now() - t0));
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
  // Saves a setting inside the workbook file, so it runs only from this explicit menu choice.
  const toggleAutoOpen = () => guard(async () => {
    const next = !autoOpenOn;
    await port.setAutoOpen(next);
    setAutoOpenOn(next);
    log("info", "ui", `open ocode automatically for this workbook: ${next ? "on" : "off"}`);
  });

  const yoloOn = choice === "yolo";
  return (
    <div className="app">
      <header>
        <span title={`bridge: ${bridge}`} className={`dot ${bridge === "connected" ? "on" : ""}`} />
        <span title={`ocode events: ${events ? "connected" : "disconnected"}`} className={`dot ${events ? "on" : ""}`} />
        <select className="session" value={active ?? ""} onChange={(e) => (e.target.value ? switchSession(e.target.value) : newChat())} aria-label="Session">
          <option value="">New chat</option>
          {sessions.map((s) => <option key={s.id} value={s.id}>{new Date(s.created).toLocaleString()}</option>)}
        </select>
        <button onClick={newChat}>New</button>
        <OverflowMenu items={[
          { label: "Recap", disabled: !active, onSelect: () => void showRecap() },
          { label: "Compact", disabled: !active || compacting, onSelect: () => setConfirmCompact(true) },
          { label: "Open ocode automatically for this workbook", disabled: false, checked: autoOpenOn, note: "This saves a setting inside the workbook file.", onSelect: () => void toggleAutoOpen() },
        ]} />
      </header>
      <nav className="tabs">
        <button className={tab === "chat" ? "on" : ""} onClick={() => setTab("chat")}>Chat</button>
        <button className={tab === "changes" ? "on" : ""} onClick={() => setTab("changes")}>
          Changes{thisTurn > 0 && <span className="count" title="Changes this turn">{thisTurn}</span>}
        </button>
        <button className={tab === "logs" ? "on" : ""} onClick={() => setTab("logs")}>Logs</button>
      </nav>
      {tab === "logs" ? <LogsView /> : tab === "changes" ? (<>
        {error && <div className="err" role="alert">{error}</div>}
        <ChangesView steps={changes} turnStart={turnStart} onUndo={undo} onShow={show} />
      </>) : <>

      <div className="strip">
        <ModeControl value={choice} onPick={pickMode} />
        {yoloOn && <span className="badge bad">YOLO on</span>}
        {savedChip && savedChip.sid === active && <span className="chip" title={`This chat is in ${savedChip.mode} mode, which the picker does not offer.`}>{savedChip.mode}</span>}
        <FileChips info={file} />
        <ModelPicker current={model} models={connected} onPick={(name) => void pickModel(name)} />
        {gauge && gauge.sid === active && <ContextGauge current={gauge.current} max={gauge.max} model={gauge.model} />}
        {choice === "auto" && <span className="note">Applies to ALL ocode sessions on this server</span>}
      </div>

      {confirmYolo && (
        <div className="card danger">
          <div>YOLO runs every tool without asking. Writes are still checkpointed and undoable here. Enable?</div>
          <div className="row">
            <button className="primary" onClick={() => void enableYolo()}>Enable</button>
            <button onClick={() => setConfirmYolo(false)}>Cancel</button>
          </div>
        </div>
      )}

      {bridge === "evicted" && (
        <div className="err" role="alert">This workbook is open in another ocode pane. Reload this pane to take over.</div>
      )}

      <div className="msgs">
        {recap !== null && (
          <div className="card">
            <div className="card-head"><b>Recap</b><button onClick={() => setRecap(null)}>Dismiss</button></div>
            <div className="recap-text">{recap}</div>
          </div>
        )}
        {confirmCompact && (
          <div className="card">
            <div>This replaces the history with a summary and cannot be undone. Compact?</div>
            <div className="row">
              <button onClick={() => void compact()}>Compact</button>
              <button onClick={() => setConfirmCompact(false)}>Cancel</button>
            </div>
          </div>
        )}
        {compacting && <div className="banner">Compacting…</div>}
        {notice && <div className="msg tool">{notice} <button onClick={() => setNotice(undefined)}>Dismiss</button></div>}
        {!view && <div className="msg tool">Ask about this workbook, or ask for a change. Every change is checkpointed and can be undone below.</div>}
        {view?.messages.map((m) => <Message key={m.id} m={m} open={!!open[m.id]} toggle={() => setOpen({ ...open, [m.id]: !open[m.id] })} />)}
        {view?.permissions.map((p) => (
          <div className="card ask" key={p.request_id}>
            <b>{p.tool}</b> wants to run{p.summary ? `: ${p.summary}` : ""}
            <div className="row">
              <button className="primary" onClick={() => resolve(p, "allow")}>Allow</button>
              <button onClick={() => resolve(p, "always_rule")}>Always allow</button>
              <button onClick={() => resolve(p, "deny")}>Deny</button>
            </div>
          </div>
        ))}
        {view?.questions.map((q) => (
          <div className="card ask" key={q.request_id}>
            {(q.questions as any[]).map((qq, i) => (
              <div key={i}><b>{qq.header}</b> {qq.question}<div className="row">{(qq.options ?? []).map((o: any) => <button key={o.label} onClick={() => answer(q.request_id, qq, o.label)}>{o.label}</button>)}</div></div>
            ))}
          </div>
        ))}
      </div>

      {changes.length > 0 && (
        <ul className="changes">
          {changes.slice(0, 8).map((c) => (
            <li key={c.id}><span>{c.undone ? <s>{c.tool} {c.sheet}!{c.address}</s> : <>{c.tool} {c.sheet}!{c.address}</>}</span>
              <button disabled={c.undone} onClick={() => undo(c.id)}>Undo</button></li>
          ))}
        </ul>
      )}

      {(error || view?.error) && <div className="err" role="alert">{error ?? view?.error}</div>}

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
