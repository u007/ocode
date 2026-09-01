import { OPEN_FILE_EVENT, type OpenFileDetail } from "./fileLinks";
import { apiPath, authHeaders } from "../api/client";

// Known code extensions — same intent as fileLinks.EXT but used as a set
// for hasExtension checks.
const EXT_LIST =
  "tsx?|jsx?|go|py|rs|rb|java|kt|swift|c|cc|cpp|cxx|h|hh|hpp|cs|php|scala|sh|bash|zsh|sql|json|jsonc|ya?ml|toml|ini|md|mdx|txt|csv|css|scss|sass|less|html?|xml|vue|svelte|lua|dart|ex|exs|erl|clj|hs|ml|proto|gradle|dockerfile|mod|sum|lock";
const EXT_RE = new RegExp(`\\.(?:${EXT_LIST})$`, "i");

// Packages/URLs we never treat as local files.
const EXTERNAL_PREFIX_RE = /^(?:https?:\/\/|data:|blob:|mailto:)/i;

// Bare npm package import: "react", "@scope/pkg", "lodash/fp" (slash but
// no extension and no leading ./) should NOT be treated as local files.
// Only relative (./ ../ /) or extension-bearing paths are local.
function isBarePackageImport(raw: string): boolean {
  if (/^\.{1,2}\//.test(raw) || raw.startsWith("/")) return false;
  if (EXTERNAL_PREFIX_RE.test(raw)) return false;
  // Any slash-containing import without an extension and without leading ./
  // is conservatively treated as a package (e.g. lodash/fp, react-dom/client,
  // @scope/pkg). Project-relative paths like "src/foo" without extension are
  // also rejected here — they must be written as "./src/foo" or have an
  // extension to be considered local. This matches the guard in fileLinks.
  if (raw.includes("/")) {
    if (!EXT_RE.test(raw) && !raw.startsWith("@")) {
      // Check if it's a bare package subpath without extension
      return true;
    }
    if (raw.startsWith("@") && !EXT_RE.test(raw) && !raw.includes("./") && !raw.includes("../")) {
      return true;
    }
    return false;
  }
  if (!raw.includes("/")) {
    if (!raw.includes(".")) return true;
    if (!EXT_RE.test(raw)) return true;
    return false;
  }
  return false;
}

export function isFileLike(raw: string): boolean {
  const trimmed = raw.trim();
  if (!trimmed) return false;
  if (EXTERNAL_PREFIX_RE.test(trimmed)) return false;
  const base = trimmed.replace(/:\d+(?::\d+)?$/, "");
  if (isBarePackageImport(base)) return false;
  if (/^\.{1,2}\//.test(base) || base.startsWith("/")) return true;
  if (EXT_RE.test(base)) return true;
  return false;
}

function posixNormalize(p: string): string {
  const parts = p.split("/");
  const out: string[] = [];
  for (const part of parts) {
    if (part === "" || part === ".") continue;
    if (part === "..") {
      if (out.length > 0) out.pop();
      else {
        // Escapes root → invalid
        return "";
      }
    } else {
      out.push(part);
    }
  }
  return out.join("/");
}

function posixDirname(p: string): string {
  const idx = p.lastIndexOf("/");
  if (idx === -1) return "";
  return p.slice(0, idx);
}

function posixJoin(a: string, b: string): string {
  if (!a) return b;
  if (!b) return a;
  return `${a}/${b}`;
}

export function hasKnownExtension(p: string): boolean {
  return EXT_RE.test(p);
}

/**
 * Resolve a raw import/path token relative to current file + projectRoot.
 * Returns a projectRoot-relative normalized path (no leading ./ or /), or
 * null if the token should not be opened as a local file.
 */
export function resolveImportPath(
  rawPath: string,
  currentFilePath: string,
  _projectRoot?: string,
): string | null {
  const rawTrim = rawPath.trim();
  if (!rawTrim) return null;
  if (EXTERNAL_PREFIX_RE.test(rawTrim)) return null;
  // Strip line suffix BEFORE file-like check — suffix must not affect filtering
  const { path: baseRaw } = splitPathToken(rawTrim);
  if (!isFileLike(baseRaw)) return null;

  let resolved: string;
  if (baseRaw.startsWith("./") || baseRaw.startsWith("../")) {
    const dir = posixDirname(currentFilePath);
    const joined = posixJoin(dir, baseRaw);
    const norm = posixNormalize(joined);
    if (!norm) return null;
    resolved = norm;
  } else if (baseRaw.startsWith("/")) {
    // Treat as project-root absolute
    const stripped = baseRaw.replace(/^\/+/, "");
    const norm = posixNormalize(stripped);
    if (!norm && stripped) return null;
    resolved = norm || stripped;
  } else if (baseRaw.includes("/")) {
    // Project-relative like "src/foo/bar" or "web/src/App.tsx"
    const norm = posixNormalize(baseRaw);
    if (!norm && baseRaw) return null;
    resolved = norm || baseRaw;
  } else {
    // Single segment with extension like "utils.ts" → relative to current dir
    if (hasKnownExtension(baseRaw)) {
      const dir = posixDirname(currentFilePath);
      const joined = posixJoin(dir, baseRaw);
      const norm = posixNormalize(joined);
      if (!norm) return null;
      resolved = norm;
    } else {
      return null;
    }
  }

  if (!resolved) return null;
  // Reject absolute filesystem escapes (e.g. still contains .. after normalize)
  if (resolved.includes("..")) return null;
  return resolved;
}

const CANDIDATE_EXTS = [".ts", ".tsx", ".js", ".jsx", ".py", ".go", ".json", ".css", ".scss"];

/**
 * Given a resolved base path (without line suffix), produce candidate file
 * paths to probe. If base already has an extension, only that path is tried.
 * Otherwise we try the bare path plus common extensions and index fallbacks.
 */
export function candidatePaths(resolvedBase: string, includeIndexFallbacks = true): string[] {
  if (hasKnownExtension(resolvedBase)) {
    return [resolvedBase];
  }
  const out: string[] = [resolvedBase];
  for (const ext of CANDIDATE_EXTS) {
    out.push(resolvedBase + ext);
  }
  if (includeIndexFallbacks) {
    for (const ext of CANDIDATE_EXTS) {
      out.push(posixJoin(resolvedBase, `index${ext}`));
    }
  }
  // Also try as-is if it's already a directory index (dedup later)
  return [...new Set(out)];
}

export function splitPathToken(token: string): { path: string; line?: number; col?: number } {
  const m = token.match(/^(.*?):(\d+)(?::(\d+))?$/);
  if (m) {
    return { path: m[1], line: parseInt(m[2], 10), col: m[3] ? parseInt(m[3], 10) : undefined };
  }
  return { path: token };
}

// Find links inside a single line of editor text.
// Returns ranges as 0-based [start, end) offsets within the line.
export interface LineLink {
  raw: string; // the token as it appears (without surrounding quotes)
  quoted: boolean;
  start: number; // 0-based inclusive
  end: number; // 0-based exclusive
  line?: number;
  col?: number;
  // Original path without line suffix
  path: string;
}

const EXTERNAL_RE = /^(?:https?:\/\/|data:|blob:)/i;

// Quoted string extraction: capture inner content of "..." '...' `...`
const QUOTED_RE = /(["'`])((?:\.{1,2}\/[^"'`]+?|\/[^"'`]+?|[\w@.\-]+\/[^"'`]*?|[\w.\-]+\.(?:tsx?|jsx?|go|py|rs|rb|java|kt|swift|c|cc|cpp|cxx|h|hh|hpp|cs|php|scala|sh|bash|zsh|sql|json|jsonc|ya?ml|toml|ini|md|mdx|txt|csv|css|scss|sass|less|html?|xml|vue|svelte|lua|dart|ex|exs|erl|clj|hs|ml|proto|gradle|dockerfile|mod|sum|lock)(?::\d+(?::\d+)?)?))(\1)/gi;

// Bare path token inside code — guarded like fileLinks.GUARD to avoid
// matching inside URLs (https://host/foo.ts) or mid-token.
const GUARD = "(?<![\\w:/.\\-])";
const BARE_PATH_RE = new RegExp(
  GUARD +
    `(?:\\.{1,2}/[\\w.\\-]+(?:/[\\w.\\-]+)*|/[\\w.\\-]+(?:/[\\w.\\-]+)*|[\\w.\\-]+(?:/[\\w.\\-]+)*\\.(?:${EXT_LIST})(?![\\w]))(?::\\d+(?::\\d+)?)?`,
  "gi",
);

export function findLinksInLine(lineText: string): LineLink[] {
  const out: LineLink[] = [];
  const covered = new Set<number>(); // char indices covered by quoted matches

  // 1. Quoted strings
  let qm: RegExpExecArray | null;
  QUOTED_RE.lastIndex = 0;
  while ((qm = QUOTED_RE.exec(lineText)) !== null) {
    const full = qm[0];
    const inner = qm[2];
    if (!inner || EXTERNAL_RE.test(inner)) continue;
    if (!isFileLike(inner)) continue;
    const innerStart = qm.index + 1; // after opening quote
    const innerEnd = innerStart + inner.length;
    // Mark covered
    for (let i = innerStart; i < innerEnd; i++) covered.add(i);
    const { path, line, col } = splitPathToken(inner);
    // isFileLike already checked on full inner; also check split path
    if (!isFileLike(path)) continue;
    out.push({ raw: inner, quoted: true, start: innerStart, end: innerEnd, line, col, path });
    // Avoid infinite loop on zero-length (not expected)
    if (full.length === 0) QUOTED_RE.lastIndex++;
  }

  // 2. Bare tokens (skip quoted regions)
  let bm: RegExpExecArray | null;
  BARE_PATH_RE.lastIndex = 0;
  while ((bm = BARE_PATH_RE.exec(lineText)) !== null) {
    const token = bm[0];
    const idx = bm.index;
    const end = idx + token.length;
    // Skip if overlaps quoted
    let overlaps = false;
    for (let i = idx; i < end; i++) if (covered.has(i)) { overlaps = true; break; }
    if (overlaps) continue;
    // Skip if inside quotes but missed by quoted regex (rare)
    // Already covered; else linkify
    const { path, line, col } = splitPathToken(token);
    out.push({ raw: token, quoted: false, start: idx, end, line, col, path });
    if (token.length === 0) BARE_PATH_RE.lastIndex++;
  }

  // Sort by start
  out.sort((a, b) => a.start - b.start);
  return out;
}

// Import-clause symbol links: `import Foo, { bar, baz as qux } from "./mod"`
// and `export { x } from "./mod"`. Each imported symbol becomes a link that
// opens the module at the symbol's declaration line.
export interface ImportSymbolLink {
  symbol: string; // exported name in the target module
  isDefault: boolean;
  path: string; // module specifier as written
  start: number; // 0-based inclusive
  end: number; // 0-based exclusive
}

const IMPORT_CLAUSE_RE = /^(\s*(?:import|export)\s+)(?:type\s+)?([^"'`]*?)\s+from\s+(["'`])([^"'`]+)\3/;
const IDENT = "[A-Za-z_$][\\w$]*";

export function findImportSymbolLinks(lineText: string): ImportSymbolLink[] {
  const m = IMPORT_CLAUSE_RE.exec(lineText);
  if (!m) return [];
  const [, head, clause, , spec] = m;
  if (!isFileLike(spec)) return [];
  const clauseStart = head.length;
  const out: ImportSymbolLink[] = [];
  const braceIdx = clause.indexOf("{");
  const outer = braceIdx === -1 ? clause : clause.slice(0, braceIdx);
  // Default import: first identifier outside the braces (skip `* as ns`).
  const def = new RegExp(`^\\s*(${IDENT})`).exec(outer);
  if (def && def[1] !== "type") {
    const start = clauseStart + def.index + def[0].length - def[1].length;
    out.push({ symbol: def[1], isDefault: true, path: spec, start, end: start + def[1].length });
  }
  if (braceIdx !== -1) {
    const closeIdx = clause.indexOf("}", braceIdx);
    const inner = clause.slice(braceIdx + 1, closeIdx === -1 ? undefined : closeIdx);
    const innerStart = clauseStart + braceIdx + 1;
    const specRe = new RegExp(`(?:^|,)\\s*(?:type\\s+)?(${IDENT})`, "g");
    let sm: RegExpExecArray | null;
    while ((sm = specRe.exec(inner)) !== null) {
      const name = sm[1];
      const start = innerStart + sm.index + sm[0].length - name.length;
      out.push({ symbol: name, isDefault: false, path: spec, start, end: start + name.length });
    }
  }
  return out;
}

function escapeRe(s: string): string {
  return s.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
}

// Locate the 1-based line declaring `symbol` in `content` (TS/JS). Default
// imports resolve to the `export default` line. Returns null when not found.
export function findDeclarationLine(content: string, symbol: string, isDefault: boolean): number | null {
  const lines = content.split("\n");
  const sym = escapeRe(symbol);
  const patterns = isDefault
    ? [/^\s*export\s+default\b/]
    : [
        new RegExp(
          `^\\s*export\\s+(?:declare\\s+)?(?:abstract\\s+)?(?:async\\s+)?(?:function\\*?|class|const|let|var|type|interface|enum|namespace)\\s+${sym}\\b`,
        ),
        new RegExp(`^\\s*export\\s*\\{[^}]*\\b${sym}\\b`),
      ];
  for (const re of patterns) {
    for (let i = 0; i < lines.length; i++) {
      if (re.test(lines[i])) return i + 1;
    }
  }
  return null;
}

// Model-level scan (used by LinkProvider)
export function findLinksInModelText(text: string): Array<{ lineNumber: number; link: LineLink }> {
  const lines = text.split("\n");
  const result: Array<{ lineNumber: number; link: LineLink }> = [];
  for (let i = 0; i < lines.length; i++) {
    const links = findLinksInLine(lines[i]);
    for (const l of links) result.push({ lineNumber: i + 1, link: l });
  }
  return result;
}

// Resolution + opening
async function fetchFileContent(path: string, projectRoot?: string): Promise<string | null> {
  const query = new URLSearchParams({ path });
  if (projectRoot) query.set("project_root", projectRoot);
  try {
    const res = await fetch(apiPath(`/api/files/content?${query.toString()}`), {
      headers: authHeaders(),
      method: "GET",
    });
    if (!res.ok) return null;
    const body = (await res.json()) as { content: string };
    return body.content;
  } catch (err) {
    console.warn("editorLinks: probe for", path, "failed:", err);
    return null;
  }
}

export async function resolveAndOpen(
  rawPath: string,
  line: number | undefined,
  currentFilePath: string,
  projectRoot?: string,
  symbol?: { name: string; isDefault: boolean },
): Promise<boolean> {
  const { path: basePath, line: suffixLine } = splitPathToken(rawPath);
  const effectiveLine = line ?? suffixLine;
  const resolved = resolveImportPath(basePath, currentFilePath, projectRoot);
  if (!resolved) return false;
  const candidates = candidatePaths(resolved);
  for (const c of candidates) {
    const content = await fetchFileContent(c, projectRoot);
    if (content === null) continue;
    let targetLine = effectiveLine;
    if (symbol) {
      const declLine = findDeclarationLine(content, symbol.name, symbol.isDefault);
      if (declLine === null) {
        console.warn("editorLinks: declaration of", symbol.name, "not found in", c, "— opening file top");
      } else {
        targetLine = declLine;
      }
    }
    dispatchOpen(c, targetLine, projectRoot);
    return true;
  }
  // No candidate existed — inert (do not open a non-existent fallback tab)
  return false;
}

function dispatchOpen(path: string, line?: number, projectRoot?: string) {
  const detail: OpenFileDetail = { path, line, projectRoot };
  window.dispatchEvent(new CustomEvent<OpenFileDetail>(OPEN_FILE_EVENT, { detail }));
}

// ── Monaco wiring ──
type MonacoLike = typeof import("monaco-editor");

interface LinkPayload {
  raw: string;
  path: string;
  line?: number;
  symbol?: string;
  isDefault?: boolean;
  currentPath: string;
  projectRoot?: string;
}

const modelInfo = new Map<string, { path: string; projectRoot?: string }>();
let providerRegistered = false;
let openerRegistered = false;

export function setEditorModelInfo(uri: string, info: { path: string; projectRoot?: string }) {
  modelInfo.set(uri, info);
}

export function clearEditorModelInfo(uri: string) {
  modelInfo.delete(uri);
}

export function getEditorModelInfo(uri: string) {
  return modelInfo.get(uri) ?? null;
}

// Idempotent global registration — call from FileEditor mount (has monaco instance).
export function ensureEditorLinks(monaco: MonacoLike) {
  if (!providerRegistered) {
    providerRegistered = true;
    const languages = [
      "typescript",
      "javascript",
      "python",
      "go",
      "rust",
      "java",
      "kotlin",
      "swift",
      "c",
      "cpp",
      "css",
      "scss",
      "less",
      "html",
      "json",
      "xml",
      "yaml",
      "markdown",
      "sql",
      "shell",
      "dockerfile",
      "plaintext",
      "terraform",
      "dart",
      "graphql",
      "vue",
      "svelte",
    ];
    const provider: import("monaco-editor").languages.LinkProvider = {
      provideLinks(model) {
        const info = modelInfo.get(model.uri.toString());
        if (!info) return { links: [] };
        const text = model.getValue();
        const links: import("monaco-editor").languages.ILink[] = [];
        const push = (
          lineNumber: number,
          start: number,
          end: number,
          payload: LinkPayload,
          tooltip: string,
        ) => {
          // Encode payload in the URL query so the opener can decode it
          // without an extra map. Monaco percent-decodes the query before
          // handing the Uri to the opener.
          const d = encodeURIComponent(JSON.stringify(payload));
          links.push({
            range: { startLineNumber: lineNumber, startColumn: start + 1, endLineNumber: lineNumber, endColumn: end + 1 },
            url: `ocode-file://open?d=${d}`,
            tooltip,
          });
        };
        const lines = text.split("\n");
        for (let i = 0; i < lines.length; i++) {
          const lineNumber = i + 1;
          for (const link of findLinksInLine(lines[i])) {
            push(
              lineNumber,
              link.start,
              link.end,
              { raw: link.raw, path: link.path, line: link.line, currentPath: info.path, projectRoot: info.projectRoot },
              link.line ? `Open ${link.path}:${link.line}` : `Open ${link.path}`,
            );
          }
          for (const sl of findImportSymbolLinks(lines[i])) {
            push(
              lineNumber,
              sl.start,
              sl.end,
              {
                raw: sl.path,
                path: sl.path,
                symbol: sl.symbol,
                isDefault: sl.isDefault,
                currentPath: info.path,
                projectRoot: info.projectRoot,
              },
              `Go to ${sl.symbol} in ${sl.path}`,
            );
          }
        }
        return { links };
      },
    };
    // Monaco's typings allow LanguageSelector = string | LanguageFilter | ...
    // Registering for each language avoids scheme mismatches (inmemory:// vs file://).
    for (const lang of languages) {
      try {
        monaco.languages.registerLinkProvider(lang, provider);
      } catch {
        // ignore duplicate / unsupported language
      }
    }
    // Fallback: also register for plaintext catch-all. Some Monaco builds
    // accept "*" or no selector overload — guard it.
    try {
      (monaco.languages as any).registerLinkProvider({ pattern: "**" }, provider);
    } catch {
      // ignore
    }
  }

  if (!openerRegistered) {
    openerRegistered = true;
    try {
      monaco.editor.registerLinkOpener({
        // Monaco calls this with a Uri (never the raw ILink), so read the
        // scheme/query off the Uri — `link.url` does not exist here.
        open(resource) {
          if (resource.scheme !== "ocode-file") return false;
          const q = resource.query;
          if (!q.startsWith("d=")) return false;
          let payload: LinkPayload;
          try {
            payload = JSON.parse(q.slice(2)) as LinkPayload;
          } catch (err) {
            console.error("editorLinks: failed to decode link payload", q, err);
            return false;
          }
          const { raw, line, currentPath, projectRoot, symbol, isDefault } = payload;
          // Opener may return a promise; resolve to whether a file was opened.
          return resolveAndOpen(
            raw,
            line,
            currentPath,
            projectRoot,
            symbol ? { name: symbol, isDefault: isDefault === true } : undefined,
          );
        },
      });
    } catch {
      // Older Monaco version without registerLinkOpener — fallback to none
    }
  }
}

// For testing: reset registration flags and modelInfo map
export function __resetEditorLinksForTest() {
  modelInfo.clear();
  providerRegistered = false;
  openerRegistered = false;
}
