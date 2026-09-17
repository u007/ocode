import { act, renderHook, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { useEditorTabs } from "./useEditorTabs";

vi.mock("../api/client", () => ({
  api: { saveFileContent: vi.fn() },
  apiPath: (p: string) => p,
  authHeaders: () => ({}),
}));

beforeEach(() => {
  window.localStorage.clear();
  vi.stubGlobal(
    "fetch",
    vi.fn().mockResolvedValue({ ok: true, json: async () => ({ content: "hello" }) }),
  );
});

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("useEditorTabs host scoping", () => {
  it("keeps the same path on two different hosts as two distinct tabs", async () => {
    const { result } = renderHook(() => useEditorTabs());

    await act(async () => {
      await result.current.handleOpenFile("src/a.ts", "/proj", "me@ssh-host");
    });
    await waitFor(() => expect(result.current.editorTabs).toHaveLength(1));

    // Same project path + relative file, but a local project (no host). This
    // must NOT be treated as the already-open remote tab.
    await act(async () => {
      await result.current.handleOpenFile("src/a.ts", "/proj");
    });
    await waitFor(() => expect(result.current.editorTabs).toHaveLength(2));

    const hosts = result.current.editorTabs.map((t) => t.projectHost ?? "");
    expect(hosts).toContain("me@ssh-host");
    expect(hosts).toContain("");
    expect(new Set(result.current.editorTabs.map((t) => t.id)).size).toBe(2);
  });

  it("preserves the remote host across a persist/restore cycle", async () => {
    const first = renderHook(() => useEditorTabs());
    await act(async () => {
      await first.result.current.handleOpenFile("src/a.ts", "/proj", "me@ssh-host");
    });
    await waitFor(() => expect(first.result.current.editorTabs).toHaveLength(1));
    first.unmount();

    const second = renderHook(() => useEditorTabs());
    await waitFor(() => expect(second.result.current.editorTabs).toHaveLength(1));
    expect(second.result.current.editorTabs[0].projectHost).toBe("me@ssh-host");
  });

  it("routes the content fetch with the host for each tab", async () => {
    const { result } = renderHook(() => useEditorTabs());
    await act(async () => {
      await result.current.handleOpenFile("src/a.ts", "/proj", "me@ssh-host");
    });
    const calls = (fetch as unknown as ReturnType<typeof vi.fn>).mock.calls.map((c) => String(c[0]));
    expect(calls.some((u) => u.includes("host=me%40ssh-host"))).toBe(true);
  });
});
