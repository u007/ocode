import { describe, expect, it } from "vitest";
import { readSSEStream } from "./client";

function sseResponse(chunks: string[]): Response {
  const encoder = new TextEncoder();
  const stream = new ReadableStream<Uint8Array>({
    start(controller) {
      for (const c of chunks) controller.enqueue(encoder.encode(c));
      controller.close();
    },
  });
  return new Response(stream, {
    status: 200,
    headers: { "Content-Type": "text/event-stream" },
  });
}

describe("readSSEStream", () => {
  it("dispatches JSON data to the handler for each event in order", async () => {
    const events: { name: string; data: unknown }[] = [];
    await readSSEStream<unknown>(
      sseResponse([
        'event: result\ndata: {"results":[{"path":"a","line":1}]}\n\n',
        'event: result\ndata: {"results":[{"path":"b","line":2}]}\n\n',
        'event: done\ndata: {"total":2,"capped":false}\n\n',
      ]),
      {
        result: (data) => events.push({ name: "result", data }),
        done: (data) => events.push({ name: "done", data }),
      },
    );
    expect(events).toEqual([
      { name: "result", data: { results: [{ path: "a", line: 1 }] } },
      { name: "result", data: { results: [{ path: "b", line: 2 }] } },
      { name: "done", data: { total: 2, capped: false } },
    ]);
  });

  it("handles frames split across arbitrary chunk boundaries", async () => {
    const events: unknown[] = [];
    await readSSEStream(
      sseResponse([
        "event: re",
        "sult\ndata: {",
        '"hello":"wor',
        'ld"}\n\nevent: done\ndata: {}',
        "\n\n",
      ]),
      {
        result: (data) => events.push(data),
        done: (data) => events.push(data),
      },
    );
    expect(events).toEqual([{ hello: "world" }, {}]);
  });

  it("tolerates CRLF frame separators", async () => {
    const events: unknown[] = [];
    await readSSEStream(
      sseResponse(['event: result\r\ndata: {"a":1}\r\n\r\nevent: done\r\ndata: {}\r\n\r\n']),
      {
        result: (data) => events.push(data),
        done: (data) => events.push(data),
      },
    );
    expect(events).toEqual([{ a: 1 }, {}]);
  });

  it("ignores comment frames and handles a trailing frame without a blank line", async () => {
    const events: unknown[] = [];
    await readSSEStream(
      sseResponse([": search started\n\nevent: result\ndata: {\"x\":1}\n"]),
      {
        result: (data) => events.push(data),
        message: (data) => events.push(data),
      },
    );
    expect(events).toEqual([{ x: 1 }]);
  });

  it("dispatches frames without an event line to the message handler", async () => {
    const events: unknown[] = [];
    await readSSEStream(sseResponse(['data: {"plain":true}\n\n']), {
      message: (data) => events.push(data),
    });
    expect(events).toEqual([{ plain: true }]);
  });

  it("passes non-JSON data through as a raw string", async () => {
    const events: unknown[] = [];
    await readSSEStream(sseResponse(["event: note\ndata: plain text\n\n"]), {
      note: (data) => events.push(data),
    });
    expect(events).toEqual(["plain text"]);
  });

  it("rejects when the underlying stream errors (fetch abort propagates)", async () => {
    const encoder = new TextEncoder();
    let fail: (reason?: unknown) => void = () => {};
    const stream = new ReadableStream<Uint8Array>({
      start(c) {
        c.enqueue(encoder.encode('event: result\ndata: {"a":1}\n\n'));
        // Simulate the fetch abort: the body read rejects mid-stream.
        fail = (reason) => c.error(reason);
      },
    });
    const res = new Response(stream, { status: 200 });
    const p = readSSEStream(res, { result: () => {} });
    setTimeout(
      () => fail(new DOMException("The operation was aborted.", "AbortError")),
      0,
    );
    await expect(p).rejects.toThrow(/abort/i);
  });
});