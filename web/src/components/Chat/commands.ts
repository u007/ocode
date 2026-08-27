import type {
  Message,
  OcrConfig,
  OcrModelsResponse,
  UsageSummary,
  PermissionsResponse,
} from "../../api/types";
import type { LucideIcon } from "lucide-react";
import { useEffect, useState } from "react";
import { api } from "../../api/client";

import {
  Plus,
  Trash2,
  Settings,
  Archive,
  FileText,
  FileDown,
  Share2,
  HelpCircle,
  History,
  Eye,
  Search,
  MessageCircle,
  Shield,
  Undo2,
  Redo2,
  Type,
  BarChart3,
  Sparkles,
  Zap,
  Bot,
  Puzzle,
  Terminal,
  GitPullRequest,
  ListChecks,
  Activity,
  Gauge,
  Cpu,
  GitBranch,
  CalendarClock,
  Radio,
} from "lucide-react";

// ─── Command Definition ────────────────────────────────────────────────────

export interface CommandDef {
  name: string;
  description: string;
  icon: LucideIcon;
}

/**
 * Canonical list of slash commands available in the web UI.
 *
 * Each entry has:
 * - `name` – the command string (e.g. "/new")
 * - `description` – short help text shown in the autocomplete popup
 * - `icon` – a lucide-react icon for the popup
 *
 * The list is used by both `SlashCommandMenu` (autocomplete popup) and
 * `ChatInput` (keyboard navigation).  Edit here only — do not duplicate.
 */
export const COMMANDS: CommandDef[] = [
  { name: "/new", description: "Start a new session", icon: Plus },
  { name: "/clear", description: "Clear conversation history", icon: Trash2 },
  { name: "/model", description: "Open model selector", icon: Settings },
  { name: "/agent", description: "List or switch the active agent", icon: Bot },
  { name: "/session", description: "List, load, or resume sessions", icon: History },
  { name: "/title", description: "Set the current session title", icon: Type },
  { name: "/ocr", description: "Show OCR status, enable/disable, set model", icon: Eye },
  { name: "/search", description: "Find a message by keyword", icon: Search },
  { name: "/btw", description: "Add a quick aside to the conversation", icon: MessageCircle },
  { name: "/mask", description: "Show secret redaction status", icon: Shield },
  { name: "/permissions", description: "Show permission rules", icon: Shield },
  { name: "/yolo", description: "Toggle YOLO (auto-approve) mode", icon: Zap },
  { name: "/undo", description: "Undo the last file change", icon: Undo2 },
  { name: "/redo", description: "Redo the last undone file change", icon: Redo2 },
  { name: "/compact", description: "Compact conversation context", icon: Archive },
  { name: "/recap", description: "Generate session recap", icon: FileText },
  { name: "/usage", description: "Show token usage & spend", icon: BarChart3 },
  { name: "/export", description: "Export session as Markdown", icon: FileDown },
  { name: "/export-claude", description: "Append session to Claude history", icon: FileDown },
  { name: "/init", description: "Create an AGENTS.md for this project", icon: Sparkles },
  { name: "/plugin", description: "List, enable, install, or remove plugins", icon: Puzzle },
  { name: "/share", description: "Share session link", icon: Share2 },
  { name: "/help", description: "Show available commands", icon: HelpCircle },
  { name: "/standup", description: "Review recent commits + pending changes (standup summary)", icon: ListChecks },
  { name: "/changes", description: "Analyze repo changes: diffs, LSP errors, specs", icon: GitBranch },
  { name: "/review", description: "AI code review of changes, file, commit, branch, or PR", icon: GitPullRequest },
  { name: "/context", description: "Show context window token budget", icon: Gauge },
  { name: "/lsp", description: "Show LSP diagnostics and error counts", icon: Activity },
  { name: "/agents", description: "Show active/queued subagents", icon: Cpu },
  { name: "/skills", description: "List available skills", icon: Sparkles },
  { name: "/mcp", description: "List or toggle MCP servers", icon: Radio },
  { name: "/cron", description: "Manage scheduled jobs", icon: CalendarClock },
  { name: "/small-model", description: "Show or switch the small model", icon: Settings },
  { name: "/advisor", description: "Set the advisor model", icon: Bot },
  { name: "/github", description: "GitHub actions (pr, issue)", icon: GitPullRequest },
  { name: "/max-step", description: "Show or set the per-turn step cap (/max-step [n])", icon: Gauge },
  { name: "/effort", description: "Show or set reasoning effort (off/low/med/high/xhigh/max)", icon: Zap },
  { name: "/thinking", description: "Thinking-block visibility note (web always shows thinking)", icon: Eye },
  { name: "/models", description: "Switch model by name, or open the picker", icon: Settings },
  { name: "/goal", description: "Dispatch a goal to the orchestrator pipeline", icon: ListChecks },
  { name: "/autocontinue", description: "Auto-continue on|off|status|model [name]", icon: Activity },
  { name: "/mem", description: "Memory on|off|status|update [scope] [focus]", icon: Archive },
  { name: "/paths", description: "Show config/data paths for this project", icon: GitBranch },
  { name: "/cd", description: "Add a project root and switch to it", icon: Terminal },
  { name: "/add-dir", description: "List or add extra allowed paths", icon: Plus },
  { name: "/localmodel", description: "Local models: status/add/enable/disable/limit", icon: Cpu },
  { name: "/discover", description: "Codebase discovery: enable/disable/status/model/ignore", icon: Search },
  { name: "/login", description: "Link this app via device-code login", icon: Shield },
  { name: "/logout", description: "Unlink sync + revoke token", icon: Shield },
  { name: "/mcp-auth", description: "MCP OAuth (requires desktop/TUI browser flow)", icon: Radio },
  { name: "/learn", description: "Audit and improve project skills ([focus])", icon: Sparkles },
  { name: "/doc-sync", description: "Sync docs with recent changes ([session|all])", icon: FileText },
  { name: "/docs", description: "Knowledge system: on|off|status|init|update|cleanup", icon: FileText },
  { name: "/commands", description: "List every available command", icon: HelpCircle },
  { name: "/ban", description: "Banned bash prefixes: list|add|remove", icon: Shield },
  { name: "/image", description: "Image gen: status|enable|disable|model|timeout", icon: Sparkles },
  { name: "/upload", description: "Show or set the upload directory", icon: FileDown },
  { name: "/connect", description: "Store a provider API key (/connect provider key)", icon: Bot },
];

// ─── Dynamic commands (custom commands + skills from the server) ──────────────
//
// Custom slash commands (GET /api/commands) and skills (GET /api/skills) are
// merged into the shared COMMANDS array so they surface in every consumer
// (SlashCommandMenu, CommandPalette, and ChatInput's inline autocomplete, which
// all read the same array reference). They are NOT dispatched locally — when
// selected they fall through to the LLM as a chat message (handled: false).
//
// Names are lowercased so ChatInput's case-sensitive `.includes()` filter and
// SlashCommandMenu's lowercased filter stay index-aligned.

let dynamicLoaded = false;
let dynamicLoadPromise: Promise<void> | null = null;

/** Fetch custom commands + skills once and append them (deduped, sorted) to
 *  the shared COMMANDS array. Safe to call from multiple mount points — the
 *  fetch runs at most once. */
