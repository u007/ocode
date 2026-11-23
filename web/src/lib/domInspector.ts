/**
 * DOM inspector model shared by Chrome/CDP mode and the local reverse-proxy
 * iframe. Both producers send a tree of `DomNode`s: Chrome's `DOM.getDocument`
 * reply is normalized here, and capture.js sends the same shape directly. The
 * SPA only ever renders node text as text — page content is untrusted.
 */

export interface DomNode {
  /** Stable within one fetch: Chrome's nodeId, or capture.js's childNodes path. */
  id: string;
  tag: string;
  attrs: Record<string, string>;
  /** Trimmed, capped text for `#text` nodes. */
  text?: string;
  children?: DomNode[];
}

export interface DomTreeResult {
  root: DomNode;
  truncated: boolean;
  nodes: number;
}

/** Cap on rendered nodes. A tree past this is marked truncated rather than
 *  silently losing children: the picker still works on the live page. */
export const DOM_NODE_LIMIT = 5000;
/** Longest text excerpt kept per text node. */
export const DOM_TEXT_LIMIT = 200;

function asRecord(value: unknown): Record<string, unknown> | null {
  return value !== null && typeof value === "object" ? (value as Record<string, unknown>) : null;
}

function attrsFromWire(raw: unknown): Record<string, string> {
  const out: Record<string, string> = {};
  if (Array.isArray(raw)) {
    for (let i = 0; i + 1 < raw.length; i += 2) {
      out[String(raw[i])] = String(raw[i + 1]);
    }
    return out;
  }
  const rec = asRecord(raw);
  if (rec) {
    for (const [k, v] of Object.entries(rec)) out[k] = String(v);
  }
  return out;
}

/**
 * Normalize Chrome's `DOM.getDocument` result. The document node itself is not
 * rendered (Chrome reports nodeType 9 with the `<html>` element as its only
 * child); comments and doctypes are dropped. Throws when the reply has no
 * document element, which is the only case where there is nothing to inspect.
 */
export function normalizeChromeTree(result: unknown): DomTreeResult {
  const rootDoc = asRecord(asRecord(result)?.root);
  if (!rootDoc) throw new Error("Chrome returned no document");
  const docChildren = Array.isArray(rootDoc.children) ? rootDoc.children : [];
  const html = docChildren.map(asRecord).find((n) => n && n.nodeType === 1);
  if (!html) throw new Error("page has no document element");

  let nodes = 0;
  let truncated = false;

  const walk = (raw: Record<string, unknown>): DomNode | null => {
    const type = typeof raw.nodeType === "number" ? raw.nodeType : 1;
    const id = raw.nodeId === undefined ? "" : String(raw.nodeId);
    if (!id) return null;
    if (type === 3) {
      const text = String(raw.nodeValue ?? "").trim().slice(0, DOM_TEXT_LIMIT);
      if (!text) return null;
      nodes++;
      return { id, tag: "#text", attrs: {}, text };
    }
    if (type !== 1) return null;
    nodes++;
    const node: DomNode = {
      id,
      tag: String(raw.localName || raw.nodeName || "node").toLowerCase(),
      attrs: attrsFromWire(raw.attributes),
    };
    const kids = Array.isArray(raw.children) ? raw.children : [];
    if (nodes >= DOM_NODE_LIMIT) {
      truncated = truncated || kids.length > 0;
      return node;
    }
    const children: DomNode[] = [];
    for (const kid of kids) {
      const rec = asRecord(kid);
      if (!rec) continue;
      const child = walk(rec);
      if (child) children.push(child);
      if (nodes >= DOM_NODE_LIMIT) {
        truncated = true;
        break;
      }
    }
    if (children.length > 0) node.children = children;
    return node;
  };

  const root = walk(html);
  if (!root) throw new Error("page has no document element");
  return { root, truncated, nodes };
}

/** Normalize capture.js's tree defensively: it comes from page JavaScript, so
 *  every field is re-typed here rather than trusted. */
export function normalizeLocalTree(result: unknown): DomTreeResult {
  const root = asRecord(asRecord(result)?.root);
  if (!root) throw new Error("page sent no DOM tree");
  let nodes = 0;

  const walk = (raw: Record<string, unknown>): DomNode | null => {
    const id = typeof raw.id === "string" ? raw.id : "";
    if (!id) return null;
    nodes++;
    const tag = typeof raw.tag === "string" && raw.tag ? raw.tag : "node";
    const node: DomNode = { id, tag, attrs: attrsFromWire(raw.attrs) };
    if (tag === "#text") {
      node.text = String(raw.text ?? "").trim().slice(0, DOM_TEXT_LIMIT);
      return node;
    }
    const kids = Array.isArray(raw.children) ? raw.children : [];
    if (nodes >= DOM_NODE_LIMIT) return node;
    const children: DomNode[] = [];
    for (const kid of kids) {
      const rec = asRecord(kid);
      if (!rec) continue;
      const child = walk(rec);
      if (child) children.push(child);
      if (nodes >= DOM_NODE_LIMIT) break;
    }
    if (children.length > 0) node.children = children;
    return node;
  };

  const normalizedRoot = walk(root);
  if (!normalizedRoot) throw new Error("page sent no DOM tree");
  return { root: normalizedRoot, truncated: result ? asRecord(result)?.truncated === true : false, nodes };
}

