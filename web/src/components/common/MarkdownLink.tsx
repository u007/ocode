import type { AnchorHTMLAttributes, ReactNode } from "react";
import { openFile } from "../../lib/fileLinks";
import { isHTTPURL, requestURLOpen } from "../../lib/externalLinks";
import { useURLOpenOwner } from "../../lib/urlOpenContext";

type MarkdownLinkProps = AnchorHTMLAttributes<HTMLAnchorElement> & {
  children?: ReactNode;
  /** react-markdown passes the hast node; it must never reach the DOM. */
  node?: unknown;
};

/** Schemes whose default handling hands off to the OS mail/tel handlers and
 *  therefore cannot replace the app. `new URL` parses them fine, so this is a
 *  scheme allow-list, not a "is it absolute" test. */
const OS_HANDOFF_SCHEMES = new Set(["mailto:", "tel:", "sms:"]);

/**
 * Whether a click may fall through to the browser's default action.
 *
 * Only two cases qualify: an in-page `#hash`, and a scheme that hands off to an
 * OS handler. Everything else — http(s), a RELATIVE path, a root-relative
 * `/path`, a protocol-relative `//host/path` — must be stopped.
 *
 * The relative cases are the reason this is not simply `isHTTPURL`. Assistant
 * prose links to project files all the time (`[foo.go](internal/x/foo.go)`),
 * and inside the desktop webview a relative href resolves against ocode's own
 * origin, so the default action hits the SPA fallback and replaces the entire
 * app with index.html. Gating `preventDefault` on `isHTTPURL` (as this
 * component once did) let exactly those through.
 */
function keepsDefaultHandling(href: string): boolean {
  if (href.startsWith("#")) return true;
  try {
    return OS_HANDOFF_SCHEMES.has(new URL(href).protocol);
  } catch {
    // Not an absolute URL: relative, root-relative, or protocol-relative.
    return false;
  }
}

/** A home-directory file link ("~" or "~/…") as the agent writes it in prose.
 *  The browser cannot expand "~", so the server does it when the file opens. */
function isHomeFileHref(href: string): boolean {
  return href === "~" || href.startsWith("~/");
}

/**
 * The single anchor renderer for every react-markdown surface.
 *
 * A plain `<a href="https://…">` inside the desktop webview is served by
 * ocode's own server, and Wails has no external-navigation delegate, so the
 * default action would replace the whole app with the target page. Intercept
 * http(s) clicks and ask the app-level chooser between an embedded browser tab
 * and the external browser. A home-directory file link
 * (`~/…`) opens in the Files tab. Every other href is still prevented (see
 * `keepsDefaultHandling`) — a chat bubble must never be able to navigate the
 * shell.
 *
 * `href`/`target`/`rel` are kept so middle-click, "copy link address", and
 * `Cmd`/`Ctrl`-click still behave like a real link.
 */
export default function MarkdownLink({ href, children, node: _node, ...props }: MarkdownLinkProps) {
  const owner = useURLOpenOwner();
  return (
    <a
      href={href}
      target="_blank"
      rel="noopener noreferrer"
      {...props}
      onClick={(event) => {
        props.onClick?.(event);
        if (!href || event.defaultPrevented) return;
        if (keepsDefaultHandling(href)) return;
        event.preventDefault();
        // A home-directory file link opens in the Files tab, the same path a
        // file link in the terminal takes. Out-of-project files get the grant
        // dialog there, so this never reads or opens anything directly.
        if (isHomeFileHref(href)) {
          openFile(href);
          return;
        }
        // Only genuinely absolute http(s) is worth offering as an external
        // destination; a relative path is a mistake, not a link out of the app.
        if (isHTTPURL(href)) requestURLOpen(href, owner ?? undefined);
      }}
      onAuxClick={(event) => {
        props.onAuxClick?.(event);
        if (event.button !== 1 || !href || event.defaultPrevented) return;
        if (!isHTTPURL(href)) return;
        event.preventDefault();
        requestURLOpen(href, owner ?? undefined);
      }}
    >
      {children}
    </a>
  );
}
