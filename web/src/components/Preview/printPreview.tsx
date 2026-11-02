import type { PreviewKind } from "../../lib/previewKind";
import { isDesktopShell } from "../../lib/desktopShell";
import { printDesktopDocument } from "../../lib/wails";

// Print / Save as PDF for the rendered preview surfaces. The document is built
// from the file CONTENT, not from the live DOM, so it works while the editor is
// in Edit mode. Browsers print it from a hidden sandboxed iframe; the desktop
// shell (WKWebView ignores window.print()) receives the whole document over the
// wails bridge and prints it from its own window.

export const PRINTABLE_KINDS: ReadonlySet<PreviewKind> = new Set<PreviewKind>(["markdown", "html", "text", "json"]);

export function isPrintableKind(kind: PreviewKind): boolean {
  return PRINTABLE_KINDS.has(kind);
}

const PRINT_MESSAGE = "ocode:print";
const PRINT_LOAD_TIMEOUT_MS = 15_000;
const PRINT_FRAME_LIFETIME_MS = 60_000;

const PAGE_CSS = "@page { margin: 16mm } html, body { background: #fff; color: #111; } article { padding: 0; }";

// Runs inside every printed document. The parent cannot call print() on a
// sandboxed frame, so it posts PRINT_MESSAGE instead. On the desktop shell the
// load event tells the native window that its document is ready to print.
const PRINT_BOOTSTRAP = `<script>
window.addEventListener("message", function (event) {
  if (event.data === "ocode:print") { window.focus(); window.print(); }
});
window.addEventListener("load", function () {
  if (window._wails && window._wails.invoke) { window._wails.invoke("ocode:print-window-ready"); }
});
</script>`;

/**
 * Rewrites every relative url(...) in a stylesheet to an absolute URL resolved
 * against `sheetHref`, so the CSS still resolves once it is inlined into a
 * document with no origin. data: and blob: URIs and #fragment refs (SVG
 * mask/filter targets) are left exactly as written.
 */
function absolutizeCssUrls(css: string, sheetHref: string): string {
  return css.replace(/url\(\s*(?:"([^"]*)"|'([^']*)'|([^'")\s]+))\s*\)/g, (match, doubleQuoted, singleQuoted, bare) => {
    const ref: string = doubleQuoted ?? singleQuoted ?? bare;
    if (/^(data:|blob:|#|$)/.test(ref)) return match;
    return `url("${new URL(ref, sheetHref).href}")`;
  });
}

async function fetchSheet(href: string): Promise<string | null> {
  try {
    const res = await fetch(href);
    if (!res.ok) throw new Error(`HTTP ${res.status}`);
    return absolutizeCssUrls(await res.text(), href);
  } catch (err) {
    console.error("print preview: could not read stylesheet, skipping it", { href }, err);
    return null;
  }
}

/**
 * Inlines the app's CSS so a printed Markdown document gets the real `.prose`
 * and Tailwind styles. Inlined rather than linked because the desktop shell
 * renders the document in a window with no origin to resolve `href` against.
 *
 * Only same-origin stylesheets are read: a cross-origin sheet is not the app's
 * own CSS and its fetch is CORS-blocked, so it is skipped without a log.
 */
export async function collectAppStyles(): Promise<string> {
  const links = Array.from(document.head.querySelectorAll<HTMLLinkElement>('link[rel="stylesheet"]')).filter(
    (link) => new URL(link.href).origin === window.location.origin,
  );
  const sheets = await Promise.all(links.map((link) => fetchSheet(link.href)));
  const linked = sheets.filter((text): text is string => text !== null).map((text) => `<style>${text}</style>`);
  // Vite dev injects the app's own CSS as <style data-vite-dev-id>. Other <style>
  // elements in head belong to runtime libraries (Monaco, toasts) and are left out.
  const inline = Array.from(document.head.querySelectorAll<HTMLStyleElement>("style[data-vite-dev-id]")).map(
    (el) => `<style>${absolutizeCssUrls(el.textContent ?? "", document.baseURI)}</style>`,
  );
  return [...linked, ...inline].join("\n");
}

