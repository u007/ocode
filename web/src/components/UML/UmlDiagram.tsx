import { useEffect, useMemo, useRef, useState, type ReactNode } from "react";
import type { UmlEdge, UmlIndex, UmlPackage, UmlSymbol } from "../../api/types";

export type UmlView = "map" | "classes";

interface Props {
  view: UmlView;
  index: UmlIndex;
  zoom: number;
  pan: { x: number; y: number };
  onViewChange: (zoom: number, pan: { x: number; y: number }) => void;
  selectedId: string | null;
  onSelect: (id: string) => void;
  onOpenSource: (s: UmlSymbol) => void;
  /** Nonce-incremented request to zoom-focus a symbol (from list click/Enter). */
  focusRequest: { id: string; nonce: number } | null;
}

const MIN_ZOOM = 0.35;
const MAX_ZOOM = 2.6;

const clusterColors: Record<string, { fill: string; stroke: string; title: string }> = {
  Go: { fill: "#131c29", stroke: "#2d3b52", title: "#9fb3cc" },
  TS: { fill: "#161d2c", stroke: "#2b3c5e", title: "#8fb3e8" },
  Python: { fill: "#1d1a12", stroke: "#4a3b1a", title: "#e6c97a" },
};

interface ClusterBox {
  p: UmlPackage;
  x: number;
  y: number;
  w: number;
  h: number;
  nodes: { s: UmlSymbol; x: number; y: number; w: number; h: number }[];
}

/** Longest-path layering over package import edges; cycles fall back to 0. */
function layerPackages(pkgs: UmlPackage[], edges: UmlEdge[]): Map<string, number> {
  const indeg = new Map<string, number>();
  const adj = new Map<string, string[]>();
  for (const p of pkgs) {
    indeg.set(p.id, 0);
    adj.set(p.id, []);
  }
  for (const e of edges) {
    if (!adj.has(e.from) || !adj.has(e.to)) continue;
    adj.get(e.from)!.push(e.to);
    indeg.set(e.to, (indeg.get(e.to) ?? 0) + 1);
  }
  const layer = new Map<string, number>();
  const remaining = new Set(pkgs.map((p) => p.id));
  const queue: string[] = [];
  for (const [id, d] of indeg) if (d === 0) queue.push(id);
  while (queue.length > 0) {
    const id = queue.shift()!;
    const base = layer.get(id) ?? 0;
    layer.set(id, base);
    for (const next of adj.get(id) ?? []) {
      const cand = base + 1;
      layer.set(next, Math.max(layer.get(next) ?? 0, cand));
      const d = (indeg.get(next) ?? 1) - 1;
      indeg.set(next, d);
      if (d === 0) queue.push(next);
    }
  }
  for (const id of remaining) if (!layer.has(id)) layer.set(id, 0); // cycle fallback
  return layer;
}

const NODE_W = 196;
const NODE_H_BASE = 52;
const MEMBER_H = 14;
const MAX_MEMBERS = 8;

function layoutMap(index: UmlIndex): Map<string, ClusterBox> {
  const layer = layerPackages(index.packages, index.edges);
  const byLayer = new Map<number, UmlPackage[]>();
  for (const p of index.packages) {
    const l = layer.get(p.id) ?? 0;
    const arr = byLayer.get(l) ?? [];
    arr.push(p);
    byLayer.set(l, arr);
  }
  const boxes = new Map<string, ClusterBox>();
  for (const [l, arr] of byLayer) {
    arr.sort((a, b) => b.symbol_count - a.symbol_count);
    let y = 40;
    for (const p of arr) {
      // Height scales with sqrt(symbol count) so the map stays compact but
      // big packages stay visibly bigger.
      const h = Math.min(150, Math.max(64, Math.round(Math.sqrt(Math.max(1, p.symbol_count)) * 9)));
      const x = 40 + l * 300;
      boxes.set(p.id, { p, x, y, w: 220, h, nodes: [] });
      y += h + 28;
    }
  }
  return boxes;
}

