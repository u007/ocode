/**
 * Per-host queue for periodic reads of a remote host.
 *
 * An unreachable host answers nothing: each request holds its browser
 * connection to the local server for the whole ssh timeout. Re-asking on every
 * poll tick keeps those connections occupied, and unrelated requests (a local
 * terminal's history fetch) queue behind them. So a periodic read of a host
 * (1) runs ONE AT A TIME per host, and (2) after a failure, waits
 * REMOTE_HOST_RETRY_MS before asking that host again.
 *
 * "One at a time" is a FIFO QUEUE, not a mutex. A mutex drops whoever asks
 * while another read is in flight, and the losers of that race are always the
 * same ones: every poller of a host ticks on its own schedule, so the
 * first-registered reader wins every tick and the others are never read at
 * all — their badges freeze forever while the host is perfectly healthy. A
 * queue gives every reader a turn, so a slow host delays each refresh by one
 * request's duration instead of starving it.
 *
 * The queue is per HOST, not per (host, endpoint): every read of one host is
 * a connection to that same host, so they share the slot. The `subject`
 * argument only labels a queued read (the project path, or the host status).
 */

/** How long a host that failed to answer is left alone before the next ask. */
export const REMOTE_HOST_RETRY_MS = 2 * 60_000;

type Waiter = {
  subject: string;
  /** A cheap liveness probe may run while the host is marked down. */
  probe: boolean;
  resolve: (granted: boolean) => void;
};

type HostGate = {
  /** True while a read holds this host's slot. */
  running: boolean;
  /** Ids of the queued reads, in arrival order. */
  queue: number[];
  /** The queued reads' resolvers, keyed by id. */
  waiters: Map<number, Waiter>;
  /** Wall-clock time until which this host counts as failed. */
  retryAt: number;
};

const gates = new Map<string, HostGate>();

/** Bumped by the test reset so a queued read can tell it was abandoned. */
let gateGeneration = 0;
let nextReadID = 0;

function gateFor(host: string): HostGate {
  let gate = gates.get(host);
  if (!gate) {
    gate = { running: false, queue: [], waiters: new Map(), retryAt: 0 };
    gates.set(host, gate);
  }
  return gate;
}

/**
 * True when a failed read says the host itself did not answer: a network-level
 * fetch failure, or a 502 from the local server that could not reach the host
 * (the proxy's `remote-connect` stage, or a remote git transport failure). Any
 * other error (404, 403, a reducer bug) means the host answered.
 */
export function isHostUnreachable(err: unknown): boolean {
  if (err instanceof TypeError) return true;
  // Duck-typed on purpose: importing ApiError from the API client would make
  // this module depend on it, and tests that mock the client would break.
  return err instanceof Error && "status" in err && err.status === 502;
}

/**
 * True when the local server answered 503 with stage `remote-connecting`: it is
 * still connecting to the host in the background. That is not a failure, so it
 * must not open the retry window a 502 does; the read is simply retried on the
 * next tick. A bare 503 (no stage marker) is an ordinary error.
 */
export function isHostConnecting(err: unknown): boolean {
  if (!(err instanceof Error) || !("status" in err) || err.status !== 503) return false;
  if (!("body" in err) || typeof err.body !== "object" || err.body === null) return false;
  return "stage" in err.body && err.body.stage === "remote-connecting";
}

export interface RemoteReadOptions {
  /**
   * Run this read even while the host is inside its retry window, and never let
   * its outcome open or clear that window (only an explicit
   * `noteRemoteHostReachable` does). Used ONLY for a cheap liveness probe such
   * as the host-status endpoint — an in-memory registry read on the local
   * server with no ssh and no connect. Without it a host that comes back can
   * never be noticed until the window expires: every read, including the one
   * that could prove the host is healthy, is skipped while the window is armed.
   */
  probeWhileRetrying?: boolean;
}

/**
 * Runs `fn` under this host's read slot, waiting in line if another read of
 * the same host is in flight.
 *
 * `fn` resolves true when the read SUCCEEDED; a false result (or a throw)
 * counts as a failure and opens the host's retry window. The caller keeps its
 * own data handling inside `fn` — this only sequences and classifies the read.
 * With `probeWhileRetrying` the read runs even inside the window and its
 * outcome never changes the window (see `RemoteReadOptions`).
 *
 * Resolves false when the read was SKIPPED: the host is inside its retry
 * window, or a test reset abandoned the queue.
 */
