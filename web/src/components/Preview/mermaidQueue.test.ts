import { describe, expect, it } from "vitest";
import { runMermaid } from "./mermaidQueue";

const tick = () => new Promise<void>((resolve) => setTimeout(resolve, 0));

describe("runMermaid", () => {
  it("runs two overlapping tasks strictly in call order", async () => {
    const events: string[] = [];
    let releaseFirst: () => void = () => {};
    const first = runMermaid(async () => {
      events.push("first:start");
      await new Promise<void>((resolve) => {
        releaseFirst = resolve;
      });
      events.push("first:end");
      return "one";
    });
    const second = runMermaid(async () => {
      events.push("second:start");
      events.push("second:end");
      return "two";
    });

    await tick();
    expect(events).toEqual(["first:start"]);

    releaseFirst();

    await expect(first).resolves.toBe("one");
    await expect(second).resolves.toBe("two");
    expect(events).toEqual(["first:start", "first:end", "second:start", "second:end"]);
  });

  it("rejects the failing caller without wedging later tasks", async () => {
    const boom = new Error("Parse error on line 1");

    const failed = runMermaid(() => Promise.reject(boom));
    const after = runMermaid(async () => "ok");

    await expect(failed).rejects.toBe(boom);
    await expect(after).resolves.toBe("ok");
  });

  it("turns a synchronous throw into a rejection for its caller only", async () => {
    const boom = new Error("initialize failed");

    const failed = runMermaid((): Promise<string> => {
      throw boom;
    });
    const after = runMermaid(async () => "ok");

    await expect(failed).rejects.toBe(boom);
    await expect(after).resolves.toBe("ok");
  });
});