export function loadDynamicCommands(): Promise<void> {
  if (dynamicLoaded) return Promise.resolve();
  if (dynamicLoadPromise) return dynamicLoadPromise;

  dynamicLoadPromise = (async () => {
    const [commands, skills] = await Promise.all([
      api.listCommands().catch((err) => {
        console.error("failed to load custom commands", err);
        return [] as { name: string; description?: string }[];
      }),
      api.listSkills().catch((err) => {
        console.error("failed to load skills", err);
        return [] as { name: string; description?: string }[];
      }),
    ]);

    const existing = new Set(COMMANDS.map((c) => c.name.toLowerCase()));
    const additions: CommandDef[] = [];

    const add = (rawName: string, description: string, icon: LucideIcon) => {
      const name = ("/" + rawName.replace(/^\//, "")).toLowerCase();
      if (existing.has(name)) return;
      existing.add(name);
      additions.push({ name, description, icon });
    };

    for (const c of commands) {
      add(c.name, c.description || "Custom command", Terminal);
    }
    for (const s of skills) {
      add(s.name, s.description || "Skill", Sparkles);
    }

    additions.sort((a, b) => a.name.localeCompare(b.name));
    COMMANDS.push(...additions);
    dynamicLoaded = true;
  })();

  return dynamicLoadPromise;
}

/** React hook: ensures dynamic commands are loaded and returns the merged
 *  command list (built-ins + custom commands + skills). Re-renders the caller
 *  once the async load completes. */
export function useCommands(): CommandDef[] {
  const [commands, setCommands] = useState<CommandDef[]>(COMMANDS);

  useEffect(() => {
    let cancelled = false;
    loadDynamicCommands().then(() => {
      // Copy the shared array into local state so React re-renders with the
      // merged list. Guard against setState-after-unmount.
      if (!cancelled) setCommands([...COMMANDS]);
    });
    return () => {
      cancelled = true;
    };
  }, []);

  return commands;
}

/** Return a `Message` (assistant role) describing the available commands. */
export function helpMessage(): Message {
  const lines = COMMANDS.map(
    (c) => `- **${c.name}** — ${c.description}`,
  );
  return {
    role: "assistant",
    content: `## Available Commands\n\n${lines.join("\n")}\n\nType \`/\` to see the autocomplete menu.`,
  };
}

// ─── Command Handler Dispatch ──────────────────────────────────────────────

/**
 * Result returned by a frontend command handler.
 * - `handled: true` — the command was fully handled on the frontend
 * - `handled: false` — the command should fall through to the LLM as a message
 * - `messages` — optional assistant messages to inject into the chat
 * - `sessionId` — optional session to load / switch to
 * - `newSession` — if true, reset to a fresh session
 * - `prompt` — a user-role message to send through the normal chat pipeline
 *   (used by repo-analysis commands like /standup, /changes, /review: the
 *   server gathers git context and returns the assembled prompt, which the
 *   client then sends verbatim so the LLM answers it like a TUI turn)
 */
export interface CommandResult {
  handled: boolean;
  messages?: Message[];
  sessionId?: string;
  newSession?: boolean;
  /** Trigger a file download in the browser. */
  download?: {
    filename: string;
    content: string;
    mimeType: string;
  };
  /** User-role message to send through the normal chat send path. */
  prompt?: string;
  /** Open the model picker dialog (web equivalent of the TUI's no-arg /model). */
  openModelPicker?: boolean;
}

/**
 * `commandName` — e.g. "/session"
 * `args` — everything after the first space, trimmed
 * `api` — the api client instance (injected so this module has no direct dep)
 */
export interface CommandContext {
  commandName: string;
  args: string;
  /** Caller-provided helpers the handler can use. */
  api: {
    listSessions: () => Promise<{ id: string; title: string }[]>;
    getSession: (id: string) => Promise<{ messages?: Message[]; title?: string }>;
    getOcrConfig: () => Promise<OcrConfig>;
    setOcrConfig: (cfg: OcrConfig) => Promise<OcrConfig>;
    getOcrModels: () => Promise<OcrModelsResponse>;
    getOcrEnabled: () => Promise<{ enabled: boolean; model: string }>;
    setOcrEnabled: (enabled: boolean) => Promise<unknown>;
    setOcrModel: (model: string) => Promise<unknown>;
    compactSession: (id: string) => Promise<{ original_len: number; compacted_len: number }>;
    recapSession: (id: string) => Promise<{ recap: string }>;
    shareSession: (id: string) => Promise<{ markdown: string }>;
    btwSession: (id: string, content: string) => Promise<{ status: string }>;
    getMaskConfig: () => Promise<{ enabled: boolean; mode: string; model: string }>;
    setMaskEnabled: (enabled: boolean) => Promise<{ enabled: boolean }>;
    setMaskMode: (mode: string) => Promise<{ mode: string }>;
    setMaskModel: (model: string) => Promise<{ model: string }>;
    /** Fetch an assembled LLM prompt for a repo-analysis command (/standup, /changes, /review). */
    getCommandContext: (name: string, args?: string) => Promise<{ prompt: string }>;
    /** Token budget for the current session (/context). */
    getSessionContext: (id: string) => Promise<{
      session_id: string;
      message_count: number;
      estimated_tokens: number;
      max_tokens?: number;
      model?: string;
    }>;
    /** LSP server status + aggregated diagnostic counts (/lsp). */
    getLSPStatuses: () => Promise<{ lsp_servers: import("../../api/types").LSPStatus[] }>;
    /** Available skills (/skills). */
    listSkills: () => Promise<import("../../api/types").SkillEntry[]>;
    /** MCP server status (/mcp). */
    getMCP: () => Promise<import("../../api/types").MCPStatus[]>;
    /** GitHub PR details + diff (/github pr). */
    getGithubPR: (owner: string, repo: string, number: number) => Promise<{ pr: Record<string, unknown>; diff?: string }>;
    /** GitHub issue list (/github issue list). */
    getGithubIssues: (owner: string, repo: string, state?: string) => Promise<Record<string, unknown>[]>;
    /** Subagent runs (/agents). */
    getAgentRuns?: () => Promise<{ id: string; agent?: string; title?: string; status?: string; state?: string }[]>;
    /** Scheduled jobs (/cron). */
    getCronJobs?: () => Promise<{ id: string | number; name?: string; next_run?: string }[]>;
    /** Small-model config (/small-model). */
    getSmallModelWithEnabled?: () => Promise<{ model: string; enabled: boolean }>;
    /** Advisor model (/advisor). */
    getAdvisor?: () => Promise<{ model: string }>;

    // ── Slash-command parity additions ──
    getLimitsConfig?: () => Promise<{ max_steps: number; image_max_dim: number; max_concurrent_agents: number; undo_max_age_delta: number }>;
    setLimitsConfig?: (fields: { max_steps: number; image_max_dim: number; max_concurrent_agents: number; undo_max_age_delta: number }) => Promise<unknown>;
    getThinkingBudget?: () => Promise<{ budget: number; level: string; levels: { level: string; budget: number }[] }>;
    setThinkingBudget?: (level: string) => Promise<unknown>;
    listModels?: () => Promise<{ name: string; model: string; provider: string; active: boolean }[]>;
    getConfigModel?: () => Promise<{ model: string }>;
    setConfigModel?: (model: string) => Promise<unknown>;
    getFeaturesConfig?: () => Promise<{ memory_enabled: boolean; doc_prompt_enabled: boolean }>;
    setFeaturesConfig?: (memory_enabled: boolean, doc_prompt_enabled: boolean) => Promise<unknown>;
    getPathsInfo?: () => Promise<{ work_dir: string; extra_allowed_paths: string[]; upload_dir: string; text: string }>;
    getPathsConfig?: () => Promise<{ extra_allowed_paths: string[]; upload_dir: string }>;
    setPathsConfig?: (extra_allowed_paths: string[], upload_dir: string) => Promise<unknown>;
    getMemoryStatus?: () => Promise<import("../../api/types").MemoryStatusResponse>;
    setBashRule?: (prefix: string, level: "allow" | "deny" | "ask") => Promise<unknown>;
    getPermissions?: () => Promise<PermissionsResponse>;
    getAutoContinue?: () => Promise<{ enabled: boolean; model: string }>;
    setAutoContinue?: (fields: { enabled?: boolean; model?: string; clear?: boolean }) => Promise<{ enabled: boolean; model: string }>;
    connectProvider?: (provider: string, api_key: string) => Promise<{ provider: string; key: string }>;
    addProject?: (path: string) => Promise<unknown>;
    getDocsStatus?: () => Promise<{ enabled: boolean; text: string }>;
    docsInit?: () => Promise<{ result: string; annotate_prompt?: string }>;
    docsUpdate?: (sessionId: string, focus: string) => Promise<{ result: string }>;
    docsCleanup?: (confirm: boolean) => Promise<{ result: string }>;
    getImageGenConfig?: () => Promise<import("../../api/client").ImageGenConfig>;
    setImageGenConfig?: (cfg: import("../../api/client").ImageGenConfig) => Promise<unknown>;
    getDiscoveryConfig?: () => Promise<import("../../api/client").DiscoveryConfig>;
    setDiscoveryConfig?: (cfg: import("../../api/client").DiscoveryConfig) => Promise<unknown>;
    getLocalModelsConfig?: () => Promise<Record<string, { enabled: boolean; max_parallel: number }>>;
    setLocalModelsConfig?: (models: Record<string, { enabled: boolean; max_parallel: number }>) => Promise<unknown>;
    syncLoginStart?: () => Promise<{ deviceCode: string; userCode: string; verifyUrl: string; expiresIn: number }>;
    syncLogout?: () => Promise<unknown>;
  };
  /** Current messages in the chat store (used by /export). */
  getMessages?: () => Message[];
  /** Current session ID (used by /export). */
  getSessionId?: () => string | null;
}

/** Dispatch a slash command to the appropriate handler. */
export async function dispatchCommand(
  cmd: string,
  ctx: CommandContext,
): Promise<CommandResult> {
  const trimmed = cmd.trim();
  const spaceIdx = trimmed.indexOf(" ");
  const commandName = spaceIdx >= 0 ? trimmed.slice(0, spaceIdx) : trimmed;
  const args = spaceIdx >= 0 ? trimmed.slice(spaceIdx + 1).trim() : "";

  switch (commandName) {
    // ── Frontend-only: return handled without async work ──
    case "/help":
      return { handled: true, messages: [helpMessage()] };

    // ── Frontend-handled with API calls ──
    case "/session":
      return handleSession(args, ctx);

    case "/ocr":
      return handleOcr(args, ctx);

    // ── Session export (server-side) ──
    case "/export":
      return handleExport(ctx);

    case "/export-claude":
      return handleExportClaude(ctx);

    // ── Session title ──
    case "/title":
      return handleTitle(args, ctx);

    // ── File edit history ──
    case "/undo":
      return handleUndo(ctx);

    case "/redo":
      return handleRedo(ctx);

    // ── Usage / init ──
    case "/usage":
      return handleUsage(args);

    case "/init":
      return handleInit();

    // ── Permissions ──
    case "/permissions":
      return handlePermissions();

    case "/yolo":
      return handleYolo(args);

    // ── Agent selection ──
    case "/agent":
      return handleAgent(args, ctx);

    // ── Plugins ──
    case "/plugin":
    case "/plugins":
      return handlePlugin(args);

    // ── Frontend-handled via API ──
    case "/mask":
      return handleMask(args, ctx);

    case "/compact":
      return handleCompact(ctx);

    case "/recap":
      return handleRecap(ctx);

    case "/share":
      return handleShare(ctx);

    case "/btw":
      return handleBtw(args, ctx);

    // ── Repo-analysis commands: server assembles the full prompt, client
    //    sends it verbatim through the normal chat pipeline (TUI parity) ──
    case "/standup":
    case "/changes":
    case "/review":
    case "/learn":
    case "/doc-sync":
      return handleCommandContext(commandName, args, ctx);

    // ── Config/state parity commands (TUI parity) ──
    case "/max-step":
    case "/max-steps":
      return handleMaxStep(args, ctx);

    case "/effort":
      return handleEffort(args, ctx);

    case "/thinking":
      return handleThinking();

    case "/models":
    case "/model":
      return handleModels(args, ctx);

    case "/goal":
      return handleGoal(args);

    case "/autocontinue":
      return handleAutoContinue(args, ctx);

    case "/mem":
      return handleMem(args, ctx);

    case "/paths":
      return handlePaths(ctx);

    case "/cd":
      return handleCd(args, ctx);

    case "/add-dir":
    case "/add-dirs":
      return handleAddDir(args, ctx);

    case "/localmodel":
      return handleLocalModel(args, ctx);

    case "/discover":
      return handleDiscover(args, ctx);

    case "/login":
      return handleLogin(ctx);

    case "/logout":
    case "/sync-logout":
      return handleLogout(ctx);

    case "/mcp-auth":
      return handleMcpAuth();

    case "/docs":
    case "/doc-mode":
      return handleDocs(args, ctx);

    case "/commands":
      return { handled: true, messages: [helpMessage()] };

    case "/ban":
      return handleBan(args, ctx);

    case "/image":
      return handleImage(args, ctx);

    case "/upload":
    case "/uploads":
      return handleUpload(args, ctx);

    case "/connect":
      return handleConnect(args, ctx);

    // ── Token budget / LSP status (assistant message) ──
    case "/context":
      return handleContext(ctx);

    case "/lsp":
      return handleLsp(ctx);

    // ── Status lists (assistant message) ──
    case "/agents":
      return handleAgents(ctx);

    case "/skills":
      return handleSkills(ctx);

    case "/mcp":
      return handleMcp(ctx);

    case "/cron":
      return handleCron(ctx);

    case "/github":
      return handleGithub(args, ctx);

    // ── Config routing (model dialog tabs) ──
    case "/small-model":
      return handleSmallModel(ctx);

    case "/advisor":
      return handleAdvisor(ctx);

    // ── Fall through to LLM (the agent may interpret them) ──
    case "/search":
    default:
      return { handled: false };
  }
}

// ─── Individual handlers ───────────────────────────────────────────────────

async function handleSession(
  args: string,
  ctx: CommandContext,
): Promise<CommandResult> {
  const parts = args.split(/\s+/);
  const sub = parts[0]?.toLowerCase();

  // /session load <id>
  if (sub === "load" && parts[1]) {
    try {
      const session = await ctx.api.getSession(parts[1]);
      const message: Message = {
        role: "assistant",
        content: `Loaded session **${session.title || parts[1]}**.`,
      };
      return {
        handled: true,
        messages: [message],
        sessionId: parts[1],
      };
    } catch (err) {
      return {
        handled: true,
        messages: [{
          role: "assistant",
          content: `**Error loading session:** ${err instanceof Error ? err.message : String(err)}`,
        }],
      };
    }
  }

  // /session list (or bare /session)
  try {
    const sessions = await ctx.api.listSessions();
    if (sessions.length === 0) {
      return {
        handled: true,
        messages: [{
          role: "assistant",
          content: "No sessions yet. Start a new conversation to create one.",
        }],
      };
    }
    const lines = sessions.map(
      (s) => `- \`${s.id}\` — ${s.title || "(untitled)"}`,
    );
    const preview = sessions.length > 20
      ? `Showing latest 20 of ${sessions.length} sessions:\n${lines.slice(0, 20).join("\n")}`
      : lines.join("\n");
    return {
      handled: true,
      messages: [{
        role: "assistant",
        content: `## Sessions\n\n${preview}\n\nUse \`/session load <id>\` to open a session.`,
      }],
    };
  } catch (err) {
    return {
      handled: true,
      messages: [{
        role: "assistant",
        content: `**Error listing sessions:** ${err instanceof Error ? err.message : String(err)}`,
      }],
    };
  }
}

async function handleOcr(
  args: string,
  ctx: CommandContext,
): Promise<CommandResult> {
  const trimmed = args.trim();
  const spaceIdx = trimmed.indexOf(" ");
  const sub = (spaceIdx >= 0 ? trimmed.slice(0, spaceIdx) : trimmed).toLowerCase();
  const rest = spaceIdx >= 0 ? trimmed.slice(spaceIdx + 1).trim() : "";

  if (sub === "" || sub === "status") {
    try {
      const cfg = await ctx.api.getOcrConfig();
      const backend = cfg.backend || "openai-compat";
      const modelName = backend === "paddle"
        ? cfg.paddle.variant
        : cfg.openai.model;
      return {
        handled: true,
        messages: [{
          role: "assistant",
          content: `**OCR status:** ${cfg.enabled ? "✅ enabled" : "❌ disabled"}\n**Backend:** ${backend}\n**Model:** ${modelName || "(not set)"}`,
        }],
      };
    } catch (err) {
      return {
        handled: true,
        messages: [{
          role: "assistant",
          content: `**Error fetching OCR status:** ${err instanceof Error ? err.message : String(err)}`,
        }],
      };
    }
  }

  if (sub === "on" || sub === "enable" || sub === "true" || sub === "yes") {
    try {
      const cfg = await ctx.api.getOcrConfig();
      cfg.enabled = true;
      await ctx.api.setOcrConfig(cfg);
      return {
        handled: true,
        messages: [{ role: "assistant", content: "OCR: **enabled.**" }],
      };
    } catch (err) {
      return {
        handled: true,
        messages: [{
          role: "assistant",
          content: `**Error enabling OCR:** ${err instanceof Error ? err.message : String(err)}`,
        }],
      };
    }
  }

  if (sub === "off" || sub === "disable" || sub === "false" || sub === "no") {
    try {
      const cfg = await ctx.api.getOcrConfig();
      cfg.enabled = false;
      await ctx.api.setOcrConfig(cfg);
      return {
        handled: true,
        messages: [{ role: "assistant", content: "OCR: **disabled.**" }],
      };
    } catch (err) {
      return {
        handled: true,
        messages: [{
          role: "assistant",
          content: `**Error disabling OCR:** ${err instanceof Error ? err.message : String(err)}`,
        }],
      };
    }
  }

  // /ocr model [backend/model | modelName]
  if (sub === "model") {
    const modelArg = rest;
    if (!modelArg) {
      // No arg — show available models
      try {
        const modelsResp = await ctx.api.getOcrModels();
        const lines: string[] = ["**Available OCR models:**"];
        for (const be of modelsResp.backends) {
          lines.push(`\n**${be.name}**`);
          if (be.error) {
            lines.push(`  ⚠️ unavailable: ${be.error}`);
            continue;
          }
          if (be.models.length === 0) {
            lines.push("  _(no models)_");
            continue;
          }
          for (const m of be.models) {
            lines.push(`  • \`${m}\``);
          }
        }
        lines.push("\nUse \`/ocr model <backend>/<name>\` to select one.");
        return { handled: true, messages: [{ role: "assistant", content: lines.join("\n") }] };
      } catch {
        return { handled: true, messages: [{ role: "assistant", content: "Could not fetch OCR models. Is the backend running?" }] };
      }
    }

    // Parse "backend/model" format
    let backend = "openai-compat";
    let modelName = modelArg;
    if (modelArg.includes("/")) {
      const parts = modelArg.split("/", 2);
      backend = parts[0];
      modelName = parts[1];
    }

    try {
      const cfg = await ctx.api.getOcrConfig();
      cfg.backend = backend as "openai-compat" | "paddle" | "lmstudio";
      if (backend === "paddle") {
        cfg.paddle.variant = modelName;
      } else {
        cfg.openai.model = modelName;
      }
      await ctx.api.setOcrConfig(cfg);
      return {
        handled: true,
        messages: [{
          role: "assistant",
          content: `OCR model set to **${backend}/${modelName}**.`,
        }],
      };
    } catch (err) {
      return {
        handled: true,
        messages: [{
          role: "assistant",
          content: `**Error setting OCR model:** ${err instanceof Error ? err.message : String(err)}`,
        }],
      };
    }
  }

  return {
    handled: true,
    messages: [{
      role: "assistant",
      content: "Usage: \`/ocr [status\\|enable\\|disable\\|model [<backend>/]<name>]\`",
    }],
  };
}

async function handleExport(ctx: CommandContext): Promise<CommandResult> {
  const sessionId = ctx.getSessionId?.();
  if (!sessionId) {
    return {
      handled: true,
      messages: [{ role: "assistant", content: "No active session to export." }],
    };
  }

  try {
    const markdown = await api.exportSessionMarkdown(sessionId);
    return {
      handled: true,
      messages: [{ role: "assistant", content: "Exported session as Markdown." }],
      download: {
        filename: `ocode_export_${sessionId}.md`,
        content: markdown,
        mimeType: "text/markdown;charset=utf-8",
      },
    };
  } catch (err) {
    return errorMessage("Export failed", err);
  }
}

async function handleExportClaude(ctx: CommandContext): Promise<CommandResult> {
  const sessionId = ctx.getSessionId?.();
  if (!sessionId) {
    return {
      handled: true,
      messages: [{ role: "assistant", content: "No active session to export." }],
    };
  }

  try {
    const { path } = await api.exportClaudeSession(sessionId);
    return {
      handled: true,
      messages: [{
        role: "assistant",
        content: `Appended session to Claude history:\n\`${path}\``,
      }],
    };
  } catch (err) {
    return errorMessage("Claude export failed", err);
  }
}

async function handleTitle(args: string, ctx: CommandContext): Promise<CommandResult> {
  const title = args.trim();
  if (!title) {
    return {
      handled: true,
      messages: [{
        role: "assistant",
        content: "Usage: `/title <text>` — set a title for the current session.",
      }],
    };
  }

  const sessionId = ctx.getSessionId?.();
  if (!sessionId) {
    return {
      handled: true,
      messages: [{ role: "assistant", content: "No active session to title." }],
    };
  }

  try {
    await api.setSessionTitle(sessionId, title);
    return {
      handled: true,
      messages: [{ role: "assistant", content: `Session title set to **${title}**.` }],
    };
  } catch (err) {
    return errorMessage("Failed to set title", err);
  }
}

async function handleUndo(ctx: CommandContext): Promise<CommandResult> {
  try {
    const res = await api.undoFileChange(ctx.getSessionId?.() ?? undefined);
    return {
      handled: true,
      messages: [{ role: "assistant", content: `Undid last change to \`${res.path}\`.` }],
    };
  } catch (err) {
    return errorMessage("Undo failed", err);
  }
}

async function handleRedo(ctx: CommandContext): Promise<CommandResult> {
  try {
    const res = await api.redoFileChange(ctx.getSessionId?.() ?? undefined);
    return {
      handled: true,
      messages: [{ role: "assistant", content: `Redid change to \`${res.path}\`.` }],
    };
  } catch (err) {
    return errorMessage("Redo failed", err);
  }
}

async function handleUsage(args: string): Promise<CommandResult> {
  const range = args.trim() || undefined;
  try {
    const summary = await api.getUsage(range);
    return {
      handled: true,
      messages: [{ role: "assistant", content: formatUsage(summary) }],
    };
  } catch (err) {
    return errorMessage("Failed to fetch usage", err);
  }
}

async function handleInit(): Promise<CommandResult> {
  try {
    const res = await api.initProject();
    const verb = res.status === "created" ? "Created" : "Found existing";
    return {
      handled: true,
      messages: [{ role: "assistant", content: `${verb} \`${res.path}\`.` }],
    };
  } catch (err) {
    return errorMessage("Init failed", err);
  }
}

async function handlePermissions(): Promise<CommandResult> {
  try {
    const p = await api.getPermissions();
    return {
      handled: true,
      messages: [{ role: "assistant", content: formatPermissions(p) }],
    };
  } catch (err) {
    return errorMessage("Failed to fetch permissions", err);
  }
}

async function handleYolo(args: string): Promise<CommandResult> {
  const sub = args.trim().toLowerCase();

  try {
    if (sub === "" || sub === "status") {
      const { yolo } = await api.getYolo();
      return {
        handled: true,
        messages: [{
          role: "assistant",
          content: `**YOLO mode:** ${yolo ? "on (tools auto-approved)" : "off"}`,
        }],
      };
    }
    if (sub === "on" || sub === "enable" || sub === "true") {
      await api.setYolo(true);
      return {
        handled: true,
        messages: [{ role: "assistant", content: "YOLO mode: **on** — tools are auto-approved." }],
      };
    }
    if (sub === "off" || sub === "disable" || sub === "false") {
      await api.setYolo(false);
      return {
        handled: true,
        messages: [{ role: "assistant", content: "YOLO mode: **off**." }],
      };
    }
    return {
      handled: true,
      messages: [{ role: "assistant", content: "Usage: `/yolo [on\\|off\\|status]`" }],
    };
  } catch (err) {
    return errorMessage("YOLO command failed", err);
  }
}

async function handleAgent(args: string, ctx: CommandContext): Promise<CommandResult> {
  const name = args.trim();
  try {
    if (!name) {
      const agents = await api.listAgents();
      const lines = agents
        .slice()
        .sort((a, b) => a.name.localeCompare(b.name))
        .map((a) => `- **${a.name}** — ${a.description || "(no description)"}`);
      return {
        handled: true,
        messages: [{
          role: "assistant",
          content: `## Agents\n\n${lines.join("\n")}\n\nUse \`/agent <name>\` to switch.`,
        }],
      };
    }

    const sessionId = ctx.getSessionId?.() ?? undefined;
    const res = await api.setAgent(name, sessionId);
    return {
      handled: true,
      messages: [{
        role: "assistant",
        content: `Switched to agent **${res.name}** — ${res.description || ""}`.trim(),
      }],
    };
  } catch (err) {
    return errorMessage("Agent command failed", err);
  }
}

async function handlePlugin(args: string): Promise<CommandResult> {
  const trimmed = args.trim();
  const spaceIdx = trimmed.indexOf(" ");
  const sub = (spaceIdx >= 0 ? trimmed.slice(0, spaceIdx) : trimmed).toLowerCase();
  const rest = spaceIdx >= 0 ? trimmed.slice(spaceIdx + 1).trim() : "";

  try {
    if (sub === "" || sub === "list") {
      const [plugins, enabled] = await Promise.all([
        api.listPlugins(),
        api.getPluginsEnabledConfig(),
      ]);
      const builtinLines = [
        `- **ast** ${enabled.ast ? "✅" : "❌"} — ast-grep structural search/rewrite`,
      ];
      const installedLines = plugins.map(
        (p) => `- **${p.name}** ${p.enabled ? "✅" : "❌"} — ${p.description || p.source || "(no description)"}`,
      );
      const installedSection =
        plugins.length === 0
          ? "\n\n_No installed plugins — use `/plugin install <source>` to add one._"
          : `\n\n**Installed plugins:**\n${installedLines.join("\n")}`;
      return {
        handled: true,
        messages: [{
          role: "assistant",
          content: `## Plugins\n\n**Builtin plugins:**\n${builtinLines.join("\n")}${installedSection}\n\n\`/plugin enable\\|disable <name>\`, \`/plugin install <source>\`, \`/plugin remove <name>\``,
        }],
      };
    }

    if ((sub === "enable" || sub === "disable") && rest) {
      // Builtin ast is gated via plugins-enabled config, not external_plugins.
      if (rest.toLowerCase() === "ast") {
        await api.setPluginsEnabledConfig(sub === "enable");
        return {
          handled: true,
          messages: [{ role: "assistant", content: `Plugin **ast**: ${sub === "enable" ? "enabled" : "disabled"}.` }],
        };
      }
      const res = await api.setPluginEnabled(rest, sub === "enable");
      return {
        handled: true,
        messages: [{ role: "assistant", content: `Plugin **${res.name}**: ${res.status}.` }],
      };
    }

    if (sub === "install" && rest) {
      const res = await api.installPlugin(rest);
      return {
        handled: true,
        messages: [{ role: "assistant", content: `Installed plugin **${res.name}** from \`${res.source}\`.` }],
      };
    }

    if (sub === "remove" && rest) {
      await api.removePlugin(rest);
      return {
        handled: true,
        messages: [{ role: "assistant", content: `Removed plugin **${rest}**.` }],
      };
    }

    return {
      handled: true,
      messages: [{
        role: "assistant",
        content: "Usage: `/plugin [list\\|enable <name>\\|disable <name>\\|install <source>\\|remove <name>]`",
      }],
    };
  } catch (err) {
    return errorMessage("Plugin command failed", err);
  }
}

// ─── Rendering helpers ───────────────────────────────────────────────────────

function errorMessage(prefix: string, err: unknown): CommandResult {
  return {
    handled: true,
    messages: [{
      role: "assistant",
      content: `**${prefix}:** ${err instanceof Error ? err.message : String(err)}`,
    }],
  };
}

// cacheRate returns the cache hit rate as a percentage of (prompt + cache
// read) tokens, i.e. how much of the input context was served from cache
// rather than sent fresh.
function cacheRate(promptTokens: number, cacheReadTokens: number): string {
  const denom = promptTokens + cacheReadTokens;
  if (denom <= 0) return "0.0%";
  return `${((cacheReadTokens / denom) * 100).toFixed(1)}%`;
}

function formatUsage(s: UsageSummary): string {
  const lines: string[] = ["## Token Usage"];
  lines.push("");
  lines.push(`**Requests:** ${s.total_requests.toLocaleString()}  `);
  lines.push(`**Total tokens:** ${s.total_tokens.toLocaleString()}  `);
  lines.push(
    `**Cache rate:** ${cacheRate(s.total_prompt_tokens, s.total_cache_read_tokens)}  `,
  );
  lines.push(`**Spend:** $${s.total_spend.toFixed(4)}`);
  lines.push("");

  if (s.by_model.length > 0) {
    lines.push("| Model | Requests | Prompt | Completion | Cache read | Cache % | Total | Spend |");
    lines.push("|---|--:|--:|--:|--:|--:|--:|--:|");
    for (const m of s.by_model) {
      lines.push(
        `| ${m.model} | ${m.request_count.toLocaleString()} | ${m.prompt_tokens.toLocaleString()} | ${m.completion_tokens.toLocaleString()} | ${m.cache_read_tokens.toLocaleString()} | ${cacheRate(m.prompt_tokens, m.cache_read_tokens)} | ${m.total_tokens.toLocaleString()} | $${m.spend.toFixed(4)} |`,
      );
    }
  }
  return lines.join("\n");
}

function formatPermissions(p: PermissionsResponse): string {
  const lines: string[] = ["## Permissions"];
  lines.push("");
  lines.push(`**Mode:** ${p.mode}  `);
  lines.push(`**Auto-allow:** ${p.auto_allow ? "on" : "off"}`);
  lines.push("");

  if (p.rules.length > 0) {
    lines.push("**Tool rules**");
    lines.push("");
    lines.push("| Tool | Level |");
    lines.push("|---|---|");
    for (const r of p.rules) {
      lines.push(`| \`${r.tool}\` | ${r.level} |`);
    }
    lines.push("");
  }

  if (p.bash_rules.length > 0) {
    lines.push("**Bash prefix rules**");
    lines.push("");
    lines.push("| Prefix | Level |");
    lines.push("|---|---|");
    for (const r of p.bash_rules) {
      lines.push(`| \`${r.tool}\` | ${r.level} |`);
    }
  }

  if (p.rules.length === 0 && p.bash_rules.length === 0) {
    lines.push("_No explicit rules configured._");
  }
  return lines.join("\n");
}

async function handleMask(args: string, ctx: CommandContext): Promise<CommandResult> {
  const sub = args.toLowerCase();

  if (sub === "" || sub === "status") {
    try {
      const mask = await ctx.api.getMaskConfig();
      const modeDesc = mask.mode === "full" ? "full (scans every message)" : "lenient (scans on keyword match)";
      return {
        handled: true,
        messages: [{
          role: "assistant",
          content: `**Secret redaction:** ${mask.enabled ? "✅ enabled" : "❌ disabled"}\n**Mode:** ${modeDesc}\n**Tier-2 model:** ${mask.model || "(not set)"}`,
        }],
      };
    } catch (err) {
      return {
        handled: true,
        messages: [{
          role: "assistant",
          content: `**Error fetching mask status:** ${err instanceof Error ? err.message : String(err)}`,
        }],
      };
    }
  }

  if (sub === "on" || sub === "enable" || sub === "true" || sub === "yes") {
    try {
      await ctx.api.setMaskEnabled(true);
      return {
        handled: true,
        messages: [{ role: "assistant", content: "Secret redaction: **enabled.**" }],
      };
    } catch (err) {
      return {
        handled: true,
        messages: [{
          role: "assistant",
          content: `**Error enabling redaction:** ${err instanceof Error ? err.message : String(err)}`,
        }],
      };
    }
  }

  if (sub === "off" || sub === "disable" || sub === "false" || sub === "no") {
    try {
      await ctx.api.setMaskEnabled(false);
      return {
        handled: true,
        messages: [{ role: "assistant", content: "Secret redaction: **disabled.**" }],
      };
    } catch (err) {
      return {
        handled: true,
        messages: [{
          role: "assistant",
          content: `**Error disabling redaction:** ${err instanceof Error ? err.message : String(err)}`,
        }],
      };
    }
  }

  if (sub.startsWith("mode ")) {
    const mode = sub.slice(5).trim();
    if (mode === "lenient" || mode === "full") {
      try {
        await ctx.api.setMaskMode(mode);
        return {
          handled: true,
          messages: [{ role: "assistant", content: `Scan mode set to **${mode}**.` }],
        };
      } catch (err) {
        return {
          handled: true,
          messages: [{
            role: "assistant",
            content: `**Error setting mode:** ${err instanceof Error ? err.message : String(err)}`,
          }],
        };
      }
    }
  }

  if (sub.startsWith("model ")) {
    const modelName = sub.slice(6).trim();
    if (modelName) {
      try {
        await ctx.api.setMaskModel(modelName);
        return {
          handled: true,
          messages: [{ role: "assistant", content: `Tier-2 model set to **${modelName}**.` }],
        };
      } catch (err) {
        return {
          handled: true,
          messages: [{
            role: "assistant",
            content: `**Error setting model:** ${err instanceof Error ? err.message : String(err)}`,
          }],
        };
      }
    }
  }

  if (sub === "mode") {
    try {
      const mask = await ctx.api.getMaskConfig();
      return {
        handled: true,
        messages: [{
          role: "assistant",
          content: `Current mode: **${mask.mode}**\n\n• \`lenient\` — LLM scans only when input contains a sensitive keyword or value pattern\n• \`full\` — LLM scans every message`,
        }],
      };
    } catch (err) {
      return {
        handled: true,
        messages: [{
          role: "assistant",
          content: `**Error fetching mask config:** ${err instanceof Error ? err.message : String(err)}`,
        }],
      };
    }
  }

  return {
    handled: true,
    messages: [{
      role: "assistant",
      content: "Usage: `/mask [status\\|on\\|off\\|mode [lenient\\|full]\\|model <name>]`",
    }],
  };
}

async function handleCompact(ctx: CommandContext): Promise<CommandResult> {
  const sessionId = ctx.getSessionId?.();
  if (!sessionId) {
    return {
      handled: true,
      messages: [{ role: "assistant", content: "No active session to compact." }],
    };
  }

  try {
    const result = await ctx.api.compactSession(sessionId);
    return {
      handled: true,
      messages: [{
        role: "assistant",
        content: `Compacted: **${result.original_len} → ${result.compacted_len}** messages.`,
      }],
    };
  } catch (err) {
    return {
      handled: true,
      messages: [{
        role: "assistant",
        content: `**Compaction failed:** ${err instanceof Error ? err.message : String(err)}`,
      }],
    };
  }
}

async function handleRecap(ctx: CommandContext): Promise<CommandResult> {
  const sessionId = ctx.getSessionId?.();
  if (!sessionId) {
    return {
      handled: true,
      messages: [{ role: "assistant", content: "No active session to recap." }],
    };
  }

  try {
    const result = await ctx.api.recapSession(sessionId);
    return {
      handled: true,
      messages: [{
        role: "assistant",
        content: `## Recap\n\n${result.recap}`,
      }],
    };
  } catch (err) {
    return {
      handled: true,
      messages: [{
        role: "assistant",
        content: `**Recap failed:** ${err instanceof Error ? err.message : String(err)}`,
      }],
    };
  }
}

async function handleShare(ctx: CommandContext): Promise<CommandResult> {
  const sessionId = ctx.getSessionId?.();
  if (!sessionId) {
    return {
      handled: true,
      messages: [{ role: "assistant", content: "No active session to share." }],
    };
  }

  try {
    const result = await ctx.api.shareSession(sessionId);
    return {
      handled: true,
      messages: [{
        role: "assistant",
        content: result.markdown,
      }],
    };
  } catch (err) {
    return {
      handled: true,
      messages: [{
        role: "assistant",
        content: `**Share failed:** ${err instanceof Error ? err.message : String(err)}`,
      }],
    };
  }
}


async function handleBtw(args: string, ctx: CommandContext): Promise<CommandResult> {
  if (!args) {
    return {
      handled: true,
      messages: [{
        role: "assistant",
        content: "Usage: `/btw <message>` — add a quick aside to the conversation.",
      }],
    };
  }

  const sessionId = ctx.getSessionId?.();
  if (!sessionId) {
    return {
      handled: true,
      messages: [{ role: "assistant", content: "No active session to add a note to." }],
    };
  }

  try {
    await ctx.api.btwSession(sessionId, args);
    return {
      handled: true,
      messages: [{
        role: "assistant",
        content: `Noted: ${args}`,
      }],
    };
  } catch (err) {
    return {
      handled: true,
      messages: [{
        role: "assistant",
        content: `**BTW failed:** ${err instanceof Error ? err.message : String(err)}`,
      }],
    };
  }
}

// ─── Repo-analysis commands (/standup, /changes, /review) ──────────────────

/**
 * Server-side prompt assembly. The server gathers the same git context the
 * TUI uses (recent commits, pending diffs, LSP, spec files) and returns the
 * full LLM prompt; the client sends it verbatim through the normal send path
 * so the web behaves exactly like the TUI /standup | /changes | /review.
 */
async function handleCommandContext(
  commandName: string,
  args: string,
  ctx: CommandContext,
): Promise<CommandResult> {
  const name = commandName.slice(1); // strip leading "/"
  try {
    const { prompt } = await ctx.api.getCommandContext(name, args || undefined);
    return { handled: true, prompt };
  } catch (err) {
    return {
      handled: true,
      messages: [{
        role: "assistant",
        content: `**${commandName} failed:** ${err instanceof Error ? err.message : String(err)}`,
      }],
    };
  }
}

// ─── Slash-command parity handlers (TUI parity for web/desktop) ───────────

/** errText normalizes an unknown thrown value into a display string. */
function errText(err: unknown): string {
  return err instanceof Error ? err.message : String(err);
}

/** unsupported builds the standard "method missing" guard result. */
function unsupported(what: string): CommandResult {
  return {
    handled: true,
    messages: [{
      role: "assistant",
      content: `**${what}** is not available — the server does not expose the required endpoint (is ocode up to date?).`,
    }],
  };
}

const EFFORT_LEVELS: Record<string, number> = {
  off: 0, none: 0, "0": 0,
  low: 1024, "1024": 1024,
  med: 8000, medium: 8000, "8000": 8000,
  high: 16000, "16000": 16000,
  xhigh: 32000, "32000": 32000,
  max: 65536, "65536": 65536,
};

// ─── /max-step [n] — per-turn step cap ─────────────────────────────────────

async function handleMaxStep(args: string, ctx: CommandContext): Promise<CommandResult> {
  if (!ctx.api.getLimitsConfig || !ctx.api.setLimitsConfig) return unsupported("/max-step");
  try {
    const cur = await ctx.api.getLimitsConfig();
    const trimmed = args.trim();
    if (!trimmed) {
      return ok(`**Max steps:** ${cur.max_steps === 0 ? "unlimited (hard cap applies)" : cur.max_steps}\n\nUse \`/max-step <n>\` to change it (0 = unlimited).`);
    }
    const n = Number.parseInt(trimmed, 10);
    if (!Number.isFinite(n) || n < 0) {
      return ok("Usage: `/max-step <n>` where n is a non-negative integer (0 = unlimited).");
    }
    await ctx.api.setLimitsConfig({ ...cur, max_steps: n });
    return ok(`**Max steps:** set to ${n === 0 ? "unlimited (hard cap applies)" : n}.`);
  } catch (err) {
    return fail("/max-step", err);
  }
}

// ─── /effort [level] — reasoning effort ────────────────────────────────────

async function handleEffort(args: string, ctx: CommandContext): Promise<CommandResult> {
  if (!ctx.api.getThinkingBudget || !ctx.api.setThinkingBudget) return unsupported("/effort");
  try {
    const cur = await ctx.api.getThinkingBudget();
    const key = args.trim().toLowerCase();
    if (!key) {
      return ok([
        `**Reasoning effort:** ${cur.budget === 0 ? "off" : cur.level} (${cur.budget.toLocaleString()} tokens)`,
        "",
        `Levels: ${cur.levels.map((l) => `\`${l.level}\``).join(" · ")}`,
        "Example: `/effort high`",
      ].join("\n"));
    }
    const known = cur.levels.find((l) => l.level === key || String(l.budget) === key);
    if (!known && !(key in EFFORT_LEVELS)) {
      return ok(`Unknown level \`${key}\`. Use ${cur.levels.map((l) => l.level).join(" | ")}.`);
    }
    const level = known?.level ?? key;
    const r = await ctx.api.setThinkingBudget(level);
    const budget = (r as { budget?: number })?.budget ?? EFFORT_LEVELS[level] ?? 0;
    return ok(`**Reasoning effort:** ${budget === 0 ? "off" : level} (${budget.toLocaleString()} tokens).`);
  } catch (err) {
    return fail("/effort", err);
  }
}

// ─── /thinking — visibility note ───────────────────────────────────────────

function handleThinking(): CommandResult {
  return ok("Web/desktop always renders thinking blocks as they stream in — there is no separate toggle. Use `/effort` to control how much thinking budget the model gets.");
}

// ─── /models [name] — model switch ─────────────────────────────────────────

async function handleModels(args: string, ctx: CommandContext): Promise<CommandResult> {
  const name = args.trim();
  if (!name) {
    // No-arg mirrors the TUI's model picker; on web that is the header dialog.
    return { handled: true, openModelPicker: true };
  }
  if (!ctx.api.setConfigModel) return unsupported("/models");
  try {
    if (ctx.api.listModels) {
      const models = await ctx.api.listModels();
      const match = models.find(
        (m) => m.model.toLowerCase() === name.toLowerCase() || m.name.toLowerCase() === name.toLowerCase(),
      );
      if (!match && models.length > 0) {
        const suggestions = models
          .filter(
            (m) =>
              m.model.toLowerCase().includes(name.toLowerCase()) ||
              m.name.toLowerCase().includes(name.toLowerCase()),
          )
          .slice(0, 8)
          .map((m) => `\`${m.model}\``);
        return ok(
          suggestions.length > 0
            ? `No exact model \`${name}\`. Did you mean:\n${suggestions.join("\n")}`
            : `No model matching \`${name}\` found (${models.length} known models).`,
        );
      }
    }
    await ctx.api.setConfigModel(name);
    return ok(`**Model:** switched to \`${name}\`. Applies to this and new sessions.`);
  } catch (err) {
    return fail("/models", err);
  }
}

// ─── /goal <text> — orchestrator dispatch ──────────────────────────────────

async function handleGoal(args: string): Promise<CommandResult> {
  const goal = args.trim();
  if (!goal) {
    return ok("Usage: `/goal <what to accomplish>` — dispatches the goal to the orchestrator agent (plan → implement → validate pipeline).");
  }
  // The TUI rewrites /goal to an /orchestrator agent switch + turn. The web
  // send path cannot switch the session agent mid-tab today, so we frame the
  // goal as an explicit orchestrator-pipeline request to the active agent.
  const prompt = [
    "Act as the orchestrator agent running its full pipeline (plan, implement, validate, iterate) for the following goal.",
    "Dispatch sub-agents via the task tool as the orchestrator workflow prescribes, and verify completion independently before reporting.",
    "",
    `Goal: ${goal}`,
  ].join("\n");
  return { handled: true, prompt };
}

// ─── /autocontinue [on|off|status|model [name]] ────────────────────────────

async function handleAutoContinue(args: string, ctx: CommandContext): Promise<CommandResult> {
  if (!ctx.api.getAutoContinue || !ctx.api.setAutoContinue) return unsupported("/autocontinue");
  try {
    const parts = args.trim().split(/\s+/).filter(Boolean);
    const sub = parts[0]?.toLowerCase();
    if (!sub || sub === "status") {
      const cur = await ctx.api.getAutoContinue();
      const model = cur.model ? `\`${cur.model}\`` : "(none — StepLimitHit only)";
      return ok(`**Auto-continue:** ${cur.enabled ? "enabled" : "disabled"}\n**Judge model:** ${model}`);
    }
    if (["on", "true", "yes", "enable"].includes(sub)) {
      const r = await ctx.api.setAutoContinue({ enabled: true });
      return ok(`**Auto-continue:** enabled. Judge model: ${r.model ? `\`${r.model}\`` : "(none)"}`);
    }
    if (["off", "false", "no", "disable"].includes(sub)) {
      await ctx.api.setAutoContinue({ enabled: false });
      return ok("**Auto-continue:** disabled.");
    }
    if (sub === "model") {
      const target = parts.slice(1).join(" ").trim();
      if (!target) {
        return ok("Usage: `/autocontinue model <name>` (or `model auto` to clear). The interactive picker is TUI-only.");
      }
      if (["auto", "none", "off"].includes(target.toLowerCase())) {
        const r = await ctx.api.setAutoContinue({ clear: true });
        return ok(`**Auto-continue judge model cleared** — falling back to StepLimitHit only. Enabled: ${r.enabled}.`);
      }
      const r = await ctx.api.setAutoContinue({ model: target });
      return ok(`**Judge model:** \`${r.model}\`.`);
    }
    return ok("Usage: `/autocontinue [on|off|status|model [name]]`.");
  } catch (err) {
    return fail("/autocontinue", err);
  }
}