function layoutClasses(index: UmlIndex): Map<string, ClusterBox> {
  const layer = layerPackages(index.packages, index.edges);
  const byPkg = new Map<string, UmlSymbol[]>();
  for (const s of index.symbols) {
    if (s.kind !== "struct" && s.kind !== "interface") continue;
    const arr = byPkg.get(s.pkg) ?? [];
    arr.push(s);
    byPkg.set(s.pkg, arr);
  }
  for (const arr of byPkg.values()) {
    arr.sort((a, b) => a.file.localeCompare(b.file) || a.line - b.line);
  }
  const boxes = new Map<string, ClusterBox>();
  const byLayer = new Map<number, UmlPackage[]>();
  for (const p of index.packages) {
    const l = layer.get(p.id) ?? 0;
    const arr = byLayer.get(l) ?? [];
    arr.push(p);
    byLayer.set(l, arr);
  }
  // Layer stride = widest cluster in the layer + gap, so clusters never overlap.
  const layerWidth = new Map<number, number>();
  for (const [l, arr] of byLayer) {
    let maxW = 260;
    for (const p of arr) {
      const syms = byPkg.get(p.id) ?? [];
      const cols = Math.min(3, Math.max(1, Math.ceil(Math.sqrt(Math.max(1, syms.length)))));
      maxW = Math.max(maxW, Math.max(260, cols * (NODE_W + 14) + 20));
    }
    layerWidth.set(l, maxW + 60);
  }
  const layerOffset = new Map<number, number>();
  {
    let acc = 0;
    const layers = [...layerWidth.keys()].sort((a, b) => a - b);
    for (const l of layers) {
      layerOffset.set(l, acc);
      acc += layerWidth.get(l) ?? 320;
    }
  }
  for (const [l, arr] of byLayer) {
    arr.sort((a, b) => (byPkg.get(b.id)?.length ?? 0) - (byPkg.get(a.id)?.length ?? 0));
    let y = 40;
    for (const p of arr) {
      const syms = byPkg.get(p.id) ?? [];
      const cols = Math.min(3, Math.max(1, Math.ceil(Math.sqrt(Math.max(1, syms.length)))));
      const colH: number[] = new Array(cols).fill(0);
      const nodes: ClusterBox["nodes"] = [];
      for (const s of syms) {
        const members = Math.min(MAX_MEMBERS, s.members?.length ?? 0);
        const h = NODE_H_BASE + members * MEMBER_H;
        // Place into the shortest column.
        let c = 0;
        for (let i = 1; i < cols; i++) if (colH[i] < colH[c]) c = i;
        nodes.push({ s, x: c * (NODE_W + 14), y: colH[c], w: NODE_W, h });
        colH[c] += h + 14;
      }
      const bodyH = Math.max(60, ...colH.map((v) => v), 0);
      const w = Math.max(260, cols * (NODE_W + 14) + 20);
      const h = bodyH + 52;
      boxes.set(p.id, {
        p,
        x: 40 + (layerOffset.get(l) ?? 0),
        y,
        w,
        h,
        nodes: nodes.map((n) => ({ ...n, x: n.x + 10, y: n.y + 44 })),
      });
      y += h + 40;
    }
  }
  return boxes;
}