/** Ancestor chain (root first, node last), or null when the id is not in the tree. */
export function pathToNode(root: DomNode, id: string): DomNode[] | null {
  if (root.id === id) return [root];
  for (const child of root.children ?? []) {
    const found = pathToNode(child, id);
    if (found) return [root, ...found];
  }
  return null;
}

export function findNode(root: DomNode, id: string): DomNode | null {
  const path = pathToNode(root, id);
  return path ? path[path.length - 1] : null;
}

const CSS_IDENT = /^-?[A-Za-z_][A-Za-z0-9_-]*$/;

function cssEscape(value: string): string {
  if (CSS_IDENT.test(value)) return value;
  if (typeof CSS !== "undefined" && typeof CSS.escape === "function") return CSS.escape(value);
  return value.replace(/[^A-Za-z0-9_-]/g, (ch) => `\\${ch.codePointAt(0)!.toString(16)} `);
}

function nthOfType(node: DomNode, parent: DomNode | null): string {
  if (!parent?.children) return "";
  const sameTag = parent.children.filter((c) => c.tag === node.tag);
  if (sameTag.length <= 1) return "";
  const index = sameTag.findIndex((c) => c.id === node.id) + 1;
  return `:nth-of-type(${index})`;
}

/**
 * A copy-and-paste CSS selector for the node. The nearest ancestor carrying an
 * id anchors the selector; without one it starts at the root element and uses
 * `:nth-of-type` at each level, so the result is unique without depending on
 * generated class names.
 */
export function selectorFor(root: DomNode, id: string): string | null {
  const chain = pathToNode(root, id);
  if (!chain || chain.length === 0) return null;
  let start = 0;
  for (let i = chain.length - 1; i >= 0; i--) {
    if (chain[i].tag !== "#text" && chain[i].attrs.id) {
      start = i;
      break;
    }
  }
  const parts: string[] = [];
  for (let i = start; i < chain.length; i++) {
    const node = chain[i];
    if (node.tag === "#text") return null;
    const idAttr = node.attrs.id;
    if (i === start && idAttr) {
      parts.push(`#${cssEscape(idAttr)}`);
      continue;
    }
    parts.push(`${cssEscape(node.tag)}${nthOfType(node, i > 0 ? chain[i - 1] : null)}`);
  }
  return parts.join(" > ");
}

const VOID_TAGS = new Set(["area", "base", "br", "col", "embed", "hr", "img", "input", "link", "meta", "param", "source", "track", "wbr"]);

function escapeHTML(value: string): string {
  return value.replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;").replace(/"/g, "&quot;");
}

/** Serialize a node back to HTML for the "Copy HTML" action. */
export function serializeNode(node: DomNode): string {
  if (node.tag === "#text") return escapeHTML(node.text ?? "");
  const attrs = Object.entries(node.attrs)
    .map(([k, v]) => ` ${k}="${escapeHTML(v)}"`)
    .join("");
  const open = `<${node.tag}${attrs}>`;
  if (VOID_TAGS.has(node.tag)) return open;
  const kids = (node.children ?? []).map(serializeNode).join("");
  return `${open}${kids}</${node.tag}>`;
}

/** One-line row label: `<div#app.root>` plus a short attribute excerpt. */
export function rowLabel(node: DomNode): string {
  if (node.tag === "#text") return `"${node.text ?? ""}"`;
  const { id, class: className, ...rest } = node.attrs;
  let out = node.tag;
  if (id) out += `#${id}`;
  const classes = (className ?? "").trim().split(/\s+/).filter(Boolean);
  if (classes.length > 0) out += `.${classes.slice(0, 2).join(".")}`;
  const extra = Object.keys(rest)
    .filter((k) => k !== "style")
    .slice(0, 2)
    .map((k) => ` ${k}="${rest[k]}"`)
    .join("");
  return out + extra;
}

/** The node itself or its nearest ancestor that can be highlighted. */
export function highlightTarget(node: DomNode | null): DomNode | null {
  return node && node.tag !== "#text" ? node : null;
}
