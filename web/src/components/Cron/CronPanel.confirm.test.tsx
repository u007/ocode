import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
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
}));

vi.mock("@/api/client", () => ({ api: mocks }));
vi.mock("./CronJobDialog", () => ({ default: () => null }));
// Mirrors the real CronOutboxPanel: a "Clear" button, disabled when empty.
vi.mock("./CronOutboxPanel", () => ({
  default: ({ entries, onClear }: { entries: unknown[]; onClear: () => Promise<void> }) => (
    <div data-testid="outbox">
      <button onClick={() => void onClear()} disabled={entries.length === 0}>
        Clear
      </button>
    </div>
  ),
}));
vi.mock("./CronTargetsPanel", () => ({ default: () => <div data-testid="targets" /> }));
vi.mock("./CronHistoryPanel", () => ({ default: () => <div data-testid="history" /> }));

import CronPanel from "./CronPanel";

const job = {
  id: "job-1",
  name: "nightly-report",
  payload: { message: "do the thing" },
  schedule: { kind: "cron", expr: "0 9 * * *" },
  state: {},
  created_at_ms: 1,
  enabled: true,
};

/**
 * Deleting a cron job was guarded by `window.confirm`, which SILENTLY RETURNS
 * FALSE in the Wails/WKWebView desktop webview — so the delete never ran there
 * while looking correct in a browser. The guard must be a rendered dialog.
 */
function renderPanel() {
  return render(<CronPanel loadingKey="cron" onLoadingEvent={() => {}} active={false} />);
}

/** Click the row's trash button. */
function clickRowTrash() {
  fireEvent.click(screen.getByRole("button", { name: "Delete nightly-report" }));
}

function dialog() {
  return screen.getByRole("dialog");
}

beforeEach(() => {
  vi.clearAllMocks();
  mocks.listCronJobs.mockResolvedValue({ jobs: [job] });
  mocks.getCronOutbox.mockResolvedValue({ entries: [] });
  mocks.getCronTargets.mockResolvedValue({ targets: {} });
  mocks.deleteCronJob.mockResolvedValue({});
});

