import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import AssetsPanel from "./AssetsPanel";

vi.mock("@/api/client", () => ({
  apiPath: (path: string) => path,
  authHeaders: () => ({}),
}));

vi.mock("../../stores/projectStore", () => ({
  useProjectState: () => ({ state: { activeProject: null } }),
}));

describe("AssetsPanel preview lifecycle", () => {
  const originalCreateObjectURL = URL.createObjectURL;
  const originalRevokeObjectURL = URL.revokeObjectURL;

  beforeEach(() => {
    Object.defineProperty(URL, "createObjectURL", {
      configurable: true,
      value: vi.fn(() => "blob:late-preview"),
    });
    Object.defineProperty(URL, "revokeObjectURL", {
      configurable: true,
      value: vi.fn(),
    });
  });

  afterEach(() => {
    if (originalCreateObjectURL) {
      Object.defineProperty(URL, "createObjectURL", {
        configurable: true,
        value: originalCreateObjectURL,
      });
    } else {
      delete (URL as unknown as { createObjectURL?: unknown }).createObjectURL;
    }
    if (originalRevokeObjectURL) {
      Object.defineProperty(URL, "revokeObjectURL", {
        configurable: true,
        value: originalRevokeObjectURL,
      });
    } else {
      delete (URL as unknown as { revokeObjectURL?: unknown }).revokeObjectURL;
    }
    vi.restoreAllMocks();
  });

  it("revokes a blob produced after the panel unmounts", async () => {
    let resolvePreview!: (response: unknown) => void;
    const previewResponse = new Promise<unknown>((resolve) => {
      resolvePreview = resolve;
    });
    const fetchMock = vi.fn((input: RequestInfo | URL) => {
      if (String(input) === "/api/uploads") {
        return Promise.resolve({
          ok: true,
          json: () =>
            Promise.resolve([
              {
                name: "image.png",
                size: 4,
                modtime: new Date().toISOString(),
                mime: "image/png",
              },
            ]),
        });
      }
      return previewResponse;
    });
    vi.stubGlobal("fetch", fetchMock);

    const { unmount } = render(<AssetsPanel />);
    const file = await screen.findByText("image.png");
    fireEvent.click(file.closest('[role="button"]')!);
    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(2));

    unmount();
    await act(async () => {
      resolvePreview({
        ok: true,
        blob: () => Promise.resolve(new Blob(["data"], { type: "image/png" })),
      });
      await previewResponse;
    });

    expect(URL.revokeObjectURL).toHaveBeenCalledWith("blob:late-preview");
  });

  it("revokes a stale preview when a newer selection wins", async () => {
    let resolveFirst!: (response: unknown) => void;
    let resolveSecond!: (response: unknown) => void;
    const firstResponse = new Promise<unknown>((resolve) => {
      resolveFirst = resolve;
    });
    const secondResponse = new Promise<unknown>((resolve) => {
      resolveSecond = resolve;
    });
    const fetchMock = vi.fn((input: RequestInfo | URL) => {
      const url = String(input);
      if (url === "/api/uploads") {
        return Promise.resolve({
          ok: true,
          json: () =>
            Promise.resolve([
              { name: "first.png", size: 4, modtime: new Date().toISOString(), mime: "image/png" },
              { name: "second.png", size: 4, modtime: new Date().toISOString(), mime: "image/png" },
            ]),
        });
      }
      return url.includes("first.png") ? firstResponse : secondResponse;
    });
    vi.stubGlobal("fetch", fetchMock);
    const createObjectURL = URL.createObjectURL as unknown as ReturnType<typeof vi.fn>;
    createObjectURL.mockReturnValueOnce("blob:first").mockReturnValueOnce("blob:second");

    const { unmount } = render(<AssetsPanel />);
    fireEvent.click((await screen.findByText("first.png")).closest('[role="button"]')!);
    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(2));
    fireEvent.click(screen.getByText("second.png").closest('[role="button"]')!);
    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(3));

    await act(async () => {
      resolveFirst({
        ok: true,
        blob: () => Promise.resolve(new Blob(["first"], { type: "image/png" })),
      });
      await firstResponse;
      await Promise.resolve();
    });
    expect(URL.revokeObjectURL).toHaveBeenCalledWith("blob:first");

    unmount();
    await act(async () => {
      resolveSecond({
        ok: true,
        blob: () => Promise.resolve(new Blob(["second"], { type: "image/png" })),
      });
      await secondResponse;
      await Promise.resolve();
    });
    expect(URL.revokeObjectURL).toHaveBeenCalledWith("blob:second");
  });
});
