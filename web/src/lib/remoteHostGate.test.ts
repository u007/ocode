import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { ApiError } from "../api/client";
import {
  __resetRemoteReadGatesForTests,
  isHostConnecting,
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
      throw new ApiError("remote connecting", 503, { error: "remote connecting", stage: "remote-connecting" });
    });
    await expect(periodicRemoteRead("dev@box", "/proj", connecting)).resolves.toBeNull();
    expect(remoteHostRetryInMs("dev@box")).toBe(0);

    const next = vi.fn(async () => "fresh");
    await expect(periodicRemoteRead("dev@box", "/other", next)).resolves.toEqual({ value: "fresh" });
  });

  it("treats a plain 503 (no connecting stage) as an ordinary error", async () => {
    const unavailable = vi.fn(async () => {
      throw new ApiError("remote connections not available", 503, { error: "remote connections not available" });
    });
    await expect(periodicRemoteRead("dev@box", "/proj", unavailable)).rejects.toThrow(
      "remote connections not available",
    );

    const next = vi.fn(async () => "fresh");
    await expect(periodicRemoteRead("dev@box", "/other", next)).resolves.toEqual({ value: "fresh" });
    expect(next).toHaveBeenCalledTimes(1);
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

describe("isHostConnecting", () => {
  it("requires the remote-connecting stage on a 503", () => {
    expect(isHostConnecting(new ApiError("remote connecting", 503, { stage: "remote-connecting" }))).toBe(true);
    expect(isHostConnecting(new ApiError("remote connections not available", 503))).toBe(false);
    expect(isHostConnecting(new ApiError("remote connect failed", 502, { stage: "remote-connect" }))).toBe(false);
    expect(isHostConnecting(new Error("reducer bug"))).toBe(false);
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

describe("withRemoteRead probeWhileRetrying", () => {
  beforeEach(() => {
    __resetRemoteReadGatesForTests();
  });

  it("runs the probe while the window is armed and leaves the window alone", async () => {
    noteRemoteHostUnreachable("dev@box");
    expect(remoteHostRetryInMs("dev@box")).toBeGreaterThan(0);

    const probe = vi.fn(async () => true);
    await expect(
      withRemoteRead("dev@box", "status", probe, { probeWhileRetrying: true }),
    ).resolves.toBe(true);
    expect(probe).toHaveBeenCalledTimes(1);

    // The probe observed the host; it did not judge it, so the window is still
    // open and an ordinary read is still skipped.
    expect(remoteHostRetryInMs("dev@box")).toBeGreaterThan(0);
    const normal = vi.fn(async () => true);
    await expect(withRemoteRead("dev@box", "/proj", normal)).resolves.toBe(false);
    expect(normal).not.toHaveBeenCalled();
  });

  it("does not open the window when a probe fails", async () => {
    const probe = vi.fn(async () => false);
    await expect(
      withRemoteRead("dev@box", "status", probe, { probeWhileRetrying: true }),
    ).resolves.toBe(false);
    expect(remoteHostRetryInMs("dev@box")).toBe(0);
  });

  it("keeps probes and rejects ordinary waiters when the window arms mid-queue", async () => {
    const host = "dev@box";
    const holdStarted = deferred<void>();
    const hold = deferred<boolean>();
    const held = withRemoteRead(host, "/hold", () => {
      holdStarted.resolve();
      return hold.promise;
    });
    await holdStarted.promise;

    // Queue both kinds behind the holder, in mixed order.
    const plain = vi.fn(async () => true);
    const queuedPlain = withRemoteRead(host, "/plain", plain);
    const probe = vi.fn(async () => true);
    const queuedProbe = withRemoteRead(host, "/probe", probe, { probeWhileRetrying: true });
    const plain2 = vi.fn(async () => true);
    const queuedPlain2 = withRemoteRead(host, "/plain2", plain2);

    // The holder FAILS, so drain arms the window and rebuilds the queue with
    // the probe only: ordinary waiters are dropped, the probe keeps its place.
    hold.resolve(false);

    await expect(held).resolves.toBe(false);
    await expect(queuedPlain).resolves.toBe(false);
    await expect(queuedPlain2).resolves.toBe(false);
    expect(plain).not.toHaveBeenCalled();
    expect(plain2).not.toHaveBeenCalled();
    await expect(queuedProbe).resolves.toBe(true);
    expect(probe).toHaveBeenCalledTimes(1);

    // Nothing is wedged afterwards: an ordinary read is still skipped while the
    // window is open, and a fresh probe still runs.
    const after = vi.fn(async () => true);
    await expect(withRemoteRead(host, "/after", after)).resolves.toBe(false);
    expect(after).not.toHaveBeenCalled();
    const laterProbe = vi.fn(async () => true);
    await expect(
      withRemoteRead(host, "/probe-later", laterProbe, { probeWhileRetrying: true }),
    ).resolves.toBe(true);
    expect(laterProbe).toHaveBeenCalledTimes(1);
  });

  it("runs queued probes one at a time", async () => {
    const host = "dev@box";
    noteRemoteHostUnreachable(host);

    const firstStarted = deferred<void>();
    const firstDone = deferred<boolean>();
    const first = withRemoteRead(
      host,
      "/probe-1",
      () => {
        firstStarted.resolve();
        return firstDone.promise;
      },
      { probeWhileRetrying: true },
    );
    await firstStarted.promise;

    const secondFn = vi.fn(async () => true);
    const second = withRemoteRead(host, "/probe-2", secondFn, { probeWhileRetrying: true });
    await Promise.resolve();
    expect(secondFn).not.toHaveBeenCalled();

    firstDone.resolve(true);
    await expect(first).resolves.toBe(true);
    await expect(second).resolves.toBe(true);
    expect(secondFn).toHaveBeenCalledTimes(1);
  });

  it("does not park a queued probe when an ordinary read is granted just before the window arms", async () => {
    const host = "dev@box";
    const holdStarted = deferred<void>();
    const hold = deferred<boolean>();
    const held = withRemoteRead(host, "/hold", () => {
      holdStarted.resolve();
      return hold.promise;
    });
    await holdStarted.promise;

    const plain = vi.fn(async () => true);
    const queuedPlain = withRemoteRead(host, "/plain", plain);
    const probe = vi.fn(async () => true);
    const queuedProbe = withRemoteRead(host, "/probe", probe, { probeWhileRetrying: true });

    // The holder finishes OK, so drain grants the slot to the ordinary read.
    hold.resolve(true);
    await Promise.resolve();
    // The window arms before that read's continuation runs; its continuation
    // takes the `!probe && retryAt` branch, frees the slot and returns.
    noteRemoteHostUnreachable(host);

    await expect(held).resolves.toBe(true);
    await expect(queuedPlain).resolves.toBe(false);
    expect(plain).not.toHaveBeenCalled();

    // The queued probe must still get its turn with NO further read arriving.
    await expect(queuedProbe).resolves.toBe(true);
    expect(probe).toHaveBeenCalledTimes(1);
  });
});

function deferred<T>(): { promise: Promise<T>; resolve: (value: T) => void } {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>((r) => {
    resolve = r;
  });
  return { promise, resolve };
}
