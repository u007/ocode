import { beforeEach, describe, expect, it, vi } from "vitest";
import { render, waitFor } from "@testing-library/react";
import { api } from "../../api/client";
import MediaViewer from "./MediaViewer";

vi.mock("../../api/client", () => ({
  api: { getMediaToken: vi.fn(), fetchFileRaw: vi.fn() },
  apiPath: (p: string) => p,
}));

beforeEach(() => {
  vi.clearAllMocks();
  // jsdom has no createObjectURL.
  URL.createObjectURL = vi.fn(() => "blob:mock");
  URL.revokeObjectURL = vi.fn();
});

describe("MediaViewer", () => {
  it("streams local media through a single-file capability URL", async () => {
    vi.mocked(api.getMediaToken).mockResolvedValue({ token: "tok123" });
    const { container } = render(<MediaViewer path="media/clip.mp4" projectRoot="/proj" kind="video" />);

    await waitFor(() => expect(container.querySelector("video")).toBeTruthy());
    expect(api.getMediaToken).toHaveBeenCalledWith("media/clip.mp4", "/proj");
    expect(container.querySelector("video")?.getAttribute("src")).toBe(
      "/api/files/raw?path=media%2Fclip.mp4&project_root=%2Fproj&media_token=tok123",
    );
    // No whole-file fetch on the streaming path.
    expect(api.fetchFileRaw).not.toHaveBeenCalled();
  });

  it("keeps the blob path for remote projects (no range transport over SSH)", async () => {
    vi.mocked(api.fetchFileRaw).mockResolvedValue(new ArrayBuffer(8));
    const { container } = render(<MediaViewer path="media/clip.mp4" projectHost="ci.local" kind="video" />);

    await waitFor(() => expect(container.querySelector("video")).toBeTruthy());
    expect(api.fetchFileRaw).toHaveBeenCalledWith("media/clip.mp4", undefined, "ci.local");
    expect(api.getMediaToken).not.toHaveBeenCalled();
    expect(container.querySelector("video")?.getAttribute("src")).toBe("blob:mock");
  });

  it("degrades to the blob path when the capability endpoint is unavailable", async () => {
    vi.mocked(api.getMediaToken).mockRejectedValue(new Error("404 not found"));
    vi.mocked(api.fetchFileRaw).mockResolvedValue(new ArrayBuffer(8));
    render(<MediaViewer path="media/song.mp3" kind="audio" />);

    await waitFor(() => expect(api.fetchFileRaw).toHaveBeenCalled());
  });

  it("retries once with a fresh capability when the element errors", async () => {
    vi.mocked(api.getMediaToken)
      .mockResolvedValueOnce({ token: "stale" })
      .mockResolvedValueOnce({ token: "fresh" });
    const { container } = render(<MediaViewer path="media/clip.webm" kind="video" />);

    await waitFor(() => expect(container.querySelector("video")?.getAttribute("src")).toContain("stale"));
    container.querySelector("video")!.dispatchEvent(new Event("error"));
    await waitFor(() => expect(container.querySelector("video")?.getAttribute("src")).toContain("fresh"));
    expect(api.getMediaToken).toHaveBeenCalledTimes(2);
  });

  it("pauses playback when the pane is hidden", async () => {
    vi.mocked(api.getMediaToken).mockResolvedValue({ token: "t" });
    const pause = vi.spyOn(HTMLMediaElement.prototype, "pause").mockImplementation(() => {});
    const { container, rerender } = render(
      <MediaViewer path="media/clip.mp4" projectRoot="/proj" kind="video" active />,
    );
    await waitFor(() => expect(container.querySelector("video")).toBeTruthy());

    // display:none does not stop playback; the element reports playing so the
    // guard takes the pause branch.
    const video = container.querySelector("video") as HTMLVideoElement;
    Object.defineProperty(video, "paused", { configurable: true, get: () => false });

    rerender(<MediaViewer path="media/clip.mp4" projectRoot="/proj" kind="video" active={false} />);
    await waitFor(() => expect(pause).toHaveBeenCalled());
    pause.mockRestore();
  });
});
