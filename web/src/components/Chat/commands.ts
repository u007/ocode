import type { Message } from "../../api/types";
import type { LucideIcon } from "lucide-react";

// ─── Formatting helpers ─────────────────────────────────────────────────────

/** Format a list of messages as a rich Markdown export string. */
function formatMessagesAsMarkdown(
  messages: Message[],
  sessionId?: string,
): string {
  const lines: string[] = [];
  lines.push("# Chat Export");
  if (sessionId) {
    lines.push(`Session: \`${sessionId}\``);
  }
  lines.push(`Exported: ${new Date().toISOString()}`);
  lines.push("");

  for (const msg of messages) {
    const role = msg.role === "user" ? "**You**" : msg.role === "assistant" ? "**Assistant**" : `**${msg.role}**`;
    lines.push(`---\n${role}\n\n${msg.content}\n`);
    if (msg.tool_calls?.length) {
      for (const tc of msg.tool_calls) {
        lines.push(`> \`🛠 ${tc.function.name}(${tc.function.arguments})\`\n`);
      }
    }
  }

  return lines.join("\n");
}
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
  { name: "/session", description: "List, load, or resume sessions", icon: History },
  { name: "/ocr", description: "Show OCR status, enable/disable, set model", icon: Eye },
  { name: "/search", description: "Find a message by keyword", icon: Search },
  { name: "/btw", description: "Add a quick aside to the conversation", icon: MessageCircle },
  { name: "/mask", description: "Show secret redaction status", icon: Shield },
  { name: "/compact", description: "Compact conversation context", icon: Archive },
  { name: "/recap", description: "Generate session recap", icon: FileText },
  { name: "/export", description: "Export session as JSON", icon: FileDown },
  { name: "/share", description: "Share session link", icon: Share2 },
  { name: "/help", description: "Show available commands", icon: HelpCircle },
];

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

    // ── Frontend-handled with local data ──
    case "/export":
      return handleExport(ctx);

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
  const sub = args.toLowerCase();

  if (sub === "" || sub === "status") {
    try {
      const status = await ctx.api.getOcrEnabled();
      return {
        handled: true,
        messages: [{
          role: "assistant",
          content: `**OCR status:** ${status.enabled ? "✅ enabled" : "❌ disabled"}\n**Model:** ${status.model || "(not set)"}`,
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
      await ctx.api.setOcrEnabled(true);
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
      await ctx.api.setOcrEnabled(false);
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

  // /ocr model <name>
  if (sub.startsWith("model ")) {
    const modelName = sub.slice(6).trim();
    if (modelName) {
      try {
        await ctx.api.setOcrModel(modelName);
        return {
          handled: true,
          messages: [{
            role: "assistant",
            content: `OCR model set to **${modelName}**.`,
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
  }

  return {
    handled: true,
    messages: [{
      role: "assistant",
      content: "Usage: `/ocr [status\\|enable\\|disable\\|model <name>]`",
    }],
  };
}

async function handleExport(ctx: CommandContext): Promise<CommandResult> {
  const messages = ctx.getMessages?.() ?? [];
  if (messages.length === 0) {
    return {
      handled: true,
      messages: [{
        role: "assistant",
        content: "No messages to export. Start a conversation first.",
      }],
    };
  }

  const sessionId = ctx.getSessionId?.() ?? undefined;
  const markdown = formatMessagesAsMarkdown(messages, sessionId);

  return {
    handled: true,
    messages: [{
      role: "assistant",
      content: `Exported **${messages.length}** message${messages.length === 1 ? "" : "s"} as Markdown.`,
    }],
    download: {
      filename: `chat-export-${sessionId || "unknown"}.md`,
      content: markdown,
      mimeType: "text/markdown;charset=utf-8",
    },
  };
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