// ─── /mem [on|off|status|update ...] ───────────────────────────────────────

function renderMemStatus(s: import("../../api/types").MemoryStatusResponse): string {
  const scopeLine = (label: string, sc: { path: string; present: boolean; preview: string }) => {
    const state = !sc.present ? "**not created**" : sc.preview.trim() ? "present" : "**empty**";
    return `- **${label}:** ${sc.path} — ${state}`;
  };
  return [
    "## Memory Status",
    `- **Memory context:** ${s.enabled ? "enabled" : "disabled"}`,
    scopeLine("User preferences", s.scopes.user),
    scopeLine("Project memory", s.scopes.project),
    scopeLine("Global history", s.scopes.global),
    "",
    "`/mem update [user|project|global] [focus]` refreshes a scope with durable knowledge.",
  ].join("\n");
}

async function handleMem(args: string, ctx: CommandContext): Promise<CommandResult> {
  const parts = args.trim().split(/\s+/).filter(Boolean);
  const sub = parts[0]?.toLowerCase();
  if (!sub || sub === "status") {
    if (!ctx.api.getMemoryStatus) return unsupported("/mem status");
    try {
      return ok(renderMemStatus(await ctx.api.getMemoryStatus()));
    } catch (err) {
      return fail("/mem", err);
    }
  }
  if (sub === "on" || sub === "off") {
    if (!ctx.api.getFeaturesConfig || !ctx.api.setFeaturesConfig) return unsupported("/mem on|off");
    try {
      const cur = await ctx.api.getFeaturesConfig();
      await ctx.api.setFeaturesConfig(sub === "on", cur.doc_prompt_enabled);
      return ok(`**Memory context:** ${sub === "on" ? "enabled" : "disabled"}.`);
    } catch (err) {
      return fail("/mem", err);
    }
  }
  if (sub === "update") {
    // Same prompt path as the TUI: server builds the byte-identical prompt.
    return handleCommandContext("mem-update", parts.slice(1).join(" "), ctx);
  }
  return ok("Usage: `/mem [on|off|status|update [user|project|global] [focus]]`.");
}

