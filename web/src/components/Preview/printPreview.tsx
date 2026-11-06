import type { ICellData } from "@univerjs/core";
import DOMPurify, { type Config } from "dompurify";
import { api } from "../../api/client";
import { imageMimeForPath, type PreviewKind } from "../../lib/previewKind";
import { PRINT_BOOTSTRAP, documentShell, escapeHtml, printDocument } from "./printDocument";
import { runMermaid } from "./mermaidQueue";
import type { ConvertedWorkbook } from "./univerData";

// Print / Save as PDF for the rendered preview surfaces. The document is built
// from the file CONTENT, not from the live DOM, so it works while the editor is
// in Edit mode. Browsers print it from a hidden sandboxed iframe; the desktop
// shell (WKWebView ignores window.print()) receives the whole document over the
// wails bridge and prints it from its own window.
//
// Each kind is built by the same code its viewer runs (the Excel converter, the
// PPTX parser and SlideCard, docx-preview, mermaid). Heavy renderers are dynamic
// imports so they stay out of the entry chunk.

export const PRINTABLE_KINDS: ReadonlySet<PreviewKind> = new Set<PreviewKind>([
  "markdown",
  "html",
  "text",
  "json",
  "mermaid",
  "image",
  "excel",
  "docx",
  "pptx",
]);

export function isPrintableKind(kind: PreviewKind): boolean {
  return PRINTABLE_KINDS.has(kind);
}

const PAGE_CSS = "@page { margin: 16mm } html, body { background: #fff; color: #111; } article { padding: 0; }";

// Every body embedded in the print document is untrusted: the html kind is the
// user's file, and docx, pptx and mermaid markup comes from file content. Each
// call passes its own config. The shared DOMPurify instance gets no hooks or
// global settings, because mermaid sanitizes its labels with that same instance.

/**
 * Mermaid labels are HTML inside foreignObject. DOMPurify drops that HTML unless
 * foreignObject is an HTML integration point, so the labels would print blank.
 */
const MERMAID_SVG_CONFIG: Config = {
  USE_PROFILES: { svg: true, svgFilters: true, html: true },
  ADD_TAGS: ["foreignObject"],
  HTML_INTEGRATION_POINTS: { foreignobject: true },
};

/** The author's file as a whole document, so its <head> styles survive. <link> keeps stylesheet references. */
const HTML_DOCUMENT_CONFIG: Config = { WHOLE_DOCUMENT: true, ADD_TAGS: ["link"] };

/** The author's doctype. Without it the printed page would render in quirks mode, unlike the viewer. */
const HTML_DOCTYPE = /^\s*<!doctype html\b[^>]*>/i;

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

/**
 * Where a preview's document is read from. `editor` is the unsaved text the
 * Files-tab split view shows, so Edit mode prints what is typed. `disk` is the
 * saved file, read through the same api its viewer uses (remote projects too).
 */
export type PrintSource =
  | { origin: "editor"; path: string; content: string }
  | { origin: "disk"; path: string; projectRoot?: string; projectHost?: string };

async function textOf(source: PrintSource): Promise<string> {
  if (source.origin === "editor") return source.content;
  return (await api.getFileContent(source.path, source.projectRoot, source.projectHost)).content;
}

async function bytesOf(source: PrintSource): Promise<ArrayBuffer> {
  if (source.origin === "editor") throw new Error(`print: ${source.path} can only be printed from the saved file`);
  return api.fetchFileRaw(source.path, source.projectRoot, source.projectHost);
}

function blobToDataUrl(blob: Blob): Promise<string> {
  return new Promise((resolve, reject) => {
    const reader = new FileReader();
    reader.onload = () => resolve(String(reader.result));
    reader.onerror = () => reject(new Error(`print: could not read image bytes: ${String(reader.error)}`));
    reader.readAsDataURL(blob);
  });
}

const IMAGE_PRINT_CSS = "img { display: block; max-width: 100%; max-height: 100vh; object-fit: contain; margin: 0 auto }";

