import { act, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const mocks = vi.hoisted(() => ({
  listCronJobs: vi.fn(),
  getCronOutbox: vi.fn(),
  getCronTargets: vi.fn(),
  setCronTarget: vi.fn(),
  drainCronOutbox: vi.fn(),
  listReminderItems: vi.fn(),
  addReminderItem: vi.fn(),
  updateReminderItem: vi.fn(),
  deleteReminderItem: vi.fn(),
  runReminderItem: vi.fn(),
  getCronJob: vi.fn(),
  deleteCronJob: vi.fn(),
  addCronJob: vi.fn(),
  updateCronJob: vi.fn(),
  getReminderItemRuns: vi.fn(),
}));

vi.mock("@/api/client", () => ({ api: mocks }));
vi.mock("./CronJobDialog", () => ({ default: () => null }));
vi.mock("./ReminderTaskDialog", () => ({ default: () => null }));
vi.mock("./CronHistoryPanel", () => ({ default: () => <div data-testid="history" /> }));
vi.mock("./CronOutboxPanel", () => ({ default: () => <div data-testid="outbox" /> }));
vi.mock("./CronTargetsPanel", () => ({ default: () => <div data-testid="targets" /> }));

import CronPanel from "./CronPanel";

/**
 * Mutable server-side state, reset in beforeEach. Modelling the round trip
 * (PATCH mutates, the next GET reflects it) is what makes a status test able to
 * fail: a call-counting mock would let the chip stay "Pending" forever and the
 * test would still pass.
 */
const server: { reminders: any[]; tasks: any[] } = { reminders: [], tasks: [] };

const TASK = {
  id: "t-1",
  kind: "task",
  title: "write the report",
  message: "the quarterly one",
  status: "pending",
  action: "notify",
  auto_complete: false,
  created_at_ms: 1,
  updated_at_ms: 1,
  runs: 0,
};

const REMINDER = {
  id: "r-1",
  kind: "reminder",
  title: "stand up",
  message: "in five minutes",
  status: "pending",
  action: "notify",
  auto_complete: false,
  due_at_ms: Date.now() + 60_000,
  created_at_ms: 2,
  updated_at_ms: 2,
  runs: 0,
};

/**
 * The Cron tab is rendered as FRONTMOST by default, because a sub-view only
 * loads once it has been shown: the pane is `active && subView === <view>`, so
 * `active={false}` would leave it on "Loading…" forever. The one test that
 * wants no polling renders it explicitly.
 */
function renderPanel(active = true) {
  return render(<CronPanel loadingKey="cron" onLoadingEvent={() => {}} active={active} />);
}

beforeEach(() => {
  vi.clearAllMocks();
  mocks.listCronJobs.mockResolvedValue({ jobs: [] });
  mocks.getCronOutbox.mockResolvedValue({ entries: [] });
  mocks.getCronTargets.mockResolvedValue({ targets: {} });
  mocks.setCronTarget.mockResolvedValue({ ok: true });
  mocks.drainCronOutbox.mockResolvedValue({ entries: [] });
  mocks.addReminderItem.mockResolvedValue(TASK);
  mocks.updateReminderItem.mockResolvedValue({ ...TASK, status: "completed" });
  mocks.deleteReminderItem.mockResolvedValue({});
  mocks.runReminderItem.mockResolvedValue(TASK);
  mocks.getReminderItemRuns.mockResolvedValue({ runs: [], total: 0 });
  // A tiny stateful "server". The real endpoints mutate on PATCH and the list
  // re-read reflects it, so a mock that only counts calls cannot express the
  // round trip a status change actually makes — and a test written against it
  // would pass while the UI kept showing a stale chip.
  server.reminders = [{ ...REMINDER }];
  server.tasks = [{ ...TASK }];
  mocks.listReminderItems.mockImplementation(async (kind: string, params?: any) => {
    const rows = kind === "reminder" ? server.reminders : server.tasks;
    const filtered = params?.status
      ? rows.filter((r) => r.status === params.status)
      : rows;
    return {
      items: filtered.map((r) => ({ ...r })),
      total: filtered.length,
      limit: params?.limit ?? 50,
      offset: params?.offset ?? 0,
    };
  });
  mocks.updateReminderItem.mockImplementation(
    async (kind: string, id: string, patch: any) => {
      const rows = kind === "reminder" ? server.reminders : server.tasks;
      const row = rows.find((r) => r.id === id);
      if (!row) throw new Error(`item ${id} not found`);
      Object.assign(row, patch);
      return { ...row };
    },
  );
  mocks.deleteReminderItem.mockImplementation(async (kind: string, id: string) => {
    const rows = kind === "reminder" ? server.reminders : server.tasks;
    const i = rows.findIndex((r) => r.id === id);
    if (i >= 0) rows.splice(i, 1);
    return {};
  });
  mocks.runReminderItem.mockImplementation(async (kind: string, id: string) => {
    const rows = kind === "reminder" ? server.reminders : server.tasks;
    const row = rows.find((r) => r.id === id);
    if (!row) throw new Error(`item ${id} not found`);
    if (row.status === "completed" || row.status === "cancelled") {
      throw new Error(`cannot run a ${row.status} item`);
    }
    row.fired_at_ms = Date.now();
    return { ...row };
  });
});

describe("CronPanel sub-views", () => {
  it("starts on Jobs and switches to Reminders and Tasks", async () => {
    renderPanel();
    await screen.findByText("No cron jobs yet.");

    // Jobs is the selected tab, and its own table is the one on screen.
    expect(screen.getByTestId("cron-subtab-jobs")).toHaveAttribute("aria-selected", "true");

    fireEvent.click(screen.getByTestId("cron-subtab-reminders"));
    expect(screen.getByTestId("cron-subtab-reminders")).toHaveAttribute(
      "aria-selected",
      "true",
    );
    await waitFor(() => {
      expect(screen.getByTestId("cron-subpanel-reminders")).toBeInTheDocument();
    });
    // The reminder list loaded from /api/reminders, not /api/tasks.
    expect(mocks.listReminderItems).toHaveBeenCalledWith(
      "reminder",
      expect.objectContaining({ limit: 50, offset: 0 }),
    );

    fireEvent.click(screen.getByTestId("cron-subtab-tasks"));
    await waitFor(() => {
      expect(screen.getByTestId("cron-subpanel-tasks")).toBeInTheDocument();
    });
    expect(mocks.listReminderItems).toHaveBeenCalledWith(
      "task",
      expect.objectContaining({ limit: 50, offset: 0 }),
    );
  });

  it("keeps the jobs toolbar out of the reminder and task views", async () => {
    renderPanel();
    await screen.findByText("No cron jobs yet.");
    // "Add job" belongs to the jobs view only. Offering it over a task list is
    // how you end up creating a job when you meant to add a task.
    expect(screen.getByRole("button", { name: /Add job/ })).toBeInTheDocument();

    fireEvent.click(screen.getByTestId("cron-subtab-tasks"));
    await waitFor(() => {
      expect(screen.getByTestId("cron-subpanel-tasks")).toBeInTheDocument();
    });
    expect(screen.queryByRole("button", { name: /Add job/ })).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: /Add task/ })).toBeInTheDocument();
  });

  it("badges each sub-tab with its collection total", async () => {
    // Set BEFORE the render: the panel loads on mount, so a mock installed
    // afterwards never reaches the first fetch.
    mocks.listCronJobs.mockResolvedValue({
      jobs: [
        {
          id: "j-1",
          name: "nightly",
          payload: { message: "do it" },
          schedule: { kind: "cron", expr: "0 9 * * *" },
          state: {},
          created_at_ms: 1,
          enabled: true,
        },
      ],
    });
    renderPanel();
    await waitFor(() => {
      expect(screen.getByTestId("cron-subtab-count-jobs")).toHaveTextContent("1");
    });

    // The badges for the two list views appear only once those views have been
    // shown, for the same reason they do not fetch: a hidden list has no total.
    fireEvent.click(screen.getByTestId("cron-subtab-reminders"));
    await waitFor(() => {
      expect(screen.getByTestId("cron-subtab-count-reminders")).toHaveTextContent("1");
    });
    fireEvent.click(screen.getByTestId("cron-subtab-tasks"));
    await waitFor(() => {
      expect(screen.getByTestId("cron-subtab-count-tasks")).toHaveTextContent("1");
    });
  });
});

