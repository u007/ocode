// Wire-format types for the per-stateKey CDP WebSocket (Part 05/06 contract).
// Server→client: binary frames [u32 BE width][u32 BE height]+JPEG and JSON
// telemetry; client→server: JSON commands. Keep in sync with
// internal/browse/cdpsocket.go and docs/superpowers/specs/
// 2026-08-31-browser-chrome-cdp-design.md § Transport.

/** Client→server command union. Exactly one message per JSON object. */
export type CdpClientMessage =
  | { t: "nav"; url: string }
  | { t: "back" }
  | { t: "forward" }
  | { t: "reload" }
  | { t: "resize"; w: number; h: number; dpr: number }
  | {
      t: "mouse";
      kind: "move" | "down" | "up" | "wheel";
      x: number;
      y: number;
      button?: string;
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
    };

/** Server→client JSON telemetry (binary frames are handled separately). */
export type CdpServerMessage =
  | { t: "console"; level: string; args: string[]; ts: number }
  | {
      t: "network";
      method: string;
      url: string;
      status: number;
      durationMs: number;
      ts: number;
      blocked?: string;
    }
  | { t: "error"; message: string };

/** Decoded screencast frame header: CSS-pixel dimensions of the JPEG body. */
export interface CdpFrameHeader {
  width: number;
  height: number;
}

/** Splits a binary WS payload into its 8-byte big-endian header + JPEG body.
 *  Returns null for frames shorter than the header (protocol violation). */
export function decodeFrame(data: ArrayBuffer): { header: CdpFrameHeader; jpeg: Uint8Array } | null {
  if (data.byteLength < 8) return null;
  const view = new DataView(data);
  return {
    header: { width: view.getUint32(0), height: view.getUint32(4) },
    jpeg: new Uint8Array(data, 8),
  };
}
