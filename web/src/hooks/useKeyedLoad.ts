import { useCallback, useEffect, useRef, useSyncExternalStore } from "react";

/** How long a refresh may run before its tab trigger shows a spinner. */
export const REFRESH_INDICATOR_DELAY_MS = 300;

export type LoadStatus = "start" | "success" | "empty" | "error";

export interface LoadRequestEvent {
  originKey: string;
  generation: number;
  status: LoadStatus;
  message?: string;
  retry?: () => void;
}

export type LoadingEventHandler = (event: LoadRequestEvent, owner?: symbol) => void;

export interface TabLoadingProps {
  loadingKey?: string;
  onLoadingEvent?: LoadingEventHandler;
}

export type TabPhase = "idle" | "initial" | "refresh" | "error";

export interface TabLoadingSnapshot {
  readonly phase: TabPhase;
  readonly error?: string;
  readonly retry?: () => void;
}

export interface KeyedLoadContext {
  readonly originKey: string;
  readonly generation: number;
  readonly signal: AbortSignal;
}

export interface KeyedLoadOptions<T> {
  /** Mark a successful response as an empty result for the store. */
  empty?: (value: T) => boolean;
  /** Re-run the complete panel operation, including its state application. */
  retry?: () => void;
}

export type KeyedLoadResult<T> =
  | { status: "success"; value: T; empty: false }
  | { status: "empty"; value: T; empty: true }
  | { status: "error"; error: unknown; message: string }
  | { status: "stale" }
  | { status: "aborted" };

export type KeyedLoadRunner = <T>(
  load: (context: KeyedLoadContext) => Promise<T>,
  options?: KeyedLoadOptions<T>,
) => Promise<KeyedLoadResult<T>>;

type Entry = {
  phase: TabPhase;
  generation: number;
  resolved: boolean;
  error?: string;
  retry?: () => void;
  refreshTimer?: ReturnType<typeof setTimeout>;
  owner?: symbol;
  view: TabLoadingSnapshot;
};

const IDLE: TabLoadingSnapshot = Object.freeze({ phase: "idle" });
const entries = new Map<string, Entry>();
type OwnerRecord = { originKey: string; invalidate: () => void };
const ownerRecords = new Map<symbol, OwnerRecord>();
const listeners = new Set<() => void>();
let storeSnapshot: ReadonlyMap<string, TabLoadingSnapshot> = new Map();

function makeEntry(generation = 0, owner?: symbol): Entry {
  const entry: Entry = {
    phase: "idle",
    generation,
    resolved: false,
    owner,
    view: IDLE,
  };
  return entry;
}

function updateView(entry: Entry): boolean {
  const view: TabLoadingSnapshot = {
    phase: entry.phase,
    ...(entry.error !== undefined ? { error: entry.error } : {}),
    ...(entry.retry !== undefined ? { retry: entry.retry } : {}),
  };
  if (
    entry.view.phase === view.phase &&
    entry.view.error === view.error &&
    entry.view.retry === view.retry
  ) {
    return false;
  }
  entry.view = Object.freeze(view);
  return true;
}

function publish(): void {
  const next = new Map<string, TabLoadingSnapshot>();
  for (const [key, entry] of entries) next.set(key, entry.view);
  storeSnapshot = next;
  for (const listener of listeners) listener();
}

function clearTimer(entry: Entry): void {
  if (entry.refreshTimer !== undefined) {
    clearTimeout(entry.refreshTimer);
    entry.refreshTimer = undefined;
  }
}

function claimTabLoadingKey(originKey: string, owner: symbol): void {
  const existing = entries.get(originKey);
  if (existing && existing.owner !== undefined && existing.owner !== owner) {
    ownerRecords.get(existing.owner)?.invalidate();
    clearTimer(existing);
    entries.delete(originKey);
  }
  if (!entries.has(originKey)) entries.set(originKey, makeEntry(0, owner));
  else entries.get(originKey)!.owner = owner;
}

function releaseTabLoadingKey(originKey: string, owner: symbol): void {
  const entry = entries.get(originKey);
  if (!entry || entry.owner !== owner) return;
  clearTimer(entry);
  entries.delete(originKey);
  publish();
}

