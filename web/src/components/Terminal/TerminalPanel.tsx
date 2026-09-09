import { useCallback, useEffect, useRef, useState } from "react";
import { createPortal } from "react-dom";
import {
  Copy,
  ClipboardPaste,
  CopyPlus,
  Trash2,
  RotateCcw,
  ArrowUpToLine,
  ArrowDownToLine,
  Search,
  Plus,
  X,
  Volume2,
} from "lucide-react";
import { Terminal } from "@xterm/xterm";
import { FitAddon } from "@xterm/addon-fit";
import { SearchAddon } from "@xterm/addon-search";
import { SerializeAddon } from "@xterm/addon-serialize";
import { WebglAddon } from "@xterm/addon-webgl";
import { WebLinksAddon } from "@xterm/addon-web-links";
import "@xterm/xterm/css/xterm.css";
import { registerFileLinkProvider } from "./terminalLinkProvider";
import TerminalFindBar from "./TerminalFindBar";
import { restoreTerminalHistory, TerminalHistoryError } from "./terminalHistory";
import { apiPath, apiWsPath, authHeaders, authToken, isRemoteSession } from "@/api/client";
import { loadTerminalBuffer, saveTerminalBuffer } from "./terminalPersistence";
import { registerTerminal, unregisterTerminal } from "@/lib/debug/terminalRegistry";
import { playAlertSound } from "./terminalAlertSound";
import { useTerminalState } from "../../stores/terminalStore";
import { registerTerminalFocus, unregisterTerminalFocus } from "./terminalFocus";
import { requestSpeech } from "../Speech/SpeechProvider";
import { sanitizeSpeechText } from "../Speech/speechUtils";

/**
 * Builds the URL and WebSocket subprotocols for /api/terminal/ws. In remote
 * mode the browser can't set an Authorization header and query-string tokens
 * are forbidden (leak vector), so the bearer token travels as a
 * Sec-WebSocket-Protocol entry (`ocode.bearer.<token>`) instead — matching
 * the server-side checkAuth handling added for HandleTerminalWS. In
 * non-remote mode the token stays in the `?token=` query string, unchanged.
 * `project_path` and `terminal_id` are never secrets, so they always go in
 * the query string regardless of mode.
 *
 * Pure function of its inputs (no calls to authToken()/isRemoteSession()
 * internally) so it's trivially unit-testable.
 */
export function buildTerminalWsConnection(opts: {
  token: string;
  projectPath: string | undefined;
  /** Remote project host (`[user@]host` or `wsl:<distro>`); undefined for local. */
  host?: string;
  terminalId: string;
  isRemote: boolean;
  historyOffset?: number;
}): { url: string; protocols: string[] | undefined } {
  const { token, projectPath, host, terminalId, isRemote, historyOffset } = opts;
  const params = new URLSearchParams();
  let protocols: string[] | undefined;
  if (token) {
    if (isRemote) {
      protocols = [`ocode.bearer.${token}`];
    } else {
      params.set("token", token);
    }
  }
  // project_path pins the shell's cwd to this tab's project; the server
  // validates it against its registered project roots. terminal_id lets the
  // terminal-processes emitter (Processes tab) correlate a pid with this tab.
  if (projectPath) params.set("project_path", projectPath);
  // host marks an ocode Remote project: the server then pty-starts
  // ssh/wsl.exe into host:project_path instead of a local shell.
  if (host) params.set("host", host);
  params.set("terminal_id", terminalId);
  if (historyOffset !== undefined) params.set("history_offset", String(historyOffset));
  const query = params.toString();
  // apiWsPath keeps the tailscale --set-path prefix and respects the
  // configured backend origin (same-origin vs hub). Handles ws/wss
  // conversion for absolute backend URLs.
  const url = apiWsPath(`/api/terminal/ws${query ? `?${query}` : ""}`);
  return { url, protocols };
}

/**
 * A single interactive terminal: one xterm.js instance bridged to one
 * pty-backed shell over /api/terminal/ws. Each panel owns its own WebSocket;
 * the server keys the shell by `id`, so a socket drop (reload, remount) only
 * detaches the shell and the next socket with the same id resumes it.
 *
 * `active` mirrors LogPanel's prop: the panel stays mounted while its tab is
 * backgrounded (so the shell keeps running and scrollback survives), but a
 * `display: none` container measures 0x0, so fitting is deferred until the tab
 * is visible again.
 *
 * `id` is also the scrollback persistence key. The server history endpoint is
 * restored oldest-to-newest before the socket opens; the bounded local copy is
 * used only when the endpoint/log is explicitly missing (404), so a restart
 * cannot silently restore only the local tail.
 */
