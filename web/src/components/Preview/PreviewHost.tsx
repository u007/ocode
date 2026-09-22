import { useEffect, useRef, useState } from "react";
import { BrowserPanel } from "../Browser/BrowserPanel";
import type { StateKey } from "../../lib/browserStore";
import { previewKindForPath, resolvePreviewDoc, type PreviewOpenRequest } from "../../lib/previewKind";
import { api } from "../../api/client";
import PreviewSurface from "./PreviewSurface";
import LegacyOfficePane from "./LegacyOfficePane";
import { dispatchOpenPreview } from "../../lib/previewKind";
import { loadSidebarPreviewState, saveSidebarPreviewState, type SidebarPreviewState } from "./sidebarPreviewState";
import { eventBus } from "../../lib/eventBus";
import {
  isMutatingTool,
  mutatedPathsFromToolCall,
  previewPathMutated,
} from "../../lib/previewLiveMutations";

type Surface = "browser" | "preview";

interface Doc {
  path: string;
  kind: Exclude<ReturnType<typeof previewKindForPath>, null>;
  projectRoot?: string;
  projectHost?: string;
}

/**
 * Sidebar PreviewHost: the side panel's two-tab shell.
 * - Browser tab: the existing BrowserPanel (side mode) untouched — web
 *   browsing keeps working exactly like before.
 * - Preview tab: file preview + editor. Text/code is editable (Monaco),
 *   markdown renders with interactive mermaid diagrams, PDFs paginate via
 *   pdf.js, Word via docx-preview, PowerPoint via the slide parser with
 *   filmstrip + Present click-through, images inline. Every text surface
 *   supports highlight → Copy / Ask-LLM; .mmd and mermaid fences support
 *   clickable nodes with branch Ask-AI, zoom, and linked-file opening.
 *
 * The AI drives this panel through the `preview_open` tool (scanned from
 * the chat transcript by App) or `ocode:open-preview` window events (file
 * tree, diagram links) — both arrive here as `request` + `nonce`.
 */
