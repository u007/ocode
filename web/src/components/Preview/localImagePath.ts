/** Image sources the browser loads itself: remote, or bytes embedded in the
 *  document. Everything else is a file on the project's disk. */
const WEB_IMAGE_SRC = /^(?:https?:|data:|blob:)/i;

/**
 * The project file an image source points at, or null when the browser can
 * load the source by itself. Shared by the markdown viewer and the print
 * documents, so both fetch the same bytes for the same source.
 *
 * A relative source resolves against the DOCUMENT's directory (the
 * CommonMark base), which is what makes `![](./shots/a.png)` work. Absolute,
 * Windows and `~/…` sources pass through: the server owns $HOME expansion and
 * containment, and it rejects a `..` segment outright, so the relative join
 * normalizes those lexically first.
 */
export function localImagePath(docPath: string, src: string): string | null {
  if (!src || src.startsWith("#")) return null;
  // `//host/x` is protocol-relative (another origin), not a disk path.
  if (WEB_IMAGE_SRC.test(src) || src.startsWith("//")) return null;
  const bare = decodePath(src.split(/[?#]/, 1)[0] ?? "");
  if (/^[a-zA-Z]:[\\/]/.test(bare) || bare.startsWith("\\\\")) return bare;
  if (bare.startsWith("/") || bare.startsWith("~/")) return bare;
  // A Windows tab path is backslash-separated; without this the split below
  // finds no base directory and every image resolves against the project root.
  const doc = docPath.replace(/\\/g, "/");
  const slash = doc.lastIndexOf("/");
  return normalizeJoin(slash === -1 ? "" : doc.slice(0, slash + 1), bare);
}

/** A markdown image source is a URL, so a space or non-ASCII file name arrives
 *  percent-escaped. A malformed escape is used as written: the server reports
 *  the miss instead of the viewer guessing a different path. */
function decodePath(path: string): string {
  try {
    return decodeURIComponent(path);
  } catch {
    // Not a valid escape sequence — nothing was encoded.
    return path;
  }
}

/** Joins two path fragments and resolves "." / ".." lexically, so a request
 *  never carries a ".." segment the server would refuse. */
function normalizeJoin(dir: string, rel: string): string {
  const absolute = dir.startsWith("/");
  const out: string[] = [];
  for (const seg of (dir + rel).split("/")) {
    if (seg === "" || seg === ".") continue;
    if (seg === "..") {
      if (out.length > 0 && out[out.length - 1] !== "..") out.pop();
      else if (!absolute) out.push("..");
      continue;
    }
    out.push(seg);
  }
  return (absolute ? "/" : "") + out.join("/");
}