export async function withRemoteRead(
  host: string,
  subject: string,
  fn: () => Promise<boolean>,
  opts?: RemoteReadOptions,
): Promise<boolean> {
  const gate = gateFor(host);
  const generation = gateGeneration;
  const probe = opts?.probeWhileRetrying === true;
  if (!probe && gate.retryAt > Date.now()) return false;

  const id = nextReadID++;
  let resolveTurn!: (granted: boolean) => void;
  const turn = new Promise<boolean>((resolve) => {
    resolveTurn = resolve;
  });
  gate.waiters.set(id, { subject, probe, resolve: resolveTurn });
  gate.queue.push(id);
  drain(gate);

  // false: dropped by a failed read or a test reset, so this read never held
  // the slot and must not release it.
  if (!(await turn)) return false;

  // A reset abandoned the slot we were granted: the gate has been rebuilt.
  if (generation !== gateGeneration) return false;
  if (!probe && gate.retryAt > Date.now()) {
    // The host was marked unreachable after drain granted us the slot. Free it
    // without opening a new window; drain drops the rest of the queue.
    gate.running = false;
    drain(gate);
    return false;
  }

  let ok = false;
  try {
    ok = await fn();
  } catch (err) {
    finish(gate, probe, false);
    throw err;
  }
  finish(gate, probe, ok);
  return ok;
}

/**
 * Releases the slot after a read. A probe never touches the retry window: it
 * exists to observe the host, not to judge it, so a probe of a still-down host
 * must not extend the window and a probe of a recovered host must not clear it
 * unless the caller explicitly calls `noteRemoteHostReachable`.
 */
function finish(gate: HostGate, probe: boolean, ok: boolean): void {
  if (probe) {
    gate.running = false;
    drain(gate);
    return;
  }
  release(gate, ok);
}

/**
 * One periodic read of project data on `host`, gated like every other remote
 * read. A read with no host is local and runs directly. Resolves null when the
 * read was skipped because the host is inside its retry window or still
 * connecting (503), so the caller keeps its last good data. Only an unreachable host (see isHostUnreachable)
 * opens the window; any other failure rethrows while the host still counts as
 * answering.
 */
export async function periodicRemoteRead<T>(
  host: string | undefined,
  subject: string,
  fn: () => Promise<T>,
): Promise<{ value: T } | null> {
  if (!host) return { value: await fn() };
  const outcome: { read: { value: T } | null; failure: { error: unknown } | null } = {
    read: null,
    failure: null,
  };
  await withRemoteRead(host, subject, async () => {
    try {
      outcome.read = { value: await fn() };
    } catch (error) {
      if (isHostConnecting(error)) return true;
      if (!isHostUnreachable(error)) {
        outcome.failure = { error };
        return true;
      }
      throw error;
    }
    return true;
  });
  if (outcome.failure) throw outcome.failure.error;
  return outcome.read;
}

/** Milliseconds until a host's retry window closes; 0 when it may be asked. */
export function remoteHostRetryInMs(host: string): number {
  return Math.max(0, gateFor(host).retryAt - Date.now());
}

/**
 * Opens a host's retry window for a read that was not made through
 * periodicRemoteRead (a poll that handles its own errors). Call it only when
 * isHostUnreachable(err) holds.
 */
export function noteRemoteHostUnreachable(host: string): void {
  const gate = gateFor(host);
  gate.retryAt = Date.now() + REMOTE_HOST_RETRY_MS;
  drain(gate);
}

/**
 * Clears a host's retry window once something has reached it: a terminal
 * WebSocket that opened, a successful connect or restart, or a status read.
 * Failures are not recorded here; periodic reads own the window.
 */
export function noteRemoteHostReachable(host: string): void {
  const gate = gateFor(host);
  gate.retryAt = 0;
  drain(gate);
}

/** Frees the slot and hands it to the next queued read, if any. */
function release(gate: HostGate, ok: boolean): void {
  gate.running = false;
  gate.retryAt = ok ? 0 : Date.now() + REMOTE_HOST_RETRY_MS;
  drain(gate);
}

/** Gives the slot to the next queued read, or drops the queue on failure. */
function drain(gate: HostGate): void {
  if (gate.running) return;
  if (gate.retryAt > Date.now()) {
    // The host just failed: ordinary reads are dropped rather than asking a
    // host that is known to be down. A probe is the liveness check that can
    // prove the host recovered, so it is allowed to run; keeping only the
    // probes means a probe still gets its turn in arrival order.
    const probes: number[] = [];
    for (const id of gate.queue) {
      const waiter = gate.waiters.get(id);
      if (waiter?.probe) {
        probes.push(id);
        continue;
      }
      gate.waiters.delete(id);
      waiter?.resolve(false);
    }
    gate.queue.length = 0;
    gate.queue.push(...probes);
  }
  while (gate.queue.length > 0) {
    const id = gate.queue.shift()!;
    const waiter = gate.waiters.get(id);
    if (!waiter) continue;
    gate.waiters.delete(id);
    gate.running = true;
    waiter.resolve(true);
    return;
  }
}

/**
 * Test hook: forgets every host's failure window and abandons anything
 * queued. Abandoned reads resolve as "skipped" (they see a bumped generation),
 * so a reset never lets a queued read fire afterwards.
 */
export function __resetRemoteReadGatesForTests(): void {
  gateGeneration++;
  for (const gate of gates.values()) {
    gate.running = false;
    gate.retryAt = 0;
    const pending = [...gate.waiters.values()];
    gate.waiters.clear();
    gate.queue.length = 0;
    for (const waiter of pending) waiter.resolve(false);
  }
}
