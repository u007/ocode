import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

const mocks = vi.hoisted(() => ({
  listCronJobs: vi.fn(),
  getCronOutbox: vi.fn(),
  getCronTargets: vi.fn(),
  addCronJob: vi.fn(),
  updateCronJob: vi.fn(),
  deleteCronJob: vi.fn(),
  drainCronOutbox: vi.fn(),
  setCronTarget: vi.fn(),
  listReminderItems: vi.fn(),
  addReminderItem: vi.fn(),
  updateReminderItem: vi.fn(),
  deleteReminderItem: vi.fn(),
  runReminderItem: vi.fn(),
  getCronRuns: vi.fn(),
}));

vi.mock("@/api/client", () => ({ api: mocks }));
vi.mock("./CronJobDialog", () => ({ default: () => null }));
vi.mock("./ReminderTaskDialog", () => ({ default: () => null }));
vi.mock("./CronHistoryPanel", () => ({ default: () => <div data-testid="history" /> }));
vi.mock("./CronOutboxPanel", () => ({ default: () => <div data-testid="outbox" /> }));
vi.mock("./CronTargetsPanel", () => ({ default: () => <div data-testid="targets" /> }));

import CronPanel from "./CronPanel";

const PROJECT_A = "/Users/james/www/alpha";
const PROJECT_B = "/Users/james/www/beta";

beforeEach(() => {
  vi.clearAllMocks();
  mocks.listCronJobs.mockResolvedValue({ jobs: [] });
  mocks.getCronOutbox.mockResolvedValue({ entries: [] });
  mocks.getCronTargets.mockResolvedValue({ targets: {} });
  mocks.listReminderItems.mockResolvedValue({ items: [], total: 0, limit: 50, offset: 0 });
});

/**
 * The cron surface is per PROJECT. The panel is already keyed per project, so
 * before the project param was threaded it reset its loading state on a project
 * switch while continuing to render the previous project's data. These tests pin
 * that every read AND every write carries the project.
 */
describe("CronPanel project scoping", () => {
  it("reads every surface for the project it was given", async () => {
    render(
      <CronPanel
        loadingKey={`cron\0${PROJECT_A}`}
        onLoadingEvent={() => {}}
        active
        project={PROJECT_A}
      />,
    );

    await waitFor(() => {
      expect(mocks.listCronJobs).toHaveBeenCalledWith(PROJECT_A, undefined);
      expect(mocks.getCronOutbox).toHaveBeenCalledWith(PROJECT_A, undefined);
      expect(mocks.getCronTargets).toHaveBeenCalledWith(PROJECT_A, undefined);
    });
    for (const m of [mocks.listCronJobs, mocks.getCronOutbox, mocks.getCronTargets]) {
      for (const call of m.mock.calls) {
        expect(call[0]).toBe(PROJECT_A);
      }
    }
  });

  it("re-reads for the NEW project when the project changes", async () => {
    const { rerender } = render(
      <CronPanel
        loadingKey={`cron\0${PROJECT_A}`}
        onLoadingEvent={() => {}}
        active
        project={PROJECT_A}
      />,
    );
    await waitFor(() => expect(mocks.listCronJobs).toHaveBeenCalled());

    mocks.listCronJobs.mockClear();
    rerender(
      <CronPanel
        loadingKey={`cron\0${PROJECT_B}`}
        onLoadingEvent={() => {}}
        active
        project={PROJECT_B}
      />,
    );
    // The new project is read; the old one is never mentioned again.
    await waitFor(() => {
      expect(mocks.listCronJobs).toHaveBeenCalledWith(PROJECT_B, undefined);
    });
    for (const call of mocks.listCronJobs.mock.calls) {
      expect(call[0]).toBe(PROJECT_B);
    }
  });

  it("threads the host through for a remote project", async () => {
    render(
      <CronPanel
        loadingKey="cron"
        onLoadingEvent={() => {}}
        active
        project="~/www/aimsai2"
        host="james@217.216.72.49"
      />,
    );
    await waitFor(() => {
      expect(mocks.listCronJobs).toHaveBeenCalledWith(
        "~/www/aimsai2",
        "james@217.216.72.49",
      );
    });
  });

  it("carries the project on a mutation, not just on a read", async () => {
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
    render(
      <CronPanel
        loadingKey="cron"
        onLoadingEvent={() => {}}
        active
        project={PROJECT_A}
      />,
    );
    await screen.findByText("nightly");

    // Delete goes through a rendered confirm (window.confirm silently returns
    // false in the desktop webview), so accept it.
    fireEvent.click(screen.getByRole("button", { name: "Delete nightly" }));
    const dialog = await screen.findByRole("dialog");
    fireEvent.click(dialog.querySelector("button:last-of-type") as HTMLButtonElement);
    await waitFor(() => {
      expect(mocks.deleteCronJob).toHaveBeenCalledWith("j-1", PROJECT_A, undefined);
    });
  });

  it("scopes the reminder and task lists too", async () => {
    render(
      <CronPanel
        loadingKey="cron"
        onLoadingEvent={() => {}}
        active
        project={PROJECT_A}
      />,
    );
    fireEvent.click(screen.getByTestId("cron-subtab-tasks"));
    await waitFor(() => {
      expect(mocks.listReminderItems).toHaveBeenCalledWith(
        "task",
        expect.objectContaining({ limit: 50, offset: 0 }),
        PROJECT_A,
        undefined,
      );
    });
    fireEvent.click(screen.getByTestId("cron-subtab-reminders"));
    await waitFor(() => {
      expect(mocks.listReminderItems).toHaveBeenCalledWith(
        "reminder",
        expect.objectContaining({ limit: 50, offset: 0 }),
        PROJECT_A,
        undefined,
      );
    });
  });
});
