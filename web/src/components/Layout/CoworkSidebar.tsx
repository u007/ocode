import { useState, useEffect } from "react";
import { api, apiPath, authHeaders } from "../../api/client";
import { useChatState, useChatDispatch } from "../../stores/chatStore";
import type { AgentInfo } from "../../api/types";
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
} from "lucide-react";

interface Props {
  isOpen: boolean;
  onClose: () => void;
  activeAgent: string;
  onModelClick?: (tab: "main" | "small" | "advisor") => void;
}

interface ConfigState {
  model: string;
  smallModel: string;
  advisorModel: string;
  permissionMode: string;
  autoAllow: boolean;
}

interface TokenUsage {
  inputTokens: number;
  outputTokens: number;
  cachedTokens: number;
}

export default function CoworkSidebar({ isOpen, onClose, activeAgent, onModelClick }: Props) {
  const [agents, setAgents] = useState<AgentInfo[]>([]);
  const [config, setConfig] = useState<ConfigState>({
    model: "",
    smallModel: "",
    advisorModel: "",
    permissionMode: "auto",
    autoAllow: false,
  });
  const [tokens] = useState<TokenUsage>({
    inputTokens: 0,
    outputTokens: 0,
    cachedTokens: 0,
  });
  const [contextFiles] = useState<string[]>([]);
  const [todoItems] = useState<string[]>([]);
  const [expandedSections, setExpandedSections] = useState<Record<string, boolean>>({
    agent: true,
    models: false,
    tools: false,
    tokens: true,
    context: false,
    todo: false,
    git: true,
  });
  const [gitBranch, setGitBranch] = useState<string>("");
  const { model, smallModel, advisorModel, advisorEnabled, sessionId } = useChatState();
  const dispatch = useChatDispatch();

  // Fetch git branch periodically
  useEffect(() => {
    const fetchBranch = async () => {
      try {
        const res = await fetch(apiPath("/api/git/status"), { headers: authHeaders() });
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

  const toggleAdvisor = () => {
    const next = !advisorEnabled;
    dispatch({ type: "SET_ADVISOR_ENABLED", enabled: next });
    api.setAdvisorEnabled(next).catch((err) => {
      console.error("failed to toggle advisor", err);
      // Roll back optimistic update on failure so the UI stays truthful.
      dispatch({ type: "SET_ADVISOR_ENABLED", enabled: advisorEnabled });
    });
  };

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

  const currentAgent = agents.find((a) => a.name === activeAgent);

  const toggleSection = (section: string) => {
    setExpandedSections((prev) => ({ ...prev, [section]: !prev[section] }));
  };

  if (!isOpen) return null;

  return (
    <aside className="w-72 flex-shrink-0 border-l border-zinc-700 bg-zinc-900 flex flex-col overflow-hidden">
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
                  {currentAgent?.name || activeAgent}
                </div>
                <div className="text-xs text-zinc-500 mt-1">
                  {currentAgent?.description || "No description"}
                </div>
              </div>
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
              <div className="text-xs">
                <div className="text-zinc-500 mb-1">Permission Model</div>
                <div className="text-zinc-300 font-mono truncate">
                  {config.autoAllow ? "Auto-allow" : config.permissionMode}
                </div>
              </div>
            </div>
          )}
        </div>

        {/* Tools Section */}
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
            Tools Allowed
          </button>
          {expandedSections.tools && (
            <div className="px-4 pb-3">
              <div className="flex flex-wrap gap-1.5">
                {[
                  "read",
                  "write",
                  "bash",
                  "grep",
                  "glob",
                  "lsp",
                  "websearch",
                  "task",
                  "agent",
                ].map((tool) => (
                  <span
                    key={tool}
                    className="inline-flex items-center px-2 py-1 rounded text-xs bg-zinc-800 text-zinc-400"
                  >
                    {tool}
                  </span>
                ))}
              </div>
            </div>
          )}
        </div>

        {/* Token Usage Section */}
        <div className="border-b border-zinc-700">
          <button
            onClick={() => toggleSection("tokens")}
            className="flex items-center gap-2 w-full px-4 py-2.5 text-sm font-medium text-zinc-300 hover:bg-zinc-800"
          >
            {expandedSections.tokens ? (
              <ChevronDown className="w-4 h-4" />
            ) : (
              <ChevronRight className="w-4 h-4" />
            )}
            <Hash className="w-4 h-4 text-cyan-400" />
            Token Usage
          </button>
          {expandedSections.tokens && (
            <div className="px-4 pb-3 space-y-2">
              <div className="grid grid-cols-3 gap-2 text-xs">
                <div className="text-center p-2 rounded bg-zinc-800">
                  <div className="text-zinc-500">In</div>
                  <div className="text-zinc-300 font-mono mt-1">
                    {tokens.inputTokens > 0 ? formatTokenCount(tokens.inputTokens) : "—"}
                  </div>
                </div>
                <div className="text-center p-2 rounded bg-zinc-800">
                  <div className="text-zinc-500">Cache</div>
                  <div className="text-zinc-300 font-mono mt-1">
                    {tokens.cachedTokens > 0 ? formatTokenCount(tokens.cachedTokens) : "—"}
                  </div>
                </div>
                <div className="text-center p-2 rounded bg-zinc-800">
                  <div className="text-zinc-500">Out</div>
                  <div className="text-zinc-300 font-mono mt-1">
                    {tokens.outputTokens > 0 ? formatTokenCount(tokens.outputTokens) : "—"}
                  </div>
                </div>
              </div>
            </div>
          )}
        </div>

        {/* Context Section */}
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
            <FileText className="w-4 h-4 text-yellow-400" />
            Context
          </button>
          {expandedSections.context && (
            <div className="px-4 pb-3">
              {contextFiles.length > 0 ? (
                <div className="space-y-1">
                  {contextFiles.map((file) => (
                    <div
                      key={file}
                      className="flex items-center gap-2 text-xs text-zinc-400 p-1.5 rounded hover:bg-zinc-800"
                    >
                      <Link className="w-3 h-3 flex-shrink-0" />
                      <span className="truncate font-mono">{file}</span>
                    </div>
                  ))}
                </div>
              ) : (
                <div className="text-xs text-zinc-500">
                  <div>No files in context</div>
                  <div className="mt-2 text-zinc-600">
                    Click files in the Files tab to add them
                  </div>
                </div>
              )}
            </div>
          )}
        </div>

        {/* TODO Section */}
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
    </aside>
  );
}

function formatTokenCount(n: number): string {
  if (n <= 0) return "0";
  if (n < 1000) return String(n);
  if (n < 1_000_000) return `${(n / 1000).toFixed(1)}k`;
  return `${(n / 1_000_000).toFixed(1)}M`;
}
