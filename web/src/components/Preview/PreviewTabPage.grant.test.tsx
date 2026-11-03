// Full-width Preview sub-tab: a file the server refuses as outside the project
// waits on the grant prompt, and the granted file opens under the granted root.
import { fireEvent, render, screen } from "@testing-library/react";
import { describe, it, expect, vi, beforeEach } from "vitest";
import PreviewTabPage from "./PreviewTabPage";
import { api } from "../../api/client";

vi.mock("../../api/client", () => ({
  api: { checkFileAccess: vi.fn(), grantFilePath: vi.fn(), getFileContent: vi.fn() },
}));
vi.mock("../Files/FilePicker", () => ({ default: () => null }));
vi.mock("./PreviewSurface", () => ({
  default: ({ path, projectRoot }: { path: string; projectRoot?: string }) => (
    <div data-testid="preview-surface" data-path={path} data-root={projectRoot ?? ""} />
  ),
}));

const OUTSIDE = "/elsewhere/shots/a.png";

describe("PreviewTabPage outside-root grant", () => {
  beforeEach(() => {
    vi.mocked(api.checkFileAccess).mockReset();
    vi.mocked(api.grantFilePath).mockReset();
    vi.mocked(api.checkFileAccess).mockImplementation(async (path, root) => {
      if (path === OUTSIDE && root === "/proj") throw new Error("path is outside the project root");
    });
  });

  it("prompts for the grant, then opens the file under the granted root", async () => {
    vi.mocked(api.grantFilePath).mockResolvedValue({
      root: "/elsewhere/shots",
      path: OUTSIDE,
      granted: true,
      roots: ["/elsewhere/shots"],
    });
    render(<PreviewTabPage projectRoot="/proj" nonce={1} request={{ path: OUTSIDE, kind: "image", page: 1 }} />);

    fireEvent.click(await screen.findByRole("button", { name: "Grant access to its folder" }));
    const surface = await screen.findByTestId("preview-surface");
    expect(api.grantFilePath).toHaveBeenCalledWith(OUTSIDE, undefined);
    expect(surface.getAttribute("data-path")).toBe("a.png");
    expect(surface.getAttribute("data-root")).toBe("/elsewhere/shots");
    expect(screen.queryByRole("button", { name: "Grant access to its folder" })).toBeNull();
  });

  it("grants a relative outside path by its absolute form, resolved against the project root", async () => {
    vi.mocked(api.checkFileAccess).mockImplementation(async (path, root) => {
      if (path === "../Downloads/pic.png" && root === "/Users/x/proj") {
        throw new Error("path is outside the project root");
      }
    });
    vi.mocked(api.grantFilePath).mockResolvedValue({
      root: "/Users/x/Downloads",
      path: "/Users/x/Downloads/pic.png",
      granted: true,
      roots: ["/Users/x/Downloads"],
    });
    render(
      <PreviewTabPage
        projectRoot="/Users/x/proj"
        nonce={1}
        request={{ path: "../Downloads/pic.png", kind: "image", page: 1 }}
      />,
    );

    expect(await screen.findByText("/Users/x/Downloads/pic.png")).toBeDefined();
    fireEvent.click(screen.getByRole("button", { name: "Grant access to its folder" }));
    const surface = await screen.findByTestId("preview-surface");
    expect(api.grantFilePath).toHaveBeenCalledWith("/Users/x/Downloads/pic.png", undefined);
    expect(surface.getAttribute("data-path")).toBe("pic.png");
    expect(surface.getAttribute("data-root")).toBe("/Users/x/Downloads");
  });
});
