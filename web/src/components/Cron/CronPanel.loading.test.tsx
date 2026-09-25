import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { LoadRequestEvent } from "@/hooks/useKeyedLoad";
import { __resetTabLoadingStoreForTests, emitTabLoadEvent, useTabLoadingStore } from "@/hooks/useKeyedLoad";

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
vi.mock("./CronOutboxPanel", () => ({ default: () => <div data-testid="outbox" /> }));
vi.mock("./CronTargetsPanel", () => ({ default: () => <div data-testid="targets" /> }));
vi.mock("./CronHistoryPanel", () => ({ default: () => <div data-testid="history" /> }));

import CronPanel from "./CronPanel";

function PhaseProbe({ loadingKey }: { loadingKey: string }) {
  const states = useTabLoadingStore();
  return <output data-testid="phase">{states.get(loadingKey)?.phase ?? "missing"}</output>;
}

beforeEach(() => {
  vi.clearAllMocks();
  __resetTabLoadingStoreForTests();
  mocks.listCronJobs.mockResolvedValue({ jobs: [] });
  mocks.getCronOutbox.mockResolvedValue({ entries: [] });
  mocks.getCronTargets.mockResolvedValue({ targets: {} });
});

afterEach(() => {
  vi.useRealTimers();
  __resetTabLoadingStoreForTests();
});

