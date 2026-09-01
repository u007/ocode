// Shared URL normalization for the embedded browser panel. The store and the
// client both need it (the store normalizes what the address bar stores; the
// client normalizes what browseSrc sends to the proxy). Keeping the two in
// sync in one place prevents the double-normalization drift that once sent
// "localhost:5173" to the proxy as https (breaking plain-HTTP dev servers).

const PRIVATE_HOST_RE =
  /^(localhost|[^/]*\.localhost|127(?:\.\d{1,3}){3}|0\.0\.0\.0|10(?:\.\d{1,3}){3}|192\.168(?:\.\d{1,3}){2}|172\.(?:1[6-9]|2\d|3[01])(?:\.\d{1,3}){2}|\[::1\]|\[[^\]]*\])$/i;

// "http://host", "ftp://host", … — a real explicit scheme with slashes.
const EXPLICIT_SCHEME_RE = /^[a-z][a-z0-9+.-]*:\/\//i;
// "localhost:3000", "javascript:foo", … — a bare colon. The suffix decides:
// a numeric suffix is a port (host form); anything else is a scheme.
const BARE_COLON_RE = /^[a-z0-9._-]+:/i;

/** Normalizes address-bar input to an absolute HTTP(S) URL.
 *  - Explicit `http://` / `https://` are kept verbatim. Any other explicit
 *    scheme (javascript:, data:, ftp:, mailto:…) throws so the caller
 *    surfaces "Unsupported URL scheme" instead of handing garbage to the
 *    proxy.
 *  - Scheme-less input gets a scheme: loopback / RFC1918 private hosts
 *    (localhost, 127.*, 10.*, 192.168.*, 172.16–31.*, IPv6 literals) default
 *    to http:// (dev servers are plain HTTP), everything else to https://. */
export function normalizeBrowseURL(raw: string): string {
  const v = raw.trim();
  if (!v) return "";
  const colon = v.indexOf(":");
  if (colon > 0 && EXPLICIT_SCHEME_RE.test(v)) {
    const scheme = v.slice(0, colon).toLowerCase();
    if (scheme !== "http" && scheme !== "https") {
      throw new Error("Unsupported URL scheme; use http:// or https://");
    }
    return v;
  }
  // A bare colon with a non-numeric suffix is a non-http scheme (javascript:
  // etc.) — reject. A numeric suffix ("localhost:3000") is a host:port and
  // falls through to the host-based scheme pick below.
  if (colon > 0 && BARE_COLON_RE.test(v) && !/^\d/.test(v.slice(colon + 1))) {
    throw new Error("Unsupported URL scheme; use http:// or https://");
  }
  // Keep IPv6 brackets intact — PRIVATE_HOST_RE matches the bracketed form.
  const host = v.replace(/^\/+/, "").split(/[/?#]/, 1)[0].replace(/:\d+(?=\/)?$/, "");
  return (PRIVATE_HOST_RE.test(host) ? "http://" : "https://") + v;
}