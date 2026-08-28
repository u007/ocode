import { describe, it, expect } from "vitest";
import { computeProjectDrag } from "./projectDrag";

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

describe("computeProjectDrag", () => {
  it("reorders within the ungrouped bucket (drag up)", () => {
    const res = computeProjectDrag(projects, groups, "/u/nanobot", "/u/ocode");
    expect(res).toEqual({
      type: "reorder",
      paths: ["/g/james", "/g/tmp", "/a/aimsai2", "/u/nanobot", "/u/ocode", "/u/kakiit"],
    });
  });

  it("reorders within the ungrouped bucket (drag down)", () => {
    const res = computeProjectDrag(projects, groups, "/u/ocode", "/u/nanobot");
    expect(res).toEqual({
      type: "reorder",
      paths: ["/g/james", "/g/tmp", "/a/aimsai2", "/u/kakiit", "/u/nanobot", "/u/ocode"],
    });
  });

  it("reorders within a group bucket", () => {
    const res = computeProjectDrag(projects, groups, "/g/tmp", "/g/james");
    expect(res).toEqual({
      type: "reorder",
      paths: ["/g/tmp", "/g/james", "/a/aimsai2", "/u/ocode", "/u/kakiit", "/u/nanobot"],
    });
  });

  it("moves an ungrouped project into a group when dropped on a grouped project", () => {
    const res = computeProjectDrag(projects, groups, "/u/ocode", "/g/tmp");
    expect(res).toEqual({
      type: "move",
      path: "/u/ocode",
      group: "old",
      paths: ["/g/james", "/u/ocode", "/g/tmp", "/a/aimsai2", "/u/kakiit", "/u/nanobot"],
    });
  });

  it("moves a grouped project out to ungrouped when dropped on an ungrouped project", () => {
    const res = computeProjectDrag(projects, groups, "/a/aimsai2", "/u/kakiit");
    expect(res).toEqual({
      type: "move",
      path: "/a/aimsai2",
      group: "",
      paths: ["/g/james", "/g/tmp", "/u/ocode", "/u/kakiit", "/a/aimsai2", "/u/nanobot"],
    });
  });

  it("moves a project into a group when dropped on the group header (appended at end)", () => {
    const res = computeProjectDrag(projects, groups, "/u/ocode", "group:old");
    expect(res).toEqual({
      type: "move",
      path: "/u/ocode",
      group: "old",
      paths: ["/g/james", "/g/tmp", "/u/ocode", "/a/aimsai2", "/u/kakiit", "/u/nanobot"],
    });
  });

  it("dropping on the header of the project's own group is a no-op", () => {
    const res = computeProjectDrag(projects, groups, "/g/tmp", "group:old");
    expect(res).toEqual({ type: "none" });
  });

  it("returns none for unknown ids", () => {
    expect(computeProjectDrag(projects, groups, "/nope", "/u/ocode")).toEqual({ type: "none" });
    expect(computeProjectDrag(projects, groups, "/u/ocode", "/nope")).toEqual({ type: "none" });
  });
});
