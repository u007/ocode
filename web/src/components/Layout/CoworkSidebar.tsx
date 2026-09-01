import { useState, useEffect } from "react";
import { api, apiPath, authHeaders } from "../../api/client";
import { useChatSelector, useChatDispatch, getSessionSlice } from "../../stores/chatStore";
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
  advisorModel?: string;
  advisorEnabled?: boolean;
  smallModel?: string;
  smallModelEnabled?: boolean;
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
  const [permLoading, setPermLoading] = useState(false);
  const [advisorLoading, setAdvisorLoading] = useState(false);
  const [smallLoading, setSmallLoading] = useState(false);
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
  const dispatch = useChatDispatch();
  const globalAdvisorModel = useChatSelector((s) => s.advisorModel);
  const globalAdvisorEnabled = useChatSelector((s) => s.advisorEnabled);
  const globalSmallModel = useChatSelector((s) => s.smallModel);
  const globalSmallModelEnabled = useChatSelector((s) => s.smallModelEnabled);
  const { activeTabId: sessionId } = projectState;
  const activeProject = projectState.state.activeProject ?? null;
  // `sessionModel` is the per-session slice field — a draft tab's locally-picked
  // model before the session exists server-side (see SessionSlice.model). The
  // authoritative model for a real session is `tuiStatus.main_model`, which is
  // that tab's own status snapshot (per-session since the multiproject event
  // work), so switching tabs shows each session's own model, not one global.
  const { tuiStatus, messages, model: sessionModel } = useChatSelector((s) =>
    getSessionSlice(s, sessionId),
  );
  const effectiveModel = tuiStatus?.main_model || sessionModel || "";

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
      api.getAdvisor().catch(() => null),
      api.getAdvisorEnabled().catch(() => null),
      api.getSmallModelWithEnabled().catch(() => null),
    ])
      .then(([modelRes, thinkingRes, permRes, yoloRes, recapRes, advisorRes, advisorEnabledRes, smallRes]) => {
        setConfig({
          model: modelRes?.model || "",
          thinkingBudget: thinkingRes?.budget,
          permissionModel: permRes?.model,
          permissionModelEnabled: permRes?.enabled,
          yolo: yoloRes?.yolo,
          recapModel: recapRes?.recap_model,
          recapModelEnabled: recapRes?.recap_model_enabled,
          contextMaxTokens: modelRes?.context_max_tokens,
          advisorModel: advisorRes?.model || "",
          advisorEnabled: advisorEnabledRes?.enabled,
          smallModel: smallRes?.model || "",
          smallModelEnabled: smallRes?.enabled,
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
  const contextModel = tuiStatus?.context_model || effectiveModel || config.model;
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

  // Current live permission mode, defaulting to the config-style yolo hint.
  const currentMode = tuiStatus?.permission_mode || (config.yolo ? "yolo" : "normal");
  const [modeLoading, setModeLoading] = useState(false);

  // Cycle permission mode: normal → yolo → locked → sandbox → normal, via the
  // dedicated mode endpoint (session-scoped, never persisted).
  const cycleMode = async () => {
    const order = ["normal", "yolo", "locked", "sandbox"];
    const idx = order.indexOf(currentMode);
    const next = order[(idx + 1) % order.length];
    setModeLoading(true);
    try {
      const res = await fetch(apiPath("/api/permissions/mode"), {
        method: "PUT",
        headers: { "Content-Type": "application/json", ...authHeaders() },
        body: JSON.stringify({ mode: next }),
      });
      if (!res.ok) {
        const j = await res.json().catch(() => null);
        console.error("set permission mode failed", j);
      } else if (sessionId) {
        const status = await api.getSessionStatus(sessionId);
        dispatch({ type: "SET_TUI_STATUS", sessionId, status });
      }
    } catch (e) {
      console.error("set permission mode error", e);
    } finally {
      setModeLoading(false);
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

  const toggleAdvisor = async () => {
    const current = tuiStatus?.advisor_enabled ?? config.advisorEnabled ?? globalAdvisorEnabled;
    const next = !current;
    setAdvisorLoading(true);
    dispatch({ type: "SET_ADVISOR_ENABLED", enabled: next });
    try {
      await api.setAdvisorEnabled(next);
      if (sessionId) {
        const status = await api.getSessionStatus(sessionId);
        dispatch({ type: "SET_TUI_STATUS", sessionId, status });
      } else {
        setConfig((prev) => ({ ...prev, advisorEnabled: next }));
      }
    } catch (e) {
      console.error("toggle advisor error", e);
      dispatch({ type: "SET_ADVISOR_ENABLED", enabled: current ?? true });
    } finally {
      setAdvisorLoading(false);
    }
  };

  const toggleSmall = async () => {
    const current = tuiStatus?.small_model_enabled ?? config.smallModelEnabled ?? globalSmallModelEnabled;
    const next = !current;
    setSmallLoading(true);
    dispatch({ type: "SET_SMALL_MODEL_ENABLED", enabled: next });
    try {
      await api.setSmallModelEnabled(next);
      if (sessionId) {
        const status = await api.getSessionStatus(sessionId);
        dispatch({ type: "SET_TUI_STATUS", sessionId, status });
      } else {
        setConfig((prev) => ({ ...prev, smallModelEnabled: next }));
      }
    } catch (e) {
      console.error("toggle small model error", e);
      dispatch({ type: "SET_SMALL_MODEL_ENABLED", enabled: current ?? false });
    } finally {
      setSmallLoading(false);
    }
  };

  const content = (
    <>
      {/* Header */}
      <div className="flex items-center justify-between px-4 py-3 border-b border-border">
        <h2 className="text-sm font-semibold text-foreground">Cowork</h2>
        <button
          onClick={onClose}
          className="text-muted-foreground hover:text-foreground text-xs"
        >
          ✕
        </button>
      </div>

      {/* Session title — mirrors TUI sidebar header (◆ title + ✦ gen) */}
      <div className="border-b border-border px-4 py-3">
        <div className="flex items-start gap-2">
          <span className="text-[#7DCFFF] font-bold text-sm leading-5 select-none" aria-hidden>◆</span>
          <span
            className={`flex-1 text-sm font-medium text-foreground break-words leading-5 cursor-pointer ${
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
        <div className="border-b border-border">
          <button
            onClick={() => toggleSection("agent")}
            className="flex items-center gap-2 w-full px-4 py-2.5 text-sm font-medium text-foreground hover:bg-muted"
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
              <div className="rounded-md bg-muted p-3">
                <div className="text-sm font-medium text-foreground">
                  {currentAgent?.name || selectedAgent}
                </div>
                <div className="text-xs text-muted-foreground mt-1">
                  {currentAgent?.description || "No description"}
                </div>
              </div>
              {agents.length > 0 && (
                <select
                  className="mt-2 w-full h-8 px-2 text-xs bg-muted border border-border rounded text-foreground disabled:opacity-50"
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
              <div className="mt-2 text-xs text-muted-foreground">
                <div>Session: {sessionId ? sessionId.slice(0, 12) + "..." : "None"}</div>
              </div>
            </div>
          )}
        </div>

        {/* Model configuration — mirrors TUI's pinned topLines (advisor/small/perm/recap)
            so web/desktop sidebars expose the same selection + toggle controls. */}
        <div className="border-b border-border px-4 py-2.5 space-y-2">
          <button
            type="button"
            onClick={() => onModelClick?.("main")}
            className="w-full rounded px-1 py-1 text-left text-xs transition-colors hover:bg-muted disabled:cursor-default disabled:hover:bg-transparent"
            disabled={!onModelClick}
          >
            <div className="text-muted-foreground mb-1">Model</div>
            <div className="text-foreground font-mono truncate" title={effectiveModel || undefined}>
              {effectiveModel || config.model || "Not set"}
            </div>
          </button>
          {/* Advisor — model picker + on/off toggle (mirrors TUI's advisor: ●on/○off <model> row) */}
          <button
            type="button"
            onClick={() => onModelClick?.("advisor")}
            className="w-full rounded px-1 py-1 text-left text-xs transition-colors hover:bg-muted disabled:cursor-default disabled:hover:bg-transparent"
            disabled={!onModelClick}
            title="Pick the advisor model"
          >
            <div className="flex items-center justify-between gap-2">
              <span className="text-muted-foreground">Advisor</span>
              <span className={`font-mono text-[11px] ${(tuiStatus?.advisor_enabled ?? config.advisorEnabled ?? globalAdvisorEnabled) ? "text-emerald-400" : "text-muted-foreground"}`}>
                {(tuiStatus?.advisor_enabled ?? config.advisorEnabled ?? globalAdvisorEnabled) ? "●on" : "○off"}
              </span>
            </div>
            <div className="text-foreground font-mono truncate">
              {tuiStatus?.advisor_model || config.advisorModel || globalAdvisorModel || "(default)"}
            </div>
          </button>
          <label className="flex items-center justify-between cursor-pointer rounded px-1 py-1 hover:bg-muted">
            <span className="text-xs text-muted-foreground">Advisor enabled</span>
            <input
              type="checkbox"
              checked={Boolean(tuiStatus?.advisor_enabled ?? config.advisorEnabled ?? globalAdvisorEnabled)}
              disabled={advisorLoading}
              onChange={toggleAdvisor}
              className="w-8 h-4 rounded-full appearance-none bg-accent checked:bg-emerald-600 relative before:content-[''] before:absolute before:w-3 before:h-3 before:bg-white before:rounded-full before:top-0.5 before:left-0.5 checked:before:translate-x-4 before:transition-all disabled:opacity-50"
            />
          </label>
          {/* Small model — model picker + on/off toggle (mirrors TUI's small: ●on/○off <model> row) */}
          <button
            type="button"
            onClick={() => onModelClick?.("small")}
            className="w-full rounded px-1 py-1 text-left text-xs transition-colors hover:bg-muted disabled:cursor-default disabled:hover:bg-transparent"
            disabled={!onModelClick}
            title="Pick the small model"
          >
            <div className="flex items-center justify-between gap-2">
              <span className="text-muted-foreground">Small</span>
              <span className={`font-mono text-[11px] ${(tuiStatus?.small_model_enabled ?? config.smallModelEnabled ?? globalSmallModelEnabled) ? "text-emerald-400" : "text-muted-foreground"}`}>
                {(tuiStatus?.small_model_enabled ?? config.smallModelEnabled ?? globalSmallModelEnabled) ? "●on" : "○off"}
              </span>
            </div>
            <div className="text-foreground font-mono truncate">
              {tuiStatus?.small_model || config.smallModel || globalSmallModel || "(none)"}
            </div>
          </button>
          <label className="flex items-center justify-between cursor-pointer rounded px-1 py-1 hover:bg-muted">
            <span className="text-xs text-muted-foreground">Small model enabled</span>
            <input
              type="checkbox"
              checked={Boolean(tuiStatus?.small_model_enabled ?? config.smallModelEnabled ?? globalSmallModelEnabled)}
              disabled={smallLoading}
              onChange={toggleSmall}
              className="w-8 h-4 rounded-full appearance-none bg-accent checked:bg-emerald-600 relative before:content-[''] before:absolute before:w-3 before:h-3 before:bg-white before:rounded-full before:top-0.5 before:left-0.5 checked:before:translate-x-4 before:transition-all disabled:opacity-50"
            />
          </label>

          {/* Reasoning level selector */}
          <ReasoningLevelSelector
            thinkingBudget={tuiStatus?.thinking_budget ?? config.thinkingBudget}
            disabled={!onModelClick}
          />
        </div>

        {/* Git Section */}
        <div className="border-b border-border">
          <button
            onClick={() => toggleSection("git")}
            className="flex items-center gap-2 w-full px-4 py-2.5 text-sm font-medium text-foreground hover:bg-muted"
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
              <div className="text-sm font-mono text-foreground">
                {gitBranch || "Loading..."}
              </div>
              {tuiStatus?.cwd && (
                <div className="text-xs text-muted-foreground mt-1 truncate" title={tuiStatus.cwd}>
                  {tuiStatus.cwd}
                </div>
              )}
            </div>
          )}
        </div>

        {/* Permissions — mirrors TUI sidebar Allowed section + perm toggle */}
        <div className="border-b border-border">
          <button
            onClick={() => toggleSection("permissions")}
            className="flex items-center gap-2 w-full px-4 py-2.5 text-sm font-medium text-foreground hover:bg-muted"
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
                <span className="text-xs text-muted-foreground">Mode</span>
                <span className="text-xs font-mono px-2 py-0.5 rounded bg-muted text-foreground">
                  {tuiStatus?.mode || "—"}
                </span>
              </div>
              {tuiStatus?.temperature !== undefined && tuiStatus?.temperature !== null && (
                <div className="flex items-center justify-between">
                  <span className="text-xs text-muted-foreground">Temperature</span>
                  <span className="text-xs font-mono text-foreground">{tuiStatus.temperature}</span>
                </div>
              )}
              <div className="flex items-center justify-between">
                <span className="text-xs text-muted-foreground">Permission</span>
                <button
                  type="button"
                  onClick={cycleMode}
                  disabled={modeLoading}
                  title="Cycle permission mode: normal → yolo → locked → sandbox → normal"
                  className="text-xs font-mono text-foreground uppercase px-2 py-0.5 rounded border border-border hover:bg-muted disabled:opacity-50"
                >
                  {currentMode}
                  {currentMode === "sandbox" &&
                    tuiStatus?.permission_effective_behavior &&
                    ` · ${tuiStatus.permission_effective_behavior}`}
                </button>
              </div>
              {tuiStatus?.permission_sandbox_supported === false && currentMode === "sandbox" && (
                <p className="text-[11px] text-amber-600">
                  Sandbox not supported on this OS — behaves like normal.
                </p>
              )}
              <label className="flex items-center justify-between cursor-pointer">
                <span className="text-xs text-muted-foreground">Auto-permission</span>
                <input
                  type="checkbox"
                  checked={Boolean(tuiStatus?.permission_auto_allow ?? config.permissionModelEnabled)}
                  disabled={permLoading}
                  onChange={togglePermEnabled}
                  className="w-8 h-4 rounded-full appearance-none bg-accent checked:bg-emerald-600 relative before:content-[''] before:absolute before:w-3 before:h-3 before:bg-white before:rounded-full before:top-0.5 before:left-0.5 checked:before:translate-x-4 before:transition-all disabled:opacity-50"
                />
              </label>
              <button
                type="button"
                onClick={() => onModelClick?.("permission")}
                disabled={!onModelClick}
                className="flex items-center justify-between w-full text-left rounded px-1 py-1 hover:bg-muted disabled:cursor-default disabled:hover:bg-transparent"
              >
                <span className="text-xs text-muted-foreground">Permission model</span>
                <span
                  className="text-xs font-mono text-foreground truncate max-w-[140px]"
                  title={tuiStatus?.permission_model || config.permissionModel || "(not set)"}
                >
                  {tuiStatus?.permission_model || config.permissionModel || "(not set)"}{" "}
                  {(tuiStatus?.permission_auto_allow ?? config.permissionModelEnabled) ? "●" : "○"}
                </span>
              </button>
              {tuiStatus?.ide_mode && (
                <div className="flex items-center justify-between">
                  <span className="text-xs text-muted-foreground">IDE</span>
                  <span className="text-xs text-foreground truncate" title={tuiStatus.ide_status || tuiStatus.ide_mode}>
                    {tuiStatus.ide_status || tuiStatus.ide_mode}
                  </span>
                </div>
              )}
              {(tuiStatus?.recap_model || config.recapModel) && (
                <div className="flex items-center justify-between">
                  <span className="text-xs text-muted-foreground">Recap</span>
                  <span
                    className="text-xs font-mono text-foreground truncate"
                    title={tuiStatus?.recap_model || config.recapModel}
                  >
                    {tuiStatus?.recap_model || config.recapModel}{" "}
                    {(tuiStatus?.recap_model_enabled ?? config.recapModelEnabled) ? "●" : "○"}
                  </span>
                </div>
              )}
              {tuiStatus?.spending_usd !== undefined && tuiStatus.spending_usd > 0 && (
                <div className="flex items-center justify-between">
                  <span className="text-xs text-muted-foreground">Spending</span>
                  <span className="text-xs font-mono text-amber-300">${tuiStatus.spending_usd.toFixed(4)}</span>
                </div>
              )}
            </div>
          )}
        </div>

        {/* Context Section — real token usage from the TUI status snapshot. */}
        <div className="border-b border-border">
          <button
            onClick={() => toggleSection("context")}
            className="flex items-center gap-2 w-full px-4 py-2.5 text-sm font-medium text-foreground hover:bg-muted"
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
                  <div className="flex items-center justify-between text-xs text-muted-foreground mb-1">
                    <span>Used</span>
                    <span className="font-mono text-muted-foreground">
                      {formatTokenCount(contextCurrent)} / {formatTokenCount(contextMax)}
                    </span>
                  </div>
                  <div className="h-2 w-full rounded bg-muted overflow-hidden">
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
                  <div className="text-right text-[11px] text-muted-foreground mt-1">
                    {contextPct}%
                    {contextModel && (
                      <span className="ml-2 text-foreground font-mono">
                        {contextModel}
                      </span>
                    )}
                  </div>
                </>
              ) : (
                <div className="text-xs text-muted-foreground">
                  No context data yet
                </div>
              )}
            </div>
          )}
        </div>

        {/* LSP Statuses Section */}
        <div className="border-b border-border">
          <button
            onClick={() => toggleSection("lsp")}
            className="flex items-center gap-2 w-full px-4 py-2.5 text-sm font-medium text-foreground hover:bg-muted"
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
                  {lspServers.map((s) => {
                    const errs = s.diagnostics_errors ?? 0;
                    const warns = s.diagnostics_warnings ?? 0;
                    const isIndexing = s.state === "starting";
                    const isFailed = s.state === "failed";
                    let sym = "✓";
                    let label = "clean";
                    let color = "text-emerald-400";
                    if (isFailed) {
                      sym = "✗";
                      label = s.detail || "failed";
                      color = "text-red-400";
                    } else if (isIndexing) {
                      sym = "◌";
                      label = "indexing…";
                      color = "text-muted-foreground";
                    } else if (errs > 0 && warns > 0) {
                      sym = "●";
                      label = `${errs} ${errs === 1 ? "error" : "errors"}, ${warns} ${warns === 1 ? "warning" : "warnings"}`;
                      color = "text-red-400";
                    } else if (errs > 0) {
                      sym = "●";
                      label = `${errs} ${errs === 1 ? "error" : "errors"}`;
                      color = "text-red-400";
                    } else if (warns > 0) {
                      sym = "△";
                      label = `${warns} ${warns === 1 ? "warning" : "warnings"}`;
                      color = "text-yellow-400";
                    }
                    return (
                      <div key={s.cmd} className="rounded bg-muted p-2">
                        <div className="flex items-center gap-2">
                          <span className="text-xs font-mono text-muted-foreground truncate flex-1">
                            {s.cmd}
                          </span>
                          <span className={`text-xs font-mono flex-shrink-0 ${color}`}>
                            {sym} {label}
                          </span>
                        </div>
                        {s.lang_id && (
                          <div className="text-[11px] text-muted-foreground truncate">
                            {s.lang_id}
                            {s.root ? ` · ${s.root}` : ""}
                          </div>
                        )}
                        {isFailed && s.detail && (
                          <div className="text-[11px] text-red-400/70 truncate mt-0.5">{s.detail}</div>
                        )}
                      </div>
                    );
                  })}
                </div>
              ) : (
                <div className="text-xs text-muted-foreground">No LSP servers</div>
              )}
            </div>
          )}
        </div>

        {/* Modified Files Section */}
        <div className="border-b border-border">
          <button
            onClick={() => toggleSection("files")}
            className="flex items-center gap-2 w-full px-4 py-2.5 text-sm font-medium text-foreground hover:bg-muted"
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
                      className="flex items-center gap-2 text-xs text-muted-foreground p-1.5 rounded hover:bg-muted"
                    >
                      <span
                        className={`flex-shrink-0 w-4 text-center font-mono ${
                          f.status === "M"
                            ? "text-yellow-400"
                            : f.status === "A"
                              ? "text-emerald-400"
                              : f.status === "D"
                                ? "text-red-400"
                                : "text-muted-foreground"
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
                <div className="text-xs text-muted-foreground">No modified files</div>
              )}
            </div>
          )}
        </div>

        {/* Plugins Section — opens the full plugin manager dialog. */}
        <div className="border-b border-border">
          <button
            onClick={() => setPluginsOpen(true)}
            className="flex items-center gap-2 w-full px-4 py-2.5 text-sm font-medium text-foreground hover:bg-muted"
          >
            <ChevronRight className="w-4 h-4" />
            <Puzzle className="w-4 h-4 text-fuchsia-400" />
            Plugins
          </button>
        </div>

        {/* TODO Section — no live data source is exposed by the backend yet,
            so this shows a stable empty state instead of a list that can never
            update. */}
        <div className="border-b border-border">
          <button
            onClick={() => toggleSection("todo")}
            className="flex items-center gap-2 w-full px-4 py-2.5 text-sm font-medium text-foreground hover:bg-muted"
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
                      className="flex items-start gap-2 text-xs text-muted-foreground p-1.5 rounded hover:bg-muted"
                    >
                      <Zap className="w-3 h-3 mt-0.5 flex-shrink-0 text-orange-400" />
                      <span>{item}</span>
                    </div>
                  ))}
                </div>
              ) : (
                <div className="text-xs text-muted-foreground">
                  <div>No TODO items</div>
                  <div className="mt-2 text-foreground">
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
          className={`fixed inset-y-0 right-0 z-50 w-72 border-l border-border bg-card flex flex-col overflow-hidden transition-transform duration-200 ${
            isOpen ? "translate-x-0" : "translate-x-full"
          }`}
        >
          {content}
        </aside>
      </>
    );
  }

  return (
    <aside className="w-72 flex-shrink-0 border-l border-border bg-card flex flex-col overflow-hidden">
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