async function imageSrc(source: PrintSource): Promise<string> {
  const mime = imageMimeForPath(source.path);
  if (mime === null) throw new Error(`print: ${source.path} is not an image`);
  if (source.origin === "editor") {
    // Only SVG has editable text (the split view); raster files are binary.
    if (mime !== "image/svg+xml") throw new Error(`print: ${source.path} has no editable text`);
    return `data:${mime};charset=utf-8,${encodeURIComponent(source.content)}`;
  }
  return blobToDataUrl(new Blob([await bytesOf(source)], { type: mime }));
}

const MERMAID_PRINT_CSS = "svg { max-width: 100%; height: auto }";

const MARKDOWN_PRINT_CSS =
  ".print-mermaid { margin: 1em 0; break-inside: avoid } .print-mermaid svg { max-width: 100%; height: auto }";

/** A fenced ```mermaid block in a Markdown file, with its source in group 1. */
const MERMAID_FENCE = /^[ \t]*```mermaid[ \t]*\r?\n([\s\S]*?)^[ \t]*```[ \t]*\r?$/gm;

/** What react-markdown emits for a fenced block (no language plugin is installed). */
const MERMAID_CODE_HTML = /<pre><code class="language-mermaid">[\s\S]*?<\/code><\/pre>/g;

/**
 * The SVG the mermaid viewer draws, rendered again on the light theme for paper.
 * mermaid's config is one global, so the viewer's theme and security level are
 * restored after the render. `strict` keeps label HTML sanitized in the printed SVG.
 */
async function mermaidSvg(code: string): Promise<string> {
  const { default: mermaid } = await import("mermaid");
  // The whole config read, initialize, render and restore run as one queued task,
  // so no viewer render can interleave with them (see mermaidQueue.ts).
  const svg = await runMermaid(async () => {
    // mermaid 11 has no public getter; getConfig lives on mermaidAPI (typed internal).
    const viewer = mermaid.mermaidAPI.getConfig();
    mermaid.initialize({ startOnLoad: false, securityLevel: "strict", theme: "default" });
    try {
      const { svg } = await mermaid.render(`mmd-print-${Math.random().toString(36).slice(2, 9)}`, code);
      return svg;
    } finally {
      mermaid.initialize({ startOnLoad: false, securityLevel: viewer.securityLevel, theme: viewer.theme });
    }
  });
  return DOMPurify.sanitize(svg, MERMAID_SVG_CONFIG);
}

/** Every fenced mermaid block in a Markdown file, in document order. */
function mermaidSources(markdown: string): string[] {
  return Array.from(markdown.matchAll(MERMAID_FENCE), (match) => match[1]!.trim());
}

/**
 * Replaces each fenced mermaid block react-markdown emitted with the diagram it
 * draws on the light theme, so the printed page shows the chart where the
 * author wrote it instead of its source. A diagram that fails to render keeps
 * its code block: one broken diagram must not block printing the whole
 * document (the `.mmd` kind propagates instead, because there the diagram IS
 * the document).
 */
async function inlineMermaid(markup: string, sources: string[]): Promise<string> {
  const blocks = Array.from(markup.matchAll(MERMAID_CODE_HTML));
  if (blocks.length === 0) return markup;
  const parts: string[] = [];
  let cursor = 0;
  for (let i = 0; i < blocks.length; i++) {
    const block = blocks[i]!;
    const at = block.index ?? cursor;
    parts.push(markup.slice(cursor, at));
    const code = sources[i];
    let replacement = block[0];
    if (code !== undefined) {
      try {
        replacement = `<div class="print-mermaid">${await mermaidSvg(code)}</div>`;
      } catch (err) {
        console.error("print preview: could not render a mermaid diagram, printing its source", err);
      }
    }
    parts.push(replacement);
    cursor = at + block[0].length;
  }
  parts.push(markup.slice(cursor));
  return parts.join("");
}

const EXCEL_PRINT_CSS =
  "table { border-collapse: collapse; font: 10px/1.35 ui-sans-serif, system-ui, sans-serif } td { border: 1px solid #b8c0cc; padding: 2px 5px; vertical-align: top; white-space: pre-wrap } h2 { font: 600 13px ui-sans-serif, system-ui, sans-serif; margin: 0 0 6px } section + section { page-break-before: always } .note { color: #555; font: 10px ui-sans-serif, system-ui, sans-serif; margin: 0 0 6px }";

