import { describe, expect, it, vi, beforeEach, afterEach, beforeAll } from "vitest";
import { render, screen, waitFor, fireEvent, act } from "@testing-library/react";
import FileTree from "./FileTree";
import {
  saveShowHiddenFiles,
  showHiddenFilesProjectKey,
  SHOW_HIDDEN_FILES_STORAGE_KEY,
} from "./showHiddenFilesPersistence";

const mocksTree = vi.hoisted(() => ({
  getPathsConfig: vi.fn(),
}));

vi.mock("@/api/client", async () => {
  const actual = await vi.importActual<typeof import("@/api/client")>("@/api/client");
  return {
    ...actual,
    api: {
      ...actual.api,
      getPathsConfig: mocksTree.getPathsConfig,
    },
    apiPath: (p: string) => p,
    authHeaders: () => ({}),
  };
});

const PROJECT_KEY = showHiddenFilesProjectKey("/proj", undefined);
const OTHER_KEY = showHiddenFilesProjectKey("/other", undefined);

function storedValue(projectKey: string): string | null {
  return localStorage.getItem(`${SHOW_HIDDEN_FILES_STORAGE_KEY}:${projectKey}`);
}

const TREE = {
  children: [
    { name: ".env", path: ".env", is_dir: false },
    { name: "src", path: "src", is_dir: true, children: [] },
  ],
  truncated: false,
};

let fetchedUrls: string[] = [];

function mockTree() {
  return vi.spyOn(globalThis as any, "fetch").mockImplementation(
    (async (input: RequestInfo | URL) => {
      const url = String(input);
      if (url.includes("/api/files/tree")) {
        fetchedUrls.push(url);
        return new Response(JSON.stringify(TREE), {
          status: 200,
          headers: { "content-type": "application/json" },
        });
      }
      return new Response(JSON.stringify({}), { status: 404 });
    }) as any,
  );
}

beforeAll(() => {
  if (!(globalThis as any).PointerEvent) {
    (globalThis as any).PointerEvent = MouseEvent;
  }
  if (!(globalThis as any).ResizeObserver) {
    (globalThis as any).ResizeObserver = class {
      observe() {}
      unobserve() {}
      disconnect() {}
    };
  }
});

// The Files tab's hidden-files control: default hides dotfiles
// (show_hidden=0), clicking reveals them (show_hidden=1) and persists the
// choice *per project*, so switching projects keeps their settings separate
// while the file picker stays in sync for the active project.
describe("FileTree hidden-files toggle", () => {
  let fetchSpy: ReturnType<typeof mockTree>;
  beforeEach(() => {
    localStorage.clear();
    fetchedUrls = [];
    vi.clearAllMocks();
    mocksTree.getPathsConfig.mockResolvedValue({ extra_allowed_paths: [], upload_dir: "" });
    fetchSpy = mockTree();
  });
  afterEach(() => {
    fetchSpy.mockRestore();
  });

  it("starts hidden and requests show_hidden=0", async () => {
    render(<FileTree onOpenFile={vi.fn()} projectPath="/proj" />);
    const toggle = await screen.findByRole("button", { name: /Hidden files/ });
    expect(toggle).toHaveAttribute("aria-pressed", "false");
    await waitFor(() => expect(fetchedUrls.some((u) => u.includes("show_hidden=0"))).toBe(true));
  });

  it("reveals hidden files and persists the choice for this project", async () => {
    render(<FileTree onOpenFile={vi.fn()} projectPath="/proj" />);
    const toggle = await screen.findByRole("button", { name: /Hidden files/ });
    fetchedUrls = [];
    fireEvent.click(toggle);
    await waitFor(() => expect(toggle).toHaveAttribute("aria-pressed", "true"));
    await waitFor(() => expect(fetchedUrls.some((u) => u.includes("show_hidden=1"))).toBe(true));
    expect(storedValue(PROJECT_KEY)).toBe("true");
    // A different project is untouched.
    expect(storedValue(OTHER_KEY)).toBeNull();
  });

  it("reflects an external change (e.g. from the file picker)", async () => {
    render(<FileTree onOpenFile={vi.fn()} projectPath="/proj" />);
    const toggle = await screen.findByRole("button", { name: /Hidden files/ });
    expect(toggle).toHaveAttribute("aria-pressed", "false");
    act(() => saveShowHiddenFiles(PROJECT_KEY, true));
    await waitFor(() => expect(toggle).toHaveAttribute("aria-pressed", "true"));
  });

  it("ignores external changes aimed at a different project", async () => {
    render(<FileTree onOpenFile={vi.fn()} projectPath="/proj" />);
    const toggle = await screen.findByRole("button", { name: /Hidden files/ });
    act(() => saveShowHiddenFiles(OTHER_KEY, true));
    expect(toggle).toHaveAttribute("aria-pressed", "false");
  });

  it("remembers the choice after a reload (fresh mount reads localStorage)", async () => {
    const first = render(<FileTree onOpenFile={vi.fn()} projectPath="/proj" />);
    const toggle = await screen.findByRole("button", { name: /Hidden files/ });
    fireEvent.click(toggle);
    await waitFor(() => expect(toggle).toHaveAttribute("aria-pressed", "true"));

    // Simulate a page reload: unmount, then mount fresh. The state must come
    // from localStorage, not component memory.
    first.unmount();
    fetchedUrls = [];
    render(<FileTree onOpenFile={vi.fn()} projectPath="/proj" />);
    const remounted = await screen.findByRole("button", { name: /Hidden files/ });
    await waitFor(() => expect(remounted).toHaveAttribute("aria-pressed", "true"));
    await waitFor(() => expect(fetchedUrls.some((u) => u.includes("show_hidden=1"))).toBe(true));
  });

  it("keeps each project's choice separate across a switch", async () => {
    const { rerender } = render(<FileTree onOpenFile={vi.fn()} projectPath="/proj" />);
    const toggle = await screen.findByRole("button", { name: /Hidden files/ });
    fireEvent.click(toggle);
    await waitFor(() => expect(toggle).toHaveAttribute("aria-pressed", "true"));

    // Switch to a project that has no override: hidden files are hidden again.
    rerender(<FileTree onOpenFile={vi.fn()} projectPath="/other" />);
    await waitFor(() =>
      expect(screen.getByRole("button", { name: /Hidden files/ })).toHaveAttribute("aria-pressed", "false"),
    );

    // Switch back: the first project's choice is restored.
    rerender(<FileTree onOpenFile={vi.fn()} projectPath="/proj" />);
    await waitFor(() =>
      expect(screen.getByRole("button", { name: /Hidden files/ })).toHaveAttribute("aria-pressed", "true"),
    );
  });
});
