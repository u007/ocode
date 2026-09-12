import { describe, it, expect } from "vitest";
import { computeProjectDrag, projectDragKey } from "./projectDrag";

// Mirrors the real data shape: groups render first (by order), then ungrouped.
const groups = [
  { name: "old", order: 1, collapsed: true },
  { name: "aims", order: 2, collapsed: false },
];

const projects = [
  { path: "/g/james", group: "old" },
  { path: "/g/tmp", group: "old" },
  { path: "/a/aimsai2", group: "aims" },
  { path: "/u/ocode", group: "" },
  { path: "/u/kakiit", group: "" },
  { path: "/u/nanobot", group: "" },
];

// Callers must pass scoped keys (projectDragKey): local entries key on the
// path, remote entries on (host, verbatim path), so the same path on two
// hosts never collides.
const k = (path: string, host?: string) => projectDragKey(path, host);
const ref = (path: string, host?: string) => (host ? { path, host } : { path });

describe("computeProjectDrag", () => {
  it("reorders within the ungrouped bucket (drag up)", () => {
    const res = computeProjectDrag(projects, groups, k("/u/nanobot"), k("/u/ocode"));
    expect(res).toEqual({
      type: "reorder",
      refs: [ref("/g/james"), ref("/g/tmp"), ref("/a/aimsai2"), ref("/u/nanobot"), ref("/u/ocode"), ref("/u/kakiit")],
    });
  });

  it("reorders within the ungrouped bucket (drag down)", () => {
    const res = computeProjectDrag(projects, groups, k("/u/ocode"), k("/u/nanobot"));
    expect(res).toEqual({
      type: "reorder",
      refs: [ref("/g/james"), ref("/g/tmp"), ref("/a/aimsai2"), ref("/u/kakiit"), ref("/u/nanobot"), ref("/u/ocode")],
    });
  });

  it("reorders within a group bucket", () => {
    const res = computeProjectDrag(projects, groups, k("/g/tmp"), k("/g/james"));
    expect(res).toEqual({
      type: "reorder",
      refs: [ref("/g/tmp"), ref("/g/james"), ref("/a/aimsai2"), ref("/u/ocode"), ref("/u/kakiit"), ref("/u/nanobot")],
    });
  });

  it("moves an ungrouped project into a group when dropped on a grouped project", () => {
    const res = computeProjectDrag(projects, groups, k("/u/ocode"), k("/g/tmp"));
    expect(res).toEqual({
      type: "move",
      ref: ref("/u/ocode"),
      group: "old",
      refs: [ref("/g/james"), ref("/u/ocode"), ref("/g/tmp"), ref("/a/aimsai2"), ref("/u/kakiit"), ref("/u/nanobot")],
    });
  });

  it("moves a grouped project out to ungrouped when dropped on an ungrouped project", () => {
    const res = computeProjectDrag(projects, groups, k("/a/aimsai2"), k("/u/kakiit"));
    expect(res).toEqual({
      type: "move",
      ref: ref("/a/aimsai2"),
      group: "",
      refs: [ref("/g/james"), ref("/g/tmp"), ref("/u/ocode"), ref("/u/kakiit"), ref("/a/aimsai2"), ref("/u/nanobot")],
    });
  });

  it("moves a project into a group when dropped on the group header (appended at end)", () => {
    const res = computeProjectDrag(projects, groups, k("/u/ocode"), "group:old");
    expect(res).toEqual({
      type: "move",
      ref: ref("/u/ocode"),
      group: "old",
      refs: [ref("/g/james"), ref("/g/tmp"), ref("/u/ocode"), ref("/a/aimsai2"), ref("/u/kakiit"), ref("/u/nanobot")],
    });
  });

  it("dropping on the header of the project's own group is a no-op", () => {
    const res = computeProjectDrag(projects, groups, k("/g/tmp"), "group:old");
    expect(res).toEqual({ type: "none" });
  });

  it("returns none for unknown ids", () => {
    expect(computeProjectDrag(projects, groups, k("/nope"), k("/u/ocode"))).toEqual({ type: "none" });
    expect(computeProjectDrag(projects, groups, k("/u/ocode"), k("/nope"))).toEqual({ type: "none" });
  });

  it("keeps same-path projects on different hosts distinct", () => {
    const mixed = [
      { path: "/home/user/app", group: "" },
      { path: "/home/user/app", group: "", host: "devbox" },
      { path: "/home/user/app", group: "", host: "wsl:Ubuntu" },
    ];
    // Dragging the SSH entry onto the WSL entry reorders by scoped identity.
    const res = computeProjectDrag(mixed, [], k("/home/user/app", "devbox"), k("/home/user/app", "wsl:Ubuntu"));
    expect(res).toEqual({
      type: "reorder",
      refs: [
        ref("/home/user/app"),
        ref("/home/user/app", "wsl:Ubuntu"),
        ref("/home/user/app", "devbox"),
      ],
    });
  });

  it("moves a remote entry into a group with its host preserved", () => {
    const mixed = [
      { path: "/g/james", group: "old" },
      { path: "/home/user/app", group: "", host: "devbox" },
    ];
    const res = computeProjectDrag(
      mixed,
      [{ name: "old", order: 1 }],
      k("/home/user/app", "devbox"),
      "group:old",
    );
    expect(res).toEqual({
      type: "move",
      ref: ref("/home/user/app", "devbox"),
      group: "old",
      refs: [ref("/g/james"), ref("/home/user/app", "devbox")],
    });
  });

  it("projectDragKey keeps local and remote identities distinct", () => {
    expect(k("/home/user/app")).not.toBe(k("/home/user/app", "devbox"));
    expect(k("/home/user/app", "devbox")).not.toBe(k("/home/user/app", "wsl:Ubuntu"));
    expect(k("/home/user/app")).toBe(projectDragKey("/home/user/app"));
  });
});
