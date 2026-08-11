import { useEffect, useState } from "react";
import { cachedHighlight, highlightCode } from "../../lib/highlighter";

// HighlightedCode renders code with Shiki syntax colors inside the caller's own
// <pre>. Shiki is async while render is sync, so it paints the plain text first
// and swaps in the highlighted spans once they resolve. An unknown language or
// a highlighting failure simply leaves the plain text in place.
export default function HighlightedCode({
  code,
  lang,
}: {
  code: string;
  lang: string;
}) {
  const [html, setHtml] = useState<string | null>(() =>
    cachedHighlight(code, lang),
  );

  useEffect(() => {
    const hit = cachedHighlight(code, lang);
    setHtml(hit);
    if (hit !== null) return;
    // The assistant text stream re-renders this with a longer `code` on every
    // token, so an older, slower highlight must not overwrite a newer one.
    let stale = false;
    void highlightCode(code, lang).then((result) => {
      if (!stale) setHtml(result);
    });
    return () => {
      stale = true;
    };
  }, [code, lang]);

  if (html === null) return <>{code}</>;
  return <span dangerouslySetInnerHTML={{ __html: html }} />;
}
