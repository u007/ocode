import { useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { api, ApiError } from "../../../api/client";
import type { McpAppRef, McpAppResource } from "../../../api/types";
import { useAppearanceTheme } from "../../../lib/appearanceTheme";
import { openExternalURL } from "../../../lib/externalLinks";
import {
  buildMcpAppSrcDoc,
  createMcpAppHost,
  type McpAppDisplayMode,
  type McpAppHost,
} from "./mcpAppHost";

// Renders an MCP Apps widget (a `ui://` HTML resource) under a tool block.
// The widget runs in a sandboxed srcdoc iframe with an opaque origin; this
// component is the host side of its JSON-RPC-over-postMessage protocol.

const DEFAULT_HEIGHT = 360;
const MIN_HEIGHT = 120;
const MAX_HEIGHT = 800;
const INLINE_MAX_HEIGHT = 800;
const GENERIC_ERROR_CODE = -32000;

interface ViewState {
  /** The user clicked "Open view" on this card. */
  activated: boolean;
  /** Last height the widget reported, clamped to [MIN_HEIGHT, MAX_HEIGHT]. */
  height?: number;
}

// Module-level so the activated flag and height survive the virtualized
// transcript unmounting and remounting this card. The display mode is NOT
// stored here: a remount always starts inline, so fullscreen is never reopened
// unasked.
const viewStates = new Map<string, ViewState>();
// Successful resource fetches only; a failure is never cached so Retry refetches.
const resourceCache = new Map<string, McpAppResource>();

function viewKey(sessionId: string, toolCallId: string): string {
  return `${sessionId}:${toolCallId}`;
}

function isRecord(v: unknown): v is Record<string, unknown> {
  return typeof v === "object" && v !== null && !Array.isArray(v);
}

function clampHeight(h: number): number {
  return Math.min(MAX_HEIGHT, Math.max(MIN_HEIGHT, Math.round(h)));
}

export interface McpAppViewProps {
  sessionId: string;
  toolCallId: string;
  host?: string;
  mcpApp: McpAppRef;
  /** The raw JSON arguments string of the tool call. */
  toolArguments?: string;
  /** The tool's text output, used when there is no structured result. */
  output?: string;
  /** False for an older card: it shows only an "Open view" button until clicked. */
  active: boolean;
}

export function McpAppView(props: McpAppViewProps) {
  const key = viewKey(props.sessionId, props.toolCallId);
  const [activated, setActivated] = useState(() => viewStates.get(key)?.activated === true);

  if (!props.active && !activated) {
    return (
      <div className="mt-1">
        <button
          type="button"
          onClick={() => {
            viewStates.set(key, { ...viewStates.get(key), activated: true });
            setActivated(true);
          }}
          className="rounded border border-border px-2 py-0.5 text-xs text-muted-foreground hover:text-foreground"
        >
          Open view
        </button>
      </div>
    );
  }
  return <ActiveMcpApp {...props} />;
}

function parseToolInput(raw: string | undefined): Record<string, unknown> {
  if (raw === undefined || raw === "") return {};
  try {
    const parsed: unknown = JSON.parse(raw);
    return isRecord(parsed) ? parsed : {};
  } catch (err) {
    // Model-generated arguments that are not JSON: the widget still starts,
    // just without its input. The failure is logged, not dropped.
    console.warn("MCP app: tool arguments are not valid JSON", err);
    return {};
  }
}

function errorBody(err: ApiError): Record<string, unknown> | undefined {
  let body: unknown = err.body;
  if (typeof body === "string") {
    try {
      body = JSON.parse(body);
    } catch {
      // A non-JSON error body carries no code; the message still comes from ApiError.
      return undefined;
    }
  }
  return isRecord(body) ? body : undefined;
}

/** Maps a failed tools/call to the Error shape the protocol host expects:
 *  `code` is the upstream JSON-RPC code when the server reported one. */
function toHostError(err: unknown): Error & { code: number } {
  if (err instanceof ApiError) {
    const body = errorBody(err);
    const code = typeof body?.code === "number" ? body.code : GENERIC_ERROR_CODE;
    const message = typeof body?.error === "string" && body.error !== "" ? body.error : err.message;
    return Object.assign(new Error(message), { code });
  }
  const message = err instanceof Error ? err.message : String(err);
  return Object.assign(new Error(message), { code: GENERIC_ERROR_CODE });
}

function describeLoadError(err: unknown): string {
  if (err instanceof ApiError && err.status === 409) {
    return "This view needs a live session. Send a message in this chat, then retry.";
  }
  return err instanceof Error ? err.message : String(err);
}

type ThemeKind = "dark" | "light";

function ActiveMcpApp({ sessionId, toolCallId, host, mcpApp, toolArguments, output }: McpAppViewProps) {
  const key = viewKey(sessionId, toolCallId);
  const cacheKey = `${host ?? ""}|${sessionId}|${mcpApp.server}|${mcpApp.resource_uri}`;
  const themeKind: ThemeKind = useAppearanceTheme().theme?.kind ?? "dark";

  const [resource, setResource] = useState<McpAppResource | null>(() => resourceCache.get(cacheKey) ?? null);
  const [loadError, setLoadError] = useState<unknown>(null);
  const [reloadToken, setReloadToken] = useState(0);
  const [displayMode, setDisplayMode] = useState<McpAppDisplayMode>("inline");
  // The frame navigated itself away from its srcdoc document: its host is torn
  // down and the view stays closed until the user asks to reload it.
  const [navigated, setNavigated] = useState(false);
  // A ui/open-link request awaiting the user's confirmation. Only one at a time.
  const [pendingLink, setPendingLink] = useState<string | null>(null);
  const [height, setHeight] = useState(() => viewStates.get(key)?.height ?? DEFAULT_HEIGHT);

  const wrapperRef = useRef<HTMLDivElement>(null);
  const iframeRef = useRef<HTMLIFrameElement>(null);
  const hostRef = useRef<McpAppHost | null>(null);
  const modeTimer = useRef<number | null>(null);

  useEffect(() => {
    const cached = resourceCache.get(cacheKey);
    if (cached) {
      setResource(cached);
      setLoadError(null);
      return;
    }
    const ctrl = new AbortController();
    setResource(null);
    setLoadError(null);
    void api
      .fetchMcpAppResource(sessionId, mcpApp.server, mcpApp.resource_uri, host, ctrl.signal)
      .then(
        (res) => {
          resourceCache.set(cacheKey, res);
          if (!ctrl.signal.aborted) setResource(res);
        },
        (err: unknown) => {
          if (!ctrl.signal.aborted) setLoadError(err);
        },
      );
    return () => ctrl.abort();
  }, [cacheKey, sessionId, mcpApp.server, mcpApp.resource_uri, host, reloadToken]);

  // A pending display-mode switch must not fire after unmount (the card is gone).
  useEffect(
    () => () => {
      if (modeTimer.current !== null) window.clearTimeout(modeTimer.current);
    },
    [],
  );

  const srcDoc = useMemo(
    () => (resource ? buildMcpAppSrcDoc(resource.html, resource.csp) : null),
    [resource],
  );

  // Latest values for the host callbacks. Volatile state is read through this
  // ref, so the host's getHostContext reports the CURRENT mode when a fresh
  // iframe initializes.
  const latest = useRef({ sessionId, host, server: mcpApp.server, themeKind, displayMode, toolArguments, output, mcpApp });
  useLayoutEffect(() => {
    latest.current = { sessionId, host, server: mcpApp.server, themeKind, displayMode, toolArguments, output, mcpApp };
  });

  function containerDims(mode: McpAppDisplayMode): { width: number; height: number } | { maxHeight: number } {
    const el = wrapperRef.current;
    if (mode === "fullscreen" && el) return { width: el.clientWidth, height: el.clientHeight };
    return { maxHeight: INLINE_MAX_HEIGHT };
  }

  // One host per mounted iframe. A display-mode change mounts a NEW iframe (the
  // fullscreen view lives under document.body), so the host is recreated with it
  // and the widget re-initializes against the new mode.
  useLayoutEffect(() => {
    if (srcDoc === null) return;
    const frameEl = iframeRef.current;
    if (frameEl === null) return;
    const ctrl = new AbortController();
    // Capture this iframe, not the ref: a torn-down host must never post into
    // the frame that replaced it.
    const frameHost = createMcpAppHost({
      post: (msg) => {
        frameEl.contentWindow?.postMessage(msg, "*");
      },
      callTool: async (name, args) => {
        const l = latest.current;
        try {
          const res = await api.callMcpAppTool(
            l.sessionId,
            { server: l.server, name, arguments: args },
            l.host,
            ctrl.signal,
          );
          return res.result;
        } catch (err) {
          throw toHostError(err);
        }
      },
      // Never opens directly: the widget gets no user gesture, so the URL waits
      // for an explicit Open click (see OpenLinkConfirm).
      openLink: (url) => setPendingLink(url),
      requestDisplayMode: (mode) => {
        // The host posts the {mode} response right after this returns. The
        // switch happens on the next tick, so the response is never lost to
        // the unmount of the frame it answers.
        if (modeTimer.current !== null) window.clearTimeout(modeTimer.current);
        modeTimer.current = window.setTimeout(() => {
          modeTimer.current = null;
          setDisplayMode(mode);
        }, 0);
        return mode;
      },
      onSize: (_width, h) => {
        if (h === undefined || latest.current.displayMode === "fullscreen") return;
        const clamped = clampHeight(h);
        viewStates.set(key, { activated: viewStates.get(key)?.activated ?? false, height: clamped });
        setHeight(clamped);
      },
      getHostContext: () => {
        const l = latest.current;
        return {
          theme: l.themeKind,
          displayMode: l.displayMode,
          availableDisplayModes: ["inline", "fullscreen"],
          platform: "web",
          containerDimensions: containerDims(l.displayMode),
        };
      },
      toolInput: parseToolInput(latest.current.toolArguments),
      toolResult: isRecord(latest.current.mcpApp.result)
        ? latest.current.mcpApp.result
        : { content: [{ type: "text", text: latest.current.output ?? "" }] },
    });
    hostRef.current = frameHost;

    const onMessage = (event: MessageEvent) => {
      // A sandboxed frame without allow-same-origin always reports origin "null";
      // anything else is not this widget. The source window pins it to this frame.
      if (event.origin !== "null") return;
      const frame = frameEl.contentWindow;
      if (!frame || event.source !== frame) return;
      frameHost.handleMessage(event.data);
    };
    window.addEventListener("message", onMessage);

    // The first load of this element is the srcdoc document. Any later load is
    // the frame navigating itself (allow-scripts lets it set its own location),
    // and that document has no CSP and may reach the network: close the view.
    let loads = 0;
    const onLoad = () => {
      loads += 1;
      if (loads === 1) return;
      frameHost.teardown();
      setNavigated(true);
      setDisplayMode("inline");
      setPendingLink(null);
    };
    frameEl.addEventListener("load", onLoad);

    return () => {
      window.removeEventListener("message", onMessage);
      frameEl.removeEventListener("load", onLoad);
      frameHost.teardown();
      ctrl.abort();
      if (hostRef.current === frameHost) hostRef.current = null;
    };
    // containerDims/latest read through refs; a new document or a new iframe
    // (display-mode change) needs a new host.
  }, [srcDoc, key, displayMode, navigated]);

  const themeSeen = useRef(themeKind);
  useLayoutEffect(() => {
    if (themeSeen.current === themeKind) return;
    themeSeen.current = themeKind;
    hostRef.current?.notifyContext({ theme: themeKind });
  }, [themeKind]);

  // Layout effect: the listener is live the moment the Close button is on screen.
  useLayoutEffect(() => {
    if (displayMode !== "fullscreen") return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") setDisplayMode("inline");
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [displayMode]);

  if (resource === null || srcDoc === null) {
    if (loadError !== null) {
      return (
        <div className="mt-1 flex flex-wrap items-center gap-2 text-xs">
          <span className="text-destructive">{describeLoadError(loadError)}</span>
          <button
            type="button"
            onClick={() => setReloadToken((t) => t + 1)}
            className="rounded border border-border px-2 py-0.5 text-muted-foreground hover:text-foreground"
          >
            Retry
          </button>
        </div>
      );
    }
    return <p className="mt-1 text-xs text-muted-foreground">Loading view…</p>;
  }

  if (navigated) {
    return (
      <div className="mt-1 flex flex-wrap items-center gap-2 text-xs">
        <span className="text-destructive">This view navigated away from its document and was closed.</span>
        <button
          type="button"
          onClick={() => setNavigated(false)}
          className="rounded border border-border px-2 py-0.5 text-muted-foreground hover:text-foreground"
        >
          Reload view
        </button>
      </div>
    );
  }

  const fullscreen = displayMode === "fullscreen";
  const prefersBorder = resource.prefers_border === true;
  const inlineHeight = clampHeight(height);

  const frame = (
    <iframe
      ref={iframeRef}
      title={mcpApp.tool}
      sandbox="allow-scripts"
      srcDoc={srcDoc}
      allow=""
      referrerPolicy="no-referrer"
      style={fullscreen ? undefined : { height: inlineHeight }}
      className={
        fullscreen
          ? "h-full w-full border-0 rounded-none"
          : `w-full rounded-md ${prefersBorder ? "border border-border" : "border-0"}`
      }
    />
  );

  const confirmLink =
    pendingLink === null ? null : (
      <OpenLinkConfirm
        url={pendingLink}
        className={fullscreen ? "absolute bottom-3 left-3 right-3 z-10" : "mt-1"}
        onOpen={() => {
          openExternalURL(pendingLink);
          setPendingLink(null);
        }}
        onCancel={() => setPendingLink(null)}
      />
    );

  if (fullscreen) {
    // The fullscreen view is portaled to document.body: a fixed element under the
    // virtual row (which carries a transform) would be confined to that row. The
    // inline slot keeps its last height so the transcript row does not collapse.
    return (
      <>
        <div
          className="mt-1 flex w-full max-w-[880px] items-center justify-center rounded-md border border-dashed border-border text-xs text-muted-foreground"
          style={{ height: inlineHeight }}
        >
          Shown fullscreen
        </div>
        {createPortal(
          <div ref={wrapperRef} className="fixed inset-0 z-50 bg-background">
            {frame}
            {confirmLink}
            <button
              type="button"
              autoFocus
              onClick={() => setDisplayMode("inline")}
              className="absolute right-3 top-3 z-10 rounded border border-border bg-background px-2 py-0.5 text-xs text-muted-foreground hover:text-foreground"
            >
              Close
            </button>
          </div>,
          document.body,
        )}
      </>
    );
  }

  return (
    <div ref={wrapperRef} className="relative mt-1 w-full max-w-[880px] min-w-0">
      {frame}
      {confirmLink}
    </div>
  );
}

function OpenLinkConfirm({
  url,
  className,
  onOpen,
  onCancel,
}: {
  url: string;
  className: string;
  onOpen: () => void;
  onCancel: () => void;
}) {
  return (
    <div
      role="group"
      aria-label="Open link confirmation"
      className={`flex items-center gap-2 rounded-md border border-border bg-background px-2 py-1 text-xs ${className}`}
    >
      <span className="shrink-0 text-muted-foreground">This view wants to open</span>
      <span className="min-w-0 flex-1 truncate font-mono" title={url}>
        {url}
      </span>
      <button
        type="button"
        onClick={onOpen}
        className="shrink-0 rounded border border-border px-2 py-0.5 hover:text-foreground"
      >
        Open
      </button>
      <button
        type="button"
        onClick={onCancel}
        className="shrink-0 rounded border border-border px-2 py-0.5 text-muted-foreground hover:text-foreground"
      >
        Cancel
      </button>
    </div>
  );
}
