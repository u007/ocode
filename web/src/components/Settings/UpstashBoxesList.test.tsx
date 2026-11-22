import { afterEach, beforeEach, describe, expect, it, vi, type Mock } from "vitest";
import { act, cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { api } from "../../api/client";
import type { UpstashAccount, UpstashBox } from "../../api/types";
import UpstashBoxesList from "./UpstashBoxesList";

// Settings → Upstash Box → Boxes. What these tests protect:
//  1. Pause and Resume are offered only where the box state allows them.
//  2. Delete asks first, names the box, and refetches after it succeeds.
//  3. A server error is shown with the server's own message.
//  4. Paging requests the next page with the same account and page size.
//  5. Polling runs only while the page is visible and stops on unmount.

const ACCOUNTS: UpstashAccount[] = [
  {
    id: "acc-1",
    name: "Work",
    key_masked: "upst••••9f2c",
    size: "medium",
    idle_minutes: 15,
    hard_cap_minutes: 120,
    retain_days: 14,
    is_default: true,
  },
  {
    id: "acc-2",
    name: "Personal",
    key_masked: "upst••••7a10",
    size: "small",
    idle_minutes: 10,
    hard_cap_minutes: 60,
    retain_days: 7,
    is_default: false,
  },
];

const NOW = Date.now();
const iso = (ms: number) => new Date(ms).toISOString();

const BOXES: UpstashBox[] = [
  {
    id: "box-run",
    name: "Alpha",
    status: "running",
    size: "small",
    kind: "persistent",
    ours: true,
    session_id: "sess_abcdef0123456789",
    login_state: "work",
    last_used: iso(NOW - 2 * 60_000),
    // 42 minutes plus a few seconds, so the rounded label is "in 42 min".
    expires_at: iso(NOW + 42 * 60_000 + 5_000),
    created_at: iso(NOW - 3_600_000),
  },
  {
    id: "box-eph",
    name: "Beta",
    status: "running",
    size: "small",
    kind: "ephemeral",
    ours: false,
    session_id: "",
    login_state: "",
    last_used: "",
    expires_at: "",
    created_at: iso(NOW - 3_600_000),
  },
  {
    id: "box-paused",
    name: "Gamma",
    status: "paused",
    size: "medium",
    kind: "persistent",
    ours: true,
    session_id: "",
    login_state: "",
    last_used: "",
    expires_at: "",
    created_at: iso(NOW - 3_600_000),
  },
  {
    id: "box-idle",
    name: "Delta",
    status: "idle",
    size: "large",
    kind: "persistent",
    ours: true,
    session_id: "",
    login_state: "",
    last_used: "",
    expires_at: "",
    created_at: iso(NOW - 3_600_000),
  },
];

let listBoxes: Mock<typeof api.upstashListBoxes>;
let pauseBox: Mock<typeof api.upstashPauseBox>;
let resumeBox: Mock<typeof api.upstashResumeBox>;
let deleteBox: Mock<typeof api.upstashDeleteBox>;
let visibility: DocumentVisibilityState;

beforeEach(() => {
  visibility = "visible";
  Object.defineProperty(document, "visibilityState", {
    configurable: true,
    get: () => visibility,
  });
  listBoxes = vi.fn(async () => ({
    boxes: BOXES,
    total: BOXES.length,
    page: 1,
    page_size: 20,
    account_id: "acc-1",
  }));
  pauseBox = vi.fn(async () => undefined);
  resumeBox = vi.fn(async () => undefined);
  deleteBox = vi.fn(async () => undefined);
  vi.spyOn(api, "upstashListBoxes").mockImplementation(listBoxes);
  vi.spyOn(api, "upstashPauseBox").mockImplementation(pauseBox);
  vi.spyOn(api, "upstashResumeBox").mockImplementation(resumeBox);
  vi.spyOn(api, "upstashDeleteBox").mockImplementation(deleteBox);
});

afterEach(() => {
  cleanup();
  vi.useRealTimers();
  vi.restoreAllMocks();
  Reflect.deleteProperty(document, "visibilityState");
});

describe("UpstashBoxesList — rows", () => {
  it("renders each box with its id, login state, shortened session, relative times and the ours note", async () => {
    render(<UpstashBoxesList accounts={ACCOUNTS} />);

    const table = await screen.findByRole("table");
    const alpha = within(table).getByText("Alpha").closest("tr") as HTMLElement;
    expect(within(alpha).getByText("box-run")).toBeTruthy();
    expect(within(alpha).getByText("Persistent")).toBeTruthy();
    expect(within(alpha).getByText("sess_abc…")).toBeTruthy();
    expect(within(alpha).getByText("work")).toBeTruthy();
    expect(within(alpha).getByText("2 min ago")).toBeTruthy();
    expect(within(alpha).getByText("in 42 min")).toBeTruthy();
    expect(within(alpha).queryByText("not created by ocode here")).toBeNull();

    const beta = within(table).getByText("Beta").closest("tr") as HTMLElement;
    expect(within(beta).getByText("Ephemeral")).toBeTruthy();
    expect(within(beta).getByText("not created by ocode here")).toBeTruthy();
    // Empty login state, session and timestamps render as a dash, not as blank cells.
    expect(within(beta).getAllByText("—")).toHaveLength(4);
  });

  it("defaults to the default account", async () => {
    render(<UpstashBoxesList accounts={ACCOUNTS} />);
    await screen.findByRole("table");
    expect(listBoxes).toHaveBeenCalledWith("acc-1", 1, 20);
  });

  it("renders the empty state", async () => {
    listBoxes.mockImplementation(async () => ({
      boxes: [],
      total: 0,
      page: 1,
      page_size: 20,
      account_id: "acc-1",
    }));
    render(<UpstashBoxesList accounts={ACCOUNTS} />);
    expect(await screen.findByText("No boxes on this account.")).toBeTruthy();
  });

  it("renders a load error with the server's message and a Retry button", async () => {
    listBoxes.mockRejectedValueOnce(new Error("Upstash rejected the key."));
    render(<UpstashBoxesList accounts={ACCOUNTS} />);
    const alert = await screen.findByRole("alert");
    expect(alert.textContent).toContain("Could not load boxes: Upstash rejected the key.");
    fireEvent.click(within(alert).getByRole("button", { name: "Retry" }));
    await waitFor(() => expect(listBoxes).toHaveBeenCalledTimes(2));
  });
});

describe("UpstashBoxesList — row actions", () => {
  it("offers Pause only for a persistent box that is running or idle", async () => {
    render(<UpstashBoxesList accounts={ACCOUNTS} />);
    await screen.findByRole("table");

    expect(screen.getByRole("button", { name: "Pause Alpha" })).toBeTruthy();
    expect(screen.getByRole("button", { name: "Pause Delta" })).toBeTruthy();
    // Ephemeral and running: not pausable.
    expect(screen.queryByRole("button", { name: "Pause Beta" })).toBeNull();
    // Paused: resumable, not pausable.
    expect(screen.queryByRole("button", { name: "Pause Gamma" })).toBeNull();
  });

  it("offers Resume only for a paused box", async () => {
    render(<UpstashBoxesList accounts={ACCOUNTS} />);
    await screen.findByRole("table");

    expect(screen.getByRole("button", { name: "Resume Gamma" })).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Resume Alpha" })).toBeNull();
    expect(screen.queryByRole("button", { name: "Resume Beta" })).toBeNull();
  });

  it("sends Pause with the box id and the account, then refetches", async () => {
    render(<UpstashBoxesList accounts={ACCOUNTS} />);
    await screen.findByRole("table");

    fireEvent.click(screen.getByRole("button", { name: "Pause Alpha" }));
    await waitFor(() => expect(pauseBox).toHaveBeenCalledWith("box-run", "acc-1"));
    await waitFor(() => expect(listBoxes).toHaveBeenCalledTimes(2));
    expect(resumeBox).not.toHaveBeenCalled();
  });

  it("keeps a row's buttons disabled while its own request runs, even when another row starts one", async () => {
    let finishPause: () => void = () => undefined;
    pauseBox.mockImplementation(
      () =>
        new Promise<void>((resolve) => {
          finishPause = resolve;
        }),
    );
    render(<UpstashBoxesList accounts={ACCOUNTS} />);
    await screen.findByRole("table");

    fireEvent.click(screen.getByRole("button", { name: "Pause Alpha" }));
    fireEvent.click(screen.getByRole("button", { name: "Pause Delta" }));
    await waitFor(() => expect(pauseBox).toHaveBeenCalledTimes(2));

    // Both requests are still open: both rows stay disabled.
    expect((screen.getByRole("button", { name: "Pause Alpha" }) as HTMLButtonElement).disabled).toBe(true);
    expect((screen.getByRole("button", { name: "Pause Delta" }) as HTMLButtonElement).disabled).toBe(true);

    await act(async () => {
      finishPause();
    });
    await waitFor(() => expect(listBoxes).toHaveBeenCalledTimes(2));
  });

  it("delete asks for confirmation before calling DELETE, then refetches", async () => {
    render(<UpstashBoxesList accounts={ACCOUNTS} />);
    await screen.findByRole("table");

    fireEvent.click(screen.getByRole("button", { name: "Delete Alpha" }));
    expect(await screen.findByText("Delete Alpha?")).toBeTruthy();
    expect(screen.getByText("The box is deleted permanently. This cannot be undone.")).toBeTruthy();
    expect(deleteBox).not.toHaveBeenCalled();

    fireEvent.click(screen.getByRole("button", { name: "Delete" }));
    await waitFor(() => expect(deleteBox).toHaveBeenCalledWith("box-run", "acc-1"));
    await waitFor(() => expect(listBoxes).toHaveBeenCalledTimes(2));
  });

  it("shows the server's message inline when an action fails", async () => {
    pauseBox.mockRejectedValueOnce(new Error("Box is busy, try again."));
    render(<UpstashBoxesList accounts={ACCOUNTS} />);
    await screen.findByRole("table");

    fireEvent.click(screen.getByRole("button", { name: "Pause Alpha" }));
    const alert = await screen.findByRole("alert");
    expect(alert.textContent).toBe("Box is busy, try again.");
    // A failed action does not refetch; the rows are still the last good list.
    expect(listBoxes).toHaveBeenCalledTimes(1);
    expect(screen.getByRole("button", { name: "Pause Alpha" })).toBeTruthy();
  });
});

describe("UpstashBoxesList — paging", () => {
  it("requests page 2 with the same account and page size of 20", async () => {
    listBoxes.mockImplementation(async (_acc, page, pageSize) => ({
      boxes: BOXES,
      total: 45,
      page,
      page_size: pageSize,
      account_id: "acc-1",
    }));
    render(<UpstashBoxesList accounts={ACCOUNTS} />);
    expect(await screen.findByText("Page 1 of 3")).toBeTruthy();

    fireEvent.click(screen.getByRole("button", { name: "Next" }));
    await waitFor(() => expect(listBoxes).toHaveBeenLastCalledWith("acc-1", 2, 20));
    expect(await screen.findByText("Page 2 of 3")).toBeTruthy();
    expect((screen.getByRole("button", { name: "Prev" }) as HTMLButtonElement).disabled).toBe(false);
  });
});

describe("UpstashBoxesList — polling", () => {
  it("polls every 15 s while visible and stops after unmount", async () => {
    vi.useFakeTimers();
    const { unmount } = render(<UpstashBoxesList accounts={ACCOUNTS} />);
    await act(async () => {});
    expect(listBoxes).toHaveBeenCalledTimes(1);

    await act(async () => {
      vi.advanceTimersByTime(15_000);
    });
    expect(listBoxes).toHaveBeenCalledTimes(2);

    unmount();
    await act(async () => {
      vi.advanceTimersByTime(60_000);
    });
    expect(listBoxes).toHaveBeenCalledTimes(2);
  });

  it("does not poll while the document is hidden", async () => {
    vi.useFakeTimers();
    visibility = "hidden";
    render(<UpstashBoxesList accounts={ACCOUNTS} />);
    await act(async () => {});
    expect(listBoxes).toHaveBeenCalledTimes(1);

    await act(async () => {
      vi.advanceTimersByTime(45_000);
    });
    expect(listBoxes).toHaveBeenCalledTimes(1);
  });
});

describe("UpstashBoxesList — retained boxes", () => {
  // A retained box that is running, kept six days before it is deleted if unused.
  const RETAINED: UpstashBox = {
    id: "box-ret",
    name: "Epsilon",
    status: "running",
    size: "small",
    kind: "retained",
    ours: true,
    session_id: "sess_retained000001",
    login_state: "work",
    last_used: iso(NOW - 3 * 60 * 60_000),
    expires_at: iso(NOW + 6 * 24 * 60 * 60_000),
    created_at: iso(NOW - 4 * 24 * 60 * 60_000),
  };

  beforeEach(() => {
    listBoxes.mockImplementation(async () => ({
      boxes: [RETAINED],
      total: 1,
      page: 1,
      page_size: 20,
      account_id: "acc-1",
    }));
  });

  it("shows a retained badge and says when the box is deleted if unused, not an Expires countdown", async () => {
    render(<UpstashBoxesList accounts={ACCOUNTS} />);
    const table = await screen.findByRole("table");
    const row = within(table).getByText("Epsilon").closest("tr") as HTMLElement;

    expect(within(row).getByText("Retained")).toBeTruthy();
    expect(within(row).queryByText("Persistent")).toBeNull();
    expect(within(row).getByText("work")).toBeTruthy();
    const deleted = within(row).getByText(/^Deleted if unused until /);
    expect(deleted.textContent).toContain(new Date(RETAINED.expires_at).getUTCFullYear().toString());
    expect(within(row).queryByText(/^in /)).toBeNull();
  });

  it("offers Pause for a running retained box, since ocode pauses it too", async () => {
    render(<UpstashBoxesList accounts={ACCOUNTS} />);
    await screen.findByRole("table");
    expect(screen.getByRole("button", { name: "Pause Epsilon" })).toBeTruthy();
    expect(screen.getByRole("button", { name: "Delete Epsilon" })).toBeTruthy();
  });

  it("offers no Resume for a paused retained box: a browser start with its login state resumes it", async () => {
    listBoxes.mockImplementation(async () => ({
      boxes: [{ ...RETAINED, status: "paused" }],
      total: 1,
      page: 1,
      page_size: 20,
      account_id: "acc-1",
    }));
    render(<UpstashBoxesList accounts={ACCOUNTS} />);
    await screen.findByRole("table");
    expect(screen.queryByRole("button", { name: "Resume Epsilon" })).toBeNull();
    expect(screen.queryByRole("button", { name: "Pause Epsilon" })).toBeNull();
  });
});
