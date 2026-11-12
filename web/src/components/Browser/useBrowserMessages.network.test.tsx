// The capture bundle → store boundary for network telemetry.
//
// capture.js posts a flat message; the DevConsole renders a NetworkEvent. A
// field-name mismatch here is silent: the row still renders (status/url/method
// survive) but loses its headers/body, which is exactly the reported "cannot
// expand to see request header and response header and body" symptom. So this
// pins the whole payload mapping, not just that a row appeared.
import { describe, it, expect, beforeEach, vi } from "vitest";
import { act, renderHook } from "@testing-library/react";
import { useBrowserMessages } from "./useBrowserMessages";

const ORIGIN = "http://browse.test:5001";
const STATE_KEY = "side:chat:abc" as const;
const BASE = "/browse";

function setup() {
  const frame = { name: "iframe" } as unknown as Window;
  const handlers = {
    pushConsole: vi.fn(),
    pushNetwork: vi.fn(),
    pushResponseBody: vi.fn(),
    fillNetworkStatus: vi.fn(),
    onTitle: vi.fn(),
    onScroll: vi.fn(),
    onNav: vi.fn(),
  };
  const view = renderHook(() =>
    useBrowserMessages(
      STATE_KEY,
      BASE,
      {
        originRef: { current: ORIGIN },
        frameRef: { current: { contentWindow: frame } as unknown as HTMLIFrameElement },
      },
      handlers,
    ),
  );
  const post = (data: Record<string, unknown>) =>
    act(() => {
      window.dispatchEvent(
        new MessageEvent("message", { data, origin: ORIGIN, source: frame as unknown as MessageEventSource }),
      );
    });
  return { ...view, handlers, post };
}

beforeEach(() => {
  vi.clearAllMocks();
});

describe("useBrowserMessages network mapping", () => {
  it("carries request/response headers, the request body and the content type", () => {
    const { post, handlers } = setup();
    post({
      type: "ocode:browse:network",
      stateKey: STATE_KEY,
      requestId: "local-1-1",
      method: "POST",
      url: "https://api.dev/collect",
      status: 204,
      duration: 7,
      ts: 1000,
      requestHeaders: { "content-type": "application/json" },
      responseHeaders: { "x-req": "1" },
      postData: '{"n":1}',
      contentType: "application/json",
      size: 128,
    });

    expect(handlers.pushNetwork).toHaveBeenCalledTimes(1);
    const [key, ev] = handlers.pushNetwork.mock.calls[0];
    expect(key).toBe(STATE_KEY);
    expect(ev).toMatchObject({
      requestId: "local-1-1",
      method: "POST",
      url: "https://api.dev/collect",
      status: 204,
      durationMs: 7,
      requestHeaders: { "content-type": "application/json" },
      responseHeaders: { "x-req": "1" },
      postData: '{"n":1}',
      contentType: "application/json",
      size: 128,
    });
    // No inline body in this message → nothing stored.
    expect(handlers.pushResponseBody).not.toHaveBeenCalled();
  });

  it("stores a body from a networkbody message under the same requestId, without a second row", () => {
    const { post, handlers } = setup();
    post({
      type: "ocode:browse:network",
      stateKey: STATE_KEY,
      requestId: "local-2-1",
      method: "GET",
      url: "https://api.dev/x",
      status: 200,
      duration: 3,
      ts: 2,
    });
    post({
      type: "ocode:browse:networkbody",
      stateKey: STATE_KEY,
      requestId: "local-2-1",
      responseBody: '{"ok":true}',
      responseBodyTruncated: true,
    });

    expect(handlers.pushNetwork).toHaveBeenCalledTimes(1);
    expect(handlers.pushResponseBody).toHaveBeenCalledTimes(1);
    expect(handlers.pushResponseBody).toHaveBeenCalledWith(STATE_KEY, "local-2-1", {
      body: '{"ok":true}',
      base64Encoded: false,
      truncated: true,
    });
  });

  it("ignores a networkbody message without a requestId or string body", () => {
    const { post, handlers } = setup();
    post({ type: "ocode:browse:networkbody", stateKey: STATE_KEY, responseBody: "orphan" });
    post({ type: "ocode:browse:networkbody", stateKey: STATE_KEY, requestId: "local-3-1", responseBody: 42 });

    expect(handlers.pushNetwork).not.toHaveBeenCalled();
    expect(handlers.pushResponseBody).not.toHaveBeenCalled();
    expect(handlers.fillNetworkStatus).not.toHaveBeenCalled();
  });

  it("fills the row status from a networkbody message that carries a status and a body", () => {
    const { post, handlers } = setup();
    post({ type: "ocode:browse:networkbody", stateKey: STATE_KEY, requestId: "local-4-1", status: 200, responseBody: "x" });

    expect(handlers.fillNetworkStatus).toHaveBeenCalledWith(STATE_KEY, "local-4-1", 200);
    expect(handlers.pushResponseBody).toHaveBeenCalledWith(STATE_KEY, "local-4-1", {
      body: "x",
      base64Encoded: false,
      truncated: false,
    });
    expect(handlers.pushNetwork).not.toHaveBeenCalled();
  });

  it("fills the status from a status-only networkbody message and does not touch the stored body", () => {
    const { post, handlers } = setup();
    post({ type: "ocode:browse:networkbody", stateKey: STATE_KEY, requestId: "local-4-2", status: 404 });

    expect(handlers.fillNetworkStatus).toHaveBeenCalledTimes(1);
    expect(handlers.fillNetworkStatus).toHaveBeenCalledWith(STATE_KEY, "local-4-2", 404);
    expect(handlers.pushResponseBody).not.toHaveBeenCalled();
    expect(handlers.pushNetwork).not.toHaveBeenCalled();
  });

  it("ignores a non-numeric status on a networkbody message", () => {
    const { post, handlers } = setup();
    post({ type: "ocode:browse:networkbody", stateKey: STATE_KEY, requestId: "local-4-3", status: "200" });

    expect(handlers.fillNetworkStatus).not.toHaveBeenCalled();
  });

  it("stores an XHR row's inline response body under the same requestId", () => {
    // fetch posts its body LATER (a networkbody message, the response stream is
    // read asynchronously), but XHR has the text synchronously at `loadend` and
    // carries it on the row itself. Dropping that field silently cost every XHR
    // row its Body tab — the exact "cannot see the body" symptom.
    const { post, handlers } = setup();
    post({
      type: "ocode:browse:network",
      stateKey: STATE_KEY,
      requestId: "local-9-1",
      method: "POST",
      url: "https://api.dev/xhr",
      status: 200,
      duration: 4,
      ts: 5,
      responseBody: '{"done":true}',
      responseBodyTruncated: true,
    });

    expect(handlers.pushNetwork).toHaveBeenCalledTimes(1);
    expect(handlers.pushResponseBody).toHaveBeenCalledWith(STATE_KEY, "local-9-1", {
      body: '{"done":true}',
      base64Encoded: false,
      truncated: true,
    });
  });

  it("falls back to `duration` and never stores a body without a requestId", () => {
    const { post, handlers } = setup();
    post({
      type: "ocode:browse:network",
      stateKey: STATE_KEY,
      method: "GET",
      url: "https://api.dev/legacy",
      status: 200,
      duration: 9,
      ts: 3,
      responseBody: "unreachable",
    });

    const ev = handlers.pushNetwork.mock.calls[0][1];
    expect(ev.durationMs).toBe(9);
    expect(ev.requestId).toBe("");
    expect(handlers.pushResponseBody).not.toHaveBeenCalled();
  });
});
