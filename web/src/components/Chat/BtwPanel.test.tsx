import { act, fireEvent, render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { api } from "../../api/client";
import {
  __applyBtwFrameForTests,
  __resetBtwStoreForTests,
  startBtw,
} from "../../lib/btwStore";
import { BtwPanel } from "./BtwPanel";

vi.mock("../../api/client", () => ({
  api: { cancelBtw: vi.fn(async () => ({ status: "cancelled" })) },
}));
vi.mock("../../lib/eventBus", () => ({
  eventBus: { on: () => () => {} },
}));

const cancelBtw = vi.mocked(api.cancelBtw);

function openState(sessionId: string, host: string | undefined, question: string) {
  act(() => {
    startBtw(sessionId, host, question);
    __applyBtwFrameForTests(host, sessionId, { generation: 1, phase: "started", question });
  });
}

describe("BtwPanel", () => {
  beforeEach(() => {
    __resetBtwStoreForTests();
    cancelBtw.mockClear();
  });

  it("renders the question and streamed answer, and Thinking… while loading", () => {
    act(() => {
      startBtw("s1", undefined, "use tabs?");
    });
    render(<BtwPanel sessionId="s1" />);
    expect(screen.getByText("use tabs?")).toBeTruthy();
    expect(screen.getByText("Thinking…")).toBeTruthy();

    act(() => {
      __applyBtwFrameForTests(undefined, "s1", { generation: 1, phase: "started", question: "use tabs?" });
      __applyBtwFrameForTests(undefined, "s1", { generation: 1, phase: "activity", text: "→ read a.go" });
      __applyBtwFrameForTests(undefined, "s1", { generation: 1, phase: "delta", text: "Tabs " });
      __applyBtwFrameForTests(undefined, "s1", { generation: 1, phase: "delta", text: "are better." });
    });

    expect(screen.getByText("→ read a.go")).toBeTruthy();
    expect(screen.getByText("Tabs are better.")).toBeTruthy();
    expect(screen.queryByText("Thinking…")).toBeNull();
  });

  it("renders nothing without state", () => {
    render(<BtwPanel sessionId="s1" />);
    expect(screen.queryByTestId("btw-panel")).toBeNull();
  });

  it("close calls api.cancelBtw and clears the panel", () => {
    openState("s1", "devbox", "q");
    render(<BtwPanel sessionId="s1" host="devbox" />);
    fireEvent.click(screen.getByLabelText("Close side query"));
    expect(cancelBtw).toHaveBeenCalledWith("s1", "devbox");
    expect(screen.queryByTestId("btw-panel")).toBeNull();
  });

  it("Escape closes only when focus is inside the panel", () => {
    openState("s1", undefined, "q");
    render(<BtwPanel sessionId="s1" />);

    // Focus outside the panel: Esc must NOT close it (it belongs to the composer).
    (document.body as HTMLElement).focus();
    fireEvent.keyDown(window, { key: "Escape" });
    expect(screen.queryByTestId("btw-panel")).toBeTruthy();

    // Focus inside (the close button): Esc closes + cancels.
    screen.getByLabelText("Close side query").focus();
    fireEvent.keyDown(window, { key: "Escape" });
    expect(screen.queryByTestId("btw-panel")).toBeNull();
    expect(cancelBtw).toHaveBeenCalledWith("s1", undefined);
  });
});