describe("ReminderTaskView status transitions", () => {
  async function openTasks() {
    renderPanel();
    fireEvent.click(screen.getByTestId("cron-subtab-tasks"));
    await screen.findByText("write the report");
  }

  it("marks a task done and back to pending", async () => {
    await openTasks();

    expect(screen.getByTestId("task-status-t-1")).toHaveTextContent("Pending");

    // Mark done. The chip must follow the PATCH and the re-read; asserting only
    // that the PATCH was SENT would pass with a chip frozen on "Pending".
    fireEvent.click(screen.getByTestId("task-complete-t-1"));
    await waitFor(() => {
      expect(mocks.updateReminderItem).toHaveBeenCalledWith("task", "t-1", {
        status: "completed",
      });
    });
    await waitFor(() => {
      expect(screen.getByTestId("task-status-t-1")).toHaveTextContent("Completed");
    });

    // "Reopen" is the unmark: it moves it back to pending, and the chip follows
    // again. This is the round trip the feature was asked for.
    fireEvent.click(screen.getByTestId("task-reopen-t-1"));
    await waitFor(() => {
      expect(mocks.updateReminderItem).toHaveBeenLastCalledWith("task", "t-1", {
        status: "pending",
      });
    });
    await waitFor(() => {
      expect(screen.getByTestId("task-status-t-1")).toHaveTextContent("Pending");
    });

    // A cancelled item can be restored the same way, and the machine refuses
    // completed -> cancelled in one step.
    fireEvent.click(screen.getByTestId("task-cancel-t-1"));
    await waitFor(() => {
      expect(screen.getByTestId("task-status-t-1")).toHaveTextContent("Cancelled");
    });
    fireEvent.click(screen.getByTestId("task-reopen-t-1"));
    await waitFor(() => {
      expect(screen.getByTestId("task-status-t-1")).toHaveTextContent("Pending");
    });
  });

  it("cancels a task", async () => {
    await openTasks();
    fireEvent.click(screen.getByTestId("task-cancel-t-1"));
    await waitFor(() => {
      expect(mocks.updateReminderItem).toHaveBeenCalledWith("task", "t-1", {
        status: "cancelled",
      });
    });
  });

  it("offers Start only from a pending item, and not when the machine forbids it", async () => {
    await openTasks();
    expect(screen.getByTestId("task-start-t-1")).not.toBeDisabled();

    fireEvent.click(screen.getByTestId("task-start-t-1"));
    await waitFor(() => {
      expect(screen.getByTestId("task-status-t-1")).toHaveTextContent("In progress");
    });
    // in_progress -> in_progress is not a legal step, so Start is gone rather
    // than offered and then refused with a 409.
    expect(screen.queryByTestId("task-start-t-1")).not.toBeInTheDocument();

    // A completed item cannot become in_progress either.
    fireEvent.click(screen.getByTestId("task-complete-t-1"));
    await waitFor(() => {
      expect(screen.getByTestId("task-status-t-1")).toHaveTextContent("Completed");
    });
    expect(screen.queryByTestId("task-start-t-1")).not.toBeInTheDocument();
  });

  it("filters the list by status", async () => {
    await openTasks();
    expect(screen.getByText("write the report")).toBeInTheDocument();

    // Select the "Completed" filter. Nothing is completed yet, so the observable
    // result is the filter's own empty state — which is what distinguishes a
    // working filter from a list that merely failed to re-read.
    //
    // Radix portals the listbox into document.body, so the option is queried
    // there rather than in the container.
    fireEvent.click(screen.getByTestId("task-status-filter"));
    fireEvent.click(await within(document.body).findByRole("option", { name: "Completed" }));
    await waitFor(() => {
      expect(mocks.listReminderItems).toHaveBeenCalledWith(
        "task",
        expect.objectContaining({ status: "completed" }),
      );
    });
    await waitFor(() => {
      expect(screen.getByText(/No tasks with status "Completed"/)).toBeInTheDocument();
    });
    expect(screen.queryByText("write the report")).not.toBeInTheDocument();

    // "All statuses" brings it back.
    fireEvent.click(screen.getByTestId("task-status-filter"));
    fireEvent.click(await within(document.body).findByRole("option", { name: "All statuses" }));
    await waitFor(() => {
      expect(mocks.listReminderItems).toHaveBeenCalledWith(
        "task",
        expect.objectContaining({ status: undefined }),
      );
    });
    await waitFor(() => {
      expect(screen.getByText("write the report")).toBeInTheDocument();
    });
  });

  it("surfaces a rejected transition instead of swallowing it", async () => {
    await openTasks();
    // A 409 means the client's transition table drifted from the server's.
    mocks.updateReminderItem.mockRejectedValueOnce(
      new Error("illegal status transition: cannot go from pending to archived"),
    );
    fireEvent.click(screen.getByTestId("task-cancel-t-1"));
    const alert = await screen.findByRole("alert");
    expect(alert.textContent).toContain("cannot go from pending");
  });
});