export default function PreviewHost({
  stateKey,
  projectRoot,
  projectHost,
  request,
  nonce,
  onConsumeActivation,
  sessionId,
}: {
  stateKey: StateKey;
  projectRoot?: string;
  /** Active session id, used to route tool events on the SSE bus so the
   *  previewed file refreshes live while the AI edits it. */
  sessionId?: string | null;
  /** Fallback host for requests that don't carry their own (derived from
   *  the active project at the App boundary). */
  projectHost?: string;
  request: PreviewOpenRequest | null;
  nonce: number;
  /** Acknowledge a consumed activation so it is not replayed after this panel
   *  remounts (App remounts it on every session/project switch). */
  onConsumeActivation?: () => void;
}) {
  // Restore the project's last sidebar state on MOUNT. The panel is keyed by
  // the active session tab, so a project switch unmounts and remounts it —
  // without this the file, page, and active tab all reset. Read once per mount
  // (not per render) and seed the initial state from it.
  const initialRef = useRef<SidebarPreviewState | null | undefined>(undefined);
  if (initialRef.current === undefined) {
    initialRef.current = loadSidebarPreviewState(projectRoot, projectHost);
  }
  const initial = initialRef.current;

  const [surface, setSurface] = useState<Surface>(initial?.surface === "preview" ? "preview" : "browser");
  const [doc, setDoc] = useState<Doc | null>(() =>
    initial?.surface === "preview" && initial.path && initial.kind
      ? { path: initial.path, kind: initial.kind, projectRoot: initial.projectRoot, projectHost: initial.projectHost }
      : null,
  );
  const [page, setPage] = useState(() => Math.max(1, initial?.page ?? 1));
  const [osOpenState, setOsOpenState] = useState<string | null>(null);
  const lastNonceRef = useRef(0);
  // Live-refresh revision: bumped whenever the active session's tool stream
  // mutates the previewed file. Viewers (markdown/text/mmd) refetch on change;
  // PDF/office viewers re-render from their own sources downstream.
  const [revision, setRevision] = useState(0);
  const revisionRef = useRef(0);
  // Whether a turn is running (tool activity implies one) — viewers use this
  // to re-arm tail-following while the AI keeps appending.
  const [turnActive, setTurnActive] = useState(false);

  // New activation → show the file in the Preview tab, starting at the
  // requested page/slide. Legacy .doc/.ppt (see resolvePreviewDoc) land on
  // an explicit OS-open fallback instead of a broken preview. The fallback
  // keeps the request's own project root so OS-open resolves (and is
  // containment-checked) against the right project in multi-project windows.
  // NOTE: "Open in app" always runs on the SERVER host (HandleOpenFile has
  // no remote branch — there is no remote desktop to open a window on).
  // For a remote doc the button is therefore hidden: showing it would open
  // an unrelated server-local file (or 400) while implying the remote file
  // opened.
  const [unsupported, setUnsupported] = useState<{ path: string; projectRoot?: string } | null>(() =>
    initial?.surface === "preview" && initial.unsupportedPath
      ? { path: initial.unsupportedPath, projectRoot: initial.projectRoot }
      : null,
  );
  useEffect(() => {
    if (!request || nonce === lastNonceRef.current) return;
    lastNonceRef.current = nonce;
    // The activation is one-shot: acknowledge it immediately so a later remount
    // of this panel cannot replay it over the state restored for another
    // project. App opens the panel off the same nonce.
    onConsumeActivation?.();
    const resolved = resolvePreviewDoc(request.path, request.kind);
    if (!resolved.kind) {
      setDoc(null);
      setUnsupported({ path: resolved.unsupported, projectRoot: request.projectRoot ?? projectRoot });
      setSurface("preview");
      return;
    }
    setUnsupported(null);
    const anchor = request.projectRoot ?? projectRoot;
    const anchorHost = request.projectHost ?? projectHost;
    setDoc({ path: request.path, kind: resolved.kind, projectRoot: anchor, projectHost: anchorHost });
    setPage(Math.max(1, request.page));
    setSurface("preview");
    // onConsumeActivation is a stable App callback; not a trigger.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [request, nonce, projectRoot, projectHost]);

  // Persist the shell state for this project so the next mount (project switch
  // back) restores it. Runs on mount too, which just rewrites the restored
  // value — idempotent. A doc whose anchor differs from this pane's project
  // (a request that carried its own root/host) must NOT be stored under this
  // project's key, or switching here would later restore a foreign file.
  useEffect(() => {
    const docBelongsHere =
      !!doc &&
      (doc.projectHost ?? "") === (projectHost ?? "") &&
      (!doc.projectRoot || doc.projectRoot === projectRoot);
    // Same containment check for the legacy fallback: a request that carried
    // another project's root must not be restored from this project's slot.
    const unsupportedBelongsHere =
      !!unsupported && (unsupported.projectRoot ?? projectRoot ?? "") === (projectRoot ?? "");
    // A foreign doc/unsupported entry cannot be stored here — but do NOT fall
    // through to writing the path-less snapshot: that would overwrite this
    // project's valid saved preview with an empty entry, so switching away and
    // back would drop it. Leave the stored state untouched instead.
    if ((doc && !docBelongsHere) || (unsupported && !unsupportedBelongsHere)) return;
    saveSidebarPreviewState(projectRoot, projectHost, {
      surface,
      path: docBelongsHere ? doc.path : undefined,
      kind: docBelongsHere ? doc.kind : undefined,
      projectRoot: docBelongsHere ? doc.projectRoot : undefined,
      projectHost: docBelongsHere ? doc.projectHost : undefined,
      page: docBelongsHere ? page : undefined,
      unsupportedPath: unsupportedBelongsHere ? unsupported.path : undefined,
    });
  }, [projectRoot, projectHost, surface, doc, page, unsupported]);

  // Follow project switches for the open doc's anchor (defensive: a project
  // change normally remounts this panel, in which case the initial-read above
  // already applied the right project's state).
  useEffect(() => {
    setDoc((d) => (d && !d.projectRoot && projectRoot ? { ...d, projectRoot } : d));
  }, [projectRoot]);
  useEffect(() => {
    setDoc((d) => (d && !d.projectHost && projectHost ? { ...d, projectHost } : d));
  }, [projectHost]);

  // ── Live refresh: watch the active session's tool stream ──
  // When the AI mutates the previewed file mid-turn (tool_start carries the
  // tool name + raw argument JSON), bump `revision` so the viewers refetch,
  // and track turn state so tail-following can re-arm while it streams.
  // Live view of the open doc for the event subscription below (the
  // subscription must not resubscribe on every doc change, but path matching
  // must always use the CURRENT doc — hence the ref).
  const docRef = useRef<Doc | null>(null);
  docRef.current = doc;
  const rootRef = useRef(projectRoot);
  rootRef.current = projectRoot;

  useEffect(() => {
    if (!sessionId) return;
    const offStart = eventBus.on("tool_start", (env) => {
      if (env.session_id && env.session_id !== sessionId) return;
      const data = env.data as { tool?: string; command?: string };
      if (!data?.tool) return;
      // Any tool activity from this session means a turn is running — that is
      // the tail-follow signal. Only MUTATING tools touching the previewed
      // file bump the revision (viewers refetch).
      setTurnActive(true);
      if (!isMutatingTool(data.tool)) return;
      const d = docRef.current;
      const paths = mutatedPathsFromToolCall(data.tool, data.command);
      if (d && previewPathMutated(d.path, paths, d.projectRoot ?? rootRef.current)) {
        revisionRef.current += 1;
        setRevision(revisionRef.current);
      }
    });
    const offTurnDone = eventBus.on("turn_done", (env) => {
      if (env.session_id && env.session_id !== sessionId) return;
      setTurnActive(false);
    });
    const offTurnError = eventBus.on("turn_error", (env) => {
      if (env.session_id && env.session_id !== sessionId) return;
      setTurnActive(false);
    });
    return () => {
      offStart();
      offTurnDone();
      offTurnError();
    };
  }, [sessionId]);

  const openWithOS = async (target?: string) => {
    const targetPath = target ?? doc?.path;
    // Legacy fallback keeps its own project root (multi-project windows);
    // docs use their anchor, else the pane default.
    // Remote docs never reach this (the button is hidden when a host is
    // set — see the toolbar below), so no host routing is needed here.
    const fallbackRoot = unsupported && targetPath && unsupported.path === targetPath ? unsupported.projectRoot : undefined;
    const targetRoot = fallbackRoot ?? doc?.projectRoot ?? projectRoot;
    const targetHost = doc?.projectHost ?? projectHost;
    if (!targetPath || targetHost) return;
    setOsOpenState("Opening…");
    try {
      await api.openFileWithOS(targetPath, targetRoot);
      setOsOpenState("Opened in OS app");
    } catch (e) {
      setOsOpenState(e instanceof Error ? e.message : String(e));
    } finally {
      setTimeout(() => setOsOpenState(null), 2500);
    }
  };

  const openLinked = (p: string) => dispatchOpenPreview(p, 1, doc?.projectRoot ?? projectRoot, doc?.projectHost ?? projectHost);

  return (
    <div className="flex h-full min-h-0 flex-col">
      <div className="flex shrink-0 items-center gap-1 border-b border-border px-2 py-1" role="tablist" aria-label="Sidebar surface">
        <button
          type="button"
          role="tab"
          aria-selected={surface === "browser"}
          onClick={() => setSurface("browser")}
          className={`rounded px-2 py-0.5 text-xs ${surface === "browser" ? "bg-muted text-foreground" : "text-muted-foreground hover:text-foreground"}`}
        >
          Browser
        </button>
        <button
          type="button"
          role="tab"
          aria-selected={surface === "preview"}
          onClick={() => setSurface("preview")}
          className={`rounded px-2 py-0.5 text-xs ${surface === "preview" ? "bg-muted text-foreground" : "text-muted-foreground hover:text-foreground"}`}
        >
          Preview{doc ? ` · ${doc.path.split("/").pop()}` : ""}
        </button>
      </div>

      {surface === "browser" ? (
        <div className="flex min-h-0 flex-1 flex-col">
          <BrowserPanel key={stateKey} stateKey={stateKey} mode="side" />
        </div>
      ) : unsupported ? (
        <div className="flex min-h-0 flex-1 flex-col">
          <LegacyOfficePane
            path={unsupported.path}
            projectRoot={unsupported.projectRoot}
            projectHost={doc?.projectHost ?? projectHost}
          />
        </div>
      ) : doc ? (
        <div className="flex min-h-0 flex-1 flex-col">
          <div className="flex shrink-0 items-center gap-1 border-b border-border px-2 py-1 text-xs">
            <span className="min-w-0 flex-1 truncate font-mono text-muted-foreground" title={doc.path}>
              {doc.path}
            </span>
            {osOpenState && <span className="shrink-0 text-muted-foreground">{osOpenState}</span>}
            {!(doc.projectHost ?? projectHost) && (
              <button type="button" onClick={() => openWithOS()} className="shrink-0 rounded px-1.5 py-0.5 hover:bg-muted" title="Open with the OS default app (native PowerPoint/Keynote/Word playback)">
                Open in app
              </button>
            )}
            <button
              type="button"
              onClick={() => navigator.clipboard?.writeText(doc.path)}
              className="shrink-0 rounded px-1.5 py-0.5 hover:bg-muted"
              title="Copy file path"
            >
              Copy path
            </button>
          </div>
          <div className="flex min-h-0 flex-1 flex-col">
          <PreviewSurface
            path={doc.path}
            kind={doc.kind}
            projectRoot={doc.projectRoot}
            projectHost={doc.projectHost ?? projectHost}
            page={page}
            onPageChange={setPage}
            slide={page}
            onSlideChange={setPage}
            onOpenFile={openLinked}
            revision={revision}
            followTail={turnActive}
          />
          </div>
        </div>
      ) : (
        <div className="flex flex-1 flex-col items-center justify-center gap-2 p-4 text-center">
          <div className="text-xs text-muted-foreground">No file previewed yet.</div>
          <div className="max-w-[220px] text-[11px] leading-relaxed text-muted-foreground/70">
            Right-click a file → Preview in sidebar, click a diagram node link, or ask the AI to preview a PDF, deck, doc, or diagram.
          </div>
        </div>
      )}
    </div>
  );
}
