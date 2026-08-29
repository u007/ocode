import { useChatSelector, getSessionSlice } from "../../stores/chatStore";
import { useProjectState } from "../../stores/projectStore";
import { Button } from "@/components/ui/button";
import { PanelRight } from "lucide-react";
import type { ToolActivityStatus } from "../../api/types";

interface Props {
  onCoworkToggle?: () => void;
  onStatusClick?: () => void;
}

// Format a token count for the context window. Shows "12.3k" / "1.2M" style
// values, similar to the TUI's formatTok helper.
function formatTok(n: number): string {
  if (!isFinite(n) || n < 0) return "0";
  if (n < 1000) return String(n);
  if (n < 1_000_000) return `${(n / 1000).toFixed(n < 10_000 ? 1 : 0)}k`;
  return `${(n / 1_000_000).toFixed(1)}M`;
}

// Format a USD amount for the spend gauge.
function formatUSD(n: number): string {
  if (!isFinite(n) || n < 0) return "$0";
  if (n < 0.01) return "<$0.01";
  if (n < 100) return `$${n.toFixed(2)}`;
  return `$${n.toFixed(0)}`;
}

// Reused across renders: constructing Intl.DateTimeFormat does full ICU locale
// resolution and is too expensive to redo on every render — this component
// re-renders once per streamed token while a tool is active.
const activityTimeFormatter = new Intl.DateTimeFormat([], { hour12: false, timeStyle: "medium" });

// Render one in-flight tool the way the TUI activity row does: `name [HH:MM:SS]`.
// Long tool names are truncated so the status bar stays readable on one row.
function toolActivityLabel(t: ToolActivityStatus): string {
  const name = t.name.length > 24 ? t.name.slice(0, 21) + "…" : t.name;
  if (!t.started_at) return `⚙ ${name}`;
  const started = new Date(t.started_at);
  if (isNaN(started.getTime())) return `⚙ ${name}`;
  return `⚙ ${name} [${activityTimeFormatter.format(started)}]`;
}

// Builds the "current running status" segment shown at the bottom, mirroring the
// TUI's activity row (⟳ LLM · ⚙ tool · @ agent). Prefers the consolidated TUI
// activity snapshot (RC-bridge mode); falls back to the per-session streaming
// state + in-flight live tool for headless/desktop mode where no TUI is attached.
function runningStatusParts(
  isRunning: boolean,
  snap: import("../../api/types").TUIStatus | null,
  liveToolName: string | undefined,
): string[] {
  const parts: string[] = [];
  if (snap?.llm_running) parts.push("⟳ llm");
  for (const agent of snap?.active_agents ?? []) parts.push(`@ ${agent}`);
  for (const t of snap?.active_tools ?? []) parts.push(toolActivityLabel(t));
  if (parts.length > 0) return parts;
  // Authoritative liveness: while a turn is active, always surface at least a
  // base "running" indicator so the row never looks idle during the silent
  // gaps of a turn when no deltas flow and the TUI snapshot reports nothing.
  if (parts.length === 0 && isRunning) parts.push("running…");
  if (liveToolName) parts.push(`⚙ ${liveToolName.length > 24 ? liveToolName.slice(0, 21) + "…" : liveToolName}`);
  return parts;
}

