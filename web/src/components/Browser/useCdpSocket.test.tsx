import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { render, act, waitFor } from "@testing-library/react";
import { useState } from "react";

// Harness: a component that mounts the hook with fixed params and records
// status/error transitions.
const { useCdpSocket } = await import("./useCdpSocket");
const { browserActions, browserStore } = await import("../../lib/browserStore");

let lastApi: ReturnType<typeof useCdpSocket> | null = null;
function Harness(props: { stateKey: "tab:abc"; base: string | null; enabled: boolean }) {
  const api = useCdpSocket(props.stateKey, props.base, props.enabled);
  lastApi = api;
  const [status, setStatus] = useState(api.status);
  // keep a re-render on status change
  if (api.status !== status) setStatus(api.status);
  return (
    <div>
      <span data-testid="status">{api.status}</span>
      <span data-testid="error">{api.error ?? ""}</span>
    </div>
  );
}

// FakeWebSocket: minimal spec-compliant double installed on globalThis.
class FakeWebSocket {
  static CONNECTING = 0;
  static OPEN = 1;
  static CLOSING = 2;
  static CLOSED = 3;
  static instances: FakeWebSocket[] = [];
  url: string;
  binaryType = "blob";
  readyState = 0;
  onopen: (() => void) | null = null;
  onclose: (() => void) | null = null;
  onerror: (() => void) | null = null;
  onmessage: ((ev: MessageEvent) => void) | null = null;
  sent: (string | ArrayBuffer)[] = [];
  closed = false;
  constructor(url: string) {
    this.url = url;
    FakeWebSocket.instances.push(this);
  }
  send(data: string | ArrayBuffer) {
    this.sent.push(data);
  }
  close() {
    this.closed = true;
    this.readyState = 3;
    if (!this.onclose) return;
    const cb = this.onclose;
    this.onclose = null;
    cb();
  }
  // test helpers
  serverOpen() {
    this.readyState = 1;
    this.onopen?.();
  }
  serverMessage(data: string | ArrayBuffer) {
    this.onmessage?.(new MessageEvent("message", { data }));
  }
  serverClose() {
    this.readyState = 3;
    const cb = this.onclose;
    this.onclose = null;
    cb?.();
  }
}

const mockGrant = vi.hoisted(() =>
  vi.fn(async (_key: string) => "GRANT-" + Math.random().toString(36).slice(2)),
);

vi.mock("../../api/client", () => ({
  mintBrowseGrant: (key: string) => mockGrant(key),
}));

beforeEach(() => {
  FakeWebSocket.instances = [];
  vi.stubGlobal("WebSocket", FakeWebSocket as unknown as typeof WebSocket);
  mockGrant.mockClear();
  // Fake timers with immediate microtask draining: the hook's grant promise
  // chain resolves on microtasks, and waitFor polls on timers — this config
  // lets both progress deterministically.
  vi.useFakeTimers({ shouldAdvanceTime: true });
  browserStore.setState(() => ({ byKey: {} }));
  browserActions.open("tab:abc");
});

afterEach(() => {
  vi.unstubAllGlobals();
  vi.useRealTimers();
});

function lastSocket(): FakeWebSocket {
  return FakeWebSocket.instances[FakeWebSocket.instances.length - 1];
}