function errorMessage(error: unknown): string {
  if (error instanceof Error && error.message) return error.message;
  if (typeof error === "string" && error) return error;
  return "Failed to load";
}

function isAbortError(error: unknown): boolean {
  return (
    (error instanceof DOMException && error.name === "AbortError") ||
    (typeof error === "object" && error !== null && "name" in error && error.name === "AbortError")
  );
}

function emitOwnedEvent(event: LoadRequestEvent, owner?: symbol): void {
  let entry = entries.get(event.originKey);
  if (!entry) {
    // Direct store users may emit a terminal event without a preceding start;
    // panel hooks always claim a key before starting, so this is only a safe
    // convenience for tests and non-panel callers.
    if (owner !== undefined) return;
    entry = makeEntry(event.generation);
    entries.set(event.originKey, entry);
  }
  if (owner !== undefined && entry.owner !== owner) return;
  if (event.status !== "start" && event.generation !== entry.generation) return;

  switch (event.status) {
    case "start": {
      entry.generation = event.generation;
      entry.error = undefined;
      entry.retry = undefined;
      if (!entry.resolved) {
        clearTimer(entry);
        if (entry.phase !== "initial") {
          entry.phase = "initial";
        }
        if (updateView(entry)) publish();
      } else {
        clearTimer(entry);
        if (entry.phase !== "refresh") {
          entry.phase = "idle";
          if (updateView(entry)) publish();
        }
        const generation = event.generation;
        entry.refreshTimer = setTimeout(() => {
          if (entry.generation !== generation || !entry.resolved) return;
          if (entry.phase !== "refresh") {
            entry.phase = "refresh";
            if (updateView(entry)) publish();
          }
        }, REFRESH_INDICATOR_DELAY_MS);
      }
      return;
    }
    case "success":
    case "empty": {
      clearTimer(entry);
      entry.resolved = true;
      entry.error = undefined;
      entry.retry = undefined;
      if (entry.phase !== "idle") {
        entry.phase = "idle";
        if (updateView(entry)) publish();
      } else {
        if (updateView(entry)) publish();
      }
      return;
    }
    case "error": {
      clearTimer(entry);
      if (!entry.resolved) {
        entry.phase = "error";
        entry.error = event.message ?? "Failed to load";
        entry.retry = event.retry;
        if (updateView(entry)) publish();
      } else {
        entry.error = undefined;
        entry.retry = undefined;
        if (entry.phase !== "idle") {
          entry.phase = "idle";
          if (updateView(entry)) publish();
        } else if (updateView(entry)) {
          publish();
        }
      }
      return;
    }
  }
}

/** Build the stable host/project/tab identity used by the loading store. */
export function tabLoadKey(
  host: string | undefined,
  projectPath: string | undefined,
  tabKey: string,
): string {
  return `${host ?? ""}\u0000${projectPath ?? ""}\u0000${tabKey}`;
}

/** Apply a panel event to the shared loading store. */
export function emitTabLoadEvent(event: LoadRequestEvent, owner?: symbol): void {
  emitOwnedEvent(event, owner);
}

/** Read a stable snapshot for a key without subscribing. */
export function getTabLoadingState(originKey: string): TabLoadingSnapshot {
  return entries.get(originKey)?.view ?? IDLE;
}

/**
 * Subscribe a component to the shared keyed loading store. The returned map
 * changes only when a visible phase/error transition occurs, so it is safe to
 * use as a useSyncExternalStore snapshot.
 */
export function useTabLoadingStore(): ReadonlyMap<string, TabLoadingSnapshot> {
  const subscribe = useCallback((listener: () => void) => {
    listeners.add(listener);
    return () => {
      listeners.delete(listener);
    };
  }, []);
  const getSnapshot = useCallback(() => storeSnapshot, []);
  return useSyncExternalStore(subscribe, getSnapshot, getSnapshot);
}

/**
 * Run one generation of a keyed request. A new run, key change, or unmount
 * aborts the previous controller; stale results are returned as `stale` and
 * never emitted to the store or applied by the caller.
 */
