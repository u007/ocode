import { useState, useEffect } from "react";
import { api, apiPath, authHeaders } from "../../api/client";
import { useChatState, useChatDispatch, getSessionSlice } from "../../stores/chatStore";
import { useProjectState } from "../../stores/projectStore";
import { eventBus } from "../../lib/eventBus";
import type { AgentInfo, LSPStatus } from "../../api/types";
import PluginsPanel from "./PluginsPanel";
import ReasoningLevelSelector from "./ReasoningLevelSelector";
import {
  Bot,
  FileText,
  ChevronDown,
  ChevronRight,
  Hash,
  Zap,
  Target,
  GitBranch,
  AlertCircle,
  AlertTriangle,
  Puzzle,
  Loader2,
} from "lucide-react";

interface Props {
  isOpen: boolean;
  onClose: () => void;
  activeAgent: string;
  onModelClick?: (tab: "main" | "small" | "advisor" | "permission") => void;
  // When true the sidebar becomes a fixed overlay (right side) with a backdrop
  // instead of pushing the chat column. Used for the mobile layout (≤767px).
  isMobile?: boolean;
}

interface ConfigState {
  model: string;
  // Process-level defaults for fields that also live on the per-session
  // TUIStatus snapshot. A brand-new session tab has no snapshot yet (it's
  // only created once the first message lands), so these preinit the
  // sidebar instead of it showing blank/"off" until then. tuiStatus, once
  // present, always wins — see the `tuiStatus?.x ?? config.x` fallbacks below.
  thinkingBudget?: number;
  permissionModel?: string;
  permissionModelEnabled?: boolean;
  yolo?: boolean;
  recapModel?: string;
  recapModelEnabled?: boolean;
  contextMaxTokens?: number;
}

// Expanded/collapsed state of the sidebar sections. Persisted to localStorage
// (versioned key, same pattern as the session tabs in projectStore) so the
// user's layout survives reloads. Models/Theme/Tools/Paths sections moved to
// the Settings tab (v2 key so removed section keys don't linger in old state).
const SIDEBAR_SECTIONS_KEY = "ocode.ui.sidebar.v2";

const DEFAULT_SECTIONS: Record<string, boolean> = {
  agent: true,
  context: true,
  lsp: false,
  files: false,
  todo: false,
  git: true,
  permissions: true,
};

function loadExpandedSections(): Record<string, boolean> {
  try {
    const raw = window.localStorage.getItem(SIDEBAR_SECTIONS_KEY);
    if (!raw) return DEFAULT_SECTIONS;
    const parsed = JSON.parse(raw);
    if (!parsed || typeof parsed !== "object") return DEFAULT_SECTIONS;
    // Merge so newly added sections inherit their defaults instead of
    // silently collapsing.
    return { ...DEFAULT_SECTIONS, ...parsed };
  } catch (err) {
    console.error("Failed to load sidebar sections:", err);
    return DEFAULT_SECTIONS;
  }
}

