import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, fireEvent, render, screen, within } from "@testing-library/react";
import { PulseTerminals } from "./PulseTerminals";
import { PULSE_TERMINALS_POLL_MS } from "./usePulseTerminals";
import { ApiError } from "../../api/client";
import type { PulseTerminal, PulseTerminalPage } from "../../api/types";

const mockList = vi.fn();
vi.mock("../../api/client", async (importOriginal) => ({
  ...(await importOriginal<typeof import("../../api/client")>()),
  api: {
    listPulseTerminals: (...a: unknown[]) => mockList(...a),
  },
}));

// The sidebar's project list decides whether Open is enabled; the jump itself
// is covered in jumpToSession.test.tsx. Mocked here so no provider is needed.
const mockOpenTerminal = vi.fn();
let mockSidebarProjects: { path: string; host?: string }[] = [];
vi.mock("@/stores/projectStore", () => ({
  useProjectState: () => ({ state: { projects: mockSidebarProjects } }),
}));
vi.mock("../../lib/jumpToSession", () => ({
  useJumpToTerminal: () => mockOpenTerminal,
}));

function term(overrides: Partial<PulseTerminal> = {}): PulseTerminal {
  return {
    id: "term-1",
    project: "/Users/james/www/app",
    title: "dev server",
    pid: 4242,
    command: "npm run dev",
    running: true,
    ...overrides,
  };
}

function page(terminals: PulseTerminal[], total = terminals.length): PulseTerminalPage {
  return { terminals, total, limit: 50, offset: 0 };
}

beforeEach(() => {
  mockList.mockReset();
  mockOpenTerminal.mockReset();
  mockSidebarProjects = [{ path: "/Users/james/www/app" }, { path: "/Users/james/www/other" }];
  vi.spyOn(console, "error").mockImplementation(() => {});
});
afterEach(() => {
  vi.useRealTimers();
  vi.restoreAllMocks();
});

describe("PulseTerminals", () => {
  it("shows a running program with its full command, and an idle shell as idle", async () => {
    mockList.mockResolvedValue(
      page([
        term({ id: "term-run", title: "dev server", command: "npm run dev -- --port 5173", running: true }),
        term({ id: "term-idle", title: "shell", project: "/Users/james/www/other", command: "zsh", running: false }),
      ]),
    );
    render(<PulseTerminals />);

    const rows = await screen.findAllByTestId("pulse-terminal-row");
    expect(rows).toHaveLength(2);
    expect(rows[0]).toHaveAttribute("data-running", "true");
    expect(rows[0]).toHaveTextContent("npm run dev -- --port 5173");
    expect(rows[1]).toHaveAttribute("data-running", "false");
    expect(rows[1]).toHaveTextContent("idle");
    expect(rows[1]).not.toHaveTextContent("zsh");
    expect(screen.getByRole("heading", { name: "Terminals (2)" })).toBeInTheDocument();
    expect(screen.getByLabelText("running program")).toBeInTheDocument();
    expect(screen.getByLabelText("idle shell")).toBeInTheDocument();
  });

  it("says so when no terminal is open", async () => {
    mockList.mockResolvedValue(page([]));
    render(<PulseTerminals />);
    expect(await screen.findByText("No open terminals.")).toBeInTheDocument();
  });

  it("reports a failed listing instead of showing an empty list", async () => {
    mockList.mockRejectedValue(new Error("terminal requires server authentication"));
    render(<PulseTerminals />);
    expect(await screen.findByRole("alert")).toHaveTextContent("terminal requires server authentication");
    expect(screen.queryByText("No open terminals.")).not.toBeInTheDocument();
  });

  it("refreshes on an interval so a program that starts or stops shows up", async () => {
    vi.useFakeTimers();
    mockList.mockResolvedValue(page([]));
    render(<PulseTerminals />);
    await act(async () => {});
    expect(mockList).toHaveBeenCalledTimes(1);

    mockList.mockResolvedValue(page([term()]));
    await act(async () => {
      vi.advanceTimersByTime(PULSE_TERMINALS_POLL_MS);
    });
    expect(mockList).toHaveBeenCalledTimes(2);
    expect(screen.getByTestId("pulse-terminal-row")).toHaveAttribute("data-running", "true");
  });

  it("expands a row to show its full command, project, pid and terminal id", async () => {
    mockList.mockResolvedValue(
      page([term({ id: "term-run", command: "npm run dev -- --port 5173 --host 0.0.0.0", pid: 9001 })]),
    );
    render(<PulseTerminals />);
    const row = await screen.findByTestId("pulse-terminal-row");
    expect(screen.queryByTestId("pulse-terminal-details")).not.toBeInTheDocument();

    const toggle = within(row).getByRole("button", { name: "Show terminal details" });
    expect(toggle).toHaveAttribute("aria-expanded", "false");
    fireEvent.click(toggle);

    expect(within(row).getByRole("button", { name: "Hide terminal details" })).toHaveAttribute("aria-expanded", "true");
    const details = screen.getByTestId("pulse-terminal-details");
    expect(within(details).getByText("npm run dev -- --port 5173 --host 0.0.0.0")).toBeInTheDocument();
    expect(within(details).getByText("/Users/james/www/app")).toBeInTheDocument();
    expect(within(details).getByText("9001")).toBeInTheDocument();
    expect(within(details).getByText("term-run")).toBeInTheDocument();
  });

  it("opens the terminal's project and terminal from the Open button", async () => {
    mockList.mockResolvedValue(page([term({ id: "term-1", title: "dev server" })]));
    render(<PulseTerminals />);
    const open = await screen.findByRole("button", { name: "Open" });

    fireEvent.click(open);

    expect(mockOpenTerminal).toHaveBeenCalledWith({
      projectPath: "/Users/james/www/app",
      terminalId: "term-1",
      title: "dev server",
    });
  });

  it("disables Open when the terminal's project is not a local project in the sidebar", async () => {
    mockSidebarProjects = [{ path: "/Users/james/www/app", host: "user@box" }];
    mockList.mockResolvedValue(page([term()]));
    render(<PulseTerminals />);

    const open = await screen.findByRole("button", { name: "Open" });

    expect(open).toBeDisabled();
    expect(open).toHaveAttribute("title", "This project is not in the sidebar");
  });

  it.each([403, 501])("stops polling and hides the section when the server answers %i", async (status) => {
    vi.useFakeTimers();
    mockList.mockRejectedValue(new ApiError("terminals unavailable", status));
    const { container } = render(<PulseTerminals />);
    await act(async () => {});
    expect(container).toBeEmptyDOMElement();

    await act(async () => {
      vi.advanceTimersByTime(PULSE_TERMINALS_POLL_MS * 3);
    });
    expect(mockList).toHaveBeenCalledTimes(1);
  });
});
