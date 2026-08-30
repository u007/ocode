import { describe, it, expect, beforeEach } from "vitest";
import { loadTabOrder, saveTabOrder, reconcileTabOrder } from "./tabOrderPersistence";

beforeEach(() => {
  window.localStorage.clear();
});

describe("tabOrderPersistence", () => {
  it("returns an empty order for a project with nothing saved", () => {
    expect(loadTabOrder("/proj")).toEqual([]);
  });

  it("round-trips a saved order", () => {
    saveTabOrder("/proj", ["term:t1", "chat:c1"]);
    expect(loadTabOrder("/proj")).toEqual(["term:t1", "chat:c1"]);
  });

  it("removes the project entry when saved with an empty order", () => {
    saveTabOrder("/proj", ["chat:c1"]);
    saveTabOrder("/proj", []);
    expect(loadTabOrder("/proj")).toEqual([]);
  });
});

describe("reconcileTabOrder", () => {
  it("keeps the saved order for ids that are still live", () => {
    expect(reconcileTabOrder(["term:t1", "chat:c1"], ["c1"], ["t1"])).toEqual(["term:t1", "chat:c1"]);
  });

  it("drops stale keys no longer present in either live id set", () => {
    expect(reconcileTabOrder(["term:t1", "chat:c1", "chat:closed"], ["c1"], ["t1"])).toEqual([
      "term:t1",
      "chat:c1",
    ]);
  });

  it("appends new live ids missing from the saved order, at the end", () => {
    expect(reconcileTabOrder(["chat:c1"], ["c1", "c2"], ["t1"])).toEqual(["chat:c1", "chat:c2", "term:t1"]);
  });

  it("keeps saved interleaving of chat, term, and browser keys", () => {
    const saved = ["term:t1", "browser:b1", "chat:c1"];
    expect(reconcileTabOrder(saved, ["c1"], ["t1"], ["b1"])).toEqual(["term:t1", "browser:b1", "chat:c1"]);
  });

  it("appends new browser ids missing from a legacy saved order", () => {
    expect(reconcileTabOrder(["chat:c1"], ["c1"], [], ["b1", "b2"])).toEqual([
      "chat:c1",
      "browser:b1",
      "browser:b2",
    ]);
  });

  it("drops stale browser keys whose id is gone", () => {
    expect(reconcileTabOrder(["browser:gone", "chat:c1"], ["c1"], [], [])).toEqual(["chat:c1"]);
  });
});
