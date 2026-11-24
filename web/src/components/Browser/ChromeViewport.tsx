import { useEffect, useRef, useState, useCallback } from "react";
import { useCdpSocket, type NodeDescription } from "./useCdpSocket";
import { useBrowserStore, useBrowserActions, type StateKey, browserStore } from "../../lib/browserStore";
import { findNode, normalizeChromeTree, selectorFor, type DomNode } from "../../lib/domInspector";
import { AnnotationPopover } from "./AnnotationPopover";
import { ANNOTATION_NO_CHAT_ERROR, annotationFileName, composeAnnotationText, cropScreencast, dispatchBrowserAnnotation, type BoxRect } from "./annotation";
import { LoadingSpinner } from "./LoadingSpinner";
import { uploadBrowseFiles } from "../../api/client";
import { ContextMenu, type ContextMenuItem } from "../Layout/ContextMenu";
import { openExternalURL } from "../../lib/externalLinks";
import { createTerminalInputGuard } from "../Terminal/terminalInputGuard";

/** CDP modifier bitmask (Input.dispatchMouseEvent/KeyEvent convention). */
const MOD_ALT = 1;
const MOD_CTRL = 2;
const MOD_META = 4;
const MOD_SHIFT = 8;

function modifiersOf(e: { altKey: boolean; ctrlKey: boolean; metaKey: boolean; shiftKey: boolean }): number {
  return (
    (e.altKey ? MOD_ALT : 0) |
    (e.ctrlKey ? MOD_CTRL : 0) |
    (e.metaKey ? MOD_META : 0) |
    (e.shiftKey ? MOD_SHIFT : 0)
  );
}

/** Printable-key text for CDP: Enter is "\r", a single-character key is
 *  itself, everything else (Backspace, arrows, F-keys, …) has no text. Chrome
 *  inserts `text` itself on keyDown, so Ctrl/Cmd chords must carry none — a
 *  real browser never types "v" for Cmd+V. */
function keyText(e: { key: string; ctrlKey: boolean; metaKey: boolean }): string {
  if (e.ctrlKey || e.metaKey) return "";
  if (e.key === "Enter") return "\r";
  return e.key.length === 1 ? e.key : "";
}

const isMac = () => /Mac|iPhone|iPad/.test(navigator.platform);

/** Chrome's zoom presets; Cmd/Ctrl +/- step through them, 0 resets. */
const ZOOM_STEPS = [0.25, 0.33, 0.5, 0.67, 0.75, 0.8, 0.9, 1, 1.1, 1.25, 1.5, 1.75, 2, 2.5, 3, 4, 5];

function zoomStep(current: number, dir: 1 | -1): number {
  const i = ZOOM_STEPS.findIndex((z) => Math.abs(z - current) < 0.005);
  if (i === -1) {
    // Off-preset (trackpad pinch landed between steps): snap to the nearest step in that direction.
    const next = dir === 1 ? ZOOM_STEPS.find((z) => z > current) : [...ZOOM_STEPS].reverse().find((z) => z < current);
    return next ?? current;
  }
  return ZOOM_STEPS[Math.min(ZOOM_STEPS.length - 1, Math.max(0, i + dir))];
}

/** Zoom shortcut for a key event: +1/-1 step or 0 for reset; null otherwise. */
function zoomShortcut(e: { code: string; key: string; altKey: boolean; ctrlKey: boolean; metaKey: boolean; shiftKey: boolean }): 1 | -1 | 0 | null {
  const primary = isMac() ? e.metaKey : e.ctrlKey;
  if (!primary || e.altKey) return null;
  if (e.code === "Equal" || e.code === "NumpadAdd" || e.key === "+") return 1;
  if (e.code === "Minus" || e.code === "NumpadSubtract") return -1;
  if (e.code === "Digit0" || e.code === "Numpad0") return 0;
  return null;
}

/** CDP `buttons` bitmask for a PointerEvent.button. */
function buttonBit(b: number): number {
  return b === 2 ? 2 : b === 1 ? 4 : 1;
}

/** Browser-chrome shortcuts never reach the page renderer through
 *  Input.dispatchKeyEvent, so translate them to navigation commands here.
 *  Primary modifier is Cmd on mac, Ctrl elsewhere. History uses Cmd+[ / ] on
 *  mac (Cmd+Arrow is a caret command in editable fields) and Alt+Arrow
 *  elsewhere. Returns null when the key is an ordinary page key. */
function chromeShortcut(e: { key: string; code: string; altKey: boolean; ctrlKey: boolean; metaKey: boolean; shiftKey: boolean }): "reload" | "back" | "forward" | null {
  const primary = isMac() ? e.metaKey : e.ctrlKey;
  if (e.key === "F5" || (primary && !e.shiftKey && !e.altKey && e.code === "KeyR")) return "reload";
  if (isMac()) {
    if (primary && !e.shiftKey && !e.altKey && e.code === "BracketLeft") return "back";
    if (primary && !e.shiftKey && !e.altKey && e.code === "BracketRight") return "forward";
  } else if (e.altKey && !e.ctrlKey && !e.metaKey && !e.shiftKey) {
    if (e.key === "ArrowLeft") return "back";
    if (e.key === "ArrowRight") return "forward";
  }
  return null;
}

function nodeAttribute(node: NodeDescription | null, name: string): string | undefined {
  if (!node?.attributes) return undefined;
  if (Array.isArray(node.attributes)) {
    for (let i = 0; i + 1 < node.attributes.length; i += 2) {
      if (node.attributes[i].toLowerCase() === name) return node.attributes[i + 1];
    }
    return undefined;
  }
  const key = Object.keys(node.attributes).find((candidate) => candidate.toLowerCase() === name);
  return key ? node.attributes[key] : undefined;
}

/** DOM.getBoxModel reports the box as a quad [x,y,x,y,x,y,x,y] in layout CSS
 *  pixels. The inspector only needs the axis-aligned bounds. */
function quadRect(quad: number[] | undefined): BoxRect | null {
  if (!quad || quad.length < 8) return null;
  const xs = [quad[0], quad[2], quad[4], quad[6]];
  const ys = [quad[1], quad[3], quad[5], quad[7]];
  const left = Math.min(...xs);
  const top = Math.min(...ys);
  return { left, top, width: Math.max(1, Math.max(...xs) - left), height: Math.max(1, Math.max(...ys) - top) };
}

export interface ChromeViewportProps {
  stateKey: StateKey;
  browseBase: string | null;
  /** The tab's project host ("" = this machine); keys the socket's grant and stream routing. */
  host: string;
  url: string;
  /** Whether this viewport is the visible, interactive browser surface. The
   * socket remains connected while false so the remote page stays alive, but
   * background surfaces do not paint frames or poll page metrics. */
  active?: boolean;
  /** User-intent navigation counter (the store's historyIndex): bumped only
   *  by navigate/back/forward/reload, never by server-reported nav events.
   *  The viewport navigates on THIS, not on `url` — a page rewriting its own
   *  URL via history.pushState/replaceState (map lat/lng/zoom, ?tab=) also
   *  changes the store url, and replaying that as {t:"nav"} would
   *  Page.navigate → full reload on every zoom/draw. */
  navSeq: number;
}

