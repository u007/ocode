import { useCallback, useEffect, useRef, useState } from "react";
import { mintBrowseGrant } from "../../api/client";
import type { CdpClientMessage, CdpServerMessage } from "./cdpProtocol";
import { decodeFrame } from "./cdpProtocol";
import { browserActions, type StateKey } from "../../lib/browserStore";

/** Result of DOM.getNodeForLocation via the cdp_request wire path. */
export interface NodeResult {
  nodeId: number;
  backendNodeId?: number;
  frameId?: string;
  href?: string;
  src?: string;
  error?: string;
}

export interface NodeDescription {
  nodeId: number;
  backendNodeId?: number;
  nodeName?: string;
  nodeValue?: string;
  attributes?: Record<string, string> | string[];
  frameId?: string;
  contentDocument?: { nodeId: number };
}

/** A pending cdp_request awaiting a cdp_response. */
type PendingRequest = {
  resolve: (result: unknown) => void;
  reject: (err: Error) => void;
  timer: ReturnType<typeof setTimeout>;
  socket: WebSocket;
};

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
  /** Subscribe to file-chooser requests from the page. Returns an unsubscribe fn. */
  onFileChooser(cb: (multiple: boolean) => void): () => void;
  /** Subscribe to "selection" replies (copy bridge). Returns an unsubscribe fn. */
  onSelection(cb: (text: string) => void): () => void;
  /** Subscribe to "findResult" replies (find-in-page bar). Returns an unsubscribe fn. */
  onFindResult(cb: (res: { query?: string; found: boolean; active: number; total: number }) => void): () => void;
  /** Ask the page for the deepest node at (x,y). Resolves with node info; rejects with timeout/disconnected/error. */
  getNodeAt(x: number, y: number): Promise<NodeResult>;
  /** Describe a node returned by getNodeAt. */
  describeNode(nodeId: number): Promise<NodeDescription>;
}

// Reconnect backoff sequence; caps at the last value.
const BACKOFF_MS = [500, 1000, 2000, 5000];

/** One WebSocket per stateKey carrying screencast frames, telemetry, and
 *  input (Part 05 wire format). Grants are minted per attempt — the server
 *  redeems them one-time on the WS URL. A close without a prior
 *  {"t":"error"} is treated as transient and retried with backoff; an error
 *  message is fatal (chrome missing / unsupported / replaced). */
