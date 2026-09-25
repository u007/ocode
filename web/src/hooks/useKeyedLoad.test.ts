import { act, renderHook } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
  __resetTabLoadingStoreForTests,
  clearTabLoadingStore,
  clearTabLoadingForProject,
  emitTabLoadEvent,
  REFRESH_INDICATOR_DELAY_MS,
  tabLoadKey,
  useKeyedLoad,
  useTabLoadingStore,
  type LoadRequestEvent,
} from "./useKeyedLoad";

function deferred<T>() {
  let resolve!: (value: T) => void;
  let reject!: (reason?: unknown) => void;
  const promise = new Promise<T>((res, rej) => {
    resolve = res;
    reject = rej;
  });
  return { promise, resolve, reject };
}

beforeEach(() => {
  vi.useFakeTimers();
  __resetTabLoadingStoreForTests();
});

afterEach(() => {
  vi.useRealTimers();
});

describe("useKeyedLoad", () => {
  it("keeps the external-store snapshot reference stable between transitions", () => {
    const { result, rerender } = renderHook(() => useTabLoadingStore());
    const initial = result.current;
    rerender();
    expect(result.current).toBe(initial);

    const key = tabLoadKey("", "/project", "files");
    act(() => emitTabLoadEvent({ originKey: key, generation: 1, status: "start" }));
    const afterStart = result.current;
    rerender();
    expect(result.current).toBe(afterStart);
  });

  it("keeps initial loading blocked until the 300ms gate and resolves empty", async () => {
    const key = tabLoadKey("", "/project", "files");
    const request = deferred<string[]>();
    const { result } = renderHook(() => {
      const run = useKeyedLoad(key, emitTabLoadEvent);
      const states = useTabLoadingStore();
      return { run, states };
    });

    let load!: Promise<unknown>;
    act(() => {
      load = result.current.run(() => request.promise, { empty: (files) => files.length === 0 });
    });

    expect(result.current.states.get(key)?.phase).toBe("initial");
    act(() => vi.advanceTimersByTime(REFRESH_INDICATOR_DELAY_MS - 1));
    expect(result.current.states.get(key)?.phase).toBe("initial");

    await act(async () => {
      request.resolve([]);
      await load;
      vi.advanceTimersByTime(REFRESH_INDICATOR_DELAY_MS);
    });
    expect(result.current.states.get(key)?.phase).toBe("idle");
  });

  it("does not blink for a fast refresh and shows a delayed refresh", async () => {
    const key = tabLoadKey("host-a", "/same", "git");
    const first = deferred<string>();
    const second = deferred<string>();
    const { result } = renderHook(() => {
      const run = useKeyedLoad(key, emitTabLoadEvent);
      const states = useTabLoadingStore();
      return { run, states };
    });

    let load!: Promise<unknown>;
    act(() => {
      load = result.current.run(() => first.promise);
    });
    await act(async () => {
      first.resolve("ready");
      await load;
    });
    expect(result.current.states.get(key)?.phase).toBe("idle");

    act(() => {
      load = result.current.run(() => second.promise);
    });
    act(() => vi.advanceTimersByTime(REFRESH_INDICATOR_DELAY_MS - 1));
    expect(result.current.states.get(key)?.phase).toBe("idle");
    act(() => vi.advanceTimersByTime(1));
    expect(result.current.states.get(key)?.phase).toBe("refresh");

    await act(async () => {
      second.resolve("updated");
      await load;
    });
    expect(result.current.states.get(key)?.phase).toBe("idle");
  });

  it("keeps errors distinct: initial exposes retry, refresh stays idle", async () => {
    const key = tabLoadKey("", "/project", "assets");
    const ready = deferred<string>();
    const first = deferred<string>();
    const { result } = renderHook(() => {
      const run = useKeyedLoad(key, emitTabLoadEvent);
      const states = useTabLoadingStore();
      return { run, states };
    });

    let firstLoad!: Promise<unknown>;
    act(() => {
      firstLoad = result.current.run(() => first.promise);
    });
    await act(async () => {
      first.reject(new Error("initial failed"));
      await firstLoad;
    });
    expect(result.current.states.get(key)?.phase).toBe("error");
    expect(result.current.states.get(key)?.error).toBe("initial failed");
    expect(result.current.states.get(key)?.retry).toBeTypeOf("function");

    let readyLoad!: Promise<unknown>;
    act(() => {
      readyLoad = result.current.run(() => ready.promise);
    });
    await act(async () => {
      ready.resolve("ready");
      await readyLoad;
    });

    const refresh = deferred<string>();
    let refreshLoad!: Promise<unknown>;
    act(() => {
      refreshLoad = result.current.run(() => refresh.promise);
    });
    expect(result.current.states.get(key)?.phase).toBe("idle");
    await act(async () => {
      refresh.reject(new Error("refresh failed"));
      await refreshLoad;
    });
    expect(result.current.states.get(key)?.phase).toBe("idle");
    expect(result.current.states.get(key)?.error).toBeUndefined();
  });

  it("drops a late response and aborts the superseded request", async () => {
    const key = tabLoadKey("", "/project", "files");
    const first = deferred<string>();
    const second = deferred<string>();
    const signals: AbortSignal[] = [];
    const { result } = renderHook(() => ({ run: useKeyedLoad(key, emitTabLoadEvent) }));

    let firstLoad!: Promise<unknown>;
    let secondLoad!: Promise<unknown>;
    act(() => {
      firstLoad = result.current.run(({ signal }) => {
        signals.push(signal);
        return first.promise;
      });
    });
    act(() => {
      secondLoad = result.current.run(({ signal }) => {
        signals.push(signal);
        return second.promise;
      });
    });
    expect(signals).toHaveLength(2);
    expect(signals[0].aborted).toBe(true);

    let firstOutcome: unknown;
    await act(async () => {
      first.resolve("stale");
      firstOutcome = await firstLoad;
    });
    expect(firstOutcome).toEqual({ status: "stale" });

    await act(async () => {
      second.resolve("current");
      await secondLoad;
    });
  });

  it("isolates identical project paths on different hosts", async () => {
    const local = tabLoadKey("", "/same", "git");
    const remote = tabLoadKey("host@example", "/same", "git");
    const localRequest = deferred<string>();
    const remoteRequest = deferred<string>();
    const localHarness = renderHook(() => {
      const run = useKeyedLoad(local, emitTabLoadEvent);
      const states = useTabLoadingStore();
      return { run, states };
    });
    const remoteHarness = renderHook(() => {
      const run = useKeyedLoad(remote, emitTabLoadEvent);
      const states = useTabLoadingStore();
      return { run, states };
    });

    act(() => {
      void localHarness.result.current.run(() => localRequest.promise);
      void remoteHarness.result.current.run(() => remoteRequest.promise);
    });

    expect(localHarness.result.current.states.get(local)?.phase).toBe("initial");
    expect(remoteHarness.result.current.states.get(remote)?.phase).toBe("initial");
    await act(async () => {
      localRequest.resolve("done");
      remoteRequest.resolve("done too");
    });
  });

  it("aborts on unmount and emits no terminal event", async () => {
    const key = tabLoadKey("", "/project", "git");
    const request = deferred<string>();
    const events: LoadRequestEvent[] = [];
    const { result, unmount } = renderHook(() => ({
      run: useKeyedLoad(key, (event) => {
        events.push(event);
        emitTabLoadEvent(event);
      }),
    }));
    let signal!: AbortSignal;
    let load!: Promise<unknown>;
    act(() => {
      load = result.current.run((context) => {
        signal = context.signal;
        return request.promise;
      });
    });
    unmount();
    expect(signal.aborted).toBe(true);
    await act(async () => {
      request.resolve("late");
      await load;
    });
    expect(events.map((event) => event.status)).toEqual(["start"]);
  });

  it("clears a project scope and invalidates its late response", async () => {
    const key = tabLoadKey("", "/project", "files");
    const request = deferred<string>();
    const { result } = renderHook(() => ({
      run: useKeyedLoad(key, emitTabLoadEvent),
      states: useTabLoadingStore(),
    }));
    let signal!: AbortSignal;
    let load!: Promise<unknown>;
    act(() => {
      load = result.current.run((context) => {
        signal = context.signal;
        return request.promise;
      });
    });
    act(() => clearTabLoadingForProject("", "/project"));
    expect(signal.aborted).toBe(true);
    let outcome: unknown;
    await act(async () => {
      request.resolve("late");
      outcome = await load;
    });
    expect(outcome).toEqual({ status: "stale" });
    expect(result.current.states.has(key)).toBe(false);
  });

  it("clears every key when the project store is reset", () => {
    const first = tabLoadKey("", "/a", "files");
    const second = tabLoadKey("host", "/b", "git");
    act(() => {
      emitTabLoadEvent({ originKey: first, generation: 1, status: "start" });
      emitTabLoadEvent({ originKey: second, generation: 1, status: "start" });
    });
    const { result } = renderHook(() => useTabLoadingStore());
    expect(result.current.size).toBe(2);
    act(() => clearTabLoadingStore());
    expect(result.current.size).toBe(0);
  });

  it("does not clear a new project key that was claimed before the switch cleanup", () => {
    const oldKey = tabLoadKey("", "/old", "files");
    const newKey = tabLoadKey("", "/new", "files");
    act(() => {
      emitTabLoadEvent({ originKey: oldKey, generation: 1, status: "start" });
      emitTabLoadEvent({ originKey: newKey, generation: 1, status: "start" });
      clearTabLoadingForProject("", "/old");
    });
    const { result } = renderHook(() => useTabLoadingStore());
    expect(result.current.has(oldKey)).toBe(false);
    expect(result.current.get(newKey)?.phase).toBe("initial");
  });

  it("leaves a new owner running when old-project cleanup runs after child effects", async () => {
    const newKey = tabLoadKey("", "/new", "git");
    const request = deferred<string>();
    const { result } = renderHook(() => ({
      run: useKeyedLoad(newKey, emitTabLoadEvent),
      states: useTabLoadingStore(),
    }));
    let signal!: AbortSignal;
    let load!: Promise<unknown>;
    act(() => {
      load = result.current.run((context) => {
        signal = context.signal;
        return request.promise;
      });
    });
    act(() => clearTabLoadingForProject("", "/old"));
    expect(signal.aborted).toBe(false);
    await act(async () => {
      request.resolve("loaded");
      await load;
    });
    expect(result.current.states.get(newKey)?.phase).toBe("idle");
  });
});
