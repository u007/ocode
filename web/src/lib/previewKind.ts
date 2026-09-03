import { apiPath } from "../api/client";

// ── Sidebar PreviewHost shared contract ─────────────────────────────
// Pure helpers (no React) so they are trivially unit-testable. The backend
// mirrors the allowlist in HandleFileRaw.previewRawTypes and the
// preview_open tool's previewOpenKinds — keep the three in sync.

export const PREVIEW_OPEN_SENTINEL = "PREVIEW_OPEN:";
export const OPEN_PREVIEW_EVENT = "ocode:open-preview";
export const PREVIEW_CONTEXT_EVENT = "ocode:preview-context";

export type PreviewKind =
  | "pdf"
  | "docx"
  | "pptx"
  | "image"
  | "mermaid"
  | "markdown"
  | "text";

const kindByExt: Record<string, PreviewKind> = {
  ".pdf": "pdf",
  ".docx": "docx",
  ".pptx": "pptx",
  ".png": "image",
  ".jpg": "image",
  ".jpeg": "image",
  ".gif": "image",
  ".webp": "image",
  ".svg": "image",
  ".mmd": "mermaid",
  ".md": "markdown",
  ".markdown": "markdown",
  ".txt": "text",
  ".ts": "text",
  ".tsx": "text",
  ".js": "text",
  ".jsx": "text",
  ".go": "text",
  ".py": "text",
  ".json": "text",
  ".yaml": "text",
  ".yml": "text",
  ".html": "text",
  ".css": "text",
};

/** Extension-based preview kind, or null when the sidebar can't preview it. */
export function previewKindForPath(path: string): PreviewKind | null {
  const dot = path.lastIndexOf(".");
  if (dot === -1) return null;
  return kindByExt[path.slice(dot).toLowerCase()] ?? null;
}

export interface PreviewOpenRequest {
  path: string;
  kind: PreviewKind;
  page: number;
  projectRoot?: string;
}

/**
 * Scans chat message content (tool results included) for the latest
 * `preview_open` tool directive: `PREVIEW_OPEN:<path>|kind=<k>|page=<n>`.
 * Returns the last match so a newer AI directive wins over an older one.
 */
export function parsePreviewOpen(contents: string[]): PreviewOpenRequest | null {
  let found: PreviewOpenRequest | null = null;
  const re = /PREVIEW_OPEN:([^\s|]+)\|kind=([a-z]+)\|page=(\d+)/g;
  for (const text of contents) {
    if (!text.includes(PREVIEW_OPEN_SENTINEL)) continue;
    // Reset per string (global regexes are stateful).
    re.lastIndex = 0;
    let m: RegExpExecArray | null;
    while ((m = re.exec(text)) !== null) {
      const kind = previewKindForPath(m[1]) ?? (m[2] as PreviewKind);
      found = { path: m[1], kind, page: Math.max(1, parseInt(m[3], 10) || 1) };
    }
  }
  return found;
}

/** Authed URL for GET /api/files/raw binary bytes (pdf/docx/pptx/image/mmd). */
export function previewRawUrl(path: string, projectRoot?: string): string {
  const q = `path=${encodeURIComponent(path)}${projectRoot ? `&project_root=${encodeURIComponent(projectRoot)}` : ""}`;
  return apiPath(`/api/files/raw?${q}`);
}

/** Ask the sidebar PreviewHost to show a file (file tree, AI tool hook). */
export function dispatchOpenPreview(path: string, page = 1, projectRoot?: string): void {
  window.dispatchEvent(
    new CustomEvent<PreviewOpenRequest>(OPEN_PREVIEW_EVENT, {
      detail: { path, kind: previewKindForPath(path) ?? "text", page, projectRoot },
    }),
  );
}

export interface PreviewSelection {
  path: string;
  /** Human citation: "p.3", "slide 4", "node <id>", "L12-L18". */
  label: string;
  excerpt: string;
  projectRoot?: string;
}

/** Push a preview highlight into the chat composer (Ask LLM). */
export function dispatchPreviewContext(sel: PreviewSelection): void {
  window.dispatchEvent(new CustomEvent<PreviewSelection>(PREVIEW_CONTEXT_EVENT, { detail: sel }));
}