// ─── /paths — path report ──────────────────────────────────────────────────

async function handlePaths(ctx: CommandContext): Promise<CommandResult> {
  if (!ctx.api.getPathsInfo) return unsupported("/paths");
  try {
    const info = await ctx.api.getPathsInfo();
    return ok(info.text);
  } catch (err) {
    return fail("/paths", err);
  }
}

// ─── /cd <path> — project root ─────────────────────────────────────────────

async function handleCd(args: string, ctx: CommandContext): Promise<CommandResult> {
  const path = args.trim();
  if (!path) {
    return ok("Usage: `/cd <path>` — adds the directory to projects and focuses it. Web sessions are per-project: open the project from the sidebar after adding.");
  }
  if (!ctx.api.addProject) return unsupported("/cd");
  try {
    await ctx.api.addProject(path);
    return ok(`Added **${path}** to projects. Open it from the sidebar to start sessions rooted there (web/desktop sessions follow their project root — no process \`chdir\`).`);
  } catch (err) {
    return fail("/cd", err);
  }
}

// ─── /add-dir [path] — extra allowed paths ─────────────────────────────────

async function handleAddDir(args: string, ctx: CommandContext): Promise<CommandResult> {
  if (!ctx.api.getPathsConfig || !ctx.api.setPathsConfig) return unsupported("/add-dir");
  try {
    const cfg = await ctx.api.getPathsConfig();
    const add = args.trim();
    if (!add) {
      const lines = cfg.extra_allowed_paths.length
        ? cfg.extra_allowed_paths.map((p) => `- \`${p}\``)
        : ["(none)"];
      return ok([`**Extra allowed paths:**`, ...lines, "", "`/add-dir <path>` to add one."].join("\n"));
    }
    if (cfg.extra_allowed_paths.includes(add)) {
      return ok(`\`${add}\` is already in the extra allowed paths.`);
    }
    await ctx.api.setPathsConfig([...cfg.extra_allowed_paths, add], cfg.upload_dir);
    return ok(`Added **${add}** to the extra allowed paths (applies immediately to tools run by the server).`);
  } catch (err) {
    return fail("/add-dir", err);
  }
}

