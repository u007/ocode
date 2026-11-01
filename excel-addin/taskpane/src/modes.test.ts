import { describe, expect, it } from "vitest";
import { choiceFor, permissionFor, savedModeView, type ModeChoice } from "./modes";

describe("mode picker mapping", () => {
  it("maps each choice to a permission mode and the auto-permission switch", () => {
    expect(permissionFor("ask")).toEqual({ mode: "normal", auto: false });
    expect(permissionFor("auto")).toEqual({ mode: "normal", auto: true });
    expect(permissionFor("yolo")).toEqual({ mode: "yolo", auto: false });
  });
  it("reads a choice back from a mode and the switch, and round-trips every choice", () => {
    for (const c of ["ask", "auto", "yolo"] as ModeChoice[]) expect(choiceFor(permissionFor(c))).toBe(c);
    expect(choiceFor({ mode: "normal", auto: false })).toBe("ask");
    expect(choiceFor({ mode: "normal", auto: true })).toBe("auto");
  });
  it("lets yolo win over the auto switch when reading back", () => {
    expect(choiceFor({ mode: "yolo", auto: true })).toBe("yolo");
  });
  it.each([
    ["yolo", false, { choice: "yolo" }],
    ["yolo", true, { choice: "yolo" }],
    ["normal", false, { choice: "ask" }],
    ["normal", true, { choice: "auto" }],
    ["locked", false, { chip: "locked" }],
    ["locked", true, { chip: "locked" }],
    ["sandbox", false, { chip: "sandbox" }],
    ["sandbox", true, { chip: "sandbox" }],
  ] as const)("shows a saved %s chat (auto-permission %s) as %j", (mode, autoOn, want) => {
    expect(savedModeView(mode, autoOn)).toEqual(want);
  });
  it.each(["", "auto", "NORMAL", "yolo "])("refuses a mode ocode does not report (%j) instead of guessing", (mode) => {
    expect(() => savedModeView(mode, false)).toThrow(`unknown permission mode: ${mode}`);
  });
});
