/**
 * In-iframe find agent for the HTML preview.
 *
 * The HTML surface renders untrusted markup in a `sandbox="allow-scripts"`
 * iframe (opaque origin), so the parent can neither read nor style its DOM.
 * The only channel is `postMessage`. The agent below is injected into the
 * `srcDoc`; it is deliberately self-contained (no imports, no module scope) so
 * `htmlFindAgent.toString()` can be serialized into the frame.
 *
 * Protocol (parent ↔ agent):
 *   parent → frame  { __ocodeFind: true, action: "search", query, caseSensitive, wholeWord }
 *                   { __ocodeFind: true, action: "step", dir: 1 | -1 }
 *                   { __ocodeFind: true, action: "close" }
 *   frame → parent  { __ocodeFindResult: true, count, current }
 *                   { __ocodeFindReady: true }
 *
 * Results are display-only; a hostile page could forge them, which buys it
 * nothing beyond a wrong match counter.
 */

export const HTML_FIND_MESSAGE = "__ocodeFind";
export const HTML_FIND_RESULT = "__ocodeFindResult";
export const HTML_FIND_READY = "__ocodeFindReady";

/**
 * Installs the agent and returns its API (the parent never uses the return
 * value — it is what makes the agent unit-testable).
 */
export function htmlFindAgent() {
  const w = window as unknown as Record<string, unknown>;
  if (w.__ocodeFindAgent) return w.__ocodeFindAgent as HtmlFindAgent;

  const MSG = "__ocodeFind";
  const RESULT = "__ocodeFindResult";
  const READY = "__ocodeFindReady";
  // Kept INSIDE the function on purpose: `htmlFindAgent.toString()` is what
  // runs in the frame, so any module-scope reference becomes an undefined
  // identifier there. Cap on painted rects (a pathological query must not hang
  // the page); matches past the cap are still counted and stepped over.
  const MAX_RECTS = 3000;

  type Seg = { node: Text; start: number; len: number };
  type Index = { text: string; segments: Seg[] };
  type Match = { start: number; end: number };
  type Point = { node: Text; offset: number };

  const SKIP: Record<string, number> = { SCRIPT: 1, STYLE: 1, NOSCRIPT: 1, TEMPLATE: 1 };
  const BLOCK: Record<string, number> = {};
  "ADDRESS,ARTICLE,ASIDE,BLOCKQUOTE,BR,CAPTION,DD,DIV,DL,DT,FIGCAPTION,FIGURE,FOOTER,FORM,H1,H2,H3,H4,H5,H6,HEADER,HR,LI,MAIN,NAV,OL,P,PRE,SECTION,TABLE,TBODY,TD,TFOOT,TH,THEAD,TR,UL"
    .split(",")
    .forEach((tag) => {
      BLOCK[tag] = 1;
    });

  let index: Index | null = null;
  let matches: Match[] = [];
  let current = 0;
  let caseSensitive = false;
  let wholeWord = false;
  let layer: HTMLDivElement | null = null;
  let raf = 0;
  let listening = false;

  function escapeRe(s: string): string {
    return s.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
  }

  function buildIndex(root: Node): Index {
    const segments: Seg[] = [];
    let text = "";
    const walk = (node: Node) => {
      if (node.nodeType === 3) {
        const data = (node as Text).data;
        if (!data) return;
        segments.push({ node: node as Text, start: text.length, len: data.length });
        text += data;
        return;
      }
      if (node.nodeType !== 1) return;
      const el = node as Element;
      const tag = el.tagName.toUpperCase();
      if (SKIP[tag]) return;
      if (el.id === "__ocode_find_layer") return;
      const block = !!BLOCK[tag];
      if (block) text += "\n";
      const kids = Array.from(el.childNodes);
      for (const kid of kids) walk(kid);
      if (block) text += "\n";
    };
    const kids = Array.from(root.childNodes);
    for (const kid of kids) walk(kid);
    return { text, segments };
  }

  function findIn(ix: Index, needle: string): Match[] {
    if (!needle) return [];
    const flags = caseSensitive ? "g" : "gi";
    const body = wholeWord ? "\\b(?:" + escapeRe(needle) + ")\\b" : escapeRe(needle);
    let re: RegExp;
    try {
      re = new RegExp(body, flags);
    } catch {
      return [];
    }
    const out: Match[] = [];
    let m = re.exec(ix.text);
    while (m) {
      if (!m[0].length) {
        re.lastIndex++;
      } else {
        out.push({ start: m.index, end: m.index + m[0].length });
      }
      m = re.exec(ix.text);
    }
    return out;
  }

  function pointAt(ix: Index, offset: number, preferNext: boolean): Point | null {
    for (const seg of ix.segments) {
      if (offset >= seg.start && offset < seg.start + seg.len) {
        return { node: seg.node, offset: offset - seg.start };
      }
    }
    if (preferNext) {
      for (const seg of ix.segments) {
        if (seg.start >= offset) return { node: seg.node, offset: 0 };
      }
      const last = ix.segments[ix.segments.length - 1];
      return last ? { node: last.node, offset: last.len } : null;
    }
    let prev: Seg | null = null;
    for (const seg of ix.segments) {
      if (seg.start < offset || (offset === 0 && seg.start === 0)) prev = seg;
    }
    return prev ? { node: prev.node, offset: prev.len } : null;
  }

  function rangeFor(ix: Index, match: Match): Range | null {
    const start = pointAt(ix, match.start, true);
    const end = pointAt(ix, match.end, false);
    if (!start || !end) return null;
    try {
      const range = document.createRange();
      range.setStart(start.node, Math.min(start.offset, start.node.data.length));
      range.setEnd(end.node, Math.min(end.offset, end.node.data.length));
      return range;
    } catch {
      return null;
    }
  }

  function removeLayer() {
    if (layer && layer.parentNode) layer.parentNode.removeChild(layer);
    layer = null;
  }

  function paint() {
    removeLayer();
    // Defensive: a layer left by an earlier injection (or a page that cloned
    // the DOM) must not shadow the one we are about to paint.
    const stale = document.getElementById("__ocode_find_layer");
    if (stale && stale.parentNode) stale.parentNode.removeChild(stale);
    if (!matches.length || !index) return;
    const doc = document.documentElement;
    if (!doc) return;
    const sx = window.scrollX || doc.scrollLeft || 0;
    const sy = window.scrollY || doc.scrollTop || 0;
    const next = document.createElement("div");
    next.id = "__ocode_find_layer";
    next.style.cssText =
      "position:absolute;left:0;top:0;width:0;height:0;pointer-events:none;z-index:2147483647;";
    let painted = 0;
    for (let i = 0; i < matches.length && painted < MAX_RECTS; i++) {
      const range = rangeFor(index, matches[i]);
      const clientRects = range && typeof range.getClientRects === "function" ? range.getClientRects() : null;
      if (!clientRects) continue;
      for (let j = 0; j < clientRects.length && painted < MAX_RECTS; j++) {
        const cr = clientRects[j];
        if (cr.width <= 0 || cr.height <= 0) continue;
        const box = document.createElement("div");
        box.style.cssText =
          "position:absolute;border-radius:2px;pointer-events:none;background:" +
          (i === current ? "rgba(249,115,22,0.60)" : "rgba(250,204,21,0.45)") +
          ";left:" + (cr.left + sx) + "px;top:" + (cr.top + sy) + "px;width:" + cr.width + "px;height:" + cr.height + "px;";
        next.appendChild(box);
        painted++;
      }
    }
    doc.appendChild(next);
    layer = next;
  }

  function reveal() {
    if (!index || !matches.length) return;
    const range = rangeFor(index, matches[current]);
    const node = range ? range.startContainer : null;
    const el = node ? ((node.nodeType === 1 ? node : node.parentElement) as HTMLElement | null) : null;
    if (!el || typeof el.scrollIntoView !== "function") return;
    try {
      el.scrollIntoView({ block: "center", inline: "nearest" });
    } catch {
      el.scrollIntoView();
    }
  }

  function schedule() {
    if (raf) return;
    raf = requestAnimationFrame(() => {
      raf = 0;
      paint();
    });
  }

  function attach() {
    if (listening) return;
    listening = true;
    // Capture: the page may scroll an inner container instead of the document.
    window.addEventListener("scroll", schedule, true);
    window.addEventListener("resize", schedule);
  }

  function detach() {
    if (!listening) return;
    listening = false;
    window.removeEventListener("scroll", schedule, true);
    window.removeEventListener("resize", schedule);
  }

  function report() {
    try {
      window.parent.postMessage({ [RESULT]: true, count: matches.length, current: current }, "*");
    } catch {
      // Cross-origin parent refused the message: nothing else to do, and
      // logging inside the previewed page would be noise.
    }
  }

  function search(query: string) {
    index = buildIndex(document.body || document.documentElement);
    matches = findIn(index, query);
    current = 0;
    if (matches.length) attach();
    else detach();
    paint();
    if (matches.length) reveal();
    report();
  }

  function step(dir: number) {
    if (!matches.length) {
      report();
      return;
    }
    current = (current + (dir === -1 ? -1 : 1) + matches.length) % matches.length;
    paint();
    reveal();
    report();
  }

  function clear() {
    matches = [];
    index = null;
    current = 0;
    removeLayer();
    detach();
    report();
  }

  const api = {
    search: (query: string, opts?: { caseSensitive?: boolean; wholeWord?: boolean }) => {
      caseSensitive = !!opts?.caseSensitive;
      wholeWord = !!opts?.wholeWord;
      search(query);
    },
    step,
    clear,
    matches: () => matches.slice(),
    current: () => current,
  };
  w.__ocodeFindAgent = api;

  window.addEventListener("message", (event: MessageEvent) => {
    if (event.source !== window.parent) return;
    const data = event.data as Record<string, unknown> | null;
    if (!data || data[MSG] !== true) return;
    if (data.action === "search") {
      caseSensitive = !!data.caseSensitive;
      wholeWord = !!data.wholeWord;
      search(typeof data.query === "string" ? data.query : "");
    } else if (data.action === "step") {
      step(data.dir === -1 ? -1 : 1);
    } else if (data.action === "close") {
      clear();
    }
  });

  const announce = () => {
    try {
      window.parent.postMessage({ [READY]: true }, "*");
    } catch {
      // Same cross-origin caveat as report().
    }
  };
  if (document.readyState === "loading") document.addEventListener("DOMContentLoaded", announce);
  else announce();

  return api;
}

export type HtmlFindAgent = {
  search: (query: string, opts?: { caseSensitive?: boolean; wholeWord?: boolean }) => void;
  step: (dir: number) => void;
  clear: () => void;
  matches: () => { start: number; end: number }[];
  current: () => number;
};

/** Append the find agent to a `srcDoc` payload. */
export function buildHtmlFindSrcDoc(html: string): string {
  const tag = "<script>(" + htmlFindAgent.toString() + ")();</script>";
  const lower = html.toLowerCase();
  const bodyClose = lower.lastIndexOf("</body>");
  if (bodyClose !== -1) return html.slice(0, bodyClose) + tag + html.slice(bodyClose);
  const htmlClose = lower.lastIndexOf("</html>");
  if (htmlClose !== -1) return html.slice(0, htmlClose) + tag + html.slice(htmlClose);
  return html + tag;
}
