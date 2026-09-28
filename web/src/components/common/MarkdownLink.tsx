import type { AnchorHTMLAttributes, ReactNode } from "react";
import { isHTTPURL, openExternalURL } from "../../lib/externalLinks";

type MarkdownLinkProps = AnchorHTMLAttributes<HTMLAnchorElement> & {
  children?: ReactNode;
  /** react-markdown passes the hast node; it must never reach the DOM. */
  node?: unknown;
};

/**
 * The single anchor renderer for every react-markdown surface.
 *
 * A plain `<a href="https://…">` inside the desktop webview is served by
 * ocode's own server, and Wails has no external-navigation delegate, so the
 * default action would replace the whole app with the target page. Intercept
 * http(s) clicks and hand them to `openExternalURL`, which opens the OS
 * browser on desktop and a new tab in a browser. Non-http schemes (`mailto:`,
 * `tel:`, in-page `#hash`) keep their default handling.
 *
 * `href`/`target`/`rel` are kept so middle-click, "copy link address", and
 * `Cmd`/`Ctrl`-click still behave like a real link.
 */
export default function MarkdownLink({ href, children, node: _node, ...props }: MarkdownLinkProps) {
  return (
    <a
      href={href}
      target="_blank"
      rel="noopener noreferrer"
      {...props}
      onClick={(event) => {
        props.onClick?.(event);
        if (!href || !isHTTPURL(href)) return;
        event.preventDefault();
        openExternalURL(href);
      }}
    >
      {children}
    </a>
  );
}
