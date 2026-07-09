import { useState, useEffect } from "react";
import { api, apiPath, authHeaders } from "../../api/client";
import { useChatState, useChatDispatch } from "../../stores/chatStore";
import type { AgentInfo, LSPStatus, MCPStatus } from "../../api/types";
import { applyThemeColors } from "../../hooks/useTheme";
import PluginsPanel from "./PluginsPanel";
import {
  Bot,
  Wrench,
  FileText,
  ChevronDown,
  ChevronRight,
  Cpu,
  Link,
  Hash,
  Zap,
  Target,
  GitBranch,
  Palette,
  AlertCircle,
  AlertTriangle,
  Puzzle,
} from "lucide-react";

interface Props {
  isOpen: boolean;
  onClose: () => void;
  activeAgent: string;
  onModelClick?: (tab: "main" | "small" | "advisor") => void;
  // When true the sidebar becomes a fixed overlay (right side) with a backdrop
  // instead of pushing the chat column. Used for the mobile layout (≤767px).
  isMobile?: boolean;
}

interface ConfigState {
  model: string;
  smallModel: string;
  advisorModel: string;
  permissionMode: string;
  autoAllow: boolean;
}

export default function CoworkSidebar({
  isOpen,
  onClose,
  activeAgent,
  onModelClick,
  isMobile,
}: Props) {
  const [agents, setAgents] = useState<AgentInfo[]>([]);
  const [config, setConfig] = useState<ConfigState>({
    model: "",
    smallModel: "",
    advisorModel: "",
    permissionMode: "auto",
    autoAllow: false,
  });
  const [expandedSections, setExpandedSections] = useState<Record<string, boolean>>({
    agent: true,
    models: false,
    tools: false,
    context: true,
    paths: false,
    lsp: false,
    files: false,
    todo: false,
    theme: false,
    git: true,
  });
  const [gitBranch, setGitBranch] = useState<string>("");
  const [mcpServers, setMcpServers] = useState<MCPStatus[]>([]);
  const [mcpBusy, setMcpBusy] = useState<string | null>(null);
  const [themes, setThemes] = useState<{ name: string; label: string }[]>([]);
  const [currentTheme, setCurrentTheme] = useState<string>("");
  const [todoItems] = useState<string[]>([]);
  // Locally-tracked active agent. The `activeAgent` prop is fixed by the
  // parent, so switching agents is reflected via this optimistic state.
  const [selectedAgent, setSelectedAgent] = useState<string>(activeAgent);
  const [agentBusy, setAgentBusy] = useState(false);
  const [pluginsOpen, setPluginsOpen] = useState(false);
  const {
    model,
    smallModel,
    advisorModel,
    advisorEnabled,
    ocrModel,
    ocrEnabled,
    ocrBackend,
    sessionId,
    tuiStatus,
  } = useChatState();
  const dispatch = useChatDispatch();

  // Fetch git branch periodically
  useEffect(() => {
    const fetchBranch = async () => {
      try {
        const res = await fetch(apiPath("/api/git/status"), {
          headers: authHeaders(),
        });
        const data = await res.json();
        if (data.branch) {
          setGitBranch(data.branch);
        }
      } catch (err) {
        console.error("Failed to fetch git branch:", err);
      }
    };
    fetchBranch();
    const interval = setInterval(fetchBranch, 10000);
    return () => clearInterval(interval);
  }, []);

  useEffect(() => {
    api.listAgents().then(setAgents).catch(console.error);

    // Fetch config
    Promise.all([
      fetch(apiPath("/api/config/model"), { headers: authHeaders() }).then((r) => r.json()),
      fetch(apiPath("/api/config/small-model"), { headers: authHeaders() }).then((r) => r.json()),
      fetch(apiPath("/api/config/advisor"), { headers: authHeaders() }).then((r) => r.json()),
      fetch(apiPath("/api/permissions"), { headers: authHeaders() }).then((r) => r.json()),
    ])
      .then(([modelRes, smallRes, advisorRes, permRes]) => {
        setConfig({
          model: (modelRes as { model: string }).model || "",
          smallModel: (smallRes as { small_model: string }).small_model || "",
          advisorModel: (advisorRes as { advisor: string }).advisor || "",
          permissionMode: (permRes as { mode: string }).mode || "auto",
          autoAllow: (permRes as { auto_allow: boolean }).auto_allow || false,
        });
      })
      .catch(console.error);
  }, []);

  // Fetch real MCP server status (replaces the previously hardcoded tool chips).
  useEffect(() => {
    api.getMCP().then(setMcpServers).catch(console.error);
  }, []);

  // Fetch the list of available themes for the theme selector.
  useEffect(() => {
    api
      .getThemes()
      .then((res) => {
        setThemes(res.themes);
        setCurrentTheme(res.current);
      })
      .catch(console.error);
  }, []);

  const currentAgent = agents.find((a) => a.name === selectedAgent);

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

  // Enable/disable an MCP server. Optimistically flips the toggle; rolls back on
  // failure so the UI stays truthful.
  const toggleMcp = (server: MCPStatus) => {
    const next = !server.enabled;
    setMcpBusy(server.name);
    setMcpServers((prev) =>
      prev.map((m) => (m.name === server.name ? { ...m, enabled: next } : m)),
    );
    api
      .setMCPEnabled(server.name, next)
      .catch((err) => {
        console.error("failed to toggle MCP server", err);
        setMcpServers((prev) =>
          prev.map((m) => (m.name === server.name ? { ...m, enabled: server.enabled } : m)),
        );
      })
      .finally(() => setMcpBusy(null));
  };

  const toggleAdvisor = () => {
    const next = !advisorEnabled;
    dispatch({ type: "SET_ADVISOR_ENABLED", enabled: next });
    api.setAdvisorEnabled(next).catch((err) => {
      console.error("failed to toggle advisor", err);
      // Roll back optimistic update on failure so the UI stays truthful.
      dispatch({ type: "SET_ADVISOR_ENABLED", enabled: advisorEnabled });
    });
  };

  const toggleOcr = () => {
    const next = !ocrEnabled;
    dispatch({ type: "SET_OCR_ENABLED", enabled: next });
    api.setOcrEnabled(next).catch((err) => {
      console.error("failed to toggle ocr", err);
      // Roll back optimistic update on failure so the UI stays truthful.
      dispatch({ type: "SET_OCR_ENABLED", enabled: ocrEnabled });
    });
  };

  // Apply a theme by fetching its colors from the server and writing them to
  // the CSS variables. The change is visual only (the web is a remote viewer);
  // the TUI theme is owned by the terminal config.
  const applyTheme = (name: string) => {
    api
      .getTheme(name)
      .then((resp) => {
        applyThemeColors(resp.colors);
        setCurrentTheme(name);
      })
      .catch((err) => console.error("failed to apply theme", err));
  };

  // Real data sourced from the live TUI status snapshot.
  const contextCurrent = tuiStatus?.context_current_tokens ?? 0;
  const contextMax = tuiStatus?.context_max_tokens ?? 0;
  const contextPct =
    contextMax > 0
      ? Math.min(100, Math.round((contextCurrent / contextMax) * 100))
      : 0;
  const lspServers: LSPStatus[] = tuiStatus?.lsp_servers ?? [];
  const modifiedFiles = tuiStatus?.modified_files ?? [];
  const extraPaths = tuiStatus?.extra_allowed_paths ?? [];

  // On mobile the sidebar is always mounted (so it can slide); when closed it
  // sits off-screen. On desktop it is fully removed when closed so the chat
  // column reclaims the space (push layout).
  if (!isOpen && !isMobile) return null;

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

        {/* Models Section */}
        <div className="border-b border-zinc-700">
          <button
            onClick={() => toggleSection("models")}
            className="flex items-center gap-2 w-full px-4 py-2.5 text-sm font-medium text-zinc-300 hover:bg-zinc-800"
          >
            {expandedSections.models ? (
              <ChevronDown className="w-4 h-4" />
            ) : (
              <ChevronRight className="w-4 h-4" />
            )}
            <Cpu className="w-4 h-4 text-purple-400" />
            Models
          </button>
          {expandedSections.models && (
            <div className="px-4 pb-3 space-y-2">
              <button
                type="button"
                onClick={() => onModelClick?.("main")}
                className="w-full rounded px-1 py-1 text-left text-xs transition-colors hover:bg-zinc-800 disabled:cursor-default disabled:hover:bg-transparent"
                disabled={!onModelClick}
              >
                <div className="text-zinc-500 mb-1">Main Model</div>
                <div className="text-zinc-300 font-mono truncate">
                  {model || config.model || "Not set"}
                </div>
              </button>
              <button
                type="button"
                onClick={() => onModelClick?.("small")}
                className="w-full rounded px-1 py-1 text-left text-xs transition-colors hover:bg-zinc-800 disabled:cursor-default disabled:hover:bg-transparent"
                disabled={!onModelClick}
              >
                <div className="text-zinc-500 mb-1">Small Model</div>
                <div className="text-zinc-300 font-mono truncate">
                  {smallModel || config.smallModel || "Not set"}
                </div>
              </button>
              <button
                type="button"
                onClick={() => onModelClick?.("advisor")}
                className="w-full rounded px-1 py-1 text-left text-xs transition-colors hover:bg-zinc-800 disabled:cursor-default disabled:hover:bg-transparent"
                disabled={!onModelClick}
              >
                <div className="text-zinc-500 mb-1">Advisor Model</div>
                <div className="text-zinc-300 font-mono truncate">
                  {advisorModel || config.advisorModel || "Not set"}
                </div>
              </button>
              {/* Runtime advisor on/off — session-only, not saved to config. */}
              <button
                type="button"
                onClick={toggleAdvisor}
                className="flex w-full items-center justify-between rounded px-1 py-1 text-left text-xs transition-colors hover:bg-zinc-800"
                title="Enable or disable the advisor tool for this session (not saved to config)"
              >
                <span className="text-zinc-500">Advisor</span>
                <span className="flex items-center gap-2">
                  <span
                    className={`font-mono ${advisorEnabled ? "text-emerald-400" : "text-zinc-500"}`}
                  >
                    {advisorEnabled ? "on" : "off"}
                  </span>
                  <span
                    className={`relative inline-flex h-4 w-7 flex-shrink-0 items-center rounded-full transition-colors ${
                      advisorEnabled ? "bg-emerald-500/80" : "bg-zinc-600"
                    }`}
                  >
                    <span
                      className={`inline-block h-3 w-3 transform rounded-full bg-white transition-transform ${
                        advisorEnabled ? "translate-x-3.5" : "translate-x-0.5"
                      }`}
                    />
                  </span>
                </span>
              </button>
              {/* OCR tool on/off toggle */}
              <div className="mt-1 mb-1 border-t border-zinc-700/50" />
              <div className="text-zinc-500 mb-1 text-xs">OCR</div>
              <div className="text-zinc-600 text-xs mb-1">{ocrBackend || "openai-compat"}</div>
              <button
                type="button"
                onClick={toggleOcr}
                className="flex w-full items-center justify-between rounded px-1 py-1 text-left text-xs transition-colors hover:bg-zinc-800"
                title="Enable or disable the OCR tool"
              >
                <span className="text-zinc-400 truncate font-mono">
                  {ocrModel || "Not set"}
                </span>
                <span className="flex items-center gap-2">
                  <span
                    className={`font-mono ${ocrEnabled ? "text-emerald-400" : "text-zinc-500"}`}
                  >
                    {ocrEnabled ? "on" : "off"}
                  </span>
                  <span
                    className={`relative inline-flex h-4 w-7 flex-shrink-0 items-center rounded-full transition-colors ${
                      ocrEnabled ? "bg-emerald-500/80" : "bg-zinc-600"
                    }`}
                  >
                    <span
                      className={`inline-block h-3 w-3 transform rounded-full bg-white transition-transform ${
                        ocrEnabled ? "translate-x-3.5" : "translate-x-0.5"
                      }`}
                    />
                  </span>
                </span>
              </button>
              <div className="text-xs">
                <div className="text-zinc-500 mb-1">Permission Model</div>
                <div className="text-zinc-300 font-mono truncate">
                  {config.autoAllow ? "Auto-allow" : config.permissionMode}
                </div>
              </div>
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
                    {tuiStatus?.context_model && (
                      <span className="ml-2 text-zinc-600 font-mono">
                        {tuiStatus.context_model}
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

        {/* Extra Allowed Paths Section */}
        <div className="border-b border-zinc-700">
          <button
            onClick={() => toggleSection("paths")}
            className="flex items-center gap-2 w-full px-4 py-2.5 text-sm font-medium text-zinc-300 hover:bg-zinc-800"
          >
            {expandedSections.paths ? (
              <ChevronDown className="w-4 h-4" />
            ) : (
              <ChevronRight className="w-4 h-4" />
            )}
            <Link className="w-4 h-4 text-indigo-400" />
            Extra Paths
          </button>
          {expandedSections.paths && (
            <div className="px-4 pb-3">
              {extraPaths.length > 0 ? (
                <div className="space-y-1">
                  {extraPaths.map((p) => (
                    <div
                      key={p}
                      className="text-xs text-zinc-400 p-1.5 rounded bg-zinc-800 font-mono truncate"
                      title={p}
                    >
                      {p}
                    </div>
                  ))}
                </div>
              ) : (
                <div className="text-xs text-zinc-500">No extra paths</div>
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

        {/* Tools / MCP Section — real MCP server status. */}
        <div className="border-b border-zinc-700">
          <button
            onClick={() => toggleSection("tools")}
            className="flex items-center gap-2 w-full px-4 py-2.5 text-sm font-medium text-zinc-300 hover:bg-zinc-800"
          >
            {expandedSections.tools ? (
              <ChevronDown className="w-4 h-4" />
            ) : (
              <ChevronRight className="w-4 h-4" />
            )}
            <Wrench className="w-4 h-4 text-green-400" />
            Tools / MCP
          </button>
          {expandedSections.tools && (
            <div className="px-4 pb-3">
              {mcpServers.length > 0 ? (
                <div className="space-y-1">
                  {mcpServers.map((m) => (
                    <div
                      key={m.name}
                      className="flex items-center justify-between text-xs text-zinc-400 p-1.5 rounded bg-zinc-800"
                    >
                      <span className="truncate font-mono" title={m.name}>
                        {m.name}
                      </span>
                      <button
                        type="button"
                        onClick={() => toggleMcp(m)}
                        disabled={mcpBusy === m.name}
                        className="flex items-center gap-2 flex-shrink-0 disabled:opacity-50"
                        title={m.enabled ? "Disable MCP server" : "Enable MCP server"}
                      >
                        <span
                          className={`text-[10px] ${
                            m.enabled ? "text-emerald-400" : "text-zinc-500"
                          }`}
                        >
                          {m.enabled ? "on" : "off"}
                        </span>
                        <span
                          className={`relative inline-flex h-4 w-7 flex-shrink-0 items-center rounded-full transition-colors ${
                            m.enabled ? "bg-emerald-500/80" : "bg-zinc-600"
                          }`}
                        >
                          <span
                            className={`inline-block h-3 w-3 transform rounded-full bg-white transition-transform ${
                              m.enabled ? "translate-x-3.5" : "translate-x-0.5"
                            }`}
                          />
                        </span>
                      </button>
                    </div>
                  ))}
                </div>
              ) : (
                <div className="text-xs text-zinc-500">No MCP servers</div>
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

        {/* Theme Section — functional theme selector. */}
        <div className="border-b border-zinc-700">
          <button
            onClick={() => toggleSection("theme")}
            className="flex items-center gap-2 w-full px-4 py-2.5 text-sm font-medium text-zinc-300 hover:bg-zinc-800"
          >
            {expandedSections.theme ? (
              <ChevronDown className="w-4 h-4" />
            ) : (
              <ChevronRight className="w-4 h-4" />
            )}
            <Palette className="w-4 h-4 text-pink-400" />
            Theme
          </button>
          {expandedSections.theme && (
            <div className="px-4 pb-3">
              {themes.length > 0 ? (
                <div className="grid grid-cols-2 gap-1.5">
                  {themes.map((t) => (
                    <button
                      key={t.name}
                      type="button"
                      onClick={() => applyTheme(t.name)}
                      className={`text-xs rounded px-2 py-1.5 truncate transition-colors ${
                        currentTheme === t.name
                          ? "bg-emerald-600/30 text-emerald-300 border border-emerald-600/50"
                          : "bg-zinc-800 text-zinc-400 hover:bg-zinc-700"
                      }`}
                      title={t.name}
                    >
                      {t.label}
                    </button>
                  ))}
                </div>
              ) : (
                <div className="text-xs text-zinc-500">No themes</div>
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
