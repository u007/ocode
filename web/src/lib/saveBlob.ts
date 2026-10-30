/**
 * Save a Blob as a file through a temporary anchor. The object URL is revoked on
 * a timer, not synchronously: Safari and the desktop app's WKWebView navigate to
 * about:blank when the URL they just clicked is revoked, which reads as a failed
 * download.
 */
export function saveBlobAsFile(blob: Blob, filename: string): void {
  const url = URL.createObjectURL(blob);
  const a = document.createElement("a");
  a.href = url;
  a.download = filename;
  document.body.appendChild(a);
  a.click();
  a.remove();
  setTimeout(() => URL.revokeObjectURL(url), 10_000);
}
