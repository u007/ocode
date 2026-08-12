import { useEffect, useRef } from "react";
import { Terminal } from "@xterm/xterm";
import { FitAddon } from "@xterm/addon-fit";
import "@xterm/xterm/css/xterm.css";
import { apiPath, authToken } from "@/api/client";

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
 */
export default function TerminalPanel({
  active,
  scrollbackLines,
  projectPath,
}: {
  active: boolean;
  scrollbackLines: number;
  projectPath: string;
}) {
  const containerRef = useRef<HTMLDivElement>(null);
  const termRef = useRef<Terminal | null>(null);
  const fitRef = useRef<FitAddon | null>(null);
  const socketRef = useRef<WebSocket | null>(null);

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

    const term = new Terminal({
      cursorBlink: true,
      scrollback: scrollbackLines,
      fontFamily: "ui-monospace, SFMono-Regular, Menlo, monospace",
      fontSize: 13,
      theme: { background: "#18181b", foreground: "#e4e4e7" },
    });
    const fit = new FitAddon();
    term.loadAddon(fit);
    term.open(el);
    termRef.current = term;
    fitRef.current = fit;

    const scheme = window.location.protocol === "https:" ? "wss:" : "ws:";
    const token = authToken();
    const params = new URLSearchParams();
    if (token) params.set("token", token);
    // project_path pins the shell's cwd to this tab's project; the server
    // validates it against its registered project roots.
    if (projectPath) params.set("project_path", projectPath);
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
    sock.onmessage = (ev) => {
      if (typeof ev.data === "string") {
        term.write(ev.data);
        return;
      }
      term.write(decoder.decode(new Uint8Array(ev.data as ArrayBuffer), { stream: true }));
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
      observer.disconnect();
      dataSub.dispose();
      // Closing the socket is what kills the pty process server-side.
      sock.close();
      socketRef.current = null;
      term.dispose();
      termRef.current = null;
      fitRef.current = null;
    };
  }, [scrollbackLines, projectPath]);

  // Re-fit when the tab comes back to the foreground: any resize that happened
  // while hidden was skipped because the container had no layout.
  useEffect(() => {
    if (active) fitAndResize.current();
  }, [active]);

  return <div ref={containerRef} className="h-full w-full bg-zinc-900 p-2" />;
}
