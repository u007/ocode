import { useCallback, useEffect, useRef, useState } from "react";
import { mintBrowseGrant } from "../../api/client";
import type { CdpClientMessage, CdpServerMessage } from "./cdpProtocol";
import { decodeFrame } from "./cdpProtocol";
import { browserActions, type StateKey } from "../../lib/browserStore";

/** Connection lifecycle: connecting = dialing/redeeming grant; open = live;
 *  reconnecting = closed without a fatal error, retry scheduled; closed =
 *  fatal (server sent {"t":"error"}) or disabled. */
export type CdpSocketStatus = "connecting" | "open" | "reconnecting" | "closed";

export interface CdpSocketApi {
  send(msg: CdpClientMessage): void;
  status: CdpSocketStatus;
  error: string | null;
  /** Subscribe to decoded screencast frames. Returns an unsubscribe fn. */
  onFrame(cb: (bitmap: ImageBitmap, w: number, h: number) => void): () => void;
}

// Reconnect backoff sequence; caps at the last value.
const BACKOFF_MS = [500, 1000, 2000, 5000];

/** One WebSocket per stateKey carrying screencast frames, telemetry, and
 *  input (Part 05 wire format). Grants are minted per attempt — the server
 *  redeems them one-time on the WS URL. A close without a prior
 *  {"t":"error"} is treated as transient and retried with backoff; an error
 *  message is fatal (chrome missing / unsupported / replaced). */
export function useCdpSocket(stateKey: StateKey, browseBase: string | null, enabled: boolean): CdpSocketApi {
  const [status, setStatus] = useState<CdpSocketStatus>("connecting");
  const [error, setError] = useState<string | null>(null);

  const wsRef = useRef<WebSocket | null>(null);
  const queueRef = useRef<CdpClientMessage[]>([]);
  const frameCbsRef = useRef(new Set<(bitmap: ImageBitmap, w: number, h: number) => void>());
  const attemptRef = useRef(0);
  const timerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const disposedRef = useRef(false);
  // Latest rendered values for the async connect loop (avoids stale reads).
  const cfgRef = useRef({ stateKey, browseBase, enabled });
  cfgRef.current = { stateKey, browseBase, enabled };

  const connect = useCallback(() => {
    const { stateKey: key, browseBase: base, enabled: on } = cfgRef.current;
    if (disposedRef.current || !on || !base) return;
    setStatus("connecting");
    const grantPromise = mintBrowseGrant(key);
    grantPromise
      .then((grant) => {
        if (disposedRef.current || !cfgRef.current.enabled) return;
        const wsUrl =
          base.replace(/^http/, "ws") +
          "/b/" +
          encodeURIComponent(key) +
          "/__cdp?__grant=" +
          encodeURIComponent(grant);
        const ws = new WebSocket(wsUrl);
        ws.binaryType = "arraybuffer";
        wsRef.current = ws;

        ws.onopen = () => {
          if (wsRef.current !== ws) return;
          attemptRef.current = 0;
          setStatus("open");
          // Flush everything queued while CONNECTING, in order.
          const q = queueRef.current;
          queueRef.current = [];
          for (const msg of q) ws.send(JSON.stringify(msg));
        };

        ws.onmessage = (ev: MessageEvent) => {
          if (wsRef.current !== ws) return;
          if (ev.data instanceof ArrayBuffer) {
            const decoded = decodeFrame(ev.data);
            if (!decoded) return; // malformed: smaller than the 8-byte header
            for (const cb of frameCbsRef.current) cb(decoded.jpeg as unknown as ImageBitmap, decoded.header.width, decoded.header.height);
            return;
          }
          if (typeof ev.data !== "string") return;
          let msg: CdpServerMessage;
          try {
            msg = JSON.parse(ev.data) as CdpServerMessage;
          } catch {
            return; // malformed JSON: ignore
          }
          switch (msg.t) {
            case "console":
              browserActions.pushConsole(key, { level: msg.level, text: msg.args.join(" "), ts: msg.ts });
              break;
            case "network":
              browserActions.pushNetwork(key, {
                method: msg.method,
                url: msg.url,
                status: msg.status,
                durationMs: msg.durationMs,
                ts: msg.ts,
              });
              break;
            case "error":
              // Fatal: chrome missing/unsupported/replaced. No reconnect.
              setError(msg.message);
              setStatus("closed");
              wsRef.current = null;
              ws.close();
              break;
          }
        };

        ws.onclose = () => {
          if (wsRef.current !== ws) return; // superseded or intentionally closed
          wsRef.current = null;
          if (disposedRef.current || !cfgRef.current.enabled) return;
          setStatus("reconnecting");
          scheduleReconnect();
        };
        ws.onerror = () => {
          // onclose always follows onerror; nothing to do here.
        };
      })
      .catch((err) => {
        // Grant mint failed (main server down?) — retry with backoff.
        if (disposedRef.current || !cfgRef.current.enabled) return;
        setStatus("reconnecting");
        setError(err instanceof Error ? err.message : String(err));
        scheduleReconnect();
      });
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const scheduleReconnect = useCallback(() => {
    const delay = BACKOFF_MS[Math.min(attemptRef.current, BACKOFF_MS.length - 1)];
    attemptRef.current += 1;
    timerRef.current = setTimeout(() => {
      timerRef.current = null;
      connect();
    }, delay);
  }, [connect]);

  useEffect(() => {
    disposedRef.current = false;
    if (enabled && browseBase) {
      attemptRef.current = 0;
      connect();
    } else {
      setStatus("closed");
      // Dropping the connection also stops the server-side screencast via
      // Detach on socket close (Part 05).
      wsRef.current?.close();
      wsRef.current = null;
    }
    return () => {
      disposedRef.current = true;
      if (timerRef.current) {
        clearTimeout(timerRef.current);
        timerRef.current = null;
      }
      const ws = wsRef.current;
      wsRef.current = null; // make onclose a no-op before it fires
      ws?.close();
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [stateKey, browseBase, enabled]);

  const send = useCallback((msg: CdpClientMessage) => {
    const ws = wsRef.current;
    if (ws && ws.readyState === WebSocket.OPEN) {
      ws.send(JSON.stringify(msg));
      return;
    }
    // CONNECTING (or brief race): queue; flushed in order on open.
    queueRef.current.push(msg);
  }, []);

  const onFrame = useCallback((cb: (bitmap: ImageBitmap, w: number, h: number) => void) => {
    frameCbsRef.current.add(cb);
    return () => {
      frameCbsRef.current.delete(cb);
    };
  }, []);

  return { send, status, error, onFrame };
}
