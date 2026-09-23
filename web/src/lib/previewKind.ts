import { apiPath } from "../api/client";

// ── Sidebar PreviewHost shared contract ─────────────────────────────
// Pure helpers (no React) so they are trivially unit-testable.
//
// Format classification is deliberately OPEN: only extensions with a
// specialized in-browser renderer are listed in `kindByExt`, known
// binary/no-renderer formats are listed in `NON_PREVIEWABLE_EXTS`, and
// EVERYTHING ELSE (including extensionless files like `Makefile`) is treated
// as text. The authoritative binary gate is content-based, not
// extension-based: GET /api/files/content returns `is_binary` from a NUL-byte
// sniff, and the text surfaces show "Binary File — Edit anyway" when it trips.
// A narrow text allowlist used to make `.sql`, `.java`, `.sh`, `Makefile`, …
// render as "Format Not Supported"; that was the bug.
//
// The specialized entries must stay in sync with HandleFileRaw.previewRawTypes
// and the preview_open tool's previewOpenKinds (the renderers that read raw
// bytes).

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
};

/**
 * Known binary formats with no in-browser renderer. These keep the deliberate
 * OS-open fallback (or, for media, simply stay un-previewable) instead of
 * being fetched as text: a huge archive or disk image read through
 * /api/files/content would be JSON-decoded into a JS string for nothing.
 * Anything not here and not in `kindByExt` is text.
 *
 * This is a UX/perf denylist, NOT the binary gate — a format missing from it
 * still degrades safely to the content endpoint's NUL-byte check.
 */
const NON_PREVIEWABLE_EXTS: ReadonlySet<string> = new Set([
  // Archives, packages, disk images.
  ".zip", ".tar", ".gz", ".tgz", ".bz2", ".tbz", ".tbz2", ".xz", ".txz",
  ".lz", ".lzma", ".zst", ".zstd", ".7z", ".rar", ".cab", ".arj", ".lzh",
  ".jar", ".war", ".ear", ".apk", ".aab", ".ipa", ".dmg", ".iso", ".img",
  ".vhd", ".vhdx", ".vmdk", ".qcow2", ".deb", ".rpm", ".pkg", ".mpkg",
  ".msi", ".msp", ".snap", ".flatpak", ".crx", ".xpi", ".whl", ".gem",
  ".nupkg", ".vsix", ".zpaq", ".lzo",
  // Executables, libraries, object/debug artifacts.
  ".exe", ".dll", ".dylib", ".so", ".o", ".obj", ".a", ".lib", ".bin",
  ".class", ".pyc", ".pyo", ".pyd", ".wasm", ".elf", ".com", ".sys", ".ko",
  ".app", ".msix", ".appx", ".node", ".out", ".pdb", ".dsym",
  // Fonts.
  ".ttf", ".otf", ".woff", ".woff2", ".eot", ".ttc", ".pfb", ".pfm", ".dfont",
  // Databases and binary indexes/dumps.
  ".sqlite", ".sqlite3", ".db", ".db3", ".mdb", ".accdb", ".idx", ".pack",
  ".lmdb", ".mdbx", ".frm", ".ibd", ".myi", ".myd", ".rdb", ".ldb", ".sst",
  // Audio/video containers with no reliable browser renderer (the playable
  // ones live in `kindByExt`).
  ".mkv", ".avi", ".wmv", ".flv", ".mpg", ".mpeg", ".m2ts", ".mts", ".vob",
  ".rm", ".rmvb", ".3gp", ".3g2", ".ogm", ".divx", ".asf", ".f4v", ".mxf",
  ".dv", ".wtv", ".wma", ".aiff", ".aif", ".aifc", ".au", ".snd", ".mid",
  ".midi", ".ra", ".ram", ".mka", ".ape", ".wv", ".amr", ".ac3", ".dts",
  ".caf", ".aax",
  // Images with no renderer in ImageViewer (png/jpg/gif/webp/svg only).
  ".tiff", ".tif", ".bmp", ".heic", ".heif", ".avif", ".ico", ".cur",
  ".psd", ".psb", ".xcf", ".raw", ".cr2", ".cr3", ".nef", ".arw", ".dng",
  ".orf", ".rw2", ".svgz", ".jp2", ".j2k",
  // Legacy/binary Office and document containers (modern .docx/.pptx/.xlsx
  // and .xls/.csv preview; .doc/.ppt go to the OS-open fallback).
  ".doc", ".ppt", ".docm", ".dot", ".dotm", ".dotx", ".xlsm", ".xlt",
  ".xltx", ".xltm", ".pptm", ".pot", ".potx", ".potm", ".pps", ".ppsx",
  ".ppsm", ".vsd", ".vsdx", ".one", ".msg", ".pub",
  // Other opaque binary containers.
  ".swf", ".ai", ".eps", ".indd", ".sketch", ".fig", ".blend", ".stl",
  ".fbx", ".3ds", ".glb", ".dwg", ".der", ".p12", ".pfx", ".jks", ".keystore",
  ".nib", ".car", ".icns", ".pak", ".bundle", ".parquet", ".avro", ".orc",
  ".arrow", ".feather", ".npy", ".npz", ".pkl", ".pickle", ".pt", ".pth",
  ".onnx", ".h5", ".hdf5", ".safetensors", ".ckpt", ".gguf", ".msgpack",
  ".bson", ".cbor",
]);

/**
 * Lowercase extension including the dot, or "" when the file has none.
 *
 * A LEADING dot is a dotfile, not an extension: `.env` / `.gitignore` return
 * "" (and are therefore text). Only a dot after the first character of the
 * basename counts, and only the basename is examined (so `my.dir/README` is
 * extensionless, and `archive.tar.gz` is `.gz`).
 */
function extensionOf(path: string): string {
  const slash = Math.max(path.lastIndexOf("/"), path.lastIndexOf("\\"));
  const base = path.slice(slash + 1);
  const dot = base.lastIndexOf(".");
  if (dot <= 0) return "";
  return base.slice(dot).toLowerCase();
}

/**
 * Preview kind for a path: a specialized renderer when the extension has one,
 * null for a known non-previewable binary, and `"text"` for everything else
 * (code, config, markup, extensionless files, dotfiles). Text is never
 * rejected here — the content endpoint's `is_binary` flag is the real gate.
 */
export function previewKindForPath(path: string): PreviewKind | null {
  const ext = extensionOf(path);
  const kind = kindByExt[ext];
  if (kind) return kind;
  if (NON_PREVIEWABLE_EXTS.has(ext)) return null;
  return "text";
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
 * Resolves an open request to a renderable kind, or an explicit unsupported
 * path for a known non-previewable binary (legacy Office, archives, …).
 *
 * The path's extension is authoritative (a request's own `kind` is ignored). Unknown and extensionless paths resolve to `text` (with the
 * content endpoint's NUL sniff as the binary gate), so a directive can never
 * fake a renderer for a path that has none, and a textual `.sql`/`Makefile`
 * is never bounced to the OS-open fallback.
 */
export function resolvePreviewDoc(
  path: string,
): { kind: PreviewKind; unsupported: null } | { kind: null; unsupported: string } {
  const known = previewKindForPath(path);
  if (known) return { kind: known, unsupported: null };
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