function escapeHtml(text: string): string {
  return text.replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;").replace(/"/g, "&quot;");
}

function documentShell(title: string, head: string, body: string): string {
  return `<!doctype html>
<html>
<head>
<meta charset="utf-8">
<title>${escapeHtml(title)}</title>
${head}
</head>
<body>
${body}
${PRINT_BOOTSTRAP}
</body>
</html>`;
}

/**
 * Full standalone HTML document for a preview. `html` content is untrusted and
 * is printed verbatim: only the bootstrap script is appended.
 */
export async function buildPrintDocument(kind: PreviewKind, content: string, title: string): Promise<string> {
  switch (kind) {
    case "markdown": {
      const [{ renderToStaticMarkup }, { default: ReactMarkdown }, { default: remarkGfm }] = await Promise.all([
        import("react-dom/server"),
        import("react-markdown"),
        import("remark-gfm"),
      ]);
      const markup = renderToStaticMarkup(<ReactMarkdown remarkPlugins={[remarkGfm]}>{content}</ReactMarkdown>);
      const styles = await collectAppStyles();
      return documentShell(title, `${styles}\n<style>${PAGE_CSS}</style>`, `<article class="prose max-w-none">${markup}</article>`);
    }
    case "text":
    case "json":
      return documentShell(
        title,
        `<style>${PAGE_CSS}</style>`,
        `<pre style="white-space: pre-wrap; word-break: break-word; font: 11px/1.45 ui-monospace, SFMono-Regular, Menlo, monospace; margin: 0">${escapeHtml(content)}</pre>`,
      );
    case "html":
      return `${content}${PRINT_BOOTSTRAP}`;
    default:
      throw new Error(`print: unsupported preview kind "${kind}"`);
  }
}

/**
 * Opens the native print dialog for a document built by buildPrintDocument.
 * Resolves once the print request has been handed off: accepted by the desktop
 * bridge, or posted to the loaded browser frame. Rejects when the bridge refuses
 * the document, the frame does not load within PRINT_LOAD_TIMEOUT_MS, or the
 * loaded frame has no window.
 */
export async function printDocument(html: string): Promise<void> {
  if (isDesktopShell()) {
    if (!printDesktopDocument(html)) throw new Error("print: desktop bridge unavailable");
    return;
  }
  const frame = document.createElement("iframe");
  frame.style.position = "fixed";
  frame.style.right = "0";
  frame.style.bottom = "0";
  frame.style.width = "1px";
  frame.style.height = "1px";
  frame.style.opacity = "0";
  frame.style.border = "0";
  frame.setAttribute("aria-hidden", "true");
  frame.setAttribute("sandbox", "allow-scripts allow-modals");
  frame.tabIndex = -1;
  frame.setAttribute("srcdoc", html);
  await new Promise<void>((resolve, reject) => {
    const watchdog = window.setTimeout(() => {
      frame.remove();
      reject(new Error(`print: preview frame did not load within ${PRINT_LOAD_TIMEOUT_MS} ms`));
    }, PRINT_LOAD_TIMEOUT_MS);
    frame.onload = () => {
      window.clearTimeout(watchdog);
      const target = frame.contentWindow;
      if (!target) {
        frame.remove();
        reject(new Error("print: preview frame has no window after load"));
        return;
      }
      target.postMessage(PRINT_MESSAGE, "*");
      // The print dialog needs the frame alive while it is open.
      window.setTimeout(() => frame.remove(), PRINT_FRAME_LIFETIME_MS);
      resolve();
    };
    document.body.appendChild(frame);
  });
}

export async function printPreview(opts: { kind: PreviewKind; title: string; content: string }): Promise<void> {
  await printDocument(await buildPrintDocument(opts.kind, opts.content, opts.title));
}
