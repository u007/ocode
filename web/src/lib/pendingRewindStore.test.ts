import { beforeEach, describe, expect, it, vi } from "vitest";
import {
  PENDING_REWIND_STORAGE_PREFIX,
  __resetPendingRewindForTests,
  clearPendingRewind,
  loadPendingRewind,
  pendingRewindStorageKey,
  rekeyPendingRewind,
  savePendingRewind,
  subscribePendingRewind,
  type PendingRewindRecord,
} from "./pendingRewindStore";

const HOST = "devbox";
const SESSION = "ses-1";

function armed(overrides: Partial<PendingRewindRecord> = {}): PendingRewindRecord {
  return {
    version: 1,
    host: HOST,
    sessionId: SESSION,
    token: "opaque-token",
    draft: "restored draft",
    previousDraft: "exact prior draft",
    targetPreview: "selected request",
    expiresAt: "2026-09-26T12:00:00.000Z",
    ...overrides,
  };
}

describe("pendingRewindStore", () => {
  beforeEach(() => {
    __resetPendingRewindForTests();
    vi.restoreAllMocks();
  });

  it("keys records by both remote host and session id", () => {
    const local = pendingRewindStorageKey(undefined, SESSION);
    const remoteA = pendingRewindStorageKey("a@host", SESSION);
    const remoteB = pendingRewindStorageKey("b@host", SESSION);
    expect(new Set([local, remoteA, remoteB]).size).toBe(3);
    expect(local.startsWith(PENDING_REWIND_STORAGE_PREFIX)).toBe(true);
  });

  it("persists and reloads the complete versioned record", () => {
    const record = armed();
    expect(savePendingRewind(record)).toBe(true);
    expect(loadPendingRewind(HOST, SESSION)).toEqual(record);
    expect(JSON.parse(localStorage.getItem(pendingRewindStorageKey(HOST, SESSION))!)).toEqual(record);
  });

  it("rejects malformed, mismatched, and unbounded stored data safely", () => {
    const key = pendingRewindStorageKey(HOST, SESSION);
    const invalidValues = [
      "not json",
      JSON.stringify({ ...armed(), version: 2 }),
      JSON.stringify({ ...armed(), token: "" }),
      JSON.stringify({ ...armed(), draft: 42 }),
      JSON.stringify({ ...armed(), expiresAt: "not-a-date" }),
      JSON.stringify({ ...armed(), host: "other-host" }),
      JSON.stringify({ ...armed(), draft: "x".repeat(1_000_001) }),
      JSON.stringify({ ...armed(), targetPreview: "x".repeat(501) }),
    ];
    for (const value of invalidValues) {
      localStorage.setItem(key, value);
      expect(loadPendingRewind(HOST, SESSION)).toBeNull();
    }
  });

  it("reports localStorage write failure instead of pretending the record is durable", () => {
    vi.spyOn(Storage.prototype, "setItem").mockImplementation(() => {
      throw new DOMException("quota exceeded", "QuotaExceededError");
    });
    vi.spyOn(console, "error").mockImplementation(() => {});

    expect(savePendingRewind(armed())).toBe(false);
  });

  it("synchronizes same-document saves and clears with the previous record", () => {
    const seen: Array<{ current: PendingRewindRecord | null; previous: PendingRewindRecord | null }> = [];
    const unsubscribe = subscribePendingRewind(HOST, SESSION, (current, previous) => {
      seen.push({ current, previous });
    });

    const record = armed();
    savePendingRewind(record);
    clearPendingRewind(HOST, SESSION);
    unsubscribe();
    savePendingRewind(record);

    expect(seen).toEqual([
      { current: record, previous: null },
      { current: null, previous: record },
    ]);
  });

  it("re-reads storage events and ignores other host/session records", () => {
    const record = armed();
    const seen: Array<PendingRewindRecord | null> = [];
    const unsubscribe = subscribePendingRewind(HOST, SESSION, (current) => seen.push(current));
    const key = pendingRewindStorageKey(HOST, SESSION);

    localStorage.setItem(key, JSON.stringify(record));
    window.dispatchEvent(new StorageEvent("storage", { key, newValue: JSON.stringify(record) }));
    localStorage.removeItem(key);
    window.dispatchEvent(new StorageEvent("storage", { key, oldValue: JSON.stringify(record), newValue: null }));
    window.dispatchEvent(new StorageEvent("storage", {
      key: pendingRewindStorageKey("other-host", SESSION),
      newValue: JSON.stringify(record),
    }));
    unsubscribe();

    expect(seen).toEqual([record, null]);
    expect(loadPendingRewind(HOST, SESSION)).toBeNull();
  });

  it("rekeys one record without disturbing another session", () => {
    const oldRecord = armed();
    const other = armed({ sessionId: "ses-other", token: "other-token" });
    savePendingRewind(oldRecord);
    savePendingRewind(other);

    expect(rekeyPendingRewind(HOST, SESSION, HOST, "ses-new")).toBe(true);
    expect(loadPendingRewind(HOST, SESSION)).toBeNull();
    expect(loadPendingRewind(HOST, "ses-new")).toEqual({ ...oldRecord, sessionId: "ses-new" });
    expect(loadPendingRewind(HOST, "ses-other")).toEqual(other);
  });

  it("clears only the requested host/session record", () => {
    savePendingRewind(armed());
    savePendingRewind(armed({ sessionId: "ses-other", token: "other-token" }));
    savePendingRewind(armed({ host: "other-host", token: "remote-token" }));

    expect(clearPendingRewind(HOST, SESSION)).toBe(true);
    expect(loadPendingRewind(HOST, SESSION)).toBeNull();
    expect(loadPendingRewind(HOST, "ses-other")).not.toBeNull();
    expect(loadPendingRewind("other-host", SESSION)).not.toBeNull();
  });
});
