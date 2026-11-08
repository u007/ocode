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
  getArtifactDetail: vi.fn(),
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

  it("fires the row unpublish and delete handlers", async () => {
    const onUnpublish = vi.fn();
    const onDelete = vi.fn();
    render(
      <ArtifactList
        projectPath="/p"
        onUnpublish={onUnpublish}
        onDelete={onDelete}
      />,
    );
    await waitFor(() =>
      expect(screen.getAllByTestId("unpublish-artifact").length).toBe(2),
    );
    fireEvent.click(screen.getAllByTestId("unpublish-artifact")[0]);
    expect(onUnpublish).toHaveBeenCalledWith(
      expect.objectContaining({ id: "a1" }),
    );
    fireEvent.click(screen.getAllByTestId("delete-artifact")[0]);
    expect(onDelete).toHaveBeenCalledWith(
      expect.objectContaining({ id: "a1" }),
    );
  });

  it("hides Unpublish for an already-unpublished artifact", async () => {
    const { listArtifacts } = await import("../../api/artifacts");
    vi.mocked(listArtifacts).mockResolvedValueOnce({
      artifacts: [
        { id: "u1", name: "U1", project_path: "/p", status: "unpublished" },
        { id: "u2", name: "U2", project_path: "/p", status: "active" },
      ],
      total: 2,
      limit: 10,
      offset: 0,
    });
    render(
      <ArtifactList
        projectPath="/p"
        onUnpublish={() => {}}
        onDelete={() => {}}
      />,
    );
    await waitFor(() => expect(screen.getByText("U1")).toBeInTheDocument());
    expect(screen.getAllByTestId("unpublish-artifact")).toHaveLength(1);
    expect(screen.getAllByTestId("delete-artifact")).toHaveLength(2);
  });

  it("reloads when refreshKey changes", async () => {
    const { listArtifacts } = await import("../../api/artifacts");
    vi.mocked(listArtifacts).mockClear();
    const { rerender } = render(
      <ArtifactList projectPath="/p" refreshKey={0} />,
    );
    await waitFor(() => expect(listArtifacts).toHaveBeenCalledTimes(1));
    rerender(<ArtifactList projectPath="/p" refreshKey={1} />);
    await waitFor(() => expect(listArtifacts).toHaveBeenCalledTimes(2));
  });

  it("expands a row to show its versions and caches the fetch", async () => {
    const { getArtifactDetail } = await import("../../api/artifacts");
    vi.mocked(getArtifactDetail).mockClear();
    vi.mocked(getArtifactDetail).mockResolvedValue({
      artifact: { id: "a1" } as any,
      versions: [
        {
          id: "v2",
          version: "20261108120000",
          size_bytes: 2048,
          created_at: new Date().toISOString(),
        },
        { id: "v1", version: "20261107120000", size_bytes: 1024 },
      ],
    } as any);
    render(<ArtifactList projectPath="/p" />);
    await waitFor(() => expect(screen.getByText("One")).toBeInTheDocument());

    fireEvent.click(screen.getAllByTestId("toggle-versions")[0]);
    const list = await screen.findByTestId("version-list");
    expect(list).toHaveTextContent("20261108120000");
    expect(list).toHaveTextContent("2.0 KB");
    expect(getArtifactDetail).toHaveBeenCalledWith("a1", undefined, "/p");

    // Collapse then expand again: the cached fetch must not repeat.
    fireEvent.click(screen.getAllByTestId("toggle-versions")[0]);
    expect(screen.queryByTestId("version-list")).toBeNull();
    fireEvent.click(screen.getAllByTestId("toggle-versions")[0]);
    await waitFor(() =>
      expect(screen.getByTestId("version-list")).toBeInTheDocument(),
    );
    expect(getArtifactDetail).toHaveBeenCalledTimes(1);
  });

  it("shows an error when the version fetch fails", async () => {
    const { getArtifactDetail } = await import("../../api/artifacts");
    vi.mocked(getArtifactDetail).mockClear();
    vi.mocked(getArtifactDetail).mockRejectedValueOnce(
      new Error("versions unavailable"),
    );
    render(<ArtifactList projectPath="/p" refreshKey={7} />);
    await waitFor(() => expect(screen.getByText("One")).toBeInTheDocument());
    fireEvent.click(screen.getAllByTestId("toggle-versions")[0]);
    expect(await screen.findByTestId("version-list")).toHaveTextContent(
      /versions unavailable/i,
    );
  });
});
