import { act, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import SyncStatusWidget from "./SyncStatusWidget";
import type { SyncStatusResponse } from "../../api/types";

const mockGetSyncStatus = vi.fn();
const mockSyncNow = vi.fn();
const mockSyncLogout = vi.fn();
const mockSyncLoginStart = vi.fn();
const mockSyncLoginPoll = vi.fn();
const externalOpen = vi.hoisted(() => vi.fn());
const requestURL = vi.hoisted(() => vi.fn());
vi.mock("../../lib/externalLinks", async (importOriginal) => {
  const actual = await importOriginal<typeof import("../../lib/externalLinks")>();
  return { ...actual, openExternalURL: externalOpen, requestURLOpen: requestURL };
});
vi.mock("../../api/client", () => ({
  api: {
    getSyncStatus: (...a: unknown[]) => mockGetSyncStatus(...a),
    syncNow: (...a: unknown[]) => mockSyncNow(...a),
    syncLogout: (...a: unknown[]) => mockSyncLogout(...a),
    syncLoginStart: (...a: unknown[]) => mockSyncLoginStart(...a),
    syncLoginPoll: (...a: unknown[]) => mockSyncLoginPoll(...a),
  },
}));

function status(configSynced: boolean): SyncStatusResponse {
  return {
    loggedIn: true,
    config: { version: 1, syncedAt: "2026-11-23T00:00:00Z", synced: configSynced },
    auth: { version: 1, syncedAt: "2026-11-23T00:00:00Z", synced: true },
  };
}

// Advance fake time and let the promises that fire in that window settle.
async function advance(ms: number) {
  await act(async () => {
    await vi.advanceTimersByTimeAsync(ms);
  });
}

beforeEach(() => {
  vi.useFakeTimers();
  vi.spyOn(console, "error").mockImplementation(() => {});
  mockGetSyncStatus.mockReset();
  mockSyncNow.mockReset();
  mockSyncLogout.mockReset();
  mockSyncLoginStart.mockReset();
  mockSyncLoginPoll.mockReset();
  externalOpen.mockReset();
  requestURL.mockReset();
});

afterEach(() => {
  vi.useRealTimers();
  vi.restoreAllMocks();
});

describe("SyncStatusWidget", () => {
  it("retries a failed auto-sync on the poll tick once the backoff has passed", async () => {
    mockGetSyncStatus.mockResolvedValue(status(false));
    mockSyncNow.mockRejectedValue(new Error("hub unreachable"));
    render(<SyncStatusWidget />);
    // Commit the mount's status read so the 800 ms initial auto-sync is scheduled.
    await advance(0);

    // The 800 ms initial auto-sync fires and fails.
    await advance(800);
    expect(mockSyncNow).toHaveBeenCalledTimes(1);

    // The 30 s tick is still inside the 30 s backoff window: no retry yet.
    await advance(30000 - 800);
    expect(mockSyncNow).toHaveBeenCalledTimes(1);

    // The 60 s tick is past the window: the widget retries.
    await advance(30000);
    expect(mockSyncNow).toHaveBeenCalledTimes(2);
  });

  it("keeps the status poll running after Log Out", async () => {
    mockGetSyncStatus.mockResolvedValue(status(true));
    mockSyncLogout.mockResolvedValue(undefined);
    render(<SyncStatusWidget />);
    await advance(0);

    // The status button is the only trigger; it is found before the dialog opens.
    fireEvent.click(screen.getByTitle("Config sync active"));
    fireEvent.click(screen.getByRole("button", { name: "Log Out" }));
    // Flush the logout and the refreshStatus it runs, then snapshot the count.
    await advance(0);
    const callsBefore = mockGetSyncStatus.mock.calls.length;

    await advance(30000);
    expect(mockGetSyncStatus.mock.calls.length).toBeGreaterThan(callsBefore);
  });

  it("keeps automatic OAuth opening direct but asks before a verification-link click", async () => {
    mockGetSyncStatus.mockResolvedValue({ ...status(false), loggedIn: false });
    mockSyncLoginStart.mockResolvedValue({
      userCode: "ABCD-1234",
      verifyUrl: "https://example.com/verify",
      deviceCode: "device-1",
    });
    vi.spyOn(window, "open").mockReturnValue(null);
    render(<SyncStatusWidget />);
    await advance(0);

    fireEvent.click(screen.getByTitle("Sync not connected"));
    fireEvent.click(screen.getByRole("button", { name: "Log In" }));
    await advance(0);
    expect(externalOpen).toHaveBeenCalledWith("https://example.com/verify");
    expect(requestURL).not.toHaveBeenCalled();

    fireEvent.click(screen.getByRole("button", { name: "Open verification page" }));
    expect(requestURL).toHaveBeenCalledWith("https://example.com/verify");
    expect(externalOpen).toHaveBeenCalledTimes(1);
  });
});
