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
      return handleUndo();

    case "/redo":
      return handleRedo();

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

async function handleUndo(): Promise<CommandResult> {
  try {
    const res = await api.undoFileChange();
    return {
      handled: true,
      messages: [{ role: "assistant", content: `Undid last change to \`${res.path}\`.` }],
    };
  } catch (err) {
    return errorMessage("Undo failed", err);
  }
}

async function handleRedo(): Promise<CommandResult> {
  try {
    const res = await api.redoFileChange();
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
      const plugins = await api.listPlugins();
      if (plugins.length === 0) {
        return {
          handled: true,
          messages: [{ role: "assistant", content: "No plugins installed." }],
        };
      }
      const lines = plugins.map(
        (p) => `- **${p.name}** ${p.enabled ? "✅" : "❌"} — ${p.description || p.source}`,
      );
      return {
        handled: true,
        messages: [{
          role: "assistant",
          content: `## Plugins\n\n${lines.join("\n")}\n\n\`/plugin enable\\|disable <name>\`, \`/plugin install <source>\`, \`/plugin remove <name>\``,
        }],
      };
    }

    if ((sub === "enable" || sub === "disable") && rest) {
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

function formatUsage(s: UsageSummary): string {
  const lines: string[] = ["## Token Usage"];
  lines.push("");
  lines.push(`**Requests:** ${s.total_requests.toLocaleString()}  `);
  lines.push(`**Total tokens:** ${s.total_tokens.toLocaleString()}  `);
  lines.push(`**Spend:** $${s.total_spend.toFixed(4)}`);
  lines.push("");

  if (s.by_model.length > 0) {
    lines.push("| Model | Requests | Prompt | Completion | Cache read | Total | Spend |");
    lines.push("|---|--:|--:|--:|--:|--:|--:|");
    for (const m of s.by_model) {
      lines.push(
        `| ${m.model} | ${m.request_count.toLocaleString()} | ${m.prompt_tokens.toLocaleString()} | ${m.completion_tokens.toLocaleString()} | ${m.cache_read_tokens.toLocaleString()} | ${m.total_tokens.toLocaleString()} | $${m.spend.toFixed(4)} |`,
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