export default function TerminalPanel({
  id,
  active,
  scrollbackLines,
  fontFamily,
  fontSize,
  projectPath,
  host,
}: {
  id: string;
  active: boolean;
  scrollbackLines: number;
  fontFamily: string;
  fontSize: number;
  projectPath: string;
  host?: string;
}) {
  const containerRef = useRef<HTMLDivElement>(null);
  const termRef = useRef<Terminal | null>(null);
  const fitRef = useRef<FitAddon | null>(null);
  const serializeRef = useRef<SerializeAddon | null>(null);
  const searchRef = useRef<SearchAddon | null>(null);
  const socketRef = useRef<WebSocket | null>(null);
  const dragCounterRef = useRef(0);
  // Drag-guard for links: suppress link activation if mouse moved between
  // mousedown and mouseup (i.e. user dragged a selection, not a plain click).
  const dragStartedRef = useRef(false);
  const dragMovedRef = useRef(false);
  const dragStartXRef = useRef(0);
  const dragStartYRef = useRef(0);
  const [isDragging, setIsDragging] = useState(false);
  const [ctxMenu, setCtxMenu] = useState<{ x: number; y: number; hasSelection: boolean } | null>(null);
  const ctxMenuRef = useRef<HTMLDivElement>(null);
  const [findOpen, setFindOpen] = useState(false);
  const [findQuery, setFindQuery] = useState("");
  const [findResult, setFindResult] = useState<{ count: number; index: number }>({ count: 0, index: -1 });
  // Mirrors `active` so the xterm event handlers (registered once at mount) can
  // read the live focused state without re-subscribing.
  const activeRef = useRef(active);
  // False during the initial scrollback replay; flipped true once the live pty
  // socket opens so a BEL baked into restored history can't false-alert.
  const readyRef = useRef(false);

  const findOpenRef = useRef(findOpen);
  const findQueryRef = useRef(findQuery);
  useEffect(() => { findOpenRef.current = findOpen; }, [findOpen]);
  useEffect(() => { findQueryRef.current = findQuery; }, [findQuery]);

  const { markAlerted, setOscTitle, openTerminal, closeTerminal } = useTerminalState();

  useEffect(() => {
    activeRef.current = active;
  }, [active]);

  // Upload dropped files and insert their names into the terminal stdin.
  const uploadAndInsert = useCallback(async (files: File[]) => {
    if (files.length === 0) return;
    const fd = new FormData();
    files.forEach((f) => fd.append("file", f));
    try {
      const query = projectPath ? `?project=${encodeURIComponent(projectPath)}` : "";
      const r = await fetch(apiPath(`/api/uploads${query}`), {
        method: "POST",
        headers: authHeaders(),
        body: fd,
      });
      if (!r.ok) {
        const body: { error?: string } = await r.json().catch(() => ({}));
        throw new Error(body.error || `upload failed with status ${r.status}`);
      }
      const raw: unknown = await r.json();
      const saved: { name: string }[] = Array.isArray(raw) ? (raw as { name: string }[]) : [];
      if (!Array.isArray(raw)) console.warn("upload response was not an array:", raw);
      const names = saved.map((f) => f.name);
      if (names.length === 0) return;
      const term = termRef.current;
      const sock = socketRef.current;
      if (!term || !sock || sock.readyState !== WebSocket.OPEN) return;
      // Insert uploaded file names into the terminal. Use the relative path
      // under the default upload directory so the user can immediately
      // reference them in shell commands (e.g. `cat .ocode/uploads/foo.txt`).
      // If a custom upload dir is configured this path won't resolve, but
      // the filename alone still helps.
      const paths = names.map((n) => `.ocode/uploads/${n}`);
      const text = paths.join(" ") + " ";
      sock.send(text);
    } catch (err) {
      console.error("terminal: file upload failed:", err);
    }
  }, [projectPath]);

  // ── Context menu (right-click) — Supacode-style ────────────────
  const handleContextMenu = useCallback((e: React.MouseEvent) => {
    e.preventDefault();
    e.stopPropagation();
    const sel = termRef.current?.getSelection() ?? "";
    setCtxMenu({ x: e.clientX, y: e.clientY, hasSelection: sel.length > 0 });
  }, []);

  useEffect(() => {
    if (!ctxMenu) return;
    const onDown = (e: MouseEvent) => {
      if (ctxMenuRef.current && !ctxMenuRef.current.contains(e.target as Node)) setCtxMenu(null);
    };
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") setCtxMenu(null);
    };
    const onScroll = () => setCtxMenu(null);
    document.addEventListener("mousedown", onDown);
    document.addEventListener("keydown", onKey);
    window.addEventListener("scroll", onScroll, true);
    window.addEventListener("resize", onScroll);
    return () => {
      document.removeEventListener("mousedown", onDown);
      document.removeEventListener("keydown", onKey);
      window.removeEventListener("scroll", onScroll, true);
      window.removeEventListener("resize", onScroll);
    };
  }, [ctxMenu]);

  const handleCopy = useCallback(async () => {
    const term = termRef.current;
    if (!term) return;
    const sel = term.getSelection();
    if (!sel) return;
    try {
      await navigator.clipboard.writeText(sel);
    } catch {
      // Fallback: use the async clipboard fallback via execCommand on a temp textarea
      const ta = document.createElement("textarea");
      ta.value = sel;
      ta.style.position = "fixed";
      ta.style.opacity = "0";
      document.body.appendChild(ta);
      ta.select();
      try { document.execCommand("copy"); } catch { /* ignore */ }
      ta.remove();
    }
    setCtxMenu(null);
  }, []);

  const handleSpeakSelection = useCallback(() => {
    const selection = termRef.current?.getSelection() ?? "";
    const text = sanitizeSpeechText(selection);
    if (text) requestSpeech(text);
    setCtxMenu(null);
  }, []);

  const handleSpeakVisible = useCallback(() => {
    setCtxMenu(null);
    const term = termRef.current;
    if (!term) return;
    const buffer = term.buffer.active;
    const start = buffer.viewportY;
    const lines = Array.from({ length: term.rows }, (_, index) => buffer.getLine(start + index)?.translateToString(true) ?? "");
    const text = sanitizeSpeechText(lines.join("\n"));
    if (text) requestSpeech(text);
  }, []);

  const handlePaste = useCallback(async () => {
    const sock = socketRef.current;
    // Prefer async clipboard; fall back to letting the browser handle paste if denied.
    try {
      const text = await navigator.clipboard.readText();
      if (text && sock && sock.readyState === WebSocket.OPEN) sock.send(text);
      else if (text) termRef.current?.paste(text);
    } catch {
      // Clipboard read requires a secure context / permission; hint the user.
      // As a fallback we focus the terminal so Ctrl+V / Cmd+V still works.
      termRef.current?.focus();
    }
    setCtxMenu(null);
  }, []);

  const handleSelectAll = useCallback(() => {
    termRef.current?.selectAll();
    setCtxMenu(null);
  }, []);

  const handleClear = useCallback(() => {
    termRef.current?.clear();
    // Persist the cleared state so the empty buffer survives reload — reuse
    // the existing serialize path immediately instead of waiting for the idle save.
    if (serializeRef.current && termRef.current) {
      saveTerminalBuffer(id, serializeRef.current.serialize({ scrollback: scrollbackLines }), termRef.current.cols, termRef.current.rows);
    }
    setCtxMenu(null);
  }, [id, scrollbackLines]);

  const handleReset = useCallback(() => {
    termRef.current?.reset();
    if (serializeRef.current && termRef.current) {
      saveTerminalBuffer(id, serializeRef.current.serialize({ scrollback: scrollbackLines }), termRef.current.cols, termRef.current.rows);
    }
    setCtxMenu(null);
  }, [id, scrollbackLines]);

  const handleScrollTop = useCallback(() => {
    termRef.current?.scrollToTop();
    setCtxMenu(null);
  }, []);

  const handleScrollBottom = useCallback(() => {
    termRef.current?.scrollToBottom();
    setCtxMenu(null);
  }, []);

  const handleFind = useCallback(() => {
    const sel = termRef.current?.getSelection() ?? "";
    if (sel && !findOpen) setFindQuery(sel);
    setFindOpen(true);
    // Keep dispatch for backwards compat with any external listener, but the
    // primary find UI is now handled locally via SearchAddon.
    window.dispatchEvent(new CustomEvent("ocode:terminal-find", { detail: { id } }));
    setCtxMenu(null);
  }, [id, findOpen]);

  const handleCloseFind = useCallback(() => {
    setFindOpen(false);
    searchRef.current?.clearDecorations();
    setFindResult({ count: 0, index: -1 });
    termRef.current?.focus();
  }, []);

  const searchOptions = useCallback(() => {
    // Enable decorations so matches are highlighted in the buffer and overview
    // ruler. Colors match the dark terminal theme; they are intentionally muted
    // to not clash with selection.
    return {
      decorations: {
        matchBackground: "#facc15",
        matchBorder: "#facc15",
        matchOverviewRuler: "#facc15",
        activeMatchBackground: "#f97316",
        activeMatchBorder: "#f97316",
        activeMatchColorOverviewRuler: "#f97316",
      },
    } as const;
  }, []);

  const handleFindNext = useCallback(() => {
    const q = findQuery;
    if (!q.trim()) return;
    searchRef.current?.findNext(q, searchOptions());
  }, [findQuery, searchOptions]);

  const handleFindPrev = useCallback(() => {
    const q = findQuery;
    if (!q.trim()) return;
    searchRef.current?.findPrevious(q, searchOptions());
  }, [findQuery, searchOptions]);

  const handleFindQueryChange = useCallback(
    (q: string) => {
      setFindQuery(q);
      if (!q.trim()) {
        searchRef.current?.clearDecorations();
        setFindResult({ count: 0, index: -1 });
        return;
      }
      // Incremental search: highlight and jump to first match as you type.
      searchRef.current?.findNext(q, searchOptions());
    },
    [searchOptions],
  );

  const handleNewTerminal = useCallback(() => {
    openTerminal(projectPath);
    setCtxMenu(null);
  }, [openTerminal, projectPath]);

  const handleCloseTerminal = useCallback(() => {
    closeTerminal(projectPath, id);
    setCtxMenu(null);
  }, [closeTerminal, projectPath, id]);

  // External find trigger (e.g. from future callers dispatching
  // ocode:terminal-find). HandleFind already opens locally, but this keeps
  // the event useful if dispatched from outside this component.
  useEffect(() => {
    const handler = (e: Event) => {
      const detail = (e as CustomEvent).detail as { id?: string } | undefined;
      if (detail?.id !== id) return;
      const sel = termRef.current?.getSelection() ?? "";
      if (sel && !findOpenRef.current) setFindQuery(sel);
      setFindOpen(true);
    };
    window.addEventListener("ocode:terminal-find", handler as EventListener);
    return () => window.removeEventListener("ocode:terminal-find", handler as EventListener);
  }, [id]);

  // Drag-and-drop handlers — follow the same counter-based pattern used by
  // ChatInput to correctly handle nested dragenter/dragleave from child
  // elements.
  const handleDragEnter = useCallback((e: React.DragEvent) => {
    e.preventDefault();
    e.stopPropagation();
    dragCounterRef.current++;
    if (e.dataTransfer.types.includes("Files")) {
      setIsDragging(true);
    }
  }, []);

  const handleDragLeave = useCallback((e: React.DragEvent) => {
    e.preventDefault();
    e.stopPropagation();
    dragCounterRef.current--;
    if (dragCounterRef.current === 0) {
      setIsDragging(false);
    }
  }, []);

  const handleDragOver = useCallback((e: React.DragEvent) => {
    e.preventDefault();
    e.stopPropagation();
  }, []);

  const handleDrop = useCallback(
    (e: React.DragEvent) => {
      e.preventDefault();
      e.stopPropagation();
      dragCounterRef.current = 0;
      setIsDragging(false);
      const files = Array.from(e.dataTransfer.files);
      if (files.length > 0) {
        uploadAndInsert(files);
      }
    },
    [uploadAndInsert],
  );

  // Fit to the container and tell the pty about the new size. No-op while the
  // container has no layout (hidden tab), which would otherwise force xterm to
  // a degenerate 1x1 grid and wreck the shell's line wrapping.
  const initialFitDoneRef = useRef(false);
  const fitAndResize = useRef(() => {
    const el = containerRef.current;
    const term = termRef.current;
    const fit = fitRef.current;
    if (!el || !term || !fit) return;
    if (el.clientWidth === 0 || el.clientHeight === 0) return;

    const applyFit = () => {
      fit.fit();
      const sock = socketRef.current;
      if (sock && sock.readyState === WebSocket.OPEN) {
        sock.send(JSON.stringify({ type: "resize", cols: term.cols, rows: term.rows }));
      }
    };

    if (initialFitDoneRef.current) {
      applyFit();
      return;
    }

    // On cold launch the surrounding layout (tab bar, sidebar panels) can
    // still be settling when this first fires, so el.clientWidth may be
    // transiently too narrow. Fitting against that reflows a restored
    // buffer's box-drawing content into garbage a later, correctly-sized fit
    // can't undo — so wait for width to hold steady across two frames before
    // the first real fit. ResizeObserver keeps re-invoking this while layout
    // is still settling, so the check naturally retries.
    const w = el.clientWidth;
    requestAnimationFrame(() => {
      requestAnimationFrame(() => {
        if (!containerRef.current || containerRef.current.clientWidth !== w) return;
        initialFitDoneRef.current = true;
        applyFit();
      });
    });
  });

  // Keep host in this lifecycle's dependencies. HomeApp gates startup on a
  // successful project-metadata snapshot, while a deliberate host identity
  // change must still rebuild history and the socket with the new destination.
  useEffect(() => {
    const el = containerRef.current;
    if (!el) return;

    const savedBuffer = loadTerminalBuffer(id);
    const term = new Terminal({
      cursorBlink: true,
      scrollback: scrollbackLines,
      fontFamily,
      fontSize,
      // Wheel/trackpad scrolled ~1 row per notch at the default
      // scrollSensitivity (1), which feels very slow on large scrollback.
      // 3x normal + 5x Alt-held fast scroll, instant (no smooth animation
      // lag) keeps long-history navigation responsive.
      scrollSensitivity: 3,
      fastScrollSensitivity: 5,
      smoothScrollDuration: 0,
      theme: { background: "#18181b", foreground: "#e4e4e7" },
      // Construct at the size the buffer was serialized at, so restoring it
      // doesn't reflow/garble the text before the ResizeObserver-driven fit()
      // ever runs. Falls back to xterm's defaults for legacy saved buffers
      // (or no buffer) that carry no cols/rows.
      ...(savedBuffer?.cols && savedBuffer?.rows
        ? { cols: savedBuffer.cols, rows: savedBuffer.rows }
        : {}),
    });
    const fit = new FitAddon();
    term.loadAddon(fit);
    const serialize = new SerializeAddon();
    term.loadAddon(serialize);
    const search = new SearchAddon();
    term.loadAddon(search);
    const searchDisp = search.onDidChangeResults((e) => {
      setFindResult({ count: e.resultCount, index: e.resultIndex });
    });
    term.open(el);
    // Links: WebLinksAddon handles http(s) URLs (e.g. Vite's localhost link),
    // registerFileLinkProvider handles file paths (src/foo.ts:12, .ocode/uploads)
    // and dispatches ocode:open-file to open the editor.
    let webLinks: InstanceType<typeof WebLinksAddon> | null = null;
    let fileLinkProvider: { dispose(): void } | null = null;
    try {
      // Open http(s) URLs on any left click (the addon's default only fires on
      // ctrl/cmd+click). Only left-click (button 0) opens; right-click pastes.
      webLinks = new WebLinksAddon((event, uri) => {
        if (event.type === "click" && event.button === 0 && /^https?:\/\//.test(uri) && !dragMovedRef.current) {
          window.open(uri, "_blank", "noopener");
        }
      });
      term.loadAddon(webLinks);
    } catch {
      webLinks = null;
    }
    try {
      fileLinkProvider = registerFileLinkProvider(term, projectPath, () => dragMovedRef.current);
    } catch {
      fileLinkProvider = { dispose() {} };
    }
    // Try WebGL renderer — GPU-accelerated, much lower memory than the canvas
    // renderer for large scrollback buffers. Falls back to canvas if WebGL is
    // unavailable (e.g. headless, offscreen, or unsupported browser).
    try {
      const webgl = new WebglAddon();
      webgl.onContextLoss(() => { webgl.dispose(); });
      term.loadAddon(webgl);
    } catch { /* fall back to canvas renderer */ }
    termRef.current = term;
    registerTerminal(id, term);
    fitRef.current = fit;
    serializeRef.current = serialize;
    searchRef.current = search;

    const searchOpts = {
      decorations: {
        matchBackground: "#facc15",
        matchBorder: "#facc15",
        matchOverviewRuler: "#facc15",
        activeMatchBackground: "#f97316",
        activeMatchBorder: "#f97316",
        activeMatchColorOverviewRuler: "#f97316",
      },
    } as const;

    // Custom key handler: Shift+Enter disambiguation, Ctrl/Cmd+F for find,
    // Esc to close find, F3/Ctrl+G to navigate matches.
    term.attachCustomKeyEventHandler((ev) => {
      if (ev.type !== "keydown") return true;
      if (ev.key === "Enter" && ev.shiftKey) {
        const sock = socketRef.current;
        if (sock && sock.readyState === WebSocket.OPEN) sock.send("\x1b[13;2u");
        return false;
      }
      if ((ev.ctrlKey || ev.metaKey) && !ev.altKey && ev.key.toLowerCase() === "f") {
        const sel = term.getSelection();
        if (sel && !findOpenRef.current) setFindQuery(sel);
        setFindOpen(true);
        return false;
      }
      if (ev.key === "Escape" && findOpenRef.current) {
        setFindOpen(false);
        search.clearDecorations();
        setFindResult({ count: 0, index: -1 });
        return false;
      }
      const q = findQueryRef.current;
      if (q && q.trim()) {
        if (ev.key === "F3" || ((ev.ctrlKey || ev.metaKey) && ev.key.toLowerCase() === "g")) {
          if (ev.shiftKey) search.findPrevious(q, searchOpts);
          else search.findNext(q, searchOpts);
          return false;
        }
      }
      return true;
    });

    // Attention signals: the BEL control char and the common notification OSC
    // sequences (9 = iTerm/most, 777/7777 = urxvt/others, 99 = kitty/notifications).
    // When the terminal is backgrounded we raise a "unread activity" badge and
    // (if the user enabled sound) play an alert. readyRef/activeRef keep the
    // handler stable across re-renders without re-subscribing; they also prevent
    // alerting for a BEL baked into the restored scrollback, or for a bell fired
    // while the user is already looking at this terminal.
    const onAttention = () => {
      if (!readyRef.current || activeRef.current) return;
      markAlerted(projectPath, id);
      playAlertSound();
    };
    const bellDisp = term.onBell(onAttention);
    // OSC 0/2 window title from the running program (claude code, the ocode
    // TUI, shells with a title-setting prompt) becomes the tab name unless
    // the user renamed the tab. Titles replayed from restored scrollback are
    // fine to apply: they are the program's last known title.
    // xterm normally exposes OSC 0/2 through onTitleChange. Register the
    // parser handlers as well so titles are captured even when a terminal is
    // hidden in the background. This also covers terminals whose title is
    // restored/replayed before xterm emits its public title event.
    const applyProgramTitle = (title: string) => setOscTitle(projectPath, id, title);
    const titleDisp = term.onTitleChange(applyProgramTitle);
    const osc0TitleDisp = term.parser.registerOscHandler(0, (title) => {
      applyProgramTitle(title);
      return true;
    });
    const osc2TitleDisp = term.parser.registerOscHandler(2, (title) => {
      applyProgramTitle(title);
      return true;
    });
    const osc9Disp = term.parser.registerOscHandler(9, () => {
      onAttention();
      return true;
    });
    const osc777Disp = term.parser.registerOscHandler(777, () => {
      onAttention();
      return true;
    });
    const osc99Disp = term.parser.registerOscHandler(99, () => {
      onAttention();
      return true;
    });

    // Defer serialization to avoid blocking the main thread. serialize() can
    // be CPU-heavy for large scrollback buffers, so we use requestIdleCallback
    // (with setTimeout fallback) for periodic saves. On pagehide/visibilitychange
    // we save immediately — the browser will complete the task before
    // unloading. The idle handle is stored so cleanup can correctly cancel it.
    let saveIdleId: number | null = null;
    const scheduleSave = () => {
      if (saveIdleId !== null) return; // already scheduled
      if (typeof requestIdleCallback === "function") {
        saveIdleId = requestIdleCallback(
          () => {
            saveIdleId = null;
            doSave();
          },
          { timeout: 5000 },
        ) as unknown as number;
      } else {
        saveIdleId = setTimeout(() => {
          saveIdleId = null;
          doSave();
        }, 0) as unknown as number;
      }
    };
    const doSave = () => {
      const s = serializeRef.current;
      if (!s) return;
      saveTerminalBuffer(id, s.serialize({ scrollback: scrollbackLines }), term.cols, term.rows);
    };
    const saveInterval = setInterval(scheduleSave, 30000);
    const onPageHide = () => doSave();
    document.addEventListener("visibilitychange", onPageHide);
    window.addEventListener("pagehide", onPageHide);

    let sock: WebSocket | null = null;
    let chunkRafId = 0;
    const pendingChunks: string[] = [];
    let pendingBytes = 0;
    let restoreCancelled = false;
    const restoreController = new AbortController();
    let serverHistoryRestored = false;
    let serverHistoryPartial = false;
    const terminalDecoder = new TextDecoder();

    const connectSocket = (historyOffset?: number) => {
      const { url, protocols } = buildTerminalWsConnection({
        token: authToken(),
        projectPath,
        host,
        terminalId: id,
        isRemote: isRemoteSession(),
        historyOffset,
      });
      const nextSocket = new WebSocket(url, protocols);
      nextSocket.binaryType = "arraybuffer";
      sock = nextSocket;
      socketRef.current = nextSocket;

      nextSocket.onopen = () => {
        readyRef.current = true;
        fitAndResize.current();
      };
      // Chunk large live writes to prevent memory spikes from one-shot decode+write.
      // This cap only applies after the complete server restore has finished;
      // dropped live bytes remain explicitly marked and are still persisted by
      // the server history log.
      const CHUNK_THRESHOLD = 64 * 1024;
      const CHUNK_SIZE = 16 * 1024;
      const PENDING_CHUNK_CAP = 2 * 1024 * 1024;
      let outputDropped = false;
      const flushChunks = () => {
        chunkRafId = 0;
        for (let i = 0; i < 4 && pendingChunks.length > 0; i++) {
          const chunk = pendingChunks.shift()!;
          pendingBytes -= chunk.length;
          term.write(chunk);
        }
        if (pendingChunks.length > 0) {
          chunkRafId = requestAnimationFrame(flushChunks);
        } else {
          outputDropped = false;
        }
      };
      nextSocket.onmessage = (ev) => {
        if (typeof ev.data === "string") {
          let msg: { type?: string; resumed?: boolean };
          try {
            msg = JSON.parse(ev.data);
          } catch (err) {
            console.error("terminal: unparseable control frame", ev.data, err);
            return;
          }
          // A cursor attach replays only bytes after the REST snapshot, so the
          // restored xterm buffer must stay intact. The old no-cursor attach
          // path still clears localStorage fallback before capped replay.
          if (msg.type === "attach" && msg.resumed && !serverHistoryRestored) term.reset();
          return;
        }
        const decoded = terminalDecoder.decode(new Uint8Array(ev.data as ArrayBuffer), { stream: true });
        if (decoded.length < CHUNK_THRESHOLD && pendingChunks.length === 0) {
          term.write(decoded);
          return;
        }
        for (let i = 0; i < decoded.length; i += CHUNK_SIZE) {
          const chunk = decoded.slice(i, i + CHUNK_SIZE);
          if (pendingBytes + chunk.length > PENDING_CHUNK_CAP) {
            if (!outputDropped) {
              outputDropped = true;
              term.write("\r\n\x1b[33m[terminal output truncated while rendering]\x1b[0m\r\n");
            }
            break;
          }
          pendingChunks.push(chunk);
          pendingBytes += chunk.length;
        }
        if (chunkRafId === 0) chunkRafId = requestAnimationFrame(flushChunks);
      };
      nextSocket.onerror = () => {
        console.error("terminal: websocket error on", url);
        term.write("\r\n\x1b[31m[terminal connection error]\x1b[0m\r\n");
      };
      nextSocket.onclose = (ev) => {
        const remainder = terminalDecoder.decode();
        if (remainder) term.write(remainder);
        if (!ev.wasClean) console.error("terminal: websocket closed unexpectedly", ev.code, ev.reason);
        term.write("\r\n\x1b[33m[terminal session ended]\x1b[0m\r\n");
      };
    };

    // xterm's scrollback is a row count, while the history cursor is a byte
    // count. Do not use snapshotEnd as scrollback: xterm allocates its
    // CircularList to that size immediately, so a large byte log would cause
    // a large, mostly empty allocation. Grow by the page's estimated row
    // count before replaying it, then release unused headroom while retaining
    // every row already rendered. Counting each code unit as up to two cells
    // is conservative for wide characters and keeps the estimate bounded by
    // the page size rather than the complete history size.
    const prepareHistoryPage = (text: string) => {
      const cols = Math.max(1, term.cols);
      let newlineRows = 1;
      for (let i = 0; i < text.length; i++) {
        if (text[i] === "\n") newlineRows++;
      }
      const wrappedRows = Math.ceil((text.length * 2) / cols);
      const pageRows = Math.max(1, newlineRows, wrappedRows);
      const currentLines = term.buffer.active.length;
      const requiredScrollback = currentLines - term.rows + pageRows;
      term.options.scrollback = Math.max(scrollbackLines, requiredScrollback);
    };
    const trimHistoryHeadroom = () => {
      const requiredScrollback = term.buffer.active.length - term.rows;
      term.options.scrollback = Math.max(scrollbackLines, requiredScrollback);
    };

    void restoreTerminalHistory({
      id,
      projectPath,
      host,
      decoder: terminalDecoder,
      signal: restoreController.signal,
      onText: (text) => {
        if (restoreCancelled || restoreController.signal.aborted) return;
        serverHistoryPartial = true;
        prepareHistoryPage(text);
        // xterm parses writes asynchronously. Wait for the page callback
        // before trimming headroom; doing it immediately would observe the
        // pre-page buffer length and could evict the page just queued.
        return new Promise<void>((resolve) => {
          term.write(text, () => {
            trimHistoryHeadroom();
            resolve();
          });
        });
      },
    }).then((result) => {
      if (restoreCancelled) return;
      if (result.kind === "missing") {
        // Only announce the fallback when there is actually a cached
        // buffer to fall back to; a brand-new terminal has nothing to restore.
        if (savedBuffer) {
          term.write(savedBuffer.text);
          term.write("\r\n\x1b[2m── local terminal cache fallback; server history unavailable ──\x1b[0m\r\n");
        }
        connectSocket();
        return;
      }
      serverHistoryRestored = true;
      connectSocket(result.snapshotEnd);
    }).catch((err: unknown) => {
      if (restoreCancelled || restoreController.signal.aborted) return;
      // Do not leave a partial server replay on screen before attaching
      // without a cursor. A resumed shell will send its capped replay, while
      // a fresh shell starts from a clean buffer; either way this avoids
      // overlapping partial output and live replay.
      if (serverHistoryPartial) term.reset();
      const message = err instanceof TerminalHistoryError ? err.message : "terminal history restore failed";
      console.error("terminal: history restore failed", err);
      term.write(`\r\n\x1b[31m[${message}; live terminal attached without restore]\x1b[0m\r\n`);
      connectSocket();
    });

    // Everything the client sends is text; the backend treats a frame starting
    // with {"type":"resize" as a control message and anything else as raw
    // keystrokes.
    const dataSub = term.onData((data) => {
      if (sock?.readyState === WebSocket.OPEN) sock.send(data);
    });

    const observer = new ResizeObserver(() => fitAndResize.current());
    observer.observe(el);

    return () => {
      restoreCancelled = true;
      restoreController.abort();
      doSave();
      clearInterval(saveInterval);
      if (saveIdleId !== null) {
        if (typeof cancelIdleCallback === "function") {
          cancelIdleCallback(saveIdleId as unknown as number);
        } else {
          clearTimeout(saveIdleId as unknown as number);
        }
      }
      document.removeEventListener("visibilitychange", onPageHide);
      window.removeEventListener("pagehide", onPageHide);
      observer.disconnect();
      dataSub.dispose();
      bellDisp.dispose();
      titleDisp.dispose();
      osc0TitleDisp.dispose();
      osc2TitleDisp.dispose();
      osc9Disp.dispose();
      osc777Disp.dispose();
      osc99Disp.dispose();
      searchDisp.dispose();
      search.dispose();
      try {
        fileLinkProvider?.dispose();
      } catch {}
      try {
        webLinks?.dispose();
      } catch {}
      // Cancel any pending chunk writes.
      if (chunkRafId) cancelAnimationFrame(chunkRafId);
      pendingChunks.length = 0;
      // Closing the socket only detaches the shell server-side: it survives
      // for the detach TTL so a reload/remount reattaches to it. Explicit tab
      // close kills it via DELETE /api/terminal/{id} in the terminal store.
      sock?.close();
      socketRef.current = null;
      term.dispose();
      unregisterTerminal(id);
      termRef.current = null;
      fitRef.current = null;
      serializeRef.current = null;
      searchRef.current = null;
    };
    // Backend switches intentionally do NOT restart existing terminals:
    // the PTY is host-local and would be lost. New terminals after a switch
    // use the new apiWsPath; a full reload migrates all.
  }, [projectPath, host, id]);

  // Apply scrollback changes without tearing down the pty.
  useEffect(() => {
    const term = termRef.current;
    if (!term) return;
    term.options.scrollback = scrollbackLines;
  }, [scrollbackLines]);

  // Apply font changes to the live terminal in place instead of tearing down
  // the session (which would kill the pty/websocket) whenever settings change.
  useEffect(() => {
    const term = termRef.current;
    if (!term) return;
    term.options.fontFamily = fontFamily;
    term.options.fontSize = fontSize;
    fitAndResize.current();
  }, [fontFamily, fontSize]);

  // Re-fit when the tab comes back to the foreground: any resize that happened
  // while hidden was skipped because the container had no layout.
  // Also focus the xterm terminal so the user can type immediately.
  useEffect(() => {
    if (active) {
      fitAndResize.current();
      termRef.current?.focus();
    }
  }, [active]);

  // Allow the tab bar to focus this shell on single left-click, even when
  // `active` does not change (already-active tab). The registry is keyed by
  // terminal id so UnifiedTabBar can call focus without threading refs through App.
  useEffect(() => {
    const doFocus = () => termRef.current?.focus();
    registerTerminalFocus(id, doFocus);
    return () => unregisterTerminalFocus(id);
  }, [id]);

  // Clamp menu to viewport (same strategy as Layout/ContextMenu)
  const clampedMenuPos = ctxMenu
    ? {
        x: Math.min(ctxMenu.x, typeof window !== "undefined" ? window.innerWidth - 220 : ctxMenu.x),
        y: Math.min(ctxMenu.y, typeof window !== "undefined" ? window.innerHeight - 360 : ctxMenu.y),
      }
    : null;

  const menuItems: Array<{
    label: string;
    icon: React.ReactNode;
    onClick: () => void;
    disabled?: boolean;
    separator?: boolean;
    destructive?: boolean;
  }> = ctxMenu
    ? [
        { label: "Copy", icon: <Copy className="h-4 w-4" />, onClick: handleCopy, disabled: !ctxMenu.hasSelection },
        { label: "Play selection", icon: <Volume2 className="h-4 w-4" />, onClick: handleSpeakSelection, disabled: !ctxMenu.hasSelection },
        { label: "Paste", icon: <ClipboardPaste className="h-4 w-4" />, onClick: handlePaste },
        { label: "Select All", icon: <CopyPlus className="h-4 w-4" />, onClick: handleSelectAll },
        { label: "", icon: null as unknown as React.ReactNode, onClick: () => {}, separator: true },
        { label: "Clear Terminal", icon: <Trash2 className="h-4 w-4" />, onClick: handleClear },
        { label: "Reset Terminal", icon: <RotateCcw className="h-4 w-4" />, onClick: handleReset },
        { label: "Scroll to Top", icon: <ArrowUpToLine className="h-4 w-4" />, onClick: handleScrollTop },
        { label: "Scroll to Bottom", icon: <ArrowDownToLine className="h-4 w-4" />, onClick: handleScrollBottom },
        { label: "Speak visible", icon: <Volume2 className="h-4 w-4" />, onClick: handleSpeakVisible },
        { label: "", icon: null as unknown as React.ReactNode, onClick: () => {}, separator: true },
        { label: "Find…", icon: <Search className="h-4 w-4" />, onClick: handleFind },
        { label: "", icon: null as unknown as React.ReactNode, onClick: () => {}, separator: true },
        { label: "New Terminal", icon: <Plus className="h-4 w-4" />, onClick: handleNewTerminal },
        { label: "Close Terminal", icon: <X className="h-4 w-4" />, onClick: handleCloseTerminal, destructive: true },
      ]
    : [];

  return (
    <div
      ref={containerRef}
      // This div is xterm's fit parent, so it must stay padding-free:
      // FitAddon subtracts only the .xterm element's own padding from the
      // parent's computed (border-box) height, so a `p-2` here made the fitted
      // grid up to a row taller than the visible content box — clipped by the
      // app-level overflow-hidden with no way to reach it. The 8px padding
      // therefore lives on .xterm itself ([&_.xterm]:p-2, same visual ring,
      // same color: --card equals the xterm background #18181b), which makes
      // the fit exact. overflow-y-auto is the requested "vertical scroll:
      // auto": a scrollbar appears only when content genuinely overflows
      // (font resize before refit, tiny windows where even one row doesn't
      // fit). Horizontal overflow belongs to xterm, hence overflow-x-hidden.
      className="relative h-full w-full bg-card overflow-y-auto overflow-x-hidden [&_.xterm]:p-2"
      onContextMenu={handleContextMenu}
      onMouseDown={(e) => {
        dragStartedRef.current = true;
        dragMovedRef.current = false;
        dragStartXRef.current = e.clientX;
        dragStartYRef.current = e.clientY;
      }}
      onMouseMove={(e) => {
        if (!dragStartedRef.current) return;
        const dx = Math.abs(e.clientX - dragStartXRef.current);
        const dy = Math.abs(e.clientY - dragStartYRef.current);
        if (dx > 2 || dy > 2) dragMovedRef.current = true;
      }}
      onMouseUp={() => {
        dragStartedRef.current = false;
      }}
      onDragEnter={handleDragEnter}
      onDragLeave={handleDragLeave}
      onDragOver={handleDragOver}
      onDrop={handleDrop}
    >
      {findOpen && (
        <TerminalFindBar
          query={findQuery}
          onQueryChange={handleFindQueryChange}
          resultCount={findResult.count}
          resultIndex={findResult.index}
          onNext={handleFindNext}
          onPrev={handleFindPrev}
          onClose={handleCloseFind}
        />
      )}
      {isDragging && (
        <div className="pointer-events-none absolute inset-0 z-10 flex items-center justify-center rounded-md border-2 border-dashed border-blue-500 bg-blue-500/10">
          <span className="text-sm font-medium text-blue-300">Drop files here</span>
        </div>
      )}
      {ctxMenu &&
        clampedMenuPos &&
        createPortal(
          <div
            ref={ctxMenuRef}
            role="menu"
            className="fixed z-50 min-w-[200px] bg-popover border border-border rounded-md shadow-md py-1 animate-in fade-in-0 zoom-in-95"
            style={{ left: clampedMenuPos.x, top: clampedMenuPos.y }}
            onClick={(e) => e.stopPropagation()}
          >
            {menuItems.map((item, i) => {
              if (item.separator) return <div key={i} className="h-px bg-border my-1" />;
              return (
                <button
                  key={i}
                  role="menuitem"
                  disabled={!!item.disabled}
                  className={`w-full flex items-center gap-2 px-3 py-1.5 text-sm text-left ${
                    item.destructive
                      ? "text-destructive hover:bg-destructive/10"
                      : "text-foreground hover:bg-accent hover:text-accent-foreground"
                  } ${item.disabled ? "opacity-50 pointer-events-none" : ""}`}
                  onClick={item.onClick}
                >
                  {item.icon && <span className="w-4 h-4 shrink-0 flex items-center justify-center">{item.icon}</span>}
                  {item.label}
                </button>
              );
            })}
          </div>,
          document.body,
        )}
    </div>
  );
}
