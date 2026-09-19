import { render, waitFor } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import ChangesPanel from "./ChangesPanel";

const mockListChanges = vi.fn();

vi.mock("@/api/client", () => ({
  api: {
    listChanges: (...a: unknown[]) => mockListChanges(...a),
    // Provided so ChangesDiffView's module import resolves; not called here.
    getChangeDiff: vi.fn(() => Promise.resolve({ path: "", patch: "" })),
  },
}));

describe("ChangesPanel host routing", () => {
  it("passes the session and project host to listChanges", async () => {
    mockListChanges.mockResolvedValue([]);
    render(<ChangesPanel session="s1" host="james@217.216.72.49" active />);
    await waitFor(() => expect(mockListChanges).toHaveBeenCalledWith("s1", "james@217.216.72.49"));
  });

  it("passes undefined host for a local session", async () => {
    mockListChanges.mockResolvedValue([]);
    render(<ChangesPanel session="s2" active />);
    await waitFor(() => expect(mockListChanges).toHaveBeenCalledWith("s2", undefined));
  });
});
