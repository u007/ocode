import { act, renderHook, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { api } from "../api/client";
import { useEditorTabs } from "./useEditorTabs";

// checkFileAccess is the real client method, so its request goes through the
// global fetch mock below and the tests can answer /api/files/access directly.
vi.mock("../api/client", async (importOriginal) => {
  const actual = await importOriginal<typeof import("../api/client")>();
  return {
    ...actual,
    api: {
      ...actual.api,
      saveFileContent: vi.fn(),
      grantFilePath: vi.fn(),
    },
    apiPath: (p: string) => p,
    authHeaders: () => ({}),
  };
});

const grantFilePath = api.grantFilePath as unknown as ReturnType<typeof vi.fn>;

// stubFetch answers /api/files/access with the given verdict and every other
// URL with file content.
function stubFetch(access: { ok: true } | { ok: false; error: string }) {
  const fetchMock = vi.fn(async (url: string) => {
    if (url.includes("/api/files/access")) {
      if (access.ok) {
        return { ok: true, status: 200, json: async () => ({ ok: true }) };
      }
      return { ok: false, status: 400, statusText: "Bad Request", json: async () => ({ error: access.error }) };
    }
    return { ok: true, status: 200, json: async () => ({ content: "hello" }) };
  });
  vi.stubGlobal("fetch", fetchMock);
  return fetchMock;
}

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
    grantFilePath.mockResolvedValue({ root: "/Users/x/Downloads", path: "/Users/x/Downloads/notes.md", granted: true, roots: ["/Users/x/Downloads"] });

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
    grantFilePath.mockResolvedValue({ root: "/home/x/notes", path: "/home/x/notes/a.md", granted: true, roots: ["/home/x/notes"] });

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
    grantFilePath.mockResolvedValue({ root: "", path: "", granted: false, roots: [] });

    const { result } = renderHook(() => useEditorTabs());
    let ok = true;
    await act(async () => {
      ok = await result.current.openOutsideProject("/Users/x/notes.md");
    });

    expect(ok).toBe(false);
    expect(result.current.editorTabs).toHaveLength(0);
  });

  it("opens a link-typed path relative to the server's canonical root", async () => {
    // macOS: the user typed /var/x/notes.md, the server resolved the grant to
    // /private/var/x and spells the file under that same root.
    grantFilePath.mockResolvedValue({
      root: "/private/var/x",
      path: "/private/var/x/notes.md",
      granted: true,
      roots: ["/private/var/x"],
    });

    const { result } = renderHook(() => useEditorTabs());
    await act(async () => {
      const ok = await result.current.openOutsideProject("/var/x/notes.md");
      expect(ok).toBe(true);
    });

    await waitFor(() => expect(result.current.editorTabs).toHaveLength(1));
    expect(result.current.editorTabs[0].path).toBe("notes.md");
    expect(result.current.editorTabs[0].projectRoot).toBe("/private/var/x");
  });

  it("throws a clear error when the file is not inside the granted root", async () => {
    grantFilePath.mockResolvedValue({
      root: "/Users/x/Downloads",
      path: "/Users/x/Other/notes.md",
      granted: true,
      roots: ["/Users/x/Downloads"],
    });

    const { result } = renderHook(() => useEditorTabs());
    await act(async () => {
      await expect(result.current.openOutsideProject("/Users/x/Other/notes.md")).rejects.toThrow(
        "Cannot open /Users/x/Other/notes.md: it is not inside the granted folder /Users/x/Downloads.",
      );
    });

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

describe("useEditorTabs — preview-only files outside the project", () => {
  it("refuses an absolute preview-only path outside the project root so the grant dialog can open", async () => {
    const fetchMock = stubFetch({ ok: false, error: "path is outside the project root" });
    const { result } = renderHook(() => useEditorTabs());
    let first = true;
    let second = true;
    await act(async () => {
      first = await result.current.handleOpenFile("/Users/x/Downloads/pic.png", "/Users/x/proj");
      second = await result.current.handleOpenFile("/Users/x/Downloads/pic.png", "/Users/x/proj");
    });

    expect(first).toBe(false);
    // A released claim reports false again; a leaked claim would report true
    // ("already open") and hide the grant dialog.
    expect(second).toBe(false);
    expect(result.current.editorTabs).toHaveLength(0);
    // The probe was asked, and the image itself was never fetched.
    const urls = fetchMock.mock.calls.map((c) => String(c[0]));
    expect(urls.some((u) => u.includes("/api/files/access") && u.includes("pic.png"))).toBe(true);
    expect(urls.some((u) => u.includes("/api/files/raw"))).toBe(false);
  });

  it("refuses an absolute preview-only path with no project root when the server says it is outside", async () => {
    stubFetch({ ok: false, error: "path is outside the project root" });
    const { result } = renderHook(() => useEditorTabs());
    let ok = true;
    await act(async () => {
      ok = await result.current.handleOpenFile("/Users/x/Downloads/pic.png");
    });

    expect(ok).toBe(false);
    expect(result.current.editorTabs).toHaveLength(0);
  });

  it("opens an absolute preview-only path inside the project root", async () => {
    stubFetch({ ok: true });
    const { result } = renderHook(() => useEditorTabs());
    let ok = false;
    await act(async () => {
      ok = await result.current.handleOpenFile("/Users/x/proj/img/pic.png", "/Users/x/proj");
    });

    expect(ok).toBe(true);
    expect(result.current.editorTabs).toHaveLength(1);
    expect(result.current.editorTabs[0].path).toBe("/Users/x/proj/img/pic.png");
  });

  it("opens a relative preview-only path under a granted root", async () => {
    stubFetch({ ok: true });
    const { result } = renderHook(() => useEditorTabs());
    let ok = false;
    await act(async () => {
      ok = await result.current.handleOpenFile("pic.png", "/Users/x/Downloads");
    });

    expect(ok).toBe(true);
    expect(result.current.editorTabs).toHaveLength(1);
    expect(result.current.editorTabs[0].path).toBe("pic.png");
  });
});
