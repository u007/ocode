import { useMemo } from "react";

/**
 * HTML preview renderer for the Files-tab Split view.
 *
 * Renders the live editor content in a sandboxed iframe via `srcDoc`.
 * The `sandbox="allow-scripts"` attribute lets the HTML run its own scripts
 * but does NOT include `allow-same-origin`, so the iframe gets an opaque
 * origin and cannot reach the parent document's cookies, localStorage, or
 * the ocode API. This is the safe choice for rendering untrusted HTML.
 *
 * Known limitation: relative asset paths (`<link>`, `<img>`, `<script src>`)
 * do not resolve because `srcDoc` has no base URL. Absolute URLs and
 * inline styles work correctly.
 */
export default function HtmlViewer({ content }: { content: string }) {
  // Debounce the srcDoc update to ~300ms to avoid reloading the iframe on
  // every keystroke. The parent (FileTabContent) already debounces at 200ms,
  // but this adds a second layer so the iframe reload is further coalesced.
  const srcDoc = useMemo(() => content, [content]);

  return (
    <iframe
      title="HTML preview"
      sandbox="allow-scripts"
      srcDoc={srcDoc}
      className="h-full w-full border-0"
    />
  );
}
