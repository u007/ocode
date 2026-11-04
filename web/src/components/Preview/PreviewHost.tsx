import { useEffect, useRef, useState } from "react";
import { BrowserPanel } from "../Browser/BrowserPanel";
import type { StateKey } from "../../lib/browserStore";
import { previewKindForPath, resolvePreviewDoc, type PreviewOpenRequest } from "../../lib/previewKind";
import { resolveAgainstRoot } from "../../lib/outOfProjectFiles";
import { api } from "../../api/client";
import PreviewSurface from "./PreviewSurface";
import { OutsideRootGrantPrompt, type GrantAnchor } from "./OutsideRootGrantPrompt";
import { isOutsideProjectRoot } from "./outsideRootAccess";
import { PreviewWidthMenu } from "./PreviewWidthMenu";
import { PrintPreviewButton } from "./PrintPreviewButton";
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
  /** Set when the user granted this file's folder: the project root the request
   *  was anchored to before the grant (projectRoot is then the granted root). */
  grantedFrom?: string;
}

/** A request the server refused as outside the project, waiting for the user
 *  to grant its folder. `page` is applied once the grant re-anchors the file. */
type PendingGrant = Doc & { page: number };

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
  // Restore this SESSION SURFACE's last sidebar state on MOUNT. The panel is
  // keyed by the active session tab, so switching sessions (or projects)
  // unmounts and remounts it — without this the file, page, and active tab all
  // reset, and without per-session keying one chat's preview would leak into
  // every other chat. Read once per mount (not per render) and seed from it.
  const initialRef = useRef<SidebarPreviewState | null | undefined>(undefined);
  if (initialRef.current === undefined) {
    initialRef.current = loadSidebarPreviewState(stateKey);
  }
  const initial = initialRef.current;

  const [surface, setSurface] = useState<Surface>(initial?.surface === "preview" ? "preview" : "browser");
  const restoringDoc = initial?.surface === "preview" && !!initial.path && !!initial.kind;
  const [doc, setDoc] = useState<Doc | null>(() =>
    initial?.surface === "preview" && initial.path && initial.kind
      ? {
          path: initial.path,
          kind: initial.kind,
          projectRoot: initial.projectRoot,
          projectHost: initial.projectHost,
          grantedFrom: initial.grantedFrom,
        }
      : null,
  );
  // A restored doc is not rendered until its mount-time access check passes: its
  // root may be a grant that did not survive a server restart.
  const [docVerified, setDocVerified] = useState(!restoringDoc);
  const [pendingGrant, setPendingGrant] = useState<PendingGrant | null>(null);
  // Every request, grant and restore check takes a new token; an access check
  // that resolves under an older token is stale and dropped.
  const probeTokenRef = useRef(0);
  // Set once the panel unmounts: a probe still in flight then resolves with no
  // mounted panel to update (see the request effect). The reset in the effect
  // body keeps StrictMode's mount → cleanup → mount cycle correct.
  const unmountedRef = useRef(false);
  useEffect(() => {
    unmountedRef.current = false;
    return () => {
      unmountedRef.current = true;
    };
  }, []);
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
    const token = ++probeTokenRef.current;
    const resolved = resolvePreviewDoc(request.path);
    if (!resolved.kind) {
      setPendingGrant(null);
      setDoc(null);
      setUnsupported({ path: resolved.unsupported, projectRoot: request.projectRoot ?? projectRoot });
      setSurface("preview");
      return;
    }
    setUnsupported(null);
    const anchor = request.projectRoot ?? projectRoot;
    const anchorHost = request.projectHost ?? projectHost;
    const next: Doc = { path: request.path, kind: resolved.kind, projectRoot: anchor, projectHost: anchorHost };
    const nextPage = Math.max(1, request.page);
    // Ask the server before showing the file. An outside-root file gets the
    // grant prompt; the viewer would only fail with the raw refusal. The grant
    // names the file's ABSOLUTE path, resolved against the anchor root.
    void isOutsideProjectRoot(request.path, anchor, anchorHost).then((outside) => {
      if (token !== probeTokenRef.current) return;
      if (unmountedRef.current) {
        // The panel is gone, so an outside-root grant prompt cannot be shown:
        // that request is dropped and the file must be opened again. An in-root
        // file is saved under this session surface so the next mount restores it.
        // Same belongs-here test as the persist effect below.
        const belongsHere =
          (next.projectHost ?? "") === (projectHost ?? "") &&
          (!next.projectRoot ||
            next.projectRoot === projectRoot ||
            (next.grantedFrom !== undefined && next.grantedFrom === projectRoot));
        if (!outside && belongsHere) {
          saveSidebarPreviewState(stateKey, {
            surface: "preview",
            path: next.path,
            kind: next.kind,
            projectRoot: next.projectRoot,
            projectHost: next.projectHost,
            grantedFrom: next.grantedFrom,
            page: nextPage,
            unsupportedPath: undefined,
          });
        }
        return;
      }
      setSurface("preview");
      const grantPath = outside ? resolveAgainstRoot(anchor ?? "", request.path) : null;
      if (grantPath !== null) {
        setPendingGrant({ ...next, path: grantPath, page: nextPage });
        return;
      }
      setPendingGrant(null);
      setDoc(next);
      setDocVerified(true);
      setPage(nextPage);
    });
    // onConsumeActivation is a stable App callback; not a trigger.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [request, nonce, projectRoot, projectHost]);

  // Access check for a restored doc, once on mount. A request applied on this
  // mount already superseded it (lastNonceRef is set only by the request effect).
  // Outside the project now means the grant it was anchored to is gone: clear
  // it silently. Never prompt on page load; only an explicit request does that.
  useEffect(() => {
    if (!restoringDoc || lastNonceRef.current !== 0 || !initial?.path) return;
    const token = probeTokenRef.current;
    void isOutsideProjectRoot(
      initial.path,
      initial.projectRoot ?? projectRoot,
      initial.projectHost ?? projectHost,
    ).then((outside) => {
      if (token !== probeTokenRef.current) return;
      if (outside) setDoc(null);
      setDocVerified(true);
    });
    // Mount-only: the restored state is read once, like the initial-read above.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  // Persist the shell state for this SESSION SURFACE so the next mount (session
  // or project switch back) restores it. Runs on mount too, which just rewrites
  // the restored value — idempotent. A doc whose anchor differs from this pane's
  // project (a request that carried its own root/host) must NOT be stored under
  // this session's key, or switching here would later restore a foreign file.
  // A granted doc is anchored to its granted root, so it belongs here only when
  // it was granted from this pane's root (grantedFrom), never for any other root.
  useEffect(() => {
    const docBelongsHere =
      !!doc &&
      (doc.projectHost ?? "") === (projectHost ?? "") &&
      (!doc.projectRoot ||
        doc.projectRoot === projectRoot ||
        (doc.grantedFrom !== undefined && doc.grantedFrom === projectRoot));
    // Same containment check for the legacy fallback: a request that carried
    // another project's root must not be restored from this project's slot.
    const unsupportedBelongsHere =
      !!unsupported && (unsupported.projectRoot ?? projectRoot ?? "") === (projectRoot ?? "");
    // A foreign doc/unsupported entry cannot be stored here — but do NOT fall
    // through to writing the path-less snapshot: that would overwrite this
    // project's valid saved preview with an empty entry, so switching away and
    // back would drop it. Leave the stored state untouched instead.
    if ((doc && !docBelongsHere) || (unsupported && !unsupportedBelongsHere)) return;
    saveSidebarPreviewState(stateKey, {
      surface,
      path: docBelongsHere ? doc.path : undefined,
      kind: docBelongsHere ? doc.kind : undefined,
      projectRoot: docBelongsHere ? doc.projectRoot : undefined,
      projectHost: docBelongsHere ? doc.projectHost : undefined,
      grantedFrom: docBelongsHere ? doc.grantedFrom : undefined,
      page: docBelongsHere ? page : undefined,
      unsupportedPath: unsupportedBelongsHere ? unsupported.path : undefined,
    });
  }, [stateKey, projectRoot, projectHost, surface, doc, page, unsupported]);

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

  // The user confirmed the grant: re-anchor the pending file to the granted root
  // and show it. The token bump keeps an in-flight access check from overriding
  // the explicit grant.
  const grantPending = (anchor: GrantAnchor) => {
    if (!pendingGrant) return;
    probeTokenRef.current += 1;
    setDoc({
      path: anchor.path,
      kind: pendingGrant.kind,
      projectRoot: anchor.projectRoot,
      projectHost: pendingGrant.projectHost,
      grantedFrom: pendingGrant.projectRoot,
    });
    setDocVerified(true);
    setPage(pendingGrant.page);
    setPendingGrant(null);
  };

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
          {/* App passes the ACTIVE project's own host here (never a lookup by
              path), and Project.host is omitted for local projects, so an
              absent projectHost is this machine, not an unresolved project. */}
          <BrowserPanel key={stateKey} stateKey={stateKey} mode="side" host={projectHost ?? ""} />
        </div>
      ) : pendingGrant ? (
        <div className="flex min-h-0 flex-1 flex-col">
          <OutsideRootGrantPrompt path={pendingGrant.path} host={pendingGrant.projectHost} onGrant={grantPending} />
        </div>
      ) : unsupported ? (
        <div className="flex min-h-0 flex-1 flex-col">
          <LegacyOfficePane
            path={unsupported.path}
            projectRoot={unsupported.projectRoot}
            projectHost={doc?.projectHost ?? projectHost}
          />
        </div>
      ) : doc && !docVerified ? (
        <div className="flex min-h-0 flex-1 flex-col" aria-busy="true" />
      ) : doc ? (
        <div className="flex min-h-0 flex-1 flex-col">
          <div className="flex shrink-0 items-center gap-1 border-b border-border px-2 py-1 text-xs">
            <span className="min-w-0 flex-1 truncate font-mono text-muted-foreground" title={doc.path}>
              {doc.path}
            </span>
            {osOpenState && <span className="shrink-0 text-muted-foreground">{osOpenState}</span>}
            {/* Reading-column control. Markdown only: every other kind either
                ignores it (PDF/deck/image) or is code, which stays full width. */}
            {doc.kind === "markdown" && <PreviewWidthMenu />}
            <PrintPreviewButton
              kind={doc.kind}
              title={doc.path.split("/").pop() || doc.path}
              source={{ origin: "disk", path: doc.path, projectRoot: doc.projectRoot, projectHost: doc.projectHost ?? projectHost }}
              className="shrink-0 flex items-center gap-1 rounded px-1.5 py-0.5 hover:bg-muted"
            />
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
