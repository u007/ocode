// Wire-format types for the per-stateKey CDP WebSocket (Part 05/06 contract).
// Server→client: binary frames [u32 BE width][u32 BE height]+JPEG and JSON
// telemetry; client→server: JSON commands. Keep in sync with
// internal/browse/cdpsocket.go and docs/superpowers/specs/
// 2026-08-31-browser-chrome-cdp-design.md § Transport.

/** Client→server command union. Exactly one message per JSON object. */
export type CdpClientMessage =
  | CdpRequest
  | { t: "nav"; url: string }
  | { t: "back" }
  | { t: "forward" }
  | { t: "reload" }
  | { t: "perfStart" }
  | { t: "perfStop" }
  /** Restore a persisted vertical scroll offset (CSS px). Best-effort. */
  | { t: "scrollTo"; y: number }
  /** Ask the server for the page's live scroll offset (replies {t:"scroll"}). */
  | { t: "getScroll" }
  | { t: "resize"; w: number; h: number; dpr: number }
  /** User dismissed the file picker opened for a {t:"fileChooser"}. */
  | { t: "fileChooserCancel" }
  | { t: "getResponseBody"; requestId: string }
  /** Insert text at the page caret (Input.insertText): host-clipboard paste
   *  and IME composition commits. */
  | { t: "insertText"; text: string }
  /** Ask for the page's selected text (replies {t:"selection"}) — copy bridge. */
  | { t: "getSelection" }
  /** Find-in-page step on the remote page (replies {t:"findResult"}). The
   *  headless screencast has no native find UI, so the SPA renders its own
   *  find bar. forward defaults to true; caseSensitive defaults to false. */
  | { t: "find"; query: string; backwards?: boolean; caseSensitive?: boolean }
  /** Clear the remote find selection/state (no reply). */
  | { t: "findClose" }
  /** Page zoom factor (1 = 100%), applied like browser zoom. */
  | { t: "zoom"; factor: number }
  /** Touch contacts that changed (Input.dispatchTouchEvent). */
  | { t: "touch"; kind: "start" | "move" | "end" | "cancel"; points: { id: number; x: number; y: number }[]; modifiers?: number }
  | {
      t: "mouse";
      kind: "move" | "down" | "up" | "wheel";
      x: number;
      y: number;
      button?: string;
      /** CDP bitmask of buttons held: left=1, right=2, middle=4. Drives
       *  drag/selection on move events. */
      buttons?: number;
      clickCount?: number;
      deltaX?: number;
      deltaY?: number;
      modifiers?: number;
    }
  | {
      t: "key";
      kind: "down" | "up" | "char";
      key?: string;
      code?: string;
      text?: string;
      modifiers?: number;
      autoRepeat?: boolean;
    };

/** CDP request messages (client→server) for DOM.getNodeForLocation /
 *  DOM.describeNode. These reuse the string requestId correlation. */
export type CdpRequest =
  | { t: "cdp_request"; method: "DOM.getNodeForLocation"; params: { x: number; y: number }; requestId: string }
  | { t: "cdp_request"; method: "DOM.describeNode"; params: { nodeId: number; depth?: number; pierce?: boolean }; requestId: string };

/** CDP response messages (server→client) for the above requests. */
type CdpNodePayload = {
  nodeId: number;
  backendNodeId?: number;
  frameId?: string;
  nodeName?: string;
  nodeValue?: string;
  attributes?: Record<string, string> | string[];
  contentDocument?: { nodeId: number };
};

export type CdpResponse =
  | { t: "cdp_response"; requestId: string; result: CdpNodePayload | { node: CdpNodePayload }; error?: never }
  | { t: "cdp_response"; requestId: string; result?: never; error: string };

/** Server→client JSON telemetry (binary frames are handled separately). */
export type CdpServerMessage =
  | CdpResponse
  | { t: "console"; level: string; args: string[]; ts: number }
  | {
      t: "network";
      requestId: string;
      method: string;
      url: string;
      status: number;
      durationMs: number;
      ts: number;
      size: number;
      blocked?: string;
      contentType?: string;
      requestHeaders?: Record<string, string>;
      responseHeaders?: Record<string, string>;
      postData?: string;
    }
  /** The page opened an <input type=file>; the SPA must show a native picker
   *  and POST the chosen files to /api/browse/upload. */
  | { t: "fileChooser"; multiple: boolean }
  /** Reply to a client "getSelection" command. */
  | { t: "selection"; text: string }
  /** Reply to a client "getResponseBody" command. */
  | {
      t: "responseBody";
      requestId: string;
      body?: string;
      base64Encoded?: boolean;
      truncated?: boolean;
      error?: string;
    }
  | { t: "performance"; metrics: Record<string, number> }
  /** Reply to a client "getScroll" command. */
  | { t: "scroll"; y: number }
  /** Authoritative perf-recording state: sent on every socket attach and as
   *  the ack to perfStart/perfStop. `error` carries a failed toggle's reason
   *  while `recording` stays the authoritative backend value. */
  | { t: "perfState"; recording: boolean; error?: string }
  /** Reply to a client "find" command. active is 1-based ("3 of 10"), 0 when
   *  nothing matches; total is best-effort and may be 0 when counting fails. */
  | { t: "findResult"; query?: string; found: boolean; active: number; total: number; error?: string }
  | { t: "error"; message: string };

/** Decoded screencast frame header: CSS-pixel dimensions of the JPEG body. */
export interface CdpFrameHeader {
  width: number;
  height: number;
}

/** Splits a binary WS payload into its 8-byte big-endian header + JPEG body.
 *  Returns null for frames shorter than the header (protocol violation). */
export function decodeFrame(data: ArrayBuffer): { header: CdpFrameHeader; jpeg: Uint8Array<ArrayBuffer> } | null {
  if (data.byteLength < 8) return null;
  const view = new DataView(data);
  return {
    header: { width: view.getUint32(0), height: view.getUint32(4) },
    jpeg: new Uint8Array(data, 8),
  };
}