// ─── /localmodel [...] — local model registry ──────────────────────────────

async function handleLocalModel(args: string, ctx: CommandContext): Promise<CommandResult> {
  if (!ctx.api.getLocalModelsConfig || !ctx.api.setLocalModelsConfig) return unsupported("/localmodel");
  try {
    const models = await ctx.api.getLocalModelsConfig();
    const names = Object.keys(models).sort();
    const parts = args.trim().split(/\s+/).filter(Boolean);
    const sub = parts[0]?.toLowerCase();

    const renderList = () =>
      names.length
        ? names.map((n) => `- **${n}** — ${models[n].enabled ? "enabled" : "disabled"}, max_parallel=${models[n].max_parallel}`).join("\n")
        : "(no local models registered)";

    if (!sub || sub === "status" || sub === "list") {
      return ok([
        "## Local Models",
        renderList(),
        "",
        "Instances start automatically on first use and are stopped when idle — start/stop is managed by the server, no manual command needed.",
        "Subcommands: `add <name>` · `enable <name…>` · `disable <name…>` · `limit <name> <1|2>`",
      ].join("\n"));
    }
    if (sub === "add" || sub === "enable" || sub === "disable") {
      const targets = parts.slice(1);
      if (targets.length === 0) {
        return ok(`Usage: \`/localmodel ${sub} <name…>\`. The interactive catalog picker is TUI-only.`);
      }
      const next: Record<string, { enabled: boolean; max_parallel: number }> = { ...models };
      for (const t of targets) {
        next[t] = {
          enabled: sub === "disable" ? false : true,
          max_parallel: next[t]?.max_parallel ?? 1,
        };
      }
      await ctx.api.setLocalModelsConfig(next);
      return ok(`${sub === "add" ? "Registered" : sub === "enable" ? "Enabled" : "Disabled"}: ${targets.map((t) => `\`${t}\``).join(", ")}.`);
    }
    if (sub === "limit") {
      const [, name, lvl] = parts;
      const parallel = Number.parseInt(lvl ?? "", 10);
      if (!name || !Number.isFinite(parallel) || parallel < 1) {
        return ok("Usage: `/localmodel limit <name> <1|2>`.");
      }
      if (!models[name]) return ok(`Unknown local model \`${name}\`.`);
      await ctx.api.setLocalModelsConfig({ ...models, [name]: { ...models[name], max_parallel: parallel } });
      return ok(`**${name}:** max_parallel=${parallel}. (Applies to instances started by this server.)`);
    }
    if (sub === "hf-token") {
      return ok("`hf-token` must be set in the config file or via the TUI (`/localmodel hf-token <token>`); the web API does not accept tokens.");
    }
    return ok("Usage: `/localmodel [list|status|add|enable|disable|limit] …`");
  } catch (err) {
    return fail("/localmodel", err);
  }
}

