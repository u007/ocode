import { useEffect, useRef, useState, useCallback } from "react";
import { useCdpSocket } from "./useCdpSocket";
import type { StateKey } from "../../lib/browserStore";

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

/** Printable-key text for CDP: Enter is "\r", everything printable is itself,
 *  control keys have no text. Mirrors how a real browser fills text for
 *  Input.dispatchKeyEvent. */
function keyText(key: string): string {
  if (key === "Enter") return "\r";
  if (key === "Backspace" || key === "Delete" || key === "Tab" || key === "Escape" || key.startsWith("Arrow") || key.startsWith("F") && /^F\d+$/.test(key)) {
    return "";
  }
  return key.length === 1 ? key : "";
}

export interface ChromeViewportProps {
  stateKey: StateKey;
  browseBase: string | null;
  url: string;
}

/** Chrome-mode viewport: renders the CDP screencast on a canvas and forwards
 *  pointer/keyboard input over the per-stateKey socket. The viewport IS the
 *  page — chrome is only the address bar's status row above it. Coordinates
 *  are CSS pixels relative to the canvas rect (Chrome expects CSS px; the
 *  screencast frames are device px and are only used for the backing store). */
export function ChromeViewport({ stateKey, browseBase, url }: ChromeViewportProps) {
  const { send, status, error, onFrame } = useCdpSocket(stateKey, browseBase, true);
  const canvasRef = useRef<HTMLCanvasElement | null>(null);
  const [hasFrame, setHasFrame] = useState(false);
  // Start empty so the FIRST mount always navigates (iframe → chrome switches,
  // e.g. the dev-server escape hatch, would otherwise mount a target sitting
  // on the initial URL with no nav command and render blank).
  const lastUrlRef = useRef("");
  // Pending pointermove, coalesced to one per animation frame (~16ms).
  const pendingMove = useRef<{ x: number; y: number; mods: number } | null>(null);
  const moveTimer = useRef<ReturnType<typeof setTimeout> | null>(null);
  const lastClickTime = useRef(0);
  const clickCount = useRef(0);

  // Navigate whenever the authoritative store URL changes (address bar,
  // back/forward, chrome hand-off). The store is the single source of truth.
  useEffect(() => {
    if (url && url !== lastUrlRef.current) {
      lastUrlRef.current = url;
      send({ t: "nav", url });
    }
  }, [url, send]);

  // Frames → backing store + paint.
  useEffect(() => {
    return onFrame((bitmap, w, h) => {
      const canvas = canvasRef.current;
      if (!canvas) return;
      if (canvas.width !== w) canvas.width = w;
      if (canvas.height !== h) canvas.height = h;
      const ctx = canvas.getContext("2d");
      if (ctx) {
        ctx.drawImage(bitmap as unknown as CanvasImageSource, 0, 0);
      }
      (bitmap as unknown as { close?: () => void }).close?.();
      setHasFrame(true);
    });
  }, [onFrame]);

  // Container resize → Emulation.setDeviceMetricsOverride via the socket.
  useEffect(() => {
    const canvas = canvasRef.current;
    if (!canvas || typeof ResizeObserver === "undefined") return;
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
  }, [send]);

  const canvasPos = useCallback((e: { clientX: number; clientY: number }) => {
    const canvas = canvasRef.current;
    if (!canvas) return { x: 0, y: 0 };
    const rect = canvas.getBoundingClientRect();
    return { x: e.clientX - rect.left, y: e.clientY - rect.top };
  }, []);

  const buttonName = (b: number): string =>
    b === 2 ? "right" : b === 1 ? "middle" : "left";

  const onPointerDown = (e: React.PointerEvent<HTMLCanvasElement>) => {
    e.preventDefault();
    canvasRef.current?.focus();
    const { x, y } = canvasPos(e);
    // Double/triple-click detection within 500ms of the same position.
    const now = Date.now();
    clickCount.current = now - lastClickTime.current < 500 ? Math.min(clickCount.current + 1, 3) : 1;
    lastClickTime.current = now;
    send({
      t: "mouse", kind: "down", x, y,
      button: buttonName(e.button),
      clickCount: clickCount.current,
      modifiers: modifiersOf(e),
    });
  };

  const onPointerUp = (e: React.PointerEvent<HTMLCanvasElement>) => {
    e.preventDefault();
    const { x, y } = canvasPos(e);
    send({
      t: "mouse", kind: "up", x, y,
      button: buttonName(e.button),
      clickCount: clickCount.current,
      modifiers: modifiersOf(e),
    });
  };

  const onPointerMove = (e: React.PointerEvent<HTMLCanvasElement>) => {
    const { x, y } = canvasPos(e);
    pendingMove.current = { x, y, mods: modifiersOf(e) };
    if (moveTimer.current) return; // coalesce: one move per frame
    moveTimer.current = setTimeout(() => {
      moveTimer.current = null;
      const p = pendingMove.current;
      pendingMove.current = null;
      if (p) send({ t: "mouse", kind: "move", x: p.x, y: p.y, button: "none", clickCount: 0, modifiers: p.mods });
    }, 16);
  };

  const onWheel = (e: React.WheelEvent<HTMLCanvasElement>) => {
    e.preventDefault();
    const { x, y } = canvasPos(e);
    send({
      t: "mouse", kind: "wheel", x, y,
      deltaX: e.deltaX, deltaY: e.deltaY,
      modifiers: modifiersOf(e),
    });
  };

  const onKeyDown = (e: React.KeyboardEvent<HTMLCanvasElement>) => {
    e.preventDefault();
    const mods = modifiersOf(e);
    send({ t: "key", kind: "down", key: e.key, code: e.code, text: keyText(e.key), modifiers: mods });
    if (keyText(e.key)) {
      send({ t: "key", kind: "char", key: e.key, code: e.code, text: keyText(e.key), modifiers: mods });
    }
  };

  const onKeyUp = (e: React.KeyboardEvent<HTMLCanvasElement>) => {
    e.preventDefault();
    send({ t: "key", kind: "up", key: e.key, code: e.code, text: keyText(e.key), modifiers: modifiersOf(e) });
  };

  return (
    <div className="relative flex-1 min-h-0 w-full">
      <canvas
        ref={canvasRef}
        tabIndex={0}
        className="block w-full h-full outline-none focus-visible:ring-1 focus-visible:ring-blue-500/60"
        onPointerDown={onPointerDown}
        onPointerUp={onPointerUp}
        onPointerMove={onPointerMove}
        onWheel={onWheel}
        onKeyDown={onKeyDown}
        onKeyUp={onKeyUp}
        onContextMenu={(e) => e.preventDefault()}
      />
      {!hasFrame && status !== "closed" && (
        <div className="absolute inset-0 flex items-center justify-center pointer-events-none">
          <span data-testid="cdp-spinner" className="animate-spin text-neutral-400">◌</span>
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
            onClick={() => window.open(url, "_blank", "noopener")}
          >
            Open externally ↗
          </button>
        </div>
      )}
    </div>
  );
}
