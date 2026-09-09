import { apiPath, authHeaders } from "@/api/client";

export interface TerminalHistoryRestoreOptions {
  id: string;
  projectPath: string;
  host?: string;
  pageSize?: number;
  signal?: AbortSignal;
  /** Share decoder state with the following WebSocket byte stream. */
  decoder?: TextDecoder;
  onSnapshotEnd?: (snapshotEnd: number) => void;
  onText?: (text: string) => void | Promise<void>;
}

export interface TerminalHistoryRestoreResult {
  kind: "restored" | "missing";
  snapshotEnd: number;
  state?: "active" | "exited";
}

interface TerminalHistoryPage {
  id: string;
  offset: number;
  next_offset: number;
  snapshot_end: number;
  eof: boolean;
  data: string;
  state: "active" | "exited";
}

export class TerminalHistoryError extends Error {
  readonly status?: number;

  constructor(message: string, status?: number) {
    super(message);
    this.name = "TerminalHistoryError";
    this.status = status;
  }
}

function isSafeInteger(value: unknown): value is number {
  return typeof value === "number" && Number.isSafeInteger(value) && value >= 0;
}

function decodeBase64(value: unknown): Uint8Array {
  if (typeof value !== "string" || value.length % 4 !== 0 || !/^(?:[A-Za-z0-9+/]{4})*(?:[A-Za-z0-9+/]{2}==|[A-Za-z0-9+/]{3}=)?$/.test(value)) {
    throw new TerminalHistoryError("terminal history returned invalid base64 data");
  }
  const binary = atob(value);
  const bytes = new Uint8Array(binary.length);
  for (let i = 0; i < binary.length; i++) bytes[i] = binary.charCodeAt(i);
  return bytes;
}

function parsePage(raw: unknown): TerminalHistoryPage {
  if (!raw || typeof raw !== "object") throw new TerminalHistoryError("terminal history returned an invalid page");
  const page = raw as Partial<TerminalHistoryPage>;
  if (
    typeof page.id !== "string" ||
    !isSafeInteger(page.offset) ||
    !isSafeInteger(page.next_offset) ||
    !isSafeInteger(page.snapshot_end) ||
    typeof page.eof !== "boolean" ||
    (page.state !== "active" && page.state !== "exited")
  ) {
    throw new TerminalHistoryError("terminal history returned invalid range metadata");
  }
  return page as TerminalHistoryPage;
}

/**
 * Replays the server's append-only terminal log from byte zero in bounded
 * pages. The server snapshot cursor is sent back on every later request so a
 * live log can grow without changing the restore boundary. The callback is
 * invoked only after each page has passed all progression and base64 checks.
 */
export async function restoreTerminalHistory({
  id,
  projectPath,
  host,
  pageSize = 64 * 1024,
  signal,
  onSnapshotEnd,
  onText,
  decoder: suppliedDecoder,
}: TerminalHistoryRestoreOptions): Promise<TerminalHistoryRestoreResult> {
  if (!Number.isSafeInteger(pageSize) || pageSize <= 0) {
    throw new TerminalHistoryError("terminal history page size must be positive");
  }

  const decoder = suppliedDecoder ?? new TextDecoder();
  let offset = 0;
  let snapshotEnd: number | undefined;
  let state: "active" | "exited" | undefined;
  let first = true;

  for (;;) {
    const params = new URLSearchParams({ offset: String(offset), limit: String(pageSize) });
    if (snapshotEnd !== undefined) params.set("snapshot_end", String(snapshotEnd));
    if (host) {
      params.set("host", host);
      params.set("project_path", projectPath);
    } else {
      params.set("project", projectPath);
    }
    const response = await fetch(apiPath(`/api/terminal/${encodeURIComponent(id)}/history?${params}`), {
      headers: authHeaders(),
      signal,
    });
    if (response.status === 404) {
      if (!first) throw new TerminalHistoryError("terminal history disappeared during restore", 404);
      return { kind: "missing", snapshotEnd: 0 };
    }
    if (!response.ok) throw new TerminalHistoryError(`terminal history request failed (${response.status})`, response.status);

    const page = parsePage(await response.json());
    if (page.id !== id || page.offset !== offset) {
      throw new TerminalHistoryError("terminal history returned a non-contiguous offset");
    }
    if (first) {
      snapshotEnd = page.snapshot_end;
      state = page.state;
      onSnapshotEnd?.(snapshotEnd);
      first = false;
    } else if (page.snapshot_end !== snapshotEnd) {
      throw new TerminalHistoryError("terminal history snapshot changed during restore");
    }
    const bytes = decodeBase64(page.data);
    if (page.next_offset !== offset + bytes.length || page.next_offset > page.snapshot_end) {
      throw new TerminalHistoryError("terminal history returned invalid byte progression");
    }
    if (page.eof !== (page.next_offset === page.snapshot_end)) {
      throw new TerminalHistoryError("terminal history returned inconsistent eof metadata");
    }
    if (bytes.length > 0) {
      const text = decoder.decode(bytes, { stream: true });
      if (text) await onText?.(text);
    }
    const previousOffset = offset;
    offset = page.next_offset;
    if (offset === previousOffset && offset !== snapshotEnd) {
      throw new TerminalHistoryError("terminal history made no progress");
    }
    if (offset === snapshotEnd) {
      // When the caller supplied the decoder, leave an incomplete UTF-8
      // sequence pending for the first WebSocket frame after history_offset.
      if (!suppliedDecoder) {
        const remainder = decoder.decode();
        if (remainder) await onText?.(remainder);
      }
      return { kind: "restored", snapshotEnd, state };
    }
    if (page.eof) throw new TerminalHistoryError("terminal history ended before snapshot end");
  }
}