// ─── /discover [...] — codebase discovery ──────────────────────────────────

async function handleDiscover(args: string, ctx: CommandContext): Promise<CommandResult> {
  if (!ctx.api.getDiscoveryConfig || !ctx.api.setDiscoveryConfig) return unsupported("/discover");
  try {
    const cfg = await ctx.api.getDiscoveryConfig();
    const parts = args.trim().split(/\s+/).filter(Boolean);
    const sub = parts[0]?.toLowerCase();

    if (!sub || sub === "status") {
      return ok([
        "## Codebase Discovery",
        `- **Enabled:** ${cfg.enabled}`,
        `- **Embedding model:** ${cfg.embedding_model || "(default)"}`,
        `- **Backend:** ${cfg.embedding_backend || "-"}`,
        `- **Ignore paths:** ${cfg.ignore_paths.length ? cfg.ignore_paths.map((p) => `\`${p}\``).join(", ") : "(none)"}`,
      ].join("\n"));
    }
    if (sub === "enable" || sub === "disable") {
      await ctx.api.setDiscoveryConfig({ ...cfg, enabled: sub === "enable" });
      return ok(`**Discovery:** ${sub === "enable" ? "enabled" : "disabled"}.`);
    }
    if (sub === "model") {
      const target = parts.slice(1).join(" ").trim();
      if (!target) return ok("Usage: `/discover model <provider/model>`. The interactive picker is TUI-only.");
      await ctx.api.setDiscoveryConfig({ ...cfg, embedding_model: target });
      return ok(`**Embedding model:** \`${target}\`.`);
    }
    if (sub === "ignore") {
      const action = parts[1]?.toLowerCase();
      const rest = parts.slice(2).filter(Boolean);
      let paths = [...cfg.ignore_paths];
      if (!action || action === "list") {
        return ok(`**Ignored paths:** ${paths.length ? paths.map((p) => `\`${p}\``).join(", ") : "(none beyond built-in defaults)"}`);
      }
      if (action === "clear") {
        paths = [];
      } else if (action === "add" && rest.length) {
        for (const p of rest) if (!paths.includes(p)) paths.push(p);
      } else if ((action === "remove" || action === "rm") && rest.length) {
        paths = paths.filter((p) => !rest.includes(p));
      } else {
        return ok("Usage: `/discover ignore [add|remove|clear] [path…]`.");
      }
      await ctx.api.setDiscoveryConfig({ ...cfg, ignore_paths: paths });
      return ok(`**Ignored paths updated:** ${paths.length ? paths.map((p) => `\`${p}\``).join(", ") : "(none)"}`);
    }
    return ok("Usage: `/discover [enable|disable|status|model <id>|ignore …]`.");
  } catch (err) {
    return fail("/discover", err);
  }
}

// ─── /login & /logout — sync account ───────────────────────────────────────

async function handleLogin(ctx: CommandContext): Promise<CommandResult> {
  if (!ctx.api.syncLoginStart) return unsupported("/login");
  try {
    const r = await ctx.api.syncLoginStart();
    return ok([
      "## Device Login",
      `Open **${r.verifyUrl}** in your browser and approve the request.`,
      `Your code: \`${r.userCode}\``,
      "",
      "Once approved, config/auth sync links automatically (the code expires in " + Math.round(r.expiresIn / 60) + " minutes — rerun `/login` for a fresh one).",
    ].join("\n"));
  } catch (err) {
    return fail("/login", err);
  }
}

