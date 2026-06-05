import type { ReactNode } from "react";
import { api } from "../api/client";

// Known source-code / config extensions. A bare filename (no slash) is only
// linkified when it ends in one of these — otherwise prose words like "e.g."
// would become links.
const EXT =
  "tsx?|jsx?|go|py|rs|rb|java|kt|swift|c|cc|cpp|cxx|h|hh|hpp|cs|php|scala|sh|bash|zsh|sql|json|jsonc|ya?ml|toml|ini|md|mdx|txt|csv|css|scss|sass|less|html?|xml|vue|svelte|lua|dart|ex|exs|erl|clj|hs|ml|proto|gradle|dockerfile|mod|sum|lock";

// A path token is either:
//   1. anchored by a leading ./  ../  or /  (clearly a path), or
//   2. a (possibly nested) path whose final segment ends in a code extension.
// Each may carry a trailing :line or :line:col suffix (e.g. handler.go:42).
// GUARD prevents starting mid-token or inside a URL ("https://host/path"); the
// extension guard (?!\w) forces the longest extension (package.json, not .js).
const LINE = "(?::\\d+(?::\\d+)?)?";
const GUARD = "(?<![\\w:/.\\-])";
const PATH_SOURCE =
  `${GUARD}(?:` +
  `(?:\\.{1,2}/|/)[\\w.\\-]+(?:/[\\w.\\-]+)*${LINE}` +
  `|[\\w.\\-]+(?:/[\\w.\\-]+)*\\.(?:${EXT})(?![\\w])${LINE}` +
  `)`;

export function buildPathRegex(): RegExp {
  return new RegExp(PATH_SOURCE, "gi");
}

// splitPathToken separates a matched token into the file path and an optional
// 1-based line number parsed from a trailing :line[:col] suffix.
export function splitPathToken(token: string): { path: string; line?: number } {
  const m = token.match(/^(.*?):(\d+)(?::\d+)?$/);
  if (m) {
    return { path: m[1], line: parseInt(m[2], 10) };
  }
  return { path: token };
}

async function openFile(path: string, line?: number) {
  try {
    await api.openFile(path, line);
  } catch (err) {
    console.error(`[fileLinks] failed to open ${path}:`, err);
  }
}

function FileLink({ token }: { token: string }) {
  const { path, line } = splitPathToken(token);
  return (
    <span
      role="link"
      tabIndex={0}
      title={`Open ${path}${line ? `:${line}` : ""}`}
      onClick={(e) => {
        e.stopPropagation();
        void openFile(path, line);
      }}
      onKeyDown={(e) => {
        if (e.key === "Enter" || e.key === " ") {
          e.preventDefault();
          void openFile(path, line);
        }
      }}
      className="cursor-pointer text-sky-400 underline-offset-2 hover:underline"
    >
      {token}
    </span>
  );
}

// linkifyPlainText splits a plain string into text + clickable file-path spans.
// Used for user messages (rendered as plain <pre>, not markdown).
export function linkifyPlainText(text: string): ReactNode[] {
  const re = buildPathRegex();
  const out: ReactNode[] = [];
  let last = 0;
  let m: RegExpExecArray | null;
  let key = 0;
  while ((m = re.exec(text)) !== null) {
    if (m.index > last) out.push(text.slice(last, m.index));
    out.push(<FileLink key={key++} token={m[0]} />);
    last = m.index + m[0].length;
  }
  if (last < text.length) out.push(text.slice(last));
  return out;
}

// ---- rehype plugin: linkify file paths in rendered markdown ----
// Walks the hast tree and splits text nodes that contain path tokens into a mix
// of text nodes and custom `filelink` element nodes. Skips text inside <a>
// (already a link) and <pre> (fenced code blocks — too noisy); inline <code>
// is still linkified.

type HastNode = {
  type: string;
  tagName?: string;
  value?: string;
  properties?: Record<string, unknown>;
  children?: HastNode[];
};

function makeFileLinkNode(token: string): HastNode {
  return {
    type: "element",
    tagName: "filelink",
    properties: { token },
    children: [{ type: "text", value: token }],
  };
}

function linkifyTextNode(value: string): HastNode[] | null {
  const re = buildPathRegex();
  const nodes: HastNode[] = [];
  let last = 0;
  let m: RegExpExecArray | null;
  let matched = false;
  while ((m = re.exec(value)) !== null) {
    matched = true;
    if (m.index > last) nodes.push({ type: "text", value: value.slice(last, m.index) });
    nodes.push(makeFileLinkNode(m[0]));
    last = m.index + m[0].length;
  }
  if (!matched) return null;
  if (last < value.length) nodes.push({ type: "text", value: value.slice(last) });
  return nodes;
}

export function rehypeFileLinks() {
  return (tree: HastNode) => {
    const walk = (node: HastNode, skip: boolean) => {
      if (!node.children) return;
      const next: HastNode[] = [];
      for (const child of node.children) {
        if (child.type === "text" && !skip && typeof child.value === "string") {
          const replaced = linkifyTextNode(child.value);
          if (replaced) {
            next.push(...replaced);
            continue;
          }
        }
        if (child.type === "element") {
          const tag = child.tagName;
          const childSkip = skip || tag === "a" || tag === "pre" || tag === "filelink";
          walk(child, childSkip);
        }
        next.push(child);
      }
      node.children = next;
    };
    walk(tree, false);
  };
}

// markdownFileLinkComponent is the react-markdown components entry that renders
// the custom `filelink` hast nodes produced by rehypeFileLinks.
export function FileLinkFromNode({ node }: { node?: HastNode }) {
  const token = (node?.properties?.token as string) ?? "";
  return <FileLink token={token} />;
}
