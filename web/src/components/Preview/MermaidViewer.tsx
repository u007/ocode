import { useEffect, useMemo, useRef, useState } from "react";
import mermaid from "mermaid";
import { dispatchPreviewContext } from "../../lib/previewKind";

mermaid.initialize({ startOnLoad: false, securityLevel: "loose", theme: "dark" });

/**
 * Flowchart viewer (mermaid — stable/maintained).
 * - Zoom in/out/reset (buttons + ctrl+wheel), scrollable pan surface.
 * - Clickable nodes: clicking a node selects that branch and offers
 *   Copy | Ask AI about this branch (label carries the node id so the LLM
 *   knows which branch was referenced).
 * - Nodes with `click <id> <href>` open the linked file in PreviewHost.
 */
export default function MermaidViewer({
  path,
  code,
  projectRoot,
  onOpenFile,
}: {
  path: string;
  code: string;
  projectRoot?: string;
  onOpenFile: (path: string) => void;
}) {
  const [svg, setSvg] = useState<string>("");
  const [error, setError] = useState<string | null>(null);
  const [zoom, setZoom] = useState(1);
  const [activeNode, setActiveNode] = useState<{ id: string; label: string } | null>(null);
  const [copied, setCopied] = useState(false);
  const hostRef = useRef<HTMLDivElement | null>(null);
  const renderId = useMemo(() => `mmd-${Math.random().toString(36).slice(2, 9)}`, []);

  useEffect(() => {
    let cancelled = false;
    setError(null);
    setSvg("");
    setActiveNode(null);
    mermaid
      .render(renderId, code)
      .then(({ svg }) => {
        if (!cancelled) setSvg(svg);
      })
      .catch((e) => {
        if (!cancelled) setError(e instanceof Error ? e.message : String(e));
      });
    return () => {
      cancelled = true;
    };
  }, [code, renderId]);

  // Node clicks: mermaid emits g.node (flowcharts) / g.node* groups with an
  // id; click handlers with hrefs render as <a> inside the node.
  useEffect(() => {
    const host = hostRef.current;
    if (!host || !svg) return;
    const nodes = host.querySelectorAll("g.node, g[class*='node']");
    const cleanups: Array<() => void> = [];
    nodes.forEach((g) => {
      const el = g as SVGGElement;
      const id = el.id || el.getAttribute("data-id") || el.textContent?.trim().slice(0, 24) || "node";
      const link = el.querySelector("a");
      const onClick = (e: Event) => {
        e.stopPropagation();
        const href = link?.getAttribute("xlink:href") || link?.getAttribute("href");
        if (href && !href.startsWith("#")) {
          onOpenFile(href);
          return;
        }
        const label = (el.textContent || "").trim().slice(0, 120);
        setActiveNode({ id, label });
      };
      el.addEventListener("click", onClick);
      el.setAttribute("cursor", "pointer");
      cleanups.push(() => el.removeEventListener("click", onClick));
    });
    return () => cleanups.forEach((fn) => fn());
  }, [svg, onOpenFile]);

  const askAboutBranch = () => {
    if (!activeNode) return;
    dispatchPreviewContext({
      path,
      label: `node ${activeNode.id}`,
      excerpt: activeNode.label || activeNode.id,
      projectRoot,
    });
    setActiveNode(null);
  };

  return (
    <div className="flex h-full min-h-0 flex-col">
      <div className="flex shrink-0 items-center gap-1 border-b border-border px-2 py-1 text-xs">
        <button type="button" onClick={() => setZoom((z) => Math.max(0.4, +(z - 0.2).toFixed(2)))} className="rounded px-1.5 py-0.5 hover:bg-muted" aria-label="Zoom out">−</button>
        <span className="w-10 text-center text-muted-foreground">{Math.round(zoom * 100)}%</span>
        <button type="button" onClick={() => setZoom((z) => Math.min(2.5, +(z + 0.2).toFixed(2)))} className="rounded px-1.5 py-0.5 hover:bg-muted" aria-label="Zoom in">+</button>
        <button type="button" onClick={() => setZoom(1)} className="rounded px-1.5 py-0.5 hover:bg-muted" aria-label="Reset zoom">Reset</button>
        <span className="ml-auto hidden truncate text-muted-foreground sm:inline">Click a node to select its branch</span>
      </div>
      {error ? (
        <div className="p-4 text-xs text-red-400">Diagram failed: {error}</div>
      ) : !svg ? (
        <div className="p-4 text-xs text-muted-foreground">Rendering diagram…</div>
      ) : (
        <div
          className="min-h-0 flex-1 overflow-auto p-3"
          onWheel={(e) => {
            if (e.ctrlKey || e.metaKey) {
              e.preventDefault();
              setZoom((z) => Math.min(2.5, Math.max(0.4, +(z + (e.deltaY < 0 ? 0.1 : -0.1)).toFixed(2))));
            }
          }}
        >
          <div ref={hostRef} style={{ transform: `scale(${zoom})`, transformOrigin: "0 0" }} dangerouslySetInnerHTML={{ __html: svg }} />
        </div>
      )}
      {activeNode && (
        <div className="flex shrink-0 items-center gap-1 border-t border-border px-2 py-1.5 text-xs" role="toolbar" aria-label="Branch actions">
          <span className="min-w-0 flex-1 truncate text-muted-foreground" title={activeNode.label}>
            Branch <span className="font-mono text-foreground">{activeNode.id}</span>
          </span>
          <button
            type="button"
            onClick={() => {
              navigator.clipboard?.writeText(activeNode.label || activeNode.id).then(
                () => {
                  setCopied(true);
                  setTimeout(() => setActiveNode(null), 600);
                },
                () => setActiveNode(null),
              );
            }}
            className="rounded px-2 py-0.5 hover:bg-muted"
          >
            {copied ? "Copied" : "Copy"}
          </button>
          <button type="button" onClick={askAboutBranch} className="rounded bg-primary px-2 py-0.5 text-primary-foreground hover:opacity-90">
            Ask AI about this branch
          </button>
        </div>
      )}
    </div>
  );
}
