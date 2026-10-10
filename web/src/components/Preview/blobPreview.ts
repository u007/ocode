/**
 * Pure helpers for the BLOB viewer.
 *
 * Kept out of the components so the safety decision (may this be rendered
 * inline?) and the formatting are directly testable — an inline-render allowlist
 * is a security boundary, not a display preference.
 */

/**
 * Media types that may be handed to an `<img>` / blob URL.
 *
 * An ALLOWLIST of raster images. `image/svg+xml` and `text/html` are documents
 * that execute script when rendered, so "the browser can display it" is not the
 * test — "it cannot run code" is. The server also sends
 * `Content-Disposition: attachment` and `X-Content-Type-Options: nosniff` as a
 * backstop, but this list is what decides, and a strict client list can only
 * ever be safer than a permissive one.
 */
const INLINE_SAFE = new Set(["image/png", "image/jpeg", "image/gif", "image/webp"]);

export function isInlineSafeMediaType(mediaType: string): boolean {
  return INLINE_SAFE.has(mediaType);
}

/** Normalise a Content-Type header: strip parameters, lowercase, trim. */
export function mediaTypeFromHeader(header: string | null): string {
  if (!header) return "";
  return header.split(";")[0]!.trim().toLowerCase();
}

/** Human-readable byte size. Binary units, one decimal above 1 KB. */
export function formatBytes(n: number): string {
  if (!Number.isFinite(n) || n < 0) return "—";
  if (n < 1024) return `${n} B`;
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KB`;
  if (n < 1024 * 1024 * 1024) return `${(n / (1024 * 1024)).toFixed(1)} MB`;
  return `${(n / (1024 * 1024 * 1024)).toFixed(1)} GB`;
}

/**
 * Render bytes as a classic hex dump: offset, 16 hex pairs, then the ASCII
 * column with non-printables as dots.
 *
 * Only the first `maxBytes` are rendered. A blob can be tens of megabytes and
 * building a string for all of it would lock the tab; the caller shows the
 * total separately so the truncation is explicit rather than silent.
 */
export function formatHexDump(data: Uint8Array, maxBytes = 4096): string {
  if (data.length === 0) return "";
  const shown = data.slice(0, Math.max(0, maxBytes));
  const lines: string[] = [];
  for (let off = 0; off < shown.length; off += 16) {
    const chunk = shown.slice(off, off + 16);
    const hex = Array.from(chunk)
      .map((b) => b.toString(16).padStart(2, "0"))
      .join(" ");
    const ascii = Array.from(chunk)
      .map((b) => (b >= 0x20 && b < 0x7f ? String.fromCharCode(b) : "."))
      .join("");
    // Pad the short final line so the ASCII column stays aligned.
    const paddedHex = hex.padEnd(16 * 3 - 1, " ");
    lines.push(`${off.toString(16).padStart(8, "0")}  ${paddedHex}  ${ascii}`);
  }
  if (data.length > shown.length) {
    lines.push(`… showing the first ${shown.length} of ${data.length} bytes`);
  }
  return lines.join("\n");
}

/** The subset of a DBBlob the grid carries: enough to decide what to show. */
export interface BlobCellInfo {
  bytes: number;
  /** True when the server only sent a prefix of the value. */
  truncated: boolean;
}

/**
 * What the dialog should say about a cell whose bytes it has NOT fetched yet.
 * The grid preview is capped, so "5 MB" here is the real size but the hex/data
 * shown inline is not the whole value — the copy must not imply otherwise.
 */
export function describeBlobCell(info: BlobCellInfo): string {
  return info.truncated
    ? `${formatBytes(info.bytes)} — only a preview is loaded; download for the whole value`
    : formatBytes(info.bytes);
}
/**
 * Offer bytes as a file download.
 *
 * The object URL is revoked on a timer, not immediately: Safari and the desktop
 * app's WKWebView navigate to about:blank when the URL they just clicked is
 * revoked synchronously, which reads as the download having failed.
 */
export function downloadBytes(filename: string, data: ArrayBuffer | Uint8Array, mediaType: string): void {
  const blob = new Blob([data as BlobPart], { type: mediaType || "application/octet-stream" });
  const url = URL.createObjectURL(blob);
  const a = document.createElement("a");
  a.href = url;
  a.download = filename;
  document.body.appendChild(a);
  a.click();
  a.remove();
  setTimeout(() => URL.revokeObjectURL(url), 10_000);
}

/** A filename for a downloaded blob: the column, then an extension for its type. */
export function blobFilename(column: string, mediaType: string): string {
  const safe = column.replace(/[^A-Za-z0-9._-]+/g, "_") || "blob";
  const ext =
    mediaType === "image/png"
      ? ".png"
      : mediaType === "image/jpeg"
        ? ".jpg"
        : mediaType === "image/gif"
          ? ".gif"
          : mediaType === "image/webp"
            ? ".webp"
            : mediaType === "application/pdf"
              ? ".pdf"
              : mediaType === "application/zip"
                ? ".zip"
                : mediaType === "application/gzip"
                  ? ".gz"
                  : ".bin";
  return `${safe}${ext}`;
}

/**
 * Read a File into bytes.
 *
 * Uses FileReader rather than `file.arrayBuffer()`: jsdom (which the component
 * tests run in) implements File but not the `arrayBuffer()` method, so the
 * promise-based API throws there. FileReader is available in both.
 */
export function fileToBytes(file: File): Promise<Uint8Array> {
  return new Promise((resolve, reject) => {
    const reader = new FileReader();
    reader.onerror = () => reject(new Error(`Could not read ${file.name}`));
    reader.onload = () => resolve(new Uint8Array(reader.result as ArrayBuffer));
    reader.readAsArrayBuffer(file);
  });
}
