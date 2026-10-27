import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { ApiError } from "../api/client";
import {
  __resetRemoteReadGatesForTests,
  isHostUnreachable,
  noteRemoteHostReachable,
  noteRemoteHostUnreachable,
  periodicRemoteRead,
  REMOTE_HOST_RETRY_MS,
  remoteHostRetryInMs,
  withRemoteRead,
} from "./remoteHostGate";

describe("periodicRemoteRead", () => {
  beforeEach(() => {
    __resetRemoteReadGatesForTests();
  });

  it("runs a local read directly, with no gate", async () => {
    const fn = vi.fn(async () => "local");
    await expect(periodicRemoteRead(undefined, "/proj", fn)).resolves.toEqual({ value: "local" });
    await expect(periodicRemoteRead("", "/proj", fn)).resolves.toEqual({ value: "local" });
    expect(fn).toHaveBeenCalledTimes(2);
  });

  it("returns the remote value on success", async () => {
    await expect(periodicRemoteRead("dev@box", "/proj", async () => 7)).resolves.toEqual({ value: 7 });
  });

  it("treats a 503 (still connecting) as a skipped read without opening the retry window", async () => {
    const connecting = vi.fn(async () => {
      throw new ApiError("remote connecting", 503);
    });
    await expect(periodicRemoteRead("dev@box", "/proj", connecting)).resolves.toBeNull();
    expect(remoteHostRetryInMs("dev@box")).toBe(0);

    const next = vi.fn(async () => "fresh");
    await expect(periodicRemoteRead("dev@box", "/other", next)).resolves.toEqual({ value: "fresh" });
  });

  it("skips the host after an unreachable failure instead of asking it again", async () => {
    const failing = vi.fn(async () => {
      throw new ApiError("bad gateway", 502);
    });
    await expect(periodicRemoteRead("dev@box", "/proj", failing)).rejects.toThrow("bad gateway");

    const next = vi.fn(async () => "fresh");
    await expect(periodicRemoteRead("dev@box", "/other", next)).resolves.toBeNull();
    expect(next).not.toHaveBeenCalled();
    expect(failing).toHaveBeenCalledTimes(1);
  });

  it("rethrows a non-unreachable error without opening the window", async () => {
    await expect(
      periodicRemoteRead("dev@box", "/proj", async () => {
        throw new ApiError("session not found", 404);
      }),
    ).rejects.toThrow("session not found");

    const next = vi.fn(async () => "fresh");
    await expect(periodicRemoteRead("dev@box", "/other", next)).resolves.toEqual({ value: "fresh" });
    expect(next).toHaveBeenCalledTimes(1);
  });

  it("does not let one host's failure gate a different host", async () => {
    await expect(
      periodicRemoteRead("dev@down", "/proj", async () => {
        throw new TypeError("Failed to fetch");
      }),
    ).rejects.toThrow("Failed to fetch");

    await expect(periodicRemoteRead("dev@up", "/proj", async () => "ok")).resolves.toEqual({ value: "ok" });
  });
});

describe("isHostUnreachable", () => {
  it("treats network failures and 502s as unreachable", () => {
    expect(isHostUnreachable(new TypeError("Failed to fetch"))).toBe(true);
    expect(isHostUnreachable(new ApiError("remote connect failed", 502))).toBe(true);
  });

  it("treats other statuses and plain errors as answered", () => {
    expect(isHostUnreachable(new ApiError("not found", 404))).toBe(false);
    expect(isHostUnreachable(new ApiError("forbidden", 403))).toBe(false);
    expect(isHostUnreachable(new Error("reducer bug"))).toBe(false);
  });
});

describe("remoteHostRetryInMs and noteRemoteHostReachable", () => {
  beforeEach(() => {
    __resetRemoteReadGatesForTests();
  });

  it("reports no window for a host that has never failed", () => {
    expect(remoteHostRetryInMs("dev@fresh")).toBe(0);
  });

  it("clears an open window once the host is reached", async () => {
    await expect(
      periodicRemoteRead("dev@box", "/proj", async () => {
        throw new ApiError("remote connect failed", 502);
      }),
    ).rejects.toThrow("remote connect failed");
    expect(remoteHostRetryInMs("dev@box")).toBeGreaterThan(0);

    noteRemoteHostReachable("dev@box");
    expect(remoteHostRetryInMs("dev@box")).toBe(0);
    await expect(periodicRemoteRead("dev@box", "/proj", async () => "ok")).resolves.toEqual({ value: "ok" });
  });
});

describe("noteRemoteHostUnreachable", () => {
  beforeEach(() => {
    __resetRemoteReadGatesForTests();
  });

  it("opens the window for a read that handled its own error", async () => {
    noteRemoteHostUnreachable("dev@box");
    expect(remoteHostRetryInMs("dev@box")).toBeGreaterThan(0);
    const fn = vi.fn(async () => "fresh");
    await expect(periodicRemoteRead("dev@box", "/proj", fn)).resolves.toBeNull();
    expect(fn).not.toHaveBeenCalled();
  });
});

describe("withRemoteRead slot hand-off", () => {
  beforeEach(() => {
    vi.useFakeTimers();
    __resetRemoteReadGatesForTests();
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it("frees the slot when the host is marked unreachable after a queued read was granted it", async () => {
    const host = "dev@box";
    const firstStarted = deferred<void>();
    const firstResult = deferred<boolean>();
    const first = withRemoteRead(host, "/first", () => {
      firstStarted.resolve();
      return firstResult.promise;
    });
    await firstStarted.promise;

    const queuedFn = vi.fn(async () => true);
    const queued = withRemoteRead(host, "/queued", queuedFn);

    // The first read succeeds: drain grants the slot to the queued read. The
    // host is marked unreachable before the queued read's continuation runs.
    firstResult.resolve(true);
    await Promise.resolve();
    noteRemoteHostUnreachable(host);

    await expect(first).resolves.toBe(true);
    await expect(queued).resolves.toBe(false);
    expect(queuedFn).not.toHaveBeenCalled();

    vi.advanceTimersByTime(REMOTE_HOST_RETRY_MS + 1);
    const nextFn = vi.fn(async () => true);
    await expect(withRemoteRead(host, "/next", nextFn)).resolves.toBe(true);
    expect(nextFn).toHaveBeenCalledTimes(1);
  });
});

function deferred<T>(): { promise: Promise<T>; resolve: (value: T) => void } {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>((r) => {
    resolve = r;
  });
  return { promise, resolve };
}