describe("ReminderTaskView delete guard", () => {
  it("does not delete until the confirm is accepted", async () => {
    renderPanel();
    fireEvent.click(screen.getByTestId("cron-subtab-tasks"));
    await screen.findByText("write the report");

    fireEvent.click(screen.getByRole("button", { name: "Delete write the report" }));
    // A rendered dialog, not window.confirm — which SILENTLY RETURNS FALSE in
    // the desktop WKWebView, making the delete unreachable there.
    const dialog = await screen.findByRole("dialog");
    expect(within(dialog).getByText(/will be removed from the list/)).toBeInTheDocument();
    expect(mocks.deleteReminderItem).not.toHaveBeenCalled();

    fireEvent.click(within(dialog).getByRole("button", { name: "Delete" }));
    await waitFor(() => {
      expect(mocks.deleteReminderItem).toHaveBeenCalledWith("task", "t-1");
    });
  });

  it("cancelling the confirm leaves the item alone", async () => {
    renderPanel();
    fireEvent.click(screen.getByTestId("cron-subtab-tasks"));
    await screen.findByText("write the report");

    fireEvent.click(screen.getByRole("button", { name: "Delete write the report" }));
    const dialog = await screen.findByRole("dialog");
    fireEvent.click(within(dialog).getByRole("button", { name: "Cancel" }));
    await waitFor(() => {
      expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
    });
    expect(mocks.deleteReminderItem).not.toHaveBeenCalled();
  });

  it("explains that a delete stops a due item from firing", async () => {
    renderPanel();
    fireEvent.click(screen.getByTestId("cron-subtab-reminders"));
    await screen.findByText("stand up");
    fireEvent.click(screen.getByRole("button", { name: "Delete stand up" }));
    const dialog = await screen.findByRole("dialog");
    expect(within(dialog).getByText(/It will no longer fire/)).toBeInTheDocument();
  });
});

