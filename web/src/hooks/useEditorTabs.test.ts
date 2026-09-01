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
    expect(api.saveFileContent).toHaveBeenCalledWith("src/a.ts", "hello", "/projects/active", expect.any(String), undefined);
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

  it("rapid concurrent opens of the same file create exactly one tab", async () => {
    // Regression: the open-id guard used to run before the async content
    // fetch, so two rapid calls (double-click on a tree row) both passed it
    // and appended two tabs with the same id — two stacked Monaco editors
    // fighting over focus and caret position.
    let resolveFetch!: (v: { ok: boolean; json: () => Promise<{ content: string }> }) => void;
    vi.stubGlobal(
      "fetch",
      vi.fn().mockImplementation(
        () =>
          new Promise((resolve) => {
            resolveFetch = resolve;
          }),
      ),
    );

    const { result } = renderHook(() => useEditorTabs());
    // Fire two opens without awaiting the first — both enter the fetch phase.
    const first = result.current.handleOpenFile("/a/b.txt");
    const second = result.current.handleOpenFile("/a/b.txt");
    await act(async () => {
      resolveFetch({ ok: true, json: async () => ({ content: "hello" }) });
      await Promise.all([first, second]);
    });

    expect(result.current.editorTabs).toHaveLength(1);
    expect(result.current.editorTabs[0].id).toBe("editor-/a/b.txt");
    expect(result.current.activeEditorTabId).toBe("editor-/a/b.txt");
  });

  it("a failed open releases its claim so the file can be reopened", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue({ ok: false, json: async () => ({}) }),
    );
    const { result } = renderHook(() => useEditorTabs());
    await act(async () => {
      await result.current.handleOpenFile("/missing.txt");
    });
    expect(result.current.editorTabs).toHaveLength(0);

    // Disk recovers — the same file must be openable again.
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue({ ok: true, json: async () => ({ content: "back" }) }),
    );
    await act(async () => {
      await result.current.handleOpenFile("/missing.txt");
    });
    await waitFor(() => expect(result.current.editorTabs).toHaveLength(1));
    expect(result.current.editorTabs[0].content).toBe("back");
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

  it("external change on dirty tab then normal save 409s and preserves baseHash until reload or force", async () => {
    const { result } = renderHook(() => useEditorTabs());
    await act(async () => {
      await result.current.handleOpenFile("/a/b.txt");
    });
    // Make dirty
    act(() => {
      result.current.handleEditorChange("editor-/a/b.txt", "hello edited");
    });
    expect(result.current.editorTabs[0].isDirty).toBe(true);
    const baseHashBefore = (result.current.editorTabs[0] as any).baseHash;
    // Simulate external disk change to "external" by stubbing fetch.
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue({ ok: true, json: async () => ({ content: "external change" }) }),
    );
    // Trigger periodic check via focus
    act(() => {
      window.dispatchEvent(new Event("focus"));
    });
    await waitFor(() => expect(result.current.editorTabs[0].externalChange).toBe(true));
    // baseHash should remain old, originalContent should not be rebased
    expect((result.current.editorTabs[0] as any).baseHash).toBe(baseHashBefore);
    // Normal save should 409 (mock api to reject with 409)
    const conflictErr: any = new Error("file has changed");
    conflictErr.status = 409;
    (api.saveFileContent as any).mockRejectedValueOnce(conflictErr);
    await act(async () => {
      try {
        await result.current.saveEditorTab("editor-/a/b.txt");
      } catch {}
    });
    expect(result.current.editorTabs[0].externalChange).toBe(true);
    // Force save should succeed and advance baseline
    (api.saveFileContent as any).mockResolvedValueOnce({ path: "/a/b.txt", saved: true });
    await act(async () => {
      await result.current.forceSaveEditorTab("editor-/a/b.txt");
    });
    expect(result.current.editorTabs[0].externalChange).toBe(false);
    expect(result.current.editorTabs[0].isDirty).toBe(false);
  });

  it("reload advances baseline after external change", async () => {
    const { result } = renderHook(() => useEditorTabs());
    await act(async () => {
      await result.current.handleOpenFile("/a/b.txt");
    });
    act(() => {
      result.current.handleEditorChange("editor-/a/b.txt", "edited");
    });
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue({ ok: true, json: async () => ({ content: "external" }) }),
    );
    act(() => {
      window.dispatchEvent(new Event("focus"));
    });
    await waitFor(() => expect(result.current.editorTabs[0].externalChange).toBe(true));
    // Reload should clear externalChange and update baseHash
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue({ ok: true, json: async () => ({ content: "external" }) }),
    );
    await act(async () => {
      await result.current.reloadTabFromDisk("editor-/a/b.txt");
    });
    expect(result.current.editorTabs[0].externalChange).toBe(false);
    expect(result.current.editorTabs[0].content).toBe("external");
    expect(result.current.editorTabs[0].isDirty).toBe(false);
  });

  it("draft persistence preserves stale baseHash after external change + additional edit", async () => {
    const { result } = renderHook(() => useEditorTabs());
    await act(async () => {
      await result.current.handleOpenFile("/a/b.txt");
    });
    const originalBaseHash = (result.current.editorTabs[0] as any).baseHash;
    act(() => {
      result.current.handleEditorChange("editor-/a/b.txt", "first edit");
    });
    // External change to "external"
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue({ ok: true, json: async () => ({ content: "external" }) }),
    );
    act(() => {
      window.dispatchEvent(new Event("focus"));
    });
    await waitFor(() => expect(result.current.editorTabs[0].externalChange).toBe(true));
    // Additional edit after external change
    act(() => {
      result.current.handleEditorChange("editor-/a/b.txt", "first edit + more");
    });
    // Flush draft via beforeunload (500ms debounce would otherwise be pending)
    act(() => {
      window.dispatchEvent(new Event("beforeunload"));
    });
    const raw = window.localStorage.getItem("ocode.editor.draft.editor-/a/b.txt");
    expect(raw).not.toBeNull();
    const draft = JSON.parse(raw!);
    // Must still be the original stale hash, not hash("external")
    expect(draft.baseHash).toBe(originalBaseHash);
    expect(draft.baseHash).not.toBe("45h"); // sanity
    // Simulate reload: new hook should restore draft with externalChange still true
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue({ ok: true, json: async () => ({ content: "external" }) }),
    );
    const second = renderHook(() => useEditorTabs());
    // Wait for restore (async)
    await waitFor(() => expect(second.result.current.editorTabs.length).toBe(1));
    await waitFor(() => expect(second.result.current.editorTabs[0].externalChange).toBe(true));
    // Normal save should still 409 (baseHash stale)
    const conflictErr: any = new Error("file has changed");
    conflictErr.status = 409;
    (api.saveFileContent as any).mockRejectedValueOnce(conflictErr);
    await act(async () => {
      try {
        await second.result.current.saveEditorTab("editor-/a/b.txt");
      } catch {}
    });
    expect(second.result.current.editorTabs[0].externalChange).toBe(true);
  });
});
