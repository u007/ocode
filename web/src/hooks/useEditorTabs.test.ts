import { act, renderHook, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { api } from "../api/client";
import { useEditorTabs } from "./useEditorTabs";

vi.mock("../api/client", () => ({
  api: { saveFileContent: vi.fn() },
  apiPath: (p: string) => p,
  authHeaders: () => ({}),
}));

beforeEach(() => {
  // The hook persists/restores open tabs via localStorage; without clearing,
  // tabs opened by one test are restored into the next test's hook.
  window.localStorage.clear();
  vi.stubGlobal(
    "fetch",
    vi.fn().mockResolvedValue({
      ok: true,
      json: async () => ({ content: "hello" }),
    }),
  );
});

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("useEditorTabs", () => {
  it("starts with no active editor tab (file tree shown)", () => {
    const { result } = renderHook(() => useEditorTabs());
    expect(result.current.activeEditorTabId).toBeNull();
  });

  it("opening a file sets it as the active editor tab", async () => {
    const { result } = renderHook(() => useEditorTabs());
    await act(async () => {
      await result.current.handleOpenFile("/a/b.txt");
    });
    await waitFor(() => expect(result.current.activeEditorTabId).toBe("editor-/a/b.txt"));
    expect(result.current.editorTabs).toHaveLength(1);
  });

  it("preserves the project root for loading and saving a file", async () => {
    const { result } = renderHook(() => useEditorTabs());
    await act(async () => {
      await result.current.handleOpenFile("src/a.ts", "/projects/active");
    });

    expect(result.current.activeEditorTabId).toBe("editor-/projects/active::src/a.ts");
    expect(result.current.editorTabs[0].projectRoot).toBe("/projects/active");
    expect(fetch).toHaveBeenCalledWith(
      "/api/files/content?path=src%2Fa.ts&project_root=%2Fprojects%2Factive",
      { headers: {} },
    );

    await act(async () => {
      await result.current.saveEditorTab(result.current.editorTabs[0].id);
    });
    expect(api.saveFileContent).toHaveBeenCalledWith("src/a.ts", "hello", "/projects/active");
  });

  it("closing the active tab falls back to null, not a string sentinel", async () => {
    const { result } = renderHook(() => useEditorTabs());
    await act(async () => {
      await result.current.handleOpenFile("/a/b.txt");
    });
    await waitFor(() => expect(result.current.activeEditorTabId).toBe("editor-/a/b.txt"));
    act(() => {
      result.current.requestCloseTab("editor-/a/b.txt");
    });
    expect(result.current.activeEditorTabId).toBeNull();
    expect(result.current.editorTabs).toHaveLength(0);
  });

  it("restores previously open tabs on mount, skipping files that fail to load", async () => {
    const first = renderHook(() => useEditorTabs());
    await act(async () => {
      await first.result.current.handleOpenFile("/a/b.txt");
      await first.result.current.handleOpenFile("src/a.ts", "/projects/active");
    });
    await waitFor(() => expect(first.result.current.editorTabs).toHaveLength(2));
    first.unmount();

    // Second file is now missing on disk — its fetch fails on restore.
    vi.stubGlobal(
      "fetch",
      vi.fn().mockImplementation(async (url: string) => {
        if (url.includes("project_root")) return { ok: false, json: async () => ({}) };
        return { ok: true, json: async () => ({ content: "hello" }) };
      }),
    );

    const second = renderHook(() => useEditorTabs());
    await waitFor(() => expect(second.result.current.editorTabs).toHaveLength(1));
    expect(second.result.current.editorTabs[0].path).toBe("/a/b.txt");
  });

  it("restores unsaved edits as a dirty draft after a reload", async () => {
    const first = renderHook(() => useEditorTabs());
    await act(async () => {
      await first.result.current.handleOpenFile("/a/b.txt");
    });
    act(() => {
      first.result.current.handleEditorChange("editor-/a/b.txt", "hello edited");
    });
    // beforeunload flushes the debounced draft synchronously (reload path);
    // dispatched after act so the edit is committed when the flush reads it.
    act(() => {
      window.dispatchEvent(new Event("beforeunload"));
    });
    first.unmount();

    const second = renderHook(() => useEditorTabs());
    await waitFor(() => expect(second.result.current.editorTabs).toHaveLength(1));
    const tab = second.result.current.editorTabs[0];
    expect(tab.content).toBe("hello edited");
    expect(tab.originalContent).toBe("hello");
    expect(tab.isDirty).toBe(true);
    expect(tab.externalChange).toBe(false);
  });

  it("flags a restored draft when the file changed on disk underneath it", async () => {
    const first = renderHook(() => useEditorTabs());
    await act(async () => {
      await first.result.current.handleOpenFile("/a/b.txt");
    });
    act(() => {
      first.result.current.handleEditorChange("editor-/a/b.txt", "hello edited");
    });
    act(() => {
      window.dispatchEvent(new Event("beforeunload"));
    });
    first.unmount();

    // File modified outside the app while we were away.
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue({ ok: true, json: async () => ({ content: "changed on disk" }) }),
    );

    const second = renderHook(() => useEditorTabs());
    await waitFor(() => expect(second.result.current.editorTabs).toHaveLength(1));
    const tab = second.result.current.editorTabs[0];
    expect(tab.content).toBe("hello edited");
    expect(tab.originalContent).toBe("changed on disk");
    expect(tab.isDirty).toBe(true);
    expect(tab.externalChange).toBe(true);
  });

  it("restores a draft even when the file was deleted on disk", async () => {
    const first = renderHook(() => useEditorTabs());
    await act(async () => {
      await first.result.current.handleOpenFile("/a/b.txt");
    });
    act(() => {
      first.result.current.handleEditorChange("editor-/a/b.txt", "hello edited");
    });
    act(() => {
      window.dispatchEvent(new Event("beforeunload"));
    });
    first.unmount();

    vi.stubGlobal("fetch", vi.fn().mockResolvedValue({ ok: false, json: async () => ({}) }));

    const second = renderHook(() => useEditorTabs());
    await waitFor(() => expect(second.result.current.editorTabs).toHaveLength(1));
    const tab = second.result.current.editorTabs[0];
    expect(tab.content).toBe("hello edited");
    expect(tab.isDirty).toBe(true);
    expect(tab.externalChange).toBe(true);
  });

  it("reloadTabFromDisk discards the draft in favour of disk content", async () => {
    const { result } = renderHook(() => useEditorTabs());
    await act(async () => {
      await result.current.handleOpenFile("/a/b.txt");
    });
    act(() => {
      result.current.handleEditorChange("editor-/a/b.txt", "hello edited");
    });
    act(() => {
      window.dispatchEvent(new Event("beforeunload"));
    });
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue({ ok: true, json: async () => ({ content: "disk wins" }) }),
    );
    await act(async () => {
      await result.current.reloadTabFromDisk("editor-/a/b.txt");
    });
    const tab = result.current.editorTabs[0];
    expect(tab.content).toBe("disk wins");
    expect(tab.isDirty).toBe(false);
    expect(tab.externalChange).toBe(false);
    expect(window.localStorage.getItem("ocode.editor.draft.editor-/a/b.txt")).toBeNull();
  });
});
