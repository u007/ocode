import { isDesktopShell } from "../../lib/desktopShell";
import { printDesktopDocument } from "../../lib/wails";

// The print hand-off shared by every printable surface: wraps an HTML document
// and opens the print dialog through the desktop bridge or a hidden sandboxed
// iframe. Kept apart from printPreview.tsx so PdfViewer can use it without
// pulling in the api client and the preview-kind graph.

const PRINT_MESSAGE = "ocode:print";
const PRINT_LOAD_TIMEOUT_MS = 15_000;
const PRINT_FRAME_LIFETIME_MS = 60_000;

// Runs inside every printed document. The parent cannot call print() on a
// sandboxed frame, so it posts PRINT_MESSAGE instead. On the desktop shell the
// load event tells the native window that its document is ready to print.
export const PRINT_BOOTSTRAP = `<script>
window.addEventListener("message", function (event) {
  if (event.data === "ocode:print") { window.focus(); window.print(); }
});
window.addEventListener("load", function () {
  if (window._wails && window._wails.invoke) { window._wails.invoke("ocode:print-window-ready"); }
});
</script>`;

export function escapeHtml(text: string): string {
  return text.replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;").replace(/"/g, "&quot;");
}

export function documentShell(title: string, head: string, body: string): string {
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