/** Chrome-mode viewport: renders the CDP screencast on a canvas and forwards
 *  pointer/keyboard input over the per-stateKey socket. The viewport IS the
 *  page — chrome is only the address bar's status row above it. Coordinates
 *  are CSS pixels relative to the canvas rect (Chrome expects CSS px; the
 *  screencast frames are device px and are only used for the backing store). */
export function ChromeViewport({ stateKey, browseBase, host, url, navSeq, active = true }: ChromeViewportProps) {
  const { send, status, error, detached, reconnect, onFrame, onFileChooser, onSelection, onFindResult, getNodeAt, describeNode, getDocument, getBoxModel, pushNodesByBackendIds } = useCdpSocket(stateKey, browseBase, host, true, active);
  const surface = useBrowserStore(stateKey);
  const actions = useBrowserActions();
  const canvasRef = useRef<HTMLCanvasElement | null>(null);
  // Invisible keyboard/IME target. A canvas cannot host an input method
  // editor, so composition (CJK, dead keys, emoji) and paste events only fire
  // for an editable element; every key still goes to the remote page.
  const keyboardRef = useRef<HTMLTextAreaElement | null>(null);
  // A phone soft keyboard can commit the same composed text twice; the remote
  // page would then receive two `insertText` commands for one word. Same guard
  // the terminal uses (see Terminal/terminalInputGuard.ts), scoped here to
  // composition commits only — pasted text is a deliberate action and never
  // deduplicated.
  const inputGuardRef = useRef<ReturnType<typeof createTerminalInputGuard> | null>(null);
  if (!inputGuardRef.current) inputGuardRef.current = createTerminalInputGuard();
  // Hidden native picker answering the page's intercepted <input type=file>.
  const fileInputRef = useRef<HTMLInputElement | null>(null);
  const activeRef = useRef(active);
  activeRef.current = active;
  const [hasFrame, setHasFrame] = useState(false);
  // Start unset so the FIRST mount always navigates (iframe → chrome switches,
  // e.g. the dev-server escape hatch, would otherwise mount a target sitting
  // on the initial URL with no nav command and render blank).
  const lastNavSeqRef = useRef<number | null>(null);
  // Pending pointermove, coalesced to one per animation frame (~16ms).
  const pendingMove = useRef<{ x: number; y: number; mods: number } | null>(null);
  const moveTimer = useRef<ReturnType<typeof setTimeout> | null>(null);
  const lastClickTime = useRef(0);
  const lastClickPos = useRef({ x: 0, y: 0 });
  const clickCount = useRef(0);
  // Button held since the last pointerdown: CDP decides "drag in progress"
  // from the button/buttons fields on *move* events, not from history.
  const held = useRef<{ button: string; buttons: number; x: number; y: number } | null>(null);
  // Physical keys currently down, so a blur mid-press can release them.
  const pressedKeys = useRef(new Map<string, { key: string; code: string }>());
  // Page zoom (1 = 100%) as last sent to the live CDP target. A target outlives
  // the socket, so its actual zoom is unknown on (re)connect; the sync effect
  // below sends the surface value on every open.
  const zoom = useRef(1);
  // Active touch contacts by pointerId; the remote page gets real touch
  // events (native scroll/pinch) instead of synthesized mouse presses.
  const touches = useRef(new Map<number, { id: number; x: number; y: number }>());
  const pendingTouchMove = useRef<Map<number, { id: number; x: number; y: number }> | null>(null);
  // Long-press (touch-and-hold) → context menu: a single contact that hasn't
  // moved past LONG_PRESS_MOVE_TOLERANCE within LONG_PRESS_MS is replayed as
  // a right-click, mirroring the touch-and-hold gesture real mobile Chrome
  // performs (CDP has no native touch-hold-for-context-menu primitive).
  const longPressTimer = useRef<ReturnType<typeof setTimeout> | null>(null);
  const longPressPointerId = useRef<number | null>(null);
  const longPressOrigin = useRef<{ x: number; y: number } | null>(null);
  const longPressScreenOrigin = useRef<{ x: number; y: number } | null>(null);
  // Find-in-page bar (Chrome canvas has no native find UI).
  const [findOpen, setFindOpen] = useState(false);
  const [findQuery, setFindQuery] = useState("");
  const [findCase, setFindCase] = useState(false);
  const [findResult, setFindResult] = useState<{ found: boolean; active: number; total: number } | null>(null);
  const findInputRef = useRef<HTMLInputElement | null>(null);
  const findQueryRef = useRef("");
  findQueryRef.current = findQuery;
  const findCaseRef = useRef(false);
  findCaseRef.current = findCase;
  // Codes consumed by the find bar on keydown (open, next/prev, Enter, Esc).
  // Keyup is matched by code alone so releasing the modifier first (Ctrl/Cmd
  // up before F/G) still swallows the release instead of forwarding a stray
  // keyup the remote page never saw a keydown for.
  const findConsumedRef = useRef(new Set<string>());
  const findDebounceRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const contextRequestRef = useRef(0);
  const [contextMenu, setContextMenu] = useState<{ x: number; y: number; node: NodeDescription | null; loading: boolean; error: string | null } | null>(null);
  // DOM inspector: the outline for the tree row under the pointer, and the
  // annotation composer opened from the context menu.
  const [domHighlight, setDomHighlight] = useState<BoxRect | null>(null);
  const highlightSeq = useRef(0);
  const [annotation, setAnnotation] = useState<{
    x: number;
    y: number;
    nodeId: number;
    label: string;
    selector: string | null;
    busy: boolean;
    error: string | null;
  } | null>(null);
  // Request token for the annotation popover; see submitAnnotation.
  const annotationSeqRef = useRef(0);

  // Navigate whenever the user requests it (address bar, back/forward,
  // reload, chrome hand-off) — i.e. when navSeq moves. The store is the
  // single source of truth for the URL, but a url change alone is not a
  // request: server-reported same-document navigations (pushState /
  // replaceState) also update it and must not be replayed as a Page.navigate.
  useEffect(() => {
    if (url && navSeq !== lastNavSeqRef.current) {
      lastNavSeqRef.current = navSeq;
      setFindOpen(false);
      setFindResult(null);
      send({ t: "findClose" });
      send({ t: "nav", url });
    }
  }, [url, navSeq, send]);

  // Listen for cdp:send events from DevConsole (e.g., getResponseBody).
  // Events are stateKey-scoped: each mounted viewport only forwards commands
  // addressed to its own surface, so one tab's toggle never leaks into
  // another tab's socket.
  useEffect(() => {
    const handler = (e: Event) => {
      const detail = (e as CustomEvent).detail;
      if (!detail) return;
      if (detail.stateKey && detail.stateKey !== stateKey) return;
      // The socket is already per-stateKey; don't leak the routing key onto
      // the wire (clientMsg has no such field).
      const msg = { ...detail };
      delete msg.stateKey;
      send(msg);
    };
    window.addEventListener("cdp:send", handler);
    return () => window.removeEventListener("cdp:send", handler);
  }, [send, stateKey]);

  // Copy bridge: the page's selection arrives after Cmd/Ctrl+C|X; write it to
  // the host clipboard (still inside the keydown's transient activation).
  useEffect(() => {
    return onSelection((text) => {
      if (!text) return;
      navigator.clipboard.writeText(text).catch((err: unknown) => {
        console.error("cdp: copy to host clipboard failed", { stateKey }, err);
      });
    });
  }, [onSelection, stateKey]);

  const doFind = useCallback((query: string, backwards: boolean, caseSensitive: boolean) => {
    if (!query) {
      setFindResult(null);
      send({ t: "findClose" });
      return;
    }
    send({ t: "find", query, backwards, caseSensitive });
  }, [send]);

  const cancelFindDebounce = useCallback(() => {
    if (findDebounceRef.current) {
      clearTimeout(findDebounceRef.current);
      findDebounceRef.current = null;
    }
  }, []);

  // Explicit navigation (Enter/F3/Cmd+G/buttons) supersedes the pending
  // type-debounce so one keypress never produces two find requests.
  const doFindNow = useCallback((query: string, backwards: boolean, caseSensitive: boolean) => {
    cancelFindDebounce();
    doFind(query, backwards, caseSensitive);
  }, [cancelFindDebounce, doFind]);

  const closeFind = useCallback(() => {
    cancelFindDebounce();
    setFindOpen(false);
    setFindResult(null);
    send({ t: "findClose" });
    keyboardRef.current?.focus({ preventScroll: true });
  }, [cancelFindDebounce, send]);

  const openFind = useCallback(() => {
    setFindOpen(true);
  }, []);

  useEffect(() => {
    if (!onFindResult) return;
    return onFindResult((res) => {
      if (typeof res.query === "string" && res.query !== findQueryRef.current) return;
      setFindResult({ found: res.found, active: res.active, total: res.total });
    });
  }, [onFindResult]);

  useEffect(() => {
    if (!findOpen) return;
    if (!findQuery) {
      setFindResult(null);
      send({ t: "findClose" });
      return;
    }
    const timer = setTimeout(() => {
      findDebounceRef.current = null;
      doFind(findQuery, false, findCaseRef.current);
    }, 250);
    findDebounceRef.current = timer;
    return () => {
      clearTimeout(timer);
      if (findDebounceRef.current === timer) findDebounceRef.current = null;
    };
  }, [findQuery, findOpen, send, doFind]);

  useEffect(() => {
    if (findOpen) {
      const t = setTimeout(() => {
        findInputRef.current?.focus();
        findInputRef.current?.select();
      }, 0);
      return () => clearTimeout(t);
    }
  }, [findOpen]);

  // Frames → backing store + paint.
  useEffect(() => {
    return onFrame((bitmap, w, h) => {
      const canvas = canvasRef.current;
      if (!canvas) {
        (bitmap as unknown as { close?: () => void }).close?.();
        return;
      }
      if (!active) {
        (bitmap as unknown as { close?: () => void }).close?.();
        return;
      }
      if (canvas.width !== w) canvas.width = w;
      if (canvas.height !== h) canvas.height = h;
      const ctx = canvas.getContext("2d");
      if (ctx) {
        ctx.drawImage(bitmap as unknown as CanvasImageSource, 0, 0);
      }
      (bitmap as unknown as { close?: () => void }).close?.();
      setHasFrame(true);
    });
  }, [active, onFrame]);

  // Page opened a file chooser → open our own picker. The click that opened
  // it is still a transient user activation (Chrome allows ~5s), which
  // input.click() needs.
  useEffect(() => {
    return onFileChooser((multiple) => {
      if (!activeRef.current) return;
      const input = fileInputRef.current;
      if (!input) return;
      input.multiple = multiple;
      input.value = "";
      input.click();
    });
  }, [onFileChooser]);

  // The input's "cancel" event (picker dismissed) has no React prop in this
  // React version; listen natively.
  useEffect(() => {
    const input = fileInputRef.current;
    if (!input) return;
    const onCancel = () => send({ t: "fileChooserCancel" });
    input.addEventListener("cancel", onCancel);
    return () => input.removeEventListener("cancel", onCancel);
  }, [send]);

  // Chrome-mode scroll save: wheel/pointer input goes straight to the page,
  // so poll the live offset; replies land in the store via useCdpSocket and
  // from there into per-URL persistence. Chrome-mode scroll restore: re-send
  // the persisted offset with bounded retries after (re)mount/navigation —
  // SPA content renders late and early scrollTo calls land short.
  useEffect(() => {
    if (status !== "open" || !active) return;
    const timer = setInterval(() => send({ t: "getScroll" }), 2000);
    return () => clearInterval(timer);
  }, [active, status, send]);

  const restoredChromeScroll = useRef("");
  useEffect(() => {
    const y = surface?.scrollByUrl?.[url] ?? 0;
    if (!active || status !== "open" || !(y > 0)) {
      if (!(y > 0)) restoredChromeScroll.current = "";
      return;
    }
    const stamp = `${url}@${y}`;
    restoredChromeScroll.current = stamp;
    let attempts = 0;
    const timer = setInterval(() => {
      if (restoredChromeScroll.current !== stamp) {
        clearInterval(timer);
        return;
      }
      attempts += 1;
      if (attempts > 4) {
        clearInterval(timer);
        return;
      }
      send({ t: "scrollTo", y });
    }, 750);
    return () => clearInterval(timer);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [active, status, url, stateKey, send]);

  const onFilesPicked = (e: React.ChangeEvent<HTMLInputElement>) => {
    const files = Array.from(e.target.files ?? []);
    if (files.length === 0) {
      send({ t: "fileChooserCancel" });
      return;
    }
    uploadBrowseFiles(stateKey, files).catch((err: unknown) => {
      console.error("cdp: file upload for chooser failed", { stateKey, files: files.map((f) => f.name) }, err);
    });
  };

  // Container resize → Emulation.setDeviceMetricsOverride via the socket.
  useEffect(() => {
    const canvas = canvasRef.current;
    if (!active || !canvas || typeof ResizeObserver === "undefined") return;
    const ro = new ResizeObserver((entries) => {
      const rect = entries[0].contentRect;
      const w = Math.round(rect.width);
      const h = Math.round(rect.height);
      if (w > 0 && h > 0) {
        send({ t: "resize", w, h, dpr: window.devicePixelRatio || 1 });
      }
    });
    ro.observe(canvas);
    return () => ro.disconnect();
  }, [active, send]);

  const canvasPos = useCallback((e: { clientX: number; clientY: number }) => {
    const canvas = canvasRef.current;
    if (!canvas) return { x: 0, y: 0 };
    const rect = canvas.getBoundingClientRect();
    const clientX = Number.isFinite(e.clientX) ? e.clientX : 0;
    const clientY = Number.isFinite(e.clientY) ? e.clientY : 0;
    return { x: clientX - rect.left, y: clientY - rect.top };
  }, []);

  const closeContextMenu = useCallback(() => {
    contextRequestRef.current += 1;
    setContextMenu(null);
  }, []);

  // The page is laid out at viewportWidth / zoom and scaled up by zoom on the
  // canvas, so layout CSS pixels and canvas CSS pixels differ by this factor.
  const zoomFactor = surface?.zoom ?? 1;

  const openContextMenuAt = useCallback((clientX: number, clientY: number) => {
    if (!active) return;
    const { x, y } = canvasPos({ clientX, clientY });
    const z = zoomFactor || 1;
    const request = ++contextRequestRef.current;
    setContextMenu({
      x: Number.isFinite(clientX) ? clientX : 0,
      y: Number.isFinite(clientY) ? clientY : 0,
      node: null,
      loading: true,
      error: null,
    });
    // Canvas px -> layout CSS px, the same conversion the picker uses.
    void getNodeAt(x / z, y / z)
      .then((location) => describeNode(location.backendNodeId).then((node) => ({ ...node, backendNodeId: location.backendNodeId })))
      .then((node) => {
        if (contextRequestRef.current !== request) return;
        setContextMenu((current) => current ? { ...current, node, loading: false, error: null } : current);
      })
      .catch((err: unknown) => {
        if (contextRequestRef.current !== request) return;
        setContextMenu((current) => current ? { ...current, node: null, loading: false, error: err instanceof Error ? err.message : String(err) } : current);
      });
  }, [active, canvasPos, describeNode, getNodeAt, zoomFactor]);

  const onContextMenu = useCallback((e: React.MouseEvent<HTMLCanvasElement>) => {
    e.preventDefault();
    e.stopPropagation();
    openContextMenuAt(e.clientX, e.clientY);
  }, [openContextMenuAt]);

  const pageUrl = surface?.url || url;
  const contextNodeName = contextMenu?.node?.nodeName?.toUpperCase() ?? "";
  const contextIsLinkOrMedia = ["A", "AREA", "IMG", "VIDEO", "SOURCE", "IFRAME"].includes(contextNodeName);
  const contextLink = contextIsLinkOrMedia
    ? nodeAttribute(contextMenu?.node ?? null, contextNodeName === "A" || contextNodeName === "AREA" ? "href" : "src") ?? null
    : null;
  const contextReady = status === "open" && !contextMenu?.loading;

  const fetchedGen = useRef(0);

  const fetchTreeForGen = useCallback(async (gen: number): Promise<DomNode | null> => {
    try {
      const tree = normalizeChromeTree(await getDocument());
      actions.setDomTree(stateKey, gen, { root: tree.root, truncated: tree.truncated });
      return tree.root;
    } catch (err) {
      actions.setDomError(stateKey, gen, err instanceof Error ? err.message : String(err));
      return null;
    }
  }, [actions, getDocument, stateKey]);

  const ensureDomTree = useCallback(async (force = false): Promise<DomNode | null> => {
    const cur = browserStore.state.byKey[stateKey]?.dom;
    if (!force && cur?.status === "ready" && cur.root) return cur.root;
    const gen = actions.beginDomInspect(stateKey, "chrome");
    fetchedGen.current = gen;
    return fetchTreeForGen(gen);
  }, [actions, fetchTreeForGen, stateKey]);

  /** Map a stable backendNodeId to the nodeId of the current tree, returned
   *  with that tree. nodeIds are only valid for the document fetch they came
   *  from, so a node missing from the cached tree (a modal opened after the
   *  fetch) gets one forced refetch and one more mapping. Null when the node is
   *  still absent. */
  const resolveBackendNode = useCallback(async (backendNodeId: number): Promise<{ root: DomNode; id: string } | null> => {
    const mapIn = async (root: DomNode): Promise<{ root: DomNode; id: string } | null> => {
      const pushed = await pushNodesByBackendIds([backendNodeId]);
      if (!Array.isArray(pushed.nodeIds)) throw new Error("push_nodes_failed");
      const nodeId = pushed.nodeIds[0];
      // 0 means Chrome no longer has the node in the document.
      if (!nodeId) return null;
      const id = String(nodeId);
      return findNode(root, id) ? { root, id } : null;
    };
    const root = await ensureDomTree();
    if (!root) return null;
    const found = await mapIn(root);
    if (found) return found;
    const fresh = await ensureDomTree(true);
    return fresh ? mapIn(fresh) : null;
  }, [ensureDomTree, pushNodesByBackendIds]);

  // A refresh started outside this component (DevConsole's Refresh button lives
  // in BrowserPanel) bumps the store generation and leaves the surface loading;
  // this is the only component holding the socket, so it performs the fetch.
  // `fetchedGen` keeps our own inspect/annotation fetch from being duplicated.
  const domGen = surface?.dom?.gen ?? 0;
  const domLoading = surface?.dom?.status === "loading" && surface?.dom?.mode === "chrome";
  useEffect(() => {
    if (!domLoading || !domGen || fetchedGen.current === domGen) return;
    fetchedGen.current = domGen;
    void fetchTreeForGen(domGen);
  }, [domGen, domLoading, fetchTreeForGen]);


  /** Fetch the tree once per page, select `nodeId`, and hand the tree back so
   *  the caller can derive a selector. A failed fetch selects nothing. */
  const inspectNode = useCallback((backendNodeId: number) => {
    void resolveBackendNode(backendNodeId).then((found) => {
      if (found) actions.selectDomNode(stateKey, found.id);
    }).catch((err: unknown) => {
      actions.pushConsole(stateKey, {
        level: "warn",
        text: `Inspect failed: ${err instanceof Error ? err.message : String(err)}`,
        ts: Date.now(),
      });
    });
  }, [actions, resolveBackendNode, stateKey]);

  const openAnnotation = useCallback((backendNodeId: number, label: string, x: number, y: number) => {
    // A new popover supersedes an in-flight submit: its late capture must not dispatch.
    annotationSeqRef.current++;
    void resolveBackendNode(backendNodeId).then((found) => {
      // A node that is not in the tree opens the annotation without a selector;
      // its screenshot step then reports the missing node in the console.
      const nodeId = found ? Number(found.id) : 0;
      const selector = found ? selectorFor(found.root, found.id) : null;
      if (found) actions.selectDomNode(stateKey, found.id);
      setAnnotation({ x, y, nodeId, label, selector, busy: false, error: null });
    }).catch((err: unknown) => {
      actions.pushConsole(stateKey, {
        level: "warn",
        text: `Annotation failed: ${err instanceof Error ? err.message : String(err)}`,
        ts: Date.now(),
      });
    });
  }, [actions, resolveBackendNode, stateKey]);

  const submitAnnotation = useCallback(async (note: string, includeShot: boolean) => {
    if (!annotation) return;
    const seq = ++annotationSeqRef.current;
    const { nodeId, selector, label } = annotation;
    setAnnotation((cur) => (cur ? { ...cur, busy: true, error: null } : cur));
    let image: Blob | null = null;
    if (includeShot) {
      const canvas = canvasRef.current;
      try {
        if (!canvas) throw new Error("the page canvas is unavailable");
        const box = await getBoxModel(nodeId);
        const rect = quadRect(box?.model?.content);
        if (!rect) throw new Error("the element has no layout box");
        const z = zoomFactor || 1;
        image = await cropScreencast(canvas, {
          left: rect.left * z,
          top: rect.top * z,
          width: rect.width * z,
          height: rect.height * z,
        });
      } catch (err) {
        // The note is still useful without a picture; say so in the console
        // instead of blocking the annotation. A superseded request says nothing.
        if (seq === annotationSeqRef.current) {
          actions.pushConsole(stateKey, {
            level: "warn",
            text: `Annotation screenshot failed: ${err instanceof Error ? err.message : String(err)}`,
            ts: Date.now(),
          });
        }
      }
      if (seq !== annotationSeqRef.current) return;
    }
    const text = composeAnnotationText({ title: surface?.pageTitle ?? "", url: pageUrl, selector, label, note });
    const accepted = dispatchBrowserAnnotation({ stateKey, text, image, imageName: annotationFileName() });
    if (accepted) {
      setAnnotation(null);
      return;
    }
    // Nothing received the note: keep the popover (and the typed text) open.
    setAnnotation((cur) => (cur && seq === annotationSeqRef.current ? { ...cur, busy: false, error: ANNOTATION_NO_CHAT_ERROR } : cur));
  }, [actions, annotation, getBoxModel, pageUrl, stateKey, surface?.pageTitle, zoomFactor]);

  const hoverId = surface?.dom?.hoverId ?? null;
  useEffect(() => {
    if (!hoverId || !/^\d+$/.test(hoverId)) {
      setDomHighlight(null);
      return;
    }
    const seq = ++highlightSeq.current;
    const z = zoomFactor || 1;
    getBoxModel(Number(hoverId)).then((box) => {
      if (highlightSeq.current !== seq) return;
      const rect = quadRect(box?.model?.content);
      setDomHighlight(rect
        ? { left: rect.left * z, top: rect.top * z, width: rect.width * z, height: rect.height * z }
        : null);
    }).catch(() => {
      if (highlightSeq.current === seq) setDomHighlight(null);
    });
    return () => {
      highlightSeq.current++;
    };
  }, [getBoxModel, hoverId, zoomFactor]);

  const pickOn = surface?.dom?.pick === true;
  useEffect(() => {
    if (!pickOn) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key !== "Escape") return;
      e.preventDefault();
      actions.setDomPick(stateKey, false);
    };
    window.addEventListener("keydown", onKey, true);
    return () => window.removeEventListener("keydown", onKey, true);
  }, [actions, pickOn, stateKey]);


  const copyContextText = async (text: string): Promise<boolean> => {
    if (!navigator.clipboard) {
      setContextMenu((current) => current ? { ...current, error: "clipboard unavailable" } : current);
      return false;
    }
    try {
      await navigator.clipboard.writeText(text);
      return true;
    } catch (err: unknown) {
      actions.pushConsole(stateKey, { level: "error", text: `Clipboard: ${err instanceof Error ? err.message : String(err)}`, ts: Date.now() });
      setContextMenu((current) => current ? { ...current, error: "clipboard write failed" } : current);
      return false;
    }
  };

  const contextItems: ContextMenuItem[] = [
    {
      label: "Back",
      disabled: status !== "open" || !surface || surface.historyIndex <= 0,
      onClick: () => actions.back(stateKey),
    },
    {
      label: "Forward",
      disabled: status !== "open" || !surface || surface.historyIndex >= surface.history.length - 1,
      onClick: () => actions.forward(stateKey),
    },
    {
      label: "Reload",
      disabled: status !== "open",
      onClick: () => send({ t: "reload" }),
    },
    ...(contextMenu?.error
      ? [{
          label: `Context lookup unavailable: ${contextMenu.error}`,
          disabled: true,
          onClick: () => undefined,
        }]
      : []),
    { label: "", separator: true, onClick: () => undefined },
    {
      label: "Copy URL",
      disabled: status !== "open" || !pageUrl,
      onClick: () => copyContextText(pageUrl),
    },
    {
      label: "Copy Link or Image",
      disabled: !contextReady || !contextLink,
      onClick: () => contextLink && copyContextText(contextLink),
    },
    {
      label: "Inspect element",
      disabled: !contextReady || contextMenu?.node?.backendNodeId === undefined,
      onClick: () => {
        const backendNodeId = contextMenu?.node?.backendNodeId;
        if (backendNodeId === undefined) return;
        inspectNode(backendNodeId);
      },
    },
    {
      label: "Add annotation…",
      disabled: !contextReady || contextMenu?.node?.backendNodeId === undefined,
      onClick: () => {
        const node = contextMenu?.node;
        const at = contextMenu;
        if (!node || !at || node.backendNodeId === undefined) return;
        closeContextMenu();
        openAnnotation(node.backendNodeId, contextNodeName.toLowerCase() || "element", at.x, at.y);
      },
    },
    { label: "", separator: true, onClick: () => undefined },
    {
      label: "Open External",
      disabled: status !== "open" || !pageUrl,
      onClick: () => {
        openExternalURL(pageUrl);
      },
    },
  ];

  const buttonName = (b: number): string =>
    b === 2 ? "right" : b === 1 ? "middle" : "left";

  const flushMove = () => {
    if (moveTimer.current) {
      clearTimeout(moveTimer.current);
      moveTimer.current = null;
    }
    const p = pendingMove.current;
    pendingMove.current = null;
    if (p) sendMove(p.x, p.y, p.mods);
  };

  const sendMove = (x: number, y: number, mods: number) => {
    const h = held.current;
    if (h) {
      h.x = x;
      h.y = y;
    }
    send({
      t: "mouse", kind: "move", x, y,
      button: h ? h.button : "none",
      buttons: h ? h.buttons : 0,
      clickCount: 0,
      modifiers: mods,
    });
  };

  const setZoom = (factor: number) => {
    const clamped = Math.min(5, Math.max(0.25, factor));
    if (Math.abs(clamped - zoom.current) < 0.001) return;
    zoom.current = clamped;
    send({ t: "zoom", factor: clamped });
    actions.setZoom(stateKey, clamped);
  };

  // Reapply the surface's persisted zoom to the live target. A target outlives
  // the socket (the manager reuses it for a stateKey), so a reconnect can find
  // the previous viewer's zoom: send ours on EVERY open, 100% included. Later
  // changes flow through surface.zoom; self-triggered ones already match
  // zoom.current, so this does not loop.
  const surfaceZoom = surface?.zoom ?? 1;
  useEffect(() => {
    if (status !== "open") return;
    zoom.current = surfaceZoom;
    send({ t: "zoom", factor: surfaceZoom });
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [status, surfaceZoom]);

  const touchPoints = (ids: Iterable<number>) => {
    const out: { id: number; x: number; y: number }[] = [];
    for (const id of ids) {
      const p = touches.current.get(id);
      if (p) out.push({ ...p });
    }
    return out;
  };

  const LONG_PRESS_MS = 500;
  const LONG_PRESS_MOVE_TOLERANCE = 10; // CSS px before a hold becomes a drag/scroll

  const clearLongPress = () => {
    if (longPressTimer.current) {
      clearTimeout(longPressTimer.current);
      longPressTimer.current = null;
    }
    longPressPointerId.current = null;
    longPressOrigin.current = null;
    longPressScreenOrigin.current = null;
  };

  const onTouchDown = (e: React.PointerEvent<HTMLCanvasElement>) => {
    const { x, y } = canvasPos(e);
    touches.current.set(e.pointerId, { id: e.pointerId, x, y });
    send({ t: "touch", kind: "start", points: touchPoints([e.pointerId]), modifiers: modifiersOf(e) });

    if (touches.current.size > 1) {
      // A second contact joined (pinch/multi-touch): no longer a candidate
      // for a single-finger long-press.
      clearLongPress();
      return;
    }
    const pointerId = e.pointerId;
    const mods = modifiersOf(e);
    longPressPointerId.current = pointerId;
    longPressOrigin.current = { x, y };
    longPressScreenOrigin.current = { x: e.clientX, y: e.clientY };
    longPressTimer.current = setTimeout(() => {
      longPressTimer.current = null;
      const t = touches.current.get(pointerId);
      if (!t) return; // already lifted or cancelled
      // Replay as a right-click at the hold point instead of a touch end, so
      // the remote page sees the same native contextmenu a real touch-hold
      // produces rather than a stray tap/scroll.
      send({ t: "touch", kind: "cancel", points: [{ ...t }], modifiers: mods });
      touches.current.delete(pointerId);
      send({ t: "mouse", kind: "down", x: t.x, y: t.y, button: "right", buttons: 2, clickCount: 1, modifiers: mods });
      send({ t: "mouse", kind: "up", x: t.x, y: t.y, button: "right", buttons: 0, clickCount: 1, modifiers: mods });
      const screen = longPressScreenOrigin.current;
      if (screen) openContextMenuAt(screen.x, screen.y);
    }, LONG_PRESS_MS);
  };

  const onTouchMove = (e: React.PointerEvent<HTMLCanvasElement>) => {
    const t = touches.current.get(e.pointerId);
    if (!t) return;
    const { x, y } = canvasPos(e);
    if (longPressPointerId.current === e.pointerId) {
      const origin = longPressOrigin.current;
      if (origin && (Math.abs(x - origin.x) > LONG_PRESS_MOVE_TOLERANCE || Math.abs(y - origin.y) > LONG_PRESS_MOVE_TOLERANCE)) {
        clearLongPress(); // moved too far: this is a drag/scroll, not a hold
      }
    }
    t.x = x;
    t.y = y;
    if (!pendingTouchMove.current) pendingTouchMove.current = new Map();
    pendingTouchMove.current.set(e.pointerId, t);
    if (moveTimer.current) return; // share the per-frame coalescer with mouse moves
    moveTimer.current = setTimeout(() => {
      moveTimer.current = null;
      flushTouchMove(modifiersOf(e));
    }, 16);
  };

  const flushTouchMove = (mods: number) => {
    const pending = pendingTouchMove.current;
    pendingTouchMove.current = null;
    if (pending && pending.size > 0) {
      send({ t: "touch", kind: "move", points: touchPoints(pending.keys()), modifiers: mods });
    }
  };

  const onTouchUp = (e: React.PointerEvent<HTMLCanvasElement>, kind: "end" | "cancel") => {
    if (longPressPointerId.current === e.pointerId) clearLongPress();
    const t = touches.current.get(e.pointerId);
    if (!t) return;
    if (moveTimer.current) {
      clearTimeout(moveTimer.current);
      moveTimer.current = null;
    }
    flushTouchMove(modifiersOf(e));
    const { x, y } = canvasPos(e);
    t.x = x;
    t.y = y;
    send({ t: "touch", kind, points: [{ ...t }], modifiers: modifiersOf(e) });
    touches.current.delete(e.pointerId);
  };

  const onPointerDown = (e: React.PointerEvent<HTMLCanvasElement>) => {
    e.preventDefault();
    // Element picker: this click chooses an element instead of reaching the
    // page, so the page's own handlers must not see it.
    if (pickOn && e.button === 0) {
      const { x, y } = canvasPos(e);
      const z = zoomFactor || 1;
      void getNodeAt(x / z, y / z)
        .then((loc) => resolveBackendNode(loc.backendNodeId))
        .then((found) => {
          if (found) actions.selectDomNode(stateKey, found.id);
        })
        .catch(() => actions.setDomPick(stateKey, false));
      return;
    }
    keyboardRef.current?.focus({ preventScroll: true });
    if (e.pointerType === "touch") {
      onTouchDown(e);
      return;
    }
    // Keep receiving move/up after the cursor leaves the canvas mid-drag.
    e.currentTarget.setPointerCapture(e.pointerId);
    const { x, y } = canvasPos(e);
    // Double/triple-click: within 500ms and 5px of the previous click.
    const now = Date.now();
    const near = Math.abs(x - lastClickPos.current.x) <= 5 && Math.abs(y - lastClickPos.current.y) <= 5;
    clickCount.current = now - lastClickTime.current < 500 && near ? Math.min(clickCount.current + 1, 3) : 1;
    lastClickTime.current = now;
    lastClickPos.current = { x, y };
    const button = buttonName(e.button);
    const buttons = buttonBit(e.button);
    held.current = { button, buttons, x, y };
    send({
      t: "mouse", kind: "down", x, y,
      button,
      buttons,
      clickCount: clickCount.current,
      modifiers: modifiersOf(e),
    });
  };

  const releaseHeld = (x: number, y: number, mods: number) => {
    const h = held.current;
    if (!h) return;
    held.current = null;
    send({
      t: "mouse", kind: "up", x, y,
      button: h.button,
      buttons: 0,
      clickCount: clickCount.current,
      modifiers: mods,
    });
  };

  const onPointerUp = (e: React.PointerEvent<HTMLCanvasElement>) => {
    e.preventDefault();
    if (e.pointerType === "touch") {
      onTouchUp(e, "end");
      return;
    }
    flushMove();
    if (e.currentTarget.hasPointerCapture(e.pointerId)) e.currentTarget.releasePointerCapture(e.pointerId);
    const { x, y } = canvasPos(e);
    releaseHeld(x, y, modifiersOf(e));
  };

  // Host cancelled the pointer stream (touch cancel, window drag, OS gesture):
  // the remote page must not be left with a stuck-down button.
  const onPointerCancel = (e: React.PointerEvent<HTMLCanvasElement>) => {
    if (e.pointerType === "touch") {
      onTouchUp(e, "cancel");
      return;
    }
    flushMove();
    const h = held.current;
    if (h) releaseHeld(h.x, h.y, modifiersOf(e));
  };

  const onPointerMove = (e: React.PointerEvent<HTMLCanvasElement>) => {
    // Picking must not move the remote page's cursor around under the pointer.
    if (pickOn) return;
    if (e.pointerType === "touch") {
      onTouchMove(e);
      return;
    }
    const { x, y } = canvasPos(e);
    pendingMove.current = { x, y, mods: modifiersOf(e) };
    if (moveTimer.current) return; // coalesce: one move per frame
    moveTimer.current = setTimeout(() => {
      moveTimer.current = null;
      const p = pendingMove.current;
      pendingMove.current = null;
      if (p) sendMove(p.x, p.y, p.mods);
    }, 16);
  };

  const onWheel = (e: React.WheelEvent<HTMLCanvasElement>) => {
    e.preventDefault();
    // Trackpad pinch arrives as ctrl+wheel: zoom continuously around the
    // current factor rather than scrolling.
    if (e.ctrlKey) {
      setZoom(zoom.current * Math.exp(-e.deltaY * 0.01));
      return;
    }
    const { x, y } = canvasPos(e);
    send({
      t: "mouse", kind: "wheel", x, y,
      deltaX: e.deltaX, deltaY: e.deltaY,
      modifiers: modifiersOf(e),
    });
  };

  const isFindOpenKey = (ev: { ctrlKey: boolean; metaKey: boolean; shiftKey: boolean; altKey: boolean; code: string }) => {
    // Accept either Cmd or Ctrl on all platforms (jsdom tests report a
    // non-Mac platform, and keyboards vary) — never forwarded to the page.
    return (ev.ctrlKey || ev.metaKey) && !ev.shiftKey && !ev.altKey && ev.code === "KeyF";
  };
  const isFindNextKey = (ev: { key: string; code: string; ctrlKey: boolean; metaKey: boolean; shiftKey: boolean; altKey: boolean }) => {
    if (ev.key === "F3" && !ev.ctrlKey && !ev.metaKey && !ev.altKey) return true;
    return (ev.ctrlKey || ev.metaKey) && !ev.altKey && ev.code === "KeyG";
  };

  const onKeyDown = (e: React.KeyboardEvent<HTMLTextAreaElement>) => {
    // IME in progress (or a dead key starting one): let the host compose;
    // the result arrives via onCompositionEnd as one insertText.
    if (e.nativeEvent.isComposing || e.key === "Process" || e.key === "Dead") return;
    const primary = isMac() ? e.metaKey : e.ctrlKey;
    // Find-in-page: the canvas has no native find UI, so consume Cmd/Ctrl+F
    // here (both down and up) and never forward it to the remote page.
    if (isFindOpenKey(e)) {
      e.preventDefault();
      findConsumedRef.current.add(e.code);
      openFind();
      return;
    }
    if (findOpen) {
      if (e.key === "Escape") {
        e.preventDefault();
        findConsumedRef.current.add(e.code);
        closeFind();
        return;
      }
      if (isFindNextKey(e)) {
        e.preventDefault();
        findConsumedRef.current.add(e.code);
        if (findQuery) doFindNow(findQuery, e.shiftKey, findCaseRef.current);
        return;
      }
      if (e.key === "Enter" && !e.altKey && !primary) {
        e.preventDefault();
        findConsumedRef.current.add(e.code);
        if (findQuery) doFindNow(findQuery, e.shiftKey, findCaseRef.current);
        return;
      }
    } else if (isFindNextKey(e) && findQuery) {
      e.preventDefault();
      findConsumedRef.current.add(e.code);
      setFindOpen(true);
      doFindNow(findQuery, e.shiftKey, findCaseRef.current);
      return;
    }
    // Paste: leave the keydown alone so the host fires a "paste" event on the
    // textarea, which carries the host clipboard without a permission prompt.
    if (primary && !e.shiftKey && !e.altKey && e.code === "KeyV") return;
    e.preventDefault();
    const nav = chromeShortcut(e);
    if (nav) {
      send({ t: nav });
      return;
    }
    const z = zoomShortcut(e);
    if (z !== null) {
      setZoom(z === 0 ? 1 : zoomStep(zoom.current, z));
      return;
    }
    const mods = modifiersOf(e);
    // Copy/cut: ask for the selection BEFORE the key lands so cut still
    // reports the text it is about to remove (socket messages are ordered).
    if (primary && !e.shiftKey && !e.altKey && (e.code === "KeyC" || e.code === "KeyX")) {
      send({ t: "getSelection" });
    }
    pressedKeys.current.set(e.code, { key: e.key, code: e.code });
    // A keystroke ends any pending IME commit, so an identical word composed
    // after it is new (see terminalInputGuard.ts).
    inputGuardRef.current!.noteInput();
    send({
      t: "key", kind: "down", key: e.key, code: e.code, text: keyText(e), modifiers: mods,
      ...(e.repeat ? { autoRepeat: true } : {}),
    });
  };

  const onKeyUp = (e: React.KeyboardEvent<HTMLTextAreaElement>) => {
    if (e.nativeEvent.isComposing || e.key === "Process" || e.key === "Dead") return;
    // Swallow releases for chords consumed on keydown, matched by code alone:
    // the user may release the modifier (Ctrl/Cmd) before the key itself, and
    // Enter navigation must not leak a stray keyup to the remote page either.
    // Never added to pressedKeys.
    if (findConsumedRef.current.has(e.code)) {
      findConsumedRef.current.delete(e.code);
      e.preventDefault();
      return;
    }
    if (isFindOpenKey(e)) {
      e.preventDefault();
      return;
    }
    if (findOpen && (e.key === "Escape" || e.key === "Enter" || isFindNextKey(e))) {
      e.preventDefault();
      return;
    }
    e.preventDefault();
    pressedKeys.current.delete(e.code);
    send({ t: "key", kind: "up", key: e.key, code: e.code, text: keyText(e), modifiers: modifiersOf(e) });
  };

  const findCountLabel = (): string => {
    if (!findQuery) return "";
    if (!findResult) return "...";
    if (!findResult.found || findResult.total <= 0) return "No results";
    if (findResult.total > 0 && findResult.active > 0) return `${findResult.active} of ${findResult.total}`;
    return `${findResult.total} matches`;
  };

  const onFindInputKeyDown = (e: React.KeyboardEvent<HTMLInputElement>) => {
    if (e.key === "Escape") {
      e.preventDefault();
      e.stopPropagation();
      closeFind();
      return;
    }
    if (e.key === "Enter") {
      e.preventDefault();
      e.stopPropagation();
      if (findQuery) doFindNow(findQuery, e.shiftKey, findCase);
      return;
    }
    if (e.key === "F3") {
      e.preventDefault();
      e.stopPropagation();
      if (findQuery) doFindNow(findQuery, e.shiftKey, findCase);
      return;
    }
    const prim = e.ctrlKey || e.metaKey;
    if (prim && !e.altKey && e.code === "KeyG") {
      e.preventDefault();
      e.stopPropagation();
      if (findQuery) doFindNow(findQuery, e.shiftKey, findCase);
      return;
    }
    if (prim && !e.shiftKey && !e.altKey && e.code === "KeyF") {
      e.preventDefault();
      e.stopPropagation();
      findInputRef.current?.select();
      return;
    }
    e.stopPropagation();
  };

  const onCompositionEnd = (e: React.CompositionEvent<HTMLTextAreaElement>) => {
    if (e.data) {
      const guard = inputGuardRef.current!;
      guard.noteComposition(e.data);
      // One soft-keyboard commit, one insertText: a re-delivered commit of the
      // same text is dropped (the terminal has the same guard on term.onData).
      if (guard.accept(e.data)) send({ t: "insertText", text: e.data });
    }
    e.currentTarget.value = "";
  };

  const onPaste = (e: React.ClipboardEvent<HTMLTextAreaElement>) => {
    e.preventDefault();
    const text = e.clipboardData.getData("text/plain");
    if (text) {
      inputGuardRef.current!.noteInput();
      send({ t: "insertText", text });
    }
  };

  // Focus left the viewport (tab switch, alt-tab, click elsewhere): release
  // whatever is still held so the remote page never sees a stuck key/button.
  const onBlur = () => {
    findConsumedRef.current.clear();
    cancelFindDebounce();
    clearLongPress();
    for (const k of pressedKeys.current.values()) {
      send({ t: "key", kind: "up", key: k.key, code: k.code, text: "", modifiers: 0 });
    }
    pressedKeys.current.clear();
    flushMove();
    const h = held.current;
    if (h) releaseHeld(h.x, h.y, 0);
    for (const t of touches.current.values()) {
      send({ t: "touch", kind: "cancel", points: [{ ...t }], modifiers: 0 });
    }
    touches.current.clear();
  };

  return (
    <div className="absolute inset-0">
      <canvas
        ref={canvasRef}
        className={`block w-full h-full outline-none touch-none${pickOn ? " cursor-crosshair" : ""}`}
        onPointerDown={onPointerDown}
        onPointerUp={onPointerUp}
        onPointerCancel={onPointerCancel}
        onPointerMove={onPointerMove}
        onWheel={onWheel}
        onContextMenu={onContextMenu}
      />
      {domHighlight && (
        <div
          data-testid="cdp-dom-highlight"
          aria-hidden="true"
          className="pointer-events-none absolute border-2 border-sky-500 bg-sky-500/10"
          style={{ left: domHighlight.left, top: domHighlight.top, width: domHighlight.width, height: domHighlight.height }}
        />
      )}
      <ContextMenu
        items={contextItems}
        open={contextMenu !== null}
        position={contextMenu ?? { x: 0, y: 0 }}
        onClose={closeContextMenu}
      />
      {annotation && (
        <AnnotationPopover
          x={annotation.x}
          y={annotation.y}
          label={annotation.selector ?? annotation.label}
          canScreenshot={status === "open"}
          busy={annotation.busy}
          error={annotation.error}
          onSubmit={(note, includeShot) => void submitAnnotation(note, includeShot)}
          onCancel={() => {
            // Cancel supersedes an in-flight submit so its late capture cannot inject the note.
            annotationSeqRef.current++;
            setAnnotation(null);
          }}
        />
      )}
      {detached && (
        <div
          data-testid="cdp-detached"
          className="absolute inset-0 z-10 flex flex-col items-center justify-center gap-2 bg-background/90 p-4 text-center"
        >
          <p className="text-xs text-muted-foreground">
            Another window or device is viewing this browser tab.
          </p>
          <button
            type="button"
            onClick={reconnect}
            className="rounded border border-border bg-background px-2.5 py-1 text-xs text-foreground hover:bg-muted"
          >
            Take over
          </button>
        </div>
      )}
      <textarea
        ref={keyboardRef}
        data-testid="cdp-keyboard"
        aria-hidden="true"
        tabIndex={0}
        autoComplete="off"
        autoCapitalize="off"
        autoCorrect="off"
        spellCheck={false}
        className="absolute top-0 left-0 w-px h-px opacity-0 resize-none overflow-hidden border-0 p-0 outline-none"
        onKeyDown={onKeyDown}
        onKeyUp={onKeyUp}
        onCompositionEnd={onCompositionEnd}
        onPaste={onPaste}
        onBlur={onBlur}
      />
      <input
        ref={fileInputRef}
        type="file"
        data-testid="cdp-file-input"
        className="hidden"
        onChange={onFilesPicked}
      />
      {!hasFrame && status !== "closed" && (
        <div className="absolute inset-0 flex items-center justify-center pointer-events-none" data-testid="cdp-spinner">
          <LoadingSpinner className="w-6 h-6" />
        </div>
      )}
      {!findOpen && (
        <button
          data-testid="cdp-find-open"
          title="Find in page (Cmd/Ctrl+F)"
          aria-label="Find in page"
          className="absolute top-1 right-1 px-1.5 py-0.5 text-xs rounded border border-neutral-300 dark:border-neutral-700 bg-white/90 dark:bg-neutral-900/90 hover:bg-neutral-100 dark:hover:bg-neutral-800"
          onClick={(e) => {
            e.preventDefault();
            openFind();
          }}
        >
          {"\u2315"}
        </button>
      )}
      {findOpen && (
        <div
          data-testid="cdp-find-bar"
          className="absolute top-1 right-1 flex items-center gap-1 px-2 py-1 text-xs rounded border border-neutral-300 dark:border-neutral-700 bg-white/95 dark:bg-neutral-900/95 shadow"
          onMouseDown={(e) => e.stopPropagation()}
        >
          <input
            ref={findInputRef}
            data-testid="cdp-find-input"
            value={findQuery}
            placeholder="Find in page"
            aria-label="Find in page"
            className="w-40 px-1.5 py-0.5 text-xs rounded border border-neutral-300 dark:border-neutral-700 bg-white dark:bg-neutral-900 outline-none"
            onChange={(e) => setFindQuery(e.target.value)}
            onKeyDown={onFindInputKeyDown}
          />
          <span data-testid="cdp-find-count" className="min-w-14 text-center tabular-nums text-neutral-500 dark:text-neutral-400">
            {findCountLabel()}
          </span>
          <button
            data-testid="cdp-find-prev"
            title="Previous (Shift+Enter)"
            aria-label="Previous match"
            className="px-1.5 py-0.5 rounded hover:bg-neutral-100 dark:hover:bg-neutral-800 disabled:opacity-40"
            disabled={!findQuery}
            onClick={(e) => {
              e.preventDefault();
              if (findQuery) doFindNow(findQuery, true, findCase);
            }}
          >
            {"\u25B2"}
          </button>
          <button
            data-testid="cdp-find-next"
            title="Next (Enter)"
            aria-label="Next match"
            className="px-1.5 py-0.5 rounded hover:bg-neutral-100 dark:hover:bg-neutral-800 disabled:opacity-40"
            disabled={!findQuery}
            onClick={(e) => {
              e.preventDefault();
              if (findQuery) doFindNow(findQuery, false, findCase);
            }}
          >
            {"\u25BC"}
          </button>
          <button
            data-testid="cdp-find-case"
            title="Match case"
            aria-label="Match case"
            aria-pressed={findCase}
            className={findCase ? "px-1.5 py-0.5 rounded bg-blue-500/20 text-blue-600 dark:text-blue-400" : "px-1.5 py-0.5 rounded hover:bg-neutral-100 dark:hover:bg-neutral-800 text-neutral-500"}
            onClick={(e) => {
              e.preventDefault();
              const next = !findCase;
              setFindCase(next);
              if (findQuery) doFindNow(findQuery, false, next);
            }}
          >
            Aa
          </button>
          <button
            data-testid="cdp-find-close"
            title="Close (Esc)"
            aria-label="Close find"
            className="px-1.5 py-0.5 rounded hover:bg-neutral-100 dark:hover:bg-neutral-800"
            onClick={(e) => {
              e.preventDefault();
              closeFind();
            }}
          >
            {"\u2715"}
          </button>
        </div>
      )}
      {status === "reconnecting" && (
        <span
          data-testid="cdp-reconnecting"
          className="absolute top-1 right-1 px-1.5 py-0.5 text-xs rounded bg-amber-500/20 text-amber-600 dark:text-amber-400"
        >
          reconnecting…
        </span>
      )}
      {status === "closed" && error && (
        <div className="absolute inset-0 flex flex-col items-center justify-center gap-3 pointer-events-auto">
          <div className="text-sm text-red-500 max-w-md text-center">{error}</div>
          <button
            data-testid="cdp-open-external"
            className="px-2 py-1 text-xs rounded border border-neutral-300 dark:border-neutral-700 hover:bg-neutral-100 dark:hover:bg-neutral-800"
            onClick={() => openExternalURL(url)}
          >
            Open externally ↗
          </button>
        </div>
      )}
    </div>
  );
}
