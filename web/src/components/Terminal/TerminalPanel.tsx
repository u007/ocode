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
} from "lucide-react";
import { Terminal } from "@xterm/xterm";
import { FitAddon } from "@xterm/addon-fit";
import { SearchAddon } from "@xterm/addon-search";
import { SerializeAddon } from "@xterm/addon-serialize";
import { WebglAddon } from "@xterm/addon-webgl";
import "@xterm/xterm/css/xterm.css";
import TerminalFindBar from "./TerminalFindBar";
import { apiPath, apiWsPath, authHeaders, authToken } from "@/api/client";
import { loadTerminalBuffer, saveTerminalBuffer } from "./terminalPersistence";
import { registerTerminal, unregisterTerminal } from "@/lib/debug/terminalRegistry";
import { playAlertSound } from "./terminalAlertSound";
import { useTerminalState } from "../../stores/terminalStore";
import { registerTerminalFocus, unregisterTerminalFocus } from "./terminalFocus";

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
 * `id` is also the scrollback persistence key: on mount, text saved under it
 * (if any) is painted first so a reload shows something immediately. When the
 * server reports the shell was resumed, that local copy is replaced by the
 * server's replay of the live shell's recent output; when the shell is gone
 * (server restarted, detach TTL expired) the local copy stays as history.
 */
