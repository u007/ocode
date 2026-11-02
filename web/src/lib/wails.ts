type WailsWindow = Window & {
  _wails?: {
    invoke?: (message: string) => void;
  };
};

// The SPA is served by ocode's HTTP server rather than Wails' asset server,
// so the full Wails runtime does not post the ready message for us. The
// desktop shell calls this until its minimal bridge has been injected.
export function notifyWailsRuntimeReady(target: Window = window): boolean {
  const invoke = (target as WailsWindow)._wails?.invoke;
  if (!invoke) {
    return false;
  }
  invoke("wails:runtime:ready");
  return true;
}

/**
 * Send a raw message to the desktop shell. Messages that do not start with
 * "wails:" reach application.Options.RawMessageHandler on the Go side; this is
 * how the SPA talks to native code without loading the full runtime module.
 *
 * Returns false in a plain browser (no bridge), so callers can no-op.
 */
export function invokeWails(message: string, target: Window = window): boolean {
  const invoke = (target as WailsWindow)._wails?.invoke;
  if (!invoke) {
    return false;
  }
  invoke(message);
  return true;
}

/**
 * Ask the desktop shell to raise and focus its window.
 *
 * Used when the user opens the Pulse dashboard from the tray or the app menu
 * while ocode is in the background: the page already switched views, but
 * without this the new view appears behind whatever app is in front, which
 * reads as "nothing happened". Returns false in a plain browser.
 */
export function focusDesktopWindow(target: Window = window): boolean {
  return invokeWails("ocode:focus-window", target);
}

// The per-launch secret the desktop shell appends to the window URL as ?clip=
// (cmd/ocode-desktop/main.go). Read once at module load: the router may rewrite
// the location later, and the shell only accepts clipboard writes and print
// requests that echo this value.
const clipboardNonce = new URLSearchParams(window.location.search).get("clip");

/**
 * Put text on the system clipboard through the desktop shell's native
 * clipboard (cmd/ocode-desktop/main.go msgClipboardWrite). Unlike
 * navigator.clipboard and execCommand("copy"), WKWebView does not require it
 * to run inside a user gesture, so a copy that arrives over a websocket (a
 * terminal program's OSC 52) still lands. Returns false in a plain browser,
 * and when the page was not opened with the clipboard nonce.
 */
export function writeDesktopClipboard(text: string, target: Window = window): boolean {
  if (!clipboardNonce) {
    return false;
  }
  return invokeWails(`ocode:clipboard-write:${clipboardNonce}:${text}`, target);
}

/**
 * Largest print request sent over the bridge, in UTF-8 bytes of the whole
 * message (prefix, nonce and document). The shell enforces no size limit of its
 * own; this cap is the SPA's guard against an unbounded single bridge string.
 */
export const MAX_PRINT_DOCUMENT_BYTES = 8 * 1024 * 1024;

/**
 * Hand a complete HTML document to the desktop shell for printing. The shell
 * renders it in its own native window and runs the native print panel, because
 * WKWebView ignores window.print(). See cmd/ocode-desktop/print.go. Returns
 * false in a plain browser, and when the page was not opened with the
 * clipboard nonce (the shell checks the same nonce for print requests).
 *
 * Throws when the request exceeds MAX_PRINT_DOCUMENT_BYTES; nothing is sent.
 *
 * The bridge has no acknowledgement: invokeWails returns void on the shell side
 * and the shell sends no reply. A `true` result therefore means the message was
 * posted, not that the shell accepted or printed it. A refusal (bad nonce, sender
 * outside the ocode page) is only logged on the Go side.
 */
export function printDesktopDocument(html: string, target: Window = window): boolean {
  if (!clipboardNonce) {
    return false;
  }
  const message = `ocode:print-document:${clipboardNonce}:${html}`;
  const bytes = new TextEncoder().encode(message).length;
  if (bytes > MAX_PRINT_DOCUMENT_BYTES) {
    throw new Error(`print: document is ${bytes} bytes, over the ${MAX_PRINT_DOCUMENT_BYTES}-byte print limit`);
  }
  return invokeWails(message, target);
}

/**
 * Largest file saved through the desktop shell, in decoded bytes. The shell
 * enforces the same limit (cmd/ocode-desktop/save_file.go, desktopSaveMaxBytes);
 * this is the SPA's guard before the bytes are base64-encoded into one bridge string.
 */
export const MAX_DESKTOP_SAVE_BYTES = 64 * 1024 * 1024;

/** True when the page runs inside the desktop shell's webview. */
export function isDesktopShell(target: Window = window): boolean {
  return Boolean((target as WailsWindow)._wails?.invoke);
}

/**
 * Save bytes through the desktop shell's native save panel. WKWebView drops a
 * blob `<a download>` click without any error, so a desktop save has to go
 * through the shell (cmd/ocode-desktop/save_file.go). The shell sends no reply:
 * a return means the request was posted, while a refusal or a failed write is
 * logged and shown by the shell.
 *
 * Throws when the page has no clipboard nonce (the shell refuses the save), when
 * the bridge is missing, or when the file exceeds MAX_DESKTOP_SAVE_BYTES. Nothing
 * is sent in those cases.
 */
export function saveDesktopFile(name: string, data: Uint8Array, target: Window = window): void {
  if (!clipboardNonce) {
    throw new Error(`save: ${name} not sent, the page has no desktop nonce`);
  }
  if (data.byteLength > MAX_DESKTOP_SAVE_BYTES) {
    throw new Error(`save: ${name} is ${data.byteLength} bytes, over the ${MAX_DESKTOP_SAVE_BYTES}-byte desktop save limit`);
  }
  const body = JSON.stringify({ name, data: bytesToBase64(data) });
  if (!invokeWails(`ocode:save-file:${clipboardNonce}:${body}`, target)) {
    throw new Error(`save: ${name} not sent, the desktop bridge is missing`);
  }
}

/** Base64 without spreading the whole array into one call (that overflows the stack for multi-MB files). */
function bytesToBase64(bytes: Uint8Array): string {
  const chunkSize = 0x8000;
  let binary = "";
  for (let i = 0; i < bytes.length; i += chunkSize) {
    binary += String.fromCharCode(...bytes.subarray(i, i + chunkSize));
  }
  return btoa(binary);
}
