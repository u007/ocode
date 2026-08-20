import { useCallback, useEffect, useRef, useState } from "react";
import { Terminal } from "@xterm/xterm";
import { FitAddon } from "@xterm/addon-fit";
import { SerializeAddon } from "@xterm/addon-serialize";
import { WebglAddon } from "@xterm/addon-webgl";
import "@xterm/xterm/css/xterm.css";
import { apiPath, authHeaders, authToken } from "@/api/client";
import { loadTerminalBuffer, saveTerminalBuffer } from "./terminalPersistence";

/**
 * A single interactive terminal: one xterm.js instance bridged to one
 * pty-backed shell over /api/terminal/ws. Each panel owns its own WebSocket,
 * so the backend stays stateless per connection and multiplicity is purely a
 * frontend concern (see TerminalTabs).
 *
 * `active` mirrors LogPanel's prop: the panel stays mounted while its tab is
 * backgrounded (so the shell keeps running and scrollback survives), but a
 * `display: none` container measures 0x0, so fitting is deferred until the tab
 * is visible again.
 *
 * `id` identifies this terminal for scrollback persistence: on mount, text
 * saved under this id (if any) is replayed before the fresh pty connects, so
 * a reload shows what happened last time even though the shell itself is new.
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
  const socketRef = useRef<WebSocket | null>(null);
  const dragCounterRef = useRef(0);
  const [isDragging, setIsDragging] = useState(false);

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
      const saved: { name: string }[] = await r.json();
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
  const fitAndResize = useRef(() => {
    const el = containerRef.current;
    const term = termRef.current;
    const fit = fitRef.current;
    if (!el || !term || !fit) return;
    if (el.clientWidth === 0 || el.clientHeight === 0) return;
    fit.fit();
    const sock = socketRef.current;
    if (sock && sock.readyState === WebSocket.OPEN) {
      sock.send(JSON.stringify({ type: "resize", cols: term.cols, rows: term.rows }));
    }
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
    fitRef.current = fit;
    serializeRef.current = serialize;

    // xterm.js sends plain "\r" for Enter regardless of Shift, so a nested
    // TUI (e.g. ocode itself) can never tell them apart. Emit the CSI-u
    // disambiguated sequence bubbletea already knows how to decode
    // (charm.land/bubbletea/v2's key disambiguation is on by default) so
    // Shift+Enter reaches it as a distinct key instead of a second Enter.
    term.attachCustomKeyEventHandler((ev) => {
      if (ev.type === "keydown" && ev.key === "Enter" && ev.shiftKey) {
        const sock = socketRef.current;
        if (sock && sock.readyState === WebSocket.OPEN) sock.send("\x1b[13;2u");
        return false;
      }
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

    const scheme = window.location.protocol === "https:" ? "wss:" : "ws:";
    const token = authToken();
    const params = new URLSearchParams();
    if (token) params.set("token", token);
    // project_path pins the shell's cwd to this tab's project; the server
    // validates it against its registered project roots. terminal_id lets the
    // terminal-processes emitter (Processes tab) correlate a pid with this tab.
    if (projectPath) params.set("project_path", projectPath);
    params.set("terminal_id", id);
    const query = params.toString();
    // apiPath() keeps the tailscale --set-path prefix, without which the proxy
    // routes the socket to whichever session owns the root path.
    const url = `${scheme}//${window.location.host}${apiPath("/api/terminal/ws")}${
      query ? `?${query}` : ""
    }`;
    const sock = new WebSocket(url);
    sock.binaryType = "arraybuffer";
    socketRef.current = sock;

    const decoder = new TextDecoder();
    sock.onopen = () => fitAndResize.current();
    // Chunk large binary writes to prevent memory spikes from one-shot decode+write.
    const CHUNK_THRESHOLD = 64 * 1024; // 64KB
    const CHUNK_SIZE = 16 * 1024;      // 16KB per chunk
    const pendingChunks: string[] = [];
    let chunkRafId = 0;
    const flushChunks = () => {
      chunkRafId = 0;
      // Write up to 4 chunks per frame to stay under 16ms.
      for (let i = 0; i < 4 && pendingChunks.length > 0; i++) {
        term.write(pendingChunks.shift()!);
      }
      if (pendingChunks.length > 0) {
        chunkRafId = requestAnimationFrame(flushChunks);
      }
    };
    sock.onmessage = (ev) => {
      if (typeof ev.data === "string") {
        term.write(ev.data);
        return;
      }
      const decoded = decoder.decode(new Uint8Array(ev.data as ArrayBuffer), { stream: true });
      if (decoded.length < CHUNK_THRESHOLD && pendingChunks.length === 0) {
        term.write(decoded);
        return;
      }
      // Split into chunks and schedule incremental writes.
      for (let i = 0; i < decoded.length; i += CHUNK_SIZE) {
        pendingChunks.push(decoded.slice(i, i + CHUNK_SIZE));
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
      // Cancel any pending chunk writes.
      if (chunkRafId) cancelAnimationFrame(chunkRafId);
      pendingChunks.length = 0;
      // Closing the socket is what kills the pty process server-side.
      sock.close();
      socketRef.current = null;
      term.dispose();
      termRef.current = null;
      fitRef.current = null;
      serializeRef.current = null;
    };
  }, [scrollbackLines, projectPath]);

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

  return (
    <div
      ref={containerRef}
      className="relative h-full w-full bg-zinc-900 p-2"
      onDragEnter={handleDragEnter}
      onDragLeave={handleDragLeave}
      onDragOver={handleDragOver}
      onDrop={handleDrop}
    >
      {isDragging && (
        <div className="pointer-events-none absolute inset-0 z-10 flex items-center justify-center rounded-md border-2 border-dashed border-blue-500 bg-blue-500/10">
          <span className="text-sm font-medium text-blue-300">Drop files here</span>
        </div>
      )}
    </div>
  );
}