describe("ReminderTaskView narrow-window layout contract", () => {
  // jsdom has NO layout engine, so this cannot measure geometry — it pins the
  // CSS CONTRACT that produces the wrap. That is the repo's convention for
  // responsive behaviour (see the mobile-layout gotcha doc), and the reason is
  // concrete: the action cluster on a row is up to seven controls, and without
  // `flex-wrap` on both the cluster and the row the window drag narrower than
  // the control set squeezes the TITLE instead of dropping the buttons onto a
  // second line. `shrink-0` keeps the cluster from being the thing that gives.
  it("wraps the switcher and each row's action cluster instead of overflowing", async () => {
    renderPanel();
    fireEvent.click(screen.getByTestId("cron-subtab-tasks"));
    await screen.findByText("write the report");

    const tablist = screen.getByRole("tablist", { name: "Cron view" });
    expect(tablist.className).toContain("flex-wrap");

    // The cluster holding Reopen / Done / Cancel / Start / history / edit /
    // run-now / delete for the single task row.
    const reopen = screen.getByTestId("task-reopen-t-1");
    const cluster = reopen.parentElement as HTMLElement;
    expect(cluster.className).toContain("flex-wrap");
    expect(cluster.className).toContain("justify-end");
  });
});

describe("ReminderTaskView polling", () => {
  afterEach(() => {
    vi.useRealTimers();
  });

  it("polls only the front sub-view", async () => {
    vi.useFakeTimers();
    renderPanel(true);
    fireEvent.click(screen.getByTestId("cron-subtab-tasks"));
    await vi.waitFor(() => {
      expect(mocks.listReminderItems).toHaveBeenCalledWith("task", expect.anything());
    });
    // The poll interval is installed by an effect keyed on `ready`, which flips
    // in the load's .finally(). Advancing timers alone would not run that React
    // render, so the interval would never exist and the assertions below would
    // be checking nothing at all. act() flushes both.
    await act(async () => {
      await vi.advanceTimersByTimeAsync(0);
    });
    const callsAfterSwitch = mocks.listReminderItems.mock.calls.length;

    // Three poll intervals with the TASKS view frontmost. Only the task
    // collection may be re-read; the hidden reminders pane must not poll.
    await act(async () => {
      await vi.advanceTimersByTimeAsync(30_000);
    });
    expect(mocks.listReminderItems.mock.calls.length).toBeGreaterThan(callsAfterSwitch);
    for (const [kind] of mocks.listReminderItems.mock.calls) {
      expect(kind).toBe("task");
    }

    // Now switch away to Reminders: the task pane must stop being re-read.
    fireEvent.click(screen.getByTestId("cron-subtab-reminders"));
    await vi.waitFor(() => {
      expect(mocks.listReminderItems).toHaveBeenCalledWith("reminder", expect.anything());
    });
    await act(async () => {
      await vi.advanceTimersByTimeAsync(0);
    });
    const afterSwitch = mocks.listReminderItems.mock.calls.length;
    await act(async () => {
      await vi.advanceTimersByTimeAsync(30_000);
    });
    const afterWait = mocks.listReminderItems.mock.calls.length;
    for (const [kind] of mocks.listReminderItems.mock.calls.slice(afterSwitch)) {
      expect(kind).toBe("reminder");
    }
    // The reminder pane is the one that grew; the task pane did not.
    expect(afterWait - afterSwitch).toBeGreaterThan(0);
    expect(
      mocks.listReminderItems.mock.calls
        .slice(afterSwitch)
        .filter(([kind]) => kind === "task").length,
    ).toBe(0);
  });

  it("does not load a sub-view that has never been shown", async () => {
    renderPanel(true);
    await screen.findByText("No cron jobs yet.");
    // Opening the Cron tab on Jobs must not spend two extra requests on lists
    // the user has not looked at.
    expect(mocks.listReminderItems).not.toHaveBeenCalled();

    fireEvent.click(screen.getByTestId("cron-subtab-reminders"));
    await waitFor(() => {
      expect(mocks.listReminderItems).toHaveBeenCalledWith("reminder", expect.anything());
    });
    // Showing Reminders still must not wake the Tasks pane.
    expect(mocks.listReminderItems).not.toHaveBeenCalledWith("task", expect.anything());
  });
});