describe("CronPanel outbox clear confirmation", () => {
  it("does not drain the outbox until the confirm is accepted", async () => {
    mocks.getCronOutbox.mockResolvedValue({
      entries: [
        {
          job_id: "job-1",
          job_name: "nightly-report",
          owner: "james",
          result: "done",
          at: "2026-09-25T09:00:00Z",
        },
      ],
    });
    renderPanel();
    await screen.findByText("nightly-report");
    await waitFor(() => expect(mocks.getCronOutbox).toHaveBeenCalled());

    fireEvent.click(screen.getByRole("button", { name: /^Clear$/ }));
    expect(dialog()).toBeDefined();
    expect(mocks.drainCronOutbox).not.toHaveBeenCalled();
  });

  it("warns that pending entries are dropped without being delivered", async () => {
    mocks.getCronOutbox.mockResolvedValue({
      entries: [
        { job_id: "job-1", job_name: "nightly-report", owner: "james", result: "done", at: "2026-09-25T09:00:00Z" },
      ],
    });
    renderPanel();
    await screen.findByText("nightly-report");
    await waitFor(() => expect(mocks.getCronOutbox).toHaveBeenCalled());
    fireEvent.click(screen.getByRole("button", { name: /^Clear$/ }));

    // Outbox.Drain() truncates the JSONL file (internal/scheduler/deliver.go),
    // so a cleared entry is gone for good and is never delivered.
    const copy = within(dialog()).getByText(/without being delivered/i);
    expect(copy).toBeDefined();
  });

  it("drains the outbox once the confirm is accepted", async () => {
    mocks.getCronOutbox.mockResolvedValue({
      entries: [
        { job_id: "job-1", job_name: "nightly-report", owner: "james", result: "done", at: "2026-09-25T09:00:00Z" },
      ],
    });
    mocks.drainCronOutbox.mockResolvedValue({ entries: [] });
    renderPanel();
    await screen.findByText("nightly-report");
    await waitFor(() => expect(mocks.getCronOutbox).toHaveBeenCalled());
    fireEvent.click(screen.getByRole("button", { name: /^Clear$/ }));
    fireEvent.click(within(dialog()).getByRole("button", { name: "Clear outbox" }));
    await waitFor(() => expect(mocks.drainCronOutbox).toHaveBeenCalledTimes(1));
  });

  it("keeps the entries when the confirm is cancelled", async () => {
    mocks.getCronOutbox.mockResolvedValue({
      entries: [
        { job_id: "job-1", job_name: "nightly-report", owner: "james", result: "done", at: "2026-09-25T09:00:00Z" },
      ],
    });
    renderPanel();
    await screen.findByText("nightly-report");
    await waitFor(() => expect(mocks.getCronOutbox).toHaveBeenCalled());
    fireEvent.click(screen.getByRole("button", { name: /^Clear$/ }));
    fireEvent.click(within(dialog()).getByRole("button", { name: "Cancel" }));

    expect(mocks.drainCronOutbox).not.toHaveBeenCalled();
    expect(screen.queryByRole("dialog")).toBeNull();
  });

  it("keeps the confirm open and shows the reason when the drain fails", async () => {
    mocks.getCronOutbox.mockResolvedValue({
      entries: [
        { job_id: "job-1", job_name: "nightly-report", owner: "james", result: "done", at: "2026-09-25T09:00:00Z" },
      ],
    });
    mocks.drainCronOutbox.mockRejectedValueOnce(new Error("outbox is locked"));
    renderPanel();
    await screen.findByText("nightly-report");
    await waitFor(() => expect(mocks.getCronOutbox).toHaveBeenCalled());
    fireEvent.click(screen.getByRole("button", { name: /^Clear$/ }));
    fireEvent.click(within(dialog()).getByRole("button", { name: "Clear outbox" }));

    await waitFor(() =>
      expect(within(dialog()).getByRole("alert").textContent).toContain("outbox is locked"),
    );
    expect(dialog()).toBeDefined();
  });
});

describe("CronPanel delete confirmation", () => {
  it("does not delete the job until the confirm is accepted", async () => {
    renderPanel();
    await screen.findByText("nightly-report");
    clickRowTrash();

    expect(screen.getByRole("dialog")).toBeDefined();
    expect(mocks.deleteCronJob).not.toHaveBeenCalled();

    fireEvent.click(within(dialog()).getByRole("button", { name: "Delete" }));
    await waitFor(() => expect(mocks.deleteCronJob).toHaveBeenCalledWith("job-1"));
  });

  it("names the job in the confirm", async () => {
    renderPanel();
    await screen.findByText("nightly-report");
    clickRowTrash();
    expect(within(dialog()).getByText("nightly-report")).toBeDefined();
  });

  it("keeps the job when the confirm is cancelled", async () => {
    renderPanel();
    await screen.findByText("nightly-report");
    clickRowTrash();
    fireEvent.click(within(dialog()).getByRole("button", { name: "Cancel" }));

    expect(mocks.deleteCronJob).not.toHaveBeenCalled();
    expect(screen.queryByRole("dialog")).toBeNull();
  });

  it("keeps the confirm open and shows the reason when the delete fails", async () => {
    mocks.deleteCronJob.mockRejectedValueOnce(new Error("job is still running"));
    renderPanel();
    await screen.findByText("nightly-report");
    clickRowTrash();
    fireEvent.click(within(dialog()).getByRole("button", { name: "Delete" }));

    await waitFor(() =>
      expect(within(dialog()).getByRole("alert").textContent).toContain("job is still running"),
    );
    expect(screen.getByRole("dialog")).toBeDefined();
  });

  it("focuses Cancel, so Enter on a fresh confirm does not delete", async () => {
    renderPanel();
    await screen.findByText("nightly-report");
    clickRowTrash();
    expect(within(dialog()).getByRole("button", { name: "Cancel" })).toHaveFocus();
  });
});