export default function TerminalPanel({
  id,
  active,
  scrollbackLines,
  fontFamily,
  fontSize,
  projectPath,
}: {
  id: string;
  active: boolean;
  scrollbackLines: number;
  fontFamily: string;
  fontSize: number;
  projectPath: string;
}) {
  const containerRef = useRef<HTMLDivElement>(null);
  const termRef = useRef<Terminal | null>(null);
  const fitRef = useRef<FitAddon | null>(null);
  const serializeRef = useRef<SerializeAddon | null>(null);
  const searchRef = useRef<SearchAddon | null>(null);
  const socketRef = useRef<WebSocket | null>(null);
  const dragCounterRef = useRef(0);
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

  const { markAlerted, openTerminal, closeTerminal } = useTerminalState();

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

  useEffect(() => {
    const el = containerRef.current;
    if (!el) return;

    const savedBuffer = loadTerminalBuffer(id);
    const term = new Terminal({
      cursorBlink: true,
      scrollback: scrollbackLines,
      fontFamily,
      fontSize,
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
    // Try WebGL renderer — GPU-accelerated, much lower memory than the DOM
    // renderer for large scrollback buffers. Falls back to DOM if WebGL is
    // unavailable (e.g. headless, offscreen, or unsupported browser).
    try {
      const webgl = new WebglAddon();
      webgl.onContextLoss(() => { webgl.dispose(); });
      term.loadAddon(webgl);
    } catch { /* fall back to DOM renderer */ }
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

    if (savedBuffer) {
      term.write(savedBuffer.text);
      term.write("\r\n\x1b[2m── restored, shell disconnected ──\x1b[0m\r\n");
    }

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

    const token = authToken();
    const params = new URLSearchParams();
    if (token) params.set("token", token);
    // project_path pins the shell's cwd to this tab's project; the server
    // validates it against its registered project roots. terminal_id lets the
    // terminal-processes emitter (Processes tab) correlate a pid with this tab.
    if (projectPath) params.set("project_path", projectPath);
    params.set("terminal_id", id);
    const query = params.toString();
    // apiWsPath keeps the tailscale --set-path prefix and respects the
    // configured backend origin (same-origin vs hub). Handles ws/wss
    // conversion for absolute backend URLs.
    const url = apiWsPath(`/api/terminal/ws${query ? `?${query}` : ""}`);
    const sock = new WebSocket(url);
    sock.binaryType = "arraybuffer";
    socketRef.current = sock;

    const decoder = new TextDecoder();
    sock.onopen = () => {
      readyRef.current = true;
      fitAndResize.current();
    };
    // Chunk large binary writes to prevent memory spikes from one-shot decode+write.
    const CHUNK_THRESHOLD = 64 * 1024; // 64KB
    const CHUNK_SIZE = 16 * 1024;      // 16KB per chunk
    const PENDING_CHUNK_CAP = 2 * 1024 * 1024;
    const pendingChunks: string[] = [];
    let pendingBytes = 0;
    let outputDropped = false;
    let chunkRafId = 0;
    const flushChunks = () => {
      chunkRafId = 0;
      // Write up to 4 chunks per frame to stay under 16ms.
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
    sock.onmessage = (ev) => {
      if (typeof ev.data === "string") {
        // The only text frame the server sends is the attach control message
        // (everything else is binary pty output). resumed=true means the
        // shell survived the disconnect and a replay of its recent output
        // follows — drop the locally restored scrollback so it isn't shown
        // twice.
        let msg: { type?: string; resumed?: boolean };
        try {
          msg = JSON.parse(ev.data);
        } catch (err) {
          console.error("terminal: unparseable control frame", ev.data, err);
          return;
        }
        if (msg.type === "attach" && msg.resumed) term.reset();
        return;
      }
      const decoded = decoder.decode(new Uint8Array(ev.data as ArrayBuffer), { stream: true });
      if (decoded.length < CHUNK_THRESHOLD && pendingChunks.length === 0) {
        term.write(decoded);
        return;
      }
      // Split into chunks and schedule incremental writes.
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
      if (chunkRafId === 0) {
        chunkRafId = requestAnimationFrame(flushChunks);
      }
    };
    sock.onerror = () => {
      // The browser gives no detail on WS errors; onclose carries the code.
      console.error("terminal: websocket error on", url);
      term.write("\r\n\x1b[31m[terminal connection error]\x1b[0m\r\n");
    };
    sock.onclose = (ev) => {
      const remainder = decoder.decode();
      if (remainder) term.write(remainder);
      if (!ev.wasClean) {
        console.error("terminal: websocket closed unexpectedly", ev.code, ev.reason);
      }
      term.write("\r\n\x1b[33m[terminal session ended]\x1b[0m\r\n");
    };

    // Everything the client sends is text; the backend treats a frame starting
    // with {"type":"resize" as a control message and anything else as raw
    // keystrokes.
    const dataSub = term.onData((data) => {
      if (sock.readyState === WebSocket.OPEN) sock.send(data);
    });

    const observer = new ResizeObserver(() => fitAndResize.current());
    observer.observe(el);

    return () => {
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
      osc9Disp.dispose();
      osc777Disp.dispose();
      osc99Disp.dispose();
      searchDisp.dispose();
      search.dispose();
      // Cancel any pending chunk writes.
      if (chunkRafId) cancelAnimationFrame(chunkRafId);
      pendingChunks.length = 0;
      // Closing the socket only detaches the shell server-side: it survives
      // for the detach TTL so a reload/remount reattaches to it. Explicit tab
      // close kills it via DELETE /api/terminal/{id} in the terminal store.
      sock.close();
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
  }, [projectPath, id]);

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
        { label: "Paste", icon: <ClipboardPaste className="h-4 w-4" />, onClick: handlePaste },
        { label: "Select All", icon: <CopyPlus className="h-4 w-4" />, onClick: handleSelectAll },
        { label: "", icon: null as unknown as React.ReactNode, onClick: () => {}, separator: true },
        { label: "Clear Terminal", icon: <Trash2 className="h-4 w-4" />, onClick: handleClear },
        { label: "Reset Terminal", icon: <RotateCcw className="h-4 w-4" />, onClick: handleReset },
        { label: "Scroll to Top", icon: <ArrowUpToLine className="h-4 w-4" />, onClick: handleScrollTop },
        { label: "Scroll to Bottom", icon: <ArrowDownToLine className="h-4 w-4" />, onClick: handleScrollBottom },
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
      className="relative h-full w-full bg-card p-2"
      onContextMenu={handleContextMenu}
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
