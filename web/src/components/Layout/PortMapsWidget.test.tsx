import { render, screen, waitFor, fireEvent } from "@testing-library/react";
import { describe, it, expect, vi, beforeEach } from "vitest";

const mockIsPortMapsAvailable = vi.hoisted(() => vi.fn());
const mockListPortMaps = vi.hoisted(() => vi.fn());
const mockAddPortMap = vi.hoisted(() => vi.fn());
const mockRemovePortMap = vi.hoisted(() => vi.fn());
const mockSetPortMapEnabled = vi.hoisted(() => vi.fn());

const FakeApiError = vi.hoisted(() => {
  return class FakeApiError extends Error {
    status: number;
    constructor(message: string, status: number) {
      super(message);
      this.status = status;
    }
  };
});

vi.mock("../../api/client", () => ({
  isPortMapsAvailable: mockIsPortMapsAvailable,
  ApiError: FakeApiError,
  api: {
    listPortMaps: mockListPortMaps,
    addPortMap: mockAddPortMap,
    removePortMap: mockRemovePortMap,
    setPortMapEnabled: mockSetPortMapEnabled,
  },
}));

import PortMapsWidget from "./PortMapsWidget";

describe("PortMapsWidget", () => {
  beforeEach(() => {
    mockIsPortMapsAvailable.mockReset();
    mockListPortMaps.mockReset().mockResolvedValue([]);
    mockAddPortMap.mockReset();
    mockRemovePortMap.mockReset();
    mockSetPortMapEnabled.mockReset();
  });

  it("renders nothing when the desktop routes aren't available", async () => {
    mockIsPortMapsAvailable.mockResolvedValue(false);
    const { container } = render(<PortMapsWidget />);
    await waitFor(() => expect(mockIsPortMapsAvailable).toHaveBeenCalled());
    expect(container.firstChild).toBeNull();
  });

  it("shows the button and loads the list when available", async () => {
    mockIsPortMapsAvailable.mockResolvedValue(true);
    mockListPortMaps.mockResolvedValue([
      { remote_port: 3000, local_port: 3000, enabled: true, live: true },
    ]);
    render(<PortMapsWidget />);
    const btn = await screen.findByTitle("Port forwards");
    fireEvent.click(btn);
    expect(await screen.findByText(/localhost:3000/)).toBeInTheDocument();
    expect(screen.getByText("live")).toBeInTheDocument();
  });

  it("adds a port map from the form", async () => {
    mockIsPortMapsAvailable.mockResolvedValue(true);
    mockListPortMaps.mockResolvedValue([]);
    mockAddPortMap.mockResolvedValue([
      { remote_port: 4000, local_port: 4000, enabled: true, live: false },
    ]);
    render(<PortMapsWidget />);
    fireEvent.click(await screen.findByTitle("Port forwards"));
    await screen.findByText(/No extra port forwards yet/);

    fireEvent.change(screen.getByPlaceholderText("3000"), { target: { value: "4000" } });
    fireEvent.click(screen.getByTitle("Add"));

    await waitFor(() => expect(mockAddPortMap).toHaveBeenCalledWith(4000, 4000));
    expect(await screen.findByText(/localhost:4000/)).toBeInTheDocument();
  });

  it("shows the API error message when add fails", async () => {
    mockIsPortMapsAvailable.mockResolvedValue(true);
    mockListPortMaps.mockResolvedValue([]);
    mockAddPortMap.mockRejectedValue(new FakeApiError("port already in use", 409));
    render(<PortMapsWidget />);
    fireEvent.click(await screen.findByTitle("Port forwards"));
    await screen.findByText(/No extra port forwards yet/);

    fireEvent.change(screen.getByPlaceholderText("3000"), { target: { value: "4000" } });
    fireEvent.click(screen.getByTitle("Add"));

    expect(await screen.findByText("port already in use")).toBeInTheDocument();
  });
});
