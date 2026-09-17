import { describe, expect, it } from "vitest";
import {
  editorTabProjectKey,
  resolveVisibleEditorTabId,
  visibleEditorTabs,
} from "./editorTabsPersistence";
import { projectSessionKey } from "../../stores/projectStore";

type Tab = { id: string; path: string; projectRoot?: string; projectHost?: string };

const remote: Tab = { id: "editor-me@ssh::/proj::a.ts", path: "a.ts", projectRoot: "/proj", projectHost: "me@ssh" };
const local: Tab = { id: "editor-/proj::a.ts", path: "a.ts", projectRoot: "/proj" };
const otherLocal: Tab = { id: "editor-/other::b.ts", path: "b.ts", projectRoot: "/other" };

describe("editorTabProjectKey", () => {
  it("matches the session-cache key convention (host-qualified)", () => {
    for (const [path, host] of [
      ["/proj", undefined],
      ["/proj", ""],
      ["/proj", "me@ssh"],
      ["", "me@ssh"],
      ["/proj", "host:2222"],
    ] as const) {
      expect(editorTabProjectKey({ projectRoot: path, projectHost: host })).toBe(
        projectSessionKey(path, host),
      );
    }
  });
});

describe("visibleEditorTabs", () => {
  const tabs = [remote, local, otherLocal];

  it("returns only the active project's tabs (host-qualified)", () => {
    expect(visibleEditorTabs(tabs, { path: "/proj", host: "me@ssh" })).toEqual([remote]);
    expect(visibleEditorTabs(tabs, { path: "/proj" })).toEqual([local]);
    expect(visibleEditorTabs(tabs, { path: "/other" })).toEqual([otherLocal]);
  });

  it("does not confuse a remote and a local project that share the same path", () => {
    const visible: Tab[] = visibleEditorTabs(tabs, { path: "/proj" });
    expect(visible).not.toContain(remote);
  });

  it("returns every tab when no project is active yet (boot/deep-link)", () => {
    expect(visibleEditorTabs(tabs, null)).toEqual(tabs);
    expect(visibleEditorTabs(tabs, undefined)).toEqual(tabs);
    expect(visibleEditorTabs(tabs, { path: "" })).toEqual(tabs);
  });
});

describe("resolveVisibleEditorTabId", () => {
  it("keeps the requested id when it is visible", () => {
    expect(resolveVisibleEditorTabId([remote, local], local.id)).toBe(local.id);
  });

  it("falls back to the most recent visible tab when the active id is hidden", () => {
    // Active tab belongs to the remote project; the visible set is local only.
    expect(resolveVisibleEditorTabId([otherLocal, local], remote.id)).toBe(local.id);
  });

  it("returns null when nothing is visible", () => {
    expect(resolveVisibleEditorTabId([], remote.id)).toBeNull();
  });
});
