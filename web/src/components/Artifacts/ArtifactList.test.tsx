import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, fireEvent, waitFor } from "@testing-library/react";
import ArtifactList from "./ArtifactList";

const artifacts = [
  {
    id: "a1",
    name: "One",
    project_path: "/p",
    status: "active",
    hosting_mode: "hub",
    share_url: "https://hub.test/p/a1/tok/",
  },
  {
    id: "a2",
    name: "Two",
    project_path: "/p",
    status: "active",
    hosting_mode: "self",
  },
  { id: "a3", name: "Other project", project_path: "/other" },
];

vi.mock("../../api/artifacts", () => ({
  listArtifacts: vi.fn(),
}));

vi.mock("../../lib/clipboard", () => ({
  copyTextToClipboard: vi.fn().mockResolvedValue(true),
}));

describe("ArtifactList", () => {
  beforeEach(async () => {
    const { listArtifacts } = await import("../../api/artifacts");
    vi.mocked(listArtifacts).mockResolvedValue({
      artifacts,
      total: artifacts.length,
      limit: 10,
      offset: 0,
    });
    const { copyTextToClipboard } = await import("../../lib/clipboard");
    vi.mocked(copyTextToClipboard).mockClear();
  });

  it("renders only the active project's artifacts", async () => {
    render(<ArtifactList projectPath="/p" />);
    await waitFor(() => expect(screen.getByText("One")).toBeInTheDocument());
    expect(screen.getByText("Two")).toBeInTheDocument();
    expect(screen.queryByText("Other project")).toBeNull();
  });

  it("copies the server share link, not a synthesized path", async () => {
    const { copyTextToClipboard } = await import("../../lib/clipboard");
    render(<ArtifactList projectPath="/p" />);
    await waitFor(() =>
      expect(screen.getAllByTestId("copy-share-link").length).toBe(2),
    );
    fireEvent.click(screen.getAllByTestId("copy-share-link")[0]);
    await waitFor(() =>
      expect(copyTextToClipboard).toHaveBeenCalledWith(
        "https://hub.test/p/a1/tok/",
      ),
    );
  });

  it("disables copy when the artifact has no share link", async () => {
    render(<ArtifactList projectPath="/p" />);
    await waitFor(() =>
      expect(screen.getAllByTestId("copy-share-link").length).toBe(2),
    );
    expect(screen.getAllByTestId("copy-share-link")[1]).toBeDisabled();
  });

  it("offers Publish per row only when a handler is given", async () => {
    const onPublish = vi.fn();
    const { rerender } = render(<ArtifactList projectPath="/p" />);
    await waitFor(() => expect(screen.getByText("One")).toBeInTheDocument());
    expect(screen.queryAllByTestId("publish-artifact")).toHaveLength(0);

    rerender(<ArtifactList projectPath="/p" onPublish={onPublish} />);
    await waitFor(() =>
      expect(screen.getAllByTestId("publish-artifact").length).toBe(2),
    );
    fireEvent.click(screen.getAllByTestId("publish-artifact")[0]);
    expect(onPublish).toHaveBeenCalledWith(
      expect.objectContaining({ id: "a1" }),
    );
  });

  it("loads only when the tab becomes active", async () => {
    const { listArtifacts } = await import("../../api/artifacts");
    vi.mocked(listArtifacts).mockClear();
    const { rerender } = render(
      <ArtifactList projectPath="/p" active={false} />,
    );
    expect(listArtifacts).not.toHaveBeenCalled();
    rerender(<ArtifactList projectPath="/p" active />);
    await waitFor(() => expect(listArtifacts).toHaveBeenCalledTimes(1));
  });
});
