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

// The widget reads the active project through the project store. ProjectProvider
// is deliberately not mounted: it runs the app's full project-loading effects,
// and these tests only need the state shape. remoteForwardTarget (the trust rule
// under test) is the real implementation.
const projectState = vi.hoisted(() => ({
  value: { projects: [] as { path: string; host?: string; remote_kind?: string }[], activeProject: null as null | { path: string } },
}));
vi.mock("../../stores/projectStore", () => ({
  useProjectState: () => ({ state: projectState.value, dispatch: vi.fn() }),
}));

import PortMapsWidget from "./PortMapsWidget";

const SSH_PROJECT = { path: "~/www/kakiit", host: "james@217.216.72.49", remote_kind: "ssh" };
const SSH_TARGET = { host: "james@217.216.72.49", path: "~/www/kakiit" };

function setActiveProject(project: { path: string; host?: string; remote_kind?: string } | null) {
  projectState.value = {
    projects: project ? [project] : [],
    activeProject: project ? { path: project.path } : null,
  };
}

describe("PortMapsWidget", () => {
  beforeEach(() => {
    projectState.value = { projects: [], activeProject: null };
    mockIsPortMapsAvailable.mockReset();
    mockListPortMaps.mockReset().mockResolvedValue([]);
    mockAddPortMap.mockReset();
    mockRemovePortMap.mockReset();
    mockSetPortMapEnabled.mockReset();
  });

  it("renders nothing when no port-map route is available", async () => {
    mockIsPortMapsAvailable.mockResolvedValue(false);
    const { container } = render(<PortMapsWidget />);
    await waitFor(() => expect(mockIsPortMapsAvailable).toHaveBeenCalled());
    expect(container.firstChild).toBeNull();
    // No remote project active → the desktop remote-workspace family is probed.
    expect(mockIsPortMapsAvailable).toHaveBeenCalledWith(undefined);
  });

  it("probes the project-scoped route with host+project when a remote SSH project is active", async () => {
    setActiveProject(SSH_PROJECT);
    mockIsPortMapsAvailable.mockResolvedValue(true);
    render(<PortMapsWidget />);
    await waitFor(() => expect(mockIsPortMapsAvailable).toHaveBeenCalledWith(SSH_TARGET));
    expect(await screen.findByTitle("Port forwards")).toBeInTheDocument();
  });

  it("uses the desktop fallback target for a local project", async () => {
    setActiveProject({ path: "/Users/james/www/ocode" });
    mockIsPortMapsAvailable.mockResolvedValue(false);
    render(<PortMapsWidget />);
    await waitFor(() => expect(mockIsPortMapsAvailable).toHaveBeenCalledWith(undefined));
  });

  it("never routes a WSL project to the project-scoped family (WSL shares localhost)", async () => {
    setActiveProject({ path: "~/www/win", host: "wsl:Ubuntu", remote_kind: "wsl" });
    mockIsPortMapsAvailable.mockResolvedValue(false);
    const { container } = render(<PortMapsWidget />);
    // WSL2 shares the Windows loopback, so the server refuses forwards for it —
    // remoteForwardTarget returns null and the desktop fallback is probed
    // instead (undefined target), which is unavailable in a local session.
    await waitFor(() => expect(mockIsPortMapsAvailable).toHaveBeenCalledWith(undefined));
    expect(container.firstChild).toBeNull();
  });

  it("loads the active project's forwards and shows live state", async () => {
    setActiveProject(SSH_PROJECT);
    mockIsPortMapsAvailable.mockResolvedValue(true);
    mockListPortMaps.mockResolvedValue([
      { remote_port: 3000, local_port: 3000, enabled: true, live: true },
    ]);
    render(<PortMapsWidget />);
    fireEvent.click(await screen.findByTitle("Port forwards"));
    expect(await screen.findByText(/localhost:3000/)).toBeInTheDocument();
    expect(screen.getByText("live")).toBeInTheDocument();
    await waitFor(() => expect(mockListPortMaps).toHaveBeenCalledWith(SSH_TARGET));
  });

  it("adds a forward against the active project", async () => {
    setActiveProject(SSH_PROJECT);
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

    await waitFor(() => expect(mockAddPortMap).toHaveBeenCalledWith(4000, 4000, SSH_TARGET));
    expect(await screen.findByText(/localhost:4000/)).toBeInTheDocument();
  });

  it("removes and toggles against the active project", async () => {
    setActiveProject(SSH_PROJECT);
    mockIsPortMapsAvailable.mockResolvedValue(true);
    mockListPortMaps.mockResolvedValue([
      { remote_port: 5173, local_port: 5173, enabled: true, live: true },
    ]);
    mockRemovePortMap.mockResolvedValue([]);
    mockSetPortMapEnabled.mockResolvedValue([
      { remote_port: 5173, local_port: 5173, enabled: false, live: false },
    ]);
    render(<PortMapsWidget />);
    fireEvent.click(await screen.findByTitle("Port forwards"));
    await screen.findByText(/localhost:5173/);

    fireEvent.click(screen.getByText("Disable"));
    await waitFor(() => expect(mockSetPortMapEnabled).toHaveBeenCalledWith(5173, false, SSH_TARGET));
    expect(await screen.findByText("disabled")).toBeInTheDocument();

    fireEvent.click(screen.getByTitle("Remove"));
    await waitFor(() => expect(mockRemovePortMap).toHaveBeenCalledWith(5173, SSH_TARGET));
    expect(await screen.findByText(/No extra port forwards yet/)).toBeInTheDocument();
  });

  it("re-probes and drops the previous project's rows when the active project changes", async () => {
    setActiveProject(SSH_PROJECT);
    mockIsPortMapsAvailable.mockResolvedValue(true);
    mockListPortMaps.mockResolvedValue([
      { remote_port: 3000, local_port: 3000, enabled: true, live: true },
    ]);
    const { rerender } = render(<PortMapsWidget />);
    fireEvent.click(await screen.findByTitle("Port forwards"));
    await screen.findByText(/localhost:3000/);

    setActiveProject({ path: "~/www/aimsai2", host: "james@217.216.72.49", remote_kind: "ssh" });
    rerender(<PortMapsWidget />);
    await waitFor(() =>
      expect(mockIsPortMapsAvailable).toHaveBeenLastCalledWith({
        host: "james@217.216.72.49",
        path: "~/www/aimsai2",
      }),
    );
    // The dialog closed on switch, so the stale row is not rendered.
    expect(screen.queryByText(/localhost:3000/)).not.toBeInTheDocument();
  });

  it("shows the API error message when add fails", async () => {
    setActiveProject(SSH_PROJECT);
    mockIsPortMapsAvailable.mockResolvedValue(true);
    mockListPortMaps.mockResolvedValue([]);
    mockAddPortMap.mockRejectedValue(new FakeApiError("port already in use", 502));
    render(<PortMapsWidget />);
    fireEvent.click(await screen.findByTitle("Port forwards"));
    await screen.findByText(/No extra port forwards yet/);

    fireEvent.change(screen.getByPlaceholderText("3000"), { target: { value: "4000" } });
    fireEvent.click(screen.getByTitle("Add"));

    expect(await screen.findByText("port already in use")).toBeInTheDocument();
  });

  it("shows a readable error when the list call returns a non-JSON 200 (regression)", async () => {
    // Before the fetchJSON guard, a 200 HTML body from the SPA fallback
    // surfaced as WebKit's cryptic "SyntaxError: The string did not match
    // the expected pattern." fetchJSON now throws a readable ApiError.
    setActiveProject(SSH_PROJECT);
    mockIsPortMapsAvailable.mockResolvedValue(true);
    mockListPortMaps.mockRejectedValue(
      new FakeApiError(
        "Non-JSON response from /api/desktop/portmaps (status 200, content-type text/html)",
        200,
      ),
    );
    render(<PortMapsWidget />);
    fireEvent.click(await screen.findByTitle("Port forwards"));
    expect(
      await screen.findByText(/Non-JSON response from \/api\/desktop\/portmaps/),
    ).toBeInTheDocument();
  });
});
