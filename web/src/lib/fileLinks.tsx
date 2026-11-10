import type { ReactNode } from "react";

export const OPEN_FILE_EVENT = "ocode:open-file";
export type OpenFileDetail = { path: string; line?: number; col?: number; projectRoot?: string };

// Known source-code / config extensions. A bare filename (no slash) is only
// linkified when it ends in one of these — otherwise prose words like "e.g."
// would become links.
const EXT =
  "tsx?|jsx?|go|py|rs|rb|java|kt|swift|c|cc|cpp|cxx|h|hh|hpp|cs|php|scala|sh|bash|zsh|sql|json|jsonc|ya?ml|toml|ini|md|mdx|txt|csv|css|scss|sass|less|html?|xml|vue|svelte|lua|dart|ex|exs|erl|clj|hs|ml|proto|gradle|dockerfile|mod|sum|lock";

// A path token is either:
//   1. a home-relative `~/...` path (see HOME_SOURCE below), or
//   2. an `.ocode/uploads/`-anchored path (see UPLOAD_SOURCE below), or
//   3. anchored by a leading ./  ../  or /  (clearly a path), or
//   4. a (possibly nested) path whose final segment ends in a code extension.
// Each may carry a trailing :line or :line:col suffix (e.g. handler.go:42).
// GUARD prevents starting mid-token or inside a URL ("https://host/path"); the
// extension guard (?!\w) forces the longest extension (package.json, not .js).
const LINE = "(?::\\d+(?::\\d+)?)?";
// The FINAL segment may not end in ".": prose ends sentences with it ("see
// ~/Downloads/notes."), and a token that keeps the period links a path that
// does not exist. Dots stay legal INSIDE a segment, so ~/a.b.c and .env still
// match whole. Intermediate segments need no such rule — a "/" follows them.
const SEG_TAIL = "[\\w.\\-]*[\\w\\-]";
// `~` is in the guard so a home-relative path can never match a SECOND time
// from its own slash: that produced a link on "/Downloads/foo" (a ROOT path)
// with the "~" left as prose, so clicking it tried to open /Downloads/foo.
const GUARD = "(?<![\\w:/.\\-~])";

// Home-relative paths ("~/Downloads/notes/") are how agents report scratch and
// out-of-project work. The "~" must stay INSIDE the token (the SPA does not
// expand it — the server owning that $HOME does, via projects.ExpandHome), and
// a trailing slash belongs to the token too: it is what makes a directory look
// like one, and the grant path opens a directory in the file tree.
// `~user/...` is deliberately NOT matched: ExpandHome leaves it alone, so it
// would resolve against the cwd instead of that user's home.
const HOME_SOURCE = `~/(?:[\\w.\\-]+/)*${SEG_TAIL}/?${LINE}`;

// Uploads are referenced in chat by the relative path `.ocode/uploads/<name>`
// (see CLAUDE.md "Web/Desktop Server"), and macOS screenshot names contain
// spaces — even a narrow no-break space (U+202F). This branch allows spaces
// ONLY inside an `.ocode/uploads/`-anchored path with an image/media
// extension, so ordinary prose with spaces never becomes link-like. Image
// extensions live here, NOT in the general EXT list: a bare "shot.png" in
// prose must not turn into a link.
const UPLOAD_EXT = "png|jpe?g|gif|webp|bmp|tiff?|svg|pdf";
const UPLOAD_SOURCE =
  `\\.ocode/uploads/[\\w.\\-]+(?:[ \\u00A0\\u202F]+[\\w.\\-]+)*\\.(?:${UPLOAD_EXT})(?![\\w])${LINE}`;
const PATH_SOURCE =
  `${GUARD}(?:` +
  HOME_SOURCE +
  `|` +
  UPLOAD_SOURCE +
  `|(?:\\.{1,2}/|/)(?:[\\w.\\-]+/)*${SEG_TAIL}${LINE}` +
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

export function openFile(path: string, line?: number) {
  window.dispatchEvent(new CustomEvent<OpenFileDetail>(OPEN_FILE_EVENT, { detail: { path, line } }));
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
        openFile(path, line);
      }}
      onKeyDown={(e) => {
        if (e.key === "Enter" || e.key === " ") {
          e.preventDefault();
          openFile(path, line);
        }
      }}
      // `file-link` is the stable hook colored surfaces (inline-code chips,
      // user bubbles) use to override the link color with their own foreground
      // token — a prose-safe link color is still illegible/indistinguishable
      // on a solid accent/primary surface (e.g. github-dark accent #58a6ff).
      // See MessageBubble.tsx. `text-link` is palette-derived (useTheme
      // computeThemeVars), AA-checked on muted + background for every theme.
      className="file-link cursor-pointer text-link underline-offset-2 hover:underline"
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
