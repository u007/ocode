import { beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import DirectoryBrowser from "./DirectoryBrowser";

const hoisted = vi.hoisted(() => ({
  api: { browseDirectory: vi.fn() },
}));

vi.mock("../../api/client", () => ({ api: hoisted.api }));

const directories = [
  { name: "Alpha", path: "/root/Alpha" },
  { name: "Beta", path: "/root/Beta" },
];

beforeEach(() => {
  vi.clearAllMocks();
  hoisted.api.browseDirectory.mockResolvedValue({
    current_path: "/root",
    parent_path: "/",
    directories,
  });
});

describe("DirectoryBrowser keyboard navigation", () => {
  it("enters the folder list from search and confirms the focused folder with Enter", async () => {
    const onSelect = vi.fn();
    const onOpenChange = vi.fn();
    render(<DirectoryBrowser open onOpenChange={onOpenChange} onSelect={onSelect} />);
    await waitFor(() => expect(screen.getByText("Alpha")).toBeInTheDocument());

    const filter = screen.getByPlaceholderText("Filter by keywords...");
    const row = screen.getByText("Alpha").closest("button");
    expect(row).not.toBeNull();

    filter.focus();
    fireEvent.keyDown(filter, { key: "ArrowDown" });
    expect(document.activeElement).toBe(row);

    fireEvent.keyDown(row!, { key: "Enter" });
    expect(onSelect).toHaveBeenCalledWith("/root/Alpha");
    expect(onOpenChange).toHaveBeenCalledWith(false);
  });

  it("keeps double-click as folder navigation", async () => {
    render(<DirectoryBrowser open onOpenChange={vi.fn()} onSelect={vi.fn()} />);
    await waitFor(() => expect(screen.getByText("Alpha")).toBeInTheDocument());

    fireEvent.doubleClick(screen.getByText("Alpha").closest("button")!);
    await waitFor(() =>
      expect(hoisted.api.browseDirectory).toHaveBeenLastCalledWith("/root/Alpha"),
    );
  });
});
