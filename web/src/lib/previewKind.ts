import { apiPath } from "../api/client";

// ── Sidebar PreviewHost shared contract ─────────────────────────────
// Pure helpers (no React) so they are trivially unit-testable. The backend
// mirrors the allowlist in HandleFileRaw.previewRawTypes and the
// preview_open tool's previewOpenKinds — keep the three in sync.

export const PREVIEW_OPEN_SENTINEL = "PREVIEW_OPEN:";
export const OPEN_PREVIEW_EVENT = "ocode:open-preview";
export const PREVIEW_CONTEXT_EVENT = "ocode:preview-context";
export const PREVIEW_PROMOTE_EVENT = "ocode:preview-promote";

export type PreviewKind =
  | "pdf"
  | "docx"
  | "pptx"
  | "excel"
  | "image"
  | "audio"
  | "video"
  | "mermaid"
  | "markdown"
  | "text";

const kindByExt: Record<string, PreviewKind> = {
  ".pdf": "pdf",
  ".docx": "docx",
  ".pptx": "pptx",
  ".xlsx": "excel",
  ".xls": "excel",
  ".csv": "excel",
  ".png": "image",
  ".jpg": "image",
  ".jpeg": "image",
  ".gif": "image",
  ".webp": "image",
  ".svg": "image",
  // Audio/video: browser-playable containers only (mirrors
  // HandleFileRaw.previewRawTypes / preview_open's previewOpenKinds; .mkv
  // and .avi stay out — no reliable renderer).
  ".mp3": "audio",
  ".m4a": "audio",
  ".aac": "audio",
  ".wav": "audio",
  ".ogg": "audio",
  ".oga": "audio",
  ".opus": "audio",
  ".flac": "audio",
  ".mp4": "video",
  ".m4v": "video",
  ".webm": "video",
  ".ogv": "video",
  ".mov": "video",
  ".mmd": "mermaid",
  ".md": "markdown",
  ".markdown": "markdown",
  // MDX is markdown-source-plus-JSX. The preview renders it as Markdown
  // (react-markdown, no JSX evaluation — see isMarkdownPath); the editor
  // highlights it with Monaco's `mdx` language, which does understand JSX.
  ".mdx": "markdown",
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

/**
 * Kinds the Files tab renders as a read-only preview surface instead of Monaco.
 * These are binary containers with no editable text representation, so opening
 * them in the editor only ever produced a "Binary File — Edit anyway" dead end.
 * `markdown`, `text`, and `mermaid` stay editable and are deliberately excluded.
 */
const PREVIEW_ONLY_KINDS: ReadonlySet<PreviewKind> = new Set(["pdf", "docx", "pptx", "excel", "image", "audio", "video"]);

/**
 * True for a Markdown document (`.md` / `.markdown` / `.mdx`). Markdown is
 * NOT preview-only — it stays editable in Monaco — but the Files tab gives it
 * an Edit/Preview/Split mode switch (default: Edit) because a rendered preview
 * is useful alongside the source. See `FileTabContent`.
 *
 * MDX (`.mdx`) is treated as Markdown for preview: react-markdown renders the
 * prose. ESM import/export lines and JSX components are NOT evaluated — they
 * render as source-level text — which is deliberate (MDX evaluation would
 * execute arbitrary JavaScript from the file).
 */
export function isMarkdownPath(path: string): boolean {
  return previewKindForPath(path) === "markdown";
}

/**
 * PreviewSurface kind for a path the Files-tab editor must NOT open in Monaco,
 * or null when the path is text-like (or not previewable). Callers use this to
 * auto-default PDFs, Office documents, and media to a preview.
 */
export function previewOnlyKindForPath(path: string): PreviewKind | null {
  const kind = previewKindForPath(path);
  return kind !== null && PREVIEW_ONLY_KINDS.has(kind) ? kind : null;
}

// Legacy Office formats with no reliable browser renderer (no server-side
// conversion in v1): .docx/.pptx preview; these do not.
const LEGACY_OFFICE_RE = /\.(doc|ppt)$/i;

/** Legacy Office (.doc/.ppt) — no in-browser renderer; show the OS-open fallback. */
export function isLegacyOfficePath(path: string): boolean {
  return LEGACY_OFFICE_RE.test(path);
}

/**
 * Resolves an open request to a renderable kind or an explicit unsupported
 * path. Unknown-but-textual extensions (e.g. `.java`, extensionless
 * `Makefile`) fall through to the text editor when the request asked for
 * text; only explicitly legacy formats (plus non-text unknowns) land on
 * the OS-open fallback — never a broken preview.
 */
export function resolvePreviewDoc(
  path: string,
  requestedKind: PreviewKind,
): { kind: PreviewKind; unsupported: null } | { kind: null; unsupported: string } {
  const known = previewKindForPath(path);
  if (known) return { kind: known, unsupported: null };
  if (requestedKind === "text" && !LEGACY_OFFICE_RE.test(path)) return { kind: "text", unsupported: null };
  return { kind: null, unsupported: path };
}

export interface PreviewOpenRequest {
  path: string;
  kind: PreviewKind;
  page: number;
  projectRoot?: string;
  /** Registered remote target for the file's project. The sidebar lookup
   *  fills this in from the active project; the dialog/file-tree caller
   *  passes it directly. */
  projectHost?: string;
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
export function previewRawUrl(path: string, projectRoot?: string, projectHost?: string): string {
  const q = `path=${encodeURIComponent(path)}${projectRoot ? `&project_root=${encodeURIComponent(projectRoot)}` : ""}${projectHost ? `&host=${encodeURIComponent(projectHost)}` : ""}`;
  return apiPath(`/api/files/raw?${q}`);
}

/** Ask the sidebar PreviewHost to show a file (file tree, AI tool hook). */
export function dispatchOpenPreview(path: string, page = 1, projectRoot?: string, projectHost?: string): void {
  window.dispatchEvent(
    new CustomEvent<PreviewOpenRequest>(OPEN_PREVIEW_EVENT, {
      detail: { path, kind: previewKindForPath(path) ?? "text", page, projectRoot, projectHost },
    }),
  );
}

export interface PreviewSelection {
  path: string;
  /** Human citation: "p.3", "slide 4", "node <id>", "L12-L18". */
  label: string;
  excerpt: string;
  projectRoot?: string;
  /** Registered remote target of the file's project. Carried through the
   *  Ask-LLM context chip so the composer can scope the reference to the
   *  right project (same ?host= routing the file fetch used). */
  projectHost?: string;
}

/** Push a preview highlight into the chat composer (Ask LLM). */
export function dispatchPreviewContext(sel: PreviewSelection): void {
  window.dispatchEvent(new CustomEvent<PreviewSelection>(PREVIEW_CONTEXT_EVENT, { detail: sel }));
}
