import { act, renderHook, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { api } from "../api/client";
import { useEditorTabs } from "./useEditorTabs";

vi.mock("../api/client", () => ({
  api: {
    saveFileContent: vi.fn(),
    grantFilePath: vi.fn(),
  },
  apiPath: (p: string) => p,
  authHeaders: () => ({}),
}));

const grantFilePath = api.grantFilePath as unknown as ReturnType<typeof vi.fn>;

beforeEach(() => {
  window.localStorage.clear();
  vi.stubGlobal(
    "fetch",
    vi.fn().mockResolvedValue({
      ok: true,
      json: async () => ({ content: "hello" }),
    }),
  );
  grantFilePath.mockReset();
});

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("useEditorTabs — out-of-project files", () => {
  it("grants the path's directory and opens the file anchored to the grant", async () => {
    grantFilePath.mockResolvedValue({ root: "/Users/x/Downloads", granted: true, roots: ["/Users/x/Downloads"] });

    const { result } = renderHook(() => useEditorTabs());
    await act(async () => {
      const ok = await result.current.openOutsideProject("/Users/x/Downloads/notes.md");
      expect(ok).toBe(true);
    });

    // The grant was requested for the user's path…
    expect(grantFilePath).toHaveBeenCalledWith("/Users/x/Downloads/notes.md", undefined);
    // …and the file was opened RELATIVE to the granted root, so the server
    // applies the same containment check it applies to a project-relative
    // path.
    await waitFor(() => expect(result.current.editorTabs).toHaveLength(1));
    const tab = result.current.editorTabs[0];
    expect(tab.path).toBe("notes.md");
    expect(tab.projectRoot).toBe("/Users/x/Downloads");
  });

  it("passes the remote host through so the grant lands on the remote server", async () => {
    grantFilePath.mockResolvedValue({ root: "/home/x/notes", granted: true, roots: ["/home/x/notes"] });

    const { result } = renderHook(() => useEditorTabs());
    await act(async () => {
      await result.current.openOutsideProject("/home/x/notes/a.md", "ci.local");
    });

    expect(grantFilePath).toHaveBeenCalledWith("/home/x/notes/a.md", "ci.local");
  });

  it("returns false and opens no tab when the server refuses the grant", async () => {
    grantFilePath.mockRejectedValue(new Error("path could not be resolved"));

    const { result } = renderHook(() => useEditorTabs());
    let ok = true;
    await act(async () => {
      ok = await result.current.openOutsideProject("/nope/notes.md");
    });

    expect(ok).toBe(false);
    expect(result.current.editorTabs).toHaveLength(0);
  });

  it("returns false when the grant response carries no root", async () => {
    grantFilePath.mockResolvedValue({ root: "", granted: false, roots: [] });

    const { result } = renderHook(() => useEditorTabs());
    let ok = true;
    await act(async () => {
      ok = await result.current.openOutsideProject("/Users/x/notes.md");
    });

    expect(ok).toBe(false);
    expect(result.current.editorTabs).toHaveLength(0);
  });

  it("surfaces the server's message when a file load is refused", async () => {
    // The server's refusal message must reach the caller so the UI can offer
    // the grant. A generic "Failed to load file" would make an out-of-project
    // file indistinguishable from a missing one.
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue({
        ok: false,
        status: 400,
        statusText: "Bad Request",
        json: async () => ({ error: "path is outside the project root" }),
      }),
    );

    const { result } = renderHook(() => useEditorTabs());
    let ok = true;
    await act(async () => {
      ok = await result.current.handleOpenFile("/outside/notes.md", "/proj");
    });

    expect(ok).toBe(false);
    expect(result.current.editorTabs).toHaveLength(0);
  });
});
