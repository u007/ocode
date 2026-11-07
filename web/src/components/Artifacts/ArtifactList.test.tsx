import { describe, it, expect, vi } from "vitest";
import { render, screen, fireEvent, waitFor } from "@testing-library/react";
import ArtifactList from "./ArtifactList";

vi.mock("../../api/artifacts", () => ({
  listArtifacts: vi.fn().mockResolvedValue({ artifacts: [{ id: "a1", name: "One" }, { id: "a2", name: "Two" }], total: 2 }),
}));

vi.mock("../../lib/clipboard", () => ({
  copyTextToClipboard: vi.fn().mockResolvedValue(true),
}));

describe("ArtifactList", () => {
  it("renders paginated results", async () => {
    render(<ArtifactList />);
    await waitFor(() => expect(screen.getByText("One")).toBeInTheDocument());
    expect(screen.getByText("Two")).toBeInTheDocument();
  });

  it("invite copy works", async () => {
    const { copyTextToClipboard } = await import("../../lib/clipboard");
    render(<ArtifactList />);
    await waitFor(() => expect(screen.getAllByTestId("copy-invite").length).toBe(2));
    fireEvent.click(screen.getAllByTestId("copy-invite")[0]);
    await waitFor(() => expect(copyTextToClipboard).toHaveBeenCalled());
  });
});
