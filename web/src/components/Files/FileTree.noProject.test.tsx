import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import FileTree from "./FileTree";

vi.mock("@/api/client", async () => {
  const actual = await vi.importActual<typeof import("@/api/client")>("@/api/client");
  return {
    ...actual,
    api: {
      ...actual.api,
      getPathsConfig: vi.fn(),
    },
    apiPath: (p: string) => p,
    authHeaders: () => ({}),
  };
});

import { api } from "@/api/client";

describe("FileTree no-project onboarding", () => {
  let fetchSpy: ReturnType<typeof vi.spyOn<any, any>>;

  beforeEach(() => {
    vi.clearAllMocks();
    fetchSpy = vi.spyOn(globalThis as any, "fetch").mockResolvedValue(
      new Response(JSON.stringify({ children: [], truncated: false }), {
        status: 200,
        headers: { "content-type": "application/json" },
      }),
    );
  });

  afterEach(() => {
    fetchSpy.mockRestore();
  });

  it("renders onboarding and makes no tree request when no project is selected", async () => {
    vi.mocked(api.getPathsConfig).mockResolvedValue({ extra_allowed_paths: [], upload_dir: "" });

    render(<FileTree onOpenFile={vi.fn()} projectPath={undefined} />);

    // Onboarding text should appear (not "No files")
    await waitFor(() =>
      expect(screen.getByText("No project added yet")).toBeInTheDocument(),
    );
    expect(screen.getByText("Add a project from the sidebar to browse files")).toBeInTheDocument();

    // Ensure no tree fetch was issued - only the paths config via api, not via fetch
    // The tree endpoint is /api/files/tree ; it should not have been fetched
    expect(fetchSpy).not.toHaveBeenCalledWith(
      expect.stringContaining("/api/files/tree"),
      expect.anything(),
    );
    // Ensure loading is gone and the generic "No files" is not shown
    expect(screen.queryByText(/^No files$/)).not.toBeInTheDocument();
  });

  it("still requests tree and renders No files for an explicit empty directory", async () => {
    vi.mocked(api.getPathsConfig).mockResolvedValue({ extra_allowed_paths: [], upload_dir: "" });
    // Server returns empty directory for the active project
    fetchSpy.mockResolvedValueOnce(
      new Response(JSON.stringify({ children: [], truncated: false }), {
        status: 200,
        headers: { "content-type": "application/json" },
      }),
    );

    render(<FileTree onOpenFile={vi.fn()} projectPath="/projects/active" />);

    await waitFor(() => expect(screen.getByText("No files")).toBeInTheDocument());

    // Should have fetched the explicit project path with depth=1
    expect(fetchSpy).toHaveBeenCalledWith(
      expect.stringContaining("/api/files/tree?path=%2Fprojects%2Factive&depth=1"),
      expect.anything(),
    );
    expect(screen.queryByText("No project added yet")).not.toBeInTheDocument();
  });
});
