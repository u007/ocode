import { beforeEach, describe, expect, it } from "vitest";
import {
  __applyBtwFrameForTests,
  __resetBtwStoreForTests,
  getBtwState,
  rekeyBtw,
  startBtw,
} from "./btwStore";

describe("rekeyBtw", () => {
  beforeEach(() => __resetBtwStoreForTests());

  it("marks an in-flight panel cancelled and resets the generation", () => {
    __applyBtwFrameForTests("h", "s1", { generation: 3, phase: "started", question: "q" });
    rekeyBtw("s1", "s2");
    const st = getBtwState("s2", "h")!;
    expect(st.loading).toBe(false);
    expect(st.error).toMatch(/cancelled/);
    expect(st.generation).toBe(0);
    expect(getBtwState("s1", "h")).toBeUndefined();
  });

  it("keeps a finished panel's answer and error untouched", () => {
    __applyBtwFrameForTests("h", "s1", { generation: 3, phase: "started", question: "q" });
    __applyBtwFrameForTests("h", "s1", { generation: 3, phase: "done", text: "ans" });
    rekeyBtw("s1", "s2");
    const st = getBtwState("s2", "h")!;
    expect(st.answer).toBe("ans");
    expect(st.error).toBeUndefined();
  });
});

describe("startBtw", () => {
  beforeEach(() => __resetBtwStoreForTests());

  it("keeps frames that arrived before the 202 for the same question", () => {
    __applyBtwFrameForTests("h", "s1", { generation: 1, phase: "started", question: "q" });
    __applyBtwFrameForTests("h", "s1", { generation: 1, phase: "activity", text: "→ read a.go" });
    __applyBtwFrameForTests("h", "s1", { generation: 1, phase: "delta", text: "txt" });
    startBtw("s1", "h", "q");
    const st = getBtwState("s1", "h")!;
    expect(st.activity).toEqual(["→ read a.go"]);
    expect(st.answer).toBe("txt");
    expect(st.loading).toBe(true);
  });

  it("keeps a terminal frame that arrived before the 202", () => {
    __applyBtwFrameForTests("h", "s1", { generation: 1, phase: "started", question: "q" });
    __applyBtwFrameForTests("h", "s1", { generation: 1, phase: "error", error: "boom" });
    startBtw("s1", "h", "q");
    const st = getBtwState("s1", "h")!;
    expect(st.loading).toBe(false);
    expect(st.error).toBe("boom");
  });

  it("resets a finished panel when a different question is asked", () => {
    __applyBtwFrameForTests("h", "s1", { generation: 1, phase: "started", question: "A" });
    __applyBtwFrameForTests("h", "s1", { generation: 1, phase: "done", text: "ans A" });
    startBtw("s1", "h", "B");
    const st = getBtwState("s1", "h")!;
    expect(st.question).toBe("B");
    expect(st.answer).toBe("");
    expect(st.loading).toBe(true);
    expect(st.generation).toBe(1);
  });
});
