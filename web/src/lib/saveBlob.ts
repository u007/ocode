import { assertDesktopSaveSize, isDesktopShell, saveDesktopFile } from "./wails";

/**
 * Save a Blob as a file. In the desktop app the bytes go to the shell's native
 * save panel (saveDesktopFile): WKWebView silently drops blob <a download>
 * clicks. In a browser this is a temporary anchor. The object URL is revoked on
 * a timer, not synchronously: Safari and the desktop app's WKWebView navigate to
 * about:blank when the URL they just clicked is revoked, which reads as a failed
 * download.
 *
 * Rejects when a desktop save cannot be sent (see saveDesktopFile).
 */
export async function saveBlobAsFile(blob: Blob, filename: string): Promise<void> {
  if (isDesktopShell()) {
    // Check the size before arrayBuffer(): reading an oversize blob would
    // allocate it whole just to be refused.
    assertDesktopSaveSize(filename, blob.size);
    saveDesktopFile(filename, new Uint8Array(await blob.arrayBuffer()));
    return;
  }
  const url = URL.createObjectURL(blob);
  const a = document.createElement("a");
  a.href = url;
  a.download = filename;
  document.body.appendChild(a);
  a.click();
  a.remove();
  setTimeout(() => URL.revokeObjectURL(url), 10_000);
}