export default function StatusBar({ onCoworkToggle, onStatusClick }: Props) {
  const { activeTabId } = useProjectState();
  const spendingUSD = useChatSelector((s) => s.spendingUSD);
  // Scoped to the active tab's session: other tabs' streamed tokens don't
  // re-render this always-mounted status bar.
  const { isStreaming, error, live, tuiStatus, turnActive } = useChatSelector((s) => getSessionSlice(s, activeTabId));
  const isRunning = isStreaming || turnActive;

  // Pull every field from the consolidated snapshot when present; fall back to
  // the per-field store state for older TUI builds that don't push "status".
  const snap = tuiStatus;
  // The in-flight tool from the per-session live buffer (headless/desktop
  // fallback): the most recent tool part that hasn't produced a result yet.
  const liveToolName = [...live]
    .reverse()
    .find(
      (p): p is Extract<import("../../api/types").LivePart, { kind: "tool" }> =>
        p.kind === "tool" && p.output === undefined,
    )?.tool;
  const runningParts = runningStatusParts(isRunning, snap, liveToolName);
  const ideStatus = snap?.ide_status || "";
  const sessionTitle = snap?.session_title || "";
  const sessionId = snap?.session_id || "";
  const cwd = snap?.cwd || "";
  const ctxCur = snap?.context_current_tokens ?? 0;
  const ctxMax = snap?.context_max_tokens ?? 0;
  const spending = spendingUSD ?? snap?.spending_usd ?? 0;
  const modifiedCount = snap?.modified_files?.length ?? 0;
  const lspCount = snap?.lsp_servers?.length ?? 0;
  const extraPathsCount = snap?.extra_allowed_paths?.length ?? 0;
  const subagent = snap?.subagent_model || "";

  // Compact display of the cwd — strip the user's HOME prefix if present so
  // long paths don't dominate the row. The full path is in the title attribute.
  const homePrefix = (() => {
    if (!cwd) return "";
    const home = "/Users/"; // macOS / Linux home base — the only platforms the web targets.
    const idx = cwd.indexOf(home);
    if (idx < 0) return "";
    const slash = cwd.indexOf("/", home.length);
    if (slash < 0) return "";
    return cwd.substring(0, slash);
  })();
  const displayCwd = homePrefix && cwd.startsWith(homePrefix + "/")
    ? "~/" + cwd.substring(homePrefix.length + 1)
    : cwd;

  return (
    <div className="flex flex-col border-t border-border px-4 py-1.5 text-xs text-muted-foreground gap-0.5">
      {/* Row 1: IDE, subagent, streaming */}
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-3 min-w-0 flex-wrap">
          {ideStatus && (
            <span className="text-muted-foreground" title="IDE integration">
              · {ideStatus}
            </span>
          )}
          {subagent && (
            <span className="text-foreground" title="Active subagent model">
              · subagent: {subagent}
            </span>
          )}
          {runningParts.length > 0 && (
            <span
              className="flex items-center gap-1 text-blue-400"
              title="Agent running status"
            >
              <span className="inline-block h-2 w-2 animate-pulse rounded-full bg-blue-500" />
              <span className="truncate max-w-[28rem]">{runningParts.join("  ·  ")}</span>
            </span>
          )}
        </div>
        {error && <span className="text-red-400">{error}</span>}
        <div className="flex items-center gap-3">
          {onStatusClick && (
            <Button
              type="button"
              variant="ghost"
              size="sm"
              onClick={onStatusClick}
              title="Show full status (modified files, LSP servers, extra paths, spending)"
              className="h-6 px-2 text-xs"
            >
              status
            </Button>
          )}
          {onCoworkToggle && (
            <Button
              type="button"
              variant="ghost"
              size="icon"
              onClick={onCoworkToggle}
              title="Toggle cowork panel"
            >
              <PanelRight className="h-4 w-4" />
            </Button>
          )}
          <span>ocode web</span>
        </div>
      </div>
      {/* Row 2: session, cwd, context, spend, files, lsp, extra paths */}
      <div className="flex items-center justify-between gap-3 min-w-0 flex-wrap">
        <div className="flex items-center gap-3 min-w-0 flex-wrap">
          {sessionTitle && (
            <span className="text-muted-foreground truncate max-w-[16rem]" title={sessionTitle}>
              {sessionTitle}
            </span>
          )}
          {sessionId && (
            <span className="text-foreground" title="Session ID">
              {sessionId}
            </span>
          )}
          {displayCwd && (
            <span className="text-foreground truncate max-w-[18rem]" title={cwd}>
              cwd: {displayCwd}
            </span>
          )}
        </div>
        <div className="flex items-center gap-3">
          {(ctxCur > 0 || ctxMax > 0) && (
            <span
              className="text-muted-foreground"
              title={`Context: ${ctxCur} / ${ctxMax} tokens`}
            >
              ctx: {formatTok(ctxCur)}/{formatTok(ctxMax)}
            </span>
          )}
          {spending > 0 && (
            <span
              className="text-muted-foreground"
              title="USD spent on this session (today)"
            >
              ${formatUSD(spending)}
            </span>
          )}
          {modifiedCount > 0 && (
            <span
              className="text-amber-500"
              title="Files modified in this session"
            >
              {modifiedCount} file{modifiedCount === 1 ? "" : "s"}
            </span>
          )}
          {lspCount > 0 && (
            <span
              className="text-muted-foreground"
              title="Active LSP servers"
            >
              lsp: {lspCount}
            </span>
          )}
          {extraPathsCount > 0 && (
            <span
              className="text-muted-foreground"
              title="Extra paths the user has pre-authorized"
            >
              paths: {extraPathsCount}
            </span>
          )}
        </div>
      </div>
    </div>
  );
}