export default function CoworkSidebar({
  isOpen,
  onClose,
  activeAgent,
  onModelClick,
  isMobile,
}: Props) {
  const [titleExpanded, setTitleExpanded] = useState(false);
  const [agents, setAgents] = useState<AgentInfo[]>([]);
  const [config, setConfig] = useState<ConfigState>({
    model: "",
  });
  const [expandedSections, setExpandedSections] =
    useState<Record<string, boolean>>(loadExpandedSections);
  const projectState = useProjectState();
  const [gitBranch, setGitBranch] = useState<string>("");
  const [todoItems] = useState<string[]>([]);
  const [yoloLoading, setYoloLoading] = useState(false);
  const [permLoading, setPermLoading] = useState(false);
  // Locally-tracked active agent. The `activeAgent` prop is fixed by the
  // parent, so switching agents is reflected via this optimistic state.
  const [selectedAgent, setSelectedAgent] = useState<string>(activeAgent);
  const [agentBusy, setAgentBusy] = useState(false);
  const [pluginsOpen, setPluginsOpen] = useState(false);
  function truncateTitle(s: string, maxLen: number): string {
    s = s.replace(/\n/g, " ").trim();
    const runes = Array.from(s);
    if (runes.length <= maxLen) return s;
    return runes.slice(0, maxLen - 3).join("") + "...";
  }

  const [titleGenerating, setTitleGenerating] = useState(false);
  const chatState = useChatState();
  const dispatch = useChatDispatch();
  const { model } = chatState;
  const { activeTabId: sessionId } = projectState;
  const activeProject = projectState.state.activeProject ?? null;
  const { tuiStatus, messages } = getSessionSlice(chatState, sessionId);

  // ── Session title display — mirrors TUI's sidebarDisplayTitle() ────────────
  // Priority: explicit session_title → first user prompt (truncated) → "Untitled".
  // Truncation mirrors the TUI/server's maxGeneratedTitleLen (80 chars) and
  // collapses newlines so the header stays single-line (web uses line-clamp-3
  // instead of TUI's wordWrap, but the underlying title string is the same).
  const displayTitle = (() => {
    const rawTitle = tuiStatus?.session_title?.trim() || "";
    if (rawTitle) return truncateTitle(rawTitle, 80);
    // Fallback: first visible user message, excluding slash commands (mirrors
    // TUI's isCommandHistoryMessage / firstUserPromptText). Slash-only sessions
    // intentionally show no title until a real prompt or LLM title exists.
    for (const m of messages) {
      const text = m.content?.trim() || "";
      if (m.role === "user" && text && !text.startsWith("/")) {
        return truncateTitle(text, 80);
      }
    }
    if (messages.length === 0) return "Untitled";
    return "";
  })();

  const canGenerateTitle = !!sessionId && !sessionId.startsWith("new-");

  const handleGenerateTitle = async () => {
    if (!sessionId || titleGenerating) return;
    setTitleGenerating(true);
    try {
      const res = await api.generateSessionTitle(sessionId);
      if (res?.title) {
        const newStatus = { ...(tuiStatus || {}), session_title: res.title } as typeof tuiStatus;
        dispatch({ type: "SET_TUI_STATUS", sessionId, status: newStatus! });
        projectState.dispatch({ type: "UPDATE_TAB_TITLE", id: sessionId, title: res.title, manual: true });
      } else if (sessionId) {
        // Fallback: refetch status so the SSE broadcast path still updates the tab
        const st = await api.getSessionStatus(sessionId).catch(() => null);
        if (st?.session_title) {
          dispatch({ type: "SET_TUI_STATUS", sessionId, status: st as any });
          projectState.dispatch({ type: "UPDATE_TAB_TITLE", id: sessionId, title: st.session_title, manual: true });
        }
      }
    } catch (e) {
      console.error("generate title failed", e);
    } finally {
      setTitleGenerating(false);
    }
  };

  // Git branch comes from `git_status` events on the shared bus (the server's
  // subscriber-aware watcher emits an initial snapshot for viewed projects and
  // on change — no 10s polling). A one-shot fetch seeds the render for the
  // case where the watcher hasn't ticked for this project yet.
  useEffect(() => {
    let cancelled = false;
    const fetchBranch = async () => {
      try {
        // Seed for the active project, not the server workdir — the legacy
        // no-param form reports the workdir's repo.
        const qs = activeProject
          ? `?project=${encodeURIComponent(activeProject.path)}`
          : "";
        const res = await fetch(apiPath(`/api/git/status${qs}`), { headers: authHeaders() });
        const data = await res.json();
        if (!cancelled && data.branch) setGitBranch(data.branch);
      } catch (err) {
        console.error("Failed to fetch git branch:", err);
      }
    };
    fetchBranch();
    const off = eventBus.on("git_status", (env) => {
      // The git section reflects the active project; events for other viewed
      // projects (another tab in a different project) are ignored.
      if (env.project && activeProject && env.project !== activeProject.path) return;
      const branch = (env.data as { branch?: string }).branch;
      if (branch) setGitBranch(branch);
    });
    return () => {
      cancelled = true;
      off();
    };
  }, [activeProject?.path]);

  useEffect(() => {
    api.listAgents().then(setAgents).catch(console.error);

    // Fetch the main model name plus the other process-level defaults that
    // back the pre-session sidebar fallbacks (see ConfigState).
    Promise.all([
      api.getConfigModel().catch(() => null),
      api.getThinkingBudget().catch(() => null),
      api.getPermissionModel().catch(() => null),
      api.getYolo().catch(() => null),
      api.getRecapConfig().catch(() => null),
    ])
      .then(([modelRes, thinkingRes, permRes, yoloRes, recapRes]) => {
        setConfig({
          model: modelRes?.model || "",
          thinkingBudget: thinkingRes?.budget,
          permissionModel: permRes?.model,
          permissionModelEnabled: permRes?.enabled,
          yolo: yoloRes?.yolo,
          recapModel: recapRes?.recap_model,
          recapModelEnabled: recapRes?.recap_model_enabled,
          contextMaxTokens: modelRes?.context_max_tokens,
        });
      })
      .catch(console.error);
  }, []);

  const currentAgent = agents.find((a) => a.name === selectedAgent);

  // Persist the expanded/collapsed section layout so it survives reloads.
  useEffect(() => {
    try {
      window.localStorage.setItem(SIDEBAR_SECTIONS_KEY, JSON.stringify(expandedSections));
    } catch (err) {
      console.error("Failed to persist sidebar sections:", err);
    }
  }, [expandedSections]);

  const toggleSection = (section: string) => {
    setExpandedSections((prev) => ({ ...prev, [section]: !prev[section] }));
  };

  // Switch the active agent for the current session. Optimistically updates the
  // displayed agent; rolls back if the request fails.
  const switchAgent = (name: string) => {
    if (!name || name === selectedAgent) return;
    const prev = selectedAgent;
    setSelectedAgent(name);
    setAgentBusy(true);
    api
      .setAgent(name, sessionId || undefined)
      .catch((err) => {
        console.error("failed to switch agent", err);
        setSelectedAgent(prev);
      })
      .finally(() => setAgentBusy(false));
  };

  // Context usage from the per-session status snapshot (populated by the
  // fetch-on-activation status fetch and patched by bus `status` events).
  const contextCurrent = tuiStatus?.context_current_tokens ?? 0;
  const contextMax = tuiStatus?.context_max_tokens ?? config.contextMaxTokens ?? 0;
  const contextModel = tuiStatus?.context_model || model || config.model;
  const contextPct =
    contextMax > 0
      ? Math.min(100, Math.round((contextCurrent / contextMax) * 100))
      : 0;
  const lspServers: LSPStatus[] = tuiStatus?.lsp_servers ?? [];
  const modifiedFiles = tuiStatus?.modified_files ?? [];

  // On mobile the sidebar is always mounted (so it can slide); when closed it
  // sits off-screen. On desktop it is fully removed when closed so the chat
  // column reclaims the space (push layout).
  if (!isOpen && !isMobile) return null;


  const toggleYolo = async () => {
    const currentMode = tuiStatus?.permission_mode || (config.yolo ? "yolo" : "");
    const enabled = currentMode !== "yolo";
    setYoloLoading(true);
    try {
      const res = await fetch(apiPath("/api/permissions/yolo"), {
        method: "PUT",
        headers: { "Content-Type": "application/json", ...authHeaders() },
        body: JSON.stringify({ enabled }),
      });
      if (!res.ok) {
        const j = await res.json().catch(() => null);
        console.error("toggle yolo failed", j);
      } else if (sessionId) {
        const status = await api.getSessionStatus(sessionId);
        dispatch({ type: "SET_TUI_STATUS", sessionId, status });
      }
    } catch (e) {
      console.error("toggle yolo error", e);
    } finally {
      setYoloLoading(false);
    }
  };

  const togglePermEnabled = async () => {
    const enabled = !(tuiStatus?.permission_auto_allow ?? config.permissionModelEnabled);
    setPermLoading(true);
    try {
      await api.setPermissionModelEnabled(enabled);
      if (sessionId) {
        const status = await api.getSessionStatus(sessionId);
        dispatch({ type: "SET_TUI_STATUS", sessionId, status });
      }
    } catch (e) {
      console.error("toggle perm enabled error", e);
    } finally {
      setPermLoading(false);
    }
  };

  const content = (
    <>
      {/* Header */}
      <div className="flex items-center justify-between px-4 py-3 border-b border-zinc-700">
        <h2 className="text-sm font-semibold text-zinc-300">Cowork</h2>
        <button
          onClick={onClose}
          className="text-zinc-500 hover:text-zinc-300 text-xs"
        >
          ✕
        </button>
      </div>

      {/* Session title — mirrors TUI sidebar header (◆ title + ✦ gen) */}
      <div className="border-b border-zinc-700 px-4 py-3">
        <div className="flex items-start gap-2">
          <span className="text-[#7DCFFF] font-bold text-sm leading-5 select-none" aria-hidden>◆</span>
          <span
            className={`flex-1 text-sm font-medium text-zinc-200 break-words leading-5 cursor-pointer ${
              titleExpanded ? "" : "line-clamp-3"
            }`}
            title={titleExpanded ? "Click to collapse" : `${displayTitle}\n\n(click to expand)`}
            onClick={() => setTitleExpanded((v) => !v)}
          >
            {displayTitle}
          </span>
          <button
            type="button"
            onClick={handleGenerateTitle}
            disabled={!canGenerateTitle || titleGenerating}
            title={canGenerateTitle ? "Regenerate title from latest task (✦ gen)" : "No session to title"}
            className="flex-shrink-0 inline-flex items-center gap-1 text-xs font-medium text-[#7DCFFF] hover:text-[#9de2ff] hover:underline disabled:opacity-40 disabled:cursor-not-allowed disabled:no-underline px-1 py-0.5 rounded"
          >
            {titleGenerating ? (
              <>
                <Loader2 className="w-3 h-3 animate-spin" />
                <span>gen</span>
              </>
            ) : (
              "✦ gen"
            )}
          </button>
        </div>
      </div>

      <div className="flex-1 overflow-y-auto">
        {/* Agent Section */}
        <div className="border-b border-zinc-700">
          <button
            onClick={() => toggleSection("agent")}
            className="flex items-center gap-2 w-full px-4 py-2.5 text-sm font-medium text-zinc-300 hover:bg-zinc-800"
          >
            {expandedSections.agent ? (
              <ChevronDown className="w-4 h-4" />
            ) : (
              <ChevronRight className="w-4 h-4" />
            )}
            <Bot className="w-4 h-4 text-blue-400" />
            Agent
          </button>
          {expandedSections.agent && (
            <div className="px-4 pb-3">
              <div className="rounded-md bg-zinc-800 p-3">
                <div className="text-sm font-medium text-zinc-200">
                  {currentAgent?.name || selectedAgent}
                </div>
                <div className="text-xs text-zinc-500 mt-1">
                  {currentAgent?.description || "No description"}
                </div>
              </div>
              {agents.length > 0 && (
                <select
                  className="mt-2 w-full h-8 px-2 text-xs bg-zinc-800 border border-zinc-700 rounded text-zinc-200 disabled:opacity-50"
                  value={selectedAgent}
                  disabled={agentBusy}
                  onChange={(e) => switchAgent(e.target.value)}
                  title="Switch the active agent for this session"
                >
                  {/* Ensure the current agent is selectable even if not in the
                      fetched list (e.g. the hardcoded default). */}
                  {!agents.some((a) => a.name === selectedAgent) && (
                    <option value={selectedAgent}>{selectedAgent}</option>
                  )}
                  {agents.map((a) => (
                    <option key={a.name} value={a.name}>
                      {a.name}
                    </option>
                  ))}
                </select>
              )}
              <div className="mt-2 text-xs text-zinc-500">
                <div>Session: {sessionId ? sessionId.slice(0, 12) + "..." : "None"}</div>
              </div>
            </div>
          )}
        </div>

        {/* Compact main-model trigger — Small/Advisor/OCR/Permissions moved to
            the Settings tab (see docs/superpowers/specs/2026-08-11-configuration-ui-design.md).
            This is the only remaining sidebar entry point for ModelDialog,
            which stays out of scope for this feature. */}
        <div className="border-b border-zinc-700 px-4 py-2.5">
          <button
            type="button"
            onClick={() => onModelClick?.("main")}
            className="w-full rounded px-1 py-1 text-left text-xs transition-colors hover:bg-zinc-800 disabled:cursor-default disabled:hover:bg-transparent"
            disabled={!onModelClick}
          >
            <div className="text-zinc-500 mb-1">Model</div>
            <div className="text-zinc-300 font-mono truncate">
              {model || config.model || "Not set"}
            </div>
          </button>
          {/* Reasoning level selector */}
          <ReasoningLevelSelector
            thinkingBudget={tuiStatus?.thinking_budget ?? config.thinkingBudget}
            disabled={!onModelClick}
          />
        </div>

        {/* Git Section */}
        <div className="border-b border-zinc-700">
          <button
            onClick={() => toggleSection("git")}
            className="flex items-center gap-2 w-full px-4 py-2.5 text-sm font-medium text-zinc-300 hover:bg-zinc-800"
          >
            {expandedSections.git ? (
              <ChevronDown className="w-4 h-4" />
            ) : (
              <ChevronRight className="w-4 h-4" />
            )}
            <GitBranch className="w-4 h-4 text-cyan-400" />
            Git
          </button>
          {expandedSections.git && (
            <div className="px-4 pb-3">
              <div className="text-sm font-mono text-zinc-300">
                {gitBranch || "Loading..."}
              </div>
              {tuiStatus?.cwd && (
                <div className="text-xs text-zinc-500 mt-1 truncate" title={tuiStatus.cwd}>
                  {tuiStatus.cwd}
                </div>
              )}
            </div>
          )}
        </div>

        {/* Permissions — mirrors TUI sidebar Allowed section + perm toggle */}
        <div className="border-b border-zinc-700">
          <button
            onClick={() => toggleSection("permissions")}
            className="flex items-center gap-2 w-full px-4 py-2.5 text-sm font-medium text-zinc-300 hover:bg-zinc-800"
          >
            {expandedSections.permissions ? (
              <ChevronDown className="w-4 h-4" />
            ) : (
              <ChevronRight className="w-4 h-4" />
            )}
            <Target className="w-4 h-4 text-purple-400" />
            Permissions
          </button>
          {expandedSections.permissions && (
            <div className="px-4 pb-3 space-y-2">
              <div className="flex items-center justify-between">
                <span className="text-xs text-zinc-400">Mode</span>
                <span className="text-xs font-mono px-2 py-0.5 rounded bg-zinc-800 text-zinc-200">
                  {tuiStatus?.mode || "—"}
                </span>
              </div>
              {tuiStatus?.temperature !== undefined && tuiStatus?.temperature !== null && (
                <div className="flex items-center justify-between">
                  <span className="text-xs text-zinc-400">Temperature</span>
                  <span className="text-xs font-mono text-zinc-300">{tuiStatus.temperature}</span>
                </div>
              )}
              <div className="flex items-center justify-between">
                <span className="text-xs text-zinc-400">Permission</span>
                <span className="text-xs font-mono text-zinc-300">
                  {tuiStatus?.permission_mode || (config.yolo ? "yolo" : "normal")}
                </span>
              </div>
              <label className="flex items-center justify-between cursor-pointer">
                <span className="text-xs text-zinc-400">YOLO (auto-allow all)</span>
                <input
                  type="checkbox"
                  checked={(tuiStatus?.permission_mode ?? (config.yolo ? "yolo" : "normal")) === "yolo"}
                  disabled={yoloLoading}
                  onChange={toggleYolo}
                  className="w-8 h-4 rounded-full appearance-none bg-zinc-700 checked:bg-purple-600 relative before:content-[''] before:absolute before:w-3 before:h-3 before:bg-white before:rounded-full before:top-0.5 before:left-0.5 checked:before:translate-x-4 before:transition-all disabled:opacity-50"
                />
              </label>
              <label className="flex items-center justify-between cursor-pointer">
                <span className="text-xs text-zinc-400">Auto-permission</span>
                <input
                  type="checkbox"
                  checked={Boolean(tuiStatus?.permission_auto_allow ?? config.permissionModelEnabled)}
                  disabled={permLoading}
                  onChange={togglePermEnabled}
                  className="w-8 h-4 rounded-full appearance-none bg-zinc-700 checked:bg-emerald-600 relative before:content-[''] before:absolute before:w-3 before:h-3 before:bg-white before:rounded-full before:top-0.5 before:left-0.5 checked:before:translate-x-4 before:transition-all disabled:opacity-50"
                />
              </label>
              <button
                type="button"
                onClick={() => onModelClick?.("permission")}
                disabled={!onModelClick}
                className="flex items-center justify-between w-full text-left rounded px-1 py-1 hover:bg-zinc-800 disabled:cursor-default disabled:hover:bg-transparent"
              >
                <span className="text-xs text-zinc-400">Permission model</span>
                <span
                  className="text-xs font-mono text-zinc-300 truncate max-w-[140px]"
                  title={tuiStatus?.permission_model || config.permissionModel || "(not set)"}
                >
                  {tuiStatus?.permission_model || config.permissionModel || "(not set)"}{" "}
                  {(tuiStatus?.permission_auto_allow ?? config.permissionModelEnabled) ? "●" : "○"}
                </span>
              </button>
              {tuiStatus?.ide_mode && (
                <div className="flex items-center justify-between">
                  <span className="text-xs text-zinc-400">IDE</span>
                  <span className="text-xs text-zinc-300 truncate" title={tuiStatus.ide_status || tuiStatus.ide_mode}>
                    {tuiStatus.ide_status || tuiStatus.ide_mode}
                  </span>
                </div>
              )}
              {(tuiStatus?.recap_model || config.recapModel) && (
                <div className="flex items-center justify-between">
                  <span className="text-xs text-zinc-400">Recap</span>
                  <span
                    className="text-xs font-mono text-zinc-300 truncate"
                    title={tuiStatus?.recap_model || config.recapModel}
                  >
                    {tuiStatus?.recap_model || config.recapModel}{" "}
                    {(tuiStatus?.recap_model_enabled ?? config.recapModelEnabled) ? "●" : "○"}
                  </span>
                </div>
              )}
              {tuiStatus?.spending_usd !== undefined && tuiStatus.spending_usd > 0 && (
                <div className="flex items-center justify-between">
                  <span className="text-xs text-zinc-400">Spending</span>
                  <span className="text-xs font-mono text-amber-300">${tuiStatus.spending_usd.toFixed(4)}</span>
                </div>
              )}
            </div>
          )}
        </div>

        {/* Context Section — real token usage from the TUI status snapshot. */}
        <div className="border-b border-zinc-700">
          <button
            onClick={() => toggleSection("context")}
            className="flex items-center gap-2 w-full px-4 py-2.5 text-sm font-medium text-zinc-300 hover:bg-zinc-800"
          >
            {expandedSections.context ? (
              <ChevronDown className="w-4 h-4" />
            ) : (
              <ChevronRight className="w-4 h-4" />
            )}
            <Hash className="w-4 h-4 text-cyan-400" />
            Context
          </button>
          {expandedSections.context && (
            <div className="px-4 pb-3">
              {contextMax > 0 ? (
                <>
                  <div className="flex items-center justify-between text-xs text-zinc-500 mb-1">
                    <span>Used</span>
                    <span className="font-mono text-zinc-400">
                      {formatTokenCount(contextCurrent)} / {formatTokenCount(contextMax)}
                    </span>
                  </div>
                  <div className="h-2 w-full rounded bg-zinc-800 overflow-hidden">
                    <div
                      className={`h-full transition-all ${
                        contextPct > 85
                          ? "bg-red-500"
                          : contextPct > 65
                            ? "bg-yellow-500"
                            : "bg-emerald-500"
                      }`}
                      style={{ width: `${contextPct}%` }}
                    />
                  </div>
                  <div className="text-right text-[11px] text-zinc-500 mt-1">
                    {contextPct}%
                    {contextModel && (
                      <span className="ml-2 text-zinc-600 font-mono">
                        {contextModel}
                      </span>
                    )}
                  </div>
                </>
              ) : (
                <div className="text-xs text-zinc-500">
                  No context data yet
                </div>
              )}
            </div>
          )}
        </div>

        {/* LSP Statuses Section */}
        <div className="border-b border-zinc-700">
          <button
            onClick={() => toggleSection("lsp")}
            className="flex items-center gap-2 w-full px-4 py-2.5 text-sm font-medium text-zinc-300 hover:bg-zinc-800"
          >
            {expandedSections.lsp ? (
              <ChevronDown className="w-4 h-4" />
            ) : (
              <ChevronRight className="w-4 h-4" />
            )}
            <Zap className="w-4 h-4 text-amber-400" />
            LSP
          </button>
          {expandedSections.lsp && (
            <div className="px-4 pb-3">
              {lspServers.length > 0 ? (
                <div className="space-y-1.5">
                  {lspServers.map((s) => (
                    <div key={s.cmd} className="rounded bg-zinc-800 p-2">
                      <div className="flex items-center justify-between gap-2">
                        <span className="text-xs font-mono text-zinc-300 truncate">
                          {s.cmd}
                        </span>
                        <span
                          className={`text-[10px] flex-shrink-0 ${
                            s.state === "running"
                              ? "text-emerald-400"
                              : s.state === "failed"
                                ? "text-red-400"
                                : "text-yellow-400"
                          }`}
                        >
                          {s.state}
                        </span>
                      </div>
                      {s.lang_id && (
                        <div className="text-[11px] text-zinc-500 truncate">
                          {s.lang_id}
                          {s.root ? ` · ${s.root}` : ""}
                        </div>
                      )}
                      {(s.diagnostics_errors ?? 0) > 0 ||
                      (s.diagnostics_warnings ?? 0) > 0 ? (
                        <div className="flex gap-3 mt-1 text-[11px]">
                          {(s.diagnostics_errors ?? 0) > 0 && (
                            <span className="flex items-center gap-1 text-red-400">
                              <AlertCircle className="w-3 h-3" />
                              {s.diagnostics_errors}
                            </span>
                          )}
                          {(s.diagnostics_warnings ?? 0) > 0 && (
                            <span className="flex items-center gap-1 text-yellow-400">
                              <AlertTriangle className="w-3 h-3" />
                              {s.diagnostics_warnings}
                            </span>
                          )}
                        </div>
                      ) : null}
                    </div>
                  ))}
                </div>
              ) : (
                <div className="text-xs text-zinc-500">No LSP servers</div>
              )}
            </div>
          )}
        </div>

        {/* Modified Files Section */}
        <div className="border-b border-zinc-700">
          <button
            onClick={() => toggleSection("files")}
            className="flex items-center gap-2 w-full px-4 py-2.5 text-sm font-medium text-zinc-300 hover:bg-zinc-800"
          >
            {expandedSections.files ? (
              <ChevronDown className="w-4 h-4" />
            ) : (
              <ChevronRight className="w-4 h-4" />
            )}
            <FileText className="w-4 h-4 text-yellow-400" />
            Modified Files
          </button>
          {expandedSections.files && (
            <div className="px-4 pb-3">
              {modifiedFiles.length > 0 ? (
                <div className="space-y-1">
                  {modifiedFiles.map((f) => (
                    <div
                      key={f.path}
                      className="flex items-center gap-2 text-xs text-zinc-400 p-1.5 rounded hover:bg-zinc-800"
                    >
                      <span
                        className={`flex-shrink-0 w-4 text-center font-mono ${
                          f.status === "M"
                            ? "text-yellow-400"
                            : f.status === "A"
                              ? "text-emerald-400"
                              : f.status === "D"
                                ? "text-red-400"
                                : "text-zinc-500"
                        }`}
                      >
                        {f.status || "?"}
                      </span>
                      <span className="truncate font-mono" title={f.path}>
                        {f.path}
                      </span>
                    </div>
                  ))}
                </div>
              ) : (
                <div className="text-xs text-zinc-500">No modified files</div>
              )}
            </div>
          )}
        </div>

        {/* Plugins Section — opens the full plugin manager dialog. */}
        <div className="border-b border-zinc-700">
          <button
            onClick={() => setPluginsOpen(true)}
            className="flex items-center gap-2 w-full px-4 py-2.5 text-sm font-medium text-zinc-300 hover:bg-zinc-800"
          >
            <ChevronRight className="w-4 h-4" />
            <Puzzle className="w-4 h-4 text-fuchsia-400" />
            Plugins
          </button>
        </div>

        {/* TODO Section — no live data source is exposed by the backend yet,
            so this shows a stable empty state instead of a list that can never
            update. */}
        <div className="border-b border-zinc-700">
          <button
            onClick={() => toggleSection("todo")}
            className="flex items-center gap-2 w-full px-4 py-2.5 text-sm font-medium text-zinc-300 hover:bg-zinc-800"
          >
            {expandedSections.todo ? (
              <ChevronDown className="w-4 h-4" />
            ) : (
              <ChevronRight className="w-4 h-4" />
            )}
            <Target className="w-4 h-4 text-orange-400" />
            TODO
          </button>
          {expandedSections.todo && (
            <div className="px-4 pb-3">
              {todoItems.length > 0 ? (
                <div className="space-y-1">
                  {todoItems.map((item, i) => (
                    <div
                      key={i}
                      className="flex items-start gap-2 text-xs text-zinc-400 p-1.5 rounded hover:bg-zinc-800"
                    >
                      <Zap className="w-3 h-3 mt-0.5 flex-shrink-0 text-orange-400" />
                      <span>{item}</span>
                    </div>
                  ))}
                </div>
              ) : (
                <div className="text-xs text-zinc-500">
                  <div>No TODO items</div>
                  <div className="mt-2 text-zinc-600">
                    Agent will add items during execution
                  </div>
                </div>
              )}
            </div>
          )}
        </div>
      </div>

      <PluginsPanel open={pluginsOpen} onOpenChange={setPluginsOpen} />
    </>
  );

  if (isMobile) {
    return (
      <>
        {/* Backdrop to dismiss the overlay on mobile. */}
        {isOpen && (
          <div
            className="fixed inset-0 z-40 bg-black/50"
            onClick={onClose}
            aria-hidden="true"
          />
        )}
        <aside
          className={`fixed inset-y-0 right-0 z-50 w-72 border-l border-zinc-700 bg-zinc-900 flex flex-col overflow-hidden transition-transform duration-200 ${
            isOpen ? "translate-x-0" : "translate-x-full"
          }`}
        >
          {content}
        </aside>
      </>
    );
  }

  return (
    <aside className="w-72 flex-shrink-0 border-l border-zinc-700 bg-zinc-900 flex flex-col overflow-hidden">
      {content}
    </aside>
  );
}

function formatTokenCount(n: number): string {
  if (n <= 0) return "0";
  if (n < 1000) return String(n);
  if (n < 1_000_000) return `${(n / 1000).toFixed(1)}k`;
  return `${(n / 1_000_000).toFixed(1)}M`;
}