export default function UmlDiagram({
  view,
  index,
  zoom,
  pan,
  onViewChange,
  selectedId,
  onSelect,
  onOpenSource,
  focusRequest,
}: Props) {
  const svgRef = useRef<SVGSVGElement>(null);
  const [pulseId, setPulseId] = useState<string | null>(null);
  const [dragging, setDragging] = useState(false);
  const dragStart = useRef<{ x: number; y: number; panX: number; panY: number } | null>(null);

  const boxes = useMemo(
    () => (view === "map" ? layoutMap(index) : layoutClasses(index)),
    [view, index],
  );
  const nodeIndex = useMemo(() => {
    const m = new Map<string, { x: number; y: number; w: number; h: number }>();
    for (const box of boxes.values()) {
      for (const n of box.nodes) {
        m.set(n.s.id, { x: box.x + n.x, y: box.y + n.y, w: n.w, h: n.h });
      }
    }
    return m;
  }, [boxes]);
  const clusterCenter = (id: string) => {
    const b = boxes.get(id);
    return b ? { x: b.x + b.w / 2, y: b.y + b.h / 2 } : null;
  };

  const setZoom = (next: number, cx?: number, cy?: number) => {
    const svg = svgRef.current;
    const rect = svg?.getBoundingClientRect();
    const k = Math.min(MAX_ZOOM, Math.max(MIN_ZOOM, next)) / zoom;
    if (!rect) {
      onViewChange(Math.min(MAX_ZOOM, Math.max(MIN_ZOOM, next)), pan);
      return;
    }
    const px = cx ?? rect.width / 2;
    const py = cy ?? rect.height / 2;
    onViewChange(Math.min(MAX_ZOOM, Math.max(MIN_ZOOM, next)), {
      x: px - (px - pan.x) * k,
      y: py - (py - pan.y) * k,
    });
  };

  useEffect(() => {
    if (!focusRequest) return;
    const target = nodeIndex.get(focusRequest.id);
    const svg = svgRef.current;
    if (!target || !svg) return;
    const rect = svg.getBoundingClientRect();
    const z = Math.max(1.4, zoom);
    onViewChange(z, {
      x: rect.width / 2 - (target.x + target.w / 2) * z,
      y: rect.height / 2 - (target.y + target.h / 2) * z,
    });
    setPulseId(focusRequest.id);
    const t = setTimeout(() => setPulseId(null), 2800);
    return () => clearTimeout(t);
    // Only the nonce may retrigger; zoom reads are fresh per render.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [focusRequest?.nonce]);

  const onWheel = (e: React.WheelEvent) => {
    if (!e.ctrlKey && !e.metaKey) return;
    e.preventDefault();
    const rect = svgRef.current!.getBoundingClientRect();
    setZoom(zoom * (e.deltaY < 0 ? 1.12 : 0.89), e.clientX - rect.left, e.clientY - rect.top);
  };

  const onPointerDown = (e: React.PointerEvent) => {
    (e.target as Element).setPointerCapture?.(e.pointerId);
    dragStart.current = { x: e.clientX, y: e.clientY, panX: pan.x, panY: pan.y };
    setDragging(true);
  };
  const onPointerMove = (e: React.PointerEvent) => {
    const d = dragStart.current;
    if (!d) return;
    onViewChange(zoom, { x: d.panX + (e.clientX - d.x), y: d.panY + (e.clientY - d.y) });
  };
  const onPointerUp = () => {
    dragStart.current = null;
    setDragging(false);
  };

  const showNodes = view === "classes" && zoom >= 0.6;
  const showMembers = view === "classes" && zoom >= 1.15;
  const dimClusters = view === "classes";

  const renderCluster = (box: ClusterBox): ReactNode => {
    const { p } = box;
    const palette = clusterColors[p.lang] ?? clusterColors.Go;
    return (
      <g key={p.id}>
        <rect
          x={box.x}
          y={box.y}
          width={box.w}
          height={box.h}
          rx={10}
          fill={palette.fill}
          fillOpacity={dimClusters ? 0.45 : 0.9}
          stroke={palette.stroke}
          strokeWidth={1.2}
        />
        <text x={box.x + 12} y={box.y + 20} fill={palette.title} fontSize={12} fontWeight={700} fontFamily="ui-monospace, Menlo, monospace">
          {p.label}
        </text>
        <text x={box.x + 12} y={box.y + 34} fill="#5b6a7f" fontSize={9}>
          {p.symbol_count} symbols · {p.lang}
        </text>
        {showNodes && box.nodes.map((n) => renderNode(box, n))}
      </g>
    );
  };

  const renderNode = (box: ClusterBox, n: ClusterBox["nodes"][number]): ReactNode => {
    const s = n.s;
    const absX = box.x + n.x;
    const absY = box.y + n.y;
    const selected = s.id === selectedId;
    const pulse = s.id === pulseId;
    const isInterface = s.kind === "interface";
    const stereotype = isInterface ? "«interface» " : "";
    const members = showMembers ? (s.members ?? []).slice(0, MAX_MEMBERS) : [];
    const extra = showMembers ? (s.members?.length ?? 0) - members.length : 0;
    return (
      <g
        key={s.id}
        data-id={s.id}
        role="button"
        tabIndex={0}
        aria-label={s.name}
        style={{ cursor: "pointer" }}
        onClick={(e) => {
          e.stopPropagation();
          onSelect(s.id);
        }}
        onDoubleClick={() => onOpenSource(s)}
        onKeyDown={(e) => {
          if (e.key === "Enter") onSelect(s.id);
        }}
      >
        <rect
          x={absX}
          y={absY}
          width={n.w}
          height={n.h}
          rx={6}
          fill={isInterface ? "#1d1830" : "#182437"}
          stroke={selected ? "#58a6ff" : isInterface ? "#5b4a8a" : "#3b5878"}
          strokeWidth={selected ? 2.2 : 1.2}
          className={pulse ? "uml-pulse" : undefined}
        />
        <text x={absX + 10} y={absY + 17} fill="#cfe3ff" fontSize={11} fontWeight={700}>
          {stereotype}
          {s.name}
        </text>
        <text x={absX + 10} y={absY + 29} fill="#5b6a7f" fontSize={7.5} fontFamily="ui-monospace, Menlo, monospace">
          {s.file}:{s.line}
        </text>
        {showMembers && <line x1={absX} y1={absY + 34} x2={absX + n.w} y2={absY + 34} stroke="#2b3850" strokeWidth={1} />}
        {members.map((m, i) => (
          <text
            key={i}
            x={absX + 12}
            y={absY + 47 + i * MEMBER_H}
            fontSize={8.5}
            fontFamily="ui-monospace, Menlo, monospace"
            fill={m.kind === "method" ? "#7ee787" : "#ffa657"}
          >
            {m.kind === "method" ? "+ " : "− "}
            {m.name}
          </text>
        ))}
        {extra > 0 && (
          <text x={absX + 12} y={absY + 47 + members.length * MEMBER_H} fontSize={8} fill="#5b6a7f">
            +{extra} more…
          </text>
        )}
      </g>
    );
  };

  const renderEdge = (e: UmlEdge): ReactNode | null => {
    const a = clusterCenter(e.from);
    const b = clusterCenter(e.to);
    if (!a || !b) return null;
    return (
      <line
        key={`${e.from}->${e.to}`}
        x1={a.x}
        y1={a.y}
        x2={b.x}
        y2={b.y}
        stroke="#3d4d68"
        strokeWidth={1.3}
        strokeDasharray={view === "map" ? undefined : "5 4"}
        markerEnd="url(#umlArrow)"
        opacity={0.75}
      />
    );
  };

  return (
    <svg
      ref={svgRef}
      viewBox={`0 0 1200 760`}
      preserveAspectRatio="xMidYMid meet"
      className="h-full w-full select-none"
      style={{ cursor: dragging ? "grabbing" : "grab", touchAction: "none" }}
      role="application"
      aria-label="UML diagram"
      onWheel={onWheel}
      onPointerDown={onPointerDown}
      onPointerMove={onPointerMove}
      onPointerUp={onPointerUp}
      onPointerLeave={onPointerUp}
    >
      <defs>
        <marker id="umlArrow" viewBox="0 0 10 10" refX="9" refY="5" markerWidth="6.5" markerHeight="6.5" orient="auto-start-reverse">
          <path d="M0,0 L10,5 L0,10 z" fill="#54678a" />
        </marker>
        <style>{`@keyframes umlPulse { 0%,100% { stroke: #58a6ff; stroke-width: 2 } 50% { stroke: #a5d6ff; stroke-width: 4.5 } } .uml-pulse { animation: umlPulse .9s ease-in-out 3; }`}</style>
      </defs>
      <g transform={`translate(${pan.x} ${pan.y}) scale(${zoom})`}>
        {index.edges.map(renderEdge)}
        {[...boxes.values()].map(renderCluster)}
      </g>
    </svg>
  );
}
