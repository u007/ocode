import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { Loader2, Network, RefreshCw, Search, ExternalLink, AlertTriangle, Package, FileCode2, Braces } from "lucide-react";
import { api } from "@/api/client";
import type { UmlIndex, UmlSymbol } from "@/api/types";
import UmlDiagram, { type UmlView } from "./UmlDiagram";
import { loadUmlViewState, saveUmlViewState, umlViewStateKey, type UmlViewStateV1 } from "./umlViewState";
import { matchesFilter, DEFAULT_KINDS, DEFAULT_LANGS, LIST_RENDER_CAP, type UmlFilter } from "./umlFilter";
import { OPEN_FILE_EVENT, type OpenFileDetail } from "../../lib/fileLinks";

interface Props {
  /** SSH/WSL host of the session's project. Calls route through the host's
   *  server (which indexes locally there); the local server refuses outright. */
  host?: string;
  /** Project root the session's tab belongs to (never the process cwd). */
  projectRoot: string;
  sessionId: string;
}

const LANGS = ["Go", "TS", "Python"] as const;
const KINDS = ["struct", "interface", "func", "method"] as const;

export default function UmlPanel({ host, projectRoot, sessionId }: Props) {
  const scope = umlViewStateKey(host, projectRoot, sessionId);
  const persisted = useMemo(() => loadUmlViewState(scope), [scope]);

  const [status, setStatus] = useState<"idle" | "indexing" | "ready" | "error">("idle");
  const [index, setIndex] = useState<UmlIndex | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [retry, setRetry] = useState(0);

  const [view, setView] = useState<UmlView>(persisted?.view ?? "map");
  const [zoom, setZoom] = useState(persisted?.zoom ?? 1);
  const [pan, setPan] = useState({ x: persisted?.panX ?? 0, y: persisted?.panY ?? 0 });
  const [selectedId, setSelectedId] = useState<string | null>(persisted?.selectedId ?? null);
  const [query, setQuery] = useState(persisted?.query ?? "");
  const [langs, setLangs] = useState<Set<string>>(new Set(persisted?.langs ?? [...DEFAULT_LANGS]));
  const [kinds, setKinds] = useState<Set<string>>(new Set(persisted?.kinds ?? [...DEFAULT_KINDS]));
  const [pkg, setPkg] = useState(persisted?.pkg ?? "all");
  const [focusRequest, setFocusRequest] = useState<{ id: string; nonce: number } | null>(null);

  const onViewChange = useCallback((z: number, p: { x: number; y: number }) => {
    setZoom(z);
    setPan(p);
  }, []);

  // One-shot fetch with 202-poll. The server builds in the background and
  // answers 202 until the fingerprinted index is cached.
  useEffect(() => {
    let cancelled = false;
    let timer: ReturnType<typeof setTimeout> | undefined;
    setStatus("indexing");
    setError(null);
    const tick = async () => {
      try {
        const resp = await api.uml(projectRoot, host);
        if (cancelled) return;
        if (resp.state === "ready" && resp.index) {
          setIndex(resp.index);
          setStatus("ready");
        } else if (resp.state === "indexing") {
          timer = setTimeout(tick, 1000);
        } else {
          setError(resp.error ?? "UML index failed");
          setStatus("error");
        }
      } catch (e) {
        if (cancelled) return;
        setError(e instanceof Error ? e.message : String(e));
        setStatus("error");
      }
    };
    void tick();
    return () => {
      cancelled = true;
      if (timer) clearTimeout(timer);
    };
  }, [projectRoot, host, retry]);

  // Persist view state, debounced.
  const persistRef = useRef<number | undefined>(undefined);
  useEffect(() => {
    const snapshot: UmlViewStateV1 = {
      view, zoom, panX: pan.x, panY: pan.y,
      selectedId, query,
      langs: [...langs], kinds: [...kinds], pkg,
    };
    window.clearTimeout(persistRef.current);
    persistRef.current = window.setTimeout(() => saveUmlViewState(scope, snapshot), 250);
  }, [scope, view, zoom, pan, selectedId, query, langs, kinds, pkg]);

  const symbols = useMemo(() => index?.symbols ?? [], [index]);
  const packages = useMemo(() => index?.packages ?? [], [index]);

  const filter: UmlFilter = useMemo(() => ({ query, langs, kinds, pkg }), [query, langs, kinds, pkg]);
  const hits = useMemo(() => symbols.filter((s) => matchesFilter(s, filter)), [symbols, filter]);
  const shown = hits.slice(0, LIST_RENDER_CAP);

  const selected = useMemo(
    () => (selectedId ? symbols.find((s) => s.id === selectedId) ?? null : null),
    [symbols, selectedId],
  );

  const selectSymbol = useCallback((id: string, focus: boolean) => {
    setSelectedId(id);
    if (focus) setFocusRequest((f) => ({ id, nonce: (f?.nonce ?? 0) + 1 }));
  }, []);

  const absPath = useCallback((rel: string) => `${projectRoot.replace(/\/+$/, "")}/${rel}`, [projectRoot]);

  const openSource = useCallback(
    (s: UmlSymbol) => {
      const detail: OpenFileDetail = { path: absPath(s.file), line: s.line, projectRoot };
      window.dispatchEvent(new CustomEvent(OPEN_FILE_EVENT, { detail }));
    },
    [absPath, projectRoot],
  );

  const toggle = useCallback((set: Set<string>, value: string, apply: (s: Set<string>) => void) => {
    const next = new Set(set);
    if (next.has(value)) next.delete(value);
    else next.add(value);
    apply(next);
  }, []);

  const onListKeyDown = (e: React.KeyboardEvent<HTMLDivElement>) => {
    if (hits.length === 0) return;
    const idx = selectedId ? hits.findIndex((s) => s.id === selectedId) : -1;
    if (e.key === "ArrowDown" || e.key === "ArrowUp") {
      e.preventDefault();
      const delta = e.key === "ArrowDown" ? 1 : -1;
      const next = hits[Math.min(hits.length - 1, Math.max(0, (idx === -1 ? 0 : idx) + delta))];
      if (next) selectSymbol(next.id, false);
    } else if (e.key === "Enter" && idx >= 0) {
      e.preventDefault();
      selectSymbol(hits[idx].id, true);
    }
  };

  const total = index ? index.symbols.length : 0;

  return (
    <div className="flex h-full min-h-0 flex-col bg-[#0d1117] text-[#d7e0ea]">
      {/* header */}
      <div className="flex flex-wrap items-center gap-2 border-b border-[#26303f] bg-[#0a0e14] px-3 py-1.5 text-xs">
        <span className="flex items-center gap-1.5 font-semibold"><Network size={13} className="text-[#58a6ff]" />UML</span>
        {index && <span className="rounded border border-[#26303f] px-1.5 py-0.5 font-mono text-[10px] text-[#8b98a9]">{index.monorepo === "none" ? "go module" : index.monorepo}</span>}
        {status === "indexing" && (
          <span className="flex items-center gap-1.5 text-[#8b98a9]"><Loader2 size={12} className="animate-spin" />Indexing Go sources…</span>
        )}
        {status === "ready" && index && (
          <span className="text-[#8b98a9]">{index.packages.length} packages · {total.toLocaleString()} symbols · {index.edges.length} import edges</span>
        )}
        {status === "error" && (
          <span className="flex items-center gap-1.5 text-[#ffa657]"><AlertTriangle size={12} />{error}</span>
        )}
        {index?.truncated && <span className="rounded border border-[#4a3b1a] bg-[#2a2214] px-1.5 py-0.5 text-[10px] text-[#e6c97a]">truncated at file cap</span>}
        <button
          type="button"
          className="ml-auto flex items-center gap-1 rounded border border-[#33405a] px-2 py-1 text-[11px] text-[#c8d3e0] hover:border-[#58a6ff]"
          onClick={() => setRetry((n) => n + 1)}
          title="Re-index (fingerprint decides whether a rebuild actually happens)"
        >
          <RefreshCw size={11} />Refresh
        </button>
      </div>

      <div className="grid min-h-0 flex-1 grid-cols-[280px_minmax(0,1fr)_280px]">
        {/* left: search + list */}
        <div className="flex min-h-0 flex-col border-r border-[#26303f] bg-[#141a22]" onKeyDown={onListKeyDown}>
          <div className="border-b border-[#26303f] p-2">
            <div className="flex items-center gap-1.5 rounded border border-[#33405a] bg-[#1a2230] px-2 focus-within:border-[#58a6ff]">
              <Search size={12} className="shrink-0 text-[#8b98a9]" />
              <input
                value={query}
                onChange={(e) => setQuery(e.target.value)}
                onKeyDown={(e) => {
                  if (e.key === "Escape") setQuery("");
                }}
                placeholder="Search symbols…  ( / )"
                spellCheck={false}
                className="h-7 w-full bg-transparent text-xs outline-none placeholder:text-[#5b6a7f]"
                aria-label="Search symbols"
              />
            </div>
            <div className="mt-1.5 flex flex-wrap gap-1">
              {LANGS.map((l) => {
                const available = l === "Go"; // TS/Python land with the umltreesitter build tag
                return (
                  <button
                    key={l}
                    type="button"
                    disabled={!available}
                    title={available ? undefined : "TypeScript/Python analysis ships behind the umltreesitter build tag (deferred)"}
                    onClick={() => toggle(langs, l, setLangs)}
                    className={`rounded-full border px-2 py-0.5 text-[10px] ${langs.has(l) ? "border-[#58a6ff] bg-[#58a6ff] text-[#03152e]" : "border-[#33405a] text-[#8b98a9]"} ${available ? "" : "cursor-not-allowed opacity-40"}`}
                  >
                    {l === "Python" ? "Python" : l}
                  </button>
                );
              })}
              {KINDS.map((k) => (
                <button
                  key={k}
                  type="button"
                  onClick={() => toggle(kinds, k, setKinds)}
                  className={`rounded-full border px-2 py-0.5 text-[10px] ${kinds.has(k) ? "border-[#58a6ff] bg-[#58a6ff] text-[#03152e]" : "border-[#33405a] text-[#8b98a9]"}`}
                >
                  {k}
                </button>
              ))}
            </div>
            <select
              value={pkg}
              onChange={(e) => setPkg(e.target.value)}
              className="mt-1.5 h-7 w-full rounded border border-[#33405a] bg-[#1a2230] px-1.5 text-[11px] text-[#c8d3e0] outline-none"
              aria-label="Package filter"
            >
              <option value="all">all packages ({packages.length})</option>
              {packages.map((p) => (
                <option key={p.id} value={p.id}>{p.id} ({p.symbol_count})</option>
              ))}
            </select>
          </div>
          <div className="flex items-center justify-between border-b border-[#26303f] px-2 py-1 text-[10px] text-[#8b98a9]">
            <span>{hits.length.toLocaleString()} result{hits.length === 1 ? "" : "s"}{hits.length > LIST_RENDER_CAP ? ` (showing ${LIST_RENDER_CAP})` : ""}</span>
            <span>↑↓ move · ↵ focus · esc clear</span>
          </div>
          <div className="min-h-0 flex-1 overflow-y-auto" role="listbox" aria-label="Symbols">
            {shown.map((s) => (
              <button
                key={s.id}
                type="button"
                role="option"
                aria-selected={s.id === selectedId}
                onClick={() => selectSymbol(s.id, true)}
                className={`block w-full border-b border-[#1d2530] border-l-2 px-2 py-1.5 text-left hover:bg-[#182031] ${s.id === selectedId ? "border-l-[#58a6ff] bg-[#1f6feb33]" : "border-l-transparent"}`}
              >
                <div className="flex items-center gap-1.5 text-[11px] font-semibold text-[#d7e0ea]">
                  <span className="rounded bg-[#00add822] px-1 font-mono text-[9px] text-[#00add8]">{s.lang === "Python" ? "PY" : s.lang}</span>
                  <span className="truncate">{s.name}</span>
                  <span className="ml-auto font-mono text-[8px] uppercase tracking-wide text-[#8b98a9]">{s.kind}</span>
                </div>
                <div className="mt-0.5 flex items-center justify-between gap-2 font-mono text-[9px] text-[#8b98a9]">
                  <span className="truncate">{s.file}:{s.line}</span>
                  <span className="shrink-0">{s.pkg}</span>
                </div>
                {s.doc && <div className="mt-0.5 truncate text-[10px] text-[#8b98a9]">{s.doc}</div>}
              </button>
            ))}
            {status === "ready" && hits.length === 0 && (
              <div className="px-3 py-6 text-center text-[11px] text-[#8b98a9]">No matches — loosen the filters or the query.</div>
            )}
          </div>
        </div>

        {/* center: diagram */}
        <div className="relative min-w-0 bg-[#0d1117]">
          {/* toolbar */}
          <div className="absolute left-2 top-2 z-10 flex items-center gap-1 rounded-lg border border-[#33405a] bg-[#141a22e6] p-1 backdrop-blur">
            <button type="button" className="h-7 w-7 rounded hover:bg-[#1a2230]" onClick={() => onViewChange(Math.max(0.35, zoom / 1.25), pan)} title="Zoom out" aria-label="Zoom out">−</button>
            <span className="w-10 text-center text-[11px] tabular-nums text-[#8b98a9]">{Math.round(zoom * 100)}%</span>
            <button type="button" className="h-7 w-7 rounded hover:bg-[#1a2230]" onClick={() => onViewChange(Math.min(2.6, zoom * 1.25), pan)} title="Zoom in" aria-label="Zoom in">+</button>
            <button type="button" className="h-7 w-7 rounded text-[11px] hover:bg-[#1a2230]" onClick={() => onViewChange(1, { x: 0, y: 0 })} title="Reset view" aria-label="Reset view">⤾</button>
          </div>
          <div className="absolute left-1/2 top-2 z-10 flex -translate-x-1/2 gap-0.5 rounded-lg border border-[#33405a] bg-[#141a22e6] p-1 backdrop-blur" role="tablist" aria-label="Diagram view">
            {(["map", "classes"] as UmlView[]).map((v) => (
              <button
                key={v}
                type="button"
                role="tab"
                aria-selected={view === v}
                onClick={() => setView(v)}
                className={`rounded px-3 py-1 text-[11px] ${view === v ? "bg-[#58a6ff] font-bold text-[#03152e]" : "text-[#8b98a9]"}`}
              >
                {v === "map" ? "Package map" : "Class diagram"}
              </button>
            ))}
          </div>
          {status === "error" && (
            <div className="absolute inset-0 grid place-items-center">
              <div className="max-w-sm rounded-lg border border-[#4a3b1a] bg-[#2a2214] p-4 text-center text-xs text-[#e6c97a]">
                <AlertTriangle className="mx-auto mb-2" size={18} />
                <p>{error}</p>
                <button type="button" className="mt-2 rounded border border-[#e6c97a] px-3 py-1" onClick={() => setRetry((n) => n + 1)}>Retry</button>
              </div>
            </div>
          )}
          {status === "indexing" && (
            <div className="absolute inset-0 grid place-items-center">
              <div className="flex items-center gap-2 text-xs text-[#8b98a9]"><Loader2 size={16} className="animate-spin" />Building the symbol index…</div>
            </div>
          )}
          {status === "ready" && index && (
            <UmlDiagram
              view={view}
              index={index}
              zoom={zoom}
              pan={pan}
              onViewChange={onViewChange}
              selectedId={selectedId}
              onSelect={(id) => selectSymbol(id, false)}
              onOpenSource={openSource}
              focusRequest={focusRequest}
            />
          )}
          <div className="pointer-events-none absolute bottom-2 left-2 z-10 rounded border border-[#33405a] bg-[#141a22e6] px-2 py-1 text-[9px] text-[#8b98a9]">
            ctrl/⌘+scroll to zoom · drag to pan · click node to inspect · double-click to open source
          </div>
        </div>

        {/* right: inspector */}
        <div className="min-h-0 overflow-y-auto border-l border-[#26303f] bg-[#141a22] p-3 text-xs">
          {selected ? (
            <>
              <div className="text-sm font-bold text-[#e8eef6]">{selected.name}</div>
              <div className="mt-0.5 font-mono text-[9px] uppercase tracking-widest text-[#8b98a9]">
                {selected.kind}{selected.receiver ? ` · receiver ${selected.receiver}` : ""} · {selected.pkg}
              </div>
              <div className="mt-2 border-t border-[#26303f] pt-2">
                <div className="mb-1 text-[9px] font-semibold uppercase tracking-widest text-[#8b98a9]">Location</div>
                <button
                  type="button"
                  onClick={() => openSource(selected)}
                  className="flex w-full items-center gap-1.5 break-all rounded border border-[#33405a] bg-[#1a2230] px-2 py-1.5 text-left font-mono text-[10px] text-[#58a6ff] hover:border-[#58a6ff]"
                  title="Open in the editor at this line"
                >
                  <ExternalLink size={11} className="shrink-0" />{selected.file}:{selected.line}
                </button>
                <button
                  type="button"
                  onClick={() => openSource(selected)}
                  className="mt-1.5 w-full rounded bg-[#58a6ff] px-2 py-1.5 text-[11px] font-bold text-[#03152e] hover:bg-[#79b8ff]"
                >
                  Open in editor at line {selected.line}
                </button>
              </div>
              <div className="mt-2 border-t border-[#26303f] pt-2">
                <div className="mb-1 text-[9px] font-semibold uppercase tracking-widest text-[#8b98a9]">Doc comment</div>
                {selected.doc ? (
                  <>
                    <pre className="whitespace-pre-wrap rounded border border-[#26303f] bg-[#1a2230] p-2 text-[10px] leading-snug text-[#c8d3e0]">{selected.doc}</pre>
                    <div className="mt-1 text-[9px] text-[#5b6a7f]">from the source doc comment</div>
                  </>
                ) : (
                  <div className="rounded border border-[#4a3b1a] bg-[#2a2214] p-2 text-[10px] text-[#e6c97a]">
                    No doc comment. Small-model generation is deferred (see TODO.md).
                  </div>
                )}
              </div>
              <div className="mt-2 border-t border-[#26303f] pt-2">
                <div className="mb-1 flex items-center justify-between text-[9px] font-semibold uppercase tracking-widest text-[#8b98a9]">
                  <span>Members ({selected.members?.length ?? 0})</span>
                  {selected.sig && <span className="normal-case text-[#5b6a7f]">{selected.kind === "func" || selected.kind === "method" ? "" : ""}</span>}
                </div>
                <ul className="font-mono text-[10px] text-[#a9b8ca]">
                  {(selected.members ?? []).slice(0, 24).map((m, i) => (
                    <li key={i} className="border-b border-dashed border-[#1d2530] py-0.5">
                      <span className={m.kind === "method" ? "text-[#7ee787]" : "text-[#ffa657]"}>{m.kind === "method" ? "fn" : "fld"}</span> {m.name}
                    </li>
                  ))}
                  {(selected.members?.length ?? 0) > 24 && <li className="py-0.5 text-[#5b6a7f]">+{selected.members!.length - 24} more…</li>}
                  {(selected.members?.length ?? 0) === 0 && <li className="text-[#5b6a7f]">—</li>}
                </ul>
                {selected.sig && (
                  <div className="mt-2">
                    <div className="mb-0.5 text-[9px] font-semibold uppercase tracking-widest text-[#8b98a9]">Signature</div>
                    <code className="block break-all rounded border border-[#26303f] bg-[#1a2230] p-1.5 text-[9px] text-[#a9b8ca]">{selected.sig}</code>
                  </div>
                )}
              </div>
            </>
          ) : (
            <div className="flex h-full flex-col items-center justify-center gap-2 text-center text-[#5b6a7f]">
              <Braces size={22} />
              <p className="text-[11px]">Select a symbol in the list<br />or click a node in the diagram.</p>
              <p className="mt-1 flex items-center gap-1 text-[9px]"><Package size={10} />Doc comments come from the source.</p>
              <p className="flex items-center gap-1 text-[9px]"><FileCode2 size={10} />TS/JS/Python analysis is deferred (umltreesitter tag).</p>
            </div>
          )}
        </div>
      </div>
    </div>
  );
}
