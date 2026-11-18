import { isDesktopShell, saveDesktopBlob, saveDesktopStream } from "./wails";

/**
 * Ceiling for a save that cannot stream (no body, or no trustworthy
 * Content-Length). 512 MiB is the largest any save could have been before the
 * streaming path existed, so this refuses nothing that used to work while
 * bounding the peak at 512 MiB instead of the whole 2 GiB desktop cap.
 */
const MAX_MATERIALIZED_SAVE_BYTES = 512 * 1024 * 1024;

/**
 * Save a Blob as a file. In the desktop app the bytes stream to the shell in
 * bounded chunks (saveDesktopBlob): WKWebView silently drops blob <a download>
 * clicks, and one giant bridge string cannot carry multi-GB files. The shell
 * writes a temp file and shows the native save panel when the transfer ends.
 * In a browser this is a temporary anchor. The object URL is revoked on a
 * timer, not synchronously: Safari and the desktop app's WKWebView navigate to
 * about:blank when the URL they just clicked is revoked, which reads as a failed
 * download.
 *
 * Rejects when a desktop save cannot be sent (see saveDesktopBlob).
 */
export async function saveBlobAsFile(blob: Blob, filename: string): Promise<void> {
  if (isDesktopShell()) {
    // size is checked before the first slice is read, so an oversize file
    // never reaches the bridge.
    await saveDesktopBlob(filename, blob);
    return;
  }
  saveBlobWithAnchor(blob, filename);
}

/**
 * Save a download Response as a file. On the desktop shell the body streams
 * straight from the network into the bridge in ~chunk-size messages, so even
 * a multi-GB file never resides in the webview. Falls back to a materialized
 * blob (capped at MAX_MATERIALIZED_SAVE_BYTES) when the body is not streamable
 * or the Content-Length is missing or untrustworthy. In a browser the body is
 * read into a blob and saved as a temporary anchor.
 */
export async function saveResponseAsFile(response: Response, filename: string): Promise<void> {
  if (isDesktopShell()) {
    const sizeHeader = response.headers.get("Content-Length");
    const size = sizeHeader === null ? NaN : Number(sizeHeader);
    if (response.body && Number.isSafeInteger(size) && size >= 0) {
      await saveDesktopStream(filename, response.body as ReadableStream<Uint8Array>, size);
      return;
    }
    // No streamable body or no usable Content-Length: materialize the blob and
    // chunk-send that instead. Same limits, more peak memory.
    await saveDesktopBlob(filename, await materializeBounded(response, filename));
    return;
  }
  saveBlobWithAnchor(await materializeBounded(response, filename), filename);
}

/**
 * Read a response that cannot be streamed into a Blob, aborting once it passes
 * MAX_MATERIALIZED_SAVE_BYTES so a header-stripping proxy cannot hand the
 * webview the whole desktop cap at once.
 */
async function materializeBounded(response: Response, filename: string): Promise<Blob> {
  if (!response.body) return response.blob();
  const reader = response.body.getReader();
  const parts: BlobPart[] = [];
  let total = 0;
  try {
    for (;;) {
      const { done, value } = await reader.read();
      if (done) break;
      if (!value || value.byteLength === 0) continue;
      total += value.byteLength;
      if (total > MAX_MATERIALIZED_SAVE_BYTES) {
        throw new Error(
          `save: ${filename} is over the ${MAX_MATERIALIZED_SAVE_BYTES}-byte materialized limit and the response could not be streamed`,
        );
      }
      parts.push(value);
    }
  } finally {
    // Release the response on every exit (the abort path included).
    await reader.cancel().catch(() => {});
  }
  return new Blob(parts);
}

function saveBlobWithAnchor(blob: Blob, filename: string): void {
  const url = URL.createObjectURL(blob);
  const a = document.createElement("a");
  a.href = url;
  a.download = filename;
  document.body.appendChild(a);
  a.click();
  a.remove();
  setTimeout(() => URL.revokeObjectURL(url), 10_000);
}
