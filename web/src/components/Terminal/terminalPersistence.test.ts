import { beforeEach, describe, expect, it } from "vitest";
import { loadProjectTerminals, saveProjectTerminals, projectTerminalsKey } from "./terminalPersistence";

describe("projectTerminalsKey", () => {
  it("qualifies remote projects as host::path and leaves local projects bare", () => {
    expect(projectTerminalsKey("/p")).toBe("/p");
    expect(projectTerminalsKey("/p", "user@box")).toBe("user@box::/p");
  });
});

describe("loadProjectTerminals / saveProjectTerminals", () => {
  beforeEach(() => {
    window.localStorage.clear();
  });

  it("round-trips a remote project under the host-qualified key", () => {
    saveProjectTerminals("/p", [{ id: "t1", title: "T" }], "t1", "user@box");
    expect(loadProjectTerminals("/p", "user@box")?.terminals[0]?.id).toBe("t1");
    // A local project at the same path must not see the remote project's tabs.
    expect(loadProjectTerminals("/p")).toBeNull();
  });

  it("still loads a pre-existing bare-path entry for a local project", () => {
    saveProjectTerminals("/p", [{ id: "t2", title: "L" }], "t2");
    expect(loadProjectTerminals("/p")?.terminals[0]?.id).toBe("t2");
  });

  it("keeps local and remote entries at the same path separate", () => {
    saveProjectTerminals("/p", [{ id: "local", title: "L" }], "local");
    saveProjectTerminals("/p", [{ id: "remote", title: "R" }], "remote", "h");
    expect(loadProjectTerminals("/p")?.terminals[0]?.id).toBe("local");
    expect(loadProjectTerminals("/p", "h")?.terminals[0]?.id).toBe("remote");
  });
});