export function useCdpSocket(
  stateKey: StateKey,
  browseBase: string | null,
  enabled: boolean,
  acceptFrames = true,
): CdpSocketApi {
  const [status, setStatus] = useState<CdpSocketStatus>("connecting");
  const [error, setError] = useState<string | null>(null);

  const wsRef = useRef<WebSocket | null>(null);
  const queueRef = useRef<CdpClientMessage[]>([]);
  const frameCbsRef = useRef(new Set<(bitmap: ImageBitmap, w: number, h: number) => void>());
  const fileChooserCbsRef = useRef(new Set<(multiple: boolean) => void>());
  const selectionCbsRef = useRef(new Set<(text: string) => void>());
  const findResultCbsRef = useRef(new Set<(res: { query?: string; found: boolean; active: number; total: number }) => void>());
  // Pending cdp_request → resolver. Tracked so ws close / stale responses
  // can reject or ignore them instead of leaking or double-resolving.
  const pendingRef = useRef<Map<string, PendingRequest>>(new Map());
  const requestSeqRef = useRef(0);
  // Serializes async JPEG decodes so onFrame fires in wire order.
  const decodeChainRef = useRef<Promise<void>>(Promise.resolve());
  const attemptRef = useRef(0);
  const connectionGenerationRef = useRef(0);
  const timerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const disposedRef = useRef(false);
  // Latest rendered values for the async connect loop (avoids stale reads).
  const cfgRef = useRef({ stateKey, browseBase, enabled, acceptFrames });
  cfgRef.current = { stateKey, browseBase, enabled, acceptFrames };

  const rejectPending = useCallback((err: Error, socket?: WebSocket) => {
    for (const [requestId, pending] of pendingRef.current) {
      if (socket && pending.socket !== socket) continue;
      clearTimeout(pending.timer);
      pendingRef.current.delete(requestId);
      pending.reject(err);
    }
  }, []);

  useEffect(() => {
    const onBeforeUnload = () => rejectPending(new Error("disconnected"));
    window.addEventListener("beforeunload", onBeforeUnload);
    return () => window.removeEventListener("beforeunload", onBeforeUnload);
  }, [rejectPending]);

  const connect = useCallback(() => {
    const { stateKey: key, browseBase: base, enabled: on } = cfgRef.current;
    if (disposedRef.current || !on || !base) return;
    const generation = connectionGenerationRef.current;
    setStatus("connecting");
    const grantPromise = mintBrowseGrant(key);
    grantPromise
      .then((grant) => {
        if (disposedRef.current || generation !== connectionGenerationRef.current || !cfgRef.current.enabled) return;
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
            // Keep the socket and Chrome target alive for background surfaces,
            // but avoid decoding screenshots that cannot be displayed.
            if (!cfgRef.current.acceptFrames) return;
            const decoded = decodeFrame(ev.data);
            if (!decoded) return; // malformed: smaller than the 8-byte header
            const { width, height } = decoded.header;
            // JPEG bytes → ImageBitmap (drawImage rejects raw bytes). Chain
            // decodes so frames reach callbacks in wire order.
            decodeChainRef.current = decodeChainRef.current
              .then(() => createImageBitmap(new Blob([decoded.jpeg], { type: "image/jpeg" })))
              .then(
                (bitmap) => {
                  if (wsRef.current !== ws || !cfgRef.current.acceptFrames) {
                    bitmap.close();
                    return; // socket superseded or surface backgrounded while decoding
                  }
                  const callbacks = [...frameCbsRef.current];
                  if (callbacks.length === 0) {
                    bitmap.close();
                    return;
                  }
                  for (const cb of callbacks) cb(bitmap, width, height);
                },
                (err: unknown) => {
                  console.error("cdp: failed to decode screencast frame", { width, height, bytes: decoded.jpeg.byteLength }, err);
                },
              );
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
                requestId: msg.requestId ?? "",
                method: msg.method,
                url: msg.url,
                status: msg.status,
                durationMs: msg.durationMs,
                ts: msg.ts,
                blocked: msg.blocked,
                contentType: msg.contentType,
                size: msg.size,
                requestHeaders: msg.requestHeaders,
                responseHeaders: msg.responseHeaders,
                postData: msg.postData,
              });
              break;
			case "fileChooser":
				for (const cb of fileChooserCbsRef.current) cb(msg.multiple);
				break;
			case "selection":
				for (const cb of selectionCbsRef.current) cb(msg.text);
				break;
			case "findResult": {
				const m = msg as { query?: string; found: boolean; active: number; total: number };
				for (const cb of findResultCbsRef.current) cb({ query: m.query, found: !!m.found, active: m.active ?? 0, total: m.total ?? 0 });
				break;
			}
			case "responseBody":
				// Store response body for the requesting row.
				browserActions.setResponseBody(key, msg.requestId, {
					body: msg.body ?? "",
					base64Encoded: msg.base64Encoded ?? false,
					truncated: msg.truncated ?? false,
					error: msg.error,
				});
				break;
			case "performance":
				browserActions.setPerformanceMetrics(key, msg.metrics ?? {});
				break;
			case "scroll":
				if (typeof (msg as { y?: unknown }).y === "number") {
					browserActions.setScrollY(key, (msg as { y: number }).y);
				}
				break;
			case "perfState":
				browserActions.setPerfRecording(key, msg.recording);
				if (msg.error) console.error("browse: perf toggle failed:", msg.error);
				break;
			case "error":
				// Fatal: chrome missing/unsupported/replaced. No reconnect.
				setError(msg.message);
				setStatus("closed");
				wsRef.current = null;
				ws.close();
				break;
            case "cdp_response": {
              // Reply to a getNodeAt request (DOM.getNodeForLocation / DOM.describeNode).
              const pending = pendingRef.current.get(msg.requestId);
              if (!pending) return; // already resolved/timeout/closed
              pendingRef.current.delete(msg.requestId);
              clearTimeout(pending.timer);
              if (msg.error) {
                pending.reject(new Error(msg.error));
              } else {
                pending.resolve(msg.result as NodeResult);
              }
              break;
            }
          }
        };

        ws.onclose = () => {
          const current = wsRef.current === ws;
          if (current) wsRef.current = null;
          // Reject requests even when this socket was superseded or closed by
          // effect cleanup; otherwise callers can wait forever for a response.
          rejectPending(new Error("disconnected"), ws);
          if (!current) return; // superseded socket
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
        if (disposedRef.current || generation !== connectionGenerationRef.current || !cfgRef.current.enabled) return;
        setStatus("reconnecting");
        setError(err instanceof Error ? err.message : String(err));
        scheduleReconnect();
      });
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [rejectPending]);

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
    connectionGenerationRef.current += 1;
    if (enabled && browseBase) {
      attemptRef.current = 0;
      connect();
    } else {
      setStatus("closed");
      rejectPending(new Error("disconnected"));
      // Dropping the connection also stops the server-side screencast via
      // Detach on socket close (Part 05).
      wsRef.current?.close();
      wsRef.current = null;
    }
    return () => {
      disposedRef.current = true;
      rejectPending(new Error("disconnected"));
      if (timerRef.current) {
        clearTimeout(timerRef.current);
        timerRef.current = null;
      }
      const ws = wsRef.current;
      wsRef.current = null; // make onclose a no-op before it fires
      ws?.close();
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [stateKey, browseBase, enabled, rejectPending]);

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

  const onFileChooser = useCallback((cb: (multiple: boolean) => void) => {
    fileChooserCbsRef.current.add(cb);
    return () => {
      fileChooserCbsRef.current.delete(cb);
    };
  }, []);

  const onFindResult = useCallback((cb: (res: { query?: string; found: boolean; active: number; total: number }) => void) => {
    findResultCbsRef.current.add(cb);
    return () => {
      findResultCbsRef.current.delete(cb);
    };
  }, []);

  const onSelection = useCallback((cb: (text: string) => void) => {
    selectionCbsRef.current.add(cb);
    return () => {
      selectionCbsRef.current.delete(cb);
    };
  }, []);
  /** Send one context-menu CDP request, correlated by string requestId with
   *  a 5s timeout. All terminal paths remove the pending entry and timer. */
  const requestCdp = useCallback(
    (method: "DOM.getNodeForLocation" | "DOM.describeNode", params: Record<string, unknown>): Promise<unknown> => {
      return new Promise((resolve, reject) => {
        const ws = wsRef.current;
        if (!ws || ws.readyState !== WebSocket.OPEN) {
          reject(new Error("disconnected"));
          return;
        }
        const requestId = `node:${Date.now()}:${requestSeqRef.current++}`;
        // 5s timeout — matches the server-side context in cdpsocket.go.
        const timer = setTimeout(() => {
          const pending = pendingRef.current.get(requestId);
          if (!pending) return;
          pendingRef.current.delete(requestId);
          reject(new Error("timeout"));
        }, 5000);
        const pending: PendingRequest = { resolve, reject, timer, socket: ws };
        pendingRef.current.set(requestId, pending);

        const msg = {
          t: "cdp_request" as const,
          method,
          params,
          requestId,
        };

        try {
          ws.send(JSON.stringify(msg));
        } catch (err) {
          clearTimeout(timer);
          pendingRef.current.delete(requestId);
          reject(err instanceof Error ? err : new Error(String(err)));
        }
      });
    },
    [],
  );

  const getNodeAt = useCallback(
    (x: number, y: number) => requestCdp("DOM.getNodeForLocation", { x, y }).then((result) => {
      const location = result as Partial<NodeResult> | undefined;
      if (typeof location?.nodeId !== "number") throw new Error("no_node");
      return location as NodeResult;
    }),
    [requestCdp],
  );

  const describeNode = useCallback(
    (nodeId: number) => requestCdp("DOM.describeNode", { nodeId, depth: 0, pierce: false }).then((result) => {
      const response = result as { node?: NodeDescription } | NodeDescription;
      return ("node" in response && response.node ? response.node : response) as NodeDescription;
    }),
    [requestCdp],
  );

  return { send, status, error, onFrame, onFileChooser, onSelection, onFindResult, getNodeAt, describeNode };
}