async function handleLogout(ctx: CommandContext): Promise<CommandResult> {
  if (!ctx.api.syncLogout) return unsupported("/logout");
  try {
    await ctx.api.syncLogout();
    return ok("Signed out of config sync; stored token revoked.");
  } catch (err) {
    return fail("/logout", err);
  }
}

// ─── /mcp-auth — browser OAuth gate ────────────────────────────────────────

function handleMcpAuth(): CommandResult {
  return ok("**/mcp-auth requires the desktop app or TUI**: MCP OAuth completes through a localhost redirect + system browser, which the browser-based client cannot host. Run `/mcp-auth <server>` inside the TUI/desktop shell.");
}

// ─── /docs [...] — knowledge system ────────────────────────────────────────

async function handleDocs(args: string, ctx: CommandContext): Promise<CommandResult> {
  const parts = args.trim().split(/\s+/).filter(Boolean);
  const sub = parts[0]?.toLowerCase();

  if (!sub || sub === "status") {
    if (!ctx.api.getDocsStatus) return unsupported("/docs status");
    try {
      return ok((await ctx.api.getDocsStatus()).text);
    } catch (err) {
      return fail("/docs", err);
    }
  }
  if (sub === "on" || sub === "off") {
    if (!ctx.api.getFeaturesConfig || !ctx.api.setFeaturesConfig) return unsupported("/docs on|off");
    try {
      const cur = await ctx.api.getFeaturesConfig();
      await ctx.api.setFeaturesConfig(cur.memory_enabled, sub === "on");
      return ok(sub === "on"
        ? "Documentation-first development prompt: **enabled**. Use `/docs init` to set up the OKF knowledge bundle."
        : "Documentation-first development prompt: **disabled**.");
    } catch (err) {
      return fail("/docs", err);
    }
  }
  if (sub === "init") {
    if (!ctx.api.docsInit) return unsupported("/docs init");
    try {
      const r = await ctx.api.docsInit();
      if (r.annotate_prompt) {
        // New bundle: mirror the TUI by dispatching the annotation pass as a
        // normal turn right after reporting init.
        return {
          handled: true,
          messages: [ok(r.result).messages![0]],
          prompt: r.annotate_prompt,
        };
      }
      return ok(r.result);
    } catch (err) {
      return fail("/docs", err);
    }
  }
  if (sub === "update") {
    if (!ctx.api.docsUpdate) return unsupported("/docs update");
    const sessionId = ctx.getSessionId?.();
    if (!sessionId) return ok("No active session — open a chat first so the maintenance pass has an agent to run on.");
    try {
      return ok((await ctx.api.docsUpdate(sessionId, parts.slice(1).join(" "))).result);
    } catch (err) {
      return fail("/docs", err);
    }
  }
  if (sub === "cleanup") {
    if (!ctx.api.docsCleanup) return unsupported("/docs cleanup");
    const confirm = parts.slice(1).some((a) => a === "--yes" || a === "-y");
    try {
      return ok((await ctx.api.docsCleanup(confirm)).result);
    } catch (err) {
      return fail("/docs", err);
    }
  }
  return ok("Usage: `/docs [on|off|status|init|update|cleanup [--yes]]`.");
}

// ─── /ban [list|add|remove|clear|<prefix>] — bash prefix rules ─────────────

async function handleBan(args: string, ctx: CommandContext): Promise<CommandResult> {
  if (!ctx.api.getPermissions) return unsupported("/ban");
  try {
    const perms = await ctx.api.getPermissions();
    const parts = args.trim().split(/\s+/).filter(Boolean);
    const sub = parts[0]?.toLowerCase();

    const renderRules = () => {
      const deny = perms.bash_rules.filter((r) => r.level === "deny");
      const other = perms.bash_rules.filter((r) => r.level !== "deny");
      const line = (r: { tool: string; level: string }) => `- \`${r.tool}\` — ${r.level}`;
      return [
        deny.length ? ["**Banned prefixes:**", ...deny.map(line)].join("\n") : "**Banned prefixes:** (none)",
        other.length ? [`\n**Other bash rules:**`, ...other.map(line)].join("\n") : "",
      ].filter(Boolean).join("\n");
    };

    if (!sub || sub === "list") return ok(renderRules());

    if (sub === "clear") {
      // Deliberate confirmation gate, mirroring the TUI's two-step /ban clear.
      return ok(
        perms.bash_rules.length === 0
          ? "No bash prefix rules to clear."
          : `${perms.bash_rules.length} rule(s):\n${renderRules()}\n\nBulk clear is confirm-gated on web too — remove individually with \`/ban remove <prefix…>\` (repeat as needed).`,
      );
    }
    if (sub === "add") {
      const prefixes = parts.slice(1).map((p) => p.toLowerCase());
      if (!prefixes.length || !ctx.api.setBashRule) return ok("Usage: `/ban add <prefix…>` (e.g. `/ban add rm git push`).");
      for (const p of prefixes) await ctx.api.setBashRule(p, "deny");
      return ok(`Banned: ${prefixes.map((p) => `\`${p}\``).join(", ")} — any bash command with these prefixes will be denied.`);
    }
    if (sub === "remove") {
      const prefixes = parts.slice(1).map((p) => p.toLowerCase());
      if (!prefixes.length || !ctx.api.setBashRule) return ok("Usage: `/ban remove <prefix…>` (sets the rule back to ask).");
      for (const p of prefixes) await ctx.api.setBashRule(p, "ask");
      return ok(`Un-banned (set to ask): ${prefixes.map((p) => `\`${p}\``).join(", ")}.`);
    }
    // Bare /ban <cmd…> falls through to add (TUI parity).
    return handleBan(`add ${args}`, ctx);
  } catch (err) {
    return fail("/ban", err);
  }
}

// ─── /image [...] — image generation ───────────────────────────────────────

async function handleImage(args: string, ctx: CommandContext): Promise<CommandResult> {
  if (!ctx.api.getImageGenConfig || !ctx.api.setImageGenConfig) return unsupported("/image");
  try {
    const cfg = await ctx.api.getImageGenConfig();
    const parts = args.trim().split(/\s+/).filter(Boolean);
    const sub = parts[0]?.toLowerCase();

    if (!sub || sub === "status") {
      return ok([
        "## Image Generation",
        `- **Enabled:** ${cfg.enabled}`,
        `- **Provider/model:** ${cfg.provider}/${cfg.model}`,
        `- **Timeout:** ${cfg.timeout ? `${cfg.timeout}s` : "(default)"}`,
      ].join("\n"));
    }
    if (sub === "enable" || sub === "disable") {
      await ctx.api.setImageGenConfig({ ...cfg, enabled: sub === "enable" });
      return ok(`**Image generation:** ${sub === "enable" ? "enabled" : "disabled"}.`);
    }
    if (sub === "model") {
      const target = parts.slice(1).join(" ").trim();
      if (!target) return ok("Usage: `/image model [provider/]model`. The interactive picker is TUI-only.");
      const [provider, model, ...rest] = target.split("/");
      const next = rest.length || !model
        ? { ...cfg, model: target }
        : { ...cfg, provider, model };
      await ctx.api.setImageGenConfig(next);
      return ok(`**Image model:** \`${next.provider}/${next.model}\`.`);
    }
    if (sub === "timeout") {
      const secs = Number.parseInt(parts[1] ?? "", 10);
      if (!Number.isFinite(secs) || secs <= 0) return ok("Usage: `/image timeout <seconds>`.");
      await ctx.api.setImageGenConfig({ ...cfg, timeout: secs });
      return ok(`**Image timeout:** ${secs}s.`);
    }
    return ok("Usage: `/image [status|enable|disable|model <id>|timeout <sec>]`.");
  } catch (err) {
    return fail("/image", err);
  }
}

// ─── /upload [path] — upload directory ─────────────────────────────────────

async function handleUpload(args: string, ctx: CommandContext): Promise<CommandResult> {
  if (!ctx.api.getPathsConfig || !ctx.api.setPathsConfig) return unsupported("/upload");
  try {
    const cfg = await ctx.api.getPathsConfig();
    const target = args.trim();
    if (!target) {
      return ok(`**Upload dir:** \`${cfg.upload_dir || "<project>/.ocode/uploads"}\`\n\nDropped files land here; reference them in chat as \`@.ocode/uploads/<name>\`.`);
    }
    await ctx.api.setPathsConfig(cfg.extra_allowed_paths, target);
    return ok(`**Upload dir:** set to \`${target}\`.`);
  } catch (err) {
    return fail("/upload", err);
  }
}

// ─── /connect <provider> <apikey> ──────────────────────────────────────────

async function handleConnect(args: string, ctx: CommandContext): Promise<CommandResult> {
  const parts = args.trim().split(/\s+/).filter(Boolean);
  if (parts.length !== 2) {
    return ok("Usage: `/connect <provider> <api-key>` — stores the key in auth.json and exports it for new sessions. The interactive dialog is TUI-only.");
  }
  if (!ctx.api.connectProvider) return unsupported("/connect");
  try {
    const r = await ctx.api.connectProvider(parts[0], parts[1]);
    return ok(`Connected **${r.provider}** (key \`${r.key}\`). New sessions pick it up; existing sessions keep their current clients until rebuilt.`);
  } catch (err) {
    return fail("/connect", err);
  }
}

// ─── helpers ───────────────────────────────────────────────────────────────

function ok(content: string): CommandResult {
  return { handled: true, messages: [{ role: "assistant", content }] };
}

function fail(name: string, err: unknown): CommandResult {
  return {
    handled: true,
    messages: [{
      role: "assistant",
      content: `**${name} failed:** ${errText(err)}`,
    }],
  };
}

// ─── /context — token budget ──────────────────────────────────────────────

async function handleContext(ctx: CommandContext): Promise<CommandResult> {
  const sessionId = ctx.getSessionId?.();
  if (!sessionId) {
    return {
      handled: true,
      messages: [{ role: "assistant", content: "No active session." }],
    };
  }
  try {
    const c = await ctx.api.getSessionContext(sessionId);
    const pct = c.max_tokens ? Math.round((c.estimated_tokens / c.max_tokens) * 100) : null;
    return {
      handled: true,
      messages: [{
        role: "assistant",
        content: [
          "## Context Budget",
          `- **Model:** ${c.model || "unknown"}`,
          `- **Messages:** ${c.message_count}`,
          `- **Estimated tokens:** ~${c.estimated_tokens.toLocaleString()}`,
          `- **Max context:** ${c.max_tokens ? c.max_tokens.toLocaleString() : "unknown"}${pct !== null ? ` (${pct}% used)` : ""}`,
        ].join("\n"),
      }],
    };
  } catch (err) {
    return {
      handled: true,
      messages: [{
        role: "assistant",
        content: `**/context failed:** ${err instanceof Error ? err.message : String(err)}`,
      }],
    };
  }
}

