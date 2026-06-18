import { useChatState } from "../../stores/chatStore";
import { api } from "../../api/client";
import { useEffect, useState } from "react";
import type { LSPStatus, FileStatus } from "../../api/types";

interface Props {
  onClose?: () => void;
}

// StatusPanel shows the full TUI status snapshot: every modified file, every
// active LSP server, every pre-authorized path, the spending breakdown, and
// the model/session metadata. Opened from the "status" button in the
// StatusBar so the user can drill in without leaving the chat.
export default function StatusPanel({ onClose }: Props) {
  const { tuiStatus, sessionContext, spendingUSD } = useChatState();
  const [files, setFiles] = useState<FileStatus[]>([]);
  const [lsps, setLsps] = useState<LSPStatus[]>([]);
  const [spending, setSpending] = useState<{ spending_usd: number; records: number } | null>(null);

  // Fetch the dedicated file/lsp/spending endpoints on mount — these are
  // cheaper than re-fetching the whole tui-status, and the user only opens
  // this panel occasionally, so a small one-shot fetch is fine.
  useEffect(() => {
    let cancelled = false;
    api
      .getModifiedFiles()
      .then((res) => {
        if (!cancelled) setFiles(res.modified_files || []);
      })
      .catch(console.error);
    api
      .getLSPStatuses()
      .then((res) => {
        if (!cancelled) setLsps(res.lsp_servers || []);
      })
      .catch(console.error);
    api
      .getSpending()
      .then((res) => {
        if (!cancelled) setSpending(res);
      })
      .catch(console.error);
    return () => {
      cancelled = true;
    };
  }, []);

  const snap = tuiStatus;
  const sessionTitle = snap?.session_title || "(untitled)";
  const sessionId = snap?.session_id || "(no session)";
  const mainModel = snap?.main_model || "";
  const smallModel = snap?.small_model || "";
  const smallOn = snap?.small_model_enabled ?? false;
  const advisorModel = snap?.advisor_model || "";
  const advisorOn = snap?.advisor_enabled ?? false;
  const ideStatus = snap?.ide_status || "(unknown)";
  const cwd = snap?.cwd || "";
  const ctxCur = sessionContext?.currentTokens ?? snap?.context_current_tokens ?? 0;
  const ctxMax = sessionContext?.maxTokens ?? snap?.context_max_tokens ?? 0;
  const subagent = snap?.subagent_model || "";
  const extraPaths = snap?.extra_allowed_paths || [];
  // Prefer the dedicated /api/files/modified fetch; fall back to the
  // snapshot's list if that endpoint isn't reachable (e.g. headless).
  const modified = files.length > 0 ? files : snap?.modified_files || [];
  const lsp = lsps.length > 0 ? lsps : snap?.lsp_servers || [];
  const spendUSD = spendingUSD ?? spending?.spending_usd ?? snap?.spending_usd ?? null;

  return (
    <div className="flex flex-col h-full overflow-auto bg-zinc-950 text-zinc-200">
      <div className="flex items-center justify-between border-b border-zinc-700 px-4 py-3 sticky top-0 bg-zinc-950 z-10">
        <h2 className="text-lg font-semibold">TUI status</h2>
        {onClose && (
          <button
            type="button"
            onClick={onClose}
            className="text-zinc-500 hover:text-zinc-300 px-2"
            title="Close"
          >
            ✕
          </button>
        )}
      </div>

      <div className="px-4 py-4 space-y-6 text-sm">
        {/* Session */}
        <section>
          <h3 className="text-xs uppercase tracking-wide text-zinc-500 mb-2">
            Session
          </h3>
          <Row k="Title" v={sessionTitle} />
          <Row k="ID" v={sessionId} mono />
          <Row k="CWD" v={cwd} mono />
          {snap?.updated_at && (
            <Row k="Last update" v={snap.updated_at} mono />
          )}
        </section>

        {/* Models */}
        <section>
          <h3 className="text-xs uppercase tracking-wide text-zinc-500 mb-2">
            Models
          </h3>
          <Row k="Main" v={mainModel || "(none)"} />
          <Row
            k="Small"
            v={smallModel ? `${smallModel} (${smallOn ? "on" : "off"})` : "(none)"}
          />
          <Row
            k="Advisor"
            v={
              advisorModel
                ? `${advisorModel} (${advisorOn ? "on" : "off"})`
                : "(none)"
            }
          />
          {subagent && <Row k="Subagent" v={subagent} />}
        </section>

        {/* IDE */}
        <section>
          <h3 className="text-xs uppercase tracking-wide text-zinc-500 mb-2">
            IDE
          </h3>
          <Row k="Status" v={ideStatus} />
          {snap?.ide_mode && <Row k="Mode" v={snap.ide_mode} />}
        </section>

        {/* Context */}
        <section>
          <h3 className="text-xs uppercase tracking-wide text-zinc-500 mb-2">
            Context window
          </h3>
          {ctxMax > 0 ? (
            <>
              <Row
                k="Current / Max"
                v={`${ctxCur.toLocaleString()} / ${ctxMax.toLocaleString()} tokens`}
                mono
              />
              <div className="mt-2 h-2 bg-zinc-800 rounded overflow-hidden">
                <div
                  className="h-full bg-emerald-500"
                  style={{
                    width: `${Math.min(100, (ctxCur / ctxMax) * 100).toFixed(1)}%`,
                  }}
                />
              </div>
            </>
          ) : (
            <Row k="Usage" v="(unknown)" />
          )}
          {snap?.context_model && <Row k="Model" v={snap.context_model} />}
        </section>

        {/* Spending */}
        <section>
          <h3 className="text-xs uppercase tracking-wide text-zinc-500 mb-2">
            Spending
          </h3>
          {spending ? (
            <>
              <Row
                k="USD (today)"
                v={`$${spendUSD?.toFixed(4) ?? "0.0000"}`}
                mono
              />
              <Row k="Records" v={String(spending.records)} mono />
            </>
          ) : (
            <Row
              k="USD"
              v={spendUSD != null ? `$${spendUSD.toFixed(4)}` : "(loading)"}
            />
          )}
        </section>

        {/* Modified files */}
        <section>
          <h3 className="text-xs uppercase tracking-wide text-zinc-500 mb-2">
            Modified files ({modified.length})
          </h3>
          {modified.length === 0 ? (
            <p className="text-zinc-600">(none)</p>
          ) : (
            <ul className="space-y-1 font-mono text-xs">
              {modified.map((f) => (
                <li key={f.path} className="flex items-center gap-2">
                  <span
                    className={
                      f.status === "M"
                        ? "text-amber-400 w-3"
                        : f.status === "A"
                          ? "text-emerald-400 w-3"
                          : f.status === "D"
                            ? "text-red-400 w-3"
                            : "text-zinc-500 w-3"
                    }
                    title={f.status || "?"}
                  >
                    {f.status || "?"}
                  </span>
                  <span className="truncate" title={f.path}>
                    {f.path}
                  </span>
                </li>
              ))}
            </ul>
          )}
        </section>

        {/* LSP servers */}
        <section>
          <h3 className="text-xs uppercase tracking-wide text-zinc-500 mb-2">
            LSP servers ({lsp.length})
          </h3>
          {lsp.length === 0 ? (
            <p className="text-zinc-600">(none running)</p>
          ) : (
            <ul className="space-y-1">
              {lsp.map((s) => (
                <li key={s.cmd} className="flex items-center gap-2">
                  <span
                    className={
                      s.state === "running"
                        ? "text-emerald-500"
                        : s.state === "failed"
                          ? "text-red-400"
                          : "text-amber-400"
                    }
                  >
                    ●
                  </span>
                  <span className="font-mono">{s.cmd}</span>
                  {s.lang_id && (
                    <span className="text-zinc-500">({s.lang_id})</span>
                  )}
                  {s.root && (
                    <span className="text-zinc-500 truncate" title={s.root}>
                      {s.root}
                    </span>
                  )}
                </li>
              ))}
            </ul>
          )}
        </section>

        {/* Extra allowed paths */}
        <section>
          <h3 className="text-xs uppercase tracking-wide text-zinc-500 mb-2">
            Extra allowed paths ({extraPaths.length})
          </h3>
          {extraPaths.length === 0 ? (
            <p className="text-zinc-600">(none)</p>
          ) : (
            <ul className="space-y-1 font-mono text-xs">
              {extraPaths.map((p) => (
                <li key={p} className="truncate" title={p}>
                  {p}
                </li>
              ))}
            </ul>
          )}
        </section>
      </div>
    </div>
  );
}

function Row({ k, v, mono }: { k: string; v: string; mono?: boolean }) {
  return (
    <div className="flex items-baseline gap-3 py-0.5">
      <span className="text-zinc-500 w-24 shrink-0">{k}</span>
      <span className={mono ? "font-mono text-zinc-300 break-all" : "text-zinc-200 break-all"}>
        {v}
      </span>
    </div>
  );
}
