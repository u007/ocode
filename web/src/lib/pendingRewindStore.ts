// Durable client-side recovery state for a server-armed session rewind.
// This is intentionally separate from shared ocode config: it contains a
// bearer-like opaque token and transient drafts, is scoped to one Web/Desktop
// origin, and is never synchronized through ~/.config or ~/.local/share.

export const PENDING_REWIND_STORAGE_PREFIX = "ocode.ui.pendingRewind.v1:";
const PENDING_REWIND_CHANGE_EVENT = "ocode:pending-rewind-change";

const MAX_TOKEN_LENGTH = 256;
const MAX_HOST_LENGTH = 512;
const MAX_SESSION_ID_LENGTH = 256;
const MAX_DRAFT_LENGTH = 1_000_000;
export const PENDING_REWIND_TARGET_PREVIEW_MAX = 500;

export interface PendingRewindRecord {
  version: 1;
  /** Empty string denotes the local server. */
  host: string;
  sessionId: string;
  token: string;
  draft: string;
  previousDraft: string;
  targetPreview: string;
  expiresAt: string;
}

export type PendingRewindChange = (
  current: PendingRewindRecord | null,
  previous: PendingRewindRecord | null,
) => void;

export function pendingRewindStorageKey(host: string | undefined, sessionId: string): string {
  return `${PENDING_REWIND_STORAGE_PREFIX}${encodeURIComponent(host ?? "")}:${encodeURIComponent(sessionId)}`;
}

function validBoundedString(value: unknown, max: number, allowEmpty = false): value is string {
  return typeof value === "string" && value.length <= max && (allowEmpty || value.length > 0);
}

function parseRecord(
  raw: string | null,
  expectedHost: string,
  expectedSessionId: string,
): PendingRewindRecord | null {
  if (!raw) return null;
  try {
    const value: unknown = JSON.parse(raw);
    if (!value || typeof value !== "object" || Array.isArray(value)) return null;
    const record = value as Record<string, unknown>;
    if (record.version !== 1) return null;
    if (record.host !== expectedHost || record.sessionId !== expectedSessionId) return null;
    if (!validBoundedString(record.host, MAX_HOST_LENGTH, true)) return null;
    if (!validBoundedString(record.sessionId, MAX_SESSION_ID_LENGTH)) return null;
    if (!validBoundedString(record.token, MAX_TOKEN_LENGTH)) return null;
    if (!validBoundedString(record.draft, MAX_DRAFT_LENGTH, true)) return null;
    if (!validBoundedString(record.previousDraft, MAX_DRAFT_LENGTH, true)) return null;
    if (!validBoundedString(record.targetPreview, PENDING_REWIND_TARGET_PREVIEW_MAX, true)) return null;
    if (!validBoundedString(record.expiresAt, 64)) return null;
    if (!Number.isFinite(Date.parse(record.expiresAt))) return null;
    return {
      version: 1,
      host: record.host,
      sessionId: record.sessionId,
      token: record.token,
      draft: record.draft,
      previousDraft: record.previousDraft,
      targetPreview: record.targetPreview,
      expiresAt: record.expiresAt,
    };
  } catch (err) {
    console.error("Failed to parse pending rewind record:", err);
    return null;
  }
}

export function loadPendingRewind(
  host: string | undefined,
  sessionId: string,
): PendingRewindRecord | null {
  const expectedHost = host ?? "";
  try {
    return parseRecord(
      window.localStorage.getItem(pendingRewindStorageKey(expectedHost, sessionId)),
      expectedHost,
      sessionId,
    );
  } catch (err) {
    console.error("Failed to load pending rewind record:", err);
    return null;
  }
}

function dispatchChange(
  host: string,
  sessionId: string,
  current: PendingRewindRecord | null,
  previous: PendingRewindRecord | null,
): void {
  window.dispatchEvent(new CustomEvent(PENDING_REWIND_CHANGE_EVENT, {
    detail: { host, sessionId, current, previous },
  }));
}

export function savePendingRewind(record: PendingRewindRecord): boolean {
  const serialized = JSON.stringify(record);
  if (!parseRecord(serialized, record.host, record.sessionId)) {
    console.error("Refusing to persist invalid pending rewind record");
    return false;
  }
  try {
    window.localStorage.setItem(pendingRewindStorageKey(record.host, record.sessionId), serialized);
  } catch (err) {
    console.error("Failed to persist pending rewind record:", err);
    return false;
  }
  dispatchChange(record.host, record.sessionId, record, null);
  return true;
}

export function clearPendingRewind(host: string | undefined, sessionId: string): boolean {
  const normalizedHost = host ?? "";
  const previous = loadPendingRewind(normalizedHost, sessionId);
  try {
    window.localStorage.removeItem(pendingRewindStorageKey(normalizedHost, sessionId));
  } catch (err) {
    console.error("Failed to clear pending rewind record:", err);
    return false;
  }
  dispatchChange(normalizedHost, sessionId, null, previous);
  return true;
}

/** Subscribe to same-document writes and cross-document storage changes.
 * Storage events are hints only: the callback always receives a fresh read of
 * the keyed record, never an unvalidated event payload. */
export function subscribePendingRewind(
  host: string | undefined,
  sessionId: string,
  onChange: PendingRewindChange,
): () => void {
  const normalizedHost = host ?? "";
  const key = pendingRewindStorageKey(normalizedHost, sessionId);
  const handleStorage = (event: StorageEvent) => {
    if (event.key !== key) return;
    const previous = parseRecord(event.oldValue, normalizedHost, sessionId);
    onChange(loadPendingRewind(normalizedHost, sessionId), previous);
  };
  const handleLocal = (event: Event) => {
    const detail = (event as CustomEvent<{
      host?: string;
      sessionId?: string;
      current?: PendingRewindRecord | null;
      previous?: PendingRewindRecord | null;
    }>).detail;
    if (!detail || detail.host !== normalizedHost || detail.sessionId !== sessionId) return;
    onChange(detail.current ?? null, detail.previous ?? null);
  };
  window.addEventListener("storage", handleStorage);
  window.addEventListener(PENDING_REWIND_CHANGE_EVENT, handleLocal);
  return () => {
    window.removeEventListener("storage", handleStorage);
    window.removeEventListener(PENDING_REWIND_CHANGE_EVENT, handleLocal);
  };
}

export function rekeyPendingRewind(
  oldHost: string | undefined,
  oldSessionId: string,
  newHost: string | undefined,
  newSessionId: string,
): boolean {
  const previous = loadPendingRewind(oldHost, oldSessionId);
  if (!previous) return true;
  if (oldHost === newHost && oldSessionId === newSessionId) return true;
  const next: PendingRewindRecord = {
    ...previous,
    host: newHost ?? "",
    sessionId: newSessionId,
  };
  if (!savePendingRewind(next)) return false;
  return clearPendingRewind(oldHost, oldSessionId);
}

export function __resetPendingRewindForTests(): void {
  try {
    const keys: string[] = [];
    for (let i = 0; i < window.localStorage.length; i++) {
      const key = window.localStorage.key(i);
      if (key?.startsWith(PENDING_REWIND_STORAGE_PREFIX)) keys.push(key);
    }
    for (const key of keys) window.localStorage.removeItem(key);
  } catch (err) {
    console.error("Failed to reset pending rewind records:", err);
  }
}