async function excelWorkbook(source: PrintSource): Promise<ConvertedWorkbook> {
  // The same converters ExcelViewer runs: live CSV/TSV text, or the file bytes.
  const { workbookFromDelimitedText, workbookFromFileBytes } = await import("./univerData");
  if (source.origin === "editor") return workbookFromDelimitedText(source.content);
  return workbookFromFileBytes(source.path, await bytesOf(source));
}

function cellText(cell: ICellData | undefined): string {
  if (cell?.v === undefined || cell.v === null) return "";
  return String(cell.v);
}

/** An empty cell gets a non-breaking space so its row keeps the height of a text row. */
function cellHtml(cell: ICellData | undefined): string {
  const text = cellText(cell);
  return `<td>${text === "" ? "&nbsp;" : escapeHtml(text)}</td>`;
}

/**
 * One section per sheet: the sheet name, then its cells as a table. Sheet names
 * and cell text are escaped; the table covers the same rows and columns the
 * viewer shows (the converter already applied its caps). Rows are walked by
 * index up to the last populated one, so blank rows between data keep their place.
 */
function excelBody(book: ConvertedWorkbook): string {
  return book.sheets
    .map((sheet) => {
      const heading = `<h2>${escapeHtml(sheet.name)}</h2>`;
      if (sheet.isEmpty) return `<section>${heading}<p class="note">(empty sheet)</p></section>`;
      const ws = book.data.sheets[sheet.id];
      const cellData = ws?.cellData;
      if (cellData === undefined) throw new Error(`print: sheet "${sheet.name}" has no cell data`);
      let lastRow = -1;
      let width = 0;
      for (const key of Object.keys(cellData)) {
        const r = Number(key);
        lastRow = Math.max(lastRow, r);
        for (const c of Object.keys(cellData[r])) width = Math.max(width, Number(c) + 1);
      }
      const rowParts: string[] = [];
      for (let r = 0; r <= lastRow; r++) {
        const row: Record<number, ICellData> | undefined = cellData[r];
        const cells: string[] = [];
        for (let c = 0; c < width; c++) cells.push(cellHtml(row === undefined ? undefined : row[c]));
        rowParts.push(`<tr>${cells.join("")}</tr>`);
      }
      const rows = rowParts.join("");
      const truncated = sheet.rowCapped || sheet.colCapped ? `<p class="note">Large sheet truncated: only the first rows and columns are printed.</p>` : "";
      return `<section>${heading}${truncated}<table>${rows}</table></section>`;
    })
    .join("\n");
}

const DOCX_PRINT_CSS =
  "@page { margin: 0 } body { margin: 0 } .docx-wrapper { background: #fff !important; padding: 0 !important } section.docx { box-shadow: none !important; margin: 0 !important; max-width: 100% !important; break-after: page } section.docx:last-child { break-after: auto }";

/**
 * docx-preview renders the same DOM the Word viewer shows. Its <style> goes to
 * a separate container so it can be inlined into <head>. useBase64URL inlines
 * images: blob: URLs would not resolve in the print window (no origin).
 */