describe("useCdpSocket", () => {
  it("mints a grant and opens the __cdp ws URL with arraybuffer binary type", async () => {
    render(<Harness stateKey="tab:abc" base="http://127.0.0.1:54321" enabled />);
    await waitFor(() => expect(FakeWebSocket.instances.length).toBe(1));
    const ws = lastSocket();
    const grant = await mockGrant.mock.results[0].value;
    // stateKey is percent-encoded in the path; compare decoded form.
    expect(decodeURIComponent(ws.url)).toBe(
      "ws://127.0.0.1:54321/b/tab:abc/__cdp?__grant=" + grant,
    );
    expect(ws.binaryType).toBe("arraybuffer");
  });

  it("converts https base to wss", async () => {
    render(<Harness stateKey="tab:abc" base="https://browse.example" enabled />);
    await waitFor(() => expect(FakeWebSocket.instances.length).toBe(1));
    expect(lastSocket().url).toMatch(/^wss:\/\//);
  });

  it("does not connect when disabled", () => {
    render(<Harness stateKey="tab:abc" base="http://127.0.0.1:54321" enabled={false} />);
    expect(FakeWebSocket.instances.length).toBe(0);
  });

  it("queues sends before open and flushes in order", async () => {
    render(<Harness stateKey="tab:abc" base="http://127.0.0.1:54321" enabled />);
    await waitFor(() => expect(FakeWebSocket.instances.length).toBe(1));
    const ws = lastSocket();
    act(() => {
      lastApi!.send({ t: "back" });
      lastApi!.send({ t: "nav", url: "https://a.com/" });
    });
    expect(ws.sent).toEqual([]); // still queued (CONNECTING)
    act(() => ws.serverOpen());
    expect(ws.sent).toEqual([JSON.stringify({ t: "back" }), JSON.stringify({ t: "nav", url: "https://a.com/" })]);
  });

  it("routes console telemetry into browserActions.pushConsole", async () => {
    render(<Harness stateKey="tab:abc" base="http://127.0.0.1:54321" enabled />);
    await waitFor(() => expect(FakeWebSocket.instances.length).toBe(1));
    const ws = lastSocket();
    act(() => {
      ws.serverOpen();
      ws.serverMessage(JSON.stringify({ t: "console", level: "warn", args: ["a", "b"], ts: 42 }));
    });
    const evs = browserStore.state.byKey["tab:abc"].consoleEvents;
    expect(evs.length).toBe(1);
    expect(evs[0]).toEqual({ level: "warn", text: "a b", ts: 42 });
  });

  it("routes network telemetry into browserActions.pushNetwork", async () => {
    render(<Harness stateKey="tab:abc" base="http://127.0.0.1:54321" enabled />);
    await waitFor(() => expect(FakeWebSocket.instances.length).toBe(1));
    const ws = lastSocket();
    act(() => {
      ws.serverOpen();
      ws.serverMessage(JSON.stringify({ t: "network", method: "GET", url: "https://x/", status: 200, durationMs: 7, ts: 9 }));
    });
    const evs = browserStore.state.byKey["tab:abc"].networkEvents;
    expect(evs.length).toBe(1);
    expect(evs[0].durationMs).toBe(7);
  });

  it("decodes binary frames big-endian and fires onFrame callbacks", async () => {
    render(<Harness stateKey="tab:abc" base="http://127.0.0.1:54321" enabled />);
    await waitFor(() => expect(FakeWebSocket.instances.length).toBe(1));
    const ws = lastSocket();
    const seen: { w: number; h: number }[] = [];
    act(() => lastApi!.onFrame((_, w, h) => seen.push({ w, h })));
    act(() => ws.serverOpen());
    const buf = new ArrayBuffer(12);
    const dv = new DataView(buf);
    dv.setUint32(0, 640);
    dv.setUint32(4, 480);
    new Uint8Array(buf, 8).set([1, 2, 3, 4]); // fake jpeg tail
    act(() => ws.serverMessage(buf));
    expect(seen).toEqual([{ w: 640, h: 480 }]);
  });

  it("reconnects with new grant + backoff on close without error", async () => {
    render(<Harness stateKey="tab:abc" base="http://127.0.0.1:54321" enabled />);
    await waitFor(() => expect(FakeWebSocket.instances.length).toBe(1));
    const first = lastSocket();
    act(() => {
      first.serverOpen();
      first.serverClose();
    });
    expect(lastApi!.status).toBe("reconnecting");
    // 500ms backoff
    act(() => vi.advanceTimersByTime(500));
    await waitFor(() => expect(FakeWebSocket.instances.length).toBe(2));
    const grants = mockGrant.mock.calls.length;
    expect(grants).toBeGreaterThanOrEqual(2); // fresh grant per attempt
    // 1s
    act(() => {
      lastSocket().serverClose();
    });
    act(() => vi.advanceTimersByTime(500));
    expect(FakeWebSocket.instances.length).toBe(2);
    act(() => vi.advanceTimersByTime(500));
    await waitFor(() => expect(FakeWebSocket.instances.length).toBe(3));
    // 2s
    act(() => {
      lastSocket().serverClose();
    });
    act(() => vi.advanceTimersByTime(3500));
    await waitFor(() => expect(FakeWebSocket.instances.length).toBe(4));
    // cap at 5s
    act(() => {
      lastSocket().serverClose();
    });
    act(() => vi.advanceTimersByTime(4999));
    expect(FakeWebSocket.instances.length).toBe(4);
    act(() => vi.advanceTimersByTime(1));
    await waitFor(() => expect(FakeWebSocket.instances.length).toBe(5));
  });

  it("error message sets error state, closes, and does not reconnect", async () => {
    render(<Harness stateKey="tab:abc" base="http://127.0.0.1:54321" enabled />);
    await waitFor(() => expect(FakeWebSocket.instances.length).toBe(1));
    const ws = lastSocket();
    act(() => {
      ws.serverOpen();
      ws.serverMessage(JSON.stringify({ t: "error", message: "chrome not found — set browser.chrome_path" }));
    });
    expect(lastApi!.status).toBe("closed");
    expect(lastApi!.error).toBe("chrome not found — set browser.chrome_path");
    act(() => vi.advanceTimersByTime(10000));
    expect(FakeWebSocket.instances.length).toBe(1); // no reconnect
  });

  it("unmount closes the socket without reconnecting", async () => {
    const { unmount } = render(<Harness stateKey="tab:abc" base="http://127.0.0.1:54321" enabled />);
    await waitFor(() => expect(FakeWebSocket.instances.length).toBe(1));
    const ws = lastSocket();
    act(() => ws.serverOpen());
    unmount();
    expect(ws.closed).toBe(true);
    act(() => vi.advanceTimersByTime(10000));
    expect(FakeWebSocket.instances.length).toBe(1);
  });

  it("toggle enabled=false closes the socket", async () => {
    const { rerender } = render(<Harness stateKey="tab:abc" base="http://127.0.0.1:54321" enabled />);
    await waitFor(() => expect(FakeWebSocket.instances.length).toBe(1));
    const ws = lastSocket();
    act(() => ws.serverOpen());
    rerender(<Harness stateKey="tab:abc" base="http://127.0.0.1:54321" enabled={false} />);
    expect(ws.closed).toBe(true);
  });
});
