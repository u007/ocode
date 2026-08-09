import { act, renderHook, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { useEditorTabs } from "./useEditorTabs";

vi.mock("../api/client", () => ({
  api: { saveFileContent: vi.fn() },
  apiPath: (p: string) => p,
  authHeaders: () => ({}),
}));

beforeEach(() => {
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
});
