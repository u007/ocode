import { uuidv4 } from "./dbIds";

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
 * keep them equal. The shell streams the bytes to a temp file in bounded
 * chunks, so this cap does not multiply the webview's memory.
 */
export const MAX_DESKTOP_SAVE_BYTES = 2 * 1024 * 1024 * 1024;

/**
 * Decoded bytes per save chunk. Each chunk message lands on the bridge as
 * ~11 MB of JSON, small enough for the webview's IPC path; the shell writes
 * each chunk to the temp file as it arrives.
 */
export const SAVE_CHUNK_BYTES = 8 * 1024 * 1024;

/** Throws when a desktop save of `bytes` decoded bytes would exceed MAX_DESKTOP_SAVE_BYTES. */
export function assertDesktopSaveSize(name: string, bytes: number): void {
  if (bytes > MAX_DESKTOP_SAVE_BYTES) {
    throw new Error(`save: ${name} is ${bytes} bytes, over the ${MAX_DESKTOP_SAVE_BYTES}-byte desktop save limit`);
  }
}

/** True when the page runs inside the desktop shell's webview. */
export function isDesktopShell(target: Window = window): boolean {
  return Boolean((target as WailsWindow)._wails?.invoke);
}

/**
 * Send a Blob to the desktop shell as a stream of start → chunk* → end
 * messages; the shell appends the chunks to a temp file and shows the native
 * save panel when the transfer completes. WKWebView drops a blob `<a download>`
 * click without any error (cmd/ocode-desktop/save_file.go), so a desktop save
 * has to go through the shell. Chunks stream via Blob slices, so peak memory
 * stays ~SAVE_CHUNK_BYTES regardless of file size; the bytes never exist as
 * one string.
 *
 * The bridge has no acknowledgement: this returns when the messages were
 * posted, not when the shell accepted or wrote them. Refusals (bad nonce,
 * over-cap chunk, a chunk arriving out of order) are only logged by the
 * shell; a failed write is surfaced by the shell as a native error dialog.
 *
 * Throws when the page has no clipboard nonce (the shell refuses the save), when
 * the bridge goes missing mid-stream, or when the file exceeds
 * MAX_DESKTOP_SAVE_BYTES. Nothing is sent in those cases (except that an
 * interrupted stream leaves a dead session in the shell, which its stale-save
 * reaper deletes).
 */
export async function saveDesktopBlob(name: string, blob: Blob, target: Window = window): Promise<void> {
  if (!clipboardNonce) {
    throw new Error(`save: ${name} not sent, the page has no desktop nonce`);
  }
  assertDesktopSaveSize(name, blob.size);
  const id = uuidv4();
  const start = JSON.stringify({ id, name, size: blob.size });
  if (!invokeWails(`ocode:save-file-start:${clipboardNonce}:${start}`, target)) {
    throw new Error(`save: ${name} not sent, the desktop bridge is missing`);
  }
  for (let offset = 0; offset < blob.size; offset += SAVE_CHUNK_BYTES) {
    const end = Math.min(offset + SAVE_CHUNK_BYTES, blob.size);
    const bytes = new Uint8Array(await blob.slice(offset, end).arrayBuffer());
    const body = JSON.stringify({ id, seq: offset / SAVE_CHUNK_BYTES, data: bytesToBase64(bytes) });
    if (!invokeWails(`ocode:save-file-chunk:${clipboardNonce}:${body}`, target)) {
      throw new Error(`save: ${name} interrupted, the desktop bridge is missing`);
    }
  }
  if (!invokeWails(`ocode:save-file-end:${clipboardNonce}:${JSON.stringify({ id })}`, target)) {
    throw new Error(`save: ${name} not finished, the desktop bridge is missing`);
  }
}

/**
 * Stream a fetch Response body to the desktop shell, chunk by chunk. Used by
 * the Files-tab download: the body is read with getReader() and forwarded in
 * ~SAVE_CHUNK_BYTES pieces, so a multi-GB file never resides in the webview —
 * the peak is ~one chunk, not ~the whole file. Requires a trustworthy
 * Content-Length (the shell verifies the final byte count); callers fall back
 * to saveDesktopBlob when the stream or the length is missing.
 *
 * As with saveDesktopBlob, the bridge has no acknowledgement; a mid-stream
 * refusal is only logged by the shell. Throws when the stream ends early or
 * runs long (the shell session is then abandoned to its stale-save reaper).
 */
export async function saveDesktopStream(
  name: string,
  body: ReadableStream<Uint8Array>,
  size: number,
  target: Window = window,
): Promise<void> {
  if (!clipboardNonce) {
    throw new Error(`save: ${name} not sent, the page has no desktop nonce`);
  }
  if (!Number.isSafeInteger(size) || size < 0) {
    throw new Error(`save: ${name} has no usable Content-Length`);
  }
  assertDesktopSaveSize(name, size);
  const id = uuidv4();
  const start = JSON.stringify({ id, name, size });
  if (!invokeWails(`ocode:save-file-start:${clipboardNonce}:${start}`, target)) {
    throw new Error(`save: ${name} not sent, the desktop bridge is missing`);
  }
  const sendChunk = async (bytes: Uint8Array, seq: number): Promise<void> => {
    const body = JSON.stringify({ id, seq, data: bytesToBase64(bytes) });
    if (!invokeWails(`ocode:save-file-chunk:${clipboardNonce}:${body}`, target)) {
      throw new Error(`save: ${name} interrupted, the desktop bridge is missing`);
    }
  };

  const reader = body.getReader();
  try {
    let pending: Uint8Array[] = [];
    let buffered = 0;
    let received = 0;
    let seq = 0;
    for (;;) {
      const { done, value } = await reader.read();
      if (done) break;
      if (!value || value.byteLength === 0) continue;
      received += value.byteLength;
      if (received > size) {
        throw new Error(`save: ${name} exceeded the declared ${size} bytes`);
      }
      pending.push(value);
      buffered += value.byteLength;
      if (buffered >= SAVE_CHUNK_BYTES) {
        await sendChunk(concatBytes(pending, buffered), seq);
        seq += 1;
        pending = [];
        buffered = 0;
      }
    }
    if (buffered > 0) {
      await sendChunk(concatBytes(pending, buffered), seq);
    }
    if (received !== size) {
      throw new Error(`save: ${name} ended at ${received} of ${size} bytes`);
    }
    if (!invokeWails(`ocode:save-file-end:${clipboardNonce}:${JSON.stringify({ id })}`, target)) {
      throw new Error(`save: ${name} not finished, the desktop bridge is missing`);
    }
  } finally {
    // Every exit path releases the response, or a failed download holds a
    // WKWebView connection slot (~6 per origin) until GC.
    await reader.cancel().catch(() => {});
  }
}

/** Concatenate chunk slices into one array of exactly `total` bytes. */
function concatBytes(parts: Uint8Array[], total: number): Uint8Array {
  const out = new Uint8Array(total);
  let offset = 0;
  for (const part of parts) {
    out.set(part, offset);
    offset += part.byteLength;
  }
  return out;
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
