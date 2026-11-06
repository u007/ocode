import { act, renderHook, waitFor } from "@testing-library/react";
import { useEffect } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { Mock } from "vitest";
import { api } from "../api/client";
import { useEditorTabs } from "./useEditorTabs";
import { __resetDraftGuardForTests, draftPersistenceBlocked } from "../lib/editorDraftGuard";

vi.mock("../api/client", () => ({
  api: { saveFileContent: vi.fn(), checkFileAccess: vi.fn() },
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

  it("opens preview-only files without fetching their contents", async () => {
    const { result } = renderHook(() => useEditorTabs());
    await act(async () => {
      await result.current.handleOpenFile("docs/report.pdf", "/proj");
    });
    expect(result.current.activeEditorTabId).toBe("editor-/proj::docs/report.pdf");
    const tab = result.current.editorTabs[0];
    expect(tab.path).toBe("docs/report.pdf");
    expect(tab.projectRoot).toBe("/proj");
    expect(tab.content).toBe("");
    expect(tab.isBinary).toBe(true);
    // The viewer reads /api/files/raw itself — no /api/files/content round-trip.
    expect(fetch).not.toHaveBeenCalled();
  });

  it("opens legacy .doc without fetching contents (OS-open pane)", async () => {
    const { result } = renderHook(() => useEditorTabs());
    await act(async () => {
      await result.current.handleOpenFile("docs/old.doc");
    });
    expect(result.current.editorTabs).toHaveLength(1);
    expect(fetch).not.toHaveBeenCalled();
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
    expect(api.saveFileContent).toHaveBeenCalledWith("src/a.ts", "hello", "/projects/active", expect.any(String), undefined, undefined);
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
    let opened: boolean | undefined;
    await act(async () => {
      opened = await result.current.handleOpenFile("/missing.txt");
    });
    expect(opened).toBe(false);
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

// The desktop quit guard: a dirty tab whose localStorage draft write fails must
// be reported to the user and must block quit, until the file is saved (which
// clears the draft and re-enables quit).
describe("useEditorTabs draft-persistence guard", () => {
  let invoke: Mock<(m: string) => void>;

  beforeEach(() => {
    __resetDraftGuardForTests();
    invoke = vi.fn<(m: string) => void>();
    (window as unknown as { _wails?: { invoke: (m: string) => void } })._wails = { invoke };
  });

  afterEach(() => {
    delete (window as unknown as { _wails?: unknown })._wails;
    __resetDraftGuardForTests();
  });

  it("blocks quit when a dirty draft cannot be persisted, and clears on save", async () => {
    const { result } = renderHook(() => useEditorTabs());
    await act(async () => {
      await result.current.handleOpenFile("/a/big.txt");
    });
    await waitFor(() => expect(result.current.editorTabs).toHaveLength(1));

    act(() => {
      result.current.handleEditorChange("editor-/a/big.txt", "x".repeat(64));
    });

    // Simulate a full localStorage quota on the draft write. `beforeunload`
    // flushes the debounced draft synchronously (same as a reload/quit).
    const setItem = vi.spyOn(Storage.prototype, "setItem").mockImplementation(() => {
      throw new Error("QuotaExceededError");
    });
    try {
      act(() => {
        window.dispatchEvent(new Event("beforeunload"));
      });
      expect(draftPersistenceBlocked()).toBe(true);
      expect(invoke).toHaveBeenCalledWith(expect.stringMatching(/^ocode:quit-guard:blocked:/));
    } finally {
      setItem.mockRestore();
    }

    (api.saveFileContent as ReturnType<typeof vi.fn>).mockResolvedValue({ ok: true });
    await act(async () => {
      await result.current.saveEditorTab("editor-/a/big.txt");
    });
    expect(draftPersistenceBlocked()).toBe(false);
    expect(invoke).toHaveBeenCalledWith("ocode:quit-guard:clear");
  });
});

// The quit guard must arm while edits are still IN MEMORY (before the debounced
// write lands), and the write must not be deferrable indefinitely by
// continuous typing.
describe("useEditorTabs in-memory draft guard", () => {
  let invoke: Mock<(m: string) => void>;

  beforeEach(() => {
    __resetDraftGuardForTests();
    invoke = vi.fn<(m: string) => void>();
    (window as unknown as { _wails?: { invoke: (m: string) => void } })._wails = { invoke };
    vi.useFakeTimers();
  });

  afterEach(() => {
    vi.useRealTimers();
    delete (window as unknown as { _wails?: unknown })._wails;
    __resetDraftGuardForTests();
  });

  it("blocks quit while edits are in memory, and clears once the draft is written", async () => {
    const { result } = renderHook(() => useEditorTabs());
    await act(async () => {
      await result.current.handleOpenFile("/a/b.txt");
    });

    act(() => {
      result.current.handleEditorChange("editor-/a/b.txt", "typed");
    });

    // Nothing has been written yet — the guard must already be armed, or a quit
    // inside the debounce window drops the edit with nobody noticing.
    expect(draftPersistenceBlocked()).toBe(true);
    expect(invoke).toHaveBeenCalledWith(expect.stringMatching(/^ocode:quit-guard:blocked:/));
    expect(window.localStorage.getItem("ocode.editor.draft.editor-/a/b.txt")).toBeNull();

    await act(async () => {
      await vi.advanceTimersByTimeAsync(600);
    });

    expect(draftPersistenceBlocked()).toBe(false);
    expect(invoke).toHaveBeenCalledWith("ocode:quit-guard:clear");
    expect(window.localStorage.getItem("ocode.editor.draft.editor-/a/b.txt")).not.toBeNull();
  });

  it("caps how long continuous typing can defer the draft write", async () => {
    const { result } = renderHook(() => useEditorTabs());
    await act(async () => {
      await result.current.handleOpenFile("/a/b.txt");
    });

    // Edit every 400ms for 2s: under the 500ms debounce, so a plain trailing
    // debounce would be pushed out forever and never write anything.
    for (let i = 1; i <= 5; i++) {
      act(() => {
        result.current.handleEditorChange("editor-/a/b.txt", `edit ${i}`);
      });
      await act(async () => {
        await vi.advanceTimersByTimeAsync(400);
      });
    }

    // A write landed mid-stream despite the never-ending typing.
    expect(window.localStorage.getItem("ocode.editor.draft.editor-/a/b.txt")).not.toBeNull();

    // ...and the final write carries the newest content (the flush path never
    // reads a stale snapshot).
    await act(async () => {
      await vi.advanceTimersByTimeAsync(600);
    });
    const raw = window.localStorage.getItem("ocode.editor.draft.editor-/a/b.txt");
    expect(JSON.parse(raw!).content).toBe("edit 5");
  });
});

describe("Excel preview tab close-confirm (t5)", () => {
  const XLSX_TAB = "editor-docs/budget.xlsx";

  beforeEach(() => {
    __resetDraftGuardForTests();
  });

  afterEach(() => {
    __resetDraftGuardForTests();
  });

  it("setTabDirty marks an Excel tab dirty and arms the quit guard", async () => {
    const { result } = renderHook(() => useEditorTabs());
    await act(async () => {
      await result.current.handleOpenFile("docs/budget.xlsx");
    });
    await waitFor(() => expect(result.current.editorTabs).toHaveLength(1));

    expect(result.current.editorTabs[0].isDirty).toBe(false);
    expect(draftPersistenceBlocked()).toBe(false);

    act(() => {
      result.current.setTabDirty(XLSX_TAB, true);
    });

    expect(result.current.editorTabs[0].isDirty).toBe(true);
    expect(draftPersistenceBlocked()).toBe(true);

    act(() => {
      result.current.setTabDirty(XLSX_TAB, false);
    });

    expect(result.current.editorTabs[0].isDirty).toBe(false);
    expect(draftPersistenceBlocked()).toBe(false);
  });

  it("setTabDirty with an unchanged flag keeps the same tabs array", async () => {
    const { result } = renderHook(() => useEditorTabs());
    await act(async () => {
      await result.current.handleOpenFile("docs/budget.xlsx");
    });
    await waitFor(() => expect(result.current.editorTabs).toHaveLength(1));

    const before = result.current.editorTabs;
    act(() => {
      result.current.setTabDirty(XLSX_TAB, false);
    });
    expect(result.current.editorTabs).toBe(before);
  });

  it("does not re-render when an effect re-reports the same Excel dirty flag", async () => {
    let renders = 0;
    const { result } = renderHook(() => {
      const tabs = useEditorTabs();
      renders += 1;
      if (renders > 50) throw new Error("render loop: setTabDirty keeps re-rendering the editor tabs");
      // Mirrors App.tsx and FileTabContent: the caller re-reports the dirty flag
      // from an effect that runs on every render.
      useEffect(() => {
        tabs.setTabDirty(XLSX_TAB, false);
      });
      return tabs;
    });
    await act(async () => {
      await result.current.handleOpenFile("docs/budget.xlsx");
    });
    await waitFor(() => expect(result.current.editorTabs).toHaveLength(1));
    expect(renders).toBeLessThan(10);
  });

  it("saves Excel preview tabs through the registered save handler", async () => {
    const { result } = renderHook(() => useEditorTabs());
    await act(async () => {
      await result.current.handleOpenFile("docs/budget.xlsx");
    });
    await waitFor(() => expect(result.current.editorTabs).toHaveLength(1));

    const handler = vi.fn(async () => true);
    act(() => {
      result.current.registerExcelSaveHandler(XLSX_TAB, handler);
      result.current.setTabDirty(XLSX_TAB, true);
    });

    expect(draftPersistenceBlocked()).toBe(true);

    await act(async () => {
      await result.current.saveEditorTab(XLSX_TAB);
    });

    expect(handler).toHaveBeenCalledTimes(1);
    expect(result.current.editorTabs[0].isDirty).toBe(false);
    expect(draftPersistenceBlocked()).toBe(false);
  });

  it("keeps the tab open when Excel save returns false (cancelled/failed)", async () => {
    const { result } = renderHook(() => useEditorTabs());
    await act(async () => {
      await result.current.handleOpenFile("docs/budget.xlsx");
    });
    await waitFor(() => expect(result.current.editorTabs).toHaveLength(1));

    const handler = vi.fn(async () => false); // first-save confirm declined
    act(() => {
      result.current.registerExcelSaveHandler(XLSX_TAB, handler);
      result.current.setTabDirty(XLSX_TAB, true);
    });
    await waitFor(() => expect(result.current.editorTabs[0].isDirty).toBe(true));
    act(() => {
      result.current.requestCloseTab(XLSX_TAB);
    });

    expect(result.current.pendingClose).not.toBeNull();
    expect(draftPersistenceBlocked()).toBe(true);

    await act(async () => {
      await result.current.confirmSaveAndClose();
    });

    // Save was cancelled → tab stays open, dialog stays open, guard stays armed
    expect(result.current.editorTabs).toHaveLength(1);
    expect(result.current.pendingClose).not.toBeNull();
    expect(draftPersistenceBlocked()).toBe(true);
    expect(handler).toHaveBeenCalledTimes(1);
  });

  it("closes the tab when Excel save succeeds", async () => {
    const { result } = renderHook(() => useEditorTabs());
    await act(async () => {
      await result.current.handleOpenFile("docs/budget.xlsx");
    });
    await waitFor(() => expect(result.current.editorTabs).toHaveLength(1));

    const handler = vi.fn(async () => true);
    act(() => {
      result.current.registerExcelSaveHandler(XLSX_TAB, handler);
      result.current.setTabDirty(XLSX_TAB, true);
    });
    await waitFor(() => expect(result.current.editorTabs[0].isDirty).toBe(true));
    act(() => {
      result.current.requestCloseTab(XLSX_TAB);
    });

    expect(result.current.pendingClose).not.toBeNull();

    await act(async () => {
      await result.current.confirmSaveAndClose();
    });

    expect(handler).toHaveBeenCalledTimes(1);
    expect(result.current.editorTabs).toHaveLength(0);
    expect(result.current.pendingClose).toBeNull();
    expect(draftPersistenceBlocked()).toBe(false);
  });

  it("discards an Excel tab without saving", async () => {
    const { result } = renderHook(() => useEditorTabs());
    await act(async () => {
      await result.current.handleOpenFile("docs/budget.xlsx");
    });
    await waitFor(() => expect(result.current.editorTabs).toHaveLength(1));

    act(() => {
      result.current.setTabDirty(XLSX_TAB, true);
    });
    await waitFor(() => expect(result.current.editorTabs[0].isDirty).toBe(true));
    act(() => {
      result.current.requestCloseTab(XLSX_TAB);
    });
    expect(result.current.pendingClose).not.toBeNull();

    act(() => {
      result.current.confirmDiscardAndClose();
    });

    expect(result.current.editorTabs).toHaveLength(0);
    expect(result.current.pendingClose).toBeNull();
    expect(draftPersistenceBlocked()).toBe(false);
  });

  it("reconcileDraftGuard removes stale entries for closed Excel tabs", async () => {
    const { result } = renderHook(() => useEditorTabs());
    await act(async () => {
      await result.current.handleOpenFile("docs/budget.xlsx");
    });
    await waitFor(() => expect(result.current.editorTabs).toHaveLength(1));

    act(() => {
      result.current.setTabDirty(XLSX_TAB, true);
    });
    await waitFor(() => expect(result.current.editorTabs[0].isDirty).toBe(true));
    expect(draftPersistenceBlocked()).toBe(true);

    act(() => {
      result.current.requestCloseTab(XLSX_TAB);
    });
    expect(result.current.pendingClose).not.toBeNull();

    act(() => {
      result.current.confirmDiscardAndClose();
    });
    expect(result.current.editorTabs).toHaveLength(0);
    expect(draftPersistenceBlocked()).toBe(false);
  });
});

describe("preview-only tab saves", () => {
  beforeEach(() => {
    __resetDraftGuardForTests();
    vi.mocked(api.saveFileContent).mockClear();
  });

  afterEach(() => {
    __resetDraftGuardForTests();
  });

  it.each(["docs/report.pdf", "docs/spec.docx"])(
    "saves %s through its registered handler, never api.saveFileContent",
    async (path) => {
      const { result } = renderHook(() => useEditorTabs());
      await act(async () => {
        await result.current.handleOpenFile(path);
      });
      await waitFor(() => expect(result.current.editorTabs).toHaveLength(1));
      const tabId = result.current.editorTabs[0].id;

      const handler = vi.fn(async () => true);
      act(() => {
        result.current.registerExcelSaveHandler(tabId, handler);
        result.current.setTabDirty(tabId, true);
      });

      await act(async () => {
        await result.current.saveEditorTab(tabId);
      });

      expect(handler).toHaveBeenCalledTimes(1);
      expect(api.saveFileContent).not.toHaveBeenCalled();
      expect(result.current.editorTabs[0].isDirty).toBe(false);
    },
  );

  it("resolves without saving a clean preview-only tab that has no handler", async () => {
    const { result } = renderHook(() => useEditorTabs());
    await act(async () => {
      await result.current.handleOpenFile("docs/photo.png");
    });
    await waitFor(() => expect(result.current.editorTabs).toHaveLength(1));
    const tabId = result.current.editorTabs[0].id;

    await act(async () => {
      await result.current.saveEditorTab(tabId);
    });

    expect(api.saveFileContent).not.toHaveBeenCalled();
    expect(result.current.saveError).toBeNull();
  });

  it("rejects saving a dirty preview-only tab with no handler without calling api.saveFileContent", async () => {
    const { result } = renderHook(() => useEditorTabs());
    await act(async () => {
      await result.current.handleOpenFile("docs/report.pdf");
    });
    await waitFor(() => expect(result.current.editorTabs).toHaveLength(1));
    const tabId = result.current.editorTabs[0].id;
    act(() => {
      result.current.setTabDirty(tabId, true);
    });

    let rejection: unknown = null;
    await act(async () => {
      try {
        await result.current.saveEditorTab(tabId);
      } catch (err) {
        rejection = err;
      }
    });

    expect(rejection).toBeInstanceOf(Error);
    expect((rejection as Error).message).toBe("No save handler is registered for docs/report.pdf; refusing to overwrite it with an empty buffer");
    expect(api.saveFileContent).not.toHaveBeenCalled();
    expect(result.current.saveError).toBe("No save handler is registered for docs/report.pdf; refusing to overwrite it with an empty buffer");
  });
});
