// parseToolArgs returns the arguments object for a tool call, or null when the
// payload is missing, malformed, or not a JSON object. Shared by the hint and
// the bash-command extractors below.
function parseToolArgs(argsJson?: string): Record<string, unknown> | null {
  if (!argsJson) return null;
  try {
    const parsed: unknown = JSON.parse(argsJson);
    if (!parsed || typeof parsed !== "object" || Array.isArray(parsed)) return null;
    return parsed as Record<string, unknown>;
  } catch {
    return null;
  }
}

// bashCommandFromArgs returns the raw shell command for a bash tool call, or ""
// when there is none. Unlike formatToolArgsHint this does NOT add the `$ `
// prefix: callers that render the command in its own code block add it
// themselves, and callers that only inspect the text (newline/length checks)
// want the command verbatim.
export function bashCommandFromArgs(argsJson?: string): string {
  const args = parseToolArgs(argsJson);
  const cmd = args?.command;
  return typeof cmd === "string" ? cmd : "";
}

// Concise one-line summary of a tool call's arguments, mirroring the TUI's
// formatToolCallHint (internal/tui/tool_render.go). Used by ToolBlock for the
// tools whose raw argument JSON is pure noise in the transcript:
//
//   - bash  -> `$ <command>`     (the JSON is just {"command": "..."})
//   - read  -> `read <path> [offset=..] [limit=..]`
//   - write -> `write <path>` (the JSON is dominated by the file body, and the
//     change itself is shown by the FormatDiff result block right below)
//
// The header composes this as `🔧 {hint}`, so the hint must not repeat the
// leading tool glyph the TUI uses (`≫ read`, `✏ write`). Returns "" for any
// other tool, which signals the caller to keep rendering the raw arguments.
export function formatToolArgsHint(tool: string, argsJson?: string): string {
  const args = parseToolArgs(argsJson);
  if (!args) return "";

  const str = (k: string): string => {
    const v = args[k];
    if (typeof v === "string") return v;
    if (typeof v === "number") return String(v);
    return "";
  };
  const first = (...keys: string[]): string => {
    for (const k of keys) {
      const v = str(k);
      if (v) return v;
    }
    return "";
  };

  switch (tool) {
    case "bash": {
      const cmd = first("command");
      return cmd ? `$ ${cmd}` : "";
    }
    case "read": {
      const p = first("path", "file_path", "filePath");
      const offset = first("offset", "start_line");
      const limit = first("limit");
      if (offset && limit) return `read ${p} offset=${offset} limit=${limit}`;
      if (offset) return `read ${p} offset=${offset}`;
      if (limit) return `read ${p} limit=${limit}`;
      return `read ${p}`;
    }
    case "write":
      return `write ${first("path", "file_path")}`;
    default:
      return "";
  }
}