export function useKeyedLoad(
  loadingKey: string | undefined,
  onLoadingEvent?: LoadingEventHandler,
): KeyedLoadRunner {
  const ownerRef = useRef<symbol | null>(null);
  if (ownerRef.current === null) ownerRef.current = Symbol("tab-loading-owner");
  const owner = ownerRef.current;
  const generationRef = useRef(0);
  const controllerRef = useRef<AbortController | null>(null);
  const keyRef = useRef(loadingKey);
  keyRef.current = loadingKey;
  const eventRef = useRef(onLoadingEvent);
  eventRef.current = onLoadingEvent;
  const runRef = useRef<KeyedLoadRunner | null>(null);
  const invalidate = useCallback(() => {
    generationRef.current += 1;
    controllerRef.current?.abort();
    controllerRef.current = null;
  }, []);

  useEffect(() => {
    const originKey = loadingKey;
    if (originKey) {
      claimTabLoadingKey(originKey, owner);
      ownerRecords.set(owner, { originKey, invalidate });
    }
    return () => {
      ownerRecords.delete(owner);
      invalidate();
      if (originKey) releaseTabLoadingKey(originKey, owner);
    };
  }, [invalidate, loadingKey, owner]);

  const run = useCallback<KeyedLoadRunner>(async <T,>(
    load: (context: KeyedLoadContext) => Promise<T>,
    options?: KeyedLoadOptions<T>,
  ): Promise<KeyedLoadResult<T>> => {
    const expectedKey = keyRef.current;
    const originKey = expectedKey ?? "";
    const generation = ++generationRef.current;
    controllerRef.current?.abort();
    const controller = new AbortController();
    controllerRef.current = controller;
    if (originKey) {
      claimTabLoadingKey(originKey, owner);
      ownerRecords.set(owner, { originKey, invalidate });
      eventRef.current?.({ originKey, generation, status: "start" }, owner);
    }
    const isCurrent = () =>
      generationRef.current === generation &&
      keyRef.current === expectedKey &&
      !controller.signal.aborted;

    try {
      const value = await load({ originKey, generation, signal: controller.signal });
      if (!isCurrent()) return { status: "stale" };
      const isEmpty = options?.empty?.(value) ?? false;
      if (originKey) {
        eventRef.current?.({
          originKey,
          generation,
          status: isEmpty ? "empty" : "success",
        }, owner);
      }
      return isEmpty ? { status: "empty", value, empty: true } : { status: "success", value, empty: false };
    } catch (error) {
      if (!isCurrent()) return { status: "stale" };
      if (isAbortError(error)) return { status: "aborted" };
      const message = errorMessage(error);
      if (originKey) {
        const retry = options?.retry ?? (() => {
          void runRef.current?.(load, options);
        });
        eventRef.current?.({ originKey, generation, status: "error", message, retry }, owner);
      }
      return { status: "error", error, message };
    } finally {
      if (controllerRef.current === controller) controllerRef.current = null;
    }
  }, [invalidate, owner]);
  runRef.current = run;

  return run;
}

/** Clear all keyed state, including delayed refresh timers. */
export function clearTabLoadingStore(): void {
  if (entries.size === 0 && ownerRecords.size === 0) return;
  for (const record of ownerRecords.values()) record.invalidate();
  for (const entry of entries.values()) clearTimer(entry);
  entries.clear();
  publish();
}

/** Clear entries belonging to one project/host scope after a project switch. */
export function clearTabLoadingForProject(host: string | undefined, projectPath: string | undefined): void {
  const prefix = `${host ?? ""}\u0000${projectPath ?? ""}\u0000`;
  let changed = false;
  for (const record of ownerRecords.values()) {
    if (record.originKey.startsWith(prefix)) record.invalidate();
  }
  for (const [key, entry] of entries) {
    if (!key.startsWith(prefix)) continue;
    clearTimer(entry);
    entries.delete(key);
    changed = true;
  }
  if (changed) publish();
}

/** Test-only reset; production callers should use the scoped clear helpers. */
export function __resetTabLoadingStoreForTests(): void {
  clearTabLoadingStore();
}
