import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import BrowserForm from "./BrowserForm";
import { api } from "../../api/client";

vi.mock("../../api/client", () => ({
  api: {
    getBrowserConfig: vi.fn(),
    setBrowserConfig: vi.fn(),
    getHtrStatus: vi.fn(),
    startHtr: vi.fn(),
    stopHtr: vi.fn(),
    listHtrTabs: vi.fn(),
  },
}));

const mockBrowser = vi.mocked(api.getBrowserConfig);
const mockStatus = vi.mocked(api.getHtrStatus);
const mockStart = vi.mocked(api.startHtr);
const mockStop = vi.mocked(api.stopHtr);
const mockTabs = vi.mocked(api.listHtrTabs);

const stopped = {
  enabled: false,
  running: false,
  managed: false,
  addr: "127.0.0.1:3846",
  port: 3846,
  socket: "",
  binary: "",
};
const running = {
  enabled: true,
  running: true,
  managed: true,
  addr: "127.0.0.1:3846",
  port: 3846,
  socket: "",
  binary: "/opt/htrcli",
};

beforeEach(() => {
  vi.clearAllMocks();
  mockBrowser.mockResolvedValue({ chrome_path: "", idle_timeout_minutes: 10, screencast_quality: 85 } as never);
  mockStatus.mockResolvedValue(stopped as never);
  mockStart.mockResolvedValue(running as never);
  mockStop.mockResolvedValue(stopped as never);
  mockTabs.mockResolvedValue({
    tabs: [{ id: 1, url: "https://example.com", title: "Example", active: true, browser: "chrome" }],
  } as never);
});

afterEach(() => {
  vi.restoreAllMocks();
});

describe("BrowserForm HTR daemon", () => {
  it("shows the stopped state when no daemon is running", async () => {
    render(<BrowserForm />);
    await waitFor(() => expect(mockStatus).toHaveBeenCalled());
    expect(screen.getByTestId("htr-status").textContent).toContain("Stopped");
  });

  it("labels the running daemon with the port it started on", async () => {
    mockStatus.mockResolvedValue(running as never);
    render(<BrowserForm />);
    await waitFor(() => expect(screen.getByTestId("htr-status").textContent).toContain("127.0.0.1:3846"));
    expect(screen.getByTestId("htr-status").textContent).toContain("/opt/htrcli");
  });

  it("starts the daemon and adopts the returned running status", async () => {
    render(<BrowserForm />);
    await waitFor(() => expect(mockStatus).toHaveBeenCalled());

    fireEvent.click(screen.getByTestId("htr-start"));

    await waitFor(() => expect(mockStart).toHaveBeenCalledTimes(1));
    await waitFor(() => expect(screen.getByTestId("htr-status").textContent).toContain("127.0.0.1:3846"));
    expect(screen.getByTestId("htr-enabled")).toBeChecked();
  });

  it("stops the daemon and clears the enabled flag", async () => {
    mockStatus.mockResolvedValue(running as never);
    render(<BrowserForm />);
    await waitFor(() => expect(screen.getByTestId("htr-stop")).toBeEnabled());

    fireEvent.click(screen.getByTestId("htr-stop"));

    await waitFor(() => expect(mockStop).toHaveBeenCalledTimes(1));
    await waitFor(() => expect(screen.getByTestId("htr-status").textContent).toContain("Stopped"));
    expect(screen.getByTestId("htr-enabled")).not.toBeChecked();
  });

  it("lists the connected browser tabs", async () => {
    mockStatus.mockResolvedValue(running as never);
    render(<BrowserForm />);
    await waitFor(() => expect(mockStatus).toHaveBeenCalled());

    fireEvent.click(screen.getByTestId("htr-list-tabs"));

    await waitFor(() => expect(mockTabs).toHaveBeenCalledTimes(1));
    const list = await screen.findByTestId("htr-tabs");
    expect(list.textContent).toContain("Example");
    expect(list.textContent).toContain("https://example.com");
  });

  it("surfaces a start failure reported in the status body", async () => {
    mockStart.mockResolvedValue({
      ...stopped,
      error: "HTR automation is unavailable: htrcli not found",
    } as never);
    render(<BrowserForm />);
    await waitFor(() => expect(mockStatus).toHaveBeenCalled());

    fireEvent.click(screen.getByTestId("htr-start"));

    await waitFor(() =>
      expect(screen.getByTestId("htr-error").textContent).toContain("htrcli not found"),
    );
  });
});
