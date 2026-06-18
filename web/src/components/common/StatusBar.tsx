import { useChatState } from "../../stores/chatStore";
import { Button } from "@/components/ui/button";
import { PanelRight } from "lucide-react";

interface Props {
  onCoworkToggle?: () => void;
  onModelClick?: () => void;
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

export default function StatusBar({ onCoworkToggle, onModelClick, onStatusClick }: Props) {
  const {
    model,
    smallModel,
    advisorModel,
    advisorEnabled,
    isStreaming,
    error,
    tuiStatus,
    sessionContext,
    spendingUSD,
  } = useChatState();

  // Pull every field from the consolidated snapshot when present; fall back to
  // the per-field store state for older TUI builds that don't push "status".
  const snap = tuiStatus;
  const mainModel = snap?.main_model || model || "";
  const liveSmallModel = snap?.small_model || smallModel || "";
  const liveAdvisorModel = snap?.advisor_model || advisorModel || "";
  const liveAdvisorEnabled = snap?.advisor_enabled ?? advisorEnabled;
  const liveSmallOn = snap?.small_model_enabled ?? false;
  const ideStatus = snap?.ide_status || "";
  const sessionTitle = snap?.session_title || "";
  const sessionId = snap?.session_id || "";
  const cwd = snap?.cwd || "";
  const ctxCur = sessionContext?.currentTokens ?? snap?.context_current_tokens ?? 0;
  const ctxMax = sessionContext?.maxTokens ?? snap?.context_max_tokens ?? 0;
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
    <div className="flex flex-col border-t border-zinc-700 px-4 py-1.5 text-xs text-zinc-500 gap-0.5">
      {/* Row 1: model, small + on/off, advisor + on/off, IDE, streaming */}
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-3 min-w-0 flex-wrap">
          <button
            type="button"
            onClick={onModelClick}
            className="hover:text-zinc-300 transition-colors truncate max-w-[20rem]"
            title={`${mainModel}${sessionTitle ? ` — ${sessionTitle}` : ""}`}
          >
            {mainModel || "no model"}
          </button>
          {liveSmallModel && (
            <span
              className={
                liveSmallOn ? "text-emerald-500" : "text-zinc-600"
              }
              title="Small model — auto for sub-tasks"
            >
              · small: {liveSmallModel} {liveSmallOn ? "●on" : "○off"}
            </span>
          )}
          {liveAdvisorModel && (
            <span
              className={
                liveAdvisorEnabled ? "text-emerald-500" : "text-zinc-600"
              }
              title="Advisor model — second opinion / sanity check"
            >
              · advisor: {liveAdvisorModel} {liveAdvisorEnabled ? "●on" : "○off"}
            </span>
          )}
          {ideStatus && (
            <span className="text-zinc-500" title="IDE integration">
              · {ideStatus}
            </span>
          )}
          {subagent && (
            <span className="text-zinc-600" title="Active subagent model">
              · subagent: {subagent}
            </span>
          )}
          {isStreaming && (
            <span className="flex items-center gap-1 text-blue-400">
              <span className="inline-block h-2 w-2 animate-pulse rounded-full bg-blue-500" />
              streaming
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
            <span className="text-zinc-400 truncate max-w-[16rem]" title={sessionTitle}>
              {sessionTitle}
            </span>
          )}
          {sessionId && (
            <span className="text-zinc-600" title="Session ID">
              {sessionId}
            </span>
          )}
          {displayCwd && (
            <span className="text-zinc-600 truncate max-w-[18rem]" title={cwd}>
              cwd: {displayCwd}
            </span>
          )}
        </div>
        <div className="flex items-center gap-3">
          {(ctxCur > 0 || ctxMax > 0) && (
            <span
              className="text-zinc-500"
              title={`Context: ${ctxCur} / ${ctxMax} tokens`}
            >
              ctx: {formatTok(ctxCur)}/{formatTok(ctxMax)}
            </span>
          )}
          {spending > 0 && (
            <span
              className="text-zinc-500"
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
              className="text-zinc-500"
              title="Active LSP servers"
            >
              lsp: {lspCount}
            </span>
          )}
          {extraPathsCount > 0 && (
            <span
              className="text-zinc-500"
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