// ─── /lsp — diagnostics status ────────────────────────────────────────────

async function handleLsp(ctx: CommandContext): Promise<CommandResult> {
  try {
    const { lsp_servers } = await ctx.api.getLSPStatuses();
    if (!lsp_servers?.length) {
      return {
        handled: true,
        messages: [{ role: "assistant", content: "No LSP servers active." }],
      };
    }
    const lines = lsp_servers.map((s) => {
      const errs = s.diagnostics_errors ?? 0;
      const warns = s.diagnostics_warnings ?? 0;
      const diag = errs + warns > 0 ? ` — ${errs} errors, ${warns} warnings` : " — no diagnostics";
      return `- **${s.lang_id || s.cmd}** (${s.state})${diag}`;
    });
    return {
      handled: true,
      messages: [{
        role: "assistant",
        content: `## LSP Status\n\n${lines.join("\n")}`,
      }],
    };
  } catch (err) {
    return {
      handled: true,
      messages: [{
        role: "assistant",
        content: `**/lsp failed:** ${err instanceof Error ? err.message : String(err)}`,
      }],
    };
  }
}

// ─── /agents — subagent status ────────────────────────────────────────────

async function handleAgents(ctx: CommandContext): Promise<CommandResult> {
  try {
    const runs = await ctx.api.getAgentRuns?.();
    if (!runs || !runs.length) {
      return {
        handled: true,
        messages: [{ role: "assistant", content: "No active or queued subagents." }],
      };
    }
    const lines = runs.map((r) => {
      const s = typeof r.status === "string" ? r.status : (r.state ?? "running");
      return `- **${r.agent || r.id}** — ${s}${r.title ? ` (${r.title})` : ""}`;
    });
    return {
      handled: true,
      messages: [{
        role: "assistant",
        content: `## Subagents\n\n${lines.join("\n")}`,
      }],
    };
  } catch (err) {
    return {
      handled: true,
      messages: [{
        role: "assistant",
        content: `**/agents failed:** ${err instanceof Error ? err.message : String(err)}`,
      }],
    };
  }
}

// ─── /skills — available skills ───────────────────────────────────────────

async function handleSkills(ctx: CommandContext): Promise<CommandResult> {
  try {
    const skills = await ctx.api.listSkills();
    if (!skills.length) {
      return {
        handled: true,
        messages: [{ role: "assistant", content: "No skills installed." }],
      };
    }
    const lines = skills.map(
      (s) => `- **${s.name}**${s.description ? ` — ${s.description}` : ""}`,
    );
    return {
      handled: true,
      messages: [{
        role: "assistant",
        content: `## Skills\n\n${lines.join("\n")}`,
      }],
    };
  } catch (err) {
    return {
      handled: true,
      messages: [{
        role: "assistant",
        content: `**/skills failed:** ${err instanceof Error ? err.message : String(err)}`,
      }],
    };
  }
}

// ─── /mcp — MCP server status ─────────────────────────────────────────────

async function handleMcp(ctx: CommandContext): Promise<CommandResult> {
  try {
    const servers = await ctx.api.getMCP();
    if (!servers.length) {
      return {
        handled: true,
        messages: [{ role: "assistant", content: "No MCP servers configured." }],
      };
    }
    const lines = servers.map(
      (s) => `- **${s.name}** — ${s.enabled ? "enabled" : "disabled"}`,
    );
    return {
      handled: true,
      messages: [{
        role: "assistant",
        content: `## MCP Servers\n\n${lines.join("\n")}\n\nUse \`/mcp enable <name>\` or \`/mcp disable <name>\` to toggle.`,
      }],
    };
  } catch (err) {
    return {
      handled: true,
      messages: [{
        role: "assistant",
        content: `**/mcp failed:** ${err instanceof Error ? err.message : String(err)}`,
      }],
    };
  }
}

// ─── /cron — scheduled jobs ───────────────────────────────────────────────

async function handleCron(ctx: CommandContext): Promise<CommandResult> {
  try {
    const jobs = await ctx.api.getCronJobs?.();
    if (!jobs || !jobs.length) {
      return {
        handled: true,
        messages: [{ role: "assistant", content: "No scheduled jobs. Use `/cron add` to create one." }],
      };
    }
    const lines = jobs.map((j) => {
      const id = typeof j.id === "string" ? j.id : String(j.id);
      const name = typeof j.name === "string" ? j.name : id;
      return `- **${name}** (\`${id}\`)${typeof j.next_run === "string" ? ` — next ${j.next_run}` : ""}`;
    });
    return {
      handled: true,
      messages: [{
        role: "assistant",
        content: `## Scheduled Jobs\n\n${lines.join("\n")}`,
      }],
    };
  } catch (err) {
    return {
      handled: true,
      messages: [{
        role: "assistant",
        content: `**/cron failed:** ${err instanceof Error ? err.message : String(err)}`,
      }],
    };
  }
}

// ─── /github — PR and issue lookup ────────────────────────────────────────

async function handleGithub(args: string, ctx: CommandContext): Promise<CommandResult> {
  const parts = args.trim().split(/\s+/).filter(Boolean);

  // /github pr <owner> <repo> <number>
  if (parts[0] === "pr" && parts.length >= 4) {
    const [, owner, repo, numStr] = parts;
    const num = Number(numStr);
    if (!Number.isFinite(num)) {
      return {
        handled: true,
        messages: [{ role: "assistant", content: "Invalid PR number. Usage: `/github pr <owner> <repo> <number>`" }],
      };
    }
    try {
      const { pr, diff } = await ctx.api.getGithubPR(owner, repo, num);
      const title = typeof pr.title === "string" ? pr.title : "(untitled)";
      const state = typeof pr.state === "string" ? pr.state : "unknown";
      const body = typeof pr.body === "string" && pr.body ? pr.body : "(no description)";
      const user = pr.user as { login?: unknown } | undefined;
      const author = typeof user?.login === "string" ? user.login : "unknown";
      const content = [
        `## PR #${num}: ${title}`,
        `**State:** ${state} | **Author:** ${author}`,
        "",
        body,
        "",
        "### Diff",
        "```diff",
        diff || "(diff unavailable)",
        "```",
      ].join("\n");
      return { handled: true, messages: [{ role: "assistant", content }] };
    } catch (err) {
      return {
        handled: true,
        messages: [{ role: "assistant", content: `**GitHub PR error:** ${err instanceof Error ? err.message : String(err)}` }],
      };
    }
  }

  // /github issue list <owner> <repo> [state]
  if (parts[0] === "issue" && parts[1] === "list" && parts.length >= 4) {
    const [, , owner, repo] = parts;
    const state = parts[4] ?? "open";
    try {
      const issues = await ctx.api.getGithubIssues(owner, repo, state);
      if (!issues.length) {
        return {
          handled: true,
          messages: [{ role: "assistant", content: `No ${state} issues in ${owner}/${repo}.` }],
        };
      }
      const lines = issues.map((i) => {
        const num = typeof i.number === "number" ? i.number : "?";
        const title = typeof i.title === "string" ? i.title : "(untitled)";
        return `- **#${num}** ${title}`;
      });
      return {
        handled: true,
        messages: [{ role: "assistant", content: `## Issues (${owner}/${repo}, ${state})\n\n${lines.join("\n")}` }],
      };
    } catch (err) {
      return {
        handled: true,
        messages: [{ role: "assistant", content: `**GitHub issue error:** ${err instanceof Error ? err.message : String(err)}` }],
      };
    }
  }

  // /github issue get <owner> <repo> <number>
  if (parts[0] === "issue" && parts[1] === "get" && parts.length >= 5) {
    const [, , owner, repo, numStr] = parts;
    const num = Number(numStr);
    if (!Number.isFinite(num)) {
      return {
        handled: true,
        messages: [{ role: "assistant", content: "Invalid issue number. Usage: `/github issue get <owner> <repo> <number>`" }],
      };
    }
    try {
      const issues = await ctx.api.getGithubIssues(owner, repo, "all");
      const issue = issues.find((i) => i.number === num);
      if (!issue) {
        return {
          handled: true,
          messages: [{ role: "assistant", content: `Issue #${num} not found in ${owner}/${repo}.` }],
        };
      }
      const title = typeof issue.title === "string" ? issue.title : "(untitled)";
      const state = typeof issue.state === "string" ? issue.state : "unknown";
      const body = typeof issue.body === "string" && issue.body ? issue.body : "(no description)";
      return {
        handled: true,
        messages: [{
          role: "assistant",
          content: `## Issue #${num}: ${title}\n\n**State:** ${state}\n\n${body}`,
        }],
      };
    } catch (err) {
      return {
        handled: true,
        messages: [{ role: "assistant", content: `**GitHub issue error:** ${err instanceof Error ? err.message : String(err)}` }],
      };
    }
  }

  return {
    handled: true,
    messages: [{
      role: "assistant",
      content: [
        "## GitHub commands",
        "- `/github pr <owner> <repo> <number>` — Get PR diff and details",
        "- `/github issue list <owner> <repo> [state]` — List issues",
        "- `/github issue get <owner> <repo> <number>` — Get issue details",
      ].join("\n"),
    }],
  };
}

// ─── /small-model, /advisor — config routing ──────────────────────────────

async function handleSmallModel(ctx: CommandContext): Promise<CommandResult> {
  try {
    const cfg = await ctx.api.getSmallModelWithEnabled?.();
    const model = cfg?.model || "not configured";
    const enabled = cfg?.enabled ?? true;
    return {
      handled: true,
      messages: [{
        role: "assistant",
        content: `**Small model:** ${model}${enabled ? "" : " (disabled)"}\n\nSwitch it from the model selector or via config.`,
      }],
    };
  } catch {
    return {
      handled: true,
      messages: [{ role: "assistant", content: "**Small model:** not configured" }],
    };
  }
}

async function handleAdvisor(ctx: CommandContext): Promise<CommandResult> {
  try {
    const cfg = await ctx.api.getAdvisor?.();
    return {
      handled: true,
      messages: [{
        role: "assistant",
        content: `**Advisor model:** ${cfg?.model || "default"}\n\nSet it with \`/advisor <model>\` or from settings.`,
      }],
    };
  } catch {
    return {
      handled: true,
      messages: [{ role: "assistant", content: "**Advisor model:** default" }],
    };
  }
}