describe("CronPanel keyed loading", () => {
  it("emits an initial completion and treats a manual refresh as another run", async () => {
    const events: LoadRequestEvent[] = [];
    const key = "cron-key";
    render(<CronPanel loadingKey={key} onLoadingEvent={(event) => events.push(event)} active={false} />);
    await waitFor(() => expect(events.some((event) => event.status === "empty")).toBe(true));
    const initialCount = events.filter((event) => event.status === "start").length;
    fireEvent.click(screen.getByRole("button", { name: /Refresh/ }));
    await waitFor(() => expect(events.filter((event) => event.status === "start").length).toBe(initialCount + 1));
  });

  it("does not re-block on a slow poll; it exposes only a delayed refresh", async () => {
    vi.useFakeTimers();
    try {
      let resolvePoll!: (value: { jobs: never[] }) => void;
      const pollPromise = new Promise<{ jobs: never[] }>((resolve) => {
        resolvePoll = resolve;
      });
      mocks.listCronJobs.mockResolvedValueOnce({ jobs: [] }).mockReturnValueOnce(pollPromise);
      const key = "poll-key";
      const events: LoadRequestEvent[] = [];
      const view = render(
        <>
          <CronPanel loadingKey={key} onLoadingEvent={(event) => { events.push(event); emitTabLoadEvent(event); }} active />
          <PhaseProbe loadingKey={key} />
        </>,
      );
      await act(async () => {
        for (let i = 0; i < 6; i += 1) await Promise.resolve();
      });
      expect(screen.getByTestId("phase")).toHaveTextContent("idle");
      expect(vi.getTimerCount()).toBeGreaterThan(0);

      await act(async () => {
        await vi.advanceTimersByTimeAsync(10_000);
      });
      expect(events.filter((event) => event.status === "start")).toHaveLength(2);
      expect(screen.getByTestId("phase")).toHaveTextContent("idle");
      act(() => vi.advanceTimersByTime(299));
      expect(screen.getByTestId("phase")).toHaveTextContent("idle");
      act(() => vi.advanceTimersByTime(1));
      expect(screen.getByTestId("phase")).toHaveTextContent("refresh");

      await act(async () => {
        resolvePoll({ jobs: [] });
        await pollPromise;
        await Promise.resolve();
      });
      expect(screen.getByTestId("phase")).toHaveTextContent("idle");
      view.unmount();
    } finally {
      vi.useRealTimers();
    }
  });

  it("does not poll while the initial load is still pending", async () => {
    vi.useFakeTimers();
    try {
      let resolveInitial!: (value: { jobs: never[] }) => void;
      const initial = new Promise<{ jobs: never[] }>((resolve) => {
        resolveInitial = resolve;
      });
      let calls = 0;
      mocks.listCronJobs.mockImplementation(() => {
        calls += 1;
        return calls === 1 ? initial : Promise.resolve({ jobs: [] });
      });
      const key = "initial-poll-key";
      render(
        <>
          <CronPanel loadingKey={key} onLoadingEvent={emitTabLoadEvent} active />
          <PhaseProbe loadingKey={key} />
        </>,
      );
      await act(async () => {
        for (let i = 0; i < 4; i += 1) await Promise.resolve();
      });
      expect(calls).toBe(1);
      await act(async () => {
        await vi.advanceTimersByTimeAsync(10_000);
      });
      expect(calls).toBe(1);
      await act(async () => {
        resolveInitial({ jobs: [] });
        for (let i = 0; i < 8; i += 1) await Promise.resolve();
      });
      expect(calls).toBe(1);
      await act(async () => {
        await vi.advanceTimersByTimeAsync(10_000);
      });
      expect(calls).toBe(2);
      expect(screen.queryByText("Loading cron jobs…")).toBeNull();
    } finally {
      vi.useRealTimers();
    }
  });

  it("does not poll over a retryable initial error", async () => {
    vi.useFakeTimers();
    try {
      mocks.listCronJobs.mockRejectedValueOnce(new Error("offline"));
      const key = "initial-error-key";
      render(
        <>
          <CronPanel loadingKey={key} onLoadingEvent={emitTabLoadEvent} active />
          <PhaseProbe loadingKey={key} />
        </>,
      );
      await act(async () => {
        for (let i = 0; i < 8; i += 1) await Promise.resolve();
      });
      expect(screen.getByTestId("phase")).toHaveTextContent("error");
      await act(async () => {
        await vi.advanceTimersByTimeAsync(20_000);
      });
      expect(mocks.listCronJobs).toHaveBeenCalledTimes(1);
      expect(screen.getByTestId("phase")).toHaveTextContent("error");
      fireEvent.click(screen.getByRole("button", { name: /Refresh/ }));
      await act(async () => {
        for (let i = 0; i < 8; i += 1) await Promise.resolve();
      });
      expect(screen.getByTestId("phase")).toHaveTextContent("idle");
      expect(mocks.listCronJobs).toHaveBeenCalledTimes(2);
    } finally {
      vi.useRealTimers();
    }
  });

  it("lets a manual refresh supersede a slow initial request", async () => {
    let resolveInitial!: (value: { jobs: never[] }) => void;
    let resolveRefresh!: (value: { jobs: never[] }) => void;
    const initialRequest = new Promise<{ jobs: never[] }>((resolve) => {
      resolveInitial = resolve;
    });
    const refreshRequest = new Promise<{ jobs: never[] }>((resolve) => {
      resolveRefresh = resolve;
    });
    mocks.listCronJobs
      .mockReturnValueOnce(initialRequest)
      .mockReturnValueOnce(refreshRequest);
    render(
      <>
        <CronPanel loadingKey="cron-latest-wins" onLoadingEvent={emitTabLoadEvent} active={false} />
        <PhaseProbe loadingKey="cron-latest-wins" />
      </>,
    );
    await waitFor(() => expect(mocks.listCronJobs).toHaveBeenCalledTimes(1));
    fireEvent.click(screen.getByRole("button", { name: /Refresh/ }));
    await waitFor(() => expect(mocks.listCronJobs).toHaveBeenCalledTimes(2));
    await act(async () => {
      resolveRefresh({ jobs: [] });
      await refreshRequest;
      for (let i = 0; i < 6; i += 1) await Promise.resolve();
    });
    expect(screen.getByTestId("phase")).toHaveTextContent("idle");
    await act(async () => {
      resolveInitial({ jobs: [] });
      await initialRequest;
      for (let i = 0; i < 4; i += 1) await Promise.resolve();
    });
    expect(screen.getByTestId("phase")).toHaveTextContent("idle");
  });
});