async function docxParts(source: PrintSource): Promise<{ styles: string; body: string }> {
  const { renderAsync } = await import("docx-preview");
  const body = document.createElement("div");
  const styles = document.createElement("div");
  await renderAsync(await bytesOf(source), body, styles, { inWrapper: true, useBase64URL: true });
  // docx-preview copies each hyperlink target verbatim. Keep web, mail and
  // in-page links only: a javascript: href would be live in the print window.
  for (const a of Array.from(body.querySelectorAll("a[href]"))) {
    if (!/^(https?:|mailto:|#)/i.test(a.getAttribute("href") ?? "")) a.removeAttribute("href");
  }
  // FORCE_BODY keeps a leading <style> in place; the parser would move it to <head>.
  // Styles are sanitized too: a font name from the file lands in a <style> as raw
  // text, so a "</style><script>" in it would break out when the string is embedded.
  return {
    styles: DOMPurify.sanitize(styles.innerHTML, { FORCE_BODY: true }),
    body: DOMPurify.sanitize(body.innerHTML),
  };
}

const PPTX_PRINT_CSS = "@page { margin: 12mm } .slide { break-after: page } .slide:last-child { break-after: auto }";

/** Most slides one print document may hold. A deck above this is refused, never cut short. */
const PPTX_PRINT_MAX_SLIDES = 200;

/**
 * Each slide is the viewer's own SlideCard, with its images inlined as data URLs.
 * Slides are rendered one at a time so only one slide's images are inlined at once.
 */
async function pptxBody(source: PrintSource): Promise<string> {
  const [{ parsePptx }, { SlideCard }, { renderToStaticMarkup }] = await Promise.all([
    import("./pptxDeck"),
    import("./PptxViewer"),
    import("react-dom/server"),
  ]);
  const slides = await parsePptx(await bytesOf(source));
  if (slides.length > PPTX_PRINT_MAX_SLIDES) {
    throw new Error(`print: ${source.path} has ${slides.length} slides; printing is limited to ${PPTX_PRINT_MAX_SLIDES}`);
  }
  const pages: string[] = [];
  for (const slide of slides) {
    const images = await Promise.all(slide.images.map(blobToDataUrl));
    const card = renderToStaticMarkup(
      <SlideCard slide={{ aspect: slide.aspect, blocks: slide.blocks, images }} number={slide.index} forPresent={false} revealAll revealed={0} />,
    );
    pages.push(`<section class="slide">${DOMPurify.sanitize(card)}</section>`);
  }
  return pages.join("\n");
}

/**
 * Full standalone HTML document for a preview. `html` content is untrusted: it is
 * sanitized (scripts, event handlers and javascript: URLs removed), and only the
 * bootstrap script is appended.
 */
export async function buildPrintDocument(kind: PreviewKind, source: PrintSource, title: string): Promise<string> {
  switch (kind) {
    case "markdown": {
      const [{ renderToStaticMarkup }, { default: ReactMarkdown }, { default: remarkGfm }] = await Promise.all([
        import("react-dom/server"),
        import("react-markdown"),
        import("remark-gfm"),
      ]);
      const markdown = await textOf(source);
      const markup = renderToStaticMarkup(<ReactMarkdown remarkPlugins={[remarkGfm]}>{markdown}</ReactMarkdown>);
      const body = await inlineMermaid(markup, mermaidSources(markdown));
      const styles = await collectAppStyles();
      return documentShell(
        title,
        `${styles}\n<style>${PAGE_CSS}${MARKDOWN_PRINT_CSS}</style>`,
        `<article class="prose max-w-none">${body}</article>`,
      );
    }
    case "text":
    case "json":
      return documentShell(
        title,
        `<style>${PAGE_CSS}</style>`,
        `<pre style="white-space: pre-wrap; word-break: break-word; font: 11px/1.45 ui-monospace, SFMono-Regular, Menlo, monospace; margin: 0">${escapeHtml(await textOf(source))}</pre>`,
      );
    case "html": {
      const content = await textOf(source);
      const doctype = HTML_DOCTYPE.test(content) ? "<!doctype html>" : "";
      return `${doctype}${DOMPurify.sanitize(content, HTML_DOCUMENT_CONFIG)}${PRINT_BOOTSTRAP}`;
    }
    case "mermaid":
      return documentShell(title, `<style>${PAGE_CSS}${MERMAID_PRINT_CSS}</style>`, `<div>${await mermaidSvg(await textOf(source))}</div>`);
    case "image":
      return documentShell(
        title,
        `<style>${PAGE_CSS}${IMAGE_PRINT_CSS}</style>`,
        `<img src="${await imageSrc(source)}" alt="${escapeHtml(title)}">`,
      );
    case "excel":
      return documentShell(title, `<style>${PAGE_CSS}${EXCEL_PRINT_CSS}</style>`, excelBody(await excelWorkbook(source)));
    case "docx": {
      const { styles, body } = await docxParts(source);
      return documentShell(title, `${styles}\n<style>${DOCX_PRINT_CSS}</style>`, body);
    }
    case "pptx": {
      const styles = await collectAppStyles();
      return documentShell(title, `${styles}\n<style>${PAGE_CSS}${PPTX_PRINT_CSS}</style>`, await pptxBody(source));
    }
    default:
      throw new Error(`print: unsupported preview kind "${kind}"`);
  }
}

export async function printPreview(opts: { kind: PreviewKind; title: string; source: PrintSource }): Promise<void> {
  await printDocument(await buildPrintDocument(opts.kind, opts.source, opts.title));
}
